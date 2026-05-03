package llm

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

// ParseJSONObject 解析模型返回内容中的 JSON 对象。
// 1. 先剥离常见的 markdown 代码块包装。
// 2. 再从混合文本里提取最外层 JSON 对象。
// 3. 这里只负责结构解析，不负责字段合法性校验。
func ParseJSONObject[T any](raw string) (*T, error) {
	clean := strings.TrimSpace(raw)
	if clean == "" {
		return nil, errors.New("模型返回为空，无法解析 JSON")
	}

	objectText := ExtractJSONObject(clean)
	if objectText == "" {
		return nil, fmt.Errorf("模型返回中未找到 JSON 对象: %s", truncateForError(clean))
	}

	var out T
	if err := json.Unmarshal([]byte(objectText), &out); err != nil {
		return nil, fmt.Errorf("JSON 解析失败: %w", err)
	}
	return &out, nil
}

// ExtractJSONObject 从混合文本中提取第一个完整的 JSON 对象。
func ExtractJSONObject(text string) string {
	clean := trimMarkdownCodeFence(strings.TrimSpace(text))
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

func trimMarkdownCodeFence(text string) string {
	trimmed := strings.TrimSpace(text)
	if !strings.HasPrefix(trimmed, "```") {
		return trimmed
	}

	lines := strings.Split(trimmed, "\n")
	if len(lines) == 0 {
		return trimmed
	}

	body := lines[1:]
	if len(body) > 0 && strings.TrimSpace(body[len(body)-1]) == "```" {
		body = body[:len(body)-1]
	}
	return strings.TrimSpace(strings.Join(body, "\n"))
}

func truncateForError(text string) string {
	if len(text) <= 160 {
		return text
	}
	return text[:160] + "..."
}
