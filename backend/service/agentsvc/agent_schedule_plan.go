package agentsvc

import (
	"context"
	"errors"
	"log"
	"strings"

	"github.com/LoveLosita/smartflow/backend/agent/scheduleplan"
	"github.com/LoveLosita/smartflow/backend/conv"
	"github.com/LoveLosita/smartflow/backend/pkg"
	"github.com/cloudwego/eino-ext/components/model/ark"
	"github.com/cloudwego/eino/schema"
)

// runSchedulePlanFlow 执行"智能排程"分支。
//
// 职责边界：
// 1. 负责把本次请求接入 scheduleplan 执行器；
// 2. 负责注入排程依赖（SmartPlanning / BatchApplyPlans / GetTaskClassByID）；
// 3. 负责对话历史获取，支持连续对话微调；
// 4. 不负责聊天持久化（由 AgentChat 主流程统一收口）。
func (s *AgentService) runSchedulePlanFlow(
	ctx context.Context,
	selectedModel *ark.ChatModel,
	userMessage string,
	userID int,
	chatID string,
	traceID string,
	extra map[string]any,
	emitStage func(stage, detail string),
	outChan chan<- string,
	modelName string,
) (string, error) {
	// 1. 依赖预检：排程依赖函数必须注入，否则无法完成排程链路。
	if s.SmartPlanningRawFunc == nil || s.BatchApplyPlansFunc == nil || s.GetTaskClassByIDFunc == nil {
		return "", errors.New("schedule plan service dependencies are not ready")
	}
	if selectedModel == nil {
		return "", errors.New("schedule plan model is nil")
	}

	// 2. 获取对话历史，用于连续对话微调场景。
	//    优先从 Redis 读取，未命中时回源 DB。
	var chatHistory []*schema.Message
	if s.agentCache != nil {
		history, err := s.agentCache.GetHistory(ctx, chatID)
		if err != nil {
			log.Printf("获取排程对话历史失败 chat_id=%s: %v", chatID, err)
		} else if history != nil {
			chatHistory = history
		}
	}

	// 2.1 缓存未命中时回源 DB。
	if chatHistory == nil && s.repo != nil {
		histories, hisErr := s.repo.GetUserChatHistories(ctx, userID, pkg.HistoryFetchLimitByModel("worker"), chatID)
		if hisErr != nil {
			log.Printf("回源 DB 获取排程对话历史失败 chat_id=%s: %v", chatID, hisErr)
		} else {
			chatHistory = conv.ToEinoMessages(histories)
		}
	}

	// 3. 初始化排程状态对象。
	state := scheduleplan.NewSchedulePlanState(traceID, userID, chatID)

	// 4. 构建依赖注入并执行 graph。
	finalState, runErr := scheduleplan.RunSchedulePlanGraph(ctx, scheduleplan.SchedulePlanGraphRunInput{
		Model: selectedModel,
		State: state,
		Deps: scheduleplan.SchedulePlanToolDeps{
			SmartPlanningRaw:       s.SmartPlanningRawFunc,
			BatchApplyPlans:        s.BatchApplyPlansFunc,
			GetTaskClassByID:       s.GetTaskClassByIDFunc,
			HybridScheduleWithPlan: s.HybridScheduleWithPlanFunc,
		},
		UserMessage: userMessage,
		Extra:       extra,
		ChatHistory: chatHistory,
		EmitStage:   emitStage,
		OutChan:     outChan,
		ModelName:   modelName,
	})

	if runErr != nil {
		return "", runErr
	}

	// 5. 提取最终回复。
	if finalState == nil {
		return "排程流程异常，请稍后重试。", nil
	}

	reply := strings.TrimSpace(finalState.FinalSummary)
	if reply == "" {
		reply = "排程流程已完成，但未生成结果摘要。"
	}

	return reply, nil
}
