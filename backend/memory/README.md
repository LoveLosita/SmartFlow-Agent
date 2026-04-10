# Memory 模块现状说明

## 当前已打通的链路

1. 用户消息落聊天历史时，会通过 outbox 发布 `memory.extract.requested`。
2. 事件消费者只负责把请求幂等写入 `memory_jobs`，不在消费回调里做重 LLM 计算。
3. 启动期会拉起 `memory worker`，后台轮询 `memory_jobs`。
4. worker 抢占任务后，调用 `backend/infra/llm` 驱动的记忆抽取编排器。
5. 抽取结果会被标准化后写入 `memory_items`，同时写入 `memory_audit_logs`。
6. 全部落库成功后，任务状态推进到 `success`；失败则走可重试状态机。

## 当前目录职责

- `module.go`：对外统一门面，负责组装 repo / service / worker / orchestrator。
- `model/`：记忆模块 DTO、状态常量、配置对象。
- `repo/`：`memory_jobs / memory_items / memory_audit_logs / memory_user_settings` 访问层。
- `service/`：任务入队、读取重排、管理维护、配置加载。
- `orchestrator/`：记忆抽取编排。
  - `write_orchestrator.go` 是纯本地 fallback。
  - `llm_write_orchestrator.go` 是当前主用的 LLM 抽取器。
- `worker/`：任务执行器与后台轮询循环。
- `utils/`：JSON 提取、候选事实标准化、设置过滤、审计构造等纯函数工具。

## 当前已补齐的内部能力

1. `Module`
   - 负责把 repo / service / worker / orchestrator 组装成统一门面。
   - 外部现在优先依赖 `memory.Module`，而不是自己手搓内部组件。
   - 支持 `WithTx(tx)`，方便接入现有统一事务管理器。
2. `EnqueueService`
   - 负责把 `memory.extract.requested` 事件转成 `memory_jobs`，不做重 LLM 计算。
3. `Runner + RunPollingLoop`
   - 负责后台轮询任务、调用抽取器、写入 `memory_items`、补写 `memory_audit_logs`。
4. `ReadService`
   - 负责在 memory 内部做“按用户开关过滤 + 轻量重排 + 访问时间刷新”。
   - 当前还没有接到 `newAgent` prompt 注入侧，这是刻意保留的切流点。
5. `ManageService`
   - 负责记忆管理面能力：列出记忆、软删除记忆、读取/更新用户记忆开关。
   - 删除动作会同步写入审计日志，保证“有变更就有审计”。

## 当前推荐接入姿势

1. 启动阶段统一创建：
   - `memoryModule := memory.NewModule(db, llmClient, memory.LoadConfigFromViper())`
2. 后台 worker 启动：
   - `memoryModule.StartWorker(ctx)`
3. 事务内写入记忆任务：
   - `memoryModule.WithTx(tx).EnqueueExtract(ctx, payload, eventID)`
4. 后续 agent 读取：
   - 直接调用 `memoryModule.Retrieve(...)`

## 当前实现边界

1. 已实现异步写入链路，也已补齐 memory 内部读取与管理能力，但还没有接“读取召回 + prompt 注入”。
2. 已实现 MySQL 事实落库，但还没有接 Milvus 向量同步。
3. 已实现 LLM 抽取和基础审计日志，但还没有做 `ADD/UPDATE/DELETE/NONE` 决策型冲突消解。
4. 当前更偏“先把 memory 自己的闭环打通”，后续再继续做 agent 注入、向量检索和冲突更新。

## 当前推荐验证方式

1. 发起一条用户消息，确认 outbox 中生成 `memory.extract.requested`。
2. 等待事件消费后，确认 `memory_jobs` 出现 `pending` 或被 worker 抢占为 `processing`。
3. 等待后台 worker 执行后，确认：
   - `memory_jobs.status = success`
   - `memory_items` 出现新记忆
   - `memory_audit_logs` 出现对应 `create` 记录
4. 直接调用 `ManageService`：
   - `ListItems` 能列出 active/archived 记忆
   - `DeleteItem` 会把状态改成 `deleted`，并新增一条 `delete` 审计
   - `GetUserSetting / UpsertUserSetting` 能返回并更新用户记忆开关

## 下一步建议

1. 把 `ReadService` 接进 `newAgent`，先注入“偏好 / 约束 / 最近 todo_hint”三类高价值记忆。
2. 引入向量召回与 rerank，把“当前话题相关的事实类记忆”补进候选集合。
3. 再补 `ADD/UPDATE/DELETE/NONE` 决策，解决“同义记忆去重”和“旧记忆更新”。
