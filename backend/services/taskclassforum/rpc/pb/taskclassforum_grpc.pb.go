package pb

import (
	context "context"

	grpc "google.golang.org/grpc"
	codes "google.golang.org/grpc/codes"
	status "google.golang.org/grpc/status"
)

const (
	TaskClassForumService_ListPosts_FullMethodName     = "/smartflow.taskclassforum.TaskClassForumService/ListPosts"
	TaskClassForumService_ListTags_FullMethodName      = "/smartflow.taskclassforum.TaskClassForumService/ListTags"
	TaskClassForumService_CreatePost_FullMethodName    = "/smartflow.taskclassforum.TaskClassForumService/CreatePost"
	TaskClassForumService_GetPost_FullMethodName       = "/smartflow.taskclassforum.TaskClassForumService/GetPost"
	TaskClassForumService_LikePost_FullMethodName      = "/smartflow.taskclassforum.TaskClassForumService/LikePost"
	TaskClassForumService_UnlikePost_FullMethodName    = "/smartflow.taskclassforum.TaskClassForumService/UnlikePost"
	TaskClassForumService_ListComments_FullMethodName  = "/smartflow.taskclassforum.TaskClassForumService/ListComments"
	TaskClassForumService_CreateComment_FullMethodName = "/smartflow.taskclassforum.TaskClassForumService/CreateComment"
	TaskClassForumService_DeleteComment_FullMethodName = "/smartflow.taskclassforum.TaskClassForumService/DeleteComment"
	TaskClassForumService_ImportPost_FullMethodName    = "/smartflow.taskclassforum.TaskClassForumService/ImportPost"
)

type TaskClassForumServiceClient interface {
	ListPosts(ctx context.Context, in *ListForumPostsRequest, opts ...grpc.CallOption) (*ListForumPostsResponse, error)
	ListTags(ctx context.Context, in *ListForumTagsRequest, opts ...grpc.CallOption) (*ListForumTagsResponse, error)
	CreatePost(ctx context.Context, in *CreateForumPostRequest, opts ...grpc.CallOption) (*CreateForumPostResponse, error)
	GetPost(ctx context.Context, in *GetForumPostRequest, opts ...grpc.CallOption) (*GetForumPostResponse, error)
	LikePost(ctx context.Context, in *LikeForumPostRequest, opts ...grpc.CallOption) (*LikeForumPostResponse, error)
	UnlikePost(ctx context.Context, in *UnlikeForumPostRequest, opts ...grpc.CallOption) (*UnlikeForumPostResponse, error)
	ListComments(ctx context.Context, in *ListForumCommentsRequest, opts ...grpc.CallOption) (*ListForumCommentsResponse, error)
	CreateComment(ctx context.Context, in *CreateForumCommentRequest, opts ...grpc.CallOption) (*CreateForumCommentResponse, error)
	DeleteComment(ctx context.Context, in *DeleteForumCommentRequest, opts ...grpc.CallOption) (*DeleteForumCommentResponse, error)
	ImportPost(ctx context.Context, in *ImportForumPostRequest, opts ...grpc.CallOption) (*ImportForumPostResponse, error)
}

type taskClassForumServiceClient struct {
	cc grpc.ClientConnInterface
}

func NewTaskClassForumServiceClient(cc grpc.ClientConnInterface) TaskClassForumServiceClient {
	return &taskClassForumServiceClient{cc}
}

func (c *taskClassForumServiceClient) ListPosts(ctx context.Context, in *ListForumPostsRequest, opts ...grpc.CallOption) (*ListForumPostsResponse, error) {
	return invokeTaskClassForum[ListForumPostsResponse](ctx, c.cc, TaskClassForumService_ListPosts_FullMethodName, in, opts...)
}

func (c *taskClassForumServiceClient) ListTags(ctx context.Context, in *ListForumTagsRequest, opts ...grpc.CallOption) (*ListForumTagsResponse, error) {
	return invokeTaskClassForum[ListForumTagsResponse](ctx, c.cc, TaskClassForumService_ListTags_FullMethodName, in, opts...)
}

func (c *taskClassForumServiceClient) CreatePost(ctx context.Context, in *CreateForumPostRequest, opts ...grpc.CallOption) (*CreateForumPostResponse, error) {
	return invokeTaskClassForum[CreateForumPostResponse](ctx, c.cc, TaskClassForumService_CreatePost_FullMethodName, in, opts...)
}

func (c *taskClassForumServiceClient) GetPost(ctx context.Context, in *GetForumPostRequest, opts ...grpc.CallOption) (*GetForumPostResponse, error) {
	return invokeTaskClassForum[GetForumPostResponse](ctx, c.cc, TaskClassForumService_GetPost_FullMethodName, in, opts...)
}

func (c *taskClassForumServiceClient) LikePost(ctx context.Context, in *LikeForumPostRequest, opts ...grpc.CallOption) (*LikeForumPostResponse, error) {
	return invokeTaskClassForum[LikeForumPostResponse](ctx, c.cc, TaskClassForumService_LikePost_FullMethodName, in, opts...)
}

func (c *taskClassForumServiceClient) UnlikePost(ctx context.Context, in *UnlikeForumPostRequest, opts ...grpc.CallOption) (*UnlikeForumPostResponse, error) {
	return invokeTaskClassForum[UnlikeForumPostResponse](ctx, c.cc, TaskClassForumService_UnlikePost_FullMethodName, in, opts...)
}

func (c *taskClassForumServiceClient) ListComments(ctx context.Context, in *ListForumCommentsRequest, opts ...grpc.CallOption) (*ListForumCommentsResponse, error) {
	return invokeTaskClassForum[ListForumCommentsResponse](ctx, c.cc, TaskClassForumService_ListComments_FullMethodName, in, opts...)
}

func (c *taskClassForumServiceClient) CreateComment(ctx context.Context, in *CreateForumCommentRequest, opts ...grpc.CallOption) (*CreateForumCommentResponse, error) {
	return invokeTaskClassForum[CreateForumCommentResponse](ctx, c.cc, TaskClassForumService_CreateComment_FullMethodName, in, opts...)
}

func (c *taskClassForumServiceClient) DeleteComment(ctx context.Context, in *DeleteForumCommentRequest, opts ...grpc.CallOption) (*DeleteForumCommentResponse, error) {
	return invokeTaskClassForum[DeleteForumCommentResponse](ctx, c.cc, TaskClassForumService_DeleteComment_FullMethodName, in, opts...)
}

func (c *taskClassForumServiceClient) ImportPost(ctx context.Context, in *ImportForumPostRequest, opts ...grpc.CallOption) (*ImportForumPostResponse, error) {
	return invokeTaskClassForum[ImportForumPostResponse](ctx, c.cc, TaskClassForumService_ImportPost_FullMethodName, in, opts...)
}

func invokeTaskClassForum[Resp any](ctx context.Context, cc grpc.ClientConnInterface, fullMethod string, in interface{}, opts ...grpc.CallOption) (*Resp, error) {
	out := new(Resp)
	err := cc.Invoke(ctx, fullMethod, in, out, opts...)
	if err != nil {
		return nil, err
	}
	return out, nil
}

type TaskClassForumServiceServer interface {
	ListPosts(context.Context, *ListForumPostsRequest) (*ListForumPostsResponse, error)
	ListTags(context.Context, *ListForumTagsRequest) (*ListForumTagsResponse, error)
	CreatePost(context.Context, *CreateForumPostRequest) (*CreateForumPostResponse, error)
	GetPost(context.Context, *GetForumPostRequest) (*GetForumPostResponse, error)
	LikePost(context.Context, *LikeForumPostRequest) (*LikeForumPostResponse, error)
	UnlikePost(context.Context, *UnlikeForumPostRequest) (*UnlikeForumPostResponse, error)
	ListComments(context.Context, *ListForumCommentsRequest) (*ListForumCommentsResponse, error)
	CreateComment(context.Context, *CreateForumCommentRequest) (*CreateForumCommentResponse, error)
	DeleteComment(context.Context, *DeleteForumCommentRequest) (*DeleteForumCommentResponse, error)
	ImportPost(context.Context, *ImportForumPostRequest) (*ImportForumPostResponse, error)
}

type UnimplementedTaskClassForumServiceServer struct{}

func (UnimplementedTaskClassForumServiceServer) ListPosts(context.Context, *ListForumPostsRequest) (*ListForumPostsResponse, error) {
	return nil, status.Errorf(codes.Unimplemented, "method ListPosts not implemented")
}

func (UnimplementedTaskClassForumServiceServer) ListTags(context.Context, *ListForumTagsRequest) (*ListForumTagsResponse, error) {
	return nil, status.Errorf(codes.Unimplemented, "method ListTags not implemented")
}

func (UnimplementedTaskClassForumServiceServer) CreatePost(context.Context, *CreateForumPostRequest) (*CreateForumPostResponse, error) {
	return nil, status.Errorf(codes.Unimplemented, "method CreatePost not implemented")
}

func (UnimplementedTaskClassForumServiceServer) GetPost(context.Context, *GetForumPostRequest) (*GetForumPostResponse, error) {
	return nil, status.Errorf(codes.Unimplemented, "method GetPost not implemented")
}

func (UnimplementedTaskClassForumServiceServer) LikePost(context.Context, *LikeForumPostRequest) (*LikeForumPostResponse, error) {
	return nil, status.Errorf(codes.Unimplemented, "method LikePost not implemented")
}

func (UnimplementedTaskClassForumServiceServer) UnlikePost(context.Context, *UnlikeForumPostRequest) (*UnlikeForumPostResponse, error) {
	return nil, status.Errorf(codes.Unimplemented, "method UnlikePost not implemented")
}

func (UnimplementedTaskClassForumServiceServer) ListComments(context.Context, *ListForumCommentsRequest) (*ListForumCommentsResponse, error) {
	return nil, status.Errorf(codes.Unimplemented, "method ListComments not implemented")
}

func (UnimplementedTaskClassForumServiceServer) CreateComment(context.Context, *CreateForumCommentRequest) (*CreateForumCommentResponse, error) {
	return nil, status.Errorf(codes.Unimplemented, "method CreateComment not implemented")
}

func (UnimplementedTaskClassForumServiceServer) DeleteComment(context.Context, *DeleteForumCommentRequest) (*DeleteForumCommentResponse, error) {
	return nil, status.Errorf(codes.Unimplemented, "method DeleteComment not implemented")
}

func (UnimplementedTaskClassForumServiceServer) ImportPost(context.Context, *ImportForumPostRequest) (*ImportForumPostResponse, error) {
	return nil, status.Errorf(codes.Unimplemented, "method ImportPost not implemented")
}

func RegisterTaskClassForumServiceServer(s grpc.ServiceRegistrar, srv TaskClassForumServiceServer) {
	s.RegisterService(&TaskClassForumService_ServiceDesc, srv)
}

func taskClassForumUnaryHandler[Req any](methodName string, fullMethod string, invoke func(TaskClassForumServiceServer, context.Context, *Req) (interface{}, error)) grpc.MethodDesc {
	return grpc.MethodDesc{
		MethodName: methodName,
		Handler: func(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpc.UnaryServerInterceptor) (interface{}, error) {
			in := new(Req)
			if err := dec(in); err != nil {
				return nil, err
			}
			if interceptor == nil {
				return invoke(srv.(TaskClassForumServiceServer), ctx, in)
			}
			info := &grpc.UnaryServerInfo{
				Server:     srv,
				FullMethod: fullMethod,
			}
			handler := func(ctx context.Context, req interface{}) (interface{}, error) {
				return invoke(srv.(TaskClassForumServiceServer), ctx, req.(*Req))
			}
			return interceptor(ctx, in, info, handler)
		},
	}
}

var TaskClassForumService_ServiceDesc = grpc.ServiceDesc{
	ServiceName: "smartflow.taskclassforum.TaskClassForumService",
	HandlerType: (*TaskClassForumServiceServer)(nil),
	Methods: []grpc.MethodDesc{
		taskClassForumUnaryHandler[ListForumPostsRequest]("ListPosts", TaskClassForumService_ListPosts_FullMethodName, func(s TaskClassForumServiceServer, ctx context.Context, req *ListForumPostsRequest) (interface{}, error) {
			return s.ListPosts(ctx, req)
		}),
		taskClassForumUnaryHandler[ListForumTagsRequest]("ListTags", TaskClassForumService_ListTags_FullMethodName, func(s TaskClassForumServiceServer, ctx context.Context, req *ListForumTagsRequest) (interface{}, error) {
			return s.ListTags(ctx, req)
		}),
		taskClassForumUnaryHandler[CreateForumPostRequest]("CreatePost", TaskClassForumService_CreatePost_FullMethodName, func(s TaskClassForumServiceServer, ctx context.Context, req *CreateForumPostRequest) (interface{}, error) {
			return s.CreatePost(ctx, req)
		}),
		taskClassForumUnaryHandler[GetForumPostRequest]("GetPost", TaskClassForumService_GetPost_FullMethodName, func(s TaskClassForumServiceServer, ctx context.Context, req *GetForumPostRequest) (interface{}, error) {
			return s.GetPost(ctx, req)
		}),
		taskClassForumUnaryHandler[LikeForumPostRequest]("LikePost", TaskClassForumService_LikePost_FullMethodName, func(s TaskClassForumServiceServer, ctx context.Context, req *LikeForumPostRequest) (interface{}, error) {
			return s.LikePost(ctx, req)
		}),
		taskClassForumUnaryHandler[UnlikeForumPostRequest]("UnlikePost", TaskClassForumService_UnlikePost_FullMethodName, func(s TaskClassForumServiceServer, ctx context.Context, req *UnlikeForumPostRequest) (interface{}, error) {
			return s.UnlikePost(ctx, req)
		}),
		taskClassForumUnaryHandler[ListForumCommentsRequest]("ListComments", TaskClassForumService_ListComments_FullMethodName, func(s TaskClassForumServiceServer, ctx context.Context, req *ListForumCommentsRequest) (interface{}, error) {
			return s.ListComments(ctx, req)
		}),
		taskClassForumUnaryHandler[CreateForumCommentRequest]("CreateComment", TaskClassForumService_CreateComment_FullMethodName, func(s TaskClassForumServiceServer, ctx context.Context, req *CreateForumCommentRequest) (interface{}, error) {
			return s.CreateComment(ctx, req)
		}),
		taskClassForumUnaryHandler[DeleteForumCommentRequest]("DeleteComment", TaskClassForumService_DeleteComment_FullMethodName, func(s TaskClassForumServiceServer, ctx context.Context, req *DeleteForumCommentRequest) (interface{}, error) {
			return s.DeleteComment(ctx, req)
		}),
		taskClassForumUnaryHandler[ImportForumPostRequest]("ImportPost", TaskClassForumService_ImportPost_FullMethodName, func(s TaskClassForumServiceServer, ctx context.Context, req *ImportForumPostRequest) (interface{}, error) {
			return s.ImportPost(ctx, req)
		}),
	},
	Streams:  []grpc.StreamDesc{},
	Metadata: "taskclassforum.proto",
}
