package agentnode

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	agentmodel "github.com/LoveLosita/smartflow/backend/agent2/model"
	agentprompt "github.com/LoveLosita/smartflow/backend/agent2/prompt"
	agentshared "github.com/LoveLosita/smartflow/backend/agent2/shared"
	"github.com/LoveLosita/smartflow/backend/model"
	"github.com/LoveLosita/smartflow/backend/respond"
	"github.com/cloudwego/eino-ext/components/model/ark"
	einoModel "github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
	arkModel "github.com/volcengine/volcengine-go-sdk/service/arkruntime/model"
)

const (
	// ScheduleRefineGraphNodeContract 是“抽取微调契约”的节点名。
	ScheduleRefineGraphNodeContract = "schedule_refine_contract"
	// ScheduleRefineGraphNodePlan 是“生成执行计划”的节点名。
	ScheduleRefineGraphNodePlan = "schedule_refine_plan"
	// ScheduleRefineGraphNodeSlice 是“生成任务切片”的节点名。
	ScheduleRefineGraphNodeSlice = "schedule_refine_slice"
	// ScheduleRefineGraphNodeRoute 是“复合路由尝试”的节点名。
	ScheduleRefineGraphNodeRoute = "schedule_refine_route"
	// ScheduleRefineGraphNodeReact 是“单任务 ReAct 微调”的节点名。
	ScheduleRefineGraphNodeReact = "schedule_refine_react"
	// ScheduleRefineGraphNodeHardCheck 是“终审硬校验”的节点名。
	ScheduleRefineGraphNodeHardCheck = "schedule_refine_hard_check"
	// ScheduleRefineGraphNodeSummary 是“生成总结回复”的节点名。
	ScheduleRefineGraphNodeSummary = "schedule_refine_summary"
)

const (
	defaultExecuteMax     = agentmodel.ScheduleRefineDefaultExecuteMax
	defaultPerTaskBudget  = agentmodel.ScheduleRefineDefaultPerTaskBudget
	defaultReplanMax      = agentmodel.ScheduleRefineDefaultReplanMax
	defaultCompositeRetry = agentmodel.ScheduleRefineDefaultCompositeRetry
)

type (
	// ScheduleRefineState 是 node 层对连续微调状态的本地别名。
	ScheduleRefineState = agentmodel.ScheduleRefineState
	// RefineContract 是连续微调契约的本地别名。
	RefineContract = agentmodel.RefineContract
	// RefineAssertion 是结构化硬断言的本地别名。
	RefineAssertion = agentmodel.RefineAssertion
	// HardCheckReport 是终审报告的本地别名。
	HardCheckReport = agentmodel.HardCheckReport
	// ReactRoundObservation 是 ReAct 轮次观察的本地别名。
	ReactRoundObservation = agentmodel.ReactRoundObservation
	// PlannerPlan 是阶段计划的本地别名。
	PlannerPlan = agentmodel.PlannerPlan
	// RefineSlicePlan 是切片计划的本地别名。
	RefineSlicePlan = agentmodel.RefineSlicePlan
	// RefineObjective 是目标编译结果的本地别名。
	RefineObjective = agentmodel.RefineObjective
)

func cloneHybridEntries(src []model.HybridScheduleEntry) []model.HybridScheduleEntry {
	return agentshared.CloneHybridEntries(src)
}

func cloneTaskClassItems(src []model.TaskClassItem) []model.TaskClassItem {
	return agentshared.CloneTaskClassItems(src)
}

func cloneWeekSchedules(src []model.UserWeekSchedule) []model.UserWeekSchedule {
	return agentshared.CloneWeekSchedules(src)
}

// ScheduleRefineGraphRunInput 描述“连续微调图”运行所需输入。
//
// 字段说明：
// 1. Model：本轮图运行使用的聊天模型。
// 2. State：预先注入的微调状态，通常来自上一版预览快照。
// 3. EmitStage：阶段回调，允许服务层把进度透传给前端。
type ScheduleRefineGraphRunInput struct {
	Model     *ark.ChatModel
	State     *agentmodel.ScheduleRefineState
	EmitStage func(stage, detail string)
}

// NewScheduleRefineState 基于上一版预览快照初始化连续微调状态。
func NewScheduleRefineState(traceID string, userID int, conversationID string, userMessage string, preview *model.SchedulePlanPreviewCache) *ScheduleRefineState {
	return agentmodel.NewScheduleRefineState(traceID, userID, conversationID, userMessage, preview)
}

// FinalHardCheckPassed 判断最终终审是否整体通过。
func FinalHardCheckPassed(st *ScheduleRefineState) bool {
	return agentmodel.FinalHardCheckPassed(st)
}

// ScheduleRefineNodes 是连续微调图的节点容器。
//
// 职责边界：
// 1. 负责收口模型与阶段回调。
// 2. 负责向 graph 层暴露可直接挂载的方法。
// 3. 不负责 graph 编译与 service 接线。
type ScheduleRefineNodes struct {
	input     ScheduleRefineGraphRunInput
	emitStage func(stage, detail string)
}

// NewScheduleRefineNodes 创建连续微调节点容器。
func NewScheduleRefineNodes(input ScheduleRefineGraphRunInput) (*ScheduleRefineNodes, error) {
	if input.Model == nil {
		return nil, errors.New("schedule refine nodes: model is nil")
	}
	if input.State == nil {
		return nil, errors.New("schedule refine nodes: state is nil")
	}

	emitStage := input.EmitStage
	if emitStage == nil {
		emitStage = func(stage, detail string) {}
	}

	return &ScheduleRefineNodes{
		input:     input,
		emitStage: emitStage,
	}, nil
}

// Contract 负责承接“契约抽取”节点。
func (n *ScheduleRefineNodes) Contract(ctx context.Context, st *agentmodel.ScheduleRefineState) (*agentmodel.ScheduleRefineState, error) {
	return scheduleRefineRunContractNode(ctx, n.input.Model, st, n.emitStage)
}

// Plan 负责承接“执行计划生成”节点。
func (n *ScheduleRefineNodes) Plan(ctx context.Context, st *agentmodel.ScheduleRefineState) (*agentmodel.ScheduleRefineState, error) {
	return scheduleRefineRunPlanNode(ctx, n.input.Model, st, n.emitStage)
}

// Slice 负责承接“任务切片”节点。
func (n *ScheduleRefineNodes) Slice(ctx context.Context, st *agentmodel.ScheduleRefineState) (*agentmodel.ScheduleRefineState, error) {
	return scheduleRefineRunSliceNode(ctx, st, n.emitStage)
}

// Route 负责承接“复合工具路由”节点。
func (n *ScheduleRefineNodes) Route(ctx context.Context, st *agentmodel.ScheduleRefineState) (*agentmodel.ScheduleRefineState, error) {
	return scheduleRefineRunCompositeRouteNode(ctx, st, n.emitStage)
}

// React 负责承接“单任务微步 ReAct”节点。
func (n *ScheduleRefineNodes) React(ctx context.Context, st *agentmodel.ScheduleRefineState) (*agentmodel.ScheduleRefineState, error) {
	return scheduleRefineRunReactLoopNode(ctx, n.input.Model, st, n.emitStage)
}

// HardCheck 负责承接“终审硬校验”节点。
func (n *ScheduleRefineNodes) HardCheck(ctx context.Context, st *agentmodel.ScheduleRefineState) (*agentmodel.ScheduleRefineState, error) {
	return scheduleRefineRunHardCheckNode(ctx, n.input.Model, st, n.emitStage)
}

// Summary 负责承接“最终总结”节点。
func (n *ScheduleRefineNodes) Summary(ctx context.Context, st *agentmodel.ScheduleRefineState) (*agentmodel.ScheduleRefineState, error) {
	return scheduleRefineRunSummaryNode(ctx, n.input.Model, st, n.emitStage)
}

const (
	nodeTimeout      = 120 * time.Second
	plannerMaxTokens = 420
	reactMaxTokens   = 360
)

const (
	jsonContractForContract    = "只输出单个 JSON 对象，不要 Markdown/代码块/解释。必须包含: intent,strategy,hard_requirements,hard_assertions,keep_relative_order,order_scope。"
	jsonContractForPlanner     = "只输出单个 JSON 对象，不要 Markdown/代码块/解释。必须包含: summary,steps。"
	jsonContractForReact       = "只输出单个 JSON 对象，不要 Markdown/代码块/解释。必须包含: done,summary,goal_check,decision,missing_info,tool_calls。"
	jsonContractForReview      = "只输出单个 JSON 对象，不要 Markdown/代码块/解释。必须包含: pass,reason,unmet。"
	jsonContractForPostReflect = "只输出单个 JSON 对象，不要 Markdown/代码块/解释。必须包含: reflection,next_strategy,should_stop。"
)

type scheduleRefineContractOutput struct {
	Intent            string                              `json:"intent"`
	Strategy          string                              `json:"strategy"`
	HardRequirements  []string                            `json:"hard_requirements"`
	HardAssertions    []scheduleRefineHardAssertionOutput `json:"hard_assertions"`
	KeepRelativeOrder bool                                `json:"keep_relative_order"`
	OrderScope        string                              `json:"order_scope"`
}

type scheduleRefineHardAssertionOutput struct {
	Metric     string `json:"metric"`
	Operator   string `json:"operator"`
	Value      int    `json:"value"`
	Min        int    `json:"min"`
	Max        int    `json:"max"`
	Week       int    `json:"week"`
	TargetWeek int    `json:"target_week"`
}

type scheduleRefinePostReflectOutput struct {
	Reflection   string `json:"reflection"`
	NextStrategy string `json:"next_strategy"`
	ShouldStop   bool   `json:"should_stop"`
}

type scheduleRefinePlannerOutput struct {
	Summary string   `json:"summary"`
	Steps   []string `json:"steps"`
}

func scheduleRefineRunContractNode(
	ctx context.Context,
	chatModel *ark.ChatModel,
	st *ScheduleRefineState,
	emitStage func(stage, detail string),
) (*ScheduleRefineState, error) {
	if st == nil {
		return nil, fmt.Errorf("schedule refine: nil state in contract node")
	}
	if chatModel == nil {
		return nil, fmt.Errorf("schedule refine: model is nil in contract node")
	}
	emitStage("schedule_refine.contract.analyzing", "正在抽取本轮微调目标与硬性约束。")

	userPrompt := scheduleRefineWithNearestJSONContract(
		fmt.Sprintf(
			"当前时间=%s\n用户请求=%s\n当前排程条目数=%d\n可调 suggested 数=%d\n已有约束=%s\n历史摘要=%s",
			st.RequestNowText,
			strings.TrimSpace(st.UserMessage),
			len(st.HybridEntries),
			scheduleRefineCountSuggested(st.HybridEntries),
			strings.Join(st.Constraints, "；"),
			scheduleRefineCondenseSummary(st.CandidatePlans),
		),
		jsonContractForContract,
	)
	raw, err := scheduleRefineCallModelText(ctx, chatModel, agentprompt.ScheduleRefineContractPrompt, userPrompt, false, 260, 0)
	if err != nil {
		st.Contract = scheduleRefineBuildFallbackContract(st)
		st.UserIntent = st.Contract.Intent
		emitStage("schedule_refine.contract.fallback", "契约抽取失败，已按兜底策略继续微调。")
		return st, nil
	}
	scheduleRefineEmitModelRawDebug(emitStage, "contract", raw)
	parsed, parseErr := scheduleRefineParseJSON[scheduleRefineContractOutput](raw)
	if parseErr != nil {
		st.Contract = scheduleRefineBuildFallbackContract(st)
		st.UserIntent = st.Contract.Intent
		emitStage("schedule_refine.contract.fallback", fmt.Sprintf("契约解析失败，已按兜底策略继续微调：%s", scheduleRefineTruncate(parseErr.Error(), 180)))
		return st, nil
	}

	intent := strings.TrimSpace(parsed.Intent)
	if intent == "" {
		intent = strings.TrimSpace(st.UserMessage)
	}
	// 1. 顺序策略以用户表达为准：默认保持顺序，明确授权乱序才放开。
	// 2. 不再让模型自行放宽顺序，避免契约漂移导致“默认乱序”。
	keepOrder := scheduleRefineDetectOrderIntent(st.UserMessage)
	reqs := append([]string(nil), parsed.HardRequirements...)
	if keepOrder {
		reqs = append(reqs, "保持任务原始相对顺序不变")
	}
	assertions := scheduleRefineNormalizeHardAssertions(parsed.HardAssertions)
	if len(assertions) == 0 {
		// 1. 当模型未给出结构化断言时，后端基于请求做兜底推断。
		// 2. 目标是保证终审一定可落到“可编程判断”的参数层，而不是停留在自然语言。
		assertions = scheduleRefineInferHardAssertionsFromRequest(st.UserMessage, reqs)
	}
	st.UserIntent = intent
	st.Contract = RefineContract{
		Intent:            intent,
		Strategy:          scheduleRefineNormalizeStrategy(parsed.Strategy),
		HardRequirements:  scheduleRefineUniqueNonEmpty(reqs),
		HardAssertions:    assertions,
		KeepRelativeOrder: keepOrder,
		OrderScope:        scheduleRefineNormalizeOrderScope(parsed.OrderScope),
	}
	emitStage("schedule_refine.contract.done", fmt.Sprintf("契约抽取完成：strategy=%s, keep_relative_order=%t。", st.Contract.Strategy, st.Contract.KeepRelativeOrder))
	return st, nil
}

func scheduleRefineRunPlanNode(
	ctx context.Context,
	chatModel *ark.ChatModel,
	st *ScheduleRefineState,
	emitStage func(stage, detail string),
) (*ScheduleRefineState, error) {
	if st == nil {
		return nil, fmt.Errorf("schedule refine: nil state in plan node")
	}
	if chatModel == nil {
		return nil, fmt.Errorf("schedule refine: model is nil in plan node")
	}
	if err := scheduleRefineRunPlannerNode(ctx, chatModel, st, emitStage, "initial"); err != nil {
		return st, err
	}
	return st, nil
}

func scheduleRefineRunSliceNode(
	ctx context.Context,
	st *ScheduleRefineState,
	emitStage func(stage, detail string),
) (*ScheduleRefineState, error) {
	_ = ctx
	if st == nil {
		return nil, fmt.Errorf("schedule refine: nil state in slice node")
	}
	emitStage("schedule_refine.slice.building", "正在构建本轮微调任务切片。")
	slice := scheduleRefineBuildSlicePlan(st)
	workset := scheduleRefineCollectWorksetTaskIDs(st.HybridEntries, slice, st.OriginOrderMap)
	if len(workset) == 0 {
		relaxed := slice
		relaxed.SourceDays = nil
		workset = scheduleRefineCollectWorksetTaskIDs(st.HybridEntries, relaxed, st.OriginOrderMap)
		if len(workset) > 0 {
			slice = relaxed
			emitStage("schedule_refine.slice.relaxed", "切片首次为空，已放宽来源日过滤。")
		}
	}
	if len(workset) == 0 {
		workset = scheduleRefineCollectWorksetTaskIDs(st.HybridEntries, RefineSlicePlan{}, st.OriginOrderMap)
		emitStage("schedule_refine.slice.fallback", "切片仍为空，已回退到全量 suggested 任务。")
	}
	st.SlicePlan = slice
	st.Objective = scheduleRefineCompileRefineObjective(st, slice)
	st.WorksetTaskIDs = workset
	st.WorksetCursor = 0
	st.CurrentTaskID = 0
	st.CurrentTaskAttempt = 0
	emitStage("schedule_refine.slice.done", fmt.Sprintf("切片完成：workset=%d，week_filter=%v，source_days=%v，target_days=%v，exclude_sections=%v。", len(workset), slice.WeekFilter, slice.SourceDays, slice.TargetDays, slice.ExcludeSections))
	if raw, err := json.Marshal(st.Objective); err == nil {
		emitStage("schedule_refine.objective.done", fmt.Sprintf("目标编译完成：%s", string(raw)))
	} else {
		emitStage("schedule_refine.objective.done", "目标编译完成。")
	}
	return st, nil
}

// scheduleRefineRunCompositeRouteNode 在 ReAct 之前做一次“全局复合动作直达”分流。
//
// 职责边界：
// 1. 负责识别是否命中全局复合目标（SpreadEven/MinContextSwitch）；
// 2. 负责直接调用一次复合工具并按配置重试，争取在进入 ReAct 前完成收口；
// 3. 不负责语义推理与逐任务细调，失败后仅负责切换到“禁复合”的 ReAct 兜底链路。
func scheduleRefineRunCompositeRouteNode(
	ctx context.Context,
	st *ScheduleRefineState,
	emitStage func(stage, detail string),
) (*ScheduleRefineState, error) {
	_ = ctx
	if st == nil {
		return nil, fmt.Errorf("schedule refine: nil state in route node")
	}
	scheduleRefineEnsureCompositeStateMaps(st)
	if st.CompositeRetryMax < 0 {
		st.CompositeRetryMax = defaultCompositeRetry
	}
	// 1. 先由后端判定本轮是否需要复合路由，避免把分流复杂度继续交给主 ReAct。
	// 2. 若已被上游标记为“禁复合兜底”，直接跳过该路由。
	if st.DisableCompositeTools {
		emitStage("schedule_refine.route.skip", "当前已处于禁复合兜底模式，跳过复合路由。")
		return st, nil
	}
	if strings.TrimSpace(st.RequiredCompositeTool) == "" {
		st.RequiredCompositeTool = scheduleRefineDetectRequiredCompositeTool(st)
	}
	required := scheduleRefineNormalizeCompositeToolName(st.RequiredCompositeTool)
	if required == "" {
		emitStage("schedule_refine.route.skip", "未命中全局复合目标，直接进入 ReAct 兜底链路。")
		return st, nil
	}

	taskIDs := scheduleRefineBuildCompositeRouteTaskIDs(st)
	if len(taskIDs) == 0 {
		// 1. 没有任务可用于复合规划时，复合路由无法落地。
		// 2. 直接降级到 ReAct，并明确禁用复合工具，避免循环重试同一失败路径。
		st.CompositeRouteTried = true
		st.DisableCompositeTools = true
		st.RequiredCompositeTool = ""
		st.CurrentPlan = scheduleRefineBuildFallbackPlan(st)
		st.BatchMoveAllowed = false
		emitStage("schedule_refine.route.fallback", "复合路由未获取到可执行任务，已切换到禁复合 ReAct 兜底。")
		return st, nil
	}

	totalAttempts := 1 + st.CompositeRetryMax
	emitStage("schedule_refine.route.start", fmt.Sprintf("命中复合路由：tool=%s，task_count=%d，首次1次+重试%d次。", required, len(taskIDs), st.CompositeRetryMax))
	st.CompositeRouteTried = true

	policy := scheduleRefineToolPolicy{
		// 1. 路由阶段只解决“坑位分布”。
		// 2. 顺序归位统一放在终审阶段，避免复合路由被顺序约束提前卡死。
		KeepRelativeOrder: false,
		OrderScope:        st.Contract.OrderScope,
		OriginOrderMap:    st.OriginOrderMap,
	}
	window := scheduleRefineBuildPlanningWindowFromEntries(st.HybridEntries)
	lastReason := ""

	for attempt := 1; attempt <= totalAttempts; attempt++ {
		if st.RoundUsed >= st.ExecuteMax {
			lastReason = "动作预算已耗尽，无法继续复合路由重试"
			break
		}
		call := scheduleRefineBuildCompositeRouteCall(st, required, taskIDs)
		callJSON, _ := json.Marshal(call.Params)
		emitStage("schedule_refine.route.attempt", fmt.Sprintf("复合路由第 %d/%d 次尝试：调用=%s 参数=%s。", attempt, totalAttempts, required, string(callJSON)))

		nextEntries, rawResult := dispatchScheduleRefineTool(cloneHybridEntries(st.HybridEntries), call, window, policy)
		result := scheduleRefineNormalizeToolResult(rawResult)
		st.RoundUsed++
		scheduleRefineMarkCompositeToolOutcome(st, result.Tool, result.Success)
		emitStage("schedule_refine.route.result", fmt.Sprintf("复合路由第 %d 次结果：success=%t,error_code=%s,detail=%s", attempt, result.Success, scheduleRefineFallbackText(result.ErrorCode, "NONE"), scheduleRefineTruncate(result.Result, 160)))

		if !result.Success {
			lastReason = scheduleRefineFallbackText(result.Result, scheduleRefineFallbackText(result.ErrorCode, "复合工具执行失败"))
			st.LastFailedCallSignature = scheduleRefineBuildToolCallSignature(call)
			st.ConsecutiveFailures++
			continue
		}

		st.HybridEntries = nextEntries
		st.EntriesVersion++
		st.LastFailedCallSignature = ""
		st.ConsecutiveFailures = 0
		st.ThinkingBoostArmed = false
		window = scheduleRefineBuildPlanningWindowFromEntries(st.HybridEntries)

		// 1. 复合动作成功后必须立刻做后端确定性校验，避免“调用成功但目标未达成”被误收口。
		// 2. 仅当业务目标与（若存在）复合门禁同时通过时，才允许跳过 ReAct。
		if pass, reason, unmet, applied := scheduleRefineEvaluateObjectiveDeterministic(st); applied {
			pass, reason, unmet = scheduleRefineApplyCompositeGateToIntentResult(st, pass, strings.TrimSpace(reason), unmet)
			if pass {
				st.CompositeRouteSucceeded = true
				emitStage("schedule_refine.route.pass", fmt.Sprintf("复合路由收口成功：%s", scheduleRefineTruncate(reason, 160)))
				st.ActionLogs = append(st.ActionLogs, fmt.Sprintf("复合路由成功收口：tool=%s，reason=%s", required, reason))
				return st, nil
			}
			lastReason = scheduleRefineFallbackText(strings.TrimSpace(reason), "确定性目标未达成")
			if len(unmet) > 0 {
				emitStage("schedule_refine.route.unmet", fmt.Sprintf("复合路由第 %d 次后仍未达成：%s", attempt, scheduleRefineTruncate(strings.Join(unmet, "；"), 180)))
			}
			continue
		}

		// 1. “均匀分散/最少上下文切换”这类复合目标，未必能编译成 deterministic objective；
		// 2. 只要本轮要求的复合工具已经成功执行，就允许独立复合分支直接出站并跳过 ReAct；
		// 3. 最终是否真正达标，继续交给 hard_check 统一裁决，避免“工具成功却被路由误判失败”。
		if reason, ok := scheduleRefineAllowCompositeRouteExitByToolSuccess(st, result); ok {
			st.CompositeRouteSucceeded = true
			emitStage("schedule_refine.route.handoff", scheduleRefineTruncate(reason, 180))
			st.ActionLogs = append(st.ActionLogs, fmt.Sprintf("复合路由直接出站：tool=%s，reason=%s", required, reason))
			return st, nil
		}

		lastReason = "未启用确定性目标，且复合工具门禁未满足，无法在复合路由直接出站"
	}

	// 1. 复合路由重试后仍失败，切入 ReAct 兜底并强制禁用复合工具。
	// 2. 禁用后仅允许基础工具逐任务搬运，避免再次回到复合失败路径造成震荡。
	st.DisableCompositeTools = true
	st.RequiredCompositeTool = ""
	st.CurrentPlan = scheduleRefineBuildFallbackPlan(st)
	st.BatchMoveAllowed = false
	emitStage("schedule_refine.route.fallback", fmt.Sprintf("复合路由未收口，切换禁复合 ReAct 兜底：%s", scheduleRefineTruncate(scheduleRefineFallbackText(lastReason, "复合路由达到重试上限"), 180)))
	st.ActionLogs = append(st.ActionLogs, fmt.Sprintf("复合路由失败后降级：%s", scheduleRefineFallbackText(lastReason, "无具体失败原因")))
	return st, nil
}

func scheduleRefineBuildCompositeRouteTaskIDs(st *ScheduleRefineState) []int {
	if st == nil {
		return nil
	}
	ids := scheduleRefineUniquePositiveInts(append([]int(nil), st.WorksetTaskIDs...))
	if len(ids) > 0 {
		return ids
	}
	ids = scheduleRefineCollectSourceTaskIDsForObjective(st.InitialHybridEntries, st.Objective, st.SlicePlan.WeekFilter)
	if len(ids) > 0 {
		return ids
	}
	// 兜底：从当前 suggested 中提取一份稳定任务集，避免因切片异常导致路由空跑。
	seen := make(map[int]struct{}, len(st.HybridEntries))
	out := make([]int, 0, len(st.HybridEntries))
	for _, entry := range st.HybridEntries {
		if !scheduleRefineIsMovableSuggestedTask(entry) {
			continue
		}
		if _, ok := seen[entry.TaskItemID]; ok {
			continue
		}
		seen[entry.TaskItemID] = struct{}{}
		out = append(out, entry.TaskItemID)
	}
	sort.Ints(out)
	return out
}

// scheduleRefineAllowCompositeRouteExitByToolSuccess 判断“复合工具成功后，是否允许跳过 ReAct 直接进入终审”。
//
// 步骤化说明：
// 1. 仅在当前没有 deterministic objective 时启用，避免覆盖原有“确定性验收优先”策略；
// 2. 只有本轮要求的复合工具已成功、且成功工具名与门禁一致时才放行；
// 3. 放行后并不代表最终成功，后续仍由 hard_check 做统一裁决。
func scheduleRefineAllowCompositeRouteExitByToolSuccess(st *ScheduleRefineState, result scheduleRefineReactToolResult) (string, bool) {
	if st == nil || !result.Success {
		return "", false
	}
	if strings.TrimSpace(st.Objective.Mode) != "" && strings.TrimSpace(st.Objective.Mode) != "none" {
		return "", false
	}
	required := scheduleRefineNormalizeCompositeToolName(st.RequiredCompositeTool)
	toolName := scheduleRefineNormalizeCompositeToolName(result.Tool)
	if required == "" || toolName == "" || required != toolName {
		return "", false
	}
	if !scheduleRefineIsRequiredCompositeSatisfied(st) {
		return "", false
	}
	return fmt.Sprintf("复合工具 %s 已成功执行；当前目标暂不支持确定性收口，跳过 ReAct，交由终审裁决。", required), true
}

func scheduleRefineBuildCompositeRouteCall(st *ScheduleRefineState, tool string, taskIDs []int) scheduleRefineReactToolCall {
	limit := len(taskIDs) * 6
	if limit < 12 {
		limit = 12
	}
	params := map[string]any{
		"task_item_ids": append([]int(nil), taskIDs...),
		"allow_embed":   true,
		"limit":         limit,
	}
	targetWeeks := append([]int(nil), st.Objective.TargetWeeks...)
	if len(targetWeeks) == 0 {
		targetWeeks = scheduleRefineKeysOfIntSet(scheduleRefineInferTargetWeekSet(st.SlicePlan))
	}
	if len(targetWeeks) == 0 {
		targetWeeks = append([]int(nil), st.Objective.SourceWeeks...)
	}
	if len(targetWeeks) == 1 {
		params["week"] = targetWeeks[0]
	} else if len(targetWeeks) > 1 {
		params["week_filter"] = targetWeeks
	}

	targetDays := append([]int(nil), st.Objective.TargetDays...)
	if len(targetDays) == 0 {
		targetDays = append([]int(nil), st.SlicePlan.TargetDays...)
	}
	if len(targetDays) > 0 {
		params["day_of_week"] = targetDays
	}
	if len(st.SlicePlan.ExcludeSections) > 0 {
		params["exclude_sections"] = append([]int(nil), st.SlicePlan.ExcludeSections...)
	}
	return scheduleRefineReactToolCall{
		Tool:   tool,
		Params: params,
	}
}

func scheduleRefineRunReactLoopNode(
	ctx context.Context,
	chatModel *ark.ChatModel,
	st *ScheduleRefineState,
	emitStage func(stage, detail string),
) (*ScheduleRefineState, error) {
	if st == nil {
		return nil, fmt.Errorf("schedule refine: nil state in react loop node")
	}
	if chatModel == nil {
		return nil, fmt.Errorf("schedule refine: model is nil in react loop node")
	}
	if st.CompositeRouteSucceeded {
		emitStage("schedule_refine.react.skip", "复合路由已收口成功，跳过 ReAct 兜底循环。")
		return st, nil
	}
	if len(st.HybridEntries) == 0 {
		st.ActionLogs = append(st.ActionLogs, "无可微调条目，跳过动作循环。")
		return st, nil
	}
	if len(st.WorksetTaskIDs) == 0 {
		st.ActionLogs = append(st.ActionLogs, "workset 为空，跳过动作循环。")
		return st, nil
	}
	if st.PerTaskBudget <= 0 {
		st.PerTaskBudget = defaultPerTaskBudget
	}
	if st.ExecuteMax <= 0 {
		st.ExecuteMax = defaultExecuteMax
	}
	if st.ReplanMax < 0 {
		st.ReplanMax = defaultReplanMax
	}
	if st.RepairReserve < 0 {
		st.RepairReserve = 0
	}
	st.MaxRounds = st.ExecuteMax + st.RepairReserve
	if st.TaskActionUsed == nil {
		st.TaskActionUsed = make(map[int]int)
	}
	if st.SeenSlotQueries == nil {
		st.SeenSlotQueries = make(map[string]struct{})
	}
	scheduleRefineEnsureCompositeStateMaps(st)
	if st.DisableCompositeTools {
		st.RequiredCompositeTool = ""
		emitStage("schedule_refine.react.fallback_mode", "当前为禁复合兜底模式：仅允许基础工具逐任务调整。")
	} else if strings.TrimSpace(st.RequiredCompositeTool) == "" {
		st.RequiredCompositeTool = scheduleRefineDetectRequiredCompositeTool(st)
	}
	if strings.TrimSpace(st.CurrentPlan.Summary) == "" {
		st.CurrentPlan = scheduleRefineApplyCompositeHardConditionToPlan(st, scheduleRefineBuildFallbackPlan(st))
		st.BatchMoveAllowed = scheduleRefineShouldAllowBatchMove(st.CurrentPlan)
	}

	window := scheduleRefineBuildPlanningWindowFromEntries(st.HybridEntries)
	sourceWeekSet := scheduleRefineInferSourceWeekSet(st.SlicePlan)
	policy := scheduleRefineToolPolicy{
		// 1. 执行期不再用顺序约束卡住 Move/Swap；
		// 2. LLM 只负责把坑位排好，顺序由后端在收口阶段统一归位。
		KeepRelativeOrder: false,
		OrderScope:        st.Contract.OrderScope,
		OriginOrderMap:    st.OriginOrderMap,
	}
	emitStage(
		"schedule_refine.react.start",
		fmt.Sprintf(
			"开始执行单任务微步 ReAct，workset=%d，per_task_budget=%d，execute_max=%d，replan_max=%d，required_composite=%s，required_success=%t。",
			len(st.WorksetTaskIDs),
			st.PerTaskBudget,
			st.ExecuteMax,
			st.ReplanMax,
			scheduleRefineFallbackText(scheduleRefineNormalizeCompositeToolName(st.RequiredCompositeTool), "无"),
			scheduleRefineIsRequiredCompositeSatisfied(st),
		),
	)

outer:
	for st.WorksetCursor < len(st.WorksetTaskIDs) && st.RoundUsed < st.ExecuteMax {
		// 1. 每次取下一个任务前先做一次全局目标短路判断。
		// 2. 目标已满足时，直接结束整个 workset 循环，避免“任务6~10 空转”。
		if pass, reason, _, applied := scheduleRefineEvaluateObjectiveDeterministic(st); applied && pass {
			if scheduleRefineIsRequiredCompositeSatisfied(st) {
				emitStage("schedule_refine.react.short_circuit", fmt.Sprintf("全局目标已满足，提前结束任务循环：%s", scheduleRefineTruncate(reason, 160)))
				st.ActionLogs = append(st.ActionLogs, fmt.Sprintf("全局目标提前达成，触发短路结束：%s", reason))
				break
			}
			st.ActionLogs = append(st.ActionLogs, fmt.Sprintf("全局目标看似达成但未满足复合工具门禁：required=%s", scheduleRefineNormalizeCompositeToolName(st.RequiredCompositeTool)))
		}
		taskID := st.WorksetTaskIDs[st.WorksetCursor]
		current, ok := scheduleRefineFindSuggestedEntryByTaskID(st.HybridEntries, taskID)
		if !ok {
			st.WorksetCursor++
			continue
		}
		if len(sourceWeekSet) > 0 {
			if _, inSourceWeek := sourceWeekSet[current.Week]; !inSourceWeek {
				emitStage("schedule_refine.react.task_skip_scope", fmt.Sprintf("任务 id=%d 当前位于 W%d，不在来源周范围，已跳过。", taskID, current.Week))
				st.WorksetCursor++
				continue
			}
		}
		st.CurrentTaskID = taskID
		st.CurrentTaskAttempt = 0
		emitStage("schedule_refine.react.task_start", fmt.Sprintf("开始处理任务 %d/%d：id=%d，%s。", st.WorksetCursor+1, len(st.WorksetTaskIDs), taskID, strings.TrimSpace(current.Name)))

		taskDone := false
		for st.CurrentTaskAttempt < st.PerTaskBudget && st.RoundUsed < st.ExecuteMax {
			// 1. 每轮开头先刷新“当前任务”的最新位置，避免模型基于旧坐标决策。
			// 2. 若该任务已满足切片目标（例如“已从周末迁出到工作日”），则直接收口当前任务。
			latest, exists := scheduleRefineFindSuggestedEntryByTaskID(st.HybridEntries, taskID)
			if !exists {
				taskDone = true
				emitStage("schedule_refine.react.task_auto_done", fmt.Sprintf("任务 id=%d 已不在 suggested 列表，视为当前任务已完成。", taskID))
				st.ActionLogs = append(st.ActionLogs, fmt.Sprintf("任务 id=%d 自动完成：任务条目已不再可调 suggested。", taskID))
				break
			}
			current = latest
			if scheduleRefineIsCurrentTaskSatisfiedBySlice(current, st.SlicePlan) {
				// 1. 自动收口前必须通过复合工具门禁。
				// 2. 这样可避免“切片已满足但未执行必需复合工具”直接跳过执行阶段。
				if scheduleRefineIsRequiredCompositeSatisfied(st) {
					taskDone = true
					emitStage("schedule_refine.react.task_auto_done", fmt.Sprintf("任务 id=%d 已满足切片目标，自动收口并切换下一任务。", taskID))
					st.ActionLogs = append(st.ActionLogs, fmt.Sprintf("任务 id=%d 自动完成：已满足切片目标。", taskID))
					break
				}
				emitStage("schedule_refine.react.task_auto_done_blocked", fmt.Sprintf("任务 id=%d 虽满足切片目标，但复合工具门禁未通过，继续执行。", taskID))
				st.ActionLogs = append(st.ActionLogs, fmt.Sprintf("任务 id=%d 阻止自动收口：required_composite=%s 尚未成功。", taskID, scheduleRefineFallbackText(scheduleRefineNormalizeCompositeToolName(st.RequiredCompositeTool), "无")))
			}

			round := st.RoundUsed + 1
			remainingAction := st.ExecuteMax - st.RoundUsed
			remainingTotal := st.MaxRounds - st.RoundUsed
			useThinking, reason := scheduleRefineShouldEnableRecoveryThinking(st)
			st.CurrentTaskAttempt++
			emitStage("schedule_refine.react.round_start", fmt.Sprintf("第 %d 轮微调开始（任务id=%d，第 %d/%d 次尝试），动作剩余=%d，总剩余=%d。", round, taskID, st.CurrentTaskAttempt, st.PerTaskBudget, remainingAction, remainingTotal))
			if useThinking {
				emitStage("schedule_refine.react.reasoning_switch", fmt.Sprintf("第 %d 轮已启用恢复态 thinking：%s", round, reason))
			}

			userPrompt := scheduleRefineBuildMicroReactUserPrompt(st, current, remainingAction, remainingTotal)
			raw, err := scheduleRefineCallModelText(ctx, chatModel, agentprompt.ScheduleRefineReactPrompt, userPrompt, useThinking, reactMaxTokens, 0)
			if err != nil {
				errDetail := scheduleRefineFormatRoundModelErrorDetail(round, err, ctx)
				st.ActionLogs = append(st.ActionLogs, errDetail)
				emitStage("schedule_refine.react.round_error", errDetail)
				if errors.Is(err, context.DeadlineExceeded) && st.RoundUsed > 0 {
					st.WorksetCursor = len(st.WorksetTaskIDs)
					break
				}
				return st, err
			}
			scheduleRefineEmitModelRawDebug(emitStage, fmt.Sprintf("react.round.%d.plan", round), raw)
			parsed, parseErr := scheduleRefineParseReactOutputWithRetryOnce(ctx, chatModel, userPrompt, raw, round, emitStage, st)
			if parseErr != nil {
				return st, parseErr
			}

			observation := ReactRoundObservation{
				Round:     round,
				GoalCheck: strings.TrimSpace(parsed.GoalCheck),
				Decision:  strings.TrimSpace(parsed.Decision),
			}
			emitStage("schedule_refine.react.plan", scheduleRefineFormatReactPlanStageDetail(round, parsed, remainingAction, useThinking))
			emitStage("schedule_refine.react.need_info", scheduleRefineFormatReactNeedInfoStageDetail(round, parsed.MissingInfo))

			if parsed.Done {
				allowDone := scheduleRefineIsCurrentTaskSatisfiedBySlice(current, st.SlicePlan)
				if allowDone && !scheduleRefineIsRequiredCompositeSatisfied(st) {
					allowDone = false
				}
				if !allowDone {
					if pass, _, _, applied := scheduleRefineEvaluateObjectiveDeterministic(st); applied && pass && scheduleRefineIsRequiredCompositeSatisfied(st) {
						allowDone = true
					}
				}
				if !allowDone {
					observation.Reflect = fmt.Sprintf("模型返回 done=true，但任务 id=%d 尚未满足切片目标或复合工具门禁未通过，继续执行。", taskID)
					st.ObservationHistory = append(st.ObservationHistory, observation)
					emitStage("schedule_refine.react.reflect", scheduleRefineFormatReactReflectStageDetail(round, observation.Reflect))
					st.ActionLogs = append(st.ActionLogs, fmt.Sprintf("任务 id=%d 拒绝提前 done：当前任务未满足目标。", taskID))
					continue
				}
				reasonText := scheduleRefineFallbackText(strings.TrimSpace(parsed.Summary), "模型判定当前任务已满足目标。")
				observation.Reflect = reasonText
				st.ObservationHistory = append(st.ObservationHistory, observation)
				st.ActionLogs = append(st.ActionLogs, fmt.Sprintf("任务 id=%d 完成：%s", taskID, reasonText))
				emitStage("schedule_refine.react.reflect", scheduleRefineFormatReactReflectStageDetail(round, observation.Reflect))
				taskDone = true
				break
			}

			call, warn := pickSingleScheduleRefineToolCall(parsed.ToolCalls)
			if warn != "" {
				emitStage("schedule_refine.react.round_warn", fmt.Sprintf("第 %d 轮告警：%s", round, warn))
			}
			if call == nil {
				observation.Reflect = "本轮未生成可执行工具动作。"
				st.ObservationHistory = append(st.ObservationHistory, observation)
				emitStage("schedule_refine.react.reflect", scheduleRefineFormatReactReflectStageDetail(round, observation.Reflect))
				break
			}
			normalizedCall := scheduleRefineCanonicalizeToolCall(*call)
			call = &normalizedCall
			emitStage("schedule_refine.react.tool_call", scheduleRefineFormatToolCallStageDetail(round, *call, remainingAction))

			callSignature := scheduleRefineBuildToolCallSignature(*call)
			taskIDs := scheduleRefineListTaskIDsFromToolCall(*call)
			if blockedResult, blocked := scheduleRefinePrecheckCurrentTaskOwnership(*call, taskIDs, taskID); blocked {
				if stop, err := scheduleRefineHandleBlockedToolResult(ctx, chatModel, st, emitStage, round, parsed, call, callSignature, blockedResult, &observation); err != nil {
					return st, err
				} else if stop {
					taskDone = true
					break
				}
				continue
			}
			if blockedResult, blocked := scheduleRefinePrecheckToolCallPolicy(st, *call, taskIDs); blocked {
				if stop, err := scheduleRefineHandleBlockedToolResult(ctx, chatModel, st, emitStage, round, parsed, call, callSignature, blockedResult, &observation); err != nil {
					return st, err
				} else if stop {
					taskDone = true
					break
				}
				continue
			}
			if scheduleRefineIsRepeatedFailedCall(st, callSignature) {
				repeat := scheduleRefineReactToolResult{Tool: strings.TrimSpace(call.Tool), Success: false, ErrorCode: "REPEAT_FAILED_ACTION", Result: "重复失败动作：与上一轮失败动作完全相同，请更换目标时段或改用 Swap。"}
				if stop, err := scheduleRefineHandleBlockedToolResult(ctx, chatModel, st, emitStage, round, parsed, call, callSignature, repeat, &observation); err != nil {
					return st, err
				} else if stop {
					taskDone = true
					break
				}
				continue
			}

			for _, id := range taskIDs {
				st.TaskActionUsed[id]++
			}
			nextEntries, rawResult := dispatchScheduleRefineTool(cloneHybridEntries(st.HybridEntries), *call, window, policy)
			result := scheduleRefineNormalizeToolResult(rawResult)
			st.RoundUsed++
			scheduleRefineMarkCompositeToolOutcome(st, result.Tool, result.Success)

			observation.ToolName = strings.TrimSpace(result.Tool)
			observation.ToolParams = scheduleRefineCloneToolParams(call.Params)
			observation.ToolSuccess = result.Success
			observation.ToolErrorCode = strings.TrimSpace(result.ErrorCode)
			observation.ToolResult = strings.TrimSpace(result.Result)
			postReflectText, _, shouldStop := scheduleRefineRunPostReflectAfterTool(ctx, chatModel, st, round, parsed, call, result, emitStage)
			observation.Reflect = postReflectText
			st.ObservationHistory = append(st.ObservationHistory, observation)

			emitStage("schedule_refine.react.tool_result", scheduleRefineFormatToolResultStageDetail(round, result, st.RoundUsed, st.MaxRounds))
			emitStage("schedule_refine.react.reflect", scheduleRefineFormatReactReflectStageDetail(round, observation.Reflect))
			if result.Success {
				st.HybridEntries = nextEntries
				window = scheduleRefineBuildPlanningWindowFromEntries(st.HybridEntries)
				if scheduleRefineIsMutatingToolName(result.Tool) {
					st.EntriesVersion++
				}
				st.LastFailedCallSignature = ""
				st.ConsecutiveFailures = 0
				st.ThinkingBoostArmed = false
				// 1. 动作成功后立即尝试全局短路，避免继续拉着后续任务空转。
				// 2. 只要 deterministic 目标达成，直接收口整个 ReAct 循环。
				if pass, reason, _, applied := scheduleRefineEvaluateObjectiveDeterministic(st); applied && pass {
					if scheduleRefineIsRequiredCompositeSatisfied(st) {
						emitStage("schedule_refine.react.short_circuit", fmt.Sprintf("动作后全局目标达成，提前结束任务循环：%s", scheduleRefineTruncate(reason, 160)))
						st.ActionLogs = append(st.ActionLogs, fmt.Sprintf("动作后全局目标达成，触发短路结束：%s", reason))
						taskDone = true
						break outer
					}
					st.ActionLogs = append(st.ActionLogs, fmt.Sprintf("动作后目标达成但复合工具门禁未通过：required=%s", scheduleRefineNormalizeCompositeToolName(st.RequiredCompositeTool)))
				}
				if latest, exists := scheduleRefineFindSuggestedEntryByTaskID(st.HybridEntries, taskID); exists {
					current = latest
					if scheduleRefineIsCurrentTaskSatisfiedBySlice(current, st.SlicePlan) {
						if scheduleRefineIsRequiredCompositeSatisfied(st) {
							taskDone = true
							emitStage("schedule_refine.react.task_auto_done", fmt.Sprintf("任务 id=%d 动作后已满足切片目标，自动结束当前任务。", taskID))
							st.ActionLogs = append(st.ActionLogs, fmt.Sprintf("任务 id=%d 自动完成：动作后已满足切片目标。", taskID))
							break
						}
						emitStage("schedule_refine.react.task_auto_done_blocked", fmt.Sprintf("任务 id=%d 动作后满足切片目标，但复合工具门禁未通过，继续执行。", taskID))
						st.ActionLogs = append(st.ActionLogs, fmt.Sprintf("任务 id=%d 阻止动作后自动收口：required_composite=%s 尚未成功。", taskID, scheduleRefineFallbackText(scheduleRefineNormalizeCompositeToolName(st.RequiredCompositeTool), "无")))
					}
				} else {
					taskDone = true
					emitStage("schedule_refine.react.task_auto_done", fmt.Sprintf("任务 id=%d 动作后已不在 suggested 列表，自动结束当前任务。", taskID))
					st.ActionLogs = append(st.ActionLogs, fmt.Sprintf("任务 id=%d 自动完成：动作后不再可调。", taskID))
					break
				}
			} else {
				st.LastFailedCallSignature = callSignature
				st.ConsecutiveFailures++
				if scheduleRefineShouldTriggerReplan(st, result) {
					if replanned, err := scheduleRefineTryReplan(ctx, chatModel, st, emitStage); err != nil {
						return st, err
					} else if replanned {
						continue
					}
				}
			}
			if shouldStop {
				// 1. 模型建议 should_stop 只作为“候选中断信号”，必须经后端目标校验确认。
				// 2. 若全局目标未达成，则继续本地循环，避免模型误停。
				if pass, reason, _, applied := scheduleRefineEvaluateObjectiveDeterministic(st); applied && pass {
					if scheduleRefineIsRequiredCompositeSatisfied(st) {
						emitStage("schedule_refine.react.short_circuit", fmt.Sprintf("模型建议停止且全局目标达成，提前收口：%s", scheduleRefineTruncate(reason, 160)))
						st.ActionLogs = append(st.ActionLogs, fmt.Sprintf("模型建议停止且目标达成，触发短路结束：%s", reason))
						taskDone = true
						break outer
					}
					st.ActionLogs = append(st.ActionLogs, fmt.Sprintf("模型建议停止但复合工具门禁未通过：required=%s", scheduleRefineNormalizeCompositeToolName(st.RequiredCompositeTool)))
				}
			}
		}

		emitStage("schedule_refine.react.task_done", fmt.Sprintf("任务 id=%d 处理完成：status=%s。", taskID, scheduleRefineTaskProgressLabel(taskDone, st.CurrentTaskAttempt, st.PerTaskBudget)))
		st.WorksetCursor++
		st.CurrentTaskID = 0
		st.CurrentTaskAttempt = 0
	}
	emitStage("schedule_refine.react.done", fmt.Sprintf("单任务微步 ReAct 结束：已执行轮次=%d，重规划次数=%d，已处理任务=%d/%d。", st.RoundUsed, st.ReplanUsed, st.WorksetCursor, len(st.WorksetTaskIDs)))
	return st, nil
}

func scheduleRefineRunHardCheckNode(
	ctx context.Context,
	chatModel *ark.ChatModel,
	st *ScheduleRefineState,
	emitStage func(stage, detail string),
) (*ScheduleRefineState, error) {
	if st == nil {
		return nil, fmt.Errorf("schedule refine: nil state in hard check node")
	}
	if chatModel == nil {
		return nil, fmt.Errorf("schedule refine: model is nil in hard check node")
	}
	emitStage("schedule_refine.hard_check.start", "正在执行终审硬校验。")
	// 1. 先锁定“业务目标是否达成”的判定结果（未排序前）。
	// 2. 后续顺序归位仅用于最终展示与顺序一致性，不得反向改变业务目标成败。
	intentPassLocked, intentReasonLocked, intentUnmetLocked := scheduleRefineEvaluateIntentForJudgement(ctx, chatModel, st, emitStage)
	emitStage("schedule_refine.hard_check.intent_locked", fmt.Sprintf("终审业务目标已锁定：pass=%t，reason=%s", intentPassLocked, scheduleRefineTruncate(intentReasonLocked, 120)))
	if changed, skipped := scheduleRefineTryNormalizeMovableTaskOrderByOrigin(st); skipped {
		emitStage("schedule_refine.hard_check.order_normalized", "已跳过顺序归位：MinContextSwitch 结果需要保留重排后的任务顺序。")
	} else if changed {
		emitStage("schedule_refine.hard_check.order_normalized", "已在终审前按 origin_rank 对坑位做顺序归位。")
	}
	report := scheduleRefineEvaluateHardChecks(ctx, chatModel, st, emitStage)
	report.IntentPassed = intentPassLocked
	report.IntentReason = intentReasonLocked
	report.IntentUnmet = append([]string(nil), intentUnmetLocked...)
	st.HardCheck = report
	if report.PhysicsPassed && report.OrderPassed && report.IntentPassed {
		emitStage("schedule_refine.hard_check.pass", "终审通过。")
		return st, nil
	}
	if st.RoundUsed >= st.MaxRounds {
		emitStage("schedule_refine.hard_check.fail", "终审未通过，且动作预算已耗尽，无法继续修复。")
		return st, nil
	}
	emitStage("schedule_refine.hard_check.repairing", "终审未通过，正在尝试一次修复动作。")
	st.HardCheck.RepairTried = true
	if err := scheduleRefineRunSingleRepairAction(ctx, chatModel, st, emitStage); err != nil {
		st.ActionLogs = append(st.ActionLogs, fmt.Sprintf("修复动作失败：%v", err))
		emitStage("schedule_refine.hard_check.fail", "修复动作失败，保留当前方案。")
		return st, nil
	}
	intentPassLocked, intentReasonLocked, intentUnmetLocked = scheduleRefineEvaluateIntentForJudgement(ctx, chatModel, st, emitStage)
	emitStage("schedule_refine.hard_check.intent_locked", fmt.Sprintf("修复后业务目标已锁定：pass=%t，reason=%s", intentPassLocked, scheduleRefineTruncate(intentReasonLocked, 120)))
	if changed, skipped := scheduleRefineTryNormalizeMovableTaskOrderByOrigin(st); skipped {
		emitStage("schedule_refine.hard_check.order_normalized", "修复后跳过顺序归位：MinContextSwitch 结果需要保留重排后的任务顺序。")
	} else if changed {
		emitStage("schedule_refine.hard_check.order_normalized", "修复后已按 origin_rank 对坑位做顺序归位。")
	}
	report = scheduleRefineEvaluateHardChecks(ctx, chatModel, st, emitStage)
	report.IntentPassed = intentPassLocked
	report.IntentReason = intentReasonLocked
	report.IntentUnmet = append([]string(nil), intentUnmetLocked...)
	report.RepairTried = true
	st.HardCheck = report
	if report.PhysicsPassed && report.OrderPassed && report.IntentPassed {
		emitStage("schedule_refine.hard_check.pass", "修复后终审通过。")
		return st, nil
	}
	emitStage("schedule_refine.hard_check.fail", "修复后仍未完全满足要求，已返回当前最优结果。")
	return st, nil
}

func scheduleRefineRunSummaryNode(
	ctx context.Context,
	chatModel *ark.ChatModel,
	st *ScheduleRefineState,
	emitStage func(stage, detail string),
) (*ScheduleRefineState, error) {
	if st == nil {
		return nil, fmt.Errorf("schedule refine: nil state in summary node")
	}
	if chatModel == nil {
		return nil, fmt.Errorf("schedule refine: model is nil in summary node")
	}
	emitStage("schedule_refine.summary.generating", "正在生成微调结果总结。")
	scheduleRefineUpdateAllocatedItemsFromEntries(st)
	st.CandidatePlans = scheduleRefineHybridEntriesToWeekSchedules(st.HybridEntries)
	reportJSON, _ := json.Marshal(st.HardCheck)
	contractJSON, _ := json.Marshal(st.Contract)
	userPrompt := fmt.Sprintf("用户请求=%s\n契约=%s\n终审报告=%s\n动作日志=%s", strings.TrimSpace(st.UserMessage), string(contractJSON), string(reportJSON), scheduleRefineSummarizeActionLogs(st.ActionLogs, 24))
	raw, err := scheduleRefineCallModelText(ctx, chatModel, agentprompt.ScheduleRefineSummaryPrompt, userPrompt, false, 280, 0.35)
	summary := strings.TrimSpace(raw)
	if err == nil {
		scheduleRefineEmitModelRawDebug(emitStage, "summary", raw)
	}
	if err != nil || summary == "" {
		if FinalHardCheckPassed(st) {
			summary = fmt.Sprintf("微调已完成，共执行 %d 轮动作，方案已通过终审。", st.RoundUsed)
		} else {
			summary = fmt.Sprintf("已完成微调并返回当前最优结果（执行 %d 轮动作）。终审仍有未满足项：%s。", st.RoundUsed, scheduleRefineFallbackText(st.HardCheck.IntentReason, "请进一步明确微调目标"))
		}
	}
	summary = scheduleRefineAlignSummaryWithHardCheck(st, summary)
	st.FinalSummary = summary
	// 1. Completed 只代表“最终终审已通过”，不再把“链路执行完毕”误写成成功；
	// 2. 这样外层持久化与展示层可以准确区分“已通过方案”与“当前最优但未达标方案”；
	// 3. 若只是返回 best-effort 结果，FinalSummary 仍会保留，但 Completed=false。
	st.Completed = FinalHardCheckPassed(st)
	emitStage("schedule_refine.summary.done", "微调总结已生成。")
	return st, nil
}

func scheduleRefineEvaluateHardChecks(ctx context.Context, chatModel *ark.ChatModel, st *ScheduleRefineState, emitStage func(stage, detail string)) HardCheckReport {
	report := HardCheckReport{}
	report.PhysicsIssues = scheduleRefinePhysicsCheck(st.HybridEntries, len(st.AllocatedItems))
	report.PhysicsPassed = len(report.PhysicsIssues) == 0
	// 1. 顺序校验默认开启：即便执行期放开顺序限制，终审也要验证“后端归位”后的顺序正确性。
	// 2. 但 MinContextSwitch 成功后，重排后的顺序本身就是业务目标，不能再拿 origin_rank 反向判错。
	// 3. 当 origin_order_map 为空时同样降级跳过，避免无基线时误报。
	needOrderCheck := len(st.OriginOrderMap) > 0 && !scheduleRefineShouldSkipOrderConstraintCheck(st)
	report.OrderIssues = scheduleRefineValidateRelativeOrder(st.HybridEntries, scheduleRefineToolPolicy{
		KeepRelativeOrder: needOrderCheck,
		OrderScope:        st.Contract.OrderScope,
		OriginOrderMap:    st.OriginOrderMap,
	})
	report.OrderPassed = len(report.OrderIssues) == 0

	// 1. 优先使用“契约编译后”的确定性终审，执行与终审共用同一份目标约束。
	// 2. 仅当目标约束不可判定时，才回退语义终审兜底。
	if pass, reason, unmet, applied := scheduleRefineEvaluateObjectiveDeterministic(st); applied {
		pass, reason, unmet = scheduleRefineApplyCompositeGateToIntentResult(st, pass, reason, unmet)
		report.IntentPassed = pass
		report.IntentReason = strings.TrimSpace(reason)
		report.IntentUnmet = append([]string(nil), unmet...)
		return report
	}

	review, err := scheduleRefineRunSemanticReview(ctx, chatModel, st, emitStage)
	if err != nil {
		report.IntentPassed = false
		report.IntentReason = fmt.Sprintf("语义校验失败：%v", err)
		report.IntentUnmet = []string{"语义校验阶段异常"}
		return report
	}
	pass, reason, unmet := scheduleRefineApplyCompositeGateToIntentResult(st, review.Pass, strings.TrimSpace(review.Reason), review.Unmet)
	report.IntentPassed = pass
	report.IntentReason = strings.TrimSpace(reason)
	report.IntentUnmet = append([]string(nil), unmet...)
	return report
}

// scheduleRefineEvaluateIntentForJudgement 在“最终排序前”计算业务目标是否达成。
//
// 说明：
// 1. 优先走 deterministic objective；
// 2. objective 不可判定时退回语义 review；
// 3. 返回值会在 hard_check 中被锁定，避免后置排序反向干扰业务目标判定。
func scheduleRefineEvaluateIntentForJudgement(
	ctx context.Context,
	chatModel *ark.ChatModel,
	st *ScheduleRefineState,
	emitStage func(stage, detail string),
) (pass bool, reason string, unmet []string) {
	if pass, reason, unmet, applied := scheduleRefineEvaluateObjectiveDeterministic(st); applied {
		pass, reason, unmet = scheduleRefineApplyCompositeGateToIntentResult(st, pass, strings.TrimSpace(reason), unmet)
		return pass, strings.TrimSpace(reason), append([]string(nil), unmet...)
	}
	review, err := scheduleRefineRunSemanticReview(ctx, chatModel, st, emitStage)
	if err != nil {
		return false, fmt.Sprintf("语义校验失败：%v", err), []string{"语义校验阶段异常"}
	}
	pass, reason, unmet = scheduleRefineApplyCompositeGateToIntentResult(st, review.Pass, strings.TrimSpace(review.Reason), review.Unmet)
	return pass, strings.TrimSpace(reason), append([]string(nil), unmet...)
}

// scheduleRefineCompileRefineObjective 把自然语言契约编译为“可执行且可校验”的目标参数。
func scheduleRefineCompileRefineObjective(st *ScheduleRefineState, slice RefineSlicePlan) RefineObjective {
	obj := RefineObjective{
		Mode:            "none",
		SourceWeeks:     scheduleRefineKeysOfIntSet(scheduleRefineInferSourceWeekSet(slice)),
		TargetWeeks:     scheduleRefineKeysOfIntSet(scheduleRefineInferTargetWeekSet(slice)),
		SourceDays:      scheduleRefineUniquePositiveInts(append([]int(nil), slice.SourceDays...)),
		TargetDays:      scheduleRefineUniquePositiveInts(append([]int(nil), slice.TargetDays...)),
		ExcludeSections: scheduleRefineUniquePositiveInts(append([]int(nil), slice.ExcludeSections...)),
	}
	// 1. 若契约断言显式给出来源/目标周，优先回填到 objective；
	// 2. 避免后续终审只能依赖自然语言猜测。
	for _, assertion := range st.Contract.HardAssertions {
		if assertion.Week > 0 && len(obj.SourceWeeks) == 0 {
			obj.SourceWeeks = []int{assertion.Week}
		}
		if assertion.TargetWeek > 0 && len(obj.TargetWeeks) == 0 {
			obj.TargetWeeks = []int{assertion.TargetWeek}
		}
	}

	if len(obj.SourceWeeks) == 0 && len(slice.WeekFilter) == 1 && slice.WeekFilter[0] > 0 {
		obj.SourceWeeks = []int{slice.WeekFilter[0]}
	}
	if len(obj.TargetWeeks) == 0 && len(slice.WeekFilter) == 1 && (len(obj.SourceDays) > 0 || len(obj.TargetDays) > 0) {
		obj.TargetWeeks = []int{slice.WeekFilter[0]}
	}

	// 来源范围为空时无法构造目标，交给语义终审兜底。
	if len(obj.SourceWeeks) == 0 && len(obj.SourceDays) == 0 {
		obj.Reason = "来源范围为空，未启用确定性目标。"
		return obj
	}
	// 目标范围为空时同样不启用确定性目标。
	if len(obj.TargetWeeks) == 0 && len(obj.TargetDays) == 0 {
		obj.Reason = "目标范围为空，未启用确定性目标。"
		return obj
	}

	sourceTaskIDs := scheduleRefineCollectSourceTaskIDsForObjective(st.InitialHybridEntries, obj, slice.WeekFilter)
	obj.BaselineSourceTaskCount = len(sourceTaskIDs)

	halfIntent := scheduleRefineHasHalfTransferIntent(st)
	if halfIntent && len(obj.SourceWeeks) > 0 && len(obj.TargetWeeks) > 0 && !scheduleRefineIsSameWeeks(obj.SourceWeeks, obj.TargetWeeks) {
		obj.Mode = "move_ratio"
		obj.RequiredMoveMin = obj.BaselineSourceTaskCount / 2
		obj.RequiredMoveMax = (obj.BaselineSourceTaskCount + 1) / 2
		obj.Reason = "检测到“半数迁移”意图，按比例目标执行与终审。"
		return obj
	}

	obj.Mode = "move_all"
	obj.RequiredMoveMin = obj.BaselineSourceTaskCount
	obj.RequiredMoveMax = obj.BaselineSourceTaskCount
	obj.Reason = "默认按来源范围任务全部进入目标范围执行与终审。"
	return obj
}

// scheduleRefineEvaluateObjectiveDeterministic 基于编译后的目标做确定性终审。
func scheduleRefineEvaluateObjectiveDeterministic(st *ScheduleRefineState) (pass bool, reason string, unmet []string, applied bool) {
	if st == nil {
		return false, "", nil, false
	}
	obj := st.Objective
	if strings.TrimSpace(obj.Mode) == "" || strings.TrimSpace(obj.Mode) == "none" {
		return false, "", nil, false
	}

	sourceTaskIDs := scheduleRefineCollectSourceTaskIDsForObjective(st.InitialHybridEntries, obj, st.SlicePlan.WeekFilter)
	if len(sourceTaskIDs) == 0 {
		return true, "确定性校验通过：来源范围无可调任务。", nil, true
	}

	byTaskID := scheduleRefineBuildMovableTaskIndex(st.HybridEntries)
	movedCount := 0
	violations := make([]string, 0, 8)
	for _, taskID := range sourceTaskIDs {
		entries := byTaskID[taskID]
		if len(entries) == 0 {
			violations = append(violations, fmt.Sprintf("任务id=%d 未在结果中找到可移动条目", taskID))
			continue
		}
		if len(entries) > 1 {
			violations = append(violations, fmt.Sprintf("任务id=%d 命中 %d 条可移动条目，状态不唯一", taskID, len(entries)))
			continue
		}
		entry := entries[0]
		moved, why := scheduleRefineIsTaskMovedIntoObjectiveTarget(entry, obj)
		if moved {
			movedCount++
			continue
		}
		if obj.Mode == "move_all" {
			violations = append(violations, fmt.Sprintf("任务id=%d 未满足目标范围：%s", taskID, why))
			continue
		}
		// 比例模式下，允许部分任务不迁移；但若任务落在来源/目标之外，视为异常。
		if !scheduleRefineIsTaskInObjectiveSource(entry, obj) {
			violations = append(violations, fmt.Sprintf("任务id=%d 既不在来源也不在目标范围（W%dD%d）", taskID, entry.Week, entry.DayOfWeek))
		}
	}

	if movedCount < obj.RequiredMoveMin || movedCount > obj.RequiredMoveMax {
		violations = append(violations, fmt.Sprintf("迁移数量未达标：要求在[%d,%d]，实际=%d", obj.RequiredMoveMin, obj.RequiredMoveMax, movedCount))
	}

	if len(violations) == 0 {
		return true, fmt.Sprintf("确定性校验通过：迁移数量达标（%d/%d）。", movedCount, len(sourceTaskIDs)), nil, true
	}
	return false, fmt.Sprintf("确定性校验未通过：仍有 %d 项约束未满足。", len(violations)), violations, true
}

func scheduleRefineCollectSourceTaskIDsForObjective(entries []model.HybridScheduleEntry, obj RefineObjective, fallbackWeekFilter []int) []int {
	if len(entries) == 0 {
		return nil
	}
	sourceWeekSet := scheduleRefineIntSliceToWeekSet(obj.SourceWeeks)
	sourceDaySet := scheduleRefineIntSliceToDaySet(obj.SourceDays)
	fallbackWeekSet := scheduleRefineIntSliceToWeekSet(fallbackWeekFilter)

	seen := make(map[int]struct{}, len(entries))
	ids := make([]int, 0, len(entries))
	for _, entry := range entries {
		if !scheduleRefineIsMovableSuggestedTask(entry) {
			continue
		}
		if len(sourceWeekSet) > 0 {
			if _, ok := sourceWeekSet[entry.Week]; !ok {
				continue
			}
		} else if len(fallbackWeekSet) > 0 {
			if _, ok := fallbackWeekSet[entry.Week]; !ok {
				continue
			}
		}
		if len(sourceDaySet) > 0 {
			if _, ok := sourceDaySet[entry.DayOfWeek]; !ok {
				continue
			}
		}
		if _, exists := seen[entry.TaskItemID]; exists {
			continue
		}
		seen[entry.TaskItemID] = struct{}{}
		ids = append(ids, entry.TaskItemID)
	}
	sort.Ints(ids)
	return ids
}

func scheduleRefineBuildMovableTaskIndex(entries []model.HybridScheduleEntry) map[int][]model.HybridScheduleEntry {
	index := make(map[int][]model.HybridScheduleEntry, len(entries))
	for _, entry := range entries {
		if !scheduleRefineIsMovableSuggestedTask(entry) {
			continue
		}
		index[entry.TaskItemID] = append(index[entry.TaskItemID], entry)
	}
	return index
}

func scheduleRefineHasHalfTransferIntent(st *ScheduleRefineState) bool {
	if st == nil {
		return false
	}
	if scheduleRefineHasHalfTransferAssertion(st.Contract.HardAssertions) {
		return true
	}
	joined := strings.ToLower(strings.Join(append([]string{st.UserMessage, st.Contract.Intent}, st.Contract.HardRequirements...), " "))
	for _, key := range []string{"一半", "半数", "对半", "50%"} {
		if strings.Contains(joined, key) {
			return true
		}
	}
	return false
}

func scheduleRefineHasHalfTransferAssertion(assertions []RefineAssertion) bool {
	for _, item := range assertions {
		metric := strings.ToLower(strings.TrimSpace(item.Metric))
		if metric == "" {
			continue
		}
		switch metric {
		case "source_move_ratio_percent", "move_ratio_percent", "half_transfer_ratio":
			switch strings.TrimSpace(item.Operator) {
			case "==", ">=", "<=", "between":
				if item.Value == 50 || item.Min == 50 || item.Max == 50 {
					return true
				}
			}
		case "source_remaining_count":
			// 1. 该断言常用于“迁走一半后来源剩余=一半”。
			// 2. 具体阈值是否满足由 objective + deterministic 校验统一判定。
			return true
		}
	}
	return false
}

func scheduleRefineNormalizeHardAssertions(raw []scheduleRefineHardAssertionOutput) []RefineAssertion {
	if len(raw) == 0 {
		return nil
	}
	out := make([]RefineAssertion, 0, len(raw))
	for _, item := range raw {
		metric := strings.TrimSpace(item.Metric)
		if metric == "" {
			continue
		}
		operator := strings.TrimSpace(item.Operator)
		if operator == "" {
			operator = "=="
		}
		assertion := RefineAssertion{
			Metric:     metric,
			Operator:   operator,
			Value:      item.Value,
			Min:        item.Min,
			Max:        item.Max,
			Week:       item.Week,
			TargetWeek: item.TargetWeek,
		}
		out = append(out, assertion)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func scheduleRefineInferHardAssertionsFromRequest(message string, requirements []string) []RefineAssertion {
	joined := strings.TrimSpace(message + " " + strings.Join(requirements, " "))
	if joined == "" {
		return nil
	}
	weeks := scheduleRefineExtractWeekFilters(joined)
	if !scheduleRefineContainsAny(strings.ToLower(joined), []string{"一半", "半数", "对半", "50%"}) {
		return nil
	}
	// 1. 兜底断言：要求来源任务迁移比例为 50%。
	// 2. week/target_week 使用文本中前两个周次，便于后续 objective 编译。
	assertion := RefineAssertion{
		Metric:   "source_move_ratio_percent",
		Operator: "==",
		Value:    50,
	}
	if len(weeks) > 0 {
		assertion.Week = weeks[0]
	}
	if len(weeks) > 1 {
		assertion.TargetWeek = weeks[1]
	}
	return []RefineAssertion{assertion}
}

func scheduleRefineIsTaskMovedIntoObjectiveTarget(entry model.HybridScheduleEntry, obj RefineObjective) (bool, string) {
	targetWeekSet := scheduleRefineIntSliceToWeekSet(obj.TargetWeeks)
	targetDaySet := scheduleRefineIntSliceToDaySet(obj.TargetDays)
	excludedSections := scheduleRefineIntSliceToSectionSet(obj.ExcludeSections)
	if len(targetWeekSet) > 0 {
		if _, ok := targetWeekSet[entry.Week]; !ok {
			return false, fmt.Sprintf("week=%d 不在目标周", entry.Week)
		}
	}
	if len(targetDaySet) > 0 {
		if _, ok := targetDaySet[entry.DayOfWeek]; !ok {
			return false, fmt.Sprintf("day_of_week=%d 不在目标日", entry.DayOfWeek)
		}
	}
	if len(excludedSections) > 0 && scheduleRefineIntersectsExcludedSections(entry.SectionFrom, entry.SectionTo, excludedSections) {
		return false, fmt.Sprintf("section=%d-%d 命中排除节次", entry.SectionFrom, entry.SectionTo)
	}
	return true, ""
}

func scheduleRefineIsTaskInObjectiveSource(entry model.HybridScheduleEntry, obj RefineObjective) bool {
	sourceWeekSet := scheduleRefineIntSliceToWeekSet(obj.SourceWeeks)
	sourceDaySet := scheduleRefineIntSliceToDaySet(obj.SourceDays)
	if len(sourceWeekSet) > 0 {
		if _, ok := sourceWeekSet[entry.Week]; !ok {
			return false
		}
	}
	if len(sourceDaySet) > 0 {
		if _, ok := sourceDaySet[entry.DayOfWeek]; !ok {
			return false
		}
	}
	return true
}

func scheduleRefineIsSameWeeks(left []int, right []int) bool {
	if len(left) == 0 || len(right) == 0 {
		return false
	}
	lset := scheduleRefineIntSliceToWeekSet(left)
	rset := scheduleRefineIntSliceToWeekSet(right)
	if len(lset) != len(rset) {
		return false
	}
	for w := range lset {
		if _, ok := rset[w]; !ok {
			return false
		}
	}
	return true
}

// scheduleRefineNormalizeMovableTaskOrderByOrigin 在“坑位不变”的前提下，按 origin_rank 归位任务顺序。
//
// 步骤化说明：
// 1. 先提取所有可移动任务的当前坑位（week/day/section）；
// 2. 再按任务跨度分组，避免把 2 节任务塞进 3 节坑位；
// 3. 每个跨度组内按坑位时间升序与 origin_rank 升序做一一映射；
// 4. 最终只改“任务身份落到哪个坑位”，不改坑位分布本身。
func scheduleRefineNormalizeMovableTaskOrderByOrigin(st *ScheduleRefineState) bool {
	if st == nil || len(st.HybridEntries) <= 1 || len(st.OriginOrderMap) == 0 {
		return false
	}
	entries := cloneHybridEntries(st.HybridEntries)
	indices := make([]int, 0, len(entries))
	for idx, entry := range entries {
		if scheduleRefineIsMovableSuggestedTask(entry) {
			indices = append(indices, idx)
		}
	}
	if len(indices) <= 1 {
		return false
	}

	type slot struct {
		Week        int
		DayOfWeek   int
		SectionFrom int
		SectionTo   int
	}
	groupSlots := make(map[int][]slot)                      // key=span
	groupTasks := make(map[int][]model.HybridScheduleEntry) // key=span
	for _, idx := range indices {
		entry := entries[idx]
		span := entry.SectionTo - entry.SectionFrom + 1
		groupSlots[span] = append(groupSlots[span], slot{
			Week:        entry.Week,
			DayOfWeek:   entry.DayOfWeek,
			SectionFrom: entry.SectionFrom,
			SectionTo:   entry.SectionTo,
		})
		groupTasks[span] = append(groupTasks[span], entry)
	}

	changed := false
	spanKeys := make([]int, 0, len(groupSlots))
	for span := range groupSlots {
		spanKeys = append(spanKeys, span)
	}
	sort.Ints(spanKeys)

	groupCursor := make(map[int]int, len(groupSlots))
	for _, span := range spanKeys {
		slots := groupSlots[span]
		tasks := groupTasks[span]
		if len(slots) != len(tasks) || len(slots) == 0 {
			continue
		}
		sort.SliceStable(slots, func(i, j int) bool {
			if slots[i].Week != slots[j].Week {
				return slots[i].Week < slots[j].Week
			}
			if slots[i].DayOfWeek != slots[j].DayOfWeek {
				return slots[i].DayOfWeek < slots[j].DayOfWeek
			}
			if slots[i].SectionFrom != slots[j].SectionFrom {
				return slots[i].SectionFrom < slots[j].SectionFrom
			}
			return slots[i].SectionTo < slots[j].SectionTo
		})
		sort.SliceStable(tasks, func(i, j int) bool {
			ri := st.OriginOrderMap[tasks[i].TaskItemID]
			rj := st.OriginOrderMap[tasks[j].TaskItemID]
			if ri <= 0 {
				ri = 1 << 30
			}
			if rj <= 0 {
				rj = 1 << 30
			}
			if ri != rj {
				return ri < rj
			}
			if tasks[i].Week != tasks[j].Week {
				return tasks[i].Week < tasks[j].Week
			}
			if tasks[i].DayOfWeek != tasks[j].DayOfWeek {
				return tasks[i].DayOfWeek < tasks[j].DayOfWeek
			}
			if tasks[i].SectionFrom != tasks[j].SectionFrom {
				return tasks[i].SectionFrom < tasks[j].SectionFrom
			}
			return tasks[i].TaskItemID < tasks[j].TaskItemID
		})
		for i := range tasks {
			tasks[i].Week = slots[i].Week
			tasks[i].DayOfWeek = slots[i].DayOfWeek
			tasks[i].SectionFrom = slots[i].SectionFrom
			tasks[i].SectionTo = slots[i].SectionTo
		}
		groupTasks[span] = tasks
	}

	for _, idx := range indices {
		entry := entries[idx]
		span := entry.SectionTo - entry.SectionFrom + 1
		cursor := groupCursor[span]
		if cursor >= len(groupTasks[span]) {
			continue
		}
		nextEntry := groupTasks[span][cursor]
		groupCursor[span] = cursor + 1
		if entry.TaskItemID != nextEntry.TaskItemID ||
			entry.Week != nextEntry.Week ||
			entry.DayOfWeek != nextEntry.DayOfWeek ||
			entry.SectionFrom != nextEntry.SectionFrom ||
			entry.SectionTo != nextEntry.SectionTo {
			changed = true
		}
		entries[idx] = nextEntry
	}
	if !changed {
		return false
	}
	scheduleRefineSortHybridEntries(entries)
	st.HybridEntries = entries
	return true
}

// scheduleRefineTryNormalizeMovableTaskOrderByOrigin 决定是否执行“按 origin_rank 顺序归位”。
//
// 步骤化说明：
// 1. 默认仍保持旧行为，继续在终审前做展示侧顺序归位；
// 2. 但当 MinContextSwitch 已成功执行时，重排后的顺序本身就是业务目标的一部分；
// 3. 此时若再按 origin_rank 归位，会把复合工具效果直接抹掉，因此必须跳过。
func scheduleRefineTryNormalizeMovableTaskOrderByOrigin(st *ScheduleRefineState) (changed bool, skipped bool) {
	if scheduleRefineShouldSkipOriginOrderNormalization(st) {
		return false, true
	}
	return scheduleRefineNormalizeMovableTaskOrderByOrigin(st), false
}

func scheduleRefineShouldSkipOriginOrderNormalization(st *ScheduleRefineState) bool {
	if st == nil {
		return false
	}
	scheduleRefineEnsureCompositeStateMaps(st)
	if st.CompositeToolSuccess["MinContextSwitch"] {
		return true
	}
	return false
}

func scheduleRefineShouldSkipOrderConstraintCheck(st *ScheduleRefineState) bool {
	return scheduleRefineShouldSkipOriginOrderNormalization(st)
}

func scheduleRefineRunSingleRepairAction(ctx context.Context, chatModel *ark.ChatModel, st *ScheduleRefineState, emitStage func(stage, detail string)) error {
	if st == nil {
		return fmt.Errorf("nil state")
	}
	if chatModel == nil {
		return fmt.Errorf("nil model")
	}
	if st.RoundUsed >= st.MaxRounds {
		return fmt.Errorf("动作预算已耗尽")
	}
	entriesJSON, _ := json.Marshal(st.HybridEntries)
	contractJSON, _ := json.Marshal(st.Contract)
	userPrompt := scheduleRefineWithNearestJSONContract(
		fmt.Sprintf(
			"用户请求=%s\n契约=%s\n未满足点=%s\n当前混合日程JSON=%s\nMove标准Schema={task_item_id,to_week,to_day,to_section_from,to_section_to}\nSwap标准Schema={task_a,task_b}",
			strings.TrimSpace(st.UserMessage),
			string(contractJSON),
			strings.Join(st.HardCheck.IntentUnmet, "；"),
			string(entriesJSON),
		),
		jsonContractForReact,
	)
	raw, err := scheduleRefineCallModelText(ctx, chatModel, agentprompt.ScheduleRefineRepairPrompt, userPrompt, false, 240, 0.15)
	if err != nil {
		return err
	}
	scheduleRefineEmitModelRawDebug(emitStage, "repair", raw)
	parsed, parseErr := parseScheduleRefineLLMOutput(raw)
	if parseErr != nil {
		return parseErr
	}
	call, warn := pickSingleScheduleRefineToolCall(parsed.ToolCalls)
	if warn != "" {
		st.ActionLogs = append(st.ActionLogs, "修复阶段告警："+warn)
	}
	if call == nil {
		return fmt.Errorf("修复阶段未给出可执行动作")
	}
	normalizedCall := scheduleRefineCanonicalizeToolCall(*call)
	call = &normalizedCall
	if !scheduleRefineIsMutatingToolName(strings.TrimSpace(call.Tool)) {
		return fmt.Errorf("修复阶段工具不允许：%s（仅允许 Move/Swap/BatchMove）", strings.TrimSpace(call.Tool))
	}
	emitStage("schedule_refine.hard_check.repair_call", scheduleRefineFormatToolCallStageDetail(st.RoundUsed+1, *call, st.MaxRounds-st.RoundUsed))
	nextEntries, result := dispatchScheduleRefineTool(cloneHybridEntries(st.HybridEntries), *call, scheduleRefineBuildPlanningWindowFromEntries(st.HybridEntries), scheduleRefineToolPolicy{
		KeepRelativeOrder: false,
		OrderScope:        st.Contract.OrderScope,
		OriginOrderMap:    st.OriginOrderMap,
	})
	result = scheduleRefineNormalizeToolResult(result)
	st.RoundUsed++
	emitStage("schedule_refine.hard_check.repair_result", scheduleRefineFormatToolResultStageDetail(st.RoundUsed, result, st.RoundUsed, st.MaxRounds))
	if !result.Success {
		st.LastFailedCallSignature = scheduleRefineBuildToolCallSignature(*call)
		return fmt.Errorf("修复动作执行失败：%s", result.Result)
	}
	st.LastFailedCallSignature = ""
	st.HybridEntries = nextEntries
	if scheduleRefineIsMutatingToolName(result.Tool) {
		st.EntriesVersion++
	}
	return nil
}

func scheduleRefineRunSemanticReview(ctx context.Context, chatModel *ark.ChatModel, st *ScheduleRefineState, emitStage func(stage, detail string)) (*scheduleRefineReviewOutput, error) {
	entriesJSON, _ := json.Marshal(st.HybridEntries)
	contractJSON, _ := json.Marshal(st.Contract)
	userPrompt := scheduleRefineWithNearestJSONContract(
		fmt.Sprintf(
			"用户请求=%s\n契约=%s\nday_of_week映射=1周一,2周二,3周三,4周四,5周五,6周六,7周日\nsuggested简表=%s\n动作日志=%s\n当前混合日程JSON=%s",
			strings.TrimSpace(st.UserMessage),
			string(contractJSON),
			scheduleRefineBuildSuggestedDigest(st.HybridEntries, 80),
			scheduleRefineSummarizeActionLogs(st.ActionLogs, 12),
			string(entriesJSON),
		),
		jsonContractForReview,
	)
	raw, err := scheduleRefineCallModelText(ctx, chatModel, agentprompt.ScheduleRefineReviewPrompt, userPrompt, false, 240, 0)
	if err != nil {
		return nil, err
	}
	scheduleRefineEmitModelRawDebug(emitStage, "review", raw)
	return parseScheduleRefineReviewOutput(raw)
}

func scheduleRefineRunPostReflectAfterTool(
	ctx context.Context,
	chatModel *ark.ChatModel,
	st *ScheduleRefineState,
	round int,
	plan *scheduleRefineReactLLMOutput,
	call *scheduleRefineReactToolCall,
	result scheduleRefineReactToolResult,
	emitStage func(stage, detail string),
) (string, string, bool) {
	if st == nil || chatModel == nil || call == nil {
		return scheduleRefineBuildPostReflectFallback(plan, result), "", false
	}
	emitStage("schedule_refine.react.post_reflect.start", fmt.Sprintf("第 %d 轮|正在基于工具真实结果进行反思。", round))
	contractJSON, _ := json.Marshal(st.Contract)
	callJSON, _ := json.Marshal(call)
	resultJSON, _ := json.Marshal(result)
	planDecision := ""
	if plan != nil {
		planDecision = strings.TrimSpace(plan.Decision)
	}
	userPrompt := scheduleRefineWithNearestJSONContract(
		fmt.Sprintf(
			"用户请求=%s\n契约=%s\n本轮计划.decision=%s\n本轮工具调用=%s\n本轮工具结果=%s\n最近观察=%s",
			strings.TrimSpace(st.UserMessage),
			string(contractJSON),
			planDecision,
			string(callJSON),
			string(resultJSON),
			scheduleRefineBuildObservationPrompt(st.ObservationHistory, 2),
		),
		jsonContractForPostReflect,
	)
	raw, err := scheduleRefineCallModelText(ctx, chatModel, agentprompt.ScheduleRefinePostReflectPrompt, userPrompt, false, 220, 0)
	if err != nil {
		fallback := scheduleRefineBuildPostReflectFallback(plan, result)
		emitStage("schedule_refine.react.post_reflect.fallback", fmt.Sprintf("第 %d 轮|模型反思失败，改用后端兜底复盘：%s", round, scheduleRefineTruncate(err.Error(), 160)))
		return fallback, "", false
	}
	scheduleRefineEmitModelRawDebug(emitStage, fmt.Sprintf("post_reflect.round.%d", round), raw)
	parsed, parseErr := scheduleRefineParseJSON[scheduleRefinePostReflectOutput](raw)
	if parseErr != nil {
		fallback := scheduleRefineBuildPostReflectFallback(plan, result)
		emitStage("schedule_refine.react.post_reflect.fallback", fmt.Sprintf("第 %d 轮|模型反思解析失败，改用后端兜底复盘：%s", round, scheduleRefineTruncate(parseErr.Error(), 160)))
		return fallback, "", false
	}
	reflection := strings.TrimSpace(parsed.Reflection)
	if reflection == "" {
		reflection = scheduleRefineBuildPostReflectFallback(plan, result)
	}
	nextStrategy := strings.TrimSpace(parsed.NextStrategy)
	if nextStrategy != "" {
		reflection = fmt.Sprintf("%s；下一步建议：%s", reflection, nextStrategy)
	}
	shouldStop := parsed.ShouldStop
	emitStage("schedule_refine.react.post_reflect.done", fmt.Sprintf("第 %d 轮|模型反思=%s|下一步=%s|should_stop=%t", round, scheduleRefineTruncate(strings.TrimSpace(parsed.Reflection), 120), scheduleRefineTruncate(nextStrategy, 120), shouldStop))
	return reflection, nextStrategy, shouldStop
}

func scheduleRefineBuildPostReflectFallback(plan *scheduleRefineReactLLMOutput, result scheduleRefineReactToolResult) string {
	modelReflect := ""
	if plan != nil {
		modelReflect = strings.TrimSpace(plan.Decision)
	}
	return scheduleRefineBuildRuntimeReflect(modelReflect, result)
}

func scheduleRefineRunPlannerNode(
	ctx context.Context,
	chatModel *ark.ChatModel,
	st *ScheduleRefineState,
	emitStage func(stage, detail string),
	mode string,
) error {
	if st == nil || chatModel == nil {
		return fmt.Errorf("planner: invalid input")
	}
	scheduleRefineEnsureCompositeStateMaps(st)
	// 1. 正常模式下由后端判定“本轮必用复合工具”。
	// 2. 若已进入禁复合兜底模式，必须清空该标记，避免规划阶段再次把复合门禁写回去。
	if st.DisableCompositeTools {
		st.RequiredCompositeTool = ""
	} else {
		st.RequiredCompositeTool = scheduleRefineDetectRequiredCompositeTool(st)
	}
	if st.PlanUsed >= st.PlanMax {
		return nil
	}
	stage := "schedule_refine.plan.generating"
	if strings.TrimSpace(mode) == "replan" {
		stage = "schedule_refine.plan.regenerating"
	}
	emitStage(stage, fmt.Sprintf("正在生成执行计划（mode=%s，已用%d/%d）。", mode, st.PlanUsed, st.PlanMax))
	contractJSON, _ := json.Marshal(st.Contract)
	userPrompt := scheduleRefineWithNearestJSONContract(
		fmt.Sprintf(
			"mode=%s\n用户请求=%s\n契约=%s\n上一轮工具观察=%s\n最近观察=%s\nsuggested简表=%s",
			mode,
			strings.TrimSpace(st.UserMessage),
			string(contractJSON),
			scheduleRefineBuildLastToolObservationPrompt(st.ObservationHistory),
			scheduleRefineBuildObservationPrompt(st.ObservationHistory, 2),
			scheduleRefineBuildSuggestedDigest(st.HybridEntries, 40),
		),
		jsonContractForPlanner,
	)
	raw, err := scheduleRefineCallModelText(ctx, chatModel, agentprompt.ScheduleRefinePlannerPrompt, userPrompt, false, plannerMaxTokens, 0)
	if err != nil {
		st.CurrentPlan = scheduleRefineApplyCompositeHardConditionToPlan(st, scheduleRefineBuildFallbackPlan(st))
		st.BatchMoveAllowed = scheduleRefineShouldAllowBatchMove(st.CurrentPlan)
		st.PlanUsed++
		emitStage("schedule_refine.plan.fallback", "Planner 调用失败，已切换后端兜底计划。")
		return nil
	}
	scheduleRefineEmitModelRawDebug(emitStage, fmt.Sprintf("planner.%s", mode), raw)
	parsed, parseErr := scheduleRefineParsePlannerOutputWithRetryOnce(ctx, chatModel, userPrompt, raw, mode, emitStage)
	if parseErr != nil {
		st.CurrentPlan = scheduleRefineApplyCompositeHardConditionToPlan(st, scheduleRefineBuildFallbackPlan(st))
		st.BatchMoveAllowed = scheduleRefineShouldAllowBatchMove(st.CurrentPlan)
		st.PlanUsed++
		emitStage("schedule_refine.plan.fallback", fmt.Sprintf("Planner 输出解析失败，已切换后端兜底计划：%s", scheduleRefineTruncate(parseErr.Error(), 180)))
		return nil
	}
	st.CurrentPlan = PlannerPlan{
		Summary: scheduleRefineFallbackText(strings.TrimSpace(parsed.Summary), "已生成可执行计划。"),
		Steps:   scheduleRefineUniqueNonEmpty(parsed.Steps),
	}
	st.CurrentPlan = scheduleRefineApplyCompositeHardConditionToPlan(st, st.CurrentPlan)
	st.BatchMoveAllowed = scheduleRefineShouldAllowBatchMove(st.CurrentPlan)
	if st.DisableCompositeTools {
		st.BatchMoveAllowed = false
	}
	st.PlanUsed++
	emitStage("schedule_refine.plan.done", fmt.Sprintf("规划完成：%s", scheduleRefineTruncate(st.CurrentPlan.Summary, 180)))
	return nil
}

func scheduleRefineHandleBlockedToolResult(
	ctx context.Context,
	chatModel *ark.ChatModel,
	st *ScheduleRefineState,
	emitStage func(stage, detail string),
	round int,
	parsed *scheduleRefineReactLLMOutput,
	call *scheduleRefineReactToolCall,
	callSignature string,
	blockedResult scheduleRefineReactToolResult,
	observation *ReactRoundObservation,
) (bool, error) {
	result := scheduleRefineNormalizeToolResult(blockedResult)
	st.RoundUsed++
	st.LastFailedCallSignature = callSignature
	st.ConsecutiveFailures++
	observation.ToolName = strings.TrimSpace(result.Tool)
	observation.ToolParams = scheduleRefineCloneToolParams(call.Params)
	observation.ToolSuccess = result.Success
	observation.ToolErrorCode = strings.TrimSpace(result.ErrorCode)
	observation.ToolResult = strings.TrimSpace(result.Result)
	postReflectText, _, shouldStop := scheduleRefineRunPostReflectAfterTool(ctx, chatModel, st, round, parsed, call, result, emitStage)
	observation.Reflect = postReflectText
	st.ObservationHistory = append(st.ObservationHistory, *observation)
	emitStage("schedule_refine.react.tool_blocked", fmt.Sprintf("第 %d 轮|动作被后端策略拦截：%s", round, scheduleRefineTruncate(result.Result, 120)))
	emitStage("schedule_refine.react.tool_result", scheduleRefineFormatToolResultStageDetail(round, result, st.RoundUsed, st.MaxRounds))
	emitStage("schedule_refine.react.reflect", scheduleRefineFormatReactReflectStageDetail(round, observation.Reflect))
	if scheduleRefineShouldTriggerReplan(st, result) {
		if replanned, err := scheduleRefineTryReplan(ctx, chatModel, st, emitStage); err != nil {
			return false, err
		} else if replanned {
			return false, nil
		}
	}
	return shouldStop, nil
}

func scheduleRefineBuildFallbackPlan(st *ScheduleRefineState) PlannerPlan {
	summary := "兜底计划：先取证再动作，优先复合工具，其次 Move，冲突时尝试 Swap。"
	if st != nil && st.Contract.KeepRelativeOrder {
		summary = "兜底计划：先取证再动作，严格保持相对顺序，优先复合工具，其次 Move，冲突时尝试 Swap。"
	}
	return PlannerPlan{
		Summary: summary,
		Steps: []string{
			"1) QueryTargetTasks 定位目标任务",
			"2) QueryAvailableSlots 获取可用空位",
			"3) 优先 SpreadEven/MinContextSwitch，其次 Move/Swap 执行动作并复盘",
			"4) 收尾前执行 Verify 自检",
		},
	}
}

// scheduleRefineEnsureCompositeStateMaps 确保复合工具状态容器已初始化。
func scheduleRefineEnsureCompositeStateMaps(st *ScheduleRefineState) {
	if st == nil {
		return
	}
	if st.CompositeToolCalled == nil {
		st.CompositeToolCalled = map[string]bool{
			"SpreadEven":       false,
			"MinContextSwitch": false,
		}
	}
	if st.CompositeToolSuccess == nil {
		st.CompositeToolSuccess = map[string]bool{
			"SpreadEven":       false,
			"MinContextSwitch": false,
		}
	}
}

// scheduleRefineDetectRequiredCompositeTool 根据请求语义识别本轮必用复合工具。
//
// 规则：
// 1. “上下文切换最少/同科目连续”优先映射 MinContextSwitch；
// 2. “均匀分散/铺开”映射 SpreadEven；
// 3. 未命中时返回空串，不强制复合工具。
func scheduleRefineDetectRequiredCompositeTool(st *ScheduleRefineState) string {
	if st == nil {
		return ""
	}
	joined := strings.TrimSpace(strings.Join([]string{
		st.UserMessage,
		st.Contract.Intent,
		strings.Join(st.Contract.HardRequirements, " "),
	}, " "))
	if joined == "" {
		return ""
	}
	contextKeys := []string{"上下文切换", "切换最少", "同个科目", "同科目", "连续处理", "连续学习", "min context", "context switch"}
	if scheduleRefineContainsAny(strings.ToLower(joined), contextKeys) || scheduleRefineContainsAny(joined, contextKeys) {
		return "MinContextSwitch"
	}
	evenKeys := []string{"均匀", "分散", "铺开", "平摊", "均摊", "spread even", "even spread"}
	if scheduleRefineContainsAny(strings.ToLower(joined), evenKeys) || scheduleRefineContainsAny(joined, evenKeys) {
		return "SpreadEven"
	}
	return ""
}

// scheduleRefineApplyCompositeHardConditionToPlan 把“必用复合工具”硬条件注入计划文本。
func scheduleRefineApplyCompositeHardConditionToPlan(st *ScheduleRefineState, plan PlannerPlan) PlannerPlan {
	required := ""
	if st != nil {
		required = scheduleRefineNormalizeCompositeToolName(st.RequiredCompositeTool)
	}
	if required == "" {
		return plan
	}

	hardStep := fmt.Sprintf("硬条件：必须成功调用 %s（COMPOSITE_SUCCESS[%s]=true）后才允许整体收口", required, required)
	hasHardStep := false
	for _, step := range plan.Steps {
		if strings.Contains(step, required) && strings.Contains(step, "COMPOSITE_SUCCESS") {
			hasHardStep = true
			break
		}
	}
	if !hasHardStep {
		plan.Steps = append([]string{hardStep}, plan.Steps...)
	}
	if !strings.Contains(plan.Summary, required) {
		plan.Summary = strings.TrimSpace(plan.Summary + "；硬条件：" + required + " 成功==true")
	}
	return plan
}

func scheduleRefineNormalizeCompositeToolName(name string) string {
	switch strings.TrimSpace(name) {
	case "SpreadEven":
		return "SpreadEven"
	case "MinContextSwitch":
		return "MinContextSwitch"
	default:
		return ""
	}
}

func scheduleRefineIsCompositeToolName(toolName string) bool {
	switch scheduleRefineNormalizeCompositeToolName(toolName) {
	case "SpreadEven", "MinContextSwitch":
		return true
	default:
		return false
	}
}

func scheduleRefineIsBaseMutatingToolName(toolName string) bool {
	switch strings.TrimSpace(toolName) {
	case "Move", "Swap", "BatchMove":
		return true
	default:
		return false
	}
}

func scheduleRefineIsRequiredCompositeSatisfied(st *ScheduleRefineState) bool {
	if st == nil {
		return true
	}
	required := scheduleRefineNormalizeCompositeToolName(st.RequiredCompositeTool)
	if required == "" {
		return true
	}
	scheduleRefineEnsureCompositeStateMaps(st)
	return st.CompositeToolSuccess[required]
}

// scheduleRefineApplyCompositeGateToIntentResult 把“必用复合工具成功”并入业务目标判定。
//
// 步骤化说明：
// 1. 先判断原始业务判定是否通过；未通过则原样返回；
// 2. 再判断是否配置了必用复合工具；未配置则原样返回；
// 3. 若配置但未成功，强制改判为失败并补充 unmet 原因。
func scheduleRefineApplyCompositeGateToIntentResult(st *ScheduleRefineState, pass bool, reason string, unmet []string) (bool, string, []string) {
	if !pass {
		return pass, reason, append([]string(nil), unmet...)
	}
	required := scheduleRefineNormalizeCompositeToolName("")
	if st != nil {
		required = scheduleRefineNormalizeCompositeToolName(st.RequiredCompositeTool)
	}
	if required == "" {
		return pass, reason, append([]string(nil), unmet...)
	}
	if scheduleRefineIsRequiredCompositeSatisfied(st) {
		return pass, reason, append([]string(nil), unmet...)
	}
	newUnmet := append([]string(nil), unmet...)
	newUnmet = append(newUnmet, fmt.Sprintf("复合工具门禁未通过：%s 尚未成功调用", required))
	return false, fmt.Sprintf("复合工具门禁未通过：要求 %s 成功==true。", required), newUnmet
}

func scheduleRefineMarkCompositeToolOutcome(st *ScheduleRefineState, toolName string, success bool) {
	if st == nil {
		return
	}
	tool := scheduleRefineNormalizeCompositeToolName(toolName)
	if tool == "" {
		return
	}
	scheduleRefineEnsureCompositeStateMaps(st)
	st.CompositeToolCalled[tool] = true
	if success {
		st.CompositeToolSuccess[tool] = true
	}
}

func scheduleRefineShouldAllowBatchMove(plan PlannerPlan) bool {
	text := strings.ToLower(strings.TrimSpace(plan.Summary))
	if strings.Contains(text, "batchmove") || strings.Contains(text, "batch move") {
		return true
	}
	for _, step := range plan.Steps {
		s := strings.ToLower(strings.TrimSpace(step))
		if strings.Contains(s, "batchmove") || strings.Contains(s, "batch move") {
			return true
		}
	}
	return false
}

func scheduleRefineShouldEnableRecoveryThinking(st *ScheduleRefineState) (bool, string) {
	if st == nil {
		return false, ""
	}
	if st.ConsecutiveFailures < 2 || st.ThinkingBoostArmed {
		return false, ""
	}
	st.ThinkingBoostArmed = true
	return true, fmt.Sprintf("连续失败=%d，触发 1 轮恢复态 thinking", st.ConsecutiveFailures)
}

func scheduleRefineShouldTriggerReplan(st *ScheduleRefineState, result scheduleRefineReactToolResult) bool {
	if st == nil {
		return false
	}
	if st.ConsecutiveFailures < 3 {
		return false
	}
	switch strings.TrimSpace(result.ErrorCode) {
	case "SLOT_CONFLICT", "ORDER_VIOLATION", "REPEAT_FAILED_ACTION", "PARAM_MISSING", "BATCH_MOVE_FAILED", "VERIFY_FAILED", "TASK_BUDGET_EXCEEDED", "BATCH_MOVE_DISABLED", "CURRENT_TASK_MISMATCH", "QUERY_REDUNDANT", "SLOT_QUERY_FAILED", "PLAN_FAILED", "PLAN_EMPTY", "COMPOSITE_REQUIRED", "COMPOSITE_DISABLED":
		return true
	default:
		return false
	}
}

func scheduleRefineTryReplan(
	ctx context.Context,
	chatModel *ark.ChatModel,
	st *ScheduleRefineState,
	emitStage func(stage, detail string),
) (bool, error) {
	if st == nil {
		return false, nil
	}
	if st.ReplanUsed >= st.ReplanMax || st.PlanUsed >= st.PlanMax {
		return false, nil
	}
	st.ReplanUsed++
	emitStage("schedule_refine.plan.replan_trigger", fmt.Sprintf("连续失败=%d，触发重规划（%d/%d）。", st.ConsecutiveFailures, st.ReplanUsed, st.ReplanMax))
	if err := scheduleRefineRunPlannerNode(ctx, chatModel, st, emitStage, "replan"); err != nil {
		return true, err
	}
	st.ConsecutiveFailures = 0
	st.ThinkingBoostArmed = false
	return true, nil
}

func scheduleRefineCallModelText(
	ctx context.Context,
	chatModel *ark.ChatModel,
	systemPrompt string,
	userPrompt string,
	useThinking bool,
	maxTokens int,
	temperature float32,
) (string, error) {
	if chatModel == nil {
		return "", fmt.Errorf("model is nil")
	}
	nodeCtx, cancel := context.WithTimeout(ctx, nodeTimeout)
	defer cancel()
	thinkingType := arkModel.ThinkingTypeDisabled
	if useThinking {
		thinkingType = arkModel.ThinkingTypeEnabled
	}
	opts := []einoModel.Option{
		ark.WithThinking(&arkModel.Thinking{Type: thinkingType}),
		einoModel.WithTemperature(temperature),
	}
	if maxTokens > 0 {
		opts = append(opts, einoModel.WithMaxTokens(maxTokens))
	}
	resp, err := chatModel.Generate(nodeCtx, []*schema.Message{
		schema.SystemMessage(systemPrompt),
		schema.UserMessage(userPrompt),
	}, opts...)
	if err != nil {
		if errors.Is(nodeCtx.Err(), context.DeadlineExceeded) {
			return "", fmt.Errorf("model call node timeout(%dms): %w", nodeTimeout.Milliseconds(), err)
		}
		if nodeCtx.Err() != nil {
			return "", fmt.Errorf("model call node canceled(%v): %w", nodeCtx.Err(), err)
		}
		if ctx.Err() != nil {
			return "", fmt.Errorf("model call parent canceled(%v): %w", ctx.Err(), err)
		}
		return "", err
	}
	if resp == nil {
		return "", fmt.Errorf("model response is nil")
	}
	content := strings.TrimSpace(resp.Content)
	if content == "" {
		return "", fmt.Errorf("model response content is empty")
	}
	return content, nil
}

func scheduleRefineParseJSON[T any](raw string) (*T, error) {
	clean := strings.TrimSpace(raw)
	if clean == "" {
		return nil, fmt.Errorf("empty response")
	}
	if strings.HasPrefix(clean, "```") {
		clean = strings.TrimPrefix(clean, "```json")
		clean = strings.TrimPrefix(clean, "```")
		clean = strings.TrimSuffix(clean, "```")
		clean = strings.TrimSpace(clean)
	}
	var out T
	if err := json.Unmarshal([]byte(clean), &out); err == nil {
		return &out, nil
	}
	obj, err := scheduleRefineExtractFirstJSONObject(clean)
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal([]byte(obj), &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func scheduleRefineExtractFirstJSONObject(text string) (string, error) {
	start := strings.Index(text, "{")
	if start < 0 {
		return "", fmt.Errorf("no json object found")
	}
	depth := 0
	inString := false
	escape := false
	for i := start; i < len(text); i++ {
		ch := text[i]
		if inString {
			if escape {
				escape = false
				continue
			}
			if ch == '\\' {
				escape = true
				continue
			}
			if ch == '"' {
				inString = false
			}
			continue
		}
		if ch == '"' {
			inString = true
			continue
		}
		if ch == '{' {
			depth++
			continue
		}
		if ch == '}' {
			depth--
			if depth == 0 {
				return text[start : i+1], nil
			}
		}
	}
	return "", fmt.Errorf("json object not closed")
}

func scheduleRefineEmitModelRawDebug(emitStage func(stage, detail string), tag string, raw string) {
	if emitStage == nil {
		return
	}
	clean := strings.TrimSpace(raw)
	if clean == "" {
		clean = "<empty>"
	}
	const chunkSize = 1600
	runes := []rune(clean)
	if len(runes) <= chunkSize {
		emitStage("schedule_refine.debug.raw", fmt.Sprintf("[debug][%s] %s", strings.TrimSpace(tag), clean))
		return
	}
	total := (len(runes) + chunkSize - 1) / chunkSize
	for i := 0; i < total; i++ {
		start := i * chunkSize
		end := start + chunkSize
		if end > len(runes) {
			end = len(runes)
		}
		emitStage("schedule_refine.debug.raw", fmt.Sprintf("[debug][%s][part %d/%d] %s", strings.TrimSpace(tag), i+1, total, string(runes[start:end])))
	}
}

func scheduleRefinePhysicsCheck(entries []model.HybridScheduleEntry, allocatedCount int) []string {
	issues := make([]string, 0, 8)
	slotMap := make(map[string]string, len(entries)*2)
	for _, entry := range entries {
		if entry.SectionFrom < 1 || entry.SectionTo > 12 || entry.SectionFrom > entry.SectionTo {
			issues = append(issues, fmt.Sprintf("节次越界：%s W%dD%d %d-%d", entry.Name, entry.Week, entry.DayOfWeek, entry.SectionFrom, entry.SectionTo))
		}
		if !scheduleRefineEntryBlocksSuggested(entry) {
			continue
		}
		for sec := entry.SectionFrom; sec <= entry.SectionTo; sec++ {
			key := fmt.Sprintf("%d-%d-%d", entry.Week, entry.DayOfWeek, sec)
			if existed, ok := slotMap[key]; ok {
				issues = append(issues, fmt.Sprintf("冲突：%s 与 %s 同时占用 W%dD%d 第%d节", existed, entry.Name, entry.Week, entry.DayOfWeek, sec))
			} else {
				slotMap[key] = entry.Name
			}
		}
	}
	if allocatedCount > 0 {
		suggested := scheduleRefineCountSuggested(entries)
		if suggested != allocatedCount {
			issues = append(issues, fmt.Sprintf("数量不一致：suggested=%d，allocated_items=%d", suggested, allocatedCount))
		}
	}
	return issues
}

func scheduleRefineUpdateAllocatedItemsFromEntries(st *ScheduleRefineState) {
	if st == nil || len(st.AllocatedItems) == 0 || len(st.HybridEntries) == 0 {
		return
	}
	byTaskID := make(map[int]model.HybridScheduleEntry, len(st.HybridEntries))
	for _, entry := range st.HybridEntries {
		if scheduleRefineIsMovableSuggestedTask(entry) {
			byTaskID[entry.TaskItemID] = entry
		}
	}
	for i := range st.AllocatedItems {
		item := &st.AllocatedItems[i]
		entry, ok := byTaskID[item.ID]
		if !ok {
			continue
		}
		if item.EmbeddedTime == nil {
			item.EmbeddedTime = &model.TargetTime{}
		}
		item.EmbeddedTime.Week = entry.Week
		item.EmbeddedTime.DayOfWeek = entry.DayOfWeek
		item.EmbeddedTime.SectionFrom = entry.SectionFrom
		item.EmbeddedTime.SectionTo = entry.SectionTo
	}
}

func scheduleRefineCountSuggested(entries []model.HybridScheduleEntry) int {
	count := 0
	for _, entry := range entries {
		if scheduleRefineIsMovableSuggestedTask(entry) {
			count++
		}
	}
	return count
}

func scheduleRefineSummarizeActionLogs(logs []string, tail int) string {
	if len(logs) == 0 {
		return "无"
	}
	if tail <= 0 || len(logs) <= tail {
		return strings.Join(logs, "\n")
	}
	return strings.Join(logs[len(logs)-tail:], "\n")
}

func scheduleRefineFallbackText(text string, fallback string) string {
	clean := strings.TrimSpace(text)
	if clean == "" {
		return fallback
	}
	return clean
}

func scheduleRefineWithNearestJSONContract(userPrompt string, jsonContract string) string {
	base := strings.TrimSpace(userPrompt)
	rule := strings.TrimSpace(jsonContract)
	if rule == "" {
		return base
	}
	if base == "" {
		return rule
	}
	return base + "\n\n" + rule
}

// scheduleRefineAlignSummaryWithHardCheck 对齐总结文案与硬校验事实，避免“通过/失败”口径冲突。
//
// 步骤化说明：
// 1. 先以 hard_check 最终结果作为唯一真值；
// 2. pass=true 且 round_used=0 时，强制输出“未执行动作但已满足”的口径；
// 3. pass=true 但文案含失败词，或 pass=false 但文案含通过词，统一纠偏。
func scheduleRefineAlignSummaryWithHardCheck(st *ScheduleRefineState, summary string) string {
	clean := strings.TrimSpace(summary)
	if st == nil {
		return clean
	}
	passed := FinalHardCheckPassed(st)
	if passed {
		if st.RoundUsed == 0 {
			return "本轮未执行调度动作（0轮），当前排程已满足终审条件。"
		}
		if clean == "" || scheduleRefineContainsAny(clean, []string{"未完全", "未达标", "未能", "差距", "失败", "未通过"}) {
			return fmt.Sprintf("微调已完成，共执行 %d 轮动作，方案已通过终审。", st.RoundUsed)
		}
		return clean
	}

	if clean == "" || scheduleRefineContainsAny(clean, []string{"终审通过", "已通过终审", "完全达成", "全部满足"}) {
		return fmt.Sprintf("已完成微调并返回当前最优结果（执行 %d 轮动作）。终审仍有未满足项：%s。", st.RoundUsed, scheduleRefineFallbackText(st.HardCheck.IntentReason, "请进一步明确微调目标"))
	}
	return clean
}

func scheduleRefineFormatReactPlanStageDetail(round int, out *scheduleRefineReactLLMOutput, remaining int, useThinking bool) string {
	if out == nil {
		return fmt.Sprintf("第 %d 轮：缺少计划输出。", round)
	}
	return fmt.Sprintf("第 %d 轮|thinking=%t|动作剩余=%d|goal_check=%s|decision=%s", round, useThinking, remaining, scheduleRefineTruncate(strings.TrimSpace(out.GoalCheck), 180), scheduleRefineTruncate(strings.TrimSpace(out.Decision), 180))
}

func scheduleRefineFormatReactNeedInfoStageDetail(round int, missing []string) string {
	if len(missing) == 0 {
		return fmt.Sprintf("第 %d 轮|模型缺口信息=无。", round)
	}
	return fmt.Sprintf("第 %d 轮|模型缺口信息=%s", round, strings.Join(scheduleRefineUniqueNonEmpty(missing), "；"))
}

func scheduleRefineFormatReactReflectStageDetail(round int, reflect string) string {
	return fmt.Sprintf("第 %d 轮|复盘=%s", round, scheduleRefineTruncate(strings.TrimSpace(reflect), 260))
}

func scheduleRefineFormatToolCallStageDetail(round int, call scheduleRefineReactToolCall, remaining int) string {
	paramsText := "{}"
	if len(call.Params) > 0 {
		if raw, err := json.Marshal(call.Params); err == nil {
			paramsText = string(raw)
		}
	}
	return fmt.Sprintf("第 %d 轮|调用工具=%s|参数=%s|调用前剩余轮次=%d", round, strings.TrimSpace(call.Tool), scheduleRefineTruncate(paramsText, 320), remaining)
}

func scheduleRefineFormatToolResultStageDetail(round int, result scheduleRefineReactToolResult, used int, total int) string {
	errorCode := strings.TrimSpace(result.ErrorCode)
	if !result.Success && errorCode == "" {
		errorCode = "TOOL_EXEC_FAILED"
	}
	if errorCode == "" {
		errorCode = "NONE"
	}
	return fmt.Sprintf("第 %d 轮|工具=%s|success=%t|error_code=%s|结果=%s|轮次进度=%d/%d", round, strings.TrimSpace(result.Tool), result.Success, errorCode, scheduleRefineTruncate(strings.TrimSpace(result.Result), 320), used, total)
}

func scheduleRefineCondenseSummary(plans []model.UserWeekSchedule) string {
	if len(plans) == 0 {
		return "无历史排程摘要"
	}
	totalEvents := 0
	startWeek := plans[0].Week
	endWeek := plans[0].Week
	for _, week := range plans {
		totalEvents += len(week.Events)
		if week.Week < startWeek {
			startWeek = week.Week
		}
		if week.Week > endWeek {
			endWeek = week.Week
		}
	}
	return fmt.Sprintf("共 %d 周，周次范围 W%d~W%d，事件总数 %d。", len(plans), startWeek, endWeek, totalEvents)
}

func scheduleRefineHybridEntriesToWeekSchedules(entries []model.HybridScheduleEntry) []model.UserWeekSchedule {
	sectionTimeMap := map[int][2]string{
		1: {"08:00", "08:45"}, 2: {"08:55", "09:40"},
		3: {"10:15", "11:00"}, 4: {"11:10", "11:55"},
		5: {"14:00", "14:45"}, 6: {"14:55", "15:40"},
		7: {"16:15", "17:00"}, 8: {"17:10", "17:55"},
		9: {"19:00", "19:45"}, 10: {"19:55", "20:40"},
		11: {"20:50", "21:35"}, 12: {"21:45", "22:30"},
	}
	weekMap := make(map[int][]model.WeeklyEventBrief)
	for _, entry := range entries {
		start, end := "", ""
		if val, ok := sectionTimeMap[entry.SectionFrom]; ok {
			start = val[0]
		}
		if val, ok := sectionTimeMap[entry.SectionTo]; ok {
			end = val[1]
		}
		weekMap[entry.Week] = append(weekMap[entry.Week], model.WeeklyEventBrief{
			ID:        entry.EventID,
			DayOfWeek: entry.DayOfWeek,
			Name:      entry.Name,
			StartTime: start,
			EndTime:   end,
			Type:      entry.Type,
			Span:      entry.SectionTo - entry.SectionFrom + 1,
			Status:    entry.Status,
		})
	}
	result := make([]model.UserWeekSchedule, 0, len(weekMap))
	for week, events := range weekMap {
		result = append(result, model.UserWeekSchedule{Week: week, Events: events})
	}
	sort.SliceStable(result, func(i, j int) bool { return result[i].Week < result[j].Week })
	return result
}

func scheduleRefineBuildFallbackContract(st *ScheduleRefineState) RefineContract {
	intent := strings.TrimSpace(st.UserMessage)
	keepOrder := scheduleRefineDetectOrderIntent(st.UserMessage)
	reqs := append([]string(nil), st.Constraints...)
	if keepOrder {
		reqs = append(reqs, "保持任务原始相对顺序不变")
	}
	assertions := scheduleRefineInferHardAssertionsFromRequest(st.UserMessage, reqs)
	return RefineContract{
		Intent:            intent,
		Strategy:          "local_adjust",
		HardRequirements:  scheduleRefineUniqueNonEmpty(reqs),
		HardAssertions:    assertions,
		KeepRelativeOrder: keepOrder,
		OrderScope:        "global",
	}
}

func scheduleRefineNormalizeStrategy(strategy string) string {
	switch strings.TrimSpace(strings.ToLower(strategy)) {
	case "keep":
		return "keep"
	default:
		return "local_adjust"
	}
}

func scheduleRefineDetectOrderIntent(userMessage string) bool {
	msg := strings.TrimSpace(userMessage)
	if msg == "" {
		return true
	}
	// 1. 默认启用顺序约束，除非用户明确授权可打乱顺序。
	// 2. 这样可避免“用户没提顺序但结果被打乱”的违和体验。
	for _, k := range []string{"可以打乱顺序", "允许打乱顺序", "顺序无所谓", "不考虑顺序", "不用保持顺序", "无需保持顺序", "随便排顺序", "乱序也行"} {
		if strings.Contains(msg, k) {
			return false
		}
	}
	return true
}

func scheduleRefineUniqueNonEmpty(items []string) []string {
	if len(items) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(items))
	out := make([]string, 0, len(items))
	for _, item := range items {
		clean := strings.TrimSpace(item)
		if clean == "" {
			continue
		}
		if _, ok := seen[clean]; ok {
			continue
		}
		seen[clean] = struct{}{}
		out = append(out, clean)
	}
	return out
}

func scheduleRefineBuildObservationPrompt(history []ReactRoundObservation, tail int) string {
	if len(history) == 0 {
		return "无"
	}
	start := 0
	if tail > 0 && len(history) > tail {
		start = len(history) - tail
	}
	raw, err := json.Marshal(history[start:])
	if err != nil {
		return err.Error()
	}
	return string(raw)
}

func scheduleRefineBuildLastToolObservationPrompt(history []ReactRoundObservation) string {
	for i := len(history) - 1; i >= 0; i-- {
		item := history[i]
		if strings.TrimSpace(item.ToolName) == "" {
			continue
		}
		raw, err := json.Marshal(item)
		if err != nil {
			return "无"
		}
		return string(raw)
	}
	return "无"
}

func scheduleRefineBuildToolCallSignature(call scheduleRefineReactToolCall) string {
	paramsText := "{}"
	if len(call.Params) > 0 {
		if raw, err := json.Marshal(call.Params); err == nil {
			paramsText = string(raw)
		}
	}
	return fmt.Sprintf("%s|%s", strings.ToUpper(strings.TrimSpace(call.Tool)), paramsText)
}

func scheduleRefineBuildSlotQuerySignature(st *ScheduleRefineState, params map[string]any) string {
	normalized := scheduleRefineCanonicalizeToolCall(scheduleRefineReactToolCall{Tool: "QueryAvailableSlots", Params: params})
	raw, _ := json.Marshal(normalized.Params)
	version := 0
	if st != nil {
		version = st.EntriesVersion
	}
	return fmt.Sprintf("v=%d|%s", version, string(raw))
}

func scheduleRefineCanonicalizeToolCall(call scheduleRefineReactToolCall) scheduleRefineReactToolCall {
	canonical := scheduleRefineReactToolCall{
		Tool:   strings.TrimSpace(call.Tool),
		Params: scheduleRefineCloneToolParams(call.Params),
	}
	switch canonical.Tool {
	case "Move":
		canonical.Params = scheduleRefineCanonicalizeMoveParams(canonical.Params)
	case "BatchMove":
		canonical.Params = scheduleRefineCanonicalizeBatchMoveParams(canonical.Params)
	case "SpreadEven", "MinContextSwitch":
		canonical.Params = scheduleRefineCanonicalizeCompositeMoveParams(canonical.Params)
	case "QueryAvailableSlots":
		canonical.Params = scheduleRefineCanonicalizeSlotQueryParams(canonical.Params)
	}
	return canonical
}

func scheduleRefineCanonicalizeMoveParams(params map[string]any) map[string]any {
	out := scheduleRefineCloneToolParams(params)
	scheduleRefineSetCanonicalInt(out, "task_item_id", out, "task_item_id", "task_id")
	scheduleRefineSetCanonicalInt(out, "to_week", out, "to_week", "target_week", "new_week", "week")
	scheduleRefineSetCanonicalInt(out, "to_day", out, "to_day", "target_day", "new_day", "target_day_of_week", "day_of_week", "day")
	scheduleRefineSetCanonicalInt(out, "to_section_from", out, "to_section_from", "target_section_from", "new_section_from", "section_from")
	scheduleRefineSetCanonicalInt(out, "to_section_to", out, "to_section_to", "target_section_to", "new_section_to", "section_to")
	return out
}

func scheduleRefineCanonicalizeBatchMoveParams(params map[string]any) map[string]any {
	out := scheduleRefineCloneToolParams(params)
	rawMoves, ok := out["moves"]
	if !ok {
		return out
	}
	moves, ok := rawMoves.([]any)
	if !ok {
		return out
	}
	normalized := make([]any, 0, len(moves))
	for _, item := range moves {
		moveMap, ok := item.(map[string]any)
		if !ok {
			continue
		}
		normalized = append(normalized, scheduleRefineCanonicalizeMoveParams(moveMap))
	}
	out["moves"] = normalized
	return out
}

func scheduleRefineCanonicalizeCompositeMoveParams(params map[string]any) map[string]any {
	out := scheduleRefineCloneToolParams(params)
	ids := scheduleRefineReadIntSlice(out, "task_item_ids", "task_ids")
	if taskID, ok := scheduleRefineParamIntAny(out, "task_item_id", "task_id"); ok {
		ids = append(ids, taskID)
	}
	if len(ids) > 0 {
		out["task_item_ids"] = scheduleRefineUniquePositiveInts(ids)
	}

	scheduleRefineSetCanonicalInt(out, "week", out, "week", "to_week", "target_week", "new_week")
	if day, ok := scheduleRefineParamIntAny(out, "day_of_week", "to_day", "target_day_of_week", "target_day", "new_day", "day"); ok {
		out["day_of_week"] = []int{day}
	}
	if weeks := scheduleRefineReadIntSlice(out, "week_filter", "weeks"); len(weeks) > 0 {
		out["week_filter"] = scheduleRefineUniquePositiveInts(weeks)
	}
	if days := scheduleRefineReadIntSlice(out, "day_of_week", "days", "day_filter"); len(days) > 0 {
		out["day_of_week"] = scheduleRefineUniquePositiveInts(days)
	}
	if sections := scheduleRefineReadIntSlice(out, "exclude_sections", "exclude_section"); len(sections) > 0 {
		out["exclude_sections"] = scheduleRefineUniquePositiveInts(sections)
	}
	return out
}

func scheduleRefineCanonicalizeSlotQueryParams(params map[string]any) map[string]any {
	out := scheduleRefineCloneToolParams(params)
	scheduleRefineSetCanonicalInt(out, "week", out, "week")
	if weeks := scheduleRefineReadIntSlice(out, "week_filter", "weeks"); len(weeks) > 0 {
		out["week_filter"] = scheduleRefineUniquePositiveInts(weeks)
	}
	if days := scheduleRefineReadIntSlice(out, "day_of_week", "days", "day_filter"); len(days) > 0 {
		out["day_filter"] = scheduleRefineUniquePositiveInts(days)
	}
	scheduleRefineSetCanonicalInt(out, "section_duration", out, "section_duration", "span", "task_duration")
	scheduleRefineSetCanonicalInt(out, "section_from", out, "section_from", "target_section_from")
	scheduleRefineSetCanonicalInt(out, "section_to", out, "section_to", "target_section_to")
	scheduleRefineSetCanonicalInt(out, "limit", out, "limit")
	return out
}

func scheduleRefineSetCanonicalInt(dst map[string]any, dstKey string, src map[string]any, keys ...string) {
	if dst == nil || src == nil {
		return
	}
	if value, ok := scheduleRefineParamIntAny(src, keys...); ok {
		dst[dstKey] = value
	}
}

func scheduleRefineListTaskIDsFromToolCall(call scheduleRefineReactToolCall) []int {
	switch strings.TrimSpace(call.Tool) {
	case "Move":
		taskID, ok := scheduleRefineParamIntAny(call.Params, "task_item_id", "task_id")
		if !ok {
			return nil
		}
		return []int{taskID}
	case "Swap":
		taskA, okA := scheduleRefineParamIntAny(call.Params, "task_a", "task_item_a", "task_item_id_a")
		taskB, okB := scheduleRefineParamIntAny(call.Params, "task_b", "task_item_b", "task_item_id_b")
		return scheduleRefineUniquePositiveInts([]int{taskA, taskB}, okA, okB)
	case "BatchMove":
		rawMoves, ok := call.Params["moves"]
		if !ok {
			return nil
		}
		moves, ok := rawMoves.([]any)
		if !ok {
			return nil
		}
		ids := make([]int, 0, len(moves))
		for _, item := range moves {
			moveMap, ok := item.(map[string]any)
			if !ok {
				continue
			}
			if taskID, ok := scheduleRefineParamIntAny(moveMap, "task_item_id", "task_id"); ok {
				ids = append(ids, taskID)
			}
		}
		return scheduleRefineUniquePositiveInts(ids)
	case "SpreadEven", "MinContextSwitch":
		ids := scheduleRefineReadIntSlice(call.Params, "task_item_ids", "task_ids")
		if taskID, ok := scheduleRefineParamIntAny(call.Params, "task_item_id", "task_id"); ok {
			ids = append(ids, taskID)
		}
		return scheduleRefineUniquePositiveInts(ids)
	default:
		return nil
	}
}

func scheduleRefinePrecheckCurrentTaskOwnership(call scheduleRefineReactToolCall, taskIDs []int, currentTaskID int) (scheduleRefineReactToolResult, bool) {
	if currentTaskID <= 0 {
		return scheduleRefineReactToolResult{}, false
	}
	if !scheduleRefineIsMutatingToolName(strings.TrimSpace(call.Tool)) {
		return scheduleRefineReactToolResult{}, false
	}
	for _, id := range taskIDs {
		if id == currentTaskID {
			return scheduleRefineReactToolResult{}, false
		}
	}
	return scheduleRefineReactToolResult{
		Tool:      strings.TrimSpace(call.Tool),
		Success:   false,
		ErrorCode: "CURRENT_TASK_MISMATCH",
		Result:    fmt.Sprintf("当前微循环任务为 id=%d，本轮改写动作未包含该任务，请改为围绕当前任务执行。", currentTaskID),
	}, true
}

func scheduleRefinePrecheckToolCallPolicy(st *ScheduleRefineState, call scheduleRefineReactToolCall, taskIDs []int) (scheduleRefineReactToolResult, bool) {
	if st == nil {
		return scheduleRefineReactToolResult{}, false
	}
	toolName := strings.TrimSpace(call.Tool)
	if st.DisableCompositeTools && scheduleRefineIsCompositeToolName(toolName) {
		return scheduleRefineReactToolResult{
			Tool:      toolName,
			Success:   false,
			ErrorCode: "COMPOSITE_DISABLED",
			Result:    "当前已进入 ReAct 兜底模式，禁止调用复合工具，请使用 Move/Swap 逐步处理。",
		}, true
	}
	if st.DisableCompositeTools && toolName == "BatchMove" {
		return scheduleRefineReactToolResult{
			Tool:      toolName,
			Success:   false,
			ErrorCode: "BATCH_MOVE_DISABLED",
			Result:    "当前兜底模式要求逐任务挪动，禁止使用 BatchMove。",
		}, true
	}
	if toolName == "BatchMove" && !st.BatchMoveAllowed {
		return scheduleRefineReactToolResult{Tool: toolName, Success: false, ErrorCode: "BATCH_MOVE_DISABLED", Result: "当前计划未显式允许 BatchMove，请改用单步 Move/Swap。"}, true
	}
	if toolName == "QueryAvailableSlots" {
		if st.SeenSlotQueries == nil {
			st.SeenSlotQueries = make(map[string]struct{})
		}
		signature := scheduleRefineBuildSlotQuerySignature(st, call.Params)
		if _, exists := st.SeenSlotQueries[signature]; exists {
			return scheduleRefineReactToolResult{
				Tool:      toolName,
				Success:   false,
				ErrorCode: "QUERY_REDUNDANT",
				Result:    "同版本排程下重复查询同一空位范围，已拒绝；请直接基于 ENV_SLOT_HINT 选择落点。",
			}, true
		}
		st.SeenSlotQueries[signature] = struct{}{}
		return scheduleRefineReactToolResult{}, false
	}
	// 1. 当计划声明“必用复合工具”且尚未成功时，先锁住基础写工具。
	// 2. 这样可避免模型绕开复合工具直接 Move，导致“命中率低 + 语义漂移”。
	requiredComposite := scheduleRefineNormalizeCompositeToolName(st.RequiredCompositeTool)
	if requiredComposite != "" && !scheduleRefineIsRequiredCompositeSatisfied(st) && scheduleRefineIsMutatingToolName(toolName) {
		if toolName != requiredComposite {
			return scheduleRefineReactToolResult{
				Tool:      toolName,
				Success:   false,
				ErrorCode: "COMPOSITE_REQUIRED",
				Result:    fmt.Sprintf("当前计划要求先成功调用 %s；在其成功前禁止使用 %s。", requiredComposite, toolName),
			}, true
		}
	}
	if !scheduleRefineIsMutatingToolName(toolName) {
		return scheduleRefineReactToolResult{}, false
	}
	if st.PerTaskBudget <= 0 || len(taskIDs) == 0 {
		return scheduleRefineReactToolResult{}, false
	}
	for _, taskID := range taskIDs {
		if st.TaskActionUsed[taskID] >= st.PerTaskBudget {
			return scheduleRefineReactToolResult{Tool: toolName, Success: false, ErrorCode: "TASK_BUDGET_EXCEEDED", Result: fmt.Sprintf("任务 id=%d 已达到单任务动作预算上限=%d，请重规划或更换目标任务。", taskID, st.PerTaskBudget)}, true
		}
	}
	return scheduleRefineReactToolResult{}, false
}

func scheduleRefineIsMutatingToolName(toolName string) bool {
	switch strings.TrimSpace(toolName) {
	case "Move", "Swap", "BatchMove", "SpreadEven", "MinContextSwitch":
		return true
	default:
		return false
	}
}

func scheduleRefineUniquePositiveInts(ids []int, oks ...bool) []int {
	allowAll := len(oks) == 0
	seen := make(map[int]struct{}, len(ids))
	out := make([]int, 0, len(ids))
	for i, id := range ids {
		if !allowAll {
			if i >= len(oks) || !oks[i] {
				continue
			}
		}
		if id <= 0 {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		out = append(out, id)
	}
	return out
}

func scheduleRefineIsRepeatedFailedCall(st *ScheduleRefineState, signature string) bool {
	if st == nil {
		return false
	}
	current := strings.TrimSpace(signature)
	last := strings.TrimSpace(st.LastFailedCallSignature)
	return current != "" && last != "" && current == last
}

func scheduleRefineNormalizeToolResult(result scheduleRefineReactToolResult) scheduleRefineReactToolResult {
	if result.Success {
		return result
	}
	if strings.TrimSpace(result.ErrorCode) != "" {
		return result
	}
	result.ErrorCode = scheduleRefineClassifyToolFailureCode(result.Result)
	return result
}

func scheduleRefineClassifyToolFailureCode(detail string) string {
	text := strings.TrimSpace(detail)
	switch {
	case strings.Contains(text, "单任务动作预算上限"):
		return "TASK_BUDGET_EXCEEDED"
	case strings.Contains(text, "未显式允许 BatchMove"):
		return "BATCH_MOVE_DISABLED"
	case strings.Contains(text, "重复失败动作"):
		return "REPEAT_FAILED_ACTION"
	case strings.Contains(text, "顺序约束不满足"):
		return "ORDER_VIOLATION"
	case strings.Contains(text, "参数缺失"):
		return "PARAM_MISSING"
	case strings.Contains(text, "目标时段已被"):
		return "SLOT_CONFLICT"
	case strings.Contains(text, "无法唯一定位"):
		return "TASK_ID_AMBIGUOUS"
	case strings.Contains(text, "任务跨度不一致"):
		return "SPAN_MISMATCH"
	case strings.Contains(text, "超出允许窗口"):
		return "OUT_OF_WINDOW"
	case strings.Contains(text, "day_of_week"):
		return "DAY_INVALID"
	case strings.Contains(text, "节次区间"):
		return "SECTION_INVALID"
	case strings.Contains(text, "未找到 task_item_id"):
		return "TASK_NOT_FOUND"
	case strings.Contains(text, "不支持的工具"):
		return "TOOL_NOT_ALLOWED"
	case strings.Contains(text, "BatchMove"):
		return "BATCH_MOVE_FAILED"
	case strings.Contains(text, "Verify"):
		return "VERIFY_FAILED"
	case strings.Contains(text, "序列化查询结果失败"), strings.Contains(text, "序列化空位结果失败"):
		return "QUERY_ENCODE_FAILED"
	default:
		return "TOOL_EXEC_FAILED"
	}
}

func scheduleRefineCloneToolParams(params map[string]any) map[string]any {
	if len(params) == 0 {
		return nil
	}
	raw, err := json.Marshal(params)
	if err != nil {
		dst := make(map[string]any, len(params))
		for k, v := range params {
			dst[k] = v
		}
		return dst
	}
	var out map[string]any
	if err = json.Unmarshal(raw, &out); err != nil {
		dst := make(map[string]any, len(params))
		for k, v := range params {
			dst[k] = v
		}
		return dst
	}
	return out
}

func scheduleRefineFormatRoundModelErrorDetail(round int, err error, parentCtx context.Context) string {
	parentState := "alive"
	if parentCtx == nil {
		parentState = "nil"
	} else if parentCtx.Err() != nil {
		parentState = parentCtx.Err().Error()
	}
	parentDeadline := "none"
	if parentCtx != nil {
		if deadline, ok := parentCtx.Deadline(); ok {
			parentDeadline = fmt.Sprintf("%dms", time.Until(deadline).Milliseconds())
		}
	}
	return fmt.Sprintf("第 %d 轮模型调用失败：%v | parent_ctx=%s | parent_deadline_in_ms=%s | node_timeout_ms=%d", round, err, parentState, parentDeadline, nodeTimeout.Milliseconds())
}

func scheduleRefineBuildRuntimeReflect(modelReflect string, result scheduleRefineReactToolResult) string {
	modelText := strings.TrimSpace(modelReflect)
	resultText := scheduleRefineTruncate(strings.TrimSpace(result.Result), 220)
	if result.Success {
		if modelText == "" {
			return fmt.Sprintf("后端复盘：工具执行成功。%s", resultText)
		}
		return fmt.Sprintf("后端复盘：工具执行成功。%s。模型预期（动作前）：%s", resultText, scheduleRefineTruncate(modelText, 180))
	}
	if modelText == "" {
		return fmt.Sprintf("后端复盘：工具执行失败。%s。本轮调整未生效，已保留原方案。", resultText)
	}
	return fmt.Sprintf("后端复盘：工具执行失败。%s。本轮调整未生效，已保留原方案。模型预期（动作前，仅供参考）：%s", resultText, scheduleRefineTruncate(modelText, 160))
}

func scheduleRefineBuildSuggestedDigest(entries []model.HybridScheduleEntry, limit int) string {
	if len(entries) == 0 {
		return "无"
	}
	list := make([]model.HybridScheduleEntry, 0, len(entries))
	for _, entry := range entries {
		if scheduleRefineIsMovableSuggestedTask(entry) {
			list = append(list, entry)
		}
	}
	if len(list) == 0 {
		return "无 suggested 条目"
	}
	scheduleRefineSortHybridEntries(list)
	if limit <= 0 {
		limit = len(list)
	}
	if len(list) > limit {
		list = list[:limit]
	}
	lines := make([]string, 0, len(list))
	for _, item := range list {
		lines = append(lines, fmt.Sprintf("id=%d|W%d|D%d(%s)|%d-%d|%s", item.TaskItemID, item.Week, item.DayOfWeek, scheduleRefineWeekdayLabel(item.DayOfWeek), item.SectionFrom, item.SectionTo, strings.TrimSpace(item.Name)))
	}
	return strings.Join(lines, "\n")
}

func scheduleRefineBuildSuggestedDigestByWeek(entries []model.HybridScheduleEntry, week int, limit int) string {
	if week <= 0 {
		return scheduleRefineBuildSuggestedDigest(entries, limit)
	}
	filtered := make([]model.HybridScheduleEntry, 0, len(entries))
	for _, entry := range entries {
		if scheduleRefineIsMovableSuggestedTask(entry) && entry.Week == week {
			filtered = append(filtered, entry)
		}
	}
	if len(filtered) == 0 {
		return "无同周 suggested 条目"
	}
	return scheduleRefineBuildSuggestedDigest(filtered, limit)
}

func scheduleRefineWeekdayLabel(day int) string {
	switch day {
	case 1:
		return "周一"
	case 2:
		return "周二"
	case 3:
		return "周三"
	case 4:
		return "周四"
	case 5:
		return "周五"
	case 6:
		return "周六"
	case 7:
		return "周日"
	default:
		return "未知"
	}
}

func scheduleRefineParseReactOutputWithRetryOnce(
	ctx context.Context,
	chatModel *ark.ChatModel,
	userPrompt string,
	firstRaw string,
	round int,
	emitStage func(stage, detail string),
	st *ScheduleRefineState,
) (*scheduleRefineReactLLMOutput, error) {
	if st == nil {
		return nil, respond.ScheduleRefineOutputParseFailed
	}
	parsed, parseErr := parseScheduleRefineLLMOutput(firstRaw)
	if parseErr == nil {
		return parsed, nil
	}
	emitStage("schedule_refine.react.parse_retry", fmt.Sprintf("第 %d 轮输出解析失败，准备重试1次：%s", round, scheduleRefineTruncate(parseErr.Error(), 260)))
	retryRaw, retryErr := scheduleRefineCallModelText(ctx, chatModel, agentprompt.ScheduleRefineReactPrompt, userPrompt, false, reactMaxTokens, 0)
	if retryErr != nil {
		emitStage("schedule_refine.react.round_error", scheduleRefineFormatRoundModelErrorDetail(round, fmt.Errorf("解析重试调用失败: %w", retryErr), ctx))
		return nil, respond.ScheduleRefineOutputParseFailed
	}
	scheduleRefineEmitModelRawDebug(emitStage, fmt.Sprintf("react.round.%d.retry", round), retryRaw)
	retryParsed, retryParseErr := parseScheduleRefineLLMOutput(retryRaw)
	if retryParseErr != nil {
		emitStage("schedule_refine.react.round_error", fmt.Sprintf("第 %d 轮输出二次解析失败：%s", round, scheduleRefineTruncate(retryParseErr.Error(), 260)))
		return nil, respond.ScheduleRefineOutputParseFailed
	}
	emitStage("schedule_refine.react.parse_retry_success", fmt.Sprintf("第 %d 轮输出重试解析成功，继续执行。", round))
	return retryParsed, nil
}

func scheduleRefineParsePlannerOutputWithRetryOnce(
	ctx context.Context,
	chatModel *ark.ChatModel,
	originUserPrompt string,
	firstRaw string,
	mode string,
	emitStage func(stage, detail string),
) (*scheduleRefinePlannerOutput, error) {
	parsed, parseErr := scheduleRefineParseJSON[scheduleRefinePlannerOutput](firstRaw)
	if parseErr == nil {
		return parsed, nil
	}
	emitStage("schedule_refine.plan.parse_retry", fmt.Sprintf("Planner 解析失败，准备重试1次（mode=%s）：%s", strings.TrimSpace(mode), scheduleRefineTruncate(parseErr.Error(), 160)))
	retryPrompt := scheduleRefineWithNearestJSONContract(
		fmt.Sprintf("%s\n\n上一轮输出解析失败（原因：JSON 不完整或不闭合）。请缩短内容并严格输出完整 JSON。", originUserPrompt),
		jsonContractForPlanner,
	)
	retryRaw, retryErr := scheduleRefineCallModelText(ctx, chatModel, agentprompt.ScheduleRefinePlannerPrompt, retryPrompt, false, plannerMaxTokens, 0)
	if retryErr != nil {
		return nil, retryErr
	}
	scheduleRefineEmitModelRawDebug(emitStage, fmt.Sprintf("planner.%s.retry", strings.TrimSpace(mode)), retryRaw)
	retryParsed, retryParseErr := scheduleRefineParseJSON[scheduleRefinePlannerOutput](retryRaw)
	if retryParseErr != nil {
		return nil, retryParseErr
	}
	emitStage("schedule_refine.plan.parse_retry_success", fmt.Sprintf("Planner 重试解析成功（mode=%s）。", strings.TrimSpace(mode)))
	return retryParsed, nil
}

func scheduleRefineBuildSlicePlan(st *ScheduleRefineState) RefineSlicePlan {
	msg := strings.TrimSpace(st.UserMessage)
	lower := strings.ToLower(msg)
	plan := RefineSlicePlan{
		WeekFilter:      scheduleRefineExtractWeekFilters(msg),
		ExcludeSections: scheduleRefineExtractExcludeSections(msg),
		Reason:          "根据用户请求抽取得到执行切片",
	}
	// 1. 优先解析“从A收敛到B”这类方向型表达，防止把 source/target 反向识别。
	// 2. 例如“周四到周五收敛到周一到周三”应得到 source=[4,5], target=[1,2,3]。
	if src, tgt, ok := scheduleRefineExtractDirectionalSourceTargetDays(msg); ok {
		plan.SourceDays = src
		plan.TargetDays = tgt
		return plan
	}
	if strings.Contains(msg, "工作日") || strings.Contains(msg, "周一到周五") || strings.Contains(msg, "周1到周5") {
		plan.TargetDays = []int{1, 2, 3, 4, 5}
	} else if scheduleRefineContainsAny(lower, []string{"移到周末", "挪到周末", "安排在周末", "放到周末"}) {
		plan.TargetDays = []int{6, 7}
	} else if days := scheduleRefineExtractTargetDaysFromMessage(msg); len(days) > 0 {
		plan.TargetDays = days
	}
	if len(plan.TargetDays) == 5 && scheduleRefineIsSameDays(plan.TargetDays, []int{1, 2, 3, 4, 5}) && strings.Contains(msg, "周末") {
		plan.SourceDays = []int{6, 7}
	}
	if day := scheduleRefineDetectOverloadedDay(msg); day > 0 {
		plan.SourceDays = scheduleRefineUniquePositiveInts(append(plan.SourceDays, day))
	}
	if fromDays := scheduleRefineExtractSourceDaysFromMessage(msg); len(fromDays) > 0 {
		plan.SourceDays = scheduleRefineUniquePositiveInts(append(plan.SourceDays, fromDays...))
	}
	return plan
}

// scheduleRefineExtractDirectionalSourceTargetDays 解析“来源日 -> 目标日”表达。
//
// 规则：
// 1. 以“收敛到/移到/挪到/调整到”等方向词为分割；
// 2. 分割前提取 source days，分割后提取 target days；
// 3. 两侧都提取成功才返回 true，避免误判。
func scheduleRefineExtractDirectionalSourceTargetDays(text string) ([]int, []int, bool) {
	verbIdx := -1
	verbLen := 0
	for _, key := range []string{"收敛到", "移到", "挪到", "调整到", "安排到", "放到", "改到", "迁移到", "分散到"} {
		if idx := strings.Index(text, key); idx >= 0 {
			verbIdx = idx
			verbLen = len(key)
			break
		}
	}
	if verbIdx < 0 {
		return nil, nil, false
	}
	left := strings.TrimSpace(text[:verbIdx])
	right := strings.TrimSpace(text[verbIdx+verbLen:])
	if left == "" || right == "" {
		return nil, nil, false
	}
	source := scheduleRefineExtractDayExpr(left)
	target := scheduleRefineExtractDayExpr(right)
	if len(source) == 0 || len(target) == 0 {
		return nil, nil, false
	}
	return source, target, true
}

// scheduleRefineExtractDayExpr 提取文本中的“星期表达式”。
// 优先提取区间（周一到周三），提不到再提取离散天。
func scheduleRefineExtractDayExpr(text string) []int {
	if days := scheduleRefineExtractRangeDays(text); len(days) > 0 {
		return days
	}
	return scheduleRefineExtractDays(text)
}

// scheduleRefineInferSourceWeekSet 推断“来源周”集合。
//
// 规则：
// 1. 当 week_filter 至少两个值时，默认第一个值视为来源周（保留用户原话顺序）；
// 2. 当 week_filter 少于两个值时，不强制来源周过滤，返回空集合；
// 3. 该规则用于收敛 workset，避免把目标周任务误纳入当前微循环。
func scheduleRefineInferSourceWeekSet(slice RefineSlicePlan) map[int]struct{} {
	if len(slice.WeekFilter) < 2 {
		return nil
	}
	sourceWeek := slice.WeekFilter[0]
	if sourceWeek <= 0 {
		return nil
	}
	return map[int]struct{}{sourceWeek: {}}
}

// scheduleRefineInferTargetWeekSet 推断“目标周”集合。
//
// 规则：
// 1. 当 week_filter 至少两个值时，除首个来源周外，其余周视为目标周；
// 2. 当 week_filter 少于两个值时，不构造目标周集合，交由其他约束判定；
// 3. 返回升维集合用于 O(1) 命中判断。
func scheduleRefineInferTargetWeekSet(slice RefineSlicePlan) map[int]struct{} {
	if len(slice.WeekFilter) < 2 {
		return nil
	}
	set := make(map[int]struct{}, len(slice.WeekFilter)-1)
	for _, week := range slice.WeekFilter[1:] {
		if week > 0 {
			set[week] = struct{}{}
		}
	}
	if len(set) == 0 {
		return nil
	}
	return set
}

func scheduleRefineCollectWorksetTaskIDs(entries []model.HybridScheduleEntry, slice RefineSlicePlan, originOrder map[int]int) []int {
	type candidate struct {
		TaskID      int
		Week        int
		Day         int
		SectionFrom int
		Rank        int
	}
	list := make([]candidate, 0, len(entries))
	seen := make(map[int]struct{}, len(entries))
	weekSet := scheduleRefineIntSliceToWeekSet(slice.WeekFilter)
	sourceWeekSet := scheduleRefineInferSourceWeekSet(slice)
	sourceSet := scheduleRefineIntSliceToDaySet(slice.SourceDays)
	for _, entry := range entries {
		if !scheduleRefineIsMovableSuggestedTask(entry) {
			continue
		}
		// 1. 方向型周次请求（例如“14周挪到13周”）下，只把“来源周”任务放入 workset。
		// 2. 这样做可以避免目标周/其他周任务被误当成当前微循环任务，触发串改。
		if len(sourceWeekSet) > 0 {
			if _, ok := sourceWeekSet[entry.Week]; !ok {
				continue
			}
		}
		if len(weekSet) > 0 {
			if _, ok := weekSet[entry.Week]; !ok {
				continue
			}
		}
		if len(sourceSet) > 0 {
			if _, ok := sourceSet[entry.DayOfWeek]; !ok {
				continue
			}
		}
		if _, ok := seen[entry.TaskItemID]; ok {
			continue
		}
		seen[entry.TaskItemID] = struct{}{}
		rank := originOrder[entry.TaskItemID]
		if rank <= 0 {
			rank = 1 << 30
		}
		list = append(list, candidate{
			TaskID:      entry.TaskItemID,
			Week:        entry.Week,
			Day:         entry.DayOfWeek,
			SectionFrom: entry.SectionFrom,
			Rank:        rank,
		})
	}
	sort.SliceStable(list, func(i, j int) bool {
		if list[i].Rank != list[j].Rank {
			return list[i].Rank < list[j].Rank
		}
		if list[i].Week != list[j].Week {
			return list[i].Week < list[j].Week
		}
		if list[i].Day != list[j].Day {
			return list[i].Day < list[j].Day
		}
		if list[i].SectionFrom != list[j].SectionFrom {
			return list[i].SectionFrom < list[j].SectionFrom
		}
		return list[i].TaskID < list[j].TaskID
	})
	ids := make([]int, 0, len(list))
	for _, item := range list {
		ids = append(ids, item.TaskID)
	}
	return ids
}

func scheduleRefineFindSuggestedEntryByTaskID(entries []model.HybridScheduleEntry, taskID int) (model.HybridScheduleEntry, bool) {
	for _, entry := range entries {
		if scheduleRefineIsMovableSuggestedTask(entry) && entry.TaskItemID == taskID {
			return entry, true
		}
	}
	return model.HybridScheduleEntry{}, false
}

// scheduleRefineIsCurrentTaskSatisfiedBySlice 判断“当前任务”是否已满足本轮切片目标。
//
// 步骤化说明：
// 1. 该判断只用于“当前任务自动收口”，不替代全局 hard_check；
// 2. 若切片包含 source_days，则任务离开 source_days 视为关键进展；
// 3. 若切片包含 target_days / exclude_sections / week_filter，则需同时满足；
// 4. 若切片没有任何约束，返回 false，避免误判导致提前结束。
func scheduleRefineIsCurrentTaskSatisfiedBySlice(entry model.HybridScheduleEntry, slice RefineSlicePlan) bool {
	if !scheduleRefineIsMovableSuggestedTask(entry) {
		return false
	}
	weekSet := scheduleRefineIntSliceToWeekSet(slice.WeekFilter)
	sourceWeekSet := scheduleRefineInferSourceWeekSet(slice)
	sourceSet := scheduleRefineIntSliceToDaySet(slice.SourceDays)
	targetSet := scheduleRefineIntSliceToDaySet(slice.TargetDays)
	excludedSet := scheduleRefineIntSliceToSectionSet(slice.ExcludeSections)

	hasConstraint := len(sourceWeekSet) > 0 || len(weekSet) > 0 || len(sourceSet) > 0 || len(targetSet) > 0 || len(excludedSet) > 0
	if !hasConstraint {
		return false
	}
	if len(sourceWeekSet) > 0 {
		if _, stillInSourceWeek := sourceWeekSet[entry.Week]; stillInSourceWeek {
			return false
		}
	}
	if len(weekSet) > 0 {
		if _, ok := weekSet[entry.Week]; !ok {
			return false
		}
	}
	if len(sourceSet) > 0 {
		if _, stillInSource := sourceSet[entry.DayOfWeek]; stillInSource {
			return false
		}
	}
	if len(targetSet) > 0 {
		if _, ok := targetSet[entry.DayOfWeek]; !ok {
			return false
		}
	}
	if len(excludedSet) > 0 && scheduleRefineIntersectsExcludedSections(entry.SectionFrom, entry.SectionTo, excludedSet) {
		return false
	}
	return true
}

func scheduleRefineTaskProgressLabel(done bool, attemptUsed int, perTaskBudget int) string {
	if done {
		return "done"
	}
	if perTaskBudget > 0 && attemptUsed >= perTaskBudget {
		return "budget_exhausted"
	}
	return "paused"
}

func scheduleRefineBuildMicroReactUserPrompt(st *ScheduleRefineState, current model.HybridScheduleEntry, remainingAction int, remainingTotal int) string {
	scheduleRefineEnsureCompositeStateMaps(st)
	contractJSON, _ := json.Marshal(st.Contract)
	planJSON, _ := json.Marshal(st.CurrentPlan)
	sliceJSON, _ := json.Marshal(st.SlicePlan)
	objectiveJSON, _ := json.Marshal(st.Objective)
	currentJSON, _ := json.Marshal(current)
	sourceWeeks := scheduleRefineKeysOfIntSet(scheduleRefineInferSourceWeekSet(st.SlicePlan))
	requiredComposite := scheduleRefineNormalizeCompositeToolName(st.RequiredCompositeTool)
	requiredSuccess := scheduleRefineIsRequiredCompositeSatisfied(st)
	compositeToolsAllowed := !st.DisableCompositeTools
	compositeCalledJSON, _ := json.Marshal(st.CompositeToolCalled)
	compositeSuccessJSON, _ := json.Marshal(st.CompositeToolSuccess)
	envSlotHint := scheduleRefineBuildEnvSlotHint(st, current)
	userPrompt := fmt.Sprintf(
		"用户本轮请求=%s\n契约=%s\n执行计划=%s\n切片=%s\n目标约束=%s\nCURRENT_TASK=%s\nSOURCE_WEEK_FILTER=%v\nBACKEND_GUARD=本轮只允许改写 task_item_id=%d；若该任务已满足切片目标或目标约束已整体达成且复合工具门禁通过，请直接 done=true；下一任务由后端自动切换。\nREQUIRED_COMPOSITE_TOOL=%s\nCOMPOSITE_TOOLS_ALLOWED=%t\nCOMPOSITE_REQUIRED_SUCCESS=%t\nCOMPOSITE_CALLED=%s\nCOMPOSITE_SUCCESS=%s\nCURRENT_TASK_ACTION_USED=%d\nPER_TASK_BUDGET=%d\n动作预算剩余=%d\n总预算剩余=%d\nENV_SLOT_HINT=%s\nLAST_TOOL_OBSERVATION=%s\nLAST_FAILED_CALL_SIGNATURE=%s\n最近观察=%s\n同周suggested摘要=%s",
		strings.TrimSpace(st.UserMessage),
		string(contractJSON),
		string(planJSON),
		string(sliceJSON),
		string(objectiveJSON),
		string(currentJSON),
		sourceWeeks,
		current.TaskItemID,
		scheduleRefineFallbackText(requiredComposite, "无"),
		compositeToolsAllowed,
		requiredSuccess,
		string(compositeCalledJSON),
		string(compositeSuccessJSON),
		st.TaskActionUsed[current.TaskItemID],
		st.PerTaskBudget,
		remainingAction,
		remainingTotal,
		envSlotHint,
		scheduleRefineBuildLastToolObservationPrompt(st.ObservationHistory),
		scheduleRefineFallbackText(st.LastFailedCallSignature, "无"),
		scheduleRefineBuildObservationPrompt(st.ObservationHistory, 2),
		scheduleRefineBuildSuggestedDigestByWeek(st.HybridEntries, current.Week, 24),
	)
	return scheduleRefineWithNearestJSONContract(userPrompt, jsonContractForReact)
}

type scheduleRefineSlotHintPayload struct {
	Count         int `json:"count"`
	StrictCount   int `json:"strict_count"`
	EmbeddedCount int `json:"embedded_count"`
	Slots         []struct {
		Week        int `json:"week"`
		DayOfWeek   int `json:"day_of_week"`
		SectionFrom int `json:"section_from"`
		SectionTo   int `json:"section_to"`
	} `json:"slots"`
}

func scheduleRefineBuildEnvSlotHint(st *ScheduleRefineState, current model.HybridScheduleEntry) string {
	if st == nil || !scheduleRefineIsMovableSuggestedTask(current) {
		return "无可用提示"
	}
	span := current.SectionTo - current.SectionFrom + 1
	if span <= 0 {
		span = 2
	}
	targetWeeks := append([]int(nil), st.Objective.TargetWeeks...)
	if len(targetWeeks) == 0 {
		targetWeeks = scheduleRefineKeysOfIntSet(scheduleRefineInferTargetWeekSet(st.SlicePlan))
	}
	if len(targetWeeks) == 0 && current.Week > 0 {
		targetWeeks = []int{current.Week}
	}
	targetDays := append([]int(nil), st.Objective.TargetDays...)
	if len(targetDays) == 0 {
		targetDays = append([]int(nil), st.SlicePlan.TargetDays...)
	}
	params := map[string]any{
		"week_filter":      targetWeeks,
		"day_filter":       targetDays,
		"section_duration": span,
		"limit":            8,
		"slot_type":        "pure",
		"exclude_sections": st.SlicePlan.ExcludeSections,
	}
	_, pureResult := scheduleRefineToolQueryAvailableSlots(st.HybridEntries, params, scheduleRefineBuildPlanningWindowFromEntries(st.HybridEntries))
	if !pureResult.Success {
		return fmt.Sprintf("pure_slot_query_failed=%s", scheduleRefineTruncate(pureResult.Result, 100))
	}
	purePayload, ok := scheduleRefineDecodeSlotHintPayload(pureResult.Result)
	if !ok {
		return "pure_slot_parse_failed"
	}

	embedParams := map[string]any{
		"week_filter":      targetWeeks,
		"day_filter":       targetDays,
		"section_duration": span,
		"limit":            8,
		"exclude_sections": st.SlicePlan.ExcludeSections,
	}
	_, fallbackResult := scheduleRefineToolQueryAvailableSlots(st.HybridEntries, embedParams, scheduleRefineBuildPlanningWindowFromEntries(st.HybridEntries))
	if !fallbackResult.Success {
		return fmt.Sprintf("pure=%d fallback_query_failed=%s", purePayload.Count, scheduleRefineTruncate(fallbackResult.Result, 100))
	}
	fallbackPayload, ok := scheduleRefineDecodeSlotHintPayload(fallbackResult.Result)
	if !ok {
		return fmt.Sprintf("pure=%d fallback_parse_failed", purePayload.Count)
	}

	top := fallbackPayload.Slots
	if len(top) > 3 {
		top = top[:3]
	}
	slotText := make([]string, 0, len(top))
	for _, item := range top {
		slotText = append(slotText, fmt.Sprintf("W%dD%d %d-%d", item.Week, item.DayOfWeek, item.SectionFrom, item.SectionTo))
	}
	if len(slotText) == 0 {
		slotText = append(slotText, "无")
	}
	return fmt.Sprintf("target_weeks=%v target_days=%v pure=%d embed_candidate=%d top=%s", targetWeeks, targetDays, purePayload.Count, fallbackPayload.EmbeddedCount, strings.Join(slotText, ","))
}

func scheduleRefineDecodeSlotHintPayload(raw string) (scheduleRefineSlotHintPayload, bool) {
	var payload scheduleRefineSlotHintPayload
	if err := json.Unmarshal([]byte(raw), &payload); err != nil {
		return scheduleRefineSlotHintPayload{}, false
	}
	return payload, true
}

func scheduleRefineExtractWeekFilters(text string) []int {
	patterns := []string{
		`第\s*(\d{1,2})\s*周`,
		`W\s*(\d{1,2})`,
		`(\d{1,2})\s*周`,
	}
	out := make([]int, 0, 8)
	for _, p := range patterns {
		re := regexp.MustCompile(p)
		for _, m := range re.FindAllStringSubmatch(text, -1) {
			if len(m) < 2 {
				continue
			}
			v, err := strconv.Atoi(strings.TrimSpace(m[1]))
			if err != nil || v <= 0 {
				continue
			}
			out = append(out, v)
		}
	}
	return scheduleRefineUniquePositiveInts(out)
}

func scheduleRefineExtractExcludeSections(text string) []int {
	normalized := strings.ReplaceAll(strings.ToLower(text), " ", "")
	if scheduleRefineContainsAny(normalized, []string{
		"不要早八", "避开早八", "不想早八", "别在早八",
		"不要1-2", "避开1-2", "不要第一节", "不要一二节",
	}) {
		return []int{1, 2}
	}
	return nil
}

func scheduleRefineExtractTargetDaysFromMessage(text string) []int {
	verbIdx := -1
	for _, key := range []string{"移到", "挪到", "改到", "安排到", "放到", "分散到", "调整到", "收敛到", "迁移到"} {
		if idx := strings.Index(text, key); idx >= 0 {
			verbIdx = idx + len(key)
			break
		}
	}
	if verbIdx < 0 || verbIdx >= len(text) {
		return nil
	}
	targetPart := strings.TrimSpace(text[verbIdx:])
	return scheduleRefineExtractDayExpr(targetPart)
}

func scheduleRefineExtractSourceDaysFromMessage(text string) []int {
	source := make([]int, 0, 4)
	re := regexp.MustCompile(`从\s*(周[一二三四五六日天]|星期[一二三四五六日天])`)
	for _, m := range re.FindAllStringSubmatch(text, -1) {
		if len(m) < 2 {
			continue
		}
		if day := scheduleRefineDayTokenToInt(m[1]); day > 0 {
			source = append(source, day)
		}
	}
	re2 := regexp.MustCompile(`把\s*(周[一二三四五六日天]|星期[一二三四五六日天])`)
	for _, m := range re2.FindAllStringSubmatch(text, -1) {
		if len(m) < 2 {
			continue
		}
		if day := scheduleRefineDayTokenToInt(m[1]); day > 0 {
			source = append(source, day)
		}
	}
	return scheduleRefineUniquePositiveInts(source)
}

func scheduleRefineDetectOverloadedDay(text string) int {
	re := regexp.MustCompile(`(周[一二三四五六日天]|星期[一二三四五六日天]).{0,8}(太多|过多|太满|过满|拥挤|太挤|塞满)`)
	m := re.FindStringSubmatch(text)
	if len(m) < 2 {
		return 0
	}
	return scheduleRefineDayTokenToInt(m[1])
}

func scheduleRefineExtractRangeDays(text string) []int {
	re := regexp.MustCompile(`(周[一二三四五六日天]|星期[一二三四五六日天])\s*[到至\-]\s*(周[一二三四五六日天]|星期[一二三四五六日天])`)
	m := re.FindStringSubmatch(text)
	if len(m) < 3 {
		return nil
	}
	start := scheduleRefineDayTokenToInt(m[1])
	end := scheduleRefineDayTokenToInt(m[2])
	if start <= 0 || end <= 0 {
		return nil
	}
	if start > end {
		start, end = end, start
	}
	out := make([]int, 0, end-start+1)
	for day := start; day <= end; day++ {
		out = append(out, day)
	}
	return out
}

func scheduleRefineExtractDays(text string) []int {
	re := regexp.MustCompile(`周[一二三四五六日天]|星期[一二三四五六日天]`)
	matches := re.FindAllString(text, -1)
	days := make([]int, 0, len(matches))
	for _, token := range matches {
		if day := scheduleRefineDayTokenToInt(token); day > 0 {
			days = append(days, day)
		}
	}
	return scheduleRefineUniquePositiveInts(days)
}

func scheduleRefineDayTokenToInt(token string) int {
	switch strings.TrimSpace(token) {
	case "周一", "星期一":
		return 1
	case "周二", "星期二":
		return 2
	case "周三", "星期三":
		return 3
	case "周四", "星期四":
		return 4
	case "周五", "星期五":
		return 5
	case "周六", "星期六":
		return 6
	case "周日", "周天", "星期日", "星期天":
		return 7
	default:
		return 0
	}
}

func scheduleRefineContainsAny(text string, keys []string) bool {
	for _, k := range keys {
		if strings.Contains(text, k) {
			return true
		}
	}
	return false
}

func scheduleRefineIsSameDays(days []int, target []int) bool {
	if len(days) != len(target) {
		return false
	}
	for i := range days {
		if days[i] != target[i] {
			return false
		}
	}
	return true
}
