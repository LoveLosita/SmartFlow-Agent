package model

import "time"

const (
	// CreditProductStatusActive 表示商品可在 Credit 商店展示和购买。
	CreditProductStatusActive = "active"
	// CreditProductStatusInactive 表示商品已下架。
	CreditProductStatusInactive = "inactive"
)

const (
	// CreditOrderStatusPending 表示订单已创建，等待支付确认。
	CreditOrderStatusPending = "pending"
	// CreditOrderStatusPaid 表示订单已确认支付，等待入账。
	CreditOrderStatusPaid = "paid"
	// CreditOrderStatusCredited 表示订单对应的 Credit 已经写入账本。
	CreditOrderStatusCredited = "credited"
	// CreditOrderStatusClosed 表示订单已关闭。
	CreditOrderStatusClosed = "closed"
)

const (
	// CreditLedgerDirectionIncome 表示正向入账。
	CreditLedgerDirectionIncome = "income"
	// CreditLedgerDirectionExpense 表示扣费出账。
	CreditLedgerDirectionExpense = "expense"
)

const (
	// CreditLedgerStatusApplied 表示该笔流水已经成为权威账本事实。
	CreditLedgerStatusApplied = "applied"
	// CreditLedgerStatusSkipped 表示事件被消费但不影响余额。
	CreditLedgerStatusSkipped = "skipped"
	// CreditLedgerStatusFailed 预留给后续补偿或人工处理。
	CreditLedgerStatusFailed = "failed"
)

const (
	// CreditLedgerSourcePurchase 表示用户购买 Credit 商品。
	CreditLedgerSourcePurchase = "purchase"
	// CreditLedgerSourceCharge 表示 LLM 调用扣费。
	CreditLedgerSourceCharge = "charge"
	// CreditLedgerSourceForumLike 预留论坛点赞奖励。
	CreditLedgerSourceForumLike = "forum_like"
	// CreditLedgerSourceForumImport 预留论坛导入奖励。
	CreditLedgerSourceForumImport = "forum_import"
	// CreditLedgerSourceManual 预留人工补偿。
	CreditLedgerSourceManual = "manual"
)

const (
	// CreditPriceRuleStatusActive 表示价格规则启用。
	CreditPriceRuleStatusActive = "active"
	// CreditPriceRuleStatusInactive 表示价格规则停用。
	CreditPriceRuleStatusInactive = "inactive"
)

const (
	// CreditRewardRuleStatusActive 表示奖励规则启用。
	CreditRewardRuleStatusActive = "active"
	// CreditRewardRuleStatusInactive 表示奖励规则停用。
	CreditRewardRuleStatusInactive = "inactive"
)

// CreditAccount 是 Credit 权威余额表。
//
// 职责边界：
// 1. 只保存用户在 TokenStore 账本口径下的当前余额与累计统计；
// 2. balance 允许被异步结算扣到 0 以下，后续由 Guard 和充值链路阻断新增调用；
// 3. 不保存逐笔明细，逐笔事实统一以 credit_ledger 为准。
type CreditAccount struct {
	ID                uint64    `gorm:"column:id;primaryKey;autoIncrement"`
	UserID            uint64    `gorm:"column:user_id;not null;uniqueIndex:uk_credit_accounts_user;comment:用户ID"`
	Balance           int64     `gorm:"column:balance;not null;default:0;comment:当前Credit余额"`
	TotalRecharged    int64     `gorm:"column:total_recharged;not null;default:0;comment:累计购买入账"`
	TotalRewarded     int64     `gorm:"column:total_rewarded;not null;default:0;comment:累计奖励入账"`
	TotalConsumed     int64     `gorm:"column:total_consumed;not null;default:0;comment:累计扣费出账"`
	LastLedgerEventID string    `gorm:"column:last_ledger_event_id;type:varchar(128);comment:最近一次账本事件ID"`
	CreatedAt         time.Time `gorm:"column:created_at;autoCreateTime;comment:创建时间"`
	UpdatedAt         time.Time `gorm:"column:updated_at;autoUpdateTime;comment:更新时间"`
}

func (CreditAccount) TableName() string {
	return "credit_accounts"
}

// CreditLedger 是 Credit 权威流水表。
//
// 职责边界：
// 1. event_id 是最终幂等键，所有异步扣费、充值、奖励都依赖它去重；
// 2. amount 使用带符号值，正数表示入账，负数表示扣费，0 表示消费成功但不影响余额；
// 3. balance_before / balance_after 记录事件落账时的权威余额快照。
type CreditLedger struct {
	ID            uint64    `gorm:"column:id;primaryKey;autoIncrement"`
	EventID       string    `gorm:"column:event_id;type:varchar(128);not null;uniqueIndex:uk_credit_ledger_event;comment:最终幂等事件ID"`
	UserID        uint64    `gorm:"column:user_id;not null;index:idx_credit_ledger_user_created,priority:1;comment:用户ID"`
	Source        string    `gorm:"column:source;type:varchar(32);not null;index:idx_credit_ledger_user_created,priority:2;comment:purchase/charge/forum_like/forum_import/manual"`
	SourceLabel   string    `gorm:"column:source_label;type:varchar(64);comment:来源展示文案"`
	Direction     string    `gorm:"column:direction;type:varchar(16);not null;comment:income/expense"`
	OrderID       *uint64   `gorm:"column:order_id;index:idx_credit_ledger_order;comment:关联订单ID"`
	SourceRefID   *string   `gorm:"column:source_ref_id;type:varchar(128);index:idx_credit_ledger_source_ref;comment:来源业务ID"`
	Amount        int64     `gorm:"column:amount;not null;comment:本次Credit变动，正数入账负数扣费"`
	BalanceBefore int64     `gorm:"column:balance_before;not null;default:0;comment:落账前余额"`
	BalanceAfter  int64     `gorm:"column:balance_after;not null;default:0;comment:落账后余额"`
	Status        string    `gorm:"column:status;type:varchar(32);not null;default:'applied';index:idx_credit_ledger_status;comment:applied/skipped/failed"`
	Description   string    `gorm:"column:description;type:varchar(255);comment:展示描述"`
	MetadataJSON  string    `gorm:"column:metadata_json;type:json;comment:扩展元数据"`
	CreatedAt     time.Time `gorm:"column:created_at;autoCreateTime;index:idx_credit_ledger_user_created,priority:3;comment:创建时间"`
	UpdatedAt     time.Time `gorm:"column:updated_at;autoUpdateTime;comment:更新时间"`
}

func (CreditLedger) TableName() string {
	return "credit_ledger"
}

// CreditProduct 是 Credit 商店商品表。
type CreditProduct struct {
	ID                uint64    `gorm:"column:id;primaryKey;autoIncrement"`
	SKU               string    `gorm:"column:sku;type:varchar(64);not null;uniqueIndex:uk_credit_products_sku;comment:商品稳定编码"`
	Name              string    `gorm:"column:name;type:varchar(80);not null;comment:商品名称"`
	Description       string    `gorm:"column:description;type:varchar(255);comment:商品描述"`
	CreditAmount      int64     `gorm:"column:credit_amount;not null;comment:包含Credit数量"`
	PriceCent         int64     `gorm:"column:price_cent;not null;comment:价格，单位分"`
	OriginalPriceCent int64     `gorm:"column:original_price_cent;not null;default:0;comment:优惠前价格，单位分"`
	Currency          string    `gorm:"column:currency;type:varchar(16);not null;default:'CNY';comment:币种"`
	Badge             string    `gorm:"column:badge;type:varchar(32);comment:角标"`
	Status            string    `gorm:"column:status;type:varchar(32);not null;default:'active';index:idx_credit_products_status_sort,priority:1;comment:active/inactive"`
	SortOrder         int       `gorm:"column:sort_order;not null;default:0;index:idx_credit_products_status_sort,priority:2;comment:展示排序"`
	CreatedAt         time.Time `gorm:"column:created_at;autoCreateTime;comment:创建时间"`
	UpdatedAt         time.Time `gorm:"column:updated_at;autoUpdateTime;comment:更新时间"`
}

func (CreditProduct) TableName() string {
	return "credit_products"
}

// CreditOrder 是 Credit 商品订单表。
type CreditOrder struct {
	ID                  uint64     `gorm:"column:id;primaryKey;autoIncrement"`
	OrderNo             string     `gorm:"column:order_no;type:varchar(64);not null;uniqueIndex:uk_credit_orders_order_no;comment:订单号"`
	UserID              uint64     `gorm:"column:user_id;not null;uniqueIndex:uk_credit_orders_user_idem,priority:1;index:idx_credit_orders_user_status_created,priority:1;comment:下单用户ID"`
	ProductID           uint64     `gorm:"column:product_id;not null;index:idx_credit_orders_product;comment:商品ID"`
	ProductSKU          string     `gorm:"column:product_sku;type:varchar(64);not null;comment:商品SKU快照"`
	ProductName         string     `gorm:"column:product_name;type:varchar(80);not null;comment:商品名称快照"`
	ProductSnapshotJSON string     `gorm:"column:product_snapshot_json;type:json;not null;comment:商品完整快照JSON"`
	Quantity            int        `gorm:"column:quantity;not null;default:1;comment:购买数量"`
	CreditAmount        int64      `gorm:"column:credit_amount;not null;comment:订单总Credit数量"`
	AmountCent          int64      `gorm:"column:amount_cent;not null;comment:订单总金额，单位分"`
	Currency            string     `gorm:"column:currency;type:varchar(16);not null;default:'CNY';comment:币种"`
	Status              string     `gorm:"column:status;type:varchar(32);not null;default:'pending';index:idx_credit_orders_user_status_created,priority:2;comment:pending/paid/credited/closed"`
	PaymentMode         string     `gorm:"column:payment_mode;type:varchar(32);not null;default:'mock';comment:支付模式"`
	IdempotencyKey      *string    `gorm:"column:idempotency_key;type:varchar(128);uniqueIndex:uk_credit_orders_user_idem,priority:2;comment:创建订单幂等键"`
	PaidAt              *time.Time `gorm:"column:paid_at;comment:支付确认时间"`
	CreditedAt          *time.Time `gorm:"column:credited_at;comment:入账时间"`
	CreatedAt           time.Time  `gorm:"column:created_at;autoCreateTime;index:idx_credit_orders_user_status_created,priority:3;comment:创建时间"`
	UpdatedAt           time.Time  `gorm:"column:updated_at;autoUpdateTime;comment:更新时间"`
}

func (CreditOrder) TableName() string {
	return "credit_orders"
}

// CreditPriceRule 是 LLM Credit 计价规则表。
//
// 职责边界：
// 1. 该表表达“某个 provider/model 在某场景下如何换算人民币与 Credit”的运营配置；
// 2. 第二步先完成表结构与读取能力，具体由 LLM 服务如何引用放到后续切流阶段；
// 3. 当前结算事件已带出最终 rmb_cost_micros 与 credit_cost，因此消费侧不在这里二次计算。
type CreditPriceRule struct {
	ID                   uint64    `gorm:"column:id;primaryKey;autoIncrement"`
	Scene                string    `gorm:"column:scene;type:varchar(64);not null;index:idx_credit_price_rules_scene_status,priority:1;comment:计费场景"`
	ProviderName         string    `gorm:"column:provider_name;type:varchar(64);not null;comment:模型提供方"`
	ModelName            string    `gorm:"column:model_name;type:varchar(128);not null;comment:模型名称"`
	InputPriceMicros     int64     `gorm:"column:input_price_micros;not null;default:0;comment:输入Token单价，单位微人民币"`
	OutputPriceMicros    int64     `gorm:"column:output_price_micros;not null;default:0;comment:输出Token单价，单位微人民币"`
	CachedPriceMicros    int64     `gorm:"column:cached_price_micros;not null;default:0;comment:缓存Token单价，单位微人民币"`
	ReasoningPriceMicros int64     `gorm:"column:reasoning_price_micros;not null;default:0;comment:推理Token单价，单位微人民币"`
	CreditPerYuan        int64     `gorm:"column:credit_per_yuan;not null;default:0;comment:1元人民币换算多少Credit"`
	Status               string    `gorm:"column:status;type:varchar(32);not null;default:'inactive';index:idx_credit_price_rules_scene_status,priority:2;comment:active/inactive"`
	Priority             int       `gorm:"column:priority;not null;default:0;comment:匹配优先级，越大越优先"`
	Description          string    `gorm:"column:description;type:varchar(255);comment:规则说明"`
	CreatedAt            time.Time `gorm:"column:created_at;autoCreateTime;comment:创建时间"`
	UpdatedAt            time.Time `gorm:"column:updated_at;autoUpdateTime;comment:更新时间"`
}

func (CreditPriceRule) TableName() string {
	return "credit_price_rules"
}

// CreditRewardRule 是 Credit 奖励规则表。
type CreditRewardRule struct {
	ID          uint64    `gorm:"column:id;primaryKey;autoIncrement"`
	Source      string    `gorm:"column:source;type:varchar(32);not null;uniqueIndex:uk_credit_reward_rules_source;comment:奖励来源"`
	Name        string    `gorm:"column:name;type:varchar(80);not null;comment:规则名称"`
	Amount      int64     `gorm:"column:amount;not null;comment:奖励Credit数量"`
	Status      string    `gorm:"column:status;type:varchar(32);not null;default:'active';index:idx_credit_reward_rules_status;comment:active/inactive"`
	Description string    `gorm:"column:description;type:varchar(255);comment:规则描述"`
	ConfigJSON  *string   `gorm:"column:config_json;type:json;comment:扩展配置"`
	CreatedAt   time.Time `gorm:"column:created_at;autoCreateTime;comment:创建时间"`
	UpdatedAt   time.Time `gorm:"column:updated_at;autoUpdateTime;comment:更新时间"`
}

func (CreditRewardRule) TableName() string {
	return "credit_reward_rules"
}
