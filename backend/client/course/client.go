package course

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	coursepb "github.com/LoveLosita/smartflow/backend/services/course/rpc/pb"
	coursecontracts "github.com/LoveLosita/smartflow/backend/shared/contracts/course"
	"github.com/zeromicro/go-zero/zrpc"
	"google.golang.org/grpc"
)

const (
	defaultEndpoint = "127.0.0.1:9087"
	// 课表导入可能一次展开大量周次与节次，RPC 默认超时与网关保持一致，避免内层先被截断。
	defaultTimeout           = 5 * time.Minute
	defaultMaxRPCMessageSize = 8 * 1024 * 1024
	rpcMessageSizePadding    = 1024 * 1024
)

type ClientConfig struct {
	Endpoints     []string
	Target        string
	Timeout       time.Duration
	MaxImageBytes int64
}

// Client 是 gateway 访问 course zrpc 的最小适配层。
//
// 职责边界：
// 1. 只负责跨进程 gRPC 调用和 JSON/bytes 透传，不触碰 DAO；
// 2. HTTP 入参仍由 gateway/api 做基础绑定，业务校验交给 course 服务；
// 3. import 冲突通过 CourseImportConflictError 返回，让 HTTP 层保留 409 + conflicts 数据。
type Client struct {
	rpc coursepb.CourseClient
}

func NewClient(cfg ClientConfig) (*Client, error) {
	timeout := cfg.Timeout
	if timeout <= 0 {
		timeout = defaultTimeout
	}
	endpoints := normalizeEndpoints(cfg.Endpoints)
	target := strings.TrimSpace(cfg.Target)
	if len(endpoints) == 0 && target == "" {
		endpoints = []string{defaultEndpoint}
	}

	maxMessageSize := normalizeMaxRPCMessageSize(cfg.MaxImageBytes)
	zclient, err := zrpc.NewClient(zrpc.RpcClientConf{
		Endpoints: endpoints,
		Target:    target,
		NonBlock:  true,
		Timeout:   int64(timeout / time.Millisecond),
	}, zrpc.WithDialOption(grpc.WithDefaultCallOptions(
		grpc.MaxCallRecvMsgSize(maxMessageSize),
		grpc.MaxCallSendMsgSize(maxMessageSize),
	)))
	if err != nil {
		return nil, err
	}
	client := &Client{rpc: coursepb.NewCourseClient(zclient.Conn())}
	if err := client.ping(timeout); err != nil {
		return nil, err
	}
	return client, nil
}

func (c *Client) ValidateCourse(ctx context.Context, req coursecontracts.UserCheckCourseRequest) error {
	_, err := c.callJSON(ctx, c.rpc.ValidateCourse, req)
	return responseFromRPCError(err)
}

func (c *Client) ImportCourses(ctx context.Context, req coursecontracts.UserImportCoursesRequest) (json.RawMessage, error) {
	resp, err := c.callJSON(ctx, c.rpc.ImportCourses, req)
	raw, err := jsonFromResponse(resp, err)
	if err != nil {
		return nil, err
	}
	var result coursecontracts.ImportCoursesResult
	if err := json.Unmarshal(raw, &result); err != nil {
		return nil, err
	}
	if result.Conflict {
		return nil, CourseImportConflictError{conflicts: result.Conflicts}
	}
	return raw, nil
}

func (c *Client) ParseCourseTableImage(ctx context.Context, req coursecontracts.CourseImageParseRequest) (json.RawMessage, error) {
	resp, err := c.rpc.ParseCourseImage(ctx, &coursepb.CourseImageRequest{
		UserId:     uint64(req.UserID),
		Filename:   req.Filename,
		MimeType:   req.MIMEType,
		ImageBytes: req.ImageBytes,
	})
	return jsonFromResponse(resp, err)
}

func (c *Client) ensureReady() error {
	if c == nil || c.rpc == nil {
		return errors.New("course zrpc client is not initialized")
	}
	return nil
}

func (c *Client) ping(timeout time.Duration) error {
	if err := c.ensureReady(); err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	_, err := c.rpc.Ping(ctx, &coursepb.StatusResponse{})
	return responseFromRPCError(err)
}

func (c *Client) callJSON(ctx context.Context, fn func(context.Context, *coursepb.JSONRequest, ...grpc.CallOption) (*coursepb.JSONResponse, error), payload any) (*coursepb.JSONResponse, error) {
	if err := c.ensureReady(); err != nil {
		return nil, err
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	return fn(ctx, &coursepb.JSONRequest{PayloadJson: raw})
}

func jsonFromResponse(resp *coursepb.JSONResponse, rpcErr error) (json.RawMessage, error) {
	if rpcErr != nil {
		return nil, responseFromRPCError(rpcErr)
	}
	if resp == nil {
		return nil, errors.New("course zrpc service returned empty JSON response")
	}
	if len(resp.DataJson) == 0 {
		return json.RawMessage("null"), nil
	}
	return json.RawMessage(resp.DataJson), nil
}

func normalizeEndpoints(values []string) []string {
	endpoints := make([]string, 0, len(values))
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed != "" {
			endpoints = append(endpoints, trimmed)
		}
	}
	return endpoints
}

func normalizeMaxRPCMessageSize(maxImageBytes int64) int {
	if maxImageBytes <= 0 {
		return defaultMaxRPCMessageSize
	}
	size := maxImageBytes + rpcMessageSizePadding
	if size < defaultMaxRPCMessageSize {
		return defaultMaxRPCMessageSize
	}
	return int(size)
}
