package observe

import (
	"context"
	"fmt"
	"log"
	"sort"
	"strings"
)

// Level 表示 memory 结构化观测事件等级。
type Level string

const (
	LevelInfo  Level = "info"
	LevelWarn  Level = "warn"
	LevelError Level = "error"
)

// Event 描述一次 memory 模块内部结构化观测事件。
//
// 职责边界：
// 1. 只承载稳定字段，不绑定具体日志平台；
// 2. 组件与操作名尽量保持低基数，避免后续指标聚合失控；
// 3. 字段内容应偏“排障与治理”，不承载大段原始文本。
type Event struct {
	Level     Level
	Component string
	Operation string
	Fields    map[string]any
}

// Observer 是 memory 模块的最小观测接口。
type Observer interface {
	Observe(ctx context.Context, event Event)
}

// ObserverFunc 允许用函数快速适配 Observer。
type ObserverFunc func(ctx context.Context, event Event)

func (f ObserverFunc) Observe(ctx context.Context, event Event) {
	if f == nil {
		return
	}
	f(ctx, event)
}

// NewNopObserver 返回空实现，保证观测能力不会反向阻塞主链路。
func NewNopObserver() Observer {
	return ObserverFunc(func(context.Context, Event) {})
}

// NewLoggerObserver 返回标准日志实现，当前阶段默认打到后端进程日志。
func NewLoggerObserver(logger *log.Logger) Observer {
	if logger == nil {
		logger = log.Default()
	}
	return &loggerObserver{logger: logger}
}

type loggerObserver struct {
	logger *log.Logger
}

func (o *loggerObserver) Observe(ctx context.Context, event Event) {
	if o == nil || o.logger == nil {
		return
	}

	level := strings.TrimSpace(string(event.Level))
	if level == "" {
		level = string(LevelInfo)
	}
	component := strings.TrimSpace(event.Component)
	if component == "" {
		component = "unknown"
	}
	operation := strings.TrimSpace(event.Operation)
	if operation == "" {
		operation = "unknown"
	}

	fields := FieldsFromContext(ctx)
	for key, value := range event.Fields {
		key = strings.TrimSpace(key)
		if key == "" || !shouldKeepField(value) {
			continue
		}
		fields[key] = value
	}

	parts := []string{
		"memory",
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
