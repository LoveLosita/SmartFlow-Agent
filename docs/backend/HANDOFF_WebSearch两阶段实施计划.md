# WebSearch 两阶段实施计划（newAgent）

## 1. 目标与范围

本文用于把 `newAgent` 的 WebSearch 能力按两阶段落地：

1. 第一阶段：先接入可用的检索与抓取能力（低风险、快交付）。
2. 第二阶段：在第一阶段基础上升级为 WebRAG 语义召回链路（提升复杂问题命中率与可解释性）。

约束：

1. 不走 `infra/smartflow-mcp-server`，直接走 `newAgent/tools` 工具注册链路。
2. 保持现有执行模式不变：读操作 `action=continue + tool_call`。
3. 第一阶段只接单供应商；第二阶段再考虑 provider fallback。

---

## 2. 第一阶段（V1）：WebSearch + 简单抓取

### 2.1 交付目标

让模型可以：

1. 通过 `web_search` 获得结构化检索结果（标题、摘要、URL、来源域名、时间）。
2. 通过 `web_fetch` 拉取指定 URL 正文并做最小清洗。
3. 在不改主流程的前提下，把结果作为标准 `tool observation` 写回历史。

### 2.2 计划新增工具

1. `web_search`
   - 输入：`query`、`top_k`、`domain_allow`、`recency_days` 等。
   - 输出：JSON 字符串（`tool`、`query`、`count`、`items[]`）。
2. `web_fetch`
   - 输入：`url`、`max_chars`。
   - 输出：JSON 字符串（`tool`、`url`、`title`、`content`、`truncated`）。

### 2.3 代码落点

新增文件：

1. `backend/newAgent/tools/web_tools.go`：工具参数解析、输出组装、错误兜底。
2. `backend/newAgent/tools/web_provider.go`：搜索供应商抽象接口与通用数据结构。
3. `backend/newAgent/tools/web_provider_tavily.go`（或 `web_provider_brave.go`）：首个 provider 实现。
4. `backend/newAgent/tools/web_fetcher.go`：URL 抓取与 HTML 最小清洗。

修改文件：

1. `backend/newAgent/tools/registry.go`：注册 `web_search`、`web_fetch` 两个读工具。
2. `backend/cmd/start.go`：初始化 provider 配置并注入 registry（或通过包级配置读取）。
3. `backend/newAgent/prompt/execute_context.go`：补充新工具的 schema 说明与示例。

### 2.4 V1 验收标准

1. 模型能稳定调用 `web_search` 并拿到可解析 JSON 结果。
2. `web_fetch` 在正文可达时返回正文，在失败时返回明确错误码与原因。
3. 工具超时、429、5xx 均不会打断主流程，只返回可恢复 observation。
4. 日志可定位：query、tool、耗时、结果数、失败原因。

---

## 3. 第二阶段（V2）：WebRAG 语义召回

### 3.1 交付目标

新增 `web_rag_search`，把“检索 + 抓取 + 分块 + 召回 + 重排 + 证据返回”收敛为一个读工具，提升复杂问答质量。

### 3.2 链路设计

1. 查询改写：把用户问题改写为 1~3 个检索子查询。
2. WebSearch 召回：拿到候选 URL 集合。
3. 抓取清洗：抽正文，去噪。
4. 分块：按段落与 token 预算切块。
5. 召回：向量召回 + 关键词召回（混合召回）。
6. 重排：按 query 相关性重排 chunk。
7. 输出：返回答案所需证据片段、来源 URL、片段得分。

### 3.3 代码落点

新增文件：

1. `backend/newAgent/tools/web_rag_tools.go`：`web_rag_search` 工具入口。
2. `backend/newAgent/tools/web_rag_chunker.go`：清洗后分块。
3. `backend/newAgent/tools/web_rag_retriever.go`：混合召回。
4. `backend/newAgent/tools/web_rag_rerank.go`：重排层。
5. `backend/newAgent/tools/web_rag_store.go`：会话级索引缓存（先内存/Redis TTL）。

修改文件：

1. `backend/newAgent/tools/registry.go`：注册 `web_rag_search`。
2. `backend/newAgent/prompt/execute_context.go`：增加 `web_rag_search` 使用规范。

### 3.4 V2 验收标准

1. 同类复杂问题下，回答引用质量和相关性明显高于 V1。
2. 返回至少包含：`answer_evidence[]`（片段+URL+score）。
3. 召回或重排失败时可降级到 V1（`web_search + web_fetch`）路径。
4. 提供基础评估指标：命中率、延迟、成本、失败率。

---

## 4. 与记忆系统的关系

`WebRAG` 与记忆系统 RAG 高度重合，建议“共用内核、分语料适配”：

1. 共用：chunk / embed / retrieve / rerank 的通用接口与实现。
2. 分开：`MemoryCorpus`（私有数据）与 `WebCorpus`（公网数据）的数据源适配层。
3. 在工具层保持两个入口：`memory_search` 与 `web_rag_search`，返回结构尽量统一。

---

## 5. 上线顺序与回滚策略

### 5.1 上线顺序

1. 先灰度 V1：仅开放 `web_search`、`web_fetch`。
2. 观察稳定性与成本后再灰度 V2：`web_rag_search`。
3. V2 稳定后再考虑 provider fallback 与更长周期缓存。

### 5.2 回滚策略

1. `web_rag_search` 异常时，快速降级为 V1 工具集。
2. V1 供应商异常时，返回“检索暂不可用”的结构化 observation，不阻断主流程。
3. 保留 feature flag：按工具级别开关，支持秒级关闭。

---

## 6. 风险清单

1. 供应商配额/限流导致查询失败。
2. 页面反爬与正文抽取质量不稳定。
3. RAG 链路成本上升（抓取+embedding+重排）。
4. 引用片段与最终答案不一致（需要强制证据对齐策略）。

---

## 7. 里程碑建议

1. M1（1~2 天）：V1 工具跑通，联调 Execute 节点可调用。
2. M2（2~4 天）：V1 稳定性优化（超时/限流/日志/错误码）。
3. M3（4~7 天）：V2 WebRAG MVP（混合召回+基础重排+证据输出）。
4. M4（后续）：统一 RAG Core，打通记忆系统复用。

