package store

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/LoveLosita/smartflow/backend/infra/rag/core"
)

// MilvusConfig 描述 Milvus REST 存储配置。
type MilvusConfig struct {
	// Address 应指向 Milvus REST 入口。
	// 当前项目联调验证使用 19530；9091 仅用于 health/metrics，不承载本文实现所走的 REST API。
	Address          string
	Token            string
	DBName           string
	CollectionName   string
	RequestTimeoutMS int
	Dimension        int
	MetricType       string
	Logger           *log.Logger
	Observer         core.Observer
}

// MilvusStore 是基于 Milvus REST API 的向量存储实现。
//
// 设计说明：
// 1. 本实现优先保证“项目内可接入、可管理、可灰度”，不强依赖额外 SDK；
// 2. 通过固定字段 + metadata JSON 的方式兼顾过滤能力与元数据完整性；
// 3. collection 在首次写入时自动创建，避免启动期额外初始化脚本。
type MilvusStore struct {
	cfg      MilvusConfig
	client   *http.Client
	observer core.Observer
	mu       sync.Mutex
	ensured  bool
}

const (
	milvusPrimaryField   = "id"
	milvusVectorField    = "vector"
	milvusTextField      = "text"
	milvusMetadataField  = "metadata"
	milvusCorpusField    = "corpus"
	milvusDocumentField  = "document_id"
	milvusUserIDField    = "user_id"
	milvusAssistantField = "assistant_id"
	milvusConvField      = "conversation_id"
	milvusRunField       = "run_id"
	milvusMemoryType     = "memory_type"
	milvusQueryIDField   = "query_id"
	milvusSessionField   = "session_id"
	milvusDomainField    = "domain"
	milvusChunkOrder     = "chunk_order"
	milvusUpdatedAtField = "updated_at"
)

var milvusFilterFieldMap = map[string]string{
	"corpus":          milvusCorpusField,
	"document_id":     milvusDocumentField,
	"user_id":         milvusUserIDField,
	"assistant_id":    milvusAssistantField,
	"conversation_id": milvusConvField,
	"run_id":          milvusRunField,
	"memory_type":     milvusMemoryType,
	"query_id":        milvusQueryIDField,
	"session_id":      milvusSessionField,
	"domain":          milvusDomainField,
	"chunk_order":     milvusChunkOrder,
}

func NewMilvusStore(cfg MilvusConfig) (*MilvusStore, error) {
	cfg.Address = strings.TrimRight(strings.TrimSpace(cfg.Address), "/")
	if cfg.Address == "" {
		return nil, errors.New("milvus address is empty")
	}
	if cfg.CollectionName == "" {
		cfg.CollectionName = "smartflow_rag_chunks"
	}
	if cfg.MetricType == "" {
		cfg.MetricType = "COSINE"
	}
	if cfg.RequestTimeoutMS <= 0 {
		cfg.RequestTimeoutMS = 1500
	}
	if cfg.Logger == nil {
		cfg.Logger = log.Default()
	}
	if cfg.Observer == nil {
		cfg.Observer = core.NewLoggerObserver(cfg.Logger)
	}

	return &MilvusStore{
		cfg:      cfg,
		client:   &http.Client{Timeout: time.Duration(cfg.RequestTimeoutMS) * time.Millisecond},
		observer: cfg.Observer,
	}, nil
}

func (s *MilvusStore) Upsert(ctx context.Context, rows []core.VectorRow) error {
	if err := s.ensureReady(); err != nil {
		return err
	}
	start := time.Now()
	if len(rows) == 0 {
		return nil
	}
	if err := s.ensureCollection(ctx, len(rows[0].Vector)); err != nil {
		s.observe(ctx, core.ObserveEvent{
			Level:     core.ObserveLevelError,
			Component: "store",
			Operation: "upsert",
			Fields: map[string]any{
				"status":     "failed",
				"row_count":  len(rows),
				"vector_dim": len(rows[0].Vector),
				"latency_ms": time.Since(start).Milliseconds(),
				"error":      err,
				"error_code": core.ClassifyErrorCode(err),
			},
		})
		return err
	}

	data := make([]map[string]any, 0, len(rows))
	for _, row := range rows {
		item := mapRowToMilvusEntity(row)
		data = append(data, item)
	}

	_, err := s.postJSON(ctx, "/v2/vectordb/entities/upsert", map[string]any{
		"collectionName": s.cfg.CollectionName,
		"data":           data,
		"dbName":         blankToNil(s.cfg.DBName),
	})
	if err != nil {
		s.observe(ctx, core.ObserveEvent{
			Level:     core.ObserveLevelError,
			Component: "store",
			Operation: "upsert",
			Fields: map[string]any{
				"status":     "failed",
				"row_count":  len(rows),
				"vector_dim": len(rows[0].Vector),
				"latency_ms": time.Since(start).Milliseconds(),
				"error":      err,
				"error_code": core.ClassifyErrorCode(err),
			},
		})
		return err
	}
	s.observe(ctx, core.ObserveEvent{
		Level:     core.ObserveLevelInfo,
		Component: "store",
		Operation: "upsert",
		Fields: map[string]any{
			"status":     "success",
			"row_count":  len(rows),
			"vector_dim": len(rows[0].Vector),
			"latency_ms": time.Since(start).Milliseconds(),
		},
	})
	return err
}

func (s *MilvusStore) Search(ctx context.Context, req core.VectorSearchRequest) ([]core.ScoredVectorRow, error) {
	if err := s.ensureReady(); err != nil {
		return nil, err
	}
	start := time.Now()
	if len(req.QueryVector) == 0 {
		return nil, nil
	}

	if err := s.ensureCollection(ctx, len(req.QueryVector)); err != nil {
		if isMilvusCollectionMissing(err) {
			return nil, nil
		}
		s.observe(ctx, core.ObserveEvent{
			Level:     core.ObserveLevelError,
			Component: "store",
			Operation: "search",
			Fields: map[string]any{
				"status":       "failed",
				"top_k":        req.TopK,
				"filter_count": len(req.Filter),
				"vector_dim":   len(req.QueryVector),
				"latency_ms":   time.Since(start).Milliseconds(),
				"error":        err,
				"error_code":   core.ClassifyErrorCode(err),
			},
		})
		return nil, err
	}

	filterExpr, err := buildMilvusFilter(req.Filter)
	if err != nil {
		s.observe(ctx, core.ObserveEvent{
			Level:     core.ObserveLevelError,
			Component: "store",
			Operation: "search",
			Fields: map[string]any{
				"status":       "failed",
				"top_k":        req.TopK,
				"filter_count": len(req.Filter),
				"vector_dim":   len(req.QueryVector),
				"latency_ms":   time.Since(start).Milliseconds(),
				"error":        err,
				"error_code":   core.ClassifyErrorCode(err),
			},
		})
		return nil, err
	}

	body := map[string]any{
		"collectionName": s.cfg.CollectionName,
		"data":           [][]float32{req.QueryVector},
		"annsField":      milvusVectorField,
		"limit":          normalizeMilvusTopK(req.TopK),
		"outputFields":   milvusOutputFields(false),
	}
	if filterExpr != "" {
		body["filter"] = filterExpr
	}
	if s.cfg.DBName != "" {
		body["dbName"] = s.cfg.DBName
	}

	respBody, err := s.postJSON(ctx, "/v2/vectordb/entities/search", body)
	if err != nil {
		if isMilvusCollectionMissing(err) {
			return nil, nil
		}
		s.observe(ctx, core.ObserveEvent{
			Level:     core.ObserveLevelError,
			Component: "store",
			Operation: "search",
			Fields: map[string]any{
				"status":       "failed",
				"top_k":        req.TopK,
				"filter_count": len(req.Filter),
				"vector_dim":   len(req.QueryVector),
				"latency_ms":   time.Since(start).Milliseconds(),
				"error":        err,
				"error_code":   core.ClassifyErrorCode(err),
			},
		})
		return nil, err
	}

	var resp milvusSearchResponse
	if err = json.Unmarshal(respBody, &resp); err != nil {
		s.observe(ctx, core.ObserveEvent{
			Level:     core.ObserveLevelError,
			Component: "store",
			Operation: "search",
			Fields: map[string]any{
				"status":       "failed",
				"top_k":        req.TopK,
				"filter_count": len(req.Filter),
				"vector_dim":   len(req.QueryVector),
				"latency_ms":   time.Since(start).Milliseconds(),
				"error":        err,
				"error_code":   core.ClassifyErrorCode(err),
			},
		})
		return nil, err
	}
	if resp.Code != 0 && resp.Code != 200 {
		err = fmt.Errorf("milvus search failed: code=%d message=%s", resp.Code, resp.Message)
		s.observe(ctx, core.ObserveEvent{
			Level:     core.ObserveLevelError,
			Component: "store",
			Operation: "search",
			Fields: map[string]any{
				"status":       "failed",
				"top_k":        req.TopK,
				"filter_count": len(req.Filter),
				"vector_dim":   len(req.QueryVector),
				"latency_ms":   time.Since(start).Milliseconds(),
				"error":        err,
				"error_code":   core.ClassifyErrorCode(err),
			},
		})
		return nil, err
	}

	result := make([]core.ScoredVectorRow, 0, len(resp.Data))
	for _, item := range resp.Data {
		row, score := item.toVectorRow()
		result = append(result, core.ScoredVectorRow{
			Row:   row,
			Score: score,
		})
	}
	s.observe(ctx, core.ObserveEvent{
		Level:     core.ObserveLevelInfo,
		Component: "store",
		Operation: "search",
		Fields: map[string]any{
			"status":       "success",
			"top_k":        req.TopK,
			"filter_count": len(req.Filter),
			"vector_dim":   len(req.QueryVector),
			"result_count": len(result),
			"latency_ms":   time.Since(start).Milliseconds(),
		},
	})
	return result, nil
}

func (s *MilvusStore) Delete(ctx context.Context, ids []string) error {
	if err := s.ensureReady(); err != nil {
		return err
	}
	start := time.Now()
	if len(ids) == 0 {
		return nil
	}
	filter := fmt.Sprintf(`%s in [%s]`, milvusPrimaryField, joinQuotedStrings(ids))
	_, err := s.postJSON(ctx, "/v2/vectordb/entities/delete", map[string]any{
		"collectionName": s.cfg.CollectionName,
		"filter":         filter,
		"dbName":         blankToNil(s.cfg.DBName),
	})
	if isMilvusCollectionMissing(err) {
		return nil
	}
	if err != nil {
		s.observe(ctx, core.ObserveEvent{
			Level:     core.ObserveLevelError,
			Component: "store",
			Operation: "delete",
			Fields: map[string]any{
				"status":     "failed",
				"id_count":   len(ids),
				"latency_ms": time.Since(start).Milliseconds(),
				"error":      err,
				"error_code": core.ClassifyErrorCode(err),
			},
		})
		return err
	}
	s.observe(ctx, core.ObserveEvent{
		Level:     core.ObserveLevelInfo,
		Component: "store",
		Operation: "delete",
		Fields: map[string]any{
			"status":     "success",
			"id_count":   len(ids),
			"latency_ms": time.Since(start).Milliseconds(),
		},
	})
	return err
}

func (s *MilvusStore) Get(ctx context.Context, ids []string) ([]core.VectorRow, error) {
	if err := s.ensureReady(); err != nil {
		return nil, err
	}
	start := time.Now()
	if len(ids) == 0 {
		return nil, nil
	}

	respBody, err := s.postJSON(ctx, "/v2/vectordb/entities/get", map[string]any{
		"collectionName": s.cfg.CollectionName,
		"id":             ids,
		"outputFields":   milvusOutputFields(true),
		"dbName":         blankToNil(s.cfg.DBName),
	})
	if err != nil {
		if isMilvusCollectionMissing(err) {
			return nil, nil
		}
		s.observe(ctx, core.ObserveEvent{
			Level:     core.ObserveLevelError,
			Component: "store",
			Operation: "get",
			Fields: map[string]any{
				"status":     "failed",
				"id_count":   len(ids),
				"latency_ms": time.Since(start).Milliseconds(),
				"error":      err,
				"error_code": core.ClassifyErrorCode(err),
			},
		})
		return nil, err
	}

	var resp milvusGetResponse
	if err = json.Unmarshal(respBody, &resp); err != nil {
		s.observe(ctx, core.ObserveEvent{
			Level:     core.ObserveLevelError,
			Component: "store",
			Operation: "get",
			Fields: map[string]any{
				"status":     "failed",
				"id_count":   len(ids),
				"latency_ms": time.Since(start).Milliseconds(),
				"error":      err,
				"error_code": core.ClassifyErrorCode(err),
			},
		})
		return nil, err
	}
	if resp.Code != 0 && resp.Code != 200 {
		err = fmt.Errorf("milvus get failed: code=%d message=%s", resp.Code, resp.Message)
		s.observe(ctx, core.ObserveEvent{
			Level:     core.ObserveLevelError,
			Component: "store",
			Operation: "get",
			Fields: map[string]any{
				"status":     "failed",
				"id_count":   len(ids),
				"latency_ms": time.Since(start).Milliseconds(),
				"error":      err,
				"error_code": core.ClassifyErrorCode(err),
			},
		})
		return nil, err
	}

	rows := make([]core.VectorRow, 0, len(resp.Data))
	for _, item := range resp.Data {
		rows = append(rows, mapMilvusRow(item, true))
	}
	s.observe(ctx, core.ObserveEvent{
		Level:     core.ObserveLevelInfo,
		Component: "store",
		Operation: "get",
		Fields: map[string]any{
			"status":     "success",
			"id_count":   len(ids),
			"row_count":  len(rows),
			"latency_ms": time.Since(start).Milliseconds(),
		},
	})
	return rows, nil
}

func (s *MilvusStore) ensureCollection(ctx context.Context, dimension int) error {
	if err := s.ensureReady(); err != nil {
		return err
	}
	start := time.Now()
	if dimension <= 0 {
		dimension = s.cfg.Dimension
	}
	if dimension <= 0 {
		return errors.New("milvus vector dimension is invalid")
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if s.ensured {
		return nil
	}

	payload := map[string]any{
		"collectionName": s.cfg.CollectionName,
		"schema": map[string]any{
			"autoId":              false,
			"enabledDynamicField": false,
			"fields": []map[string]any{
				buildVarcharField(milvusPrimaryField, true, 256),
				buildVectorField(milvusVectorField, dimension),
				buildVarcharField(milvusTextField, false, 65535),
				{"fieldName": milvusMetadataField, "dataType": "JSON"},
				buildVarcharField(milvusCorpusField, false, 64),
				buildVarcharField(milvusDocumentField, false, 256),
				{"fieldName": milvusUserIDField, "dataType": "Int64"},
				buildVarcharField(milvusAssistantField, false, 128),
				buildVarcharField(milvusConvField, false, 128),
				buildVarcharField(milvusRunField, false, 128),
				buildVarcharField(milvusMemoryType, false, 64),
				buildVarcharField(milvusQueryIDField, false, 128),
				buildVarcharField(milvusSessionField, false, 128),
				buildVarcharField(milvusDomainField, false, 128),
				{"fieldName": milvusChunkOrder, "dataType": "Int64"},
				{"fieldName": milvusUpdatedAtField, "dataType": "Int64"},
			},
		},
		"indexParams": []map[string]any{
			{
				"fieldName":  milvusVectorField,
				"indexName":  milvusVectorField,
				"metricType": s.cfg.MetricType,
				"indexType":  "AUTOINDEX",
			},
		},
	}
	if s.cfg.DBName != "" {
		payload["dbName"] = s.cfg.DBName
	}

	_, err := s.postJSON(ctx, "/v2/vectordb/collections/create", payload)
	if err != nil {
		if isMilvusAlreadyExists(err) {
			s.ensured = true
			s.observe(ctx, core.ObserveEvent{
				Level:     core.ObserveLevelInfo,
				Component: "store",
				Operation: "ensure_collection",
				Fields: map[string]any{
					"status":      "already_exists",
					"vector_dim":  dimension,
					"metric_type": s.cfg.MetricType,
					"latency_ms":  time.Since(start).Milliseconds(),
				},
			})
			return nil
		}
		s.observe(ctx, core.ObserveEvent{
			Level:     core.ObserveLevelError,
			Component: "store",
			Operation: "ensure_collection",
			Fields: map[string]any{
				"status":      "failed",
				"vector_dim":  dimension,
				"metric_type": s.cfg.MetricType,
				"latency_ms":  time.Since(start).Milliseconds(),
				"error":       err,
				"error_code":  core.ClassifyErrorCode(err),
			},
		})
		return err
	}
	s.ensured = true
	s.observe(ctx, core.ObserveEvent{
		Level:     core.ObserveLevelInfo,
		Component: "store",
		Operation: "ensure_collection",
		Fields: map[string]any{
			"status":      "created",
			"vector_dim":  dimension,
			"metric_type": s.cfg.MetricType,
			"latency_ms":  time.Since(start).Milliseconds(),
		},
	})
	return nil
}

func (s *MilvusStore) postJSON(ctx context.Context, path string, payload map[string]any) ([]byte, error) {
	if err := s.ensureReady(); err != nil {
		return nil, err
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.cfg.Address+path, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	if token := strings.TrimSpace(s.cfg.Token); token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	resp, err := s.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	respBody, readErr := io.ReadAll(resp.Body)
	if readErr != nil {
		return nil, readErr
	}

	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("milvus http failed: status=%d body=%s", resp.StatusCode, string(respBody))
	}

	var basic milvusBasicResponse
	if jsonErr := json.Unmarshal(respBody, &basic); jsonErr == nil {
		if basic.Code != 0 && basic.Code != 200 {
			return nil, fmt.Errorf("milvus api failed: code=%d message=%s", basic.Code, basic.Message)
		}
	}

	return respBody, nil
}

func (s *MilvusStore) ensureReady() error {
	if s == nil || s.client == nil {
		return errors.New("milvus store is not initialized")
	}
	if strings.TrimSpace(s.cfg.Address) == "" {
		return errors.New("milvus address is empty")
	}
	if strings.TrimSpace(s.cfg.CollectionName) == "" {
		return errors.New("milvus collection name is empty")
	}
	return nil
}

func (s *MilvusStore) observe(ctx context.Context, event core.ObserveEvent) {
	if s == nil || s.observer == nil {
		return
	}

	fields := cloneMap(event.Fields)
	fields["store"] = "milvus"
	fields["collection"] = s.cfg.CollectionName
	if strings.TrimSpace(s.cfg.DBName) != "" {
		fields["db_name"] = s.cfg.DBName
	}

	s.observer.Observe(ctx, core.ObserveEvent{
		Level:     event.Level,
		Component: event.Component,
		Operation: event.Operation,
		Fields:    fields,
	})
}

func mapRowToMilvusEntity(row core.VectorRow) map[string]any {
	metadata := cloneMap(row.Metadata)
	entity := map[string]any{
		milvusPrimaryField:  row.ID,
		milvusVectorField:   row.Vector,
		milvusTextField:     row.Text,
		milvusMetadataField: metadata,
		milvusCorpusField:   asString(metadata["corpus"]),
		milvusDocumentField: asString(metadata["document_id"]),
		milvusUpdatedAtField: func() int64 {
			if row.UpdatedAt.IsZero() {
				return time.Now().UnixMilli()
			}
			return row.UpdatedAt.UnixMilli()
		}(),
	}
	assignMilvusScalar(entity, milvusUserIDField, metadata["user_id"])
	assignMilvusScalar(entity, milvusAssistantField, metadata["assistant_id"])
	assignMilvusScalar(entity, milvusConvField, metadata["conversation_id"])
	assignMilvusScalar(entity, milvusRunField, metadata["run_id"])
	assignMilvusScalar(entity, milvusMemoryType, metadata["memory_type"])
	assignMilvusScalar(entity, milvusQueryIDField, metadata["query_id"])
	assignMilvusScalar(entity, milvusSessionField, metadata["session_id"])
	assignMilvusScalar(entity, milvusDomainField, metadata["domain"])
	assignMilvusScalar(entity, milvusChunkOrder, metadata["chunk_order"])
	return entity
}

func assignMilvusScalar(target map[string]any, field string, value any) {
	if value == nil {
		return
	}
	switch field {
	case milvusUserIDField, milvusChunkOrder:
		if parsed, ok := toInt64(value); ok {
			target[field] = parsed
		}
	default:
		if text := asString(value); text != "" {
			target[field] = text
		}
	}
}

func buildMilvusFilter(filter map[string]any) (string, error) {
	if len(filter) == 0 {
		return "", nil
	}

	parts := make([]string, 0, len(filter))
	for key, value := range filter {
		field, ok := milvusFilterFieldMap[key]
		if !ok {
			return "", fmt.Errorf("unsupported milvus filter key: %s", key)
		}
		switch field {
		case milvusUserIDField, milvusChunkOrder:
			parsed, parseOK := toInt64(value)
			if !parseOK {
				return "", fmt.Errorf("milvus filter key=%s expects integer", key)
			}
			parts = append(parts, fmt.Sprintf("%s == %d", field, parsed))
		default:
			text := escapeMilvusString(asString(value))
			parts = append(parts, fmt.Sprintf(`%s == "%s"`, field, text))
		}
	}
	return strings.Join(parts, " and "), nil
}

func buildVarcharField(name string, isPrimary bool, maxLength int) map[string]any {
	field := map[string]any{
		"fieldName":         name,
		"dataType":          "VarChar",
		"elementTypeParams": map[string]any{"max_length": maxLength},
	}
	if isPrimary {
		field["isPrimary"] = true
	}
	return field
}

func buildVectorField(name string, dimension int) map[string]any {
	return map[string]any{
		"fieldName":         name,
		"dataType":          "FloatVector",
		"elementTypeParams": map[string]any{"dim": dimension},
	}
}

func milvusOutputFields(includeVector bool) []string {
	fields := []string{
		milvusTextField,
		milvusMetadataField,
		milvusCorpusField,
		milvusDocumentField,
		milvusUserIDField,
		milvusAssistantField,
		milvusConvField,
		milvusRunField,
		milvusMemoryType,
		milvusQueryIDField,
		milvusSessionField,
		milvusDomainField,
		milvusChunkOrder,
		milvusUpdatedAtField,
	}
	if includeVector {
		fields = append(fields, milvusVectorField)
	}
	return fields
}

func normalizeMilvusTopK(topK int) int {
	if topK <= 0 {
		return 8
	}
	return topK
}

func blankToNil(v string) any {
	v = strings.TrimSpace(v)
	if v == "" {
		return nil
	}
	return v
}

func escapeMilvusString(v string) string {
	v = strings.ReplaceAll(v, `\`, `\\`)
	return strings.ReplaceAll(v, `"`, `\"`)
}

func joinQuotedStrings(values []string) string {
	parts := make([]string, 0, len(values))
	for _, value := range values {
		parts = append(parts, fmt.Sprintf(`"%s"`, escapeMilvusString(value)))
	}
	return strings.Join(parts, ",")
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

func toInt64(v any) (int64, bool) {
	switch value := v.(type) {
	case int:
		return int64(value), true
	case int32:
		return int64(value), true
	case int64:
		return value, true
	case float64:
		return int64(value), true
	case json.Number:
		parsed, err := value.Int64()
		return parsed, err == nil
	case string:
		parsed, err := strconv.ParseInt(strings.TrimSpace(value), 10, 64)
		return parsed, err == nil
	default:
		return 0, false
	}
}

func isMilvusAlreadyExists(err error) bool {
	if err == nil {
		return false
	}
	text := strings.ToLower(err.Error())
	return strings.Contains(text, "already exist") ||
		strings.Contains(text, "already exists") ||
		strings.Contains(text, "duplicate collection")
}

func isMilvusCollectionMissing(err error) bool {
	if err == nil {
		return false
	}
	text := strings.ToLower(err.Error())
	return strings.Contains(text, "can't find collection") || strings.Contains(text, "collection not found")
}

type milvusBasicResponse struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type milvusSearchResponse struct {
	Code    int                `json:"code"`
	Message string             `json:"message"`
	Data    []milvusSearchItem `json:"data"`
}

type milvusSearchItem map[string]any

func (m milvusSearchItem) toVectorRow() (core.VectorRow, float64) {
	row := mapMilvusRow(map[string]any(m), false)
	score := 0.0
	if value, ok := m["distance"].(float64); ok {
		score = value
	}
	return row, score
}

type milvusGetResponse struct {
	Code    int              `json:"code"`
	Message string           `json:"message"`
	Data    []map[string]any `json:"data"`
}

func mapMilvusRow(raw map[string]any, includeVector bool) core.VectorRow {
	metadata := cloneMap(readMetadataMap(raw[milvusMetadataField]))
	assignMetadataIfPresent(metadata, "corpus", raw[milvusCorpusField])
	assignMetadataIfPresent(metadata, "document_id", raw[milvusDocumentField])
	assignMetadataIfPresent(metadata, "user_id", raw[milvusUserIDField])
	assignMetadataIfPresent(metadata, "assistant_id", raw[milvusAssistantField])
	assignMetadataIfPresent(metadata, "conversation_id", raw[milvusConvField])
	assignMetadataIfPresent(metadata, "run_id", raw[milvusRunField])
	assignMetadataIfPresent(metadata, "memory_type", raw[milvusMemoryType])
	assignMetadataIfPresent(metadata, "query_id", raw[milvusQueryIDField])
	assignMetadataIfPresent(metadata, "session_id", raw[milvusSessionField])
	assignMetadataIfPresent(metadata, "domain", raw[milvusDomainField])
	assignMetadataIfPresent(metadata, "chunk_order", raw[milvusChunkOrder])

	row := core.VectorRow{
		ID:       asString(raw[milvusPrimaryField]),
		Text:     asString(raw[milvusTextField]),
		Metadata: metadata,
	}
	if row.ID == "" {
		row.ID = asString(raw["id"])
	}
	if includeVector {
		row.Vector = readFloat32Vector(raw[milvusVectorField])
	}
	return row
}

func readMetadataMap(value any) map[string]any {
	switch data := value.(type) {
	case map[string]any:
		return data
	default:
		return map[string]any{}
	}
}

func readFloat32Vector(value any) []float32 {
	switch vector := value.(type) {
	case []float32:
		return vector
	case []any:
		result := make([]float32, 0, len(vector))
		for _, item := range vector {
			switch number := item.(type) {
			case float64:
				result = append(result, float32(number))
			case float32:
				result = append(result, number)
			}
		}
		return result
	default:
		return nil
	}
}

func assignMetadataIfPresent(target map[string]any, key string, value any) {
	if value == nil {
		return
	}
	switch typed := value.(type) {
	case string:
		if strings.TrimSpace(typed) == "" {
			return
		}
		target[key] = strings.TrimSpace(typed)
	default:
		target[key] = typed
	}
}
