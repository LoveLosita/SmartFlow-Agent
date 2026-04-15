package newagentnode

import (
	"context"
	"encoding/json"
	"fmt"
	"log"

	newagentmodel "github.com/LoveLosita/smartflow/backend/newAgent/model"
	newagentprompt "github.com/LoveLosita/smartflow/backend/newAgent/prompt"
	newagentstream "github.com/LoveLosita/smartflow/backend/newAgent/stream"
	"github.com/LoveLosita/smartflow/backend/pkg"
	"github.com/cloudwego/eino/schema"
)

// compactExecuteMessagesIfNeeded 检查 Execute prompt 的 token 预算，
// 超限时对 msg1（历史对话）和 msg2（ReAct Loop）执行 LLM 压缩。
//
// 消息布局约定（由 BuildExecuteMessages 返回）：
//
//	[0] system    — msg0: 系统规则
//	[1] assistant — msg1: 历史对话上下文
//	[2] assistant — msg2: 当轮 ReAct Loop 记录
//	[3] system    — msg3: 当前状态 + 用户提示
func compactExecuteMessagesIfNeeded(
	ctx context.Context,
	messages []*schema.Message,
	input ExecuteNodeInput,
	flowState *newagentmodel.CommonState,
	emitter *newagentstream.ChunkEmitter,
) []*schema.Message {
	if len(messages) != 4 {
		return messages
	}

	// 提取四条消息的文本内容
	msg0 := messages[0].Content
	msg1 := messages[1].Content
	msg2 := messages[2].Content
	msg3 := messages[3].Content

	// Token 预算检查
	breakdown, overBudget, needCompactMsg1, needCompactMsg2 := pkg.CheckExecuteTokenBudget(msg0, msg1, msg2, msg3)

	log.Printf(
		"[COMPACT] token budget check: total=%d budget=%d over=%v compactMsg1=%v compactMsg2=%v (msg0=%d msg1=%d msg2=%d msg3=%d)",
		breakdown.Total, breakdown.Budget, overBudget, needCompactMsg1, needCompactMsg2,
		breakdown.Msg0, breakdown.Msg1, breakdown.Msg2, breakdown.Msg3,
	)

	if !overBudget {
		// 未超限，记录 token 分布后直接返回
		saveTokenStats(ctx, input, flowState, breakdown)
		return messages
	}

	// ---- msg1 压缩 ----
	if needCompactMsg1 {
		msg1 = compactMsg1IfNeeded(ctx, input, flowState, emitter, msg1)
		messages[1].Content = msg1
		// 压缩 msg1 后重算预算
		breakdown = pkg.EstimateExecuteMessagesTokens(msg0, msg1, msg2, msg3)
	}

	// ---- msg2 压缩 ----
	if needCompactMsg2 || breakdown.Total > pkg.ExecuteTokenBudget {
		msg2 = compactMsg2IfNeeded(ctx, input, flowState, emitter, msg2)
		messages[2].Content = msg2
		breakdown = pkg.EstimateExecuteMessagesTokens(msg0, msg1, msg2, msg3)
	}

	// 记录最终 token 分布
	saveTokenStats(ctx, input, flowState, breakdown)

	log.Printf(
		"[COMPACT] after compaction: total=%d budget=%d (msg0=%d msg1=%d msg2=%d msg3=%d)",
		breakdown.Total, breakdown.Budget,
		breakdown.Msg0, breakdown.Msg1, breakdown.Msg2, breakdown.Msg3,
	)
	return messages
}

// compactMsg1IfNeeded 对 msg1（历史对话）执行 LLM 压缩。
func compactMsg1IfNeeded(
	ctx context.Context,
	input ExecuteNodeInput,
	flowState *newagentmodel.CommonState,
	emitter *newagentstream.ChunkEmitter,
	msg1 string,
) string {
	compactionStore := input.CompactionStore
	if compactionStore == nil {
		log.Printf("[COMPACT] CompactionStore is nil, skip msg1 compaction")
		return msg1
	}

	// 加载已有压缩摘要
	existingSummary, _, err := compactionStore.LoadCompaction(ctx, flowState.UserID, flowState.ConversationID)
	if err != nil {
		log.Printf("[COMPACT] load existing compaction failed: %v, proceed without cache", err)
	}

	// SSE: 压缩开始
	tokenBefore := pkg.EstimateTextTokens(msg1)
	_ = emitter.EmitStatus(
		executeStatusBlockID, "compact_msg1", "context_compact_start",
		fmt.Sprintf("正在压缩对话历史（%d tokens）...", tokenBefore),
		false,
	)

	// 调用 LLM 压缩
	newSummary, err := newagentprompt.CompactMsg1(ctx, input.Client, msg1, existingSummary)
	if err != nil {
		log.Printf("[COMPACT] compact msg1 failed: %v", err)
		_ = emitter.EmitStatus(
			executeStatusBlockID, "compact_msg1", "context_compact_done",
			"对话历史压缩失败，使用原始文本",
			false,
		)
		return msg1
	}

	// SSE: 压缩完成
	tokenAfter := pkg.EstimateTextTokens(newSummary)
	_ = emitter.EmitStatus(
		executeStatusBlockID, "compact_msg1", "context_compact_done",
		fmt.Sprintf("对话历史已压缩：%d → %d tokens", tokenBefore, tokenAfter),
		false,
	)

	// 持久化压缩结果
	if err := compactionStore.SaveCompaction(ctx, flowState.UserID, flowState.ConversationID, newSummary, flowState.RoundUsed); err != nil {
		log.Printf("[COMPACT] save compaction failed: %v", err)
	}

	return newSummary
}

// compactMsg2IfNeeded 对 msg2（ReAct Loop 记录）执行 LLM 压缩。
func compactMsg2IfNeeded(
	ctx context.Context,
	input ExecuteNodeInput,
	flowState *newagentmodel.CommonState,
	emitter *newagentstream.ChunkEmitter,
	msg2 string,
) string {
	// SSE: 压缩开始
	tokenBefore := pkg.EstimateTextTokens(msg2)
	_ = emitter.EmitStatus(
		executeStatusBlockID, "compact_msg2", "context_compact_start",
		fmt.Sprintf("正在压缩执行记录（%d tokens）...", tokenBefore),
		false,
	)

	// 调用 LLM 压缩
	compressed, err := newagentprompt.CompactMsg2(ctx, input.Client, msg2)
	if err != nil {
		log.Printf("[COMPACT] compact msg2 failed: %v", err)
		_ = emitter.EmitStatus(
			executeStatusBlockID, "compact_msg2", "context_compact_done",
			"执行记录压缩失败，使用原始文本",
			false,
		)
		return msg2
	}

	// SSE: 压缩完成
	tokenAfter := pkg.EstimateTextTokens(compressed)
	_ = emitter.EmitStatus(
		executeStatusBlockID, "compact_msg2", "context_compact_done",
		fmt.Sprintf("执行记录已压缩：%d → %d tokens", tokenBefore, tokenAfter),
		false,
	)

	return compressed
}

// saveTokenStats 持久化当前 token 分布到 DB。
func saveTokenStats(
	ctx context.Context,
	input ExecuteNodeInput,
	flowState *newagentmodel.CommonState,
	breakdown pkg.ExecuteTokenBreakdown,
) {
	compactionStore := input.CompactionStore
	if compactionStore == nil {
		return
	}
	statsJSON, err := json.Marshal(breakdown)
	if err != nil {
		log.Printf("[COMPACT] marshal token stats failed: %v", err)
		return
	}
	if err := compactionStore.SaveContextTokenStats(ctx, flowState.UserID, flowState.ConversationID, string(statsJSON)); err != nil {
		log.Printf("[COMPACT] save token stats failed: %v", err)
	}
}
