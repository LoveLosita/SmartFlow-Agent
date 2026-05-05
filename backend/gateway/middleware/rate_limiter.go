package middleware

import (
	"fmt"
	"log"

	"github.com/LoveLosita/smartflow/backend/gateway/shared/respond"
	ratelimit "github.com/LoveLosita/smartflow/backend/shared/infra/ratelimit"
	"github.com/gin-gonic/gin"
)

func RateLimitMiddleware(limiter *ratelimit.RateLimiter, capacity, rate int) gin.HandlerFunc {
	return func(c *gin.Context) {
		// 1. 确定限流对象：可以用 UserID，也可以用 IP
		// 这里建议用 UserID，防止某个用户换 IP 疯狂刷
		userID := c.GetInt("user_id") // 假设你之前的 JWT 已经塞进去了
		key := fmt.Sprintf("rate_limit:user:%d", userID)

		// 2. 执行限流检查
		allowed, err := limiter.Allow(c.Request.Context(), key, capacity, rate)
		if err != nil {
			// 如果 Redis 挂了，为了保证业务可用，通常选择“放行”并记录日志
			log.Printf("Redis limiter error: %v", err)
			c.Next()
			return
		}

		if !allowed {
			// 3. 触发限流：直接调你写好的 DealWithError
			// 可以在 respond 里定义一个新错误：TooManyRequests
			respond.DealWithError(c, respond.TooManyRequests)
			c.Abort() // 拦截，不执行后续 Handler
			return
		}

		c.Next()
	}
}
