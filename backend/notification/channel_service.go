package notification

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/LoveLosita/smartflow/backend/model"
	"gorm.io/gorm"
)

const (
	ChannelTestStatusSuccess = "success"
	ChannelTestStatusFailed  = "failed"
)

var ErrInvalidChannelConfig = errors.New("notification channel config invalid")

type UserNotificationChannelStore interface {
	UserNotificationChannelReader
	UpsertUserNotificationChannel(ctx context.Context, channel *model.UserNotificationChannel) error
	DeleteUserNotificationChannel(ctx context.Context, userID int, channel string) error
	UpdateUserNotificationChannelTestResult(ctx context.Context, userID int, channel string, status string, testErr string, testedAt time.Time) error
}

type SaveFeishuWebhookRequest struct {
	Enabled     bool
	WebhookURL  string
	AuthType    string
	BearerToken string
}

type ChannelResponse struct {
	Channel        string     `json:"channel"`
	Enabled        bool       `json:"enabled"`
	Configured     bool       `json:"configured"`
	WebhookURLMask string     `json:"webhook_url_mask,omitempty"`
	AuthType       string     `json:"auth_type"`
	HasBearerToken bool       `json:"has_bearer_token"`
	LastTestStatus string     `json:"last_test_status,omitempty"`
	LastTestError  string     `json:"last_test_error,omitempty"`
	LastTestAt     *time.Time `json:"last_test_at,omitempty"`
}

type TestResult struct {
	Channel  ChannelResponse `json:"channel"`
	Status   string          `json:"status"`
	Outcome  string          `json:"outcome"`
	Message  string          `json:"message,omitempty"`
	TraceID  string          `json:"trace_id,omitempty"`
	SentAt   time.Time       `json:"sent_at"`
	Skipped  bool            `json:"skipped"`
	Provider string          `json:"provider"`
}

type ChannelServiceOptions struct {
	Now func() time.Time
}

// ChannelService 管理用户通知通道配置和测试发送。
//
// 职责边界：
// 1. 负责保存、查询、删除当前用户的飞书 webhook 配置；
// 2. 负责调用同一套 provider 发送测试事件并回写 last_test_*；
// 3. 不参与主动调度 trigger / preview / notification_records 状态机。
type ChannelService struct {
	store    UserNotificationChannelStore
	provider FeishuProvider
	now      func() time.Time
}

func NewChannelService(store UserNotificationChannelStore, provider FeishuProvider, opts ChannelServiceOptions) (*ChannelService, error) {
	if store == nil {
		return nil, errors.New("notification channel store is nil")
	}
	if provider == nil {
		return nil, errors.New("feishu provider is nil")
	}
	now := opts.Now
	if now == nil {
		now = time.Now
	}
	return &ChannelService{
		store:    store,
		provider: provider,
		now:      now,
	}, nil
}

func (s *ChannelService) GetFeishuWebhook(ctx context.Context, userID int) (ChannelResponse, error) {
	if userID <= 0 {
		return ChannelResponse{}, ErrInvalidChannelConfig
	}
	row, err := s.store.GetUserNotificationChannel(ctx, userID, model.NotificationChannelFeishuWebhook)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return ChannelResponse{
				Channel:    model.NotificationChannelFeishuWebhook,
				AuthType:   model.NotificationAuthTypeNone,
				Configured: false,
			}, nil
		}
		return ChannelResponse{}, err
	}
	return responseFromChannel(row), nil
}

func (s *ChannelService) SaveFeishuWebhook(ctx context.Context, userID int, req SaveFeishuWebhookRequest) (ChannelResponse, error) {
	if userID <= 0 {
		return ChannelResponse{}, ErrInvalidChannelConfig
	}
	webhookURL := strings.TrimSpace(req.WebhookURL)
	if err := ValidateFeishuWebhookURL(webhookURL); err != nil {
		return ChannelResponse{}, ErrInvalidChannelConfig
	}
	authType := normalizeAuthType(req.AuthType)
	bearerToken := strings.TrimSpace(req.BearerToken)
	if authType == model.NotificationAuthTypeBearer && bearerToken == "" {
		return ChannelResponse{}, ErrInvalidChannelConfig
	}
	row := &model.UserNotificationChannel{
		UserID:      userID,
		Channel:     model.NotificationChannelFeishuWebhook,
		Enabled:     req.Enabled,
		WebhookURL:  webhookURL,
		AuthType:    authType,
		BearerToken: bearerToken,
	}
	if err := s.store.UpsertUserNotificationChannel(ctx, row); err != nil {
		return ChannelResponse{}, err
	}
	return s.GetFeishuWebhook(ctx, userID)
}

func (s *ChannelService) DeleteFeishuWebhook(ctx context.Context, userID int) error {
	if userID <= 0 {
		return ErrInvalidChannelConfig
	}
	return s.store.DeleteUserNotificationChannel(ctx, userID, model.NotificationChannelFeishuWebhook)
}

func (s *ChannelService) TestFeishuWebhook(ctx context.Context, userID int) (TestResult, error) {
	if userID <= 0 {
		return TestResult{}, ErrInvalidChannelConfig
	}
	now := s.now()
	traceID := "trace_feishu_webhook_test"
	sendResult, sendErr := s.provider.Send(ctx, FeishuSendRequest{
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
		return TestResult{}, sendErr
	}

	status := ChannelTestStatusFailed
	testErr := strings.TrimSpace(sendResult.ErrorMessage)
	if sendResult.Outcome == FeishuSendOutcomeSuccess {
		status = ChannelTestStatusSuccess
		testErr = ""
	}
	if sendResult.Outcome == FeishuSendOutcomeSkipped && testErr == "" {
		testErr = "飞书 webhook 未配置或未启用"
	}
	if err := s.store.UpdateUserNotificationChannelTestResult(ctx, userID, model.NotificationChannelFeishuWebhook, status, testErr, now); err != nil {
		return TestResult{}, err
	}
	channel, err := s.GetFeishuWebhook(ctx, userID)
	if err != nil {
		return TestResult{}, err
	}
	return TestResult{
		Channel:  channel,
		Status:   status,
		Outcome:  string(sendResult.Outcome),
		Message:  testErr,
		TraceID:  traceID,
		SentAt:   now,
		Skipped:  sendResult.Outcome == FeishuSendOutcomeSkipped,
		Provider: ChannelFeishu,
	}, nil
}

func responseFromChannel(row *model.UserNotificationChannel) ChannelResponse {
	if row == nil {
		return ChannelResponse{
			Channel:    model.NotificationChannelFeishuWebhook,
			AuthType:   model.NotificationAuthTypeNone,
			Configured: false,
		}
	}
	return ChannelResponse{
		Channel:        row.Channel,
		Enabled:        row.Enabled,
		Configured:     strings.TrimSpace(row.WebhookURL) != "",
		WebhookURLMask: MaskWebhookURL(row.WebhookURL),
		AuthType:       normalizeAuthType(row.AuthType),
		HasBearerToken: strings.TrimSpace(row.BearerToken) != "",
		LastTestStatus: row.LastTestStatus,
		LastTestError:  row.LastTestError,
		LastTestAt:     row.LastTestAt,
	}
}

func normalizeAuthType(authType string) string {
	switch strings.ToLower(strings.TrimSpace(authType)) {
	case model.NotificationAuthTypeBearer:
		return model.NotificationAuthTypeBearer
	default:
		return model.NotificationAuthTypeNone
	}
}
