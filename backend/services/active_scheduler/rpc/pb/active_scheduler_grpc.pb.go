package pb

import (
	context "context"

	grpc "google.golang.org/grpc"
	codes "google.golang.org/grpc/codes"
	status "google.golang.org/grpc/status"
)

const (
	ActiveScheduler_DryRun_FullMethodName         = "/smartflow.active_scheduler.ActiveScheduler/DryRun"
	ActiveScheduler_Trigger_FullMethodName        = "/smartflow.active_scheduler.ActiveScheduler/Trigger"
	ActiveScheduler_CreatePreview_FullMethodName  = "/smartflow.active_scheduler.ActiveScheduler/CreatePreview"
	ActiveScheduler_GetPreview_FullMethodName     = "/smartflow.active_scheduler.ActiveScheduler/GetPreview"
	ActiveScheduler_ConfirmPreview_FullMethodName = "/smartflow.active_scheduler.ActiveScheduler/ConfirmPreview"
)

type ActiveSchedulerClient interface {
	DryRun(ctx context.Context, in *ActiveScheduleRequest, opts ...grpc.CallOption) (*JSONResponse, error)
	Trigger(ctx context.Context, in *ActiveScheduleRequest, opts ...grpc.CallOption) (*TriggerResponse, error)
	CreatePreview(ctx context.Context, in *ActiveScheduleRequest, opts ...grpc.CallOption) (*JSONResponse, error)
	GetPreview(ctx context.Context, in *GetPreviewRequest, opts ...grpc.CallOption) (*JSONResponse, error)
	ConfirmPreview(ctx context.Context, in *ConfirmPreviewRequest, opts ...grpc.CallOption) (*JSONResponse, error)
}

type activeSchedulerClient struct {
	cc grpc.ClientConnInterface
}

func NewActiveSchedulerClient(cc grpc.ClientConnInterface) ActiveSchedulerClient {
	return &activeSchedulerClient{cc}
}

func (c *activeSchedulerClient) DryRun(ctx context.Context, in *ActiveScheduleRequest, opts ...grpc.CallOption) (*JSONResponse, error) {
	out := new(JSONResponse)
	err := c.cc.Invoke(ctx, ActiveScheduler_DryRun_FullMethodName, in, out, opts...)
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (c *activeSchedulerClient) Trigger(ctx context.Context, in *ActiveScheduleRequest, opts ...grpc.CallOption) (*TriggerResponse, error) {
	out := new(TriggerResponse)
	err := c.cc.Invoke(ctx, ActiveScheduler_Trigger_FullMethodName, in, out, opts...)
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (c *activeSchedulerClient) CreatePreview(ctx context.Context, in *ActiveScheduleRequest, opts ...grpc.CallOption) (*JSONResponse, error) {
	out := new(JSONResponse)
	err := c.cc.Invoke(ctx, ActiveScheduler_CreatePreview_FullMethodName, in, out, opts...)
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (c *activeSchedulerClient) GetPreview(ctx context.Context, in *GetPreviewRequest, opts ...grpc.CallOption) (*JSONResponse, error) {
	out := new(JSONResponse)
	err := c.cc.Invoke(ctx, ActiveScheduler_GetPreview_FullMethodName, in, out, opts...)
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (c *activeSchedulerClient) ConfirmPreview(ctx context.Context, in *ConfirmPreviewRequest, opts ...grpc.CallOption) (*JSONResponse, error) {
	out := new(JSONResponse)
	err := c.cc.Invoke(ctx, ActiveScheduler_ConfirmPreview_FullMethodName, in, out, opts...)
	if err != nil {
		return nil, err
	}
	return out, nil
}

type ActiveSchedulerServer interface {
	DryRun(context.Context, *ActiveScheduleRequest) (*JSONResponse, error)
	Trigger(context.Context, *ActiveScheduleRequest) (*TriggerResponse, error)
	CreatePreview(context.Context, *ActiveScheduleRequest) (*JSONResponse, error)
	GetPreview(context.Context, *GetPreviewRequest) (*JSONResponse, error)
	ConfirmPreview(context.Context, *ConfirmPreviewRequest) (*JSONResponse, error)
}

type UnimplementedActiveSchedulerServer struct{}

func (UnimplementedActiveSchedulerServer) DryRun(context.Context, *ActiveScheduleRequest) (*JSONResponse, error) {
	return nil, status.Errorf(codes.Unimplemented, "method DryRun not implemented")
}

func (UnimplementedActiveSchedulerServer) Trigger(context.Context, *ActiveScheduleRequest) (*TriggerResponse, error) {
	return nil, status.Errorf(codes.Unimplemented, "method Trigger not implemented")
}

func (UnimplementedActiveSchedulerServer) CreatePreview(context.Context, *ActiveScheduleRequest) (*JSONResponse, error) {
	return nil, status.Errorf(codes.Unimplemented, "method CreatePreview not implemented")
}

func (UnimplementedActiveSchedulerServer) GetPreview(context.Context, *GetPreviewRequest) (*JSONResponse, error) {
	return nil, status.Errorf(codes.Unimplemented, "method GetPreview not implemented")
}

func (UnimplementedActiveSchedulerServer) ConfirmPreview(context.Context, *ConfirmPreviewRequest) (*JSONResponse, error) {
	return nil, status.Errorf(codes.Unimplemented, "method ConfirmPreview not implemented")
}

func RegisterActiveSchedulerServer(s grpc.ServiceRegistrar, srv ActiveSchedulerServer) {
	s.RegisterService(&ActiveScheduler_ServiceDesc, srv)
}

func _ActiveScheduler_DryRun_Handler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpc.UnaryServerInterceptor) (interface{}, error) {
	in := new(ActiveScheduleRequest)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(ActiveSchedulerServer).DryRun(ctx, in)
	}
	info := &grpc.UnaryServerInfo{Server: srv, FullMethod: ActiveScheduler_DryRun_FullMethodName}
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return srv.(ActiveSchedulerServer).DryRun(ctx, req.(*ActiveScheduleRequest))
	}
	return interceptor(ctx, in, info, handler)
}

func _ActiveScheduler_Trigger_Handler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpc.UnaryServerInterceptor) (interface{}, error) {
	in := new(ActiveScheduleRequest)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(ActiveSchedulerServer).Trigger(ctx, in)
	}
	info := &grpc.UnaryServerInfo{Server: srv, FullMethod: ActiveScheduler_Trigger_FullMethodName}
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return srv.(ActiveSchedulerServer).Trigger(ctx, req.(*ActiveScheduleRequest))
	}
	return interceptor(ctx, in, info, handler)
}

func _ActiveScheduler_CreatePreview_Handler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpc.UnaryServerInterceptor) (interface{}, error) {
	in := new(ActiveScheduleRequest)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(ActiveSchedulerServer).CreatePreview(ctx, in)
	}
	info := &grpc.UnaryServerInfo{Server: srv, FullMethod: ActiveScheduler_CreatePreview_FullMethodName}
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return srv.(ActiveSchedulerServer).CreatePreview(ctx, req.(*ActiveScheduleRequest))
	}
	return interceptor(ctx, in, info, handler)
}

func _ActiveScheduler_GetPreview_Handler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpc.UnaryServerInterceptor) (interface{}, error) {
	in := new(GetPreviewRequest)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(ActiveSchedulerServer).GetPreview(ctx, in)
	}
	info := &grpc.UnaryServerInfo{Server: srv, FullMethod: ActiveScheduler_GetPreview_FullMethodName}
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return srv.(ActiveSchedulerServer).GetPreview(ctx, req.(*GetPreviewRequest))
	}
	return interceptor(ctx, in, info, handler)
}

func _ActiveScheduler_ConfirmPreview_Handler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpc.UnaryServerInterceptor) (interface{}, error) {
	in := new(ConfirmPreviewRequest)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(ActiveSchedulerServer).ConfirmPreview(ctx, in)
	}
	info := &grpc.UnaryServerInfo{Server: srv, FullMethod: ActiveScheduler_ConfirmPreview_FullMethodName}
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return srv.(ActiveSchedulerServer).ConfirmPreview(ctx, req.(*ConfirmPreviewRequest))
	}
	return interceptor(ctx, in, info, handler)
}

var ActiveScheduler_ServiceDesc = grpc.ServiceDesc{
	ServiceName: "smartflow.active_scheduler.ActiveScheduler",
	HandlerType: (*ActiveSchedulerServer)(nil),
	Methods: []grpc.MethodDesc{
		{MethodName: "DryRun", Handler: _ActiveScheduler_DryRun_Handler},
		{MethodName: "Trigger", Handler: _ActiveScheduler_Trigger_Handler},
		{MethodName: "CreatePreview", Handler: _ActiveScheduler_CreatePreview_Handler},
		{MethodName: "GetPreview", Handler: _ActiveScheduler_GetPreview_Handler},
		{MethodName: "ConfirmPreview", Handler: _ActiveScheduler_ConfirmPreview_Handler},
	},
	Streams:  []grpc.StreamDesc{},
	Metadata: "services/active_scheduler/rpc/active_scheduler.proto",
}
