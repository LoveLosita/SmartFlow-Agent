package service

import (
	"strings"
	"testing"

	"github.com/LoveLosita/smartflow/backend/agent/quicknote"
	"github.com/LoveLosita/smartflow/backend/agent/route"
)

// TestParseQuickNoteRouteControlTag_QuickNote
// 目的：验证模型控制码在 action=quick_note 时可被稳定解析，
// 并且会校验 nonce，避免历史脏内容或伪造片段误命中。
func TestParseQuickNoteRouteControlTag_QuickNote(t *testing.T) {
	nonce := "abc123nonce"
	raw := `<SMARTFLOW_ROUTE nonce="abc123nonce" action="quick_note"></SMARTFLOW_ROUTE>
<SMARTFLOW_REASON>用户明确在请求未来提醒</SMARTFLOW_REASON>`

	decision, err := route.ParseQuickNoteRouteControlTag(raw, nonce)
	if err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	if decision == nil {
		t.Fatalf("decision 不应为空")
	}
	if decision.Action != route.ActionQuickNote {
		t.Fatalf("action 解析错误，期望=%s 实际=%s", route.ActionQuickNote, decision.Action)
	}
	if strings.TrimSpace(decision.Reason) == "" {
		t.Fatalf("reason 不应为空")
	}
}

// TestParseQuickNoteRouteControlTag_NonceMismatch
// 目的：确保 nonce 不匹配时直接报错，避免把非本次请求的控制码当作有效路由。
func TestParseQuickNoteRouteControlTag_NonceMismatch(t *testing.T) {
	raw := `<SMARTFLOW_ROUTE nonce="wrongnonce" action="chat"></SMARTFLOW_ROUTE>`
	if _, err := route.ParseQuickNoteRouteControlTag(raw, "expectednonce"); err == nil {
		t.Fatalf("期望 nonce 不匹配时报错，但未报错")
	}
}

// TestBuildQuickNoteFinalReply_NoFalseSuccessWithoutTaskID
// 目的：即使 state.Persisted 被错误置为 true，只要 task_id 无效，也不能返回“安排成功”文案。
func TestBuildQuickNoteFinalReply_NoFalseSuccessWithoutTaskID(t *testing.T) {
	state := &quicknote.QuickNoteState{
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
// 目的：当聚合规划阶段已经产出 banter 时，最终回复应直接复用，避免再次调用润色模型。
func TestBuildQuickNoteFinalReply_UseExtractedBanter(t *testing.T) {
	state := &quicknote.QuickNoteState{
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
