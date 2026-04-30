package notification

import (
	"context"
	"fmt"
	"sync"
	"time"
)

// MockFeishuMode 描述 mock provider 下一次返回哪类结果。
type MockFeishuMode string

const (
	MockFeishuModeSuccess       MockFeishuMode = "success"
	MockFeishuModeTemporaryFail MockFeishuMode = "temporary_fail"
	MockFeishuModePermanentFail MockFeishuMode = "permanent_fail"
)

// MockFeishuProvider 是进程内 mock provider。
//
// 职责边界：
// 1. 只用于本地联调、单元测试和阶段性验收；
// 2. 不做真实 HTTP 调用，直接根据预设 mode 返回 success / temporary_fail / permanent_fail；
// 3. 保留调用历史，方便测试断言“有没有重复发飞书”。
type MockFeishuProvider struct {
	mu          sync.Mutex
	defaultMode MockFeishuMode
	queuedModes []MockFeishuMode
	calls       []FeishuSendRequest
}

// NewMockFeishuProvider 创建一个进程内 mock provider。
func NewMockFeishuProvider(defaultMode MockFeishuMode) *MockFeishuProvider {
	if defaultMode == "" {
		defaultMode = MockFeishuModeSuccess
	}
	return &MockFeishuProvider{defaultMode: defaultMode}
}

// SetDefaultMode 设置默认返回模式。
func (p *MockFeishuProvider) SetDefaultMode(mode MockFeishuMode) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if mode == "" {
		mode = MockFeishuModeSuccess
	}
	p.defaultMode = mode
}

// PushModes 追加一组“一次性模式”。
//
// 说明：
// 1. 先进先出消费，便于测试“先失败再成功”的重试路径；
// 2. 队列用尽后回退到 defaultMode；
// 3. 空模式会被自动忽略，避免测试代码误塞脏数据。
func (p *MockFeishuProvider) PushModes(modes ...MockFeishuMode) {
	p.mu.Lock()
	defer p.mu.Unlock()
	for _, mode := range modes {
		if mode == "" {
			continue
		}
		p.queuedModes = append(p.queuedModes, mode)
	}
}

// Calls 返回当前 provider 已记录的调用快照。
func (p *MockFeishuProvider) Calls() []FeishuSendRequest {
	p.mu.Lock()
	defer p.mu.Unlock()
	copied := make([]FeishuSendRequest, len(p.calls))
	copy(copied, p.calls)
	return copied
}

// Send 按预设模式返回模拟结果。
//
// 步骤说明：
// 1. 先记录本次请求，方便测试校验是否发生重复投递；
// 2. 再按 queuedModes -> defaultMode 的顺序决定 outcome；
// 3. 最后返回可落库审计的 request/response 摘要。
func (p *MockFeishuProvider) Send(_ context.Context, req FeishuSendRequest) (FeishuSendResult, error) {
	p.mu.Lock()
	p.calls = append(p.calls, req)

	mode := p.defaultMode
	if len(p.queuedModes) > 0 {
		mode = p.queuedModes[0]
		p.queuedModes = p.queuedModes[1:]
	}
	p.mu.Unlock()

	switch mode {
	case MockFeishuModeTemporaryFail:
		return FeishuSendResult{
			Outcome:      FeishuSendOutcomeTemporaryFail,
			ErrorCode:    FeishuErrorCodeProviderTimeout,
			ErrorMessage: "mock feishu provider temporary failure",
			RequestPayload: map[string]any{
				"notification_id": req.NotificationID,
				"user_id":         req.UserID,
				"preview_id":      req.PreviewID,
				"target_url":      req.TargetURL,
			},
			ResponsePayload: map[string]any{
				"mode":   string(mode),
				"reason": "mock temporary failure",
			},
		}, nil
	case MockFeishuModePermanentFail:
		return FeishuSendResult{
			Outcome:      FeishuSendOutcomePermanentFail,
			ErrorCode:    FeishuErrorCodePayloadInvalid,
			ErrorMessage: "mock feishu provider permanent failure",
			RequestPayload: map[string]any{
				"notification_id": req.NotificationID,
				"user_id":         req.UserID,
				"preview_id":      req.PreviewID,
				"target_url":      req.TargetURL,
			},
			ResponsePayload: map[string]any{
				"mode":   string(mode),
				"reason": "mock permanent failure",
			},
		}, nil
	default:
		return FeishuSendResult{
			Outcome:           FeishuSendOutcomeSuccess,
			ProviderMessageID: fmt.Sprintf("mock_feishu_%d", time.Now().UnixNano()),
			RequestPayload: map[string]any{
				"notification_id": req.NotificationID,
				"user_id":         req.UserID,
				"preview_id":      req.PreviewID,
				"target_url":      req.TargetURL,
			},
			ResponsePayload: map[string]any{
				"mode":   string(MockFeishuModeSuccess),
				"status": "ok",
			},
		}, nil
	}
}
