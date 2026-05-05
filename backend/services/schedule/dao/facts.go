package dao

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/LoveLosita/smartflow/backend/services/runtime/conv"
	"github.com/LoveLosita/smartflow/backend/services/runtime/model"
	schedulecontracts "github.com/LoveLosita/smartflow/backend/shared/contracts/schedule"
	"gorm.io/gorm"
)

// GetScheduleFactsByWindow 读取主动调度所需的滚动窗口日程事实。
//
// 职责边界：
// 1. 只读取 schedule_events / schedules 并转换为跨进程 facts；
// 2. 不生成候选、不写 preview、不写正式日程；
// 3. active-scheduler 通过 RPC 调用该能力后，不再直连 schedule 表读取窗口事实。
func (d *ScheduleDAO) GetScheduleFactsByWindow(ctx context.Context, req schedulecontracts.ScheduleWindowRequest) (schedulecontracts.ScheduleWindowFacts, error) {
	if d == nil || d.db == nil {
		return schedulecontracts.ScheduleWindowFacts{}, errors.New("schedule dao 未初始化")
	}
	if req.UserID <= 0 || req.WindowStart.IsZero() || !req.WindowEnd.After(req.WindowStart) {
		return schedulecontracts.ScheduleWindowFacts{}, nil
	}

	windowSlots, err := buildWindowSlots(req.WindowStart, req.WindowEnd)
	if err != nil {
		return schedulecontracts.ScheduleWindowFacts{}, err
	}
	weeks := uniqueWeeks(windowSlots)

	var schedules []model.Schedule
	if len(weeks) > 0 {
		err = d.db.WithContext(ctx).
			Preload("Event").
			Where("user_id = ? AND week IN ?", req.UserID, weeks).
			Find(&schedules).Error
		if err != nil {
			return schedulecontracts.ScheduleWindowFacts{}, err
		}
	}

	occupiedByKey := make(map[string]model.Schedule, len(schedules))
	eventFacts := make(map[int]*schedulecontracts.ScheduleEventFact)
	targetAlreadyScheduled := false
	for _, schedule := range schedules {
		if schedule.Event == nil {
			continue
		}
		slot, ok := slotFromSchedule(schedule)
		if !ok || slot.StartAt.Before(req.WindowStart) || !slot.StartAt.Before(req.WindowEnd) {
			continue
		}
		occupiedByKey[slotKey(slot)] = schedule
		eventFact := eventFacts[schedule.EventID]
		if eventFact == nil {
			eventFact = scheduleToEventFact(schedule)
			eventFacts[schedule.EventID] = eventFact
		}
		eventFact.Slots = append(eventFact.Slots, slot)
		if isSameTarget(schedule.Event, req.TargetType, req.TargetID) {
			targetAlreadyScheduled = true
		}
	}

	occupiedSlots := make([]schedulecontracts.Slot, 0, len(occupiedByKey))
	freeSlots := make([]schedulecontracts.Slot, 0, len(windowSlots))
	for _, slot := range windowSlots {
		if schedule, exists := occupiedByKey[slotKey(slot)]; exists {
			occupied, ok := slotFromSchedule(schedule)
			if ok {
				occupiedSlots = append(occupiedSlots, occupied)
			}
			continue
		}
		freeSlots = append(freeSlots, slot)
	}

	events := make([]schedulecontracts.ScheduleEventFact, 0, len(eventFacts))
	for _, fact := range eventFacts {
		events = append(events, *fact)
	}
	return schedulecontracts.ScheduleWindowFacts{
		Events:                 events,
		OccupiedSlots:          occupiedSlots,
		FreeSlots:              freeSlots,
		NextDynamicTask:        firstDynamicTask(events, req.Now),
		TargetAlreadyScheduled: targetAlreadyScheduled,
	}, nil
}

// GetFeedbackSignal 读取主动调度 unfinished_feedback 的日程目标事实。
//
// 职责边界：
// 1. 第一版没有独立 feedback 表，因此只在 target_type=schedule_event 时定位日程事件；
// 2. 目标缺失时返回 found=true + TargetKnown=false，让 active-scheduler 稳定追问用户；
// 3. 不修改 schedule，也不写 active-scheduler 会话状态。
func (d *ScheduleDAO) GetFeedbackSignal(ctx context.Context, req schedulecontracts.FeedbackRequest) (schedulecontracts.FeedbackFact, bool, error) {
	if d == nil || d.db == nil {
		return schedulecontracts.FeedbackFact{}, false, errors.New("schedule dao 未初始化")
	}
	if req.TargetType != schedulecontracts.TargetTypeScheduleEvent || req.TargetID <= 0 {
		return unknownFeedbackTarget(req), true, nil
	}

	var event model.ScheduleEvent
	err := d.db.WithContext(ctx).
		Where("id = ? AND user_id = ?", req.TargetID, req.UserID).
		First(&event).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return unknownFeedbackTarget(req), true, nil
		}
		return schedulecontracts.FeedbackFact{}, false, err
	}
	taskItemID := 0
	if event.RelID != nil {
		taskItemID = *event.RelID
	}
	return schedulecontracts.FeedbackFact{
		FeedbackID:       firstNonEmpty(req.FeedbackID, req.IdempotencyKey),
		TargetKnown:      true,
		TargetEventID:    event.ID,
		TargetTaskItemID: taskItemID,
		TargetTitle:      event.Name,
		SubmittedAt:      time.Now(),
	}, true, nil
}

func buildWindowSlots(startAt, endAt time.Time) ([]schedulecontracts.Slot, error) {
	slots := make([]schedulecontracts.Slot, 0, 24)
	for day := truncateToDate(startAt); day.Before(endAt); day = day.AddDate(0, 0, 1) {
		week, dayOfWeek, err := conv.RealDateToRelativeDate(day.Format(conv.DateFormat))
		if err != nil {
			return nil, err
		}
		for section := 1; section <= 12; section++ {
			sectionStart, sectionEnd, err := conv.RelativeTimeToRealTime(week, dayOfWeek, section, section)
			if err != nil {
				return nil, err
			}
			if sectionStart.Before(startAt) || !sectionStart.Before(endAt) {
				continue
			}
			slots = append(slots, schedulecontracts.Slot{
				Week:      week,
				DayOfWeek: dayOfWeek,
				Section:   section,
				StartAt:   sectionStart,
				EndAt:     sectionEnd,
			})
		}
	}
	return slots, nil
}

func slotFromSchedule(schedule model.Schedule) (schedulecontracts.Slot, bool) {
	startAt, endAt, err := conv.RelativeTimeToRealTime(schedule.Week, schedule.DayOfWeek, schedule.Section, schedule.Section)
	if err != nil {
		return schedulecontracts.Slot{}, false
	}
	return schedulecontracts.Slot{
		Week:      schedule.Week,
		DayOfWeek: schedule.DayOfWeek,
		Section:   schedule.Section,
		StartAt:   startAt,
		EndAt:     endAt,
	}, true
}

func scheduleToEventFact(schedule model.Schedule) *schedulecontracts.ScheduleEventFact {
	event := schedule.Event
	relID := 0
	if event.RelID != nil {
		relID = *event.RelID
	}
	sourceType := event.TaskSourceType
	if sourceType == "" && event.Type == "task" {
		sourceType = schedulecontracts.TargetTypeTaskItem
	}
	return &schedulecontracts.ScheduleEventFact{
		ID:            event.ID,
		UserID:        event.UserID,
		Title:         event.Name,
		SourceType:    sourceType,
		RelID:         relID,
		IsDynamicTask: event.Type == "task",
		TaskItemID:    relID,
	}
}

func isSameTarget(event *model.ScheduleEvent, targetType string, targetID int) bool {
	if event == nil || targetID <= 0 || event.RelID == nil || event.Type != "task" {
		return false
	}
	sourceType := event.TaskSourceType
	if sourceType == "" {
		sourceType = schedulecontracts.TargetTypeTaskItem
	}
	return sourceType == targetType && *event.RelID == targetID
}

func firstDynamicTask(events []schedulecontracts.ScheduleEventFact, now time.Time) *schedulecontracts.ScheduleEventFact {
	for i := range events {
		if !events[i].IsDynamicTask {
			continue
		}
		for _, slot := range events[i].Slots {
			if slot.StartAt.IsZero() || !slot.StartAt.Before(now) {
				return &events[i]
			}
		}
	}
	return nil
}

func uniqueWeeks(slots []schedulecontracts.Slot) []int {
	seen := make(map[int]struct{})
	weeks := make([]int, 0)
	for _, slot := range slots {
		if _, exists := seen[slot.Week]; exists {
			continue
		}
		seen[slot.Week] = struct{}{}
		weeks = append(weeks, slot.Week)
	}
	return weeks
}

func slotKey(slot schedulecontracts.Slot) string {
	return fmt.Sprintf("%d:%d:%d", slot.Week, slot.DayOfWeek, slot.Section)
}

func truncateToDate(t time.Time) time.Time {
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, t.Location())
}

func unknownFeedbackTarget(req schedulecontracts.FeedbackRequest) schedulecontracts.FeedbackFact {
	return schedulecontracts.FeedbackFact{
		FeedbackID:  firstNonEmpty(req.FeedbackID, req.IdempotencyKey),
		TargetKnown: false,
		SubmittedAt: time.Now(),
	}
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}
