package dao

import (
	"context"
	"errors"
	"fmt"

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
