package web

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"time"
)

// SearchToolHandler web_search 工具 handler。
//
// 职责：
// 1. 解析 args（query / top_k / domain_allow / recency_days）；
// 2. 调用 SearchProvider 执行检索；
// 3. 组装结构化 JSON observation 返回给模型。
//
// 不负责：
// 1. 不负责 provider 生命周期管理（由注册层注入）；
// 2. 不负责重试（provider 内部处理）。
type SearchToolHandler struct {
	provider SearchProvider
}

// NewSearchToolHandler 创建 web_search 工具 handler。
//
// 1. provider 为 nil 时，Handle 返回"搜索暂未启用"的 observation；
// 2. 这样做的好处是：即使未配置 provider，也不会阻断主流程。
func NewSearchToolHandler(provider SearchProvider) *SearchToolHandler {
	return &SearchToolHandler{provider: provider}
}

// searchToolArgs web_search 工具的参数定义。
type searchToolArgs struct {
	Query       string   `json:"query"`
	TopK        int      `json:"top_k"`
	DomainAllow []string `json:"domain_allow"`
	RecencyDays int      `json:"recency_days"`
}

// searchToolResult web_search 工具的输出结构。
type searchToolResult struct {
	Tool  string       `json:"tool"`
	Query string       `json:"query"`
	Count int          `json:"count"`
	Items []searchItem `json:"items"`
}

// searchItem 输出给模型的单条搜索结果。
type searchItem struct {
	Title       string `json:"title"`
	URL         string `json:"url"`
	Snippet     string `json:"snippet"`
	Domain      string `json:"domain"`
	PublishedAt string `json:"published_at,omitempty"`
}

// Handle 执行 web_search 工具。
//
// 1. 解析参数，query 为必填，缺失时直接返回错误 observation；
// 2. 调用 provider.Search，超时上限 10 秒；
// 3. 失败时返回可恢复 observation（包含错误原因），不 panic、不阻断主流程。
func (h *SearchToolHandler) Handle(args map[string]any) string {
	// 1. provider 为 nil 说明未启用，直接返回提示。
	if h.provider == nil {
		return `{"tool":"web_search","error":"搜索暂未启用，请跳过 web_search 继续执行其他操作。"}`
	}

	// 2. 提取必填参数 query。
	query, _ := args["query"].(string)
	query = strings.TrimSpace(query)
	if query == "" {
		return `{"tool":"web_search","error":"参数错误：缺少必填参数 query。"}`
	}

	// 3. 提取可选参数。
	topK, _ := args["top_k"].(float64)
	var domainAllow []string
	if raw, ok := args["domain_allow"].([]any); ok {
		for _, v := range raw {
			if s, ok := v.(string); ok {
				domainAllow = append(domainAllow, s)
			}
		}
	}
	recencyDays, _ := args["recency_days"].(float64)

	// 4. 构建带超时的 context，防止搜索请求卡死。
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// 5. 调用 provider。
	start := time.Now()
	resp, err := h.provider.Search(ctx, query, SearchOptions{
		TopK:        int(topK),
		DomainAllow: domainAllow,
		RecencyDays: int(recencyDays),
	})
	elapsed := time.Since(start)

	// 6. 记录日志，方便排查搜索耗时、结果数、失败原因。
	log.Printf("[web_search] provider=%s query=%q topK=%d elapsed=%s results=%d err=%v",
		h.provider.Name(), query, int(topK), elapsed, len(resp.Items), err)

	if err != nil {
		// 7. 失败时返回可恢复 observation：模型看到后可选择换 query 或跳过。
		return fmt.Sprintf(`{"tool":"web_search","error":"搜索失败：%s","query":%q}`, err.Error(), query)
	}

	// 8. 组装输出 JSON。
	items := make([]searchItem, 0, len(resp.Items))
	for _, item := range resp.Items {
		si := searchItem{
			Title:   item.Title,
			URL:     item.URL,
			Snippet: item.Snippet,
			Domain:  item.Domain,
		}
		if !item.PublishedAt.IsZero() {
			si.PublishedAt = item.PublishedAt.Format("2006-01-02")
		}
		items = append(items, si)
	}

	result := searchToolResult{
		Tool:  "web_search",
		Query: query,
		Count: len(items),
		Items: items,
	}

	out, err := json.Marshal(result)
	if err != nil {
		return fmt.Sprintf(`{"tool":"web_search","error":"序列化结果失败：%s"}`, err.Error())
	}
	return string(out)
}

// FetchToolHandler web_fetch 工具 handler。
//
// 职责：
// 1. 解析 args（url / max_chars）；
// 2. 调用 Fetcher 抓取并清洗正文；
// 3. 组装 JSON observation 返回给模型。
type FetchToolHandler struct {
	fetcher *Fetcher
}

// NewFetchToolHandler 创建 web_fetch 工具 handler。
func NewFetchToolHandler(fetcher *Fetcher) *FetchToolHandler {
	return &FetchToolHandler{fetcher: fetcher}
}

// fetchToolResult web_fetch 工具的输出结构。
type fetchToolResult struct {
	Tool      string `json:"tool"`
	URL       string `json:"url"`
	Title     string `json:"title"`
	Content   string `json:"content"`
	Truncated bool   `json:"truncated"`
}

// Handle 执行 web_fetch 工具。
//
// 1. 解析参数，url 为必填；
// 2. max_chars 可选，为 0 时使用 Fetcher 默认值（4000）；
// 3. 所有失败场景返回结构化错误 observation，不 panic。
func (h *FetchToolHandler) Handle(args map[string]any) string {
	// 1. fetcher 为 nil 说明未初始化。
	if h.fetcher == nil {
		return `{"tool":"web_fetch","error":"抓取服务暂未初始化，请跳过 web_fetch 继续执行。"}`
	}

	// 2. 提取必填参数 url。
	url, _ := args["url"].(string)
	url = strings.TrimSpace(url)
	if url == "" {
		return `{"tool":"web_fetch","error":"参数错误：缺少必填参数 url。"}`
	}

	// 3. 提取可选参数 max_chars，覆盖 Fetcher 默认值。
	maxChars := 0
	if v, ok := args["max_chars"].(float64); ok {
		maxChars = int(v)
	}

	// 4. 若调用方指定 max_chars，临时覆盖 Fetcher 配置。
	savedMaxChars := h.fetcher.MaxChars
	if maxChars > 0 {
		h.fetcher.MaxChars = maxChars
	}
	defer func() {
		h.fetcher.MaxChars = savedMaxChars
	}()

	// 5. 构建带超时的 context。
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	// 6. 调用 Fetcher。
	start := time.Now()
	result, err := h.fetcher.Fetch(ctx, url)
	elapsed := time.Since(start)

	log.Printf("[web_fetch] url=%q elapsed=%s truncated=%v err=%v", url, elapsed, result != nil && result.Truncated, err)

	if err != nil {
		// 7. 失败时返回可恢复 observation。
		return fmt.Sprintf(`{"tool":"web_fetch","error":"抓取失败：%s","url":%q}`, err.Error(), url)
	}

	// 8. 组装输出 JSON。
	out := fetchToolResult{
		Tool:      "web_fetch",
		URL:       url,
		Title:     result.Title,
		Content:   result.Content,
		Truncated: result.Truncated,
	}

	raw, err := json.Marshal(out)
	if err != nil {
		return fmt.Sprintf(`{"tool":"web_fetch","error":"序列化结果失败：%s"}`, err.Error())
	}
	return string(raw)
}
