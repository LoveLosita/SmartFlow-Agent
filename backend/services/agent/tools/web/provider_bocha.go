package web

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// BochaProvider 博查（Bocha）搜索供应商实现。
//
// 职责：
// 1. 将 SearchOptions 映射为博查 API 请求参数；
// 2. 发起 HTTP POST 调用博查 web-search 端点；
// 3. 将博查响应转换为统一的 SearchResponse 结构。
//
// 不负责：
// 1. 不负责 API Key 管理（由调用方注入）；
// 2. 不负责重试（单次调用失败直接返回 error）；
// 3. 不负责 URL 正文抓取（由 Fetcher 承担）。
//
// 博查 API 文档：https://open.bochaai.com/
type BochaProvider struct {
	// apiKey 博查 API Key，从配置注入。
	apiKey string

	// httpClient 带超时的 HTTP 客户端。
	httpClient *http.Client

	// baseURL 博查 API 基础地址，默认 https://api.bochaai.com/v1。
	baseURL string
}

// NewBochaProvider 创建博查搜索供应商。
//
// 1. apiKey 必填，为空时 Search 会返回明确错误；
// 2. 超时默认 10 秒，与工具层 ctx 超时对齐；
// 3. baseURL 留空则使用默认地址。
func NewBochaProvider(apiKey, baseURL string) *BochaProvider {
	if baseURL == "" {
		baseURL = "https://api.bochaai.com/v1"
	}
	return &BochaProvider{
		apiKey: apiKey,
		httpClient: &http.Client{
			Timeout: 10 * time.Second,
		},
		baseURL: baseURL,
	}
}

// Name 返回供应商标识。
func (b *BochaProvider) Name() string { return "bocha" }

// Search 调用博查 web-search API 执行检索。
//
// 流程：
// 1. 参数校验（apiKey 非空、query 非空）；
// 2. 将 SearchOptions 映射为博查请求体（count / freshness / summary）；
// 3. 发起 HTTP POST，读取响应；
// 4. 解析博查 JSON 响应，提取 webPages.value 数组；
// 5. 转换为统一 SearchItem 结构返回。
//
// 错误处理：
// - apiKey 为空 → 返回明确错误；
// - HTTP 非 2xx → 返回带状态码的错误；
// - 响应解析失败 → 返回原始响应片段供排查。
func (b *BochaProvider) Search(ctx context.Context, query string, opts SearchOptions) (*SearchResponse, error) {
	// 1. 参数校验。
	if b.apiKey == "" {
		return nil, fmt.Errorf("博查 API Key 未配置")
	}
	query = strings.TrimSpace(query)
	if query == "" {
		return nil, fmt.Errorf("查询关键词为空")
	}

	// 2. 组装请求体。
	// 2.1 count：博查支持 1~50，默认 10。
	count := normalizeTopK(opts.TopK, 10, 50)

	// 2.2 freshness：将 RecencyDays 映射为博查的时间过滤枚举。
	freshness := mapRecencyDaysToFreshness(opts.RecencyDays)

	reqBody := bochaSearchRequest{
		Query:     query,
		Count:     count,
		Freshness: freshness,
		Summary:   true, // 开启 AI 摘要，提升结果信息密度
	}

	bodyBytes, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("序列化请求体失败：%w", err)
	}

	// 3. 发起 HTTP POST。
	url := b.baseURL + "/web-search"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, strings.NewReader(string(bodyBytes)))
	if err != nil {
		return nil, fmt.Errorf("构建请求失败：%w", err)
	}
	req.Header.Set("Authorization", "Bearer "+b.apiKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := b.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("请求博查 API 失败：%w", err)
	}
	defer resp.Body.Close()

	// 4. 读取响应体（限制 2MB，防止异常响应撑爆内存）。
	respBody, err := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
	if err != nil {
		return nil, fmt.Errorf("读取博查响应失败：%w", err)
	}

	// 5. 检查 HTTP 状态码。
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		// 截取前 200 字符作为错误上下文，避免日志过长。
		snippet := string(respBody)
		if len(snippet) > 200 {
			snippet = snippet[:200]
		}
		return nil, fmt.Errorf("博查 API 返回 HTTP %d：%s", resp.StatusCode, snippet)
	}

	// 6. 解析响应 JSON。
	var bochaResp bochaSearchAPIResponse
	if err := json.Unmarshal(respBody, &bochaResp); err != nil {
		return nil, fmt.Errorf("解析博查响应失败：%w", err)
	}

	// 7. 提取搜索结果。
	items := make([]SearchItem, 0, len(bochaResp.Data.WebPages.Value))
	for _, v := range bochaResp.Data.WebPages.Value {
		item := SearchItem{
			Title:   v.Name,
			URL:     v.URL,
			Snippet: v.Summary, // 优先使用 AI 摘要；若为空则回退到 snippet
			Domain:  v.SiteName,
		}
		if item.Snippet == "" {
			item.Snippet = v.Snippet
		}
		// 解析发布时间（博查格式：2024-07-22T00:00:00+08:00）。
		if v.DatePublished != "" {
			if t, err := time.Parse(time.RFC3339, v.DatePublished); err == nil {
				item.PublishedAt = t
			}
		}
		items = append(items, item)
	}

	return &SearchResponse{
		Query: query,
		Items: items,
	}, nil
}

// mapRecencyDaysToFreshness 将 RecencyDays 映射为博查 freshness 枚举值。
//
// 映射规则：
// - 0 → noLimit（不限时间）
// - 1 → oneDay
// - 2~7 → oneWeek
// - 8~30 → oneMonth
// - 31~365 → oneYear
// - >365 → noLimit
func mapRecencyDaysToFreshness(days int) string {
	switch {
	case days <= 0:
		return "noLimit"
	case days <= 1:
		return "oneDay"
	case days <= 7:
		return "oneWeek"
	case days <= 30:
		return "oneMonth"
	case days <= 365:
		return "oneYear"
	default:
		return "noLimit"
	}
}

// ==================== 博查 API 请求/响应结构体 ====================

// bochaSearchRequest 博查 web-search 请求体。
type bochaSearchRequest struct {
	Query     string `json:"query"`
	Count     int    `json:"count"`
	Freshness string `json:"freshness"`
	Summary   bool   `json:"summary"`
}

// bochaSearchAPIResponse 博查 web-search 响应体（只提取需要的字段）。
type bochaSearchAPIResponse struct {
	Data struct {
		WebPages struct {
			Value []bochaWebPageItem `json:"value"`
		} `json:"webPages"`
	} `json:"data"`
}

// bochaWebPageItem 博查单条搜索结果。
type bochaWebPageItem struct {
	Name          string `json:"name"`
	URL           string `json:"url"`
	Snippet       string `json:"snippet"`
	Summary       string `json:"summary"`
	SiteName      string `json:"siteName"`
	DatePublished string `json:"datePublished"`
}
