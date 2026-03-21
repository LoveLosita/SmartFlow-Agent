package scheduleplan

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/LoveLosita/smartflow/backend/model"
	"github.com/cloudwego/eino-ext/components/model/ark"
	"github.com/cloudwego/eino/schema"
	arkModel "github.com/volcengine/volcengine-go-sdk/service/arkruntime/model"
)

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
		schema.SystemMessage(SchedulePlanDailyReactPrompt),
		schema.UserMessage(fmt.Sprintf(
			"以下是今天的日程（JSON）：\n%s\n\n仅优化这一天的数据，不要跨天移动。",
			string(hybridJSON),
		)),
	}

	for round := 0; round < maxRounds; round++ {
		roundCtx, cancel := context.WithTimeout(ctx, dailyReactRoundTimeout)
		resp, generateErr := chatModel.Generate(
			roundCtx,
			messages,
			// 1. 日内优化只做“单天局部微调”，任务边界清晰，默认关闭 thinking 以降低时延。
			// 2. 周级全局配平仍保留 thinking（在 weekly_refine），这里不承担跨天复杂推理职责。
			// 3. 若后续观测到质量回退，可只在 suggested 很多时按条件重开 thinking，而不是全量开启。
			ark.WithThinking(&arkModel.Thinking{Type: arkModel.ThinkingTypeDisabled}),
		)
		cancel()
		if generateErr != nil {
			return entries, fmt.Errorf("日内 ReAct 第%d轮失败: %w", round+1, generateErr)
		}
		if resp == nil {
			return entries, fmt.Errorf("日内 ReAct 第%d轮返回为空", round+1)
		}

		content := strings.TrimSpace(resp.Content)
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
