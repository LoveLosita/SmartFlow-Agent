package corpus

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/LoveLosita/smartflow/backend/infra/rag/core"
)

const memoryCorpusName = "memory"

// MemoryIngestItem 是记忆语料入库项。
type MemoryIngestItem struct {
	MemoryID         int64
	UserID           int
	ConversationID   string
	AssistantID      string
	RunID            string
	MemoryType       string
	Title            string
	Content          string
	Confidence       float64
	Importance       float64
	SensitivityLevel int
	IsExplicit       bool
	Status           string
	TTLAt            *time.Time
	CreatedAt        *time.Time
}

// MemoryRetrieveInput 是记忆检索过滤输入。
type MemoryRetrieveInput struct {
	UserID         int
	ConversationID string
	AssistantID    string
	RunID          string
	MemoryType     string
}

// MemoryCorpus 是记忆语料适配器。
type MemoryCorpus struct{}

func NewMemoryCorpus() *MemoryCorpus {
	return &MemoryCorpus{}
}

func (c *MemoryCorpus) Name() string {
	return memoryCorpusName
}

func (c *MemoryCorpus) BuildIngestDocuments(_ context.Context, input any) ([]core.SourceDocument, error) {
	items, err := toMemoryItems(input)
	if err != nil {
		return nil, err
	}
	result := make([]core.SourceDocument, 0, len(items))
	for _, item := range items {
		if item.UserID <= 0 {
			return nil, errors.New("memory ingest item user_id is invalid")
		}
		text := strings.TrimSpace(item.Content)
		if text == "" {
			continue
		}
		docID := fmt.Sprintf("memory:%d", item.MemoryID)
		if item.MemoryID <= 0 {
			docID = fmt.Sprintf("memory:uid:%d:%s", item.UserID, hashLikeText(text))
		}
		metadata := map[string]any{
			"user_id":           item.UserID,
			"conversation_id":   strings.TrimSpace(item.ConversationID),
			"assistant_id":      strings.TrimSpace(item.AssistantID),
			"run_id":            strings.TrimSpace(item.RunID),
			"memory_type":       strings.TrimSpace(strings.ToLower(item.MemoryType)),
			"title":             strings.TrimSpace(item.Title),
			"confidence":        item.Confidence,
			"importance":        item.Importance,
			"sensitivity_level": item.SensitivityLevel,
			"is_explicit":       item.IsExplicit,
			"status":            strings.TrimSpace(item.Status),
		}
		if item.TTLAt != nil {
			metadata["ttl_at"] = item.TTLAt.Format(time.RFC3339)
		}
		createdAt := time.Now()
		if item.CreatedAt != nil {
			createdAt = *item.CreatedAt
		}
		result = append(result, core.SourceDocument{
			ID:        docID,
			Text:      text,
			Title:     strings.TrimSpace(item.Title),
			Metadata:  metadata,
			CreatedAt: createdAt,
		})
	}
	return result, nil
}

func (c *MemoryCorpus) BuildRetrieveFilter(_ context.Context, req any) (map[string]any, error) {
	input, ok := req.(MemoryRetrieveInput)
	if !ok {
		if ptr, isPtr := req.(*MemoryRetrieveInput); isPtr && ptr != nil {
			input = *ptr
		} else if req == nil {
			return nil, errors.New("memory retrieve input is nil")
		} else {
			return nil, errors.New("invalid memory retrieve input")
		}
	}
	if input.UserID <= 0 {
		return nil, errors.New("memory retrieve user_id is invalid")
	}
	filter := map[string]any{
		"user_id": input.UserID,
	}
	if v := strings.TrimSpace(input.ConversationID); v != "" {
		filter["conversation_id"] = v
	}
	if v := strings.TrimSpace(input.AssistantID); v != "" {
		filter["assistant_id"] = v
	}
	if v := strings.TrimSpace(input.RunID); v != "" {
		filter["run_id"] = v
	}
	if v := strings.TrimSpace(strings.ToLower(input.MemoryType)); v != "" {
		filter["memory_type"] = v
	}
	return filter, nil
}

func toMemoryItems(input any) ([]MemoryIngestItem, error) {
	switch value := input.(type) {
	case MemoryIngestItem:
		return []MemoryIngestItem{value}, nil
	case *MemoryIngestItem:
		if value == nil {
			return nil, errors.New("memory ingest item is nil")
		}
		return []MemoryIngestItem{*value}, nil
	case []MemoryIngestItem:
		return value, nil
	case []*MemoryIngestItem:
		items := make([]MemoryIngestItem, 0, len(value))
		for _, ptr := range value {
			if ptr == nil {
				continue
			}
			items = append(items, *ptr)
		}
		return items, nil
	default:
		return nil, errors.New("invalid memory ingest input")
	}
}
