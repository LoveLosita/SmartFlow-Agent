package model

import "time"

const (
	// TokenProductStatusActive 表示商品可在 Token 商店展示和购买。
	TokenProductStatusActive = "active"
	// TokenProductStatusInactive 表示商品已下架，不再对前端展示。
	TokenProductStatusInactive = "inactive"
)

const (
	// TokenOrderStatusPending 表示订单已创建，等待支付确认。
	TokenOrderStatusPending = "pending"
	// TokenOrderStatusPaid 表示订单已确认支付，等待写入获取账本。
	TokenOrderStatusPaid = "paid"
	// TokenOrderStatusGranted 表示订单已经写入 token_grants 获取账本。
	TokenOrderStatusGranted = "granted"
	// TokenOrderStatusClosed 表示订单关闭，P0 暂不实现复杂关闭流程。
	TokenOrderStatusClosed = "closed"
)

const (
	// TokenGrantStatusRecorded 表示 Token 获取事实已记录在 token-store 内。
	TokenGrantStatusRecorded = "recorded"
	// TokenGrantStatusApplied 表示后续已同步到 user/auth 权威额度。
	TokenGrantStatusApplied = "applied"
	// TokenGrantStatusSkipped 表示命中奖励规则或幂等条件后跳过发放。
	TokenGrantStatusSkipped = "skipped"
	// TokenGrantStatusFailed 表示记录或后续同步失败，可按 event_id 重试。
	TokenGrantStatusFailed = "failed"
)

const (
	// TokenGrantSourcePurchase 表示购买 Token 商品产生的获取记录。
	TokenGrantSourcePurchase = "purchase"
	// TokenGrantSourceForumLike 表示计划被点赞产生的作者奖励。
	TokenGrantSourceForumLike = "forum_like"
	// TokenGrantSourceForumImport 表示计划被导入产生的作者奖励。
	TokenGrantSourceForumImport = "forum_import"
	// TokenGrantSourceManual 预留人工补偿来源，P0 不做管理后台。
	TokenGrantSourceManual = "manual"
)

const (
	// TokenRewardRuleStatusActive 表示奖励规则启用。
	TokenRewardRuleStatusActive = "active"
	// TokenRewardRuleStatusInactive 表示奖励规则停用。
	TokenRewardRuleStatusInactive = "inactive"
)

// TokenProduct 是 Token 商店商品表。
//
// 职责边界：
// 1. P0 从表读取商品，由 seed 初始化 2-3 个固定商品；
// 2. 不承载真实支付渠道配置，也不做商品管理后台；
// 3. 下单时会复制商品快照到订单，避免后续改价影响历史订单。
type TokenProduct struct {
	ID          uint64    `gorm:"column:id;primaryKey;autoIncrement"`
	SKU         string    `gorm:"column:sku;type:varchar(64);not null;uniqueIndex:uk_token_products_sku;comment:商品稳定编码"`
	Name        string    `gorm:"column:name;type:varchar(80);not null;comment:商品名称"`
	Description string    `gorm:"column:description;type:varchar(255);comment:商品描述"`
	TokenAmount int64     `gorm:"column:token_amount;not null;comment:商品包含Token数量"`
	PriceCent   int64     `gorm:"column:price_cent;not null;comment:价格，单位分"`
	Currency    string    `gorm:"column:currency;type:varchar(16);not null;default:'CNY';comment:币种"`
	Badge       string    `gorm:"column:badge;type:varchar(32);comment:前端角标"`
	Status      string    `gorm:"column:status;type:varchar(32);not null;default:'active';index:idx_token_products_status_sort,priority:1;comment:active/inactive"`
	SortOrder   int       `gorm:"column:sort_order;not null;default:0;index:idx_token_products_status_sort,priority:2;comment:展示排序"`
	CreatedAt   time.Time `gorm:"column:created_at;autoCreateTime;comment:创建时间"`
	UpdatedAt   time.Time `gorm:"column:updated_at;autoUpdateTime;comment:更新时间"`
}

func (TokenProduct) TableName() string {
	return "token_products"
}

// TokenOrder 是 Token 商品订单表。
//
// 职责边界：
// 1. 记录用户购买商品的订单状态机；
// 2. P0 只支持 mock paid，不接真实支付网关；
// 3. granted 只表示已写入 token-store 获取账本，不代表已同步到 user/auth 权威额度。
type TokenOrder struct {
	ID                  uint64     `gorm:"column:id;primaryKey;autoIncrement"`
	OrderNo             string     `gorm:"column:order_no;type:varchar(64);not null;uniqueIndex:uk_token_orders_order_no;comment:订单号"`
	UserID              uint64     `gorm:"column:user_id;not null;uniqueIndex:uk_token_orders_user_idem,priority:1;index:idx_token_orders_user_status_created,priority:1;comment:下单用户ID"`
	ProductID           uint64     `gorm:"column:product_id;not null;index:idx_token_orders_product;comment:商品ID"`
	ProductSKU          string     `gorm:"column:product_sku;type:varchar(64);not null;comment:商品SKU快照"`
	ProductName         string     `gorm:"column:product_name;type:varchar(80);not null;comment:商品名称快照"`
	ProductSnapshotJSON string     `gorm:"column:product_snapshot_json;type:json;not null;comment:商品完整快照JSON"`
	Quantity            int        `gorm:"column:quantity;not null;default:1;comment:购买数量"`
	TokenAmount         int64      `gorm:"column:token_amount;not null;comment:订单总Token数量"`
	AmountCent          int64      `gorm:"column:amount_cent;not null;comment:订单总金额，单位分"`
	Currency            string     `gorm:"column:currency;type:varchar(16);not null;default:'CNY';comment:币种"`
	Status              string     `gorm:"column:status;type:varchar(32);not null;default:'pending';index:idx_token_orders_user_status_created,priority:2;comment:pending/paid/granted/closed"`
	PaymentMode         string     `gorm:"column:payment_mode;type:varchar(32);not null;default:'mock';comment:支付模式，P0为mock"`
	IdempotencyKey      *string    `gorm:"column:idempotency_key;type:varchar(128);uniqueIndex:uk_token_orders_user_idem,priority:2;comment:创建订单幂等键"`
	PaidAt              *time.Time `gorm:"column:paid_at;comment:支付确认时间"`
	GrantedAt           *time.Time `gorm:"column:granted_at;comment:写入获取账本时间"`
	CreatedAt           time.Time  `gorm:"column:created_at;autoCreateTime;index:idx_token_orders_user_status_created,priority:3;comment:创建时间"`
	UpdatedAt           time.Time  `gorm:"column:updated_at;autoUpdateTime;comment:更新时间"`
}

func (TokenOrder) TableName() string {
	return "token_orders"
}

// TokenGrant 是 Token 获取账本表。
//
// 职责边界：
// 1. 记录购买、论坛点赞奖励、论坛导入奖励等 Token 获取事实；
// 2. event_id 是最终幂等边界，避免订单或 outbox 重试重复发放；
// 3. P0 不直接修改 users 表，quota_applied 默认为 false。
type TokenGrant struct {
	ID           uint64     `gorm:"column:id;primaryKey;autoIncrement"`
	EventID      string     `gorm:"column:event_id;type:varchar(128);not null;uniqueIndex:uk_token_grants_event;comment:幂等事件ID"`
	UserID       uint64     `gorm:"column:user_id;not null;index:idx_token_grants_user_source_created,priority:1;comment:获得Token的用户ID"`
	Source       string     `gorm:"column:source;type:varchar(32);not null;index:idx_token_grants_user_source_created,priority:2;comment:purchase/forum_like/forum_import/manual"`
	SourceLabel  string     `gorm:"column:source_label;type:varchar(64);comment:前端展示来源"`
	SourceRefID  *uint64    `gorm:"column:source_ref_id;index:idx_token_grants_source_ref;comment:来源业务ID，如order_id/post_id/import_id"`
	OrderID      *uint64    `gorm:"column:order_id;index:idx_token_grants_order;comment:购买订单ID，非购买来源为空"`
	Amount       int64      `gorm:"column:amount;not null;comment:获取Token数量"`
	Status       string     `gorm:"column:status;type:varchar(32);not null;default:'recorded';index:idx_token_grants_status;comment:recorded/applied/skipped/failed"`
	QuotaApplied bool       `gorm:"column:quota_applied;not null;default:false;comment:是否已同步到user/auth权威额度"`
	Description  string     `gorm:"column:description;type:varchar(255);comment:前端展示描述"`
	AppliedAt    *time.Time `gorm:"column:applied_at;comment:同步到权威额度时间"`
	LastError    *string    `gorm:"column:last_error;type:text;comment:后续同步失败原因"`
	CreatedAt    time.Time  `gorm:"column:created_at;autoCreateTime;index:idx_token_grants_user_source_created,priority:3;comment:创建时间"`
	UpdatedAt    time.Time  `gorm:"column:updated_at;autoUpdateTime;comment:更新时间"`
}

func (TokenGrant) TableName() string {
	return "token_grants"
}

// TokenRewardRule 是社区奖励规则表。
//
// 职责边界：
// 1. P0 可用 seed 初始化点赞、导入奖励额度；
// 2. 不提供管理后台，规则调整先通过配置或 seed 变更；
// 3. 规则命中后的最终发放仍以 token_grants.event_id 幂等为准。
type TokenRewardRule struct {
	ID         uint64    `gorm:"column:id;primaryKey;autoIncrement"`
	Source     string    `gorm:"column:source;type:varchar(32);not null;uniqueIndex:uk_token_reward_rules_source;comment:forum_like/forum_import"`
	Name       string    `gorm:"column:name;type:varchar(80);not null;comment:规则名称"`
	Amount     int64     `gorm:"column:amount;not null;comment:奖励Token数量"`
	Status     string    `gorm:"column:status;type:varchar(32);not null;default:'active';index:idx_token_reward_rules_status;comment:active/inactive"`
	ConfigJSON *string   `gorm:"column:config_json;type:json;comment:预留扩展配置"`
	CreatedAt  time.Time `gorm:"column:created_at;autoCreateTime;comment:创建时间"`
	UpdatedAt  time.Time `gorm:"column:updated_at;autoUpdateTime;comment:更新时间"`
}

func (TokenRewardRule) TableName() string {
	return "token_reward_rules"
}
