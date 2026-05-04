package schedulercontext

import (
	"time"

	"github.com/LoveLosita/smartflow/backend/services/active_scheduler/core/ports"
	"github.com/LoveLosita/smartflow/backend/services/active_scheduler/core/trigger"
)

const (
	WindowReasonRolling24H = "rolling_24h"
)

// ActiveScheduleContext 是主动调度 dry-run 的只读事实快照。
//
// 职责边界：
// 1. 负责承载 BuildContext 阶段聚合出的事实；
// 2. 不包含 DAO、service 或 provider 实例；
// 3. 不负责生成候选，也不负责写 preview、通知或正式日程。
type ActiveScheduleContext struct {
	Trigger       trigger.ActiveScheduleTrigger
	User          UserFacts
	Now           NowFacts
	Window        WindowFacts
	Target        TargetFacts
	TaskPoolFacts TaskPoolFacts
	ScheduleFacts ScheduleFacts
	FeedbackFacts FeedbackFacts
	DerivedFacts  DerivedFacts
	Trace         TraceFacts
}

type UserFacts struct {
	UserID   int
	Timezone string
}

type NowFacts struct {
	RealNow      time.Time
	EffectiveNow time.Time
}

type WindowFacts struct {
	StartAt       time.Time
	EndAt         time.Time
	RelativeSlots []ports.Slot
	WindowReason  string
}

type TargetFacts struct {
	SourceType         trigger.TargetType
	TaskID             int
	ScheduleEventID    int
	TaskItemID         int
	Title              string
	EstimatedSections  int
	DeadlineAt         *time.Time
	UrgencyThresholdAt *time.Time
	Priority           int
	Status             string
}

type TaskPoolFacts struct {
	TargetTask *ports.TaskFact
}

type ScheduleFacts struct {
	Events          []ports.ScheduleEventFact
	OccupiedSlots   []ports.Slot
	FreeSlots       []ports.Slot
	NextDynamicTask *ports.ScheduleEventFact
}

type FeedbackFacts struct {
	FeedbackID       string
	FeedbackText     string
	FeedbackTarget   string
	TargetKnown      bool
	TargetEventID    int
	TargetTaskItemID int
}

type DerivedFacts struct {
	TargetAlreadyScheduled bool
	TargetCompleted        bool
	AvailableCapacity      int
	MissingInfo            []string
}

type TraceFacts struct {
	TraceID    string
	BuildSteps []string
	Warnings   []string
}
