package sv

import (
	"context"
	"errors"

	rootmodel "github.com/LoveLosita/smartflow/backend/model"
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
