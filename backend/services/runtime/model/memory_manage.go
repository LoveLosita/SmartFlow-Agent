package model

import "time"

// MemoryGetItemRequest 描述“查看我的某条记忆”所需的最小参数。
type MemoryGetItemRequest struct {
	UserID   int
	MemoryID int64
}

// MemoryCreateItemRequest 描述“手动新增一条记忆”的输入。
type MemoryCreateItemRequest struct {
	UserID           int        `json:"-"`
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
	OperatorType     string     `json:"-"`
}

// MemoryUpdateItemRequest 描述“手动修改一条记忆”的 Patch 输入。
//
// 说明：
// 1. 使用指针区分“未传字段”和“显式传零值”；
// 2. ClearTTL 用于表达“显式清空 ttl_at”；
// 3. 当前仍只允许修改内容侧字段，不开放跨用户、跨归属字段改写。
type MemoryUpdateItemRequest struct {
	UserID           int        `json:"-"`
	MemoryID         int64      `json:"-"`
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
	OperatorType     string     `json:"-"`
}

// MemoryDeleteItemRequest 描述“删除我的一条记忆”的输入。
type MemoryDeleteItemRequest struct {
	UserID       int
	MemoryID     int64
	Reason       string
	OperatorType string
}

// MemoryRestoreItemRequest 描述“恢复我的一条记忆”的输入。
type MemoryRestoreItemRequest struct {
	UserID       int
	MemoryID     int64
	Reason       string
	OperatorType string
}

// MemoryDedupCleanupRequest 描述离线去重治理任务的执行参数。
type MemoryDedupCleanupRequest struct {
	UserID       int
	Limit        int
	DryRun       bool
	Reason       string
	OperatorType string
}

// MemoryDedupCleanupResult 描述一次离线去重治理的汇总结果。
type MemoryDedupCleanupResult struct {
	ScannedGroupCount int     `json:"scanned_group_count"`
	DedupedGroupCount int     `json:"deduped_group_count"`
	KeptCount         int     `json:"kept_count"`
	ArchivedCount     int     `json:"archived_count"`
	ArchivedIDs       []int64 `json:"archived_ids,omitempty"`
	DryRun            bool    `json:"dry_run"`
}

// MemoryItemView 是前端可见的记忆条目视图。
type MemoryItemView struct {
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
