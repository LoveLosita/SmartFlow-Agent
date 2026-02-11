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

func (d *CacheDAO) GetTaskClassList(ctx context.Context, userID int) (model.UserGetTaskClassesResponse, error) {
	key := fmt.Sprintf("smartflow:task_classes:%d", userID)
	var resp model.UserGetTaskClassesResponse
	// 1. 从 Redis 获取字符串
	val, err := d.client.Get(ctx, key).Result()
	if err != nil {
		// 注意：如果是 redis.Nil，交给 Service 层处理查库逻辑
		return resp, err
	}
	// 2. 反序列化：将 JSON 还原回结构体
	err = json.Unmarshal([]byte(val), &resp)
	return resp, err
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
