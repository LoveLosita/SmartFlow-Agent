package sv

import (
	"context"
	"errors"

	tokenstoredao "github.com/LoveLosita/smartflow/backend/services/tokenstore/dao"
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
	tokenDAO    *tokenstoredao.TokenStoreDAO
	grantOutlet TokenGrantOutlet
}

func New(opts Options) *Service {
	return &Service{
		db:          opts.DB,
		tokenDAO:    tokenstoredao.NewTokenStoreDAO(opts.DB),
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
