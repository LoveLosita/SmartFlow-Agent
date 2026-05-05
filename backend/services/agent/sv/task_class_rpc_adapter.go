package sv

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/LoveLosita/smartflow/backend/model"
	agenttools "github.com/LoveLosita/smartflow/backend/services/agent/tools"
	taskclasscontracts "github.com/LoveLosita/smartflow/backend/shared/contracts/taskclass"
)

const taskClassUpsertRPCTimeout = 6 * time.Second

// TaskClassUpsertRPCClient 描述 agent 写入任务类时依赖的 task-class RPC 最小能力。
//
// 职责边界：
// 1. 只覆盖 upsert_task_class 工具需要的新增/更新能力；
// 2. 不暴露 task-class DAO、事务细节或 schedule 迁移期直写语义；
// 3. 读取型能力由 schedule provider 的独立接口承载，避免接口膨胀。
type TaskClassUpsertRPCClient interface {
	AddTaskClass(ctx context.Context, req taskclasscontracts.UpsertTaskClassRequest) (json.RawMessage, error)
	UpdateTaskClass(ctx context.Context, req taskclasscontracts.UpsertTaskClassRequest) (json.RawMessage, error)
}

type taskClassRPCUpsertAdapter struct {
	client TaskClassUpsertRPCClient
}

// NewTaskClassRPCUpsertFunc 把 task-class zrpc client 适配成 agent 工具写入函数。
//
// 职责边界：
// 1. 只替换 agent upsert_task_class 的 DAO 直连路径；
// 2. 入参仍复用 agent 工具层已标准化的 UserAddTaskClassRequest；
// 3. client 为空时返回会失败的闭包，让工具层保留既有错误包装语义。
func NewTaskClassRPCUpsertFunc(client TaskClassUpsertRPCClient) func(userID int, input agenttools.TaskClassUpsertInput) (agenttools.TaskClassUpsertPersistResult, error) {
	adapter := &taskClassRPCUpsertAdapter{client: client}
	return adapter.UpsertTaskClass
}

// UpsertTaskClass 通过 task-class zrpc 新增或更新任务类，并返回稳定 task_class_id。
func (a *taskClassRPCUpsertAdapter) UpsertTaskClass(userID int, input agenttools.TaskClassUpsertInput) (agenttools.TaskClassUpsertPersistResult, error) {
	if a == nil || a.client == nil {
		return agenttools.TaskClassUpsertPersistResult{}, errors.New("task-class rpc client is nil")
	}
	req := taskClassUpsertInputToContract(userID, input)

	ctx, cancel := context.WithTimeout(context.Background(), taskClassUpsertRPCTimeout)
	defer cancel()

	var raw json.RawMessage
	var err error
	created := input.ID == 0
	// 调用目的：把 agent 工具产出的任务类写入 task-class 服务，避免 agent 继续直连 task_classes/task_items。
	if created {
		raw, err = a.client.AddTaskClass(ctx, req)
	} else {
		raw, err = a.client.UpdateTaskClass(ctx, req)
	}
	if err != nil {
		return agenttools.TaskClassUpsertPersistResult{}, err
	}

	var resp taskclasscontracts.UpsertTaskClassResponse
	if err := json.Unmarshal(raw, &resp); err != nil {
		return agenttools.TaskClassUpsertPersistResult{}, err
	}
	if resp.TaskClassID <= 0 {
		return agenttools.TaskClassUpsertPersistResult{}, errors.New("task-class rpc upsert returned invalid task_class_id")
	}
	return agenttools.TaskClassUpsertPersistResult{
		TaskClassID: resp.TaskClassID,
		Created:     resp.Created,
	}, nil
}

func taskClassUpsertInputToContract(userID int, input agenttools.TaskClassUpsertInput) taskclasscontracts.UpsertTaskClassRequest {
	req := input.Request
	items := make([]taskclasscontracts.UpsertTaskClassItemConfig, 0, len(req.Items))
	for _, item := range req.Items {
		items = append(items, taskclasscontracts.UpsertTaskClassItemConfig{
			ID:           item.ID,
			Order:        item.Order,
			Content:      strings.TrimSpace(item.Content),
			EmbeddedTime: toTaskClassContractTargetTime(item.EmbeddedTime),
		})
	}
	return taskclasscontracts.UpsertTaskClassRequest{
		UserID:             userID,
		TaskClassID:        input.ID,
		Name:               strings.TrimSpace(req.Name),
		StartDate:          strings.TrimSpace(req.StartDate),
		EndDate:            strings.TrimSpace(req.EndDate),
		Mode:               strings.TrimSpace(req.Mode),
		SubjectType:        strings.TrimSpace(req.SubjectType),
		DifficultyLevel:    strings.TrimSpace(req.DifficultyLevel),
		CognitiveIntensity: strings.TrimSpace(req.CognitiveIntensity),
		Config: taskclasscontracts.UpsertTaskClassConfig{
			TotalSlots:         req.Config.TotalSlots,
			AllowFillerCourse:  req.Config.AllowFillerCourse,
			Strategy:           strings.TrimSpace(req.Config.Strategy),
			ExcludedSlots:      append([]int(nil), req.Config.ExcludedSlots...),
			ExcludedDaysOfWeek: append([]int(nil), req.Config.ExcludedDaysOfWeek...),
		},
		Items: items,
	}
}

func toTaskClassContractTargetTime(value *model.TargetTime) *taskclasscontracts.TargetTime {
	if value == nil {
		return nil
	}
	return &taskclasscontracts.TargetTime{
		Week:        value.Week,
		DayOfWeek:   value.DayOfWeek,
		SectionFrom: value.SectionFrom,
		SectionTo:   value.SectionTo,
	}
}
