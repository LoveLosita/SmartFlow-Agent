package llm

import (
	"context"
	"strings"
)

type billingContextKey struct{}

// BillingContext 描述一次 LLM 调用必需的计费上下文。
//
// 职责边界：
// 1. 只承载计费、审计、幂等所需的调用上下文；
// 2. 不承载 Temperature / MaxTokens 这类模型行为参数；
// 3. 不混入 prompt 文本，避免把业务输入复制成第二份协议。
type BillingContext struct {
	UserID         uint64 `json:"user_id"`
	EventID        string `json:"event_id"`
	Scene          string `json:"scene"`
	RequestID      string `json:"request_id"`
	ConversationID string `json:"conversation_id"`
	ModelAlias     string `json:"model_alias"`
	SkipCharge     bool   `json:"skip_charge"`
}

// Normalize 返回去空格后的 BillingContext 副本。
func (c BillingContext) Normalize() BillingContext {
	c.EventID = strings.TrimSpace(c.EventID)
	c.Scene = strings.TrimSpace(c.Scene)
	c.RequestID = strings.TrimSpace(c.RequestID)
	c.ConversationID = strings.TrimSpace(c.ConversationID)
	c.ModelAlias = strings.TrimSpace(c.ModelAlias)
	return c
}

// IsZero 判断是否完全没有注入计费上下文。
func (c BillingContext) IsZero() bool {
	return c.UserID == 0 &&
		strings.TrimSpace(c.EventID) == "" &&
		strings.TrimSpace(c.Scene) == "" &&
		strings.TrimSpace(c.RequestID) == "" &&
		strings.TrimSpace(c.ConversationID) == "" &&
		strings.TrimSpace(c.ModelAlias) == "" &&
		!c.SkipCharge
}

// WithBillingContext 把计费上下文挂进调用 ctx。
//
// 设计说明：
// 1. 这次优先保持 GenerateText / GenerateJSON / Stream 原有签名基本不变；
// 2. 计费必填信息不再塞进 GenerateOptions.Metadata，而是走强语义 ctx；
// 3. 后续若统一切为显式 request struct，可继续复用本结构体，不改业务语义。
func WithBillingContext(ctx context.Context, billing BillingContext) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	billing = billing.Normalize()
	return context.WithValue(ctx, billingContextKey{}, billing)
}

// BillingContextFromContext 读取调用上下文中的计费信息。
func BillingContextFromContext(ctx context.Context) (BillingContext, bool) {
	if ctx == nil {
		return BillingContext{}, false
	}
	value := ctx.Value(billingContextKey{})
	billing, ok := value.(BillingContext)
	if !ok {
		return BillingContext{}, false
	}
	billing = billing.Normalize()
	if billing.IsZero() {
		return BillingContext{}, false
	}
	return billing, true
}
