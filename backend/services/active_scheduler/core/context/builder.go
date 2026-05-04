package schedulercontext

import (
	"context"
	"errors"
	"time"

	"github.com/LoveLosita/smartflow/backend/services/active_scheduler/core/ports"
	"github.com/LoveLosita/smartflow/backend/services/active_scheduler/core/trigger"
)

// Builder 负责把统一 trigger 转成主动调度只读事实快照。
//
// 职责边界：
// 1. 只通过 ports 读取外部事实；
// 2. 不生成 candidates，不调用 LLM，不写 preview；
// 3. 缺少业务事实时尽量写入 MissingInfo，让 observe 阶段裁决 ask_user。
type Builder struct {
	readers ports.Readers
	clock   func() time.Time
}

func NewBuilder(readers ports.Readers) (*Builder, error) {
	if readers.ScheduleReader == nil {
		return nil, errors.New("ScheduleReader 不能为空")
	}
	if readers.TaskReader == nil {
		return nil, errors.New("TaskReader 不能为空")
	}
	if readers.FeedbackReader == nil {
		return nil, errors.New("FeedbackReader 不能为空")
	}
	return &Builder{
		readers: readers,
		clock:   time.Now,
	}, nil
}

// SetClock 允许测试注入稳定时钟。
//
// 职责边界：
// 1. 仅影响 real_now；
// 2. 不覆盖 trigger.MockNow 的业务语义；
// 3. nil 会被忽略，避免测试误把时钟置空。
func (b *Builder) SetClock(clock func() time.Time) {
	if clock != nil {
		b.clock = clock
	}
}

// BuildContext 执行 dry-run 主链路第一步：构造主动调度上下文。
func (b *Builder) BuildContext(ctx context.Context, trig trigger.ActiveScheduleTrigger) (*ActiveScheduleContext, error) {
	if err := trig.Validate(); err != nil {
		return nil, err
	}

	realNow := b.clock()
	effectiveNow := trig.EffectiveNow(realNow)
	windowStart := effectiveNow
	windowEnd := effectiveNow.Add(24 * time.Hour)
	result := &ActiveScheduleContext{
		Trigger: trig,
		User: UserFacts{
			UserID:   trig.UserID,
			Timezone: effectiveNow.Location().String(),
		},
		Now: NowFacts{
			RealNow:      realNow,
			EffectiveNow: effectiveNow,
		},
		Window: WindowFacts{
			StartAt:      windowStart,
			EndAt:        windowEnd,
			WindowReason: WindowReasonRolling24H,
		},
		Target: TargetFacts{SourceType: trig.TargetType},
		Trace: TraceFacts{
			TraceID: trig.TraceID,
			BuildSteps: []string{
				"1. 校验 trigger 并确定 real_now / effective_now。",
				"2. 构造滚动 24 小时时间窗，后续读取均基于同一窗口。",
			},
		},
	}

	switch trig.TriggerType {
	case trigger.TriggerTypeImportantUrgentTask:
		if err := b.fillTaskPoolFacts(ctx, result); err != nil {
			return nil, err
		}
	case trigger.TriggerTypeUnfinishedFeedback:
		if err := b.fillFeedbackFacts(ctx, result); err != nil {
			return nil, err
		}
	}

	if err := b.fillScheduleFacts(ctx, result); err != nil {
		return nil, err
	}
	b.fillDerivedFacts(result)
	return result, nil
}

func (b *Builder) fillTaskPoolFacts(ctx context.Context, result *ActiveScheduleContext) error {
	task, found, err := b.readers.TaskReader.GetTaskForActiveSchedule(ctx, ports.TaskRequest{
		UserID: result.Trigger.UserID,
		TaskID: result.Trigger.TargetID,
		Now:    result.Now.EffectiveNow,
	})
	if err != nil {
		return err
	}
	if !found {
		result.DerivedFacts.MissingInfo = append(result.DerivedFacts.MissingInfo, "target_task")
		result.Trace.Warnings = append(result.Trace.Warnings, "未读取到目标 task_pool 任务，后续应转为 ask_user。")
		return nil
	}

	estimatedSections := task.EstimatedSections
	if estimatedSections <= 0 {
		// 1. 旧数据可能没有 estimated_sections；MVP 兜底为 1 节，避免空值阻断 dry-run。
		// 2. 正式 adapter 后续应尽量提供真实字段，减少兜底带来的预览偏差。
		estimatedSections = 1
	}
	result.TaskPoolFacts.TargetTask = &task
	result.Target.TaskID = task.ID
	result.Target.Title = task.Title
	result.Target.EstimatedSections = estimatedSections
	result.Target.DeadlineAt = task.DeadlineAt
	result.Target.UrgencyThresholdAt = task.UrgencyThresholdAt
	result.Target.Priority = task.Priority
	if task.IsCompleted {
		result.Target.Status = "completed"
	} else {
		result.Target.Status = "pending"
	}
	result.Trace.BuildSteps = append(result.Trace.BuildSteps, "3. 通过 TaskReader 读取 task_pool 目标事实。")
	return nil
}

func (b *Builder) fillFeedbackFacts(ctx context.Context, result *ActiveScheduleContext) error {
	feedback, found, err := b.readers.FeedbackReader.GetFeedbackSignal(ctx, ports.FeedbackRequest{
		UserID:         result.Trigger.UserID,
		FeedbackID:     result.Trigger.FeedbackID,
		IdempotencyKey: result.Trigger.IdempotencyKey,
		TargetType:     string(result.Trigger.TargetType),
		TargetID:       result.Trigger.TargetID,
	})
	if err != nil {
		return err
	}
	if !found {
		result.DerivedFacts.MissingInfo = append(result.DerivedFacts.MissingInfo, "feedback_signal")
		result.Trace.Warnings = append(result.Trace.Warnings, "未读取到反馈信号，后续应转为 ask_user。")
		return nil
	}

	result.FeedbackFacts = FeedbackFacts{
		FeedbackID:       feedback.FeedbackID,
		FeedbackText:     feedback.Text,
		TargetKnown:      feedback.TargetKnown,
		TargetEventID:    feedback.TargetEventID,
		TargetTaskItemID: feedback.TargetTaskItemID,
		FeedbackTarget:   feedback.TargetTitle,
	}
	result.Target.ScheduleEventID = feedback.TargetEventID
	result.Target.TaskItemID = feedback.TargetTaskItemID
	result.Target.Title = feedback.TargetTitle
	result.Target.EstimatedSections = 1
	if !feedback.TargetKnown {
		result.DerivedFacts.MissingInfo = append(result.DerivedFacts.MissingInfo, "feedback_target")
	}
	result.Trace.BuildSteps = append(result.Trace.BuildSteps, "3. 通过 FeedbackReader 读取 unfinished_feedback 信号。")
	return nil
}

func (b *Builder) fillScheduleFacts(ctx context.Context, result *ActiveScheduleContext) error {
	facts, err := b.readers.ScheduleReader.GetScheduleFactsByWindow(ctx, ports.ScheduleWindowRequest{
		UserID:      result.Trigger.UserID,
		TargetType:  string(result.Trigger.TargetType),
		TargetID:    result.Trigger.TargetID,
		WindowStart: result.Window.StartAt,
		WindowEnd:   result.Window.EndAt,
		Now:         result.Now.EffectiveNow,
	})
	if err != nil {
		return err
	}
	result.ScheduleFacts = ScheduleFacts{
		Events:          facts.Events,
		OccupiedSlots:   facts.OccupiedSlots,
		FreeSlots:       facts.FreeSlots,
		NextDynamicTask: facts.NextDynamicTask,
	}
	result.Window.RelativeSlots = append(result.Window.RelativeSlots, facts.OccupiedSlots...)
	result.Window.RelativeSlots = append(result.Window.RelativeSlots, facts.FreeSlots...)
	result.DerivedFacts.TargetAlreadyScheduled = facts.TargetAlreadyScheduled
	result.Trace.BuildSteps = append(result.Trace.BuildSteps, "4. 通过 ScheduleReader 读取滚动 24 小时日程事实。")
	return nil
}

func (b *Builder) fillDerivedFacts(result *ActiveScheduleContext) {
	result.DerivedFacts.AvailableCapacity = len(result.ScheduleFacts.FreeSlots)
	if result.TaskPoolFacts.TargetTask != nil {
		result.DerivedFacts.TargetCompleted = result.TaskPoolFacts.TargetTask.IsCompleted
	}
	if result.Trigger.TriggerType == trigger.TriggerTypeUnfinishedFeedback && !result.FeedbackFacts.TargetKnown {
		result.DerivedFacts.MissingInfo = appendMissing(result.DerivedFacts.MissingInfo, "feedback_target")
	}
	result.Trace.BuildSteps = append(result.Trace.BuildSteps, "5. 汇总完成状态、已排状态、可用容量与缺失事实。")
}

func appendMissing(values []string, next string) []string {
	for _, value := range values {
		if value == next {
			return values
		}
	}
	return append(values, next)
}
