package newagentnode

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"regexp"
	"strconv"
	"strings"
	"time"

	infrallm "github.com/LoveLosita/smartflow/backend/infra/llm"
	newagentmodel "github.com/LoveLosita/smartflow/backend/newAgent/model"
	newagentprompt "github.com/LoveLosita/smartflow/backend/newAgent/prompt"
	newagentrouter "github.com/LoveLosita/smartflow/backend/newAgent/router"
	newagentstream "github.com/LoveLosita/smartflow/backend/newAgent/stream"
	newagenttools "github.com/LoveLosita/smartflow/backend/newAgent/tools"
	"github.com/LoveLosita/smartflow/backend/newAgent/tools/schedule"
	"github.com/cloudwego/eino/schema"
	"github.com/google/uuid"
)

const (
	executeStageName               = "execute"
	executeStatusBlockID           = "execute.status"
	executeSpeakBlockID            = "execute.speak"
	executePinnedKey               = "execution_context"
	toolAnalyzeHealth              = "analyze_health"
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
// 6. OriginalScheduleState 继续保留，供 Redis 快照恢复时维持“当前态/原始态”成对语义。
type ExecuteNodeInput struct {
	RuntimeState          *newagentmodel.AgentRuntimeState
	ConversationContext   *newagentmodel.ConversationContext
	UserInput             string
	Client                *infrallm.Client
	ChunkEmitter          *newagentstream.ChunkEmitter
	ResumeNode            string
	ToolRegistry          *newagenttools.ToolRegistry
	ScheduleState         *schedule.ScheduleState
	CompactionStore       newagentmodel.CompactionStore
	WriteSchedulePreview  newagentmodel.WriteSchedulePreviewFunc
	OriginalScheduleState *schedule.ScheduleState
	AlwaysExecute         bool // true 时写工具跳过确认闸门直接执行
	ThinkingEnabled       bool // 是否开启 thinking，由 config.yaml 的 agent.thinking.execute 注入
	PersistVisibleMessage newagentmodel.PersistVisibleMessageFunc
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
	applyPendingContextHook(flowState)

	// 1.5. 确认执行分支：如果用户已确认写操作，直接执行工具。
	if runtimeState.PendingConfirmTool != nil {
		return executePendingTool(
			ctx,
			runtimeState,
			conversationContext,
			input.ToolRegistry,
			input.ScheduleState,
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
		schedule.ResetTaskProcessingQueue(input.ScheduleState)
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

	// 5.1 Token 预算检查 & 上下文压缩。
	messages = compactUnifiedMessagesIfNeeded(ctx, messages, UnifiedCompactInput{
		Client:          input.Client,
		CompactionStore: input.CompactionStore,
		FlowState:       flowState,
		Emitter:         emitter,
		StageName:       executeStageName,
		StatusBlockID:   executeStatusBlockID,
	})

	logNodeLLMContext(executeStageName, "decision", flowState, messages)

	// 两阶段流式执行：从 LLM 流中先提取 <SMARTFLOW_DECISION> 决策标签，再流式推送 speak 正文。
	reader, err := input.Client.Stream(
		ctx,
		messages,
		infrallm.GenerateOptions{
			Temperature: 1.0,
			// 注意：当前模型接口 max_tokens 上限为 131072，超过会 400。
			MaxTokens: 131072,
			Thinking:  resolveThinkingMode(input.ThinkingEnabled),
			Metadata: map[string]any{
				"stage":      executeStageName,
				"step_index": flowState.CurrentStep,
				"round_used": flowState.RoundUsed,
			},
		},
	)
	if err != nil {
		return fmt.Errorf("执行阶段 Stream 调用失败: %w", err)
	}

	parser := newagentrouter.NewStreamDecisionParser()
	firstChunk := true
	speakStreamed := false
	askUserHistoryAppended := false
	var decision *newagentmodel.ExecuteDecision
	var fullText strings.Builder
	rawText := ""
	parsedBeforeText := ""
	parsedAfterText := ""

	// 阶段一：解析决策标签。
	for {
		chunk, recvErr := reader.Recv()
		if recvErr == io.EOF {
			break
		}
		if recvErr != nil {
			log.Printf("[WARN] execute stream recv error chat=%s err=%v", flowState.ConversationID, recvErr)
			break
		}

		if chunk != nil && strings.TrimSpace(chunk.ReasoningContent) != "" {
			if emitErr := emitter.EmitReasoningText(executeSpeakBlockID, executeStageName, chunk.ReasoningContent, firstChunk); emitErr != nil {
				return fmt.Errorf("执行 thinking 推送失败: %w", emitErr)
			}
			firstChunk = false
		}

		content := ""
		if chunk != nil {
			content = chunk.Content
		}

		visible, ready, _ := parser.Feed(content)
		if !ready {
			continue
		}

		result := parser.Result()
		rawText = result.RawBuffer
		parsedBeforeText = result.BeforeText
		parsedAfterText = result.AfterText

		if result.Fallback || result.ParseFailed {
			log.Printf("[DEBUG] execute LLM 输出解析失败 chat=%s round=%d raw=%s",
				flowState.ConversationID, flowState.RoundUsed, rawText)
			flowState.ConsecutiveCorrections++
			if flowState.ConsecutiveCorrections >= maxConsecutiveCorrections {
				return fmt.Errorf("连续 %d 次输出非 JSON，终止执行: 原始输出=%s",
					flowState.ConsecutiveCorrections, rawText)
			}
			var errorDesc, optionHint string
			if strings.Contains(rawText, `"tool_call": [`) || strings.Contains(rawText, `"tool_call":[`) {
				errorDesc = "你在 tool_call 字段传入了数组，但每轮只能调用一个工具，不支持批量格式。"
				optionHint = "请把多个工具调用拆开，每轮只调一个，拿到结果后再继续下一步。"
			} else {
				errorDesc = "你的输出不包含合法的 SMARTFLOW_DECISION 标签，无法解析。"
				optionHint = "你必须先输出 <SMARTFLOW_DECISION>{JSON}</SMARTFLOW_DECISION>，然后在标签后输出正文。"
			}
			AppendLLMCorrectionWithHint(conversationContext, rawText, errorDesc, optionHint)
			return nil
		}

		var parseErr error
		decision, parseErr = infrallm.ParseJSONObject[newagentmodel.ExecuteDecision](result.DecisionJSON)
		if parseErr != nil {
			log.Printf("[DEBUG] execute LLM JSON 解析失败 chat=%s round=%d json=%s raw=%s",
				flowState.ConversationID, flowState.RoundUsed, result.DecisionJSON, rawText)
			flowState.ConsecutiveCorrections++
			if flowState.ConsecutiveCorrections >= maxConsecutiveCorrections {
				return fmt.Errorf("连续 %d 次输出非 JSON，终止执行: 原始输出=%s",
					flowState.ConsecutiveCorrections, rawText)
			}
			// 1. parseErr 场景不回灌原始错误 JSON，避免把错误模板（如 goal_check 对象）再次灌回 msg1；
			// 2. 明确补充 goal_check 类型要求，降低模型在 plan 模式下再次输出对象格式的概率。
			AppendLLMCorrectionWithHint(conversationContext, "",
				"决策标签内的 JSON 格式不合法。",
				"请确保 <SMARTFLOW_DECISION> 标签内是合法 JSON；当 action=next_plan/done 时，goal_check 必须是字符串（不要输出对象）。")
			return nil
		}

		// 阶段二：流式推送 speak（同一 reader 继续读取）。
		if visible != "" {
			if emitErr := emitter.EmitAssistantText(executeSpeakBlockID, executeStageName, visible, firstChunk); emitErr != nil {
				return fmt.Errorf("执行文案推送失败: %w", emitErr)
			}
			speakStreamed = true
			fullText.WriteString(visible)
			firstChunk = false
		}
		for {
			chunk2, recvErr2 := reader.Recv()
			if recvErr2 == io.EOF {
				break
			}
			if recvErr2 != nil {
				log.Printf("[WARN] execute speak stream error chat=%s err=%v", flowState.ConversationID, recvErr2)
				break
			}
			if chunk2 == nil {
				continue
			}
			if strings.TrimSpace(chunk2.ReasoningContent) != "" {
				_ = emitter.EmitReasoningText(executeSpeakBlockID, executeStageName, chunk2.ReasoningContent, false)
			}
			if chunk2.Content != "" {
				if emitErr := emitter.EmitAssistantText(executeSpeakBlockID, executeStageName, chunk2.Content, firstChunk); emitErr != nil {
					return fmt.Errorf("执行文案推送失败: %w", emitErr)
				}
				speakStreamed = true
				fullText.WriteString(chunk2.Content)
				firstChunk = false
			}
		}
		break
	}

	// 流结束但未找到决策标签。
	if decision == nil {
		if strings.TrimSpace(rawText) == "" {
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
				"请重新输出 <SMARTFLOW_DECISION>{JSON}</SMARTFLOW_DECISION> 格式的执行决策。",
			)
			return nil
		}
		return fmt.Errorf("执行阶段流结束但未提取到决策标签")
	}

	decision.Speak = pickExecuteVisibleSpeak(
		fullText.String(),
		parsedAfterText,
		parsedBeforeText,
		decision,
	)

	// 调试日志：输出解析后的决策，方便排查。
	log.Printf("[DEBUG] execute LLM 响应 chat=%s round=%d action=%s speak_len=%d raw_len=%d raw_preview=%.200s",
		flowState.ConversationID, flowState.RoundUsed,
		decision.Action, len(decision.Speak), len(rawText), rawText)

	// done 收尾兼容：若模型在 done 时顺手带了 context_tools_remove，直接忽略该 tool_call。
	//
	// 1. done 语义是“结束本轮”，不应再发起工具调用；
	// 2. 动态区清理由系统在 Done() 自动完成，不依赖 LLM 显式 remove；
	// 3. 仅对 context_tools_remove 放宽，其他 done+tool_call 仍按非法决策处理。
	if decision.Action == newagentmodel.ExecuteActionDone &&
		decision.ToolCall != nil &&
		strings.EqualFold(strings.TrimSpace(decision.ToolCall.Name), newagenttools.ToolNameContextToolsRemove) {
		decision.ToolCall = nil
	}

	if err := decision.Validate(); err != nil {
		flowState.ConsecutiveCorrections++
		log.Printf("[WARN] execute 决策不合法 chat=%s round=%d consecutive=%d/%d err=%s",
			flowState.ConversationID, flowState.RoundUsed,
			flowState.ConsecutiveCorrections, maxConsecutiveCorrections, err.Error())
		if flowState.ConsecutiveCorrections >= maxConsecutiveCorrections {
			return fmt.Errorf("连续 %d 次决策不合法，终止执行: %s (原始输出: %s)",
				flowState.ConsecutiveCorrections, err.Error(), rawText)
		}
		_ = emitter.EmitStatus(
			executeStatusBlockID,
			executeStageName,
			"executing",
			fmt.Sprintf("执行校验：决策不合法（%s），已请求模型重试。", err.Error()),
			false,
		)
		// 给 LLM 修正机会。
		AppendLLMCorrectionWithHint(
			conversationContext,
			"",
			fmt.Sprintf("你的执行决策不合法：%s", err.Error()),
			"合法的 action 包括：continue（继续当前步骤）、ask_user（追问用户）、confirm（写操作确认）、next_plan（推进到下一步）、done（任务完成）、abort（正式终止本轮流程）。",
		)
		return nil
	}

	// 决策合法，重置连续修正计数。
	flowState.ConsecutiveCorrections = 0

	// speak 兜底：
	// 1. 优先使用标签后正文（主协议）；
	// 2. 若标签后无正文，则回退到标签前前言；
	// 3. 前后都没有时，再使用 reason / 默认短句，避免前端出现“静默一轮”。
	decision.Speak = pickExecuteVisibleSpeak(
		decision.Speak,
		parsedAfterText,
		parsedBeforeText,
		decision,
	)

	// speak 后处理：补列表序号换行 + 末尾加 \n 防止连续 speak 在前端粘连。
	decision.Speak = normalizeSpeak(decision.Speak) // 末尾已含 \n

	// 非写工具的 confirm 动作自动降级为 continue。
	// 调用目的：快捷随口记这类非日程写工具不应走确认卡片流程；
	// 即使 LLM 误输出 action=confirm，也在此处强制修正，
	// 确保 speak 正常推流和持久化，不会因 confirm 卡片跳过 persistVisibleAssistantMessage。
	if decision.Action == newagentmodel.ExecuteActionConfirm &&
		decision.ToolCall != nil &&
		input.ToolRegistry != nil &&
		!input.ToolRegistry.IsWriteTool(decision.ToolCall.Name) {
		decision.Action = newagentmodel.ExecuteActionContinue
	}

	// 1. context_tools_add/remove 属于“工具准备步”，不应在历史里保留完整追问文案；
	// 2. 若该类动作携带了较长 speak，会在下一轮被 msg1/msg2 双重回灌，导致模型复读；
	// 3. 这里统一清空 speak，仅保留工具调用事实，避免“同一句 ask_user 文案”被连续输出两次。
	if decision.Action == newagentmodel.ExecuteActionContinue &&
		decision.ToolCall != nil &&
		newagenttools.IsContextManagementTool(decision.ToolCall.Name) {
		decision.Speak = ""
	}

	// 若模型把自然语言放在标签前，或完全漏掉了标签后正文，
	// 这里在“本轮尚未真正向前端推过正文”时补发最终 speak，
	// 保证前端和历史都能看到同一份可见文案。
	if !speakStreamed && strings.TrimSpace(decision.Speak) != "" {
		if emitErr := emitter.EmitAssistantText(
			executeSpeakBlockID,
			executeStageName,
			decision.Speak,
			firstChunk,
		); emitErr != nil {
			return fmt.Errorf("执行文案兜底推送失败: %w", emitErr)
		}
		speakStreamed = true
		firstChunk = false
	}

	// 1. execute 正文若已经在流式阶段推给前端，normalizeSpeak 新补出来的尾部（最常见是末尾 \n）
	//    不会自动回流到前端，只会留在 history / persist 中。
	// 2. 这会导致下一跳 deliver 首条正文直接接在 execute 最后一段后面，前端表现成两段文本黏连。
	// 3. 这里只补发“归一化后新增的尾巴”，不重发整段正文，也不改写中间内容，避免误伤已有流式体验。
	if speakStreamed {
		streamedText := fullText.String()
		if tail := buildExecuteNormalizedSpeakTail(streamedText, decision.Speak); tail != "" {
			if emitErr := emitter.EmitAssistantText(
				executeSpeakBlockID,
				executeStageName,
				tail,
				firstChunk,
			); emitErr != nil {
				return fmt.Errorf("执行文案尾部补发失败: %w", emitErr)
			}
			firstChunk = false
		}
	}

	// 自省校验（仅 Plan 模式）：next_plan / done 必须附带 goal_check，否则不推进，追加修正让 LLM 重试。
	//
	// 1. ReAct（无预定义步骤）下不强制 goal_check，避免 done 被错误拦截后进入循环；
	// 2. Plan（有 done_when）下才要求 goal_check，对齐“按步骤验收”的语义；
	// 3. 校验失败时推送一条可见状态，避免前端观察到“静默继续下一轮”。
	if flowState.HasPlan() &&
		(decision.Action == newagentmodel.ExecuteActionNextPlan ||
			decision.Action == newagentmodel.ExecuteActionDone) {
		if strings.TrimSpace(decision.GoalCheck) == "" {
			flowState.ConsecutiveCorrections++
			if flowState.ConsecutiveCorrections >= maxConsecutiveCorrections {
				return fmt.Errorf("连续 %d 次 goal_check 为空，终止执行", flowState.ConsecutiveCorrections)
			}
			_ = emitter.EmitStatus(
				executeStatusBlockID,
				executeStageName,
				"executing",
				fmt.Sprintf("执行校验：action=%s 缺少 goal_check，已请求模型重试。", decision.Action),
				false,
			)
			AppendLLMCorrectionWithHint(
				conversationContext,
				"",
				fmt.Sprintf("你输出了 action=%s，但 goal_check 为空。", decision.Action),
				fmt.Sprintf("输出 %s 时，必须在 goal_check 中对照 done_when 逐条说明完成依据。", decision.Action),
			)
			return nil
		}
	}

	// 6. speak 已在流式循环中推送，此处仅做持久化与历史写入。
	speakText := decision.Speak
	if speakText != "" {
		isConfirmWithCard := decision.Action == newagentmodel.ExecuteActionConfirm && !input.AlwaysExecute
		isAskUser := decision.Action == newagentmodel.ExecuteActionAskUser
		isAbort := decision.Action == newagentmodel.ExecuteActionAbort

		if !isConfirmWithCard && !isAskUser && !isAbort {
			msg := schema.AssistantMessage(speakText, nil)
			persistVisibleAssistantMessage(ctx, input.PersistVisibleMessage, flowState, msg)
		}
		// confirm / ask_user 的 speak 仍写入历史，避免下一轮 LLM 丢失上下文；
		// abort 不写历史，避免与 deliver 终止文案冲突。
		if !isAbort {
			conversationContext.AppendHistory(&schema.Message{
				Role:    schema.Assistant,
				Content: speakText,
			})
			if isAskUser {
				askUserHistoryAppended = true
			}
		}
	}

	// 7. 按 LLM 决策执行动作，后端信任 LLM 判断，不做语义校验。
	switch decision.Action {
	case newagentmodel.ExecuteActionContinue:
		// 继续当前步骤的 ReAct 循环。
		// 若有工具调用意图，则执行工具并记录证据。
		if decision.ToolCall != nil {
			// 1. 所有写工具都必须走 confirm；continue 只允许读工具。
			// 2. 若模型误输出 continue+写工具，这里先做纠偏，不直接执行写操作。
			if input.ToolRegistry != nil && input.ToolRegistry.IsWriteTool(decision.ToolCall.Name) {
				flowState.ConsecutiveCorrections++
				log.Printf(
					"[WARN] execute 决策协议违规 chat=%s round=%d action=continue tool=%s consecutive=%d/%d",
					flowState.ConversationID,
					flowState.RoundUsed,
					strings.TrimSpace(decision.ToolCall.Name),
					flowState.ConsecutiveCorrections,
					maxConsecutiveCorrections,
				)
				if flowState.ConsecutiveCorrections >= maxConsecutiveCorrections {
					return fmt.Errorf("连续 %d 次输出 continue+写工具，终止执行", flowState.ConsecutiveCorrections)
				}
				_ = emitter.EmitStatus(
					executeStatusBlockID,
					executeStageName,
					"executing",
					fmt.Sprintf("执行校验：写工具 %q 未执行。原因：模型输出了 action=continue；所有写工具都必须使用 action=confirm。", strings.TrimSpace(decision.ToolCall.Name)),
					false,
				)
				llmOutput := decision.Speak
				if strings.TrimSpace(llmOutput) == "" {
					llmOutput = decision.Reason
				}
				AppendLLMCorrectionWithHint(
					conversationContext,
					llmOutput,
					fmt.Sprintf("你输出了 action=continue，但工具 %q 属于写操作。", decision.ToolCall.Name),
					"所有写操作都必须输出 action=confirm，并附带同一个 tool_call；continue 仅用于读工具。这次写操作没有执行，请直接重发 confirm。",
				)
				return nil
			}
			if shouldForceFeasibilityNegotiation(flowState, input.ToolRegistry, decision.ToolCall.Name) {
				runtimeState.OpenAskUserInteraction(
					uuid.NewString(),
					buildInfeasibleNegotiationQuestion(flowState),
					strings.TrimSpace(input.ResumeNode),
				)
				return nil
			}
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
		// 1. execute 阶段可能已流式推送 ask_user 文本；
		// 2. interrupt 节点读取该元信息后可跳过二次正文推送，避免前端重复显示；
		// 3. history 是否已写入也一并标记，防止上下文重复追加。
		runtimeState.SetPendingInteractionMetadata(newagentmodel.PendingMetaAskUserSpeakStreamed, speakStreamed)
		runtimeState.SetPendingInteractionMetadata(newagentmodel.PendingMetaAskUserHistoryAppended, askUserHistoryAppended)
		return nil

	case newagentmodel.ExecuteActionConfirm:
		// AlwaysExecute=true：跳过确认闸门，直接执行内存写工具，不走 confirm 节点。
		if decision.ToolCall != nil && shouldForceFeasibilityNegotiation(flowState, input.ToolRegistry, decision.ToolCall.Name) {
			runtimeState.OpenAskUserInteraction(
				uuid.NewString(),
				buildInfeasibleNegotiationQuestion(flowState),
				strings.TrimSpace(input.ResumeNode),
			)
			return nil
		}
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

// pickExecuteVisibleSpeak 统一按“后文 -> 前言 -> fallback”选择最终可见文案。
//
// 规则：
// 1. streamed / afterText 对应 </SMARTFLOW_DECISION> 后的正文，优先级最高；
// 2. beforeText 对应标签前前言，仅在后文为空时兜底使用；
// 3. 三者都为空时，再回退到 reason / 默认短句。
func pickExecuteVisibleSpeak(
	streamed string,
	afterText string,
	beforeText string,
	decision *newagentmodel.ExecuteDecision,
) string {
	if text := strings.TrimSpace(streamed); text != "" {
		return text
	}
	if text := strings.TrimSpace(afterText); text != "" {
		return text
	}
	if text := strings.TrimSpace(beforeText); text != "" {
		return text
	}
	return buildExecuteSpeakWithFallback(decision)
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

func buildScopeDaySet(state *schedule.ScheduleState, scope *executeStepScope) map[int]struct{} {
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

func dayMatchesScope(state *schedule.ScheduleState, scope *executeStepScope, day int) bool {
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

func estimateCandidateDaysFromArgs(state *schedule.ScheduleState, args map[string]any) (map[int]struct{}, bool, error) {
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

func parseAnyToStringSlice(value any) []string {
	switch values := value.(type) {
	case []string:
		result := make([]string, 0, len(values))
		for _, item := range values {
			text := strings.TrimSpace(item)
			if text == "" {
				continue
			}
			result = append(result, text)
		}
		return result
	case []any:
		result := make([]string, 0, len(values))
		for _, item := range values {
			text := strings.TrimSpace(fmt.Sprintf("%v", item))
			if text == "" || text == "<nil>" {
				continue
			}
			result = append(result, text)
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
	scheduleState *schedule.ScheduleState,
	writePreview newagentmodel.WriteSchedulePreviewFunc,
) error {
	if toolCall == nil {
		return nil
	}

	toolName := strings.TrimSpace(toolCall.Name)
	if toolName == "" {
		return fmt.Errorf("工具调用缺少工具名称")
	}

	// 推送工具调用开始事件（结构化）。
	if err := emitter.EmitToolCallStart(
		executeStatusBlockID,
		executeStageName,
		toolName,
		buildToolCallStartSummary(toolName, toolCall.Arguments),
		buildToolArgumentsPreviewCN(toolCall.Arguments),
		false,
	); err != nil {
		return fmt.Errorf("工具调用开始事件推送失败: %w", err)
	}

	// 1. 校验依赖。
	if registry == nil {
		return fmt.Errorf("工具注册表未注入")
	}
	if scheduleState == nil && registry.RequiresScheduleState(toolName) {
		return fmt.Errorf("日程状态未加载，无法执行工具 %q", toolName)
	}
	if registry.IsToolTemporarilyDisabled(toolName) {
		flowState.ConsecutiveCorrections++
		if flowState.ConsecutiveCorrections >= maxConsecutiveCorrections {
			return fmt.Errorf("连续 %d 次调用临时禁用工具，终止执行: %s",
				flowState.ConsecutiveCorrections, toolName)
		}
		blockedResult := buildTemporarilyDisabledToolResult(toolName)
		_ = emitter.EmitToolCallResult(
			executeStatusBlockID,
			executeStageName,
			toolName,
			"blocked",
			blockedResult,
			buildToolArgumentsPreviewCN(toolCall.Arguments),
			false,
		)
		appendToolCallResultHistory(conversationContext, toolName, toolCall.Arguments, blockedResult)
		AppendLLMCorrectionWithHint(
			conversationContext,
			"",
			fmt.Sprintf("工具 %q 当前暂时禁用。", toolName),
			"请改用 move/swap/batch_move/unplace 等基础微调工具继续推进。",
		)
		return nil
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
	if !isToolVisibleForCurrentExecuteMode(flowState, registry, toolName) {
		flowState.ConsecutiveCorrections++
		if flowState.ConsecutiveCorrections >= maxConsecutiveCorrections {
			return fmt.Errorf("连续 %d 次调用未激活域工具，终止执行: %s（active_domain=%q active_packs=%v）",
				flowState.ConsecutiveCorrections,
				toolName,
				flowState.ActiveToolDomain,
				newagenttools.ResolveEffectiveToolPacks(flowState.ActiveToolDomain, flowState.ActiveToolPacks))
		}

		addHint := `请先调用 context_tools_add 激活目标工具域后再继续。`
		if flowState != nil && flowState.ActiveOptimizeOnly {
			addHint = `当前处于“粗排后主动优化专用模式”，只允许使用 analyze_health、move、swap；不要再尝试 query_target_tasks / query_available_slots 等全窗搜索工具。`
		} else if domain, pack, ok := newagenttools.ResolveToolDomainPack(toolName); ok {
			if newagenttools.IsFixedToolPack(domain, pack) {
				addHint = fmt.Sprintf(`请先调用 context_tools_add，参数 domain="%s"。`, domain)
			} else {
				addHint = fmt.Sprintf(`请先调用 context_tools_add，参数 domain="%s", packs=["%s"]。`, domain, pack)
			}
		}

		AppendLLMCorrectionWithHint(
			conversationContext,
			"",
			fmt.Sprintf("你调用的工具 %q 当前不在已激活工具域内。", toolName),
			addHint,
		)
		return nil
	}

	// 2. 执行工具。
	if shouldForceFeasibilityNegotiation(flowState, registry, toolName) {
		blockedResult := buildInfeasibleBlockedResult(flowState)
		_ = emitter.EmitToolCallResult(
			executeStatusBlockID,
			executeStageName,
			toolName,
			"blocked",
			blockedResult,
			buildToolArgumentsPreviewCN(toolCall.Arguments),
			false,
		)
		appendToolCallResultHistory(conversationContext, toolName, toolCall.Arguments, blockedResult)
		return nil
	}

	beforeDigest := summarizeScheduleStateForDebug(scheduleState)
	// 调用目的：为不依赖 ScheduleState 的工具注入用户身份，工具层通过 args["_user_id"] 提取。
	if !registry.RequiresScheduleState(toolName) {
		if toolCall.Arguments == nil {
			toolCall.Arguments = make(map[string]any)
		}
		toolCall.Arguments["_user_id"] = flowState.UserID
	}
	result := registry.Execute(scheduleState, toolName, toolCall.Arguments)
	updateHealthSnapshotV2(flowState, toolName, result)
	updateTaskClassUpsertSnapshot(flowState, toolName, result)
	updateActiveToolDomainSnapshot(flowState, toolName, result)
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
	_ = emitter.EmitToolCallResult(
		executeStatusBlockID,
		executeStageName,
		toolName,
		resolveToolEventResultStatus(result),
		buildToolEventResultSummary(result),
		buildToolArgumentsPreviewCN(toolCall.Arguments),
		false,
	)

	// 3. 以标准 assistant+tool 消息对写回历史，避免消息链断裂。
	appendToolCallResultHistory(conversationContext, toolName, toolCall.Arguments, result)

	// 3.1 仅“日程修改工具”才算日程变更。
	// 任务类写库（如 upsert_task_class）不应触发顺序守卫与排程完成卡片。
	if registry.IsScheduleMutationTool(toolName) {
		flowState.HasScheduleWriteOps = true
		flowState.HasScheduleChanges = true
	}

	// 4. 写工具实时预览：每次写工具执行后都尝试刷新 Redis 预览，确保前端可见“最新操作结果”。
	//
	// 步骤化说明：
	// 1. 仅写工具触发实时预览刷新，读工具不触发，避免无意义放大写流量；
	// 2. 这里采用“失败不阻断主流程”策略：预览写失败只记日志，不影响当前执行链路；
	// 3. Deliver 节点仍保留最终覆盖写，保证 order_guard/收口后的最终态一致。
	tryWritePreviewAfterWriteTool(ctx, flowState, scheduleState, registry, toolName, writePreview)

	return nil
}

// applyPendingContextHook 在 execute 轮次开始时消费一次 plan 传递的 context_hook。
//
// 步骤化说明：
// 1. 仅在存在 PendingContextHook 时生效，避免无意义状态写入；
// 2. 域与 packs 按工具映射规则归一化，保证和 context_tools_add 的结果语义一致；
// 3. 消费后立即清空 PendingContextHook，避免每轮重复覆盖造成噪声。
func applyPendingContextHook(flowState *newagentmodel.CommonState) {
	if flowState == nil || flowState.PendingContextHook == nil {
		return
	}
	hook := flowState.PendingContextHook
	domain := newagenttools.NormalizeToolDomain(hook.Domain)
	if domain == "" {
		flowState.PendingContextHook = nil
		return
	}
	flowState.ActiveToolDomain = domain
	flowState.ActiveToolPacks = newagenttools.ResolveEffectiveToolPacks(domain, hook.Packs)
	flowState.PendingContextHook = nil
}

// isToolVisibleForCurrentExecuteMode 统一判定“当前 execute 轮次里，这个工具到底能不能被调”。
//
// 步骤化说明：
// 1. 先走原有的 domain + pack 可见性校验，保证普通链路行为不变；
// 2. 若当前开启了主动优化专用模式，再叠加一道更强的白名单裁剪；
// 3. 这样可以做到“工具定义仍保留，但主动优化场景只露最小闭环”，且不影响普通服务链路。
func isToolVisibleForCurrentExecuteMode(
	flowState *newagentmodel.CommonState,
	registry *newagenttools.ToolRegistry,
	toolName string,
) bool {
	if registry == nil {
		return false
	}
	activeDomain := ""
	var activePacks []string
	if flowState != nil {
		activeDomain = flowState.ActiveToolDomain
		activePacks = flowState.ActiveToolPacks
	}
	if !registry.IsToolVisibleInDomain(activeDomain, activePacks, toolName) {
		return false
	}
	if flowState != nil && flowState.ActiveOptimizeOnly && !newagenttools.IsToolAllowedInActiveOptimize(toolName) {
		return false
	}
	return true
}

// buildTemporarilyDisabledToolResult 统一生成“工具临时禁用”的观察文本。
func buildTemporarilyDisabledToolResult(toolName string) string {
	return fmt.Sprintf("工具 %q 当前暂时禁用。请改用 move/swap/batch_move/unplace 等基础微调工具。", strings.TrimSpace(toolName))
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
	scheduleState *schedule.ScheduleState,
	originalState *schedule.ScheduleState,
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

	// 2. 推送工具调用开始事件（结构化）。
	if err := emitter.EmitToolCallStart(
		executeStatusBlockID,
		executeStageName,
		pending.ToolName,
		buildToolCallStartSummary(pending.ToolName, args),
		buildToolArgumentsPreviewCN(args),
		false,
	); err != nil {
		return fmt.Errorf("工具调用开始事件推送失败: %w", err)
	}

	// 3. 校验依赖：写工具必须持有有效的日程状态。
	if scheduleState == nil {
		return fmt.Errorf("日程状态未加载，无法执行已确认的写工具 %s", pending.ToolName)
	}
	flowState := runtimeState.EnsureCommonState()
	if registry.IsToolTemporarilyDisabled(pending.ToolName) {
		blockedResult := buildTemporarilyDisabledToolResult(pending.ToolName)
		_ = emitter.EmitToolCallResult(
			executeStatusBlockID,
			executeStageName,
			pending.ToolName,
			"blocked",
			blockedResult,
			buildToolArgumentsPreviewCN(args),
			false,
		)
		appendToolCallResultHistory(conversationContext, pending.ToolName, args, blockedResult)
		runtimeState.PendingConfirmTool = nil
		return nil
	}

	if shouldForceFeasibilityNegotiation(flowState, registry, pending.ToolName) {
		blockedResult := buildInfeasibleBlockedResult(flowState)
		_ = emitter.EmitToolCallResult(
			executeStatusBlockID,
			executeStageName,
			pending.ToolName,
			"blocked",
			blockedResult,
			buildToolArgumentsPreviewCN(args),
			false,
		)
		appendToolCallResultHistory(conversationContext, pending.ToolName, args, blockedResult)
		runtimeState.PendingConfirmTool = nil
		return nil
	}

	// 4. 执行工具。
	beforeDigest := summarizeScheduleStateForDebug(scheduleState)
	// 调用目的：为不依赖 ScheduleState 的工具注入用户身份，工具层通过 args["_user_id"] 提取。
	if !registry.RequiresScheduleState(pending.ToolName) {
		if args == nil {
			args = make(map[string]any)
		}
		args["_user_id"] = flowState.UserID
	}
	result := registry.Execute(scheduleState, pending.ToolName, args)
	updateHealthSnapshotV2(flowState, pending.ToolName, result)
	updateTaskClassUpsertSnapshot(flowState, pending.ToolName, result)
	updateActiveToolDomainSnapshot(flowState, pending.ToolName, result)
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
	_ = emitter.EmitToolCallResult(
		executeStatusBlockID,
		executeStageName,
		pending.ToolName,
		resolveToolEventResultStatus(result),
		buildToolEventResultSummary(result),
		buildToolArgumentsPreviewCN(args),
		false,
	)

	// 5. 将工具调用和结果写回历史，维持标准 tool_call 配对格式。
	appendToolCallResultHistory(conversationContext, pending.ToolName, args, result)

	// 5.1 仅“日程修改工具”才算日程变更。
	if registry.IsScheduleMutationTool(pending.ToolName) {
		flowState.HasScheduleWriteOps = true
		flowState.HasScheduleChanges = true
	}

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
	scheduleState *schedule.ScheduleState,
	registry *newagenttools.ToolRegistry,
	toolName string,
	writePreview newagentmodel.WriteSchedulePreviewFunc,
) {
	if flowState == nil || scheduleState == nil || registry == nil || writePreview == nil {
		return
	}
	if !registry.IsScheduleMutationTool(toolName) {
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

// buildExecuteNormalizedSpeakTail 计算“归一化后新增、但前端尚未收到”的 execute 文案尾巴。
//
// 职责边界：
// 1. 只处理“streamed 原文是 normalized 的前缀”这一保守场景，典型就是只缺末尾换行；
// 2. 不尝试回放中间格式差异，避免把整段已流式输出的正文再推一遍；
// 3. 若无法安全判断差额，则返回空串，交给现有行为继续执行。
func buildExecuteNormalizedSpeakTail(streamed, normalized string) string {
	streamed = strings.ReplaceAll(streamed, "\r\n", "\n")
	normalized = strings.ReplaceAll(normalized, "\r\n", "\n")
	if streamed == "" || normalized == "" {
		return ""
	}
	if !strings.HasPrefix(normalized, streamed) {
		return ""
	}
	return normalized[len(streamed):]
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
func summarizeScheduleStateForDebug(state *schedule.ScheduleState) string {
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
		case schedule.IsPendingTask(*t):
			pendingNoSlot++
		case schedule.IsSuggestedTask(*t):
			suggestedTotal++
		case schedule.IsExistingTask(*t):
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

// resolveToolEventResultStatus 将工具返回文本映射为前端可识别的结果状态。
//
// 职责边界：
// 1. 只做轻量字符串规则判断，不做业务语义推理；
// 2. 默认归类为 done，只有明显失败关键字才判定 failed；
// 3. blocked 场景在调用侧显式传入，不由这里推断。
func resolveToolEventResultStatus(result string) string {
	normalized := strings.TrimSpace(result)
	if normalized == "" {
		return "done"
	}
	if strings.Contains(normalized, "失败") {
		return "failed"
	}
	lower := strings.ToLower(normalized)
	if strings.Contains(lower, "error") || strings.Contains(lower, "failed") {
		return "failed"
	}
	return "done"
}

// buildToolEventResultSummary 生成用于前端工具行的结果摘要。
//
// 职责边界：
// 1. 优先从 JSON 结果提炼中文结论，避免前端直接看到原始字段；
// 2. 提炼失败时回退到“压平 + 截断”，保证仍有可读摘要；
// 3. 空结果给出固定兜底文案，避免前端出现空白行。
func buildToolEventResultSummary(result string) string {
	flat := flattenForLog(result)
	if flat == "" {
		return "工具已执行完成。"
	}

	// 1. 工具很多返回 JSON；直接截断 JSON 会把字段名原样暴露到前端，阅读体验差。
	// 2. 这里优先做结构化提炼，转成一句中文结论（成功/失败/关键结果）。
	// 3. 仅在无法提炼时才回退到原文截断，保证不会丢失可读信息。
	if summary, ok := tryExtractToolResultSummaryCN(flat); ok {
		return summary
	}

	runes := []rune(flat)
	if len(runes) <= 48 {
		return flat
	}
	return string(runes[:48]) + "..."
}

func tryExtractToolResultSummaryCN(raw string) (string, bool) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return "", false
	}

	var payload map[string]any
	if err := json.Unmarshal([]byte(trimmed), &payload); err != nil {
		return "", false
	}

	toolRaw := strings.TrimSpace(readStringAnyFromMap(payload, "tool"))
	toolName := resolveToolDisplayNameCN(toolRaw)

	// 任务类写入工具优先走结构化提炼，确保前端摘要直接暴露“是否缺字段”。
	if strings.EqualFold(toolRaw, "upsert_task_class") {
		if summary, ok := buildUpsertTaskClassSummaryCN(payload); ok {
			return truncateToolSummaryCN(summary), true
		}
	}

	if errText := strings.TrimSpace(readStringAnyFromMap(payload, "error", "err")); errText != "" {
		return truncateToolSummaryCN(fmt.Sprintf("%s失败：%s", toolName, errText)), true
	}

	if success, exists := payload["success"]; exists {
		if ok, isBool := success.(bool); isBool && !ok {
			reason := strings.TrimSpace(readStringAnyFromMap(payload, "reason", "message"))
			if reason != "" {
				return truncateToolSummaryCN(fmt.Sprintf("%s失败：%s", toolName, reason)), true
			}
			return truncateToolSummaryCN(fmt.Sprintf("%s执行失败。", toolName)), true
		}
	}

	if message := strings.TrimSpace(readStringAnyFromMap(payload, "result", "message", "reason")); message != "" {
		return truncateToolSummaryCN(message), true
	}

	pending, hasPending := readIntAnyFromMap(payload, "pending_count")
	completed, hasCompleted := readIntAnyFromMap(payload, "completed_count")
	if hasPending || hasCompleted {
		skipped, _ := readIntAnyFromMap(payload, "skipped_count")
		return fmt.Sprintf("队列状态：待处理 %d，已完成 %d，已跳过 %d。", pending, completed, skipped), true
	}

	if hasHead, exists := payload["has_head"]; exists {
		if b, isBool := hasHead.(bool); isBool {
			if b {
				return "已取到当前队首任务。", true
			}
			return "当前队列没有可处理任务。", true
		}
	}

	if _, ok := payload["slot_candidates"]; ok {
		if total, exists := readIntAnyFromMap(payload, "total"); exists {
			return fmt.Sprintf("已找到 %d 个可用时段。", total), true
		}
	}

	if toolRaw != "" {
		return fmt.Sprintf("已完成「%s」。", toolName), true
	}

	return "", false
}

func buildUpsertTaskClassSummaryCN(payload map[string]any) (string, bool) {
	validationRaw, hasValidation := payload["validation"]
	if !hasValidation {
		return "", false
	}
	validation, ok := validationRaw.(map[string]any)
	if !ok {
		return "", false
	}

	validationOK, hasValidationOK := validation["ok"].(bool)
	issues := parseAnyToStringSlice(validation["issues"])

	if hasValidationOK && !validationOK {
		if len(issues) > 0 {
			return fmt.Sprintf("任务类写入未通过校验：%s。", strings.Join(issues, "；")), true
		}
		return "任务类写入未通过校验，请先补齐缺失字段。", true
	}

	success, hasSuccess := payload["success"].(bool)
	if hasSuccess && success {
		if taskClassID, ok := readIntAnyFromMap(payload, "task_class_id"); ok && taskClassID > 0 {
			return fmt.Sprintf("任务类写入成功，task_class_id=%d。", taskClassID), true
		}
		return "任务类写入成功。", true
	}

	return "", false
}

func truncateToolSummaryCN(text string) string {
	runes := []rune(strings.TrimSpace(text))
	if len(runes) <= 48 {
		return string(runes)
	}
	return string(runes[:48]) + "..."
}

// buildToolCallStartSummary 生成“工具开始调用”的中文摘要。
//
// 职责边界：
// 1. 摘要面向前端展示，避免直接暴露参数字段名；
// 2. 只做轻量信息拼接，不做业务语义推断；
// 3. 仅展示少量关键参数，避免消息过长抢占正文注意力。
func buildToolCallStartSummary(toolName string, args map[string]any) string {
	displayName := resolveToolDisplayNameCN(toolName)
	argSummary := buildToolArgumentsPreviewCN(args)
	if argSummary == "" {
		return fmt.Sprintf("已调用工具：%s。", displayName)
	}
	return fmt.Sprintf("已调用工具：%s（%s）。", displayName, argSummary)
}

// buildToolArgumentsPreviewCN 把工具参数转换为中文可读摘要。
//
// 职责边界：
// 1. 只输出白名单字段的中文标签，避免把原始参数键直接透出给前端；
// 2. 默认最多展示 2 组参数，防止工具行过长；
// 3. 无可展示参数时返回空字符串，由上层决定是否展示。
func buildToolArgumentsPreviewCN(args map[string]any) string {
	if len(args) <= 0 {
		return ""
	}

	type argPair struct {
		Key   string
		Label string
	}

	orderedPairs := []argPair{
		{Key: "title", Label: "任务标题"},
		{Key: "task_name", Label: "任务名称"},
		{Key: "deadline_at", Label: "截止时间"},
		{Key: "new_day", Label: "目标天"},
		{Key: "new_slot_start", Label: "目标开始节次"},
		{Key: "day", Label: "天"},
		{Key: "day_start", Label: "起始天"},
		{Key: "day_end", Label: "结束天"},
		{Key: "day_scope", Label: "日期范围"},
		{Key: "day_of_week", Label: "星期"},
		{Key: "week", Label: "周"},
		{Key: "week_from", Label: "起始周"},
		{Key: "week_to", Label: "结束周"},
		{Key: "week_filter", Label: "周筛选"},
		{Key: "slot_start", Label: "开始节次"},
		{Key: "slot_end", Label: "结束节次"},
		{Key: "slot_type", Label: "时段类型"},
		{Key: "slot_types", Label: "时段类型"},
		{Key: "task_id", Label: "任务编号"},
		{Key: "task_ids", Label: "任务列表"},
		{Key: "task_item_id", Label: "任务条目"},
		{Key: "task_item_ids", Label: "任务条目列表"},
		{Key: "query", Label: "搜索词"},
		{Key: "keyword", Label: "关键词"},
		{Key: "domain", Label: "工具域"},
		{Key: "mode", Label: "注入模式"},
		{Key: "all", Label: "清空全部"},
		{Key: "top_k", Label: "返回数量"},
		{Key: "url", Label: "链接"},
		{Key: "reason", Label: "原因"},
		{Key: "limit", Label: "数量"},
	}

	items := make([]string, 0, 2)
	for _, pair := range orderedPairs {
		rawValue, exists := args[pair.Key]
		if !exists {
			continue
		}
		valueText := formatToolArgValueByKeyCN(pair.Key, rawValue)
		if valueText == "" {
			continue
		}
		items = append(items, fmt.Sprintf("%s：%s", pair.Label, valueText))
		if len(items) >= 2 {
			break
		}
	}

	return strings.Join(items, "，")
}

// resolveToolDisplayNameCN 返回工具中文展示名。
func resolveToolDisplayNameCN(toolName string) string {
	name := strings.TrimSpace(toolName)
	if name == "" {
		return "未知工具"
	}

	displayNameMap := map[string]string{
		"get_overview":          "查看总览",
		"query_range":           "查询时段详情",
		"queue_status":          "查看任务队列",
		"queue_pop_head":        "获取队首任务",
		"queue_apply_head_move": "调整队首任务时段",
		"queue_skip_head":       "跳过队首任务",
		"query_target_tasks":    "查询目标任务",
		"query_available_slots": "查询可用时间段",
		"get_task_info":         "查看任务详情",
		"analyze_health":        "综合体检",
		"analyze_rhythm":        "分析学习节奏",
		"web_search":            "网页搜索",
		"web_fetch":             "网页抓取",
		"move":                  "移动任务",
		"place":                 "放置任务",
		"swap":                  "交换任务",
		"batch_move":            "批量移动任务",
		"unplace":               "移除任务安排",
		"upsert_task_class":     "写入任务类",
		"context_tools_add":     "激活工具域",
		"context_tools_remove":  "移除工具域",
	}

	if label, ok := displayNameMap[name]; ok {
		return label
	}
	return name
}

func formatToolArgValueByKeyCN(key string, value any) string {
	switch key {
	case "day_scope":
		scope := strings.ToLower(strings.TrimSpace(formatToolArgValueCN(value)))
		switch scope {
		case "workday":
			return "工作日"
		case "weekend":
			return "周末"
		case "all":
			return "全部日期"
		default:
			return scope
		}
	case "day_of_week":
		weekdays := parseAnyToIntSlice(value)
		if len(weekdays) <= 0 {
			return formatToolArgValueCN(value)
		}
		labels := make([]string, 0, len(weekdays))
		for _, day := range weekdays {
			labels = append(labels, fmt.Sprintf("周%d", day))
			if len(labels) >= 4 {
				break
			}
		}
		return strings.Join(labels, "、")
	case "task_ids", "task_item_ids", "week_filter":
		values := parseAnyToIntSlice(value)
		if len(values) <= 0 {
			return formatToolArgValueCN(value)
		}
		items := make([]string, 0, len(values))
		for _, current := range values {
			items = append(items, strconv.Itoa(current))
			if len(items) >= 4 {
				break
			}
		}
		return strings.Join(items, "、")
	case "url":
		return truncateToolSummaryCN(formatToolArgValueCN(value))
	case "reason", "title", "task_name", "query", "keyword":
		return truncateToolSummaryCN(formatToolArgValueCN(value))
	default:
		return formatToolArgValueCN(value)
	}
}

// formatToolArgValueCN 把参数值格式化为中文可读字符串。
func formatToolArgValueCN(value any) string {
	switch v := value.(type) {
	case string:
		text := strings.TrimSpace(v)
		if text == "" {
			return ""
		}
		return text
	case int:
		return strconv.Itoa(v)
	case int8:
		return strconv.Itoa(int(v))
	case int16:
		return strconv.Itoa(int(v))
	case int32:
		return strconv.Itoa(int(v))
	case int64:
		return strconv.Itoa(int(v))
	case float32:
		return strings.TrimSpace(strconv.FormatFloat(float64(v), 'f', -1, 32))
	case float64:
		return strings.TrimSpace(strconv.FormatFloat(v, 'f', -1, 64))
	case bool:
		if v {
			return "是"
		}
		return "否"
	case []any:
		values := make([]string, 0, len(v))
		for _, item := range v {
			text := formatToolArgValueCN(item)
			if text == "" {
				continue
			}
			values = append(values, text)
			if len(values) >= 3 {
				break
			}
		}
		return strings.Join(values, "、")
	default:
		if value == nil {
			return ""
		}
		text := strings.TrimSpace(fmt.Sprintf("%v", value))
		if text == "" || text == "<nil>" || text == "map[]" {
			return ""
		}
		return text
	}
}

// shouldForceFeasibilityNegotiation 判定是否需要先协商再继续写操作。
func shouldForceFeasibilityNegotiation(
	flowState *newagentmodel.CommonState,
	registry *newagenttools.ToolRegistry,
	toolName string,
) bool {
	if flowState == nil || registry == nil {
		return false
	}
	if !flowState.HealthCheckDone || flowState.HealthIsFeasible {
		return false
	}
	// 仅拦截“依赖日程状态”的写工具，避免影响 upsert_task_class 等独立写库能力。
	if !registry.IsWriteTool(toolName) || !registry.RequiresScheduleState(toolName) {
		return false
	}
	return true
}

// buildInfeasibleNegotiationQuestion 生成不可行场景下的协商提示。
func buildInfeasibleNegotiationQuestion(flowState *newagentmodel.CommonState) string {
	capacityGap := 0
	reasonCode := "capacity_insufficient"
	if flowState != nil {
		capacityGap = flowState.HealthCapacityGap
		if strings.TrimSpace(flowState.HealthReasonCode) != "" {
			reasonCode = strings.TrimSpace(flowState.HealthReasonCode)
		}
	}
	return fmt.Sprintf(
		"当前方案在现有约束下不可行（capacity_gap=%d，reason=%s），继续挪动任务无法消除根因。请告诉我你希望哪种处理方向：扩展时间窗、放宽约束、缩减范围/预算，或接受风险并先收口。",
		capacityGap,
		reasonCode,
	)
}

// buildInfeasibleBlockedResult 构造写工具被不可行约束拦截后的 observation。
func buildInfeasibleBlockedResult(flowState *newagentmodel.CommonState) string {
	capacityGap := 0
	reasonCode := "capacity_insufficient"
	if flowState != nil {
		capacityGap = flowState.HealthCapacityGap
		if strings.TrimSpace(flowState.HealthReasonCode) != "" {
			reasonCode = strings.TrimSpace(flowState.HealthReasonCode)
		}
	}
	return fmt.Sprintf(
		"已阻断本次写操作：analyze_health 判定当前约束不可行（capacity_gap=%d，reason=%s）。请先与用户协商：扩展时间窗 / 放宽约束 / 缩减范围或预算 / 接受风险收口。",
		capacityGap,
		reasonCode,
	)
}

type contextToolsResultEnvelope struct {
	Tool    string   `json:"tool"`
	Success bool     `json:"success"`
	Domain  string   `json:"domain,omitempty"`
	Packs   []string `json:"packs,omitempty"`
	Mode    string   `json:"mode,omitempty"`
	All     bool     `json:"all,omitempty"`
}

type analyzeHealthResultEnvelope struct {
	Tool        string                         `json:"tool"`
	Success     bool                           `json:"success"`
	Feasibility *analyzeHealthFeasibilityBrief `json:"feasibility,omitempty"`
	Decision    *analyzeHealthDecisionBrief    `json:"decision,omitempty"`
}

type analyzeHealthFeasibilityBrief struct {
	IsFeasible  bool   `json:"is_feasible"`
	CapacityGap int    `json:"capacity_gap"`
	ReasonCode  string `json:"reason_code"`
}

type analyzeHealthDecisionBrief struct {
	ShouldContinueOptimize bool   `json:"should_continue_optimize"`
	PrimaryProblem         string `json:"primary_problem,omitempty"`
	RecommendedOperation   string `json:"recommended_operation,omitempty"`
	IsForcedImperfection   bool   `json:"is_forced_imperfection"`
	ImprovementSignal      string `json:"improvement_signal,omitempty"`
}

type upsertTaskClassResultEnvelope struct {
	Tool       string                         `json:"tool"`
	Success    bool                           `json:"success"`
	Validation *upsertTaskClassValidationPart `json:"validation,omitempty"`
	Error      string                         `json:"error,omitempty"`
	ErrorCode  string                         `json:"error_code,omitempty"`
}

type upsertTaskClassValidationPart struct {
	OK     bool     `json:"ok"`
	Issues []string `json:"issues"`
}

// updateActiveToolDomainSnapshot 根据 context 管理工具结果回写激活工具域与二级包。
//
// 步骤化说明：
// 1. 仅处理 context_tools_add/remove，其他工具直接跳过；
// 2. 仅在 success=true 且结果可解析时更新，解析失败时保持旧值，避免误删关键域；
// 3. add 成功时覆盖域并写入 packs；remove 成功时按 all/domain/packs 精确回收。
func updateActiveToolDomainSnapshot(flowState *newagentmodel.CommonState, toolName string, result string) {
	if flowState == nil || !newagenttools.IsContextManagementTool(toolName) {
		return
	}

	var envelope contextToolsResultEnvelope
	if err := json.Unmarshal([]byte(result), &envelope); err != nil {
		return
	}
	if !envelope.Success {
		return
	}

	switch strings.TrimSpace(toolName) {
	case newagenttools.ToolNameContextToolsAdd:
		domain := newagenttools.NormalizeToolDomain(envelope.Domain)
		if domain == "" {
			return
		}
		nextPacks := newagenttools.ResolveEffectiveToolPacks(domain, envelope.Packs)
		mode := strings.ToLower(strings.TrimSpace(envelope.Mode))
		if mode == "merge" && newagenttools.NormalizeToolDomain(flowState.ActiveToolDomain) == domain {
			merged := make([]string, 0, len(flowState.ActiveToolPacks)+len(nextPacks))
			seen := make(map[string]struct{}, len(flowState.ActiveToolPacks)+len(nextPacks))
			current := newagenttools.ResolveEffectiveToolPacks(domain, flowState.ActiveToolPacks)
			for _, pack := range current {
				if _, exists := seen[pack]; exists {
					continue
				}
				seen[pack] = struct{}{}
				merged = append(merged, pack)
			}
			for _, pack := range nextPacks {
				if _, exists := seen[pack]; exists {
					continue
				}
				seen[pack] = struct{}{}
				merged = append(merged, pack)
			}
			nextPacks = merged
		}
		flowState.ActiveToolDomain = domain
		flowState.ActiveToolPacks = nextPacks
	case newagenttools.ToolNameContextToolsRemove:
		if envelope.All {
			flowState.ActiveToolDomain = ""
			flowState.ActiveToolPacks = nil
			return
		}
		domain := newagenttools.NormalizeToolDomain(envelope.Domain)
		if domain == "" {
			return
		}
		currentDomain := newagenttools.NormalizeToolDomain(flowState.ActiveToolDomain)
		if currentDomain != domain {
			return
		}

		removedPacks := newagenttools.NormalizeToolPacks(domain, envelope.Packs)
		if len(removedPacks) == 0 {
			flowState.ActiveToolDomain = ""
			flowState.ActiveToolPacks = nil
			return
		}

		currentEffective := newagenttools.ResolveEffectiveToolPacks(domain, flowState.ActiveToolPacks)
		if len(currentEffective) == 0 {
			flowState.ActiveToolDomain = ""
			flowState.ActiveToolPacks = nil
			return
		}

		removedSet := make(map[string]struct{}, len(removedPacks))
		for _, pack := range removedPacks {
			removedSet[pack] = struct{}{}
		}
		remaining := make([]string, 0, len(currentEffective))
		for _, pack := range currentEffective {
			if _, shouldRemove := removedSet[pack]; shouldRemove {
				continue
			}
			remaining = append(remaining, pack)
		}
		if len(remaining) == 0 {
			flowState.ActiveToolDomain = ""
			flowState.ActiveToolPacks = nil
			return
		}
		flowState.ActiveToolPacks = remaining
	}
}

// updateHealthFeasibilitySnapshot 从 analyze_health 的结构化返回中更新可行性快照。
func updateHealthFeasibilitySnapshot(flowState *newagentmodel.CommonState, toolName string, result string) {
	if flowState == nil || !strings.EqualFold(strings.TrimSpace(toolName), toolAnalyzeHealth) {
		return
	}

	// 先重置成“未知”状态，避免沿用旧快照误导后续决策。
	flowState.HealthCheckDone = false
	flowState.HealthIsFeasible = true
	flowState.HealthCapacityGap = 0
	flowState.HealthReasonCode = ""

	var envelope analyzeHealthResultEnvelope
	if err := json.Unmarshal([]byte(result), &envelope); err != nil {
		return
	}
	if !envelope.Success || envelope.Feasibility == nil {
		return
	}

	flowState.HealthCheckDone = true
	flowState.HealthIsFeasible = envelope.Feasibility.IsFeasible
	flowState.HealthCapacityGap = envelope.Feasibility.CapacityGap
	flowState.HealthReasonCode = strings.TrimSpace(envelope.Feasibility.ReasonCode)
}

// updateTaskClassUpsertSnapshot 从 upsert_task_class 返回中更新“任务类写入回盘”运行态。
//
// 步骤化说明：
// 1. 仅在工具名命中 upsert_task_class 时更新，避免污染其他链路；
// 2. 每次先标记 last_tried=true，再根据 success/validation 更新成功态与缺失项；
// 3. 连续失败计数仅用于软提示：成功归零，失败递增，不做硬拦截。
func updateTaskClassUpsertSnapshot(flowState *newagentmodel.CommonState, toolName string, result string) {
	if flowState == nil || !strings.EqualFold(strings.TrimSpace(toolName), "upsert_task_class") {
		return
	}

	flowState.TaskClassUpsertLastTried = true
	flowState.TaskClassUpsertLastSuccess = false
	flowState.TaskClassUpsertLastIssues = nil

	var envelope upsertTaskClassResultEnvelope
	if err := json.Unmarshal([]byte(result), &envelope); err != nil {
		flowState.TaskClassUpsertConsecutiveFailures++
		return
	}

	success := envelope.Success
	issues := make([]string, 0)
	if envelope.Validation != nil {
		issues = append(issues, parseAnyToStringSlice(any(envelope.Validation.Issues))...)
		if !envelope.Validation.OK {
			success = false
		}
	}
	if !success && strings.TrimSpace(envelope.Error) != "" && len(issues) == 0 {
		issues = append(issues, strings.TrimSpace(envelope.Error))
	}
	issues = uniqueNonEmptyStrings(issues)

	flowState.TaskClassUpsertLastSuccess = success
	flowState.TaskClassUpsertLastIssues = issues
	if success {
		flowState.TaskClassUpsertConsecutiveFailures = 0
		return
	}
	flowState.TaskClassUpsertConsecutiveFailures++
}

func uniqueNonEmptyStrings(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(values))
	result := make([]string, 0, len(values))
	for _, value := range values {
		text := strings.TrimSpace(value)
		if text == "" {
			continue
		}
		if _, exists := seen[text]; exists {
			continue
		}
		seen[text] = struct{}{}
		result = append(result, text)
	}
	return result
}

// updateHealthSnapshotV2 从 analyze_health 的结构化返回中同步“是否继续优化”的业务快照。
//
// 职责边界：
// 1. 只负责把 analyze_health 的关键结论回写到 CommonState，供 execute prompt 直接消费；
// 2. 不负责替 LLM 生成下一步参数，也不做写工具硬拦截；
// 3. 若结果解析失败，则回到保守默认值，避免沿用旧结论误导本轮判断。
func updateHealthSnapshotV2(flowState *newagentmodel.CommonState, toolName string, result string) {
	if flowState == nil || !strings.EqualFold(strings.TrimSpace(toolName), toolAnalyzeHealth) {
		return
	}

	prevSignal := strings.TrimSpace(flowState.HealthImprovementSignal)
	flowState.HealthCheckDone = false
	flowState.HealthIsFeasible = true
	flowState.HealthCapacityGap = 0
	flowState.HealthReasonCode = ""
	flowState.HealthShouldContinueOptimize = false
	flowState.HealthTightnessLevel = ""
	flowState.HealthPrimaryProblem = ""
	flowState.HealthRecommendedOperation = ""
	flowState.HealthIsForcedImperfection = false
	flowState.HealthImprovementSignal = ""

	var envelope struct {
		Success     bool                           `json:"success"`
		Feasibility *analyzeHealthFeasibilityBrief `json:"feasibility,omitempty"`
		Metrics     struct {
			Tightness *struct {
				TightnessLevel string `json:"tightness_level"`
			} `json:"tightness,omitempty"`
		} `json:"metrics"`
		Decision *analyzeHealthDecisionBrief `json:"decision,omitempty"`
	}
	if err := json.Unmarshal([]byte(result), &envelope); err != nil {
		flowState.HealthStagnationCount = 0
		return
	}
	if !envelope.Success || envelope.Feasibility == nil {
		flowState.HealthStagnationCount = 0
		return
	}

	flowState.HealthCheckDone = true
	flowState.HealthIsFeasible = envelope.Feasibility.IsFeasible
	flowState.HealthCapacityGap = envelope.Feasibility.CapacityGap
	flowState.HealthReasonCode = strings.TrimSpace(envelope.Feasibility.ReasonCode)
	if envelope.Metrics.Tightness != nil {
		flowState.HealthTightnessLevel = strings.TrimSpace(envelope.Metrics.Tightness.TightnessLevel)
	}
	if envelope.Decision != nil {
		flowState.HealthShouldContinueOptimize = envelope.Decision.ShouldContinueOptimize
		flowState.HealthPrimaryProblem = strings.TrimSpace(envelope.Decision.PrimaryProblem)
		flowState.HealthRecommendedOperation = strings.TrimSpace(envelope.Decision.RecommendedOperation)
		flowState.HealthIsForcedImperfection = envelope.Decision.IsForcedImperfection
		flowState.HealthImprovementSignal = strings.TrimSpace(envelope.Decision.ImprovementSignal)
	}
	if signal := strings.TrimSpace(flowState.HealthImprovementSignal); signal != "" && prevSignal != "" && signal == prevSignal {
		flowState.HealthStagnationCount++
		return
	}
	flowState.HealthStagnationCount = 0
}
