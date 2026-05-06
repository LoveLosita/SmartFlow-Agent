package llm

import (
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"

	llmcontracts "github.com/LoveLosita/smartflow/backend/shared/contracts/llm"
	"github.com/cloudwego/eino/schema"
)

// EnsureTextBillingIdentity 负责在老调用点未显式提供 event_id 时兜底补稳定事件号。
//
// 兼容策略：
// 1. 只在 user_id、request_id 已具备且 event_id 为空时触发，避免覆盖显式幂等键；
// 2. stage 优先从 GenerateOptions.Metadata["stage"] 读取，兼容 agent 现有大量调用点；
// 3. 输入摘要使用 messages 的稳定哈希，确保同一 request_id 下不同阶段/不同输入不会串账。
func EnsureTextBillingIdentity(billing BillingContext, options llmcontracts.GenerateOptions, messages []*schema.Message) BillingContext {
	return ensureBillingIdentity(billing, readStageFromMetadata(options.Metadata), messages)
}

// EnsureResponsesBillingIdentity 负责给 Responses 调用补稳定事件号。
func EnsureResponsesBillingIdentity(billing BillingContext, messages []llmcontracts.ResponsesMessage) BillingContext {
	return ensureBillingIdentity(billing, "", messages)
}

func ensureBillingIdentity(billing BillingContext, stage string, payload any) BillingContext {
	billing = billing.Normalize()
	if billing.UserID == 0 || strings.TrimSpace(billing.RequestID) == "" || strings.TrimSpace(billing.EventID) != "" {
		return billing
	}

	stage = strings.TrimSpace(stage)
	billing.EventID = buildStableBillingEventID(billing.RequestID, stage, hashPayload(payload))
	return billing
}

func readStageFromMetadata(metadata map[string]any) string {
	if len(metadata) == 0 {
		return ""
	}
	raw, ok := metadata["stage"]
	if !ok || raw == nil {
		return ""
	}
	return strings.TrimSpace(fmt.Sprint(raw))
}

func hashPayload(payload any) string {
	raw, err := json.Marshal(payload)
	if err != nil || len(raw) == 0 {
		return ""
	}
	sum := sha1.Sum(raw)
	return hex.EncodeToString(sum[:8])
}

func buildStableBillingEventID(requestID, stage, payloadDigest string) string {
	requestID = strings.TrimSpace(requestID)
	stage = strings.TrimSpace(stage)
	payloadDigest = strings.TrimSpace(payloadDigest)

	base := requestID + "|" + stage + "|" + payloadDigest
	sum := sha1.Sum([]byte(base))
	return requestID + ":" + hex.EncodeToString(sum[:8])
}
