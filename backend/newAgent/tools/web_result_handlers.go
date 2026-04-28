package newagenttools

import (
	"strings"

	"github.com/LoveLosita/smartflow/backend/newAgent/tools/schedule"
	"github.com/LoveLosita/smartflow/backend/newAgent/tools/web"
	webresult "github.com/LoveLosita/smartflow/backend/newAgent/tools/web_result"
)

// NewWebSearchToolHandler 返回 web_search 的结构化结果 handler。
//
// 职责边界：
// 1. 负责执行底层 web_search 工具，并保留原始 ObservationText 给模型。
// 2. 负责把工具参数投影成 web_result 子包需要的最小输入。
// 3. 不负责注册接线；registry.go 由主代理统一切流。
func NewWebSearchToolHandler(provider web.SearchProvider) ToolHandler {
	searchHandler := web.NewSearchToolHandler(provider)
	return func(state *schedule.ScheduleState, args map[string]any) ToolExecutionResult {
		observation := searchHandler.Handle(args)
		legacy := LegacyResultWithState("web_search", args, state, observation)

		view := webresult.BuildSearchView(webresult.SearchViewInput{
			Observation: observation,
			Query:       readStringArg(args, "query"),
			TopK:        readIntArg(args, "top_k"),
			DomainAllow: readStringSliceArg(args, "domain_allow"),
			RecencyDays: readIntArg(args, "recency_days"),
		})
		return buildWebExecutionResult(legacy, args, view)
	}
}

// NewWebFetchToolHandler 返回 web_fetch 的结构化结果 handler。
//
// 职责边界：
// 1. 负责执行底层 web_fetch 工具，并保留原始 ObservationText 给模型。
// 2. 负责把抓取参数投影成 web_result 子包需要的最小输入。
// 3. 不负责注册接线；registry.go 由主代理统一切流。
func NewWebFetchToolHandler(fetcher *web.Fetcher) ToolHandler {
	fetchHandler := web.NewFetchToolHandler(fetcher)
	return func(state *schedule.ScheduleState, args map[string]any) ToolExecutionResult {
		observation := fetchHandler.Handle(args)
		legacy := LegacyResultWithState("web_fetch", args, state, observation)

		view := webresult.BuildFetchView(webresult.FetchViewInput{
			Observation: observation,
			URL:         readStringArg(args, "url"),
			MaxChars:    readIntArg(args, "max_chars"),
		})
		return buildWebExecutionResult(legacy, args, view)
	}
}

// buildWebExecutionResult 负责把子包纯展示视图包回父包统一协议。
//
// 步骤化说明：
// 1. 先以 legacy 结果为基础，复用父包现有的参数预览、错误抽取与兜底字段。
// 2. 再用子包 collapsed.status 覆盖最终状态，支持“未启用 provider -> blocked”的卡片语义。
// 3. 最后补齐 raw_text / status_label，保证 execute、SSE、timeline 都消费同一份 observation。
func buildWebExecutionResult(
	legacy ToolExecutionResult,
	args map[string]any,
	view webresult.ResultView,
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
	if _, exists := expanded["machine_payload"]; !exists {
		expanded["machine_payload"] = map[string]any{}
	}

	viewType := strings.TrimSpace(view.ViewType)
	if viewType == "" {
		viewType = webresult.ViewTypeSearchResult
	}
	version := view.Version
	if version <= 0 {
		version = webresult.ViewVersionResult
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

func readStringArg(args map[string]any, key string) string {
	if len(args) == 0 {
		return ""
	}
	raw, exists := args[strings.TrimSpace(key)]
	if !exists || raw == nil {
		return ""
	}
	text, ok := raw.(string)
	if !ok {
		return ""
	}
	return strings.TrimSpace(text)
}

func readIntArg(args map[string]any, key string) int {
	if len(args) == 0 {
		return 0
	}
	value, ok := toInt(args[strings.TrimSpace(key)])
	if !ok {
		return 0
	}
	return value
}

func readStringSliceArg(args map[string]any, key string) []string {
	if len(args) == 0 {
		return nil
	}
	raw, exists := args[strings.TrimSpace(key)]
	if !exists || raw == nil {
		return nil
	}
	switch typed := raw.(type) {
	case []string:
		out := make([]string, 0, len(typed))
		for _, item := range typed {
			item = strings.TrimSpace(item)
			if item == "" {
				continue
			}
			out = append(out, item)
		}
		return out
	case []any:
		out := make([]string, 0, len(typed))
		for _, item := range typed {
			text, ok := item.(string)
			if !ok {
				continue
			}
			text = strings.TrimSpace(text)
			if text == "" {
				continue
			}
			out = append(out, text)
		}
		return out
	default:
		return nil
	}
}
