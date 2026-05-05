package sv

import (
	"context"
	"errors"
	"log"

	kafkabus "github.com/LoveLosita/smartflow/backend/infra/kafka"
	outboxinfra "github.com/LoveLosita/smartflow/backend/infra/outbox"
	coremodel "github.com/LoveLosita/smartflow/backend/model"
	eventsvc "github.com/LoveLosita/smartflow/backend/service/events"
	memorymodule "github.com/LoveLosita/smartflow/backend/services/memory"
	memorymodel "github.com/LoveLosita/smartflow/backend/services/memory/model"
	memorycontracts "github.com/LoveLosita/smartflow/backend/shared/contracts/memory"
)

// Service 是 memory 独立进程的服务门面。
//
// 职责边界：
// 1. 负责持有现有 memory.Module，复用 repo / service / worker / orchestrator 核心逻辑；
// 2. 负责把 memory.extract.requested 注册到 memory 服务自己的 outbox consumer；
// 3. 负责承接 CP2 后 gateway memory 管理流量，但不负责 HTTP 参数绑定、鉴权或幂等。
type Service struct {
	module   *memorymodule.Module
	eventBus *outboxinfra.EventBus
}

// Options 描述 memory 服务启动所需依赖。
type Options struct {
	Module      *memorymodule.Module
	OutboxRepo  *outboxinfra.Repository
	KafkaConfig kafkabus.Config
}

// NewService 组装 memory 独立服务。
//
// 步骤化说明：
// 1. 先校验 Module，保证 memory repo / worker / orchestrator 已由启动层完成装配；
// 2. 再登记 memory.extract.requested -> memory 的服务归属，避免 outbox 路由回落到 agent；
// 3. 最后在 Kafka 开启时创建 memory 服务自己的 EventBus 并注册消费 handler。
func NewService(opts Options) (*Service, error) {
	if opts.Module == nil {
		return nil, errors.New("memory module dependency not initialized")
	}

	if err := outboxinfra.RegisterEventService(eventsvc.EventTypeMemoryExtractRequested, outboxinfra.ServiceMemory); err != nil {
		return nil, err
	}

	var eventBus *outboxinfra.EventBus
	if opts.OutboxRepo != nil {
		bus, err := outboxinfra.NewEventBus(opts.OutboxRepo, opts.KafkaConfig)
		if err != nil {
			return nil, err
		}
		eventBus = bus
		if eventBus != nil {
			if err := eventsvc.RegisterMemoryExtractRequestedHandler(eventBus, opts.OutboxRepo, opts.Module); err != nil {
				return nil, err
			}
		}
	}

	return &Service{
		module:   opts.Module,
		eventBus: eventBus,
	}, nil
}

// Ping 用于 zrpc 启动期健康检查。
//
// 返回语义：
// 1. nil 表示 memory Module 已完成装配；
// 2. error 表示服务依赖缺失，调用方应认为 memory 服务不可用。
func (s *Service) Ping(context.Context) error {
	if s == nil || s.module == nil {
		return errors.New("memory service dependency not initialized")
	}
	return nil
}

// Retrieve 读取 agent 主链路后续可注入 prompt 的候选记忆。
//
// 职责边界：
// 1. 只把跨进程契约转成既有 memory.Module 的读取请求，避免重写召回、门控和降级逻辑；
// 2. 不负责 prompt 拼装、Redis 预取缓存和主链路失败降级，这些仍留在 agent 服务侧；
// 3. 返回字段保持与 ItemView 一致，保证 CP3 只改变进程边界，不改变注入内容语义。
func (s *Service) Retrieve(ctx context.Context, req memorycontracts.RetrieveRequest) ([]memorycontracts.ItemDTO, error) {
	if err := s.ensureModule(); err != nil {
		return nil, err
	}
	items, err := s.module.Retrieve(ctx, memorymodel.RetrieveRequest{
		Query:          req.Query,
		UserID:         req.UserID,
		ConversationID: req.ConversationID,
		AssistantID:    req.AssistantID,
		RunID:          req.RunID,
		MemoryTypes:    append([]string(nil), req.MemoryTypes...),
		Limit:          req.Limit,
		Now:            req.Now,
	})
	if err != nil {
		return nil, err
	}
	return toItemDTOs(items), nil
}

// ListItems 查询当前用户的记忆管理列表。
//
// 职责边界：
// 1. 只把跨进程契约转成现有 memory.Module 请求，复用旧管理逻辑；
// 2. 不在服务门面重做 limit/status/type 等业务规则，避免 CP2 改坏既有语义；
// 3. 返回稳定 ItemView，保持 gateway 切流前后的 JSON 字段一致。
func (s *Service) ListItems(ctx context.Context, req memorycontracts.ListItemsRequest) ([]memorycontracts.ItemView, error) {
	if err := s.ensureModule(); err != nil {
		return nil, err
	}
	items, err := s.module.ListItems(ctx, memorymodel.ListItemsRequest{
		UserID:         req.UserID,
		ConversationID: req.ConversationID,
		Statuses:       append([]string(nil), req.Statuses...),
		MemoryTypes:    append([]string(nil), req.MemoryTypes...),
		Limit:          req.Limit,
	})
	if err != nil {
		return nil, err
	}
	return toItemViews(items), nil
}

// GetItem 返回当前用户自己的单条记忆详情。
func (s *Service) GetItem(ctx context.Context, req memorycontracts.GetItemRequest) (*memorycontracts.ItemView, error) {
	if err := s.ensureModule(); err != nil {
		return nil, err
	}
	item, err := s.module.GetItem(ctx, coremodel.MemoryGetItemRequest{
		UserID:   req.UserID,
		MemoryID: req.MemoryID,
	})
	return toItemViewPtr(item), err
}

// CreateItem 手动新增一条用户记忆，并沿用既有审计与向量同步逻辑。
func (s *Service) CreateItem(ctx context.Context, req memorycontracts.CreateItemRequest) (*memorycontracts.ItemView, error) {
	if err := s.ensureModule(); err != nil {
		return nil, err
	}
	item, err := s.module.CreateItem(ctx, coremodel.MemoryCreateItemRequest{
		UserID:           req.UserID,
		ConversationID:   req.ConversationID,
		AssistantID:      req.AssistantID,
		RunID:            req.RunID,
		MemoryType:       req.MemoryType,
		Title:            req.Title,
		Content:          req.Content,
		Confidence:       req.Confidence,
		Importance:       req.Importance,
		SensitivityLevel: req.SensitivityLevel,
		IsExplicit:       req.IsExplicit,
		TTLAt:            req.TTLAt,
		Reason:           req.Reason,
		OperatorType:     req.OperatorType,
	})
	return toItemViewPtr(item), err
}

// UpdateItem 手动修改一条用户记忆，并沿用既有审计与向量同步逻辑。
func (s *Service) UpdateItem(ctx context.Context, req memorycontracts.UpdateItemRequest) (*memorycontracts.ItemView, error) {
	if err := s.ensureModule(); err != nil {
		return nil, err
	}
	item, err := s.module.UpdateItem(ctx, coremodel.MemoryUpdateItemRequest{
		UserID:           req.UserID,
		MemoryID:         req.MemoryID,
		MemoryType:       req.MemoryType,
		Title:            req.Title,
		Content:          req.Content,
		Confidence:       req.Confidence,
		Importance:       req.Importance,
		SensitivityLevel: req.SensitivityLevel,
		IsExplicit:       req.IsExplicit,
		TTLAt:            req.TTLAt,
		ClearTTL:         req.ClearTTL,
		Reason:           req.Reason,
		OperatorType:     req.OperatorType,
	})
	return toItemViewPtr(item), err
}

// DeleteItem 软删除一条记忆，返回删除后的条目视图。
func (s *Service) DeleteItem(ctx context.Context, req memorycontracts.DeleteItemRequest) (*memorycontracts.ItemView, error) {
	if err := s.ensureModule(); err != nil {
		return nil, err
	}
	item, err := s.module.DeleteItem(ctx, coremodel.MemoryDeleteItemRequest{
		UserID:       req.UserID,
		MemoryID:     req.MemoryID,
		Reason:       req.Reason,
		OperatorType: req.OperatorType,
	})
	return toItemViewPtr(item), err
}

// RestoreItem 恢复一条 deleted/archived 记忆，返回恢复后的条目视图。
func (s *Service) RestoreItem(ctx context.Context, req memorycontracts.RestoreItemRequest) (*memorycontracts.ItemView, error) {
	if err := s.ensureModule(); err != nil {
		return nil, err
	}
	item, err := s.module.RestoreItem(ctx, coremodel.MemoryRestoreItemRequest{
		UserID:       req.UserID,
		MemoryID:     req.MemoryID,
		Reason:       req.Reason,
		OperatorType: req.OperatorType,
	})
	return toItemViewPtr(item), err
}

// StartWorkers 启动 memory 服务拥有的后台生命周期。
//
// 步骤化说明：
// 1. 先启动 memory outbox relay / consumer，让 memory.extract.requested 可以被转成 memory_jobs；
// 2. 再启动 memory worker 轮询 memory_jobs，执行抽取、审计与向量同步；
// 3. Kafka 关闭时 eventBus 为空，只启动本地 worker，保留无 Kafka 环境下的降级能力。
func (s *Service) StartWorkers(ctx context.Context) {
	if s == nil {
		return
	}
	if s.eventBus != nil {
		s.eventBus.Start(ctx)
		log.Println("Memory outbox consumer started")
	} else {
		log.Println("Memory outbox consumer is disabled")
	}
	if s.module != nil {
		s.module.StartWorker(ctx)
	}
}

// Close 关闭 memory 服务持有的外部资源。
func (s *Service) Close() {
	if s == nil || s.eventBus == nil {
		return
	}
	s.eventBus.Close()
}

func (s *Service) ensureModule() error {
	if s == nil || s.module == nil {
		return errors.New("memory service dependency not initialized")
	}
	return nil
}

func toItemViews(items []memorymodel.ItemDTO) []memorycontracts.ItemView {
	if len(items) == 0 {
		return nil
	}
	result := make([]memorycontracts.ItemView, 0, len(items))
	for _, item := range items {
		result = append(result, toItemView(item))
	}
	return result
}

func toItemDTOs(items []memorymodel.ItemDTO) []memorycontracts.ItemDTO {
	return toItemViews(items)
}

func toItemViewPtr(item *memorymodel.ItemDTO) *memorycontracts.ItemView {
	if item == nil {
		return nil
	}
	view := toItemView(*item)
	return &view
}

func toItemView(item memorymodel.ItemDTO) memorycontracts.ItemView {
	return memorycontracts.ItemView{
		ID:               item.ID,
		UserID:           item.UserID,
		ConversationID:   item.ConversationID,
		AssistantID:      item.AssistantID,
		RunID:            item.RunID,
		MemoryType:       item.MemoryType,
		Title:            item.Title,
		Content:          item.Content,
		ContentHash:      item.ContentHash,
		Confidence:       item.Confidence,
		Importance:       item.Importance,
		SensitivityLevel: item.SensitivityLevel,
		IsExplicit:       item.IsExplicit,
		Status:           item.Status,
		TTLAt:            item.TTLAt,
		CreatedAt:        item.CreatedAt,
		UpdatedAt:        item.UpdatedAt,
	}
}
