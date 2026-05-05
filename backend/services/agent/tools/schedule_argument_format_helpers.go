package agenttools

import (
	"fmt"
	"strings"

	"github.com/LoveLosita/smartflow/backend/services/agent/tools/schedule"
)

// formatScheduleDayCN 为参数展示生成中文日期标签。
//
// 职责边界：
// 1. 只服务父包 ArgumentView 的中文展示；
// 2. 不参与 schedule.read_result 卡片构造，read 卡片已切到 schedule_read 子包；
// 3. 当 state 缺失或日期映射不存在时，退回稳定的“第 N 天”文本。
func formatScheduleDayCN(state *schedule.ScheduleState, day int) string {
	if day <= 0 {
		return "未知日期"
	}
	if state != nil {
		if week, dayOfWeek, ok := state.DayToWeekDay(day); ok {
			return fmt.Sprintf("第%d天（第%d周 %s）", day, week, formatScheduleWeekdayCN(dayOfWeek))
		}
	}
	return fmt.Sprintf("第%d天", day)
}

func formatScheduleWeekdayCN(dayOfWeek int) string {
	switch dayOfWeek {
	case 1:
		return "周一"
	case 2:
		return "周二"
	case 3:
		return "周三"
	case 4:
		return "周四"
	case 5:
		return "周五"
	case 6:
		return "周六"
	case 7:
		return "周日"
	default:
		return fmt.Sprintf("周%d", dayOfWeek)
	}
}

func formatScheduleWeekListCN(weeks []int) string {
	if len(weeks) == 0 {
		return "不限周次"
	}
	parts := make([]string, 0, len(weeks))
	for _, week := range weeks {
		if week <= 0 {
			continue
		}
		parts = append(parts, fmt.Sprintf("第%d周", week))
	}
	if len(parts) == 0 {
		return "不限周次"
	}
	return strings.Join(parts, "、")
}

func formatScheduleSectionListCN(sections []int) string {
	if len(sections) == 0 {
		return "无"
	}
	parts := make([]string, 0, len(sections))
	for _, section := range sections {
		if section <= 0 {
			continue
		}
		parts = append(parts, fmt.Sprintf("第%d节", section))
	}
	if len(parts) == 0 {
		return "无"
	}
	return strings.Join(parts, "、")
}

func formatTargetPoolStatusCN(status string) string {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "all":
		return "全部任务"
	case "existing":
		return "已安排任务"
	case "suggested":
		return "已预排任务"
	case "pending":
		return "待安排任务"
	default:
		return fallbackText(status, "任务池")
	}
}

func formatSlotTypeLabelCN(slotType string) string {
	switch strings.ToLower(strings.TrimSpace(slotType)) {
	case "", "empty", "strict":
		return "纯空位"
	case "embedded_candidate", "embedded", "embed":
		return "可嵌入候选"
	default:
		return strings.TrimSpace(slotType)
	}
}

func formatDayScopeLabelCN(scope string) string {
	switch strings.ToLower(strings.TrimSpace(scope)) {
	case "workday":
		return "工作日"
	case "weekend":
		return "周末"
	default:
		return "全部日期"
	}
}

func formatBoolLabelCN(value bool) string {
	if value {
		return "是"
	}
	return "否"
}

func formatWeekdayListCN(days []int) string {
	if len(days) == 0 {
		return "不限星期"
	}
	parts := make([]string, 0, len(days))
	for _, day := range days {
		parts = append(parts, formatScheduleWeekdayCN(day))
	}
	return strings.Join(parts, "、")
}

func fallbackText(text string, fallback string) string {
	if strings.TrimSpace(text) == "" {
		return strings.TrimSpace(fallback)
	}
	return strings.TrimSpace(text)
}
