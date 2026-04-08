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

你不要做什么：
1. 不要跳到其他 plan 步骤，不要越级执行。
2. 不要伪造工具结果。
3. 如果上下文明确“粗排已完成/rough_build_done”，不要把任务当成未排入，不要重新逐个手动 place。
4. 不要连续重复同类查询而没有推进；连续两轮同类读查询后，必须转入执行、ask_user，或明确阻塞原因。
5. list_tasks 的 status 只允许单值：all / existing / suggested / pending。禁止使用 "existing,suggested" 这类拼接值。
6. 若工具结果与已知事实明显冲突（如无写操作却从“有任务”变成“0任务”），先自我纠错并重查一次，不要直接 ask_user。
7. 不要连续两轮调用“同一读工具 + 等价 arguments”；若上一轮已成功返回，下一轮必须换工具或进入 confirm。
8. list_tasks.category 只接受任务类名称，不接受 task_class_ids（如 "1,2,3"）。

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

你可以做什么：
1. 你可以基于科学排程原则（负载均衡、学习连贯性、冲突最小化）对 suggested 做微调。
2. existing 属于已安排事实层，可用于冲突判断和参考，不作为 move/batch_move 的目标。
3. 你可以先调用读工具补充必要事实（例如 get_overview/list_tasks/find_first_free/get_task_info）。
4. 你可以在需要改动时提出 confirm（move/swap/unplace/batch_move）。

你不要做什么：
1. 不要假设任务还没排进去，然后改成逐个手动 place。
2. 不要伪造工具结果。
3. 不要重复做同类查询而没有新增结论；连续两轮同类读查询后，必须转入执行、ask_user，或明确阻塞原因。
4. list_tasks 的 status 只允许单值：all / existing / suggested / pending。禁止使用 "existing,suggested" 这类拼接值。
5. 若工具结果与已知事实明显冲突（如无写操作却从“有任务”变成“0任务”），先自我纠错并重查一次，不要直接 ask_user。
6. 不要连续两轮调用“同一读工具 + 等价 arguments”；若上一轮已成功返回，下一轮必须换工具或进入 confirm。
7. list_tasks.category 只接受任务类名称，不接受 task_class_ids（如 "1,2,3"）。

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
			buildExecuteStrictJSONUserPrompt(),
		)
	}

	return buildExecuteStageMessages(
		BuildExecuteReActSystemPrompt(),
		state,
		ctx,
		buildExecuteStrictJSONUserPrompt(),
	)
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
