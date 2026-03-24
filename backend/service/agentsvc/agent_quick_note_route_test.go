package agentsvc

import (
	"strings"
	"testing"

	agentmodel "github.com/LoveLosita/smartflow/backend/agent2/model"
	agentrouter "github.com/LoveLosita/smartflow/backend/agent2/router"
)

// TestParseQuickNoteRouteControlTag_QuickNote
// 目的：
// 1. 验证旧 quick note 兼容入口仍然可以解析控制码；
// 2. 验证旧 action=quick_note 会被统一映射到新动作 quick_note_create；
// 3. 验证 reason 仍然会被保留下来，方便上层做阶段提示与排障。
func TestParseQuickNoteRouteControlTag_QuickNote(t *testing.T) {
	nonce := "abc123nonce"
	raw := `<SMARTFLOW_ROUTE nonce="abc123nonce" action="quick_note"></SMARTFLOW_ROUTE>
<SMARTFLOW_REASON>用户明确在请求未来提醒</SMARTFLOW_REASON>`

	decision, err := agentrouter.ParseQuickNoteRouteControlTag(raw, nonce)
	if err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	if decision == nil {
		t.Fatalf("decision 不应为空")
	}
	if decision.Action != agentrouter.ActionQuickNoteCreate {
		t.Fatalf("action 解析错误，期望=%s 实际=%s", agentrouter.ActionQuickNoteCreate, decision.Action)
	}
	if strings.TrimSpace(decision.Reason) == "" {
		t.Fatalf("reason 不应为空")
	}
}

// TestParseRouteControlTag_TaskQuery
// 目的：验证通用分流控制码在 action=task_query 时可以被稳定解析。
func TestParseRouteControlTag_TaskQuery(t *testing.T) {
	nonce := "taskquerynonce"
	raw := `<SMARTFLOW_ROUTE nonce="taskquerynonce" action="task_query"></SMARTFLOW_ROUTE>
<SMARTFLOW_REASON>用户在查最紧急任务</SMARTFLOW_REASON>`

	decision, err := agentrouter.ParseRouteControlTag(raw, nonce)
	if err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	if decision == nil {
		t.Fatalf("decision 不应为空")
	}
	if decision.Action != agentrouter.ActionTaskQuery {
		t.Fatalf("action 解析错误，期望=%s 实际=%s", agentrouter.ActionTaskQuery, decision.Action)
	}
}

// TestParseQuickNoteRouteControlTag_NonceMismatch
// 目的：确保 nonce 不匹配时直接报错，避免把别的请求控制码误判成当前请求。
func TestParseQuickNoteRouteControlTag_NonceMismatch(t *testing.T) {
	raw := `<SMARTFLOW_ROUTE nonce="wrongnonce" action="chat"></SMARTFLOW_ROUTE>`
	if _, err := agentrouter.ParseQuickNoteRouteControlTag(raw, "expectednonce"); err == nil {
		t.Fatalf("期望 nonce 不匹配时报错，但未报错")
	}
}

// TestBuildQuickNoteFinalReply_NoFalseSuccessWithoutTaskID
// 目的：
// 1. 即使状态被错误标记为 Persisted=true；
// 2. 只要没有有效 task_id，就不能回成功文案；
// 3. 避免出现“回复成功但库里没数据”的假成功体验。
func TestBuildQuickNoteFinalReply_NoFalseSuccessWithoutTaskID(t *testing.T) {
	state := &agentmodel.QuickNoteState{
		Persisted:       true,
		PersistedTaskID: 0,
		ExtractedTitle:  "去下馆子",
	}

	reply := buildQuickNoteFinalReply(nil, nil, "我今天晚上6点要去下馆子，记得喊我", state)
	if strings.Contains(reply, "给你安排上了") || strings.Contains(reply, "已安排") {
		t.Fatalf("不应返回成功文案，实际回复=%s", reply)
	}
}

// TestBuildQuickNoteFinalReply_UseExtractedBanter
// 目的：
// 1. 当聚合规划阶段已经产出 banter 时，最终回复应直接复用；
// 2. 避免为了润色再次调用模型，增加不必要时延。
func TestBuildQuickNoteFinalReply_UseExtractedBanter(t *testing.T) {
	state := &agentmodel.QuickNoteState{
		Persisted:         true,
		PersistedTaskID:   12,
		ExtractedTitle:    "明天去取快递",
		ExtractedPriority: 2,
		ExtractedBanter:   "取件路上注意保暖，别被风吹懵了。",
	}

	reply := buildQuickNoteFinalReply(nil, nil, "明天上午12点我要去取快递，到时候记得q我", state)
	if !strings.Contains(reply, "取件路上注意保暖") {
		t.Fatalf("期望复用 ExtractedBanter，实际回复=%s", reply)
	}
}
