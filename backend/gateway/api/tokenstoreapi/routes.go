package tokenstoreapi

import (
	"github.com/LoveLosita/smartflow/backend/services/runtime/dao"
	gatewaymiddleware "github.com/LoveLosita/smartflow/backend/gateway/middleware"
	rootmiddleware "github.com/LoveLosita/smartflow/backend/gateway/middleware"
	ratelimit "github.com/LoveLosita/smartflow/backend/shared/infra/ratelimit"
	"github.com/LoveLosita/smartflow/backend/shared/ports"
	"github.com/gin-gonic/gin"
)

// RegisterRoutes 把 Token 商店 HTTP 入口挂到 gateway 路由组。
//
// 职责边界：
// 1. 只注册 /token-store 下的边缘路由，不承载订单和 grant 业务规则；
// 2. P0 全部接口都要求登录，并统一走限流保护；
// 3. 只有创建订单与 mock paid 需要幂等键，避免重复下单或重复确认支付。
func RegisterRoutes(apiGroup *gin.RouterGroup, handler *Handler, authClient ports.AccessTokenValidator, cache *dao.CacheDAO, limiter *ratelimit.RateLimiter) {
	if apiGroup == nil || handler == nil {
		return
	}

	tokenStoreGroup := apiGroup.Group("/token-store")
	tokenStoreGroup.Use(gatewaymiddleware.JWTTokenAuth(authClient), rootmiddleware.RateLimitMiddleware(limiter, 20, 1))
	{
		tokenStoreGroup.GET("/summary", handler.GetSummary)
		tokenStoreGroup.GET("/products", handler.ListProducts)
		tokenStoreGroup.POST("/orders", rootmiddleware.IdempotencyMiddleware(cache), handler.CreateOrder)
		tokenStoreGroup.GET("/orders", handler.ListOrders)
		tokenStoreGroup.GET("/orders/:order_id", handler.GetOrder)
		tokenStoreGroup.POST("/orders/:order_id/mock-paid", rootmiddleware.IdempotencyMiddleware(cache), handler.MockPaidOrder)
		tokenStoreGroup.GET("/grants", handler.ListGrants)
	}
}
