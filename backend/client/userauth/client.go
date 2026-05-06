package userauth

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/LoveLosita/smartflow/backend/services/userauth/rpc/pb"
	contracts "github.com/LoveLosita/smartflow/backend/shared/contracts/userauth"
	"github.com/zeromicro/go-zero/zrpc"
)

const (
	defaultEndpoint = "127.0.0.1:9081"
	defaultTimeout  = 2 * time.Second
)

type ClientConfig struct {
	Endpoints []string
	Target    string
	Timeout   time.Duration
}

// Client 是 gateway 侧 user/auth zrpc 的最小适配层。
//
// 职责边界：
// 1. 只负责跨进程 gRPC 调用和响应转译，不碰 DB / Redis / JWT 细节；
// 2. 服务端业务错误先通过 gRPC status 传输，再在这里反解回 respond.Response 风格；
// 3. 上层调用方仍然可以保持 `res, err :=` 的统一用法。
type Client struct {
	rpc pb.UserAuthClient
}

func NewClient(cfg ClientConfig) (*Client, error) {
	timeout := cfg.Timeout
	if timeout <= 0 {
		timeout = defaultTimeout
	}
	endpoints := normalizeEndpoints(cfg.Endpoints)
	target := strings.TrimSpace(cfg.Target)
	if len(endpoints) == 0 && target == "" {
		endpoints = []string{defaultEndpoint}
	}

	zclient, err := zrpc.NewClient(zrpc.RpcClientConf{
		Endpoints: endpoints,
		Target:    target,
		NonBlock:  true,
		Timeout:   int64(timeout / time.Millisecond),
	})
	if err != nil {
		return nil, err
	}
	return &Client{rpc: pb.NewUserAuthClient(zclient.Conn())}, nil
}

func (c *Client) Register(ctx context.Context, req contracts.RegisterRequest) (*contracts.RegisterResponse, error) {
	if err := c.ensureReady(); err != nil {
		return nil, err
	}
	resp, err := c.rpc.Register(ctx, &pb.RegisterRequest{
		Username:    req.Username,
		Password:    req.Password,
		PhoneNumber: req.PhoneNumber,
	})
	if err != nil {
		return nil, responseFromRPCError(err)
	}
	if resp == nil {
		return nil, errors.New("userauth zrpc service returned empty register response")
	}
	return &contracts.RegisterResponse{ID: uint(resp.Id)}, nil
}

func (c *Client) Login(ctx context.Context, req contracts.LoginRequest) (*contracts.Tokens, error) {
	if err := c.ensureReady(); err != nil {
		return nil, err
	}
	resp, err := c.rpc.Login(ctx, &pb.LoginRequest{
		Username: req.Username,
		Password: req.Password,
	})
	if err != nil {
		return nil, responseFromRPCError(err)
	}
	return tokensFromResponse(resp)
}

func (c *Client) RefreshToken(ctx context.Context, req contracts.RefreshTokenRequest) (*contracts.Tokens, error) {
	if err := c.ensureReady(); err != nil {
		return nil, err
	}
	resp, err := c.rpc.RefreshToken(ctx, &pb.RefreshTokenRequest{
		RefreshToken: req.RefreshToken,
	})
	if err != nil {
		return nil, responseFromRPCError(err)
	}
	return tokensFromResponse(resp)
}

func (c *Client) Logout(ctx context.Context, accessToken string) error {
	if err := c.ensureReady(); err != nil {
		return err
	}
	resp, err := c.rpc.Logout(ctx, &pb.LogoutRequest{
		AccessToken: accessToken,
	})
	if err != nil {
		return responseFromRPCError(err)
	}
	if resp == nil {
		return errors.New("userauth zrpc service returned empty logout response")
	}
	return nil
}

func (c *Client) ValidateAccessToken(ctx context.Context, accessToken string) (*contracts.ValidateAccessTokenResponse, error) {
	if err := c.ensureReady(); err != nil {
		return nil, err
	}
	resp, err := c.rpc.ValidateAccessToken(ctx, &pb.ValidateAccessTokenRequest{
		AccessToken: accessToken,
	})
	if err != nil {
		return nil, responseFromRPCError(err)
	}
	if resp == nil {
		return nil, errors.New("userauth zrpc service returned empty validate response")
	}
	return &contracts.ValidateAccessTokenResponse{
		Valid:     resp.Valid,
		UserID:    int(resp.UserId),
		TokenType: resp.TokenType,
		JTI:       resp.Jti,
		ExpiresAt: timeFromUnixNano(resp.ExpiresAtUnixNano),
	}, nil
}

func (c *Client) ensureReady() error {
	if c == nil || c.rpc == nil {
		return errors.New("userauth zrpc client is not initialized")
	}
	return nil
}

func tokensFromResponse(resp *pb.TokensResponse) (*contracts.Tokens, error) {
	if resp == nil {
		return nil, errors.New("userauth zrpc service returned empty token response")
	}
	return &contracts.Tokens{
		AccessToken:  resp.AccessToken,
		RefreshToken: resp.RefreshToken,
	}, nil
}

func normalizeEndpoints(values []string) []string {
	endpoints := make([]string, 0, len(values))
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed != "" {
			endpoints = append(endpoints, trimmed)
		}
	}
	return endpoints
}

func timeFromUnixNano(value int64) time.Time {
	if value <= 0 {
		return time.Time{}
	}
	return time.Unix(0, value)
}
