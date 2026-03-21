package scheduleplan

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/LoveLosita/smartflow/backend/model"
)

// SchedulePlanToolDeps 描述“智能排程 graph”运行所需的外部业务依赖。
//
// 职责边界：
// 1. 只负责声明“需要哪些能力”，不负责具体实现（实现由 service 层注入）。
// 2. 只收口函数签名，不承载业务状态，避免跨请求共享可变数据。
// 3. 当前统一采用 task_class_ids 语义，不再依赖单 task_class_id 主路径。
type SchedulePlanToolDeps struct {
	// SmartPlanningMultiRaw 是可选依赖：
	// 1) 用于需要单独输出“粗排预览”时复用；
	// 2) 当前主链路已由 HybridScheduleWithPlanMulti 覆盖，可不注入。
	SmartPlanningMultiRaw func(ctx context.Context, userID int, taskClassIDs []int) ([]model.UserWeekSchedule, []model.TaskClassItem, error)

	// HybridScheduleWithPlanMulti 把“既有日程 + 粗排结果”合并成统一的 HybridScheduleEntry 切片，
	// 供 daily/weekly ReAct 节点在内存中继续优化。
	HybridScheduleWithPlanMulti func(ctx context.Context, userID int, taskClassIDs []int) ([]model.HybridScheduleEntry, []model.TaskClassItem, error)

	// ResolvePlanningWindow 根据 task_class_ids 解析“全局排程窗口”的相对周/天边界。
	//
	// 返回语义：
	// 1. startWeek/startDay：窗口起点（含）；
	// 2. endWeek/endDay：窗口终点（含）；
	// 3. error：解析失败（如任务类不存在、日期非法）。
	//
	// 用途：
	// 1. 给周级 Move 工具加硬边界，避免把任务移动到窗口外的天数；
	// 2. 解决“首尾不足一周”场景下的周内越界问题。
	ResolvePlanningWindow func(ctx context.Context, userID int, taskClassIDs []int) (startWeek, startDay, endWeek, endDay int, err error)
}

// validate 校验依赖完整性。
//
// 失败处理：
// 1. 任意依赖缺失都直接返回错误，避免 graph 运行到中途才 panic。
// 2. 调用方（runSchedulePlanFlow）收到错误后会走回退链路，不影响普通聊天可用性。
func (d SchedulePlanToolDeps) validate() error {
	if d.HybridScheduleWithPlanMulti == nil {
		return errors.New("schedule plan tool deps: HybridScheduleWithPlanMulti is nil")
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

// ExtraIntSlice 从 extra map 中安全提取整数切片。
//
// 兼容输入：
// 1) []any（JSON 数组反序列化后的常见类型）；
// 2) []int；
// 3) []float64；
// 4) 逗号分隔字符串（例如 "1,2,3"）。
//
// 返回语义：
// 1) ok=true：至少成功解析出一个整数；
// 2) ok=false：字段不存在或全部解析失败。
func ExtraIntSlice(extra map[string]any, key string) ([]int, bool) {
	v, exists := extra[key]
	if !exists {
		return nil, false
	}

	parseOne := func(raw any) (int, error) {
		switch n := raw.(type) {
		case int:
			return n, nil
		case float64:
			return int(n), nil
		case string:
			i, err := strconv.Atoi(n)
			if err != nil {
				return 0, err
			}
			return i, nil
		default:
			return 0, fmt.Errorf("unsupported type: %T", raw)
		}
	}

	out := make([]int, 0)
	switch arr := v.(type) {
	case []int:
		for _, item := range arr {
			out = append(out, item)
		}
	case []float64:
		for _, item := range arr {
			out = append(out, int(item))
		}
	case []any:
		for _, item := range arr {
			if parsed, err := parseOne(item); err == nil {
				out = append(out, parsed)
			}
		}
	case string:
		parts := strings.Split(arr, ",")
		for _, part := range parts {
			part = strings.TrimSpace(part)
			if part == "" {
				continue
			}
			if parsed, err := strconv.Atoi(part); err == nil {
				out = append(out, parsed)
			}
		}
	default:
		return nil, false
	}

	if len(out) == 0 {
		return nil, false
	}
	return out, true
}
