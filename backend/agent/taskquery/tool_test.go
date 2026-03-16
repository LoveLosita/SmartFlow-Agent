package taskquery

import "testing"

// TestNormalizeToolInput_Default
// 目的：验证空入参会回填默认查询参数，保证工具在“参数缺失”场景仍可执行。
func TestNormalizeToolInput_Default(t *testing.T) {
	req, err := normalizeToolInput(nil)
	if err != nil {
		t.Fatalf("不应报错: %v", err)
	}
	if req.SortBy != "deadline" || req.Order != "asc" || req.Limit != 5 || req.IncludeCompleted {
		t.Fatalf("默认值异常: %+v", req)
	}
}

// TestNormalizeToolInput_InvalidQuadrant
// 目的：验证 quadrant 越界时会被拦截，避免无效过滤条件进入业务层。
func TestNormalizeToolInput_InvalidQuadrant(t *testing.T) {
	invalid := 6
	_, err := normalizeToolInput(&TaskQueryToolInput{
		Quadrant: &invalid,
	})
	if err == nil {
		t.Fatalf("期望 quadrant 越界时报错")
	}
}

// TestNormalizeToolInput_DateRange
// 目的：验证时间上下界可解析并正确落入请求结构。
func TestNormalizeToolInput_DateRange(t *testing.T) {
	req, err := normalizeToolInput(&TaskQueryToolInput{
		DeadlineAfter:  "2026-03-01 08:00",
		DeadlineBefore: "2026-03-31",
	})
	if err != nil {
		t.Fatalf("不应报错: %v", err)
	}
	if req.DeadlineAfter == nil || req.DeadlineBefore == nil {
		t.Fatalf("时间上下界不应为空: %+v", req)
	}
	if req.DeadlineAfter.After(*req.DeadlineBefore) {
		t.Fatalf("时间上下界关系异常: after=%v before=%v", req.DeadlineAfter, req.DeadlineBefore)
	}
}
