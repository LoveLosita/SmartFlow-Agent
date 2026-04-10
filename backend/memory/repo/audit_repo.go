package repo

import (
	"context"
	"errors"

	"github.com/LoveLosita/smartflow/backend/model"
	"gorm.io/gorm"
)

// AuditRepo 封装 memory_audit_logs 的数据访问。
type AuditRepo struct {
	db *gorm.DB
}

func NewAuditRepo(db *gorm.DB) *AuditRepo {
	return &AuditRepo{db: db}
}

func (r *AuditRepo) Create(ctx context.Context, log model.MemoryAuditLog) error {
	if r == nil || r.db == nil {
		return errors.New("memory audit repo is nil")
	}
	return r.db.WithContext(ctx).Create(&log).Error
}
