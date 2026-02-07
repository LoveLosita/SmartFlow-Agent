package conv

import (
	"fmt"

	"github.com/LoveLosita/smartflow/backend/model"
)

import "sort"

func SchedulesToScheduleConflictDetail(schedules []model.Schedule) []model.ScheduleConflictDetail {
	if len(schedules) == 0 {
		return []model.ScheduleConflictDetail{}
	}
	// 1. 使用 Map 进行逻辑分组
	// Key 格式: EventID-Week-Day (防止同一事件在不同天出现时被混为一谈)
	groups := make(map[string]*model.ScheduleConflictDetail)
	for _, s := range schedules {
		key := fmt.Sprintf("%d-%d-%d", s.EventID, s.Week, s.DayOfWeek)

		if _, ok := groups[key]; !ok {
			// 初始化该分组
			groups[key] = &model.ScheduleConflictDetail{
				EventID:   s.EventID,
				Name:      s.Event.Name,
				Location:  *s.Event.Location, // 假设字段是 *string
				Type:      s.Event.Type,
				Week:      s.Week,
				DayOfWeek: s.DayOfWeek,
			}
		}
		// 将当前节次加入数组
		groups[key].Sections = append(groups[key].Sections, s.Section)
	}

	// 2. 处理每个分组的区间逻辑
	res := make([]model.ScheduleConflictDetail, 0, len(groups))
	for _, detail := range groups {
		// 排序节次，例如把 [3, 1, 2] 变成 [1, 2, 3]
		sort.Ints(detail.Sections)

		// 最小值即起始，最大值即结束
		detail.StartSection = detail.Sections[0]
		detail.EndSection = detail.Sections[len(detail.Sections)-1]

		res = append(res, *detail)
	}

	// 3. 可选：对结果集按时间排序，让前端收到的 DTO 也是有序的
	sort.Slice(res, func(i, j int) bool {
		if res[i].Week != res[j].Week {
			return res[i].Week < res[j].Week
		}
		if res[i].DayOfWeek != res[j].DayOfWeek {
			return res[i].DayOfWeek < res[j].DayOfWeek
		}
		return res[i].StartSection < res[j].StartSection
	})

	return res
}
