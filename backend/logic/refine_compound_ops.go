package logic

import (
	"fmt"
	"sort"
	"strings"
)

// RefineTaskCandidate 表示复合工具规划阶段可移动的任务候选。
//
// 职责边界：
// 1. 只承载“任务当前坐标 + 规划所需标签”；
// 2. 不承载冲突判断、窗口判断等执行期逻辑；
// 3. 由调用方保证 task_item_id 唯一且为正数。
type RefineTaskCandidate struct {
	TaskItemID  int
	Week        int
	DayOfWeek   int
	SectionFrom int
	SectionTo   int
	Name        string
	ContextTag  string
	OriginRank  int
}

// RefineSlotCandidate 表示复合工具可选落点（坑位）。
//
// 职责边界：
// 1. 只描述可候选的时段坐标；
// 2. 不描述“为什么可用”，可用性由调用方预先筛好；
// 3. Span 由 SectionFrom/SectionTo 推导，不单独存储。
type RefineSlotCandidate struct {
	Week        int
	DayOfWeek   int
	SectionFrom int
	SectionTo   int
}

// RefineMovePlanItem 表示“任务 -> 目标坑位”的确定性规划结果。
type RefineMovePlanItem struct {
	TaskItemID    int
	ToWeek        int
	ToDay         int
	ToSectionFrom int
	ToSectionTo   int
}

// RefineCompositePlanOptions 是复合规划器的可选辅助输入。
//
// 说明：
// 1. ExistingDayLoad 用于提供“目标范围内的既有负载基线”，用于均匀铺开打分；
// 2. key 约定为 "week-day"，例如 "16-3"；
// 3. 未提供时，规划器按 0 基线处理。
type RefineCompositePlanOptions struct {
	ExistingDayLoad map[string]int
}

// PlanEvenSpreadMoves 规划“均匀铺开”的确定性移动方案。
//
// 步骤化说明：
// 1. 先按稳定顺序归一化任务与坑位，保证同输入必同输出；
// 2. 逐任务选择“投放后日负载最小”的坑位，主目标是降低日负载离散度；
// 3. 同分时按时间更早优先，进一步保证确定性；
// 4. 若某任务不存在同跨度坑位，直接失败并返回明确错误。
func PlanEvenSpreadMoves(tasks []RefineTaskCandidate, slots []RefineSlotCandidate, options RefineCompositePlanOptions) ([]RefineMovePlanItem, error) {
	normalizedTasks, err := normalizeRefineTasks(tasks)
	if err != nil {
		return nil, err
	}
	normalizedSlots, err := normalizeRefineSlots(slots)
	if err != nil {
		return nil, err
	}
	if len(normalizedSlots) < len(normalizedTasks) {
		return nil, fmt.Errorf("可用坑位不足：tasks=%d, slots=%d", len(normalizedTasks), len(normalizedSlots))
	}

	// 1. dayLoad 记录“当前已占 + 本次规划已分配”的日负载。
	// 2. 这里先写入调用方提供的既有基线，再在循环中动态递增。
	dayLoad := make(map[string]int, len(options.ExistingDayLoad)+len(normalizedSlots))
	for key, value := range options.ExistingDayLoad {
		if value <= 0 {
			continue
		}
		dayLoad[strings.TrimSpace(key)] = value
	}

	used := make([]bool, len(normalizedSlots))
	moves := make([]RefineMovePlanItem, 0, len(normalizedTasks))
	selectedSlots := make([]RefineSlotCandidate, 0, len(normalizedTasks))

	for _, task := range normalizedTasks {
		taskSpan := sectionSpan(task.SectionFrom, task.SectionTo)
		bestIdx := -1
		bestScore := int(^uint(0) >> 1) // max int

		for idx, slot := range normalizedSlots {
			if used[idx] {
				continue
			}
			if sectionSpan(slot.SectionFrom, slot.SectionTo) != taskSpan {
				continue
			}
			if slotOverlapsAny(slot, selectedSlots) {
				continue
			}
			dayKey := composeDayKey(slot.Week, slot.DayOfWeek)
			projectedLoad := dayLoad[dayKey] + 1
			// 1. projectedLoad 是主目标（越小越均衡）；
			// 2. idx 是次级目标（越早的坑位越优先，保证稳定）。
			score := projectedLoad*10000 + idx
			if score < bestScore {
				bestScore = score
				bestIdx = idx
			}
		}
		if bestIdx < 0 {
			return nil, fmt.Errorf("任务 id=%d 无可用同跨度坑位", task.TaskItemID)
		}

		chosen := normalizedSlots[bestIdx]
		used[bestIdx] = true
		selectedSlots = append(selectedSlots, chosen)
		dayLoad[composeDayKey(chosen.Week, chosen.DayOfWeek)]++
		moves = append(moves, RefineMovePlanItem{
			TaskItemID:    task.TaskItemID,
			ToWeek:        chosen.Week,
			ToDay:         chosen.DayOfWeek,
			ToSectionFrom: chosen.SectionFrom,
			ToSectionTo:   chosen.SectionTo,
		})
	}
	return moves, nil
}

// PlanMinContextSwitchMoves 规划“同科目上下文切换最少”的确定性移动方案。
//
// 步骤化说明：
// 1. 先把任务按 context_tag 分组，目标是让同组任务尽量连续；
// 2. 分组顺序按“组大小降序 + 最早 origin_rank + 标签字典序”稳定排序；
// 3. 组内按任务稳定顺序排，再顺序填入时间上最早可用同跨度坑位；
// 4. 若某任务不存在同跨度坑位，立即失败并返回明确错误。
func PlanMinContextSwitchMoves(tasks []RefineTaskCandidate, slots []RefineSlotCandidate, _ RefineCompositePlanOptions) ([]RefineMovePlanItem, error) {
	normalizedTasks, err := normalizeRefineTasks(tasks)
	if err != nil {
		return nil, err
	}
	normalizedSlots, err := normalizeRefineSlots(slots)
	if err != nil {
		return nil, err
	}
	if len(normalizedSlots) < len(normalizedTasks) {
		return nil, fmt.Errorf("可用坑位不足：tasks=%d, slots=%d", len(normalizedTasks), len(normalizedSlots))
	}

	type taskGroup struct {
		ContextKey string
		Tasks      []RefineTaskCandidate
		MinRank    int
	}
	groupMap := make(map[string]*taskGroup)
	groupOrder := make([]string, 0, len(normalizedTasks))

	for _, task := range normalizedTasks {
		key := normalizeContextKey(task.ContextTag)
		group, exists := groupMap[key]
		if !exists {
			group = &taskGroup{
				ContextKey: key,
				MinRank:    normalizedOriginRank(task),
			}
			groupMap[key] = group
			groupOrder = append(groupOrder, key)
		}
		group.Tasks = append(group.Tasks, task)
		if rank := normalizedOriginRank(task); rank < group.MinRank {
			group.MinRank = rank
		}
	}

	groups := make([]taskGroup, 0, len(groupMap))
	for _, key := range groupOrder {
		group := groupMap[key]
		sort.SliceStable(group.Tasks, func(i, j int) bool {
			return compareTaskOrder(group.Tasks[i], group.Tasks[j]) < 0
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
		return groups[i].ContextKey < groups[j].ContextKey
	})

	orderedTasks := make([]RefineTaskCandidate, 0, len(normalizedTasks))
	for _, group := range groups {
		orderedTasks = append(orderedTasks, group.Tasks...)
	}

	used := make([]bool, len(normalizedSlots))
	moves := make([]RefineMovePlanItem, 0, len(orderedTasks))
	selectedSlots := make([]RefineSlotCandidate, 0, len(orderedTasks))
	for _, task := range orderedTasks {
		taskSpan := sectionSpan(task.SectionFrom, task.SectionTo)
		chosenIdx := -1
		for idx, slot := range normalizedSlots {
			if used[idx] {
				continue
			}
			if sectionSpan(slot.SectionFrom, slot.SectionTo) != taskSpan {
				continue
			}
			if slotOverlapsAny(slot, selectedSlots) {
				continue
			}
			chosenIdx = idx
			break
		}
		if chosenIdx < 0 {
			return nil, fmt.Errorf("任务 id=%d 无可用同跨度坑位", task.TaskItemID)
		}
		chosen := normalizedSlots[chosenIdx]
		used[chosenIdx] = true
		selectedSlots = append(selectedSlots, chosen)
		moves = append(moves, RefineMovePlanItem{
			TaskItemID:    task.TaskItemID,
			ToWeek:        chosen.Week,
			ToDay:         chosen.DayOfWeek,
			ToSectionFrom: chosen.SectionFrom,
			ToSectionTo:   chosen.SectionTo,
		})
	}
	return moves, nil
}

func normalizeRefineTasks(tasks []RefineTaskCandidate) ([]RefineTaskCandidate, error) {
	if len(tasks) == 0 {
		return nil, fmt.Errorf("任务列表为空")
	}
	normalized := make([]RefineTaskCandidate, 0, len(tasks))
	seen := make(map[int]struct{}, len(tasks))
	for _, task := range tasks {
		if task.TaskItemID <= 0 {
			return nil, fmt.Errorf("存在非法 task_item_id=%d", task.TaskItemID)
		}
		if _, exists := seen[task.TaskItemID]; exists {
			return nil, fmt.Errorf("任务 id=%d 重复", task.TaskItemID)
		}
		if !isValidDay(task.DayOfWeek) {
			return nil, fmt.Errorf("任务 id=%d day_of_week 非法=%d", task.TaskItemID, task.DayOfWeek)
		}
		if !isValidSection(task.SectionFrom, task.SectionTo) {
			return nil, fmt.Errorf("任务 id=%d 节次区间非法=%d-%d", task.TaskItemID, task.SectionFrom, task.SectionTo)
		}
		seen[task.TaskItemID] = struct{}{}
		normalized = append(normalized, task)
	}
	sort.SliceStable(normalized, func(i, j int) bool {
		return compareTaskOrder(normalized[i], normalized[j]) < 0
	})
	return normalized, nil
}

func normalizeRefineSlots(slots []RefineSlotCandidate) ([]RefineSlotCandidate, error) {
	if len(slots) == 0 {
		return nil, fmt.Errorf("可用坑位为空")
	}
	normalized := make([]RefineSlotCandidate, 0, len(slots))
	seen := make(map[string]struct{}, len(slots))
	for _, slot := range slots {
		if slot.Week <= 0 {
			return nil, fmt.Errorf("存在非法 week=%d", slot.Week)
		}
		if !isValidDay(slot.DayOfWeek) {
			return nil, fmt.Errorf("存在非法 day_of_week=%d", slot.DayOfWeek)
		}
		if !isValidSection(slot.SectionFrom, slot.SectionTo) {
			return nil, fmt.Errorf("存在非法节次区间=%d-%d", slot.SectionFrom, slot.SectionTo)
		}
		key := fmt.Sprintf("%d-%d-%d-%d", slot.Week, slot.DayOfWeek, slot.SectionFrom, slot.SectionTo)
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		normalized = append(normalized, slot)
	}
	sort.SliceStable(normalized, func(i, j int) bool {
		if normalized[i].Week != normalized[j].Week {
			return normalized[i].Week < normalized[j].Week
		}
		if normalized[i].DayOfWeek != normalized[j].DayOfWeek {
			return normalized[i].DayOfWeek < normalized[j].DayOfWeek
		}
		if normalized[i].SectionFrom != normalized[j].SectionFrom {
			return normalized[i].SectionFrom < normalized[j].SectionFrom
		}
		return normalized[i].SectionTo < normalized[j].SectionTo
	})
	return normalized, nil
}

func compareTaskOrder(a, b RefineTaskCandidate) int {
	rankA := normalizedOriginRank(a)
	rankB := normalizedOriginRank(b)
	if rankA != rankB {
		return rankA - rankB
	}
	if a.Week != b.Week {
		return a.Week - b.Week
	}
	if a.DayOfWeek != b.DayOfWeek {
		return a.DayOfWeek - b.DayOfWeek
	}
	if a.SectionFrom != b.SectionFrom {
		return a.SectionFrom - b.SectionFrom
	}
	if a.SectionTo != b.SectionTo {
		return a.SectionTo - b.SectionTo
	}
	return a.TaskItemID - b.TaskItemID
}

func normalizedOriginRank(task RefineTaskCandidate) int {
	if task.OriginRank > 0 {
		return task.OriginRank
	}
	// 1. 无 origin_rank 时回退到较大稳定值，避免把“未知顺序”抢到前面。
	// 2. 叠加 task_id 作为细粒度稳定因子，保证排序可复现。
	return 1_000_000 + task.TaskItemID
}

func normalizeContextKey(tag string) string {
	text := strings.TrimSpace(tag)
	if text == "" {
		return "General"
	}
	return text
}

func composeDayKey(week, day int) string {
	return fmt.Sprintf("%d-%d", week, day)
}

func sectionSpan(from, to int) int {
	return to - from + 1
}

func isValidDay(day int) bool {
	return day >= 1 && day <= 7
}

func isValidSection(from, to int) bool {
	if from < 1 || to > 12 {
		return false
	}
	return from <= to
}

func slotOverlapsAny(candidate RefineSlotCandidate, selected []RefineSlotCandidate) bool {
	for _, current := range selected {
		if current.Week != candidate.Week || current.DayOfWeek != candidate.DayOfWeek {
			continue
		}
		if current.SectionFrom <= candidate.SectionTo && candidate.SectionFrom <= current.SectionTo {
			return true
		}
	}
	return false
}
