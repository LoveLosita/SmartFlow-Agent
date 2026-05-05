package agenttools

import (
	"encoding/json"
	"strings"

	"github.com/LoveLosita/smartflow/backend/services/agent/tools/schedule"
	toolcontextresult "github.com/LoveLosita/smartflow/backend/services/agent/tools/tool_context_result"
)

type contextToolsAddResult struct {
	Tool      string   `json:"tool"`
	Success   bool     `json:"success"`
	Action    string   `json:"action"`
	Domain    string   `json:"domain,omitempty"`
	Packs     []string `json:"packs,omitempty"`
	Mode      string   `json:"mode,omitempty"`
	Message   string   `json:"message,omitempty"`
	Error     string   `json:"error,omitempty"`
	ErrorCode string   `json:"error_code,omitempty"`
}

type contextToolsRemoveResult struct {
	Tool      string   `json:"tool"`
	Success   bool     `json:"success"`
	Action    string   `json:"action"`
	Domain    string   `json:"domain,omitempty"`
	Packs     []string `json:"packs,omitempty"`
	All       bool     `json:"all,omitempty"`
	Message   string   `json:"message,omitempty"`
	Error     string   `json:"error,omitempty"`
	ErrorCode string   `json:"error_code,omitempty"`
}

// NewContextToolsAddHandler 创建 context_tools_add 工具。
//
// 职责边界：
// 1. 这里只负责校验 domain / packs / mode，并产出结构化结果；
// 2. 不直接改 CommonState，真正的激活切流仍由 execute 层读取 observation 后更新快照；
// 3. 因为这里拿不到 CommonState，所以卡片展示的是“本次工具结果返回的 domain/packs/mode”，不是全局最终快照。
func NewContextToolsAddHandler() ToolHandler {
	return func(state *schedule.ScheduleState, args map[string]any) ToolExecutionResult {
		_ = state

		domain := NormalizeToolDomain(readContextToolString(args["domain"]))
		if domain == "" {
			return buildContextToolsAddExecutionResult(args, contextToolsAddResult{
				Tool:      ToolNameContextToolsAdd,
				Success:   false,
				Action:    "reject",
				Error:     "参数非法：domain 仅支持 schedule/taskclass",
				ErrorCode: "invalid_domain",
			})
		}

		mode := strings.ToLower(strings.TrimSpace(readContextToolString(args["mode"])))
		if mode == "" {
			mode = "replace"
		}
		if mode != "replace" && mode != "merge" {
			return buildContextToolsAddExecutionResult(args, contextToolsAddResult{
				Tool:      ToolNameContextToolsAdd,
				Success:   false,
				Action:    "reject",
				Domain:    domain,
				Error:     "参数非法：mode 仅支持 replace/merge",
				ErrorCode: "invalid_mode",
			})
		}

		packsRaw := readContextToolStringSlice(args["packs"])
		packs, errCode, errText := validateContextPacks(domain, packsRaw, false)
		if errCode != "" {
			return buildContextToolsAddExecutionResult(args, contextToolsAddResult{
				Tool:      ToolNameContextToolsAdd,
				Success:   false,
				Action:    "reject",
				Domain:    domain,
				Error:     errText,
				ErrorCode: errCode,
			})
		}

		// 1. schedule 未显式传 packs 时，默认激活最小可用包 mutation+analyze。
		// 2. taskclass 当前没有可选包，所以这里会保持空切片，由 execute 层只保留固定 core。
		// 3. 这样做可以让 observation 直接表达“本次实际生效的可选包集合”，减少展示层再二次猜测。
		if domain == ToolDomainSchedule && len(packsRaw) == 0 {
			packs = ResolveEffectiveToolPacks(domain, nil)
		}

		return buildContextToolsAddExecutionResult(args, contextToolsAddResult{
			Tool:    ToolNameContextToolsAdd,
			Success: true,
			Action:  "activate",
			Domain:  domain,
			Packs:   packs,
			Mode:    mode,
			Message: "已激活目标工具域，可继续调用对应业务工具。",
		})
	}
}

// NewContextToolsRemoveHandler 创建 context_tools_remove 工具。
//
// 职责边界：
// 1. 这里只解释 domain / packs / all 的语义，并返回结构化结果；
// 2. all=true 表示清空全部业务工具域，domain+packs 表示移除某域下的可选包；
// 3. 实际 CommonState 的域/包更新，仍由 execute 层统一消费 observation 完成。
func NewContextToolsRemoveHandler() ToolHandler {
	return func(state *schedule.ScheduleState, args map[string]any) ToolExecutionResult {
		_ = state

		all := readContextToolBool(args["all"])
		domainRaw := strings.ToLower(strings.TrimSpace(readContextToolString(args["domain"])))
		packsRaw := readContextToolStringSlice(args["packs"])

		// 兼容旧写法：domain=all 也视为清空全部业务工具域。
		if domainRaw == "all" {
			all = true
		}
		if all {
			return buildContextToolsRemoveExecutionResult(args, contextToolsRemoveResult{
				Tool:    ToolNameContextToolsRemove,
				Success: true,
				Action:  "clear_all",
				All:     true,
				Message: "已移除全部业务工具域，仅保留 context 管理工具。",
			})
		}

		domain := NormalizeToolDomain(domainRaw)
		if domain == "" {
			return buildContextToolsRemoveExecutionResult(args, contextToolsRemoveResult{
				Tool:      ToolNameContextToolsRemove,
				Success:   false,
				Action:    "reject",
				Error:     "参数非法：需要提供 domain=schedule/taskclass 或 all=true",
				ErrorCode: "invalid_domain",
			})
		}

		packs, errCode, errText := validateContextPacks(domain, packsRaw, true)
		if errCode != "" {
			return buildContextToolsRemoveExecutionResult(args, contextToolsRemoveResult{
				Tool:      ToolNameContextToolsRemove,
				Success:   false,
				Action:    "reject",
				Domain:    domain,
				Error:     errText,
				ErrorCode: errCode,
			})
		}

		if len(packs) > 0 {
			return buildContextToolsRemoveExecutionResult(args, contextToolsRemoveResult{
				Tool:    ToolNameContextToolsRemove,
				Success: true,
				Action:  "deactivate_packs",
				Domain:  domain,
				Packs:   packs,
				Message: "已移除指定工具包。",
			})
		}

		return buildContextToolsRemoveExecutionResult(args, contextToolsRemoveResult{
			Tool:    ToolNameContextToolsRemove,
			Success: true,
			Action:  "deactivate",
			Domain:  domain,
			Message: "已移除指定工具域。",
		})
	}
}

func buildContextToolsAddExecutionResult(args map[string]any, payload contextToolsAddResult) ToolExecutionResult {
	observation := marshalContextToolsAddResult(payload)
	legacy := LegacyResult(ToolNameContextToolsAdd, args, observation)
	view := toolcontextresult.BuildAddView(toContextToolsAddPayload(payload), observation)
	return buildContextToolExecutionResult(legacy, args, view)
}

func buildContextToolsRemoveExecutionResult(args map[string]any, payload contextToolsRemoveResult) ToolExecutionResult {
	observation := marshalContextToolsRemoveResult(payload)
	legacy := LegacyResult(ToolNameContextToolsRemove, args, observation)
	view := toolcontextresult.BuildRemoveView(toContextToolsRemovePayload(payload), observation)
	return buildContextToolExecutionResult(legacy, args, view)
}

// buildContextToolExecutionResult 负责把子包纯展示结构包回 ToolExecutionResult。
//
// 职责边界：
// 1. 只做 ContextResultView -> ToolDisplayView 的协议桥接；
// 2. 不改写 ObservationText，确保模型侧仍消费原始 observation JSON；
// 3. 错误码与错误文案继续复用父包现有 JSON/text 解析逻辑，避免多套失败判定分叉。
func buildContextToolExecutionResult(
	legacy ToolExecutionResult,
	args map[string]any,
	view toolcontextresult.ContextResultView,
) ToolExecutionResult {
	result := legacy
	status := normalizeToolStatus(result.Status)
	if status == "" {
		status = ToolStatusDone
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

	result.Status = status
	result.Success = status == ToolStatusDone
	result.ResultView = &ToolDisplayView{
		ViewType:  strings.TrimSpace(view.ViewType),
		Version:   view.Version,
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

func toContextToolsAddPayload(payload contextToolsAddResult) toolcontextresult.ContextToolsAddPayload {
	return toolcontextresult.ContextToolsAddPayload{
		Tool:      payload.Tool,
		Success:   payload.Success,
		Action:    payload.Action,
		Domain:    payload.Domain,
		Packs:     append([]string(nil), payload.Packs...),
		Mode:      payload.Mode,
		Message:   payload.Message,
		Error:     payload.Error,
		ErrorCode: payload.ErrorCode,
	}
}

func toContextToolsRemovePayload(payload contextToolsRemoveResult) toolcontextresult.ContextToolsRemovePayload {
	return toolcontextresult.ContextToolsRemovePayload{
		Tool:      payload.Tool,
		Success:   payload.Success,
		Action:    payload.Action,
		Domain:    payload.Domain,
		Packs:     append([]string(nil), payload.Packs...),
		All:       payload.All,
		Message:   payload.Message,
		Error:     payload.Error,
		ErrorCode: payload.ErrorCode,
	}
}

func validateContextPacks(domain string, packs []string, forRemove bool) ([]string, string, string) {
	normalizedDomain := NormalizeToolDomain(domain)
	if normalizedDomain == "" {
		return nil, "invalid_domain", "参数非法：domain 非法"
	}
	if len(packs) == 0 {
		return nil, "", ""
	}

	if normalizedDomain == ToolDomainTaskClass {
		return nil, "unsupported_packs_for_domain", "参数非法：taskclass 暂不支持 packs"
	}

	normalized := make([]string, 0, len(packs))
	seen := make(map[string]struct{}, len(packs))
	for _, raw := range packs {
		trimmed := strings.TrimSpace(raw)
		if trimmed == "" {
			continue
		}
		pack := NormalizeToolPack(normalizedDomain, trimmed)
		if pack == "" {
			return nil, "invalid_pack", "参数非法：存在不支持的 pack"
		}
		if IsFixedToolPack(normalizedDomain, pack) {
			if forRemove {
				return nil, "fixed_pack_forbidden", "参数非法：core 为固定包，不允许 remove"
			}
			return nil, "fixed_pack_forbidden", "参数非法：core 为固定包，不允许 add"
		}
		if _, exists := seen[pack]; exists {
			continue
		}
		seen[pack] = struct{}{}
		normalized = append(normalized, pack)
	}
	if len(normalized) == 0 {
		return nil, "invalid_pack", "参数非法：packs 为空或无效"
	}
	return normalized, "", ""
}

func readContextToolString(raw any) string {
	text, _ := raw.(string)
	return strings.TrimSpace(text)
}

func readContextToolStringSlice(raw any) []string {
	switch typed := raw.(type) {
	case []string:
		out := make([]string, 0, len(typed))
		for _, item := range typed {
			text := strings.TrimSpace(item)
			if text == "" {
				continue
			}
			out = append(out, text)
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
	case string:
		text := strings.TrimSpace(typed)
		if text == "" {
			return nil
		}
		parts := strings.Split(text, ",")
		out := make([]string, 0, len(parts))
		for _, part := range parts {
			part = strings.TrimSpace(part)
			if part == "" {
				continue
			}
			out = append(out, part)
		}
		return out
	default:
		return nil
	}
}

func readContextToolBool(raw any) bool {
	switch v := raw.(type) {
	case bool:
		return v
	case string:
		value := strings.ToLower(strings.TrimSpace(v))
		return value == "1" || value == "true" || value == "yes"
	case float64:
		return v != 0
	case float32:
		return v != 0
	case int:
		return v != 0
	case int8:
		return v != 0
	case int16:
		return v != 0
	case int32:
		return v != 0
	case int64:
		return v != 0
	default:
		return false
	}
}

func marshalContextToolsAddResult(result contextToolsAddResult) string {
	raw, err := json.Marshal(result)
	if err != nil {
		return `{"tool":"context_tools_add","success":false,"action":"reject","error":"result encode failed","error_code":"encode_failed"}`
	}
	return string(raw)
}

func marshalContextToolsRemoveResult(result contextToolsRemoveResult) string {
	raw, err := json.Marshal(result)
	if err != nil {
		return `{"tool":"context_tools_remove","success":false,"action":"reject","error":"result encode failed","error_code":"encode_failed"}`
	}
	return string(raw)
}
