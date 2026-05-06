package tokenstoreapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/LoveLosita/smartflow/backend/gateway/shared/respond"
	creditcontracts "github.com/LoveLosita/smartflow/backend/shared/contracts/creditstore"
	"github.com/gin-gonic/gin"
)

const requestTimeout = 5 * time.Second

const (
	creditConsumptionPeriod24h = "24h"
	creditConsumptionPeriod7d  = "7d"
	creditConsumptionPeriod30d = "30d"
	creditConsumptionPeriodAll = "all"
	creditDashboardPageSize    = 50
)

// TokenStoreClient 是商店页 credit-store 语义所需的最小依赖面。
//
// 职责边界：
// 1. 只暴露 Credit 商店和流水所需能力，不再承接旧 token 商店接口。
// 2. 所有方法都以“当前登录用户”口径访问，不开放跨用户查询。
// 3. 具体 DB、RPC、Redis 细节统一封装在 tokenstore client 内。
type TokenStoreClient interface {
	GetCreditBalanceSnapshot(ctx context.Context, userID uint64) (*creditcontracts.CreditBalanceSnapshot, error)
	GetCreditConsumptionDashboard(ctx context.Context, req creditcontracts.GetCreditConsumptionDashboardRequest) (*creditcontracts.CreditConsumptionDashboardView, error)
	ListCreditProducts(ctx context.Context, actorUserID uint64) ([]creditcontracts.CreditProductView, error)
	CreateCreditOrder(ctx context.Context, req creditcontracts.CreateCreditOrderRequest) (*creditcontracts.CreditOrderView, error)
	ListCreditOrders(ctx context.Context, req creditcontracts.ListCreditOrdersRequest) ([]creditcontracts.CreditOrderView, creditcontracts.PageResult, error)
	GetCreditOrder(ctx context.Context, actorUserID uint64, orderID uint64) (*creditcontracts.CreditOrderView, error)
	MockPaidCreditOrder(ctx context.Context, req creditcontracts.MockPaidCreditOrderRequest) (*creditcontracts.CreditOrderView, error)
	ListCreditTransactions(ctx context.Context, req creditcontracts.ListCreditTransactionsRequest) ([]creditcontracts.CreditTransactionView, creditcontracts.PageResult, error)
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

type creditSummaryEnvelope struct {
	CurrentCreditTotal      int64   `json:"current_credit_total"`
	RecordedCreditTotal     int64   `json:"recorded_credit_total"`
	AppliedCreditTotal      int64   `json:"applied_credit_total"`
	PendingApplyCreditTotal int64   `json:"pending_apply_credit_total"`
	ValidUntil              *string `json:"valid_until"`
	QuotaSyncStatus         string  `json:"quota_sync_status"`
	Tip                     string  `json:"tip"`
}

type paymentAction struct {
	Type  string `json:"type"`
	Label string `json:"label"`
}

type creditOrderEnvelope struct {
	OrderID       uint64         `json:"order_id"`
	OrderNo       string         `json:"order_no"`
	Status        string         `json:"status"`
	Quantity      int            `json:"quantity"`
	CreditAmount  int64          `json:"credit_amount"`
	AmountCent    int64          `json:"amount_cent"`
	PriceText     string         `json:"price_text"`
	Currency      string         `json:"currency"`
	PaymentMode   string         `json:"payment_mode"`
	ProductName   string         `json:"product_name"`
	ProductDetail map[string]any `json:"product_snapshot,omitempty"`
	CreatedAt     string         `json:"created_at"`
	PaidAt        *string        `json:"paid_at"`
	CreditedAt    *string        `json:"credited_at"`
	PaymentAction paymentAction  `json:"payment_action"`
}

type creditTransactionEnvelope struct {
	GrantID      uint64  `json:"grant_id"`
	SourceLabel  string  `json:"source_label"`
	Amount       int64   `json:"amount"`
	Status       string  `json:"status"`
	Description  string  `json:"description"`
	CreatedAt    string  `json:"created_at"`
	Direction    string  `json:"direction"`
	BalanceAfter int64   `json:"balance_after"`
	EventID      string  `json:"event_id"`
	OrderID      *uint64 `json:"order_id"`
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

	snapshot, err := client.GetCreditBalanceSnapshot(ctx, currentUserID(c))
	if err != nil {
		respond.DealWithError(c, err)
		return
	}
	c.JSON(http.StatusOK, respond.RespWithData(respond.Ok, buildCreditSummaryEnvelope(snapshot)))
}

func (h *Handler) GetConsumptionDashboard(c *gin.Context) {
	client, ok := h.ready(c)
	if !ok {
		return
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), requestTimeout)
	defer cancel()

	dashboard, err := client.GetCreditConsumptionDashboard(ctx, creditcontracts.GetCreditConsumptionDashboardRequest{
		ActorUserID: currentUserID(c),
		Period:      c.Query("period"),
	})
	if err != nil {
		respond.DealWithError(c, err)
		return
	}
	c.JSON(http.StatusOK, respond.RespWithData(respond.Ok, dashboard))
}

func (h *Handler) ListProducts(c *gin.Context) {
	client, ok := h.ready(c)
	if !ok {
		return
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), requestTimeout)
	defer cancel()

	items, err := client.ListCreditProducts(ctx, currentUserID(c))
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

	order, err := client.CreateCreditOrder(ctx, creditcontracts.CreateCreditOrderRequest{
		ActorUserID:    currentUserID(c),
		ProductID:      body.ProductID,
		Quantity:       body.Quantity,
		IdempotencyKey: strings.TrimSpace(c.GetHeader("X-Idempotency-Key")),
	})
	if err != nil {
		respond.DealWithError(c, err)
		return
	}
	c.JSON(http.StatusOK, respond.RespWithData(respond.Ok, buildCreditOrderEnvelope(order)))
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

	items, page, err := client.ListCreditOrders(ctx, creditcontracts.ListCreditOrdersRequest{
		ActorUserID: currentUserID(c),
		Page:        pageValue,
		PageSize:    pageSize,
		Status:      c.Query("status"),
	})
	if err != nil {
		respond.DealWithError(c, err)
		return
	}

	result := make([]creditOrderEnvelope, 0, len(items))
	for i := range items {
		item := items[i]
		result = append(result, buildCreditOrderEnvelope(&item))
	}
	c.JSON(http.StatusOK, respond.RespWithData(respond.Ok, newPageEnvelope(result, page)))
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

	order, err := client.GetCreditOrder(ctx, currentUserID(c), orderID)
	if err != nil {
		respond.DealWithError(c, err)
		return
	}
	c.JSON(http.StatusOK, respond.RespWithData(respond.Ok, buildCreditOrderEnvelope(order)))
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

	order, err := client.MockPaidCreditOrder(ctx, creditcontracts.MockPaidCreditOrderRequest{
		ActorUserID:    currentUserID(c),
		OrderID:        orderID,
		MockChannel:    firstNonEmptyString(strings.TrimSpace(body.MockChannel), "mock"),
		IdempotencyKey: strings.TrimSpace(c.GetHeader("X-Idempotency-Key")),
	})
	if err != nil {
		respond.DealWithError(c, err)
		return
	}
	c.JSON(http.StatusOK, respond.RespWithData(respond.Ok, buildCreditOrderEnvelope(order)))
}

func (h *Handler) ListTransactions(c *gin.Context) {
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

	items, page, err := client.ListCreditTransactions(ctx, creditcontracts.ListCreditTransactionsRequest{
		ActorUserID: currentUserID(c),
		Page:        pageValue,
		PageSize:    pageSize,
		Source:      c.Query("source"),
		Direction:   c.Query("direction"),
	})
	if err != nil {
		respond.DealWithError(c, err)
		return
	}

	result := make([]creditTransactionEnvelope, 0, len(items))
	for i := range items {
		result = append(result, buildCreditTransactionEnvelope(items[i]))
	}
	c.JSON(http.StatusOK, respond.RespWithData(respond.Ok, newPageEnvelope(result, page)))
}

func (h *Handler) ready(c *gin.Context) (TokenStoreClient, bool) {
	if h == nil || h.client == nil {
		c.JSON(http.StatusInternalServerError, respond.InternalError(errors.New("credit-store gateway client 未初始化")))
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

// buildCreditSummaryEnvelope 负责把权威余额快照转换成钱包摘要返回值。
//
// 职责边界：
// 1. current_credit_total 明确表示“当前可用 Credit 总数”，口径直接复用权威余额。
// 2. 老字段继续保留，避免影响现有前端和兼容逻辑。
// 3. 这里只做展示层拼装，不在网关重复做扣费或账本计算。
func buildCreditSummaryEnvelope(snapshot *creditcontracts.CreditBalanceSnapshot) creditSummaryEnvelope {
	if snapshot == nil {
		return creditSummaryEnvelope{
			CurrentCreditTotal:      0,
			RecordedCreditTotal:     0,
			AppliedCreditTotal:      0,
			PendingApplyCreditTotal: 0,
			ValidUntil:              nil,
			QuotaSyncStatus:         "synced",
			Tip:                     "当前为 Credit 权威账本，购买、奖励和 AI 消费都会实时入账。",
		}
	}

	recordedTotal := snapshot.TotalRecharged + snapshot.TotalRewarded
	if recordedTotal <= 0 && snapshot.Balance > 0 {
		recordedTotal = snapshot.Balance + snapshot.TotalConsumed
	}

	tip := "当前为 Credit 权威账本，购买、奖励和 AI 消费都会实时入账。"
	if snapshot.IsBlocked {
		tip = "当前 Credit 余额不足，AI 调用会被阻断；充值后会自动恢复。"
	}

	return creditSummaryEnvelope{
		CurrentCreditTotal:      snapshot.Balance,
		RecordedCreditTotal:     maxInt64(recordedTotal, 0),
		AppliedCreditTotal:      snapshot.Balance,
		PendingApplyCreditTotal: 0,
		ValidUntil:              nil,
		QuotaSyncStatus:         "synced",
		Tip:                     tip,
	}
}

func buildCreditOrderEnvelope(order *creditcontracts.CreditOrderView) creditOrderEnvelope {
	if order == nil {
		return creditOrderEnvelope{
			PaymentAction: paymentAction{
				Type:  "mock_paid",
				Label: "确认支付",
			},
		}
	}

	return creditOrderEnvelope{
		OrderID:       order.OrderID,
		OrderNo:       order.OrderNo,
		Status:        order.Status,
		Quantity:      order.Quantity,
		CreditAmount:  order.CreditAmount,
		AmountCent:    order.AmountCent,
		PriceText:     order.PriceText,
		Currency:      order.Currency,
		PaymentMode:   order.PaymentMode,
		ProductName:   order.ProductName,
		ProductDetail: parseJSONMap(order.ProductSnapshot),
		CreatedAt:     order.CreatedAt,
		PaidAt:        order.PaidAt,
		CreditedAt:    order.CreditedAt,
		PaymentAction: paymentAction{
			Type:  "mock_paid",
			Label: "确认支付",
		},
	}
}

func buildCreditTransactionEnvelope(item creditcontracts.CreditTransactionView) creditTransactionEnvelope {
	return creditTransactionEnvelope{
		GrantID:      item.TransactionID,
		SourceLabel:  item.SourceLabel,
		Amount:       item.Amount,
		Status:       item.Status,
		Description:  firstNonEmptyString(item.Description, item.SourceLabel),
		CreatedAt:    item.CreatedAt,
		Direction:    item.Direction,
		BalanceAfter: item.BalanceAfter,
		EventID:      item.EventID,
		OrderID:      item.OrderID,
	}
}

func parseJSONMap(raw string) map[string]any {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return nil
	}

	result := make(map[string]any)
	if err := json.Unmarshal([]byte(trimmed), &result); err != nil {
		return nil
	}
	return result
}

func newPageEnvelope[T any](items []T, page creditcontracts.PageResult) pageEnvelope[T] {
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

func firstNonEmptyString(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

func maxInt64(left int64, right int64) int64 {
	if left > right {
		return left
	}
	return right
}

func normalizeCreditConsumptionPeriod(raw string) (string, error) {
	switch strings.TrimSpace(raw) {
	case "", creditConsumptionPeriod24h:
		return creditConsumptionPeriod24h, nil
	case creditConsumptionPeriod7d:
		return creditConsumptionPeriod7d, nil
	case creditConsumptionPeriod30d:
		return creditConsumptionPeriod30d, nil
	case creditConsumptionPeriodAll:
		return creditConsumptionPeriodAll, nil
	default:
		return "", errors.New("invalid consumption period")
	}
}

func buildCreditConsumptionDashboard(ctx context.Context, client TokenStoreClient, actorUserID uint64, period string) (creditcontracts.CreditConsumptionDashboardView, error) {
	startAt, hasWindow := resolveCreditConsumptionWindow(period, time.Now())
	dashboard := creditcontracts.CreditConsumptionDashboardView{
		Period: period,
	}

	for page := 1; ; page++ {
		items, pageResult, err := client.ListCreditTransactions(ctx, creditcontracts.ListCreditTransactionsRequest{
			ActorUserID: actorUserID,
			Page:        page,
			PageSize:    creditDashboardPageSize,
			Source:      "charge",
			Direction:   "expense",
		})
		if err != nil {
			return creditcontracts.CreditConsumptionDashboardView{}, err
		}
		if len(items) == 0 {
			return dashboard, nil
		}

		for _, item := range items {
			if strings.EqualFold(strings.TrimSpace(item.Status), "failed") {
				continue
			}

			createdAt, ok := parseCreditTransactionCreatedAt(item.CreatedAt)
			if hasWindow && ok && createdAt.Before(startAt) {
				continue
			}
			if hasWindow && !ok {
				continue
			}

			dashboard.CreditConsumed += normalizeCreditConsumedAmount(item.Amount)
			dashboard.TokenConsumed += extractChargeTokenConsumed(item.MetadataJSON)
		}

		if !pageResult.HasMore {
			return dashboard, nil
		}
		if hasWindow && isCreditTransactionPageBeforeWindow(items, startAt) {
			return dashboard, nil
		}
	}
}

func resolveCreditConsumptionWindow(period string, now time.Time) (time.Time, bool) {
	switch strings.TrimSpace(period) {
	case creditConsumptionPeriod24h:
		return now.Add(-24 * time.Hour), true
	case creditConsumptionPeriod7d:
		return now.Add(-7 * 24 * time.Hour), true
	case creditConsumptionPeriod30d:
		return now.Add(-30 * 24 * time.Hour), true
	default:
		return time.Time{}, false
	}
}

func parseCreditTransactionCreatedAt(raw string) (time.Time, bool) {
	parsed, err := time.Parse(time.RFC3339, strings.TrimSpace(raw))
	if err != nil {
		return time.Time{}, false
	}
	return parsed, true
}

func isCreditTransactionPageBeforeWindow(items []creditcontracts.CreditTransactionView, startAt time.Time) bool {
	if len(items) == 0 {
		return false
	}

	oldest, ok := parseCreditTransactionCreatedAt(items[len(items)-1].CreatedAt)
	if !ok {
		return false
	}
	return oldest.Before(startAt)
}

func normalizeCreditConsumedAmount(amount int64) int64 {
	if amount >= 0 {
		return 0
	}
	return -amount
}

func extractChargeTokenConsumed(metadataJSON string) int64 {
	if strings.TrimSpace(metadataJSON) == "" {
		return 0
	}

	var metadata struct {
		InputTokens  int64 `json:"input_tokens"`
		OutputTokens int64 `json:"output_tokens"`
		TotalTokens  int64 `json:"total_tokens"`
	}
	if err := json.Unmarshal([]byte(metadataJSON), &metadata); err != nil {
		return 0
	}
	if metadata.TotalTokens > 0 {
		return metadata.TotalTokens
	}

	total := metadata.InputTokens + metadata.OutputTokens
	if total < 0 {
		return 0
	}
	return total
}
