package newagenttools

import "fmt"

// ==================== 参数解析辅助 ====================
// 这些函数专门用于从 LLM 输出的 map[string]any 中提取工具参数。
// JSON 反序列化后数字默认为 float64，字符串为 string，需要类型断言。

// argsInt 从 map 中提取 int 值。支持 float64（JSON 反序列化的默认类型）。
func argsInt(args map[string]any, key string) (int, bool) {
	v, ok := args[key]
	if !ok {
		return 0, false
	}
	switch n := v.(type) {
	case float64:
		return int(n), true
	case int:
		return n, true
	}
	return 0, false
}

// argsString 从 map 中提取 string 值。
func argsString(args map[string]any, key string) (string, bool) {
	v, ok := args[key]
	if !ok {
		return "", false
	}
	s, ok := v.(string)
	return s, ok
}

// argsIntPtr 从 map 中提取可选 int 值，不存在返回 nil。
func argsIntPtr(args map[string]any, key string) *int {
	v, ok := argsInt(args, key)
	if !ok {
		return nil
	}
	return &v
}

// argsStringPtr 从 map 中提取可选 string 值，不存在返回 nil。
func argsStringPtr(args map[string]any, key string) *string {
	v, ok := argsString(args, key)
	if !ok {
		return nil
	}
	return &v
}

// argsIntSlice 从 map 中提取 int 数组，支持 []any / []int / []float64。
func argsIntSlice(args map[string]any, key string) ([]int, bool) {
	v, ok := args[key]
	if !ok {
		return nil, false
	}
	switch arr := v.(type) {
	case []int:
		if len(arr) == 0 {
			return []int{}, true
		}
		result := make([]int, len(arr))
		copy(result, arr)
		return result, true
	case []float64:
		result := make([]int, 0, len(arr))
		for _, item := range arr {
			result = append(result, int(item))
		}
		return result, true
	case []any:
		result := make([]int, 0, len(arr))
		for _, item := range arr {
			switch n := item.(type) {
			case float64:
				result = append(result, int(n))
			case int:
				result = append(result, n)
			default:
				return nil, false
			}
		}
		return result, true
	default:
		return nil, false
	}
}

// argsMoveList 从 map 中提取 batch_move 的 moves 数组。
func argsMoveList(args map[string]any) ([]MoveRequest, error) {
	v, ok := args["moves"]
	if !ok {
		return nil, fmt.Errorf("缺少 moves 参数")
	}
	arr, ok := v.([]any)
	if !ok {
		return nil, fmt.Errorf("moves 参数必须是数组")
	}
	moves := make([]MoveRequest, 0, len(arr))
	for i, item := range arr {
		m, ok := item.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("moves[%d] 不是有效对象", i)
		}
		taskID, ok := argsInt(m, "task_id")
		if !ok {
			return nil, fmt.Errorf("moves[%d].task_id 缺失或无效", i)
		}
		newDay, ok := argsInt(m, "new_day")
		if !ok {
			return nil, fmt.Errorf("moves[%d].new_day 缺失或无效", i)
		}
		newSlotStart, ok := argsInt(m, "new_slot_start")
		if !ok {
			return nil, fmt.Errorf("moves[%d].new_slot_start 缺失或无效", i)
		}
		moves = append(moves, MoveRequest{
			TaskID:       taskID,
			NewDay:       newDay,
			NewSlotStart: newSlotStart,
		})
	}
	return moves, nil
}
