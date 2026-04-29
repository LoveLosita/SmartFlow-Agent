package events

import (
	"errors"
	"fmt"

	"github.com/LoveLosita/smartflow/backend/dao"
	outboxinfra "github.com/LoveLosita/smartflow/backend/infra/outbox"
	"github.com/LoveLosita/smartflow/backend/memory"
)

// RegisterCoreOutboxHandlers 注册核心业务 outbox handler。
//
// 职责边界：
// 1. 只负责聚合注册当前核心业务 handler，便于 start / worker/all 等启动入口复用同一套接线顺序。
// 2. 不负责创建 eventBus/outboxRepo/DAO/memoryModule，也不负责启动或关闭事件总线。
// 3. 不改变单个 Register* 函数的职责；具体 payload 解析、幂等消费和业务落库仍由各自 handler 负责。
// 4. 入口先完整校验依赖，避免注册到一半才发现依赖缺失，导致事件总线处于半注册状态。
func RegisterCoreOutboxHandlers(
	eventBus *outboxinfra.EventBus,
	outboxRepo *outboxinfra.Repository,
	repoManager *dao.RepoManager,
	agentRepo *dao.AgentDAO,
	cacheRepo *dao.CacheDAO,
	memoryModule *memory.Module,
) error {
	if err := validateCoreOutboxHandlerDeps(eventBus, outboxRepo, repoManager, agentRepo, cacheRepo, memoryModule); err != nil {
		return err
	}

	// 1. 按照现有 start.go 的接线顺序注册，保证迁移到 worker/all 后消费行为不发生隐式变化。
	// 2. 每一步只包一层业务语义错误，便于启动日志直接定位是哪类 handler 注册失败。
	if err := RegisterChatHistoryPersistHandler(eventBus, outboxRepo, repoManager); err != nil {
		return fmt.Errorf("注册聊天历史持久化 handler 失败: %w", err)
	}
	if err := RegisterTaskUrgencyPromoteHandler(eventBus, outboxRepo, repoManager); err != nil {
		return fmt.Errorf("注册任务紧急度平移 handler 失败: %w", err)
	}
	if err := RegisterChatTokenUsageAdjustHandler(eventBus, outboxRepo, repoManager); err != nil {
		return fmt.Errorf("注册会话 token 调整 handler 失败: %w", err)
	}
	if err := RegisterAgentStateSnapshotHandler(eventBus, outboxRepo, repoManager); err != nil {
		return fmt.Errorf("注册 agent 状态快照 handler 失败: %w", err)
	}
	if err := RegisterAgentTimelinePersistHandler(eventBus, outboxRepo, agentRepo, cacheRepo); err != nil {
		return fmt.Errorf("注册 agent 时间线持久化 handler 失败: %w", err)
	}
	if err := RegisterMemoryExtractRequestedHandler(eventBus, outboxRepo, memoryModule); err != nil {
		return fmt.Errorf("注册记忆抽取 handler 失败: %w", err)
	}

	return nil
}

// validateCoreOutboxHandlerDeps 校验核心 outbox handler 聚合注册所需依赖。
//
// 职责边界：
// 1. 只做 nil 校验，不做数据库、Redis、Kafka 连通性探测，避免注册函数承担启动健康检查职责。
// 2. 返回 error 表示依赖缺失；返回 nil 表示可以安全进入逐项注册流程。
func validateCoreOutboxHandlerDeps(
	eventBus *outboxinfra.EventBus,
	outboxRepo *outboxinfra.Repository,
	repoManager *dao.RepoManager,
	agentRepo *dao.AgentDAO,
	cacheRepo *dao.CacheDAO,
	memoryModule *memory.Module,
) error {
	if eventBus == nil {
		return errors.New("event bus is nil")
	}
	if outboxRepo == nil {
		return errors.New("outbox repository is nil")
	}
	if repoManager == nil {
		return errors.New("repo manager is nil")
	}
	if agentRepo == nil {
		return errors.New("agent repo is nil")
	}
	if cacheRepo == nil {
		return errors.New("cache repo is nil")
	}
	if memoryModule == nil {
		return errors.New("memory module is nil")
	}
	return nil
}
