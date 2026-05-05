package sv

import (
	"context"
	"errors"

	rootmodel "github.com/LoveLosita/smartflow/backend/model"
	"github.com/LoveLosita/smartflow/backend/respond"
	"github.com/LoveLosita/smartflow/backend/services/schedule/core/applyadapter"
	schedulecontracts "github.com/LoveLosita/smartflow/backend/shared/contracts/schedule"
)

// DeleteScheduleEventByContract 把跨进程删除契约转换为既有 schedule 核心逻辑入参。
func (ss *ScheduleService) DeleteScheduleEventByContract(ctx context.Context, req schedulecontracts.DeleteScheduleEventsRequest) error {
	events := make([]rootmodel.UserDeleteScheduleEvent, 0, len(req.Events))
	for _, event := range req.Events {
		events = append(events, rootmodel.UserDeleteScheduleEvent{
			ID:                 event.ID,
			DeleteCourse:       event.DeleteCourse,
			DeleteEmbeddedTask: event.DeleteEmbeddedTask,
		})
	}
	return ss.DeleteScheduleEvent(ctx, events, req.UserID)
}

// GetScheduleFactsByWindow 暴露主动调度需要的滚动窗口日程事实。
func (ss *ScheduleService) GetScheduleFactsByWindow(ctx context.Context, req schedulecontracts.ScheduleWindowRequest) (schedulecontracts.ScheduleWindowFacts, error) {
	if ss == nil || ss.scheduleDAO == nil {
		return schedulecontracts.ScheduleWindowFacts{}, errors.New("schedule facts service 未初始化")
	}
	return ss.scheduleDAO.GetScheduleFactsByWindow(ctx, req)
}

// GetAgentWeekSchedule 为 agent provider 暴露原始周日程槽位事实。
//
// 职责边界：
// 1. 只读取 schedule 服务拥有的 schedules / schedule_events 数据；
// 2. 保留 embedded_task_id 和 can_be_embedded，避免 agent 用前端 DTO 还原时丢语义；
// 3. 不做缓存、不做前端展示聚合，调用侧负责组装 ScheduleState。
func (ss *ScheduleService) GetAgentWeekSchedule(ctx context.Context, req schedulecontracts.AgentScheduleWeekRequest) (schedulecontracts.AgentScheduleWeekResponse, error) {
	if ss == nil || ss.scheduleDAO == nil {
		return schedulecontracts.AgentScheduleWeekResponse{}, errors.New("schedule agent week service 未初始化")
	}
	if req.Week < 0 || req.Week > 25 {
		return schedulecontracts.AgentScheduleWeekResponse{}, respond.WeekOutOfRange
	}
	schedules, err := ss.scheduleDAO.GetUserWeeklySchedule(ctx, req.UserID, req.Week)
	if err != nil {
		return schedulecontracts.AgentScheduleWeekResponse{}, err
	}
	return schedulesToAgentWeekContract(schedules), nil
}

// GetFeedbackSignal 暴露主动调度 unfinished_feedback 的日程目标定位事实。
func (ss *ScheduleService) GetFeedbackSignal(ctx context.Context, req schedulecontracts.FeedbackRequest) (schedulecontracts.FeedbackFact, bool, error) {
	if ss == nil || ss.scheduleDAO == nil {
		return schedulecontracts.FeedbackFact{}, false, errors.New("schedule feedback service 未初始化")
	}
	return ss.scheduleDAO.GetFeedbackSignal(ctx, req)
}

// ApplyActiveScheduleChanges 在 schedule 服务内执行主动调度正式写入。
//
// 职责边界：
// 1. 只把 shared 契约转换为 schedule 私有 applyadapter 入参；
// 2. 具体事务、锁定、冲突检查和写库仍由搬运后的 applyadapter 负责；
// 3. 返回结果只包含正式落库 ID，不回写 active-scheduler preview 状态。
func (ss *ScheduleService) ApplyActiveScheduleChanges(ctx context.Context, req schedulecontracts.ApplyActiveScheduleRequest) (schedulecontracts.ApplyActiveScheduleResult, error) {
	if ss == nil || ss.applyAdapter == nil {
		return schedulecontracts.ApplyActiveScheduleResult{}, errors.New("schedule apply adapter 未初始化")
	}
	result, err := ss.applyAdapter.ApplyActiveScheduleChanges(ctx, toAdapterApplyRequest(req))
	if err != nil {
		return schedulecontracts.ApplyActiveScheduleResult{}, err
	}
	return schedulecontracts.ApplyActiveScheduleResult{
		ApplyID:            result.ApplyID,
		AppliedEventIDs:    result.AppliedEventIDs,
		AppliedScheduleIDs: result.AppliedScheduleIDs,
	}, nil
}

func toAdapterApplyRequest(req schedulecontracts.ApplyActiveScheduleRequest) applyadapter.ApplyActiveScheduleRequest {
	changes := make([]applyadapter.ApplyChange, 0, len(req.Changes))
	for _, change := range req.Changes {
		changes = append(changes, applyadapter.ApplyChange{
			ChangeID:         change.ChangeID,
			ChangeType:       change.ChangeType,
			TargetType:       change.TargetType,
			TargetID:         change.TargetID,
			ToSlot:           toAdapterSlotSpan(change.ToSlot),
			DurationSections: change.DurationSections,
			Metadata:         cloneStringMap(change.Metadata),
		})
	}
	return applyadapter.ApplyActiveScheduleRequest{
		PreviewID:   req.PreviewID,
		ApplyID:     req.ApplyID,
		UserID:      req.UserID,
		CandidateID: req.CandidateID,
		Changes:     changes,
		RequestedAt: req.RequestedAt,
		TraceID:     req.TraceID,
	}
}

func schedulesToAgentWeekContract(schedules []rootmodel.Schedule) schedulecontracts.AgentScheduleWeekResponse {
	out := make([]schedulecontracts.AgentScheduleSlot, 0, len(schedules))
	for _, item := range schedules {
		out = append(out, schedulecontracts.AgentScheduleSlot{
			ID:             item.ID,
			EventID:        item.EventID,
			UserID:         item.UserID,
			Week:           item.Week,
			DayOfWeek:      item.DayOfWeek,
			Section:        item.Section,
			EmbeddedTaskID: cloneIntPtr(item.EmbeddedTaskID),
			Status:         item.Status,
			Event:          scheduleEventToAgentContract(item.Event),
			EmbeddedTask:   scheduleTaskItemToAgentContract(item.EmbeddedTask),
		})
	}
	return schedulecontracts.AgentScheduleWeekResponse{Schedules: out}
}

func scheduleEventToAgentContract(event *rootmodel.ScheduleEvent) *schedulecontracts.AgentScheduleEvent {
	if event == nil {
		return nil
	}
	return &schedulecontracts.AgentScheduleEvent{
		ID:             event.ID,
		UserID:         event.UserID,
		Name:           event.Name,
		Location:       cloneStringPtr(event.Location),
		Type:           event.Type,
		RelID:          cloneIntPtr(event.RelID),
		TaskSourceType: event.TaskSourceType,
		CanBeEmbedded:  event.CanBeEmbedded,
		StartTime:      event.StartTime,
		EndTime:        event.EndTime,
	}
}

func scheduleTaskItemToAgentContract(item *rootmodel.TaskClassItem) *schedulecontracts.AgentScheduleTaskItem {
	if item == nil {
		return nil
	}
	return &schedulecontracts.AgentScheduleTaskItem{
		ID:           item.ID,
		CategoryID:   cloneIntPtr(item.CategoryID),
		Order:        cloneIntPtr(item.Order),
		Content:      derefString(item.Content),
		EmbeddedTime: scheduleTargetTimeToAgentContract(item.EmbeddedTime),
		Status:       cloneIntPtr(item.Status),
	}
}

func scheduleTargetTimeToAgentContract(value *rootmodel.TargetTime) *schedulecontracts.AgentScheduleTargetTime {
	if value == nil {
		return nil
	}
	return &schedulecontracts.AgentScheduleTargetTime{
		Week:        value.Week,
		DayOfWeek:   value.DayOfWeek,
		SectionFrom: value.SectionFrom,
		SectionTo:   value.SectionTo,
	}
}

func cloneIntPtr(value *int) *int {
	if value == nil {
		return nil
	}
	copied := *value
	return &copied
}

func cloneStringPtr(value *string) *string {
	if value == nil {
		return nil
	}
	copied := *value
	return &copied
}

func derefString(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func toAdapterSlotSpan(span *schedulecontracts.SlotSpan) *applyadapter.SlotSpan {
	if span == nil {
		return nil
	}
	return &applyadapter.SlotSpan{
		Start: applyadapter.Slot{
			Week:      span.Start.Week,
			DayOfWeek: span.Start.DayOfWeek,
			Section:   span.Start.Section,
		},
		End: applyadapter.Slot{
			Week:      span.End.Week,
			DayOfWeek: span.End.DayOfWeek,
			Section:   span.End.Section,
		},
		DurationSections: span.DurationSections,
	}
}

func cloneStringMap(input map[string]string) map[string]string {
	if len(input) == 0 {
		return nil
	}
	output := make(map[string]string, len(input))
	for key, value := range input {
		output[key] = value
	}
	return output
}
