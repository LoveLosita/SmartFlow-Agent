package model

import "time"

const (
	// SchedulePlanStateVersionV1 表示当前 schedule_plan 快照结构版本。
	//
	// 设计说明：
	// 1. 当后续快照字段发生不兼容变更时，版本号用于区分反序列化逻辑；
	// 2. 当前版本先固定为 1，后续升级时由写入端递增；
	// 3. 读取端可依据版本做兼容兜底，避免历史快照直接失效。
	SchedulePlanStateVersionV1 = 1
)

// AgentScheduleState 是“单用户单会话”的智能排程状态快照持久化模型。
//
// 职责边界：
// 1. 负责保存“可恢复的排程中间状态与最终预览”，用于连续对话微调承接；
// 2. 负责承载结构化 JSON 快照（任务类、混合条目、候选方案等）；
// 3. 不负责正式日程落库（正式落库仍走你现有的确认/应用链路）；
// 4. 不负责消息总线投递（该快照要求强实时可读，直接写 MySQL）。
type AgentScheduleState struct {
	ID int64 `gorm:"column:id;primaryKey;autoIncrement"`

	// 1. 一对话一状态：同 user_id + conversation_id 永远只保留最新快照。
	// 2. revision 在 upsert 更新时自增，便于排查“同会话被覆盖了几次”。
	UserID         int    `gorm:"column:user_id;not null;uniqueIndex:uk_schedule_state_user_conv,priority:1;index:idx_schedule_state_user_updated,priority:1"`
	ConversationID string `gorm:"column:conversation_id;type:varchar(36);not null;uniqueIndex:uk_schedule_state_user_conv,priority:2"`
	Revision       int    `gorm:"column:revision;not null;default:1"`
	StateVersion   int    `gorm:"column:state_version;not null;default:1"`

	// 3. 为了避免跨层结构体强耦合，复杂切片统一序列化为 JSON 字符串存储。
	TaskClassIDsJSON   string `gorm:"column:task_class_ids;type:json;not null"`
	ConstraintsJSON    string `gorm:"column:constraints;type:json;not null"`
	HybridEntriesJSON  string `gorm:"column:hybrid_entries;type:json;not null"`
	AllocatedItemsJSON string `gorm:"column:allocated_items;type:json;not null"`
	CandidatePlansJSON string `gorm:"column:candidate_plans;type:json;not null"`

	// 4. 这组字段用于恢复“本轮策略语义”，支持后续在会话内连续微调。
	UserIntent       string `gorm:"column:user_intent;type:text"`
	Strategy         string `gorm:"column:strategy;type:varchar(32);not null;default:steady"`
	AdjustmentScope  string `gorm:"column:adjustment_scope;type:varchar(16);not null;default:large"`
	RestartRequested bool   `gorm:"column:restart_requested;not null;default:false"`

	// 5. 这组字段用于预览展示与链路排障。
	FinalSummary string `gorm:"column:final_summary;type:text"`
	Completed    bool   `gorm:"column:completed;not null;default:false"`
	TraceID      string `gorm:"column:trace_id;type:varchar(64);index:idx_schedule_state_trace_id"`

	CreatedAt time.Time `gorm:"column:created_at;autoCreateTime"`
	UpdatedAt time.Time `gorm:"column:updated_at;autoUpdateTime;index:idx_schedule_state_user_updated,priority:2"`
}

func (AgentScheduleState) TableName() string {
	return "agent_schedule_states"
}

// SchedulePlanStateSnapshot 是服务层与 DAO 之间的快照传输结构（DTO）。
//
// 职责边界：
// 1. 负责在 service 与 dao 之间传递“强类型快照”；
// 2. 由 DAO 负责把该结构序列化/反序列化为数据库 JSON 字段；
// 3. 不承载运行期临时字段（如并发信号、chan、上下文对象等）。
type SchedulePlanStateSnapshot struct {
	UserID         int
	ConversationID string
	Revision       int
	StateVersion   int

	TaskClassIDs   []int
	Constraints    []string
	HybridEntries  []HybridScheduleEntry
	AllocatedItems []TaskClassItem
	CandidatePlans []UserWeekSchedule

	UserIntent       string
	Strategy         string
	AdjustmentScope  string
	RestartRequested bool
	FinalSummary     string
	Completed        bool
	TraceID          string

	UpdatedAt time.Time
}
