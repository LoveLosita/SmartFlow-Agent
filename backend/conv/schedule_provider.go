package conv

import (
	"context"
	"fmt"
	"time"

	"github.com/LoveLosita/smartflow/backend/dao"
	"github.com/LoveLosita/smartflow/backend/model"
	newagenttools "github.com/LoveLosita/smartflow/backend/newAgent/tools"
)

// ScheduleProvider 实现 model.ScheduleStateProvider 接口。
// 通过 DAO 层加载用户的日程和任务数据，调用 LoadScheduleState 构建内存状态。
//
// 职责边界：
// 1. 只负责"从 DB 查数据 + 调 LoadScheduleState 转换"，不含业务逻辑；
// 2. 不负责缓存（由上层 Service 决定是否缓存）；
// 3. 不负责 Diff 和持久化（由 Confirm 流程负责）。
type ScheduleProvider struct {
	scheduleDAO  *dao.ScheduleDAO
	taskClassDAO *dao.TaskClassDAO
}

// NewScheduleProvider 创建 ScheduleProvider。
func NewScheduleProvider(scheduleDAO *dao.ScheduleDAO, taskClassDAO *dao.TaskClassDAO) *ScheduleProvider {
	return &ScheduleProvider{
		scheduleDAO:  scheduleDAO,
		taskClassDAO: taskClassDAO,
	}
}

// LoadScheduleState 实现 model.ScheduleStateProvider 接口。
// 加载用户当前周的日程和所有待安排任务，构建 ScheduleState。
func (p *ScheduleProvider) LoadScheduleState(ctx context.Context, userID int) (*newagenttools.ScheduleState, error) {
	// 1. 确定当前周。
	now := time.Now()
	week, _, err := RealDateToRelativeDate(now.Format(DateFormat))
	if err != nil {
		return nil, fmt.Errorf("解析当前日期失败: %w", err)
	}

	// 2. 加载当前周的所有日程（含 Event + EmbeddedTask 预加载）。
	schedules, err := p.scheduleDAO.GetUserWeeklySchedule(ctx, userID, week)
	if err != nil {
		return nil, fmt.Errorf("加载用户周日程失败: %w", err)
	}

	// 3. 加载用户所有任务类（含 Items 预加载）。
	// 两步：先拿 ID 列表，再批量获取完整数据（含 Items）。
	taskClasses, err := p.loadCompleteTaskClasses(ctx, userID)
	if err != nil {
		return nil, err
	}

	// 4. 构建 WindowDay 列表（当前周 7 天）。
	windowDays := make([]WindowDay, 7)
	for i := 0; i < 7; i++ {
		windowDays[i] = WindowDay{Week: week, DayOfWeek: i + 1}
	}

	// 5. 构建额外 item category 映射（已加载全部 taskClass，通常为空）。
	extraItemCategories := buildExtraItemCategories(schedules, taskClasses)

	// 6. 调用已有的 LoadScheduleState 构建内存状态。
	return LoadScheduleState(schedules, taskClasses, extraItemCategories, windowDays), nil
}

// loadCompleteTaskClasses 批量加载用户所有任务类（含 Items 预加载）。
func (p *ScheduleProvider) loadCompleteTaskClasses(ctx context.Context, userID int) ([]model.TaskClass, error) {
	basicClasses, err := p.taskClassDAO.GetUserTaskClasses(userID)
	if err != nil {
		return nil, fmt.Errorf("加载用户任务类失败: %w", err)
	}
	if len(basicClasses) == 0 {
		return nil, nil
	}

	ids := make([]int, len(basicClasses))
	for i, tc := range basicClasses {
		ids[i] = tc.ID
	}

	complete, err := p.taskClassDAO.GetCompleteTaskClassesByIDs(ctx, userID, ids)
	if err != nil {
		return nil, fmt.Errorf("加载完整任务类失败: %w", err)
	}
	return complete, nil
}

// buildExtraItemCategories 从已有日程中提取不属于给定 taskClasses 的 task event 的 category 映射。
// 当加载全部 taskClass 时，通常返回空 map。
func buildExtraItemCategories(schedules []model.Schedule, taskClasses []model.TaskClass) map[int]string {
	knownItemIDs := make(map[int]bool)
	for _, tc := range taskClasses {
		for _, item := range tc.Items {
			knownItemIDs[item.ID] = true
		}
	}

	categories := make(map[int]string)
	for _, s := range schedules {
		if s.Event == nil || s.Event.Type != "task" || s.Event.RelID == nil {
			continue
		}
		itemID := *s.Event.RelID
		if !knownItemIDs[itemID] {
			categories[itemID] = "任务"
		}
	}
	return categories
}
