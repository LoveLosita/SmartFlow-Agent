package dao

import (
	"context"
	"encoding/json"
	"errors"

	"fmt"
	memorymodel "github.com/LoveLosita/smartflow/backend/services/memory/model"
	"strings"
	"time"

	"github.com/LoveLosita/smartflow/backend/services/runtime/model"
	"github.com/go-redis/redis/v8"
)

type CacheDAO struct {
	client *redis.Client
}

func NewCacheDAO(client *redis.Client) *CacheDAO {
	return &CacheDAO{client: client}
}

func (d *CacheDAO) schedulePreviewKey(userID int, conversationID string) string {
	return fmt.Sprintf("smartflow:schedule_preview:u:%d:c:%s", userID, conversationID)
}

func (d *CacheDAO) conversationTimelineKey(userID int, conversationID string) string {
	return fmt.Sprintf("smartflow:conversation_timeline:u:%d:c:%s", userID, conversationID)
}

func (d *CacheDAO) conversationTimelineSeqKey(userID int, conversationID string) string {
	return fmt.Sprintf("smartflow:conversation_timeline_seq:u:%d:c:%s", userID, conversationID)
}

func (d *CacheDAO) AddTaskClassList(ctx context.Context, userID int, list *model.UserGetTaskClassesResponse) error {
	// 1. 定义 Key，使用 userID 隔离不同用户的数据。
	key := fmt.Sprintf("smartflow:task_classes:%d", userID)
	// 2. 序列化：将结构体转为 []byte。
	data, err := json.Marshal(list)
	if err != nil {
		return err
	}
	// 3. 存储：设置 30 分钟过期，可按业务需要调整。
	return d.client.Set(ctx, key, data, 30*time.Minute).Err()
}

func (d *CacheDAO) GetTaskClassList(ctx context.Context, userID int) (*model.UserGetTaskClassesResponse, error) {
	key := fmt.Sprintf("smartflow:task_classes:%d", userID)
	var resp model.UserGetTaskClassesResponse
	// 1. 从 Redis 获取字符串。
	val, err := d.client.Get(ctx, key).Result()
	if err != nil {
		// 注意：若是 redis.Nil，则交给 Service 层处理回源查询逻辑。
		return &resp, err
	}
	// 2. 反序列化：将 JSON 还原回结构体。
	err = json.Unmarshal([]byte(val), &resp)
	return &resp, err
}

func (d *CacheDAO) DeleteTaskClassList(ctx context.Context, userID int) error {
	key := fmt.Sprintf("smartflow:task_classes:%d", userID)
	return d.client.Del(ctx, key).Err()
}

func (d *CacheDAO) GetRecord(ctx context.Context, key string) (string, error) {
	val, err := d.client.Get(ctx, key).Result()
	if errors.Is(err, redis.Nil) {
		return "", nil // 正常未命中
	}
	return val, err // 真正的 Redis 错误
}

func (d *CacheDAO) SaveRecord(ctx context.Context, key string, val string, ttl time.Duration) error {
	return d.client.Set(ctx, key, val, ttl).Err()
}

func (d *CacheDAO) AcquireLock(ctx context.Context, key string, ttl time.Duration) (bool, error) {
	return d.client.SetNX(ctx, key, "processing", ttl).Result()
}

func (d *CacheDAO) ReleaseLock(ctx context.Context, key string) error {
	return d.client.Del(ctx, key).Err()
}

// GetUserTasksFromCache 读取用户任务缓存（内部模型版本）。
//
// 职责边界：
// 1. 负责从 Redis 读取 `[]model.Task`，供 Service 层做“读时派生优先级”；
// 2. 不负责把模型转换成对外 DTO（该职责在 conv 层）；
// 3. 不负责缓存回填和缓存失效（回填由 Service 控制，失效由 GORM cache_deleter 统一处理）。
//
// 输入输出语义：
// 1. 命中缓存时返回任务模型切片与 nil error；
// 2. 未命中时返回 redis.Nil，由上层决定是否回源 DB；
// 3. 反序列化失败时返回 error，避免把损坏缓存继续向后传播。
func (d *CacheDAO) GetUserTasksFromCache(ctx context.Context, userID int) ([]model.Task, error) {
	key := fmt.Sprintf("smartflow:tasks:%d", userID)
	var tasks []model.Task
	val, err := d.client.Get(ctx, key).Result()
	if err != nil {
		return nil, err // 注意：若是 redis.Nil，则交给 Service 层处理回源查询逻辑
	}
	err = json.Unmarshal([]byte(val), &tasks)
	return tasks, err
}

// SetUserTasksToCache 写入用户任务缓存（内部模型版本）。
//
// 职责边界：
// 1. 负责把 DB 读取到的原始 `[]model.Task` 写入缓存；
// 2. 不负责对任务做“紧急性平移派生”，避免把派生结果写回缓存导致后续无法继续触发异步平移；
// 3. 不负责缓存删除，删除策略由 cache_deleter 在写库后触发。
//
// 步骤说明：
// 1. 先把模型序列化为 JSON，确保 `urgency_threshold_at` 等字段完整保留；
// 2. 再写入固定 TTL 缓存，命中后可减少 DB 读取压力；
// 3. 若序列化失败立即返回 error，避免写入半结构化垃圾数据。
func (d *CacheDAO) SetUserTasksToCache(ctx context.Context, userID int, tasks []model.Task) error {
	key := fmt.Sprintf("smartflow:tasks:%d", userID)
	data, err := json.Marshal(tasks)
	if err != nil {
		return err
	}
	return d.client.Set(ctx, key, data, 24*time.Hour).Err()
}

func (d *CacheDAO) DeleteUserTasksFromCache(ctx context.Context, userID int) error {
	key := fmt.Sprintf("smartflow:tasks:%d", userID)
	return d.client.Del(ctx, key).Err()
}

func (d *CacheDAO) GetUserTodayScheduleFromCache(ctx context.Context, userID int) ([]model.UserTodaySchedule, error) {
	key := fmt.Sprintf("smartflow:today_schedule:%d", userID)
	var schedules []model.UserTodaySchedule
	val, err := d.client.Get(ctx, key).Result()
	if err != nil {
		return nil, err // 注意：若是 redis.Nil，则交给 Service 层处理回源查询逻辑
	}
	err = json.Unmarshal([]byte(val), &schedules)
	return schedules, err
}

func (d *CacheDAO) SetUserTodayScheduleToCache(ctx context.Context, userID int, schedules []model.UserTodaySchedule) error {
	key := fmt.Sprintf("smartflow:today_schedule:%d", userID)
	data, err := json.Marshal(schedules)
	if err != nil {
		return err
	}
	// 设置过期时间为“当天剩余时间”，保证每天自然刷新一次缓存。
	return d.client.Set(ctx, key, data, time.Until(time.Date(time.Now().Year(), time.Now().Month(), time.Now().Day()+1, 0, 0, 0, 0, time.Now().Location()))).Err()
}

func (d *CacheDAO) DeleteUserTodayScheduleFromCache(ctx context.Context, userID int) error {
	key := fmt.Sprintf("smartflow:today_schedule:%d", userID)
	return d.client.Del(ctx, key).Err()
}

func (d *CacheDAO) GetUserWeeklyScheduleFromCache(ctx context.Context, userID int, week int) (*model.UserWeekSchedule, error) {
	key := fmt.Sprintf("smartflow:weekly_schedule:%d:%d", userID, week)
	var schedules model.UserWeekSchedule
	val, err := d.client.Get(ctx, key).Result()
	if err != nil {
		return nil, err // 注意：若是 redis.Nil，则交给 Service 层处理回源查询逻辑
	}
	err = json.Unmarshal([]byte(val), &schedules)
	return &schedules, err
}

func (d *CacheDAO) SetUserWeeklyScheduleToCache(ctx context.Context, userID int, schedules *model.UserWeekSchedule) error {
	key := fmt.Sprintf("smartflow:weekly_schedule:%d:%d", userID, schedules.Week)
	data, err := json.Marshal(schedules)
	if err != nil {
		return err
	}
	// 设置过期时间为一天。
	return d.client.Set(ctx, key, data, 24*time.Hour).Err()
}

func (d *CacheDAO) DeleteUserWeeklyScheduleFromCache(ctx context.Context, userID int, week int) error {
	key := fmt.Sprintf("smartflow:weekly_schedule:%d:%d", userID, week)
	return d.client.Del(ctx, key).Err()
}

func (d *CacheDAO) GetUserRecentCompletedSchedulesFromCache(ctx context.Context, userID, index, limit int) (*model.UserRecentCompletedScheduleResponse, error) {
	key := fmt.Sprintf("smartflow:recent_completed_schedules:%d:%d:%d", userID, index, limit)
	var resp model.UserRecentCompletedScheduleResponse
	val, err := d.client.Get(ctx, key).Result()
	if err != nil {
		return &resp, err // 注意：若是 redis.Nil，则交给 Service 层处理回源查询逻辑
	}
	err = json.Unmarshal([]byte(val), &resp)
	return &resp, err
}

func (d *CacheDAO) SetUserRecentCompletedSchedulesToCache(ctx context.Context, userID, index, limit int, resp *model.UserRecentCompletedScheduleResponse) error {
	key := fmt.Sprintf("smartflow:recent_completed_schedules:%d:%d:%d", userID, index, limit)
	data, err := json.Marshal(resp)
	if err != nil {
		return err
	}
	// 设置过期时间为 30 分钟。
	return d.client.Set(ctx, key, data, 30*time.Minute).Err()
}

func (d *CacheDAO) DeleteUserRecentCompletedSchedulesFromCache(ctx context.Context, userID int) error {
	pattern := fmt.Sprintf("smartflow:recent_completed_schedules:%d:*", userID)

	var cursor uint64
	for {
		keys, next, err := d.client.Scan(ctx, cursor, pattern, 500).Result()
		if err != nil {
			return err
		}
		if len(keys) > 0 {
			// 使用 UNLINK() 异步删除，降低阻塞风险；若需要强一致删除可改用 Del()。
			if err := d.client.Unlink(ctx, keys...).Err(); err != nil {
				return err
			}
		}
		cursor = next
		if cursor == 0 {
			break
		}
	}
	return nil
}

func (d *CacheDAO) GetUserOngoingScheduleFromCache(ctx context.Context, userID int) (*model.OngoingSchedule, error) {
	key := fmt.Sprintf("smartflow:ongoing_schedule:%d", userID)
	var schedule model.OngoingSchedule
	val, err := d.client.Get(ctx, key).Result()
	if err != nil {
		return &schedule, err // 注意：若是 redis.Nil，则交给 Service 层处理回源查询逻辑
	}
	if val == "null" {
		return nil, nil // 之前缓存过“当前没有正在进行的日程”，这里直接返回 nil
	}
	err = json.Unmarshal([]byte(val), &schedule)
	return &schedule, err
}

func (d *CacheDAO) SetUserOngoingScheduleToCache(ctx context.Context, userID int, schedule *model.OngoingSchedule) error {
	if schedule == nil {
		// 如果当前没有正在进行的日程，则缓存空值并短暂过期，避免频繁回源查询。
		key := fmt.Sprintf("smartflow:ongoing_schedule:%d", userID)
		return d.client.Set(ctx, key, "null", 5*time.Minute).Err()
	}
	key := fmt.Sprintf("smartflow:ongoing_schedule:%d", userID)
	data, err := json.Marshal(schedule)
	if err != nil {
		return err
	}
	// 设置过期时间为距离 endTime 的剩余时长；若已过期，则不再写入缓存。
	ttl := time.Until(schedule.EndTime)
	if ttl <= 0 {
		return nil
	}
	return d.client.Set(ctx, key, data, ttl).Err()
}

func (d *CacheDAO) DeleteUserOngoingScheduleFromCache(ctx context.Context, userID int) error {
	key := fmt.Sprintf("smartflow:ongoing_schedule:%d", userID)
	return d.client.Del(ctx, key).Err()
}

// SetSchedulePlanPreviewToCache 写入“排程预览”缓存。
//
// 职责边界：
// 1. 负责按 user_id + conversation_id 写入结构化预览快照；
// 2. 负责 preview 入库前的基础参数校验，避免无效 key；
// 3. 不负责 DB 回源，不负责业务重试策略。
//
// 步骤化说明：
// 1. 先校验 user_id / conversation_id / preview，防止脏写；
// 2. 再序列化 preview 为 JSON，保证缓存结构稳定；
// 3. 最后按固定 TTL 写入 Redis，超时后自动失效。
func (d *CacheDAO) SetSchedulePlanPreviewToCache(ctx context.Context, userID int, conversationID string, preview *model.SchedulePlanPreviewCache) error {
	if d == nil || d.client == nil {
		return errors.New("cache dao is not initialized")
	}
	if userID <= 0 {
		return fmt.Errorf("invalid user_id: %d", userID)
	}
	normalizedConversationID := strings.TrimSpace(conversationID)
	if normalizedConversationID == "" {
		return errors.New("conversation_id is empty")
	}
	if preview == nil {
		return errors.New("schedule preview is nil")
	}

	data, err := json.Marshal(preview)
	if err != nil {
		return fmt.Errorf("marshal schedule preview failed: %w", err)
	}
	return d.client.Set(ctx, d.schedulePreviewKey(userID, normalizedConversationID), data, 1*time.Hour).Err()
}

// GetSchedulePlanPreviewFromCache 读取“排程预览”缓存。
//
// 输入输出语义：
// 1. 命中时返回 (*SchedulePlanPreviewCache, nil)；
// 2. 未命中时返回 (nil, nil)；
// 3. Redis 异常或反序列化失败时返回 error。
func (d *CacheDAO) GetSchedulePlanPreviewFromCache(ctx context.Context, userID int, conversationID string) (*model.SchedulePlanPreviewCache, error) {
	if d == nil || d.client == nil {
		return nil, errors.New("cache dao is not initialized")
	}
	if userID <= 0 {
		return nil, fmt.Errorf("invalid user_id: %d", userID)
	}
	normalizedConversationID := strings.TrimSpace(conversationID)
	if normalizedConversationID == "" {
		return nil, errors.New("conversation_id is empty")
	}

	raw, err := d.client.Get(ctx, d.schedulePreviewKey(userID, normalizedConversationID)).Result()
	if err == redis.Nil {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	var preview model.SchedulePlanPreviewCache
	if err = json.Unmarshal([]byte(raw), &preview); err != nil {
		return nil, fmt.Errorf("unmarshal schedule preview failed: %w", err)
	}
	return &preview, nil
}

// DeleteSchedulePlanPreviewFromCache 删除“排程预览”缓存。
//
// 说明：
// 1. 删除操作是幂等的，key 不存在也视为成功；
// 2. 该方法用于新排程前清旧预览，或状态快照更新后触发失效。
func (d *CacheDAO) DeleteSchedulePlanPreviewFromCache(ctx context.Context, userID int, conversationID string) error {
	if d == nil || d.client == nil {
		return errors.New("cache dao is not initialized")
	}
	if userID <= 0 {
		return fmt.Errorf("invalid user_id: %d", userID)
	}
	normalizedConversationID := strings.TrimSpace(conversationID)
	if normalizedConversationID == "" {
		return errors.New("conversation_id is empty")
	}
	return d.client.Del(ctx, d.schedulePreviewKey(userID, normalizedConversationID)).Err()
}

// IncrConversationTimelineSeq 原子递增并返回会话时间线 seq。
//
// 说明：
// 1. seq 只在同一 user_id + conversation_id 维度内递增；
// 2. 使用 Redis INCR 保证并发下不会拿到重复顺序号；
// 3. 该 key 也会设置 TTL，避免长尾会话长期占用缓存。
func (d *CacheDAO) IncrConversationTimelineSeq(ctx context.Context, userID int, conversationID string) (int64, error) {
	if d == nil || d.client == nil {
		return 0, errors.New("cache dao is not initialized")
	}
	if userID <= 0 {
		return 0, fmt.Errorf("invalid user_id: %d", userID)
	}
	normalizedConversationID := strings.TrimSpace(conversationID)
	if normalizedConversationID == "" {
		return 0, errors.New("conversation_id is empty")
	}

	key := d.conversationTimelineSeqKey(userID, normalizedConversationID)
	pipe := d.client.Pipeline()
	incrCmd := pipe.Incr(ctx, key)
	pipe.Expire(ctx, key, 24*time.Hour)
	if _, err := pipe.Exec(ctx); err != nil {
		return 0, err
	}
	return incrCmd.Val(), nil
}

// SetConversationTimelineSeq 强制设置会话时间线当前 seq（DB 回填 Redis 兜底场景）。
func (d *CacheDAO) SetConversationTimelineSeq(ctx context.Context, userID int, conversationID string, seq int64) error {
	if d == nil || d.client == nil {
		return errors.New("cache dao is not initialized")
	}
	if userID <= 0 {
		return fmt.Errorf("invalid user_id: %d", userID)
	}
	normalizedConversationID := strings.TrimSpace(conversationID)
	if normalizedConversationID == "" {
		return errors.New("conversation_id is empty")
	}
	if seq < 0 {
		seq = 0
	}
	return d.client.Set(ctx, d.conversationTimelineSeqKey(userID, normalizedConversationID), seq, 24*time.Hour).Err()
}

// AppendConversationTimelineEventToCache 追加单条时间线缓存事件。
func (d *CacheDAO) AppendConversationTimelineEventToCache(
	ctx context.Context,
	userID int,
	conversationID string,
	item model.GetConversationTimelineItem,
) error {
	if d == nil || d.client == nil {
		return errors.New("cache dao is not initialized")
	}
	if userID <= 0 {
		return fmt.Errorf("invalid user_id: %d", userID)
	}
	normalizedConversationID := strings.TrimSpace(conversationID)
	if normalizedConversationID == "" {
		return errors.New("conversation_id is empty")
	}

	data, err := json.Marshal(item)
	if err != nil {
		return fmt.Errorf("marshal conversation timeline item failed: %w", err)
	}

	key := d.conversationTimelineKey(userID, normalizedConversationID)
	pipe := d.client.Pipeline()
	pipe.RPush(ctx, key, data)
	pipe.Expire(ctx, key, 24*time.Hour)
	_, err = pipe.Exec(ctx)
	return err
}

// SetConversationTimelineToCache 全量回填时间线缓存。
func (d *CacheDAO) SetConversationTimelineToCache(ctx context.Context, userID int, conversationID string, items []model.GetConversationTimelineItem) error {
	if d == nil || d.client == nil {
		return errors.New("cache dao is not initialized")
	}
	if userID <= 0 {
		return fmt.Errorf("invalid user_id: %d", userID)
	}
	normalizedConversationID := strings.TrimSpace(conversationID)
	if normalizedConversationID == "" {
		return errors.New("conversation_id is empty")
	}

	key := d.conversationTimelineKey(userID, normalizedConversationID)
	pipe := d.client.Pipeline()
	pipe.Del(ctx, key)
	if len(items) > 0 {
		values := make([]interface{}, 0, len(items))
		for _, item := range items {
			data, err := json.Marshal(item)
			if err != nil {
				return fmt.Errorf("marshal conversation timeline item failed: %w", err)
			}
			values = append(values, data)
		}
		pipe.RPush(ctx, key, values...)
	}
	pipe.Expire(ctx, key, 24*time.Hour)
	_, err := pipe.Exec(ctx)
	return err
}

// GetConversationTimelineFromCache 读取时间线缓存（按 seq 正序）。
func (d *CacheDAO) GetConversationTimelineFromCache(ctx context.Context, userID int, conversationID string) ([]model.GetConversationTimelineItem, error) {
	if d == nil || d.client == nil {
		return nil, errors.New("cache dao is not initialized")
	}
	if userID <= 0 {
		return nil, fmt.Errorf("invalid user_id: %d", userID)
	}
	normalizedConversationID := strings.TrimSpace(conversationID)
	if normalizedConversationID == "" {
		return nil, errors.New("conversation_id is empty")
	}

	rawItems, err := d.client.LRange(ctx, d.conversationTimelineKey(userID, normalizedConversationID), 0, -1).Result()
	if err == redis.Nil {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if len(rawItems) == 0 {
		return nil, nil
	}

	items := make([]model.GetConversationTimelineItem, 0, len(rawItems))
	for _, raw := range rawItems {
		var item model.GetConversationTimelineItem
		if err := json.Unmarshal([]byte(raw), &item); err != nil {
			return nil, fmt.Errorf("unmarshal conversation timeline item failed: %w", err)
		}
		items = append(items, item)
	}
	return items, nil
}

// DeleteConversationTimelineFromCache 删除时间线缓存和 seq 缓存。
func (d *CacheDAO) DeleteConversationTimelineFromCache(ctx context.Context, userID int, conversationID string) error {
	if d == nil || d.client == nil {
		return errors.New("cache dao is not initialized")
	}
	if userID <= 0 {
		return fmt.Errorf("invalid user_id: %d", userID)
	}
	normalizedConversationID := strings.TrimSpace(conversationID)
	if normalizedConversationID == "" {
		return errors.New("conversation_id is empty")
	}
	return d.client.Del(
		ctx,
		d.conversationTimelineKey(userID, normalizedConversationID),
		d.conversationTimelineSeqKey(userID, normalizedConversationID),
	).Err()
}

// agentStateKey 返回 agent 运行态快照的 Redis key。
//
// Key 设计：
// 1. 使用 smartflow:agent_state 前缀，与现有 key 命名空间隔离；
// 2. 使用 conversationID 作为唯一标识，因为 agent 状态是按会话维度持久化的。
const activeScheduleSessionCacheTTL = 2 * time.Hour

// activeScheduleSessionKey 生成 session_id 维度的主动调度会话缓存 key。
func (d *CacheDAO) activeScheduleSessionKey(sessionID string) string {
	return fmt.Sprintf("smartflow:active_schedule_session:s:%s", strings.TrimSpace(sessionID))
}

// activeScheduleSessionConversationKey 生成 user_id + conversation_id 维度的主动调度会话缓存 key。
func (d *CacheDAO) activeScheduleSessionConversationKey(userID int, conversationID string) string {
	return fmt.Sprintf("smartflow:active_schedule_session:u:%d:c:%s", userID, strings.TrimSpace(conversationID))
}

// SetActiveScheduleSessionToCache 同步写入主动调度会话缓存。
//
// 步骤化说明：
// 1. 先校验 snapshot 和主键，避免把无效会话写进 Redis；
// 2. 再把同一份快照写入 session_id / conversation_id 两个维度的 key；
// 3. 若 conversation_id 还没绑定，只写 session_id key，避免生成空路由 key。
func (d *CacheDAO) SetActiveScheduleSessionToCache(ctx context.Context, snapshot *model.ActiveScheduleSessionSnapshot) error {
	if d == nil || d.client == nil {
		return errors.New("cache dao is not initialized")
	}
	if snapshot == nil {
		return errors.New("active schedule session snapshot is nil")
	}

	sessionID := strings.TrimSpace(snapshot.SessionID)
	if sessionID == "" {
		return errors.New("session_id is empty")
	}

	data, err := json.Marshal(snapshot)
	if err != nil {
		return fmt.Errorf("marshal active schedule session cache failed: %w", err)
	}

	pipe := d.client.Pipeline()
	pipe.Set(ctx, d.activeScheduleSessionKey(sessionID), data, activeScheduleSessionCacheTTL)
	if conversationID := strings.TrimSpace(snapshot.ConversationID); conversationID != "" && snapshot.UserID > 0 {
		pipe.Set(ctx, d.activeScheduleSessionConversationKey(snapshot.UserID, conversationID), data, activeScheduleSessionCacheTTL)
	}
	_, err = pipe.Exec(ctx)
	return err
}

// GetActiveScheduleSessionFromCache 按 session_id 读取主动调度会话缓存。
func (d *CacheDAO) GetActiveScheduleSessionFromCache(ctx context.Context, sessionID string) (*model.ActiveScheduleSessionSnapshot, error) {
	if d == nil || d.client == nil {
		return nil, errors.New("cache dao is not initialized")
	}

	normalizedSessionID := strings.TrimSpace(sessionID)
	if normalizedSessionID == "" {
		return nil, errors.New("session_id is empty")
	}

	raw, err := d.client.Get(ctx, d.activeScheduleSessionKey(normalizedSessionID)).Result()
	if errors.Is(err, redis.Nil) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	var snapshot model.ActiveScheduleSessionSnapshot
	if err = json.Unmarshal([]byte(raw), &snapshot); err != nil {
		return nil, fmt.Errorf("unmarshal active schedule session cache failed: %w", err)
	}
	return &snapshot, nil
}

// GetActiveScheduleSessionFromConversationCache 按 user_id + conversation_id 读取主动调度会话缓存。
func (d *CacheDAO) GetActiveScheduleSessionFromConversationCache(ctx context.Context, userID int, conversationID string) (*model.ActiveScheduleSessionSnapshot, error) {
	if d == nil || d.client == nil {
		return nil, errors.New("cache dao is not initialized")
	}
	if userID <= 0 {
		return nil, fmt.Errorf("invalid user_id: %d", userID)
	}

	normalizedConversationID := strings.TrimSpace(conversationID)
	if normalizedConversationID == "" {
		return nil, errors.New("conversation_id is empty")
	}

	raw, err := d.client.Get(ctx, d.activeScheduleSessionConversationKey(userID, normalizedConversationID)).Result()
	if errors.Is(err, redis.Nil) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	var snapshot model.ActiveScheduleSessionSnapshot
	if err = json.Unmarshal([]byte(raw), &snapshot); err != nil {
		return nil, fmt.Errorf("unmarshal active schedule session cache failed: %w", err)
	}
	return &snapshot, nil
}

// DeleteActiveScheduleSessionFromCache 删除主动调度会话缓存。
//
// 说明：
// 1. 会同时清理 session_id 和 conversation_id 两个维度，避免旧路由缓存残留；
// 2. conversation_id 为空时只清 session_id key；
// 3. 删除操作本身幂等，即使 key 不存在也视为成功。
func (d *CacheDAO) DeleteActiveScheduleSessionFromCache(ctx context.Context, sessionID string, userID int, conversationID string) error {
	if d == nil || d.client == nil {
		return errors.New("cache dao is not initialized")
	}

	normalizedSessionID := strings.TrimSpace(sessionID)
	if normalizedSessionID == "" {
		return errors.New("session_id is empty")
	}

	keys := []string{d.activeScheduleSessionKey(normalizedSessionID)}
	if userID > 0 {
		if normalizedConversationID := strings.TrimSpace(conversationID); normalizedConversationID != "" {
			keys = append(keys, d.activeScheduleSessionConversationKey(userID, normalizedConversationID))
		}
	}
	return d.client.Del(ctx, keys...).Err()
}

func (d *CacheDAO) agentStateKey(conversationID string) string {
	return fmt.Sprintf("smartflow:agent_state:%s", conversationID)
}

// SaveAgentState 序列化并保存 agent 运行态快照到 Redis。
//
// 职责边界：
// 1. 只负责 JSON 序列化 + Redis SET，不做业务校验；
// 2. TTL 默认 2h，过期自动清理，配合 MySQL outbox 异步持久化；
// 3. snapshot 为 nil 时直接返回，避免写入无效数据。
func (d *CacheDAO) SaveAgentState(ctx context.Context, conversationID string, snapshot any) error {
	if d == nil || d.client == nil {
		return errors.New("cache dao is not initialized")
	}
	normalizedID := strings.TrimSpace(conversationID)
	if normalizedID == "" {
		return errors.New("conversation_id is empty")
	}
	if snapshot == nil {
		return nil
	}

	data, err := json.Marshal(snapshot)
	if err != nil {
		return fmt.Errorf("marshal agent state failed: %w", err)
	}
	return d.client.Set(ctx, d.agentStateKey(normalizedID), data, 2*time.Hour).Err()
}

// LoadAgentState 从 Redis 读取并反序列化 agent 运行态快照。
//
// 返回值语义：
// 1. (result, true, nil)：命中快照，正常返回；
// 2. (nil, false, nil)：未命中，不是错误，调用方应走新建对话路径；
// 3. (nil, false, error)：Redis 或反序列化错误。
func (d *CacheDAO) LoadAgentState(ctx context.Context, conversationID string, result any) (bool, error) {
	if d == nil || d.client == nil {
		return false, errors.New("cache dao is not initialized")
	}
	normalizedID := strings.TrimSpace(conversationID)
	if normalizedID == "" {
		return false, errors.New("conversation_id is empty")
	}

	raw, err := d.client.Get(ctx, d.agentStateKey(normalizedID)).Result()
	if errors.Is(err, redis.Nil) {
		return false, nil
	}
	if err != nil {
		return false, err
	}

	if err := json.Unmarshal([]byte(raw), result); err != nil {
		return false, fmt.Errorf("unmarshal agent state failed: %w", err)
	}
	return true, nil
}

// DeleteAgentState 删除指定会话的 agent 运行态快照。
//
// 语义：
// 1. 删除操作是幂等的，key 不存在也视为成功；
// 2. 典型调用时机：Deliver 节点任务完成后清理。
func (d *CacheDAO) DeleteAgentState(ctx context.Context, conversationID string) error {
	if d == nil || d.client == nil {
		return errors.New("cache dao is not initialized")
	}
	normalizedID := strings.TrimSpace(conversationID)
	if normalizedID == "" {
		return errors.New("conversation_id is empty")
	}
	return d.client.Del(ctx, d.agentStateKey(normalizedID)).Err()
}

// --- 记忆预取缓存 ---

const (
	memoryPrefetchTTL = 30 * time.Minute
)

// memoryPrefetchKey 生成用户+会话维度的记忆预取缓存 key。
//
// 1. 格式：smartflow:memory_prefetch:u:{userID}:c:{chatID}，与 conversationTimelineKey / schedulePreviewKey 命名风格一致；
// 2. chatID 为空时 key 为 smartflow:memory_prefetch:u:5:c:，仍然合法且唯一，不会与其他会话 key 冲突；
// 3. 加 chatID 隔离后，不同会话各自维护独立的预取缓存，避免会话间记忆上下文互相覆盖。
func (d *CacheDAO) memoryPrefetchKey(userID int, chatID string) string {
	return fmt.Sprintf("smartflow:memory_prefetch:u:%d:c:%s", userID, chatID)
}

// GetMemoryPrefetchCache 读取用户记忆预取缓存。
//
// 输入输出语义：
// 1. 命中时返回 ItemDTO 切片与 nil error；
// 2. 未命中时返回 nil, nil；
// 3. Redis 异常或反序列化失败时返回 error。
func (d *CacheDAO) GetMemoryPrefetchCache(ctx context.Context, userID int, chatID string) ([]memorymodel.ItemDTO, error) {
	if d == nil || d.client == nil {
		return nil, errors.New("cache dao is not initialized")
	}
	if userID <= 0 {
		return nil, nil
	}

	key := d.memoryPrefetchKey(userID, chatID)
	raw, err := d.client.Get(ctx, key).Result()
	if errors.Is(err, redis.Nil) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	var items []memorymodel.ItemDTO
	if err = json.Unmarshal([]byte(raw), &items); err != nil {
		return nil, fmt.Errorf("unmarshal memory prefetch cache failed: %w", err)
	}
	return items, nil
}

// SetMemoryPrefetchCache 写入用户记忆预取缓存。
//
// 职责边界：
// 1. 负责将检索后的记忆 DTO 写入 Redis，供下一轮 Chat 节点即时消费；
// 2. TTL 30 分钟，靠自然过期淘汰，不需要显式 Invalidate；
// 3. items 为空或 nil 时直接返回，避免写入无效数据。
func (d *CacheDAO) SetMemoryPrefetchCache(ctx context.Context, userID int, chatID string, items []memorymodel.ItemDTO) error {
	if d == nil || d.client == nil {
		return errors.New("cache dao is not initialized")
	}
	if userID <= 0 || len(items) == 0 {
		return nil
	}

	data, err := json.Marshal(items)
	if err != nil {
		return fmt.Errorf("marshal memory prefetch cache failed: %w", err)
	}
	key := d.memoryPrefetchKey(userID, chatID)
	return d.client.Set(ctx, key, data, memoryPrefetchTTL).Err()
}

// DeleteMemoryPrefetchCacheByUser 删除指定用户所有会话的记忆预取缓存。
//
// 步骤化说明：
// 1. 用 SCAN 遍历 smartflow:memory_prefetch:u:{userID}:c:* 匹配的所有 key；
// 2. 用 UNLINK 异步删除，避免阻塞 Redis 主线程；
// 3. 复用 DeleteUserRecentCompletedSchedulesFromCache 的 SCAN+UNLINK 模式；
// 4. 该方法被 GORM cache deleter 和空检索清理两条链路共同调用，保证缓存一致性。
func (d *CacheDAO) DeleteMemoryPrefetchCacheByUser(ctx context.Context, userID int) error {
	if d == nil || d.client == nil {
		return errors.New("cache dao is not initialized")
	}
	if userID <= 0 {
		return nil
	}

	pattern := fmt.Sprintf("smartflow:memory_prefetch:u:%d:c:*", userID)
	var cursor uint64
	for {
		keys, next, err := d.client.Scan(ctx, cursor, pattern, 500).Result()
		if err != nil {
			return err
		}
		if len(keys) > 0 {
			// 1. UNLINK 是 DEL 的异步版本，不会阻塞 Redis 主线程；
			// 2. 即使 key 不存在也不会报错，幂等安全。
			if err := d.client.Unlink(ctx, keys...).Err(); err != nil {
				return err
			}
		}
		cursor = next
		if cursor == 0 {
			break
		}
	}
	return nil
}
