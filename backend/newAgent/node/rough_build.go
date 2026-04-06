package newagentnode

import (
	"context"
	"fmt"

	newagentmodel "github.com/LoveLosita/smartflow/backend/newAgent/model"
	newagenttools "github.com/LoveLosita/smartflow/backend/newAgent/tools"
)

const (
	roughBuildStageName   = "rough_build"
	roughBuildStatusBlock = "rough_build.status"
)

// RunRoughBuildNode 执行粗排节点逻辑。
//
// 步骤说明：
//  1. 推送"正在粗排"状态给前端；
//  2. 从 CommonState 读取 TaskClassIDs，确认有需要排课的任务类；
//  3. 加载 ScheduleState（含 DayMapping）；
//  4. 调用 RoughBuildFunc 拿到粗排结果（[]RoughBuildPlacement）；
//  5. 把粗排结果写入 ScheduleState 的对应 task.Slots（pending 任务预填位置）；
//  6. 推送"粗排完成"状态，清除 NeedsRoughBuild 标记，进入执行阶段。
func RunRoughBuildNode(ctx context.Context, st *newagentmodel.AgentGraphState) error {
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
		flowState.Phase = newagentmodel.PhaseExecuting
		flowState.NeedsRoughBuild = false
		return nil
	}

	// 4. 加载 ScheduleState（含 DayMapping，用于坐标转换）。
	scheduleState, err := st.EnsureScheduleState(ctx)
	if err != nil {
		return fmt.Errorf("rough build node: 加载日程状态失败: %w", err)
	}
	if scheduleState == nil {
		return fmt.Errorf("rough build node: ScheduleState 为空，无法执行粗排")
	}

	// 5. 调用粗排算法。
	placements, err := st.Deps.RoughBuildFunc(ctx, flowState.UserID, taskClassIDs)
	if err != nil {
		return fmt.Errorf("rough build node: 粗排算法失败: %w", err)
	}

	// 6. 把粗排结果写入 ScheduleState。
	applyRoughBuildPlacements(scheduleState, placements)

	// 7. 推送完成状态。
	_ = emitter.EmitStatus(
		roughBuildStatusBlock,
		roughBuildStageName,
		"rough_build_done",
		fmt.Sprintf("初始排课方案已生成，共 %d 个任务已预排，进入微调阶段。", len(placements)),
		false,
	)

	// 8. 把粗排完成信息写入 pinned context，让 Execute 阶段的 LLM 直接跳过"触发粗排"，
	//    进入验证和微调，避免 LLM 误以为需要自己运行算法而浪费一轮工具调用。
	st.EnsureConversationContext().UpsertPinnedBlock(newagentmodel.ContextBlock{
		Key:   "rough_build_done",
		Title: "粗排已完成",
		Content: fmt.Sprintf(
			"后端已自动运行粗排算法，初始排课方案已写入日程状态（共 %d 个任务已预排）。\n"+
				"请直接调用 get_overview 查看预排结果，然后用 move/swap 微调不合理的位置。\n"+
				"无需再次触发粗排，也不要在 plan_steps 里描述触发粗排相关的操作。",
			len(placements),
		),
	})

	// 9. 清除标记，进入执行阶段。
	flowState.NeedsRoughBuild = false
	flowState.Phase = newagentmodel.PhaseExecuting
	return nil
}

// applyRoughBuildPlacements 把粗排结果写入 ScheduleState 对应任务的 Slots。
//
// 设计说明：
//  1. 通过 task_item_id（SourceID）定位任务；
//  2. 用 DayMapping 把 (week, dayOfWeek) 转为 day_index；
//  3. task.Status 保持 "pending"，让 LLM 在 Execute 阶段看到"有建议位置的待安排任务"，
//     可用 move/swap 微调，也可用 unplace 推翻粗排结果；
//  4. 转换失败的条目静默跳过，不中断整体流程。
func applyRoughBuildPlacements(state *newagenttools.ScheduleState, placements []newagentmodel.RoughBuildPlacement) {
	if state == nil {
		return
	}
	for _, p := range placements {
		day, ok := state.WeekDayToDay(p.Week, p.DayOfWeek)
		if !ok {
			continue // DayMapping 里没有对应 day，跳过
		}
		for i := range state.Tasks {
			t := &state.Tasks[i]
			if t.Source != "task_item" || t.SourceID != p.TaskItemID {
				continue
			}
			t.Slots = []newagenttools.TaskSlot{
				{Day: day, SlotStart: p.SectionFrom, SlotEnd: p.SectionTo},
			}
			break
		}
	}
}
