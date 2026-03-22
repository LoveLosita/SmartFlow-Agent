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

// UpsertScheduleStateSnapshot 以“user_id + conversation_id”维度写入/覆盖排程状态快照。
//
// 职责边界：
// 1. 负责把强类型快照序列化并持久化到 agent_schedule_states；
// 2. 负责 upsert 冲突更新（同会话覆盖），并自动 revision+1；
// 3. 不负责 Redis 缓存读写，不负责业务分流，不负责正式日程落库。
//
// 步骤化说明：
// 1. 先做参数与主键语义校验，避免把脏快照写入数据库；
// 2. 再把切片字段统一序列化为 JSON，保证表内口径稳定；
// 3. 最后执行 OnConflict upsert：
// 3.1 新记录直接插入；
// 3.2 已存在记录则覆盖业务字段，并把 revision 自增；
// 3.3 任一阶段失败都返回 error，由上层决定是否降级。
func (a *AgentDAO) UpsertScheduleStateSnapshot(ctx context.Context, snapshot *model.SchedulePlanStateSnapshot) error {
	if a == nil || a.db == nil {
		return errors.New("agent dao is not initialized")
	}
	if snapshot == nil {
		return errors.New("schedule state snapshot is nil")
	}
	if snapshot.UserID <= 0 {
		return fmt.Errorf("invalid snapshot user_id: %d", snapshot.UserID)
	}
	conversationID := strings.TrimSpace(snapshot.ConversationID)
	if conversationID == "" {
		return errors.New("schedule state snapshot conversation_id is empty")
	}

	taskClassIDsJSON, err := marshalJSONOrDefault(snapshot.TaskClassIDs, "[]")
	if err != nil {
		return fmt.Errorf("marshal task_class_ids failed: %w", err)
	}
	constraintsJSON, err := marshalJSONOrDefault(snapshot.Constraints, "[]")
	if err != nil {
		return fmt.Errorf("marshal constraints failed: %w", err)
	}
	hybridEntriesJSON, err := marshalJSONOrDefault(snapshot.HybridEntries, "[]")
	if err != nil {
		return fmt.Errorf("marshal hybrid_entries failed: %w", err)
	}
	allocatedItemsJSON, err := marshalJSONOrDefault(snapshot.AllocatedItems, "[]")
	if err != nil {
		return fmt.Errorf("marshal allocated_items failed: %w", err)
	}
	candidatePlansJSON, err := marshalJSONOrDefault(snapshot.CandidatePlans, "[]")
	if err != nil {
		return fmt.Errorf("marshal candidate_plans failed: %w", err)
	}

	stateVersion := snapshot.StateVersion
	if stateVersion <= 0 {
		stateVersion = model.SchedulePlanStateVersionV1
	}
	revision := snapshot.Revision
	if revision <= 0 {
		revision = 1
	}

	row := model.AgentScheduleState{
		UserID:             snapshot.UserID,
		ConversationID:     conversationID,
		Revision:           revision,
		StateVersion:       stateVersion,
		TaskClassIDsJSON:   taskClassIDsJSON,
		ConstraintsJSON:    constraintsJSON,
		HybridEntriesJSON:  hybridEntriesJSON,
		AllocatedItemsJSON: allocatedItemsJSON,
		CandidatePlansJSON: candidatePlansJSON,
		UserIntent:         strings.TrimSpace(snapshot.UserIntent),
		Strategy:           normalizeStrategy(snapshot.Strategy),
		AdjustmentScope:    normalizeAdjustmentScope(snapshot.AdjustmentScope),
		RestartRequested:   snapshot.RestartRequested,
		FinalSummary:       strings.TrimSpace(snapshot.FinalSummary),
		Completed:          snapshot.Completed,
		TraceID:            strings.TrimSpace(snapshot.TraceID),
	}

	now := time.Now()
	return a.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns: []clause.Column{
			{Name: "user_id"},
			{Name: "conversation_id"},
		},
		DoUpdates: clause.Assignments(map[string]any{
			"revision":          gorm.Expr("revision + 1"),
			"state_version":     row.StateVersion,
			"task_class_ids":    row.TaskClassIDsJSON,
			"constraints":       row.ConstraintsJSON,
			"hybrid_entries":    row.HybridEntriesJSON,
			"allocated_items":   row.AllocatedItemsJSON,
			"candidate_plans":   row.CandidatePlansJSON,
			"user_intent":       row.UserIntent,
			"strategy":          row.Strategy,
			"adjustment_scope":  row.AdjustmentScope,
			"restart_requested": row.RestartRequested,
			"final_summary":     row.FinalSummary,
			"completed":         row.Completed,
			"trace_id":          row.TraceID,
			"updated_at":        now,
		}),
	}).Create(&row).Error
}

// GetScheduleStateSnapshot 读取指定会话的排程状态快照。
//
// 职责边界：
// 1. 负责按 user_id + conversation_id 查询快照；
// 2. 负责把数据库 JSON 字段反序列化回强类型结构；
// 3. 不负责回填 Redis，不负责业务分流判定。
//
// 返回语义：
// 1. 命中：返回 snapshot, nil；
// 2. 未命中：返回 nil, nil（上层可继续走其他兜底）；
// 3. 反序列化失败：返回 error（说明库内数据不合法，需要排障）。
func (a *AgentDAO) GetScheduleStateSnapshot(ctx context.Context, userID int, conversationID string) (*model.SchedulePlanStateSnapshot, error) {
	if a == nil || a.db == nil {
		return nil, errors.New("agent dao is not initialized")
	}
	if userID <= 0 {
		return nil, fmt.Errorf("invalid user_id: %d", userID)
	}
	normalizedConversationID := strings.TrimSpace(conversationID)
	if normalizedConversationID == "" {
		return nil, errors.New("conversation_id is empty")
	}

	var row model.AgentScheduleState
	err := a.db.WithContext(ctx).
		Where("user_id = ? AND conversation_id = ?", userID, normalizedConversationID).
		First(&row).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}

	taskClassIDs := make([]int, 0)
	if err = unmarshalJSONOrDefault(row.TaskClassIDsJSON, &taskClassIDs, []int{}); err != nil {
		return nil, fmt.Errorf("unmarshal task_class_ids failed: %w", err)
	}
	constraints := make([]string, 0)
	if err = unmarshalJSONOrDefault(row.ConstraintsJSON, &constraints, []string{}); err != nil {
		return nil, fmt.Errorf("unmarshal constraints failed: %w", err)
	}
	hybridEntries := make([]model.HybridScheduleEntry, 0)
	if err = unmarshalJSONOrDefault(row.HybridEntriesJSON, &hybridEntries, []model.HybridScheduleEntry{}); err != nil {
		return nil, fmt.Errorf("unmarshal hybrid_entries failed: %w", err)
	}
	allocatedItems := make([]model.TaskClassItem, 0)
	if err = unmarshalJSONOrDefault(row.AllocatedItemsJSON, &allocatedItems, []model.TaskClassItem{}); err != nil {
		return nil, fmt.Errorf("unmarshal allocated_items failed: %w", err)
	}
	candidatePlans := make([]model.UserWeekSchedule, 0)
	if err = unmarshalJSONOrDefault(row.CandidatePlansJSON, &candidatePlans, []model.UserWeekSchedule{}); err != nil {
		return nil, fmt.Errorf("unmarshal candidate_plans failed: %w", err)
	}

	return &model.SchedulePlanStateSnapshot{
		UserID:           row.UserID,
		ConversationID:   row.ConversationID,
		Revision:         row.Revision,
		StateVersion:     row.StateVersion,
		TaskClassIDs:     taskClassIDs,
		Constraints:      constraints,
		HybridEntries:    hybridEntries,
		AllocatedItems:   allocatedItems,
		CandidatePlans:   candidatePlans,
		UserIntent:       row.UserIntent,
		Strategy:         normalizeStrategy(row.Strategy),
		AdjustmentScope:  normalizeAdjustmentScope(row.AdjustmentScope),
		RestartRequested: row.RestartRequested,
		FinalSummary:     row.FinalSummary,
		Completed:        row.Completed,
		TraceID:          row.TraceID,
		UpdatedAt:        row.UpdatedAt,
	}, nil
}

// marshalJSONOrDefault 统一处理“结构体 -> JSON 字符串”序列化。
//
// 设计目的：
// 1. 避免每个字段手写重复的 marshal 判空逻辑；
// 2. nil 场景统一写成默认 JSON（例如 []）以保持数据库口径稳定；
// 3. 序列化失败直接上抛，防止写入半成品快照。
func marshalJSONOrDefault(v any, defaultJSON string) (string, error) {
	if v == nil {
		return defaultJSON, nil
	}
	raw, err := json.Marshal(v)
	if err != nil {
		return "", err
	}
	text := strings.TrimSpace(string(raw))
	if text == "" || text == "null" {
		return defaultJSON, nil
	}
	return text, nil
}

// unmarshalJSONOrDefault 统一处理“JSON 字符串 -> 结构体”反序列化。
//
// 设计目的：
// 1. 数据为空、null 时回落到默认值，避免上层到处判空；
// 2. 保留错误上抛，便于定位历史脏数据；
// 3. 保障读取到的快照字段始终有确定值语义。
func unmarshalJSONOrDefault[T any](raw string, target *T, defaultValue T) error {
	clean := strings.TrimSpace(raw)
	if clean == "" || clean == "null" {
		*target = defaultValue
		return nil
	}
	return json.Unmarshal([]byte(clean), target)
}

// normalizeStrategy 归一化快照中的 strategy 字段。
func normalizeStrategy(raw string) string {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "rapid":
		return "rapid"
	default:
		return "steady"
	}
}

// normalizeAdjustmentScope 归一化快照中的微调力度字段。
func normalizeAdjustmentScope(raw string) string {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "small":
		return "small"
	case "medium":
		return "medium"
	default:
		return "large"
	}
}
