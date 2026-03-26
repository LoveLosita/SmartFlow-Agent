package dao

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
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

	// 1. 序列化 Eino 消息。
	data, err := json.Marshal(msg)
	if err != nil {
		return fmt.Errorf("marshal message failed: %w", err)
	}

	// 2. 使用 Pipeline 保证“写入+裁剪+续期”原子执行。
	pipe := m.client.Pipeline()
	pipe.LPush(ctx, key, data)
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
		// LRANGE 返回 [最新...最旧]，这里反转成 [最旧...最新]
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
	pipe.LPush(ctx, key, values...)
	pipe.LTrim(ctx, key, 0, int64(size-1))
	pipe.Expire(ctx, key, m.expiration)
	_, err = pipe.Exec(ctx)
	return err
}

func (m *AgentCache) ApplyRetrySeed(ctx context.Context, sessionID, retryGroupID string, sourceUserMessageID, sourceAssistantMessageID int) error {
	if m == nil || m.client == nil {
		return nil
	}
	groupID := strings.TrimSpace(retryGroupID)
	if groupID == "" {
		return nil
	}

	vals, err := m.client.LRange(ctx, m.historyKey(sessionID), 0, -1).Result()
	if err != nil {
		return err
	}
	if len(vals) == 0 {
		return nil
	}

	changed := false
	targets := map[int]struct{}{}
	if sourceUserMessageID > 0 {
		targets[sourceUserMessageID] = struct{}{}
	}
	if sourceAssistantMessageID > 0 {
		targets[sourceAssistantMessageID] = struct{}{}
	}
	if len(targets) == 0 {
		return nil
	}

	indexOne := 1
	for idx, raw := range vals {
		var msg schema.Message
		if err := json.Unmarshal([]byte(raw), &msg); err != nil {
			return err
		}
		historyID := extractMessageHistoryID(&msg)
		if historyID <= 0 {
			continue
		}
		if _, ok := targets[historyID]; !ok {
			continue
		}
		if msg.Extra == nil {
			msg.Extra = make(map[string]any)
		}
		msg.Extra["retry_group_id"] = groupID
		msg.Extra["retry_index"] = indexOne
		updated, err := json.Marshal(&msg)
		if err != nil {
			return err
		}
		vals[idx] = string(updated)
		changed = true
	}

	if !changed {
		return nil
	}

	pipe := m.client.Pipeline()
	key := m.historyKey(sessionID)
	pipe.Del(ctx, key)
	values := make([]interface{}, 0, len(vals))
	for _, item := range vals {
		values = append(values, item)
	}
	pipe.RPush(ctx, key, values...)
	pipe.LTrim(ctx, key, 0, int64(len(vals)-1))
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
	// 仅用于“存在性”标记：只有不存在时才写入，避免重复写。
	return m.client.SetNX(ctx, key, 1, m.expiration).Err()
}

func (m *AgentCache) DeleteConversationStatus(ctx context.Context, sessionID string) error {
	key := fmt.Sprintf("smartflow:conversation_status:%s", sessionID)
	return m.client.Del(ctx, key).Err()
}

func extractMessageHistoryID(msg *schema.Message) int {
	if msg == nil || msg.Extra == nil {
		return 0
	}
	raw, ok := msg.Extra["history_id"]
	if !ok {
		return 0
	}
	// 1. history_id 主要来自 DB 回填，正常情况下是 number。
	// 2. 但 Redis 往返、灰度期数据修复或手工写入时，仍可能出现字符串数字。
	// 3. 这里做一次宽松解析，避免重试分组补种时因为类型差异找不到源消息。
	switch v := raw.(type) {
	case int:
		return v
	case int32:
		return int(v)
	case int64:
		return int(v)
	case float64:
		return int(v)
	case json.Number:
		if parsed, err := v.Int64(); err == nil {
			return int(parsed)
		}
		if parsed, err := v.Float64(); err == nil {
			return int(parsed)
		}
		return 0
	case string:
		trimmed := strings.TrimSpace(v)
		if trimmed == "" {
			return 0
		}
		parsed, err := strconv.Atoi(trimmed)
		if err != nil {
			return 0
		}
		return parsed
	default:
		return 0
	}
}
