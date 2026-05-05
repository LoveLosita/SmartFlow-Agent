package api

import (
	"context"
	"net/http"
	"time"

	"github.com/LoveLosita/smartflow/backend/gateway/shared/respond"
	contracts "github.com/LoveLosita/smartflow/backend/shared/contracts/notification"
	"github.com/LoveLosita/smartflow/backend/shared/ports"
	"github.com/gin-gonic/gin"
)

const notificationAPITimeout = 8 * time.Second

// NotificationAPI 承载当前用户的外部通知通道配置接口。
//
// 职责边界：
// 1. 只负责从 JWT 上下文取得当前 user_id、绑定请求体并调用 notification zrpc client；
// 2. 不直接读写 user_notification_channels，避免 API 层绕过 webhook 校验和脱敏规则；
// 3. 不参与主动调度、notification_records 状态机和 outbox 消费。
type NotificationAPI struct {
	client ports.NotificationCommandClient
}

func NewNotificationAPI(client ports.NotificationCommandClient) *NotificationAPI {
	return &NotificationAPI{client: client}
}

type saveFeishuWebhookRequest struct {
	Enabled     *bool  `json:"enabled"`
	WebhookURL  string `json:"webhook_url" binding:"required"`
	AuthType    string `json:"auth_type"`
	BearerToken string `json:"bearer_token"`
}

// GetFeishuWebhook 查询当前用户的飞书 Webhook 触发器配置。
func (api *NotificationAPI) GetFeishuWebhook(c *gin.Context) {
	if api == nil || api.client == nil {
		c.JSON(http.StatusInternalServerError, respond.InternalError(nilServiceError("通知通道 service 未初始化")))
		return
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), notificationAPITimeout)
	defer cancel()

	channel, err := api.client.GetFeishuWebhook(ctx, contracts.GetFeishuWebhookRequest{
		UserID: c.GetInt("user_id"),
	})
	if err != nil {
		respond.DealWithError(c, err)
		return
	}
	c.JSON(http.StatusOK, respond.RespWithData(respond.Ok, channel))
}

// SaveFeishuWebhook 幂等保存当前用户的飞书 Webhook 触发器配置。
func (api *NotificationAPI) SaveFeishuWebhook(c *gin.Context) {
	if api == nil || api.client == nil {
		c.JSON(http.StatusInternalServerError, respond.InternalError(nilServiceError("通知通道 service 未初始化")))
		return
	}

	var req saveFeishuWebhookRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, respond.WrongParamType)
		return
	}
	enabled := true
	if req.Enabled != nil {
		enabled = *req.Enabled
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), notificationAPITimeout)
	defer cancel()

	channel, err := api.client.SaveFeishuWebhook(ctx, contracts.SaveFeishuWebhookRequest{
		UserID:      c.GetInt("user_id"),
		Enabled:     enabled,
		WebhookURL:  req.WebhookURL,
		AuthType:    req.AuthType,
		BearerToken: req.BearerToken,
	})
	if err != nil {
		respond.DealWithError(c, err)
		return
	}
	c.JSON(http.StatusOK, respond.RespWithData(respond.Ok, channel))
}

// DeleteFeishuWebhook 删除当前用户的飞书 Webhook 触发器配置。
func (api *NotificationAPI) DeleteFeishuWebhook(c *gin.Context) {
	if api == nil || api.client == nil {
		c.JSON(http.StatusInternalServerError, respond.InternalError(nilServiceError("通知通道 service 未初始化")))
		return
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), notificationAPITimeout)
	defer cancel()

	if err := api.client.DeleteFeishuWebhook(ctx, contracts.DeleteFeishuWebhookRequest{
		UserID: c.GetInt("user_id"),
	}); err != nil {
		respond.DealWithError(c, err)
		return
	}
	c.JSON(http.StatusOK, respond.RespWithData(respond.Ok, gin.H{"deleted": true}))
}

// TestFeishuWebhook 发送一条最小业务 JSON 到当前用户配置的飞书 Webhook。
func (api *NotificationAPI) TestFeishuWebhook(c *gin.Context) {
	if api == nil || api.client == nil {
		c.JSON(http.StatusInternalServerError, respond.InternalError(nilServiceError("通知通道 service 未初始化")))
		return
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), notificationAPITimeout)
	defer cancel()

	result, err := api.client.TestFeishuWebhook(ctx, contracts.TestFeishuWebhookRequest{
		UserID: c.GetInt("user_id"),
	})
	if err != nil {
		respond.DealWithError(c, err)
		return
	}
	c.JSON(http.StatusOK, respond.RespWithData(respond.Ok, result))
}
