package service

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/LoveLosita/smartflow/backend/dao"
	"github.com/LoveLosita/smartflow/backend/model"
	activeapply "github.com/LoveLosita/smartflow/backend/services/active_scheduler/core/apply"
	"github.com/LoveLosita/smartflow/backend/services/active_scheduler/core/applyadapter"
	activepreview "github.com/LoveLosita/smartflow/backend/services/active_scheduler/core/preview"
	"gorm.io/gorm"
)

// PreviewConfirmService 编排第三阶段的预览生成、查询和确认应用。
//
// 职责边界：
// 1. 复用 dry-run 结果写 preview，不重新实现候选生成；
// 2. confirm 时只负责 preview 状态、幂等和 apply port 调用编排；
// 3. 正式 schedule 写入仍由 applyadapter 在事务中完成。
type PreviewConfirmService struct {
	dryRun       *DryRunService
	preview      *activepreview.Service
	activeDAO    *dao.ActiveScheduleDAO
	applyAdapter scheduleApplyAdapter
	clock        func() time.Time
}

type scheduleApplyAdapter interface {
	ApplyActiveScheduleChanges(ctx context.Context, req applyadapter.ApplyActiveScheduleRequest) (applyadapter.ApplyActiveScheduleResult, error)
}

func NewPreviewConfirmService(dryRun *DryRunService, previewService *activepreview.Service, activeDAO *dao.ActiveScheduleDAO, applyAdapter scheduleApplyAdapter) (*PreviewConfirmService, error) {
	if dryRun == nil {
		return nil, errors.New("dry-run service 不能为空")
	}
	if previewService == nil {
		return nil, errors.New("preview service 不能为空")
	}
	if activeDAO == nil {
		return nil, errors.New("active schedule dao 不能为空")
	}
	if applyAdapter == nil {
		return nil, errors.New("apply adapter 不能为空")
	}
	return &PreviewConfirmService{
		dryRun:       dryRun,
		preview:      previewService,
		activeDAO:    activeDAO,
		applyAdapter: applyAdapter,
		clock:        time.Now,
	}, nil
}

func (s *PreviewConfirmService) SetClock(clock func() time.Time) {
	if s != nil && clock != nil {
		s.clock = clock
	}
}

func (s *PreviewConfirmService) CreatePreviewFromDryRun(ctx context.Context, req activepreview.CreatePreviewRequest) (*activepreview.CreatePreviewResponse, error) {
	if s == nil || s.preview == nil {
		return nil, errors.New("preview confirm service 未初始化")
	}
	return s.preview.CreatePreview(ctx, req)
}

func (s *PreviewConfirmService) GetPreview(ctx context.Context, userID int, previewID string) (*activepreview.ActiveSchedulePreviewDetail, error) {
	if s == nil || s.preview == nil {
		return nil, errors.New("preview confirm service 未初始化")
	}
	return s.preview.GetPreview(ctx, userID, previewID)
}

// ConfirmPreview 同步确认并应用主动调度预览。
//
// 步骤化说明：
// 1. 先读取 preview 并做同用户校验，避免跨用户确认；
// 2. 对已应用且命中同一幂等键的请求直接返回历史结果，避免重复写日程；
// 3. 转换 candidate/edited_changes 为 apply 请求；
// 4. 先把 preview 标记 applying，再调用正式 apply adapter；
// 5. 成功或失败都回写 preview，保证接口返回后可排障。
func (s *PreviewConfirmService) ConfirmPreview(ctx context.Context, req activeapply.ConfirmRequest) (*activeapply.ConfirmResult, error) {
	if s == nil || s.activeDAO == nil || s.applyAdapter == nil {
		return nil, errors.New("preview confirm service 未初始化")
	}
	now := s.now()
	if req.RequestedAt.IsZero() {
		req.RequestedAt = now
	}
	previewRow, err := s.activeDAO.GetPreviewByID(ctx, req.PreviewID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, activeapply.NewApplyError(activeapply.ErrorCodeTargetNotFound, "预览不存在或已被删除", err)
		}
		return nil, err
	}
	if previewRow.UserID != req.UserID {
		return nil, activeapply.NewApplyError(activeapply.ErrorCodeForbidden, "预览不属于当前用户", nil)
	}
	if previewRow.ApplyStatus == model.ActiveScheduleApplyStatusApplied {
		if previewRow.ApplyIdempotencyKey == req.IdempotencyKey {
			return alreadyAppliedResult(*previewRow), nil
		}
		return nil, activeapply.NewApplyError(activeapply.ErrorCodeAlreadyApplied, "预览已经应用，不能使用新的幂等键重复确认", nil)
	}

	applyReq, err := activeapply.ConvertConfirmToApplyRequest(*previewRow, req, now)
	if err != nil {
		_ = s.markApplyFailed(ctx, previewRow.ID, "", err)
		return nil, err
	}
	if len(applyReq.Commands) == 0 {
		return s.markNoopApplied(ctx, *applyReq)
	}
	if err = s.markApplying(ctx, *applyReq); err != nil {
		return nil, err
	}

	adapterReq := toAdapterRequest(*applyReq)
	adapterResult, err := s.applyAdapter.ApplyActiveScheduleChanges(ctx, adapterReq)
	if err != nil {
		classifiedErr := classifyAdapterApplyError(err)
		_ = s.markApplyFailed(ctx, previewRow.ID, applyReq.ApplyID, classifiedErr)
		return nil, classifiedErr
	}

	result := activeapply.ApplyActiveScheduleResult{
		ApplyID:              applyReq.ApplyID,
		ApplyStatus:          activeapply.ApplyStatusApplied,
		AppliedEventIDs:      adapterResult.AppliedEventIDs,
		AppliedScheduleIDs:   adapterResult.AppliedScheduleIDs,
		AppliedChanges:       applyReq.Changes,
		SkippedChanges:       applyReq.SkippedChanges,
		RequestHash:          applyReq.RequestHash,
		NormalizedChangeHash: applyReq.NormalizedChangesHash,
	}
	if err = s.markApplied(ctx, *applyReq, result); err != nil {
		return nil, err
	}
	return &activeapply.ConfirmResult{
		PreviewID:       applyReq.PreviewID,
		ApplyID:         applyReq.ApplyID,
		ApplyStatus:     activeapply.ApplyStatusApplied,
		CandidateID:     applyReq.CandidateID,
		RequestHash:     applyReq.RequestHash,
		RequestBodyHash: applyReq.RequestBodyHash,
		ApplyRequest:    applyReq,
		ApplyResult:     &result,
		SkippedChanges:  applyReq.SkippedChanges,
	}, nil
}

func (s *PreviewConfirmService) markApplying(ctx context.Context, req activeapply.ApplyActiveScheduleRequest) error {
	return s.activeDAO.UpdatePreviewFields(ctx, req.PreviewID, map[string]any{
		"apply_id":              req.ApplyID,
		"apply_status":          model.ActiveScheduleApplyStatusApplying,
		"apply_candidate_id":    req.CandidateID,
		"apply_idempotency_key": req.IdempotencyKey,
		"apply_request_hash":    req.RequestHash,
	})
}

// markNoopApplied 处理 notify_only / ask_user / close 这类“确认成功但不写正式日程”的候选。
//
// 职责边界：
// 1. 只把 preview 标记为已处理，并保留幂等字段，便于同 key 重试直接命中历史结果；
// 2. 不调用 apply adapter，因为这些 change 在转换阶段已经被归类为 skipped_changes；
// 3. 失败时直接返回数据库错误，调用方应按系统错误处理，避免前端误以为确认成功。
func (s *PreviewConfirmService) markNoopApplied(ctx context.Context, req activeapply.ApplyActiveScheduleRequest) (*activeapply.ConfirmResult, error) {
	result := activeapply.ApplyActiveScheduleResult{
		ApplyID:              req.ApplyID,
		ApplyStatus:          activeapply.ApplyStatusApplied,
		AppliedChanges:       []activeapply.ApplyChange{},
		SkippedChanges:       req.SkippedChanges,
		RequestHash:          req.RequestHash,
		NormalizedChangeHash: req.NormalizedChangesHash,
	}
	if err := s.markApplied(ctx, req, result); err != nil {
		return nil, err
	}
	return &activeapply.ConfirmResult{
		PreviewID:       req.PreviewID,
		ApplyID:         req.ApplyID,
		ApplyStatus:     activeapply.ApplyStatusApplied,
		CandidateID:     req.CandidateID,
		RequestHash:     req.RequestHash,
		RequestBodyHash: req.RequestBodyHash,
		ApplyRequest:    &req,
		ApplyResult:     &result,
		SkippedChanges:  req.SkippedChanges,
	}, nil
}

func (s *PreviewConfirmService) markApplied(ctx context.Context, req activeapply.ApplyActiveScheduleRequest, result activeapply.ApplyActiveScheduleResult) error {
	now := s.now()
	appliedChangesJSON := mustJSON(result.AppliedChanges)
	appliedEventIDsJSON := mustJSON(result.AppliedEventIDs)
	return s.activeDAO.UpdatePreviewFields(ctx, req.PreviewID, map[string]any{
		"status":                 model.ActiveSchedulePreviewStatusApplied,
		"apply_id":               req.ApplyID,
		"apply_status":           model.ActiveScheduleApplyStatusApplied,
		"apply_candidate_id":     req.CandidateID,
		"apply_idempotency_key":  req.IdempotencyKey,
		"apply_request_hash":     req.RequestHash,
		"applied_changes_json":   &appliedChangesJSON,
		"applied_event_ids_json": &appliedEventIDsJSON,
		"apply_error":            nil,
		"applied_at":             &now,
	})
}

func (s *PreviewConfirmService) markApplyFailed(ctx context.Context, previewID string, applyID string, err error) error {
	if previewID == "" {
		return nil
	}
	message := ""
	if err != nil {
		message = err.Error()
	}
	status := model.ActiveScheduleApplyStatusFailed
	if applyErr, ok := activeapply.AsApplyError(err); ok {
		switch applyErr.Code {
		case activeapply.ErrorCodeExpired:
			status = model.ActiveScheduleApplyStatusExpired
		case activeapply.ErrorCodeDBError:
			status = model.ActiveScheduleApplyStatusFailed
		default:
			status = model.ActiveScheduleApplyStatusRejected
		}
	}
	updates := map[string]any{
		"apply_status": status,
		"apply_error":  &message,
	}
	if applyID != "" {
		updates["apply_id"] = applyID
	}
	return s.activeDAO.UpdatePreviewFields(ctx, previewID, updates)
}

// classifyAdapterApplyError 把正式写库 adapter 的错误转换为 confirm 层统一错误码。
//
// 职责边界：
// 1. 只处理 applyadapter 已声明的业务错误码，保持 API 层只理解 active_scheduler/apply 包；
// 2. 未知错误统一归为 db_error，避免把真实系统故障错误映射为用户可修正的 4xx；
// 3. 原始错误作为 cause 保留，日志和 apply_error 仍能追到 adapter 返回的完整信息。
func classifyAdapterApplyError(err error) error {
	if err == nil {
		return nil
	}
	var adapterErr *applyadapter.ApplyError
	if !errors.As(err, &adapterErr) {
		return activeapply.NewApplyError(activeapply.ErrorCodeDBError, "主动调度正式写库失败", err)
	}
	switch adapterErr.Code {
	case applyadapter.ErrorCodeInvalidRequest:
		return activeapply.NewApplyError(activeapply.ErrorCodeInvalidRequest, adapterErr.Message, err)
	case applyadapter.ErrorCodeUnsupportedChangeType:
		return activeapply.NewApplyError(activeapply.ErrorCodeUnsupportedChangeType, adapterErr.Message, err)
	case applyadapter.ErrorCodeTargetNotFound:
		return activeapply.NewApplyError(activeapply.ErrorCodeTargetNotFound, adapterErr.Message, err)
	case applyadapter.ErrorCodeTargetCompleted:
		return activeapply.NewApplyError(activeapply.ErrorCodeTargetCompleted, adapterErr.Message, err)
	case applyadapter.ErrorCodeTargetAlreadyScheduled:
		return activeapply.NewApplyError(activeapply.ErrorCodeTargetAlreadySchedule, adapterErr.Message, err)
	case applyadapter.ErrorCodeSlotConflict:
		return activeapply.NewApplyError(activeapply.ErrorCodeSlotConflict, adapterErr.Message, err)
	case applyadapter.ErrorCodeInvalidEditedChanges:
		return activeapply.NewApplyError(activeapply.ErrorCodeInvalidEditedChanges, adapterErr.Message, err)
	default:
		return activeapply.NewApplyError(activeapply.ErrorCodeDBError, adapterErr.Message, err)
	}
}

func (s *PreviewConfirmService) now() time.Time {
	if s == nil || s.clock == nil {
		return time.Now()
	}
	return s.clock()
}

func toAdapterRequest(req activeapply.ApplyActiveScheduleRequest) applyadapter.ApplyActiveScheduleRequest {
	changes := make([]applyadapter.ApplyChange, 0, len(req.Changes))
	for _, change := range req.Changes {
		changes = append(changes, toAdapterChange(change))
	}
	return applyadapter.ApplyActiveScheduleRequest{
		PreviewID:   req.PreviewID,
		ApplyID:     req.ApplyID,
		UserID:      req.UserID,
		CandidateID: req.CandidateID,
		Changes:     changes,
		RequestedAt: req.RequestedAt,
		TraceID:     req.TraceID,
	}
}

func toAdapterChange(change activeapply.ApplyChange) applyadapter.ApplyChange {
	return applyadapter.ApplyChange{
		ChangeID:         change.ChangeID,
		ChangeType:       string(change.Type),
		TargetType:       change.TargetType,
		TargetID:         change.TargetID,
		ToSlot:           toAdapterSlotSpan(change),
		DurationSections: change.DurationSections,
		Metadata:         cloneStringMap(change.Metadata),
	}
}

func toAdapterSlotSpan(change activeapply.ApplyChange) *applyadapter.SlotSpan {
	if len(change.Slots) == 0 {
		return nil
	}
	start := change.Slots[0]
	end := change.Slots[len(change.Slots)-1]
	return &applyadapter.SlotSpan{
		Start:            applyadapter.Slot{Week: start.Week, DayOfWeek: start.DayOfWeek, Section: start.Section},
		End:              applyadapter.Slot{Week: end.Week, DayOfWeek: end.DayOfWeek, Section: end.Section},
		DurationSections: len(change.Slots),
	}
}

func alreadyAppliedResult(preview model.ActiveSchedulePreview) *activeapply.ConfirmResult {
	appliedEventIDs := []int{}
	if preview.AppliedEventIDsJSON != nil && *preview.AppliedEventIDsJSON != "" {
		_ = json.Unmarshal([]byte(*preview.AppliedEventIDsJSON), &appliedEventIDs)
	}
	return &activeapply.ConfirmResult{
		PreviewID:   preview.ID,
		ApplyID:     stringValue(preview.ApplyID),
		ApplyStatus: activeapply.ApplyStatusApplied,
		CandidateID: preview.ApplyCandidateID,
		RequestHash: preview.ApplyRequestHash,
		ApplyResult: &activeapply.ApplyActiveScheduleResult{
			ApplyID:         stringValue(preview.ApplyID),
			ApplyStatus:     activeapply.ApplyStatusApplied,
			AppliedEventIDs: appliedEventIDs,
			RequestHash:     preview.ApplyRequestHash,
		},
	}
}

func mustJSON(value any) string {
	raw, err := json.Marshal(value)
	if err != nil {
		return "null"
	}
	return string(raw)
}

func stringValue(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func cloneStringMap(input map[string]string) map[string]string {
	if len(input) == 0 {
		return nil
	}
	output := make(map[string]string, len(input))
	for key, value := range input {
		output[key] = value
	}
	return output
}
