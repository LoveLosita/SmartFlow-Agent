package newagentprompt

import (
	"fmt"
	"strings"

	newagentmodel "github.com/LoveLosita/smartflow/backend/newAgent/model"
	"github.com/cloudwego/eino/schema"
)

const deliverHistoryKindExecuteLoopClosed = "execute_loop_closed"

// sliceHistoryAfterLastExecuteLoopClosed 基于最后一个 execute_loop_closed 标记切出当前活跃窗口。
//
// 步骤化说明：
// 1. 先读取完整 history 快照，避免直接在 ConversationContext 原地切片，减少后续调用方误改底层数组的风险；
// 2. 从后往前找最后一个 execute_loop_closed，确保拿到的是“最近一次已正常收口”的边界；
// 3. 命中边界后只返回边界之后的消息，这样 deliver 看到的就是当前活跃轮次；
// 4. 若完全没有边界，说明会话尚未形成稳定闭环，此时退回全量 history，避免误丢当前活跃上下文。
func sliceHistoryAfterLastExecuteLoopClosed(ctx *newagentmodel.ConversationContext) []*schema.Message {
	if ctx == nil {
		return nil
	}

	history := ctx.HistorySnapshot()
	if len(history) == 0 {
		return nil
	}

	cut := -1
	for i := len(history) - 1; i >= 0; i-- {
		if isDeliverExecuteLoopClosedMarker(history[i]) {
			cut = i
			break
		}
	}
	if cut < 0 {
		return history
	}
	if cut+1 >= len(history) {
		return nil
	}
	return history[cut+1:]
}

// isDeliverExecuteLoopClosedMarker 判断一条历史消息是否为 execute loop 正常收口边界。
//
// 职责边界：
// 1. 这里只识别 prompt 层真正关心的 execute_loop_closed 标记；
// 2. 不负责推断其他中断/恢复语义，避免把 confirm/ask_user 等同一轮过程误判成新边界；
// 3. 若消息结构不完整，则统一按“非边界”处理，保证切窗策略保守可回退。
func isDeliverExecuteLoopClosedMarker(msg *schema.Message) bool {
	if msg == nil || msg.Extra == nil {
		return false
	}
	kind, ok := msg.Extra[executeHistoryKindKey].(string)
	if !ok {
		return false
	}
	return strings.TrimSpace(kind) == deliverHistoryKindExecuteLoopClosed
}

// buildDeliverExecuteWindow 基于当前活跃 history 窗口渲染 deliver 节点要看的执行事实视图。
//
// 步骤化说明：
// 1. 先按 execute_loop_closed 切掉旧轮次，只保留当前仍活跃的执行窗口；
// 2. 再分别抽取“本轮真实对话流”和“本轮 ReAct 工具事实链”，避免 deliver 回看旧 deliver 总结；
// 3. 若本轮还没有工具调用，也要明确告诉模型“当前无工具事实”，避免它擅自脑补；
// 4. 整段文本只服务 deliver.msg2，不改变四段式骨架，也不回写任何状态。
func buildDeliverExecuteWindow(ctx *newagentmodel.ConversationContext) string {
	lines := []string{"本轮 execute 窗口："}

	historyWindow := sliceHistoryAfterLastExecuteLoopClosed(ctx)
	if len(historyWindow) == 0 {
		lines = append(lines, "- 当前没有可用的本轮执行窗口，请仅依据结果态工作区诚实收口。")
		return strings.Join(lines, "\n")
	}

	turns := collectExecuteConversationTurns(historyWindow)
	if len(turns) == 0 {
		lines = append(lines, "- 本轮对话流：暂无。")
	} else {
		lines = append(lines, "本轮对话流：")
		for _, turn := range turns {
			lines = append(lines, fmt.Sprintf("- %s: %q", turn.Role, turn.Content))
		}
	}

	loops := collectExecuteLoopRecords(historyWindow)
	if len(loops) == 0 {
		lines = append(lines, "- 本轮 ReAct 记录：暂无工具调用。")
		return strings.Join(lines, "\n")
	}

	lines = append(lines, "本轮 ReAct 记录：")
	for i, loop := range loops {
		lines = append(lines, fmt.Sprintf("%d. thought/reason：%s", i+1, loop.Thought))
		lines = append(lines, fmt.Sprintf("   tool_call：%s", renderExecuteToolCallText(loop.ToolName, loop.ToolArgs)))
		lines = append(lines, fmt.Sprintf("   observation：%s", loop.Observation))
	}

	return strings.Join(lines, "\n")
}
