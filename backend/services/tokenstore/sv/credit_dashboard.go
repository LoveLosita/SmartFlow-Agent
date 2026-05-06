package sv

import (
	"context"
	"strings"
	"time"

	tokenstoredao "github.com/LoveLosita/smartflow/backend/services/tokenstore/dao"
	creditcontracts "github.com/LoveLosita/smartflow/backend/shared/contracts/creditstore"
	"github.com/LoveLosita/smartflow/backend/shared/respond"
)

// GetCreditConsumptionDashboard 返回当前用户的 Credit 消耗看板。
//
// 职责边界：
// 1. 负责把前端周期参数归一化为 tokenstore 的统一时间窗口。
// 2. 只校验当前用户语义和周期合法性，真正的聚合查询下沉到 DAO。
// 3. 返回值只包含前端顶部看板需要的两个指标，不夹带商品、流水等其它信息。
func (s *Service) GetCreditConsumptionDashboard(ctx context.Context, req creditcontracts.GetCreditConsumptionDashboardRequest) (*creditcontracts.CreditConsumptionDashboardView, error) {
	if err := s.Ready(); err != nil {
		return nil, err
	}
	if req.ActorUserID == 0 {
		return nil, respond.MissingParam
	}

	period, err := normalizeCreditConsumptionPeriod(req.Period)
	if err != nil {
		return nil, err
	}

	query := tokenstoredao.GetCreditConsumptionDashboardQuery{
		UserID:      req.ActorUserID,
		CreatedFrom: resolveCreditConsumptionWindowStart(period, time.Now()),
	}
	aggregate, err := s.creditDAO.GetCreditConsumptionDashboard(ctx, query)
	if err != nil {
		return nil, err
	}

	return &creditcontracts.CreditConsumptionDashboardView{
		Period:         period,
		CreditConsumed: aggregate.CreditConsumed,
		TokenConsumed:  aggregate.TokenConsumed,
	}, nil
}

// normalizeCreditConsumptionPeriod 只负责把前端周期值收敛到固定枚举。
//
// 1. 空值默认回落到 24h，保证首页初次进入时可直接展示。
// 2. 非法值直接返回业务坏参，避免网关和前端各自维护一份不一致的枚举。
// 3. 这里不做时间计算，方便后续单独复用和测试。
func normalizeCreditConsumptionPeriod(raw string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "", creditcontracts.CreditConsumptionPeriod24h:
		return creditcontracts.CreditConsumptionPeriod24h, nil
	case creditcontracts.CreditConsumptionPeriod7d:
		return creditcontracts.CreditConsumptionPeriod7d, nil
	case creditcontracts.CreditConsumptionPeriod30d:
		return creditcontracts.CreditConsumptionPeriod30d, nil
	case creditcontracts.CreditConsumptionPeriodAll:
		return creditcontracts.CreditConsumptionPeriodAll, nil
	default:
		return "", tokenStoreBadRequest("period 仅支持 24h、7d、30d 或 all")
	}
}

// resolveCreditConsumptionWindowStart 负责把固定周期映射为统计起点。
//
// 1. only "all" 返回 nil，表示不加 created_at 过滤。
// 2. 其它周期统一按当前时间回退固定时长，保证前后端口径一致。
// 3. 这里不处理时区格式化，因为最终查询直接使用 time.Time 传给 DAO。
func resolveCreditConsumptionWindowStart(period string, now time.Time) *time.Time {
	var startAt time.Time
	switch period {
	case creditcontracts.CreditConsumptionPeriod24h:
		startAt = now.Add(-24 * time.Hour)
	case creditcontracts.CreditConsumptionPeriod7d:
		startAt = now.Add(-7 * 24 * time.Hour)
	case creditcontracts.CreditConsumptionPeriod30d:
		startAt = now.Add(-30 * 24 * time.Hour)
	default:
		return nil
	}
	return &startAt
}
