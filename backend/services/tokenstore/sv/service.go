package sv

import (
	"errors"

	tokenstoredao "github.com/LoveLosita/smartflow/backend/services/tokenstore/dao"
	"gorm.io/gorm"
)

var ErrNotImplemented = errors.New("tokenstore service method not implemented")

// Options 是 token-store 服务的依赖注入参数。
type Options struct {
	DB          *gorm.DB
	CreditCache *tokenstoredao.CreditCacheDAO
}

// Service 承载 token-store 内部业务编排。
//
// 职责边界：
// 1. 同时承载旧 Token 商店与新 Credit 权威账本两套能力，服务进程先并行存在；
// 2. Token 与 Credit 分别走各自 DAO，不在服务层混写数据表访问；
// 3. 真正的跨服务 HTTP/gateway 接线留给后续第三步，本层只暴露 RPC 可用能力。
type Service struct {
	db          *gorm.DB
	creditDAO   *tokenstoredao.CreditStoreDAO
	creditCache *tokenstoredao.CreditCacheDAO
}

func New(opts Options) *Service {
	return &Service{
		db:          opts.DB,
		creditDAO:   tokenstoredao.NewCreditStoreDAO(opts.DB),
		creditCache: opts.CreditCache,
	}
}

// Ready 用于服务依赖检查。
func (s *Service) Ready() error {
	if s == nil {
		return errors.New("tokenstore service is nil")
	}
	if s.db == nil {
		return errors.New("tokenstore db is nil")
	}
	return nil
}
