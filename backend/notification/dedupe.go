package notification

import (
	"fmt"
	"strings"
	"time"
)

const (
	// DefaultFeishuDedupeWindow 是 notification 第一版固定的 30 分钟去重窗口。
	DefaultFeishuDedupeWindow = 30 * time.Minute
)

// BuildTimeWindowDedupeKey 构造“user_id + trigger_type + time_window”去重键。
//
// 职责边界：
// 1. 供事件发布方在生成 `notification.feishu.requested` payload 时复用；
// 2. 只负责把 30 分钟窗口归一成稳定 key，不负责落 notification_records；
// 3. unfinished_feedback 若要改用 feedback_id / idempotency_key，可不使用这个 helper。
func BuildTimeWindowDedupeKey(userID int, triggerType string, requestedAt time.Time, window time.Duration) string {
	if window <= 0 {
		window = DefaultFeishuDedupeWindow
	}
	if userID <= 0 || strings.TrimSpace(triggerType) == "" || requestedAt.IsZero() {
		return ""
	}

	// 1. 先把请求时间归一到固定窗口起点，保证 30 分钟内多次触发得到同一 key。
	// 2. requestedAt 为空或非法时直接返回空字符串，让上游显式感知入参不完整。
	windowStartUnix := requestedAt.Unix() / int64(window.Seconds())
	return fmt.Sprintf("%d:%s:%d", userID, strings.TrimSpace(triggerType), windowStartUnix)
}
