# NewAgent 架构全景

> 本文档帮助读者建立对 newAgent 的完整心智模型，从宏观到微观逐层展开。

---

## 一、一句话概括

newAgent 是一个 **状态机驱动的有向图**：用户消息进入 Chat 节点，经过意图分类、计划生成、用户确认、工具执行、最终交付，每一步由 Phase 和 PendingInteraction 驱动路由。

---

## 二、宏观架构

### 2.1 目录结构

```
newAgent/
├── graph/           图骨架：节点注册、边连线、分支路由
├── model/           数据模型：状态、合约、接口定义
├── node/            节点实现：每个节点的业务逻辑
├── prompt/          提示词：每个阶段的 system prompt 和用户 prompt 构造
├── llm/             LLM 客户端：文本生成、JSON 解析、流式适配
├── stream/          SSE 输出：伪流式推送、OpenAI 兼容格式
├── tools/           工具层：10 个排程工具 + 注册表
├── shared/          公共工具：重试、时区
├── router/          路由（当前为空，路由逻辑在 graph/ 中）
├── ROADMAP.md       改造计划
└── ARCHITECTURE.md  本文档
```

### 2.2 图结构

```
START
  │
  v
 Chat ──────┬── 意图=chat ────→ END（直接回复）
  │         │
  │         └── 意图=task ──→ Plan ──┬── continue ──→ Plan（继续规划）
  │                                │      │
  │                                │      ├── ask_user ──→ Interrupt ──→ END
  │                                │      │
  │                                │      └── plan_done ──→ Confirm
  │                                │                │
  │                                │                ├── 有计划 → 确认卡片
  │                                │                │          │
  │                                │                │          v
  │                                │                │     Interrupt ──→ END
  │                                │                │
  │                                │                └── 有 PendingConfirmTool → 确认卡片
  │                                                       │
  │                                                       v
  │                                                  Interrupt ──→ END
  │
  │  [用户确认后重新进入图]
  │
  v
 Chat(resume) ──┬── accept → 恢复 PendingConfirmTool → Execute
               │
               └── reject → 回到 planning 或 executing

Execute ──┬── continue(读工具) ──→ Execute（继续 ReAct）
          ├── continue(无工具) ──→ Execute
          ├── confirm(写工具) ──→ Confirm ──→ Interrupt ──→ END
          ├── ask_user ──→ Interrupt ──→ END
          ├── next_plan ──┬── 有剩余步骤 → Execute
          │                └── 无剩余步骤 → Deliver
          ├── done ──→ Deliver
          └── 轮次耗尽 ──→ Deliver（强制）

Deliver ──→ END（最终总结，清理持久化快照）
```

### 2.3 一次完整排课的请求序列

```
请求1: 用户发 "帮我安排下周的复习"
  → Chat(intent=task) → Plan(plan_done, 2步计划) → Confirm → Interrupt → END
  前端展示确认卡片

请求2: 用户点 "确认"
  → Chat(resume, accept) → 确认通过 → Execute(读工具 get_overview)
  → Execute(读工具 find_free)
  → Execute(写工具 place → confirm) → Confirm → Interrupt → END
  前端展示写操作确认卡片

请求3: 用户点 "确认"
  → Chat(resume, accept) → Execute(执行 pending tool place) → 持久化
  → Execute(next_plan → 下一步)
  → Execute(done) → Deliver → END
  前端展示最终总结
```

---

## 三、状态模型

理解 newAgent 的关键在于理解 **什么东西在什么时机变化**。

### 3.1 三个核心状态对象

```
┌─────────────────────┐   持久化到 Redis
│   AgentRuntimeState  │   ← StateStore.Save/Load
│                     │
│  ┌───────────────┐  │
│  │ CommonState   │  │   ← 每个节点都可能修改
│  │ - Phase       │  │
│  │ - PlanSteps   │  │
│  │ - CurrentStep │  │
│  │ - RoundUsed   │  │
│  └───────────────┘  │
│                     │
│  PendingInteraction │   ← 确认/追问 的交互快照
│  PendingConfirmTool │   ← Execute→Confirm 的临时邮箱
└─────────────────────┘

┌─────────────────────────┐   不持久化，每次请求重建
│  ConversationContext    │
│  - SystemPrompt         │   ← 各节点 prompt 函数构造
│  - History []*Message   │   ← 对话历史（assistant+tool 配对）
│  - PinnedBlocks         │   ← 置顶上下文（计划、工具摘要）
│  - ToolSchemas          │   ← 工具 schema 注入
└─────────────────────────┘

┌─────────────────────────┐   懒加载，首次 Execute 时读取
│  ScheduleState          │
│  - Window (天数+映射)    │
│  - Tasks []ScheduleTask │   ← 工具操作的数据源
└─────────────────────────┘
```

### 3.2 Phase 状态转换

```
PhasePlanning       Plan 节点 plan_done + 用户确认后
    │
    v
PhaseExecuting      Execute 节点执行中
    │
    ├──→ PhaseWaitingConfirm   Execute 输出 action=confirm
    │        │
    │        v  用户确认
    │    PhaseExecuting          恢复继续执行
    │
    ├──→ PhaseDone              Execute 输出 done 或所有步骤完成
    │
    └──→ PhaseInterrupted       被中断（追问/确认等待用户输入）
```

### 3.3 PendingInteraction 生命周期

```
场景 A: 计划确认
  Plan → plan_done → Confirm 节点 → OpenConfirmInteraction(type="confirm")
  → Interrupt 展示 → END
  → 用户确认 → Chat(resume) → ResumeFromPending() → Phase=executing

场景 B: 写操作确认
  Execute → action=confirm → 设置 PendingConfirmTool → Confirm 节点
  → OpenConfirmInteraction(type="confirm", PendingTool=快照)
  → Interrupt 展示 → END
  → 用户确认 → Chat(resume) → PendingConfirmTool 从快照恢复 → Execute 执行工具

场景 C: 追问
  Execute → action=ask_user → OpenAskUserInteraction(question)
  → Interrupt 展示 → END
  → 用户回复 → Chat(resume) → Phase 回到 executing
```

### 3.4 PendingConfirmTool 临时邮箱

这个字段 **不持久化**，只在单次图运行中存在：

```
Execute(action=confirm)
  → PendingConfirmTool = {ToolName, ArgsJSON, Summary}
  → Phase = waiting_confirm
  → Confirm 节点读取 → 转入 PendingInteraction.PendingTool
  → PendingConfirmTool 被清空

用户确认后重新进入图：
  → Chat(resume) → 从 PendingInteraction.PendingTool 恢复到 PendingConfirmTool
  → Execute 发现 PendingConfirmTool 非空 → 直接执行工具 → 清空
```

---

## 四、各节点详解

### 4.1 Chat 节点 (`node/chat.go`)

**职责**：入口分流 + 中断恢复

**两条路径**：
1. **首次进入**：调 LLM 做意图分类（"chat" / "task"），chat 直接回复，task 转到 Plan
2. **中断恢复**：读取 PendingInteraction，根据类型（ask_user / confirm）走不同恢复路径

**关键逻辑**：
```
if HasPendingInteraction():
    handleChatResume()     // 不调 LLM
else:
    chatIntentDecision()   // 调 LLM 做意图分类
```

**confirm resume 的 accept/reject 处理**：
- accept：从 PendingInteraction.PendingTool 恢复 PendingConfirmTool，Phase=executing
- reject（有 PendingTool）：不恢复 PendingConfirmTool，Phase=executing（LLM 换方案）
- reject（无 PendingTool）：调用 RejectPlan()，Phase=planning（回到规划）

### 4.2 Plan 节点 (`node/plan.go`)

**职责**：LLM 生成结构化计划

**两阶段 LLM 调用**：
1. **Phase 1 快速评估**：temperature=0.2, max_tokens=1600, thinking=关闭
   - 输出 PlanDecision，判断 Complexity(simple/moderate/complex)
2. **Phase 2 深度规划**（仅 complex 任务触发）：thinking=开启, max_tokens=3200
   - 生成更详细的 PlanStep 列表（含 DoneWhen 完成判定条件）

**三种 action**：
- `continue`：继续规划（多轮对话中补充信息）
- `ask_user`：追问用户
- `plan_done`：规划完成，输出 PlanSteps

**计划写入 PinnedBlocks**：用 `UpsertPinnedBlock` 把计划文本注入 ConversationContext，后续 Execute 阶段自动带入。

### 4.3 Confirm 节点 (`node/confirm.go`)

**职责**：创建确认卡片，不调 LLM

**两种确认**：
1. **计划确认**（Phase=waiting_confirm, PendingConfirmTool 为空）：格式化计划摘要，创建 PendingInteraction
2. **工具确认**（PendingConfirmTool 非空）：格式化工具操作摘要，把 PendingTool 快照转入 PendingInteraction

**关键**：Confirm 节点执行后，PendingConfirmTool 被清空（数据已转移到 PendingInteraction.PendingTool）。

### 4.4 Execute 节点 (`node/execute.go`)

**职责**：LLM 主导的 ReAct 循环，这是最复杂的节点。

**入口判断优先级**：
```
1. PendingConfirmTool 非空 → executePendingTool() → 结束
2. 无有效 PlanStep → 报错
3. 正常 ReAct → 调 LLM → 处理决策
```

**LLM 调用参数**：temperature=0.3, max_tokens=1200, thinking=开启

**JSON 解析失败处理**（correction 机制）：
```
LLM 输出非 JSON:
  → ConsecutiveCorrections++
  → 追加修正消息到历史
  → return nil（图循环回来，LLM 看到修正消息后重试）
  → 连续 3 次失败 → 返回硬错误，终止
```

**五种 action 处理**：
| action | 行为 | 工具执行？ |
|--------|------|-----------|
| continue + tool_call | 读工具直接执行 | 是，executeToolCall() |
| continue 无 tool | 仅说话，继续循环 | 否 |
| confirm | 暂存 PendingConfirmTool | 否，等用户确认 |
| ask_user | 打开追问 | 否 |
| next_plan | 推进步骤 | 否 |
| done | 结束所有步骤 | 否 |

**工具执行后历史消息格式**：
```
assistant message: {Role: "assistant", ToolCalls: [{ID, Function: {Name, Arguments}}]}
tool message:     {Role: "tool", ToolCallID: <匹配ID>, Content: "工具结果"}
```
这对消息必须配对，否则 OpenAI 兼容 API 会拒绝请求。

**轮次预算**：MaxRounds 默认 30，耗尽强制进入 Deliver。

### 4.5 Interrupt 节点 (`node/interrupt.go`)

**职责**：向用户展示消息后暂停图执行

**三种类型**：
- ask_user：伪流式展示 DisplayText
- confirm：展示确认状态
- 默认：展示通用中断信息

### 4.6 Deliver 节点 (`node/deliver.go`)

**职责**：生成最终总结

- 调 LLM（temperature=0.5, max_tokens=800）生成总结
- 失败时降级到机械格式化（逐条列出步骤 + 完成标记）
- 完成后调用 deleteAgentState() 清理 Redis 快照

---

## 五、LLM 交互模式

### 5.1 统一 JSON 协议

所有 LLM 输出都是严格 JSON，不是纯文本。每个阶段有自己的合约：

**Plan 合约** (`model/plan_contract.go`)：
```json
{
  "speak": "...",
  "action": "continue|ask_user|plan_done",
  "reason": "...",
  "complexity": "simple|moderate|complex",
  "need_thinking": false,
  "plan_steps": [{"content": "...", "done_when": "..."}]
}
```

**Execute 合约** (`model/execute_contract.go`)：
```json
{
  "speak": "...",
  "action": "continue|ask_user|confirm|next_plan|done",
  "reason": "...",
  "goal_check": "（next_plan/done 时必填）",
  "tool_call": {"name": "工具名", "arguments": {...}}
}
```

### 5.2 JSON 解析容错

`llm/json.go` 的 `ParseJSONObject` 能处理：
- LLM 在 JSON 前后附带文字 → 提取中间的 JSON 对象
- Markdown 代码块包裹（```json ... ```）→ 剥离
- 嵌套对象（大括号配对计数）

### 5.3 Correction 循环

当 LLM 输出非法 JSON 时：
```
1. 原始输出作为 assistant 消息追加到历史
2. 修正提示作为 user 消息追加到历史
3. return nil → 图循环回来
4. LLM 看到修正消息，下一轮输出合法 JSON
5. ConsecutiveCorrections 重置为 0
6. 连续 3 次失败 → 硬错误终止
```

---

## 六、工具系统

### 6.1 数据模型

`ScheduleState` 是工具操作的唯一数据源：

```
ScheduleState
├── Window                    时间窗口
│   ├── TotalDays             总天数（如 5 或 7）
│   └── DayMapping[]          day_index → (week, day_of_week) 映射
└── Tasks[]                   扁平任务列表
    ├── source="event"        来自日程表的已有课程/任务
    │   ├── Slots[]           压缩的时段范围
    │   ├── CanEmbed          是否允许嵌入
    │   └── Locked            是否锁定（不可移动）
    └── source="task_item"    来自任务类的待安排任务
        ├── Duration          需要的连续时段数
        ├── CategoryID        所属 TaskClass.ID
        └── Status="pending"  待安排
```

### 6.2 10 个工具

**读工具（直接执行，不需要确认）**：

| 工具 | 用途 | 典型调用时机 |
|------|------|------------|
| `get_overview` | 全局概览：天数、占用统计、可嵌入、待安排 | LLM 需要了解全局 |
| `query_range` | 查询指定天/时段的详情 | LLM 需要具体位置信息 |
| `find_free` | 查找连续空闲时段 | LLM 需要找空位放任务 |
| `list_tasks` | 按条件列出任务 | LLM 需要筛选任务 |
| `get_task_info` | 单个任务详情（含嵌入关系） | LLM 需要具体任务信息 |

**写工具（需用户确认）**：

| 工具 | 用途 | 关键逻辑 |
|------|------|---------|
| `place` | 放置 pending 任务到时段 | 自动检测嵌入（CanEmbed=true 的宿主） |
| `move` | 移动已有任务到新位置 | 冲突检测（排除自身） |
| `swap` | 交换两个等时长任务的时段 | 冲突时自动回滚 |
| `batch_move` | 批量移动多个任务 | 原子性：任一冲突全部回滚 |
| `unplace` | 取消放置，恢复 pending | 清理双向嵌入关系 |

### 6.3 工具执行流程

**读工具**（action=continue + tool_call）：
```
Execute → executeToolCall() → registry.Execute() → 追加 assistant+tool 消息对 → return nil → 图循环
```

**写工具**（action=confirm）：
```
Execute → handleExecuteActionConfirm() → 暂存 PendingConfirmTool
→ Confirm 节点 → Interrupt → 用户确认
→ Chat(resume) → 恢复 PendingConfirmTool
→ Execute → executePendingTool() → registry.Execute() + persistor + 追加消息对
```

### 6.4 持久化路径

```
工具执行成功
  → DiffScheduleState(original, modified) → []ScheduleChange
  → PersistScheduleChanges(事务)
    → applyPlaceChange / applyMoveChange / applyUnplaceChange
```

**当前限制**：`applyPlaceChange` 只处理 `source="event"`，`source="task_item"` 会报错。详见 ROADMAP.md P0 缺口。

---

## 七、SSE 输出系统

### 7.1 ChunkEmitter

所有节点通过 `ChunkEmitter` 向前端推送事件：

```
EmitPseudoAssistantText()  → 伪流式文本（分段推送，模拟打字效果）
EmitStatus()                → 状态推送（"正在执行第2步"）
EmitConfirmRequest()        → 确认卡片
EmitFinish() / EmitDone()   → 结束标记
```

### 7.2 伪流式

LLM 的一次性文本输出通过 `SplitPseudoStreamText` 拆分成多个 chunk：
- 按中英文标点断句
- 每个 chunk 8~24 个字符
- 间隔 40ms 推送

### 7.3 OpenAI 兼容格式

`stream/openai.go` 定义了 OpenAI 兼容的 SSE 格式，通过 `ext` 字段扩展：
- `reasoning_text`：思考过程
- `assistant_text`：正文
- `status`：状态更新
- `tool_call` / `tool_result`：工具调用
- `confirm_request`：确认卡片
- `interrupt`：中断消息

---

## 八、持久化模型

### 8.1 三个持久化层次

| 层级 | 机制 | 何时触发 | 存什么 |
|------|------|---------|--------|
| 快照 | AgentStateStore (Redis) | Plan/Confirm/Execute 节点后 | AgentRuntimeState + ConversationContext |
| 变更 | SchedulePersistor (MySQL) | 写工具执行后 | ScheduleState 的 diff |
| 历史 | Redis + MySQL | 图运行完成后 | 完整对话历史 |

### 8.2 快照恢复流程

```
用户发送新消息（图需要从中断恢复）
  → loadOrCreateRuntimeState()
    → StateStore.Load(conversationID)
    → 如果存在：恢复 RuntimeState + ConversationContext
    → 如果不存在：创建全新状态
```

快照在 Deliver 后被 `deleteAgentState()` 清理。

---

## 九、Prompt 体系

### 9.1 prompt 构造模式

所有阶段共享 `buildStageMessages()` 函数：

```
System Prompt（节点专属）
    │
    v
Pinned Blocks（置顶上下文块，作为独立 system 消息注入）
    │
    v
Tool Schemas（工具 schema，作为独立 system 消息注入）
    │
    v
History（对话历史，Tool 消息降级为 User 消息以兼容 API）
    │
    v
User Prompt（节点专属用户提示）
```

### 9.2 各阶段 prompt 要点

| 阶段 | 核心指令 | 关键约束 |
|------|---------|---------|
| Chat | 分类意图：chat vs task | 保守默认为 task |
| Plan | 两阶段：快速评估 + 深度规划 | 简单任务不开启 thinking |
| Execute | ReAct：思考→执行→观察 | goal_check 为 next_plan/done 必填 |
| Deliver | 总结计划执行结果 | 失败降级到机械格式化 |

### 9.3 置顶上下文块

```
PinnedBlocks 是跨节点共享的上下文，通过 Key 去重：

execution_context  ← Execute 节点注入（当前步骤、完成判定等）
plan                ← Plan 节点注入（完整计划文本）
tool_summary        ← Execute 节点注入（可用工具摘要）
```

---

## 十、图路由逻辑 (`graph/common_graph.go`)

路由函数是图的核心控制逻辑，决定了每步之后走向哪个节点：

### branchAfterChat
```
if PendingInteraction → Interrupt
else switch Phase:
  planning → Plan
  executing → Execute
  done → Deliver
  chatting → END
```

### branchAfterPlan
```
if PendingInteraction → Interrupt
else switch Phase:
  waiting_confirm → Confirm
  planning → Plan（continue，继续规划）
  executing → Execute（不应该发生，但防御性路由）
```

### branchAfterConfirm
```
if PendingInteraction → Interrupt
else → Execute（确认通过）
```

### branchAfterExecute
```
if PendingInteraction → Interrupt
else switch Phase:
  executing → Execute（继续循环）
  done → Deliver
  waiting_confirm → Confirm（不应该发生，防御性路由）
```

### 关键保护机制

所有分支函数都以 `branchIfInterrupted()` 开头：
```go
func branchIfInterrupted(st *AgentGraphState) string {
    if st.RuntimeState.HasPendingInteraction() {
        return "interrupt"
    }
    return ""
}
```
这确保任何节点设置了 PendingInteraction 后，图都会走向 Interrupt 节点展示给用户。

---

## 十一、Service 集成层 (`service/agentsvc/agent_newagent.go`)

### 入口函数：runNewAgentGraph

```
1. 规范化 conversationID, modelName
2. 确保会话存在（Redis 缓存 → DB）
3. 构建重试元数据
4. 加载或创建 RuntimeState（从 Redis 快照恢复）
5. 构建 AgentGraphRequest（ConfirmAction 从 extra 取）
6. 包装 Ark 客户端
7. 创建 SSE 适配器 + ChunkEmitter
8. 组装 AgentGraphDeps（注入所有依赖）
9. 调用 RunAgentGraph()
10. 持久化对话历史到 Redis + MySQL
11. 发送 [DONE] 标记，触发异步标题生成
```

### 依赖注入

```
cmd/start.go:
  → NewScheduleProvider(scheduleDAO, taskClassDAO)  → SetScheduleProvider()
  → NewSchedulePersistorAdapter(repoManager)         → SetSchedulePersistor()
  → NewDefaultRegistry()                              → SetToolRegistry()
  → NewRedisStateStore(cacheDAO)                      → SetAgentStateStore()
```

---

## 十二、如何调试

### 12.1 日志关键字

| 搜索关键字 | 含义 |
|-----------|------|
| `[DEBUG] execute LLM` | Execute 节点的 LLM 原始输出和解析结果 |
| `[DEBUG] plan LLM` | Plan 节点的 LLM 输出 |
| `[WARN] execute 决策不合法` | LLM 输出合法 JSON 但 action 不合法 |
| `[DEBUG] execute LLM 输出解析失败` | JSON 解析失败，触发 correction |
| `PersistScheduleChanges` | 持久化调用 |
| `loadOrCreateRuntimeState` | 状态恢复/创建 |

### 12.2 常见问题排查

**SSE 断开**：
1. 检查 `[DEBUG] execute LLM` 日志，看 LLM 输出是否为合法 JSON
2. 如果输出 `[NEXT_PLAN]` 等纯文本 → prompt 问题（已修复，参考 execute.go 的 correction 机制）
3. 如果输出合法 JSON 但 action 不对 → 检查 prompt 的合约文本

**工具不执行**：
1. 检查 PendingConfirmTool 是否被正确设置和恢复
2. 检查 ScheduleState 是否为 nil（可能 ScheduleProvider 未注入）
3. 检查 history 中 assistant+tool 消息是否配对（ToolCallID 是否匹配）

**图循环不退出**：
1. 检查 ConsecutiveCorrections 计数（可能 LLM 反复输出非法 JSON）
2. 检查 RoundUsed 是否耗尽（MaxRounds 默认 30）
3. 检查 Phase 是否卡在某个状态

### 12.3 单元测试

```
node/execute_confirm_flow_test.go   → 7 个测试，覆盖完整 confirm 回路
node/llm_tool_orchestration_test.go → 5 个测试，覆盖真实排课场景
```

测试使用 mock LLM（预定义 JSON 响应序列）和 mock 工具注册表，不依赖外部服务。

---

## 十三、关键设计决策及理由

| 决策 | 理由 |
|------|------|
| Phase 驱动路由而非硬编码序列 | 同一个图支持多种流程（直接聊天、排课、追问恢复），Phase 是最小状态信号 |
| PendingInteraction 作为中断快照 | 图是无状态的（每次请求重新运行），需要一种机制跨请求传递"等用户回复"的上下文 |
| PendingConfirmTool 作为临时邮箱 | Execute 和 Confirm 之间不能直接传参（中间隔了 Interrupt+END+Chat），用运行态字段传递 |
| JSON 协议而非文本标记 | LLM 输出结构化数据，后端用泛型解析，避免正则匹配的不确定性 |
| Correction 机制 | LLM 不是 100% 可靠，需要给修正机会，但限制最大连续次数避免死循环 |
| 伪流式而非真流式 | LLM API 的一次性返回更适合分段推送，真流式实现复杂且收益低 |
| 工具操作扁平 ScheduleState | 避免嵌套数据结构，工具只需关心"在哪里放什么" |
| Diff 持久化 | 只持久化变更部分，减少 DB 操作，支持原子性 |
| PinnedBlocks 注入上下文 | 计划、工具摘要等信息不需要每轮都重复，用置顶块注入一次即可 |

---

## 十四、关键文件速查

| 想了解... | 看这个文件 |
|----------|----------|
| 图怎么连的 | `graph/common_graph.go` |
| 每个节点怎么被调用的 | `node/agent_nodes.go` |
| Chat 怎么分类意图的 | `prompt/chat.go` + `node/chat.go` |
| Plan 怎么生成计划的 | `prompt/plan.go` + `node/plan.go` |
| Execute 的 ReAct 循环 | `node/execute.go` |
| confirm 回路怎么转的 | `node/confirm.go` + `node/chat.go`(handleConfirmResume) |
| LLM 输出什么格式 | `model/plan_contract.go` + `model/execute_contract.go` |
| JSON 解析怎么容错的 | `llm/json.go` |
| correction 怎么追回的 | `node/correction.go` |
| 工具怎么注册和执行的 | `tools/registry.go` |
| 工具操作什么数据 | `tools/state.go` |
| SSE 输出什么格式 | `stream/openai.go` |
| 状态怎么持久化的 | `model/state_store.go` + `conv/schedule_persist.go` |
| 日程数据怎么加载的 | `conv/schedule_provider.go` + `conv/schedule_state.go` |
| Service 怎么组装的 | `service/agentsvc/agent_newagent.go` |
| API 怎么调用的 | `api/agent.go` |
| 距离全链路还差什么 | `ROADMAP.md` |
