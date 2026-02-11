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

// SectionToTime 映射表：将原子节次转为起始/结束时间点
// 此处以重邮为例
var sectionTimeMap = map[int][2]string{
	1: {"08:00", "08:45"}, 2: {"08:55", "09:40"},
	3: {"10:15", "11:00"}, 4: {"11:10", "11:55"},
	5: {"14:00", "14:45"}, 6: {"14:55", "15:40"},
	7: {"16:15", "17:00"}, 8: {"17:10", "17:55"},
	9: {"19:00", "19:45"}, 10: {"19:55", "20:40"},
	11: {"20:50", "21:35"}, 12: {"21:45", "22:30"},
}

func SchedulesToUserTodaySchedule(schedules []model.Schedule) []model.UserTodaySchedule {
	if len(schedules) == 0 {
		return []model.UserTodaySchedule{}
	}
	// 1. 数据预处理：按 Week-Day 分组
	dayGroups := make(map[string][]model.Schedule)
	for _, s := range schedules {
		dayKey := fmt.Sprintf("%d-%d", s.Week, s.DayOfWeek)
		dayGroups[dayKey] = append(dayGroups[dayKey], s)
	}
	var result []model.UserTodaySchedule
	for _, daySchedules := range dayGroups {
		todayDTO := model.UserTodaySchedule{
			Week:      daySchedules[0].Week,
			DayOfWeek: daySchedules[0].DayOfWeek,
			Events:    []model.EventBrief{},
		}
		// 💡 关键点：建立一个 Section 查找表，方便 O(1) 确定某节课是什么
		sectionMap := make(map[int]model.Schedule)
		for _, s := range daySchedules {
			sectionMap[s.Section] = s
		}
		order := 1
		// 💡 线性扫描：从第 1 节巡检到第 12 节
		for curr := 1; curr <= 12; {
			if slot, ok := sectionMap[curr]; ok {
				// === A 场景：当前节次有课 ===
				// 1. 寻找该事件的连续范围（比如 9-12 节连上）
				// 我们向后探测，直到 EventID 变化或节次断开
				end := curr
				for next := curr + 1; next <= 12; next++ {
					if nextSlot, exist := sectionMap[next]; exist && nextSlot.EventID == slot.EventID {
						end = next
					} else {
						break
					}
				}
				// 2. 封装 EventBrief
				brief := model.EventBrief{
					ID:        slot.EventID,
					Order:     order,
					Name:      slot.Event.Name,
					Location:  *slot.Event.Location,
					Type:      slot.Event.Type,
					StartTime: sectionTimeMap[curr][0],
					EndTime:   sectionTimeMap[end][1],
					Span:      end - curr + 1,
				}
				// 3. 处理嵌入任务
				// 只要这几个连续节次里有一个有任务，就带上
				for i := curr; i <= end; i++ {
					if s, exist := sectionMap[i]; exist && s.EmbeddedTask != nil {
						brief.EmbeddedTaskInfo = model.TaskBrief{
							ID:   s.EmbeddedTask.ID,
							Name: *s.EmbeddedTask.Content,
							Type: "task",
						}
						break
					}
				}
				todayDTO.Events = append(todayDTO.Events, brief)
				// 💡 指针跳跃：直接跳过已处理的节次
				curr = end + 1
				order++
			} else {
				// === B 场景：当前节次没课（Type = "empty"） ===
				// 逻辑：按照学校标准大节（1-2, 3-4...）进行空位合并
				// 如果当前是奇数节（1, 3, 5...）且下一节也没课，就合并成一个空块
				emptyEnd := curr
				if curr%2 != 0 && curr < 12 {
					if _, nextHasClass := sectionMap[curr+1]; !nextHasClass {
						emptyEnd = curr + 1
					}
				}
				todayDTO.Events = append(todayDTO.Events, model.EventBrief{
					ID:        0, // 空课 ID 为 0
					Order:     order,
					Name:      "无课",
					Type:      "empty",
					StartTime: sectionTimeMap[curr][0],
					EndTime:   sectionTimeMap[emptyEnd][1],
					Location:  "休息时间",
				})
				curr = emptyEnd + 1
				order++
			}
		}
		result = append(result, todayDTO)
	}
	return result
}

func SchedulesToUserWeeklySchedule(schedules []model.Schedule) []model.UserWeekSchedule {
	if len(schedules) == 0 {
		return []model.UserWeekSchedule{}
	}
	// 1. 数据预处理：按 Week 分组
	weekGroups := make(map[int][]model.Schedule)
	for _, s := range schedules {
		weekGroups[s.Week] = append(weekGroups[s.Week], s)
	}
	var result []model.UserWeekSchedule
	// 2. 遍历每一个周
	for week, weekSchedules := range weekGroups {
		weekDTO := model.UserWeekSchedule{
			Week:   week,
			Events: []model.WeeklyEventBrief{},
		}
		// 💡 核心优化：建立 [天][节次] 的快速索引地图
		// indexMap[day][section] -> model.Schedule
		indexMap := make(map[int]map[int]model.Schedule)
		for d := 1; d <= 7; d++ {
			indexMap[d] = make(map[int]model.Schedule)
		}
		for _, s := range weekSchedules {
			indexMap[s.DayOfWeek][s.Section] = s
		}
		// 3. 线性扫描 1-7 天
		for day := 1; day <= 7; day++ {
			order := 1 // 每一天开始时，Order 重置
			// 4. 线性扫描 1-12 节
			for curr := 1; curr <= 12; {
				// 检查当前槽位是否有课
				if slot, hasClass := indexMap[day][curr]; hasClass {
					// === A 场景：有课，寻找连续边界 (Span 计算) ===
					end := curr
					// 探测逻辑：只要 EventID 相同且在同一天，就视为同一个块
					for next := curr + 1; next <= 12; next++ {
						if nextSlot, exist := indexMap[day][next]; exist && nextSlot.EventID == slot.EventID {
							end = next
						} else {
							break
						}
					}
					span := end - curr + 1
					brief := model.WeeklyEventBrief{
						ID:        slot.EventID,
						Order:     order,
						DayOfWeek: day,
						Name:      slot.Event.Name,
						Location:  *slot.Event.Location,
						Type:      slot.Event.Type,
						StartTime: sectionTimeMap[curr][0],
						EndTime:   sectionTimeMap[end][1],
						Span:      span,
					}
					// 提取嵌入任务信息 (如果有)
					for i := curr; i <= end; i++ {
						if s, exist := indexMap[day][i]; exist && s.EmbeddedTask != nil {
							brief.EmbeddedTaskInfo = model.TaskBrief{
								ID:   s.EmbeddedTask.ID,
								Name: *s.EmbeddedTask.Content,
								Type: "task",
							}
							break
						}
					}
					weekDTO.Events = append(weekDTO.Events, brief)
					curr = end + 1 // 指针跳跃
					order++
				} else {
					// === B 场景：无课 (Type="empty") ===
					// 逻辑：默认按“大节”合并空位（1-2, 3-4...）
					emptyEnd := curr
					// 如果是奇数节且下一节也没课，则合并为一个 2 节的大空块
					if curr%2 != 0 && curr < 12 {
						if _, nextHasClass := indexMap[day][curr+1]; !nextHasClass {
							emptyEnd = curr + 1
						}
					}
					weekDTO.Events = append(weekDTO.Events, model.WeeklyEventBrief{
						ID:        0,
						Order:     order,
						DayOfWeek: day,
						Name:      "无课",
						Type:      "empty",
						StartTime: sectionTimeMap[curr][0],
						EndTime:   sectionTimeMap[emptyEnd][1],
						Span:      emptyEnd - curr + 1,
						Location:  "",
					})
					curr = emptyEnd + 1
					order++
				}
			}
		}
		result = append(result, weekDTO)
	}
	return result
}

func SchedulesToRecentCompletedSchedules(schedules []model.Schedule) model.UserRecentCompletedScheduleResponse {
	var result model.UserRecentCompletedScheduleResponse
	for _, s := range schedules {
		strTime := s.Event.EndTime.Format("2006-01-02 15:04:05")
		temp := model.RecentCompletedEventBrief{
			ID:   s.ID,
			Name: s.Event.Name,
			Type: s.Event.Type,
			// CompletedTime 需要从 ScheduleEvent 的 CompletedTime 字段获取
			CompletedTime: strTime,
		}
		result.Events = append(result.Events, temp)
	}
	return result

}
