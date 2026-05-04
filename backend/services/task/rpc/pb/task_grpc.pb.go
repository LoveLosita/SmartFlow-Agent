package pb

import (
	context "context"

	grpc "google.golang.org/grpc"
	codes "google.golang.org/grpc/codes"
	status "google.golang.org/grpc/status"
)

const (
	Task_Ping_FullMethodName                     = "/smartflow.task.Task/Ping"
	Task_AddTask_FullMethodName                  = "/smartflow.task.Task/AddTask"
	Task_GetUserTasks_FullMethodName             = "/smartflow.task.Task/GetUserTasks"
	Task_BatchTaskStatus_FullMethodName          = "/smartflow.task.Task/BatchTaskStatus"
	Task_CompleteTask_FullMethodName             = "/smartflow.task.Task/CompleteTask"
	Task_UndoCompleteTask_FullMethodName         = "/smartflow.task.Task/UndoCompleteTask"
	Task_UpdateTask_FullMethodName               = "/smartflow.task.Task/UpdateTask"
	Task_DeleteTask_FullMethodName               = "/smartflow.task.Task/DeleteTask"
	Task_GetTaskForActiveSchedule_FullMethodName = "/smartflow.task.Task/GetTaskForActiveSchedule"
)

type TaskClient interface {
	Ping(ctx context.Context, in *StatusResponse, opts ...grpc.CallOption) (*StatusResponse, error)
	AddTask(ctx context.Context, in *JSONRequest, opts ...grpc.CallOption) (*JSONResponse, error)
	GetUserTasks(ctx context.Context, in *JSONRequest, opts ...grpc.CallOption) (*JSONResponse, error)
	BatchTaskStatus(ctx context.Context, in *JSONRequest, opts ...grpc.CallOption) (*JSONResponse, error)
	CompleteTask(ctx context.Context, in *JSONRequest, opts ...grpc.CallOption) (*JSONResponse, error)
	UndoCompleteTask(ctx context.Context, in *JSONRequest, opts ...grpc.CallOption) (*JSONResponse, error)
	UpdateTask(ctx context.Context, in *JSONRequest, opts ...grpc.CallOption) (*JSONResponse, error)
	DeleteTask(ctx context.Context, in *JSONRequest, opts ...grpc.CallOption) (*JSONResponse, error)
	GetTaskForActiveSchedule(ctx context.Context, in *JSONRequest, opts ...grpc.CallOption) (*JSONResponse, error)
}

type taskClient struct {
	cc grpc.ClientConnInterface
}

func NewTaskClient(cc grpc.ClientConnInterface) TaskClient {
	return &taskClient{cc}
}

func (c *taskClient) Ping(ctx context.Context, in *StatusResponse, opts ...grpc.CallOption) (*StatusResponse, error) {
	out := new(StatusResponse)
	err := c.cc.Invoke(ctx, Task_Ping_FullMethodName, in, out, opts...)
	return out, err
}

func (c *taskClient) AddTask(ctx context.Context, in *JSONRequest, opts ...grpc.CallOption) (*JSONResponse, error) {
	out := new(JSONResponse)
	err := c.cc.Invoke(ctx, Task_AddTask_FullMethodName, in, out, opts...)
	return out, err
}

func (c *taskClient) GetUserTasks(ctx context.Context, in *JSONRequest, opts ...grpc.CallOption) (*JSONResponse, error) {
	out := new(JSONResponse)
	err := c.cc.Invoke(ctx, Task_GetUserTasks_FullMethodName, in, out, opts...)
	return out, err
}

func (c *taskClient) BatchTaskStatus(ctx context.Context, in *JSONRequest, opts ...grpc.CallOption) (*JSONResponse, error) {
	out := new(JSONResponse)
	err := c.cc.Invoke(ctx, Task_BatchTaskStatus_FullMethodName, in, out, opts...)
	return out, err
}

func (c *taskClient) CompleteTask(ctx context.Context, in *JSONRequest, opts ...grpc.CallOption) (*JSONResponse, error) {
	out := new(JSONResponse)
	err := c.cc.Invoke(ctx, Task_CompleteTask_FullMethodName, in, out, opts...)
	return out, err
}

func (c *taskClient) UndoCompleteTask(ctx context.Context, in *JSONRequest, opts ...grpc.CallOption) (*JSONResponse, error) {
	out := new(JSONResponse)
	err := c.cc.Invoke(ctx, Task_UndoCompleteTask_FullMethodName, in, out, opts...)
	return out, err
}

func (c *taskClient) UpdateTask(ctx context.Context, in *JSONRequest, opts ...grpc.CallOption) (*JSONResponse, error) {
	out := new(JSONResponse)
	err := c.cc.Invoke(ctx, Task_UpdateTask_FullMethodName, in, out, opts...)
	return out, err
}

func (c *taskClient) DeleteTask(ctx context.Context, in *JSONRequest, opts ...grpc.CallOption) (*JSONResponse, error) {
	out := new(JSONResponse)
	err := c.cc.Invoke(ctx, Task_DeleteTask_FullMethodName, in, out, opts...)
	return out, err
}

func (c *taskClient) GetTaskForActiveSchedule(ctx context.Context, in *JSONRequest, opts ...grpc.CallOption) (*JSONResponse, error) {
	out := new(JSONResponse)
	err := c.cc.Invoke(ctx, Task_GetTaskForActiveSchedule_FullMethodName, in, out, opts...)
	return out, err
}

type TaskServer interface {
	Ping(context.Context, *StatusResponse) (*StatusResponse, error)
	AddTask(context.Context, *JSONRequest) (*JSONResponse, error)
	GetUserTasks(context.Context, *JSONRequest) (*JSONResponse, error)
	BatchTaskStatus(context.Context, *JSONRequest) (*JSONResponse, error)
	CompleteTask(context.Context, *JSONRequest) (*JSONResponse, error)
	UndoCompleteTask(context.Context, *JSONRequest) (*JSONResponse, error)
	UpdateTask(context.Context, *JSONRequest) (*JSONResponse, error)
	DeleteTask(context.Context, *JSONRequest) (*JSONResponse, error)
	GetTaskForActiveSchedule(context.Context, *JSONRequest) (*JSONResponse, error)
}

type UnimplementedTaskServer struct{}

func (UnimplementedTaskServer) Ping(context.Context, *StatusResponse) (*StatusResponse, error) {
	return nil, status.Errorf(codes.Unimplemented, "method Ping not implemented")
}
func (UnimplementedTaskServer) AddTask(context.Context, *JSONRequest) (*JSONResponse, error) {
	return nil, status.Errorf(codes.Unimplemented, "method AddTask not implemented")
}
func (UnimplementedTaskServer) GetUserTasks(context.Context, *JSONRequest) (*JSONResponse, error) {
	return nil, status.Errorf(codes.Unimplemented, "method GetUserTasks not implemented")
}
func (UnimplementedTaskServer) BatchTaskStatus(context.Context, *JSONRequest) (*JSONResponse, error) {
	return nil, status.Errorf(codes.Unimplemented, "method BatchTaskStatus not implemented")
}
func (UnimplementedTaskServer) CompleteTask(context.Context, *JSONRequest) (*JSONResponse, error) {
	return nil, status.Errorf(codes.Unimplemented, "method CompleteTask not implemented")
}
func (UnimplementedTaskServer) UndoCompleteTask(context.Context, *JSONRequest) (*JSONResponse, error) {
	return nil, status.Errorf(codes.Unimplemented, "method UndoCompleteTask not implemented")
}
func (UnimplementedTaskServer) UpdateTask(context.Context, *JSONRequest) (*JSONResponse, error) {
	return nil, status.Errorf(codes.Unimplemented, "method UpdateTask not implemented")
}
func (UnimplementedTaskServer) DeleteTask(context.Context, *JSONRequest) (*JSONResponse, error) {
	return nil, status.Errorf(codes.Unimplemented, "method DeleteTask not implemented")
}
func (UnimplementedTaskServer) GetTaskForActiveSchedule(context.Context, *JSONRequest) (*JSONResponse, error) {
	return nil, status.Errorf(codes.Unimplemented, "method GetTaskForActiveSchedule not implemented")
}

func RegisterTaskServer(s grpc.ServiceRegistrar, srv TaskServer) {
	s.RegisterService(&Task_ServiceDesc, srv)
}

func _Task_Ping_Handler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpc.UnaryServerInterceptor) (interface{}, error) {
	in := new(StatusResponse)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(TaskServer).Ping(ctx, in)
	}
	info := &grpc.UnaryServerInfo{Server: srv, FullMethod: Task_Ping_FullMethodName}
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return srv.(TaskServer).Ping(ctx, req.(*StatusResponse))
	}
	return interceptor(ctx, in, info, handler)
}

func _Task_JSON_Handler(fullMethod string, invoke func(TaskServer, context.Context, *JSONRequest) (*JSONResponse, error)) grpc.MethodHandler {
	return func(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpc.UnaryServerInterceptor) (interface{}, error) {
		in := new(JSONRequest)
		if err := dec(in); err != nil {
			return nil, err
		}
		if interceptor == nil {
			return invoke(srv.(TaskServer), ctx, in)
		}
		info := &grpc.UnaryServerInfo{Server: srv, FullMethod: fullMethod}
		handler := func(ctx context.Context, req interface{}) (interface{}, error) {
			return invoke(srv.(TaskServer), ctx, req.(*JSONRequest))
		}
		return interceptor(ctx, in, info, handler)
	}
}

var Task_ServiceDesc = grpc.ServiceDesc{
	ServiceName: "smartflow.task.Task",
	HandlerType: (*TaskServer)(nil),
	Methods: []grpc.MethodDesc{
		{MethodName: "Ping", Handler: _Task_Ping_Handler},
		{MethodName: "AddTask", Handler: _Task_JSON_Handler(Task_AddTask_FullMethodName, TaskServer.AddTask)},
		{MethodName: "GetUserTasks", Handler: _Task_JSON_Handler(Task_GetUserTasks_FullMethodName, TaskServer.GetUserTasks)},
		{MethodName: "BatchTaskStatus", Handler: _Task_JSON_Handler(Task_BatchTaskStatus_FullMethodName, TaskServer.BatchTaskStatus)},
		{MethodName: "CompleteTask", Handler: _Task_JSON_Handler(Task_CompleteTask_FullMethodName, TaskServer.CompleteTask)},
		{MethodName: "UndoCompleteTask", Handler: _Task_JSON_Handler(Task_UndoCompleteTask_FullMethodName, TaskServer.UndoCompleteTask)},
		{MethodName: "UpdateTask", Handler: _Task_JSON_Handler(Task_UpdateTask_FullMethodName, TaskServer.UpdateTask)},
		{MethodName: "DeleteTask", Handler: _Task_JSON_Handler(Task_DeleteTask_FullMethodName, TaskServer.DeleteTask)},
		{MethodName: "GetTaskForActiveSchedule", Handler: _Task_JSON_Handler(Task_GetTaskForActiveSchedule_FullMethodName, TaskServer.GetTaskForActiveSchedule)},
	},
	Streams:  []grpc.StreamDesc{},
	Metadata: "task.proto",
}
