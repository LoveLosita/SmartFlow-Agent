package model

import (
	"strings"

	"github.com/cloudwego/eino/schema"
)

// ConversationContext 承载“本轮要喂给模型的输入材料”。
//
// 职责边界：
// 1. 负责保存 system prompt、对话历史、置顶注入块、工具 schema 摘要；
// 2. 负责提供最小必要的安全访问方法，避免 node / prompt 层直接散落切片操作；
// 3. 不负责流程推进，phase / round / current step 仍归 CommonState 管；
// 4. 不负责真正的 prompt 组装，消息如何拼接仍应放在 prompt 层处理。
//
// TODO(newagent/prompt): 后续由 plan / execute 的 prompt builder 读取这里的数据，组装真正发给 LLM 的 messages。
// TODO(newagent/node): 后续 planNode / executeNode 只通过这里的访问方法读写上下文，避免多处直接改切片。
type ConversationContext struct {
	SystemPrompt string
	History      []*schema.Message
	PinnedBlocks []ContextBlock
	ToolSchemas  []ToolSchemaContext
}

// ContextBlock 表示一段可被“置顶注入”的自然语言上下文。
//
// 设计目的：
// 1. Key 用于让调用方按语义覆盖，例如 current_plan / current_step / execution_rule；
// 2. Title 用于 prompt 层后续决定是否渲染成小标题；
// 3. Content 存真正的自然语言内容，保持你当前“plan 用自然语言表达”的思路。
type ContextBlock struct {
	Key     string
	Title   string
	Content string
}

// ToolSchemaContext 是工具描述的轻量快照。
//
// 职责边界：
// 1. 这里只保留 prompt 注入真正需要的摘要信息；
// 2. SchemaText 约定存“已经整理好的自然语言 / JSON schema 摘要”；
// 3. 不直接耦合具体 tool registry 里的复杂结构，避免 model 层反向依赖工具实现。
type ToolSchemaContext struct {
	Name       string
	Desc       string
	SchemaText string
}

// NewConversationContext 创建最小上下文容器。
func NewConversationContext(systemPrompt string) *ConversationContext {
	return &ConversationContext{
		SystemPrompt: strings.TrimSpace(systemPrompt),
	}
}

// SetSystemPrompt 更新系统提示词。
func (c *ConversationContext) SetSystemPrompt(systemPrompt string) {
	if c == nil {
		return
	}
	c.SystemPrompt = strings.TrimSpace(systemPrompt)
}

// ReplaceHistory 整体替换对话历史。
//
// 职责边界：
// 1. 负责把“会话快照恢复”这类场景需要的一次性覆盖入口收口到这里；
// 2. 只复制消息切片本身，避免调用方后续 append 污染同一底层数组；
// 3. 不深拷贝每个 message 指针，消息对象本身仍默认由上游按只读方式使用。
func (c *ConversationContext) ReplaceHistory(history []*schema.Message) {
	if c == nil {
		return
	}
	c.History = cloneMessageSlice(history)
}

// AppendHistory 追加对话历史。
//
// 处理策略：
// 1. 跳过 nil message，避免后续 prompt 拼装时出现空指针；
// 2. 仅负责顺序追加，不做去重，不做裁剪；
// 3. 历史裁剪策略属于后续 prompt / memory 层能力，此处先不下沉。
func (c *ConversationContext) AppendHistory(messages ...*schema.Message) {
	if c == nil || len(messages) == 0 {
		return
	}
	for _, msg := range messages {
		if msg == nil {
			continue
		}
		c.History = append(c.History, msg)
	}
}

// HistorySnapshot 返回历史消息的浅拷贝切片。
func (c *ConversationContext) HistorySnapshot() []*schema.Message {
	if c == nil {
		return nil
	}
	return cloneMessageSlice(c.History)
}

// UpsertPinnedBlock 按 Key 写入或覆盖一段置顶上下文。
//
// 步骤说明：
// 1. Key 为空时直接忽略，因为后续无法做稳定覆盖；
// 2. 若已存在同 Key block，则原位覆盖，保证“当前 plan / 当前步骤”这类上下文始终只有一份；
// 3. 若不存在，则追加到末尾，至于渲染顺序由 prompt 层统一决定；
// 4. 此处不自动裁剪旧内容，避免 model 层擅自丢信息。
func (c *ConversationContext) UpsertPinnedBlock(block ContextBlock) {
	if c == nil {
		return
	}

	key := strings.TrimSpace(block.Key)
	if key == "" {
		return
	}
	block.Key = key
	block.Title = strings.TrimSpace(block.Title)
	block.Content = strings.TrimSpace(block.Content)

	for i := range c.PinnedBlocks {
		if c.PinnedBlocks[i].Key == key {
			c.PinnedBlocks[i] = block
			return
		}
	}
	c.PinnedBlocks = append(c.PinnedBlocks, block)
}

// RemovePinnedBlock 删除指定 Key 的置顶上下文。
func (c *ConversationContext) RemovePinnedBlock(key string) bool {
	if c == nil {
		return false
	}

	key = strings.TrimSpace(key)
	if key == "" {
		return false
	}

	for i := range c.PinnedBlocks {
		if c.PinnedBlocks[i].Key != key {
			continue
		}
		c.PinnedBlocks = append(c.PinnedBlocks[:i], c.PinnedBlocks[i+1:]...)
		return true
	}
	return false
}

// PinnedBlockByKey 按 Key 读取指定的置顶上下文。
func (c *ConversationContext) PinnedBlockByKey(key string) (ContextBlock, bool) {
	if c == nil {
		return ContextBlock{}, false
	}

	key = strings.TrimSpace(key)
	if key == "" {
		return ContextBlock{}, false
	}

	for i := range c.PinnedBlocks {
		if c.PinnedBlocks[i].Key == key {
			return c.PinnedBlocks[i], true
		}
	}
	return ContextBlock{}, false
}

// PinnedBlocksSnapshot 返回置顶上下文块的浅拷贝切片。
func (c *ConversationContext) PinnedBlocksSnapshot() []ContextBlock {
	if c == nil {
		return nil
	}
	result := make([]ContextBlock, len(c.PinnedBlocks))
	copy(result, c.PinnedBlocks)
	return result
}

// SetToolSchemas 整体替换工具 schema 摘要。
func (c *ConversationContext) SetToolSchemas(schemas []ToolSchemaContext) {
	if c == nil {
		return
	}
	c.ToolSchemas = cloneToolSchemaSlice(schemas)
}

// ToolSchemasSnapshot 返回工具 schema 摘要的浅拷贝切片。
func (c *ConversationContext) ToolSchemasSnapshot() []ToolSchemaContext {
	if c == nil {
		return nil
	}
	return cloneToolSchemaSlice(c.ToolSchemas)
}

func cloneMessageSlice(messages []*schema.Message) []*schema.Message {
	if len(messages) == 0 {
		return nil
	}
	result := make([]*schema.Message, len(messages))
	copy(result, messages)
	return result
}

func cloneToolSchemaSlice(schemas []ToolSchemaContext) []ToolSchemaContext {
	if len(schemas) == 0 {
		return nil
	}
	result := make([]ToolSchemaContext, len(schemas))
	copy(result, schemas)
	return result
}
