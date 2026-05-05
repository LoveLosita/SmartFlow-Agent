package agenttools

import (
	"fmt"
	"strings"

	"github.com/LoveLosita/smartflow/backend/services/agent/tools/schedule"
	scheduleread "github.com/LoveLosita/smartflow/backend/services/agent/tools/schedule_read"
)

type scheduleReadObserveFunc func(state *schedule.ScheduleState, args map[string]any) (string, *ToolExecutionResult)
type scheduleReadViewBuilder func(input scheduleReadAdapterInput) scheduleread.ReadResultView

// scheduleReadAdapterInput 是父包传给 schedule_read 子包前的最小上下文。
//
// 职责边界：
// 1. 只携带展示构造需要的 state、args、observation 与已本地化参数字段；
// 2. 不把 ToolExecutionResult / ToolArgumentView 传入子包，避免反向依赖父包；
// 3. ObservationText 必须原样来自底层 schedule 工具，不在 adapter 层改写。
type scheduleReadAdapterInput struct {
	ToolName        string
	Args            map[string]any
	State           *schedule.ScheduleState
	ObservationText string
	ArgFields       []scheduleread.KVField
}

// NewQueryAvailableSlotsToolHandler 为 query_available_slots 生成结构化读结果。
func NewQueryAvailableSlotsToolHandler() ToolHandler {
	return newScheduleReadToolHandler(
		"query_available_slots",
		func(state *schedule.ScheduleState, args map[string]any) (string, *ToolExecutionResult) {
			return schedule.QueryAvailableSlots(state, args), nil
		},
		func(input scheduleReadAdapterInput) scheduleread.ReadResultView {
			return scheduleread.BuildAvailableSlotsView(scheduleread.AvailableSlotsViewInput{
				State:       input.State,
				Observation: input.ObservationText,
				ArgFields:   input.ArgFields,
			})
		},
	)
}

// NewQueryRangeToolHandler 为 query_range 生成结构化读结果。
func NewQueryRangeToolHandler() ToolHandler {
	return newScheduleReadToolHandler(
		"query_range",
		func(state *schedule.ScheduleState, args map[string]any) (string, *ToolExecutionResult) {
			day, ok := schedule.ArgsInt(args, "day")
			if !ok {
				result := buildScheduleReadFailureResult("query_range", args, state, "查询失败：缺少必填参数 day。")
				return "", &result
			}
			if state == nil {
				result := buildScheduleReadFailureResult("query_range", args, nil, "查询失败：日程状态为空，无法读取时间范围。")
				return "", &result
			}
			return schedule.QueryRange(state, day, schedule.ArgsIntPtr(args, "slot_start"), schedule.ArgsIntPtr(args, "slot_end")), nil
		},
		func(input scheduleReadAdapterInput) scheduleread.ReadResultView {
			day, _ := schedule.ArgsInt(input.Args, "day")
			return scheduleread.BuildRangeView(scheduleread.RangeViewInput{
				State:       input.State,
				Observation: input.ObservationText,
				Day:         day,
				SlotStart:   schedule.ArgsIntPtr(input.Args, "slot_start"),
				SlotEnd:     schedule.ArgsIntPtr(input.Args, "slot_end"),
				ArgFields:   input.ArgFields,
			})
		},
	)
}

// NewQueryTargetTasksToolHandler 为 query_target_tasks 生成结构化读结果。
func NewQueryTargetTasksToolHandler() ToolHandler {
	return newScheduleReadToolHandler(
		"query_target_tasks",
		func(state *schedule.ScheduleState, args map[string]any) (string, *ToolExecutionResult) {
			return schedule.QueryTargetTasks(state, args), nil
		},
		func(input scheduleReadAdapterInput) scheduleread.ReadResultView {
			return scheduleread.BuildTargetTasksView(scheduleread.TargetTasksViewInput{
				State:       input.State,
				Observation: input.ObservationText,
				ArgFields:   input.ArgFields,
			})
		},
	)
}

// NewGetTaskInfoToolHandler 为 get_task_info 生成结构化读结果。
func NewGetTaskInfoToolHandler() ToolHandler {
	return newScheduleReadToolHandler(
		"get_task_info",
		func(state *schedule.ScheduleState, args map[string]any) (string, *ToolExecutionResult) {
			taskID, ok := schedule.ArgsInt(args, "task_id")
			if !ok {
				result := buildScheduleReadFailureResult("get_task_info", args, state, "查询失败：缺少必填参数 task_id。")
				return "", &result
			}
			if state == nil {
				result := buildScheduleReadFailureResult("get_task_info", args, nil, "查询失败：日程状态为空，无法读取任务详情。")
				return "", &result
			}
			return schedule.GetTaskInfo(state, taskID), nil
		},
		func(input scheduleReadAdapterInput) scheduleread.ReadResultView {
			taskID, _ := schedule.ArgsInt(input.Args, "task_id")
			return scheduleread.BuildTaskInfoView(scheduleread.TaskInfoViewInput{
				State:       input.State,
				Observation: input.ObservationText,
				TaskID:      taskID,
				ArgFields:   input.ArgFields,
			})
		},
	)
}

// NewGetOverviewToolHandler 为 get_overview 生成结构化读结果。
func NewGetOverviewToolHandler() ToolHandler {
	return newScheduleReadToolHandler(
		"get_overview",
		func(state *schedule.ScheduleState, args map[string]any) (string, *ToolExecutionResult) {
			if state == nil {
				result := buildScheduleReadFailureResult("get_overview", args, nil, "查看总览失败：日程状态为空，无法读取总览。")
				return "", &result
			}
			return schedule.GetOverview(state), nil
		},
		func(input scheduleReadAdapterInput) scheduleread.ReadResultView {
			return scheduleread.BuildOverviewView(scheduleread.OverviewViewInput{
				State:       input.State,
				Observation: input.ObservationText,
				ArgFields:   input.ArgFields,
			})
		},
	)
}

// NewQueueStatusToolHandler 为 queue_status 生成结构化读结果。
func NewQueueStatusToolHandler() ToolHandler {
	return newScheduleReadToolHandler(
		"queue_status",
		func(state *schedule.ScheduleState, args map[string]any) (string, *ToolExecutionResult) {
			observation := schedule.QueueStatus(state, args)
			if state == nil {
				result := buildScheduleReadFailureResult("queue_status", args, nil, observation)
				return "", &result
			}
			return observation, nil
		},
		func(input scheduleReadAdapterInput) scheduleread.ReadResultView {
			return scheduleread.BuildQueueStatusView(scheduleread.QueueStatusViewInput{
				State:       input.State,
				Observation: input.ObservationText,
				ArgFields:   input.ArgFields,
			})
		},
	)
}

// newScheduleReadToolHandler 统一构造父包 read adapter。
//
// 步骤化说明：
// 1. 先执行底层 schedule 工具，拿到原始 observation，保证 LLM 观察文本不变；
// 2. 再用 LegacyResultWithState 复用父包状态判断、参数中文展示与默认字段；
// 3. 最后调用 schedule_read 子包生成纯展示视图，并包回 ToolExecutionResult。
func newScheduleReadToolHandler(
	toolName string,
	observe scheduleReadObserveFunc,
	buildView scheduleReadViewBuilder,
) ToolHandler {
	return func(state *schedule.ScheduleState, args map[string]any) ToolExecutionResult {
		observation, earlyResult := observe(state, args)
		if earlyResult != nil {
			return EnsureToolResultDefaults(*earlyResult, args)
		}

		legacy := LegacyResultWithState(toolName, args, state, observation)
		input := scheduleReadAdapterInput{
			ToolName:        toolName,
			Args:            cloneAnyMap(args),
			State:           state,
			ObservationText: observation,
			ArgFields:       extractScheduleReadArgumentFields(legacy.ArgumentView),
		}

		view := buildView(input)
		if normalizeToolStatus(legacy.Status) != ToolStatusDone {
			view = scheduleread.BuildFailureView(scheduleread.BuildFailureViewInput{
				ToolName:    toolName,
				Status:      legacy.Status,
				Observation: observation,
				ArgFields:   input.ArgFields,
			})
		}
		return buildScheduleReadExecutionResult(legacy, args, view)
	}
}

// buildScheduleReadFailureResult 用于底层工具执行前即可确定失败的参数/状态场景。
func buildScheduleReadFailureResult(
	toolName string,
	args map[string]any,
	state *schedule.ScheduleState,
	observation string,
) ToolExecutionResult {
	legacy := LegacyResultWithState(toolName, args, state, observation)
	view := scheduleread.BuildFailureView(scheduleread.BuildFailureViewInput{
		ToolName:    toolName,
		Status:      ToolStatusFailed,
		Observation: observation,
		ArgFields:   extractScheduleReadArgumentFields(legacy.ArgumentView),
	})
	return buildScheduleReadExecutionResult(legacy, args, view)
}

// buildScheduleReadExecutionResult 负责把子包纯展示视图包回父包统一协议。
//
// 职责边界：
// 1. 只做 ReadResultView -> ToolDisplayView 的协议桥接；
// 2. 不改写 ObservationText，确保 execute / SSE / timeline 仍使用同一份 observation；
// 3. 错误码与错误文案继续复用父包既有 JSON / 文本解析逻辑。
func buildScheduleReadExecutionResult(
	legacy ToolExecutionResult,
	args map[string]any,
	view scheduleread.ReadResultView,
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
		expanded["raw_text"] = strings.TrimSpace(result.ObservationText)
	}

	viewType := strings.TrimSpace(view.ViewType)
	if viewType == "" {
		viewType = scheduleread.ViewTypeReadResult
	}
	version := view.Version
	if version <= 0 {
		version = scheduleread.ViewVersionReadResult
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

// extractScheduleReadArgumentFields 把父包 ToolArgumentView 投影成子包可消费的 KVField。
func extractScheduleReadArgumentFields(view *ToolArgumentView) []scheduleread.KVField {
	if view == nil || view.Expanded == nil {
		return make([]scheduleread.KVField, 0)
	}
	rawFields, exists := view.Expanded["fields"]
	if !exists {
		return make([]scheduleread.KVField, 0)
	}

	fields := make([]scheduleread.KVField, 0)
	appendField := func(row map[string]any) {
		label, _ := row["label"].(string)
		display, _ := row["display"].(string)
		label = strings.TrimSpace(label)
		display = strings.TrimSpace(display)
		if label == "" || display == "" {
			return
		}
		fields = append(fields, scheduleread.BuildKVField(label, display))
	}

	switch typed := rawFields.(type) {
	case []map[string]any:
		for _, row := range typed {
			appendField(row)
		}
	case []any:
		for _, item := range typed {
			row, ok := item.(map[string]any)
			if !ok {
				continue
			}
			appendField(row)
		}
	}
	if len(fields) == 0 {
		return make([]scheduleread.KVField, 0)
	}
	return fields
}

func readStringAnyMap(payload map[string]any, key string) (string, bool) {
	if len(payload) == 0 {
		return "", false
	}
	raw, exists := payload[key]
	if !exists || raw == nil {
		return "", false
	}
	text := strings.TrimSpace(fmt.Sprintf("%v", raw))
	if text == "" || text == "<nil>" {
		return "", false
	}
	return text, true
}
