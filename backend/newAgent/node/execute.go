package newagentnode

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"regexp"
	"strconv"
	"strings"
	"time"

	infrallm "github.com/LoveLosita/smartflow/backend/infra/llm"
	newagentmodel "github.com/LoveLosita/smartflow/backend/newAgent/model"
	newagentprompt "github.com/LoveLosita/smartflow/backend/newAgent/prompt"
	newagentstream "github.com/LoveLosita/smartflow/backend/newAgent/stream"
	newagenttools "github.com/LoveLosita/smartflow/backend/newAgent/tools"
	"github.com/cloudwego/eino/schema"
	"github.com/google/uuid"
)

const (
	executeStageName               = "execute"
	executeStatusBlockID           = "execute.status"
	executeSpeakBlockID            = "execute.speak"
	executePinnedKey               = "execution_context"
	toolMinContextSwitch           = "min_context_switch"
	executeHistoryKindKey          = "newagent_history_kind"
	executeHistoryKindStepAdvanced = "execute_step_advanced"

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
	Client                *infrallm.Client
	ChunkEmitter          *newagentstream.ChunkEmitter
	ResumeNode            string
	ToolRegistry          *newagenttools.ToolRegistry
	ScheduleState         *newagenttools.ScheduleState
	SchedulePersistor     newagentmodel.SchedulePersistor
	WriteSchedulePreview  newagentmodel.WriteSchedulePreviewFunc
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
		return executePendingTool(
			ctx,
			runtimeState,
			conversationContext,
			input.ToolRegistry,
			input.ScheduleState,
			input.SchedulePersistor,
			input.OriginalScheduleState,
			input.WriteSchedulePreview,
			emitter,
		)
	}

	// 1.6. 顺序守卫基线初始化：
	// 1) 仅在未授权打乱顺序时记录 suggested 顺序基线；
	// 2) 只在基线为空时初始化，避免执行循环中反复覆盖；
	// 3) 后续由 order_guard 节点基于该基线做相对顺序校验。
	//
	// 同时在“本轮 execute 首轮”重置一次临时队列，避免上一轮残留队列污染新请求。
	// 判定依据：
	// 1. RoundUsed==0 说明当前还未消耗执行预算；
	// 2. 此时清理不会影响断线恢复中的中间进度（恢复场景通常 RoundUsed>0）。
	if input.ScheduleState != nil && flowState.RoundUsed == 0 {
		newagenttools.ResetTaskProcessingQueue(input.ScheduleState)
	}
	if !flowState.AllowReorder && len(flowState.SuggestedOrderBaseline) == 0 {
		flowState.SuggestedOrderBaseline = buildSuggestedOrderSnapshot(input.ScheduleState)
	}

	// 1. 每轮 execute 开始前先刷新一次执行锚点，避免 LLM 继续读取旧的当前步骤。
	// 2. 这里仅维护上下文一致性，不改变流程状态。
	syncExecutePinnedContext(conversationContext, flowState)

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
	decision, rawResult, err := infrallm.GenerateJSON[newagentmodel.ExecuteDecision](
		ctx,
		input.Client,
		messages,
		infrallm.GenerateOptions{
			Temperature: 1.0,   // thinking 模式强制要求 temperature=1
			MaxTokens:   16000, // 需为 thinking chain 留出足够预算
			Thinking:    infrallm.ThinkingModeEnabled,
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
			return executeToolCall(
				ctx,
				flowState,
				conversationContext,
				decision.ToolCall,
				emitter,
				input.ToolRegistry,
				input.ScheduleState,
				input.WriteSchedulePreview,
			)
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
			return executeToolCall(
				ctx,
				flowState,
				conversationContext,
				decision.ToolCall,
				emitter,
				input.ToolRegistry,
				input.ScheduleState,
				input.WriteSchedulePreview,
			)
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
		// 1. 写入“步骤推进完成”边界标记，把上一步骤 loop 从 msg2 挪入 msg1。
		// 2. 标记只作为 prompt 分层锚点，不参与业务语义判断。
		appendExecuteStepAdvancedMarker(conversationContext)
		// 1. next_plan 推进后立刻刷新 current_step / execution_context。
		// 2. 若计划已结束，这里会移除 current_step，避免下轮读取到旧步骤。
		syncExecutePinnedContext(conversationContext, flowState)
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
// syncExecutePinnedContext 同步 execute 阶段的置顶上下文。
//
// 步骤说明：
// 1. 每轮先刷新 execution_context，确保模型始终看到最新执行锚点。
// 2. 若当前仍在计划执行且 current_step 可读，则覆盖 current_step 置顶块。
// 3. 若计划已执行完或当前步骤不可读，则移除 current_step，避免模型误读旧步骤。
func syncExecutePinnedContext(
	conversationContext *newagentmodel.ConversationContext,
	flowState *newagentmodel.CommonState,
) {
	if conversationContext == nil || flowState == nil {
		return
	}

	execContent := buildExecuteContextPinnedMarkdown(flowState)
	if strings.TrimSpace(execContent) != "" {
		conversationContext.UpsertPinnedBlock(newagentmodel.ContextBlock{
			Key:     executePinnedKey,
			Title:   "执行上下文",
			Content: execContent,
		})
	}

	if !flowState.HasPlan() {
		conversationContext.RemovePinnedBlock(planCurrentStepKey)
		return
	}

	step, ok := flowState.CurrentPlanStep()
	if !ok {
		conversationContext.RemovePinnedBlock(planCurrentStepKey)
		return
	}

	current, total := flowState.PlanProgress()
	title := strings.TrimSpace(planCurrentStepTitle)
	if title == "" {
		title = "当前步骤"
	}
	conversationContext.UpsertPinnedBlock(newagentmodel.ContextBlock{
		Key:     planCurrentStepKey,
		Title:   title,
		Content: buildCurrentPlanStepPinnedMarkdown(step, current, total),
	})
}

// appendExecuteStepAdvancedMarker 在 history 中写入“步骤已推进”标记。
//
// 职责边界：
// 1. 仅写轻量 marker，供 prompt 侧把“上一步骤 loop”归档进 msg1；
// 2. 若末尾已是同类 marker，则幂等跳过；
// 3. 不负责裁剪历史、不负责摘要压缩。
func appendExecuteStepAdvancedMarker(conversationContext *newagentmodel.ConversationContext) {
	if conversationContext == nil {
		return
	}

	history := conversationContext.HistorySnapshot()
	if len(history) > 0 {
		last := history[len(history)-1]
		if last != nil && last.Extra != nil {
			if kind, ok := last.Extra[executeHistoryKindKey].(string); ok && strings.TrimSpace(kind) == executeHistoryKindStepAdvanced {
				return
			}
		}
	}

	conversationContext.AppendHistory(&schema.Message{
		Role:    schema.Assistant,
		Content: "",
		Extra: map[string]any{
			executeHistoryKindKey: executeHistoryKindStepAdvanced,
		},
	})
}

// buildExecuteContextPinnedMarkdown 构造 execute 节点给模型的执行锚点文本。
func buildExecuteContextPinnedMarkdown(flowState *newagentmodel.CommonState) string {
	if flowState == nil {
		return ""
	}

	lines := make([]string, 0, 8)
	if flowState.HasPlan() {
		lines = append(lines, "执行模式：计划执行（按步骤推进）")
		current, total := flowState.PlanProgress()
		lines = append(lines, fmt.Sprintf("计划进度：第 %d/%d 步", current, total))

		if step, ok := flowState.CurrentPlanStep(); ok {
			lines = append(lines, "当前步骤："+compactExecutePinnedText(step.Content))
			doneWhen := compactExecutePinnedText(step.DoneWhen)
			if doneWhen != "" {
				lines = append(lines, "完成判定(done_when)："+doneWhen)
			}
			lines = append(lines, "动作纪律：未满足 done_when 禁止 next_plan；满足后优先 next_plan。")
		} else {
			lines = append(lines, "当前步骤：不可读（可能已执行完成）")
		}
	} else {
		lines = append(lines, "执行模式：自由执行（无预定义步骤）")
	}

	if flowState.MaxRounds > 0 {
		lines = append(lines, fmt.Sprintf("轮次预算：%d/%d", flowState.RoundUsed, flowState.MaxRounds))
	}
	return strings.TrimSpace(strings.Join(lines, "\n"))
}

// buildCurrentPlanStepPinnedMarkdown 构造 current_step 置顶块内容。
func buildCurrentPlanStepPinnedMarkdown(step newagentmodel.PlanStep, current, total int) string {
	lines := make([]string, 0, 4)
	lines = append(lines, fmt.Sprintf("步骤进度：第 %d/%d 步", current, total))

	content := compactExecutePinnedText(step.Content)
	if content == "" {
		content = "（空）"
	}
	lines = append(lines, "步骤内容："+content)

	doneWhen := compactExecutePinnedText(step.DoneWhen)
	if doneWhen != "" {
		lines = append(lines, "完成判定："+doneWhen)
	}

	return strings.TrimSpace(strings.Join(lines, "\n"))
}

// compactExecutePinnedText 把多行文本压成单行，避免置顶块出现冗长换行噪音。
func compactExecutePinnedText(text string) string {
	text = strings.TrimSpace(text)
	if text == "" {
		return ""
	}
	text = strings.ReplaceAll(text, "\r\n", "\n")
	text = strings.ReplaceAll(text, "\n", "；")
	return strings.TrimSpace(text)
}

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

// executeStepScope 描述当前计划步骤提取出的“硬范围约束”。
//
// 约束语义：
// 1. WeekFrom/WeekTo：限制到指定周范围；
// 2. DayStart/DayEnd：限制到指定 day_index 范围；
// 3. DayOfWeekSet：限制到指定周几集合（1=周一 ... 7=周日）。
type executeStepScope struct {
	HasWeek  bool
	WeekFrom int
	WeekTo   int

	HasDay   bool
	DayStart int
	DayEnd   int

	DayOfWeekSet map[int]struct{}
}

var (
	executeScopeWeekRangeRe    = regexp.MustCompile(`第\s*(\d+)\s*(?:-|到|至|~)\s*(\d+)\s*周`)
	executeScopeWeekSingleRe   = regexp.MustCompile(`第\s*(\d+)\s*周`)
	executeScopeDayRangeReA    = regexp.MustCompile(`第\s*(\d+)\s*(?:-|到|至|~)\s*(\d+)\s*天`)
	executeScopeDayRangeReB    = regexp.MustCompile(`第\s*(\d+)\s*天\s*(?:-|到|至|~)\s*第?\s*(\d+)\s*天`)
	executeScopeDaySingleRe    = regexp.MustCompile(`第\s*(\d+)\s*天`)
	executeScopeWeekdayRangeRe = regexp.MustCompile(`周\s*([一二三四五六日天])\s*(?:-|到|至|~)\s*周?\s*([一二三四五六日天])`)
	executeScopeWeekdayRe      = regexp.MustCompile(`周\s*([一二三四五六日天])`)
)

// deriveExecuteStepScope 从当前步骤文本提取范围锚点。
//
// 提取优先级：
// 1. 优先识别“第X周 / 第X-Y周”；
// 2. 其次识别“周一到周五 / 工作日 / 周末”等周几约束；
// 3. 补充识别“第A-B天 / 第A天到第B天”。
func deriveExecuteStepScope(flowState *newagentmodel.CommonState) (*executeStepScope, bool) {
	if flowState == nil || !flowState.HasPlan() {
		return nil, false
	}
	step, ok := flowState.CurrentPlanStep()
	if !ok {
		return nil, false
	}

	text := strings.TrimSpace(step.Content + "\n" + step.DoneWhen)
	if text == "" {
		return nil, false
	}

	scope := &executeStepScope{
		DayOfWeekSet: make(map[int]struct{}, 7),
	}
	hit := false

	if match := executeScopeWeekRangeRe.FindStringSubmatch(text); len(match) == 3 {
		start, okStart := parseRegexInt(match[1])
		end, okEnd := parseRegexInt(match[2])
		if okStart && okEnd {
			if start > end {
				start, end = end, start
			}
			scope.HasWeek = true
			scope.WeekFrom = start
			scope.WeekTo = end
			hit = true
		}
	} else {
		if match := executeScopeWeekSingleRe.FindStringSubmatch(text); len(match) == 2 {
			week, okWeek := parseRegexInt(match[1])
			if okWeek {
				scope.HasWeek = true
				scope.WeekFrom = week
				scope.WeekTo = week
				hit = true
			}
		}
	}

	if rangeStart, rangeEnd, okRange := parseExecuteScopeDayRange(text); okRange {
		scope.HasDay = true
		scope.DayStart = rangeStart
		scope.DayEnd = rangeEnd
		hit = true
	} else {
		dayMatches := executeScopeDaySingleRe.FindAllStringSubmatch(text, -1)
		if len(dayMatches) == 1 && len(dayMatches[0]) == 2 {
			day, okDay := parseRegexInt(dayMatches[0][1])
			if okDay {
				scope.HasDay = true
				scope.DayStart = day
				scope.DayEnd = day
				hit = true
			}
		}
	}

	for dayOfWeek := range parseExecuteScopeWeekdays(text) {
		scope.DayOfWeekSet[dayOfWeek] = struct{}{}
		hit = true
	}
	if len(scope.DayOfWeekSet) == 0 {
		scope.DayOfWeekSet = nil
	}

	if !hit {
		return nil, false
	}
	return scope, true
}

func parseExecuteScopeDayRange(text string) (start int, end int, ok bool) {
	if match := executeScopeDayRangeReA.FindStringSubmatch(text); len(match) == 3 {
		startA, okA := parseRegexInt(match[1])
		endA, okB := parseRegexInt(match[2])
		if okA && okB {
			if startA > endA {
				startA, endA = endA, startA
			}
			return startA, endA, true
		}
	}
	if match := executeScopeDayRangeReB.FindStringSubmatch(text); len(match) == 3 {
		startB, okA := parseRegexInt(match[1])
		endB, okB := parseRegexInt(match[2])
		if okA && okB {
			if startB > endB {
				startB, endB = endB, startB
			}
			return startB, endB, true
		}
	}
	return 0, 0, false
}

func parseExecuteScopeWeekdays(text string) map[int]struct{} {
	result := make(map[int]struct{}, 7)
	compact := strings.TrimSpace(text)
	if compact == "" {
		return result
	}

	for _, match := range executeScopeWeekdayRangeRe.FindAllStringSubmatch(compact, -1) {
		if len(match) != 3 {
			continue
		}
		from, okFrom := normalizeChineseWeekday(match[1])
		to, okTo := normalizeChineseWeekday(match[2])
		if !okFrom || !okTo {
			continue
		}
		if from <= to {
			for day := from; day <= to; day++ {
				result[day] = struct{}{}
			}
			continue
		}
		for day := from; day <= 7; day++ {
			result[day] = struct{}{}
		}
		for day := 1; day <= to; day++ {
			result[day] = struct{}{}
		}
	}

	if len(result) == 0 {
		switch {
		case strings.Contains(compact, "工作日"):
			for day := 1; day <= 5; day++ {
				result[day] = struct{}{}
			}
		case strings.Contains(compact, "周末"):
			result[6] = struct{}{}
			result[7] = struct{}{}
		}
	}

	if len(result) == 0 {
		matches := executeScopeWeekdayRe.FindAllStringSubmatch(compact, -1)
		if len(matches) == 1 && len(matches[0]) == 2 {
			if day, ok := normalizeChineseWeekday(matches[0][1]); ok {
				result[day] = struct{}{}
			}
		}
	}
	return result
}

func normalizeChineseWeekday(raw string) (int, bool) {
	switch strings.TrimSpace(raw) {
	case "一":
		return 1, true
	case "二":
		return 2, true
	case "三":
		return 3, true
	case "四":
		return 4, true
	case "五":
		return 5, true
	case "六":
		return 6, true
	case "日", "天":
		return 7, true
	default:
		return 0, false
	}
}

func parseRegexInt(raw string) (int, bool) {
	value, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil {
		return 0, false
	}
	return value, true
}

func renderExecuteStepScope(scope *executeStepScope) string {
	if scope == nil {
		return "未设范围"
	}
	parts := make([]string, 0, 3)
	if scope.HasWeek {
		if scope.WeekFrom == scope.WeekTo {
			parts = append(parts, fmt.Sprintf("第%d周", scope.WeekFrom))
		} else {
			parts = append(parts, fmt.Sprintf("第%d-%d周", scope.WeekFrom, scope.WeekTo))
		}
	}
	if scope.HasDay {
		if scope.DayStart == scope.DayEnd {
			parts = append(parts, fmt.Sprintf("第%d天", scope.DayStart))
		} else {
			parts = append(parts, fmt.Sprintf("第%d-%d天", scope.DayStart, scope.DayEnd))
		}
	}
	if len(scope.DayOfWeekSet) > 0 {
		weekdays := make([]string, 0, 7)
		for _, day := range []int{1, 2, 3, 4, 5, 6, 7} {
			if _, ok := scope.DayOfWeekSet[day]; !ok {
				continue
			}
			weekdays = append(weekdays, fmt.Sprintf("周%d", day))
		}
		if len(weekdays) > 0 {
			parts = append(parts, strings.Join(weekdays, "/"))
		}
	}
	if len(parts) == 0 {
		return "未设范围"
	}
	return strings.Join(parts, "，")
}

func buildScopeDaySet(state *newagenttools.ScheduleState, scope *executeStepScope) map[int]struct{} {
	result := make(map[int]struct{}, 16)
	if state == nil || scope == nil {
		return result
	}
	for day := 1; day <= state.Window.TotalDays; day++ {
		if dayMatchesScope(state, scope, day) {
			result[day] = struct{}{}
		}
	}
	return result
}

func dayMatchesScope(state *newagenttools.ScheduleState, scope *executeStepScope, day int) bool {
	if state == nil || scope == nil {
		return true
	}
	if day < 1 || day > state.Window.TotalDays {
		return false
	}
	week, dayOfWeek, ok := state.DayToWeekDay(day)
	if !ok {
		return false
	}
	if scope.HasWeek && (week < scope.WeekFrom || week > scope.WeekTo) {
		return false
	}
	if scope.HasDay && (day < scope.DayStart || day > scope.DayEnd) {
		return false
	}
	if len(scope.DayOfWeekSet) > 0 {
		if _, matched := scope.DayOfWeekSet[dayOfWeek]; !matched {
			return false
		}
	}
	return true
}

func estimateCandidateDaysFromArgs(state *newagenttools.ScheduleState, args map[string]any) (map[int]struct{}, bool, error) {
	result := make(map[int]struct{}, 16)
	if state == nil {
		return result, false, fmt.Errorf("日程状态为空")
	}

	day, hasDay := readIntAnyFromMap(args, "day")
	dayStart, hasDayStart := readIntAnyFromMap(args, "day_start")
	dayEnd, hasDayEnd := readIntAnyFromMap(args, "day_end")
	if hasDay && (hasDayStart || hasDayEnd) {
		return nil, true, fmt.Errorf("day 与 day_start/day_end 不能同时传入")
	}

	if hasDay && (day < 1 || day > state.Window.TotalDays) {
		return nil, true, fmt.Errorf("day=%d 超出窗口范围(1-%d)", day, state.Window.TotalDays)
	}
	if hasDayStart && (dayStart < 1 || dayStart > state.Window.TotalDays) {
		return nil, true, fmt.Errorf("day_start=%d 超出窗口范围(1-%d)", dayStart, state.Window.TotalDays)
	}
	if hasDayEnd && (dayEnd < 1 || dayEnd > state.Window.TotalDays) {
		return nil, true, fmt.Errorf("day_end=%d 超出窗口范围(1-%d)", dayEnd, state.Window.TotalDays)
	}

	start := 1
	end := state.Window.TotalDays
	if hasDay {
		start, end = day, day
	} else {
		if hasDayStart {
			start = dayStart
		}
		if hasDayEnd {
			end = dayEnd
		}
	}
	if start > end {
		return nil, true, fmt.Errorf("day_start=%d 不能大于 day_end=%d", start, end)
	}

	week, hasWeek := readIntAnyFromMap(args, "week")
	weekFrom, hasWeekFrom := readIntAnyFromMap(args, "week_from")
	weekTo, hasWeekTo := readIntAnyFromMap(args, "week_to")
	if hasWeek {
		weekFrom, weekTo = week, week
		hasWeekFrom, hasWeekTo = true, true
	}
	if hasWeekFrom && hasWeekTo && weekFrom > weekTo {
		weekFrom, weekTo = weekTo, weekFrom
	}
	weekFilter := intSliceToSet(readIntSliceAnyFromMap(args, "week_filter"))

	dayOfWeekSet := intSliceToSet(readIntSliceAnyFromMap(args, "day_of_week"))
	dayScope := strings.ToLower(strings.TrimSpace(readStringAnyFromMap(args, "day_scope")))
	if dayScope == "" {
		dayScope = "all"
	}

	hasCalendarFilter := hasAnyCalendarArg(args)
	for dayIndex := start; dayIndex <= end; dayIndex++ {
		weekValue, dayOfWeek, ok := state.DayToWeekDay(dayIndex)
		if !ok {
			continue
		}
		if hasWeekFrom && weekValue < weekFrom {
			continue
		}
		if hasWeekTo && weekValue > weekTo {
			continue
		}
		if len(weekFilter) > 0 {
			if _, hit := weekFilter[weekValue]; !hit {
				continue
			}
		}
		if len(dayOfWeekSet) > 0 {
			if _, hit := dayOfWeekSet[dayOfWeek]; !hit {
				continue
			}
		} else if !matchDayScopeForGuard(dayOfWeek, dayScope) {
			continue
		}
		result[dayIndex] = struct{}{}
	}
	return result, hasCalendarFilter, nil
}

func matchDayScopeForGuard(dayOfWeek int, scope string) bool {
	switch scope {
	case "workday":
		return dayOfWeek >= 1 && dayOfWeek <= 5
	case "weekend":
		return dayOfWeek == 6 || dayOfWeek == 7
	default:
		return true
	}
}

func hasAnyCalendarArg(args map[string]any) bool {
	if len(args) == 0 {
		return false
	}
	keys := []string{"day", "day_start", "day_end", "week", "week_from", "week_to", "week_filter", "day_of_week", "day_scope"}
	for _, key := range keys {
		if _, exists := args[key]; exists {
			return true
		}
	}
	return false
}

func extractBatchMoveNewDays(args map[string]any) ([]int, error) {
	rawMoves, exists := args["moves"]
	if !exists {
		return nil, fmt.Errorf("缺少 moves")
	}
	list, ok := rawMoves.([]any)
	if !ok {
		return nil, fmt.Errorf("moves 不是数组")
	}

	result := make([]int, 0, len(list))
	for _, item := range list {
		moveMap, ok := item.(map[string]any)
		if !ok {
			continue
		}
		newDay, hasDay := readIntAnyFromMap(moveMap, "new_day")
		if !hasDay {
			continue
		}
		result = append(result, newDay)
	}
	if len(result) == 0 {
		return nil, fmt.Errorf("moves 未提供有效 new_day")
	}
	return result, nil
}

func intSliceToSet(values []int) map[int]struct{} {
	result := make(map[int]struct{}, len(values))
	for _, value := range values {
		result[value] = struct{}{}
	}
	return result
}

func readIntAnyFromMap(args map[string]any, keys ...string) (int, bool) {
	for _, key := range keys {
		if args == nil {
			continue
		}
		raw, exists := args[key]
		if !exists {
			continue
		}
		if value, ok := parseAnyToInt(raw); ok {
			return value, true
		}
	}
	return 0, false
}

func readIntSliceAnyFromMap(args map[string]any, keys ...string) []int {
	for _, key := range keys {
		if args == nil {
			continue
		}
		raw, exists := args[key]
		if !exists {
			continue
		}
		values := parseAnyToIntSlice(raw)
		if len(values) > 0 {
			return values
		}
	}
	return nil
}

func readStringAnyFromMap(args map[string]any, keys ...string) string {
	for _, key := range keys {
		if args == nil {
			continue
		}
		raw, exists := args[key]
		if !exists {
			continue
		}
		if text, ok := raw.(string); ok {
			return text
		}
	}
	return ""
}

func parseAnyToInt(value any) (int, bool) {
	switch v := value.(type) {
	case int:
		return v, true
	case int8:
		return int(v), true
	case int16:
		return int(v), true
	case int32:
		return int(v), true
	case int64:
		return int(v), true
	case float32:
		return int(v), true
	case float64:
		return int(v), true
	case json.Number:
		if iv, err := v.Int64(); err == nil {
			return int(iv), true
		}
		if fv, err := v.Float64(); err == nil {
			return int(fv), true
		}
	case string:
		text := strings.TrimSpace(v)
		if text == "" {
			return 0, false
		}
		iv, err := strconv.Atoi(text)
		if err == nil {
			return iv, true
		}
	}
	return 0, false
}

func parseAnyToIntSlice(value any) []int {
	switch values := value.(type) {
	case []int:
		result := make([]int, 0, len(values))
		for _, value := range values {
			result = append(result, value)
		}
		return result
	case []any:
		result := make([]int, 0, len(values))
		for _, item := range values {
			iv, ok := parseAnyToInt(item)
			if !ok {
				continue
			}
			result = append(result, iv)
		}
		return result
	default:
		return nil
	}
}

// appendToolCallResultHistory 统一把“assistant tool_call + tool observation”写回历史。
//
// 设计说明：
// 1. 采用标准配对消息格式，兼容 OpenAI tool_call 约束；
// 2. args 序列化失败时降级为 "{}"，保证消息结构完整；
// 3. 仅负责写历史，不负责工具执行或状态更新。
func appendToolCallResultHistory(
	conversationContext *newagentmodel.ConversationContext,
	toolName string,
	args map[string]any,
	result string,
) {
	if conversationContext == nil {
		return
	}

	argsJSON := "{}"
	if args != nil {
		if raw, err := json.Marshal(args); err == nil {
			argsJSON = string(raw)
		}
	}
	toolCallID := uuid.NewString()
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
	writePreview newagentmodel.WriteSchedulePreviewFunc,
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
		appendToolCallResultHistory(conversationContext, toolName, toolCall.Arguments, blockedResult)
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

	// 3. 以标准 assistant+tool 消息对写回历史，避免消息链断裂。
	appendToolCallResultHistory(conversationContext, toolName, toolCall.Arguments, result)

	// 4. 写工具实时预览：每次写工具执行后都尝试刷新 Redis 预览，确保前端可见“最新操作结果”。
	//
	// 步骤化说明：
	// 1. 仅写工具触发实时预览刷新，读工具不触发，避免无意义放大写流量；
	// 2. 这里采用“失败不阻断主流程”策略：预览写失败只记日志，不影响当前执行链路；
	// 3. Deliver 节点仍保留最终覆盖写，保证 order_guard/收口后的最终态一致。
	tryWritePreviewAfterWriteTool(ctx, flowState, scheduleState, registry, toolName, writePreview)

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
	writePreview newagentmodel.WriteSchedulePreviewFunc,
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
	flowState := runtimeState.EnsureCommonState()

	// 3.1 顺序护栏在确认执行路径同样生效，避免绕过前置约束。
	if shouldBlockMinContextSwitch(flowState, pending.ToolName) {
		blockedResult := "已拒绝执行 min_context_switch：当前未授权打乱顺序。如需使用该工具，请先由用户明确说明“允许打乱顺序”。"
		_ = emitter.EmitStatus(
			executeStatusBlockID,
			executeStageName,
			"tool_blocked",
			blockedResult,
			false,
		)
		appendToolCallResultHistory(conversationContext, pending.ToolName, args, blockedResult)
		runtimeState.PendingConfirmTool = nil
		return nil
	}

	// 4. 执行工具。
	beforeDigest := summarizeScheduleStateForDebug(scheduleState)
	result := registry.Execute(scheduleState, pending.ToolName, args)
	afterDigest := summarizeScheduleStateForDebug(scheduleState)
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

	// 5. 将工具调用和结果写回历史，维持标准 tool_call 配对格式。
	appendToolCallResultHistory(conversationContext, pending.ToolName, args, result)

	// 5. 写工具实时预览：confirm accept 后真实执行写工具时，立即刷新一次预览缓存。
	tryWritePreviewAfterWriteTool(ctx, flowState, scheduleState, registry, pending.ToolName, writePreview)

	// 6. 清空临时邮箱，避免重复执行。
	runtimeState.PendingConfirmTool = nil

	return nil
}

// tryWritePreviewAfterWriteTool 在写工具执行后尝试刷新一次排程预览缓存。
//
// 职责边界：
// 1. 只负责“写工具后实时可见”的旁路写入，不负责最终收口；
// 2. 只在 write tool 命中时执行，读工具直接跳过；
// 3. 失败只记日志，不影响主流程，避免因为缓存抖动打断执行。
func tryWritePreviewAfterWriteTool(
	ctx context.Context,
	flowState *newagentmodel.CommonState,
	scheduleState *newagenttools.ScheduleState,
	registry *newagenttools.ToolRegistry,
	toolName string,
	writePreview newagentmodel.WriteSchedulePreviewFunc,
) {
	if flowState == nil || scheduleState == nil || registry == nil || writePreview == nil {
		return
	}
	if !registry.IsWriteTool(toolName) {
		return
	}

	if err := writePreview(ctx, scheduleState, flowState.UserID, flowState.ConversationID, flowState.TaskClassIDs); err != nil {
		log.Printf(
			"[WARN] execute realtime preview write failed chat=%s tool=%s err=%v",
			flowState.ConversationID,
			toolName,
			err,
		)
		return
	}

	log.Printf(
		"[DEBUG] execute realtime preview write success chat=%s tool=%s",
		flowState.ConversationID,
		toolName,
	)
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
