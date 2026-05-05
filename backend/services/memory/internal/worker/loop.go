package worker

import (
	"context"
	"log"
	"time"
)

// RunPollingLoop 持续轮询 memory_jobs，把异步 worker 真正跑起来。
//
// 职责边界：
// 1. 这里只负责“循环 + 轮询频率 + 批量触发”；
// 2. 不负责抽取逻辑，也不负责落库逻辑；
// 3. 任意一次 RunOnce 报错时只打日志并继续下一轮，避免整个后台循环退出。
func RunPollingLoop(ctx context.Context, runner *Runner, pollEvery time.Duration, claimBatch int) {
	if runner == nil {
		return
	}
	if runner.logger == nil {
		runner.logger = log.Default()
	}
	if pollEvery <= 0 {
		pollEvery = 2 * time.Second
	}
	if claimBatch <= 0 {
		claimBatch = 1
	}

	runBatch := func() {
		for i := 0; i < claimBatch; i++ {
			result, err := runner.RunOnce(ctx)
			if err != nil {
				runner.logger.Printf("memory worker loop run once failed: %v", err)
				return
			}
			if result == nil || !result.Claimed {
				return
			}
		}
	}

	runBatch()

	ticker := time.NewTicker(pollEvery)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			runner.logger.Printf("memory worker loop stopped: %v", ctx.Err())
			return
		case <-ticker.C:
			runBatch()
		}
	}
}
