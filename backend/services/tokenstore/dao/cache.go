package dao

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/go-redis/redis/v8"
)

const (
	defaultCreditSnapshotTTL = 10 * time.Minute
	defaultCreditBlockedTTL  = 30 * time.Minute
)

// CreditBalanceSnapshot 是 TokenStore 在 Redis 中维护的余额快照。
type CreditBalanceSnapshot struct {
	UserID         uint64    `json:"user_id"`
	Balance        int64     `json:"balance"`
	TotalRecharged int64     `json:"total_recharged"`
	TotalRewarded  int64     `json:"total_rewarded"`
	TotalConsumed  int64     `json:"total_consumed"`
	UpdatedAt      time.Time `json:"updated_at"`
}

// CreditCacheDAO 只承载 Credit 余额快照相关的 Redis 能力。
type CreditCacheDAO struct {
	client *redis.Client
}

func NewCreditCacheDAO(client *redis.Client) *CreditCacheDAO {
	return &CreditCacheDAO{client: client}
}

func creditBalanceSnapshotKey(userID uint64) string {
	return fmt.Sprintf("smartflow:credit_balance_snapshot:%d", userID)
}

func creditBlockedKey(userID uint64) string {
	return fmt.Sprintf("smartflow:credit_blocked:%d", userID)
}

// SnapshotTTL 返回余额快照默认 TTL。
func (d *CreditCacheDAO) SnapshotTTL() time.Duration {
	return defaultCreditSnapshotTTL
}

// BlockedTTL 返回阻断标记默认 TTL。
func (d *CreditCacheDAO) BlockedTTL() time.Duration {
	return defaultCreditBlockedTTL
}

// GetCreditBalanceSnapshot 读取用户余额快照。
func (d *CreditCacheDAO) GetCreditBalanceSnapshot(ctx context.Context, userID uint64) (*CreditBalanceSnapshot, bool, error) {
	if d == nil || d.client == nil || userID == 0 {
		return nil, false, nil
	}

	val, err := d.client.Get(ctx, creditBalanceSnapshotKey(userID)).Result()
	if errors.Is(err, redis.Nil) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}

	var snapshot CreditBalanceSnapshot
	if err = json.Unmarshal([]byte(val), &snapshot); err != nil {
		return nil, false, err
	}
	return &snapshot, true, nil
}

// SetCreditBalanceSnapshot 写入用户余额快照。
func (d *CreditCacheDAO) SetCreditBalanceSnapshot(ctx context.Context, userID uint64, snapshot CreditBalanceSnapshot, ttl time.Duration) error {
	if d == nil || d.client == nil || userID == 0 {
		return nil
	}
	if ttl <= 0 {
		ttl = d.SnapshotTTL()
	}
	data, err := json.Marshal(snapshot)
	if err != nil {
		return err
	}
	return d.client.Set(ctx, creditBalanceSnapshotKey(userID), data, ttl).Err()
}

// DeleteCreditBalanceSnapshot 删除余额快照。
func (d *CreditCacheDAO) DeleteCreditBalanceSnapshot(ctx context.Context, userID uint64) error {
	if d == nil || d.client == nil || userID == 0 {
		return nil
	}
	return d.client.Del(ctx, creditBalanceSnapshotKey(userID)).Err()
}

// IsUserCreditBlocked 判断用户是否被阻断。
func (d *CreditCacheDAO) IsUserCreditBlocked(ctx context.Context, userID uint64) (bool, error) {
	if d == nil || d.client == nil || userID == 0 {
		return false, nil
	}
	result, err := d.client.Get(ctx, creditBlockedKey(userID)).Result()
	if errors.Is(err, redis.Nil) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return result == "1", nil
}

// SetUserCreditBlocked 写入用户阻断标记。
func (d *CreditCacheDAO) SetUserCreditBlocked(ctx context.Context, userID uint64, ttl time.Duration) error {
	if d == nil || d.client == nil || userID == 0 {
		return nil
	}
	if ttl <= 0 {
		ttl = d.BlockedTTL()
	}
	return d.client.Set(ctx, creditBlockedKey(userID), "1", ttl).Err()
}

// DeleteUserCreditBlocked 删除用户阻断标记。
func (d *CreditCacheDAO) DeleteUserCreditBlocked(ctx context.Context, userID uint64) error {
	if d == nil || d.client == nil || userID == 0 {
		return nil
	}
	return d.client.Del(ctx, creditBlockedKey(userID)).Err()
}
