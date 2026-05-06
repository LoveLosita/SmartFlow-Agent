package dao

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/go-redis/redis/v8"
)

const defaultCreditSnapshotTTL = 10 * time.Minute

// CreditBalanceSnapshot 是 LLM 准入守卫读取的余额快照。
type CreditBalanceSnapshot struct {
	AvailableCredit int64     `json:"balance"`
	UpdatedAt       time.Time `json:"updated_at"`
}

// CacheDAO 只承载 LLM 服务私有的 Redis Key 读写。
type CacheDAO struct {
	client *redis.Client
}

func NewCacheDAO(client *redis.Client) *CacheDAO {
	return &CacheDAO{client: client}
}

func userCreditBalanceSnapshotKey(userID uint64) string {
	return fmt.Sprintf("smartflow:credit_balance_snapshot:%d", userID)
}

func userCreditBlockedKey(userID uint64) string {
	return fmt.Sprintf("smartflow:credit_blocked:%d", userID)
}

func (d *CacheDAO) GetUserCreditBalanceSnapshot(ctx context.Context, userID uint64) (*CreditBalanceSnapshot, bool, error) {
	if d == nil || d.client == nil || userID == 0 {
		return nil, false, nil
	}

	value, err := d.client.Get(ctx, userCreditBalanceSnapshotKey(userID)).Result()
	if errors.Is(err, redis.Nil) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}

	var snapshot CreditBalanceSnapshot
	if err = json.Unmarshal([]byte(value), &snapshot); err != nil {
		return nil, false, err
	}
	return &snapshot, true, nil
}

func (d *CacheDAO) SetUserCreditBalanceSnapshot(ctx context.Context, userID uint64, snapshot CreditBalanceSnapshot, ttl time.Duration) error {
	if d == nil || d.client == nil || userID == 0 {
		return nil
	}
	if ttl <= 0 {
		ttl = defaultCreditSnapshotTTL
	}

	raw, err := json.Marshal(snapshot)
	if err != nil {
		return err
	}
	return d.client.Set(ctx, userCreditBalanceSnapshotKey(userID), raw, ttl).Err()
}

func (d *CacheDAO) DeleteUserCreditBalanceSnapshot(ctx context.Context, userID uint64) error {
	if d == nil || d.client == nil || userID == 0 {
		return nil
	}
	return d.client.Del(ctx, userCreditBalanceSnapshotKey(userID)).Err()
}

func (d *CacheDAO) IsUserCreditBlocked(ctx context.Context, userID uint64) (bool, error) {
	if d == nil || d.client == nil || userID == 0 {
		return false, nil
	}

	value, err := d.client.Get(ctx, userCreditBlockedKey(userID)).Result()
	if errors.Is(err, redis.Nil) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return value == "1", nil
}

func (d *CacheDAO) SetUserCreditBlocked(ctx context.Context, userID uint64, ttl time.Duration) error {
	if d == nil || d.client == nil || userID == 0 {
		return nil
	}
	return d.client.Set(ctx, userCreditBlockedKey(userID), "1", ttl).Err()
}

func (d *CacheDAO) DeleteUserCreditBlocked(ctx context.Context, userID uint64) error {
	if d == nil || d.client == nil || userID == 0 {
		return nil
	}
	return d.client.Del(ctx, userCreditBlockedKey(userID)).Err()
}
