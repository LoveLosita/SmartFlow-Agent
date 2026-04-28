package newagenttools

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/LoveLosita/smartflow/backend/newAgent/tools/schedule"
)

// NewQueryAvailableSlotsToolHandler 为 query_available_slots 生成结构化读结果。
func NewQueryAvailableSlotsToolHandler() ToolHandler {
	return func(state *schedule.ScheduleState, args map[string]any) ToolExecutionResult {
		observation := schedule.QueryAvailableSlots(state, args)
		status, _ := resolveToolStatusAndSuccess(observation)
		if status != ToolStatusDone {
			return buildScheduleReadSimpleFailureResult("query_available_slots", args, state, observation)
		}

		payload, machinePayload := decodeAvailableSlotsPayload(observation)
		items := make([]map[string]any, 0, len(payload.Slots))
		for _, slot := range payload.Slots {
			tags := []string{
				fmt.Sprintf("第%d周", slot.Week),
				formatScheduleWeekdayCN(slot.DayOfWeek),
				formatSlotTypeLabelCN(slot.SlotType),
			}
			detailLines := []string{
				fmt.Sprintf("位置：%s", formatScheduleDaySlotCN(state, slot.Day, slot.SlotStart, slot.SlotEnd)),
				fmt.Sprintf("跨度：%d 节", slot.SlotEnd-slot.SlotStart+1),
			}
			if strings.Contains(strings.ToLower(strings.TrimSpace(slot.SlotType)), "embed") {
				if host := findScheduleHostTaskBySlotForRead(state, slot.Day, slot.SlotStart); host != nil {
					detailLines = append(detailLines, fmt.Sprintf(
						"宿主：[%d]%s｜%s",
						host.StateID,
						fallbackText(host.Name, "未命名任务"),
						formatScheduleTaskStatusCN(*host),
					))
				}
			}
			items = append(items, buildScheduleReadItem(
				formatScheduleDaySlotCN(state, slot.Day, slot.SlotStart, slot.SlotEnd),
				formatSlotTypeLabelCN(slot.SlotType),
				tags,
				detailLines,
				map[string]any{
					"day":         slot.Day,
					"week":        slot.Week,
					"day_of_week": slot.DayOfWeek,
					"slot_start":  slot.SlotStart,
					"slot_end":    slot.SlotEnd,
					"slot_type":   slot.SlotType,
				},
			))
		}

		metrics := []map[string]any{
			buildScheduleReadMetric("候选时段", fmt.Sprintf("%d 个", payload.Count)),
			buildScheduleReadMetric("纯空位", fmt.Sprintf("%d 个", payload.StrictCount)),
		}
		if payload.AllowEmbed {
			metrics = append(metrics, buildScheduleReadMetric("可嵌入候选", fmt.Sprintf("%d 个", payload.EmbeddedCount)))
		}

		sections := []map[string]any{
			buildScheduleReadKVSection("查询概况", []map[string]any{
				buildScheduleReadKV("查询跨度", fmt.Sprintf("%d 节连续时段", maxInt(payload.Span, 1))),
				buildScheduleReadKV("日期范围", formatDayScopeLabelCN(payload.DayScope)),
				buildScheduleReadKV("星期过滤", formatWeekdayListCN(payload.DayOfWeek)),
				buildScheduleReadKV("周次范围", buildWeekRangeLabelCN(payload.WeekFrom, payload.WeekTo, payload.WeekFilter)),
				buildScheduleReadKV("允许嵌入补位", formatBoolLabelCN(payload.AllowEmbed)),
				buildScheduleReadKV("排除节次", formatScheduleSectionListCN(payload.ExcludeSections)),
			}),
		}
		appendSectionIfPresent(&sections, buildScheduleReadArgsSection("筛选条件", LegacyResultWithState("query_available_slots", args, state, observation).ArgumentView))
		if len(items) > 0 {
			sections = append(sections, buildScheduleReadItemsSection("候选时段", items))
		} else {
			sections = append(sections, buildScheduleReadCalloutSection(
				"没有找到可用时段",
				"当前筛选条件下没有命中的候选落点。",
				"info",
				[]string{"可以调整周次、星期、节次范围或是否允许嵌入补位。"},
			))
		}

		title := fmt.Sprintf("找到 %d 个可用时段", payload.Count)
		if payload.Count == 0 {
			title = "未找到可用时段"
		}
		return buildScheduleReadResult(
			"query_available_slots",
			args,
			state,
			observation,
			ToolStatusDone,
			title,
			buildAvailableSlotsSubtitle(payload),
			metrics,
			items,
			sections,
			machinePayload,
		)
	}
}

// NewQueryRangeToolHandler 为 query_range 生成结构化读结果。
func NewQueryRangeToolHandler() ToolHandler {
	return func(state *schedule.ScheduleState, args map[string]any) ToolExecutionResult {
		day, ok := schedule.ArgsInt(args, "day")
		if !ok {
			return buildScheduleReadSimpleFailureResult("query_range", args, state, "查询失败：缺少必填参数 day。")
		}
		if state == nil {
			return buildScheduleReadSimpleFailureResult("query_range", args, nil, "查询失败：日程状态为空，无法读取时间范围。")
		}

		slotStart := schedule.ArgsIntPtr(args, "slot_start")
		slotEnd := schedule.ArgsIntPtr(args, "slot_end")
		observation := schedule.QueryRange(state, day, slotStart, slotEnd)
		status, _ := resolveToolStatusAndSuccess(observation)
		if status != ToolStatusDone {
			return buildScheduleReadSimpleFailureResult("query_range", args, state, observation)
		}
		if slotStart == nil || slotEnd == nil {
			return buildFullDayRangeReadResult(args, state, observation, day)
		}
		return buildSpecificRangeReadResult(args, state, observation, day, *slotStart, *slotEnd)
	}
}

func buildFullDayRangeReadResult(args map[string]any, state *schedule.ScheduleState, observation string, day int) ToolExecutionResult {
	totalOccupied := countScheduleDayOccupiedForRead(state, day)
	taskOccupied := countScheduleDayTaskOccupiedForRead(state, day)
	freeRanges := findScheduleFreeRangesOnDayForRead(state, day)

	bandItems := make([]map[string]any, 0, 6)
	for start := 1; start <= 11; start += 2 {
		end := start + 1
		occupants := listScheduleTasksInRangeForRead(state, day, start, end, true)
		detailLines := make([]string, 0, len(occupants))
		for _, occupant := range occupants {
			detailLines = append(detailLines, buildRangeOccupantLine(*occupant.Task))
		}
		subtitle := "空闲"
		tags := []string{"2 节"}
		if len(occupants) > 0 {
			subtitle = fmt.Sprintf("%d 个事项", len(occupants))
			tags = append(tags, "已占用")
		} else {
			tags = append(tags, "空闲")
			detailLines = append(detailLines, "这一段当前可直接安排任务。")
		}
		bandItems = append(bandItems, buildScheduleReadItem(
			formatScheduleSlotRangeCN(start, end),
			subtitle,
			tags,
			detailLines,
			map[string]any{
				"day":        day,
				"slot_start": start,
				"slot_end":   end,
			},
		))
	}

	freeItems := make([]map[string]any, 0, len(freeRanges))
	for _, freeRange := range freeRanges {
		freeItems = append(freeItems, buildScheduleReadItem(
			formatScheduleSlotRangeCN(freeRange.SlotStart, freeRange.SlotEnd),
			fmt.Sprintf("%d 节连续空闲", freeRange.SlotEnd-freeRange.SlotStart+1),
			[]string{"连续空闲"},
			[]string{fmt.Sprintf("位置：%s", formatScheduleDaySlotCN(state, day, freeRange.SlotStart, freeRange.SlotEnd))},
			map[string]any{
				"day":        day,
				"slot_start": freeRange.SlotStart,
				"slot_end":   freeRange.SlotEnd,
			},
		))
	}

	taskEntries := listScheduleTasksOnDayForRead(state, day, false)
	taskItems := make([]map[string]any, 0, len(taskEntries))
	for _, entry := range taskEntries {
		taskItems = append(taskItems, buildScheduleReadItem(
			fmt.Sprintf("[%d]%s", entry.Task.StateID, fallbackText(entry.Task.Name, "未命名任务")),
			formatScheduleTaskStatusCN(*entry.Task),
			[]string{fallbackText(entry.Task.Category, "未分类")},
			[]string{
				fmt.Sprintf("时段：%s", formatScheduleSlotRangeCN(entry.SlotStart, entry.SlotEnd)),
				fmt.Sprintf("来源：%s", formatScheduleTaskSourceCN(*entry.Task)),
			},
			map[string]any{
				"task_id":     entry.Task.StateID,
				"slot_start":  entry.SlotStart,
				"slot_end":    entry.SlotEnd,
				"task_status": entry.Task.Status,
			},
		))
	}

	sections := []map[string]any{
		buildScheduleReadKVSection("当日概况", []map[string]any{
			buildScheduleReadKV("总占用", fmt.Sprintf("%d/12 节", totalOccupied)),
			buildScheduleReadKV("任务占用", fmt.Sprintf("%d/12 节", taskOccupied)),
			buildScheduleReadKV("连续空闲段", fmt.Sprintf("%d 段", len(freeRanges))),
		}),
		buildScheduleReadItemsSection("时段分布", bandItems),
	}
	if len(freeItems) > 0 {
		sections = append(sections, buildScheduleReadItemsSection("连续空闲区", freeItems))
	}
	if embeddableItems := buildEmbeddableItemsForDay(state, day); len(embeddableItems) > 0 {
		sections = append(sections, buildScheduleReadItemsSection("可嵌入时段", embeddableItems))
	}
	if len(taskItems) > 0 {
		sections = append(sections, buildScheduleReadItemsSection("当日任务", taskItems))
	}
	appendSectionIfPresent(&sections, buildScheduleReadArgsSection("查询条件", LegacyResultWithState("query_range", args, state, observation).ArgumentView))

	return buildScheduleReadResult(
		"query_range",
		args,
		state,
		observation,
		ToolStatusDone,
		fmt.Sprintf("%s全日概况", formatScheduleDayCN(state, day)),
		fmt.Sprintf("已占用 %d/12 节，连续空闲 %d 段。", totalOccupied, len(freeRanges)),
		[]map[string]any{
			buildScheduleReadMetric("总占用", fmt.Sprintf("%d/12", totalOccupied)),
			buildScheduleReadMetric("任务占用", fmt.Sprintf("%d/12", taskOccupied)),
			buildScheduleReadMetric("空闲段", fmt.Sprintf("%d 段", len(freeRanges))),
		},
		bandItems,
		sections,
		map[string]any{
			"mode":                "full_day",
			"day":                 day,
			"occupied_slots":      totalOccupied,
			"task_occupied_slots": taskOccupied,
			"free_range_count":    len(freeRanges),
		},
	)
}

func buildSpecificRangeReadResult(args map[string]any, state *schedule.ScheduleState, observation string, day int, slotStart int, slotEnd int) ToolExecutionResult {
	total := slotEnd - slotStart + 1
	freeCount := 0
	slotItems := make([]map[string]any, 0, total)
	for section := slotStart; section <= slotEnd; section++ {
		occupants := listScheduleTasksInRangeForRead(state, day, section, section, true)
		detailLines := make([]string, 0, len(occupants))
		for _, occupant := range occupants {
			detailLines = append(detailLines, buildRangeOccupantLine(*occupant.Task))
		}
		subtitle := "空闲"
		tags := []string{"空闲"}
		if len(occupants) > 0 {
			subtitle = fmt.Sprintf("%d 个事项", len(occupants))
			tags = []string{"已占用"}
		} else {
			freeCount++
			detailLines = append(detailLines, "这一节当前为空。")
		}
		slotItems = append(slotItems, buildScheduleReadItem(
			fmt.Sprintf("第%d节", section),
			subtitle,
			tags,
			detailLines,
			map[string]any{
				"day":        day,
				"slot_start": section,
				"slot_end":   section,
			},
		))
	}

	seen := make(map[int]struct{})
	rangeTaskItems := make([]map[string]any, 0)
	for _, occupant := range listScheduleTasksInRangeForRead(state, day, slotStart, slotEnd, true) {
		if _, exists := seen[occupant.Task.StateID]; exists {
			continue
		}
		seen[occupant.Task.StateID] = struct{}{}
		rangeTaskItems = append(rangeTaskItems, buildScheduleReadItem(
			fmt.Sprintf("[%d]%s", occupant.Task.StateID, fallbackText(occupant.Task.Name, "未命名任务")),
			formatScheduleTaskStatusCN(*occupant.Task),
			[]string{fallbackText(occupant.Task.Category, "未分类")},
			[]string{
				fmt.Sprintf("覆盖范围：%s", formatScheduleDaySlotCN(state, day, occupant.SlotStart, occupant.SlotEnd)),
				fmt.Sprintf("来源：%s", formatScheduleTaskSourceCN(*occupant.Task)),
			},
			map[string]any{
				"task_id":     occupant.Task.StateID,
				"slot_start":  occupant.SlotStart,
				"slot_end":    occupant.SlotEnd,
				"task_status": occupant.Task.Status,
			},
		))
	}

	sections := []map[string]any{
		buildScheduleReadKVSection("范围概况", []map[string]any{
			buildScheduleReadKV("查询范围", formatScheduleSlotRangeCN(slotStart, slotEnd)),
			buildScheduleReadKV("总节数", fmt.Sprintf("%d 节", total)),
			buildScheduleReadKV("空闲节数", fmt.Sprintf("%d 节", freeCount)),
			buildScheduleReadKV("占用节数", fmt.Sprintf("%d 节", total-freeCount)),
		}),
		buildScheduleReadItemsSection("逐节情况", slotItems),
	}
	if len(rangeTaskItems) > 0 {
		sections = append(sections, buildScheduleReadItemsSection("范围内事项", rangeTaskItems))
	}
	appendSectionIfPresent(&sections, buildScheduleReadArgsSection("查询条件", LegacyResultWithState("query_range", args, state, observation).ArgumentView))

	return buildScheduleReadResult(
		"query_range",
		args,
		state,
		observation,
		ToolStatusDone,
		fmt.Sprintf("%s %s", formatScheduleDayCN(state, day), formatScheduleSlotRangeCN(slotStart, slotEnd)),
		fmt.Sprintf("共 %d 节，空闲 %d 节，占用 %d 节。", total, freeCount, total-freeCount),
		[]map[string]any{
			buildScheduleReadMetric("总节数", fmt.Sprintf("%d 节", total)),
			buildScheduleReadMetric("空闲", fmt.Sprintf("%d 节", freeCount)),
			buildScheduleReadMetric("事项", fmt.Sprintf("%d 个", len(rangeTaskItems))),
		},
		slotItems,
		sections,
		map[string]any{
			"mode":           "specific_range",
			"day":            day,
			"slot_start":     slotStart,
			"slot_end":       slotEnd,
			"free_count":     freeCount,
			"occupied_count": total - freeCount,
		},
	)
}

func decodeAvailableSlotsPayload(observation string) (scheduleReadAvailableSlotsPayload, map[string]any) {
	var payload scheduleReadAvailableSlotsPayload
	_ = json.Unmarshal([]byte(strings.TrimSpace(observation)), &payload)
	raw, _ := parseObservationJSON(strings.TrimSpace(observation))
	return payload, raw
}

func buildAvailableSlotsSubtitle(payload scheduleReadAvailableSlotsPayload) string {
	parts := []string{
		fmt.Sprintf("%d 节连续时段", maxInt(payload.Span, 1)),
		formatDayScopeLabelCN(payload.DayScope),
		buildWeekRangeLabelCN(payload.WeekFrom, payload.WeekTo, payload.WeekFilter),
	}
	if len(payload.DayOfWeek) > 0 {
		parts = append(parts, formatWeekdayListCN(payload.DayOfWeek))
	}
	if payload.AllowEmbed {
		parts = append(parts, "允许补充可嵌入候选")
	} else {
		parts = append(parts, "仅查看纯空位")
	}
	return strings.Join(parts, "，")
}

func buildEmbeddableItemsForDay(state *schedule.ScheduleState, day int) []map[string]any {
	if state == nil {
		return nil
	}
	items := make([]map[string]any, 0)
	for i := range state.Tasks {
		task := state.Tasks[i]
		if !task.CanEmbed || task.EmbeddedBy != nil || task.EmbedHost != nil {
			continue
		}
		for _, slot := range task.Slots {
			if slot.Day != day {
				continue
			}
			items = append(items, buildScheduleReadItem(
				formatScheduleSlotRangeCN(slot.SlotStart, slot.SlotEnd),
				fmt.Sprintf("可嵌入到 [%d]%s", task.StateID, fallbackText(task.Name, "未命名任务")),
				[]string{fallbackText(task.Category, "未分类"), formatScheduleTaskStatusCN(task)},
				[]string{
					fmt.Sprintf("宿主时段：%s", formatScheduleDaySlotCN(state, day, slot.SlotStart, slot.SlotEnd)),
					"该时段允许放入更短的嵌入任务。",
				},
				map[string]any{
					"host_task_id": task.StateID,
					"day":          day,
					"slot_start":   slot.SlotStart,
					"slot_end":     slot.SlotEnd,
				},
			))
		}
	}
	return items
}

func buildRangeOccupantLine(task schedule.ScheduleTask) string {
	return fmt.Sprintf(
		"[%d]%s｜%s｜%s",
		task.StateID,
		fallbackText(task.Name, "未命名任务"),
		formatScheduleTaskStatusCN(task),
		fallbackText(task.Category, "未分类"),
	)
}
