package auth

import (
	"time"

	"github.com/golang-jwt/jwt/v4"
	"github.com/smartflow/backend/respond"
	"github.com/spf13/viper"
)

var RefreshKey = []byte(viper.GetString("jwt.accessSecret")) // 用于签名和验证刷新Token的密钥
var AccessKey = []byte(viper.GetString("jwt.refreshSecret")) // 用于签名和验证访问Token的密钥

// GenerateTokens 生成访问令牌和刷新令牌
func GenerateTokens(userID int) (string, string, error) {
	// 创建访问令牌
	accessToken := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"user_id":    userID,                                  // 获取用户ID
		"exp":        time.Now().Add(15 * time.Minute).Unix(), // 设置访问令牌过期时间为 15 分钟
		"token_type": "access_token",                          // 令牌类型为访问令牌
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
	})

	// 使用密钥签名刷新令牌
	refreshTokenString, err := refreshToken.SignedString(RefreshKey)
	if err != nil {
		return "", "", err
	}

	return accessTokenString, refreshTokenString, nil
}

func ValidateRefreshToken(tokenString string) (*jwt.Token, error) {
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
