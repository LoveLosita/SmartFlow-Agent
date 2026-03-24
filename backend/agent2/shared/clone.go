package agentshared

import "github.com/LoveLosita/smartflow/backend/model"

// CloneWeekSchedules 深拷贝周视图排程结果。
//
// 职责边界：
// 1. 负责断开 []UserWeekSchedule 与内部 Events 切片的引用共享；
// 2. 负责服务于“缓存 DTO / graph state / API 响应”之间的安全复制；
// 3. 不负责业务过滤，不负责排序。
func CloneWeekSchedules(src []model.UserWeekSchedule) []model.UserWeekSchedule {
	if len(src) == 0 {
		return nil
	}

	dst := make([]model.UserWeekSchedule, 0, len(src))
	for _, week := range src {
		eventsCopy := make([]model.WeeklyEventBrief, len(week.Events))
		copy(eventsCopy, week.Events)
		dst = append(dst, model.UserWeekSchedule{
			Week:   week.Week,
			Events: eventsCopy,
		})
	}
	return dst
}

// CloneHybridEntries 深拷贝混合排程条目切片。
func CloneHybridEntries(src []model.HybridScheduleEntry) []model.HybridScheduleEntry {
	if len(src) == 0 {
		return nil
	}
	dst := make([]model.HybridScheduleEntry, len(src))
	copy(dst, src)
	return dst
}

// CloneTaskClassItems 深拷贝任务块切片。
//
// 这里不能直接 copy：
// 1. 因为 TaskClassItem 内部带若干指针字段；
// 2. 如果浅拷贝，后续某一步修改 EmbeddedTime / Status，会污染原状态；
// 3. 排程 graph 连续微调时，这种共享引用会非常难查，所以必须在公共层兜住。
func CloneTaskClassItems(src []model.TaskClassItem) []model.TaskClassItem {
	if len(src) == 0 {
		return nil
	}

	dst := make([]model.TaskClassItem, 0, len(src))
	for _, item := range src {
		copied := item
		if item.CategoryID != nil {
			v := *item.CategoryID
			copied.CategoryID = &v
		}
		if item.Order != nil {
			v := *item.Order
			copied.Order = &v
		}
		if item.Content != nil {
			v := *item.Content
			copied.Content = &v
		}
		if item.Status != nil {
			v := *item.Status
			copied.Status = &v
		}
		if item.EmbeddedTime != nil {
			t := *item.EmbeddedTime
			copied.EmbeddedTime = &t
		}
		dst = append(dst, copied)
	}
	return dst
}

// CloneInts 深拷贝 int 切片。
func CloneInts(src []int) []int {
	if len(src) == 0 {
		return nil
	}
	dst := make([]int, len(src))
	copy(dst, src)
	return dst
}

// CloneStrings 深拷贝 string 切片。
func CloneStrings(src []string) []string {
	if len(src) == 0 {
		return nil
	}
	dst := make([]string, len(src))
	copy(dst, src)
	return dst
}
