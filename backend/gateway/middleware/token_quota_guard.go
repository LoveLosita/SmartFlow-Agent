package middleware

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/LoveLosita/smartflow/backend/gateway/shared/respond"
	"github.com/LoveLosita/smartflow/backend/shared/ports"
	"github.com/gin-gonic/gin"
)

// TokenQuotaGuard 在请求入口做 token 额度门禁。
// 职责边界：
// 1. 只负责调用 user/auth 服务判断当前用户是否还能继续消耗 token；
// 2. 不再直连 users 表或 Redis 额度细节；
// 3. 额度超限时直接拒绝，不进入业务 handler。
func TokenQuotaGuard(checker ports.TokenQuotaChecker) gin.HandlerFunc {
	return func(c *gin.Context) {
		if checker == nil {
			c.JSON(http.StatusInternalServerError, respond.InternalError(errors.New("token quota checker dependency not initialized")))
			c.Abort()
			return
		}

		userID := c.GetInt("user_id")
		if userID <= 0 {
			c.JSON(http.StatusUnauthorized, respond.ErrUnauthorized)
			c.Abort()
			return
		}

		ctx, cancel := context.WithTimeout(c.Request.Context(), 2*time.Second)
		defer cancel()

		resp, err := checker.CheckTokenQuota(ctx, userID)
		if err != nil {
			writeRespondError(c, err)
			c.Abort()
			return
		}
		if resp == nil || !resp.Allowed {
			c.JSON(http.StatusBadRequest, respond.TokenUsageExceedsLimit)
			c.Abort()
			return
		}

		c.Next()
	}
}
