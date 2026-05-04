package sv

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/LoveLosita/smartflow/backend/respond"
	tokenmodel "github.com/LoveLosita/smartflow/backend/services/tokenstore/model"
	tokencontracts "github.com/LoveLosita/smartflow/backend/shared/contracts/tokenstore"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

const (
	defaultPage     = 1
	defaultPageSize = 20
	maxPageSize     = 50

	tokenSummaryQuotaStatusNotConnected = "not_connected"
	tokenSummaryTipP0                   = "当前仅统计 Token 商店已记录的获得记录，尚未同步到 user/auth 可用额度。"
)

type productSnapshot struct {
	ProductID   uint64 `json:"product_id"`
	SKU         string `json:"sku"`
	Name        string `json:"name"`
	Description string `json:"description"`
	TokenAmount int64  `json:"token_amount"`
	PriceCent   int64  `json:"price_cent"`
	PriceText   string `json:"price_text"`
	Currency    string `json:"currency"`
	Badge       string `json:"badge"`
	Status      string `json:"status"`
	SortOrder   int    `json:"sort_order"`
}

func normalizePage(page int, pageSize int) (int, int) {
	if page <= 0 {
		page = defaultPage
	}
	if pageSize <= 0 {
		pageSize = defaultPageSize
	}
	if pageSize > maxPageSize {
		pageSize = maxPageSize
	}
	return page, pageSize
}

func pageResult(page int, pageSize int, total int64) tokencontracts.PageResult {
	return tokencontracts.PageResult{
		Page:     page,
		PageSize: pageSize,
		Total:    int(total),
		HasMore:  int64(page*pageSize) < total,
	}
}

func formatTime(value time.Time) string {
	if value.IsZero() {
		return ""
	}
	return value.Format(time.RFC3339)
}

func formatTimePtr(value *time.Time) *string {
	if value == nil || value.IsZero() {
		return nil
	}
	formatted := value.Format(time.RFC3339)
	return &formatted
}

func formatPriceText(currency string, amountCent int64) string {
	if strings.EqualFold(strings.TrimSpace(currency), "CNY") {
		return fmt.Sprintf("¥%.2f", float64(amountCent)/100)
	}
	return fmt.Sprintf("%s %.2f", strings.ToUpper(strings.TrimSpace(currency)), float64(amountCent)/100)
}

func stringPtrFromNonEmpty(value string) *string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return nil
	}
	return &trimmed
}

func productViewFromModel(product tokenmodel.TokenProduct) tokencontracts.TokenProductView {
	return tokencontracts.TokenProductView{
		ProductID:   product.ID,
		Name:        product.Name,
		Description: product.Description,
		TokenAmount: product.TokenAmount,
		PriceCent:   product.PriceCent,
		PriceText:   formatPriceText(product.Currency, product.PriceCent),
		Currency:    product.Currency,
		Badge:       product.Badge,
		Status:      product.Status,
		SortOrder:   product.SortOrder,
	}
}

func grantViewFromModel(grant tokenmodel.TokenGrant) tokencontracts.TokenGrantView {
	return tokencontracts.TokenGrantView{
		GrantID:      grant.ID,
		EventID:      grant.EventID,
		Source:       grant.Source,
		SourceLabel:  grantSourceLabel(grant.Source, grant.SourceLabel),
		Amount:       grant.Amount,
		Status:       grant.Status,
		QuotaApplied: grant.QuotaApplied,
		Description:  grant.Description,
		CreatedAt:    formatTime(grant.CreatedAt),
	}
}

func orderViewFromModel(order tokenmodel.TokenOrder, grant *tokenmodel.TokenGrant) tokencontracts.TokenOrderView {
	var grantView *tokencontracts.TokenGrantView
	if grant != nil {
		view := grantViewFromModel(*grant)
		grantView = &view
	}

	return tokencontracts.TokenOrderView{
		OrderID:         order.ID,
		OrderNo:         order.OrderNo,
		Status:          order.Status,
		ProductSnapshot: order.ProductSnapshotJSON,
		ProductName:     order.ProductName,
		Quantity:        order.Quantity,
		TokenAmount:     order.TokenAmount,
		AmountCent:      order.AmountCent,
		PriceText:       formatPriceText(order.Currency, order.AmountCent),
		Currency:        order.Currency,
		PaymentMode:     order.PaymentMode,
		Grant:           grantView,
		CreatedAt:       formatTime(order.CreatedAt),
		PaidAt:          formatTimePtr(order.PaidAt),
		GrantedAt:       formatTimePtr(order.GrantedAt),
	}
}

func grantSourceLabel(source string, fallback string) string {
	if strings.TrimSpace(fallback) != "" {
		return fallback
	}
	switch strings.TrimSpace(source) {
	case tokenmodel.TokenGrantSourcePurchase:
		return "购买充值"
	case tokenmodel.TokenGrantSourceForumLike:
		return "计划被点赞"
	case tokenmodel.TokenGrantSourceForumImport:
		return "计划被导入"
	case tokenmodel.TokenGrantSourceManual:
		return "人工补发"
	default:
		return "Token 获得记录"
	}
}

func buildProductSnapshot(product tokenmodel.TokenProduct) (string, error) {
	snapshot := productSnapshot{
		ProductID:   product.ID,
		SKU:         product.SKU,
		Name:        product.Name,
		Description: product.Description,
		TokenAmount: product.TokenAmount,
		PriceCent:   product.PriceCent,
		PriceText:   formatPriceText(product.Currency, product.PriceCent),
		Currency:    product.Currency,
		Badge:       product.Badge,
		Status:      product.Status,
		SortOrder:   product.SortOrder,
	}
	raw, err := json.Marshal(snapshot)
	if err != nil {
		return "", err
	}
	return string(raw), nil
}

func newOrderNo() string {
	return fmt.Sprintf(
		"TS%s%s",
		time.Now().Format("20060102150405"),
		strings.ReplaceAll(uuid.NewString(), "-", ""),
	)
}

func purchaseGrantEventID(orderID uint64) string {
	return fmt.Sprintf("order:%d:paid", orderID)
}

func purchaseGrantDescription(productName string) string {
	trimmed := strings.TrimSpace(productName)
	if trimmed == "" {
		return "购买 Token 商品"
	}
	return fmt.Sprintf("购买%s", trimmed)
}

func isDuplicateKeyError(err error) bool {
	if err == nil {
		return false
	}
	lower := strings.ToLower(err.Error())
	return strings.Contains(lower, "duplicate entry") ||
		strings.Contains(lower, "duplicate key") ||
		strings.Contains(lower, "unique constraint") ||
		strings.Contains(lower, "unique violation") ||
		strings.Contains(lower, "error 1062")
}

func normalizeRecordNotFound(err error, fallback error) error {
	if errorsIsRecordNotFound(err) {
		return fallback
	}
	return err
}

func errorsIsRecordNotFound(err error) bool {
	return errors.Is(err, gorm.ErrRecordNotFound)
}

// tokenStoreBadRequestStatus 是 token-store P0 统一业务校验错误码。
// 具体错误原因仍放在 Info，避免为每个商品/订单校验分支提前扩散大量细分码。
const tokenStoreBadRequestStatus = "40067"

func tokenStoreBadRequest(message string) respond.Response {
	return respond.Response{
		Status: tokenStoreBadRequestStatus,
		Info:   strings.TrimSpace(message),
	}
}
