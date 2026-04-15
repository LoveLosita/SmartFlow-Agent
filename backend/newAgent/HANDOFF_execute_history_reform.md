# Execute 阶段历史消息注入改造 Handoff

## 背景

execute 阶段给 LLM 的 4 条消息（msg0-msg3）全部是人工构造的摘要，原始对话历史从未被直接注入。
导致断连恢复后 "继续" 成为孤立的 currentGoal，LLM 无法从上下文推断原始意图。

## 现状

### 消息结构 (`execute_context.go:50-67`)

```
msg0 (System):     规则 + 工具简表
msg1 (Assistant):  历史摘要 — 用 pickExecuteUserInputs 提取 firstUser/lastUser 拼成一行
msg2 (Assistant):  当轮 ReAct Loop 窗口 — thought/tool_call/observation 详细记录
msg3 (System):     执行状态 + 锚点 — 又用 firstUser/lastUser 拼 "当前用户诉求"/"首轮目标来源"
```

### History 存储方式 (`execute.go`)

每一轮 ReAct loop 往 ConversationContext.History 写 3 条消息：
1. `assistant` + speak（自然语言，如 "我先查看当前安排。"）
2. `assistant` + ToolCalls（工具调用 JSON，content 为空）
3. `tool` + observation（工具返回，可能很长）

### 问题函数

- `pickExecuteUserInputs()` (execute_context.go:772) — 从全量 history 取第一条和最后一条 user message
- `extractExecuteGoalAnchors()` (execute_context.go:751) — 用上面的结果作为 initial/current goal
- `buildExecuteMessage1V3()` (execute_context.go:269) — 把 goal 拼成摘要行
- `buildExecuteMessage3()` (execute_context.go:351) — 把 goal 拼成执行锚点

## 改造方案

### 核心思路

msg1 从"人工提炼的摘要"变成"真实对话流"。只注入 **user + assistant speak**，不含 tool_call / observation（这些已由 msg2 承载）。

### 改造后的消息结构

```
msg0 (System):     规则 + 工具简表（不变）
msg1 (Assistant):  真实对话历史（user + assistant speak 交替）
msg2 (Assistant):  当轮 ReAct Loop 窗口（不变）
msg3 (System):     执行状态（删掉 goal 锚点，因为 msg1 已包含完整意图链）
```

### msg1 改造示例

现在：
```
历史上下文（仅供参考）：
- 用户目标：帮我排一下这些任务类；最近补充：继续
- 历史归档 ReAct 摘要：已折叠 15 条旧记录...
```

改造后：
```
对话历史：
user: "帮我把周末的课整到工作日"
assistant: "好的，我来查看当前安排。"
assistant: "已找到6个周末任务，开始逐个移动。"
assistant: "第一个任务已移动完成。"
user: "继续"
assistant: "继续处理剩余任务。"

历史归档 ReAct 摘要：已折叠 15 条旧记录...
```

### msg3 改造

删掉以下由 firstUser/lastUser 驱动的锚点：
- `当前用户诉求：xxx`
- `首轮目标来源：xxx`

保留其他锚点（轮次、模式、plan 步骤、任务类等）。

### 上下文开销

speak 都是短句（1-2 句），60 轮 execute 约 60 条 speak，远小于 msg2 里的工具结果 JSON。
可考虑加条数上限（如最近 30 条 user+assistant），超出部分走归档摘要。

## 涉及文件

| 文件 | 改动 |
|---|---|
| `prompt/execute_context.go` | 重写 `buildExecuteMessage1V3`，新增从 history 提取 user+speak 的函数；删 `pickExecuteUserInputs` / `extractExecuteGoalAnchors`；简化 `buildExecuteMessage3` |
| `prompt/execute.go` | 无改动（系统规则不含 list_tasks 相关内容，已清理） |
| `node/execute.go` | 无改动（history 写入逻辑已经存了 speak，无需修改） |

## 已完成的前置工作

- `list_tasks` 工具已删除（registry / prompt / context 全部清理干净，编译通过）
