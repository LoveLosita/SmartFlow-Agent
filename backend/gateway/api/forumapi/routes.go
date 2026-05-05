package forumapi

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/LoveLosita/smartflow/backend/services/runtime/dao"
	gatewaymiddleware "github.com/LoveLosita/smartflow/backend/gateway/middleware"
	rootmiddleware "github.com/LoveLosita/smartflow/backend/gateway/middleware"
	"github.com/LoveLosita/smartflow/backend/gateway/shared/respond"
	ratelimit "github.com/LoveLosita/smartflow/backend/shared/infra/ratelimit"
	"github.com/LoveLosita/smartflow/backend/shared/ports"
	"github.com/gin-gonic/gin"
)

// RegisterRoutes 把计划广场 HTTP 入口挂到 gateway 路由组。
//
// 职责边界：
// 1. 只注册 /plan-square 下的边缘路由，不承载论坛业务规则；
// 2. 公开读接口允许匿名访问，若携带 token 则补齐 viewer_state；
// 3. 写接口必须登录，并按既有 Redis 幂等中间件保护重复提交。
func RegisterRoutes(apiGroup *gin.RouterGroup, handler *Handler, authClient ports.AccessTokenValidator, cache *dao.CacheDAO, limiter *ratelimit.RateLimiter) {
	if apiGroup == nil || handler == nil {
		return
	}

	planSquare := apiGroup.Group("/plan-square")
	{
		publicGroup := planSquare.Group("")
		publicGroup.Use(optionalJWTTokenAuth(authClient), rootmiddleware.RateLimitMiddleware(limiter, 40, 1))
		publicGroup.GET("/posts", handler.ListPosts)
		publicGroup.GET("/tags", handler.ListTags)
		publicGroup.GET("/posts/:post_id", handler.GetPost)
		publicGroup.GET("/posts/:post_id/comments", handler.ListComments)

		writeGroup := planSquare.Group("")
		writeGroup.Use(gatewaymiddleware.JWTTokenAuth(authClient), rootmiddleware.RateLimitMiddleware(limiter, 20, 1))
		writeGroup.POST("/posts", rootmiddleware.IdempotencyMiddleware(cache), handler.CreatePost)
		writeGroup.POST("/posts/:post_id/like", handler.LikePost)
		writeGroup.DELETE("/posts/:post_id/like", handler.UnlikePost)
		writeGroup.POST("/posts/:post_id/comments", rootmiddleware.IdempotencyMiddleware(cache), handler.CreateComment)
		writeGroup.DELETE("/comments/:comment_id", handler.DeleteComment)
		writeGroup.POST("/posts/:post_id/import", rootmiddleware.IdempotencyMiddleware(cache), handler.ImportPost)
	}
}

// optionalJWTTokenAuth 为计划广场公开读接口提供“可登录增强”。
//
// 步骤说明：
// 1. 没有 Authorization 时直接放行，让匿名用户也能浏览计划广场；
// 2. 有 Authorization 时复用 user/auth 校验，并把 user_id 写入上下文；
// 3. token 非法时按正常鉴权失败返回，避免前端误以为已登录状态仍可用。
func optionalJWTTokenAuth(validator ports.AccessTokenValidator) gin.HandlerFunc {
	return func(c *gin.Context) {
		tokenString := gatewaymiddleware.ExtractTokenFromAuthorization(c.GetHeader("Authorization"))
		if tokenString == "" {
			c.Next()
			return
		}
		if validator == nil {
			c.JSON(http.StatusInternalServerError, respond.InternalError(errors.New("计划广场可选鉴权依赖未初始化")))
			c.Abort()
			return
		}

		ctx, cancel := context.WithTimeout(c.Request.Context(), 2*time.Second)
		defer cancel()

		resp, err := validator.ValidateAccessToken(ctx, tokenString)
		if err != nil {
			respond.DealWithError(c, err)
			c.Abort()
			return
		}
		if resp == nil || !resp.Valid || resp.UserID <= 0 {
			c.JSON(http.StatusUnauthorized, respond.InvalidClaims)
			c.Abort()
			return
		}

		c.Set("user_id", resp.UserID)
		c.Set("claims", resp)
		c.Next()
	}
}
