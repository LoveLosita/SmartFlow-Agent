package api

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
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
		conversationID = uuid.NewString()
	}
	c.Writer.Header().Set("X-Conversation-ID", conversationID)

	userID := c.GetInt("user_id")
	outChan, errChan := api.svc.AgentChat(c.Request.Context(), req.Message, req.Thinking, req.Model, userID, conversationID)

	// 4) 转发 SSE 流
	c.Stream(func(w io.Writer) bool {
		select {
		case err, ok := <-errChan:
			if ok && err != nil {
				errPayload, _ := json.Marshal(map[string]any{
					"error": map[string]any{
						"message": err.Error(),
						"type":    "server_error",
					},
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
