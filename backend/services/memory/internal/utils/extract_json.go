package utils

import (
	"encoding/json"
	"errors"
	"regexp"
	"strings"
)

var fencedJSONPattern = regexp.MustCompile("(?s)```(?:json)?\\s*([\\[{].*[\\]}])\\s*```")

// ExtractJSON 从模型输出中提取 JSON 文本（兼容代码块包裹）。
//
// 步骤：
// 1. 先判断整段文本是否本身就是合法 JSON；
// 2. 再尝试匹配 ```json ... ``` 代码块；
// 3. 最后做一次“首个 JSON 对象/数组”扫描提取。
func ExtractJSON(raw string) (string, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return "", errors.New("empty model output")
	}

	// 1. 直接 JSON 命中时，避免做额外启发式扫描。
	if json.Valid([]byte(trimmed)) {
		return trimmed, nil
	}

	// 2. 兼容 markdown 代码块包裹 JSON。
	matches := fencedJSONPattern.FindStringSubmatch(trimmed)
	if len(matches) > 1 {
		candidate := strings.TrimSpace(matches[1])
		if json.Valid([]byte(candidate)) {
			return candidate, nil
		}
	}

	// 3. 兜底扫描首个完整 JSON 片段，尽量提升容错能力。
	if candidate, ok := findFirstJSONSegment(trimmed); ok {
		return candidate, nil
	}
	return "", errors.New("json not found in model output")
}

func findFirstJSONSegment(raw string) (string, bool) {
	start := -1
	var open, close rune
	for i, ch := range raw {
		if ch == '{' {
			start = i
			open = '{'
			close = '}'
			break
		}
		if ch == '[' {
			start = i
			open = '['
			close = ']'
			break
		}
	}
	if start < 0 {
		return "", false
	}

	depth := 0
	inString := false
	escaped := false
	for i, ch := range raw[start:] {
		if inString {
			if escaped {
				escaped = false
				continue
			}
			if ch == '\\' {
				escaped = true
				continue
			}
			if ch == '"' {
				inString = false
			}
			continue
		}
		if ch == '"' {
			inString = true
			continue
		}
		if ch == open {
			depth++
			continue
		}
		if ch == close {
			depth--
			if depth == 0 {
				candidate := strings.TrimSpace(raw[start : start+i+1])
				if json.Valid([]byte(candidate)) {
					return candidate, true
				}
				return "", false
			}
		}
	}
	return "", false
}
