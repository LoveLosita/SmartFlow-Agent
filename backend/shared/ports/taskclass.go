package ports

import (
	"context"
	"encoding/json"

	taskclasscontracts "github.com/LoveLosita/smartflow/backend/shared/contracts/taskclass"
)

// TaskClassCommandClient 是 gateway 调用 task-class 服务的最小能力集合。
//
// 职责边界：
// 1. 只覆盖当前 `/api/v1/task-class/*` HTTP 门面需要的能力；
// 2. 不暴露 task-class DAO、事务编排或迁移期 schedule 直写细节；
// 3. 复杂响应以 JSON 透传，避免 gateway 复制 task-class 内部 DTO。
type TaskClassCommandClient interface {
	AddTaskClass(ctx context.Context, req taskclasscontracts.UpsertTaskClassRequest) (json.RawMessage, error)
	ListTaskClasses(ctx context.Context, userID int) (json.RawMessage, error)
	GetTaskClass(ctx context.Context, req taskclasscontracts.GetTaskClassRequest) (json.RawMessage, error)
	UpdateTaskClass(ctx context.Context, req taskclasscontracts.UpsertTaskClassRequest) (json.RawMessage, error)
	InsertTaskClassItemIntoSchedule(ctx context.Context, req taskclasscontracts.InsertTaskClassItemIntoScheduleRequest) (json.RawMessage, error)
	DeleteTaskClassItem(ctx context.Context, req taskclasscontracts.DeleteTaskClassItemRequest) (json.RawMessage, error)
	DeleteTaskClass(ctx context.Context, req taskclasscontracts.DeleteTaskClassRequest) (json.RawMessage, error)
	ApplyBatchIntoSchedule(ctx context.Context, req taskclasscontracts.ApplyBatchIntoScheduleRequest) (json.RawMessage, error)
}
