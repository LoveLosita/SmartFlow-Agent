package sv

import (
	"context"
	"strings"

	"github.com/LoveLosita/smartflow/backend/respond"
	tokenstoredao "github.com/LoveLosita/smartflow/backend/services/tokenstore/dao"
	tokencontracts "github.com/LoveLosita/smartflow/backend/shared/contracts/tokenstore"
)

// GetSummary 聚合当前用户在 token-store 账本中的获得记录。
//
// 职责边界：
// 1. 只统计 token_grants，不读取 user/auth 的权威额度。
// 2. 只汇总正向获取额度，避免把未来的冲正或补偿误算进 P0 展示口径。
// 3. quota_sync_status 在 P0 固定为 not_connected，明确告知尚未打通权威额度。
func (s *Service) GetSummary(ctx context.Context, actorUserID uint64) (*tokencontracts.TokenSummary, error) {
	if err := s.Ready(); err != nil {
		return nil, err
	}
	if actorUserID == 0 {
		return nil, respond.MissingParam
	}

	summary, err := s.tokenDAO.SummarizePositiveGrants(ctx, actorUserID)
	if err != nil {
		return nil, err
	}

	pending := summary.RecordedTokenTotal - summary.AppliedTokenTotal
	if pending < 0 {
		pending = 0
	}

	return &tokencontracts.TokenSummary{
		RecordedTokenTotal:     summary.RecordedTokenTotal,
		AppliedTokenTotal:      summary.AppliedTokenTotal,
		PendingApplyTokenTotal: pending,
		QuotaSyncStatus:        tokenSummaryQuotaStatusNotConnected,
		Tip:                    tokenSummaryTipP0,
	}, nil
}

// ListGrants 按用户分页查询 Token 获得记录。
//
// 职责边界：
// 1. 只支持 user_id 维度分页和 source 过滤，不做跨用户检索。
// 2. 负责把空 source 归一化为“不筛选”，避免 DAO 层重复处理入口噪音。
// 3. 结果只来自账本事实，不推导 user/auth 可用额度。
func (s *Service) ListGrants(ctx context.Context, req tokencontracts.ListTokenGrantsRequest) ([]tokencontracts.TokenGrantView, tokencontracts.PageResult, error) {
	if err := s.Ready(); err != nil {
		return nil, tokencontracts.PageResult{}, err
	}
	if req.ActorUserID == 0 {
		return nil, tokencontracts.PageResult{}, respond.MissingParam
	}

	page, pageSize := normalizePage(req.Page, req.PageSize)
	query := tokenstoredao.ListTokenGrantsQuery{
		UserID:   req.ActorUserID,
		Page:     page,
		PageSize: pageSize,
		Source:   strings.TrimSpace(req.Source),
	}

	total, err := s.tokenDAO.CountGrants(ctx, query)
	if err != nil {
		return nil, tokencontracts.PageResult{}, err
	}
	grants, err := s.tokenDAO.ListGrants(ctx, query)
	if err != nil {
		return nil, tokencontracts.PageResult{}, err
	}
	if len(grants) == 0 {
		return []tokencontracts.TokenGrantView{}, pageResult(page, pageSize, total), nil
	}

	result := make([]tokencontracts.TokenGrantView, 0, len(grants))
	for _, grant := range grants {
		result = append(result, grantViewFromModel(grant))
	}
	return result, pageResult(page, pageSize, total), nil
}
