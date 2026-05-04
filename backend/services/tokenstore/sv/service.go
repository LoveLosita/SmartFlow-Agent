package sv

import (
	"context"
	"errors"

	tokencontracts "github.com/LoveLosita/smartflow/backend/shared/contracts/tokenstore"
	"gorm.io/gorm"
)

// ErrNotImplemented 表示 RPC 骨架已接线，但对应业务用例还在后续步骤实现。
var ErrNotImplemented = errors.New("tokenstore service method not implemented")

// TokenGrantOutlet 是 token-store 后续切到 user/auth 权威额度的内部发放出口。
//
// 职责边界：
// 1. P0 只记录 token-store 自己的获取事实和账本；
// 2. 禁止直接修改 users 表；
// 3. 后续切 user/auth 时新增 adapter，服务编排层不重写。
type TokenGrantOutlet interface {
	RecordAcquisition(ctx context.Context, grant tokencontracts.TokenGrantRecord) error
}

// Options 是 token-store 服务的依赖注入参数。
type Options struct {
	DB          *gorm.DB
	GrantOutlet TokenGrantOutlet
}

// Service 承载 Token 商店服务内部业务编排。
//
// 职责边界：
// 1. 负责商品、订单、mock paid、grant 账本和奖励规则；
// 2. 不负责登录鉴权，也不直接修改 user/auth 权威额度；
// 3. 不负责真实第三方支付回调，P0 只处理 mock paid。
type Service struct {
	db          *gorm.DB
	grantOutlet TokenGrantOutlet
}

func New(opts Options) *Service {
	return &Service{
		db:          opts.DB,
		grantOutlet: opts.GrantOutlet,
	}
}

// Ready 用于第二步骨架阶段的依赖检查。
func (s *Service) Ready() error {
	if s == nil {
		return errors.New("tokenstore service is nil")
	}
	if s.db == nil {
		return errors.New("tokenstore db is nil")
	}
	return nil
}

// ListProducts 是商品列表用例占位，第四步实现真实查询。
func (s *Service) ListProducts(ctx context.Context, actorUserID uint64) ([]tokencontracts.TokenProductView, error) {
	_ = ctx
	_ = actorUserID
	return nil, ErrNotImplemented
}

// GetSummary 是 Token 概览用例占位，第四步实现 grant 账本聚合。
func (s *Service) GetSummary(ctx context.Context, actorUserID uint64) (*tokencontracts.TokenSummary, error) {
	_ = ctx
	_ = actorUserID
	return nil, ErrNotImplemented
}

// CreateOrder 是创建订单用例占位，第四步实现商品读取、订单幂等和金额快照。
func (s *Service) CreateOrder(ctx context.Context, req tokencontracts.CreateTokenOrderRequest) (*tokencontracts.TokenOrderView, error) {
	_ = ctx
	_ = req
	return nil, ErrNotImplemented
}

// ListOrders 是订单列表用例占位，第四步实现用户维度分页查询。
func (s *Service) ListOrders(ctx context.Context, req tokencontracts.ListTokenOrdersRequest) ([]tokencontracts.TokenOrderView, tokencontracts.PageResult, error) {
	_ = ctx
	_ = req
	return nil, tokencontracts.PageResult{}, ErrNotImplemented
}

// GetOrder 是订单详情用例占位，第四步实现订单归属校验。
func (s *Service) GetOrder(ctx context.Context, actorUserID uint64, orderID uint64) (*tokencontracts.TokenOrderView, error) {
	_ = ctx
	_ = actorUserID
	_ = orderID
	return nil, ErrNotImplemented
}

// MockPaidOrder 是 P0 mock paid 用例占位，第四步实现支付态流转和 grant 账本。
func (s *Service) MockPaidOrder(ctx context.Context, req tokencontracts.MockPaidOrderRequest) (*tokencontracts.TokenOrderView, error) {
	_ = ctx
	_ = req
	return nil, ErrNotImplemented
}

// ListGrants 是 Token 获取记录用例占位，第四步实现账本分页查询。
func (s *Service) ListGrants(ctx context.Context, req tokencontracts.ListTokenGrantsRequest) ([]tokencontracts.TokenGrantView, tokencontracts.PageResult, error) {
	_ = ctx
	_ = req
	return nil, tokencontracts.PageResult{}, ErrNotImplemented
}
