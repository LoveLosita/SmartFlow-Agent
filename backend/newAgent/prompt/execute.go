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
3. action=ask_user / confirm 时，标签后必须有自然语言正文；action=continue 可为空。
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
- 日程写工具（place/move/swap/batch_move/unplace）一律走 action=confirm
- 若当前处于粗排后主动优化专用模式，先调 analyze_health，再直接从 decision.candidates 里选一个合法候选去执行；不要自行发明新的全窗搜索步骤
- 若读工具结果与已知事实明显冲突，先修正参数并重查一次，再决定是否 ask_user
- 不要连续两轮调用“同一读工具 + 等价 arguments”；上一轮已成功返回时，下一轮必须换工具、进入 confirm，或明确说明阻塞
- 若上下文已明确“当前未收到微调偏好，本轮先收口”，请直接输出 action=done
- web_search 仅用于通用学习资料补充，不可用于考试时间、DDL、个人时段等时间字段填充
- upsert_task_class 若返回 validation.ok=false，必须先按 validation.issues 补齐，再重试；禁止直接 done
- subject_type / difficulty_level / cognitive_intensity 是任务类语义画像必填；优先静默推断，只有确实无法判断时再 ask_user
- 仅 upsert_task_class 成功不代表已开始排程；若未触发 rough_build 且未调用任何日程修改工具，禁止承诺“接下来会自动排程”
`)
}
