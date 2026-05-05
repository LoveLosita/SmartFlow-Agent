package utils

import (
	"fmt"
	"strings"

	memorymodel "github.com/LoveLosita/smartflow/backend/services/memory/model"
)

// 合法关系类型集合，用于校验 LLM 输出的 relation 字段。
var validRelations = map[string]struct{}{
	memorymodel.RelationDuplicate: {},
	memorymodel.RelationUpdate:    {},
	memorymodel.RelationConflict:  {},
	memorymodel.RelationUnrelated: {},
}

// ValidateComparisonResult 校验单次比对结果的基本合法性。
//
// 职责边界：
// 1. 只校验 LLM 输出的结构合法性，不校验业务语义；
// 2. relation 必须是四种合法值之一，update 时必须有 UpdatedContent；
// 3. 校验失败直接返回 error，调用方决定丢弃或重试。
func ValidateComparisonResult(result *memorymodel.ComparisonResult) error {
	if result == nil {
		return fmt.Errorf("比对结果不能为空")
	}

	// 1. MemoryID 必须大于 0，确保能定位到旧记忆。
	if result.MemoryID <= 0 {
		return fmt.Errorf("比对结果 memory_id 无效: %d", result.MemoryID)
	}

	// 2. relation 必须是四种合法值之一，防止 LLM 输出非法值。
	relation := strings.TrimSpace(strings.ToLower(result.Relation))
	if _, ok := validRelations[relation]; !ok {
		return fmt.Errorf("比对结果 relation 非法: %s", result.Relation)
	}

	// 3. relation=update 时，UpdatedContent 不能为空。
	// 原因：update 需要合并后的完整内容，不能只写差异部分。
	if relation == memorymodel.RelationUpdate {
		if strings.TrimSpace(result.UpdatedContent) == "" {
			return fmt.Errorf("relation=update 时 updated_content 不能为空")
		}
	}

	return nil
}
