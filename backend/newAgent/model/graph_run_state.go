package model

import (
	"context"
	"strings"
	"time"

	infrallm "github.com/LoveLosita/smartflow/backend/infra/llm"
	newagentstream "github.com/LoveLosita/smartflow/backend/newAgent/stream"
	newagenttools "github.com/LoveLosita/smartflow/backend/newAgent/tools"
	schedule "github.com/LoveLosita/smartflow/backend/newAgent/tools/schedule"
	"github.com/cloudwego/eino/schema"
)

// AgentGraphRequest 描述一次 agent graph 运行的请求级输入。
//
// 职责边界：
// 1. 这里只放"当前这次请求"天然携带的轻量数据，例如用户本轮输入；
// 2. 不负责承载可持久化流程状态，流程状态仍归 AgentRuntimeState；
// 3. 不负责承载 LLM / emitter / store 等依赖，这些统一放进 AgentGraphDeps。
type AgentGraphRequest struct {
	UserInput     string
	ConfirmAction string // "accept" / "reject" / ""，仅 confirm 恢复场景由前端传入
	// ResumeInteractionID 用于校验“本次恢复请求”是否命中了当前 pending 交互，避免旧卡片误恢复。
	ResumeInteractionID string
	AlwaysExecute       bool // true 时写工具跳过确认闸门直接执行，适合前端已展示预览、用户无需逐步确认的场景
}

// Normalize 统一清洗请求级输入中的字符串字段。
func (r *AgentGraphRequest) Normalize() {
	if r == nil {
		return
	}
	r.UserInput = strings.TrimSpace(r.UserInput)
	r.ConfirmAction = strings.TrimSpace(r.ConfirmAction)
	r.ResumeInteractionID = strings.TrimSpace(r.ResumeInteractionID)
}

// RoughBuildPlacement 是粗排算法返回的单条放置结果。
// 字段使用 DB 坐标系（week/dayOfWeek/section），由 RoughBuild 节点转换为 ScheduleState 的 day_index。
type RoughBuildPlacement struct {
	TaskItemID  int
	Week        int
	DayOfWeek   int
	SectionFrom int
	SectionTo   int
}

// RoughBuildFunc 是粗排算法的依赖注入签名。
// 由 service 层封装 HybridScheduleWithPlanMulti 后注入，newAgent 层不直接依赖外层 model。
type RoughBuildFunc func(ctx context.Context, userID int, taskClassIDs []int) ([]RoughBuildPlacement, error)

// WriteSchedulePreviewFunc 是排程预览写入的依赖注入签名。
// 由 service 层封装 cacheDAO 后注入，execute/deliver 节点可按需调用：
// 1. execute 写工具后可实时刷新，保障前端及时看到最新调整；
// 2. deliver 结束时再做最终覆盖写，保障收口状态一致。
type WriteSchedulePreviewFunc func(ctx context.Context, state *schedule.ScheduleState, userID int, conversationID string, taskClassIDs []int) error

// PersistVisibleMessageFunc 是 newAgent 主循环逐条持久化可见消息的回调签名。
//
// 职责边界：
// 1. 只处理真正对用户可见的 assistant speak，不处理工具结果或内部纠错提示；
// 2. 由节点在 AppendHistory 之后主动调用，让上层同步把这条消息写入 Redis + MySQL；
// 3. 执行方可以做无损降级（例如 Redis 写失败只记日志），但应返回 error 便于上层记录。
type PersistVisibleMessageFunc func(ctx context.Context, state *CommonState, msg *schema.Message) error

// AgentGraphDeps 描述 graph/node 层运行时真正依赖的可插拔能力。
//
// 设计目的：
// 1. 让 graph 不再只拿到"裸状态"，而是能拿到上下文、模型和输出能力；
// 2. Chat/Plan/Execute/Deliver 允许分别挂不同 client，但也允许先复用同一个 client；
// 3. ChunkEmitter 统一承接阶段提示、正文、工具事件、确认请求等 SSE 输出。
type AgentGraphDeps struct {
	ChatClient           *infrallm.Client
	PlanClient           *infrallm.Client
	ExecuteClient        *infrallm.Client
	DeliverClient        *infrallm.Client
	ChunkEmitter         *newagentstream.ChunkEmitter
	StateStore           AgentStateStore
	ToolRegistry         *newagenttools.ToolRegistry
	ScheduleProvider     ScheduleStateProvider    // 按 DAO 注入，Execute 节点按需加载 ScheduleState
	CompactionStore      CompactionStore          // 按 DAO 注入，用于 Execute 上下文压缩持久化
	RoughBuildFunc       RoughBuildFunc           // 按 Service 注入，粗排算法入口
	WriteSchedulePreview WriteSchedulePreviewFunc // 按 Service 注入，排程预览写入入口

	// thinking 开关：由 config.yaml 的 agent.thinking 段注入，各节点按需读取。
	ThinkingPlan    bool
	ThinkingExecute bool
	ThinkingDeliver bool

	// 记忆预取管线：由 service 层启动的后台检索 goroutine 写入。
	// channel 携带已渲染的文本内容（非原始 ItemDTO），节点直接写入 pinned block。
	MemoryFuture   chan string // buffered(1)，携带 renderMemoryPinnedContentByMode 的输出
	MemoryConsumed bool        // 保证 channel 只读一次，后续 Execute ReAct 循环跳过等待

	// PersistVisibleMessage 按 Service 注入，newAgent 每个节点产出的可见 speak
	// 都会在 AppendHistory 之后立刻调用这个回调，把消息同步落到 Redis + MySQL。
	PersistVisibleMessage PersistVisibleMessageFunc
}

// --- 记忆 pinned block 常量（供 agentsvc 和 node 层共享） ---

const (
	// MemoryContextBlockKey 记忆上下文在 ConversationContext PinnedBlock 中的唯一 key。
	MemoryContextBlockKey = "memory_context"
	// MemoryContextBlockTitle 记忆上下文 pinned block 的标题，用于 prompt 渲染。
	MemoryContextBlockTitle = "相关记忆"
	// MemoryFreshTimeout 是 Execute/Plan 节点等待后台记忆检索完成的最大时长。
	MemoryFreshTimeout = 500 * time.Millisecond
)

// EnsureChunkEmitter 保证 graph 运行时始终有一个可用的 chunk 发射器。
//
// 步骤说明：
// 1. 依赖为空时回退到 Noop emitter，避免骨架期因为没接前端而到处判空；
// 2. 这里只兜底"能安全调用"，不负责填充真实 request_id / model_name；
// 3. 后续 service 层一旦接上真实 emitter，会自然覆盖这里的空实现。
func (d *AgentGraphDeps) EnsureChunkEmitter() *newagentstream.ChunkEmitter {
	if d == nil {
		return newagentstream.NewChunkEmitter(newagentstream.NoopPayloadEmitter(), "", "", 0)
	}
	if d.ChunkEmitter == nil {
		d.ChunkEmitter = newagentstream.NewChunkEmitter(newagentstream.NoopPayloadEmitter(), "", "", 0)
	}
	return d.ChunkEmitter
}

// ResolveChatClient 返回 chat 阶段可用的模型客户端。
func (d *AgentGraphDeps) ResolveChatClient() *infrallm.Client {
	if d == nil {
		return nil
	}
	return d.ChatClient
}

// ResolvePlanClient 返回 planning 阶段可用的模型客户端。
//
// 兜底策略：
// 1. 优先使用显式注入的 PlanClient；
// 2. 若未单独注入，则回退到 ChatClient；
// 3. 这样在骨架期可先用一套 client 跑通，再按需拆分 strategist / worker。
func (d *AgentGraphDeps) ResolvePlanClient() *infrallm.Client {
	if d == nil {
		return nil
	}
	if d.PlanClient != nil {
		return d.PlanClient
	}
	return d.ChatClient
}

// ResolveExecuteClient 返回 execute 阶段可用的模型客户端。
func (d *AgentGraphDeps) ResolveExecuteClient() *infrallm.Client {
	if d == nil {
		return nil
	}
	if d.ExecuteClient != nil {
		return d.ExecuteClient
	}
	if d.PlanClient != nil {
		return d.PlanClient
	}
	return d.ChatClient
}

// ResolveDeliverClient 返回 deliver 阶段可用的模型客户端。
func (d *AgentGraphDeps) ResolveDeliverClient() *infrallm.Client {
	if d == nil {
		return nil
	}
	if d.DeliverClient != nil {
		return d.DeliverClient
	}
	if d.ExecuteClient != nil {
		return d.ExecuteClient
	}
	if d.PlanClient != nil {
		return d.PlanClient
	}
	return d.ChatClient
}

// AgentGraphRunInput 是执行 newAgent 通用 graph 所需的完整入口参数。
//
// 字段说明：
// 1. RuntimeState：可持久化流程状态与 pending interaction；
// 2. ConversationContext：本轮喂给模型的上下文材料；
// 3. Request：当前这次请求的轻量输入；
// 4. Deps：graph/node 层真正依赖的可插拔能力。
type AgentGraphRunInput struct {
	RuntimeState          *AgentRuntimeState
	ConversationContext   *ConversationContext
	ScheduleState         *schedule.ScheduleState
	OriginalScheduleState *schedule.ScheduleState
	Request               AgentGraphRequest
	Deps                  AgentGraphDeps
}

// AgentGraphState 是 graph 内部真正流转的运行态容器。
//
// 职责边界：
// 1. 负责把"流程状态 + 对话上下文 + 请求输入 + 运行依赖"收口到同一个对象；
// 2. 负责给 graph 分支和 node 提供最小必要的兜底访问方法；
// 3. 不负责持久化，不负责真正业务执行。
type AgentGraphState struct {
	RuntimeState          *AgentRuntimeState
	ConversationContext   *ConversationContext
	Request               AgentGraphRequest
	Deps                  AgentGraphDeps
	ScheduleState         *schedule.ScheduleState // 工具操作的内存数据源，Execute 节点按需加载
	OriginalScheduleState *schedule.ScheduleState // 首次加载时的原始快照，供 diff 用
}

// NewAgentGraphState 把入口参数整理成 graph 内部状态。
func NewAgentGraphState(input AgentGraphRunInput) *AgentGraphState {
	st := &AgentGraphState{
		RuntimeState:          input.RuntimeState,
		ConversationContext:   input.ConversationContext,
		Request:               input.Request,
		Deps:                  input.Deps,
		ScheduleState:         input.ScheduleState,
		OriginalScheduleState: input.OriginalScheduleState,
	}
	st.Request.Normalize()
	st.EnsureRuntimeState()
	st.EnsureConversationContext()
	st.Deps.EnsureChunkEmitter()
	return st
}

// EnsureRuntimeState 保证 graph 内部始终持有一份可用的运行态。
func (s *AgentGraphState) EnsureRuntimeState() *AgentRuntimeState {
	if s == nil {
		return nil
	}
	if s.RuntimeState == nil {
		s.RuntimeState = NewAgentRuntimeState(nil)
	}
	s.RuntimeState.EnsureCommonState()
	return s.RuntimeState
}

// EnsureFlowState 返回可持久化的主流程状态。
func (s *AgentGraphState) EnsureFlowState() *CommonState {
	runtimeState := s.EnsureRuntimeState()
	if runtimeState == nil {
		return nil
	}
	return runtimeState.EnsureCommonState()
}

// EnsureConversationContext 保证 graph 内部始终持有一份可用的会话上下文。
func (s *AgentGraphState) EnsureConversationContext() *ConversationContext {
	if s == nil {
		return nil
	}
	if s.ConversationContext == nil {
		s.ConversationContext = NewConversationContext("")
	}
	return s.ConversationContext
}

// EnsureChunkEmitter 返回 graph 可安全调用的 chunk 发射器。
func (s *AgentGraphState) EnsureChunkEmitter() *newagentstream.ChunkEmitter {
	if s == nil {
		return newagentstream.NewChunkEmitter(newagentstream.NoopPayloadEmitter(), "", "", 0)
	}
	return s.Deps.EnsureChunkEmitter()
}

// ResolveToolRegistry 返回可用的工具注册表。
func (s *AgentGraphState) ResolveToolRegistry() *newagenttools.ToolRegistry {
	if s == nil {
		return nil
	}
	return s.Deps.ToolRegistry
}

// EnsureScheduleState 确保 ScheduleState 已加载。
// 首次调用时通过 ScheduleProvider 从 DB 加载，后续复用内存中的 state。
func (s *AgentGraphState) EnsureScheduleState(ctx context.Context) (*schedule.ScheduleState, error) {
	if s == nil {
		return nil, nil
	}
	flowState := s.EnsureFlowState()
	if s.ScheduleState != nil {
		if s.OriginalScheduleState == nil {
			// 1. 兼容老快照：历史 Redis 快照里可能还没带 original_state。
			// 2. 当前阶段虽然已经不落库，但后续若重新接回 diff 链，仍需要稳定的原始快照。
			// 3. 因此这里在"已恢复出 ScheduleState、但缺 original"时补一份克隆兜底。
			s.OriginalScheduleState = s.ScheduleState.Clone()
		}
		schedule.FilterScheduleStateForTaskClassScope(s.ScheduleState, flowState.TaskClassIDs)
		schedule.FilterScheduleStateForTaskClassScope(s.OriginalScheduleState, flowState.TaskClassIDs)
		return s.ScheduleState, nil
	}
	if s.Deps.ScheduleProvider == nil {
		return nil, nil
	}
	userID := flowState.UserID
	var (
		state *schedule.ScheduleState
		err   error
	)
	// 1. 若 provider 支持按 task_class_ids 精确加载，则优先走 scoped 入口。
	// 2. 这样可以让 DayMapping 与粗排算法使用同一批任务类窗口，避免"全量任务类脏日期污染本轮窗口"。
	// 3. 若当前实现尚未支持 scoped 加载，则回退到旧入口，并继续复用后面的 scope 裁剪。
	if scopedProvider, ok := s.Deps.ScheduleProvider.(ScopedScheduleStateProvider); ok && len(flowState.TaskClassIDs) > 0 {
		state, err = scopedProvider.LoadScheduleStateForTaskClasses(ctx, userID, flowState.TaskClassIDs)
	} else {
		state, err = s.Deps.ScheduleProvider.LoadScheduleState(ctx, userID)
	}
	if err != nil {
		return nil, err
	}
	s.ScheduleState = state
	// 保存原始快照，供后续 diff 使用。
	s.OriginalScheduleState = state.Clone()
	schedule.FilterScheduleStateForTaskClassScope(s.ScheduleState, flowState.TaskClassIDs)
	schedule.FilterScheduleStateForTaskClassScope(s.OriginalScheduleState, flowState.TaskClassIDs)
	return state, nil
}
