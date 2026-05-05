package main

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	rootdao "github.com/LoveLosita/smartflow/backend/dao"
	"github.com/LoveLosita/smartflow/backend/model"
	activeapplyadapter "github.com/LoveLosita/smartflow/backend/services/active_scheduler/core/applyadapter"
	activefeedbacklocate "github.com/LoveLosita/smartflow/backend/services/active_scheduler/core/feedbacklocate"
	activegraph "github.com/LoveLosita/smartflow/backend/services/active_scheduler/core/graph"
	activepreview "github.com/LoveLosita/smartflow/backend/services/active_scheduler/core/preview"
	activesel "github.com/LoveLosita/smartflow/backend/services/active_scheduler/core/selection"
	activesvc "github.com/LoveLosita/smartflow/backend/services/active_scheduler/core/service"
	activeTrigger "github.com/LoveLosita/smartflow/backend/services/active_scheduler/core/trigger"
	agentstream "github.com/LoveLosita/smartflow/backend/services/agent/stream"
	agentsv "github.com/LoveLosita/smartflow/backend/services/agent/sv"
)

func buildActiveSchedulePreviewConfirmService(activeDAO *rootdao.ActiveScheduleDAO, dryRun *activesvc.DryRunService, scheduleApplyAdapter interface {
	ApplyActiveScheduleChanges(context.Context, activeapplyadapter.ApplyActiveScheduleRequest) (activeapplyadapter.ApplyActiveScheduleResult, error)
}) (*activesvc.PreviewConfirmService, error) {
	previewService, err := activepreview.NewService(activeDAO)
	if err != nil {
		return nil, err
	}
	return activesvc.NewPreviewConfirmService(dryRun, previewService, activeDAO, scheduleApplyAdapter)
}

// buildActiveScheduleSessionRerunFunc 把主动调度定位器 / graph / preview 能力装成聊天入口可调用的 rerun 闭包。
//
// 说明：
// 1. 这里只做最小接线：复用现有定位器 -> trigger -> graph -> preview 组件，不把 worker/notification 再搬一遍；
// 2. 成功时返回 session 状态、assistant 文本和业务卡片数据；
// 3. 失败时直接把 error 交回聊天入口，由上层统一写失败日志和 SSE 错误。
func buildActiveScheduleSessionRerunFunc(
	activeDAO *rootdao.ActiveScheduleDAO,
	graphRunner *activegraph.Runner,
	previewConfirm *activesvc.PreviewConfirmService,
	feedbackLocator *activefeedbacklocate.Service,
) agentsv.ActiveScheduleSessionRerunFunc {
	return func(
		ctx context.Context,
		session *model.ActiveScheduleSessionSnapshot,
		userMessage string,
		traceID string,
		requestStart time.Time,
	) (*agentsv.ActiveScheduleSessionRerunResult, error) {
		if activeDAO == nil || graphRunner == nil || previewConfirm == nil {
			return nil, fmt.Errorf("主动调度 rerun 依赖未初始化")
		}
		if session == nil {
			return nil, fmt.Errorf("主动调度 session 不能为空")
		}

		triggerRow, err := activeDAO.GetTriggerByID(ctx, session.TriggerID)
		if err != nil {
			return nil, err
		}
		resolvedTargetType := activeTrigger.TargetType(triggerRow.TargetType)
		resolvedTargetID := triggerRow.TargetID
		needsFeedbackLocate := activeTrigger.TriggerType(triggerRow.TriggerType) == activeTrigger.TriggerTypeUnfinishedFeedback &&
			(resolvedTargetID <= 0 || containsString(session.State.MissingInfo, "feedback_target"))

		if needsFeedbackLocate {
			if feedbackLocator == nil {
				question := firstNonEmptyString(
					activefeedbacklocate.BuildAskUserQuestion(session.State.MissingInfo),
					session.State.PendingQuestion,
				)
				nextState := session.State
				nextState.PendingQuestion = question
				nextState.MissingInfo = appendMissingString(nextState.MissingInfo, "feedback_target")
				nextState.LastCandidateID = ""
				nextState.LastNotificationID = ""
				nextState.FailedReason = ""
				nextState.ExpiresAt = nil
				return &agentsv.ActiveScheduleSessionRerunResult{
					AssistantText: question,
					SessionState:  nextState,
					SessionStatus: model.ActiveScheduleSessionStatusWaitingUserReply,
				}, nil
			}
			locateResult, locateErr := feedbackLocator.Resolve(ctx, activefeedbacklocate.Request{
				UserID:          triggerRow.UserID,
				UserMessage:     userMessage,
				PendingQuestion: session.State.PendingQuestion,
				MissingInfo:     cloneStringSlice(session.State.MissingInfo),
			})
			if locateErr != nil {
				return nil, locateErr
			}
			if locateResult.ShouldAskUser() {
				question := firstNonEmptyString(
					locateResult.AskUserQuestion,
					activefeedbacklocate.BuildAskUserQuestion(session.State.MissingInfo),
					session.State.PendingQuestion,
				)
				nextState := session.State
				nextState.PendingQuestion = question
				nextState.MissingInfo = appendMissingString(nextState.MissingInfo, "feedback_target")
				nextState.LastCandidateID = ""
				nextState.LastNotificationID = ""
				nextState.FailedReason = ""
				nextState.ExpiresAt = nil
				return &agentsv.ActiveScheduleSessionRerunResult{
					AssistantText: question,
					SessionState:  nextState,
					SessionStatus: model.ActiveScheduleSessionStatusWaitingUserReply,
				}, nil
			}
			resolvedTargetType = activeTrigger.TargetType(locateResult.TargetType)
			resolvedTargetID = locateResult.TargetID
		}

		domainTrigger := activeTrigger.ActiveScheduleTrigger{
			TriggerID:      triggerRow.ID,
			UserID:         triggerRow.UserID,
			TriggerType:    activeTrigger.TriggerType(triggerRow.TriggerType),
			Source:         activeTrigger.SourceUserFeedback,
			TargetType:     resolvedTargetType,
			TargetID:       resolvedTargetID,
			FeedbackID:     triggerRow.FeedbackID,
			IdempotencyKey: triggerRow.IdempotencyKey,
			MockNow:        nil,
			IsMockTime:     false,
			RequestedAt:    requestStart,
			TraceID:        traceID,
		}
		if err := domainTrigger.Validate(); err != nil {
			return nil, err
		}

		graphResult, err := graphRunner.Run(ctx, domainTrigger)
		if err != nil {
			return nil, err
		}
		if graphResult == nil || graphResult.DryRunData == nil || graphResult.DryRunData.Context == nil {
			return nil, fmt.Errorf("主动调度 graph 返回空结果")
		}

		selectionResult := graphResult.SelectionResult
		state := session.State
		state.LastCandidateID = strings.TrimSpace(selectionResult.SelectedCandidateID)
		state.LastNotificationID = ""
		state.FailedReason = ""
		state.MissingInfo = cloneStringSlice(graphResult.DryRunData.Context.DerivedFacts.MissingInfo)

		switch selectionResult.Action {
		case activesel.ActionSelectCandidate:
			if !graphResult.DryRunData.Observation.Decision.ShouldWritePreview {
				return nil, fmt.Errorf("主动调度 graph 选择了候选，但未产出可写 preview")
			}
			previewResp, err := previewConfirm.CreatePreviewFromDryRun(ctx, activepreview.CreatePreviewRequest{
				ActiveContext:       graphResult.DryRunData.Context,
				Observation:         graphResult.DryRunData.Observation,
				Candidates:          graphResult.DryRunData.Candidates,
				TriggerID:           triggerRow.ID,
				GeneratedAt:         requestStart,
				SelectedCandidateID: selectionResult.SelectedCandidateID,
				ExplanationText:     selectionResult.ExplanationText,
				NotificationSummary: selectionResult.NotificationSummary,
				FallbackUsed:        selectionResult.FallbackUsed,
			})
			if err != nil {
				return nil, err
			}
			state.PendingQuestion = ""
			state.MissingInfo = nil
			state.FailedReason = ""
			expiresAt := previewResp.Detail.ExpiresAt
			state.ExpiresAt = &expiresAt

			return &agentsv.ActiveScheduleSessionRerunResult{
				AssistantText: firstNonEmptyString(selectionResult.ExplanationText, selectionResult.NotificationSummary, previewResp.Detail.Explanation, previewResp.Detail.Notification, "主动调度建议已更新。"),
				BusinessCard: &agentstream.StreamBusinessCardExtra{
					CardType: "active_schedule_preview",
					Title:    "SmartFlow 日程调整建议",
					Summary:  firstNonEmptyString(selectionResult.NotificationSummary, previewResp.Detail.Notification, previewResp.Detail.Explanation),
					Data:     previewDetailToMap(previewResp.Detail),
				},
				SessionState:  state,
				SessionStatus: model.ActiveScheduleSessionStatusReadyPreview,
				PreviewID:     previewResp.Detail.PreviewID,
			}, nil

		case activesel.ActionAskUser:
			question := firstNonEmptyString(selectionResult.AskUserQuestion, selectionResult.ExplanationText, "请继续补充主动调度需要的信息。")
			state.PendingQuestion = question
			state.ExpiresAt = nil
			return &agentsv.ActiveScheduleSessionRerunResult{
				AssistantText: question,
				SessionState:  state,
				SessionStatus: model.ActiveScheduleSessionStatusWaitingUserReply,
			}, nil

		default:
			assistantText := firstNonEmptyString(selectionResult.ExplanationText, selectionResult.NotificationSummary, "当前主动调度暂时没有需要继续处理的内容。")
			state.PendingQuestion = ""
			state.MissingInfo = nil
			state.ExpiresAt = nil
			return &agentsv.ActiveScheduleSessionRerunResult{
				AssistantText: assistantText,
				SessionState:  state,
				SessionStatus: model.ActiveScheduleSessionStatusIgnored,
			}, nil
		}
	}
}

func previewDetailToMap(detail activepreview.ActiveSchedulePreviewDetail) map[string]any {
	raw, err := json.Marshal(detail)
	if err != nil {
		return map[string]any{}
	}
	var output map[string]any
	if err := json.Unmarshal(raw, &output); err != nil {
		return map[string]any{}
	}
	return output
}

func firstNonEmptyString(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

func cloneStringSlice(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	copied := make([]string, len(values))
	copy(copied, values)
	return copied
}

func appendMissingString(values []string, next string) []string {
	trimmed := strings.TrimSpace(next)
	if trimmed == "" {
		return cloneStringSlice(values)
	}
	for _, value := range values {
		if strings.TrimSpace(value) == trimmed {
			return cloneStringSlice(values)
		}
	}
	result := cloneStringSlice(values)
	return append(result, trimmed)
}

func containsString(values []string, target string) bool {
	trimmed := strings.TrimSpace(target)
	if trimmed == "" {
		return false
	}
	for _, value := range values {
		if strings.TrimSpace(value) == trimmed {
			return true
		}
	}
	return false
}
