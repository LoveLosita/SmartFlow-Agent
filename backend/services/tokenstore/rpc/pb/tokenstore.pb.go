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
