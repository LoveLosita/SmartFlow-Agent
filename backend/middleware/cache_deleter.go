package middleware

import (
	"context"
	"log"
	"reflect"

	"github.com/LoveLosita/smartflow/backend/dao"
	"github.com/LoveLosita/smartflow/backend/model"
	"gorm.io/gorm"
)

type GormCachePlugin struct {
	cacheDAO *dao.CacheDAO
}

func NewGormCachePlugin(dao *dao.CacheDAO) *GormCachePlugin {
	return &GormCachePlugin{
		cacheDAO: dao,
	}
}

// Name 插件名称
func (p *GormCachePlugin) Name() string {
	return "GormCachePlugin"
}

// Initialize 注册 GORM 钩子
func (p *GormCachePlugin) Initialize(db *gorm.DB) error {
	// 在增、删、改成功后，统一触发清理逻辑
	_ = db.Callback().Create().After("gorm:create").Register("clear_related_cache_after_create", p.afterWrite)
	_ = db.Callback().Update().After("gorm:update").Register("clear_related_cache_after_update", p.afterWrite)
	_ = db.Callback().Delete().After("gorm:delete").Register("clear_related_cache_after_delete", p.afterWrite)
	return nil
}

func (p *GormCachePlugin) afterWrite(db *gorm.DB) {
	if db.Error != nil || db.Statement.Schema == nil {
		return
	}

	// 获取 Model 的真实值（剥掉所有指针）
	val := reflect.Indirect(reflect.ValueOf(db.Statement.Model))

	// 如果是切片，拿切片里元素的类型
	if val.Kind() == reflect.Slice {
		if val.Len() > 0 {
			p.dispatchCacheLogic(val.Index(0).Interface(), db)
		}
	} else {
		p.dispatchCacheLogic(val.Interface(), db)
	}
}

// 根据不同的 Model 类型，调用不同的缓存失效逻辑
func (p *GormCachePlugin) dispatchCacheLogic(modelObj interface{}, db *gorm.DB) {
	switch m := modelObj.(type) {
	case model.Schedule:
		// 无论传的是 &s, s, 还是 &[]s，剥开后都是 model.Schedule
		p.invalidScheduleCache(m.UserID, m.Week)
	case model.TaskClass:
		p.invalidTaskClassCache(*m.UserID)
	case model.Task:
		p.invalidTaskCache(m.UserID)
	case model.AgentOutboxMessage, model.ChatHistory, model.AgentChat:
		// 这些模型目前没有定义缓存逻辑，先不处理
	default:
		// 只有真正没定义的模型才会到这里
		log.Printf("[GORM-Cache] No logic defined for model: %T", modelObj)
	}
}

func (p *GormCachePlugin) invalidScheduleCache(userID int, week int) {
	if userID == 0 || week == 0 {
		return
	}
	// 3. 异步执行，不阻塞主业务事务
	go func() {
		// 这里调用你的 CacheDAO 删缓存
		_ = p.cacheDAO.DeleteUserWeeklyScheduleFromCache(context.Background(), userID, week)
		_ = p.cacheDAO.DeleteUserTodayScheduleFromCache(context.Background(), userID)            // 同时删当天日程的缓存，确保数据一致
		_ = p.cacheDAO.DeleteUserRecentCompletedSchedulesFromCache(context.Background(), userID) // 同时删最近完成日程的缓存，确保数据一致
		_ = p.cacheDAO.DeleteUserOngoingScheduleFromCache(context.Background(), userID)          // 同时删正在进行日程的缓存，确保数据一致
		log.Printf("[GORM-Cache] Invalidated cache for user %d, week %d", userID, week)
	}()
}

func (p *GormCachePlugin) invalidTaskClassCache(userID int) {
	if userID == 0 {
		return
	}
	go func() {
		_ = p.cacheDAO.DeleteTaskClassList(context.Background(), userID)
		log.Printf("[GORM-Cache] Invalidated task class list cache for user %d", userID)
	}()
}

func (p *GormCachePlugin) invalidTaskCache(userID int) {
	if userID == 0 {
		return
	}
	go func() {
		_ = p.cacheDAO.DeleteUserTasksFromCache(context.Background(), userID)
		log.Printf("[GORM-Cache] Invalidated task list cache for user %d", userID)
	}()
}
