package sv

import (
	"context"
	"log"
	"time"
)

// StartRetryLoop 启动 notification_records 重试扫描器。
//
// 说明：
// 1. 只在 worker/all 或独立 notification 进程启动；API / RPC 入口不主动扫重试；
// 2. provider 失败后的重试由本循环负责，避免通用 outbox 被外部服务慢失败拖住；
// 3. 每轮失败只写日志，下一轮继续扫描。
func (s *Service) StartRetryLoop(ctx context.Context, every time.Duration, limit int) {
	if s == nil {
		return
	}
	if every <= 0 {
		every = time.Minute
	}
	if limit <= 0 {
		limit = 50
	}
	go func() {
		ticker := time.NewTicker(every)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				result, err := s.RetryFeishuNotifications(ctx, time.Now(), limit)
				if err != nil {
					log.Printf("飞书通知重试扫描失败: err=%v", err)
					continue
				}
				if result.Scanned > 0 {
					log.Printf("飞书通知重试扫描完成: scanned=%d sent=%d failed=%d dead=%d skipped=%d", result.Scanned, result.Sent, result.Failed, result.Dead, result.Skipped)
				}
			}
		}
	}()
}
