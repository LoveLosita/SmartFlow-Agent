package corpus

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/LoveLosita/smartflow/backend/infra/rag/core"
)

const webCorpusName = "web"

// WebIngestItem 是网页语料入库项。
type WebIngestItem struct {
	URL         string
	Title       string
	Content     string
	Snippet     string
	Domain      string
	QueryID     string
	SessionID   string
	PublishedAt *time.Time
	FetchedAt   *time.Time
	SourceRank  int
}

// WebRetrieveInput 是网页检索过滤输入。
type WebRetrieveInput struct {
	QueryID   string
	SessionID string
	Domain    string
}

// WebCorpus 是网页语料适配器。
type WebCorpus struct{}

func NewWebCorpus() *WebCorpus {
	return &WebCorpus{}
}

func (c *WebCorpus) Name() string {
	return webCorpusName
}

func (c *WebCorpus) BuildIngestDocuments(_ context.Context, input any) ([]core.SourceDocument, error) {
	items, err := toWebItems(input)
	if err != nil {
		return nil, err
	}
	result := make([]core.SourceDocument, 0, len(items))
	for _, item := range items {
		url := strings.TrimSpace(item.URL)
		if url == "" {
			return nil, errors.New("web ingest item url is empty")
		}

		mainText := buildWebText(item)
		if strings.TrimSpace(mainText) == "" {
			continue
		}

		docID := fmt.Sprintf("web:%s", hashLikeText(url+"|"+mainText))
		metadata := map[string]any{
			"url":         url,
			"domain":      strings.TrimSpace(item.Domain),
			"query_id":    strings.TrimSpace(item.QueryID),
			"session_id":  strings.TrimSpace(item.SessionID),
			"source_rank": item.SourceRank,
		}
		if item.PublishedAt != nil {
			metadata["published_at"] = item.PublishedAt.Format(time.RFC3339)
		}
		if item.FetchedAt != nil {
			metadata["fetched_at"] = item.FetchedAt.Format(time.RFC3339)
		}

		createdAt := time.Now()
		if item.FetchedAt != nil {
			createdAt = *item.FetchedAt
		}
		result = append(result, core.SourceDocument{
			ID:        docID,
			Text:      mainText,
			Title:     strings.TrimSpace(item.Title),
			Metadata:  metadata,
			CreatedAt: createdAt,
		})
	}
	return result, nil
}

func (c *WebCorpus) BuildRetrieveFilter(_ context.Context, req any) (map[string]any, error) {
	input, ok := req.(WebRetrieveInput)
	if !ok {
		if ptr, isPtr := req.(*WebRetrieveInput); isPtr && ptr != nil {
			input = *ptr
		} else if req == nil {
			return nil, errors.New("web retrieve input is nil")
		} else {
			return nil, errors.New("invalid web retrieve input")
		}
	}

	// 1. query_id/session_id 至少要有一个，避免跨问题串数据。
	queryID := strings.TrimSpace(input.QueryID)
	sessionID := strings.TrimSpace(input.SessionID)
	if queryID == "" && sessionID == "" {
		return nil, errors.New("web retrieve filter requires query_id or session_id")
	}

	filter := map[string]any{}
	if queryID != "" {
		filter["query_id"] = queryID
	}
	if sessionID != "" {
		filter["session_id"] = sessionID
	}
	if domain := strings.TrimSpace(input.Domain); domain != "" {
		filter["domain"] = domain
	}
	return filter, nil
}

func toWebItems(input any) ([]WebIngestItem, error) {
	switch value := input.(type) {
	case WebIngestItem:
		return []WebIngestItem{value}, nil
	case *WebIngestItem:
		if value == nil {
			return nil, errors.New("web ingest item is nil")
		}
		return []WebIngestItem{*value}, nil
	case []WebIngestItem:
		return value, nil
	case []*WebIngestItem:
		items := make([]WebIngestItem, 0, len(value))
		for _, ptr := range value {
			if ptr == nil {
				continue
			}
			items = append(items, *ptr)
		}
		return items, nil
	default:
		return nil, errors.New("invalid web ingest input")
	}
}

func buildWebText(item WebIngestItem) string {
	parts := make([]string, 0, 3)
	if title := strings.TrimSpace(item.Title); title != "" {
		parts = append(parts, title)
	}
	if snippet := strings.TrimSpace(item.Snippet); snippet != "" {
		parts = append(parts, snippet)
	}
	if content := strings.TrimSpace(item.Content); content != "" {
		parts = append(parts, content)
	}
	return strings.Join(parts, "\n\n")
}
