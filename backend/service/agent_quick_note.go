package service

import (
	"context"
	"fmt"
	"log"
	"regexp"
	"strings"
	"time"

	"github.com/LoveLosita/smartflow/backend/agent"
	"github.com/LoveLosita/smartflow/backend/model"
	"github.com/cloudwego/eino-ext/components/model/ark"
	einoModel "github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
	"github.com/google/uuid"
	arkModel "github.com/volcengine/volcengine-go-sdk/service/arkruntime/model"
)

const (
	// quickNoteRouteControlTimeout 是“模型控制码分流”这一步的额外超时。
	// 说明：
	// 1) 设为 0 代表“不额外加子超时”，完全跟随父请求上下文；
	// 2) 避免路由步骤因过短子超时反复触发 context deadline exceeded；
	// 3) 若后续需要强制保护，可再改为 >0 的值并通过配置化管理。
	quickNoteRouteControlTimeout = 0 * time.Second
)

var (
	// quickNoteRouteHeaderRegex 解析模型返回的控制头：
	// <SMARTFLOW_ROUTE nonce="xxx" action="quick_note|chat"></SMARTFLOW_ROUTE>
	quickNoteRouteHeaderRegex = regexp.MustCompile(`(?is)<\s*smartflow_route\b[^>]*\bnonce\s*=\s*["']?([a-zA-Z0-9\-]+)["']?[^>]*\baction\s*=\s*["']?(quick_note|chat)["']?[^>]*>`)
	// quickNoteRouteReasonRegex 解析可选理由块：
	// <SMARTFLOW_REASON>...</SMARTFLOW_REASON>
	quickNoteRouteReasonRegex = regexp.MustCompile(`(?is)<\s*smartflow_reason\s*>(.*?)<\s*/\s*smartflow_reason\s*>`)
)

type quickNoteRouteAction string

const (
	quickNoteRouteActionChat      quickNoteRouteAction = "chat"
	quickNoteRouteActionQuickNote quickNoteRouteAction = "quick_note"
)

// quickNoteRouteControlDecision 是“模型控制码分流”的结构化结果。
// 该结构不会直接暴露给前端，仅用于服务端决定后续链路：
// - action=quick_note -> 进入随口记 graph；
// - action=chat -> 进入普通聊天流。
type quickNoteRouteControlDecision struct {
	Action quickNoteRouteAction
	Reason string
	Raw    string
}

// quickNoteRoutingDecision 是对“是否进入随口记 graph”的最终决策。
// 字段说明：
// - EnterQuickNote：是否进入随口记 graph；
// - TrustRoute：是否信任上游控制码并跳过 graph 内的二次意图判定；
// - Detail：阶段状态文案，用于前端/调试可观测性。
type quickNoteRoutingDecision struct {
	EnterQuickNote bool
	TrustRoute     bool
	Detail         string
}

// quickNoteProgressEmitter 负责把“链路阶段状态”伪装成 OpenAI 兼容的 reasoning_content chunk。
// 设计目标：
// 1) 不改现有 OpenAI 兼容协议外壳；
// 2) 让 Apifox 在等待期间也能看到“思考块”，避免用户空等；
// 3) 该 emitter 只负责状态，不负责最终正文回复和 [DONE] 结束块。
type quickNoteProgressEmitter struct {
	outChan    chan<- string
	modelName  string
	requestID  string
	created    int64
	enablePush bool
}

func newQuickNoteProgressEmitter(outChan chan<- string, modelName string, enable bool) *quickNoteProgressEmitter {
	resolvedModel := strings.TrimSpace(modelName)
	if resolvedModel == "" {
		resolvedModel = "worker"
	}
	return &quickNoteProgressEmitter{
		outChan:    outChan,
		modelName:  resolvedModel,
		requestID:  "chatcmpl-" + uuid.NewString(),
		created:    time.Now().Unix(),
		enablePush: enable,
	}
}

// Emit 按“阶段 + 说明”输出 reasoning_content。
// 注意：
// - 这里不输出 role，避免和后续正文的 role 块冲突；
// - 即使发送失败，也只记录日志，不影响主流程继续执行。
func (e *quickNoteProgressEmitter) Emit(stage, detail string) {
	if e == nil || !e.enablePush || e.outChan == nil {
		return
	}
	stage = strings.TrimSpace(stage)
	detail = strings.TrimSpace(detail)
	if stage == "" && detail == "" {
		return
	}

	reasoning := fmt.Sprintf("阶段：%s", stage)
	if detail != "" {
		reasoning += "\n" + detail
	}

	chunk, err := agent.ToOpenAIStream(&schema.Message{ReasoningContent: reasoning}, e.requestID, e.modelName, e.created, false)
	if err != nil {
		log.Printf("输出随口记阶段状态失败 stage=%s err=%v", stage, err)
		return
	}
	if chunk != "" {
		e.outChan <- chunk
	}
}

// tryHandleQuickNoteWithGraph 尝试用“随口记 graph”处理本次用户输入。
// 返回值语义：
// - handled=true：本次请求已在随口记链路处理完成（成功/失败都会返回文案）；
// - handled=false：不是随口记意图，调用方应回落普通聊天链路；
// - state：用于拼接最终“一次性正文回复”。
// 参数说明：
// - trustRoute=true：信任上游控制码，graph 跳过二次意图判定，直接进入时间校验/优先级/写库流程。
func (s *AgentService) tryHandleQuickNoteWithGraph(
	ctx context.Context,
	selectedModel *ark.ChatModel,
	userMessage string,
	userID int,
	chatID string,
	traceID string,
	trustRoute bool,
	emitStage func(stage, detail string),
) (handled bool, state *agent.QuickNoteState, err error) {
	if s.taskRepo == nil || selectedModel == nil {
		return false, nil, nil
	}

	state = agent.NewQuickNoteState(traceID, userID, chatID, userMessage)
	finalState, runErr := agent.RunQuickNoteGraph(ctx, agent.QuickNoteGraphRunInput{
		Model: selectedModel,
		State: state,
		Deps: agent.QuickNoteToolDeps{
			ResolveUserID: func(ctx context.Context) (int, error) {
				return userID, nil
			},
			CreateTask: func(ctx context.Context, req agent.QuickNoteCreateTaskRequest) (*agent.QuickNoteCreateTaskResult, error) {
				taskModel := &model.Task{
					UserID:      req.UserID,
					Title:       req.Title,
					Priority:    req.PriorityGroup,
					IsCompleted: false,
					DeadlineAt:  req.DeadlineAt,
				}
				created, createErr := s.taskRepo.AddTask(taskModel)
				if createErr != nil {
					return nil, createErr
				}
				return &agent.QuickNoteCreateTaskResult{
					TaskID:        created.ID,
					Title:         created.Title,
					PriorityGroup: created.Priority,
					DeadlineAt:    created.DeadlineAt,
				}, nil
			},
		},
		SkipIntentVerification: trustRoute,
		EmitStage:              emitStage,
	})
	if runErr != nil {
		return false, nil, runErr
	}
	if finalState == nil || !finalState.IsQuickNoteIntent {
		return false, nil, nil
	}

	return true, finalState, nil
}

// emitSingleAssistantCompletion 将单条完整回复包装成 OpenAI 兼容 chunk 流并写入 outChan。
// 说明：
// - 保持现有 OpenAI 兼容格式不变；
// - 正文只发一次，不做伪分段。
func emitSingleAssistantCompletion(outChan chan<- string, modelName, reply string) error {
	if strings.TrimSpace(modelName) == "" {
		modelName = "worker"
	}
	requestID := "chatcmpl-" + uuid.NewString()
	created := time.Now().Unix()

	chunk, err := agent.ToOpenAIStream(&schema.Message{Role: schema.Assistant, Content: reply}, requestID, modelName, created, true)
	if err != nil {
		return err
	}
	if chunk != "" {
		outChan <- chunk
	}

	finishChunk, err := agent.ToOpenAIFinishStream(requestID, modelName, created)
	if err != nil {
		return err
	}
	outChan <- finishChunk
	outChan <- "[DONE]"
	return nil
}

// buildQuickNoteFinalReply 生成最终的一次性正文回复。
// 组合策略：
// 1) 任务事实（标题/优先级/截止时间）由后端拼接，确保准确；
// 2) 轻松跟进句交给 AI 生成，贴合用户话题（避免硬编码“薯饼”这类场景分支）；
// 3) AI 生成失败时自动降级为固定友好文案，保证稳定可用。
func buildQuickNoteFinalReply(ctx context.Context, selectedModel *ark.ChatModel, userMessage string, state *agent.QuickNoteState) string {
	if state == nil {
		return "我这次没成功记上，别急，再发我一次我马上补上。"
	}

	// 仅当“确实拿到了有效 task_id”时才走成功文案，避免出现“回复成功但库里没数据”的错觉。
	if state.Persisted && state.PersistedTaskID > 0 {
		title := strings.TrimSpace(state.ExtractedTitle)
		if title == "" {
			title = "这条任务"
		}

		priorityText := "已安排优先级"
		if agent.IsValidTaskPriority(state.ExtractedPriority) {
			priorityText = fmt.Sprintf("优先级：%s", agent.PriorityLabelCN(state.ExtractedPriority))
		}

		deadlineText := ""
		if state.ExtractedDeadline != nil {
			deadlineText = fmt.Sprintf("；截止时间 %s", state.ExtractedDeadline.In(time.Local).Format("2006-01-02 15:04"))
		}

		factLine := fmt.Sprintf("好，给你安排上了：%s（%s%s）。", title, priorityText, deadlineText)

		// 优先复用“聚合规划阶段”产出的跟进句，避免再触发一次润色模型调用。
		if strings.TrimSpace(state.ExtractedBanter) != "" {
			return factLine + " " + strings.TrimSpace(state.ExtractedBanter)
		}
		if state.PlannedBySingleCall {
			// 快路径兜底：单请求聚合已走过一次模型调用，若未产出 banter 则直接使用固定文案，
			// 避免再发起额外模型请求拉高总时延。
			return factLine + " 已帮你稳稳记下，放心推进。"
		}

		banter, err := generateQuickNoteBanter(ctx, selectedModel, userMessage, title, priorityText, deadlineText)
		if err != nil {
			return factLine + " 这下可以先安心推进，不用等 ddl 来敲门了。"
		}
		if strings.TrimSpace(banter) == "" {
			return factLine + " 这下可以先安心推进，不用等 ddl 来敲门了。"
		}
		return factLine + " " + banter
	}

	if strings.TrimSpace(state.DeadlineValidationError) != "" {
		return "我识别到你给了时间，但格式不够明确，暂时不敢乱记。你可以改成比如：2026-03-20 18:30、明天下午3点、下周一上午9点，我立刻帮你安排。"
	}

	if strings.TrimSpace(state.AssistantReply) != "" {
		return strings.TrimSpace(state.AssistantReply)
	}
	return "这次没成功写入任务，我没跑路，再给我一次我就把它稳稳记上。"
}

// generateQuickNoteBanter 让模型根据用户原话生成一条“贴题轻松句”。
// 约束：
// - 只生成跟进语气，不承担事实表达；
// - 不得改动任务事实；
// - 输出控制在一句，方便直接拼接在事实句后。
func generateQuickNoteBanter(
	ctx context.Context,
	selectedModel *ark.ChatModel,
	userMessage string,
	title string,
	priorityText string,
	deadlineText string,
) (string, error) {
	if selectedModel == nil {
		return "", fmt.Errorf("model is nil")
	}

	prompt := fmt.Sprintf(`用户原话：%s
已确认事实：
- 任务标题：%s
- %s
- %s

请输出一句轻松自然的跟进话术（仅一句）。`,
		strings.TrimSpace(userMessage),
		strings.TrimSpace(title),
		strings.TrimSpace(priorityText),
		strings.TrimSpace(deadlineText),
	)

	messages := []*schema.Message{
		schema.SystemMessage(agent.QuickNoteReplyBanterPrompt),
		schema.UserMessage(prompt),
	}

	resp, err := selectedModel.Generate(ctx, messages,
		ark.WithThinking(&arkModel.Thinking{Type: arkModel.ThinkingTypeDisabled}),
		einoModel.WithTemperature(0.7),
		einoModel.WithMaxTokens(72),
	)
	if err != nil {
		return "", err
	}
	if resp == nil {
		return "", fmt.Errorf("empty response")
	}

	text := strings.TrimSpace(resp.Content)
	text = strings.Trim(text, "\"'“”‘’")
	if text == "" {
		return "", fmt.Errorf("empty content")
	}

	// 简单兜底：只保留首行，避免模型输出多段。
	if idx := strings.Index(text, "\n"); idx >= 0 {
		text = strings.TrimSpace(text[:idx])
	}
	return text, nil
}

// decideQuickNoteRouting 决定当前输入是否进入“随口记 graph”。
// 新策略：改为“模型控制码分流”，不再依赖关键词和本地猜测。
//
// 处理流程：
// 1) 先调用路由模型拿控制码（quick_note / chat）；
// 2) 控制码可解析时按模型判定分流；
// 3) 控制码超时/解析失败时，进入随口记 graph 做兜底意图识别，避免遗漏任务。
//
// 返回值说明：
// - EnterQuickNote=true：进入随口记 graph；
// - TrustRoute=true：跳过 graph 内二次意图判定；
// - Detail：用于阶段推送，向前端解释“为何进入该分支”。
func (s *AgentService) decideQuickNoteRouting(ctx context.Context, selectedModel *ark.ChatModel, userMessage string) quickNoteRoutingDecision {
	decision, err := s.routeByModelControlTag(ctx, selectedModel, userMessage)
	if err != nil {
		if deadline, ok := ctx.Deadline(); ok {
			log.Printf("quick note 路由控制码失败，进入 graph 兜底: err=%v parent_deadline_in_ms=%d route_timeout_ms=%d",
				err,
				time.Until(deadline).Milliseconds(),
				quickNoteRouteControlTimeout.Milliseconds(),
			)
		} else {
			log.Printf("quick note 路由控制码失败，进入 graph 兜底: err=%v parent_deadline=none route_timeout_ms=%d",
				err,
				quickNoteRouteControlTimeout.Milliseconds(),
			)
		}
		return quickNoteRoutingDecision{
			EnterQuickNote: true,
			TrustRoute:     false,
			Detail:         "路由判定暂不可用，已进入任务识别兜底流程。",
		}
	}

	switch decision.Action {
	case quickNoteRouteActionQuickNote:
		reason := strings.TrimSpace(decision.Reason)
		if reason == "" {
			reason = "模型识别到任务安排请求，准备执行随口记。"
		}
		return quickNoteRoutingDecision{
			EnterQuickNote: true,
			TrustRoute:     true,
			Detail:         reason,
		}
	case quickNoteRouteActionChat:
		return quickNoteRoutingDecision{
			EnterQuickNote: false,
			TrustRoute:     false,
			Detail:         "",
		}
	default:
		log.Printf("quick note 未知路由动作，进入 graph 兜底: action=%s raw=%s", decision.Action, decision.Raw)
		return quickNoteRoutingDecision{
			EnterQuickNote: true,
			TrustRoute:     false,
			Detail:         "路由结果异常，已进入任务识别兜底流程。",
		}
	}
}

// routeByModelControlTag 通过模型返回“控制码”完成分流。
// 输出协议由 QuickNoteRouteControlPrompt 约束，核心字段：
// - nonce：防伪随机串，防止模型回显历史脏内容；
// - action：quick_note / chat。
func (s *AgentService) routeByModelControlTag(ctx context.Context, selectedModel *ark.ChatModel, userMessage string) (*quickNoteRouteControlDecision, error) {
	if selectedModel == nil {
		return nil, fmt.Errorf("model is nil")
	}

	nonce := strings.ToLower(strings.ReplaceAll(uuid.NewString(), "-", ""))
	routeCtx, cancel := deriveRouteControlContext(ctx, quickNoteRouteControlTimeout)
	defer cancel()

	nowText := time.Now().In(time.Local).Format("2006-01-02 15:04")
	userPrompt := fmt.Sprintf("nonce=%s\n当前时间=%s\n用户输入=%s", nonce, nowText, strings.TrimSpace(userMessage))
	resp, err := selectedModel.Generate(routeCtx, []*schema.Message{
		schema.SystemMessage(agent.QuickNoteRouteControlPrompt),
		schema.UserMessage(userPrompt),
	},
		ark.WithThinking(&arkModel.Thinking{Type: arkModel.ThinkingTypeDisabled}),
		einoModel.WithTemperature(0),
		einoModel.WithMaxTokens(80),
	)
	if err != nil {
		return nil, err
	}
	if resp == nil {
		return nil, fmt.Errorf("empty route response")
	}

	raw := strings.TrimSpace(resp.Content)
	if raw == "" {
		return nil, fmt.Errorf("empty route content")
	}

	decision, parseErr := parseQuickNoteRouteControlTag(raw, nonce)
	if parseErr != nil {
		return nil, parseErr
	}
	return decision, nil
}

// deriveRouteControlContext 为“控制码路由”创建子上下文。
// 设计要点：
//  1. 如果父 ctx 没有 deadline，则增加一个默认上限，防止异常请求无限等待；
//  2. 如果父 ctx 已有更紧 deadline，则直接沿用父 ctx，不再额外缩短，
//     避免出现“父请求还活着，但子路由因更短超时提前失败”的误判。
func deriveRouteControlContext(parent context.Context, timeout time.Duration) (context.Context, context.CancelFunc) {
	if timeout <= 0 {
		return context.WithCancel(parent)
	}
	if deadline, ok := parent.Deadline(); ok {
		if time.Until(deadline) <= timeout {
			return context.WithCancel(parent)
		}
	}
	return context.WithTimeout(parent, timeout)
}

// parseQuickNoteRouteControlTag 解析模型输出控制码。
// 容错策略：
// - 允许大小写、属性顺序、标签内额外属性有差异；
// - 但 nonce 必须精确匹配，action 必须为 quick_note/chat。
func parseQuickNoteRouteControlTag(raw, expectedNonce string) (*quickNoteRouteControlDecision, error) {
	text := strings.TrimSpace(raw)
	if text == "" {
		return nil, fmt.Errorf("route content is empty")
	}

	header := quickNoteRouteHeaderRegex.FindStringSubmatch(text)
	if len(header) < 3 {
		return nil, fmt.Errorf("route header not found: %s", text)
	}

	nonce := strings.ToLower(strings.TrimSpace(header[1]))
	if nonce != strings.ToLower(strings.TrimSpace(expectedNonce)) {
		return nil, fmt.Errorf("route nonce mismatch")
	}

	actionText := strings.ToLower(strings.TrimSpace(header[2]))
	action := quickNoteRouteAction(actionText)
	if action != quickNoteRouteActionQuickNote && action != quickNoteRouteActionChat {
		return nil, fmt.Errorf("invalid route action: %s", actionText)
	}

	reason := ""
	reasonMatch := quickNoteRouteReasonRegex.FindStringSubmatch(text)
	if len(reasonMatch) >= 2 {
		reason = strings.TrimSpace(reasonMatch[1])
	}

	return &quickNoteRouteControlDecision{
		Action: action,
		Reason: reason,
		Raw:    text,
	}, nil
}

// persistChatAfterReply 在“随口记 graph”返回后，复用当前项目的后置持久化策略：
// 1) 用户消息写 Redis + outbox/DB；
// 2) 助手消息写 Redis + outbox/DB。
func (s *AgentService) persistChatAfterReply(
	ctx context.Context,
	userID int,
	chatID string,
	userMessage string,
	assistantReply string,
	errChan chan error,
) {
	if err := s.agentCache.PushMessage(ctx, chatID, &schema.Message{Role: schema.User, Content: userMessage}); err != nil {
		log.Printf("写入用户消息到 Redis 失败: %v", err)
	}

	if err := s.saveChatHistoryReliable(ctx, model.ChatHistoryPersistPayload{
		UserID:         userID,
		ConversationID: chatID,
		Role:           "user",
		Message:        userMessage,
	}); err != nil {
		pushErrNonBlocking(errChan, err)
		return
	}

	if err := s.agentCache.PushMessage(context.Background(), chatID, &schema.Message{Role: schema.Assistant, Content: assistantReply}); err != nil {
		log.Printf("写入助手消息到 Redis 失败: %v", err)
	}

	if err := s.saveChatHistoryReliable(context.Background(), model.ChatHistoryPersistPayload{
		UserID:         userID,
		ConversationID: chatID,
		Role:           "assistant",
		Message:        assistantReply,
	}); err != nil {
		pushErrNonBlocking(errChan, err)
	}
}
