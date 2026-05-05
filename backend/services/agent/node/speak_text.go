package agentnode

import (
	"regexp"
	"strings"
)

// listItemRe 匹配被粘连在一起的列表序号，用于正文归一化时自动补换行。
var listItemRe = regexp.MustCompile(`([^\n])([2-9][\.、]\s)`)

// normalizeSpeak 统一整理要展示给用户的正文。
func normalizeSpeak(speak string) string {
	speak = strings.TrimSpace(speak)
	if speak == "" {
		return speak
	}
	if !strings.Contains(speak, "\n") {
		speak = listItemRe.ReplaceAllString(speak, "$1\n$2")
	}
	return speak + "\n"
}
