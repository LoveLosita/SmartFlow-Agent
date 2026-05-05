package observe

import (
	"sort"
	"strings"
	"sync"
)

// CounterSnapshot 是轻量计数器的快照视图，供后续排障或接平台时读取。
type CounterSnapshot struct {
	Name   string
	Labels map[string]string
	Value  int64
}

// MetricsRecorder 描述 memory 模块对计数器的最小依赖。
type MetricsRecorder interface {
	AddCounter(name string, delta int64, labels map[string]string)
	Snapshot() []CounterSnapshot
}

// NewNopMetrics 返回空实现，保证无观测平台时仍可安全运行。
func NewNopMetrics() MetricsRecorder {
	return nopMetrics{}
}

type nopMetrics struct{}

func (nopMetrics) AddCounter(string, int64, map[string]string) {}

func (nopMetrics) Snapshot() []CounterSnapshot {
	return nil
}

// MetricsRegistry 是 memory 模块当前阶段的轻量内存计数器实现。
//
// 职责边界：
// 1. 只做线程安全计数，不负责导出协议；
// 2. 标签做低基数归一化，避免治理期临时字段把指标打爆；
// 3. 后续若项目统一接 Prometheus，可直接保留调用口径并替换实现。
type MetricsRegistry struct {
	mu       sync.RWMutex
	counters map[string]*counterRecord
}

type counterRecord struct {
	name   string
	labels map[string]string
	value  int64
}

func NewMetricsRegistry() *MetricsRegistry {
	return &MetricsRegistry{
		counters: make(map[string]*counterRecord),
	}
}

// AddCounter 追加计数值；delta<=0 时直接忽略，避免脏数据污染快照。
func (r *MetricsRegistry) AddCounter(name string, delta int64, labels map[string]string) {
	if r == nil || delta <= 0 {
		return
	}

	name = strings.TrimSpace(name)
	if name == "" {
		return
	}
	normalizedLabels := normalizeLabels(labels)
	key := buildCounterKey(name, normalizedLabels)

	r.mu.Lock()
	defer r.mu.Unlock()
	if existing, ok := r.counters[key]; ok {
		existing.value += delta
		return
	}
	r.counters[key] = &counterRecord{
		name:   name,
		labels: normalizedLabels,
		value:  delta,
	}
}

// Snapshot 返回当前全部计数器快照，便于后续排障或测试读取。
func (r *MetricsRegistry) Snapshot() []CounterSnapshot {
	if r == nil {
		return nil
	}

	r.mu.RLock()
	defer r.mu.RUnlock()
	if len(r.counters) == 0 {
		return nil
	}

	keys := make([]string, 0, len(r.counters))
	for key := range r.counters {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	result := make([]CounterSnapshot, 0, len(keys))
	for _, key := range keys {
		record := r.counters[key]
		labels := make(map[string]string, len(record.labels))
		for labelKey, labelValue := range record.labels {
			labels[labelKey] = labelValue
		}
		result = append(result, CounterSnapshot{
			Name:   record.name,
			Labels: labels,
			Value:  record.value,
		})
	}
	return result
}

func normalizeLabels(labels map[string]string) map[string]string {
	if len(labels) == 0 {
		return nil
	}

	result := make(map[string]string, len(labels))
	for key, value := range labels {
		key = strings.TrimSpace(key)
		value = strings.TrimSpace(value)
		if key == "" || value == "" {
			continue
		}
		result[key] = value
	}
	if len(result) == 0 {
		return nil
	}
	return result
}

func buildCounterKey(name string, labels map[string]string) string {
	if len(labels) == 0 {
		return name
	}

	keys := make([]string, 0, len(labels))
	for key := range labels {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	var sb strings.Builder
	sb.WriteString(name)
	for _, key := range keys {
		sb.WriteString("|")
		sb.WriteString(key)
		sb.WriteString("=")
		sb.WriteString(labels[key])
	}
	return sb.String()
}
