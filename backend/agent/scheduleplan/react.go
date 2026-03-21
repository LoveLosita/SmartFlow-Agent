package scheduleplan

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/LoveLosita/smartflow/backend/model"
	"github.com/cloudwego/eino-ext/components/model/ark"
	einoModel "github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
	arkModel "github.com/volcengine/volcengine-go-sdk/service/arkruntime/model"
)

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
	resp, err := chatModel.Generate(
		ctx,
		messages,
		ark.WithThinking(&arkModel.Thinking{Type: arkModel.ThinkingTypeEnabled}),
		einoModel.WithTemperature(0.2),
	)
	if err != nil {
		return "", err
	}
	if resp == nil {
		return "", fmt.Errorf("周级单步调用返回为空")
	}
	content := strings.TrimSpace(resp.Content)
	if content == "" {
		return "", fmt.Errorf("周级单步调用返回内容为空")
	}
	return content, nil
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

	prompt := SchedulePlanWeeklyReactPrompt
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
