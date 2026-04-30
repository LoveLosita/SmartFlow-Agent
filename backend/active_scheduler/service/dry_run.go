package service

import (
	"context"
	"errors"
	"time"

	"github.com/LoveLosita/smartflow/backend/active_scheduler/candidate"
	schedulercontext "github.com/LoveLosita/smartflow/backend/active_scheduler/context"
	"github.com/LoveLosita/smartflow/backend/active_scheduler/observe"
	"github.com/LoveLosita/smartflow/backend/active_scheduler/ports"
	"github.com/LoveLosita/smartflow/backend/active_scheduler/trigger"
)

// DryRunResult 是 API dry-run / worker 测试入口可直接消费的同步结果。
type DryRunResult struct {
	Context     *schedulercontext.ActiveScheduleContext
	Observation observe.Result
	Candidates  []candidate.Candidate
}

// DryRunService 编排主动调度 dry-run 主链路。
//
// 职责边界：
// 1. 固定执行 BuildContext -> Observe -> GenerateCandidates；
// 2. 不调用 LLM、不写 preview、不发 notification、不正式写日程；
// 3. 后续 API / worker 应复用该入口，避免出现第二套 dry-run 诊断逻辑。
type DryRunService struct {
	builder   *schedulercontext.Builder
	analyzer  *observe.Analyzer
	generator *candidate.Generator
}

// NewDryRunService 创建主动调度 dry-run 服务。
func NewDryRunService(readers ports.Readers) (*DryRunService, error) {
	builder, err := schedulercontext.NewBuilder(readers)
	if err != nil {
		return nil, err
	}
	return &DryRunService{
		builder:   builder,
		analyzer:  observe.NewAnalyzer(),
		generator: candidate.NewGenerator(),
	}, nil
}

// SetClock 注入测试时钟。
func (s *DryRunService) SetClock(clock func() time.Time) {
	if s != nil && s.builder != nil {
		s.builder.SetClock(clock)
	}
}

// DryRun 执行主动调度同步诊断。
func (s *DryRunService) DryRun(ctx context.Context, trig trigger.ActiveScheduleTrigger) (*DryRunResult, error) {
	if s == nil || s.builder == nil || s.analyzer == nil || s.generator == nil {
		return nil, errors.New("DryRunService 尚未正确初始化")
	}

	// 1. 构造上下文：读取 task / schedule / feedback 的只读事实快照。
	activeContext, err := s.builder.BuildContext(ctx, trig)
	if err != nil {
		return nil, err
	}

	// 2. 主动观测：生成 metrics、issues 和初步裁决，不生成正式变更。
	observation := s.analyzer.Observe(activeContext)

	// 3. 候选生成：只枚举第一版允许的确定性候选，压缩融合保持关闭。
	candidates := s.generator.GenerateCandidates(activeContext, observation)
	fallbackCandidateID := ""
	if len(candidates) > 0 {
		fallbackCandidateID = candidates[0].CandidateID
	}
	observation = s.analyzer.FinalizeDecision(observation, len(applicableCandidates(candidates)), fallbackCandidateID)

	return &DryRunResult{
		Context:     activeContext,
		Observation: observation,
		Candidates:  candidates,
	}, nil
}

func applicableCandidates(candidates []candidate.Candidate) []candidate.Candidate {
	result := make([]candidate.Candidate, 0, len(candidates))
	for _, item := range candidates {
		if item.CandidateType == candidate.TypeAddTaskPoolToSchedule || item.CandidateType == candidate.TypeCreateMakeup {
			result = append(result, item)
		}
	}
	return result
}
