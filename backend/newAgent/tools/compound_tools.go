package newagenttools

import (
	"fmt"
	"sort"
	"strings"
)

type minContextSnapshot struct {
	StateID    int
	Name       string
	ContextTag string
	Slot       TaskSlot
}

type minContextPlanTask struct {
	StateID     int
	Name        string
	ContextTag  string
	GroupingKey string
	OriginRank  int
	Span        int
}

type minContextPlanGroup struct {
	Key     string
	MinRank int
	Tasks   []minContextPlanTask
}

// MinContextSwitch 在给定任务集合内重排 suggested 任务，减少上下文切换次数。
//
// 职责边界：
// 1. 只处理“已落位的 suggested 任务”重排，不负责粗排；
// 2. 仅在给定 task_ids 集合内部重排，不改动集合外任务；
// 3. 采用原子提交：任一校验失败则整体不生效。
//
// 并行迁移说明：
// 1. 这里没有直接复用 backend/logic 的同名规划器；
// 2. 原因是 logic 包依赖链会回流到 newAgent/tools，直接引用会产生 import cycle；
// 3. 因此在 tools 层内置一份最小可用的确定性规划逻辑，先保证线上可用，再在后续结构迁移时抽公共层。
func MinContextSwitch(state *ScheduleState, taskIDs []int) string {
	if state == nil {
		return "减少上下文切换失败：日程状态为空。"
	}

	normalizedIDs := uniquePositiveInts(taskIDs)
	if len(normalizedIDs) < 2 {
		return "减少上下文切换失败：task_ids 至少需要 2 个有效任务 ID。"
	}

	// 1. 构建规划输入并做前置校验。
	plannerTasks := make([]minContextPlanTask, 0, len(normalizedIDs))
	plannerSlots := make([]TaskSlot, 0, len(normalizedIDs))
	beforeByID := make(map[int]minContextSnapshot, len(normalizedIDs))
	excludeIDs := make([]int, 0, len(normalizedIDs))

	for rank, taskID := range normalizedIDs {
		task := state.TaskByStateID(taskID)
		if task == nil {
			return fmt.Sprintf("减少上下文切换失败：任务ID %d 不存在。", taskID)
		}
		if !IsSuggestedTask(*task) {
			return fmt.Sprintf("减少上下文切换失败：[%d]%s 不是 suggested 任务，仅 suggested 可参与该工具。", task.StateID, task.Name)
		}
		if err := checkLocked(*task); err != nil {
			return fmt.Sprintf("减少上下文切换失败：%s。", err.Error())
		}
		if len(task.Slots) != 1 {
			return fmt.Sprintf("减少上下文切换失败：[%d]%s 当前包含 %d 段时段，暂不支持该形态。", task.StateID, task.Name, len(task.Slots))
		}

		slot := task.Slots[0]
		if err := validateDay(state, slot.Day); err != nil {
			return fmt.Sprintf("减少上下文切换失败：[%d]%s 的时段非法：%s。", task.StateID, task.Name, err.Error())
		}
		if err := validateSlotRange(slot.SlotStart, slot.SlotEnd); err != nil {
			return fmt.Sprintf("减少上下文切换失败：[%d]%s 的节次非法：%s。", task.StateID, task.Name, err.Error())
		}

		contextTag := normalizeMinContextTag(*task)
		beforeByID[task.StateID] = minContextSnapshot{
			StateID:    task.StateID,
			Name:       task.Name,
			ContextTag: contextTag,
			Slot:       slot,
		}
		excludeIDs = append(excludeIDs, task.StateID)
		plannerTasks = append(plannerTasks, minContextPlanTask{
			StateID:    task.StateID,
			Name:       strings.TrimSpace(task.Name),
			ContextTag: contextTag,
			OriginRank: rank + 1,
			Span:       slot.SlotEnd - slot.SlotStart + 1,
		})
		plannerSlots = append(plannerSlots, slot)
	}

	plannedSlots, err := planMinContextAssignments(plannerTasks, plannerSlots)
	if err != nil {
		return fmt.Sprintf("减少上下文切换失败：%s。", err.Error())
	}

	afterByID := make(map[int]minContextSnapshot, len(beforeByID))
	for taskID, before := range beforeByID {
		targetSlot, ok := plannedSlots[taskID]
		if !ok {
			return "减少上下文切换失败：规划结果不完整。"
		}
		if err := validateDay(state, targetSlot.Day); err != nil {
			return fmt.Sprintf("减少上下文切换失败：任务 [%d]%s 目标天非法：%s。", before.StateID, before.Name, err.Error())
		}
		if err := validateSlotRange(targetSlot.SlotStart, targetSlot.SlotEnd); err != nil {
			return fmt.Sprintf("减少上下文切换失败：任务 [%d]%s 目标节次非法：%s。", before.StateID, before.Name, err.Error())
		}
		if conflict := findConflict(state, targetSlot.Day, targetSlot.SlotStart, targetSlot.SlotEnd, excludeIDs...); conflict != nil {
			return fmt.Sprintf(
				"减少上下文切换失败：任务 [%d]%s 目标位置 %s 与 [%d]%s 冲突。",
				before.StateID,
				before.Name,
				formatDaySlotLabel(state, targetSlot.Day, targetSlot.SlotStart, targetSlot.SlotEnd),
				conflict.StateID,
				conflict.Name,
			)
		}
		afterByID[before.StateID] = minContextSnapshot{
			StateID:    before.StateID,
			Name:       before.Name,
			ContextTag: before.ContextTag,
			Slot:       targetSlot,
		}
	}

	// 2. 全量通过后再原子提交，避免中间态污染。
	clone := state.Clone()
	for taskID, after := range afterByID {
		task := clone.TaskByStateID(taskID)
		if task == nil {
			return fmt.Sprintf("减少上下文切换失败：任务ID %d 在提交阶段不存在。", taskID)
		}
		task.Slots = []TaskSlot{after.Slot}
	}
	state.Tasks = clone.Tasks

	beforeOrdered := sortMinContextSnapshots(beforeByID)
	afterOrdered := sortMinContextSnapshots(afterByID)
	beforeSwitches := countMinContextSwitches(beforeOrdered)
	afterSwitches := countMinContextSwitches(afterOrdered)

	changedLines := make([]string, 0, len(beforeOrdered))
	affectedDays := make(map[int]bool, len(beforeOrdered)*2)
	for _, before := range beforeOrdered {
		after := afterByID[before.StateID]
		if sameTaskSlot(before.Slot, after.Slot) {
			continue
		}
		changedLines = append(changedLines, fmt.Sprintf(
			"  [%d]%s：%s -> %s",
			before.StateID,
			before.Name,
			formatDaySlotLabel(state, before.Slot.Day, before.Slot.SlotStart, before.Slot.SlotEnd),
			formatDaySlotLabel(state, after.Slot.Day, after.Slot.SlotStart, after.Slot.SlotEnd),
		))
		affectedDays[before.Slot.Day] = true
		affectedDays[after.Slot.Day] = true
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf(
		"最少上下文切换重排完成：共处理 %d 个任务，上下文切换次数 %d -> %d。\n",
		len(beforeByID), beforeSwitches, afterSwitches,
	))
	if len(changedLines) == 0 {
		sb.WriteString("当前任务顺序已是较优结果，无需调整。")
		return sb.String()
	}

	sb.WriteString("本次调整：\n")
	for _, line := range changedLines {
		sb.WriteString(line + "\n")
	}
	for _, day := range sortedKeys(affectedDays) {
		sb.WriteString(formatDayOccupancy(state, day) + "\n")
	}
	return strings.TrimSpace(sb.String())
}

func parseMinContextSwitchTaskIDs(args map[string]any) ([]int, error) {
	if ids, ok := argsIntSlice(args, "task_ids"); ok && len(ids) > 0 {
		return ids, nil
	}
	if id, ok := argsInt(args, "task_id"); ok {
		return []int{id}, nil
	}
	return nil, fmt.Errorf("缺少必填参数 task_ids（兼容单值 task_id）")
}

func planMinContextAssignments(tasks []minContextPlanTask, slots []TaskSlot) (map[int]TaskSlot, error) {
	if len(tasks) == 0 {
		return nil, fmt.Errorf("任务列表为空")
	}
	if len(slots) == 0 {
		return nil, fmt.Errorf("可用坑位为空")
	}
	if len(slots) < len(tasks) {
		return nil, fmt.Errorf("可用坑位不足：tasks=%d, slots=%d", len(tasks), len(slots))
	}

	sort.SliceStable(tasks, func(i, j int) bool {
		if tasks[i].OriginRank != tasks[j].OriginRank {
			return tasks[i].OriginRank < tasks[j].OriginRank
		}
		return tasks[i].StateID < tasks[j].StateID
	})
	for i := range tasks {
		tasks[i].GroupingKey = normalizeMinContextGroupingKey(tasks[i].ContextTag)
	}
	applyMinContextNameFallback(tasks)

	groupMap := make(map[string]*minContextPlanGroup, len(tasks))
	groupOrder := make([]string, 0, len(tasks))
	for _, task := range tasks {
		group, exists := groupMap[task.GroupingKey]
		if !exists {
			group = &minContextPlanGroup{
				Key:     task.GroupingKey,
				MinRank: task.OriginRank,
			}
			groupMap[task.GroupingKey] = group
			groupOrder = append(groupOrder, task.GroupingKey)
		}
		if task.OriginRank < group.MinRank {
			group.MinRank = task.OriginRank
		}
		group.Tasks = append(group.Tasks, task)
	}

	groups := make([]minContextPlanGroup, 0, len(groupMap))
	for _, key := range groupOrder {
		group := groupMap[key]
		sort.SliceStable(group.Tasks, func(i, j int) bool {
			if group.Tasks[i].OriginRank != group.Tasks[j].OriginRank {
				return group.Tasks[i].OriginRank < group.Tasks[j].OriginRank
			}
			return group.Tasks[i].StateID < group.Tasks[j].StateID
		})
		groups = append(groups, *group)
	}
	sort.SliceStable(groups, func(i, j int) bool {
		if len(groups[i].Tasks) != len(groups[j].Tasks) {
			return len(groups[i].Tasks) > len(groups[j].Tasks)
		}
		if groups[i].MinRank != groups[j].MinRank {
			return groups[i].MinRank < groups[j].MinRank
		}
		return groups[i].Key < groups[j].Key
	})

	orderedTasks := make([]minContextPlanTask, 0, len(tasks))
	for _, group := range groups {
		orderedTasks = append(orderedTasks, group.Tasks...)
	}

	sortedSlots := make([]TaskSlot, len(slots))
	copy(sortedSlots, slots)
	sort.SliceStable(sortedSlots, func(i, j int) bool {
		if sortedSlots[i].Day != sortedSlots[j].Day {
			return sortedSlots[i].Day < sortedSlots[j].Day
		}
		if sortedSlots[i].SlotStart != sortedSlots[j].SlotStart {
			return sortedSlots[i].SlotStart < sortedSlots[j].SlotStart
		}
		if sortedSlots[i].SlotEnd != sortedSlots[j].SlotEnd {
			return sortedSlots[i].SlotEnd < sortedSlots[j].SlotEnd
		}
		return i < j
	})

	used := make([]bool, len(sortedSlots))
	result := make(map[int]TaskSlot, len(orderedTasks))
	for _, task := range orderedTasks {
		chosenIdx := -1
		for idx, slot := range sortedSlots {
			if used[idx] {
				continue
			}
			if slot.SlotEnd-slot.SlotStart+1 != task.Span {
				continue
			}
			chosenIdx = idx
			break
		}
		if chosenIdx < 0 {
			return nil, fmt.Errorf("任务 id=%d 无可用同跨度坑位", task.StateID)
		}
		used[chosenIdx] = true
		result[task.StateID] = sortedSlots[chosenIdx]
	}
	return result, nil
}

func applyMinContextNameFallback(tasks []minContextPlanTask) {
	distinctExplicit := make(map[string]struct{}, len(tasks))
	distinctNonCoarse := make(map[string]struct{}, len(tasks))
	for _, task := range tasks {
		key := normalizeMinContextGroupingKey(task.GroupingKey)
		distinctExplicit[key] = struct{}{}
		if !isCoarseMinContextKey(key) {
			distinctNonCoarse[key] = struct{}{}
		}
	}
	if len(distinctNonCoarse) >= 2 {
		return
	}
	if len(distinctExplicit) > 1 && len(distinctNonCoarse) > 0 {
		return
	}

	distinctInferred := make(map[string]struct{}, len(tasks))
	for i := range tasks {
		inferred := inferMinContextKeyFromTaskName(tasks[i].Name)
		if inferred == "" {
			inferred = tasks[i].GroupingKey
		}
		tasks[i].GroupingKey = inferred
		distinctInferred[inferred] = struct{}{}
	}
	if len(distinctInferred) < 2 {
		for i := range tasks {
			tasks[i].GroupingKey = normalizeMinContextGroupingKey(tasks[i].ContextTag)
		}
	}
}

func uniquePositiveInts(values []int) []int {
	seen := make(map[int]struct{}, len(values))
	result := make([]int, 0, len(values))
	for _, value := range values {
		if value <= 0 {
			continue
		}
		if _, exists := seen[value]; exists {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	return result
}

func normalizeMinContextTag(task ScheduleTask) string {
	if tag := strings.TrimSpace(task.Category); tag != "" {
		return tag
	}
	if tag := strings.TrimSpace(task.Name); tag != "" {
		return tag
	}
	return "General"
}

func normalizeMinContextGroupingKey(tag string) string {
	trimmed := strings.TrimSpace(tag)
	if trimmed == "" {
		return "General"
	}
	return trimmed
}

func isCoarseMinContextKey(key string) bool {
	switch strings.ToLower(strings.TrimSpace(key)) {
	case "", "general", "high-logic", "high_logic", "memory", "review":
		return true
	default:
		return false
	}
}

func inferMinContextKeyFromTaskName(name string) string {
	text := strings.ToLower(strings.TrimSpace(name))
	if text == "" {
		return ""
	}

	subjectKeywordGroups := []struct {
		keywords []string
		groupKey string
	}{
		{
			keywords: []string{
				"概率", "随机事件", "随机变量", "条件概率", "全概率", "贝叶斯",
				"分布", "大数定律", "中心极限定理", "参数估计", "期望", "方差", "协方差", "相关系数",
			},
			groupKey: "subject:probability",
		},
		{
			keywords: []string{
				"数制", "码制", "逻辑代数", "逻辑函数", "卡诺图", "译码器", "编码器",
				"数据选择器", "触发器", "时序电路", "状态图", "状态化简", "计数器", "寄存器", "数电",
			},
			groupKey: "subject:digital_logic",
		},
		{
			keywords: []string{
				"命题逻辑", "谓词逻辑", "量词", "等值演算", "集合", "关系", "函数",
				"图论", "欧拉回路", "哈密顿", "生成树", "离散", "组合数学", "容斥", "递推",
			},
			groupKey: "subject:discrete_math",
		},
	}
	for _, group := range subjectKeywordGroups {
		for _, keyword := range group.keywords {
			if strings.Contains(text, keyword) {
				return group.groupKey
			}
		}
	}
	return ""
}

func sortMinContextSnapshots(snapshotByID map[int]minContextSnapshot) []minContextSnapshot {
	items := make([]minContextSnapshot, 0, len(snapshotByID))
	for _, item := range snapshotByID {
		items = append(items, item)
	}
	sort.SliceStable(items, func(i, j int) bool {
		if items[i].Slot.Day != items[j].Slot.Day {
			return items[i].Slot.Day < items[j].Slot.Day
		}
		if items[i].Slot.SlotStart != items[j].Slot.SlotStart {
			return items[i].Slot.SlotStart < items[j].Slot.SlotStart
		}
		if items[i].Slot.SlotEnd != items[j].Slot.SlotEnd {
			return items[i].Slot.SlotEnd < items[j].Slot.SlotEnd
		}
		return items[i].StateID < items[j].StateID
	})
	return items
}

func countMinContextSwitches(ordered []minContextSnapshot) int {
	if len(ordered) < 2 {
		return 0
	}
	switches := 0
	prevTag := strings.TrimSpace(ordered[0].ContextTag)
	for i := 1; i < len(ordered); i++ {
		currentTag := strings.TrimSpace(ordered[i].ContextTag)
		if currentTag != prevTag {
			switches++
		}
		prevTag = currentTag
	}
	return switches
}

func sameTaskSlot(a, b TaskSlot) bool {
	return a.Day == b.Day && a.SlotStart == b.SlotStart && a.SlotEnd == b.SlotEnd
}
