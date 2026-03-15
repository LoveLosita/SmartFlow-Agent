package outbox

import (
	"context"
	"encoding/json"
	"errors"

	kafkabus "github.com/LoveLosita/smartflow/backend/infra/kafka"
	"github.com/LoveLosita/smartflow/backend/model"
)

// ChatHistoryAsync 是“聊天记录异步持久化”的业务适配器。
//
// 设计目的：
// 1) 让业务层只调用 EnqueueChatHistoryPersist，而不感知扫描/投递/消费细节；
// 2) 保持现有 Agent 代码调用习惯，降低改造面；
// 3) 把具体的 outbox+kafka 主流程彻底收敛到 infra。
type ChatHistoryAsync struct {
	engine *Engine
}

// NewChatHistoryAsync 创建聊天记录异步适配器并注册处理器。
//
// 处理器职责：
// 1) 从 envelope payload 解析聊天载荷；
// 2) 调用仓储“落库并标记 consumed”；
// 3) 解析失败时标记 dead（不可恢复错误），避免无意义重试。
func NewChatHistoryAsync(repo *Repository, cfg kafkabus.Config) (*ChatHistoryAsync, error) {
	// 1. 先创建通用引擎，内部会按 cfg.Enabled 决定是否启用。
	engine, err := NewEngine(repo, cfg)
	if err != nil {
		return nil, err
	}
	if engine == nil {
		// 2. 异步开关关闭：返回 nil 交给上层走同步降级路径。
		return nil, nil
	}

	// 3. 注册“聊天记录持久化”业务处理器。
	//    该处理器只做三件事：
	//    3.1 解析 payload；
	//    3.2 调仓储落库并推进 consumed；
	//    3.3 遇到不可恢复错误时标记 dead。
	if err = engine.RegisterHandler(model.OutboxBizTypeChatHistoryPersist, func(ctx context.Context, envelope kafkabus.Envelope) error {
		var payload model.ChatHistoryPersistPayload
		if unmarshalErr := json.Unmarshal(envelope.Payload, &payload); unmarshalErr != nil {
			_ = repo.MarkDead(ctx, envelope.OutboxID, "解析聊天持久化载荷失败: "+unmarshalErr.Error())
			// 返回 nil：该错误已被标记为 dead，不需要再走重试。
			return nil
		}
		// 返回 error：由 engine 统一标记为 retry 并提交 offset。
		return repo.PersistChatHistoryAndMarkConsumed(ctx, envelope.OutboxID, payload)
	}); err != nil {
		// 4. 注册失败时回收已创建的引擎资源，防止泄漏。
		engine.Close()
		return nil, err
	}

	// 5. 返回业务适配器，对业务层暴露“更语义化”的调用入口。
	return &ChatHistoryAsync{engine: engine}, nil
}

// Start 启动异步引擎（扫描 + 消费）。
func (a *ChatHistoryAsync) Start(ctx context.Context) {
	// 允许在未初始化（例如异步关闭）时被安全调用。
	if a == nil || a.engine == nil {
		return
	}
	a.engine.Start(ctx)
}

// Close 关闭异步引擎资源。
func (a *ChatHistoryAsync) Close() {
	// 允许在未初始化（例如异步关闭）时被安全调用。
	if a == nil || a.engine == nil {
		return
	}
	a.engine.Close()
}

// EnqueueChatHistoryPersist 将聊天记录持久化请求写入 outbox。
// 该方法是业务层唯一需要调用的入口。
func (a *ChatHistoryAsync) EnqueueChatHistoryPersist(ctx context.Context, payload model.ChatHistoryPersistPayload) error {
	// 1. 若引擎未初始化，说明启动配置有问题或异步功能未启用。
	//    这里显式返回错误，交由业务层按需降级/告警。
	if a == nil || a.engine == nil {
		return errors.New("chat history async is not initialized")
	}
	// 2. 以 conversation_id 作为 messageKey，尽量让同会话消息落在稳定分区。
	return a.engine.Enqueue(ctx, model.OutboxBizTypeChatHistoryPersist, payload.ConversationID, payload)
}
