package middleware

import (
	"errors"
	"net/http"

	"github.com/LoveLosita/smartflow/backend/auth"
	"github.com/LoveLosita/smartflow/backend/dao"
	"github.com/LoveLosita/smartflow/backend/model"
	"github.com/LoveLosita/smartflow/backend/respond"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v4"
)

// JWTTokenAuth 接收 cache 实例，体现依赖注入
func JWTTokenAuth(cache *dao.CacheDAO) gin.HandlerFunc {
	return func(c *gin.Context) {
		// 1. 获取 Token (Gin 的 GetHeader 直接返回 string)
		tokenString := c.GetHeader("Authorization")
		if tokenString == "" {
			c.JSON(http.StatusUnauthorized, respond.MissingToken)
			c.Abort()
			return
		}

		// 2. 改动：使用 ParseWithClaims 直接解析到你的结构体
		// 假设你的结构体叫 model.MyCustomClaims
		token, err := jwt.ParseWithClaims(tokenString, &model.MyCustomClaims{}, func(token *jwt.Token) (interface{}, error) {
			return auth.AccessKey, nil
		})

		if err != nil || !token.Valid {
			c.JSON(http.StatusUnauthorized, respond.InvalidToken)
			c.Abort()
			return
		}

		// 3. 校验 Claims
		claims, ok := token.Claims.(*model.MyCustomClaims)
		if !ok {
			c.JSON(http.StatusUnauthorized, respond.InvalidClaims)
			c.Abort()
			return
		}
		// --- 🛡️ 核心改造：设卡检查 ---
		if claims.TokenType != "access_token" {
			c.JSON(http.StatusUnauthorized, respond.WrongTokenType)
			c.Abort()
			return
		}

		// 拿着 jti 去 Redis 查一下
		isBlack, err := cache.IsBlacklisted(claims.Jti)
		if err != nil {
			// 如果 Redis 挂了，为了安全通常选择报错，或者降级放行（取决于你的业务）
			c.JSON(http.StatusInternalServerError, respond.InternalError(errors.New("无法验证令牌状态")))
			c.Abort()
			return
		}
		if isBlack {
			c.JSON(http.StatusUnauthorized, respond.UserLoggedOut)
			c.Abort()
			return
		}

		// 4. 存入上下文
		c.Set("user_id", claims.UserID)
		c.Set("claims", claims)
		c.Next() // 只有所有关卡都过了，才放行
	}
}
