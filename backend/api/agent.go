package api

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"

	"github.com/LoveLosita/smartflow/backend/model"
	"github.com/LoveLosita/smartflow/backend/respond"
	"github.com/LoveLosita/smartflow/backend/service"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
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
