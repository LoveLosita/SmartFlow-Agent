package newagentnode

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"regexp"
	"strings"
	"time"

	newagentllm "github.com/LoveLosita/smartflow/backend/newAgent/llm"
	newagentmodel "github.com/LoveLosita/smartflow/backend/newAgent/model"
	newagentprompt "github.com/LoveLosita/smartflow/backend/newAgent/prompt"
	newagentstream "github.com/LoveLosita/smartflow/backend/newAgent/stream"
	newagenttools "github.com/LoveLosita/smartflow/backend/newAgent/tools"
	"github.com/cloudwego/eino/schema"
	"github.com/google/uuid"
)

const (
	executeStageName     = "execute"
	executeStatusBlockID = "execute.status"
	executeSpeakBlockID  = "execute.speak"
	executePinnedKey     = "execution_context"
	toolMinContextSwitch = "min_context_switch"

	// maxConsecutiveCorrections 是 Execute 节点连续修正次数上限。
	// 超过此阈值后终止执行，防止 LLM 陷入无限修正循环。
	// 适用场景：JSON 解析失败、决策不合法、goal_check 为空、工具名不存在。
	maxConsecutiveCorrections = 3
)

// ExecuteNodeInput 描述执行节点单轮运行所需的最小依赖。
//
// 职责边界：
// 1. 只承载"本轮执行"需要的输入，不负责持久化；
// 2. RuntimeState 提供 plan 步骤与轮次预算；
// 3. ConversationContext 提供历史对话与置顶上下文；
// 4. ToolRegistry 提供工具注册表；
// 5. ScheduleState 提供工具操作的内存数据源（可为 nil，由调用方按需加载）；
// 6. SchedulePersistor 仍保留注入位，但当前阶段不调用，避免写库；
// 7. OriginalScheduleState 继续保留，供 Redis 快照恢复时维持“当前态/原始态”成对语义。
type ExecuteNodeInput struct {
	RuntimeState          *newagentmodel.AgentRuntimeState
	ConversationContext   *newagentmodel.ConversationContext
	UserInput             string
	Client                *newagentllm.Client
	ChunkEmitter          *newagentstream.ChunkEmitter
	ResumeNode            string
	ToolRegistry          *newagenttools.ToolRegistry
	ScheduleState         *newagenttools.ScheduleState
	SchedulePersistor     newagentmodel.SchedulePersistor
	OriginalScheduleState *newagenttools.ScheduleState
	AlwaysExecute         bool // true 时写工具跳过确认闸门直接执行
}

// ExecuteRoundObservation 记录执行阶段每轮的关键观察。
//
// 设计说明：
// 1. 参考 coding agent 模式，后端只记录事实，不做语义校验；
// 2. ToolResult 存储工具调用的原始返回，供 LLM 下一轮决策；
// 3. 该结构后续可扩展用于调试、回放、审计。
type ExecuteRoundObservation struct {
	Round       int    `json:"round"`
	StepIndex   int    `json:"step_index"`
	GoalCheck   string `json:"goal_check,omitempty"`
	Decision    string `json:"decision,omitempty"`
	ToolName    string `json:"tool_name,omitempty"`
	ToolParams  string `json:"tool_params,omitempty"`
	ToolSuccess bool   `json:"tool_success"`
	ToolResult  string `json:"tool_result,omitempty"`
}

// RunExecuteNode 执行一轮执行节点逻辑。
//
// 核心设计原则：
// 1. LLM 主导：LLM 自己判断 done_when 是否满足，自己决定何时推进/完成；
// 2. 后端兜底：只做资源控制（轮次预算）、安全兜底（防无限循环）、证据记录；
// 3. 不做硬校验：后端不质疑 LLM 的 advance/complete 决策，信任 LLM 判断。
//
// 步骤说明：
//  1. 校验最小依赖，推送"正在执行"状态，避免用户空等；
//  2. 检查当前是否有可执行的 plan 步骤，无计划则报错；
//  3. 构造执行阶段 prompt，调用 LLM 获取决策；
//  4. 若 LLM 先对用户说话，则伪流式推送并写回历史；
//  5. 按 LLM 决策执行动作：
//     5.1 call_tool：执行工具调用，记录证据，推进轮次；
//     5.2 ask_user：打开追问交互，等待用户回复；
//     5.3 advance：LLM 判定当前步骤完成，推进到下一步；
//     5.4 complete：LLM 判定整个任务完成，进入交付阶段；
//  6. 安全兜底：轮次耗尽时强制进入交付，避免无限循环。
func RunExecuteNode(ctx context.Context, input ExecuteNodeInput) error {
	// 1. 校验依赖并准备运行态。
	runtimeState, conversationContext, emitter, err := prepareExecuteNodeInput(input)
	if err != nil {
		return err
	}
	flowState := runtimeState.EnsureCommonState()

	// 1.5. 确认执行分支：如果用户已确认写操作，直接执行工具。
	if runtimeState.PendingConfirmTool != nil {
		return executePendingTool(ctx, runtimeState, conversationContext, input.ToolRegistry, input.ScheduleState, input.SchedulePersistor, input.OriginalScheduleState, emitter)
	}

	// 1.6. 顺序守卫基线初始化：
	// 1) 仅在未授权打乱顺序时记录 suggested 顺序基线；
	// 2) 只在基线为空时初始化，避免执行循环中反复覆盖；
	// 3) 后续由 order_guard 节点基于该基线做相对顺序校验。
	if !flowState.AllowReorder && len(flowState.SuggestedOrderBaseline) == 0 {
		flowState.SuggestedOrderBaseline = buildSuggestedOrderSnapshot(input.ScheduleState)
	}

	// 2. 推送执行阶段状态，让前端知道当前进度。
	if flowState.HasCurrentPlanStep() {
		// 有 plan：显示步骤进度。
		current, total := flowState.PlanProgress()
		currentStep, _ := flowState.CurrentPlanStep()
		if err := emitter.EmitStatus(
			executeStatusBlockID,
			executeStageName,
			"executing",
			fmt.Sprintf("正在执行第 %d/%d 步：%s", current, total, truncateText(currentStep.Content, 60)),
			false,
		); err != nil {
			return fmt.Errorf("执行阶段状态推送失败: %w", err)
		}
	} else {
		// 无 plan：纯 ReAct 模式。
		if err := emitter.EmitStatus(
			executeStatusBlockID,
			executeStageName,
			"executing",
			"正在处理你的请求...",
			false,
		); err != nil {
			return fmt.Errorf("执行阶段状态推送失败: %w", err)
		}
	}

	// 4. 消耗一轮预算，并检查是否耗尽。
	if !flowState.NextRound() {
		// 1. 轮次耗尽属于安全边界触发的被动停止，不应伪装成“正常完成”。
		// 2. 这里统一写入 exhausted 终止结果，让 deliver 阶段按未完成收口。
		// 3. 后续 graph 只需围绕 CommonState 的终止结果路由，无需再猜测原因。
		flowState.Exhaust(
			executeStageName,
			"本轮执行已达到安全轮次上限，当前先停止继续操作。如需继续，我可以在你确认后接着处理剩余步骤。",
			"execute rounds exhausted before task completion",
		)
		return nil
	}

	// 5. 构造本轮执行输入，请求 LLM 输出 ExecuteDecision。
	messages := newagentprompt.BuildExecuteMessages(flowState, conversationContext)
	log.Printf(
		"[DEBUG] execute LLM context begin chat=%s round=%d message_count=%d\n%s\n[DEBUG] execute LLM context end chat=%s round=%d",
		flowState.ConversationID,
		flowState.RoundUsed,
		len(messages),
		formatExecuteLLMMessagesForDebug(messages),
		flowState.ConversationID,
		flowState.RoundUsed,
	)
	decision, rawResult, err := newagentllm.GenerateJSON[newagentmodel.ExecuteDecision](
		ctx,
		input.Client,
		messages,
		newagentllm.GenerateOptions{
			Temperature: 1.0,   // thinking 模式强制要求 temperature=1
			MaxTokens:   16000, // 需为 thinking chain 留出足够预算
			Thinking:    newagentllm.ThinkingModeEnabled,
			Metadata: map[string]any{
				"stage":      executeStageName,
				"step_index": flowState.CurrentStep,
				"round_used": flowState.RoundUsed,
			},
		},
	)
	// 提前捕获原始文本，用于日志和 correction。
	rawText := ""
	if rawResult != nil {
		rawText = strings.TrimSpace(rawResult.Text)
	}

	if err != nil {
		if rawText != "" {
			log.Printf("[DEBUG] execute LLM 输出解析失败 chat=%s round=%d raw=%s",
				flowState.ConversationID, flowState.RoundUsed, rawText)
			flowState.ConsecutiveCorrections++
			if flowState.ConsecutiveCorrections >= maxConsecutiveCorrections {
				return fmt.Errorf("连续 %d 次输出非 JSON，终止执行: 原始输出=%s",
					flowState.ConsecutiveCorrections, rawText)
			}
			// 区分两种常见失败：
			// 1. tool_call 是数组（LLM 想批量调工具）→ 告知只能单次调用，保留已有上下文；
			// 2. 真正的 JSON 格式损坏 → 要求重新输出合法 JSON。
			var errorDesc, optionHint string
			if strings.Contains(rawText, `"tool_call": [`) || strings.Contains(rawText, `"tool_call":[`) {
				errorDesc = "你在 tool_call 字段传入了数组，但每轮只能调用一个工具，不支持批量格式。"
				optionHint = "请把多个工具调用拆开，每轮只调一个，拿到结果后再继续下一步。示例：{\"speak\":\"...\",\"action\":\"continue\",\"reason\":\"...\",\"tool_call\":{\"name\":\"get_task_info\",\"arguments\":{\"task_id\":1}}}"
			} else {
				errorDesc = "你的输出不是合法 JSON，无法解析。"
				optionHint = "你必须输出严格的 JSON 格式。合法格式示例：{\"speak\":\"...\",\"action\":\"continue\",\"reason\":\"...\",\"tool_call\":{\"name\":\"工具名\",\"arguments\":{}}}"
			}
			AppendLLMCorrectionWithHint(conversationContext, rawText, errorDesc, optionHint)
			return nil
		}

		// 模型返回空文本（常见原因：上下文过长、模型异常），走 correction 重试而非直接 fatal。
		if strings.Contains(err.Error(), "empty text") {
			log.Printf("[WARN] execute LLM 返回空文本 chat=%s round=%d consecutive=%d/%d",
				flowState.ConversationID, flowState.RoundUsed,
				flowState.ConsecutiveCorrections+1, maxConsecutiveCorrections)
			flowState.ConsecutiveCorrections++
			if flowState.ConsecutiveCorrections >= maxConsecutiveCorrections {
				return fmt.Errorf("连续 %d 次模型返回空文本，终止执行", flowState.ConsecutiveCorrections)
			}
			AppendLLMCorrectionWithHint(
				conversationContext,
				"",
				"模型没有返回任何内容。",
				"请重新输出合法 JSON 格式的执行决策。",
			)
			return nil
		}

		return fmt.Errorf("执行阶段模型调用失败: %w", err)
	}

	// 调试日志：输出 LLM 原始返回和解析后的决策，方便排查。
	log.Printf("[DEBUG] execute LLM 响应 chat=%s round=%d action=%s speak_len=%d raw_len=%d raw_preview=%.200s",
		flowState.ConversationID, flowState.RoundUsed,
		decision.Action, len(decision.Speak), len(rawText), rawText)

	if err := decision.Validate(); err != nil {
		flowState.ConsecutiveCorrections++
		log.Printf("[WARN] execute 决策不合法 chat=%s round=%d consecutive=%d/%d err=%s",
			flowState.ConversationID, flowState.RoundUsed,
			flowState.ConsecutiveCorrections, maxConsecutiveCorrections, err.Error())
		if flowState.ConsecutiveCorrections >= maxConsecutiveCorrections {
			return fmt.Errorf("连续 %d 次决策不合法，终止执行: %s (原始输出: %s)",
				flowState.ConsecutiveCorrections, err.Error(), rawText)
		}
		// 给 LLM 修正机会。
		AppendLLMCorrectionWithHint(
			conversationContext,
			rawText,
			fmt.Sprintf("你的执行决策不合法：%s", err.Error()),
			"合法的 action 包括：continue（继续当前步骤）、ask_user（追问用户）、confirm（写操作确认）、next_plan（推进到下一步）、done（任务完成）、abort（正式终止本轮流程）。",
		)
		return nil
	}

	// 决策合法，重置连续修正计数。
	flowState.ConsecutiveCorrections = 0

	// speak 兜底：continue / ask_user / confirm 三类动作对前端可读文案是强依赖。
	// 若模型漏填 speak，这里回退到 reason 或默认短句，避免前端出现“静默一轮”。
	decision.Speak = buildExecuteSpeakWithFallback(decision)

	// speak 后处理：补列表序号换行 + 末尾加 \n 防止连续 speak 在前端粘连。
	decision.Speak = normalizeSpeak(decision.Speak) // 末尾已含 \n

	// 自省校验：next_plan / done 必须附带 goal_check，否则不推进，追加修正让 LLM 重试。
	if decision.Action == newagentmodel.ExecuteActionNextPlan ||
		decision.Action == newagentmodel.ExecuteActionDone {
		if strings.TrimSpace(decision.GoalCheck) == "" {
			flowState.ConsecutiveCorrections++
			if flowState.ConsecutiveCorrections >= maxConsecutiveCorrections {
				return fmt.Errorf("连续 %d 次 goal_check 为空，终止执行", flowState.ConsecutiveCorrections)
			}
			// hint 区分有 plan / ReAct 两种模式：
			// - 有 plan：要求对照 done_when 逐条验证；
			// - ReAct：没有 done_when，只要求总结完成事实。
			var goalCheckHint string
			if flowState.HasPlan() {
				goalCheckHint = fmt.Sprintf("输出 %s 时，必须在 goal_check 中对照 done_when 逐条说明完成依据。", decision.Action)
			} else {
				goalCheckHint = fmt.Sprintf("输出 %s 时，必须在 goal_check 中总结任务已完成的事实证据（调用了哪些工具、得到了什么结果）。", decision.Action)
			}
			AppendLLMCorrectionWithHint(
				conversationContext,
				decision.Speak,
				fmt.Sprintf("你输出了 action=%s，但 goal_check 为空。", decision.Action),
				goalCheckHint,
			)
			return nil
		}
	}

	// 6. speak 推流与历史写入。
	//
	// AlwaysExecute=true 时，confirm 动作不走确认卡片，speak 和 continue 一样直接推流；
	// AlwaysExecute=false 时，confirm 的 speak 不推流（由确认卡片展示），但仍写入历史，
	// 防止 LLM 下一轮忘记自己的计划，形成重复确认循环。
	speakText := decision.Speak // 已由 normalizeSpeak 处理，末尾含 \n
	if speakText != "" {
		isConfirmWithCard := decision.Action == newagentmodel.ExecuteActionConfirm && !input.AlwaysExecute
		isAskUser := decision.Action == newagentmodel.ExecuteActionAskUser
		isAbort := decision.Action == newagentmodel.ExecuteActionAbort

		if !isConfirmWithCard && !isAskUser && !isAbort {
			// 推流给前端
			if err := emitter.EmitPseudoAssistantText(
				ctx,
				executeSpeakBlockID,
				executeStageName,
				speakText,
				newagentstream.DefaultPseudoStreamOptions(),
			); err != nil {
				return fmt.Errorf("执行文案推送失败: %w", err)
			}
		}
		// 1. confirm / ask_user 的 speak 仍要写入历史，避免下一轮 LLM 丢失自己的执行上下文。
		// 2. abort 不在这里写历史，避免先输出中间 speak，再在 deliver 收到第二份终止文案。
		// 3. ask_user 只是不在这里伪流式推送，真正的对外展示仍由 PendingInteraction.DisplayText 承担。
		if !isAbort {
			conversationContext.AppendHistory(&schema.Message{
				Role:    schema.Assistant,
				Content: speakText,
			})
		}
	}

	// 7. 按 LLM 决策执行动作，后端信任 LLM 判断，不做语义校验。
	switch decision.Action {
	case newagentmodel.ExecuteActionContinue:
		// 继续当前步骤的 ReAct 循环。
		// 若有工具调用意图，则执行工具并记录证据。
		if decision.ToolCall != nil {
			return executeToolCall(ctx, flowState, conversationContext, decision.ToolCall, emitter, input.ToolRegistry, input.ScheduleState)
		}
		// 无工具调用且 speak 为空（speak 非空时已在步骤 6 写入历史）。
		// 若 history 本轮完全没有更新，下一轮 LLM 会收到完全相同的上下文，容易死循环。
		// 把 reason 写入历史，保证上下文向前推进。
		if strings.TrimSpace(decision.Speak) == "" && strings.TrimSpace(decision.Reason) != "" {
			conversationContext.AppendHistory(&schema.Message{
				Role:    schema.Assistant,
				Content: decision.Reason,
			})
		}
		return nil

	case newagentmodel.ExecuteActionAskUser:
		// LLM 判定缺少关键信息，打开追问交互。
		question := resolveExecuteAskUserText(decision)
		runtimeState.OpenAskUserInteraction(uuid.NewString(), question, strings.TrimSpace(input.ResumeNode))
		return nil

	case newagentmodel.ExecuteActionConfirm:
		// AlwaysExecute=true：跳过确认闸门，直接执行内存写工具，不走 confirm 节点。
		if input.AlwaysExecute && decision.ToolCall != nil {
			return executeToolCall(ctx, flowState, conversationContext, decision.ToolCall, emitter, input.ToolRegistry, input.ScheduleState)
		}
		// AlwaysExecute=false（默认）：暂存工具意图，设 Phase → 下游 confirm 节点接管。
		return handleExecuteActionConfirm(decision, runtimeState, flowState)

	case newagentmodel.ExecuteActionNextPlan:
		// LLM 判定当前步骤已完成，推进到下一步。
		// 后端信任 LLM 判断，不做硬校验。
		if !flowState.AdvanceStep() {
			// 所有步骤已完成，进入交付阶段。
			flowState.Done()
		}
		return nil

	case newagentmodel.ExecuteActionDone:
		// LLM 判定整个任务已完成，直接进入交付阶段。
		// 后端信任 LLM 判断，不做硬校验。
		flowState.Done()
		return nil

	case newagentmodel.ExecuteActionAbort:
		// 1. abort 是 execute 层的正式终止协议。
		// 2. 这里只负责把终止结果写入 CommonState，真正的用户收口统一交给 deliver。
		// 3. 这样 rough_build / execute / 后续其他 stop 条件都能走同一套图内收口。
		return handleExecuteActionAbort(decision, flowState)

	default:
		// 1. LLM 输出了不支持的 action，不应直接报错终止，而应给它修正机会。
		// 2. 使用通用修正函数追加错误反馈，让 Graph 继续循环。
		// 3. LLM 下一轮会看到错误反馈并修正自己的输出。
		llmOutput := decision.Speak
		if strings.TrimSpace(llmOutput) == "" {
			llmOutput = decision.Reason
		}
		AppendLLMCorrectionWithHint(
			conversationContext,
			llmOutput,
			fmt.Sprintf("你输出的 action \"%s\" 不是合法的执行动作。", decision.Action),
			"合法的 action 包括：continue（继续当前步骤）、ask_user（追问用户）、confirm（写操作确认）、next_plan（推进到下一步）、done（任务完成）、abort（正式终止本轮流程）。",
		)
		return nil
	}
}

// prepareExecuteNodeInput 校验并准备执行节点的运行态依赖。
//
// 职责边界：
// 1. 校验必要依赖是否注入；
// 2. 为空依赖提供兜底值，避免空指针；
// 3. 不负责持久化，不负责业务逻辑。
func prepareExecuteNodeInput(input ExecuteNodeInput) (*newagentmodel.AgentRuntimeState, *newagentmodel.ConversationContext, *newagentstream.ChunkEmitter, error) {
	if input.RuntimeState == nil {
		return nil, nil, nil, fmt.Errorf("execute node: runtime state 不能为空")
	}
	if input.Client == nil {
		return nil, nil, nil, fmt.Errorf("execute node: execute client 未注入")
	}

	input.RuntimeState.EnsureCommonState()
	if input.ConversationContext == nil {
		input.ConversationContext = newagentmodel.NewConversationContext("")
	}
	if input.ChunkEmitter == nil {
		input.ChunkEmitter = newagentstream.NewChunkEmitter(newagentstream.NoopPayloadEmitter(), "", "", time.Now().Unix())
	}
	return input.RuntimeState, input.ConversationContext, input.ChunkEmitter, nil
}

// resolveExecuteAskUserText 解析追问用户的文案。
//
// 优先级：
// 1. 优先使用 LLM 输出的 speak；
// 2. 其次使用 reason；
// 3. 最后使用默认文案。
func resolveExecuteAskUserText(decision *newagentmodel.ExecuteDecision) string {
	if decision == nil {
		return "执行过程中遇到不确定的情况，需要向你确认。"
	}
	if strings.TrimSpace(decision.Speak) != "" {
		return strings.TrimSpace(decision.Speak)
	}
	if strings.TrimSpace(decision.Reason) != "" {
		return strings.TrimSpace(decision.Reason)
	}
	return "执行过程中遇到不确定的情况，需要向你确认。"
}

// buildExecuteSpeakWithFallback 统一为需要面向用户展示的动作补齐 speak 文案。
//
// 规则：
// 1. continue / ask_user / confirm 缺 speak 时，优先回退到 reason；
// 2. 若 reason 也为空，再按动作使用最短默认文案；
// 3. next_plan / done / abort 不强制补 speak，避免影响终止与收口语义。
func buildExecuteSpeakWithFallback(decision *newagentmodel.ExecuteDecision) string {
	if decision == nil {
		return ""
	}

	speak := strings.TrimSpace(decision.Speak)
	if speak != "" {
		return speak
	}

	switch decision.Action {
	case newagentmodel.ExecuteActionContinue,
		newagentmodel.ExecuteActionAskUser,
		newagentmodel.ExecuteActionConfirm:
		if reason := strings.TrimSpace(decision.Reason); reason != "" {
			return reason
		}
		switch decision.Action {
		case newagentmodel.ExecuteActionAskUser:
			return "我还缺少一条关键信息，想先向你确认。"
		case newagentmodel.ExecuteActionConfirm:
			return "我先整理好这一步操作，等待你的确认。"
		default:
			return "我先继续这一步处理，马上给你结果。"
		}
	default:
		return speak
	}
}

// handleExecuteActionConfirm 处理 LLM 申报的写操作确认请求。
//
// 步骤：
// 1. 把 ToolCallIntent 转成 PendingToolCallSnapshot 暂存到运行态；
// 2. 设 Phase = PhaseWaitingConfirm，让下游 confirm 节点接管；
// 3. 不执行工具，也不生成确认事件 — 这些都是 confirm 节点的职责。
func handleExecuteActionConfirm(
	decision *newagentmodel.ExecuteDecision,
	runtimeState *newagentmodel.AgentRuntimeState,
	flowState *newagentmodel.CommonState,
) error {
	toolCall := decision.ToolCall

	// 序列化工具参数。
	argsJSON := ""
	if toolCall.Arguments != nil {
		if raw, err := json.Marshal(toolCall.Arguments); err == nil {
			argsJSON = string(raw)
		}
	}

	// 暂存到运行态邮箱，confirm 节点会读出来。
	runtimeState.PendingConfirmTool = &newagentmodel.PendingToolCallSnapshot{
		ToolName: toolCall.Name,
		ArgsJSON: argsJSON,
		Summary:  strings.TrimSpace(decision.Speak),
	}

	// 设 Phase，让 branchAfterExecute 路由到 confirm 节点。
	flowState.Phase = newagentmodel.PhaseWaitingConfirm
	return nil
}

// handleExecuteActionAbort 处理 execute 阶段声明的正式终止请求。
//
// 职责边界：
// 1. 这里只负责把 abort 协议落到 CommonState；
// 2. 不直接向用户发最终文案，避免和 deliver 收口重复；
// 3. 若模型未提供 internal_reason，则回退到 decision.Reason 作为排查信息。
func handleExecuteActionAbort(
	decision *newagentmodel.ExecuteDecision,
	flowState *newagentmodel.CommonState,
) error {
	if decision == nil || decision.Abort == nil {
		return fmt.Errorf("abort 动作缺少终止信息")
	}
	if flowState == nil {
		return fmt.Errorf("abort 动作缺少流程状态")
	}

	internalReason := strings.TrimSpace(decision.Abort.InternalReason)
	if internalReason == "" {
		internalReason = strings.TrimSpace(decision.Reason)
	}

	flowState.Abort(
		executeStageName,
		decision.Abort.Code,
		decision.Abort.UserMessage,
		internalReason,
	)
	return nil
}

// executeToolCall 执行工具调用并记录证据。
//
// 职责边界：
// 1. 只负责执行工具调用，记录结果；
// 2. 不负责判断工具调用是否成功（由 LLM 下一轮判断）；
// 3. 不负责重试（由外层 Graph 循环控制）。
func executeToolCall(
	ctx context.Context,
	flowState *newagentmodel.CommonState,
	conversationContext *newagentmodel.ConversationContext,
	toolCall *newagentmodel.ToolCallIntent,
	emitter *newagentstream.ChunkEmitter,
	registry *newagenttools.ToolRegistry,
	scheduleState *newagenttools.ScheduleState,
) error {
	if toolCall == nil {
		return nil
	}

	toolName := strings.TrimSpace(toolCall.Name)
	if toolName == "" {
		return fmt.Errorf("工具调用缺少工具名称")
	}

	// 推送工具调用状态，让前端知道当前在做什么。
	if err := emitter.EmitStatus(
		executeStatusBlockID,
		executeStageName,
		"tool_call",
		fmt.Sprintf("正在调用工具：%s", toolName),
		false,
	); err != nil {
		return fmt.Errorf("工具调用状态推送失败: %w", err)
	}

	// 1. 校验依赖。
	if registry == nil {
		return fmt.Errorf("工具注册表未注入")
	}
	if scheduleState == nil {
		return fmt.Errorf("日程状态未加载，无法执行工具")
	}
	if !registry.HasTool(toolName) {
		// LLM 拼错或编造了工具名，走 correction 机制给重试机会，而非直接 fatal。
		// 与 action 不合法、决策校验失败等路径一致：追加错误反馈 → Graph 循环 → LLM 修正。
		flowState.ConsecutiveCorrections++
		if flowState.ConsecutiveCorrections >= maxConsecutiveCorrections {
			return fmt.Errorf("连续 %d 次调用未知工具，终止执行: %s（可用工具：%s）",
				flowState.ConsecutiveCorrections, toolName, strings.Join(registry.ToolNames(), "、"))
		}
		log.Printf("[WARN] execute 工具名不合法 chat=%s round=%d tool=%s consecutive=%d/%d available=%v",
			flowState.ConversationID, flowState.RoundUsed, toolName,
			flowState.ConsecutiveCorrections, maxConsecutiveCorrections, registry.ToolNames())
		AppendLLMCorrectionWithHint(
			conversationContext,
			"",
			fmt.Sprintf("你调用的工具 \"%s\" 不存在。", toolName),
			fmt.Sprintf("可用工具：%s。请检查拼写后重新输出。", strings.Join(registry.ToolNames(), "、")),
		)
		return nil
	}

	// 2. 执行工具。
	// 顺序护栏：未授权打乱顺序时，拒绝执行 min_context_switch，并写回工具观察结果。
	if shouldBlockMinContextSwitch(flowState, toolName) {
		blockedResult := "已拒绝执行 min_context_switch：当前未授权打乱顺序。如需使用该工具，请先由用户明确说明“允许打乱顺序”。"
		log.Printf(
			"[WARN] execute tool blocked chat=%s round=%d tool=%s allow_reorder=%v",
			flowState.ConversationID,
			flowState.RoundUsed,
			toolName,
			flowState.AllowReorder,
		)
		_ = emitter.EmitStatus(
			executeStatusBlockID,
			executeStageName,
			"tool_blocked",
			blockedResult,
			false,
		)

		toolCallID := uuid.NewString()
		argsJSON := "{}"
		if toolCall.Arguments != nil {
			if raw, marshalErr := json.Marshal(toolCall.Arguments); marshalErr == nil {
				argsJSON = string(raw)
			}
		}
		conversationContext.AppendHistory(&schema.Message{
			Role:    schema.Assistant,
			Content: "",
			ToolCalls: []schema.ToolCall{
				{
					ID:   toolCallID,
					Type: "function",
					Function: schema.FunctionCall{
						Name:      toolName,
						Arguments: argsJSON,
					},
				},
			},
		})
		conversationContext.AppendHistory(&schema.Message{
			Role:       schema.Tool,
			Content:    blockedResult,
			ToolCallID: toolCallID,
			ToolName:   toolName,
		})
		return nil
	}

	beforeDigest := summarizeScheduleStateForDebug(scheduleState)
	result := registry.Execute(scheduleState, toolName, toolCall.Arguments)
	afterDigest := summarizeScheduleStateForDebug(scheduleState)
	log.Printf(
		"[DEBUG] execute tool chat=%s round=%d tool=%s args=%s before=%s after=%s result_preview=%.200s",
		flowState.ConversationID,
		flowState.RoundUsed,
		toolName,
		marshalArgsForDebug(toolCall.Arguments),
		beforeDigest,
		afterDigest,
		flattenForLog(result),
	)

	// 3. 将工具调用和结果以合法的 assistant+tool 消息对追加到对话历史。
	//
	// 修复说明：
	// 旧实现直接追加裸 Tool 消息（无 ToolCallID、无前置 assistant tool_calls），
	// 违反 OpenAI 兼容 API 消息格式约束，导致 API 拒绝请求、连接断开。
	// 正确做法：先追加带 ToolCalls 的 assistant 消息，再追加带匹配 ToolCallID 的 tool 消息。
	toolCallID := uuid.NewString()

	argsJSON := "{}"
	if toolCall.Arguments != nil {
		if raw, err := json.Marshal(toolCall.Arguments); err == nil {
			argsJSON = string(raw)
		}
	}

	conversationContext.AppendHistory(&schema.Message{
		Role:    schema.Assistant,
		Content: "",
		ToolCalls: []schema.ToolCall{
			{
				ID:   toolCallID,
				Type: "function",
				Function: schema.FunctionCall{
					Name:      toolName,
					Arguments: argsJSON,
				},
			},
		},
	})

	conversationContext.AppendHistory(&schema.Message{
		Role:       schema.Tool,
		Content:    result,
		ToolCallID: toolCallID,
		ToolName:   toolName,
	})

	return nil
}

// shouldBlockMinContextSwitch 判断是否要拦截 min_context_switch 工具。
//
// 说明：
// 1. 仅当工具名为 min_context_switch 且未授权打乱顺序时返回 true；
// 2. 其余场景统一放行；
// 3. nil flowState 视为未命中拦截条件，避免因状态缺失导致误阻断。
func shouldBlockMinContextSwitch(flowState *newagentmodel.CommonState, toolName string) bool {
	if flowState == nil {
		return false
	}
	return !flowState.AllowReorder && strings.EqualFold(strings.TrimSpace(toolName), toolMinContextSwitch)
}

// executePendingTool 执行用户已确认的写工具。
//
// 职责边界：
// 1. 从 PendingConfirmTool 读取工具名和参数（已序列化）；
// 2. 反序列化参数后调用工具执行；
// 3. 将结果追加到历史，清空 PendingConfirmTool；
// 4. 当前阶段只保留内存修改，不在这里落库；
// 5. 不调用 LLM，直接返回让下一轮继续。
func executePendingTool(
	ctx context.Context,
	runtimeState *newagentmodel.AgentRuntimeState,
	conversationContext *newagentmodel.ConversationContext,
	registry *newagenttools.ToolRegistry,
	scheduleState *newagenttools.ScheduleState,
	persistor newagentmodel.SchedulePersistor,
	originalState *newagenttools.ScheduleState,
	emitter *newagentstream.ChunkEmitter,
) error {
	pending := runtimeState.PendingConfirmTool
	if pending == nil {
		return nil
	}

	// 1. 反序列化参数。
	var args map[string]any
	if err := json.Unmarshal([]byte(pending.ArgsJSON), &args); err != nil {
		return fmt.Errorf("解析工具参数失败: %w", err)
	}

	// 2. 推送状态。
	if err := emitter.EmitStatus(
		executeStatusBlockID,
		executeStageName,
		"tool_call",
		fmt.Sprintf("正在执行工具：%s", pending.ToolName),
		false,
	); err != nil {
		return fmt.Errorf("工具调用状态推送失败: %w", err)
	}

	// 3. 校验依赖：写工具必须持有有效的日程状态。
	if scheduleState == nil {
		return fmt.Errorf("日程状态未加载，无法执行已确认的写工具 %s", pending.ToolName)
	}

	// 4. 执行工具。
	beforeDigest := summarizeScheduleStateForDebug(scheduleState)
	result := registry.Execute(scheduleState, pending.ToolName, args)
	afterDigest := summarizeScheduleStateForDebug(scheduleState)
	flowState := runtimeState.EnsureCommonState()
	log.Printf(
		"[DEBUG] execute pending tool chat=%s round=%d tool=%s args=%s before=%s after=%s result_preview=%.200s",
		flowState.ConversationID,
		flowState.RoundUsed,
		pending.ToolName,
		marshalArgsForDebug(args),
		beforeDigest,
		afterDigest,
		flattenForLog(result),
	)

	// 5. 将工具调用和结果以合法的 assistant+tool 消息对追加到历史。
	//
	// 修复说明：同 executeToolCall，需要配对的 assistant+tool 消息。
	toolCallID := uuid.NewString()

	conversationContext.AppendHistory(&schema.Message{
		Role:    schema.Assistant,
		Content: "",
		ToolCalls: []schema.ToolCall{
			{
				ID:   toolCallID,
				Type: "function",
				Function: schema.FunctionCall{
					Name:      pending.ToolName,
					Arguments: pending.ArgsJSON,
				},
			},
		},
	})

	conversationContext.AppendHistory(&schema.Message{
		Role:       schema.Tool,
		Content:    result,
		ToolCallID: toolCallID,
		ToolName:   pending.ToolName,
	})

	// 6. 清空临时邮箱，避免重复执行。
	runtimeState.PendingConfirmTool = nil

	return nil
}

// listItemRe 匹配被粘连在一起的列表序号（如 "）2. " "水课3. "），用于自动补换行。
// 规则：非换行字符后紧跟 2-9 的序号（"2. " "3、" 等），说明 LLM 漏写了换行。
var listItemRe = regexp.MustCompile(`([^\n])([2-9][\.、]\s)`)

// normalizeSpeak 对 LLM 输出的 speak 做后处理：
// 1. 在列表序号（2. 3. …）前补 \n，防止列表项粘连；
// 2. 统一补尾部 \n，防止多轮 speak 推流时文字头尾粘连。
func normalizeSpeak(speak string) string {
	speak = strings.TrimSpace(speak)
	if speak == "" {
		return speak
	}
	if !strings.Contains(speak, "\n") {
		speak = listItemRe.ReplaceAllString(speak, "$1\n$2")
	}
	return speak + "\n"
}

// truncateText 截断文本到指定长度。
//
// 用于状态推送时避免超长文本影响前端展示。
func truncateText(text string, maxLen int) string {
	text = strings.TrimSpace(text)
	if len(text) <= maxLen {
		return text
	}
	if maxLen <= 3 {
		return text[:maxLen]
	}
	return text[:maxLen-3] + "..."
}

// summarizeScheduleStateForDebug 返回内存日程状态的关键计数，用于判断工具是否真的修改了 state。
func summarizeScheduleStateForDebug(state *newagenttools.ScheduleState) string {
	if state == nil {
		return "state=nil"
	}

	total := len(state.Tasks)
	pendingNoSlot := 0
	suggestedTotal := 0
	existingTotal := 0
	taskItemWithSlot := 0
	eventWithSlot := 0

	for i := range state.Tasks {
		t := &state.Tasks[i]
		hasSlot := len(t.Slots) > 0

		switch {
		case newagenttools.IsPendingTask(*t):
			pendingNoSlot++
		case newagenttools.IsSuggestedTask(*t):
			suggestedTotal++
		case newagenttools.IsExistingTask(*t):
			existingTotal++
		}

		if hasSlot {
			if t.Source == "task_item" {
				taskItemWithSlot++
			}
			if t.Source == "event" {
				eventWithSlot++
			}
		}
	}

	return fmt.Sprintf(
		"tasks=%d pending=%d suggested=%d existing=%d task_item_with_slot=%d event_with_slot=%d",
		total,
		pendingNoSlot,
		suggestedTotal,
		existingTotal,
		taskItemWithSlot,
		eventWithSlot,
	)
}

// marshalArgsForDebug 将工具参数序列化为日志可读的短文本。
func marshalArgsForDebug(args map[string]any) string {
	if len(args) == 0 {
		return "{}"
	}
	raw, err := json.Marshal(args)
	if err != nil {
		return "<marshal_error>"
	}
	return string(raw)
}

// flattenForLog 将多行文本压成单行，避免日志换行影响排查。
func flattenForLog(text string) string {
	text = strings.ReplaceAll(text, "\n", " ")
	text = strings.ReplaceAll(text, "\r", " ")
	return strings.TrimSpace(text)
}

// formatExecuteLLMMessagesForDebug 将本轮送入 LLM 的完整消息上下文展开成可读多行日志。
//
// 说明：
// 1. 按消息索引逐条输出，便于和上游上下文构造步骤逐项对齐；
// 2. 完整输出 content / reasoning_content / tool_calls / extra，不做截断；
// 3. 仅用于调试打点，不参与业务决策。
func formatExecuteLLMMessagesForDebug(messages []*schema.Message) string {
	if len(messages) == 0 {
		return "(empty messages)"
	}

	var sb strings.Builder
	for i, msg := range messages {
		sb.WriteString(fmt.Sprintf("----- message[%d] -----\n", i))
		if msg == nil {
			sb.WriteString("role: <nil>\n\n")
			continue
		}

		sb.WriteString(fmt.Sprintf("role: %s\n", msg.Role))

		if strings.TrimSpace(msg.ToolCallID) != "" {
			sb.WriteString(fmt.Sprintf("tool_call_id: %s\n", msg.ToolCallID))
		}
		if strings.TrimSpace(msg.ToolName) != "" {
			sb.WriteString(fmt.Sprintf("tool_name: %s\n", msg.ToolName))
		}

		if len(msg.ToolCalls) > 0 {
			sb.WriteString("tool_calls:\n")
			for j, call := range msg.ToolCalls {
				sb.WriteString(fmt.Sprintf("  - [%d] id=%s type=%s function=%s\n", j, call.ID, call.Type, call.Function.Name))
				sb.WriteString("    arguments:\n")
				sb.WriteString(indentMultilineForDebug(call.Function.Arguments, "      "))
				sb.WriteString("\n")
			}
		}

		if strings.TrimSpace(msg.ReasoningContent) != "" {
			sb.WriteString("reasoning_content:\n")
			sb.WriteString(indentMultilineForDebug(msg.ReasoningContent, "  "))
			sb.WriteString("\n")
		}

		sb.WriteString("content:\n")
		sb.WriteString(indentMultilineForDebug(msg.Content, "  "))
		sb.WriteString("\n")

		if len(msg.Extra) > 0 {
			sb.WriteString("extra:\n")
			raw, err := json.MarshalIndent(msg.Extra, "", "  ")
			if err != nil {
				sb.WriteString(indentMultilineForDebug("<marshal_error>", "  "))
			} else {
				sb.WriteString(indentMultilineForDebug(string(raw), "  "))
			}
			sb.WriteString("\n")
		}

		sb.WriteString("\n")
	}
	return sb.String()
}

// indentMultilineForDebug 为多行文本统一添加前缀缩进，避免日志折行后难以阅读。
func indentMultilineForDebug(text, prefix string) string {
	if text == "" {
		return prefix + "<empty>"
	}
	lines := strings.Split(text, "\n")
	for i := range lines {
		lines[i] = prefix + lines[i]
	}
	return strings.Join(lines, "\n")
}
