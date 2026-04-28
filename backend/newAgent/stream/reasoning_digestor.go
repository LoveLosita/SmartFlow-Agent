package newagentstream

import (
	"context"
	"errors"
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"
)

const (
	defaultReasoningDigestMinNewRunes  = 120
	defaultReasoningDigestMinNewTokens = 80
	defaultReasoningDigestMinInterval  = 3 * time.Second
)

// ReasoningSummaryFunc 负责真正调用摘要模型。
//
// 职责边界：
// 1. 该函数只负责“把输入整理成一份摘要结果”，不负责调度、节流、正文闸门和结果丢弃；
// 2. 返回值里的 short/detail 由模型或适配层填写；
// 3. summary_seq / final / duration_seconds 由 ReasoningDigestor 统一补齐，避免上层重复维护运行态字段。
type ReasoningSummaryFunc func(ctx context.Context, input ReasoningSummaryInput) (StreamThinkingSummaryExtra, error)

// ReasoningSummarySink 负责消费一条已经通过闸门校验的摘要结果。
//
// 职责边界：
// 1. 常见用法是把结果交给 ChunkEmitter.EmitThinkingSummary；
// 2. 该回调不参与单飞、重试、水位线判断；
// 3. 回调为 nil 时，Digestor 仍会维护 LatestSummary，方便调用方按需主动拉取。
type ReasoningSummarySink func(summary StreamThinkingSummaryExtra)

// ReasoningSummaryInput 是注入给摘要模型调用方的统一输入。
//
// 职责边界：
// 1. FullReasoning 提供完整 reasoning 缓冲区，适合做“全量重摘要”；
// 2. DeltaReasoning + PreviousSummary 提供增量上下文，适合做“旧摘要续写”；
// 3. CandidateSeq / Final / DurationSeconds 仅表达调度层意图，不要求模型原样回填。
type ReasoningSummaryInput struct {
	FullReasoning   string                      `json:"full_reasoning,omitempty"`
	DeltaReasoning  string                      `json:"delta_reasoning,omitempty"`
	PreviousSummary *StreamThinkingSummaryExtra `json:"previous_summary,omitempty"`
	CandidateSeq    int                         `json:"candidate_seq,omitempty"`
	Final           bool                        `json:"final,omitempty"`
	DurationSeconds float64                     `json:"duration_seconds,omitempty"`
}

// ReasoningDigestorOptions 描述 reasoning 摘要器的调度参数。
type ReasoningDigestorOptions struct {
	SummaryFunc    ReasoningSummaryFunc
	SummarySink    ReasoningSummarySink
	BaseContext    context.Context
	MinNewRunes    int
	MinNewTokens   int
	MinInterval    time.Duration
	SummaryTimeout time.Duration
	Now            func() time.Time
}

// ReasoningDigestor 负责把流式 reasoning 文本整理成“低频摘要事件”。
//
// 职责边界：
// 1. 只负责缓冲、单飞、水位线、正文闸门、Flush/Close，不直接依赖 AgentService；
// 2. 只通过 SummaryFunc / SummarySink 两个函数注入模型调用与结果消费，不在这里选模型；
// 3. 一旦正文开始或显式关闸，后续摘要结果即使返回成功也必须丢弃，避免前端和持久化出现越界数据。
type ReasoningDigestor struct {
	summaryFunc    ReasoningSummaryFunc
	summarySink    ReasoningSummarySink
	baseContext    context.Context
	minNewRunes    int
	minNewTokens   int
	minInterval    time.Duration
	summaryTimeout time.Duration
	now            func() time.Time

	mu             sync.Mutex
	cond           *sync.Cond
	buffer         strings.Builder
	deltaBuffer    strings.Builder
	startedAt      time.Time
	lastRequestAt  time.Time
	pendingRunes   int
	pendingTokens  int
	summarySeq     int
	latestSummary  *StreamThinkingSummaryExtra
	finalEmitted   bool
	inFlight       bool
	gateClosed     bool
	contentStarted bool
	closed         bool
	timer          *time.Timer
	timerArmed     bool
	currentCancel  context.CancelFunc
}

type reasoningDigestCall struct {
	ctx   context.Context
	stop  context.CancelFunc
	input ReasoningSummaryInput
	final bool
}

// NewReasoningDigestor 创建一个只关注“流式思考摘要调度”的核心对象。
//
// 步骤说明：
// 1. 先校验 SummaryFunc；它是唯一必填项，因为 Digestor 不在本文件里选择模型；
// 2. 再补齐默认水位线和最小时间间隔，让调用方即使只传核心依赖也能启动；
// 3. 最后只初始化并发控制原语，不在构造阶段启动常驻主循环，避免引入额外 goroutine 生命周期负担。
func NewReasoningDigestor(options ReasoningDigestorOptions) (*ReasoningDigestor, error) {
	if options.SummaryFunc == nil {
		return nil, errors.New("reasoning digestor: SummaryFunc 不能为空")
	}

	if options.MinNewRunes < 0 {
		options.MinNewRunes = 0
	}
	if options.MinNewTokens < 0 {
		options.MinNewTokens = 0
	}
	if options.MinNewRunes == 0 && options.MinNewTokens == 0 {
		options.MinNewRunes = defaultReasoningDigestMinNewRunes
		options.MinNewTokens = defaultReasoningDigestMinNewTokens
	}
	if options.MinInterval <= 0 {
		options.MinInterval = defaultReasoningDigestMinInterval
	}
	if options.BaseContext == nil {
		options.BaseContext = context.Background()
	}
	if options.Now == nil {
		options.Now = time.Now
	}

	digestor := &ReasoningDigestor{
		summaryFunc:    options.SummaryFunc,
		summarySink:    options.SummarySink,
		baseContext:    options.BaseContext,
		minNewRunes:    options.MinNewRunes,
		minNewTokens:   options.MinNewTokens,
		minInterval:    options.MinInterval,
		summaryTimeout: options.SummaryTimeout,
		now:            options.Now,
	}
	digestor.cond = sync.NewCond(&digestor.mu)
	return digestor, nil
}

// Append 追加一段 reasoning chunk，并按水位线决定是否后台触发摘要。
//
// 步骤说明：
// 1. 先把原始 reasoning 文本写入 full buffer，保证 Flush/Close 可以拿到全量上下文；
// 2. 再把本轮新增文本记入 deltaBuffer 与 rune/token 水位线，用于“最小新增量”判断；
// 3. 若正文闸门已关闭，则只保留缓冲快照，不再调度摘要；
// 4. 若当前已有摘要请求在飞，则只更新 dirty/latest，不排队第二个请求，等单飞请求返回后再决定是否补一次。
func (d *ReasoningDigestor) Append(reasoning string) {
	if d == nil || reasoning == "" {
		return
	}

	var call reasoningDigestCall
	var shouldStart bool

	d.mu.Lock()
	if d.closed {
		d.mu.Unlock()
		return
	}

	if d.startedAt.IsZero() {
		d.startedAt = d.now()
	}
	d.buffer.WriteString(reasoning)

	if d.gateClosed || d.contentStarted {
		d.mu.Unlock()
		return
	}

	d.deltaBuffer.WriteString(reasoning)
	d.pendingRunes += utf8.RuneCountInString(reasoning)
	d.pendingTokens += estimateReasoningTokens(reasoning)
	d.finalEmitted = false

	call, shouldStart = d.prepareSummaryLocked(d.baseContext, false, false)
	d.mu.Unlock()

	if shouldStart {
		go d.runSummary(call)
	}
}

// MarkContentStarted 标记“正文已经开始输出”。
//
// 职责边界：
// 1. 该方法会直接关闭摘要闸门；
// 2. 它不回收旧摘要结果，但会丢弃后续任何尚未完成的摘要调用；
// 3. 调用后即使继续 Append reasoning，也只保留缓冲，不再触发新摘要。
func (d *ReasoningDigestor) MarkContentStarted() {
	d.closeGate(true)
}

// CloseGate 显式关闭摘要闸门，但不额外声明正文已经开始。
func (d *ReasoningDigestor) CloseGate() {
	d.closeGate(false)
}

// Flush 在正文尚未开始时尝试补发最后一次摘要。
//
// 步骤说明：
// 1. 先等待当前单飞请求结束，避免 Flush 与后台自动摘要并发跑两次；
// 2. 若正文已经开始或闸门已关，则直接返回，不再补摘要；
// 3. 若此前已经发过 final 且没有新增 reasoning，则跳过，避免重复 final 事件；
// 4. 其余场景会强制走一次摘要，即使新增量还没达到自动触发水位线。
func (d *ReasoningDigestor) Flush(ctx context.Context) error {
	if d == nil {
		return nil
	}

	call, shouldStart := d.prepareFlushCall(ctx)
	if !shouldStart {
		return nil
	}
	return d.runSummary(call)
}

// Close 结束摘要器生命周期。
//
// 步骤说明：
// 1. 若正文还未开始，先尝试 Flush 一次 final 摘要；
// 2. 再关闭闸门、停止等待中的定时器，并取消正在进行的摘要调用；
// 3. 最后等待单飞调用完全退出，避免遗留后台 goroutine 持续写结果。
func (d *ReasoningDigestor) Close(ctx context.Context) error {
	if d == nil {
		return nil
	}

	if err := d.Flush(ctx); err != nil {
		return err
	}

	d.mu.Lock()
	if d.closed {
		d.mu.Unlock()
		return nil
	}

	d.closed = true
	d.gateClosed = true
	d.stopTimerLocked()
	if d.currentCancel != nil {
		d.currentCancel()
	}
	for d.inFlight {
		d.cond.Wait()
	}
	d.mu.Unlock()
	return nil
}

// LatestSummary 返回最近一次通过闸门校验并成功发布的摘要。
func (d *ReasoningDigestor) LatestSummary() (StreamThinkingSummaryExtra, bool) {
	if d == nil {
		return StreamThinkingSummaryExtra{}, false
	}

	d.mu.Lock()
	defer d.mu.Unlock()

	if d.latestSummary == nil {
		return StreamThinkingSummaryExtra{}, false
	}
	return *cloneThinkingSummaryExtra(d.latestSummary), true
}

func (d *ReasoningDigestor) closeGate(markContentStarted bool) {
	if d == nil {
		return
	}

	d.mu.Lock()
	if markContentStarted {
		d.contentStarted = true
	}
	d.gateClosed = true
	d.pendingRunes = 0
	d.pendingTokens = 0
	d.deltaBuffer.Reset()
	d.stopTimerLocked()
	if d.currentCancel != nil {
		d.currentCancel()
	}
	d.mu.Unlock()
}

func (d *ReasoningDigestor) prepareFlushCall(ctx context.Context) (reasoningDigestCall, bool) {
	d.mu.Lock()
	defer d.mu.Unlock()

	if d.closed || d.gateClosed || d.contentStarted {
		return reasoningDigestCall{}, false
	}

	d.stopTimerLocked()
	for d.inFlight {
		d.cond.Wait()
		if d.closed || d.gateClosed || d.contentStarted {
			return reasoningDigestCall{}, false
		}
	}

	if strings.TrimSpace(d.buffer.String()) == "" {
		return reasoningDigestCall{}, false
	}
	if d.finalEmitted && d.pendingRunes == 0 && d.pendingTokens == 0 {
		return reasoningDigestCall{}, false
	}
	return d.prepareSummaryLocked(ctx, true, true)
}

func (d *ReasoningDigestor) prepareSummaryLocked(parent context.Context, force bool, final bool) (reasoningDigestCall, bool) {
	if d.closed || d.gateClosed || d.contentStarted || d.inFlight {
		return reasoningDigestCall{}, false
	}

	fullReasoning := d.buffer.String()
	if strings.TrimSpace(fullReasoning) == "" {
		return reasoningDigestCall{}, false
	}

	// 1. 自动摘要必须同时满足“新增量水位线 + 最小时间间隔”。
	// 2. 若新增量不足，则直接等待后续 Append，不做空转请求。
	// 3. 若时间间隔未到，则只挂一个定时器做兜底唤醒，避免排队多个请求。
	if !force {
		if !d.reachedWatermarkLocked() {
			return reasoningDigestCall{}, false
		}
		wait := d.nextAllowedIntervalLocked()
		if wait > 0 {
			d.armTimerLocked(wait)
			return reasoningDigestCall{}, false
		}
	}

	callCtx, stop := d.newCallContext(parent)
	call := reasoningDigestCall{
		ctx:  callCtx,
		stop: stop,
		input: ReasoningSummaryInput{
			FullReasoning:   strings.Clone(fullReasoning),
			DeltaReasoning:  strings.Clone(d.deltaBuffer.String()),
			PreviousSummary: cloneThinkingSummaryExtra(d.latestSummary),
			CandidateSeq:    d.summarySeq + 1,
			Final:           final,
			DurationSeconds: d.durationSecondsLocked(),
		},
		final: final,
	}

	d.stopTimerLocked()
	d.inFlight = true
	d.lastRequestAt = d.now()
	d.pendingRunes = 0
	d.pendingTokens = 0
	d.deltaBuffer.Reset()
	d.currentCancel = stop

	return call, true
}

func (d *ReasoningDigestor) runSummary(call reasoningDigestCall) error {
	if call.stop == nil {
		return nil
	}
	defer call.stop()

	summary, err := d.summaryFunc(call.ctx, call.input)
	if err != nil {
		// 1. 摘要失败时不把错误扩散回主流式链路，避免 reasoning 展示被摘要能力反向拖垮。
		// 2. 若失败期间又追加了新 reasoning，则仍按单飞规则尝试补下一次；否则等待后续 Append/Flush 兜底。
		_, _, nextCall, shouldStart := d.finishSummary(call.final, nil)
		if shouldStart {
			go d.runSummary(nextCall)
		}
		return err
	}

	normalized := normalizeThinkingSummary(summary, call.input.Final, call.input.DurationSeconds)
	emittedSummary, sink, nextCall, shouldStart := d.finishSummary(call.final, &normalized)
	if emittedSummary != nil && sink != nil {
		sink(*emittedSummary)
	}
	if shouldStart {
		go d.runSummary(nextCall)
	}
	return nil
}

func (d *ReasoningDigestor) finishSummary(final bool, summary *StreamThinkingSummaryExtra) (*StreamThinkingSummaryExtra, ReasoningSummarySink, reasoningDigestCall, bool) {
	d.mu.Lock()
	defer d.mu.Unlock()

	d.inFlight = false
	d.currentCancel = nil
	d.cond.Broadcast()

	var emittedSummary *StreamThinkingSummaryExtra
	var sink ReasoningSummarySink

	// 1. 先判断正文闸门；正文一旦开始，所有晚到结果都必须丢弃。
	// 2. 再补齐 summary_seq/final/duration，并缓存 LatestSummary 供上层读取。
	// 3. 若当前请求期间又积累了新 reasoning，则只启动下一次单飞摘要，不排队多次。
	if summary != nil && !d.closed && !d.gateClosed && !d.contentStarted {
		normalized := *summary
		d.summarySeq++
		normalized.SummarySeq = d.summarySeq
		normalized.Final = final
		if normalized.DurationSeconds <= 0 {
			normalized.DurationSeconds = d.durationSecondsLocked()
		}
		d.latestSummary = cloneThinkingSummaryExtra(&normalized)
		d.finalEmitted = final
		emittedSummary = cloneThinkingSummaryExtra(&normalized)
		sink = d.summarySink
	}

	if d.closed || d.gateClosed || d.contentStarted || final {
		return emittedSummary, sink, reasoningDigestCall{}, false
	}

	nextCall, shouldStart := d.prepareSummaryLocked(d.baseContext, false, false)
	return emittedSummary, sink, nextCall, shouldStart
}

func (d *ReasoningDigestor) reachedWatermarkLocked() bool {
	return reachedReasoningWatermark(d.pendingRunes, d.pendingTokens, d.minNewRunes, d.minNewTokens)
}

func (d *ReasoningDigestor) nextAllowedIntervalLocked() time.Duration {
	if d.lastRequestAt.IsZero() {
		return 0
	}
	wait := d.minInterval - d.now().Sub(d.lastRequestAt)
	if wait < 0 {
		return 0
	}
	return wait
}

func (d *ReasoningDigestor) armTimerLocked(wait time.Duration) {
	if wait <= 0 || d.closed || d.gateClosed || d.contentStarted {
		return
	}
	if d.timer == nil {
		d.timer = time.AfterFunc(wait, d.onTimer)
		d.timerArmed = true
		return
	}
	if d.timerArmed {
		d.timer.Reset(wait)
		return
	}
	d.timer.Reset(wait)
	d.timerArmed = true
}

func (d *ReasoningDigestor) stopTimerLocked() {
	if d.timer == nil {
		return
	}
	if d.timer.Stop() {
		d.timerArmed = false
		return
	}
	d.timerArmed = false
}

func (d *ReasoningDigestor) onTimer() {
	if d == nil {
		return
	}

	var call reasoningDigestCall
	var shouldStart bool

	d.mu.Lock()
	d.timerArmed = false
	call, shouldStart = d.prepareSummaryLocked(d.baseContext, false, false)
	d.mu.Unlock()

	if shouldStart {
		go d.runSummary(call)
	}
}

func (d *ReasoningDigestor) newCallContext(parent context.Context) (context.Context, context.CancelFunc) {
	if parent == nil {
		parent = d.baseContext
	}
	if parent == nil {
		parent = context.Background()
	}

	baseCtx, baseCancel := context.WithCancel(parent)
	if d.summaryTimeout <= 0 {
		return baseCtx, baseCancel
	}

	timeoutCtx, timeoutCancel := context.WithTimeout(baseCtx, d.summaryTimeout)
	return timeoutCtx, func() {
		timeoutCancel()
		baseCancel()
	}
}

func (d *ReasoningDigestor) durationSecondsLocked() float64 {
	if d.startedAt.IsZero() {
		return 0
	}
	duration := d.now().Sub(d.startedAt)
	if duration <= 0 {
		return 0
	}
	return float64(duration.Milliseconds()) / 1000
}

func reachedReasoningWatermark(pendingRunes, pendingTokens, minRunes, minTokens int) bool {
	if minRunes > 0 && pendingRunes >= minRunes {
		return true
	}
	if minTokens > 0 && pendingTokens >= minTokens {
		return true
	}
	return false
}

func normalizeThinkingSummary(summary StreamThinkingSummaryExtra, final bool, durationSeconds float64) StreamThinkingSummaryExtra {
	summary.ShortSummary = strings.TrimSpace(summary.ShortSummary)
	summary.DetailSummary = strings.TrimSpace(summary.DetailSummary)

	// 1. 短摘要只是实时展示兜底，允许从长摘要压一个默认值。
	// 2. 反过来不能把短摘要补成 detail_summary，否则会绕过“短摘要不持久化”的产品语义。
	// 3. 若模型没有给 detail_summary，timeline 层会跳过持久化，仅保留本次 SSE 展示。
	if summary.ShortSummary == "" {
		summary.ShortSummary = summary.DetailSummary
	}
	summary.Final = final
	if summary.DurationSeconds <= 0 {
		summary.DurationSeconds = durationSeconds
	}
	return summary
}

func cloneThinkingSummaryExtra(src *StreamThinkingSummaryExtra) *StreamThinkingSummaryExtra {
	if src == nil {
		return nil
	}
	clone := *src
	return &clone
}

func estimateReasoningTokens(text string) int {
	text = strings.TrimSpace(text)
	if text == "" {
		return 0
	}

	asciiRunes := 0
	totalTokens := 0
	for _, r := range text {
		switch {
		case unicode.IsSpace(r):
			if asciiRunes > 0 {
				totalTokens += compactASCIITokens(asciiRunes)
				asciiRunes = 0
			}
		case r <= unicode.MaxASCII && (unicode.IsLetter(r) || unicode.IsDigit(r)):
			asciiRunes++
		default:
			if asciiRunes > 0 {
				totalTokens += compactASCIITokens(asciiRunes)
				asciiRunes = 0
			}
			totalTokens++
		}
	}
	if asciiRunes > 0 {
		totalTokens += compactASCIITokens(asciiRunes)
	}
	return totalTokens
}

func compactASCIITokens(asciiRunes int) int {
	if asciiRunes <= 0 {
		return 0
	}
	return max(1, (asciiRunes+3)/4)
}
