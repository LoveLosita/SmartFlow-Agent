package newagenttools

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/LoveLosita/smartflow/backend/newAgent/tools/schedule"
)

const (
	ToolStatusDone    = "done"
	ToolStatusFailed  = "failed"
	ToolStatusBlocked = "blocked"
)

// ToolDisplayView 描述工具结果的结构化展示视图。
type ToolDisplayView struct {
	ViewType  string         `json:"view_type,omitempty"`
	Version   int            `json:"version,omitempty"`
	Collapsed map[string]any `json:"collapsed,omitempty"`
	Expanded  map[string]any `json:"expanded,omitempty"`
}

// ToolArgumentView 描述工具参数的结构化展示视图。
type ToolArgumentView struct {
	ViewType  string         `json:"view_type,omitempty"`
	Version   int            `json:"version,omitempty"`
	Collapsed map[string]any `json:"collapsed,omitempty"`
	Expanded  map[string]any `json:"expanded,omitempty"`
}

// ToolExecutionResult 是 newAgent 工具主接口的统一结果结构。
//
// 职责边界：
// 1. 负责承载 execute、SSE、timeline 所需的最小公共字段；
// 2. 负责保留 ObservationText，保证第一阶段 LLM 观察文本不变；
// 3. 不负责具体工具业务语义，工具语义由各工具 handler 决定。
type ToolExecutionResult struct {
	Tool             string            `json:"tool,omitempty"`
	Status           string            `json:"status,omitempty"` // done / failed / blocked
	Success          bool              `json:"success"`
	ObservationText  string            `json:"observation_text,omitempty"`
	Summary          string            `json:"summary,omitempty"`
	ArgumentsPreview string            `json:"arguments_preview,omitempty"`
	ArgumentView     *ToolArgumentView `json:"argument_view,omitempty"`
	ResultView       *ToolDisplayView  `json:"result_view,omitempty"`
	ErrorCode        string            `json:"error_code,omitempty"`
	ErrorMessage     string            `json:"error_message,omitempty"`
}

// LegacyResult 用于未做专属卡片的工具兜底。
func LegacyResult(toolName string, args map[string]any, oldText string) ToolExecutionResult {
	return LegacyResultWithState(toolName, args, nil, oldText)
}

// LegacyResultWithState 在 LegacyResult 基础上支持读取 ScheduleState 补齐中文参数展示。
func LegacyResultWithState(toolName string, args map[string]any, state *schedule.ScheduleState, oldText string) ToolExecutionResult {
	status, success := resolveToolStatusAndSuccess(oldText)
	errorCode, errorMessage := extractToolErrorInfo(oldText, status)
	tool := strings.TrimSpace(toolName)
	toolLabel := resolveToolLabelCN(tool)

	argumentView := buildLocalizedArgumentView(tool, args, state)
	argumentsPreview := readArgumentSummary(argumentView)

	result := ToolExecutionResult{
		Tool:             tool,
		Status:           status,
		Success:          success,
		ObservationText:  oldText,
		Summary:          buildToolSummary(oldText),
		ArgumentsPreview: argumentsPreview,
		ArgumentView:     argumentView,
		ResultView: &ToolDisplayView{
			ViewType: "legacy_text",
			Version:  1,
			Collapsed: map[string]any{
				"title":        buildLegacyTitle(toolLabel, status),
				"status":       status,
				"status_label": resolveToolStatusLabelCN(status),
				"tool":         tool,
				"tool_label":   toolLabel,
				"has_output":   strings.TrimSpace(oldText) != "",
			},
			Expanded: map[string]any{
				"raw_text_label": "原始结果",
				"raw_text":       oldText,
			},
		},
		ErrorCode:    errorCode,
		ErrorMessage: errorMessage,
	}
	return ensureToolResultDefaults(result, args)
}

// BlockedResult 构造被拦截类结果，供 execute 链路统一复用。
func BlockedResult(toolName string, args map[string]any, observationText, errorCode, errorMessage string) ToolExecutionResult {
	result := LegacyResult(toolName, args, observationText)
	result.Status = ToolStatusBlocked
	result.Success = false
	result.ErrorCode = strings.TrimSpace(errorCode)
	result.ErrorMessage = strings.TrimSpace(errorMessage)
	if result.ResultView != nil {
		if result.ResultView.Collapsed == nil {
			result.ResultView.Collapsed = make(map[string]any)
		}
		result.ResultView.Collapsed["status"] = ToolStatusBlocked
		result.ResultView.Collapsed["status_label"] = resolveToolStatusLabelCN(ToolStatusBlocked)
		result.ResultView.Collapsed["title"] = buildLegacyTitle(resolveToolLabelCN(toolName), ToolStatusBlocked)
	}
	return ensureToolResultDefaults(result, args)
}

// EnsureToolResultDefaults 负责兜底补齐 execute 侧依赖字段，避免空值扩散。
func EnsureToolResultDefaults(result ToolExecutionResult, args map[string]any) ToolExecutionResult {
	return ensureToolResultDefaults(result, args)
}

func ensureToolResultDefaults(result ToolExecutionResult, args map[string]any) ToolExecutionResult {
	if strings.TrimSpace(result.Tool) == "" {
		result.Tool = "unknown_tool"
	}
	if strings.TrimSpace(result.Status) == "" {
		if result.Success {
			result.Status = ToolStatusDone
		} else {
			result.Status = ToolStatusFailed
		}
	}
	if strings.TrimSpace(result.Summary) == "" {
		result.Summary = buildToolSummary(result.ObservationText)
	}
	if result.ArgumentView == nil {
		result.ArgumentView = buildLocalizedArgumentView(result.Tool, args, nil)
	}
	if strings.TrimSpace(result.ArgumentsPreview) == "" {
		result.ArgumentsPreview = readArgumentSummary(result.ArgumentView)
	}
	if strings.TrimSpace(result.ArgumentsPreview) == "" && len(args) > 0 {
		result.ArgumentsPreview = fmt.Sprintf("共 %d 个参数", len(args))
	}
	if result.ResultView == nil {
		result.ResultView = &ToolDisplayView{
			ViewType: "legacy_text",
			Version:  1,
			Collapsed: map[string]any{
				"title":        buildLegacyTitle(resolveToolLabelCN(result.Tool), result.Status),
				"status":       result.Status,
				"status_label": resolveToolStatusLabelCN(result.Status),
				"tool":         strings.TrimSpace(result.Tool),
				"tool_label":   resolveToolLabelCN(result.Tool),
				"has_output":   strings.TrimSpace(result.ObservationText) != "",
			},
			Expanded: map[string]any{
				"raw_text_label": "原始结果",
				"raw_text":       result.ObservationText,
			},
		}
	}
	return result
}

// ToolArgumentViewToMap 把参数视图转换成 stream/timeline 可直接落库的 map。
func ToolArgumentViewToMap(view *ToolArgumentView) map[string]any {
	if view == nil {
		return nil
	}
	out := map[string]any{
		"view_type": strings.TrimSpace(view.ViewType),
		"version":   view.Version,
	}
	if len(view.Collapsed) > 0 {
		out["collapsed"] = cloneAnyMap(view.Collapsed)
	}
	if len(view.Expanded) > 0 {
		out["expanded"] = cloneAnyMap(view.Expanded)
	}
	return out
}

// ToolDisplayViewToMap 把结果视图转换成 stream/timeline 可直接落库的 map。
func ToolDisplayViewToMap(view *ToolDisplayView) map[string]any {
	if view == nil {
		return nil
	}
	out := map[string]any{
		"view_type": strings.TrimSpace(view.ViewType),
		"version":   view.Version,
	}
	if len(view.Collapsed) > 0 {
		out["collapsed"] = cloneAnyMap(view.Collapsed)
	}
	if len(view.Expanded) > 0 {
		out["expanded"] = cloneAnyMap(view.Expanded)
	}
	return out
}

func resolveToolStatusAndSuccess(observation string) (string, bool) {
	trimmed := strings.TrimSpace(observation)
	if trimmed == "" {
		return ToolStatusDone, true
	}

	// 1. 优先解析 JSON 结构字段，避免依赖自然语言文本。
	// 2. 若 JSON 明确给出 success/status/error，则以结构字段为准。
	// 3. 仅在无法结构化解析时，回退关键词兜底。
	if payload, ok := parseObservationJSON(trimmed); ok {
		if statusText, ok := readStringFromMap(payload, "status"); ok {
			status := normalizeToolStatus(statusText)
			if status != "" {
				return status, status == ToolStatusDone
			}
		}
		if blocked, ok := readBoolFromMap(payload, "blocked"); ok && blocked {
			return ToolStatusBlocked, false
		}
		if success, ok := readBoolFromMap(payload, "success"); ok {
			if success {
				return ToolStatusDone, true
			}
			return ToolStatusFailed, false
		}
		if errText, ok := readStringFromMap(payload, "error", "err"); ok && strings.TrimSpace(errText) != "" {
			return ToolStatusFailed, false
		}
	}

	lower := strings.ToLower(trimmed)
	if strings.Contains(trimmed, "阻断") || strings.Contains(trimmed, "禁用") || strings.Contains(lower, "blocked") {
		return ToolStatusBlocked, false
	}
	if strings.Contains(trimmed, "失败") || strings.Contains(lower, "failed") || strings.Contains(lower, "error") {
		return ToolStatusFailed, false
	}
	return ToolStatusDone, true
}

func normalizeToolStatus(status string) string {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case ToolStatusDone:
		return ToolStatusDone
	case ToolStatusFailed:
		return ToolStatusFailed
	case ToolStatusBlocked:
		return ToolStatusBlocked
	default:
		return ""
	}
}

func extractToolErrorInfo(observation string, status string) (string, string) {
	trimmed := strings.TrimSpace(observation)
	if trimmed == "" || status == ToolStatusDone {
		return "", ""
	}

	if payload, ok := parseObservationJSON(trimmed); ok {
		errorCode, _ := readStringFromMap(payload, "error_code", "code")
		errorMessage, _ := readStringFromMap(payload, "error", "err", "message", "reason")
		if strings.TrimSpace(errorCode) == "" && status == ToolStatusBlocked {
			errorCode = "blocked"
		}
		if strings.TrimSpace(errorMessage) != "" {
			return strings.TrimSpace(errorCode), strings.TrimSpace(errorMessage)
		}
		if status == ToolStatusBlocked {
			return strings.TrimSpace(errorCode), "工具被策略阻断"
		}
		return strings.TrimSpace(errorCode), ""
	}

	if status == ToolStatusBlocked {
		return "blocked", trimmed
	}
	return "", trimmed
}

func buildToolSummary(observation string) string {
	trimmed := strings.TrimSpace(observation)
	if trimmed == "" {
		return "工具已执行完成。"
	}

	if payload, ok := parseObservationJSON(trimmed); ok {
		if errText, ok := readStringFromMap(payload, "error", "err", "message"); ok && strings.TrimSpace(errText) != "" {
			return truncateSummary(fmt.Sprintf("执行失败：%s", strings.TrimSpace(errText)))
		}
		if message, ok := readStringFromMap(payload, "result", "summary", "reason", "message"); ok && strings.TrimSpace(message) != "" {
			return truncateSummary(strings.TrimSpace(message))
		}
		if success, ok := readBoolFromMap(payload, "success"); ok && success {
			return "工具执行成功。"
		}
	}

	flat := strings.Join(strings.Fields(trimmed), " ")
	return truncateSummary(flat)
}

func truncateSummary(text string) string {
	runes := []rune(strings.TrimSpace(text))
	if len(runes) <= 48 {
		return string(runes)
	}
	return string(runes[:48]) + "..."
}

func buildLegacyTitle(toolLabel string, status string) string {
	switch normalizeToolStatus(status) {
	case ToolStatusDone:
		return fmt.Sprintf("%s已完成", strings.TrimSpace(toolLabel))
	case ToolStatusBlocked:
		return fmt.Sprintf("%s已阻断", strings.TrimSpace(toolLabel))
	default:
		return fmt.Sprintf("%s失败", strings.TrimSpace(toolLabel))
	}
}

func resolveToolStatusLabelCN(status string) string {
	switch normalizeToolStatus(status) {
	case ToolStatusDone:
		return "已完成"
	case ToolStatusBlocked:
		return "已阻断"
	default:
		return "失败"
	}
}

func resolveToolLabelCN(toolName string) string {
	name := strings.TrimSpace(toolName)
	switch name {
	case "query_available_slots":
		return "查询可用时段"
	case "query_target_tasks":
		return "查询目标任务"
	case "queue_status":
		return "查看队列状态"
	case "queue_pop_head":
		return "获取队首任务"
	case "queue_skip_head":
		return "跳过队首任务"
	case "analyze_health":
		return "综合体检"
	case "analyze_rhythm":
		return "分析学习节奏"
	case "web_search":
		return "网页搜索"
	case "web_fetch":
		return "网页抓取"
	case "upsert_task_class":
		return "写入任务类"
	case ToolNameContextToolsAdd:
		return "激活工具域"
	case ToolNameContextToolsRemove:
		return "移除工具域"
	case "move":
		return "移动任务"
	case "place":
		return "预排任务"
	case "swap":
		return "交换任务"
	case "batch_move":
		return "批量移动"
	case "unplace":
		return "移出任务"
	case "queue_apply_head_move":
		return "应用队首任务"
	case "get_overview":
		return "查看总览"
	case "query_range":
		return "查询时间范围"
	case "get_task_info":
		return "查看任务信息"
	default:
		if name == "" {
			return "工具"
		}
		return name
	}
}

func resolveOperationLabelCN(operation string) string {
	switch strings.TrimSpace(operation) {
	case "move":
		return "移动任务"
	case "place":
		return "预排任务"
	case "swap":
		return "交换任务"
	case "batch_move":
		return "批量移动"
	case "unplace":
		return "移出任务"
	case "queue_apply_head_move":
		return "应用队首任务"
	default:
		return resolveToolLabelCN(operation)
	}
}

func readArgumentSummary(view *ToolArgumentView) string {
	if view == nil || len(view.Collapsed) == 0 {
		return ""
	}
	summary, ok := view.Collapsed["summary"].(string)
	if !ok {
		return ""
	}
	return strings.TrimSpace(summary)
}

func buildLocalizedArgumentView(toolName string, args map[string]any, state *schedule.ScheduleState) *ToolArgumentView {
	fields := buildArgumentFields(toolName, args, state)
	summary := buildArgumentSummary(fields)
	if summary == "" {
		summary = "无参数"
	}
	return &ToolArgumentView{
		ViewType: "tool.arguments",
		Version:  1,
		Collapsed: map[string]any{
			"summary":    summary,
			"args_count": len(args),
		},
		Expanded: map[string]any{
			"args":   cloneAnyMap(args),
			"fields": fields,
		},
	}
}

func buildArgumentFields(toolName string, args map[string]any, state *schedule.ScheduleState) []map[string]any {
	if len(args) == 0 {
		return make([]map[string]any, 0)
	}
	keys := make([]string, 0, len(args))
	for key := range args {
		if strings.TrimSpace(key) == "_user_id" {
			continue
		}
		keys = append(keys, key)
	}
	sort.SliceStable(keys, func(i, j int) bool {
		leftRank := argumentDisplayRank(keys[i])
		rightRank := argumentDisplayRank(keys[j])
		if leftRank != rightRank {
			return leftRank < rightRank
		}
		return keys[i] < keys[j]
	})

	fields := make([]map[string]any, 0, len(keys))
	for _, key := range keys {
		raw := args[key]
		label := resolveArgumentLabelCN(strings.TrimSpace(key))
		display := formatArgumentDisplay(toolName, strings.TrimSpace(key), raw, args, state)
		field := map[string]any{
			"key":     key,
			"label":   label,
			"value":   raw,
			"display": display,
		}
		fields = append(fields, field)
	}
	return fields
}

func argumentDisplayRank(key string) int {
	switch strings.TrimSpace(key) {
	case "task_id", "task_a", "task_b":
		return 10
	case "day", "new_day":
		return 20
	case "slot_start", "new_slot_start":
		return 30
	case "moves":
		return 40
	case "reason":
		return 50
	default:
		return 100
	}
}

func buildArgumentSummary(fields []map[string]any) string {
	if len(fields) == 0 {
		return ""
	}
	items := make([]string, 0, 2)
	for _, field := range fields {
		label, _ := field["label"].(string)
		display, _ := field["display"].(string)
		label = strings.TrimSpace(label)
		display = strings.TrimSpace(display)
		if label == "" || display == "" {
			continue
		}
		items = append(items, fmt.Sprintf("%s：%s", label, display))
		if len(items) >= 2 {
			break
		}
	}
	if len(items) == 0 {
		return fmt.Sprintf("共 %d 个参数", len(fields))
	}
	if len(fields) > len(items) {
		return strings.Join(items, "，") + fmt.Sprintf(" 等 %d 项", len(fields))
	}
	return strings.Join(items, "，")
}

func resolveArgumentLabelCN(key string) string {
	switch strings.TrimSpace(key) {
	case "task_id":
		return "任务"
	case "task_a":
		return "任务A"
	case "task_b":
		return "任务B"
	case "day":
		return "目标日期"
	case "new_day":
		return "目标日期"
	case "slot_start":
		return "目标时段"
	case "new_slot_start":
		return "目标时段"
	case "moves":
		return "移动列表"
	case "reason":
		return "原因"
	case "status":
		return "状态"
	case "limit":
		return "数量"
	case "query":
		return "查询内容"
	case "url":
		return "链接"
	default:
		return "参数"
	}
}

func formatArgumentDisplay(
	toolName string,
	key string,
	value any,
	args map[string]any,
	state *schedule.ScheduleState,
) string {
	switch key {
	case "task_id", "task_a", "task_b":
		if taskID, ok := toInt(value); ok {
			return resolveTaskLabelByID(state, taskID, true)
		}
	case "day", "new_day":
		if day, ok := toInt(value); ok {
			return formatDayLabelCN(day)
		}
	case "slot_start", "new_slot_start":
		if slotStart, ok := toInt(value); ok {
			slotEnd := slotStart
			if taskID, ok := toInt(args["task_id"]); ok {
				if task := stateTaskByID(state, taskID); task != nil && task.Duration > 1 {
					slotEnd = slotStart + task.Duration - 1
				}
			}
			if day, ok := toInt(args["day"]); ok {
				return fmt.Sprintf("%s%s", formatDayLabelCN(day), formatSlotRangeCN(slotStart, slotEnd))
			}
			if day, ok := toInt(args["new_day"]); ok {
				return fmt.Sprintf("%s%s", formatDayLabelCN(day), formatSlotRangeCN(slotStart, slotEnd))
			}
			return formatSlotRangeCN(slotStart, slotEnd)
		}
	case "moves":
		return formatMovesArgumentCN(value, state)
	}
	return formatAnyValueCN(value)
}

func formatMovesArgumentCN(value any, state *schedule.ScheduleState) string {
	list, ok := value.([]any)
	if !ok {
		return formatAnyValueCN(value)
	}
	if len(list) == 0 {
		return "空"
	}
	parts := make([]string, 0, len(list))
	for _, item := range list {
		move, ok := item.(map[string]any)
		if !ok {
			continue
		}
		taskID, _ := toInt(move["task_id"])
		day, _ := toInt(move["new_day"])
		slotStart, _ := toInt(move["new_slot_start"])
		taskLabel := resolveTaskLabelByID(state, taskID, false)
		slotEnd := slotStart
		if task := stateTaskByID(state, taskID); task != nil && task.Duration > 1 {
			slotEnd = slotStart + task.Duration - 1
		}
		if day > 0 && slotStart > 0 {
			parts = append(parts, fmt.Sprintf("%s→%s%s", taskLabel, formatDayLabelCN(day), formatSlotRangeCN(slotStart, slotEnd)))
		}
	}
	if len(parts) == 0 {
		return fmt.Sprintf("%d 项", len(list))
	}
	if len(parts) > 3 {
		return strings.Join(parts[:3], "；") + fmt.Sprintf(" 等 %d 项", len(parts))
	}
	return strings.Join(parts, "；")
}

func formatAnyValueCN(value any) string {
	switch typed := value.(type) {
	case string:
		text := strings.TrimSpace(typed)
		if text == "" {
			return "空"
		}
		return text
	case int:
		return fmt.Sprintf("%d", typed)
	case int8:
		return fmt.Sprintf("%d", typed)
	case int16:
		return fmt.Sprintf("%d", typed)
	case int32:
		return fmt.Sprintf("%d", typed)
	case int64:
		return fmt.Sprintf("%d", typed)
	case float32:
		return fmt.Sprintf("%g", typed)
	case float64:
		return fmt.Sprintf("%g", typed)
	case bool:
		if typed {
			return "是"
		}
		return "否"
	case []any:
		return fmt.Sprintf("%d 项", len(typed))
	default:
		if value == nil {
			return "空"
		}
		raw, err := json.Marshal(value)
		if err != nil {
			return fmt.Sprintf("%v", value)
		}
		return strings.TrimSpace(string(raw))
	}
}

func resolveTaskLabelByID(state *schedule.ScheduleState, taskID int, withID bool) string {
	if taskID <= 0 {
		return "未知任务"
	}
	task := stateTaskByID(state, taskID)
	if task == nil {
		if withID {
			return fmt.Sprintf("[%d]任务", taskID)
		}
		return fmt.Sprintf("任务%d", taskID)
	}
	name := strings.TrimSpace(task.Name)
	if name == "" {
		name = "任务"
	}
	if withID {
		return fmt.Sprintf("[%d]%s", task.StateID, name)
	}
	return name
}

func stateTaskByID(state *schedule.ScheduleState, taskID int) *schedule.ScheduleTask {
	if state == nil || taskID <= 0 {
		return nil
	}
	return state.TaskByStateID(taskID)
}

func formatDayLabelCN(day int) string {
	if day <= 0 {
		return "未知日期"
	}
	return fmt.Sprintf("第%d天", day)
}

func formatSlotRangeCN(start int, end int) string {
	if start <= 0 && end <= 0 {
		return "未知时段"
	}
	if end <= 0 {
		end = start
	}
	if end < start {
		end = start
	}
	return fmt.Sprintf("第%d-%d节", start, end)
}

func toInt(value any) (int, bool) {
	switch typed := value.(type) {
	case int:
		return typed, true
	case int8:
		return int(typed), true
	case int16:
		return int(typed), true
	case int32:
		return int(typed), true
	case int64:
		return int(typed), true
	case float32:
		return int(typed), true
	case float64:
		return int(typed), true
	default:
		return 0, false
	}
}

func parseObservationJSON(text string) (map[string]any, bool) {
	var payload map[string]any
	if err := json.Unmarshal([]byte(text), &payload); err != nil {
		return nil, false
	}
	return payload, true
}

func readStringFromMap(payload map[string]any, keys ...string) (string, bool) {
	for _, key := range keys {
		raw, exists := payload[key]
		if !exists || raw == nil {
			continue
		}
		text := strings.TrimSpace(fmt.Sprintf("%v", raw))
		if text == "" || text == "<nil>" {
			continue
		}
		return text, true
	}
	return "", false
}

func readBoolFromMap(payload map[string]any, key string) (bool, bool) {
	raw, exists := payload[key]
	if !exists {
		return false, false
	}
	value, ok := raw.(bool)
	return value, ok
}

func cloneAnyMap(input map[string]any) map[string]any {
	if len(input) == 0 {
		return nil
	}
	out := make(map[string]any, len(input))
	for k, v := range input {
		out[k] = v
	}
	return out
}
