package tools

import (
	"context"
	"fmt"

	"github.com/LoveLosita/smartflow/infra/smartflow-mcp-server/internal/store"
)

type RedisGetTool struct {
	client         *store.RedisClient
	valueMaxItems  int
	maxStringBytes int
}

func NewRedisGetTool(client *store.RedisClient, valueMaxItems int, maxStringBytes int) *RedisGetTool {
	return &RedisGetTool{client: client, valueMaxItems: valueMaxItems, maxStringBytes: maxStringBytes}
}

func (t *RedisGetTool) Name() string {
	return "redis_get"
}

func (t *RedisGetTool) Description() string {
	return "Get a Redis key by name and return its type and value."
}

func (t *RedisGetTool) InputSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"key": map[string]any{
				"type":        "string",
				"description": "Redis key",
			},
		},
		"required":             []string{"key"},
		"additionalProperties": false,
	}
}

func (t *RedisGetTool) Execute(ctx context.Context, args map[string]any) (map[string]any, error) {
	key, ok := args["key"].(string)
	if !ok || key == "" {
		return nil, fmt.Errorf("key must be a non-empty string")
	}
	res, err := t.client.GetWithType(ctx, key, t.valueMaxItems, t.maxStringBytes)
	if err != nil {
		return nil, err
	}
	return map[string]any{
		"exists":     res.Exists,
		"key":        res.Key,
		"type":       res.Type,
		"value":      res.Value,
		"truncated":  res.Truncated,
		"durationMs": res.DurationMs,
	}, nil
}

type RedisScanTool struct {
	client       *store.RedisClient
	maxKeys      int
	maxScanCount int
}

func NewRedisScanTool(client *store.RedisClient, maxKeys int, maxScanCount int) *RedisScanTool {
	return &RedisScanTool{client: client, maxKeys: maxKeys, maxScanCount: maxScanCount}
}

func (t *RedisScanTool) Name() string {
	return "redis_scan"
}

func (t *RedisScanTool) Description() string {
	return "Scan Redis keys by pattern with capped result size."
}

func (t *RedisScanTool) InputSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"pattern": map[string]any{
				"type":        "string",
				"description": "Pattern, for example user:*",
			},
			"count": map[string]any{
				"type":        "number",
				"description": "Optional scan count hint",
			},
		},
		"required":             []string{"pattern"},
		"additionalProperties": false,
	}
}

func (t *RedisScanTool) Execute(ctx context.Context, args map[string]any) (map[string]any, error) {
	pattern, ok := args["pattern"].(string)
	if !ok || pattern == "" {
		return nil, fmt.Errorf("pattern must be a non-empty string")
	}

	count := int64(20)
	if rawCount, ok := args["count"]; ok {
		number, ok := rawCount.(float64)
		if !ok {
			return nil, fmt.Errorf("count must be a number")
		}
		if number <= 0 {
			return nil, fmt.Errorf("count must be > 0")
		}
		count = int64(number)
	}
	if count > int64(t.maxScanCount) {
		count = int64(t.maxScanCount)
	}

	res, err := t.client.ScanKeys(ctx, pattern, count, t.maxKeys)
	if err != nil {
		return nil, err
	}
	return map[string]any{
		"pattern":    res.Pattern,
		"keys":       res.Keys,
		"returned":   res.Returned,
		"nextCursor": res.NextCursor,
		"truncated":  res.Truncated,
		"durationMs": res.DurationMs,
	}, nil
}
