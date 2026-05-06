package llm

import (
	"strings"

	einoinfra "github.com/LoveLosita/smartflow/backend/shared/infra/eino"
)

// Service 只负责统一暴露已经构造好的模型客户端，不负责 prompt 和业务编排。
type Service struct {
	liteClient                 *Client
	proClient                  *Client
	maxClient                  *Client
	courseImageResponsesClient *ArkResponsesClient
}

// Options 描述 llm-service 初始化时需要接管的启动期依赖。
// 1. AIHub 仍然是当前进程内 Ark ChatModel 的来源，但服务层只保存统一 Client。
// 2. CourseImageResponsesClient 允许外部预先注入，便于测试或特殊启动路径复用。
// 3. 某个字段为空时不报错，直接保留 nil，交给上层继续走兼容降级。
type Options struct {
	AIHub                      *einoinfra.AIHub
	APIKey                     string
	BaseURL                    string
	CourseVisionModel          string
	CourseImageResponsesClient *ArkResponsesClient
}

// AgentModelClients 一次性暴露 agent 图常用的模型分配结果。
type AgentModelClients struct {
	Chat    *Client
	Plan    *Client
	Execute *Client
	Deliver *Client
	Summary *Client
}

// StaticClients 用于在不依赖 AIHub 的情况下直接注入已构造好的客户端。
//
// 职责边界：
// 1. 只负责把已经准备好的 client 聚合成 Service；
// 2. 不负责选择 provider，也不负责初始化远端 RPC 连接；
// 3. 供独立 llm zrpc client、测试替身和迁移期桥接入口复用。
type StaticClients struct {
	Lite                 *Client
	Pro                  *Client
	Max                  *Client
	CourseImageResponses *ArkResponsesClient
}

// New 构造 llm-service。
// 1. 不返回 error，是为了让上层继续按 nil 客户端做逐步降级。
// 2. 只要 AIHub 已初始化，就把其中的 ChatModel 收敛成统一 Client。
// 3. 课程图片解析客户端在这里统一构建，避免业务层直接依赖 Responses SDK。
func New(opts Options) *Service {
	svc := &Service{}

	if opts.AIHub != nil {
		svc.liteClient = WrapArkClient(opts.AIHub.Lite)
		svc.proClient = WrapArkClient(opts.AIHub.Pro)
		svc.maxClient = WrapArkClient(opts.AIHub.Max)
	}

	if opts.CourseImageResponsesClient != nil {
		svc.courseImageResponsesClient = opts.CourseImageResponsesClient
	} else {
		apiKey := strings.TrimSpace(opts.APIKey)
		baseURL := strings.TrimSpace(opts.BaseURL)
		model := strings.TrimSpace(opts.CourseVisionModel)
		if apiKey != "" && model != "" {
			svc.courseImageResponsesClient = NewArkResponsesClient(apiKey, baseURL, model)
		}
	}

	return svc
}

// NewWithClients 使用外部注入的现成客户端构造 Service。
func NewWithClients(clients StaticClients) *Service {
	return &Service{
		liteClient:                 clients.Lite,
		proClient:                  clients.Pro,
		maxClient:                  clients.Max,
		courseImageResponsesClient: clients.CourseImageResponses,
	}
}

// LiteClient 返回低成本短输出模型客户端。
func (s *Service) LiteClient() *Client {
	if s == nil {
		return nil
	}
	return s.liteClient
}

// ProClient 返回默认复杂对话模型客户端。
func (s *Service) ProClient() *Client {
	if s == nil {
		return nil
	}
	return s.proClient
}

// MaxClient 返回深度推理模型客户端。
func (s *Service) MaxClient() *Client {
	if s == nil {
		return nil
	}
	return s.maxClient
}

// CourseImageResponsesClient 返回课程图片解析所用的 Responses 客户端。
func (s *Service) CourseImageResponsesClient() *ArkResponsesClient {
	if s == nil {
		return nil
	}
	return s.courseImageResponsesClient
}

// NewAgentModelClients 一次性返回 agent 图里常用的模型分配。
func (s *Service) NewAgentModelClients() AgentModelClients {
	if s == nil {
		return AgentModelClients{}
	}
	return AgentModelClients{
		Chat:    s.ProClient(),
		Plan:    s.MaxClient(),
		Execute: s.MaxClient(),
		Deliver: s.ProClient(),
		Summary: s.LiteClient(),
	}
}
