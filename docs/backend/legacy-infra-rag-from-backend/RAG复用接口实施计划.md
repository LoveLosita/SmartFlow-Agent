# RAG 复用接口实施计划（Memory + WebSearch 统一底座）

## 1. 目标与原则

1. 在 `backend/infra/rag` 抽离共享 RAG Core，统一 `chunk/embed/retrieve/rerank` 能力。
2. 先接入 `MemoryCorpus` 与 `WebCorpus` 两个适配器，避免后续重复造轮子。
3. 保持“并行迁移”策略：新老链路并存，先接入、再灰度、再切流、最后删除旧实现。
4. 不阻塞现有主链路；任何 RAG 子能力失败都必须可降级。

## 2. 本轮范围与非目标

### 2.1 本轮范围

1. 定义 RAG Core 接口、标准数据结构、错误码和回退语义。
2. 提供 `MemoryCorpus` 与 `WebCorpus` 适配层设计。
3. 给出分阶段落地步骤、验收标准、风险控制。

### 2.2 本轮非目标

1. 不在本轮实现完整生产级向量检索细节（Milvus 连接器可先占位）。
2. 不在本轮统一改造所有调用方，只做首批接入点。
3. 不在本轮引入多 Provider 工厂（先保证单 Provider 可替换）。

## 3. 目录与模块规划

建议目录（先建骨架，逐轮填实）：

```text
backend/infra/rag/
  core/
    types.go
    interfaces.go
    pipeline.go
    errors.go
  chunk/
    text_chunker.go
  embed/
    eino_embedder.go
  retrieve/
    vector_retriever.go
  rerank/
    eino_reranker.go
  store/
    vector_store.go
    milvus_store.go
  corpus/
    memory_corpus.go
    web_corpus.go
  config/
    config.go
```

## 4. 核心接口设计（建议签名）

```go
type Chunker interface {
    Chunk(ctx context.Context, doc SourceDocument, opt ChunkOption) ([]Chunk, error)
}

type Embedder interface {
    Embed(ctx context.Context, texts []string, action string) ([][]float32, error)
}

type Retriever interface {
    Retrieve(ctx context.Context, req RetrieveRequest) ([]ScoredChunk, error)
}

type Reranker interface {
    Rerank(ctx context.Context, query string, candidates []ScoredChunk, topK int) ([]ScoredChunk, error)
}

type VectorStore interface {
    Upsert(ctx context.Context, rows []VectorRow) error
    Search(ctx context.Context, req VectorSearchRequest) ([]ScoredVectorRow, error)
    Delete(ctx context.Context, ids []string) error
    Get(ctx context.Context, ids []string) ([]VectorRow, error)
}

type CorpusAdapter interface {
    Name() string
    BuildIngestDocuments(ctx context.Context, input any) ([]SourceDocument, error)
    BuildRetrieveFilter(ctx context.Context, req any) (map[string]any, error)
}
```

## 5. 统一流程约定

### 5.1 Ingest 流程

1. `CorpusAdapter.BuildIngestDocuments` 生成标准文档。
2. `Chunker.Chunk` 切块（固定 chunk_size + overlap）。
3. `Embedder.Embed(action=add/update)` 生成向量。
4. `VectorStore.Upsert` 写入。
5. 任一步失败按“可补偿”记录状态，不影响主业务成功返回。

### 5.2 Retrieve 流程

1. `CorpusAdapter.BuildRetrieveFilter` 构建过滤条件。
2. `Embedder.Embed(action=search)` 向量化 query。
3. `VectorStore.Search` 召回候选。
4. `threshold` 过滤。
5. 可选 `Reranker` 重排；失败则 fallback 到原排序并记录原因码。

## 6. 两类 Corpus 适配器设计

### 6.1 MemoryCorpus

1. 数据源：`memory_items`（结构化记忆事实）。
2. 强约束过滤：`user_id + assistant_id + conversation_id`。
3. 元数据：`memory_type/confidence/sensitivity_level/ttl_at/source_event_id`。
4. 注入优先级：`constraint/preference` 高于 `fact/todo_hint`。

### 6.2 WebCorpus

1. 数据源：websearch 抓取结果（`url/title/snippet/content`）。
2. 强约束过滤：`query_id/session_id`，避免跨问题污染。
3. 元数据：`domain/published_at/fetched_at/language/source_rank`。
4. 检索策略：先向量召回，再结合域名可信度做轻量加权。

## 7. 与 Eino 的集成方式

1. `embed/eino_embedder.go`：封装 Eino embedding 调用。
2. `rerank/eino_reranker.go`：封装 Eino 重排调用。
3. 统一配置入口：`rag.enabled/top_k/threshold/reranker_enabled/timeout`。
4. 统一日志字段：`trace_id/corpus/action/fallback_reason/latency_ms/hit_count`。

## 8. 分阶段实施（建议 4 轮）

### Round 1：基础骨架（不切流）

1. 建 `infra/rag` 目录与接口、类型、错误码。
2. 提供 `NoopReranker`、`MockEmbedder` 兜底实现。
3. 验收：编译通过，主链路行为不变。

### Round 2：MemoryCorpus 接入（灰度）

1. 把记忆检索从“模块内直连”改为调用 RAG Core。
2. 保留旧路径开关 `memory.rag.enabled`，默认关闭。
3. 验收：开启开关后功能等价，失败可自动降级旧链路。

### Round 3：WebCorpus 接入（灰度）

1. websearch 召回改走 RAG Core。
2. 加入 `web.rag.enabled` 灰度开关。
3. 验收：检索可复用同一 pipeline，质量不低于旧实现。

### Round 4：统一切流与清理

1. 默认开启 RAG Core，旧链路保留一段观察窗口。
2. 指标稳定后删除旧实现。
3. 验收：两条业务链路均通过统一接口，文档与监控齐全。

## 9. 配置建议

```yaml
rag:
  enabled: true
  topK: 8
  threshold: 0.55
  reranker:
    enabled: true
    timeoutMs: 1200
  ingest:
    chunkSize: 400
    chunkOverlap: 80
  retrieve:
    timeoutMs: 1500
```

## 10. 验收标准（DoD）

1. 同一套 Core 能同时服务 Memory 与 WebSearch。
2. `rerank` 异常时可观测地降级，不影响主功能可用性。
3. 支持按 corpus 维度查看命中率、耗时、降级率。
4. 新老链路可开关切换，回滚路径明确。

## 11. 风险与应对

1. 风险：一次性切流影响面大。  
   应对：按 corpus 分轮灰度，先 Memory 后 Web。
2. 风险：向量检索延迟波动。  
   应对：超时控制 + fallback + 本地缓存热点 query。
3. 风险：跨域检索串数据。  
   应对：强制 filter 校验，不满足维度直接拒绝检索。

## 12. 下一步执行清单（紧接实现）

1. 先补 `core/interfaces.go + core/types.go + core/pipeline.go`。
2. 再补 `corpus/memory_corpus.go`（首个适配器）。
3. 然后给 websearch 接 `corpus/web_corpus.go` 占位适配器。
4. 最后补 `store/milvus_store.go` 与配置接线（当前 docker compose 已准备 Milvus 依赖）。
