package agentsvc

import (
	"context"
	"sync"

	einoCallbacks "github.com/cloudwego/eino/callbacks"
	einoModel "github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
	templatecb "github.com/cloudwego/eino/utils/callbacks"
)

type requestTokenMeterCtxKey struct{}

// RequestTokenMeter 是“单次请求级”的 token 统计容器。
//
// 设计目标：
// 1. 聚合本次请求内所有模型调用 token（路由/图节点/流式主对话）；
// 2. 线程安全，允许在同一请求内被多个链路节点并发累加；
// 3. 最终由服务层一次性读取快照并写入持久化。
type RequestTokenMeter struct {
	mu sync.Mutex

	promptTokens     int
	completionTokens int
	totalTokens      int
}

// RequestTokenMeterSnapshot 是 RequestTokenMeter 的只读快照。
type RequestTokenMeterSnapshot struct {
	PromptTokens     int
	CompletionTokens int
	TotalTokens      int
}

var registerTokenMeterCallbackOnce sync.Once

// ensureTokenMeterCallbackRegistered 注册一次全局 ChatModel callback。
//
// 说明：
// 1. callback 只负责“采集并累加 token”，不做业务决策；
// 2. 仅当 ctx 里存在 RequestTokenMeter 时才会生效；
// 3. 采用 once，避免在测试/多次构造服务时重复注册。
func ensureTokenMeterCallbackRegistered() {
	registerTokenMeterCallbackOnce.Do(func() {
		handler := templatecb.NewHandlerHelper().
			ChatModel(&templatecb.ModelCallbackHandler{
				OnEnd: func(ctx context.Context, _ *einoCallbacks.RunInfo, output *einoModel.CallbackOutput) context.Context {
					if output == nil || output.TokenUsage == nil {
						return ctx
					}
					addModelUsageIntoRequest(ctx, output.TokenUsage)
					return ctx
				},
			}).
			Handler()
		einoCallbacks.AppendGlobalHandlers(handler)
	})
}

// withRequestTokenMeter 创建并挂载“请求级 token 统计器”。
func withRequestTokenMeter(ctx context.Context) (context.Context, *RequestTokenMeter) {
	meter := &RequestTokenMeter{}
	return context.WithValue(ctx, requestTokenMeterCtxKey{}, meter), meter
}

// getRequestTokenMeter 读取请求上下文中的 token 统计器。
func getRequestTokenMeter(ctx context.Context) *RequestTokenMeter {
	if ctx == nil {
		return nil
	}
	meter, _ := ctx.Value(requestTokenMeterCtxKey{}).(*RequestTokenMeter)
	return meter
}

// addSchemaUsageIntoRequest 把 schema usage 累加到请求级统计器。
func addSchemaUsageIntoRequest(ctx context.Context, usage *schema.TokenUsage) {
	if usage == nil {
		return
	}
	addTokenUsageValues(ctx, usage.PromptTokens, usage.CompletionTokens, normalizeUsageTotal(usage.TotalTokens, usage.PromptTokens, usage.CompletionTokens))
}

// addModelUsageIntoRequest 把 Eino model callback usage 累加到请求级统计器。
func addModelUsageIntoRequest(ctx context.Context, usage *einoModel.TokenUsage) {
	if usage == nil {
		return
	}
	addTokenUsageValues(ctx, usage.PromptTokens, usage.CompletionTokens, normalizeUsageTotal(usage.TotalTokens, usage.PromptTokens, usage.CompletionTokens))
}

// addTokenUsageValues 统一累加 token 数值。
func addTokenUsageValues(ctx context.Context, promptTokens, completionTokens, totalTokens int) {
	meter := getRequestTokenMeter(ctx)
	if meter == nil {
		return
	}

	if promptTokens < 0 {
		promptTokens = 0
	}
	if completionTokens < 0 {
		completionTokens = 0
	}
	if totalTokens < 0 {
		totalTokens = 0
	}

	meter.mu.Lock()
	defer meter.mu.Unlock()
	meter.promptTokens += promptTokens
	meter.completionTokens += completionTokens
	meter.totalTokens += totalTokens
}

// snapshotRequestTokenMeter 获取请求级 token 统计快照。
func snapshotRequestTokenMeter(ctx context.Context) RequestTokenMeterSnapshot {
	meter := getRequestTokenMeter(ctx)
	if meter == nil {
		return RequestTokenMeterSnapshot{}
	}
	meter.mu.Lock()
	defer meter.mu.Unlock()
	return RequestTokenMeterSnapshot{
		PromptTokens:     meter.promptTokens,
		CompletionTokens: meter.completionTokens,
		TotalTokens:      meter.totalTokens,
	}
}

// normalizeUsageTotal 统一 total token 口径。
//
// 规则：
// 1. 模型返回 total>0 时优先使用 total；
// 2. total 缺失时使用 prompt+completion 回退。
func normalizeUsageTotal(totalTokens, promptTokens, completionTokens int) int {
	if totalTokens > 0 {
		return totalTokens
	}
	sum := promptTokens + completionTokens
	if sum < 0 {
		return 0
	}
	return sum
}
