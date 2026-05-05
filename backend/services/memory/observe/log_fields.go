package observe

import (
	"context"
	"errors"
	"strings"
)

const (
	ComponentRead    = "read"
	ComponentWrite   = "write"
	ComponentInject  = "inject"
	ComponentManage  = "manage"
	ComponentCleanup = "cleanup"

	OperationRetrieve = "retrieve"
	OperationDecision = "decision"
	OperationInject   = "inject"
	OperationManage   = "manage"
	OperationDedup    = "dedup"

	MetricJobTotal               = "memory_job_total"
	MetricJobRetryTotal          = "memory_job_retry_total"
	MetricDecisionTotal          = "memory_decision_total"
	MetricDecisionFallbackTotal  = "memory_decision_fallback_total"
	MetricRetrieveHitTotal       = "memory_retrieve_hit_total"
	MetricRetrieveDedupDropTotal = "memory_retrieve_dedup_drop_total"
	MetricInjectItemTotal        = "memory_inject_item_total"
	MetricRAGFallbackTotal       = "memory_rag_fallback_total"
	MetricManageTotal            = "memory_manage_total"
	MetricCleanupRunTotal        = "memory_cleanup_run_total"
	MetricCleanupArchivedTotal   = "memory_cleanup_archived_total"
)

type fieldsContextKey struct{}

// WithFields 把 memory 链路公共字段挂进上下文，供下游日志复用。
//
// 职责边界：
// 1. 只负责字段透传与覆盖，不负责真正打印日志；
// 2. 只保留有意义的字段，避免结构化日志长期堆积空值；
// 3. 若上游已写入同名字段，则以后写值为准，方便链路逐层补齐上下文。
func WithFields(ctx context.Context, fields map[string]any) context.Context {
	if len(fields) == 0 {
		return ctx
	}
	if ctx == nil {
		ctx = context.Background()
	}

	merged := FieldsFromContext(ctx)
	for key, value := range fields {
		key = strings.TrimSpace(key)
		if key == "" || !shouldKeepField(value) {
			continue
		}
		merged[key] = value
	}
	if len(merged) == 0 {
		return ctx
	}
	return context.WithValue(ctx, fieldsContextKey{}, merged)
}

// FieldsFromContext 读取当前上下文中已经累积的观测字段。
func FieldsFromContext(ctx context.Context) map[string]any {
	if ctx == nil {
		return map[string]any{}
	}
	raw, ok := ctx.Value(fieldsContextKey{}).(map[string]any)
	if !ok || len(raw) == 0 {
		return map[string]any{}
	}

	result := make(map[string]any, len(raw))
	for key, value := range raw {
		result[key] = value
	}
	return result
}

// MergeFields 合并多份结构化字段，后写同名字段覆盖先写字段。
func MergeFields(parts ...map[string]any) map[string]any {
	result := make(map[string]any)
	for _, part := range parts {
		for key, value := range part {
			key = strings.TrimSpace(key)
			if key == "" || !shouldKeepField(value) {
				continue
			}
			result[key] = value
		}
	}
	return result
}

// ClassifyError 把常见错误压成稳定错误码，便于日志与指标统一聚合。
func ClassifyError(err error) string {
	switch {
	case err == nil:
		return ""
	case errors.Is(err, context.DeadlineExceeded):
		return "deadline_exceeded"
	case errors.Is(err, context.Canceled):
		return "canceled"
	default:
		return "memory_error"
	}
}

func shouldKeepField(value any) bool {
	if value == nil {
		return false
	}
	if text, ok := value.(string); ok {
		return strings.TrimSpace(text) != ""
	}
	return true
}
