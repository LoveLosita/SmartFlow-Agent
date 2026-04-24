package newagenttools

import (
	"fmt"
	"sort"
	"strings"

	infrarag "github.com/LoveLosita/smartflow/backend/infra/rag"
	"github.com/LoveLosita/smartflow/backend/newAgent/tools/schedule"
	"github.com/LoveLosita/smartflow/backend/newAgent/tools/web"
)

// ToolHandler 是所有工具的统一执行签名。
type ToolHandler func(state *schedule.ScheduleState, args map[string]any) string

// ToolSchemaEntry 是注入给模型的工具说明快照。
type ToolSchemaEntry struct {
	Name       string
	Desc       string
	SchemaText string
}

// DefaultRegistryDeps 描述默认工具注册表可选依赖。
//
// 说明：
// 1. 这层依赖注入先为后续 websearch / memory 工具预留统一入口；
// 2. 当前即便部分依赖暂未使用，也不应让业务侧再自行 new 底层 Infra；
// 3. 后续新增读工具时，应优先在这里扩展依赖而不是走包级全局变量。
type DefaultRegistryDeps struct {
	RAGRuntime infrarag.Runtime

	// WebSearchProvider Web 搜索供应商。为 nil 时 web_search / web_fetch 返回"暂未启用"，不阻断主流程。
	WebSearchProvider web.SearchProvider
}

// ToolRegistry 管理工具注册、查找与执行。
type ToolRegistry struct {
	handlers map[string]ToolHandler
	schemas  []ToolSchemaEntry
	deps     DefaultRegistryDeps
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
func (r *ToolRegistry) Execute(state *schedule.ScheduleState, toolName string, args map[string]any) string {
	handler, ok := r.handlers[toolName]
	if !ok {
		return fmt.Sprintf("工具调用失败：未知工具 %q。可用工具：%s", toolName, strings.Join(r.ToolNames(), "、"))
	}
	return handler(state, args)
}

// HasTool 检查工具是否已注册。
func (r *ToolRegistry) HasTool(name string) bool {
	_, ok := r.handlers[name]
	return ok
}

// ToolNames 返回已注册工具名（按 schema 顺序）。
func (r *ToolRegistry) ToolNames() []string {
	names := make([]string, 0, len(r.handlers))
	for _, item := range r.schemas {
		names = append(names, item.Name)
	}
	return names
}

// Schemas 返回 schema 快照。
func (r *ToolRegistry) Schemas() []ToolSchemaEntry {
	result := make([]ToolSchemaEntry, len(r.schemas))
	copy(result, r.schemas)
	return result
}

// IsWriteTool 判断工具是否是写工具（需要 confirm）。
func (r *ToolRegistry) IsWriteTool(name string) bool {
	return writeTools[name]
}

// RequiresScheduleState 判断工具是否依赖 ScheduleState。
// 调用目的：execute 节点据此决定是否允许在 ScheduleState 为 nil 时调用该工具。
func (r *ToolRegistry) RequiresScheduleState(name string) bool {
	return !scheduleFreeTools[name]
}

// ==================== 写工具集合 ====================

var writeTools = map[string]bool{
	"place":                 true,
	"move":                  true,
	"swap":                  true,
	"batch_move":            true,
	"queue_apply_head_move": true,
	"spread_even":           true,
	"min_context_switch":    true,
	"unplace":               true,
}

// ==================== 不依赖 ScheduleState 的工具集合 ====================
// 调用目的：这些工具不需要日程状态即可执行，execute 节点在 ScheduleState 为 nil 时允许调用。

var scheduleFreeTools = map[string]bool{
	"web_search": true,
	"web_fetch":  true,
}

// ==================== 默认注册表 ====================

// NewDefaultRegistry 创建默认日程工具注册表。
func NewDefaultRegistry() *ToolRegistry {
	return NewDefaultRegistryWithDeps(DefaultRegistryDeps{})
}

// NewDefaultRegistryWithDeps 创建带依赖的默认日程工具注册表。
func NewDefaultRegistryWithDeps(deps DefaultRegistryDeps) *ToolRegistry {
	r := NewToolRegistryWithDeps(deps)

	// --- 读工具 ---
	r.Register("get_overview",
		"获取规划窗口总览（任务视角，全量返回）：保留课程占位统计，展开任务清单（过滤课程明细）。",
		`{"name":"get_overview","parameters":{}}`,
		func(state *schedule.ScheduleState, args map[string]any) string {
			return schedule.GetOverview(state)
		},
	)

	r.Register("query_range",
		"查看某天或某时段的细粒度占用详情。day 必填，slot_start/slot_end 选填（不填查整天）。",
		`{"name":"query_range","parameters":{"day":{"type":"int","required":true},"slot_start":{"type":"int"},"slot_end":{"type":"int"}}}`,
		func(state *schedule.ScheduleState, args map[string]any) string {
			day, ok := schedule.ArgsInt(args, "day")
			if !ok {
				return "查询失败：缺少必填参数 day。"
			}
			return schedule.QueryRange(state, day, schedule.ArgsIntPtr(args, "slot_start"), schedule.ArgsIntPtr(args, "slot_end"))
		},
	)

	r.Register("query_available_slots",
		"查询候选空位池（先返回纯空位，不足再补可嵌入位），适合 move 前的落点筛选。",
		`{"name":"query_available_slots","parameters":{"span":{"type":"int"},"duration":{"type":"int"},"limit":{"type":"int"},"allow_embed":{"type":"bool"},"day":{"type":"int"},"day_start":{"type":"int"},"day_end":{"type":"int"},"day_scope":{"type":"string","enum":["all","workday","weekend"]},"day_of_week":{"type":"array","items":{"type":"int"}},"week":{"type":"int"},"week_filter":{"type":"array","items":{"type":"int"}},"week_from":{"type":"int"},"week_to":{"type":"int"},"slot_type":{"type":"string"},"slot_types":{"type":"array","items":{"type":"string"}},"exclude_sections":{"type":"array","items":{"type":"int"}},"after_section":{"type":"int"},"before_section":{"type":"int"},"section_from":{"type":"int"},"section_to":{"type":"int"}}}`,
		func(state *schedule.ScheduleState, args map[string]any) string {
			return schedule.QueryAvailableSlots(state, args)
		},
	)

	r.Register("query_target_tasks",
		"查询候选任务集合，可按 status/week/day/task_id/category 筛选；默认自动入队，供后续 queue_pop_head 逐项处理。",
		`{"name":"query_target_tasks","parameters":{"status":{"type":"string","enum":["all","existing","suggested","pending"]},"category":{"type":"string"},"limit":{"type":"int"},"day_scope":{"type":"string","enum":["all","workday","weekend"]},"day":{"type":"int"},"day_start":{"type":"int"},"day_end":{"type":"int"},"day_of_week":{"type":"array","items":{"type":"int"}},"week":{"type":"int"},"week_filter":{"type":"array","items":{"type":"int"}},"week_from":{"type":"int"},"week_to":{"type":"int"},"task_ids":{"type":"array","items":{"type":"int"}},"task_id":{"type":"int"},"task_item_ids":{"type":"array","items":{"type":"int"}},"task_item_id":{"type":"int"},"enqueue":{"type":"bool"},"reset_queue":{"type":"bool"}}}`,
		func(state *schedule.ScheduleState, args map[string]any) string {
			return schedule.QueryTargetTasks(state, args)
		},
	)

	r.Register("queue_pop_head",
		"弹出并返回当前队首任务；若已有 current 则复用，保证一次只处理一个任务。",
		`{"name":"queue_pop_head","parameters":{}}`,
		func(state *schedule.ScheduleState, args map[string]any) string {
			return schedule.QueuePopHead(state, args)
		},
	)

	r.Register("queue_status",
		"查看当前待处理队列状态（pending/current/completed/skipped）。",
		`{"name":"queue_status","parameters":{}}`,
		func(state *schedule.ScheduleState, args map[string]any) string {
			return schedule.QueueStatus(state, args)
		},
	)

	r.Register("get_task_info",
		"查询单个任务详细信息，包括类别、状态、占用时段、嵌入关系。",
		`{"name":"get_task_info","parameters":{"task_id":{"type":"int","required":true}}}`,
		func(state *schedule.ScheduleState, args map[string]any) string {
			taskID, ok := schedule.ArgsInt(args, "task_id")
			if !ok {
				return "查询失败：缺少必填参数 task_id。"
			}
			return schedule.GetTaskInfo(state, taskID)
		},
	)

	// --- 写工具 ---
	r.Register("place",
		"将一个待安排任务预排到指定位置。自动检测可嵌入宿主。task_id/day/slot_start 必填。",
		`{"name":"place","parameters":{"task_id":{"type":"int","required":true},"day":{"type":"int","required":true},"slot_start":{"type":"int","required":true}}}`,
		func(state *schedule.ScheduleState, args map[string]any) string {
			taskID, ok := schedule.ArgsInt(args, "task_id")
			if !ok {
				return "放置失败：缺少必填参数 task_id。"
			}
			day, ok := schedule.ArgsInt(args, "day")
			if !ok {
				return "放置失败：缺少必填参数 day。"
			}
			slotStart, ok := schedule.ArgsInt(args, "slot_start")
			if !ok {
				return "放置失败：缺少必填参数 slot_start。"
			}
			return schedule.Place(state, taskID, day, slotStart)
		},
	)

	r.Register("move",
		"将一个已预排任务（仅 suggested）移动到新位置。existing 属于已安排事实层，不参与 move。task_id/new_day/new_slot_start 必填。",
		`{"name":"move","parameters":{"task_id":{"type":"int","required":true},"new_day":{"type":"int","required":true},"new_slot_start":{"type":"int","required":true}}}`,
		func(state *schedule.ScheduleState, args map[string]any) string {
			taskID, ok := schedule.ArgsInt(args, "task_id")
			if !ok {
				return "移动失败：缺少必填参数 task_id。"
			}
			newDay, ok := schedule.ArgsInt(args, "new_day")
			if !ok {
				return "移动失败：缺少必填参数 new_day。"
			}
			newSlotStart, ok := schedule.ArgsInt(args, "new_slot_start")
			if !ok {
				return "移动失败：缺少必填参数 new_slot_start。"
			}
			return schedule.Move(state, taskID, newDay, newSlotStart)
		},
	)

	r.Register("swap",
		"交换两个已落位任务的位置。两个任务必须时长相同。task_a/task_b 必填。",
		`{"name":"swap","parameters":{"task_a":{"type":"int","required":true},"task_b":{"type":"int","required":true}}}`,
		func(state *schedule.ScheduleState, args map[string]any) string {
			taskA, ok := schedule.ArgsInt(args, "task_a")
			if !ok {
				return "交换失败：缺少必填参数 task_a。"
			}
			taskB, ok := schedule.ArgsInt(args, "task_b")
			if !ok {
				return "交换失败：缺少必填参数 task_b。"
			}
			return schedule.Swap(state, taskA, taskB)
		},
	)

	r.Register("batch_move",
		"原子性批量移动多个任务（仅 suggested，最多2条），全部成功才生效。若含 existing/pending 或任一冲突将整批失败回滚。",
		`{"name":"batch_move","parameters":{"moves":{"type":"array","required":true,"items":{"task_id":"int","new_day":"int","new_slot_start":"int"}}}}`,
		func(state *schedule.ScheduleState, args map[string]any) string {
			moves, err := schedule.ArgsMoveList(args)
			if err != nil {
				return fmt.Sprintf("批量移动失败：%s", err.Error())
			}
			return schedule.BatchMove(state, moves)
		},
	)

	r.Register("queue_apply_head_move",
		"将当前队首任务移动到指定位置并自动出队。仅作用于 current，不接受 task_id。new_day/new_slot_start 必填。",
		`{"name":"queue_apply_head_move","parameters":{"new_day":{"type":"int","required":true},"new_slot_start":{"type":"int","required":true}}}`,
		func(state *schedule.ScheduleState, args map[string]any) string {
			return schedule.QueueApplyHeadMove(state, args)
		},
	)

	r.Register("queue_skip_head",
		"跳过当前队首任务（不改日程），将其标记为 skipped 并继续后续队列。",
		`{"name":"queue_skip_head","parameters":{"reason":{"type":"string"}}}`,
		func(state *schedule.ScheduleState, args map[string]any) string {
			return schedule.QueueSkipHead(state, args)
		},
	)

	r.Register("min_context_switch",
		"在指定任务集合内重排 suggested 任务，尽量让同类任务连续以减少上下文切换。仅在用户明确允许打乱顺序时使用。task_ids 必填（兼容 task_id）。",
		`{"name":"min_context_switch","parameters":{"task_ids":{"type":"array","required":true,"items":{"type":"int"}},"task_id":{"type":"int"}}}`,
		func(state *schedule.ScheduleState, args map[string]any) string {
			taskIDs, err := schedule.ParseMinContextSwitchTaskIDs(args)
			if err != nil {
				return fmt.Sprintf("减少上下文切换失败：%s。", err.Error())
			}
			return schedule.MinContextSwitch(state, taskIDs)
		},
	)

	r.Register("spread_even",
		"在给定任务集合内做均匀化铺开：先按筛选条件收集候选坑位，再规划并原子落地。task_ids 必填（兼容 task_id）。",
		`{"name":"spread_even","parameters":{"task_ids":{"type":"array","required":true,"items":{"type":"int"}},"task_id":{"type":"int"},"limit":{"type":"int"},"allow_embed":{"type":"bool"},"day":{"type":"int"},"day_start":{"type":"int"},"day_end":{"type":"int"},"day_scope":{"type":"string","enum":["all","workday","weekend"]},"day_of_week":{"type":"array","items":{"type":"int"}},"week":{"type":"int"},"week_filter":{"type":"array","items":{"type":"int"}},"week_from":{"type":"int"},"week_to":{"type":"int"},"slot_type":{"type":"string"},"slot_types":{"type":"array","items":{"type":"string"}},"exclude_sections":{"type":"array","items":{"type":"int"}},"after_section":{"type":"int"},"before_section":{"type":"int"}}}`,
		func(state *schedule.ScheduleState, args map[string]any) string {
			taskIDs, err := schedule.ParseSpreadEvenTaskIDs(args)
			if err != nil {
				return fmt.Sprintf("均匀化调整失败：%s。", err.Error())
			}
			return schedule.SpreadEven(state, taskIDs, args)
		},
	)

	r.Register("unplace",
		"将一个已落位任务移除，恢复为待安排状态。会自动清理嵌入关系。task_id 必填。",
		`{"name":"unplace","parameters":{"task_id":{"type":"int","required":true}}}`,
		func(state *schedule.ScheduleState, args map[string]any) string {
			taskID, ok := schedule.ArgsInt(args, "task_id")
			if !ok {
				return "移除失败：缺少必填参数 task_id。"
			}
			return schedule.Unplace(state, taskID)
		},
	)

	// --- Web 搜索读工具 ---
	// 1. provider 为 nil 时 handler 返回"暂未启用"的 observation，不会阻断主流程；
	// 2. 两个工具均为读操作，走 action=continue + tool_call 模式。
	webSearchHandler := web.NewSearchToolHandler(deps.WebSearchProvider)
	webFetchHandler := web.NewFetchToolHandler(web.NewFetcher())

	r.Register("web_search",
		"Web 搜索：根据 query 返回结构化检索结果（标题/摘要/URL/来源域名/时间）。query 必填。",
		`{"name":"web_search","parameters":{"query":{"type":"string","required":true},"top_k":{"type":"int"},"domain_allow":{"type":"array","items":{"type":"string"}},"recency_days":{"type":"int"}}}`,
		func(state *schedule.ScheduleState, args map[string]any) string {
			return webSearchHandler.Handle(args)
		},
	)

	r.Register("web_fetch",
		"抓取指定 URL 的正文内容并做最小 HTML 清洗。url 必填。",
		`{"name":"web_fetch","parameters":{"url":{"type":"string","required":true},"max_chars":{"type":"int"}}}`,
		func(state *schedule.ScheduleState, args map[string]any) string {
			return webFetchHandler.Handle(args)
		},
	)

	// 按 schema name 排序，确保输出稳定。
	sort.Slice(r.schemas, func(i, j int) bool {
		return r.schemas[i].Name < r.schemas[j].Name
	})

	return r
}
