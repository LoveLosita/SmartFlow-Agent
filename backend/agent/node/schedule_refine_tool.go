package agentnode

import (
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/LoveLosita/smartflow/backend/logic"
	"github.com/LoveLosita/smartflow/backend/model"
)

// scheduleRefineReactToolCall 表示模型输出的单个工具调用指令。
type scheduleRefineReactToolCall struct {
	Tool   string         `json:"tool"`
	Params map[string]any `json:"params"`
}

// scheduleRefineReactToolResult 表示工具调用的结构化执行结果。
type scheduleRefineReactToolResult struct {
	Tool      string `json:"tool"`
	Success   bool   `json:"success"`
	ErrorCode string `json:"error_code,omitempty"`
	Result    string `json:"result"`
}

// scheduleRefineReactLLMOutput 表示“强 ReAct”要求的固定 JSON 输出结构。
//
// 字段语义：
// 1. goal_check：本轮要先验证的目标点；
// 2. decision：本轮动作选择依据；
// 3. tool_calls：本轮工具动作列表（业务侧只取第一条）。
type scheduleRefineReactLLMOutput struct {
	Done        bool                          `json:"done"`
	Summary     string                        `json:"summary"`
	GoalCheck   string                        `json:"goal_check"`
	Decision    string                        `json:"decision"`
	MissingInfo []string                      `json:"missing_info,omitempty"`
	ToolCalls   []scheduleRefineReactToolCall `json:"tool_calls"`
}

// scheduleRefineReviewOutput 表示终审节点要求的固定 JSON 输出结构。
type scheduleRefineReviewOutput struct {
	Pass   bool     `json:"pass"`
	Reason string   `json:"reason"`
	Unmet  []string `json:"unmet"`
}

// scheduleRefinePlanningWindow 表示微调工具允许活动的 week/day 边界窗口。
//
// 设计说明：
// 1. 这里用已有 HybridEntries 自动推导窗口，避免把任务移动到完全无关的周；
// 2. 若窗口不可用（没有任何 entry），则降级为“仅做基础合法性校验”。
type scheduleRefinePlanningWindow struct {
	Enabled   bool
	StartWeek int
	StartDay  int
	EndWeek   int
	EndDay    int
}

// scheduleRefineToolPolicy 是工具层硬约束策略。
//
// 职责边界：
// 1. 负责承载“是否强制保持相对顺序”的策略开关；
// 2. 负责承载顺序校验需要的 origin_order 映射；
// 3. 不负责语义判定（语义仍由 LLM 终审节点负责）。
type scheduleRefineToolPolicy struct {
	KeepRelativeOrder bool
	OrderScope        string
	OriginOrderMap    map[int]int
}

// dispatchScheduleRefineTool 负责把模型输出的 tool_call 分发到具体工具实现。
//
// 步骤化说明：
// 1. 先识别工具名并路由到对应实现；
// 2. 工具实现内部负责参数校验、冲突校验、边界校验、顺序校验；
// 3. 任何失败都返回 Success=false 的结构化结果，而不是直接 panic。
func dispatchScheduleRefineTool(entries []model.HybridScheduleEntry, call scheduleRefineReactToolCall, window scheduleRefinePlanningWindow, policy scheduleRefineToolPolicy) ([]model.HybridScheduleEntry, scheduleRefineReactToolResult) {
	switch strings.TrimSpace(call.Tool) {
	case "QueryTargetTasks":
		return scheduleRefineToolQueryTargetTasks(entries, call.Params, policy)
	case "QueryAvailableSlots":
		return scheduleRefineToolQueryAvailableSlots(entries, call.Params, window)
	case "Move":
		return scheduleRefineToolMove(entries, call.Params, window, policy)
	case "Swap":
		return scheduleRefineToolSwap(entries, call.Params, window, policy)
	case "BatchMove":
		return scheduleRefineToolBatchMove(entries, call.Params, window, policy)
	case "SpreadEven":
		return scheduleRefineToolSpreadEven(entries, call.Params, window, policy)
	case "MinContextSwitch":
		return scheduleRefineToolMinContextSwitch(entries, call.Params, window, policy)
	case "Verify":
		return scheduleRefineToolVerify(entries, call.Params, policy)
	default:
		return entries, scheduleRefineReactToolResult{
			Tool:    strings.TrimSpace(call.Tool),
			Success: false,
			Result:  fmt.Sprintf("不支持的工具：%s（仅允许 QueryTargetTasks/QueryAvailableSlots/Move/Swap/BatchMove/SpreadEven/MinContextSwitch/Verify）", strings.TrimSpace(call.Tool)),
		}
	}
}

// pickSingleScheduleRefineToolCall 在“单步动作”策略下选取一个工具调用。
//
// 返回语义：
// 1. call=nil：本轮无可执行动作；
// 2. warn 非空：模型返回了多个调用，本轮只执行第一个并记录告警。
func pickSingleScheduleRefineToolCall(calls []scheduleRefineReactToolCall) (*scheduleRefineReactToolCall, string) {
	if len(calls) == 0 {
		return nil, ""
	}
	call := calls[0]
	if len(calls) == 1 {
		return &call, ""
	}
	return &call, fmt.Sprintf("模型返回了 %d 个工具调用，本轮仅执行第一个：%s", len(calls), call.Tool)
}

// parseScheduleRefineLLMOutput 解析模型输出的 ReAct JSON。
//
// 容错策略：
// 1. 兼容 ```json 代码块包装；
// 2. 兼容 JSON 前后有解释性文字（提取最外层对象）。
func parseScheduleRefineLLMOutput(raw string) (*scheduleRefineReactLLMOutput, error) {
	clean := strings.TrimSpace(raw)
	if clean == "" {
		return nil, fmt.Errorf("ReAct 输出为空")
	}
	if strings.HasPrefix(clean, "```") {
		clean = strings.TrimPrefix(clean, "```json")
		clean = strings.TrimPrefix(clean, "```")
		clean = strings.TrimSuffix(clean, "```")
		clean = strings.TrimSpace(clean)
	}

	var out scheduleRefineReactLLMOutput
	if err := json.Unmarshal([]byte(clean), &out); err == nil {
		return &out, nil
	}
	obj, objErr := scheduleRefineExtractFirstJSONObject(clean)
	if objErr != nil {
		return nil, fmt.Errorf("无法从输出中提取 JSON：%s", scheduleRefineTruncate(clean, 220))
	}
	if err := json.Unmarshal([]byte(obj), &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// parseScheduleRefineReviewOutput 解析终审评估节点输出。
func parseScheduleRefineReviewOutput(raw string) (*scheduleRefineReviewOutput, error) {
	clean := strings.TrimSpace(raw)
	if clean == "" {
		return nil, fmt.Errorf("review 输出为空")
	}
	if strings.HasPrefix(clean, "```") {
		clean = strings.TrimPrefix(clean, "```json")
		clean = strings.TrimPrefix(clean, "```")
		clean = strings.TrimSuffix(clean, "```")
		clean = strings.TrimSpace(clean)
	}

	var out scheduleRefineReviewOutput
	if err := json.Unmarshal([]byte(clean), &out); err == nil {
		return &out, nil
	}
	obj, objErr := scheduleRefineExtractFirstJSONObject(clean)
	if objErr != nil {
		return nil, fmt.Errorf("无法从 review 输出中提取 JSON：%s", scheduleRefineTruncate(clean, 220))
	}
	if err := json.Unmarshal([]byte(obj), &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// scheduleRefineToolMove 执行“移动一个 suggested 任务到指定时段”。
//
// 步骤化说明：
// 1. 先校验参数完整性与目标时段合法性，避免写入脏坐标；
// 2. 再校验原任务是否存在、跨度是否一致（防止任务长度被模型改坏）；
// 3. 再校验窗口边界与冲突，确保不会穿透到不可用位置；
// 4. 若启用顺序硬约束，再校验“移动后是否打乱原相对顺序”；
// 5. 全部通过后才真正修改 entries 并返回 Success=true。
func scheduleRefineToolMove(entries []model.HybridScheduleEntry, params map[string]any, window scheduleRefinePlanningWindow, policy scheduleRefineToolPolicy) ([]model.HybridScheduleEntry, scheduleRefineReactToolResult) {
	// 0. task_id 兼容策略：
	// 0.1 标准键是 task_item_id；
	// 0.2 为了兼容模型偶发输出别名 task_id，这里做兜底兼容，避免“语义正确但参数名不一致”导致整轮白跑；
	// 0.3 两者都不存在时，仍按参数缺失返回失败，由上层 ReAct 继续下一轮决策。
	taskID, ok := scheduleRefineParamIntAny(params, "task_item_id", "task_id")
	if !ok {
		return entries, scheduleRefineReactToolResult{Tool: "Move", Success: false, Result: "参数缺失：task_item_id"}
	}
	// 1. 参数兼容策略：
	// 1.1 优先读取标准键（to_week/to_day/...）；
	// 1.2 若模型输出了历史别名（target_xxx/day_of_week 等），也兼容解析；
	// 1.3 目标是减少“仅参数名不一致导致的无效失败轮次”。
	toWeek, okWeek := scheduleRefineParamIntAny(params, "to_week", "target_week", "new_week", "week")
	toDay, okDay := scheduleRefineParamIntAny(params, "to_day", "target_day", "new_day", "target_day_of_week", "day_of_week", "day")
	toSF, okSF := scheduleRefineParamIntAny(params, "to_section_from", "target_section_from", "new_section_from", "section_from")
	toST, okST := scheduleRefineParamIntAny(params, "to_section_to", "target_section_to", "new_section_to", "section_to")
	if !okWeek || !okDay || !okSF || !okST {
		return entries, scheduleRefineReactToolResult{
			Tool:    "Move",
			Success: false,
			Result:  "参数缺失：需要 to_week/to_day/to_section_from/to_section_to",
		}
	}
	if toDay < 1 || toDay > 7 {
		return entries, scheduleRefineReactToolResult{Tool: "Move", Success: false, Result: fmt.Sprintf("day_of_week=%d 非法，必须在 1~7", toDay)}
	}
	if toSF < 1 || toST > 12 || toSF > toST {
		return entries, scheduleRefineReactToolResult{Tool: "Move", Success: false, Result: fmt.Sprintf("节次区间 %d-%d 非法", toSF, toST)}
	}
	allowEmbed := scheduleRefineParamBoolAnyWithDefault(params, true, "allow_embed", "allow_embedding")

	idx, locateErr := scheduleRefineFindUniqueSuggestedByID(entries, taskID)
	if locateErr != nil {
		return entries, scheduleRefineReactToolResult{Tool: "Move", Success: false, Result: locateErr.Error()}
	}
	origSpan := entries[idx].SectionTo - entries[idx].SectionFrom
	newSpan := toST - toSF
	if origSpan != newSpan {
		return entries, scheduleRefineReactToolResult{
			Tool:    "Move",
			Success: false,
			Result:  fmt.Sprintf("任务跨度不一致：原跨度=%d，目标跨度=%d", origSpan+1, newSpan+1),
		}
	}

	if !scheduleRefineIsWithinWindow(window, toWeek, toDay) {
		return entries, scheduleRefineReactToolResult{
			Tool:    "Move",
			Success: false,
			Result:  fmt.Sprintf("目标 W%dD%d 超出允许窗口", toWeek, toDay),
		}
	}

	if conflict, name := scheduleRefineHasConflict(entries, toWeek, toDay, toSF, toST, map[int]bool{idx: true}, allowEmbed); conflict {
		return entries, scheduleRefineReactToolResult{
			Tool:    "Move",
			Success: false,
			Result:  fmt.Sprintf("目标时段已被 %s 占用", name),
		}
	}

	beforeEntries := cloneHybridEntries(entries)
	entry := &entries[idx]
	before := fmt.Sprintf("W%dD%d %d-%d", entry.Week, entry.DayOfWeek, entry.SectionFrom, entry.SectionTo)
	entry.Week = toWeek
	entry.DayOfWeek = toDay
	entry.SectionFrom = toSF
	entry.SectionTo = toST
	after := fmt.Sprintf("W%dD%d %d-%d", entry.Week, entry.DayOfWeek, entry.SectionFrom, entry.SectionTo)

	scheduleRefineSortHybridEntries(entries)
	if issues := scheduleRefineValidateRelativeOrder(entries, policy); len(issues) > 0 {
		return beforeEntries, scheduleRefineReactToolResult{
			Tool:    "Move",
			Success: false,
			Result:  "顺序约束不满足：" + strings.Join(issues, "；"),
		}
	}

	return entries, scheduleRefineReactToolResult{
		Tool:    "Move",
		Success: true,
		Result:  fmt.Sprintf("已将任务[%s](id=%d,type=%s,status=%s) 从 %s 移动到 %s", entry.Name, taskID, strings.TrimSpace(entry.Type), strings.TrimSpace(entry.Status), before, after),
	}
}

// scheduleRefineToolSwap 执行“交换两个 suggested 任务的位置”。
//
// 步骤化说明：
// 1. 先校验两端 task_item_id；
// 2. 再双向验证交换后的落点是否与其他条目冲突；
// 3. 若启用顺序硬约束，再校验“交换后是否打乱相对顺序”；
// 4. 校验通过后提交交换并返回成功。
func scheduleRefineToolSwap(entries []model.HybridScheduleEntry, params map[string]any, window scheduleRefinePlanningWindow, policy scheduleRefineToolPolicy) ([]model.HybridScheduleEntry, scheduleRefineReactToolResult) {
	// 1. 参数兼容策略同 Move：
	// 1.1 兼容 task_a/task_b 与 task_item_a/task_item_b 等常见别名；
	// 1.2 目标是减少模型输出字段差异导致的无效失败。
	idA, okA := scheduleRefineParamIntAny(params, "task_a", "task_item_a", "task_item_id_a")
	idB, okB := scheduleRefineParamIntAny(params, "task_b", "task_item_b", "task_item_id_b")
	if !okA || !okB {
		return entries, scheduleRefineReactToolResult{Tool: "Swap", Success: false, Result: "参数缺失：task_a/task_b"}
	}
	allowEmbed := scheduleRefineParamBoolAnyWithDefault(params, true, "allow_embed", "allow_embedding")
	if idA == idB {
		return entries, scheduleRefineReactToolResult{Tool: "Swap", Success: false, Result: "task_a 与 task_b 不能相同"}
	}

	idxA, errA := scheduleRefineFindUniqueSuggestedByID(entries, idA)
	if errA != nil {
		return entries, scheduleRefineReactToolResult{Tool: "Swap", Success: false, Result: errA.Error()}
	}
	idxB, errB := scheduleRefineFindUniqueSuggestedByID(entries, idB)
	if errB != nil {
		return entries, scheduleRefineReactToolResult{Tool: "Swap", Success: false, Result: errB.Error()}
	}

	a := entries[idxA]
	b := entries[idxB]
	if !scheduleRefineIsWithinWindow(window, b.Week, b.DayOfWeek) || !scheduleRefineIsWithinWindow(window, a.Week, a.DayOfWeek) {
		return entries, scheduleRefineReactToolResult{Tool: "Swap", Success: false, Result: "交换目标超出允许窗口"}
	}

	excludes := map[int]bool{idxA: true, idxB: true}
	if conflict, name := scheduleRefineHasConflict(entries, b.Week, b.DayOfWeek, b.SectionFrom, b.SectionTo, excludes, allowEmbed); conflict {
		return entries, scheduleRefineReactToolResult{Tool: "Swap", Success: false, Result: fmt.Sprintf("任务A交换后将与 %s 冲突", name)}
	}
	if conflict, name := scheduleRefineHasConflict(entries, a.Week, a.DayOfWeek, a.SectionFrom, a.SectionTo, excludes, allowEmbed); conflict {
		return entries, scheduleRefineReactToolResult{Tool: "Swap", Success: false, Result: fmt.Sprintf("任务B交换后将与 %s 冲突", name)}
	}

	beforeEntries := cloneHybridEntries(entries)
	entries[idxA].Week, entries[idxB].Week = entries[idxB].Week, entries[idxA].Week
	entries[idxA].DayOfWeek, entries[idxB].DayOfWeek = entries[idxB].DayOfWeek, entries[idxA].DayOfWeek
	entries[idxA].SectionFrom, entries[idxB].SectionFrom = entries[idxB].SectionFrom, entries[idxA].SectionFrom
	entries[idxA].SectionTo, entries[idxB].SectionTo = entries[idxB].SectionTo, entries[idxA].SectionTo

	scheduleRefineSortHybridEntries(entries)
	if issues := scheduleRefineValidateRelativeOrder(entries, policy); len(issues) > 0 {
		return beforeEntries, scheduleRefineReactToolResult{
			Tool:    "Swap",
			Success: false,
			Result:  "顺序约束不满足：" + strings.Join(issues, "；"),
		}
	}

	return entries, scheduleRefineReactToolResult{
		Tool:    "Swap",
		Success: true,
		Result:  fmt.Sprintf("已交换任务 id=%d 与 id=%d 的时段", idA, idB),
	}
}

// scheduleRefineToolBatchMove 执行“原子批量移动 suggested 任务”。
//
// 步骤化说明：
// 1. 参数要求：params.moves 必须是数组，每个元素都满足 Move 的参数格式；
// 2. 执行策略：在 working 副本上按顺序逐条执行 Move；
// 3. 原子语义：任一步失败，整批回滚（返回原 entries）；全部成功才一次性提交；
// 4. 适用场景：用户明确希望“同一轮挪多个任务”，减少 ReAct 往返轮次。
func scheduleRefineToolBatchMove(entries []model.HybridScheduleEntry, params map[string]any, window scheduleRefinePlanningWindow, policy scheduleRefineToolPolicy) ([]model.HybridScheduleEntry, scheduleRefineReactToolResult) {
	moveParamsList, parseErr := scheduleRefineParseBatchMoveParams(params)
	if parseErr != nil {
		return entries, scheduleRefineReactToolResult{
			Tool:      "BatchMove",
			Success:   false,
			ErrorCode: "PARAM_MISSING",
			Result:    parseErr.Error(),
		}
	}
	// 2. 批级 allow_embed 默认值：
	// 2.1 如果子动作未显式声明 allow_embed/allow_embedding，则继承批级开关；
	// 2.2 默认 true，和 Move/Swap 一致：允许嵌入，但由 QueryAvailableSlots 先给纯空位。
	batchAllowEmbed := scheduleRefineParamBoolAnyWithDefault(params, true, "allow_embed", "allow_embedding")
	for i := range moveParamsList {
		if _, ok := moveParamsList[i]["allow_embed"]; ok {
			continue
		}
		if _, ok := moveParamsList[i]["allow_embedding"]; ok {
			continue
		}
		moveParamsList[i]["allow_embed"] = batchAllowEmbed
	}

	// 1. 在副本上执行，保证原子性：
	// 1.1 每一步都复用 scheduleRefineToolMove 的全部校验逻辑（冲突、窗口、顺序、跨度）；
	// 1.2 只要任一步失败就中止并回滚到原 entries；
	// 1.3 全部成功后再返回 working，作为整批提交结果。
	working := cloneHybridEntries(entries)
	stepSummary := make([]string, 0, len(moveParamsList))
	currentWindow := scheduleRefineBuildPlanningWindowFromEntries(working)
	if !currentWindow.Enabled {
		currentWindow = window
	}
	for idx, moveParams := range moveParamsList {
		nextEntries, stepResult := scheduleRefineToolMove(working, moveParams, currentWindow, policy)
		if !stepResult.Success {
			return entries, scheduleRefineReactToolResult{
				Tool:      "BatchMove",
				Success:   false,
				ErrorCode: scheduleRefineClassifyBatchMoveErrorCode(stepResult.Result),
				Result:    fmt.Sprintf("BatchMove 第%d步失败：%s", idx+1, stepResult.Result),
			}
		}
		working = nextEntries
		currentWindow = scheduleRefineBuildPlanningWindowFromEntries(working)
		stepSummary = append(stepSummary, fmt.Sprintf("第%d步:%s", idx+1, scheduleRefineTruncate(stepResult.Result, 120)))
	}

	return working, scheduleRefineReactToolResult{
		Tool:    "BatchMove",
		Success: true,
		Result:  fmt.Sprintf("BatchMove 原子提交成功，共执行%d步。%s", len(moveParamsList), strings.Join(stepSummary, " | ")),
	}
}

type scheduleRefineCompositePlannerFn func(
	tasks []logic.RefineTaskCandidate,
	slots []logic.RefineSlotCandidate,
	options logic.RefineCompositePlanOptions,
) ([]logic.RefineMovePlanItem, error)

// scheduleRefineToolSpreadEven 执行“均匀铺开”复合动作。
//
// 职责边界：
// 1. 负责参数解析、候选收集、调用确定性规划器；
// 2. 不直接改写 entries，统一通过 BatchMove 原子落地；
// 3. 规划算法实现位于 logic 包，工具层只负责编排。
func scheduleRefineToolSpreadEven(entries []model.HybridScheduleEntry, params map[string]any, window scheduleRefinePlanningWindow, policy scheduleRefineToolPolicy) ([]model.HybridScheduleEntry, scheduleRefineReactToolResult) {
	return scheduleRefineToolCompositeMove(entries, params, window, policy, "SpreadEven", logic.PlanEvenSpreadMoves)
}

// scheduleRefineToolMinContextSwitch 执行“最少上下文切换”复合动作。
//
// 职责边界：
// 1. 负责锁定“当前任务已占坑位集合”，避免为了聚类把任务远距离迁移；
// 2. 负责在固定坑位集合内调用确定性规划器，只重排“任务 -> 坑位”的映射；
// 3. 不直接改写 entries，统一通过 BatchMove 原子落地。
func scheduleRefineToolMinContextSwitch(entries []model.HybridScheduleEntry, params map[string]any, window scheduleRefinePlanningWindow, policy scheduleRefineToolPolicy) ([]model.HybridScheduleEntry, scheduleRefineReactToolResult) {
	taskIDs := scheduleRefineCollectCompositeTaskIDs(params)
	if len(taskIDs) == 0 {
		return entries, scheduleRefineReactToolResult{
			Tool:      "MinContextSwitch",
			Success:   false,
			ErrorCode: "PARAM_MISSING",
			Result:    "参数缺失：复合工具需要 task_item_ids 或 task_item_id",
		}
	}
	tasks, taskResult, ok := scheduleRefineCollectCompositeTasks(entries, taskIDs, policy, "MinContextSwitch")
	if !ok {
		return entries, taskResult
	}

	// 1. MinContextSwitch 的产品语义是“尽量少切换，同时尽量少折腾坑位”；
	// 2. 因此这里不再查询整周新坑位，而是直接复用当前任务已占据的坑位集合；
	// 3. 这样最终只会发生“任务之间互换位置”，不会跳到用户意料之外的远处时段。
	currentSlots := scheduleRefineBuildCompositeCurrentTaskSlots(tasks)
	plannedMoves, planErr := logic.PlanMinContextSwitchMoves(tasks, currentSlots, logic.RefineCompositePlanOptions{})
	if planErr != nil {
		return entries, scheduleRefineReactToolResult{
			Tool:      "MinContextSwitch",
			Success:   false,
			ErrorCode: "PLAN_FAILED",
			Result:    planErr.Error(),
		}
	}
	return scheduleRefineApplyFixedSlotCompositeMoves(entries, policy, "MinContextSwitch", plannedMoves)
}

// scheduleRefineToolCompositeMove 是复合动作工具的统一执行框架。
//
// 步骤化说明：
// 1. 先解析“目标任务集合”，确保任务来源明确且可唯一落到 task_item_id；
// 2. 再按任务跨度查询候选坑位，避免跨度不一致导致执行期失败；
// 3. 调用 logic 包的确定性规划函数，得到 moves；
// 4. 最后复用 BatchMove 原子提交，任一步失败整批回滚。
func scheduleRefineToolCompositeMove(
	entries []model.HybridScheduleEntry,
	params map[string]any,
	window scheduleRefinePlanningWindow,
	policy scheduleRefineToolPolicy,
	toolName string,
	planner scheduleRefineCompositePlannerFn,
) ([]model.HybridScheduleEntry, scheduleRefineReactToolResult) {
	taskIDs := scheduleRefineCollectCompositeTaskIDs(params)
	if len(taskIDs) == 0 {
		return entries, scheduleRefineReactToolResult{
			Tool:      toolName,
			Success:   false,
			ErrorCode: "PARAM_MISSING",
			Result:    "参数缺失：复合工具需要 task_item_ids 或 task_item_id",
		}
	}
	tasks, taskResult, ok := scheduleRefineCollectCompositeTasks(entries, taskIDs, policy, toolName)
	if !ok {
		return entries, taskResult
	}
	idSet := scheduleRefineIntSliceToIDSet(taskIDs)
	spanNeed := scheduleRefineBuildCompositeSpanNeed(tasks)

	slots, slotErr := scheduleRefineCollectCompositeSlotsBySpan(entries, params, window, spanNeed)
	if slotErr != nil {
		return entries, scheduleRefineReactToolResult{
			Tool:      toolName,
			Success:   false,
			ErrorCode: "SLOT_QUERY_FAILED",
			Result:    slotErr.Error(),
		}
	}
	options := logic.RefineCompositePlanOptions{
		ExistingDayLoad: scheduleRefineBuildCompositeDayLoadBaseline(entries, idSet, slots),
	}
	plannedMoves, planErr := planner(tasks, slots, options)
	if planErr != nil {
		return entries, scheduleRefineReactToolResult{
			Tool:      toolName,
			Success:   false,
			ErrorCode: "PLAN_FAILED",
			Result:    planErr.Error(),
		}
	}
	return scheduleRefineApplyCompositePlannedMoves(entries, params, window, policy, toolName, plannedMoves)
}

// scheduleRefineCollectCompositeTasks 收集复合动作参与的可移动任务，并做唯一性校验。
//
// 步骤化说明：
// 1. 只收 suggested 且可移动的 task，避免误改 existing/course；
// 2. task_item_id 必须一一命中，命中多条或缺失都直接失败；
// 3. 输出顺序保持 entries 原始遍历顺序，后续再由规划器做稳定排序。
func scheduleRefineCollectCompositeTasks(entries []model.HybridScheduleEntry, taskIDs []int, policy scheduleRefineToolPolicy, toolName string) ([]logic.RefineTaskCandidate, scheduleRefineReactToolResult, bool) {
	idSet := scheduleRefineIntSliceToIDSet(taskIDs)
	tasks := make([]logic.RefineTaskCandidate, 0, len(taskIDs))
	found := make(map[int]struct{}, len(taskIDs))
	for _, entry := range entries {
		if !scheduleRefineIsMovableSuggestedTask(entry) {
			continue
		}
		if _, ok := idSet[entry.TaskItemID]; !ok {
			continue
		}
		if _, duplicated := found[entry.TaskItemID]; duplicated {
			return nil, scheduleRefineReactToolResult{
				Tool:      toolName,
				Success:   false,
				ErrorCode: "TASK_ID_AMBIGUOUS",
				Result:    fmt.Sprintf("task_item_id=%d 命中多条可移动 suggested 任务，无法唯一定位", entry.TaskItemID),
			}, false
		}
		found[entry.TaskItemID] = struct{}{}
		tasks = append(tasks, logic.RefineTaskCandidate{
			TaskItemID:  entry.TaskItemID,
			Week:        entry.Week,
			DayOfWeek:   entry.DayOfWeek,
			SectionFrom: entry.SectionFrom,
			SectionTo:   entry.SectionTo,
			Name:        strings.TrimSpace(entry.Name),
			ContextTag:  strings.TrimSpace(entry.ContextTag),
			OriginRank:  policy.OriginOrderMap[entry.TaskItemID],
		})
	}
	if len(tasks) != len(taskIDs) {
		missing := make([]int, 0, len(taskIDs))
		for _, id := range taskIDs {
			if _, ok := found[id]; !ok {
				missing = append(missing, id)
			}
		}
		return nil, scheduleRefineReactToolResult{
			Tool:      toolName,
			Success:   false,
			ErrorCode: "TASK_NOT_FOUND",
			Result:    fmt.Sprintf("未找到以下 task_item_id 的可移动 suggested 任务：%v", missing),
		}, false
	}
	return tasks, scheduleRefineReactToolResult{}, true
}

func scheduleRefineBuildCompositeSpanNeed(tasks []logic.RefineTaskCandidate) map[int]int {
	spanNeed := make(map[int]int, len(tasks))
	for _, task := range tasks {
		spanNeed[task.SectionTo-task.SectionFrom+1]++
	}
	return spanNeed
}

func scheduleRefineBuildCompositeCurrentTaskSlots(tasks []logic.RefineTaskCandidate) []logic.RefineSlotCandidate {
	slots := make([]logic.RefineSlotCandidate, 0, len(tasks))
	for _, task := range tasks {
		slots = append(slots, logic.RefineSlotCandidate{
			Week:        task.Week,
			DayOfWeek:   task.DayOfWeek,
			SectionFrom: task.SectionFrom,
			SectionTo:   task.SectionTo,
		})
	}
	return slots
}

func scheduleRefineApplyCompositePlannedMoves(
	entries []model.HybridScheduleEntry,
	params map[string]any,
	window scheduleRefinePlanningWindow,
	policy scheduleRefineToolPolicy,
	toolName string,
	plannedMoves []logic.RefineMovePlanItem,
) ([]model.HybridScheduleEntry, scheduleRefineReactToolResult) {
	if len(plannedMoves) == 0 {
		return entries, scheduleRefineReactToolResult{
			Tool:      toolName,
			Success:   false,
			ErrorCode: "PLAN_EMPTY",
			Result:    "规划结果为空：未生成任何可执行移动",
		}
	}

	moveParams := make([]any, 0, len(plannedMoves))
	for _, move := range plannedMoves {
		moveParams = append(moveParams, map[string]any{
			"task_item_id":    move.TaskItemID,
			"to_week":         move.ToWeek,
			"to_day":          move.ToDay,
			"to_section_from": move.ToSectionFrom,
			"to_section_to":   move.ToSectionTo,
		})
	}
	batchParams := map[string]any{
		"moves":       moveParams,
		"allow_embed": scheduleRefineParamBoolAnyWithDefault(params, true, "allow_embed", "allow_embedding"),
	}
	nextEntries, batchResult := scheduleRefineToolBatchMove(entries, batchParams, window, policy)
	if !batchResult.Success {
		return entries, scheduleRefineReactToolResult{
			Tool:      toolName,
			Success:   false,
			ErrorCode: batchResult.ErrorCode,
			Result:    fmt.Sprintf("%s 执行失败：%s", toolName, batchResult.Result),
		}
	}
	return nextEntries, scheduleRefineReactToolResult{
		Tool:    toolName,
		Success: true,
		Result:  fmt.Sprintf("%s 执行成功：已规划并提交 %d 条移动。", toolName, len(plannedMoves)),
	}
}

// scheduleRefineApplyFixedSlotCompositeMoves 以“同时改写坐标”的方式提交固定坑位重排结果。
//
// 步骤化说明：
// 1. 该函数专门服务“坑位集合固定”的复合工具，避免 BatchMove 顺序执行时出现互相占位冲突；
// 2. 先在副本上一次性改写所有目标任务的坐标，再统一排序与校验；
// 3. 若发现目标坑位重复、任务缺失、或顺序约束不满足，则整批失败并回滚。
func scheduleRefineApplyFixedSlotCompositeMoves(
	entries []model.HybridScheduleEntry,
	policy scheduleRefineToolPolicy,
	toolName string,
	plannedMoves []logic.RefineMovePlanItem,
) ([]model.HybridScheduleEntry, scheduleRefineReactToolResult) {
	if len(plannedMoves) == 0 {
		return entries, scheduleRefineReactToolResult{
			Tool:      toolName,
			Success:   false,
			ErrorCode: "PLAN_EMPTY",
			Result:    "规划结果为空：未生成任何可执行移动",
		}
	}

	working := cloneHybridEntries(entries)
	indexByTaskID := make(map[int]int, len(working))
	for idx, entry := range working {
		if !scheduleRefineIsMovableSuggestedTask(entry) {
			continue
		}
		if _, exists := indexByTaskID[entry.TaskItemID]; exists {
			return entries, scheduleRefineReactToolResult{
				Tool:      toolName,
				Success:   false,
				ErrorCode: "TASK_ID_AMBIGUOUS",
				Result:    fmt.Sprintf("%s 执行失败：task_item_id=%d 命中多条可移动 suggested 任务", toolName, entry.TaskItemID),
			}
		}
		indexByTaskID[entry.TaskItemID] = idx
	}

	targetSeen := make(map[string]int, len(plannedMoves))
	for _, move := range plannedMoves {
		if _, ok := indexByTaskID[move.TaskItemID]; !ok {
			return entries, scheduleRefineReactToolResult{
				Tool:      toolName,
				Success:   false,
				ErrorCode: "TASK_NOT_FOUND",
				Result:    fmt.Sprintf("%s 执行失败：task_item_id=%d 未找到可移动 suggested 任务", toolName, move.TaskItemID),
			}
		}
		key := fmt.Sprintf("%d-%d-%d-%d", move.ToWeek, move.ToDay, move.ToSectionFrom, move.ToSectionTo)
		if prevID, exists := targetSeen[key]; exists {
			return entries, scheduleRefineReactToolResult{
				Tool:      toolName,
				Success:   false,
				ErrorCode: "PLAN_CONFLICT",
				Result:    fmt.Sprintf("%s 执行失败：任务 id=%d 与 id=%d 目标坑位重复", toolName, prevID, move.TaskItemID),
			}
		}
		targetSeen[key] = move.TaskItemID
	}

	for _, move := range plannedMoves {
		idx := indexByTaskID[move.TaskItemID]
		working[idx].Week = move.ToWeek
		working[idx].DayOfWeek = move.ToDay
		working[idx].SectionFrom = move.ToSectionFrom
		working[idx].SectionTo = move.ToSectionTo
	}
	scheduleRefineSortHybridEntries(working)
	if issues := scheduleRefineValidateRelativeOrder(working, policy); len(issues) > 0 {
		return entries, scheduleRefineReactToolResult{
			Tool:      toolName,
			Success:   false,
			ErrorCode: "ORDER_CONSTRAINT_VIOLATED",
			Result:    "顺序约束不满足：" + strings.Join(issues, "；"),
		}
	}
	return working, scheduleRefineReactToolResult{
		Tool:    toolName,
		Success: true,
		Result:  fmt.Sprintf("%s 执行成功：已在固定坑位集合内重排 %d 条任务。", toolName, len(plannedMoves)),
	}
}

func scheduleRefineCollectCompositeTaskIDs(params map[string]any) []int {
	ids := scheduleRefineReadIntSlice(params, "task_item_ids", "task_ids")
	if id, ok := scheduleRefineParamIntAny(params, "task_item_id", "task_id"); ok {
		ids = append(ids, id)
	}
	return scheduleRefineUniquePositiveInts(ids)
}

func scheduleRefineCollectCompositeSlotsBySpan(
	entries []model.HybridScheduleEntry,
	params map[string]any,
	window scheduleRefinePlanningWindow,
	spanNeed map[int]int,
) ([]logic.RefineSlotCandidate, error) {
	if len(spanNeed) == 0 {
		return nil, fmt.Errorf("未识别到任务跨度需求")
	}

	spans := make([]int, 0, len(spanNeed))
	for span := range spanNeed {
		spans = append(spans, span)
	}
	sort.Ints(spans)

	allSlots := make([]logic.RefineSlotCandidate, 0, 16)
	for _, span := range spans {
		required := spanNeed[span]
		queryParams := scheduleRefineBuildCompositeSlotQueryParams(params, span, required)
		_, queryResult := scheduleRefineToolQueryAvailableSlots(entries, queryParams, window)
		if !queryResult.Success {
			return nil, fmt.Errorf("查询跨度=%d 的候选坑位失败：%s", span, queryResult.Result)
		}

		var payload struct {
			Slots []struct {
				Week        int `json:"week"`
				DayOfWeek   int `json:"day_of_week"`
				SectionFrom int `json:"section_from"`
				SectionTo   int `json:"section_to"`
			} `json:"slots"`
		}
		if err := json.Unmarshal([]byte(queryResult.Result), &payload); err != nil {
			return nil, fmt.Errorf("解析跨度=%d 的空位结果失败：%v", span, err)
		}
		if len(payload.Slots) < required {
			return nil, fmt.Errorf("跨度=%d 可用坑位不足：required=%d, got=%d", span, required, len(payload.Slots))
		}
		for _, slot := range payload.Slots {
			allSlots = append(allSlots, logic.RefineSlotCandidate{
				Week:        slot.Week,
				DayOfWeek:   slot.DayOfWeek,
				SectionFrom: slot.SectionFrom,
				SectionTo:   slot.SectionTo,
			})
		}
	}
	return allSlots, nil
}

func scheduleRefineBuildCompositeSlotQueryParams(params map[string]any, span int, required int) map[string]any {
	query := make(map[string]any, 12)
	query["span"] = span

	// 1. limit 以“任务数 * 兜底系数”估算，给规划器保留可选空间；
	// 2. 若调用方显式给了 limit，则采用更大的那个，避免被过小 limit 限死。
	limit := required * 6
	if limit < required {
		limit = required
	}
	if customLimit, ok := scheduleRefineParamIntAny(params, "limit"); ok && customLimit > limit {
		limit = customLimit
	}
	query["limit"] = limit
	query["allow_embed"] = scheduleRefineParamBoolAnyWithDefault(params, true, "allow_embed", "allow_embedding")

	for _, key := range []string{"week", "week_from", "week_to", "day_scope", "after_section", "before_section"} {
		if value, ok := params[key]; ok {
			query[key] = value
		}
	}

	// 1. 复合路由主链路自身使用的是 week_filter/day_of_week/exclude_sections；
	// 2. 这里必须优先透传这些“规范键”，再兼容历史别名；
	// 3. 否则会出现复合工具已被调用，但内部查坑位时丢失目标范围，导致规划结果漂移。
	scheduleRefineCopyIntSliceParam(params, query, "week_filter", "week_filter", "weeks")
	scheduleRefineCopyIntSliceParam(params, query, "day_of_week", "day_of_week", "days", "day_filter")
	scheduleRefineCopyIntSliceParam(params, query, "exclude_sections", "exclude_sections", "exclude_section")

	// 兼容 Move 风格别名，降低模型参数名漂移导致的失败。
	if week, ok := scheduleRefineParamIntAny(params, "to_week", "target_week", "new_week"); ok {
		query["week"] = week
	}
	if day, ok := scheduleRefineParamIntAny(params, "to_day", "target_day", "target_day_of_week", "new_day", "day"); ok {
		query["day_of_week"] = []int{day}
	}
	return query
}

func scheduleRefineCopyIntSliceParam(src map[string]any, dst map[string]any, dstKey string, srcKeys ...string) {
	values := scheduleRefineReadIntSlice(src, srcKeys...)
	if len(values) == 0 {
		return
	}
	normalized := scheduleRefineUniquePositiveInts(values)
	if len(normalized) == 0 {
		return
	}
	dst[dstKey] = normalized
}

func scheduleRefineBuildCompositeDayLoadBaseline(
	entries []model.HybridScheduleEntry,
	excludeTaskIDs map[int]struct{},
	slots []logic.RefineSlotCandidate,
) map[string]int {
	if len(slots) == 0 {
		return nil
	}
	targetDays := make(map[string]struct{}, len(slots))
	for _, slot := range slots {
		targetDays[fmt.Sprintf("%d-%d", slot.Week, slot.DayOfWeek)] = struct{}{}
	}

	load := make(map[string]int, len(targetDays))
	for _, entry := range entries {
		if !scheduleRefineIsMovableSuggestedTask(entry) {
			continue
		}
		if _, excluded := excludeTaskIDs[entry.TaskItemID]; excluded {
			continue
		}
		key := fmt.Sprintf("%d-%d", entry.Week, entry.DayOfWeek)
		if _, inTarget := targetDays[key]; !inTarget {
			continue
		}
		load[key]++
	}
	return load
}

// scheduleRefineToolQueryTargetTasks 查询“本轮潜在目标任务集合”。
//
// 步骤化说明：
// 1. 支持按 day_scope(weekend/workday/all)、week 范围、limit 过滤；
// 2. 只读查询，不修改 entries；
// 3. 返回结构化 JSON 字符串，供下一轮模型直接消费。
func scheduleRefineToolQueryTargetTasks(entries []model.HybridScheduleEntry, params map[string]any, policy scheduleRefineToolPolicy) ([]model.HybridScheduleEntry, scheduleRefineReactToolResult) {
	scope := scheduleRefineNormalizeDayScope(scheduleRefineReadString(params, "day_scope", "all"))
	statusFilter := scheduleRefineNormalizeStatusFilter(scheduleRefineReadString(params, "status", "suggested"))
	weekFilter := scheduleRefineIntSliceToWeekSet(scheduleRefineReadIntSlice(params, "week_filter", "weeks"))
	weekFrom, hasWeekFrom := scheduleRefineParamIntAny(params, "week_from", "from_week")
	weekTo, hasWeekTo := scheduleRefineParamIntAny(params, "week_to", "to_week")
	if week, hasWeek := scheduleRefineParamIntAny(params, "week"); hasWeek {
		weekFrom, weekTo = week, week
		hasWeekFrom, hasWeekTo = true, true
	}
	if hasWeekFrom && hasWeekTo && weekFrom > weekTo {
		weekFrom, weekTo = weekTo, weekFrom
	}
	if !hasWeekFrom || !hasWeekTo {
		startWeek, endWeek := scheduleRefineInferWeekBounds(entries, scheduleRefinePlanningWindow{Enabled: false})
		if !hasWeekFrom {
			weekFrom = startWeek
		}
		if !hasWeekTo {
			weekTo = endWeek
		}
	}
	limit, okLimit := scheduleRefineParamIntAny(params, "limit")
	if !okLimit || limit <= 0 {
		limit = 16
	}
	dayFilter := scheduleRefineIntSliceToDaySet(scheduleRefineReadIntSlice(params, "day_of_week", "days", "day_filter"))
	taskIDs := scheduleRefineReadIntSlice(params, "task_item_ids", "task_ids")
	if taskID, ok := scheduleRefineParamIntAny(params, "task_item_id", "task_id"); ok {
		taskIDs = append(taskIDs, taskID)
	}
	taskIDSet := scheduleRefineIntSliceToIDSet(taskIDs)

	type targetTask struct {
		TaskItemID   int    `json:"task_item_id"`
		Name         string `json:"name"`
		Week         int    `json:"week"`
		DayOfWeek    int    `json:"day_of_week"`
		SectionFrom  int    `json:"section_from"`
		SectionTo    int    `json:"section_to"`
		OriginRank   int    `json:"origin_rank,omitempty"`
		ContextTag   string `json:"context_tag,omitempty"`
		CurrentState string `json:"status"`
	}

	list := make([]targetTask, 0, 32)
	for _, entry := range entries {
		if !scheduleRefineMatchStatusFilter(entry.Status, statusFilter) {
			continue
		}
		// suggested 视图只允许看到“可移动任务”，避免把课程类条目当成可调任务暴露给模型。
		if statusFilter == "suggested" && !scheduleRefineIsMovableSuggestedTask(entry) {
			continue
		}
		if entry.TaskItemID <= 0 {
			continue
		}
		if len(taskIDSet) > 0 {
			if _, ok := taskIDSet[entry.TaskItemID]; !ok {
				continue
			}
		}
		if len(dayFilter) > 0 {
			if _, ok := dayFilter[entry.DayOfWeek]; !ok {
				continue
			}
		} else if !scheduleRefineMatchDayScope(entry.DayOfWeek, scope) {
			continue
		}
		if len(weekFilter) > 0 {
			if _, ok := weekFilter[entry.Week]; !ok {
				continue
			}
		}
		if hasWeekFrom && entry.Week < weekFrom {
			continue
		}
		if hasWeekTo && entry.Week > weekTo {
			continue
		}
		list = append(list, targetTask{
			TaskItemID:   entry.TaskItemID,
			Name:         strings.TrimSpace(entry.Name),
			Week:         entry.Week,
			DayOfWeek:    entry.DayOfWeek,
			SectionFrom:  entry.SectionFrom,
			SectionTo:    entry.SectionTo,
			OriginRank:   policy.OriginOrderMap[entry.TaskItemID],
			ContextTag:   strings.TrimSpace(entry.ContextTag),
			CurrentState: entry.Status,
		})
	}
	sort.SliceStable(list, func(i, j int) bool {
		if list[i].Week != list[j].Week {
			return list[i].Week < list[j].Week
		}
		if list[i].DayOfWeek != list[j].DayOfWeek {
			return list[i].DayOfWeek < list[j].DayOfWeek
		}
		if list[i].SectionFrom != list[j].SectionFrom {
			return list[i].SectionFrom < list[j].SectionFrom
		}
		return list[i].TaskItemID < list[j].TaskItemID
	})
	if len(list) > limit {
		list = list[:limit]
	}

	payload := map[string]any{
		"tool":        "QueryTargetTasks",
		"count":       len(list),
		"status":      statusFilter,
		"day_scope":   scope,
		"week_filter": scheduleRefineKeysOfIntSet(weekFilter),
		"week_from":   weekFrom,
		"week_to":     weekTo,
		"day_of_week": scheduleRefineKeysOfIntSet(dayFilter),
		"items":       list,
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return entries, scheduleRefineReactToolResult{
			Tool:      "QueryTargetTasks",
			Success:   false,
			ErrorCode: "QUERY_ENCODE_FAILED",
			Result:    fmt.Sprintf("序列化查询结果失败：%v", err),
		}
	}
	return entries, scheduleRefineReactToolResult{
		Tool:    "QueryTargetTasks",
		Success: true,
		Result:  string(raw),
	}
}

// scheduleRefineToolQueryAvailableSlots 查询“可放置 suggested 的空位”。
//
// 步骤化说明：
// 1. 根据 day_scope/week 范围/span/exclude_sections 过滤候选时段；
// 2. 默认先收集“纯空位”，不足 limit 再补“可嵌入课程位”（第二优先级）；
// 3. 返回结构化 JSON 字符串，不修改 entries。
func scheduleRefineToolQueryAvailableSlots(entries []model.HybridScheduleEntry, params map[string]any, window scheduleRefinePlanningWindow) ([]model.HybridScheduleEntry, scheduleRefineReactToolResult) {
	scope := scheduleRefineNormalizeDayScope(scheduleRefineReadString(params, "day_scope", "all"))
	dayFilter := scheduleRefineIntSliceToDaySet(scheduleRefineReadIntSlice(params, "day_of_week", "days", "day_filter"))
	weekFilter := scheduleRefineIntSliceToWeekSet(scheduleRefineReadIntSlice(params, "week_filter", "weeks"))
	// 1. 空位优先策略：
	// 1.1 默认 allow_embed=true，但查询分两阶段执行；
	// 1.2 第一阶段只收集“纯空白位”（不与 existing 重叠）；
	// 1.3 第二阶段仅在空白位不足 limit 时，补充“可嵌入课程位”。
	allowEmbed := scheduleRefineParamBoolAnyWithDefault(params, true, "allow_embed", "allow_embedding")
	// 1.4 兼容 slot_type/slot_types：
	// 1.4.1 当明确请求 pure/empty/strict 时，强制只查纯空位（关闭嵌入候选）。
	// 1.4.2 当未声明时，维持“空位优先，空位不足再补嵌入候选”的默认策略。
	slotTypeHints := scheduleRefineReadStringSlice(params, "slot_types")
	if single := strings.TrimSpace(scheduleRefineReadString(params, "slot_type", "")); single != "" {
		slotTypeHints = append(slotTypeHints, single)
	}
	for _, hint := range slotTypeHints {
		normalized := strings.ToLower(strings.TrimSpace(hint))
		if normalized == "pure" || normalized == "empty" || normalized == "strict" {
			allowEmbed = false
			break
		}
	}
	span, okSpan := scheduleRefineParamIntAny(params, "span", "section_duration", "task_duration")
	if !okSpan || span <= 0 {
		span = 2
	}
	if span > 12 {
		return entries, scheduleRefineReactToolResult{
			Tool:      "QueryAvailableSlots",
			Success:   false,
			ErrorCode: "SPAN_INVALID",
			Result:    fmt.Sprintf("span=%d 非法，必须在 1~12", span),
		}
	}
	limit, okLimit := scheduleRefineParamIntAny(params, "limit")
	if !okLimit || limit <= 0 {
		limit = 12
	}

	weekFrom, hasWeekFrom := scheduleRefineParamIntAny(params, "week_from", "from_week")
	weekTo, hasWeekTo := scheduleRefineParamIntAny(params, "week_to", "to_week")
	if week, hasWeek := scheduleRefineParamIntAny(params, "week"); hasWeek {
		weekFrom, weekTo = week, week
		hasWeekFrom, hasWeekTo = true, true
	}
	if hasWeekFrom && hasWeekTo && weekFrom > weekTo {
		weekFrom, weekTo = weekTo, weekFrom
	}
	if !hasWeekFrom || !hasWeekTo {
		startWeek, endWeek := scheduleRefineInferWeekBounds(entries, window)
		if !hasWeekFrom {
			weekFrom = startWeek
		}
		if !hasWeekTo {
			weekTo = endWeek
		}
	}
	weeksToIterate := scheduleRefineBuildWeekIterList(weekFilter, weekFrom, weekTo)
	if len(weeksToIterate) == 0 {
		return entries, scheduleRefineReactToolResult{
			Tool:      "QueryAvailableSlots",
			Success:   false,
			ErrorCode: "PARAM_MISSING",
			Result:    "周范围为空：请提供 week / week_filter 或确保排程窗口有效",
		}
	}
	weekFrom = weeksToIterate[0]
	weekTo = weeksToIterate[len(weeksToIterate)-1]

	excludedSet := make(map[int]struct{})
	for _, sec := range scheduleRefineReadIntSlice(params, "exclude_sections", "exclude_section") {
		if sec >= 1 && sec <= 12 {
			excludedSet[sec] = struct{}{}
		}
	}
	afterSection, hasAfter := scheduleRefineParamIntAny(params, "after_section")
	beforeSection, hasBefore := scheduleRefineParamIntAny(params, "before_section")
	exactSectionFrom, hasExactFrom := scheduleRefineParamIntAny(params, "section_from", "target_section_from")
	exactSectionTo, hasExactTo := scheduleRefineParamIntAny(params, "section_to", "target_section_to")
	if hasExactFrom != hasExactTo {
		return entries, scheduleRefineReactToolResult{
			Tool:      "QueryAvailableSlots",
			Success:   false,
			ErrorCode: "PARAM_MISSING",
			Result:    "精确节次查询需同时提供 section_from 和 section_to",
		}
	}
	if hasExactFrom {
		if exactSectionFrom < 1 || exactSectionTo > 12 || exactSectionFrom > exactSectionTo {
			return entries, scheduleRefineReactToolResult{
				Tool:      "QueryAvailableSlots",
				Success:   false,
				ErrorCode: "SPAN_INVALID",
				Result:    fmt.Sprintf("精确节次区间非法：%d-%d", exactSectionFrom, exactSectionTo),
			}
		}
		span = exactSectionTo - exactSectionFrom + 1
	}

	type slot struct {
		Week        int    `json:"week"`
		DayOfWeek   int    `json:"day_of_week"`
		SectionFrom int    `json:"section_from"`
		SectionTo   int    `json:"section_to"`
		SlotType    string `json:"slot_type,omitempty"`
	}
	slots := make([]slot, 0, limit)
	seen := make(map[string]struct{}, limit*2)
	strictCount := 0
	collect := func(embedAllowed bool, slotType string) {
		if len(slots) >= limit {
			return
		}
		for _, week := range weeksToIterate {
			for day := 1; day <= 7; day++ {
				if len(dayFilter) > 0 {
					if _, ok := dayFilter[day]; !ok {
						continue
					}
				} else if !scheduleRefineMatchDayScope(day, scope) {
					continue
				}
				if !scheduleRefineIsWithinWindow(window, week, day) {
					continue
				}
				for sf := 1; sf+span-1 <= 12; sf++ {
					st := sf + span - 1
					if hasExactFrom && (sf != exactSectionFrom || st != exactSectionTo) {
						continue
					}
					if hasAfter && sf <= afterSection {
						continue
					}
					if hasBefore && st >= beforeSection {
						continue
					}
					if scheduleRefineIntersectsExcludedSections(sf, st, excludedSet) {
						continue
					}
					if conflict, _ := scheduleRefineHasConflict(entries, week, day, sf, st, nil, embedAllowed); conflict {
						continue
					}
					key := fmt.Sprintf("%d-%d-%d-%d", week, day, sf, st)
					if _, ok := seen[key]; ok {
						continue
					}
					seen[key] = struct{}{}
					slots = append(slots, slot{
						Week:        week,
						DayOfWeek:   day,
						SectionFrom: sf,
						SectionTo:   st,
						SlotType:    slotType,
					})
					if len(slots) >= limit {
						return
					}
				}
			}
		}
	}
	collect(false, "empty")
	strictCount = len(slots)
	if allowEmbed && len(slots) < limit {
		collect(true, "embedded_candidate")
	}
	embeddedCount := len(slots) - strictCount

	payload := map[string]any{
		"tool":             "QueryAvailableSlots",
		"count":            len(slots),
		"strict_count":     strictCount,
		"embedded_count":   embeddedCount,
		"fallback_used":    embeddedCount > 0,
		"day_scope":        scope,
		"day_of_week":      scheduleRefineKeysOfIntSet(dayFilter),
		"week_filter":      scheduleRefineKeysOfIntSet(weekFilter),
		"week_from":        weekFrom,
		"week_to":          weekTo,
		"span":             span,
		"allow_embed":      allowEmbed,
		"exclude_sections": scheduleRefineKeysOfIntSet(excludedSet),
		"slots":            slots,
	}
	if hasAfter {
		payload["after_section"] = afterSection
	}
	if hasBefore {
		payload["before_section"] = beforeSection
	}
	if hasExactFrom {
		payload["section_from"] = exactSectionFrom
		payload["section_to"] = exactSectionTo
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return entries, scheduleRefineReactToolResult{
			Tool:      "QueryAvailableSlots",
			Success:   false,
			ErrorCode: "QUERY_ENCODE_FAILED",
			Result:    fmt.Sprintf("序列化空位结果失败：%v", err),
		}
	}
	return entries, scheduleRefineReactToolResult{
		Tool:    "QueryAvailableSlots",
		Success: true,
		Result:  string(raw),
	}
}

// scheduleRefineToolVerify 进行“轻量确定性自检”。
//
// 说明：
// 1. 当前只做 deterministic 校验（冲突/顺序），不做语义 LLM 终审；
// 2. 语义层终审仍在 hard_check 节点统一处理；
// 3. 该工具用于给执行阶段一个“可提前自查”的信号。
func scheduleRefineToolVerify(entries []model.HybridScheduleEntry, params map[string]any, policy scheduleRefineToolPolicy) ([]model.HybridScheduleEntry, scheduleRefineReactToolResult) {
	physicsIssues := scheduleRefinePhysicsCheck(entries, 0)
	orderIssues := scheduleRefineValidateRelativeOrder(entries, policy)
	if len(physicsIssues) > 0 || len(orderIssues) > 0 {
		payload := map[string]any{
			"tool":           "Verify",
			"pass":           false,
			"physics_issues": physicsIssues,
			"order_issues":   orderIssues,
		}
		raw, err := json.Marshal(payload)
		if err != nil {
			return entries, scheduleRefineReactToolResult{
				Tool:      "Verify",
				Success:   false,
				ErrorCode: "VERIFY_FAILED",
				Result:    "Verify 校验失败且结果无法序列化",
			}
		}
		return entries, scheduleRefineReactToolResult{
			Tool:      "Verify",
			Success:   false,
			ErrorCode: "VERIFY_FAILED",
			Result:    string(raw),
		}
	}

	// 1. 若携带 task_item_id / 目标坐标参数，则执行“针对性核验”，避免“全局 pass”掩盖当前任务不匹配。
	// 2. 该核验是可选增强：没传 task_id 时仍维持全局 deterministic 行为。
	taskID, hasTaskID := scheduleRefineParamIntAny(params, "task_item_id", "task_id")
	if hasTaskID {
		idx, locateErr := scheduleRefineFindUniqueSuggestedByID(entries, taskID)
		if locateErr != nil {
			return entries, scheduleRefineReactToolResult{
				Tool:      "Verify",
				Success:   false,
				ErrorCode: "VERIFY_FAILED",
				Result:    fmt.Sprintf(`{"tool":"Verify","pass":false,"reason":"%s"}`, locateErr.Error()),
			}
		}
		target := entries[idx]
		verifyWeek, hasWeek := scheduleRefineParamIntAny(params, "week", "to_week", "target_week")
		verifyDay, hasDay := scheduleRefineParamIntAny(params, "day_of_week", "to_day", "target_day_of_week")
		verifyFrom, hasFrom := scheduleRefineParamIntAny(params, "section_from", "to_section_from", "target_section_from")
		verifyTo, hasTo := scheduleRefineParamIntAny(params, "section_to", "to_section_to", "target_section_to")

		mismatch := make([]string, 0, 4)
		if hasWeek && target.Week != verifyWeek {
			mismatch = append(mismatch, fmt.Sprintf("week=%d(实际=%d)", verifyWeek, target.Week))
		}
		if hasDay && target.DayOfWeek != verifyDay {
			mismatch = append(mismatch, fmt.Sprintf("day_of_week=%d(实际=%d)", verifyDay, target.DayOfWeek))
		}
		if hasFrom && target.SectionFrom != verifyFrom {
			mismatch = append(mismatch, fmt.Sprintf("section_from=%d(实际=%d)", verifyFrom, target.SectionFrom))
		}
		if hasTo && target.SectionTo != verifyTo {
			mismatch = append(mismatch, fmt.Sprintf("section_to=%d(实际=%d)", verifyTo, target.SectionTo))
		}
		if len(mismatch) > 0 {
			return entries, scheduleRefineReactToolResult{
				Tool:      "Verify",
				Success:   false,
				ErrorCode: "VERIFY_FAILED",
				Result:    fmt.Sprintf(`{"tool":"Verify","pass":false,"reason":"任务坐标不匹配：%s"}`, strings.Join(mismatch, "；")),
			}
		}
		return entries, scheduleRefineReactToolResult{
			Tool:    "Verify",
			Success: true,
			Result:  `{"tool":"Verify","pass":true,"reason":"task-level deterministic checks passed"}`,
		}
	}

	return entries, scheduleRefineReactToolResult{
		Tool:    "Verify",
		Success: true,
		Result:  `{"tool":"Verify","pass":true,"reason":"deterministic checks passed"}`,
	}
}

// scheduleRefineValidateRelativeOrder 校验 suggested 任务是否保持“初始相对顺序”。
//
// 步骤化说明：
// 1. 若策略未启用 keep_relative_order，直接通过；
// 2. 否则按时间位置排序 suggested 任务，并映射到 origin_rank；
// 3. 检查 rank 是否单调不降；一旦逆序即判定失败；
// 4. 支持 week 作用域：仅要求每周内保持相对顺序。
func scheduleRefineValidateRelativeOrder(entries []model.HybridScheduleEntry, policy scheduleRefineToolPolicy) []string {
	if !policy.KeepRelativeOrder {
		return nil
	}
	if len(policy.OriginOrderMap) == 0 {
		return []string{"未提供顺序基线(origin_order_map)"}
	}

	suggested := make([]model.HybridScheduleEntry, 0, len(entries))
	for _, entry := range entries {
		// 1. 顺序校验与执行口径必须一致：
		// 1.1 这里只校验“可移动 suggested 任务”，避免把 course 等不可移动条目误纳入顺序约束；
		// 1.2 若把不可移动条目纳入，会出现“动作层不允许改、顺序层却报错”的左右脑互搏。
		if scheduleRefineIsMovableSuggestedTask(entry) {
			suggested = append(suggested, entry)
		}
	}
	if len(suggested) <= 1 {
		return nil
	}
	sort.SliceStable(suggested, func(i, j int) bool {
		left := suggested[i]
		right := suggested[j]
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
		return left.TaskItemID < right.TaskItemID
	})

	scope := scheduleRefineNormalizeOrderScope(policy.OrderScope)
	issues := make([]string, 0, 4)
	if scope == "week" {
		lastRankByWeek := make(map[int]int)
		lastNameByWeek := make(map[int]string)
		lastIDByWeek := make(map[int]int)
		for _, entry := range suggested {
			rank, ok := policy.OriginOrderMap[entry.TaskItemID]
			if !ok {
				issues = append(issues, fmt.Sprintf("任务 id=%d 缺少 origin_rank", entry.TaskItemID))
				continue
			}
			last, exists := lastRankByWeek[entry.Week]
			if exists && rank < last {
				issues = append(issues, fmt.Sprintf(
					"W%d 出现逆序：任务[%s](id=%d,rank=%d) 被排在 [%s](id=%d,rank=%d) 之后",
					entry.Week, entry.Name, entry.TaskItemID, rank, lastNameByWeek[entry.Week], lastIDByWeek[entry.Week], last,
				))
			}
			lastRankByWeek[entry.Week] = rank
			lastNameByWeek[entry.Week] = entry.Name
			lastIDByWeek[entry.Week] = entry.TaskItemID
		}
		return issues
	}

	lastRank := -1
	lastName := ""
	lastID := 0
	for _, entry := range suggested {
		rank, ok := policy.OriginOrderMap[entry.TaskItemID]
		if !ok {
			issues = append(issues, fmt.Sprintf("任务 id=%d 缺少 origin_rank", entry.TaskItemID))
			continue
		}
		if lastRank >= 0 && rank < lastRank {
			issues = append(issues, fmt.Sprintf(
				"出现逆序：任务[%s](id=%d,rank=%d) 被排在 [%s](id=%d,rank=%d) 之后",
				entry.Name, entry.TaskItemID, rank, lastName, lastID, lastRank,
			))
		}
		lastRank = rank
		lastName = entry.Name
		lastID = entry.TaskItemID
	}
	return issues
}

// scheduleRefineNormalizeOrderScope 规范化顺序约束作用域。
func scheduleRefineNormalizeOrderScope(scope string) string {
	switch strings.TrimSpace(strings.ToLower(scope)) {
	case "week":
		return "week"
	default:
		return "global"
	}
}

// scheduleRefineBuildPlanningWindowFromEntries 根据现有条目推导允许移动窗口。
func scheduleRefineBuildPlanningWindowFromEntries(entries []model.HybridScheduleEntry) scheduleRefinePlanningWindow {
	if len(entries) == 0 {
		return scheduleRefinePlanningWindow{Enabled: false}
	}
	startWeek, startDay := entries[0].Week, entries[0].DayOfWeek
	endWeek, endDay := entries[0].Week, entries[0].DayOfWeek
	for _, entry := range entries {
		if scheduleRefineCompareWeekDay(entry.Week, entry.DayOfWeek, startWeek, startDay) < 0 {
			startWeek, startDay = entry.Week, entry.DayOfWeek
		}
		if scheduleRefineCompareWeekDay(entry.Week, entry.DayOfWeek, endWeek, endDay) > 0 {
			endWeek, endDay = entry.Week, entry.DayOfWeek
		}
	}
	return scheduleRefinePlanningWindow{
		Enabled:   true,
		StartWeek: startWeek,
		StartDay:  startDay,
		EndWeek:   endWeek,
		EndDay:    endDay,
	}
}

// scheduleRefineIsWithinWindow 判断目标 week/day 是否落在窗口内。
func scheduleRefineIsWithinWindow(window scheduleRefinePlanningWindow, week, day int) bool {
	if !window.Enabled {
		return true
	}
	if day < 1 || day > 7 {
		return false
	}
	if scheduleRefineCompareWeekDay(week, day, window.StartWeek, window.StartDay) < 0 {
		return false
	}
	if scheduleRefineCompareWeekDay(week, day, window.EndWeek, window.EndDay) > 0 {
		return false
	}
	return true
}

// scheduleRefineCompareWeekDay 比较两个 week/day 坐标。
// 返回：
// 1) <0：left 更早；
// 2) =0：相同；
// 3) >0：left 更晚。
func scheduleRefineCompareWeekDay(leftWeek, leftDay, rightWeek, rightDay int) int {
	if leftWeek != rightWeek {
		return leftWeek - rightWeek
	}
	return leftDay - rightDay
}

// scheduleRefineFindSuggestedByID 在 entries 中查找指定 task_item_id 的 suggested 条目索引。
func scheduleRefineFindSuggestedByID(entries []model.HybridScheduleEntry, taskItemID int) int {
	for i, entry := range entries {
		if scheduleRefineIsMovableSuggestedTask(entry) && entry.TaskItemID == taskItemID {
			return i
		}
	}
	return -1
}

// scheduleRefineFindUniqueSuggestedByID 查找可唯一定位的可移动 suggested 任务。
//
// 说明：
// 1. “可移动”定义由 scheduleRefineIsMovableSuggestedTask 统一控制；
// 2. 当 task_item_id 命中 0 条或 >1 条时都返回错误，避免把动作落到错误任务上。
func scheduleRefineFindUniqueSuggestedByID(entries []model.HybridScheduleEntry, taskItemID int) (int, error) {
	first := -1
	count := 0
	for idx, entry := range entries {
		if !scheduleRefineIsMovableSuggestedTask(entry) {
			continue
		}
		if entry.TaskItemID != taskItemID {
			continue
		}
		if first < 0 {
			first = idx
		}
		count++
	}
	if count == 0 {
		return -1, fmt.Errorf("未找到 task_item_id=%d 的可移动 suggested 任务", taskItemID)
	}
	if count > 1 {
		return -1, fmt.Errorf("task_item_id=%d 命中 %d 条可移动 suggested 任务，无法唯一定位", taskItemID, count)
	}
	return first, nil
}

// scheduleRefineIsMovableSuggestedTask 判断条目是否属于“可被微调工具改写”的任务。
//
// 规则：
// 1. 必须是 suggested 且 task_item_id>0；
// 2. type=course 明确禁止移动（即便被错误标记为 suggested）；
// 3. 其余类型（含空值）按任务处理，兼容历史快照。
func scheduleRefineIsMovableSuggestedTask(entry model.HybridScheduleEntry) bool {
	if strings.TrimSpace(entry.Status) != "suggested" || entry.TaskItemID <= 0 {
		return false
	}
	if strings.EqualFold(strings.TrimSpace(entry.Type), "course") {
		return false
	}
	return true
}

// scheduleRefineHasConflict 检查目标时段是否与其他条目冲突。
//
// 判断规则：
// 1. 仅把“会阻塞 suggested 的条目”纳入冲突判断；
// 2. excludes 中的索引会被跳过（常用于 Move 自身排除或 Swap 双排除）。
func scheduleRefineHasConflict(entries []model.HybridScheduleEntry, week, day, sf, st int, excludes map[int]bool, allowEmbed bool) (bool, string) {
	for idx, entry := range entries {
		if excludes != nil && excludes[idx] {
			continue
		}
		if !scheduleRefineEntryBlocksSuggestedWithPolicy(entry, allowEmbed) {
			continue
		}
		if entry.Week == week && entry.DayOfWeek == day && scheduleRefineSectionsOverlap(entry.SectionFrom, entry.SectionTo, sf, st) {
			return true, fmt.Sprintf("%s(%s)", entry.Name, entry.Type)
		}
	}
	return false, ""
}

// scheduleRefineEntryBlocksSuggested 判断条目是否会阻塞 suggested 任务落位。
func scheduleRefineEntryBlocksSuggested(entry model.HybridScheduleEntry) bool {
	return scheduleRefineEntryBlocksSuggestedWithPolicy(entry, true)
}

// scheduleRefineEntryBlocksSuggestedWithPolicy 判断条目是否阻塞 suggested 落位。
//
// 策略说明：
// 1. allowEmbed=true：沿用 block_for_suggested 语义；
// 2. allowEmbed=false：existing 一律阻塞，只允许纯空白课位；
// 3. unknown status 保守阻塞，防止漏检。
func scheduleRefineEntryBlocksSuggestedWithPolicy(entry model.HybridScheduleEntry, allowEmbed bool) bool {
	if entry.Status == "suggested" {
		return true
	}
	if entry.Status == "existing" {
		if !allowEmbed {
			return true
		}
		return entry.BlockForSuggested
	}
	// 未知状态保守处理为阻塞，避免写入潜在冲突。
	return true
}

// scheduleRefineSectionsOverlap 判断两个节次区间是否有交叠。
func scheduleRefineSectionsOverlap(aFrom, aTo, bFrom, bTo int) bool {
	return aFrom <= bTo && bFrom <= aTo
}

// scheduleRefineParamInt 从 map 中提取 int 参数，兼容 JSON 常见数值类型。
func scheduleRefineParamInt(params map[string]any, key string) (int, bool) {
	raw, ok := params[key]
	if !ok {
		return 0, false
	}
	switch v := raw.(type) {
	case int:
		return v, true
	case float64:
		return int(v), true
	case string:
		n, err := strconv.Atoi(strings.TrimSpace(v))
		if err != nil {
			return 0, false
		}
		return n, true
	default:
		return 0, false
	}
}

// scheduleRefineParamIntAny 按“候选键优先级”提取 int 参数。
//
// 步骤化说明：
// 1. 按传入顺序依次尝试每个 key；
// 2. 命中第一个合法值即返回；
// 3. 全部未命中则返回 false，由上层统一抛参数缺失错误。
func scheduleRefineParamIntAny(params map[string]any, keys ...string) (int, bool) {
	for _, key := range keys {
		if v, ok := scheduleRefineParamInt(params, key); ok {
			return v, true
		}
	}
	return 0, false
}

// scheduleRefineParamBool 从 map 中提取 bool 参数，兼容 JSON 常见布尔表示。
func scheduleRefineParamBool(params map[string]any, key string) (bool, bool) {
	raw, ok := params[key]
	if !ok {
		return false, false
	}
	switch v := raw.(type) {
	case bool:
		return v, true
	case string:
		text := strings.TrimSpace(strings.ToLower(v))
		switch text {
		case "true", "1", "yes", "y":
			return true, true
		case "false", "0", "no", "n":
			return false, true
		default:
			return false, false
		}
	case int:
		if v == 1 {
			return true, true
		}
		if v == 0 {
			return false, true
		}
		return false, false
	case float64:
		if v == 1 {
			return true, true
		}
		if v == 0 {
			return false, true
		}
		return false, false
	default:
		return false, false
	}
}

// scheduleRefineParamBoolAnyWithDefault 按候选键提取 bool，未命中时返回 fallback。
func scheduleRefineParamBoolAnyWithDefault(params map[string]any, fallback bool, keys ...string) bool {
	for _, key := range keys {
		if v, ok := scheduleRefineParamBool(params, key); ok {
			return v
		}
	}
	return fallback
}

// scheduleRefineReadString 读取字符串参数，缺失时返回默认值。
func scheduleRefineReadString(params map[string]any, key string, fallback string) string {
	raw, ok := params[key]
	if !ok {
		return fallback
	}
	text := strings.TrimSpace(fmt.Sprintf("%v", raw))
	if text == "" {
		return fallback
	}
	return text
}

// scheduleRefineNormalizeDayScope 规范化 day_scope 取值。
func scheduleRefineNormalizeDayScope(scope string) string {
	switch strings.ToLower(strings.TrimSpace(scope)) {
	case "weekend":
		return "weekend"
	case "workday":
		return "workday"
	default:
		return "all"
	}
}

// scheduleRefineNormalizeStatusFilter 规范化 status 过滤条件。
func scheduleRefineNormalizeStatusFilter(status string) string {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "existing":
		return "existing"
	case "all":
		return "all"
	default:
		return "suggested"
	}
}

// scheduleRefineMatchStatusFilter 判断条目状态是否命中 status 过滤。
func scheduleRefineMatchStatusFilter(entryStatus string, statusFilter string) bool {
	switch strings.ToLower(strings.TrimSpace(statusFilter)) {
	case "all":
		return true
	case "existing":
		return strings.TrimSpace(entryStatus) == "existing"
	default:
		return strings.TrimSpace(entryStatus) == "suggested"
	}
}

// scheduleRefineMatchDayScope 判断 day_of_week 是否满足 scope 过滤条件。
func scheduleRefineMatchDayScope(day int, scope string) bool {
	switch scope {
	case "weekend":
		return day == 6 || day == 7
	case "workday":
		return day >= 1 && day <= 5
	default:
		return day >= 1 && day <= 7
	}
}

// scheduleRefineIntSliceToDaySet 把 day 切片转换为 set，并去除非法 day 值。
func scheduleRefineIntSliceToDaySet(items []int) map[int]struct{} {
	if len(items) == 0 {
		return nil
	}
	set := make(map[int]struct{}, len(items))
	for _, item := range items {
		if item < 1 || item > 7 {
			continue
		}
		set[item] = struct{}{}
	}
	if len(set) == 0 {
		return nil
	}
	return set
}

// scheduleRefineIntSliceToWeekSet 把周次切片转换为 set，并去除非正数。
func scheduleRefineIntSliceToWeekSet(items []int) map[int]struct{} {
	if len(items) == 0 {
		return nil
	}
	set := make(map[int]struct{}, len(items))
	for _, item := range items {
		if item <= 0 {
			continue
		}
		set[item] = struct{}{}
	}
	if len(set) == 0 {
		return nil
	}
	return set
}

// scheduleRefineIntSliceToSectionSet 把节次切片转换为 set，并去除非法节次。
func scheduleRefineIntSliceToSectionSet(items []int) map[int]struct{} {
	if len(items) == 0 {
		return nil
	}
	set := make(map[int]struct{}, len(items))
	for _, item := range items {
		if item < 1 || item > 12 {
			continue
		}
		set[item] = struct{}{}
	}
	if len(set) == 0 {
		return nil
	}
	return set
}

// scheduleRefineIntSliceToIDSet 把正整数 ID 切片转换为 set。
func scheduleRefineIntSliceToIDSet(items []int) map[int]struct{} {
	if len(items) == 0 {
		return nil
	}
	set := make(map[int]struct{}, len(items))
	for _, item := range items {
		if item <= 0 {
			continue
		}
		set[item] = struct{}{}
	}
	if len(set) == 0 {
		return nil
	}
	return set
}

// scheduleRefineInferWeekBounds 推断查询周区间。
func scheduleRefineInferWeekBounds(entries []model.HybridScheduleEntry, window scheduleRefinePlanningWindow) (int, int) {
	if window.Enabled {
		return window.StartWeek, window.EndWeek
	}
	if len(entries) == 0 {
		return 1, 1
	}
	minWeek, maxWeek := entries[0].Week, entries[0].Week
	for _, entry := range entries {
		if entry.Week < minWeek {
			minWeek = entry.Week
		}
		if entry.Week > maxWeek {
			maxWeek = entry.Week
		}
	}
	return minWeek, maxWeek
}

// scheduleRefineBuildWeekIterList 构建周次迭代列表。
//
// 规则：
// 1. weekFilter 非空时，严格按过滤集合遍历；
// 2. weekFilter 为空时，按 weekFrom~weekTo 连续区间遍历；
// 3. 返回结果升序，便于日志与排查。
func scheduleRefineBuildWeekIterList(weekFilter map[int]struct{}, weekFrom, weekTo int) []int {
	if len(weekFilter) > 0 {
		return scheduleRefineKeysOfIntSet(weekFilter)
	}
	if weekFrom <= 0 || weekTo <= 0 || weekFrom > weekTo {
		return nil
	}
	out := make([]int, 0, weekTo-weekFrom+1)
	for w := weekFrom; w <= weekTo; w++ {
		out = append(out, w)
	}
	return out
}

// scheduleRefineReadIntSlice 读取 int 切片参数，兼容 []any / []int / 单个数值。
func scheduleRefineReadIntSlice(params map[string]any, keys ...string) []int {
	for _, key := range keys {
		raw, ok := params[key]
		if !ok {
			continue
		}
		switch v := raw.(type) {
		case []int:
			out := make([]int, len(v))
			copy(out, v)
			return out
		case []any:
			out := make([]int, 0, len(v))
			for _, item := range v {
				switch n := item.(type) {
				case int:
					out = append(out, n)
				case float64:
					out = append(out, int(n))
				case string:
					if parsed, err := strconv.Atoi(strings.TrimSpace(n)); err == nil {
						out = append(out, parsed)
					}
				}
			}
			return out
		default:
			if n, okNum := scheduleRefineParamInt(params, key); okNum {
				return []int{n}
			}
		}
	}
	return nil
}

// scheduleRefineReadStringSlice 读取 string 切片参数，兼容 []any / []string / 单个字符串。
func scheduleRefineReadStringSlice(params map[string]any, keys ...string) []string {
	for _, key := range keys {
		raw, ok := params[key]
		if !ok || raw == nil {
			continue
		}
		switch vv := raw.(type) {
		case []string:
			out := make([]string, 0, len(vv))
			for _, item := range vv {
				text := strings.TrimSpace(item)
				if text != "" {
					out = append(out, text)
				}
			}
			return out
		case []any:
			out := make([]string, 0, len(vv))
			for _, item := range vv {
				text := strings.TrimSpace(fmt.Sprintf("%v", item))
				if text != "" {
					out = append(out, text)
				}
			}
			return out
		case string:
			text := strings.TrimSpace(vv)
			if text != "" {
				return []string{text}
			}
		default:
			text := strings.TrimSpace(fmt.Sprintf("%v", vv))
			if text != "" {
				return []string{text}
			}
		}
	}
	return nil
}

// scheduleRefineIntersectsExcludedSections 判断候选区间是否与排除节次有交集。
func scheduleRefineIntersectsExcludedSections(from, to int, excluded map[int]struct{}) bool {
	if len(excluded) == 0 {
		return false
	}
	for sec := from; sec <= to; sec++ {
		if _, ok := excluded[sec]; ok {
			return true
		}
	}
	return false
}

// scheduleRefineKeysOfIntSet 返回 int set 的有序键。
func scheduleRefineKeysOfIntSet(set map[int]struct{}) []int {
	if len(set) == 0 {
		return nil
	}
	keys := make([]int, 0, len(set))
	for k := range set {
		keys = append(keys, k)
	}
	sort.Ints(keys)
	return keys
}

// scheduleRefineParseBatchMoveParams 解析 BatchMove 的 moves 参数。
//
// 步骤化说明：
// 1. 先读取 params["moves"]，必须存在且为非空数组；
// 2. 再把数组元素逐条转换成 map[string]any，便于复用 scheduleRefineToolMove；
// 3. 任一元素类型非法即整体失败，避免“部分可执行、部分不可执行”带来的语义歧义。
func scheduleRefineParseBatchMoveParams(params map[string]any) ([]map[string]any, error) {
	rawMoves, ok := params["moves"]
	if !ok {
		return nil, fmt.Errorf("参数缺失：BatchMove 需要 moves 数组")
	}

	var items []any
	switch v := rawMoves.(type) {
	case []any:
		items = v
	case []map[string]any:
		items = make([]any, 0, len(v))
		for _, item := range v {
			items = append(items, item)
		}
	default:
		return nil, fmt.Errorf("参数类型错误：BatchMove 的 moves 必须是数组")
	}
	if len(items) == 0 {
		return nil, fmt.Errorf("参数错误：BatchMove 的 moves 不能为空")
	}

	moveParamsList := make([]map[string]any, 0, len(items))
	for idx, item := range items {
		paramMap, ok := item.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("参数类型错误：BatchMove 第%d步不是对象", idx+1)
		}
		moveParamsList = append(moveParamsList, paramMap)
	}
	return moveParamsList, nil
}

// scheduleRefineClassifyBatchMoveErrorCode 把单步 Move 失败原因映射为 BatchMove 层错误码。
//
// 说明：
// 1. 映射保持与普通 Move 的错误语义一致，便于模型统一处理；
// 2. 这里按失败文案做轻量推断，避免引入跨文件循环依赖。
func scheduleRefineClassifyBatchMoveErrorCode(detail string) string {
	text := strings.TrimSpace(detail)
	switch {
	case strings.Contains(text, "顺序约束不满足"):
		return "ORDER_VIOLATION"
	case strings.Contains(text, "参数缺失"):
		return "PARAM_MISSING"
	case strings.Contains(text, "目标时段已被"):
		return "SLOT_CONFLICT"
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
	default:
		return "BATCH_MOVE_FAILED"
	}
}

// scheduleRefineSortHybridEntries 对混合条目做稳定排序，保证日志与预览输出稳定。
func scheduleRefineSortHybridEntries(entries []model.HybridScheduleEntry) {
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
		return left.Name < right.Name
	})
}

// scheduleRefineTruncate 截断日志内容，避免错误信息无上限增长。
func scheduleRefineTruncate(text string, maxLen int) string {
	if maxLen <= 0 {
		return ""
	}
	runes := []rune(text)
	if len(runes) <= maxLen {
		return text
	}
	return string(runes[:maxLen]) + "..."
}
