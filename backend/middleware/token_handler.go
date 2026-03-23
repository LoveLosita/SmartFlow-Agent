package middleware

import (
	"errors"
	"net/http"
	"strings"

	"github.com/LoveLosita/smartflow/backend/auth"
	"github.com/LoveLosita/smartflow/backend/dao"
	"github.com/LoveLosita/smartflow/backend/model"
	"github.com/LoveLosita/smartflow/backend/respond"
	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v4"
)

// extractTokenFromAuthorization 负责解析 Authorization 头中的 token。
// 职责边界：
// 1. 兼容“裸 token”和“Bearer <token>”两种传参方式。
// 2. 不负责 token 合法性校验，只做字符串提取。
// 3. 输入输出语义：header 为空或格式非法时返回空字符串。
func extractTokenFromAuthorization(header string) string {
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

// JWTTokenAuth 负责 access token 的鉴权拦截。
// 职责边界：
// 1. 负责解析 token、验签、校验 token_type 与黑名单状态。
// 2. 不负责签发 token，也不负责用户登录逻辑。
// 3. 输出语义：校验通过时写入 user_id/claims 到上下文并放行；失败则中断请求。
func JWTTokenAuth(cache *dao.CacheDAO) gin.HandlerFunc {
	return func(c *gin.Context) {
		tokenString := extractTokenFromAuthorization(c.GetHeader("Authorization"))
		if tokenString == "" {
			c.JSON(http.StatusUnauthorized, respond.MissingToken)
			c.Abort()
			return
		}

		accessKey, err := auth.AccessSigningKey()
		if err != nil {
			c.JSON(http.StatusInternalServerError, respond.InternalError(err))
			c.Abort()
			return
		}

		// 1. 先验签并由 jwt 库统一校验 exp 等标准声明。
		token, err := jwt.ParseWithClaims(tokenString, &model.MyCustomClaims{}, func(token *jwt.Token) (interface{}, error) {
			if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
				return nil, respond.InvalidTokenSingingMethod
			}
			return accessKey, nil
		})
		if err != nil || !token.Valid {
			c.JSON(http.StatusUnauthorized, respond.InvalidToken)
			c.Abort()
			return
		}

		// 2. 再做业务声明校验，防止 refresh token 越权访问业务接口。
		claims, ok := token.Claims.(*model.MyCustomClaims)
		if !ok {
			c.JSON(http.StatusUnauthorized, respond.InvalidClaims)
			c.Abort()
			return
		}
		if claims.TokenType != "access_token" {
			c.JSON(http.StatusUnauthorized, respond.WrongTokenType)
			c.Abort()
			return
		}

		// 3. 最后查黑名单，兜住“用户已登出但 token 仍未到期”的场景。
		isBlack, err := cache.IsBlacklisted(claims.Jti)
		if err != nil {
			c.JSON(http.StatusInternalServerError, respond.InternalError(errors.New("无法验证令牌状态")))
			c.Abort()
			return
		}
		if isBlack {
			c.JSON(http.StatusUnauthorized, respond.UserLoggedOut)
			c.Abort()
			return
		}

		c.Set("user_id", claims.UserID)
		c.Set("claims", claims)
		c.Next()
	}
}
