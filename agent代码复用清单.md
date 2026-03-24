# Agent 代码复用清单（施工备忘）

## 1. 文档目的
- 这份文档只做一件事：记录当前 `backend/agent` 与 `backend/service/agentsvc` 中“明明可以复用、却被重复实现”的代码点。
- 目标不是立刻改代码，而是防止后续压缩上下文后忘记哪些地方最值得先收口。
- 这里优先关注“公共能力重复实现”，不讨论具体业务对错。

## 2. 当前最明显的重复结论
- 重复最严重的不是某一个 skill，而是“每个 skill 都在各自实现一套模型调用、JSON 解析、阶段推送、兜底、深拷贝、缓存快照读写、工具分发”。
- `quicknote`、`taskquery`、`scheduleplan`、`schedulerefine` 已经形成了“四套相似但不统一的 agent 基建”。
- `service/agentsvc` 里也开始出现重复胶水层逻辑，尤其是排程预览和连续微调相关读写。

---

## 3. 高优先级复用点

## 3.1 模型调用封装重复

### 现状
- `quicknote` 自己实现了 `callModelForJSON` / `callModelForJSONWithMaxTokens`。
- `taskquery` 自己实现了 `callTaskQueryModelForJSON`。
- `scheduleplan` 自己实现了 `callScheduleModelForJSON`。
- `schedulerefine` 自己实现了 `callModelText`，而且附带了一整套 JSON contract 变体。

### 证据
- [quicknote/nodes.go](E:/SmartFlow-Agent/backend/agent/quicknote/nodes.go)
- [taskquery/nodes.go](E:/SmartFlow-Agent/backend/agent/taskquery/nodes.go)
- [scheduleplan/nodes.go](E:/SmartFlow-Agent/backend/agent/scheduleplan/nodes.go)
- [schedulerefine/nodes.go](E:/SmartFlow-Agent/backend/agent/schedulerefine/nodes.go)

### 问题
- 都在做同一类事情：
  - 拼 `system + user` 消息
  - 关闭/开启 thinking
  - 设置 `temperature/maxTokens`
  - 调 `Generate`
  - 判空响应
  - 返回文本
- 现在每个模块一套，导致：
  - 参数风格不统一
  - 错误文案不统一
  - token 统计与 callback 复用也难进一步规范

### 建议抽取
- 抽到统一的 `agent/core/modelx` 或类似目录。
- 至少统一三个入口：
  - `GenerateText`
  - `GenerateJSON`
  - `GenerateJSONWithContract`

### 优先级
- `P0`

---

## 3.2 JSON 解析与对象提取重复

### 现状
- `quicknote` 自己实现了 `parseJSONPayload` + `extractJSONObject`。
- `taskquery` 有自己的 `parseTaskQueryJSON`。
- `scheduleplan` 有自己的 `parseScheduleJSON`。
- `schedulerefine` 有自己的 `parseJSON`，并且带重试解析逻辑。

### 证据
- [quicknote/nodes.go](E:/SmartFlow-Agent/backend/agent/quicknote/nodes.go)
- [taskquery/nodes.go](E:/SmartFlow-Agent/backend/agent/taskquery/nodes.go)
- [scheduleplan/nodes.go](E:/SmartFlow-Agent/backend/agent/scheduleplan/nodes.go)
- [schedulerefine/nodes.go](E:/SmartFlow-Agent/backend/agent/schedulerefine/nodes.go)

### 问题
- 都在解决“模型返回不是纯 JSON，需要做容错提取”。
- 解析失败后的兜底策略也高度相似，只是文案不同。

### 建议抽取
- 抽统一的 `agent/core/jsonx`：
  - `ParseJSONObject[T]`
  - `ExtractJSONObject`
  - `ParseWithRetryHint`（后续可选）

### 优先级
- `P0`

---

## 3.3 阶段推送（EmitStage）重复

### 现状
- `quicknote/graph.go` 自己封装 `EmitStage` 判空。
- `taskquery/graph.go` 自己写 `runner.emit`。
- `scheduleplan/graph.go` 自己包一层 `emitStage`。
- `schedulerefine/graph.go` 也自带一套。

### 证据
- [quicknote/graph.go](E:/SmartFlow-Agent/backend/agent/quicknote/graph.go)
- [taskquery/graph.go](E:/SmartFlow-Agent/backend/agent/taskquery/graph.go)
- [scheduleplan/graph.go](E:/SmartFlow-Agent/backend/agent/scheduleplan/graph.go)
- [schedulerefine/graph.go](E:/SmartFlow-Agent/backend/agent/schedulerefine/graph.go)

### 问题
- 所有 graph 都在做相同事情：封装一个 `func(stage, detail string)`，把 nil 判断藏起来。
- 这一层目前很轻，但 skill 一多就会持续复制。

### 建议抽取
- 抽统一的 `agent/core/progress`：
  - `type Emitter func(stage, detail string)`
  - `func WrapEmitter(fn func(string, string)) Emitter`
  - `func NoopEmitter() Emitter`

### 优先级
- `P1`

---

## 3.4 OpenAI 兼容流式包装能力重复调用方式不统一

### 现状
- 统一实现其实已经有了，在 [chat/stream.go](E:/SmartFlow-Agent/backend/agent/chat/stream.go) 里：
  - `ToOpenAIStream`
  - `ToOpenAIFinishStream`
- 但 `agentsvc` 侧又额外自己实现了“阶段推送器”和“单条 assistant completion 包装”。

### 证据
- [chat/stream.go](E:/SmartFlow-Agent/backend/agent/chat/stream.go)
- [agent_quick_note.go](E:/SmartFlow-Agent/backend/service/agentsvc/agent_quick_note.go)

### 问题
- 现在虽然底层函数复用了，但“如何构造阶段消息 / 一次性正文 / finish chunk”的协议层仍散落在 service 层。
- 后续 schedule、memory、websearch 也大概率继续复制。

### 建议抽取
- 抽统一的 `agent/core/streamx`：
  - `EmitReasoningStage`
  - `EmitSingleAssistantReply`
  - `EmitErrorChunk`

### 优先级
- `P1`

---

## 3.5 深拷贝函数重复

### 现状
- `agentsvc/agent_schedule_preview.go` 有：
  - `cloneWeekSchedules`
  - `cloneHybridEntries`
  - `cloneTaskClassItems`
- `schedulerefine/state.go` 又复制了一份同类深拷贝函数。
- `scheduleplan` 侧也多处依赖同样的拷贝语义。

### 证据
- [agent_schedule_preview.go](E:/SmartFlow-Agent/backend/service/agentsvc/agent_schedule_preview.go)
- [schedulerefine/state.go](E:/SmartFlow-Agent/backend/agent/schedulerefine/state.go)

### 问题
- 这是非常典型的“纯工具函数”重复实现。
- 后面只要结构体字段一变，就容易出现一边更新一边漏更。

### 建议抽取
- 抽统一的 `agent/core/clone` 或 `agent/core/snapshot`。

### 优先级
- `P0`

---

## 3.6 预览快照读取/回填逻辑重复

### 现状
- `runSchedulePlanFlow` 自己在做：
  - 查 Redis 预览
  - miss 后查 MySQL snapshot
  - 回填 Redis
  - 清旧 preview
- `runScheduleRefineFlow` 又通过 `loadSchedulePreviewContext` 再做一套类似的逻辑。
- `GetSchedulePlanPreview` 也重复做：
  - 查 Redis
  - miss 查 MySQL
  - 回填 Redis

### 证据
- [agent_schedule_plan.go](E:/SmartFlow-Agent/backend/service/agentsvc/agent_schedule_plan.go)
- [agent_schedule_refine.go](E:/SmartFlow-Agent/backend/service/agentsvc/agent_schedule_refine.go)
- [agent_schedule_preview.go](E:/SmartFlow-Agent/backend/service/agentsvc/agent_schedule_preview.go)

### 问题
- 这是同一个“排程预览仓储语义”，却被 service 层复制成了三种读法。
- 长期会导致缓存行为不一致。

### 建议抽取
- 抽成统一 `schedule preview repository/gateway`：
  - `LoadPreviewContext`
  - `SavePreviewContext`
  - `DeletePreviewContext`
  - `LoadPreviewForRead`

### 优先级
- `P0`

---

## 3.7 fallback 文案与早退逻辑重复

### 现状
- `quicknote` 有自己的 fallback reply / persisted 判定。
- `taskquery` 有 `buildTaskQueryFallbackReply`。
- `scheduleplan` 和 `schedulerefine` 里也散落大量“解析失败/模型失败/回退继续”的逻辑。

### 证据
- [quicknote/nodes.go](E:/SmartFlow-Agent/backend/agent/quicknote/nodes.go)
- [taskquery/nodes.go](E:/SmartFlow-Agent/backend/agent/taskquery/nodes.go)
- [scheduleplan/nodes.go](E:/SmartFlow-Agent/backend/agent/scheduleplan/nodes.go)
- [schedulerefine/nodes.go](E:/SmartFlow-Agent/backend/agent/schedulerefine/nodes.go)

### 问题
- 不是所有 fallback 文案都能共用，但 fallback 模式本身是共性的：
  - 模型失败 -> 本地兜底
  - JSON 失败 -> 本地兜底
  - 工具失败 -> 重试 / 降级

### 建议抽取
- 不建议直接抽“文案”。
- 但建议抽“失败策略框架”：
  - `FallbackPolicy`
  - `RetryOrFallback`
  - `ParseOrFallback`

### 优先级
- `P1`

---

## 3.8 时间解析能力重复 / 分散

### 现状
- `quicknote/tool.go` 内有一整套时间解析：
  - 相对时间
  - 中文时间
  - deadline hint
  - 默认时刻
- 排程链路后续也非常可能要继续用类似时间语义。

### 证据
- [quicknote/tool.go](E:/SmartFlow-Agent/backend/agent/quicknote/tool.go)

### 问题
- 目前还没出现“多份复制”，但这是非常明确的“已形成公共能力、却还放在 skill 私有目录”的情况。
- 如果不提前抽，schedule / memory / websearch 接进来后一定复制。

### 建议抽取
- 提前迁出到 `agent/core/timeparse`。

### 优先级
- `P1`

---

## 3.9 runner 形态重复

### 现状
- `quicknote/runner.go`
- `scheduleplan/runner.go`
- `schedulerefine/runner.go`

### 问题
- 本质都在做同一件事：把 graph 输入依赖注入到每个步骤函数。
- 结构相似，但没有统一约定。

### 建议抽取
- 不一定要抽通用基类。
- 但至少要统一 runner 模板风格，减少 skill 间阅读切换成本。

### 优先级
- `P2`

---

## 3.10 route 控制码能力与 skill 判定方式分裂

### 现状
- 统一路由已经在 [route/route.go](E:/SmartFlow-Agent/backend/agent/route/route.go)。
- 但 skill 内部仍有不少二次意图确认 / fallback 判定。

### 问题
- 这不是完全重复代码问题，而是“判定责任分裂”。
- 结果是：
  - route 做一遍
  - skill 首节点再做一遍
  - service 再决定回不回聊天

### 建议抽取
- 统一成：
  - `route` 只做一级分流
  - skill 只做 skill 内业务判定
  - service 不再写 skill 特有 fallback 判定

### 优先级
- `P1`

---

## 4. 中优先级复用点

## 4.1 Extra 参数解析工具只在 scheduleplan 私有

### 现状
- `scheduleplan/tool.go` 有 `ExtraInt`、`ExtraIntSlice`。

### 问题
- 这明显属于通用 agent 请求参数解析能力，不该锁死在某个 skill 下。

### 建议抽取
- 抽到 `agent/core/extrax`。

### 优先级
- `P2`

---

## 4.2 工具分发 dispatch 逻辑重复

### 现状
- `scheduleplan/tools_react.go` 有 `dispatchReactTool`、`dispatchWeeklySingleActionTool`。
- `scheduleplan/daily_refine.go` 有 `dispatchDailyReactTool`。
- `schedulerefine/tool.go` 有 `dispatchRefineTool`。

### 问题
- 都在做“执行工具调用 -> 应用结果 -> 返回标准结果”的模式。
- 虽然业务不同，但骨架高度类似。

### 建议抽取
- 抽公共的 `tool dispatcher` 框架，业务只实现 apply。

### 优先级
- `P2`

---

## 4.3 preview/snapshot DTO 映射重复

### 现状
- `snapshotToSchedulePlanPreviewCache`
- `snapshotToSchedulePlanPreviewResponse`
- `buildSchedulePlanSnapshotFromState`
- `convertRefineStateToPlanState`

### 问题
- 同一批结构在 service 层到处转来转去。
- 说明“排程预览状态”还没有独立成一个稳定模型层。

### 建议抽取
- 抽 `schedule snapshot mapper`。

### 优先级
- `P2`

---

## 5. 已经出现“文件承担过多职责”的区域

### 5.1 `schedulerefine/nodes.go`
- 3140 行。
- 当前同时承担：
  - 模型调用
  - JSON 解析
  - 规则归一化
  - planner
  - react loop
  - hard check
  - summary
- 这是最先需要拆的文件。

### 5.2 `schedulerefine/tool.go`
- 1768 行。
- 当前同时承担：
  - tool 定义
  - tool 分发
  - payload 解析
  - policy
  - fallback query
  - slot hint 构造
- 应拆成：
  - `tool_defs`
  - `tool_dispatch`
  - `tool_payload`
  - `tool_policy`

### 5.3 `scheduleplan/nodes.go`
- 713 行。
- 当前已经开始混：
  - plan
  - rough build
  - preview return
  - model call helper
- 现在还来得及拆。

---

## 6. 建议的第一批收口顺序

### 第一批（先抽公共层，不碰业务语义）
- `P0-1` 抽模型调用公共层。
- `P0-2` 抽 JSON 解析公共层。
- `P0-3` 抽深拷贝公共层。
- `P0-4` 抽排程预览加载/保存 gateway。

### 第二批（统一 skill 编排骨架）
- `P1-1` 统一 `EmitStage`。
- `P1-2` 统一流式阶段输出辅助。
- `P1-3` 统一 fallback/retry 框架。

### 第三批（拆超大文件）
- `P2-1` 拆 `schedulerefine/nodes.go`
- `P2-2` 拆 `schedulerefine/tool.go`
- `P2-3` 拆 `scheduleplan/nodes.go`

---

## 7. 可以考虑直接删除/合并的嫌疑区域

### 7.1 `scheduleplan` 与 `schedulerefine` 的边界可能切得过细
- 这两个包共享大量状态与工具语义。
- 后续大概率应该合并成一个 `schedule` 能力域，内部再区分 `create/refine` flow。

### 7.2 `agentsvc` 侧的排程桥接文件可能过多
- 当前已有：
  - [agent_schedule_plan.go](E:/SmartFlow-Agent/backend/service/agentsvc/agent_schedule_plan.go)
  - [agent_schedule_refine.go](E:/SmartFlow-Agent/backend/service/agentsvc/agent_schedule_refine.go)
  - [agent_schedule_preview.go](E:/SmartFlow-Agent/backend/service/agentsvc/agent_schedule_preview.go)
- 这些文件后续应下沉部分逻辑到 gateway / repository / snapshot service。

---

## 8. 后续重构时的硬约束
- 先抽公共能力，再动 skill 业务逻辑，避免一边搬家一边改语义导致回归难查。
- 每抽一类公共件，都优先让 `quicknote` 和 `taskquery` 先接一次，确认不是只为 `schedule` 定制。
- 抽公共层时，不要先追求“最优抽象”，先追求“把四份重复收成一份”。

## 9. 一句话版本
- 当前 agent 代码最该先收口的 4 个点是：
  - 模型调用
  - JSON 解析
  - 深拷贝/快照
  - 排程预览缓存与快照读写
- 当前最该先拆的 2 个大文件是：
  - [nodes.go](E:/SmartFlow-Agent/backend/agent/schedulerefine/nodes.go)
  - [tool.go](E:/SmartFlow-Agent/backend/agent/schedulerefine/tool.go)

