package conv

import (
	"context"
	"fmt"
	"sort"
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
//
// 窗口策略：
// 1. 优先从 task class 的 StartDate/EndDate 推算规划窗口，覆盖粗排所需的完整日期范围；
// 2. task class 无日期信息时，降级到当前周 7 天（兼容普通查询场景）。
//
// 日程加载策略：对窗口内每周分别调用 GetUserWeeklySchedule 并合并结果。
func (p *ScheduleProvider) LoadScheduleState(ctx context.Context, userID int) (*newagenttools.ScheduleState, error) {
	// 1. 加载用户所有任务类（含 Items 预加载）。
	taskClasses, err := p.loadCompleteTaskClasses(ctx, userID)
	if err != nil {
		return nil, err
	}

	// 2. 确定规划窗口：优先使用 task class 日期范围，降级到当前周。
	windowDays, weeks := buildWindowFromTaskClasses(taskClasses)
	if len(windowDays) == 0 {
		now := time.Now()
		currentWeek, _, err := RealDateToRelativeDate(now.Format(DateFormat))
		if err != nil {
			return nil, fmt.Errorf("解析当前日期失败: %w", err)
		}
		windowDays = make([]WindowDay, 7)
		for i := 0; i < 7; i++ {
			windowDays[i] = WindowDay{Week: currentWeek, DayOfWeek: i + 1}
		}
		weeks = []int{currentWeek}
	}

	// 3. 按周加载日程（含 Event + EmbeddedTask 预加载）。
	var allSchedules []model.Schedule
	for _, w := range weeks {
		weekSchedules, err := p.scheduleDAO.GetUserWeeklySchedule(ctx, userID, w)
		if err != nil {
			return nil, fmt.Errorf("加载用户周日程失败 week=%d: %w", w, err)
		}
		allSchedules = append(allSchedules, weekSchedules...)
	}

	// 4. 构建额外 item category 映射。
	extraItemCategories := buildExtraItemCategories(allSchedules, taskClasses)

	// 5. 调用已有的 LoadScheduleState 构建内存状态。
	return LoadScheduleState(allSchedules, taskClasses, extraItemCategories, windowDays), nil
}

// buildWindowFromTaskClasses 从 task class 的 StartDate/EndDate 推算规划窗口。
//
// 返回值：
//   - windowDays：窗口内每天的 (week, dayOfWeek) 有序列表；
//   - weeks：窗口覆盖的周号（去重、升序），供按周加载日程使用；
//   - 若无有效日期信息，返回空切片，调用方应降级到默认窗口。
func buildWindowFromTaskClasses(taskClasses []model.TaskClass) (windowDays []WindowDay, weeks []int) {
	var minDate, maxDate *time.Time
	for _, tc := range taskClasses {
		if tc.StartDate != nil && (minDate == nil || tc.StartDate.Before(*minDate)) {
			t := *tc.StartDate
			minDate = &t
		}
		if tc.EndDate != nil && (maxDate == nil || tc.EndDate.After(*maxDate)) {
			t := *tc.EndDate
			maxDate = &t
		}
	}
	if minDate == nil || maxDate == nil {
		return nil, nil
	}

	startWeek, startDay, err := RealDateToRelativeDate(minDate.Format(DateFormat))
	if err != nil {
		return nil, nil
	}
	endWeek, endDay, err := RealDateToRelativeDate(maxDate.Format(DateFormat))
	if err != nil {
		return nil, nil
	}

	weeksSet := make(map[int]bool)
	w, d := startWeek, startDay
	for {
		windowDays = append(windowDays, WindowDay{Week: w, DayOfWeek: d})
		weeksSet[w] = true
		if w == endWeek && d == endDay {
			break
		}
		d++
		if d > 7 {
			d = 1
			w++
		}
		if w > endWeek+1 { // 防止因日期转换异常导致无限循环
			break
		}
	}

	weeks = make([]int, 0, len(weeksSet))
	for wk := range weeksSet {
		weeks = append(weeks, wk)
	}
	sort.Ints(weeks)
	return windowDays, weeks
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

// LoadTaskClassMetas 加载指定任务类的约束元数据（不含 Items、不含日程），供 Plan 阶段提前消费。
func (p *ScheduleProvider) LoadTaskClassMetas(ctx context.Context, userID int, taskClassIDs []int) ([]newagenttools.TaskClassMeta, error) {
	if len(taskClassIDs) == 0 {
		return nil, nil
	}
	complete, err := p.taskClassDAO.GetCompleteTaskClassesByIDs(ctx, userID, taskClassIDs)
	if err != nil {
		return nil, fmt.Errorf("加载任务类元数据失败: %w", err)
	}
	metas := make([]newagenttools.TaskClassMeta, 0, len(complete))
	for _, tc := range complete {
		meta := newagenttools.TaskClassMeta{
			ID:   tc.ID,
			Name: derefString(tc.Name),
		}
		if tc.Strategy != nil {
			meta.Strategy = *tc.Strategy
		}
		if tc.TotalSlots != nil {
			meta.TotalSlots = *tc.TotalSlots
		}
		if tc.AllowFillerCourse != nil {
			meta.AllowFillerCourse = *tc.AllowFillerCourse
		}
		if tc.ExcludedSlots != nil {
			meta.ExcludedSlots = []int(tc.ExcludedSlots)
		}
		if tc.StartDate != nil {
			meta.StartDate = tc.StartDate.Format("2006-01-02")
		}
		if tc.EndDate != nil {
			meta.EndDate = tc.EndDate.Format("2006-01-02")
		}
		metas = append(metas, meta)
	}
	return metas, nil
}

func derefString(s *string) string {
	if s == nil {
		return ""
	}
	return *s
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
