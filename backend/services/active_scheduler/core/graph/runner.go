package graph

import (
	"context"
	"errors"
	"fmt"

	"github.com/LoveLosita/smartflow/backend/services/active_scheduler/core/candidate"
	schedulercontext "github.com/LoveLosita/smartflow/backend/services/active_scheduler/core/context"
	"github.com/LoveLosita/smartflow/backend/services/active_scheduler/core/observe"
	"github.com/LoveLosita/smartflow/backend/services/active_scheduler/core/selection"
	"github.com/LoveLosita/smartflow/backend/services/active_scheduler/core/trigger"
	"github.com/cloudwego/eino/compose"
)

const GraphName = "active_schedule_graph"

const (
	NodeDryRun = "dry_run"
	NodeSelect = "select"
)

// DryRunData 承载 graph 中 dry-run 节点产出的只读结果。
//
// 职责边界：
// 1. 只装载构造 preview / 选择器所需的 dry-run 结果；
// 2. 不包含落库状态，也不包含通知副作用；
// 3. 由 graph runner 负责串联后续选择步骤。
type DryRunData struct {
	Context     *schedulercontext.ActiveScheduleContext
	Observation observe.Result
	Candidates  []candidate.Candidate
}

// DryRunFunc 描述 graph 依赖的 dry-run 执行入口。
//
// 职责边界：
// 1. 只负责把 trigger 变成 dry-run 结果；
// 2. 不负责 selection / preview / notification；
// 3. 由 service 层用现有 DryRunService 做适配注入。
type DryRunFunc func(ctx context.Context, trig trigger.ActiveScheduleTrigger) (*DryRunData, error)

// Selector 描述 graph 依赖的候选选择器。
//
// 职责边界：
// 1. 只负责在后端候选里做有限选择与解释生成；
// 2. 不负责构造 dry-run 结果；
// 3. 不负责 preview 落库与通知投递。
type Selector interface {
	Select(ctx context.Context, req selection.SelectRequest) (selection.Result, error)
}

// RunResult 是 graph 的最终输出。
//
// 职责边界：
// 1. 只回传 dry-run 与选择结果；
// 2. 不包含 preview / notification 的持久化结果；
// 3. 上层 service 仍负责事务、落库和 outbox。
type RunResult struct {
	DryRunData      *DryRunData
	SelectionResult selection.Result
}

type runState struct {
	Trigger         trigger.ActiveScheduleTrigger
	DryRunData      *DryRunData
	SelectionResult selection.Result
}

// Runner 把 dry-run 与受限选择串成一条可复用的 graph。
type Runner struct {
	dryRun   DryRunFunc
	selector Selector
}

// NewRunner 创建主动调度 graph runner。
func NewRunner(dryRun DryRunFunc, selector Selector) (*Runner, error) {
	if dryRun == nil {
		return nil, errors.New("active scheduler dry-run 不能为空")
	}
	if selector == nil {
		return nil, errors.New("active scheduler selector 不能为空")
	}
	return &Runner{dryRun: dryRun, selector: selector}, nil
}

// Run 执行 dry-run -> select 的 graph。
//
// 步骤化说明：
// 1. 先跑 dry-run，拿到上下文、观测结果与候选；
// 2. 再交给受限选择器做选中与解释生成；
// 3. 任一步失败都直接返回，避免写入半截 preview；
// 4. 上层 service 再决定是否写 preview / 发通知。
func (r *Runner) Run(ctx context.Context, trig trigger.ActiveScheduleTrigger) (*RunResult, error) {
	if r == nil || r.dryRun == nil || r.selector == nil {
		return nil, errors.New("active scheduler graph runner 未初始化")
	}

	state := &runState{Trigger: trig}
	g := compose.NewGraph[*runState, *runState]()

	if err := g.AddLambdaNode(NodeDryRun, compose.InvokableLambda(r.dryRunNode())); err != nil {
		return nil, err
	}
	if err := g.AddLambdaNode(NodeSelect, compose.InvokableLambda(r.selectNode())); err != nil {
		return nil, err
	}

	if err := g.AddEdge(compose.START, NodeDryRun); err != nil {
		return nil, err
	}
	if err := g.AddEdge(NodeDryRun, NodeSelect); err != nil {
		return nil, err
	}
	if err := g.AddEdge(NodeSelect, compose.END); err != nil {
		return nil, err
	}

	runnable, err := g.Compile(ctx,
		compose.WithGraphName(GraphName),
		compose.WithMaxRunSteps(8),
		compose.WithNodeTriggerMode(compose.AnyPredecessor),
	)
	if err != nil {
		return nil, err
	}

	out, err := runnable.Invoke(ctx, state)
	if err != nil {
		return nil, err
	}
	if out == nil {
		return nil, errors.New("active scheduler graph 返回空状态")
	}
	return &RunResult{
		DryRunData:      out.DryRunData,
		SelectionResult: out.SelectionResult,
	}, nil
}

func (r *Runner) dryRunNode() func(context.Context, *runState) (*runState, error) {
	return func(ctx context.Context, state *runState) (*runState, error) {
		if state == nil {
			return nil, errors.New("active scheduler graph state 不能为空")
		}
		if r == nil || r.dryRun == nil {
			return nil, errors.New("active scheduler dry-run 不能为空")
		}

		// 1. 先跑 dry-run，汇集上下文、观测结果和候选，避免 selection 直接依赖数据库。
		// 2. dry-run 出错时直接返回，不进入后续选择。
		result, err := r.dryRun(ctx, state.Trigger)
		if err != nil {
			return nil, err
		}
		state.DryRunData = result
		return state, nil
	}
}

func (r *Runner) selectNode() func(context.Context, *runState) (*runState, error) {
	return func(ctx context.Context, state *runState) (*runState, error) {
		if state == nil {
			return nil, errors.New("active scheduler graph state 不能为空")
		}
		if state.DryRunData == nil {
			return nil, errors.New("active scheduler graph 缺少 dry-run 结果")
		}
		if r == nil || r.selector == nil {
			return nil, errors.New("active scheduler selector 不能为空")
		}

		// 1. 没有候选时，不强行调用选择器，直接返回空选择结果，交给上层决定是否继续写 preview。
		// 2. 有候选时再做受限 LLM 选择，确保模型只看后端生成的候选视图。
		if len(state.DryRunData.Candidates) == 0 {
			state.SelectionResult = selection.Result{}
			return state, nil
		}

		result, err := r.selector.Select(ctx, selection.SelectRequest{
			ActiveContext: state.DryRunData.Context,
			Observation:   state.DryRunData.Observation,
			Candidates:    state.DryRunData.Candidates,
		})
		if err != nil {
			return nil, err
		}
		state.SelectionResult = result
		return state, nil
	}
}

func (r *Runner) String() string {
	if r == nil {
		return "active_scheduler_graph(nil)"
	}
	return fmt.Sprintf("active_scheduler_graph(dry_run=%t, selector=%t)", r.dryRun != nil, r.selector != nil)
}
