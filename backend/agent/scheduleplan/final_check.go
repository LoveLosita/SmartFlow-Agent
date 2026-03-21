package scheduleplan

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/LoveLosita/smartflow/backend/model"
	"github.com/cloudwego/eino-ext/components/model/ark"
	einoModel "github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
	arkModel "github.com/volcengine/volcengine-go-sdk/service/arkruntime/model"
)

// runFinalCheckNode 负责“终审校验 + 总结生成”。
//
// 职责边界：
// 1. 负责执行物理校验（冲突、节次越界、数量核对）；
// 2. 负责在校验失败时回退到 MergeSnapshot；
// 3. 负责生成最终给用户看的自然语言总结；
// 4. 不负责写库（本期只做预览）。
func runFinalCheckNode(
	ctx context.Context,
	st *SchedulePlanState,
	chatModel *ark.ChatModel,
	emitStage func(stage, detail string),
) (*SchedulePlanState, error) {
	if st == nil {
		return nil, fmt.Errorf("schedule plan final check: nil state")
	}

	emitStage("schedule_plan.final_check.start", "正在进行终审校验。")

	// 1. 先做物理校验。
	issues := physicsCheck(st)
	if len(issues) > 0 {
		emitStage("schedule_plan.final_check.issues", fmt.Sprintf("发现 %d 个问题，已回退到日内优化结果。", len(issues)))
		// 1.1 回退策略：
		// 1.1.1 优先回退到 merge 快照（已经过冲突校验）；
		// 1.1.2 若快照为空，保留当前结果继续走总结，保证可返回。
		if len(st.MergeSnapshot) > 0 {
			st.HybridEntries = deepCopyEntries(st.MergeSnapshot)
		}
	}

	// 2. 生成人性化总结。
	//
	// 2.1 总结失败不影响主流程；
	// 2.2 失败时使用兜底文案，保证前端始终有可展示文本。
	summary, err := generateHumanSummary(ctx, chatModel, st.HybridEntries, st.Constraints, st.WeeklyActionLogs)
	if err != nil || strings.TrimSpace(summary) == "" {
		st.FinalSummary = fmt.Sprintf("排程优化完成，共安排了 %d 个任务。", countSuggested(st.HybridEntries))
	} else {
		st.FinalSummary = strings.TrimSpace(summary)
	}

	emitStage("schedule_plan.final_check.done", "终审校验完成。")
	return st, nil
}

// physicsCheck 执行物理层面校验。
//
// 校验项：
// 1. 时间冲突：同一 slot 不允许多任务占用；
// 2. 节次越界：section 必须落在 1..12 且 from<=to；
// 3. 数量核对：suggested 数量应与原始 AllocatedItems 数量一致。
func physicsCheck(st *SchedulePlanState) []string {
	issues := make([]string, 0)
	if st == nil {
		return append(issues, "state 为空")
	}

	// 1. 时间冲突校验。
	if conflict := detectConflicts(st.HybridEntries); conflict != "" {
		issues = append(issues, "时间冲突："+conflict)
	}

	// 2. 节次越界校验。
	for _, entry := range st.HybridEntries {
		if entry.SectionFrom < 1 || entry.SectionTo > 12 || entry.SectionFrom > entry.SectionTo {
			issues = append(
				issues,
				fmt.Sprintf("节次越界：[%s] W%dD%d 第%d-%d节", entry.Name, entry.Week, entry.DayOfWeek, entry.SectionFrom, entry.SectionTo),
			)
		}
	}

	// 3. 数量一致性校验。
	// 3.1 判断依据：suggested 表示“待应用任务块”，应与 allocatedItems 数量匹配；
	// 3.2 若不匹配，可能表示工具调用丢失或重复覆盖。
	suggestedCount := countSuggested(st.HybridEntries)
	if suggestedCount != len(st.AllocatedItems) {
		issues = append(
			issues,
			fmt.Sprintf("任务数量不匹配：suggested=%d，原始分配=%d", suggestedCount, len(st.AllocatedItems)),
		)
	}

	return issues
}

// countSuggested 统计 suggested 条目数量。
func countSuggested(entries []model.HybridScheduleEntry) int {
	count := 0
	for _, entry := range entries {
		if entry.Status == "suggested" {
			count++
		}
	}
	return count
}

// generateHumanSummary 调用模型生成“用户可读”的总结文案。
//
// 职责边界：
// 1. 只做读模型，不修改任何 state；
// 2. 输出纯文本；
// 3. 失败时把错误返回给上层，由上层决定兜底文案。
func generateHumanSummary(
	ctx context.Context,
	chatModel *ark.ChatModel,
	entries []model.HybridScheduleEntry,
	constraints []string,
	actionLogs []string,
) (string, error) {
	if chatModel == nil {
		return "", fmt.Errorf("final summary model is nil")
	}
	entriesJSON, _ := json.Marshal(entries)
	constraintText := "无"
	if len(constraints) > 0 {
		constraintText = strings.Join(constraints, "、")
	}
	actionLogText := "无"
	if len(actionLogs) > 0 {
		// 1. 只取最后 30 条动作日志，避免上下文无限膨胀。
		// 2. 周级优化是“渐进式动作链”，取尾部更能体现最终收敛过程。
		// 3. 这里仅做展示收敛，不改原日志，保证调试信息完整保留在 state 中。
		start := 0
		if len(actionLogs) > 30 {
			start = len(actionLogs) - 30
		}
		actionLogText = strings.Join(actionLogs[start:], "\n")
	}

	userPrompt := fmt.Sprintf(
		"以下是最终排程方案（JSON）：\n%s\n\n用户约束：%s\n\n以下是本次周级优化动作日志（按时间顺序）：\n%s\n\n请基于“结果+过程”输出2-3句自然中文总结，重点说明本方案的优点和改进点。",
		string(entriesJSON),
		constraintText,
		actionLogText,
	)

	resp, err := chatModel.Generate(
		ctx,
		[]*schema.Message{
			schema.SystemMessage(SchedulePlanFinalCheckPrompt),
			schema.UserMessage(userPrompt),
		},
		ark.WithThinking(&arkModel.Thinking{Type: arkModel.ThinkingTypeDisabled}),
		einoModel.WithTemperature(0.4),
		einoModel.WithMaxTokens(256),
	)
	if err != nil {
		return "", err
	}
	if resp == nil {
		return "", fmt.Errorf("final summary response is nil")
	}
	return strings.TrimSpace(resp.Content), nil
}
