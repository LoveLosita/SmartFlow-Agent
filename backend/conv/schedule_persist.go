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
	// Place：pending → placed，为现有 Event 创建 Schedule
	// 前提：Event 已经存在（SourceID 是 ScheduleEvent.ID）
	// NewCoords 包含所有需要放置的位置（可能多天/多节）

	if len(change.NewCoords) == 0 {
		return fmt.Errorf("place 变更缺少目标位置")
	}

	if change.Source != "event" || change.SourceID == 0 {
		return fmt.Errorf("place 变更需要有效的 event source")
	}

	// 按周天分组，压缩成 slot ranges
	groups := groupCoordsByWeekDay(change.NewCoords)
	for week, dayGroups := range groups {
		for dayOfWeek, coords := range dayGroups {
			startSection, endSection := minMaxSection(coords)

			// 创建 schedule 记录（event 已存在，只创建 schedule）
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

			// 批量创建
			_, err := manager.Schedule.AddSchedules(schedules)
			if err != nil {
				return fmt.Errorf("创建 schedule 失败: %w", err)
			}
		}
	}
	return nil
}

// applyMoveChange 应用移动变更。
func applyMoveChange(ctx context.Context, manager *dao.RepoManager, change ScheduleChange, userID int) error {
	// Move：已有 schedule，只更新位置
	// 需要删除旧位置的 schedule，在新位置创建新 schedule

	// 1. 删除旧位置
	if change.Source == "event" && change.SourceID != 0 {
		if err := manager.Schedule.DeleteScheduleEventAndSchedule(ctx, change.SourceID, userID); err != nil {
			return fmt.Errorf("删除旧位置失败: %w", err)
		}
	}

	// 2. 创建新位置（复用 place 逻辑）
	return applyPlaceChange(ctx, manager, change, userID)
}

// applyUnplaceChange 应用移除变更。
func applyUnplaceChange(ctx context.Context, manager *dao.RepoManager, change ScheduleChange, userID int) error {
	// Unplace：删除 schedule，任务恢复为 pending
	if change.Source == "event" && change.SourceID != 0 {
		return manager.Schedule.DeleteScheduleEventAndSchedule(ctx, change.SourceID, userID)
	}
	return fmt.Errorf("unplace 变更的 source 不是 event: %s", change.Source)
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
