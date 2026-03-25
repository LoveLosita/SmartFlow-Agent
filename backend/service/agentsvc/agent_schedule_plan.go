package agentsvc

import (
	"context"
	"errors"
	"log"
	"strings"

	agentgraph "github.com/LoveLosita/smartflow/backend/agent2/graph"
	agentmodel "github.com/LoveLosita/smartflow/backend/agent2/model"
	agentnode "github.com/LoveLosita/smartflow/backend/agent2/node"
	"github.com/LoveLosita/smartflow/backend/conv"
	"github.com/LoveLosita/smartflow/backend/model"
	"github.com/LoveLosita/smartflow/backend/pkg"
	"github.com/cloudwego/eino-ext/components/model/ark"
	"github.com/cloudwego/eino/schema"
	"github.com/spf13/viper"
)

// runSchedulePlanFlow 执行“智能排程”分支。
//
// 职责边界：
// 1. 负责把本次请求接入 scheduleplan graph，并注入运行依赖。
// 2. 负责读取对话历史（优先 Redis，未命中再回源 DB）用于连续对话微调。
// 3. 负责把排程预览快照写入 Redis（供查询接口拉取 JSON）。
// 4. 负责返回给上层“可直接发给用户的最终文本回复”。
// 5. 不负责聊天持久化（由 AgentChat 主链路统一处理）。
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
	// 1. 依赖预检：缺硬依赖时直接失败，避免进入 graph 后才出现空指针或半途失败。
	// 1.1 SmartPlanningMultiRaw / HybridScheduleWithPlanMulti / ResolvePlanningWindow 任一缺失都无法继续。
	// 1.2 selectedModel 为空时无法执行 LLM 节点，直接返回错误由上层处理。
	if s.SmartPlanningMultiRawFunc == nil || s.HybridScheduleWithPlanMultiFunc == nil || s.ResolvePlanningWindowFunc == nil {
		return "", errors.New("schedule plan service dependencies are not ready")
	}
	if selectedModel == nil {
		return "", errors.New("schedule plan model is nil")
	}

	// 2. 连续对话微调前置处理：先尝试读取“上一版预览快照”，再清理旧 key。
	// 2.1 先读后删的原因：
	// 2.1.1 若先删再读，会丢失“连续微调起点”；
	// 2.1.2 先读可让本轮在内存中复用上轮 HybridEntries。
	// 2.2 清理旧 key 仍然保留，避免前端在本轮进行中误读到旧结果。
	var previousPreview *model.SchedulePlanPreviewCache
	if s.cacheDAO != nil {
		preview, getErr := s.cacheDAO.GetSchedulePlanPreviewFromCache(ctx, userID, chatID)
		if getErr != nil {
			log.Printf("读取上一版排程预览失败 chat_id=%s: %v", chatID, getErr)
		} else {
			previousPreview = preview
		}
		if delErr := s.cacheDAO.DeleteSchedulePlanPreviewFromCache(ctx, userID, chatID); delErr != nil {
			log.Printf("清理旧排程预览失败 chat_id=%s: %v", chatID, delErr)
		}
	}
	// 2.3 Redis miss 时回落 MySQL 快照：
	// 2.3.1 目的：即使 Redis TTL 过期，也能延续同会话微调语境；
	// 2.3.2 回填：命中 DB 后尝试回填 Redis，提高后续读取命中率；
	// 2.3.3 失败策略：DB 读取异常只打日志，链路继续按“无历史快照”执行。
	if previousPreview == nil && s.repo != nil {
		snapshot, snapshotErr := s.repo.GetScheduleStateSnapshot(ctx, userID, chatID)
		if snapshotErr != nil {
			log.Printf("从 MySQL 读取排程快照失败 chat_id=%s: %v", chatID, snapshotErr)
		} else if snapshot != nil {
			previousPreview = snapshotToSchedulePlanPreviewCache(snapshot)
			if s.cacheDAO != nil && previousPreview != nil {
				if setErr := s.cacheDAO.SetSchedulePlanPreviewToCache(ctx, userID, chatID, previousPreview); setErr != nil {
					log.Printf("回填排程预览缓存失败 chat_id=%s: %v", chatID, setErr)
				}
			}
		}
	}

	// 3. 读取对话历史：先快后稳。
	// 3.1 先查 Redis，命中则避免回源 DB，降低请求时延。
	// 3.2 Redis 异常仅记录日志，不中断主流程（回源 DB 兜底）。
	var chatHistory []*schema.Message
	if s.agentCache != nil {
		history, err := s.agentCache.GetHistory(ctx, chatID)
		if err != nil {
			log.Printf("获取排程对话历史失败 chat_id=%s: %v", chatID, err)
		} else if history != nil {
			chatHistory = history
		}
	}

	// 3.3 Redis 未命中时回源 DB，保证链路在缓存波动时仍可用。
	// 3.4 DB 回源失败同样只记日志并继续，让 graph 按“无历史”降级运行。
	if chatHistory == nil && s.repo != nil {
		histories, hisErr := s.repo.GetUserChatHistories(ctx, userID, pkg.HistoryFetchLimitByModel("worker"), chatID)
		if hisErr != nil {
			log.Printf("回源 DB 获取排程对话历史失败 chat_id=%s: %v", chatID, hisErr)
		} else {
			chatHistory = conv.ToEinoMessages(histories)
		}
	}

	// 4. 执行 graph 主流程。
	// 4.1 这里只负责参数拼装与调用，不在 service 层重复实现 graph 节点逻辑。
	// 4.2 并发度/预算从配置注入，避免把调优参数写死在代码中。
	state := agentmodel.NewSchedulePlanState(traceID, userID, chatID)
	// 4.3 连续对话微调注入：
	// 4.3.1 若命中上轮预览，则把任务类/混合条目/分配结果注入 state；
	// 4.3.2 这样 rough_build 可按需复用旧底板，避免每轮都重新粗排。
	if previousPreview != nil {
		state.HasPreviousPreview = true
		state.PreviousTaskClassIDs = append([]int(nil), previousPreview.TaskClassIDs...)
		state.PreviousHybridEntries = cloneHybridEntries(previousPreview.HybridEntries)
		state.PreviousAllocatedItems = cloneTaskClassItems(previousPreview.AllocatedItems)
		state.PreviousCandidatePlans = cloneWeekSchedules(previousPreview.CandidatePlans)
	}
	finalState, runErr := agentgraph.RunSchedulePlanGraph(ctx, agentnode.SchedulePlanGraphRunInput{
		Model: selectedModel,
		State: state,
		Deps: agentnode.SchedulePlanToolDeps{
			SmartPlanningMultiRaw:       s.SmartPlanningMultiRawFunc,
			HybridScheduleWithPlanMulti: s.HybridScheduleWithPlanMultiFunc,
			ResolvePlanningWindow:       s.ResolvePlanningWindowFunc,
		},
		UserMessage:            userMessage,
		Extra:                  extra,
		ChatHistory:            chatHistory,
		EmitStage:              emitStage,
		OutChan:                outChan,
		ModelName:              modelName,
		DailyRefineConcurrency: viper.GetInt("agent.dailyRefineConcurrency"),
		WeeklyAdjustBudget:     viper.GetInt("agent.weeklyAdjustBudget"),
	})
	if runErr != nil {
		// 4.3 graph 失败直接上抛，由上层决定回落或报错。
		return "", runErr
	}

	// 5. 组装最终回复文本。
	// 5.1 明确移除“把排程结果序列化成 JSON 文本直接回传”的抽象，
	//     避免在 SSE 聊天链路里吐出原始 JSON，影响前端展示与用户体验。
	// 5.2 当 finalState 为空或 summary 为空时，返回统一兜底文案，保证接口有稳定输出。
	if finalState == nil {
		return "排程流程异常，请稍后重试。", nil
	}
	reply := strings.TrimSpace(finalState.FinalSummary)
	if reply == "" {
		reply = "排程流程已完成，但未生成结果摘要。"
	}

	// 6. 旁路写入排程预览缓存（结构化 JSON），给查询接口拉取。
	// 6.1 失败只记日志，不影响本次对话回复；
	// 6.2 成功后前端可通过 conversation_id 获取 candidate_plans。
	s.saveSchedulePlanPreviewAgent2(ctx, userID, chatID, finalState)
	return reply, nil
}
