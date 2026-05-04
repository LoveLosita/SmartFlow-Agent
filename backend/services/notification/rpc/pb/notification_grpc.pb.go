package pb

import (
	context "context"

	grpc "google.golang.org/grpc"
	codes "google.golang.org/grpc/codes"
	status "google.golang.org/grpc/status"
)

const (
	Notification_GetFeishuWebhook_FullMethodName    = "/smartflow.notification.Notification/GetFeishuWebhook"
	Notification_SaveFeishuWebhook_FullMethodName   = "/smartflow.notification.Notification/SaveFeishuWebhook"
	Notification_DeleteFeishuWebhook_FullMethodName = "/smartflow.notification.Notification/DeleteFeishuWebhook"
	Notification_TestFeishuWebhook_FullMethodName   = "/smartflow.notification.Notification/TestFeishuWebhook"
)

type NotificationClient interface {
	GetFeishuWebhook(ctx context.Context, in *GetFeishuWebhookRequest, opts ...grpc.CallOption) (*ChannelResponse, error)
	SaveFeishuWebhook(ctx context.Context, in *SaveFeishuWebhookRequest, opts ...grpc.CallOption) (*ChannelResponse, error)
	DeleteFeishuWebhook(ctx context.Context, in *DeleteFeishuWebhookRequest, opts ...grpc.CallOption) (*StatusResponse, error)
	TestFeishuWebhook(ctx context.Context, in *TestFeishuWebhookRequest, opts ...grpc.CallOption) (*TestResult, error)
}

type notificationClient struct {
	cc grpc.ClientConnInterface
}

func NewNotificationClient(cc grpc.ClientConnInterface) NotificationClient {
	return &notificationClient{cc}
}

func (c *notificationClient) GetFeishuWebhook(ctx context.Context, in *GetFeishuWebhookRequest, opts ...grpc.CallOption) (*ChannelResponse, error) {
	out := new(ChannelResponse)
	err := c.cc.Invoke(ctx, Notification_GetFeishuWebhook_FullMethodName, in, out, opts...)
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (c *notificationClient) SaveFeishuWebhook(ctx context.Context, in *SaveFeishuWebhookRequest, opts ...grpc.CallOption) (*ChannelResponse, error) {
	out := new(ChannelResponse)
	err := c.cc.Invoke(ctx, Notification_SaveFeishuWebhook_FullMethodName, in, out, opts...)
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (c *notificationClient) DeleteFeishuWebhook(ctx context.Context, in *DeleteFeishuWebhookRequest, opts ...grpc.CallOption) (*StatusResponse, error) {
	out := new(StatusResponse)
	err := c.cc.Invoke(ctx, Notification_DeleteFeishuWebhook_FullMethodName, in, out, opts...)
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (c *notificationClient) TestFeishuWebhook(ctx context.Context, in *TestFeishuWebhookRequest, opts ...grpc.CallOption) (*TestResult, error) {
	out := new(TestResult)
	err := c.cc.Invoke(ctx, Notification_TestFeishuWebhook_FullMethodName, in, out, opts...)
	if err != nil {
		return nil, err
	}
	return out, nil
}

type NotificationServer interface {
	GetFeishuWebhook(context.Context, *GetFeishuWebhookRequest) (*ChannelResponse, error)
	SaveFeishuWebhook(context.Context, *SaveFeishuWebhookRequest) (*ChannelResponse, error)
	DeleteFeishuWebhook(context.Context, *DeleteFeishuWebhookRequest) (*StatusResponse, error)
	TestFeishuWebhook(context.Context, *TestFeishuWebhookRequest) (*TestResult, error)
}

type UnimplementedNotificationServer struct{}

func (UnimplementedNotificationServer) GetFeishuWebhook(context.Context, *GetFeishuWebhookRequest) (*ChannelResponse, error) {
	return nil, status.Errorf(codes.Unimplemented, "method GetFeishuWebhook not implemented")
}

func (UnimplementedNotificationServer) SaveFeishuWebhook(context.Context, *SaveFeishuWebhookRequest) (*ChannelResponse, error) {
	return nil, status.Errorf(codes.Unimplemented, "method SaveFeishuWebhook not implemented")
}

func (UnimplementedNotificationServer) DeleteFeishuWebhook(context.Context, *DeleteFeishuWebhookRequest) (*StatusResponse, error) {
	return nil, status.Errorf(codes.Unimplemented, "method DeleteFeishuWebhook not implemented")
}

func (UnimplementedNotificationServer) TestFeishuWebhook(context.Context, *TestFeishuWebhookRequest) (*TestResult, error) {
	return nil, status.Errorf(codes.Unimplemented, "method TestFeishuWebhook not implemented")
}

func RegisterNotificationServer(s grpc.ServiceRegistrar, srv NotificationServer) {
	s.RegisterService(&Notification_ServiceDesc, srv)
}

func _Notification_GetFeishuWebhook_Handler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpc.UnaryServerInterceptor) (interface{}, error) {
	in := new(GetFeishuWebhookRequest)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(NotificationServer).GetFeishuWebhook(ctx, in)
	}
	info := &grpc.UnaryServerInfo{
		Server:     srv,
		FullMethod: Notification_GetFeishuWebhook_FullMethodName,
	}
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return srv.(NotificationServer).GetFeishuWebhook(ctx, req.(*GetFeishuWebhookRequest))
	}
	return interceptor(ctx, in, info, handler)
}

func _Notification_SaveFeishuWebhook_Handler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpc.UnaryServerInterceptor) (interface{}, error) {
	in := new(SaveFeishuWebhookRequest)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(NotificationServer).SaveFeishuWebhook(ctx, in)
	}
	info := &grpc.UnaryServerInfo{
		Server:     srv,
		FullMethod: Notification_SaveFeishuWebhook_FullMethodName,
	}
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return srv.(NotificationServer).SaveFeishuWebhook(ctx, req.(*SaveFeishuWebhookRequest))
	}
	return interceptor(ctx, in, info, handler)
}

func _Notification_DeleteFeishuWebhook_Handler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpc.UnaryServerInterceptor) (interface{}, error) {
	in := new(DeleteFeishuWebhookRequest)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(NotificationServer).DeleteFeishuWebhook(ctx, in)
	}
	info := &grpc.UnaryServerInfo{
		Server:     srv,
		FullMethod: Notification_DeleteFeishuWebhook_FullMethodName,
	}
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return srv.(NotificationServer).DeleteFeishuWebhook(ctx, req.(*DeleteFeishuWebhookRequest))
	}
	return interceptor(ctx, in, info, handler)
}

func _Notification_TestFeishuWebhook_Handler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpc.UnaryServerInterceptor) (interface{}, error) {
	in := new(TestFeishuWebhookRequest)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(NotificationServer).TestFeishuWebhook(ctx, in)
	}
	info := &grpc.UnaryServerInfo{
		Server:     srv,
		FullMethod: Notification_TestFeishuWebhook_FullMethodName,
	}
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return srv.(NotificationServer).TestFeishuWebhook(ctx, req.(*TestFeishuWebhookRequest))
	}
	return interceptor(ctx, in, info, handler)
}

var Notification_ServiceDesc = grpc.ServiceDesc{
	ServiceName: "smartflow.notification.Notification",
	HandlerType: (*NotificationServer)(nil),
	Methods: []grpc.MethodDesc{
		{
			MethodName: "GetFeishuWebhook",
			Handler:    _Notification_GetFeishuWebhook_Handler,
		},
		{
			MethodName: "SaveFeishuWebhook",
			Handler:    _Notification_SaveFeishuWebhook_Handler,
		},
		{
			MethodName: "DeleteFeishuWebhook",
			Handler:    _Notification_DeleteFeishuWebhook_Handler,
		},
		{
			MethodName: "TestFeishuWebhook",
			Handler:    _Notification_TestFeishuWebhook_Handler,
		},
	},
	Streams:  []grpc.StreamDesc{},
	Metadata: "services/notification/rpc/notification.proto",
}
