package adapters

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/LoveLosita/smartflow/backend/active_scheduler/ports"
	"github.com/LoveLosita/smartflow/backend/active_scheduler/trigger"
	"github.com/LoveLosita/smartflow/backend/conv"
	"github.com/LoveLosita/smartflow/backend/model"
	"gorm.io/gorm"
)

// GormReaders 是主动调度 dry-run 的只读适配器。
//
// 职责边界：
// 1. 只在 adapter 层直接读取现有表，把外部领域模型转换为主动调度 facts；
// 2. 不生成候选、不写 preview、不写正式日程；
// 3. 后续拆微服务时可替换为 RPC/read model adapter，active_scheduler 主链路无需改动。
type GormReaders struct {
	db *gorm.DB
}

func NewGormReaders(db *gorm.DB) *GormReaders {
	return &GormReaders{db: db}
}

func ReadersFromGorm(readers *GormReaders) ports.Readers {
	return ports.Readers{
		TaskReader:     readers,
		ScheduleReader: readers,
		FeedbackReader: readers,
	}
}

func (r *GormReaders) ensureDB() error {
	if r == nil || r.db == nil {
		return errors.New("主动调度 GormReaders 未初始化")
	}
	return nil
}

func (r *GormReaders) GetTaskForActiveSchedule(ctx context.Context, req ports.TaskRequest) (ports.TaskFact, bool, error) {
	if err := r.ensureDB(); err != nil {
		return ports.TaskFact{}, false, err
	}
	if req.UserID <= 0 || req.TaskID <= 0 {
		return ports.TaskFact{}, false, nil
	}

	var task model.Task
	err := r.db.WithContext(ctx).
		Where("id = ? AND user_id = ?", req.TaskID, req.UserID).
		First(&task).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return ports.TaskFact{}, false, nil
		}
		return ports.TaskFact{}, false, err
	}

	estimatedSections := task.EstimatedSections
	if estimatedSections <= 0 {
		estimatedSections = 1
	}
	if estimatedSections > 4 {
		estimatedSections = 4
	}
	return ports.TaskFact{
		ID:                 task.ID,
		UserID:             task.UserID,
		Title:              task.Title,
		Priority:           task.Priority,
		IsCompleted:        task.IsCompleted,
		DeadlineAt:         task.DeadlineAt,
		UrgencyThresholdAt: task.UrgencyThresholdAt,
		EstimatedSections:  estimatedSections,
	}, true, nil
}

func (r *GormReaders) GetScheduleFactsByWindow(ctx context.Context, req ports.ScheduleWindowRequest) (ports.ScheduleWindowFacts, error) {
	if err := r.ensureDB(); err != nil {
		return ports.ScheduleWindowFacts{}, err
	}
	if req.UserID <= 0 || req.WindowStart.IsZero() || !req.WindowEnd.After(req.WindowStart) {
		return ports.ScheduleWindowFacts{}, nil
	}

	windowSlots, err := buildWindowSlots(req.WindowStart, req.WindowEnd)
	if err != nil {
		return ports.ScheduleWindowFacts{}, err
	}
	weeks := uniqueWeeks(windowSlots)

	var schedules []model.Schedule
	if len(weeks) > 0 {
		err = r.db.WithContext(ctx).
			Preload("Event").
			Where("user_id = ? AND week IN ?", req.UserID, weeks).
			Find(&schedules).Error
		if err != nil {
			return ports.ScheduleWindowFacts{}, err
		}
	}

	occupiedByKey := make(map[string]model.Schedule, len(schedules))
	eventFacts := make(map[int]*ports.ScheduleEventFact)
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

	occupiedSlots := make([]ports.Slot, 0, len(occupiedByKey))
	freeSlots := make([]ports.Slot, 0, len(windowSlots))
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

	events := make([]ports.ScheduleEventFact, 0, len(eventFacts))
	for _, fact := range eventFacts {
		events = append(events, *fact)
	}
	return ports.ScheduleWindowFacts{
		Events:                 events,
		OccupiedSlots:          occupiedSlots,
		FreeSlots:              freeSlots,
		NextDynamicTask:        firstDynamicTask(events, req.Now),
		TargetAlreadyScheduled: targetAlreadyScheduled,
	}, nil
}

func (r *GormReaders) GetFeedbackSignal(ctx context.Context, req ports.FeedbackRequest) (ports.FeedbackFact, bool, error) {
	if err := r.ensureDB(); err != nil {
		return ports.FeedbackFact{}, false, err
	}
	// 1. 第一版没有独立 feedback 表，显式传入 schedule_event target 时，把该事件视为已定位反馈目标。
	// 2. 若无法定位目标，返回 found=true + TargetKnown=false，让 observe 阶段稳定降级 ask_user。
	if req.TargetType != string(trigger.TargetTypeScheduleEvent) || req.TargetID <= 0 {
		return ports.FeedbackFact{
			FeedbackID:  firstNonEmpty(req.FeedbackID, req.IdempotencyKey),
			TargetKnown: false,
			SubmittedAt: time.Now(),
		}, true, nil
	}

	var event model.ScheduleEvent
	err := r.db.WithContext(ctx).
		Where("id = ? AND user_id = ?", req.TargetID, req.UserID).
		First(&event).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return ports.FeedbackFact{
				FeedbackID:  firstNonEmpty(req.FeedbackID, req.IdempotencyKey),
				TargetKnown: false,
				SubmittedAt: time.Now(),
			}, true, nil
		}
		return ports.FeedbackFact{}, false, err
	}
	taskItemID := 0
	if event.RelID != nil {
		taskItemID = *event.RelID
	}
	return ports.FeedbackFact{
		FeedbackID:       firstNonEmpty(req.FeedbackID, req.IdempotencyKey),
		TargetKnown:      true,
		TargetEventID:    event.ID,
		TargetTaskItemID: taskItemID,
		TargetTitle:      event.Name,
		SubmittedAt:      time.Now(),
	}, true, nil
}

func buildWindowSlots(startAt, endAt time.Time) ([]ports.Slot, error) {
	slots := make([]ports.Slot, 0, 24)
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
			slots = append(slots, ports.Slot{
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

func slotFromSchedule(schedule model.Schedule) (ports.Slot, bool) {
	startAt, endAt, err := conv.RelativeTimeToRealTime(schedule.Week, schedule.DayOfWeek, schedule.Section, schedule.Section)
	if err != nil {
		return ports.Slot{}, false
	}
	return ports.Slot{
		Week:      schedule.Week,
		DayOfWeek: schedule.DayOfWeek,
		Section:   schedule.Section,
		StartAt:   startAt,
		EndAt:     endAt,
	}, true
}

func scheduleToEventFact(schedule model.Schedule) *ports.ScheduleEventFact {
	event := schedule.Event
	relID := 0
	if event.RelID != nil {
		relID = *event.RelID
	}
	sourceType := event.TaskSourceType
	if sourceType == "" && event.Type == "task" {
		sourceType = string(trigger.TargetTypeTaskItem)
	}
	return &ports.ScheduleEventFact{
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
		sourceType = string(trigger.TargetTypeTaskItem)
	}
	return sourceType == targetType && *event.RelID == targetID
}

func firstDynamicTask(events []ports.ScheduleEventFact, now time.Time) *ports.ScheduleEventFact {
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

func uniqueWeeks(slots []ports.Slot) []int {
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

func slotKey(slot ports.Slot) string {
	return fmt.Sprintf("%d:%d:%d", slot.Week, slot.DayOfWeek, slot.Section)
}

func truncateToDate(t time.Time) time.Time {
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, t.Location())
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}
