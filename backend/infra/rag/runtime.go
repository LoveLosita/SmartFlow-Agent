package rag

import (
	"context"
	"fmt"
	"runtime/debug"
	"strings"
	"time"

	ragconfig "github.com/LoveLosita/smartflow/backend/infra/rag/config"
	"github.com/LoveLosita/smartflow/backend/infra/rag/core"
	"github.com/LoveLosita/smartflow/backend/infra/rag/corpus"
)

type runtime struct {
	cfg          ragconfig.Config
	pipeline     *core.Pipeline
	memoryCorpus *corpus.MemoryCorpus
	webCorpus    *corpus.WebCorpus
	observer     Observer
}

func newRuntime(cfg ragconfig.Config, pipeline *core.Pipeline, observer Observer) Runtime {
	if observer == nil {
		observer = NewNopObserver()
	}
	return &runtime{
		cfg:          cfg,
		pipeline:     pipeline,
		memoryCorpus: corpus.NewMemoryCorpus(),
		webCorpus:    corpus.NewWebCorpus(),
		observer:     observer,
	}
}

// IngestMemory 统一承接记忆语料入库。
func (r *runtime) IngestMemory(ctx context.Context, req MemoryIngestRequest) (result *IngestResult, err error) {
	defer r.recoverPublicPanic(ctx, req.TraceID, "memory", normalizeAction(req.Action, "add"), "ingest", &err)

	items := make([]corpus.MemoryIngestItem, 0, len(req.Items))
	for _, item := range req.Items {
		items = append(items, corpus.MemoryIngestItem{
			MemoryID:         item.MemoryID,
			UserID:           item.UserID,
			ConversationID:   item.ConversationID,
			AssistantID:      item.AssistantID,
			RunID:            item.RunID,
			MemoryType:       item.MemoryType,
			Title:            item.Title,
			Content:          item.Content,
			Confidence:       item.Confidence,
			Importance:       item.Importance,
			SensitivityLevel: item.SensitivityLevel,
			IsExplicit:       item.IsExplicit,
			Status:           item.Status,
			TTLAt:            item.TTLAt,
			CreatedAt:        item.CreatedAt,
		})
	}
	return r.ingestWithCorpus(ctx, req.TraceID, "memory", r.memoryCorpus, items, req.Action)
}

// RetrieveMemory 统一承接记忆语料检索。
func (r *runtime) RetrieveMemory(ctx context.Context, req MemoryRetrieveRequest) (result *RetrieveResult, err error) {
	defer r.recoverPublicPanic(ctx, req.TraceID, "memory", normalizeAction(req.Action, "search"), "retrieve", &err)

	corpusInput := corpus.MemoryRetrieveInput{
		UserID:         req.UserID,
		ConversationID: req.ConversationID,
		AssistantID:    req.AssistantID,
		RunID:          req.RunID,
	}
	if len(req.MemoryTypes) == 1 {
		corpusInput.MemoryType = req.MemoryTypes[0]
	}

	result, err = r.retrieveWithCorpus(ctx, req.TraceID, "memory", r.memoryCorpus, core.RetrieveRequest{
		Query:       req.Query,
		TopK:        normalizeTopK(req.TopK, r.cfg.TopK),
		Threshold:   normalizeThreshold(req.Threshold, r.cfg.Threshold),
		Action:      normalizeAction(req.Action, "search"),
		CorpusInput: corpusInput,
	})
	if err != nil {
		return nil, err
	}
	if len(req.MemoryTypes) <= 1 {
		return result, nil
	}

	// 1. 当前底层过滤仍以等值条件为主，先保持 Runtime 做多类型二次筛选；
	// 2. 这样可以避免把 “memory_type in (...)” 的实现细节扩散到所有 Store；
	// 3. 等后续底层过滤能力统一后，再考虑把该逻辑继续下沉。
	allowed := make(map[string]struct{}, len(req.MemoryTypes))
	for _, item := range req.MemoryTypes {
		value := strings.TrimSpace(strings.ToLower(item))
		if value == "" {
			continue
		}
		allowed[value] = struct{}{}
	}

	filtered := make([]RetrieveHit, 0, len(result.Items))
	for _, item := range result.Items {
		memoryType := strings.TrimSpace(strings.ToLower(asString(item.Metadata["memory_type"])))
		if len(allowed) > 0 {
			if _, ok := allowed[memoryType]; !ok {
				continue
			}
		}
		filtered = append(filtered, item)
	}
	result.Items = filtered
	if req.TopK > 0 && len(result.Items) > req.TopK {
		result.Items = result.Items[:req.TopK]
	}
	return result, nil
}

// DeleteMemory 删除记忆语料中的指定向量。
func (r *runtime) DeleteMemory(ctx context.Context, documentIDs []string) (err error) {
	defer r.recoverPublicPanic(ctx, "", "memory", "delete", "delete", &err)

	if r == nil || r.pipeline == nil || len(documentIDs) == 0 {
		return nil
	}
	return r.pipeline.Delete(ctx, documentIDs)
}

// IngestWeb 统一承接网页语料入库。
func (r *runtime) IngestWeb(ctx context.Context, req WebIngestRequest) (result *IngestResult, err error) {
	defer r.recoverPublicPanic(ctx, req.TraceID, "web", normalizeAction(req.Action, "add"), "ingest", &err)

	items := make([]corpus.WebIngestItem, 0, len(req.Items))
	for _, item := range req.Items {
		items = append(items, corpus.WebIngestItem{
			URL:         item.URL,
			Title:       item.Title,
			Content:     item.Content,
			Snippet:     item.Snippet,
			Domain:      item.Domain,
			QueryID:     item.QueryID,
			SessionID:   item.SessionID,
			PublishedAt: item.PublishedAt,
			FetchedAt:   item.FetchedAt,
			SourceRank:  item.SourceRank,
		})
	}
	return r.ingestWithCorpus(ctx, req.TraceID, "web", r.webCorpus, items, req.Action)
}

// RetrieveWeb 统一承接网页语料检索。
func (r *runtime) RetrieveWeb(ctx context.Context, req WebRetrieveRequest) (result *RetrieveResult, err error) {
	defer r.recoverPublicPanic(ctx, req.TraceID, "web", normalizeAction(req.Action, "search"), "retrieve", &err)

	return r.retrieveWithCorpus(ctx, req.TraceID, "web", r.webCorpus, core.RetrieveRequest{
		Query:     req.Query,
		TopK:      normalizeTopK(req.TopK, r.cfg.TopK),
		Threshold: normalizeThreshold(req.Threshold, r.cfg.Threshold),
		Action:    normalizeAction(req.Action, "search"),
		CorpusInput: corpus.WebRetrieveInput{
			QueryID:   req.QueryID,
			SessionID: req.SessionID,
			Domain:    req.Domain,
		},
	})
}

func (r *runtime) ingestWithCorpus(
	ctx context.Context,
	traceID string,
	corpusName string,
	adapter core.CorpusAdapter,
	input any,
	action string,
) (*IngestResult, error) {
	start := time.Now()
	if r == nil || r.pipeline == nil || adapter == nil {
		return nil, core.ErrNilDependency
	}

	action = normalizeAction(action, "add")
	observeCtx := newObserveContext(ctx, traceID, corpusName, action)

	docs, err := adapter.BuildIngestDocuments(observeCtx, input)
	if err != nil {
		r.observe(observeCtx, ObserveEvent{
			Level:     ObserveLevelError,
			Component: "runtime",
			Operation: "ingest",
			Fields: map[string]any{
				"status":      "failed",
				"latency_ms":  time.Since(start).Milliseconds(),
				"phase":       "build_documents",
				"error":       err,
				"error_code":  core.ClassifyErrorCode(err),
				"input_count": estimateInputCount(input),
			},
		})
		return nil, err
	}

	docIDs := make([]string, 0, len(docs))
	for _, doc := range docs {
		docIDs = append(docIDs, doc.ID)
	}

	result, err := r.pipeline.IngestDocuments(observeCtx, adapter.Name(), docs, core.IngestOption{
		Chunk: core.ChunkOption{
			ChunkSize:    r.cfg.ChunkSize,
			ChunkOverlap: r.cfg.ChunkOverlap,
		},
		Action: action,
	})
	if err != nil {
		r.observe(observeCtx, ObserveEvent{
			Level:     ObserveLevelError,
			Component: "runtime",
			Operation: "ingest",
			Fields: map[string]any{
				"status":         "failed",
				"latency_ms":     time.Since(start).Milliseconds(),
				"document_count": len(docs),
				"error":          err,
				"error_code":     core.ClassifyErrorCode(err),
			},
		})
		return nil, err
	}

	r.observe(observeCtx, ObserveEvent{
		Level:     ObserveLevelInfo,
		Component: "runtime",
		Operation: "ingest",
		Fields: map[string]any{
			"status":         "success",
			"latency_ms":     time.Since(start).Milliseconds(),
			"document_count": result.DocumentCount,
			"chunk_count":    result.ChunkCount,
		},
	})
	return &IngestResult{
		DocumentCount: result.DocumentCount,
		ChunkCount:    result.ChunkCount,
		DocumentIDs:   docIDs,
	}, nil
}

func (r *runtime) retrieveWithCorpus(
	ctx context.Context,
	traceID string,
	corpusName string,
	adapter core.CorpusAdapter,
	req core.RetrieveRequest,
) (*RetrieveResult, error) {
	start := time.Now()
	if r == nil || r.pipeline == nil || adapter == nil {
		return nil, core.ErrNilDependency
	}

	action := normalizeAction(req.Action, "search")
	req.Action = action
	observeCtx := newObserveContext(ctx, traceID, corpusName, action)

	timeoutCtx := observeCtx
	cancel := func() {}
	if r.cfg.RetrieveTimeoutMS > 0 {
		timeoutCtx, cancel = context.WithTimeout(observeCtx, time.Duration(r.cfg.RetrieveTimeoutMS)*time.Millisecond)
	}
	defer cancel()

	result, err := r.pipeline.Retrieve(timeoutCtx, adapter, req)
	if err != nil {
		r.observe(observeCtx, ObserveEvent{
			Level:     ObserveLevelError,
			Component: "runtime",
			Operation: "retrieve",
			Fields: map[string]any{
				"status":     "failed",
				"latency_ms": time.Since(start).Milliseconds(),
				"query_len":  len(strings.TrimSpace(req.Query)),
				"top_k":      req.TopK,
				"threshold":  req.Threshold,
				"error":      err,
				"error_code": core.ClassifyErrorCode(err),
			},
		})
		return nil, err
	}

	items := make([]RetrieveHit, 0, len(result.Items))
	for _, item := range result.Items {
		items = append(items, RetrieveHit{
			ChunkID:    item.ChunkID,
			DocumentID: item.DocumentID,
			Text:       item.Text,
			Score:      item.Score,
			Metadata:   cloneMap(item.Metadata),
		})
	}

	r.observe(observeCtx, ObserveEvent{
		Level:     ObserveLevelInfo,
		Component: "runtime",
		Operation: "retrieve",
		Fields: map[string]any{
			"status":          "success",
			"latency_ms":      time.Since(start).Milliseconds(),
			"query_len":       len(strings.TrimSpace(req.Query)),
			"top_k":           req.TopK,
			"threshold":       req.Threshold,
			"raw_count":       result.RawCount,
			"hit_count":       len(result.Items),
			"fallback_used":   result.FallbackUsed,
			"fallback_reason": result.FallbackReason,
		},
	})
	return &RetrieveResult{
		Items:          items,
		RawCount:       result.RawCount,
		FallbackUsed:   result.FallbackUsed,
		FallbackReason: result.FallbackReason,
	}, nil
}

func (r *runtime) observe(ctx context.Context, event ObserveEvent) {
	if r == nil || r.observer == nil {
		return
	}
	r.observer.Observe(ctx, event)
}

func (r *runtime) recoverPublicPanic(
	ctx context.Context,
	traceID string,
	corpusName string,
	action string,
	operation string,
	errPtr *error,
) {
	recovered := recover()
	if recovered == nil || errPtr == nil {
		return
	}

	// 1. runtime 是 RAG Infra 对业务侧暴露的最终方法面，任何下层 panic 都不应再穿透到业务协程。
	// 2. 这里统一把 panic 转成 error，并补一条结构化观测，方便继续排查是哪一层依赖失控。
	// 3. 保留 stack 是为了在“进程不崩”的前提下仍能定位根因，避免只剩一句 recovered 无法复盘。
	panicErr := fmt.Errorf("rag runtime panic recovered: corpus=%s operation=%s panic=%v", corpusName, operation, recovered)
	*errPtr = panicErr

	observeCtx := newObserveContext(ctx, traceID, corpusName, action)
	r.observe(observeCtx, ObserveEvent{
		Level:     ObserveLevelError,
		Component: "runtime",
		Operation: operation + "_panic_recovered",
		Fields: map[string]any{
			"status":     "failed",
			"panic":      fmt.Sprintf("%v", recovered),
			"panic_type": fmt.Sprintf("%T", recovered),
			"error":      panicErr,
			"error_code": core.ClassifyErrorCode(panicErr),
			"stack":      string(debug.Stack()),
		},
	})
}

func newObserveContext(ctx context.Context, traceID string, corpusName string, action string) context.Context {
	fields := map[string]any{
		"corpus": corpusName,
		"action": action,
	}
	if traceID = strings.TrimSpace(traceID); traceID != "" {
		fields["trace_id"] = traceID
	}
	return core.WithObserveFields(ctx, fields)
}

func estimateInputCount(input any) int {
	switch value := input.(type) {
	case []corpus.MemoryIngestItem:
		return len(value)
	case []corpus.WebIngestItem:
		return len(value)
	default:
		return 0
	}
}

func normalizeAction(action string, fallback string) string {
	action = strings.TrimSpace(action)
	if action == "" {
		return fallback
	}
	return action
}

func normalizeTopK(topK int, fallback int) int {
	if topK > 0 {
		return topK
	}
	if fallback > 0 {
		return fallback
	}
	return 8
}

func normalizeThreshold(threshold float64, fallback float64) float64 {
	if threshold >= 0 {
		return threshold
	}
	if fallback >= 0 {
		return fallback
	}
	return 0
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

func asString(v any) string {
	if v == nil {
		return ""
	}
	return strings.TrimSpace(fmt.Sprintf("%v", v))
}
