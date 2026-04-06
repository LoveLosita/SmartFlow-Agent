package dao

import (
	"context"
	"errors"

	newagentmodel "github.com/LoveLosita/smartflow/backend/newAgent/model"
)

// AgentStateStoreAdapter 将 CacheDAO 适配为 newAgent 的 AgentStateStore 接口。
//
// 职责边界：
// 1. CacheDAO 的 LoadAgentState 使用 out-parameter 模式，需要适配到返回值模式；
// 2. CacheDAO 的 SaveAgentState 接受 any，需要适配到 *AgentStateSnapshot；
// 3. DeleteAgentState 签名已匹配，直接转发。
type AgentStateStoreAdapter struct {
	cache *CacheDAO
}

// NewAgentStateStoreAdapter 创建适配器。
func NewAgentStateStoreAdapter(cache *CacheDAO) *AgentStateStoreAdapter {
	return &AgentStateStoreAdapter{cache: cache}
}

// Save 序列化并保存 agent 状态快照。
func (a *AgentStateStoreAdapter) Save(ctx context.Context, conversationID string, snapshot *newagentmodel.AgentStateSnapshot) error {
	if a == nil || a.cache == nil {
		return errors.New("agent state store adapter is not initialized")
	}
	return a.cache.SaveAgentState(ctx, conversationID, snapshot)
}

// Load 读取并反序列化 agent 状态快照。
func (a *AgentStateStoreAdapter) Load(ctx context.Context, conversationID string) (*newagentmodel.AgentStateSnapshot, bool, error) {
	if a == nil || a.cache == nil {
		return nil, false, errors.New("agent state store adapter is not initialized")
	}

	var snapshot newagentmodel.AgentStateSnapshot
	ok, err := a.cache.LoadAgentState(ctx, conversationID, &snapshot)
	if err != nil || !ok {
		return nil, ok, err
	}
	return &snapshot, true, nil
}

// Delete 删除 agent 状态快照。
func (a *AgentStateStoreAdapter) Delete(ctx context.Context, conversationID string) error {
	if a == nil || a.cache == nil {
		return errors.New("agent state store adapter is not initialized")
	}
	return a.cache.DeleteAgentState(ctx, conversationID)
}
