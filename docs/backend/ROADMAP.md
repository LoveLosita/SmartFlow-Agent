# NewAgent 全链路改造计划

> 本文档面向后续 coding agent，描述当前 newAgent 的架构现状、改造计划，以及距离"会主动问用户问题、生成多个任务类、自己 ReAct 排日程的智能体"还差什么。

---

## 一、目标智能体行为

用户说："帮我安排下周的复习计划"。

期望的完整链路：

```
1. Chat 节点：LLM 主动追问
   - "这周有几门考试？"
   - "复习强度偏好？均匀分布还是集中在前几天？"
   - "有没有要排除的时段？"

2. Plan 节点：LLM 生成结构化计划
   - 识别意图为"批量安排任务类到日程"
   - 输出 needs_rough_build=true + task_class_ids
   - 或者：LLM 调用 create_task_class 工具创建新的任务类

3. Confirm 节点：展示计划，用户确认

4. RoughBuild 节点（新增）：确定性粗排
   - 调用 SmartPlanningRawItemsMulti() 算法
   - 结果写入 ScheduleState（pending tasks 预填 suggested slots）

5. Execute 节点：LLM 用读写工具微调
   - 查看 get_overview，发现粗排结果
   - 用 move/swap 调整不合理的安排
   - 用 place 处理粗排未能安排的任务
   - 每次写操作经 confirm 流程

6. Deliver 节点：生成最终总结
   - 变更持久化到 DB
   - 向用户展示排课结果
```

---

## 二、当前架构

### 图结构

```
Chat → Plan → Confirm → Execute(ReAct) → Deliver
```

### 已实现的能力

| 模块 | 文件 | 状态 |
|------|------|------|
| 图骨架 | `node/agent_nodes.go` | 已实现，6 个节点 |
| Chat 节点 | `node/chat.go` | 已实现，支持 confirm resume |
| Plan 节点 | `node/plan.go` + `prompt/plan.go` | 已实现，LLM 生成结构化 PlanStep |
| Confirm 节点 | `node/confirm.go` | 已实现，创建 PendingInteraction |
| Execute 节点 | `node/execute.go` + `prompt/execute.go` | 已实现，ReAct + correction + confirm 流 |
| Deliver 节点 | `node/deliver.go` | 已实现，LLM 生成总结 |
| 10 个读写工具 | `tools/read_tools.go` + `tools/write_tools.go` | 已实现，5 读 5 写 |
| 工具注册表 | `tools/registry.go` | 已实现 |
| ScheduleState 加载 | `conv/schedule_provider.go` + `conv/schedule_state.go` | 已实现，从 DB 加载日程+任务类 |
| Confirm 回路测试 | `node/execute_confirm_flow_test.go` | 7 个测试全通过 |
| 端到端排课测试 | `node/llm_tool_orchestration_test.go` | 5 个测试全通过 |
| JSON 协议修正 | `prompt/execute.go` | 已修复，LLM 输出严格 JSON |

### 已有数据流

```
ScheduleProvider.LoadScheduleState(userID)
  → ScheduleDAO.GetUserWeeklySchedule()     // 现有日程
  → TaskClassDAO.GetCompleteTaskClassesByIDs() // 任务类（含 Items）
  → LoadScheduleState()                      // 合并为 ScheduleState
     - existing tasks (source="event")
     - pending tasks (source="task_item", status="pending")
```

---

## 三、缺口分析

### 按优先级排列

#### P0：粗排接入（核心能力）

**问题**：新 agent 没有 `SmartPlanningRawItemsMulti` 的调用路径。让 LLM 一个个 place 效率极低且全局最优性差。

**改造内容**：

1. **Plan 节点输出扩展**
   - 文件：`model/plan_contract.go`（或 PlanDecision 所在文件）
   - PlanDecision 增加 `NeedsRoughBuild bool` 和 `TaskClassIDs []int`
   - `prompt/plan.go` 引导 LLM 判断意图：
     - 用户意图为"批量安排/智能排课/把任务类排进日程" → `needs_rough_build: true`
     - 从前端 `extra` 或对话中提取 `task_class_ids`
     - 其他意图 → `needs_rough_build: false`

2. **新增 RoughBuild 图节点**
   - 新文件：`node/rough_build.go`
   - 不调 LLM，纯确定性逻辑：
     ```
     输入：task_class_ids, userID
     步骤：
       1. 调 ScheduleService.HybridScheduleWithPlanMulti(ctx, userID, taskClassIDs)
          （内部调用 SmartPlanningRawItemsMulti 粗排）
       2. 将粗排结果写入 ScheduleState：
          - pending tasks 的 Slots 字段填入 suggested 位置
          - pending tasks 的 Status 保持 "pending"（LLM 可调整，也可以改为 "suggested" 区分）
       3. 推送状态给前端
     输出：ScheduleState 已填充粗排结果
     ```
   - 路由：`needs_rough_build=true` 时 Confirm 之后走 RoughBuild，否则跳过

3. **图路由修改**
   - 文件：`node/agent_nodes.go`
   - Confirm 之后、Execute 之前插入条件分支：
     ```go
     func (n *AgentNodes) branchAfterConfirm(st *AgentGraphState) string {
         plan := st.RuntimeState.PlanDecision
         if plan != nil && plan.NeedsRoughBuild {
             return "rough_build"
         }
         return "execute"
     }
     ```

4. **依赖注入**
   - 文件：`model/graph_run_state.go` AgentGraphDeps
   - 新增 `RoughBuildFunc` 闭包（和旧 agent 的 `HybridScheduleWithPlanMultiFunc` 同理）
   - 文件：`service/agentsvc/agent_newagent.go`
   - 注入 `ScheduleService.HybridScheduleWithPlanMulti` 到 AgentGraphDeps

**参考实现**：旧 agent 的 `agent/node/schedule_plan.go` 的 `runRoughBuildNode()` 函数。

---

#### P0：持久化 task_item 放置（必须）

**问题**：`conv/schedule_persist.go` 的 `applyPlaceChange` 只处理 `source="event"`，当 LLM place 一个 `source="task_item"` 的 pending 任务时会报错。

**改造内容**：

- 文件：`conv/schedule_persist.go`
- `applyPlaceChange` 增加 `source="task_item"` 分支：
  ```
  if source == "task_item":
    1. 创建 ScheduleEvent(type="task", rel_id=SourceID, name=change.Name)
    2. 为每个 NewCoord 创建 Schedule 记录
    3. 如果有嵌入关系（EmbedHost 非空）：
       - 找到宿主 schedule 记录
       - 设置 schedules.embedded_task_id = SourceID
    4. 更新 task_items.embedded_time（调用 TaskClassDAO.UpdateTaskClassItemEmbeddedTime）
    5. 更新 task_items.status = 2 (applied)
  ```
- `applyUnplaceChange` 也需要增加 `source="task_item"` 分支（反向清理）

**参考实现**：旧 agent 的 `service/task-class.go` 的 `BatchApplyPlans()` 函数（第 327-536 行）。

---

#### P1：TaskClass 约束元数据暴露给 LLM

**问题**：LLM 看到的 pending task 只有 name/category/duration，缺少 TaskClass 级别的调度约束。

**改造内容**：

1. **扩展 ScheduleState 结构**
   - 文件：`newAgent/tools/state.go`
   - 新增：
     ```go
     type TaskClassMeta struct {
         ID                int    `json:"id"`
         Name              string `json:"name"`
         Strategy          string `json:"strategy"`            // "steady" | "rapid"
         ExcludedSlots     []int  `json:"excluded_slots"`      // 排除的半天时段索引
         AllowFillerCourse bool   `json:"allow_filler_course"` // 是否允许嵌入水课
         TotalSlots        int    `json:"total_slots"`         // 总时间预算
     }
     ```
   - `ScheduleState` 增加 `TaskClasses []TaskClassMeta`

2. **LoadScheduleState 填充元数据**
   - 文件：`conv/schedule_state.go`
   - 在 Step 4（处理 pending task items）同时填充 `state.TaskClasses`

3. **读工具输出约束信息**
   - 文件：`tools/read_tools.go`
   - `get_overview` 输出中增加任务类约束描述
   - 示例：
     ```
     任务类约束：
     [学习] 策略=均匀分布, 总预算=12节, 允许嵌入水课=是, 排除时段=[3,4]
     ```

---

#### P2：任务类 ID 从前端传入

**问题**：前端请求需要在 `extra` 中传递 `task_class_ids`，后端需要接收并传递到图内部。

**改造内容**：

1. **API 层**：`api/agent.go` 的请求结构增加 `Extra map[string]any`
2. **Service 层**：`service/agentsvc/agent_newagent.go` 从 `extra` 中提取 `task_class_ids`，存入 RuntimeState 或 AgentGraphRequest
3. **Plan 节点**：从 RuntimeState/Request 中读取 `task_class_ids`，合并 LLM 从对话中提取的 IDs

**参考实现**：旧 agent 的 `agent/node/schedule_plan.go` 的 `normalizeTaskClassIDs()` 函数。

---

#### P3：LLM 主动追问能力增强

**问题**：当前 Chat 节点主要做"接收用户消息 + confirm resume"，缺少"LLM 主动收集排课需求"的能力。

**改造内容**：

- Chat 节点的 prompt 增强：
  - 引导 LLM 在信息不足时主动追问
  - 追问内容：考试科目、复习偏好、时段排除、强度偏好
  - 追问方式：通过 `ask_user` action 或直接在 speak 中提问
- 可能需要新增 ConversationContext 的"收集到的需求"字段
- 收集到的需求在 Plan 节点中被使用

---

#### P4：LLM 创建任务类工具（锦上添花）

**问题**：用户说"帮我安排复习"，但系统里没有对应的 TaskClass，LLM 无法创建。

**改造内容**：

1. **新增工具**：`create_task_class`
   - 参数：`name`, `strategy`, `total_slots`, `allow_filler_course`, `items: [{content, duration}]`
   - 行为：调用 TaskClassDAO 在 DB 中创建 TaskClass + Items
   - 返回：创建结果 + 新的 task_class_id

2. **新增工具**：`update_task_class`（可选）
   - 修改已有任务类的参数

3. **ScheduleState 动态刷新**
   - 创建后需要重新加载 ScheduleState 以反映新的 pending tasks

---

## 四、改造顺序建议

```
Phase 1（打通核心链路）
  ├── P0: 粗排接入（RoughBuild 节点 + 图路由 + 依赖注入）
  ├── P0: 持久化 task_item 放置
  └── P2: task_class_ids 从前端传入

Phase 2（提升排课质量）
  ├── P1: TaskClass 约束元数据暴露
  └── P1: 读工具输出优化（携带约束信息）

Phase 3（智能化）
  ├── P3: Chat 节点追问能力增强
  └── P4: create_task_class 工具
```

---

## 五、关键设计决策记录

1. **粗排由图节点驱动，不由 LLM 驱动**：粗排是确定性算法，浪费 LLM 调用不划算。
2. **粗排通过 Plan 节点的 `needs_rough_build` 标签触发**：不是所有请求都需要粗排，LLM 判断意图后打标签。
3. **粗排结果写入 ScheduleState 的 Slots 字段**：LLM 在 Execute 阶段看到的是"已粗排、可调整"的状态，用 move/swap 微调。
4. **task_class_ids 来源**：前端 `extra` 传入为主，LLM 从对话提取为辅。
5. **持久化用 Diff 模式**：对比 original 和 modified ScheduleState，只持久化变更部分。

---

## 六、关键文件索引

| 用途 | 文件 |
|------|------|
| 图骨架与路由 | `newAgent/node/agent_nodes.go` |
| Chat 节点 | `newAgent/node/chat.go` |
| Plan 节点 | `newAgent/node/plan.go` |
| Confirm 节点 | `newAgent/node/confirm.go` |
| Execute 节点 | `newAgent/node/execute.go` |
| Deliver 节点 | `newAgent/node/deliver.go` |
| Execute prompt | `newAgent/prompt/execute.go` |
| Plan prompt | `newAgent/prompt/plan.go` |
| 工具状态模型 | `newAgent/tools/state.go` |
| 读工具 | `newAgent/tools/read_tools.go` |
| 写工具 | `newAgent/tools/write_tools.go` |
| 工具注册表 | `newAgent/tools/registry.go` |
| 图运行态 | `newAgent/model/graph_run_state.go` |
| 公共状态 | `newAgent/model/common_state.go` |
| 日程状态加载 | `conv/schedule_provider.go` |
| DB→State 转换 | `conv/schedule_state.go` |
| Diff 算法 | `conv/schedule_state.go` (DiffScheduleState) |
| 持久化 | `conv/schedule_persist.go` |
| Service 集成 | `service/agentsvc/agent_newagent.go` |
| 粗排算法（旧，复用） | `logic/smart_planning.go` |
| 旧 agent 粗排节点（参考） | `agent/node/schedule_plan.go` |
| 旧 agent 批量应用（参考） | `service/task-class.go` BatchApplyPlans |
