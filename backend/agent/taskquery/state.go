package taskquery

import "time"

const (
	// DefaultTaskQueryLimit 是任务查询默认返回条数。
	DefaultTaskQueryLimit = 5
	// MaxTaskQueryLimit 是任务查询最大返回条数。
	MaxTaskQueryLimit = 20
	// DefaultReflectRetryMax 是反思重试默认上限。
	DefaultReflectRetryMax = 2
)

// TaskQueryState 是任务查询图在节点间传递的统一状态容器。
//
// 职责边界：
// 1. 保存“规划参数、查询结果、反思决策、最终回复”；
// 2. 控制“是否重试 + 已重试次数”状态机；
// 3. 不负责真正查库，查库由工具执行。
type TaskQueryState struct {
	// 请求上下文
	UserMessage    string
	RequestNowText string

	// 规划结果
	UserGoal string
	Plan     QueryPlan
	// ExplicitLimit 表示“用户原话中明确指定的数量”。
	//
	// 语义说明：
	// 1. 0 代表未显式指定；
	// 2. >0 时应锁定该数量，不允许反思补丁或自动放宽改写。
	ExplicitLimit int

	// 上一轮查询结果
	LastQueryItems []TaskQueryToolRecord
	LastQueryTotal int

	// 自动放宽状态
	AutoBroadenApplied bool

	// 反思状态
	RetryCount      int
	MaxReflectRetry int
	NeedRetry       bool
	ReflectReason   string

	// 最终输出
	FinalReply string
}

// QueryPlan 是“任务查询计划”的统一结构。
//
// 语义说明：
// 1. Quadrants 为空表示“查全部象限”；非空表示“只查这些象限”；
// 2. DeadlineBefore/AfterText 保留原始文本，方便日志和反思 prompt；
// 3. DeadlineBefore/After 是解析后的时间对象，供工具调用使用。
type QueryPlan struct {
	Quadrants []int

	SortBy string
	Order  string
	Limit  int

	IncludeCompleted bool
	Keyword          string

	DeadlineBeforeText string
	DeadlineAfterText  string
	DeadlineBefore     *time.Time
	DeadlineAfter      *time.Time
}

// NewTaskQueryState 创建任务查询初始状态。
func NewTaskQueryState(userMessage, requestNowText string, maxReflectRetry int) *TaskQueryState {
	if maxReflectRetry <= 0 {
		maxReflectRetry = DefaultReflectRetryMax
	}
	return &TaskQueryState{
		UserMessage:        userMessage,
		RequestNowText:     requestNowText,
		MaxReflectRetry:    maxReflectRetry,
		LastQueryItems:     make([]TaskQueryToolRecord, 0),
		NeedRetry:          false,
		ReflectReason:      "",
		AutoBroadenApplied: false,
	}
}
