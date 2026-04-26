package newagentconv

import (
	"context"
	"fmt"
	"sort"
	"time"

	baseconv "github.com/LoveLosita/smartflow/backend/conv"
	"github.com/LoveLosita/smartflow/backend/dao"
	"github.com/LoveLosita/smartflow/backend/model"
	schedule "github.com/LoveLosita/smartflow/backend/newAgent/tools/schedule"
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
func (p *ScheduleProvider) LoadScheduleState(ctx context.Context, userID int) (*schedule.ScheduleState, error) {
	// 1. 加载用户所有任务类（含 Items 预加载）。
	taskClasses, err := p.loadCompleteTaskClasses(ctx, userID)
	if err != nil {
		return nil, err
	}

	// 2. 全量读场景保留“当前周兜底”，兼容“只看本周课表/微调”类请求。
	return p.loadScheduleStateWithTaskClasses(ctx, userID, taskClasses, true)
}

// LoadScheduleStateForTaskClasses 按“本轮请求的任务类范围”加载 ScheduleState。
//
// 设计说明：
// 1. 负责：让粗排 / Execute 首次读取的 DayMapping 与本轮 task_class_ids 保持同一时间窗口；
// 2. 不负责：裁掉窗口内已有的 existing/suggested 阻塞物，这部分仍由日程加载主流程统一保留；
// 3. 失败策略：若 task_class_ids 为空，则退回全量加载，避免调用方额外分支。
func (p *ScheduleProvider) LoadScheduleStateForTaskClasses(
	ctx context.Context,
	userID int,
	taskClassIDs []int,
) (*schedule.ScheduleState, error) {
	if len(taskClassIDs) == 0 {
		return p.LoadScheduleState(ctx, userID)
	}

	taskClasses, err := p.loadCompleteTaskClassesByIDs(ctx, userID, taskClassIDs)
	if err != nil {
		return nil, err
	}

	// 1. 粗排/主动编排场景必须严格按任务类时间窗加载；
	// 2. 若任务类缺少起止日期，则返回错误，交给上层 ask_user 补齐，而不是静默退回当前周。
	return p.loadScheduleStateWithTaskClasses(ctx, userID, taskClasses, false)
}

// loadScheduleStateWithTaskClasses 负责把“指定任务类集合”装配成可操作的 ScheduleState。
//
// 步骤说明：
// 1. 先根据传入 taskClasses 计算 DayMapping 窗口，保证粗排坐标能映射回 day_index；
// 2. 若窗口无法从任务类日期推导，则退回当前周 7 天，兼容普通查询场景；
// 3. 再按窗口覆盖的周批量拉取 existing schedules，与 taskClasses 一起交给 LoadScheduleState 统一建模。
func (p *ScheduleProvider) loadScheduleStateWithTaskClasses(
	ctx context.Context,
	userID int,
	taskClasses []model.TaskClass,
	allowCurrentWeekFallback bool,
) (*schedule.ScheduleState, error) {
	// 1. 确定规划窗口：优先使用 task class 日期范围，降级到当前周。
	windowDays, weeks := buildWindowFromTaskClasses(taskClasses)
	if len(windowDays) == 0 {
		if !allowCurrentWeekFallback {
			return nil, fmt.Errorf("任务类缺少有效时间窗：请补充 start_date/end_date 后再进行智能编排")
		}
		var err error
		windowDays, weeks, err = buildCurrentWeekWindow()
		if err != nil {
			return nil, err
		}
	}

	// 2. 按周加载日程（含 Event + EmbeddedTask 预加载）。
	var allSchedules []model.Schedule
	for _, w := range weeks {
		weekSchedules, err := p.scheduleDAO.GetUserWeeklySchedule(ctx, userID, w)
		if err != nil {
			return nil, fmt.Errorf("加载用户周日程失败 week=%d: %w", w, err)
		}
		allSchedules = append(allSchedules, weekSchedules...)
	}

	// 3. 构建额外 item category 映射。
	extraItemCategories := buildExtraItemCategories(allSchedules, taskClasses)

	// 4. 调用已有的 LoadScheduleState 构建内存状态。
	return LoadScheduleState(allSchedules, taskClasses, extraItemCategories, windowDays), nil
}

// buildWindowFromTaskClasses 从 task class 的 StartDate/EndDate 推算规划窗口。
//
// 返回值：
//   - windowDays：窗口内每天的 (week, dayOfWeek) 有序列表；
//   - weeks：窗口覆盖的周号（去重、升序），供按周加载日程使用；
//   - 若无有效日期信息，返回空切片，调用方应降级到默认窗口。
func buildWindowFromTaskClasses(taskClasses []model.TaskClass) (windowDays []WindowDay, weeks []int) {
	minWeek, minDay := 0, 0
	maxWeek, maxDay := 0, 0
	hasWindow := false

	for _, tc := range taskClasses {
		// 1. 先要求任务类具备完整且合法的起止日期，避免坏数据把整轮窗口拖坏。
		// 2. 再逐条做绝对日期 -> 相对周/天转换；转换失败的任务类直接忽略，不影响其余合法任务类。
		// 3. 只有至少一条任务类成功进入窗口后，才返回有效 DayMapping。
		if tc.StartDate == nil || tc.EndDate == nil || tc.EndDate.Before(*tc.StartDate) {
			continue
		}
		startWeek, startDay, err := baseconv.RealDateToRelativeDate(tc.StartDate.Format(baseconv.DateFormat))
		if err != nil {
			continue
		}
		endWeek, endDay, err := baseconv.RealDateToRelativeDate(tc.EndDate.Format(baseconv.DateFormat))
		if err != nil {
			continue
		}
		if !hasWindow || isRelativeDateBefore(startWeek, startDay, minWeek, minDay) {
			minWeek, minDay = startWeek, startDay
		}
		if !hasWindow || isRelativeDateBefore(maxWeek, maxDay, endWeek, endDay) {
			maxWeek, maxDay = endWeek, endDay
		}
		hasWindow = true
	}
	if !hasWindow {
		return nil, nil
	}

	weeksSet := make(map[int]bool)
	w, d := minWeek, minDay
	for {
		windowDays = append(windowDays, WindowDay{Week: w, DayOfWeek: d})
		weeksSet[w] = true
		if w == maxWeek && d == maxDay {
			break
		}
		d++
		if d > 7 {
			d = 1
			w++
		}
		if w > maxWeek+1 { // 防止因日期转换异常导致无限循环
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

// buildCurrentWeekWindow 构造“当前周 7 天”的兜底窗口。
func buildCurrentWeekWindow() (windowDays []WindowDay, weeks []int, err error) {
	now := time.Now()
	currentWeek, _, err := baseconv.RealDateToRelativeDate(now.Format(baseconv.DateFormat))
	if err != nil {
		return nil, nil, fmt.Errorf("解析当前日期失败: %w", err)
	}
	windowDays = make([]WindowDay, 7)
	for i := 0; i < 7; i++ {
		windowDays[i] = WindowDay{Week: currentWeek, DayOfWeek: i + 1}
	}
	return windowDays, []int{currentWeek}, nil
}

// isRelativeDateBefore 比较两个“相对周/天”坐标的先后关系。
func isRelativeDateBefore(leftWeek, leftDay, rightWeek, rightDay int) bool {
	if leftWeek != rightWeek {
		return leftWeek < rightWeek
	}
	return leftDay < rightDay
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

// loadCompleteTaskClassesByIDs 批量加载指定任务类（含 Items 预加载）。
func (p *ScheduleProvider) loadCompleteTaskClassesByIDs(
	ctx context.Context,
	userID int,
	taskClassIDs []int,
) ([]model.TaskClass, error) {
	if len(taskClassIDs) == 0 {
		return nil, nil
	}

	complete, err := p.taskClassDAO.GetCompleteTaskClassesByIDs(ctx, userID, taskClassIDs)
	if err != nil {
		return nil, fmt.Errorf("加载指定任务类失败: %w", err)
	}
	return complete, nil
}

// LoadTaskClassMetas 加载指定任务类的约束元数据（不含 Items、不含日程），供 Plan 阶段提前消费。
func (p *ScheduleProvider) LoadTaskClassMetas(ctx context.Context, userID int, taskClassIDs []int) ([]schedule.TaskClassMeta, error) {
	if len(taskClassIDs) == 0 {
		return nil, nil
	}
	complete, err := p.taskClassDAO.GetCompleteTaskClassesByIDs(ctx, userID, taskClassIDs)
	if err != nil {
		return nil, fmt.Errorf("加载任务类元数据失败: %w", err)
	}
	metas := make([]schedule.TaskClassMeta, 0, len(complete))
	for _, tc := range complete {
		meta := schedule.TaskClassMeta{
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
		if tc.ExcludedDaysOfWeek != nil {
			meta.ExcludedDaysOfWeek = []int(tc.ExcludedDaysOfWeek)
		}
		if tc.StartDate != nil {
			meta.StartDate = tc.StartDate.Format("2006-01-02")
		}
		if tc.EndDate != nil {
			meta.EndDate = tc.EndDate.Format("2006-01-02")
		}
		if tc.SubjectType != nil {
			meta.SubjectType = *tc.SubjectType
		}
		if tc.DifficultyLevel != nil {
			meta.DifficultyLevel = *tc.DifficultyLevel
		}
		if tc.CognitiveIntensity != nil {
			meta.CognitiveIntensity = *tc.CognitiveIntensity
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
