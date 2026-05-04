package ports

import (
	"context"
	"encoding/json"

	contracts "github.com/LoveLosita/smartflow/backend/shared/contracts/activescheduler"
)

// ActiveSchedulerCommandClient 是 gateway 调用 active-scheduler 服务的最小能力集合。
//
// 职责边界：
// 1. 只覆盖当前 HTTP 入口需要的 dry-run / trigger / preview / confirm；
// 2. 不暴露 active-scheduler 的 DAO、graph、selection、job scanner 或 outbox consumer；
// 3. 复杂响应先以原始 JSON 透传，避免 gateway 重建一套主动调度 DTO。
type ActiveSchedulerCommandClient interface {
	DryRun(ctx context.Context, req contracts.ActiveScheduleRequest) (json.RawMessage, error)
	Trigger(ctx context.Context, req contracts.ActiveScheduleRequest) (*contracts.TriggerResponse, error)
	CreatePreview(ctx context.Context, req contracts.ActiveScheduleRequest) (json.RawMessage, error)
	GetPreview(ctx context.Context, req contracts.GetPreviewRequest) (json.RawMessage, error)
	ConfirmPreview(ctx context.Context, req contracts.ConfirmPreviewRequest) (json.RawMessage, error)
}
