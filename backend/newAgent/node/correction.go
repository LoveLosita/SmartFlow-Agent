package newagentnode

import (
	"fmt"
	"strings"

	newagentmodel "github.com/LoveLosita/smartflow/backend/newAgent/model"
	"github.com/cloudwego/eino/schema"
)

const (
	correctionHistoryKindKey            = "newagent_history_kind"
	correctionHistoryKindCorrectionUser = "llm_correction_prompt"
)

// AppendLLMCorrection 追加 LLM 修正提示到对话历史。
//
// 设计目的：
// 1. 当 LLM 输出不符合预期（如不支持的 action、格式错误等），不应直接报错终止；
// 2. 应该给 LLM 一个自我修正的机会，把错误反馈写回历史，让它重新生成；
// 3. 该函数封装了"追加 assistant 消息 + 追加纠正提示"的通用流程。
//
// 参数说明：
// - conversationContext: 对话上下文，用于追加历史消息；
// - llmOutput: LLM 的原始输出内容，会作为 assistant 消息追加；
// - validOptionsDesc: 合法选项的描述，用于构造纠正提示。
//
// 使用示例：
//
//	AppendLLMCorrection(conversationContext, decision.Speak, "合法的 action 包括：continue、ask_user、next_plan、done")
//
// 返回值：
// - 返回 nil 表示修正流程完成，调用方应继续 Graph 循环；
// - 该函数不会返回 error，因为追加历史失败不影响主流程。
func AppendLLMCorrection(
	conversationContext *newagentmodel.ConversationContext,
	llmOutput string,
	validOptionsDesc string,
) {
	if conversationContext == nil {
		return
	}

	// 1. 构造 assistant 消息，让 LLM 知道自己刚才输出了什么。
	// 2. 空输出不回灌，避免把占位文本写进历史造成噪音。
	// 3. 与最近一条 assistant 完全相同则跳过，避免重复回灌放大复读。
	assistantContent := strings.TrimSpace(llmOutput)
	appendCorrectionAssistantIfNeeded(conversationContext, assistantContent)

	// 2. 构造纠正提示，明确告知 LLM 哪里错了、合法选项有哪些。
	//    不做硬编码的错误类型，由调用方通过 validOptionsDesc 传入。
	correctionContent := fmt.Sprintf(
		"你的输出不符合预期。%s 请重新分析当前状态，输出正确的内容。",
		validOptionsDesc,
	)
	conversationContext.AppendHistory(&schema.Message{
		Role:    schema.User,
		Content: correctionContent,
		Extra: map[string]any{
			correctionHistoryKindKey: correctionHistoryKindCorrectionUser,
		},
	})
}

// AppendLLMCorrectionWithHint 追加 LLM 修正提示（带自定义错误描述）。
//
// 相比 AppendLLMCorrection，该函数允许调用方提供更详细的错误描述，
// 适用于需要明确告知 LLM 具体哪里出错的场景。
//
// 参数说明：
// - conversationContext: 对话上下文；
// - llmOutput: LLM 的原始输出内容；
// - errorDesc: 具体的错误描述，如 "action \"invalid\" 不是合法的执行动作"；
// - validOptionsDesc: 合法选项的描述。
func AppendLLMCorrectionWithHint(
	conversationContext *newagentmodel.ConversationContext,
	llmOutput string,
	errorDesc string,
	validOptionsDesc string,
) {
	if conversationContext == nil {
		return
	}

	assistantContent := strings.TrimSpace(llmOutput)
	appendCorrectionAssistantIfNeeded(conversationContext, assistantContent)

	correctionContent := fmt.Sprintf(
		"%s %s 请重新分析当前状态，输出正确的内容。",
		errorDesc,
		validOptionsDesc,
	)
	conversationContext.AppendHistory(&schema.Message{
		Role:    schema.User,
		Content: correctionContent,
		Extra: map[string]any{
			correctionHistoryKindKey: correctionHistoryKindCorrectionUser,
		},
	})
}

// appendCorrectionAssistantIfNeeded 在纠错回灌前做最小降噪。
//
// 1. 空文本直接跳过，避免写入“占位噪音”；
// 2. 若与“最近一条 assistant 文本”完全一致则跳过，避免同句反复回灌；
// 3. 仅负责“是否回灌”判定，不负责生成纠错 user 提示。
func appendCorrectionAssistantIfNeeded(
	conversationContext *newagentmodel.ConversationContext,
	assistantContent string,
) {
	if conversationContext == nil {
		return
	}
	assistantContent = strings.TrimSpace(assistantContent)
	if assistantContent == "" {
		return
	}

	history := conversationContext.HistorySnapshot()
	for i := len(history) - 1; i >= 0; i-- {
		msg := history[i]
		if msg == nil || msg.Role != schema.Assistant {
			continue
		}
		if strings.TrimSpace(msg.Content) == assistantContent {
			return
		}
		// 只看最近一条 assistant，避免误去重很久以前的正常重复表达。
		break
	}

	conversationContext.AppendHistory(&schema.Message{
		Role:    schema.Assistant,
		Content: assistantContent,
	})
}
