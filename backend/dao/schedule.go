package dao

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/LoveLosita/smartflow/backend/model"
	"github.com/LoveLosita/smartflow/backend/respond"
	"gorm.io/gorm"
)

type ScheduleDAO struct {
	db *gorm.DB
}

// NewScheduleDAO 创建TaskClassDAO实例
func NewScheduleDAO(db *gorm.DB) *ScheduleDAO {
	return &ScheduleDAO{
		db: db,
	}
}

func (d *ScheduleDAO) WithTx(tx *gorm.DB) *ScheduleDAO {
	return &ScheduleDAO{db: tx}
}

func (d *ScheduleDAO) AddSchedules(schedules []model.Schedule) ([]int, error) {
	if err := d.db.Create(&schedules).Error; err != nil {
		return nil, err
	}
	ids := make([]int, len(schedules))
	for i, s := range schedules {
		ids[i] = s.ID
	}
	return ids, nil
}

func (d *ScheduleDAO) EmbedTaskIntoSchedule(startSection, endSection, dayOfWeek, week, userID, taskID int) error {
	// 仅更新指定：用户/周/星期/节次区间 的记录，将 embedded_task_id 精准写入 taskID
	res := d.db.
		Table("schedules").
		Where("user_id = ? AND week = ? AND day_of_week = ? AND section BETWEEN ? AND ?", userID, week, dayOfWeek, startSection, endSection).
		Update("embedded_task_id", taskID)

	return res.Error
}

func (d *ScheduleDAO) GetCourseUserIDByID(ctx context.Context, courseScheduleEventID int) (int, error) {
	type row struct {
		UserID *int `gorm:"column:user_id"`
	}

	var r row
	err := d.db.WithContext(ctx).
		Table("schedule_events").
		Select("user_id").
		Where("id = ?", courseScheduleEventID).
		First(&r).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return 0, respond.WrongCourseID
		}
		return 0, err
	}
	if r.UserID == nil {
		return 0, respond.WrongCourseID
	}
	return *r.UserID, nil
}

// IsCourseEmbeddedByOtherTaskBlock 判断课程在给定节次区间内是否已被其他任务块嵌入（用于业务限制）
func (d *ScheduleDAO) IsCourseEmbeddedByOtherTaskBlock(ctx context.Context, courseID, startSection, endSection int) (bool, error) {
	// 若区间非法，视为不冲突
	if startSection <= 0 || endSection <= 0 || startSection > endSection {
		return false, nil
	}

	var cnt int64
	err := d.db.WithContext(ctx).
		Table("schedules").
		Where("id = ?", courseID).
		Where("section BETWEEN ? AND ?", startSection, endSection).
		Where("embedded_task_id IS NOT NULL AND embedded_task_id <> 0").
		Count(&cnt).Error
	if err != nil {
		return false, err
	}
	return cnt > 0, nil
}

func (d *ScheduleDAO) HasUserScheduleConflict(ctx context.Context, userID, week, dayOfWeek int, sections []int) (bool, error) {
	// 无节次则视为无冲突
	if len(sections) == 0 {
		return false, nil
	}
	// 统计同一用户、同一周、同一天、且节次有交集的排程数量
	// 约定表字段：user_id, week, day_of_week, section
	var cnt int64
	err := d.db.WithContext(ctx).
		Table("schedules").
		Where("user_id = ? AND week = ? AND day_of_week = ?", userID, week, dayOfWeek).
		Where("section IN ?", sections).
		Count(&cnt).Error
	if err != nil {
		return false, err
	}
	return cnt > 0, nil
}

func (d *ScheduleDAO) IsCourseTimeMatch(ctx context.Context, courseScheduleEventID, week, dayOfWeek, startSection, endSection int) (bool, error) {
	// 区间非法直接不匹配
	if startSection <= 0 || endSection <= 0 || startSection > endSection {
		return false, nil
	}

	// 核对该课程事件在指定 周\+星期 下，是否存在覆盖整个节次区间的排程记录
	// 说明：此处按你当前表结构的用法（schedule\_events 存事件，schedules 存节次明细）来写：
	// schedules 里通过 schedule\_event\_id 关联到 schedule\_events.id
	var cnt int64
	err := d.db.WithContext(ctx).
		Table("schedules").
		Where("event_id = ?", courseScheduleEventID).
		Where("week = ? AND day_of_week = ?", week, dayOfWeek).
		Where("section BETWEEN ? AND ?", startSection, endSection).
		Count(&cnt).Error
	if err != nil {
		return false, err
	}

	// 需要区间内的每一节都存在记录才算匹配
	return cnt == int64(endSection-startSection+1), nil
}

func (d *ScheduleDAO) AddScheduleEvent(scheduleEvent *model.ScheduleEvent) (int, error) {
	if err := d.db.Create(&scheduleEvent).Error; err != nil {
		return 0, err
	}
	return scheduleEvent.ID, nil
}

// CheckScheduleConflict 检查给定的 Schedule 切片中是否存在课程的冲突（即同一用户、同一周、同一天、且节次有交集的记录，并且只管课程，不管其它任务类型）
func (d *ScheduleDAO) CheckScheduleConflict(ctx context.Context, schedules []model.Schedule) (bool, error) {
	if len(schedules) == 0 {
		return false, nil
	}

	// 聚合：同一 user/week/day 的节次去重后一次性查库
	type key struct {
		UserID    int
		Week      int
		DayOfWeek int
	}
	groups := make(map[key]map[int]struct{})

	for _, s := range schedules {
		// 基础字段不合法直接跳过（按不冲突处理）
		if s.UserID <= 0 || s.Week <= 0 || s.DayOfWeek <= 0 || s.Section <= 0 {
			continue
		}
		k := key{UserID: s.UserID, Week: s.Week, DayOfWeek: s.DayOfWeek}
		if _, ok := groups[k]; !ok {
			groups[k] = make(map[int]struct{})
		}
		groups[k][s.Section] = struct{}{}
	}

	for k, set := range groups {
		if len(set) == 0 {
			continue
		}

		sections := make([]int, 0, len(set))
		for sec := range set {
			sections = append(sections, sec)
		}

		// 仅判断“课程（type=course）”是否冲突：
		// schedules.event_id -> schedule_events.id，再用 schedule_events.type 过滤
		var cnt int64
		err := d.db.WithContext(ctx).
			Table("schedules s").
			Joins("JOIN schedule_events e ON e.id = s.event_id").
			Where("s.user_id = ? AND s.week = ? AND s.day_of_week = ?", k.UserID, k.Week, k.DayOfWeek).
			Where("s.section IN ?", sections).
			Where("e.type = ?", "course").
			Count(&cnt).Error
		if err != nil {
			return false, err
		}
		if cnt > 0 {
			return true, nil
		}
	}

	return false, nil
}

func (d *ScheduleDAO) GetNonCourseScheduleConflicts(ctx context.Context, newSchedules []model.Schedule) ([]model.Schedule, error) {
	if len(newSchedules) == 0 {
		return nil, nil
	}

	// 1. 构建指纹图：用于快速比对坐标
	userID := newSchedules[0].UserID
	weeksMap := make(map[int]bool)
	newSlotsFingerprints := make(map[string]bool)

	for _, s := range newSchedules {
		weeksMap[s.Week] = true
		key := fmt.Sprintf("%d-%d-%d", s.Week, s.DayOfWeek, s.Section)
		newSlotsFingerprints[key] = true
	}

	weeks := make([]int, 0, len(weeksMap))
	for w := range weeksMap {
		weeks = append(weeks, w)
	}

	// 2. 第一步：定义一个临时小结构体，精准捞取坐标和 EventID
	type simpleSlot struct {
		EventID   int
		Week      int
		DayOfWeek int
		Section   int
	}
	var candidates []simpleSlot

	// 💡 这里的逻辑：只查索引覆盖到的字段，速度极快
	err := d.db.WithContext(ctx).
		Table("schedules").
		Select("schedules.event_id, schedules.week, schedules.day_of_week, schedules.section").
		Joins("JOIN schedule_events ON schedule_events.id = schedules.event_id").
		Where("schedules.user_id = ? AND schedules.week IN ? AND schedule_events.type != ?", userID, weeks, "course").
		Scan(&candidates).Error

	if err != nil {
		return nil, err
	}

	// 3. 筛选出真正碰撞的 EventID
	eventIDMap := make(map[int]bool)
	for _, s := range candidates {
		key := fmt.Sprintf("%d-%d-%d", s.Week, s.DayOfWeek, s.Section)
		if newSlotsFingerprints[key] {
			eventIDMap[s.EventID] = true
		}
	}

	if len(eventIDMap) == 0 {
		return nil, nil
	}

	// 4. 第二步：“抄全家”——根据碰撞到的 ID 捞出这些任务的所有原子槽位
	var ids []int
	for id := range eventIDMap {
		ids = append(ids, id)
	}

	var fullConflicts []model.Schedule
	// 💡 关键：这里必须 Preload("Event")，这样 DTO 才有名称显示
	err = d.db.WithContext(ctx).
		Preload("Event").
		Where("event_id IN ?", ids).
		Find(&fullConflicts).Error

	return fullConflicts, err
}
func (d *ScheduleDAO) GetUserTodaySchedule(ctx context.Context, userID, week, dayOfWeek int) ([]model.Schedule, error) {
	var schedules []model.Schedule

	// 1. Preload("Event"): 拿到课程/任务的基础信息（名、地、型）
	// 2. Preload("EmbeddedTask"): 拿到“水课”里嵌入的具体任务详情
	err := d.db.WithContext(ctx).
		Preload("Event").
		Preload("EmbeddedTask").
		Where("user_id = ? AND week = ? AND day_of_week = ?", userID, week, dayOfWeek).
		Order("section ASC").
		Find(&schedules).Error

	if err != nil {
		return nil, err
	}

	return schedules, nil
}

func (d *ScheduleDAO) GetUserWeeklySchedule(ctx context.Context, userID, week int) ([]model.Schedule, error) {
	var schedules []model.Schedule

	err := d.db.WithContext(ctx).
		Preload("Event").
		Preload("EmbeddedTask").
		Where("user_id = ? AND week = ?", userID, week).
		Order("day_of_week ASC, section ASC").
		Find(&schedules).Error

	if err != nil {
		return nil, err
	}

	return schedules, nil
}

func (d *ScheduleDAO) DeleteScheduleEventAndSchedule(ctx context.Context, eventID int, userID int) error {
	return d.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// 先查出要删除的 schedules，让 GORM 在 Delete 时能带上模型字段（供钩子读取 UserID/Week）
		var schedules []model.Schedule
		if err := tx.
			Where("event_id = ? AND user_id = ?", eventID, userID).
			Find(&schedules).Error; err != nil {
			return err
		}

		// 显式删子表 schedules（触发 schedules 的 GORM Delete 回调/插件）
		if len(schedules) > 0 {
			if err := tx.Delete(&schedules).Error; err != nil {
				return err
			}
		}

		// 再删父表 schedule_events（同样触发回调/插件）
		res := tx.Where("id = ? AND user_id = ?", eventID, userID).
			Delete(&model.ScheduleEvent{})
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected == 0 {
			return respond.WrongScheduleEventID
		}
		return nil
	})
}

func (d *ScheduleDAO) GetScheduleTypeByEventID(ctx context.Context, eventID, userID int) (string, error) {
	type row struct {
		Type *string `gorm:"column:type"`
	}
	var r row
	err := d.db.WithContext(ctx).
		Table("schedule_events").
		Select("type").
		Where("id = ? AND user_id=?", eventID, userID).
		First(&r).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return "", respond.WrongScheduleEventID // 事件不存在或不属于该用户，统一返回错误
		}
		return "", err
	}
	if r.Type == nil {
		return "", respond.WrongScheduleEventID
	}
	return *r.Type, nil
}

func (d *ScheduleDAO) GetScheduleEmbeddedTaskID(ctx context.Context, eventID int) (int, error) {
	// embedded_task_id 存在于 schedules 表中（按 event_id 聚合取一个非空值）
	// 若该事件没有任何嵌入任务，则返回 0, nil
	type row struct {
		EmbeddedTaskID *int `gorm:"column:embedded_task_id"`
	}

	var r row
	err := d.db.WithContext(ctx).
		Table("schedules").
		Select("embedded_task_id").
		Where("event_id = ?", eventID).
		Where("embedded_task_id IS NOT NULL AND embedded_task_id <> 0").
		Order("id ASC").
		Limit(1).
		Scan(&r).Error
	if err != nil {
		return 0, err
	}
	if r.EmbeddedTaskID == nil { // 没有任何嵌入任务
		return 0, nil
	}
	return *r.EmbeddedTaskID, nil
}

func (d *ScheduleDAO) IfScheduleEventIDExists(ctx context.Context, eventID int) (bool, error) {
	var count int64
	err := d.db.WithContext(ctx).
		Table("schedule_events").
		Where("id = ?", eventID).
		Count(&count).Error
	if err != nil {
		return false, err
	}
	return count > 0, nil
}

func (d *ScheduleDAO) SetScheduleEmbeddedTaskIDToNull(ctx context.Context, eventID int) (int, error) {
	// 先取出该事件当前嵌入的任务 id（若没有嵌入则返回对应业务错误）
	embeddedTaskID, err := d.GetScheduleEmbeddedTaskID(ctx, eventID)
	if err != nil {
		return 0, err
	}
	if embeddedTaskID == 0 {
		return 0, respond.TargetScheduleNotHaveEmbeddedTask
	}

	// 将 schedules 表中指定 event_id 的 embedded_task_id 字段置空（用于解除嵌入关系）
	res := d.db.WithContext(ctx).
		Table("schedules").
		Where("event_id = ?", eventID).
		Where("embedded_task_id IS NOT NULL AND embedded_task_id <> 0").
		Update("embedded_task_id", nil)
	if res.Error != nil {
		return 0, res.Error
	}
	if res.RowsAffected == 0 {
		return 0, respond.TargetScheduleNotHaveEmbeddedTask
	}
	return embeddedTaskID, nil
}

func (d *ScheduleDAO) FindEmbeddedTaskIDAndDeleteIt(ctx context.Context, taskID int) (int, error) {
	// 1. 先找到 schedules 表中 embedded_task_id = taskID 的记录，获取对应的 event_id
	type row struct {
		EventID *int `gorm:"column:event_id"`
	}
	var r row
	err := d.db.WithContext(ctx).
		Table("schedules").
		Select("event_id").
		Where("embedded_task_id = ?", taskID).
		Order("id ASC").
		Limit(1).
		Scan(&r).Error
	if err != nil {
		return 0, err
	}
	if r.EventID == nil {
		return 0, respond.TargetTaskNotEmbeddedInAnySchedule
	}
	eventID := *r.EventID

	// 2. 删除该 event_id 对应的课程事件（通过级联删除实现）
	res := d.db.WithContext(ctx).
		Table("schedule_events").
		Where("id = ?", eventID).
		Delete(&model.ScheduleEvent{})
	if res.Error != nil {
		return 0, res.Error
	}
	if res.RowsAffected == 0 {
		return 0, respond.TargetTaskNotEmbeddedInAnySchedule
	}
	return eventID, nil
}

func (d *ScheduleDAO) DeleteScheduleEventByTaskItemID(ctx context.Context, taskItemID int) error {
	//直接找schedule_events表中type=task且rel_id=taskItemID的记录，删除它（级联删schedules）
	res := d.db.WithContext(ctx).
		Table("schedule_events").
		Where("type = ? AND rel_id = ?", "task", taskItemID).
		Delete(&model.ScheduleEvent{})
	if res.Error != nil {
		return res.Error
	}
	return nil
}

func (d *ScheduleDAO) GetUserRecentCompletedSchedules(ctx context.Context, nowTime time.Time, userID int, index, limit int) ([]model.Schedule, error) {
	var schedules []model.Schedule
	err := d.db.WithContext(ctx).
		Preload("Event").
		Preload("EmbeddedTask").
		Joins("JOIN schedule_events ON schedule_events.id = schedules.event_id").
		// 修改后的核心逻辑：
		// 1. 用户匹配 & 已结束
		// 2. 满足 (事件本身是任务) OR (虽然是课程但嵌入了任务)
		Where("schedules.user_id = ? AND schedule_events.end_time < ? AND (schedule_events.type = ? OR schedules.embedded_task_id IS NOT NULL)",
							userID, nowTime, "task").
		Order("schedule_events.end_time DESC"). // 命中索引
		Offset(index).
		Limit(limit).
		Find(&schedules).Error
	if err != nil {
		return nil, err
	}
	return schedules, nil
}

func (d *ScheduleDAO) GetScheduleEventWeekByID(ctx context.Context, eventID int) (int, error) {
	type row struct {
		Week *int `gorm:"column:week"`
	}
	var r row
	err := d.db.WithContext(ctx).
		Table("schedules").
		Select("week").
		Where("event_id = ?", eventID).
		Order("id ASC").
		Limit(1).
		Scan(&r).Error
	if err != nil {
		return 0, err
	}
	if r.Week == nil {
		return 0, respond.WrongScheduleEventID
	}
	return *r.Week, nil
}

func (d *ScheduleDAO) GetUserOngoingSchedule(ctx context.Context, userID int, nowTime time.Time) ([]model.Schedule, error) {
	var schedules []model.Schedule
	err := d.db.WithContext(ctx).
		Preload("Event").
		Preload("EmbeddedTask").
		Joins("JOIN schedule_events ON schedule_events.id = schedules.event_id").
		Where("schedules.user_id = ?  AND schedule_events.start_time <= ? AND schedule_events.end_time >= ?",
			userID, nowTime, nowTime).
		Or("schedules.user_id = ?  AND schedule_events.start_time > ?",
								userID, nowTime).
		Order("schedule_events.start_time ASC"). // 命中索引
		Find(&schedules).Error
	if err != nil {
		return nil, err
	}
	return schedules, nil
}

func (d *ScheduleDAO) RevocateSchedulesByEventID(ctx context.Context, eventID int) error {
	// 将 schedules 表中指定 event_id 的 embedded_task_id 字段置空（用于撤销嵌入关系）
	res := d.db.WithContext(ctx).
		Table("schedules").
		Where("event_id = ?", eventID).
		Update("status", "interrupted")
	if res.RowsAffected == 0 {
		return respond.WrongScheduleEventID
	}
	return res.Error
}

func (d *ScheduleDAO) GetRelIDByScheduleEventID(ctx context.Context, eventID int) (int, error) {
	type row struct {
		RelID *int `gorm:"column:rel_id"`
	}
	var r row
	err := d.db.WithContext(ctx).
		Table("schedule_events").
		Select("rel_id").
		Where("id = ?", eventID).
		First(&r).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return 0, respond.WrongScheduleEventID
		}
		return 0, err
	}
	if r.RelID == nil {
		return 0, nil
	}
	return *r.RelID, nil
}
