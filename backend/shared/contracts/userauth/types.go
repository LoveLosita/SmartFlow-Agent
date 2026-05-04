package userauth

import "time"

// RegisterRequest 是 user/auth 服务对外暴露的注册契约。
// 职责边界：只描述跨进程请求字段，不承载校验、加密或持久化逻辑。
type RegisterRequest struct {
	Username    string `json:"username"`
	Password    string `json:"password"`
	PhoneNumber string `json:"phone_number"`
}

// RegisterResponse 是注册成功后的稳定响应契约。
type RegisterResponse struct {
	ID uint `json:"id"`
}

// LoginRequest 是登录请求契约。
type LoginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

// Tokens 保持历史接口 access_token / refresh_token 字段名不变。
type Tokens struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
}

// RefreshTokenRequest 保持历史 old_refresh_token 字段名不变。
type RefreshTokenRequest struct {
	RefreshToken string `json:"old_refresh_token"`
}

// ValidateAccessTokenRequest 是 gateway 调用 user/auth 做边缘鉴权时使用的内部契约。
type ValidateAccessTokenRequest struct {
	AccessToken string `json:"access_token"`
}

// ValidateAccessTokenResponse 返回 gateway 后续轻量组合所需的最小身份信息。
// Valid=false 预留给后续“软失败”兼容；当前实现遇到非法 token 会直接返回错误。
type ValidateAccessTokenResponse struct {
	Valid     bool      `json:"valid"`
	UserID    int       `json:"user_id"`
	TokenType string    `json:"token_type"`
	JTI       string    `json:"jti"`
	ExpiresAt time.Time `json:"expires_at"`
}

// CheckTokenQuotaRequest 是 agent/chat 进入业务前的额度门禁请求。
type CheckTokenQuotaRequest struct {
	UserID int `json:"user_id"`
}

// AdjustTokenUsageRequest 是业务链路回写用户 token 账本的请求。
type AdjustTokenUsageRequest struct {
	EventID    string `json:"event_id"`
	UserID     int    `json:"user_id"`
	TokenDelta int    `json:"token_delta"`
}

// CheckTokenQuotaResponse 返回额度门禁判断结果。
type CheckTokenQuotaResponse struct {
	Allowed     bool      `json:"allowed"`
	TokenLimit  int       `json:"token_limit"`
	TokenUsage  int       `json:"token_usage"`
	LastResetAt time.Time `json:"last_reset_at"`
}
