# Handoff

以下内容可直接交给下一位助理继续做。

## 目标

当前有两条主线要继续推进：

1. 粗排算法修复与链路纠偏  
目标：粗排完成后，不应该再把 LLM 引导到“手动一个个 `place` 补洞”。如果粗排后仍有 `pending`，按当前业务理解，这属于异常，应直接终止并报错，而不是继续优化或补排。

2. `execute` 上下文瘦身 + 可插拔 prompt 重构  
目标：把现在的“消息流水账堆砌”改成“结构化执行简报”，并且 prompt 不能写死成排程专用，要能复用于排程、加任务、学习计划等不同任务域。

## 用户已经明确确认的业务结论

- `always_execute`、后端是否自动放行、是否写库，这些是后端执行层语义，不应写进 prompt。
- LLM 只需要按统一协议产出 `continue / confirm / ask_user / done / abort` 这类动作；后端怎么处理是后端自己的事。
- 对排程场景，LLM 的主要职责是“粗排后的优化器”，不是“粗排补洞工”。
- 如果“粗排完成后仍有 pending 任务”，这不是要让 LLM 手工 `place` 的正常状态，而是异常状态。
- prompt 需要明显的文字引导，必须有编号和子编号，让 LLM 每轮都收到一份规范文本。
- prompt 必须是可插拔的，不能写死成“排程优化”专用。

## 已经完成的改动

- 已修复“同一轮 user message 重复写入上下文”的问题。  
实现位置：`backend/newAgent/node/chat.go`  
改动点：`handleChatResume` 不再重复 `AppendHistory(schema.UserMessage(...))`，现在 user message 只在 service 层统一写入一次。

- 已经给 `execute` 节点加了完整上下文调试打点。  
实现位置：`backend/newAgent/node/execute.go`  
关键函数：`formatExecuteLLMMessagesForDebug`

- 之前已经做过一轮粗排结果接入修复：`makeRoughBuildFunc` 改为使用 `HybridScheduleWithPlanMultiFunc` 的 `entries` 结果，而不是只看 `[]TaskClassItem`。  
实现位置：`backend/service/agentsvc/agent_newagent.go`

## 当前上下文链路的真实现状

`execute` 真正喂给 LLM 的消息来自：

- `backend/newAgent/node/execute.go`
- `backend/newAgent/prompt/execute.go`
- `backend/newAgent/prompt/base.go`

当前拼装顺序是：

- `system`：基础 persona + execute 阶段规则
- `system`：工具摘要
- `history`：完整历史消息
- `system`：pinned blocks
- `user`：运行时执行提示词

这套链路的核心问题不是“少了什么”，而是“保留了太多不该保留的东西”。

## 已确认的上下文膨胀问题

基于用户提供的第 13 轮上下文样本，当前冗余主要有这些：

- 大型 `tool result` 长期保留。  
典型是 `get_overview`、`list_tasks`、`find_free` 的超长结果被反复塞进 history。

- 同工具同参数的重复查询长期保留。  
例如 `find_free(duration=2)` 连续多次查询，主体内容几乎相同；`list_tasks(all)` 与 `get_overview` 也重复大量信息。

- 大量 assistant 过程性话术进入 history。  
例如“我先查一下”“我需要先获取”“我将安排……请确认”这类文本，对后续决策价值很低，却持续吃 token。

- 失败回合被原样保留。  
例如 `place` 缺 `task_id`、`find_free` 缺 `duration` 的失败记录，不需要完整原文链路，只需要摘要化保留“最近失败模式”。

- 指令层重复。  
`renderStateSummary`、pinned blocks、运行时 user prompt 存在明显重叠。

- `newAgent` 目前没有接旧链路那套历史 token budget 裁剪。  
对照位置：
  - 新链路：`backend/service/agentsvc/agent_newagent.go`
  - 旧链路：`backend/service/agentsvc/agent.go`
  - token budget 工具：`backend/pkg/token_budget.go`

## 当前排程链路里最需要纠偏的错误引导

当前这段逻辑已经不符合用户现在确认的业务前提：

- `backend/newAgent/node/rough_build.go`

这里现在会在粗排后写入一段 pinned 文本，大意是：

- 如果还有 `pending`，就让 LLM 去 `get_overview/find_free/place`
- 重复 place，直到 pending 归零

这段引导现在应视为错误业务语义。下一位助理需要重点改掉它。

## 粗排算法主线的交接意见

下一位助理要继续查两件事：

- 粗排算法本体是否真的仍会漏排。  
重点排查：
  - `makeRoughBuildFunc`
  - `RunRoughBuildNode`
  - `placements` 写入 `ScheduleState` 后，是否所有目标任务都应有初始落位

- 如果业务上“粗排不应漏排”已经成立，那么链路要改成：
  - 粗排完成且 `pending > 0`：直接异常结束
  - 不再把 LLM 引导成“手工补排”
  - 最好在执行层支持 `abort` 语义，而不是让模型继续乱试

## prompt 重构主线的交接意见

用户已经认可的新方向是：把 prompt 改成“通用执行内核 + 可插拔领域模块 + 当前任务简报”。

推荐的 3-message 结构如下。

### 第一条消息：通用执行内核

职责：

- 定义 agent 身份
- 定义通用规则
- 定义通用动作协议
- 提供最小必要的 JSON 示例

### 第二条消息：领域模块

职责：

- 注入当前领域名称、职责边界、目标、非目标
- 注入领域工具简表
- 注入领域硬约束、软目标
- 注入异常定义与完成判定

### 第三条消息：运行时任务简报

职责：

- 给出用户原始目标与最新补充
- 给出当前实例级约束
- 给出最新状态快照
- 给出最近操作摘要
- 给出上一次工具调用结果
- 给出本轮目标

## 用户已经认可的 prompt 设计原则

- 必须保留 JSON 示例，否则 LLM 容易不会按协议输出。
- prompt 必须有显式编号和子编号，例如 `1. / 1.1 / 2.1`。
- prompt 不能写死成排程专用。
- 排程只是一个领域模块示例，不是通用内核的一部分。
- 对排程领域来说，应明确：
  - 这是“粗排后的优化器”
  - 不是“补排器”
  - `pending > 0` 是异常条件，不是待办事项
- 对不同领域，应通过占位参数注入，不要把具体业务写进通用层。

## 已产出的可插拔 prompt 方案要点

建议最终落地成这三层：

### 通用执行内核

- 身份
- 通用规则
- 通用动作协议
- 输出字段定义
- 最小 JSON 示例

### 领域模块

- `domain_name`
- `task_type`
- `domain_primary_responsibility`
- `domain_out_of_scope`
- `domain_goals`
- `domain_non_goals`
- `tool_catalog_brief`
- `tool_usage_rules`
- `tool_required_args_rules`
- `tool_common_failures`
- `hard_constraints`
- `soft_objectives`
- `abort_conditions`
- `abort_handling_rules`
- `done_conditions`
- `abort_output_conditions`

### 运行时任务简报

- `original_user_goal`
- `latest_user_instruction`
- `current_effective_goal`
- `current_phase`
- `current_round`
- `instance_constraints`
- `latest_state_summary`
- `latest_state_delta`
- `latest_risks`
- `recent_operation_summary`
- `recent_failure_patterns`
- `last_tool_name`
- `last_tool_arguments_summary`
- `last_tool_result_summary`
- `last_tool_success`
- `last_tool_state_change`
- `last_tool_takeaway`
- `current_round_goal`
- `recommended_next_action`

## 排程领域的具体模块语义

如果当前领域是“粗排后的排程优化”，建议这样填：

- `domain_name = schedule_optimization`
- `domain_primary_responsibility = 在粗排结果基础上优化排程质量`
- `domain_out_of_scope = 手工补排粗排遗漏任务`
- `domain_goals = 更均匀、更符合学习规律、更平衡每日负载`
- `domain_non_goals = 把 pending 任务一个个 place 进去`
- `abort_conditions = 粗排完成后仍有 pending 任务`
- `abort_handling_rules = 不再继续优化，不再 place，直接 abort`
- `done_conditions = 方案满足硬约束且整体分布合理`

## 代码层建议的实施顺序

建议下一位助理按这个顺序做，风险最低：

1. 先改粗排后 pinned 引导  
重点文件：`backend/newAgent/node/rough_build.go`  
目标：删掉“pending 继续 place”的提示，换成“pending 是异常”的提示。

2. 再补 `abort` 动作语义  
重点文件：
  - `backend/newAgent/node/execute.go`
  - 相关 decision model 定义文件
  - 可能涉及 deliver / graph 分支  
目标：让 LLM 可以正规地终止异常流程，而不是只能 continue / done / ask_user / confirm。

3. 再做 prompt 结构重构  
重点文件：
  - `backend/newAgent/prompt/base.go`
  - `backend/newAgent/prompt/execute.go`
  - 如有必要，可新增一个领域模块文件  
目标：把目前“system/tool/history/pinned/runtime prompt”重组为“通用内核 + 领域模块 + 任务简报”。

4. 最后再做历史瘦身  
目标：
  - 同工具同参数结果只保留最近一份原文
  - 更早历史改摘要
  - assistant 废话不入 history
  - 失败模式摘要化
  - 必要时接入 token budget

## 关于历史瘦身，已达成的结论

下一位助理可以直接照这个原则做：

- 不再把几十条 `assistant/tool` 原始流水账直接喂给模型
- 把历史改成“状态快照 + 最近摘要 + 上一次结果 + 本轮目标”
- `tool result` 只保留：
  - 最新一条原文
  - 更早的同类结果摘要
- 重复查询要压缩：
  - 同工具同参数只保留最新一条
- assistant 过程话术要剔除：
  - “我先查一下”“我将继续……”之类原则上不入模型历史
- 保留最近失败模式：
  - 例如 `place` 缺 `task_id`
  - 例如 `find_free` 缺 `duration`

## 测试与验证注意事项

- 运行 `go test` 后，必须清理项目根目录 `.gocache`。
- 当前环境可能会因为网络限制导致 `go test` 拉依赖失败；之前已经出现过这种情况。
- 项目要求：
  - 注释、接口文案、说明、评审反馈都用中文
  - 文件编码 UTF-8（无 BOM）
  - 不要把 agent 改回写库逻辑；当前用户明确要求 agent 操作只写内存，不写数据库
- 代码中若改动复杂逻辑，注释要同步更新，且注释必须用中文

## 关键文件清单

- 执行节点与上下文打点：`backend/newAgent/node/execute.go`
- prompt 拼装基础：`backend/newAgent/prompt/base.go`
- execute prompt：`backend/newAgent/prompt/execute.go`
- 粗排节点：`backend/newAgent/node/rough_build.go`
- graph 节点装配：`backend/newAgent/node/agent_nodes.go`
- newAgent service 入口：`backend/service/agentsvc/agent_newagent.go`
- 旧链路 token budget 参考：`backend/service/agentsvc/agent.go`
- token budget 工具：`backend/pkg/token_budget.go`

## 一句话总结给下一位助理

当前要做的，不是继续 patch 某个 prompt 文案，而是同时完成两件事：

- 把“粗排后 pending 还让 LLM 手工补排”的错误业务语义彻底清掉
- 把 `execute` 从“消息流水账喂模”重构成“通用执行内核 + 可插拔领域模块 + 运行时任务简报”的结构化 prompt

## TODO Checklist

### 粗排算法与异常语义

- [ ] 确认粗排算法本体是否真的会漏排
- [ ] 确认 `placements` 写入 `ScheduleState` 后是否所有目标任务都已有初始落位
- [ ] 删除 `rough_build` 节点里“pending 继续 place”的错误提示
- [ ] 改成“粗排后 pending > 0 即异常”的提示语义
- [ ] 在执行决策层补齐 `abort` 动作语义

### Prompt 重构

- [ ] 抽出通用执行内核 prompt
- [ ] 抽出领域模块 prompt
- [ ] 抽出运行时任务简报拼装逻辑
- [ ] 保留最小必要 JSON 示例
- [ ] 清除后端执行层语义对 LLM 的干扰
- [ ] 让排程领域以模块方式接入，而不是写死在内核

### 历史瘦身

- [ ] 同工具同参数仅保留最新一条原文
- [ ] 更早同类结果改为摘要
- [ ] assistant 过程性废话不再进入模型历史
- [ ] 最近失败模式摘要化保留
- [ ] 必要时接入 token budget

