package model

import "strings"

const (
	// MemoryTypePreference 表示用户偏好类记忆。
	MemoryTypePreference = "preference"
	// MemoryTypeConstraint 表示硬约束类记忆。
	MemoryTypeConstraint = "constraint"
	// MemoryTypeFact 表示一般事实类记忆。
	MemoryTypeFact = "fact"
)

const (
	// DecisionActionAdd 表示新增记忆。
	DecisionActionAdd = "ADD"
	// DecisionActionUpdate 表示更新记忆。
	DecisionActionUpdate = "UPDATE"
	// DecisionActionDelete 表示删除记忆。
	DecisionActionDelete = "DELETE"
	// DecisionActionNone 表示不做写入动作。
	DecisionActionNone = "NONE"
)

var validMemoryTypes = map[string]struct{}{
	MemoryTypePreference: {},
	MemoryTypeConstraint: {},
	MemoryTypeFact:       {},
}

var validDecisionActions = map[string]struct{}{
	DecisionActionAdd:    {},
	DecisionActionUpdate: {},
	DecisionActionDelete: {},
	DecisionActionNone:   {},
}

// NormalizeMemoryType 统一记忆类型字符串。
//
// 职责边界：
// 1. 只做字符串标准化，不做业务兜底；
// 2. 若调用方传入非法类型，返回空字符串供上游决定丢弃或降级。
func NormalizeMemoryType(memoryType string) string {
	normalized := strings.ToLower(strings.TrimSpace(memoryType))
	if _, ok := validMemoryTypes[normalized]; !ok {
		return ""
	}
	return normalized
}

// IsValidDecisionAction 校验决策动作是否合法。
func IsValidDecisionAction(action string) bool {
	normalized := strings.ToUpper(strings.TrimSpace(action))
	_, ok := validDecisionActions[normalized]
	return ok
}
