package newagenttools

import (
	"fmt"
	"sort"
	"strings"
)

// validateToolArgsStrict 校验工具参数是否全部命中 schema 白名单。
//
// 职责边界：
// 1. 只做“字段名是否允许”的校验，不校验字段值合法性；
// 2. 发现未知字段时直接报错，避免静默忽略导致范围漂移；
// 3. 该函数不做别名兼容，调用方应自行传入 schema 中允许的字段。
func validateToolArgsStrict(args map[string]any, allowedKeys []string) error {
	if len(args) == 0 {
		return nil
	}
	allowed := make(map[string]struct{}, len(allowedKeys))
	for _, key := range allowedKeys {
		allowed[strings.TrimSpace(key)] = struct{}{}
	}

	unknown := make([]string, 0, len(args))
	for key := range args {
		trimmed := strings.TrimSpace(key)
		if trimmed == "" {
			continue
		}
		if _, ok := allowed[trimmed]; ok {
			continue
		}
		unknown = append(unknown, trimmed)
	}
	if len(unknown) == 0 {
		return nil
	}

	sort.Strings(unknown)
	hint := "请仅使用当前工具 schema 中声明的参数字段。"
	if containsAnyUnknownArg(unknown, "day_from", "day_to") {
		hint = "请仅使用当前工具 schema 中声明的参数字段；day_from/day_to 不受支持，请改用 day_start/day_end。"
	}
	return fmt.Errorf("参数非法：%s。%s", strings.Join(unknown, "、"), hint)
}

func containsAnyUnknownArg(keys []string, targets ...string) bool {
	if len(keys) == 0 || len(targets) == 0 {
		return false
	}
	targetSet := make(map[string]struct{}, len(targets))
	for _, target := range targets {
		targetSet[strings.TrimSpace(target)] = struct{}{}
	}
	for _, key := range keys {
		if _, ok := targetSet[strings.TrimSpace(key)]; ok {
			return true
		}
	}
	return false
}
