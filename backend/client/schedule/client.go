package schedule

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	schedulepb "github.com/LoveLosita/smartflow/backend/services/schedule/rpc/pb"
	schedulecontracts "github.com/LoveLosita/smartflow/backend/shared/contracts/schedule"
	"github.com/zeromicro/go-zero/zrpc"
)

const (
	defaultEndpoint = "127.0.0.1:9084"
	defaultTimeout  = 6 * time.Second
)

type ClientConfig struct {
	Endpoints []string
	Target    string
	Timeout   time.Duration
}

// Client 是 gateway 侧 schedule zrpc 的最小适配层。
//
// 职责边界：
// 1. 只负责跨进程 gRPC 调用和 JSON 透传，不碰 DAO、粗排算法或正式日程 apply 状态机；
// 2. HTTP 入参仍由 gateway/api 做基础绑定，业务校验交给 schedule 服务；
// 3. 复杂响应不在 gateway 重建模型，避免 DTO 复制扩散。
type Client struct {
	rpc schedulepb.ScheduleClient
}

func NewClient(cfg ClientConfig) (*Client, error) {
	timeout := cfg.Timeout
	if timeout <= 0 {
		timeout = defaultTimeout
	}
	endpoints := normalizeEndpoints(cfg.Endpoints)
	target := strings.TrimSpace(cfg.Target)
	if len(endpoints) == 0 && target == "" {
		endpoints = []string{defaultEndpoint}
	}

	zclient, err := zrpc.NewClient(zrpc.RpcClientConf{
		Endpoints: endpoints,
		Target:    target,
		NonBlock:  true,
		Timeout:   int64(timeout / time.Millisecond),
	})
	if err != nil {
		return nil, err
	}
	client := &Client{rpc: schedulepb.NewScheduleClient(zclient.Conn())}
	if err := client.ping(timeout); err != nil {
		return nil, err
	}
	return client, nil
}

func (c *Client) GetUserTodaySchedule(ctx context.Context, userID int) (json.RawMessage, error) {
	if err := c.ensureReady(); err != nil {
		return nil, err
	}
	resp, err := c.rpc.GetToday(ctx, &schedulepb.UserRequest{UserId: int64(userID)})
	return jsonFromResponse(resp, err)
}

func (c *Client) GetUserWeeklySchedule(ctx context.Context, userID int, week int) (json.RawMessage, error) {
	if err := c.ensureReady(); err != nil {
		return nil, err
	}
	resp, err := c.rpc.GetWeek(ctx, &schedulepb.WeekRequest{UserId: int64(userID), Week: int64(week)})
	return jsonFromResponse(resp, err)
}

func (c *Client) DeleteScheduleEvent(ctx context.Context, req schedulecontracts.DeleteScheduleEventsRequest) error {
	if err := c.ensureReady(); err != nil {
		return err
	}
	eventsJSON, err := json.Marshal(req.Events)
	if err != nil {
		return err
	}
	_, err = c.rpc.DeleteEvents(ctx, &schedulepb.DeleteEventsRequest{
		UserId:     int64(req.UserID),
		EventsJson: eventsJSON,
	})
	return responseFromRPCError(err)
}

func (c *Client) GetUserRecentCompletedSchedules(ctx context.Context, req schedulecontracts.RecentCompletedRequest) (json.RawMessage, error) {
	if err := c.ensureReady(); err != nil {
		return nil, err
	}
	resp, err := c.rpc.GetRecentCompleted(ctx, &schedulepb.RecentCompletedRequest{
		UserId: int64(req.UserID),
		Index:  int64(req.Index),
		Limit:  int64(req.Limit),
	})
	return jsonFromResponse(resp, err)
}

func (c *Client) GetUserOngoingSchedule(ctx context.Context, userID int) (json.RawMessage, error) {
	if err := c.ensureReady(); err != nil {
		return nil, err
	}
	resp, err := c.rpc.GetCurrent(ctx, &schedulepb.UserRequest{UserId: int64(userID)})
	return jsonFromResponse(resp, err)
}

func (c *Client) RevokeTaskItemFromSchedule(ctx context.Context, req schedulecontracts.RevokeTaskItemRequest) error {
	if err := c.ensureReady(); err != nil {
		return err
	}
	_, err := c.rpc.RevokeTaskItem(ctx, &schedulepb.RevokeTaskItemRequest{
		UserId:  int64(req.UserID),
		EventId: int64(req.EventID),
	})
	return responseFromRPCError(err)
}

func (c *Client) SmartPlanning(ctx context.Context, req schedulecontracts.SmartPlanningRequest) (json.RawMessage, error) {
	if err := c.ensureReady(); err != nil {
		return nil, err
	}
	resp, err := c.rpc.SmartPlanning(ctx, &schedulepb.SmartPlanningRequest{
		UserId:      int64(req.UserID),
		TaskClassId: int64(req.TaskClassID),
	})
	return jsonFromResponse(resp, err)
}

func (c *Client) SmartPlanningMulti(ctx context.Context, req schedulecontracts.SmartPlanningMultiRequest) (json.RawMessage, error) {
	if err := c.ensureReady(); err != nil {
		return nil, err
	}
	taskClassIDs := make([]int64, 0, len(req.TaskClassIDs))
	for _, id := range req.TaskClassIDs {
		taskClassIDs = append(taskClassIDs, int64(id))
	}
	resp, err := c.rpc.SmartPlanningMulti(ctx, &schedulepb.SmartPlanningMultiRequest{
		UserId:       int64(req.UserID),
		TaskClassIds: taskClassIDs,
	})
	return jsonFromResponse(resp, err)
}

func (c *Client) GetAgentWeekSchedule(ctx context.Context, req schedulecontracts.AgentScheduleWeekRequest) (json.RawMessage, error) {
	if err := c.ensureReady(); err != nil {
		return nil, err
	}
	payload, err := json.Marshal(req)
	if err != nil {
		return nil, err
	}
	resp, err := c.rpc.GetAgentWeekSchedule(ctx, &schedulepb.JSONRequest{PayloadJson: payload})
	return jsonFromResponse(resp, err)
}

func (c *Client) ensureReady() error {
	if c == nil || c.rpc == nil {
		return errors.New("schedule zrpc client is not initialized")
	}
	return nil
}

func (c *Client) ping(timeout time.Duration) error {
	if err := c.ensureReady(); err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	_, err := c.rpc.Ping(ctx, &schedulepb.StatusResponse{})
	return responseFromRPCError(err)
}

func jsonFromResponse(resp *schedulepb.JSONResponse, rpcErr error) (json.RawMessage, error) {
	if rpcErr != nil {
		return nil, responseFromRPCError(rpcErr)
	}
	if resp == nil {
		return nil, errors.New("schedule zrpc service returned empty JSON response")
	}
	if len(resp.DataJson) == 0 {
		return json.RawMessage("null"), nil
	}
	return json.RawMessage(resp.DataJson), nil
}

func normalizeEndpoints(values []string) []string {
	endpoints := make([]string, 0, len(values))
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed != "" {
			endpoints = append(endpoints, trimmed)
		}
	}
	return endpoints
}
