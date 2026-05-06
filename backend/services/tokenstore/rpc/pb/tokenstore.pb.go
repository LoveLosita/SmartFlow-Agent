package pb

import proto "github.com/golang/protobuf/proto"

var _ = proto.Marshal

const _ = proto.ProtoPackageIsVersion3

type PageResponse struct {
	Page     int32 `protobuf:"varint,1,opt,name=page,proto3" json:"page,omitempty"`
	PageSize int32 `protobuf:"varint,2,opt,name=page_size,json=pageSize,proto3" json:"page_size,omitempty"`
	Total    int32 `protobuf:"varint,3,opt,name=total,proto3" json:"total,omitempty"`
	HasMore  bool  `protobuf:"varint,4,opt,name=has_more,json=hasMore,proto3" json:"has_more,omitempty"`
}

func (m *PageResponse) Reset()         { *m = PageResponse{} }
func (m *PageResponse) String() string { return proto.CompactTextString(m) }
func (*PageResponse) ProtoMessage()    {}

type TokenSummary struct {
	RecordedTokenTotal     int64  `protobuf:"varint,1,opt,name=recorded_token_total,json=recordedTokenTotal,proto3" json:"recorded_token_total,omitempty"`
	AppliedTokenTotal      int64  `protobuf:"varint,2,opt,name=applied_token_total,json=appliedTokenTotal,proto3" json:"applied_token_total,omitempty"`
	PendingApplyTokenTotal int64  `protobuf:"varint,3,opt,name=pending_apply_token_total,json=pendingApplyTokenTotal,proto3" json:"pending_apply_token_total,omitempty"`
	QuotaSyncStatus        string `protobuf:"bytes,4,opt,name=quota_sync_status,json=quotaSyncStatus,proto3" json:"quota_sync_status,omitempty"`
	Tip                    string `protobuf:"bytes,5,opt,name=tip,proto3" json:"tip,omitempty"`
}

func (m *TokenSummary) Reset()         { *m = TokenSummary{} }
func (m *TokenSummary) String() string { return proto.CompactTextString(m) }
func (*TokenSummary) ProtoMessage()    {}

type TokenProductView struct {
	ProductId   uint64 `protobuf:"varint,1,opt,name=product_id,json=productId,proto3" json:"product_id,omitempty"`
	Name        string `protobuf:"bytes,2,opt,name=name,proto3" json:"name,omitempty"`
	Description string `protobuf:"bytes,3,opt,name=description,proto3" json:"description,omitempty"`
	TokenAmount int64  `protobuf:"varint,4,opt,name=token_amount,json=tokenAmount,proto3" json:"token_amount,omitempty"`
	PriceCent   int64  `protobuf:"varint,5,opt,name=price_cent,json=priceCent,proto3" json:"price_cent,omitempty"`
	PriceText   string `protobuf:"bytes,6,opt,name=price_text,json=priceText,proto3" json:"price_text,omitempty"`
	Currency    string `protobuf:"bytes,7,opt,name=currency,proto3" json:"currency,omitempty"`
	Badge       string `protobuf:"bytes,8,opt,name=badge,proto3" json:"badge,omitempty"`
	Status      string `protobuf:"bytes,9,opt,name=status,proto3" json:"status,omitempty"`
	SortOrder   int32  `protobuf:"varint,10,opt,name=sort_order,json=sortOrder,proto3" json:"sort_order,omitempty"`
}

func (m *TokenProductView) Reset()         { *m = TokenProductView{} }
func (m *TokenProductView) String() string { return proto.CompactTextString(m) }
func (*TokenProductView) ProtoMessage()    {}

type TokenGrantView struct {
	GrantId      uint64 `protobuf:"varint,1,opt,name=grant_id,json=grantId,proto3" json:"grant_id,omitempty"`
	EventId      string `protobuf:"bytes,2,opt,name=event_id,json=eventId,proto3" json:"event_id,omitempty"`
	Source       string `protobuf:"bytes,3,opt,name=source,proto3" json:"source,omitempty"`
	SourceLabel  string `protobuf:"bytes,4,opt,name=source_label,json=sourceLabel,proto3" json:"source_label,omitempty"`
	Amount       int64  `protobuf:"varint,5,opt,name=amount,proto3" json:"amount,omitempty"`
	Status       string `protobuf:"bytes,6,opt,name=status,proto3" json:"status,omitempty"`
	QuotaApplied bool   `protobuf:"varint,7,opt,name=quota_applied,json=quotaApplied,proto3" json:"quota_applied,omitempty"`
	Description  string `protobuf:"bytes,8,opt,name=description,proto3" json:"description,omitempty"`
	CreatedAt    string `protobuf:"bytes,9,opt,name=created_at,json=createdAt,proto3" json:"created_at,omitempty"`
}

func (m *TokenGrantView) Reset()         { *m = TokenGrantView{} }
func (m *TokenGrantView) String() string { return proto.CompactTextString(m) }
func (*TokenGrantView) ProtoMessage()    {}

type TokenOrderView struct {
	OrderId         uint64          `protobuf:"varint,1,opt,name=order_id,json=orderId,proto3" json:"order_id,omitempty"`
	OrderNo         string          `protobuf:"bytes,2,opt,name=order_no,json=orderNo,proto3" json:"order_no,omitempty"`
	Status          string          `protobuf:"bytes,3,opt,name=status,proto3" json:"status,omitempty"`
	TokenAmount     int64           `protobuf:"varint,4,opt,name=token_amount,json=tokenAmount,proto3" json:"token_amount,omitempty"`
	AmountCent      int64           `protobuf:"varint,5,opt,name=amount_cent,json=amountCent,proto3" json:"amount_cent,omitempty"`
	PriceText       string          `protobuf:"bytes,6,opt,name=price_text,json=priceText,proto3" json:"price_text,omitempty"`
	Currency        string          `protobuf:"bytes,7,opt,name=currency,proto3" json:"currency,omitempty"`
	PaymentMode     string          `protobuf:"bytes,8,opt,name=payment_mode,json=paymentMode,proto3" json:"payment_mode,omitempty"`
	Grant           *TokenGrantView `protobuf:"bytes,9,opt,name=grant,proto3" json:"grant,omitempty"`
	CreatedAt       string          `protobuf:"bytes,10,opt,name=created_at,json=createdAt,proto3" json:"created_at,omitempty"`
	PaidAt          string          `protobuf:"bytes,11,opt,name=paid_at,json=paidAt,proto3" json:"paid_at,omitempty"`
	GrantedAt       string          `protobuf:"bytes,12,opt,name=granted_at,json=grantedAt,proto3" json:"granted_at,omitempty"`
	ProductSnapshot string          `protobuf:"bytes,13,opt,name=product_snapshot,json=productSnapshot,proto3" json:"product_snapshot,omitempty"`
	ProductName     string          `protobuf:"bytes,14,opt,name=product_name,json=productName,proto3" json:"product_name,omitempty"`
	Quantity        int32           `protobuf:"varint,15,opt,name=quantity,proto3" json:"quantity,omitempty"`
}

func (m *TokenOrderView) Reset()         { *m = TokenOrderView{} }
func (m *TokenOrderView) String() string { return proto.CompactTextString(m) }
func (*TokenOrderView) ProtoMessage()    {}

type GetTokenSummaryRequest struct {
	ActorUserId uint64 `protobuf:"varint,1,opt,name=actor_user_id,json=actorUserId,proto3" json:"actor_user_id,omitempty"`
}

func (m *GetTokenSummaryRequest) Reset()         { *m = GetTokenSummaryRequest{} }
func (m *GetTokenSummaryRequest) String() string { return proto.CompactTextString(m) }
func (*GetTokenSummaryRequest) ProtoMessage()    {}

type GetTokenSummaryResponse struct {
	Summary *TokenSummary `protobuf:"bytes,1,opt,name=summary,proto3" json:"summary,omitempty"`
}

func (m *GetTokenSummaryResponse) Reset()         { *m = GetTokenSummaryResponse{} }
func (m *GetTokenSummaryResponse) String() string { return proto.CompactTextString(m) }
func (*GetTokenSummaryResponse) ProtoMessage()    {}

type ListTokenProductsRequest struct {
	ActorUserId uint64 `protobuf:"varint,1,opt,name=actor_user_id,json=actorUserId,proto3" json:"actor_user_id,omitempty"`
}

func (m *ListTokenProductsRequest) Reset()         { *m = ListTokenProductsRequest{} }
func (m *ListTokenProductsRequest) String() string { return proto.CompactTextString(m) }
func (*ListTokenProductsRequest) ProtoMessage()    {}

type ListTokenProductsResponse struct {
	Items []*TokenProductView `protobuf:"bytes,1,rep,name=items,proto3" json:"items,omitempty"`
}

func (m *ListTokenProductsResponse) Reset()         { *m = ListTokenProductsResponse{} }
func (m *ListTokenProductsResponse) String() string { return proto.CompactTextString(m) }
func (*ListTokenProductsResponse) ProtoMessage()    {}

type CreateTokenOrderRequest struct {
	ActorUserId    uint64 `protobuf:"varint,1,opt,name=actor_user_id,json=actorUserId,proto3" json:"actor_user_id,omitempty"`
	ProductId      uint64 `protobuf:"varint,2,opt,name=product_id,json=productId,proto3" json:"product_id,omitempty"`
	Quantity       int32  `protobuf:"varint,3,opt,name=quantity,proto3" json:"quantity,omitempty"`
	IdempotencyKey string `protobuf:"bytes,4,opt,name=idempotency_key,json=idempotencyKey,proto3" json:"idempotency_key,omitempty"`
}

func (m *CreateTokenOrderRequest) Reset()         { *m = CreateTokenOrderRequest{} }
func (m *CreateTokenOrderRequest) String() string { return proto.CompactTextString(m) }
func (*CreateTokenOrderRequest) ProtoMessage()    {}

type CreateTokenOrderResponse struct {
	Order *TokenOrderView `protobuf:"bytes,1,opt,name=order,proto3" json:"order,omitempty"`
}

func (m *CreateTokenOrderResponse) Reset()         { *m = CreateTokenOrderResponse{} }
func (m *CreateTokenOrderResponse) String() string { return proto.CompactTextString(m) }
func (*CreateTokenOrderResponse) ProtoMessage()    {}

type ListTokenOrdersRequest struct {
	ActorUserId uint64 `protobuf:"varint,1,opt,name=actor_user_id,json=actorUserId,proto3" json:"actor_user_id,omitempty"`
	Page        int32  `protobuf:"varint,2,opt,name=page,proto3" json:"page,omitempty"`
	PageSize    int32  `protobuf:"varint,3,opt,name=page_size,json=pageSize,proto3" json:"page_size,omitempty"`
	Status      string `protobuf:"bytes,4,opt,name=status,proto3" json:"status,omitempty"`
}

func (m *ListTokenOrdersRequest) Reset()         { *m = ListTokenOrdersRequest{} }
func (m *ListTokenOrdersRequest) String() string { return proto.CompactTextString(m) }
func (*ListTokenOrdersRequest) ProtoMessage()    {}

type ListTokenOrdersResponse struct {
	Items []*TokenOrderView `protobuf:"bytes,1,rep,name=items,proto3" json:"items,omitempty"`
	Page  *PageResponse     `protobuf:"bytes,2,opt,name=page,proto3" json:"page,omitempty"`
}

func (m *ListTokenOrdersResponse) Reset()         { *m = ListTokenOrdersResponse{} }
func (m *ListTokenOrdersResponse) String() string { return proto.CompactTextString(m) }
func (*ListTokenOrdersResponse) ProtoMessage()    {}

type GetTokenOrderRequest struct {
	ActorUserId uint64 `protobuf:"varint,1,opt,name=actor_user_id,json=actorUserId,proto3" json:"actor_user_id,omitempty"`
	OrderId     uint64 `protobuf:"varint,2,opt,name=order_id,json=orderId,proto3" json:"order_id,omitempty"`
}

func (m *GetTokenOrderRequest) Reset()         { *m = GetTokenOrderRequest{} }
func (m *GetTokenOrderRequest) String() string { return proto.CompactTextString(m) }
func (*GetTokenOrderRequest) ProtoMessage()    {}

type GetTokenOrderResponse struct {
	Order *TokenOrderView `protobuf:"bytes,1,opt,name=order,proto3" json:"order,omitempty"`
}

func (m *GetTokenOrderResponse) Reset()         { *m = GetTokenOrderResponse{} }
func (m *GetTokenOrderResponse) String() string { return proto.CompactTextString(m) }
func (*GetTokenOrderResponse) ProtoMessage()    {}

type MockPaidOrderRequest struct {
	ActorUserId    uint64 `protobuf:"varint,1,opt,name=actor_user_id,json=actorUserId,proto3" json:"actor_user_id,omitempty"`
	OrderId        uint64 `protobuf:"varint,2,opt,name=order_id,json=orderId,proto3" json:"order_id,omitempty"`
	MockChannel    string `protobuf:"bytes,3,opt,name=mock_channel,json=mockChannel,proto3" json:"mock_channel,omitempty"`
	IdempotencyKey string `protobuf:"bytes,4,opt,name=idempotency_key,json=idempotencyKey,proto3" json:"idempotency_key,omitempty"`
}

func (m *MockPaidOrderRequest) Reset()         { *m = MockPaidOrderRequest{} }
func (m *MockPaidOrderRequest) String() string { return proto.CompactTextString(m) }
func (*MockPaidOrderRequest) ProtoMessage()    {}

type MockPaidOrderResponse struct {
	Order *TokenOrderView `protobuf:"bytes,1,opt,name=order,proto3" json:"order,omitempty"`
}

func (m *MockPaidOrderResponse) Reset()         { *m = MockPaidOrderResponse{} }
func (m *MockPaidOrderResponse) String() string { return proto.CompactTextString(m) }
func (*MockPaidOrderResponse) ProtoMessage()    {}

type ListTokenGrantsRequest struct {
	ActorUserId uint64 `protobuf:"varint,1,opt,name=actor_user_id,json=actorUserId,proto3" json:"actor_user_id,omitempty"`
	Page        int32  `protobuf:"varint,2,opt,name=page,proto3" json:"page,omitempty"`
	PageSize    int32  `protobuf:"varint,3,opt,name=page_size,json=pageSize,proto3" json:"page_size,omitempty"`
	Source      string `protobuf:"bytes,4,opt,name=source,proto3" json:"source,omitempty"`
}

func (m *ListTokenGrantsRequest) Reset()         { *m = ListTokenGrantsRequest{} }
func (m *ListTokenGrantsRequest) String() string { return proto.CompactTextString(m) }
func (*ListTokenGrantsRequest) ProtoMessage()    {}

type ListTokenGrantsResponse struct {
	Items []*TokenGrantView `protobuf:"bytes,1,rep,name=items,proto3" json:"items,omitempty"`
	Page  *PageResponse     `protobuf:"bytes,2,opt,name=page,proto3" json:"page,omitempty"`
}

func (m *ListTokenGrantsResponse) Reset()         { *m = ListTokenGrantsResponse{} }
func (m *ListTokenGrantsResponse) String() string { return proto.CompactTextString(m) }
func (*ListTokenGrantsResponse) ProtoMessage()    {}

type RecordForumRewardGrantRequest struct {
	EventId        string `protobuf:"bytes,1,opt,name=event_id,json=eventId,proto3" json:"event_id,omitempty"`
	ReceiverUserId uint64 `protobuf:"varint,2,opt,name=receiver_user_id,json=receiverUserId,proto3" json:"receiver_user_id,omitempty"`
	Source         string `protobuf:"bytes,3,opt,name=source,proto3" json:"source,omitempty"`
	SourceRefId    string `protobuf:"bytes,4,opt,name=source_ref_id,json=sourceRefId,proto3" json:"source_ref_id,omitempty"`
}

func (m *RecordForumRewardGrantRequest) Reset()         { *m = RecordForumRewardGrantRequest{} }
func (m *RecordForumRewardGrantRequest) String() string { return proto.CompactTextString(m) }
func (*RecordForumRewardGrantRequest) ProtoMessage()    {}

type RecordForumRewardGrantResponse struct {
	Grant *TokenGrantView `protobuf:"bytes,1,opt,name=grant,proto3" json:"grant,omitempty"`
}

func (m *RecordForumRewardGrantResponse) Reset()         { *m = RecordForumRewardGrantResponse{} }
func (m *RecordForumRewardGrantResponse) String() string { return proto.CompactTextString(m) }
func (*RecordForumRewardGrantResponse) ProtoMessage()    {}

type CreditBalanceSnapshotView struct {
	UserId         uint64 `protobuf:"varint,1,opt,name=user_id,json=userId,proto3" json:"user_id,omitempty"`
	Balance        int64  `protobuf:"varint,2,opt,name=balance,proto3" json:"balance,omitempty"`
	IsBlocked      bool   `protobuf:"varint,3,opt,name=is_blocked,json=isBlocked,proto3" json:"is_blocked,omitempty"`
	SnapshotSource string `protobuf:"bytes,4,opt,name=snapshot_source,json=snapshotSource,proto3" json:"snapshot_source,omitempty"`
	UpdatedAt      string `protobuf:"bytes,5,opt,name=updated_at,json=updatedAt,proto3" json:"updated_at,omitempty"`
	TotalRecharged int64  `protobuf:"varint,6,opt,name=total_recharged,json=totalRecharged,proto3" json:"total_recharged,omitempty"`
	TotalRewarded  int64  `protobuf:"varint,7,opt,name=total_rewarded,json=totalRewarded,proto3" json:"total_rewarded,omitempty"`
	TotalConsumed  int64  `protobuf:"varint,8,opt,name=total_consumed,json=totalConsumed,proto3" json:"total_consumed,omitempty"`
}

func (m *CreditBalanceSnapshotView) Reset()         { *m = CreditBalanceSnapshotView{} }
func (m *CreditBalanceSnapshotView) String() string { return proto.CompactTextString(m) }
func (*CreditBalanceSnapshotView) ProtoMessage()    {}

type CreditConsumptionDashboardView struct {
	Period         string `protobuf:"bytes,1,opt,name=period,proto3" json:"period,omitempty"`
	CreditConsumed int64  `protobuf:"varint,2,opt,name=credit_consumed,json=creditConsumed,proto3" json:"credit_consumed,omitempty"`
	TokenConsumed  int64  `protobuf:"varint,3,opt,name=token_consumed,json=tokenConsumed,proto3" json:"token_consumed,omitempty"`
}

func (m *CreditConsumptionDashboardView) Reset()         { *m = CreditConsumptionDashboardView{} }
func (m *CreditConsumptionDashboardView) String() string { return proto.CompactTextString(m) }
func (*CreditConsumptionDashboardView) ProtoMessage()    {}

type GetCreditConsumptionDashboardRequest struct {
	ActorUserId uint64 `protobuf:"varint,1,opt,name=actor_user_id,json=actorUserId,proto3" json:"actor_user_id,omitempty"`
	Period      string `protobuf:"bytes,2,opt,name=period,proto3" json:"period,omitempty"`
}

func (m *GetCreditConsumptionDashboardRequest) Reset()         { *m = GetCreditConsumptionDashboardRequest{} }
func (m *GetCreditConsumptionDashboardRequest) String() string { return proto.CompactTextString(m) }
func (*GetCreditConsumptionDashboardRequest) ProtoMessage()    {}

type GetCreditConsumptionDashboardResponse struct {
	Dashboard *CreditConsumptionDashboardView `protobuf:"bytes,1,opt,name=dashboard,proto3" json:"dashboard,omitempty"`
}

func (m *GetCreditConsumptionDashboardResponse) Reset()         { *m = GetCreditConsumptionDashboardResponse{} }
func (m *GetCreditConsumptionDashboardResponse) String() string { return proto.CompactTextString(m) }
func (*GetCreditConsumptionDashboardResponse) ProtoMessage()    {}

type CreditProductView struct {
	ProductId         uint64 `protobuf:"varint,1,opt,name=product_id,json=productId,proto3" json:"product_id,omitempty"`
	Name              string `protobuf:"bytes,2,opt,name=name,proto3" json:"name,omitempty"`
	Description       string `protobuf:"bytes,3,opt,name=description,proto3" json:"description,omitempty"`
	CreditAmount      int64  `protobuf:"varint,4,opt,name=credit_amount,json=creditAmount,proto3" json:"credit_amount,omitempty"`
	PriceCent         int64  `protobuf:"varint,5,opt,name=price_cent,json=priceCent,proto3" json:"price_cent,omitempty"`
	PriceText         string `protobuf:"bytes,6,opt,name=price_text,json=priceText,proto3" json:"price_text,omitempty"`
	Currency          string `protobuf:"bytes,7,opt,name=currency,proto3" json:"currency,omitempty"`
	Badge             string `protobuf:"bytes,8,opt,name=badge,proto3" json:"badge,omitempty"`
	Status            string `protobuf:"bytes,9,opt,name=status,proto3" json:"status,omitempty"`
	SortOrder         int32  `protobuf:"varint,10,opt,name=sort_order,json=sortOrder,proto3" json:"sort_order,omitempty"`
	OriginalPriceCent int64  `protobuf:"varint,11,opt,name=original_price_cent,json=originalPriceCent,proto3" json:"original_price_cent,omitempty"`
}

func (m *CreditProductView) Reset()         { *m = CreditProductView{} }
func (m *CreditProductView) String() string { return proto.CompactTextString(m) }
func (*CreditProductView) ProtoMessage()    {}

type CreditOrderView struct {
	OrderId         uint64 `protobuf:"varint,1,opt,name=order_id,json=orderId,proto3" json:"order_id,omitempty"`
	OrderNo         string `protobuf:"bytes,2,opt,name=order_no,json=orderNo,proto3" json:"order_no,omitempty"`
	Status          string `protobuf:"bytes,3,opt,name=status,proto3" json:"status,omitempty"`
	CreditAmount    int64  `protobuf:"varint,4,opt,name=credit_amount,json=creditAmount,proto3" json:"credit_amount,omitempty"`
	AmountCent      int64  `protobuf:"varint,5,opt,name=amount_cent,json=amountCent,proto3" json:"amount_cent,omitempty"`
	PriceText       string `protobuf:"bytes,6,opt,name=price_text,json=priceText,proto3" json:"price_text,omitempty"`
	Currency        string `protobuf:"bytes,7,opt,name=currency,proto3" json:"currency,omitempty"`
	PaymentMode     string `protobuf:"bytes,8,opt,name=payment_mode,json=paymentMode,proto3" json:"payment_mode,omitempty"`
	CreatedAt       string `protobuf:"bytes,9,opt,name=created_at,json=createdAt,proto3" json:"created_at,omitempty"`
	PaidAt          string `protobuf:"bytes,10,opt,name=paid_at,json=paidAt,proto3" json:"paid_at,omitempty"`
	CreditedAt      string `protobuf:"bytes,11,opt,name=credited_at,json=creditedAt,proto3" json:"credited_at,omitempty"`
	ProductSnapshot string `protobuf:"bytes,12,opt,name=product_snapshot,json=productSnapshot,proto3" json:"product_snapshot,omitempty"`
	ProductName     string `protobuf:"bytes,13,opt,name=product_name,json=productName,proto3" json:"product_name,omitempty"`
	Quantity        int32  `protobuf:"varint,14,opt,name=quantity,proto3" json:"quantity,omitempty"`
}

func (m *CreditOrderView) Reset()         { *m = CreditOrderView{} }
func (m *CreditOrderView) String() string { return proto.CompactTextString(m) }
func (*CreditOrderView) ProtoMessage()    {}

type CreditTransactionView struct {
	TransactionId uint64 `protobuf:"varint,1,opt,name=transaction_id,json=transactionId,proto3" json:"transaction_id,omitempty"`
	EventId       string `protobuf:"bytes,2,opt,name=event_id,json=eventId,proto3" json:"event_id,omitempty"`
	Source        string `protobuf:"bytes,3,opt,name=source,proto3" json:"source,omitempty"`
	SourceLabel   string `protobuf:"bytes,4,opt,name=source_label,json=sourceLabel,proto3" json:"source_label,omitempty"`
	Direction     string `protobuf:"bytes,5,opt,name=direction,proto3" json:"direction,omitempty"`
	Amount        int64  `protobuf:"varint,6,opt,name=amount,proto3" json:"amount,omitempty"`
	BalanceAfter  int64  `protobuf:"varint,7,opt,name=balance_after,json=balanceAfter,proto3" json:"balance_after,omitempty"`
	Status        string `protobuf:"bytes,8,opt,name=status,proto3" json:"status,omitempty"`
	Description   string `protobuf:"bytes,9,opt,name=description,proto3" json:"description,omitempty"`
	MetadataJson  string `protobuf:"bytes,10,opt,name=metadata_json,json=metadataJson,proto3" json:"metadata_json,omitempty"`
	CreatedAt     string `protobuf:"bytes,11,opt,name=created_at,json=createdAt,proto3" json:"created_at,omitempty"`
	OrderId       uint64 `protobuf:"varint,12,opt,name=order_id,json=orderId,proto3" json:"order_id,omitempty"`
}

func (m *CreditTransactionView) Reset()         { *m = CreditTransactionView{} }
func (m *CreditTransactionView) String() string { return proto.CompactTextString(m) }
func (*CreditTransactionView) ProtoMessage()    {}

type CreditPriceRuleView struct {
	RuleId                     uint64 `protobuf:"varint,1,opt,name=rule_id,json=ruleId,proto3" json:"rule_id,omitempty"`
	Scene                      string `protobuf:"bytes,2,opt,name=scene,proto3" json:"scene,omitempty"`
	ProviderName               string `protobuf:"bytes,3,opt,name=provider_name,json=providerName,proto3" json:"provider_name,omitempty"`
	ModelName                  string `protobuf:"bytes,4,opt,name=model_name,json=modelName,proto3" json:"model_name,omitempty"`
	InputPriceMicros           int64  `protobuf:"varint,5,opt,name=input_price_micros,json=inputPriceMicros,proto3" json:"input_price_micros,omitempty"`
	OutputPriceMicros          int64  `protobuf:"varint,6,opt,name=output_price_micros,json=outputPriceMicros,proto3" json:"output_price_micros,omitempty"`
	CachedPriceMicros          int64  `protobuf:"varint,7,opt,name=cached_price_micros,json=cachedPriceMicros,proto3" json:"cached_price_micros,omitempty"`
	ReasoningPriceMicros       int64  `protobuf:"varint,8,opt,name=reasoning_price_micros,json=reasoningPriceMicros,proto3" json:"reasoning_price_micros,omitempty"`
	CreditPerYuan              int64  `protobuf:"varint,9,opt,name=credit_per_yuan,json=creditPerYuan,proto3" json:"credit_per_yuan,omitempty"`
	Status                     string `protobuf:"bytes,10,opt,name=status,proto3" json:"status,omitempty"`
	Priority                   int32  `protobuf:"varint,11,opt,name=priority,proto3" json:"priority,omitempty"`
	Description                string `protobuf:"bytes,12,opt,name=description,proto3" json:"description,omitempty"`
	ProfitRateBps              int64  `protobuf:"varint,13,opt,name=profit_rate_bps,json=profitRateBps,proto3" json:"profit_rate_bps,omitempty"`
	ChargeInputPriceMicros     int64  `protobuf:"varint,14,opt,name=charge_input_price_micros,json=chargeInputPriceMicros,proto3" json:"charge_input_price_micros,omitempty"`
	ChargeOutputPriceMicros    int64  `protobuf:"varint,15,opt,name=charge_output_price_micros,json=chargeOutputPriceMicros,proto3" json:"charge_output_price_micros,omitempty"`
	ChargeCachedPriceMicros    int64  `protobuf:"varint,16,opt,name=charge_cached_price_micros,json=chargeCachedPriceMicros,proto3" json:"charge_cached_price_micros,omitempty"`
	ChargeReasoningPriceMicros int64  `protobuf:"varint,17,opt,name=charge_reasoning_price_micros,json=chargeReasoningPriceMicros,proto3" json:"charge_reasoning_price_micros,omitempty"`
}

func (m *CreditPriceRuleView) Reset()         { *m = CreditPriceRuleView{} }
func (m *CreditPriceRuleView) String() string { return proto.CompactTextString(m) }
func (*CreditPriceRuleView) ProtoMessage()    {}

type CreditRewardRuleView struct {
	RuleId      uint64 `protobuf:"varint,1,opt,name=rule_id,json=ruleId,proto3" json:"rule_id,omitempty"`
	Source      string `protobuf:"bytes,2,opt,name=source,proto3" json:"source,omitempty"`
	Name        string `protobuf:"bytes,3,opt,name=name,proto3" json:"name,omitempty"`
	Amount      int64  `protobuf:"varint,4,opt,name=amount,proto3" json:"amount,omitempty"`
	Status      string `protobuf:"bytes,5,opt,name=status,proto3" json:"status,omitempty"`
	Description string `protobuf:"bytes,6,opt,name=description,proto3" json:"description,omitempty"`
}

func (m *CreditRewardRuleView) Reset()         { *m = CreditRewardRuleView{} }
func (m *CreditRewardRuleView) String() string { return proto.CompactTextString(m) }
func (*CreditRewardRuleView) ProtoMessage()    {}

type GetCreditBalanceSnapshotRequest struct {
	UserId uint64 `protobuf:"varint,1,opt,name=user_id,json=userId,proto3" json:"user_id,omitempty"`
}

func (m *GetCreditBalanceSnapshotRequest) Reset()         { *m = GetCreditBalanceSnapshotRequest{} }
func (m *GetCreditBalanceSnapshotRequest) String() string { return proto.CompactTextString(m) }
func (*GetCreditBalanceSnapshotRequest) ProtoMessage()    {}

type GetCreditBalanceSnapshotResponse struct {
	Snapshot *CreditBalanceSnapshotView `protobuf:"bytes,1,opt,name=snapshot,proto3" json:"snapshot,omitempty"`
}

func (m *GetCreditBalanceSnapshotResponse) Reset()         { *m = GetCreditBalanceSnapshotResponse{} }
func (m *GetCreditBalanceSnapshotResponse) String() string { return proto.CompactTextString(m) }
func (*GetCreditBalanceSnapshotResponse) ProtoMessage()    {}

type ListCreditProductsRequest struct {
	ActorUserId uint64 `protobuf:"varint,1,opt,name=actor_user_id,json=actorUserId,proto3" json:"actor_user_id,omitempty"`
}

func (m *ListCreditProductsRequest) Reset()         { *m = ListCreditProductsRequest{} }
func (m *ListCreditProductsRequest) String() string { return proto.CompactTextString(m) }
func (*ListCreditProductsRequest) ProtoMessage()    {}

type ListCreditProductsResponse struct {
	Items []*CreditProductView `protobuf:"bytes,1,rep,name=items,proto3" json:"items,omitempty"`
}

func (m *ListCreditProductsResponse) Reset()         { *m = ListCreditProductsResponse{} }
func (m *ListCreditProductsResponse) String() string { return proto.CompactTextString(m) }
func (*ListCreditProductsResponse) ProtoMessage()    {}

type CreateCreditOrderRequest struct {
	ActorUserId    uint64 `protobuf:"varint,1,opt,name=actor_user_id,json=actorUserId,proto3" json:"actor_user_id,omitempty"`
	ProductId      uint64 `protobuf:"varint,2,opt,name=product_id,json=productId,proto3" json:"product_id,omitempty"`
	Quantity       int32  `protobuf:"varint,3,opt,name=quantity,proto3" json:"quantity,omitempty"`
	IdempotencyKey string `protobuf:"bytes,4,opt,name=idempotency_key,json=idempotencyKey,proto3" json:"idempotency_key,omitempty"`
}

func (m *CreateCreditOrderRequest) Reset()         { *m = CreateCreditOrderRequest{} }
func (m *CreateCreditOrderRequest) String() string { return proto.CompactTextString(m) }
func (*CreateCreditOrderRequest) ProtoMessage()    {}

type CreateCreditOrderResponse struct {
	Order *CreditOrderView `protobuf:"bytes,1,opt,name=order,proto3" json:"order,omitempty"`
}

func (m *CreateCreditOrderResponse) Reset()         { *m = CreateCreditOrderResponse{} }
func (m *CreateCreditOrderResponse) String() string { return proto.CompactTextString(m) }
func (*CreateCreditOrderResponse) ProtoMessage()    {}

type ListCreditOrdersRequest struct {
	ActorUserId uint64 `protobuf:"varint,1,opt,name=actor_user_id,json=actorUserId,proto3" json:"actor_user_id,omitempty"`
	Page        int32  `protobuf:"varint,2,opt,name=page,proto3" json:"page,omitempty"`
	PageSize    int32  `protobuf:"varint,3,opt,name=page_size,json=pageSize,proto3" json:"page_size,omitempty"`
	Status      string `protobuf:"bytes,4,opt,name=status,proto3" json:"status,omitempty"`
}

func (m *ListCreditOrdersRequest) Reset()         { *m = ListCreditOrdersRequest{} }
func (m *ListCreditOrdersRequest) String() string { return proto.CompactTextString(m) }
func (*ListCreditOrdersRequest) ProtoMessage()    {}

type ListCreditOrdersResponse struct {
	Items []*CreditOrderView `protobuf:"bytes,1,rep,name=items,proto3" json:"items,omitempty"`
	Page  *PageResponse      `protobuf:"bytes,2,opt,name=page,proto3" json:"page,omitempty"`
}

func (m *ListCreditOrdersResponse) Reset()         { *m = ListCreditOrdersResponse{} }
func (m *ListCreditOrdersResponse) String() string { return proto.CompactTextString(m) }
func (*ListCreditOrdersResponse) ProtoMessage()    {}

type GetCreditOrderRequest struct {
	ActorUserId uint64 `protobuf:"varint,1,opt,name=actor_user_id,json=actorUserId,proto3" json:"actor_user_id,omitempty"`
	OrderId     uint64 `protobuf:"varint,2,opt,name=order_id,json=orderId,proto3" json:"order_id,omitempty"`
}

func (m *GetCreditOrderRequest) Reset()         { *m = GetCreditOrderRequest{} }
func (m *GetCreditOrderRequest) String() string { return proto.CompactTextString(m) }
func (*GetCreditOrderRequest) ProtoMessage()    {}

type GetCreditOrderResponse struct {
	Order *CreditOrderView `protobuf:"bytes,1,opt,name=order,proto3" json:"order,omitempty"`
}

func (m *GetCreditOrderResponse) Reset()         { *m = GetCreditOrderResponse{} }
func (m *GetCreditOrderResponse) String() string { return proto.CompactTextString(m) }
func (*GetCreditOrderResponse) ProtoMessage()    {}

type MockPaidCreditOrderRequest struct {
	ActorUserId    uint64 `protobuf:"varint,1,opt,name=actor_user_id,json=actorUserId,proto3" json:"actor_user_id,omitempty"`
	OrderId        uint64 `protobuf:"varint,2,opt,name=order_id,json=orderId,proto3" json:"order_id,omitempty"`
	MockChannel    string `protobuf:"bytes,3,opt,name=mock_channel,json=mockChannel,proto3" json:"mock_channel,omitempty"`
	IdempotencyKey string `protobuf:"bytes,4,opt,name=idempotency_key,json=idempotencyKey,proto3" json:"idempotency_key,omitempty"`
}

func (m *MockPaidCreditOrderRequest) Reset()         { *m = MockPaidCreditOrderRequest{} }
func (m *MockPaidCreditOrderRequest) String() string { return proto.CompactTextString(m) }
func (*MockPaidCreditOrderRequest) ProtoMessage()    {}

type MockPaidCreditOrderResponse struct {
	Order *CreditOrderView `protobuf:"bytes,1,opt,name=order,proto3" json:"order,omitempty"`
}

func (m *MockPaidCreditOrderResponse) Reset()         { *m = MockPaidCreditOrderResponse{} }
func (m *MockPaidCreditOrderResponse) String() string { return proto.CompactTextString(m) }
func (*MockPaidCreditOrderResponse) ProtoMessage()    {}

type ListCreditTransactionsRequest struct {
	ActorUserId uint64 `protobuf:"varint,1,opt,name=actor_user_id,json=actorUserId,proto3" json:"actor_user_id,omitempty"`
	Page        int32  `protobuf:"varint,2,opt,name=page,proto3" json:"page,omitempty"`
	PageSize    int32  `protobuf:"varint,3,opt,name=page_size,json=pageSize,proto3" json:"page_size,omitempty"`
	Source      string `protobuf:"bytes,4,opt,name=source,proto3" json:"source,omitempty"`
	Direction   string `protobuf:"bytes,5,opt,name=direction,proto3" json:"direction,omitempty"`
}

func (m *ListCreditTransactionsRequest) Reset()         { *m = ListCreditTransactionsRequest{} }
func (m *ListCreditTransactionsRequest) String() string { return proto.CompactTextString(m) }
func (*ListCreditTransactionsRequest) ProtoMessage()    {}

type ListCreditTransactionsResponse struct {
	Items []*CreditTransactionView `protobuf:"bytes,1,rep,name=items,proto3" json:"items,omitempty"`
	Page  *PageResponse            `protobuf:"bytes,2,opt,name=page,proto3" json:"page,omitempty"`
}

func (m *ListCreditTransactionsResponse) Reset()         { *m = ListCreditTransactionsResponse{} }
func (m *ListCreditTransactionsResponse) String() string { return proto.CompactTextString(m) }
func (*ListCreditTransactionsResponse) ProtoMessage()    {}

type ListCreditPriceRulesRequest struct {
	Scene        string `protobuf:"bytes,1,opt,name=scene,proto3" json:"scene,omitempty"`
	ProviderName string `protobuf:"bytes,2,opt,name=provider_name,json=providerName,proto3" json:"provider_name,omitempty"`
	ModelName    string `protobuf:"bytes,3,opt,name=model_name,json=modelName,proto3" json:"model_name,omitempty"`
	Status       string `protobuf:"bytes,4,opt,name=status,proto3" json:"status,omitempty"`
}

func (m *ListCreditPriceRulesRequest) Reset()         { *m = ListCreditPriceRulesRequest{} }
func (m *ListCreditPriceRulesRequest) String() string { return proto.CompactTextString(m) }
func (*ListCreditPriceRulesRequest) ProtoMessage()    {}

type ListCreditPriceRulesResponse struct {
	Items []*CreditPriceRuleView `protobuf:"bytes,1,rep,name=items,proto3" json:"items,omitempty"`
}

func (m *ListCreditPriceRulesResponse) Reset()         { *m = ListCreditPriceRulesResponse{} }
func (m *ListCreditPriceRulesResponse) String() string { return proto.CompactTextString(m) }
func (*ListCreditPriceRulesResponse) ProtoMessage()    {}

type ListCreditRewardRulesRequest struct {
	Source string `protobuf:"bytes,1,opt,name=source,proto3" json:"source,omitempty"`
	Status string `protobuf:"bytes,2,opt,name=status,proto3" json:"status,omitempty"`
}

func (m *ListCreditRewardRulesRequest) Reset()         { *m = ListCreditRewardRulesRequest{} }
func (m *ListCreditRewardRulesRequest) String() string { return proto.CompactTextString(m) }
func (*ListCreditRewardRulesRequest) ProtoMessage()    {}

type ListCreditRewardRulesResponse struct {
	Items []*CreditRewardRuleView `protobuf:"bytes,1,rep,name=items,proto3" json:"items,omitempty"`
}

func (m *ListCreditRewardRulesResponse) Reset()         { *m = ListCreditRewardRulesResponse{} }
func (m *ListCreditRewardRulesResponse) String() string { return proto.CompactTextString(m) }
func (*ListCreditRewardRulesResponse) ProtoMessage()    {}
