package newagenttools

import (
	"fmt"
	"sort"
	"strings"

	"github.com/LoveLosita/smartflow/backend/newAgent/tools/schedule"
)

// buildScheduleReadResult 统一封装第二批 read 工具的结构化返回。
//
// 职责边界：
// 1. 负责保留原始 ObservationText，确保 LLM 看到的 observation 不变。
// 2. 负责把各工具已经算好的中文标题、指标、分区组装成统一的 ResultView。
// 3. 不负责具体业务解释，具体内容由各 read handler 先计算后传入。
func buildScheduleReadResult(
	toolName string,
	args map[string]any,
	state *schedule.ScheduleState,
	observation string,
	status string,
	title string,
	subtitle string,
	metrics []map[string]any,
	items []map[string]any,
	sections []map[string]any,
	machinePayload map[string]any,
) ToolExecutionResult {
	result := LegacyResultWithState(toolName, args, state, observation)

	normalizedStatus := normalizeToolStatus(status)
	if normalizedStatus == "" {
		normalizedStatus = ToolStatusDone
	}
	if metrics == nil {
		metrics = make([]map[string]any, 0)
	}
	if items == nil {
		items = make([]map[string]any, 0)
	}
	if sections == nil {
		sections = make([]map[string]any, 0)
	}

	expanded := map[string]any{
		"items":    items,
		"sections": sections,
		"raw_text": strings.TrimSpace(observation),
	}
	if len(machinePayload) > 0 {
		expanded["machine_payload"] = cloneAnyMap(machinePayload)
	}

	result.Status = normalizedStatus
	result.Success = normalizedStatus == ToolStatusDone
	result.Summary = strings.TrimSpace(title)
	result.ResultView = &ToolDisplayView{
		ViewType: scheduleReadResultViewType,
		Version:  1,
		Collapsed: map[string]any{
			"title":        strings.TrimSpace(title),
			"subtitle":     strings.TrimSpace(subtitle),
			"status":       normalizedStatus,
			"status_label": resolveToolStatusLabelCN(normalizedStatus),
			"metrics":      metrics,
		},
		Expanded: expanded,
	}
	if !result.Success {
		errorCode, errorMessage := extractToolErrorInfo(observation, normalizedStatus)
		if strings.TrimSpace(result.ErrorCode) == "" {
			result.ErrorCode = errorCode
		}
		if strings.TrimSpace(result.ErrorMessage) == "" {
			result.ErrorMessage = errorMessage
		}
	}
	return EnsureToolResultDefaults(result, args)
}

func buildScheduleReadMetric(label string, value string) map[string]any {
	return map[string]any{
		"label": strings.TrimSpace(label),
		"value": strings.TrimSpace(value),
	}
}

func buildScheduleReadItem(title string, subtitle string, tags []string, detailLines []string, meta map[string]any) map[string]any {
	item := map[string]any{
		"title":        strings.TrimSpace(title),
		"subtitle":     strings.TrimSpace(subtitle),
		"tags":         normalizeStringSlice(tags),
		"detail_lines": normalizeStringSlice(detailLines),
	}
	if len(meta) > 0 {
		item["meta"] = cloneAnyMap(meta)
	}
	return item
}

func buildScheduleReadKV(label string, value string) map[string]any {
	return map[string]any{
		"label": strings.TrimSpace(label),
		"value": strings.TrimSpace(value),
	}
}

func buildScheduleReadItemsSection(title string, items []map[string]any) map[string]any {
	if items == nil {
		items = make([]map[string]any, 0)
	}
	return map[string]any{
		"type":  "items",
		"title": strings.TrimSpace(title),
		"items": items,
	}
}

func buildScheduleReadKVSection(title string, fields []map[string]any) map[string]any {
	if fields == nil {
		fields = make([]map[string]any, 0)
	}
	return map[string]any{
		"type":   "kv",
		"title":  strings.TrimSpace(title),
		"fields": fields,
	}
}

func buildScheduleReadCalloutSection(title string, subtitle string, tone string, detailLines []string) map[string]any {
	return map[string]any{
		"type":         "callout",
		"title":        strings.TrimSpace(title),
		"subtitle":     strings.TrimSpace(subtitle),
		"tone":         strings.TrimSpace(tone),
		"detail_lines": normalizeStringSlice(detailLines),
	}
}

func buildScheduleReadArgsSection(title string, view *ToolArgumentView) map[string]any {
	if view == nil || view.Expanded == nil {
		return nil
	}
	rawFields, ok := view.Expanded["fields"].([]map[string]any)
	if ok {
		fields := make([]map[string]any, 0, len(rawFields))
		for _, raw := range rawFields {
			label, _ := raw["label"].(string)
			display, _ := raw["display"].(string)
			if strings.TrimSpace(label) == "" || strings.TrimSpace(display) == "" {
				continue
			}
			fields = append(fields, buildScheduleReadKV(label, display))
		}
		if len(fields) > 0 {
			return buildScheduleReadKVSection(title, fields)
		}
		return nil
	}

	rawAny, ok := view.Expanded["fields"].([]any)
	if !ok {
		return nil
	}
	fields := make([]map[string]any, 0, len(rawAny))
	for _, current := range rawAny {
		row, ok := current.(map[string]any)
		if !ok {
			continue
		}
		label, _ := row["label"].(string)
		display, _ := row["display"].(string)
		if strings.TrimSpace(label) == "" || strings.TrimSpace(display) == "" {
			continue
		}
		fields = append(fields, buildScheduleReadKV(label, display))
	}
	if len(fields) == 0 {
		return nil
	}
	return buildScheduleReadKVSection(title, fields)
}

func buildReadFailureSections(argView *ToolArgumentView, observation string) []map[string]any {
	message := trimFailureText(observation, "读取结果失败，请检查参数后重试。")
	sections := []map[string]any{
		buildScheduleReadCalloutSection("执行失败", message, "danger", []string{message}),
	}
	appendSectionIfPresent(&sections, buildScheduleReadArgsSection("查询条件", argView))
	return sections
}

func buildScheduleReadSimpleFailureResult(toolName string, args map[string]any, state *schedule.ScheduleState, observation string) ToolExecutionResult {
	legacy := LegacyResultWithState(toolName, args, state, observation)
	return buildScheduleReadResult(
		toolName,
		args,
		state,
		observation,
		ToolStatusFailed,
		fmt.Sprintf("%s失败", resolveToolLabelCN(toolName)),
		trimFailureText(observation, "请检查筛选条件后重试。"),
		nil,
		nil,
		buildReadFailureSections(legacy.ArgumentView, observation),
		nil,
	)
}

func appendSectionIfPresent(target *[]map[string]any, section map[string]any) {
	if section == nil {
		return
	}
	*target = append(*target, section)
}

func normalizeStringSlice(values []string) []string {
	if len(values) == 0 {
		return make([]string, 0)
	}
	out := make([]string, 0, len(values))
	for _, value := range values {
		text := strings.TrimSpace(value)
		if text == "" {
			continue
		}
		out = append(out, text)
	}
	if len(out) == 0 {
		return make([]string, 0)
	}
	return out
}

func trimFailureText(observation string, fallback string) string {
	status, _ := resolveToolStatusAndSuccess(observation)
	_, message := extractToolErrorInfo(observation, status)
	if strings.TrimSpace(message) != "" {
		return strings.TrimSpace(message)
	}
	if strings.TrimSpace(observation) != "" {
		return strings.TrimSpace(observation)
	}
	return strings.TrimSpace(fallback)
}

func formatScheduleDayCN(state *schedule.ScheduleState, day int) string {
	if day <= 0 {
		return "未知日期"
	}
	if state != nil {
		if week, dayOfWeek, ok := state.DayToWeekDay(day); ok {
			return fmt.Sprintf("第%d天（第%d周 %s）", day, week, formatScheduleWeekdayCN(dayOfWeek))
		}
	}
	return fmt.Sprintf("第%d天", day)
}

func formatScheduleWeekdayCN(dayOfWeek int) string {
	switch dayOfWeek {
	case 1:
		return "周一"
	case 2:
		return "周二"
	case 3:
		return "周三"
	case 4:
		return "周四"
	case 5:
		return "周五"
	case 6:
		return "周六"
	case 7:
		return "周日"
	default:
		return fmt.Sprintf("周%d", dayOfWeek)
	}
}

func formatScheduleSlotRangeCN(start int, end int) string {
	if start <= 0 {
		return "未知节次"
	}
	if end <= 0 || end < start {
		end = start
	}
	return fmt.Sprintf("第%d-%d节", start, end)
}

func formatScheduleDaySlotCN(state *schedule.ScheduleState, day int, start int, end int) string {
	return fmt.Sprintf("%s %s", formatScheduleDayCN(state, day), formatScheduleSlotRangeCN(start, end))
}

func formatScheduleWeekListCN(weeks []int) string {
	if len(weeks) == 0 {
		return "不限周次"
	}
	parts := make([]string, 0, len(weeks))
	for _, week := range weeks {
		if week <= 0 {
			continue
		}
		parts = append(parts, fmt.Sprintf("第%d周", week))
	}
	if len(parts) == 0 {
		return "不限周次"
	}
	return strings.Join(parts, "、")
}

func formatScheduleSectionListCN(sections []int) string {
	if len(sections) == 0 {
		return "无"
	}
	parts := make([]string, 0, len(sections))
	for _, section := range sections {
		if section <= 0 {
			continue
		}
		parts = append(parts, fmt.Sprintf("第%d节", section))
	}
	if len(parts) == 0 {
		return "无"
	}
	return strings.Join(parts, "、")
}

func formatScheduleTaskStatusCN(task schedule.ScheduleTask) string {
	switch {
	case schedule.IsPendingTask(task):
		return "待安排"
	case schedule.IsSuggestedTask(task):
		return "已预排"
	default:
		if task.Locked {
			return "已安排（固定）"
		}
		return "已安排"
	}
}

func formatTargetTaskStatusCN(status string) string {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "existing":
		return "已安排"
	case "suggested":
		return "已预排"
	case "pending":
		return "待安排"
	default:
		return fallbackText(status, "未标注")
	}
}

func formatTargetPoolStatusCN(status string) string {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "all":
		return "全部任务"
	case "existing":
		return "已安排任务"
	case "suggested":
		return "已预排任务"
	case "pending":
		return "待安排任务"
	default:
		return fallbackText(status, "任务池")
	}
}

func formatSlotTypeLabelCN(slotType string) string {
	switch strings.ToLower(strings.TrimSpace(slotType)) {
	case "", "empty", "strict":
		return "纯空位"
	case "embedded_candidate", "embedded", "embed":
		return "可嵌入候选"
	default:
		return strings.TrimSpace(slotType)
	}
}

func formatDayScopeLabelCN(scope string) string {
	switch strings.ToLower(strings.TrimSpace(scope)) {
	case "workday":
		return "工作日"
	case "weekend":
		return "周末"
	default:
		return "全部日期"
	}
}

func buildWeekRangeLabelCN(weekFrom int, weekTo int, weekFilter []int) string {
	if len(weekFilter) > 0 {
		return formatScheduleWeekListCN(weekFilter)
	}
	if weekFrom > 0 && weekTo > 0 {
		if weekFrom == weekTo {
			return fmt.Sprintf("第%d周", weekFrom)
		}
		return fmt.Sprintf("第%d-%d周", weekFrom, weekTo)
	}
	return "全部周次"
}

func formatBoolLabelCN(value bool) string {
	if value {
		return "是"
	}
	return "否"
}

func formatWeekdayListCN(days []int) string {
	if len(days) == 0 {
		return "不限星期"
	}
	parts := make([]string, 0, len(days))
	for _, day := range days {
		parts = append(parts, formatScheduleWeekdayCN(day))
	}
	return strings.Join(parts, "、")
}

func formatScheduleTaskSourceCN(task schedule.ScheduleTask) string {
	switch strings.TrimSpace(task.Source) {
	case "event":
		if isCourseScheduleTaskForRead(task) {
			return "课程表"
		}
		return "日程事件"
	case "task_item":
		return "任务项"
	default:
		return fallbackText(task.Source, "未知来源")
	}
}

func formatScheduleTaskSlotsBriefCN(state *schedule.ScheduleState, slots []schedule.TaskSlot) string {
	if len(slots) == 0 {
		return "尚未落位"
	}
	parts := make([]string, 0, len(slots))
	for _, slot := range cloneAndSortTaskSlots(slots) {
		parts = append(parts, formatScheduleDaySlotCN(state, slot.Day, slot.SlotStart, slot.SlotEnd))
	}
	return strings.Join(parts, "；")
}

func countScheduleDayOccupiedForRead(state *schedule.ScheduleState, day int) int {
	if state == nil {
		return 0
	}
	occupied := 0
	for i := range state.Tasks {
		task := state.Tasks[i]
		if task.EmbedHost != nil {
			continue
		}
		for _, slot := range task.Slots {
			if slot.Day == day {
				occupied += slot.SlotEnd - slot.SlotStart + 1
			}
		}
	}
	return occupied
}

func countScheduleDayTaskOccupiedForRead(state *schedule.ScheduleState, day int) int {
	if state == nil {
		return 0
	}
	occupied := 0
	for i := range state.Tasks {
		task := state.Tasks[i]
		if isCourseScheduleTaskForRead(task) || task.EmbedHost != nil {
			continue
		}
		for _, slot := range task.Slots {
			if slot.Day == day {
				occupied += slot.SlotEnd - slot.SlotStart + 1
			}
		}
	}
	return occupied
}

func listScheduleTasksOnDayForRead(state *schedule.ScheduleState, day int, includeCourse bool) []scheduleReadTaskOnDay {
	if state == nil {
		return nil
	}
	items := make([]scheduleReadTaskOnDay, 0)
	for i := range state.Tasks {
		task := &state.Tasks[i]
		if !includeCourse && isCourseScheduleTaskForRead(*task) {
			continue
		}
		for _, slot := range task.Slots {
			if slot.Day != day {
				continue
			}
			items = append(items, scheduleReadTaskOnDay{
				Task:      task,
				SlotStart: slot.SlotStart,
				SlotEnd:   slot.SlotEnd,
			})
		}
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].SlotStart != items[j].SlotStart {
			return items[i].SlotStart < items[j].SlotStart
		}
		if items[i].SlotEnd != items[j].SlotEnd {
			return items[i].SlotEnd < items[j].SlotEnd
		}
		return items[i].Task.StateID < items[j].Task.StateID
	})
	return items
}

func listScheduleTasksInRangeForRead(state *schedule.ScheduleState, day int, start int, end int, includeCourse bool) []scheduleReadTaskOnDay {
	items := listScheduleTasksOnDayForRead(state, day, includeCourse)
	filtered := make([]scheduleReadTaskOnDay, 0, len(items))
	for _, item := range items {
		if item.SlotStart <= end && item.SlotEnd >= start {
			filtered = append(filtered, item)
		}
	}
	return filtered
}

func findScheduleFreeRangesOnDayForRead(state *schedule.ScheduleState, day int) []scheduleReadFreeRange {
	if state == nil {
		return nil
	}
	occupied := make([]bool, 13)
	for i := range state.Tasks {
		task := state.Tasks[i]
		if task.EmbedHost != nil {
			continue
		}
		for _, slot := range task.Slots {
			if slot.Day != day {
				continue
			}
			for section := slot.SlotStart; section <= slot.SlotEnd; section++ {
				if section >= 1 && section <= 12 {
					occupied[section] = true
				}
			}
		}
	}

	ranges := make([]scheduleReadFreeRange, 0)
	start := 0
	for section := 1; section <= 12; section++ {
		if !occupied[section] {
			if start == 0 {
				start = section
			}
			continue
		}
		if start > 0 {
			ranges = append(ranges, scheduleReadFreeRange{Day: day, SlotStart: start, SlotEnd: section - 1})
			start = 0
		}
	}
	if start > 0 {
		ranges = append(ranges, scheduleReadFreeRange{Day: day, SlotStart: start, SlotEnd: 12})
	}
	return ranges
}

func findScheduleHostTaskBySlotForRead(state *schedule.ScheduleState, day int, section int) *schedule.ScheduleTask {
	if state == nil {
		return nil
	}
	for i := range state.Tasks {
		task := &state.Tasks[i]
		if task.EmbedHost != nil {
			continue
		}
		for _, slot := range task.Slots {
			if slot.Day == day && section >= slot.SlotStart && section <= slot.SlotEnd {
				return task
			}
		}
	}
	return nil
}

func isCourseScheduleTaskForRead(task schedule.ScheduleTask) bool {
	if strings.TrimSpace(task.Source) != "event" {
		return false
	}
	if strings.EqualFold(strings.TrimSpace(task.EventType), "course") {
		return true
	}
	return strings.TrimSpace(task.Category) == "课程"
}

func cloneAndSortTaskSlots(slots []schedule.TaskSlot) []schedule.TaskSlot {
	if len(slots) == 0 {
		return nil
	}
	out := make([]schedule.TaskSlot, len(slots))
	copy(out, slots)
	sort.Slice(out, func(i, j int) bool {
		if out[i].Day != out[j].Day {
			return out[i].Day < out[j].Day
		}
		if out[i].SlotStart != out[j].SlotStart {
			return out[i].SlotStart < out[j].SlotStart
		}
		return out[i].SlotEnd < out[j].SlotEnd
	})
	return out
}

func fallbackText(text string, fallback string) string {
	if strings.TrimSpace(text) == "" {
		return strings.TrimSpace(fallback)
	}
	return strings.TrimSpace(text)
}
