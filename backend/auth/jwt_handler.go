package auth

import (
	"errors"
	"time"

	"github.com/LoveLosita/smartflow/backend/dao"
	"github.com/LoveLosita/smartflow/backend/model"
	"github.com/LoveLosita/smartflow/backend/respond"
	"github.com/golang-jwt/jwt/v4"
	"github.com/google/uuid"
	"github.com/spf13/viper"
)

var RefreshKey = []byte(viper.GetString("jwt.refreshSecret")) // 用于签名和验证刷新Token的密钥
var AccessKey = []byte(viper.GetString("jwt.accessSecret"))   // 用于签名和验证访问Token的密钥

// generateJTI 生成唯一的 JWT ID
func generateJTI() string {
	return uuid.New().String()
}

// GenerateTokens 生成访问令牌和刷新令牌
func GenerateTokens(userID int) (string, string, error) {
	// 创建访问令牌
	sid := generateJTI()
	accessToken := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"user_id":    userID,                                  // 获取用户ID
		"exp":        time.Now().Add(15 * time.Minute).Unix(), // 设置访问令牌过期时间为 15 分钟
		"token_type": "access_token",                          // 令牌类型为访问令牌
		"jti":        sid,                                     // 亲子共用的 JWT ID
	})

	// 使用密钥签名访问令牌
	accessTokenString, err := accessToken.SignedString(AccessKey)
	if err != nil {
		return "", "", err
	}

	// 创建刷新令牌
	refreshToken := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"user_id":    userID,                                    // 获取用户ID
		"exp":        time.Now().Add(7 * 24 * time.Hour).Unix(), // 设置刷新令牌过期时间为 7 天
		"token_type": "refresh_token",                           // 令牌类型为刷新令牌
		"jti":        sid,                                       // 亲子共用的 JWT ID
	})

	// 使用密钥签名刷新令牌
	refreshTokenString, err := refreshToken.SignedString(RefreshKey)
	if err != nil {
		return "", "", err
	}

	return accessTokenString, refreshTokenString, nil
}

// ValidateRefreshToken 验证刷新令牌的有效性
/*func ValidateRefreshToken(tokenString string) (*jwt.Token, error) {
	token, err := jwt.Parse(tokenString, func(token *jwt.Token) (interface{}, error) {
		// 检查签名方法是否为 HMAC
		if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, respond.InvalidTokenSingingMethod
		}
		// 返回用于验证的密钥
		return RefreshKey, nil
	})
	if err != nil {
		return nil, err
	}

	// 进一步检查载荷中 token_type 是否正确
	claims, ok := token.Claims.(jwt.MapClaims)
	if !ok {
		return nil, respond.InvalidClaims
	}
	// 检查 token_type 是否是 refresh_token
	if claimType, ok := claims["token_type"].(string); !ok || claimType != "refresh_token" {
		return nil, respond.WrongTokenType
	}
	return token, nil
}
*/

// ValidateRefreshToken 验证刷新令牌的有效性，并增加 Redis 黑名单检查
func ValidateRefreshToken(tokenString string, cache *dao.CacheDAO) (*jwt.Token, error) {
	// 1. 解析 Token 并直接绑定到你的自定义结构体
	token, err := jwt.ParseWithClaims(tokenString, &model.MyCustomClaims{}, func(token *jwt.Token) (interface{}, error) {
		if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, respond.InvalidTokenSingingMethod
		}
		return RefreshKey, nil
	})

	if err != nil || !token.Valid {
		return nil, err
	}

	// 2. 断言获取 Claims
	claims, ok := token.Claims.(*model.MyCustomClaims)
	if !ok {
		return nil, respond.InvalidClaims
	}

	// 3. 核心“设卡”：检查 token_type 是否是 refresh_token
	if claims.TokenType != "refresh_token" {
		return nil, respond.WrongTokenType
	}

	// 4. --- 🛡️ 终极关卡：检查 Redis 黑名单 ---
	// 即使签名没过期，如果 jti 在黑名单里（用户已登出），也视为无效
	isBlack, err := cache.IsBlacklisted(claims.Jti)
	if err != nil {
		// Redis 出错时的处理逻辑，建议报错以防“漏网之鱼”
		return nil, respond.InternalError(errors.New("无法验证令牌状态"))
	}
	if isBlack {
		return nil, respond.UserLoggedOut // 返回你定义的“用户已登出”错误
	}

	return token, nil
}
