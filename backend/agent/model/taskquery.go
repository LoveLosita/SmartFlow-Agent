package agentmodel

import "time"

const (
	// DefaultTaskQueryLimit 是任务查询默认返回条数。
	DefaultTaskQueryLimit = 5
	// MaxTaskQueryLimit 是任务查询允许的最大返回条数，用于限制模型输出范围。
	MaxTaskQueryLimit = 20
	// DefaultTaskQueryReflectRetry 是任务查询反思节点的默认重试次数。
	DefaultTaskQueryReflectRetry = 2
)

// TaskQueryItem 是任务查询链路最终展示给模型和用户的轻量任务视图。
//
// 职责边界：
// 1. 只承载展示和反思所需字段，避免把底层数据库结构直接暴露给图层。
// 2. 不负责描述完整任务实体，也不负责持久化。
type TaskQueryItem struct {
	ID                 int    `json:"id"`
	Title              string `json:"title"`
	PriorityGroup      int    `json:"priority_group"`
	PriorityLabel      string `json:"priority_label"`
	IsCompleted        bool   `json:"is_completed"`
	DeadlineAt         string `json:"deadline_at,omitempty"`
	UrgencyThresholdAt string `json:"urgency_threshold_at,omitempty"`
}

// TaskQueryPlan 是计划节点产出的内部查询方案。
//
// 输入输出语义：
// 1. DeadlineBeforeText / DeadlineAfterText 保留原始文本，便于继续透传给工具和日志。
// 2. DeadlineBefore / DeadlineAfter 是归一化后的时间对象，仅供执行期使用。
// 3. IncludeCompleted=true 表示允许把已完成任务纳入候选集。
type TaskQueryPlan struct {
	Quadrants []int
	SortBy    string
	Order     string
	Limit     int

	IncludeCompleted   bool
	Keyword            string
	DeadlineBeforeText string
	DeadlineAfterText  string
	DeadlineBefore     *time.Time
	DeadlineAfter      *time.Time
}

// TaskQueryState 是任务查询图在各节点之间流转的完整状态。
//
// 职责边界：
// 1. 负责保存用户输入、结构化计划、工具结果和反思过程状态。
// 2. 不负责图编排本身，也不直接绑定外部数据库实体。
type TaskQueryState struct {
	UserMessage    string
	RequestNowText string
	UserGoal       string
	Plan           TaskQueryPlan
	ExplicitLimit  int

	LastQueryItems     []TaskQueryItem
	LastQueryTotal     int
	AutoBroadenApplied bool
	RetryCount         int
	MaxReflectRetry    int
	NeedRetry          bool
	ReflectReason      string
	FinalReply         string
}

// NewTaskQueryState 负责创建任务查询图的初始状态。
//
// 输入输出语义：
// 1. maxReflectRetry <= 0 时会自动回退到默认值，避免上层遗漏配置导致无法重试。
// 2. 返回的状态对象已初始化空切片，可直接进入 graph 执行。
func NewTaskQueryState(userMessage, requestNowText string, maxReflectRetry int) *TaskQueryState {
	if maxReflectRetry <= 0 {
		maxReflectRetry = DefaultTaskQueryReflectRetry
	}
	return &TaskQueryState{
		UserMessage:        userMessage,
		RequestNowText:     requestNowText,
		MaxReflectRetry:    maxReflectRetry,
		LastQueryItems:     make([]TaskQueryItem, 0),
		AutoBroadenApplied: false,
	}
}
