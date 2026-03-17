package middleware

import (
	"errors"
	"log"
	"net/http"
	"time"

	"github.com/LoveLosita/smartflow/backend/dao"
	"github.com/LoveLosita/smartflow/backend/respond"
	"github.com/gin-gonic/gin"
)

const (
	// userTokenResetInterval 是“用户 token 周期重置”的窗口长度。
	// 当前按需求设置为 7 天。
	userTokenResetInterval = 7 * 24 * time.Hour

	// userTokenQuotaSnapshotTTL 是额度快照缓存时长。
	// 说明：该值越大，读 DB 越少；但“超额生效”的最坏延迟会变长。
	userTokenQuotaSnapshotTTL = 60 * time.Second

	// minUserTokenBlockTTL 是封禁键的最小 TTL，避免出现 0/负数导致“刚封就失效”。
	minUserTokenBlockTTL = 30 * time.Second
)

// TokenQuotaGuard 在请求入口做“token 额度门禁 + 懒重置”。
//
// 职责边界：
// 1. 负责在进入业务 Handler 前判断“该用户是否还能继续消费 token”；
// 2. 负责按 7 天窗口执行懒重置（只有访问时才判断是否重置）；
// 3. 负责维护 Redis 快照与封禁键，降低每次请求都查库的成本；
// 4. 不负责 token 累加记账（记账由聊天持久化链路负责）。
func TokenQuotaGuard(cache *dao.CacheDAO, userRepo *dao.UserDAO) gin.HandlerFunc {
	return func(c *gin.Context) {
		// 1. 基础依赖判空：
		// 1.1 若中间件依赖未初始化，直接返回 500，避免出现“无门禁放行”的安全漏洞；
		// 1.2 这里选择 fail-close（拒绝），因为该中间件是额度治理主入口。
		if cache == nil || userRepo == nil {
			c.JSON(http.StatusInternalServerError, respond.InternalError(errors.New("token quota guard dependencies not initialized")))
			c.Abort()
			return
		}

		// 2. 从 JWT 中间件上下文获取 user_id：
		// 2.1 若 user_id 非法，说明鉴权链路异常，直接按未授权拦截；
		// 2.2 这里不尝试兜底查 token，避免重复实现鉴权逻辑。
		userID := c.GetInt("user_id")
		if userID <= 0 {
			c.JSON(http.StatusUnauthorized, respond.ErrUnauthorized)
			c.Abort()
			return
		}

		ctx := c.Request.Context()
		now := time.Now()

		// 3. 快速封禁检查（Redis）：
		// 3.1 命中封禁键直接拒绝，避免每次都查 DB；
		// 3.2 Redis 查询失败时不立即放行，而是继续走 DB 严格校验，保证安全性。
		blocked, blockedErr := cache.IsUserTokenBlocked(ctx, userID)
		if blockedErr != nil {
			log.Printf("TokenQuotaGuard: 查询封禁键失败 user_id=%d err=%v，回退 DB 校验", userID, blockedErr)
		} else if blocked {
			c.JSON(http.StatusBadRequest, respond.TokenUsageExceedsLimit)
			c.Abort()
			return
		}

		// 4. 优先尝试走快照快速路径：
		// 4.1 命中快照且未到重置窗口时，直接用快照判断；
		// 4.2 快照未命中/已到重置窗口/读取失败，则回源 DB 做权威判断。
		snapshot, hit, snapshotErr := cache.GetUserTokenQuotaSnapshot(ctx, userID)
		if snapshotErr != nil {
			log.Printf("TokenQuotaGuard: 读取额度快照失败 user_id=%d err=%v，回退 DB 校验", userID, snapshotErr)
		}
		if hit && snapshot != nil && !isResetDue(snapshot.LastResetAt, now) {
			if snapshot.TokenUsage > snapshot.TokenLimit {
				// 4.3 快照判断超额时，顺手写入封禁键，后续请求可 O(1) 拦截。
				ttl := calcBlockTTL(snapshot.LastResetAt, now)
				if err := cache.SetUserTokenBlocked(ctx, userID, ttl); err != nil {
					log.Printf("TokenQuotaGuard: 写入封禁键失败 user_id=%d err=%v", userID, err)
				}
				c.JSON(http.StatusBadRequest, respond.TokenUsageExceedsLimit)
				c.Abort()
				return
			}

			// 4.4 快照命中且未超额，直接放行，避免本次请求访问 DB。
			c.Next()
			return
		}

		// 5. 回源 DB（权威判断路径）：
		// 5.1 先读取用户额度字段；
		// 5.2 若已到重置窗口，执行条件更新懒重置，再回读最新值；
		// 5.3 最后依据“token_usage > token_limit”判断是否拦截。
		quota, err := userRepo.GetUserTokenQuotaByID(ctx, userID)
		if err != nil {
			log.Printf("TokenQuotaGuard: 查询用户额度失败 user_id=%d err=%v", userID, err)
			c.JSON(http.StatusInternalServerError, respond.InternalError(err))
			c.Abort()
			return
		}

		if isResetDue(quota.LastResetAt, now) {
			_, resetErr := userRepo.ResetUserTokenUsageIfDue(ctx, userID, now.Add(-userTokenResetInterval), now)
			if resetErr != nil {
				log.Printf("TokenQuotaGuard: 懒重置失败 user_id=%d err=%v", userID, resetErr)
				c.JSON(http.StatusInternalServerError, respond.InternalError(resetErr))
				c.Abort()
				return
			}

			// 5.2.1 重置后回读一次最新额度，避免使用旧值继续判断；
			// 5.2.2 同时主动清理封禁键，防止“已重置仍被封”的残留状态。
			quota, err = userRepo.GetUserTokenQuotaByID(ctx, userID)
			if err != nil {
				log.Printf("TokenQuotaGuard: 重置后回读失败 user_id=%d err=%v", userID, err)
				c.JSON(http.StatusInternalServerError, respond.InternalError(err))
				c.Abort()
				return
			}
			if delErr := cache.DeleteUserTokenBlocked(ctx, userID); delErr != nil {
				log.Printf("TokenQuotaGuard: 清理封禁键失败 user_id=%d err=%v", userID, delErr)
			}
		}

		// 6. 把最新权威值回填快照：
		// 6.1 回填失败不影响主流程（仅影响性能，不影响正确性）；
		// 6.2 这样后续同用户短时间内请求可直接走快照快速路径。
		if setErr := cache.SetUserTokenQuotaSnapshot(ctx, userID, dao.UserTokenQuotaSnapshot{
			TokenLimit:  quota.TokenLimit,
			TokenUsage:  quota.TokenUsage,
			LastResetAt: quota.LastResetAt,
		}, userTokenQuotaSnapshotTTL); setErr != nil {
			log.Printf("TokenQuotaGuard: 回填额度快照失败 user_id=%d err=%v", userID, setErr)
		}

		// 7. 最终判定：
		// 7.1 按你的规则使用“>”判断超额（等于不拦截）；
		// 7.2 超额时写封禁键并拒绝；未超额则继续放行。
		if quota.TokenUsage > quota.TokenLimit {
			ttl := calcBlockTTL(quota.LastResetAt, now)
			if err = cache.SetUserTokenBlocked(ctx, userID, ttl); err != nil {
				log.Printf("TokenQuotaGuard: 写入封禁键失败 user_id=%d err=%v", userID, err)
			}
			c.JSON(http.StatusBadRequest, respond.TokenUsageExceedsLimit)
			c.Abort()
			return
		}

		c.Next()
	}
}

// isResetDue 判断“是否到达 7 天懒重置窗口”。
//
// 说明：
// 1. lastResetAt 为零值时，视为到期（首次迁移数据时兜底）；
// 2. now - lastResetAt >= 7 天 时返回 true。
func isResetDue(lastResetAt time.Time, now time.Time) bool {
	if lastResetAt.IsZero() {
		return true
	}
	return !lastResetAt.Add(userTokenResetInterval).After(now)
}

// calcBlockTTL 计算封禁键 TTL。
//
// 规则：
// 1. 目标是封到“下一次重置时间”；
// 2. 若计算结果非正数，回退到最小 TTL，避免封禁键瞬时失效。
func calcBlockTTL(lastResetAt time.Time, now time.Time) time.Duration {
	if lastResetAt.IsZero() {
		return minUserTokenBlockTTL
	}
	nextResetAt := lastResetAt.Add(userTokenResetInterval)
	ttl := nextResetAt.Sub(now)
	if ttl <= 0 {
		return minUserTokenBlockTTL
	}
	return ttl
}
