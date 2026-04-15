package newagentnode

import (
	"context"
	"errors"
	"fmt"
	"log"

	newagentmodel "github.com/LoveLosita/smartflow/backend/newAgent/model"
	"github.com/LoveLosita/smartflow/backend/newAgent/tools/schedule"
)

// AgentNodes 是 newAgent 通用图的节点容器。
//
// 职责边界：
// 1. 负责把 node 层真正实现的方法统一暴露给 graph 注册；
// 2. 负责收口"graph 只编排、node 真执行"的结构约束；
// 3. 负责在每个节点执行成功后统一做状态持久化（Save/Delete）。
type AgentNodes struct{}

// NewAgentNodes 创建通用节点容器。
func NewAgentNodes() *AgentNodes {
	return &AgentNodes{}
}

// Chat 是聊天入口的正式节点方法。
//
// 职责边界：
// 1. 这里只做 graph -> node 的参数转接；
// 2. 真正的入口逻辑仍由 RunChatNode 负责；
// 3. Chat 的 Save 交给 Service 层处理，这里不做持久化。
func (n *AgentNodes) Chat(ctx context.Context, st *newagentmodel.AgentGraphState) (*newagentmodel.AgentGraphState, error) {
	if st == nil {
		return nil, errors.New("chat node: state is nil")
	}

	// 注入工具 schema 到 ConversationContext，让路由决策更智能。
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

	if err := RunChatNode(
		ctx,
		ChatNodeInput{
			RuntimeState:        st.EnsureRuntimeState(),
			ConversationContext: st.EnsureConversationContext(),
			UserInput:           st.Request.UserInput,
			ConfirmAction:       st.Request.ConfirmAction,
			Client:              st.Deps.ResolveChatClient(),
			ChunkEmitter:        st.EnsureChunkEmitter(),
		},
	); err != nil {
		return nil, err
	}
	return st, nil
}

// Confirm 是确认阶段的正式节点方法。
//
// 职责边界：
// 1. 这里只做 graph -> node 的参数转接；
// 2. 真正的确认逻辑仍由 RunConfirmNode 负责；
// 3. 不需要 LLM Client — 确认内容由已有状态机械格式化。
// 4. Confirm 执行成功后保存状态，因为它创建了 PendingInteraction。
func (n *AgentNodes) Confirm(ctx context.Context, st *newagentmodel.AgentGraphState) (*newagentmodel.AgentGraphState, error) {
	if st == nil {
		return nil, errors.New("confirm node: state is nil")
	}

	if err := RunConfirmNode(
		ctx,
		ConfirmNodeInput{
			RuntimeState:        st.EnsureRuntimeState(),
			ConversationContext: st.EnsureConversationContext(),
			ChunkEmitter:        st.EnsureChunkEmitter(),
		},
	); err != nil {
		return nil, err
	} else if st.Deps.WriteSchedulePreview != nil && st.ScheduleState == nil {
		flowState := st.EnsureFlowState()
		log.Printf("[WARN] deliver: schedule state is nil, skip preview write chat=%s", flowState.ConversationID)
	}

	saveAgentState(ctx, st)
	return st, nil
}

// Plan 是规划阶段的正式节点方法。
//
// 职责边界：
// 1. 这里只做 graph -> node 的参数转接；
// 2. 真正的单轮规划逻辑仍由 RunPlanNode 负责；
// 3. Plan 执行成功后保存状态，支持意外断线恢复。
func (n *AgentNodes) Plan(ctx context.Context, st *newagentmodel.AgentGraphState) (*newagentmodel.AgentGraphState, error) {
	if st == nil {
		return nil, errors.New("plan node: state is nil")
	}

	if err := RunPlanNode(
		ctx,
		PlanNodeInput{
			RuntimeState:        st.EnsureRuntimeState(),
			ConversationContext: st.EnsureConversationContext(),
			UserInput:           st.Request.UserInput,
			Client:              st.Deps.ResolvePlanClient(),
			ChunkEmitter:        st.EnsureChunkEmitter(),
			ResumeNode:          "plan",
			AlwaysExecute:       st.Request.AlwaysExecute,
		},
	); err != nil {
		return nil, err
	}

	saveAgentState(ctx, st)
	return st, nil
}

// RoughBuild 是粗排阶段的正式节点方法。
//
// 职责边界：
// 1. 调用注入的 RoughBuildFunc 执行粗排算法；
// 2. 把粗排结果写入 ScheduleState；
// 3. 完成后保存状态，支持意外断线恢复。
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

// Interrupt 是中断阶段的正式节点方法。
//
// 职责边界：
// 1. 这里只做 graph -> node 的参数转接；
// 2. 真正的中断逻辑仍由 RunInterruptNode 负责；
// 3. 不需要 LLM Client — 所有文本已在 PendingInteraction 里。
// 4. 不需要 Save — 上游节点（Plan/Execute/Confirm）已经存过了。
func (n *AgentNodes) Interrupt(ctx context.Context, st *newagentmodel.AgentGraphState) (*newagentmodel.AgentGraphState, error) {
	if st == nil {
		return nil, errors.New("interrupt node: state is nil")
	}

	if err := RunInterruptNode(
		ctx,
		InterruptNodeInput{
			RuntimeState:        st.EnsureRuntimeState(),
			ConversationContext: st.EnsureConversationContext(),
			ChunkEmitter:        st.EnsureChunkEmitter(),
		},
	); err != nil {
		return nil, err
	}
	return st, nil
}

// Execute 是执行阶段的正式节点方法。
//
// 职责边界：
// 1. 这里只做 graph -> node 的参数转接；
// 2. 真正的单轮执行逻辑仍由 RunExecuteNode 负责。
//
// 设计原则：
// 1. LLM 主导：LLM 自己判断 done_when 是否满足，自己决定何时推进/完成；
// 2. 后端兜底：只做资源控制、安全兜底、证据记录；
// 3. 不做硬校验：后端不质疑 LLM 的 advance/complete 决策。
// 4. Execute 每轮执行成功后保存状态，支持意外断线恢复。
func (n *AgentNodes) Execute(ctx context.Context, st *newagentmodel.AgentGraphState) (*newagentmodel.AgentGraphState, error) {
	if st == nil {
		return nil, errors.New("execute node: state is nil")
	}

	// 按需加载 ScheduleState（首次执行时从 DB 加载，后续复用内存中的 state）。
	var scheduleState *schedule.ScheduleState
	if ss, loadErr := st.EnsureScheduleState(ctx); loadErr != nil {
		return nil, fmt.Errorf("execute node: 加载日程状态失败: %w", loadErr)
	} else if ss != nil {
		scheduleState = ss
	}

	// 注入工具 schema 到 ConversationContext，让 LLM 能看到可用工具列表。
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

	if err := RunExecuteNode(
		ctx,
		ExecuteNodeInput{
			RuntimeState:          st.EnsureRuntimeState(),
			ConversationContext:   st.EnsureConversationContext(),
			UserInput:             st.Request.UserInput,
			Client:                st.Deps.ResolveExecuteClient(),
			ChunkEmitter:          st.EnsureChunkEmitter(),
			ResumeNode:            "execute",
			ToolRegistry:          st.Deps.ToolRegistry,
			ScheduleState:         scheduleState,
			SchedulePersistor:     st.Deps.SchedulePersistor,
			CompactionStore:       st.Deps.CompactionStore,
			WriteSchedulePreview:  st.Deps.WriteSchedulePreview,
			OriginalScheduleState: st.OriginalScheduleState,
			AlwaysExecute:         st.Request.AlwaysExecute,
		},
	); err != nil {
		return nil, err
	}

	saveAgentState(ctx, st)
	return st, nil
}

// OrderGuard 是顺序守卫阶段的正式节点方法。
//
// 职责边界：
// 1. 只负责调用 RunOrderGuardNode 做 suggested 相对顺序校验；
// 2. 不负责交付文案生成，校验结果统一交给 Deliver 节点收口；
// 3. 节点执行后保存状态，保证异常中断后仍可复盘守卫结果。
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

// Deliver 是交付阶段的正式节点方法。
//
// 职责边界：
// 1. 这里只做 graph -> node 的参数转接；
// 2. 真正的交付逻辑仍由 RunDeliverNode 负责；
// 3. 调 LLM 生成任务总结，失败时降级到机械格式化。
// 4. 任务完成后保存最终状态到 Redis（2h TTL），支持断线恢复和 MySQL outbox 异步持久化。
func (n *AgentNodes) Deliver(ctx context.Context, st *newagentmodel.AgentGraphState) (*newagentmodel.AgentGraphState, error) {
	if st == nil {
		return nil, errors.New("deliver node: state is nil")
	}

	if err := RunDeliverNode(
		ctx,
		DeliverNodeInput{
			RuntimeState:        st.EnsureRuntimeState(),
			ConversationContext: st.EnsureConversationContext(),
			Client:              st.Deps.ResolveDeliverClient(),
			ChunkEmitter:        st.EnsureChunkEmitter(),
		},
	); err != nil {
		return nil, err
	}

	// 任务完成后写排程预览缓存：只有走到 Deliver 才代表排程结果已稳定，
	// 中断（confirm/ask_user）路径不写，避免把中间态暴露给前端。
	if st.Deps.WriteSchedulePreview != nil && st.ScheduleState != nil {
		flowState := st.EnsureFlowState()
		if flowState != nil && flowState.IsCompleted() {
			if err := st.Deps.WriteSchedulePreview(ctx, st.ScheduleState, flowState.UserID, flowState.ConversationID, flowState.TaskClassIDs); err != nil {
				// 写缓存失败不阻断主流程，降级为仅 log。
				log.Printf("[WARN] deliver: 写入排程预览缓存失败 chat=%s: %v", flowState.ConversationID, err)
			}
		} else if flowState != nil {
			log.Printf("[DEBUG] deliver: skip schedule preview chat=%s terminal_status=%s", flowState.ConversationID, flowState.TerminalStatus())
		}
	}

	saveAgentState(ctx, st)
	return st, nil
}

// --- 持久化辅助 ---

// saveAgentState 在节点执行成功后，将当前运行态快照保存到 Redis。
//
// 设计原则：
// 1. Save 失败只记日志，不中断 Graph 流程；
// 2. StateStore 为空时静默跳过（骨架期 / 测试环境）；
// 3. conversationID 为空时也静默跳过，避免写入无效 key。
//
// TODO: 接入项目统一的日志框架后，把 _ = err 改成结构化日志。
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

// deleteAgentState 在任务完成后，删除 Redis 中的运行态快照。
//
// 设计原则：
// 1. Delete 失败只记日志，不中断 Graph 流程；
// 2. 删除是幂等的，key 不存在也视为成功；
// 3. StateStore 为空时静默跳过。
//
// TODO: 接入项目统一的日志框架后，把 _ = err 改成结构化日志。
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
