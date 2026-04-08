# Handoff

以下内容可直接交给下一位助理继续做。

本文档更新时间：2026-04-08

## 0. 当前结论先说清

当前可以明确分成三段看：

1. 第 1-2 阶段已完成  
   粗排链路和 `abort -> deliver` 正式终止协议已打通，真实链路已验证可跑。

2. 第 3 阶段第一版已落地  
   `execute` 上下文已改成固定 4 消息结构，并接入当轮 ReAct 窗口压缩（按工具去重保留最新 observation）；  
   工具结果在执行链路中已改为不截断，prompt 也已加硬约束（JSON、参数名、读写动作、重复读约束等）。

3. 当前主矛盾已转移到“工具收敛能力”  
   最新日志显示：不是上下文失控，而是工具输出对“何时停止微调”支持不足，导致模型持续 `list_tasks/find_first_free/move` 小步循环。

一句话总结：

- 粗排和执行框架已经能跑；
- 上下文瘦身第一版已经到位；
- 下一棒应优先改工具层的“冲突表达 + 候选位质量 + 结束判据”，而不是再回头补粗排。

---

## 1. 用户已经明确确认的业务语义

这些结论已经和用户对齐，后续不要再摇摆：

1. 第一轮实现优先参考旧 agent 链路，不能闭门造车。

2. 对排程场景来说，LLM 的角色是“粗排后的优化器”，不是“粗排漏排后的人工补排器”。

3. 如果粗排完成后仍有真实 `pending` 任务，这不是正常状态，而是异常状态。  
   正确处理是：
   - 直接终止本轮
   - 明确报错
   - 不再引导 LLM 去一个个 `place`

4. `always_execute`、是否自动放行、是否写库，这些都是后端执行层语义，不应该写进 prompt 让 LLM 判断。

5. prompt 必须可插拔，不能写死成“排程专用 prompt”。

6. prompt 必须保留明确编号结构，例如 `1. / 1.1 / 2.1`，不能写成一坨说明文。

7. prompt 里要保留最小可用 JSON 示例，否则 LLM 很容易输出跑偏。

---

## 2. 第 1 阶段：粗排链路修复，已完成

### 2.1 已完成的核心修复

#### A. 粗排结果来源修正

位置：

- `backend/service/agentsvc/agent_newagent.go`

已做：

- `makeRoughBuildFunc()` 不再只看 `[]TaskClassItem`
- 改为使用 `HybridScheduleWithPlanMulti()` 返回的 `[]HybridScheduleEntry`
- 只提取 `status="suggested"` 且 `task_item_id>0` 的条目

意义：

- 旧问题是：只会拿到有 `EmbeddedTime` 的一部分结果，普通 suggested 条目会丢
- 现在粗排 placement 来源和旧 agent 主链路一致

#### B. 作用域修正：只按本轮 `task_class_ids` 看 pending

位置：

- `backend/newAgent/tools/status.go`
- `backend/newAgent/model/graph_run_state.go`
- `backend/newAgent/node/rough_build.go`

已做：

- 新增按 `task_class_ids` 判断 task 是否属于本轮范围
- `EnsureScheduleState()` 会对 `ScheduleState` / `OriginalScheduleState` 做域内裁剪
- `countPendingTasks()` 只统计本轮任务类范围内的真实 pending

意义：

- 修掉“用户所有任务类的 pending 被误算进本轮粗排结果”的问题

#### C. 窗口修正：ScheduleState 的 DayMapping 改为优先按本轮任务类加载

位置：

- `backend/newAgent/model/state_store.go`
- `backend/newAgent/model/graph_run_state.go`
- `backend/conv/schedule_provider.go`

已做：

- 新增可选接口 `ScopedScheduleStateProvider`
- `AgentGraphState.EnsureScheduleState()` 首次加载时，若 provider 支持 scoped 加载，则优先走 `LoadScheduleStateForTaskClasses()`
- `ScheduleProvider` 实现了按本轮 `task_class_ids` 精确加载 `ScheduleState`
- `buildWindowFromTaskClasses()` 改成逐个任务类做日期转换，坏日期/超学期日期会被忽略，不再把整轮窗口拖坏

这次修的是一个真实 bug：

- 用户真实日志里曾出现：
  - `placements=44`
  - `pending_in_scope=44`
- 这说明 placement 已经出来了，但一个都没写回 state
- 根因不是粗排没算出来，而是 `DayMapping` 窗口被全量任务类脏日期污染，导致 `WeekDayToDay()` 映射失败

现在这个问题已经修掉，用户已确认“粗排跑通了”。

#### D. 粗排落位语义改成 `suggested`

位置：

- `backend/newAgent/node/rough_build.go`
- `backend/newAgent/tools/read_tools.go`
- `backend/newAgent/tools/read_helpers.go`
- `backend/newAgent/tools/write_tools.go`
- `backend/newAgent/tools/SCHEDULE_TOOLS.md`

已做：

- 粗排成功写回后，任务状态会从真实 pending 变成 `suggested`
- 不再使用“pending + slots”的旧兼容语义作为主路径
- 工具读写层已经统一支持 `existing / suggested / pending`

意义：

- 粗排结果是“建议落位”，后续应使用 `move / swap / unplace`
- 不应该再让 LLM 把这些任务当作待补排任务来 `place`

#### E. 粗排节点增加了可观测 debug

位置：

- `backend/newAgent/node/rough_build.go`

现在会打这些日志：

- `placements`
- `applied`
- `day_mapping_miss`
- `task_item_match_miss`
- `pending_in_scope`
- `window_days`
- 少量 miss 样本

意义：

- 下次如果粗排再挂，不需要再靠猜
- 能直接区分是：
  - DayMapping 没命中
  - 还是 `task_item_id` 没命中

### 2.2 粗排阶段当前的真实状态

当前真实结论：

- 粗排已经能跑通
- 用户已在真实链路确认这一点

所以粗排这条线当前不是主矛盾了。

---

## 3. 第 2 阶段：正式 abort/terminal 协议，已完成

### 3.1 已完成的核心改动

#### A. CommonState 引入统一 terminal outcome

位置：

- `backend/newAgent/model/common_state.go`

已做：

- 新增：
  - `FlowTerminalStatus`
  - `FlowTerminalOutcome`
- 新增方法：
  - `Abort(...)`
  - `Exhaust(...)`
  - `HasTerminalOutcome()`
  - `TerminalStatus()`
  - `IsCompleted()`
  - `IsAborted()`
  - `IsExhaustedTerminal()`
  - `ClearTerminalOutcome()`

意义：

- 后续 graph / execute / deliver 不再各自猜“当前算不算结束”
- 统一围绕一份终止结果收口

#### B. execute contract 正式支持 `abort`

位置：

- `backend/newAgent/model/execute_contract.go`
- `backend/newAgent/prompt/execute.go`

已做：

- 新增 `ExecuteActionAbort = "abort"`
- `ExecuteDecision` 新增 `Abort *AbortIntent`
- `Validate()` 已补齐互斥/必填规则
- execute prompt / react prompt 都已加入 `abort` 契约和 JSON 示例

#### C. rough_build 异常会正式 abort，而不是让 LLM 补排

位置：

- `backend/newAgent/node/rough_build.go`

已做：

- 若粗排后仍有真实 pending：
  - 发失败状态
  - `flowState.Abort(...)`
  - 直接交给 deliver 收口

这部分已经和用户确认过业务语义，是对的。

#### D. execute 层接入 abort / exhausted 正式语义

位置：

- `backend/newAgent/node/execute.go`

已做：

- execute 轮次耗尽时，不再伪装成 `done`
- 改为 `flowState.Exhaust(...)`
- 新增 `handleExecuteActionAbort(...)`
- `abort` 不在 execute 内直接最终答复，而是交给 deliver

#### E. graph 路由改成看正式 terminal outcome

位置：

- `backend/newAgent/graph/common_graph.go`

已做：

- `RoughBuild -> Execute` 改为 branch
- 粗排异常终止时可以直接 `RoughBuild -> Deliver`
- `branchAfterExecute()` 不再因为“刚好最后一轮”就提前 deliver
- 必须等 execute 正式写入终止结果

#### F. deliver 收口统一按 terminal outcome 输出

位置：

- `backend/newAgent/node/deliver.go`
- `backend/newAgent/node/agent_nodes.go`

已做：

- deliver 不再无脑 `Done()`
- deliver summary 会优先看 terminal outcome
- 新增：
  - `buildAbortSummary`
  - `buildExhaustedSummary`
- 只有 `completed` 路径才写 schedule preview
- `aborted / exhausted` 跳过 preview

### 3.2 第 2 阶段的验证情况

本地已跑通过：

```bash
go test ./conv ./newAgent/node ./newAgent/model ./newAgent/graph ./newAgent/tools ./service/agentsvc
```

注意：

- 每次 `go test` 后都已清理根目录 `.gocache`
- 过程中使用过临时 `*_test.go` 验证窗口问题和粗排写回问题，跑完后已全部删除

---

## 4. 非常重要：当前“上下文”到底改没改

这个点后续助理很容易误判，这里单独写清楚。

### 4.1 没做的事

这轮**没有**做第 3 阶段那种“系统性的上下文瘦身”。

也就是说，以下这些东西没有被系统重构：

- `ConversationContext` 的整体装配框架
- 历史恢复/回填主流程
- 历史裁剪策略
- token budget 接入
- “把 history 从流水账改成摘要流”的体系化工程

### 4.2 但确实改了会进入模型上下文的内容

为了适配新的粗排/abort 语义，这轮确实改了“上下文内容本身”，主要包括：

1. 粗排完成后的 pinned block 文案变了  
   位置：
   - `backend/newAgent/node/rough_build.go`

2. execute 的提示词/输出契约变了  
   位置：
   - `backend/newAgent/prompt/execute.go`

3. 工具层的状态语义从 `pending/existing` 扩成了 `pending/suggested/existing`  
   位置：
   - `backend/newAgent/tools/read_tools.go`
   - `backend/newAgent/tools/write_tools.go`

4. state summary 会展示 terminal outcome  
   位置：
   - `backend/newAgent/prompt/base.go`

### 4.3 所以当前准确结论是

可以这样理解：

- **没有做上下文瘦身**
- **但确实改了进入模型的上下文内容**

所以如果下游 `execute` 现在表现依旧很差，不能简单说：

- “是第 1-2 阶段没做好”

更准确的说法是：

- 第 1-2 阶段把粗排和终止语义打通了
- 但下游本来就被上下文噪音淹着
- 现在必须进入第 3 阶段，先瘦身，再评价整体效果

---

## 5. 当前主矛盾：不是粗排，而是 execute 上下文过胖

用户已经明确表达：

- “下游本来就基本跑不通”
- “必须要瘦身上下文才能看出整体的效果”

这意味着明天接手时不要再把精力放在：

- 继续 patch 粗排小问题
- 继续围绕 abort 收口补语义

下一步的主任务必须切换为：

- 第 3 阶段：上下文瘦身
- 第 4 阶段：prompt 结构重构

---

## 6. 第 3 阶段：上下文瘦身，明天应该怎么做

### 6.1 目标

目标不是“再写一版更长的 prompt”，而是：

- 让 execute 每轮看到的输入，变成“足够决策、但不淹没”的信息量

换句话说，要把现在的：

- `system + tool summary + 全量 history + pinned + runtime prompt`

改造成更可控的：

- 少量稳定规则
- 少量当前必要状态
- 少量最近真实有效观察

### 6.2 第 3 阶段必须完成的事

#### A. 历史去流水账

重点文件：

- `backend/service/agentsvc/agent_newagent.go`
- `backend/newAgent/prompt/base.go`
- `backend/newAgent/node/execute.go`

要做：

- 同工具同参数的重复查询，不保留多份原文
- 旧结果改成摘要
- 只保留最近一条原始结果

典型对象：

- `get_overview`
- `list_tasks`
- `find_free`

#### B. assistant 过程废话不再进入后续模型历史

要处理的典型文本：

- “我先查一下”
- “我接下来会……”
- “我准备先获取……”

这些文本对下一轮决策价值极低，但会稳定吃 token。

#### C. 失败模式保留“摘要”，不要保留整段原始失败链路

例如：

- `place` 缺 `task_id`
- `find_free` 缺 `duration`

正确保留方式应该更像：

- 最近失败模式：`place` 缺少 `task_id`

而不是整段原始 tool call / tool result 全保留。

#### D. 缩短 state summary / pinned / runtime prompt 的重叠信息

目前这三层有明显重复：

- `renderStateSummary`
- pinned blocks
- execute runtime user prompt

第 3 阶段要先做减法，减少重复指令。

#### E. 参考旧链路的 token budget

重点参考：

- `backend/service/agentsvc/agent.go`
- `backend/pkg/token_budget.go`

要求：

- 不一定照搬
- 但至少要借鉴旧链路“按预算裁历史”的思路

### 6.3 第 3 阶段推荐顺序

建议按这个顺序做：

1. 先打印/抓取 `BuildExecuteMessages()` 的真实输入样本
2. 再压 `history`
3. 再压 `pinned/state summary/runtime prompt` 的重叠
4. 最后再接 token budget

原因：

- 先把“到底胖在哪”看清楚
- 避免直接上 budget 把有用信息也一起砍掉

### 6.4 第 3 阶段完成标准

至少要满足：

1. execute 首轮 messages 显著变短
2. 同类查询不会反复堆原始结果
3. assistant 流水话大幅减少
4. 最近失败模式还能被模型感知
5. 不破坏第 1-2 阶段已经打通的粗排/abort 语义

---

## 7. 第 4 阶段：prompt 结构重构，明天应该怎么做

### 7.1 目标

把当前 execute prompt 从“堆规则 + 堆领域细节 + 堆运行时说明”的混合物，重构成：

1. 通用执行内核
2. 领域模块
3. 运行时任务简报

这是用户已经认可的方向。

### 7.2 建议的三层结构

#### 第一层：通用执行内核

负责：

- agent 身份
- 通用动作协议
- 通用输出字段
- 最小 JSON 示例

这里不要写死排程语义。

#### 第二层：领域模块

对排程领域来说，建议抽成单独模块，至少包含：

- `domain_name`
- `domain_primary_responsibility`
- `domain_out_of_scope`
- `domain_goals`
- `domain_non_goals`
- `tool_catalog_brief`
- `hard_constraints`
- `soft_objectives`
- `abort_conditions`
- `done_conditions`

对“粗排后排程优化”这个领域，必须明确写清：

- 这是优化器，不是补排器
- `pending > 0` 是异常，不是待办

#### 第三层：运行时任务简报

负责：

- 用户原始目标
- 最新补充指令
- 当前阶段
- 当前轮次
- 当前实例级约束
- 最近状态变化
- 最近失败摘要
- 上一次工具结果摘要
- 本轮目标
- 推荐下一步动作

### 7.3 第 4 阶段不要做成什么样

不要做成：

- 换一版更长的 execute prompt
- 继续把排程规则硬编码进通用层
- 继续把运行时状态散落在多个 message 里重复讲

### 7.4 第 4 阶段推荐落地文件

可以考虑在以下位置拆分：

- `backend/newAgent/prompt/base.go`
- `backend/newAgent/prompt/execute.go`

如有必要，可以新增文件，例如：

- `backend/newAgent/prompt/execute_core.go`
- `backend/newAgent/prompt/execute_domain_schedule.go`
- `backend/newAgent/prompt/execute_runtime_brief.go`

### 7.5 第 4 阶段完成标准

至少要满足：

1. 通用执行协议不再写死排程业务
2. 排程语义以领域模块方式注入
3. 运行时信息有单独的结构化简报
4. prompt 保留编号结构
5. prompt 保留最小 JSON 示例

---

## 8. 明天接手时，最重要的判断标准

明天的助理接手后，不要先问：

- “为什么微调效果还是差？”

而要先问：

- “第 3 阶段有没有把上下文瘦下来？”

只有在第 3 阶段做完之后，才适合重新评估：

- execute 是否真正理解了粗排后的 suggested 语义
- prompt 重构是否真的提升了整体表现
- 端到端排程链路是否比之前更稳定

---

## 9. 关键文件清单

### 第 1-2 阶段已改文件

- `backend/conv/schedule_provider.go`
- `backend/newAgent/model/state_store.go`
- `backend/newAgent/model/graph_run_state.go`
- `backend/newAgent/model/common_state.go`
- `backend/newAgent/model/execute_contract.go`
- `backend/newAgent/graph/common_graph.go`
- `backend/newAgent/node/rough_build.go`
- `backend/newAgent/node/execute.go`
- `backend/newAgent/node/deliver.go`
- `backend/newAgent/node/agent_nodes.go`
- `backend/newAgent/prompt/base.go`
- `backend/newAgent/prompt/execute.go`
- `backend/newAgent/tools/read_helpers.go`
- `backend/newAgent/tools/read_tools.go`
- `backend/newAgent/tools/write_tools.go`
- `backend/newAgent/tools/SCHEDULE_TOOLS.md`
- `backend/service/agentsvc/agent_newagent.go`

### 第 3-4 阶段重点关注文件

- `backend/service/agentsvc/agent_newagent.go`
- `backend/newAgent/prompt/base.go`
- `backend/newAgent/prompt/execute.go`
- `backend/newAgent/node/execute.go`
- `backend/pkg/token_budget.go`
- `backend/service/agentsvc/agent.go`

---

## 10. 测试与验证注意事项

1. 跑 `go test` 后必须清理项目根目录 `.gocache`

2. 如果为了验证临时补 `*_test.go`，跑完必须删除，不要长期保留

3. 当前用户明确不希望这轮把 agent 写库逻辑接回去，仍然以“内存态运行”为主

4. 所有说明、注释、文档都继续用中文

---

## 11. 一句话交给下一位助理

第 1-2 阶段已经把“粗排接入”和“正式 abort 收口”打通了，粗排真实链路也已经跑通；现在不要再围绕粗排打补丁，直接进入第 3 阶段做 execute 上下文瘦身，再做第 4 阶段 prompt 三层重构，完成后再评估整体链路效果。

---

## 12. 2026-04-08 最新增量交接（以本节为准）

> 本节优先级高于前文历史描述。接手时请先读本节，再看上文细节。

### 12.1 本轮已完成的落地项

1. execute 上下文结构已固定为 4 条消息：
   - `message[0]`：固定 prompt（规则 + JSON 约束 + 工具简表）
   - `message[1]`：历史上下文短摘要（聊天摘要 + 早期 ReAct 摘要）
   - `message[2]`：当轮 ReAct Loop 窗口（thought/reason + tool_call + observation 绑定）
   - `message[3]`：当前执行状态（初始目标、结束判断、非目标）

2. 当轮 ReAct 压缩已接入：
   - 窗口内同工具只保留最新 observation 原文；
   - 被压缩旧结果替换为“当前工具调用结果过于久远，已经被删除。”

3. execute 输出稳态增强：
   - `continue / ask_user / confirm` 缺 `speak` 时会兜底回退 `reason`；
   - 工具结果写入 history 前的截断已删除（不再自动裁到 3000 字符）。

4. 工具能力已升级：
   - `get_overview` 改为任务视角全量输出（课程仅占位统计，不展开课程明细）；
   - 新增 `find_first_free`，`find_free` 保留兼容别名；
   - `move / batch_move` 限定仅允许 `suggested`；
   - `list_tasks` 增加输入约束（`status` 单值、`category` 不接受 task_class_ids 列表）。

5. prompt 与文档同步：
   - execute prompt 已切换到 `find_first_free` 表达；
   - `SCHEDULE_TOOLS.md` 已同步 `get_overview / find_first_free / move/batch_move` 新语义；
   - plan prompt 中读工具示例也已从 `find_free` 更新为 `find_first_free`。

### 12.2 最新日志结论（关键）

本轮问题已不是“上下文塞不下”，而是“工具不利于收敛”：

1. `find_first_free` 当前策略过于贪心  
   只返回最早可用位，模型会持续把任务向前挪，容易出现“局部改善但全局不收口”的微调循环。

2. `query_range` 把“可嵌入共存”和“硬冲突”混合输出  
   模型会把可嵌入并存也当成冲突，导致不必要移动。

3. 缺少“完成判据工具”  
   当前只有读写事实工具，没有明确“是否可结束”的评估口径，模型自然倾向继续优化。

4. 写工具冲突口径存在潜在不一致  
   `findConflict` 对 `CanEmbed` 的处理与 `findEmbedHost` 的可嵌入约束并非同一套判据，后续应统一。

### 12.3 下一棒建议优先级（按顺序做）

#### P0（必须先做）

1. 新增评估类只读工具（建议名：`evaluate_balance`）  
   返回最少三项：
   - `hard_conflict_count`
   - `load_variance`（或等价离散指标）
   - `done_suggestion`（可结束/建议继续 + 原因）

2. 改 `find_first_free` 为“候选集”而非单点  
   建议支持 `top_k`（默认 3）并返回每个候选的：
   - 位置
   - 目标日负载变化
   - 是否涉及可嵌入

3. 改 `query_range` 输出结构  
   必须区分：
   - `hard_conflict`
   - `embeddable_overlap`
   避免模型把可嵌入并存误判为硬冲突。

#### P1（P0 后做）

4. prompt 增加收敛指引  
   明确要求模型在每次写操作后优先调用 `evaluate_balance`，满足条件就 `done`，避免“无限微调”。

### 12.4 接手后的最小验证清单

1. 跑一轮真实 execute，确认不会长时间卡在 `list_tasks/find_first_free/move` 循环。
2. 确认 `query_range` 可区分硬冲突与可嵌入并存。
3. 确认 `evaluate_balance` 能触发 `done` 收口。
4. 每次 `go test` 后清理项目根目录 `.gocache`。

### 12.5 关键文件（本轮增量相关）

- `backend/newAgent/prompt/execute_context.go`
- `backend/newAgent/prompt/execute.go`
- `backend/newAgent/prompt/plan.go`
- `backend/newAgent/node/execute.go`
- `backend/newAgent/tools/read_tools.go`
- `backend/newAgent/tools/write_tools.go`
- `backend/newAgent/tools/registry.go`
- `backend/newAgent/tools/SCHEDULE_TOOLS.md`
