package agentsvc

import (
	"context"
	"fmt"
	"log"
	"strings"
	"time"

	agentgraph "github.com/LoveLosita/smartflow/backend/agent2/graph"
	agentllm "github.com/LoveLosita/smartflow/backend/agent2/llm"
	agentmodel "github.com/LoveLosita/smartflow/backend/agent2/model"
	agentnode "github.com/LoveLosita/smartflow/backend/agent2/node"
	agentrouter "github.com/LoveLosita/smartflow/backend/agent2/router"
	agentstream "github.com/LoveLosita/smartflow/backend/agent2/stream"
	"github.com/LoveLosita/smartflow/backend/model"
	"github.com/cloudwego/eino-ext/components/model/ark"
	"github.com/cloudwego/eino/schema"
	"github.com/google/uuid"
)

// quickNoteRoutingDecision 只是路由层结果的本地别名。
// 保留这个别名是为了尽量少改调用侧（agent.go 中的字段访问保持不变）。
type quickNoteRoutingDecision = agentrouter.RoutingDecision

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
	reasoning  strings.Builder
	startedAt  *time.Time
}

// newQuickNoteProgressEmitter 构造“阶段进度推送器”。
// 该推送器只负责发 reasoning 块，不负责正文回复。
func newQuickNoteProgressEmitter(outChan chan<- string, modelName string, enable bool) *quickNoteProgressEmitter {
	// 1. 模型名兜底，避免出现空 model 字段导致客户端兼容性问题。
	resolvedModel := strings.TrimSpace(modelName)
	if resolvedModel == "" {
		resolvedModel = "worker"
	}
	// 2. 每次请求生成独立 request_id，方便前端或日志侧关联本次流式输出。
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
// 1) 这里不输出 role，避免和后续正文 role 块冲突；
// 2) 即使发送失败，也只记录日志，不影响主流程继续执行。
func (e *quickNoteProgressEmitter) Emit(stage, detail string) {
	// 1. 推送器不可用（nil/禁用/无通道）时直接返回，避免 panic。
	if e == nil || !e.enablePush || e.outChan == nil {
		return
	}
	// 2. 统一清理空白，避免日志和输出里出现异常空字符串。
	stage = strings.TrimSpace(stage)
	detail = strings.TrimSpace(detail)
	if stage == "" && detail == "" {
		return
	}
	if e.startedAt == nil {
		now := time.Now()
		e.startedAt = &now
	}
	if e.reasoning.Len() > 0 {
		e.reasoning.WriteString("\n\n")
	}
	if stage != "" {
		e.reasoning.WriteString("阶段：")
		e.reasoning.WriteString(stage)
	}
	if detail != "" {
		if stage != "" {
			e.reasoning.WriteString("\n")
		}
		e.reasoning.WriteString(detail)
	}

	// 3. 调用目的：阶段提示统一走 agent2/stream 的 reasoning chunk 包装，
	//    避免 service 层继续自己拼 OpenAI 兼容 JSON。
	err := agentstream.EmitStageAsReasoning(func(payload string) error {
		e.outChan <- payload
		return nil
	}, e.requestID, e.modelName, e.created, stage, detail, false)
	if err != nil {
		// 3.1 阶段推送失败不应影响主链路，只打日志即可。
		log.Printf("输出随口记阶段状态失败 stage=%s err=%v", stage, err)
		return
	}
}

func (e *quickNoteProgressEmitter) HistoryText() string {
	if e == nil {
		return ""
	}
	return strings.TrimSpace(e.reasoning.String())
}

func (e *quickNoteProgressEmitter) StartedAt() *time.Time {
	if e == nil || e.startedAt == nil {
		return nil
	}
	startCopy := *e.startedAt
	return &startCopy
}

func (e *quickNoteProgressEmitter) DurationSeconds(end time.Time) int {
	if e == nil || e.startedAt == nil {
		return 0
	}
	if !end.After(*e.startedAt) {
		return 0
	}
	return int(end.Sub(*e.startedAt) / time.Second)
}

// tryHandleQuickNoteWithGraph 尝试用“随口记 graph”处理本次用户输入。
// 返回值语义：
// 1) handled=true：本次请求已在随口记链路处理完成（成功/失败都会返回文案）；
// 2) handled=false：不是随口记意图，调用方应回落普通聊天链路；
// 3) state：用于拼接最终“一次性正文回复”。
func (s *AgentService) tryHandleQuickNoteWithGraph(
	ctx context.Context,
	selectedModel *ark.ChatModel,
	userMessage string,
	userID int,
	chatID string,
	traceID string,
	trustRoute bool,
	emitStage func(stage, detail string),
) (handled bool, state *agentmodel.QuickNoteState, err error) {
	// 1. 依赖预检：taskRepo 或模型未注入时，不做随口记处理，交给上层回落聊天。
	if s.taskRepo == nil || selectedModel == nil {
		return false, nil, nil
	}

	// 2. 初始化随口记状态对象（贯穿 graph 全流程的共享上下文）。
	state = agentmodel.NewQuickNoteState(traceID, userID, chatID, userMessage)

	// 3. 执行 quick note graph。
	//    本次依赖注入了两个“工具能力”：
	//    3.1 ResolveUserID：从当前请求上下文确定 user_id；
	//    3.2 CreateTask：真正执行任务写库。
	finalState, runErr := agentgraph.RunQuickNoteGraph(ctx, agentnode.QuickNoteGraphRunInput{
		Model: selectedModel,
		State: state,
		Deps: agentnode.QuickNoteToolDeps{
			ResolveUserID: func(ctx context.Context) (int, error) {
				// 当前链路 userID 已由上层鉴权拿到，这里直接复用。
				return userID, nil
			},
			CreateTask: func(ctx context.Context, req agentnode.QuickNoteCreateTaskRequest) (*agentnode.QuickNoteCreateTaskResult, error) {
				// 3.2.1 把 quick note 的工具入参映射成项目 Task 模型。
				taskModel := &model.Task{
					UserID:             req.UserID,
					Title:              req.Title,
					Priority:           req.PriorityGroup,
					IsCompleted:        false,
					DeadlineAt:         req.DeadlineAt,
					UrgencyThresholdAt: req.UrgencyThresholdAt,
				}

				// 3.2.2 调用 DAO 写库。
				created, createErr := s.taskRepo.AddTask(taskModel)
				if createErr != nil {
					return nil, createErr
				}

				// 3.2.3 把写库结果回填给 graph 状态，用于后续回复拼装。
				return &agentnode.QuickNoteCreateTaskResult{
					TaskID:             created.ID,
					Title:              created.Title,
					PriorityGroup:      created.Priority,
					DeadlineAt:         created.DeadlineAt,
					UrgencyThresholdAt: created.UrgencyThresholdAt,
				}, nil
			},
		},
		SkipIntentVerification: trustRoute,
		EmitStage:              emitStage,
	})
	if runErr != nil {
		// 4. graph 执行失败由上层统一决定是否回退普通聊天。
		return false, nil, runErr
	}

	// 5. graph 正常结束但判定“非随口记”时，明确返回 handled=false。
	if finalState == nil || !finalState.IsQuickNoteIntent {
		return false, nil, nil
	}
	// 6. 走到这里表示随口记链路已完成（含写库成功或业务失败反馈文案）。
	return true, finalState, nil
}

// emitSingleAssistantCompletion 将单条完整回复包装成 OpenAI 兼容 chunk 流并写入 outChan。
// 说明：
// 1) 保持现有 OpenAI 兼容格式不变；
// 2) 正文只发一次，不做伪分段。
func emitSingleAssistantCompletion(outChan chan<- string, modelName, reply string) error {
	// 1. 模型名兜底，保持 OpenAI 兼容响应字段完整。
	if strings.TrimSpace(modelName) == "" {
		modelName = "worker"
	}
	requestID := "chatcmpl-" + uuid.NewString()
	created := time.Now().Unix()

	emit := func(payload string) error {
		outChan <- payload
		return nil
	}
	if err := agentstream.EmitAssistantReply(emit, requestID, modelName, created, reply, true); err != nil {
		return err
	}
	if err := agentstream.EmitFinish(emit, requestID, modelName, created); err != nil {
		return err
	}
	return agentstream.EmitDone(emit)
}

// buildQuickNoteFinalReply 生成最终的一次性正文回复。
// 组合策略：
// 1) 任务事实（标题/优先级/截止时间）由后端拼接，确保准确；
// 2) 轻松跟进句交给 AI 生成，贴合用户话题；
// 3) AI 生成失败时自动降级为固定友好文案，保证稳定可用。
func buildQuickNoteFinalReply(ctx context.Context, selectedModel *ark.ChatModel, userMessage string, state *agentmodel.QuickNoteState) string {
	// 1. 极端兜底：状态为空时给出稳定失败文案，避免返回空字符串。
	if state == nil {
		return "我这次没成功记上，别急，再发我一次我马上补上。"
	}

	// 仅当“确实拿到了有效 task_id”时才走成功文案，避免出现“回复成功但库里没数据”的错觉。
	if state.Persisted && state.PersistedTaskID > 0 {
		// 2. 组装“事实段”：标题 + 优先级 + 截止时间。
		title := strings.TrimSpace(state.ExtractedTitle)
		if title == "" {
			title = "这条任务"
		}

		priorityText := "已安排优先级"
		if agentmodel.IsValidTaskPriority(state.ExtractedPriority) {
			priorityText = fmt.Sprintf("优先级：%s", agentmodel.PriorityLabelCN(state.ExtractedPriority))
		}

		deadlineText := ""
		if state.ExtractedDeadline != nil {
			deadlineText = fmt.Sprintf("；截止时间 %s", state.ExtractedDeadline.In(time.Local).Format("2006-01-02 15:04"))
		}

		factLine := fmt.Sprintf("好，给你安排上了：%s（%s%s）。", title, priorityText, deadlineText)

		// 2.1 如果 graph 单次请求已生成 banter，直接使用，避免重复调用模型。
		if strings.TrimSpace(state.ExtractedBanter) != "" {
			return factLine + " " + strings.TrimSpace(state.ExtractedBanter)
		}
		// 2.2 聚合调用模式下，通常已在主流程完成风格化，给稳定文案即可。
		if state.PlannedBySingleCall {
			return factLine + " 已帮你稳稳记下，放心推进。"
		}

		// 2.3 兜底生成轻松跟进句；失败则降级固定文案，确保体验连续。
		banter, err := agentllm.GenerateQuickNoteBanter(ctx, selectedModel, userMessage, title, priorityText, deadlineText)
		if err != nil {
			return factLine + " 这下可以先安心推进，不用等 ddl 来敲门了。"
		}
		if strings.TrimSpace(banter) == "" {
			return factLine + " 这下可以先安心推进，不用等 ddl 来敲门了。"
		}
		return factLine + " " + banter
	}

	// 3. 若时间校验失败，优先返回“可执行的修正引导”。
	if strings.TrimSpace(state.DeadlineValidationError) != "" {
		return "我识别到你给了时间，但格式不够明确，暂时不敢乱记。你可以改成比如：2026-03-20 18:30、明天下午3点、下周一上午9点，我立刻帮你安排。"
	}

	// 4. 若 graph 已给出助手回复（例如非意图/业务失败原因），优先透传。
	if strings.TrimSpace(state.AssistantReply) != "" {
		return strings.TrimSpace(state.AssistantReply)
	}
	// 5. 最终兜底文案。
	return "这次没成功写入任务，我没跑路，再给我一次我就把它稳稳记上。"
}

// decideQuickNoteRouting 决定当前输入是否进入“随口记 graph”。
// 该函数只是服务层薄封装，具体控制码解析逻辑已下沉到 agent/route 包。
func (s *AgentService) decideQuickNoteRouting(ctx context.Context, selectedModel *ark.ChatModel, userMessage string) quickNoteRoutingDecision {
	// 这里保留方法是为了让 AgentService 对外语义完整，
	// 同时避免上层调用方直接依赖 route 包，降低耦合。
	_ = s
	return agentrouter.DecideQuickNoteRouting(ctx, selectedModel, userMessage)
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
	assistantReasoning string,
	assistantReasoningDurationSeconds int,
	retryMeta *chatRetryMeta,
	userTokens int,
	assistantTokens int,
	errChan chan error,
) {
	// 1. 先把用户消息写入 Redis，保证会话上下文“马上可见”。
	userMsg := &schema.Message{Role: schema.User, Content: userMessage}
	if retryExtra := retryMeta.CacheExtra(); len(retryExtra) > 0 {
		userMsg.Extra = retryExtra
	}
	if err := s.agentCache.PushMessage(ctx, chatID, userMsg); err != nil {
		log.Printf("写入用户消息到 Redis 失败: %v", err)
	}

	// 2. 再把用户消息写入可靠持久化通道（outbox 或同步 DB）。
	if err := s.PersistChatHistory(ctx, model.ChatHistoryPersistPayload{
		UserID:                      userID,
		ConversationID:              chatID,
		Role:                        "user",
		Message:                     userMessage,
		ReasoningContent:            "",
		ReasoningDurationSeconds:    0,
		RetryGroupID:                retryMeta.GroupIDPtr(),
		RetryIndex:                  retryMeta.IndexPtr(),
		RetryFromUserMessageID:      retryMeta.FromUserMessageIDPtr(),
		RetryFromAssistantMessageID: retryMeta.FromAssistantMessageIDPtr(),
		TokensConsumed:              userTokens,
	}); err != nil {
		pushErrNonBlocking(errChan, err)
		return
	}
	userCreatedAt := time.Now()
	s.appendConversationHistoryCacheOptimistically(
		context.Background(),
		userID,
		chatID,
		buildOptimisticConversationHistoryItem(
			"user",
			userMessage,
			"",
			0,
			retryMeta,
			userCreatedAt,
		),
	)

	// 3. 助手消息同样遵循“Redis 先行 + 可靠持久化补齐”策略。
	assistantMsg := &schema.Message{Role: schema.Assistant, Content: assistantReply, ReasoningContent: assistantReasoning}
	if assistantReasoningDurationSeconds > 0 {
		assistantMsg.Extra = map[string]any{"reasoning_duration_seconds": assistantReasoningDurationSeconds}
	}
	if retryExtra := retryMeta.CacheExtra(); len(retryExtra) > 0 {
		if assistantMsg.Extra == nil {
			assistantMsg.Extra = make(map[string]any, len(retryExtra))
		}
		for key, value := range retryExtra {
			assistantMsg.Extra[key] = value
		}
	}
	if err := s.agentCache.PushMessage(context.Background(), chatID, assistantMsg); err != nil {
		log.Printf("写入助手消息到 Redis 失败: %v", err)
	}

	// 4. 助手消息持久化失败不阻断主流程，通过 errChan 异步上报。
	if err := s.PersistChatHistory(context.Background(), model.ChatHistoryPersistPayload{
		UserID:                      userID,
		ConversationID:              chatID,
		Role:                        "assistant",
		Message:                     assistantReply,
		ReasoningContent:            assistantReasoning,
		ReasoningDurationSeconds:    assistantReasoningDurationSeconds,
		RetryGroupID:                retryMeta.GroupIDPtr(),
		RetryIndex:                  retryMeta.IndexPtr(),
		RetryFromUserMessageID:      retryMeta.FromUserMessageIDPtr(),
		RetryFromAssistantMessageID: retryMeta.FromAssistantMessageIDPtr(),
		TokensConsumed:              assistantTokens,
	}); err != nil {
		pushErrNonBlocking(errChan, err)
		return
	}
	s.appendConversationHistoryCacheOptimistically(
		context.Background(),
		userID,
		chatID,
		buildOptimisticConversationHistoryItem(
			"assistant",
			assistantReply,
			assistantReasoning,
			assistantReasoningDurationSeconds,
			retryMeta,
			userCreatedAt.Add(time.Millisecond),
		),
	)
}
