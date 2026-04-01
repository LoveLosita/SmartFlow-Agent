package newagentprompt

import (
	"strings"

	newagentmodel "github.com/LoveLosita/smartflow/backend/newAgent/model"
	"github.com/cloudwego/eino/schema"
)

const chatIntentSystemPrompt = `
你是 SmartFlow 的意图分类器。
你的唯一任务是判断用户本轮输入是"纯闲聊"还是"包含任务意图"。

判断规则：
1. chat：打招呼、感谢、简单问答、情感表达、闲聊，不涉及任何具体任务或操作请求。
2. task：包含任何需要规划/执行/操作的意图，包括但不限于查询信息、创建内容、修改数据、安排日程、继续已有任务等。

保守原则：当不确定时，倾向于判断为 task，宁可多走一次规划也不要丢失用户意图。

严格输出以下 JSON（不要输出 markdown，不要在 JSON 外补文字）：
{"intent":"chat或task","reply":"仅当intent=chat时填写你的闲聊回复，task时留空","reason":"简短判断依据"}
`

// BuildChatIntentSystemPrompt 返回意图分类系统提示词。
func BuildChatIntentSystemPrompt() string {
	return strings.TrimSpace(chatIntentSystemPrompt)
}

// BuildChatIntentMessages 组装意图分类的 messages。
//
// 职责边界：
// 1. 只取最近 6 条历史，保证分类高效；
// 2. 不注入 pinned blocks / tool schemas，分类不需要这些信息；
// 3. 不负责解析模型输出。
func BuildChatIntentMessages(conversationContext *newagentmodel.ConversationContext, userInput string) []*schema.Message {
	messages := make([]*schema.Message, 0, 8)

	messages = append(messages, schema.SystemMessage(BuildChatIntentSystemPrompt()))

	if conversationContext != nil {
		history := conversationContext.HistorySnapshot()
		if len(history) > 6 {
			history = history[len(history)-6:]
		}
		if len(history) > 0 {
			messages = append(messages, history...)
		}
	}

	trimmedInput := strings.TrimSpace(userInput)
	if trimmedInput != "" {
		messages = append(messages, schema.UserMessage(trimmedInput))
	}

	return messages
}
