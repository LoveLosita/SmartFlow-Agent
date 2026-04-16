package worker

import (
	"context"
	"fmt"
	"strings"

	memorymodel "github.com/LoveLosita/smartflow/backend/memory/model"
	memoryrepo "github.com/LoveLosita/smartflow/backend/memory/repo"
	memoryutils "github.com/LoveLosita/smartflow/backend/memory/utils"
	"github.com/LoveLosita/smartflow/backend/model"
)

// ApplyActionOutcome 是单个决策动作的执行结果。
//
// 说明：
// 1. Action 记录本次执行的动作类型（ADD/UPDATE/DELETE/NONE）；
// 2. OldItem 仅在 UPDATE/DELETE 时有值，用于审计 before 快照；
// 3. NewItem 仅在 ADD/UPDATE 时有值，用于审计 after 快照和向量同步；
// 4. NeedsSync 标记是否需要触发向量同步（ADD 和 UPDATE 需要）。
type ApplyActionOutcome struct {
	Action    string
	MemoryID  int64
	OldItem   *model.MemoryItem // UPDATE/DELETE 时的 before 快照
	NewItem   *model.MemoryItem // ADD/UPDATE 时的 after 快照
	NeedsSync bool              // 是否需要向量同步
}

// ApplyFinalDecision 把汇总后的最终决策落为数据库动作。
//
// 职责边界：
// 1. 在调用方事务内执行，不做独立事务管理；
// 2. 负责写 memory_items + memory_audit_logs，不负责 job 状态推进；
// 3. 所有动作的审计日志都由这里统一产出。
//
// 参数说明：
// - itemRepo/auditRepo 必须是事务绑定的实例（WithTx 后的）；
// - fact 是当前正在处理的标准化事实；
// - job/payload 提供写入所需的上下文（user_id、conversation_id 等）。
func ApplyFinalDecision(
	ctx context.Context,
	itemRepo *memoryrepo.ItemRepo,
	auditRepo *memoryrepo.AuditRepo,
	decision memorymodel.FinalDecision,
	fact memorymodel.NormalizedFact,
	job *model.MemoryJob,
	payload memorymodel.ExtractJobPayload,
) (*ApplyActionOutcome, error) {
	switch decision.Action {
	case memorymodel.DecisionActionAdd:
		return applyAdd(ctx, itemRepo, auditRepo, fact, job, payload, decision.Reason)
	case memorymodel.DecisionActionUpdate:
		return applyUpdate(ctx, itemRepo, auditRepo, decision, fact, job, payload)
	case memorymodel.DecisionActionDelete:
		return applyDelete(ctx, itemRepo, auditRepo, decision, payload.UserID)
	case memorymodel.DecisionActionNone:
		return &ApplyActionOutcome{
			Action:    memorymodel.DecisionActionNone,
			NeedsSync: false,
		}, nil
	default:
		return nil, fmt.Errorf("未知的决策动作: %s", decision.Action)
	}
}

// applyAdd 执行新增动作：构建 MemoryItem → 写库 → 写审计。
func applyAdd(
	ctx context.Context,
	itemRepo *memoryrepo.ItemRepo,
	auditRepo *memoryrepo.AuditRepo,
	fact memorymodel.NormalizedFact,
	job *model.MemoryJob,
	payload memorymodel.ExtractJobPayload,
	reason string,
) (*ApplyActionOutcome, error) {
	// 1. 复用 runner.go 的 buildMemoryItems 构建单条 MemoryItem。
	items := buildMemoryItems(job, payload, []memorymodel.NormalizedFact{fact})
	if len(items) == 0 {
		return nil, fmt.Errorf("构建记忆条目失败: memory_type=%s", fact.MemoryType)
	}

	// 2. 写库，GORM Create 会自动填充 items[0].ID。
	if err := itemRepo.UpsertItems(ctx, items); err != nil {
		return nil, fmt.Errorf("新增记忆写入失败: %w", err)
	}
	// 注意：必须在 UpsertItems 之后取 items[0]，因为 GORM Create 回填 ID 到 items[i]，
	// 之前用 item := items[0] 在 UpsertItems 之前拷贝，导致副本 ID 永远为 0。
	item := items[0]

	// 3. 写审计日志（create 动作只有 after 快照）。
	audit := memoryutils.BuildItemAuditLog(
		item.ID,
		item.UserID,
		memoryutils.AuditOperationCreate,
		"system",
		formatAuditReason("决策层新增", reason),
		nil,
		&item,
	)
	if err := auditRepo.Create(ctx, audit); err != nil {
		return nil, fmt.Errorf("新增审计写入失败: %w", err)
	}

	return &ApplyActionOutcome{
		Action:    memorymodel.DecisionActionAdd,
		MemoryID:  item.ID,
		NewItem:   &item,
		NeedsSync: true,
	}, nil
}

// applyUpdate 执行更新动作：查 before → 更新字段 → 写审计（before+after）。
func applyUpdate(
	ctx context.Context,
	itemRepo *memoryrepo.ItemRepo,
	auditRepo *memoryrepo.AuditRepo,
	decision memorymodel.FinalDecision,
	fact memorymodel.NormalizedFact,
	job *model.MemoryJob,
	payload memorymodel.ExtractJobPayload,
) (*ApplyActionOutcome, error) {
	// 1. 查 before 快照，同时确认旧记忆存在且属于该用户。
	oldItem, err := itemRepo.GetByIDForUser(ctx, payload.UserID, decision.TargetID)
	if err != nil {
		return nil, fmt.Errorf("查询旧记忆失败(id=%d): %w", decision.TargetID, err)
	}

	// 2. 重新计算 NormalizedContent 和 ContentHash，保证和 NormalizeFacts 的逻辑一致。
	// 原因：LLM 输出的 merged content 需要重新走归一化链，避免大小写/空格差异导致后续 Hash 去重失效。
	updatedContent := strings.TrimSpace(decision.Content)
	if updatedContent == "" {
		updatedContent = fact.Content
	}
	normalizedContent := strings.ToLower(updatedContent)
	// 复用 utils.HashContent 的 sha256(memoryType + "::" + normalizedContent) 算法。
	contentHash := memoryutils.HashContent(fact.MemoryType, normalizedContent)

	title := strings.TrimSpace(decision.Title)
	if title == "" {
		title = oldItem.Title
	}

	// 3. 执行内容更新。
	fields := memorymodel.UpdateContentFields{
		Title:             title,
		Content:           updatedContent,
		NormalizedContent: normalizedContent,
		ContentHash:       contentHash,
		Confidence:        fact.Confidence,
		Importance:        fact.Importance,
	}
	if err := itemRepo.UpdateContentByID(ctx, decision.TargetID, fields); err != nil {
		return nil, fmt.Errorf("更新记忆内容失败(id=%d): %w", decision.TargetID, err)
	}

	// 4. 构造 after 快照用于审计。
	afterItem := *oldItem
	afterItem.Title = title
	afterItem.Content = updatedContent
	if afterItem.NormalizedContent != nil {
		afterItem.NormalizedContent = &normalizedContent
	} else {
		afterItem.NormalizedContent = strPtrFromValue(normalizedContent)
	}
	if afterItem.ContentHash != nil {
		afterItem.ContentHash = &contentHash
	} else {
		afterItem.ContentHash = strPtrFromValue(contentHash)
	}
	afterItem.Confidence = fact.Confidence
	afterItem.Importance = fact.Importance

	// 5. 写审计日志（update 动作同时有 before 和 after 快照）。
	audit := memoryutils.BuildItemAuditLog(
		oldItem.ID,
		oldItem.UserID,
		memoryutils.AuditOperationUpdate,
		"system",
		formatAuditReason("决策层更新", decision.Reason),
		oldItem,
		&afterItem,
	)
	if err := auditRepo.Create(ctx, audit); err != nil {
		return nil, fmt.Errorf("更新审计写入失败: %w", err)
	}

	// 6. 向量状态重置为 pending，触发向量重同步。
	// 原因：内容变了，旧向量已过期，需要重新 embed。
	_ = itemRepo.UpdateVectorStateByID(ctx, oldItem.ID, "pending", nil)

	return &ApplyActionOutcome{
		Action:    memorymodel.DecisionActionUpdate,
		MemoryID:  oldItem.ID,
		OldItem:   oldItem,
		NewItem:   &afterItem,
		NeedsSync: true,
	}, nil
}

// applyDelete 执行软删除动作：查 before → 软删 → 写审计（before only）。
func applyDelete(
	ctx context.Context,
	itemRepo *memoryrepo.ItemRepo,
	auditRepo *memoryrepo.AuditRepo,
	decision memorymodel.FinalDecision,
	userID int,
) (*ApplyActionOutcome, error) {
	// 1. 查 before 快照。
	oldItem, err := itemRepo.GetByIDForUser(ctx, userID, decision.TargetID)
	if err != nil {
		return nil, fmt.Errorf("查询旧记忆失败(id=%d): %w", decision.TargetID, err)
	}

	// 2. 执行软删除。
	if err := itemRepo.SoftDeleteByID(ctx, userID, decision.TargetID); err != nil {
		return nil, fmt.Errorf("软删除记忆失败(id=%d): %w", decision.TargetID, err)
	}

	// 3. 写审计日志（delete 动作只有 before 快照）。
	audit := memoryutils.BuildItemAuditLog(
		oldItem.ID,
		oldItem.UserID,
		memoryutils.AuditOperationDelete,
		"system",
		formatAuditReason("决策层删除", decision.Reason),
		oldItem,
		nil,
	)
	if err := auditRepo.Create(ctx, audit); err != nil {
		return nil, fmt.Errorf("删除审计写入失败: %w", err)
	}

	return &ApplyActionOutcome{
		Action:    memorymodel.DecisionActionDelete,
		MemoryID:  oldItem.ID,
		OldItem:   oldItem,
		NeedsSync: false,
	}, nil
}

// formatAuditReason 统一审计日志的 reason 格式。
func formatAuditReason(prefix, detail string) string {
	detail = strings.TrimSpace(detail)
	if detail == "" {
		return prefix
	}
	return prefix + ": " + detail
}
