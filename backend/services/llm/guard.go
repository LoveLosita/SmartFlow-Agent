package llm

import (
	"context"
	"errors"
	"log"
	"time"

	llmdao "github.com/LoveLosita/smartflow/backend/services/llm/dao"
	creditcontracts "github.com/LoveLosita/smartflow/backend/shared/contracts/creditstore"
)

var (
	ErrRuntimeServiceNotReady = errors.New("llm runtime service dependency not initialized")
	ErrUnsupportedModelAlias  = errors.New("llm model alias is unsupported")
	ErrCreditBalanceBlocked   = errors.New("credit balance is insufficient")
)

const (
	defaultCreditBlockedTTL      = 5 * time.Minute
	defaultCreditSnapshotTimeout = time.Second
)

type CreditBalanceSnapshotProvider interface {
	GetCreditBalanceSnapshot(ctx context.Context, userID uint64) (*creditcontracts.CreditBalanceSnapshot, error)
}

// CreditBalanceGuard 负责在真正发起 LLM 调用前做一次轻量余额准入。
type CreditBalanceGuard struct {
	cacheDAO         *llmdao.CacheDAO
	snapshotProvider CreditBalanceSnapshotProvider
	blockTTL         time.Duration
	snapshotTimeout  time.Duration
}

type CreditBalanceGuardOptions struct {
	CacheDAO         *llmdao.CacheDAO
	SnapshotProvider CreditBalanceSnapshotProvider
	BlockTTL         time.Duration
	SnapshotTimeout  time.Duration
}

func NewCreditBalanceGuard(opts CreditBalanceGuardOptions) *CreditBalanceGuard {
	blockTTL := opts.BlockTTL
	if blockTTL <= 0 {
		blockTTL = defaultCreditBlockedTTL
	}
	snapshotTimeout := opts.SnapshotTimeout
	if snapshotTimeout <= 0 {
		snapshotTimeout = defaultCreditSnapshotTimeout
	}
	return &CreditBalanceGuard{
		cacheDAO:         opts.CacheDAO,
		snapshotProvider: opts.SnapshotProvider,
		blockTTL:         blockTTL,
		snapshotTimeout:  snapshotTimeout,
	}
}

// Guard 只做 Redis 快照级别的 fail-open 准入检查。
//
// 设计说明：
// 1. 先查 blocked key，命中则直接拒绝，避免每次都回源余额快照；
// 2. 再查余额快照；若快照明确余额 <= 0，则写 blocked key 并拒绝；
// 3. Redis 读失败或快照缺失时保持放行，避免基础设施抖动直接阻断全部 LLM 调用。
func (g *CreditBalanceGuard) Guard(ctx context.Context, billing BillingContext) error {
	if g == nil || g.cacheDAO == nil {
		return nil
	}

	billing = billing.Normalize()
	if billing.UserID == 0 || billing.SkipCharge {
		return nil
	}

	blocked, err := g.cacheDAO.IsUserCreditBlocked(ctx, billing.UserID)
	if err != nil {
		log.Printf("llm credit guard read blocked key failed: user_id=%d err=%v", billing.UserID, err)
		return nil
	}
	if blocked {
		return ErrCreditBalanceBlocked
	}

	snapshot, found, err := g.cacheDAO.GetUserCreditBalanceSnapshot(ctx, billing.UserID)
	if err != nil {
		log.Printf("llm credit guard read balance snapshot failed: user_id=%d err=%v", billing.UserID, err)
		return nil
	}
	if !found || snapshot == nil {
		snapshot, err = g.fetchSnapshot(ctx, billing.UserID)
		if err != nil {
			log.Printf("llm credit guard fetch balance snapshot failed: user_id=%d err=%v", billing.UserID, err)
			return nil
		}
	}
	if snapshot == nil {
		return nil
	}
	if snapshot.AvailableCredit > 0 {
		return nil
	}

	if err = g.cacheDAO.SetUserCreditBlocked(ctx, billing.UserID, g.blockTTL); err != nil {
		log.Printf("llm credit guard set blocked key failed: user_id=%d err=%v", billing.UserID, err)
	}
	return ErrCreditBalanceBlocked
}

func (g *CreditBalanceGuard) fetchSnapshot(ctx context.Context, userID uint64) (*llmdao.CreditBalanceSnapshot, error) {
	if g == nil || g.snapshotProvider == nil || userID == 0 {
		return nil, nil
	}

	fetchCtx := ctx
	if fetchCtx == nil {
		fetchCtx = context.Background()
	}
	if g.snapshotTimeout > 0 {
		var cancel context.CancelFunc
		fetchCtx, cancel = context.WithTimeout(context.WithoutCancel(fetchCtx), g.snapshotTimeout)
		defer cancel()
	}

	snapshotView, err := g.snapshotProvider.GetCreditBalanceSnapshot(fetchCtx, userID)
	if err != nil {
		return nil, err
	}
	if snapshotView == nil {
		return nil, nil
	}

	snapshot := &llmdao.CreditBalanceSnapshot{
		AvailableCredit: snapshotView.Balance,
		UpdatedAt:       time.Now(),
	}
	if err = g.cacheDAO.SetUserCreditBalanceSnapshot(fetchCtx, userID, *snapshot, 0); err != nil {
		log.Printf("llm credit guard backfill balance snapshot failed: user_id=%d err=%v", userID, err)
	}

	if snapshotView.IsBlocked || snapshotView.Balance <= 0 {
		if err = g.cacheDAO.SetUserCreditBlocked(fetchCtx, userID, g.blockTTL); err != nil {
			log.Printf("llm credit guard backfill blocked key failed: user_id=%d err=%v", userID, err)
		}
		return snapshot, nil
	}

	if err = g.cacheDAO.DeleteUserCreditBlocked(fetchCtx, userID); err != nil {
		log.Printf("llm credit guard clear blocked key failed: user_id=%d err=%v", userID, err)
	}
	return snapshot, nil
}
