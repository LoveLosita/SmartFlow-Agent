package kafka

import (
	"strings"
	"time"

	"github.com/spf13/viper"
)

const (
	DefaultTopic = "smartflow.agent.outbox"
	DefaultGroup = "smartflow-agent-outbox-consumer"
)

// Config 描述 outbox 异步链路所需的 Kafka 配置。
// 说明：这些参数同时影响“发送端（producer）”与“消费端（consumer）”。
type Config struct {
	Enabled bool
	Brokers []string
	Topic   string
	GroupID string
	// ServiceName 表示当前进程所属的 outbox 服务；为空时保持单体全量模式。
	ServiceName string
	// RetryScanInterval/RetryBatchSize/MaxRetry 作用于 outbox 扫描与失败重试。
	RetryScanInterval time.Duration
	RetryBatchSize    int
	MaxRetry          int
}

// LoadConfig 从配置中心读取 Kafka 配置，并做兜底默认值。
// 兼容性：优先读取 kafka.brokers（数组），为空时降级读取 kafka.broker（单值）。
func LoadConfig() Config {
	brokers := viper.GetStringSlice("kafka.brokers")
	if len(brokers) == 0 {
		single := strings.TrimSpace(viper.GetString("kafka.broker"))
		if single != "" {
			brokers = []string{single}
		}
	}
	cfg := Config{
		Enabled:           viper.GetBool("kafka.enabled"),
		Brokers:           brokers,
		Topic:             strings.TrimSpace(viper.GetString("kafka.topic")),
		GroupID:           strings.TrimSpace(viper.GetString("kafka.groupID")),
		ServiceName:       strings.TrimSpace(viper.GetString("outbox.serviceName")),
		RetryScanInterval: viper.GetDuration("kafka.retryScanInterval"),
		RetryBatchSize:    viper.GetInt("kafka.retryBatchSize"),
		MaxRetry:          viper.GetInt("kafka.maxRetry"),
	}
	if cfg.ServiceName == "" {
		cfg.ServiceName = strings.TrimSpace(viper.GetString("kafka.serviceName"))
	}
	if cfg.Topic == "" {
		cfg.Topic = DefaultTopic
	}
	if cfg.GroupID == "" {
		cfg.GroupID = DefaultGroup
	}
	if cfg.RetryScanInterval <= 0 {
		cfg.RetryScanInterval = time.Second
	}
	if cfg.RetryBatchSize <= 0 {
		cfg.RetryBatchSize = 100
	}
	if cfg.MaxRetry <= 0 {
		cfg.MaxRetry = 20
	}
	return cfg
}
