package adapter

import (
	"context"
	"encoding/json"
	"errors"
	"sort"
	"strings"
	"time"

	legacydao "github.com/LoveLosita/smartflow/backend/dao"
	legacymodel "github.com/LoveLosita/smartflow/backend/model"
	"github.com/LoveLosita/smartflow/backend/respond"
	forumsv "github.com/LoveLosita/smartflow/backend/services/taskclassforum/sv"
	"gorm.io/gorm"
)

const legacyTaskClassDateLayout = "2006-01-02"

var errLegacyTaskClassAdapterNotReady = errors.New("taskclassforum legacy taskclass adapter is not initialized")

// LegacyTaskClassAdapter 负责把旧 task-class DAO 适配成计划广场需要的快照端口。
//
// 职责边界：
// 1. 只复用旧 TaskClassDAO 读写 task_classes / task_items；
// 2. 只产出/消费 TaskClass 白名单快照，不透传 embedded_time 和任何 schedule 绑定；
// 3. 不承载论坛帖子、模板、导入记录事务，这些仍由 taskclassforum service 编排。
type LegacyTaskClassAdapter struct {
	taskClassDAO *legacydao.TaskClassDAO
}

var _ forumsv.TaskClassSnapshotPort = (*LegacyTaskClassAdapter)(nil)

// NewLegacyTaskClassAdapter 创建 legacy TaskClass 适配器。
//
// 职责边界：
// 1. 只做依赖注入，不主动探活数据库；
// 2. 不创建 DAO 以外的额外资源；
// 3. 若传入 nil，真正报错延后到方法调用时返回，便于上层统一做依赖检查。
func NewLegacyTaskClassAdapter(taskClassDAO *legacydao.TaskClassDAO) *LegacyTaskClassAdapter {
	return &LegacyTaskClassAdapter{
		taskClassDAO: taskClassDAO,
	}
}

// GetOwnedTaskClassSnapshot 读取当前用户自己的旧 TaskClass，并投影为论坛可分享快照。
//
// 职责边界：
// 1. 只读取 user_id 归属下的单个 TaskClass；
// 2. 只返回白名单字段与条目 source id/order/content；
// 3. 不返回 embedded_time、schedule 绑定和其他用户私有排程状态。
func (a *LegacyTaskClassAdapter) GetOwnedTaskClassSnapshot(ctx context.Context, userID uint64, taskClassID uint64) (*forumsv.TaskClassSnapshot, error) {
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

	legacyTaskClass, err := a.taskClassDAO.GetCompleteTaskClassByID(ctx, taskClassIDInt, userIDInt)
	if err != nil {
		return nil, normalizeLegacyTaskClassLookupError(err)
	}
	if legacyTaskClass == nil {
		return nil, respond.UserTaskClassNotFound
	}

	snapshot, err := snapshotFromLegacyTaskClass(*legacyTaskClass)
	if err != nil {
		return nil, err
	}
	return &snapshot, nil
}

// CreateTaskClassFromSnapshot 基于论坛模板快照为当前用户创建旧 TaskClass 副本。
//
// 职责边界：
// 1. 只创建 task_classes / task_items 副本，不写 forum_imports；
// 2. 只写白名单字段，所有新建 item 都强制重置为未安排状态；
// 3. 不保留原始 item ID，避免误触旧 DAO 的“更新已有记录”分支。
func (a *LegacyTaskClassAdapter) CreateTaskClassFromSnapshot(ctx context.Context, userID uint64, snapshot forumsv.TaskClassSnapshot, targetTitle string) (*forumsv.CreatedTaskClass, error) {
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

	startDate, endDate, err := parseSnapshotDateRange(snapshot.Mode, snapshot.StartDate, snapshot.EndDate)
	if err != nil {
		return nil, err
	}

	createTaskClass := buildLegacyTaskClassModel(title, snapshot, userIDInt, startDate, endDate)
	createItems := buildLegacyTaskClassItems(snapshot.Items)
	if len(createItems) == 0 {
		return nil, respond.MissingParam
	}

	created := &forumsv.CreatedTaskClass{
		Title: title,
	}

	// 1. 先在旧 DAO 事务里创建 task_class 主记录，拿到新主键。
	// 2. 再把所有快照条目改写成“当前用户的新副本条目”，统一挂到新主键下。
	// 3. 任一步失败都回滚，避免出现“有主表、没子项”的半写状态。
	if err := a.taskClassDAO.Transaction(func(txDAO *legacydao.TaskClassDAO) error {
		taskClassID, txErr := txDAO.AddOrUpdateTaskClass(userIDInt, createTaskClass)
		if txErr != nil {
			return txErr
		}

		for i := range createItems {
			createItems[i].CategoryID = intPtr(taskClassID)
		}
		if txErr := txDAO.AddOrUpdateTaskClassItems(userIDInt, createItems); txErr != nil {
			return txErr
		}

		created.TaskClassID = uint64(taskClassID)
		return nil
	}); err != nil {
		return nil, err
	}

	return created, nil
}

// snapshotFromLegacyTaskClass 把旧 TaskClass 模型转换成论坛白名单快照。
//
// 职责边界：
// 1. 负责字段投影与默认值归一化；
// 2. 负责过滤 embedded_time，只保留条目 source id/order/content；
// 3. 负责生成与论坛模板同口径的 ConfigSnapshotJSON。
func snapshotFromLegacyTaskClass(taskClass legacymodel.TaskClass) (forumsv.TaskClassSnapshot, error) {
	items := snapshotItemsFromLegacyItems(taskClass.Items)
	snapshot := forumsv.TaskClassSnapshot{
		TaskClassID:        uint64(taskClass.ID),
		Title:              stringValue(taskClass.Name),
		Mode:               stringValue(taskClass.Mode),
		StartDate:          formatDate(taskClass.StartDate),
		EndDate:            formatDate(taskClass.EndDate),
		SubjectType:        stringValue(taskClass.SubjectType),
		DifficultyLevel:    stringValue(taskClass.DifficultyLevel),
		CognitiveIntensity: stringValue(taskClass.CognitiveIntensity),
		TotalSlots:         intValue(taskClass.TotalSlots),
		AllowFillerCourse:  boolValue(taskClass.AllowFillerCourse),
		Strategy:           stringValue(taskClass.Strategy),
		ExcludedSlots:      cloneIntSlice([]int(taskClass.ExcludedSlots)),
		ExcludedDaysOfWeek: cloneIntSlice([]int(taskClass.ExcludedDaysOfWeek)),
		StrategyLabels:     legacyStrategyLabels(stringValue(taskClass.Strategy)),
		Items:              items,
	}

	configJSON, err := buildConfigSnapshotJSON(snapshot)
	if err != nil {
		return forumsv.TaskClassSnapshot{}, err
	}
	snapshot.ConfigSnapshotJSON = configJSON
	return snapshot, nil
}

// snapshotItemsFromLegacyItems 过滤旧 task_items 的可分享字段。
//
// 职责边界：
// 1. 只保留 source id、order、content；
// 2. 不复制 embedded_time、status 等用户私有排程状态；
// 3. 输出前按 order、source id 做稳定排序，保证论坛快照可重复。
func snapshotItemsFromLegacyItems(items []legacymodel.TaskClassItem) []forumsv.TaskClassSnapshotItem {
	if len(items) == 0 {
		return []forumsv.TaskClassSnapshotItem{}
	}

	sorted := append([]legacymodel.TaskClassItem(nil), items...)
	sort.SliceStable(sorted, func(i, j int) bool {
		leftOrder := intValue(sorted[i].Order)
		rightOrder := intValue(sorted[j].Order)
		if leftOrder != rightOrder {
			return leftOrder < rightOrder
		}
		return sorted[i].ID < sorted[j].ID
	})

	result := make([]forumsv.TaskClassSnapshotItem, 0, len(sorted))
	for _, item := range sorted {
		result = append(result, forumsv.TaskClassSnapshotItem{
			TaskItemID: uint64(item.ID),
			Order:      intValue(item.Order),
			Content:    stringValue(item.Content),
		})
	}
	return result
}

// buildLegacyTaskClassModel 把论坛快照转换成旧 task_classes 主表模型。
//
// 职责边界：
// 1. 只负责主表字段映射；
// 2. 不负责 items 生成；
// 3. 不负责事务提交，调用方必须交给 DAO.Transaction 执行。
func buildLegacyTaskClassModel(title string, snapshot forumsv.TaskClassSnapshot, userID int, startDate *time.Time, endDate *time.Time) *legacymodel.TaskClass {
	totalSlots := snapshot.TotalSlots
	allowFillerCourse := snapshot.AllowFillerCourse
	mode := strings.TrimSpace(snapshot.Mode)
	strategy := strings.TrimSpace(snapshot.Strategy)

	return &legacymodel.TaskClass{
		UserID:             intPtr(userID),
		Name:               stringPtr(strings.TrimSpace(title)),
		Mode:               stringPtr(mode),
		StartDate:          startDate,
		EndDate:            endDate,
		SubjectType:        optionalStringPtr(snapshot.SubjectType),
		DifficultyLevel:    optionalStringPtr(snapshot.DifficultyLevel),
		CognitiveIntensity: optionalStringPtr(snapshot.CognitiveIntensity),
		TotalSlots:         &totalSlots,
		AllowFillerCourse:  &allowFillerCourse,
		Strategy:           optionalStringPtr(strategy),
		ExcludedSlots:      legacymodel.IntSlice(cloneIntSlice(snapshot.ExcludedSlots)),
		ExcludedDaysOfWeek: legacymodel.IntSlice(cloneIntSlice(snapshot.ExcludedDaysOfWeek)),
	}
}

// buildLegacyTaskClassItems 把论坛快照条目改写成旧 task_items 待创建模型。
//
// 职责边界：
// 1. 只构造“新建 item”模型，因此 ID 固定为 0；
// 2. 强制清空 EmbeddedTime，并把状态写成未安排；
// 3. 跳过纯空白内容，避免把无意义条目写回旧表。
func buildLegacyTaskClassItems(snapshotItems []forumsv.TaskClassSnapshotItem) []legacymodel.TaskClassItem {
	if len(snapshotItems) == 0 {
		return []legacymodel.TaskClassItem{}
	}

	sorted := append([]forumsv.TaskClassSnapshotItem(nil), snapshotItems...)
	sort.SliceStable(sorted, func(i, j int) bool {
		if sorted[i].Order != sorted[j].Order {
			return sorted[i].Order < sorted[j].Order
		}
		return sorted[i].TaskItemID < sorted[j].TaskItemID
	})

	result := make([]legacymodel.TaskClassItem, 0, len(sorted))
	for _, item := range sorted {
		if strings.TrimSpace(item.Content) == "" {
			continue
		}

		order := item.Order
		content := item.Content
		status := legacymodel.TaskItemStatusUnscheduled

		result = append(result, legacymodel.TaskClassItem{
			Order:        &order,
			Content:      &content,
			EmbeddedTime: nil,
			Status:       &status,
		})
	}
	return result
}

// parseSnapshotDateRange 解析论坛快照中的日期范围。
//
// 职责边界：
// 1. 只负责 2006-01-02 格式解析；
// 2. 只在 mode=auto 时执行起止日期必填和先后顺序校验；
// 3. 不负责校验节次、星期等其他业务规则。
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

// buildConfigSnapshotJSON 生成论坛模板沿用的配置白名单 JSON。
//
// 职责边界：
// 1. 只序列化配置白名单字段；
// 2. 不写 items、embedded_time、schedule 相关数据；
// 3. 输出键名保持和 taskclassforum 发布链路一致，避免模板口径漂移。
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
		"strategy_labels":       cloneStringSlice(snapshot.StrategyLabels),
	})
	if err != nil {
		return "", err
	}
	return string(raw), nil
}

func (a *LegacyTaskClassAdapter) ensureReady() error {
	if a == nil || a.taskClassDAO == nil {
		return errLegacyTaskClassAdapterNotReady
	}
	return nil
}

func normalizeLegacyTaskClassLookupError(err error) error {
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return respond.UserTaskClassNotFound
	}
	return err
}

func parseDatePtr(value string) (*time.Time, error) {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return nil, nil
	}
	parsed, err := time.ParseInLocation(legacyTaskClassDateLayout, trimmed, time.Local)
	if err != nil {
		return nil, err
	}
	return &parsed, nil
}

func formatDate(value *time.Time) string {
	if value == nil || value.IsZero() {
		return ""
	}
	return value.Format(legacyTaskClassDateLayout)
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

func stringValue(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func intValue(value *int) int {
	if value == nil {
		return 0
	}
	return *value
}

func boolValue(value *bool) bool {
	if value == nil {
		return true
	}
	return *value
}

func legacyStrategyLabels(strategy string) []string {
	trimmed := strings.TrimSpace(strategy)
	if trimmed == "" {
		return []string{}
	}
	return []string{trimmed}
}

func stringPtr(value string) *string {
	return &value
}

func intPtr(value int) *int {
	return &value
}

func optionalStringPtr(value string) *string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return nil
	}
	return &trimmed
}

func cloneIntSlice(values []int) []int {
	if len(values) == 0 {
		return []int{}
	}
	return append([]int(nil), values...)
}

func cloneStringSlice(values []string) []string {
	if len(values) == 0 {
		return []string{}
	}
	return append([]string(nil), values...)
}
