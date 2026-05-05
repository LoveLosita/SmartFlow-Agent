package agentshared

const (
	TaskPriorityImportantUrgent     = 1
	TaskPriorityImportantNotUrgent  = 2
	TaskPrioritySimpleNotImportant  = 3
	TaskPriorityComplexNotImportant = 4
)

// QuickNote 优先级别名，保持与旧 agent/model 命名兼容。
const (
	QuickNotePriorityImportantUrgent     = TaskPriorityImportantUrgent
	QuickNotePriorityImportantNotUrgent  = TaskPriorityImportantNotUrgent
	QuickNotePrioritySimpleNotImportant  = TaskPrioritySimpleNotImportant
	QuickNotePriorityComplexNotImportant = TaskPriorityComplexNotImportant
)

func IsValidTaskPriority(priority int) bool {
	return priority >= TaskPriorityImportantUrgent && priority <= TaskPriorityComplexNotImportant
}

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
