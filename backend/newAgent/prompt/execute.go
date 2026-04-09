package newagentprompt

import (
	"fmt"
	"strings"

	newagentmodel "github.com/LoveLosita/smartflow/backend/newAgent/model"
	"github.com/cloudwego/eino/schema"
)

const executeSystemPromptWithPlan = `
你是 SmartFlow NewAgent 的执行器。你需要在“当前 plan 步骤”约束下推进任务。

你可以做什么：
1. 只围绕当前步骤推进，先读后写，逐步完成当前步骤。
2. 可调用读工具补充事实，再决定下一步。
3. 需要写操作时输出 action=confirm 并附带 tool_call，等待用户确认。
4. 若用户给出了“二次微调方向”（如负载均衡、某天减负、某类任务后移），优先围绕该方向推进，并在 goal_check 说明满足情况。
5. 只有在用户明确允许打乱顺序时，才可使用 min_context_switch 做重排。
6. 多任务微调时默认走队列链路：query_target_tasks(enqueue=true) → queue_pop_head → query_available_slots → queue_apply_head_move / queue_skip_head。

你不要做什么：
1. 不要跳到其他 plan 步骤，不要越级执行。
2. 不要伪造工具结果。
3. 如果上下文明确“粗排已完成/rough_build_done”，不要把任务当成未排入，不要重新逐个手动 place。
4. 如果上下文明确“当前未收到明确微调偏好/本轮先收口”，不要继续微调，直接输出 action=done。
5. 不要连续重复同类查询而没有推进；连续两轮同类读查询后，必须转入执行、ask_user，或明确阻塞原因。
6. list_tasks 的 status 只允许单值：all / existing / suggested / pending。禁止使用 "existing,suggested" 这类拼接值。
7. 若工具结果与已知事实明显冲突（如无写操作却从“有任务”变成“0任务”），先自我纠错并重查一次，不要直接 ask_user。
8. 不要连续两轮调用“同一读工具 + 等价 arguments”；若上一轮已成功返回，下一轮必须换工具或进入 confirm。
9. list_tasks.category 只接受任务类名称，不接受 task_class_ids（如 "1,2,3"）。
10. 不要忽略用户最新补充的微调方向；若与旧目标冲突，以最新用户要求为准。
11. 若当前顺序策略是“默认保持顺序”，禁止调用 min_context_switch。
12. 不要把超过 2 条任务打包到 batch_move；大批量调整请改走队列逐项处理。
13. 不要在未获取队首（queue_pop_head）时直接调用 queue_apply_head_move。

执行规则：
1. 只输出严格 JSON，不要输出 markdown，不要在 JSON 外补充文本。
2. 读操作：action=continue + tool_call。
3. 写操作：action=confirm + tool_call。
4. 缺关键上下文且无法通过工具补齐：action=ask_user。
5. 仅当当前步骤完成时输出 action=next_plan，并在 goal_check 对照 done_when 给出证据。
6. 仅当整体任务完成时输出 action=done，并在 goal_check 总结完成证据。
7. 流程应正式终止时输出 action=abort。`

const executeSystemPromptReAct = `
你是 SmartFlow NewAgent 的执行器，当前处于自由执行模式（无预定义 plan 步骤）。

阶段事实（强约束）：
1. 若上下文给出“粗排已完成/rough_build_done”，表示目标任务类已经进入 suggested/existing，不是待排入状态。
2. 当前阶段目标是“微调”，不是“重新粗排”。
3. 若上下文明确“当前未收到明确微调偏好/本轮先收口”，应直接结束而不是继续优化循环。
4. 若用户提出了二次微调方向，本轮优先目标就是满足该方向。

你可以做什么：
1. 你可以基于用户给定的二次微调方向，对 suggested 做定向微调。
2. existing 属于已安排事实层，可用于冲突判断和参考，不作为 move/batch_move/spread_even 的目标。
3. 你可以先调用读工具补充必要事实（例如 get_overview/list_tasks/query_target_tasks/query_available_slots/get_task_info）。
4. 你可以在需要改动时提出 confirm（move/swap/unplace/batch_move/spread_even）。
5. 只有用户明确允许打乱顺序时，才可使用 min_context_switch。
6. 多任务处理默认使用队列链路：先 query_target_tasks(enqueue=true) 入队，再 queue_pop_head 逐项处理。

你不要做什么：
1. 不要假设任务还没排进去，然后改成逐个手动 place。
2. 不要伪造工具结果。
3. 不要重复做同类查询而没有新增结论；连续两轮同类读查询后，必须转入执行、ask_user，或明确阻塞原因。
4. list_tasks 的 status 只允许单值：all / existing / suggested / pending。禁止使用 "existing,suggested" 这类拼接值。
5. 若工具结果与已知事实明显冲突（如无写操作却从“有任务”变成“0任务”），先自我纠错并重查一次，不要直接 ask_user。
6. 不要连续两轮调用“同一读工具 + 等价 arguments”；若上一轮已成功返回，下一轮必须换工具或进入 confirm。
7. list_tasks.category 只接受任务类名称，不接受 task_class_ids（如 "1,2,3"）。
8. 若已明确“本轮先收口”，不要继续调用 list_tasks/query_available_slots/move 做无目标微调。
9. 若用户明确了微调方向，不要只做“局部看起来更空”的随机调整；每次改动都要能对应到该方向。
10. 若顺序策略为“保持顺序”，禁止调用 min_context_switch。
11. 不要在同一轮构造大规模 batch_move；batch_move 最多 2 条，超过请走队列逐项处理。
12. 未调用 queue_pop_head 获取 current 前，不要调用 queue_apply_head_move。

执行规则：
1. 只输出严格 JSON，不要输出 markdown，不要在 JSON 外补充文本。
2. 读操作：action=continue + tool_call。
3. 写操作：action=confirm + tool_call。
4. 缺关键上下文且无法通过工具补齐：action=ask_user。
5. 任务完成：action=done，并在 goal_check 总结完成证据。
6. 流程应正式终止：action=abort。`

// BuildExecuteSystemPrompt 返回执行阶段系统提示词（有 plan 模式）。
func BuildExecuteSystemPrompt() string {
	return buildExecutePromptWithFormatGuard(executeSystemPromptWithPlan)
}

// BuildExecuteReActSystemPrompt 返回执行阶段系统提示词（自由执行模式）。
func BuildExecuteReActSystemPrompt() string {
	return buildExecutePromptWithFormatGuard(executeSystemPromptReAct)
}

// BuildExecuteDecisionContractText 返回执行阶段输出协议（有 plan 模式）。
func BuildExecuteDecisionContractText() string {
	return strings.TrimSpace(fmt.Sprintf(`
输出协议（严格 JSON）：
- speak：给用户看的话
- action：只能是 %s / %s / %s / %s / %s
- reason：给后端和日志看的简短说明
- goal_check：输出 %s 或 %s 时必填，对照 done_when 逐条验证
- tool_call：输出 %s（写操作，需 confirm）或 %s（读操作）时可附带
- tool_call 格式：{"name":"工具名","arguments":{...}}

示例：
{
  "speak": "我先查看当前整体安排。",
  "action": "%s",
  "reason": "需要先调用 get_overview 获取事实",
  "tool_call": {
    "name": "get_overview",
    "arguments": {}
  }
}

{
  "speak": "当前步骤已完成。",
  "action": "%s",
  "reason": "已完成当前步骤所需查询与校验",
  "goal_check": "已满足当前步骤 done_when 条件"
}

{
  "speak": "",
  "action": "%s",
  "reason": "整体任务已完成"
}
`,
		newagentmodel.ExecuteActionContinue,
		newagentmodel.ExecuteActionAskUser,
		newagentmodel.ExecuteActionConfirm,
		newagentmodel.ExecuteActionNextPlan,
		newagentmodel.ExecuteActionDone,
		newagentmodel.ExecuteActionNextPlan,
		newagentmodel.ExecuteActionDone,
		newagentmodel.ExecuteActionConfirm,
		newagentmodel.ExecuteActionContinue,
		newagentmodel.ExecuteActionContinue,
		newagentmodel.ExecuteActionNextPlan,
		newagentmodel.ExecuteActionDone,
	))
}

// BuildExecuteReActContractText 返回自由执行模式输出协议。
func BuildExecuteReActContractText() string {
	return strings.TrimSpace(fmt.Sprintf(`
输出协议（严格 JSON）：
- speak：给用户看的话
- action：只能是 %s / %s / %s / %s
- reason：给后端和日志看的简短说明
- goal_check：输出 %s 时必填，总结任务完成证据
- tool_call：输出 %s（写操作，需 confirm）或 %s（读操作）时可附带
- tool_call 格式：{"name":"工具名","arguments":{...}}

示例：
{
  "speak": "我先看一下现在的安排分布。",
  "action": "%s",
  "reason": "先读取概览再决定微调方向",
  "tool_call": {
    "name": "get_overview",
    "arguments": {}
  }
}

{
  "speak": "我准备把两项任务对调位置，你确认后执行。",
  "action": "%s",
  "reason": "写操作需要确认",
  "tool_call": {
    "name": "swap",
    "arguments": {"task_a": 1, "task_b": 2}
  }
}

{
  "speak": "已完成你的请求。",
  "action": "%s",
  "reason": "微调执行完毕并已校验结果",
  "goal_check": "目标任务类已完成微调，且关键约束满足"
}
`,
		newagentmodel.ExecuteActionContinue,
		newagentmodel.ExecuteActionAskUser,
		newagentmodel.ExecuteActionConfirm,
		newagentmodel.ExecuteActionDone,
		newagentmodel.ExecuteActionDone,
		newagentmodel.ExecuteActionConfirm,
		newagentmodel.ExecuteActionContinue,
		newagentmodel.ExecuteActionContinue,
		newagentmodel.ExecuteActionConfirm,
		newagentmodel.ExecuteActionDone,
	))
}

// BuildExecuteDecisionContractTextV2 返回补齐 abort 协议后的执行输出契约（有 plan 模式）。
func BuildExecuteDecisionContractTextV2() string {
	return strings.TrimSpace(fmt.Sprintf(`
输出协议（严格 JSON）：
- speak：给用户看的话；若 action=%s，通常留空
- action：只能是 %s / %s / %s / %s / %s / %s
- reason：给后端和日志看的简短说明
- goal_check：输出 %s 或 %s 时必填，对照 done_when 逐条验证
- tool_call：输出 %s（写操作，需 confirm）或 %s（读操作）时可附带
- abort：仅在 action=%s 时必填，格式为 {"code":"...","user_message":"...","internal_reason":"..."}
- tool_call 与 abort 互斥，禁止同时出现

示例：
{
  "speak": "我先查看当前安排。",
  "action": "%s",
  "reason": "先读取事实再决策",
  "tool_call": {
    "name": "get_overview",
    "arguments": {}
  }
}

{
  "speak": "当前步骤完成。",
  "action": "%s",
  "reason": "步骤完成条件满足",
  "goal_check": "已满足当前步骤 done_when"
}

{
  "speak": "",
  "action": "%s",
  "reason": "流程不应继续执行",
  "abort": {
    "code": "execute_abort",
    "user_message": "当前流程无法继续执行，本轮先终止。",
    "internal_reason": "execute declared abort"
  }
}
`,
		newagentmodel.ExecuteActionAbort,
		newagentmodel.ExecuteActionContinue,
		newagentmodel.ExecuteActionAskUser,
		newagentmodel.ExecuteActionConfirm,
		newagentmodel.ExecuteActionNextPlan,
		newagentmodel.ExecuteActionDone,
		newagentmodel.ExecuteActionAbort,
		newagentmodel.ExecuteActionNextPlan,
		newagentmodel.ExecuteActionDone,
		newagentmodel.ExecuteActionConfirm,
		newagentmodel.ExecuteActionContinue,
		newagentmodel.ExecuteActionAbort,
		newagentmodel.ExecuteActionContinue,
		newagentmodel.ExecuteActionNextPlan,
		newagentmodel.ExecuteActionAbort,
	))
}

// BuildExecuteReActContractTextV2 返回补齐 abort 协议后的自由执行输出契约。
func BuildExecuteReActContractTextV2() string {
	return strings.TrimSpace(fmt.Sprintf(`
输出协议（严格 JSON）：
- speak：给用户看的话；若 action=%s，通常留空
- action：只能是 %s / %s / %s / %s / %s
- reason：给后端和日志看的简短说明
- goal_check：输出 %s 时必填，总结任务完成证据
- tool_call：输出 %s（写操作，需 confirm）或 %s（读操作）时可附带
- abort：仅在 action=%s 时必填，格式为 {"code":"...","user_message":"...","internal_reason":"..."}
- tool_call 与 abort 互斥，禁止同时出现

示例：
{
  "speak": "我先读取当前安排。",
  "action": "%s",
  "reason": "先获取事实再决策",
  "tool_call": {
    "name": "get_overview",
    "arguments": {}
  }
}

{
  "speak": "我准备执行写操作，等待你确认。",
  "action": "%s",
  "reason": "写操作需要确认",
  "tool_call": {
    "name": "move",
    "arguments": {"task_id": 5, "new_day": 3, "new_slot_start": 1}
  }
}

{
  "speak": "",
  "action": "%s",
  "reason": "当前流程不应继续执行",
  "abort": {
    "code": "domain_abort",
    "user_message": "当前流程无法继续执行，本轮先终止。",
    "internal_reason": "execute declared abort"
  }
}
`,
		newagentmodel.ExecuteActionAbort,
		newagentmodel.ExecuteActionContinue,
		newagentmodel.ExecuteActionAskUser,
		newagentmodel.ExecuteActionConfirm,
		newagentmodel.ExecuteActionDone,
		newagentmodel.ExecuteActionAbort,
		newagentmodel.ExecuteActionDone,
		newagentmodel.ExecuteActionConfirm,
		newagentmodel.ExecuteActionContinue,
		newagentmodel.ExecuteActionAbort,
		newagentmodel.ExecuteActionContinue,
		newagentmodel.ExecuteActionConfirm,
		newagentmodel.ExecuteActionAbort,
	))
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

// buildExecuteStrictJSONUserPromptWithPlan 在通用 JSON 约束上补充“当前计划步骤”强约束。
//
// 职责边界：
// 1. 负责把“当前是第几步、当前步骤内容、done_when 判定”明确写进用户指令；
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
- 禁止跳步：不要提前执行后续步骤。`,
		base, current, total, stepContent, doneWhen))
}

// buildExecutePromptWithFormatGuard 统一补一层更硬的 JSON 输出约束。
func buildExecutePromptWithFormatGuard(base string) string {
	base = strings.TrimSpace(base)
	guard := strings.TrimSpace(`
补充 JSON 约束：
1. 只输出当前 action 真正需要的字段；无关字段直接省略，不要用 ""、{}、[]、null 占位。
2. 若输出 tool_call，参数字段名只能是 arguments，禁止写成 parameters。
3. tool_call 只能是单个对象：{"name":"工具名","arguments":{...}}，不能输出数组。
4. 只有 action=abort 时才允许输出 abort 字段；非 abort 动作不要输出 abort。
5. action=continue / ask_user / confirm 时，speak 必须是非空自然语言。`)
	if base == "" {
		return guard
	}
	return base + "\n\n" + guard
}

// buildExecuteStrictJSONUserPrompt 统一构造 execute 阶段面向模型的最终用户指令。
func buildExecuteStrictJSONUserPrompt() string {
	return strings.TrimSpace(`
请继续当前任务的执行阶段，严格输出 JSON。
输出字段：
- speak
- action
- reason
- goal_check
- tool_call
- abort

补充格式要求：
- 与当前 action 无关的字段直接省略，不要输出空字符串、空对象、空数组或 null 占位
- tool_call 只能写 {"name":"工具名","arguments":{...}}，且每轮最多一个
- 不要写 {"tool_call":{"name":"工具名","parameters":{...}}}
- 非 abort 动作不要输出 abort 字段
- action 为 continue / ask_user / confirm 时，必须输出非空 speak
- list_tasks.arguments.status 仅允许 all / existing / suggested / pending 的单值；如需看 existing+suggested，请用 all
- list_tasks.arguments.category 仅接受任务类名称，不要传 task_class_ids（如 "1,2,3"）
- 若读工具结果与已知事实明显冲突，先修正参数并重查一次，再决定是否 ask_user
- 不要连续两轮调用“同一读工具 + 等价 arguments”；若上一轮已成功返回，下一轮必须换工具或进入 confirm
- 若用户本轮给了二次微调方向，优先满足该方向，再考虑通用均衡优化
- 若上下文已明确“当前未收到微调偏好，本轮先收口”，请直接输出 action=done
- 仅当顺序策略明确允许打乱顺序时，才可以调用 min_context_switch
- spread_even 用于“范围内均匀化”，必须先用 query_target_tasks 明确目标任务集合
- 多任务调整默认先调用 query_target_tasks(enqueue=true)，再用 queue_pop_head 逐项处理
- queue_apply_head_move 只能用于 current 任务；若当前任务无法落位，调用 queue_skip_head 后继续
- batch_move 一次最多 2 条；超过 2 条必须改走队列逐项处理
`)
}

// BuildExecuteUserPrompt 构造有 plan 模式的用户提示词。
func BuildExecuteUserPrompt(_ *newagentmodel.CommonState) string {
	return strings.TrimSpace(`
请继续当前任务的执行阶段，严格输出 JSON。
输出字段：
- speak
- action
- reason
- goal_check
- tool_call
- abort
`)
}

// BuildExecuteReActUserPrompt 构造自由执行模式的用户提示词。
func BuildExecuteReActUserPrompt(_ *newagentmodel.CommonState) string {
	return strings.TrimSpace(`
请继续当前任务的执行阶段，严格输出 JSON。
输出字段：
- speak
- action
- reason
- goal_check
- tool_call
- abort
`)
}
