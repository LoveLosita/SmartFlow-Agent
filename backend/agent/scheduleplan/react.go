package scheduleplan

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/LoveLosita/smartflow/backend/agent/chat"
	"github.com/cloudwego/eino-ext/components/model/ark"
	"github.com/cloudwego/eino/schema"
	arkModel "github.com/volcengine/volcengine-go-sdk/service/arkruntime/model"
)

// reactRoundTimeout 是单轮 ReAct 的超时时间。
// 深度思考模式下 reasoning 阶段可能耗时较长，需要给足时间。
const reactRoundTimeout = 15 * time.Minute

// runReactRefineNode 执行 ReAct 精排循环。
//
// 核心流程（最多 ReactMaxRound 轮）：
// 1. 构造 messages（system prompt + 混合日程 JSON + 上轮 tool 结果）
// 2. 调用 chatModel.Stream() + ThinkingTypeEnabled
// 3. reasoning_content 实时推送到 outChan（前端可见思考过程）
// 4. content 累积后解析：done=true 则退出，tool_calls 则执行
// 5. tool 结果拼入下一轮 messages
func runReactRefineNode(
	ctx context.Context,
	st *SchedulePlanState,
	chatModel *ark.ChatModel,
	outChan chan<- string,
	modelName string,
	emitStage func(stage, detail string),
) (*SchedulePlanState, error) {
	if st == nil {
		return nil, fmt.Errorf("schedule plan graph: nil state in reactRefine node")
	}
	if chatModel == nil {
		return nil, fmt.Errorf("schedule plan graph: model is nil in reactRefine node")
	}
	if len(st.HybridEntries) == 0 {
		st.ReactDone = true
		st.ReactSummary = "无可优化的排程条目。"
		return st, nil
	}

	// 准备 SSE 流式输出的基础参数
	if strings.TrimSpace(modelName) == "" {
		modelName = "smartflow-worker"
	}

	// 构造混合日程 JSON（只在首轮构造，后续轮次复用）
	hybridJSON, err := json.Marshal(st.HybridEntries)
	if err != nil {
		return nil, fmt.Errorf("序列化混合日程失败: %w", err)
	}

	// 用户约束文本
	constraintsText := "无"
	if len(st.Constraints) > 0 {
		constraintsText = strings.Join(st.Constraints, "、")
	}

	// 对话历史：跨轮次累积
	messages := []*schema.Message{
		schema.SystemMessage(SchedulePlanReactSystemPrompt),
		schema.UserMessage(fmt.Sprintf(
			"以下是当前混合日程（JSON）：\n%s\n\n用户约束：%s\n\n请分析并优化 suggested 任务的时间安排。",
			string(hybridJSON), constraintsText,
		)),
	}

	// ── ReAct 主循环 ──
	for st.ReactRound < st.ReactMaxRound {
		st.ReactRound++
		emitStage("schedule_plan.react.round", fmt.Sprintf("第 %d 轮优化思考...", st.ReactRound))

		// 1. 带超时的 context
		roundCtx, cancel := context.WithTimeout(ctx, reactRoundTimeout)

		// 2. 调用模型（流式 + 深度思考）
		content, streamErr := streamReactRound(roundCtx, chatModel, modelName, messages, outChan)
		cancel()

		if streamErr != nil {
			emitStage("schedule_plan.react.error", fmt.Sprintf("第 %d 轮模型调用失败: %s", st.ReactRound, streamErr.Error()))
			// 明确标记为失败，不伪装成功
			st.ReactDone = true
			st.ReactSummary = fmt.Sprintf("排程优化未完成：第 %d 轮模型调用超时或失败，使用粗排结果。", st.ReactRound)
			break
		}

		// 3. 解析 LLM 输出
		parsed, parseErr := parseReactLLMOutput(content)
		if parseErr != nil {
			// 解析失败，把原始输出当作摘要，结束循环
			emitStage("schedule_plan.react.parse_error", "LLM 输出格式异常，结束优化。")
			st.ReactSummary = "排程优化已完成（LLM 输出格式异常，使用当前结果）。"
			st.ReactDone = true
			break
		}

		// 4. 检查是否完成
		if parsed.Done {
			st.ReactSummary = parsed.Summary
			st.ReactDone = true
			emitStage("schedule_plan.react.done", "优化完成。")
			break
		}

		// 5. 执行 tool calls
		if len(parsed.ToolCalls) == 0 {
			// 没有 tool 调用也没有 done，视为完成
			st.ReactSummary = "排程优化已完成。"
			st.ReactDone = true
			break
		}

		results := make([]reactToolResult, 0, len(parsed.ToolCalls))
		for _, call := range parsed.ToolCalls {
			var result reactToolResult
			st.HybridEntries, result = dispatchReactTool(st.HybridEntries, call)
			results = append(results, result)
			statusMark := "OK"
			if !result.Success {
				statusMark = "FAIL"
			}
			emitStage("schedule_plan.react.tool_call",
				fmt.Sprintf("[%s] %s: %s", statusMark, result.Tool, result.Result))
		}

		// 6. 将 tool 结果拼入下一轮 messages
		// 先追加 assistant 的输出
		messages = append(messages, schema.AssistantMessage(content, nil))
		// 再追加 tool 结果作为 user message
		resultsJSON, _ := json.Marshal(results)
		messages = append(messages, schema.UserMessage(
			fmt.Sprintf("工具执行结果：\n%s\n\n请继续优化，或输出 {\"done\":true,\"summary\":\"...\"} 完成。", string(resultsJSON)),
		))
	}

	// 循环结束兜底
	if !st.ReactDone {
		st.ReactDone = true
		if strings.TrimSpace(st.ReactSummary) == "" {
			st.ReactSummary = fmt.Sprintf("排程优化已达最大轮次（%d 轮），使用当前结果。", st.ReactRound)
		}
		emitStage("schedule_plan.react.max_round", "已达最大优化轮次，使用当前结果。")
	}

	return st, nil
}

// streamReactRound 执行单轮 ReAct 模型调用：
// - 流式推送 reasoning_content 到 outChan（前端可见思考过程）
// - 累积 content 并返回（包含 tool_calls 或 done 信号）
func streamReactRound(
	ctx context.Context,
	chatModel *ark.ChatModel,
	modelName string,
	messages []*schema.Message,
	outChan chan<- string,
) (string, error) {
	// 开启深度思考
	reader, err := chatModel.Stream(ctx, messages,
		ark.WithThinking(&arkModel.Thinking{Type: arkModel.ThinkingTypeEnabled}),
	)
	if err != nil {
		return "", fmt.Errorf("模型 Stream 调用失败: %w", err)
	}
	defer reader.Close()

	requestID := "react-" + fmt.Sprintf("%d", time.Now().UnixMilli())
	created := time.Now().Unix()
	var contentBuilder strings.Builder

	for {
		chunk, recvErr := reader.Recv()
		if recvErr == io.EOF {
			break
		}
		if recvErr != nil {
			return contentBuilder.String(), fmt.Errorf("流式接收失败: %w", recvErr)
		}
		if chunk == nil {
			continue
		}

		// 推送 reasoning_content 到前端（实时思考过程）
		if chunk.ReasoningContent != "" && outChan != nil {
			payload, fmtErr := chat.ToOpenAIStream(
				&schema.Message{ReasoningContent: chunk.ReasoningContent},
				requestID, modelName, created, false,
			)
			if fmtErr == nil && payload != "" {
				outChan <- payload
			}
		}

		// 累积 content（tool_calls 或 done 信号）
		if chunk.Content != "" {
			contentBuilder.WriteString(chunk.Content)
		}
	}

	return strings.TrimSpace(contentBuilder.String()), nil
}
