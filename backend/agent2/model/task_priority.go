package agentmodel

const (
	TaskPriorityImportantUrgent     = 1
	TaskPriorityImportantNotUrgent  = 2
	TaskPrioritySimpleNotImportant  = 3
	TaskPriorityComplexNotImportant = 4
)

// IsValidTaskPriority 用于校验任务优先级是否合法。
//
// 职责边界：
// 1. 只负责判断 priority 是否落在系统支持的 1~4 范围内。
// 2. 不负责把自然语言映射成优先级，也不负责做业务兜底推断。
func IsValidTaskPriority(priority int) bool {
	return priority >= TaskPriorityImportantUrgent && priority <= TaskPriorityComplexNotImportant
}

// PriorityLabelCN 返回任务优先级对应的中文标签。
//
// 职责边界：
// 1. 只负责“优先级枚举 -> 中文展示文案”的稳定映射。
// 2. 不负责国际化、多语言切换或业务规则解释。
func PriorityLabelCN(priority int) string {
	switch priority {
	case TaskPriorityImportantUrgent:
		return "重要且紧急"
	case TaskPriorityImportantNotUrgent:
		return "重要不紧急"
	case TaskPrioritySimpleNotImportant:
		return "简单不重要"
	case TaskPriorityComplexNotImportant:
		return "复杂不重要"
	default:
		return "未知优先级"
	}
}
