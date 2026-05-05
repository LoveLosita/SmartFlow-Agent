package agentconv

import (
	schedule "github.com/LoveLosita/smartflow/backend/services/agent/tools/schedule"
	"github.com/LoveLosita/smartflow/backend/services/runtime/model"
	"github.com/LoveLosita/smartflow/backend/shared/respond"
)

// ApplyPlacedItems 将前端提交的绝对时间放置项应用到 ScheduleState。
//
// 职责边界：
// 1. 只修改 source=task_item 的任务，source=event 的课程不受影响；
// 2. 不在请求中的任务保持原样（slots/status/embed 不变）；
// 3. 不校验 Slots 的业务合法性（冲突等由 execute 节点兜底）；
// 4. 返回 respond.XXX 错误，调用方可直接透传给 DealWithError。
func ApplyPlacedItems(
	state *schedule.ScheduleState,
	items []model.SaveScheduleStatePlacedItem,
) error {
	// 1. 构建索引。
	sourceIDToTask := make(map[int]*schedule.ScheduleTask, len(state.Tasks))
	eventSourceIDToTask := make(map[int]*schedule.ScheduleTask)
	for i := range state.Tasks {
		t := &state.Tasks[i]
		if t.Source == "task_item" {
			sourceIDToTask[t.SourceID] = t
		} else if t.Source == "event" {
			eventSourceIDToTask[t.SourceID] = t
		}
	}

	// 2. 去重检查。
	seen := make(map[int]struct{}, len(items))
	for _, item := range items {
		if _, dup := seen[item.TaskItemID]; dup {
			return respond.ScheduleStateDuplicateTaskItem
		}
		seen[item.TaskItemID] = struct{}{}
	}

	// 3. 逐个处理 item。
	for _, item := range items {
		// 3.1 绝对坐标 → 相对 day_index。
		dayIndex, ok := state.WeekDayToDay(item.Week, item.DayOfWeek)
		if !ok {
			return respond.ScheduleStateInvalidCoordinates
		}

		// 3.2 在快照中查找对应的 task_item。
		task, found := sourceIDToTask[item.TaskItemID]
		if !found {
			return respond.ScheduleStateTaskItemNotFound
		}

		// 3.3 清除旧嵌入关系。
		if task.EmbedHost != nil {
			oldHost := state.TaskByStateID(*task.EmbedHost)
			if oldHost != nil {
				oldHost.EmbeddedBy = nil
			}
			task.EmbedHost = nil
		}

		// 3.4 设置新嵌入关系。
		if item.EmbedCourseEventID != 0 {
			hostEvent := eventSourceIDToTask[item.EmbedCourseEventID]
			if hostEvent == nil {
				return respond.ScheduleStateEventNotFound
			}
			hostStateID := hostEvent.StateID
			guestStateID := task.StateID
			task.EmbedHost = &hostStateID
			hostEvent.EmbeddedBy = &guestStateID
		}

		// 3.5 更新 Slots。
		task.Slots = []schedule.TaskSlot{{
			Day:       dayIndex,
			SlotStart: item.StartSection,
			SlotEnd:   item.EndSection,
		}}

		// 3.6 pending → suggested。
		if task.Status == schedule.TaskStatusPending {
			task.Status = schedule.TaskStatusSuggested
		}
	}

	return nil
}
