package adapter

import (
	"context"
	"encoding/json"
	"errors"
	"sort"
	"strings"
	"time"

	taskclassclient "github.com/LoveLosita/smartflow/backend/client/taskclass"
	forumsv "github.com/LoveLosita/smartflow/backend/services/taskclassforum/sv"
	taskclasscontracts "github.com/LoveLosita/smartflow/backend/shared/contracts/taskclass"
	"github.com/LoveLosita/smartflow/backend/shared/respond"
)

var errTaskClassRPCAdapterNotReady = errors.New("taskclassforum task-class rpc adapter is not initialized")

// TaskClassRPCAdapter 负责把 task-class 独立服务适配成计划广场需要的快照端口。
//
// 职责边界：
// 1. 只通过 task-class zrpc 读取/创建任务类，不直连 task_classes / task_items 物理表；
// 2. 只暴露论坛导入/发布需要的白名单快照语义，不透传 schedule 写入能力；
// 3. 论坛业务层只依赖快照端口，后续 task-class 契约继续演进时只改这一层。
type TaskClassRPCAdapter struct {
	client *taskclassclient.Client
}

var _ forumsv.TaskClassSnapshotPort = (*TaskClassRPCAdapter)(nil)

// NewTaskClassRPCAdapter 创建基于 task-class zrpc 的论坛快照适配器。
func NewTaskClassRPCAdapter(client *taskclassclient.Client) *TaskClassRPCAdapter {
	return &TaskClassRPCAdapter{client: client}
}

// GetOwnedTaskClassSnapshot 读取当前用户自己的 TaskClass，并投影为论坛可分享快照。
//
// 职责边界：
// 1. 只读取当前用户可见的单个 TaskClass；
// 2. 只返回论坛白名单字段和条目 source id/order/content；
// 3. 不透传 embedded_time、status 和任何 schedule 绑定细节。
func (a *TaskClassRPCAdapter) GetOwnedTaskClassSnapshot(ctx context.Context, userID uint64, taskClassID uint64) (*forumsv.TaskClassSnapshot, error) {
	if err := a.ensureReady(); err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	userIDInt, err := toUserID(userID)
	if err != nil {
		return nil, err
	}
	taskClassIDInt, err := toTaskClassID(taskClassID)
	if err != nil {
		return nil, err
	}

	raw, err := a.client.GetAgentTaskClasses(ctx, taskclasscontracts.AgentTaskClassesRequest{
		UserID:       userIDInt,
		TaskClassIDs: []int{taskClassIDInt},
	})
	if err != nil {
		return nil, err
	}

	var resp taskclasscontracts.AgentTaskClassesResponse
	if err := json.Unmarshal(raw, &resp); err != nil {
		return nil, err
	}
	if len(resp.TaskClasses) == 0 {
		return nil, respond.UserTaskClassNotFound
	}

	snapshot, err := snapshotFromTaskClass(resp.TaskClasses[0])
	if err != nil {
		return nil, err
	}
	return &snapshot, nil
}

// CreateTaskClassFromSnapshot 基于论坛模板快照为当前用户创建 task-class 服务里的副本。
//
// 职责边界：
// 1. 只创建 task-class 主体与 items，不写 forum_imports；
// 2. 所有 item 都作为新记录创建，不沿用原任务条目的 ID；
// 3. 不写 schedule，导入后仍保持“当前用户自己的未安排副本”语义。
func (a *TaskClassRPCAdapter) CreateTaskClassFromSnapshot(ctx context.Context, userID uint64, snapshot forumsv.TaskClassSnapshot, targetTitle string) (*forumsv.CreatedTaskClass, error) {
	if err := a.ensureReady(); err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	userIDInt, err := toUserID(userID)
	if err != nil {
		return nil, err
	}

	title := strings.TrimSpace(targetTitle)
	if title == "" {
		title = strings.TrimSpace(snapshot.Title)
	}
	if title == "" || strings.TrimSpace(snapshot.Mode) == "" {
		return nil, respond.MissingParam
	}

	if _, _, err := parseSnapshotDateRange(snapshot.Mode, snapshot.StartDate, snapshot.EndDate); err != nil {
		return nil, err
	}

	raw, err := a.client.AddTaskClass(ctx, buildUpsertTaskClassRequest(userIDInt, title, snapshot))
	if err != nil {
		return nil, err
	}

	var created taskclasscontracts.UpsertTaskClassResponse
	if err := json.Unmarshal(raw, &created); err != nil {
		return nil, err
	}
	if created.TaskClassID <= 0 {
		return nil, respond.InternalError(errors.New("task-class rpc add response missing task_class_id"))
	}
	return &forumsv.CreatedTaskClass{
		TaskClassID: uint64(created.TaskClassID),
		Title:       title,
	}, nil
}

func snapshotFromTaskClass(taskClass taskclasscontracts.AgentTaskClass) (forumsv.TaskClassSnapshot, error) {
	items := snapshotItemsFromTaskClassItems(taskClass.Items)
	snapshot := forumsv.TaskClassSnapshot{
		TaskClassID:        uint64(taskClass.ID),
		Title:              strings.TrimSpace(taskClass.Name),
		Mode:               strings.TrimSpace(taskClass.Mode),
		StartDate:          strings.TrimSpace(taskClass.StartDate),
		EndDate:            strings.TrimSpace(taskClass.EndDate),
		SubjectType:        strings.TrimSpace(taskClass.SubjectType),
		DifficultyLevel:    strings.TrimSpace(taskClass.DifficultyLevel),
		CognitiveIntensity: strings.TrimSpace(taskClass.CognitiveIntensity),
		TotalSlots:         taskClass.TotalSlots,
		AllowFillerCourse:  taskClass.AllowFillerCourse,
		Strategy:           strings.TrimSpace(taskClass.Strategy),
		ExcludedSlots:      cloneIntSlice(taskClass.ExcludedSlots),
		ExcludedDaysOfWeek: cloneIntSlice(taskClass.ExcludedDaysOfWeek),
		StrategyLabels:     strategyLabels(taskClass.Strategy),
		Items:              items,
	}

	configJSON, err := buildConfigSnapshotJSON(snapshot)
	if err != nil {
		return forumsv.TaskClassSnapshot{}, err
	}
	snapshot.ConfigSnapshotJSON = configJSON
	return snapshot, nil
}

func snapshotItemsFromTaskClassItems(items []taskclasscontracts.AgentTaskClassItem) []forumsv.TaskClassSnapshotItem {
	if len(items) == 0 {
		return []forumsv.TaskClassSnapshotItem{}
	}

	sorted := append([]taskclasscontracts.AgentTaskClassItem(nil), items...)
	sort.SliceStable(sorted, func(i, j int) bool {
		leftOrder := derefInt(sorted[i].Order)
		rightOrder := derefInt(sorted[j].Order)
		if leftOrder != rightOrder {
			return leftOrder < rightOrder
		}
		return sorted[i].ID < sorted[j].ID
	})

	result := make([]forumsv.TaskClassSnapshotItem, 0, len(sorted))
	for _, item := range sorted {
		content := strings.TrimSpace(item.Content)
		if content == "" {
			continue
		}
		result = append(result, forumsv.TaskClassSnapshotItem{
			TaskItemID: uint64(item.ID),
			Order:      derefInt(item.Order),
			Content:    content,
		})
	}
	return result
}

func buildUpsertTaskClassRequest(userID int, title string, snapshot forumsv.TaskClassSnapshot) taskclasscontracts.UpsertTaskClassRequest {
	items := make([]taskclasscontracts.UpsertTaskClassItemConfig, 0, len(snapshot.Items))
	sortedItems := append([]forumsv.TaskClassSnapshotItem(nil), snapshot.Items...)
	sort.SliceStable(sortedItems, func(i, j int) bool {
		if sortedItems[i].Order != sortedItems[j].Order {
			return sortedItems[i].Order < sortedItems[j].Order
		}
		return sortedItems[i].TaskItemID < sortedItems[j].TaskItemID
	})
	for _, item := range sortedItems {
		content := strings.TrimSpace(item.Content)
		if content == "" {
			continue
		}
		items = append(items, taskclasscontracts.UpsertTaskClassItemConfig{
			Order:   item.Order,
			Content: content,
		})
	}

	return taskclasscontracts.UpsertTaskClassRequest{
		UserID:             userID,
		Name:               title,
		StartDate:          strings.TrimSpace(snapshot.StartDate),
		EndDate:            strings.TrimSpace(snapshot.EndDate),
		Mode:               strings.TrimSpace(snapshot.Mode),
		SubjectType:        strings.TrimSpace(snapshot.SubjectType),
		DifficultyLevel:    strings.TrimSpace(snapshot.DifficultyLevel),
		CognitiveIntensity: strings.TrimSpace(snapshot.CognitiveIntensity),
		Config: taskclasscontracts.UpsertTaskClassConfig{
			TotalSlots:         snapshot.TotalSlots,
			AllowFillerCourse:  snapshot.AllowFillerCourse,
			Strategy:           strings.TrimSpace(snapshot.Strategy),
			ExcludedSlots:      cloneIntSlice(snapshot.ExcludedSlots),
			ExcludedDaysOfWeek: cloneIntSlice(snapshot.ExcludedDaysOfWeek),
		},
		Items: items,
	}
}

func strategyLabels(strategy string) []string {
	trimmed := strings.TrimSpace(strategy)
	if trimmed == "" {
		return []string{}
	}
	return []string{trimmed}
}

func (a *TaskClassRPCAdapter) ensureReady() error {
	if a == nil || a.client == nil {
		return errTaskClassRPCAdapterNotReady
	}
	return nil
}

func toUserID(value uint64) (int, error) {
	if value == 0 || value > uint64(maxIntValue()) {
		return 0, respond.WrongUserID
	}
	return int(value), nil
}

func toTaskClassID(value uint64) (int, error) {
	if value == 0 || value > uint64(maxIntValue()) {
		return 0, respond.WrongTaskClassID
	}
	return int(value), nil
}

func maxIntValue() int {
	return int(^uint(0) >> 1)
}

func derefInt(value *int) int {
	if value == nil {
		return 0
	}
	return *value
}

func parseSnapshotDateRange(mode string, startDate string, endDate string) (*time.Time, *time.Time, error) {
	parsedStart, err := parseDatePtr(startDate)
	if err != nil {
		return nil, nil, respond.WrongParamType
	}
	parsedEnd, err := parseDatePtr(endDate)
	if err != nil {
		return nil, nil, respond.WrongParamType
	}

	if strings.TrimSpace(mode) != "auto" {
		return parsedStart, parsedEnd, nil
	}
	if parsedStart == nil || parsedEnd == nil {
		return nil, nil, respond.MissingParamForAutoScheduling
	}
	if parsedStart.After(*parsedEnd) {
		return nil, nil, respond.InvalidDateRange
	}
	return parsedStart, parsedEnd, nil
}

func buildConfigSnapshotJSON(snapshot forumsv.TaskClassSnapshot) (string, error) {
	raw, err := json.Marshal(map[string]any{
		"mode":                  snapshot.Mode,
		"start_date":            snapshot.StartDate,
		"end_date":              snapshot.EndDate,
		"subject_type":          snapshot.SubjectType,
		"difficulty_level":      snapshot.DifficultyLevel,
		"cognitive_intensity":   snapshot.CognitiveIntensity,
		"total_slots":           snapshot.TotalSlots,
		"allow_filler_course":   snapshot.AllowFillerCourse,
		"strategy":              snapshot.Strategy,
		"excluded_slots":        cloneIntSlice(snapshot.ExcludedSlots),
		"excluded_days_of_week": cloneIntSlice(snapshot.ExcludedDaysOfWeek),
		"strategy_labels":       append([]string(nil), snapshot.StrategyLabels...),
	})
	if err != nil {
		return "", err
	}
	return string(raw), nil
}

func parseDatePtr(value string) (*time.Time, error) {
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

func cloneIntSlice(values []int) []int {
	if len(values) == 0 {
		return []int{}
	}
	return append([]int(nil), values...)
}
