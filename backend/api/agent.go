package api

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/LoveLosita/smartflow/backend/model"
	"github.com/LoveLosita/smartflow/backend/respond"
	"github.com/LoveLosita/smartflow/backend/service"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

type AgentHandler struct {
	svc *service.AgentService
}

// NewAgentHandler 组装 AgentHandler。
func NewAgentHandler(svc *service.AgentService) *AgentHandler {
	return &AgentHandler{
		svc: svc,
	}
}

func writeSSEData(w io.Writer, payload string) error {
	_, err := io.WriteString(w, "data: "+payload+"\n\n")
	return err
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

	// 3) 规范化会话 ID
	conversationID := strings.TrimSpace(req.ConversationID)
	if conversationID == "" {
		// confirm_action 需要关联已存在的会话状态，缺少 conversation_id 直接报错。
		if _, ok := req.Extra["confirm_action"]; ok {
			c.JSON(http.StatusBadRequest, respond.MissingConversationID)
			return
		}
		conversationID = uuid.NewString()
	}
	c.Writer.Header().Set("X-Conversation-ID", conversationID)

	userID := c.GetInt("user_id")
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
				// 4.1 统一 SSE 错误体：
				// 4.1.1 默认按内部错误输出 message/type；
				// 4.1.2 若是 respond.Response（含业务码），额外透传 code，便于前端识别 5xxxx 等自定义错误。
				errorBody := map[string]any{
					"message": err.Error(),
					"type":    "server_error",
				}
				var respErr respond.Response
				if errors.As(err, &respErr) {
					errorBody["code"] = respErr.Status
				}
				errPayload, _ := json.Marshal(map[string]any{
					"error": errorBody,
				})
				_ = writeSSEData(w, string(errPayload))
				_ = writeSSEData(w, "[DONE]")
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

// GetConversationMeta 返回单个会话的元信息（标题、消息数、最近消息时间等）。
// 设计说明：
// 1) 该接口用于配合 SSE 聊天链路：标题异步生成后，前端可通过 conversation_id 拉取；
// 2) 不依赖 SSE header 动态更新，避免“header 必须首包前写入”的协议限制；
// 3) 会话不存在时返回 400，避免前端把无效会话当成系统错误。
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

	// 4. 调 service 查询会话元信息。
	meta, err := api.svc.GetConversationMeta(ctx, userID, conversationID)
	if err != nil {
		// 会话不存在按参数错误处理，返回 400 给前端更直观。
		if errors.Is(err, gorm.ErrRecordNotFound) {
			c.JSON(http.StatusBadRequest, respond.WrongParamType)
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

	// 5. 调 service 查询并返回统一响应结构。
	resp, err := api.svc.GetConversationList(ctx, userID, page, pageSize, status)
	if err != nil {
		respond.DealWithError(c, err)
		return
	}
	c.JSON(http.StatusOK, respond.RespWithData(respond.Ok, resp))
}

// GetConversationHistory 返回指定会话的聊天历史记录。
//
// 设计说明：
// 1) 该接口只读历史，不负责改写 Redis/DB 中的会话状态；
// 2) 读取顺序复用现有服务层能力：先校验归属，再查 Redis，未命中再回源 DB；
// 3) 会话不存在时统一返回 400，避免前端把无效会话误判成系统故障。
func (api *AgentHandler) GetConversationHistory(c *gin.Context) {
	// 1. 参数校验：conversation_id 必填。
	conversationID := strings.TrimSpace(c.Query("conversation_id"))
	if conversationID == "" {
		c.JSON(http.StatusBadRequest, respond.MissingParam)
		return
	}

	// 2. 从鉴权上下文取当前用户 ID，确保查询范围只落在“本人会话”内。
	userID := c.GetInt("user_id")

	// 3. 设置短超时，避免缓存抖动或慢查询长期占用连接。
	ctx, cancel := context.WithTimeout(c.Request.Context(), 2*time.Second)
	defer cancel()

	// 4. 调 service 查询聊天历史。
	history, err := api.svc.GetConversationHistory(ctx, userID, conversationID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			c.JSON(http.StatusBadRequest, respond.WrongParamType)
			return
		}
		respond.DealWithError(c, err)
		return
	}

	// 5. 返回统一响应结构。
	c.JSON(http.StatusOK, respond.RespWithData(respond.Ok, history))
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

	// 4. 调 service 查询并返回统一响应结构。
	preview, err := api.svc.GetSchedulePlanPreview(ctx, userID, conversationID)
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

	statsJSON, err := api.svc.GetContextStats(ctx, userID, conversationID)
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
