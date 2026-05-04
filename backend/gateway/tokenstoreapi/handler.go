package tokenstoreapi

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	gatewaytokenstore "github.com/LoveLosita/smartflow/backend/gateway/tokenstore"
	"github.com/LoveLosita/smartflow/backend/respond"
	tokencontracts "github.com/LoveLosita/smartflow/backend/shared/contracts/tokenstore"
	"github.com/gin-gonic/gin"
)

const requestTimeout = 2 * time.Second

type TokenStoreClient interface {
	GetSummary(ctx context.Context, actorUserID uint64) (*tokencontracts.TokenSummary, error)
	ListProducts(ctx context.Context, actorUserID uint64) ([]tokencontracts.TokenProductView, error)
	CreateOrder(ctx context.Context, req tokencontracts.CreateTokenOrderRequest) (*gatewaytokenstore.OrderView, error)
	ListOrders(ctx context.Context, req tokencontracts.ListTokenOrdersRequest) ([]gatewaytokenstore.OrderView, tokencontracts.PageResult, error)
	GetOrder(ctx context.Context, actorUserID uint64, orderID uint64) (*gatewaytokenstore.OrderView, error)
	MockPaidOrder(ctx context.Context, req tokencontracts.MockPaidOrderRequest) (*gatewaytokenstore.OrderView, error)
	ListGrants(ctx context.Context, req tokencontracts.ListTokenGrantsRequest) ([]tokencontracts.TokenGrantView, tokencontracts.PageResult, error)
}

type Handler struct {
	client TokenStoreClient
}

func NewHandler(client TokenStoreClient) *Handler {
	return &Handler{client: client}
}

type pageEnvelope[T any] struct {
	Items    []T  `json:"items"`
	Page     int  `json:"page"`
	PageSize int  `json:"page_size"`
	Total    int  `json:"total"`
	HasMore  bool `json:"has_more"`
}

type paymentAction struct {
	Type  string `json:"type"`
	Label string `json:"label"`
}

type orderCreateEnvelope struct {
	OrderID         uint64                             `json:"order_id"`
	OrderNo         string                             `json:"order_no"`
	Status          string                             `json:"status"`
	ProductSnapshot *gatewaytokenstore.ProductSnapshot `json:"product_snapshot"`
	Quantity        int                                `json:"quantity"`
	TokenAmount     int64                              `json:"token_amount"`
	AmountCent      int64                              `json:"amount_cent"`
	PriceText       string                             `json:"price_text"`
	Currency        string                             `json:"currency"`
	PaymentMode     string                             `json:"payment_mode"`
	PaymentAction   paymentAction                      `json:"payment_action"`
	CreatedAt       string                             `json:"created_at"`
}

type orderListItemEnvelope struct {
	OrderID     uint64  `json:"order_id"`
	OrderNo     string  `json:"order_no"`
	Status      string  `json:"status"`
	ProductName string  `json:"product_name"`
	TokenAmount int64   `json:"token_amount"`
	PriceText   string  `json:"price_text"`
	CreatedAt   string  `json:"created_at"`
	PaidAt      *string `json:"paid_at"`
	GrantedAt   *string `json:"granted_at"`
}

type orderDetailEnvelope struct {
	OrderID         uint64                             `json:"order_id"`
	OrderNo         string                             `json:"order_no"`
	Status          string                             `json:"status"`
	ProductSnapshot *gatewaytokenstore.ProductSnapshot `json:"product_snapshot"`
	Quantity        int                                `json:"quantity"`
	TokenAmount     int64                              `json:"token_amount"`
	AmountCent      int64                              `json:"amount_cent"`
	PriceText       string                             `json:"price_text"`
	Currency        string                             `json:"currency"`
	PaymentMode     string                             `json:"payment_mode"`
	Grant           *tokencontracts.TokenGrantView     `json:"grant"`
	CreatedAt       string                             `json:"created_at"`
	PaidAt          *string                            `json:"paid_at"`
	GrantedAt       *string                            `json:"granted_at"`
}

type createOrderBody struct {
	ProductID uint64 `json:"product_id"`
	Quantity  int    `json:"quantity"`
}

type mockPaidBody struct {
	MockChannel string `json:"mock_channel"`
}

func (h *Handler) GetSummary(c *gin.Context) {
	client, ok := h.ready(c)
	if !ok {
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), requestTimeout)
	defer cancel()

	summary, err := client.GetSummary(ctx, currentUserID(c))
	if err != nil {
		respond.DealWithError(c, err)
		return
	}
	c.JSON(http.StatusOK, respond.RespWithData(respond.Ok, summary))
}

func (h *Handler) ListProducts(c *gin.Context) {
	client, ok := h.ready(c)
	if !ok {
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), requestTimeout)
	defer cancel()

	items, err := client.ListProducts(ctx, currentUserID(c))
	if err != nil {
		respond.DealWithError(c, err)
		return
	}
	c.JSON(http.StatusOK, respond.RespWithData(respond.Ok, gin.H{"items": items}))
}

func (h *Handler) CreateOrder(c *gin.Context) {
	client, ok := h.ready(c)
	if !ok {
		return
	}
	var body createOrderBody
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, respond.WrongParamType)
		return
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), requestTimeout)
	defer cancel()
	order, err := client.CreateOrder(ctx, tokencontracts.CreateTokenOrderRequest{
		ActorUserID:    currentUserID(c),
		ProductID:      body.ProductID,
		Quantity:       body.Quantity,
		IdempotencyKey: strings.TrimSpace(c.GetHeader("X-Idempotency-Key")),
	})
	if err != nil {
		respond.DealWithError(c, err)
		return
	}
	c.JSON(http.StatusOK, respond.RespWithData(respond.Ok, newOrderCreateEnvelope(order)))
}

func (h *Handler) ListOrders(c *gin.Context) {
	client, ok := h.ready(c)
	if !ok {
		return
	}
	pageValue, ok := intQuery(c, "page")
	if !ok {
		return
	}
	pageSize, ok := intQuery(c, "page_size")
	if !ok {
		return
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), requestTimeout)
	defer cancel()
	items, page, err := client.ListOrders(ctx, tokencontracts.ListTokenOrdersRequest{
		ActorUserID: currentUserID(c),
		Page:        pageValue,
		PageSize:    pageSize,
		Status:      c.Query("status"),
	})
	if err != nil {
		respond.DealWithError(c, err)
		return
	}
	c.JSON(http.StatusOK, respond.RespWithData(respond.Ok, newPageEnvelope(newOrderListItemEnvelopes(items), page)))
}

func (h *Handler) GetOrder(c *gin.Context) {
	client, ok := h.ready(c)
	if !ok {
		return
	}
	orderID, ok := uint64Param(c, "order_id")
	if !ok {
		return
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), requestTimeout)
	defer cancel()
	order, err := client.GetOrder(ctx, currentUserID(c), orderID)
	if err != nil {
		respond.DealWithError(c, err)
		return
	}
	c.JSON(http.StatusOK, respond.RespWithData(respond.Ok, newOrderDetailEnvelope(order)))
}

func (h *Handler) MockPaidOrder(c *gin.Context) {
	client, ok := h.ready(c)
	if !ok {
		return
	}
	orderID, ok := uint64Param(c, "order_id")
	if !ok {
		return
	}
	var body mockPaidBody
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, respond.WrongParamType)
		return
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), requestTimeout)
	defer cancel()
	order, err := client.MockPaidOrder(ctx, tokencontracts.MockPaidOrderRequest{
		ActorUserID:    currentUserID(c),
		OrderID:        orderID,
		MockChannel:    body.MockChannel,
		IdempotencyKey: strings.TrimSpace(c.GetHeader("X-Idempotency-Key")),
	})
	if err != nil {
		respond.DealWithError(c, err)
		return
	}
	c.JSON(http.StatusOK, respond.RespWithData(respond.Ok, newOrderDetailEnvelope(order)))
}

func (h *Handler) ListGrants(c *gin.Context) {
	client, ok := h.ready(c)
	if !ok {
		return
	}
	pageValue, ok := intQuery(c, "page")
	if !ok {
		return
	}
	pageSize, ok := intQuery(c, "page_size")
	if !ok {
		return
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), requestTimeout)
	defer cancel()
	items, page, err := client.ListGrants(ctx, tokencontracts.ListTokenGrantsRequest{
		ActorUserID: currentUserID(c),
		Page:        pageValue,
		PageSize:    pageSize,
		Source:      c.Query("source"),
	})
	if err != nil {
		respond.DealWithError(c, err)
		return
	}
	c.JSON(http.StatusOK, respond.RespWithData(respond.Ok, newPageEnvelope(items, page)))
}

func (h *Handler) ready(c *gin.Context) (TokenStoreClient, bool) {
	if h == nil || h.client == nil {
		c.JSON(http.StatusInternalServerError, respond.InternalError(errors.New("token-store gateway client 未初始化")))
		return nil, false
	}
	return h.client, true
}

func currentUserID(c *gin.Context) uint64 {
	userID := c.GetInt("user_id")
	if userID <= 0 {
		return 0
	}
	return uint64(userID)
}

func newOrderCreateEnvelope(order *gatewaytokenstore.OrderView) orderCreateEnvelope {
	if order == nil {
		return orderCreateEnvelope{
			PaymentAction: paymentAction{
				Type:  "mock_paid",
				Label: "确认支付",
			},
		}
	}
	return orderCreateEnvelope{
		OrderID:         order.OrderID,
		OrderNo:         order.OrderNo,
		Status:          order.Status,
		ProductSnapshot: order.ProductSnapshot,
		Quantity:        order.Quantity,
		TokenAmount:     order.TokenAmount,
		AmountCent:      order.AmountCent,
		PriceText:       order.PriceText,
		Currency:        order.Currency,
		PaymentMode:     order.PaymentMode,
		PaymentAction: paymentAction{
			Type:  "mock_paid",
			Label: "确认支付",
		},
		CreatedAt: order.CreatedAt,
	}
}

func newOrderListItemEnvelopes(items []gatewaytokenstore.OrderView) []orderListItemEnvelope {
	if len(items) == 0 {
		return []orderListItemEnvelope{}
	}
	result := make([]orderListItemEnvelope, 0, len(items))
	for _, item := range items {
		productName := item.ProductName
		if productName == "" && item.ProductSnapshot != nil {
			productName = item.ProductSnapshot.Name
		}
		result = append(result, orderListItemEnvelope{
			OrderID:     item.OrderID,
			OrderNo:     item.OrderNo,
			Status:      item.Status,
			ProductName: productName,
			TokenAmount: item.TokenAmount,
			PriceText:   item.PriceText,
			CreatedAt:   item.CreatedAt,
			PaidAt:      item.PaidAt,
			GrantedAt:   item.GrantedAt,
		})
	}
	return result
}

func newOrderDetailEnvelope(order *gatewaytokenstore.OrderView) orderDetailEnvelope {
	if order == nil {
		return orderDetailEnvelope{}
	}
	return orderDetailEnvelope{
		OrderID:         order.OrderID,
		OrderNo:         order.OrderNo,
		Status:          order.Status,
		ProductSnapshot: order.ProductSnapshot,
		Quantity:        order.Quantity,
		TokenAmount:     order.TokenAmount,
		AmountCent:      order.AmountCent,
		PriceText:       order.PriceText,
		Currency:        order.Currency,
		PaymentMode:     order.PaymentMode,
		Grant:           order.Grant,
		CreatedAt:       order.CreatedAt,
		PaidAt:          order.PaidAt,
		GrantedAt:       order.GrantedAt,
	}
}

func newPageEnvelope[T any](items []T, page tokencontracts.PageResult) pageEnvelope[T] {
	return pageEnvelope[T]{
		Items:    items,
		Page:     page.Page,
		PageSize: page.PageSize,
		Total:    page.Total,
		HasMore:  page.HasMore,
	}
}

func intQuery(c *gin.Context, key string) (int, bool) {
	raw := strings.TrimSpace(c.Query(key))
	if raw == "" {
		return 0, true
	}
	value, err := strconv.Atoi(raw)
	if err != nil {
		c.JSON(http.StatusBadRequest, respond.WrongParamType)
		return 0, false
	}
	return value, true
}

func uint64Param(c *gin.Context, key string) (uint64, bool) {
	value, err := strconv.ParseUint(strings.TrimSpace(c.Param(key)), 10, 64)
	if err != nil || value == 0 {
		c.JSON(http.StatusBadRequest, respond.WrongParamType)
		return 0, false
	}
	return value, true
}
