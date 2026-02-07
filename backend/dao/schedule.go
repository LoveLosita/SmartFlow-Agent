package dao

import (
	"context"
	"errors"

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

func (dao *ScheduleDAO) AddSchedules(schedules []model.Schedule) ([]int, error) {
	if err := dao.db.Create(&schedules).Error; err != nil {
		return nil, err
	}
	ids := make([]int, len(schedules))
	for i, s := range schedules {
		ids[i] = s.ID
	}
	return ids, nil
}

func (dao *ScheduleDAO) EmbedTaskIntoSchedule(startSection, endSection, dayOfWeek, week, userID, taskID int) error {
	// 仅更新指定：用户/周/星期/节次区间 的记录，将 embedded_task_id 精准写入 taskID
	res := dao.db.
		Table("schedules").
		Where("user_id = ? AND week = ? AND day_of_week = ? AND section BETWEEN ? AND ?", userID, week, dayOfWeek, startSection, endSection).
		Update("embedded_task_id", taskID)

	return res.Error
}

func (dao *ScheduleDAO) GetCourseUserIDByID(ctx context.Context, courseScheduleEventID int) (int, error) {
	type row struct {
		UserID *int `gorm:"column:user_id"`
	}

	var r row
	err := dao.db.WithContext(ctx).
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
func (dao *ScheduleDAO) IsCourseEmbeddedByOtherTaskBlock(ctx context.Context, courseID, startSection, endSection int) (bool, error) {
	// 若区间非法，视为不冲突
	if startSection <= 0 || endSection <= 0 || startSection > endSection {
		return false, nil
	}

	var cnt int64
	err := dao.db.WithContext(ctx).
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

func (dao *ScheduleDAO) HasUserScheduleConflict(ctx context.Context, userID, week, dayOfWeek int, sections []int) (bool, error) {
	// 无节次则视为无冲突
	if len(sections) == 0 {
		return false, nil
	}
	// 统计同一用户、同一周、同一天、且节次有交集的排程数量
	// 约定表字段：user_id, week, day_of_week, section
	var cnt int64
	err := dao.db.WithContext(ctx).
		Table("schedules").
		Where("user_id = ? AND week = ? AND day_of_week = ?", userID, week, dayOfWeek).
		Where("section IN ?", sections).
		Count(&cnt).Error
	if err != nil {
		return false, err
	}
	return cnt > 0, nil
}

func (dao *ScheduleDAO) IsCourseTimeMatch(ctx context.Context, courseScheduleEventID, week, dayOfWeek, startSection, endSection int) (bool, error) {
	// 区间非法直接不匹配
	if startSection <= 0 || endSection <= 0 || startSection > endSection {
		return false, nil
	}

	// 核对该课程事件在指定 周\+星期 下，是否存在覆盖整个节次区间的排程记录
	// 说明：此处按你当前表结构的用法（schedule\_events 存事件，schedules 存节次明细）来写：
	// schedules 里通过 schedule\_event\_id 关联到 schedule\_events.id
	var cnt int64
	err := dao.db.WithContext(ctx).
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

func (dao *ScheduleDAO) AddScheduleEvent(scheduleEvent *model.ScheduleEvent) (int, error) {
	if err := dao.db.Create(&scheduleEvent).Error; err != nil {
		return 0, err
	}
	return scheduleEvent.ID, nil
}
