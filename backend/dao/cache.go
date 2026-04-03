package dao

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/LoveLosita/smartflow/backend/model"
	"github.com/go-redis/redis/v8"
)

type CacheDAO struct {
	client *redis.Client
}

// UserTokenQuotaSnapshot 是“用户额度判断”的 Redis 快照结构。
//
// 设计说明：
// 1. 只保留额度判断必要字段，避免把 users 全字段塞进缓存；
// 2. 该结构仅用于“快速门禁判断”，权威账本仍以 MySQL 为准。
type UserTokenQuotaSnapshot struct {
	TokenLimit  int       `json:"token_limit"`
	TokenUsage  int       `json:"token_usage"`
	LastResetAt time.Time `json:"last_reset_at"`
}

func NewCacheDAO(client *redis.Client) *CacheDAO {
	return &CacheDAO{client: client}
}

func (d *CacheDAO) schedulePreviewKey(userID int, conversationID string) string {
	return fmt.Sprintf("smartflow:schedule_preview:u:%d:c:%s", userID, conversationID)
}

func (d *CacheDAO) conversationHistoryKey(userID int, conversationID string) string {
	return fmt.Sprintf("smartflow:conversation_history:u:%d:c:%s", userID, conversationID)
}

// SetBlacklist 把 Token 写入黑名单。
func (d *CacheDAO) SetBlacklist(jti string, expiration time.Duration) error {
	return d.client.Set(context.Background(), "blacklist:"+jti, "1", expiration).Err()
}

// IsBlacklisted 检查 Token 是否在黑名单中。
func (d *CacheDAO) IsBlacklisted(jti string) (bool, error) {
	result, err := d.client.Get(context.Background(), "blacklist:"+jti).Result()
	if errors.Is(err, redis.Nil) {
		return false, nil // 不在黑名单中
	} else if err != nil {
		return false, err // 其他错误
	}
	return result == "1", nil // 在黑名单中
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

func userTokenQuotaSnapshotKey(userID int) string {
	return fmt.Sprintf("smartflow:user_token_quota_snapshot:%d", userID)
}

func userTokenBlockedKey(userID int) string {
	return fmt.Sprintf("smartflow:user_token_blocked:%d", userID)
}

// GetUserTokenQuotaSnapshot 读取用户 token 配额快照。
//
// 输入输出语义：
// 1. 命中返回 (*UserTokenQuotaSnapshot, true, nil)；
// 2. 未命中返回 (nil, false, nil)；
// 3. Redis/反序列化错误返回 (nil, false, err)。
func (d *CacheDAO) GetUserTokenQuotaSnapshot(ctx context.Context, userID int) (*UserTokenQuotaSnapshot, bool, error) {
	key := userTokenQuotaSnapshotKey(userID)
	val, err := d.client.Get(ctx, key).Result()
	if errors.Is(err, redis.Nil) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}

	var snapshot UserTokenQuotaSnapshot
	if err = json.Unmarshal([]byte(val), &snapshot); err != nil {
		return nil, false, err
	}
	return &snapshot, true, nil
}

// SetUserTokenQuotaSnapshot 写入用户 token 配额快照。
//
// 职责边界：
// 1. 只做缓存写入，不做额度判断；
// 2. ttl 由上层策略控制，便于按场景调优“性能 vs 一致性”。
func (d *CacheDAO) SetUserTokenQuotaSnapshot(ctx context.Context, userID int, snapshot UserTokenQuotaSnapshot, ttl time.Duration) error {
	key := userTokenQuotaSnapshotKey(userID)
	data, err := json.Marshal(snapshot)
	if err != nil {
		return err
	}
	return d.client.Set(ctx, key, data, ttl).Err()
}

// DeleteUserTokenQuotaSnapshot 删除用户 token 快照缓存。
func (d *CacheDAO) DeleteUserTokenQuotaSnapshot(ctx context.Context, userID int) error {
	return d.client.Del(ctx, userTokenQuotaSnapshotKey(userID)).Err()
}

// IsUserTokenBlocked 检查用户是否被“额度封禁键”命中。
func (d *CacheDAO) IsUserTokenBlocked(ctx context.Context, userID int) (bool, error) {
	result, err := d.client.Get(ctx, userTokenBlockedKey(userID)).Result()
	if errors.Is(err, redis.Nil) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return result == "1", nil
}

// SetUserTokenBlocked 设置用户“额度封禁键”。
//
// 说明：
// 1. 该键是快速拦截层，不是权威账本；
// 2. ttl 建议设置到“下一次重置时间”，到期自动解封。
func (d *CacheDAO) SetUserTokenBlocked(ctx context.Context, userID int, ttl time.Duration) error {
	return d.client.Set(ctx, userTokenBlockedKey(userID), "1", ttl).Err()
}

// DeleteUserTokenBlocked 清理用户“额度封禁键”。
func (d *CacheDAO) DeleteUserTokenBlocked(ctx context.Context, userID int) error {
	return d.client.Del(ctx, userTokenBlockedKey(userID)).Err()
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

// SetConversationHistoryToCache 写入“会话历史视图”缓存。
//
// 职责边界：
// 1. 负责按 user_id + conversation_id 写入前端历史查询所需的稳定 DTO；
// 2. 只负责缓存当前可展示历史，不负责上下文窗口缓存；
// 3. 不负责 DB 回源，也不负责重试分组补算。
func (d *CacheDAO) SetConversationHistoryToCache(ctx context.Context, userID int, conversationID string, items []model.GetConversationHistoryItem) error {
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

	data, err := json.Marshal(items)
	if err != nil {
		return fmt.Errorf("marshal conversation history failed: %w", err)
	}
	return d.client.Set(ctx, d.conversationHistoryKey(userID, normalizedConversationID), data, 1*time.Hour).Err()
}

// GetConversationHistoryFromCache 读取“会话历史视图”缓存。
//
// 输入输出语义：
// 1. 命中时返回历史 DTO 切片与 nil error；
// 2. 未命中时返回 (nil, nil)；
// 3. Redis 异常或反序列化失败时返回 error。
func (d *CacheDAO) GetConversationHistoryFromCache(ctx context.Context, userID int, conversationID string) ([]model.GetConversationHistoryItem, error) {
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

	raw, err := d.client.Get(ctx, d.conversationHistoryKey(userID, normalizedConversationID)).Result()
	if err == redis.Nil {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	var items []model.GetConversationHistoryItem
	if err = json.Unmarshal([]byte(raw), &items); err != nil {
		return nil, fmt.Errorf("unmarshal conversation history failed: %w", err)
	}
	return items, nil
}

// DeleteConversationHistoryFromCache 删除“会话历史视图”缓存。
//
// 说明：
// 1. 删除操作是幂等的，key 不存在也视为成功；
// 2. 该方法用于 chat_histories 写入/补种 retry 分组后触发失效；
// 3. 这里只处理前端历史视图缓存，不影响 Agent 上下文热缓存。
func (d *CacheDAO) DeleteConversationHistoryFromCache(ctx context.Context, userID int, conversationID string) error {
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
	return d.client.Del(ctx, d.conversationHistoryKey(userID, normalizedConversationID)).Err()
}

// agentStateKey 返回 agent 运行态快照的 Redis key。
//
// Key 设计：
// 1. 使用 smartflow:agent_state 前缀，与现有 key 命名空间隔离；
// 2. 使用 conversationID 作为唯一标识，因为 agent 状态是按会话维度持久化的。
func (d *CacheDAO) agentStateKey(conversationID string) string {
	return fmt.Sprintf("smartflow:agent_state:%s", conversationID)
}

// SaveAgentState 序列化并保存 agent 运行态快照到 Redis。
//
// 职责边界：
// 1. 只负责 JSON 序列化 + Redis SET，不做业务校验；
// 2. TTL 默认 24h，过期自动清理，避免已完成任务的快照堆积；
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
	return d.client.Set(ctx, d.agentStateKey(normalizedID), data, 24*time.Hour).Err()
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
