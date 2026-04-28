package newagenttools

import (
	"strings"

	"github.com/LoveLosita/smartflow/backend/model"
	"github.com/LoveLosita/smartflow/backend/newAgent/tools/schedule"
	taskclassresult "github.com/LoveLosita/smartflow/backend/newAgent/tools/taskclass_result"
)

type taskClassUpsertExecutionInput struct {
	Result     taskClassUpsertToolResult
	Normalized *TaskClassUpsertInput
}

// NewTaskClassUpsertToolHandler 返回 upsert_task_class 的结构化结果 handler。
//
// 职责边界：
// 1. 只负责参数解析、校验、调用依赖与包装结构化结果；
// 2. 不改变既有写库语义、confirm 语义与 observation JSON 合约；
// 3. 老实现暂以 legacy 函数保留，便于本轮并行迁移后回溯与对照。
func NewTaskClassUpsertToolHandler(deps TaskClassWriteDeps) ToolHandler {
	return func(state *schedule.ScheduleState, args map[string]any) ToolExecutionResult {
		_ = state

		if deps.UpsertTaskClass == nil {
			return buildTaskClassUpsertExecutionResult(args, taskClassUpsertExecutionInput{
				Result: taskClassUpsertToolResult{
					Tool:       "upsert_task_class",
					Success:    false,
					Validation: taskClassValidationResult{OK: false, Issues: []string{"任务类写库依赖未注入"}},
					Error:      "任务类写库依赖未注入",
					ErrorCode:  "dependency_missing",
				},
			})
		}

		userID, ok := readUpsertUserID(args["_user_id"])
		if !ok || userID <= 0 {
			return buildTaskClassUpsertExecutionResult(args, taskClassUpsertExecutionInput{
				Result: taskClassUpsertToolResult{
					Tool:       "upsert_task_class",
					Success:    false,
					Validation: taskClassValidationResult{OK: false, Issues: []string{"无法识别用户身份"}},
					Error:      "工具调用失败：无法识别用户身份",
					ErrorCode:  "missing_user_id",
				},
			})
		}

		input, parseErr := parseTaskClassUpsertInput(args)
		if parseErr != nil {
			return buildTaskClassUpsertExecutionResult(args, taskClassUpsertExecutionInput{
				Result: taskClassUpsertToolResult{
					Tool:       "upsert_task_class",
					Success:    false,
					Validation: taskClassValidationResult{OK: false, Issues: []string{parseErr.Error()}},
					Error:      parseErr.Error(),
					ErrorCode:  "invalid_args",
				},
			})
		}

		issues := validateTaskClassUpsertRequest(input.Request, input.ID)
		if len(issues) > 0 {
			return buildTaskClassUpsertExecutionResult(args, taskClassUpsertExecutionInput{
				Result: taskClassUpsertToolResult{
					Tool:       "upsert_task_class",
					Success:    false,
					Validation: taskClassValidationResult{OK: false, Issues: issues},
					Error:      strings.Join(issues, "；"),
					ErrorCode:  "validation_failed",
				},
				Normalized: &input,
			})
		}

		result, err := deps.UpsertTaskClass(userID, input)
		if err != nil {
			return buildTaskClassUpsertExecutionResult(args, taskClassUpsertExecutionInput{
				Result: taskClassUpsertToolResult{
					Tool:       "upsert_task_class",
					Success:    false,
					Validation: taskClassValidationResult{OK: false, Issues: []string{"持久化写入失败"}},
					Error:      err.Error(),
					ErrorCode:  "persist_failed",
				},
				Normalized: &input,
			})
		}
		if result.TaskClassID <= 0 {
			return buildTaskClassUpsertExecutionResult(args, taskClassUpsertExecutionInput{
				Result: taskClassUpsertToolResult{
					Tool:       "upsert_task_class",
					Success:    false,
					Validation: taskClassValidationResult{OK: false, Issues: []string{"未返回有效 task_class_id"}},
					Error:      "写入后未返回有效 task_class_id",
					ErrorCode:  "invalid_persist_result",
				},
				Normalized: &input,
			})
		}

		return buildTaskClassUpsertExecutionResult(args, taskClassUpsertExecutionInput{
			Result: taskClassUpsertToolResult{
				Tool:        "upsert_task_class",
				Success:     true,
				TaskClassID: result.TaskClassID,
				Created:     result.Created,
				Validation:  taskClassValidationResult{OK: true, Issues: []string{}},
				Error:       "",
				ErrorCode:   "",
			},
			Normalized: &input,
		})
	}
}

// buildTaskClassUpsertExecutionResult 负责把 upsert_task_class 的原始 observation 包装成结构化卡片。
//
// 步骤化说明：
// 1. 先沿用 LegacyResult 保留 observation、参数预览、错误码提取等既有链路能力；
// 2. 再把规范化请求摘要和写入结果投影到 taskclass_result 子包，避免展示层反向依赖父包；
// 3. 最后只替换 ResultView / Summary，不改写写库语义、confirm 语义和原始错误文本。
func buildTaskClassUpsertExecutionResult(
	args map[string]any,
	input taskClassUpsertExecutionInput,
) ToolExecutionResult {
	observation := marshalTaskClassUpsertResult(input.Result)
	legacy := LegacyResult("upsert_task_class", args, observation)

	requestSummary := buildTaskClassRequestSummary(args, input.Normalized)
	view := taskclassresult.BuildUpsertTaskClassView(taskclassresult.BuildUpsertTaskClassViewInput{
		Status:      legacy.Status,
		Observation: observation,
		Result: taskclassresult.UpsertResult{
			Tool:             strings.TrimSpace(input.Result.Tool),
			Success:          input.Result.Success,
			TaskClassID:      input.Result.TaskClassID,
			Created:          input.Result.Created,
			Error:            strings.TrimSpace(input.Result.Error),
			ErrorCode:        strings.TrimSpace(input.Result.ErrorCode),
			ValidationOK:     input.Result.Validation.OK,
			ValidationIssues: append([]string(nil), input.Result.Validation.Issues...),
		},
		Request:        requestSummary,
		MachinePayload: buildTaskClassMachinePayload(input.Result, requestSummary),
	})
	return buildTaskClassWriteExecutionResult(legacy, args, view)
}

// buildTaskClassWriteExecutionResult 负责把子包纯展示视图包回父包统一协议。
func buildTaskClassWriteExecutionResult(
	legacy ToolExecutionResult,
	args map[string]any,
	view taskclassresult.WriteResultView,
) ToolExecutionResult {
	result := legacy
	status := normalizeToolStatus(result.Status)
	if status == "" {
		status = ToolStatusDone
	}
	if collapsedStatus, ok := readStringAnyMap(view.Collapsed, "status"); ok {
		if normalized := normalizeToolStatus(collapsedStatus); normalized != "" {
			status = normalized
		}
	}

	collapsed := cloneAnyMap(view.Collapsed)
	if collapsed == nil {
		collapsed = make(map[string]any)
	}
	expanded := cloneAnyMap(view.Expanded)
	if expanded == nil {
		expanded = make(map[string]any)
	}

	collapsed["status"] = status
	if _, exists := collapsed["status_label"]; !exists {
		collapsed["status_label"] = resolveToolStatusLabelCN(status)
	}
	if _, exists := expanded["raw_text"]; !exists {
		expanded["raw_text"] = result.ObservationText
	}

	viewType := strings.TrimSpace(view.ViewType)
	if viewType == "" {
		viewType = taskclassresult.ViewTypeWriteResult
	}
	version := view.Version
	if version <= 0 {
		version = taskclassresult.ViewVersionWriteResult
	}

	result.Status = status
	result.Success = status == ToolStatusDone
	result.ResultView = &ToolDisplayView{
		ViewType:  viewType,
		Version:   version,
		Collapsed: collapsed,
		Expanded:  expanded,
	}
	if title, ok := readStringAnyMap(collapsed, "title"); ok {
		result.Summary = title
	}
	if !result.Success {
		errorCode, errorMessage := extractToolErrorInfo(result.ObservationText, status)
		if strings.TrimSpace(result.ErrorCode) == "" {
			result.ErrorCode = strings.TrimSpace(errorCode)
		}
		if strings.TrimSpace(result.ErrorMessage) == "" {
			result.ErrorMessage = strings.TrimSpace(errorMessage)
		}
	}
	return EnsureToolResultDefaults(result, args)
}

func buildTaskClassRequestSummary(
	args map[string]any,
	normalized *TaskClassUpsertInput,
) taskclassresult.RequestSummary {
	summary := buildTaskClassRequestSummaryFromArgs(args)
	if normalized == nil {
		return summary
	}

	summary.RequestedID = normalized.ID
	summary.Name = strings.TrimSpace(normalized.Request.Name)
	summary.Mode = strings.TrimSpace(normalized.Request.Mode)
	summary.StartDate = strings.TrimSpace(normalized.Request.StartDate)
	summary.EndDate = strings.TrimSpace(normalized.Request.EndDate)
	summary.SubjectType = strings.TrimSpace(normalized.Request.SubjectType)
	summary.DifficultyLevel = strings.TrimSpace(normalized.Request.DifficultyLevel)
	summary.CognitiveIntensity = strings.TrimSpace(normalized.Request.CognitiveIntensity)
	summary.TotalSlots = normalized.Request.Config.TotalSlots
	summary.AllowFillerCourse = normalized.Request.Config.AllowFillerCourse
	summary.Strategy = strings.TrimSpace(normalized.Request.Config.Strategy)
	summary.ExcludedSlots = cloneIntSlice(normalized.Request.Config.ExcludedSlots)
	summary.ExcludedDaysOfWeek = cloneIntSlice(normalized.Request.Config.ExcludedDaysOfWeek)
	summary.Source = strings.TrimSpace(normalized.Source)
	summary.Items = buildTaskClassItemsSummary(normalized.Request.Items)
	return summary
}

func buildTaskClassRequestSummaryFromArgs(args map[string]any) taskclassresult.RequestSummary {
	summary := taskclassresult.RequestSummary{
		RequestedID:        readOptionalInt(args, "id"),
		Source:             readOptionalString(args, "source"),
		Items:              make([]taskclassresult.TaskClassItemSummary, 0),
		ExcludedSlots:      make([]int, 0),
		ExcludedDaysOfWeek: make([]int, 0),
	}

	taskClassMap, _ := readAnyMap(args["task_class"])
	if taskClassMap == nil {
		return summary
	}

	summary.Name = strings.TrimSpace(readAnyString(taskClassMap["name"]))
	summary.Mode = strings.TrimSpace(readAnyString(taskClassMap["mode"]))
	summary.StartDate = strings.TrimSpace(readAnyString(taskClassMap["start_date"]))
	summary.EndDate = strings.TrimSpace(readAnyString(taskClassMap["end_date"]))
	summary.SubjectType = strings.TrimSpace(readAnyString(taskClassMap["subject_type"]))
	summary.DifficultyLevel = strings.TrimSpace(readAnyString(taskClassMap["difficulty_level"]))
	summary.CognitiveIntensity = strings.TrimSpace(readAnyString(taskClassMap["cognitive_intensity"]))

	configMap, _ := readAnyMap(taskClassMap["config"])
	summary.TotalSlots = readAnyInt(configMap["total_slots"])
	summary.AllowFillerCourse = readAnyBool(configMap["allow_filler_course"])
	summary.Strategy = strings.TrimSpace(readAnyString(configMap["strategy"]))
	summary.ExcludedSlots = readAnyIntSlice(configMap["excluded_slots"])
	summary.ExcludedDaysOfWeek = readAnyIntSlice(configMap["excluded_days_of_week"])

	rawItems := taskClassMap["items"]
	if topLevelItems, exists := args["items"]; exists {
		rawItems = topLevelItems
	}
	summary.Items = buildTaskClassItemsSummaryFromRaw(rawItems)
	return summary
}

func buildTaskClassItemsSummary(items []model.UserAddTaskClassItemRequest) []taskclassresult.TaskClassItemSummary {
	if len(items) == 0 {
		return make([]taskclassresult.TaskClassItemSummary, 0)
	}
	out := make([]taskclassresult.TaskClassItemSummary, 0, len(items))
	for _, item := range items {
		summary := taskclassresult.TaskClassItemSummary{
			ID:      item.ID,
			Order:   item.Order,
			Content: strings.TrimSpace(item.Content),
		}
		if item.EmbeddedTime != nil {
			summary.EmbeddedWeek = item.EmbeddedTime.Week
			summary.EmbeddedDay = item.EmbeddedTime.DayOfWeek
			summary.EmbeddedSectionFrom = item.EmbeddedTime.SectionFrom
			summary.EmbeddedSectionTo = item.EmbeddedTime.SectionTo
		}
		out = append(out, summary)
	}
	return out
}

func buildTaskClassItemsSummaryFromRaw(raw any) []taskclassresult.TaskClassItemSummary {
	rawList, ok := raw.([]any)
	if !ok || len(rawList) == 0 {
		return make([]taskclassresult.TaskClassItemSummary, 0)
	}
	out := make([]taskclassresult.TaskClassItemSummary, 0, len(rawList))
	for index, row := range rawList {
		itemMap, ok := row.(map[string]any)
		if !ok {
			continue
		}
		summary := taskclassresult.TaskClassItemSummary{
			ID:    readAnyInt(itemMap["id"]),
			Order: maxPositiveInt(readAnyInt(itemMap["order"]), index+1),
			Content: strings.TrimSpace(firstNonEmptyString(
				readAnyString(itemMap["content"]),
				readAnyString(itemMap["description"]),
				readAnyString(itemMap["title"]),
				readAnyString(itemMap["name"]),
			)),
		}
		if embeddedMap, ok := readAnyMap(itemMap["embedded_time"]); ok {
			summary.EmbeddedWeek = readAnyInt(embeddedMap["week"])
			summary.EmbeddedDay = readAnyInt(embeddedMap["day_of_week"])
			summary.EmbeddedSectionFrom = readAnyInt(embeddedMap["section_from"])
			summary.EmbeddedSectionTo = readAnyInt(embeddedMap["section_to"])
		}
		out = append(out, summary)
	}
	return out
}

func buildTaskClassMachinePayload(
	result taskClassUpsertToolResult,
	request taskclassresult.RequestSummary,
) map[string]any {
	return map[string]any{
		"parsed_result": map[string]any{
			"tool":          strings.TrimSpace(result.Tool),
			"success":       result.Success,
			"task_class_id": result.TaskClassID,
			"created":       result.Created,
			"validation": map[string]any{
				"ok":     result.Validation.OK,
				"issues": append([]string(nil), result.Validation.Issues...),
			},
			"error":      strings.TrimSpace(result.Error),
			"error_code": strings.TrimSpace(result.ErrorCode),
		},
		"input_summary": map[string]any{
			"requested_id":          request.RequestedID,
			"name":                  request.Name,
			"mode":                  request.Mode,
			"start_date":            request.StartDate,
			"end_date":              request.EndDate,
			"subject_type":          request.SubjectType,
			"difficulty_level":      request.DifficultyLevel,
			"cognitive_intensity":   request.CognitiveIntensity,
			"total_slots":           request.TotalSlots,
			"allow_filler_course":   request.AllowFillerCourse,
			"strategy":              request.Strategy,
			"excluded_slots":        cloneIntSlice(request.ExcludedSlots),
			"excluded_days_of_week": cloneIntSlice(request.ExcludedDaysOfWeek),
			"source":                request.Source,
			"items":                 buildTaskClassItemMachinePayload(request.Items),
		},
	}
}

func buildTaskClassItemMachinePayload(items []taskclassresult.TaskClassItemSummary) []map[string]any {
	if len(items) == 0 {
		return make([]map[string]any, 0)
	}
	out := make([]map[string]any, 0, len(items))
	for _, item := range items {
		out = append(out, map[string]any{
			"id":            item.ID,
			"order":         item.Order,
			"content":       item.Content,
			"embedded_week": item.EmbeddedWeek,
			"embedded_day":  item.EmbeddedDay,
			"section_from":  item.EmbeddedSectionFrom,
			"section_to":    item.EmbeddedSectionTo,
		})
	}
	return out
}

func readOptionalString(args map[string]any, key string) string {
	if len(args) == 0 {
		return ""
	}
	return strings.TrimSpace(readAnyString(args[key]))
}

func readOptionalInt(args map[string]any, key string) int {
	if len(args) == 0 {
		return 0
	}
	return readAnyInt(args[key])
}

func readAnyInt(raw any) int {
	value, ok := readUpsertInt(raw)
	if !ok {
		return 0
	}
	return value
}

func readAnyBool(raw any) bool {
	value, ok := raw.(bool)
	return ok && value
}

func readAnyIntSlice(raw any) []int {
	switch typed := raw.(type) {
	case []int:
		return cloneIntSlice(typed)
	case []any:
		out := make([]int, 0, len(typed))
		for _, item := range typed {
			value, ok := readUpsertInt(item)
			if !ok {
				continue
			}
			out = append(out, value)
		}
		return out
	default:
		return make([]int, 0)
	}
}

func cloneIntSlice(values []int) []int {
	if len(values) == 0 {
		return make([]int, 0)
	}
	out := make([]int, len(values))
	copy(out, values)
	return out
}

func maxPositiveInt(left int, right int) int {
	if left <= 0 {
		return right
	}
	if right <= 0 {
		return left
	}
	if left > right {
		return left
	}
	return right
}
