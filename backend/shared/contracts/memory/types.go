package memory

import "time"

// ListItemsRequest 是 gateway 查询记忆管理列表时传给 memory 服务的契约。
//
// 职责边界：
// 1. UserID 由 gateway 鉴权后补齐，不信任前端传入；
// 2. ConversationID / Statuses / MemoryTypes / Limit 只表达查询条件，不承载过滤策略；
// 3. 具体默认状态、最大 limit 和越权判断仍由 memory 服务内部处理。
type ListItemsRequest struct {
	UserID         int      `json:"user_id"`
	ConversationID string   `json:"conversation_id,omitempty"`
	Statuses       []string `json:"statuses,omitempty"`
	MemoryTypes    []string `json:"memory_types,omitempty"`
	Limit          int      `json:"limit,omitempty"`
}

// RetrieveRequest 描述 agent 主链路注入记忆前的跨进程读取请求。
//
// 职责边界：
// 1. 只表达“按当前用户输入召回候选记忆”所需的最小参数；
// 2. 不承载 prompt 渲染、缓存预取、降级策略，这些仍由 agent 服务负责；
// 3. Now 允许调用方传入统一时间基准，空值时由 memory 服务复用既有默认逻辑。
type RetrieveRequest struct {
	Query          string    `json:"query,omitempty"`
	UserID         int       `json:"user_id"`
	ConversationID string    `json:"conversation_id,omitempty"`
	AssistantID    string    `json:"assistant_id,omitempty"`
	RunID          string    `json:"run_id,omitempty"`
	MemoryTypes    []string  `json:"memory_types,omitempty"`
	Limit          int       `json:"limit,omitempty"`
	Now            time.Time `json:"now,omitempty"`
}

// GetItemRequest 描述查看当前用户某条记忆的跨进程请求。
type GetItemRequest struct {
	UserID   int   `json:"user_id"`
	MemoryID int64 `json:"memory_id"`
}

// CreateItemRequest 描述手动新增记忆的跨进程请求。
//
// 职责边界：
// 1. UserID / OperatorType 由 gateway 填充，前端只提交业务字段；
// 2. Confidence / Importance 等指针字段保留“未传”和“显式零值”的区别；
// 3. 业务校验、审计、向量同步仍归 memory 服务内部负责。
type CreateItemRequest struct {
	UserID           int        `json:"user_id"`
	ConversationID   string     `json:"conversation_id,omitempty"`
	AssistantID      string     `json:"assistant_id,omitempty"`
	RunID            string     `json:"run_id,omitempty"`
	MemoryType       string     `json:"memory_type"`
	Title            string     `json:"title"`
	Content          string     `json:"content"`
	Confidence       *float64   `json:"confidence,omitempty"`
	Importance       *float64   `json:"importance,omitempty"`
	SensitivityLevel *int       `json:"sensitivity_level,omitempty"`
	IsExplicit       *bool      `json:"is_explicit,omitempty"`
	TTLAt            *time.Time `json:"ttl_at,omitempty"`
	Reason           string     `json:"reason,omitempty"`
	OperatorType     string     `json:"operator_type,omitempty"`
}

// UpdateItemRequest 描述手动修改记忆的跨进程请求。
type UpdateItemRequest struct {
	UserID           int        `json:"user_id"`
	MemoryID         int64      `json:"memory_id"`
	MemoryType       *string    `json:"memory_type,omitempty"`
	Title            *string    `json:"title,omitempty"`
	Content          *string    `json:"content,omitempty"`
	Confidence       *float64   `json:"confidence,omitempty"`
	Importance       *float64   `json:"importance,omitempty"`
	SensitivityLevel *int       `json:"sensitivity_level,omitempty"`
	IsExplicit       *bool      `json:"is_explicit,omitempty"`
	TTLAt            *time.Time `json:"ttl_at,omitempty"`
	ClearTTL         bool       `json:"clear_ttl,omitempty"`
	Reason           string     `json:"reason,omitempty"`
	OperatorType     string     `json:"operator_type,omitempty"`
}

// DeleteItemRequest 描述软删除记忆的跨进程请求。
type DeleteItemRequest struct {
	UserID       int    `json:"user_id"`
	MemoryID     int64  `json:"memory_id"`
	Reason       string `json:"reason,omitempty"`
	OperatorType string `json:"operator_type,omitempty"`
}

// RestoreItemRequest 描述恢复 deleted/archived 记忆的跨进程请求。
type RestoreItemRequest struct {
	UserID       int    `json:"user_id"`
	MemoryID     int64  `json:"memory_id"`
	Reason       string `json:"reason,omitempty"`
	OperatorType string `json:"operator_type,omitempty"`
}

// ItemView 是 memory 管理接口对 gateway 返回的稳定 JSON 视图。
//
// 职责边界：
// 1. 只保存前端可见字段，不暴露 GORM 字段或内部向量同步状态；
// 2. JSON 字段名保持原 `/api/v1/memory/items` 语义，避免 CP2 切流影响前端；
// 3. 时间字段继续使用 time.Time 指针，由标准 JSON 编码输出 RFC3339。
type ItemView struct {
	ID               int64      `json:"id"`
	UserID           int        `json:"user_id"`
	ConversationID   string     `json:"conversation_id,omitempty"`
	AssistantID      string     `json:"assistant_id,omitempty"`
	RunID            string     `json:"run_id,omitempty"`
	MemoryType       string     `json:"memory_type"`
	Title            string     `json:"title"`
	Content          string     `json:"content"`
	ContentHash      string     `json:"content_hash,omitempty"`
	Confidence       float64    `json:"confidence"`
	Importance       float64    `json:"importance"`
	SensitivityLevel int        `json:"sensitivity_level"`
	IsExplicit       bool       `json:"is_explicit"`
	Status           string     `json:"status"`
	TTLAt            *time.Time `json:"ttl_at,omitempty"`
	CreatedAt        *time.Time `json:"created_at,omitempty"`
	UpdatedAt        *time.Time `json:"updated_at,omitempty"`
}

// ItemDTO 是 agent 读取链路使用的记忆传输视图。
//
// 迁移期 retrieve 与管理接口共享同一组可传输字段，避免在 contract 层维护两份
// 形状完全一致的结构；后续若 agent 读取需要隐藏或新增字段，再单独拆出独立 DTO。
type ItemDTO = ItemView
