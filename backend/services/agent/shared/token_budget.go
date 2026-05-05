package agentshared

import (
	"math"
	"strings"
	"unicode"

	"github.com/cloudwego/eino/schema"
)

const (
	// Worker 模型最大输入上下文（用户提供）
	WorkerMaxInputTokens = 224000
	// 给模型输出和协议开销预留的冗余 token
	ContextReserveTokens = 28000

	// 缓存未命中时，从数据库拉取的历史消息上限
	DefaultHistoryFetchLimit = 1200

	// Redis 会话窗口上下限与缓冲
	SessionWindowMin    = 32
	SessionWindowMax    = 4096
	SessionWindowBuffer = 2

	// ---- Execute Context Compaction 预算 ----
	// Execute 阶段 prompt 总 token 上限
	ExecuteTokenBudget = 80000
	// msg0 + msg3 固定开销 + 安全余量
	ExecuteReserveTokens = 8000
	StageTokenBudget     = ExecuteTokenBudget
	StageReserveTokens   = ExecuteReserveTokens
)

// MaxContextTokensByModel 返回指定模型的最大上下文 token。
func MaxContextTokensByModel(modelName string) int {
	switch strings.ToLower(strings.TrimSpace(modelName)) {
	case "worker", "strategist":
		return WorkerMaxInputTokens
	default:
		return WorkerMaxInputTokens
	}
}

// HistoryFetchLimitByModel 返回缓存未命中时的历史拉取条数。
func HistoryFetchLimitByModel(_ string) int {
	return DefaultHistoryFetchLimit
}

// HistoryTokenBudgetByModel 计算“历史上下文”可使用的 token 预算。
func HistoryTokenBudgetByModel(modelName, systemPrompt, userInput string) int {
	maxTokens := MaxContextTokensByModel(modelName)
	baseTokens := EstimateTextTokens(systemPrompt) + EstimateTextTokens(userInput) + 64
	budget := maxTokens - ContextReserveTokens - baseTokens
	if budget < 0 {
		return 0
	}
	return budget
}

// EstimateTextTokens 粗略估算文本 token：
// - CJK 字符约 1:1
// - ASCII 字符约 4:1
// - 其他字符约 2:1
func EstimateTextTokens(text string) int {
	if strings.TrimSpace(text) == "" {
		return 0
	}

	var cjkCount, asciiCount, otherCount int
	for _, r := range text {
		switch {
		case unicode.IsSpace(r):
			continue
		case r <= unicode.MaxASCII:
			asciiCount++
		case isCJK(r):
			cjkCount++
		default:
			otherCount++
		}
	}

	tokens := cjkCount + int(math.Ceil(float64(asciiCount)/4.0)) + int(math.Ceil(float64(otherCount)/2.0))
	if tokens <= 0 {
		return 1
	}
	return tokens
}

// EstimateMessageTokens 估算单条消息 token（包含固定协议开销）。
func EstimateMessageTokens(msg *schema.Message) int {
	if msg == nil {
		return 0
	}
	const messageOverhead = 6
	return messageOverhead + EstimateTextTokens(msg.Content) + EstimateTextTokens(msg.ReasoningContent)
}

// EstimateHistoryTokens 估算历史消息总 token。
func EstimateHistoryTokens(history []*schema.Message) int {
	total := 0
	for _, msg := range history {
		total += EstimateMessageTokens(msg)
	}
	return total
}

// TrimHistoryByTokenBudget 从最旧消息开始裁剪，直到历史 token 不超过预算。
// 返回值：裁剪后历史、裁剪前 token、裁剪后 token、裁掉条数。
func TrimHistoryByTokenBudget(history []*schema.Message, historyBudget int) ([]*schema.Message, int, int, int) {
	if len(history) == 0 {
		return history, 0, 0, 0
	}

	totalBefore := EstimateHistoryTokens(history)
	if historyBudget <= 0 {
		return []*schema.Message{}, totalBefore, 0, len(history)
	}
	if totalBefore <= historyBudget {
		return history, totalBefore, totalBefore, 0
	}

	tokenPerMsg := make([]int, len(history))
	total := 0
	for i, msg := range history {
		t := EstimateMessageTokens(msg)
		tokenPerMsg[i] = t
		total += t
	}

	drop := 0
	for total > historyBudget && drop < len(history) {
		total -= tokenPerMsg[drop]
		drop++
	}

	return history[drop:], totalBefore, total, drop
}

// CalcSessionWindowSize 根据裁剪后消息条数计算 Redis 队列窗口大小。
func CalcSessionWindowSize(trimmedHistoryLen int) int {
	size := trimmedHistoryLen + SessionWindowBuffer
	if size < SessionWindowMin {
		size = SessionWindowMin
	}
	if size > SessionWindowMax {
		size = SessionWindowMax
	}
	return size
}

func isCJK(r rune) bool {
	return unicode.Is(unicode.Han, r) || unicode.Is(unicode.Hiragana, r) || unicode.Is(unicode.Katakana, r) || unicode.Is(unicode.Hangul, r)
}

// StageTokenBreakdown 记录四条阶段消息的 token 分布。
type StageTokenBreakdown struct {
	Msg0   int `json:"msg0"`
	Msg1   int `json:"msg1"`
	Msg2   int `json:"msg2"`
	Msg3   int `json:"msg3"`
	Total  int `json:"total"`
	Budget int `json:"budget"`
}

// ExecuteTokenBreakdown 保留为历史兼容别名，避免旧调用点改动。
type ExecuteTokenBreakdown = StageTokenBreakdown

// EstimateStageMessagesTokens 估算四条阶段消息的 token 分布。
func EstimateStageMessagesTokens(msg0, msg1, msg2, msg3 string) StageTokenBreakdown {
	b := StageTokenBreakdown{
		Msg0:   EstimateTextTokens(msg0),
		Msg1:   EstimateTextTokens(msg1),
		Msg2:   EstimateTextTokens(msg2),
		Msg3:   EstimateTextTokens(msg3),
		Budget: StageTokenBudget,
	}
	b.Total = b.Msg0 + b.Msg1 + b.Msg2 + b.Msg3
	return b
}

// CheckStageTokenBudget 检查是否超出阶段预算，并给出需要压缩的消息标记。
//
// 1. 先计算四条消息的 token 分布，便于后续日志和统计。
// 2. 如果总量没有超预算，直接返回。
// 3. 如果超预算，则按 msg1 / msg2 的相对占比判断是否需要分别压缩。
func CheckStageTokenBudget(msg0, msg1, msg2, msg3 string) (breakdown StageTokenBreakdown, overBudget bool, needCompactMsg1 bool, needCompactMsg2 bool) {
	breakdown = EstimateStageMessagesTokens(msg0, msg1, msg2, msg3)
	overBudget = breakdown.Total > StageTokenBudget
	if !overBudget {
		return
	}
	// msg1 过大时，优先压缩历史对话。
	available := StageTokenBudget - StageReserveTokens
	needCompactMsg1 = breakdown.Msg1 > available/2
	// 若压缩 msg1 后仍然超限，再压缩执行记录区。
	needCompactMsg2 = (breakdown.Total - breakdown.Msg1 + available/4) > StageTokenBudget
	return
}

// EstimateExecuteMessagesTokens 保留旧名称，内部复用阶段预算实现。
func EstimateExecuteMessagesTokens(msg0, msg1, msg2, msg3 string) StageTokenBreakdown {
	return EstimateStageMessagesTokens(msg0, msg1, msg2, msg3)
}

// CheckExecuteTokenBudget 保留旧名称，内部复用阶段预算实现。
func CheckExecuteTokenBudget(msg0, msg1, msg2, msg3 string) (breakdown StageTokenBreakdown, overBudget bool, needCompactMsg1 bool, needCompactMsg2 bool) {
	return CheckStageTokenBudget(msg0, msg1, msg2, msg3)
}
