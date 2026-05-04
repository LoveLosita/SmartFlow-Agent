package ports

import (
	"context"
	"encoding/json"

	schedulecontracts "github.com/LoveLosita/smartflow/backend/shared/contracts/schedule"
)

// ScheduleCommandClient 是 gateway 调用 schedule 服务的最小能力集合。
//
// 职责边界：
// 1. 只覆盖当前 /api/v1/schedule HTTP 门面需要的能力；
// 2. 不暴露 schedule DAO、事务编排、粗排算法或 apply 状态机；
// 3. 复杂响应先以 JSON 透传，避免 gateway 复制 schedule 内部 DTO。
type ScheduleCommandClient interface {
	GetUserTodaySchedule(ctx context.Context, userID int) (json.RawMessage, error)
	GetUserWeeklySchedule(ctx context.Context, userID int, week int) (json.RawMessage, error)
	DeleteScheduleEvent(ctx context.Context, req schedulecontracts.DeleteScheduleEventsRequest) error
	GetUserRecentCompletedSchedules(ctx context.Context, req schedulecontracts.RecentCompletedRequest) (json.RawMessage, error)
	GetUserOngoingSchedule(ctx context.Context, userID int) (json.RawMessage, error)
	RevokeTaskItemFromSchedule(ctx context.Context, req schedulecontracts.RevokeTaskItemRequest) error
	SmartPlanning(ctx context.Context, req schedulecontracts.SmartPlanningRequest) (json.RawMessage, error)
	SmartPlanningMulti(ctx context.Context, req schedulecontracts.SmartPlanningMultiRequest) (json.RawMessage, error)
}
