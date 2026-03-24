package agentnode

import "testing"

// TestNormalizeTaskQueryToolInput_Default 验证空输入会回填默认查询参数。
func TestNormalizeTaskQueryToolInput_Default(t *testing.T) {
	req, err := normalizeTaskQueryToolInput(nil)
	if err != nil {
		t.Fatalf("不应报错: %v", err)
	}
	if req.SortBy != "deadline" || req.Order != "asc" || req.Limit != 5 || req.IncludeCompleted {
		t.Fatalf("默认值异常: %+v", req)
	}
}

// TestNormalizeTaskQueryToolInput_InvalidQuadrant 验证 quadrant 越界时会被拦截。
func TestNormalizeTaskQueryToolInput_InvalidQuadrant(t *testing.T) {
	invalid := 6
	_, err := normalizeTaskQueryToolInput(&TaskQueryToolInput{Quadrant: &invalid})
	if err == nil {
		t.Fatalf("期望 quadrant 越界时报错")
	}
}

// TestNormalizeTaskQueryToolInput_DateRange 验证时间上下界可被正确解析。
func TestNormalizeTaskQueryToolInput_DateRange(t *testing.T) {
	req, err := normalizeTaskQueryToolInput(&TaskQueryToolInput{
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
