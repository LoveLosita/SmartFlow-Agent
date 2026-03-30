package newagentstream

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// PayloadEmitter 是真正向外层 SSE 管道写 chunk 的最小接口。
//
// 说明：
// 1. 这里刻意不用 chan/string 绑死实现；
// 2. 上层既可以传“写 channel”的函数，也可以传“写 gin stream”的函数；
// 3. 只要签名是 `func(string) error`，都能接进来。
type PayloadEmitter func(payload string) error

// StageEmitter 是 graph/node 对“当前阶段”进行推送的兼容接口。
//
// 设计说明：
// 1. 旧调用侧仍然只关心 stage/detail 两段文本，因此这里先保留；
// 2. 新的结构化事件能力会通过 ChunkEmitter 补齐，而不是继续扩展这个函数签名；
// 3. 这样能兼顾当前兼容性和后续协议升级空间。
type StageEmitter func(stage, detail string)

// PseudoStreamOptions 描述“整段文字伪流式输出”的切块与节奏配置。
//
// 字段语义：
// 1. MinChunkRunes：达到该最小长度后，若命中标点/换行等边界，可提前切块；
// 2. MaxChunkRunes：单块最大 rune 数，超过后强制切块，避免一次性发太长；
// 3. ChunkInterval：块与块之间的等待时间；为 0 时表示只做切块，不做人为延迟。
type PseudoStreamOptions struct {
	MinChunkRunes int
	MaxChunkRunes int
	ChunkInterval time.Duration
}

const (
	defaultPseudoStreamMinChunkRunes = 8
	defaultPseudoStreamMaxChunkRunes = 24
)

// DefaultPseudoStreamOptions 返回一份适合中文短句展示的默认伪流式配置。
func DefaultPseudoStreamOptions() PseudoStreamOptions {
	return PseudoStreamOptions{
		MinChunkRunes: defaultPseudoStreamMinChunkRunes,
		MaxChunkRunes: defaultPseudoStreamMaxChunkRunes,
	}
}

// ChunkEmitter 是 newAgent 统一的 SSE chunk 发射器。
//
// 职责边界：
// 1. 负责把“正文 / 思考 / 工具事件 / 确认请求 / 中断提示”统一转换成 OpenAI 兼容 payload；
// 2. 负责在必要时把结构化事件附带成 extra，同时给当前前端提供可读的降级文本；
// 3. 不负责决定什么时候发什么，也不负责持久化状态。
type ChunkEmitter struct {
	emit      PayloadEmitter
	RequestID string
	ModelName string
	Created   int64
}

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

// NewChunkEmitter 创建统一 chunk 发射器。
//
// 兜底策略：
// 1. emit 为空时回退到 Noop，避免骨架期到处判空；
// 2. modelName 为空时回填 worker，保持 OpenAI 兼容字段稳定；
// 3. created <= 0 时用当前时间兜底，避免上层还没决定时间戳就无法复用。
func NewChunkEmitter(emit PayloadEmitter, requestID, modelName string, created int64) *ChunkEmitter {
	if emit == nil {
		emit = NoopPayloadEmitter()
	}

	modelName = strings.TrimSpace(modelName)
	if modelName == "" {
		modelName = "worker"
	}
	if created <= 0 {
		created = time.Now().Unix()
	}

	return &ChunkEmitter{
		emit:      emit,
		RequestID: strings.TrimSpace(requestID),
		ModelName: modelName,
		Created:   created,
	}
}

// EmitReasoningText 输出一段 reasoning 文字，并附带 reasoning_text extra。
func (e *ChunkEmitter) EmitReasoningText(blockID, stage, text string, includeRole bool) error {
	if e == nil || e.emit == nil {
		return nil
	}

	text = strings.TrimSpace(text)
	if text == "" {
		return nil
	}

	payload, err := ToOpenAIReasoningChunkWithExtra(
		e.RequestID,
		e.ModelName,
		e.Created,
		text,
		includeRole,
		NewReasoningTextExtra(blockID, stage),
	)
	if err != nil {
		return err
	}
	if payload == "" {
		return nil
	}
	return e.emit(payload)
}

// EmitAssistantText 输出一段 assistant 正文，并附带 assistant_text extra。
func (e *ChunkEmitter) EmitAssistantText(blockID, stage, text string, includeRole bool) error {
	if e == nil || e.emit == nil {
		return nil
	}

	text = strings.TrimSpace(text)
	if text == "" {
		return nil
	}

	payload, err := ToOpenAIAssistantChunkWithExtra(
		e.RequestID,
		e.ModelName,
		e.Created,
		text,
		includeRole,
		NewAssistantTextExtra(blockID, stage),
	)
	if err != nil {
		return err
	}
	if payload == "" {
		return nil
	}
	return e.emit(payload)
}

// EmitPseudoReasoningText 把整段 reasoning 文本按伪流式方式逐块推出。
func (e *ChunkEmitter) EmitPseudoReasoningText(ctx context.Context, blockID, stage, text string, options PseudoStreamOptions) error {
	return e.emitPseudoText(
		ctx,
		text,
		options,
		func(chunk string, includeRole bool) error {
			return e.EmitReasoningText(blockID, stage, chunk, includeRole)
		},
	)
}

// EmitPseudoAssistantText 把整段 assistant 文本按伪流式方式逐块推出。
func (e *ChunkEmitter) EmitPseudoAssistantText(ctx context.Context, blockID, stage, text string, options PseudoStreamOptions) error {
	return e.emitPseudoText(
		ctx,
		text,
		options,
		func(chunk string, includeRole bool) error {
			return e.EmitAssistantText(blockID, stage, chunk, includeRole)
		},
	)
}

// EmitStatus 输出一条阶段状态事件。
//
// 当前兼容策略：
// 1. extra 用 status 表达结构化语义；
// 2. reasoning_content 里同时放一份可读降级文本，保证旧前端也能看到。
func (e *ChunkEmitter) EmitStatus(blockID, stage, code, summary string, includeRole bool) error {
	if e == nil || e.emit == nil {
		return nil
	}

	text := BuildStageReasoningText(stage, summary)
	payload, err := ToOpenAIReasoningChunkWithExtra(
		e.RequestID,
		e.ModelName,
		e.Created,
		text,
		includeRole,
		NewStatusExtra(blockID, stage, code, summary),
	)
	if err != nil {
		return err
	}
	if payload == "" {
		return nil
	}
	return e.emit(payload)
}

// EmitToolCallStart 输出一次工具调用开始事件。
func (e *ChunkEmitter) EmitToolCallStart(blockID, stage, toolName, summary, argumentsPreview string, includeRole bool) error {
	if e == nil || e.emit == nil {
		return nil
	}

	text := BuildToolCallReasoningText(toolName, summary, argumentsPreview)
	payload, err := ToOpenAIReasoningChunkWithExtra(
		e.RequestID,
		e.ModelName,
		e.Created,
		text,
		includeRole,
		NewToolCallExtra(blockID, stage, toolName, "start", summary, argumentsPreview),
	)
	if err != nil {
		return err
	}
	if payload == "" {
		return nil
	}
	return e.emit(payload)
}

// EmitToolCallResult 输出一次工具调用结果事件。
func (e *ChunkEmitter) EmitToolCallResult(blockID, stage, toolName, summary, argumentsPreview string, includeRole bool) error {
	if e == nil || e.emit == nil {
		return nil
	}

	text := BuildToolResultReasoningText(toolName, summary)
	payload, err := ToOpenAIReasoningChunkWithExtra(
		e.RequestID,
		e.ModelName,
		e.Created,
		text,
		includeRole,
		NewToolResultExtra(blockID, stage, toolName, "done", summary, argumentsPreview),
	)
	if err != nil {
		return err
	}
	if payload == "" {
		return nil
	}
	return e.emit(payload)
}

// EmitConfirmRequest 输出一次待确认事件。
//
// 当前展示策略：
// 1. 对旧前端，confirm 文案通过 assistant content 直接可见；
// 2. 对新前端，extra.confirm 可直接驱动确认卡片或按钮；
// 3. 默认使用伪流式，避免确认文案整块砸下来太生硬。
func (e *ChunkEmitter) EmitConfirmRequest(ctx context.Context, blockID, stage, interactionID, title, summary string, options PseudoStreamOptions) error {
	if e == nil || e.emit == nil {
		return nil
	}

	text := BuildConfirmAssistantText(title, summary)
	extra := NewConfirmRequestExtra(blockID, stage, interactionID, title, summary)
	return e.emitPseudoText(
		ctx,
		text,
		options,
		func(chunk string, includeRole bool) error {
			payload, err := ToOpenAIAssistantChunkWithExtra(
				e.RequestID,
				e.ModelName,
				e.Created,
				chunk,
				includeRole,
				extra,
			)
			if err != nil {
				return err
			}
			if payload == "" {
				return nil
			}
			return e.emit(payload)
		},
	)
}

// EmitInterruptMessage 输出一次中断提示。
//
// 适用场景：
// 1. ask_user 追问；
// 2. 告知用户当前会话已进入等待状态；
// 3. 后续 connection_lost 恢复若需要对用户补一句解释，也可复用这一入口。
func (e *ChunkEmitter) EmitInterruptMessage(ctx context.Context, blockID, stage, interactionID, interactionType, summary string, options PseudoStreamOptions) error {
	if e == nil || e.emit == nil {
		return nil
	}

	text := BuildInterruptAssistantText(interactionType, summary)
	extra := NewInterruptExtra(blockID, stage, interactionID, interactionType, summary)
	return e.emitPseudoText(
		ctx,
		text,
		options,
		func(chunk string, includeRole bool) error {
			payload, err := ToOpenAIAssistantChunkWithExtra(
				e.RequestID,
				e.ModelName,
				e.Created,
				chunk,
				includeRole,
				extra,
			)
			if err != nil {
				return err
			}
			if payload == "" {
				return nil
			}
			return e.emit(payload)
		},
	)
}

// EmitFinish 统一输出 stop 结束块，并带上 finish extra。
func (e *ChunkEmitter) EmitFinish(blockID, stage string) error {
	if e == nil || e.emit == nil {
		return nil
	}

	payload, err := ToOpenAIFinishStreamWithExtra(
		e.RequestID,
		e.ModelName,
		e.Created,
		NewFinishExtra(blockID, stage),
	)
	if err != nil {
		return err
	}
	if payload == "" {
		return nil
	}
	return e.emit(payload)
}

// EmitDone 统一输出 OpenAI 兼容流式结束标记。
func (e *ChunkEmitter) EmitDone() error {
	if e == nil || e.emit == nil {
		return nil
	}
	return e.emit("[DONE]")
}

// EmitStageAsReasoning 把“阶段提示”伪装成 reasoning chunk 推给前端。
//
// 兼容说明：
// 1. 保留旧函数签名，方便当前旧链路直接复用；
// 2. 实际实现已升级为统一的 ChunkEmitter + status extra；
// 3. 这样后续新链路可以直接跳过这个兼容函数，转用结构化方法。
func EmitStageAsReasoning(emit PayloadEmitter, requestID, modelName string, created int64, stage, detail string, includeRole bool) error {
	return NewChunkEmitter(emit, requestID, modelName, created).EmitStatus(stage, stage, stage, detail, includeRole)
}

// EmitAssistantReply 把一段完整正文作为 assistant chunk 推出。
//
// 注意：
// 1. 这里保持“整段发”，不主动切块；
// 2. 若后续某条链路需要更自然的阅读节奏，应直接调用 EmitPseudoAssistantText；
// 3. 为兼容老调用侧，这里 blockID 和 stage 都留空。
func EmitAssistantReply(emit PayloadEmitter, requestID, modelName string, created int64, content string, includeRole bool) error {
	return NewChunkEmitter(emit, requestID, modelName, created).EmitAssistantText("", "", content, includeRole)
}

// EmitFinish 统一输出 stop 结束块。
func EmitFinish(emit PayloadEmitter, requestID, modelName string, created int64) error {
	return NewChunkEmitter(emit, requestID, modelName, created).EmitFinish("", "")
}

// EmitDone 统一输出 OpenAI 兼容流式结束标记。
func EmitDone(emit PayloadEmitter) error {
	return NewChunkEmitter(emit, "", "", 0).EmitDone()
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

// BuildToolCallReasoningText 生成“工具调用开始”时的可读提示文本。
func BuildToolCallReasoningText(toolName, summary, argumentsPreview string) string {
	toolName = strings.TrimSpace(toolName)
	summary = strings.TrimSpace(summary)
	argumentsPreview = strings.TrimSpace(argumentsPreview)

	lines := make([]string, 0, 3)
	if toolName != "" {
		lines = append(lines, fmt.Sprintf("正在调用工具：%s", toolName))
	}
	if summary != "" {
		lines = append(lines, summary)
	}
	if argumentsPreview != "" {
		lines = append(lines, fmt.Sprintf("参数摘要：%s", argumentsPreview))
	}
	return strings.TrimSpace(strings.Join(lines, "\n"))
}

// BuildToolResultReasoningText 生成“工具调用结果”时的可读提示文本。
func BuildToolResultReasoningText(toolName, summary string) string {
	toolName = strings.TrimSpace(toolName)
	summary = strings.TrimSpace(summary)

	switch {
	case toolName != "" && summary != "":
		return fmt.Sprintf("工具结果：%s\n%s", toolName, summary)
	case toolName != "":
		return fmt.Sprintf("工具结果：%s", toolName)
	default:
		return summary
	}
}

// BuildConfirmAssistantText 生成给用户看的确认文案。
func BuildConfirmAssistantText(title, summary string) string {
	title = strings.TrimSpace(title)
	summary = strings.TrimSpace(summary)

	switch {
	case title != "" && summary != "":
		return fmt.Sprintf("%s\n%s", title, summary)
	case title != "":
		return title
	default:
		return summary
	}
}

// BuildInterruptAssistantText 生成给用户看的中断文案。
func BuildInterruptAssistantText(interactionType, summary string) string {
	interactionType = strings.TrimSpace(interactionType)
	summary = strings.TrimSpace(summary)

	switch {
	case interactionType != "" && summary != "":
		return fmt.Sprintf("当前进入 %s 阶段。\n%s", interactionType, summary)
	case summary != "":
		return summary
	default:
		return interactionType
	}
}

func (e *ChunkEmitter) emitPseudoText(ctx context.Context, text string, options PseudoStreamOptions, emitChunk func(chunk string, includeRole bool) error) error {
	if emitChunk == nil {
		return nil
	}
	text = strings.TrimSpace(text)
	if text == "" {
		return nil
	}

	chunks := SplitPseudoStreamText(text, options)
	for i, chunk := range chunks {
		if err := emitChunk(chunk, i == 0); err != nil {
			return err
		}
		if i < len(chunks)-1 {
			if err := waitPseudoStreamInterval(ctx, options.ChunkInterval); err != nil {
				return err
			}
		}
	}
	return nil
}

// SplitPseudoStreamText 按“标点优先、长度兜底”的策略切分整段文本。
//
// 步骤说明：
// 1. 优先在句号、问号、感叹号、分号、换行等自然边界切块，保证阅读顺畅；
// 2. 若长时间遇不到合适边界，则在 MaxChunkRunes 处强制切块，避免整段卡太久；
// 3. 对中文文本优先按 rune 长度处理，避免多字节字符被截断。
func SplitPseudoStreamText(text string, options PseudoStreamOptions) []string {
	text = strings.TrimSpace(text)
	if text == "" {
		return nil
	}

	options = normalizePseudoStreamOptions(options)
	runes := []rune(text)
	if len(runes) <= options.MaxChunkRunes {
		return []string{text}
	}

	chunks := make([]string, 0, len(runes)/options.MinChunkRunes+1)
	start := 0
	size := 0
	for i, r := range runes {
		size++

		shouldFlush := false
		if size >= options.MaxChunkRunes {
			shouldFlush = true
		}
		if size >= options.MinChunkRunes && isPseudoStreamBoundary(r) {
			shouldFlush = true
		}
		if !shouldFlush {
			continue
		}

		chunk := strings.TrimSpace(string(runes[start : i+1]))
		if chunk != "" {
			chunks = append(chunks, chunk)
		}
		start = i + 1
		size = 0
	}

	if start < len(runes) {
		chunk := strings.TrimSpace(string(runes[start:]))
		if chunk != "" {
			chunks = append(chunks, chunk)
		}
	}

	if len(chunks) == 0 {
		return []string{text}
	}
	return chunks
}

func normalizePseudoStreamOptions(options PseudoStreamOptions) PseudoStreamOptions {
	if options.MinChunkRunes <= 0 {
		options.MinChunkRunes = defaultPseudoStreamMinChunkRunes
	}
	if options.MaxChunkRunes <= 0 {
		options.MaxChunkRunes = defaultPseudoStreamMaxChunkRunes
	}
	if options.MaxChunkRunes < options.MinChunkRunes {
		options.MaxChunkRunes = options.MinChunkRunes
	}
	return options
}

func isPseudoStreamBoundary(r rune) bool {
	switch r {
	case '。', '！', '？', '；', '：', '，', '.', '!', '?', ';', ':', ',', '\n':
		return true
	default:
		return false
	}
}

func waitPseudoStreamInterval(ctx context.Context, interval time.Duration) error {
	if interval <= 0 {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}

	timer := time.NewTimer(interval)
	defer timer.Stop()

	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
