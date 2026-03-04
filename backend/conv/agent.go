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
		switch *m.Role {
		case "user":
			role = schema.User
		case "assistant":
			role = schema.Assistant
		default:
			role = schema.System
		}
		res = append(res, &schema.Message{
			Role:    role,
			Content: *m.MessageContent,
		})
	}
	return res
}
