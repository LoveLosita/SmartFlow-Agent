package model

import "time"

// ItemDTO 是记忆条目对外读写 DTO。
//
// 职责边界：
// 1. 面向 memory 模块内部服务层使用；
// 2. 不直接绑定 GORM 标签，避免传输结构与存储结构强耦合。
type ItemDTO struct {
	ID               int64
	UserID           int
	ConversationID   string
	AssistantID      string
	RunID            string
	MemoryType       string
	Title            string
	Content          string
	ContentHash      string
	Confidence       float64
	Importance       float64
	SensitivityLevel int
	IsExplicit       bool
	Status           string
	TTLAt            *time.Time
	CreatedAt        *time.Time
	UpdatedAt        *time.Time
}

// ItemQuery 描述 memory_items 的通用查询条件。
//
// 职责边界：
// 1. 只表达 memory 仓储层需要的过滤条件；
// 2. 不直接承载注入策略、重排策略等上层业务语义；
// 3. IncludeGlobal 用于“会话级 + 全局级”混合读取场景。
type ItemQuery struct {
	UserID         int
	ConversationID string
	AssistantID    string
	RunID          string
	Statuses       []string
	MemoryTypes    []string
	IncludeGlobal  bool
	OnlyUnexpired  bool
	Limit          int
	Now            time.Time
}

// RetrieveRequest 描述“供提示词注入前读取”所需的最小参数。
type RetrieveRequest struct {
	Query          string
	UserID         int
	ConversationID string
	AssistantID    string
	RunID          string
	MemoryTypes    []string
	Limit          int
	Now            time.Time
}

// ListItemsRequest 描述记忆管理页列表查询参数。
type ListItemsRequest struct {
	UserID         int
	ConversationID string
	Statuses       []string
	MemoryTypes    []string
	Limit          int
}

// CreateItemFields 是 repo 层落库时真正需要的字段集合。
type CreateItemFields struct {
	UserID            int
	ConversationID    string
	AssistantID       string
	RunID             string
	MemoryType        string
	Title             string
	Content           string
	NormalizedContent string
	ContentHash       string
	Confidence        float64
	Importance        float64
	SensitivityLevel  int
	IsExplicit        bool
	Status            string
	TTLAt             *time.Time
	VectorStatus      string
	SourceMessageID   *int64
	SourceEventID     *string
	LastAccessAt      *time.Time
}

// UpdateItemFields 是“用户管理侧修改记忆”时 repo 层允许更新的字段集合。
type UpdateItemFields struct {
	MemoryType        string
	Title             string
	Content           string
	NormalizedContent string
	ContentHash       string
	Confidence        float64
	Importance        float64
	SensitivityLevel  int
	IsExplicit        bool
	TTLAt             *time.Time
}
