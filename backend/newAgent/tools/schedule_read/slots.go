package schedule_read

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/LoveLosita/smartflow/backend/newAgent/tools/schedule"
)

// BuildAvailableSlotsView 构造 query_available_slots 的纯展示视图。
func BuildAvailableSlotsView(input AvailableSlotsViewInput) ReadResultView {
	payload, machinePayload, ok := DecodeAvailableSlotsPayload(input.Observation)
	if !ok || !payload.Success {
		return BuildFailureView(BuildFailureViewInput{
			ToolName:    "query_available_slots",
			Observation: input.Observation,
			ArgFields:   input.ArgFields,
		})
	}

	items := make([]ItemView, 0, len(payload.Slots))
	for _, slot := range payload.Slots {
		tags := []string{
			fmt.Sprintf("第%d周", slot.Week),
			formatScheduleWeekdayCN(slot.DayOfWeek),
			formatSlotTypeLabelCN(slot.SlotType),
		}
		detailLines := []string{
			fmt.Sprintf("位置：%s", formatScheduleDaySlotCN(input.State, slot.Day, slot.SlotStart, slot.SlotEnd)),
			fmt.Sprintf("跨度：%d 节", slot.SlotEnd-slot.SlotStart+1),
		}
		if strings.Contains(strings.ToLower(strings.TrimSpace(slot.SlotType)), "embed") {
			if host := findScheduleHostTaskBySlotForRead(input.State, slot.Day, slot.SlotStart); host != nil {
				detailLines = append(detailLines, fmt.Sprintf(
					"宿主：[%d]%s，%s",
					host.StateID,
					fallbackText(host.Name, "未命名任务"),
					formatScheduleTaskStatusCN(*host),
				))
			}
		}
		items = append(items, BuildItem(
			formatScheduleDaySlotCN(input.State, slot.Day, slot.SlotStart, slot.SlotEnd),
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

	metrics := []MetricField{
		BuildMetric("候选时段", fmt.Sprintf("%d 个", payload.Count)),
		BuildMetric("纯空位", fmt.Sprintf("%d 个", payload.StrictCount)),
	}
	if payload.AllowEmbed {
		metrics = append(metrics, BuildMetric("可嵌入候选", fmt.Sprintf("%d 个", payload.EmbeddedCount)))
	}

	sections := []map[string]any{
		BuildKVSection("查询概况", []KVField{
			BuildKVField("查询跨度", fmt.Sprintf("%d 节连续时段", maxInt(payload.Span, 1))),
			BuildKVField("日期范围", formatDayScopeLabelCN(payload.DayScope)),
			BuildKVField("星期过滤", formatWeekdayListCN(payload.DayOfWeek)),
			BuildKVField("周次范围", buildWeekRangeLabelCN(payload.WeekFrom, payload.WeekTo, payload.WeekFilter)),
			BuildKVField("允许嵌入补位", formatBoolLabelCN(payload.AllowEmbed)),
			BuildKVField("排除节次", formatScheduleSectionListCN(payload.ExcludeSections)),
		}),
	}
	appendSectionIfPresent(&sections, BuildArgsSection("筛选条件", input.ArgFields))
	if len(items) > 0 {
		sections = append(sections, BuildItemsSection("候选时段", items))
	} else {
		sections = append(sections, BuildCalloutSection(
			"没有找到可用时段",
			"当前筛选条件下没有命中的候选落点。",
			"info",
			[]string{"可以调整周次、星期、节次范围，或修改是否允许嵌入补位。"},
		))
	}

	title := fmt.Sprintf("找到 %d 个可用时段", payload.Count)
	if payload.Count == 0 {
		title = "未找到可用时段"
	}
	return BuildResultView(BuildResultViewInput{
		Status:         StatusDone,
		Title:          title,
		Subtitle:       buildAvailableSlotsSubtitle(payload),
		Metrics:        metrics,
		Items:          items,
		Sections:       sections,
		Observation:    input.Observation,
		MachinePayload: machinePayload,
	})
}

// BuildRangeView 根据是否传入 slot_start / slot_end 选择整天或指定范围视图。
func BuildRangeView(input RangeViewInput) ReadResultView {
	if input.State == nil {
		return BuildFailureView(BuildFailureViewInput{
			ToolName:    "query_range",
			Observation: input.Observation,
			ArgFields:   input.ArgFields,
		})
	}
	if input.SlotStart == nil || input.SlotEnd == nil {
		return BuildRangeFullDayView(RangeFullDayViewInput{
			State:       input.State,
			Observation: input.Observation,
			Day:         input.Day,
			ArgFields:   input.ArgFields,
		})
	}
	return BuildRangeSpecificView(RangeSpecificViewInput{
		State:       input.State,
		Observation: input.Observation,
		Day:         input.Day,
		SlotStart:   *input.SlotStart,
		SlotEnd:     *input.SlotEnd,
		ArgFields:   input.ArgFields,
	})
}

// BuildRangeFullDayView 构造 query_range 整天模式视图。
func BuildRangeFullDayView(input RangeFullDayViewInput) ReadResultView {
	if input.State == nil {
		return BuildFailureView(BuildFailureViewInput{
			ToolName:    "query_range",
			Observation: input.Observation,
			ArgFields:   input.ArgFields,
		})
	}

	totalOccupied := countScheduleDayOccupiedForRead(input.State, input.Day)
	taskOccupied := countScheduleDayTaskOccupiedForRead(input.State, input.Day)
	freeRanges := findScheduleFreeRangesOnDayForRead(input.State, input.Day)

	bandItems := make([]ItemView, 0, 6)
	for start := 1; start <= 11; start += 2 {
		end := start + 1
		occupants := listScheduleTasksInRangeForRead(input.State, input.Day, start, end, true)
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
		bandItems = append(bandItems, BuildItem(
			formatScheduleSlotRangeCN(start, end),
			subtitle,
			tags,
			detailLines,
			map[string]any{
				"day":        input.Day,
				"slot_start": start,
				"slot_end":   end,
			},
		))
	}

	freeItems := make([]ItemView, 0, len(freeRanges))
	for _, freeRange := range freeRanges {
		freeItems = append(freeItems, BuildItem(
			formatScheduleSlotRangeCN(freeRange.SlotStart, freeRange.SlotEnd),
			fmt.Sprintf("%d 节连续空闲", freeRange.SlotEnd-freeRange.SlotStart+1),
			[]string{"连续空闲"},
			[]string{fmt.Sprintf("位置：%s", formatScheduleDaySlotCN(input.State, input.Day, freeRange.SlotStart, freeRange.SlotEnd))},
			map[string]any{
				"day":        input.Day,
				"slot_start": freeRange.SlotStart,
				"slot_end":   freeRange.SlotEnd,
			},
		))
	}

	taskEntries := listScheduleTasksOnDayForRead(input.State, input.Day, false)
	taskItems := make([]ItemView, 0, len(taskEntries))
	for _, entry := range taskEntries {
		taskItems = append(taskItems, BuildItem(
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
		BuildKVSection("当日概况", []KVField{
			BuildKVField("总占用", fmt.Sprintf("%d/12 节", totalOccupied)),
			BuildKVField("任务占用", fmt.Sprintf("%d/12 节", taskOccupied)),
			BuildKVField("连续空闲段", fmt.Sprintf("%d 段", len(freeRanges))),
		}),
		BuildItemsSection("时段分布", bandItems),
	}
	if len(freeItems) > 0 {
		sections = append(sections, BuildItemsSection("连续空闲区", freeItems))
	}
	if embeddableItems := buildEmbeddableItemsForDay(input.State, input.Day); len(embeddableItems) > 0 {
		sections = append(sections, BuildItemsSection("可嵌入时段", embeddableItems))
	}
	if len(taskItems) > 0 {
		sections = append(sections, BuildItemsSection("当日任务", taskItems))
	}
	appendSectionIfPresent(&sections, BuildArgsSection("查询条件", input.ArgFields))

	return BuildResultView(BuildResultViewInput{
		Status:   StatusDone,
		Title:    fmt.Sprintf("%s全日概况", formatScheduleDayCN(input.State, input.Day)),
		Subtitle: fmt.Sprintf("已占用 %d/12 节，连续空闲 %d 段。", totalOccupied, len(freeRanges)),
		Metrics: []MetricField{
			BuildMetric("总占用", fmt.Sprintf("%d/12", totalOccupied)),
			BuildMetric("任务占用", fmt.Sprintf("%d/12", taskOccupied)),
			BuildMetric("空闲段", fmt.Sprintf("%d 段", len(freeRanges))),
		},
		Items:       bandItems,
		Sections:    sections,
		Observation: input.Observation,
		MachinePayload: map[string]any{
			"mode":                "full_day",
			"day":                 input.Day,
			"occupied_slots":      totalOccupied,
			"task_occupied_slots": taskOccupied,
			"free_range_count":    len(freeRanges),
		},
	})
}

// BuildRangeSpecificView 构造 query_range 指定范围模式视图。
func BuildRangeSpecificView(input RangeSpecificViewInput) ReadResultView {
	if input.State == nil {
		return BuildFailureView(BuildFailureViewInput{
			ToolName:    "query_range",
			Observation: input.Observation,
			ArgFields:   input.ArgFields,
		})
	}

	total := input.SlotEnd - input.SlotStart + 1
	freeCount := 0
	slotItems := make([]ItemView, 0, total)
	for section := input.SlotStart; section <= input.SlotEnd; section++ {
		occupants := listScheduleTasksInRangeForRead(input.State, input.Day, section, section, true)
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
		slotItems = append(slotItems, BuildItem(
			fmt.Sprintf("第%d节", section),
			subtitle,
			tags,
			detailLines,
			map[string]any{
				"day":        input.Day,
				"slot_start": section,
				"slot_end":   section,
			},
		))
	}

	seen := make(map[int]struct{})
	rangeTaskItems := make([]ItemView, 0)
	for _, occupant := range listScheduleTasksInRangeForRead(input.State, input.Day, input.SlotStart, input.SlotEnd, true) {
		if _, exists := seen[occupant.Task.StateID]; exists {
			continue
		}
		seen[occupant.Task.StateID] = struct{}{}
		rangeTaskItems = append(rangeTaskItems, BuildItem(
			fmt.Sprintf("[%d]%s", occupant.Task.StateID, fallbackText(occupant.Task.Name, "未命名任务")),
			formatScheduleTaskStatusCN(*occupant.Task),
			[]string{fallbackText(occupant.Task.Category, "未分类")},
			[]string{
				fmt.Sprintf("覆盖范围：%s", formatScheduleDaySlotCN(input.State, input.Day, occupant.SlotStart, occupant.SlotEnd)),
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
		BuildKVSection("范围概况", []KVField{
			BuildKVField("查询范围", formatScheduleSlotRangeCN(input.SlotStart, input.SlotEnd)),
			BuildKVField("总节数", fmt.Sprintf("%d 节", total)),
			BuildKVField("空闲节数", fmt.Sprintf("%d 节", freeCount)),
			BuildKVField("占用节数", fmt.Sprintf("%d 节", total-freeCount)),
		}),
		BuildItemsSection("逐节情况", slotItems),
	}
	if len(rangeTaskItems) > 0 {
		sections = append(sections, BuildItemsSection("范围内事项", rangeTaskItems))
	}
	appendSectionIfPresent(&sections, BuildArgsSection("查询条件", input.ArgFields))

	return BuildResultView(BuildResultViewInput{
		Status:   StatusDone,
		Title:    fmt.Sprintf("%s %s", formatScheduleDayCN(input.State, input.Day), formatScheduleSlotRangeCN(input.SlotStart, input.SlotEnd)),
		Subtitle: fmt.Sprintf("共 %d 节，空闲 %d 节，占用 %d 节。", total, freeCount, total-freeCount),
		Metrics: []MetricField{
			BuildMetric("总节数", fmt.Sprintf("%d 节", total)),
			BuildMetric("空闲", fmt.Sprintf("%d 节", freeCount)),
			BuildMetric("事项", fmt.Sprintf("%d 个", len(rangeTaskItems))),
		},
		Items:       slotItems,
		Sections:    sections,
		Observation: input.Observation,
		MachinePayload: map[string]any{
			"mode":           "specific_range",
			"day":            input.Day,
			"slot_start":     input.SlotStart,
			"slot_end":       input.SlotEnd,
			"free_count":     freeCount,
			"occupied_count": total - freeCount,
		},
	})
}

// DecodeAvailableSlotsPayload 解析 query_available_slots 的 JSON observation。
func DecodeAvailableSlotsPayload(observation string) (AvailableSlotsPayload, map[string]any, bool) {
	var payload AvailableSlotsPayload
	trimmed := strings.TrimSpace(observation)
	if trimmed == "" {
		return payload, nil, false
	}
	if err := json.Unmarshal([]byte(trimmed), &payload); err != nil {
		return payload, nil, false
	}
	raw, ok := parseObservationJSON(trimmed)
	return payload, raw, ok
}

func buildAvailableSlotsSubtitle(payload AvailableSlotsPayload) string {
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

func buildEmbeddableItemsForDay(state *schedule.ScheduleState, day int) []ItemView {
	if state == nil {
		return nil
	}
	items := make([]ItemView, 0)
	for i := range state.Tasks {
		task := state.Tasks[i]
		if !task.CanEmbed || task.EmbeddedBy != nil || task.EmbedHost != nil {
			continue
		}
		for _, slot := range task.Slots {
			if slot.Day != day {
				continue
			}
			items = append(items, BuildItem(
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
		"[%d]%s，%s，%s",
		task.StateID,
		fallbackText(task.Name, "未命名任务"),
		formatScheduleTaskStatusCN(task),
		fallbackText(task.Category, "未分类"),
	)
}
