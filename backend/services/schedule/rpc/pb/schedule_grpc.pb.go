package pb

import (
	context "context"

	grpc "google.golang.org/grpc"
	codes "google.golang.org/grpc/codes"
	status "google.golang.org/grpc/status"
)

const (
	Schedule_Ping_FullMethodName                       = "/smartflow.schedule.Schedule/Ping"
	Schedule_GetToday_FullMethodName                   = "/smartflow.schedule.Schedule/GetToday"
	Schedule_GetWeek_FullMethodName                    = "/smartflow.schedule.Schedule/GetWeek"
	Schedule_DeleteEvents_FullMethodName               = "/smartflow.schedule.Schedule/DeleteEvents"
	Schedule_GetRecentCompleted_FullMethodName         = "/smartflow.schedule.Schedule/GetRecentCompleted"
	Schedule_GetCurrent_FullMethodName                 = "/smartflow.schedule.Schedule/GetCurrent"
	Schedule_RevokeTaskItem_FullMethodName             = "/smartflow.schedule.Schedule/RevokeTaskItem"
	Schedule_SmartPlanning_FullMethodName              = "/smartflow.schedule.Schedule/SmartPlanning"
	Schedule_SmartPlanningMulti_FullMethodName         = "/smartflow.schedule.Schedule/SmartPlanningMulti"
	Schedule_GetAgentWeekSchedule_FullMethodName       = "/smartflow.schedule.Schedule/GetAgentWeekSchedule"
	Schedule_GetScheduleFactsByWindow_FullMethodName   = "/smartflow.schedule.Schedule/GetScheduleFactsByWindow"
	Schedule_GetFeedbackSignal_FullMethodName          = "/smartflow.schedule.Schedule/GetFeedbackSignal"
	Schedule_ApplyActiveScheduleChanges_FullMethodName = "/smartflow.schedule.Schedule/ApplyActiveScheduleChanges"
)

type ScheduleClient interface {
	Ping(ctx context.Context, in *StatusResponse, opts ...grpc.CallOption) (*StatusResponse, error)
	GetToday(ctx context.Context, in *UserRequest, opts ...grpc.CallOption) (*JSONResponse, error)
	GetWeek(ctx context.Context, in *WeekRequest, opts ...grpc.CallOption) (*JSONResponse, error)
	DeleteEvents(ctx context.Context, in *DeleteEventsRequest, opts ...grpc.CallOption) (*StatusResponse, error)
	GetRecentCompleted(ctx context.Context, in *RecentCompletedRequest, opts ...grpc.CallOption) (*JSONResponse, error)
	GetCurrent(ctx context.Context, in *UserRequest, opts ...grpc.CallOption) (*JSONResponse, error)
	RevokeTaskItem(ctx context.Context, in *RevokeTaskItemRequest, opts ...grpc.CallOption) (*StatusResponse, error)
	SmartPlanning(ctx context.Context, in *SmartPlanningRequest, opts ...grpc.CallOption) (*JSONResponse, error)
	SmartPlanningMulti(ctx context.Context, in *SmartPlanningMultiRequest, opts ...grpc.CallOption) (*JSONResponse, error)
	GetAgentWeekSchedule(ctx context.Context, in *JSONRequest, opts ...grpc.CallOption) (*JSONResponse, error)
	GetScheduleFactsByWindow(ctx context.Context, in *JSONRequest, opts ...grpc.CallOption) (*JSONResponse, error)
	GetFeedbackSignal(ctx context.Context, in *JSONRequest, opts ...grpc.CallOption) (*JSONResponse, error)
	ApplyActiveScheduleChanges(ctx context.Context, in *JSONRequest, opts ...grpc.CallOption) (*JSONResponse, error)
}

type scheduleClient struct {
	cc grpc.ClientConnInterface
}

func NewScheduleClient(cc grpc.ClientConnInterface) ScheduleClient {
	return &scheduleClient{cc}
}

func (c *scheduleClient) Ping(ctx context.Context, in *StatusResponse, opts ...grpc.CallOption) (*StatusResponse, error) {
	out := new(StatusResponse)
	err := c.cc.Invoke(ctx, Schedule_Ping_FullMethodName, in, out, opts...)
	return out, err
}

func (c *scheduleClient) GetToday(ctx context.Context, in *UserRequest, opts ...grpc.CallOption) (*JSONResponse, error) {
	out := new(JSONResponse)
	err := c.cc.Invoke(ctx, Schedule_GetToday_FullMethodName, in, out, opts...)
	return out, err
}

func (c *scheduleClient) GetWeek(ctx context.Context, in *WeekRequest, opts ...grpc.CallOption) (*JSONResponse, error) {
	out := new(JSONResponse)
	err := c.cc.Invoke(ctx, Schedule_GetWeek_FullMethodName, in, out, opts...)
	return out, err
}

func (c *scheduleClient) DeleteEvents(ctx context.Context, in *DeleteEventsRequest, opts ...grpc.CallOption) (*StatusResponse, error) {
	out := new(StatusResponse)
	err := c.cc.Invoke(ctx, Schedule_DeleteEvents_FullMethodName, in, out, opts...)
	return out, err
}

func (c *scheduleClient) GetRecentCompleted(ctx context.Context, in *RecentCompletedRequest, opts ...grpc.CallOption) (*JSONResponse, error) {
	out := new(JSONResponse)
	err := c.cc.Invoke(ctx, Schedule_GetRecentCompleted_FullMethodName, in, out, opts...)
	return out, err
}

func (c *scheduleClient) GetCurrent(ctx context.Context, in *UserRequest, opts ...grpc.CallOption) (*JSONResponse, error) {
	out := new(JSONResponse)
	err := c.cc.Invoke(ctx, Schedule_GetCurrent_FullMethodName, in, out, opts...)
	return out, err
}

func (c *scheduleClient) RevokeTaskItem(ctx context.Context, in *RevokeTaskItemRequest, opts ...grpc.CallOption) (*StatusResponse, error) {
	out := new(StatusResponse)
	err := c.cc.Invoke(ctx, Schedule_RevokeTaskItem_FullMethodName, in, out, opts...)
	return out, err
}

func (c *scheduleClient) SmartPlanning(ctx context.Context, in *SmartPlanningRequest, opts ...grpc.CallOption) (*JSONResponse, error) {
	out := new(JSONResponse)
	err := c.cc.Invoke(ctx, Schedule_SmartPlanning_FullMethodName, in, out, opts...)
	return out, err
}

func (c *scheduleClient) SmartPlanningMulti(ctx context.Context, in *SmartPlanningMultiRequest, opts ...grpc.CallOption) (*JSONResponse, error) {
	out := new(JSONResponse)
	err := c.cc.Invoke(ctx, Schedule_SmartPlanningMulti_FullMethodName, in, out, opts...)
	return out, err
}

func (c *scheduleClient) GetAgentWeekSchedule(ctx context.Context, in *JSONRequest, opts ...grpc.CallOption) (*JSONResponse, error) {
	out := new(JSONResponse)
	err := c.cc.Invoke(ctx, Schedule_GetAgentWeekSchedule_FullMethodName, in, out, opts...)
	return out, err
}

func (c *scheduleClient) GetScheduleFactsByWindow(ctx context.Context, in *JSONRequest, opts ...grpc.CallOption) (*JSONResponse, error) {
	out := new(JSONResponse)
	err := c.cc.Invoke(ctx, Schedule_GetScheduleFactsByWindow_FullMethodName, in, out, opts...)
	return out, err
}

func (c *scheduleClient) GetFeedbackSignal(ctx context.Context, in *JSONRequest, opts ...grpc.CallOption) (*JSONResponse, error) {
	out := new(JSONResponse)
	err := c.cc.Invoke(ctx, Schedule_GetFeedbackSignal_FullMethodName, in, out, opts...)
	return out, err
}

func (c *scheduleClient) ApplyActiveScheduleChanges(ctx context.Context, in *JSONRequest, opts ...grpc.CallOption) (*JSONResponse, error) {
	out := new(JSONResponse)
	err := c.cc.Invoke(ctx, Schedule_ApplyActiveScheduleChanges_FullMethodName, in, out, opts...)
	return out, err
}

type ScheduleServer interface {
	Ping(context.Context, *StatusResponse) (*StatusResponse, error)
	GetToday(context.Context, *UserRequest) (*JSONResponse, error)
	GetWeek(context.Context, *WeekRequest) (*JSONResponse, error)
	DeleteEvents(context.Context, *DeleteEventsRequest) (*StatusResponse, error)
	GetRecentCompleted(context.Context, *RecentCompletedRequest) (*JSONResponse, error)
	GetCurrent(context.Context, *UserRequest) (*JSONResponse, error)
	RevokeTaskItem(context.Context, *RevokeTaskItemRequest) (*StatusResponse, error)
	SmartPlanning(context.Context, *SmartPlanningRequest) (*JSONResponse, error)
	SmartPlanningMulti(context.Context, *SmartPlanningMultiRequest) (*JSONResponse, error)
	GetAgentWeekSchedule(context.Context, *JSONRequest) (*JSONResponse, error)
	GetScheduleFactsByWindow(context.Context, *JSONRequest) (*JSONResponse, error)
	GetFeedbackSignal(context.Context, *JSONRequest) (*JSONResponse, error)
	ApplyActiveScheduleChanges(context.Context, *JSONRequest) (*JSONResponse, error)
}

type UnimplementedScheduleServer struct{}

func (UnimplementedScheduleServer) Ping(context.Context, *StatusResponse) (*StatusResponse, error) {
	return nil, status.Errorf(codes.Unimplemented, "method Ping not implemented")
}

func (UnimplementedScheduleServer) GetToday(context.Context, *UserRequest) (*JSONResponse, error) {
	return nil, status.Errorf(codes.Unimplemented, "method GetToday not implemented")
}
func (UnimplementedScheduleServer) GetWeek(context.Context, *WeekRequest) (*JSONResponse, error) {
	return nil, status.Errorf(codes.Unimplemented, "method GetWeek not implemented")
}
func (UnimplementedScheduleServer) DeleteEvents(context.Context, *DeleteEventsRequest) (*StatusResponse, error) {
	return nil, status.Errorf(codes.Unimplemented, "method DeleteEvents not implemented")
}
func (UnimplementedScheduleServer) GetRecentCompleted(context.Context, *RecentCompletedRequest) (*JSONResponse, error) {
	return nil, status.Errorf(codes.Unimplemented, "method GetRecentCompleted not implemented")
}
func (UnimplementedScheduleServer) GetCurrent(context.Context, *UserRequest) (*JSONResponse, error) {
	return nil, status.Errorf(codes.Unimplemented, "method GetCurrent not implemented")
}
func (UnimplementedScheduleServer) RevokeTaskItem(context.Context, *RevokeTaskItemRequest) (*StatusResponse, error) {
	return nil, status.Errorf(codes.Unimplemented, "method RevokeTaskItem not implemented")
}
func (UnimplementedScheduleServer) SmartPlanning(context.Context, *SmartPlanningRequest) (*JSONResponse, error) {
	return nil, status.Errorf(codes.Unimplemented, "method SmartPlanning not implemented")
}
func (UnimplementedScheduleServer) SmartPlanningMulti(context.Context, *SmartPlanningMultiRequest) (*JSONResponse, error) {
	return nil, status.Errorf(codes.Unimplemented, "method SmartPlanningMulti not implemented")
}
func (UnimplementedScheduleServer) GetAgentWeekSchedule(context.Context, *JSONRequest) (*JSONResponse, error) {
	return nil, status.Errorf(codes.Unimplemented, "method GetAgentWeekSchedule not implemented")
}
func (UnimplementedScheduleServer) GetScheduleFactsByWindow(context.Context, *JSONRequest) (*JSONResponse, error) {
	return nil, status.Errorf(codes.Unimplemented, "method GetScheduleFactsByWindow not implemented")
}
func (UnimplementedScheduleServer) GetFeedbackSignal(context.Context, *JSONRequest) (*JSONResponse, error) {
	return nil, status.Errorf(codes.Unimplemented, "method GetFeedbackSignal not implemented")
}
func (UnimplementedScheduleServer) ApplyActiveScheduleChanges(context.Context, *JSONRequest) (*JSONResponse, error) {
	return nil, status.Errorf(codes.Unimplemented, "method ApplyActiveScheduleChanges not implemented")
}

func RegisterScheduleServer(s grpc.ServiceRegistrar, srv ScheduleServer) {
	s.RegisterService(&Schedule_ServiceDesc, srv)
}

func _Schedule_Ping_Handler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpc.UnaryServerInterceptor) (interface{}, error) {
	in := new(StatusResponse)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(ScheduleServer).Ping(ctx, in)
	}
	info := &grpc.UnaryServerInfo{Server: srv, FullMethod: Schedule_Ping_FullMethodName}
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return srv.(ScheduleServer).Ping(ctx, req.(*StatusResponse))
	}
	return interceptor(ctx, in, info, handler)
}

func _Schedule_GetToday_Handler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpc.UnaryServerInterceptor) (interface{}, error) {
	in := new(UserRequest)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(ScheduleServer).GetToday(ctx, in)
	}
	info := &grpc.UnaryServerInfo{Server: srv, FullMethod: Schedule_GetToday_FullMethodName}
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return srv.(ScheduleServer).GetToday(ctx, req.(*UserRequest))
	}
	return interceptor(ctx, in, info, handler)
}

func _Schedule_GetWeek_Handler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpc.UnaryServerInterceptor) (interface{}, error) {
	in := new(WeekRequest)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(ScheduleServer).GetWeek(ctx, in)
	}
	info := &grpc.UnaryServerInfo{Server: srv, FullMethod: Schedule_GetWeek_FullMethodName}
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return srv.(ScheduleServer).GetWeek(ctx, req.(*WeekRequest))
	}
	return interceptor(ctx, in, info, handler)
}

func _Schedule_DeleteEvents_Handler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpc.UnaryServerInterceptor) (interface{}, error) {
	in := new(DeleteEventsRequest)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(ScheduleServer).DeleteEvents(ctx, in)
	}
	info := &grpc.UnaryServerInfo{Server: srv, FullMethod: Schedule_DeleteEvents_FullMethodName}
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return srv.(ScheduleServer).DeleteEvents(ctx, req.(*DeleteEventsRequest))
	}
	return interceptor(ctx, in, info, handler)
}

func _Schedule_GetRecentCompleted_Handler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpc.UnaryServerInterceptor) (interface{}, error) {
	in := new(RecentCompletedRequest)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(ScheduleServer).GetRecentCompleted(ctx, in)
	}
	info := &grpc.UnaryServerInfo{Server: srv, FullMethod: Schedule_GetRecentCompleted_FullMethodName}
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return srv.(ScheduleServer).GetRecentCompleted(ctx, req.(*RecentCompletedRequest))
	}
	return interceptor(ctx, in, info, handler)
}

func _Schedule_GetCurrent_Handler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpc.UnaryServerInterceptor) (interface{}, error) {
	in := new(UserRequest)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(ScheduleServer).GetCurrent(ctx, in)
	}
	info := &grpc.UnaryServerInfo{Server: srv, FullMethod: Schedule_GetCurrent_FullMethodName}
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return srv.(ScheduleServer).GetCurrent(ctx, req.(*UserRequest))
	}
	return interceptor(ctx, in, info, handler)
}

func _Schedule_RevokeTaskItem_Handler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpc.UnaryServerInterceptor) (interface{}, error) {
	in := new(RevokeTaskItemRequest)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(ScheduleServer).RevokeTaskItem(ctx, in)
	}
	info := &grpc.UnaryServerInfo{Server: srv, FullMethod: Schedule_RevokeTaskItem_FullMethodName}
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return srv.(ScheduleServer).RevokeTaskItem(ctx, req.(*RevokeTaskItemRequest))
	}
	return interceptor(ctx, in, info, handler)
}

func _Schedule_SmartPlanning_Handler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpc.UnaryServerInterceptor) (interface{}, error) {
	in := new(SmartPlanningRequest)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(ScheduleServer).SmartPlanning(ctx, in)
	}
	info := &grpc.UnaryServerInfo{Server: srv, FullMethod: Schedule_SmartPlanning_FullMethodName}
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return srv.(ScheduleServer).SmartPlanning(ctx, req.(*SmartPlanningRequest))
	}
	return interceptor(ctx, in, info, handler)
}

func _Schedule_SmartPlanningMulti_Handler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpc.UnaryServerInterceptor) (interface{}, error) {
	in := new(SmartPlanningMultiRequest)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(ScheduleServer).SmartPlanningMulti(ctx, in)
	}
	info := &grpc.UnaryServerInfo{Server: srv, FullMethod: Schedule_SmartPlanningMulti_FullMethodName}
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return srv.(ScheduleServer).SmartPlanningMulti(ctx, req.(*SmartPlanningMultiRequest))
	}
	return interceptor(ctx, in, info, handler)
}

func _Schedule_GetAgentWeekSchedule_Handler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpc.UnaryServerInterceptor) (interface{}, error) {
	in := new(JSONRequest)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(ScheduleServer).GetAgentWeekSchedule(ctx, in)
	}
	info := &grpc.UnaryServerInfo{Server: srv, FullMethod: Schedule_GetAgentWeekSchedule_FullMethodName}
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return srv.(ScheduleServer).GetAgentWeekSchedule(ctx, req.(*JSONRequest))
	}
	return interceptor(ctx, in, info, handler)
}

func _Schedule_GetScheduleFactsByWindow_Handler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpc.UnaryServerInterceptor) (interface{}, error) {
	in := new(JSONRequest)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(ScheduleServer).GetScheduleFactsByWindow(ctx, in)
	}
	info := &grpc.UnaryServerInfo{Server: srv, FullMethod: Schedule_GetScheduleFactsByWindow_FullMethodName}
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return srv.(ScheduleServer).GetScheduleFactsByWindow(ctx, req.(*JSONRequest))
	}
	return interceptor(ctx, in, info, handler)
}

func _Schedule_GetFeedbackSignal_Handler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpc.UnaryServerInterceptor) (interface{}, error) {
	in := new(JSONRequest)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(ScheduleServer).GetFeedbackSignal(ctx, in)
	}
	info := &grpc.UnaryServerInfo{Server: srv, FullMethod: Schedule_GetFeedbackSignal_FullMethodName}
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return srv.(ScheduleServer).GetFeedbackSignal(ctx, req.(*JSONRequest))
	}
	return interceptor(ctx, in, info, handler)
}

func _Schedule_ApplyActiveScheduleChanges_Handler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpc.UnaryServerInterceptor) (interface{}, error) {
	in := new(JSONRequest)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(ScheduleServer).ApplyActiveScheduleChanges(ctx, in)
	}
	info := &grpc.UnaryServerInfo{Server: srv, FullMethod: Schedule_ApplyActiveScheduleChanges_FullMethodName}
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return srv.(ScheduleServer).ApplyActiveScheduleChanges(ctx, req.(*JSONRequest))
	}
	return interceptor(ctx, in, info, handler)
}

var Schedule_ServiceDesc = grpc.ServiceDesc{
	ServiceName: "smartflow.schedule.Schedule",
	HandlerType: (*ScheduleServer)(nil),
	Methods: []grpc.MethodDesc{
		{MethodName: "Ping", Handler: _Schedule_Ping_Handler},
		{MethodName: "GetToday", Handler: _Schedule_GetToday_Handler},
		{MethodName: "GetWeek", Handler: _Schedule_GetWeek_Handler},
		{MethodName: "DeleteEvents", Handler: _Schedule_DeleteEvents_Handler},
		{MethodName: "GetRecentCompleted", Handler: _Schedule_GetRecentCompleted_Handler},
		{MethodName: "GetCurrent", Handler: _Schedule_GetCurrent_Handler},
		{MethodName: "RevokeTaskItem", Handler: _Schedule_RevokeTaskItem_Handler},
		{MethodName: "SmartPlanning", Handler: _Schedule_SmartPlanning_Handler},
		{MethodName: "SmartPlanningMulti", Handler: _Schedule_SmartPlanningMulti_Handler},
		{MethodName: "GetAgentWeekSchedule", Handler: _Schedule_GetAgentWeekSchedule_Handler},
		{MethodName: "GetScheduleFactsByWindow", Handler: _Schedule_GetScheduleFactsByWindow_Handler},
		{MethodName: "GetFeedbackSignal", Handler: _Schedule_GetFeedbackSignal_Handler},
		{MethodName: "ApplyActiveScheduleChanges", Handler: _Schedule_ApplyActiveScheduleChanges_Handler},
	},
	Streams:  []grpc.StreamDesc{},
	Metadata: "schedule.proto",
}
