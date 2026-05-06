package outbox

import (
	"fmt"
	"sort"
	"strings"
	"sync"

	"github.com/spf13/viper"
)

const (
	ServiceAgent           = "agent"
	ServiceTask            = "task"
	ServiceMemory          = "memory"
	ServiceActiveScheduler = "active-scheduler"
	ServiceNotification    = "notification"
	ServiceTaskClassForum  = "taskclass-forum"
	ServiceLLM             = "llm"
	ServiceTokenStore      = "token-store"
)

// ServiceConfig 描述一个服务级 outbox 的固定归属。
//
// 职责边界：
// 1. 只描述“事件属于哪个服务、写哪张表、发哪个 topic、用哪个 group”。
// 2. 不承载具体业务 handler，也不承载 Kafka 消息体格式。
// 3. 服务级写入、扫描和消费都应从这里读取同一份映射，避免配置漂移。
type ServiceConfig struct {
	Name      string
	Topic     string
	GroupID   string
	TableName string
}

var serviceCatalogCache = struct {
	sync.RWMutex
	loaded  bool
	entries map[string]ServiceConfig
}{
	entries: make(map[string]ServiceConfig),
}

// LoadServiceConfigs 读取服务级 outbox 目录。
//
// 说明：
// 1. 先给出默认终态映射，再允许通过配置中心覆盖 topic/groupID/table；
// 2. 该目录只负责服务级 outbox 基础设施，不混入业务逻辑；
// 3. 若某个服务配置缺失，直接使用默认值，避免启动期因为非关键配置崩掉。
func LoadServiceConfigs() map[string]ServiceConfig {
	serviceCatalogCache.Lock()
	defer serviceCatalogCache.Unlock()

	if serviceCatalogCache.loaded {
		return cloneServiceConfigs(serviceCatalogCache.entries)
	}

	entries := map[string]ServiceConfig{
		ServiceAgent: {
			Name:      ServiceAgent,
			Topic:     "smartflow.agent.outbox",
			GroupID:   "smartflow-agent-outbox-consumer",
			TableName: "agent_outbox_messages",
		},
		ServiceTask: {
			Name:      ServiceTask,
			Topic:     "smartflow.task.outbox",
			GroupID:   "smartflow-task-outbox-consumer",
			TableName: "task_outbox_messages",
		},
		ServiceMemory: {
			Name:      ServiceMemory,
			Topic:     "smartflow.memory.outbox",
			GroupID:   "smartflow-memory-outbox-consumer",
			TableName: "memory_outbox_messages",
		},
		ServiceActiveScheduler: {
			Name:      ServiceActiveScheduler,
			Topic:     "smartflow.active-scheduler.outbox",
			GroupID:   "smartflow-active-scheduler-outbox-consumer",
			TableName: "active_scheduler_outbox_messages",
		},
		ServiceNotification: {
			Name:      ServiceNotification,
			Topic:     "smartflow.notification.outbox",
			GroupID:   "smartflow-notification-outbox-consumer",
			TableName: "notification_outbox_messages",
		},
		ServiceTaskClassForum: {
			Name:      ServiceTaskClassForum,
			Topic:     "smartflow.taskclass-forum.outbox",
			GroupID:   "smartflow-taskclass-forum-outbox-consumer",
			TableName: "taskclass_forum_outbox_messages",
		},
		ServiceLLM: {
			Name:      ServiceLLM,
			Topic:     "smartflow.llm.outbox",
			GroupID:   "smartflow-llm-outbox-consumer",
			TableName: "llm_outbox_messages",
		},
		ServiceTokenStore: {
			Name:      ServiceTokenStore,
			Topic:     "smartflow.token-store.outbox",
			GroupID:   "smartflow-token-store-outbox-consumer",
			TableName: "token_store_outbox_messages",
		},
	}

	for name, entry := range entries {
		entries[name] = overrideServiceConfig(entry)
	}

	serviceCatalogCache.entries = entries
	serviceCatalogCache.loaded = true
	return cloneServiceConfigs(entries)
}

// ResolveServiceConfig 查询某个服务的 outbox 目录。
func ResolveServiceConfig(serviceName string) (ServiceConfig, bool) {
	serviceName = strings.TrimSpace(serviceName)
	if serviceName == "" {
		return ServiceConfig{}, false
	}

	entries := LoadServiceConfigs()
	cfg, ok := entries[serviceName]
	return cfg, ok
}

// ResolveEventServiceConfig 先解析事件归属服务，再返回该服务的 outbox 目录。
func ResolveEventServiceConfig(eventType string) (ServiceConfig, bool) {
	serviceName, ok := ResolveEventService(eventType)
	if !ok {
		return ServiceConfig{}, false
	}
	return ResolveServiceConfig(serviceName)
}

// ServiceTables 返回当前目录中的所有 outbox 表名。
func ServiceTables() []string {
	entries := LoadServiceConfigs()
	tables := make([]string, 0, len(entries))
	for _, entry := range entries {
		tables = append(tables, entry.TableName)
	}
	sort.Strings(tables)
	return tables
}

// ServiceNames 返回当前目录中的所有服务名。
func ServiceNames() []string {
	entries := LoadServiceConfigs()
	names := make([]string, 0, len(entries))
	for name := range entries {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func overrideServiceConfig(entry ServiceConfig) ServiceConfig {
	upperName := strings.TrimSpace(entry.Name)
	if upperName == "" {
		return entry
	}

	topicKey := fmt.Sprintf("outbox.services.%s.topic", upperName)
	groupKey := fmt.Sprintf("outbox.services.%s.groupID", upperName)
	tableKey := fmt.Sprintf("outbox.services.%s.table", upperName)

	if topic := strings.TrimSpace(viper.GetString(topicKey)); topic != "" {
		entry.Topic = topic
	}
	if groupID := strings.TrimSpace(viper.GetString(groupKey)); groupID != "" {
		entry.GroupID = groupID
	}
	if tableName := strings.TrimSpace(viper.GetString(tableKey)); tableName != "" {
		entry.TableName = tableName
	}
	return entry
}

func cloneServiceConfigs(entries map[string]ServiceConfig) map[string]ServiceConfig {
	cloned := make(map[string]ServiceConfig, len(entries))
	for name, entry := range entries {
		cloned[name] = entry
	}
	return cloned
}
