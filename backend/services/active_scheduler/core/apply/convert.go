package apply

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/LoveLosita/smartflow/backend/model"
)

const (
	rawChangeTypeAdd  ChangeType = "add"
	rawChangeTypeNone ChangeType = "none"
)

type candidateSnapshot struct {
	CandidateID   string
	CandidateType ChangeType
	Changes       []ApplyChange
}

// ConvertConfirmToApplyRequest 把 preview 中的候选和 confirm 请求转换为正式 apply 请求。
//
// 职责边界：
// 1. 只读取调用方传入的 preview 快照，不直接访问数据库；
// 2. 负责候选定位、edited_changes 覆盖、范围校验、幂等摘要和 ApplyCommand 生成；
// 3. 不写 schedules，也不执行 task/schedule 当前真值重校验，后者由 ScheduleApplyPort 完成。
func ConvertConfirmToApplyRequest(preview model.ActiveSchedulePreview, req ConfirmRequest, now time.Time) (*ApplyActiveScheduleRequest, error) {
	if req.PreviewID == "" {
		req.PreviewID = preview.ID
	}
	if req.Action == "" {
		req.Action = ConfirmActionConfirm
	}
	if req.RequestedAt.IsZero() {
		req.RequestedAt = now
	}
	if req.UserID <= 0 {
		return nil, newApplyError(ErrorCodeInvalidRequest, "user_id 必须由接入层填入", nil)
	}
	if strings.TrimSpace(req.CandidateID) == "" {
		return nil, newApplyError(ErrorCodeInvalidRequest, "candidate_id 不能为空", nil)
	}
	if req.Action != ConfirmActionConfirm {
		return nil, newApplyError(ErrorCodeInvalidRequest, "当前只支持 confirm 动作", nil)
	}
	if err := ValidatePreviewConfirmable(preview, req.UserID, now); err != nil {
		return nil, err
	}

	requestHash, err := BuildConfirmRequestHash(preview.ID, req)
	if err != nil {
		return nil, err
	}
	if DetectIdempotencyConflict(preview, requestHash.ApplyHash, req.IdempotencyKey) {
		return nil, newApplyError(ErrorCodeIdempotencyConflict, "同一个幂等键已绑定不同确认内容", nil)
	}

	candidate, err := FindCandidateInPreview(preview, req.CandidateID)
	if err != nil {
		return nil, err
	}

	originalChanges, err := NormalizeChanges(candidate.Changes, candidate.CandidateType)
	if err != nil {
		return nil, err
	}

	changes := originalChanges
	if len(req.EditedChanges) > 0 {
		editedChanges, normalizeErr := NormalizeChanges(req.EditedChanges, candidate.CandidateType)
		if normalizeErr != nil {
			return nil, normalizeErr
		}
		if validateErr := ValidateChangeScope(originalChanges, editedChanges); validateErr != nil {
			return nil, validateErr
		}
		changes = editedChanges
	}

	normalizedHash, err := BuildNormalizedChangesHash(changes)
	if err != nil {
		return nil, err
	}
	commands, skipped, err := ConvertChangesToCommands(changes)
	if err != nil {
		return nil, err
	}

	return &ApplyActiveScheduleRequest{
		PreviewID:             preview.ID,
		ApplyID:               requestHash.ApplyID,
		IdempotencyKey:        strings.TrimSpace(req.IdempotencyKey),
		RequestHash:           requestHash.ApplyHash,
		RequestBodyHash:       requestHash.BodyHash,
		UserID:                req.UserID,
		CandidateID:           req.CandidateID,
		BaseVersion:           preview.BaseVersion,
		Changes:               changes,
		Commands:              commands,
		SkippedChanges:        skipped,
		NormalizedChangesHash: normalizedHash,
		RequestedAt:           req.RequestedAt,
		TraceID:               req.TraceID,
	}, nil
}

// FindCandidateInPreview 从 selected_candidate_json 或 candidates_json 中定位 confirm 指定候选。
//
// 职责边界：
// 1. 优先使用 selected_candidate_json，只有候选 ID 不匹配时才回退 candidates_json；
// 2. 兼容 Go 结构体默认 JSON 字段名和前端常用 snake_case 字段；
// 3. 只返回候选快照，不判断候选是否仍可落库。
func FindCandidateInPreview(preview model.ActiveSchedulePreview, candidateID string) (candidateSnapshot, error) {
	candidateID = strings.TrimSpace(candidateID)
	if candidateID == "" {
		return candidateSnapshot{}, newApplyError(ErrorCodeInvalidRequest, "candidate_id 不能为空", nil)
	}

	if preview.SelectedCandidateJSON != nil && strings.TrimSpace(*preview.SelectedCandidateJSON) != "" {
		selected, err := parseCandidateSnapshot([]byte(*preview.SelectedCandidateJSON))
		if err != nil {
			return candidateSnapshot{}, err
		}
		if selected.CandidateID == candidateID {
			return selected, nil
		}
	}

	candidates, err := parseCandidateList(preview.CandidatesJSON)
	if err != nil {
		return candidateSnapshot{}, err
	}
	for _, item := range candidates {
		if item.CandidateID == candidateID {
			return item, nil
		}
	}
	return candidateSnapshot{}, newApplyError(ErrorCodeTargetNotFound, "confirm 指定的 candidate_id 不属于当前 preview", nil)
}

// NormalizeChanges 对候选或用户编辑后的 changes 做最小规范化。
//
// 职责边界：
// 1. 填充 change_type、target、duration 和 slots 等缺省字段；
// 2. 合并同一目标的连续节次，降低后续 port 写入复杂度；
// 3. 不做数据库事实校验，不判断冲突。
func NormalizeChanges(changes []ApplyChange, candidateType ChangeType) ([]ApplyChange, error) {
	if len(changes) == 0 && isNoopChangeType(candidateType) {
		changes = []ApplyChange{{Type: candidateType}}
	}
	if len(changes) == 0 {
		return nil, newApplyError(ErrorCodeInvalidEditedChanges, "候选没有可转换的 changes", nil)
	}

	normalized := make([]ApplyChange, 0, len(changes))
	for _, change := range changes {
		item, err := normalizeChange(change, candidateType)
		if err != nil {
			return nil, err
		}
		normalized = append(normalized, item)
	}

	sort.SliceStable(normalized, func(i, j int) bool {
		return changeSortKey(normalized[i]) < changeSortKey(normalized[j])
	})
	merged := mergeContinuousChanges(normalized)
	for i := range merged {
		hash, err := hashJSON(changeForHash(merged[i]))
		if err != nil {
			return nil, err
		}
		merged[i].NormalizedHash = hash
	}
	return merged, nil
}

// ValidateChangeScope 校验 edited_changes 没有新增候选外目标。
//
// 职责边界：
// 1. 只比较候选原始 changes 与用户编辑后的 changes；
// 2. 允许 EditedAllowed=true 的 change 改时间坐标，但不允许改 target/type；
// 3. 更细的冲突、课程覆盖、base_version 重校验仍交给 apply port。
func ValidateChangeScope(original []ApplyChange, edited []ApplyChange) error {
	allowed := make(map[string]ApplyChange, len(original))
	for _, change := range original {
		allowed[changeScopeKey(change)] = change
	}
	seen := make(map[string]struct{}, len(edited))
	for _, change := range edited {
		key := changeScopeKey(change)
		base, ok := allowed[key]
		if !ok {
			return newApplyError(ErrorCodeInvalidEditedChanges, "edited_changes 包含候选外目标或变更类型", nil)
		}
		if _, exists := seen[key]; exists {
			return newApplyError(ErrorCodeInvalidEditedChanges, "edited_changes 存在重复目标", nil)
		}
		seen[key] = struct{}{}
		if !base.EditedAllowed && !sameChangeForScope(base, change) {
			return newApplyError(ErrorCodeInvalidEditedChanges, "该 change 不允许用户编辑", nil)
		}
	}
	return nil
}

// ConvertChangesToCommands 把规范化后的 changes 转成正式写入命令。
//
// 职责边界：
// 1. MVP 只生成 task_pool 新增和补做块新增两类命令；
// 2. ask_user/notify_only/close 只返回 skipped_changes，不写正式日程；
// 3. compress_with_next_dynamic_task 明确拒绝，避免 confirm 后出现不可应用候选。
func ConvertChangesToCommands(changes []ApplyChange) ([]ApplyCommand, []SkippedChange, error) {
	commands := make([]ApplyCommand, 0, len(changes))
	skipped := make([]SkippedChange, 0)
	for _, change := range changes {
		switch change.Type {
		case ChangeTypeAddTaskPoolToSchedule:
			if change.TargetID <= 0 {
				return nil, nil, newApplyError(ErrorCodeInvalidEditedChanges, "task_pool change 缺少 task_id/target_id", nil)
			}
			commands = append(commands, ApplyCommand{
				CommandType: CommandTypeInsertTaskPoolEvent,
				ChangeID:    change.ChangeID,
				ChangeType:  change.Type,
				TargetType:  firstNonEmpty(change.TargetType, "task_pool"),
				TargetID:    change.TargetID,
				Slots:       slotsFromChange(change),
				Metadata:    cloneMetadata(change.Metadata),
			})
		case ChangeTypeCreateMakeup:
			sourceEventID := firstPositive(change.SourceEventID, change.MakeupForEventID, change.EventID, change.TargetID)
			if sourceEventID <= 0 {
				return nil, nil, newApplyError(ErrorCodeInvalidEditedChanges, "create_makeup 缺少原 schedule_event id", nil)
			}
			commands = append(commands, ApplyCommand{
				CommandType:   CommandTypeInsertMakeupEvent,
				ChangeID:      change.ChangeID,
				ChangeType:    change.Type,
				TargetType:    firstNonEmpty(change.TargetType, "schedule_event"),
				TargetID:      firstPositive(change.TargetID, sourceEventID),
				Slots:         slotsFromChange(change),
				SourceEventID: sourceEventID,
				Metadata:      cloneMetadata(change.Metadata),
			})
		case ChangeTypeAskUser, ChangeTypeNotifyOnly, ChangeTypeClose:
			skipped = append(skipped, SkippedChange{
				ChangeID:   change.ChangeID,
				ChangeType: change.Type,
				Reason:     "该候选只更新交互状态或通知结果，不写正式日程",
			})
		case ChangeTypeCompressWithNextDynamicTask:
			return nil, nil, newApplyError(ErrorCodeUnsupportedChangeType, "MVP 明确关闭压缩融合 apply", nil)
		default:
			return nil, nil, newApplyError(ErrorCodeUnsupportedChangeType, fmt.Sprintf("不支持的 change_type: %s", change.Type), nil)
		}
	}
	return commands, skipped, nil
}
