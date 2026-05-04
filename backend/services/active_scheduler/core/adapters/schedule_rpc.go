package adapters

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	activeapplyadapter "github.com/LoveLosita/smartflow/backend/services/active_scheduler/core/applyadapter"
	activeports "github.com/LoveLosita/smartflow/backend/services/active_scheduler/core/ports"
	schedulepb "github.com/LoveLosita/smartflow/backend/services/schedule/rpc/pb"
	schedulecontracts "github.com/LoveLosita/smartflow/backend/shared/contracts/schedule"
	"github.com/zeromicro/go-zero/zrpc"
	"google.golang.org/genproto/googleapis/rpc/errdetails"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const (
	defaultScheduleRPCEndpoint = "127.0.0.1:9084"
	defaultScheduleRPCTimeout  = 6 * time.Second
	scheduleApplyErrorDomain   = "smartflow.schedule.apply"
)

type ScheduleRPCConfig struct {
	Endpoints []string
	Target    string
	Timeout   time.Duration
}

// ScheduleRPCAdapter 是 active-scheduler 访问 schedule 服务的 RPC 适配器。
//
// 职责边界：
// 1. 只把 active-scheduler 内部端口 DTO 与 shared/contracts/schedule 互转；
// 2. 不读取数据库、不实现 schedule 写入状态机；
// 3. 让 active-scheduler 不再直接访问 schedule_events / schedules。
type ScheduleRPCAdapter struct {
	rpc schedulepb.ScheduleClient
}

func NewScheduleRPCAdapter(cfg ScheduleRPCConfig) (*ScheduleRPCAdapter, error) {
	timeout := cfg.Timeout
	if timeout <= 0 {
		timeout = defaultScheduleRPCTimeout
	}
	endpoints := normalizeScheduleRPCEndpoints(cfg.Endpoints)
	target := strings.TrimSpace(cfg.Target)
	if len(endpoints) == 0 && target == "" {
		endpoints = []string{defaultScheduleRPCEndpoint}
	}
	zclient, err := zrpc.NewClient(zrpc.RpcClientConf{
		Endpoints: endpoints,
		Target:    target,
		NonBlock:  true,
		Timeout:   int64(timeout / time.Millisecond),
	})
	if err != nil {
		return nil, err
	}
	adapter := &ScheduleRPCAdapter{rpc: schedulepb.NewScheduleClient(zclient.Conn())}
	if err := adapter.ping(timeout); err != nil {
		return nil, err
	}
	return adapter, nil
}

func ReadersWithScheduleRPC(taskReader activeports.TaskReader, scheduleReader *ScheduleRPCAdapter) activeports.Readers {
	return activeports.Readers{
		TaskReader:     taskReader,
		ScheduleReader: scheduleReader,
		FeedbackReader: scheduleReader,
	}
}

func (a *ScheduleRPCAdapter) GetScheduleFactsByWindow(ctx context.Context, req activeports.ScheduleWindowRequest) (activeports.ScheduleWindowFacts, error) {
	if err := a.ensureReady(); err != nil {
		return activeports.ScheduleWindowFacts{}, err
	}
	payload, err := json.Marshal(toScheduleWindowContract(req))
	if err != nil {
		return activeports.ScheduleWindowFacts{}, err
	}
	resp, err := a.rpc.GetScheduleFactsByWindow(ctx, &schedulepb.JSONRequest{PayloadJson: payload})
	if err != nil {
		return activeports.ScheduleWindowFacts{}, scheduleRPCError(err)
	}
	var facts schedulecontracts.ScheduleWindowFacts
	if err := json.Unmarshal(jsonBytes(resp), &facts); err != nil {
		return activeports.ScheduleWindowFacts{}, err
	}
	return scheduleFactsToActive(facts), nil
}

func (a *ScheduleRPCAdapter) GetFeedbackSignal(ctx context.Context, req activeports.FeedbackRequest) (activeports.FeedbackFact, bool, error) {
	if err := a.ensureReady(); err != nil {
		return activeports.FeedbackFact{}, false, err
	}
	payload, err := json.Marshal(schedulecontracts.FeedbackRequest{
		UserID:         req.UserID,
		FeedbackID:     req.FeedbackID,
		IdempotencyKey: req.IdempotencyKey,
		TargetType:     req.TargetType,
		TargetID:       req.TargetID,
	})
	if err != nil {
		return activeports.FeedbackFact{}, false, err
	}
	resp, err := a.rpc.GetFeedbackSignal(ctx, &schedulepb.JSONRequest{PayloadJson: payload})
	if err != nil {
		return activeports.FeedbackFact{}, false, scheduleRPCError(err)
	}
	var contractResp schedulecontracts.FeedbackResponse
	if err := json.Unmarshal(jsonBytes(resp), &contractResp); err != nil {
		return activeports.FeedbackFact{}, false, err
	}
	return feedbackFactToActive(contractResp.Feedback), contractResp.Found, nil
}

func (a *ScheduleRPCAdapter) ApplyActiveScheduleChanges(ctx context.Context, req activeapplyadapter.ApplyActiveScheduleRequest) (activeapplyadapter.ApplyActiveScheduleResult, error) {
	if err := a.ensureReady(); err != nil {
		return activeapplyadapter.ApplyActiveScheduleResult{}, err
	}
	payload, err := json.Marshal(toScheduleApplyContract(req))
	if err != nil {
		return activeapplyadapter.ApplyActiveScheduleResult{}, err
	}
	resp, err := a.rpc.ApplyActiveScheduleChanges(ctx, &schedulepb.JSONRequest{PayloadJson: payload})
	if err != nil {
		return activeapplyadapter.ApplyActiveScheduleResult{}, scheduleRPCError(err)
	}
	var result schedulecontracts.ApplyActiveScheduleResult
	if err := json.Unmarshal(jsonBytes(resp), &result); err != nil {
		return activeapplyadapter.ApplyActiveScheduleResult{}, err
	}
	return activeapplyadapter.ApplyActiveScheduleResult{
		ApplyID:            result.ApplyID,
		AppliedEventIDs:    result.AppliedEventIDs,
		AppliedScheduleIDs: result.AppliedScheduleIDs,
	}, nil
}

func (a *ScheduleRPCAdapter) ensureReady() error {
	if a == nil || a.rpc == nil {
		return errors.New("schedule rpc adapter 未初始化")
	}
	return nil
}

func (a *ScheduleRPCAdapter) ping(timeout time.Duration) error {
	if err := a.ensureReady(); err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	_, err := a.rpc.Ping(ctx, &schedulepb.StatusResponse{})
	return scheduleRPCError(err)
}

func scheduleRPCError(err error) error {
	if err == nil {
		return nil
	}
	st, ok := status.FromError(err)
	if !ok {
		return err
	}
	for _, detail := range st.Details() {
		info, ok := detail.(*errdetails.ErrorInfo)
		if !ok || info.Domain != scheduleApplyErrorDomain {
			continue
		}
		message := strings.TrimSpace(st.Message())
		if message == "" && info.Metadata != nil {
			message = strings.TrimSpace(info.Metadata["info"])
		}
		return &activeapplyadapter.ApplyError{
			Code:    strings.TrimSpace(info.Reason),
			Message: message,
			Cause:   err,
		}
	}
	if st.Code() == codes.Internal || st.Code() == codes.Unavailable || st.Code() == codes.DeadlineExceeded {
		return fmt.Errorf("调用 schedule zrpc 服务失败: %w", err)
	}
	return err
}

func toScheduleWindowContract(req activeports.ScheduleWindowRequest) schedulecontracts.ScheduleWindowRequest {
	return schedulecontracts.ScheduleWindowRequest{
		UserID:      req.UserID,
		TargetType:  req.TargetType,
		TargetID:    req.TargetID,
		WindowStart: req.WindowStart,
		WindowEnd:   req.WindowEnd,
		Now:         req.Now,
	}
}

func scheduleFactsToActive(facts schedulecontracts.ScheduleWindowFacts) activeports.ScheduleWindowFacts {
	events := make([]activeports.ScheduleEventFact, 0, len(facts.Events))
	for _, event := range facts.Events {
		events = append(events, scheduleEventFactToActive(event))
	}
	return activeports.ScheduleWindowFacts{
		Events:                 events,
		OccupiedSlots:          scheduleSlotsToActive(facts.OccupiedSlots),
		FreeSlots:              scheduleSlotsToActive(facts.FreeSlots),
		NextDynamicTask:        scheduleEventFactPtrToActive(facts.NextDynamicTask),
		TargetAlreadyScheduled: facts.TargetAlreadyScheduled,
	}
}

func scheduleEventFactToActive(event schedulecontracts.ScheduleEventFact) activeports.ScheduleEventFact {
	return activeports.ScheduleEventFact{
		ID:             event.ID,
		UserID:         event.UserID,
		Title:          event.Title,
		SourceType:     event.SourceType,
		RelID:          event.RelID,
		IsDynamicTask:  event.IsDynamicTask,
		IsCompleted:    event.IsCompleted,
		Slots:          scheduleSlotsToActive(event.Slots),
		TaskClassID:    event.TaskClassID,
		TaskItemID:     event.TaskItemID,
		CanBeShortened: event.CanBeShortened,
	}
}

func scheduleEventFactPtrToActive(event *schedulecontracts.ScheduleEventFact) *activeports.ScheduleEventFact {
	if event == nil {
		return nil
	}
	converted := scheduleEventFactToActive(*event)
	return &converted
}

func scheduleSlotsToActive(slots []schedulecontracts.Slot) []activeports.Slot {
	out := make([]activeports.Slot, 0, len(slots))
	for _, slot := range slots {
		out = append(out, activeports.Slot{
			Week:      slot.Week,
			DayOfWeek: slot.DayOfWeek,
			Section:   slot.Section,
			StartAt:   slot.StartAt,
			EndAt:     slot.EndAt,
		})
	}
	return out
}

func feedbackFactToActive(feedback schedulecontracts.FeedbackFact) activeports.FeedbackFact {
	return activeports.FeedbackFact{
		FeedbackID:       feedback.FeedbackID,
		Text:             feedback.Text,
		TargetKnown:      feedback.TargetKnown,
		TargetEventID:    feedback.TargetEventID,
		TargetTaskItemID: feedback.TargetTaskItemID,
		TargetTitle:      feedback.TargetTitle,
		SubmittedAt:      feedback.SubmittedAt,
	}
}

func toScheduleApplyContract(req activeapplyadapter.ApplyActiveScheduleRequest) schedulecontracts.ApplyActiveScheduleRequest {
	changes := make([]schedulecontracts.ApplyChange, 0, len(req.Changes))
	for _, change := range req.Changes {
		changes = append(changes, schedulecontracts.ApplyChange{
			ChangeID:         change.ChangeID,
			ChangeType:       change.ChangeType,
			TargetType:       change.TargetType,
			TargetID:         change.TargetID,
			ToSlot:           toScheduleSlotSpan(change.ToSlot),
			DurationSections: change.DurationSections,
			Metadata:         cloneStringMap(change.Metadata),
		})
	}
	return schedulecontracts.ApplyActiveScheduleRequest{
		PreviewID:   req.PreviewID,
		ApplyID:     req.ApplyID,
		UserID:      req.UserID,
		CandidateID: req.CandidateID,
		Changes:     changes,
		RequestedAt: req.RequestedAt,
		TraceID:     req.TraceID,
	}
}

func toScheduleSlotSpan(span *activeapplyadapter.SlotSpan) *schedulecontracts.SlotSpan {
	if span == nil {
		return nil
	}
	return &schedulecontracts.SlotSpan{
		Start:            schedulecontracts.Slot{Week: span.Start.Week, DayOfWeek: span.Start.DayOfWeek, Section: span.Start.Section},
		End:              schedulecontracts.Slot{Week: span.End.Week, DayOfWeek: span.End.DayOfWeek, Section: span.End.Section},
		DurationSections: span.DurationSections,
	}
}

func jsonBytes(resp *schedulepb.JSONResponse) []byte {
	if resp == nil || len(resp.DataJson) == 0 {
		return []byte("null")
	}
	return resp.DataJson
}

func normalizeScheduleRPCEndpoints(values []string) []string {
	endpoints := make([]string, 0, len(values))
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed != "" {
			endpoints = append(endpoints, trimmed)
		}
	}
	return endpoints
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
