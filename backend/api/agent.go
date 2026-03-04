package api

import (
	"io"
	"net/http"

	"github.com/LoveLosita/smartflow/backend/model"
	"github.com/LoveLosita/smartflow/backend/respond"
	"github.com/LoveLosita/smartflow/backend/service"
	"github.com/gin-gonic/gin"
)

type AgentHandler struct {
	svc *service.AgentService
}

// NewAgentHandler 组装 Handler 的“工厂”
func NewAgentHandler(svc *service.AgentService) *AgentHandler {
	return &AgentHandler{
		svc: svc, // 把传进来的 Service 揣进口袋里
	}
}

func (api *AgentHandler) ChatAgent(c *gin.Context) {
	// 1. 设置请求头
	c.Writer.Header().Set("Content-Type", "text/event-stream")
	c.Writer.Header().Set("Cache-Control", "no-cache")
	c.Writer.Header().Set("Connection", "keep-alive")
	c.Writer.Header().Set("Transfer-Encoding", "chunked")
	// 2. 从请求中获取用户输入
	var req model.UserSendMessageRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, respond.WrongParamType)
		return
	}
	userID := c.GetInt("user_id") // 从上下文中获取用户 ID
	// 3. 调用 Service 层的聊天方法，获取输出通道和错误通道
	outChan, errChan := api.svc.AgentChat(c.Request.Context(), req.Message, userID, req.ConversationID)
	// 4. 循环转发消息/错误
	c.Stream(func(w io.Writer) bool {
		select {
		case err, ok := <-errChan:
			if ok && err != nil {
				respond.DealWithError(c, err)
			}
			return false
		case msg, ok := <-outChan:
			if !ok {
				return false
			}
			c.SSEvent("message", msg) // 发送 SSE 格式消息
			return true
		case <-c.Request.Context().Done():
			return false
		}
	})
}
