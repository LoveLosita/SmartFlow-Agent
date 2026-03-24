package agentshared

import (
	"sync"
	"time"
)

const (
	// MinuteLayout 是 agent2 内部统一的分钟级时间文本格式。
	//
	// 设计原因：
	// 1. agent 里大量场景只需要精确到分钟；
	// 2. 秒级精度会增加提示词噪声，也容易让“同一请求内的当前时间”出现抖动；
	// 3. 先统一成一份常量，后续 quicknote / schedule 都直接复用。
	MinuteLayout = "2006-01-02 15:04"
)

var (
	shanghaiLocOnce sync.Once
	shanghaiLoc     *time.Location
)

// ShanghaiLocation 返回 agent2 内部统一使用的东八区时区。
func ShanghaiLocation() *time.Location {
	shanghaiLocOnce.Do(func() {
		loc, err := time.LoadLocation("Asia/Shanghai")
		if err != nil {
			// 兜底使用固定东八区，避免极端环境下因为系统时区文件缺失导致整个链路失败。
			loc = time.FixedZone("CST", 8*3600)
		}
		shanghaiLoc = loc
	})
	return shanghaiLoc
}

// NowToMinute 返回当前北京时间，并截断到分钟级。
func NowToMinute() time.Time {
	return time.Now().In(ShanghaiLocation()).Truncate(time.Minute)
}

// NormalizeToMinute 把任意时间统一到北京时间分钟粒度。
func NormalizeToMinute(t time.Time) time.Time {
	return t.In(ShanghaiLocation()).Truncate(time.Minute)
}

// FormatMinute 把时间格式化为统一分钟级文本。
func FormatMinute(t time.Time) string {
	return NormalizeToMinute(t).Format(MinuteLayout)
}
