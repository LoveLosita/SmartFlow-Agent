package agentmodel

// AgentRequest 是 agent2 总入口接收的统一请求结构。
type AgentRequest struct {
	UserID         int
	ConversationID string
	UserMessage    string
	ModelName      string
	Extra          map[string]any
}

// AgentResponse 是 agent2 总入口返回的统一响应结构。
type AgentResponse struct {
	Action AgentAction
	Reply  string
	Meta   map[string]any
}
