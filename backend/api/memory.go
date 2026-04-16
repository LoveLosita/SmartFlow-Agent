package api

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	memorypkg "github.com/LoveLosita/smartflow/backend/memory"
	memorymodel "github.com/LoveLosita/smartflow/backend/memory/model"
	"github.com/LoveLosita/smartflow/backend/model"
	"github.com/LoveLosita/smartflow/backend/respond"
	"github.com/gin-gonic/gin"
)

type MemoryHandler struct {
	module *memorypkg.Module
}

var errMemoryHandlerNotReady = errors.New("memory handler is not initialized")

func NewMemoryHandler(module *memorypkg.Module) *MemoryHandler {
	return &MemoryHandler{module: module}
}

func (h *MemoryHandler) ListItems(c *gin.Context) {
	if h == nil || h.module == nil {
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

	items, err := h.module.ListItems(ctx, memorymodel.ListItemsRequest{
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

	c.JSON(http.StatusOK, respond.RespWithData(respond.Ok, toMemoryItemViews(items)))
}

func (h *MemoryHandler) GetItem(c *gin.Context) {
	if h == nil || h.module == nil {
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

	item, err := h.module.GetItem(ctx, model.MemoryGetItemRequest{
		UserID:   c.GetInt("user_id"),
		MemoryID: memoryID,
	})
	if err != nil {
		respond.DealWithError(c, err)
		return
	}
	c.JSON(http.StatusOK, respond.RespWithData(respond.Ok, toMemoryItemView(item)))
}

func (h *MemoryHandler) CreateItem(c *gin.Context) {
	if h == nil || h.module == nil {
		c.JSON(http.StatusInternalServerError, respond.InternalError(errMemoryHandlerNotReady))
		return
	}

	var req model.MemoryCreateItemRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, respond.WrongParamType)
		return
	}
	req.UserID = c.GetInt("user_id")
	req.OperatorType = "user"

	ctx, cancel := context.WithTimeout(c.Request.Context(), 2*time.Second)
	defer cancel()

	item, err := h.module.CreateItem(ctx, req)
	if err != nil {
		respond.DealWithError(c, err)
		return
	}
	c.JSON(http.StatusOK, respond.RespWithData(respond.Ok, toMemoryItemView(item)))
}

func (h *MemoryHandler) UpdateItem(c *gin.Context) {
	if h == nil || h.module == nil {
		c.JSON(http.StatusInternalServerError, respond.InternalError(errMemoryHandlerNotReady))
		return
	}

	memoryID, ok := parseMemoryIDParam(c)
	if !ok {
		c.JSON(http.StatusBadRequest, respond.WrongParamType)
		return
	}

	var req model.MemoryUpdateItemRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, respond.WrongParamType)
		return
	}
	req.UserID = c.GetInt("user_id")
	req.MemoryID = memoryID
	req.OperatorType = "user"

	ctx, cancel := context.WithTimeout(c.Request.Context(), 2*time.Second)
	defer cancel()

	item, err := h.module.UpdateItem(ctx, req)
	if err != nil {
		respond.DealWithError(c, err)
		return
	}
	c.JSON(http.StatusOK, respond.RespWithData(respond.Ok, toMemoryItemView(item)))
}

func (h *MemoryHandler) DeleteItem(c *gin.Context) {
	if h == nil || h.module == nil {
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

	item, err := h.module.DeleteItem(ctx, model.MemoryDeleteItemRequest{
		UserID:       c.GetInt("user_id"),
		MemoryID:     memoryID,
		Reason:       strings.TrimSpace(body.Reason),
		OperatorType: "user",
	})
	if err != nil {
		respond.DealWithError(c, err)
		return
	}
	c.JSON(http.StatusOK, respond.RespWithData(respond.Ok, toMemoryItemView(item)))
}

func (h *MemoryHandler) RestoreItem(c *gin.Context) {
	if h == nil || h.module == nil {
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

	item, err := h.module.RestoreItem(ctx, model.MemoryRestoreItemRequest{
		UserID:       c.GetInt("user_id"),
		MemoryID:     memoryID,
		Reason:       strings.TrimSpace(body.Reason),
		OperatorType: "user",
	})
	if err != nil {
		respond.DealWithError(c, err)
		return
	}
	c.JSON(http.StatusOK, respond.RespWithData(respond.Ok, toMemoryItemView(item)))
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

func toMemoryItemViews(items []memorymodel.ItemDTO) []model.MemoryItemView {
	if len(items) == 0 {
		return nil
	}
	result := make([]model.MemoryItemView, 0, len(items))
	for _, item := range items {
		result = append(result, toMemoryItemView(&item))
	}
	return result
}

func toMemoryItemView(item *memorymodel.ItemDTO) model.MemoryItemView {
	if item == nil {
		return model.MemoryItemView{}
	}
	return model.MemoryItemView{
		ID:               item.ID,
		UserID:           item.UserID,
		ConversationID:   item.ConversationID,
		AssistantID:      item.AssistantID,
		RunID:            item.RunID,
		MemoryType:       item.MemoryType,
		Title:            item.Title,
		Content:          item.Content,
		ContentHash:      item.ContentHash,
		Confidence:       item.Confidence,
		Importance:       item.Importance,
		SensitivityLevel: item.SensitivityLevel,
		IsExplicit:       item.IsExplicit,
		Status:           item.Status,
		TTLAt:            item.TTLAt,
		CreatedAt:        item.CreatedAt,
		UpdatedAt:        item.UpdatedAt,
	}
}
