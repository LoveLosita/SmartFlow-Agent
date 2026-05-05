package agentprompt

import (
	"fmt"
	"strings"

	agentmodel "github.com/LoveLosita/smartflow/backend/services/agent/model"
	agenttools "github.com/LoveLosita/smartflow/backend/services/agent/tools"
)

// renderExecuteNextStepHintV2 生成 execute.msg3 的轻量方向提示。
//
// 设计目标：
// 1. 主动优化模式下，只强调“先 analyze_health，再从 candidates 里选”，不再散发额外搜索暗示。
// 2. 普通链路仍保留必要的业务引导，避免误伤用户明确提出的普通调整请求。
// 3. 提示只给方向，不替模型代填最终写参数。
func renderExecuteNextStepHintV2(
	state *agentmodel.CommonState,
	latestAnalyze string,
	latestMutation string,
	roughBuildDone bool,
) string {
	if state == nil {
		return ""
	}

	activeDomain := strings.TrimSpace(state.ActiveToolDomain)
	activePacks := agenttools.ResolveEffectiveToolPacks(state.ActiveToolDomain, state.ActiveToolPacks)

	if state.ActiveOptimizeOnly {
		switch {
		case activeDomain == "" && roughBuildDone:
			return "当前是粗排后主动优化专用模式；先激活 schedule，并只围绕 analyze_health -> move/swap 候选闭环推进。"
		case !state.HealthCheckDone:
			return "当前是粗排后主动优化专用模式；先调 analyze_health，等待后端给出 candidates，再做选择。"
		case !state.HealthIsFeasible || strings.EqualFold(strings.TrimSpace(state.HealthRecommendedOperation), "ask_user"):
			return "analyze_health 已判定当前更像时间窗或信息约束问题；不要继续挪动，先把冲突或缺失点明确告诉用户。"
		case !state.HealthShouldContinueOptimize:
			return "analyze_health 已判定当前无需继续主动优化；若用户没有新增要求，直接收口。"
		default:
			return "当前是粗排后主动优化专用模式；直接从 analyze_health 的 decision.candidates 里选一个合法 move/swap 执行，不要再自己搜索读工具。"
		}
	}

	if activeDomain == "schedule" && state.HealthCheckDone {
		switch {
		case !state.HealthShouldContinueOptimize && state.HealthIsForcedImperfection:
			return fmt.Sprintf(
				"analyze_health 已判定当前更像约束代价：tightness=%s，主问题=%s。优先考虑收口。",
				fallbackExecuteText(state.HealthTightnessLevel, "unknown"),
				fallbackExecuteText(state.HealthPrimaryProblem, "无"),
			)
		case !state.HealthShouldContinueOptimize:
			return fmt.Sprintf(
				"analyze_health 已判定当前没有更值得继续处理的局部问题：%s。若用户未追加新要求，优先收口。",
				fallbackExecuteText(state.HealthPrimaryProblem, "当前可直接收口"),
			)
		case state.HealthStagnationCount > 0:
			return fmt.Sprintf(
				"最近诊断已连续 %d 次无明显改善；若本轮仍不能让主问题变轻，优先收口。当前主问题：%s。",
				state.HealthStagnationCount,
				fallbackExecuteText(state.HealthPrimaryProblem, "无"),
			)
		case strings.EqualFold(strings.TrimSpace(state.HealthRecommendedOperation), "swap"):
			return fmt.Sprintf(
				"当前主问题：%s。优先在已有落位之间做局部 swap，别把问题扩散到更远的天数。",
				fallbackExecuteText(state.HealthPrimaryProblem, "无"),
			)
		case strings.EqualFold(strings.TrimSpace(state.HealthRecommendedOperation), "move"):
			return fmt.Sprintf(
				"当前主问题：%s。若要 move，只在近范围合法落点里小修，不要做全窗口搜索。",
				fallbackExecuteText(state.HealthPrimaryProblem, "无"),
			)
		}
	}

	if activeDomain == "" {
		if roughBuildDone {
			return `先激活 schedule 业务域；当前是粗排后的微调场景，通常至少需要 mutation+analyze。若要按统一条件逐个处理一批任务，再加 packs=["queue"]。`
		}
		return `先判断当前任务属于哪个业务域，再用 context_tools_add 激活对应工具。若用户只是在描述学习目标、总节数、难度、节次偏好、禁排时段、排除星期、内容拆分授权，默认先走 taskclass；只有用户明确要求“排进日程 / 给出具体时间安排 / 现在就排一版”时，才切 schedule。`
	}

	if activeDomain == "schedule" &&
		strings.Contains(latestMutation, "batch_move") &&
		(strings.Contains(latestMutation, "缺少") || strings.Contains(latestMutation, "无效")) {
		return `当前 batch_move 路径受参数约束；若要处理一批符合同一条件的任务，优先加 packs=["queue"] 逐个处理。`
	}

	if activeDomain == "schedule" &&
		latestAnalyze != "" &&
		strings.Contains(latestAnalyze, "metrics") &&
		!containsExecutePack(activePacks, agenttools.ToolPackQueue) {
		return `若诊断已经完成，下一步应转入读事实或写操作，不要重复 analyze_health；涉及同类批量任务时优先考虑 packs=["queue"]。`
	}

	if activeDomain == "taskclass" &&
		state.TaskClassUpsertLastTried &&
		!state.TaskClassUpsertLastSuccess {
		return `先判断 validation.issues 是“用户缺信息”还是“内部表示修正”；能从上下文补的先静默补齐，再用 confirm 重试 upsert_task_class，不要继续解释底层约束，更不要直接收口。`
	}

	return ""
}
