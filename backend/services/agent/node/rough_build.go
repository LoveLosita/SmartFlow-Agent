package agentnode

import (
	"context"
	"fmt"
	"log"
	"strconv"
	"strings"

	agentmodel "github.com/LoveLosita/smartflow/backend/services/agent/model"
	agenttools "github.com/LoveLosita/smartflow/backend/services/agent/tools"
	"github.com/LoveLosita/smartflow/backend/services/agent/tools/schedule"
)

const (
	roughBuildStageName   = "rough_build"
	roughBuildStatusBlock = "rough_build.status"
	roughBuildSampleLimit = 3
)

type roughBuildApplyStats struct {
	AppliedCount             int
	DayMappingMissCount      int
	TaskItemMatchMissCount   int
	DayMappingMissSamples    []string
	TaskItemMatchMissSamples []string
}

// RunRoughBuildNode 执行粗排节点逻辑。
//
// 步骤说明：
//  1. 推送"正在粗排"状态给前端；
//  2. 从 CommonState 读取 TaskClassIDs，确认有需要排课的任务类；
//  3. 加载 ScheduleState（含 DayMapping）；
//  4. 调用 RoughBuildFunc 拿到粗排结果（[]RoughBuildPlacement）；
//  5. 把粗排结果写入 ScheduleState，把已落位任务标记为 suggested；
//  6. 若粗排后仍存在真实 pending，则写入正式 abort 结果并结束本轮；
//  7. 否则按“是否需要粗排后立即微调”分流：
//     - 无明确微调诉求：直接 Done -> Deliver；
//     - 有明确微调诉求：进入 Execute。
func RunRoughBuildNode(ctx context.Context, st *agentmodel.AgentGraphState) error {
	if st == nil {
		return fmt.Errorf("rough build node: state is nil")
	}

	flowState := st.EnsureFlowState()
	emitter := st.EnsureChunkEmitter()

	// 1. 推送状态：告知前端进入粗排环节。
	_ = emitter.EmitStatus(
		roughBuildStatusBlock,
		roughBuildStageName,
		"rough_building",
		"正在为你生成初始排课方案，请稍候。",
		true,
	)

	// 2. 校验依赖。
	if st.Deps.RoughBuildFunc == nil {
		return fmt.Errorf("rough build node: RoughBuildFunc 未注入")
	}

	// 3. 读取任务类 IDs。
	taskClassIDs := flowState.TaskClassIDs
	if len(taskClassIDs) == 0 {
		// 没有任务类 ID 时静默跳过粗排，直接进入执行阶段。
		flowState.Phase = agentmodel.PhaseExecuting
		flowState.NeedsRoughBuild = false
		flowState.NeedsRefineAfterRoughBuild = false
		return nil
	}

	// 4. 粗排前强制刷新 ScheduleState，避免复用旧快照窗口。
	// 4.1 设计意图：当用户做“超前规划”时，窗口必须跟随本轮 task_class_ids，而不是沿用历史“当前周”窗口。
	// 4.2 做法：主动丢弃内存中的旧 state，让 EnsureScheduleState 走 provider 重新加载。
	// 4.3 失败策略：若任务类缺少有效起止日期，provider 会返回错误，由上层统一透传并让用户补齐字段。
	st.ScheduleState = nil
	st.OriginalScheduleState = nil

	// 5. 加载 ScheduleState（含 DayMapping，用于坐标转换）。
	scheduleState, err := st.EnsureScheduleState(ctx)
	if err != nil {
		// 1. 当任务类时间窗缺失时，按“可恢复失败”收口：提示用户先补齐起止日期，再重试粗排。
		// 2. 不把这类输入缺失上抛为系统错误，避免整条链路直接 fallback 到普通聊天。
		if strings.Contains(err.Error(), "任务类缺少有效时间窗") {
			failureMessage := "开始智能编排前，我需要任务类的起止日期（start_date / end_date）。请先补齐时间窗，再让我继续排课。"
			_ = emitter.EmitStatus(
				roughBuildStatusBlock,
				roughBuildStageName,
				"rough_build_need_time_window",
				failureMessage,
				true,
			)
			flowState.NeedsRoughBuild = false
			flowState.Abort(
				roughBuildStageName,
				"rough_build_window_missing",
				failureMessage,
				err.Error(),
			)
			return nil
		}
		return fmt.Errorf("rough build node: 加载日程状态失败: %w", err)
	}
	if scheduleState == nil {
		return fmt.Errorf("rough build node: ScheduleState 为空，无法执行粗排")
	}

	// 6. 调用粗排算法。
	placements, err := st.Deps.RoughBuildFunc(ctx, flowState.UserID, taskClassIDs)
	if err != nil {
		return fmt.Errorf("rough build node: 粗排算法失败: %w", err)
	}

	// 7. 把粗排结果写入 ScheduleState。
	applyStats := applyRoughBuildPlacements(scheduleState, placements)

	// 7.1 标记本轮产生过日程变更，供 deliver 节点判断是否推送“排程完毕”卡片。
	if applyStats.AppliedCount > 0 {
		flowState.HasScheduleChanges = true
	}

	// 8. 先校验粗排后是否仍有真实 pending。
	stillPending := countPendingTasks(scheduleState, taskClassIDs)
	log.Printf(
		"[DEBUG] rough_build scope_task_classes=%v placements=%d applied=%d day_mapping_miss=%d task_item_match_miss=%d pending_in_scope=%d total_tasks=%d window_days=%d",
		taskClassIDs,
		len(placements),
		applyStats.AppliedCount,
		applyStats.DayMappingMissCount,
		applyStats.TaskItemMatchMissCount,
		stillPending,
		len(scheduleState.Tasks),
		len(scheduleState.Window.DayMapping),
	)
	if applyStats.DayMappingMissCount > 0 {
		log.Printf(
			"[DEBUG] rough_build day_mapping_miss_samples=%v window=%s",
			applyStats.DayMappingMissSamples,
			summarizeRoughBuildWindow(scheduleState),
		)
	}
	if applyStats.TaskItemMatchMissCount > 0 {
		log.Printf(
			"[DEBUG] rough_build task_item_match_miss_samples=%v scoped_task_samples=%v",
			applyStats.TaskItemMatchMissSamples,
			collectScopedTaskSamples(scheduleState, taskClassIDs),
		)
	}
	if stillPending > 0 {
		failureMessage := fmt.Sprintf(
			"初始排课方案构建异常：粗排后仍有 %d 个任务未获得初始落位。按当前规则，本轮不进入微调，请检查粗排算法或任务数据。",
			stillPending,
		)
		_ = emitter.EmitStatus(
			roughBuildStatusBlock,
			roughBuildStageName,
			"rough_build_failed",
			failureMessage,
			true,
		)
		flowState.NeedsRoughBuild = false
		flowState.Abort(
			roughBuildStageName,
			"rough_build_pending_remaining",
			failureMessage,
			fmt.Sprintf("rough build finished with %d real pending tasks remaining", stillPending),
		)
		return nil
	}

	// 8. 计算是否需要“粗排后立即微调”。
	//
	// 1. 只在“无计划直执行”链路下应用该止血分流；
	// 2. 有计划链路依旧进入 execute，避免改变既有 plan->execute 语义；
	// 3. chat 路由明确标记 needs_refine_after_rough_build=true 时才进微调。
	shouldRefineAfterRoughBuild := flowState.HasPlan() || flowState.NeedsRefineAfterRoughBuild

	// 9. 推送完成状态（区分“继续微调”与“直接收口”两种路径）。
	doneStatus := "rough_build_done"
	doneMessage := fmt.Sprintf("初始排课方案已生成，共 %d 个任务已预排，进入微调阶段。", len(placements))
	if !shouldRefineAfterRoughBuild {
		doneStatus = "rough_build_done_no_refine"
		doneMessage = fmt.Sprintf("初始排课方案已生成，共 %d 个任务已预排。本轮按默认策略先结束；如需优化，请继续告诉我你的偏好。", len(placements))
	}
	_ = emitter.EmitStatus(
		roughBuildStatusBlock,
		roughBuildStageName,
		doneStatus,
		doneMessage,
		false,
	)

	// 10. 把粗排完成信息写入 pinned context，让后续节点能拿到一致事实。

	// 构造任务类 ID 字符串，供 pinned block 明确标注，避免 Execute LLM 因找不到 task_class_id 来源而 ask_user。
	idParts := make([]string, len(taskClassIDs))
	for i, id := range taskClassIDs {
		idParts[i] = strconv.Itoa(id)
	}
	idStr := strings.Join(idParts, ", ")

	pinnedContent := fmt.Sprintf(
		"后端已自动运行粗排算法（任务类 ID：[%s]），初始排课方案已写入日程状态（共 %d 个任务已预排）。\n"+
			"这些预排任务已标记为 suggested，表示“可继续优化的建议落位”，不是待补排任务。\n"+
			"本轮不需要再调用 place，也无需再次触发粗排。",
		idStr, len(placements),
	)
	if shouldRefineAfterRoughBuild {
		pinnedContent += "\n请先调用 get_overview 查看整体分布，再使用 move / swap / unplace 微调不合理的位置。"
	} else {
		pinnedContent += "\n当前未收到明确微调偏好，流程将先收口；如需进一步优化，请基于本次结果提出调整要求。"
	}
	st.EnsureConversationContext().UpsertPinnedBlock(agentmodel.ContextBlock{
		Key:     "rough_build_done",
		Title:   "粗排已完成",
		Content: pinnedContent,
	})

	// 11. 清除粗排标记，并按分流结果进入执行或直接收口。
	//
	// 1. 无明确微调诉求：直接标记 completed，graph 会路由到 deliver；
	// 2. 有明确微调诉求：进入 execute 节点继续工具微调；
	// 3. 无论哪条路径，都要重置粗排相关标记，避免污染后续轮次。
	flowState.NeedsRoughBuild = false
	flowState.NeedsRefineAfterRoughBuild = false
	if !shouldRefineAfterRoughBuild {
		flowState.ActiveOptimizeOnly = false
		flowState.Done()
		return nil
	}
	if strings.TrimSpace(flowState.OptimizationMode) == "" {
		flowState.OptimizationMode = "first_full"
	}
	// 1. 仅“粗排后自动进入微调”的链路打开主动优化专用模式。
	// 2. 该模式会把 execute 裁成 analyze_health + move + swap 的最小工具面，
	//    迫使 LLM 基于候选做选择，而不是重新全窗乱搜。
	// 3. 用户后续重开新请求时，会在 CommonState 的重置入口统一清掉这个标记。
	flowState.ActiveOptimizeOnly = true
	// 12. 粗排后进入 execute 微调时，补一条一次性 context hook。
	//
	// 1. 目的：即使这条链路不回 plan，也能在 execute 首轮拿到建议工具面（analyze + mutation）。
	// 2. 边界：这里只写“建议激活域/包”，不直接执行 context_tools_add，仍由 execute 按统一入口消费。
	// 3. 回退：hook 无效时 execute 会自动忽略并清空，不影响主流程。
	flowState.PendingContextHook = &agentmodel.ContextHook{
		Domain: agenttools.ToolDomainSchedule,
		Packs: []string{
			agenttools.ToolPackAnalyze,
			agenttools.ToolPackMutation,
		},
		Reason: "rough_build_post_refine",
	}
	flowState.Phase = agentmodel.PhaseExecuting
	return nil
}

// countPendingTasks 统计粗排后仍无位置的待安排任务数。
//
// 说明：
// 1. 第一轮修复后，粗排成功会把任务直接标记为 suggested；
// 2. 为兼容旧快照，仍按“pending 且 Slots 为空”认定真正未覆盖；
// 3. 只要这里仍大于 0，就应视为粗排异常，而不是交给 LLM 补排。
func countPendingTasks(state *schedule.ScheduleState, taskClassIDs []int) int {
	if state == nil {
		return 0
	}
	count := 0
	for i := range state.Tasks {
		task := state.Tasks[i]
		if !schedule.IsPendingTask(task) {
			continue
		}
		if len(taskClassIDs) > 0 && !schedule.IsTaskInRequestedClassScope(task, taskClassIDs) {
			continue
		}
		if schedule.IsPendingTask(task) {
			count++
		}
	}
	return count
}

// applyRoughBuildPlacements 把粗排结果写入 ScheduleState 对应任务的 Slots。
//
// 设计说明：
//  1. 通过 task_item_id（SourceID）定位任务；
//  2. 用 DayMapping 把 (week, dayOfWeek) 转为 day_index；
//  3. 对成功落位的任务写入 Slots，并显式标记为 suggested；
//  4. suggested 表示“粗排建议位”，后续可用 move/swap/unplace 微调；
//  5. 转换失败的条目静默跳过，不中断整体流程。
func applyRoughBuildPlacements(
	state *schedule.ScheduleState,
	placements []agentmodel.RoughBuildPlacement,
) roughBuildApplyStats {
	stats := roughBuildApplyStats{}
	if state == nil {
		return stats
	}

	taskIndexByItemID := make(map[int][]int)
	for i := range state.Tasks {
		task := state.Tasks[i]
		if task.Source != "task_item" {
			continue
		}
		taskIndexByItemID[task.SourceID] = append(taskIndexByItemID[task.SourceID], i)
	}

	for _, p := range placements {
		day, ok := state.WeekDayToDay(p.Week, p.DayOfWeek)
		if !ok {
			stats.DayMappingMissCount++
			stats.DayMappingMissSamples = appendPlacementSample(stats.DayMappingMissSamples, p)
			continue // DayMapping 里没有对应 day，跳过
		}

		matched := false
		for _, index := range taskIndexByItemID[p.TaskItemID] {
			t := &state.Tasks[index]
			t.Slots = []schedule.TaskSlot{
				{Day: day, SlotStart: p.SectionFrom, SlotEnd: p.SectionTo},
			}
			t.Status = schedule.TaskStatusSuggested
			stats.AppliedCount++
			matched = true
			break
		}
		if !matched {
			stats.TaskItemMatchMissCount++
			stats.TaskItemMatchMissSamples = appendPlacementSample(stats.TaskItemMatchMissSamples, p)
		}
	}
	return stats
}

// appendPlacementSample 记录有限数量的 miss 样本，避免 debug 日志爆量。
func appendPlacementSample(samples []string, placement agentmodel.RoughBuildPlacement) []string {
	if len(samples) >= roughBuildSampleLimit {
		return samples
	}
	return append(samples, fmt.Sprintf(
		"task_item_id=%d week=%d day=%d sections=%d-%d",
		placement.TaskItemID,
		placement.Week,
		placement.DayOfWeek,
		placement.SectionFrom,
		placement.SectionTo,
	))
}

// summarizeRoughBuildWindow 提供 DayMapping 的紧凑摘要，便于判断窗口是否退化到错误周。
func summarizeRoughBuildWindow(state *schedule.ScheduleState) string {
	if state == nil || len(state.Window.DayMapping) == 0 {
		return "empty"
	}
	first := state.Window.DayMapping[0]
	last := state.Window.DayMapping[len(state.Window.DayMapping)-1]
	return fmt.Sprintf(
		"days=%d first=W%dD%d last=W%dD%d",
		len(state.Window.DayMapping),
		first.Week,
		first.DayOfWeek,
		last.Week,
		last.DayOfWeek,
	)
}

// collectScopedTaskSamples 提供当前 state 中可用于匹配的 task_item 样本，便于排查 ID 对不上。
func collectScopedTaskSamples(state *schedule.ScheduleState, taskClassIDs []int) []string {
	if state == nil {
		return nil
	}
	samples := make([]string, 0, roughBuildSampleLimit)
	for i := range state.Tasks {
		task := state.Tasks[i]
		if task.Source != "task_item" {
			continue
		}
		if len(taskClassIDs) > 0 && !schedule.IsTaskInRequestedClassScope(task, taskClassIDs) {
			continue
		}
		samples = append(samples, fmt.Sprintf(
			"source_id=%d task_class_id=%d status=%s name=%q",
			task.SourceID,
			task.TaskClassID,
			task.Status,
			task.Name,
		))
		if len(samples) >= roughBuildSampleLimit {
			break
		}
	}
	return samples
}
