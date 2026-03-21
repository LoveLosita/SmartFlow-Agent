package logic

import (
	"fmt"

	"github.com/LoveLosita/smartflow/backend/conv"
	"github.com/LoveLosita/smartflow/backend/model"
	"github.com/LoveLosita/smartflow/backend/respond"
)

type slotStatus int

const (
	Free     slotStatus = iota // 0: 纯空闲
	Occupied                   // 1: 已有课/任务，不可动
	Blocked                    // 2: 用户屏蔽时段
	Filler                     // 3: 水课，允许嵌入
)

type slotNode struct {
	Status  slotStatus
	EventID uint // 🚀 关键：记录课程 ID，用于识别水课边界
}

type grid struct {
	data      map[int]map[int][13]slotNode
	startWeek int
	startDay  int
	endWeek   int
	endDay    int
}

// getNode 和 setNode 是对 grid 数据结构的封装，确保我们在访问时能正确处理默认值（Free）和边界情况
func (g *grid) getNode(w, d, s int) slotNode {
	if dayMap, ok := g.data[w]; ok {
		return dayMap[d][s]
	}
	return slotNode{Status: Free, EventID: 0}
}

func (g *grid) setNode(w, d, s int, node slotNode) {
	if _, ok := g.data[w]; !ok {
		g.data[w] = make(map[int][13]slotNode)
	}
	dayData := g.data[w][d]
	dayData[s] = node
	g.data[w][d] = dayData
}

// 检查是否可用 (Free 或 Filler 且不在 Blocked 时段内)
func (g *grid) isAvailable(w, d, s int) bool {
	node := g.getNode(w, d, s)
	return node.Status == Free || node.Status == Filler
}

// countAvailableSlots 统计指定周次范围内所有可用的原子节次总数
func (g *grid) countAvailableSlots(currW, currD, currS int) int {
	count := 0
	if currW == 0 && currD == 0 && currS == 0 {
		currW, currD, currS = g.startWeek, g.startDay, 1
	}
	for w := currW; w <= g.endWeek; w++ {
		dayMap, hasData := g.data[w]
		for d := 1; d <= 7; d++ {
			// 🚀 头部裁剪：过滤开始日期前的天数
			if w == currW && d < currD {
				continue
			}
			// 🚀 尾部裁剪：过滤结束日期后的天数
			if w == g.endWeek && d > g.endDay {
				break
			}
			var dayData [13]slotNode
			if hasData {
				dayData = dayMap[d]
			}
			for s := 1; s <= 12; s++ {
				if w == currW && d == currD && s < currS {
					continue
				}
				if dayData[s].Status == Free || dayData[s].Status == Filler {
					count++
				}
			}
		}
	}
	return count
}

// FindNextAvailable 从当前时间点开始，按周、天、节次顺序查找下一个可用格子
func (g *grid) FindNextAvailable(currW, currD, currS int) (int, int, int) {
	// 基础越界检查
	if currW > g.endWeek || (currW == g.endWeek && currD > g.endDay) {
		return -1, -1, -1
	}

	for w := currW; w <= g.endWeek; w++ {
		dayMap, hasData := g.data[w]
		for d := 1; d <= 7; d++ {
			if w == currW && d < currD {
				continue
			}
			if w == g.endWeek && d > g.endDay {
				break
			} // 🚀 守住结束天

			var dayData [13]slotNode
			if hasData {
				dayData = dayMap[d]
			}

			for s := 1; s <= 12; s++ {
				if w == currW && d == currD && s < currS {
					continue
				}
				if w == g.endWeek && d == g.endDay {
					break
				} // 🚀 守住结束节

				if dayData[s].Status == Free || dayData[s].Status == Filler {
					return w, d, s
				}
			}
		}
	}
	return -1, -1, -1
}

// 辅助函数：向后跳过指定数量的可用坑位
func (g *grid) skipAvailableSlots(w, d, s, skipCount int) (int, int, int) {
	if skipCount <= 0 {
		// 即使 gap 为 0，也要至少移到下一节
		s++
		if s > 12 {
			s = 1
			d++
			if d > 7 {
				d = 1
				w++
			}
		}
		return w, d, s
	}
	found := 0
	currW, currD, currS := w, d, s+1
	for currW <= g.endWeek {
		if currS > 12 {
			currS = 1
			currD++
			if currD > 7 {
				currD = 1
				currW++
			}
			continue
		}
		// 如果已经跳到了最后一天，不要再跳了，直接返回终点坐标
		if currW == g.endWeek && currD > g.endDay {
			return g.endWeek, g.endDay, 12
		}
		if g.isAvailable(currW, currD, currS) {
			found++
			if found > skipCount {
				return currW, currD, currS
			}
		}
		currS++
	}
	return currW, currD, currS
}

func SmartPlanningMainLogic(schedules []model.Schedule, taskClass *model.TaskClass) ([]model.UserWeekSchedule, error) {
	//1.先构建时间格子
	g := buildTimeGrid(schedules, taskClass)
	//2.根据时间格子和排课策略计算每个任务块的具体安排时间
	allocatedItems, err := computeAllocation(g, taskClass.Items, *taskClass.Strategy)
	if err != nil {
		return nil, err
	}
	//3.把这些时间通过DTO函数回填到涉��周的 UserWeekSchedule 结构中，供前端展示
	return conv.PlanningResultToUserWeekSchedules(schedules, allocatedItems), nil
}

// SmartPlanningRawItems 执行粗排算法并直接返回已分配的任务项列表。
//
// 与 SmartPlanningMainLogic 共享完全相同的构建网格和分配逻辑，
// 但不做展示格式转换，直接返回 allocatedItems（每项的 EmbeddedTime 已回填）。
// 供 Agent 排程链路使用，避免从展示结构反向解析导致信息丢失。
func SmartPlanningRawItems(schedules []model.Schedule, taskClass *model.TaskClass) ([]model.TaskClassItem, error) {
	g := buildTimeGrid(schedules, taskClass)
	return computeAllocation(g, taskClass.Items, *taskClass.Strategy)
}

// SmartPlanningRawItemsMulti 执行“多任务类共享资源池”粗排。
//
// 职责边界：
// 1. 复用现有 SmartPlanningRawItems 的单任务类分配能力，不重写核心算法；
// 2. 通过“增量占位”把前一个任务类的建议结果写入共享工作日程，供后续任务类避让；
// 3. 返回聚合后的 allocatedItems（每项 EmbeddedTime 已回填）；
// 4. 不负责展示结构转换（由 service/conv 层处理）。
func SmartPlanningRawItemsMulti(schedules []model.Schedule, taskClasses []*model.TaskClass) ([]model.TaskClassItem, error) {
	if len(taskClasses) == 0 {
		return []model.TaskClassItem{}, nil
	}

	// 1. 构建“工作副本”：
	// 1.1 原始 schedules 不直接修改，避免污染调用方数据；
	// 1.2 后续每完成一个任务类分配，就把结果增量写入 workingSchedules。
	workingSchedules := cloneSchedulesForPlanning(schedules)
	allAllocated := make([]model.TaskClassItem, 0)

	// 2. syntheticEventID 用于给“虚拟占位任务”分配唯一 EventID。
	// 2.1 采用负数区间，避免和数据库自增正数 EventID 冲突；
	// 2.2 每个任务块占用一个 synthetic event，跨节次共享同一 eventID。
	nextSyntheticEventID := -1

	for _, taskClass := range taskClasses {
		if taskClass == nil {
			continue
		}
		if taskClass.Strategy == nil {
			return nil, fmt.Errorf("task_class_id=%d 缺少 strategy 配置", taskClass.ID)
		}

		// 3. 复用单任务类粗排。
		allocatedItems, err := SmartPlanningRawItems(workingSchedules, taskClass)
		if err != nil {
			// 3.1 明确标注失败任务类，便于上层快速定位。
			return nil, fmt.Errorf("task_class_id=%d 粗排失败: %w", taskClass.ID, err)
		}
		allAllocated = append(allAllocated, allocatedItems...)

		// 4. 把本任务类分配结果转成“虚拟 Schedule 占位”追加回工作副本。
		// 4.1 目的：让后续任务类把这些已分配任务当成 Occupied，避免重叠；
		// 4.2 若某任务块没有 EmbeddedTime，直接跳过，不阻断后续。
		virtualSchedules, nextID := buildVirtualSchedulesFromAllocated(allocatedItems, taskClass, nextSyntheticEventID)
		nextSyntheticEventID = nextID
		if len(virtualSchedules) > 0 {
			workingSchedules = append(workingSchedules, virtualSchedules...)
		}
	}

	return allAllocated, nil
}

// cloneSchedulesForPlanning 深拷贝 schedules，确保后续在算法中安全修改。
//
// 说明：
// 1. 主要拷贝 Schedule 结构体本身；
// 2. Event 指针做浅字段复制，避免共享同一 Event 指针导致意外改写；
// 3. EmbeddedTask 在粗排阶段不参与状态写入，保留原值即可。
func cloneSchedulesForPlanning(src []model.Schedule) []model.Schedule {
	if len(src) == 0 {
		return []model.Schedule{}
	}
	dst := make([]model.Schedule, len(src))
	for i := range src {
		dst[i] = src[i]
		if src[i].Event != nil {
			eventCopy := *src[i].Event
			dst[i].Event = &eventCopy
		}
	}
	return dst
}

// buildVirtualSchedulesFromAllocated 将已分配任务块转成“虚拟占位 schedules”。
//
// 设计目的：
// 1. 让后续任务类在共享资源池里自动避让已分配任务；
// 2. 不落库，仅用于内存中的粗排冲突控制；
// 3. 通过 Type=task + CanBeEmbedded=false 强制标记为不可再嵌入。
func buildVirtualSchedulesFromAllocated(allocatedItems []model.TaskClassItem, taskClass *model.TaskClass, eventIDStart int) ([]model.Schedule, int) {
	if len(allocatedItems) == 0 {
		return []model.Schedule{}, eventIDStart
	}

	userID := 0
	if taskClass != nil && taskClass.UserID != nil {
		userID = *taskClass.UserID
	}

	virtual := make([]model.Schedule, 0)
	nextEventID := eventIDStart
	for _, item := range allocatedItems {
		if item.EmbeddedTime == nil {
			continue
		}

		taskName := "未命名任务"
		if item.Content != nil && *item.Content != "" {
			taskName = *item.Content
		}
		location := ""
		event := &model.ScheduleEvent{
			ID:            nextEventID,
			UserID:        userID,
			Name:          taskName,
			Location:      &location,
			Type:          "task",
			CanBeEmbedded: false,
		}
		for section := item.EmbeddedTime.SectionFrom; section <= item.EmbeddedTime.SectionTo; section++ {
			virtual = append(virtual, model.Schedule{
				EventID:   nextEventID,
				UserID:    userID,
				Week:      item.EmbeddedTime.Week,
				DayOfWeek: item.EmbeddedTime.DayOfWeek,
				Section:   section,
				Event:     event,
				Status:    "normal",
			})
		}
		nextEventID--
	}
	return virtual, nextEventID
}

// buildTimeGrid 构建一个时间格子，标记出哪些时间段被占用、哪些被屏蔽、哪些是水课

func buildTimeGrid(schedules []model.Schedule, taskClass *model.TaskClass) *grid {
	// 🚀 核心修正：获取精确的起始坐标
	startW, startD, _ := conv.RealDateToRelativeDate(taskClass.StartDate.Format(conv.DateFormat))
	endW, endD, _ := conv.RealDateToRelativeDate(taskClass.EndDate.Format(conv.DateFormat))
	// 将信息初始化到 grid 结构中
	g := &grid{
		data:      make(map[int]map[int][13]slotNode),
		startWeek: startW,
		startDay:  startD,
		endWeek:   endW,
		endDay:    endD,
	}
	//标记屏蔽时段 (Blocked)
	for _, blockIdx := range taskClass.ExcludedSlots {
		sFrom, sTo := (blockIdx-1)*2+1, blockIdx*2
		for w := startW; w <= endW; w++ {
			for d := 1; d <= 7; d++ { //🚀 注意：这里的屏蔽是针对每天的，所以直接循环 1-7 天
				for s := sFrom; s <= sTo; s++ {
					g.setNode(w, d, s, slotNode{Status: Blocked})
				}
			}
		}
	}

	// 映射日程 (尊重 Blocked 且只处理范围内的数据)
	for _, s := range schedules {
		if s.Week >= startW && s.Week <= endW {
			if g.getNode(s.Week, s.DayOfWeek, s.Section).Status == Blocked {
				continue
			}
			status := Occupied
			// 只有当课程允许嵌入且当前事件支持嵌入时，才标记为 Filler
			if *taskClass.AllowFillerCourse && s.Event.CanBeEmbedded {
				status = Filler
			}
			g.setNode(s.Week, s.DayOfWeek, s.Section, slotNode{Status: status, EventID: uint(s.EventID)})
		}
	}
	return g
}

// computeAllocation 是核心函数，负责根据当前的时间格子状态和排课策略，计算出每个任务块的具体安排时间
/*func computeAllocation(g *grid, items []model.TaskClassItem, strategy string) ([]model.TaskClassItem, error) {
	if len(items) == 0 {
		return items, nil
	}

	// 🚀 核心修正 1：获取真正的开始坐标（周、天、节）
	// 这里假设你已经通过 conv 把 StartDate 换成了 w1, d1, s1
	startW := g.startWeek
	startD := g.startDay // 建议从 conv 传入具体的 DayOfWeek
	startS := 1

	// 1. 获取可用资源总量
	totalAvailable := g.countAvailableSlots(0, 0, 0)
	// 假设每个任务块至少占用 2 个原子槽位
	totalRequired := len(items) * 2

	// 🚀 核心改进：容量预判
	if totalAvailable < totalRequired {
		// 如果连最基本的坑位都不够，直接报错，不进行任何编排
		return nil, respond.TimeNotEnoughForAutoScheduling
	}

	// 🚀 核心修正 2：步长改为“逻辑间隔”，不再是物理跳跃
	// gap 表示：每两个任务之间，我们要故意空出多少个“可用位”
	gap := 0
	if strategy == "steady" && totalAvailable > totalRequired {
		gap = (totalAvailable - totalRequired) / (len(items) + 1)
	}

	currW, currD, currS := startW, startD, startS
	lastPlacedIndex := -1
	for i := range items {
		w, d, s := g.FindNextAvailable(currW, currD, currS)
		if w == -1 || w > g.endWeek {
			break
		}

		node := g.getNode(w, d, s)
		slotLen := 2
		if node.Status == Filler {
			slotLen = 1
			currID := node.EventID
			for checkS := s + 1; checkS <= 12; checkS++ {
				if next := g.getNode(w, d, checkS); next.Status == Filler && next.EventID == currID {
					slotLen++
				} else {
					break
				}
			}
		}

		endS := s + slotLen - 1
		items[i].EmbeddedTime = &model.TargetTime{
			SectionFrom: s, SectionTo: endS,
			Week: w, DayOfWeek: d,
		}

		for sec := s; sec <= endS; sec++ {
			g.setNode(w, d, sec, slotNode{Status: Occupied})
		}

		// 🚀 核心修正 3：基于“可用位”推进指针，而非物理索引
		// 我们要在 grid 中向后数出 gap 个可用位置，作为下一个任务的起点
		currW, currD, currS = g.skipAvailableSlots(w, d, endS, gap)

		lastPlacedIndex = i // 记录最后一个成功安放的任务索引
	}
	// 🚀 核心改进：结果完整性校验
	if lastPlacedIndex < len(items)-1 {
				return nil, fmt.Errorf("排程中断：由于时间片碎片化，仅成功安排了 %d/%d 个任务块，请尝试扩充时间范围或删减屏蔽位", lastPlacedIndex+1, len(items))
		return nil, respond.TimeNotEnoughForAutoScheduling
	}

	return items, nil
}*/

type slotCoord struct {
	w, d, s int
}

// getAllAvailable 获取窗口内所有可用的原子节次坐标（逻辑一维化）
func (g *grid) getAllAvailable() []slotCoord {
	var coords []slotCoord
	for w := g.startWeek; w <= g.endWeek; w++ {
		dayMap, hasData := g.data[w]
		for d := 1; d <= 7; d++ {
			// 边界裁剪逻辑
			if w == g.startWeek && d < g.startDay {
				continue
			}
			if w == g.endWeek && d > g.endDay {
				break
			}

			var dayData [13]slotNode
			if hasData {
				dayData = dayMap[d]
			}

			for s := 1; s <= 12; s++ {
				// 顺着你的逻辑，不限开始节次，但需注意状态判定
				if dayData[s].Status == Free || dayData[s].Status == Filler {
					coords = append(coords, slotCoord{w, d, s})
				}
			}
		}
	}
	return coords
}

func computeAllocation(g *grid, items []model.TaskClassItem, strategy string) ([]model.TaskClassItem, error) {
	if len(items) == 0 {
		return items, nil
	}

	// 1. 预处理：提取所有可用坑位
	coords := g.getAllAvailable()
	totalAvailable := len(coords)
	totalRequired := len(items) * 2 // 基础需求：每个任务 2 节

	if totalAvailable < totalRequired {
		return nil, respond.TimeNotEnoughForAutoScheduling
	}

	// 2. 计算精准步长
	gap := 0
	if strategy == "steady" {
		gap = (totalAvailable - totalRequired) / (len(items) + 1)
	}

	// 3. 线性映射分配
	// cursor 是我们在逻辑切片中的“指针”
	cursor := gap
	lastPlacedIndex := -1

	for i := range items {
		if cursor >= totalAvailable {
			break
		}

		// 获取当前逻辑位置对应的物理坐标
		startLoc := coords[cursor]
		w, d, s := startLoc.w, startLoc.d, startLoc.s

		// 4. 计算本次任务块的落点区间。
		// 4.1 默认按 2 节处理（普通空闲位优先遵循“每任务2节”的主策略）；
		// 4.2 命中 Filler（可嵌入课程）时，必须先回溯到同课程块起点，再计算完整连续跨度；
		// 4.3 失败兜底：若普通空闲位后继不可用，只能退化为 1 节，避免越界或覆盖占用位。
		node := g.getNode(w, d, s)
		sectionFrom := s
		slotLen := 2
		if node.Status == Filler {
			// 4.2.1 先向左回溯到“同一课程块”的起点。
			// 目的：修复“指针落在课程中间节次时被错误切成 1 节”的问题。
			// 例如课程占 9-10 节，若 cursor 命中 10 节，必须回溯到 9 节再整体计算。
			currID := node.EventID
			for checkS := s - 1; checkS >= 1; checkS-- {
				prev := g.getNode(w, d, checkS)
				if prev.Status == Filler && prev.EventID == currID {
					sectionFrom = checkS
					continue
				}
				break
			}

			// 4.2.2 再从起点向右扩展，拿到同一课程块的完整连续节次长度。
			sectionTo := sectionFrom
			for checkS := sectionFrom + 1; checkS <= 12; checkS++ {
				if next := g.getNode(w, d, checkS); next.Status == Filler && next.EventID == currID {
					sectionTo = checkS
				} else {
					break
				}
			}
			slotLen = sectionTo - sectionFrom + 1
		} else if s == 12 || !g.isAvailable(w, d, s+1) {
			// 如果是 Free 区域，但下一节不可用，则被迫设为 1 节
			slotLen = 1
		}

		// 回填时间
		endS := sectionFrom + slotLen - 1
		items[i].EmbeddedTime = &model.TargetTime{
			SectionFrom: sectionFrom, SectionTo: endS,
			Week: w, DayOfWeek: d,
		}

		// 标记占用 (物理网格)
		for sec := sectionFrom; sec <= endS; sec++ {
			g.setNode(w, d, sec, slotNode{Status: Occupied})
		}

		// 🚀 核心进步：逻辑跳跃
		// 既然任务占用了 slotLen 节，我们在逻辑切片中也向后推 slotLen 个位置，再加 gap
		cursor += slotLen + gap
		lastPlacedIndex = i
	}

	if lastPlacedIndex < len(items)-1 {
		return nil, respond.TimeNotEnoughForAutoScheduling
	}

	return items, nil
}
