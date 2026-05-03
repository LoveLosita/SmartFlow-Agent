package core

import (
	"context"
	"errors"
	"fmt"
	"log"
	"runtime/debug"
	"strings"
	"time"
)

const (
	defaultTopK       = 8
	defaultThreshold  = 0
	defaultChunkSize  = 400
	defaultChunkOvLap = 80
)

// Pipeline 是 RAG Core 编排器。
//
// 职责边界：
// 1. 负责统一 chunk/embed/retrieve/rerank 流程；
// 2. 负责失败降级语义；
// 3. 不承载任何具体业务语义（由 CorpusAdapter 提供）。
type Pipeline struct {
	chunker  Chunker
	embedder Embedder
	store    VectorStore
	reranker Reranker
	logger   *log.Logger
	observer Observer
}

func NewPipeline(chunker Chunker, embedder Embedder, store VectorStore, reranker Reranker) *Pipeline {
	return &Pipeline{
		chunker:  chunker,
		embedder: embedder,
		store:    store,
		reranker: reranker,
		logger:   log.Default(),
		observer: NewNopObserver(),
	}
}

// SetLogger 设置 Pipeline 使用的日志器。
func (p *Pipeline) SetLogger(logger *log.Logger) {
	if p == nil || logger == nil {
		return
	}
	p.logger = logger
}

// SetObserver 设置 Pipeline 使用的统一观测器。
func (p *Pipeline) SetObserver(observer Observer) {
	if p == nil || observer == nil {
		return
	}
	p.observer = observer
}

// Ingest 执行统一入库流程。
//
// 步骤化说明：
// 1. 先由 CorpusAdapter 生成统一文档，确保不同语料入口一致；
// 2. 再统一切块与向量化，避免业务侧重复实现；
// 3. 最后一次性 Upsert，失败直接返回，交由上层决定是否重试。
func (p *Pipeline) Ingest(
	ctx context.Context,
	corpus CorpusAdapter,
	input any,
	opt IngestOption,
) (result *IngestResult, err error) {
	defer p.recoverExecutionPanic(ctx, "ingest", &err)

	if p == nil || p.chunker == nil || p.embedder == nil || p.store == nil {
		return nil, ErrNilDependency
	}
	if corpus == nil {
		return nil, errors.New("nil corpus adapter")
	}

	docs, err := corpus.BuildIngestDocuments(ctx, input)
	if err != nil {
		return nil, err
	}
	return p.IngestDocuments(ctx, corpus.Name(), docs, opt)
}

// IngestDocuments 执行“已标准化文档”的统一入库流程。
//
// 职责边界：
// 1. 负责处理已经完成 CorpusAdapter 映射的标准文档；
// 2. 负责统一切块、向量化与 Upsert；
// 3. 不负责再做业务输入解析，避免 Runtime 为拿到 document_id 重复 build 文档。
func (p *Pipeline) IngestDocuments(
	ctx context.Context,
	corpusName string,
	docs []SourceDocument,
	opt IngestOption,
) (result *IngestResult, err error) {
	defer p.recoverExecutionPanic(ctx, "ingest_documents", &err)

	if p == nil || p.chunker == nil || p.embedder == nil || p.store == nil {
		return nil, ErrNilDependency
	}
	if len(docs) == 0 {
		return &IngestResult{DocumentCount: 0, ChunkCount: 0}, nil
	}

	chunkOpt := normalizeChunkOption(opt.Chunk)
	chunks := make([]Chunk, 0, len(docs)*2)
	for _, doc := range docs {
		// 1. 对每个文档独立切块，失败直接中断，避免写入半成品。
		docChunks, chunkErr := p.chunker.Chunk(ctx, doc, chunkOpt)
		if chunkErr != nil {
			return nil, chunkErr
		}
		chunks = append(chunks, docChunks...)
	}
	if len(chunks) == 0 {
		return &IngestResult{DocumentCount: len(docs), ChunkCount: 0}, nil
	}

	texts := make([]string, 0, len(chunks))
	for _, chunk := range chunks {
		texts = append(texts, chunk.Text)
	}

	action := strings.TrimSpace(opt.Action)
	if action == "" {
		action = "add"
	}
	vectors, err := p.embedder.Embed(ctx, texts, action)
	if err != nil {
		return nil, err
	}
	if len(vectors) != len(chunks) {
		return nil, fmt.Errorf("embedding result length mismatch: chunks=%d vectors=%d", len(chunks), len(vectors))
	}

	rows := make([]VectorRow, 0, len(chunks))
	now := time.Now()
	for i, chunk := range chunks {
		metadata := cloneMap(chunk.Metadata)
		metadata["corpus"] = corpusName
		metadata["document_id"] = chunk.DocumentID
		metadata["chunk_order"] = chunk.Order
		rows = append(rows, VectorRow{
			ID:        chunk.ID,
			Vector:    vectors[i],
			Text:      chunk.Text,
			Metadata:  metadata,
			CreatedAt: now,
			UpdatedAt: now,
		})
	}

	if err = p.store.Upsert(ctx, rows); err != nil {
		return nil, err
	}
	return &IngestResult{
		DocumentCount: len(docs),
		ChunkCount:    len(chunks),
	}, nil
}

// Retrieve 执行统一检索流程。
//
// 步骤化说明：
// 1. 先做 query 向量化与向量检索；
// 2. 再执行阈值过滤，减少低质量候选；
// 3. 最后可选 rerank，若失败则降级回原排序并打日志。
func (p *Pipeline) Retrieve(
	ctx context.Context,
	corpus CorpusAdapter,
	req RetrieveRequest,
) (result *RetrieveResult, err error) {
	defer p.recoverExecutionPanic(ctx, "retrieve", &err)

	if p == nil || p.embedder == nil || p.store == nil {
		return nil, ErrNilDependency
	}
	query := strings.TrimSpace(req.Query)
	if query == "" {
		return nil, ErrInvalidQuery
	}

	topK := req.TopK
	if topK <= 0 {
		topK = defaultTopK
	}
	threshold := req.Threshold
	if threshold < 0 {
		threshold = defaultThreshold
	}

	filter := cloneMap(req.Filter)
	if corpus != nil {
		// 1. 先拼接 corpus 过滤条件，避免跨语料串召回。
		corpusFilter, err := corpus.BuildRetrieveFilter(ctx, req.CorpusInput)
		if err != nil {
			return nil, err
		}
		filter = mergeMap(filter, corpusFilter)
		filter["corpus"] = corpus.Name()
	}

	action := strings.TrimSpace(req.Action)
	if action == "" {
		action = "search"
	}

	vectors, err := p.embedder.Embed(ctx, []string{query}, action)
	if err != nil {
		return nil, err
	}
	if len(vectors) != 1 {
		return nil, fmt.Errorf("embedding query length mismatch: %d", len(vectors))
	}

	scoredRows, err := p.store.Search(ctx, VectorSearchRequest{
		QueryVector: vectors[0],
		TopK:        topK,
		Filter:      filter,
	})
	if err != nil {
		return nil, err
	}

	rawCount := len(scoredRows)
	candidates := make([]ScoredChunk, 0, len(scoredRows))
	for _, row := range scoredRows {
		if row.Score < threshold {
			continue
		}
		candidates = append(candidates, ScoredChunk{
			ChunkID:    row.Row.ID,
			DocumentID: asString(row.Row.Metadata["document_id"]),
			Text:       row.Row.Text,
			Score:      row.Score,
			Metadata:   cloneMap(row.Row.Metadata),
		})
	}

	result = &RetrieveResult{
		Items:        candidates,
		RawCount:     rawCount,
		FallbackUsed: false,
	}
	if len(candidates) == 0 || p.reranker == nil {
		return result, nil
	}

	reranked, rerankErr := p.reranker.Rerank(ctx, query, candidates, topK)
	if rerankErr != nil {
		// 2. rerank 异常不终止主流程，统一降级为原排序。
		result.FallbackUsed = true
		result.FallbackReason = FallbackReasonRerankFailed
		if p.observer != nil {
			p.observer.Observe(ctx, ObserveEvent{
				Level:     ObserveLevelWarn,
				Component: "pipeline",
				Operation: "rerank_fallback",
				Fields: map[string]any{
					"status":          "fallback",
					"fallback_reason": FallbackReasonRerankFailed,
					"candidate_count": len(candidates),
					"top_k":           topK,
					"error":           rerankErr,
					"error_code":      ClassifyErrorCode(rerankErr),
				},
			})
		} else if p.logger != nil {
			p.logger.Printf("rag rerank fallback: reason=%s err=%v", FallbackReasonRerankFailed, rerankErr)
		}
		return result, nil
	}
	result.Items = reranked
	return result, nil
}

// Delete 删除指定 ID 的向量。
func (p *Pipeline) Delete(ctx context.Context, ids []string) error {
	if p == nil || p.store == nil {
		return nil
	}
	return p.store.Delete(ctx, ids)
}

func (p *Pipeline) recoverExecutionPanic(ctx context.Context, operation string, errPtr *error) {
	recovered := recover()
	if recovered == nil || errPtr == nil {
		return
	}

	panicErr := fmt.Errorf("rag pipeline panic recovered: operation=%s panic=%v", operation, recovered)
	*errPtr = panicErr

	// 1. Pipeline 是 chunk/embed/store/rerank 的统一编排边界，第三方依赖异常不应直接杀掉上层请求。
	// 2. 这里统一 recover 后继续走 error 语义，让 runtime/service 决定降级、回退或记日志。
	// 3. stack 只写观测层，不塞进返回值，避免把超长堆栈直接暴露给上层业务错误文案。
	if p != nil && p.observer != nil {
		p.observer.Observe(ctx, ObserveEvent{
			Level:     ObserveLevelError,
			Component: "pipeline",
			Operation: operation + "_panic_recovered",
			Fields: map[string]any{
				"status":     "failed",
				"panic":      fmt.Sprintf("%v", recovered),
				"panic_type": fmt.Sprintf("%T", recovered),
				"error":      panicErr,
				"error_code": ClassifyErrorCode(panicErr),
				"stack":      string(debug.Stack()),
			},
		})
		return
	}
	if p != nil && p.logger != nil {
		p.logger.Printf("rag pipeline panic recovered: operation=%s panic=%v stack=%s", operation, recovered, string(debug.Stack()))
	}
}

func normalizeChunkOption(opt ChunkOption) ChunkOption {
	if opt.ChunkSize <= 0 {
		opt.ChunkSize = defaultChunkSize
	}
	if opt.ChunkOverlap < 0 {
		opt.ChunkOverlap = 0
	}
	if opt.ChunkOverlap >= opt.ChunkSize {
		opt.ChunkOverlap = defaultChunkOvLap
		if opt.ChunkOverlap >= opt.ChunkSize {
			opt.ChunkOverlap = opt.ChunkSize / 5
		}
	}
	return opt
}

func cloneMap(src map[string]any) map[string]any {
	if len(src) == 0 {
		return map[string]any{}
	}
	dst := make(map[string]any, len(src))
	for key, value := range src {
		dst[key] = value
	}
	return dst
}

func mergeMap(base map[string]any, ext map[string]any) map[string]any {
	if base == nil {
		base = map[string]any{}
	}
	for key, value := range ext {
		base[key] = value
	}
	return base
}

func asString(v any) string {
	if v == nil {
		return ""
	}
	return fmt.Sprintf("%v", v)
}
