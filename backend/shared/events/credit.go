package events

import (
	"errors"
	"fmt"
	"strings"
	"time"
)

const (
	// CreditChargeRequestedEventType 表示 LLM 服务已经拿到最终 usage，等待 TokenStore 异步结算。
	CreditChargeRequestedEventType = "credit.charge.requested"
	// CreditChargeEventVersion 是当前 Credit 扣费事件版本。
	CreditChargeEventVersion = "v1"
)

// CreditChargeRequestedPayload 是 LLM -> TokenStore 的统一扣费事件。
//
// 职责边界：
// 1. 只表达“一次已经完成的模型调用需要如何结算”，不承载准入决策；
// 2. event_id 是最终幂等键，LLM outbox 重试和 TokenStore 记账都依赖它去重；
// 3. credit_cost / rmb_cost_micros 在 LLM 侧就要算好，TokenStore 只负责权威落账。
type CreditChargeRequestedPayload struct {
	EventID         string    `json:"event_id"`
	UserID          uint64    `json:"user_id"`
	Scene           string    `json:"scene"`
	RequestID       string    `json:"request_id"`
	ConversationID  string    `json:"conversation_id"`
	ModelAlias      string    `json:"model_alias"`
	ProviderName    string    `json:"provider_name"`
	ModelName       string    `json:"model_name"`
	InputTokens     int64     `json:"input_tokens"`
	OutputTokens    int64     `json:"output_tokens"`
	CachedTokens    int64     `json:"cached_tokens"`
	ReasoningTokens int64     `json:"reasoning_tokens"`
	TotalTokens     int64     `json:"total_tokens"`
	RMBCostMicros   int64     `json:"rmb_cost_micros"`
	CreditCost      int64     `json:"credit_cost"`
	TriggeredAt     time.Time `json:"triggered_at"`
	SkipCharge      bool      `json:"skip_charge,omitempty"`
}

// EventType 返回当前 payload 对应的标准事件类型。
func (p CreditChargeRequestedPayload) EventType() string {
	return CreditChargeRequestedEventType
}

// MessageKey 返回 Kafka / outbox 统一消息键。
func (p CreditChargeRequestedPayload) MessageKey() string {
	return strings.TrimSpace(p.EventID)
}

// AggregateID 返回扣费事件聚合键。
func (p CreditChargeRequestedPayload) AggregateID() string {
	if p.UserID <= 0 {
		return ""
	}
	return fmt.Sprintf("user:%d", p.UserID)
}

// Validate 校验扣费事件最小字段集。
func (p CreditChargeRequestedPayload) Validate() error {
	if strings.TrimSpace(p.EventID) == "" {
		return errors.New("credit charge event_id 不能为空")
	}
	if p.UserID == 0 {
		return errors.New("credit charge user_id 不能为空")
	}
	if strings.TrimSpace(p.Scene) == "" {
		return errors.New("credit charge scene 不能为空")
	}
	if strings.TrimSpace(p.ProviderName) == "" {
		return errors.New("credit charge provider_name 不能为空")
	}
	if strings.TrimSpace(p.ModelName) == "" {
		return errors.New("credit charge model_name 不能为空")
	}
	if p.TriggeredAt.IsZero() {
		return errors.New("credit charge triggered_at 不能为空")
	}
	if p.SkipCharge {
		return nil
	}
	if p.CreditCost < 0 {
		return errors.New("credit charge credit_cost 不能为负数")
	}
	if p.RMBCostMicros < 0 {
		return errors.New("credit charge rmb_cost_micros 不能为负数")
	}
	return nil
}
