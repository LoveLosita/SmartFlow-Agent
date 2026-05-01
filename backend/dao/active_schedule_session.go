package dao

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/LoveLosita/smartflow/backend/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var activeScheduleSessionLiveStatuses = []string{
	model.ActiveScheduleSessionStatusWaitingUserReply,
	model.ActiveScheduleSessionStatusRerunning,
}

// ActiveScheduleSessionDAO 负责主动调度会话的数据库读写。
//
// 职责边界：
// 1. 只管 session 表本身，不管聊天入口拦截策略；
// 2. 只提供按 session_id / conversation_id 的读写能力，不编排 graph；
// 3. cache 命中策略由上层决定，这里始终把 MySQL 当作最终真相。
type ActiveScheduleSessionDAO struct {
	db *gorm.DB
}

// NewActiveScheduleSessionDAO 创建主动调度会话 DAO。
func NewActiveScheduleSessionDAO(db *gorm.DB) *ActiveScheduleSessionDAO {
	return &ActiveScheduleSessionDAO{db: db}
}

// WithTx 基于外部事务句柄构造同事务 DAO。
func (d *ActiveScheduleSessionDAO) WithTx(tx *gorm.DB) *ActiveScheduleSessionDAO {
	return &ActiveScheduleSessionDAO{db: tx}
}

func (d *ActiveScheduleSessionDAO) ensureDB() error {
	if d == nil || d.db == nil {
		return errors.New("active schedule session dao 未初始化")
	}
	return nil
}

// UpsertActiveScheduleSession 按 session_id 幂等写入或覆盖主动调度会话。
//
// 步骤化说明：
// 1. 先校验主键、归属用户和状态，避免把脏会话写进数据表；
// 2. 再把轻量 state 统一序列化为 state_json，保证数据库侧格式稳定；
// 3. 最后走 OnConflict upsert，保留 created_at，仅刷新业务字段和 updated_at。
func (d *ActiveScheduleSessionDAO) UpsertActiveScheduleSession(ctx context.Context, snapshot *model.ActiveScheduleSessionSnapshot) error {
	if err := d.ensureDB(); err != nil {
		return err
	}

	normalized, err := normalizeActiveScheduleSessionSnapshot(snapshot)
	if err != nil {
		return err
	}

	stateJSON, err := marshalActiveScheduleSessionState(normalized.State)
	if err != nil {
		return fmt.Errorf("marshal active schedule session state failed: %w", err)
	}

	now := time.Now()
	row := model.ActiveScheduleSession{
		SessionID:        normalized.SessionID,
		UserID:           normalized.UserID,
		ConversationID:   nullableStringPtr(normalized.ConversationID),
		TriggerID:        normalized.TriggerID,
		CurrentPreviewID: nullableStringPtr(normalized.CurrentPreviewID),
		Status:           normalized.Status,
		StateJSON:        stateJSON,
		CreatedAt:        normalized.CreatedAt,
		UpdatedAt:        now,
	}
	if row.CreatedAt.IsZero() {
		row.CreatedAt = now
	}

	return d.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns: []clause.Column{
			{Name: "session_id"},
		},
		DoUpdates: clause.Assignments(map[string]any{
			"user_id":            row.UserID,
			"conversation_id":    row.ConversationID,
			"trigger_id":         row.TriggerID,
			"current_preview_id": row.CurrentPreviewID,
			"status":             row.Status,
			"state_json":         row.StateJSON,
			"updated_at":         row.UpdatedAt,
		}),
	}).Create(&row).Error
}

// GetActiveScheduleSessionBySessionID 按 session_id 读取任意状态的会话记录。
//
// 返回语义：
// 1. 命中：返回 snapshot, nil；
// 2. 未命中：返回 nil, nil，交给上层判断是否需要走回源或新建；
// 3. 数据损坏：返回 error，避免把坏状态继续传给拦截逻辑。
func (d *ActiveScheduleSessionDAO) GetActiveScheduleSessionBySessionID(ctx context.Context, sessionID string) (*model.ActiveScheduleSessionSnapshot, error) {
	if err := d.ensureDB(); err != nil {
		return nil, err
	}

	normalizedSessionID := strings.TrimSpace(sessionID)
	if normalizedSessionID == "" {
		return nil, errors.New("session_id is empty")
	}

	var row model.ActiveScheduleSession
	err := d.db.WithContext(ctx).
		Where("session_id = ?", normalizedSessionID).
		First(&row).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}

	return activeScheduleSessionSnapshotFromRow(&row)
}

// GetActiveScheduleSessionByConversationID 按 user_id + conversation_id 读取最新的会话记录。
//
// 职责边界：
// 1. 始终返回同一 conversation 最新的一条记录，方便上层直接判断当前 status；
// 2. 不在 DAO 内部做“是否拦截”的业务裁决，避免把路由规则写死在存储层；
// 3. 若同一 conversation 误写出多条记录，按最近更新时间优先返回。
func (d *ActiveScheduleSessionDAO) GetActiveScheduleSessionByConversationID(ctx context.Context, userID int, conversationID string) (*model.ActiveScheduleSessionSnapshot, error) {
	if err := d.ensureDB(); err != nil {
		return nil, err
	}
	if userID <= 0 {
		return nil, fmt.Errorf("invalid user_id: %d", userID)
	}

	normalizedConversationID := strings.TrimSpace(conversationID)
	if normalizedConversationID == "" {
		return nil, errors.New("conversation_id is empty")
	}

	var row model.ActiveScheduleSession
	err := d.db.WithContext(ctx).
		Where("user_id = ? AND conversation_id = ?", userID, normalizedConversationID).
		Order("updated_at DESC, created_at DESC, session_id DESC").
		First(&row).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}

	return activeScheduleSessionSnapshotFromRow(&row)
}

// UpdateActiveScheduleSessionFieldsBySessionID 按 session_id 更新局部字段。
//
// 说明：
// 1. 这里不负责 state_json 的序列化，调用方需要自己准备好最终字段值；
// 2. 若 updates 为空，直接返回 nil，避免多余的数据库写入；
// 3. updated_at 会在这里自动刷新，保证时间线可追踪。
func (d *ActiveScheduleSessionDAO) UpdateActiveScheduleSessionFieldsBySessionID(ctx context.Context, sessionID string, updates map[string]any) error {
	if err := d.ensureDB(); err != nil {
		return err
	}

	normalizedSessionID := strings.TrimSpace(sessionID)
	if normalizedSessionID == "" {
		return errors.New("session_id is empty")
	}
	if len(updates) == 0 {
		return nil
	}

	normalizedUpdates := cloneUpdateMap(updates)
	if _, ok := normalizedUpdates["updated_at"]; !ok {
		normalizedUpdates["updated_at"] = time.Now()
	}

	return d.db.WithContext(ctx).
		Model(&model.ActiveScheduleSession{}).
		Where("session_id = ?", normalizedSessionID).
		Updates(normalizedUpdates).Error
}

// UpdateActiveScheduleSessionFieldsByConversationID 按 user_id + conversation_id 更新最新记录的局部字段。
//
// 步骤化说明：
// 1. 先定位同一 conversation 最新的 session，再按 session_id 回写，避免一次 update 覆盖多条历史；
// 2. 再写入局部字段和 updated_at，保证状态变化可以按会话维度回写；
// 3. 找不到任何会话时直接返回，交给上层决定是否要新建 session 或释放普通聊天。
func (d *ActiveScheduleSessionDAO) UpdateActiveScheduleSessionFieldsByConversationID(ctx context.Context, userID int, conversationID string, updates map[string]any) error {
	if err := d.ensureDB(); err != nil {
		return err
	}
	if userID <= 0 {
		return fmt.Errorf("invalid user_id: %d", userID)
	}

	normalizedConversationID := strings.TrimSpace(conversationID)
	if normalizedConversationID == "" {
		return errors.New("conversation_id is empty")
	}
	if len(updates) == 0 {
		return nil
	}

	row, err := d.GetActiveScheduleSessionByConversationID(ctx, userID, normalizedConversationID)
	if err != nil {
		return err
	}
	if row == nil {
		return gorm.ErrRecordNotFound
	}

	normalizedUpdates := cloneUpdateMap(updates)
	if _, ok := normalizedUpdates["updated_at"]; !ok {
		normalizedUpdates["updated_at"] = time.Now()
	}

	return d.db.WithContext(ctx).
		Model(&model.ActiveScheduleSession{}).
		Where("session_id = ?", row.SessionID).
		Updates(normalizedUpdates).Error
}

func normalizeActiveScheduleSessionSnapshot(snapshot *model.ActiveScheduleSessionSnapshot) (*model.ActiveScheduleSessionSnapshot, error) {
	if snapshot == nil {
		return nil, errors.New("active schedule session snapshot is nil")
	}

	normalizedSessionID := strings.TrimSpace(snapshot.SessionID)
	if normalizedSessionID == "" {
		return nil, errors.New("session_id is empty")
	}
	if snapshot.UserID <= 0 {
		return nil, fmt.Errorf("invalid user_id: %d", snapshot.UserID)
	}

	normalizedStatus, err := normalizeActiveScheduleSessionStatus(snapshot.Status)
	if err != nil {
		return nil, err
	}

	normalizedTriggerID := strings.TrimSpace(snapshot.TriggerID)
	if normalizedTriggerID == "" {
		return nil, errors.New("trigger_id is empty")
	}

	normalized := *snapshot
	normalized.SessionID = normalizedSessionID
	normalized.UserID = snapshot.UserID
	normalized.ConversationID = strings.TrimSpace(snapshot.ConversationID)
	normalized.TriggerID = normalizedTriggerID
	normalized.CurrentPreviewID = strings.TrimSpace(snapshot.CurrentPreviewID)
	normalized.Status = normalizedStatus
	normalized.State = normalizeActiveScheduleSessionState(snapshot.State)
	return &normalized, nil
}

func normalizeActiveScheduleSessionStatus(raw string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case model.ActiveScheduleSessionStatusWaitingUserReply:
		return model.ActiveScheduleSessionStatusWaitingUserReply, nil
	case model.ActiveScheduleSessionStatusRerunning:
		return model.ActiveScheduleSessionStatusRerunning, nil
	case model.ActiveScheduleSessionStatusReadyPreview:
		return model.ActiveScheduleSessionStatusReadyPreview, nil
	case model.ActiveScheduleSessionStatusApplied:
		return model.ActiveScheduleSessionStatusApplied, nil
	case model.ActiveScheduleSessionStatusIgnored:
		return model.ActiveScheduleSessionStatusIgnored, nil
	case model.ActiveScheduleSessionStatusExpired:
		return model.ActiveScheduleSessionStatusExpired, nil
	case model.ActiveScheduleSessionStatusFailed:
		return model.ActiveScheduleSessionStatusFailed, nil
	default:
		return "", fmt.Errorf("invalid active schedule session status: %s", raw)
	}
}

func normalizeActiveScheduleSessionState(state model.ActiveScheduleSessionState) model.ActiveScheduleSessionState {
	state.PendingQuestion = strings.TrimSpace(state.PendingQuestion)
	state.LastCandidateID = strings.TrimSpace(state.LastCandidateID)
	state.LastNotificationID = strings.TrimSpace(state.LastNotificationID)
	state.FailedReason = strings.TrimSpace(state.FailedReason)
	if state.ExpiresAt != nil && state.ExpiresAt.IsZero() {
		state.ExpiresAt = nil
	}
	if len(state.MissingInfo) > 0 {
		state.MissingInfo = dedupeAndTrimStrings(state.MissingInfo)
	}
	return state
}

func marshalActiveScheduleSessionState(state model.ActiveScheduleSessionState) (string, error) {
	normalized := normalizeActiveScheduleSessionState(state)
	raw, err := json.Marshal(normalized)
	if err != nil {
		return "", err
	}
	text := strings.TrimSpace(string(raw))
	if text == "" {
		return "{}", nil
	}
	return text, nil
}

func unmarshalActiveScheduleSessionState(raw string) (model.ActiveScheduleSessionState, error) {
	clean := strings.TrimSpace(raw)
	if clean == "" || clean == "null" {
		return model.ActiveScheduleSessionState{}, nil
	}

	var state model.ActiveScheduleSessionState
	if err := json.Unmarshal([]byte(clean), &state); err != nil {
		return model.ActiveScheduleSessionState{}, err
	}
	state = normalizeActiveScheduleSessionState(state)
	return state, nil
}

func activeScheduleSessionSnapshotFromRow(row *model.ActiveScheduleSession) (*model.ActiveScheduleSessionSnapshot, error) {
	if row == nil {
		return nil, errors.New("active schedule session row is nil")
	}

	state, err := unmarshalActiveScheduleSessionState(row.StateJSON)
	if err != nil {
		return nil, fmt.Errorf("unmarshal active schedule session state failed: %w", err)
	}

	return &model.ActiveScheduleSessionSnapshot{
		SessionID:        row.SessionID,
		UserID:           row.UserID,
		ConversationID:   nullableStringValue(row.ConversationID),
		TriggerID:        row.TriggerID,
		CurrentPreviewID: nullableStringValue(row.CurrentPreviewID),
		Status:           row.Status,
		State:            state,
		CreatedAt:        row.CreatedAt,
		UpdatedAt:        row.UpdatedAt,
	}, nil
}

func nullableStringPtr(raw string) *string {
	normalized := strings.TrimSpace(raw)
	if normalized == "" {
		return nil
	}
	return &normalized
}

func nullableStringValue(raw *string) string {
	if raw == nil {
		return ""
	}
	return strings.TrimSpace(*raw)
}

func cloneUpdateMap(updates map[string]any) map[string]any {
	cloned := make(map[string]any, len(updates)+1)
	for key, value := range updates {
		cloned[key] = value
	}
	return cloned
}

func dedupeAndTrimStrings(values []string) []string {
	if len(values) == 0 {
		return nil
	}

	result := make([]string, 0, len(values))
	seen := make(map[string]struct{}, len(values))
	for _, item := range values {
		normalized := strings.TrimSpace(item)
		if normalized == "" {
			continue
		}
		if _, ok := seen[normalized]; ok {
			continue
		}
		seen[normalized] = struct{}{}
		result = append(result, normalized)
	}
	if len(result) == 0 {
		return nil
	}
	return result
}
