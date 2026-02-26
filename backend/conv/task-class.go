package conv

import (
	"errors"
	"time"

	"github.com/LoveLosita/smartflow/backend/model"
	"github.com/LoveLosita/smartflow/backend/respond"
)

const dateLayout = "2006-01-02"

func parseDatePtr(s string) (*time.Time, error) {
	if s == "" {
		return nil, nil
	}
	t, err := time.ParseInLocation(dateLayout, s, time.Local)
	if err != nil {
		return nil, err
	}
	return &t, nil
}

func ProcessUserAddTaskClassRequest(req *model.UserAddTaskClassRequest, userID int) (*model.TaskClass, []model.TaskClassItem, error) {
	startDate, err := parseDatePtr(req.StartDate)
	if err != nil {
		return nil, nil, respond.WrongParamType
	}
	endDate, err := parseDatePtr(req.EndDate)
	if err != nil {
		return nil, nil, respond.WrongParamType
	}
	//1.填充section1,2
	taskClass := model.TaskClass{
		Name:      &req.Name,
		Mode:      &req.Mode,
		StartDate: startDate,
		EndDate:   endDate,
		UserID:    &userID,
	}
	//2.填充section3
	taskClass.TotalSlots = &req.Config.TotalSlots
	taskClass.AllowFillerCourse = &req.Config.AllowFillerCourse
	taskClass.Strategy = &req.Config.Strategy
	/*//处理 ExcludedSlots 切片为 JSON 字符串
	if len(req.Config.ExcludedSlots) > 0 {
		//转换为 JSON 字符串
		excludedSlotsJSON := "["
		for i, slot := range req.Config.ExcludedSlots {
			excludedSlotsJSON += string(rune(slot + '0')) //简单转换为字符
			if i != len(req.Config.ExcludedSlots)-1 {
				excludedSlotsJSON += ","
			}
		}
		excludedSlotsJSON += "]"
		taskClass.ExcludedSlots = &excludedSlotsJSON
	} else {
		emptyJSON := "[]"
		taskClass.ExcludedSlots = &emptyJSON
	}*/
	taskClass.ExcludedSlots = req.Config.ExcludedSlots // 直接复用 IntSlice 类型，前端也能正确解析为 []int
	//3.开始构建 items
	var items []model.TaskClassItem
	for _, itemReq := range req.Items {
		item := model.TaskClassItem{ //填充section 2
			Order:        &itemReq.Order,
			Content:      &itemReq.Content,
			EmbeddedTime: itemReq.EmbeddedTime,
			Status:       nil,
		}
		items = append(items, item)
	}
	return &taskClass, items, nil
}

func timeOrZero(t *time.Time) time.Time {
	if t == nil {
		return time.Time{}
	}
	return *t
}

func TaskClassModelToResponse(taskClasses []model.TaskClass) *model.UserGetTaskClassesResponse {
	var resp model.UserGetTaskClassesResponse
	for _, tc := range taskClasses {
		tcResp := model.TaskClassSummary{
			ID:         tc.ID,
			Name:       *tc.Name,
			Mode:       *tc.Mode,
			StartDate:  timeOrZero(tc.StartDate),
			EndDate:    timeOrZero(tc.EndDate),
			TotalSlots: *tc.TotalSlots,
			Strategy:   *tc.Strategy,
		}
		resp.TaskClasses = append(resp.TaskClasses, tcResp)
	}
	return &resp
}

func ProcessUserGetCompleteTaskClassRequest(taskClass *model.TaskClass) (*model.UserAddTaskClassRequest, error) {
	if taskClass == nil {
		return nil, errors.New("源数据对象不可为空")
	}
	// 1. 映射基础信息 (处理指针解引用)
	req := &model.UserAddTaskClassRequest{
		Name:      safeStr(taskClass.Name),
		Mode:      safeStr(taskClass.Mode),
		StartDate: formatTime(taskClass.StartDate),
		EndDate:   formatTime(taskClass.EndDate),
	}
	// 2. 映射配置信息 (Config Section)
	req.Config = model.UserAddTaskClassConfig{
		TotalSlots:        safeInt(taskClass.TotalSlots),
		AllowFillerCourse: safeBool(taskClass.AllowFillerCourse),
		Strategy:          safeStr(taskClass.Strategy),
	}
	/*// 3. 处理 ExcludedSlots JSON 字符串 -> []int
	if taskClass.ExcludedSlots != nil && *taskClass.ExcludedSlots != "" {
		var excluded []int
		// 直接使用标准反序列化，比手动处理 rune 字符要健壮得多
		if err := json.Unmarshal([]byte(*taskClass.ExcludedSlots), &excluded); err == nil {
			req.Config.ExcludedSlots = excluded
		}
	}*/
	req.Config.ExcludedSlots = taskClass.ExcludedSlots // 直接复用 IntSlice 类型，前端也能正确解析为 []int
	// 4. 映射子项信息 (Items Section)
	// 此时 items 已经通过 Preload 加载到了 taskClass.Items 中
	req.Items = make([]model.UserAddTaskClassItemRequest, 0, len(taskClass.Items))
	for _, item := range taskClass.Items {
		itemReq := model.UserAddTaskClassItemRequest{
			Order:        safeInt(item.Order),
			Content:      safeStr(item.Content),
			EmbeddedTime: item.EmbeddedTime, // 结构体指针直接复用
		}
		req.Items = append(req.Items, itemReq)
	}
	return req, nil
}

// UserInsertTaskItemRequestToModel 用于将填入空闲时段日程的请求转换为 Schedule 模型
func UserInsertTaskItemRequestToModel(req *model.UserInsertTaskClassItemToScheduleRequest, item *model.TaskClassItem, taskID *int, userID, startSection, endSection int) ([]model.Schedule, *model.ScheduleEvent, error) {
	var schedules []model.Schedule
	for section := startSection; section <= endSection; section++ {
		req1 := &model.Schedule{
			UserID:         userID,
			EmbeddedTaskID: taskID,
			Week:           req.Week,
			DayOfWeek:      req.DayOfWeek,
			Section:        section,
			Status:         "normal",
		}
		schedules = append(schedules, *req1)
	}
	startTime, endTime, err := RelativeTimeToRealTime(req.Week, req.DayOfWeek, startSection, endSection)
	if err != nil {
		return nil, nil, err
	}
	req2 := &model.ScheduleEvent{
		UserID:        userID,                // 由调用方填充
		Name:          safeStr(item.Content), // 任务内容作为事件名称
		Type:          "task",
		RelID:         &item.ID, // 关联到 TaskClassItem 的 ID
		CanBeEmbedded: false,    // 任务事件允许嵌入其他任务（如果需要的话）
		StartTime:     startTime,
		EndTime:       endTime,
	}
	return schedules, req2, nil
}

// --- 🛡️ 辅助工具函数：保持代码清爽并防止 Panic ---

func safeStr(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

func safeInt(i *int) int {
	if i == nil {
		return 0
	}
	return *i
}

func safeBool(b *bool) bool {
	if b == nil {
		return true
	}
	return *b
}

func formatTime(t *time.Time) string {
	if t == nil {
		return ""
	}
	// 务必使用 2006-01-02 格式以匹配前端校验
	return t.Format("2006-01-02")
}
