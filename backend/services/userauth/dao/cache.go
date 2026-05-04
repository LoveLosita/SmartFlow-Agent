package dao

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/go-redis/redis/v8"
)

// TokenQuotaSnapshot 是 user/auth 服务内部的额度快照缓存结构。
type TokenQuotaSnapshot struct {
	TokenLimit  int       `json:"token_limit"`
	TokenUsage  int       `json:"token_usage"`
	LastResetAt time.Time `json:"last_reset_at"`
}

// CacheDAO 只承载 user/auth 领域需要的 Redis 能力。
type CacheDAO struct {
	client *redis.Client
}

func NewCacheDAO(client *redis.Client) *CacheDAO {
	return &CacheDAO{client: client}
}

func blacklistKey(jti string) string {
	return "blacklist:" + jti
}

func sessionBlacklistKey(sessionID string) string {
	return "session_blacklist:" + sessionID
}

func userTokenQuotaSnapshotKey(userID int) string {
	return fmt.Sprintf("smartflow:user_token_quota_snapshot:%d", userID)
}

func userTokenBlockedKey(userID int) string {
	return fmt.Sprintf("smartflow:user_token_blocked:%d", userID)
}

func (d *CacheDAO) SetBlacklist(jti string, expiration time.Duration) error {
	return d.client.Set(context.Background(), blacklistKey(jti), "1", expiration).Err()
}

// SetBlacklistIfAbsent 使用 Redis SET NX 原子抢占某个 JTI。
//
// 职责边界：
// 1. 用于 refresh token 轮转时保证旧 refresh 只能被消费一次；
// 2. 返回 ok=false 表示该 JTI 已经被其它请求消费过；
// 3. 不负责解析 JWT，也不负责判断 token 类型。
func (d *CacheDAO) SetBlacklistIfAbsent(jti string, expiration time.Duration) (bool, error) {
	return d.client.SetNX(context.Background(), blacklistKey(jti), "1", expiration).Result()
}

func (d *CacheDAO) IsBlacklisted(jti string) (bool, error) {
	result, err := d.client.Get(context.Background(), blacklistKey(jti)).Result()
	if errors.Is(err, redis.Nil) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return result == "1", nil
}

func (d *CacheDAO) SetSessionBlacklist(sessionID string, expiration time.Duration) error {
	return d.client.Set(context.Background(), sessionBlacklistKey(sessionID), "1", expiration).Err()
}

func (d *CacheDAO) IsSessionBlacklisted(sessionID string) (bool, error) {
	result, err := d.client.Get(context.Background(), sessionBlacklistKey(sessionID)).Result()
	if errors.Is(err, redis.Nil) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return result == "1", nil
}

func (d *CacheDAO) GetUserTokenQuotaSnapshot(ctx context.Context, userID int) (*TokenQuotaSnapshot, bool, error) {
	val, err := d.client.Get(ctx, userTokenQuotaSnapshotKey(userID)).Result()
	if errors.Is(err, redis.Nil) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}

	var snapshot TokenQuotaSnapshot
	if err = json.Unmarshal([]byte(val), &snapshot); err != nil {
		return nil, false, err
	}
	return &snapshot, true, nil
}

func (d *CacheDAO) SetUserTokenQuotaSnapshot(ctx context.Context, userID int, snapshot TokenQuotaSnapshot, ttl time.Duration) error {
	data, err := json.Marshal(snapshot)
	if err != nil {
		return err
	}
	return d.client.Set(ctx, userTokenQuotaSnapshotKey(userID), data, ttl).Err()
}

func (d *CacheDAO) DeleteUserTokenQuotaSnapshot(ctx context.Context, userID int) error {
	return d.client.Del(ctx, userTokenQuotaSnapshotKey(userID)).Err()
}

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

func (d *CacheDAO) SetUserTokenBlocked(ctx context.Context, userID int, ttl time.Duration) error {
	return d.client.Set(ctx, userTokenBlockedKey(userID), "1", ttl).Err()
}

func (d *CacheDAO) DeleteUserTokenBlocked(ctx context.Context, userID int) error {
	return d.client.Del(ctx, userTokenBlockedKey(userID)).Err()
}
