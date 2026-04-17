package conv

import (
	"github.com/LoveLosita/smartflow/backend/model"
	"github.com/cloudwego/eino/schema"
)

// ToEinoMessages 将数据库模型转换为 Eino 模型
func ToEinoMessages(dbMsgs []model.ChatHistory) []*schema.Message {
	res := make([]*schema.Message, 0)
	for _, m := range dbMsgs {
		var role schema.RoleType
		switch safeChatHistoryRole(m.Role) {
		case "user":
			role = schema.User
		case "assistant":
			role = schema.Assistant
		default:
			role = schema.System
		}
		msg := &schema.Message{
			Role:             role,
			Content:          safeChatHistoryText(m.MessageContent),
			ReasoningContent: safeChatHistoryText(m.ReasoningContent),
		}
		// retry 机制已整体下线：历史数据里的 retry_* 列不再回灌到运行期上下文。
		extra := make(map[string]any)
		extra["history_id"] = m.ID
		if m.ReasoningDurationSeconds > 0 {
			extra["reasoning_duration_seconds"] = m.ReasoningDurationSeconds
		}
		if len(extra) > 0 {
			msg.Extra = extra
		}
		res = append(res, msg)
	}
	return res
}

func safeChatHistoryRole(role *string) string {
	if role == nil {
		return ""
	}
	return *role
}

func safeChatHistoryText(text *string) string {
	if text == nil {
		return ""
	}
	return *text
}
