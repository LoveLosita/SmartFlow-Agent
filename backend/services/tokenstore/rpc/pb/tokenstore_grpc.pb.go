package pb

import (
	context "context"

	grpc "google.golang.org/grpc"
	codes "google.golang.org/grpc/codes"
	status "google.golang.org/grpc/status"
)

const (
	TokenStoreService_GetSummary_FullMethodName             = "/smartflow.tokenstore.TokenStoreService/GetSummary"
	TokenStoreService_ListProducts_FullMethodName           = "/smartflow.tokenstore.TokenStoreService/ListProducts"
	TokenStoreService_CreateOrder_FullMethodName            = "/smartflow.tokenstore.TokenStoreService/CreateOrder"
	TokenStoreService_ListOrders_FullMethodName             = "/smartflow.tokenstore.TokenStoreService/ListOrders"
	TokenStoreService_GetOrder_FullMethodName               = "/smartflow.tokenstore.TokenStoreService/GetOrder"
	TokenStoreService_MockPaidOrder_FullMethodName          = "/smartflow.tokenstore.TokenStoreService/MockPaidOrder"
	TokenStoreService_ListGrants_FullMethodName             = "/smartflow.tokenstore.TokenStoreService/ListGrants"
	TokenStoreService_RecordForumRewardGrant_FullMethodName = "/smartflow.tokenstore.TokenStoreService/RecordForumRewardGrant"
)

type TokenStoreServiceClient interface {
	GetSummary(ctx context.Context, in *GetTokenSummaryRequest, opts ...grpc.CallOption) (*GetTokenSummaryResponse, error)
	ListProducts(ctx context.Context, in *ListTokenProductsRequest, opts ...grpc.CallOption) (*ListTokenProductsResponse, error)
	CreateOrder(ctx context.Context, in *CreateTokenOrderRequest, opts ...grpc.CallOption) (*CreateTokenOrderResponse, error)
	ListOrders(ctx context.Context, in *ListTokenOrdersRequest, opts ...grpc.CallOption) (*ListTokenOrdersResponse, error)
	GetOrder(ctx context.Context, in *GetTokenOrderRequest, opts ...grpc.CallOption) (*GetTokenOrderResponse, error)
	MockPaidOrder(ctx context.Context, in *MockPaidOrderRequest, opts ...grpc.CallOption) (*MockPaidOrderResponse, error)
	ListGrants(ctx context.Context, in *ListTokenGrantsRequest, opts ...grpc.CallOption) (*ListTokenGrantsResponse, error)
	RecordForumRewardGrant(ctx context.Context, in *RecordForumRewardGrantRequest, opts ...grpc.CallOption) (*RecordForumRewardGrantResponse, error)
}

type tokenStoreServiceClient struct {
	cc grpc.ClientConnInterface
}

func NewTokenStoreServiceClient(cc grpc.ClientConnInterface) TokenStoreServiceClient {
	return &tokenStoreServiceClient{cc}
}

func (c *tokenStoreServiceClient) GetSummary(ctx context.Context, in *GetTokenSummaryRequest, opts ...grpc.CallOption) (*GetTokenSummaryResponse, error) {
	return invokeTokenStore[GetTokenSummaryResponse](ctx, c.cc, TokenStoreService_GetSummary_FullMethodName, in, opts...)
}

func (c *tokenStoreServiceClient) ListProducts(ctx context.Context, in *ListTokenProductsRequest, opts ...grpc.CallOption) (*ListTokenProductsResponse, error) {
	return invokeTokenStore[ListTokenProductsResponse](ctx, c.cc, TokenStoreService_ListProducts_FullMethodName, in, opts...)
}

func (c *tokenStoreServiceClient) CreateOrder(ctx context.Context, in *CreateTokenOrderRequest, opts ...grpc.CallOption) (*CreateTokenOrderResponse, error) {
	return invokeTokenStore[CreateTokenOrderResponse](ctx, c.cc, TokenStoreService_CreateOrder_FullMethodName, in, opts...)
}

func (c *tokenStoreServiceClient) ListOrders(ctx context.Context, in *ListTokenOrdersRequest, opts ...grpc.CallOption) (*ListTokenOrdersResponse, error) {
	return invokeTokenStore[ListTokenOrdersResponse](ctx, c.cc, TokenStoreService_ListOrders_FullMethodName, in, opts...)
}

func (c *tokenStoreServiceClient) GetOrder(ctx context.Context, in *GetTokenOrderRequest, opts ...grpc.CallOption) (*GetTokenOrderResponse, error) {
	return invokeTokenStore[GetTokenOrderResponse](ctx, c.cc, TokenStoreService_GetOrder_FullMethodName, in, opts...)
}

func (c *tokenStoreServiceClient) MockPaidOrder(ctx context.Context, in *MockPaidOrderRequest, opts ...grpc.CallOption) (*MockPaidOrderResponse, error) {
	return invokeTokenStore[MockPaidOrderResponse](ctx, c.cc, TokenStoreService_MockPaidOrder_FullMethodName, in, opts...)
}

func (c *tokenStoreServiceClient) ListGrants(ctx context.Context, in *ListTokenGrantsRequest, opts ...grpc.CallOption) (*ListTokenGrantsResponse, error) {
	return invokeTokenStore[ListTokenGrantsResponse](ctx, c.cc, TokenStoreService_ListGrants_FullMethodName, in, opts...)
}

func (c *tokenStoreServiceClient) RecordForumRewardGrant(ctx context.Context, in *RecordForumRewardGrantRequest, opts ...grpc.CallOption) (*RecordForumRewardGrantResponse, error) {
	return invokeTokenStore[RecordForumRewardGrantResponse](ctx, c.cc, TokenStoreService_RecordForumRewardGrant_FullMethodName, in, opts...)
}

func invokeTokenStore[Resp any](ctx context.Context, cc grpc.ClientConnInterface, fullMethod string, in interface{}, opts ...grpc.CallOption) (*Resp, error) {
	out := new(Resp)
	err := cc.Invoke(ctx, fullMethod, in, out, opts...)
	if err != nil {
		return nil, err
	}
	return out, nil
}

type TokenStoreServiceServer interface {
	GetSummary(context.Context, *GetTokenSummaryRequest) (*GetTokenSummaryResponse, error)
	ListProducts(context.Context, *ListTokenProductsRequest) (*ListTokenProductsResponse, error)
	CreateOrder(context.Context, *CreateTokenOrderRequest) (*CreateTokenOrderResponse, error)
	ListOrders(context.Context, *ListTokenOrdersRequest) (*ListTokenOrdersResponse, error)
	GetOrder(context.Context, *GetTokenOrderRequest) (*GetTokenOrderResponse, error)
	MockPaidOrder(context.Context, *MockPaidOrderRequest) (*MockPaidOrderResponse, error)
	ListGrants(context.Context, *ListTokenGrantsRequest) (*ListTokenGrantsResponse, error)
	RecordForumRewardGrant(context.Context, *RecordForumRewardGrantRequest) (*RecordForumRewardGrantResponse, error)
}

type UnimplementedTokenStoreServiceServer struct{}

func (UnimplementedTokenStoreServiceServer) GetSummary(context.Context, *GetTokenSummaryRequest) (*GetTokenSummaryResponse, error) {
	return nil, status.Errorf(codes.Unimplemented, "method GetSummary not implemented")
}

func (UnimplementedTokenStoreServiceServer) ListProducts(context.Context, *ListTokenProductsRequest) (*ListTokenProductsResponse, error) {
	return nil, status.Errorf(codes.Unimplemented, "method ListProducts not implemented")
}

func (UnimplementedTokenStoreServiceServer) CreateOrder(context.Context, *CreateTokenOrderRequest) (*CreateTokenOrderResponse, error) {
	return nil, status.Errorf(codes.Unimplemented, "method CreateOrder not implemented")
}

func (UnimplementedTokenStoreServiceServer) ListOrders(context.Context, *ListTokenOrdersRequest) (*ListTokenOrdersResponse, error) {
	return nil, status.Errorf(codes.Unimplemented, "method ListOrders not implemented")
}

func (UnimplementedTokenStoreServiceServer) GetOrder(context.Context, *GetTokenOrderRequest) (*GetTokenOrderResponse, error) {
	return nil, status.Errorf(codes.Unimplemented, "method GetOrder not implemented")
}

func (UnimplementedTokenStoreServiceServer) MockPaidOrder(context.Context, *MockPaidOrderRequest) (*MockPaidOrderResponse, error) {
	return nil, status.Errorf(codes.Unimplemented, "method MockPaidOrder not implemented")
}

func (UnimplementedTokenStoreServiceServer) ListGrants(context.Context, *ListTokenGrantsRequest) (*ListTokenGrantsResponse, error) {
	return nil, status.Errorf(codes.Unimplemented, "method ListGrants not implemented")
}

func (UnimplementedTokenStoreServiceServer) RecordForumRewardGrant(context.Context, *RecordForumRewardGrantRequest) (*RecordForumRewardGrantResponse, error) {
	return nil, status.Errorf(codes.Unimplemented, "method RecordForumRewardGrant not implemented")
}

func RegisterTokenStoreServiceServer(s grpc.ServiceRegistrar, srv TokenStoreServiceServer) {
	s.RegisterService(&TokenStoreService_ServiceDesc, srv)
}

func tokenStoreUnaryHandler[Req any](methodName string, fullMethod string, invoke func(TokenStoreServiceServer, context.Context, *Req) (interface{}, error)) grpc.MethodDesc {
	return grpc.MethodDesc{
		MethodName: methodName,
		Handler: func(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpc.UnaryServerInterceptor) (interface{}, error) {
			in := new(Req)
			if err := dec(in); err != nil {
				return nil, err
			}
			if interceptor == nil {
				return invoke(srv.(TokenStoreServiceServer), ctx, in)
			}
			info := &grpc.UnaryServerInfo{
				Server:     srv,
				FullMethod: fullMethod,
			}
			handler := func(ctx context.Context, req interface{}) (interface{}, error) {
				return invoke(srv.(TokenStoreServiceServer), ctx, req.(*Req))
			}
			return interceptor(ctx, in, info, handler)
		},
	}
}

var TokenStoreService_ServiceDesc = grpc.ServiceDesc{
	ServiceName: "smartflow.tokenstore.TokenStoreService",
	HandlerType: (*TokenStoreServiceServer)(nil),
	Methods: []grpc.MethodDesc{
		tokenStoreUnaryHandler[GetTokenSummaryRequest]("GetSummary", TokenStoreService_GetSummary_FullMethodName, func(s TokenStoreServiceServer, ctx context.Context, req *GetTokenSummaryRequest) (interface{}, error) {
			return s.GetSummary(ctx, req)
		}),
		tokenStoreUnaryHandler[ListTokenProductsRequest]("ListProducts", TokenStoreService_ListProducts_FullMethodName, func(s TokenStoreServiceServer, ctx context.Context, req *ListTokenProductsRequest) (interface{}, error) {
			return s.ListProducts(ctx, req)
		}),
		tokenStoreUnaryHandler[CreateTokenOrderRequest]("CreateOrder", TokenStoreService_CreateOrder_FullMethodName, func(s TokenStoreServiceServer, ctx context.Context, req *CreateTokenOrderRequest) (interface{}, error) {
			return s.CreateOrder(ctx, req)
		}),
		tokenStoreUnaryHandler[ListTokenOrdersRequest]("ListOrders", TokenStoreService_ListOrders_FullMethodName, func(s TokenStoreServiceServer, ctx context.Context, req *ListTokenOrdersRequest) (interface{}, error) {
			return s.ListOrders(ctx, req)
		}),
		tokenStoreUnaryHandler[GetTokenOrderRequest]("GetOrder", TokenStoreService_GetOrder_FullMethodName, func(s TokenStoreServiceServer, ctx context.Context, req *GetTokenOrderRequest) (interface{}, error) {
			return s.GetOrder(ctx, req)
		}),
		tokenStoreUnaryHandler[MockPaidOrderRequest]("MockPaidOrder", TokenStoreService_MockPaidOrder_FullMethodName, func(s TokenStoreServiceServer, ctx context.Context, req *MockPaidOrderRequest) (interface{}, error) {
			return s.MockPaidOrder(ctx, req)
		}),
		tokenStoreUnaryHandler[ListTokenGrantsRequest]("ListGrants", TokenStoreService_ListGrants_FullMethodName, func(s TokenStoreServiceServer, ctx context.Context, req *ListTokenGrantsRequest) (interface{}, error) {
			return s.ListGrants(ctx, req)
		}),
		tokenStoreUnaryHandler[RecordForumRewardGrantRequest]("RecordForumRewardGrant", TokenStoreService_RecordForumRewardGrant_FullMethodName, func(s TokenStoreServiceServer, ctx context.Context, req *RecordForumRewardGrantRequest) (interface{}, error) {
			return s.RecordForumRewardGrant(ctx, req)
		}),
	},
	Streams:  []grpc.StreamDesc{},
	Metadata: "tokenstore.proto",
}
