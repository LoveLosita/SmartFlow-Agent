package ports

import (
	"context"
	"time"
)

// Slot 是主动调度内部使用的原子节次坐标。
//
// 职责边界：
// 1. 只描述可比较、可落预览的时间格；
// 2. 不绑定 schedules 表模型；
// 3. StartAt / EndAt 可为空值，排序会退回到 week/day/section。
type Slot struct {
	Week      int
	DayOfWeek int
	Section   int
	StartAt   time.Time
	EndAt     time.Time
}

// SlotSpan 表示一个连续节次块。
type SlotSpan struct {
	Start            Slot
	End              Slot
	DurationSections int
}

// TaskFact 是 task_pool 任务在主动调度里的最小事实快照。
type TaskFact struct {
	ID                 int
	UserID             int
	Title              string
	Priority           int
	IsCompleted        bool
	DeadlineAt         *time.Time
	UrgencyThresholdAt *time.Time
	EstimatedSections  int
}

// ScheduleEventFact 是日程块在主动调度里的最小事实快照。
type ScheduleEventFact struct {
	ID             int
	UserID         int
	Title          string
	SourceType     string
	RelID          int
	IsDynamicTask  bool
	IsCompleted    bool
	Slots          []Slot
	TaskClassID    int
	TaskItemID     int
	CanBeShortened bool
}

// ScheduleWindowFacts 是滚动窗口内日程事实快照。
type ScheduleWindowFacts struct {
	Events                 []ScheduleEventFact
	OccupiedSlots          []Slot
	FreeSlots              []Slot
	NextDynamicTask        *ScheduleEventFact
	TargetAlreadyScheduled bool
}

// FeedbackFact 是 unfinished_feedback 的最小事实快照。
type FeedbackFact struct {
	FeedbackID       string
	Text             string
	TargetKnown      bool
	TargetEventID    int
	TargetTaskItemID int
	TargetTitle      string
	SubmittedAt      time.Time
}

// TaskRequest 是任务读取端口的入参。
type TaskRequest struct {
	UserID int
	TaskID int
	Now    time.Time
}

// ScheduleWindowRequest 是日程窗口读取端口的入参。
type ScheduleWindowRequest struct {
	UserID      int
	TargetType  string
	TargetID    int
	WindowStart time.Time
	WindowEnd   time.Time
	Now         time.Time
}

// FeedbackRequest 是反馈读取端口的入参。
type FeedbackRequest struct {
	UserID         int
	FeedbackID     string
	IdempotencyKey string
	TargetType     string
	TargetID       int
}

// TaskReader 负责读取主动调度所需的 task_pool 事实。
//
// 职责边界：
// 1. 可以由 adapter 调用既有 service / DAO 组装事实；
// 2. active_scheduler 主链路只依赖该端口，不直接 import 其它领域 DAO；
// 3. found=false 表示目标不存在或当前用户无权访问，由观察链路转成 ask_user。
type TaskReader interface {
	GetTaskForActiveSchedule(ctx context.Context, req TaskRequest) (task TaskFact, found bool, err error)
}

// ScheduleReader 负责读取滚动时间窗内的日程事实。
type ScheduleReader interface {
	GetScheduleFactsByWindow(ctx context.Context, req ScheduleWindowRequest) (ScheduleWindowFacts, error)
}

// FeedbackReader 负责读取用户反馈信号。
type FeedbackReader interface {
	GetFeedbackSignal(ctx context.Context, req FeedbackRequest) (feedback FeedbackFact, found bool, err error)
}

// Readers 聚合 dry-run 主链路依赖的外部读取端口。
//
// 职责边界：
// 1. 只聚合读取依赖，不包含正式写入 preview / schedule / notification 的能力；
// 2. 便于 API、worker 和测试使用同一套 dry-run service；
// 3. 任一必需端口为空时，由 service 初始化阶段拒绝。
type Readers struct {
	TaskReader     TaskReader
	ScheduleReader ScheduleReader
	FeedbackReader FeedbackReader
}
