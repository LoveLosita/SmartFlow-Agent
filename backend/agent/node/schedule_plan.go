package agentnode

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	agentllm "github.com/LoveLosita/smartflow/backend/agent/llm"
	agentmodel "github.com/LoveLosita/smartflow/backend/agent/model"
	agentprompt "github.com/LoveLosita/smartflow/backend/agent/prompt"
	agentshared "github.com/LoveLosita/smartflow/backend/agent/shared"
	"github.com/LoveLosita/smartflow/backend/model"
	"github.com/cloudwego/eino-ext/components/model/ark"
	"github.com/cloudwego/eino/schema"
)

const (
	// SchedulePlanGraphNodePlan 是“识别排程意图与约束”的节点名。
	SchedulePlanGraphNodePlan = "schedule_plan_plan"
	// SchedulePlanGraphNodeRoughBuild 是“粗排构建”的节点名。
	SchedulePlanGraphNodeRoughBuild = "schedule_plan_rough_build"
	// SchedulePlanGraphNodeExit 是“提前退出”的节点名。
	SchedulePlanGraphNodeExit = "schedule_plan_exit"
	// SchedulePlanGraphNodeDailySplit 是“按天拆分”的节点名。
	SchedulePlanGraphNodeDailySplit = "schedule_plan_daily_split"
	// SchedulePlanGraphNodeQuickRefine 是“小改动快速微调”的节点名。
	SchedulePlanGraphNodeQuickRefine = "schedule_plan_quick_refine"
	// SchedulePlanGraphNodeDailyRefine 是“并发日内优化”的节点名。
	SchedulePlanGraphNodeDailyRefine = "schedule_plan_daily_refine"
	// SchedulePlanGraphNodeMerge 是“合并日内优化结果”的节点名。
	SchedulePlanGraphNodeMerge = "schedule_plan_merge"
	// SchedulePlanGraphNodeWeeklyRefine 是“周级配平优化”的节点名。
	SchedulePlanGraphNodeWeeklyRefine = "schedule_plan_weekly_refine"
	// SchedulePlanGraphNodeFinalCheck 是“终审校验”的节点名。
	SchedulePlanGraphNodeFinalCheck = "schedule_plan_final_check"
	// SchedulePlanGraphNodeReturnPreview 是“返回预览结果”的节点名。
	SchedulePlanGraphNodeReturnPreview = "schedule_plan_return_preview"
)

const (
	schedulePlanGraphNodePlan          = SchedulePlanGraphNodePlan
	schedulePlanGraphNodeRoughBuild    = SchedulePlanGraphNodeRoughBuild
	schedulePlanGraphNodeExit          = SchedulePlanGraphNodeExit
	schedulePlanGraphNodeDailySplit    = SchedulePlanGraphNodeDailySplit
	schedulePlanGraphNodeQuickRefine   = SchedulePlanGraphNodeQuickRefine
	schedulePlanGraphNodeDailyRefine   = SchedulePlanGraphNodeDailyRefine
	schedulePlanGraphNodeMerge         = SchedulePlanGraphNodeMerge
	schedulePlanGraphNodeWeeklyRefine  = SchedulePlanGraphNodeWeeklyRefine
	schedulePlanGraphNodeFinalCheck    = SchedulePlanGraphNodeFinalCheck
	schedulePlanGraphNodeReturnPreview = SchedulePlanGraphNodeReturnPreview
)

const (
	schedulePlanDefaultDailyRefineConcurrency  = agentmodel.SchedulePlanDefaultDailyRefineConcurrency
	schedulePlanDefaultWeeklyAdjustBudget      = agentmodel.SchedulePlanDefaultWeeklyAdjustBudget
	schedulePlanDefaultWeeklyTotalBudget       = agentmodel.SchedulePlanDefaultWeeklyTotalBudget
	schedulePlanDefaultWeeklyRefineConcurrency = agentmodel.SchedulePlanDefaultWeeklyRefineConcurrency
	schedulePlanAdjustmentScopeSmall           = agentmodel.SchedulePlanAdjustmentScopeSmall
	schedulePlanAdjustmentScopeMedium          = agentmodel.SchedulePlanAdjustmentScopeMedium
	schedulePlanAdjustmentScopeLarge           = agentmodel.SchedulePlanAdjustmentScopeLarge
)

type (
	// SchedulePlanState 是 node 层对排程状态的本地别名。
	// 这样做的目的，是让节点文件在迁移期保持旧逻辑可读，不需要把每个类型都写成长前缀。
	SchedulePlanState = agentmodel.SchedulePlanState
	// DayGroup 是按天拆分后的最小优化单元别名。
	DayGroup = agentmodel.DayGroup
)

// SchedulePlanGraphRunInput 是执行“智能排程 graph”所需输入。
//
// 字段说明：
// 1. Extra：前端附加参数（重点是 task_class_ids）；
// 2. ChatHistory：支持连续对话微调；
// 3. OutChan/ModelName：保留兼容字段（当前 weekly refine 主要输出阶段状态）；
// 4. DailyRefineConcurrency/WeeklyAdjustBudget：可选运行参数覆盖。
type SchedulePlanGraphRunInput struct {
	Model       *ark.ChatModel
	State       *agentmodel.SchedulePlanState
	Deps        SchedulePlanToolDeps
	UserMessage string
	Extra       map[string]any
	ChatHistory []*schema.Message
	EmitStage   func(stage, detail string)

	OutChan   chan<- string
	ModelName string

	DailyRefineConcurrency int
	WeeklyAdjustBudget     int
}

// SchedulePlanNodes 是“首次排程”图的节点容器。
//
// 职责边界：
// 1. 负责收口请求级依赖（model / extra / history / stage emitter）；
// 2. 负责向 graph 层暴露可直接挂载的方法；
// 3. 不负责 graph 编译，也不负责 service 层接线。
type SchedulePlanNodes struct {
	input     SchedulePlanGraphRunInput
	emitStage func(stage, detail string)
}

// NewSchedulePlanNodes 创建排程节点容器。
//
// 职责边界：
// 1. 负责校验“图运行的最小依赖”是否齐全；
// 2. 负责把空的阶段回调收敛成 no-op，避免节点内部到处判空；
// 3. 不负责调整 state 业务字段，state 预处理由 graph 层完成。
func NewSchedulePlanNodes(input SchedulePlanGraphRunInput) (*SchedulePlanNodes, error) {
	if input.Model == nil {
		return nil, errors.New("schedule plan nodes: model is nil")
	}
	if input.State == nil {
		return nil, errors.New("schedule plan nodes: state is nil")
	}
	if err := input.Deps.Validate(); err != nil {
		return nil, err
	}

	emitStage := input.EmitStage
	if emitStage == nil {
		emitStage = func(stage, detail string) {}
	}
	return &SchedulePlanNodes{
		input:     input,
		emitStage: emitStage,
	}, nil
}

// Plan 负责承接“排程意图分析”节点。
func (n *SchedulePlanNodes) Plan(ctx context.Context, st *agentmodel.SchedulePlanState) (*agentmodel.SchedulePlanState, error) {
	return runPlanNode(ctx, st, n.input.Model, n.input.UserMessage, n.input.Extra, n.input.ChatHistory, n.emitStage)
}

// RoughBuild 负责承接“粗排构建”节点。
func (n *SchedulePlanNodes) RoughBuild(ctx context.Context, st *agentmodel.SchedulePlanState) (*agentmodel.SchedulePlanState, error) {
	return runRoughBuildNode(ctx, st, n.input.Deps, n.emitStage)
}

// DailySplit 负责承接“按天拆分”节点。
func (n *SchedulePlanNodes) DailySplit(ctx context.Context, st *agentmodel.SchedulePlanState) (*agentmodel.SchedulePlanState, error) {
	return runDailySplitNode(ctx, st, n.emitStage)
}

// QuickRefine 负责承接“小改动快速微调”节点。
func (n *SchedulePlanNodes) QuickRefine(ctx context.Context, st *agentmodel.SchedulePlanState) (*agentmodel.SchedulePlanState, error) {
	return runQuickRefineNode(ctx, st, n.emitStage)
}

// DailyRefine 负责承接“并发日内优化”节点。
func (n *SchedulePlanNodes) DailyRefine(ctx context.Context, st *agentmodel.SchedulePlanState) (*agentmodel.SchedulePlanState, error) {
	return runDailyRefineNode(ctx, st, n.input.Model, n.input.DailyRefineConcurrency, n.emitStage)
}

// Merge 负责承接“合并日内优化结果”节点。
func (n *SchedulePlanNodes) Merge(ctx context.Context, st *agentmodel.SchedulePlanState) (*agentmodel.SchedulePlanState, error) {
	return runMergeNode(ctx, st, n.emitStage)
}

// WeeklyRefine 负责承接“周级配平优化”节点。
func (n *SchedulePlanNodes) WeeklyRefine(ctx context.Context, st *agentmodel.SchedulePlanState) (*agentmodel.SchedulePlanState, error) {
	return runWeeklyRefineNode(ctx, st, n.input.Model, n.input.OutChan, n.input.ModelName, n.emitStage)
}

// FinalCheck 负责承接“终审校验”节点。
func (n *SchedulePlanNodes) FinalCheck(ctx context.Context, st *agentmodel.SchedulePlanState) (*agentmodel.SchedulePlanState, error) {
	return runFinalCheckNode(ctx, st, n.input.Model, n.emitStage)
}

// ReturnPreview 负责承接“生成结构化预览输出”节点。
func (n *SchedulePlanNodes) ReturnPreview(ctx context.Context, st *agentmodel.SchedulePlanState) (*agentmodel.SchedulePlanState, error) {
	return runReturnPreviewNode(ctx, st, n.emitStage)
}

// Exit 是图中的显式退出节点。
//
// 职责边界：
// 1. 只作为图收口占位，保持状态原样透传；
// 2. 不做额外副作用，避免“退出节点偷偷改状态”。
func (n *SchedulePlanNodes) Exit(ctx context.Context, st *agentmodel.SchedulePlanState) (*agentmodel.SchedulePlanState, error) {
	_ = ctx
	return st, nil
}

// NextAfterPlan 根据 plan 节点结果决定下一步。
func (n *SchedulePlanNodes) NextAfterPlan(ctx context.Context, st *agentmodel.SchedulePlanState) (string, error) {
	_ = ctx
	return selectNextAfterPlan(st), nil
}

// NextAfterRoughBuild 根据粗排构建结果决定后续路径。
//
// 规则：
// 1. 没有可优化条目 -> exit；
// 2. 连续微调且判定为 small -> quickRefine；
// 3. 连续微调且判定为 medium -> weeklyRefine；
// 4. large 或非微调：多任务类走 dailySplit，单任务类直达 weeklyRefine。
func (n *SchedulePlanNodes) NextAfterRoughBuild(ctx context.Context, st *agentmodel.SchedulePlanState) (string, error) {
	_ = ctx
	if st == nil || len(st.HybridEntries) == 0 {
		return SchedulePlanGraphNodeExit, nil
	}
	if st.IsAdjustment && st.AdjustmentScope == schedulePlanAdjustmentScopeSmall {
		return SchedulePlanGraphNodeQuickRefine, nil
	}
	if st.IsAdjustment && st.AdjustmentScope == schedulePlanAdjustmentScopeMedium {
		return SchedulePlanGraphNodeWeeklyRefine, nil
	}
	if len(st.TaskClassIDs) >= 2 {
		return SchedulePlanGraphNodeDailySplit, nil
	}
	return SchedulePlanGraphNodeWeeklyRefine, nil
}

// normalizeAdjustmentScope 统一把微调力度归一化到 small/medium/large。
//
// 调用目的：
// 1. 旧 scheduleplan 节点逻辑已经大量直接调用这个函数名；
// 2. 迁到 Agent 后，这里保留同名收口，避免节点层到处散落包前缀；
// 3. 真正的归一化规则仍以下层 model 层为准，避免多处维护。
func normalizeAdjustmentScope(raw string) string {
	return agentmodel.NormalizeSchedulePlanAdjustmentScope(raw)
}

// schedulePlanIntentOutput 是 plan 节点要求模型返回的结构化结果。
//
// 兼容说明：
//  1. 新主语义是 task_class_ids（数组）；
//  2. 为兼容旧 prompt/旧缓存输出，保留 task_class_id（单值）兜底解析；
//  3. TaskTags 的 key 兼容两种写法：
//     3.1 推荐：task_item_id（例如 "12"）；
//     3.2 兼容：任务名称（例如 "高数复习"）。
type schedulePlanIntentOutput = agentllm.ScheduleIntentOutput

// runPlanNode 负责“识别排程意图 + 提取约束 + 收敛任务类 ID”。
//
// 职责边界：
// 1. 负责把用户自然语言和 extra 参数收敛为统一状态；
// 2. 负责输出后续节点需要的最小上下文（TaskClassIDs/约束/策略/标签）；
// 3. 不负责调用粗排算法，不负责写库。
func runPlanNode(
	ctx context.Context,
	st *SchedulePlanState,
	chatModel *ark.ChatModel,
	userMessage string,
	extra map[string]any,
	chatHistory []*schema.Message,
	emitStage func(stage, detail string),
) (*SchedulePlanState, error) {
	if st == nil {
		return nil, errors.New("schedule plan graph: nil state in plan node")
	}
	st.RestartRequested = false
	st.AdjustmentReason = ""
	st.AdjustmentConfidence = 0
	st.AdjustmentScope = schedulePlanAdjustmentScopeLarge

	emitStage("schedule_plan.plan.analyzing", "正在分析你的排程需求。")

	// 1. 先收敛 extra 中显式传入的任务类 ID（优先级高于模型推断）。
	// 1.1 先读 task_class_ids 数组；
	// 1.2 再兼容读取单值 task_class_id；
	// 1.3 最后统一做过滤 + 去重，防止非法值或重复值污染状态机。
	if extra != nil {
		mergedIDs := make([]int, 0, len(st.TaskClassIDs)+2)
		mergedIDs = append(mergedIDs, st.TaskClassIDs...)
		if tcIDs, ok := ExtraIntSlice(extra, "task_class_ids"); ok {
			mergedIDs = append(mergedIDs, tcIDs...)
		}
		if tcID, ok := ExtraInt(extra, "task_class_id"); ok && tcID > 0 {
			mergedIDs = append(mergedIDs, tcID)
		}
		st.TaskClassIDs = normalizeTaskClassIDs(mergedIDs)
	}
	// 1.4 若本轮请求没带 task_class_ids，但会话里存在上一次排程快照，则用快照中的任务类兜底。
	// 1.4.1 这样用户可以直接说“把周三晚上的高数挪到周五”，无需每轮都重复传任务类集合；
	// 1.4.2 失败兜底：若快照也没有任务类，后续按原逻辑处理（可能提前退出并提示补参）。
	if len(st.TaskClassIDs) == 0 && len(st.PreviousTaskClassIDs) > 0 {
		st.TaskClassIDs = normalizeTaskClassIDs(append([]int(nil), st.PreviousTaskClassIDs...))
	}

	// 2. 识别“是否为连续对话微调”场景。
	// 2.1 只做历史探测，不做历史改写；
	// 2.2 探测失败不影响主链路，只是少一个 prompt hint。
	if st.HasPreviousPreview && len(st.PreviousHybridEntries) > 0 {
		st.IsAdjustment = true
		st.AdjustmentScope = schedulePlanAdjustmentScopeMedium
	}
	previousPlan := extractPreviousPlanFromHistory(chatHistory)
	if previousPlan != "" {
		st.PreviousPlanJSON = previousPlan
		st.IsAdjustment = true
		st.AdjustmentScope = schedulePlanAdjustmentScopeMedium
	}

	// 3. 组装模型提示词。
	adjustmentHint := ""
	if st.IsAdjustment {
		adjustmentHint = "\n注意：这是对已有排程的微调请求，请重点抽取本次新增或变更的约束。"
	}
	prompt := fmt.Sprintf(
		"当前时间（北京时间）：%s\n用户输入：%s%s\n\n请提取排程意图与约束。",
		st.RequestNowText,
		strings.TrimSpace(userMessage),
		adjustmentHint,
	)

	// 4. 调模型拿结构化输出。
	// 4.1 如果失败但已经有 TaskClassIDs，则降级继续；
	// 4.2 如果失败且没有任务类 ID，直接给出可执行错误提示。
	raw, callErr := callScheduleModelForJSON(ctx, chatModel, agentprompt.SchedulePlanIntentPrompt, prompt, 256)
	if callErr != nil {
		if len(st.TaskClassIDs) > 0 {
			st.UserIntent = strings.TrimSpace(userMessage)
			emitStage("schedule_plan.plan.fallback", "意图识别失败，已使用请求参数兜底继续。")
			return st, nil
		}
		st.FinalSummary = "抱歉，我没拿到有效的任务类信息。请在请求中传入 task_class_ids。"
		return st, nil
	}

	parsed, parseErr := parseScheduleJSON[schedulePlanIntentOutput](raw)
	if parseErr != nil {
		if len(st.TaskClassIDs) > 0 {
			st.UserIntent = strings.TrimSpace(userMessage)
			emitStage("schedule_plan.plan.fallback", "模型返回解析失败，已使用请求参数兜底继续。")
			return st, nil
		}
		st.FinalSummary = "抱歉，我没能解析排程意图。请重试，或直接传入 task_class_ids。"
		return st, nil
	}

	// 5. 回填基础字段。
	st.UserIntent = strings.TrimSpace(parsed.Intent)
	if st.UserIntent == "" {
		st.UserIntent = strings.TrimSpace(userMessage)
	}
	if len(parsed.Constraints) > 0 {
		st.Constraints = parsed.Constraints
	}
	if strings.EqualFold(strings.TrimSpace(parsed.Strategy), "rapid") {
		st.Strategy = "rapid"
	}
	st.RestartRequested = parsed.Restart
	st.AdjustmentScope = normalizeAdjustmentScope(parsed.AdjustmentScope)
	st.AdjustmentReason = strings.TrimSpace(parsed.Reason)
	st.AdjustmentConfidence = clampAdjustmentConfidence(parsed.Confidence)

	// 5.1 分级语义兜底：
	// 5.1.1 非微调请求不走 small/medium，强制按 large 进入完整排程；
	// 5.1.2 微调请求默认至少走 medium，避免 scope 缺失时误判；
	// 5.1.3 restart=true 时强制重排并清空历史快照承接。
	if !st.IsAdjustment {
		st.AdjustmentScope = schedulePlanAdjustmentScopeLarge
	} else if st.AdjustmentScope == "" {
		st.AdjustmentScope = schedulePlanAdjustmentScopeMedium
	}
	if st.RestartRequested {
		st.IsAdjustment = false
		st.AdjustmentScope = schedulePlanAdjustmentScopeLarge
		clearPreviousPreviewContext(st)
	}

	// 6. 合并任务类 ID（新字段 + 旧字段双兼容）。
	// 6.1 先拼接已有值与模型输出；
	// 6.2 再统一清洗，保证后续节点使用稳定语义。
	mergedIDs := make([]int, 0, len(st.TaskClassIDs)+len(parsed.TaskClassIDs)+1)
	mergedIDs = append(mergedIDs, st.TaskClassIDs...)
	mergedIDs = append(mergedIDs, parsed.TaskClassIDs...)
	if parsed.TaskClassID > 0 {
		mergedIDs = append(mergedIDs, parsed.TaskClassID)
	}
	st.TaskClassIDs = normalizeTaskClassIDs(mergedIDs)

	// 7. 回填任务标签映射（给 daily_split 注入 context_tag 用）。
	// 7.1 TaskTags（按 task_item_id）优先；
	// 7.2 无法转成 ID 的 key 先存到 TaskTagHintsByName，等 roughBuild 阶段再映射；
	// 7.3 单条标签解析失败不影响主流程。
	if st.TaskTags == nil {
		st.TaskTags = make(map[int]string)
	}
	if st.TaskTagHintsByName == nil {
		st.TaskTagHintsByName = make(map[string]string)
	}
	for rawKey, rawTag := range parsed.TaskTags {
		tag := normalizeContextTag(rawTag)
		key := strings.TrimSpace(rawKey)
		if key == "" {
			continue
		}
		if id, convErr := strconv.Atoi(key); convErr == nil && id > 0 {
			st.TaskTags[id] = tag
			continue
		}
		st.TaskTagHintsByName[key] = tag
	}

	emitStage(
		"schedule_plan.plan.done",
		fmt.Sprintf(
			"已识别排程意图，任务类数量=%d，微调=%t，力度=%s，重排=%t。",
			len(st.TaskClassIDs),
			st.IsAdjustment,
			st.AdjustmentScope,
			st.RestartRequested,
		),
	)
	return st, nil
}

// selectNextAfterPlan 根据 plan 节点结果决定下一步。
//
// 分支规则：
// 1. 如果 FinalSummary 已经有内容，说明已确定要提前退出 -> exit；
// 2. 如果任务类为空，说明无法继续构建方案 -> exit；
// 3. 其余情况 -> roughBuild。
func selectNextAfterPlan(st *SchedulePlanState) string {
	if st == nil {
		return schedulePlanGraphNodeExit
	}
	if strings.TrimSpace(st.FinalSummary) != "" {
		return schedulePlanGraphNodeExit
	}
	if len(st.TaskClassIDs) == 0 {
		return schedulePlanGraphNodeExit
	}
	return schedulePlanGraphNodeRoughBuild
}

// runRoughBuildNode 负责“一次性完成粗排结果构建”。
//
// 职责边界：
// 1. 调用多任务类混排能力，生成 HybridEntries + AllocatedItems；
// 2. 把 HybridEntries 转成 CandidatePlans，便于后续预览输出；
// 3. 不做 daily/weekly 优化本身，只提供下游输入。
func runRoughBuildNode(
	ctx context.Context,
	st *SchedulePlanState,
	deps SchedulePlanToolDeps,
	emitStage func(stage, detail string),
) (*SchedulePlanState, error) {
	if st == nil {
		return nil, errors.New("schedule plan graph: nil state in roughBuild node")
	}
	if deps.HybridScheduleWithPlanMulti == nil {
		return nil, errors.New("schedule plan graph: HybridScheduleWithPlanMulti dependency not injected")
	}

	// 1. 清洗并校验任务类 ID。
	// 1.1 统一在节点入口做一次最终收敛，避免上游遗漏导致语义漂移；
	// 1.2 若最终仍为空，直接结束，避免无意义调用下游服务。
	taskClassIDs := normalizeTaskClassIDs(st.TaskClassIDs)
	// 1.3 连续对话兜底：若本轮任务类为空且命中历史快照，则回退到上轮任务类集合。
	if len(taskClassIDs) == 0 && st.IsAdjustment && len(st.PreviousTaskClassIDs) > 0 {
		taskClassIDs = normalizeTaskClassIDs(append([]int(nil), st.PreviousTaskClassIDs...))
	}
	if len(taskClassIDs) == 0 {
		st.FinalSummary = "缺少有效的任务类 ID，无法生成排程方案。请传入 task_class_ids。"
		return st, nil
	}
	st.TaskClassIDs = taskClassIDs

	// 2. 连续对话微调优先复用上一版混合日程作为起点，避免“每轮都重新粗排”。
	// 2.1 触发条件：IsAdjustment=true 且 PreviousHybridEntries 非空；
	// 2.2 失败兜底：若快照不完整（例如 AllocatedItems 为空），会构造最小占位任务块，保持下游校验可运行；
	// 2.3 回退策略：若没有可复用快照，再走全量粗排构建路径。
	canReusePreviousPlan := st.IsAdjustment &&
		!st.RestartRequested &&
		len(st.PreviousHybridEntries) > 0 &&
		sameTaskClassSet(taskClassIDs, st.PreviousTaskClassIDs)
	if canReusePreviousPlan {
		emitStage("schedule_plan.rough_build.reuse_previous", "检测到连续对话微调，复用上一版排程作为优化起点。")
		st.HybridEntries = deepCopyEntries(st.PreviousHybridEntries)
		st.CandidatePlans = deepCopyWeekSchedules(st.PreviousCandidatePlans)
		if len(st.CandidatePlans) == 0 {
			st.CandidatePlans = hybridEntriesToWeekSchedules(st.HybridEntries)
		}
		st.AllocatedItems = deepCopyTaskClassItems(st.PreviousAllocatedItems)
		if len(st.AllocatedItems) == 0 {
			st.AllocatedItems = buildAllocatedItemsFromHybridEntries(st.HybridEntries)
		}

		// 2.2 复用模式下同样尝试解析窗口边界，保证周级 Move 约束仍然有效。
		if deps.ResolvePlanningWindow != nil {
			startWeek, startDay, endWeek, endDay, windowErr := deps.ResolvePlanningWindow(ctx, st.UserID, taskClassIDs)
			if windowErr != nil {
				st.FinalSummary = fmt.Sprintf("解析排程窗口失败：%s。", windowErr.Error())
				return st, nil
			}
			st.HasPlanningWindow = true
			st.PlanStartWeek = startWeek
			st.PlanStartDay = startDay
			st.PlanEndWeek = endWeek
			st.PlanEndDay = endDay
		}

		st.MergeSnapshot = deepCopyEntries(st.HybridEntries)
		suggestedCount := 0
		for _, e := range st.HybridEntries {
			if e.Status == "suggested" {
				suggestedCount++
			}
		}
		emitStage(
			"schedule_plan.rough_build.done",
			fmt.Sprintf("已复用历史方案，条目总数=%d，可优化条目=%d。", len(st.HybridEntries), suggestedCount),
		)
		return st, nil
	}

	emitStage("schedule_plan.rough_build.building", "正在构建粗排候选方案。")

	// 3. 调用服务层统一能力构建混合日程。
	// 3.1 该能力内部会完成“多任务类粗排 + 既有日程合并”；
	// 3.2 这里不再拆成 preview/hybrid 两段，避免跨节点重复计算。
	entries, allocatedItems, err := deps.HybridScheduleWithPlanMulti(ctx, st.UserID, taskClassIDs)
	if err != nil {
		st.FinalSummary = fmt.Sprintf("构建粗排方案失败：%s。", err.Error())
		return st, nil
	}
	if len(entries) == 0 {
		st.FinalSummary = "没有生成可优化的排程条目，请检查任务类时间范围或课表占用。"
		return st, nil
	}

	// 4. 回填状态。
	st.HybridEntries = entries
	st.AllocatedItems = allocatedItems
	st.CandidatePlans = hybridEntriesToWeekSchedules(entries)

	// 4.1 解析全局排程窗口（可选依赖）。
	// 4.1.1 目的：给周级 Move 增加“首尾不足一周”的硬边界校验；
	// 4.1.2 失败策略：若依赖已注入但解析失败，直接结束本次排程，避免带着错误窗口继续优化。
	if deps.ResolvePlanningWindow != nil {
		startWeek, startDay, endWeek, endDay, windowErr := deps.ResolvePlanningWindow(ctx, st.UserID, taskClassIDs)
		if windowErr != nil {
			st.FinalSummary = fmt.Sprintf("解析排程窗口失败：%s。", windowErr.Error())
			return st, nil
		}
		st.HasPlanningWindow = true
		st.PlanStartWeek = startWeek
		st.PlanStartDay = startDay
		st.PlanEndWeek = endWeek
		st.PlanEndDay = endDay
	}

	// 4.2 记录 merge 快照：
	// 4.2.1 单任务类路径可直接作为 final_check 回退基线；
	// 4.2.2 多任务类路径后续 merge 节点会覆盖成“日内优化后快照”。
	st.MergeSnapshot = deepCopyEntries(entries)

	// 5. 把“按名称提示的标签”尽可能映射到 task_item_id。
	// 5.1 目的：后续 daily_split 统一按 task_item_id 维度写入 context_tag；
	// 5.2 失败策略：映射不上不报错，后续默认走 General 标签。
	if st.TaskTags == nil {
		st.TaskTags = make(map[int]string)
	}
	if len(st.TaskTagHintsByName) > 0 {
		for i := range st.HybridEntries {
			entry := &st.HybridEntries[i]
			if entry.Status != "suggested" || entry.TaskItemID <= 0 {
				continue
			}
			if _, exists := st.TaskTags[entry.TaskItemID]; exists {
				continue
			}
			if tag, ok := st.TaskTagHintsByName[entry.Name]; ok {
				st.TaskTags[entry.TaskItemID] = normalizeContextTag(tag)
			}
		}
	}

	suggestedCount := 0
	for _, e := range entries {
		if e.Status == "suggested" {
			suggestedCount++
		}
	}
	emitStage(
		"schedule_plan.rough_build.done",
		fmt.Sprintf("粗排构建完成，条目总数=%d，可优化条目=%d。", len(entries), suggestedCount),
	)
	return st, nil
}

// callScheduleModelForJSON 调用模型并要求返回 JSON。
//
// 职责边界：
// 1. 仅负责模型调用参数装配，不做业务字段解释；
// 2. 统一关闭 thinking，减少路由/抽取场景的延迟和 token 开销。
func callScheduleModelForJSON(ctx context.Context, chatModel *ark.ChatModel, systemPrompt, userPrompt string, maxTokens int) (string, error) {
	return agentllm.CallArkText(ctx, chatModel, systemPrompt, userPrompt, agentllm.ArkCallOptions{
		Temperature: 0,
		MaxTokens:   maxTokens,
		Thinking:    agentllm.ThinkingModeDisabled,
	})
}

// parseScheduleJSON 解析模型返回的 JSON 内容。
//
// 兼容策略：
// 1. 兼容 ```json ... ``` 包裹；
// 2. 兼容模型在 JSON 前后带解释文本（提取最外层对象）。
func parseScheduleJSON[T any](raw string) (*T, error) {
	return agentllm.ParseJSONObject[T](raw)
}

// extractPreviousPlanFromHistory 从对话历史中提取最近一次排程结果文本。
func extractPreviousPlanFromHistory(history []*schema.Message) string {
	if len(history) == 0 {
		return ""
	}
	for i := len(history) - 1; i >= 0; i-- {
		msg := history[i]
		if msg == nil || msg.Role != schema.Assistant {
			continue
		}
		content := strings.TrimSpace(msg.Content)
		if strings.Contains(content, "排程完成") || strings.Contains(content, "已成功安排") {
			return content
		}
	}
	return ""
}

// runReturnPreviewNode 负责把优化后的 HybridEntries 转成“前端可直接展示”的预览结构。
//
// 职责边界：
// 1. 把 suggested 结果回填到 AllocatedItems，便于后续确认后直接落库；
// 2. 生成 CandidatePlans；
// 3. 生成最终文案；
// 4. 不执行实际写库。
func runReturnPreviewNode(
	ctx context.Context,
	st *SchedulePlanState,
	emitStage func(stage, detail string),
) (*SchedulePlanState, error) {
	_ = ctx
	if st == nil {
		return nil, errors.New("schedule plan graph: nil state in returnPreview node")
	}

	emitStage("schedule_plan.preview_return.building", "正在生成优化后的排程预览。")

	// 1. 把 HybridEntries 中 suggested 的最终位置回填到 AllocatedItems。
	suggestedMap := make(map[int]*model.HybridScheduleEntry)
	for i := range st.HybridEntries {
		e := &st.HybridEntries[i]
		if e.Status == "suggested" && e.TaskItemID > 0 {
			suggestedMap[e.TaskItemID] = e
		}
	}
	for i := range st.AllocatedItems {
		item := &st.AllocatedItems[i]
		if entry, ok := suggestedMap[item.ID]; ok && item.EmbeddedTime != nil {
			item.EmbeddedTime.Week = entry.Week
			item.EmbeddedTime.DayOfWeek = entry.DayOfWeek
			item.EmbeddedTime.SectionFrom = entry.SectionFrom
			item.EmbeddedTime.SectionTo = entry.SectionTo
		}
	}

	// 2. 生成前端预览结构。
	st.CandidatePlans = hybridEntriesToWeekSchedules(st.HybridEntries)

	// 3. 生成最终摘要：
	// 3.1 优先保留 final_check 的输出；
	// 3.2 若没有 final_check 输出，则回退 weekly refine 摘要；
	// 3.3 都没有时给兜底文案。
	if strings.TrimSpace(st.FinalSummary) == "" {
		if strings.TrimSpace(st.ReactSummary) != "" {
			st.FinalSummary = st.ReactSummary
		} else {
			st.FinalSummary = fmt.Sprintf("排程优化完成，共 %d 个任务已安排，请确认后应用。", len(suggestedMap))
		}
	}
	st.Completed = true

	emitStage("schedule_plan.preview_return.done", "排程预览已生成，等待你确认。")
	return st, nil
}

// buildAllocatedItemsFromHybridEntries 根据 suggested 条目构造最小可用的任务块快照。
//
// 设计目的：
// 1. 连续微调复用历史方案时，若缓存里没有 AllocatedItems，仍然保证 final_check 的数量核对可运行；
// 2. return_preview 仍可依据 TaskItemID 回填最终 embedded_time；
// 3. 该函数只做“兜底构造”，不替代真实粗排输出。
func buildAllocatedItemsFromHybridEntries(entries []model.HybridScheduleEntry) []model.TaskClassItem {
	if len(entries) == 0 {
		return nil
	}
	items := make([]model.TaskClassItem, 0)
	for _, entry := range entries {
		if entry.Status != "suggested" {
			continue
		}
		embedded := &model.TargetTime{
			Week:        entry.Week,
			DayOfWeek:   entry.DayOfWeek,
			SectionFrom: entry.SectionFrom,
			SectionTo:   entry.SectionTo,
		}
		taskID := entry.TaskItemID
		items = append(items, model.TaskClassItem{
			ID:           taskID,
			EmbeddedTime: embedded,
		})
	}
	return items
}

// deepCopyTaskClassItems 深拷贝任务块切片（包含指针字段），避免跨节点共享引用。
func deepCopyTaskClassItems(src []model.TaskClassItem) []model.TaskClassItem {
	return agentshared.CloneTaskClassItems(src)
}

// normalizeContextTag 归一化任务标签。
//
// 失败兜底：
// 1. 未识别/空值统一回落到 General；
// 2. 保证后续 prompt 构造不会出现空标签。
func normalizeContextTag(raw string) string {
	tag := strings.TrimSpace(raw)
	if tag == "" {
		return "General"
	}
	switch strings.ToLower(tag) {
	case "high-logic", "high_logic", "logic":
		return "High-Logic"
	case "memory":
		return "Memory"
	case "review":
		return "Review"
	case "general":
		return "General"
	default:
		return "General"
	}
}

// normalizeTaskClassIDs 清洗 task_class_ids（去重 + 过滤非法值）。
func normalizeTaskClassIDs(ids []int) []int {
	if len(ids) == 0 {
		return nil
	}
	seen := make(map[int]struct{}, len(ids))
	out := make([]int, 0, len(ids))
	for _, id := range ids {
		if id <= 0 {
			continue
		}
		if _, exists := seen[id]; exists {
			continue
		}
		seen[id] = struct{}{}
		out = append(out, id)
	}
	return out
}

// clearPreviousPreviewContext 清空会话承接快照字段。
//
// 触发场景：
// 1. 用户明确要求 restart（重新排）；
// 2. 需要强制断开“沿用历史方案”的路径，避免脏状态渗透到新方案。
func clearPreviousPreviewContext(st *SchedulePlanState) {
	if st == nil {
		return
	}
	st.HasPreviousPreview = false
	st.PreviousTaskClassIDs = nil
	st.PreviousHybridEntries = nil
	st.PreviousAllocatedItems = nil
	st.PreviousCandidatePlans = nil
	st.PreviousPlanJSON = ""
}

// clampAdjustmentConfidence 约束置信度字段到 [0,1]。
func clampAdjustmentConfidence(v float64) float64 {
	if v < 0 {
		return 0
	}
	if v > 1 {
		return 1
	}
	return v
}

// deepCopyWeekSchedules 深拷贝周视图方案切片，避免跨节点共享引用。
func deepCopyWeekSchedules(src []model.UserWeekSchedule) []model.UserWeekSchedule {
	return agentshared.CloneWeekSchedules(src)
}

// sameTaskClassSet 判断两组 task_class_ids 是否表示同一集合（忽略顺序，忽略重复）。
//
// 语义：
// 1. 两边经清洗后都为空，返回 false（空集合不作为“可复用历史方案”的依据）；
// 2. 元素集合完全一致返回 true；
// 3. 任一元素差异返回 false。
func sameTaskClassSet(left []int, right []int) bool {
	l := normalizeTaskClassIDs(left)
	r := normalizeTaskClassIDs(right)
	if len(l) == 0 || len(r) == 0 {
		return false
	}
	if len(l) != len(r) {
		return false
	}
	seen := make(map[int]struct{}, len(l))
	for _, id := range l {
		seen[id] = struct{}{}
	}
	for _, id := range r {
		if _, ok := seen[id]; !ok {
			return false
		}
	}
	return true
}

// hybridEntriesToWeekSchedules 把内存中的混合条目转换成前端周视图格式。
func hybridEntriesToWeekSchedules(entries []model.HybridScheduleEntry) []model.UserWeekSchedule {
	sectionTimeMap := map[int][2]string{
		1: {"08:00", "08:45"}, 2: {"08:55", "09:40"},
		3: {"10:15", "11:00"}, 4: {"11:10", "11:55"},
		5: {"14:00", "14:45"}, 6: {"14:55", "15:40"},
		7: {"16:15", "17:00"}, 8: {"17:10", "17:55"},
		9: {"19:00", "19:45"}, 10: {"19:55", "20:40"},
		11: {"20:50", "21:35"}, 12: {"21:45", "22:30"},
	}

	weekMap := make(map[int][]model.WeeklyEventBrief)
	for _, e := range entries {
		startTime := ""
		endTime := ""
		if t, ok := sectionTimeMap[e.SectionFrom]; ok {
			startTime = t[0]
		}
		if t, ok := sectionTimeMap[e.SectionTo]; ok {
			endTime = t[1]
		}

		brief := model.WeeklyEventBrief{
			DayOfWeek: e.DayOfWeek,
			Name:      e.Name,
			StartTime: startTime,
			EndTime:   endTime,
			Type:      e.Type,
			Span:      e.SectionTo - e.SectionFrom + 1,
			Status:    e.Status,
		}
		if e.EventID > 0 {
			brief.ID = e.EventID
		}
		weekMap[e.Week] = append(weekMap[e.Week], brief)
	}

	result := make([]model.UserWeekSchedule, 0, len(weekMap))
	for week, events := range weekMap {
		result = append(result, model.UserWeekSchedule{
			Week:   week,
			Events: events,
		})
	}
	for i := 0; i < len(result); i++ {
		for j := i + 1; j < len(result); j++ {
			if result[j].Week < result[i].Week {
				result[i], result[j] = result[j], result[i]
			}
		}
	}
	return result
}

// runDailySplitNode 负责“按天拆分 + 标签注入 + 跳过判断”。
//
// 职责边界：
// 1. 负责把全量 HybridEntries 拆成 DayGroup，供后续并发日内优化；
// 2. 负责把 TaskTags(task_item_id -> tag) 注入到条目的 ContextTag；
// 3. 负责识别“低收益天”（suggested<=2）并标记 SkipRefine；
// 4. 不负责调用模型，不负责并发执行，不负责结果合并。
func runDailySplitNode(
	ctx context.Context,
	st *SchedulePlanState,
	emitStage func(stage, detail string),
) (*SchedulePlanState, error) {
	_ = ctx
	if st == nil || len(st.HybridEntries) == 0 {
		return st, nil
	}

	emitStage("schedule_plan.daily_split.start", "正在按天拆分排程并标记优化单元。")

	// 1. 初始化容器：
	// 1.1 groups 以 week/day 二级索引保存 DayGroup；
	// 1.2 这么做的目的是后续 daily_refine 可以直接并发遍历，不再重复分组。
	groups := make(map[int]map[int]*DayGroup)

	// 2. 遍历混合条目，执行“标签注入 + 分组”。
	for i := range st.HybridEntries {
		entry := &st.HybridEntries[i]

		// 2.1 仅对 suggested 条目注入 ContextTag。
		// 2.1.1 existing 条目是固定课表/已落库任务，不参与认知标签优化。
		// 2.1.2 注入失败时兜底 General，避免后续 prompt 出现空标签。
		if entry.Status == "suggested" && entry.TaskItemID > 0 {
			if tag, ok := st.TaskTags[entry.TaskItemID]; ok {
				entry.ContextTag = normalizeContextTag(tag)
			} else {
				entry.ContextTag = "General"
			}
		}

		// 2.2 建立分组索引。
		if groups[entry.Week] == nil {
			groups[entry.Week] = make(map[int]*DayGroup)
		}
		if groups[entry.Week][entry.DayOfWeek] == nil {
			groups[entry.Week][entry.DayOfWeek] = &DayGroup{
				Week:      entry.Week,
				DayOfWeek: entry.DayOfWeek,
			}
		}
		groups[entry.Week][entry.DayOfWeek].Entries = append(groups[entry.Week][entry.DayOfWeek].Entries, *entry)
	}

	// 3. 逐天计算 suggested 数量，标记是否跳过日内优化。
	//
	// 3.1 为什么阈值设为 <=2：
	// 3.1.1 suggested 很少时，模型优化收益通常不足以覆盖请求成本；
	// 3.1.2 直接跳过可减少无效模型调用和阶段等待。
	// 3.2 失败策略：
	// 3.2.1 这里只做内存标记，不会失败；
	// 3.2.2 即使阈值判断不完美，也只影响优化深度，不影响功能正确性。
	totalDays := 0
	skipDays := 0
	for _, dayMap := range groups {
		for _, dayGroup := range dayMap {
			totalDays++
			suggestedCount := 0
			for _, e := range dayGroup.Entries {
				if e.Status == "suggested" {
					suggestedCount++
				}
			}
			if suggestedCount <= 2 {
				dayGroup.SkipRefine = true
				skipDays++
			}
		}
	}

	// 4. 回填状态，交给后续节点使用。
	st.DailyGroups = groups
	emitStage(
		"schedule_plan.daily_split.done",
		fmt.Sprintf("已拆分为 %d 天，其中 %d 天跳过日内优化。", totalDays, skipDays),
	)
	return st, nil
}

// runQuickRefineNode 是 small 微调分支的“轻量预算收缩节点”。
//
// 职责边界：
// 1. 负责在进入 weekly_refine 前收缩预算与并发，避免小改动走重链路；
// 2. 负责保留“可回退”的最低预算，避免直接压成 0 导致无动作可执行；
// 3. 不负责执行任何 Move/Swap（真正动作仍由 weekly_refine 完成）。
func runQuickRefineNode(
	ctx context.Context,
	st *SchedulePlanState,
	emitStage func(stage, detail string),
) (*SchedulePlanState, error) {
	_ = ctx
	if st == nil {
		return nil, fmt.Errorf("schedule plan quick refine: nil state")
	}

	emitStage("schedule_plan.quick_refine.start", "检测到小幅微调，正在切换到快速优化路径。")

	// 1. 预算收缩策略：
	// 1.1 small 场景目标是“快速响应 + 可解释改动”，不追求大规模重排；
	// 1.2 因此把总预算压到最多 2 次尝试、有效预算压到最多 1 次成功动作；
	// 1.3 如果上游已配置更小预算，则尊重更小值，不做反向放大。
	if st.WeeklyTotalBudget <= 0 {
		st.WeeklyTotalBudget = schedulePlanDefaultWeeklyTotalBudget
	}
	if st.WeeklyAdjustBudget <= 0 {
		st.WeeklyAdjustBudget = schedulePlanDefaultWeeklyAdjustBudget
	}
	st.WeeklyTotalBudget = clampBudgetUpper(st.WeeklyTotalBudget, 2)
	st.WeeklyAdjustBudget = clampBudgetUpper(st.WeeklyAdjustBudget, 1)

	// 2. 预算一致性兜底：
	// 2.1 总预算至少为 1（否则 weekly worker 无法执行）；
	// 2.2 有效预算至少为 1（否则所有成功动作都不被允许）；
	// 2.3 有效预算永远不能超过总预算。
	if st.WeeklyTotalBudget < 1 {
		st.WeeklyTotalBudget = 1
	}
	if st.WeeklyAdjustBudget < 1 {
		st.WeeklyAdjustBudget = 1
	}
	if st.WeeklyAdjustBudget > st.WeeklyTotalBudget {
		st.WeeklyAdjustBudget = st.WeeklyTotalBudget
	}

	// 3. 小改动路径把周级并发收敛到 1，优先保证稳定与可观察性。
	st.WeeklyRefineConcurrency = 1

	emitStage(
		"schedule_plan.quick_refine.done",
		fmt.Sprintf(
			"快速微调预算已生效：总预算=%d，有效预算=%d，并发=%d。",
			st.WeeklyTotalBudget,
			st.WeeklyAdjustBudget,
			st.WeeklyRefineConcurrency,
		),
	)
	return st, nil
}

// clampBudgetUpper 把预算裁剪到“非负且不超过上限”。
func clampBudgetUpper(current int, upper int) int {
	if current < 0 {
		return 0
	}
	if current > upper {
		return upper
	}
	return current
}

const (
	// dailyReactRoundTimeout 是日内单轮模型调用超时。
	// 日内节点走并发调用，超时要比周级更保守，避免占满资源。
	dailyReactRoundTimeout = 3 * time.Minute
)

// runDailyRefineNode 负责“并发日内优化”。
//
// 职责边界：
// 1. 负责按 DayGroup 并发调用单日 ReAct；
// 2. 负责输出“按天开始/完成”的阶段状态块（不推 reasoning 细流）；
// 3. 负责把单日失败回退到原始数据，确保全链路可继续；
// 4. 不负责跨天配平（交给 weekly_refine），不负责最终总结（交给 final_check）。
func runDailyRefineNode(
	ctx context.Context,
	st *SchedulePlanState,
	chatModel *ark.ChatModel,
	dailyRefineConcurrency int,
	emitStage func(stage, detail string),
) (*SchedulePlanState, error) {
	if st == nil || len(st.DailyGroups) == 0 {
		return st, nil
	}
	if chatModel == nil {
		return st, fmt.Errorf("schedule plan daily refine: model is nil")
	}

	// 1. 并发度兜底：
	// 1.1 优先使用注入参数；
	// 1.2 若注入参数非法，则回退到 state 值；
	// 1.3 state 也非法时，回退到编译期默认值。
	if dailyRefineConcurrency <= 0 {
		dailyRefineConcurrency = st.DailyRefineConcurrency
	}
	if dailyRefineConcurrency <= 0 {
		dailyRefineConcurrency = schedulePlanDefaultDailyRefineConcurrency
	}

	emitStage(
		"schedule_plan.daily_refine.start",
		fmt.Sprintf("正在并发优化各天日程，并发度=%d。", dailyRefineConcurrency),
	)

	// 2. 拉平所有 DayGroup 并排序，确保日志与阶段输出稳定可读。
	allGroups := flattenAndSortDayGroups(st.DailyGroups)
	if len(allGroups) == 0 {
		st.DailyResults = make(map[int]map[int][]model.HybridScheduleEntry)
		emitStage("schedule_plan.daily_refine.done", "没有可优化的天，跳过日内优化。")
		return st, nil
	}

	// 3. 并发执行：
	// 3.1 sem 控制并发上限；
	// 3.2 wg 等待全部 goroutine 完成；
	// 3.3 mu 保护 results/firstErr，避免竞态。
	sem := make(chan struct{}, dailyRefineConcurrency)
	var wg sync.WaitGroup
	var mu sync.Mutex
	totalGroups := int32(len(allGroups))
	var finishedGroups int32

	results := make(map[int]map[int][]model.HybridScheduleEntry)
	var firstErr error

	for _, group := range allGroups {
		g := group
		wg.Add(1)
		go func() {
			defer wg.Done()

			// 3.4 先申请并发令牌；若 ctx 已取消，直接回退原始数据并结束。
			select {
			case sem <- struct{}{}:
				defer func() { <-sem }()
			case <-ctx.Done():
				mu.Lock()
				if firstErr == nil {
					firstErr = ctx.Err()
				}
				ensureDayResult(results, g.Week, g.DayOfWeek, g.Entries)
				mu.Unlock()
				// 3.4.1 取消场景也要计入进度，避免前端看到“卡住不动”。
				done := atomic.AddInt32(&finishedGroups, 1)
				emitStage(
					"schedule_plan.daily_refine.day_done",
					fmt.Sprintf("W%dD%d 已取消并回退原方案。（进度 %d/%d）", g.Week, g.DayOfWeek, done, totalGroups),
				)
				return
			}

			emitStage(
				"schedule_plan.daily_refine.day_start",
				fmt.Sprintf("正在安排 W%dD%d。（当前进度 %d/%d）", g.Week, g.DayOfWeek, atomic.LoadInt32(&finishedGroups), totalGroups),
			)

			// 3.5 低收益天直接跳过模型调用，原样透传。
			if g.SkipRefine {
				mu.Lock()
				ensureDayResult(results, g.Week, g.DayOfWeek, g.Entries)
				mu.Unlock()
				done := atomic.AddInt32(&finishedGroups, 1)
				emitStage(
					"schedule_plan.daily_refine.day_done",
					fmt.Sprintf("W%dD%d suggested 较少，已跳过优化。（进度 %d/%d）", g.Week, g.DayOfWeek, done, totalGroups),
				)
				return
			}

			// 3.6 深拷贝输入，避免并发场景下意外修改共享切片。
			localEntries := deepCopyEntries(g.Entries)

			// 3.7 动态轮次：
			// 3.7.1 suggested <= 4：1轮足够；
			// 3.7.2 suggested > 4：最多2轮，提升复杂天优化质量。
			maxRounds := 1
			if countSuggested(localEntries) > 4 {
				maxRounds = 2
			}

			optimized, refineErr := runSingleDayReact(ctx, chatModel, localEntries, maxRounds, g.Week, g.DayOfWeek)
			if refineErr != nil {
				mu.Lock()
				if firstErr == nil {
					firstErr = refineErr
				}
				// 3.8 单天失败回退：
				// 3.8.1 保证失败只影响该天；
				// 3.8.2 保证总流程可继续推进到 merge/weekly/final。
				ensureDayResult(results, g.Week, g.DayOfWeek, g.Entries)
				mu.Unlock()
				done := atomic.AddInt32(&finishedGroups, 1)
				emitStage(
					"schedule_plan.daily_refine.day_done",
					fmt.Sprintf("W%dD%d 优化失败，已回退原方案。（进度 %d/%d）", g.Week, g.DayOfWeek, done, totalGroups),
				)
				return
			}

			mu.Lock()
			ensureDayResult(results, g.Week, g.DayOfWeek, optimized)
			mu.Unlock()
			done := atomic.AddInt32(&finishedGroups, 1)
			emitStage(
				"schedule_plan.daily_refine.day_done",
				fmt.Sprintf("W%dD%d 已安排完成。（进度 %d/%d）", g.Week, g.DayOfWeek, done, totalGroups),
			)
		}()
	}

	wg.Wait()
	st.DailyResults = results
	if firstErr != nil {
		emitStage("schedule_plan.daily_refine.partial_error", fmt.Sprintf("部分天优化失败，已自动回退。原因：%s", firstErr.Error()))
	}
	emitStage("schedule_plan.daily_refine.done", "日内优化阶段完成。")
	return st, nil
}

// runSingleDayReact 执行单天封闭式 ReAct 优化。
//
// 关键约束：
// 1. prompt 只包含当天数据；
// 2. 代码层再做“Move 不能跨天”硬校验；
// 3. Thinking 默认关闭，优先降低日内并发阶段的长尾时延。
func runSingleDayReact(
	ctx context.Context,
	chatModel *ark.ChatModel,
	entries []model.HybridScheduleEntry,
	maxRounds int,
	week int,
	dayOfWeek int,
) ([]model.HybridScheduleEntry, error) {
	hybridJSON, err := json.Marshal(entries)
	if err != nil {
		return entries, err
	}

	messages := []*schema.Message{
		schema.SystemMessage(agentprompt.SchedulePlanDailyReactPrompt),
		schema.UserMessage(fmt.Sprintf(
			"以下是今天的日程（JSON）：\n%s\n\n仅优化这一天的数据，不要跨天移动。",
			string(hybridJSON),
		)),
	}

	for round := 0; round < maxRounds; round++ {
		roundCtx, cancel := context.WithTimeout(ctx, dailyReactRoundTimeout)
		// 1. 日内优化只做“单天局部微调”，任务边界清晰，默认关闭 thinking 以降低时延。
		// 2. 周级全局配平仍保留 thinking（在 weekly_refine），这里不承担跨天复杂推理职责。
		// 3. 模型调用细节统一下沉到 llm 层，避免 schedule skill 再维护一份 SDK 样板。
		content, generateErr := agentllm.GenerateScheduleDailyReactRound(roundCtx, chatModel, messages)
		cancel()
		if generateErr != nil {
			return entries, fmt.Errorf("日内 ReAct 第%d轮失败: %w", round+1, generateErr)
		}
		parsed, parseErr := parseReactLLMOutput(content)
		if parseErr != nil {
			// 解析失败时回退当前轮，不把异常向上放大成整条链路失败。
			return entries, nil
		}
		if parsed.Done || len(parsed.ToolCalls) == 0 {
			break
		}

		// 1. 执行工具调用。
		// 1.1 每个调用都经过“日内策略约束”校验；
		// 1.2 任何单次调用失败都只返回 failed result，不中断整轮。
		results := make([]reactToolResult, 0, len(parsed.ToolCalls))
		for _, call := range parsed.ToolCalls {
			var result reactToolResult
			entries, result = dispatchDailyReactTool(entries, call, week, dayOfWeek)
			results = append(results, result)
		}

		// 2. 把“本轮模型输出 + 工具执行结果”拼入下一轮上下文。
		// 2.1 这样模型可以看到操作反馈，继续迭代；
		// 2.2 若下一轮仍无有效动作，会自然在 done/空 tool_calls 退出。
		messages = append(messages, schema.AssistantMessage(content, nil))
		resultJSON, _ := json.Marshal(results)
		messages = append(messages, schema.UserMessage(
			fmt.Sprintf("工具执行结果：\n%s\n\n请继续优化或输出 {\"done\":true,\"summary\":\"...\"}。", string(resultJSON)),
		))
	}

	return entries, nil
}

// dispatchDailyReactTool 在通用工具分发前增加“日内硬约束”。
//
// 职责边界：
// 1. 只负责校验 Move 的目标是否仍在当前天；
// 2. 通过后复用 dispatchReactTool 执行；
// 3. 不负责复杂冲突判定（冲突判定由底层工具函数处理）。
func dispatchDailyReactTool(entries []model.HybridScheduleEntry, call reactToolCall, week int, dayOfWeek int) ([]model.HybridScheduleEntry, reactToolResult) {
	if call.Tool == "Move" {
		toWeek, weekOK := paramInt(call.Params, "to_week")
		toDay, dayOK := paramInt(call.Params, "to_day")
		if !weekOK || !dayOK {
			return entries, reactToolResult{
				Tool:    "Move",
				Success: false,
				Result:  "参数缺失：to_week/to_day",
			}
		}
		if toWeek != week || toDay != dayOfWeek {
			return entries, reactToolResult{
				Tool:    "Move",
				Success: false,
				Result:  fmt.Sprintf("日内优化禁止跨天移动：当前仅允许 W%dD%d", week, dayOfWeek),
			}
		}
	}
	return dispatchReactTool(entries, call)
}

// flattenAndSortDayGroups 把 map 结构摊平成有序切片，便于稳定并发调度。
func flattenAndSortDayGroups(groups map[int]map[int]*DayGroup) []*DayGroup {
	out := make([]*DayGroup, 0)
	for _, dayMap := range groups {
		for _, g := range dayMap {
			if g != nil {
				out = append(out, g)
			}
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Week != out[j].Week {
			return out[i].Week < out[j].Week
		}
		return out[i].DayOfWeek < out[j].DayOfWeek
	})
	return out
}

// ensureDayResult 确保 results[week][day] 存在并写入值。
func ensureDayResult(results map[int]map[int][]model.HybridScheduleEntry, week int, day int, entries []model.HybridScheduleEntry) {
	if results[week] == nil {
		results[week] = make(map[int][]model.HybridScheduleEntry)
	}
	results[week][day] = entries
}

// deepCopyEntries 深拷贝 HybridScheduleEntry 切片。
func deepCopyEntries(src []model.HybridScheduleEntry) []model.HybridScheduleEntry {
	dst := make([]model.HybridScheduleEntry, len(src))
	copy(dst, src)
	return dst
}

// runMergeNode 负责“合并日内结果 + 冲突校验 + 回退快照”。
//
// 职责边界：
// 1. 负责把 DailyResults 合并回全量 HybridEntries；
// 2. 负责执行时间冲突检测；
// 3. 负责在冲突时回退原始数据；
// 4. 负责产出 MergeSnapshot，供 final_check 失败时回退。
func runMergeNode(
	ctx context.Context,
	st *SchedulePlanState,
	emitStage func(stage, detail string),
) (*SchedulePlanState, error) {
	_ = ctx
	if st == nil || len(st.DailyResults) == 0 {
		return st, nil
	}

	emitStage("schedule_plan.merge.start", "正在合并日内优化结果。")

	// 1. 先保存 merge 前原始数据，作为冲突时的第一层回退兜底。
	originalEntries := deepCopyEntries(st.HybridEntries)

	// 2. 展平 daily results。
	merged := make([]model.HybridScheduleEntry, 0)
	for _, dayMap := range st.DailyResults {
		for _, dayEntries := range dayMap {
			merged = append(merged, dayEntries...)
		}
	}

	// 3. 冲突校验。
	//
	// 3.1 判断依据：同一 (week, day, section) 只能有一个条目占用；
	// 3.2 失败处理：一旦冲突，整批回退到 merge 前原始结果；
	// 3.3 回退策略：回退后仍继续链路，避免请求直接失败。
	if conflict := detectConflicts(merged); conflict != "" {
		st.HybridEntries = originalEntries
		emitStage("schedule_plan.merge.conflict", fmt.Sprintf("检测到冲突并回退：%s", conflict))
	} else {
		st.HybridEntries = merged
		emitStage("schedule_plan.merge.done", fmt.Sprintf("合并完成，共 %d 个条目。", len(merged)))
	}

	// 4. 无论是否冲突，都生成“可回退快照”。
	st.MergeSnapshot = deepCopyEntries(st.HybridEntries)
	return st, nil
}

// detectConflicts 检测条目是否存在时间冲突。
//
// 返回语义：
// 1. 返回空字符串：无冲突；
// 2. 返回非空字符串：冲突描述，可直接用于日志/阶段提示。
func detectConflicts(entries []model.HybridScheduleEntry) string {
	type slotKey struct {
		week, day, section int
	}
	occupied := make(map[slotKey]string)
	for _, entry := range entries {
		// 1. 仅“阻塞建议任务”的条目参与冲突校验。
		// 2. 可嵌入且当前未占用的课程槽位不应被判定为冲突。
		if !entryBlocksSuggested(entry) {
			continue
		}
		for section := entry.SectionFrom; section <= entry.SectionTo; section++ {
			key := slotKey{week: entry.Week, day: entry.DayOfWeek, section: section}
			if prevName, exists := occupied[key]; exists {
				return fmt.Sprintf(
					"W%dD%d 第%d节 冲突：[%s] 与 [%s]",
					entry.Week, entry.DayOfWeek, section, prevName, entry.Name,
				)
			}
			occupied[key] = entry.Name
		}
	}
	return ""
}

const (
	// weeklyReactRoundTimeout 是周级“单步动作”单轮超时时间。
	//
	// 说明：
	// 1. 当前周级策略是“每轮只做一个动作”，单轮输入较短，超时可比旧版更保守；
	// 2. 过长超时会放大长尾等待，影响并发周优化的整体收口速度。
	weeklyReactRoundTimeout = 4 * time.Minute
)

// weeklyRefineWorkerResult 是“单周 worker”输出。
//
// 职责边界：
// 1. 记录该周优化后的 entries；
// 2. 记录预算消耗（总动作/有效动作）；
// 3. 记录动作日志，供 final_check 生成“过程可解释”总结；
// 4. 记录该周摘要，便于最终汇总。
type weeklyRefineWorkerResult struct {
	Week          int
	Entries       []model.HybridScheduleEntry
	TotalUsed     int
	EffectiveUsed int
	Summary       string
	ActionLogs    []string
}

// runWeeklyRefineNode 执行“周级单步优化”。
//
// 新链路目标：
//  1. 把全量周数据拆成“按周并发”执行，降低单次模型输入规模；
//  2. 每轮只允许一个动作（Move/Swap）或 done，减少模型犹豫；
//  3. 使用“双预算”约束迭代：
//     3.1 总动作预算：成功/失败都扣减；
//     3.2 有效动作预算：仅成功动作扣减；
//  4. 不在该阶段输出 reasoning 文本，改为阶段状态 + 动作结果，避免刷屏。
func runWeeklyRefineNode(
	ctx context.Context,
	st *SchedulePlanState,
	chatModel *ark.ChatModel,
	outChan chan<- string,
	modelName string,
	emitStage func(stage, detail string),
) (*SchedulePlanState, error) {
	_ = outChan
	if st == nil {
		return nil, fmt.Errorf("schedule plan weekly refine: nil state")
	}
	if chatModel == nil {
		return nil, fmt.Errorf("schedule plan weekly refine: model is nil")
	}
	if len(st.HybridEntries) == 0 {
		st.ReactDone = true
		st.ReactSummary = "无可优化的排程条目。"
		return st, nil
	}
	if strings.TrimSpace(modelName) == "" {
		modelName = "worker"
	}

	// 1. 预算与并发兜底。
	// 1.1 有效预算（旧字段）<=0 时回退默认值；
	// 1.2 总预算 <=0 时回退默认值；
	// 1.3 为避免“有效预算 > 总预算”的反直觉状态，做一次归一化修正；
	// 1.4 周级并发度默认不高于周数，避免空并发浪费。
	if st.WeeklyAdjustBudget <= 0 {
		st.WeeklyAdjustBudget = schedulePlanDefaultWeeklyAdjustBudget
	}
	if st.WeeklyTotalBudget <= 0 {
		st.WeeklyTotalBudget = schedulePlanDefaultWeeklyTotalBudget
	}
	if st.WeeklyAdjustBudget > st.WeeklyTotalBudget {
		st.WeeklyAdjustBudget = st.WeeklyTotalBudget
	}
	if st.WeeklyRefineConcurrency <= 0 {
		st.WeeklyRefineConcurrency = schedulePlanDefaultWeeklyRefineConcurrency
	}

	// 2. 按周拆分输入。
	weekOrder, weekEntries := splitHybridEntriesByWeek(st.HybridEntries)
	if len(weekOrder) == 0 {
		st.ReactDone = true
		st.ReactSummary = "无可优化的排程条目。"
		return st, nil
	}

	// 3. 只对“包含 suggested 的周”分配预算，其余周直接透传。
	activeWeeks := make([]int, 0, len(weekOrder))
	for _, week := range weekOrder {
		if countSuggested(weekEntries[week]) > 0 {
			activeWeeks = append(activeWeeks, week)
		}
	}
	if len(activeWeeks) == 0 {
		st.ReactDone = true
		st.ReactSummary = "当前方案中没有可调整的 suggested 任务，已跳过周级优化。"
		return st, nil
	}

	// 3.1 强制“每个有效周至少 1 个总预算 + 1 个有效预算”。
	// 3.1.1 判断依据：任何有效周都必须有机会进入优化，避免出现 0 预算跳过。
	// 3.1.2 实现方式：当全局预算不足时，自动抬升到 activeWeeks 数量。
	// 3.1.3 失败/兜底：该步骤仅做内存字段修正，不依赖外部资源，不会新增失败点。
	minBudgetRequired := len(activeWeeks)
	if st.WeeklyTotalBudget < minBudgetRequired {
		st.WeeklyTotalBudget = minBudgetRequired
	}
	if st.WeeklyAdjustBudget < minBudgetRequired {
		st.WeeklyAdjustBudget = minBudgetRequired
	}
	if st.WeeklyAdjustBudget > st.WeeklyTotalBudget {
		st.WeeklyAdjustBudget = st.WeeklyTotalBudget
	}

	totalBudgetByWeek, effectiveBudgetByWeek, weeklyLoads, coveredWeeks := splitWeeklyBudgetsByLoad(
		activeWeeks,
		weekEntries,
		st.WeeklyTotalBudget,
		st.WeeklyAdjustBudget,
	)
	budgetIndexByWeek := make(map[int]int, len(activeWeeks))
	for idx, week := range activeWeeks {
		budgetIndexByWeek[week] = idx
	}
	if coveredWeeks < len(activeWeeks) {
		emitStage(
			"schedule_plan.weekly_refine.budget_fallback",
			fmt.Sprintf(
				"周级预算不足以覆盖全部有效周（有效周=%d，至少需预算=%d；当前总预算=%d，有效预算=%d）。已按周负载优先覆盖 %d 个周，其余周预算置 0 并透传原方案。",
				len(activeWeeks),
				len(activeWeeks),
				st.WeeklyTotalBudget,
				st.WeeklyAdjustBudget,
				coveredWeeks,
			),
		)
	}

	workerConcurrency := st.WeeklyRefineConcurrency
	if workerConcurrency > len(activeWeeks) {
		workerConcurrency = len(activeWeeks)
	}
	if workerConcurrency <= 0 {
		workerConcurrency = 1
	}

	emitStage(
		"schedule_plan.weekly_refine.start",
		fmt.Sprintf(
			"周级单步优化开始：周数=%d（可优化=%d），并发度=%d，总动作预算=%d，有效动作预算=%d，覆盖周=%d/%d，周负载=%v。",
			len(weekOrder),
			len(activeWeeks),
			workerConcurrency,
			st.WeeklyTotalBudget,
			st.WeeklyAdjustBudget,
			coveredWeeks,
			len(activeWeeks),
			weeklyLoads,
		),
	)

	// 4. 并发执行“单周 worker”。
	sem := make(chan struct{}, workerConcurrency)
	var wg sync.WaitGroup
	var mu sync.Mutex

	workerResults := make(map[int]weeklyRefineWorkerResult, len(weekOrder))
	var firstErr error
	completedWeeks := 0

	for _, week := range weekOrder {
		week := week
		entries := deepCopyEntries(weekEntries[week])

		// 4.1 没有 suggested 的周直接透传，不占模型调用预算。
		if countSuggested(entries) == 0 {
			workerResults[week] = weeklyRefineWorkerResult{
				Week:    week,
				Entries: entries,
				Summary: fmt.Sprintf("W%d 无 suggested 任务，跳过周级优化。", week),
			}
			continue
		}

		wg.Add(1)
		go func() {
			defer wg.Done()

			select {
			case sem <- struct{}{}:
				defer func() { <-sem }()
			case <-ctx.Done():
				mu.Lock()
				if firstErr == nil {
					firstErr = ctx.Err()
				}
				completedWeeks++
				workerResults[week] = weeklyRefineWorkerResult{
					Week:    week,
					Entries: entries,
					Summary: fmt.Sprintf("W%d 优化取消，已保留原方案。", week),
				}
				emitStage(
					"schedule_plan.weekly_refine.week_done",
					fmt.Sprintf("W%d 已取消并回退原方案。（进度 %d/%d）", week, completedWeeks, len(activeWeeks)),
				)
				mu.Unlock()
				return
			}

			idx := budgetIndexByWeek[week]
			weekTotalBudget := totalBudgetByWeek[idx]
			weekEffectiveBudget := effectiveBudgetByWeek[idx]
			emitStage(
				"schedule_plan.weekly_refine.week_start",
				fmt.Sprintf(
					"W%d 开始周级单步优化：总预算=%d，有效预算=%d。",
					week,
					weekTotalBudget,
					weekEffectiveBudget,
				),
			)

			result, workerErr := runSingleWeekRefineWorker(
				ctx,
				chatModel,
				modelName,
				week,
				entries,
				st.Constraints,
				weeklyPlanningWindow{
					Enabled:   st.HasPlanningWindow,
					StartWeek: st.PlanStartWeek,
					StartDay:  st.PlanStartDay,
					EndWeek:   st.PlanEndWeek,
					EndDay:    st.PlanEndDay,
				},
				weekTotalBudget,
				weekEffectiveBudget,
				emitStage,
			)

			mu.Lock()
			defer mu.Unlock()
			if workerErr != nil && firstErr == nil {
				firstErr = workerErr
			}
			completedWeeks++
			workerResults[week] = result
			emitStage(
				"schedule_plan.weekly_refine.week_done",
				fmt.Sprintf(
					"W%d 周级优化完成（总已用=%d/%d，有效已用=%d/%d）。（进度 %d/%d）",
					week,
					result.TotalUsed,
					weekTotalBudget,
					result.EffectiveUsed,
					weekEffectiveBudget,
					completedWeeks,
					len(activeWeeks),
				),
			)
		}()
	}
	wg.Wait()

	// 5. 汇总 worker 结果，重建全量 HybridEntries。
	mergedEntries := make([]model.HybridScheduleEntry, 0, len(st.HybridEntries))
	st.WeeklyTotalUsed = 0
	st.WeeklyAdjustUsed = 0
	st.WeeklyActionLogs = st.WeeklyActionLogs[:0]
	weekSummaries := make([]string, 0, len(weekOrder))

	for _, week := range weekOrder {
		result, exists := workerResults[week]
		if !exists {
			// 理论上不会发生；兜底透传该周原始条目。
			result = weeklyRefineWorkerResult{
				Week:    week,
				Entries: deepCopyEntries(weekEntries[week]),
				Summary: fmt.Sprintf("W%d 未拿到 worker 结果，已保留原方案。", week),
			}
		}
		mergedEntries = append(mergedEntries, result.Entries...)
		st.WeeklyTotalUsed += result.TotalUsed
		st.WeeklyAdjustUsed += result.EffectiveUsed
		st.WeeklyActionLogs = append(st.WeeklyActionLogs, result.ActionLogs...)
		if strings.TrimSpace(result.Summary) != "" {
			weekSummaries = append(weekSummaries, result.Summary)
		}
	}
	sortHybridEntries(mergedEntries)
	st.HybridEntries = mergedEntries

	// 6. 生成阶段摘要并收口状态。
	st.ReactDone = true
	st.ReactRound = st.WeeklyTotalUsed
	if len(weekSummaries) == 0 {
		st.ReactSummary = fmt.Sprintf(
			"周级优化完成：总动作已用 %d/%d，有效动作已用 %d/%d。",
			st.WeeklyTotalUsed, st.WeeklyTotalBudget, st.WeeklyAdjustUsed, st.WeeklyAdjustBudget,
		)
	} else {
		st.ReactSummary = strings.Join(weekSummaries, "；")
	}
	if firstErr != nil {
		emitStage("schedule_plan.weekly_refine.partial_error", fmt.Sprintf("周级并发优化部分失败，已自动保留失败周原方案。原因：%s", firstErr.Error()))
	}
	emitStage(
		"schedule_plan.weekly_refine.done",
		fmt.Sprintf(
			"周级单步优化结束：总动作已用 %d/%d，有效动作已用 %d/%d。",
			st.WeeklyTotalUsed, st.WeeklyTotalBudget, st.WeeklyAdjustUsed, st.WeeklyAdjustBudget,
		),
	)
	return st, nil
}

// runSingleWeekRefineWorker 执行“单周 + 单步动作”循环。
//
// 流程说明：
// 1. 每轮只允许 1 个工具调用或 done；
// 2. 每次工具调用都扣“总预算”；
// 3. 仅成功调用再扣“有效预算”；
// 4. 工具结果会回灌到下一轮上下文，驱动“走一步看一步”。
func runSingleWeekRefineWorker(
	ctx context.Context,
	chatModel *ark.ChatModel,
	modelName string,
	week int,
	entries []model.HybridScheduleEntry,
	constraints []string,
	window weeklyPlanningWindow,
	totalBudget int,
	effectiveBudget int,
	emitStage func(stage, detail string),
) (weeklyRefineWorkerResult, error) {
	result := weeklyRefineWorkerResult{
		Week:    week,
		Entries: deepCopyEntries(entries),
	}

	if totalBudget <= 0 || effectiveBudget <= 0 {
		result.Summary = fmt.Sprintf("W%d 预算为 0，跳过周级优化。", week)
		return result, nil
	}

	hybridJSON, err := json.Marshal(result.Entries)
	if err != nil {
		result.Summary = fmt.Sprintf("W%d 序列化失败，已保留原方案。", week)
		return result, fmt.Errorf("周级 worker 序列化失败 week=%d: %w", week, err)
	}
	constraintsText := "无"
	if len(constraints) > 0 {
		constraintsText = strings.Join(constraints, "、")
	}

	messages := []*schema.Message{
		schema.SystemMessage(
			renderWeeklyPromptWithBudget(
				effectiveBudget-result.EffectiveUsed,
				effectiveBudget,
				result.EffectiveUsed,
				totalBudget-result.TotalUsed,
				totalBudget,
				result.TotalUsed,
			),
		),
		schema.UserMessage(fmt.Sprintf(
			"当前处理周次：W%d\n以下是当前周混合日程（JSON）：\n%s\n\n用户约束：%s\n\n注意：本 worker 仅允许优化 W%d 内的任务。",
			week,
			string(hybridJSON),
			constraintsText,
			week,
		)),
	}

	for result.TotalUsed < totalBudget && result.EffectiveUsed < effectiveBudget {
		remainingTotal := totalBudget - result.TotalUsed
		remainingEffective := effectiveBudget - result.EffectiveUsed
		emitStage(
			"schedule_plan.weekly_refine.round",
			fmt.Sprintf(
				"W%d 新一轮决策：总预算剩余=%d/%d，有效预算剩余=%d/%d。",
				week,
				remainingTotal,
				totalBudget,
				remainingEffective,
				effectiveBudget,
			),
		)

		// 1. 每轮更新系统提示中的预算占位符。
		messages[0] = schema.SystemMessage(
			renderWeeklyPromptWithBudget(
				remainingEffective,
				effectiveBudget,
				result.EffectiveUsed,
				remainingTotal,
				totalBudget,
				result.TotalUsed,
			),
		)

		roundCtx, cancel := context.WithTimeout(ctx, weeklyReactRoundTimeout)
		content, genErr := generateWeeklyRefineRound(roundCtx, chatModel, messages)
		cancel()
		if genErr != nil {
			result.Summary = fmt.Sprintf("W%d 模型调用失败，已保留当前结果。", week)
			return result, fmt.Errorf("周级 worker 调用失败 week=%d: %w", week, genErr)
		}

		parsed, parseErr := parseReactLLMOutput(content)
		if parseErr != nil {
			result.Summary = fmt.Sprintf("W%d 输出格式异常，已保留当前结果。", week)
			return result, fmt.Errorf("周级 worker 解析失败 week=%d: %w", week, parseErr)
		}

		// 2. done=true 直接正常结束，不再消耗预算。
		if parsed.Done {
			summary := strings.TrimSpace(parsed.Summary)
			if summary == "" {
				summary = fmt.Sprintf(
					"W%d 优化结束（总动作已用 %d/%d，有效动作已用 %d/%d）。",
					week,
					result.TotalUsed, totalBudget,
					result.EffectiveUsed, effectiveBudget,
				)
			}
			result.Summary = summary
			break
		}

		// 3. 只取一个工具调用，强制单步。
		call, warn := pickSingleToolCall(parsed.ToolCalls)
		if call == nil {
			result.Summary = fmt.Sprintf(
				"W%d 无可执行动作，提前结束（总动作已用 %d/%d，有效动作已用 %d/%d）。",
				week,
				result.TotalUsed, totalBudget,
				result.EffectiveUsed, effectiveBudget,
			)
			break
		}
		if warn != "" {
			result.ActionLogs = append(result.ActionLogs, fmt.Sprintf("W%d 警告：%s", week, warn))
		}

		// 4. 执行工具：总预算总是扣减；有效预算仅成功时扣减。
		result.TotalUsed++
		nextEntries, toolResult := dispatchWeeklySingleActionTool(result.Entries, *call, week, window)
		if toolResult.Success {
			result.EffectiveUsed++
			result.Entries = nextEntries
		}

		logLine := fmt.Sprintf(
			"W%d 动作[%s] 结果=%t，总预算=%d/%d，有效预算=%d/%d，详情=%s",
			week,
			toolResult.Tool,
			toolResult.Success,
			result.TotalUsed,
			totalBudget,
			result.EffectiveUsed,
			effectiveBudget,
			toolResult.Result,
		)
		result.ActionLogs = append(result.ActionLogs, logLine)
		statusMark := "FAIL"
		if toolResult.Success {
			statusMark = "OK"
		}
		emitStage("schedule_plan.weekly_refine.tool_call", fmt.Sprintf("[%s] %s", statusMark, logLine))

		// 5. 把“本轮输出 + 工具结果”拼回下一轮上下文，驱动增量推理。
		messages = append(messages, schema.AssistantMessage(content, nil))
		toolResultJSON, _ := json.Marshal([]reactToolResult{toolResult})
		messages = append(messages, schema.UserMessage(
			fmt.Sprintf(
				"上一轮工具结果：%s\n当前预算：总剩余=%d，有效剩余=%d\n请继续按“单步动作”规则决策（仅一个工具调用或 done）。",
				string(toolResultJSON),
				totalBudget-result.TotalUsed,
				effectiveBudget-result.EffectiveUsed,
			),
		))
	}

	if strings.TrimSpace(result.Summary) == "" {
		result.Summary = fmt.Sprintf(
			"W%d 预算耗尽停止（总动作已用 %d/%d，有效动作已用 %d/%d）。",
			week,
			result.TotalUsed, totalBudget,
			result.EffectiveUsed, effectiveBudget,
		)
	}
	return result, nil
}

// generateWeeklyRefineRound 调用模型生成“单周单步”决策输出。
//
// 说明：
// 1. 周级仍保留 thinking（提高复杂排程准确率）；
// 2. 但不把 reasoning 实时透传给前端，避免刷屏；
// 3. 仅返回最终 content，交给 JSON 解析器处理。
func generateWeeklyRefineRound(
	ctx context.Context,
	chatModel *ark.ChatModel,
	messages []*schema.Message,
) (string, error) {
	return agentllm.GenerateScheduleWeeklyReactRound(ctx, chatModel, messages)
}

// renderWeeklyPromptWithBudget 渲染周级单步优化的预算占位符。
//
// 1. 保留旧占位符 {{budget*}} 兼容历史模板；
// 2. 新增 action_total/action_effective 占位符表达双预算语义；
// 3. 所有负值都会在这里兜底归零，避免传给模型异常预算。
func renderWeeklyPromptWithBudget(
	remainingEffective int,
	effectiveBudget int,
	usedEffective int,
	remainingTotal int,
	totalBudget int,
	usedTotal int,
) string {
	if effectiveBudget <= 0 {
		effectiveBudget = schedulePlanDefaultWeeklyAdjustBudget
	}
	if totalBudget <= 0 {
		totalBudget = schedulePlanDefaultWeeklyTotalBudget
	}
	if remainingEffective < 0 {
		remainingEffective = 0
	}
	if remainingTotal < 0 {
		remainingTotal = 0
	}
	if usedEffective < 0 {
		usedEffective = 0
	}
	if usedTotal < 0 {
		usedTotal = 0
	}
	if usedEffective > effectiveBudget {
		usedEffective = effectiveBudget
	}
	if usedTotal > totalBudget {
		usedTotal = totalBudget
	}

	prompt := agentprompt.SchedulePlanWeeklyReactPrompt
	prompt = strings.ReplaceAll(prompt, "{{action_total_remaining}}", fmt.Sprintf("%d", remainingTotal))
	prompt = strings.ReplaceAll(prompt, "{{action_total_budget}}", fmt.Sprintf("%d", totalBudget))
	prompt = strings.ReplaceAll(prompt, "{{action_total_used}}", fmt.Sprintf("%d", usedTotal))
	prompt = strings.ReplaceAll(prompt, "{{action_effective_remaining}}", fmt.Sprintf("%d", remainingEffective))
	prompt = strings.ReplaceAll(prompt, "{{action_effective_budget}}", fmt.Sprintf("%d", effectiveBudget))
	prompt = strings.ReplaceAll(prompt, "{{action_effective_used}}", fmt.Sprintf("%d", usedEffective))

	// 兼容旧模板占位符，避免历史 prompt 残留时出现未替换文本。
	prompt = strings.ReplaceAll(prompt, "{{budget_remaining}}", fmt.Sprintf("%d", remainingEffective))
	prompt = strings.ReplaceAll(prompt, "{{budget_total}}", fmt.Sprintf("%d", effectiveBudget))
	prompt = strings.ReplaceAll(prompt, "{{budget_used}}", fmt.Sprintf("%d", usedEffective))
	prompt = strings.ReplaceAll(prompt, "{{budget}}", fmt.Sprintf("%d（总额度 %d，已用 %d）", remainingEffective, effectiveBudget, usedEffective))
	return prompt
}

// pickSingleToolCall 在“单步动作模式”下选择一个工具调用。
//
// 返回语义：
// 1. call=nil：没有可执行工具；
// 2. warn 非空：模型返回了多个工具，本轮仅执行第一个。
func pickSingleToolCall(toolCalls []reactToolCall) (*reactToolCall, string) {
	if len(toolCalls) == 0 {
		return nil, ""
	}
	call := toolCalls[0]
	if len(toolCalls) == 1 {
		return &call, ""
	}
	return &call, fmt.Sprintf("模型返回了 %d 个工具调用，单步模式仅执行第一个：%s", len(toolCalls), call.Tool)
}

// splitHybridEntriesByWeek 按 week 对混合条目分组并返回稳定周序。
func splitHybridEntriesByWeek(entries []model.HybridScheduleEntry) ([]int, map[int][]model.HybridScheduleEntry) {
	byWeek := make(map[int][]model.HybridScheduleEntry)
	for _, entry := range entries {
		byWeek[entry.Week] = append(byWeek[entry.Week], entry)
	}
	weeks := make([]int, 0, len(byWeek))
	for week := range byWeek {
		weeks = append(weeks, week)
	}
	sort.Ints(weeks)
	return weeks, byWeek
}

type weightedBudgetRemainder struct {
	Index     int
	Remainder int
	Load      int
}

// splitWeeklyBudgetsByLoad 根据“有效周保底 + 周负载加权”拆分预算。
//
// 职责边界：
// 1. 负责：返回与 activeWeeks 同索引对齐的总预算/有效预算；
// 2. 负责：在预算不足时按负载优先覆盖高负载周；
// 3. 不负责：执行周级动作与状态落盘（由 runSingleWeekRefineWorker / runWeeklyRefineNode 负责）。
//
// 输入输出语义：
// 1. coveredWeeks 表示“同时拿到 >=1 总预算和 >=1 有效预算”的周数；
// 2. 当任一全局预算 <=0 时，返回全 0；上游将据此跳过对应周优化；
// 3. 返回的 weeklyLoads 仅用于可观测性，不参与后续状态持久化。
func splitWeeklyBudgetsByLoad(
	activeWeeks []int,
	weekEntries map[int][]model.HybridScheduleEntry,
	totalBudget int,
	effectiveBudget int,
) (totalByWeek []int, effectiveByWeek []int, weeklyLoads []int, coveredWeeks int) {
	weekCount := len(activeWeeks)
	if weekCount == 0 {
		return nil, nil, nil, 0
	}

	if totalBudget < 0 {
		totalBudget = 0
	}
	if effectiveBudget < 0 {
		effectiveBudget = 0
	}

	weeklyLoads = buildWeeklyLoadScores(activeWeeks, weekEntries)
	totalByWeek = make([]int, weekCount)
	effectiveByWeek = make([]int, weekCount)
	if totalBudget == 0 || effectiveBudget == 0 {
		return totalByWeek, effectiveByWeek, weeklyLoads, 0
	}

	// 1. 先计算“可保底覆盖周数”。
	// 1.1 目标是每个有效周至少 1 个总预算 + 1 个有效预算；
	// 1.2 失败场景：当预算小于有效周数量时，不可能全覆盖；
	// 1.3 兜底策略：只覆盖高负载周，避免把预算分散到无法执行的周。
	coveredWeeks = weekCount
	if totalBudget < coveredWeeks {
		coveredWeeks = totalBudget
	}
	if effectiveBudget < coveredWeeks {
		coveredWeeks = effectiveBudget
	}
	if coveredWeeks <= 0 {
		return totalByWeek, effectiveByWeek, weeklyLoads, 0
	}

	coveredIndexes := pickTopLoadWeekIndexes(weeklyLoads, coveredWeeks)
	for _, idx := range coveredIndexes {
		totalByWeek[idx]++
		effectiveByWeek[idx]++
	}

	// 2. 再把剩余预算按周负载加权分配。
	// 2.1 判断依据：负载越高，给到的额外预算越多，优先解决高密度周；
	// 2.2 失败场景：负载异常（<=0）会导致权重失真；
	// 2.3 兜底策略：权重最小按 1 处理，保证分配可持续、不会 panic。
	addWeightedBudget(totalByWeek, weeklyLoads, coveredIndexes, totalBudget-coveredWeeks)
	addWeightedBudget(effectiveByWeek, weeklyLoads, coveredIndexes, effectiveBudget-coveredWeeks)
	return totalByWeek, effectiveByWeek, weeklyLoads, coveredWeeks
}

// buildWeeklyLoadScores 计算每个有效周的负载评分。
//
// 职责边界：
// 1. 负责：以 suggested 任务的节次跨度作为周负载；
// 2. 不负责：预算分配策略与排序决策（由 splitWeeklyBudgetsByLoad/pickTopLoadWeekIndexes 负责）。
func buildWeeklyLoadScores(
	activeWeeks []int,
	weekEntries map[int][]model.HybridScheduleEntry,
) []int {
	loads := make([]int, len(activeWeeks))
	for idx, week := range activeWeeks {
		load := 0
		for _, entry := range weekEntries[week] {
			if entry.Status != "suggested" {
				continue
			}
			span := entry.SectionTo - entry.SectionFrom + 1
			if span <= 0 {
				span = 1
			}
			load += span
		}
		if load <= 0 {
			// 兜底：脏数据或异常节次下仍给该周最小权重，避免被完全饿死。
			load = 1
		}
		loads[idx] = load
	}
	return loads
}

// pickTopLoadWeekIndexes 选择负载最高的 topN 个周索引。
func pickTopLoadWeekIndexes(loads []int, topN int) []int {
	if topN <= 0 || len(loads) == 0 {
		return nil
	}
	indexes := make([]int, len(loads))
	for i := range loads {
		indexes[i] = i
	}
	sort.SliceStable(indexes, func(i, j int) bool {
		left := loads[indexes[i]]
		right := loads[indexes[j]]
		if left != right {
			return left > right
		}
		return indexes[i] < indexes[j]
	})
	if topN > len(indexes) {
		topN = len(indexes)
	}
	selected := append([]int(nil), indexes[:topN]...)
	sort.Ints(selected)
	return selected
}

// addWeightedBudget 把剩余预算按权重分配到目标周。
//
// 说明：
// 1. 先按整数份额分配；
// 2. 再按“最大余数法”分发尾差，保证总和严格守恒；
// 3. 余数相同时优先高负载周，再按索引稳定排序，避免结果抖动。
func addWeightedBudget(
	budgets []int,
	loads []int,
	targetIndexes []int,
	remainingBudget int,
) {
	if remainingBudget <= 0 || len(targetIndexes) == 0 {
		return
	}

	totalLoad := 0
	normalizedLoadByIndex := make(map[int]int, len(targetIndexes))
	for _, idx := range targetIndexes {
		load := 1
		if idx >= 0 && idx < len(loads) && loads[idx] > 0 {
			load = loads[idx]
		}
		normalizedLoadByIndex[idx] = load
		totalLoad += load
	}
	if totalLoad <= 0 {
		// 理论上不会出现；兜底均匀轮询分配，保证不会丢预算。
		for i := 0; i < remainingBudget; i++ {
			budgets[targetIndexes[i%len(targetIndexes)]]++
		}
		return
	}

	allocated := 0
	remainders := make([]weightedBudgetRemainder, 0, len(targetIndexes))
	for _, idx := range targetIndexes {
		load := normalizedLoadByIndex[idx]
		shareProduct := remainingBudget * load
		share := shareProduct / totalLoad
		budgets[idx] += share
		allocated += share
		remainders = append(remainders, weightedBudgetRemainder{
			Index:     idx,
			Remainder: shareProduct % totalLoad,
			Load:      load,
		})
	}

	left := remainingBudget - allocated
	if left <= 0 {
		return
	}
	sort.SliceStable(remainders, func(i, j int) bool {
		if remainders[i].Remainder != remainders[j].Remainder {
			return remainders[i].Remainder > remainders[j].Remainder
		}
		if remainders[i].Load != remainders[j].Load {
			return remainders[i].Load > remainders[j].Load
		}
		return remainders[i].Index < remainders[j].Index
	})
	for i := 0; i < left; i++ {
		budgets[remainders[i%len(remainders)].Index]++
	}
}

// sortHybridEntries 对条目做稳定排序，确保后续预览输出稳定。
func sortHybridEntries(entries []model.HybridScheduleEntry) {
	sort.SliceStable(entries, func(i, j int) bool {
		left := entries[i]
		right := entries[j]
		if left.Week != right.Week {
			return left.Week < right.Week
		}
		if left.DayOfWeek != right.DayOfWeek {
			return left.DayOfWeek < right.DayOfWeek
		}
		if left.SectionFrom != right.SectionFrom {
			return left.SectionFrom < right.SectionFrom
		}
		if left.SectionTo != right.SectionTo {
			return left.SectionTo < right.SectionTo
		}
		if left.Status != right.Status {
			// existing 放前，suggested 放后，便于观察课表底板与建议层。
			return left.Status < right.Status
		}
		return left.Name < right.Name
	})
}

// runFinalCheckNode 负责“终审校验 + 总结生成”。
//
// 职责边界：
// 1. 负责执行物理校验（冲突、节次越界、数量核对）；
// 2. 负责在校验失败时回退到 MergeSnapshot；
// 3. 负责生成最终给用户看的自然语言总结；
// 4. 不负责写库（本期只做预览）。
func runFinalCheckNode(
	ctx context.Context,
	st *SchedulePlanState,
	chatModel *ark.ChatModel,
	emitStage func(stage, detail string),
) (*SchedulePlanState, error) {
	if st == nil {
		return nil, fmt.Errorf("schedule plan final check: nil state")
	}

	emitStage("schedule_plan.final_check.start", "正在进行终审校验。")

	// 1. 先做物理校验。
	issues := physicsCheck(st)
	if len(issues) > 0 {
		emitStage("schedule_plan.final_check.issues", fmt.Sprintf("发现 %d 个问题，已回退到日内优化结果。", len(issues)))
		// 1.1 回退策略：
		// 1.1.1 优先回退到 merge 快照（已经过冲突校验）；
		// 1.1.2 若快照为空，保留当前结果继续走总结，保证可返回。
		if len(st.MergeSnapshot) > 0 {
			st.HybridEntries = deepCopyEntries(st.MergeSnapshot)
		}
	}

	// 2. 生成人性化总结。
	//
	// 2.1 总结失败不影响主流程；
	// 2.2 失败时使用兜底文案，保证前端始终有可展示文本。
	summary, err := generateHumanSummary(ctx, chatModel, st.HybridEntries, st.Constraints, st.WeeklyActionLogs)
	if err != nil || strings.TrimSpace(summary) == "" {
		st.FinalSummary = fmt.Sprintf("排程优化完成，共安排了 %d 个任务。", countSuggested(st.HybridEntries))
	} else {
		st.FinalSummary = strings.TrimSpace(summary)
	}

	emitStage("schedule_plan.final_check.done", "终审校验完成。")
	return st, nil
}

// physicsCheck 执行物理层面校验。
//
// 校验项：
// 1. 时间冲突：同一 slot 不允许多任务占用；
// 2. 节次越界：section 必须落在 1..12 且 from<=to；
// 3. 数量核对：suggested 数量应与原始 AllocatedItems 数量一致。
func physicsCheck(st *SchedulePlanState) []string {
	issues := make([]string, 0)
	if st == nil {
		return append(issues, "state 为空")
	}

	// 1. 时间冲突校验。
	if conflict := detectConflicts(st.HybridEntries); conflict != "" {
		issues = append(issues, "时间冲突："+conflict)
	}

	// 2. 节次越界校验。
	for _, entry := range st.HybridEntries {
		if entry.SectionFrom < 1 || entry.SectionTo > 12 || entry.SectionFrom > entry.SectionTo {
			issues = append(
				issues,
				fmt.Sprintf("节次越界：[%s] W%dD%d 第%d-%d节", entry.Name, entry.Week, entry.DayOfWeek, entry.SectionFrom, entry.SectionTo),
			)
		}
	}

	// 3. 数量一致性校验。
	// 3.1 判断依据：suggested 表示“待应用任务块”，应与 allocatedItems 数量匹配；
	// 3.2 若不匹配，可能表示工具调用丢失或重复覆盖。
	suggestedCount := countSuggested(st.HybridEntries)
	if suggestedCount != len(st.AllocatedItems) {
		issues = append(
			issues,
			fmt.Sprintf("任务数量不匹配：suggested=%d，原始分配=%d", suggestedCount, len(st.AllocatedItems)),
		)
	}

	return issues
}

// countSuggested 统计 suggested 条目数量。
func countSuggested(entries []model.HybridScheduleEntry) int {
	count := 0
	for _, entry := range entries {
		if entry.Status == "suggested" {
			count++
		}
	}
	return count
}

// generateHumanSummary 调用模型生成“用户可读”的总结文案。
//
// 职责边界：
// 1. 只做读模型，不修改任何 state；
// 2. 输出纯文本；
// 3. 失败时把错误返回给上层，由上层决定兜底文案。
func generateHumanSummary(
	ctx context.Context,
	chatModel *ark.ChatModel,
	entries []model.HybridScheduleEntry,
	constraints []string,
	actionLogs []string,
) (string, error) {
	return agentllm.GenerateScheduleHumanSummary(ctx, chatModel, entries, constraints, actionLogs)
}
