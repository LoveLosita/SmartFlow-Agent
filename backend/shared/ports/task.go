package ports

import (
	"context"
	"encoding/json"

	taskcontracts "github.com/LoveLosita/smartflow/backend/shared/contracts/task"
)

// TaskCommandClient 是 gateway 调用 task 服务的最小能力集合。
//
// 职责边界：
// 1. 只覆盖当前 `/api/v1/task/*` HTTP 门面需要的能力；
// 2. 不暴露 task DAO、outbox 状态机或 active-scheduler due job 同步细节；
// 3. 复杂响应先以 JSON 透传，避免 gateway 复制 task 内部 DTO。
type TaskCommandClient interface {
	AddTask(ctx context.Context, req taskcontracts.AddTaskRequest) (json.RawMessage, error)
	GetUserTasks(ctx context.Context, userID int) (json.RawMessage, error)
	BatchTaskStatus(ctx context.Context, req taskcontracts.BatchTaskStatusRequest) (json.RawMessage, error)
	CompleteTask(ctx context.Context, req taskcontracts.CompleteTaskRequest) (json.RawMessage, error)
	UndoCompleteTask(ctx context.Context, req taskcontracts.UndoCompleteTaskRequest) (json.RawMessage, error)
	UpdateTask(ctx context.Context, req taskcontracts.UpdateTaskRequest) (json.RawMessage, error)
	DeleteTask(ctx context.Context, req taskcontracts.DeleteTaskRequest) (json.RawMessage, error)
}
