package scheduleplan

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/LoveLosita/smartflow/backend/model"
)

// ── ReAct Tool 调用/结果结构 ──

// reactToolCall 是 LLM 输出的单个工具调用。
type reactToolCall struct {
	Tool   string         `json:"tool"`
	Params map[string]any `json:"params"`
}

// reactToolResult 是单个工具调用的执行结果。
type reactToolResult struct {
	Tool    string `json:"tool"`
	Success bool   `json:"success"`
	Result  string `json:"result"`
}

// reactLLMOutput 是 LLM 输出的完整 JSON 结构。
type reactLLMOutput struct {
	Done      bool            `json:"done"`
	Summary   string          `json:"summary"`
	ToolCalls []reactToolCall `json:"tool_calls"`
}

// ── 工具分发器 ──

// dispatchReactTool 根据工具名分发调用，返回（可能修改后的）entries 和执行结果。
func dispatchReactTool(entries []model.HybridScheduleEntry, call reactToolCall) ([]model.HybridScheduleEntry, reactToolResult) {
	switch call.Tool {
	case "Swap":
		return reactToolSwap(entries, call.Params)
	case "Move":
		return reactToolMove(entries, call.Params)
	case "TimeAvailable":
		return entries, reactToolTimeAvailable(entries, call.Params)
	case "GetAvailableSlots":
		return entries, reactToolGetAvailableSlots(entries, call.Params)
	default:
		return entries, reactToolResult{Tool: call.Tool, Success: false, Result: fmt.Sprintf("未知工具: %s", call.Tool)}
	}
}

// ── 参数提取辅助 ──

func paramInt(params map[string]any, key string) (int, bool) {
	v, ok := params[key]
	if !ok {
		return 0, false
	}
	switch n := v.(type) {
	case float64:
		return int(n), true
	case int:
		return n, true
	default:
		return 0, false
	}
}

// findSuggestedByID 在 entries 中查找指定 TaskItemID 的 suggested 条目索引。
func findSuggestedByID(entries []model.HybridScheduleEntry, taskItemID int) int {
	for i, e := range entries {
		if e.TaskItemID == taskItemID && e.Status == "suggested" {
			return i
		}
	}
	return -1
}

// sectionsOverlap 判断两个节次区间是否有交集。
func sectionsOverlap(aFrom, aTo, bFrom, bTo int) bool {
	return aFrom <= bTo && bFrom <= aTo
}

// hasConflict 检查目标时间段是否与 entries 中任何条目冲突（排除 excludeIdx）。
func hasConflict(entries []model.HybridScheduleEntry, week, day, sf, st, excludeIdx int) (bool, string) {
	for i, e := range entries {
		if i == excludeIdx {
			continue
		}
		if e.Week == week && e.DayOfWeek == day && sectionsOverlap(e.SectionFrom, e.SectionTo, sf, st) {
			return true, fmt.Sprintf("%s(%s)", e.Name, e.Type)
		}
	}
	return false, ""
}

// ══════════════════════════════════════════════════════════════
//  Tool 1: Swap — 交换两个 suggested 任务的时间
// ══════════════════════════════════════════════════════════════

func reactToolSwap(entries []model.HybridScheduleEntry, params map[string]any) ([]model.HybridScheduleEntry, reactToolResult) {
	idA, okA := paramInt(params, "task_a")
	idB, okB := paramInt(params, "task_b")
	if !okA || !okB {
		return entries, reactToolResult{Tool: "Swap", Success: false, Result: "参数缺失：需要 task_a 和 task_b（task_item_id）"}
	}
	if idA == idB {
		return entries, reactToolResult{Tool: "Swap", Success: false, Result: "task_a 和 task_b 不能相同"}
	}

	idxA := findSuggestedByID(entries, idA)
	idxB := findSuggestedByID(entries, idB)
	if idxA == -1 {
		return entries, reactToolResult{Tool: "Swap", Success: false, Result: fmt.Sprintf("找不到 task_item_id=%d 的 suggested 任务", idA)}
	}
	if idxB == -1 {
		return entries, reactToolResult{Tool: "Swap", Success: false, Result: fmt.Sprintf("找不到 task_item_id=%d 的 suggested 任务", idB)}
	}

	// 交换时间坐标
	a, b := &entries[idxA], &entries[idxB]
	a.Week, b.Week = b.Week, a.Week
	a.DayOfWeek, b.DayOfWeek = b.DayOfWeek, a.DayOfWeek
	a.SectionFrom, b.SectionFrom = b.SectionFrom, a.SectionFrom
	a.SectionTo, b.SectionTo = b.SectionTo, a.SectionTo

	return entries, reactToolResult{
		Tool: "Swap", Success: true,
		Result: fmt.Sprintf("已交换 [%s](id=%d) 和 [%s](id=%d) 的时间", a.Name, idA, b.Name, idB),
	}
}

// ══════════════════════════════════════════════════════════════
//  Tool 2: Move — 将一个 suggested 任务移动到新时间
// ══════════════════════════════════════════════════════════════

func reactToolMove(entries []model.HybridScheduleEntry, params map[string]any) ([]model.HybridScheduleEntry, reactToolResult) {
	taskID, ok := paramInt(params, "task_item_id")
	if !ok {
		return entries, reactToolResult{Tool: "Move", Success: false, Result: "参数缺失：需要 task_item_id"}
	}
	toWeek, ok1 := paramInt(params, "to_week")
	toDay, ok2 := paramInt(params, "to_day")
	toSF, ok3 := paramInt(params, "to_section_from")
	toST, ok4 := paramInt(params, "to_section_to")
	if !ok1 || !ok2 || !ok3 || !ok4 {
		return entries, reactToolResult{Tool: "Move", Success: false, Result: "参数缺失：需要 to_week, to_day, to_section_from, to_section_to"}
	}

	// 基础校验
	if toDay < 1 || toDay > 7 {
		return entries, reactToolResult{Tool: "Move", Success: false, Result: fmt.Sprintf("day_of_week=%d 不合法，应为 1-7", toDay)}
	}
	if toSF < 1 || toST > 12 || toSF > toST {
		return entries, reactToolResult{Tool: "Move", Success: false, Result: fmt.Sprintf("节次范围 %d-%d 不合法，应为 1-12 且 from<=to", toSF, toST)}
	}

	idx := findSuggestedByID(entries, taskID)
	if idx == -1 {
		return entries, reactToolResult{Tool: "Move", Success: false, Result: fmt.Sprintf("找不到 task_item_id=%d 的 suggested 任务", taskID)}
	}

	// 节次跨度必须一致
	origSpan := entries[idx].SectionTo - entries[idx].SectionFrom
	newSpan := toST - toSF
	if origSpan != newSpan {
		return entries, reactToolResult{Tool: "Move", Success: false,
			Result: fmt.Sprintf("节次跨度不一致：原任务占 %d 节，目标占 %d 节", origSpan+1, newSpan+1)}
	}

	// 冲突检测（排除自身）
	if conflict, name := hasConflict(entries, toWeek, toDay, toSF, toST, idx); conflict {
		return entries, reactToolResult{Tool: "Move", Success: false,
			Result: fmt.Sprintf("目标时间 W%dD%d 第%d-%d节 已被 %s 占用", toWeek, toDay, toSF, toST, name)}
	}

	// 执行移动
	e := &entries[idx]
	oldDesc := fmt.Sprintf("W%dD%d 第%d-%d节", e.Week, e.DayOfWeek, e.SectionFrom, e.SectionTo)
	e.Week, e.DayOfWeek, e.SectionFrom, e.SectionTo = toWeek, toDay, toSF, toST
	newDesc := fmt.Sprintf("W%dD%d 第%d-%d节", toWeek, toDay, toSF, toST)

	return entries, reactToolResult{
		Tool: "Move", Success: true,
		Result: fmt.Sprintf("已将 [%s](id=%d) 从 %s 移动到 %s", e.Name, taskID, oldDesc, newDesc),
	}
}

// ══════════════════════════════════════════════════════════════
//  Tool 3: TimeAvailable — 检查目标时间段是否可用
// ══════════════════════════════════════════════════════════════

func reactToolTimeAvailable(entries []model.HybridScheduleEntry, params map[string]any) reactToolResult {
	week, ok1 := paramInt(params, "week")
	day, ok2 := paramInt(params, "day_of_week")
	sf, ok3 := paramInt(params, "section_from")
	st, ok4 := paramInt(params, "section_to")
	if !ok1 || !ok2 || !ok3 || !ok4 {
		return reactToolResult{Tool: "TimeAvailable", Success: false, Result: "参数缺失：需要 week, day_of_week, section_from, section_to"}
	}

	if conflict, name := hasConflict(entries, week, day, sf, st, -1); conflict {
		return reactToolResult{Tool: "TimeAvailable", Success: true,
			Result: fmt.Sprintf(`{"available":false,"conflict_with":"%s"}`, name)}
	}
	return reactToolResult{Tool: "TimeAvailable", Success: true, Result: `{"available":true}`}
}

// ══════════════════════════════════════════════════════════════
//  Tool 4: GetAvailableSlots — 返回可用时间段列表
// ══════════════════════════════════════════════════════════════

func reactToolGetAvailableSlots(entries []model.HybridScheduleEntry, params map[string]any) reactToolResult {
	filterWeek, _ := paramInt(params, "week") // 0 表示不过滤

	// 1. 收集所有周次范围
	minW, maxW := 999, 0
	for _, e := range entries {
		if e.Week < minW {
			minW = e.Week
		}
		if e.Week > maxW {
			maxW = e.Week
		}
	}
	if minW > maxW {
		return reactToolResult{Tool: "GetAvailableSlots", Success: true, Result: "[]"}
	}

	// 2. 构建占用集合
	type slotKey struct{ W, D, S int }
	occupied := make(map[slotKey]bool)
	for _, e := range entries {
		for s := e.SectionFrom; s <= e.SectionTo; s++ {
			occupied[slotKey{e.Week, e.DayOfWeek, s}] = true
		}
	}

	// 3. 遍历所有时间格，找出空闲并合并连续节次
	type availSlot struct {
		Week, Day, From, To int
	}
	var slots []availSlot

	startW, endW := minW, maxW
	if filterWeek > 0 {
		startW, endW = filterWeek, filterWeek
	}

	for w := startW; w <= endW; w++ {
		for d := 1; d <= 7; d++ {
			runStart := 0
			for s := 1; s <= 12; s++ {
				if !occupied[slotKey{w, d, s}] {
					if runStart == 0 {
						runStart = s
					}
				} else {
					if runStart > 0 {
						slots = append(slots, availSlot{w, d, runStart, s - 1})
						runStart = 0
					}
				}
			}
			if runStart > 0 {
				slots = append(slots, availSlot{w, d, runStart, 12})
			}
		}
	}

	// 4. 按自然顺序排序（已经是了，但确保）
	sort.Slice(slots, func(i, j int) bool {
		if slots[i].Week != slots[j].Week {
			return slots[i].Week < slots[j].Week
		}
		if slots[i].Day != slots[j].Day {
			return slots[i].Day < slots[j].Day
		}
		return slots[i].From < slots[j].From
	})

	// 5. 序列化
	type slotJSON struct {
		Week        int `json:"week"`
		DayOfWeek   int `json:"day_of_week"`
		SectionFrom int `json:"section_from"`
		SectionTo   int `json:"section_to"`
	}
	out := make([]slotJSON, 0, len(slots))
	for _, s := range slots {
		out = append(out, slotJSON{s.Week, s.Day, s.From, s.To})
	}

	data, _ := json.Marshal(out)
	return reactToolResult{Tool: "GetAvailableSlots", Success: true, Result: string(data)}
}

// ── 辅助：解析 LLM 输出 ──

// parseReactLLMOutput 解析 LLM 的 JSON 输出。
// 兼容 ```json ... ``` 包裹。
func parseReactLLMOutput(raw string) (*reactLLMOutput, error) {
	clean := strings.TrimSpace(raw)
	if clean == "" {
		return nil, fmt.Errorf("LLM 输出为空")
	}
	// 兼容 markdown 包裹
	if strings.HasPrefix(clean, "```") {
		clean = strings.TrimPrefix(clean, "```json")
		clean = strings.TrimPrefix(clean, "```")
		clean = strings.TrimSuffix(clean, "```")
		clean = strings.TrimSpace(clean)
	}

	var out reactLLMOutput
	if err := json.Unmarshal([]byte(clean), &out); err == nil {
		return &out, nil
	}

	// 提取最外层 JSON 对象
	start := strings.Index(clean, "{")
	end := strings.LastIndex(clean, "}")
	if start == -1 || end == -1 || end <= start {
		return nil, fmt.Errorf("无法从 LLM 输出中提取 JSON: %s", truncate(clean, 200))
	}
	obj := clean[start : end+1]
	if err := json.Unmarshal([]byte(obj), &out); err != nil {
		return nil, fmt.Errorf("JSON 解析失败: %w", err)
	}
	return &out, nil
}

// truncate 截断字符串到指定长度。
func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}
