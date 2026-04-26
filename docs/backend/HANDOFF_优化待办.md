# newAgent 优化待办 Handoff

> 日期：2026-04-21
> 来源：迁移 agent/ → newAgent/ 完成后的架构审视

---

## 1. TaskQuery 紧急度提升统一

### 问题

LLM 工具查询任务（`AgentService.QueryTasksForTool`）使用 `applyReadTimeUrgencyPromotion` 只做内存态优先级提升，不触发 outbox 写 MySQL。
前端查询任务（`TaskService.GetUserTasks`）使用 `deriveTaskUrgencyForRead` + `tryEnqueueTaskUrgencyPromote`，会异步持久化。

两条路径行为不一致：LLM 看到的优先级可能比 DB 里的高。

### 方案

1. `service/task.go` — 从 `GetUserTasks` 中提取公共方法（如 `GetTasksWithUrgencyPromotion`），返回已提升的 `[]model.Task` 并触发 outbox
2. `service/agentsvc/agent.go` — 新增 `taskSvc *service.TaskService` 字段
3. `service/agentsvc/agent_task_query.go` — 重写 `QueryTasksForTool`，调用 TaskService 公共方法；删除 `applyReadTimeUrgencyPromotion` 死代码
4. `cmd/start.go` — 注入 TaskService 到 AgentService

### 涉及文件

| 文件 | 改动 |
|------|------|
| `service/task.go` | 提取公共方法 |
| `service/agentsvc/agent.go` | 加 taskSvc 字段 |
| `service/agentsvc/agent_task_query.go` | 重写，删 `applyReadTimeUrgencyPromotion` |
| `cmd/start.go` | 注入 TaskService |

---

## 2. service/agentsvc 层瘦身（低优先级）

### 现状

`service/agentsvc/` 目前 11 个文件，大部分是 HTTP→DB 转接层，职责合理。但有两个纯逻辑文件理论上可下沉：

| 文件 | 内容 | 可移至 |
|------|------|--------|
| `agent_memory_render.go` | 纯文本转换，零 DB 交互 | `memory/` 包 |
| `agent_task_query.go` 的 `taskMatchesQueryFilter` / `sortTasksForQuery` | 纯过滤/排序 | `newAgent/tools/` |

### 判断

当前体量小（加起来约 200 行纯函数），搬出去收益不大，反而多一层 import 间接。如果未来这些函数膨胀再搬不迟。

---

## 3. go mod tidy

迁移完成后 `go.mod` 中有未使用的依赖（如 `github.com/bytedance/mockey`）。建议跑一次 `go mod tidy` 清理。
