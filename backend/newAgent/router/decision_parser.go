package newagentrouter

import (
	"fmt"
	"regexp"
	"strings"
)

var (
	// decisionTagRegex 从模型流式输出中提取 <SMARTFLOW_DECISION>...</SMARTFLOW_DECISION> 标签。
	//
	// 格式示例：
	//   <SMARTFLOW_DECISION>{"action":"continue","reason":"...","tool_call":{...}}</SMARTFLOW_DECISION>
	//   用户可见的友好文案在这里流式输出...
	//
	// 使用 (?s) dotall 模式使 . 匹配换行符（JSON 可能包含换行），
	// 非贪婪 (.*?) 避免匹配到多个标签时过度消耗。
	decisionTagRegex = regexp.MustCompile(
		`(?s)<\s*SMARTFLOW_DECISION\s*>(.*?)</\s*SMARTFLOW_DECISION\s*>`)
	// decisionTagHeadRegex 仅用于识别“起始标签是否已经出现”。
	// 目的：避免模型已经输出了 <SMARTFLOW_DECISION 开头但尚未输出闭合标签时，
	// 被长度阈值误判为 fallback（即“假截断”）。
	decisionTagHeadRegex = regexp.MustCompile(`(?i)<\s*SMARTFLOW_DECISION\b`)
)

// StreamDecisionResult 描述解析器的最终输出状态。
type StreamDecisionResult struct {
	// DecisionJSON 是标签内提取的完整 JSON 字符串。
	// 调用方应使用 infrallm.ParseJSONObject[T] 将其解析为具体决策类型。
	DecisionJSON string

	// BeforeText 是 <SMARTFLOW_DECISION> 标签之前的自然语言前言。
	// 仅用于“标签后正文为空”时的兜底展示，不参与 JSON 解析。
	BeforeText string

	// AfterText 是 </SMARTFLOW_DECISION> 标签之后的自然语言正文。
	// 这是主协议约定的用户可见文本来源。
	AfterText string

	// Fallback=true 表示流中未找到决策标签（超过 500 字符阈值），
	// RawBuffer 包含全部累积文本，调用方应走 correction 路径。
	Fallback bool

	// ParseFailed=true 表示找到了标签但内部 JSON 为空或括号计数提取失败，
	// RawBuffer 包含全部累积文本，调用方应走 correction 路径。
	ParseFailed bool

	// RawBuffer 是流式累积的原始文本，用于 correction / 日志。
	RawBuffer string
}

// StreamDecisionParser 从 LLM 流式输出中增量提取 <SMARTFLOW_DECISION> 标签内的 JSON。
//
// 协议约定：模型先输出 <SMARTFLOW_DECISION>{json}</SMARTFLOW_DECISION>，然后输出用户可见正文。
// 调用方在 ready=true 后通过 DecisionJSON() 获取 JSON 字符串并自行解析，
// 同一个 StreamReader 继续读取标签后的正文逐 token 推流。
//
// 职责边界：
//  1. 只负责从流式 chunk 中提取决策标签和 JSON 字符串；
//  2. 不负责 JSON 反序列化（由调用方用 ParseJSONObject 完成）；
//  3. 不负责推送 SSE chunk。
type StreamDecisionParser struct {
	buf           strings.Builder
	decisionFound bool
	decisionJSON  string
	beforeText    string
	afterText     string
	rawBuf        string // 用于 fallback/correction
}

// NewStreamDecisionParser 创建决策标签流式解析器。
func NewStreamDecisionParser() *StreamDecisionParser {
	return &StreamDecisionParser{}
}

// Feed 写入一段 chunk content。
//
// 返回值：
//   - visible：决策标签之后的文本（用户可见内容，可能为空）；
//   - ready：决策是否已提取完毕（成功或 fallback）；
//   - err：非 nil 时表示 fallback 或解析失败。
//
// 调用方应在 ready=true 后：
//  1. 调用 Result() 获取解析结果；
//  2. 若 Fallback/ParseFailed 则走 correction 路径；
//  3. 否则用 DecisionJSON 解析为具体决策类型；
//  4. 继续读取同一个 reader，逐 token 推流 visible 及后续 chunk。
func (p *StreamDecisionParser) Feed(content string) (visible string, ready bool, err error) {
	if p.decisionFound {
		return content, true, nil
	}

	p.buf.WriteString(content)

	text := p.buf.String()
	match := decisionTagRegex.FindStringSubmatchIndex(text)
	if match == nil {
		// 1. 标签尚未完整，检查 fallback 阈值。
		// 2. 仅当“完全没有出现起始标签”时才允许 fallback。
		// 3. 若已经出现起始标签但还没闭合，则继续等待后续 chunk，避免早退。
		if len(text) > 500 {
			if decisionTagHeadRegex.MatchString(text) {
				return "", false, nil
			}
			p.decisionFound = true
			p.rawBuf = text
			return text, true, fmt.Errorf("决策标签解析超时，未找到 SMARTFLOW_DECISION 标签")
		}
		return "", false, nil
	}

	// 提取标签内文本（子组 1）。
	groups := decisionTagRegex.FindStringSubmatch(text)
	if len(groups) < 2 {
		p.decisionFound = true
		p.rawBuf = text
		return "", true, fmt.Errorf("决策标签正则子组不足")
	}

	inner := groups[1]
	jsonStr := extractJSONFromTag(inner)
	if jsonStr == "" {
		p.decisionFound = true
		p.rawBuf = text
		return "", true, fmt.Errorf("决策标签内未找到有效 JSON")
	}

	p.decisionFound = true
	p.decisionJSON = jsonStr
	p.rawBuf = text

	// 1. 同时提取标签前/标签后的自然语言片段。
	// 2. 标签后正文仍然作为主协议 visible 返回，保持现有流式链路不变。
	// 3. 标签前前言只记入 Result，供 execute 在“后文为空”时兜底补发。
	fullMatch := groups[0]
	tagEndIdx := strings.Index(text, fullMatch)
	if tagEndIdx >= 0 {
		beforeTag := strings.TrimSpace(text[:tagEndIdx])
		afterTag := text[tagEndIdx+len(fullMatch):]
		afterTag = strings.TrimPrefix(afterTag, "\r\n")
		afterTag = strings.TrimPrefix(afterTag, "\n")
		p.beforeText = beforeTag
		p.afterText = afterTag
		return afterTag, true, nil
	}

	return "", true, nil
}

// Ready 返回决策是否已提取完毕。
func (p *StreamDecisionParser) Ready() bool {
	return p.decisionFound
}

// DecisionJSON 返回标签内提取的 JSON 字符串。
// 仅在 Ready()=true 且 Result().Fallback=false && Result().ParseFailed=false 时有效。
func (p *StreamDecisionParser) DecisionJSON() string {
	return p.decisionJSON
}

// Result 返回完整解析结果，包含 fallback/parseFailed 状态和原始缓冲。
func (p *StreamDecisionParser) Result() *StreamDecisionResult {
	r := &StreamDecisionResult{
		DecisionJSON: p.decisionJSON,
		BeforeText:   p.beforeText,
		AfterText:    p.afterText,
		RawBuffer:    p.rawBuf,
	}
	if p.rawBuf != "" && p.decisionJSON == "" {
		// 没有提取到 JSON：判断是 fallback 还是 parseFailed。
		// fallback = buf 里根本没有标签；parseFailed = 有标签但 JSON 提取失败。
		if decisionTagRegex.FindStringSubmatchIndex(p.rawBuf) != nil {
			r.ParseFailed = true
		} else {
			r.Fallback = true
		}
	}
	return r
}

// extractJSONFromTag 从标签内文本中提取第一个完整 JSON 对象。
// 复用括号计数逻辑，与 infrallm.ExtractJSONObject 一致。
func extractJSONFromTag(text string) string {
	clean := strings.TrimSpace(text)
	if clean == "" {
		return ""
	}

	start := strings.Index(clean, "{")
	if start < 0 {
		return ""
	}

	depth := 0
	inString := false
	escaped := false
	for idx := start; idx < len(clean); idx++ {
		ch := clean[idx]

		if escaped {
			escaped = false
			continue
		}
		if ch == '\\' && inString {
			escaped = true
			continue
		}
		if ch == '"' {
			inString = !inString
			continue
		}
		if inString {
			continue
		}

		switch ch {
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return clean[start : idx+1]
			}
		}
	}
	return ""
}
