package pb

import (
	context "context"
	io "io"

	grpc "google.golang.org/grpc"
	codes "google.golang.org/grpc/codes"
	status "google.golang.org/grpc/status"
)

const (
	Agent_Ping_FullMethodName                    = "/smartflow.agent.Agent/Ping"
	Agent_Chat_FullMethodName                    = "/smartflow.agent.Agent/Chat"
	Agent_GetConversationMeta_FullMethodName     = "/smartflow.agent.Agent/GetConversationMeta"
	Agent_GetConversationList_FullMethodName     = "/smartflow.agent.Agent/GetConversationList"
	Agent_GetConversationTimeline_FullMethodName = "/smartflow.agent.Agent/GetConversationTimeline"
	Agent_GetSchedulePlanPreview_FullMethodName  = "/smartflow.agent.Agent/GetSchedulePlanPreview"
	Agent_GetContextStats_FullMethodName         = "/smartflow.agent.Agent/GetContextStats"
	Agent_SaveScheduleState_FullMethodName       = "/smartflow.agent.Agent/SaveScheduleState"
)

type AgentClient interface {
	Ping(ctx context.Context, in *StatusResponse, opts ...grpc.CallOption) (*StatusResponse, error)
	Chat(ctx context.Context, in *ChatRequest, opts ...grpc.CallOption) (Agent_ChatClient, error)
	GetConversationMeta(ctx context.Context, in *JSONRequest, opts ...grpc.CallOption) (*JSONResponse, error)
	GetConversationList(ctx context.Context, in *JSONRequest, opts ...grpc.CallOption) (*JSONResponse, error)
	GetConversationTimeline(ctx context.Context, in *JSONRequest, opts ...grpc.CallOption) (*JSONResponse, error)
	GetSchedulePlanPreview(ctx context.Context, in *JSONRequest, opts ...grpc.CallOption) (*JSONResponse, error)
	GetContextStats(ctx context.Context, in *JSONRequest, opts ...grpc.CallOption) (*JSONResponse, error)
	SaveScheduleState(ctx context.Context, in *JSONRequest, opts ...grpc.CallOption) (*JSONResponse, error)
}

type agentClient struct {
	cc grpc.ClientConnInterface
}

func NewAgentClient(cc grpc.ClientConnInterface) AgentClient {
	return &agentClient{cc}
}

func (c *agentClient) Ping(ctx context.Context, in *StatusResponse, opts ...grpc.CallOption) (*StatusResponse, error) {
	out := new(StatusResponse)
	err := c.cc.Invoke(ctx, Agent_Ping_FullMethodName, in, out, opts...)
	return out, err
}

func (c *agentClient) Chat(ctx context.Context, in *ChatRequest, opts ...grpc.CallOption) (Agent_ChatClient, error) {
	stream, err := c.cc.NewStream(ctx, &Agent_ServiceDesc.Streams[0], Agent_Chat_FullMethodName, opts...)
	if err != nil {
		return nil, err
	}
	client := &agentChatClient{stream}
	if err := client.ClientStream.SendMsg(in); err != nil {
		return nil, err
	}
	if err := client.ClientStream.CloseSend(); err != nil {
		return nil, err
	}
	return client, nil
}

func (c *agentClient) GetConversationMeta(ctx context.Context, in *JSONRequest, opts ...grpc.CallOption) (*JSONResponse, error) {
	out := new(JSONResponse)
	err := c.cc.Invoke(ctx, Agent_GetConversationMeta_FullMethodName, in, out, opts...)
	return out, err
}

func (c *agentClient) GetConversationList(ctx context.Context, in *JSONRequest, opts ...grpc.CallOption) (*JSONResponse, error) {
	out := new(JSONResponse)
	err := c.cc.Invoke(ctx, Agent_GetConversationList_FullMethodName, in, out, opts...)
	return out, err
}

func (c *agentClient) GetConversationTimeline(ctx context.Context, in *JSONRequest, opts ...grpc.CallOption) (*JSONResponse, error) {
	out := new(JSONResponse)
	err := c.cc.Invoke(ctx, Agent_GetConversationTimeline_FullMethodName, in, out, opts...)
	return out, err
}

func (c *agentClient) GetSchedulePlanPreview(ctx context.Context, in *JSONRequest, opts ...grpc.CallOption) (*JSONResponse, error) {
	out := new(JSONResponse)
	err := c.cc.Invoke(ctx, Agent_GetSchedulePlanPreview_FullMethodName, in, out, opts...)
	return out, err
}

func (c *agentClient) GetContextStats(ctx context.Context, in *JSONRequest, opts ...grpc.CallOption) (*JSONResponse, error) {
	out := new(JSONResponse)
	err := c.cc.Invoke(ctx, Agent_GetContextStats_FullMethodName, in, out, opts...)
	return out, err
}

func (c *agentClient) SaveScheduleState(ctx context.Context, in *JSONRequest, opts ...grpc.CallOption) (*JSONResponse, error) {
	out := new(JSONResponse)
	err := c.cc.Invoke(ctx, Agent_SaveScheduleState_FullMethodName, in, out, opts...)
	return out, err
}

type Agent_ChatClient interface {
	Recv() (*ChatChunk, error)
	grpc.ClientStream
}

type agentChatClient struct {
	grpc.ClientStream
}

func (x *agentChatClient) Recv() (*ChatChunk, error) {
	m := new(ChatChunk)
	if err := x.ClientStream.RecvMsg(m); err != nil {
		if err == io.EOF {
			return nil, err
		}
		return nil, err
	}
	return m, nil
}

type AgentServer interface {
	Ping(context.Context, *StatusResponse) (*StatusResponse, error)
	Chat(*ChatRequest, Agent_ChatServer) error
	GetConversationMeta(context.Context, *JSONRequest) (*JSONResponse, error)
	GetConversationList(context.Context, *JSONRequest) (*JSONResponse, error)
	GetConversationTimeline(context.Context, *JSONRequest) (*JSONResponse, error)
	GetSchedulePlanPreview(context.Context, *JSONRequest) (*JSONResponse, error)
	GetContextStats(context.Context, *JSONRequest) (*JSONResponse, error)
	SaveScheduleState(context.Context, *JSONRequest) (*JSONResponse, error)
}

type UnimplementedAgentServer struct{}

func (UnimplementedAgentServer) Ping(context.Context, *StatusResponse) (*StatusResponse, error) {
	return nil, status.Errorf(codes.Unimplemented, "method Ping not implemented")
}

func (UnimplementedAgentServer) Chat(*ChatRequest, Agent_ChatServer) error {
	return status.Errorf(codes.Unimplemented, "method Chat not implemented")
}

func (UnimplementedAgentServer) GetConversationMeta(context.Context, *JSONRequest) (*JSONResponse, error) {
	return nil, status.Errorf(codes.Unimplemented, "method GetConversationMeta not implemented")
}

func (UnimplementedAgentServer) GetConversationList(context.Context, *JSONRequest) (*JSONResponse, error) {
	return nil, status.Errorf(codes.Unimplemented, "method GetConversationList not implemented")
}

func (UnimplementedAgentServer) GetConversationTimeline(context.Context, *JSONRequest) (*JSONResponse, error) {
	return nil, status.Errorf(codes.Unimplemented, "method GetConversationTimeline not implemented")
}

func (UnimplementedAgentServer) GetSchedulePlanPreview(context.Context, *JSONRequest) (*JSONResponse, error) {
	return nil, status.Errorf(codes.Unimplemented, "method GetSchedulePlanPreview not implemented")
}

func (UnimplementedAgentServer) GetContextStats(context.Context, *JSONRequest) (*JSONResponse, error) {
	return nil, status.Errorf(codes.Unimplemented, "method GetContextStats not implemented")
}

func (UnimplementedAgentServer) SaveScheduleState(context.Context, *JSONRequest) (*JSONResponse, error) {
	return nil, status.Errorf(codes.Unimplemented, "method SaveScheduleState not implemented")
}

func RegisterAgentServer(s grpc.ServiceRegistrar, srv AgentServer) {
	s.RegisterService(&Agent_ServiceDesc, srv)
}

type Agent_ChatServer interface {
	Send(*ChatChunk) error
	grpc.ServerStream
}

type agentChatServer struct {
	grpc.ServerStream
}

func (x *agentChatServer) Send(m *ChatChunk) error {
	return x.ServerStream.SendMsg(m)
}

func _Agent_Ping_Handler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpc.UnaryServerInterceptor) (interface{}, error) {
	in := new(StatusResponse)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(AgentServer).Ping(ctx, in)
	}
	info := &grpc.UnaryServerInfo{
		Server:     srv,
		FullMethod: Agent_Ping_FullMethodName,
	}
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return srv.(AgentServer).Ping(ctx, req.(*StatusResponse))
	}
	return interceptor(ctx, in, info, handler)
}

func _Agent_GetConversationMeta_Handler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpc.UnaryServerInterceptor) (interface{}, error) {
	in := new(JSONRequest)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(AgentServer).GetConversationMeta(ctx, in)
	}
	info := &grpc.UnaryServerInfo{Server: srv, FullMethod: Agent_GetConversationMeta_FullMethodName}
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return srv.(AgentServer).GetConversationMeta(ctx, req.(*JSONRequest))
	}
	return interceptor(ctx, in, info, handler)
}

func _Agent_GetConversationList_Handler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpc.UnaryServerInterceptor) (interface{}, error) {
	in := new(JSONRequest)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(AgentServer).GetConversationList(ctx, in)
	}
	info := &grpc.UnaryServerInfo{Server: srv, FullMethod: Agent_GetConversationList_FullMethodName}
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return srv.(AgentServer).GetConversationList(ctx, req.(*JSONRequest))
	}
	return interceptor(ctx, in, info, handler)
}

func _Agent_GetConversationTimeline_Handler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpc.UnaryServerInterceptor) (interface{}, error) {
	in := new(JSONRequest)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(AgentServer).GetConversationTimeline(ctx, in)
	}
	info := &grpc.UnaryServerInfo{Server: srv, FullMethod: Agent_GetConversationTimeline_FullMethodName}
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return srv.(AgentServer).GetConversationTimeline(ctx, req.(*JSONRequest))
	}
	return interceptor(ctx, in, info, handler)
}

func _Agent_GetSchedulePlanPreview_Handler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpc.UnaryServerInterceptor) (interface{}, error) {
	in := new(JSONRequest)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(AgentServer).GetSchedulePlanPreview(ctx, in)
	}
	info := &grpc.UnaryServerInfo{Server: srv, FullMethod: Agent_GetSchedulePlanPreview_FullMethodName}
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return srv.(AgentServer).GetSchedulePlanPreview(ctx, req.(*JSONRequest))
	}
	return interceptor(ctx, in, info, handler)
}

func _Agent_GetContextStats_Handler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpc.UnaryServerInterceptor) (interface{}, error) {
	in := new(JSONRequest)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(AgentServer).GetContextStats(ctx, in)
	}
	info := &grpc.UnaryServerInfo{Server: srv, FullMethod: Agent_GetContextStats_FullMethodName}
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return srv.(AgentServer).GetContextStats(ctx, req.(*JSONRequest))
	}
	return interceptor(ctx, in, info, handler)
}

func _Agent_SaveScheduleState_Handler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpc.UnaryServerInterceptor) (interface{}, error) {
	in := new(JSONRequest)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(AgentServer).SaveScheduleState(ctx, in)
	}
	info := &grpc.UnaryServerInfo{Server: srv, FullMethod: Agent_SaveScheduleState_FullMethodName}
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return srv.(AgentServer).SaveScheduleState(ctx, req.(*JSONRequest))
	}
	return interceptor(ctx, in, info, handler)
}

func _Agent_Chat_Handler(srv interface{}, stream grpc.ServerStream) error {
	m := new(ChatRequest)
	if err := stream.RecvMsg(m); err != nil {
		return err
	}
	return srv.(AgentServer).Chat(m, &agentChatServer{stream})
}

var Agent_ServiceDesc = grpc.ServiceDesc{
	ServiceName: "smartflow.agent.Agent",
	HandlerType: (*AgentServer)(nil),
	Methods: []grpc.MethodDesc{
		{MethodName: "Ping", Handler: _Agent_Ping_Handler},
		{MethodName: "GetConversationMeta", Handler: _Agent_GetConversationMeta_Handler},
		{MethodName: "GetConversationList", Handler: _Agent_GetConversationList_Handler},
		{MethodName: "GetConversationTimeline", Handler: _Agent_GetConversationTimeline_Handler},
		{MethodName: "GetSchedulePlanPreview", Handler: _Agent_GetSchedulePlanPreview_Handler},
		{MethodName: "GetContextStats", Handler: _Agent_GetContextStats_Handler},
		{MethodName: "SaveScheduleState", Handler: _Agent_SaveScheduleState_Handler},
	},
	Streams: []grpc.StreamDesc{
		{StreamName: "Chat", Handler: _Agent_Chat_Handler, ServerStreams: true},
	},
	Metadata: "services/agent/rpc/agent.proto",
}
