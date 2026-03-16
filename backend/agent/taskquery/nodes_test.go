package taskquery

import (
	"strings"
	"testing"
)

// TestExtractExplicitLimitFromUser_Number
// 目的：验证用户原话里的阿拉伯数字数量诉求可以被正确提取。
func TestExtractExplicitLimitFromUser_Number(t *testing.T) {
	limit, ok := extractExplicitLimitFromUser("给我3个优先级低的任务")
	if !ok {
		t.Fatalf("期望识别到显式数量")
	}
	if limit != 3 {
		t.Fatalf("数量识别错误，期望=3 实际=%d", limit)
	}
}

// TestExtractExplicitLimitFromUser_ChineseNumber
// 目的：验证常见中文数字（如“前五个”）也能识别数量。
func TestExtractExplicitLimitFromUser_ChineseNumber(t *testing.T) {
	limit, ok := extractExplicitLimitFromUser("前五个简单任务给我看看")
	if !ok {
		t.Fatalf("期望识别到中文数量")
	}
	if limit != 5 {
		t.Fatalf("数量识别错误，期望=5 实际=%d", limit)
	}
}

// TestExtractExplicitLimitFromUser_LaiYiGe
// 目的：验证“来一个...”这种口语数量表达也能识别为 1。
func TestExtractExplicitLimitFromUser_LaiYiGe(t *testing.T) {
	limit, ok := extractExplicitLimitFromUser("来一个我的简单任务")
	if !ok {
		t.Fatalf("期望识别到“来一个”的显式数量")
	}
	if limit != 1 {
		t.Fatalf("数量识别错误，期望=1 实际=%d", limit)
	}
}

// TestBuildTaskQueryFinalReply_RespectsLimit
// 目的：验证最终回复会按 plan.limit 输出对应条数，而不是由 LLM 自由决定条数。
func TestBuildTaskQueryFinalReply_RespectsLimit(t *testing.T) {
	items := []TaskQueryToolRecord{
		{ID: 1, Title: "任务1", PriorityLabel: "简单不重要", DeadlineAt: "2026-03-16 10:00"},
		{ID: 2, Title: "任务2", PriorityLabel: "简单不重要", DeadlineAt: "2026-03-17 10:00"},
		{ID: 3, Title: "任务3", PriorityLabel: "简单不重要", DeadlineAt: "2026-03-18 10:00"},
	}
	reply := buildTaskQueryFinalReply(items, QueryPlan{Limit: 2}, "好的")
	if !strings.Contains(reply, "整理了 2 条任务") {
		t.Fatalf("回复未体现 limit=2，reply=%s", reply)
	}
	if strings.Contains(reply, "3. ") {
		t.Fatalf("回复不应出现第3条，reply=%s", reply)
	}
}

// TestBuildTaskQueryFinalReply_NoDuplicateList
// 目的：验证当 llmReply 已带列表内容时，不会和后端确定性列表重复拼接。
func TestBuildTaskQueryFinalReply_NoDuplicateList(t *testing.T) {
	items := []TaskQueryToolRecord{
		{ID: 1, Title: "任务1", PriorityLabel: "简单不重要", DeadlineAt: "2026-03-16 10:00"},
	}
	llmReply := "以下是你的任务：\n#1 任务1"
	reply := buildTaskQueryFinalReply(items, QueryPlan{Limit: 1}, llmReply)
	if strings.Contains(reply, "以下是你的任务") {
		t.Fatalf("不应保留 llm 列表头，reply=%s", reply)
	}
	if !strings.Contains(reply, "整理了 1 条任务") {
		t.Fatalf("应保留后端确定性列表头，reply=%s", reply)
	}
}

// TestApplyRetryPatch_RespectExplicitLimit
// 目的：验证用户显式数量存在时，反思补丁不能改写 limit。
func TestApplyRetryPatch_RespectExplicitLimit(t *testing.T) {
	plan := QueryPlan{Limit: 1, SortBy: "deadline", Order: "asc"}
	limit := 10
	next := applyRetryPatch(plan, taskQueryRetryPatch{Limit: &limit}, 1)
	if next.Limit != 1 {
		t.Fatalf("显式数量锁应生效，期望=1 实际=%d", next.Limit)
	}
}
