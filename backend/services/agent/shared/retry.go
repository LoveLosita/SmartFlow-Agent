package agentshared

import (
	"context"
	"time"
)

// RetryOptions 描述公共重试策略。
//
// 职责边界：
// 1. 这里只定义“是否重试、最多几次、间隔多久”；
// 2. 不关心具体业务是工具调用失败、模型 JSON 失败还是 DB 暂时不可用；
// 3. 真正的业务兜底文案仍应由上层 node 决定。
type RetryOptions struct {
	MaxAttempts int
	Interval    time.Duration
	ShouldRetry func(err error) bool
	OnRetry     func(attempt int, err error)
}

// Do 执行一个只返回 error 的重试任务。
//
// 执行规则：
// 1. 第一次执行也算一次 attempt；
// 2. 任意一次成功即立即返回；
// 3. 上下文取消、达到最大次数、或 ShouldRetry=false 时立即停止。
func Do(ctx context.Context, options RetryOptions, fn func(attempt int) error) error {
	_, err := DoValue[struct{}](ctx, options, func(attempt int) (struct{}, error) {
		return struct{}{}, fn(attempt)
	})
	return err
}

// DoValue 执行一个带返回值的通用重试任务。
//
// 设计说明：
// 1. 旧 agent 里后续很多地方都会出现“失败重试 2~3 次”的模式；
// 2. 这里先把循环骨架统一，避免每个 skill 自己写 for + sleep + ctx.Done；
// 3. 上层只需关心“本轮失败要不要继续”，而不是重复造轮子。
func DoValue[T any](ctx context.Context, options RetryOptions, fn func(attempt int) (T, error)) (T, error) {
	var zero T

	maxAttempts := options.MaxAttempts
	if maxAttempts <= 0 {
		maxAttempts = 1
	}

	for attempt := 1; attempt <= maxAttempts; attempt++ {
		if err := ctx.Err(); err != nil {
			return zero, err
		}

		value, err := fn(attempt)
		if err == nil {
			return value, nil
		}

		// 1. 到最后一次了，直接返回原错误，避免无意义等待。
		if attempt >= maxAttempts {
			return zero, err
		}
		// 2. 业务显式声明“不值得重试”时，立刻停止。
		if options.ShouldRetry != nil && !options.ShouldRetry(err) {
			return zero, err
		}
		// 3. 把重试钩子留给上层，用于打点或阶段提示。
		if options.OnRetry != nil {
			options.OnRetry(attempt, err)
		}
		// 4. 没有配置间隔则马上下一轮；配置了则等待，同时尊重 ctx 取消。
		if options.Interval <= 0 {
			continue
		}

		timer := time.NewTimer(options.Interval)
		select {
		case <-ctx.Done():
			timer.Stop()
			return zero, ctx.Err()
		case <-timer.C:
		}
	}

	return zero, nil
}
