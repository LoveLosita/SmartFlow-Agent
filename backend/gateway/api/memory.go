package api

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/LoveLosita/smartflow/backend/gateway/shared/respond"
	memorycontracts "github.com/LoveLosita/smartflow/backend/shared/contracts/memory"
	"github.com/LoveLosita/smartflow/backend/shared/ports"
	"github.com/gin-gonic/gin"
)

type MemoryHandler struct {
	client ports.MemoryCommandClient
}

var errMemoryHandlerNotReady = errors.New("memory handler is not initialized")

// NewMemoryHandler 创建 memory HTTP 门面。
//
// 职责边界：
// 1. gateway 只负责鉴权后的参数绑定、超时和响应透传；
// 2. 记忆管理业务、审计、向量同步和状态校验都交给 memory zrpc 服务；
// 3. agent 的 memory reader 已在 CP3 切到 memory zrpc，HTTP 管理面这里只保留管理职责。
func NewMemoryHandler(client ports.MemoryCommandClient) *MemoryHandler {
	return &MemoryHandler{client: client}
}

func (h *MemoryHandler) ListItems(c *gin.Context) {
	if h == nil || h.client == nil {
		c.JSON(http.StatusInternalServerError, respond.InternalError(errMemoryHandlerNotReady))
		return
	}

	limit, ok := parseOptionalInt(c.Query("limit"))
	if !ok {
		c.JSON(http.StatusBadRequest, respond.WrongParamType)
		return
	}
	statusesRaw := c.Query("statuses")
	if strings.TrimSpace(statusesRaw) == "" {
		statusesRaw = c.Query("status")
	}
	memoryTypesRaw := c.Query("memory_types")
	if strings.TrimSpace(memoryTypesRaw) == "" {
		memoryTypesRaw = c.Query("memory_type")
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), 2*time.Second)
	defer cancel()

	resp, err := h.client.ListItems(ctx, memorycontracts.ListItemsRequest{
		UserID:         c.GetInt("user_id"),
		ConversationID: strings.TrimSpace(c.Query("conversation_id")),
		Statuses:       splitCSV(statusesRaw),
		MemoryTypes:    splitCSV(memoryTypesRaw),
		Limit:          limit,
	})
	if err != nil {
		respond.DealWithError(c, err)
		return
	}

	c.JSON(http.StatusOK, respond.RespWithData(respond.Ok, resp))
}

func (h *MemoryHandler) GetItem(c *gin.Context) {
	if h == nil || h.client == nil {
		c.JSON(http.StatusInternalServerError, respond.InternalError(errMemoryHandlerNotReady))
		return
	}

	memoryID, ok := parseMemoryIDParam(c)
	if !ok {
		c.JSON(http.StatusBadRequest, respond.WrongParamType)
		return
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), 2*time.Second)
	defer cancel()

	resp, err := h.client.GetItem(ctx, memorycontracts.GetItemRequest{
		UserID:   c.GetInt("user_id"),
		MemoryID: memoryID,
	})
	if err != nil {
		respond.DealWithError(c, err)
		return
	}
	c.JSON(http.StatusOK, respond.RespWithData(respond.Ok, resp))
}

func (h *MemoryHandler) CreateItem(c *gin.Context) {
	if h == nil || h.client == nil {
		c.JSON(http.StatusInternalServerError, respond.InternalError(errMemoryHandlerNotReady))
		return
	}

	var req memorycontracts.CreateItemRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, respond.WrongParamType)
		return
	}
	req.UserID = c.GetInt("user_id")
	req.OperatorType = "user"

	ctx, cancel := context.WithTimeout(c.Request.Context(), 2*time.Second)
	defer cancel()

	resp, err := h.client.CreateItem(ctx, req)
	if err != nil {
		respond.DealWithError(c, err)
		return
	}
	c.JSON(http.StatusOK, respond.RespWithData(respond.Ok, resp))
}

func (h *MemoryHandler) UpdateItem(c *gin.Context) {
	if h == nil || h.client == nil {
		c.JSON(http.StatusInternalServerError, respond.InternalError(errMemoryHandlerNotReady))
		return
	}

	memoryID, ok := parseMemoryIDParam(c)
	if !ok {
		c.JSON(http.StatusBadRequest, respond.WrongParamType)
		return
	}

	var req memorycontracts.UpdateItemRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, respond.WrongParamType)
		return
	}
	req.UserID = c.GetInt("user_id")
	req.MemoryID = memoryID
	req.OperatorType = "user"

	ctx, cancel := context.WithTimeout(c.Request.Context(), 2*time.Second)
	defer cancel()

	resp, err := h.client.UpdateItem(ctx, req)
	if err != nil {
		respond.DealWithError(c, err)
		return
	}
	c.JSON(http.StatusOK, respond.RespWithData(respond.Ok, resp))
}

func (h *MemoryHandler) DeleteItem(c *gin.Context) {
	if h == nil || h.client == nil {
		c.JSON(http.StatusInternalServerError, respond.InternalError(errMemoryHandlerNotReady))
		return
	}

	memoryID, ok := parseMemoryIDParam(c)
	if !ok {
		c.JSON(http.StatusBadRequest, respond.WrongParamType)
		return
	}

	var body struct {
		Reason string `json:"reason"`
	}
	_ = c.ShouldBindJSON(&body)

	ctx, cancel := context.WithTimeout(c.Request.Context(), 2*time.Second)
	defer cancel()

	resp, err := h.client.DeleteItem(ctx, memorycontracts.DeleteItemRequest{
		UserID:       c.GetInt("user_id"),
		MemoryID:     memoryID,
		Reason:       strings.TrimSpace(body.Reason),
		OperatorType: "user",
	})
	if err != nil {
		respond.DealWithError(c, err)
		return
	}
	c.JSON(http.StatusOK, respond.RespWithData(respond.Ok, resp))
}

func (h *MemoryHandler) RestoreItem(c *gin.Context) {
	if h == nil || h.client == nil {
		c.JSON(http.StatusInternalServerError, respond.InternalError(errMemoryHandlerNotReady))
		return
	}

	memoryID, ok := parseMemoryIDParam(c)
	if !ok {
		c.JSON(http.StatusBadRequest, respond.WrongParamType)
		return
	}

	var body struct {
		Reason string `json:"reason"`
	}
	_ = c.ShouldBindJSON(&body)

	ctx, cancel := context.WithTimeout(c.Request.Context(), 2*time.Second)
	defer cancel()

	resp, err := h.client.RestoreItem(ctx, memorycontracts.RestoreItemRequest{
		UserID:       c.GetInt("user_id"),
		MemoryID:     memoryID,
		Reason:       strings.TrimSpace(body.Reason),
		OperatorType: "user",
	})
	if err != nil {
		respond.DealWithError(c, err)
		return
	}
	c.JSON(http.StatusOK, respond.RespWithData(respond.Ok, resp))
}

func parseMemoryIDParam(c *gin.Context) (int64, bool) {
	raw := strings.TrimSpace(c.Param("id"))
	if raw == "" {
		return 0, false
	}
	value, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || value <= 0 {
		return 0, false
	}
	return value, true
}

func parseOptionalInt(raw string) (int, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 0, true
	}
	value, err := strconv.Atoi(raw)
	if err != nil {
		return 0, false
	}
	return value, true
}

func splitCSV(raw string) []string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	parts := strings.Split(raw, ",")
	result := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		result = append(result, part)
	}
	return result
}
