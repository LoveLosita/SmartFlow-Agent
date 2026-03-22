package schedulerefine

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/LoveLosita/smartflow/backend/model"
	"github.com/LoveLosita/smartflow/backend/respond"
	"github.com/cloudwego/eino-ext/components/model/ark"
	einoModel "github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
	arkModel "github.com/volcengine/volcengine-go-sdk/service/arkruntime/model"
)

const (
	// nodeTimeout 是单节点调用模型的超时预算。
	// 说明：这里给到 120s，避免复杂轮次在网络抖动时过早超时。
	nodeTimeout = 120 * time.Second
	// plannerMaxTokens 是 Planner 节点输出预算。
	// 说明：Planner 需要输出 steps/success_signals，预算过小会导致 JSON 被截断。
	plannerMaxTokens = 420
	// reactMaxTokens 是执行器单轮计划输出预算。
	// 说明：当 tool_calls 含 BatchMove 时，参数体更长，需要更高预算避免半截 JSON。
	reactMaxTokens = 480
)

const (
	// 说明：把 JSON 约束贴到 userPrompt 末尾，降低“系统提示词很长后模型偏离结构”的概率。
	// 1. 每个节点都使用最小必要字段约束，避免提示过重导致上下文负担变大；
	// 2. 要求“仅输出 JSON 对象”，减少 markdown/code fence 干扰；
	// 3. 放在上下文最后，尽量靠近模型最终解码位置。
	jsonContractForContract = `【输出协议（必须严格遵守）】
只输出单个 JSON 对象，不要输出 Markdown、代码块、解释文字。
必须包含键：intent, strategy, hard_requirements, keep_relative_order, order_scope, reason。`

	jsonContractForPlanner = `【输出协议（必须严格遵守）】
只输出单个 JSON 对象，不要输出 Markdown、代码块、解释文字。
必须包含键：summary, steps, success_signals, fallback。`

	jsonContractForReact = `【输出协议（必须严格遵守）】
只输出单个 JSON 对象，不要输出 Markdown、代码块、解释文字。
必须包含键：done, summary, goal_check, decision, missing_info, reflect, tool_calls。`

	jsonContractForReview = `【输出协议（必须严格遵守）】
只输出单个 JSON 对象，不要输出 Markdown、代码块、解释文字。
必须包含键：pass, reason, unmet。`

	jsonContractForPostReflect = `【输出协议（必须严格遵守）】
只输出单个 JSON 对象，不要输出 Markdown、代码块、解释文字。
必须包含键：reflection, next_strategy, should_stop, stop_reason。`
)

type contractOutput struct {
	Intent            string   `json:"intent"`
	Strategy          string   `json:"strategy"`
	HardRequirements  []string `json:"hard_requirements"`
	KeepRelativeOrder bool     `json:"keep_relative_order"`
	OrderScope        string   `json:"order_scope"`
	Reason            string   `json:"reason"`
}

// postReflectOutput 表示“动作执行后真反思”节点的结构化输出。
//
// 字段语义：
// 1. reflection：基于真实工具结果的复盘；
// 2. next_strategy：下一轮建议策略；
// 3. should_stop：是否建议结束动作循环；
// 4. stop_reason：建议结束的原因。
type postReflectOutput struct {
	Reflection   string `json:"reflection"`
	NextStrategy string `json:"next_strategy"`
	ShouldStop   bool   `json:"should_stop"`
	StopReason   string `json:"stop_reason"`
}

// plannerOutput 表示 Planner 阶段的结构化输出。
type plannerOutput struct {
	Summary        string   `json:"summary"`
	Steps          []string `json:"steps"`
	SuccessSignals []string `json:"success_signals"`
	Fallback       string   `json:"fallback"`
}

// runContractNode 执行“微调契约抽取”。
//
// 步骤化说明：
// 1. 先把用户本轮请求与当前排程摘要打包给模型，抽取结构化目标。
// 2. 再把模型输出映射到 state.Contract，作为后续动作与终审共同的判断基准。
// 3. 若模型失败或解析失败，使用保守兜底契约继续流程，避免整链路中断。
func runContractNode(
	ctx context.Context,
	chatModel *ark.ChatModel,
	st *ScheduleRefineState,
	emitStage func(stage, detail string),
) (*ScheduleRefineState, error) {
	if st == nil {
		return nil, fmt.Errorf("schedule refine: nil state in contract node")
	}
	if chatModel == nil {
		return nil, fmt.Errorf("schedule refine: model is nil in contract node")
	}

	emitStage("schedule_refine.contract.analyzing", "正在抽取本轮微调目标与硬性约束。")

	entryCount := len(st.HybridEntries)
	suggestedCount := countSuggested(st.HybridEntries)
	userPrompt := withNearestJSONContract(
		fmt.Sprintf(
			"当前时间（北京时间）=%s\n用户请求=%s\n当前排程条目数=%d\n可调 suggested 数=%d\n已有约束=%s\n历史摘要=%s",
			st.RequestNowText,
			strings.TrimSpace(st.UserMessage),
			entryCount,
			suggestedCount,
			strings.Join(st.Constraints, "；"),
			condenseSummary(st.CandidatePlans),
		),
		jsonContractForContract,
	)

	raw, err := callModelText(ctx, chatModel, contractPrompt, userPrompt, false, 260, 0)
	if err != nil {
		st.Contract = buildFallbackContract(st)
		st.UserIntent = st.Contract.Intent
		emitStage("schedule_refine.contract.fallback", "契约抽取失败，已按兜底策略继续微调。")
		return st, nil
	}
	emitModelRawDebug(emitStage, "contract", raw)

	parsed, parseErr := parseJSON[contractOutput](raw)
	if parseErr != nil {
		st.Contract = buildFallbackContract(st)
		st.UserIntent = st.Contract.Intent
		emitStage("schedule_refine.contract.fallback", fmt.Sprintf("契约解析失败，已按兜底策略继续微调：%s", truncate(parseErr.Error(), 180)))
		return st, nil
	}

	strategy := normalizeStrategy(parsed.Strategy)
	intent := strings.TrimSpace(parsed.Intent)
	if intent == "" {
		intent = strings.TrimSpace(st.UserMessage)
	}
	reason := strings.TrimSpace(parsed.Reason)
	if reason == "" {
		reason = "已根据本轮请求抽取微调契约。"
	}

	// 1. keep_relative_order 既接受模型判断，也允许基于用户原话兜底增强。
	// 2. 这样做的目的：避免模型偶发漏判“保持顺序”导致工具层约束缺失。
	keepRelativeOrder := parsed.KeepRelativeOrder || detectOrderIntent(st.UserMessage)
	orderScope := normalizeOrderScope(parsed.OrderScope)
	hardRequirements := append([]string(nil), parsed.HardRequirements...)
	if keepRelativeOrder {
		hardRequirements = append(hardRequirements, "保持任务原始相对顺序不变")
	}

	st.UserIntent = intent
	st.Contract = RefineContract{
		Intent:            intent,
		Strategy:          strategy,
		HardRequirements:  uniqueNonEmpty(hardRequirements),
		KeepRelativeOrder: keepRelativeOrder,
		OrderScope:        orderScope,
		Reason:            reason,
	}
	emitStage("schedule_refine.contract.done", fmt.Sprintf("契约抽取完成：strategy=%s, keep_relative_order=%t。", strategy, keepRelativeOrder))
	return st, nil
}

// runReactLoopNode 执行“强 ReAct 微调循环”。
//
// 步骤化说明：
// 1. 严格按 PlanMax/ExecuteMax/ReplanMax 控制规划与执行预算，并把 MaxRounds 对齐为 ExecuteMax+RepairReserve。
// 2. 每轮先输出“计划/缺口/动作/结果”，再触发一次“动作后真反思（post-reflect）”。
// 3. 每轮最多一个 tool_call（允许 BatchMove 在单调用内原子多步），失败也写入观察历史，驱动下一轮模型修正策略。
// 4. 当模型给出 done=true、post-reflect 建议停止、或动作预算耗尽时退出循环。
func runReactLoopNode(
	ctx context.Context,
	chatModel *ark.ChatModel,
	st *ScheduleRefineState,
	emitStage func(stage, detail string),
) (*ScheduleRefineState, error) {
	if st == nil {
		return nil, fmt.Errorf("schedule refine: nil state in react loop node")
	}
	if chatModel == nil {
		return nil, fmt.Errorf("schedule refine: model is nil in react loop node")
	}
	if len(st.HybridEntries) == 0 {
		st.ActionLogs = append(st.ActionLogs, "无可微调条目，跳过动作循环。")
		return st, nil
	}
	if st.PlanMax <= 0 {
		st.PlanMax = defaultPlanMax
	}
	if st.ExecuteMax <= 0 {
		st.ExecuteMax = defaultExecuteMax
	}
	if st.ReplanMax < 0 {
		st.ReplanMax = defaultReplanMax
	}
	if st.RepairReserve < 0 {
		st.RepairReserve = 0
	}
	st.MaxRounds = st.ExecuteMax + st.RepairReserve
	if st.RepairReserve >= st.MaxRounds {
		st.RepairReserve = 0
	}

	window := buildPlanningWindowFromEntries(st.HybridEntries)
	policy := refineToolPolicy{
		KeepRelativeOrder: st.Contract.KeepRelativeOrder,
		OrderScope:        st.Contract.OrderScope,
		OriginOrderMap:    st.OriginOrderMap,
	}
	emitStage(
		"schedule_refine.react.start",
		fmt.Sprintf("开始执行 Plan-and-Execute 微调，plan_max=%d，execute_max=%d，replan_max=%d，修复预留=%d。", st.PlanMax, st.ExecuteMax, st.ReplanMax, st.RepairReserve),
	)

	// 1. 先规划：Planner 决定“先取证还是先动作”，执行器按计划自由迭代。
	// 2. 规划失败时走后端兜底计划，保证链路可继续。
	if err := runPlannerNode(ctx, chatModel, st, emitStage, "initial"); err != nil {
		return st, err
	}

	for st.RoundUsed < st.ExecuteMax {
		round := st.RoundUsed + 1
		remainingAction := st.ExecuteMax - st.RoundUsed
		remainingTotal := st.MaxRounds - st.RoundUsed

		useThinking, reason := shouldEnableRecoveryThinking(st)
		emitStage("schedule_refine.react.round_start", fmt.Sprintf("第 %d 轮微调开始，动作剩余=%d，总剩余=%d。", round, remainingAction, remainingTotal))
		if useThinking {
			// 用户拍板要求：
			// 1. 默认关闭 thinking；
			// 2. 连续两次失败后，开启 1 轮 thinking，并把原因通过 SSE 透传给前端。
			emitStage("schedule_refine.react.reasoning_switch", fmt.Sprintf("第 %d 轮|已启用恢复性 thinking：%s", round, reason))
		}

		entriesJSON, _ := json.Marshal(st.HybridEntries)
		contractJSON, _ := json.Marshal(st.Contract)
		planJSON, _ := json.Marshal(st.CurrentPlan)
		observationText := buildObservationPrompt(st.ObservationHistory, 6)
		lastObservationText := buildLastToolObservationPrompt(st.ObservationHistory)
		lastFailedSignature := fallbackText(st.LastFailedCallSignature, "无")
		userPrompt := withNearestJSONContract(
			fmt.Sprintf(
				"用户本轮请求=%s\n契约=%s\n当前计划=%s\n已有约束=%s\n动作预算剩余=%d\n总预算剩余=%d\nLAST_TOOL_RESULT=%s\nLAST_TOOL_OBSERVATION=%s\nLAST_FAILED_CALL_SIGNATURE=%s\nLAST_POST_STRATEGY=%s\n历史观察=%s\nday_of_week映射=1周一,2周二,3周三,4周四,5周五,6周六,7周日\nsuggested简表=%s\n当前混合日程JSON=%s",
				strings.TrimSpace(st.UserMessage),
				string(contractJSON),
				string(planJSON),
				strings.Join(st.Constraints, "；"),
				remainingAction,
				remainingTotal,
				fallbackText(st.LastToolResult, "无"),
				lastObservationText,
				lastFailedSignature,
				fallbackText(st.LastPostStrategy, "无"),
				observationText,
				buildSuggestedDigest(st.HybridEntries, 80),
				string(entriesJSON),
			),
			jsonContractForReact,
		)

		// 1. ReAct 节点优先稳定性而非文风多样性：
		// 1.1 温度固定 0，降低“同约束下每轮输出漂移”与非结构化长输出概率；
		// 1.2 结合 parse_retry，可把“偶发半截 JSON”进一步压低。
		raw, err := callModelText(ctx, chatModel, reactPrompt, userPrompt, useThinking, reactMaxTokens, 0)
		if err != nil {
			errDetail := formatRoundModelErrorDetail(round, err, ctx)
			st.ActionLogs = append(st.ActionLogs, errDetail)
			emitStage("schedule_refine.react.round_error", errDetail)
			// 1. 若本轮前已产生过有效动作，则超时后不中断整链路。
			// 2. 这样可以避免“前面已调好一部分，后面一轮超时导致全盘失败”。
			if errors.Is(err, context.DeadlineExceeded) && st.RoundUsed > 0 {
				emitStage("schedule_refine.react.round_timeout_continue", fmt.Sprintf("第 %d 轮超时，已保留前序结果并继续终审。", round))
				break
			}
			return st, err
		}
		emitModelRawDebug(emitStage, fmt.Sprintf("react.round.%d.plan", round), raw)

		// 1. 解析重试策略：
		// 1.1 首次解析失败时，同轮再请求一次模型输出并再次解析；
		// 1.2 重试成功则继续后续动作，不影响本轮链路；
		// 1.3 二次解析仍失败时，返回统一业务错误码（respond 包），而不是裸 parseErr。
		parsed, parseErr := parseReactOutputWithRetryOnce(ctx, chatModel, userPrompt, raw, round, emitStage, st)
		if parseErr != nil {
			return st, parseErr
		}

		observation := ReactRoundObservation{
			Round:       round,
			GoalCheck:   strings.TrimSpace(parsed.GoalCheck),
			Decision:    strings.TrimSpace(parsed.Decision),
			MissingInfo: append([]string(nil), parsed.MissingInfo...),
			// 这里先记录“计划备注（动作前）”，执行工具后会用 post-reflect 的真反思覆盖。
			Reflect: strings.TrimSpace(parsed.Reflect),
		}

		emitStage("schedule_refine.react.plan", formatReactPlanStageDetail(round, parsed, remainingAction, useThinking))
		if useThinking {
			emitStage("schedule_refine.react.reasoning_content", fmt.Sprintf("第 %d 轮思考摘要：%s", round, truncate(strings.TrimSpace(parsed.Decision), 180)))
		}
		emitStage("schedule_refine.react.need_info", formatReactNeedInfoStageDetail(round, parsed.MissingInfo))

		if parsed.Done {
			doneReason := fallbackText(strings.TrimSpace(parsed.Summary), "模型判定当前方案已满足目标。")
			st.ActionLogs = append(st.ActionLogs, fmt.Sprintf("第 %d 轮主动结束：%s", round, doneReason))
			observation.Reflect = fallbackText(observation.Reflect, doneReason)
			st.ObservationHistory = append(st.ObservationHistory, observation)
			emitStage("schedule_refine.react.reflect", formatReactReflectStageDetail(round, observation.Reflect))
			emitStage("schedule_refine.react.round_done", fmt.Sprintf("第 %d 轮结束：模型返回 done=true。", round))
			break
		}

		call, warn := pickSingleToolCall(parsed.ToolCalls)
		if warn != "" {
			st.ActionLogs = append(st.ActionLogs, fmt.Sprintf("第 %d 轮告警：%s", round, warn))
			emitStage("schedule_refine.react.round_warn", fmt.Sprintf("第 %d 轮告警：%s", round, warn))
		}
		if call == nil {
			st.ActionLogs = append(st.ActionLogs, fmt.Sprintf("第 %d 轮无可执行动作，结束微调。", round))
			observation.Reflect = fallbackText(observation.Reflect, "本轮未生成可执行工具动作。")
			st.ObservationHistory = append(st.ObservationHistory, observation)
			emitStage("schedule_refine.react.reflect", formatReactReflectStageDetail(round, observation.Reflect))
			emitStage("schedule_refine.react.round_done", fmt.Sprintf("第 %d 轮无动作，流程结束。", round))
			break
		}

		emitStage("schedule_refine.react.tool_call", formatToolCallStageDetail(round, *call, remainingAction))

		callSignature := buildToolCallSignature(*call)
		if isRepeatedFailedCall(st, callSignature) {
			// 1. 后端硬兜底：
			// 1.1 若本轮动作与“上一轮失败动作签名”完全一致，直接拒绝执行，防止模型在同一坑位空转；
			// 1.2 该失败会结构化写回上下文，驱动下一轮明确改道（换时段或改用 Swap）。
			result := normalizeToolResult(reactToolResult{
				Tool:      strings.TrimSpace(call.Tool),
				Success:   false,
				ErrorCode: "REPEAT_FAILED_ACTION",
				Result:    "重复失败动作：与上一轮失败动作完全相同，请更换目标时段或改用 Swap。",
			})
			st.RoundUsed++
			st.LastToolResult = formatStructuredToolResult(result)
			st.LastFailedCallSignature = callSignature
			st.ConsecutiveFailures++

			observation.ToolName = strings.TrimSpace(result.Tool)
			observation.ToolParams = cloneToolParams(call.Params)
			observation.ToolSuccess = result.Success
			observation.ToolErrorCode = strings.TrimSpace(result.ErrorCode)
			observation.ToolResult = strings.TrimSpace(result.Result)
			postReflectText, nextStrategy, shouldStop := runPostReflectAfterTool(ctx, chatModel, st, round, parsed, call, result, emitStage)
			observation.Reflect = postReflectText
			st.ObservationHistory = append(st.ObservationHistory, observation)

			st.ActionLogs = append(st.ActionLogs, fmt.Sprintf("第 %d 轮动作被拒绝：tool=%s error_code=%s detail=%s", round, result.Tool, result.ErrorCode, result.Result))
			emitStage("schedule_refine.react.tool_blocked", fmt.Sprintf("第 %d 轮|检测到重复失败动作，已拒绝执行并要求模型改道。", round))
			emitStage("schedule_refine.react.tool_result", formatToolResultStageDetail(round, result, st.RoundUsed, st.MaxRounds))
			emitStage("schedule_refine.react.reflect", formatReactReflectStageDetail(round, observation.Reflect))
			st.LastPostStrategy = fallbackText(nextStrategy, st.LastPostStrategy)
			if shouldTriggerReplan(st, result) {
				if replanned, err := tryReplan(ctx, chatModel, st, emitStage); err != nil {
					return st, err
				} else if replanned {
					continue
				}
			}
			if shouldStop {
				emitStage("schedule_refine.react.round_done", fmt.Sprintf("第 %d 轮结束：post-reflect 建议停止。", round))
				break
			}
			continue
		}

		nextEntries, rawResult := dispatchRefineTool(cloneHybridEntries(st.HybridEntries), *call, window, policy)
		result := normalizeToolResult(rawResult)
		st.RoundUsed++
		st.LastToolResult = formatStructuredToolResult(result)

		observation.ToolName = strings.TrimSpace(result.Tool)
		observation.ToolParams = cloneToolParams(call.Params)
		observation.ToolSuccess = result.Success
		observation.ToolErrorCode = strings.TrimSpace(result.ErrorCode)
		observation.ToolResult = strings.TrimSpace(result.Result)
		postReflectText, nextStrategy, shouldStop := runPostReflectAfterTool(ctx, chatModel, st, round, parsed, call, result, emitStage)
		observation.Reflect = postReflectText
		st.ObservationHistory = append(st.ObservationHistory, observation)

		st.ActionLogs = append(st.ActionLogs, fmt.Sprintf("第 %d 轮动作：tool=%s success=%t detail=%s", round, result.Tool, result.Success, result.Result))
		emitStage("schedule_refine.react.tool_result", formatToolResultStageDetail(round, result, st.RoundUsed, st.MaxRounds))
		emitStage("schedule_refine.react.reflect", formatReactReflectStageDetail(round, observation.Reflect))
		st.LastPostStrategy = fallbackText(nextStrategy, st.LastPostStrategy)

		if result.Success {
			st.HybridEntries = nextEntries
			window = buildPlanningWindowFromEntries(st.HybridEntries)
			st.LastFailedCallSignature = ""
			st.ConsecutiveFailures = 0
			st.ThinkingBoostArmed = false
		} else {
			st.LastFailedCallSignature = callSignature
			st.ConsecutiveFailures++
			if shouldTriggerReplan(st, result) {
				if replanned, err := tryReplan(ctx, chatModel, st, emitStage); err != nil {
					return st, err
				} else if replanned {
					continue
				}
			}
		}
		if shouldStop {
			emitStage("schedule_refine.react.round_done", fmt.Sprintf("第 %d 轮结束：post-reflect 建议停止。", round))
			break
		}
	}

	emitStage("schedule_refine.react.done", fmt.Sprintf("Plan-and-Execute 微调结束，已执行动作轮次=%d，重规划次数=%d。", st.RoundUsed, st.ReplanUsed))
	return st, nil
}

// runPlannerNode 执行一次 Planner 规划。
//
// 步骤化说明：
// 1. 读取当前约束、最近观察、失败上下文，生成结构化执行计划；
// 2. 规划失败时使用后端兜底计划，保证执行器仍可继续；
// 3. mode=initial/replan 仅用于阶段展示和日志区分。
func runPlannerNode(
	ctx context.Context,
	chatModel *ark.ChatModel,
	st *ScheduleRefineState,
	emitStage func(stage, detail string),
	mode string,
) error {
	if st == nil || chatModel == nil {
		return fmt.Errorf("planner: invalid input")
	}
	if st.PlanUsed >= st.PlanMax {
		return nil
	}
	stage := "schedule_refine.plan.generating"
	if strings.TrimSpace(mode) == "replan" {
		stage = "schedule_refine.plan.regenerating"
	}
	emitStage(stage, fmt.Sprintf("正在生成执行计划（mode=%s，已用%d/%d）。", mode, st.PlanUsed, st.PlanMax))

	contractJSON, _ := json.Marshal(st.Contract)
	observationText := buildObservationPrompt(st.ObservationHistory, 6)
	userPrompt := withNearestJSONContract(
		fmt.Sprintf(
			"mode=%s\n用户请求=%s\n契约=%s\n已有约束=%s\n上一轮工具结果=%s\n上一轮策略=%s\n最近观察=%s\nsuggested简表=%s",
			mode,
			strings.TrimSpace(st.UserMessage),
			string(contractJSON),
			strings.Join(st.Constraints, "；"),
			fallbackText(st.LastToolResult, "无"),
			fallbackText(st.LastPostStrategy, "无"),
			observationText,
			buildSuggestedDigest(st.HybridEntries, 80),
		),
		jsonContractForPlanner,
	)

	raw, err := callModelText(ctx, chatModel, plannerPrompt, userPrompt, false, plannerMaxTokens, 0)
	if err != nil {
		st.CurrentPlan = buildFallbackPlan(st)
		st.PlanUsed++
		st.ActionLogs = append(st.ActionLogs, fmt.Sprintf("Planner 调用失败，已使用兜底计划：%v", err))
		emitStage("schedule_refine.plan.fallback", "Planner 调用失败，已切换后端兜底计划。")
		return nil
	}
	emitModelRawDebug(emitStage, fmt.Sprintf("planner.%s", mode), raw)

	parsed, parseErr := parsePlannerOutputWithRetryOnce(ctx, chatModel, userPrompt, raw, mode, emitStage)
	if parseErr != nil {
		st.CurrentPlan = buildFallbackPlan(st)
		st.PlanUsed++
		st.ActionLogs = append(st.ActionLogs, fmt.Sprintf("Planner 解析失败，已使用兜底计划：%v", parseErr))
		emitStage("schedule_refine.plan.fallback", fmt.Sprintf("Planner 输出解析失败，已切换后端兜底计划：%s", truncate(parseErr.Error(), 180)))
		return nil
	}

	st.CurrentPlan = PlannerPlan{
		Summary:        fallbackText(strings.TrimSpace(parsed.Summary), "已生成可执行计划。"),
		Steps:          uniqueNonEmpty(parsed.Steps),
		SuccessSignals: uniqueNonEmpty(parsed.SuccessSignals),
		Fallback:       strings.TrimSpace(parsed.Fallback),
	}
	st.PlanUsed++
	emitStage("schedule_refine.plan.done", fmt.Sprintf("规划完成：%s", truncate(st.CurrentPlan.Summary, 180)))
	return nil
}

// buildFallbackPlan 构造“Planner 失败时兜底计划”。
func buildFallbackPlan(st *ScheduleRefineState) PlannerPlan {
	summary := "兜底计划：先取证再动作，优先原子批量移动，失败后改道。"
	if st != nil && st.Contract.KeepRelativeOrder {
		summary = "兜底计划：先取证再动作，严格保持相对顺序，优先原子批量移动。"
	}
	return PlannerPlan{
		Summary: summary,
		Steps: []string{
			"1) 调用 QueryTargetTasks 定位目标任务",
			"2) 调用 QueryAvailableSlots 获取可用时段",
			"3) 优先尝试 BatchMove，失败后改用 Move/Swap",
			"4) 收尾前调用 Verify 做确定性自检",
		},
		SuccessSignals: []string{
			"工具动作成功且无冲突",
			"Verify 通过",
		},
		Fallback: "若连续失败，重规划并更换工具路径。",
	}
}

// shouldEnableRecoveryThinking 判断本轮是否触发“失败兜底 thinking”。
//
// 规则：
// 1. 默认关闭 thinking；
// 2. 连续失败达到 2 次时，仅开启 1 轮 thinking；
// 3. 在同一失败串里只触发一次，直到出现成功再重置。
func shouldEnableRecoveryThinking(st *ScheduleRefineState) (bool, string) {
	if st == nil {
		return false, ""
	}
	if st.ConsecutiveFailures < 2 {
		return false, ""
	}
	if st.ThinkingBoostArmed {
		return false, ""
	}
	st.ThinkingBoostArmed = true
	return true, fmt.Sprintf("连续失败=%d，触发1轮恢复性 thinking", st.ConsecutiveFailures)
}

// shouldTriggerReplan 判断是否应该进入重规划。
//
// 触发条件：
// 1. 连续失败 >=3；
// 2. 且错误码属于“路径错误类”（冲突/顺序/重复失败/参数缺失/批量失败）。
func shouldTriggerReplan(st *ScheduleRefineState, result reactToolResult) bool {
	if st == nil {
		return false
	}
	if st.ConsecutiveFailures < 3 {
		return false
	}
	switch strings.TrimSpace(result.ErrorCode) {
	case "SLOT_CONFLICT", "ORDER_VIOLATION", "REPEAT_FAILED_ACTION", "PARAM_MISSING", "BATCH_MOVE_FAILED", "VERIFY_FAILED":
		return true
	default:
		return false
	}
}

// tryReplan 在满足条件时触发一次重规划。
func tryReplan(
	ctx context.Context,
	chatModel *ark.ChatModel,
	st *ScheduleRefineState,
	emitStage func(stage, detail string),
) (bool, error) {
	if st == nil {
		return false, nil
	}
	if st.ReplanUsed >= st.ReplanMax {
		return false, nil
	}
	if st.PlanUsed >= st.PlanMax {
		return false, nil
	}
	st.ReplanUsed++
	emitStage("schedule_refine.plan.replan_trigger", fmt.Sprintf("连续失败=%d，触发重规划（%d/%d）。", st.ConsecutiveFailures, st.ReplanUsed, st.ReplanMax))
	if err := runPlannerNode(ctx, chatModel, st, emitStage, "replan"); err != nil {
		return true, err
	}
	// 1. 重规划后重置失败串，避免刚重规划就再次被失败门槛立即打断；
	// 2. 同时允许后续再次触发一次 thinking 兜底。
	st.ConsecutiveFailures = 0
	st.ThinkingBoostArmed = false
	return true, nil
}

// runHardCheckNode 执行“物理校验 + 顺序校验 + 语义校验 + 单次修复”。
func runHardCheckNode(
	ctx context.Context,
	chatModel *ark.ChatModel,
	st *ScheduleRefineState,
	emitStage func(stage, detail string),
) (*ScheduleRefineState, error) {
	if st == nil {
		return nil, fmt.Errorf("schedule refine: nil state in hard check node")
	}
	if chatModel == nil {
		return nil, fmt.Errorf("schedule refine: model is nil in hard check node")
	}

	emitStage("schedule_refine.hard_check.start", "正在执行终审硬校验。")
	report := evaluateHardChecks(ctx, chatModel, st, emitStage)
	st.HardCheck = report

	if report.PhysicsPassed && report.OrderPassed && report.IntentPassed {
		emitStage("schedule_refine.hard_check.pass", "终审通过。")
		return st, nil
	}

	if st.RoundUsed >= st.MaxRounds {
		emitStage("schedule_refine.hard_check.fail", "终审未通过，且动作预算已耗尽，无法继续修复。")
		return st, nil
	}

	emitStage("schedule_refine.hard_check.repairing", "终审未通过，正在尝试一次修复动作。")
	st.HardCheck.RepairTried = true
	if err := runSingleRepairAction(ctx, chatModel, st, emitStage); err != nil {
		st.ActionLogs = append(st.ActionLogs, fmt.Sprintf("修复动作失败：%v", err))
		emitStage("schedule_refine.hard_check.fail", "修复动作失败，保留当前方案。")
		return st, nil
	}

	report = evaluateHardChecks(ctx, chatModel, st, emitStage)
	report.RepairTried = true
	st.HardCheck = report
	if report.PhysicsPassed && report.OrderPassed && report.IntentPassed {
		emitStage("schedule_refine.hard_check.pass", "修复后终审通过。")
		return st, nil
	}

	emitStage("schedule_refine.hard_check.fail", "修复后仍未完全满足要求，已返回当前最优结果。")
	return st, nil
}

// runSummaryNode 生成最终用户可读总结，并回填结构化预览字段。
func runSummaryNode(
	ctx context.Context,
	chatModel *ark.ChatModel,
	st *ScheduleRefineState,
	emitStage func(stage, detail string),
) (*ScheduleRefineState, error) {
	if st == nil {
		return nil, fmt.Errorf("schedule refine: nil state in summary node")
	}
	if chatModel == nil {
		return nil, fmt.Errorf("schedule refine: model is nil in summary node")
	}

	emitStage("schedule_refine.summary.generating", "正在生成微调结果总结。")

	updateAllocatedItemsFromEntries(st)
	st.CandidatePlans = hybridEntriesToWeekSchedules(st.HybridEntries)

	reportJSON, _ := json.Marshal(st.HardCheck)
	actionLogText := summarizeActionLogs(st.ActionLogs, 24)
	contractJSON, _ := json.Marshal(st.Contract)
	userPrompt := fmt.Sprintf(
		"用户请求=%s\n契约=%s\n终审报告=%s\n动作日志=%s",
		strings.TrimSpace(st.UserMessage),
		string(contractJSON),
		string(reportJSON),
		actionLogText,
	)

	raw, err := callModelText(ctx, chatModel, summaryPrompt, userPrompt, false, 280, 0.35)
	summary := strings.TrimSpace(raw)
	if err == nil {
		emitModelRawDebug(emitStage, "summary", raw)
	}
	if err != nil || summary == "" {
		if st.HardCheck.PhysicsPassed && st.HardCheck.OrderPassed && st.HardCheck.IntentPassed {
			summary = fmt.Sprintf("微调已完成，共执行 %d 轮动作。当前方案已通过终审校验，可以继续使用。", st.RoundUsed)
		} else {
			summary = fmt.Sprintf("已完成微调并返回当前最优结果（执行 %d 轮动作）。终审仍有未满足项：%s。", st.RoundUsed, fallbackText(st.HardCheck.IntentReason, "请进一步明确你的微调要求"))
		}
	}

	st.FinalSummary = summary
	st.Completed = true
	emitStage("schedule_refine.summary.done", "微调总结已生成。")
	return st, nil
}

// evaluateHardChecks 执行一次完整硬校验（物理 + 顺序 + 语义）。
func evaluateHardChecks(ctx context.Context, chatModel *ark.ChatModel, st *ScheduleRefineState, emitStage func(stage, detail string)) HardCheckReport {
	report := HardCheckReport{}

	report.PhysicsIssues = physicsCheck(st.HybridEntries, len(st.AllocatedItems))
	report.PhysicsPassed = len(report.PhysicsIssues) == 0

	report.OrderIssues = validateRelativeOrder(st.HybridEntries, refineToolPolicy{
		KeepRelativeOrder: st.Contract.KeepRelativeOrder,
		OrderScope:        st.Contract.OrderScope,
		OriginOrderMap:    st.OriginOrderMap,
	})
	report.OrderPassed = len(report.OrderIssues) == 0

	review, err := runSemanticReview(ctx, chatModel, st, emitStage)
	if err != nil {
		report.IntentPassed = false
		report.IntentReason = fmt.Sprintf("语义校验失败：%v", err)
		report.IntentUnmet = []string{"语义校验阶段异常"}
		return report
	}
	report.IntentPassed = review.Pass
	report.IntentReason = strings.TrimSpace(review.Reason)
	report.IntentUnmet = append([]string(nil), review.Unmet...)
	return report
}

// runSingleRepairAction 在终审失败后执行一次修复动作。
func runSingleRepairAction(ctx context.Context, chatModel *ark.ChatModel, st *ScheduleRefineState, emitStage func(stage, detail string)) error {
	if st == nil {
		return fmt.Errorf("nil state")
	}
	if chatModel == nil {
		return fmt.Errorf("nil model")
	}
	if st.RoundUsed >= st.MaxRounds {
		return fmt.Errorf("动作预算已耗尽")
	}

	entriesJSON, _ := json.Marshal(st.HybridEntries)
	contractJSON, _ := json.Marshal(st.Contract)
	userPrompt := withNearestJSONContract(
		fmt.Sprintf(
			"用户请求=%s\n契约=%s\n未满足点=%s\n当前混合日程JSON=%s",
			strings.TrimSpace(st.UserMessage),
			string(contractJSON),
			strings.Join(st.HardCheck.IntentUnmet, "；"),
			string(entriesJSON),
		),
		jsonContractForReact,
	)

	raw, err := callModelText(ctx, chatModel, repairPrompt, userPrompt, false, 240, 0.15)
	if err != nil {
		return err
	}
	emitModelRawDebug(emitStage, "repair", raw)
	parsed, parseErr := parseReactLLMOutput(raw)
	if parseErr != nil {
		return parseErr
	}

	call, warn := pickSingleToolCall(parsed.ToolCalls)
	if warn != "" {
		st.ActionLogs = append(st.ActionLogs, "修复阶段告警："+warn)
	}
	if call == nil {
		return fmt.Errorf("修复阶段未给出可执行动作")
	}
	emitStage("schedule_refine.hard_check.repair_call", formatToolCallStageDetail(st.RoundUsed+1, *call, st.MaxRounds-st.RoundUsed))

	policy := refineToolPolicy{
		KeepRelativeOrder: st.Contract.KeepRelativeOrder,
		OrderScope:        st.Contract.OrderScope,
		OriginOrderMap:    st.OriginOrderMap,
	}
	nextEntries, result := dispatchRefineTool(cloneHybridEntries(st.HybridEntries), *call, buildPlanningWindowFromEntries(st.HybridEntries), policy)
	result = normalizeToolResult(result)
	st.RoundUsed++
	st.LastToolResult = formatStructuredToolResult(result)
	st.ActionLogs = append(st.ActionLogs, fmt.Sprintf("修复动作：tool=%s success=%t detail=%s", result.Tool, result.Success, result.Result))
	emitStage("schedule_refine.hard_check.repair_result", formatToolResultStageDetail(st.RoundUsed, result, st.RoundUsed, st.MaxRounds))

	if !result.Success {
		st.LastFailedCallSignature = buildToolCallSignature(*call)
		return fmt.Errorf("修复动作执行失败：%s", result.Result)
	}
	st.LastFailedCallSignature = ""
	st.HybridEntries = nextEntries
	return nil
}

// runSemanticReview 通过模型判断“当前方案是否满足用户本轮目标”。
func runSemanticReview(ctx context.Context, chatModel *ark.ChatModel, st *ScheduleRefineState, emitStage func(stage, detail string)) (*reviewOutput, error) {
	entriesJSON, _ := json.Marshal(st.HybridEntries)
	contractJSON, _ := json.Marshal(st.Contract)
	userPrompt := withNearestJSONContract(
		fmt.Sprintf(
			"用户请求=%s\n契约=%s\nday_of_week映射=1周一,2周二,3周三,4周四,5周五,6周六,7周日\nsuggested简表=%s\n动作日志=%s\n当前混合日程JSON=%s",
			strings.TrimSpace(st.UserMessage),
			string(contractJSON),
			buildSuggestedDigest(st.HybridEntries, 80),
			summarizeActionLogs(st.ActionLogs, 12),
			string(entriesJSON),
		),
		jsonContractForReview,
	)
	raw, err := callModelText(ctx, chatModel, reviewPrompt, userPrompt, false, 240, 0)
	if err != nil {
		return nil, err
	}
	emitModelRawDebug(emitStage, "review", raw)
	return parseReviewOutput(raw)
}

// runPostReflectAfterTool 执行“工具动作后的真反思”。
//
// 步骤化说明：
// 1. 输入本轮计划、工具调用参数、后端真实工具结果；
// 2. 调用专用 postReflectPrompt，让模型基于真实结果给出复盘与下一步策略；
// 3. 解析失败时使用后端兜底复盘文本，保证链路不被“反思失败”拖垮；
// 4. 返回反思文本与 shouldStop 标记，供主循环决定是否提前结束。
func runPostReflectAfterTool(
	ctx context.Context,
	chatModel *ark.ChatModel,
	st *ScheduleRefineState,
	round int,
	plan *reactLLMOutput,
	call *reactToolCall,
	result reactToolResult,
	emitStage func(stage, detail string),
) (string, string, bool) {
	if st == nil || chatModel == nil || call == nil {
		return buildPostReflectFallback(plan, result), "", false
	}

	emitStage("schedule_refine.react.post_reflect.start", fmt.Sprintf("第 %d 轮|正在基于工具真实结果进行反思。", round))

	contractJSON, _ := json.Marshal(st.Contract)
	callJSON, _ := json.Marshal(call)
	resultJSON, _ := json.Marshal(result)
	planGoal := ""
	planDecision := ""
	planNote := ""
	if plan != nil {
		planGoal = strings.TrimSpace(plan.GoalCheck)
		planDecision = strings.TrimSpace(plan.Decision)
		planNote = strings.TrimSpace(plan.Reflect)
	}
	userPrompt := withNearestJSONContract(
		fmt.Sprintf(
			"用户请求=%s\n契约=%s\n本轮计划.goal_check=%s\n本轮计划.decision=%s\n本轮计划.note=%s\n本轮工具调用=%s\n本轮工具结果=%s\n最近观察=%s\n",
			strings.TrimSpace(st.UserMessage),
			string(contractJSON),
			planGoal,
			planDecision,
			planNote,
			string(callJSON),
			string(resultJSON),
			buildObservationPrompt(st.ObservationHistory, 4),
		),
		jsonContractForPostReflect,
	)

	raw, err := callModelText(ctx, chatModel, postReflectPrompt, userPrompt, false, 220, 0)
	if err != nil {
		fallback := buildPostReflectFallback(plan, result)
		emitStage("schedule_refine.react.post_reflect.fallback", fmt.Sprintf("第 %d 轮|模型反思失败，改用后端兜底复盘：%s", round, truncate(err.Error(), 160)))
		return fallback, "", false
	}
	emitModelRawDebug(emitStage, fmt.Sprintf("post_reflect.round.%d", round), raw)

	parsed, parseErr := parseJSON[postReflectOutput](raw)
	if parseErr != nil {
		fallback := buildPostReflectFallback(plan, result)
		emitStage("schedule_refine.react.post_reflect.fallback", fmt.Sprintf("第 %d 轮|模型反思解析失败，改用后端兜底复盘：%s", round, truncate(parseErr.Error(), 160)))
		return fallback, "", false
	}

	reflection := strings.TrimSpace(parsed.Reflection)
	if reflection == "" {
		reflection = buildPostReflectFallback(plan, result)
	}
	nextStrategy := strings.TrimSpace(parsed.NextStrategy)
	if nextStrategy != "" {
		reflection = fmt.Sprintf("%s；下一步建议：%s", reflection, nextStrategy)
	}
	shouldStop := parsed.ShouldStop
	stopReason := strings.TrimSpace(parsed.StopReason)
	if shouldStop {
		if stopReason == "" {
			stopReason = "模型判定继续动作收益较低，建议转终审。"
		}
		reflection = fmt.Sprintf("%s；停止建议：%s", reflection, stopReason)
	}
	emitStage(
		"schedule_refine.react.post_reflect.done",
		fmt.Sprintf("第 %d 轮|模型反思=%s|下一步=%s|should_stop=%t", round, truncate(strings.TrimSpace(parsed.Reflection), 120), truncate(nextStrategy, 120), shouldStop),
	)
	return reflection, nextStrategy, shouldStop
}

// buildPostReflectFallback 生成“动作后真反思”的后端兜底文案。
//
// 说明：
// 1. 当 post-reflect 模型调用/解析失败时，仍需给前端可解释文本；
// 2. 兜底文本以真实工具结果为主，计划备注仅作补充；
// 3. 该函数不决定 shouldStop，只负责生成可读复盘。
func buildPostReflectFallback(plan *reactLLMOutput, result reactToolResult) string {
	planNote := ""
	if plan != nil {
		planNote = strings.TrimSpace(plan.Reflect)
	}
	return buildRuntimeReflect(planNote, result)
}

// callModelText 统一封装模型调用，避免各节点重复拼装参数。
func callModelText(
	ctx context.Context,
	chatModel *ark.ChatModel,
	systemPrompt string,
	userPrompt string,
	useThinking bool,
	maxTokens int,
	temperature float32,
) (string, error) {
	if chatModel == nil {
		return "", fmt.Errorf("model is nil")
	}
	nodeCtx, cancel := context.WithTimeout(ctx, nodeTimeout)
	defer cancel()

	thinkingType := arkModel.ThinkingTypeDisabled
	if useThinking {
		thinkingType = arkModel.ThinkingTypeEnabled
	}
	opts := []einoModel.Option{
		ark.WithThinking(&arkModel.Thinking{Type: thinkingType}),
		einoModel.WithTemperature(temperature),
	}
	if maxTokens > 0 {
		opts = append(opts, einoModel.WithMaxTokens(maxTokens))
	}

	resp, err := chatModel.Generate(nodeCtx, []*schema.Message{
		schema.SystemMessage(systemPrompt),
		schema.UserMessage(userPrompt),
	}, opts...)
	if err != nil {
		if errors.Is(nodeCtx.Err(), context.DeadlineExceeded) {
			return "", fmt.Errorf("model call node timeout(%dms): %w", nodeTimeout.Milliseconds(), err)
		}
		if nodeCtx.Err() != nil {
			return "", fmt.Errorf("model call node canceled(%v): %w", nodeCtx.Err(), err)
		}
		if ctx.Err() != nil {
			return "", fmt.Errorf("model call parent canceled(%v): %w", ctx.Err(), err)
		}
		return "", err
	}
	if resp == nil {
		return "", fmt.Errorf("model response is nil")
	}
	content := strings.TrimSpace(resp.Content)
	if content == "" {
		return "", fmt.Errorf("model response content is empty")
	}
	return content, nil
}

// parseJSON 是通用 JSON 解析器，兼容 markdown code fence。
func parseJSON[T any](raw string) (*T, error) {
	clean := strings.TrimSpace(raw)
	if clean == "" {
		return nil, fmt.Errorf("empty response")
	}
	if strings.HasPrefix(clean, "```") {
		clean = strings.TrimPrefix(clean, "```json")
		clean = strings.TrimPrefix(clean, "```")
		clean = strings.TrimSuffix(clean, "```")
		clean = strings.TrimSpace(clean)
	}
	var out T
	if err := json.Unmarshal([]byte(clean), &out); err == nil {
		return &out, nil
	}
	obj, err := extractFirstJSONObject(clean)
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal([]byte(obj), &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// extractFirstJSONObject 从文本中提取“第一个完整 JSON 对象”。
//
// 设计说明：
// 1. 相比“first { + last }”的粗糙截取，这里使用括号配对，避免模型输出多段文本时误截；
// 2. 兼容字符串内大括号（通过字符串状态机跳过）；
// 3. 提取失败时返回明确错误，便于上层阶段日志提示。
func extractFirstJSONObject(text string) (string, error) {
	start := strings.Index(text, "{")
	if start < 0 {
		return "", fmt.Errorf("no json object found")
	}
	depth := 0
	inString := false
	escape := false
	for i := start; i < len(text); i++ {
		ch := text[i]
		if inString {
			if escape {
				escape = false
				continue
			}
			if ch == '\\' {
				escape = true
				continue
			}
			if ch == '"' {
				inString = false
			}
			continue
		}
		if ch == '"' {
			inString = true
			continue
		}
		if ch == '{' {
			depth++
			continue
		}
		if ch == '}' {
			depth--
			if depth == 0 {
				return text[start : i+1], nil
			}
		}
	}
	return "", fmt.Errorf("json object not closed")
}

// emitModelRawDebug 统一输出模型原始文本到 SSE 调试阶段。
//
// 规则：
// 1. 所有模型节点都可调用该函数输出原始 raw，帮助定位解析失败；
// 2. detail 统一带 `[debug][tag]` 前缀，满足前端快速筛选；
// 3. 当 raw 过长时，按分片逐条输出，避免“单条截断导致看起来像 JSON 不闭合”的误判。
func emitModelRawDebug(emitStage func(stage, detail string), tag string, raw string) {
	if emitStage == nil {
		return
	}
	clean := strings.TrimSpace(raw)
	if clean == "" {
		clean = "<empty>"
	}

	// 1. 这里按 rune 分片而不是按 byte 分片，避免中文被截断后出现乱码。
	// 2. 每片控制在较小体量，降低 SSE 单条过大造成前端展示异常或丢帧。
	// 3. 分片时携带 part 序号，便于前端/日志侧拼接复盘完整 raw。
	const chunkSize = 1600
	tag = strings.TrimSpace(tag)
	runes := []rune(clean)
	if len(runes) <= chunkSize {
		emitStage("schedule_refine.debug.raw", fmt.Sprintf("[debug][%s] %s", tag, clean))
		return
	}
	total := (len(runes) + chunkSize - 1) / chunkSize
	for i := 0; i < total; i++ {
		start := i * chunkSize
		end := start + chunkSize
		if end > len(runes) {
			end = len(runes)
		}
		part := string(runes[start:end])
		emitStage(
			"schedule_refine.debug.raw",
			fmt.Sprintf("[debug][%s][part %d/%d] %s", tag, i+1, total, part),
		)
	}
}

// physicsCheck 做确定性物理校验。
func physicsCheck(entries []model.HybridScheduleEntry, allocatedCount int) []string {
	issues := make([]string, 0, 8)
	slotMap := make(map[string]string, len(entries)*2)
	for _, entry := range entries {
		if entry.SectionFrom < 1 || entry.SectionTo > 12 || entry.SectionFrom > entry.SectionTo {
			issues = append(issues, fmt.Sprintf("节次越界：%s W%dD%d %d-%d", entry.Name, entry.Week, entry.DayOfWeek, entry.SectionFrom, entry.SectionTo))
		}
		if !entryBlocksSuggested(entry) {
			continue
		}
		for section := entry.SectionFrom; section <= entry.SectionTo; section++ {
			key := fmt.Sprintf("%d-%d-%d", entry.Week, entry.DayOfWeek, section)
			if existed, ok := slotMap[key]; ok {
				issues = append(issues, fmt.Sprintf("冲突：%s 与 %s 同时占用 W%dD%d 第%d节", existed, entry.Name, entry.Week, entry.DayOfWeek, section))
			} else {
				slotMap[key] = entry.Name
			}
		}
	}
	if allocatedCount > 0 {
		suggested := countSuggested(entries)
		if suggested != allocatedCount {
			issues = append(issues, fmt.Sprintf("数量不一致：suggested=%d，allocated_items=%d", suggested, allocatedCount))
		}
	}
	return issues
}

func updateAllocatedItemsFromEntries(st *ScheduleRefineState) {
	if st == nil || len(st.AllocatedItems) == 0 || len(st.HybridEntries) == 0 {
		return
	}
	byTaskID := make(map[int]model.HybridScheduleEntry, len(st.HybridEntries))
	for _, entry := range st.HybridEntries {
		if entry.Status == "suggested" && entry.TaskItemID > 0 {
			byTaskID[entry.TaskItemID] = entry
		}
	}
	for i := range st.AllocatedItems {
		item := &st.AllocatedItems[i]
		entry, ok := byTaskID[item.ID]
		if !ok {
			continue
		}
		if item.EmbeddedTime == nil {
			item.EmbeddedTime = &model.TargetTime{}
		}
		item.EmbeddedTime.Week = entry.Week
		item.EmbeddedTime.DayOfWeek = entry.DayOfWeek
		item.EmbeddedTime.SectionFrom = entry.SectionFrom
		item.EmbeddedTime.SectionTo = entry.SectionTo
	}
}

func countSuggested(entries []model.HybridScheduleEntry) int {
	count := 0
	for _, entry := range entries {
		if entry.Status == "suggested" {
			count++
		}
	}
	return count
}

func summarizeActionLogs(logs []string, tail int) string {
	if len(logs) == 0 {
		return "无"
	}
	if tail <= 0 || len(logs) <= tail {
		return strings.Join(logs, "\n")
	}
	return strings.Join(logs[len(logs)-tail:], "\n")
}

func fallbackText(text string, fallback string) string {
	clean := strings.TrimSpace(text)
	if clean == "" {
		return fallback
	}
	return clean
}

// withNearestJSONContract 把“严格 JSON 输出约束”追加到 userPrompt 末尾。
//
// 步骤化说明：
// 1. 先做 trim，避免多余空白影响模型对结尾指令的关注；
// 2. 再把结构化约束放在最后两行，确保它离模型输出位置最近；
// 3. 若约束为空则原样返回，避免把空字符串误拼进 prompt。
func withNearestJSONContract(userPrompt string, jsonContract string) string {
	base := strings.TrimSpace(userPrompt)
	rule := strings.TrimSpace(jsonContract)
	if rule == "" {
		return base
	}
	if base == "" {
		return rule
	}
	return base + "\n\n" + rule
}

func formatReactPlanStageDetail(round int, out *reactLLMOutput, remaining int, useThinking bool) string {
	if out == nil {
		return fmt.Sprintf("第 %d 轮：缺少计划输出。", round)
	}
	return fmt.Sprintf(
		"第 %d 轮|thinking=%t|动作剩余=%d|goal_check=%s|decision=%s",
		round, useThinking, remaining,
		truncate(strings.TrimSpace(out.GoalCheck), 180),
		truncate(strings.TrimSpace(out.Decision), 180),
	)
}

func formatReactNeedInfoStageDetail(round int, missing []string) string {
	if len(missing) == 0 {
		return fmt.Sprintf("第 %d 轮|模型缺口信息=无。", round)
	}
	return fmt.Sprintf("第 %d 轮|模型缺口信息=%s", round, truncate(strings.Join(uniqueNonEmpty(missing), "；"), 260))
}

func formatReactReflectStageDetail(round int, reflect string) string {
	// 这里统一用“复盘”而不是“反思”：
	// 1. 当前内容由“后端真实执行结果 + 模型预期说明”拼接而成，不是纯模型自述；
	// 2. 用词改为复盘，能更准确表达“以执行结果为准”的定位，减少用户误解为“模型已经真的完成了这一步”。
	return fmt.Sprintf("第 %d 轮|复盘=%s", round, truncate(strings.TrimSpace(reflect), 260))
}

func formatToolCallStageDetail(round int, call reactToolCall, remaining int) string {
	paramsText := "{}"
	if len(call.Params) > 0 {
		if raw, err := json.Marshal(call.Params); err == nil {
			paramsText = string(raw)
		}
	}
	return fmt.Sprintf("第 %d 轮|调用工具=%s|参数=%s|调用前剩余轮次=%d", round, strings.TrimSpace(call.Tool), truncate(paramsText, 320), remaining)
}

func formatToolResultStageDetail(round int, result reactToolResult, used int, total int) string {
	errorCode := strings.TrimSpace(result.ErrorCode)
	if !result.Success && errorCode == "" {
		errorCode = "TOOL_EXEC_FAILED"
	}
	if errorCode == "" {
		errorCode = "NONE"
	}
	return fmt.Sprintf(
		"第 %d 轮|工具=%s|success=%t|error_code=%s|结果=%s|轮次进度=%d/%d",
		round, strings.TrimSpace(result.Tool), result.Success, errorCode, truncate(strings.TrimSpace(result.Result), 320), used, total,
	)
}

func condenseSummary(plans []model.UserWeekSchedule) string {
	if len(plans) == 0 {
		return "无历史排程摘要"
	}
	totalEvents := 0
	startWeek := plans[0].Week
	endWeek := plans[0].Week
	for _, week := range plans {
		totalEvents += len(week.Events)
		if week.Week < startWeek {
			startWeek = week.Week
		}
		if week.Week > endWeek {
			endWeek = week.Week
		}
	}
	return fmt.Sprintf("共 %d 周，周次范围 W%d~W%d，事件总数 %d。", len(plans), startWeek, endWeek, totalEvents)
}

func hybridEntriesToWeekSchedules(entries []model.HybridScheduleEntry) []model.UserWeekSchedule {
	sectionTimeMap := map[int][2]string{
		1: {"08:00", "08:45"}, 2: {"08:55", "09:40"},
		3: {"10:15", "11:00"}, 4: {"11:10", "11:55"},
		5: {"14:00", "14:45"}, 6: {"14:55", "15:40"},
		7: {"16:15", "17:00"}, 8: {"17:10", "17:55"},
		9: {"19:00", "19:45"}, 10: {"19:55", "20:40"},
		11: {"20:50", "21:35"}, 12: {"21:45", "22:30"},
	}
	weekMap := make(map[int][]model.WeeklyEventBrief)
	for _, entry := range entries {
		start, end := "", ""
		if val, ok := sectionTimeMap[entry.SectionFrom]; ok {
			start = val[0]
		}
		if val, ok := sectionTimeMap[entry.SectionTo]; ok {
			end = val[1]
		}
		weekMap[entry.Week] = append(weekMap[entry.Week], model.WeeklyEventBrief{
			ID:        entry.EventID,
			DayOfWeek: entry.DayOfWeek,
			Name:      entry.Name,
			StartTime: start,
			EndTime:   end,
			Type:      entry.Type,
			Span:      entry.SectionTo - entry.SectionFrom + 1,
			Status:    entry.Status,
		})
	}
	result := make([]model.UserWeekSchedule, 0, len(weekMap))
	for week, events := range weekMap {
		result = append(result, model.UserWeekSchedule{Week: week, Events: events})
	}
	for i := 0; i < len(result); i++ {
		for j := i + 1; j < len(result); j++ {
			if result[j].Week < result[i].Week {
				result[i], result[j] = result[j], result[i]
			}
		}
	}
	return result
}

func buildFallbackContract(st *ScheduleRefineState) RefineContract {
	intent := strings.TrimSpace(st.UserMessage)
	keepOrder := detectOrderIntent(st.UserMessage)
	hardRequirements := append([]string(nil), st.Constraints...)
	if keepOrder {
		hardRequirements = append(hardRequirements, "保持任务原始相对顺序不变")
	}
	return RefineContract{
		Intent:            intent,
		Strategy:          "local_adjust",
		HardRequirements:  uniqueNonEmpty(hardRequirements),
		KeepRelativeOrder: keepOrder,
		OrderScope:        "global",
		Reason:            "契约抽取失败，按兜底策略继续。",
	}
}

func normalizeStrategy(strategy string) string {
	switch strings.TrimSpace(strings.ToLower(strategy)) {
	case "keep":
		return "keep"
	default:
		return "local_adjust"
	}
}

func detectOrderIntent(userMessage string) bool {
	msg := strings.TrimSpace(userMessage)
	if msg == "" {
		return false
	}
	keywords := []string{"顺序不变", "保持顺序", "按原顺序", "不要打乱顺序", "不打乱顺序", "先后顺序", "原顺序"}
	for _, k := range keywords {
		if strings.Contains(msg, k) {
			return true
		}
	}
	return false
}

func uniqueNonEmpty(items []string) []string {
	if len(items) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(items))
	out := make([]string, 0, len(items))
	for _, item := range items {
		clean := strings.TrimSpace(item)
		if clean == "" {
			continue
		}
		if _, ok := seen[clean]; ok {
			continue
		}
		seen[clean] = struct{}{}
		out = append(out, clean)
	}
	return out
}

func buildObservationPrompt(history []ReactRoundObservation, tail int) string {
	if len(history) == 0 {
		return "无"
	}
	start := 0
	if tail > 0 && len(history) > tail {
		start = len(history) - tail
	}
	raw, err := json.Marshal(history[start:])
	if err != nil {
		return summarizeActionLogs([]string{err.Error()}, 1)
	}
	return string(raw)
}

// buildLastToolObservationPrompt 返回“上一轮结构化工具观察”。
//
// 步骤化说明：
// 1. 从观察历史末尾向前找最近一条带工具名的记录，避免把“done轮/无动作轮”误当工具观察；
// 2. 输出 JSON 字符串，供模型按结构化字段读取 success/error_code/params；
// 3. 若不存在工具观察则返回“无”。
func buildLastToolObservationPrompt(history []ReactRoundObservation) string {
	for i := len(history) - 1; i >= 0; i-- {
		item := history[i]
		if strings.TrimSpace(item.ToolName) == "" {
			continue
		}
		raw, err := json.Marshal(item)
		if err != nil {
			return "无"
		}
		return string(raw)
	}
	return "无"
}

// buildToolCallSignature 构造工具调用签名（tool+params）。
//
// 说明：
// 1. 用于识别“与上一轮失败动作完全相同”的重复调用；
// 2. 采用 JSON 序列化参数，保证签名稳定、可记录、可回放；
// 3. 签名只用于去重，不用于业务持久化。
func buildToolCallSignature(call reactToolCall) string {
	paramsText := "{}"
	if len(call.Params) > 0 {
		if raw, err := json.Marshal(call.Params); err == nil {
			paramsText = string(raw)
		}
	}
	return fmt.Sprintf("%s|%s", strings.ToUpper(strings.TrimSpace(call.Tool)), paramsText)
}

// isRepeatedFailedCall 判断当前动作是否重复了“上一轮失败动作”。
func isRepeatedFailedCall(st *ScheduleRefineState, signature string) bool {
	if st == nil {
		return false
	}
	current := strings.TrimSpace(signature)
	last := strings.TrimSpace(st.LastFailedCallSignature)
	if current == "" || last == "" {
		return false
	}
	return current == last
}

// normalizeToolResult 对工具结果做统一规范化。
//
// 步骤化说明：
// 1. 成功结果保留现状；
// 2. 失败结果若未设置 error_code，则按结果文案推断统一错误码；
// 3. 统一错误码后，可被模型下一轮稳定消费，减少“读不懂上一轮失败原因”。
func normalizeToolResult(result reactToolResult) reactToolResult {
	if result.Success {
		return result
	}
	if strings.TrimSpace(result.ErrorCode) != "" {
		return result
	}
	result.ErrorCode = classifyToolFailureCode(result.Result)
	return result
}

// classifyToolFailureCode 把工具失败文案映射为稳定错误码。
func classifyToolFailureCode(detail string) string {
	text := strings.TrimSpace(detail)
	switch {
	case strings.Contains(text, "重复失败动作"):
		return "REPEAT_FAILED_ACTION"
	case strings.Contains(text, "顺序约束不满足"):
		return "ORDER_VIOLATION"
	case strings.Contains(text, "参数缺失"):
		return "PARAM_MISSING"
	case strings.Contains(text, "目标时段已被"):
		return "SLOT_CONFLICT"
	case strings.Contains(text, "任务跨度不一致"):
		return "SPAN_MISMATCH"
	case strings.Contains(text, "超出允许窗口"):
		return "OUT_OF_WINDOW"
	case strings.Contains(text, "day_of_week"):
		return "DAY_INVALID"
	case strings.Contains(text, "节次区间"):
		return "SECTION_INVALID"
	case strings.Contains(text, "未找到 task_item_id"):
		return "TASK_NOT_FOUND"
	case strings.Contains(text, "不支持的工具"):
		return "TOOL_NOT_ALLOWED"
	case strings.Contains(text, "BatchMove"):
		return "BATCH_MOVE_FAILED"
	case strings.Contains(text, "Verify"):
		return "VERIFY_FAILED"
	case strings.Contains(text, "序列化查询结果失败"), strings.Contains(text, "序列化空位结果失败"):
		return "QUERY_ENCODE_FAILED"
	default:
		return "TOOL_EXEC_FAILED"
	}
}

// formatStructuredToolResult 把工具执行结果编码为结构化文本。
//
// 说明：
// 1. 该字符串会写入 state，并在下一轮 prompt 以 LAST_TOOL_RESULT 透传给模型；
// 2. 采用 JSON 结构，减少模型对自然语言描述的误读；
// 3. 编码失败时降级为简短纯文本，避免链路中断。
func formatStructuredToolResult(result reactToolResult) string {
	obj := map[string]any{
		"tool":       strings.TrimSpace(result.Tool),
		"success":    result.Success,
		"error_code": strings.TrimSpace(result.ErrorCode),
		"result":     strings.TrimSpace(result.Result),
	}
	raw, err := json.Marshal(obj)
	if err != nil {
		return fmt.Sprintf("tool=%s success=%t error_code=%s result=%s", result.Tool, result.Success, result.ErrorCode, result.Result)
	}
	return string(raw)
}

// cloneToolParams 深拷贝工具参数，避免后续 map 复用造成历史观察污染。
func cloneToolParams(params map[string]any) map[string]any {
	if len(params) == 0 {
		return nil
	}
	raw, err := json.Marshal(params)
	if err != nil {
		dst := make(map[string]any, len(params))
		for k, v := range params {
			dst[k] = v
		}
		return dst
	}
	var out map[string]any
	if err = json.Unmarshal(raw, &out); err != nil {
		dst := make(map[string]any, len(params))
		for k, v := range params {
			dst[k] = v
		}
		return dst
	}
	return out
}

func formatRoundModelErrorDetail(round int, err error, parentCtx context.Context) string {
	parentState := "alive"
	if parentCtx == nil {
		parentState = "nil"
	} else if parentCtx.Err() != nil {
		parentState = parentCtx.Err().Error()
	}
	parentDeadline := "none"
	if parentCtx != nil {
		if deadline, ok := parentCtx.Deadline(); ok {
			parentDeadline = fmt.Sprintf("%dms", time.Until(deadline).Milliseconds())
		}
	}
	return fmt.Sprintf("第 %d 轮模型调用失败：%v | parent_ctx=%s | parent_deadline_in_ms=%s | node_timeout_ms=%d", round, err, parentState, parentDeadline, nodeTimeout.Milliseconds())
}

func buildRuntimeReflect(modelReflect string, result reactToolResult) string {
	modelText := strings.TrimSpace(modelReflect)
	resultText := truncate(strings.TrimSpace(result.Result), 220)
	if result.Success {
		if modelText == "" {
			return fmt.Sprintf("后端复盘：工具执行成功。%s", resultText)
		}
		// 1. 成功分支下，模型文本仅作为“动作前预期”的补充说明；
		// 2. 业务上真正生效的是后端工具结果，因此前缀固定写“后端复盘”。
		return fmt.Sprintf("后端复盘：工具执行成功。%s。模型预期（动作前）：%s", resultText, truncate(modelText, 180))
	}
	if modelText == "" {
		return fmt.Sprintf("后端复盘：工具执行失败。%s。本轮调整未生效，已保留原方案。", resultText)
	}
	// 1. 失败分支必须把“未生效”写死，防止用户把模型话术当成已执行事实；
	// 2. 模型文本仅保留为“动作前预期”，用于解释它为什么会选这一步。
	return fmt.Sprintf("后端复盘：工具执行失败。%s。本轮调整未生效，已保留原方案。模型预期（动作前，仅供参考）：%s", resultText, truncate(modelText, 160))
}

func buildSuggestedDigest(entries []model.HybridScheduleEntry, limit int) string {
	if len(entries) == 0 {
		return "无"
	}
	list := make([]model.HybridScheduleEntry, 0, len(entries))
	for _, entry := range entries {
		if entry.Status == "suggested" && entry.TaskItemID > 0 {
			list = append(list, entry)
		}
	}
	if len(list) == 0 {
		return "无 suggested 条目"
	}
	sortHybridEntries(list)
	if limit <= 0 {
		limit = len(list)
	}
	if len(list) > limit {
		list = list[:limit]
	}
	lines := make([]string, 0, len(list))
	for _, item := range list {
		lines = append(lines, fmt.Sprintf(
			"id=%d|W%d|D%d(%s)|%d-%d|%s",
			item.TaskItemID,
			item.Week,
			item.DayOfWeek,
			weekdayLabel(item.DayOfWeek),
			item.SectionFrom,
			item.SectionTo,
			strings.TrimSpace(item.Name),
		))
	}
	return strings.Join(lines, "\n")
}

func weekdayLabel(day int) string {
	switch day {
	case 1:
		return "周一"
	case 2:
		return "周二"
	case 3:
		return "周三"
	case 4:
		return "周四"
	case 5:
		return "周五"
	case 6:
		return "周六"
	case 7:
		return "周日"
	default:
		return "未知"
	}
}

// parseReactOutputWithRetryOnce 对 ReAct 输出做“单次重试解析”。
//
// 步骤化说明：
// 1. 先解析首次模型输出，成功即直接返回。
// 2. 首次解析失败时，同轮重试一次模型调用（关闭 thinking + 温度置 0），提升结构化稳定性。
// 3. 若重试后解析成功，则发出成功阶段信号并继续流程。
// 4. 若重试调用或二次解析仍失败，则返回统一业务错误码，避免前端拿到不可控的原始解析错误。
func parseReactOutputWithRetryOnce(
	ctx context.Context,
	chatModel *ark.ChatModel,
	userPrompt string,
	firstRaw string,
	round int,
	emitStage func(stage, detail string),
	st *ScheduleRefineState,
) (*reactLLMOutput, error) {
	if st == nil {
		return nil, respond.ScheduleRefineOutputParseFailed
	}

	parsed, parseErr := parseReactLLMOutput(firstRaw)
	if parseErr == nil {
		return parsed, nil
	}

	firstFail := fmt.Sprintf("第 %d 轮输出解析失败，准备重试1次：%s", round, truncate(parseErr.Error(), 260))
	st.ActionLogs = append(st.ActionLogs, firstFail)
	emitStage("schedule_refine.react.parse_retry", firstFail)

	retryRaw, retryErr := callModelText(ctx, chatModel, reactPrompt, userPrompt, false, reactMaxTokens, 0)
	if retryErr != nil {
		retryErrDetail := formatRoundModelErrorDetail(round, fmt.Errorf("解析重试调用失败: %w", retryErr), ctx)
		st.ActionLogs = append(st.ActionLogs, retryErrDetail)
		emitStage("schedule_refine.react.round_error", retryErrDetail)
		return nil, respond.ScheduleRefineOutputParseFailed
	}
	emitModelRawDebug(emitStage, fmt.Sprintf("react.round.%d.retry", round), retryRaw)

	retryParsed, retryParseErr := parseReactLLMOutput(retryRaw)
	if retryParseErr != nil {
		secondFail := fmt.Sprintf("第 %d 轮输出二次解析失败：%s", round, truncate(retryParseErr.Error(), 260))
		st.ActionLogs = append(st.ActionLogs, secondFail)
		emitStage("schedule_refine.react.round_error", secondFail)
		return nil, respond.ScheduleRefineOutputParseFailed
	}

	emitStage("schedule_refine.react.parse_retry_success", fmt.Sprintf("第 %d 轮输出重试解析成功，继续执行。", round))
	return retryParsed, nil
}

// parsePlannerOutputWithRetryOnce 对 Planner 输出做“单次重试解析”。
//
// 步骤化说明：
// 1. 先解析首次 Planner 输出，成功则直接返回；
// 2. 若失败，触发一次“严格 JSON 重试请求”，并打出 retry raw debug；
// 3. 若重试仍失败，返回错误给上层，由上层走兜底计划。
func parsePlannerOutputWithRetryOnce(
	ctx context.Context,
	chatModel *ark.ChatModel,
	originUserPrompt string,
	firstRaw string,
	mode string,
	emitStage func(stage, detail string),
) (*plannerOutput, error) {
	parsed, parseErr := parseJSON[plannerOutput](firstRaw)
	if parseErr == nil {
		return parsed, nil
	}

	emitStage(
		"schedule_refine.plan.parse_retry",
		fmt.Sprintf("Planner 解析失败，准备重试1次（mode=%s）：%s", strings.TrimSpace(mode), truncate(parseErr.Error(), 160)),
	)

	retryPrompt := withNearestJSONContract(
		fmt.Sprintf(
			"%s\n\n上一次输出解析失败（原因：JSON 不完整或不闭合）。请缩短内容并严格输出完整 JSON。",
			originUserPrompt,
		),
		jsonContractForPlanner,
	)
	retryRaw, retryErr := callModelText(ctx, chatModel, plannerPrompt, retryPrompt, false, plannerMaxTokens, 0)
	if retryErr != nil {
		return nil, retryErr
	}
	emitModelRawDebug(emitStage, fmt.Sprintf("planner.%s.retry", strings.TrimSpace(mode)), retryRaw)

	retryParsed, retryParseErr := parseJSON[plannerOutput](retryRaw)
	if retryParseErr != nil {
		return nil, retryParseErr
	}
	emitStage("schedule_refine.plan.parse_retry_success", fmt.Sprintf("Planner 重试解析成功（mode=%s）。", strings.TrimSpace(mode)))
	return retryParsed, nil
}
