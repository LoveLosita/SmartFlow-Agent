package pb

import (
	context "context"

	grpc "google.golang.org/grpc"
	codes "google.golang.org/grpc/codes"
	status "google.golang.org/grpc/status"
)

const (
	UserAuth_Register_FullMethodName            = "/smartflow.userauth.UserAuth/Register"
	UserAuth_Login_FullMethodName               = "/smartflow.userauth.UserAuth/Login"
	UserAuth_RefreshToken_FullMethodName        = "/smartflow.userauth.UserAuth/RefreshToken"
	UserAuth_Logout_FullMethodName              = "/smartflow.userauth.UserAuth/Logout"
	UserAuth_ValidateAccessToken_FullMethodName = "/smartflow.userauth.UserAuth/ValidateAccessToken"
	UserAuth_CheckTokenQuota_FullMethodName     = "/smartflow.userauth.UserAuth/CheckTokenQuota"
	UserAuth_AdjustTokenUsage_FullMethodName    = "/smartflow.userauth.UserAuth/AdjustTokenUsage"
)

type UserAuthClient interface {
	Register(ctx context.Context, in *RegisterRequest, opts ...grpc.CallOption) (*RegisterResponse, error)
	Login(ctx context.Context, in *LoginRequest, opts ...grpc.CallOption) (*TokensResponse, error)
	RefreshToken(ctx context.Context, in *RefreshTokenRequest, opts ...grpc.CallOption) (*TokensResponse, error)
	Logout(ctx context.Context, in *LogoutRequest, opts ...grpc.CallOption) (*StatusResponse, error)
	ValidateAccessToken(ctx context.Context, in *ValidateAccessTokenRequest, opts ...grpc.CallOption) (*ValidateAccessTokenResponse, error)
	CheckTokenQuota(ctx context.Context, in *CheckTokenQuotaRequest, opts ...grpc.CallOption) (*CheckTokenQuotaResponse, error)
	AdjustTokenUsage(ctx context.Context, in *AdjustTokenUsageRequest, opts ...grpc.CallOption) (*CheckTokenQuotaResponse, error)
}

type userAuthClient struct {
	cc grpc.ClientConnInterface
}

func NewUserAuthClient(cc grpc.ClientConnInterface) UserAuthClient {
	return &userAuthClient{cc}
}

func (c *userAuthClient) Register(ctx context.Context, in *RegisterRequest, opts ...grpc.CallOption) (*RegisterResponse, error) {
	out := new(RegisterResponse)
	err := c.cc.Invoke(ctx, UserAuth_Register_FullMethodName, in, out, opts...)
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (c *userAuthClient) Login(ctx context.Context, in *LoginRequest, opts ...grpc.CallOption) (*TokensResponse, error) {
	out := new(TokensResponse)
	err := c.cc.Invoke(ctx, UserAuth_Login_FullMethodName, in, out, opts...)
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (c *userAuthClient) RefreshToken(ctx context.Context, in *RefreshTokenRequest, opts ...grpc.CallOption) (*TokensResponse, error) {
	out := new(TokensResponse)
	err := c.cc.Invoke(ctx, UserAuth_RefreshToken_FullMethodName, in, out, opts...)
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (c *userAuthClient) Logout(ctx context.Context, in *LogoutRequest, opts ...grpc.CallOption) (*StatusResponse, error) {
	out := new(StatusResponse)
	err := c.cc.Invoke(ctx, UserAuth_Logout_FullMethodName, in, out, opts...)
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (c *userAuthClient) ValidateAccessToken(ctx context.Context, in *ValidateAccessTokenRequest, opts ...grpc.CallOption) (*ValidateAccessTokenResponse, error) {
	out := new(ValidateAccessTokenResponse)
	err := c.cc.Invoke(ctx, UserAuth_ValidateAccessToken_FullMethodName, in, out, opts...)
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (c *userAuthClient) CheckTokenQuota(ctx context.Context, in *CheckTokenQuotaRequest, opts ...grpc.CallOption) (*CheckTokenQuotaResponse, error) {
	out := new(CheckTokenQuotaResponse)
	err := c.cc.Invoke(ctx, UserAuth_CheckTokenQuota_FullMethodName, in, out, opts...)
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (c *userAuthClient) AdjustTokenUsage(ctx context.Context, in *AdjustTokenUsageRequest, opts ...grpc.CallOption) (*CheckTokenQuotaResponse, error) {
	out := new(CheckTokenQuotaResponse)
	err := c.cc.Invoke(ctx, UserAuth_AdjustTokenUsage_FullMethodName, in, out, opts...)
	if err != nil {
		return nil, err
	}
	return out, nil
}

type UserAuthServer interface {
	Register(context.Context, *RegisterRequest) (*RegisterResponse, error)
	Login(context.Context, *LoginRequest) (*TokensResponse, error)
	RefreshToken(context.Context, *RefreshTokenRequest) (*TokensResponse, error)
	Logout(context.Context, *LogoutRequest) (*StatusResponse, error)
	ValidateAccessToken(context.Context, *ValidateAccessTokenRequest) (*ValidateAccessTokenResponse, error)
	CheckTokenQuota(context.Context, *CheckTokenQuotaRequest) (*CheckTokenQuotaResponse, error)
	AdjustTokenUsage(context.Context, *AdjustTokenUsageRequest) (*CheckTokenQuotaResponse, error)
}

type UnimplementedUserAuthServer struct{}

func (UnimplementedUserAuthServer) Register(context.Context, *RegisterRequest) (*RegisterResponse, error) {
	return nil, status.Errorf(codes.Unimplemented, "method Register not implemented")
}

func (UnimplementedUserAuthServer) Login(context.Context, *LoginRequest) (*TokensResponse, error) {
	return nil, status.Errorf(codes.Unimplemented, "method Login not implemented")
}

func (UnimplementedUserAuthServer) RefreshToken(context.Context, *RefreshTokenRequest) (*TokensResponse, error) {
	return nil, status.Errorf(codes.Unimplemented, "method RefreshToken not implemented")
}

func (UnimplementedUserAuthServer) Logout(context.Context, *LogoutRequest) (*StatusResponse, error) {
	return nil, status.Errorf(codes.Unimplemented, "method Logout not implemented")
}

func (UnimplementedUserAuthServer) ValidateAccessToken(context.Context, *ValidateAccessTokenRequest) (*ValidateAccessTokenResponse, error) {
	return nil, status.Errorf(codes.Unimplemented, "method ValidateAccessToken not implemented")
}

func (UnimplementedUserAuthServer) CheckTokenQuota(context.Context, *CheckTokenQuotaRequest) (*CheckTokenQuotaResponse, error) {
	return nil, status.Errorf(codes.Unimplemented, "method CheckTokenQuota not implemented")
}

func (UnimplementedUserAuthServer) AdjustTokenUsage(context.Context, *AdjustTokenUsageRequest) (*CheckTokenQuotaResponse, error) {
	return nil, status.Errorf(codes.Unimplemented, "method AdjustTokenUsage not implemented")
}

func RegisterUserAuthServer(s grpc.ServiceRegistrar, srv UserAuthServer) {
	s.RegisterService(&UserAuth_ServiceDesc, srv)
}

func _UserAuth_Register_Handler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpc.UnaryServerInterceptor) (interface{}, error) {
	in := new(RegisterRequest)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(UserAuthServer).Register(ctx, in)
	}
	info := &grpc.UnaryServerInfo{
		Server:     srv,
		FullMethod: UserAuth_Register_FullMethodName,
	}
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return srv.(UserAuthServer).Register(ctx, req.(*RegisterRequest))
	}
	return interceptor(ctx, in, info, handler)
}

func _UserAuth_Login_Handler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpc.UnaryServerInterceptor) (interface{}, error) {
	in := new(LoginRequest)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(UserAuthServer).Login(ctx, in)
	}
	info := &grpc.UnaryServerInfo{
		Server:     srv,
		FullMethod: UserAuth_Login_FullMethodName,
	}
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return srv.(UserAuthServer).Login(ctx, req.(*LoginRequest))
	}
	return interceptor(ctx, in, info, handler)
}

func _UserAuth_RefreshToken_Handler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpc.UnaryServerInterceptor) (interface{}, error) {
	in := new(RefreshTokenRequest)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(UserAuthServer).RefreshToken(ctx, in)
	}
	info := &grpc.UnaryServerInfo{
		Server:     srv,
		FullMethod: UserAuth_RefreshToken_FullMethodName,
	}
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return srv.(UserAuthServer).RefreshToken(ctx, req.(*RefreshTokenRequest))
	}
	return interceptor(ctx, in, info, handler)
}

func _UserAuth_Logout_Handler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpc.UnaryServerInterceptor) (interface{}, error) {
	in := new(LogoutRequest)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(UserAuthServer).Logout(ctx, in)
	}
	info := &grpc.UnaryServerInfo{
		Server:     srv,
		FullMethod: UserAuth_Logout_FullMethodName,
	}
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return srv.(UserAuthServer).Logout(ctx, req.(*LogoutRequest))
	}
	return interceptor(ctx, in, info, handler)
}

func _UserAuth_ValidateAccessToken_Handler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpc.UnaryServerInterceptor) (interface{}, error) {
	in := new(ValidateAccessTokenRequest)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(UserAuthServer).ValidateAccessToken(ctx, in)
	}
	info := &grpc.UnaryServerInfo{
		Server:     srv,
		FullMethod: UserAuth_ValidateAccessToken_FullMethodName,
	}
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return srv.(UserAuthServer).ValidateAccessToken(ctx, req.(*ValidateAccessTokenRequest))
	}
	return interceptor(ctx, in, info, handler)
}

func _UserAuth_CheckTokenQuota_Handler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpc.UnaryServerInterceptor) (interface{}, error) {
	in := new(CheckTokenQuotaRequest)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(UserAuthServer).CheckTokenQuota(ctx, in)
	}
	info := &grpc.UnaryServerInfo{
		Server:     srv,
		FullMethod: UserAuth_CheckTokenQuota_FullMethodName,
	}
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return srv.(UserAuthServer).CheckTokenQuota(ctx, req.(*CheckTokenQuotaRequest))
	}
	return interceptor(ctx, in, info, handler)
}

func _UserAuth_AdjustTokenUsage_Handler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpc.UnaryServerInterceptor) (interface{}, error) {
	in := new(AdjustTokenUsageRequest)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(UserAuthServer).AdjustTokenUsage(ctx, in)
	}
	info := &grpc.UnaryServerInfo{
		Server:     srv,
		FullMethod: UserAuth_AdjustTokenUsage_FullMethodName,
	}
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return srv.(UserAuthServer).AdjustTokenUsage(ctx, req.(*AdjustTokenUsageRequest))
	}
	return interceptor(ctx, in, info, handler)
}

var UserAuth_ServiceDesc = grpc.ServiceDesc{
	ServiceName: "smartflow.userauth.UserAuth",
	HandlerType: (*UserAuthServer)(nil),
	Methods: []grpc.MethodDesc{
		{
			MethodName: "Register",
			Handler:    _UserAuth_Register_Handler,
		},
		{
			MethodName: "Login",
			Handler:    _UserAuth_Login_Handler,
		},
		{
			MethodName: "RefreshToken",
			Handler:    _UserAuth_RefreshToken_Handler,
		},
		{
			MethodName: "Logout",
			Handler:    _UserAuth_Logout_Handler,
		},
		{
			MethodName: "ValidateAccessToken",
			Handler:    _UserAuth_ValidateAccessToken_Handler,
		},
		{
			MethodName: "CheckTokenQuota",
			Handler:    _UserAuth_CheckTokenQuota_Handler,
		},
		{
			MethodName: "AdjustTokenUsage",
			Handler:    _UserAuth_AdjustTokenUsage_Handler,
		},
	},
	Streams:  []grpc.StreamDesc{},
	Metadata: "services/userauth/rpc/userauth.proto",
}
