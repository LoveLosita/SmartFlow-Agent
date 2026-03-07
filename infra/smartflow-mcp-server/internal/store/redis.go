package store

import (
	"context"
	"fmt"
	"strings"
	"time"

	cfgpkg "github.com/LoveLosita/smartflow/infra/smartflow-mcp-server/internal/config"
	"github.com/go-redis/redis/v8"
)

type RedisClient struct {
	client *redis.Client
}

type RedisGetResult struct {
	Exists     bool   `json:"exists"`
	Key        string `json:"key"`
	Type       string `json:"type"`
	Value      any    `json:"value,omitempty"`
	Truncated  bool   `json:"truncated"`
	DurationMs int64  `json:"durationMs"`
}

type RedisScanResult struct {
	Pattern    string   `json:"pattern"`
	Keys       []string `json:"keys"`
	Returned   int      `json:"returned"`
	NextCursor uint64   `json:"nextCursor"`
	Truncated  bool     `json:"truncated"`
	DurationMs int64    `json:"durationMs"`
}

func NewRedisClient(ctx context.Context, cfg cfgpkg.RedisConfig) (*RedisClient, error) {
	client := redis.NewClient(&redis.Options{
		Addr:     cfg.Addr,
		Password: cfg.Password,
		DB:       cfg.DB,
	})

	pingCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	if err := client.Ping(pingCtx).Err(); err != nil {
		_ = client.Close()
		return nil, fmt.Errorf("ping redis: %w", err)
	}
	return &RedisClient{client: client}, nil
}

func (c *RedisClient) Close() error {
	if c == nil || c.client == nil {
		return nil
	}
	return c.client.Close()
}

func (c *RedisClient) GetWithType(ctx context.Context, key string, maxItems int, maxStringBytes int) (RedisGetResult, error) {
	start := time.Now()
	t, err := c.client.Type(ctx, key).Result()
	if err != nil {
		return RedisGetResult{}, err
	}
	if t == "none" {
		return RedisGetResult{
			Exists:     false,
			Key:        key,
			Type:       "none",
			Truncated:  false,
			DurationMs: time.Since(start).Milliseconds(),
		}, nil
	}

	result := RedisGetResult{Exists: true, Key: key, Type: t}
	switch t {
	case "string":
		v, err := c.client.Get(ctx, key).Result()
		if err != nil {
			return RedisGetResult{}, err
		}
		if len(v) > maxStringBytes {
			result.Value = v[:maxStringBytes]
			result.Truncated = true
		} else {
			result.Value = v
		}
	case "list":
		vals, err := c.client.LRange(ctx, key, 0, int64(maxItems-1)).Result()
		if err != nil {
			return RedisGetResult{}, err
		}
		result.Value = vals
		if length, _ := c.client.LLen(ctx, key).Result(); length > int64(maxItems) {
			result.Truncated = true
		}
	case "set":
		vals, err := c.client.SMembers(ctx, key).Result()
		if err != nil {
			return RedisGetResult{}, err
		}
		if len(vals) > maxItems {
			result.Value = vals[:maxItems]
			result.Truncated = true
		} else {
			result.Value = vals
		}
	case "zset":
		vals, err := c.client.ZRangeWithScores(ctx, key, 0, int64(maxItems-1)).Result()
		if err != nil {
			return RedisGetResult{}, err
		}
		resultRows := make([]map[string]any, 0, len(vals))
		for _, item := range vals {
			resultRows = append(resultRows, map[string]any{"member": item.Member, "score": item.Score})
		}
		result.Value = resultRows
		if length, _ := c.client.ZCard(ctx, key).Result(); length > int64(maxItems) {
			result.Truncated = true
		}
	case "hash":
		vals, err := c.client.HGetAll(ctx, key).Result()
		if err != nil {
			return RedisGetResult{}, err
		}
		if len(vals) <= maxItems {
			result.Value = vals
		} else {
			trimmed := make(map[string]string, maxItems)
			count := 0
			for k, v := range vals {
				trimmed[k] = v
				count++
				if count >= maxItems {
					break
				}
			}
			result.Value = trimmed
			result.Truncated = true
		}
	default:
		raw, err := c.client.Dump(ctx, key).Result()
		if err != nil {
			return RedisGetResult{}, err
		}
		result.Value = strings.ToUpper(fmt.Sprintf("UNSUPPORTED_TYPE_%s_DUMP_SIZE_%d", t, len(raw)))
	}
	result.DurationMs = time.Since(start).Milliseconds()
	return result, nil
}

func (c *RedisClient) ScanKeys(ctx context.Context, pattern string, count int64, maxKeys int) (RedisScanResult, error) {
	start := time.Now()
	if pattern == "" {
		pattern = "*"
	}
	if count <= 0 {
		count = 20
	}

	keys := make([]string, 0, maxKeys)
	var cursor uint64
	truncated := false
	for {
		batch, nextCursor, err := c.client.Scan(ctx, cursor, pattern, count).Result()
		if err != nil {
			return RedisScanResult{}, err
		}
		for _, key := range batch {
			if len(keys) >= maxKeys {
				truncated = true
				break
			}
			keys = append(keys, key)
		}
		cursor = nextCursor
		if truncated || cursor == 0 {
			break
		}
	}

	return RedisScanResult{
		Pattern:    pattern,
		Keys:       keys,
		Returned:   len(keys),
		NextCursor: cursor,
		Truncated:  truncated,
		DurationMs: time.Since(start).Milliseconds(),
	}, nil
}
