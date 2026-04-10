package llm

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

// ParseJSONObject 解析模型返回中的 JSON 对象。
//
// 职责边界：
// 1. 负责处理“模型输出前后夹杂解释文字 / markdown 代码块”的常见情况；
// 2. 负责提取最外层 JSON object 并反序列化为目标结构；
// 3. 不负责业务字段合法性校验，应由上层调用方自行校验。
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

// ExtractJSONObject 从混合文本里提取第一个完整 JSON 对象。
//
// 设计说明：
// 1. LLM 很容易输出“这里是结果：{...}”这种半结构化文本；
// 2. 这里用括号计数而不是正则，避免嵌套对象一多就误截断；
// 3. 目前只提取 object，不提取 array，因为当前契约基本都是对象。
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

	// 1. 去掉首行 ```json / ```；
	// 2. 若末行是 ```，一并去掉；
	// 3. 中间正文保持原样，避免破坏 JSON 的换行结构。
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
