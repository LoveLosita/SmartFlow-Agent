package agentexecute

import (
	"context"
	"fmt"
	agentshared "github.com/LoveLosita/smartflow/backend/services/agent/shared"

	agentmodel "github.com/LoveLosita/smartflow/backend/services/agent/model"
	agentprompt "github.com/LoveLosita/smartflow/backend/services/agent/prompt"
	agentstream "github.com/LoveLosita/smartflow/backend/services/agent/stream"
	agenttools "github.com/LoveLosita/smartflow/backend/services/agent/tools"
	"github.com/LoveLosita/smartflow/backend/services/agent/tools/schedule"
	llmservice "github.com/LoveLosita/smartflow/backend/services/llm"
)

// execute 阶段常量：
// 1. 统一约束 execute 节点在流式输出、上下文挂载、历史标记中的固定标识；
// 2. 这些值会在 prompt、stream、history、tool runtime 多处复用，集中定义可避免字符串散落；
// 3. 报告里可将其理解为 execute 节点的“协议层元数据”，负责让编排层、展示层和执行层说同一种语言。
const (
	// executeStageName 是 graph/node/stream 共同识别的执行阶段名。
	// 调用目的：用于状态推送、日志埋点、终止态写入时标识“当前发生在 execute 阶段”。
	executeStageName = "execute"

	// executeStatusBlockID 是前端状态区块 ID。
	// 调用目的：把“正在执行第几步”“工具执行中”等状态稳定写到同一个可更新区块，避免重复刷屏。
	executeStatusBlockID = "execute.status"

	// executeSpeakBlockID 是 execute 阶段可见回复文本的输出区块 ID。
	// 调用目的：让模型在 execute 阶段产生的正文、补发文本、thinking 摘要归并到同一个展示区域。
	executeSpeakBlockID = "execute.speak"

	// executePinnedKey 是 execute 专属 pinned context 的 key。
	// 调用目的：把“执行模式、计划步、done_when、轮次预算”等关键信息固定挂到会话上下文，供后续每轮提示词复用。
	executePinnedKey = "execution_context"

	// toolAnalyzeHealth 是粗排后健康度分析工具名。
	// 调用目的：在主动优化链路中复用固定工具标识，避免分支判断时手写魔法字符串。
	toolAnalyzeHealth = "analyze_health"

	// executeHistoryKindKey 是 execute 阶段写入 history extra 时使用的分类 key。
	// 调用目的：给某些“无正文但有语义”的历史记录打标签，方便后续节点识别这是系统插入的流程标记。
	executeHistoryKindKey = "newagent_history_kind"

	// executeHistoryKindStepAdvanced 表示“计划步骤已推进”的历史标记值。
	// 调用目的：在 next_plan 后往历史里写一个轻量哨兵，避免重复推进时无法判断上一步是否已切换。
	executeHistoryKindStepAdvanced = "execute_step_advanced"

	// maxConsecutiveCorrections 是 execute 阶段允许的最大连续纠错次数。
	// 调用目的：限制模型在 JSON 解析失败、动作不合法、goal_check 缺失等异常情况下的自我修正轮数，防止无限空转。
	maxConsecutiveCorrections = 3
)

// ExecuteNodeInput 描述 execute 节点单轮运行所需的全部输入依赖。
//
// 职责边界：
// 1. 负责承载 execute 节点真正运行时要读到的“状态 + 上下文 + 模型 + 工具 + 输出能力”；
// 2. 不负责 graph 拓扑与分支决策，这些仍由 compose.Graph 和 branchAfterExecute 管理；
// 3. 不负责具体工具实现或 LLM 协议细节，它只是 node 层与下游能力之间的依赖注入容器。
//
// 报告可重点说明：
// 1. 这个结构体体现了本项目如何把 Eino agent 的单节点执行显式建模为一个“有完整依赖注入边界”的入口；
// 2. 与把所有依赖散落在全局变量不同，这种做法更便于恢复执行、单测替身注入和不同模型/工具组合切换；
// 3. execute 节点能否安全回环，核心就在于这些输入把“状态、上下文、工具、输出”完整串起来了。
type ExecuteNodeInput struct {
	// RuntimeState 是 execute 节点的主运行态容器。
	// 1. 负责承载 CommonState、待确认工具、待追问交互等跨轮次状态；
	// 2. execute 节点对 phase、round、plan step、pending interaction 的修改都会落到这里；
	// 3. 若为空则无法恢复和推进 agent 执行，因此 prepareExecuteNodeInput 会把它视为必填依赖。
	RuntimeState *agentmodel.AgentRuntimeState

	// ConversationContext 是当前会话的统一上下文快照。
	// 1. 负责保存历史消息、pinned block、工具 schema 等 prompt 装配素材；
	// 2. execute 每一轮都会基于它重建 Eino message；
	// 3. 它不是持久化存储本身，而是当前 graph 运行时使用的内存态上下文。
	ConversationContext *agentmodel.ConversationContext

	// UserInput 是本轮用户原始输入文本。
	// 1. 主要用于让 execute 节点在必要时回看用户当前明确意图；
	// 2. 不直接等价于最终 prompt，全量消息仍由 BuildExecuteMessages 统一组织；
	// 3. 保留该字段是为了让 execute 在恢复、追问、纠错时仍能感知本轮入口意图。
	UserInput string

	// Client 是 execute 阶段专用 LLM 客户端。
	// 1. 负责发起流式推理请求，生成结构化 SMARTFLOW_DECISION；
	// 2. 它可以与 chat / plan / deliver 阶段使用不同模型配置；
	// 3. 若未注入，execute 节点无法完成“思考下一步”的核心职责。
	Client *llmservice.Client

	// ChunkEmitter 是流式输出器。
	// 1. 负责把状态、正文、thinking 摘要、工具调用过程实时推给前端；
	// 2. execute 节点的大部分用户可见体验都经由它完成；
	// 3. 即使外部没接真实前端，系统也会兜底一个 noop emitter，保证流程代码无需处处判空。
	ChunkEmitter *agentstream.ChunkEmitter

	// ResumeNode 表示挂起交互恢复后应回到的节点名。
	// 1. 主要用于 ask_user / confirm 之后恢复 graph 时定位回 execute；
	// 2. 它是 pending interaction 与 graph 路由之间的衔接信息；
	// 3. 当前通常写为 "execute"，便于中断后继续原执行链路。
	ResumeNode string

	// ToolRegistry 是 execute 阶段可调用工具的统一注册表。
	// 1. 负责提供工具 schema、工具可见性判断和真正的工具执行入口；
	// 2. execute 不直接依赖具体工具实现，而是通过注册表做统一调度；
	// 3. 这让 agent 的工具能力可以按域动态裁剪，而不必修改 execute 主流程。
	ToolRegistry *agenttools.ToolRegistry

	// ScheduleState 是当前排程态快照。
	// 1. 只有涉及日程构建、挪动、交换等排程工具时才会被真正消费；
	// 2. 首轮 execute 会对它做必要初始化，后续轮次复用同一份内存状态；
	// 3. 该字段为空时，依赖日程状态的工具会被阻止执行，避免工具在残缺状态上落地。
	ScheduleState *schedule.ScheduleState

	// CompactionStore 是消息压缩存储依赖。
	// 1. 当上下文过长时，execute 会通过它记录或读取统一压缩结果；
	// 2. 目的是在长链路中控制 token 成本，同时尽量保留关键执行语义；
	// 3. 这是长流程 agent 可持续运行的重要辅助能力，而非核心业务状态。
	CompactionStore agentmodel.CompactionStore

	// WriteSchedulePreview 是排程预览写入函数。
	// 1. 当 execute 阶段调用写排程工具后，可实时把最新预览写给前端/缓存；
	// 2. 它只负责预览落盘，不负责真正修改 graph 路由或业务状态；
	// 3. 这样用户在确认或微调过程中就能看到中间结果，而不必等到最终 deliver。
	WriteSchedulePreview agentmodel.WriteSchedulePreviewFunc

	// OriginalScheduleState 是进入本轮执行前的原始排程快照。
	// 1. 主要用于需要对比、确认、恢复时保留“变更前基线”；
	// 2. executePendingTool 等恢复场景可能会用到这份原始视角；
	// 3. 它不是当前正在被修改的工作态，当前工作态仍是 ScheduleState。
	OriginalScheduleState *schedule.ScheduleState

	// AlwaysExecute 表示是否跳过确认直接执行写工具。
	// 1. 为 true 时，confirm 类动作可以直接落地，不再等待用户二次确认；
	// 2. 为 false 时，高风险写工具需先转成 pending confirm，遵守安全确认链路；
	// 3. 这给不同入口场景（自动执行/显式确认）保留了统一的 execute 主流程。
	AlwaysExecute bool

	// ThinkingEnabled 控制 execute 阶段是否开启 reasoning/thinking 模式。
	// 1. 它影响 LLM 请求时的推理模式选择；
	// 2. 开启后系统会额外汇总 reasoning 摘要并推送给前端；
	// 3. 该能力只影响模型推理表现，不改变 execute 节点的状态机协议。
	ThinkingEnabled bool

	// PersistVisibleMessage 是可见消息持久化函数。
	// 1. 负责把应落库的 assistant 可见回复保存下来，供后续历史回放；
	// 2. execute 不直接操作数据库，而是通过该函数把持久化职责下沉；
	// 3. 这样 node 层仍保持“编排与调度”为主，不把存储细节耦合进来。
	PersistVisibleMessage agentmodel.PersistVisibleMessageFunc
}

// ExecuteRoundObservation 描述 execute 阶段单轮观测结果。
//
// 职责边界：
// 1. 负责记录一轮 execute 中“模型做了什么决策、调用了什么工具、结果如何”；
// 2. 不负责承载完整上下文或最终业务结果，它更偏向调试、回放、分析视角；
// 3. 该结构体的价值在于把 agent 的单轮行为沉淀成可审计、可解释的数据切片。
type ExecuteRoundObservation struct {
	// Round 是执行轮次编号。
	// 调用目的：标识这是第几轮 execute 回环，便于分析 agent 是否在某一步反复空转。
	Round int `json:"round"`

	// StepIndex 是当前计划步骤索引。
	// 调用目的：把本轮行为与 plan 中的具体步骤关联起来，支持“第几步发生了什么”这类复盘。
	StepIndex int `json:"step_index"`

	// GoalCheck 是模型在 next_plan / done 时给出的完成性核验说明。
	// 调用目的：保留“为什么它认为当前步骤已完成”的证据文本，增强 agent 行为可解释性。
	GoalCheck string `json:"goal_check,omitempty"`

	// Decision 是本轮结构化决策动作摘要。
	// 调用目的：记录 action 层面的选择结果，如 continue / confirm / ask_user / done。
	Decision string `json:"decision,omitempty"`

	// ToolName 是本轮调用的工具名。
	// 调用目的：标识 agent 在这一轮真正把哪项外部能力接入执行链。
	ToolName string `json:"tool_name,omitempty"`

	// ToolParams 是工具调用参数的可读化快照。
	// 调用目的：辅助排查“工具为什么执行出这个结果”，但它只保留观测视图，不替代真实原始参数对象。
	ToolParams string `json:"tool_params,omitempty"`

	// ToolSuccess 表示工具是否执行成功。
	// 调用目的：在轮次观测里快速区分“正常推进”与“工具失败/被阻断”的分支结果。
	ToolSuccess bool `json:"tool_success"`

	// ToolResult 是工具返回结果的摘要文本。
	// 调用目的：保留工具观察结果，供下一轮上下文构造、日志调试或报告中的执行链路说明使用。
	ToolResult string `json:"tool_result,omitempty"`
}

// RunExecuteNode 是 Eino agent graph 中 execute 节点的“单轮执行入口”。
//
// 职责边界：
// 1. 负责把 graph 层传入的运行态、会话上下文、工具目录、流式输出器等依赖整理成一次可执行的 execute 轮次。
// 2. 负责完成 execute 阶段的一次完整闭环：状态校准 -> 提示词装配 -> LLM 决策 -> 决策落地。
// 3. 不负责定义 Eino graph 的节点拓扑；节点注册、连边与回环由 graph/common_graph.go 中的 compose.Graph 统一管理。
// 4. 不负责具体工具实现和流式 JSON 解析细节；工具执行下沉到 tool_runtime.go，模型决策采集下沉到 action_router.go。
//
// 报告可重点说明：
// 1. 这个函数体现了“Graph 负责编排、Node 负责单轮执行”的 Eino 接入方式。
// 2. execute 节点不是直接把用户请求一次性做完，而是以“轮次”为单位驱动 ReAct/Plan-and-Execute 风格的 agent 闭环。
// 3. 每一轮都会基于当前状态重建 Eino message，上下文、计划进度、工具域、历史观察都会重新送入模型，让模型只决定“下一步该做什么”。
func RunExecuteNode(ctx context.Context, input ExecuteNodeInput) error {
	// 1. 先做 execute 节点入参归一化：
	//    1.1 这里会兜底 RuntimeState / ConversationContext / ChunkEmitter，避免 graph 恢复执行或单测场景出现空指针；
	//    1.2 execute 节点后续所有状态推进都依赖这三类对象，因此必须在第一步统一校准；
	//    1.3 若最基础的依赖（如 RuntimeState、LLM Client）缺失，直接返回错误，避免带着残缺上下文进入模型决策。
	runtimeState, conversationContext, emitter, err := prepareExecuteNodeInput(input)
	if err != nil {
		return err
	}

	// 2. 取出 graph 内部跨节点共享的 CommonState。
	//    2.1 Chat / Plan / RoughBuild / Execute 都围绕这份状态协作；
	//    2.2 execute 节点真正关心的“当前阶段、计划步、轮次预算、工具域、终止态”等信息都挂在这里；
	//    2.3 这也是 Eino graph 在多节点之间传递 agent 运行语义的核心载体。
	flowState := runtimeState.EnsureCommonState()

	// 3. 应用待生效的上下文 hook。
	//    3.1 上游节点（如 plan / rough_build）可能只先写入 PendingContextHook，而不立刻改 ActiveToolDomain；
	//    3.2 execute 节点在真正请求模型前统一消费这个 hook，保证“提示词里的规则”和“模型看见的工具 schema”同轮生效；
	//    3.3 若这里不先同步，模型第一轮可能拿到旧工具域，导致它想做的动作与实际可见工具不一致。
	applyPendingContextHook(flowState)

	// 4. 若当前存在“待确认写工具”，说明上一轮已经完成了 LLM 决策，本轮是用户确认后的恢复执行。
	//    4.1 这时不应该再让模型重新思考，否则会破坏“确认后执行原工具”的事务语义；
	//    4.2 因此这里直接短路进入 executePendingTool，把已确认的工具参数原样执行；
	//    4.3 这也是本项目把“模型决策”和“高风险写操作落地”显式拆开的关键安全设计。
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

	// 5. 首轮进入 execute 且携带日程状态时，重置任务处理队列。
	//    5.1 这样可以保证一次新的执行会话从干净的调度处理顺序开始；
	//    5.2 只在 RoundUsed == 0 时重置，避免多轮 execute 回环过程中把中间状态反复抹掉；
	//    5.3 这类“首轮初始化、后续轮次复用”的策略，是 agent 在长流程里保持状态连续性的基础。
	if input.ScheduleState != nil && flowState.RoundUsed == 0 {
		schedule.ResetTaskProcessingQueue(input.ScheduleState)
	}

	// 6. 把 execute 阶段的关键信息同步到会话 pinned context。
	//    6.1 这里会把执行模式、当前计划步、done_when、轮次预算等内容写成可复用的上下文块；
	//    6.2 后续 BuildExecuteMessages 会把这些 pinned block 一并装配进 Eino message；
	//    6.3 这样模型每轮都能看到“当前执行到哪一步、什么算完成”，避免在长对话里丢失阶段感。
	syncExecutePinnedContext(conversationContext, flowState)

	// 7. 先向前端推送 execute 阶段状态，让用户看到 agent 当前正在做什么。
	//    7.1 若当前处于 plan 模式，就明确展示“第几步 / 共几步 + 当前步骤摘要”；
	//    7.2 若没有计划，则退化为通用“正在处理请求”文案；
	//    7.3 这里的状态推送不影响 graph 路由，但会直接影响产品侧的可观察性与交互体验。
	if flowState.HasCurrentPlanStep() {
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

	// 8. 进入真正的模型决策前，先消费一轮执行预算。
	//    8.1 execute 节点在 Eino graph 中是允许回环的；branchAfterExecute 会在未完成时再次回到本节点；
	//    8.2 NextRound() 失败时，不再继续请求模型，而是把“达到安全轮次上限”的终止原因写入状态；
	//    8.3 这样 deliver 节点后续收口时可以读取统一的 terminal outcome，避免无限循环或异常失控。
	if !flowState.NextRound() {
		flowState.Exhaust(
			executeStageName,
			"本轮执行已达到安全轮次上限，当前先停止继续操作。如需继续，我可以在你确认后接着处理剩余步骤。",
			"execute rounds exhausted before task completion",
		)
		return nil
	}

	// 9. 基于当前 flowState + ConversationContext 重新构建本轮要发给模型的消息。
	//    9.1 BuildExecuteMessages 会产出 []*schema.Message，这是 Eino 模型调用使用的标准消息结构；
	//    9.2 消息里不仅有历史对话，还会带上 execute 专属系统提示词、计划步骤约束、工具 schema、pinned context；
	//    9.3 这一步体现了本项目的核心思想：不是把 Agent 写死成 if/else，而是把“当前可行动空间”编码进 prompt + state。
	messages := agentprompt.BuildExecuteMessages(flowState, conversationContext)

	// 10. 若上下文过长，则在真正调用模型前统一压缩消息。
	//     10.1 压缩逻辑仍围绕统一的 Eino message 结构工作，不破坏后续模型调用接口；
	//     10.2 这样既能控制 token 成本，又能尽量保住当前步骤、工具观察、关键 pinned 信息；
	//     10.3 对长流程 agent 来说，这一步是“让多轮执行可持续”的关键工程补偿层。
	messages = agentshared.CompactUnifiedMessagesIfNeeded(ctx, messages, agentshared.UnifiedCompactInput{
		Client:          input.Client,
		CompactionStore: input.CompactionStore,
		FlowState:       flowState,
		Emitter:         emitter,
		StageName:       executeStageName,
		StatusBlockID:   executeStatusBlockID,
	})

	// 11. 在发起模型请求前记录本轮 LLM 上下文快照，便于排查“为什么模型会做出这个动作”。
	//     11.1 这类日志不参与业务逻辑，但对调试 agent prompt、状态迁移和工具选择非常重要；
	//     11.2 尤其在 execute 回环链路里，它能帮助我们复盘每一轮看到的上下文是否一致。
	agentshared.LogNodeLLMContext(executeStageName, "decision", flowState, messages)

	// 12. 调用模型，流式采集 execute 决策。
	//     12.1 下游会使用 Eino 风格的 message 输入向 LLM 发起 Stream 请求；
	//     12.2 模型需要输出 <SMARTFLOW_DECISION>{...}</SMARTFLOW_DECISION> 结构化决策，再附带可见回答；
	//     12.3 这里拿到的不是“最终答案”，而是 agent 下一步动作的机器可执行描述：继续读工具、请求确认、追问用户、推进计划或结束。
	decisionOutput, err := collectExecuteDecisionFromLLM(
		ctx,
		input,
		flowState,
		conversationContext,
		emitter,
		messages,
	)
	if err != nil {
		return err
	}

	// 13. 把模型决策真正落到运行时。
	//     13.1 handleExecuteDecision 会校验 action 合法性，并根据决策修改 flowState / runtimeState / conversation history；
	//     13.2 若是 continue + tool_call，会执行读工具或普通工具；若是 confirm，会挂起等待用户确认；若是 next_plan / done，会推进计划或写入终止态；
	//     13.3 至此，一次 execute 节点的“思考 + 行动”闭环完成，Eino graph 再根据更新后的状态决定下一跳是回到 execute、转 confirm，还是进入 deliver。
	return handleExecuteDecision(
		ctx,
		input,
		runtimeState,
		flowState,
		conversationContext,
		emitter,
		decisionOutput,
	)
}
