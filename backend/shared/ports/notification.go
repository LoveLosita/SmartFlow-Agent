package ports

import (
	"context"

	contracts "github.com/LoveLosita/smartflow/backend/shared/contracts/notification"
)

// NotificationCommandClient 是 gateway 调用 notification 服务的通道配置能力集合。
//
// 职责边界：
// 1. 只描述 HTTP 入口需要的配置查询、保存、删除和测试能力；
// 2. 不暴露 notification_records、provider、outbox consumer 或 retry loop 细节；
// 3. 具体通信协议由 gateway adapter 决定，API 层保持 res, err 的统一调用语义。
type NotificationCommandClient interface {
	GetFeishuWebhook(ctx context.Context, req contracts.GetFeishuWebhookRequest) (*contracts.ChannelResponse, error)
	SaveFeishuWebhook(ctx context.Context, req contracts.SaveFeishuWebhookRequest) (*contracts.ChannelResponse, error)
	DeleteFeishuWebhook(ctx context.Context, req contracts.DeleteFeishuWebhookRequest) error
	TestFeishuWebhook(ctx context.Context, req contracts.TestFeishuWebhookRequest) (*contracts.TestResult, error)
}
