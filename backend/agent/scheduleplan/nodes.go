package scheduleplan

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/LoveLosita/smartflow/backend/model"
	"github.com/cloudwego/eino-ext/components/model/ark"
	einoModel "github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
	arkModel "github.com/volcengine/volcengine-go-sdk/service/arkruntime/model"
)

// schedulePlanIntentOutput 是 plan 节点要求模型返回的结构化结果。
//
// 兼容说明：
//  1. 新主语义是 task_class_ids（数组）；
//  2. 为兼容旧 prompt/旧缓存输出，保留 task_class_id（单值）兜底解析；
//  3. TaskTags 的 key 兼容两种写法：
//     3.1 推荐：task_item_id（例如 "12"）；
//     3.2 兼容：任务名称（例如 "高数复习"）。
type schedulePlanIntentOutput struct {
	Intent       string            `json:"intent"`
	Constraints  []string          `json:"constraints"`
	TaskClassIDs []int             `json:"task_class_ids"`
	TaskClassID  int               `json:"task_class_id"`
	Strategy     string            `json:"strategy"`
	TaskTags     map[string]string `json:"task_tags"`
}

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
	}
	previousPlan := extractPreviousPlanFromHistory(chatHistory)
	if previousPlan != "" {
		st.PreviousPlanJSON = previousPlan
		st.IsAdjustment = true
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
	raw, callErr := callScheduleModelForJSON(ctx, chatModel, SchedulePlanIntentPrompt, prompt, 256)
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
		fmt.Sprintf("已识别排程意图，任务类数量=%d。", len(st.TaskClassIDs)),
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
		len(st.PreviousHybridEntries) > 0 &&
		sameTaskClassSet(taskClassIDs, st.PreviousTaskClassIDs)
	if canReusePreviousPlan {
		emitStage("schedule_plan.rough_build.reuse_previous", "检测到连续对话微调，复用上一版排程作为优化起点。")
		st.HybridEntries = deepCopyEntries(st.PreviousHybridEntries)
		st.CandidatePlans = hybridEntriesToWeekSchedules(st.HybridEntries)
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
	if chatModel == nil {
		return "", errors.New("schedule plan: model is nil")
	}

	messages := []*schema.Message{
		schema.SystemMessage(systemPrompt),
		schema.UserMessage(userPrompt),
	}
	opts := []einoModel.Option{
		ark.WithThinking(&arkModel.Thinking{Type: arkModel.ThinkingTypeDisabled}),
		einoModel.WithTemperature(0),
	}
	if maxTokens > 0 {
		opts = append(opts, einoModel.WithMaxTokens(maxTokens))
	}

	resp, err := chatModel.Generate(ctx, messages, opts...)
	if err != nil {
		return "", err
	}
	if resp == nil {
		return "", errors.New("模型返回为空")
	}
	content := strings.TrimSpace(resp.Content)
	if content == "" {
		return "", errors.New("模型返回内容为空")
	}
	return content, nil
}

// parseScheduleJSON 解析模型返回的 JSON 内容。
//
// 兼容策略：
// 1. 兼容 ```json ... ``` 包裹；
// 2. 兼容模型在 JSON 前后带解释文本（提取最外层对象）。
func parseScheduleJSON[T any](raw string) (*T, error) {
	clean := strings.TrimSpace(raw)
	if clean == "" {
		return nil, errors.New("empty response")
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

	start := strings.Index(clean, "{")
	end := strings.LastIndex(clean, "}")
	if start == -1 || end == -1 || end <= start {
		return nil, fmt.Errorf("no json object found in: %s", clean)
	}
	obj := clean[start : end+1]
	if err := json.Unmarshal([]byte(obj), &out); err != nil {
		return nil, err
	}
	return &out, nil
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
	if len(src) == 0 {
		return nil
	}
	dst := make([]model.TaskClassItem, 0, len(src))
	for _, item := range src {
		copied := item
		if item.CategoryID != nil {
			v := *item.CategoryID
			copied.CategoryID = &v
		}
		if item.Order != nil {
			v := *item.Order
			copied.Order = &v
		}
		if item.Content != nil {
			v := *item.Content
			copied.Content = &v
		}
		if item.Status != nil {
			v := *item.Status
			copied.Status = &v
		}
		if item.EmbeddedTime != nil {
			t := *item.EmbeddedTime
			copied.EmbeddedTime = &t
		}
		dst = append(dst, copied)
	}
	return dst
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
