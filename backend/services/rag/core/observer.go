package core

import (
	"context"
	"errors"
	"fmt"
	"log"
	"sort"
	"strings"
)

// ObserveLevel 表示观测事件等级。
type ObserveLevel string

const (
	ObserveLevelInfo  ObserveLevel = "info"
	ObserveLevelWarn  ObserveLevel = "warn"
	ObserveLevelError ObserveLevel = "error"
)

// ObserveEvent 描述一次统一观测事件。
//
// 职责边界：
// 1. 只承载 RAG service 的结构化运行信息；
// 2. 不绑定具体日志系统、指标系统或 tracing 实现；
// 3. 字段内容应尽量稳定，便于后续统一接入全局观测平台。
type ObserveEvent struct {
	Level     ObserveLevel
	Component string
	Operation string
	Fields    map[string]any
}

// Observer 是 RAG service 的最小观测接口。
//
// 职责边界：
// 1. 负责消费结构化事件；
// 2. 不负责决定业务逻辑是否继续执行；
// 3. 任一实现都不应反向影响主链路稳定性。
type Observer interface {
	Observe(ctx context.Context, event ObserveEvent)
}

// ObserverFunc 允许用函数快速适配 Observer。
type ObserverFunc func(ctx context.Context, event ObserveEvent)

func (f ObserverFunc) Observe(ctx context.Context, event ObserveEvent) {
	if f == nil {
		return
	}
	f(ctx, event)
}

// NewNopObserver 返回空实现，适合在未接入统一观测平台时兜底。
func NewNopObserver() Observer {
	return ObserverFunc(func(context.Context, ObserveEvent) {})
}

// NewLoggerObserver 返回标准日志适配器。
//
// 说明：
// 1. 当前项目尚未建立统一日志平台时，先把结构化字段稳定打印出来；
// 2. 后续若项目引入统一 logger/metrics/tracing，只需替换该 Observer 注入实现；
// 3. 该适配器默认保持单行输出，减少和现有日志风格的割裂感。
func NewLoggerObserver(logger *log.Logger) Observer {
	if logger == nil {
		logger = log.Default()
	}
	return &loggerObserver{logger: logger}
}

type loggerObserver struct {
	logger *log.Logger
}

func (o *loggerObserver) Observe(ctx context.Context, event ObserveEvent) {
	if o == nil || o.logger == nil {
		return
	}

	level := strings.TrimSpace(string(event.Level))
	if level == "" {
		level = string(ObserveLevelInfo)
	}
	component := strings.TrimSpace(event.Component)
	if component == "" {
		component = "unknown"
	}
	operation := strings.TrimSpace(event.Operation)
	if operation == "" {
		operation = "unknown"
	}

	fields := ObserveFieldsFromContext(ctx)
	for key, value := range event.Fields {
		key = strings.TrimSpace(key)
		if key == "" || !shouldKeepObserveField(value) {
			continue
		}
		fields[key] = value
	}

	parts := []string{
		"rag",
		fmt.Sprintf("level=%s", level),
		fmt.Sprintf("component=%s", component),
		fmt.Sprintf("operation=%s", operation),
	}

	keys := make([]string, 0, len(fields))
	for key := range fields {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		parts = append(parts, fmt.Sprintf("%s=%v", key, fields[key]))
	}

	o.logger.Print(strings.Join(parts, " "))
}

type observeFieldsContextKey struct{}

// WithObserveFields 把通用观测字段挂入上下文，便于下游组件复用。
//
// 步骤化说明：
// 1. 先读取已有上下文字段，保证 Runtime / Pipeline / Store 能逐层补充信息；
// 2. 后写字段覆盖同名旧值，确保下游拿到的是最新语义；
// 3. 仅保存“有意义”的字段，避免日志长期堆积大量空值。
func WithObserveFields(ctx context.Context, fields map[string]any) context.Context {
	if len(fields) == 0 {
		return ctx
	}
	if ctx == nil {
		ctx = context.Background()
	}

	merged := ObserveFieldsFromContext(ctx)
	for key, value := range fields {
		key = strings.TrimSpace(key)
		if key == "" || !shouldKeepObserveField(value) {
			continue
		}
		merged[key] = value
	}
	if len(merged) == 0 {
		return ctx
	}
	return context.WithValue(ctx, observeFieldsContextKey{}, merged)
}

// ObserveFieldsFromContext 提取上下文中已经累积的观测字段。
func ObserveFieldsFromContext(ctx context.Context) map[string]any {
	if ctx == nil {
		return map[string]any{}
	}
	raw, ok := ctx.Value(observeFieldsContextKey{}).(map[string]any)
	if !ok || len(raw) == 0 {
		return map[string]any{}
	}
	result := make(map[string]any, len(raw))
	for key, value := range raw {
		result[key] = value
	}
	return result
}

// ClassifyErrorCode 统一把常见错误压缩为稳定错误码，便于后续接入全局观测平台。
func ClassifyErrorCode(err error) string {
	switch {
	case err == nil:
		return ""
	case errors.Is(err, context.DeadlineExceeded):
		return "DEADLINE_EXCEEDED"
	case errors.Is(err, context.Canceled):
		return "CANCELED"
	default:
		return "RAG_ERROR"
	}
}

func shouldKeepObserveField(value any) bool {
	if value == nil {
		return false
	}
	if text, ok := value.(string); ok {
		return strings.TrimSpace(text) != ""
	}
	return true
}
