package auth

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/LoveLosita/smartflow/backend/dao"
	"github.com/LoveLosita/smartflow/backend/model"
	"github.com/LoveLosita/smartflow/backend/respond"
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

type jwtRuntimeConfig struct {
	AccessKey     []byte
	RefreshKey    []byte
	AccessExpire  time.Duration
	RefreshExpire time.Duration
}

// AccessSigningKey 负责提供访问令牌签名/验签密钥。
// 职责边界：
// 1. 负责从运行时配置读取 accessSecret 并做空值校验。
// 2. 不负责 token 解析、业务鉴权与错误码映射。
// 3. 返回值语义：[]byte 为签名密钥；error 非空表示配置不可用。
func AccessSigningKey() ([]byte, error) {
	cfg, err := loadJWTConfig()
	if err != nil {
		return nil, err
	}
	return cfg.AccessKey, nil
}

// generateJTI 生成唯一的 JWT ID。
func generateJTI() string {
	return uuid.New().String()
}

// loadJWTConfig 负责聚合 JWT 运行时配置。
// 职责边界：
// 1. 负责读取密钥与过期时间配置，并转换为可直接使用的结构。
// 2. 不负责持久化配置，也不负责降级到“不安全默认密钥”。
// 3. 返回值语义：cfg 可直接用于签发/校验；error 非空表示配置不合法。
func loadJWTConfig() (*jwtRuntimeConfig, error) {
	accessKey, err := readJWTSecret(accessSecretConfigKey)
	if err != nil {
		return nil, err
	}
	refreshKey, err := readJWTSecret(refreshSecretConfigKey)
	if err != nil {
		return nil, err
	}

	accessExpire, err := readJWTExpireDuration(accessExpireConfigKey, defaultAccessTokenExpire)
	if err != nil {
		return nil, err
	}
	refreshExpire, err := readJWTExpireDuration(refreshExpireConfigKey, defaultRefreshTokenExpire)
	if err != nil {
		return nil, err
	}

	return &jwtRuntimeConfig{
		AccessKey:     accessKey,
		RefreshKey:    refreshKey,
		AccessExpire:  accessExpire,
		RefreshExpire: refreshExpire,
	}, nil
}

// readJWTSecret 负责读取并校验 JWT 密钥配置。
// 职责边界：
// 1. 负责“读配置 + 去空白 + 空值校验”。
// 2. 不负责任何默认值回退，避免静默使用弱配置。
// 3. 返回值语义：[]byte 为密钥；error 非空表示该配置项不可用。
func readJWTSecret(configKey string) ([]byte, error) {
	secret := strings.TrimSpace(viper.GetString(configKey))
	if secret == "" {
		return nil, fmt.Errorf("jwt 配置缺失: %s", configKey)
	}
	return []byte(secret), nil
}

// readJWTExpireDuration 负责读取并解析 JWT 过期时间配置。
// 职责边界：
// 1. 负责把字符串配置解析成 time.Duration，并保证结果大于 0。
// 2. 不负责签发 token；仅提供“可计算”的过期时长。
// 3. 返回值语义：duration 为最终时长；error 非空表示格式非法。
func readJWTExpireDuration(configKey string, fallback time.Duration) (time.Duration, error) {
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

// parseFlexibleDuration 负责解析项目内常见时长格式。
// 职责边界：
// 1. 负责兼容 Go 标准格式（如 15m、168h）与项目常见格式（如 15min、7d）。
// 2. 不负责读取配置键名；仅解析输入字符串。
// 3. 输入输出语义：raw 为原始时长文本；返回解析后的正时长或错误。
func parseFlexibleDuration(raw string) (time.Duration, error) {
	normalized := strings.ToLower(strings.TrimSpace(raw))
	if normalized == "" {
		return 0, errors.New("时长不能为空")
	}

	// 1. 先走 Go 原生解析，优先兼容标准写法（如 15m/168h）。
	if d, err := time.ParseDuration(normalized); err == nil {
		return d, nil
	}

	// 2. 原生解析失败后，兼容项目常见简写（如 15min、7d）。
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

// GenerateTokens 负责按配置签发访问令牌与刷新令牌。
// 职责边界：
// 1. 负责根据配置生成 exp，并签发 access/refresh 双 token。
// 2. 不负责登录鉴权（用户名/密码验证在 service 层处理）。
// 3. 返回值语义：第一个为 access token，第二个为 refresh token，error 非空表示签发失败。
func GenerateTokens(userID int) (string, string, error) {
	cfg, err := loadJWTConfig()
	if err != nil {
		return "", "", err
	}

	now := time.Now()
	sid := generateJTI()

	// 1. 先签 access token：短期有效，面向接口访问。
	accessToken := jwt.NewWithClaims(jwt.SigningMethodHS256, model.MyCustomClaims{
		UserID:    userID,
		TokenType: "access_token",
		Jti:       sid,
		RegisteredClaims: jwt.RegisteredClaims{
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(cfg.AccessExpire)),
		},
	})
	accessTokenString, err := accessToken.SignedString(cfg.AccessKey)
	if err != nil {
		return "", "", err
	}

	// 2. 再签 refresh token：长期有效，仅用于换发新 token。
	refreshToken := jwt.NewWithClaims(jwt.SigningMethodHS256, model.MyCustomClaims{
		UserID:    userID,
		TokenType: "refresh_token",
		Jti:       sid,
		RegisteredClaims: jwt.RegisteredClaims{
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(cfg.RefreshExpire)),
		},
	})
	refreshTokenString, err := refreshToken.SignedString(cfg.RefreshKey)
	if err != nil {
		return "", "", err
	}

	return accessTokenString, refreshTokenString, nil
}

// ValidateRefreshToken 验证刷新令牌的有效性，并增加 Redis 黑名单检查。
func ValidateRefreshToken(tokenString string, cache *dao.CacheDAO) (*jwt.Token, error) {
	cfg, err := loadJWTConfig()
	if err != nil {
		return nil, err
	}

	// 1. 解析 refresh token，并强制校验签名算法与密钥来源。
	token, err := jwt.ParseWithClaims(tokenString, &model.MyCustomClaims{}, func(token *jwt.Token) (interface{}, error) {
		if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, respond.InvalidTokenSingingMethod
		}
		return cfg.RefreshKey, nil
	})
	if err != nil {
		return nil, respond.InvalidRefreshToken
	}
	if !token.Valid {
		return nil, respond.InvalidRefreshToken
	}

	// 2. 断言 claims 类型，后续业务字段都从结构体读取。
	claims, ok := token.Claims.(*model.MyCustomClaims)
	if !ok {
		return nil, respond.InvalidClaims
	}

	// 3. 校验 token_type，防止把 access token 当 refresh token 用。
	if claims.TokenType != "refresh_token" {
		return nil, respond.WrongTokenType
	}

	// 4. 黑名单校验：签名合法也要确认 jti 未被主动注销。
	isBlack, err := cache.IsBlacklisted(claims.Jti)
	if err != nil {
		return nil, errors.New("无法验证令牌状态")
	}
	if isBlack {
		return nil, respond.UserLoggedOut
	}

	return token, nil
}
