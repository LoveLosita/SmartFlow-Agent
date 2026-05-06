package creditstore

// PageResult 是 Credit 领域的分页结果契约。
type PageResult struct {
	Page     int  `json:"page"`
	PageSize int  `json:"page_size"`
	Total    int  `json:"total"`
	HasMore  bool `json:"has_more"`
}

// CreditBalanceSnapshot 提供给商店页和 LLM 准入 Guard 的余额快照。
//
// 职责边界：
// 1. 只表达 TokenStore 权威账本视角下的 Credit 余额与阻断状态。
// 2. snapshot_source 用于说明结果来自 cache 还是 db。
// 3. 不承载任何扣费规则细节，避免把价格表语义耦合到余额查询。
type CreditBalanceSnapshot struct {
	UserID         uint64 `json:"user_id"`
	Balance        int64  `json:"balance"`
	TotalRecharged int64  `json:"total_recharged"`
	TotalRewarded  int64  `json:"total_rewarded"`
	TotalConsumed  int64  `json:"total_consumed"`
	IsBlocked      bool   `json:"is_blocked"`
	SnapshotSource string `json:"snapshot_source"`
	UpdatedAt      string `json:"updated_at"`
}

// CreditProductView 是 Credit 商品卡片展示结构。
type CreditProductView struct {
	ProductID         uint64 `json:"product_id"`
	Name              string `json:"name"`
	Description       string `json:"description"`
	CreditAmount      int64  `json:"credit_amount"`
	PriceCent         int64  `json:"price_cent"`
	OriginalPriceCent int64  `json:"original_price_cent"`
	PriceText         string `json:"price_text"`
	Currency          string `json:"currency"`
	Badge             string `json:"badge"`
	Status            string `json:"status"`
	SortOrder         int    `json:"sort_order"`
}

// CreditOrderView 是 Credit 订单展示结构。
type CreditOrderView struct {
	OrderID         uint64  `json:"order_id"`
	OrderNo         string  `json:"order_no"`
	Status          string  `json:"status"`
	ProductSnapshot string  `json:"product_snapshot"`
	ProductName     string  `json:"product_name"`
	Quantity        int     `json:"quantity"`
	CreditAmount    int64   `json:"credit_amount"`
	AmountCent      int64   `json:"amount_cent"`
	PriceText       string  `json:"price_text"`
	Currency        string  `json:"currency"`
	PaymentMode     string  `json:"payment_mode"`
	CreatedAt       string  `json:"created_at"`
	PaidAt          *string `json:"paid_at"`
	CreditedAt      *string `json:"credited_at"`
}

// CreditTransactionView 是 Credit 流水展示结构。
type CreditTransactionView struct {
	TransactionID uint64  `json:"transaction_id"`
	EventID       string  `json:"event_id"`
	Source        string  `json:"source"`
	SourceLabel   string  `json:"source_label"`
	Direction     string  `json:"direction"`
	Amount        int64   `json:"amount"`
	BalanceAfter  int64   `json:"balance_after"`
	Status        string  `json:"status"`
	Description   string  `json:"description"`
	MetadataJSON  string  `json:"metadata_json"`
	CreatedAt     string  `json:"created_at"`
	OrderID       *uint64 `json:"order_id"`
}

const (
	// CreditConsumptionPeriod24h 表示统计最近 24 小时内的消耗。
	CreditConsumptionPeriod24h = "24h"
	// CreditConsumptionPeriod7d 表示统计最近 7 天内的消耗。
	CreditConsumptionPeriod7d = "7d"
	// CreditConsumptionPeriod30d 表示统计最近 30 天内的消耗。
	CreditConsumptionPeriod30d = "30d"
	// CreditConsumptionPeriodAll 表示统计全部历史消耗。
	CreditConsumptionPeriodAll = "all"
)

// CreditConsumptionDashboardView 是商店页顶部消耗看板的展示结构。
type CreditConsumptionDashboardView struct {
	Period         string `json:"period"`
	CreditConsumed int64  `json:"credit_consumed"`
	TokenConsumed  int64  `json:"token_consumed"`
}

// CreditPriceRuleView 是 Credit 计价规则展示结构。
type CreditPriceRuleView struct {
	RuleID               uint64 `json:"rule_id"`
	Scene                string `json:"scene"`
	ProviderName         string `json:"provider_name"`
	ModelName            string `json:"model_name"`
	InputPriceMicros     int64  `json:"input_price_micros"`
	OutputPriceMicros    int64  `json:"output_price_micros"`
	CachedPriceMicros    int64  `json:"cached_price_micros"`
	ReasoningPriceMicros int64  `json:"reasoning_price_micros"`
	CreditPerYuan        int64  `json:"credit_per_yuan"`
	Status               string `json:"status"`
	Priority             int    `json:"priority"`
	Description          string `json:"description"`
}

// CreditRewardRuleView 是 Credit 奖励规则展示结构。
type CreditRewardRuleView struct {
	RuleID      uint64 `json:"rule_id"`
	Source      string `json:"source"`
	Name        string `json:"name"`
	Amount      int64  `json:"amount"`
	Status      string `json:"status"`
	Description string `json:"description"`
}

// CreateCreditOrderRequest 是创建 Credit 订单请求契约。
type CreateCreditOrderRequest struct {
	ActorUserID    uint64 `json:"actor_user_id"`
	ProductID      uint64 `json:"product_id"`
	Quantity       int    `json:"quantity"`
	IdempotencyKey string `json:"idempotency_key"`
}

// ListCreditOrdersRequest 是 Credit 订单列表查询契约。
type ListCreditOrdersRequest struct {
	ActorUserID uint64 `json:"actor_user_id"`
	Page        int    `json:"page"`
	PageSize    int    `json:"page_size"`
	Status      string `json:"status"`
}

// GetCreditConsumptionDashboardRequest 是查询当前用户消耗看板的契约。
type GetCreditConsumptionDashboardRequest struct {
	ActorUserID uint64 `json:"actor_user_id"`
	Period      string `json:"period"`
}

// MockPaidCreditOrderRequest 是 Credit 商店 mock paid 请求契约。
type MockPaidCreditOrderRequest struct {
	ActorUserID    uint64 `json:"actor_user_id"`
	OrderID        uint64 `json:"order_id"`
	MockChannel    string `json:"mock_channel"`
	IdempotencyKey string `json:"idempotency_key"`
}

// ListCreditTransactionsRequest 是当前用户查询自己 Credit 流水的契约。
type ListCreditTransactionsRequest struct {
	ActorUserID uint64 `json:"actor_user_id"`
	Page        int    `json:"page"`
	PageSize    int    `json:"page_size"`
	Source      string `json:"source"`
	Direction   string `json:"direction"`
}

// ListCreditPriceRulesRequest 是 Credit 价格规则查询契约。
type ListCreditPriceRulesRequest struct {
	Scene        string `json:"scene"`
	ProviderName string `json:"provider_name"`
	ModelName    string `json:"model_name"`
	Status       string `json:"status"`
}

// ListCreditRewardRulesRequest 是 Credit 奖励规则查询契约。
type ListCreditRewardRulesRequest struct {
	Source string `json:"source"`
	Status string `json:"status"`
}
