# 功能决策记录：Execute Context Compaction

## 1. 基本信息
- 记录编号：FDR-2026-003
- 功能名称：Execute 节点上下文窗口动态管理与 LLM 压缩
- 记录日期：2026-04-15
- 决策状态：已采纳
- 负责人：LoveLosita
- 关联需求 / Issue：Execute 节点 msg1/msg2 硬编码裁剪导致 LLM 上下文不完整

## 2. 背景与问题
- 业务背景：Execute 节点通过 4 条消息（msg0=系统规则, msg1=历史对话, msg2=ReAct Loop, msg3=当前状态）构建 LLM prompt，指导 ReAct 循环决策。
- 现状问题：
  1. msg1 使用 1400 字符总上限 + 30 轮对话上限 + 单条 120 字符截断，三重硬编码限制叠加导致长对话中 LLM 看不到完整的用户诉求和约束条件。
  2. msg2 仅保留最近 8 条 ReAct Loop 记录，复杂任务中早期工具调用结果丢失，LLM 无法理解执行全貌。
  3. 四条消息之间没有统一的 token 预算管理，各维度独立裁剪，无法保证总 prompt 不超模型上下文窗口。
- 不做此决策的后果：随着任务复杂度和对话轮数增长，LLM 因上下文裁剪频繁丢失关键信息，导致决策质量下降（漏掉用户约束、重复执行已完成的操作、无法回溯错误路径）。

## 3. 决策目标
- 目标 1：移除所有硬编码裁剪限制，改为全量加载 msg1/msg2 数据。
- 目标 2：基于 80k token 总预算统一管理四条消息的 token 分配，超限时通过 LLM 压缩保留语义信息。
- 目标 3：提供上下文窗口实时查询接口，便于调试和监控。
- 非目标：不改造 Chat/Plan/Deliver 节点的上下文管理（各节点 token 策略独立），不引入流式压缩（压缩在 ReAct 循环内同步完成）。

## 4. 备选方案

### 方案 A：维持硬编码裁剪 + 调参
- 描述：保持现有架构，仅上调 1400 字符、30 轮、8 条等阈值。
- 优点：零开发成本，改动最小。
- 缺点：治标不治本，调参后仍有上限；不同任务的最优阈值不同，无法一劳永逸。
- 复杂度 / 成本：极低。

### 方案 B：动态 Token 预算 + LLM 滚动压缩（采纳）
- 描述：全量加载后统一计算 token，超 80k 预算时按 msg1 → msg2 优先级调用 LLM 生成压缩摘要，替代原始文本。压缩结果持久化到 DB，后续轮次复用摘要 + 新增量。
- 优点：
  1. 无硬编码上限，短对话全量放行，长对话按语义压缩。
  2. watermark 机制保证跨轮增量压缩，不重复处理已压缩内容。
  3. 独立压缩 msg1（历史对话）和 msg2（ReAct Loop），互不干扰。
- 缺点：首次触发压缩时需额外调用一次 LLM，增加约 2-5 秒延迟和 token 开销。
- 复杂度 / 成本：中等（新增 ~200 行核心逻辑，涉及 prompt/node/service/dao/api 五层）。

### 方案 C：滑动窗口 + 向量检索
- 描述：将历史上下文存入向量数据库，每轮按相关性检索 top-k 片段注入 prompt。
- 优点：理论上最精确的上下文选择。
- 缺点：引入向量数据库依赖，架构复杂度剧增；Execute 阶段对时序连续性敏感，检索结果可能打乱因果链。
- 复杂度 / 成本：高。

## 5. 最终决策
- 采纳方案：方案 B
- 关键理由：
  1. 方案 A 无法根治问题，每次阈值调整都是临时修补。
  2. 方案 C 过度设计，引入的依赖和复杂度不适合当前阶段。
  3. 方案 B 在可控复杂度内解决了核心问题：无硬编码上限 + 语义保留 + 持久化复用。
  4. LLM 压缩的额外延迟仅在首次超限时触发，后续轮次复用缓存结果。

## 6. 影响范围
- 涉及模块：
  - `newAgent/node/execute.go` — ReAct 循环中新增 token 预算检查和压缩调用
  - `newAgent/node/execute_compact.go` — 新增压缩逻辑（独立文件）
  - `newAgent/prompt/compact_msg1.go` — msg1 压缩 prompt
  - `newAgent/prompt/compact_msg2.go` — msg2 压缩 prompt
  - `newAgent/prompt/execute_context.go` — 移除硬编码裁剪
  - `newAgent/model/state_store.go` — 新增 CompactionStore 接口
  - `newAgent/model/graph_run_state.go` — AgentGraphDeps 新增字段
  - `pkg/token_budget.go` — 新增预算常量和检查函数
  - `dao/agent.go` — 新增 compaction CRUD
  - `dao/agent-cache.go` — 新增 Redis 缓存
  - `model/agent.go` — AgentChat 新增字段
  - `service/agentsvc/agent.go` — 新增 compactionStore 字段和注入
  - `service/agentsvc/agent_meta.go` — 新增 GetContextStats 方法
  - `api/agent.go` — 新增 GetContextStats handler
  - `routers/routers.go` — 注册 /context-stats 路由
  - `cmd/start.go` — 注入 CompactionStore
- 数据与存储影响：
  - `agent_chats` 表新增 3 列：`compaction_summary TEXT`、`compaction_watermark INT DEFAULT 0`、`context_token_stats JSON`
  - Redis 新增 `smartflow:compaction:{chatID}` 缓存 key
- 接口 / 协议影响：
  - 新增 SSE 状态码 `context_compact_start` / `context_compact_done`（stage 为 `compact_msg1` 或 `compact_msg2`）
  - 新增 HTTP API `GET /api/v1/agent/context-stats?conversation_id=xxx`
- 监控与日志影响：
  - ReAct 循环中新增 `[COMPACT]` 前缀日志，记录 token 分布和压缩决策
  - 每轮 execute 结束后持久化 token 分布到 `context_token_stats` 字段

## 7. 风险与应对
- 风险 1：LLM 压缩摘要丢失关键信息，导致后续决策质量下降。
  - 应对策略：压缩 prompt 明确要求保留用户诉求、约束条件、操作结果、偏好信息；前端可通过 SSE 通知感知压缩发生，提醒用户关键约束可能被折叠。
- 风险 2：首次触发压缩时的额外延迟影响用户体验。
  - 应对策略：通过 SSE `context_compact_start` 推送前端展示"正在压缩上下文..."提示；watermark 机制保证后续轮次复用缓存，不重复触发。
- 风险 3：Token 估算不精确（CJK 1:1, ASCII 4:1 是粗略估算），导致实际 token 超出模型限制。
  - 应对策略：预算常量 `ExecuteTokenBudget=80000` 留有充足余量（模型实际支持 128k+）；后续可接入模型 tokenizer 精确计数作为优化项。

## 8. 验证与回滚
- 验证方式：
  1. `go build ./...` 全量编译通过
  2. 短对话场景：不触发压缩，msg1/msg2 全量放行
  3. 长对话场景：触发 msg1 压缩，验证摘要质量、SSE 通知、token 降幅
  4. `GET /context-stats` 接口返回正确的 token 分布 JSON
- 成功判定标准：
  1. 编译零错误
  2. 短对话功能不受影响（回归）
  3. 长对话中压缩后 LLM 决策质量不低于压缩前
  4. SSE 通知正确推送
- 回滚方案：将 `compactExecuteMessagesIfNeeded` 函数改为直接 `return messages` 即可跳过压缩逻辑，所有其他改动均为增量添加，不影响原有功能。

## 9. 里程碑与后续计划
- 里程碑 1：编译通过 + 短对话回归验证（已完成）
- 里程碑 2：集成测试 — 模拟长对话触发压缩场景（待做）
- 后续优化项：
  1. 接入模型 tokenizer 替换粗略估算
  2. 压缩摘要质量自动化评估（对比压缩前后 LLM 决策准确率）
  3. Redis 缓存 compaction 结果的 TTL 策略优化
  4. 前端 UI 展示上下文窗口使用情况和压缩状态

## 10. 复盘结论（上线后补充）
- 实际效果：
- 与预期偏差：
- 后续是否需要二次决策：
