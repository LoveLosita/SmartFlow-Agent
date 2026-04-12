package web

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"time"
)

// Fetcher 抓取指定 URL 正文并做最小 HTML 清洗。
//
// 职责：
// 1. 发起 HTTP GET 请求并读取响应体；
// 2. 剥离 HTML 标签，保留纯文本内容；
// 3. 按 MaxChars 截断，避免超长正文占用模型上下文。
//
// 不负责：
// 1. 不负责 JS 渲染（无法处理 SPA 页面）；
// 2. 不负责反爬绕过（遇到 403 直接返回错误）；
// 3. 不负责正文提取算法优化（仅做粗粒度标签剥离）。
type Fetcher struct {
	// Client 带超时的 HTTP 客户端，由调用方注入。
	Client *http.Client

	// MaxChars 正文最大字符数。超出时截断并标记 truncated=true。0 使用默认值 4000。
	MaxChars int
}

// NewFetcher 创建默认 Fetcher。
//
// 1. 超时默认 10 秒，足够覆盖大多数静态页面；
// 2. MaxChars 默认 4000 字符，约占 1000~2000 token，不会挤占过多上下文。
func NewFetcher() *Fetcher {
	return &Fetcher{
		Client: &http.Client{
			Timeout: 10 * time.Second,
		},
		MaxChars: 4000,
	}
}

// FetchResult 抓取结果。
type FetchResult struct {
	// Title 页面标题（从 <title> 标签提取）。
	Title string

	// Content 清洗后的纯文本正文。
	Content string

	// Truncated 正文是否被截断。
	Truncated bool
}

// Fetch 抓取指定 URL 并返回清洗后的正文。
//
// 流程：
// 1. 构建带超时的 HTTP GET 请求；
// 2. 检查状态码，非 2xx 直接返回可读错误；
// 3. 读取响应体，提取 <title>；
// 4. 剥离 HTML 标签，按 MaxChars 截断；
// 5. 所有失败场景返回 error，由工具层兜底组装 observation。
func (f *Fetcher) Fetch(ctx context.Context, url string) (*FetchResult, error) {
	// 1. 构建请求，注入 ctx 用于超时与取消。
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("构建请求失败：%w", err)
	}

	// 2. 模拟浏览器 User-Agent，避免部分站点直接拒绝。
	req.Header.Set("User-Agent", "SmartFlow-Agent/1.0 (compatible; web_fetch)")

	resp, err := f.Client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("请求失败：%w", err)
	}
	defer resp.Body.Close()

	// 3. 非 2xx 返回明确状态码，方便工具层区分 4xx/5xx。
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("HTTP %d：%s", resp.StatusCode, resp.Status)
	}

	// 4. 限制读取量（最多 1MB），防止恶意超长响应撑爆内存。
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, fmt.Errorf("读取响应体失败：%w", err)
	}

	htmlStr := string(body)

	// 5. 提取 <title> 内容。
	title := extractHTMLTitle(htmlStr)

	// 6. 剥离 HTML 标签，得到纯文本。
	text := stripHTMLTags(htmlStr)

	// 7. 清理多余空白（连续换行、行首行尾空格）。
	text = cleanWhitespace(text)

	// 8. 按 MaxChars 截断。
	maxChars := f.MaxChars
	if maxChars <= 0 {
		maxChars = 4000
	}
	truncated := false
	runes := []rune(text)
	if len(runes) > maxChars {
		truncated = true
		runes = runes[:maxChars]
	}

	return &FetchResult{
		Title:     title,
		Content:   string(runes),
		Truncated: truncated,
	}, nil
}

// extractHTMLTitle 从 HTML 中提取 <title> 标签内容。
//
// 1. 使用正则匹配，不做 DOM 解析（兼顾性能与简单性）；
// 2. 找不到时返回空字符串，不报错。
func extractHTMLTitle(htmlStr string) string {
	re := regexp.MustCompile("(?i)<title[^>]*>(.*?)</title>")
	matches := re.FindStringSubmatch(htmlStr)
	if len(matches) >= 2 {
		return strings.TrimSpace(matches[1])
	}
	return ""
}

// stripHTMLTags 剥离所有 HTML 标签，保留纯文本。
//
// 1. 先移除 <script> / <style> 块（避免 JS/CSS 内容污染正文）；
// 2. 再移除所有 HTML 标签；
// 3. 解码常见 HTML 实体（&amp; &lt; &gt; &quot;）。
func stripHTMLTags(htmlStr string) string {
	// 1. 移除 script/style 块
	re := regexp.MustCompile("(?is)<(script|style)[^>]*>.*?</\\1>")
	text := re.ReplaceAllString(htmlStr, " ")

	// 2. 移除所有 HTML 标签
	reTag := regexp.MustCompile("<[^>]+>")
	text = reTag.ReplaceAllString(text, " ")

	// 3. 解码常见 HTML 实体
	text = strings.ReplaceAll(text, "&amp;", "&")
	text = strings.ReplaceAll(text, "&lt;", "<")
	text = strings.ReplaceAll(text, "&gt;", ">")
	text = strings.ReplaceAll(text, "&quot;", "\"")
	text = strings.ReplaceAll(text, "&#39;", "'")
	text = strings.ReplaceAll(text, "&nbsp;", " ")

	return text
}

// cleanWhitespace 清理多余空白：连续空行合并为单个换行，去除行首行尾空格。
func cleanWhitespace(text string) string {
	// 1. 连续换行压缩为最多两个换行（保留段落分隔感）。
	re := regexp.MustCompile("\\n{3,}")
	text = re.ReplaceAllString(text, "\n\n")

	// 2. 按行去除首尾空白后重新拼装。
	lines := strings.Split(text, "\n")
	cleaned := make([]string, 0, len(lines))
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		cleaned = append(cleaned, trimmed)
	}

	return strings.Join(cleaned, "\n")
}
