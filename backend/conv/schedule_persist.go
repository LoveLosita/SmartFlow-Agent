package conv

import (
	"context"
	"fmt"

	"github.com/LoveLosita/smartflow/backend/dao"
	"github.com/LoveLosita/smartflow/backend/model"
	newagenttools "github.com/LoveLosita/smartflow/backend/newAgent/tools"
)

// SchedulePersistorAdapter 实现 model.SchedulePersistor 接口。
// 组合 RepoManager，调用 PersistScheduleChanges 持久化变更。
type SchedulePersistorAdapter struct {
	manager *dao.RepoManager
}

// NewSchedulePersistorAdapter 创建持久化适配器。
func NewSchedulePersistorAdapter(manager *dao.RepoManager) *SchedulePersistorAdapter {
	return &SchedulePersistorAdapter{manager: manager}
}

// PersistScheduleChanges 实现 model.SchedulePersistor 接口。
func (a *SchedulePersistorAdapter) PersistScheduleChanges(ctx context.Context, original, modified *newagenttools.ScheduleState, userID int) error {
	return PersistScheduleChanges(ctx, a.manager, original, modified, userID)
}

// PersistScheduleChanges 将内存中的 ScheduleState 变更持久化到数据库。
//
// 职责边界：
// 1. 调用 DiffScheduleState 计算变更；
// 2. 在事务中逐个应用变更到数据库；
// 3. 全部成功或全部回滚，保证原子性。
func PersistScheduleChanges(
	ctx context.Context,
	manager *dao.RepoManager,
	original *newagenttools.ScheduleState,
	modified *newagenttools.ScheduleState,
	userID int,
) error {
	changes := DiffScheduleState(original, modified)
	if len(changes) == 0 {
		return nil
	}

	return manager.Transaction(ctx, func(txM *dao.RepoManager) error {
		for _, change := range changes {
			if err := applyScheduleChange(ctx, txM, change, userID); err != nil {
				return fmt.Errorf("应用变更失败 [%s %s]: %w", change.Type, change.Name, err)
			}
		}
		return nil
	})
}

// applyScheduleChange 应用单个变更到数据库。
func applyScheduleChange(ctx context.Context, manager *dao.RepoManager, change ScheduleChange, userID int) error {
	switch change.Type {
	case ChangePlace:
		return applyPlaceChange(ctx, manager, change, userID)
	case ChangeMove:
		return applyMoveChange(ctx, manager, change, userID)
	case ChangeUnplace:
		return applyUnplaceChange(ctx, manager, change, userID)
	default:
		return fmt.Errorf("未知变更类型: %s", change.Type)
	}
}

// applyPlaceChange 应用放置变更。
func applyPlaceChange(ctx context.Context, manager *dao.RepoManager, change ScheduleChange, userID int) error {
	if len(change.NewCoords) == 0 {
		return fmt.Errorf("place 变更缺少目标位置")
	}
	switch change.Source {
	case "event":
		return applyPlaceEventSource(ctx, manager, change, userID)
	case "task_item":
		return applyPlaceTaskItem(ctx, manager, change, userID)
	default:
		return fmt.Errorf("place 变更不支持的 source: %s", change.Source)
	}
}

// applyPlaceEventSource 处理 source=event 的放置（为已有 Event 创建 Schedule 记录）。
func applyPlaceEventSource(ctx context.Context, manager *dao.RepoManager, change ScheduleChange, userID int) error {
	if change.SourceID == 0 {
		return fmt.Errorf("place event 变更需要有效的 source_id")
	}
	groups := groupCoordsByWeekDay(change.NewCoords)
	for week, dayGroups := range groups {
		for dayOfWeek, coords := range dayGroups {
			startSection, endSection := minMaxSection(coords)
			schedules := make([]model.Schedule, endSection-startSection+1)
			for sec := startSection; sec <= endSection; sec++ {
				schedules[sec-startSection] = model.Schedule{
					UserID:    userID,
					Week:      week,
					DayOfWeek: dayOfWeek,
					Section:   sec,
					EventID:   change.SourceID,
				}
			}
			if _, err := manager.Schedule.AddSchedules(schedules); err != nil {
				return fmt.Errorf("创建 schedule 失败: %w", err)
			}
		}
	}
	return nil
}

// applyPlaceTaskItem 处理 source=task_item 的放置。
//
// 两条路径：
// 1. 嵌入水课（HostEventID != 0）：在宿主 Schedule 记录上设置 embedded_task_id。
// 2. 普通放置（HostEventID == 0）：新建 ScheduleEvent(type=task) + Schedule 记录。
// 两条路径最终都更新 task_items.embedded_time。
func applyPlaceTaskItem(ctx context.Context, manager *dao.RepoManager, change ScheduleChange, userID int) error {
	if change.SourceID == 0 {
		return fmt.Errorf("place task_item 变更需要有效的 source_id")
	}

	// task_item 只占一段连续时段，取第一个 coord 的 week/dayOfWeek
	first := change.NewCoords[0]
	week, dayOfWeek := first.Week, first.DayOfWeek
	startSection, endSection := minMaxSection(change.NewCoords)

	targetTime := &model.TargetTime{
		Week:        week,
		DayOfWeek:   dayOfWeek,
		SectionFrom: startSection,
		SectionTo:   endSection,
	}

	if change.HostEventID != 0 {
		// 嵌入路径：更新宿主 Schedule 记录的 embedded_task_id
		if err := manager.Schedule.EmbedTaskIntoSchedule(
			startSection, endSection, dayOfWeek, week, userID, change.SourceID,
		); err != nil {
			return fmt.Errorf("嵌入水课失败: %w", err)
		}
	} else {
		// 普通路径：新建 ScheduleEvent + Schedule 记录
		startTime, endTime, err := RelativeTimeToRealTime(week, dayOfWeek, startSection, endSection)
		if err != nil {
			return fmt.Errorf("时间转换失败: %w", err)
		}
		relID := change.SourceID
		event := model.ScheduleEvent{
			UserID:        userID,
			Name:          change.Name,
			Type:          "task",
			RelID:         &relID,
			CanBeEmbedded: false,
			StartTime:     startTime,
			EndTime:       endTime,
		}
		eventID, err := manager.Schedule.AddScheduleEvent(&event)
		if err != nil {
			return fmt.Errorf("创建 schedule_event 失败: %w", err)
		}
		schedules := make([]model.Schedule, endSection-startSection+1)
		for i, sec := 0, startSection; sec <= endSection; i, sec = i+1, sec+1 {
			schedules[i] = model.Schedule{
				UserID:    userID,
				Week:      week,
				DayOfWeek: dayOfWeek,
				Section:   sec,
				EventID:   eventID,
				Status:    "normal",
			}
		}
		if _, err := manager.Schedule.AddSchedules(schedules); err != nil {
			return fmt.Errorf("创建 schedule 记录失败: %w", err)
		}
	}

	if err := manager.TaskClass.UpdateTaskClassItemEmbeddedTime(ctx, change.SourceID, targetTime); err != nil {
		return fmt.Errorf("更新 task_item embedded_time 失败: %w", err)
	}
	return nil
}

// applyMoveChange 应用移动变更。
func applyMoveChange(ctx context.Context, manager *dao.RepoManager, change ScheduleChange, userID int) error {
	switch change.Source {
	case "event":
		if change.SourceID != 0 {
			if err := manager.Schedule.DeleteScheduleEventAndSchedule(ctx, change.SourceID, userID); err != nil {
				return fmt.Errorf("删除旧位置失败: %w", err)
			}
		}
	case "task_item":
		// 清理旧位置
		if change.OldHostEventID != 0 {
			// 旧位置是嵌入：清空宿主的 embedded_task_id
			if _, err := manager.Schedule.SetScheduleEmbeddedTaskIDToNull(ctx, change.OldHostEventID); err != nil {
				return fmt.Errorf("清除旧嵌入关系失败: %w", err)
			}
		} else {
			// 旧位置是普通 task event：按 task_item_id 删除
			if err := manager.Schedule.DeleteScheduleEventByTaskItemID(ctx, change.SourceID); err != nil {
				return fmt.Errorf("删除旧 task_item 日程失败: %w", err)
			}
		}
	}
	return applyPlaceChange(ctx, manager, change, userID)
}

// applyUnplaceChange 应用移除变更。
func applyUnplaceChange(ctx context.Context, manager *dao.RepoManager, change ScheduleChange, userID int) error {
	switch change.Source {
	case "event":
		if change.SourceID == 0 {
			return fmt.Errorf("unplace event 变更需要有效的 source_id")
		}
		return manager.Schedule.DeleteScheduleEventAndSchedule(ctx, change.SourceID, userID)
	case "task_item":
		if change.SourceID == 0 {
			return fmt.Errorf("unplace task_item 变更需要有效的 source_id")
		}
		if change.HostEventID != 0 {
			// 是嵌入：清空宿主 Schedule 的 embedded_task_id
			if _, err := manager.Schedule.SetScheduleEmbeddedTaskIDToNull(ctx, change.HostEventID); err != nil {
				return fmt.Errorf("清除嵌入关系失败: %w", err)
			}
		} else {
			// 普通 task event：按 task_item_id 删除
			if err := manager.Schedule.DeleteScheduleEventByTaskItemID(ctx, change.SourceID); err != nil {
				return fmt.Errorf("删除 task_item 日程失败: %w", err)
			}
		}
		if err := manager.TaskClass.DeleteTaskClassItemEmbeddedTime(ctx, change.SourceID); err != nil {
			return fmt.Errorf("清除 task_item embedded_time 失败: %w", err)
		}
		return nil
	default:
		return fmt.Errorf("unplace 变更不支持的 source: %s", change.Source)
	}
}

// ==================== 辅助函数 ====================

// intPtr 返回 int 指针，零值返回 nil。
func intPtr(v int) *int {
	if v == 0 {
		return nil
	}
	return &v
}

// groupCoordsByWeekDay 按周天分组坐标。
func groupCoordsByWeekDay(coords []SlotCoord) map[int]map[int][]SlotCoord {
	result := make(map[int]map[int][]SlotCoord)
	for _, coord := range coords {
		if result[coord.Week] == nil {
			result[coord.Week] = make(map[int][]SlotCoord)
		}
		result[coord.Week][coord.DayOfWeek] = append(result[coord.Week][coord.DayOfWeek], coord)
	}
	return result
}

// minMaxSection 返回坐标列表中的最小和最大节次。
func minMaxSection(coords []SlotCoord) (min, max int) {
	if len(coords) == 0 {
		return 0, 0
	}
	min, max = coords[0].Section, coords[0].Section
	for _, c := range coords[1:] {
		if c.Section < min {
			min = c.Section
		}
		if c.Section > max {
			max = c.Section
		}
	}
	return
}
