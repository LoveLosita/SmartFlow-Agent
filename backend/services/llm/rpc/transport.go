package rpc

import (
	"context"
	"io"

	llmcontracts "github.com/LoveLosita/smartflow/backend/shared/contracts/llm"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const (
	LLM_Ping_FullMethodName                  = "/smartflow.llm.LLM/Ping"
	LLM_GenerateText_FullMethodName          = "/smartflow.llm.LLM/GenerateText"
	LLM_StreamText_FullMethodName            = "/smartflow.llm.LLM/StreamText"
	LLM_GenerateResponsesText_FullMethodName = "/smartflow.llm.LLM/GenerateResponsesText"
)

type LLMClient interface {
	Ping(ctx context.Context, in *llmcontracts.PingRequest, opts ...grpc.CallOption) (*llmcontracts.PingResponse, error)
	GenerateText(ctx context.Context, in *llmcontracts.TextRequest, opts ...grpc.CallOption) (*llmcontracts.TextResponse, error)
	StreamText(ctx context.Context, in *llmcontracts.StreamTextRequest, opts ...grpc.CallOption) (LLM_StreamTextClient, error)
	GenerateResponsesText(ctx context.Context, in *llmcontracts.ResponsesRequest, opts ...grpc.CallOption) (*llmcontracts.ResponsesResponse, error)
}

type llmClient struct {
	cc grpc.ClientConnInterface
}

func NewLLMClient(cc grpc.ClientConnInterface) LLMClient {
	return &llmClient{cc: cc}
}

func (c *llmClient) Ping(ctx context.Context, in *llmcontracts.PingRequest, opts ...grpc.CallOption) (*llmcontracts.PingResponse, error) {
	out := new(llmcontracts.PingResponse)
	err := c.cc.Invoke(ctx, LLM_Ping_FullMethodName, in, out, opts...)
	return out, err
}

func (c *llmClient) GenerateText(ctx context.Context, in *llmcontracts.TextRequest, opts ...grpc.CallOption) (*llmcontracts.TextResponse, error) {
	out := new(llmcontracts.TextResponse)
	err := c.cc.Invoke(ctx, LLM_GenerateText_FullMethodName, in, out, opts...)
	return out, err
}

func (c *llmClient) StreamText(ctx context.Context, in *llmcontracts.StreamTextRequest, opts ...grpc.CallOption) (LLM_StreamTextClient, error) {
	stream, err := c.cc.NewStream(ctx, &LLM_ServiceDesc.Streams[0], LLM_StreamText_FullMethodName, opts...)
	if err != nil {
		return nil, err
	}
	client := &llmStreamTextClient{ClientStream: stream}
	if err = client.SendMsg(in); err != nil {
		return nil, err
	}
	if err = client.CloseSend(); err != nil {
		return nil, err
	}
	return client, nil
}

func (c *llmClient) GenerateResponsesText(ctx context.Context, in *llmcontracts.ResponsesRequest, opts ...grpc.CallOption) (*llmcontracts.ResponsesResponse, error) {
	out := new(llmcontracts.ResponsesResponse)
	err := c.cc.Invoke(ctx, LLM_GenerateResponsesText_FullMethodName, in, out, opts...)
	return out, err
}

type LLM_StreamTextClient interface {
	Recv() (*llmcontracts.StreamChunk, error)
	grpc.ClientStream
}

type llmStreamTextClient struct {
	grpc.ClientStream
}

func (x *llmStreamTextClient) Recv() (*llmcontracts.StreamChunk, error) {
	m := new(llmcontracts.StreamChunk)
	if err := x.ClientStream.RecvMsg(m); err != nil {
		if err == io.EOF {
			return nil, err
		}
		return nil, err
	}
	return m, nil
}

type LLMServer interface {
	Ping(context.Context, *llmcontracts.PingRequest) (*llmcontracts.PingResponse, error)
	GenerateText(context.Context, *llmcontracts.TextRequest) (*llmcontracts.TextResponse, error)
	StreamText(*llmcontracts.StreamTextRequest, LLM_StreamTextServer) error
	GenerateResponsesText(context.Context, *llmcontracts.ResponsesRequest) (*llmcontracts.ResponsesResponse, error)
}

type UnimplementedLLMServer struct{}

func (UnimplementedLLMServer) Ping(context.Context, *llmcontracts.PingRequest) (*llmcontracts.PingResponse, error) {
	return nil, status.Errorf(codes.Unimplemented, "method Ping not implemented")
}

func (UnimplementedLLMServer) GenerateText(context.Context, *llmcontracts.TextRequest) (*llmcontracts.TextResponse, error) {
	return nil, status.Errorf(codes.Unimplemented, "method GenerateText not implemented")
}

func (UnimplementedLLMServer) StreamText(*llmcontracts.StreamTextRequest, LLM_StreamTextServer) error {
	return status.Errorf(codes.Unimplemented, "method StreamText not implemented")
}

func (UnimplementedLLMServer) GenerateResponsesText(context.Context, *llmcontracts.ResponsesRequest) (*llmcontracts.ResponsesResponse, error) {
	return nil, status.Errorf(codes.Unimplemented, "method GenerateResponsesText not implemented")
}

func RegisterLLMServer(s grpc.ServiceRegistrar, srv LLMServer) {
	s.RegisterService(&LLM_ServiceDesc, srv)
}

type LLM_StreamTextServer interface {
	Send(*llmcontracts.StreamChunk) error
	grpc.ServerStream
}

type llmStreamTextServer struct {
	grpc.ServerStream
}

func (x *llmStreamTextServer) Send(m *llmcontracts.StreamChunk) error {
	return x.ServerStream.SendMsg(m)
}

func _LLM_Ping_Handler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpc.UnaryServerInterceptor) (interface{}, error) {
	in := new(llmcontracts.PingRequest)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(LLMServer).Ping(ctx, in)
	}
	info := &grpc.UnaryServerInfo{Server: srv, FullMethod: LLM_Ping_FullMethodName}
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return srv.(LLMServer).Ping(ctx, req.(*llmcontracts.PingRequest))
	}
	return interceptor(ctx, in, info, handler)
}

func _LLM_GenerateText_Handler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpc.UnaryServerInterceptor) (interface{}, error) {
	in := new(llmcontracts.TextRequest)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(LLMServer).GenerateText(ctx, in)
	}
	info := &grpc.UnaryServerInfo{Server: srv, FullMethod: LLM_GenerateText_FullMethodName}
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return srv.(LLMServer).GenerateText(ctx, req.(*llmcontracts.TextRequest))
	}
	return interceptor(ctx, in, info, handler)
}

func _LLM_GenerateResponsesText_Handler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpc.UnaryServerInterceptor) (interface{}, error) {
	in := new(llmcontracts.ResponsesRequest)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(LLMServer).GenerateResponsesText(ctx, in)
	}
	info := &grpc.UnaryServerInfo{Server: srv, FullMethod: LLM_GenerateResponsesText_FullMethodName}
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return srv.(LLMServer).GenerateResponsesText(ctx, req.(*llmcontracts.ResponsesRequest))
	}
	return interceptor(ctx, in, info, handler)
}

func _LLM_StreamText_Handler(srv interface{}, stream grpc.ServerStream) error {
	m := new(llmcontracts.StreamTextRequest)
	if err := stream.RecvMsg(m); err != nil {
		return err
	}
	return srv.(LLMServer).StreamText(m, &llmStreamTextServer{ServerStream: stream})
}

var LLM_ServiceDesc = grpc.ServiceDesc{
	ServiceName: "smartflow.llm.LLM",
	HandlerType: (*LLMServer)(nil),
	Methods: []grpc.MethodDesc{
		{MethodName: "Ping", Handler: _LLM_Ping_Handler},
		{MethodName: "GenerateText", Handler: _LLM_GenerateText_Handler},
		{MethodName: "GenerateResponsesText", Handler: _LLM_GenerateResponsesText_Handler},
	},
	Streams: []grpc.StreamDesc{
		{StreamName: "StreamText", Handler: _LLM_StreamText_Handler, ServerStreams: true},
	},
	Metadata: "services/llm/rpc/llm.proto",
}
