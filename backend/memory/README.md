# Memory 模块（Day1 骨架）

## 本轮目标

1. 打通 `memory.extract.requested` 事件发布与消费。
2. 消费后把任务可靠写入 `memory_jobs`（支持幂等）。
3. 提供 `worker.RunOnce()`，可手工推进 `pending -> processing -> success/failed`。

## 本轮边界（刻意不做）

1. 不接真实 LLM 抽取与冲突决策。
2. 不接 Milvus 向量召回。
3. 不做读取注入链路（Day2 再接）。

## 目录说明

- `model/`：记忆领域 DTO、状态机、配置对象。
- `repo/`：`memory_*` 表访问。
- `service/`：任务入队门面与配置加载。
- `orchestrator/`：写入链路编排（Day1 为 mock 抽取）。
- `worker/`：任务执行器（支持手工触发单次运行）。
- `utils/`：`ExtractJSON`、`NormalizeFacts` 等工具函数。

## 手工验证建议

1. 发起一轮聊天后，检查 outbox 是否存在 `memory.extract.requested`。
2. 等待消费后，检查 `memory_jobs` 是否新增 `pending` 记录。
3. 手工调用 `worker.RunOnce()`，确认任务推进到 `success/failed`。
