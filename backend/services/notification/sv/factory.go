package sv

import notificationfeishu "github.com/LoveLosita/smartflow/backend/services/notification/internal/feishu"

// FeishuWebhookProviderOptions 定义生产默认飞书 Webhook provider 的启动参数。
//
// 职责边界：
// 1. 只承载 provider 初始化需要的外部配置，不暴露 internal/feishu 的具体实现；
// 2. 不负责 notification 状态机参数，重试次数和扫描批量仍由 ServiceOptions 管理；
// 3. 后续若新增 OpenID 等 provider，应新增对应构造器，避免把多 provider 分支堆进 cmd 入口。
type FeishuWebhookProviderOptions struct {
	FrontendBaseURL string
}

// NewNotificationServiceWithFeishuWebhook 创建生产默认的飞书 Webhook notification 服务。
//
// 职责边界：
// 1. 在 sv 层完成 internal/feishu provider 装配，cmd 入口不直接依赖 internal 包；
// 2. 只组合 notification 领域内部依赖，不连接数据库、不读取配置；
// 3. provider 构造失败时直接返回 error，避免启动出半初始化服务。
func NewNotificationServiceWithFeishuWebhook(recordStore RecordStore, channelStore ChannelStore, providerOpts FeishuWebhookProviderOptions, serviceOpts ServiceOptions) (*Service, error) {
	provider, err := notificationfeishu.NewWebhookProvider(channelStore, notificationfeishu.WebhookProviderOptions{
		FrontendBaseURL: providerOpts.FrontendBaseURL,
	})
	if err != nil {
		return nil, err
	}
	return NewNotificationService(recordStore, channelStore, provider, serviceOpts)
}
