package scheduleplan

import (
	"context"
	"errors"
	"strconv"

	"github.com/LoveLosita/smartflow/backend/model"
)

// SchedulePlanToolDeps 描述"智能排程工具包"需要的外部依赖。
//
// 设计目标：
// 1) 通过函数注入把 agent 包与 service/dao 解耦，避免循环依赖；
// 2) 每个函数对应一个可独立 mock 的业务能力；
// 3) 后续可按需扩展（如局部修补、任务类自动生成等）。
type SchedulePlanToolDeps struct {
	// SmartPlanningRaw 调用粗排算法，同时返回展示结构和已分配的任务项。
	// 返回值：
	// - []UserWeekSchedule：展示型结构，供 SSE 阶段推送给前端预览；
	// - []TaskClassItem：已分配的任务项（EmbeddedTime 已回填），供 materialize 直接转换。
	SmartPlanningRaw func(ctx context.Context, userID, taskClassID int) ([]model.UserWeekSchedule, []model.TaskClassItem, error)

	// BatchApplyPlans 将排程方案批量落库。
	// 输入：taskClassID、userID、落库请求体。
	// 输出：error（nil 表示全部成功）。
	BatchApplyPlans func(ctx context.Context, taskClassID, userID int, plans *model.UserInsertTaskClassItemToScheduleRequestBatch) error

	// GetTaskClassByID 获取任务类详情（含关联的 Items）。
	// 用于：
	// 1) 校验 task_class_id 合法性；
	// 2) 获取 Items 列表，为连续对话微调提供上下文。
	GetTaskClassByID func(ctx context.Context, taskClassID, userID int) (*model.TaskClass, error)

	// HybridScheduleWithPlan 构建混合日程（既有日程 + 粗排建议），供 ReAct 精排使用。
	// 可选依赖：未注入时 ReAct 精排阶段不可用，走原有 materialize 路径。
	HybridScheduleWithPlan func(ctx context.Context, userID, taskClassID int) ([]model.HybridScheduleEntry, []model.TaskClassItem, error)
}

// validate 校验依赖完整性，缺失任意一个都无法完成排程链路。
func (d SchedulePlanToolDeps) validate() error {
	if d.SmartPlanningRaw == nil {
		return errors.New("schedule plan tool deps: SmartPlanningRaw is nil")
	}
	if d.BatchApplyPlans == nil {
		return errors.New("schedule plan tool deps: BatchApplyPlans is nil")
	}
	if d.GetTaskClassByID == nil {
		return errors.New("schedule plan tool deps: GetTaskClassByID is nil")
	}
	return nil
}

// ExtraInt 从 extra map 中安全提取整数值。
//
// 兼容策略：
// 1) JSON 数字默认解析为 float64，做 int 转换；
// 2) 兼容字符串形式（如 "42"），用 Atoi 解析；
// 3) 其余类型返回 false，由调用方决定后续处理。
func ExtraInt(extra map[string]any, key string) (int, bool) {
	v, ok := extra[key]
	if !ok {
		return 0, false
	}
	switch n := v.(type) {
	case float64:
		return int(n), true
	case int:
		return n, true
	case string:
		i, err := strconv.Atoi(n)
		return i, err == nil
	default:
		return 0, false
	}
}
