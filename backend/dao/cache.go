package dao

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/LoveLosita/smartflow/backend/model"
	"github.com/go-redis/redis/v8"
)

type CacheDAO struct {
	client *redis.Client
}

func NewCacheDAO(client *redis.Client) *CacheDAO {
	return &CacheDAO{client: client}
}

// SetBlacklist 鎶?Token 鎵旇繘榛戝悕鍗?
func (d *CacheDAO) SetBlacklist(jti string, expiration time.Duration) error {
	return d.client.Set(context.Background(), "blacklist:"+jti, "1", expiration).Err()
}

// IsBlacklisted 妫€鏌?Token 鏄惁鍦ㄩ粦鍚嶅崟涓?
func (d *CacheDAO) IsBlacklisted(jti string) (bool, error) {
	result, err := d.client.Get(context.Background(), "blacklist:"+jti).Result()
	if errors.Is(err, redis.Nil) {
		return false, nil // 涓嶅湪榛戝悕鍗?
	} else if err != nil {
		return false, err // 鍏朵粬閿欒
	}
	return result == "1", nil // 鍦ㄩ粦鍚嶅崟
}

func (d *CacheDAO) AddTaskClassList(ctx context.Context, userID int, list *model.UserGetTaskClassesResponse) error {
	// 1. 瀹氫箟 Key锛屼娇鐢?userID 闅旂涓嶅悓鐢ㄦ埛鐨勬暟鎹?
	key := fmt.Sprintf("smartflow:task_classes:%d", userID)
	// 2. 搴忓垪鍖栵細灏嗙粨鏋勪綋杞负 []byte
	data, err := json.Marshal(list)
	if err != nil {
		return err
	}
	// 3. 瀛樺偍锛氳缃?30 鍒嗛挓杩囨湡锛堟牴鎹笟鍔＄伒娲昏皟鏁达級
	return d.client.Set(ctx, key, data, 30*time.Minute).Err()
}

func (d *CacheDAO) GetTaskClassList(ctx context.Context, userID int) (*model.UserGetTaskClassesResponse, error) {
	key := fmt.Sprintf("smartflow:task_classes:%d", userID)
	var resp model.UserGetTaskClassesResponse
	// 1. 浠?Redis 鑾峰彇瀛楃涓?
	val, err := d.client.Get(ctx, key).Result()
	if err != nil {
		// 娉ㄦ剰锛氬鏋滄槸 redis.Nil锛屼氦缁?Service 灞傚鐞嗘煡搴撻€昏緫
		return &resp, err
	}
	// 2. 鍙嶅簭鍒楀寲锛氬皢 JSON 杩樺師鍥炵粨鏋勪綋
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
		return "", nil // 姝ｅ父娌″懡涓殑鎯呭喌
	}
	return val, err // 鐪熸鐨?Redis 鎶ラ敊
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
		return nil, err // 娉ㄦ剰锛氬鏋滄槸 redis.Nil锛屼氦缁?Service 灞傚鐞嗘煡搴撻€昏緫
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
		return nil, err // 娉ㄦ剰锛氬鏋滄槸 redis.Nil锛屼氦缁?Service 灞傚鐞嗘煡搴撻€昏緫
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
	// 璁剧疆杩囨湡鏃堕棿涓哄綋澶╁墿浣欑殑鏃堕棿锛岀‘淇濇瘡澶╂洿鏂颁竴娆＄紦瀛?
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
		return nil, err // 娉ㄦ剰锛氬鏋滄槸 redis.Nil锛屼氦缁?Service 灞傚鐞嗘煡搴撻€昏緫
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
	// 璁剧疆杩囨湡鏃堕棿涓轰竴澶?
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
		return &resp, err // 娉ㄦ剰锛氬鏋滄槸 redis.Nil锛屼氦缁?Service 灞傚鐞嗘煡搴撻€昏緫
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
	// 璁剧疆杩囨湡鏃堕棿涓?0鍒嗛挓
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
			// 鐢?UNLINK\(\) 寮傛鍒犻櫎锛岄檷浣庨樆濉為闄╋紱濡傞渶寮轰竴鑷村垹闄ゅ彲鏀圭敤 Del\(\)
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
		return &schedule, err // 娉ㄦ剰锛氬鏋滄槸 redis.Nil锛屼氦缁?Service 灞傚鐞嗘煡搴撻€昏緫
	}
	if val == "null" {
		return nil, nil // 涔嬪墠缂撳瓨杩囨病鏈夋鍦ㄨ繘琛岀殑鏃ョ▼锛岀洿鎺ヨ繑鍥?nil
	}
	err = json.Unmarshal([]byte(val), &schedule)
	return &schedule, err
}

func (d *CacheDAO) SetUserOngoingScheduleToCache(ctx context.Context, userID int, schedule *model.OngoingSchedule) error {
	if schedule == nil {
		// 濡傛灉娌℃湁姝ｅ湪杩涜鐨勬棩绋嬶紝璁剧疆绌哄€煎苟鐭殏杩囨湡锛岄伩鍏嶉绻佹煡搴?
		key := fmt.Sprintf("smartflow:ongoing_schedule:%d", userID)
		return d.client.Set(ctx, key, "null", 5*time.Minute).Err()
	}
	key := fmt.Sprintf("smartflow:ongoing_schedule:%d", userID)
	data, err := json.Marshal(schedule)
	if err != nil {
		return err
	}
	// 璁剧疆杩囨湡鏃堕棿涓哄埌 endTime 鐨勫墿浣欐椂闂达紙鑻ュ凡杩囨湡鍒欎笉鍐欏叆缂撳瓨锛?
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
