package dao

import (
	"context"
	"errors"
	"time"

	"github.com/go-redis/redis/v8"
)

type CacheDAO struct {
	client *redis.Client
}

func NewCacheDAO(client *redis.Client) *CacheDAO {
	return &CacheDAO{client: client}
}

// SetBlacklist 把 Token 扔进黑名单
func (dao *CacheDAO) SetBlacklist(jti string, expiration time.Duration) error {
	return dao.client.Set(context.Background(), "blacklist:"+jti, "1", expiration).Err()
}

// IsBlacklisted 检查 Token 是否在黑名单中
func (dao *CacheDAO) IsBlacklisted(jti string) (bool, error) {
	result, err := dao.client.Get(context.Background(), "blacklist:"+jti).Result()
	if errors.Is(err, redis.Nil) {
		return false, nil // 不在黑名单
	} else if err != nil {
		return false, err // 其他错误
	}
	return result == "1", nil // 在黑名单
}
