package agentexecute

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/LoveLosita/smartflow/backend/services/agent/tools/schedule"
)

func summarizeScheduleStateForDebug(state *schedule.ScheduleState) string {
	if state == nil {
		return "state=nil"
	}

	total := len(state.Tasks)
	pendingNoSlot := 0
	suggestedTotal := 0
	existingTotal := 0
	taskItemWithSlot := 0
	eventWithSlot := 0

	for i := range state.Tasks {
		t := &state.Tasks[i]
		hasSlot := len(t.Slots) > 0

		switch {
		case schedule.IsPendingTask(*t):
			pendingNoSlot++
		case schedule.IsSuggestedTask(*t):
			suggestedTotal++
		case schedule.IsExistingTask(*t):
			existingTotal++
		}

		if hasSlot {
			if t.Source == "task_item" {
				taskItemWithSlot++
			}
			if t.Source == "event" {
				eventWithSlot++
			}
		}
	}

	return fmt.Sprintf(
		"tasks=%d pending=%d suggested=%d existing=%d task_item_with_slot=%d event_with_slot=%d",
		total,
		pendingNoSlot,
		suggestedTotal,
		existingTotal,
		taskItemWithSlot,
		eventWithSlot,
	)
}

func marshalArgsForDebug(args map[string]any) string {
	if len(args) == 0 {
		return "{}"
	}
	raw, err := json.Marshal(args)
	if err != nil {
		return "<marshal_error>"
	}
	return string(raw)
}

func flattenForLog(text string) string {
	text = strings.ReplaceAll(text, "\n", " ")
	text = strings.ReplaceAll(text, "\r", " ")
	return strings.TrimSpace(text)
}

func resolveToolEventResultStatus(result string) string {
	normalized := strings.TrimSpace(result)
	if normalized == "" {
		return "done"
	}
	if strings.Contains(normalized, "失败") {
		return "failed"
	}
	lower := strings.ToLower(normalized)
	if strings.Contains(lower, "error") || strings.Contains(lower, "failed") {
		return "failed"
	}
	return "done"
}

func buildToolEventResultSummary(result string) string {
	flat := flattenForLog(result)
	if flat == "" {
		return "工具已执行完成。"
	}

	if summary, ok := tryExtractToolResultSummaryCN(flat); ok {
		return summary
	}

	runes := []rune(flat)
	if len(runes) <= 48 {
		return flat
	}
	return string(runes[:48]) + "..."
}

func tryExtractToolResultSummaryCN(raw string) (string, bool) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return "", false
	}

	var payload map[string]any
	if err := json.Unmarshal([]byte(trimmed), &payload); err != nil {
		return "", false
	}

	toolRaw := strings.TrimSpace(readStringAnyFromMap(payload, "tool"))
	toolName := resolveToolDisplayNameCN(toolRaw)

	if strings.EqualFold(toolRaw, "upsert_task_class") {
		if summary, ok := buildUpsertTaskClassSummaryCN(payload); ok {
			return truncateToolSummaryCN(summary), true
		}
	}

	if errText := strings.TrimSpace(readStringAnyFromMap(payload, "error", "err")); errText != "" {
		return truncateToolSummaryCN(fmt.Sprintf("%s失败：%s", toolName, errText)), true
	}

	if success, exists := payload["success"]; exists {
		if ok, isBool := success.(bool); isBool && !ok {
			reason := strings.TrimSpace(readStringAnyFromMap(payload, "reason", "message"))
			if reason != "" {
				return truncateToolSummaryCN(fmt.Sprintf("%s失败：%s", toolName, reason)), true
			}
			return truncateToolSummaryCN(fmt.Sprintf("%s执行失败。", toolName)), true
		}
	}

	if message := strings.TrimSpace(readStringAnyFromMap(payload, "result", "message", "reason")); message != "" {
		return truncateToolSummaryCN(message), true
	}

	pending, hasPending := readIntAnyFromMap(payload, "pending_count")
	completed, hasCompleted := readIntAnyFromMap(payload, "completed_count")
	if hasPending || hasCompleted {
		skipped, _ := readIntAnyFromMap(payload, "skipped_count")
		return fmt.Sprintf("队列状态：待处理 %d，已完成 %d，已跳过 %d。", pending, completed, skipped), true
	}

	if hasHead, exists := payload["has_head"]; exists {
		if b, isBool := hasHead.(bool); isBool {
			if b {
				return "已获取当前队首任务。", true
			}
			return "当前队列没有可处理任务。", true
		}
	}

	if _, ok := payload["slot_candidates"]; ok {
		if total, exists := readIntAnyFromMap(payload, "total"); exists {
			return fmt.Sprintf("共找到 %d 个可用时段。", total), true
		}
	}

	if toolRaw != "" {
		return fmt.Sprintf("已完成“%s”操作。", toolName), true
	}

	return "", false
}

func buildUpsertTaskClassSummaryCN(payload map[string]any) (string, bool) {
	validationRaw, hasValidation := payload["validation"]
	if !hasValidation {
		return "", false
	}
	validation, ok := validationRaw.(map[string]any)
	if !ok {
		return "", false
	}

	validationOK, hasValidationOK := validation["ok"].(bool)
	issues := parseAnyToStringSlice(validation["issues"])

	if hasValidationOK && !validationOK {
		if len(issues) > 0 {
			return fmt.Sprintf("任务类写入未通过校验：%s。", strings.Join(issues, "；")), true
		}
		return "任务类写入未通过校验，请先补齐缺失字段。", true
	}

	success, hasSuccess := payload["success"].(bool)
	if hasSuccess && success {
		if taskClassID, ok := readIntAnyFromMap(payload, "task_class_id"); ok && taskClassID > 0 {
			return fmt.Sprintf("任务类写入成功，task_class_id=%d。", taskClassID), true
		}
		return "任务类写入成功。", true
	}

	return "", false
}

func truncateToolSummaryCN(text string) string {
	runes := []rune(strings.TrimSpace(text))
	if len(runes) <= 48 {
		return string(runes)
	}
	return string(runes[:48]) + "..."
}

func buildToolCallStartSummary(toolName string, args map[string]any) string {
	displayName := resolveToolDisplayNameCN(toolName)
	argSummary := buildToolArgumentsPreviewCN(args)
	if argSummary == "" {
		return fmt.Sprintf("已调用工具：%s。", displayName)
	}
	return fmt.Sprintf("已调用工具：%s（%s）。", displayName, argSummary)
}

func buildToolArgumentsPreviewCN(args map[string]any) string {
	if len(args) <= 0 {
		return ""
	}

	type argPair struct {
		Key   string
		Label string
	}

	orderedPairs := []argPair{
		{Key: "title", Label: "任务标题"},
		{Key: "task_name", Label: "任务名称"},
		{Key: "deadline_at", Label: "截止时间"},
		{Key: "new_day", Label: "目标日期"},
		{Key: "new_slot_start", Label: "目标开始时段"},
		{Key: "day", Label: "日期"},
		{Key: "day_start", Label: "开始日"},
		{Key: "day_end", Label: "结束日"},
		{Key: "day_scope", Label: "日期范围"},
		{Key: "day_of_week", Label: "星期"},
		{Key: "week", Label: "周"},
		{Key: "week_from", Label: "起始周"},
		{Key: "week_to", Label: "结束周"},
		{Key: "week_filter", Label: "周筛选"},
		{Key: "slot_start", Label: "开始时段"},
		{Key: "slot_end", Label: "结束时段"},
		{Key: "slot_type", Label: "时段类型"},
		{Key: "slot_types", Label: "时段类型"},
		{Key: "task_id", Label: "任务 ID"},
		{Key: "task_ids", Label: "任务 ID 列表"},
		{Key: "task_item_id", Label: "任务项 ID"},
		{Key: "task_item_ids", Label: "任务项 ID 列表"},
		{Key: "query", Label: "查询词"},
		{Key: "keyword", Label: "关键词"},
		{Key: "domain", Label: "工具域"},
		{Key: "mode", Label: "激活模式"},
		{Key: "all", Label: "移除全部"},
		{Key: "top_k", Label: "返回数量"},
		{Key: "url", Label: "链接"},
		{Key: "reason", Label: "原因"},
		{Key: "limit", Label: "数量"},
	}

	items := make([]string, 0, 2)
	for _, pair := range orderedPairs {
		rawValue, exists := args[pair.Key]
		if !exists {
			continue
		}
		valueText := formatToolArgValueByKeyCN(pair.Key, rawValue)
		if valueText == "" {
			continue
		}
		items = append(items, fmt.Sprintf("%s：%s", pair.Label, valueText))
		if len(items) >= 2 {
			break
		}
	}

	return strings.Join(items, "，")
}

func resolveToolDisplayNameCN(toolName string) string {
	name := strings.TrimSpace(toolName)
	if name == "" {
		return "未知工具"
	}

	displayNameMap := map[string]string{
		"get_overview":          "查看总览",
		"query_range":           "查询时间范围",
		"queue_status":          "查看任务队列",
		"queue_pop_head":        "获取队首任务",
		"queue_apply_head_move": "应用队首任务时段",
		"queue_skip_head":       "跳过队首任务",
		"query_target_tasks":    "查询目标任务",
		"query_available_slots": "查询可用时段",
		"get_task_info":         "查看任务信息",
		"analyze_health":        "综合体检",
		"analyze_rhythm":        "分析学习节律",
		"web_search":            "网页搜索",
		"web_fetch":             "网页抓取",
		"move":                  "移动任务",
		"place":                 "放置任务",
		"swap":                  "交换任务",
		"batch_move":            "批量移动任务",
		"unplace":               "移出任务安排",
		"upsert_task_class":     "写入任务类",
		"context_tools_add":     "激活工具域",
		"context_tools_remove":  "移除工具域",
	}

	if label, ok := displayNameMap[name]; ok {
		return label
	}
	return name
}

func formatToolArgValueByKeyCN(key string, value any) string {
	switch key {
	case "day_scope":
		scope := strings.ToLower(strings.TrimSpace(formatToolArgValueCN(value)))
		switch scope {
		case "workday":
			return "工作日"
		case "weekend":
			return "周末"
		case "all":
			return "全部日期"
		default:
			return scope
		}
	case "day_of_week":
		weekdays := parseAnyToIntSlice(value)
		if len(weekdays) <= 0 {
			return formatToolArgValueCN(value)
		}
		labels := make([]string, 0, len(weekdays))
		for _, day := range weekdays {
			labels = append(labels, fmt.Sprintf("周%d", day))
			if len(labels) >= 4 {
				break
			}
		}
		return strings.Join(labels, "、")
	case "task_ids", "task_item_ids", "week_filter":
		values := parseAnyToIntSlice(value)
		if len(values) <= 0 {
			return formatToolArgValueCN(value)
		}
		items := make([]string, 0, len(values))
		for _, current := range values {
			items = append(items, strconv.Itoa(current))
			if len(items) >= 4 {
				break
			}
		}
		return strings.Join(items, "、")
	case "url":
		return truncateToolSummaryCN(formatToolArgValueCN(value))
	case "reason", "title", "task_name", "query", "keyword":
		return truncateToolSummaryCN(formatToolArgValueCN(value))
	default:
		return formatToolArgValueCN(value)
	}
}

func formatToolArgValueCN(value any) string {
	switch v := value.(type) {
	case string:
		text := strings.TrimSpace(v)
		if text == "" {
			return ""
		}
		return text
	case int:
		return strconv.Itoa(v)
	case int8:
		return strconv.Itoa(int(v))
	case int16:
		return strconv.Itoa(int(v))
	case int32:
		return strconv.Itoa(int(v))
	case int64:
		return strconv.Itoa(int(v))
	case float32:
		return strings.TrimSpace(strconv.FormatFloat(float64(v), 'f', -1, 32))
	case float64:
		return strings.TrimSpace(strconv.FormatFloat(v, 'f', -1, 64))
	case bool:
		if v {
			return "是"
		}
		return "否"
	case []any:
		values := make([]string, 0, len(v))
		for _, item := range v {
			text := formatToolArgValueCN(item)
			if text == "" {
				continue
			}
			values = append(values, text)
			if len(values) >= 3 {
				break
			}
		}
		return strings.Join(values, "、")
	default:
		if value == nil {
			return ""
		}
		text := strings.TrimSpace(fmt.Sprintf("%v", value))
		if text == "" || text == "<nil>" || text == "map[]" {
			return ""
		}
		return text
	}
}
