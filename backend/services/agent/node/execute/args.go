package agentexecute

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
)

func intSliceToSet(values []int) map[int]struct{} {
	result := make(map[int]struct{}, len(values))
	for _, value := range values {
		result[value] = struct{}{}
	}
	return result
}

func readIntAnyFromMap(args map[string]any, keys ...string) (int, bool) {
	for _, key := range keys {
		if args == nil {
			continue
		}
		raw, exists := args[key]
		if !exists {
			continue
		}
		if value, ok := parseAnyToInt(raw); ok {
			return value, true
		}
	}
	return 0, false
}

func readIntSliceAnyFromMap(args map[string]any, keys ...string) []int {
	for _, key := range keys {
		if args == nil {
			continue
		}
		raw, exists := args[key]
		if !exists {
			continue
		}
		values := parseAnyToIntSlice(raw)
		if len(values) > 0 {
			return values
		}
	}
	return nil
}

func readStringAnyFromMap(args map[string]any, keys ...string) string {
	for _, key := range keys {
		if args == nil {
			continue
		}
		raw, exists := args[key]
		if !exists {
			continue
		}
		if text, ok := raw.(string); ok {
			return text
		}
	}
	return ""
}

func parseAnyToInt(value any) (int, bool) {
	switch v := value.(type) {
	case int:
		return v, true
	case int8:
		return int(v), true
	case int16:
		return int(v), true
	case int32:
		return int(v), true
	case int64:
		return int(v), true
	case float32:
		return int(v), true
	case float64:
		return int(v), true
	case json.Number:
		if iv, err := v.Int64(); err == nil {
			return int(iv), true
		}
		if fv, err := v.Float64(); err == nil {
			return int(fv), true
		}
	case string:
		text := strings.TrimSpace(v)
		if text == "" {
			return 0, false
		}
		iv, err := strconv.Atoi(text)
		if err == nil {
			return iv, true
		}
	}
	return 0, false
}

func parseAnyToIntSlice(value any) []int {
	switch values := value.(type) {
	case []int:
		result := make([]int, 0, len(values))
		for _, value := range values {
			result = append(result, value)
		}
		return result
	case []any:
		result := make([]int, 0, len(values))
		for _, item := range values {
			iv, ok := parseAnyToInt(item)
			if !ok {
				continue
			}
			result = append(result, iv)
		}
		return result
	default:
		return nil
	}
}

func parseAnyToStringSlice(value any) []string {
	switch values := value.(type) {
	case []string:
		result := make([]string, 0, len(values))
		for _, item := range values {
			text := strings.TrimSpace(item)
			if text == "" {
				continue
			}
			result = append(result, text)
		}
		return result
	case []any:
		result := make([]string, 0, len(values))
		for _, item := range values {
			text := strings.TrimSpace(fmt.Sprintf("%v", item))
			if text == "" || text == "<nil>" {
				continue
			}
			result = append(result, text)
		}
		return result
	default:
		return nil
	}
}

func truncateText(text string, maxLen int) string {
	text = strings.TrimSpace(text)
	if len(text) <= maxLen {
		return text
	}
	if maxLen <= 3 {
		return text[:maxLen]
	}
	return text[:maxLen-3] + "..."
}
