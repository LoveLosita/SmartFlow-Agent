package feishu

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	notificationmodel "github.com/LoveLosita/smartflow/backend/services/notification/model"
	"gorm.io/gorm"
)

const (
	defaultWebhookTimeout     = 5 * time.Second
	defaultFrontendBaseURL    = "https://smartflow.example.com"
	webhookPayloadEvent       = "smartflow.schedule_adjustment_ready"
	webhookPayloadVersion     = "1"
	webhookMessageTitle       = "SmartFlow 日程调整建议"
	webhookMessageActionText  = "查看并确认调整"
	maxWebhookResponseBodyLen = 64 * 1024
)

// ChannelReader 描述 webhook provider 读取用户通知配置所需的最小能力。
//
// 职责边界：
// 1. 只读取 user_id + channel 对应的配置；
// 2. 不负责保存配置和测试结果；
// 3. 生产环境由 notification/dao.ChannelDAO 实现，测试可替换为内存 fake。
type ChannelReader interface {
	GetUserNotificationChannel(ctx context.Context, userID int, channel string) (*notificationmodel.UserNotificationChannel, error)
}

type WebhookProviderOptions struct {
	HTTPClient      *http.Client
	FrontendBaseURL string
	Timeout         time.Duration
	Now             func() time.Time
}

// WebhookProvider 把 SmartFlow 通知事件发送到用户配置的飞书 Webhook 触发器。
//
// 职责边界：
// 1. 只负责读取用户 webhook 配置、拼装极简业务 JSON 并执行 HTTP POST；
// 2. 不负责 notification_records 的创建、重试节奏和幂等；
// 3. 不实现飞书群自定义机器人 msg_type 协议，私聊/群发由飞书流程自行编排。
type WebhookProvider struct {
	store           ChannelReader
	client          *http.Client
	frontendBaseURL string
	now             func() time.Time
}

type WebhookPayload struct {
	Event          string         `json:"event"`
	Version        string         `json:"version"`
	NotificationID int64          `json:"notification_id"`
	UserID         int            `json:"user_id"`
	PreviewID      string         `json:"preview_id"`
	TriggerID      string         `json:"trigger_id"`
	TriggerType    string         `json:"trigger_type"`
	TargetType     string         `json:"target_type"`
	TargetID       int            `json:"target_id"`
	Message        WebhookMessage `json:"message"`
	TraceID        string         `json:"trace_id,omitempty"`
	SentAt         string         `json:"sent_at"`
}

type WebhookMessage struct {
	Title      string `json:"title"`
	Summary    string `json:"summary"`
	ActionText string `json:"action_text"`
	ActionURL  string `json:"action_url"`
}

func NewWebhookProvider(store ChannelReader, opts WebhookProviderOptions) (*WebhookProvider, error) {
	if store == nil {
		return nil, errors.New("user notification channel store is nil")
	}
	timeout := opts.Timeout
	if timeout <= 0 {
		timeout = defaultWebhookTimeout
	}
	client := opts.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: timeout}
	}
	now := opts.Now
	if now == nil {
		now = time.Now
	}
	return &WebhookProvider{
		store:           store,
		client:          client,
		frontendBaseURL: normalizeFrontendBaseURL(opts.FrontendBaseURL),
		now:             now,
	}, nil
}

// BuildWebhookPayload 生成飞书 Webhook 触发器消费的极简业务 JSON。
//
// 说明：
// 1. 该结构不包含飞书群机器人 msg_type 字段；
// 2. message 四个字段是飞书流程拼私聊消息的稳定输入；
// 3. 其它字段用于用户流程分支、SmartFlow 排障和审计。
func BuildWebhookPayload(req SendRequest, frontendBaseURL string, sentAt time.Time) WebhookPayload {
	if sentAt.IsZero() {
		sentAt = time.Now()
	}
	summary := strings.TrimSpace(req.MessageText)
	if summary == "" {
		summary = "我为你生成了一份日程调整建议，请回到系统确认是否应用。"
	}
	return WebhookPayload{
		Event:          webhookPayloadEvent,
		Version:        webhookPayloadVersion,
		NotificationID: req.NotificationID,
		UserID:         req.UserID,
		PreviewID:      strings.TrimSpace(req.PreviewID),
		TriggerID:      strings.TrimSpace(req.TriggerID),
		TriggerType:    strings.TrimSpace(req.TriggerType),
		TargetType:     strings.TrimSpace(req.TargetType),
		TargetID:       req.TargetID,
		Message: WebhookMessage{
			Title:      webhookMessageTitle,
			Summary:    summary,
			ActionText: webhookMessageActionText,
			ActionURL:  buildActionURL(frontendBaseURL, req.TargetURL),
		},
		TraceID: strings.TrimSpace(req.TraceID),
		SentAt:  sentAt.Format(time.RFC3339),
	}
}

// Send 向用户配置的飞书 Webhook 触发器投递一次 SmartFlow 通知事件。
func (p *WebhookProvider) Send(ctx context.Context, req SendRequest) (SendResult, error) {
	if p == nil || p.store == nil || p.client == nil {
		return SendResult{}, errors.New("webhook feishu provider 未初始化")
	}
	config, err := p.store.GetUserNotificationChannel(ctx, req.UserID, notificationmodel.ChannelFeishuWebhook)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return skippedResult(req, "用户未配置飞书 Webhook 触发器"), nil
		}
		return SendResult{}, err
	}
	if config == nil || !config.Enabled || strings.TrimSpace(config.WebhookURL) == "" {
		return skippedResult(req, "用户未启用飞书 Webhook 触发器"), nil
	}
	if err = ValidateWebhookURL(config.WebhookURL); err != nil {
		return SendResult{
			Outcome:      SendOutcomePermanentFail,
			ErrorCode:    ErrorCodeInvalidURL,
			ErrorMessage: err.Error(),
			RequestPayload: map[string]any{
				"notification_id": req.NotificationID,
				"user_id":         req.UserID,
				"webhook":         MaskWebhookURL(config.WebhookURL),
			},
		}, nil
	}

	payload := BuildWebhookPayload(req, p.frontendBaseURL, p.now())
	raw, err := json.Marshal(payload)
	if err != nil {
		return SendResult{}, err
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimSpace(config.WebhookURL), bytes.NewReader(raw))
	if err != nil {
		return permanentWebhookResult(req, payload, nil, ErrorCodeInvalidURL, err.Error()), nil
	}
	httpReq.Header.Set("Content-Type", "application/json; charset=utf-8")
	if strings.EqualFold(strings.TrimSpace(config.AuthType), notificationmodel.AuthTypeBearer) && strings.TrimSpace(config.BearerToken) != "" {
		httpReq.Header.Set("Authorization", "Bearer "+strings.TrimSpace(config.BearerToken))
	}

	resp, err := p.client.Do(httpReq)
	if err != nil {
		return temporaryWebhookResult(req, payload, nil, classifyNetworkError(err), err.Error()), nil
	}
	defer resp.Body.Close()

	body, readErr := io.ReadAll(io.LimitReader(resp.Body, maxWebhookResponseBodyLen))
	responsePayload := buildWebhookResponsePayload(resp.StatusCode, body, readErr)
	if readErr != nil {
		return temporaryWebhookResult(req, payload, responsePayload, ErrorCodeNetworkError, readErr.Error()), nil
	}
	return classifyWebhookHTTPResult(req, payload, responsePayload, resp.StatusCode, body), nil
}

func classifyWebhookHTTPResult(req SendRequest, payload WebhookPayload, responsePayload map[string]any, statusCode int, body []byte) SendResult {
	if statusCode >= 200 && statusCode < 300 {
		if len(strings.TrimSpace(string(body))) > 0 {
			var parsed struct {
				Code *int   `json:"code"`
				Msg  string `json:"msg"`
			}
			if err := json.Unmarshal(body, &parsed); err == nil && parsed.Code != nil && *parsed.Code != 0 {
				return permanentWebhookResult(req, payload, responsePayload, ErrorCodePayloadInvalid, firstNonEmpty(parsed.Msg, fmt.Sprintf("飞书 webhook 返回 code=%d", *parsed.Code)))
			}
		}
		return SendResult{
			Outcome:           SendOutcomeSuccess,
			ProviderMessageID: fmt.Sprintf("feishu_webhook_%d_%d", req.NotificationID, time.Now().UnixNano()),
			RequestPayload:    payload,
			ResponsePayload:   responsePayload,
		}
	}
	switch {
	case statusCode == http.StatusTooManyRequests:
		return temporaryWebhookResult(req, payload, responsePayload, ErrorCodeProviderRateLimited, fmt.Sprintf("飞书 webhook HTTP %d", statusCode))
	case statusCode >= 500:
		return temporaryWebhookResult(req, payload, responsePayload, ErrorCodeProvider5xx, fmt.Sprintf("飞书 webhook HTTP %d", statusCode))
	case statusCode == http.StatusUnauthorized || statusCode == http.StatusForbidden:
		return permanentWebhookResult(req, payload, responsePayload, ErrorCodeProviderAuthFailed, fmt.Sprintf("飞书 webhook 鉴权失败 HTTP %d", statusCode))
	default:
		return permanentWebhookResult(req, payload, responsePayload, ErrorCodePayloadInvalid, fmt.Sprintf("飞书 webhook HTTP %d", statusCode))
	}
}

func skippedResult(req SendRequest, reason string) SendResult {
	return SendResult{
		Outcome:      SendOutcomeSkipped,
		ErrorCode:    ErrorCodeRecipientMissing,
		ErrorMessage: reason,
		RequestPayload: map[string]any{
			"notification_id": req.NotificationID,
			"user_id":         req.UserID,
			"preview_id":      req.PreviewID,
		},
		ResponsePayload: map[string]any{
			"skipped": true,
			"reason":  reason,
		},
	}
}

func temporaryWebhookResult(_ SendRequest, payload WebhookPayload, responsePayload any, code string, message string) SendResult {
	return SendResult{
		Outcome:         SendOutcomeTemporaryFail,
		ErrorCode:       code,
		ErrorMessage:    message,
		RequestPayload:  payload,
		ResponsePayload: responsePayload,
	}
}

func permanentWebhookResult(_ SendRequest, payload WebhookPayload, responsePayload any, code string, message string) SendResult {
	return SendResult{
		Outcome:         SendOutcomePermanentFail,
		ErrorCode:       code,
		ErrorMessage:    message,
		RequestPayload:  payload,
		ResponsePayload: responsePayload,
	}
}

func buildWebhookResponsePayload(statusCode int, body []byte, readErr error) map[string]any {
	payload := map[string]any{
		"status_code": statusCode,
	}
	if len(body) > 0 {
		payload["body"] = string(body)
	}
	if readErr != nil {
		payload["read_error"] = readErr.Error()
	}
	return payload
}

func classifyNetworkError(err error) string {
	if errors.Is(err, context.DeadlineExceeded) {
		return ErrorCodeProviderTimeout
	}
	return ErrorCodeNetworkError
}

func normalizeFrontendBaseURL(value string) string {
	trimmed := strings.TrimRight(strings.TrimSpace(value), "/")
	if trimmed == "" {
		return defaultFrontendBaseURL
	}
	return trimmed
}

func buildActionURL(frontendBaseURL string, targetURL string) string {
	targetURL = strings.TrimSpace(targetURL)
	if strings.HasPrefix(targetURL, "https://") || strings.HasPrefix(targetURL, "http://") {
		return targetURL
	}
	base := normalizeFrontendBaseURL(frontendBaseURL)
	return base + "/" + strings.TrimLeft(targetURL, "/")
}

// ValidateWebhookURL 校验第一版允许保存的飞书 Webhook 触发器地址。
func ValidateWebhookURL(rawURL string) error {
	parsed, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil {
		return err
	}
	if parsed.Scheme != "https" {
		return errors.New("飞书 webhook 必须使用 https")
	}
	host := strings.ToLower(parsed.Hostname())
	if host != "www.feishu.cn" && host != "feishu.cn" {
		return errors.New("飞书 webhook 域名必须是 feishu.cn")
	}
	if !strings.HasPrefix(parsed.EscapedPath(), "/flow/api/trigger-webhook/") {
		return errors.New("飞书 webhook 路径必须是 /flow/api/trigger-webhook/{key}")
	}
	return nil
}

// MaskWebhookURL 对 webhook URL 做脱敏，避免接口和日志泄露完整密钥。
func MaskWebhookURL(rawURL string) string {
	trimmed := strings.TrimSpace(rawURL)
	parsed, err := url.Parse(trimmed)
	if err != nil || parsed.Host == "" {
		return maskMiddle(trimmed)
	}
	parts := strings.Split(strings.Trim(parsed.Path, "/"), "/")
	if len(parts) == 0 {
		return parsed.Scheme + "://" + parsed.Host
	}
	last := parts[len(parts)-1]
	parts[len(parts)-1] = maskMiddle(last)
	parsed.Path = "/" + strings.Join(parts, "/")
	parsed.RawQuery = ""
	parsed.Fragment = ""
	return parsed.String()
}

func MaskSecret(value string) string {
	return maskMiddle(strings.TrimSpace(value))
}

func maskMiddle(value string) string {
	if value == "" {
		return ""
	}
	runes := []rune(value)
	if len(runes) <= 8 {
		return "****"
	}
	return string(runes[:4]) + "..." + string(runes[len(runes)-4:])
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}
