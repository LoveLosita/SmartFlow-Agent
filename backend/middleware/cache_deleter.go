package middleware

import (
	"context"
	"log"
	"reflect"
	"strings"

	"github.com/LoveLosita/smartflow/backend/dao"
	"github.com/LoveLosita/smartflow/backend/model"
	"gorm.io/gorm"
)

// GormCachePlugin 负责在 GORM 写操作成功后，按模型类型触发对应缓存失效。
//
// 职责边界：
// 1. 只负责“识别模型 -> 调用对应缓存删除逻辑”；
// 2. 不负责业务事务提交，也不负责缓存回填；
// 3. 只处理当前项目真正依赖的前台读取缓存，未接缓存的模型应静默忽略。
type GormCachePlugin struct {
	cacheDAO *dao.CacheDAO
}

func NewGormCachePlugin(dao *dao.CacheDAO) *GormCachePlugin {
	return &GormCachePlugin{
		cacheDAO: dao,
	}
}

// Name 返回 GORM 插件名。
func (p *GormCachePlugin) Name() string {
	return "GormCachePlugin"
}

// Initialize 注册 create/update/delete 成功后的统一失效钩子。
func (p *GormCachePlugin) Initialize(db *gorm.DB) error {
	_ = db.Callback().Create().After("gorm:create").Register("clear_related_cache_after_create", p.afterWrite)
	_ = db.Callback().Update().After("gorm:update").Register("clear_related_cache_after_update", p.afterWrite)
	_ = db.Callback().Delete().After("gorm:delete").Register("clear_related_cache_after_delete", p.afterWrite)
	return nil
}

func (p *GormCachePlugin) afterWrite(db *gorm.DB) {
	if db.Error != nil || db.Statement.Schema == nil {
		return
	}

	// 1. 先剥掉所有指针，拿到真实模型值。
	// 2. 若本次写入的是切片，按“切片元素类型”分发缓存逻辑即可。
	val := reflect.Indirect(reflect.ValueOf(db.Statement.Model))
	if val.Kind() == reflect.Slice {
		if val.Len() > 0 {
			p.dispatchCacheLogic(val.Index(0).Interface())
		}
		return
	}

	p.dispatchCacheLogic(val.Interface())
}

// dispatchCacheLogic 根据模型类型决定是否需要缓存失效。
//
// 步骤说明：
// 1. 先匹配真正有前台缓存读取依赖的模型，命中后执行对应删除逻辑；
// 2. 对已确认“不需要缓存失效”的模型显式静默忽略，避免正常链路反复刷屏；
// 3. 只有未知模型才打印日志，方便后续补齐遗漏的缓存策略。
func (p *GormCachePlugin) dispatchCacheLogic(modelObj interface{}) {
	switch m := modelObj.(type) {
	case model.Schedule:
		p.invalidScheduleCache(m.UserID, m.Week)
	case model.TaskClass:
		p.invalidTaskClassCache(*m.UserID)
	case model.Task:
		p.invalidTaskCache(m.UserID)
	case model.AgentScheduleState:
		p.invalidSchedulePlanPreviewCache(m.UserID, m.ConversationID)
	case model.MemoryItem:
		// 1. 管理面删除/修改/恢复/新增记忆时，自动失效该用户所有会话的预取缓存；
		// 2. repo 方法通过 Model(&model.MemoryItem{UserID: userID}) 携带 userID，
		//    此处从模型实例中提取 UserID 进行精准失效；
		// 3. 若 UserID 为 0（无 userID 参数的 repo 方法），invalidMemoryPrefetchCache 内部守卫会直接跳过。
		p.invalidMemoryPrefetchCache(m.UserID)
	case model.AgentOutboxMessage,
		model.User,
		model.ChatHistory,
		model.AgentChat,
		model.AgentTimelineEvent,
		model.AgentStateSnapshotRecord,
		model.MemoryJob,
		model.MemoryAuditLog,
		model.MemoryUserSetting:
		// 这些模型当前没有前台缓存读取链路依赖，故意静默忽略。
		return
	default:
		log.Printf("[GORM-Cache] No logic defined for model: %T", modelObj)
	}
}

func (p *GormCachePlugin) invalidScheduleCache(userID int, week int) {
	if userID == 0 || week == 0 {
		return
	}

	// 1. 异步删除缓存，避免阻塞主事务提交。
	// 2. 周视图变化后，同时清今天/最近完成/进行中缓存，保证口径一致。
	go func() {
		_ = p.cacheDAO.DeleteUserWeeklyScheduleFromCache(context.Background(), userID, week)
		_ = p.cacheDAO.DeleteUserTodayScheduleFromCache(context.Background(), userID)
		_ = p.cacheDAO.DeleteUserRecentCompletedSchedulesFromCache(context.Background(), userID)
		_ = p.cacheDAO.DeleteUserOngoingScheduleFromCache(context.Background(), userID)
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

func (p *GormCachePlugin) invalidSchedulePlanPreviewCache(userID int, conversationID string) {
	normalizedConversationID := strings.TrimSpace(conversationID)
	if userID == 0 || normalizedConversationID == "" {
		return
	}

	go func() {
		// 1. 排程快照被覆盖后，预览缓存必须同步删除，避免 Redis 里继续挂旧结果。
		// 2. 删除失败只记日志，不影响主事务，因为缓存永远是可回源的副本。
		if err := p.cacheDAO.DeleteSchedulePlanPreviewFromCache(context.Background(), userID, normalizedConversationID); err != nil {
			log.Printf("[GORM-Cache] Failed to invalidate schedule preview cache for user %d conversation %s: %v", userID, normalizedConversationID, err)
			return
		}
		log.Printf("[GORM-Cache] Invalidated schedule preview cache for user %d conversation %s", userID, normalizedConversationID)
	}()
}

// invalidMemoryPrefetchCache 失效指定用户所有会话的记忆预取缓存。
//
// 步骤化说明：
// 1. 先守卫 userID==0，无 userID 的 repo 方法（如 UpdateContentByID）触发 callback 时直接跳过；
// 2. 异步调用 DeleteMemoryPrefetchCacheByUser，按模式 smartflow:memory_prefetch:u:{userID}:c:* 批量删除；
// 3. 失败只记日志，不阻塞主事务，30 分钟 TTL 自然过期兜底。
func (p *GormCachePlugin) invalidMemoryPrefetchCache(userID int) {
	if userID == 0 {
		return
	}

	go func() {
		if err := p.cacheDAO.DeleteMemoryPrefetchCacheByUser(context.Background(), userID); err != nil {
			log.Printf("[GORM-Cache] Failed to invalidate memory prefetch cache for user %d: %v", userID, err)
			return
		}
		log.Printf("[GORM-Cache] Invalidated memory prefetch cache for user %d", userID)
	}()
}
