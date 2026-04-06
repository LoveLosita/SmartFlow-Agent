package newagenttools

import (
	"fmt"
	"sort"
	"strings"
)

// ToolHandler 是所有工具的统一执行签名。
// 接收当前 ScheduleState + LLM 输出的原始参数，返回自然语言结果。
type ToolHandler func(state *ScheduleState, args map[string]any) string

// ToolSchemaEntry 是工具描述的轻量快照，用于 LLM prompt 注入。
// 在注入 ConversationContext 时转换为 model.ToolSchemaContext。
type ToolSchemaEntry struct {
	Name       string
	Desc       string
	SchemaText string
}

// ToolRegistry 管理所有工具的注册、查找和执行。
//
// 职责边界：
// 1. 负责工具名 → handler 的映射；
// 2. 负责工具 schema 的存储（供 LLM prompt 注入）；
// 3. 不负责 ScheduleState 的生命周期管理；
// 4. 不负责 confirm 流程（由 execute.go 的 action 分支处理）。
type ToolRegistry struct {
	handlers map[string]ToolHandler
	schemas  []ToolSchemaEntry
}

// NewToolRegistry 创建空注册表。
func NewToolRegistry() *ToolRegistry {
	return &ToolRegistry{
		handlers: make(map[string]ToolHandler),
		schemas:  make([]ToolSchemaEntry, 0),
	}
}

// Register 注册一个工具及其 schema 描述。
func (r *ToolRegistry) Register(name, desc, schemaText string, handler ToolHandler) {
	r.handlers[name] = handler
	r.schemas = append(r.schemas, ToolSchemaEntry{
		Name:       name,
		Desc:       desc,
		SchemaText: schemaText,
	})
}

// Execute 执行指定工具。
// 工具名不存在时返回错误提示字符串。
func (r *ToolRegistry) Execute(state *ScheduleState, toolName string, args map[string]any) string {
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

// ToolNames 返回所有已注册工具名（按注册顺序）。
func (r *ToolRegistry) ToolNames() []string {
	names := make([]string, 0, len(r.handlers))
	for _, s := range r.schemas {
		names = append(names, s.Name)
	}
	return names
}

// Schemas 返回所有工具的 schema 描述（供 LLM prompt 注入）。
func (r *ToolRegistry) Schemas() []ToolSchemaEntry {
	result := make([]ToolSchemaEntry, len(r.schemas))
	copy(result, r.schemas)
	return result
}

// IsWriteTool 判断指定工具是否为写工具（需要 confirm 流程）。
func (r *ToolRegistry) IsWriteTool(name string) bool {
	return writeTools[name]
}

// ==================== 写工具名集合 ====================

var writeTools = map[string]bool{
	"place":      true,
	"move":       true,
	"swap":       true,
	"batch_move": true,
	"unplace":    true,
}

// ==================== 默认注册表 ====================

// NewDefaultRegistry 创建包含全部 10 个日程工具的注册表。
func NewDefaultRegistry() *ToolRegistry {
	r := NewToolRegistry()

	// --- 读工具 ---
	r.Register("get_overview",
		"获取规划窗口的粗粒度总览，包括每日占用、可嵌入时段和待安排任务。",
		`{"name":"get_overview","parameters":{}}`,
		func(state *ScheduleState, args map[string]any) string {
			return GetOverview(state)
		},
	)

	r.Register("query_range",
		"查看某天或某时段的细粒度占用详情。day 必填，slot_start/slot_end 选填（不填查整天）。",
		`{"name":"query_range","parameters":{"day":{"type":"int","required":true},"slot_start":{"type":"int"},"slot_end":{"type":"int"}}}`,
		func(state *ScheduleState, args map[string]any) string {
			day, ok := argsInt(args, "day")
			if !ok {
				return "查询失败：缺少必填参数 day。"
			}
			return QueryRange(state, day, argsIntPtr(args, "slot_start"), argsIntPtr(args, "slot_end"))
		},
	)

	r.Register("find_free",
		"查找满足指定连续时段长度的空闲位置。duration 必填，day 选填（不填搜全部天）。",
		`{"name":"find_free","parameters":{"duration":{"type":"int","required":true},"day":{"type":"int"}}}`,
		func(state *ScheduleState, args map[string]any) string {
			duration, ok := argsInt(args, "duration")
			if !ok {
				return "查询失败：缺少必填参数 duration。"
			}
			return FindFree(state, duration, argsIntPtr(args, "day"))
		},
	)

	r.Register("list_tasks",
		"列出任务清单，可按类别和状态过滤。category 选填，status 选填（默认 all）。",
		`{"name":"list_tasks","parameters":{"category":{"type":"string"},"status":{"type":"string","enum":["all","existing","pending"]}}}`,
		func(state *ScheduleState, args map[string]any) string {
			return ListTasks(state, argsStringPtr(args, "category"), argsStringPtr(args, "status"))
		},
	)

	r.Register("get_task_info",
		"查询单个任务的详细信息，包括类别、状态、占用时段、嵌入关系。",
		`{"name":"get_task_info","parameters":{"task_id":{"type":"int","required":true}}}`,
		func(state *ScheduleState, args map[string]any) string {
			taskID, ok := argsInt(args, "task_id")
			if !ok {
				return "查询失败：缺少必填参数 task_id。"
			}
			return GetTaskInfo(state, taskID)
		},
	)

	// --- 写工具 ---
	r.Register("place",
		"将一个待安排任务放到指定位置。自动检测可嵌入宿主。task_id/day/slot_start 必填。",
		`{"name":"place","parameters":{"task_id":{"type":"int","required":true},"day":{"type":"int","required":true},"slot_start":{"type":"int","required":true}}}`,
		func(state *ScheduleState, args map[string]any) string {
			taskID, ok := argsInt(args, "task_id")
			if !ok {
				return "放置失败：缺少必填参数 task_id。"
			}
			day, ok := argsInt(args, "day")
			if !ok {
				return "放置失败：缺少必填参数 day。"
			}
			slotStart, ok := argsInt(args, "slot_start")
			if !ok {
				return "放置失败：缺少必填参数 slot_start。"
			}
			return Place(state, taskID, day, slotStart)
		},
	)

	r.Register("move",
		"将一个已安排任务移动到新位置。task_id/new_day/new_slot_start 必填。",
		`{"name":"move","parameters":{"task_id":{"type":"int","required":true},"new_day":{"type":"int","required":true},"new_slot_start":{"type":"int","required":true}}}`,
		func(state *ScheduleState, args map[string]any) string {
			taskID, ok := argsInt(args, "task_id")
			if !ok {
				return "移动失败：缺少必填参数 task_id。"
			}
			newDay, ok := argsInt(args, "new_day")
			if !ok {
				return "移动失败：缺少必填参数 new_day。"
			}
			newSlotStart, ok := argsInt(args, "new_slot_start")
			if !ok {
				return "移动失败：缺少必填参数 new_slot_start。"
			}
			return Move(state, taskID, newDay, newSlotStart)
		},
	)

	r.Register("swap",
		"交换两个已安排任务的位置。两个任务必须时长相同。task_a/task_b 必填。",
		`{"name":"swap","parameters":{"task_a":{"type":"int","required":true},"task_b":{"type":"int","required":true}}}`,
		func(state *ScheduleState, args map[string]any) string {
			taskA, ok := argsInt(args, "task_a")
			if !ok {
				return "交换失败：缺少必填参数 task_a。"
			}
			taskB, ok := argsInt(args, "task_b")
			if !ok {
				return "交换失败：缺少必填参数 task_b。"
			}
			return Swap(state, taskA, taskB)
		},
	)

	r.Register("batch_move",
		"原子性批量移动多个任务，全部成功才生效。moves 数组必填。",
		`{"name":"batch_move","parameters":{"moves":{"type":"array","required":true,"items":{"task_id":"int","new_day":"int","new_slot_start":"int"}}}}`,
		func(state *ScheduleState, args map[string]any) string {
			moves, err := argsMoveList(args)
			if err != nil {
				return fmt.Sprintf("批量移动失败：%s", err.Error())
			}
			return BatchMove(state, moves)
		},
	)

	r.Register("unplace",
		"将一个已安排任务移除，恢复为待安排状态。会自动清理嵌入关系。task_id 必填。",
		`{"name":"unplace","parameters":{"task_id":{"type":"int","required":true}}}`,
		func(state *ScheduleState, args map[string]any) string {
			taskID, ok := argsInt(args, "task_id")
			if !ok {
				return "移除失败：缺少必填参数 task_id。"
			}
			return Unplace(state, taskID)
		},
	)

	// 按 schema name 排序，保证输出稳定。
	sort.Slice(r.schemas, func(i, j int) bool {
		return r.schemas[i].Name < r.schemas[j].Name
	})

	return r
}
