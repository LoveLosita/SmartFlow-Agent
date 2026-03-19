# 智能排程 Agent — ReAct 精排引擎 决策记录

## 1. 基本信息
- 记录编号：FDR-008
- 功能名称：智能排程 ReAct 精排引擎（阶段 1.5：粗排 + AI 语义化微调）
- 记录日期：2026-03-19
- 决策状态：已采纳，开发中
- 负责人：SmartFlow 团队
- 关联需求：FDR-007（智能排程 Agent 阶段 1）

## 2. 背景与问题
- 业务背景：阶段 1 已打通"意图识别 → 粗排 → 落库"全链路，但粗排算法是纯规则的线性分配（cursor-based），不考虑科目特性、学习效率曲线、上下文切换成本等语义因素。
- 现状问题：
  1. 粗排结果机械化：高认知负荷科目可能被安排在低效时段（如晚间安排数学）；
  2. 缺乏科目间协调：同类任务可能被分散到不连贯的时间段，增加上下文切换成本；
  3. 用户无法感知 AI 的"思考过程"，排程结果缺乏可解释性。
- 不做此决策的后果：排程质量停留在"能用但不好用"阶段，无法体现 AI 的语义理解能力。

## 3. 决策目标
- 目标 1：在粗排之后引入 LLM 精排环节，通过 ReAct 范式对任务时间进行语义化优化。
- 目标 2：精排过程中 LLM 的深度思考（reasoning）实时流式推送到前端，用户可见。
- 目标 3：精排结果仅作为预览返回，不自动落库，用户确认后再持久化。
- 目标 4：向后兼容——未注入精排依赖时自动走原有 materialize 路径。
- 非目标：
  - 本阶段不做用户确认后的落库链路（后续阶段）；
  - 本阶段不做 RAG 规则注入（阶段 3）；
  - 本阶段不做多方案对比（只输出一个优化后的方案）。

## 4. 备选方案

### 方案 A：后处理脚本（规则引擎）
- 描述：在粗排之后用硬编码规则（如"数学只排上午"）做二次调整。
- 优点：确定性强，无 LLM 调用开销。
- 缺点：规则维护成本高，无法处理复杂的多科目协调；不可解释。
- 复杂度 / 成本：低，但扩展性极差。

### 方案 B：ReAct 范式 + 手动 Tool 调用（采纳）
- 描述：LLM 开启深度思考，分析粗排结果后通过 Tool（Swap/Move/TimeAvailable/GetAvailableSlots）在内存中调整任务时间，多轮循环直到满意。
- 优点：
  1. 充分利用 LLM 的语义理解能力，优化维度丰富；
  2. reasoning_content 实时推送，用户可见思考过程；
  3. Tool 操作内存数据，天然支持预览模式（不落库）；
  4. 手动 ReAct 循环给予完全的流式控制权。
- 缺点：依赖 LLM 输出质量；深度思考模式耗时较长。
- 复杂度 / 成本：中高，约 1 周。

### 方案 C：Eino 内置 ToolsNode
- 描述：使用 Eino 框架的 ToolsNode + function_calling 原生能力。
- 优点：框架原生支持，代码量少。
- 缺点：无法在 Tool 执行过程中流式推送 reasoning_content；无法精细控制每轮 SSE 输出；项目中无现有 function_calling 基础设施。
- 复杂度 / 成本：中，但灵活性不足。

## 5. 最终决策
- 采纳方案：方案 B（ReAct 范式 + 手动 Tool 调用）
- 关键理由：
  1. 手动 ReAct 循环可以精确控制 SSE 流式输出（reasoning + stage + tool_call 穿插）；
  2. Tool 操作纯内存数据，预览模式零风险；
  3. 与现有 graph 架构无缝集成（新增 3 个节点，不破坏原有链路）。

## 6. 技术方案

### 6.1 新流程（graph 结构）
```
plan → preview(粗排) → hybridBuild(混合日程) → reactRefine(ReAct循环) → returnPreview → END
                              ↑                        |
                              └────────────────────────┘ (tool失败重试，最多N轮)
```
当 `HybridScheduleWithPlan` 依赖未注入时，preview 后自动走原有 materialize → apply 路径。

### 6.2 混合日程（HybridScheduleEntry）
将既有日程（existing）和粗排建议（suggested）统一到同一结构：
- `existing` + `course/task`：不可移动
- `suggested` + `task`：LLM 可通过 Tool 调整

### 6.3 ReAct Tool 设计
| Tool | 功能 | 操作对象 |
|------|------|---------|
| Swap | 交换两个 suggested 任务的时间 | 内存 HybridEntries |
| Move | 移动一个 suggested 任务到新时间 | 内存 HybridEntries |
| TimeAvailable | 检查目标时间是否可用 | 只读查询 |
| GetAvailableSlots | 返回可用时间段列表 | 只读查询 |

### 6.4 SSE 推送设计
- `schedule_plan.hybrid.building/done` — 混合日程构建阶段
- `schedule_plan.react.round` — 第 N 轮优化开始
- `reasoning_content` 流式 chunk — LLM 深度思考过程（实时推送）
- `schedule_plan.react.tool_call` — Tool 执行结果
- `schedule_plan.react.done` — 优化完成
- `schedule_plan.preview_return.done` — 预览生成完成

## 7. 影响范围
- 新增文件：
  - `backend/agent/scheduleplan/tools_react.go`：4 个 Tool + dispatcher + LLM 输出解析
  - `backend/agent/scheduleplan/react.go`：ReAct 循环核心 + 流式推送
- 修改文件：
  - `backend/model/schedule.go`：+HybridScheduleEntry
  - `backend/agent/scheduleplan/state.go`：+ReAct 字段
  - `backend/agent/scheduleplan/prompt.go`：+ReAct system prompt
  - `backend/agent/scheduleplan/nodes.go`：+hybridBuild/returnPreview 节点
  - `backend/agent/scheduleplan/runner.go`：+outChan/modelName/新节点适配
  - `backend/agent/scheduleplan/graph.go`：+3 节点/重新连线
  - `backend/agent/scheduleplan/tool.go`：+HybridScheduleWithPlan 依赖
  - `backend/service/schedule.go`：+HybridScheduleWithPlan 方法
  - `backend/service/agent_bridge.go`：+注入新依赖
  - `backend/service/agentsvc/agent.go`：+字段/传参
  - `backend/service/agentsvc/agent_schedule_plan.go`：+outChan/modelName/新依赖
- 数据与存储影响：无。所有 Tool 操作纯内存，不涉及 DB。
- 接口 / 协议影响：无新增接口。SSE 新增 react 相关阶段推送（向下兼容）。

## 8. 已知问题与后续优化

### 8.1 深度思考超时（当前）
- 现象：模型开启深度思考后 reasoning 阶段耗时较长，当前 5 分钟超时仍可能不够。
- 影响：超时后使用粗排结果，精排未生效。
- 后续方案：
  - [ ] 调整超时策略（按模型实际耗时动态设置，或改为不设超时由父 context 控制）
  - [ ] 优化 prompt，引导模型减少冗余推理
  - [ ] 评估是否关闭深度思考，改用普通模式 + 多轮调用换取稳定性

### 8.2 模型输出质量
- 现象：模型思考过程较啰嗦，可能输出无效的 tool 调用。
- 后续方案：
  - [ ] 精炼 ReAct system prompt，加入 few-shot 示例
  - [ ] 对 tool_calls 做预校验，过滤明显无效的调用
  - [ ] 收集真实案例建立评测集

### 8.3 用户确认落库链路
- 现象：当前精排结果仅预览，用户确认后的落库链路尚未实现。
- 后续方案：
  - [ ] 新增"确认落库"接口或对话指令
  - [ ] 复用现有 materialize → apply 路径，从 HybridEntries 转换

### 8.4 连续对话微调
- 现象：精排后的连续对话微调（如"把数学挪到上午"）尚未与 ReAct 引擎打通。
- 后续方案：
  - [ ] 将上一轮 HybridEntries 序列化到对话历史
  - [ ] 支持增量 ReAct（只调整用户指定的部分）

## 9. 验证与回滚
- 验证方式：
  1. `go build ./...` + `go vet ./...` 编译通过
  2. 发送排程请求，验证 SSE 流中出现 react 阶段推送和 reasoning_content
  3. 验证不落库：数据库 schedules 表无新增记录
  4. 向后兼容：不注入 HybridScheduleWithPlan 时走原有 materialize 路径
- 回滚方案：在 `agent_bridge.go` 中注释掉 `HybridScheduleWithPlanFunc` 注入即可，preview 后自动回退到 materialize 路径。

## 10. 复盘结论（上线后补充）
- 实际效果：待补充
- 与预期偏差：待补充
- 后续是否需要二次决策：待补充
