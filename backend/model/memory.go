package model

import "time"

const (
	// MemoryItemStatusActive 表示记忆条目可参与检索与注入。
	MemoryItemStatusActive = "active"
	// MemoryItemStatusArchived 表示记忆条目被归档，不再默认参与注入。
	MemoryItemStatusArchived = "archived"
	// MemoryItemStatusDeleted 表示记忆条目已软删除。
	MemoryItemStatusDeleted = "deleted"
)

const (
	// MemoryJobTypeExtract 表示“候选事实抽取”任务。
	MemoryJobTypeExtract = "extract"
	// MemoryJobTypeEmbed 表示“向量化同步”任务（Day1 仅预留）。
	MemoryJobTypeEmbed = "embed"
	// MemoryJobTypeReconcile 表示“冲突消解”任务（Day1 仅预留）。
	MemoryJobTypeReconcile = "reconcile"
)

const (
	// MemoryJobStatusPending 表示任务待执行。
	MemoryJobStatusPending = "pending"
	// MemoryJobStatusProcessing 表示任务执行中。
	MemoryJobStatusProcessing = "processing"
	// MemoryJobStatusSuccess 表示任务执行成功（最终态）。
	MemoryJobStatusSuccess = "success"
	// MemoryJobStatusFailed 表示任务执行失败但可重试。
	MemoryJobStatusFailed = "failed"
	// MemoryJobStatusDead 表示任务不可恢复失败（最终态）。
	MemoryJobStatusDead = "dead"
)

// MemoryItem 对应 memory_items 表，用于保存长期可注入记忆。
//
// 职责边界：
// 1. 该模型只定义存储结构，不承载抽取/决策业务逻辑；
// 2. Day1 先建表与基础字段，Day2 再补读取注入链路；
// 3. 向量字段（vector_status/vector_id）仅做状态桥接，不等于向量库真值。
type MemoryItem struct {
	ID int64 `gorm:"column:id;primaryKey;autoIncrement"`

	UserID         int     `gorm:"column:user_id;not null;index:idx_memory_items_user_status_type,priority:1;index:idx_memory_items_user_conv_status,priority:1;index:idx_memory_items_user_asst_run_status,priority:1;index:idx_memory_items_user_type_hash,priority:1;comment:用户ID"`
	ConversationID *string `gorm:"column:conversation_id;type:varchar(64);index:idx_memory_items_user_conv_status,priority:2;comment:会话ID"`
	AssistantID    *string `gorm:"column:assistant_id;type:varchar(64);index:idx_memory_items_user_asst_run_status,priority:2;comment:助手ID"`
	RunID          *string `gorm:"column:run_id;type:varchar(64);index:idx_memory_items_user_asst_run_status,priority:3;comment:运行ID"`

	MemoryType string `gorm:"column:memory_type;type:varchar(32);not null;index:idx_memory_items_user_status_type,priority:3;index:idx_memory_items_user_type_hash,priority:2;comment:preference/constraint/fact"`
	Title      string `gorm:"column:title;type:varchar(128);not null;comment:记忆标题"`
	Content    string `gorm:"column:content;type:text;not null;comment:记忆内容"`

	NormalizedContent *string `gorm:"column:normalized_content;type:text;comment:标准化内容"`
	ContentHash       *string `gorm:"column:content_hash;type:varchar(64);index:idx_memory_items_user_type_hash,priority:3;comment:幂等去重哈希"`

	Confidence       float64 `gorm:"column:confidence;type:decimal(5,4);not null;default:0.6;comment:置信度"`
	Importance       float64 `gorm:"column:importance;type:decimal(5,4);not null;default:0.5;comment:重要度"`
	SensitivityLevel int     `gorm:"column:sensitivity_level;not null;default:0;comment:敏感级别"`

	SourceMessageID *int64  `gorm:"column:source_message_id;index:idx_memory_items_source_message;comment:来源消息ID"`
	SourceEventID   *string `gorm:"column:source_event_id;type:varchar(64);comment:来源事件ID"`
	IsExplicit      bool    `gorm:"column:is_explicit;not null;default:false;comment:是否显式记忆"`

	Status string `gorm:"column:status;type:varchar(16);not null;default:active;index:idx_memory_items_user_status_type,priority:2;index:idx_memory_items_user_conv_status,priority:3;index:idx_memory_items_user_asst_run_status,priority:4;comment:active/archived/deleted"`

	TTLAt        *time.Time `gorm:"column:ttl_at;index:idx_memory_items_ttl;comment:过期时间"`
	LastAccessAt *time.Time `gorm:"column:last_access_at;comment:最后访问时间"`
	CreatedAt    *time.Time `gorm:"column:created_at;autoCreateTime"`
	UpdatedAt    *time.Time `gorm:"column:updated_at;autoUpdateTime"`

	VectorStatus string  `gorm:"column:vector_status;type:varchar(16);not null;default:pending;comment:pending/synced/failed"`
	VectorID     *string `gorm:"column:vector_id;type:varchar(128);comment:向量库映射ID"`
}

func (MemoryItem) TableName() string {
	return "memory_items"
}

// MemoryJob 对应 memory_jobs 表，用于承接异步任务。
//
// 职责边界：
// 1. 该表是“可重试状态机”，不是业务事实库；
// 2. payload_json 只存任务执行最小上下文；
// 3. status/retry_count/next_retry_at 组合定义可重试行为。
type MemoryJob struct {
	ID int64 `gorm:"column:id;primaryKey;autoIncrement"`

	UserID          int     `gorm:"column:user_id;not null;index:idx_memory_jobs_user_created,priority:1;comment:用户ID"`
	ConversationID  *string `gorm:"column:conversation_id;type:varchar(64);comment:会话ID"`
	SourceMessageID *int64  `gorm:"column:source_message_id;comment:来源消息ID"`
	SourceEventID   *string `gorm:"column:source_event_id;type:varchar(64);index:idx_memory_jobs_source_event;comment:来源事件ID"`

	JobType        string `gorm:"column:job_type;type:varchar(32);not null;comment:extract/embed/reconcile"`
	IdempotencyKey string `gorm:"column:idempotency_key;type:varchar(128);not null;uniqueIndex:uk_memory_jobs_idempotency;comment:幂等键"`
	PayloadJSON    string `gorm:"column:payload_json;type:longtext;not null;comment:任务载荷JSON"`

	Status      string     `gorm:"column:status;type:varchar(16);not null;index:idx_memory_jobs_status_next,priority:1;comment:pending/processing/success/failed/dead"`
	RetryCount  int        `gorm:"column:retry_count;not null;default:0;comment:已重试次数"`
	MaxRetry    int        `gorm:"column:max_retry;not null;default:6;comment:最大重试次数"`
	NextRetryAt *time.Time `gorm:"column:next_retry_at;index:idx_memory_jobs_status_next,priority:2;comment:下次重试时间"`
	LastError   *string    `gorm:"column:last_error;type:varchar(2000);comment:最后错误"`

	CreatedAt *time.Time `gorm:"column:created_at;autoCreateTime;index:idx_memory_jobs_user_created,priority:2"`
	UpdatedAt *time.Time `gorm:"column:updated_at;autoUpdateTime"`
}

func (MemoryJob) TableName() string {
	return "memory_jobs"
}

// MemoryAuditLog 对应 memory_audit_logs 表，用于记忆变更审计。
type MemoryAuditLog struct {
	ID int64 `gorm:"column:id;primaryKey;autoIncrement"`

	MemoryID int64 `gorm:"column:memory_id;not null;index:idx_memory_audit_memory_id;comment:记忆ID"`
	UserID   int   `gorm:"column:user_id;not null;index:idx_memory_audit_user_id;comment:用户ID"`

	Operation    string `gorm:"column:operation;type:varchar(32);not null;comment:create/update/archive/delete/restore"`
	OperatorType string `gorm:"column:operator_type;type:varchar(16);not null;comment:system/user"`
	Reason       string `gorm:"column:reason;type:varchar(255);not null;default:'';comment:操作原因"`

	BeforeJSON *string `gorm:"column:before_json;type:longtext;comment:变更前快照"`
	AfterJSON  *string `gorm:"column:after_json;type:longtext;comment:变更后快照"`

	CreatedAt *time.Time `gorm:"column:created_at;autoCreateTime"`
}

func (MemoryAuditLog) TableName() string {
	return "memory_audit_logs"
}

// MemoryUserSetting 对应 memory_user_settings 表，用于用户记忆开关控制。
type MemoryUserSetting struct {
	UserID int `gorm:"column:user_id;primaryKey;comment:用户ID"`

	MemoryEnabled          bool `gorm:"column:memory_enabled;not null;default:true;comment:总开关"`
	ImplicitMemoryEnabled  bool `gorm:"column:implicit_memory_enabled;not null;default:true;comment:隐式记忆开关"`
	SensitiveMemoryEnabled bool `gorm:"column:sensitive_memory_enabled;not null;default:false;comment:敏感记忆开关"`

	UpdatedAt *time.Time `gorm:"column:updated_at;autoUpdateTime"`
}

func (MemoryUserSetting) TableName() string {
	return "memory_user_settings"
}

// MemoryExtractRequestedPayload 是 memory.extract.requested(v1) 事件载荷。
//
// 说明：
// 1. Day1 先承载最小可执行字段；
// 2. assistant_id/run_id/source_message_id/trace_id 允许为空，后续链路补齐；
// 3. idempotency_key 必填，用于 memory_jobs 去重与无副作用消费。
type MemoryExtractRequestedPayload struct {
	UserID          int       `json:"user_id"`
	ConversationID  string    `json:"conversation_id"`
	AssistantID     string    `json:"assistant_id,omitempty"`
	RunID           string    `json:"run_id,omitempty"`
	SourceMessageID int64     `json:"source_message_id,omitempty"`
	SourceRole      string    `json:"source_role"`
	SourceText      string    `json:"source_text"`
	OccurredAt      time.Time `json:"occurred_at"`
	TraceID         string    `json:"trace_id,omitempty"`
	IdempotencyKey  string    `json:"idempotency_key"`
}
