package schedule_read

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/LoveLosita/smartflow/backend/newAgent/tools/schedule"
)

// BuildResultView 统一封装 schedule.read_result 结构。
//
// 职责边界：
// 1. 负责把已经计算好的折叠态/展开态内容组装成标准视图。
// 2. 负责在子包内补齐 status / status_label，避免依赖父包常量。
// 3. 不负责 ToolExecutionResult 外层协议，也不改写 observation 原文。
func BuildResultView(input BuildResultViewInput) ReadResultView {
	status := normalizeStatus(input.Status)
	if status == "" {
		status = StatusDone
	}

	collapsed := CollapsedView{
		Title:       input.Title,
		Subtitle:    input.Subtitle,
		Status:      status,
		StatusLabel: resolveStatusLabelCN(status),
		Metrics:     appendMetricCopy(input.Metrics),
	}
	expanded := ExpandedView{
		Items:          appendItemCopy(input.Items),
		Sections:       cloneSectionList(input.Sections),
		RawText:        input.Observation,
		MachinePayload: cloneAnyMap(input.MachinePayload),
	}

	return ReadResultView{
		ViewType:  ViewTypeReadResult,
		Version:   ViewVersionReadResult,
		Collapsed: collapsed.Map(),
		Expanded:  expanded.Map(),
	}
}

// BuildFailureView 统一生成 read 工具失败卡片视图。
//
// 职责边界：
// 1. 负责把失败 observation 提炼成展开态提示与参数回显。
// 2. 不负责决定是否要失败；调用方需要在进入这里前确认失败条件。
// 3. 若标题、副标题未显式传入，则按工具名与 observation 兜底生成。
func BuildFailureView(input BuildFailureViewInput) ReadResultView {
	status := normalizeStatus(input.Status)
	if status == "" {
		status = StatusFailed
	}

	title := strings.TrimSpace(input.Title)
	if title == "" {
		title = fmt.Sprintf("%s失败", resolveToolLabelCN(input.ToolName))
	}

	subtitle := strings.TrimSpace(input.Subtitle)
	if subtitle == "" {
		subtitle = trimFailureText(input.Observation, "请检查筛选条件后重试。")
	}

	return BuildResultView(BuildResultViewInput{
		Status:      status,
		Title:       title,
		Subtitle:    subtitle,
		Sections:    buildReadFailureSections(input.ArgFields, input.Observation),
		Observation: input.Observation,
	})
}

// BuildMetric 是 collapsed.metrics 的便捷构造器。
func BuildMetric(label string, value string) MetricField {
	return MetricField{
		Label: strings.TrimSpace(label),
		Value: strings.TrimSpace(value),
	}
}

// BuildKVField 是 kv section 的便捷构造器。
func BuildKVField(label string, value string) KVField {
	return KVField{
		Label: strings.TrimSpace(label),
		Value: strings.TrimSpace(value),
	}
}

// BuildItem 是 items 的便捷构造器。
func BuildItem(title string, subtitle string, tags []string, detailLines []string, meta map[string]any) ItemView {
	return ItemView{
		Title:       strings.TrimSpace(title),
		Subtitle:    strings.TrimSpace(subtitle),
		Tags:        normalizeStringSlice(tags),
		DetailLines: normalizeStringSlice(detailLines),
		Meta:        cloneAnyMap(meta),
	}
}

// BuildItemsSection 把条目列表包装成 items section。
func BuildItemsSection(title string, items []ItemView) map[string]any {
	normalized := make([]map[string]any, 0, len(items))
	for _, item := range items {
		normalized = append(normalized, item.Map())
	}
	return map[string]any{
		"type":  "items",
		"title": strings.TrimSpace(title),
		"items": normalized,
	}
}

// BuildKVSection 把 kv 列表包装成 kv section。
func BuildKVSection(title string, fields []KVField) map[string]any {
	normalized := make([]map[string]any, 0, len(fields))
	for _, field := range fields {
		label := strings.TrimSpace(field.Label)
		value := strings.TrimSpace(field.Value)
		if label == "" || value == "" {
			continue
		}
		normalized = append(normalized, map[string]any{
			"label": label,
			"value": value,
		})
	}
	return map[string]any{
		"type":   "kv",
		"title":  strings.TrimSpace(title),
		"fields": normalized,
	}
}

// BuildCalloutSection 把提示块包装成 callout section。
func BuildCalloutSection(title string, subtitle string, tone string, detailLines []string) map[string]any {
	return map[string]any{
		"type":         "callout",
		"title":        strings.TrimSpace(title),
		"subtitle":     strings.TrimSpace(subtitle),
		"tone":         strings.TrimSpace(tone),
		"detail_lines": normalizeStringSlice(detailLines),
	}
}

// BuildArgsSection 负责把父包已经格式化好的参数字段拼成查询条件 section。
//
// 职责边界：
// 1. 这里只接受纯 KVField，不依赖父包 ToolArgumentView。
// 2. 只过滤空 label / value，不补充额外解释文案。
// 3. 没有有效字段时返回 nil，交给调用方决定是否追加 section。
func BuildArgsSection(title string, fields []KVField) map[string]any {
	if len(fields) == 0 {
		return nil
	}
	valid := make([]KVField, 0, len(fields))
	for _, field := range fields {
		label := strings.TrimSpace(field.Label)
		value := strings.TrimSpace(field.Value)
		if label == "" || value == "" {
			continue
		}
		valid = append(valid, BuildKVField(label, value))
	}
	if len(valid) == 0 {
		return nil
	}
	return BuildKVSection(title, valid)
}

func buildReadFailureSections(argFields []KVField, observation string) []map[string]any {
	message := trimFailureText(observation, "读取结果失败，请检查参数后重试。")
	sections := []map[string]any{
		BuildCalloutSection("执行失败", message, "danger", []string{message}),
	}
	appendSectionIfPresent(&sections, BuildArgsSection("查询条件", argFields))
	return sections
}

func appendSectionIfPresent(target *[]map[string]any, section map[string]any) {
	if section == nil {
		return
	}
	*target = append(*target, section)
}

func appendMetricCopy(metrics []MetricField) []MetricField {
	if len(metrics) == 0 {
		return make([]MetricField, 0)
	}
	out := make([]MetricField, 0, len(metrics))
	for _, metric := range metrics {
		label := strings.TrimSpace(metric.Label)
		value := strings.TrimSpace(metric.Value)
		if label == "" || value == "" {
			continue
		}
		out = append(out, MetricField{Label: label, Value: value})
	}
	if len(out) == 0 {
		return make([]MetricField, 0)
	}
	return out
}

func appendItemCopy(items []ItemView) []ItemView {
	if len(items) == 0 {
		return make([]ItemView, 0)
	}
	out := make([]ItemView, 0, len(items))
	for _, item := range items {
		out = append(out, BuildItem(item.Title, item.Subtitle, item.Tags, item.DetailLines, item.Meta))
	}
	return out
}

func normalizeStatus(status string) string {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case StatusDone:
		return StatusDone
	case StatusBlocked:
		return StatusBlocked
	case StatusFailed:
		return StatusFailed
	default:
		return ""
	}
}

func resolveStatusLabelCN(status string) string {
	switch normalizeStatus(status) {
	case StatusDone:
		return "已完成"
	case StatusBlocked:
		return "已阻断"
	default:
		return "失败"
	}
}

func resolveToolLabelCN(toolName string) string {
	switch strings.TrimSpace(toolName) {
	case "query_available_slots":
		return "查询可用时段"
	case "query_range":
		return "查询范围"
	case "query_target_tasks":
		return "查询目标任务"
	case "get_task_info":
		return "查询任务详情"
	case "get_overview":
		return "查看排程总览"
	case "queue_status":
		return "查看队列状态"
	default:
		return "读取结果"
	}
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
	if payload, ok := parseObservationJSON(observation); ok {
		if message, ok := readStringFromMap(payload, "error", "err", "message", "reason"); ok && strings.TrimSpace(message) != "" {
			return strings.TrimSpace(message)
		}
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

type taskOnDay struct {
	Task      *schedule.ScheduleTask
	SlotStart int
	SlotEnd   int
}

type freeRange struct {
	Day       int
	SlotStart int
	SlotEnd   int
}

func listScheduleTasksOnDayForRead(state *schedule.ScheduleState, day int, includeCourse bool) []taskOnDay {
	if state == nil {
		return nil
	}
	items := make([]taskOnDay, 0)
	for i := range state.Tasks {
		task := &state.Tasks[i]
		if !includeCourse && isCourseScheduleTaskForRead(*task) {
			continue
		}
		for _, slot := range task.Slots {
			if slot.Day != day {
				continue
			}
			items = append(items, taskOnDay{
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

func listScheduleTasksInRangeForRead(state *schedule.ScheduleState, day int, start int, end int, includeCourse bool) []taskOnDay {
	items := listScheduleTasksOnDayForRead(state, day, includeCourse)
	filtered := make([]taskOnDay, 0, len(items))
	for _, item := range items {
		if item.SlotStart <= end && item.SlotEnd >= start {
			filtered = append(filtered, item)
		}
	}
	return filtered
}

func findScheduleFreeRangesOnDayForRead(state *schedule.ScheduleState, day int) []freeRange {
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

	ranges := make([]freeRange, 0)
	start := 0
	for section := 1; section <= 12; section++ {
		if !occupied[section] {
			if start == 0 {
				start = section
			}
			continue
		}
		if start > 0 {
			ranges = append(ranges, freeRange{Day: day, SlotStart: start, SlotEnd: section - 1})
			start = 0
		}
	}
	if start > 0 {
		ranges = append(ranges, freeRange{Day: day, SlotStart: start, SlotEnd: 12})
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

func parseObservationJSON(text string) (map[string]any, bool) {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" || !strings.HasPrefix(trimmed, "{") {
		return nil, false
	}
	var payload map[string]any
	if err := json.Unmarshal([]byte(trimmed), &payload); err != nil {
		return nil, false
	}
	return payload, true
}

func readStringFromMap(payload map[string]any, keys ...string) (string, bool) {
	if len(payload) == 0 {
		return "", false
	}
	for _, key := range keys {
		raw, ok := payload[key]
		if !ok {
			continue
		}
		value, ok := raw.(string)
		if ok {
			return value, true
		}
	}
	return "", false
}

func cloneSectionList(sections []map[string]any) []map[string]any {
	if len(sections) == 0 {
		return make([]map[string]any, 0)
	}
	out := make([]map[string]any, 0, len(sections))
	for _, section := range sections {
		out = append(out, cloneAnyMap(section))
	}
	return out
}

func cloneAnyMap(input map[string]any) map[string]any {
	if len(input) == 0 {
		return nil
	}
	out := make(map[string]any, len(input))
	for key, value := range input {
		out[key] = cloneAnyValue(value)
	}
	return out
}

func cloneAnyValue(value any) any {
	switch current := value.(type) {
	case map[string]any:
		return cloneAnyMap(current)
	case []map[string]any:
		out := make([]map[string]any, 0, len(current))
		for _, item := range current {
			out = append(out, cloneAnyMap(item))
		}
		return out
	case []any:
		out := make([]any, 0, len(current))
		for _, item := range current {
			out = append(out, cloneAnyValue(item))
		}
		return out
	case []string:
		out := make([]string, len(current))
		copy(out, current)
		return out
	case []int:
		out := make([]int, len(current))
		copy(out, current)
		return out
	default:
		return current
	}
}

func maxInt(values ...int) int {
	if len(values) == 0 {
		return 0
	}
	best := values[0]
	for _, value := range values[1:] {
		if value > best {
			best = value
		}
	}
	return best
}

func toInt(value any) (int, bool) {
	switch current := value.(type) {
	case int:
		return current, true
	case int32:
		return int(current), true
	case int64:
		return int(current), true
	case float64:
		return int(current), true
	default:
		return 0, false
	}
}

func optionalIntValue(value *int) any {
	if value == nil {
		return nil
	}
	return *value
}
