package newagenttools

import (
	"encoding/json"
	"strings"

	"github.com/LoveLosita/smartflow/backend/newAgent/tools/schedule"
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
// 1. 仅负责校验 domain/mode/packs 并返回结构化结果，不直接修改流程状态；
// 2. 真正的“激活态写回”由 execute 节点根据工具结果回写 CommonState；
// 3. schedule 支持可选 packs，taskclass 当前不支持可选 packs。
func NewContextToolsAddHandler() ToolHandler {
	return func(state *schedule.ScheduleState, args map[string]any) ToolExecutionResult {
		_ = state

		domain := NormalizeToolDomain(readContextToolString(args["domain"]))
		if domain == "" {
			return LegacyResult(ToolNameContextToolsAdd, args, marshalContextToolsAddResult(contextToolsAddResult{
				Tool:      ToolNameContextToolsAdd,
				Success:   false,
				Action:    "reject",
				Error:     "参数非法：domain 仅支持 schedule/taskclass",
				ErrorCode: "invalid_domain",
			}))
		}

		mode := strings.ToLower(strings.TrimSpace(readContextToolString(args["mode"])))
		if mode == "" {
			mode = "replace"
		}
		if mode != "replace" && mode != "merge" {
			return LegacyResult(ToolNameContextToolsAdd, args, marshalContextToolsAddResult(contextToolsAddResult{
				Tool:      ToolNameContextToolsAdd,
				Success:   false,
				Action:    "reject",
				Domain:    domain,
				Error:     "参数非法：mode 仅支持 replace/merge",
				ErrorCode: "invalid_mode",
			}))
		}

		packsRaw := readContextToolStringSlice(args["packs"])
		packs, errCode, errText := validateContextPacks(domain, packsRaw, false)
		if errCode != "" {
			return LegacyResult(ToolNameContextToolsAdd, args, marshalContextToolsAddResult(contextToolsAddResult{
				Tool:      ToolNameContextToolsAdd,
				Success:   false,
				Action:    "reject",
				Domain:    domain,
				Error:     errText,
				ErrorCode: errCode,
			}))
		}

		// schedule 未显式传 packs 时，默认启用最小可用包（mutation + analyze）。
		if domain == ToolDomainSchedule && len(packsRaw) == 0 {
			packs = ResolveEffectiveToolPacks(domain, nil)
		}

		return LegacyResult(ToolNameContextToolsAdd, args, marshalContextToolsAddResult(contextToolsAddResult{
			Tool:    ToolNameContextToolsAdd,
			Success: true,
			Action:  "activate",
			Domain:  domain,
			Packs:   packs,
			Mode:    mode,
			Message: "已激活工具域，可继续调用对应业务工具。",
		}))
	}
}

// NewContextToolsRemoveHandler 创建 context_tools_remove 工具。
//
// 职责边界：
// 1. 仅解析 domain/all/packs 语义并返回结构化结果，不直接触碰上下文存储；
// 2. all=true 表示清空动态区业务工具，domain+packs 表示移除该域下指定二级包；
// 3. 仅 schedule 支持按 packs 移除，且 core 不允许显式移除。
func NewContextToolsRemoveHandler() ToolHandler {
	return func(state *schedule.ScheduleState, args map[string]any) ToolExecutionResult {
		_ = state

		all := readContextToolBool(args["all"])
		domainRaw := strings.ToLower(strings.TrimSpace(readContextToolString(args["domain"])))
		packsRaw := readContextToolStringSlice(args["packs"])

		// 兼容写法：domain=all 视为清空全部。
		if domainRaw == "all" {
			all = true
		}
		if all {
			return LegacyResult(ToolNameContextToolsRemove, args, marshalContextToolsRemoveResult(contextToolsRemoveResult{
				Tool:    ToolNameContextToolsRemove,
				Success: true,
				Action:  "clear_all",
				All:     true,
				Message: "已移除全部业务工具域，仅保留上下文管理工具。",
			}))
		}

		domain := NormalizeToolDomain(domainRaw)
		if domain == "" {
			return LegacyResult(ToolNameContextToolsRemove, args, marshalContextToolsRemoveResult(contextToolsRemoveResult{
				Tool:      ToolNameContextToolsRemove,
				Success:   false,
				Action:    "reject",
				Error:     "参数非法：需提供 domain=schedule/taskclass 或 all=true",
				ErrorCode: "invalid_domain",
			}))
		}

		packs, errCode, errText := validateContextPacks(domain, packsRaw, true)
		if errCode != "" {
			return LegacyResult(ToolNameContextToolsRemove, args, marshalContextToolsRemoveResult(contextToolsRemoveResult{
				Tool:      ToolNameContextToolsRemove,
				Success:   false,
				Action:    "reject",
				Domain:    domain,
				Error:     errText,
				ErrorCode: errCode,
			}))
		}

		if len(packs) > 0 {
			return LegacyResult(ToolNameContextToolsRemove, args, marshalContextToolsRemoveResult(contextToolsRemoveResult{
				Tool:    ToolNameContextToolsRemove,
				Success: true,
				Action:  "deactivate_packs",
				Domain:  domain,
				Packs:   packs,
				Message: "已移除指定工具包。",
			}))
		}

		return LegacyResult(ToolNameContextToolsRemove, args, marshalContextToolsRemoveResult(contextToolsRemoveResult{
			Tool:    ToolNameContextToolsRemove,
			Success: true,
			Action:  "deactivate",
			Domain:  domain,
			Message: "已移除指定工具域。",
		}))
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
