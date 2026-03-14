package service

import (
	"context"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/LoveLosita/smartflow/backend/agent/chat"
	"github.com/LoveLosita/smartflow/backend/agent/quicknote"
	"github.com/LoveLosita/smartflow/backend/agent/route"
	"github.com/LoveLosita/smartflow/backend/model"
	"github.com/cloudwego/eino-ext/components/model/ark"
	einoModel "github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
	"github.com/google/uuid"
	arkModel "github.com/volcengine/volcengine-go-sdk/service/arkruntime/model"
)

// quickNoteRoutingDecision 只是路由层结果的本地别名。
// 保留这个别名是为了尽量少改调用侧（agent.go 中的字段访问保持不变）。
type quickNoteRoutingDecision = route.RoutingDecision

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
// 1) 这里不输出 role，避免和后续正文 role 块冲突；
// 2) 即使发送失败，也只记录日志，不影响主流程继续执行。
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

	chunk, err := chat.ToOpenAIStream(&schema.Message{ReasoningContent: reasoning}, e.requestID, e.modelName, e.created, false)
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
) (handled bool, state *quicknote.QuickNoteState, err error) {
	if s.taskRepo == nil || selectedModel == nil {
		return false, nil, nil
	}

	state = quicknote.NewQuickNoteState(traceID, userID, chatID, userMessage)
	finalState, runErr := quicknote.RunQuickNoteGraph(ctx, quicknote.QuickNoteGraphRunInput{
		Model: selectedModel,
		State: state,
		Deps: quicknote.QuickNoteToolDeps{
			ResolveUserID: func(ctx context.Context) (int, error) {
				return userID, nil
			},
			CreateTask: func(ctx context.Context, req quicknote.QuickNoteCreateTaskRequest) (*quicknote.QuickNoteCreateTaskResult, error) {
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
				return &quicknote.QuickNoteCreateTaskResult{
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
// 1) 保持现有 OpenAI 兼容格式不变；
// 2) 正文只发一次，不做伪分段。
func emitSingleAssistantCompletion(outChan chan<- string, modelName, reply string) error {
	if strings.TrimSpace(modelName) == "" {
		modelName = "worker"
	}
	requestID := "chatcmpl-" + uuid.NewString()
	created := time.Now().Unix()

	chunk, err := chat.ToOpenAIStream(&schema.Message{Role: schema.Assistant, Content: reply}, requestID, modelName, created, true)
	if err != nil {
		return err
	}
	if chunk != "" {
		outChan <- chunk
	}

	finishChunk, err := chat.ToOpenAIFinishStream(requestID, modelName, created)
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
// 2) 轻松跟进句交给 AI 生成，贴合用户话题；
// 3) AI 生成失败时自动降级为固定友好文案，保证稳定可用。
func buildQuickNoteFinalReply(ctx context.Context, selectedModel *ark.ChatModel, userMessage string, state *quicknote.QuickNoteState) string {
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
		if quicknote.IsValidTaskPriority(state.ExtractedPriority) {
			priorityText = fmt.Sprintf("优先级：%s", quicknote.PriorityLabelCN(state.ExtractedPriority))
		}

		deadlineText := ""
		if state.ExtractedDeadline != nil {
			deadlineText = fmt.Sprintf("；截止时间 %s", state.ExtractedDeadline.In(time.Local).Format("2006-01-02 15:04"))
		}

		factLine := fmt.Sprintf("好，给你安排上了：%s（%s%s）。", title, priorityText, deadlineText)
		if strings.TrimSpace(state.ExtractedBanter) != "" {
			return factLine + " " + strings.TrimSpace(state.ExtractedBanter)
		}
		if state.PlannedBySingleCall {
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
// 1) 只生成跟进语气，不承担事实表达；
// 2) 不得改动任务事实；
// 3) 输出控制在一句，方便直接拼接在事实句后。
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
		schema.SystemMessage(quicknote.QuickNoteReplyBanterPrompt),
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
	if idx := strings.Index(text, "\n"); idx >= 0 {
		text = strings.TrimSpace(text[:idx])
	}
	return text, nil
}

// decideQuickNoteRouting 决定当前输入是否进入“随口记 graph”。
// 该函数只是服务层薄封装，具体控制码解析逻辑已下沉到 agent/route 包。
func (s *AgentService) decideQuickNoteRouting(ctx context.Context, selectedModel *ark.ChatModel, userMessage string) quickNoteRoutingDecision {
	_ = s
	return route.DecideQuickNoteRouting(ctx, selectedModel, userMessage)
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
