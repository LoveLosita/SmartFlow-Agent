package agenttools

import "strings"

var activeOptimizeAllowedTools = map[string]struct{}{
	ToolNameContextToolsAdd:    {},
	ToolNameContextToolsRemove: {},
	"analyze_health":           {},
	"move":                     {},
	"swap":                     {},
}

// IsToolAllowedInActiveOptimize 判定工具是否允许出现在“粗排后主动优化专用模式”里。
//
// 职责边界：
// 1. 这里只做场景级白名单裁剪，不参与工具是否已注册、是否被临时禁用、是否需要 confirm 的判断；
// 2. 该白名单只服务于“首次粗排后自动微调”链路，避免 LLM 在主动优化时重新暴露大量读工具；
// 3. context_tools_add/remove 仍保留，是为了兼容系统级动态区协议，但不代表会重新放开其它业务工具。
func IsToolAllowedInActiveOptimize(name string) bool {
	_, ok := activeOptimizeAllowedTools[strings.TrimSpace(name)]
	return ok
}

// FilterSchemasForActiveOptimize 过滤出主动优化专用模式允许暴露给 LLM 的工具 schema。
func FilterSchemasForActiveOptimize(schemas []ToolSchemaEntry) []ToolSchemaEntry {
	if len(schemas) == 0 {
		return nil
	}
	filtered := make([]ToolSchemaEntry, 0, len(schemas))
	for _, item := range schemas {
		if !IsToolAllowedInActiveOptimize(item.Name) {
			continue
		}
		filtered = append(filtered, item)
	}
	return filtered
}
