package web

import (
	"context"
	"fmt"
	"time"
)

// MockProvider 空实现搜索供应商，返回硬编码结果。
//
// 用途：
// 1. 在真实 API Key 到手前，先跑通工具注册→调用→observation 写回的完整链路；
// 2. 后续替换为 Tavily/Brave 实现后，Mock 保留用于单元测试。
//
// 不负责：
// 1. 不负责真实 HTTP 调用；
// 2. 不负责网络错误模拟（如需测试超时可另行实现 TimeoutMockProvider）。
type MockProvider struct{}

// Name 返回供应商标识。
func (m *MockProvider) Name() string { return "mock" }

// Search 返回 2 条硬编码搜索结果，模拟正常检索链路。
//
// 1. 无论 query 内容如何，始终返回相同结果；
// 2. ctx 仅做形式兼容，不检查超时；
// 3. 永远不返回 error（Mock 不模拟失败场景）。
func (m *MockProvider) Search(_ context.Context, query string, opts SearchOptions) (*SearchResponse, error) {
	topK := normalizeTopK(opts.TopK, 5, 20)

	// 1. 准备 2 条模拟数据，覆盖核心字段（title/url/snippet/domain/published_at）；
	// 2. 若调用方 topK=1 则只返回第一条。
	mockItems := []SearchItem{
		{
			Title:       fmt.Sprintf("搜索结果示例 - %s", query),
			URL:         "https://example.com/search-result-1",
			Snippet:     "这是 MockProvider 返回的模拟搜索摘要，用于验证工具链路是否通畅。",
			Domain:      "example.com",
			PublishedAt: time.Now().Add(-24 * time.Hour),
		},
		{
			Title:       fmt.Sprintf("相关资料 - %s", query),
			URL:         "https://example.com/related-resource-2",
			Snippet:     "这是第二条 Mock 结果，模拟同主题下的补充信息来源。",
			Domain:      "example.com",
			PublishedAt: time.Now().Add(-48 * time.Hour),
		},
	}

	if topK < len(mockItems) {
		mockItems = mockItems[:topK]
	}

	return &SearchResponse{
		Query: query,
		Items: mockItems,
	}, nil
}
