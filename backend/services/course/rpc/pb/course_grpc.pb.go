package pb

import (
	context "context"

	grpc "google.golang.org/grpc"
	codes "google.golang.org/grpc/codes"
	status "google.golang.org/grpc/status"
)

const (
	Course_Ping_FullMethodName             = "/smartflow.course.Course/Ping"
	Course_ValidateCourse_FullMethodName   = "/smartflow.course.Course/ValidateCourse"
	Course_ImportCourses_FullMethodName    = "/smartflow.course.Course/ImportCourses"
	Course_ParseCourseImage_FullMethodName = "/smartflow.course.Course/ParseCourseImage"
)

type CourseClient interface {
	Ping(ctx context.Context, in *StatusResponse, opts ...grpc.CallOption) (*StatusResponse, error)
	ValidateCourse(ctx context.Context, in *JSONRequest, opts ...grpc.CallOption) (*JSONResponse, error)
	ImportCourses(ctx context.Context, in *JSONRequest, opts ...grpc.CallOption) (*JSONResponse, error)
	ParseCourseImage(ctx context.Context, in *CourseImageRequest, opts ...grpc.CallOption) (*JSONResponse, error)
}

type courseClient struct {
	cc grpc.ClientConnInterface
}

func NewCourseClient(cc grpc.ClientConnInterface) CourseClient {
	return &courseClient{cc}
}

func (c *courseClient) Ping(ctx context.Context, in *StatusResponse, opts ...grpc.CallOption) (*StatusResponse, error) {
	out := new(StatusResponse)
	err := c.cc.Invoke(ctx, Course_Ping_FullMethodName, in, out, opts...)
	return out, err
}

func (c *courseClient) ValidateCourse(ctx context.Context, in *JSONRequest, opts ...grpc.CallOption) (*JSONResponse, error) {
	out := new(JSONResponse)
	err := c.cc.Invoke(ctx, Course_ValidateCourse_FullMethodName, in, out, opts...)
	return out, err
}

func (c *courseClient) ImportCourses(ctx context.Context, in *JSONRequest, opts ...grpc.CallOption) (*JSONResponse, error) {
	out := new(JSONResponse)
	err := c.cc.Invoke(ctx, Course_ImportCourses_FullMethodName, in, out, opts...)
	return out, err
}

func (c *courseClient) ParseCourseImage(ctx context.Context, in *CourseImageRequest, opts ...grpc.CallOption) (*JSONResponse, error) {
	out := new(JSONResponse)
	err := c.cc.Invoke(ctx, Course_ParseCourseImage_FullMethodName, in, out, opts...)
	return out, err
}

type CourseServer interface {
	Ping(context.Context, *StatusResponse) (*StatusResponse, error)
	ValidateCourse(context.Context, *JSONRequest) (*JSONResponse, error)
	ImportCourses(context.Context, *JSONRequest) (*JSONResponse, error)
	ParseCourseImage(context.Context, *CourseImageRequest) (*JSONResponse, error)
}

type UnimplementedCourseServer struct{}

func (UnimplementedCourseServer) Ping(context.Context, *StatusResponse) (*StatusResponse, error) {
	return nil, status.Errorf(codes.Unimplemented, "method Ping not implemented")
}
func (UnimplementedCourseServer) ValidateCourse(context.Context, *JSONRequest) (*JSONResponse, error) {
	return nil, status.Errorf(codes.Unimplemented, "method ValidateCourse not implemented")
}
func (UnimplementedCourseServer) ImportCourses(context.Context, *JSONRequest) (*JSONResponse, error) {
	return nil, status.Errorf(codes.Unimplemented, "method ImportCourses not implemented")
}
func (UnimplementedCourseServer) ParseCourseImage(context.Context, *CourseImageRequest) (*JSONResponse, error) {
	return nil, status.Errorf(codes.Unimplemented, "method ParseCourseImage not implemented")
}

func RegisterCourseServer(s grpc.ServiceRegistrar, srv CourseServer) {
	s.RegisterService(&Course_ServiceDesc, srv)
}

func _Course_Ping_Handler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpc.UnaryServerInterceptor) (interface{}, error) {
	in := new(StatusResponse)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(CourseServer).Ping(ctx, in)
	}
	info := &grpc.UnaryServerInfo{Server: srv, FullMethod: Course_Ping_FullMethodName}
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return srv.(CourseServer).Ping(ctx, req.(*StatusResponse))
	}
	return interceptor(ctx, in, info, handler)
}

func _Course_JSON_Handler(fullMethod string, invoke func(CourseServer, context.Context, *JSONRequest) (*JSONResponse, error)) grpc.MethodHandler {
	return func(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpc.UnaryServerInterceptor) (interface{}, error) {
		in := new(JSONRequest)
		if err := dec(in); err != nil {
			return nil, err
		}
		if interceptor == nil {
			return invoke(srv.(CourseServer), ctx, in)
		}
		info := &grpc.UnaryServerInfo{Server: srv, FullMethod: fullMethod}
		handler := func(ctx context.Context, req interface{}) (interface{}, error) {
			return invoke(srv.(CourseServer), ctx, req.(*JSONRequest))
		}
		return interceptor(ctx, in, info, handler)
	}
}

func _Course_ParseCourseImage_Handler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpc.UnaryServerInterceptor) (interface{}, error) {
	in := new(CourseImageRequest)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(CourseServer).ParseCourseImage(ctx, in)
	}
	info := &grpc.UnaryServerInfo{Server: srv, FullMethod: Course_ParseCourseImage_FullMethodName}
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return srv.(CourseServer).ParseCourseImage(ctx, req.(*CourseImageRequest))
	}
	return interceptor(ctx, in, info, handler)
}

var Course_ServiceDesc = grpc.ServiceDesc{
	ServiceName: "smartflow.course.Course",
	HandlerType: (*CourseServer)(nil),
	Methods: []grpc.MethodDesc{
		{MethodName: "Ping", Handler: _Course_Ping_Handler},
		{MethodName: "ValidateCourse", Handler: _Course_JSON_Handler(Course_ValidateCourse_FullMethodName, CourseServer.ValidateCourse)},
		{MethodName: "ImportCourses", Handler: _Course_JSON_Handler(Course_ImportCourses_FullMethodName, CourseServer.ImportCourses)},
		{MethodName: "ParseCourseImage", Handler: _Course_ParseCourseImage_Handler},
	},
	Streams:  []grpc.StreamDesc{},
	Metadata: "course.proto",
}
