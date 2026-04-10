# HANDOFF：RAG Infra 一步到位接入方案

## 1. 文档目的

本文用于把 `backend/infra/rag` 从“可运行骨架”推进到“可被业务正式接入的共享基础设施”。

本文重点回答 4 个问题：

1. 当前 `RAG Infra` 已经做到了什么，还缺什么。
2. 什么样的状态，才算“合格、可接入、可灰度、可回滚”的 `RAG Infra`。
3. 如何以“依赖注入 + 对外只暴露方法入口”的方式收口，避免业务侧直接依赖底层实现细节。
4. 如何在不打断现有业务的前提下，把 `memory` 与 `websearch` 并行迁移到统一 `RAG Infra`。

---

## 2. 当前现状

## 2.1 已完成部分

当前 `backend/infra/rag` 已经具备共享骨架，主要包括：

1. 通用接口与类型：
   - `core/interfaces.go`
   - `core/types.go`
   - `core/errors.go`
2. 通用编排器：
   - `core/pipeline.go`
3. 默认切块器：
   - `chunk/text_chunker.go`
4. 语料适配器：
   - `corpus/memory_corpus.go`
   - `corpus/web_corpus.go`
5. 默认可运行实现：
   - `embed/mock_embedder.go`
   - `rerank/noop_reranker.go`
   - `store/inmemory_store.go`
6. 配置骨架：
   - `config/config.go`

这说明项目已经完成了“共享 RAG Core 的第一阶段搭骨架”，不再是单纯的设计想法。

## 2.2 当前存在的问题

虽然骨架已经有了，但距离“可正式接入的 Infra”还差关键几步：

1. 运行时没有正式装配入口。
   - 当前仍主要依赖 `rag.NewDefaultPipeline()`。
   - 启动阶段没有统一按配置组装 `embedder / store / reranker / corpus runtime`。
2. 真实底层实现还是占位。
   - `embed/eino_embedder.go` 未实现。
   - `rerank/eino_reranker.go` 未实现。
   - `store/milvus_store.go` 未实现。
3. 配置虽有结构，但还未真正接入运行链路。
   - `rag/config/config.go` 定义了 `rag.*` 配置。
   - `backend/cmd/start.go` 尚未实例化并注入 `RAG Runtime`。
4. 业务尚未真正切流。
   - `memory` 读取链路还没有正式走 `Pipeline.Retrieve`。
   - `websearch` 还没有通过 `WebCorpus + Pipeline` 形成正式 WebRAG 路径。
5. 工程化能力不完整。
   - 缺统一 timeout。
   - 缺统一日志字段。
   - 缺基础指标。
   - 缺单元测试与集成测试。
6. 还存在潜在重复实现风险。
   - `retrieve/vector_retriever.go` 与 `core/pipeline.go` 都承载部分检索逻辑。
   - 若后续两套逻辑并存，容易出现行为漂移与维护成本上升。

## 2.3 当前状态结论

当前 `RAG Infra` 的状态，更准确地说是：

1. 已经完成“共享骨架搭建”。
2. 还没有完成“统一装配、真实实现、正式接入、工程化收口”。
3. 目前适合继续扩展，但还不适合直接作为长期稳定的业务依赖面。

---

## 3. 目标定义：什么叫“合格的 RAG Infra”

本轮改造完成后，`backend/infra/rag` 应满足以下标准：

1. 启动时可统一构造并注入，不再靠业务模块自行拼装底层依赖。
2. 对外只暴露稳定方法入口，不暴露底层 `Pipeline / Store / Embedder / Reranker` 的装配细节。
3. 支持按配置切换实现：
   - `inmemory / milvus`
   - `mock / eino`
   - `noop / eino`
4. 支持 `memory` 与 `websearch` 两类语料复用同一套 `chunk / embed / retrieve / rerank / fallback` 流程。
5. 支持灰度开关与回滚，不要求业务“一次性硬切流”。
6. 支持基础观测：
   - 延迟
   - 命中数
   - fallback 原因
   - 错误码
7. 具备最小可依赖测试集，保证公共层改动不会悄悄破坏业务。

---

## 4. 核心改造原则

## 4.1 原则一：依赖注入统一由 Infra 自己负责

`RAG Infra` 必须自己承接“底层实现装配”，业务侧不应感知：

1. 当前用的是 `Milvus` 还是 `InMemoryStore`。
2. 当前用的是 `MockEmbedder` 还是 `EinoEmbedder`。
3. 当前是否开启 `Reranker`。
4. 当前超时、阈值、切块参数是多少。

业务只拿到一个已经注入好的 `RAG Runtime` 或 `RAG Service`，直接调用方法。

## 4.2 原则二：对外只暴露方法，不暴露底层零件

业务层不应直接依赖这些细粒度对象：

1. `core.Pipeline`
2. `core.VectorStore`
3. `core.Embedder`
4. `core.Reranker`
5. `corpus.MemoryCorpus`
6. `corpus.WebCorpus`

这些对象应被视为 `infra/rag` 内部拼装细节。

业务层只应调用诸如以下方法：

1. `IngestMemory`
2. `RetrieveMemory`
3. `IngestWeb`
4. `RetrieveWeb`

这样做的好处是：

1. 业务依赖面更稳定。
2. 后续替换底层实现时，不会把改动扩散到多个业务模块。
3. 便于统一日志、监控、降级和权限边界。

## 4.3 原则三：业务语义留在业务层，通用 RAG 工序下沉到 Infra

下沉到 `infra/rag` 的内容：

1. 切块
2. 向量化
3. 向量存储
4. 召回
5. rerank
6. threshold 过滤
7. fallback 语义
8. 统一日志与指标

留在业务层的内容：

1. `memory` 的注入优先级、门控规则、显式/隐式策略
2. `websearch` 的 provider 搜索、query 改写、时间过滤、domain 白名单、抓取策略
3. 最终给模型注入哪些证据、注入多少、如何组织引用

## 4.4 原则四：并行迁移，不一步删旧

本轮改造虽然目标是“一步到位把 Infra 做完整”，但切流必须保持并行迁移：

1. 新 Infra 建好后，先让 `memory` 接入并保留旧逻辑兜底。
2. 再让 `websearch` 接入并保留 V1 路径兜底。
3. 观察稳定后再删除旧分支。

---

## 5. 目标架构

## 5.1 推荐对外结构

建议在 `backend/infra/rag` 新增统一对外门面，例如：

1. `runtime.go`
2. `factory.go`
3. `service.go`

推荐把正式对外依赖面收敛为一个接口，例如：

```go
type Runtime interface {
    IngestMemory(ctx context.Context, input MemoryIngestRequest) (*IngestResult, error)
    RetrieveMemory(ctx context.Context, input MemoryRetrieveRequest) (*RetrieveResult, error)

    IngestWeb(ctx context.Context, input WebIngestRequest) (*IngestResult, error)
    RetrieveWeb(ctx context.Context, input WebRetrieveRequest) (*RetrieveResult, error)
}
```

说明：

1. 业务侧只依赖 `Runtime`。
2. `Runtime` 内部再去调用 `Pipeline + CorpusAdapter + Store + Embedder + Reranker`。
3. 这样可以保证业务不会直接 import `core` 包下的底层细节。

## 5.2 推荐内部结构

建议内部形成以下分工：

1. `factory.go`
   - 负责按配置创建 `Embedder / Store / Reranker / Pipeline`
2. `runtime.go`
   - 负责持有 `Pipeline + MemoryCorpus + WebCorpus + Logger + Metrics`
3. `service.go`
   - 负责定义 `Runtime` 接口与对外方法
4. `core/`
   - 保持底层通用编排逻辑
5. `corpus/`
   - 只负责“语料 -> 标准文档”和“业务过滤 -> 标准 filter”

## 5.3 推荐依赖注入方式

在 `backend/cmd/start.go` 中，启动期统一创建 `RAG Runtime`，例如：

1. 读取 `rag.*` 配置
2. 构造 `RAGFactory`
3. 生成 `RAGRuntime`
4. 注入给：
   - `memory service`
   - `newAgent web tools`

业务侧只拿运行好的对象，不再自己 new 任何底层实现。

---

## 6. 对外方法面设计

## 6.1 Memory 对外方法

推荐对外暴露以下方法：

1. `IngestMemory`
   - 输入：标准化后的记忆入库请求
   - 输出：文档数、chunk 数、同步结果
2. `RetrieveMemory`
   - 输入：用户、会话、助手、run、query、topK、threshold
   - 输出：标准 `RetrieveResult`

注意：

1. `memory` 业务层不应直接调用 `MemoryCorpus`。
2. `memory` 业务层不应自己拼向量过滤条件。
3. 所有过滤条件由 `RetrieveMemory` 内部统一转换。

## 6.2 Web 对外方法

推荐对外暴露以下方法：

1. `IngestWeb`
   - 输入：抓取结果 `url/title/snippet/content/domain/query_id/session_id`
   - 输出：统一入库摘要
2. `RetrieveWeb`
   - 输入：query、query_id/session_id、domain、topK、threshold
   - 输出：标准 `RetrieveResult`

注意：

1. `websearch` 业务层不应直接持有 `WebCorpus`。
2. `websearch` 业务层只负责“拿到页面内容”与“决定是否需要调用 RAG”。
3. 实际向量入库、检索、rerank 由 `infra/rag` 统一处理。

## 6.3 对外方法设计边界

方法层负责什么：

1. 参数合法性校验
2. 内部 filter 组装
3. 调 `Pipeline.Ingest / Retrieve`
4. 统一日志、指标、fallback

方法层不负责什么：

1. 不负责 `websearch provider` 搜索
2. 不负责 HTML 抓取
3. 不负责 prompt 注入
4. 不负责业务排序偏好

---

## 7. 具体改造计划

## 7.1 第一部分：把 RAG Infra 自身做完整

### 目标

让 `backend/infra/rag` 成为“正式可注入、正式可切换、正式可依赖”的共享基础设施。

### 实施项

1. 新增正式运行时与工厂：
   - `backend/infra/rag/runtime.go`
   - `backend/infra/rag/factory.go`
   - 如有需要，新增 `backend/infra/rag/service.go`
2. 扩展配置：
   - `rag.enabled`
   - `rag.store`
   - `rag.embed.provider`
   - `rag.embed.model`
   - `rag.embed.timeoutMs`
   - `rag.embed.dimension`
   - `rag.reranker.provider`
   - `rag.reranker.timeoutMs`
   - `rag.retrieve.timeoutMs`
   - `rag.ingest.chunkSize`
   - `rag.ingest.chunkOverlap`
3. 收口运行入口：
   - `rag.NewDefaultPipeline()` 保留为本地 fallback
   - 正式业务接入走 `NewRuntimeFromConfig(...)`
4. 消除重复检索路径：
   - 明确 `Pipeline` 是官方检索入口
   - `retrieve/vector_retriever.go` 要么内聚为内部实现，要么后续删除，避免双轨

### 验收

1. 启动期可按配置成功构造 `RAG Runtime`。
2. 业务侧不需要自己组装 `Pipeline / Store / Embedder / Reranker`。
3. 对外暴露面稳定，底层实现可替换。

## 7.2 第二部分：补齐真实底层实现

### 目标

让 `RAG Infra` 具备真实可用的向量能力，而不是停留在 mock。

### 实施项

1. 实现 `embed/eino_embedder.go`
   - 负责 embedding 调用
   - 负责 embedding timeout
   - 负责错误包装与统一日志
2. 实现 `rerank/eino_reranker.go`
   - 负责 rerank 调用
   - 负责 rerank timeout
   - 负责失败降级到原排序
3. 实现 `store/milvus_store.go`
   - `Upsert`
   - `Search`
   - `Delete`
   - `Get`
4. Milvus 元数据设计建议：
   - 高频过滤字段应做显式标量字段，不建议全部依赖大 JSON 过滤
   - 重点字段包括：
     - `corpus`
     - `user_id`
     - `assistant_id`
     - `conversation_id`
     - `run_id`
     - `memory_type`
     - `query_id`
     - `session_id`
     - `domain`

### 验收

1. `MilvusStore` 在已准备好的 Docker 环境中可稳定完成写入与检索。
2. `EinoEmbedder` 和 `EinoReranker` 可按配置启用。
3. provider 波动时，主链路仍能 fallback。

## 7.3 第三部分：补齐工程化能力

### 目标

让 `RAG Infra` 具备“可观测、可测试、可回滚”的基础设施属性。

### 实施项

1. timeout 接线：
   - embedding timeout
   - retrieve timeout
   - rerank timeout
2. 统一日志字段：
   - `trace_id`
   - `corpus`
   - `action`
   - `provider`
   - `latency_ms`
   - `hit_count`
   - `fallback_reason`
3. 指标补齐：
   - `rag_ingest_count`
   - `rag_retrieve_count`
   - `rag_hit_count`
   - `rag_fallback_rate`
   - `rag_latency_ms`
4. 测试补齐：
   - `chunker` 单测
   - `corpus filter` 单测
   - `pipeline fallback` 单测
   - `MilvusStore` 集成测试
   - `memory/web` 过滤隔离测试

### 验收

1. 出现检索问题时，可从日志定位是：
   - 没命中
   - 超时
   - rerank 降级
   - filter 过滤过严
2. 公共层测试可稳定覆盖关键路径。

## 7.4 第四部分：接入 Memory

### 目标

让 `memory` 成为第一个正式接入 `RAG Infra` 的业务域。

### 实施项

1. 写入链路接入：
   - 在 memory worker 成功写入 `memory_items` 后，调用 `RAGRuntime.IngestMemory`
   - 复用 `memory_items.vector_status/vector_id`
2. 读取链路接入：
   - 在 `memory/service/read_service.go` 中新增 `RetrieveMemory` 路径
   - 强制过滤：
     - `user_id`
     - `assistant_id`
     - `conversation_id`
     - `run_id`
3. 开关控制：
   - `memory.rag.enabled=false` 默认关闭
   - 打开后先灰度使用新路径
4. 降级策略：
   - `RAG` 检索失败 -> 回退旧读取链路
   - `Reranker` 失败 -> 保留原始排序

### 验收

1. 开关关闭时行为与当前一致。
2. 开关开启时，记忆召回可稳定工作。
3. 失败时不会影响主链路回复。

## 7.5 第五部分：接入 WebSearch

### 目标

让 `websearch` 成为第二个正式接入 `RAG Infra` 的业务域，并复用 `WebCorpus`。

### 实施项

1. 保留 V1 路径：
   - `web_search` 做 provider 搜索
   - `web_fetch` 做正文抓取与清洗
2. 新增 V2 路径：
   - 把抓取结果映射为 `WebIngestItem`
   - 调 `RAGRuntime.IngestWeb`
   - 再调 `RAGRuntime.RetrieveWeb`
3. 强约束过滤：
   - `query_id` 或 `session_id` 至少有一个
   - 避免跨 query/session 串召回
4. 开关控制：
   - `websearch.rag.enabled=false` 默认关闭
5. 降级策略：
   - `web_rag_search` 失败 -> 回退到 `web_search + web_fetch`

### 验收

1. 新旧链路并存，互不影响。
2. 新链路不会跨 query/session 串数据。
3. 失败可立刻回退到 V1。

## 7.6 第六部分：启动接线与统一管理

### 目标

让 `RAG Runtime` 成为启动期统一装配、统一管理的依赖。

### 实施项

1. 在 `backend/cmd/start.go` 中：
   - 读取 `rag.*` 配置
   - 构造 `RAG Runtime`
   - 注入给 `memory` 与 `newAgent web tools`
2. 统一由启动期管理依赖生命周期：
   - 初始化
   - 健康检查
   - 关闭清理
3. 业务层禁止直接 new 底层实现：
   - 禁止业务自己构建 `MilvusStore`
   - 禁止业务自己构建 `EinoEmbedder`
   - 禁止业务自己拼 `Pipeline`

### 验收

1. 依赖管理集中在启动层。
2. 业务代码只依赖方法入口，不接触底层实现。
3. 后续替换实现时，无需大面积修改业务层代码。

---

## 8. 推荐目录改造方案

建议新增或调整如下文件：

1. `backend/infra/rag/runtime.go`
2. `backend/infra/rag/factory.go`
3. `backend/infra/rag/service.go`
4. `backend/infra/rag/README.md` 或在本文件持续追加
5. `backend/infra/rag/embed/eino_embedder.go`
6. `backend/infra/rag/rerank/eino_reranker.go`
7. `backend/infra/rag/store/milvus_store.go`
8. `backend/infra/rag/core/pipeline_test.go`
9. `backend/infra/rag/chunk/text_chunker_test.go`
10. `backend/infra/rag/corpus/memory_corpus_test.go`
11. `backend/infra/rag/corpus/web_corpus_test.go`
12. `backend/infra/rag/store/milvus_store_integration_test.go`

配套改动文件：

1. `backend/cmd/start.go`
2. `backend/config.example.yaml`
3. `backend/memory/service/read_service.go`
4. `backend/newAgent/tools/registry.go`
5. `backend/agent/通用能力接入文档.md`

---

## 9. 配置建议

建议新增如下配置结构：

```yaml
rag:
  enabled: true
  store: "milvus"
  topK: 8
  threshold: 0.55
  retrieve:
    timeoutMs: 1500
  ingest:
    chunkSize: 400
    chunkOverlap: 80
  embed:
    provider: "eino"
    model: ""
    timeoutMs: 1200
    dimension: 1024
  reranker:
    enabled: true
    provider: "eino"
    timeoutMs: 1200

memory:
  rag:
    enabled: false

websearch:
  rag:
    enabled: false
```

说明：

1. `rag.enabled` 控制公共层是否启用。
2. `memory.rag.enabled` 与 `websearch.rag.enabled` 控制业务级切流。
3. 即使 `rag.enabled=true`，也不代表所有业务立刻默认走新链路。

---

## 10. 回滚策略

推荐回滚顺序如下：

1. 先关业务级开关：
   - `memory.rag.enabled=false`
   - `websearch.rag.enabled=false`
2. 再关重排：
   - `rag.reranker.enabled=false`
3. 再切底层实现：
   - `rag.store=inmemory`
   - `rag.embed.provider=mock`
   - `rag.reranker.provider=noop`
4. 若仍异常，再回退到业务旧链路

这样可以做到：

1. 不因单个 provider 波动打断主流程。
2. 保留最小可用能力。
3. 故障定位粒度更细。

---

## 11. 风险与应对

1. 风险：Milvus 过滤能力与现有 metadata 结构不匹配。
   - 应对：高频过滤字段单独建模，不依赖大 JSON 粗暴过滤。
2. 风险：embedding/rerank provider 波动影响延迟。
   - 应对：超时控制 + fallback + 业务级开关。
3. 风险：业务层绕过 Infra 直接依赖底层实现。
   - 应对：通过 `Runtime` 方法面统一收口，代码评审禁止横向绕过。
4. 风险：新旧检索路径长期并存导致维护成本上升。
   - 应对：本轮先保留兜底，稳定后明确删除旧实现。
5. 风险：跨 query/session 串召回。
   - 应对：`WebRetrieve` 强制校验 `query_id/session_id` 至少其一存在。

---

## 12. 最小落地顺序

如果按“尽快落成可接入 Infra”的优先级来排，本轮建议顺序如下：

1. 先做 `runtime/factory/service`，把依赖注入和方法面收口。
2. 再实现 `MilvusStore + EinoEmbedder + EinoReranker`。
3. 再补 timeout、日志、指标、测试。
4. 然后优先接 `memory`。
5. 最后接 `websearch`。

原因：

1. 若先接业务、不先收口方法面，后面会把底层细节泄露到业务层。
2. 若先接 websearch、不先接 memory，会导致共享 Infra 价值不够集中，面试叙事也不完整。

---

## 13. 本轮完成后的预期收益

完成本方案后，项目会获得以下收益：

1. `memory` 与 `websearch` 共享一套真正可运行的 RAG 基础设施。
2. 业务侧不再重复实现切块、召回、重排与降级逻辑。
3. `infra/rag` 成为正式公共能力，具备统一依赖注入与统一管理能力。
4. 后续新增新语料域时，只需新增 `CorpusAdapter + 方法面`，无需再复制一套 RAG 链路。
5. 项目简历叙事会更完整：
   - “抽象并实现共享 RAG Infra”
   - “统一 Memory/WebSearch 的检索与重排能力”
   - “通过依赖注入与门面方法收口底层复杂度”

---

## 14. 当前建议结论

建议把本轮目标明确为：

1. **不是**“再给 RAG 补几个占位实现”。
2. **而是**“把 `backend/infra/rag` 一次性做成正式可接入的公共基础设施”。

关键落点是两句话：

1. 依赖注入统一由 `infra/rag` 自己负责。
2. 对外只暴露方法入口，业务侧不直接接触底层实现细节。

只要这两点收住，后续 `memory`、`websearch`、甚至更多语料域都会明显更好管理。
