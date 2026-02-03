package model

import "github.com/golang-jwt/jwt/v4"

type Tokens struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
}

type MyCustomClaims struct {
	UserID               int    `json:"user_id"`
	TokenType            string `json:"token_type"`
	Jti                  string `json:"jti"`
	jwt.RegisteredClaims        // 包含 ExpiresAt, IssuedAt 等标准字段
}
