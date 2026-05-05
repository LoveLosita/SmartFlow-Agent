package middleware

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/LoveLosita/smartflow/backend/gateway/shared/respond"
	"github.com/LoveLosita/smartflow/backend/shared/ports"
	"github.com/gin-gonic/gin"
)

// ExtractTokenFromAuthorization 从 Authorization 头中提取 token。
// 职责边界：
// 1. 兼容裸 token 与 Bearer token 两种传参方式；
// 2. 不做签名校验，只做字符串提取；
// 3. 返回空串表示缺少或格式非法。
func ExtractTokenFromAuthorization(header string) string {
	trimmed := strings.TrimSpace(header)
	if trimmed == "" {
		return ""
	}

	parts := strings.Fields(trimmed)
	if len(parts) == 2 && strings.EqualFold(parts[0], "Bearer") {
		return strings.TrimSpace(parts[1])
	}
	if len(parts) == 1 {
		return parts[0]
	}
	return ""
}

// JWTTokenAuth 负责 access token 的 gateway 边缘鉴权。
// 职责边界：
// 1. 只验证 token，并把 user_id 写入 gin 上下文；
// 2. 不直连 Redis、JWT 或 users 表，所有核心校验都交给 userauth 服务；
// 3. 校验失败时直接中断请求，由 respond 风格统一写回前端。
func JWTTokenAuth(validator ports.AccessTokenValidator) gin.HandlerFunc {
	return func(c *gin.Context) {
		if validator == nil {
			c.JSON(http.StatusInternalServerError, respond.InternalError(errors.New("token validator dependency not initialized")))
			c.Abort()
			return
		}

		tokenString := ExtractTokenFromAuthorization(c.GetHeader("Authorization"))
		if tokenString == "" {
			c.JSON(http.StatusUnauthorized, respond.MissingToken)
			c.Abort()
			return
		}

		ctx, cancel := context.WithTimeout(c.Request.Context(), 2*time.Second)
		defer cancel()

		resp, err := validator.ValidateAccessToken(ctx, tokenString)
		if err != nil {
			writeRespondError(c, err)
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
