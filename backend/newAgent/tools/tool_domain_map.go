package newagenttools

import "strings"

const (
	// ToolDomainSchedule 表示“排程调整”工具域。
	ToolDomainSchedule = "schedule"
	// ToolDomainTaskClass 表示“任务类定义”工具域。
	ToolDomainTaskClass = "taskclass"
)

const (
	// ToolNameContextToolsAdd 表示“向 msg0 动态区注入目标工具域定义”工具。
	ToolNameContextToolsAdd = "context_tools_add"
	// ToolNameContextToolsRemove 表示“从 msg0 动态区移除目标工具域定义”工具。
	ToolNameContextToolsRemove = "context_tools_remove"
)

const (
	// ToolPackCore 是固定包：始终注入，不允许显式 add/remove。
	ToolPackCore = "core"

	// schedule 二级包（可选）。
	ToolPackQueue       = "queue"
	ToolPackMutation    = "mutation"
	ToolPackAnalyze     = "analyze"
	ToolPackDetailRead  = "detail_read"
	ToolPackDeepAnalyze = "deep_analyze"
	ToolPackWeb         = "web"
)

type toolProfile struct {
	Domain string
	Pack   string
}

// toolProfileByName 维护“工具名 -> 域/二级包”映射。
//
// 设计说明：
// 1. context 管理工具不参与域/包映射；
// 2. schedule 的 core 包是固定注入；其余能力按二级包按需注入；
// 3. taskclass 目前只有 core 包（固定注入）。
var toolProfileByName = map[string]toolProfile{
	"get_overview":          {Domain: ToolDomainSchedule, Pack: ToolPackCore},
	"query_available_slots": {Domain: ToolDomainSchedule, Pack: ToolPackCore},
	"query_target_tasks":    {Domain: ToolDomainSchedule, Pack: ToolPackCore},
	"analyze_health":        {Domain: ToolDomainSchedule, Pack: ToolPackAnalyze},

	"query_range":   {Domain: ToolDomainSchedule, Pack: ToolPackDetailRead},
	"get_task_info": {Domain: ToolDomainSchedule, Pack: ToolPackDetailRead},

	"queue_status":          {Domain: ToolDomainSchedule, Pack: ToolPackQueue},
	"queue_pop_head":        {Domain: ToolDomainSchedule, Pack: ToolPackQueue},
	"queue_apply_head_move": {Domain: ToolDomainSchedule, Pack: ToolPackQueue},
	"queue_skip_head":       {Domain: ToolDomainSchedule, Pack: ToolPackQueue},

	"place":      {Domain: ToolDomainSchedule, Pack: ToolPackMutation},
	"move":       {Domain: ToolDomainSchedule, Pack: ToolPackMutation},
	"swap":       {Domain: ToolDomainSchedule, Pack: ToolPackMutation},
	"batch_move": {Domain: ToolDomainSchedule, Pack: ToolPackMutation},
	"unplace":    {Domain: ToolDomainSchedule, Pack: ToolPackMutation},

	"analyze_rhythm": {Domain: ToolDomainSchedule, Pack: ToolPackDeepAnalyze},

	"web_search": {Domain: ToolDomainSchedule, Pack: ToolPackWeb},
	"web_fetch":  {Domain: ToolDomainSchedule, Pack: ToolPackWeb},

	"upsert_task_class": {Domain: ToolDomainTaskClass, Pack: ToolPackCore},
}

// NormalizeToolDomain 统一规范化工具域字符串。
func NormalizeToolDomain(domain string) string {
	switch strings.ToLower(strings.TrimSpace(domain)) {
	case ToolDomainSchedule:
		return ToolDomainSchedule
	case ToolDomainTaskClass:
		return ToolDomainTaskClass
	default:
		return ""
	}
}

// IsSupportedToolDomain 判断是否为当前支持的业务工具域。
func IsSupportedToolDomain(domain string) bool {
	return NormalizeToolDomain(domain) != ""
}

// NormalizeToolPack 统一规范化指定域下的二级包名。
func NormalizeToolPack(domain, pack string) string {
	normalizedDomain := NormalizeToolDomain(domain)
	normalizedPack := strings.ToLower(strings.TrimSpace(pack))
	if normalizedDomain == "" || normalizedPack == "" {
		return ""
	}

	switch normalizedDomain {
	case ToolDomainSchedule:
		switch normalizedPack {
		case ToolPackCore, ToolPackQueue, ToolPackMutation, ToolPackAnalyze, ToolPackDetailRead, ToolPackDeepAnalyze, ToolPackWeb:
			return normalizedPack
		default:
			return ""
		}
	case ToolDomainTaskClass:
		if normalizedPack == ToolPackCore {
			return ToolPackCore
		}
		return ""
	default:
		return ""
	}
}

// IsSupportedToolPack 判断某域下某二级包是否受支持。
func IsSupportedToolPack(domain, pack string) bool {
	return NormalizeToolPack(domain, pack) != ""
}

// IsFixedToolPack 判断某域下某二级包是否属于固定注入包。
func IsFixedToolPack(domain, pack string) bool {
	normalizedPack := NormalizeToolPack(domain, pack)
	return normalizedPack == ToolPackCore
}

// ListOptionalToolPacks 返回某域可选二级包列表（不含 core）。
func ListOptionalToolPacks(domain string) []string {
	switch NormalizeToolDomain(domain) {
	case ToolDomainSchedule:
		return []string{
			ToolPackMutation,
			ToolPackAnalyze,
			ToolPackDetailRead,
			ToolPackDeepAnalyze,
			ToolPackQueue,
			ToolPackWeb,
		}
	default:
		return nil
	}
}

// ListDefaultToolPacks 返回某域“默认注入”的可选包集合。
//
// 说明：
// 1. 仅用于 packs 为空时的兜底，目的是降低 msg0 噪声；
// 2. schedule 默认只开 mutation+analyze，其他包按需 add；
// 3. taskclass 当前无可选包。
func ListDefaultToolPacks(domain string) []string {
	switch NormalizeToolDomain(domain) {
	case ToolDomainSchedule:
		return []string{ToolPackMutation, ToolPackAnalyze}
	default:
		return nil
	}
}

// NormalizeToolPacks 规范化 pack 列表，并去重。
//
// 1. 仅返回受支持的 pack；
// 2. 自动剔除固定包 core（core 不接受显式管理）；
// 3. 顺序保持第一次出现顺序，便于日志和 prompt 可读性。
func NormalizeToolPacks(domain string, packs []string) []string {
	normalizedDomain := NormalizeToolDomain(domain)
	if normalizedDomain == "" || len(packs) == 0 {
		return nil
	}

	seen := make(map[string]struct{}, len(packs))
	result := make([]string, 0, len(packs))
	for _, rawPack := range packs {
		pack := NormalizeToolPack(normalizedDomain, rawPack)
		if pack == "" || IsFixedToolPack(normalizedDomain, pack) {
			continue
		}
		if _, exists := seen[pack]; exists {
			continue
		}
		seen[pack] = struct{}{}
		result = append(result, pack)
	}
	if len(result) == 0 {
		return nil
	}
	return result
}

// ResolveEffectiveToolPacks 返回某域下“真正生效”的可选包集合。
//
// 兼容策略：
// 1. schedule 域且 packs 为空时，默认启用最小可用包（mutation+analyze）；
// 2. taskclass 目前无可选包，统一返回 nil；
// 3. 非法域统一返回 nil。
func ResolveEffectiveToolPacks(domain string, packs []string) []string {
	normalizedDomain := NormalizeToolDomain(domain)
	if normalizedDomain == "" {
		return nil
	}

	if normalizedDomain == ToolDomainTaskClass {
		return nil
	}

	normalizedPacks := NormalizeToolPacks(normalizedDomain, packs)
	if len(normalizedPacks) > 0 {
		return normalizedPacks
	}

	defaultPacks := ListDefaultToolPacks(normalizedDomain)
	if len(defaultPacks) == 0 {
		return nil
	}
	result := make([]string, len(defaultPacks))
	copy(result, defaultPacks)
	return result
}

// IsContextManagementTool 判断工具是否属于上下文管理工具。
func IsContextManagementTool(name string) bool {
	switch strings.TrimSpace(name) {
	case ToolNameContextToolsAdd, ToolNameContextToolsRemove:
		return true
	default:
		return false
	}
}

// ResolveToolDomain 返回工具所属业务域。
func ResolveToolDomain(name string) (string, bool) {
	domain, _, ok := ResolveToolDomainPack(name)
	return domain, ok
}

// ResolveToolDomainPack 返回工具所属域与二级包。
//
// 返回语义：
// 1. 命中映射返回 (domain, pack, true)；
// 2. 未命中返回 ("", "", false)；
// 3. context 管理工具统一返回 ("", "", false)。
func ResolveToolDomainPack(name string) (string, string, bool) {
	toolName := strings.TrimSpace(name)
	if IsContextManagementTool(toolName) {
		return "", "", false
	}
	profile, ok := toolProfileByName[toolName]
	if !ok {
		return "", "", false
	}
	return profile.Domain, profile.Pack, true
}
