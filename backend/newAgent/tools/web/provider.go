package web

import (
	"context"
	"time"
)

// SearchProvider 搜索供应商抽象接口。
//
// 职责：
// 1. 接收检索查询与选项，返回结构化搜索结果；
// 2. 实现方负责 HTTP 调用、错误重试、限流兜底；
// 3. 调用方不感知底层是 Bocha / Mock 还是其他供应商。
//
// 不负责：
// 1. 不负责 URL 正文抓取（由 Fetcher 承担）；
// 2. 不负责结果缓存（由上层工具决定）。
type SearchProvider interface {
	// Name 返回供应商名称（如 "mock"、"bocha"），用于日志与降级标识。
	Name() string

	// Search 执行一次检索。
	//
	// 1. ctx 用于超时控制与取消；
	// 2. opts.TopK 默认 5，上限 20，超出自动截断；
	// 3. 失败时返回 error，调用方负责兜底 observation 组装。
	Search(ctx context.Context, query string, opts SearchOptions) (*SearchResponse, error)
}

// SearchOptions 搜索可选参数。
type SearchOptions struct {
	// TopK 返回结果数上限。0 表示使用供应商默认值（通常为 5）。
	TopK int

	// DomainAllow 仅返回指定域名下的结果。空表示不限。
	DomainAllow []string

	// RecencyDays 仅返回最近 N 天内的结果。0 表示不限时间。
	RecencyDays int
}

// SearchResponse 搜索结果集合。
type SearchResponse struct {
	// Query 原始查询文本，用于日志追踪。
	Query string

	// Items 搜索结果条目，按相关性降序排列。
	Items []SearchItem
}

// SearchItem 单条搜索结果。
type SearchItem struct {
	// Title 页面标题。
	Title string

	// URL 页面链接。
	URL string

	// Snippet 搜索引擎返回的摘要片段。
	Snippet string

	// Domain 来源域名（如 "example.com"），由实现方从 URL 提取。
	Domain string

	// PublishedAt 页面发布时间（若供应商可提供）。零值表示未知。
	PublishedAt time.Time

	// Raw 供应商原始响应字段，供调试用，不传给模型。
	Raw map[string]any
}

// normalizeTopK 将用户传入的 topK 归一化到 [1, max] 区间。
// 默认值 5，上限 20，防止模型传入异常值导致 API 爆炸。
func normalizeTopK(topK, defaultVal, maxVal int) int {
	if topK <= 0 {
		return defaultVal
	}
	if topK > maxVal {
		return maxVal
	}
	return topK
}
