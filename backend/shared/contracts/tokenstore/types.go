package tokenstore

// PageResult 是 token-store 分页响应的跨层契约。
type PageResult struct {
	Page     int  `json:"page"`
	PageSize int  `json:"page_size"`
	Total    int  `json:"total"`
	HasMore  bool `json:"has_more"`
}

// TokenSummary 是 Token 商店概览响应。
//
// 职责边界：
// 1. P0 展示 token-store 已记录的获取事实；
// 2. 不承诺这些 Token 已经同步到 user/auth 权威额度；
// 3. 后续接入 user/auth 后可把 QuotaSyncStatus 调整为 synced。
type TokenSummary struct {
	RecordedTokenTotal     int64  `json:"recorded_token_total"`
	AppliedTokenTotal      int64  `json:"applied_token_total"`
	PendingApplyTokenTotal int64  `json:"pending_apply_token_total"`
	QuotaSyncStatus        string `json:"quota_sync_status"`
	Tip                    string `json:"tip"`
}

// TokenProductView 是商品卡片展示结构。
type TokenProductView struct {
	ProductID   uint64 `json:"product_id"`
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

// TokenGrantView 是 Token 获取记录展示结构。
type TokenGrantView struct {
	GrantID      uint64 `json:"grant_id"`
	EventID      string `json:"event_id"`
	Source       string `json:"source"`
	SourceLabel  string `json:"source_label"`
	Amount       int64  `json:"amount"`
	Status       string `json:"status"`
	QuotaApplied bool   `json:"quota_applied"`
	Description  string `json:"description"`
	CreatedAt    string `json:"created_at"`
}

// TokenOrderView 是订单展示结构。
type TokenOrderView struct {
	OrderID         uint64          `json:"order_id"`
	OrderNo         string          `json:"order_no"`
	Status          string          `json:"status"`
	ProductSnapshot string          `json:"product_snapshot"`
	ProductName     string          `json:"product_name"`
	Quantity        int             `json:"quantity"`
	TokenAmount     int64           `json:"token_amount"`
	AmountCent      int64           `json:"amount_cent"`
	PriceText       string          `json:"price_text"`
	Currency        string          `json:"currency"`
	PaymentMode     string          `json:"payment_mode"`
	Grant           *TokenGrantView `json:"grant"`
	CreatedAt       string          `json:"created_at"`
	PaidAt          *string         `json:"paid_at"`
	GrantedAt       *string         `json:"granted_at"`
}

// CreateTokenOrderRequest 是创建订单请求契约。
type CreateTokenOrderRequest struct {
	ActorUserID    uint64 `json:"actor_user_id"`
	ProductID      uint64 `json:"product_id"`
	Quantity       int    `json:"quantity"`
	IdempotencyKey string `json:"idempotency_key"`
}

// ListTokenOrdersRequest 是订单列表查询契约。
type ListTokenOrdersRequest struct {
	ActorUserID uint64 `json:"actor_user_id"`
	Page        int    `json:"page"`
	PageSize    int    `json:"page_size"`
	Status      string `json:"status"`
}

// MockPaidOrderRequest 是 P0 mock paid 请求契约。
type MockPaidOrderRequest struct {
	ActorUserID    uint64 `json:"actor_user_id"`
	OrderID        uint64 `json:"order_id"`
	MockChannel    string `json:"mock_channel"`
	IdempotencyKey string `json:"idempotency_key"`
}

// ListTokenGrantsRequest 是 Token 获取记录列表查询契约。
type ListTokenGrantsRequest struct {
	ActorUserID uint64 `json:"actor_user_id"`
	Page        int    `json:"page"`
	PageSize    int    `json:"page_size"`
	Source      string `json:"source"`
}

// RecordForumRewardGrantRequest 是论坛奖励入账的内部 RPC 契约。
//
// 职责边界：
// 1. 只描述一条待记录到 token_grants 的论坛奖励事实；
// 2. 不携带最终奖励金额，金额由 token-store 按 source 和配置解析；
// 3. source_ref_id 使用字符串承接 post_id / import_id，服务层再按当前库表结构落成整数。
type RecordForumRewardGrantRequest struct {
	EventID        string `json:"event_id"`
	ReceiverUserID uint64 `json:"receiver_user_id"`
	Source         string `json:"source"`
	SourceRefID    string `json:"source_ref_id"`
}

// TokenGrantRecord 是 token-store 内部发放出口使用的获取事实。
type TokenGrantRecord struct {
	EventID     string `json:"event_id"`
	UserID      uint64 `json:"user_id"`
	Source      string `json:"source"`
	SourceRefID uint64 `json:"source_ref_id"`
	OrderID     uint64 `json:"order_id"`
	Amount      int64  `json:"amount"`
	Description string `json:"description"`
}
