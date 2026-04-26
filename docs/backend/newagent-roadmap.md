# NewAgent 改造路线

## 核心架构

砍掉路由和多张业务 graph，换成一张通用循环图，用 eino compose 搭建。能力通过 tool 横向扩展，图本身不再变。

### 循环图结构

```
START → Plan Loop(循环，直到 PLAN_DONE) → Confirm Plan → Execute Loop(ReAct + Reflection) → 交付 → END
```

- Plan Loop：每轮后端注入上下文（当前阶段、已确定步骤、已收集信息），LLM 每次只想一步，可调感知类 tool 收集信息，输出 [PLAN_DONE] 进入下一阶段
- Confirm：plan 完成后推给用户确认，不 ok 回 Plan 重来
- Execute Loop：按 plan 逐步调 tool，每步完 reflection，发现 plan 有问题可自行修正
- 交付：执行结果推给用户检查

### 关键设计决策

1. **不需要路由**：整个 agent 就是一个 tool-use 循环，LLM 自己判断聊天还是干活，上下文理解能力本身就是最好的路由器
2. **写操作前必须 confirm**：所有会改动日程的 tool 执行前自动触发用户确认，读操作不需要
3. **Thinking 策略**：首轮理解意图时开，后续 tool 循环轮次关掉
4. **Graph 的角色**：从业务流程编排降级为基础设施，编排 agent loop 本身（LLM → tool → LLM 循环），业务逻辑下沉到 tools

## 工具设计原则

工具做计算，LLM 做决策。LLM 不碰原始时间数据，只看自然语言级别的信息。

### 感知类（让 LLM "看"时间）
- `get_free_slots(date_range, min_duration)` — 返回空闲时段
- `get_conflicts(proposed_event)` — 返回冲突信息
- `get_day_summary(date)` — 某天负载概况
- `get_task_context(task_id)` — 任务完整上下文

### 操作类（让 LLM "动手"）
- `create_event` / `update_event` / `delete_event` — 基础 CRUD
- `batch_create_events(events[])` — 批量创建
- `swap_events(event_a, event_b)` — 交换时间段
- `reschedule_event(event_id, constraints)` — 自动找合适时段重排

### 分析类（让 LLM "想"）
- `estimate_workload(date_range)` — 工作量分布
- `find_best_slot(duration, deadline, preferences)` — 给定约束算最优时段
- `check_feasibility(task_list, deadline)` — 可行性检测

## 状态设计

State 和 Context 分离：

- `AgentState`：阶段标记、plan 步骤、confirm 状态、tool 调用记录、轮次计数（流程控制）
- `ConversationContext`：消息历史、system prompt、tool schemas、注入的阶段上下文（对话管理）

两者通过 traceID / session 关联，数据结构分开。

## 落地方式

- `newagent/` 复制型搬迁，分层结构延续
- 已搬迁：`llm/`（client、ark、json）、`stream/`（emitter、openai）、`shared/`（time、retry）
- 包名统一为 `newagent` 前缀（newagentllm、newagentstream、newagentshared）
- 新增 `tool/` 目录存放可插拔工具
- 老 agent 保留对照，跑通再删

## 优先级

P0（先做）：循环图 + 感知类工具 + 基础 CRUD + ask_user 多轮交互 + confirm 机制
P1（后续）：记忆机制、websearch、skills、分析类工具

## 协作模式

我（人类）搭函数框架 + 写清楚注释，Codex 负责填充实现。架构决策权在人类手里。
