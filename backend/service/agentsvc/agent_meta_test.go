package agentsvc

import (
	"strings"
	"testing"

	"github.com/cloudwego/eino/schema"
)

// TestNormalizeConversationTitle
// 目的：确保标题清洗逻辑能去掉引号/前缀并裁剪到上限长度。
func TestNormalizeConversationTitle(t *testing.T) {
	raw := "标题：\"明天上午去机场接人并顺路取快递，记得提前出门\""
	got := normalizeConversationTitle(raw)
	if strings.HasPrefix(got, "标题") {
		t.Fatalf("标题前缀未清洗，got=%s", got)
	}
	if len([]rune(got)) > conversationTitleMaxChars {
		t.Fatalf("标题长度超限，got=%s", got)
	}
	if strings.TrimSpace(got) == "" {
		t.Fatalf("清洗后标题不应为空")
	}
}

// TestBuildConversationTitleUserPrompt
// 目的：确保 prompt 构造时能正确标注用户/助手角色并包含有效内容。
func TestBuildConversationTitleUserPrompt(t *testing.T) {
	msgs := []*schema.Message{
		{Role: schema.User, Content: "明天早上九点去机场接人"},
		{Role: schema.Assistant, Content: "收到，我帮你记下了。"},
	}
	prompt := buildConversationTitleUserPrompt(msgs)
	if !strings.Contains(prompt, "用户：明天早上九点去机场接人") {
		t.Fatalf("prompt 未包含用户内容，prompt=%s", prompt)
	}
	if !strings.Contains(prompt, "助手：收到，我帮你记下了。") {
		t.Fatalf("prompt 未包含助手内容，prompt=%s", prompt)
	}
}
