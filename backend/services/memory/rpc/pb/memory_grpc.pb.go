package pb

import (
	context "context"

	grpc "google.golang.org/grpc"
	codes "google.golang.org/grpc/codes"
	status "google.golang.org/grpc/status"
)

const (
	Memory_Ping_FullMethodName        = "/smartflow.memory.Memory/Ping"
	Memory_Retrieve_FullMethodName    = "/smartflow.memory.Memory/Retrieve"
	Memory_ListItems_FullMethodName   = "/smartflow.memory.Memory/ListItems"
	Memory_GetItem_FullMethodName     = "/smartflow.memory.Memory/GetItem"
	Memory_CreateItem_FullMethodName  = "/smartflow.memory.Memory/CreateItem"
	Memory_UpdateItem_FullMethodName  = "/smartflow.memory.Memory/UpdateItem"
	Memory_DeleteItem_FullMethodName  = "/smartflow.memory.Memory/DeleteItem"
	Memory_RestoreItem_FullMethodName = "/smartflow.memory.Memory/RestoreItem"
)

type MemoryClient interface {
	Ping(ctx context.Context, in *StatusResponse, opts ...grpc.CallOption) (*StatusResponse, error)
	Retrieve(ctx context.Context, in *JSONRequest, opts ...grpc.CallOption) (*JSONResponse, error)
	ListItems(ctx context.Context, in *JSONRequest, opts ...grpc.CallOption) (*JSONResponse, error)
	GetItem(ctx context.Context, in *JSONRequest, opts ...grpc.CallOption) (*JSONResponse, error)
	CreateItem(ctx context.Context, in *JSONRequest, opts ...grpc.CallOption) (*JSONResponse, error)
	UpdateItem(ctx context.Context, in *JSONRequest, opts ...grpc.CallOption) (*JSONResponse, error)
	DeleteItem(ctx context.Context, in *JSONRequest, opts ...grpc.CallOption) (*JSONResponse, error)
	RestoreItem(ctx context.Context, in *JSONRequest, opts ...grpc.CallOption) (*JSONResponse, error)
}

type memoryClient struct {
	cc grpc.ClientConnInterface
}

func NewMemoryClient(cc grpc.ClientConnInterface) MemoryClient {
	return &memoryClient{cc}
}

func (c *memoryClient) Ping(ctx context.Context, in *StatusResponse, opts ...grpc.CallOption) (*StatusResponse, error) {
	out := new(StatusResponse)
	err := c.cc.Invoke(ctx, Memory_Ping_FullMethodName, in, out, opts...)
	return out, err
}

func (c *memoryClient) Retrieve(ctx context.Context, in *JSONRequest, opts ...grpc.CallOption) (*JSONResponse, error) {
	out := new(JSONResponse)
	err := c.cc.Invoke(ctx, Memory_Retrieve_FullMethodName, in, out, opts...)
	return out, err
}

func (c *memoryClient) ListItems(ctx context.Context, in *JSONRequest, opts ...grpc.CallOption) (*JSONResponse, error) {
	out := new(JSONResponse)
	err := c.cc.Invoke(ctx, Memory_ListItems_FullMethodName, in, out, opts...)
	return out, err
}

func (c *memoryClient) GetItem(ctx context.Context, in *JSONRequest, opts ...grpc.CallOption) (*JSONResponse, error) {
	out := new(JSONResponse)
	err := c.cc.Invoke(ctx, Memory_GetItem_FullMethodName, in, out, opts...)
	return out, err
}

func (c *memoryClient) CreateItem(ctx context.Context, in *JSONRequest, opts ...grpc.CallOption) (*JSONResponse, error) {
	out := new(JSONResponse)
	err := c.cc.Invoke(ctx, Memory_CreateItem_FullMethodName, in, out, opts...)
	return out, err
}

func (c *memoryClient) UpdateItem(ctx context.Context, in *JSONRequest, opts ...grpc.CallOption) (*JSONResponse, error) {
	out := new(JSONResponse)
	err := c.cc.Invoke(ctx, Memory_UpdateItem_FullMethodName, in, out, opts...)
	return out, err
}

func (c *memoryClient) DeleteItem(ctx context.Context, in *JSONRequest, opts ...grpc.CallOption) (*JSONResponse, error) {
	out := new(JSONResponse)
	err := c.cc.Invoke(ctx, Memory_DeleteItem_FullMethodName, in, out, opts...)
	return out, err
}

func (c *memoryClient) RestoreItem(ctx context.Context, in *JSONRequest, opts ...grpc.CallOption) (*JSONResponse, error) {
	out := new(JSONResponse)
	err := c.cc.Invoke(ctx, Memory_RestoreItem_FullMethodName, in, out, opts...)
	return out, err
}

type MemoryServer interface {
	Ping(context.Context, *StatusResponse) (*StatusResponse, error)
	Retrieve(context.Context, *JSONRequest) (*JSONResponse, error)
	ListItems(context.Context, *JSONRequest) (*JSONResponse, error)
	GetItem(context.Context, *JSONRequest) (*JSONResponse, error)
	CreateItem(context.Context, *JSONRequest) (*JSONResponse, error)
	UpdateItem(context.Context, *JSONRequest) (*JSONResponse, error)
	DeleteItem(context.Context, *JSONRequest) (*JSONResponse, error)
	RestoreItem(context.Context, *JSONRequest) (*JSONResponse, error)
}

type UnimplementedMemoryServer struct{}

func (UnimplementedMemoryServer) Ping(context.Context, *StatusResponse) (*StatusResponse, error) {
	return nil, status.Errorf(codes.Unimplemented, "method Ping not implemented")
}
func (UnimplementedMemoryServer) Retrieve(context.Context, *JSONRequest) (*JSONResponse, error) {
	return nil, status.Errorf(codes.Unimplemented, "method Retrieve not implemented")
}
func (UnimplementedMemoryServer) ListItems(context.Context, *JSONRequest) (*JSONResponse, error) {
	return nil, status.Errorf(codes.Unimplemented, "method ListItems not implemented")
}
func (UnimplementedMemoryServer) GetItem(context.Context, *JSONRequest) (*JSONResponse, error) {
	return nil, status.Errorf(codes.Unimplemented, "method GetItem not implemented")
}
func (UnimplementedMemoryServer) CreateItem(context.Context, *JSONRequest) (*JSONResponse, error) {
	return nil, status.Errorf(codes.Unimplemented, "method CreateItem not implemented")
}
func (UnimplementedMemoryServer) UpdateItem(context.Context, *JSONRequest) (*JSONResponse, error) {
	return nil, status.Errorf(codes.Unimplemented, "method UpdateItem not implemented")
}
func (UnimplementedMemoryServer) DeleteItem(context.Context, *JSONRequest) (*JSONResponse, error) {
	return nil, status.Errorf(codes.Unimplemented, "method DeleteItem not implemented")
}
func (UnimplementedMemoryServer) RestoreItem(context.Context, *JSONRequest) (*JSONResponse, error) {
	return nil, status.Errorf(codes.Unimplemented, "method RestoreItem not implemented")
}

func RegisterMemoryServer(s grpc.ServiceRegistrar, srv MemoryServer) {
	s.RegisterService(&Memory_ServiceDesc, srv)
}

func _Memory_Ping_Handler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpc.UnaryServerInterceptor) (interface{}, error) {
	in := new(StatusResponse)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(MemoryServer).Ping(ctx, in)
	}
	info := &grpc.UnaryServerInfo{
		Server:     srv,
		FullMethod: Memory_Ping_FullMethodName,
	}
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return srv.(MemoryServer).Ping(ctx, req.(*StatusResponse))
	}
	return interceptor(ctx, in, info, handler)
}

func _Memory_JSON_Handler(fullMethod string, invoke func(MemoryServer, context.Context, *JSONRequest) (*JSONResponse, error)) grpc.MethodHandler {
	return func(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpc.UnaryServerInterceptor) (interface{}, error) {
		in := new(JSONRequest)
		if err := dec(in); err != nil {
			return nil, err
		}
		if interceptor == nil {
			return invoke(srv.(MemoryServer), ctx, in)
		}
		info := &grpc.UnaryServerInfo{Server: srv, FullMethod: fullMethod}
		handler := func(ctx context.Context, req interface{}) (interface{}, error) {
			return invoke(srv.(MemoryServer), ctx, req.(*JSONRequest))
		}
		return interceptor(ctx, in, info, handler)
	}
}

var Memory_ServiceDesc = grpc.ServiceDesc{
	ServiceName: "smartflow.memory.Memory",
	HandlerType: (*MemoryServer)(nil),
	Methods: []grpc.MethodDesc{
		{MethodName: "Ping", Handler: _Memory_Ping_Handler},
		{MethodName: "Retrieve", Handler: _Memory_JSON_Handler(Memory_Retrieve_FullMethodName, MemoryServer.Retrieve)},
		{MethodName: "ListItems", Handler: _Memory_JSON_Handler(Memory_ListItems_FullMethodName, MemoryServer.ListItems)},
		{MethodName: "GetItem", Handler: _Memory_JSON_Handler(Memory_GetItem_FullMethodName, MemoryServer.GetItem)},
		{MethodName: "CreateItem", Handler: _Memory_JSON_Handler(Memory_CreateItem_FullMethodName, MemoryServer.CreateItem)},
		{MethodName: "UpdateItem", Handler: _Memory_JSON_Handler(Memory_UpdateItem_FullMethodName, MemoryServer.UpdateItem)},
		{MethodName: "DeleteItem", Handler: _Memory_JSON_Handler(Memory_DeleteItem_FullMethodName, MemoryServer.DeleteItem)},
		{MethodName: "RestoreItem", Handler: _Memory_JSON_Handler(Memory_RestoreItem_FullMethodName, MemoryServer.RestoreItem)},
	},
	Streams:  []grpc.StreamDesc{},
	Metadata: "services/memory/rpc/memory.proto",
}
