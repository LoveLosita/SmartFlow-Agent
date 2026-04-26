package newagentprompt

import (
	"fmt"
	"strings"
	"time"

	newagentmodel "github.com/LoveLosita/smartflow/backend/newAgent/model"
	newagenttools "github.com/LoveLosita/smartflow/backend/newAgent/tools"
)

const (
	executeRulePackCoreMin          = "core_min"
	executeRulePackSafetyHard       = "safety_hard"
	executeRulePackContextProtocol  = "context_protocol"
	executeRulePackModePlan         = "mode_plan"
	executeRulePackModeReAct        = "mode_react"
	executeRulePackDomainSchedule   = "domain_schedule"
	executeRulePackDomainTaskClass  = "domain_taskclass"
	executeRulePackScheduleMutation = "schedule_mutation"
	executeRulePackScheduleAnalyze  = "schedule_analyze"
	executeRulePackScheduleWeb      = "schedule_web"
	executeRulePackMicroRoughDone   = "micro_rough_build_done"
	executeRulePackMicroDiagLoop    = "micro_diag_tune_loop"
	executeRulePackMicroQueue       = "micro_queue_chain"
	executeRulePackMicroTaskRetry   = "micro_taskclass_retry"
)

const executeSystemPromptBaseWithPlan = `
你叫 SmartMate，是时伴（SmartMate）的中文 AI 排程伙伴，面向大学生提供陪伴式日程管理与日常协助。
你擅长课表与任务安排、任务管理、学习规划和随口记，也可以正常回答日常问答、生活建议、信息整理、分析讨论等非排程问题。
你的目标是像一个越用越懂用户的伙伴一样，结合历史对话、长期记忆和当前上下文，给出贴心、清晰、可信的帮助。
你当前处于“计划执行”模式。你必须围绕当前计划步骤推进，并通过 SMARTFLOW_DECISION 输出结构化动作。`

const executeSystemPromptBaseReAct = `
你叫 SmartMate，是时伴（SmartMate）的中文 AI 排程伙伴，面向大学生提供陪伴式日程管理与日常协助。
你擅长课表与任务安排、任务管理、学习规划和随口记，也可以正常回答日常问答、生活建议、信息整理、分析讨论等非排程问题。
你的目标是像一个越用越懂用户的伙伴一样，结合历史对话、长期记忆和当前上下文，给出贴心、清晰、可信的帮助。
你当前处于“自由执行（ReAct）”模式。你需要根据当前目标自主推进、按需调用工具，并通过 SMARTFLOW_DECISION 输出结构化动作。`

type executeRulePack struct {
	Name    string
	Content string
}

// renderExecuteRulePackSection 渲染 execute.msg0 的动态规则包区域。
//
// 1. 这里负责“选哪些包 + 以什么顺序展示”，不负责工具目录本身。
// 2. 固定先放通用硬约束，再放 mode/domain/micro 包，保证模型先读边界后读特例。
// 3. 如果没有任何可展示规则包，则直接返回空串，避免无意义占位。
func renderExecuteRulePackSection(state *newagentmodel.CommonState, ctx *newagentmodel.ConversationContext) (string, []string) {
	packs := selectExecuteRulePacks(state, ctx)
	if len(packs) == 0 {
		return "", nil
	}

	lines := []string{"执行规则包（msg0 动态注入）:"}
	names := make([]string, 0, len(packs))
	for _, pack := range packs {
		content := strings.TrimSpace(pack.Content)
		if content == "" {
			continue
		}
		lines = append(lines, fmt.Sprintf("[%s]", pack.Name))
		lines = append(lines, content)
		names = append(names, pack.Name)
	}
	if len(names) == 0 {
		return "", nil
	}
	return strings.Join(lines, "\n"), names
}

func selectExecuteRulePacks(state *newagentmodel.CommonState, ctx *newagentmodel.ConversationContext) []executeRulePack {
	selected := make([]executeRulePack, 0, 8)
	seen := map[string]bool{}

	appendPack := func(pack executeRulePack) {
		name := strings.TrimSpace(pack.Name)
		if name == "" || seen[name] {
			return
		}
		seen[name] = true
		selected = append(selected, pack)
	}

	appendPack(buildExecuteCoreMinPack())
	appendPack(buildExecuteSafetyHardPack())
	appendPack(buildExecuteContextProtocolPack())

	if state != nil && state.HasPlan() {
		appendPack(buildExecuteModePlanPack())
	} else {
		appendPack(buildExecuteModeReActPack())
	}

	switch normalizeExecuteToolDomain(readExecuteActiveToolDomain(state)) {
	case "schedule":
		activePacks := readExecuteActiveToolPacks(state)
		appendPack(buildExecuteSchedulePack())
		if hasExecutePack(activePacks, newagenttools.ToolPackQueue) {
			appendPack(buildExecuteQueueMicroPack())
		}
		if hasExecutePack(activePacks, newagenttools.ToolPackMutation) {
			appendPack(buildExecuteScheduleMutationPack())
		}
		if hasExecutePack(activePacks, newagenttools.ToolPackAnalyze) {
			appendPack(buildExecuteScheduleAnalyzePackV2())
		}
		if hasExecutePack(activePacks, newagenttools.ToolPackWeb) {
			appendPack(buildExecuteScheduleWebPack())
		}
	case "taskclass":
		appendPack(buildExecuteTaskClassPack())
	}

	if hasExecuteRoughBuildDone(ctx) {
		appendPack(buildExecuteRoughDoneMicroPack())
	}
	if shouldInjectExecuteDiagLoopPack(state, ctx) {
		appendPack(buildExecuteDiagLoopMicroPackV2())
	}
	if state != nil && state.TaskClassUpsertLastTried && !state.TaskClassUpsertLastSuccess {
		appendPack(buildExecuteTaskClassRetryMicroPack())
	}

	return selected
}

func readExecuteActiveToolDomain(state *newagentmodel.CommonState) string {
	if state == nil {
		return ""
	}
	return strings.TrimSpace(state.ActiveToolDomain)
}

func readExecuteActiveToolPacks(state *newagentmodel.CommonState) []string {
	if state == nil {
		return nil
	}
	return newagenttools.ResolveEffectiveToolPacks(state.ActiveToolDomain, state.ActiveToolPacks)
}

func hasExecutePack(packs []string, target string) bool {
	target = strings.ToLower(strings.TrimSpace(target))
	if target == "" {
		return false
	}
	for _, pack := range packs {
		if strings.ToLower(strings.TrimSpace(pack)) == target {
			return true
		}
	}
	return false
}

// containsExecutePack 兼容旧调用点。
//
// 1. 这里只做别名转发，不引入第二套判断口径。
// 2. 保留它是为了避免下一轮再因为历史调用点而误删。
func containsExecutePack(packs []string, target string) bool {
	return hasExecutePack(packs, target)
}

func normalizeExecuteToolDomain(domain string) string {
	switch strings.ToLower(strings.TrimSpace(domain)) {
	case "schedule":
		return "schedule"
	case "taskclass":
		return "taskclass"
	default:
		return ""
	}
}

func buildExecuteCoreMinPack() executeRulePack {
	return executeRulePack{
		Name: executeRulePackCoreMin,
		Content: strings.TrimSpace(fmt.Sprintf(`
- 当前时间锚点：%s。涉及“今天/明天/本周”等相对时间时，先按该锚点换算。
- 用户意图优先：只推进用户当前明确要求；未明确部分优先 ask_user。
- 先事实后动作：优先读工具补齐事实，再决定下一步。
- 只要决定调用 place/move/swap/batch_move/unplace 这类写工具，就必须输出 action=confirm；continue + 写工具无效。
- 输出格式固定：先 <SMARTFLOW_DECISION>{JSON}</SMARTFLOW_DECISION>，再输出用户可见正文。`,
			buildExecuteNowAnchorLine())),
	}
}

func buildExecuteNowAnchorLine() string {
	now := time.Now()
	weekdays := []string{"周日", "周一", "周二", "周三", "周四", "周五", "周六"}
	return fmt.Sprintf("%s（%s，%s）", now.Format("2006-01-02 15:04:05 -07:00"), weekdays[int(now.Weekday())], now.Format("MST"))
}

func buildExecuteSafetyHardPack() executeRulePack {
	return executeRulePack{
		Name: executeRulePackSafetyHard,
		Content: strings.TrimSpace(`
- 严禁伪造工具结果；若新结果与既有事实冲突，先重查一次再决定。
- 工具参数必须严格使用 schema 字段名，禁止自造别名。
- JSON 只保留当前 action 必需字段；不要输出空字符串、空对象、空数组或 null 占位。
- P1 阶段禁止调用 min_context_switch。
- 连续两轮同类读查询后，必须转执行 / ask_user / 明确说明阻塞，不能无限空转。`),
	}
}

func buildExecuteContextProtocolPack() executeRulePack {
	return executeRulePack{
		Name: executeRulePackContextProtocol,
		Content: strings.TrimSpace(`
- msg0 动态区初始仅保留 context_tools_add / context_tools_remove。
- 需要业务工具前先 context_tools_add：排程用 domain="schedule"，任务类写入用 domain="taskclass"。
- schedule 可选 packs=["mutation","analyze","detail_read","deep_analyze","queue","web"]；core 固定注入，不要显式传 core。
- 只在业务方向切换时再 remove；done 后的动态区清理由系统自动完成，不必手动 remove。
- 如果目标工具当前不在可用列表，先 add 对应 domain / packs，再继续执行。`),
	}
}

func buildExecuteModePlanPack() executeRulePack {
	return executeRulePack{
		Name: executeRulePackModePlan,
		Content: strings.TrimSpace(`
- 当前为计划执行模式：必须围绕当前计划步骤推进。
- 未满足 done_when 时，只能 continue / confirm / ask_user，禁止 next_plan。
- next_plan / done 时，goal_check 必须是字符串，并对照 done_when 给出完成证据。
- 禁止跳步执行后续计划。`),
	}
}

func buildExecuteModeReActPack() executeRulePack {
	return executeRulePack{
		Name: executeRulePackModeReAct,
		Content: strings.TrimSpace(`
- 当前为自由执行（ReAct）模式：可自主决定 continue / confirm / ask_user / done / abort。
- 如果关键事实无法通过工具补齐，优先 ask_user，不做猜测落库。
- 自主推进时要小步快跑，优先闭合当前局部问题，不要发散成大范围开放搜索。`),
	}
}

func buildExecuteSchedulePack() executeRulePack {
	return executeRulePack{
		Name: executeRulePackDomainSchedule,
		Content: strings.TrimSpace(`
- 当前业务域为 schedule：只处理当前目标任务类，不重排无关内容。
- existing 只作事实参考；真正可调对象优先看 suggested。
- 同任务类内部顺序必须保持，任何越过前驱/后继边界的移动都会被写工具拒绝。`),
	}
}

func buildExecuteScheduleMutationPack() executeRulePack {
	return executeRulePack{
		Name: executeRulePackScheduleMutation,
		Content: strings.TrimSpace(`
- mutation 包负责真正落日程写操作：place / move / swap / batch_move / unplace。
- 写操作必须走 action=confirm；不要在 continue 里偷跑写工具。
- 若是主动优化链路，优先在后端给出的合法候选中选择，不要自己再全窗搜索新坑位。`),
	}
}

func buildExecuteQueueMicroPack() executeRulePack {
	return executeRulePack{
		Name: executeRulePackMicroQueue,
		Content: strings.TrimSpace(`
- queue 包适合“按同一条件逐个处理一批任务”的场景，例如把所有早八任务依次挪走。
- query_target_tasks 可结合 enqueue=true 先把候选任务入队，再用 queue_pop_head / queue_apply_head_move / queue_skip_head 顺序处理。
- 当你需要连续处理多条相似任务时，优先走 queue，避免把整批任务细节长期堆在上下文里。`),
	}
}

func buildExecuteScheduleWebPack() executeRulePack {
	return executeRulePack{
		Name: executeRulePackScheduleWeb,
		Content: strings.TrimSpace(`
- web 包只用于补充通用学习资料或通识信息，不用于捏造个人时间、考试时间、DDL 或排程事实。
- web_search 先粗搜，web_fetch 再抓正文；不确定时宁可不用，也不要把网页结果当成排程事实直接写入。`),
	}
}

func buildExecuteTaskClassPack() executeRulePack {
	return executeRulePack{
		Name: executeRulePackDomainTaskClass,
		Content: strings.TrimSpace(`
- taskclass 域只负责生成或修正任务类，不代表已经开始排程。
- upsert_task_class 若返回 validation.ok=false，必须先处理 validation.issues，再考虑重试或 ask_user。
- subject_type / difficulty_level / cognitive_intensity 是任务类语义画像必填项；优先静默推断，只有确实无法判断时再 ask_user。
- excluded_slots 取值应与系统节次定义一致；excluded_days_of_week 使用 1~7 表示周一到周日。`),
	}
}

func buildExecuteRoughDoneMicroPack() executeRulePack {
	return executeRulePack{
		Name: executeRulePackMicroRoughDone,
		Content: strings.TrimSpace(`
- 已有 rough_build_done：本轮以微调为主，不要把任务重新当成“未排入”再全量 place。
- 若当前问题已经可接受，应优先收口，不要为了追求完美继续反复局部打磨。`),
	}
}

func buildExecuteTaskClassRetryMicroPack() executeRulePack {
	return executeRulePack{
		Name: executeRulePackMicroTaskRetry,
		Content: strings.TrimSpace(`
- 最近一次 upsert_task_class 失败时，优先围绕 validation.issues 修补。
- 问题未解决前，不要用 done 假装收口；要么重试，要么 ask_user 补关键信息。`),
	}
}

func shouldInjectExecuteDiagLoopPack(state *newagentmodel.CommonState, ctx *newagentmodel.ConversationContext) bool {
	if state == nil || !hasExecuteRoughBuildDone(ctx) {
		return false
	}
	if normalizeExecuteToolDomain(readExecuteActiveToolDomain(state)) != "schedule" {
		return false
	}
	activePacks := readExecuteActiveToolPacks(state)
	return hasExecutePack(activePacks, newagenttools.ToolPackAnalyze) &&
		hasExecutePack(activePacks, newagenttools.ToolPackMutation)
}
