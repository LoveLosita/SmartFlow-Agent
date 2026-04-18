package agentsvc

import (
	"context"
	"errors"
	"sort"
	"strings"
	"time"

	agentgraph "github.com/LoveLosita/smartflow/backend/agent/graph"
	agentmodel "github.com/LoveLosita/smartflow/backend/agent/model"
	agentnode "github.com/LoveLosita/smartflow/backend/agent/node"
	"github.com/LoveLosita/smartflow/backend/model"
	"github.com/LoveLosita/smartflow/backend/respond"
	"github.com/cloudwego/eino-ext/components/model/ark"
)

func (s *AgentService) runTaskQueryFlow(
	ctx context.Context,
	selectedModel *ark.ChatModel,
	userMessage string,
	userID int,
	emitStage func(stage, detail string),
) (string, error) {
	if s == nil || s.taskRepo == nil {
		return "", errors.New("task query service dependency is not ready")
	}
	if selectedModel == nil {
		return "", errors.New("task query model is nil")
	}

	requestNow := time.Now().In(time.Local).Format("2006-01-02 15:04")
	state := agentmodel.NewTaskQueryState(strings.TrimSpace(userMessage), requestNow, agentmodel.DefaultTaskQueryReflectRetry)
	return agentgraph.RunTaskQueryGraph(ctx, agentnode.TaskQueryGraphRunInput{
		Model:     selectedModel,
		State:     state,
		EmitStage: emitStage,
		Deps: agentnode.TaskQueryToolDeps{
			QueryTasks: func(ctx context.Context, req agentnode.TaskQueryRequest) ([]agentnode.TaskQueryTaskRecord, error) {
				req.UserID = userID
				return s.QueryTasksForTool(ctx, req)
			},
		},
	})
}

func (s *AgentService) QueryTasksForTool(ctx context.Context, req agentnode.TaskQueryRequest) ([]agentnode.TaskQueryTaskRecord, error) {
	_ = ctx
	if req.UserID <= 0 {
		return nil, errors.New("invalid user_id in task query")
	}
	if s.taskRepo == nil {
		return nil, errors.New("task repository is nil")
	}

	tasks, err := s.taskRepo.GetTasksByUserID(req.UserID)
	if err != nil {
		if errors.Is(err, respond.UserTasksEmpty) {
			return make([]agentnode.TaskQueryTaskRecord, 0), nil
		}
		return nil, err
	}

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

	sortTasksForQuery(filtered, req)
	if req.Limit > 0 && len(filtered) > req.Limit {
		filtered = filtered[:req.Limit]
	}

	records := make([]agentnode.TaskQueryTaskRecord, 0, len(filtered))
	for _, task := range filtered {
		records = append(records, agentnode.TaskQueryTaskRecord{
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

func taskMatchesQueryFilter(task model.Task, req agentnode.TaskQueryRequest) bool {
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

func sortTasksForQuery(tasks []model.Task, req agentnode.TaskQueryRequest) {
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
