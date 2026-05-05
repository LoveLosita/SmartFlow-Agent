package sv

import (
	"context"
	"errors"
	"log"
	"strings"
	"time"

	userauthdao "github.com/LoveLosita/smartflow/backend/services/userauth/dao"
	userauthmodel "github.com/LoveLosita/smartflow/backend/services/userauth/model"
	contracts "github.com/LoveLosita/smartflow/backend/shared/contracts/userauth"
	"github.com/LoveLosita/smartflow/backend/shared/respond"
)

const (
	userTokenResetInterval    = 7 * 24 * time.Hour
	userTokenQuotaSnapshotTTL = 60 * time.Second
	minUserTokenBlockTTL      = 30 * time.Second
)

// CheckTokenQuota 是 user/auth 服务内的 token 额度门禁。
//
// 职责边界：
// 1. 判断用户是否还能继续发起高消耗 agent/chat 请求；
// 2. 维护额度周期懒重置、Redis 快照和封禁键；
// 3. 不负责本轮对话完成后的 token 记账，记账由 AdjustTokenUsage 处理。
func (s *Service) CheckTokenQuota(ctx context.Context, req contracts.CheckTokenQuotaRequest) (*contracts.CheckTokenQuotaResponse, error) {
	if s == nil || s.userRepo == nil || s.cacheRepo == nil {
		return nil, errors.New("userauth quota dependencies not initialized")
	}
	if req.UserID <= 0 {
		return nil, respond.ErrUnauthorized
	}

	now := time.Now()

	// 1. 先查封禁键。封禁键的 TTL 按重置窗口计算，命中时可以避免每次回源 DB。
	blocked, blockedErr := s.cacheRepo.IsUserTokenBlocked(ctx, req.UserID)
	if blockedErr != nil {
		log.Printf("userauth quota: 查询封禁键失败 user_id=%d err=%v，回源 DB 校验", req.UserID, blockedErr)
	} else if blocked {
		return &contracts.CheckTokenQuotaResponse{Allowed: false}, nil
	}

	// 2. 快照未到重置窗口时直接判断；快照损坏或过期则回源 DB。
	snapshot, hit, snapshotErr := s.cacheRepo.GetUserTokenQuotaSnapshot(ctx, req.UserID)
	if snapshotErr != nil {
		log.Printf("userauth quota: 读取额度快照失败 user_id=%d err=%v，回源 DB 校验", req.UserID, snapshotErr)
	}
	if hit && snapshot != nil && !isResetDue(snapshot.LastResetAt, now) {
		if isQuotaExceeded(snapshot.TokenLimit, snapshot.TokenUsage) {
			ttl := calcBlockTTL(snapshot.LastResetAt, now)
			if err := s.cacheRepo.SetUserTokenBlocked(ctx, req.UserID, ttl); err != nil {
				log.Printf("userauth quota: 写入封禁键失败 user_id=%d err=%v", req.UserID, err)
			}
			return quotaResponse(false, snapshot.TokenLimit, snapshot.TokenUsage, snapshot.LastResetAt), nil
		}
		return quotaResponse(true, snapshot.TokenLimit, snapshot.TokenUsage, snapshot.LastResetAt), nil
	}

	// 3. 回源 DB 做权威判断；到 7 天窗口则先懒重置，再回读最新额度。
	quota, err := s.userRepo.GetUserTokenQuotaByID(ctx, req.UserID)
	if err != nil {
		return nil, err
	}
	if isResetDue(quota.LastResetAt, now) {
		if _, err = s.userRepo.ResetUserTokenUsageIfDue(ctx, req.UserID, now.Add(-userTokenResetInterval), now); err != nil {
			return nil, err
		}
		quota, err = s.userRepo.GetUserTokenQuotaByID(ctx, req.UserID)
		if err != nil {
			return nil, err
		}
		if delErr := s.cacheRepo.DeleteUserTokenBlocked(ctx, req.UserID); delErr != nil {
			log.Printf("userauth quota: 清理封禁键失败 user_id=%d err=%v", req.UserID, delErr)
		}
	}
	return s.cacheQuotaAndBuildResponse(ctx, req.UserID, quota, now, "quota")
}

// AdjustTokenUsage 在 user/auth 服务内回写用户 token 账本。
//
// 职责边界：
// 1. 只负责 users.token_usage 的增量调整与 quota 缓存刷新；
// 2. 不负责 agent 会话 token_total，调用方仍需在各自领域内维护会话统计；
// 3. event_id 非空时通过 MySQL 幂等表和 users 更新同事务提交，避免 outbox 重试或并发重放重复记账。
func (s *Service) AdjustTokenUsage(ctx context.Context, req contracts.AdjustTokenUsageRequest) (*contracts.CheckTokenQuotaResponse, error) {
	if s == nil || s.userRepo == nil || s.cacheRepo == nil {
		return nil, errors.New("userauth adjust dependencies not initialized")
	}
	if req.UserID <= 0 || req.TokenDelta <= 0 {
		return nil, respond.MissingParam
	}

	now := time.Now()
	eventID := strings.TrimSpace(req.EventID)

	var currentQuota *userauthmodel.User
	var err error
	if eventID != "" {
		var duplicated bool
		currentQuota, duplicated, err = s.userRepo.AdjustTokenUsageOnce(ctx, eventID, req.UserID, req.TokenDelta, now.Add(-userTokenResetInterval), now)
		if err != nil {
			return nil, err
		}
		if duplicated {
			return s.CheckTokenQuota(ctx, contracts.CheckTokenQuotaRequest{UserID: req.UserID})
		}
	} else {
		currentQuota, err = s.userRepo.GetUserTokenQuotaByID(ctx, req.UserID)
		if err != nil {
			return nil, err
		}
		if isResetDue(currentQuota.LastResetAt, now) {
			if _, err = s.userRepo.ResetUserTokenUsageIfDue(ctx, req.UserID, now.Add(-userTokenResetInterval), now); err != nil {
				return nil, err
			}
		}
		if _, err = s.userRepo.AddTokenUsage(ctx, req.UserID, req.TokenDelta); err != nil {
			return nil, err
		}
		currentQuota, err = s.userRepo.GetUserTokenQuotaByID(ctx, req.UserID)
		if err != nil {
			return nil, err
		}
	}

	return s.cacheQuotaAndBuildResponse(ctx, req.UserID, currentQuota, now, "adjust")
}

func (s *Service) cacheQuotaAndBuildResponse(ctx context.Context, userID int, quota *userauthmodel.User, now time.Time, source string) (*contracts.CheckTokenQuotaResponse, error) {
	if quota == nil {
		return nil, errors.New("userauth quota is nil")
	}

	snapshot := userauthdao.TokenQuotaSnapshot{
		TokenLimit:  quota.TokenLimit,
		TokenUsage:  quota.TokenUsage,
		LastResetAt: quota.LastResetAt,
	}
	if setErr := s.cacheRepo.SetUserTokenQuotaSnapshot(ctx, userID, snapshot, userTokenQuotaSnapshotTTL); setErr != nil {
		log.Printf("userauth %s: 回填额度快照失败 user_id=%d err=%v", source, userID, setErr)
		if delErr := s.cacheRepo.DeleteUserTokenQuotaSnapshot(ctx, userID); delErr != nil {
			log.Printf("userauth %s: 清理失效额度快照失败 user_id=%d err=%v", source, userID, delErr)
		}
	}

	if isQuotaExceeded(quota.TokenLimit, quota.TokenUsage) {
		ttl := calcBlockTTL(quota.LastResetAt, now)
		if err := s.cacheRepo.SetUserTokenBlocked(ctx, userID, ttl); err != nil {
			log.Printf("userauth %s: 写入封禁标记失败 user_id=%d err=%v", source, userID, err)
		}
		return quotaResponse(false, quota.TokenLimit, quota.TokenUsage, quota.LastResetAt), nil
	}

	if delErr := s.cacheRepo.DeleteUserTokenBlocked(ctx, userID); delErr != nil {
		log.Printf("userauth %s: 清理封禁标记失败 user_id=%d err=%v", source, userID, delErr)
	}
	return quotaResponse(true, quota.TokenLimit, quota.TokenUsage, quota.LastResetAt), nil
}

func quotaResponse(allowed bool, tokenLimit int, tokenUsage int, lastResetAt time.Time) *contracts.CheckTokenQuotaResponse {
	return &contracts.CheckTokenQuotaResponse{
		Allowed:     allowed,
		TokenLimit:  tokenLimit,
		TokenUsage:  tokenUsage,
		LastResetAt: lastResetAt,
	}
}

func isQuotaExceeded(tokenLimit int, tokenUsage int) bool {
	return tokenUsage >= tokenLimit
}

func isResetDue(lastResetAt time.Time, now time.Time) bool {
	if lastResetAt.IsZero() {
		return true
	}
	return !lastResetAt.Add(userTokenResetInterval).After(now)
}

func calcBlockTTL(lastResetAt time.Time, now time.Time) time.Duration {
	if lastResetAt.IsZero() {
		return minUserTokenBlockTTL
	}
	ttl := lastResetAt.Add(userTokenResetInterval).Sub(now)
	if ttl <= 0 {
		return minUserTokenBlockTTL
	}
	return ttl
}
