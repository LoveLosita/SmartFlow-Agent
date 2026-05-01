package service

import (
	"context"

	activegraph "github.com/LoveLosita/smartflow/backend/active_scheduler/graph"
	"github.com/LoveLosita/smartflow/backend/active_scheduler/trigger"
)

// AsGraphDryRunFunc 把现有 dry-run service 适配成 graph runner 可用的入口。
//
// 职责边界：
// 1. 只做 service.Result -> graph.DryRunData 的轻量转换；
// 2. 不改写 dry-run 行为，不引入额外候选逻辑；
// 3. 让 graph runner 可以复用现有 BuildContext -> Observe -> GenerateCandidates 链路。
func (s *DryRunService) AsGraphDryRunFunc() activegraph.DryRunFunc {
	if s == nil {
		return nil
	}
	return func(ctx context.Context, trig trigger.ActiveScheduleTrigger) (*activegraph.DryRunData, error) {
		result, err := s.DryRun(ctx, trig)
		if err != nil {
			return nil, err
		}
		return &activegraph.DryRunData{
			Context:     result.Context,
			Observation: result.Observation,
			Candidates:  result.Candidates,
		}, nil
	}
}
