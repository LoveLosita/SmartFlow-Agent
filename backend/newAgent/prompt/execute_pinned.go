package newagentprompt

import (
	"strings"

	newagentmodel "github.com/LoveLosita/smartflow/backend/newAgent/model"
)

const executeMemoryContextKey = "memory_context"

// renderExecuteMemoryContext 提取 Execute 阶段需要补充到 msg3 的记忆文本。
//
// 步骤化说明：
// 1. 只白名单消费 memory_context，避免把 execution_context / current_step 等 Execute 自有块再次注入；
// 2. 若 block 不存在或正文为空，直接返回空串，不给 msg3 留空段；
// 3. 这里不重新渲染记忆，只消费 agentsvc 已经产出的最终文本，保证所有阶段口径一致。
func renderExecuteMemoryContext(ctx *newagentmodel.ConversationContext) string {
	if ctx == nil {
		return ""
	}

	block, ok := ctx.PinnedBlockByKey(executeMemoryContextKey)
	if !ok {
		return ""
	}
	content := strings.TrimSpace(block.Content)
	if content == "" {
		return ""
	}
	return content
}
