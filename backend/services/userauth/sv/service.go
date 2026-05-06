package sv

import (
	"context"
	"errors"
	"strings"
	"time"

	userauthauth "github.com/LoveLosita/smartflow/backend/services/userauth/internal/auth"
	userauthmodel "github.com/LoveLosita/smartflow/backend/services/userauth/model"
	contracts "github.com/LoveLosita/smartflow/backend/shared/contracts/userauth"
	"github.com/LoveLosita/smartflow/backend/shared/respond"
	"gorm.io/gorm"
)

type UserRepo interface {
	Create(ctx context.Context, username, phoneNumber, password string) (*userauthmodel.User, error)
	IfUsernameExists(ctx context.Context, name string) (bool, error)
	GetUserHashedPasswordByName(ctx context.Context, name string) (string, error)
	GetUserIDByName(ctx context.Context, name string) (int, error)
}

type CacheRepo interface {
	IsBlacklisted(jti string) (bool, error)
	SetBlacklist(jti string, expiration time.Duration) error
	SetBlacklistIfAbsent(jti string, expiration time.Duration) (bool, error)
	IsSessionBlacklisted(sessionID string) (bool, error)
	SetSessionBlacklist(sessionID string, expiration time.Duration) error
}

// Service 承载 user/auth 服务内部业务规则。
//
// 职责边界：
// 1. 负责注册、登录、刷新、登出、JWT 签发/校验和黑名单；
// 2. 不负责 Gin gateway 的响应适配、路由聚合和 SSE 等边缘职责；
// 3. 旧 token 额度门禁与记账能力已下线，不再由 userauth 承担计费相关职责。
type Service struct {
	userRepo  UserRepo
	cacheRepo CacheRepo
}

func New(userRepo UserRepo, cacheRepo CacheRepo) *Service {
	return &Service{
		userRepo:  userRepo,
		cacheRepo: cacheRepo,
	}
}

func (s *Service) Register(ctx context.Context, req contracts.RegisterRequest) (*contracts.RegisterResponse, error) {
	if strings.TrimSpace(req.Username) == "" || strings.TrimSpace(req.Password) == "" || strings.TrimSpace(req.PhoneNumber) == "" {
		return nil, respond.MissingParam
	}
	if len(req.Username) > 45 || len(req.Password) > 229 || len(req.PhoneNumber) > 18 {
		return nil, respond.ParamTooLong
	}

	exists, err := s.userRepo.IfUsernameExists(ctx, req.Username)
	if err != nil {
		return nil, err
	}
	if exists {
		return nil, respond.InvalidName
	}

	hashedPwd, err := userauthauth.HashPassword(req.Password)
	if err != nil {
		return nil, err
	}
	newUser, err := s.userRepo.Create(ctx, req.Username, req.PhoneNumber, hashedPwd)
	if err != nil {
		return nil, err
	}
	return &contracts.RegisterResponse{ID: newUser.ID}, nil
}

func (s *Service) Login(ctx context.Context, req contracts.LoginRequest) (*contracts.Tokens, error) {
	hashedPwd, err := s.userRepo.GetUserHashedPasswordByName(ctx, req.Username)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, respond.WrongName
		}
		return nil, err
	}

	matched, err := userauthauth.CompareHashPwdAndPwd(hashedPwd, req.Password)
	if err != nil {
		return nil, err
	}
	if !matched {
		return nil, respond.WrongPwd
	}

	userID, err := s.userRepo.GetUserIDByName(ctx, req.Username)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, respond.WrongName
		}
		return nil, err
	}
	return userauthauth.GenerateTokens(userID)
}

func (s *Service) RefreshToken(ctx context.Context, req contracts.RefreshTokenRequest) (*contracts.Tokens, error) {
	if strings.TrimSpace(req.RefreshToken) == "" {
		return nil, respond.MissingParam
	}
	claims, err := userauthauth.ValidateRefreshToken(req.RefreshToken, s.cacheRepo)
	if err != nil {
		return nil, err
	}

	ttl := time.Until(claims.ExpiresAt.Time)
	if ttl <= 0 {
		return nil, respond.InvalidRefreshToken
	}
	// 1. 先用 SET NX 抢占旧 refresh 的 JTI，确保并发刷新时只有一个请求能继续签发新 token。
	// 2. 这里只黑掉旧 refresh，不黑掉整个 session，避免误伤同一会话下新签发的 access token。
	consumed, err := s.cacheRepo.SetBlacklistIfAbsent(claims.JTI, ttl)
	if err != nil {
		return nil, err
	}
	if !consumed {
		return nil, respond.InvalidRefreshToken
	}

	return userauthauth.GenerateTokensWithSession(claims.UserID, claims.SessionID)
}

func (s *Service) LogoutByAccessToken(ctx context.Context, accessToken string) error {
	if strings.TrimSpace(accessToken) == "" {
		return respond.MissingToken
	}
	claims, err := userauthauth.ValidateAccessToken(accessToken, s.cacheRepo)
	if err != nil {
		return err
	}
	// 1. logout 的目标是整段会话，而不是单个 access token。
	// 2. 先按会话维度拉黑，再让 access / refresh 各自的 validate 流程拒绝后续请求。
	if strings.TrimSpace(claims.SessionID) == "" {
		return s.cacheRepo.SetBlacklist(claims.JTI, time.Until(claims.ExpiresAt.Time))
	}
	sessionTTL, err := userauthauth.SessionBlacklistTTL()
	if err != nil {
		return err
	}
	return s.cacheRepo.SetSessionBlacklist(claims.SessionID, sessionTTL)
}

func (s *Service) ValidateAccessToken(ctx context.Context, req contracts.ValidateAccessTokenRequest) (*contracts.ValidateAccessTokenResponse, error) {
	if strings.TrimSpace(req.AccessToken) == "" {
		return nil, respond.MissingToken
	}
	claims, err := userauthauth.ValidateAccessToken(req.AccessToken, s.cacheRepo)
	if err != nil {
		return nil, err
	}
	return &contracts.ValidateAccessTokenResponse{
		Valid:     true,
		UserID:    claims.UserID,
		TokenType: claims.TokenType,
		JTI:       claims.JTI,
		ExpiresAt: claims.ExpiresAt.Time,
	}, nil
}
