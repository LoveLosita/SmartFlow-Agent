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

func SchedulesToUserWeeklySchedule(schedules []model.Schedule) *model.UserWeekSchedule {
	if len(schedules) == 0 {
		return &model.UserWeekSchedule{
			Week:   0,
			Events: []model.WeeklyEventBrief{},
		}
	}
	// 1. 初始化返回结构 (默认取第一条数据的周次)
	weekDTO := &model.UserWeekSchedule{
		Week:   schedules[0].Week,
		Events: []model.WeeklyEventBrief{},
	}

	// 2. 建立 [天][节次] 的快速索引地图
	// indexMap[day][section] -> model.Schedule
	indexMap := make(map[int]map[int]model.Schedule)
	for d := 1; d <= 7; d++ {
		indexMap[d] = make(map[int]model.Schedule)
	}
	for _, s := range schedules {
		indexMap[s.DayOfWeek][s.Section] = s
	}

	// 3. 线性扫描 1-7 天
	for day := 1; day <= 7; day++ {
		order := 1 // 每一天开始时，内部显示顺序重置

		// 4. 线性扫描 1-12 节
		for curr := 1; curr <= 12; {
			// 场景 A：当前槽位有课/有任务
			if slot, hasClass := indexMap[day][curr]; hasClass {
				end := curr
				// 探测逻辑：合并相同 EventID 的连续节次 (Span 计算)
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
					StartTime: sectionTimeMap[curr][0], // 使用你定义的映射表
					EndTime:   sectionTimeMap[end][1],
					Span:      span,
				}

				// 提取嵌入任务信息 (逻辑同前，探测整个 Span)
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
				curr = end + 1 // 指针跳跃到下一块
				order++
			} else {
				// 场景 B：无课 (Type="empty")，进行逻辑合并
				emptyEnd := curr
				// 奇数节起步且下一节也空，则合并为大节 (1-2, 3-4...)
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

	return weekDTO
}

func SchedulesToRecentCompletedSchedules(schedules []model.Schedule) *model.UserRecentCompletedScheduleResponse {
	// 1. 初始化结果集，确保即使为空也返回空数组而非 nil
	result := &model.UserRecentCompletedScheduleResponse{
		Events: make([]model.RecentCompletedEventBrief, 0),
	}

	if len(schedules) == 0 {
		return result
	}

	// 💡 核心：去重地图，key 是 EventID
	seen := make(map[int]bool)

	for _, s := range schedules {
		// 2. 检查这个逻辑事件（课程或任务块）是否已经处理过
		if seen[s.EventID] {
			continue
		}

		// 3. 确定显示的“名分”和“类型”
		displayName := s.Event.Name
		displayType := s.Event.Type

		// 🚀 关键逻辑：如果存在嵌入任务，则“鸠占鹊巢”
		// 即使载体是 course，只要里面塞了任务，我们就对外宣称这是一个 task
		if s.EmbeddedTask != nil && s.EmbeddedTask.Content != nil {
			displayName = *s.EmbeddedTask.Content
			displayType = "embedded_task"
		}

		// 4. 格式化结束时间 (即完成时间)
		strTime := s.Event.EndTime.Format("2006-01-02 15:04:05")

		// 5. 构造 Brief
		temp := model.RecentCompletedEventBrief{
			// ID 统一使用 EventID，确保唯一性且方便前端追踪逻辑块
			ID:            s.EventID,
			Name:          displayName,
			Type:          displayType,
			CompletedTime: strTime,
		}

		result.Events = append(result.Events, temp)

		// 6. 标记该事件已处理
		seen[s.EventID] = true
	}

	return result
}

func SchedulesToUserOngoingSchedule(schedules []model.Schedule) *model.OngoingSchedule {
	if len(schedules) == 0 {
		return nil
	}
	//取第一个 Schedule 的 Event 作为正在进行的事件
	ongoing := schedules[0]
	return &model.OngoingSchedule{
		ID:        ongoing.EventID,
		Name:      ongoing.Event.Name,
		Type:      ongoing.Event.Type,
		Location:  *ongoing.Event.Location,
		StartTime: ongoing.Event.StartTime,
		EndTime:   ongoing.Event.EndTime,
	}
}

// 这里我们使用一个临时的内部结构来兼容“实日程”和“虚计划”
type slotInfo struct {
	schedule *model.Schedule
	plan     *model.TaskClassItem
}

func PlanningResultToUserWeekSchedules(userSchedule []model.Schedule, plans []model.TaskClassItem) []model.UserWeekSchedule {
	// 1. 周次范围探测与数据分桶 (保持高效的 O(N) 复杂度)
	minW, maxW := 25, 1
	weekMap := make(map[int][]model.Schedule)
	for _, s := range userSchedule {
		if s.Week < minW {
			minW = s.Week
		}
		if s.Week > maxW {
			maxW = s.Week
		}
		weekMap[s.Week] = append(weekMap[s.Week], s)
	}

	planMap := make(map[int][]model.TaskClassItem)
	for _, p := range plans {
		if p.EmbeddedTime == nil {
			continue
		}
		w := p.EmbeddedTime.Week
		if w < minW {
			minW = w
		}
		if w > maxW {
			maxW = w
		}
		planMap[w] = append(planMap[w], p)
	}

	var results []model.UserWeekSchedule

	for w := minW; w <= maxW; w++ {
		// 构建当前周的逻辑网格
		indexMap := make(map[int]map[int]slotInfo)
		for d := 1; d <= 7; d++ {
			indexMap[d] = make(map[int]slotInfo)
		}

		for _, s := range weekMap[w] {
			indexMap[s.DayOfWeek][s.Section] = slotInfo{schedule: &s}
		}
		for _, p := range planMap[w] {
			for sec := p.EmbeddedTime.SectionFrom; sec <= p.EmbeddedTime.SectionTo; sec++ {
				info := indexMap[p.EmbeddedTime.DayOfWeek][sec]
				info.plan = &p
				indexMap[p.EmbeddedTime.DayOfWeek][sec] = info
			}
		}

		weekDTO := &model.UserWeekSchedule{Week: w, Events: []model.WeeklyEventBrief{}}

		for day := 1; day <= 7; day++ {
			order := 1
			for curr := 1; curr <= 12; {
				slot := indexMap[day][curr]

				if slot.schedule != nil || slot.plan != nil {
					end := curr
					// 🚀 修复逻辑 A：精准探测合并边界
					for next := curr + 1; next <= 12; next++ {
						nextSlot := indexMap[day][next]
						isSame := false

						if slot.schedule != nil && nextSlot.schedule != nil {
							// 场景：都是课，且是同一门课
							isSame = slot.schedule.EventID == nextSlot.schedule.EventID
						} else if slot.schedule == nil && nextSlot.schedule == nil && slot.plan != nil && nextSlot.plan != nil {
							// 场景：都是新排任务，且是同一个 TaskItem (修复了之前会合并不同任务的 Bug)
							isSame = slot.plan.ID == nextSlot.plan.ID
						}

						if isSame {
							end = next
						} else {
							break
						}
					}

					// 🚀 修复逻辑 B：直接计算 span 并传值，消除重复计算
					span := end - curr + 1
					brief := buildBrief(slot, day, curr, end, span, order)

					weekDTO.Events = append(weekDTO.Events, brief)
					curr = end + 1
					order++
				} else {
					// 场景 B：留空处理 (逻辑保持原子化)
					emptyEnd := curr
					if curr%2 != 0 && curr < 12 {
						if next := indexMap[day][curr+1]; next.schedule == nil && next.plan == nil {
							emptyEnd = curr + 1
						}
					}
					weekDTO.Events = append(weekDTO.Events, model.WeeklyEventBrief{
						Name: "无课", Type: "empty", DayOfWeek: day, Order: order,
						StartTime: sectionTimeMap[curr][0], EndTime: sectionTimeMap[emptyEnd][1],
						Span: emptyEnd - curr + 1,
					})
					curr = emptyEnd + 1
					order++
				}
			}
		}
		results = append(results, *weekDTO)
	}
	return results
}

func buildBrief(slot slotInfo, day, start, end, span, order int) model.WeeklyEventBrief {
	brief := model.WeeklyEventBrief{
		DayOfWeek: day,
		Order:     order,
		StartTime: sectionTimeMap[start][0],
		EndTime:   sectionTimeMap[end][1],
		Span:      span,
		Status:    "normal", // 默认设为正常状态
	}

	if slot.schedule != nil {
		// 场景 A：它是数据库里原有的课 (实日程)
		brief.ID = slot.schedule.EventID
		brief.Name = slot.schedule.Event.Name
		brief.Location = *slot.schedule.Event.Location
		brief.Type = slot.schedule.Event.Type

		// 如果这节课里被算法“塞”进了一个计划任务
		if slot.plan != nil {
			brief.Status = "suggested" // 标记为建议状态，前端据此高亮整块
			brief.EmbeddedTaskInfo = model.TaskBrief{
				ID:   slot.plan.ID,
				Name: *slot.plan.Content,
				Type: "task",
			}
		}
	} else if slot.plan != nil {
		// 场景 B：它是算法在空地新建的任务块 (虚日程)
		brief.Name = *slot.plan.Content
		brief.Type = "task"
		brief.Status = "suggested" // 标记为建议状态
	}

	return brief
}
