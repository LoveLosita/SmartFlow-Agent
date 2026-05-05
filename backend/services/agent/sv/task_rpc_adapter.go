package sv

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	agentmodel "github.com/LoveLosita/smartflow/backend/services/agent/model"
	"github.com/LoveLosita/smartflow/backend/services/runtime/model"
	taskcontracts "github.com/LoveLosita/smartflow/backend/shared/contracts/task"
	"github.com/LoveLosita/smartflow/backend/shared/respond"
)

const quickTaskCreateRPCTimeout = 3 * time.Second

// TaskRPCClient 描述 agent 快捷任务链路访问 task zrpc 所需的最小能力。
//
// 职责边界：
// 1. 只覆盖快捷任务的创建和查询，不暴露 task DAO 或其它写接口；
// 2. 不要求 agent 编排层感知 pb / grpc 细节；
// 3. 错误原样返回，由 quick_task 节点转换成面向用户的失败文案。
type TaskRPCClient interface {
	AddTask(ctx context.Context, req taskcontracts.AddTaskRequest) (json.RawMessage, error)
	GetUserTasks(ctx context.Context, userID int) (json.RawMessage, error)
}

type taskRPCAdapter struct {
	client TaskRPCClient
}

// NewTaskRPCQuickTaskDeps 把 task zrpc client 适配成 agent 快捷任务依赖。
//
// 职责边界：
// 1. 只替换 agent quick task 的 task DAO 直连路径；
// 2. 不迁移 task-class upsert、schedule provider 或 agent 编排本体；
// 3. client 为空时返回零值依赖，让 quick_task 节点沿用既有“依赖缺失则报错”语义。
func NewTaskRPCQuickTaskDeps(client TaskRPCClient) agentmodel.QuickTaskDeps {
	if client == nil {
		return agentmodel.QuickTaskDeps{}
	}
	adapter := &taskRPCAdapter{client: client}
	return agentmodel.QuickTaskDeps{
		CreateTask: adapter.CreateTask,
		QueryTasks: adapter.QueryTasks,
	}
}

// CreateTask 通过 task zrpc 创建四象限任务，返回 task_id。
func (a *taskRPCAdapter) CreateTask(
	userID int,
	title string,
	priorityGroup int,
	estimatedSections int,
	deadlineAt *time.Time,
	urgencyThresholdAt *time.Time,
) (int, error) {
	if a == nil || a.client == nil {
		return 0, errors.New("task rpc client is nil")
	}
	ctx, cancel := context.WithTimeout(context.Background(), quickTaskCreateRPCTimeout)
	defer cancel()

	// 调用目的：把 quick_task 节点产出的结构化任务写入 task 服务，避免 agent 继续直连 tasks 表。
	raw, err := a.client.AddTask(ctx, taskcontracts.AddTaskRequest{
		UserID:             userID,
		Title:              strings.TrimSpace(title),
		PriorityGroup:      priorityGroup,
		EstimatedSections:  estimatedSections,
		DeadlineAt:         deadlineAt,
		UrgencyThresholdAt: urgencyThresholdAt,
	})
	if err != nil {
		return 0, err
	}

	var resp taskcontracts.AddTaskResponse
	if err := json.Unmarshal(raw, &resp); err != nil {
		return 0, err
	}
	if resp.ID <= 0 {
		return 0, errors.New("task rpc add task returned invalid task id")
	}
	return resp.ID, nil
}

// QueryTasks 通过 task zrpc 读取用户任务，再复用 agent 侧既有过滤、排序和展示转换语义。
func (a *taskRPCAdapter) QueryTasks(
	ctx context.Context,
	userID int,
	params agentmodel.TaskQueryParams,
) ([]agentmodel.TaskQueryResult, error) {
	if a == nil || a.client == nil {
		return nil, errors.New("task rpc client is nil")
	}
	raw, err := a.client.GetUserTasks(ctx, userID)
	if err != nil {
		if errors.Is(err, respond.UserTasksEmpty) {
			return []agentmodel.TaskQueryResult{}, nil
		}
		return nil, err
	}

	var items []taskcontracts.TaskListItem
	if len(raw) > 0 && string(raw) != "null" {
		if err := json.Unmarshal(raw, &items); err != nil {
			return nil, err
		}
	}
	tasks := taskListItemsToModels(items)

	req := agentmodel.TaskQueryRequest{
		UserID:           userID,
		Quadrant:         params.Quadrant,
		SortBy:           params.SortBy,
		Order:            params.Order,
		Limit:            params.Limit,
		IncludeCompleted: params.IncludeCompleted,
		Keyword:          params.Keyword,
		DeadlineBefore:   params.DeadlineBefore,
		DeadlineAfter:    params.DeadlineAfter,
	}

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
	return taskModelsToQueryResults(filtered), nil
}

func taskListItemsToModels(items []taskcontracts.TaskListItem) []model.Task {
	if len(items) == 0 {
		return nil
	}
	result := make([]model.Task, 0, len(items))
	for _, item := range items {
		result = append(result, model.Task{
			ID:                 item.ID,
			UserID:             item.UserID,
			Title:              item.Title,
			Priority:           item.PriorityGroup,
			EstimatedSections:  model.NormalizeEstimatedSections(&item.EstimatedSections),
			IsCompleted:        item.IsCompleted,
			DeadlineAt:         parseTaskListTime(item.Deadline),
			UrgencyThresholdAt: parseTaskListTime(item.UrgencyThresholdAt),
		})
	}
	return result
}

func taskModelsToQueryResults(tasks []model.Task) []agentmodel.TaskQueryResult {
	if len(tasks) == 0 {
		return []agentmodel.TaskQueryResult{}
	}
	results := make([]agentmodel.TaskQueryResult, 0, len(tasks))
	for _, task := range tasks {
		deadlineStr := ""
		if task.DeadlineAt != nil {
			deadlineStr = task.DeadlineAt.In(time.Local).Format("2006-01-02 15:04")
		}
		results = append(results, agentmodel.TaskQueryResult{
			ID:                task.ID,
			Title:             task.Title,
			PriorityGroup:     task.Priority,
			EstimatedSections: model.NormalizeEstimatedSections(&task.EstimatedSections),
			IsCompleted:       task.IsCompleted,
			DeadlineAt:        deadlineStr,
		})
	}
	return results
}

func parseTaskListTime(value string) *time.Time {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	for _, layout := range []string{time.RFC3339Nano, time.RFC3339, "2006-01-02 15:04:05", "2006-01-02 15:04"} {
		parsed, err := time.ParseInLocation(layout, value, time.Local)
		if err == nil {
			return &parsed
		}
	}
	return nil
}
