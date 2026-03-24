# agent2 逐批搬迁实施细节

## 1. 文档目的
- 本文档用于指导 `backend/agent -> backend/agent2` 的渐进式重构。
- 目标是“在逻辑不变的前提下重整结构、消除冗余、逐步切流”，而不是一次性重写。
- 该文档优先服务于施工过程，强调“怎么搬、先搬什么、如何验证、何时删除旧代码”。

## 2. 总体迁移原则

### 2.1 不直接改老目录，先并行维护
- 保持现有 `backend/agent` 不动，作为稳定对照组。
- 新建 `backend/agent2`，所有重构都先落在新目录。
- `backend/service/agentsvc` 在迁移期充当“总开关层”，决定某条链路接旧目录还是新目录。

### 2.2 先搬结构，再收冗余
- 第一优先级不是“顺手优化业务逻辑”，而是把职责边界搬顺。
- 迁移时允许短期存在“代码原样搬过去”的情况，但必须同步记录哪些地方后续要抽公共层。
- 严禁一边大改业务逻辑、一边改目录结构，否则回归问题无法定位。

### 2.3 新旧链路必须可对照
- 每搬完一个 skill，都要保证：
  - 老链路还能跑；
  - 新链路能独立接入；
  - service 层可以随时切回旧实现。
- 迁移期间所有新入口都必须保留 feature flag 或切换点。

### 2.4 先做“能收敛冗余”的迁移
- 优先搬那些已经出现明显重复代码的能力，不要先搬边缘功能。
- 迁移不是“把史山平移到新目录”，而是“借搬迁之机把重复公共件抽出来”。

---

## 3. agent2 目标结构

```text
backend/agent2/
  entrance.go

  router/
    route.go
    route_model.go

  graph/
    quicknote.go
    taskquery.go
    schedule.go

  node/
    quicknote.go
    taskquery.go
    schedule_plan.go
    schedule_refine.go

  llm/
    client.go
    json.go
    route.go
    quicknote.go
    taskquery.go
    schedule.go

  model/
    common.go
    route.go
    quicknote.go
    taskquery.go
    schedule.go

  prompt/
    route.go
    quicknote.go
    taskquery.go
    schedule.go

  stream/
    emitter.go
    openai.go

  shared/
    clone.go
    retry.go
    time.go
```

## 4. 各层职责约束

### 4.1 `entrance.go`
- 是整个 `agent2` 模块唯一总入口。
- 负责：
  - 接收请求上下文；
  - 调路由；
  - 调 skill graph；
  - 收口 token、SSE、持久化。
- 不负责：
  - 某个 skill 内部节点的业务实现；
  - 直接写 prompt；
  - 直接调用模型。

### 4.2 `router/`
- 只负责一级分流。
- 输出统一路由结果，例如：
  - `chat`
  - `quick_note_create`
  - `task_query`
  - `schedule_plan_create`
  - `schedule_plan_refine`
- 不负责 skill 内部二次判断。

### 4.3 `graph/`
- 只负责画 graph。
- 文件里应尽量只出现：
  - `AddLambdaNode`
  - `AddEdge`
  - `AddBranch`
- 不允许在 `graph/` 内直接写复杂业务逻辑、模型调用或数据转换。

### 4.4 `node/`
- 只负责每个节点内部的业务逻辑。
- 可以调用：
  - `llm/`
  - `shared/`
  - service/dao 注入依赖
- 不负责：
  - Graph 连线；
  - OpenAI chunk 输出格式；
  - 大段 prompt 常量定义。

### 4.5 `llm/`
- 专门负责和模型交互。
- 统一收口：
  - `GenerateText`
  - `GenerateJSON`
  - `Stream`
  - JSON 提取与解析
  - thinking/temperature/maxTokens 等参数
- 这一层的定位，等价于“模型数据访问层”。

### 4.6 `model/`
- 存放 agent 域模型，而不是数据库模型。
- 包括：
  - state
  - dto
  - route decision
  - tool result

### 4.7 `prompt/`
- 统一收口 prompt 文本。
- 禁止在 `node/` 和 `llm/` 中内联大段 prompt。

### 4.8 `stream/`
- 统一收口 SSE/OpenAI 兼容块输出。
- 所有阶段推送、单条 assistant 回复、finish chunk 都从这里走。

### 4.9 `shared/`
- 只放纯工具，不放业务编排。
- 当前预计优先放：
  - 深拷贝
  - 重试策略
  - 时间解析

---

## 5. 逐批搬迁顺序

## 第 0 批：先搭骨架，不接流量

### 目标
- 先把 `agent2` 目录骨架建起来。
- 不迁任何业务，只搭结构。

### 产物
- `backend/agent2/` 目录
- `entrance.go`
- `router/graph/node/llm/model/prompt/stream/shared` 空壳

### 验证
- 能编译通过。
- 不接任何线上/实际业务入口。

---

## 第 1 批：先迁公共能力，不迁 skill

### 目标
- 先抽最明显的公共重复件，给后续 skill 迁移打基础。

### 优先抽取的公共件
- `llm/json.go`
  - 统一 `GenerateJSON`
  - 统一 JSON object 提取
  - 统一空响应/解析失败错误
- `llm/client.go`
  - 统一 `GenerateText`
  - 统一 thinking/maxTokens/temperature 参数装配
- `stream/openai.go`
  - 迁入现有 `ToOpenAIStream`
  - 迁入 `ToOpenAIFinishStream`
- `stream/emitter.go`
  - 统一阶段推送
  - 统一单条 assistant completion 输出
- `shared/clone.go`
  - 抽 `cloneWeekSchedules`
  - 抽 `cloneHybridEntries`
  - 抽 `cloneTaskClassItems`

### 说明
- 这一批不碰 quicknote/taskquery/schedule 的业务语义。
- 只把公共重复代码先搬进 `agent2`。

---

## 第 2 批：迁 `quicknote`，作为样板 skill

### 目标
- 用最小 skill 验证三层结构是否顺手。

### 对应关系
- 旧：
  - `backend/agent/quicknote/graph.go`
  - `backend/agent/quicknote/nodes.go`
  - `backend/agent/quicknote/tool.go`
  - `backend/agent/quicknote/state.go`
  - `backend/agent/quicknote/prompt.go`
- 新：
  - `backend/agent2/graph/quicknote.go`
  - `backend/agent2/node/quicknote.go`
  - `backend/agent2/llm/quicknote.go`
  - `backend/agent2/model/quicknote.go`
  - `backend/agent2/prompt/quicknote.go`

### 迁移要求
- 逻辑不变。
- 原有测试优先复用。
- 如果出现可直接删除的冗余函数，必须当场删除，不允许“先复制一份以后再说”。

### 接入方式
- 在 `agentsvc` 里增加切换开关：
  - `run quicknote with agent`
  - `run quicknote with agent2`

---

## 第 3 批：迁 `taskquery`

### 目标
- 用第二个 skill 验证公共层抽取是否真的通用，而不是只服务于 quicknote。

### 核心检查点
- `taskquery` 是否能完全复用第 1 批抽出的 `llm/json.go`。
- fallback/retry 是否还能保持一致。
- 阶段推送是否已经不需要 skill 自己再包一层。

### 完成标准
- `taskquery` 不再保留自己私有的 JSON 调用封装。

---

## 第 4 批：迁 `route`

### 目标
- 把统一分流也纳入 `agent2`。

### 要求
- `entrance.go` 以后只依赖 `agent2/router`。
- `router` 只做一级分流，不再夹杂 skill 级 fallback。

---

## 第 5 批：迁 `scheduleplan`

### 目标
- 把排程首次创建链路迁入新结构。

### 迁移策略
- 不直接优化复杂业务。
- 先把它拆成：
  - `graph/schedule.go`
  - `node/schedule_plan.go`
  - `llm/schedule.go`
  - `model/schedule.go`
  - `prompt/schedule.go`

### 强制要求
- 不允许继续保留“节点文件里夹杂模型调用帮助函数”的写法。
- 不允许在 `node/` 里再内联 JSON parse helper。

---

## 第 6 批：迁 `schedulerefine`

### 目标
- 处理当前最重的史山。

### 原则
- 这一步不是“原样平移”。
- 这是第一次允许在迁移中顺手拆大文件。

### 最低拆分要求
- 旧 `nodes.go` 至少拆成：
  - `schedule_refine_contract`
  - `schedule_refine_plan`
  - `schedule_refine_react`
  - `schedule_refine_review`
  - `schedule_refine_summary`
- 旧 `tool.go` 至少拆成：
  - `tool_defs`
  - `tool_dispatch`
  - `tool_payload`
  - `tool_policy`

---

## 第 7 批：切流与删除旧目录

### 前提条件
- `quicknote`
- `taskquery`
- `route`
- `scheduleplan`
- `schedulerefine`

以上链路都已经切到 `agent2`，并经过回归验证。

### 操作顺序
- 1. service 层入口全部切到 `agent2`
- 2. 跑一轮完整回归
- 3. 删除旧 `backend/agent`
- 4. `backend/agent2` 改名为 `backend/agent`

### 注意
- 第 4 步必须最后做。
- 在此之前不要提前重命名，避免 review 和 diff 变得混乱。

---

## 6. 迁移过程中的强约束

### 6.1 逻辑不变优先于结构完美
- 只要旧逻辑还能清晰搬进新层次，就先搬。
- 不要在同一轮同时追求“换架构 + 换策略 + 换 prompt”。

### 6.2 每次只迁一个能力域
- 一次只动一个 skill 或一类公共件。
- 严禁“这一轮顺手把三个 skill 一起改了”。

### 6.3 旧代码不删前，新代码必须已接入验证
- 先搬
- 再切
- 再验
- 最后删

### 6.4 公共能力一旦发现第二处重复，就必须考虑抽层
- 不允许在 `agent2` 里继续复制第三份、第四份。
- `agent2` 的目标不是“新目录复刻旧史山”，而是“迁移过程中顺手收口公共能力”。

---

## 7. 每一批迁移完成后的检查项
- 是否出现了新的重复模型调用 helper？
- 是否出现了新的重复 JSON parse helper？
- 是否又把 prompt 写回节点文件里了？
- 是否又在 service 层偷偷加了一层业务桥接？
- 是否还能一眼看出：
  - 入口在哪
  - 分流在哪
  - 编排在哪
  - 节点逻辑在哪
  - 模型交互在哪

---

## 8. 当前最值得优先搬的公共件
- `GenerateJSON/GenerateText`
- JSON 提取与解析
- OpenAI 兼容 SSE 包装
- 阶段推送 emitter
- schedule 相关深拷贝工具
- schedule preview / snapshot 读写逻辑

---

## 9. 一句话版本
- 先建 `agent2` 骨架。
- 先抽公共层，再迁 skill。
- 先迁 `quicknote` 做样板，再迁 `taskquery` 验证复用，再动 `schedule`。
- 迁移期间新旧并存、随时可切回。
- 删除旧 `agent` 只能发生在所有 skill 全部切流并验证通过之后。

