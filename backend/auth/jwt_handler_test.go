package auth

import (
	"testing"
	"time"

	"github.com/LoveLosita/smartflow/backend/model"
	"github.com/golang-jwt/jwt/v4"
	"github.com/spf13/viper"
)

func TestParseFlexibleDuration(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name     string
		raw      string
		want     time.Duration
		wantFail bool
	}{
		{name: "标准格式", raw: "15m", want: 15 * time.Minute},
		{name: "项目分钟简写", raw: "15min", want: 15 * time.Minute},
		{name: "项目天简写", raw: "7d", want: 7 * 24 * time.Hour},
		{name: "非法格式", raw: "abc", wantFail: true},
	}

	for _, tc := range testCases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, err := parseFlexibleDuration(tc.raw)
			if tc.wantFail {
				if err == nil {
					t.Fatalf("期望解析失败，但得到成功: %s", tc.raw)
				}
				return
			}
			if err != nil {
				t.Fatalf("解析失败: %v", err)
			}
			if got != tc.want {
				t.Fatalf("解析结果不符合预期，got=%v want=%v", got, tc.want)
			}
		})
	}
}

func TestGenerateTokens_UseConfigExpire(t *testing.T) {
	const (
		accessSecret  = "unit-test-access-secret"
		refreshSecret = "unit-test-refresh-secret"
		accessExpire  = "2h"
		refreshExpire = "3d"
	)

	originAccessSecret := viper.GetString(accessSecretConfigKey)
	originRefreshSecret := viper.GetString(refreshSecretConfigKey)
	originAccessExpire := viper.GetString(accessExpireConfigKey)
	originRefreshExpire := viper.GetString(refreshExpireConfigKey)

	viper.Set(accessSecretConfigKey, accessSecret)
	viper.Set(refreshSecretConfigKey, refreshSecret)
	viper.Set(accessExpireConfigKey, accessExpire)
	viper.Set(refreshExpireConfigKey, refreshExpire)

	t.Cleanup(func() {
		viper.Set(accessSecretConfigKey, originAccessSecret)
		viper.Set(refreshSecretConfigKey, originRefreshSecret)
		viper.Set(accessExpireConfigKey, originAccessExpire)
		viper.Set(refreshExpireConfigKey, originRefreshExpire)
	})

	start := time.Now()
	accessTokenString, refreshTokenString, err := GenerateTokens(9527)
	if err != nil {
		t.Fatalf("签发 token 失败: %v", err)
	}

	accessClaims := parseTokenClaimsForTest(t, accessTokenString, []byte(accessSecret))
	refreshClaims := parseTokenClaimsForTest(t, refreshTokenString, []byte(refreshSecret))

	if accessClaims.TokenType != "access_token" {
		t.Fatalf("access token_type 不符合预期: %s", accessClaims.TokenType)
	}
	if refreshClaims.TokenType != "refresh_token" {
		t.Fatalf("refresh token_type 不符合预期: %s", refreshClaims.TokenType)
	}
	if accessClaims.Jti == "" || refreshClaims.Jti == "" {
		t.Fatalf("jti 不能为空")
	}
	if accessClaims.Jti != refreshClaims.Jti {
		t.Fatalf("access/refresh 应共享同一个 jti")
	}

	assertExpireNear(t, accessClaims.ExpiresAt.Time, start.Add(2*time.Hour), 3*time.Second)
	assertExpireNear(t, refreshClaims.ExpiresAt.Time, start.Add(3*24*time.Hour), 3*time.Second)
}

func parseTokenClaimsForTest(t *testing.T, tokenString string, key []byte) *model.MyCustomClaims {
	t.Helper()

	token, err := jwt.ParseWithClaims(tokenString, &model.MyCustomClaims{}, func(token *jwt.Token) (interface{}, error) {
		return key, nil
	})
	if err != nil {
		t.Fatalf("解析 token 失败: %v", err)
	}
	if !token.Valid {
		t.Fatalf("token 无效")
	}

	claims, ok := token.Claims.(*model.MyCustomClaims)
	if !ok {
		t.Fatalf("claims 类型断言失败")
	}
	return claims
}

func assertExpireNear(t *testing.T, got time.Time, want time.Time, tolerance time.Duration) {
	t.Helper()
	delta := got.Sub(want)
	if delta < 0 {
		delta = -delta
	}
	if delta > tolerance {
		t.Fatalf("exp 偏差超出容忍范围，got=%s want=%s delta=%s tolerance=%s", got.Format(time.RFC3339), want.Format(time.RFC3339), delta, tolerance)
	}
}
