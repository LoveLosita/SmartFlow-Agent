package ports

import (
	"context"
	"encoding/json"

	memorycontracts "github.com/LoveLosita/smartflow/backend/shared/contracts/memory"
)

// MemoryCommandClient 是 gateway 调用 memory 管理服务的最小能力集合。
//
// 职责边界：
// 1. 只覆盖当前 `/api/v1/memory/items` HTTP 门面需要的管理能力；
// 2. 不暴露 memory repo、worker、orchestrator、向量同步或 outbox consumer；
// 3. 复杂响应以 JSON 透传，避免 gateway 复制 memory 内部 DTO。
type MemoryCommandClient interface {
	ListItems(ctx context.Context, req memorycontracts.ListItemsRequest) (json.RawMessage, error)
	GetItem(ctx context.Context, req memorycontracts.GetItemRequest) (json.RawMessage, error)
	CreateItem(ctx context.Context, req memorycontracts.CreateItemRequest) (json.RawMessage, error)
	UpdateItem(ctx context.Context, req memorycontracts.UpdateItemRequest) (json.RawMessage, error)
	DeleteItem(ctx context.Context, req memorycontracts.DeleteItemRequest) (json.RawMessage, error)
	RestoreItem(ctx context.Context, req memorycontracts.RestoreItemRequest) (json.RawMessage, error)
}

// MemoryReaderClient 是 agent 主链路读取 memory zrpc 的最小端口。
//
// 职责边界：
// 1. 只覆盖 prompt 注入前的候选记忆召回；
// 2. 不暴露管理写接口，避免 agent 侧误拿管理能力做读取以外的事；
// 3. 调用失败由 agent 预取链路软降级，不在端口层吞错。
type MemoryReaderClient interface {
	Retrieve(ctx context.Context, req memorycontracts.RetrieveRequest) ([]memorycontracts.ItemDTO, error)
}
