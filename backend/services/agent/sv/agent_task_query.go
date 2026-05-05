package sv

import (
	"context"
	"errors"
	"sort"
	"strings"
	"time"

	"github.com/LoveLosita/smartflow/backend/model"
	"github.com/LoveLosita/smartflow/backend/respond"
	agentmodel "github.com/LoveLosita/smartflow/backend/services/agent/model"
)

func (s *AgentService) QueryTasksForTool(ctx context.Context, req agentmodel.TaskQueryRequest) ([]agentmodel.TaskQueryTaskRecord, error) {
	if req.UserID <= 0 {
		return nil, errors.New("invalid user_id in task query")
	}

	var tasks []model.Task
	var err error

	// 优先使用统一提升链路（含缓存读取 + 读时派生 + outbox 异步落库）。
	if s.GetTasksWithUrgencyPromotionFunc != nil {
		tasks, err = s.GetTasksWithUrgencyPromotionFunc(ctx, req.UserID)
		if err != nil {
			if errors.Is(err, respond.UserTasksEmpty) {
				return make([]agentmodel.TaskQueryTaskRecord, 0), nil
			}
			return nil, err
		}
	} else {
		// 回退：未注入时走旧的 taskRepo 直接读取（无缓存、无持久化）。
		if s.taskRepo == nil {
			return nil, errors.New("task repository is nil")
		}
		tasks, err = s.taskRepo.GetTasksByUserID(req.UserID)
		if err != nil {
			if errors.Is(err, respond.UserTasksEmpty) {
				return make([]agentmodel.TaskQueryTaskRecord, 0), nil
			}
			return nil, err
		}
		now := time.Now()
		for i := range tasks {
			applyReadTimeUrgencyPromotion(&tasks[i], now)
		}
	}

	// 过滤、排序、截断。
	filtered := make([]model.Task, 0, len(tasks))
	for _, task := range tasks {
		if !taskMatchesQueryFilter(task, req) {
			continue
		}
		filtered = append(filtered, task)
	}

	sortTasksForQuery(filtered, req)
	if req.Limit > 0 && len(filtered) > req.Limit {
		filtered = filtered[:req.Limit]
	}

	records := make([]agentmodel.TaskQueryTaskRecord, 0, len(filtered))
	for _, task := range filtered {
		records = append(records, agentmodel.TaskQueryTaskRecord{
			ID:                 task.ID,
			Title:              task.Title,
			PriorityGroup:      task.Priority,
			EstimatedSections:  model.NormalizeEstimatedSections(&task.EstimatedSections),
			IsCompleted:        task.IsCompleted,
			DeadlineAt:         task.DeadlineAt,
			UrgencyThresholdAt: task.UrgencyThresholdAt,
		})
	}
	return records, nil
}

func applyReadTimeUrgencyPromotion(task *model.Task, now time.Time) {
	if task == nil || task.IsCompleted || task.UrgencyThresholdAt == nil {
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

func taskMatchesQueryFilter(task model.Task, req agentmodel.TaskQueryRequest) bool {
	if !req.IncludeCompleted && task.IsCompleted {
		return false
	}
	if req.Quadrant != nil && task.Priority != *req.Quadrant {
		return false
	}
	keyword := strings.TrimSpace(req.Keyword)
	if keyword != "" && !strings.Contains(strings.ToLower(task.Title), strings.ToLower(keyword)) {
		return false
	}
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

func sortTasksForQuery(tasks []model.Task, req agentmodel.TaskQueryRequest) {
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
			return left.ID > right.ID
		case "id":
			if order == "desc" {
				return left.ID > right.ID
			}
			return left.ID < right.ID
		default:
			if less, decided := compareDeadline(left.DeadlineAt, right.DeadlineAt, order); decided {
				return less
			}
			return left.ID > right.ID
		}
	})
}

func compareDeadline(left, right *time.Time, order string) (less bool, decided bool) {
	if left == nil && right == nil {
		return false, false
	}
	if left == nil && right != nil {
		return false, true
	}
	if left != nil && right == nil {
		return true, true
	}
	if left.Equal(*right) {
		return false, false
	}
	if order == "desc" {
		return left.After(*right), true
	}
	return left.Before(*right), true
}
