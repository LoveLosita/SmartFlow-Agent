package service

import (
	"context"
	"errors"
	"strings"
	"time"

	memorymodel "github.com/LoveLosita/smartflow/backend/memory/model"
	memoryrepo "github.com/LoveLosita/smartflow/backend/memory/repo"
	memoryutils "github.com/LoveLosita/smartflow/backend/memory/utils"
	"github.com/LoveLosita/smartflow/backend/model"
	"gorm.io/gorm"
)

const (
	defaultManageListLimit = 20
	maxManageListLimit     = 100
)

// ManageService 负责 memory 模块内部的管理面能力。
//
// 职责边界：
// 1. 负责“列出记忆 / 删除记忆 / 读取与更新用户开关”这类维护动作；
// 2. 负责把用户主动管理行为补充进 memory_audit_logs；
// 3. 不负责 prompt 注入、不负责向量召回，也不负责后台抽取任务执行。
type ManageService struct {
	db           *gorm.DB
	itemRepo     *memoryrepo.ItemRepo
	auditRepo    *memoryrepo.AuditRepo
	settingsRepo *memoryrepo.SettingsRepo
}

func NewManageService(
	db *gorm.DB,
	itemRepo *memoryrepo.ItemRepo,
	auditRepo *memoryrepo.AuditRepo,
	settingsRepo *memoryrepo.SettingsRepo,
) *ManageService {
	return &ManageService{
		db:           db,
		itemRepo:     itemRepo,
		auditRepo:    auditRepo,
		settingsRepo: settingsRepo,
	}
}

// ListItems 列出某个用户当前可管理的记忆条目。
//
// 说明：
// 1. 这里面向“管理视角”，不会按用户开关再做二次过滤；
// 2. 即便用户暂时关闭 memory，总览页仍需要看见已有记忆，便于手动删除或核对；
// 3. 默认只返回 active/archived，除非显式传入 deleted。
func (s *ManageService) ListItems(ctx context.Context, req memorymodel.ListItemsRequest) ([]memorymodel.ItemDTO, error) {
	if s == nil || s.itemRepo == nil {
		return nil, errors.New("memory manage service is nil")
	}
	if req.UserID <= 0 {
		return nil, nil
	}

	conversationID := strings.TrimSpace(req.ConversationID)
	query := memorymodel.ItemQuery{
		UserID:         req.UserID,
		ConversationID: conversationID,
		Statuses:       normalizeManageStatuses(req.Statuses),
		MemoryTypes:    normalizeMemoryTypes(req.MemoryTypes),
		IncludeGlobal:  conversationID != "",
		OnlyUnexpired:  false,
		Limit:          normalizeLimit(req.Limit, defaultManageListLimit, maxManageListLimit),
	}

	items, err := s.itemRepo.FindByQuery(ctx, query)
	if err != nil {
		return nil, err
	}
	return toItemDTOs(items), nil
}

// DeleteItem 软删除一条记忆，并补写审计日志。
//
// 步骤化说明：
// 1. 先在事务里读取当前条目快照，确保审计前镜像和实际删除对象一致；
// 2. 若该条目已是 deleted，则直接按幂等语义返回，避免重复写多条删除审计；
// 3. 状态更新成功后再写 audit log，保证“有删除就有审计”，失败时整笔事务回滚。
func (s *ManageService) DeleteItem(ctx context.Context, req memorymodel.DeleteItemRequest) (*memorymodel.ItemDTO, error) {
	if s == nil || s.db == nil || s.itemRepo == nil || s.auditRepo == nil {
		return nil, errors.New("memory manage service is not initialized")
	}
	if req.UserID <= 0 || req.MemoryID <= 0 {
		return nil, nil
	}

	now := time.Now()
	operatorType := memoryutils.NormalizeOperatorType(req.OperatorType)
	reason := normalizeDeleteReason(req.Reason)

	var deletedItem model.MemoryItem
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		itemRepo := s.itemRepo.WithTx(tx)
		auditRepo := s.auditRepo.WithTx(tx)

		current, err := itemRepo.GetByIDForUser(ctx, req.UserID, req.MemoryID)
		if err != nil {
			return err
		}
		if current.Status == model.MemoryItemStatusDeleted {
			deletedItem = *current
			return nil
		}

		before := *current
		after := before
		after.Status = model.MemoryItemStatusDeleted
		after.UpdatedAt = &now

		if err = itemRepo.UpdateStatusByIDAt(ctx, req.UserID, req.MemoryID, model.MemoryItemStatusDeleted, now); err != nil {
			return err
		}

		audit := memoryutils.BuildItemAuditLog(
			req.MemoryID,
			req.UserID,
			memoryutils.AuditOperationDelete,
			operatorType,
			reason,
			&before,
			&after,
		)
		if err = auditRepo.Create(ctx, audit); err != nil {
			return err
		}

		deletedItem = after
		return nil
	})
	if err != nil {
		return nil, err
	}
	if deletedItem.ID <= 0 {
		return nil, nil
	}

	result := toItemDTO(deletedItem)
	return &result, nil
}

// GetUserSetting 返回用户当前生效的记忆开关。
//
// 返回语义：
// 1. 若数据库中还没有记录，返回系统默认开关，而不是 nil；
// 2. 这样前端/上层调用方始终拿到完整结构，避免再做一层判空补默认值；
// 3. 这里只读 settings，不附带修改动作。
func (s *ManageService) GetUserSetting(ctx context.Context, userID int) (memorymodel.UserSettingDTO, error) {
	if s == nil || s.settingsRepo == nil {
		return memorymodel.UserSettingDTO{}, errors.New("memory manage service is nil")
	}
	if userID <= 0 {
		return memorymodel.UserSettingDTO{}, nil
	}

	setting, err := s.settingsRepo.GetByUserID(ctx, userID)
	if err != nil {
		return memorymodel.UserSettingDTO{}, err
	}
	return toUserSettingDTO(memoryutils.EffectiveUserSetting(setting, userID)), nil
}

// UpsertUserSetting 写入用户记忆开关。
//
// 说明：
// 1. 当前阶段先直接覆盖三类开关，不做 patch 语义；
// 2. 这样便于前端把整块设置表单一次性提交，接口语义更稳定；
// 3. 若后续需要记录设置变更审计，再单独扩展 setting audit，而不是复用 item audit。
func (s *ManageService) UpsertUserSetting(ctx context.Context, req memorymodel.UpdateUserSettingRequest) (memorymodel.UserSettingDTO, error) {
	if s == nil || s.settingsRepo == nil {
		return memorymodel.UserSettingDTO{}, errors.New("memory manage service is nil")
	}
	if req.UserID <= 0 {
		return memorymodel.UserSettingDTO{}, nil
	}

	now := time.Now()
	setting := model.MemoryUserSetting{
		UserID:                 req.UserID,
		MemoryEnabled:          req.MemoryEnabled,
		ImplicitMemoryEnabled:  req.ImplicitMemoryEnabled,
		SensitiveMemoryEnabled: req.SensitiveMemoryEnabled,
		UpdatedAt:              &now,
	}
	if err := s.settingsRepo.Upsert(ctx, setting); err != nil {
		return memorymodel.UserSettingDTO{}, err
	}
	return toUserSettingDTO(setting), nil
}

func normalizeDeleteReason(reason string) string {
	reason = strings.TrimSpace(reason)
	if reason == "" {
		return "用户删除记忆"
	}
	return reason
}
