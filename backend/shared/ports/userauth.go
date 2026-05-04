package ports

import (
	"context"

	contracts "github.com/LoveLosita/smartflow/backend/shared/contracts/userauth"
)

// UserCommandClient 是用户入口依赖的 user/auth 命令能力集合。
// 职责边界：
// 1. 只描述注册、登录、刷新、登出这些入口能力；
// 2. 不暴露 DAO、Redis、JWT 签发细节；
// 3. 具体通信协议由 adapter 实现，调用方只按 res, err 语义处理。
type UserCommandClient interface {
	Register(ctx context.Context, req contracts.RegisterRequest) (*contracts.RegisterResponse, error)
	Login(ctx context.Context, req contracts.LoginRequest) (*contracts.Tokens, error)
	RefreshToken(ctx context.Context, req contracts.RefreshTokenRequest) (*contracts.Tokens, error)
	Logout(ctx context.Context, accessToken string) error
}

// AccessTokenValidator 是边缘鉴权依赖的最小接口。
// 职责边界：只校验 access token 并返回后续业务所需的最小身份信息。
type AccessTokenValidator interface {
	ValidateAccessToken(ctx context.Context, accessToken string) (*contracts.ValidateAccessTokenResponse, error)
}

// TokenQuotaChecker 是 agent/chat 入口做额度门禁时依赖的最小接口。
// 职责边界：只判断当前用户是否允许继续消费 token，不负责 token 入账。
type TokenQuotaChecker interface {
	CheckTokenQuota(ctx context.Context, userID int) (*contracts.CheckTokenQuotaResponse, error)
}

// TokenUsageAdjuster 是业务链路回写 token 账本时依赖的最小接口。
// 职责边界：只做 token 账本增量调整，不承载鉴权与登录逻辑。
type TokenUsageAdjuster interface {
	AdjustTokenUsage(ctx context.Context, req contracts.AdjustTokenUsageRequest) (*contracts.CheckTokenQuotaResponse, error)
}

// UserAuthClient 组合当前阶段需要的 user/auth 能力。
// 职责边界：作为统一装配口径，避免 gateway 和 core service 各自维护一份接口。
type UserAuthClient interface {
	UserCommandClient
	AccessTokenValidator
	TokenQuotaChecker
	TokenUsageAdjuster
}
