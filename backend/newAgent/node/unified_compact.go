package newagentnode

import (
	"context"
	"encoding/json"
	"fmt"
	"log"

	infrallm "github.com/LoveLosita/smartflow/backend/infra/llm"
	newagentmodel "github.com/LoveLosita/smartflow/backend/newAgent/model"
	newagentprompt "github.com/LoveLosita/smartflow/backend/newAgent/prompt"
	newagentstream "github.com/LoveLosita/smartflow/backend/newAgent/stream"
	"github.com/LoveLosita/smartflow/backend/pkg"
	"github.com/cloudwego/eino/schema"
)

// UnifiedCompactInput 是统一压缩入口的参数。
//
// 设计说明：
//  1. 从 ExecuteNodeInput 中提取压缩所需的公共字段，消除对 Execute 的直接依赖；
//  2. 各节点（Plan/Chat/Deliver）构造此参数时从自己的 NodeInput 中提取对应字段；
//  3. StageName 和 StatusBlockID 用于区分日志来源和 SSE 状态推送。
type UnifiedCompactInput struct {
	// Client 用于调用 LLM 压缩 msg1/msg2。
	Client *infrallm.Client
	// CompactionStore 用于持久化压缩摘要和 token 统计，为 nil 时跳过持久化。
	CompactionStore newagentmodel.CompactionStore
	// FlowState 提供 userID / chatID / roundUsed 等定位信息。
	FlowState *newagentmodel.CommonState
	// Emitter 用于推送压缩进度 SSE 事件。
	Emitter *newagentstream.ChunkEmitter
	// StageName 标识当前阶段（如 "execute"/"plan"/"chat"/"deliver"），用于日志和缓存 key。
	StageName string
	// StatusBlockID 是 SSE 状态推送的 block ID，各节点使用自己的 block ID。
	StatusBlockID string
}

// compactUnifiedMessagesIfNeeded 检查统一消息结构的 token 预算，
// 超限时对 msg1（历史对话）和 msg2（阶段工作区）执行 LLM 压缩。
//
// 消息布局约定（由 buildUnifiedStageMessages 返回）：
//
//	[0] system    — msg0: 系统规则 + 工具简表
//	[1] assistant — msg1: 历史对话上下文
//	[2] assistant — msg2: 阶段工作区（Execute=ReAct Loop，其余="暂无"）
//	[3] system    — msg3: 阶段状态 + 记忆 + 指令
//
// 压缩策略：
//  1. msg1 超过可用预算一半时触发 LLM 压缩（合并已有摘要 + 新内容）；
//  2. msg1 压缩后仍超限，则对 msg2 也做 LLM 压缩；
//  3. 压缩结果持久化到 CompactionStore，下一轮可复用摘要避免重复计算。
func compactUnifiedMessagesIfNeeded(
	ctx context.Context,
	messages []*schema.Message,
	input UnifiedCompactInput,
) []*schema.Message {
	if input.FlowState == nil {
		log.Printf("[COMPACT:%s] FlowState is nil, skip token stats refresh", input.StageName)
		return messages
	}

	// 1. 非严格 4 段式时，退化成按角色汇总的统计，确保 context_token_stats 仍然刷新。
	if len(messages) != 4 {
		breakdown := estimateFallbackStageTokenBreakdown(messages)
		log.Printf(
			"[COMPACT:%s] fallback token stats refresh: total=%d budget=%d count=%d (msg0=%d msg1=%d msg2=%d msg3=%d)",
			input.StageName, breakdown.Total, breakdown.Budget, len(messages),
			breakdown.Msg0, breakdown.Msg1, breakdown.Msg2, breakdown.Msg3,
		)
		saveUnifiedTokenStats(ctx, input, breakdown)
		return messages
	}

	// 2. 提取四条消息的文本内容。
	msg0 := messages[0].Content
	msg1 := messages[1].Content
	msg2 := messages[2].Content
	msg3 := messages[3].Content

	// 3. Token 预算检查。
	breakdown, overBudget, needCompactMsg1, needCompactMsg2 := pkg.CheckStageTokenBudget(msg0, msg1, msg2, msg3)

	log.Printf(
		"[COMPACT:%s] token budget check: total=%d budget=%d over=%v compactMsg1=%v compactMsg2=%v (msg0=%d msg1=%d msg2=%d msg3=%d)",
		input.StageName, breakdown.Total, breakdown.Budget, overBudget, needCompactMsg1, needCompactMsg2,
		breakdown.Msg0, breakdown.Msg1, breakdown.Msg2, breakdown.Msg3,
	)

	if !overBudget {
		// 4. 未超限，记录 token 分布后直接返回。
		saveUnifiedTokenStats(ctx, input, breakdown)
		return messages
	}

	// 5. msg1 压缩（历史对话 → LLM 摘要）。
	if needCompactMsg1 {
		msg1 = compactUnifiedMsg1(ctx, input, msg1)
		messages[1].Content = msg1
		// 压缩 msg1 后重算预算。
		breakdown = pkg.EstimateStageMessagesTokens(msg0, msg1, msg2, msg3)
	}

	// 6. msg2 压缩（阶段工作区 → LLM 摘要）。
	if needCompactMsg2 || breakdown.Total > pkg.StageTokenBudget {
		msg2 = compactUnifiedMsg2(ctx, input, msg2)
		messages[2].Content = msg2
		breakdown = pkg.EstimateStageMessagesTokens(msg0, msg1, msg2, msg3)
	}

	// 7. 记录最终 token 分布。
	saveUnifiedTokenStats(ctx, input, breakdown)

	log.Printf(
		"[COMPACT:%s] after compaction: total=%d budget=%d (msg0=%d msg1=%d msg2=%d msg3=%d)",
		input.StageName, breakdown.Total, breakdown.Budget,
		breakdown.Msg0, breakdown.Msg1, breakdown.Msg2, breakdown.Msg3,
	)
	return messages
}

// estimateFallbackStageTokenBreakdown 在非统一 4 段式场景下按消息角色做近似统计。
//
// 步骤说明：
// 1. 先按消息类型汇总 token，保证总量准确；
// 2. 再把最后一个 user 消息尽量视作 msg3，保留阶段指令语义；
// 3. 其他历史内容归入 msg1 / msg2，确保上下文统计不会因为结构不标准而断更。
func estimateFallbackStageTokenBreakdown(messages []*schema.Message) pkg.StageTokenBreakdown {
	breakdown := pkg.StageTokenBreakdown{Budget: pkg.StageTokenBudget}
	if len(messages) == 0 {
		return breakdown
	}

	lastUserIndex := -1
	for i := len(messages) - 1; i >= 0; i-- {
		msg := messages[i]
		if msg == nil {
			continue
		}
		if msg.Role == schema.User {
			lastUserIndex = i
			break
		}
	}

	for i, msg := range messages {
		if msg == nil {
			continue
		}
		tokens := pkg.EstimateMessageTokens(msg)
		breakdown.Total += tokens

		switch msg.Role {
		case schema.System:
			breakdown.Msg0 += tokens
		case schema.User:
			if i == lastUserIndex {
				breakdown.Msg3 += tokens
			} else {
				breakdown.Msg1 += tokens
			}
		case schema.Tool:
			breakdown.Msg2 += tokens
		case schema.Assistant:
			if len(msg.ToolCalls) > 0 {
				breakdown.Msg2 += tokens
			} else {
				breakdown.Msg1 += tokens
			}
		default:
			breakdown.Msg1 += tokens
		}
	}

	return breakdown
}

// compactUnifiedMsg1 对 msg1（历史对话）执行 LLM 压缩。
//
// 步骤化说明：
//  1. CompactionStore 为 nil 时跳过（测试环境 / 骨架期）；
//  2. 先加载该阶段已有的压缩摘要，与当前 msg1 合并后调 LLM 压缩；
//  3. 压缩失败时降级为原始文本，不中断主流程；
//  4. 压缩成功后持久化新摘要，供下一轮复用。
func compactUnifiedMsg1(
	ctx context.Context,
	input UnifiedCompactInput,
	msg1 string,
) string {
	// 1. CompactionStore 为 nil 时无法加载/保存摘要，跳过压缩。
	if input.CompactionStore == nil {
		log.Printf("[COMPACT:%s] CompactionStore is nil, skip msg1 compaction", input.StageName)
		return msg1
	}

	// 2. 加载该阶段已有的压缩摘要（可能为空）。
	existingSummary, _, err := input.CompactionStore.LoadStageCompaction(ctx, input.FlowState.UserID, input.FlowState.ConversationID, input.StageName)
	if err != nil {
		log.Printf("[COMPACT:%s] load existing compaction failed: %v, proceed without cache", input.StageName, err)
	}

	// 3. SSE: 压缩开始。
	tokenBefore := pkg.EstimateTextTokens(msg1)
	_ = input.Emitter.EmitStatus(
		input.StatusBlockID, input.StageName, "context_compact_start",
		fmt.Sprintf("正在压缩对话历史（%d tokens）...", tokenBefore),
		false,
	)

	// 4. 调用 LLM 压缩：将 msg1 全文 + 已有摘要合并为一份紧凑摘要。
	newSummary, err := newagentprompt.CompactMsg1(ctx, input.Client, msg1, existingSummary)
	if err != nil {
		log.Printf("[COMPACT:%s] compact msg1 failed: %v", input.StageName, err)
		_ = input.Emitter.EmitStatus(
			input.StatusBlockID, input.StageName, "context_compact_done",
			"对话历史压缩失败，使用原始文本",
			false,
		)
		return msg1
	}

	// 5. SSE: 压缩完成。
	tokenAfter := pkg.EstimateTextTokens(newSummary)
	_ = input.Emitter.EmitStatus(
		input.StatusBlockID, input.StageName, "context_compact_done",
		fmt.Sprintf("对话历史已压缩：%d → %d tokens", tokenBefore, tokenAfter),
		false,
	)

	// 6. 持久化压缩结果，下一轮可直接复用摘要。
	if err := input.CompactionStore.SaveStageCompaction(ctx, input.FlowState.UserID, input.FlowState.ConversationID, input.StageName, newSummary, input.FlowState.RoundUsed); err != nil {
		log.Printf("[COMPACT:%s] save compaction failed: %v", input.StageName, err)
	}

	return newSummary
}

// compactUnifiedMsg2 对 msg2（阶段工作区）执行 LLM 压缩。
//
// 步骤化说明：
//  1. 非 Execute 阶段的 msg2 通常是"暂无"，压缩无意义但不会出错；
//  2. Execute 阶段的 msg2 包含 ReAct loop 记录，压缩可显著节省 token；
//  3. 压缩失败时降级为原始文本，不中断主流程。
func compactUnifiedMsg2(
	ctx context.Context,
	input UnifiedCompactInput,
	msg2 string,
) string {
	// 1. SSE: 压缩开始。
	tokenBefore := pkg.EstimateTextTokens(msg2)
	_ = input.Emitter.EmitStatus(
		input.StatusBlockID, input.StageName, "context_compact_start",
		fmt.Sprintf("正在压缩执行记录（%d tokens）...", tokenBefore),
		false,
	)

	// 2. 调用 LLM 压缩。
	compressed, err := newagentprompt.CompactMsg2(ctx, input.Client, msg2)
	if err != nil {
		log.Printf("[COMPACT:%s] compact msg2 failed: %v", input.StageName, err)
		_ = input.Emitter.EmitStatus(
			input.StatusBlockID, input.StageName, "context_compact_done",
			"执行记录压缩失败，使用原始文本",
			false,
		)
		return msg2
	}

	// 3. SSE: 压缩完成。
	tokenAfter := pkg.EstimateTextTokens(compressed)
	_ = input.Emitter.EmitStatus(
		input.StatusBlockID, input.StageName, "context_compact_done",
		fmt.Sprintf("执行记录已压缩：%d → %d tokens", tokenBefore, tokenAfter),
		false,
	)

	return compressed
}

// saveUnifiedTokenStats 持久化当前 token 分布到 DB。
//
// 步骤化说明：
//  1. CompactionStore 为 nil 时跳过（测试环境 / 骨架期）；
//  2. 序列化失败只记日志，不中断主流程；
//  3. 写入失败只记日志，不中断主流程。
func saveUnifiedTokenStats(
	ctx context.Context,
	input UnifiedCompactInput,
	breakdown pkg.StageTokenBreakdown,
) {
	if input.CompactionStore == nil || input.FlowState == nil {
		return
	}
	statsJSON, err := json.Marshal(breakdown)
	if err != nil {
		log.Printf("[COMPACT:%s] marshal token stats failed: %v", input.StageName, err)
		return
	}
	if err := input.CompactionStore.SaveContextTokenStats(ctx, input.FlowState.UserID, input.FlowState.ConversationID, string(statsJSON)); err != nil {
		log.Printf("[COMPACT:%s] save token stats failed: %v", input.StageName, err)
	}
}
