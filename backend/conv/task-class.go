package conv

import (
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
	//处理 ExcludedSlots 切片为 JSON 字符串
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
	}
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
