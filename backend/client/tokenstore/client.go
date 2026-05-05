package tokenstore

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/LoveLosita/smartflow/backend/services/tokenstore/rpc/pb"
	tokencontracts "github.com/LoveLosita/smartflow/backend/shared/contracts/tokenstore"
	"github.com/zeromicro/go-zero/zrpc"
)

const (
	defaultEndpoint = "127.0.0.1:9095"
	defaultTimeout  = 2 * time.Second
)

type ClientConfig struct {
	Endpoints []string
	Target    string
	Timeout   time.Duration
}

// ProductSnapshot 是订单详情里内嵌的商品快照。
//
// 职责边界：
// 1. 只承载 HTTP gateway 当前需要透出的商品摘要；
// 2. 不补充 description、price 等商品列表字段，避免把详情快照扩成第二份商品实体；
// 3. 若下游 proto/contract 还未合入对应字段，这里允许保持 nil/零值兜底。
type ProductSnapshot struct {
	ProductID   uint64 `json:"product_id"`
	Name        string `json:"name"`
	TokenAmount int64  `json:"token_amount"`
}

// OrderView 是 gateway 侧订单展示结构。
//
// 职责边界：
// 1. 复用 token-store contract 里已稳定的订单字段；
// 2. 为前端 P0 额外承载 product_snapshot / product_name / quantity 三个 HTTP 所需字段；
// 3. 不反向影响 shared/contracts，等并行 worker 合入正式字段后可再收敛。
type OrderView struct {
	OrderID         uint64                         `json:"order_id"`
	OrderNo         string                         `json:"order_no"`
	Status          string                         `json:"status"`
	ProductSnapshot *ProductSnapshot               `json:"product_snapshot,omitempty"`
	ProductName     string                         `json:"product_name,omitempty"`
	Quantity        int                            `json:"quantity"`
	TokenAmount     int64                          `json:"token_amount"`
	AmountCent      int64                          `json:"amount_cent"`
	PriceText       string                         `json:"price_text"`
	Currency        string                         `json:"currency"`
	PaymentMode     string                         `json:"payment_mode"`
	Grant           *tokencontracts.TokenGrantView `json:"grant"`
	CreatedAt       string                         `json:"created_at"`
	PaidAt          *string                        `json:"paid_at"`
	GrantedAt       *string                        `json:"granted_at"`
}

// Client 是 gateway 侧访问 token-store zrpc 的适配层。
//
// 职责边界：
// 1. 只负责 HTTP gateway 与 token-store zrpc 之间的协议转译；
// 2. 不直连 token_* 表，也不承载订单/支付业务规则；
// 3. gRPC 业务错误会在这里反解回 respond.Response，便于 HTTP 层统一返回。
type Client struct {
	rpc pb.TokenStoreServiceClient
}

func NewClient(cfg ClientConfig) (*Client, error) {
	timeout := cfg.Timeout
	if timeout <= 0 {
		timeout = defaultTimeout
	}
	endpoints := normalizeEndpoints(cfg.Endpoints)
	target := strings.TrimSpace(cfg.Target)
	if len(endpoints) == 0 && target == "" {
		endpoints = []string{defaultEndpoint}
	}

	zclient, err := zrpc.NewClient(zrpc.RpcClientConf{
		Endpoints: endpoints,
		Target:    target,
		NonBlock:  true,
		Timeout:   int64(timeout / time.Millisecond),
	})
	if err != nil {
		return nil, err
	}
	return &Client{rpc: pb.NewTokenStoreServiceClient(zclient.Conn())}, nil
}

func (c *Client) GetSummary(ctx context.Context, actorUserID uint64) (*tokencontracts.TokenSummary, error) {
	if err := c.ensureReady(); err != nil {
		return nil, err
	}
	resp, err := c.rpc.GetSummary(ctx, &pb.GetTokenSummaryRequest{ActorUserId: actorUserID})
	if err != nil {
		return nil, responseFromRPCError(err)
	}
	if resp == nil {
		return nil, errors.New("tokenstore zrpc service returned empty get summary response")
	}
	summary := tokenSummaryFromPB(resp.Summary)
	return &summary, nil
}

func (c *Client) ListProducts(ctx context.Context, actorUserID uint64) ([]tokencontracts.TokenProductView, error) {
	if err := c.ensureReady(); err != nil {
		return nil, err
	}
	resp, err := c.rpc.ListProducts(ctx, &pb.ListTokenProductsRequest{ActorUserId: actorUserID})
	if err != nil {
		return nil, responseFromRPCError(err)
	}
	if resp == nil {
		return nil, errors.New("tokenstore zrpc service returned empty list products response")
	}
	return tokenProductsFromPB(resp.Items), nil
}

func (c *Client) CreateOrder(ctx context.Context, req tokencontracts.CreateTokenOrderRequest) (*OrderView, error) {
	if err := c.ensureReady(); err != nil {
		return nil, err
	}
	resp, err := c.rpc.CreateOrder(ctx, &pb.CreateTokenOrderRequest{
		ActorUserId:    req.ActorUserID,
		ProductId:      req.ProductID,
		Quantity:       int32(req.Quantity),
		IdempotencyKey: req.IdempotencyKey,
	})
	if err != nil {
		return nil, responseFromRPCError(err)
	}
	if resp == nil {
		return nil, errors.New("tokenstore zrpc service returned empty create order response")
	}
	order := tokenOrderFromPB(resp.Order)
	return &order, nil
}

func (c *Client) ListOrders(ctx context.Context, req tokencontracts.ListTokenOrdersRequest) ([]OrderView, tokencontracts.PageResult, error) {
	if err := c.ensureReady(); err != nil {
		return nil, tokencontracts.PageResult{}, err
	}
	resp, err := c.rpc.ListOrders(ctx, &pb.ListTokenOrdersRequest{
		ActorUserId: req.ActorUserID,
		Page:        int32(req.Page),
		PageSize:    int32(req.PageSize),
		Status:      req.Status,
	})
	if err != nil {
		return nil, tokencontracts.PageResult{}, responseFromRPCError(err)
	}
	if resp == nil {
		return nil, tokencontracts.PageResult{}, errors.New("tokenstore zrpc service returned empty list orders response")
	}
	return tokenOrdersFromPB(resp.Items), pageFromPB(resp.Page), nil
}

func (c *Client) GetOrder(ctx context.Context, actorUserID uint64, orderID uint64) (*OrderView, error) {
	if err := c.ensureReady(); err != nil {
		return nil, err
	}
	resp, err := c.rpc.GetOrder(ctx, &pb.GetTokenOrderRequest{
		ActorUserId: actorUserID,
		OrderId:     orderID,
	})
	if err != nil {
		return nil, responseFromRPCError(err)
	}
	if resp == nil {
		return nil, errors.New("tokenstore zrpc service returned empty get order response")
	}
	order := tokenOrderFromPB(resp.Order)
	return &order, nil
}

func (c *Client) MockPaidOrder(ctx context.Context, req tokencontracts.MockPaidOrderRequest) (*OrderView, error) {
	if err := c.ensureReady(); err != nil {
		return nil, err
	}
	resp, err := c.rpc.MockPaidOrder(ctx, &pb.MockPaidOrderRequest{
		ActorUserId:    req.ActorUserID,
		OrderId:        req.OrderID,
		MockChannel:    req.MockChannel,
		IdempotencyKey: req.IdempotencyKey,
	})
	if err != nil {
		return nil, responseFromRPCError(err)
	}
	if resp == nil {
		return nil, errors.New("tokenstore zrpc service returned empty mock paid response")
	}
	order := tokenOrderFromPB(resp.Order)
	return &order, nil
}

func (c *Client) ListGrants(ctx context.Context, req tokencontracts.ListTokenGrantsRequest) ([]tokencontracts.TokenGrantView, tokencontracts.PageResult, error) {
	if err := c.ensureReady(); err != nil {
		return nil, tokencontracts.PageResult{}, err
	}
	resp, err := c.rpc.ListGrants(ctx, &pb.ListTokenGrantsRequest{
		ActorUserId: req.ActorUserID,
		Page:        int32(req.Page),
		PageSize:    int32(req.PageSize),
		Source:      req.Source,
	})
	if err != nil {
		return nil, tokencontracts.PageResult{}, responseFromRPCError(err)
	}
	if resp == nil {
		return nil, tokencontracts.PageResult{}, errors.New("tokenstore zrpc service returned empty list grants response")
	}
	return tokenGrantsFromPB(resp.Items), pageFromPB(resp.Page), nil
}

func (c *Client) RecordForumRewardGrant(ctx context.Context, req tokencontracts.RecordForumRewardGrantRequest) (*tokencontracts.TokenGrantView, error) {
	if err := c.ensureReady(); err != nil {
		return nil, err
	}
	resp, err := c.rpc.RecordForumRewardGrant(ctx, &pb.RecordForumRewardGrantRequest{
		EventId:        req.EventID,
		ReceiverUserId: req.ReceiverUserID,
		Source:         req.Source,
		SourceRefId:    req.SourceRefID,
	})
	if err != nil {
		return nil, responseFromRPCError(err)
	}
	if resp == nil {
		return nil, errors.New("tokenstore zrpc service returned empty record forum reward grant response")
	}
	return tokenGrantFromPB(resp.Grant), nil
}

func (c *Client) ensureReady() error {
	if c == nil || c.rpc == nil {
		return errors.New("tokenstore zrpc client is not initialized")
	}
	return nil
}

func normalizeEndpoints(values []string) []string {
	endpoints := make([]string, 0, len(values))
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed != "" {
			endpoints = append(endpoints, trimmed)
		}
	}
	return endpoints
}

func pageFromPB(page *pb.PageResponse) tokencontracts.PageResult {
	if page == nil {
		return tokencontracts.PageResult{}
	}
	return tokencontracts.PageResult{
		Page:     int(page.Page),
		PageSize: int(page.PageSize),
		Total:    int(page.Total),
		HasMore:  page.HasMore,
	}
}

func tokenSummaryFromPB(summary *pb.TokenSummary) tokencontracts.TokenSummary {
	if summary == nil {
		return tokencontracts.TokenSummary{}
	}
	return tokencontracts.TokenSummary{
		RecordedTokenTotal:     summary.RecordedTokenTotal,
		AppliedTokenTotal:      summary.AppliedTokenTotal,
		PendingApplyTokenTotal: summary.PendingApplyTokenTotal,
		QuotaSyncStatus:        summary.QuotaSyncStatus,
		Tip:                    summary.Tip,
	}
}

func tokenProductFromPB(product *pb.TokenProductView) tokencontracts.TokenProductView {
	if product == nil {
		return tokencontracts.TokenProductView{}
	}
	return tokencontracts.TokenProductView{
		ProductID:   product.ProductId,
		Name:        product.Name,
		Description: product.Description,
		TokenAmount: product.TokenAmount,
		PriceCent:   product.PriceCent,
		PriceText:   product.PriceText,
		Currency:    product.Currency,
		Badge:       product.Badge,
		Status:      product.Status,
		SortOrder:   int(product.SortOrder),
	}
}

func tokenProductsFromPB(items []*pb.TokenProductView) []tokencontracts.TokenProductView {
	if len(items) == 0 {
		return []tokencontracts.TokenProductView{}
	}
	result := make([]tokencontracts.TokenProductView, 0, len(items))
	for _, item := range items {
		result = append(result, tokenProductFromPB(item))
	}
	return result
}

func tokenGrantFromPB(grant *pb.TokenGrantView) *tokencontracts.TokenGrantView {
	if grant == nil {
		return nil
	}
	return &tokencontracts.TokenGrantView{
		GrantID:      grant.GrantId,
		EventID:      grant.EventId,
		Source:       grant.Source,
		SourceLabel:  grant.SourceLabel,
		Amount:       grant.Amount,
		Status:       grant.Status,
		QuotaApplied: grant.QuotaApplied,
		Description:  grant.Description,
		CreatedAt:    grant.CreatedAt,
	}
}

func tokenGrantsFromPB(items []*pb.TokenGrantView) []tokencontracts.TokenGrantView {
	if len(items) == 0 {
		return []tokencontracts.TokenGrantView{}
	}
	result := make([]tokencontracts.TokenGrantView, 0, len(items))
	for _, item := range items {
		if grant := tokenGrantFromPB(item); grant != nil {
			result = append(result, *grant)
		}
	}
	return result
}

func tokenOrderFromPB(order *pb.TokenOrderView) OrderView {
	if order == nil {
		return OrderView{}
	}
	productSnapshot := tokenProductSnapshotFromJSON(order.ProductSnapshot)
	productName := strings.TrimSpace(order.ProductName)
	if productName == "" && productSnapshot != nil {
		productName = productSnapshot.Name
	}
	return OrderView{
		OrderID:         order.OrderId,
		OrderNo:         order.OrderNo,
		Status:          order.Status,
		ProductSnapshot: productSnapshot,
		ProductName:     productName,
		Quantity:        int(order.Quantity),
		TokenAmount:     order.TokenAmount,
		AmountCent:      order.AmountCent,
		PriceText:       order.PriceText,
		Currency:        order.Currency,
		PaymentMode:     order.PaymentMode,
		Grant:           tokenGrantFromPB(order.Grant),
		CreatedAt:       order.CreatedAt,
		PaidAt:          stringPtrFromNonEmpty(order.PaidAt),
		GrantedAt:       stringPtrFromNonEmpty(order.GrantedAt),
	}
}

func tokenOrdersFromPB(items []*pb.TokenOrderView) []OrderView {
	if len(items) == 0 {
		return []OrderView{}
	}
	result := make([]OrderView, 0, len(items))
	for _, item := range items {
		result = append(result, tokenOrderFromPB(item))
	}
	return result
}

// tokenProductSnapshotFromJSON 负责把 RPC 内部快照字符串转成 HTTP 展示对象。
//
// 职责边界：
// 1. 只解析 product_id / name / token_amount 三个前端需要的字段；
// 2. 不把解析失败暴露成接口错误，避免历史脏快照影响订单主流程展示；
// 3. 不反查商品表，订单详情必须以当时下单快照为准。
func tokenProductSnapshotFromJSON(raw string) *ProductSnapshot {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return nil
	}
	var snapshot ProductSnapshot
	if err := json.Unmarshal([]byte(trimmed), &snapshot); err != nil {
		return nil
	}
	if snapshot.ProductID == 0 && snapshot.Name == "" && snapshot.TokenAmount == 0 {
		return nil
	}
	return &snapshot
}

func stringPtrFromNonEmpty(value string) *string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return nil
	}
	return &trimmed
}
