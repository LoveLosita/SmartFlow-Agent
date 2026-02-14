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

// SetBlacklist 把 Token 扔进黑名单
func (d *CacheDAO) SetBlacklist(jti string, expiration time.Duration) error {
	return d.client.Set(context.Background(), "blacklist:"+jti, "1", expiration).Err()
}

// IsBlacklisted 检查 Token 是否在黑名单中
func (d *CacheDAO) IsBlacklisted(jti string) (bool, error) {
	result, err := d.client.Get(context.Background(), "blacklist:"+jti).Result()
	if errors.Is(err, redis.Nil) {
		return false, nil // 不在黑名单
	} else if err != nil {
		return false, err // 其他错误
	}
	return result == "1", nil // 在黑名单
}

func (d *CacheDAO) AddTaskClassList(ctx context.Context, userID int, list *model.UserGetTaskClassesResponse) error {
	// 1. 定义 Key，使用 userID 隔离不同用户的数据
	key := fmt.Sprintf("smartflow:task_classes:%d", userID)
	// 2. 序列化：将结构体转为 []byte
	data, err := json.Marshal(list)
	if err != nil {
		return err
	}
	// 3. 存储：设置 30 分钟过期（根据业务灵活调整）
	return d.client.Set(ctx, key, data, 30*time.Minute).Err()
}

func (d *CacheDAO) GetTaskClassList(ctx context.Context, userID int) (*model.UserGetTaskClassesResponse, error) {
	key := fmt.Sprintf("smartflow:task_classes:%d", userID)
	var resp model.UserGetTaskClassesResponse
	// 1. 从 Redis 获取字符串
	val, err := d.client.Get(ctx, key).Result()
	if err != nil {
		// 注意：如果是 redis.Nil，交给 Service 层处理查库逻辑
		return &resp, err
	}
	// 2. 反序列化：将 JSON 还原回结构体
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
		return "", nil // 正常没命中的情况
	}
	return val, err // 真正的 Redis 报错
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

func (d *CacheDAO) GetUserTasksFromCache(ctx context.Context, userID int) ([]model.GetUserTaskResp, error) {
	key := fmt.Sprintf("smartflow:tasks:%d", userID)
	var tasks []model.GetUserTaskResp
	val, err := d.client.Get(ctx, key).Result()
	if err != nil {
		return nil, err // 注意：如果是 redis.Nil，交给 Service 层处理查库逻辑
	}
	err = json.Unmarshal([]byte(val), &tasks)
	return tasks, err
}

func (d *CacheDAO) SetUserTasksToCache(ctx context.Context, userID int, tasks []model.GetUserTaskResp) error {
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
		return nil, err // 注意：如果是 redis.Nil，交给 Service 层处理查库逻辑
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
	// 设置过期时间为当天剩余的时间，确保每天更新一次缓存
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
		return nil, err // 注意：如果是 redis.Nil，交给 Service 层处理查库逻辑
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
	// 设置过期时间为一天
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
		return &resp, err // 注意：如果是 redis.Nil，交给 Service 层处理查库逻辑
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
	// 设置过期时间为30分钟
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
			// 用 UNLINK\(\) 异步删除，降低阻塞风险；如需强一致删除可改用 Del\(\)
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
		return &schedule, err // 注意：如果是 redis.Nil，交给 Service 层处理查库逻辑
	}
	if val == "null" {
		return nil, nil // 之前缓存过没有正在进行的日程，直接返回 nil
	}
	err = json.Unmarshal([]byte(val), &schedule)
	return &schedule, err
}

func (d *CacheDAO) SetUserOngoingScheduleToCache(ctx context.Context, userID int, schedule *model.OngoingSchedule) error {
	if schedule == nil {
		// 如果没有正在进行的日程，设置空值并短暂过期，避免频繁查库
		key := fmt.Sprintf("smartflow:ongoing_schedule:%d", userID)
		return d.client.Set(ctx, key, "null", 5*time.Minute).Err()
	}
	key := fmt.Sprintf("smartflow:ongoing_schedule:%d", userID)
	data, err := json.Marshal(schedule)
	if err != nil {
		return err
	}
	// 设置过期时间为到 endTime 的剩余时间（若已过期则不写入缓存）
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
