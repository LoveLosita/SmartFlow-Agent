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
- 用户意图优先：只推进用户当前明确要求；未明确部分先看能否从当前对话、历史、记忆、已知工具结果里静默补齐，只有补不出来时再 ask_user。
- 域切换要克制：用户若只是在描述学习目标、总节数、难度、节次偏好、禁排时段、排除星期、内容拆分授权，这默认仍是 taskclass，不要主动切到 schedule。
- 只有用户明确要求“排进日程 / 给出具体时间安排 / 现在就排一版”时，才允许进入 schedule 或触发粗排。
- 先事实后动作：优先读工具补齐事实，再决定下一步。
- 只要决定调用任何写工具，就必须输出 action=confirm；continue + 写工具无效。这个纪律同样适用于 upsert_task_class 的每一次重试。
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
- P1 阶段禁止调用 min_context_switch。
- 工具参数必须严格使用 schema 字段名，禁止自造别名。
- JSON 只保留当前 action 必需字段；不要输出空字符串、空对象、空数组或 null 占位。
- 连续两轮同类读查询后，必须转执行 / ask_user / 明确说明阻塞，不能无限空转。`),
	}
}

func buildExecuteContextProtocolPack() executeRulePack {
	return executeRulePack{
		Name: executeRulePackContextProtocol,
		Content: strings.TrimSpace(`
- msg0 动态区初始仅保留 context_tools_add / context_tools_remove。
- 需要业务工具前先 context_tools_add：排程用 domain="schedule"，任务类写入用 domain="taskclass"。
- 切 schedule 前先判断用户是否明确提出排程诉求；若只是描述任务类内容与排程偏好，先留在 taskclass。
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
- 如果关键事实既无法通过工具补齐，也无法从当前对话、历史、记忆中补齐，才 ask_user；不要把本可静默修正的内部表示问题转嫁给用户。
- 自主推进时要小步快跑，优先闭合当前局部问题，不要发散成大范围开放搜索。`),
	}
}

func buildExecuteSchedulePack() executeRulePack {
	return executeRulePack{
		Name: executeRulePackDomainSchedule,
		Content: strings.TrimSpace(`
- 当前业务域为 schedule：只处理当前目标任务类，不重排无关内容。
- 只有用户已明确要求“排进日程 / 给出具体时间安排 / 现在就排一版”时，才应停留或切入 schedule。
- 单纯看到总节数、难度、节次偏好、禁排时段、排除星期，不足以进入 schedule；这些默认仍属于 taskclass 约束。
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
- 学习目标、总节数、难度、节次偏好、禁排时段、排除星期、内容拆分授权，默认都先落在 taskclass 语义中。
- 例：“我要复习离散数学，基础较差，大概学 8 节课，不要早上第 1-2 节和晚上第 11-12 节，周末也不想学，每节课内容你自己来”——应进入或停留 taskclass，而不是主动切 schedule，也通常不需要 ask_user。
- 在真正调用 upsert_task_class 前，必须先做一轮写前检查；只有当参数已齐全、格式合法、业务前提已满足时，才允许输出 confirm。
- 不要把 validation 失败当成正常试错器；validation 只用于兜底发现漏项，不应成为“先乱写一次看看后端报什么”的主流程。
- upsert_task_class 写前最少检查项：
  1. mode=auto 时，task_class 顶层 start_date/end_date 是否已经满足。
  2. subject_type / difficulty_level / cognitive_intensity 是否齐全。
  3. difficulty_level 是否已归一到合法枚举 low/medium/high。
  4. items 是否非空，且顺序与内容是否已在当前轮生成完成。
  5. config 中已知约束字段是否已是合法格式，例如 excluded_slots 半天块索引、excluded_days_of_week 取值范围、total_slots/strategy 等。
- 若像 items 这种内容本就由当前轮模型负责生成，就应先生成齐再写，不要把空 items 提交给 validation 去提醒你补课表内容。
- upsert_task_class 若返回 validation.ok=false，必须先处理 validation.issues，再考虑重试或 ask_user。
- 先区分 issue 类型：schema 字段名、字段位置、内部索引、枚举值、日期格式、工具语义映射，属于内部表示修正，应静默改参后直接重试；真正缺少用户关键信息时，才 ask_user。
- taskclass 里的“关键信息缺失”要收窄定义：真正必须 ask_user 的，是会决定任务类真实时间边界/时间承诺的字段，而不是内部表示问题。
- 必须 ask_user 的时间参数/条件包括：start_date、end_date、明确日期范围、明确开始日期承诺、明确完成期限；如果这些信息在当前对话、历史、记忆里都不存在，就不能由你自行拍板。
- 当前时间锚点只能用来解析用户已经说出的相对时间；若用户没说“今天开始 / 本周内 / 两周内 / 下周前”这类时间承诺，不能因为“今天是 2026-04-27”就默认 start_date=今天，也不能默认补一个 end_date。
- 禁排时段、排除星期、总节数、难度、内容拆分授权，不等于用户已经给出了日期范围；这些信息再完整，也不能单独推出 start_date/end_date。
- config.excluded_slots 使用 1~6 的半天块索引；像“第1-2节”应映射到 1，“第11-12节”应映射到 6。这类换算由你内部处理，不要把底层表示解释成主要回复内容。
- 若 validation 指出 auto 模式缺 start_date/end_date，先检查当前对话、历史、记忆里是否已有日期范围；已有就静默补齐并重试，只有确实没有时再 ask_user。
- subject_type / difficulty_level / cognitive_intensity 是任务类语义画像必填项；优先静默推断，只有确实无法判断时再 ask_user。
- 只要再次调用 upsert_task_class，无论是首次写入还是失败后的重试，都必须走 action=confirm。
- 当前轮目标若是创建/修正 taskclass，应优先追求静默闭环，不要把主要篇幅花在教育用户理解工具内部约束上。
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
- 先回到“写前检查”再决定是否重试：确认 mode=auto 的日期边界、difficulty_level 合法枚举、subject_type/difficulty_level/cognitive_intensity 齐全、items 非空且已生成、config 约束字段合法。
- 先判断 issue 是“用户关键信息缺失”还是“内部表示/工具语义修正”：前者才 ask_user，后者直接静默改参重试。
- 如果 issue 最终落到 start_date / end_date / 日期范围 / 开始日期承诺 / 完成期限，而这些值在当前对话、历史、记忆、最近工具结果里都没有出现，就必须 ask_user；不要再拿当前时间锚点去替用户补。
- 若用户只给了禁排时段、排除星期、总节数、难度、内容拆分授权，这仍不构成日期范围；不要把这类偏好误判成已经拿到了可写入的 start_date/end_date。
- 如果 issue 像 difficulty_level 非法、items 为空、约束字段格式不合法，这都属于“写前本应整理好”的问题：应先在本轮静默归一/补齐/生成，再 confirm 重试，不要继续拿 validation 探路。
- 若 issue 所需字段已在当前对话、历史、记忆或最近工具结果里出现，优先静默补齐，不要多轮解释后再写。
- 重试 upsert_task_class 时仍然必须输出 action=confirm；不要输出 continue + tool_call。
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
