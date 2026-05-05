package utils

import (
	"encoding/json"
	"strings"

	"github.com/LoveLosita/smartflow/backend/services/runtime/model"
)

const (
	// AuditOperationCreate 表示系统新建一条记忆。
	AuditOperationCreate = "create"
	// AuditOperationUpdate 表示决策层更新已有记忆的内容。
	AuditOperationUpdate = "update"
	// AuditOperationArchive 表示治理层把重复记忆归档。
	AuditOperationArchive = "archive"
	// AuditOperationDelete 表示对已有记忆做软删除。
	AuditOperationDelete = "delete"
	// AuditOperationRestore 表示把已删除/归档记忆恢复为 active。
	AuditOperationRestore = "restore"
)

// BuildItemAuditLog 构造记忆变更审计日志。
//
// 职责边界：
// 1. 负责把 before/after 快照统一序列化为审计日志结构；
// 2. 不负责决定“是否应该写审计”，该决策由上层 service/worker 控制；
// 3. 不负责落库，调用方仍需显式调用 AuditRepo。
func BuildItemAuditLog(
	memoryID int64,
	userID int,
	operation string,
	operatorType string,
	reason string,
	before *model.MemoryItem,
	after *model.MemoryItem,
) model.MemoryAuditLog {
	return model.MemoryAuditLog{
		MemoryID:     memoryID,
		UserID:       userID,
		Operation:    strings.TrimSpace(operation),
		OperatorType: NormalizeOperatorType(operatorType),
		Reason:       strings.TrimSpace(reason),
		BeforeJSON:   marshalMemoryItemSnapshot(before),
		AfterJSON:    marshalMemoryItemSnapshot(after),
	}
}

// NormalizeOperatorType 统一规整审计操作者类型。
//
// 规则说明：
// 1. 目前只接受 user/system 两类固定值；
// 2. 空值或未知值统一回退为 user，避免把脏值直接写进审计表；
// 3. 若后续扩展 admin/tool 等类型，再在这里集中放开即可。
func NormalizeOperatorType(raw string) string {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "system":
		return "system"
	default:
		return "user"
	}
}

func marshalMemoryItemSnapshot(item *model.MemoryItem) *string {
	if item == nil {
		return nil
	}

	raw, err := json.Marshal(item)
	if err != nil {
		empty := "{}"
		return &empty
	}

	value := string(raw)
	return &value
}
