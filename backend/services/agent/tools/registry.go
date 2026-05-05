package agenttools

import (
	"fmt"
	"sort"
	"strings"

	"github.com/LoveLosita/smartflow/backend/services/agent/tools/schedule"
	"github.com/LoveLosita/smartflow/backend/services/agent/tools/web"
	ragservice "github.com/LoveLosita/smartflow/backend/services/rag"
)

// ToolHandler 约定所有工具的统一执行签名。
//
// 职责边界：
// 1. 负责消费当前 ScheduleState 与模型传入参数；
// 2. 返回 ToolExecutionResult，供 execute 节点写回 observation 与结构化事件；
// 3. 不负责 confirm、上下文注入、轮次控制，这些由上层节点处理。
type ToolHandler func(state *schedule.ScheduleState, args map[string]any) ToolExecutionResult

// ToolSchemaEntry 描述注入给模型的工具快照。
type ToolSchemaEntry struct {
	Name       string
	Desc       string
	SchemaText string
}

// DefaultRegistryDeps 描述默认注册表需要的外部依赖。
//
// 职责边界：
// 1. 这里只承载工具层需要的依赖注入，不承载业务状态；
// 2. 某些依赖即便暂未使用也允许保留，避免业务层重新到处 new；
// 3. 具体依赖缺失时由对应工具自行返回结构化失败结果。
type DefaultRegistryDeps struct {
	RAGRuntime ragservice.Runtime

	// WebSearchProvider 为 nil 时，web_search / web_fetch 仍会注册，
	// 但 handler 会返回“暂未启用”的只读 observation，不阻断主流程。
	WebSearchProvider web.SearchProvider

	// TaskClassWriteDeps 供 upsert_task_class 调用持久化层。
	TaskClassWriteDeps TaskClassWriteDeps
}

// ToolRegistry 管理工具注册、过滤与执行。
type ToolRegistry struct {
	handlers map[string]ToolHandler
	schemas  []ToolSchemaEntry
	deps     DefaultRegistryDeps
}

// temporaryDisabledTools 描述“已注册但当前阶段临时禁用”的工具。
//
// 设计说明：
// 1. 这些工具仍保留定义，避免 prompt / 旧链路 / 历史日志里出现悬空名字；
// 2. execute 会在调用前统一阻断，并向模型返回纠错提示；
// 3. ToolNames / Schemas 也会默认隐藏它们，避免继续污染 msg0。
var temporaryDisabledTools = map[string]bool{}

// IsTemporarilyDisabledTool 判断工具是否在当前阶段被临时禁用。
func IsTemporarilyDisabledTool(name string) bool {
	return temporaryDisabledTools[strings.TrimSpace(name)]
}

// NewToolRegistry 创建空注册表。
func NewToolRegistry() *ToolRegistry {
	return NewToolRegistryWithDeps(DefaultRegistryDeps{})
}

// NewToolRegistryWithDeps 创建带依赖的空注册表。
func NewToolRegistryWithDeps(deps DefaultRegistryDeps) *ToolRegistry {
	return &ToolRegistry{
		handlers: make(map[string]ToolHandler),
		schemas:  make([]ToolSchemaEntry, 0),
		deps:     deps,
	}
}

// Register 注册一个工具及其 schema。
func (r *ToolRegistry) Register(name, desc, schemaText string, handler ToolHandler) {
	r.handlers[name] = handler
	r.schemas = append(r.schemas, ToolSchemaEntry{
		Name:       name,
		Desc:       desc,
		SchemaText: schemaText,
	})
}

// Execute 执行指定工具。
//
// 职责边界：
// 1. 这里只负责找到 handler 并调用；
// 2. 工具临时禁用时直接返回 blocked 结构化结果，不进入 handler；
// 3. 参数 schema 级纠错仍由 handler 内处理。
func (r *ToolRegistry) Execute(state *schedule.ScheduleState, toolName string, args map[string]any) ToolExecutionResult {
	if r.IsToolTemporarilyDisabled(toolName) {
		observation := fmt.Sprintf("工具 %q 当前阶段已临时禁用，请优先使用 analyze_health、move、swap 等当前主链工具。", strings.TrimSpace(toolName))
		return BlockedResult(toolName, args, observation, "tool_temporarily_disabled", observation)
	}
	handler, ok := r.handlers[toolName]
	if !ok {
		observation := fmt.Sprintf("工具调用失败：未知工具 %q。可用工具：%s", toolName, strings.Join(r.ToolNames(), "、"))
		result := LegacyResult(toolName, args, observation)
		result.Status = ToolStatusFailed
		result.Success = false
		result.ErrorCode = "unknown_tool"
		result.ErrorMessage = observation
		return EnsureToolResultDefaults(result, args)
	}
	return EnsureToolResultDefaults(handler(state, args), args)
}

// HasTool 判断工具是否已注册且当前可见。
func (r *ToolRegistry) HasTool(name string) bool {
	if r.IsToolTemporarilyDisabled(name) {
		return false
	}
	_, ok := r.handlers[name]
	return ok
}

// IsToolTemporarilyDisabled 判断工具是否处于“已注册但暂不允许调用”状态。
func (r *ToolRegistry) IsToolTemporarilyDisabled(name string) bool {
	return IsTemporarilyDisabledTool(name)
}

// ToolNames 返回当前可暴露给模型的工具名。
func (r *ToolRegistry) ToolNames() []string {
	names := make([]string, 0, len(r.schemas))
	for _, item := range r.schemas {
		if r.IsToolTemporarilyDisabled(item.Name) {
			continue
		}
		names = append(names, item.Name)
	}
	return names
}

// Schemas 返回当前可暴露给模型的 schema 快照。
func (r *ToolRegistry) Schemas() []ToolSchemaEntry {
	result := make([]ToolSchemaEntry, 0, len(r.schemas))
	for _, item := range r.schemas {
		if r.IsToolTemporarilyDisabled(item.Name) {
			continue
		}
		result = append(result, item)
	}
	return result
}

// SchemasForActiveDomain 返回某业务域当前真正可见的工具 schema。
//
// 职责边界：
// 1. context_tools_add/remove 始终保留，用于动态区协议；
// 2. 仅当工具域已激活时，才暴露该域下可见工具；
// 3. schedule 域支持按 pack 过滤；taskclass 目前只有 core。
func (r *ToolRegistry) SchemasForActiveDomain(activeDomain string, activePacks []string) []ToolSchemaEntry {
	normalizedDomain := NormalizeToolDomain(activeDomain)
	effectivePacks := ResolveEffectiveToolPacks(normalizedDomain, activePacks)
	effectivePackSet := make(map[string]struct{}, len(effectivePacks))
	for _, pack := range effectivePacks {
		effectivePackSet[pack] = struct{}{}
	}

	selected := make([]ToolSchemaEntry, 0, len(r.schemas))
	for _, item := range r.schemas {
		name := strings.TrimSpace(item.Name)
		if r.IsToolTemporarilyDisabled(name) {
			continue
		}
		if IsContextManagementTool(name) {
			selected = append(selected, item)
			continue
		}
		if normalizedDomain == "" {
			continue
		}

		domain, pack, ok := ResolveToolDomainPack(name)
		if !ok {
			// 兼容历史未建档工具：仅在 schedule 域下继续暴露，避免突然失联。
			if normalizedDomain == ToolDomainSchedule {
				selected = append(selected, item)
			}
			continue
		}
		if domain != normalizedDomain {
			continue
		}
		if IsFixedToolPack(domain, pack) {
			selected = append(selected, item)
			continue
		}
		if _, exists := effectivePackSet[pack]; exists {
			selected = append(selected, item)
		}
	}

	result := make([]ToolSchemaEntry, len(selected))
	copy(result, selected)
	return result
}

// IsToolVisibleInDomain 判断某工具在当前动态区下是否应对模型可见。
func (r *ToolRegistry) IsToolVisibleInDomain(activeDomain string, activePacks []string, toolName string) bool {
	name := strings.TrimSpace(toolName)
	if name == "" {
		return false
	}
	for _, item := range r.SchemasForActiveDomain(activeDomain, activePacks) {
		if strings.TrimSpace(item.Name) == name {
			return true
		}
	}
	return false
}

// IsWriteTool 判断工具是否属于写工具。
func (r *ToolRegistry) IsWriteTool(name string) bool {
	return writeTools[strings.TrimSpace(name)]
}

// IsScheduleMutationTool 判断工具是否会真实修改 ScheduleState 中的日程布局。
// upsert_task_class 会写库，但不修改当前日程预览，因此不计入此集合。
func (r *ToolRegistry) IsScheduleMutationTool(name string) bool {
	return scheduleMutationTools[strings.TrimSpace(name)]
}

// RequiresScheduleState 判断工具是否依赖 ScheduleState。
func (r *ToolRegistry) RequiresScheduleState(name string) bool {
	return !scheduleFreeTools[strings.TrimSpace(name)]
}

var writeTools = map[string]bool{
	"place":                 true,
	"move":                  true,
	"swap":                  true,
	"batch_move":            true,
	"queue_apply_head_move": true,
	"unplace":               true,
	"upsert_task_class":     true,
}

var scheduleMutationTools = map[string]bool{
	"place":                 true,
	"move":                  true,
	"swap":                  true,
	"batch_move":            true,
	"queue_apply_head_move": true,
	"unplace":               true,
}

// scheduleFreeTools 描述“即使没有 ScheduleState 也能安全执行”的工具。
var scheduleFreeTools = map[string]bool{
	"web_search":               true,
	"web_fetch":                true,
	"upsert_task_class":        true,
	ToolNameContextToolsAdd:    true,
	ToolNameContextToolsRemove: true,
}

// NewDefaultRegistry 创建默认注册表。
func NewDefaultRegistry() *ToolRegistry {
	return NewDefaultRegistryWithDeps(DefaultRegistryDeps{})
}

// NewDefaultRegistryWithDeps 创建带依赖的默认注册表。
//
// 步骤化说明：
// 1. 先注册上下文管理工具，保证动态区协议随时可用；
// 2. 再注册 schedule 域的读、诊断、写工具；
// 3. 最后注册 taskclass 与 web 工具，并统一按 name 排序，保证 prompt 输出稳定。
func NewDefaultRegistryWithDeps(deps DefaultRegistryDeps) *ToolRegistry {
	r := NewToolRegistryWithDeps(deps)

	registerContextTools(r)
	registerScheduleReadTools(r)
	registerScheduleAnalyzeTools(r)
	registerScheduleMutationTools(r)
	registerTaskClassTools(r, deps)
	registerWebTools(r, deps)

	sort.Slice(r.schemas, func(i, j int) bool {
		return r.schemas[i].Name < r.schemas[j].Name
	})
	return r
}

func registerContextTools(r *ToolRegistry) {
	r.Register(
		ToolNameContextToolsAdd,
		"激活指定工具域，并可附带 schedule 二级包 packs。core 固定注入。",
		`{"name":"context_tools_add","parameters":{"domain":{"type":"string","required":true,"enum":["schedule","taskclass"]},"packs":{"type":"array","items":{"type":"string","enum":["mutation","analyze","detail_read","deep_analyze","queue","web"]}},"mode":{"type":"string","enum":["replace","merge"]}}}`,
		NewContextToolsAddHandler(),
	)
	r.Register(
		ToolNameContextToolsRemove,
		"移除指定工具域、指定二级包，或清空全部业务工具域（all=true）。core 固定包不支持 remove。",
		`{"name":"context_tools_remove","parameters":{"domain":{"type":"string","enum":["schedule","taskclass","all"]},"packs":{"type":"array","items":{"type":"string","enum":["mutation","analyze","detail_read","deep_analyze","queue","web"]}},"all":{"type":"bool"}}}`,
		NewContextToolsRemoveHandler(),
	)
}

func registerScheduleReadTools(r *ToolRegistry) {
	r.Register(
		"get_overview",
		"获取当前窗口总览：保留课程占位统计，展开任务清单。",
		`{"name":"get_overview","parameters":{}}`,
		NewGetOverviewToolHandler(),
	)
	r.Register(
		"query_range",
		"查看某天或某时段的占用详情。day 必填，slot_start/slot_end 选填。",
		`{"name":"query_range","parameters":{"day":{"type":"int","required":true},"slot_start":{"type":"int"},"slot_end":{"type":"int"}}}`,
		NewQueryRangeToolHandler(),
	)
	r.Register(
		"query_available_slots",
		"查询候选空位池，适合 move 前筛落点。",
		`{"name":"query_available_slots","parameters":{"span":{"type":"int"},"duration":{"type":"int"},"limit":{"type":"int"},"allow_embed":{"type":"bool"},"day":{"type":"int"},"day_start":{"type":"int"},"day_end":{"type":"int"},"day_scope":{"type":"string","enum":["all","workday","weekend"]},"day_of_week":{"type":"array","items":{"type":"int"}},"week":{"type":"int"},"week_filter":{"type":"array","items":{"type":"int"}},"week_from":{"type":"int"},"week_to":{"type":"int"},"slot_type":{"type":"string"},"slot_types":{"type":"array","items":{"type":"string"}},"exclude_sections":{"type":"array","items":{"type":"int"}},"after_section":{"type":"int"},"before_section":{"type":"int"},"section_from":{"type":"int"},"section_to":{"type":"int"}}}`,
		NewQueryAvailableSlotsToolHandler(),
	)
	r.Register(
		"query_target_tasks",
		"查询候选任务集合，可按 status/week/day/task_id/category 筛选；支持 enqueue。",
		`{"name":"query_target_tasks","parameters":{"status":{"type":"string","enum":["all","existing","suggested","pending"]},"category":{"type":"string"},"limit":{"type":"int"},"day_scope":{"type":"string","enum":["all","workday","weekend"]},"day":{"type":"int"},"day_start":{"type":"int"},"day_end":{"type":"int"},"day_of_week":{"type":"array","items":{"type":"int"}},"week":{"type":"int"},"week_filter":{"type":"array","items":{"type":"int"}},"week_from":{"type":"int"},"week_to":{"type":"int"},"task_ids":{"type":"array","items":{"type":"int"}},"task_id":{"type":"int"},"task_item_ids":{"type":"array","items":{"type":"int"}},"task_item_id":{"type":"int"},"enqueue":{"type":"bool"},"reset_queue":{"type":"bool"}}}`,
		NewQueryTargetTasksToolHandler(),
	)
	r.Register(
		"queue_pop_head",
		"弹出并返回当前队首任务；若已有 current 则复用。",
		`{"name":"queue_pop_head","parameters":{}}`,
		NewQueuePopHeadToolHandler(),
	)
	r.Register(
		"queue_status",
		"查看当前队列状态（pending/current/completed/skipped）。",
		`{"name":"queue_status","parameters":{}}`,
		NewQueueStatusToolHandler(),
	)
	r.Register(
		"get_task_info",
		"查看单个任务详情，包括类别、状态与落位。",
		`{"name":"get_task_info","parameters":{"task_id":{"type":"int","required":true}}}`,
		NewGetTaskInfoToolHandler(),
	)
}

func registerScheduleAnalyzeTools(r *ToolRegistry) {
	r.Register(
		"analyze_rhythm",
		"分析学习节奏与切换情况。",
		`{"name":"analyze_rhythm","parameters":{"category":{"type":"string"},"include_pending":{"type":"bool"},"detail":{"type":"string","enum":["summary","full"]},"hard_categories":{"type":"array","items":{"type":"string"}}}}`,
		NewAnalyzeRhythmToolHandler(),
	)
	r.Register(
		"analyze_health",
		"主动优化裁判入口：聚焦 rhythm/semantic_profile/tightness，判断当前是否还值得继续优化，并给出候选。",
		`{"name":"analyze_health","parameters":{"detail":{"type":"string","enum":["summary","full"]},"dimensions":{"type":"array","items":{"type":"string"}},"threshold":{"type":"string","enum":["strict","normal","relaxed"]}}}`,
		NewAnalyzeHealthToolHandler(),
	)
}

func registerScheduleMutationTools(r *ToolRegistry) {
	r.Register(
		"place",
		"将一个待安排任务预排到指定位置。task_id/day/slot_start 必填。",
		`{"name":"place","parameters":{"task_id":{"type":"int","required":true},"day":{"type":"int","required":true},"slot_start":{"type":"int","required":true}}}`,
		NewPlaceToolHandler(),
	)
	r.Register(
		"move",
		"将一个已预排任务（仅 suggested）移动到新位置。task_id/new_day/new_slot_start 必填。",
		`{"name":"move","parameters":{"task_id":{"type":"int","required":true},"new_day":{"type":"int","required":true},"new_slot_start":{"type":"int","required":true}}}`,
		NewMoveToolHandler(),
	)
	r.Register(
		"swap",
		"交换两个已落位任务的位置。task_a/task_b 必填，且两任务时长必须一致。",
		`{"name":"swap","parameters":{"task_a":{"type":"int","required":true},"task_b":{"type":"int","required":true}}}`,
		NewSwapToolHandler(),
	)
	r.Register(
		"batch_move",
		"原子性批量移动多个任务。moves 必填。",
		`{"name":"batch_move","parameters":{"moves":{"type":"array","required":true,"items":{"task_id":"int","new_day":"int","new_slot_start":"int"}}}}`,
		NewBatchMoveToolHandler(),
	)
	r.Register(
		"queue_apply_head_move",
		"将当前队首任务移动到指定位置并自动出队。new_day/new_slot_start 必填。",
		`{"name":"queue_apply_head_move","parameters":{"new_day":{"type":"int","required":true},"new_slot_start":{"type":"int","required":true}}}`,
		NewQueueApplyHeadMoveToolHandler(),
	)
	r.Register(
		"queue_skip_head",
		"跳过当前队首任务，将其标记为 skipped。",
		`{"name":"queue_skip_head","parameters":{"reason":{"type":"string"}}}`,
		NewQueueSkipHeadToolHandler(),
	)
	r.Register(
		"unplace",
		"将一个已落位任务移除，恢复为待安排状态。task_id 必填。",
		`{"name":"unplace","parameters":{"task_id":{"type":"int","required":true}}}`,
		NewUnplaceToolHandler(),
	)
}

func registerTaskClassTools(r *ToolRegistry, deps DefaultRegistryDeps) {
	r.Register(
		"upsert_task_class",
		"创建或更新任务类（统一写入口，必须 confirm）。auto 模式下 start_date/end_date 必须在 task_class 顶层字段。",
		`{"name":"upsert_task_class","parameters":{"id":{"type":"int"},"task_class":{"type":"object","required":true},"items":{"type":"array","items":{"type":"object"}},"source":{"type":"string"}}}`,
		NewTaskClassUpsertToolHandler(deps.TaskClassWriteDeps),
	)
}

func registerWebTools(r *ToolRegistry, deps DefaultRegistryDeps) {
	r.Register(
		"web_search",
		"Web 搜索：根据 query 返回结构化检索结果。query 必填。",
		`{"name":"web_search","parameters":{"query":{"type":"string","required":true},"top_k":{"type":"int"},"domain_allow":{"type":"array","items":{"type":"string"}},"recency_days":{"type":"int"}}}`,
		NewWebSearchToolHandler(deps.WebSearchProvider),
	)
	r.Register(
		"web_fetch",
		"抓取指定 URL 的正文内容并做最小清洗。url 必填。",
		`{"name":"web_fetch","parameters":{"url":{"type":"string","required":true},"max_chars":{"type":"int"}}}`,
		NewWebFetchToolHandler(web.NewFetcher()),
	)
}
