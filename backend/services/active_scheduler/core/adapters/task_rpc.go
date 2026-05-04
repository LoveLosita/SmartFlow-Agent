package adapters

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	activeports "github.com/LoveLosita/smartflow/backend/services/active_scheduler/core/ports"
	taskpb "github.com/LoveLosita/smartflow/backend/services/task/rpc/pb"
	taskcontracts "github.com/LoveLosita/smartflow/backend/shared/contracts/task"
	"github.com/zeromicro/go-zero/zrpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const (
	defaultTaskRPCEndpoint = "127.0.0.1:9085"
	defaultTaskRPCTimeout  = 6 * time.Second
)

type TaskRPCConfig struct {
	Endpoints []string
	Target    string
	Timeout   time.Duration
}

// TaskRPCAdapter 是 active-scheduler 访问 task 服务的 RPC 适配器。
//
// 职责边界：
// 1. 只读取 task_pool 事实并转换为 active-scheduler 内部 DTO；
// 2. 不写 tasks 表、不维护 task outbox，也不处理 due job 状态；
// 3. 让 active-scheduler dry-run / due scanner 不再直接访问 tasks 表。
type TaskRPCAdapter struct {
	rpc taskpb.TaskClient
}

func NewTaskRPCAdapter(cfg TaskRPCConfig) (*TaskRPCAdapter, error) {
	timeout := cfg.Timeout
	if timeout <= 0 {
		timeout = defaultTaskRPCTimeout
	}
	endpoints := normalizeTaskRPCEndpoints(cfg.Endpoints)
	target := strings.TrimSpace(cfg.Target)
	if len(endpoints) == 0 && target == "" {
		endpoints = []string{defaultTaskRPCEndpoint}
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
	adapter := &TaskRPCAdapter{rpc: taskpb.NewTaskClient(zclient.Conn())}
	if err := adapter.ping(timeout); err != nil {
		return nil, err
	}
	return adapter, nil
}

func (a *TaskRPCAdapter) GetTaskForActiveSchedule(ctx context.Context, req activeports.TaskRequest) (activeports.TaskFact, bool, error) {
	if err := a.ensureReady(); err != nil {
		return activeports.TaskFact{}, false, err
	}
	payload, err := json.Marshal(taskcontracts.TaskFactRequest{
		UserID: req.UserID,
		TaskID: req.TaskID,
		Now:    req.Now,
	})
	if err != nil {
		return activeports.TaskFact{}, false, err
	}
	resp, err := a.rpc.GetTaskForActiveSchedule(ctx, &taskpb.JSONRequest{PayloadJson: payload})
	if err != nil {
		return activeports.TaskFact{}, false, taskRPCError(err)
	}
	var contractResp taskcontracts.TaskFactResponse
	if err := json.Unmarshal(taskJSONBytes(resp), &contractResp); err != nil {
		return activeports.TaskFact{}, false, err
	}
	return taskFactToActive(contractResp.Task), contractResp.Found, nil
}

func (a *TaskRPCAdapter) ensureReady() error {
	if a == nil || a.rpc == nil {
		return errors.New("task rpc adapter 未初始化")
	}
	return nil
}

func (a *TaskRPCAdapter) ping(timeout time.Duration) error {
	if err := a.ensureReady(); err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	_, err := a.rpc.Ping(ctx, &taskpb.StatusResponse{})
	return taskRPCError(err)
}

func taskRPCError(err error) error {
	if err == nil {
		return nil
	}
	st, ok := status.FromError(err)
	if !ok {
		return err
	}
	if st.Code() == codes.NotFound {
		return nil
	}
	if st.Code() == codes.Internal || st.Code() == codes.Unavailable || st.Code() == codes.DeadlineExceeded {
		return fmt.Errorf("调用 task zrpc 服务失败: %w", err)
	}
	return err
}

func taskFactToActive(task taskcontracts.TaskFact) activeports.TaskFact {
	return activeports.TaskFact{
		ID:                 task.ID,
		UserID:             task.UserID,
		Title:              task.Title,
		Priority:           task.Priority,
		IsCompleted:        task.IsCompleted,
		DeadlineAt:         task.DeadlineAt,
		UrgencyThresholdAt: task.UrgencyThresholdAt,
		EstimatedSections:  task.EstimatedSections,
	}
}

func taskJSONBytes(resp *taskpb.JSONResponse) []byte {
	if resp == nil || len(resp.DataJson) == 0 {
		return []byte("null")
	}
	return resp.DataJson
}

func normalizeTaskRPCEndpoints(values []string) []string {
	endpoints := make([]string, 0, len(values))
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed != "" {
			endpoints = append(endpoints, trimmed)
		}
	}
	return endpoints
}
