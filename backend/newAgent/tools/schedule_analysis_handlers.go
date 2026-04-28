package newagenttools

import (
	"strings"

	"github.com/LoveLosita/smartflow/backend/newAgent/tools/schedule"
	scheduleanalysis "github.com/LoveLosita/smartflow/backend/newAgent/tools/schedule_analysis"
)

type scheduleAnalyzeObserveFunc func(state *schedule.ScheduleState, args map[string]any) string
type scheduleAnalyzeViewBuilder func(input scheduleAnalysisAdapterInput) scheduleanalysis.AnalysisResultView

// scheduleAnalysisAdapterInput 是父包传给 schedule_analysis 子包前的最小上下文。
//
// 职责边界：
// 1. 只携带展示构造需要的 observation 与已本地化参数字段；
// 2. 不把 ToolExecutionResult / ToolArgumentView 传入子包，避免反向依赖父包；
// 3. ObservationText 必须原样来自底层 schedule.AnalyzeXxx，不在 adapter 层改写。
type scheduleAnalysisAdapterInput struct {
	ToolName        string
	Args            map[string]any
	State           *schedule.ScheduleState
	ObservationText string
	ArgFields       []scheduleanalysis.KVField
}

// NewAnalyzeHealthToolHandler 为 analyze_health 生成结构化诊断结果。
func NewAnalyzeHealthToolHandler() ToolHandler {
	return newScheduleAnalyzeToolHandler(
		"analyze_health",
		schedule.AnalyzeHealth,
		func(input scheduleAnalysisAdapterInput) scheduleanalysis.AnalysisResultView {
			return scheduleanalysis.BuildAnalyzeHealthView(scheduleanalysis.AnalyzeHealthViewInput{
				Observation: input.ObservationText,
				ArgFields:   input.ArgFields,
			})
		},
	)
}

// NewAnalyzeRhythmToolHandler 为 analyze_rhythm 生成结构化诊断结果。
func NewAnalyzeRhythmToolHandler() ToolHandler {
	return newScheduleAnalyzeToolHandler(
		"analyze_rhythm",
		schedule.AnalyzeRhythm,
		func(input scheduleAnalysisAdapterInput) scheduleanalysis.AnalysisResultView {
			return scheduleanalysis.BuildAnalyzeRhythmView(scheduleanalysis.AnalyzeRhythmViewInput{
				Observation: input.ObservationText,
				ArgFields:   input.ArgFields,
			})
		},
	)
}

// newScheduleAnalyzeToolHandler 统一构造父包 analysis adapter。
//
// 步骤化说明：
// 1. 先调用现有 schedule.AnalyzeXxx，确保 state_snapshot / prompt 摘要消费的 JSON 完全不变；
// 2. 再用 LegacyResultWithState 复用父包参数展示、状态判断和错误信息提取；
// 3. 最后调用 schedule_analysis 子包生成纯展示视图，并包回 ToolExecutionResult。
func newScheduleAnalyzeToolHandler(
	toolName string,
	observe scheduleAnalyzeObserveFunc,
	buildView scheduleAnalyzeViewBuilder,
) ToolHandler {
	return func(state *schedule.ScheduleState, args map[string]any) ToolExecutionResult {
		observation := observe(state, args)
		legacy := LegacyResultWithState(toolName, args, state, observation)
		input := scheduleAnalysisAdapterInput{
			ToolName:        toolName,
			Args:            cloneAnyMap(args),
			State:           state,
			ObservationText: observation,
			ArgFields:       extractScheduleAnalysisArgumentFields(legacy.ArgumentView),
		}
		return buildScheduleAnalysisExecutionResult(legacy, args, buildView(input))
	}
}

// buildScheduleAnalysisExecutionResult 负责把子包纯展示视图包回父包统一协议。
//
// 职责边界：
// 1. 只做 AnalysisResultView -> ToolDisplayView 的协议桥接；
// 2. 不改写 ObservationText，确保主动优化状态快照仍读取原始 JSON；
// 3. 错误码与错误文案继续复用父包既有 JSON / 文本解析逻辑。
func buildScheduleAnalysisExecutionResult(
	legacy ToolExecutionResult,
	args map[string]any,
	view scheduleanalysis.AnalysisResultView,
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
		viewType = scheduleanalysis.ViewTypeAnalysisResult
	}
	version := view.Version
	if version <= 0 {
		version = scheduleanalysis.ViewVersionAnalysisResult
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

// extractScheduleAnalysisArgumentFields 把父包 ToolArgumentView 投影成子包可消费的 KVField。
//
// 说明：
// 1. 参数字段只做回显，尤其 detail / threshold / hard_categories 不在这里解释为真实生效；
// 2. 子包只接收中文 label/display，避免理解父包参数 view 结构；
// 3. 字段缺失时返回空切片，由子包跳过参数 section。
func extractScheduleAnalysisArgumentFields(view *ToolArgumentView) []scheduleanalysis.KVField {
	if view == nil || view.Expanded == nil {
		return make([]scheduleanalysis.KVField, 0)
	}
	rawFields, exists := view.Expanded["fields"]
	if !exists {
		return make([]scheduleanalysis.KVField, 0)
	}

	fields := make([]scheduleanalysis.KVField, 0)
	appendField := func(row map[string]any) {
		label, _ := row["label"].(string)
		display, _ := row["display"].(string)
		label = strings.TrimSpace(label)
		display = strings.TrimSpace(display)
		if label == "" || display == "" {
			return
		}
		fields = append(fields, scheduleanalysis.BuildKVField(label, display))
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
		return make([]scheduleanalysis.KVField, 0)
	}
	return fields
}
