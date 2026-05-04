package applyadapter

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/LoveLosita/smartflow/backend/conv"
	"github.com/LoveLosita/smartflow/backend/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// GormApplyAdapter 负责把主动调度确认后的变更写入正式 schedule 表。
//
// 职责边界：
// 1. 只写 schedule_events / schedules，并在事务内完成目标重校验与冲突重校验；
// 2. 不回写 active_schedule_previews，不发布 outbox，不调用 API/service/task；
// 3. 不创建 task_item，也不更新 task / task_items 状态，task_pool 是否已安排由 schedule_events 反查判断。
type GormApplyAdapter struct {
	db *gorm.DB
}

func NewGormApplyAdapter(db *gorm.DB) *GormApplyAdapter {
	return &GormApplyAdapter{db: db}
}

// ApplyActiveScheduleChanges 在单个数据库事务内应用主动调度变更。
//
// 事务语义：
// 1. 先规范化所有 change 的节次，并检查本次请求内部是否自相冲突；
// 2. 事务内锁定目标事实并重查 schedules 占用，任何冲突都直接返回 slot_conflict；
// 3. 所有 event 和 schedules 都成功插入后才提交；任一错误都会回滚，避免半写。
//
// 输入输出：
// 1. req.UserID / req.PreviewID / req.Changes 必须有效；
// 2. 返回的 AppliedEventIDs 是新建 schedule_events.id；
// 3. error 若为 *ApplyError，上游可按 Code 分类处理。
func (a *GormApplyAdapter) ApplyActiveScheduleChanges(ctx context.Context, req ApplyActiveScheduleRequest) (ApplyActiveScheduleResult, error) {
	if a == nil || a.db == nil {
		return ApplyActiveScheduleResult{}, newApplyError(ErrorCodeInvalidRequest, "主动调度 apply adapter 未初始化", nil)
	}
	normalized, err := normalizeRequest(req)
	if err != nil {
		return ApplyActiveScheduleResult{}, err
	}

	result := ApplyActiveScheduleResult{ApplyID: req.ApplyID}
	err = a.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		appliedEventIDs := make([]int, 0, len(normalized))
		appliedScheduleIDs := make([]int, 0)
		for _, change := range normalized {
			var eventIDs []int
			var scheduleIDs []int
			var applyErr error
			switch {
			case isAddTaskPoolChange(change):
				eventIDs, scheduleIDs, applyErr = a.applyTaskPoolChange(ctx, tx, req, change)
			case isCreateMakeupChange(change):
				eventIDs, scheduleIDs, applyErr = a.applyMakeupChange(ctx, tx, req, change)
			default:
				applyErr = newApplyError(ErrorCodeUnsupportedChangeType, fmt.Sprintf("不支持的主动调度变更类型：%s", change.ChangeType), nil)
			}
			if applyErr != nil {
				return applyErr
			}
			appliedEventIDs = append(appliedEventIDs, eventIDs...)
			appliedScheduleIDs = append(appliedScheduleIDs, scheduleIDs...)
		}
		result.AppliedEventIDs = appliedEventIDs
		result.AppliedScheduleIDs = appliedScheduleIDs
		return nil
	})
	if err != nil {
		return ApplyActiveScheduleResult{}, classifyDBError(err)
	}
	return result, nil
}

func (a *GormApplyAdapter) applyTaskPoolChange(ctx context.Context, tx *gorm.DB, req ApplyActiveScheduleRequest, change normalizedChange) ([]int, []int, error) {
	targetID := change.TargetID
	if change.TargetType != "" && change.TargetType != TargetTypeTaskPool {
		return nil, nil, newApplyError(ErrorCodeInvalidEditedChanges, "add_task_pool_to_schedule 只能写入 task_pool 目标", nil)
	}

	// 调用目的：锁住同一个 task_pool 任务，串行化“是否已经进入日程”的判断，避免并发确认写出重复任务块。
	task, err := lockTaskPool(ctx, tx, req.UserID, targetID)
	if err != nil {
		return nil, nil, err
	}
	if task.IsCompleted {
		return nil, nil, newApplyError(ErrorCodeTargetCompleted, "task_pool 任务已完成，不能再加入日程", nil)
	}
	if err := ensureTaskPoolNotScheduled(ctx, tx, req.UserID, task.ID); err != nil {
		return nil, nil, err
	}
	if err := ensureSlotsFree(ctx, tx, req.UserID, change); err != nil {
		return nil, nil, err
	}

	eventName := strings.TrimSpace(task.Title)
	if eventName == "" {
		eventName = fmt.Sprintf("任务 %d", task.ID)
	}
	relID := task.ID
	return insertTaskEventWithSchedules(ctx, tx, req, change, eventPayload{
		Name:           eventName,
		TaskSourceType: TaskSourceTypeTaskPool,
		RelID:          relID,
		Sections:       change.Sections,
	})
}

func (a *GormApplyAdapter) applyMakeupChange(ctx context.Context, tx *gorm.DB, req ApplyActiveScheduleRequest, change normalizedChange) ([]int, []int, error) {
	target, err := resolveMakeupTarget(ctx, tx, req.UserID, change)
	if err != nil {
		return nil, nil, err
	}
	if err := ensureSlotsFree(ctx, tx, req.UserID, change); err != nil {
		return nil, nil, err
	}

	return insertTaskEventWithSchedules(ctx, tx, req, change, eventPayload{
		Name:             target.Name,
		TaskSourceType:   target.TaskSourceType,
		RelID:            target.RelID,
		MakeupForEventID: &target.MakeupForEventID,
		Sections:         change.Sections,
	})
}

type normalizedChange struct {
	ApplyChange
	Week      int
	DayOfWeek int
	Sections  []int
}

func normalizeRequest(req ApplyActiveScheduleRequest) ([]normalizedChange, error) {
	if req.UserID <= 0 {
		return nil, newApplyError(ErrorCodeInvalidRequest, "user_id 不能为空", nil)
	}
	if strings.TrimSpace(req.PreviewID) == "" {
		return nil, newApplyError(ErrorCodeInvalidRequest, "preview_id 不能为空", nil)
	}
	if len(req.Changes) == 0 {
		return nil, newApplyError(ErrorCodeInvalidRequest, "changes 不能为空", nil)
	}

	seenSlots := make(map[string]struct{})
	normalized := make([]normalizedChange, 0, len(req.Changes))
	for _, change := range req.Changes {
		sections, err := normalizeSections(change)
		if err != nil {
			return nil, err
		}
		for _, section := range sections {
			key := fmt.Sprintf("%d:%d:%d", change.ToSlot.Start.Week, change.ToSlot.Start.DayOfWeek, section)
			if _, exists := seenSlots[key]; exists {
				return nil, newApplyError(ErrorCodeSlotConflict, "本次确认请求内部存在重复节次", nil)
			}
			seenSlots[key] = struct{}{}
		}
		normalized = append(normalized, normalizedChange{
			ApplyChange: change,
			Week:        change.ToSlot.Start.Week,
			DayOfWeek:   change.ToSlot.Start.DayOfWeek,
			Sections:    sections,
		})
	}
	return normalized, nil
}

func normalizeSections(change ApplyChange) ([]int, error) {
	if change.TargetID <= 0 {
		return nil, newApplyError(ErrorCodeInvalidEditedChanges, "变更目标 ID 不能为空", nil)
	}
	if change.ToSlot == nil {
		return nil, newApplyError(ErrorCodeInvalidEditedChanges, "变更缺少目标节次", nil)
	}
	start := change.ToSlot.Start
	end := change.ToSlot.End
	if start.Week <= 0 || start.DayOfWeek < 1 || start.DayOfWeek > 7 || start.Section < 1 || start.Section > 12 {
		return nil, newApplyError(ErrorCodeInvalidEditedChanges, "目标起始节次不合法", nil)
	}
	duration := change.DurationSections
	if duration <= 0 {
		duration = change.ToSlot.DurationSections
	}
	if end.Section <= 0 && duration > 0 {
		end = Slot{Week: start.Week, DayOfWeek: start.DayOfWeek, Section: start.Section + duration - 1}
	}
	if end.Week <= 0 && end.DayOfWeek <= 0 && end.Section <= 0 {
		end = start
	}
	if end.Week != start.Week || end.DayOfWeek != start.DayOfWeek || end.Section < start.Section {
		return nil, newApplyError(ErrorCodeInvalidEditedChanges, "目标节次必须是同一天内的连续区间", nil)
	}
	if end.Section > 12 {
		return nil, newApplyError(ErrorCodeInvalidEditedChanges, "目标结束节次不合法", nil)
	}
	actualDuration := end.Section - start.Section + 1
	if duration > 0 && duration != actualDuration {
		return nil, newApplyError(ErrorCodeInvalidEditedChanges, "duration_sections 与目标节次跨度不一致", nil)
	}
	sections := make([]int, 0, actualDuration)
	for section := start.Section; section <= end.Section; section++ {
		sections = append(sections, section)
	}
	return sections, nil
}

func isAddTaskPoolChange(change normalizedChange) bool {
	if change.ChangeType == ChangeTypeAddTaskPoolToSchedule {
		return true
	}
	return change.ChangeType == changeTypeAdd && change.TargetType == TargetTypeTaskPool
}

func isCreateMakeupChange(change normalizedChange) bool {
	return change.ChangeType == ChangeTypeCreateMakeup
}

func lockTaskPool(ctx context.Context, tx *gorm.DB, userID, taskID int) (model.Task, error) {
	var task model.Task
	err := tx.WithContext(ctx).
		Clauses(clause.Locking{Strength: "UPDATE"}).
		Where("id = ? AND user_id = ?", taskID, userID).
		First(&task).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return model.Task{}, newApplyError(ErrorCodeTargetNotFound, "task_pool 任务不存在或不属于当前用户", nil)
		}
		return model.Task{}, newApplyError(ErrorCodeDBError, "读取 task_pool 任务失败", err)
	}
	return task, nil
}

func ensureTaskPoolNotScheduled(ctx context.Context, tx *gorm.DB, userID, taskID int) error {
	var count int64
	err := tx.WithContext(ctx).
		Model(&model.ScheduleEvent{}).
		Where("user_id = ? AND type = ? AND task_source_type = ? AND rel_id = ?", userID, scheduleEventTypeTask, TaskSourceTypeTaskPool, taskID).
		Count(&count).Error
	if err != nil {
		return newApplyError(ErrorCodeDBError, "检查 task_pool 是否已进入日程失败", err)
	}
	if count > 0 {
		return newApplyError(ErrorCodeTargetAlreadyScheduled, "task_pool 任务已进入日程", nil)
	}
	return nil
}

func ensureSlotsFree(ctx context.Context, tx *gorm.DB, userID int, change normalizedChange) error {
	sections := change.Sections
	if len(sections) == 0 {
		return newApplyError(ErrorCodeInvalidEditedChanges, "目标节次不能为空", nil)
	}
	sort.Ints(sections)
	startSection := sections[0]
	endSection := sections[len(sections)-1]

	// 1. 在事务内对目标节次加行锁，命中任何已有 schedules 都视为冲突。
	// 2. 若并发事务在检查后抢先插入同一唯一键，后续 Create 会被唯一索引兜底拦截并整体回滚。
	// 3. MVP 不处理课程嵌入，任何已有课程、固定日程或任务都不可覆盖。
	var occupied []model.Schedule
	err := tx.WithContext(ctx).
		Model(&model.Schedule{}).
		Clauses(clause.Locking{Strength: "UPDATE"}).
		Where("user_id = ? AND week = ? AND day_of_week = ? AND section IN ?", userID, change.Week, change.DayOfWeek, sections).
		Find(&occupied).Error
	if err != nil {
		return newApplyError(ErrorCodeDBError, "检查目标节次冲突失败", err)
	}
	if len(occupied) > 0 {
		return newApplyError(ErrorCodeSlotConflict, fmt.Sprintf("第 %d-%d 节已被占用", startSection, endSection), nil)
	}
	return nil
}

type eventPayload struct {
	Name             string
	TaskSourceType   string
	RelID            int
	MakeupForEventID *int
	Sections         []int
}

func insertTaskEventWithSchedules(ctx context.Context, tx *gorm.DB, req ApplyActiveScheduleRequest, change normalizedChange, payload eventPayload) ([]int, []int, error) {
	sections := append([]int(nil), payload.Sections...)
	sort.Ints(sections)
	start := sections[0]
	end := sections[len(sections)-1]
	startTime, endTime, err := conv.RelativeTimeToRealTime(change.Week, change.DayOfWeek, start, end)
	if err != nil {
		return nil, nil, newApplyError(ErrorCodeInvalidEditedChanges, "目标节次无法转换为绝对时间", err)
	}

	previewID := strings.TrimSpace(req.PreviewID)
	event := model.ScheduleEvent{
		UserID:           req.UserID,
		Name:             payload.Name,
		Type:             scheduleEventTypeTask,
		TaskSourceType:   payload.TaskSourceType,
		RelID:            &payload.RelID,
		MakeupForEventID: payload.MakeupForEventID,
		ActivePreviewID:  &previewID,
		CanBeEmbedded:    false,
		StartTime:        startTime,
		EndTime:          endTime,
	}
	if err := tx.WithContext(ctx).Create(&event).Error; err != nil {
		return nil, nil, newApplyError(ErrorCodeDBError, "写入 schedule_events 失败", err)
	}

	schedules := make([]model.Schedule, 0, len(sections))
	for _, section := range sections {
		schedules = append(schedules, model.Schedule{
			EventID:   event.ID,
			UserID:    req.UserID,
			Week:      change.Week,
			DayOfWeek: change.DayOfWeek,
			Section:   section,
			Status:    scheduleStatusNormal,
		})
	}
	if err := tx.WithContext(ctx).Create(&schedules).Error; err != nil {
		return nil, nil, newApplyError(ErrorCodeDBError, "写入 schedules 失败", err)
	}

	scheduleIDs := make([]int, 0, len(schedules))
	for _, schedule := range schedules {
		scheduleIDs = append(scheduleIDs, schedule.ID)
	}
	return []int{event.ID}, scheduleIDs, nil
}

type makeupTarget struct {
	Name             string
	TaskSourceType   string
	RelID            int
	MakeupForEventID int
}

func resolveMakeupTarget(ctx context.Context, tx *gorm.DB, userID int, change normalizedChange) (makeupTarget, error) {
	makeupForEventID := parsePositiveInt(change.Metadata["makeup_for_event_id"])
	if change.TargetType == "" || change.TargetType == TargetTypeScheduleEvent {
		if change.TargetID > 0 {
			makeupForEventID = change.TargetID
		}
		return resolveMakeupFromEvent(ctx, tx, userID, makeupForEventID)
	}
	if makeupForEventID <= 0 {
		return makeupTarget{}, newApplyError(ErrorCodeInvalidEditedChanges, "create_makeup 必须提供 makeup_for_event_id", nil)
	}
	if _, err := lockScheduleEvent(ctx, tx, userID, makeupForEventID); err != nil {
		return makeupTarget{}, err
	}

	switch change.TargetType {
	case TargetTypeTaskPool:
		task, err := lockTaskPool(ctx, tx, userID, change.TargetID)
		if err != nil {
			return makeupTarget{}, err
		}
		if task.IsCompleted {
			return makeupTarget{}, newApplyError(ErrorCodeTargetCompleted, "补做目标 task_pool 已完成", nil)
		}
		return makeupTarget{
			Name:             nonEmpty(task.Title, fmt.Sprintf("任务 %d", task.ID)),
			TaskSourceType:   TaskSourceTypeTaskPool,
			RelID:            task.ID,
			MakeupForEventID: makeupForEventID,
		}, nil
	case TargetTypeTaskItem:
		item, err := lockTaskItemForUser(ctx, tx, userID, change.TargetID)
		if err != nil {
			return makeupTarget{}, err
		}
		return makeupTarget{
			Name:             nonEmpty(stringPtrValue(item.Content), fmt.Sprintf("任务块 %d", item.ID)),
			TaskSourceType:   TaskSourceTypeTaskItem,
			RelID:            item.ID,
			MakeupForEventID: makeupForEventID,
		}, nil
	default:
		return makeupTarget{}, newApplyError(ErrorCodeInvalidEditedChanges, "create_makeup 目标类型不合法", nil)
	}
}

func resolveMakeupFromEvent(ctx context.Context, tx *gorm.DB, userID, eventID int) (makeupTarget, error) {
	event, err := lockScheduleEvent(ctx, tx, userID, eventID)
	if err != nil {
		return makeupTarget{}, err
	}
	if event.Type != scheduleEventTypeTask || event.RelID == nil || *event.RelID <= 0 {
		return makeupTarget{}, newApplyError(ErrorCodeInvalidEditedChanges, "补做来源必须是已排任务日程", nil)
	}
	sourceType := event.TaskSourceType
	if sourceType == "" {
		sourceType = TaskSourceTypeTaskItem
	}
	if sourceType != TaskSourceTypeTaskItem && sourceType != TaskSourceTypeTaskPool {
		return makeupTarget{}, newApplyError(ErrorCodeInvalidEditedChanges, "补做来源任务类型不合法", nil)
	}
	return makeupTarget{
		Name:             nonEmpty(event.Name, fmt.Sprintf("补做任务 %d", event.ID)),
		TaskSourceType:   sourceType,
		RelID:            *event.RelID,
		MakeupForEventID: event.ID,
	}, nil
}

func lockScheduleEvent(ctx context.Context, tx *gorm.DB, userID, eventID int) (model.ScheduleEvent, error) {
	if eventID <= 0 {
		return model.ScheduleEvent{}, newApplyError(ErrorCodeInvalidEditedChanges, "makeup_for_event_id 不能为空", nil)
	}
	var event model.ScheduleEvent
	err := tx.WithContext(ctx).
		Clauses(clause.Locking{Strength: "UPDATE"}).
		Where("id = ? AND user_id = ?", eventID, userID).
		First(&event).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return model.ScheduleEvent{}, newApplyError(ErrorCodeTargetNotFound, "补做来源日程不存在或不属于当前用户", nil)
		}
		return model.ScheduleEvent{}, newApplyError(ErrorCodeDBError, "读取补做来源日程失败", err)
	}
	return event, nil
}

func lockTaskItemForUser(ctx context.Context, tx *gorm.DB, userID, taskItemID int) (model.TaskClassItem, error) {
	var item model.TaskClassItem
	err := tx.WithContext(ctx).
		Table("task_items").
		Select("task_items.*").
		Joins("JOIN task_classes ON task_classes.id = task_items.category_id").
		Clauses(clause.Locking{Strength: "UPDATE"}).
		Where("task_items.id = ? AND task_classes.user_id = ?", taskItemID, userID).
		First(&item).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return model.TaskClassItem{}, newApplyError(ErrorCodeTargetNotFound, "task_item 不存在或不属于当前用户", nil)
		}
		return model.TaskClassItem{}, newApplyError(ErrorCodeDBError, "读取 task_item 失败", err)
	}
	return item, nil
}

func parsePositiveInt(value string) int {
	parsed, err := strconv.Atoi(strings.TrimSpace(value))
	if err != nil || parsed <= 0 {
		return 0
	}
	return parsed
}

func nonEmpty(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return strings.TrimSpace(value)
}

func stringPtrValue(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func classifyDBError(err error) error {
	if err == nil {
		return nil
	}
	var applyErr *ApplyError
	if errors.As(err, &applyErr) {
		return applyErr
	}
	message := strings.ToLower(err.Error())
	if strings.Contains(message, "duplicate entry") ||
		strings.Contains(message, "unique constraint") ||
		strings.Contains(message, "unique violation") ||
		strings.Contains(message, "idx_user_slot_atomic") {
		return newApplyError(ErrorCodeSlotConflict, "目标节次已被其他日程占用", err)
	}
	return newApplyError(ErrorCodeDBError, "主动调度正式写库失败", err)
}
