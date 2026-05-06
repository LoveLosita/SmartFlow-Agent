package sv

import (
	"strconv"
	"strings"

	storemodel "github.com/LoveLosita/smartflow/backend/services/tokenstore/model"
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

func normalizeForumRewardGrantRequest(req forumRewardGrantRequest) (forumRewardGrantRequest, error) {
	normalized := forumRewardGrantRequest{
		EventID:        strings.TrimSpace(req.EventID),
		ReceiverUserID: req.ReceiverUserID,
		Source:         strings.ToLower(strings.TrimSpace(req.Source)),
		SourceRefID:    req.SourceRefID,
	}

	switch {
	case normalized.EventID == "":
		return forumRewardGrantRequest{}, tokenStoreBadRequest("event_id 不能为空")
	case normalized.ReceiverUserID == 0:
		return forumRewardGrantRequest{}, tokenStoreBadRequest("receiver_user_id 不能为空")
	case normalized.SourceRefID == 0:
		return forumRewardGrantRequest{}, tokenStoreBadRequest("source_ref_id 不能为空")
	}

	switch normalized.Source {
	case storemodel.CreditLedgerSourceForumLike, storemodel.CreditLedgerSourceForumImport:
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

func skippedForumRewardDecision(source string, description string) forumRewardDecision {
	return forumRewardDecision{
		Amount:      0,
		Status:      storemodel.CreditLedgerStatusSkipped,
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
	case storemodel.CreditLedgerSourceForumLike:
		return "计划被点赞奖励"
	case storemodel.CreditLedgerSourceForumImport:
		return "计划被导入奖励"
	default:
		return "论坛奖励入账"
	}
}
