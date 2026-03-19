package scheduleplan

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/LoveLosita/smartflow/backend/model"
	"github.com/cloudwego/eino-ext/components/model/ark"
	einoModel "github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
	arkModel "github.com/volcengine/volcengine-go-sdk/service/arkruntime/model"
)

// ── plan 节点模型输出结构 ──

type schedulePlanIntentOutput struct {
	Intent      string   `json:"intent"`
	Constraints []string `json:"constraints"`
	TaskClassID int      `json:"task_class_id"`
	Strategy    string   `json:"strategy"`
}

// ── materialize 节点模型输出结构 ──

type schedulePlanMaterializeOutput struct {
	Assignments       []materializeAssignment `json:"assignments"`
	UnassignedItemIDs []int                   `json:"unassigned_item_ids"`
}

type materializeAssignment struct {
	TaskItemID         int `json:"task_item_id"`
	Week               int `json:"week"`
	DayOfWeek          int `json:"day_of_week"`
	StartSection       int `json:"start_section"`
	EndSection         int `json:"end_section"`
	EmbedCourseEventID int `json:"embed_course_event_id"`
}

// ── reflect 节点模型输出结构 ──

type schedulePlanReflectOutput struct {
	Action             string                  `json:"action"`
	Reason             string                  `json:"reason"`
	PatchedAssignments []materializeAssignment `json:"patched_assignments"`
	RemoveItemIDs      []int                   `json:"remove_item_ids"`
}

// ══════════════════════════════════════════════════════════════
//  plan 节点
// ══════════════════════════════════════════════════════════════

// runPlanNode 负责"意图识别 + 约束提取"。
//
// 职责边界：
// 1) 从用户消息中提取排程意图、约束条件、策略；
// 2) task_class_id 优先从 Extra 字段获取，模型推断作为兜底；
// 3) 不负责调用粗排算法，只做意图分析。
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

	emitStage("schedule_plan.plan.analyzing", "正在分析你的排程需求...")

	// 1. 优先从 Extra 字段获取 task_class_id，避免依赖模型推断。
	if extra != nil {
		if tcID, ok := ExtraInt(extra, "task_class_id"); ok && tcID > 0 {
			st.TaskClassID = tcID
		}
	}

	// 2. 检查对话历史中是否包含上版排程方案（用于连续对话微调）。
	previousPlan := extractPreviousPlanFromHistory(chatHistory)
	if previousPlan != "" {
		st.PreviousPlanJSON = previousPlan
		st.IsAdjustment = true
	}

	// 3. 构造 prompt 让模型分析意图和约束。
	adjustmentHint := ""
	if st.IsAdjustment {
		adjustmentHint = "\n注意：这是对已有排程的微调请求。用户可能只想调整部分内容（如'早八不想学习'），请只提取变更部分的约束。"
	}

	prompt := fmt.Sprintf(`当前时间（北京时间）：%s
用户输入：%s%s

请分析用户的排程意图并提取约束条件。`,
		st.RequestNowText,
		strings.TrimSpace(userMessage),
		adjustmentHint,
	)

	// 3.1 模型调用失败时保守处理：只要有 task_class_id 就继续，否则报错。
	raw, callErr := callScheduleModelForJSON(ctx, chatModel, SchedulePlanIntentPrompt, prompt, 256)
	if callErr != nil {
		if st.TaskClassID > 0 {
			// 有 task_class_id 就可以继续，意图用兜底值。
			st.UserIntent = strings.TrimSpace(userMessage)
			emitStage("schedule_plan.plan.fallback", "意图分析失败，使用默认配置继续。")
			return st, nil
		}
		st.FinalSummary = "抱歉，我没能理解你的排程需求，请再描述一下或直接传入任务类 ID。"
		return st, nil
	}

	// 3.2 解析模型输出。
	parsed, parseErr := parseScheduleJSON[schedulePlanIntentOutput](raw)
	if parseErr != nil {
		if st.TaskClassID > 0 {
			st.UserIntent = strings.TrimSpace(userMessage)
			return st, nil
		}
		st.FinalSummary = "抱歉，我没能解析排程意图，请再试一次。"
		return st, nil
	}

	// 4. 回填状态。
	st.UserIntent = strings.TrimSpace(parsed.Intent)
	if st.UserIntent == "" {
		st.UserIntent = strings.TrimSpace(userMessage)
	}
	if len(parsed.Constraints) > 0 {
		st.Constraints = parsed.Constraints
	}
	if st.TaskClassID <= 0 && parsed.TaskClassID > 0 {
		st.TaskClassID = parsed.TaskClassID
	}
	if parsed.Strategy == "rapid" {
		st.Strategy = "rapid"
	}

	emitStage("schedule_plan.plan.done", fmt.Sprintf("已理解排程意图：%s", st.UserIntent))
	return st, nil
}

// ══════════════════════════════════════════════════════════════
//  preview 节点
// ══════════════════════════════════════════════════════════════

// runPreviewNode 负责调用粗排算法生成候选方案。
//
// 职责边界：
// 1) 调用 SmartPlanningRaw 服务，同时获取展示结构和已分配的任务项；
// 2) 展示结构供 SSE 阶段推送给前端预览；
// 3) 已分配的任务项供 materialize 节点直接转换为落库请求，无需模型介入。
func runPreviewNode(
	ctx context.Context,
	st *SchedulePlanState,
	deps SchedulePlanToolDeps,
	emitStage func(stage, detail string),
) (*SchedulePlanState, error) {
	if st == nil {
		return nil, errors.New("schedule plan graph: nil state in preview node")
	}

	// 1. 校验 task_class_id 必须有效。
	if st.TaskClassID <= 0 {
		st.FinalSummary = "缺少任务类 ID，无法生成排程方案。请在请求中传入 task_class_id。"
		return st, nil
	}

	emitStage("schedule_plan.preview.generating", "正在调用排程算法生成候选方案...")

	// 2. 调用粗排服务，同时拿到展示结构和已分配的任务项。
	displayPlans, allocatedItems, err := deps.SmartPlanningRaw(ctx, st.UserID, st.TaskClassID)
	if err != nil {
		st.FinalSummary = fmt.Sprintf("排程算法执行失败：%s。请检查任务类配置是否正确。", err.Error())
		return st, nil
	}

	if len(allocatedItems) == 0 {
		st.FinalSummary = "排程算法未找到可用时间槽，可能是课表已排满或任务类时间范围内无空闲。"
		return st, nil
	}

	st.CandidatePlans = displayPlans
	st.AllocatedItems = allocatedItems
	emitStage("schedule_plan.preview.done", fmt.Sprintf("已生成候选方案，共 %d 个任务项已分配。", len(allocatedItems)))
	return st, nil
}

// ══════════════════════════════════════════════════════════════
//  materialize 节点
// ══════════════════════════════════════════════════════════════

// runMaterializeNode 负责将粗排已分配的任务项转换为可落库结构。
//
// 职责边界：
// 1) 纯代码转换，不调用模型——粗排算法已完成分配，每个 item 的 EmbeddedTime 已回填；
// 2) 直接将 AllocatedItems 转为 BatchApplyPlans 可消费的 SingleTaskClassItem 数组；
// 3) 跳过 EmbeddedTime 为空的项（未成功分配的任务项），并在回复中说明。
func runMaterializeNode(
	ctx context.Context,
	st *SchedulePlanState,
	chatModel *ark.ChatModel,
	deps SchedulePlanToolDeps,
	emitStage func(stage, detail string),
) (*SchedulePlanState, error) {
	if st == nil {
		return nil, errors.New("schedule plan graph: nil state in materialize node")
	}
	if len(st.AllocatedItems) == 0 {
		// 无已分配项，preview 已设置了 FinalSummary，直接透传。
		return st, nil
	}

	emitStage("schedule_plan.materialize.converting", "正在将排程方案转换为可执行计划...")

	// 1. 将已分配的任务项直接转换为 BatchApplyPlans 请求结构。
	//    粗排算法已在 EmbeddedTime 中回填了 Week/DayOfWeek/SectionFrom/SectionTo，
	//    这里只做格式映射，不做二次分配。
	items := make([]model.SingleTaskClassItem, 0, len(st.AllocatedItems))
	skippedCount := 0
	for _, allocated := range st.AllocatedItems {
		if allocated.EmbeddedTime == nil {
			// EmbeddedTime 为空说明粗排未能为该项找到可用槽位，跳过。
			skippedCount++
			continue
		}
		items = append(items, model.SingleTaskClassItem{
			TaskItemID:         allocated.ID,
			Week:               allocated.EmbeddedTime.Week,
			DayOfWeek:          allocated.EmbeddedTime.DayOfWeek,
			StartSection:       allocated.EmbeddedTime.SectionFrom,
			EndSection:         allocated.EmbeddedTime.SectionTo,
			EmbedCourseEventID: 0, // 阶段 1 暂不支持嵌入水课，后续可扩展
		})
	}

	if len(items) == 0 {
		st.FinalSummary = "所有任务项均未能分配到可用时间槽，请检查课表或调整时间范围。"
		return st, nil
	}

	st.ApplyRequest = &model.UserInsertTaskClassItemToScheduleRequestBatch{
		TaskClassID: st.TaskClassID,
		Items:       items,
	}

	detail := fmt.Sprintf("已生成 %d 项排程安排。", len(items))
	if skippedCount > 0 {
		detail += fmt.Sprintf("（%d 项因槽位不足未能安排）", skippedCount)
	}
	emitStage("schedule_plan.materialize.done", detail)
	return st, nil
}

// ══════════════════════════════════════════════════════════════
//  apply 节点
// ══════════════════════════════════════════════════════════════

// runApplyNode 负责将排程方案落库。
//
// 职责边界：
// 1) 调用 BatchApplyPlans 服务执行写库；
// 2) 成功时标记 Applied=true；
// 3) 失败时记录错误信息，由分支逻辑决定是否进入 reflect 重试。
func runApplyNode(
	ctx context.Context,
	st *SchedulePlanState,
	deps SchedulePlanToolDeps,
	emitStage func(stage, detail string),
) (*SchedulePlanState, error) {
	if st == nil {
		return nil, errors.New("schedule plan graph: nil state in apply node")
	}
	if st.ApplyRequest == nil || len(st.ApplyRequest.Items) == 0 {
		return st, nil
	}

	emitStage("schedule_plan.apply.persisting", "正在检查冲突并落库排程方案...")

	err := deps.BatchApplyPlans(ctx, st.TaskClassID, st.UserID, st.ApplyRequest)
	if err != nil {
		st.RecordApplyError(err.Error())
		if st.CanRetry() {
			emitStage("schedule_plan.apply.conflict", fmt.Sprintf("落库失败（第%d次），准备调整方案...", st.RetryCount))
		} else {
			emitStage("schedule_plan.apply.failed", "多次尝试后仍无法落库，请手动调整。")
		}
		return st, nil
	}

	st.Applied = true
	st.ApplyError = ""
	emitStage("schedule_plan.apply.done", "排程方案已成功落库！")
	return st, nil
}

// ══════════════════════════════════════════════════════════════
//  reflect 节点
// ══════════════════════════════════════════════════════════════

// runReflectNode 负责分析落库失败原因并生成修补方案。
//
// 职责边界：
// 1) 把后端错误信息喂给模型，让模型决定修补策略；
// 2) retry_with_patch：重新构建 ApplyRequest 并回到 apply；
// 3) partial_apply：移除冲突项后重新构建 ApplyRequest；
// 4) give_up：设置 FinalSummary 并退出。
func runReflectNode(
	ctx context.Context,
	st *SchedulePlanState,
	chatModel *ark.ChatModel,
	emitStage func(stage, detail string),
) (*SchedulePlanState, error) {
	if st == nil {
		return nil, errors.New("schedule plan graph: nil state in reflect node")
	}

	emitStage("schedule_plan.reflect.analyzing", "正在分析失败原因并调整方案...")

	// 1. 构造 prompt，包含错误信息和当前方案。
	currentPlanJSON, _ := json.Marshal(st.ApplyRequest)
	prompt := fmt.Sprintf(`排程落库失败，错误信息：%s

当前排程方案（%d 个任务项）：
%s

请分析失败原因并给出修补方案。`,
		st.ApplyError,
		len(st.ApplyRequest.Items),
		string(currentPlanJSON),
	)

	raw, callErr := callScheduleModelForJSON(ctx, chatModel, SchedulePlanReflectPrompt, prompt, 1024)
	if callErr != nil {
		// 模型调用失败，直接放弃。
		st.ReflectAction = "give_up"
		st.FinalSummary = fmt.Sprintf("排程落库失败且无法自动修补：%s。请手动调整排程。", st.ApplyError)
		return st, nil
	}

	parsed, parseErr := parseScheduleJSON[schedulePlanReflectOutput](raw)
	if parseErr != nil {
		st.ReflectAction = "give_up"
		st.FinalSummary = fmt.Sprintf("排程落库失败：%s。请手动调整。", st.ApplyError)
		return st, nil
	}

	st.ReflectAction = strings.TrimSpace(parsed.Action)

	switch st.ReflectAction {
	case "retry_with_patch":
		// 2. 用模型给出的修补方案替换当前请求。
		if len(parsed.PatchedAssignments) > 0 {
			items := make([]model.SingleTaskClassItem, 0, len(parsed.PatchedAssignments))
			for _, a := range parsed.PatchedAssignments {
				items = append(items, model.SingleTaskClassItem{
					TaskItemID:         a.TaskItemID,
					Week:               a.Week,
					DayOfWeek:          a.DayOfWeek,
					StartSection:       a.StartSection,
					EndSection:         a.EndSection,
					EmbedCourseEventID: a.EmbedCourseEventID,
				})
			}
			st.ApplyRequest.Items = items
		}
		emitStage("schedule_plan.reflect.patched", "已调整方案，准备重新落库。")

	case "partial_apply":
		// 3. 移除冲突项后重试。
		if len(parsed.RemoveItemIDs) > 0 {
			removeSet := make(map[int]bool)
			for _, id := range parsed.RemoveItemIDs {
				removeSet[id] = true
			}
			filtered := make([]model.SingleTaskClassItem, 0)
			for _, item := range st.ApplyRequest.Items {
				if !removeSet[item.TaskItemID] {
					filtered = append(filtered, item)
				}
			}
			st.ApplyRequest.Items = filtered
		}
		if len(st.ApplyRequest.Items) == 0 {
			st.ReflectAction = "give_up"
			st.FinalSummary = "移除冲突项后没有剩余可安排的任务，请检查课表或调整时间范围。"
			return st, nil
		}
		emitStage("schedule_plan.reflect.partial", fmt.Sprintf("已移除冲突项，剩余 %d 项准备落库。", len(st.ApplyRequest.Items)))

	default:
		// 4. give_up 或未知动作。
		reason := strings.TrimSpace(parsed.Reason)
		if reason == "" {
			reason = st.ApplyError
		}
		st.FinalSummary = fmt.Sprintf("排程无法自动完成：%s。建议手动调整。", reason)
	}

	return st, nil
}

// ══════════════════════════════════════════════════════════════
//  finalize 节点
// ══════════════════════════════════════════════════════════════

// runFinalizeNode 负责生成最终回复文案。
//
// 职责边界：
// 1) 落库成功时调用模型生成友好摘要；
// 2) 落库失败时透传已有的 FinalSummary；
// 3) 将上版方案信息嵌入回复，支持前端在连续对话中回传。
func runFinalizeNode(
	ctx context.Context,
	st *SchedulePlanState,
	chatModel *ark.ChatModel,
	emitStage func(stage, detail string),
) (*SchedulePlanState, error) {
	if st == nil {
		return nil, errors.New("schedule plan graph: nil state in finalize node")
	}

	// 1. 如果已有 FinalSummary（失败场景），直接使用。
	if strings.TrimSpace(st.FinalSummary) != "" {
		return st, nil
	}

	// 2. 落库未成功时给兜底文案。
	if !st.Applied {
		st.FinalSummary = "本次排程未能成功落库，请检查任务类配置后重试。"
		return st, nil
	}

	emitStage("schedule_plan.finalize.summarizing", "正在生成排程结果摘要...")

	// 3. 调用模型生成友好摘要。
	planJSON, _ := json.Marshal(st.ApplyRequest)
	constraintsText := "无"
	if len(st.Constraints) > 0 {
		constraintsText = strings.Join(st.Constraints, "、")
	}

	prompt := fmt.Sprintf(`排程结果：
- 成功安排 %d 个任务项
- 排程方案：%s
- 用户约束：%s
- 排程意图：%s

请生成结果摘要。`,
		len(st.ApplyRequest.Items),
		string(planJSON),
		constraintsText,
		st.UserIntent,
	)

	raw, callErr := callScheduleModelForJSON(ctx, chatModel, SchedulePlanFinalizePrompt, prompt, 256)
	if callErr != nil {
		// 模型生成摘要失败，使用固定文案。
		st.FinalSummary = fmt.Sprintf("排程完成！已成功安排 %d 个任务项。", len(st.ApplyRequest.Items))
	} else {
		summary := strings.TrimSpace(raw)
		// 移除可能的 JSON 包裹或 markdown。
		summary = strings.Trim(summary, "\"'`")
		if summary == "" {
			summary = fmt.Sprintf("排程完成！已成功安排 %d 个任务项。", len(st.ApplyRequest.Items))
		}
		st.FinalSummary = summary
	}

	st.Completed = true
	emitStage("schedule_plan.finalize.done", "排程完成！")
	return st, nil
}

// ══════════════════════════════════════════════════════════════
//  分支决策函数
// ══════════════════════════════════════════════════════════════

// selectNextAfterPlan 根据 plan 节点结果决定下一步。
//
// 分支规则：
// 1) FinalSummary 非空 -> exit（plan 阶段已确定无法继续）
// 2) TaskClassID 无效 -> exit
// 3) 其余 -> preview
func selectNextAfterPlan(st *SchedulePlanState) string {
	if st == nil {
		return schedulePlanGraphNodeExit
	}
	if strings.TrimSpace(st.FinalSummary) != "" {
		return schedulePlanGraphNodeExit
	}
	if st.TaskClassID <= 0 {
		return schedulePlanGraphNodeExit
	}
	return schedulePlanGraphNodePreview
}

// selectNextAfterApply 根据 apply 节点结果决定下一步。
//
// 分支规则：
// 1) Applied=true -> finalize（成功落库）
// 2) CanRetry=true -> reflect（尝试修补）
// 3) CanRetry=false -> finalize（重试耗尽，由 finalize 输出失败文案）
func selectNextAfterApply(st *SchedulePlanState) string {
	if st == nil {
		return schedulePlanGraphNodeFinalize
	}
	if st.Applied {
		return schedulePlanGraphNodeFinalize
	}
	if st.CanRetry() {
		return schedulePlanGraphNodeReflect
	}
	// 重试耗尽，设置失败文案后进入 finalize。
	if strings.TrimSpace(st.FinalSummary) == "" {
		st.FinalSummary = fmt.Sprintf("排程落库多次失败：%s。请手动调整。", st.ApplyError)
	}
	return schedulePlanGraphNodeFinalize
}

// selectNextAfterReflect 根据 reflect 节点结果决定下一步。
//
// 分支规则：
// 1) give_up -> finalize
// 2) retry_with_patch / partial_apply -> apply（重新落库）
func selectNextAfterReflect(st *SchedulePlanState) string {
	if st == nil {
		return schedulePlanGraphNodeFinalize
	}
	if st.ReflectAction == "give_up" {
		return schedulePlanGraphNodeFinalize
	}
	if st.ApplyRequest == nil || len(st.ApplyRequest.Items) == 0 {
		return schedulePlanGraphNodeFinalize
	}
	return schedulePlanGraphNodeApply
}

// ══════════════════════════════════════════════════════════════
//  工具函数
// ══════════════════════════════════════════════════════════════

// callScheduleModelForJSON 调用模型并期望返回 JSON 结果。
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
// 兼容 ```json ... ``` 包裹和额外文本。
func parseScheduleJSON[T any](raw string) (*T, error) {
	clean := strings.TrimSpace(raw)
	if clean == "" {
		return nil, errors.New("empty response")
	}

	// 兼容 ```json ... ``` 包裹。
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

	// 提取最外层 JSON 对象。
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

// buildTaskItemsInfo 将任务项列表格式化为模型可读的文本。
func buildTaskItemsInfo(items []model.TaskClassItem) string {
	if len(items) == 0 {
		return "无任务项"
	}
	var sb strings.Builder
	for i, item := range items {
		content := "未命名"
		if item.Content != nil && strings.TrimSpace(*item.Content) != "" {
			content = strings.TrimSpace(*item.Content)
		}
		order := i + 1
		if item.Order != nil {
			order = *item.Order
		}
		sb.WriteString(fmt.Sprintf("- ID=%d, 序号=%d, 内容=%s\n", item.ID, order, content))
	}
	return sb.String()
}

// extractPreviousPlanFromHistory 从对话历史中提取上版排程方案。
//
// 策略：
// 在助手消息中查找包含"排程完成"标记的最近一条，提取其中的方案信息。
// 当前版本使用简单的文本匹配，后续可升级为结构化存储。
func extractPreviousPlanFromHistory(history []*schema.Message) string {
	if len(history) == 0 {
		return ""
	}
	// 从后往前遍历，找最近的排程成功消息。
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

// ══════════════════════════════════════════════════════════════
//  hybridBuild 节点
// ══════════════════════════════════════════════════════════════

// runHybridBuildNode 负责构建"混合日程"：将既有日程与粗排建议合并。
//
// 职责边界：
// 1) 调用 HybridScheduleWithPlan 服务方法；
// 2) 将结果写入 State.HybridEntries，供 ReAct 精排节点操作；
// 3) 同时保留 AllocatedItems，供后续可能的落库使用。
func runHybridBuildNode(
	ctx context.Context,
	st *SchedulePlanState,
	deps SchedulePlanToolDeps,
	emitStage func(stage, detail string),
) (*SchedulePlanState, error) {
	if st == nil {
		return nil, errors.New("schedule plan graph: nil state in hybridBuild node")
	}
	if deps.HybridScheduleWithPlan == nil {
		return nil, errors.New("schedule plan graph: HybridScheduleWithPlan dependency not injected")
	}
	if st.TaskClassID <= 0 {
		st.FinalSummary = "缺少任务类 ID，无法构建混合日程。"
		return st, nil
	}

	emitStage("schedule_plan.hybrid.building", "正在构建混合日程...")

	entries, allocatedItems, err := deps.HybridScheduleWithPlan(ctx, st.UserID, st.TaskClassID)
	if err != nil {
		st.FinalSummary = fmt.Sprintf("构建混合日程失败：%s", err.Error())
		return st, nil
	}
	if len(entries) == 0 {
		st.FinalSummary = "混合日程为空，无可优化内容。"
		return st, nil
	}

	st.HybridEntries = entries
	st.AllocatedItems = allocatedItems

	suggestedCount := 0
	for _, e := range entries {
		if e.Status == "suggested" {
			suggestedCount++
		}
	}
	emitStage("schedule_plan.hybrid.done", fmt.Sprintf("混合日程已构建，共 %d 个条目（%d 个可优化）。", len(entries), suggestedCount))
	return st, nil
}

// ══════════════════════════════════════════════════════════════
//  returnPreview 节点
// ══════════════════════════════════════════════════════════════

// runReturnPreviewNode 负责将 ReAct 优化后的混合日程转为前端预览格式。
//
// 职责边界：
// 1) 从 HybridEntries 中提取最终排程结果；
// 2) 转换为 []UserWeekSchedule 格式（复用 sectionTimeMap）；
// 3) 设置 FinalSummary 为 ReAct 的优化摘要；
// 4) 不落库——用户需确认后再走落库链路。
func runReturnPreviewNode(
	ctx context.Context,
	st *SchedulePlanState,
	emitStage func(stage, detail string),
) (*SchedulePlanState, error) {
	if st == nil {
		return nil, errors.New("schedule plan graph: nil state in returnPreview node")
	}

	emitStage("schedule_plan.preview_return.building", "正在生成优化后的排程预览...")

	// 1. 将 HybridEntries 中 suggested 的任务回写到 AllocatedItems 的 EmbeddedTime。
	//    这样后续如果用户确认，可以直接走 materialize → apply 落库。
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

	// 2. 将 HybridEntries 转为 CandidatePlans（[]UserWeekSchedule）供前端展示。
	st.CandidatePlans = hybridEntriesToWeekSchedules(st.HybridEntries)

	// 3. 设置最终摘要。
	if strings.TrimSpace(st.ReactSummary) != "" {
		st.FinalSummary = st.ReactSummary
	} else {
		st.FinalSummary = fmt.Sprintf("排程优化完成，共 %d 个任务已安排。请确认后落库。", len(suggestedMap))
	}
	st.Completed = true

	emitStage("schedule_plan.preview_return.done", "排程预览已生成，等待确认。")
	return st, nil
}

// hybridEntriesToWeekSchedules 将混合日程条目转为前端展示格式。
func hybridEntriesToWeekSchedules(entries []model.HybridScheduleEntry) []model.UserWeekSchedule {
	// sectionTimeMap 与 conv/schedule.go 保持一致。
	sectionTimeMap := map[int][2]string{
		1: {"08:00", "08:45"}, 2: {"08:55", "09:40"},
		3: {"10:15", "11:00"}, 4: {"11:10", "11:55"},
		5: {"14:00", "14:45"}, 6: {"14:55", "15:40"},
		7: {"16:15", "17:00"}, 8: {"17:10", "17:55"},
		9: {"19:00", "19:45"}, 10: {"19:55", "20:40"},
		11: {"20:50", "21:35"}, 12: {"21:45", "22:30"},
	}

	// 按周分组
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

	// 排序输出
	result := make([]model.UserWeekSchedule, 0, len(weekMap))
	for w, events := range weekMap {
		result = append(result, model.UserWeekSchedule{Week: w, Events: events})
	}
	// 按周次排序
	for i := 0; i < len(result); i++ {
		for j := i + 1; j < len(result); j++ {
			if result[j].Week < result[i].Week {
				result[i], result[j] = result[j], result[i]
			}
		}
	}
	return result
}
