package newagentnode

import (
	"context"
	"errors"
	"fmt"
	"log"
	"strings"
	"time"

	newagentmodel "github.com/LoveLosita/smartflow/backend/newAgent/model"
	"github.com/LoveLosita/smartflow/backend/newAgent/tools/schedule"
)

// AgentNodes 负责把 graph 层的节点调用统一转成 node 层真正的执行入口。
//
// 职责边界：
// 1. 这里只做参数转发、依赖注入和状态落盘，不承载业务决策。
// 2. 各节点真正的执行逻辑仍在对应的 RunXXXNode 内。
// 3. 节点成功后统一保存快照，方便断线恢复。
type AgentNodes struct{}

// NewAgentNodes 创建通用节点容器。
func NewAgentNodes() *AgentNodes {
	return &AgentNodes{}
}

// Chat 负责把 graph 的 chat 节点请求转给 RunChatNode。
func (n *AgentNodes) Chat(ctx context.Context, st *newagentmodel.AgentGraphState) (*newagentmodel.AgentGraphState, error) {
	if st == nil {
		return nil, errors.New("chat node: state is nil")
	}

	// 1. Chat 阶段只负责路由与纯对话，不需要看到工具目录，避免能力细节干扰判断。
	st.EnsureConversationContext().SetToolSchemas(nil)

	if err := RunChatNode(ctx, ChatNodeInput{
		RuntimeState:          st.EnsureRuntimeState(),
		ConversationContext:   st.EnsureConversationContext(),
		UserInput:             st.Request.UserInput,
		ConfirmAction:         st.Request.ConfirmAction,
		ResumeInteractionID:   st.Request.ResumeInteractionID,
		Client:                st.Deps.ResolveChatClient(),
		ChunkEmitter:          st.EnsureChunkEmitter(),
		CompactionStore:       st.Deps.CompactionStore,
		PersistVisibleMessage: st.Deps.PersistVisibleMessage,
	}); err != nil {
		return nil, err
	}

	saveAgentState(ctx, st)
	return st, nil
}

// Confirm 负责把 graph 的 confirm 节点请求转给 RunConfirmNode。
func (n *AgentNodes) Confirm(ctx context.Context, st *newagentmodel.AgentGraphState) (*newagentmodel.AgentGraphState, error) {
	if st == nil {
		return nil, errors.New("confirm node: state is nil")
	}

	if err := RunConfirmNode(ctx, ConfirmNodeInput{
		RuntimeState:        st.EnsureRuntimeState(),
		ConversationContext: st.EnsureConversationContext(),
		ChunkEmitter:        st.EnsureChunkEmitter(),
	}); err != nil {
		return nil, err
	}

	saveAgentState(ctx, st)
	return st, nil
}

// Plan 负责把 graph 的 plan 节点请求转给 RunPlanNode。
func (n *AgentNodes) Plan(ctx context.Context, st *newagentmodel.AgentGraphState) (*newagentmodel.AgentGraphState, error) {
	if st == nil {
		return nil, errors.New("plan node: state is nil")
	}

	// 等待后端记忆检索完成，再把最新结果注入上下文。
	ensureFreshMemory(st)

	if err := RunPlanNode(ctx, PlanNodeInput{
		RuntimeState:          st.EnsureRuntimeState(),
		ConversationContext:   st.EnsureConversationContext(),
		UserInput:             st.Request.UserInput,
		Client:                st.Deps.ResolvePlanClient(),
		ChunkEmitter:          st.EnsureChunkEmitter(),
		ResumeNode:            "plan",
		AlwaysExecute:         st.Request.AlwaysExecute,
		ThinkingEnabled:       st.Deps.ThinkingPlan,
		CompactionStore:       st.Deps.CompactionStore,
		PersistVisibleMessage: st.Deps.PersistVisibleMessage,
	}); err != nil {
		return nil, err
	}

	saveAgentState(ctx, st)
	return st, nil
}

// RoughBuild 负责把 graph 的 rough_build 节点请求转给 RunRoughBuildNode。
func (n *AgentNodes) RoughBuild(ctx context.Context, st *newagentmodel.AgentGraphState) (*newagentmodel.AgentGraphState, error) {
	if st == nil {
		return nil, errors.New("rough_build node: state is nil")
	}

	if err := RunRoughBuildNode(ctx, st); err != nil {
		return nil, err
	}

	saveAgentState(ctx, st)
	return st, nil
}

// Interrupt 负责把 graph 的 interrupt 节点请求转给 RunInterruptNode。
func (n *AgentNodes) Interrupt(ctx context.Context, st *newagentmodel.AgentGraphState) (*newagentmodel.AgentGraphState, error) {
	if st == nil {
		return nil, errors.New("interrupt node: state is nil")
	}

	if err := RunInterruptNode(ctx, InterruptNodeInput{
		RuntimeState:          st.EnsureRuntimeState(),
		ConversationContext:   st.EnsureConversationContext(),
		ChunkEmitter:          st.EnsureChunkEmitter(),
		PersistVisibleMessage: st.Deps.PersistVisibleMessage,
	}); err != nil {
		return nil, err
	}

	return st, nil
}

// Execute 负责把 graph 的 execute 节点请求转给 RunExecuteNode。
func (n *AgentNodes) Execute(ctx context.Context, st *newagentmodel.AgentGraphState) (*newagentmodel.AgentGraphState, error) {
	if st == nil {
		return nil, errors.New("execute node: state is nil")
	}

	// 1. 首次进入时按需加载日程状态，后续轮次复用内存状态。
	var scheduleState *schedule.ScheduleState
	if ss, loadErr := st.EnsureScheduleState(ctx); loadErr != nil {
		return nil, fmt.Errorf("execute node: 加载日程状态失败: %w", loadErr)
	} else if ss != nil {
		scheduleState = ss
	}

	// 2. 把工具 schema 注入上下文，供 LLM 看到真实工具边界。
	if st.Deps.ToolRegistry != nil {
		schemas := st.Deps.ToolRegistry.Schemas()
		toolSchemas := make([]newagentmodel.ToolSchemaContext, len(schemas))
		for i, s := range schemas {
			toolSchemas[i] = newagentmodel.ToolSchemaContext{
				Name:       s.Name,
				Desc:       s.Desc,
				SchemaText: s.SchemaText,
			}
		}
		st.EnsureConversationContext().SetToolSchemas(toolSchemas)
	}

	// 3. 等待后端记忆检索结果，再把最新结果注入上下文。
	ensureFreshMemory(st)

	if err := RunExecuteNode(ctx, ExecuteNodeInput{
		RuntimeState:          st.EnsureRuntimeState(),
		ConversationContext:   st.EnsureConversationContext(),
		UserInput:             st.Request.UserInput,
		Client:                st.Deps.ResolveExecuteClient(),
		ChunkEmitter:          st.EnsureChunkEmitter(),
		ResumeNode:            "execute",
		ToolRegistry:          st.Deps.ToolRegistry,
		ScheduleState:         scheduleState,
		CompactionStore:       st.Deps.CompactionStore,
		WriteSchedulePreview:  st.Deps.WriteSchedulePreview,
		OriginalScheduleState: st.OriginalScheduleState,
		AlwaysExecute:         st.Request.AlwaysExecute,
		ThinkingEnabled:       st.Deps.ThinkingExecute,
		PersistVisibleMessage: st.Deps.PersistVisibleMessage,
	}); err != nil {
		return nil, err
	}

	saveAgentState(ctx, st)
	return st, nil
}

// OrderGuard 负责把 graph 的 order_guard 节点请求转给 RunOrderGuardNode。
func (n *AgentNodes) OrderGuard(ctx context.Context, st *newagentmodel.AgentGraphState) (*newagentmodel.AgentGraphState, error) {
	if st == nil {
		return nil, errors.New("order_guard node: state is nil")
	}

	if err := RunOrderGuardNode(ctx, st); err != nil {
		return nil, err
	}

	saveAgentState(ctx, st)
	return st, nil
}

// QuickTask 负责把 graph 的 quick_task 节点请求转给 RunQuickTaskNode。
func (n *AgentNodes) QuickTask(ctx context.Context, st *newagentmodel.AgentGraphState) (*newagentmodel.AgentGraphState, error) {
	if st == nil {
		return nil, errors.New("quick_task node: state is nil")
	}

	// QuickTask 不需要工具目录，直接复用 ChatClient。
	st.EnsureConversationContext().SetToolSchemas(nil)

	if err := RunQuickTaskNode(ctx, QuickTaskNodeInput{
		RuntimeState:          st.EnsureRuntimeState(),
		ConversationContext:   st.EnsureConversationContext(),
		UserInput:             st.Request.UserInput,
		Client:                st.Deps.ResolveChatClient(),
		ChunkEmitter:          st.EnsureChunkEmitter(),
		QuickTaskDeps:         st.Deps.QuickTaskDeps,
		PersistVisibleMessage: st.Deps.PersistVisibleMessage,
	}); err != nil {
		return nil, err
	}

	saveAgentState(ctx, st)
	return st, nil
}

// Deliver 负责把 graph 的 deliver 节点请求转给 RunDeliverNode。
func (n *AgentNodes) Deliver(ctx context.Context, st *newagentmodel.AgentGraphState) (*newagentmodel.AgentGraphState, error) {
	if st == nil {
		return nil, errors.New("deliver node: state is nil")
	}

	// 1. Deliver 只做最终收口总结，不需要工具目录，避免无关能力信息污染总结。
	st.EnsureConversationContext().SetToolSchemas(nil)

	if err := RunDeliverNode(ctx, DeliverNodeInput{
		RuntimeState:          st.EnsureRuntimeState(),
		ConversationContext:   st.EnsureConversationContext(),
		Client:                st.Deps.ResolveDeliverClient(),
		ChunkEmitter:          st.EnsureChunkEmitter(),
		ThinkingEnabled:       st.Deps.ThinkingDeliver,
		CompactionStore:       st.Deps.CompactionStore,
		PersistVisibleMessage: st.Deps.PersistVisibleMessage,
	}); err != nil {
		return nil, err
	}

	// 只有真正完成时才写入排程预览，避免中间态污染前端展示。
	if st.Deps.WriteSchedulePreview != nil && st.ScheduleState != nil {
		flowState := st.EnsureFlowState()
		if flowState != nil && flowState.IsCompleted() {
			if err := st.Deps.WriteSchedulePreview(ctx, st.ScheduleState, flowState.UserID, flowState.ConversationID, flowState.TaskClassIDs); err != nil {
				log.Printf("[WARN] deliver: 写入排程预览缓存失败 chat=%s: %v", flowState.ConversationID, err)
			}
		} else if flowState != nil {
			log.Printf("[DEBUG] deliver: skip schedule preview chat=%s terminal_status=%s", flowState.ConversationID, flowState.TerminalStatus())
		}
	}

	saveAgentState(ctx, st)
	return st, nil
}

// ensureFreshMemory 等待后端记忆检索完成，并把最新结果写入 ConversationContext。
//
// 1. 只在首次调用时等待 channel，后续调用直接跳过。
// 2. 超时后保留原有上下文，不额外覆盖。
// 3. 记忆为空时也不做额外写入，避免污染 prompt。
func ensureFreshMemory(st *newagentmodel.AgentGraphState) {
	if st == nil || st.Deps.MemoryConsumed || st.Deps.MemoryFuture == nil {
		return
	}
	st.Deps.MemoryConsumed = true

	select {
	case content := <-st.Deps.MemoryFuture:
		if strings.TrimSpace(content) != "" {
			st.EnsureConversationContext().UpsertPinnedBlock(newagentmodel.ContextBlock{
				Key:     newagentmodel.MemoryContextBlockKey,
				Title:   newagentmodel.MemoryContextBlockTitle,
				Content: content,
			})
		}
	case <-time.After(newagentmodel.MemoryFreshTimeout):
		// 超时后保留原有上下文即可。
	}
}

// saveAgentState 在节点成功执行后保存运行快照。
func saveAgentState(ctx context.Context, st *newagentmodel.AgentGraphState) {
	if st == nil {
		return
	}
	store := st.Deps.StateStore
	if store == nil {
		return
	}

	runtimeState := st.EnsureRuntimeState()
	if runtimeState == nil {
		return
	}

	flowState := runtimeState.EnsureCommonState()
	if flowState == nil || flowState.ConversationID == "" {
		return
	}

	snapshot := &newagentmodel.AgentStateSnapshot{
		RuntimeState:          runtimeState,
		ConversationContext:   st.EnsureConversationContext(),
		ScheduleState:         st.ScheduleState.Clone(),
		OriginalScheduleState: st.OriginalScheduleState.Clone(),
	}

	_ = store.Save(ctx, flowState.ConversationID, snapshot)
}

// deleteAgentState 在任务完成后删除运行快照。
func deleteAgentState(ctx context.Context, st *newagentmodel.AgentGraphState) {
	if st == nil {
		return
	}
	store := st.Deps.StateStore
	if store == nil {
		return
	}

	runtimeState := st.EnsureRuntimeState()
	if runtimeState == nil {
		return
	}

	flowState := runtimeState.EnsureCommonState()
	if flowState == nil || flowState.ConversationID == "" {
		return
	}

	_ = store.Delete(ctx, flowState.ConversationID)
}
