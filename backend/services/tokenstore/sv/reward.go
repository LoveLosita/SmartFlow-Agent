package sv

import (
	"context"
	"errors"
	"strconv"
	"strings"

	tokenmodel "github.com/LoveLosita/smartflow/backend/services/tokenstore/model"
	tokencontracts "github.com/LoveLosita/smartflow/backend/shared/contracts/tokenstore"
	"github.com/spf13/viper"
)

const (
	forumLikeRewardConfigKey   = "tokenstore.reward.forumLikeAmount"
	forumImportRewardConfigKey = "tokenstore.reward.forumImportAmount"

	defaultForumLikeRewardAmount   int64 = 1
	defaultForumImportRewardAmount int64 = 5
)

type forumRewardGrantRequest struct {
	EventID        string
	ReceiverUserID uint64
	Source         string
	SourceRefID    uint64
}

type forumRewardDecision struct {
	Amount      int64
	Status      string
	Description string
}

// RecordForumRewardGrant 负责把论坛点赞/导入奖励写入 token_grants。
//
// 职责边界：
// 1. 只处理 forum_like / forum_import 两类奖励账本写入，不修改 users，也不调用 user/auth；
// 2. 以 event_id 作为最终幂等边界，重复请求校验一致后返回既有 grant；
// 3. 奖励金额优先读取 token_reward_rules，配置和代码默认值只作为兜底。
func (s *Service) RecordForumRewardGrant(ctx context.Context, req tokencontracts.RecordForumRewardGrantRequest) (*tokencontracts.TokenGrantView, error) {
	if err := s.Ready(); err != nil {
		return nil, err
	}

	normalized, err := normalizeForumRewardGrantRequest(req)
	if err != nil {
		return nil, err
	}

	// 1. 先按 event_id 回查，命中时直接视为成功，避免 outbox 重试重复写账本。
	// 2. 命中后必须校验用户、来源和来源业务 ID，避免错误复用 event_id 时静默吞掉错账。
	// 3. 校验通过才返回既有 grant，兼容“首次已成功、调用方超时后重试”的常见场景。
	existing, err := s.tokenDAO.FindGrantByEventID(ctx, normalized.EventID)
	if err != nil {
		return nil, err
	}
	if existing != nil {
		if err := validateExistingForumRewardGrant(*existing, normalized); err != nil {
			return nil, err
		}
		view := grantViewFromModel(*existing)
		return &view, nil
	}

	sourceRefID := normalized.SourceRefID
	decision, err := s.forumRewardDecision(ctx, normalized.Source)
	if err != nil {
		return nil, err
	}
	grant := tokenmodel.TokenGrant{
		EventID:      normalized.EventID,
		UserID:       normalized.ReceiverUserID,
		Source:       normalized.Source,
		SourceLabel:  grantSourceLabel(normalized.Source, ""),
		SourceRefID:  &sourceRefID,
		Amount:       decision.Amount,
		Status:       decision.Status,
		QuotaApplied: false,
		Description:  decision.Description,
	}

	// 1. 账本写入只依赖 token_grants.event_id 唯一约束兜底并发幂等。
	// 2. 若并发下插入触发唯一键冲突，立刻回查 event_id，把已有 grant 当作成功结果返回。
	// 3. 只有“冲突后仍查不到旧记录”这种异常态才上抛内部错误，避免吞掉真实一致性问题。
	if err := s.tokenDAO.CreateGrant(ctx, &grant); err != nil {
		if !isDuplicateKeyError(err) {
			return nil, err
		}

		existing, err := s.tokenDAO.FindGrantByEventID(ctx, normalized.EventID)
		if err != nil {
			return nil, err
		}
		if existing == nil {
			return nil, errors.New("forum reward grant duplicated but not found by event_id")
		}
		if err := validateExistingForumRewardGrant(*existing, normalized); err != nil {
			return nil, err
		}
		view := grantViewFromModel(*existing)
		return &view, nil
	}

	view := grantViewFromModel(grant)
	return &view, nil
}

func normalizeForumRewardGrantRequest(req tokencontracts.RecordForumRewardGrantRequest) (forumRewardGrantRequest, error) {
	normalized := forumRewardGrantRequest{
		EventID:        strings.TrimSpace(req.EventID),
		ReceiverUserID: req.ReceiverUserID,
		Source:         strings.ToLower(strings.TrimSpace(req.Source)),
	}

	switch {
	case normalized.EventID == "":
		return forumRewardGrantRequest{}, tokenStoreBadRequest("event_id 不能为空")
	case normalized.ReceiverUserID == 0:
		return forumRewardGrantRequest{}, tokenStoreBadRequest("receiver_user_id 不能为空")
	}

	sourceRefID, err := parseForumRewardSourceRefID(req.SourceRefID)
	if err != nil {
		return forumRewardGrantRequest{}, err
	}
	normalized.SourceRefID = sourceRefID

	switch normalized.Source {
	case tokenmodel.TokenGrantSourceForumLike, tokenmodel.TokenGrantSourceForumImport:
		return normalized, nil
	default:
		return forumRewardGrantRequest{}, tokenStoreBadRequest("source 仅支持 forum_like 或 forum_import")
	}
}

func parseForumRewardSourceRefID(raw string) (uint64, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return 0, tokenStoreBadRequest("source_ref_id 不能为空")
	}

	parsed, err := strconv.ParseUint(trimmed, 10, 64)
	if err != nil || parsed == 0 {
		return 0, tokenStoreBadRequest("source_ref_id 必须是正整数")
	}
	return parsed, nil
}

// validateExistingForumRewardGrant 校验重复 event_id 是否真的是同一条论坛奖励。
//
// 职责边界：
// 1. 只比较幂等所需的最小字段：接收人、来源和来源业务 ID；
// 2. 不比较金额和状态，避免规则调整后重放旧事件被误判；
// 3. 不一致时返回业务校验错误，让上游暴露这类错账风险。
func validateExistingForumRewardGrant(existing tokenmodel.TokenGrant, req forumRewardGrantRequest) error {
	sourceRefID := uint64(0)
	if existing.SourceRefID != nil {
		sourceRefID = *existing.SourceRefID
	}
	if existing.UserID != req.ReceiverUserID || existing.Source != req.Source || sourceRefID != req.SourceRefID {
		return tokenStoreBadRequest("event_id 幂等冲突：已有奖励记录与本次论坛奖励请求不一致")
	}
	return nil
}

// forumRewardDecision 解析论坛奖励发放决策。
//
// 职责边界：
// 1. 优先读取 token_reward_rules，保持“从表里读”的 P0 口径；
// 2. 规则停用或金额非正时写 skipped 账本，消费 outbox 但不增加 Token；
// 3. 表规则缺失时再读取配置和代码默认值，兼容旧环境尚未 seed 的情况。
func (s *Service) forumRewardDecision(ctx context.Context, source string) (forumRewardDecision, error) {
	rule, err := s.tokenDAO.FindRewardRuleBySource(ctx, source)
	if err != nil {
		return forumRewardDecision{}, err
	}
	if rule != nil {
		if strings.TrimSpace(rule.Status) != tokenmodel.TokenRewardRuleStatusActive {
			return skippedForumRewardDecision(source, "奖励规则已停用，未发放 Token"), nil
		}
		if rule.Amount <= 0 {
			return skippedForumRewardDecision(source, "奖励规则金额非正，未发放 Token"), nil
		}
		return recordedForumRewardDecision(source, rule.Amount), nil
	}

	switch strings.TrimSpace(source) {
	case tokenmodel.TokenGrantSourceForumLike:
		return recordedForumRewardDecision(source, positiveConfigAmountOrDefault(forumLikeRewardConfigKey, defaultForumLikeRewardAmount)), nil
	case tokenmodel.TokenGrantSourceForumImport:
		return recordedForumRewardDecision(source, positiveConfigAmountOrDefault(forumImportRewardConfigKey, defaultForumImportRewardAmount)), nil
	default:
		return skippedForumRewardDecision(source, "未知论坛奖励来源，未发放 Token"), nil
	}
}

func recordedForumRewardDecision(source string, amount int64) forumRewardDecision {
	if amount <= 0 {
		return skippedForumRewardDecision(source, "奖励金额非正，未发放 Token")
	}
	return forumRewardDecision{
		Amount:      amount,
		Status:      tokenmodel.TokenGrantStatusRecorded,
		Description: forumRewardDescription(source),
	}
}

func skippedForumRewardDecision(source string, description string) forumRewardDecision {
	return forumRewardDecision{
		Amount:      0,
		Status:      tokenmodel.TokenGrantStatusSkipped,
		Description: strings.TrimSpace(description),
	}
}

func positiveConfigAmountOrDefault(configKey string, fallback int64) int64 {
	amount := viper.GetInt64(configKey)
	if amount <= 0 {
		return fallback
	}
	return amount
}

func forumRewardDescription(source string) string {
	switch strings.TrimSpace(source) {
	case tokenmodel.TokenGrantSourceForumLike:
		return "计划被点赞奖励"
	case tokenmodel.TokenGrantSourceForumImport:
		return "计划被导入奖励"
	default:
		return "论坛奖励入账"
	}
}
