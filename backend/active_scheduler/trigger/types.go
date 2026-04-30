package trigger

import (
	"errors"
	"time"
)

// TriggerType 是主动调度第一版允许进入 dry-run 主链路的触发类型。
//
// 职责边界：
// 1. 只表达触发信号分类；
// 2. 不负责判断任务是否真的需要调度；
// 3. 不承载 preview、notification 或 apply 状态。
type TriggerType string

const (
	TriggerTypeImportantUrgentTask TriggerType = "important_urgent_task"
	TriggerTypeUnfinishedFeedback  TriggerType = "unfinished_feedback"
)

// Source 表示触发信号来源；dry-run 第一版只消费该字段用于审计和 mock_now 校验。
type Source string

const (
	SourceWorkerDueJob Source = "worker_due_job"
	SourceAPITrigger   Source = "api_trigger"
	SourceAPIDryRun    Source = "api_dry_run"
	SourceUserFeedback Source = "user_feedback"
)

// TargetType 表示触发信号指向的业务对象类型。
type TargetType string

const (
	TargetTypeTaskPool      TargetType = "task_pool"
	TargetTypeScheduleEvent TargetType = "schedule_event"
	TargetTypeTaskItem      TargetType = "task_item"
)

// ActiveScheduleTrigger 是主动调度主链路的统一输入。
//
// 职责边界：
// 1. 负责承载 API dry-run、正式 trigger、worker 与用户反馈归一后的输入；
// 2. 不负责读取任务、日程或反馈事实；
// 3. TargetID 在 unfinished_feedback 且反馈目标未知时允许为 0，由观察链路转成 ask_user。
type ActiveScheduleTrigger struct {
	TriggerID      string
	UserID         int
	TriggerType    TriggerType
	Source         Source
	TargetType     TargetType
	TargetID       int
	FeedbackID     string
	IdempotencyKey string
	MockNow        *time.Time
	IsMockTime     bool
	RequestedAt    time.Time
	TraceID        string
}

// Validate 校验触发信号是否能进入主动调度 dry-run 主链路。
//
// 职责边界：
// 1. 只做枚举、归属与 mock_now 入口级校验；
// 2. 不判断目标是否存在，也不判断是否应生成候选；
// 3. 返回 nil 表示可以继续构造上下文，error 表示调用方应直接拒绝请求。
func (t ActiveScheduleTrigger) Validate() error {
	if t.UserID <= 0 {
		return errors.New("user_id 必须大于 0")
	}
	if t.TriggerType != TriggerTypeImportantUrgentTask && t.TriggerType != TriggerTypeUnfinishedFeedback {
		return errors.New("trigger_type 不受支持")
	}
	if t.Source != SourceWorkerDueJob && t.Source != SourceAPITrigger && t.Source != SourceAPIDryRun && t.Source != SourceUserFeedback {
		return errors.New("source 不受支持")
	}
	if t.TargetType != TargetTypeTaskPool && t.TargetType != TargetTypeScheduleEvent && t.TargetType != TargetTypeTaskItem {
		return errors.New("target_type 不受支持")
	}
	if t.TargetID <= 0 && t.TriggerType != TriggerTypeUnfinishedFeedback {
		return errors.New("target_id 必须大于 0")
	}
	if t.MockNow != nil && t.Source != SourceAPIDryRun && t.Source != SourceAPITrigger {
		return errors.New("mock_now 只允许 API dry-run 或 API trigger 使用")
	}
	if t.MockNow != nil && !t.IsMockTime {
		return errors.New("传入 mock_now 时必须显式标记 is_mock_time")
	}
	return nil
}

// EffectiveNow 返回主动调度本次运行应使用的业务当前时间。
//
// 职责边界：
// 1. dry-run / 测试 trigger 可使用 MockNow；
// 2. 后台 worker 使用调用方传入的真实 now；
// 3. 不负责时区转换，调用方应保证 now 与用户时区语义一致。
func (t ActiveScheduleTrigger) EffectiveNow(realNow time.Time) time.Time {
	if t.MockNow != nil {
		return *t.MockNow
	}
	return realNow
}
