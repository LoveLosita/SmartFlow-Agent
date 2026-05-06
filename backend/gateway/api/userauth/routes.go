package userauthapi

import (
	gatewaymiddleware "github.com/LoveLosita/smartflow/backend/gateway/middleware"
	rootmiddleware "github.com/LoveLosita/smartflow/backend/gateway/middleware"
	ratelimit "github.com/LoveLosita/smartflow/backend/shared/infra/ratelimit"
	"github.com/LoveLosita/smartflow/backend/shared/ports"
	"github.com/gin-gonic/gin"
)

// RegisterRoutes 把 user/auth HTTP 入口挂到 gateway 路由组。
// 职责边界：
// 1. 只注册 /user 下的边缘路由，不关心其它业务域路由；
// 2. 登录、注册、刷新 token 只做请求转发；登出需要先经过 access token 边缘鉴权；
// 3. 限流仍复用当前通用中间件，后续若 gateway 独立成包，可再整体下沉。
func RegisterRoutes(apiGroup *gin.RouterGroup, handler *UserHandler, authClient ports.AccessTokenValidator, limiter *ratelimit.RateLimiter) {
	if apiGroup == nil || handler == nil {
		return
	}

	userGroup := apiGroup.Group("/user")
	{
		userGroup.GET("/captcha/register", handler.CaptchaRegister)
		userGroup.POST("/register", handler.UserRegister)
		userGroup.POST("/login", handler.UserLogin)
		userGroup.POST("/refresh-token", handler.RefreshTokenHandler)
		userGroup.POST("/logout", gatewaymiddleware.JWTTokenAuth(authClient), rootmiddleware.RateLimitMiddleware(limiter, 20, 1), handler.UserLogout)
	}
}
