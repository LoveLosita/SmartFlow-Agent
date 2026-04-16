package service

import (
	"context"
	"errors"
	"strings"
	"time"

	memorymodel "github.com/LoveLosita/smartflow/backend/memory/model"
	memoryobserve "github.com/LoveLosita/smartflow/backend/memory/observe"
	memoryrepo "github.com/LoveLosita/smartflow/backend/memory/repo"
	memoryutils "github.com/LoveLosita/smartflow/backend/memory/utils"
	memoryvectorsync "github.com/LoveLosita/smartflow/backend/memory/vectorsync"
	"github.com/LoveLosita/smartflow/backend/model"
	"github.com/LoveLosita/smartflow/backend/respond"
	"gorm.io/gorm"
)

const (
	defaultManageListLimit  = 20
	maxManageListLimit      = 100
	defaultManualConfidence = 0.95
	defaultManualImportance = 0.90
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
	vectorSyncer *memoryvectorsync.Syncer
	observer     memoryobserve.Observer
	metrics      memoryobserve.MetricsRecorder
}

func NewManageService(
	db *gorm.DB,
	itemRepo *memoryrepo.ItemRepo,
	auditRepo *memoryrepo.AuditRepo,
	settingsRepo *memoryrepo.SettingsRepo,
	vectorSyncer *memoryvectorsync.Syncer,
	observer memoryobserve.Observer,
	metrics memoryobserve.MetricsRecorder,
) *ManageService {
	if observer == nil {
		observer = memoryobserve.NewNopObserver()
	}
	if metrics == nil {
		metrics = memoryobserve.NewNopMetrics()
	}
	return &ManageService{
		db:           db,
		itemRepo:     itemRepo,
		auditRepo:    auditRepo,
		settingsRepo: settingsRepo,
		vectorSyncer: vectorSyncer,
		observer:     observer,
		metrics:      metrics,
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

// GetItem 返回“当前用户自己的某条记忆”详情。
func (s *ManageService) GetItem(ctx context.Context, req model.MemoryGetItemRequest) (*memorymodel.ItemDTO, error) {
	if s == nil || s.itemRepo == nil {
		return nil, errors.New("memory manage service is nil")
	}
	if req.UserID <= 0 {
		return nil, respond.WrongUserID
	}
	if req.MemoryID <= 0 {
		return nil, respond.WrongParamType
	}

	item, err := s.itemRepo.GetByIDForUser(ctx, req.UserID, req.MemoryID)
	if err != nil {
		return nil, translateManageError(err)
	}
	dto := toItemDTO(*item)
	return &dto, nil
}

// CreateItem 手动新增一条用户记忆，并补审计与向量同步桥接。
func (s *ManageService) CreateItem(ctx context.Context, req model.MemoryCreateItemRequest) (*memorymodel.ItemDTO, error) {
	if s == nil || s.db == nil || s.itemRepo == nil || s.auditRepo == nil {
		return nil, errors.New("memory manage service is not initialized")
	}
	if req.UserID <= 0 {
		return nil, respond.WrongUserID
	}

	fields, err := buildCreateItemFields(req)
	if err != nil {
		s.recordManageAction(ctx, "create", req.UserID, 0, fields.MemoryType, false, err)
		return nil, err
	}

	var createdItem model.MemoryItem
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		itemRepo := s.itemRepo.WithTx(tx)
		auditRepo := s.auditRepo.WithTx(tx)

		created, createErr := itemRepo.Create(ctx, fields)
		if createErr != nil {
			return createErr
		}
		createdItem = *created

		audit := memoryutils.BuildItemAuditLog(
			createdItem.ID,
			createdItem.UserID,
			memoryutils.AuditOperationCreate,
			memoryutils.NormalizeOperatorType(req.OperatorType),
			normalizeManageReason(req.Reason, "用户手动新增记忆"),
			nil,
			&createdItem,
		)
		return auditRepo.Create(ctx, audit)
	})
	if err != nil {
		err = translateManageError(err)
		s.recordManageAction(ctx, "create", req.UserID, 0, fields.MemoryType, false, err)
		return nil, err
	}

	s.vectorSyncer.Upsert(ctx, "", []model.MemoryItem{createdItem})
	s.recordManageAction(ctx, "create", req.UserID, createdItem.ID, createdItem.MemoryType, true, nil)
	dto := toItemDTO(createdItem)
	return &dto, nil
}

// UpdateItem 手动修改一条用户记忆，并补审计与向量重同步桥接。
func (s *ManageService) UpdateItem(ctx context.Context, req model.MemoryUpdateItemRequest) (*memorymodel.ItemDTO, error) {
	if s == nil || s.db == nil || s.itemRepo == nil || s.auditRepo == nil {
		return nil, errors.New("memory manage service is not initialized")
	}
	if req.UserID <= 0 {
		return nil, respond.WrongUserID
	}
	if req.MemoryID <= 0 {
		return nil, respond.WrongParamType
	}

	var updatedItem model.MemoryItem
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		itemRepo := s.itemRepo.WithTx(tx)
		auditRepo := s.auditRepo.WithTx(tx)

		current, getErr := itemRepo.GetByIDForUser(ctx, req.UserID, req.MemoryID)
		if getErr != nil {
			return getErr
		}

		fields, afterItem, buildErr := buildUpdateItemFields(req, *current)
		if buildErr != nil {
			return buildErr
		}
		now := time.Now()
		afterItem.UpdatedAt = &now
		afterItem.VectorStatus = "pending"

		if updateErr := itemRepo.UpdateManagedFieldsByIDAt(ctx, req.UserID, req.MemoryID, fields, now); updateErr != nil {
			return updateErr
		}

		audit := memoryutils.BuildItemAuditLog(
			current.ID,
			current.UserID,
			memoryutils.AuditOperationUpdate,
			memoryutils.NormalizeOperatorType(req.OperatorType),
			normalizeManageReason(req.Reason, "用户手动修改记忆"),
			current,
			&afterItem,
		)
		if auditErr := auditRepo.Create(ctx, audit); auditErr != nil {
			return auditErr
		}

		updatedItem = afterItem
		return nil
	})
	if err != nil {
		err = translateManageError(err)
		s.recordManageAction(ctx, "update", req.UserID, req.MemoryID, resolveUpdateMemoryType(req), false, err)
		return nil, err
	}

	s.vectorSyncer.Upsert(ctx, "", []model.MemoryItem{updatedItem})
	s.recordManageAction(ctx, "update", req.UserID, updatedItem.ID, updatedItem.MemoryType, true, nil)
	dto := toItemDTO(updatedItem)
	return &dto, nil
}

// DeleteItem 软删除一条记忆，并补写审计日志。
//
// 步骤化说明：
// 1. 先在事务里读取当前条目快照，确保审计前镜像和实际删除对象一致；
// 2. 若该条目已是 deleted，则直接按幂等语义返回，避免重复写多条删除审计；
// 3. 状态更新成功后再写 audit log，保证“有删除就有审计”，失败时整笔事务回滚。
func (s *ManageService) DeleteItem(ctx context.Context, req model.MemoryDeleteItemRequest) (*memorymodel.ItemDTO, error) {
	if s == nil || s.db == nil || s.itemRepo == nil || s.auditRepo == nil {
		return nil, errors.New("memory manage service is not initialized")
	}
	if req.UserID <= 0 {
		return nil, respond.WrongUserID
	}
	if req.MemoryID <= 0 {
		return nil, respond.WrongParamType
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
		after.VectorStatus = "pending"

		if err = itemRepo.SoftDeleteByID(ctx, req.UserID, req.MemoryID); err != nil {
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
		err = translateManageError(err)
		s.recordManageAction(ctx, "delete", req.UserID, req.MemoryID, "", false, err)
		return nil, err
	}
	if deletedItem.ID <= 0 {
		return nil, nil
	}

	if deletedItem.Status == model.MemoryItemStatusDeleted {
		s.vectorSyncer.Delete(ctx, "", []int64{deletedItem.ID})
	}
	s.recordManageAction(ctx, "delete", req.UserID, deletedItem.ID, deletedItem.MemoryType, true, nil)
	result := toItemDTO(deletedItem)
	return &result, nil
}

// RestoreItem 把 archived/deleted 记忆恢复为 active，并补审计与向量同步桥接。
func (s *ManageService) RestoreItem(ctx context.Context, req model.MemoryRestoreItemRequest) (*memorymodel.ItemDTO, error) {
	if s == nil || s.db == nil || s.itemRepo == nil || s.auditRepo == nil {
		return nil, errors.New("memory manage service is not initialized")
	}
	if req.UserID <= 0 {
		return nil, respond.WrongUserID
	}
	if req.MemoryID <= 0 {
		return nil, respond.WrongParamType
	}

	var restoredItem model.MemoryItem
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		itemRepo := s.itemRepo.WithTx(tx)
		auditRepo := s.auditRepo.WithTx(tx)

		current, getErr := itemRepo.GetByIDForUser(ctx, req.UserID, req.MemoryID)
		if getErr != nil {
			return getErr
		}
		if current.Status == model.MemoryItemStatusActive {
			restoredItem = *current
			return nil
		}

		now := time.Now()
		before := *current
		after := before
		after.Status = model.MemoryItemStatusActive
		after.UpdatedAt = &now
		after.VectorStatus = "pending"

		if restoreErr := itemRepo.RestoreByIDAt(ctx, req.UserID, req.MemoryID, now); restoreErr != nil {
			return restoreErr
		}

		audit := memoryutils.BuildItemAuditLog(
			before.ID,
			before.UserID,
			memoryutils.AuditOperationRestore,
			memoryutils.NormalizeOperatorType(req.OperatorType),
			normalizeManageReason(req.Reason, "用户恢复记忆"),
			&before,
			&after,
		)
		if auditErr := auditRepo.Create(ctx, audit); auditErr != nil {
			return auditErr
		}

		restoredItem = after
		return nil
	})
	if err != nil {
		err = translateManageError(err)
		s.recordManageAction(ctx, "restore", req.UserID, req.MemoryID, "", false, err)
		return nil, err
	}

	s.vectorSyncer.Upsert(ctx, "", []model.MemoryItem{restoredItem})
	s.recordManageAction(ctx, "restore", req.UserID, restoredItem.ID, restoredItem.MemoryType, true, nil)
	dto := toItemDTO(restoredItem)
	return &dto, nil
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

func normalizeManageReason(reason string, fallback string) string {
	reason = strings.TrimSpace(reason)
	if reason == "" {
		return fallback
	}
	return reason
}

func translateManageError(err error) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, gorm.ErrRecordNotFound):
		return respond.MemoryItemNotFound
	default:
		return err
	}
}

func buildCreateItemFields(req model.MemoryCreateItemRequest) (memorymodel.CreateItemFields, error) {
	memoryType, err := normalizeManagedMemoryType(req.MemoryType)
	if err != nil {
		return memorymodel.CreateItemFields{}, err
	}
	content, normalizedContent, err := normalizeManagedContent(req.Content)
	if err != nil {
		return memorymodel.CreateItemFields{}, err
	}

	title := normalizeManagedTitle(req.Title, content)
	return memorymodel.CreateItemFields{
		UserID:            req.UserID,
		ConversationID:    strings.TrimSpace(req.ConversationID),
		AssistantID:       strings.TrimSpace(req.AssistantID),
		RunID:             strings.TrimSpace(req.RunID),
		MemoryType:        memoryType,
		Title:             title,
		Content:           content,
		NormalizedContent: normalizedContent,
		ContentHash:       memoryutils.HashContent(memoryType, normalizedContent),
		Confidence:        normalizeManageScore(req.Confidence, defaultManualConfidence),
		Importance:        normalizeManageScore(req.Importance, defaultManualImportance),
		SensitivityLevel:  normalizeManageSensitivity(req.SensitivityLevel, 0),
		IsExplicit:        normalizeManageBool(req.IsExplicit, true),
		Status:            model.MemoryItemStatusActive,
		TTLAt:             req.TTLAt,
		VectorStatus:      "pending",
	}, nil
}

func buildUpdateItemFields(
	req model.MemoryUpdateItemRequest,
	current model.MemoryItem,
) (memorymodel.UpdateItemFields, model.MemoryItem, error) {
	memoryType := current.MemoryType
	if req.MemoryType != nil {
		normalizedType, err := normalizeManagedMemoryType(*req.MemoryType)
		if err != nil {
			return memorymodel.UpdateItemFields{}, model.MemoryItem{}, err
		}
		memoryType = normalizedType
	}

	content := current.Content
	if req.Content != nil {
		normalizedContentValue, _, err := normalizeManagedContent(*req.Content)
		if err != nil {
			return memorymodel.UpdateItemFields{}, model.MemoryItem{}, err
		}
		content = normalizedContentValue
	}
	normalizedContent := normalizeContentForHash(content)
	if normalizedContent == "" {
		return memorymodel.UpdateItemFields{}, model.MemoryItem{}, respond.MemoryInvalidContent
	}

	title := current.Title
	if req.Title != nil {
		title = normalizeManagedTitle(*req.Title, content)
	}
	ttlAt := current.TTLAt
	if req.ClearTTL {
		ttlAt = nil
	} else if req.TTLAt != nil {
		ttlAt = req.TTLAt
	}

	fields := memorymodel.UpdateItemFields{
		MemoryType:        memoryType,
		Title:             title,
		Content:           content,
		NormalizedContent: normalizedContent,
		ContentHash:       memoryutils.HashContent(memoryType, normalizedContent),
		Confidence:        normalizeManageScore(req.Confidence, current.Confidence),
		Importance:        normalizeManageScore(req.Importance, current.Importance),
		SensitivityLevel:  normalizeManageSensitivity(req.SensitivityLevel, current.SensitivityLevel),
		IsExplicit:        normalizeManageBool(req.IsExplicit, current.IsExplicit),
		TTLAt:             ttlAt,
	}

	after := current
	after.MemoryType = fields.MemoryType
	after.Title = fields.Title
	after.Content = fields.Content
	after.NormalizedContent = strPtr(fields.NormalizedContent)
	after.ContentHash = strPtr(fields.ContentHash)
	after.Confidence = fields.Confidence
	after.Importance = fields.Importance
	after.SensitivityLevel = fields.SensitivityLevel
	after.IsExplicit = fields.IsExplicit
	after.TTLAt = fields.TTLAt
	return fields, after, nil
}

func normalizeManagedMemoryType(raw string) (string, error) {
	normalized := memorymodel.NormalizeMemoryType(raw)
	if normalized == "" {
		return "", respond.MemoryInvalidType
	}
	return normalized, nil
}

func normalizeManagedContent(raw string) (string, string, error) {
	content := strings.TrimSpace(raw)
	if content == "" {
		return "", "", respond.MemoryInvalidContent
	}
	normalized := normalizeContentForHash(content)
	if normalized == "" {
		return "", "", respond.MemoryInvalidContent
	}
	return content, normalized, nil
}

func normalizeManagedTitle(raw string, content string) string {
	title := strings.TrimSpace(raw)
	if title != "" {
		return title
	}
	content = strings.TrimSpace(content)
	if content == "" {
		return "未命名记忆"
	}
	runes := []rune(content)
	if len(runes) > 24 {
		return string(runes[:24])
	}
	return content
}

func normalizeManageScore(value *float64, defaultValue float64) float64 {
	if value == nil {
		return clamp01(defaultValue)
	}
	return clamp01(*value)
}

func normalizeManageSensitivity(value *int, defaultValue int) int {
	if value == nil {
		return defaultValue
	}
	if *value < 0 {
		return defaultValue
	}
	return *value
}

func normalizeManageBool(value *bool, defaultValue bool) bool {
	if value == nil {
		return defaultValue
	}
	return *value
}

func resolveUpdateMemoryType(req model.MemoryUpdateItemRequest) string {
	if req.MemoryType == nil {
		return ""
	}
	return strings.TrimSpace(*req.MemoryType)
}

func strPtr(value string) *string {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	result := value
	return &result
}

func (s *ManageService) recordManageAction(
	ctx context.Context,
	operation string,
	userID int,
	memoryID int64,
	memoryType string,
	success bool,
	err error,
) {
	if s == nil {
		return
	}

	status := "success"
	level := memoryobserve.LevelInfo
	if !success || err != nil {
		status = "error"
		level = memoryobserve.LevelWarn
	}

	s.metrics.AddCounter(memoryobserve.MetricManageTotal, 1, map[string]string{
		"operation": strings.TrimSpace(operation),
		"status":    status,
	})
	s.observer.Observe(ctx, memoryobserve.Event{
		Level:     level,
		Component: memoryobserve.ComponentManage,
		Operation: memoryobserve.OperationManage,
		Fields: map[string]any{
			"user_id":     userID,
			"memory_id":   memoryID,
			"action":      strings.TrimSpace(operation),
			"memory_type": strings.TrimSpace(memoryType),
			"success":     success && err == nil,
			"error":       err,
			"error_code":  memoryobserve.ClassifyError(err),
		},
	})
}
