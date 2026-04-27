package newagentexecute

import (
	"encoding/json"
	"fmt"
	"strings"

	newagentmodel "github.com/LoveLosita/smartflow/backend/newAgent/model"
)

func resolveExecuteAskUserText(decision *newagentmodel.ExecuteDecision) string {
	if decision == nil {
		return "执行过程中遇到不确定的情况，需要向你确认。"
	}
	if strings.TrimSpace(decision.Speak) != "" {
		return strings.TrimSpace(decision.Speak)
	}
	if strings.TrimSpace(decision.Reason) != "" {
		return strings.TrimSpace(decision.Reason)
	}
	return "执行过程中遇到不确定的情况，需要向你确认。"
}

func pickExecuteVisibleSpeak(
	streamed string,
	afterText string,
	beforeText string,
	decision *newagentmodel.ExecuteDecision,
) string {
	if text := strings.TrimSpace(streamed); text != "" {
		return text
	}
	if text := strings.TrimSpace(afterText); text != "" {
		return text
	}
	if text := strings.TrimSpace(beforeText); text != "" {
		return text
	}
	return buildExecuteSpeakWithFallback(decision)
}

func buildExecuteSpeakWithFallback(decision *newagentmodel.ExecuteDecision) string {
	if decision == nil {
		return ""
	}

	speak := strings.TrimSpace(decision.Speak)
	if speak != "" {
		return speak
	}

	switch decision.Action {
	case newagentmodel.ExecuteActionContinue,
		newagentmodel.ExecuteActionAskUser,
		newagentmodel.ExecuteActionConfirm:
		if reason := strings.TrimSpace(decision.Reason); reason != "" {
			return reason
		}
		switch decision.Action {
		case newagentmodel.ExecuteActionAskUser:
			return "我还缺少一条关键信息，想先向你确认。"
		case newagentmodel.ExecuteActionConfirm:
			return "我先整理好这一步操作，等待你的确认。"
		default:
			return "我先继续这一步处理，马上给你结果。"
		}
	default:
		return speak
	}
}

func handleExecuteActionConfirm(
	decision *newagentmodel.ExecuteDecision,
	runtimeState *newagentmodel.AgentRuntimeState,
	flowState *newagentmodel.CommonState,
) error {
	toolCall := decision.ToolCall

	argsJSON := ""
	if toolCall.Arguments != nil {
		if raw, err := json.Marshal(toolCall.Arguments); err == nil {
			argsJSON = string(raw)
		}
	}

	runtimeState.PendingConfirmTool = &newagentmodel.PendingToolCallSnapshot{
		ToolName: toolCall.Name,
		ArgsJSON: argsJSON,
		Summary:  strings.TrimSpace(decision.Speak),
	}

	flowState.Phase = newagentmodel.PhaseWaitingConfirm
	return nil
}

func handleExecuteActionAbort(
	decision *newagentmodel.ExecuteDecision,
	flowState *newagentmodel.CommonState,
) error {
	if decision == nil || decision.Abort == nil {
		return fmt.Errorf("abort 动作缺少终止信息")
	}
	if flowState == nil {
		return fmt.Errorf("abort 动作缺少流程状态")
	}

	internalReason := strings.TrimSpace(decision.Abort.InternalReason)
	if internalReason == "" {
		internalReason = strings.TrimSpace(decision.Reason)
	}

	flowState.Abort(
		executeStageName,
		decision.Abort.Code,
		decision.Abort.UserMessage,
		internalReason,
	)
	return nil
}
