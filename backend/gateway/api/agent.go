package api

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	agentclient "github.com/LoveLosita/smartflow/backend/client/agent"
	"github.com/LoveLosita/smartflow/backend/gateway/shared/respond"
	agentsv "github.com/LoveLosita/smartflow/backend/services/agent/sv"
	"github.com/LoveLosita/smartflow/backend/services/runtime/model"
	agentcontracts "github.com/LoveLosita/smartflow/backend/shared/contracts/agent"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/spf13/viper"
	"gorm.io/gorm"
)

const (
	agentChatHeartbeatInterval = 5 * time.Second
	agentRPCChatEnabledKey     = "agent.rpc.chat.enabled"
	agentRPCAPIEnabledKey      = "agent.rpc.api.enabled"
)

type AgentHandler struct {
	svc         *agentsv.AgentService
	rpcClient   *agentclient.Client
	rpcClientMu sync.Mutex
}

// NewAgentHandler 组装 AgentHandler。
func NewAgentHandler(svc *agentsv.AgentService) *AgentHandler {
	return &AgentHandler{
		svc: svc,
	}
}

// NewAgentHandlerWithRPC 组装带 agent RPC stream 适配能力的 AgentHandler。
//
// 职责边界：
// 1. HTTP / SSE 协议仍由 Gateway 持有；
// 2. agent RPC 作为 chat stream 与非 chat /agent/* 查询/命令的服务间通道；
// 3. svc 只用于 RPC 开关关闭时的迁移期 fallback，当前默认可为 nil；
// 4. rpcClient 为空时允许按配置懒加载，避免测试和旧装配必须提前构造 client。
func NewAgentHandlerWithRPC(svc *agentsv.AgentService, rpcClient *agentclient.Client) *AgentHandler {
	return &AgentHandler{
		svc:       svc,
		rpcClient: rpcClient,
	}
}

func writeSSEData(w io.Writer, payload string) error {
	_, err := io.WriteString(w, "data: "+payload+"\n\n")
	return err
}

// mapResumeConfirmAction 把 extra.resume.action 映射为现有 confirm_action 口径。
//
// 映射规则：
// 1. approve -> accept（确认执行）；
// 2. reject/cancel -> reject（拒绝执行）；
// 3. 兜底走 reject，避免脏值误触发执行。
func mapResumeConfirmAction(action model.AgentResumeAction) string {
	switch action {
	case model.AgentResumeActionApprove:
		return "accept"
	case model.AgentResumeActionReject, model.AgentResumeActionCancel:
		return "reject"
	default:
		return "reject"
	}
}

type agentChatStreamEvent struct {
	payload   string
	done      bool
	errorJSON json.RawMessage
	err       error
}

func (api *AgentHandler) ChatAgent(c *gin.Context) {
	// 1) 设置 SSE 响应头
	c.Writer.Header().Set("Content-Type", "text/event-stream")
	c.Writer.Header().Set("Cache-Control", "no-cache")
	c.Writer.Header().Set("Connection", "keep-alive")
	c.Writer.Header().Set("Transfer-Encoding", "chunked")
	c.Writer.Header().Set("X-Accel-Buffering", "no")

	// 2) 解析请求体
	var req model.UserSendMessageRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, respond.WrongParamType)
		return
	}

	// 2.1 兼容新恢复协议：把 extra.resume 统一映射到现有内部字段。
	// 1. 前端新协议只传 resume，不再直接传 confirm_action；
	// 2. 后端这里做一次入口归一，保证下游状态机继续按既有字段消费；
	// 3. 解析失败直接返回 400，避免把非法恢复请求当普通消息继续执行。
	resumeReq, resumeErr := req.ResumeRequest()
	if resumeErr != nil {
		c.JSON(http.StatusBadRequest, respond.WrongParamType)
		return
	}
	if resumeReq != nil {
		if req.Extra == nil {
			req.Extra = make(map[string]any)
		}
		req.Extra["resume_interaction_id"] = resumeReq.InteractionID
		if resumeReq.IsConfirmResume() {
			req.Extra["confirm_action"] = mapResumeConfirmAction(resumeReq.Action)
		}
	}

	// 3) 规范化会话 ID
	conversationID := strings.TrimSpace(req.ConversationID)
	if conversationID == "" {
		// 恢复类请求必须关联既有会话状态，缺少 conversation_id 直接报错。
		if resumeReq != nil {
			c.JSON(http.StatusBadRequest, respond.MissingConversationID)
			return
		}
		// 兼容旧协议：confirm_action 也必须绑定已有会话。
		if _, ok := req.Extra["confirm_action"]; ok {
			c.JSON(http.StatusBadRequest, respond.MissingConversationID)
			return
		}
		conversationID = uuid.NewString()
	}
	c.Writer.Header().Set("X-Conversation-ID", conversationID)

	userID := c.GetInt("user_id")
	if api.useAgentRPCChat() {
		api.streamAgentChatByRPC(c, req, userID, conversationID)
		return
	}
	if api.svc == nil {
		writeAgentSSEError(c.Writer, errors.New("agent local fallback is disabled"))
		flushSSEWriter(c.Writer)
		return
	}

	outChan, errChan := api.svc.AgentChat(c.Request.Context(), req.Message, req.Thinking, req.Model, userID, conversationID, req.Extra)

	// 4) 转发 SSE 流
	// 4.0 心跳保活：LLM thinking 静默期可达 10+ 秒，Vite dev proxy 会判 idle 切断连接。
	// 每 5 秒发送 SSE 标准注释行 ": ping\n\n"，前端 JSON.parse 失败后丢弃，不污染 UI。
	heartbeat := time.NewTicker(5 * time.Second)
	defer heartbeat.Stop()

	c.Stream(func(w io.Writer) bool {
		select {
		case err, ok := <-errChan:
			if ok && err != nil {
				writeAgentSSEError(w, err)
			}
			return false
		case msg, ok := <-outChan:
			if !ok {
				return false
			}
			if err := writeSSEData(w, msg); err != nil {
				return false
			}
			return true
		case <-c.Request.Context().Done():
			return false
		// 心跳分支：LLM thinking 静默期每 5 秒推送 SSE 注释行，防止代理判 idle 断连。
		case <-heartbeat.C:
			io.WriteString(w, ": ping\n\n")
			c.Writer.(http.Flusher).Flush()
			return true
		}
	})
}

func (api *AgentHandler) useAgentRPCChat() bool {
	return api != nil && viper.GetBool(agentRPCChatEnabledKey)
}

func (api *AgentHandler) useAgentRPCAPI() bool {
	return api != nil && viper.GetBool(agentRPCAPIEnabledKey)
}

// streamAgentChatByRPC 把 agent RPC server-stream 平滑转成前端既有 SSE。
//
// 职责边界：
// 1. Gateway 继续负责 SSE header、心跳和 data 帧写出；
// 2. agent RPC 只负责服务间 chunk stream，不暴露 Go channel 给跨进程调用方；
// 3. RPC 建流失败或服务端 error_json 仍按现有 SSE 错误体输出，再追加 [DONE]。
func (api *AgentHandler) streamAgentChatByRPC(c *gin.Context, req model.UserSendMessageRequest, userID int, conversationID string) {
	client, err := api.getAgentRPCClient()
	if err != nil {
		writeAgentSSEError(c.Writer, err)
		flushSSEWriter(c.Writer)
		return
	}

	extraJSON, err := json.Marshal(req.Extra)
	if err != nil {
		writeAgentSSEError(c.Writer, err)
		flushSSEWriter(c.Writer)
		return
	}
	stream, err := client.Chat(c.Request.Context(), agentcontracts.ChatRequest{
		Message:        req.Message,
		Thinking:       req.Thinking,
		Model:          req.Model,
		UserID:         userID,
		ConversationID: conversationID,
		ExtraJSON:      extraJSON,
	})
	if err != nil {
		writeAgentSSEError(c.Writer, err)
		flushSSEWriter(c.Writer)
		return
	}

	recvCh := make(chan agentChatStreamEvent, 1)
	requestCtx := c.Request.Context()
	go func() {
		defer close(recvCh)
		sendEvent := func(event agentChatStreamEvent) bool {
			select {
			case recvCh <- event:
				return true
			case <-requestCtx.Done():
				return false
			}
		}
		for {
			chunk, recvErr := stream.Recv()
			if recvErr != nil {
				if errors.Is(recvErr, io.EOF) {
					return
				}
				sendEvent(agentChatStreamEvent{err: recvErr})
				return
			}
			if !sendEvent(agentChatStreamEvent{
				payload:   chunk.Payload,
				done:      chunk.Done,
				errorJSON: append(json.RawMessage(nil), chunk.ErrorJSON...),
			}) {
				return
			}
			if chunk.Done || len(chunk.ErrorJSON) > 0 {
				return
			}
		}
	}()

	heartbeat := time.NewTicker(agentChatHeartbeatInterval)
	defer heartbeat.Stop()

	c.Stream(func(w io.Writer) bool {
		select {
		case event, ok := <-recvCh:
			if !ok {
				return false
			}
			if event.err != nil {
				writeAgentSSEError(w, event.err)
				return false
			}
			if event.payload != "" {
				if err := writeSSEData(w, event.payload); err != nil {
					return false
				}
			}
			if len(event.errorJSON) > 0 {
				_ = writeSSEData(w, string(normalizeAgentRPCErrorJSON(event.errorJSON)))
				_ = writeSSEData(w, "[DONE]")
				return false
			}
			if event.done {
				_ = writeSSEData(w, "[DONE]")
				return false
			}
			return true
		case <-c.Request.Context().Done():
			return false
		case <-heartbeat.C:
			_, _ = io.WriteString(w, ": ping\n\n")
			flushSSEWriter(c.Writer)
			return true
		}
	})
}

func writeAgentSSEError(w io.Writer, err error) {
	if err == nil {
		return
	}
	_ = writeSSEData(w, string(buildAgentErrorEnvelopeJSON(errorCodeFromError(err), err.Error(), "server_error")))
	_ = writeSSEData(w, "[DONE]")
}

func (api *AgentHandler) getAgentRPCClient() (*agentclient.Client, error) {
	if api == nil {
		return nil, errors.New("agent handler is not initialized")
	}

	api.rpcClientMu.Lock()
	defer api.rpcClientMu.Unlock()

	if api.rpcClient != nil {
		return api.rpcClient, nil
	}

	client, err := agentclient.NewClient(agentclient.ClientConfig{
		Endpoints: viper.GetStringSlice("agent.rpc.endpoints"),
		Target:    viper.GetString("agent.rpc.target"),
		Timeout:   viper.GetDuration("agent.rpc.timeout"),
	})
	if err != nil {
		return nil, err
	}

	api.rpcClient = client
	return api.rpcClient, nil
}

func normalizeAgentRPCErrorJSON(raw json.RawMessage) json.RawMessage {
	trimmed := strings.TrimSpace(string(raw))
	if trimmed == "" {
		return buildAgentErrorEnvelopeJSON("", "agent rpc service returned empty error payload", "server_error")
	}

	var payload map[string]any
	if err := json.Unmarshal([]byte(trimmed), &payload); err != nil {
		return buildAgentErrorEnvelopeJSON("", trimmed, "server_error")
	}

	if nested, ok := payload["error"].(map[string]any); ok {
		return buildAgentErrorEnvelopeJSON(
			firstNonEmptyString(stringFromAny(nested["code"]), stringFromAny(nested["status"])),
			firstNonEmptyString(stringFromAny(nested["message"]), stringFromAny(nested["info"]), "agent rpc service returned error"),
			firstNonEmptyString(stringFromAny(nested["type"]), "server_error"),
		)
	}

	return buildAgentErrorEnvelopeJSON(
		firstNonEmptyString(stringFromAny(payload["code"]), stringFromAny(payload["status"])),
		firstNonEmptyString(stringFromAny(payload["message"]), stringFromAny(payload["info"]), trimmed),
		firstNonEmptyString(stringFromAny(payload["type"]), "server_error"),
	)
}

func buildAgentErrorEnvelopeJSON(code string, message string, errorType string) json.RawMessage {
	errorBody := map[string]any{
		"message": strings.TrimSpace(message),
		"type":    strings.TrimSpace(errorType),
	}
	if errorBody["message"] == "" {
		errorBody["message"] = "agent stream error"
	}
	if errorBody["type"] == "" {
		errorBody["type"] = "server_error"
	}
	if trimmedCode := strings.TrimSpace(code); trimmedCode != "" {
		errorBody["code"] = trimmedCode
	}

	payload, err := json.Marshal(map[string]any{"error": errorBody})
	if err != nil {
		return json.RawMessage(`{"error":{"message":"agent stream error","type":"server_error"}}`)
	}
	return payload
}

func errorCodeFromError(err error) string {
	var respErr respond.Response
	if errors.As(err, &respErr) {
		return strings.TrimSpace(respErr.Status)
	}
	return ""
}

func stringFromAny(value any) string {
	switch typed := value.(type) {
	case string:
		return strings.TrimSpace(typed)
	case json.Number:
		return strings.TrimSpace(typed.String())
	case float64:
		return strings.TrimSpace(strconv.FormatFloat(typed, 'f', -1, 64))
	case float32:
		return strings.TrimSpace(strconv.FormatFloat(float64(typed), 'f', -1, 32))
	case int:
		return strconv.Itoa(typed)
	case int32:
		return strconv.FormatInt(int64(typed), 10)
	case int64:
		return strconv.FormatInt(typed, 10)
	case uint:
		return strconv.FormatUint(uint64(typed), 10)
	case uint32:
		return strconv.FormatUint(uint64(typed), 10)
	case uint64:
		return strconv.FormatUint(typed, 10)
	default:
		return ""
	}
}

func firstNonEmptyString(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

func flushSSEWriter(w io.Writer) {
	if flusher, ok := w.(http.Flusher); ok {
		flusher.Flush()
	}
}

func writeAgentHTTPError(c *gin.Context, err error) {
	if err == nil {
		return
	}
	var respErr respond.Response
	if errors.As(err, &respErr) && respErr.Status == respond.ConversationNotFound.Status {
		c.JSON(http.StatusNotFound, respErr)
		return
	}
	respond.DealWithError(c, err)
}

// GetConversationMeta 返回单个会话的元信息（标题、消息数、最近消息时间等）。
// 设计说明：
// 1) 该接口用于配合 SSE 聊天链路：标题异步生成后，前端可通过 conversation_id 拉取；
// 2) 不依赖 SSE header 动态更新，避免“header 必须首包前写入”的协议限制；
// 3) 会话不存在或不属于当前用户时返回 404，避免前端把无效会话误判成参数类型错误。
func (api *AgentHandler) GetConversationMeta(c *gin.Context) {
	// 1. 读取 query 参数并做基础校验。
	conversationID := strings.TrimSpace(c.Query("conversation_id"))
	if conversationID == "" {
		c.JSON(http.StatusBadRequest, respond.MissingParam)
		return
	}

	// 2. 统一透传 user_id，避免越权读取他人会话。
	userID := c.GetInt("user_id")

	// 3. 设置短超时，避免该查询接口被慢查询长时间占用。
	ctx, cancel := context.WithTimeout(c.Request.Context(), 1*time.Second)
	defer cancel()

	if api.useAgentRPCAPI() {
		client, err := api.getAgentRPCClient()
		if err != nil {
			writeAgentHTTPError(c, err)
			return
		}
		meta, err := client.GetConversationMeta(ctx, agentcontracts.ConversationQueryRequest{
			UserID:         userID,
			ConversationID: conversationID,
		})
		if err != nil {
			writeAgentHTTPError(c, err)
			return
		}
		c.JSON(http.StatusOK, respond.RespWithData(respond.Ok, meta))
		return
	}

	localSvc, ok := api.localAgentService(c)
	if !ok {
		return
	}

	// 4. 调 service 查询会话元信息。
	meta, err := localSvc.GetConversationMeta(ctx, userID, conversationID)
	if err != nil {
		// 会话不存在或越权访问时返回 404，让前端能和“参数格式错误”区分开。
		if errors.Is(err, gorm.ErrRecordNotFound) {
			c.JSON(http.StatusNotFound, respond.ConversationNotFound)
			return
		}
		respond.DealWithError(c, err)
		return
	}

	// 5. 返回统一响应结构。
	c.JSON(http.StatusOK, respond.RespWithData(respond.Ok, meta))
}

// GetConversationList 返回当前登录用户的会话列表（分页）。
//
// 设计说明：
// 1) 接口只返回“列表元信息”，不返回消息正文，避免列表接口过重；
// 2) page/page_size 为可选参数，缺省值由 service 层统一兜底；
// 3) status 可选，支持 active/archived，非法值直接返回 400。
func (api *AgentHandler) GetConversationList(c *gin.Context) {
	// 1. 从 JWT 上下文读取 user_id，保证只查“当前用户自己的会话”。
	userID := c.GetInt("user_id")

	// 2. 解析分页参数（可选）：
	// 2.1 参数不存在时保持 0，让 service 使用默认值；
	// 2.2 参数存在但格式非法时直接返回 400，避免脏参数下沉。
	page := 0
	if rawPage := strings.TrimSpace(c.Query("page")); rawPage != "" {
		parsedPage, err := strconv.Atoi(rawPage)
		if err != nil {
			c.JSON(http.StatusBadRequest, respond.WrongParamType)
			return
		}
		page = parsedPage
	}

	pageSize := 0
	if rawPageSize := strings.TrimSpace(c.Query("page_size")); rawPageSize != "" {
		parsedPageSize, err := strconv.Atoi(rawPageSize)
		if err != nil {
			c.JSON(http.StatusBadRequest, respond.WrongParamType)
			return
		}
		pageSize = parsedPageSize
	}

	// 2.3 limit 是 page_size 的懒加载别名：
	// 2.3.1 前端若显式传 limit，则以 limit 为准，避免前端再做字段转换；
	// 2.3.2 若 limit 非法同样直接返回 400，避免把脏参数下沉到 service；
	// 2.3.3 若未传 limit，则继续沿用历史 page_size 行为，保持老前端兼容。
	if rawLimit := strings.TrimSpace(c.Query("limit")); rawLimit != "" {
		parsedLimit, err := strconv.Atoi(rawLimit)
		if err != nil {
			c.JSON(http.StatusBadRequest, respond.WrongParamType)
			return
		}
		pageSize = parsedLimit
	}

	// 3. status 过滤器可选，最终合法性由 service 层统一校验。
	status := strings.TrimSpace(c.Query("status"))

	// 4. 读接口设置短超时，避免慢查询占用连接。
	ctx, cancel := context.WithTimeout(c.Request.Context(), 1*time.Second)
	defer cancel()

	if api.useAgentRPCAPI() {
		client, err := api.getAgentRPCClient()
		if err != nil {
			writeAgentHTTPError(c, err)
			return
		}
		resp, err := client.GetConversationList(ctx, agentcontracts.ConversationListRequest{
			UserID:   userID,
			Page:     page,
			PageSize: pageSize,
			Status:   status,
		})
		if err != nil {
			writeAgentHTTPError(c, err)
			return
		}
		c.JSON(http.StatusOK, respond.RespWithData(respond.Ok, resp))
		return
	}

	localSvc, ok := api.localAgentService(c)
	if !ok {
		return
	}

	// 5. 调 service 查询并返回统一响应结构。
	resp, err := localSvc.GetConversationList(ctx, userID, page, pageSize, status)
	if err != nil {
		respond.DealWithError(c, err)
		return
	}
	c.JSON(http.StatusOK, respond.RespWithData(respond.Ok, resp))
}

// GetConversationTimeline 返回指定会话的统一时间线（正文+卡片）。
//
// 说明：
// 1. 该接口是新前端刷新重建的单一来源；
// 2. 返回结果已按 seq 升序，前端按数组顺序渲染即可；
// 3. 会话不存在或不属于当前用户时统一返回 404，避免误判成参数格式问题。
func (api *AgentHandler) GetConversationTimeline(c *gin.Context) {
	conversationID := strings.TrimSpace(c.Query("conversation_id"))
	if conversationID == "" {
		c.JSON(http.StatusBadRequest, respond.MissingParam)
		return
	}

	userID := c.GetInt("user_id")

	ctx, cancel := context.WithTimeout(c.Request.Context(), 2*time.Second)
	defer cancel()

	if api.useAgentRPCAPI() {
		client, err := api.getAgentRPCClient()
		if err != nil {
			writeAgentHTTPError(c, err)
			return
		}
		timeline, err := client.GetConversationTimeline(ctx, agentcontracts.ConversationQueryRequest{
			UserID:         userID,
			ConversationID: conversationID,
		})
		if err != nil {
			writeAgentHTTPError(c, err)
			return
		}
		c.JSON(http.StatusOK, respond.RespWithData(respond.Ok, timeline))
		return
	}

	localSvc, ok := api.localAgentService(c)
	if !ok {
		return
	}

	timeline, err := localSvc.GetConversationTimeline(ctx, userID, conversationID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			c.JSON(http.StatusNotFound, respond.ConversationNotFound)
			return
		}
		respond.DealWithError(c, err)
		return
	}

	c.JSON(http.StatusOK, respond.RespWithData(respond.Ok, timeline))
}

// GetSchedulePlanPreview 返回“指定会话”的排程结构化预览。
//
// 设计说明：
// 1) 该接口只读 Redis 预览快照，不修改聊天主链路协议；
// 2) 按 conversation_id + user_id 读取，避免跨用户越权访问；
// 3) 预览受 TTL 影响，若不存在会返回业务错误码。
func (api *AgentHandler) GetSchedulePlanPreview(c *gin.Context) {
	// 1. 参数校验：conversation_id 必填。
	conversationID := strings.TrimSpace(c.Query("conversation_id"))
	if conversationID == "" {
		c.JSON(http.StatusBadRequest, respond.MissingParam)
		return
	}

	// 2. 从鉴权上下文取当前用户 ID，保证查询范围只在“本人会话”内。
	userID := c.GetInt("user_id")

	// 3. 设置短超时，防止缓存抖动时占用连接过久。
	ctx, cancel := context.WithTimeout(c.Request.Context(), 1*time.Second)
	defer cancel()

	if api.useAgentRPCAPI() {
		client, err := api.getAgentRPCClient()
		if err != nil {
			writeAgentHTTPError(c, err)
			return
		}
		preview, err := client.GetSchedulePlanPreview(ctx, agentcontracts.ConversationQueryRequest{
			UserID:         userID,
			ConversationID: conversationID,
		})
		if err != nil {
			writeAgentHTTPError(c, err)
			return
		}
		c.JSON(http.StatusOK, respond.RespWithData(respond.Ok, preview))
		return
	}

	localSvc, ok := api.localAgentService(c)
	if !ok {
		return
	}

	// 4. 调 service 查询并返回统一响应结构。
	preview, err := localSvc.GetSchedulePlanPreview(ctx, userID, conversationID)
	if err != nil {
		respond.DealWithError(c, err)
		return
	}
	c.JSON(http.StatusOK, respond.RespWithData(respond.Ok, preview))
}

// GetContextStats 获取指定会话的上下文窗口 token 分布统计。
func (api *AgentHandler) GetContextStats(c *gin.Context) {
	conversationID := strings.TrimSpace(c.Query("conversation_id"))
	if conversationID == "" {
		c.JSON(http.StatusBadRequest, respond.MissingParam)
		return
	}

	userID := c.GetInt("user_id")

	ctx, cancel := context.WithTimeout(c.Request.Context(), 1*time.Second)
	defer cancel()

	if api.useAgentRPCAPI() {
		client, err := api.getAgentRPCClient()
		if err != nil {
			writeAgentHTTPError(c, err)
			return
		}
		statsJSON, err := client.GetContextStats(ctx, agentcontracts.ConversationQueryRequest{
			UserID:         userID,
			ConversationID: conversationID,
		})
		if err != nil {
			writeAgentHTTPError(c, err)
			return
		}
		if strings.TrimSpace(statsJSON) == "" {
			statsJSON = "null"
		}
		var raw json.RawMessage = json.RawMessage(statsJSON)
		c.JSON(http.StatusOK, respond.RespWithData(respond.Ok, raw))
		return
	}

	localSvc, ok := api.localAgentService(c)
	if !ok {
		return
	}

	statsJSON, err := localSvc.GetContextStats(ctx, userID, conversationID)
	if err != nil {
		respond.DealWithError(c, err)
		return
	}

	// 直接透传 JSON 字符串，避免二次序列化。
	// 当会话尚未产生 compaction 统计时，LoadContextTokenStats 返回空字符串，
	// 此时 json.RawMessage("") 在 MarshalJSON 时会报 "unexpected end of JSON input"，
	// 所以空值时需要替换为 "null"，保证序列化安全。
	if strings.TrimSpace(statsJSON) == "" {
		statsJSON = "null"
	}
	var raw json.RawMessage = json.RawMessage(statsJSON)
	c.JSON(http.StatusOK, respond.RespWithData(respond.Ok, raw))
}

// SaveScheduleState 前端暂存日程调整到 Redis 快照。
//
// 设计说明：
// 1. 前端在 confirm 卡片上拖拽调整任务位置后，调用此接口以绝对时间格式提交放置项；
// 2. 后端将绝对坐标转换为 ScheduleState 内部的相对 day_index，只修改 task_item，不动课程；
// 3. 不触发 LLM 调用、不写 MySQL、不刷新预览缓存。
//
// 降级策略：
// 1. 快照不存在（TTL 过期或会话未进入排程）返回 400，让前端提示用户重新对话；
// 2. 坐标越界、task_item_id 不存在等校验错误统一返回 400。
func (api *AgentHandler) SaveScheduleState(c *gin.Context) {
	// 1. 解析请求体。
	var req model.SaveScheduleStateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, respond.WrongParamType)
		return
	}

	// 2. 校验 conversation_id。
	conversationID := strings.TrimSpace(req.ConversationID)
	if conversationID == "" {
		c.JSON(http.StatusBadRequest, respond.MissingParam)
		return
	}

	// 3. 从鉴权上下文取当前用户 ID。
	userID := c.GetInt("user_id")

	// 4. 设置短超时，防止快照读写阻塞过久。
	ctx, cancel := context.WithTimeout(c.Request.Context(), 3*time.Second)
	defer cancel()

	if api.useAgentRPCAPI() {
		client, err := api.getAgentRPCClient()
		if err != nil {
			writeAgentHTTPError(c, err)
			return
		}
		if err := client.SaveScheduleState(ctx, agentcontracts.SaveScheduleStateRequest{
			UserID:         userID,
			ConversationID: conversationID,
			Items:          toAgentContractScheduleStateItems(req.Items),
		}); err != nil {
			writeAgentHTTPError(c, err)
			return
		}
		c.JSON(http.StatusOK, respond.RespWithData(respond.Ok, nil))
		return
	}

	localSvc, ok := api.localAgentService(c)
	if !ok {
		return
	}

	// 5. 调用 service 层执行 Load → 应用放置项 → Save。
	if err := localSvc.SaveScheduleState(ctx, userID, conversationID, req.Items); err != nil {
		respond.DealWithError(c, err)
		return
	}
	c.JSON(http.StatusOK, respond.RespWithData(respond.Ok, nil))
}

// localAgentService 返回迁移期本地 fallback 服务。
//
// 职责边界：
// 1. 只服务于 RPC 开关关闭时的回退路径；
// 2. 默认 RPC 切流态允许 svc 为 nil，因此所有本地调用前必须经过此处；
// 3. 缺失时返回 500，提示启动配置和运行时装配不一致，而不是让 handler panic。
func (api *AgentHandler) localAgentService(c *gin.Context) (*agentsv.AgentService, bool) {
	if api != nil && api.svc != nil {
		return api.svc, true
	}
	respond.DealWithError(c, errors.New("agent local fallback is disabled"))
	return nil, false
}

func toAgentContractScheduleStateItems(items []model.SaveScheduleStatePlacedItem) []agentcontracts.SaveScheduleStatePlacedItem {
	if len(items) == 0 {
		return nil
	}
	result := make([]agentcontracts.SaveScheduleStatePlacedItem, 0, len(items))
	for _, item := range items {
		result = append(result, agentcontracts.SaveScheduleStatePlacedItem{
			TaskItemID:         item.TaskItemID,
			Week:               item.Week,
			DayOfWeek:          item.DayOfWeek,
			StartSection:       item.StartSection,
			EndSection:         item.EndSection,
			EmbedCourseEventID: item.EmbedCourseEventID,
		})
	}
	return result
}
