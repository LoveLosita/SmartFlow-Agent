package dao

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/cloudwego/eino/schema"
	"github.com/go-redis/redis/v8"
)

type AgentCache struct {
	client *redis.Client
	// 默认滑动窗口大小，比如 20 条消息
	windowSize int
	// 缓存过期时间
	expiration time.Duration
}

func NewAgentCache(client *redis.Client) *AgentCache {
	return &AgentCache{
		client:     client,
		windowSize: 20,            // 后续更新：根据 Token 消耗灵活调整
		expiration: 1 * time.Hour, // 保持一小时的热记忆
	}
}

func (m *AgentCache) PushMessage(ctx context.Context, sessionID string, msg *schema.Message) error {
	key := fmt.Sprintf("smartflow:history:%s", sessionID)

	// 1. 序列化 Eino 消息
	data, err := json.Marshal(msg)
	if err != nil {
		return fmt.Errorf("marshal message failed: %w", err)
	}

	// 2. 利用 Pipeline 保证原子操作
	pipe := m.client.Pipeline()

	// 往左侧推入最新消息 (LIFO 逻辑)
	pipe.LPush(ctx, key, data)

	// 核心：强制修剪，只保留最新的 windowSize 条
	// 0 是最新的一条，windowSize-1 是最后一条
	pipe.LTrim(ctx, key, 0, int64(m.windowSize-1))

	// 刷新过期时间
	pipe.Expire(ctx, key, m.expiration)

	_, err = pipe.Exec(ctx)
	return err
}

func (m *AgentCache) GetHistory(ctx context.Context, sessionID string) ([]*schema.Message, error) {
	key := fmt.Sprintf("smartflow:history:%s", sessionID)

	// 获取所有缓存的消息
	vals, err := m.client.LRange(ctx, key, 0, -1).Result()
	if err != nil {
		return nil, err
	}

	// 如果 Redis 为空，这里返回 nil 触发后续的 MySQL 捞取逻辑
	if len(vals) == 0 {
		return nil, nil
	}

	messages := make([]*schema.Message, len(vals))
	for i, val := range vals {
		var msg schema.Message
		if err := json.Unmarshal([]byte(val), &msg); err != nil {
			return nil, err
		}

		// 关键逻辑：反转顺序
		// LRANGE 返回顺序：[MsgN, MsgN-1, ... Msg1]
		// 我们需要的顺序：[Msg1, ... MsgN-1, MsgN]
		messages[len(vals)-1-i] = &msg
	}

	return messages, nil
}

// BackfillHistory 用于缓存失效时，从数据库加载完数据后一次性回填 Redis
func (m *AgentCache) BackfillHistory(ctx context.Context, sessionID string, messages []*schema.Message) error {
	if len(messages) == 0 {
		return nil
	}

	key := fmt.Sprintf("smartflow:history:%s", sessionID)

	// 1. 将所有 Eino 消息序列化为 []interface{} 供 redis 批量写入
	values := make([]interface{}, len(messages))
	for i, msg := range messages {
		data, err := json.Marshal(msg)
		if err != nil {
			return fmt.Errorf("marshal failed at index %d: %w", i, err)
		}
		values[i] = data
	}

	// 2. 执行原子回填
	pipe := m.client.Pipeline()

	// 先清理旧 Key（防止数据重复或残留）
	pipe.Del(ctx, key)

	// 批量写入：按照 [最旧 -> 最新] 的顺序 LPUSH
	// 结果在 Redis 中：[最新, ..., 最旧] (符合我们 GetHistory 的反转逻辑)
	pipe.LPush(ctx, key, values...)

	// 依然要进行修剪，确保不超过窗口大小
	pipe.LTrim(ctx, key, 0, int64(m.windowSize-1))

	// 设置过期时间
	pipe.Expire(ctx, key, m.expiration)

	_, err := pipe.Exec(ctx)
	return err
}

func (m *AgentCache) ClearHistory(ctx context.Context, sessionID string) error {
	key := fmt.Sprintf("smartflow:history:%s", sessionID)
	return m.client.Del(ctx, key).Err()
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
