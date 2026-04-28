package newagentstream

import (
	"encoding/json"

	"github.com/cloudwego/eino/schema"
)

// OpenAIChunkResponse 是 OpenAI 兼容的流式 chunk DTO。
//
// 设计说明：
// 1. 外层继续保持 OpenAI 兼容壳，避免前端和调试工具一次性大改；
// 2. 新增顶层 Extra 字段，用来承载“工具调用 / 确认请求 / 中断恢复”等结构化事件；
// 3. 这样旧前端仍可继续读取 delta.content / delta.reasoning_content，新前端则可渐进消费 extra。
type OpenAIChunkResponse struct {
	ID      string              `json:"id"`
	Object  string              `json:"object"`
	Created int64               `json:"created"`
	Model   string              `json:"model"`
	Choices []OpenAIChunkChoice `json:"choices,omitempty"`
	Extra   *OpenAIChunkExtra   `json:"extra,omitempty"`
}

// OpenAIChunkChoice 对应 OpenAI choices[0]。
type OpenAIChunkChoice struct {
	Index        int              `json:"index"`
	Delta        OpenAIChunkDelta `json:"delta"`
	FinishReason *string          `json:"finish_reason"`
}

// OpenAIChunkDelta 是真正承载 role/content/reasoning 的位置。
type OpenAIChunkDelta struct {
	Role             string `json:"role,omitempty"`
	Content          string `json:"content,omitempty"`
	ReasoningContent string `json:"reasoning_content,omitempty"`
}

// StreamExtraKind 表示当前 chunk 在业务语义上属于哪类事件。
type StreamExtraKind string

const (
	StreamExtraKindReasoningText     StreamExtraKind = "reasoning_text"
	StreamExtraKindAssistantText     StreamExtraKind = "assistant_text"
	StreamExtraKindStatus            StreamExtraKind = "status"
	StreamExtraKindToolCall          StreamExtraKind = "tool_call"
	StreamExtraKindToolResult        StreamExtraKind = "tool_result"
	StreamExtraKindConfirm           StreamExtraKind = "confirm_request"
	StreamExtraKindInterrupt         StreamExtraKind = "interrupt"
	StreamExtraKindBusinessCard      StreamExtraKind = "business_card"
	StreamExtraKindFinish            StreamExtraKind = "finish"
	StreamExtraKindScheduleCompleted StreamExtraKind = "schedule_completed"
)

// StreamDisplayMode 表示前端更适合如何展示该结构化事件。
type StreamDisplayMode string

const (
	StreamDisplayModeAppend  StreamDisplayMode = "append"
	StreamDisplayModeReplace StreamDisplayMode = "replace"
	StreamDisplayModeCard    StreamDisplayMode = "card"
)

// OpenAIChunkExtra 是挂在 OpenAI 兼容壳上的结构化扩展字段。
//
// 职责边界：
// 1. Kind / Stage / BlockID 提供前端排版和分组所需的最小元信息；
// 2. Status / Tool / Confirm / Interrupt / BusinessCard 只存展示层真正需要的摘要，不直接耦合后端完整状态对象；
// 3. Meta 留给后续做灰度扩展，避免每加一种小字段都要立刻改 DTO 结构。
type OpenAIChunkExtra struct {
	Kind         StreamExtraKind          `json:"kind,omitempty"`
	BlockID      string                   `json:"block_id,omitempty"`
	Stage        string                   `json:"stage,omitempty"`
	DisplayMode  StreamDisplayMode        `json:"display_mode,omitempty"`
	Status       *StreamStatusExtra       `json:"status,omitempty"`
	Tool         *StreamToolExtra         `json:"tool,omitempty"`
	Confirm      *StreamConfirmExtra      `json:"confirm,omitempty"`
	Interrupt    *StreamInterruptExtra    `json:"interrupt,omitempty"`
	BusinessCard *StreamBusinessCardExtra `json:"business_card,omitempty"`
	Meta         map[string]any           `json:"meta,omitempty"`
}

// StreamStatusExtra 表示普通阶段状态或提示性事件。
type StreamStatusExtra struct {
	Code    string `json:"code,omitempty"`
	Summary string `json:"summary,omitempty"`
}

// StreamToolExtra 表示一次工具调用相关事件。
type StreamToolExtra struct {
	Name             string         `json:"name,omitempty"`
	Status           string         `json:"status,omitempty"`
	Summary          string         `json:"summary,omitempty"`
	ArgumentsPreview string         `json:"arguments_preview,omitempty"`
	ArgumentView     map[string]any `json:"argument_view,omitempty"`
	ResultView       map[string]any `json:"result_view,omitempty"`
}

// StreamConfirmExtra 表示一次待确认事件的展示摘要。
type StreamConfirmExtra struct {
	InteractionID string `json:"interaction_id,omitempty"`
	Title         string `json:"title,omitempty"`
	Summary       string `json:"summary,omitempty"`
}

// StreamInterruptExtra 表示一次中断事件的展示摘要。
type StreamInterruptExtra struct {
	InteractionID string `json:"interaction_id,omitempty"`
	Type          string `json:"type,omitempty"`
	Summary       string `json:"summary,omitempty"`
}

// StreamBusinessCardExtra 表示一张业务结果卡片。
//
// 职责边界：
// 1. CardType 只允许前端已约定的卡片类型（task_query/task_record）；
// 2. Source 仅在 task_record 时有语义，其他卡片类型可为空；
// 3. Data 承载“可直接渲染的最小快照”，避免前端再二次补拉才能看到结果。
type StreamBusinessCardExtra struct {
	CardType string         `json:"card_type,omitempty"`
	Title    string         `json:"title,omitempty"`
	Summary  string         `json:"summary,omitempty"`
	Source   string         `json:"source,omitempty"`
	Data     map[string]any `json:"data,omitempty"`
}

// ToOpenAIStream 把 Eino message 转成 OpenAI 兼容 chunk。
func ToOpenAIStream(chunk *schema.Message, requestID, modelName string, created int64, includeRole bool) (string, error) {
	return ToOpenAIStreamWithExtra(chunk, requestID, modelName, created, includeRole, nil)
}

// ToOpenAIStreamWithExtra 把 Eino message 转成带 extra 的 OpenAI 兼容 chunk。
//
// 职责边界：
// 1. 负责把 chunk.Content / chunk.ReasoningContent 映射到协议字段；
// 2. 负责挂载可选 extra，供前端识别工具调用、确认请求等结构化事件；
// 3. 不负责发送，也不负责决定“这个 chunk 该不该推”。
func ToOpenAIStreamWithExtra(chunk *schema.Message, requestID, modelName string, created int64, includeRole bool, extra *OpenAIChunkExtra) (string, error) {
	delta := OpenAIChunkDelta{}
	if includeRole {
		delta.Role = "assistant"
	}
	if chunk != nil {
		delta.Content = chunk.Content
		delta.ReasoningContent = chunk.ReasoningContent
	}
	return buildOpenAIChunkPayload(requestID, modelName, created, delta, nil, extra)
}

// ToOpenAIReasoningChunk 直接构造一个 reasoning chunk。
func ToOpenAIReasoningChunk(requestID, modelName string, created int64, reasoning string, includeRole bool) (string, error) {
	return ToOpenAIReasoningChunkWithExtra(requestID, modelName, created, reasoning, includeRole, nil)
}

// ToOpenAIReasoningChunkWithExtra 直接构造一个带 extra 的 reasoning chunk。
func ToOpenAIReasoningChunkWithExtra(requestID, modelName string, created int64, reasoning string, includeRole bool, extra *OpenAIChunkExtra) (string, error) {
	delta := OpenAIChunkDelta{ReasoningContent: reasoning}
	if includeRole {
		delta.Role = "assistant"
	}
	return buildOpenAIChunkPayload(requestID, modelName, created, delta, nil, extra)
}

// ToOpenAIAssistantChunk 直接构造一个正文 chunk。
func ToOpenAIAssistantChunk(requestID, modelName string, created int64, content string, includeRole bool) (string, error) {
	return ToOpenAIAssistantChunkWithExtra(requestID, modelName, created, content, includeRole, nil)
}

// ToOpenAIAssistantChunkWithExtra 直接构造一个带 extra 的正文 chunk。
func ToOpenAIAssistantChunkWithExtra(requestID, modelName string, created int64, content string, includeRole bool, extra *OpenAIChunkExtra) (string, error) {
	delta := OpenAIChunkDelta{Content: content}
	if includeRole {
		delta.Role = "assistant"
	}
	return buildOpenAIChunkPayload(requestID, modelName, created, delta, nil, extra)
}

// ToOpenAIFinishStream 生成流式结束 chunk（finish_reason=stop）。
func ToOpenAIFinishStream(requestID, modelName string, created int64) (string, error) {
	return ToOpenAIFinishStreamWithExtra(requestID, modelName, created, nil)
}

// ToOpenAIFinishStreamWithExtra 生成带 extra 的流式结束 chunk。
func ToOpenAIFinishStreamWithExtra(requestID, modelName string, created int64, extra *OpenAIChunkExtra) (string, error) {
	stop := "stop"
	return buildOpenAIChunkPayload(requestID, modelName, created, OpenAIChunkDelta{}, &stop, extra)
}

// NewReasoningTextExtra 创建“思考文字”事件的 extra。
func NewReasoningTextExtra(blockID, stage string) *OpenAIChunkExtra {
	return &OpenAIChunkExtra{
		Kind:        StreamExtraKindReasoningText,
		BlockID:     blockID,
		Stage:       stage,
		DisplayMode: StreamDisplayModeAppend,
	}
}

// NewAssistantTextExtra 创建“正文文字”事件的 extra。
func NewAssistantTextExtra(blockID, stage string) *OpenAIChunkExtra {
	return &OpenAIChunkExtra{
		Kind:        StreamExtraKindAssistantText,
		BlockID:     blockID,
		Stage:       stage,
		DisplayMode: StreamDisplayModeAppend,
	}
}

// NewStatusExtra 创建普通状态事件的 extra。
func NewStatusExtra(blockID, stage, code, summary string) *OpenAIChunkExtra {
	return &OpenAIChunkExtra{
		Kind:        StreamExtraKindStatus,
		BlockID:     blockID,
		Stage:       stage,
		DisplayMode: StreamDisplayModeCard,
		Status: &StreamStatusExtra{
			Code:    code,
			Summary: summary,
		},
	}
}

// NewToolCallExtra 创建“工具调用开始/中间态”事件的 extra。
func NewToolCallExtra(blockID, stage, toolName, status, summary, argumentsPreview string) *OpenAIChunkExtra {
	return &OpenAIChunkExtra{
		Kind:        StreamExtraKindToolCall,
		BlockID:     blockID,
		Stage:       stage,
		DisplayMode: StreamDisplayModeCard,
		Tool: &StreamToolExtra{
			Name:             toolName,
			Status:           status,
			Summary:          summary,
			ArgumentsPreview: argumentsPreview,
		},
	}
}

// NewToolResultExtra 创建“工具结果”事件的 extra。
func NewToolResultExtra(
	blockID string,
	stage string,
	toolName string,
	status string,
	summary string,
	argumentsPreview string,
	argumentView map[string]any,
	resultView map[string]any,
) *OpenAIChunkExtra {
	return &OpenAIChunkExtra{
		Kind:        StreamExtraKindToolResult,
		BlockID:     blockID,
		Stage:       stage,
		DisplayMode: StreamDisplayModeCard,
		Tool: &StreamToolExtra{
			Name:             toolName,
			Status:           status,
			Summary:          summary,
			ArgumentsPreview: argumentsPreview,
			ArgumentView:     argumentView,
			ResultView:       resultView,
		},
	}
}

// NewConfirmRequestExtra 创建“待确认”事件的 extra。
func NewConfirmRequestExtra(blockID, stage, interactionID, title, summary string) *OpenAIChunkExtra {
	return &OpenAIChunkExtra{
		Kind:        StreamExtraKindConfirm,
		BlockID:     blockID,
		Stage:       stage,
		DisplayMode: StreamDisplayModeCard,
		Confirm: &StreamConfirmExtra{
			InteractionID: interactionID,
			Title:         title,
			Summary:       summary,
		},
	}
}

// NewInterruptExtra 创建“中断”事件的 extra。
func NewInterruptExtra(blockID, stage, interactionID, interactionType, summary string) *OpenAIChunkExtra {
	return &OpenAIChunkExtra{
		Kind:        StreamExtraKindInterrupt,
		BlockID:     blockID,
		Stage:       stage,
		DisplayMode: StreamDisplayModeCard,
		Interrupt: &StreamInterruptExtra{
			InteractionID: interactionID,
			Type:          interactionType,
			Summary:       summary,
		},
	}
}

// NewBusinessCardExtra 创建“业务结果卡片”事件的 extra。
func NewBusinessCardExtra(blockID, stage string, businessCard *StreamBusinessCardExtra) *OpenAIChunkExtra {
	return &OpenAIChunkExtra{
		Kind:         StreamExtraKindBusinessCard,
		BlockID:      blockID,
		Stage:        stage,
		DisplayMode:  StreamDisplayModeCard,
		BusinessCard: businessCard,
	}
}

// NewScheduleCompletedExtra 创建”排程完毕”卡片事件的 extra。
//
// 职责边界：
// 1. 仅作为前端渲染”排程完毕小卡片”的信号，不携带排程数据；
// 2. 前端收到此事件后，自行通过对话 ID 调用现有接口拉取排程详情；
// 3. 触发条件：CommonState.HasScheduleChanges == true 且 IsCompleted()。
func NewScheduleCompletedExtra(blockID, stage string) *OpenAIChunkExtra {
	return &OpenAIChunkExtra{
		Kind:        StreamExtraKindScheduleCompleted,
		BlockID:     blockID,
		Stage:       stage,
		DisplayMode: StreamDisplayModeCard,
	}
}

// NewFinishExtra 创建”收尾完成”事件的 extra。
func NewFinishExtra(blockID, stage string) *OpenAIChunkExtra {
	return &OpenAIChunkExtra{
		Kind:        StreamExtraKindFinish,
		BlockID:     blockID,
		Stage:       stage,
		DisplayMode: StreamDisplayModeReplace,
	}
}

func buildOpenAIChunkPayload(requestID, modelName string, created int64, delta OpenAIChunkDelta, finishReason *string, extra *OpenAIChunkExtra) (string, error) {
	// 1. 若既没有 role，也没有正文/思考，也没有 finish_reason，且也没有 extra，则视为“空块”，直接跳过。
	// 2. 这样后续 emitter 即使拆成“结构化事件 + 文本事件”双轨，也能复用统一的空块兜底。
	if delta.Role == "" && delta.Content == "" && delta.ReasoningContent == "" && finishReason == nil && !hasStreamExtra(extra) {
		return "", nil
	}

	choices := make([]OpenAIChunkChoice, 0, 1)
	if delta.Role != "" || delta.Content != "" || delta.ReasoningContent != "" || finishReason != nil {
		choices = append(choices, OpenAIChunkChoice{
			Index:        0,
			Delta:        delta,
			FinishReason: finishReason,
		})
	}

	dto := OpenAIChunkResponse{
		ID:      requestID,
		Object:  "chat.completion.chunk",
		Created: created,
		Model:   modelName,
		Choices: choices,
		Extra:   extra,
	}
	data, err := json.Marshal(dto)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func hasStreamExtra(extra *OpenAIChunkExtra) bool {
	if extra == nil {
		return false
	}
	return extra.Kind != "" ||
		extra.BlockID != "" ||
		extra.Stage != "" ||
		extra.DisplayMode != "" ||
		extra.Status != nil ||
		extra.Tool != nil ||
		extra.Confirm != nil ||
		extra.Interrupt != nil ||
		extra.BusinessCard != nil ||
		len(extra.Meta) > 0
}
