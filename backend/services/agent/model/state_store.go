package model

import (
	"context"

	schedule "github.com/LoveLosita/smartflow/backend/services/agent/tools/schedule"
)

// AgentStateSnapshot 是需要持久化的 agent 运行态最小快照。
//
// 设计说明：
// 1. 只保存恢复执行所需的 RuntimeState 和 ConversationContext；
// 2. 不保存 Request（每轮请求级，天然不跨连接）；
// 3. 不保存 Deps（依赖注入，每次由 Service 层重建）；
// 4. 不保存 ToolSchemas（每次请求由 Service 层重新注入）。
type AgentStateSnapshot struct {
	RuntimeState          *AgentRuntimeState      `json:"runtime_state"`
	ConversationContext   *ConversationContext    `json:"conversation_context"`
	ScheduleState         *schedule.ScheduleState `json:"schedule_state,omitempty"`
	OriginalScheduleState *schedule.ScheduleState `json:"original_schedule_state,omitempty"`
}

// AgentStateStore 定义 agent 状态持久化的最小接口。
//
// 职责边界：
// 1. 只负责"存 / 取 / 删"三个原子操作；
// 2. 不负责序列化细节（由实现层决定 JSON / protobuf）；
// 3. 不负责业务级状态校验，校验仍在 node / graph 层完成。
//
// 实现层：
// 1. dao/cache.go 上的 CacheDAO 隐式实现该接口（Go duck typing）；
// 2. agent 包不直接 import dao，由 Service 层在组装 Deps 时注入。
type AgentStateStore interface {
	// Save 序列化并保存一份 agent 状态快照。
	//
	// 语义：
	// 1. 同一 conversationID 被覆盖写入，保证 Redis 里始终只有最新快照；
	// 2. 实现层应设 TTL，避免已完成的任务快照永不清理。
	Save(ctx context.Context, conversationID string, snapshot *AgentStateSnapshot) error

	// Load 读取并反序列化 agent 状态快照。
	//
	// 返回值语义：
	// 1. (snapshot, true, nil)：命中快照，正常返回；
	// 2. (nil, false, nil)：未命中，不是错误，调用方应走新建对话路径；
	// 3. (nil, false, error)：真正的存储层错误。
	Load(ctx context.Context, conversationID string) (*AgentStateSnapshot, bool, error)

	// Delete 删除指定会话的 agent 状态快照。
	//
	// 语义：
	// 1. 删除是幂等的，key 不存在也视为成功；
	// 2. 典型调用时机：Deliver 节点任务完成后清理。
	Delete(ctx context.Context, conversationID string) error
}

// ScheduleStateProvider 定义加载 ScheduleState 的接口。
// 由 DAO 层或 Service 层实现，注入到 AgentGraphDeps 中。
// 使用接口而非具体 DAO 类型，避免 model → dao 的循环依赖。
type ScheduleStateProvider interface {
	LoadScheduleState(ctx context.Context, userID int) (*schedule.ScheduleState, error)
	// LoadTaskClassMetas 只加载指定任务类的约束元数据，供 Plan 节点提前消费。
	LoadTaskClassMetas(ctx context.Context, userID int, taskClassIDs []int) ([]schedule.TaskClassMeta, error)
}

// ScopedScheduleStateProvider 定义“按本轮任务类范围加载 ScheduleState”的可选增强接口。
//
// 设计说明：
// 1. 负责：在 Execute / RoughBuild 首次加载状态时，把 DayMapping、TaskClasses 与 pending 任务限定在本轮 task_class_ids 相关窗口；
// 2. 不负责：改变既有 ScheduleStateProvider 的基础能力，老实现仍可只实现 LoadScheduleState；
// 3. 兜底策略：若调用方拿到的 provider 不实现该接口，则回退到全量 LoadScheduleState，再走工具层 scope 裁剪。
type ScopedScheduleStateProvider interface {
	LoadScheduleStateForTaskClasses(ctx context.Context, userID int, taskClassIDs []int) (*schedule.ScheduleState, error)
}

// CompactionStore 定义上下文压缩的持久化接口。
// 由 Service 层实现（组合 DAO + Redis Cache），注入到各阶段 NodeInput。
type CompactionStore interface {
	LoadCompaction(ctx context.Context, userID int, chatID string) (summary string, watermark int, err error)
	SaveCompaction(ctx context.Context, userID int, chatID string, summary string, watermark int) error
	SaveContextTokenStats(ctx context.Context, userID int, chatID string, statsJSON string) error

	// LoadStageCompaction 按 stageKey 加载压缩摘要和水位线。
	// stageKey 区分不同节点（如 "execute"/"plan"/"chat"/"deliver"），
	// 使各节点可以独立维护各自的压缩状态，互不覆盖。
	LoadStageCompaction(ctx context.Context, userID int, chatID string, stageKey string) (summary string, watermark int, err error)
	// SaveStageCompaction 按 stageKey 保存压缩摘要和水位线。
	SaveStageCompaction(ctx context.Context, userID int, chatID string, stageKey string, summary string, watermark int) error
}
