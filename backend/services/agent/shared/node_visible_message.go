package agentshared

import (
	"context"
	"log"
	"strings"

	agentmodel "github.com/LoveLosita/smartflow/backend/services/agent/model"
	"github.com/cloudwego/eino/schema"
)

// PersistVisibleAssistantMessage 负责把“真正要展示给用户”的 assistant 文本交给 service 层持久化。
//
// 职责边界：
// 1. 只处理可见的 assistant 消息，不处理内部纠错提示、工具调用结果和纯状态文案；
// 2. 持久化失败只记日志，不反向中断节点主流程，避免“已经对外输出但后端补写失败”时把用户请求打断；
// 3. 具体的 Redis / MySQL / 乐观缓存写入由 service 回调统一完成。
func PersistVisibleAssistantMessage(
	ctx context.Context,
	persist agentmodel.PersistVisibleMessageFunc,
	state *agentmodel.CommonState,
	msg *schema.Message,
) {
	if persist == nil || state == nil || msg == nil {
		return
	}

	role := strings.TrimSpace(string(msg.Role))
	content := strings.TrimSpace(msg.Content)
	if role != string(schema.Assistant) || content == "" {
		return
	}

	if err := persist(ctx, state, msg); err != nil {
		log.Printf("[WARN] persist visible assistant message failed chat=%s phase=%s err=%v", state.ConversationID, state.Phase, err)
	}
}
