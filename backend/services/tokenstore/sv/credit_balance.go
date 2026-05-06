package sv

import (
	"context"

	creditcontracts "github.com/LoveLosita/smartflow/backend/shared/contracts/creditstore"
	"github.com/LoveLosita/smartflow/backend/shared/respond"
)

// GetCreditBalanceSnapshot 返回用户 Credit 余额快照。
//
// 职责边界：
// 1. 优先读 tokenstore 自己维护的 Redis 快照，未命中再回源 DB；
// 2. 只返回余额与阻断状态，不在这里计算价格或校验扣费规则；
// 3. DB 回源成功后会尽力回填缓存，但缓存失败不影响本次查询结果。
func (s *Service) GetCreditBalanceSnapshot(ctx context.Context, userID uint64) (*creditcontracts.CreditBalanceSnapshot, error) {
	if err := s.Ready(); err != nil {
		return nil, err
	}
	if userID == 0 {
		return nil, respond.MissingParam
	}

	if s.creditCache != nil {
		snapshot, ok, err := s.creditCache.GetCreditBalanceSnapshot(ctx, userID)
		if err == nil && ok && snapshot != nil {
			blocked, blockedErr := s.creditCache.IsUserCreditBlocked(ctx, userID)
			if blockedErr == nil {
				return &creditcontracts.CreditBalanceSnapshot{
					UserID:         userID,
					Balance:        snapshot.Balance,
					TotalRecharged: snapshot.TotalRecharged,
					TotalRewarded:  snapshot.TotalRewarded,
					TotalConsumed:  snapshot.TotalConsumed,
					IsBlocked:      blocked || snapshot.Balance <= 0,
					SnapshotSource: creditSnapshotSourceCache,
					UpdatedAt:      formatTime(snapshot.UpdatedAt),
				}, nil
			}
		}
	}

	account, err := s.creditDAO.FindAccountByUserID(ctx, userID)
	if err != nil {
		return nil, err
	}

	result := &creditcontracts.CreditBalanceSnapshot{
		UserID:         userID,
		SnapshotSource: creditSnapshotSourceDB,
	}
	if account != nil {
		result.Balance = account.Balance
		result.TotalRecharged = account.TotalRecharged
		result.TotalRewarded = account.TotalRewarded
		result.TotalConsumed = account.TotalConsumed
		result.IsBlocked = account.Balance <= 0
		result.UpdatedAt = formatTime(account.UpdatedAt)
	} else {
		result.Balance = 0
		result.IsBlocked = true
	}

	s.syncCreditCacheBestEffort(ctx, userID, account, nil)
	return result, nil
}
