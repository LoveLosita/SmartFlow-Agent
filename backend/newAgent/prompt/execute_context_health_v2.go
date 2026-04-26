package newagentprompt

import (
	"fmt"
	"strings"
)

// summarizeExecuteAnalyzeHealthObservationV2 把 analyze_health 结果压成更短的单行摘要。
//
// 职责边界：
// 1. 只保留 execute 下一步真正需要消费的裁决字段，不重复展开整份 metrics。
// 2. 若存在候选，会优先展示“候选数量 + 前两个候选工具”，帮助模型迅速进入选择题。
// 3. 这里只做摘要，不负责改变决策含义；真实判定仍以 analyze_health 原始 JSON 为准。
func summarizeExecuteAnalyzeHealthObservationV2(payload map[string]any) string {
	decision, _ := payload["decision"].(map[string]any)
	metrics, _ := payload["metrics"].(map[string]any)
	rhythmMetrics, _ := metrics["rhythm"].(map[string]any)
	tightnessMetrics, _ := metrics["tightness"].(map[string]any)
	candidates, _ := decision["candidates"].([]any)

	parts := make([]string, 0, 7)
	if text := compactHealthAny(decision["should_continue_optimize"]); text != "" {
		parts = append(parts, "continue="+text)
	}
	if text := strings.TrimSpace(asExecuteString(decision["recommended_operation"])); text != "" {
		parts = append(parts, "recommended="+text)
	}
	if text := strings.TrimSpace(asExecuteString(tightnessMetrics["tightness_level"])); text != "" {
		parts = append(parts, "tightness="+text)
	}
	if text := buildBlockBalanceSummary(rhythmMetrics); text != "" {
		parts = append(parts, text)
	}
	if text := compactHealthAny(decision["is_forced_imperfection"]); text != "" {
		parts = append(parts, "forced="+text)
	}
	if len(candidates) > 0 {
		parts = append(parts, fmt.Sprintf("candidates=%d", len(candidates)))
		if preview := compactHealthCandidatePreview(candidates); preview != "" {
			parts = append(parts, "options="+preview)
		}
	}
	if text := strings.TrimSpace(asExecuteString(decision["primary_problem"])); text != "" {
		parts = append(parts, "problem="+compactExecuteText(text, 36))
	}
	if len(parts) == 0 {
		return "返回了健康裁决结果。"
	}
	return strings.Join(parts, " | ")
}

// buildBlockBalanceSummary 把 block_balance 连同正负来源一起压成单段摘要。
//
// 职责边界：
// 1. 这里只做 execute 摘要层的可读性补充，避免 LLM 只看到 balance=0 却看不到来源。
// 2. 不改变 analyze_health 原始 JSON 结构；原始结构仍由 metrics.rhythm 提供完整字段。
// 3. 若三个字段都缺失，则直接留空，避免构造误导性的默认值。
func buildBlockBalanceSummary(rhythmMetrics map[string]any) string {
	if len(rhythmMetrics) == 0 {
		return ""
	}

	blockBalance := compactHealthAny(rhythmMetrics["block_balance"])
	fragmentedCount := compactHealthAny(rhythmMetrics["fragmented_count"])
	compressedCount := compactHealthAny(rhythmMetrics["compressed_run_count"])
	if blockBalance == "" && fragmentedCount == "" && compressedCount == "" {
		return ""
	}

	return fmt.Sprintf(
		"block_balance=%s(fragmented=%s,compressed=%s)",
		fallbackExecuteText(blockBalance, "?"),
		fallbackExecuteText(fragmentedCount, "?"),
		fallbackExecuteText(compressedCount, "?"),
	)
}

func compactHealthCandidatePreview(candidates []any) string {
	if len(candidates) == 0 {
		return ""
	}
	preview := make([]string, 0, 2)
	for _, raw := range candidates {
		item, _ := raw.(map[string]any)
		if len(item) == 0 {
			continue
		}
		id := strings.TrimSpace(asExecuteString(item["candidate_id"]))
		tool := strings.TrimSpace(asExecuteString(item["tool"]))
		if id == "" && tool == "" {
			continue
		}
		switch {
		case id != "" && tool != "":
			preview = append(preview, id+":"+tool)
		case id != "":
			preview = append(preview, id)
		default:
			preview = append(preview, tool)
		}
		if len(preview) >= 2 {
			break
		}
	}
	return strings.Join(preview, ",")
}
