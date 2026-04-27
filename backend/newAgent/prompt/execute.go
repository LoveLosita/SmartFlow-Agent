package newagentprompt

import (
	"fmt"
	"strings"

	newagentmodel "github.com/LoveLosita/smartflow/backend/newAgent/model"
	"github.com/cloudwego/eino/schema"
)

// BuildExecuteSystemPrompt 返回执行阶段系统提示词（有 plan 模式）。
func BuildExecuteSystemPrompt() string {
	return buildExecutePromptWithFormatGuard(executeSystemPromptBaseWithPlan)
}

// BuildExecuteReActSystemPrompt 返回执行阶段系统提示词（自由执行模式）。
func BuildExecuteReActSystemPrompt() string {
	return buildExecutePromptWithFormatGuard(executeSystemPromptBaseReAct)
}

// BuildExecuteMessages 组装执行阶段消息。
func BuildExecuteMessages(state *newagentmodel.CommonState, ctx *newagentmodel.ConversationContext) []*schema.Message {
	if state != nil && state.HasPlan() {
		return buildExecuteStageMessages(
			BuildExecuteSystemPrompt(),
			state,
			ctx,
			buildExecuteStrictJSONUserPromptWithPlan(state),
		)
	}

	return buildExecuteStageMessages(
		BuildExecuteReActSystemPrompt(),
		state,
		ctx,
		buildExecuteStrictJSONUserPrompt(),
	)
}

// buildExecuteStrictJSONUserPromptWithPlan 在通用 JSON 约束上补充"当前计划步骤"强约束。
//
// 职责边界：
// 1. 负责把"当前是第几步、当前步骤内容、done_when 判定"明确写进用户指令；
// 2. 不负责替代系统提示词中的工具规则和安全边界；
// 3. 当 state 无法提供有效当前步骤时，仅追加兜底提示，不在此处推进流程状态。
func buildExecuteStrictJSONUserPromptWithPlan(state *newagentmodel.CommonState) string {
	base := buildExecuteStrictJSONUserPrompt()
	if state == nil || !state.HasPlan() {
		return base
	}

	current, total := state.PlanProgress()
	step, ok := state.CurrentPlanStep()
	if !ok {
		return strings.TrimSpace(base + `

计划步骤强约束：
- 当前没有可执行的计划步骤，请先基于已有事实检查是否已完成全部计划。
- 若全部计划已完成：输出 action=done，并在 goal_check 总结完成证据。
- goal_check 字段类型必须为 string，不要输出对象或数组。
- 若未完成但缺少关键信息：输出 action=ask_user。`)
	}

	stepContent := strings.TrimSpace(step.Content)
	if stepContent == "" {
		stepContent = "（当前步骤内容为空，以 done_when 为准）"
	}
	doneWhen := strings.TrimSpace(step.DoneWhen)
	if doneWhen == "" {
		doneWhen = "（未提供 done_when，需基于当前步骤目标给出可验证完成证据）"
	}

	return strings.TrimSpace(fmt.Sprintf(`%s

计划步骤强约束：
- 你当前只允许推进第 %d/%d 步。
- 当前步骤内容：%s
- 当前步骤完成判定(done_when)：%s
- 未满足 done_when 时：只能输出 continue / confirm / ask_user，禁止输出 next_plan。
- 满足 done_when 时：优先输出 action=next_plan，并在 goal_check 逐条对照 done_when 给出证据。
- goal_check 字段类型固定为 string（示例："已满足 done_when：...；证据：..."），禁止输出 {"done_when":"...","evidence":"..."}。
- 禁止跳步：不要提前执行后续步骤。`,
		base, current, total, stepContent, doneWhen))
}

// buildExecutePromptWithFormatGuard 统一补一层更硬的 JSON 输出约束。
func buildExecutePromptWithFormatGuard(base string) string {
	base = strings.TrimSpace(base)
	guard := strings.TrimSpace(`
输出协议硬约束：
1. 只输出当前 action 真正需要的字段；不要输出空字符串、空对象、空数组或 null 占位。
2. tool_call 只能是 {"name":"工具名","arguments":{...}}；不能写 parameters，也不能一次输出多个 tool_call。
3. action=ask_user / confirm 时，标签后必须有自然语言正文；action=continue 可为空，但只允许配合读工具或纯思考，不能携带任何写工具。
4. action=done 时不要携带 tool_call；action=next_plan / done 时，goal_check 必须是字符串。
5. 只有 action=abort 时才允许输出 abort 字段。
6. <SMARTFLOW_DECISION> 标签内只放 JSON，不要放自然语言。
7. 不要在 <SMARTFLOW_DECISION> 标签前输出任何前言、寒暄、解释或铺垫；给用户看的正文只能放在 </SMARTFLOW_DECISION> 之后。
8. 任何动作都不得擅自超出用户当前明确意图；用户没让你做的下一步，不要自作主张推进。`)
	if base == "" {
		return guard
	}
	return base + "\n\n" + guard
}

// buildExecuteStrictJSONUserPrompt 统一构造 execute 阶段面向模型的最终用户指令。
func buildExecuteStrictJSONUserPrompt() string {
	return strings.TrimSpace(`
请继续当前任务的执行阶段，严格按 SMARTFLOW_DECISION 标签格式输出。
输出格式：先输出 <SMARTFLOW_DECISION>{JSON 决策}</SMARTFLOW_DECISION>，然后换行输出给用户看的正文。

执行提醒：
- JSON 中不要包含 speak 字段；给用户看的话放在 </SMARTFLOW_DECISION> 标签之后
- 不要在 <SMARTFLOW_DECISION> 标签之前输出任何文字；哪怕只有一句“我先看下”也不行
- 任何写工具都一律走 action=confirm，包括 upsert_task_class 与日程写工具（place/move/swap/batch_move/unplace）；哪怕只是“按 validation.issues 重试一次”，也不能输出 continue + 写工具
- 若当前处于粗排后主动优化专用模式，先调 analyze_health，再直接从 decision.candidates 里选一个合法候选去执行；不要自行发明新的全窗搜索步骤
- 若读工具结果与已知事实明显冲突，先修正参数并重查一次，再决定是否 ask_user
- 不要连续两轮调用“同一读工具 + 等价 arguments”；上一轮已成功返回时，下一轮必须换工具、进入 confirm，或明确说明阻塞
- 若上下文已明确“当前未收到微调偏好，本轮先收口”，请直接输出 action=done
- web_search 仅用于通用学习资料补充，不可用于考试时间、DDL、个人时段等时间字段填充
- 任何写工具在真正输出 action=confirm 前，都必须先做一次“写前检查”：确认参数已齐全、格式合法、业务前提已满足；若尚未通过检查，就先补齐/归一/生成/ask_user，不要把 validation 失败当成正常探索路径
- upsert_task_class 若返回 validation.ok=false，必须先按 validation.issues 补齐，再重试；禁止直接 done
- 对 upsert_task_class，写前至少检查：mode=auto 时日期边界是否已满足；subject_type / difficulty_level / cognitive_intensity 是否齐；difficulty_level 是否已映射到合法枚举；items 是否非空且顺序内容已生成；config 中已知约束字段是否已落到合法格式
- 若像 items 这种内容本就由当前轮模型负责生成，就应先把内容生成齐、顺序排好，再写入；不要先写一个 items 为空的 taskclass 去让 validation 提醒你补内容
- 处理 validation.issues 时先分类：若是用户关键信息确实缺失，才 action=ask_user；若是 schema 字段名、字段位置、内部索引、枚举值、日期格式、工具语义映射等内部表示问题，应静默改参后直接重试，不要把底层表示教学抛给用户
- 像 config.excluded_slots 的半天块索引映射，默认属于内部表示修正：你应自己把“第1-2节 / 第11-12节”换算成合法块索引，不要为此 ask_user，不要长篇解释底层表示
- 当前时间锚点只用于解析用户已经明确说出的相对时间（如“今天开始”“两周内”“下周一前”），不能反过来把“现在是今天”当成用户已经同意从今天开始，更不能据此默认生成 start_date / end_date
- 像 auto 模式缺 start_date/end_date 这类问题，先检查当前对话、历史、记忆、已知工具结果里是否已经出现可用日期；若已出现就静默补齐并重试，只有在上下文里确实没有时再 ask_user
- 若当前是首次创建/修正 taskclass，且上下文里并没有用户明确给出的开始日期、结束日期、日期范围、完成期限、或可直接换算出的相对时间承诺，就不要擅自写 start_date / end_date；此时若工具闭环确实要求这些字段，必须 ask_user
- 对 taskclass 来说，以下属于必须 ask_user 的关键信息：start_date、end_date、明确的日期范围、明确的开始时间承诺、明确的完成期限；这些会决定任务类的真实时间边界，不能由模型自行拍板
- subject_type / difficulty_level / cognitive_intensity 是任务类语义画像必填；优先静默推断，只有确实无法判断时再 ask_user
- 学习目标、总节数、难度、节次偏好、禁排时段、排除星期、内容拆分授权，这些默认都只是 taskclass 语义；不要因为信息完整就自动切进 schedule
- 只有用户明确要求“排进日程 / 给出具体时间安排 / 现在就排一版”时，才允许 context_tools_add domain="schedule"、触发 rough_build，或继续 schedule 链路
- 例：“我要复习离散数学，基础较差，大概学 8 节课，不要早上第 1-2 节和晚上第 11-12 节，周末也不想学，每节课内容你自己来”——应先生成或更新 taskclass，而不是主动排进日程
- 仅 upsert_task_class 成功不代表已开始排程；若未触发 rough_build 且未调用任何日程修改工具，禁止承诺“接下来会自动排程”
- 当前轮目标若是创建/修正 taskclass，就优先把 taskclass 静默闭环；除非真缺用户关键信息，否则不要把主要篇幅花在解释工具内部约束上
`)
}
