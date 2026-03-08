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

type Config struct {
	Enabled           bool
	Brokers           []string
	Topic             string
	GroupID           string
	RetryScanInterval time.Duration
	RetryBatchSize    int
	MaxRetry          int
}

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
		RetryScanInterval: viper.GetDuration("kafka.retryScanInterval"),
		RetryBatchSize:    viper.GetInt("kafka.retryBatchSize"),
		MaxRetry:          viper.GetInt("kafka.maxRetry"),
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
