# HANDOFF：RAG 复用后续实施计划（Memory 目录留档）

## 1. 文档目的

1. 给下一位开发者一个可直接执行的后续实施蓝图。
2. 明确“已完成/未完成/切流点/回滚点”，避免重复摸索。
3. 保证 Memory 与 WebSearch 共用一套 RAG Core，减少重复实现。

## 2. 当前状态（截至本次交接）

### 2.1 已完成

1. `backend/infra/rag` 共享骨架已建好，包含：
   - `core`：统一类型、接口、pipeline、错误码。
   - `chunk/embed/retrieve/rerank/store`：默认可运行实现（mock/in-memory/noop）。
   - `corpus`：`MemoryCorpus`、`WebCorpus` 适配器。
   - `config`：`rag.*` 配置读取。
2. Memory Day1 写入链路已打通（`memory.extract.requested` 事件、`memory_jobs` 入库、worker 状态推进）。
3. Milvus 相关 `docker-compose` 服务定义已补齐（本机因网络问题尚未拉起验证）。

### 2.2 未完成

1. RAG Core 尚未接入真实业务调用点（当前仍是并行骨架状态）。
2. Milvus 真正读写实现未落地（`MilvusStore` 为占位）。
3. Eino Embedding/Reranker 未落地（当前占位实现）。
4. Memory 读取注入链路（Day2）尚未切到 RAG Core。
5. WebSearch 尚未切到 `WebCorpus + RAG Core`。

## 3. 后续实施总原则

1. 并行迁移：新旧逻辑并存，先灰度后切流，最后删除旧实现。
2. 单轮只做一个能力域：先 Memory 读链路，再 WebSearch。
3. 保留可回滚开关：任何切流都必须有一键回退路径。
4. 失败可降级：Rerank/Vector 异常不影响主链路响应。

## 4. 建议执行顺序（4 轮）

## Round 2：Memory 读链路接入 RAG Core（优先）

### 目标

1. Memory 检索由 `infra/rag` 承载，保留旧检索兜底。
2. 注入效果不劣于当前逻辑，且可观测。

### 实施项

1. 在 Memory ReadService 中接入 `core.Pipeline.Retrieve`。
2. 构造 `MemoryRetrieveInput`，强制过滤：
   - `user_id + assistant_id + conversation_id`
3. 配置开关：
   - `memory.rag.enabled`（默认 `false`，灰度开启）
4. 降级策略：
   - RAG 失败 -> 回退旧检索链路；
   - Rerank 失败 -> 使用原排序（pipeline 已支持 fallback 标记）。
5. 指标补齐：
   - `memory_rag_hit_count`
   - `memory_rag_fallback_rate`
   - `memory_rag_latency_ms`

### 验收

1. 开关关闭时行为与当前一致。
2. 开关开启时可稳定召回，失败能回退。
3. 日志可追踪“为什么没注入某条记忆”。

## Round 3：WebSearch 接入 WebCorpus + RAG Core

### 目标

1. WebSearch 复用同一检索流程，不再各写一套召回逻辑。
2. 严格限制跨 query/session 串召回。

### 实施项

1. 把抓取结果映射为 `WebIngestItem` 入 RAG。
2. 检索时必须带：
   - `query_id` 或 `session_id`
3. 配置开关：
   - `websearch.rag.enabled`（默认 `false`）
4. 保留原 websearch 结果路径，RAG 仅先做“补充召回层”。

### 验收

1. 开启后答案质量不下降，且无跨 query 污染。
2. 关闭后立即回到旧逻辑。

## Round 4：Milvus + Eino 真实实现替换

### 目标

1. 将 `InMemoryStore` 替换为 `MilvusStore`（生产可用）。
2. 将 `MockEmbedder/NoopReranker` 替换为 Eino 实现。

### 实施项

1. `store/milvus_store.go`：
   - 实现 `Upsert/Search/Delete/Get`
   - 建 collection 与 metadata filter 映射
2. `embed/eino_embedder.go`：
   - 完成 embedding 调用与超时控制
3. `rerank/eino_reranker.go`：
   - 完成重排调用与错误降级
4. 配置补齐：
   - `rag.store=milvus`
   - `rag.embed.provider=eino`
   - `rag.reranker.provider=eino`

### 验收

1. Milvus 可稳定写入/检索。
2. 模型服务波动时主链路可降级。
3. 指标完整（命中、延迟、fallback、错误码）。

## Round 5：统一切流与旧逻辑收敛

### 目标

1. Memory + WebSearch 默认走 RAG Core。
2. 删除重复旧实现，避免双轨长期维护成本。

### 实施项

1. 提升开关默认值为 `true`。
2. 观察窗口（建议 3~7 天）稳定后删除旧分支代码。
3. 更新文档：
   - `backend/memory/记忆模块实施计划.md`
   - `backend/agent/通用能力接入文档.md`（若新增/替换通用能力，必须同步）

### 验收

1. 代码无重复检索实现。
2. 回滚开关仍可用（应急时关闭 RAG）。
3. 线上指标稳定且可追踪。

## 5. 开关与回滚建议

1. 建议开关：
   - `memory.rag.enabled`
   - `websearch.rag.enabled`
   - `rag.reranker.enabled`
2. 回滚策略：
   - 先关 corpus 级开关（memory/websearch）。
   - 再关 reranker。
   - 极端情况降级到旧检索链路。

## 6. 关键风险与应对

1. 风险：切流后召回漂移。  
   应对：双写日志对比（旧链路 vs 新链路 TopK）。
2. 风险：Milvus 延迟波动。  
   应对：检索超时 + fallback + 限流。
3. 风险：跨会话串数据。  
   应对：过滤维度强校验，不满足则拒绝检索。

## 7. 接手即办清单（最小行动）

1. 先做 Round 2（Memory 读链路接 RAG），不要同时改 WebSearch。
2. 提交前必须跑：
   - `go test ./...`
3. 每次本地 `go test` 后清理项目根目录 `.gocache`。
4. 完成一轮后在本文件追加：
   - 已落地清单
   - 待办差距
   - 下一轮入口

