package dao

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"time"

	"github.com/cloudwego/eino/schema"
	"github.com/go-redis/redis/v8"
)

type AgentCache struct {
	client *redis.Client
	// 默认窗口大小（会被会话级动态窗口覆盖）
	windowSize int
	// 缓存过期时间
	expiration time.Duration
}

const (
	minHistoryWindowSize = 16
	maxHistoryWindowSize = 4096
)

func NewAgentCache(client *redis.Client) *AgentCache {
	return &AgentCache{
		client:     client,
		windowSize: 128,
		expiration: 1 * time.Hour,
	}
}

func (m *AgentCache) historyKey(sessionID string) string {
	return fmt.Sprintf("smartflow:history:%s", sessionID)
}

func (m *AgentCache) historyWindowKey(sessionID string) string {
	return fmt.Sprintf("smartflow:history_window:%s", sessionID)
}

func (m *AgentCache) normalizeWindowSize(size int) int {
	if size < minHistoryWindowSize {
		return minHistoryWindowSize
	}
	if size > maxHistoryWindowSize {
		return maxHistoryWindowSize
	}
	return size
}

func (m *AgentCache) getSessionWindowSize(ctx context.Context, sessionID string) (int, error) {
	windowKey := m.historyWindowKey(sessionID)
	val, err := m.client.Get(ctx, windowKey).Result()
	if err == redis.Nil {
		return m.windowSize, nil
	}
	if err != nil {
		return 0, err
	}
	size, convErr := strconv.Atoi(val)
	if convErr != nil {
		return m.windowSize, nil
	}
	return m.normalizeWindowSize(size), nil
}

// SetSessionWindowSize 设置会话级窗口上限。
func (m *AgentCache) SetSessionWindowSize(ctx context.Context, sessionID string, size int) error {
	normalized := m.normalizeWindowSize(size)
	windowKey := m.historyWindowKey(sessionID)
	return m.client.Set(ctx, windowKey, normalized, m.expiration).Err()
}

// EnforceHistoryWindow 按当前会话窗口强制修剪历史队列。
func (m *AgentCache) EnforceHistoryWindow(ctx context.Context, sessionID string) error {
	size, err := m.getSessionWindowSize(ctx, sessionID)
	if err != nil {
		return err
	}
	key := m.historyKey(sessionID)
	pipe := m.client.Pipeline()
	pipe.LTrim(ctx, key, 0, int64(size-1))
	pipe.Expire(ctx, key, m.expiration)
	_, err = pipe.Exec(ctx)
	return err
}

func (m *AgentCache) PushMessage(ctx context.Context, sessionID string, msg *schema.Message) error {
	key := m.historyKey(sessionID)
	size, err := m.getSessionWindowSize(ctx, sessionID)
	if err != nil {
		return err
	}

	// 1. 序列化 Eino 消息
	data, err := json.Marshal(msg)
	if err != nil {
		return fmt.Errorf("marshal message failed: %w", err)
	}

	// 2. 利用 Pipeline 保证原子操作
	pipe := m.client.Pipeline()
	// 往左侧推入最新消息（LIFO）
	pipe.LPush(ctx, key, data)
	// 只保留最新 size 条
	pipe.LTrim(ctx, key, 0, int64(size-1))
	pipe.Expire(ctx, key, m.expiration)

	_, err = pipe.Exec(ctx)
	return err
}

func (m *AgentCache) GetHistory(ctx context.Context, sessionID string) ([]*schema.Message, error) {
	key := m.historyKey(sessionID)

	vals, err := m.client.LRange(ctx, key, 0, -1).Result()
	if err != nil {
		return nil, err
	}
	if len(vals) == 0 {
		return nil, nil
	}

	messages := make([]*schema.Message, len(vals))
	for i, val := range vals {
		var msg schema.Message
		if err := json.Unmarshal([]byte(val), &msg); err != nil {
			return nil, err
		}
		// LRANGE 返回 [最新..最旧]，这里反转成 [最旧..最新]
		messages[len(vals)-1-i] = &msg
	}

	return messages, nil
}

// BackfillHistory 在缓存失效时，把历史消息一次性回填到 Redis。
func (m *AgentCache) BackfillHistory(ctx context.Context, sessionID string, messages []*schema.Message) error {
	key := m.historyKey(sessionID)
	size, err := m.getSessionWindowSize(ctx, sessionID)
	if err != nil {
		return err
	}

	if len(messages) == 0 {
		return m.client.Del(ctx, key).Err()
	}

	values := make([]interface{}, len(messages))
	for i, msg := range messages {
		data, err := json.Marshal(msg)
		if err != nil {
			return fmt.Errorf("marshal failed at index %d: %w", i, err)
		}
		values[i] = data
	}

	pipe := m.client.Pipeline()
	pipe.Del(ctx, key)
	// 输入是 [最旧..最新]，LPUSH 后变成 [最新..最旧]
	pipe.LPush(ctx, key, values...)
	pipe.LTrim(ctx, key, 0, int64(size-1))
	pipe.Expire(ctx, key, m.expiration)

	_, err = pipe.Exec(ctx)
	return err
}

func (m *AgentCache) ClearHistory(ctx context.Context, sessionID string) error {
	historyKey := m.historyKey(sessionID)
	windowKey := m.historyWindowKey(sessionID)
	return m.client.Del(ctx, historyKey, windowKey).Err()
}

func (m *AgentCache) GetConversationStatus(ctx context.Context, sessionID string) (bool, error) {
	key := fmt.Sprintf("smartflow:conversation_status:%s", sessionID)
	n, err := m.client.Exists(ctx, key).Result()
	if err != nil {
		return false, err
	}
	return n == 1, nil
}

func (m *AgentCache) SetConversationStatus(ctx context.Context, sessionID string) error {
	key := fmt.Sprintf("smartflow:conversation_status:%s", sessionID)
	// 仅用于“存在性”标记：只有不存在时才写入，避免重复写
	return m.client.SetNX(ctx, key, 1, m.expiration).Err()
}

func (m *AgentCache) DeleteConversationStatus(ctx context.Context, sessionID string) error {
	key := fmt.Sprintf("smartflow:conversation_status:%s", sessionID)
	return m.client.Del(ctx, key).Err()
}
