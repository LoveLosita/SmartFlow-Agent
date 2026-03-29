package newagentstream

import (
	"fmt"
	"strings"
)

// PayloadEmitter 是真正向外层 SSE 管道写 chunk 的最小接口。
//
// 说明：
// 1. 这里刻意不用 chan/string 绑死实现；
// 2. 上层既可以传“写 channel”的函数，也可以传“写 gin stream”的函数；
// 3. 只要签名是 `func(string) error`，都能接进来。
type PayloadEmitter func(payload string) error

// StageEmitter 是 graph/node 对“当前阶段”进行推送的最小接口。
type StageEmitter func(stage, detail string)

// NoopPayloadEmitter 返回一个空实现，便于骨架期安全占位。
func NoopPayloadEmitter() PayloadEmitter {
	return func(string) error { return nil }
}

// NoopStageEmitter 返回一个空实现，避免 graph 在没有接前端时处处判空。
func NoopStageEmitter() StageEmitter {
	return func(stage, detail string) {}
}

// WrapStageEmitter 把可空函数包装成稳定的 StageEmitter。
func WrapStageEmitter(fn func(stage, detail string)) StageEmitter {
	if fn == nil {
		return NoopStageEmitter()
	}
	return fn
}

// EmitStageAsReasoning 把“阶段提示”伪装成 reasoning chunk 推给前端。
//
// 设计背景：
// 1. 你当前 Apifox 只认思考块和正文块，因此阶段提示需要先借 reasoning_content 走通；
// 2. 这样后续真正前端上线时，只需要在这一层换协议，而不必回到各 skill 重改 graph；
// 3. 这里不拼花哨格式，只给出稳定、可读、可 grep 的文本。
func EmitStageAsReasoning(emit PayloadEmitter, requestID, modelName string, created int64, stage, detail string, includeRole bool) error {
	if emit == nil {
		return nil
	}

	text := BuildStageReasoningText(stage, detail)
	payload, err := ToOpenAIReasoningChunk(requestID, modelName, created, text, includeRole)
	if err != nil {
		return err
	}
	if payload == "" {
		return nil
	}
	return emit(payload)
}

// EmitAssistantReply 把一段完整正文作为 assistant chunk 推出。
//
// 注意：
// 1. 这里是“整段发”，不是把文本强行拆碎；
// 2. 这样后续如果某条链路不需要真流式，也可以复用统一出口；
// 3. 真正按 token/chunk 细粒度流式输出，应由 llm.Stream + 上层循环处理。
func EmitAssistantReply(emit PayloadEmitter, requestID, modelName string, created int64, content string, includeRole bool) error {
	if emit == nil {
		return nil
	}
	payload, err := ToOpenAIAssistantChunk(requestID, modelName, created, content, includeRole)
	if err != nil {
		return err
	}
	if payload == "" {
		return nil
	}
	return emit(payload)
}

// EmitFinish 统一输出 stop 结束块。
func EmitFinish(emit PayloadEmitter, requestID, modelName string, created int64) error {
	if emit == nil {
		return nil
	}
	payload, err := ToOpenAIFinishStream(requestID, modelName, created)
	if err != nil {
		return err
	}
	if payload == "" {
		return nil
	}
	return emit(payload)
}

// EmitDone 统一输出 OpenAI 兼容流式结束标记。
func EmitDone(emit PayloadEmitter) error {
	if emit == nil {
		return nil
	}
	return emit("[DONE]")
}

// BuildStageReasoningText 生成统一阶段提示文本。
func BuildStageReasoningText(stage, detail string) string {
	stage = strings.TrimSpace(stage)
	detail = strings.TrimSpace(detail)

	switch {
	case stage != "" && detail != "":
		return fmt.Sprintf("阶段：%s\n%s", stage, detail)
	case stage != "":
		return fmt.Sprintf("阶段：%s", stage)
	default:
		return detail
	}
}
