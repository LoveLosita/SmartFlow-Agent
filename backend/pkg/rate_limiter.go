package pkg

import (
	"context"
	"time"

	"github.com/go-redis/redis/v8"
)

var tokenBucketScript = redis.NewScript(`-- KEYS[1]: 限流标识 (如 rate_limit:user_123)
-- ARGV[1]: 令牌桶最大容量 (Capacity)
-- ARGV[2]: 令牌填充速率 (Tokens per second)
-- ARGV[3]: 当前时间戳 (Current Unix timestamp in seconds)
-- ARGV[4]: 请求需要的令牌数 (通常为 1)

local bucket_info = redis.call("HMGET", KEYS[1], "last_tokens", "last_refreshed")
local last_tokens = tonumber(bucket_info[1])
local last_refreshed = tonumber(bucket_info[2])

local capacity = tonumber(ARGV[1])
local rate = tonumber(ARGV[2])
local now = tonumber(ARGV[3])
local requested = tonumber(ARGV[4])

-- 如果是首次访问，初始化桶
if last_tokens == nil then
    last_tokens = capacity
    last_refreshed = now
end

-- 💡 核心逻辑：计算这段时间新产生的令牌
local delta = math.max(0, now - last_refreshed)
local new_tokens = math.min(capacity, last_tokens + (delta * rate))

local allowed = false
if new_tokens >= requested then
    new_tokens = new_tokens - requested
    allowed = true
end

-- 更新 Redis 状态
redis.call("HMSET", KEYS[1], "last_tokens", new_tokens, "last_refreshed", now)
-- 设置过期时间（比如 1 小时没人访问就删掉，省内存）
redis.call("EXPIRE", KEYS[1], 3600)

return allowed and 1 or 0`)

type RateLimiter struct {
	client *redis.Client
}

func NewRateLimiter(client *redis.Client) *RateLimiter {
	return &RateLimiter{client: client}
}

func (r *RateLimiter) Allow(ctx context.Context, key string, capacity, rate int) (bool, error) {
	// 传参：Key, 容量, 速率, 当前时间, 请求数
	res, err := tokenBucketScript.Run(ctx, r.client, []string{key},
		capacity, rate, time.Now().Unix(), 1).Int()
	if err != nil {
		return false, err
	}
	return res == 1, nil
}
