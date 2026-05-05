package model

import "time"

// AuditLogDTO 是审计日志领域对象。
type AuditLogDTO struct {
	ID           int64
	MemoryID     int64
	UserID       int
	Operation    string
	OperatorType string
	Reason       string
	BeforeJSON   string
	AfterJSON    string
	CreatedAt    *time.Time
}
