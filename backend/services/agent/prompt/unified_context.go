package agentprompt

import (
	"fmt"
	"strings"

	agentmodel "github.com/LoveLosita/smartflow/backend/services/agent/model"
	"github.com/cloudwego/eino/schema"
)

// ConversationTurn 表示对话历史中的一轮自然语言交互。
//
// 职责边界：
// 1. 这里只承载 user 与 assistant speak，不承载 tool_call 和 tool observation；
// 2. 供 chat / plan / deliver 等节点复用，避免各节点重复写一套提取逻辑；
// 3. 不负责裁剪长度，长度预算统一交给压缩层处理。
type ConversationTurn struct {
	Role    string
	Content string
}

// StageMessagesConfig 描述统一四段式骨架下，各节点自行提供的内容块。
//
// 设计目标：
// 1. 统一层只负责“四条消息怎么拼”，不再替节点决定“每条消息里该放什么”；
// 2. Msg1 / Msg2 / Msg3Prefix / Msg3Suffix 都由节点自己渲染，避免 chat / plan / deliver 继续套 execute 的内容模板；
// 3. memory_context 仍由统一层单入口注入到 msg3，避免多处重复注入。
type StageMessagesConfig struct {
	// SystemPrompt 是节点自己的系统提示词。
	SystemPrompt string

	// Msg1Content 是第 2 条 assistant 消息，通常放“节点想看的历史视图”。
	Msg1Content string

	// Msg2Content 是第 3 条 assistant 消息，通常放“节点自己的工作区/补充约束”。
	Msg2Content string

	// Msg3Prefix 是第 4 条消息中位于 memory_context 之前的内容。
	// 常见放法：阶段状态、规划工作区摘要、交付收口约束等。
	Msg3Prefix string

	// Msg3Suffix 是第 4 条消息中位于 memory_context 之后的内容。
	// 对 user-role 节点来说，这里通常放最终用户指令，保证“用户输入收尾”。
	Msg3Suffix string

	// Msg3Role 指定第 4 条消息的角色。
	// Execute 继续使用 system，其余节点一般使用 user。
	Msg3Role schema.RoleType

	// SkipBaseSystemPrompt 为 true 时，msg0 只使用节点自己的 SystemPrompt，
	// 不再拼接 ConversationContext.SystemPrompt。
	SkipBaseSystemPrompt bool

	// UseLiteToolCatalogMsg 为 true 时，msg0 工具目录采用轻量模式（仅名称与职责）。
	UseLiteToolCatalogMsg bool
}

// buildUnifiedStageMessages 组装统一 4 段式消息骨架。
//
// 固定布局：
// 1. msg0(system)：系统规则 + 阶段规则 + 工具简表；
// 2. msg1(assistant)：节点自定义的历史视图；
// 3. msg2(assistant)：节点自定义的工作区；
// 4. msg3(user/system)：节点自定义前后缀 + 统一 memory_context。
func buildUnifiedStageMessages(
	ctx *agentmodel.ConversationContext,
	config StageMessagesConfig,
) []*schema.Message {
	msg0 := buildUnifiedMsg0(config.SystemPrompt, ctx, config.SkipBaseSystemPrompt, config.UseLiteToolCatalogMsg)
	msg1 := buildUnifiedMsg1(config.Msg1Content)
	msg2 := buildUnifiedMsg2(config.Msg2Content)
	msg3 := buildUnifiedMsg3(ctx, config)

	return []*schema.Message{
		schema.SystemMessage(msg0),
		{Role: schema.Assistant, Content: msg1},
		{Role: schema.Assistant, Content: msg2},
		buildUnifiedMsg3Message(msg3, config.Msg3Role),
	}
}

// buildUnifiedMsg3Message 根据配置决定第 4 条消息的角色。
func buildUnifiedMsg3Message(content string, role schema.RoleType) *schema.Message {
	if role == schema.User {
		return schema.UserMessage(content)
	}
	return schema.SystemMessage(content)
}

// buildUnifiedMsg0 合并系统提示 + 工具简表，生成 msg0。
//
// 步骤化说明：
// 1. 先合并基础系统提示与节点系统提示，保证模型身份稳定；
// 2. 若当前节点注入了工具 schema，则附加紧凑工具目录；
// 3. 若两部分都为空，则回退到最小兜底提示，避免出现空消息。
func buildUnifiedMsg0(stageSystemPrompt string, ctx *agentmodel.ConversationContext, skipBaseSystemPrompt bool, useLiteToolCatalog bool) string {
	base := ""
	if skipBaseSystemPrompt {
		base = strings.TrimSpace(stageSystemPrompt)
	} else {
		base = strings.TrimSpace(mergeSystemPrompts(ctx, stageSystemPrompt))
	}
	if base == "" {
		base = "你是 SmartMate 助手，请继续当前阶段。"
	}

	toolCatalog := renderExecuteToolCatalogCompact(ctx, nil)
	if useLiteToolCatalog {
		toolCatalog = renderUnifiedToolCatalogLite(ctx)
	}
	if toolCatalog == "" {
		return base
	}
	return base + "\n\n" + toolCatalog
}

// renderUnifiedToolCatalogLite 渲染统一阶段可用工具的轻量目录。
//
// 1. 只展示工具名和一句话职责，避免把 execute 的参数/返回示例污染到 plan/chat/deliver。
// 2. 目录信息仅用于“能力边界感知”，不承担具体参数指导。
// 3. 当工具数量过多时保留前若干项并给出省略提示，控制 msg0 体积。
func renderUnifiedToolCatalogLite(ctx *agentmodel.ConversationContext) string {
	if ctx == nil {
		return ""
	}

	schemas := ctx.ToolSchemasSnapshot()
	if len(schemas) == 0 {
		return ""
	}

	const maxItems = 18
	lines := []string{"当前可用工具（轻量目录）："}
	added := 0

	for _, item := range schemas {
		name := strings.TrimSpace(item.Name)
		if name == "" {
			continue
		}
		desc := strings.TrimSpace(item.Desc)
		if desc == "" {
			lines = append(lines, fmt.Sprintf("- %s", name))
		} else {
			lines = append(lines, fmt.Sprintf("- %s：%s", name, desc))
		}
		added++
		if added >= maxItems {
			break
		}
	}

	if added == 0 {
		return ""
	}
	if len(schemas) > added {
		lines = append(lines, fmt.Sprintf("- 其余 %d 个工具已省略（按需再看）。", len(schemas)-added))
	}
	return strings.Join(lines, "\n")
}

// buildUnifiedMsg1 返回节点自行提供的历史视图。
//
// 说明：
// 1. 统一层不再内置 execute 风格的 ReAct 摘要；
// 2. 节点若未传入内容，则回退到最小占位，保证四段结构稳定；
// 3. 压缩层仍会统一统计和压缩这条消息。
func buildUnifiedMsg1(content string) string {
	content = strings.TrimSpace(content)
	if content != "" {
		return content
	}
	return "历史上下文：暂无。"
}

// buildUnifiedMsg2 返回节点自行提供的工作区。
//
// 说明：
// 1. 非 execute 节点也允许有自己的 msg2，不再被统一层硬塞“暂无”语义；
// 2. 若节点暂时没有额外工作区，则回退到最小占位，保证结构稳定。
func buildUnifiedMsg2(content string) string {
	content = strings.TrimSpace(content)
	if content != "" {
		return content
	}
	return "阶段工作区：暂无。"
}

// buildUnifiedMsg3 统一拼装 msg3：前缀 + memory_context + 后缀。
//
// 步骤化说明：
// 1. 前缀由节点决定，适合放轻量状态或阶段约束；
// 2. memory_context 只在这里注入一次，避免 pinned block 多入口重复出现；
// 3. 后缀由节点决定。对于 user-role 节点，通常把最终用户指令放在这里，保证消息末尾仍是用户输入。
func buildUnifiedMsg3(ctx *agentmodel.ConversationContext, config StageMessagesConfig) string {
	var sections []string

	if prefix := strings.TrimSpace(config.Msg3Prefix); prefix != "" {
		sections = append(sections, prefix)
	}
	if memoryText := renderUnifiedMemoryContext(ctx); memoryText != "" {
		sections = append(sections, "相关记忆（仅在确有帮助时参考，不要机械复述）：\n"+memoryText)
	}
	if suffix := strings.TrimSpace(config.Msg3Suffix); suffix != "" {
		sections = append(sections, suffix)
	}

	if len(sections) == 0 {
		return "请继续当前阶段。"
	}
	return strings.Join(sections, "\n\n")
}

// renderUnifiedMemoryContext 提取需要补充到 msg3 的记忆文本。
//
// 步骤化说明：
// 1. 只消费 memory_context，避免把 execution_context / current_step 等阶段专属块混回 prompt；
// 2. block 不存在或正文为空时直接返回空串；
// 3. 这里只读取 agent/sv 已经产出的最终文本，不在这里重新拼装记忆。
func renderUnifiedMemoryContext(ctx *agentmodel.ConversationContext) string {
	if ctx == nil {
		return ""
	}

	block, ok := ctx.PinnedBlockByKey("memory_context")
	if !ok {
		return ""
	}
	content := strings.TrimSpace(block.Content)
	if content == "" {
		return ""
	}
	return content
}

// CollectConversationTurns 从历史消息中提取 user + assistant speak 对话流。
//
// 提取规则：
// 1. 只保留 user 消息（排除 correction prompt）和 assistant 纯文本消息；
// 2. assistant tool_call 消息与 tool observation 消息不纳入“真实对话”；
// 3. 返回顺序保持与原始 history 一致。
func CollectConversationTurns(history []*schema.Message) []ConversationTurn {
	if len(history) == 0 {
		return nil
	}

	turns := make([]ConversationTurn, 0, len(history))
	for _, msg := range history {
		if msg == nil {
			continue
		}
		text := strings.TrimSpace(msg.Content)
		if text == "" {
			continue
		}
		switch msg.Role {
		case schema.User:
			// 1. 跳过后端注入的 correction prompt，避免把纠错文案误判为用户真实意图。
			if isExecuteCorrectionPrompt(msg) {
				continue
			}
			turns = append(turns, ConversationTurn{Role: "user", Content: text})
		case schema.Assistant:
			// 2. 跳过工具调用消息，只保留真正面向用户的 speak/答复。
			if len(msg.ToolCalls) > 0 {
				continue
			}
			turns = append(turns, ConversationTurn{Role: "assistant", Content: text})
		}
	}

	return turns
}
