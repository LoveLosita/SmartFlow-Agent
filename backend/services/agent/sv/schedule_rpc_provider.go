package sv

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	agentconv "github.com/LoveLosita/smartflow/backend/services/agent/conv"
	scheduletool "github.com/LoveLosita/smartflow/backend/services/agent/tools/schedule"
	"github.com/LoveLosita/smartflow/backend/services/runtime/model"
	schedulecontracts "github.com/LoveLosita/smartflow/backend/shared/contracts/schedule"
	taskclasscontracts "github.com/LoveLosita/smartflow/backend/shared/contracts/taskclass"
)

const scheduleProviderRPCTimeout = 6 * time.Second

// ScheduleAgentRPCClient 描述 agent schedule provider 读取 schedule 服务所需的最小能力。
//
// 职责边界：
// 1. 只读取按周原始日程槽位事实；
// 2. 不暴露 schedule DAO、缓存或写入状态机；
// 3. 返回 JSON 契约后由 provider 复用既有 LoadScheduleState 建模逻辑。
type ScheduleAgentRPCClient interface {
	GetAgentWeekSchedule(ctx context.Context, req schedulecontracts.AgentScheduleWeekRequest) (json.RawMessage, error)
}

// TaskClassAgentReadRPCClient 描述 agent schedule provider 读取 task-class 服务所需的最小能力。
type TaskClassAgentReadRPCClient interface {
	GetAgentTaskClasses(ctx context.Context, req taskclasscontracts.AgentTaskClassesRequest) (json.RawMessage, error)
}

// TaskClassAgentRPCClient 聚合 agent 当前依赖的 task-class RPC 写入与读取能力。
type TaskClassAgentRPCClient interface {
	TaskClassUpsertRPCClient
	TaskClassAgentReadRPCClient
}

// ScheduleRPCProvider 通过 schedule/task-class zrpc 构建 agent ScheduleState。
//
// 职责边界：
// 1. 只替换 agent schedule provider 的 DAO 读取路径；
// 2. 窗口推导、extra category 与 ScheduleState 建模继续复用 agent/conv 老逻辑；
// 3. 不负责持久化 Diff，不改变 confirm/apply 链路。
type ScheduleRPCProvider struct {
	scheduleClient  ScheduleAgentRPCClient
	taskClassClient TaskClassAgentReadRPCClient
}

func NewScheduleRPCProvider(scheduleClient ScheduleAgentRPCClient, taskClassClient TaskClassAgentReadRPCClient) *ScheduleRPCProvider {
	return &ScheduleRPCProvider{
		scheduleClient:  scheduleClient,
		taskClassClient: taskClassClient,
	}
}

func (p *ScheduleRPCProvider) LoadScheduleState(ctx context.Context, userID int) (*scheduletool.ScheduleState, error) {
	taskClasses, err := p.loadCompleteTaskClasses(ctx, userID, nil)
	if err != nil {
		return nil, err
	}
	return p.loadScheduleStateWithTaskClasses(ctx, userID, taskClasses, true)
}

func (p *ScheduleRPCProvider) LoadScheduleStateForTaskClasses(ctx context.Context, userID int, taskClassIDs []int) (*scheduletool.ScheduleState, error) {
	if len(taskClassIDs) == 0 {
		return p.LoadScheduleState(ctx, userID)
	}
	taskClasses, err := p.loadCompleteTaskClasses(ctx, userID, taskClassIDs)
	if err != nil {
		return nil, err
	}
	return p.loadScheduleStateWithTaskClasses(ctx, userID, taskClasses, false)
}

func (p *ScheduleRPCProvider) LoadTaskClassMetas(ctx context.Context, userID int, taskClassIDs []int) ([]scheduletool.TaskClassMeta, error) {
	if len(taskClassIDs) == 0 {
		return nil, nil
	}
	taskClasses, err := p.loadCompleteTaskClasses(ctx, userID, taskClassIDs)
	if err != nil {
		return nil, err
	}
	return agentconv.TaskClassesToScheduleMetas(taskClasses), nil
}

func (p *ScheduleRPCProvider) loadScheduleStateWithTaskClasses(ctx context.Context, userID int, taskClasses []model.TaskClass, allowCurrentWeekFallback bool) (*scheduletool.ScheduleState, error) {
	windowDays, weeks := agentconv.BuildWindowFromTaskClasses(taskClasses)
	if len(windowDays) == 0 {
		if !allowCurrentWeekFallback {
			return nil, fmt.Errorf("任务类缺少有效时间窗：请补充 start_date/end_date 后再进行智能编排")
		}
		var err error
		windowDays, weeks, err = agentconv.BuildCurrentWeekWindow()
		if err != nil {
			return nil, err
		}
	}

	allSchedules := make([]model.Schedule, 0)
	for _, week := range weeks {
		weekSchedules, err := p.loadWeekSchedules(ctx, userID, week)
		if err != nil {
			return nil, fmt.Errorf("通过 schedule RPC 加载用户周日程失败 week=%d: %w", week, err)
		}
		allSchedules = append(allSchedules, weekSchedules...)
	}

	extraItemCategories := agentconv.BuildExtraItemCategories(allSchedules, taskClasses)
	return agentconv.LoadScheduleState(allSchedules, taskClasses, extraItemCategories, windowDays), nil
}

func (p *ScheduleRPCProvider) loadCompleteTaskClasses(ctx context.Context, userID int, taskClassIDs []int) ([]model.TaskClass, error) {
	if p == nil || p.taskClassClient == nil {
		return nil, errors.New("task-class rpc reader is nil")
	}
	callCtx, cancel := context.WithTimeout(ctx, scheduleProviderRPCTimeout)
	defer cancel()

	raw, err := p.taskClassClient.GetAgentTaskClasses(callCtx, taskclasscontracts.AgentTaskClassesRequest{
		UserID:       userID,
		TaskClassIDs: append([]int(nil), taskClassIDs...),
	})
	if err != nil {
		return nil, err
	}
	var resp taskclasscontracts.AgentTaskClassesResponse
	if len(raw) > 0 && string(raw) != "null" {
		if err := json.Unmarshal(raw, &resp); err != nil {
			return nil, err
		}
	}
	taskClasses := make([]model.TaskClass, 0, len(resp.TaskClasses))
	for _, item := range resp.TaskClasses {
		taskClass, err := agentTaskClassToModel(item)
		if err != nil {
			return nil, err
		}
		taskClasses = append(taskClasses, taskClass)
	}
	return taskClasses, nil
}

func (p *ScheduleRPCProvider) loadWeekSchedules(ctx context.Context, userID int, week int) ([]model.Schedule, error) {
	if p == nil || p.scheduleClient == nil {
		return nil, errors.New("schedule rpc reader is nil")
	}
	callCtx, cancel := context.WithTimeout(ctx, scheduleProviderRPCTimeout)
	defer cancel()

	raw, err := p.scheduleClient.GetAgentWeekSchedule(callCtx, schedulecontracts.AgentScheduleWeekRequest{
		UserID: userID,
		Week:   week,
	})
	if err != nil {
		return nil, err
	}
	var resp schedulecontracts.AgentScheduleWeekResponse
	if len(raw) > 0 && string(raw) != "null" {
		if err := json.Unmarshal(raw, &resp); err != nil {
			return nil, err
		}
	}
	schedules := make([]model.Schedule, 0, len(resp.Schedules))
	for _, item := range resp.Schedules {
		schedules = append(schedules, agentScheduleSlotToModel(item))
	}
	return schedules, nil
}

func agentTaskClassToModel(in taskclasscontracts.AgentTaskClass) (model.TaskClass, error) {
	startDate, err := parseAgentDate(in.StartDate)
	if err != nil {
		return model.TaskClass{}, err
	}
	endDate, err := parseAgentDate(in.EndDate)
	if err != nil {
		return model.TaskClass{}, err
	}
	items := make([]model.TaskClassItem, 0, len(in.Items))
	for _, item := range in.Items {
		content := item.Content
		items = append(items, model.TaskClassItem{
			ID:           item.ID,
			CategoryID:   cloneIntPtr(item.CategoryID),
			Order:        cloneIntPtr(item.Order),
			Content:      &content,
			EmbeddedTime: taskClassContractTargetTimeToModel(item.EmbeddedTime),
			Status:       cloneIntPtr(item.Status),
		})
	}
	return model.TaskClass{
		ID:                 in.ID,
		UserID:             intPtrOrNil(in.UserID),
		Name:               stringPtrOrNil(in.Name),
		Mode:               stringPtrOrNil(in.Mode),
		StartDate:          startDate,
		EndDate:            endDate,
		SubjectType:        stringPtrOrNil(in.SubjectType),
		DifficultyLevel:    stringPtrOrNil(in.DifficultyLevel),
		CognitiveIntensity: stringPtrOrNil(in.CognitiveIntensity),
		TotalSlots:         intPtrOrNil(in.TotalSlots),
		AllowFillerCourse:  boolPtr(in.AllowFillerCourse),
		Strategy:           stringPtrOrNil(in.Strategy),
		ExcludedSlots:      model.IntSlice(append([]int(nil), in.ExcludedSlots...)),
		ExcludedDaysOfWeek: model.IntSlice(append([]int(nil), in.ExcludedDaysOfWeek...)),
		Items:              items,
	}, nil
}

func agentScheduleSlotToModel(in schedulecontracts.AgentScheduleSlot) model.Schedule {
	return model.Schedule{
		ID:             in.ID,
		EventID:        in.EventID,
		UserID:         in.UserID,
		Week:           in.Week,
		DayOfWeek:      in.DayOfWeek,
		Section:        in.Section,
		EmbeddedTaskID: cloneIntPtr(in.EmbeddedTaskID),
		Status:         in.Status,
		Event:          agentScheduleEventToModel(in.Event),
		EmbeddedTask:   agentScheduleTaskItemToModel(in.EmbeddedTask),
	}
}

func agentScheduleEventToModel(in *schedulecontracts.AgentScheduleEvent) *model.ScheduleEvent {
	if in == nil {
		return nil
	}
	return &model.ScheduleEvent{
		ID:             in.ID,
		UserID:         in.UserID,
		Name:           in.Name,
		Location:       cloneStringPtr(in.Location),
		Type:           in.Type,
		RelID:          cloneIntPtr(in.RelID),
		TaskSourceType: in.TaskSourceType,
		CanBeEmbedded:  in.CanBeEmbedded,
		StartTime:      in.StartTime,
		EndTime:        in.EndTime,
	}
}

func agentScheduleTaskItemToModel(in *schedulecontracts.AgentScheduleTaskItem) *model.TaskClassItem {
	if in == nil {
		return nil
	}
	content := in.Content
	return &model.TaskClassItem{
		ID:           in.ID,
		CategoryID:   cloneIntPtr(in.CategoryID),
		Order:        cloneIntPtr(in.Order),
		Content:      &content,
		EmbeddedTime: scheduleContractTargetTimeToModel(in.EmbeddedTime),
		Status:       cloneIntPtr(in.Status),
	}
}

func parseAgentDate(value string) (*time.Time, error) {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return nil, nil
	}
	parsed, err := time.ParseInLocation("2006-01-02", trimmed, time.Local)
	if err != nil {
		return nil, err
	}
	return &parsed, nil
}

func taskClassContractTargetTimeToModel(value *taskclasscontracts.TargetTime) *model.TargetTime {
	if value == nil {
		return nil
	}
	return &model.TargetTime{
		Week:        value.Week,
		DayOfWeek:   value.DayOfWeek,
		SectionFrom: value.SectionFrom,
		SectionTo:   value.SectionTo,
	}
}

func scheduleContractTargetTimeToModel(value *schedulecontracts.AgentScheduleTargetTime) *model.TargetTime {
	if value == nil {
		return nil
	}
	return &model.TargetTime{
		Week:        value.Week,
		DayOfWeek:   value.DayOfWeek,
		SectionFrom: value.SectionFrom,
		SectionTo:   value.SectionTo,
	}
}

func stringPtrOrNil(value string) *string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return nil
	}
	return &trimmed
}

func intPtrOrNil(value int) *int {
	if value == 0 {
		return nil
	}
	return &value
}

func boolPtr(value bool) *bool {
	return &value
}

func cloneIntPtr(value *int) *int {
	if value == nil {
		return nil
	}
	copied := *value
	return &copied
}

func cloneStringPtr(value *string) *string {
	if value == nil {
		return nil
	}
	copied := *value
	return &copied
}
