package auth

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	contracts "github.com/LoveLosita/smartflow/backend/shared/contracts/userauth"
	"github.com/LoveLosita/smartflow/backend/shared/respond"
	"github.com/golang-jwt/jwt/v4"
	"github.com/google/uuid"
	"github.com/spf13/viper"
)

const (
	accessSecretConfigKey  = "jwt.accessSecret"
	refreshSecretConfigKey = "jwt.refreshSecret"
	accessExpireConfigKey  = "jwt.accessTokenExpire"
	refreshExpireConfigKey = "jwt.refreshTokenExpire"

	defaultAccessTokenExpire  = 15 * time.Minute
	defaultRefreshTokenExpire = 7 * 24 * time.Hour
)

type BlacklistReader interface {
	IsBlacklisted(jti string) (bool, error)
	IsSessionBlacklisted(sessionID string) (bool, error)
}

type runtimeConfig struct {
	AccessKey     []byte
	RefreshKey    []byte
	AccessExpire  time.Duration
	RefreshExpire time.Duration
}

// Claims 是 user/auth 服务内部使用的 JWT 声明结构。
type Claims struct {
	UserID               int    `json:"user_id"`
	SessionID            string `json:"sid"`
	TokenType            string `json:"token_type"`
	JTI                  string `json:"jti"`
	jwt.RegisteredClaims        // 标准字段包含 exp/iat 等时间声明。
}

func generateJTI() string {
	return uuid.New().String()
}

func loadConfig() (*runtimeConfig, error) {
	accessKey, err := readSecret(accessSecretConfigKey)
	if err != nil {
		return nil, err
	}
	refreshKey, err := readSecret(refreshSecretConfigKey)
	if err != nil {
		return nil, err
	}
	accessExpire, err := readExpireDuration(accessExpireConfigKey, defaultAccessTokenExpire)
	if err != nil {
		return nil, err
	}
	refreshExpire, err := readExpireDuration(refreshExpireConfigKey, defaultRefreshTokenExpire)
	if err != nil {
		return nil, err
	}
	return &runtimeConfig{
		AccessKey:     accessKey,
		RefreshKey:    refreshKey,
		AccessExpire:  accessExpire,
		RefreshExpire: refreshExpire,
	}, nil
}

func readSecret(configKey string) ([]byte, error) {
	secret := strings.TrimSpace(viper.GetString(configKey))
	if secret == "" {
		return nil, fmt.Errorf("jwt 配置缺失: %s", configKey)
	}
	return []byte(secret), nil
}

func readExpireDuration(configKey string, fallback time.Duration) (time.Duration, error) {
	raw := strings.TrimSpace(viper.GetString(configKey))
	if raw == "" {
		return fallback, nil
	}
	d, err := parseFlexibleDuration(raw)
	if err != nil {
		return 0, fmt.Errorf("jwt 配置项 %s 非法: %w", configKey, err)
	}
	if d <= 0 {
		return 0, fmt.Errorf("jwt 配置项 %s 必须大于 0", configKey)
	}
	return d, nil
}

// SessionBlacklistTTL 返回 logout 后会话黑名单需要保留的时长。
//
// 职责边界：
// 1. 只负责从配置推导“会话级黑名单”保留多久；
// 2. 不负责写 Redis，也不负责判断具体 token 是否过期；
// 3. 取 access / refresh 中更长的有效期，避免旧 access 在 refresh 轮转后把整段会话放掉。
func SessionBlacklistTTL() (time.Duration, error) {
	cfg, err := loadConfig()
	if err != nil {
		return 0, err
	}
	if cfg.RefreshExpire >= cfg.AccessExpire {
		return cfg.RefreshExpire, nil
	}
	return cfg.AccessExpire, nil
}

// parseFlexibleDuration 兼容 Go 原生时长和项目历史配置中的 7d / 15min。
func parseFlexibleDuration(raw string) (time.Duration, error) {
	normalized := strings.ToLower(strings.TrimSpace(raw))
	if normalized == "" {
		return 0, errors.New("时长不能为空")
	}
	if d, err := time.ParseDuration(normalized); err == nil {
		return d, nil
	}

	type unitDef struct {
		Suffix     string
		Multiplier time.Duration
	}
	unitDefs := []unitDef{
		{Suffix: "minutes", Multiplier: time.Minute},
		{Suffix: "minute", Multiplier: time.Minute},
		{Suffix: "mins", Multiplier: time.Minute},
		{Suffix: "min", Multiplier: time.Minute},
		{Suffix: "days", Multiplier: 24 * time.Hour},
		{Suffix: "day", Multiplier: 24 * time.Hour},
		{Suffix: "d", Multiplier: 24 * time.Hour},
		{Suffix: "hours", Multiplier: time.Hour},
		{Suffix: "hour", Multiplier: time.Hour},
		{Suffix: "h", Multiplier: time.Hour},
		{Suffix: "seconds", Multiplier: time.Second},
		{Suffix: "second", Multiplier: time.Second},
		{Suffix: "secs", Multiplier: time.Second},
		{Suffix: "sec", Multiplier: time.Second},
		{Suffix: "m", Multiplier: time.Minute},
		{Suffix: "s", Multiplier: time.Second},
	}

	for _, unit := range unitDefs {
		if !strings.HasSuffix(normalized, unit.Suffix) {
			continue
		}
		numberPart := strings.TrimSpace(strings.TrimSuffix(normalized, unit.Suffix))
		value, err := strconv.Atoi(numberPart)
		if err != nil {
			return 0, fmt.Errorf("时长数值非法: %q", numberPart)
		}
		if value <= 0 {
			return 0, fmt.Errorf("时长数值必须大于 0: %d", value)
		}
		return time.Duration(value) * unit.Multiplier, nil
	}
	return 0, fmt.Errorf("不支持的时长格式: %s", raw)
}

// GenerateTokens 签发访问令牌与刷新令牌。
func GenerateTokens(userID int) (*contracts.Tokens, error) {
	return GenerateTokensWithSession(userID, "")
}

// GenerateTokensWithSession 为同一个登录会话签发一对 access / refresh token。
//
// 职责边界：
// 1. 负责生成新的会话标识，或复用传入的会话标识；
// 2. access / refresh 各自使用独立 JTI，避免 refresh 轮转时误伤新 access；
// 3. 不负责黑名单写入，黑名单由 logout / refresh 重放防护链路处理。
func GenerateTokensWithSession(userID int, sessionID string) (*contracts.Tokens, error) {
	cfg, err := loadConfig()
	if err != nil {
		return nil, err
	}

	now := time.Now()
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		sessionID = generateJTI()
	}
	accessJTI := generateJTI()
	refreshJTI := generateJTI()

	accessToken := jwt.NewWithClaims(jwt.SigningMethodHS256, Claims{
		UserID:    userID,
		SessionID: sessionID,
		TokenType: "access_token",
		JTI:       accessJTI,
		RegisteredClaims: jwt.RegisteredClaims{
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(cfg.AccessExpire)),
		},
	})
	accessTokenString, err := accessToken.SignedString(cfg.AccessKey)
	if err != nil {
		return nil, err
	}

	refreshToken := jwt.NewWithClaims(jwt.SigningMethodHS256, Claims{
		UserID:    userID,
		SessionID: sessionID,
		TokenType: "refresh_token",
		JTI:       refreshJTI,
		RegisteredClaims: jwt.RegisteredClaims{
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(cfg.RefreshExpire)),
		},
	})
	refreshTokenString, err := refreshToken.SignedString(cfg.RefreshKey)
	if err != nil {
		return nil, err
	}

	return &contracts.Tokens{
		AccessToken:  accessTokenString,
		RefreshToken: refreshTokenString,
	}, nil
}

func parseToken(tokenString string, signingKey []byte) (*Claims, error) {
	token, err := jwt.ParseWithClaims(tokenString, &Claims{}, func(token *jwt.Token) (interface{}, error) {
		if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, respond.InvalidTokenSingingMethod
		}
		return signingKey, nil
	})
	if err != nil || !token.Valid {
		return nil, respond.InvalidToken
	}
	claims, ok := token.Claims.(*Claims)
	if !ok || claims.ExpiresAt == nil || claims.UserID <= 0 || strings.TrimSpace(claims.JTI) == "" {
		return nil, respond.InvalidClaims
	}
	return claims, nil
}

func ensureAccessActive(cache BlacklistReader, sessionID, jti string) error {
	if cache == nil {
		return errors.New("token blacklist dependency is not initialized")
	}
	sessionID = strings.TrimSpace(sessionID)
	if sessionID != "" {
		isBlack, err := cache.IsSessionBlacklisted(sessionID)
		if err != nil {
			return errors.New("无法验证令牌状态")
		}
		if isBlack {
			return respond.UserLoggedOut
		}
		return nil
	}
	isBlack, err := cache.IsBlacklisted(jti)
	if err != nil {
		return errors.New("无法验证令牌状态")
	}
	if isBlack {
		return respond.UserLoggedOut
	}
	return nil
}

func ensureRefreshActive(cache BlacklistReader, sessionID, jti string) error {
	if cache == nil {
		return errors.New("token blacklist dependency is not initialized")
	}
	sessionID = strings.TrimSpace(sessionID)
	if sessionID != "" {
		isBlack, err := cache.IsSessionBlacklisted(sessionID)
		if err != nil {
			return errors.New("无法验证令牌状态")
		}
		if isBlack {
			return respond.UserLoggedOut
		}
	}
	isBlack, err := cache.IsBlacklisted(jti)
	if err != nil {
		return errors.New("无法验证令牌状态")
	}
	if isBlack {
		return respond.InvalidRefreshToken
	}
	return nil
}

// ValidateAccessToken 校验 access token，并统一检查黑名单。
func ValidateAccessToken(tokenString string, cache BlacklistReader) (*Claims, error) {
	cfg, err := loadConfig()
	if err != nil {
		return nil, err
	}
	claims, err := parseToken(tokenString, cfg.AccessKey)
	if err != nil {
		return nil, err
	}
	if claims.TokenType != "access_token" {
		return nil, respond.WrongTokenType
	}
	if err = ensureAccessActive(cache, claims.SessionID, claims.JTI); err != nil {
		return nil, err
	}
	return claims, nil
}

// ValidateRefreshToken 校验 refresh token，并统一检查黑名单。
func ValidateRefreshToken(tokenString string, cache BlacklistReader) (*Claims, error) {
	cfg, err := loadConfig()
	if err != nil {
		return nil, err
	}
	claims, err := parseToken(tokenString, cfg.RefreshKey)
	if err != nil {
		return nil, respond.InvalidRefreshToken
	}
	if claims.TokenType != "refresh_token" {
		return nil, respond.WrongTokenType
	}
	if err = ensureRefreshActive(cache, claims.SessionID, claims.JTI); err != nil {
		return nil, err
	}
	return claims, nil
}
