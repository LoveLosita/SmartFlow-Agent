package pb

import (
	context "context"

	grpc "google.golang.org/grpc"
	codes "google.golang.org/grpc/codes"
	status "google.golang.org/grpc/status"
)

const (
	TaskClass_Ping_FullMethodName                            = "/smartflow.taskclass.TaskClass/Ping"
	TaskClass_AddTaskClass_FullMethodName                    = "/smartflow.taskclass.TaskClass/AddTaskClass"
	TaskClass_ListTaskClasses_FullMethodName                 = "/smartflow.taskclass.TaskClass/ListTaskClasses"
	TaskClass_GetTaskClass_FullMethodName                    = "/smartflow.taskclass.TaskClass/GetTaskClass"
	TaskClass_UpdateTaskClass_FullMethodName                 = "/smartflow.taskclass.TaskClass/UpdateTaskClass"
	TaskClass_GetAgentTaskClasses_FullMethodName             = "/smartflow.taskclass.TaskClass/GetAgentTaskClasses"
	TaskClass_InsertTaskClassItemIntoSchedule_FullMethodName = "/smartflow.taskclass.TaskClass/InsertTaskClassItemIntoSchedule"
	TaskClass_DeleteTaskClassItem_FullMethodName             = "/smartflow.taskclass.TaskClass/DeleteTaskClassItem"
	TaskClass_DeleteTaskClass_FullMethodName                 = "/smartflow.taskclass.TaskClass/DeleteTaskClass"
	TaskClass_ApplyBatchIntoSchedule_FullMethodName          = "/smartflow.taskclass.TaskClass/ApplyBatchIntoSchedule"
)

type TaskClassClient interface {
	Ping(ctx context.Context, in *StatusResponse, opts ...grpc.CallOption) (*StatusResponse, error)
	AddTaskClass(ctx context.Context, in *JSONRequest, opts ...grpc.CallOption) (*JSONResponse, error)
	ListTaskClasses(ctx context.Context, in *JSONRequest, opts ...grpc.CallOption) (*JSONResponse, error)
	GetTaskClass(ctx context.Context, in *JSONRequest, opts ...grpc.CallOption) (*JSONResponse, error)
	UpdateTaskClass(ctx context.Context, in *JSONRequest, opts ...grpc.CallOption) (*JSONResponse, error)
	GetAgentTaskClasses(ctx context.Context, in *JSONRequest, opts ...grpc.CallOption) (*JSONResponse, error)
	InsertTaskClassItemIntoSchedule(ctx context.Context, in *JSONRequest, opts ...grpc.CallOption) (*JSONResponse, error)
	DeleteTaskClassItem(ctx context.Context, in *JSONRequest, opts ...grpc.CallOption) (*JSONResponse, error)
	DeleteTaskClass(ctx context.Context, in *JSONRequest, opts ...grpc.CallOption) (*JSONResponse, error)
	ApplyBatchIntoSchedule(ctx context.Context, in *JSONRequest, opts ...grpc.CallOption) (*JSONResponse, error)
}

type taskClassClient struct {
	cc grpc.ClientConnInterface
}

func NewTaskClassClient(cc grpc.ClientConnInterface) TaskClassClient {
	return &taskClassClient{cc}
}

func (c *taskClassClient) Ping(ctx context.Context, in *StatusResponse, opts ...grpc.CallOption) (*StatusResponse, error) {
	out := new(StatusResponse)
	err := c.cc.Invoke(ctx, TaskClass_Ping_FullMethodName, in, out, opts...)
	return out, err
}

func (c *taskClassClient) AddTaskClass(ctx context.Context, in *JSONRequest, opts ...grpc.CallOption) (*JSONResponse, error) {
	out := new(JSONResponse)
	err := c.cc.Invoke(ctx, TaskClass_AddTaskClass_FullMethodName, in, out, opts...)
	return out, err
}

func (c *taskClassClient) ListTaskClasses(ctx context.Context, in *JSONRequest, opts ...grpc.CallOption) (*JSONResponse, error) {
	out := new(JSONResponse)
	err := c.cc.Invoke(ctx, TaskClass_ListTaskClasses_FullMethodName, in, out, opts...)
	return out, err
}

func (c *taskClassClient) GetTaskClass(ctx context.Context, in *JSONRequest, opts ...grpc.CallOption) (*JSONResponse, error) {
	out := new(JSONResponse)
	err := c.cc.Invoke(ctx, TaskClass_GetTaskClass_FullMethodName, in, out, opts...)
	return out, err
}

func (c *taskClassClient) UpdateTaskClass(ctx context.Context, in *JSONRequest, opts ...grpc.CallOption) (*JSONResponse, error) {
	out := new(JSONResponse)
	err := c.cc.Invoke(ctx, TaskClass_UpdateTaskClass_FullMethodName, in, out, opts...)
	return out, err
}

func (c *taskClassClient) GetAgentTaskClasses(ctx context.Context, in *JSONRequest, opts ...grpc.CallOption) (*JSONResponse, error) {
	out := new(JSONResponse)
	err := c.cc.Invoke(ctx, TaskClass_GetAgentTaskClasses_FullMethodName, in, out, opts...)
	return out, err
}

func (c *taskClassClient) InsertTaskClassItemIntoSchedule(ctx context.Context, in *JSONRequest, opts ...grpc.CallOption) (*JSONResponse, error) {
	out := new(JSONResponse)
	err := c.cc.Invoke(ctx, TaskClass_InsertTaskClassItemIntoSchedule_FullMethodName, in, out, opts...)
	return out, err
}

func (c *taskClassClient) DeleteTaskClassItem(ctx context.Context, in *JSONRequest, opts ...grpc.CallOption) (*JSONResponse, error) {
	out := new(JSONResponse)
	err := c.cc.Invoke(ctx, TaskClass_DeleteTaskClassItem_FullMethodName, in, out, opts...)
	return out, err
}

func (c *taskClassClient) DeleteTaskClass(ctx context.Context, in *JSONRequest, opts ...grpc.CallOption) (*JSONResponse, error) {
	out := new(JSONResponse)
	err := c.cc.Invoke(ctx, TaskClass_DeleteTaskClass_FullMethodName, in, out, opts...)
	return out, err
}

func (c *taskClassClient) ApplyBatchIntoSchedule(ctx context.Context, in *JSONRequest, opts ...grpc.CallOption) (*JSONResponse, error) {
	out := new(JSONResponse)
	err := c.cc.Invoke(ctx, TaskClass_ApplyBatchIntoSchedule_FullMethodName, in, out, opts...)
	return out, err
}

type TaskClassServer interface {
	Ping(context.Context, *StatusResponse) (*StatusResponse, error)
	AddTaskClass(context.Context, *JSONRequest) (*JSONResponse, error)
	ListTaskClasses(context.Context, *JSONRequest) (*JSONResponse, error)
	GetTaskClass(context.Context, *JSONRequest) (*JSONResponse, error)
	UpdateTaskClass(context.Context, *JSONRequest) (*JSONResponse, error)
	GetAgentTaskClasses(context.Context, *JSONRequest) (*JSONResponse, error)
	InsertTaskClassItemIntoSchedule(context.Context, *JSONRequest) (*JSONResponse, error)
	DeleteTaskClassItem(context.Context, *JSONRequest) (*JSONResponse, error)
	DeleteTaskClass(context.Context, *JSONRequest) (*JSONResponse, error)
	ApplyBatchIntoSchedule(context.Context, *JSONRequest) (*JSONResponse, error)
}

type UnimplementedTaskClassServer struct{}

func (UnimplementedTaskClassServer) Ping(context.Context, *StatusResponse) (*StatusResponse, error) {
	return nil, status.Errorf(codes.Unimplemented, "method Ping not implemented")
}
func (UnimplementedTaskClassServer) AddTaskClass(context.Context, *JSONRequest) (*JSONResponse, error) {
	return nil, status.Errorf(codes.Unimplemented, "method AddTaskClass not implemented")
}
func (UnimplementedTaskClassServer) ListTaskClasses(context.Context, *JSONRequest) (*JSONResponse, error) {
	return nil, status.Errorf(codes.Unimplemented, "method ListTaskClasses not implemented")
}
func (UnimplementedTaskClassServer) GetTaskClass(context.Context, *JSONRequest) (*JSONResponse, error) {
	return nil, status.Errorf(codes.Unimplemented, "method GetTaskClass not implemented")
}
func (UnimplementedTaskClassServer) UpdateTaskClass(context.Context, *JSONRequest) (*JSONResponse, error) {
	return nil, status.Errorf(codes.Unimplemented, "method UpdateTaskClass not implemented")
}
func (UnimplementedTaskClassServer) GetAgentTaskClasses(context.Context, *JSONRequest) (*JSONResponse, error) {
	return nil, status.Errorf(codes.Unimplemented, "method GetAgentTaskClasses not implemented")
}
func (UnimplementedTaskClassServer) InsertTaskClassItemIntoSchedule(context.Context, *JSONRequest) (*JSONResponse, error) {
	return nil, status.Errorf(codes.Unimplemented, "method InsertTaskClassItemIntoSchedule not implemented")
}
func (UnimplementedTaskClassServer) DeleteTaskClassItem(context.Context, *JSONRequest) (*JSONResponse, error) {
	return nil, status.Errorf(codes.Unimplemented, "method DeleteTaskClassItem not implemented")
}
func (UnimplementedTaskClassServer) DeleteTaskClass(context.Context, *JSONRequest) (*JSONResponse, error) {
	return nil, status.Errorf(codes.Unimplemented, "method DeleteTaskClass not implemented")
}
func (UnimplementedTaskClassServer) ApplyBatchIntoSchedule(context.Context, *JSONRequest) (*JSONResponse, error) {
	return nil, status.Errorf(codes.Unimplemented, "method ApplyBatchIntoSchedule not implemented")
}

func RegisterTaskClassServer(s grpc.ServiceRegistrar, srv TaskClassServer) {
	s.RegisterService(&TaskClass_ServiceDesc, srv)
}

func _TaskClass_Ping_Handler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpc.UnaryServerInterceptor) (interface{}, error) {
	in := new(StatusResponse)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(TaskClassServer).Ping(ctx, in)
	}
	info := &grpc.UnaryServerInfo{Server: srv, FullMethod: TaskClass_Ping_FullMethodName}
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return srv.(TaskClassServer).Ping(ctx, req.(*StatusResponse))
	}
	return interceptor(ctx, in, info, handler)
}

func _TaskClass_JSON_Handler(fullMethod string, invoke func(TaskClassServer, context.Context, *JSONRequest) (*JSONResponse, error)) grpc.MethodHandler {
	return func(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpc.UnaryServerInterceptor) (interface{}, error) {
		in := new(JSONRequest)
		if err := dec(in); err != nil {
			return nil, err
		}
		if interceptor == nil {
			return invoke(srv.(TaskClassServer), ctx, in)
		}
		info := &grpc.UnaryServerInfo{Server: srv, FullMethod: fullMethod}
		handler := func(ctx context.Context, req interface{}) (interface{}, error) {
			return invoke(srv.(TaskClassServer), ctx, req.(*JSONRequest))
		}
		return interceptor(ctx, in, info, handler)
	}
}

var TaskClass_ServiceDesc = grpc.ServiceDesc{
	ServiceName: "smartflow.taskclass.TaskClass",
	HandlerType: (*TaskClassServer)(nil),
	Methods: []grpc.MethodDesc{
		{MethodName: "Ping", Handler: _TaskClass_Ping_Handler},
		{MethodName: "AddTaskClass", Handler: _TaskClass_JSON_Handler(TaskClass_AddTaskClass_FullMethodName, TaskClassServer.AddTaskClass)},
		{MethodName: "ListTaskClasses", Handler: _TaskClass_JSON_Handler(TaskClass_ListTaskClasses_FullMethodName, TaskClassServer.ListTaskClasses)},
		{MethodName: "GetTaskClass", Handler: _TaskClass_JSON_Handler(TaskClass_GetTaskClass_FullMethodName, TaskClassServer.GetTaskClass)},
		{MethodName: "UpdateTaskClass", Handler: _TaskClass_JSON_Handler(TaskClass_UpdateTaskClass_FullMethodName, TaskClassServer.UpdateTaskClass)},
		{MethodName: "GetAgentTaskClasses", Handler: _TaskClass_JSON_Handler(TaskClass_GetAgentTaskClasses_FullMethodName, TaskClassServer.GetAgentTaskClasses)},
		{MethodName: "InsertTaskClassItemIntoSchedule", Handler: _TaskClass_JSON_Handler(TaskClass_InsertTaskClassItemIntoSchedule_FullMethodName, TaskClassServer.InsertTaskClassItemIntoSchedule)},
		{MethodName: "DeleteTaskClassItem", Handler: _TaskClass_JSON_Handler(TaskClass_DeleteTaskClassItem_FullMethodName, TaskClassServer.DeleteTaskClassItem)},
		{MethodName: "DeleteTaskClass", Handler: _TaskClass_JSON_Handler(TaskClass_DeleteTaskClass_FullMethodName, TaskClassServer.DeleteTaskClass)},
		{MethodName: "ApplyBatchIntoSchedule", Handler: _TaskClass_JSON_Handler(TaskClass_ApplyBatchIntoSchedule_FullMethodName, TaskClassServer.ApplyBatchIntoSchedule)},
	},
	Streams:  []grpc.StreamDesc{},
	Metadata: "task_class.proto",
}
