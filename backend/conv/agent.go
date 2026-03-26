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
		extra := make(map[string]any)
		extra["history_id"] = m.ID
		if m.ReasoningDurationSeconds > 0 {
			extra["reasoning_duration_seconds"] = m.ReasoningDurationSeconds
		}
		if m.RetryGroupID != nil && *m.RetryGroupID != "" {
			extra["retry_group_id"] = *m.RetryGroupID
		}
		if m.RetryIndex != nil && *m.RetryIndex > 0 {
			extra["retry_index"] = *m.RetryIndex
		}
		if m.RetryFromUserMessageID != nil && *m.RetryFromUserMessageID > 0 {
			extra["retry_from_user_message_id"] = *m.RetryFromUserMessageID
		}
		if m.RetryFromAssistantMessageID != nil && *m.RetryFromAssistantMessageID > 0 {
			extra["retry_from_assistant_message_id"] = *m.RetryFromAssistantMessageID
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
