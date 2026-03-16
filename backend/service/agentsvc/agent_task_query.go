package agentsvc

import (
	"context"
	"errors"
	"sort"
	"strings"
	"time"

	"github.com/LoveLosita/smartflow/backend/agent/taskquery"
	"github.com/LoveLosita/smartflow/backend/model"
	"github.com/LoveLosita/smartflow/backend/respond"
	"github.com/cloudwego/eino-ext/components/model/ark"
)

// runTaskQueryFlow 执行“任务查询”分支。
//
// 职责边界：
// 1. 负责把本次请求接入 taskquery 执行器；
// 2. 负责把 user_id 注入工具依赖，确保模型无法越权查他人任务；
// 3. 不负责聊天持久化（由 AgentChat 主流程统一收口）。
func (s *AgentService) runTaskQueryFlow(
	ctx context.Context,
	selectedModel *ark.ChatModel,
	userMessage string,
	userID int,
	emitStage func(stage, detail string),
) (string, error) {
	// 1. 依赖预检：任务查询必须依赖 taskRepo + model。
	if s == nil || s.taskRepo == nil {
		return "", errors.New("task query service dependency is not ready")
	}
	if selectedModel == nil {
		return "", errors.New("task query model is nil")
	}

	// 2. 构建执行输入并启动 tool-calling。
	// 2.1 RequestNow 仅用于 prompt 辅助，不参与数据库过滤。
	requestNow := time.Now().In(time.Local).Format("2006-01-02 15:04")
	return taskquery.RunTaskQueryGraph(ctx, taskquery.QueryGraphRunInput{
		Model:           selectedModel,
		UserMessage:     userMessage,
		RequestNowText:  requestNow,
		MaxReflectRetry: 2,
		EmitStage:       emitStage,
		Deps: taskquery.TaskQueryToolDeps{
			QueryTasks: func(ctx context.Context, req taskquery.TaskQueryRequest) ([]taskquery.TaskRecord, error) {
				// 2.2 调用目的：在工具层做完参数校验后，这里把 user_id 强制注入，再执行真实查询。
				//      这样可以保证模型永远只能查当前登录用户的数据。
				req.UserID = userID
				return s.queryTasksForAgent(ctx, req)
			},
		},
	})
}

// queryTasksForAgent 在 Agent 任务查询场景下读取并筛选任务。
//
// 职责边界：
// 1. 负责“读取原始任务 + 读时优先级派生 + 条件筛选 + 排序 + 截断”；
// 2. 不负责写库，不触发 outbox（只读查询链路）；
// 3. 返回的是工具层结构，不直接暴露 DAO 模型给上层。
func (s *AgentService) queryTasksForAgent(ctx context.Context, req taskquery.TaskQueryRequest) ([]taskquery.TaskRecord, error) {
	_ = ctx

	// 1. 基础参数校验。
	if req.UserID <= 0 {
		return nil, errors.New("invalid user_id in task query")
	}
	if s.taskRepo == nil {
		return nil, errors.New("task repository is nil")
	}

	// 2. 读取用户全部任务。
	// 2.1 当前 TaskDAO 读取接口无 context 参数，这里保持最小侵入复用既有能力；
	// 2.2 若用户任务为空，返回空切片而不是 error，方便模型自然回复“暂无任务”。
	tasks, err := s.taskRepo.GetTasksByUserID(req.UserID)
	if err != nil {
		if errors.Is(err, respond.UserTasksEmpty) {
			return make([]taskquery.TaskRecord, 0), nil
		}
		return nil, err
	}

	// 3. 读时派生 + 条件筛选：
	// 3.1 先按“紧急分界线”做内存派生，保证查询视图与主业务口径一致；
	// 3.2 再应用 include_completed/quadrant/keyword/deadline 条件。
	now := time.Now()
	filtered := make([]model.Task, 0, len(tasks))
	for _, originalTask := range tasks {
		currentTask := originalTask
		applyReadTimeUrgencyPromotion(&currentTask, now)
		if !taskMatchesQueryFilter(currentTask, req) {
			continue
		}
		filtered = append(filtered, currentTask)
	}

	// 4. 排序与截断：
	// 4.1 排序字段/方向已经在工具层校验过，这里按约定执行；
	// 4.2 limit 截断只发生在排序之后，保证“前 N 条”语义正确。
	sortTasksForQuery(filtered, req)
	if req.Limit > 0 && len(filtered) > req.Limit {
		filtered = filtered[:req.Limit]
	}

	// 5. 映射成工具输出结构。
	records := make([]taskquery.TaskRecord, 0, len(filtered))
	for _, task := range filtered {
		records = append(records, taskquery.TaskRecord{
			ID:                 task.ID,
			Title:              task.Title,
			PriorityGroup:      task.Priority,
			IsCompleted:        task.IsCompleted,
			DeadlineAt:         task.DeadlineAt,
			UrgencyThresholdAt: task.UrgencyThresholdAt,
		})
	}
	return records, nil
}

// applyReadTimeUrgencyPromotion 复用“读时紧急性派生”口径（内存态）。
//
// 规则：
// 1. 已完成任务不派生；
// 2. 未到紧急分界线不派生；
// 3. 到线后仅做 2->1、4->3 的象限平移；
// 4. 只改内存对象，不改数据库。
func applyReadTimeUrgencyPromotion(task *model.Task, now time.Time) {
	if task == nil {
		return
	}
	if task.IsCompleted || task.UrgencyThresholdAt == nil {
		return
	}
	if task.UrgencyThresholdAt.After(now) {
		return
	}

	switch task.Priority {
	case 2:
		task.Priority = 1
	case 4:
		task.Priority = 3
	}
}

// taskMatchesQueryFilter 判断任务是否满足查询条件。
func taskMatchesQueryFilter(task model.Task, req taskquery.TaskQueryRequest) bool {
	// 1. include_completed=false 时默认过滤掉已完成任务。
	if !req.IncludeCompleted && task.IsCompleted {
		return false
	}

	// 2. quadrant 过滤：只保留指定象限。
	if req.Quadrant != nil && task.Priority != *req.Quadrant {
		return false
	}

	// 3. keyword 过滤：对标题做大小写不敏感包含匹配。
	keyword := strings.TrimSpace(req.Keyword)
	if keyword != "" {
		if !strings.Contains(strings.ToLower(task.Title), strings.ToLower(keyword)) {
			return false
		}
	}

	// 4. deadline 区间过滤：
	// 4.1 只要设置了上下界，deadline_at 为空的任务默认不匹配；
	// 4.2 区间边界为闭区间（>= after 且 <= before）。
	if req.DeadlineAfter != nil {
		if task.DeadlineAt == nil || task.DeadlineAt.Before(*req.DeadlineAfter) {
			return false
		}
	}
	if req.DeadlineBefore != nil {
		if task.DeadlineAt == nil || task.DeadlineAt.After(*req.DeadlineBefore) {
			return false
		}
	}
	return true
}

// sortTasksForQuery 按查询条件排序任务。
//
// 排序策略：
// 1. sort_by=deadline：按截止时间排，deadline 为空的任务统一放末尾；
// 2. sort_by=priority：按象限数值排（1 最紧急），同优先级再按 id 倒序；
// 3. sort_by=id：按 id 排（可近似“新旧顺序”）。
func sortTasksForQuery(tasks []model.Task, req taskquery.TaskQueryRequest) {
	if len(tasks) <= 1 {
		return
	}

	order := strings.ToLower(strings.TrimSpace(req.Order))
	if order != "desc" {
		order = "asc"
	}
	sortBy := strings.ToLower(strings.TrimSpace(req.SortBy))
	if sortBy == "" {
		sortBy = "deadline"
	}

	sort.SliceStable(tasks, func(i, j int) bool {
		left := tasks[i]
		right := tasks[j]

		switch sortBy {
		case "priority":
			if left.Priority != right.Priority {
				if order == "desc" {
					return left.Priority > right.Priority
				}
				return left.Priority < right.Priority
			}
			// 同优先级时按 id 倒序，保证排序稳定且更接近“最近创建在前”。
			return left.ID > right.ID
		case "id":
			if order == "desc" {
				return left.ID > right.ID
			}
			return left.ID < right.ID
		default: // deadline
			if less, decided := compareDeadline(left.DeadlineAt, right.DeadlineAt, order); decided {
				return less
			}
			// 截止时间相同或都为空时，回退 id 倒序保证稳定性。
			return left.ID > right.ID
		}
	})
}

// compareDeadline 比较两个可选截止时间。
//
// 返回语义：
// 1. less：left 是否应排在 right 前；
// 2. decided：本次比较是否已能得出顺序；false 表示需要上层继续用次级键比较。
func compareDeadline(left, right *time.Time, order string) (less bool, decided bool) {
	// 1. 都为空：本次不决策，交给次级键。
	if left == nil && right == nil {
		return false, false
	}
	// 2. 只有一边为空：为空的一侧统一放末尾。
	if left == nil && right != nil {
		return false, true
	}
	if left != nil && right == nil {
		return true, true
	}

	// 3. 两边都不为空：按 order 做时间比较。
	if left.Equal(*right) {
		return false, false
	}
	if order == "desc" {
		return left.After(*right), true
	}
	return left.Before(*right), true
}
