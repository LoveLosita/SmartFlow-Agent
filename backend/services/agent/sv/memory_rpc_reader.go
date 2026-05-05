package sv

import (
	"context"
	"errors"

	memorymodel "github.com/LoveLosita/smartflow/backend/services/memory/model"
	memoryobserve "github.com/LoveLosita/smartflow/backend/services/memory/observe"
	memorycontracts "github.com/LoveLosita/smartflow/backend/shared/contracts/memory"
)

// MemoryRPCReaderClient 描述 agent 读取 memory zrpc 所需的最小能力。
//
// 职责边界：
// 1. 只读取候选记忆，不暴露管理写接口；
// 2. 不要求调用方知道 backend/client/memory 的具体实现；
// 3. 错误原样返回给预取链路，由 agent 侧负责软降级和观测记录。
type MemoryRPCReaderClient interface {
	Retrieve(ctx context.Context, req memorycontracts.RetrieveRequest) ([]memorycontracts.ItemDTO, error)
}

type memoryRPCReader struct {
	client   MemoryRPCReaderClient
	observer memoryobserve.Observer
	metrics  memoryobserve.MetricsRecorder
}

// NewMemoryRPCReader 创建跨进程 memory reader 适配器。
//
// 职责边界：
// 1. 只把 agent 内部的 memorymodel.RetrieveRequest 转成共享契约；
// 2. 不持有 memory.Module，避免 CP3 后 agent 主链路继续直连本进程记忆服务；
// 3. observer / metrics 只用于 agent 注入观测，不参与 retrieve 业务调用；
// 4. client 为空时返回 nil，让 SetMemoryReader 保持既有“无 reader 则不注入”的降级语义。
func NewMemoryRPCReader(
	client MemoryRPCReaderClient,
	observer memoryobserve.Observer,
	metrics memoryobserve.MetricsRecorder,
) MemoryReader {
	if client == nil {
		return nil
	}
	if observer == nil {
		observer = memoryobserve.NewNopObserver()
	}
	if metrics == nil {
		metrics = memoryobserve.NewNopMetrics()
	}
	return &memoryRPCReader{
		client:   client,
		observer: observer,
		metrics:  metrics,
	}
}

// Retrieve 通过 memory zrpc 读取候选记忆并转换回 agent 内部 DTO。
func (r *memoryRPCReader) Retrieve(ctx context.Context, req memorymodel.RetrieveRequest) ([]memorymodel.ItemDTO, error) {
	if r == nil || r.client == nil {
		return nil, errors.New("memory rpc reader client is nil")
	}
	items, err := r.client.Retrieve(ctx, memorycontracts.RetrieveRequest{
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
	return toMemoryModelItems(items), nil
}

// MemoryObserver 暴露 agent 注入链路使用的 observer，保持 CP3 切流前后的注入观测连续。
func (r *memoryRPCReader) MemoryObserver() memoryobserve.Observer {
	if r == nil || r.observer == nil {
		return memoryobserve.NewNopObserver()
	}
	return r.observer
}

// MemoryMetrics 暴露 agent 注入链路使用的 metrics，避免 RPC reader 切流后指标静默丢失。
func (r *memoryRPCReader) MemoryMetrics() memoryobserve.MetricsRecorder {
	if r == nil || r.metrics == nil {
		return memoryobserve.NewNopMetrics()
	}
	return r.metrics
}

// toMemoryModelItems 只做跨层 DTO 字段搬运，不改变排序、过滤和记忆内容。
func toMemoryModelItems(items []memorycontracts.ItemDTO) []memorymodel.ItemDTO {
	if len(items) == 0 {
		return nil
	}
	result := make([]memorymodel.ItemDTO, 0, len(items))
	for _, item := range items {
		result = append(result, memorymodel.ItemDTO{
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
		})
	}
	return result
}
