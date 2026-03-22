package route

import "testing"

func TestParseRouteControlTag_SchedulePlanCreate(t *testing.T) {
	nonce := "nonce-create"
	raw := `<SMARTFLOW_ROUTE nonce="nonce-create" action="schedule_plan_create"></SMARTFLOW_ROUTE>
<SMARTFLOW_REASON>新建排程</SMARTFLOW_REASON>`

	decision, err := ParseRouteControlTag(raw, nonce)
	if err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	if decision.Action != ActionSchedulePlanCreate {
		t.Fatalf("action 不匹配，期望=%s 实际=%s", ActionSchedulePlanCreate, decision.Action)
	}
}

func TestParseRouteControlTag_SchedulePlanRefine(t *testing.T) {
	nonce := "nonce-refine"
	raw := `<SMARTFLOW_ROUTE nonce="nonce-refine" action="schedule_plan_refine"></SMARTFLOW_ROUTE>
<SMARTFLOW_REASON>微调排程</SMARTFLOW_REASON>`

	decision, err := ParseRouteControlTag(raw, nonce)
	if err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	if decision.Action != ActionSchedulePlanRefine {
		t.Fatalf("action 不匹配，期望=%s 实际=%s", ActionSchedulePlanRefine, decision.Action)
	}
}

func TestParseRouteControlTag_LegacySchedulePlan(t *testing.T) {
	nonce := "nonce-legacy"
	raw := `<SMARTFLOW_ROUTE nonce="nonce-legacy" action="schedule_plan"></SMARTFLOW_ROUTE>
<SMARTFLOW_REASON>兼容旧动作</SMARTFLOW_REASON>`

	decision, err := ParseRouteControlTag(raw, nonce)
	if err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	if decision.Action != ActionSchedulePlanCreate {
		t.Fatalf("旧动作映射错误，期望=%s 实际=%s", ActionSchedulePlanCreate, decision.Action)
	}
}
