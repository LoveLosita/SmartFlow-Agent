package sv

import (
	"context"
	"errors"
	"strings"

	"github.com/LoveLosita/smartflow/backend/respond"
	notificationfeishu "github.com/LoveLosita/smartflow/backend/services/notification/internal/feishu"
	notificationmodel "github.com/LoveLosita/smartflow/backend/services/notification/model"
	contracts "github.com/LoveLosita/smartflow/backend/shared/contracts/notification"
	"gorm.io/gorm"
)

const (
	channelTestStatusSuccess = "success"
	channelTestStatusFailed  = "failed"
)

// GetFeishuWebhook 查询当前用户的飞书 Webhook 触发器配置。
func (s *Service) GetFeishuWebhook(ctx context.Context, userID int) (contracts.ChannelResponse, error) {
	if userID <= 0 {
		return contracts.ChannelResponse{}, respond.ErrUnauthorized
	}
	row, err := s.channelStore.GetUserNotificationChannel(ctx, userID, notificationmodel.ChannelFeishuWebhook)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return contracts.ChannelResponse{
				Channel:    notificationmodel.ChannelFeishuWebhook,
				AuthType:   notificationmodel.AuthTypeNone,
				Configured: false,
			}, nil
		}
		return contracts.ChannelResponse{}, err
	}
	return responseFromChannel(row), nil
}

// SaveFeishuWebhook 幂等保存当前用户的飞书 Webhook 触发器配置。
func (s *Service) SaveFeishuWebhook(ctx context.Context, userID int, req contracts.SaveFeishuWebhookRequest) (contracts.ChannelResponse, error) {
	if userID <= 0 {
		return contracts.ChannelResponse{}, respond.ErrUnauthorized
	}
	webhookURL := strings.TrimSpace(req.WebhookURL)
	if webhookURL == "" {
		return contracts.ChannelResponse{}, respond.MissingParam
	}
	if err := notificationfeishu.ValidateWebhookURL(webhookURL); err != nil {
		return contracts.ChannelResponse{}, respond.WrongParamType
	}
	authType := normalizeAuthType(req.AuthType)
	bearerToken := strings.TrimSpace(req.BearerToken)
	if authType == notificationmodel.AuthTypeBearer && bearerToken == "" {
		return contracts.ChannelResponse{}, respond.MissingParam
	}
	row := &notificationmodel.UserNotificationChannel{
		UserID:      userID,
		Channel:     notificationmodel.ChannelFeishuWebhook,
		Enabled:     req.Enabled,
		WebhookURL:  webhookURL,
		AuthType:    authType,
		BearerToken: bearerToken,
	}
	if err := s.channelStore.UpsertUserNotificationChannel(ctx, row); err != nil {
		return contracts.ChannelResponse{}, err
	}
	return s.GetFeishuWebhook(ctx, userID)
}

// DeleteFeishuWebhook 删除当前用户的飞书 Webhook 触发器配置。
func (s *Service) DeleteFeishuWebhook(ctx context.Context, userID int) error {
	if userID <= 0 {
		return respond.ErrUnauthorized
	}
	return s.channelStore.DeleteUserNotificationChannel(ctx, userID, notificationmodel.ChannelFeishuWebhook)
}

// TestFeishuWebhook 发送一条最小业务 JSON 到当前用户配置的飞书 Webhook。
func (s *Service) TestFeishuWebhook(ctx context.Context, userID int) (contracts.TestResult, error) {
	if userID <= 0 {
		return contracts.TestResult{}, respond.ErrUnauthorized
	}
	now := s.options.Now()
	traceID := "trace_feishu_webhook_test"
	sendResult, sendErr := s.provider.Send(ctx, notificationfeishu.SendRequest{
		NotificationID: 0,
		UserID:         userID,
		TriggerID:      "ast_test_webhook",
		PreviewID:      "asp_test_webhook",
		TriggerType:    "manual_test",
		TargetType:     "notification_channel",
		TargetID:       0,
		TargetURL:      "/assistant/00000000-0000-0000-0000-000000000000",
		MessageText:    "这是一条 SmartFlow 飞书 Webhook 测试消息。",
		TraceID:        traceID,
		AttemptCount:   1,
	})
	if sendErr != nil {
		return contracts.TestResult{}, sendErr
	}

	status := channelTestStatusFailed
	testErr := strings.TrimSpace(sendResult.ErrorMessage)
	if sendResult.Outcome == notificationfeishu.SendOutcomeSuccess {
		status = channelTestStatusSuccess
		testErr = ""
	}
	if sendResult.Outcome == notificationfeishu.SendOutcomeSkipped && testErr == "" {
		testErr = "飞书 webhook 未配置或未启用"
	}
	if err := s.channelStore.UpdateUserNotificationChannelTestResult(ctx, userID, notificationmodel.ChannelFeishuWebhook, status, testErr, now); err != nil {
		return contracts.TestResult{}, err
	}
	channel, err := s.GetFeishuWebhook(ctx, userID)
	if err != nil {
		return contracts.TestResult{}, err
	}
	return contracts.TestResult{
		Channel:  channel,
		Status:   status,
		Outcome:  string(sendResult.Outcome),
		Message:  testErr,
		TraceID:  traceID,
		SentAt:   now,
		Skipped:  sendResult.Outcome == notificationfeishu.SendOutcomeSkipped,
		Provider: notificationfeishu.Channel,
	}, nil
}

func responseFromChannel(row *notificationmodel.UserNotificationChannel) contracts.ChannelResponse {
	if row == nil {
		return contracts.ChannelResponse{
			Channel:    notificationmodel.ChannelFeishuWebhook,
			AuthType:   notificationmodel.AuthTypeNone,
			Configured: false,
		}
	}
	return contracts.ChannelResponse{
		Channel:        row.Channel,
		Enabled:        row.Enabled,
		Configured:     strings.TrimSpace(row.WebhookURL) != "",
		WebhookURLMask: notificationfeishu.MaskWebhookURL(row.WebhookURL),
		AuthType:       normalizeAuthType(row.AuthType),
		HasBearerToken: strings.TrimSpace(row.BearerToken) != "",
		LastTestStatus: row.LastTestStatus,
		LastTestError:  row.LastTestError,
		LastTestAt:     row.LastTestAt,
	}
}

func normalizeAuthType(authType string) string {
	switch strings.ToLower(strings.TrimSpace(authType)) {
	case notificationmodel.AuthTypeBearer:
		return notificationmodel.AuthTypeBearer
	default:
		return notificationmodel.AuthTypeNone
	}
}
