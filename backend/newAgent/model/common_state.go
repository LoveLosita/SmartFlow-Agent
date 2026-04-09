package model

import (
	"strings"

	newagenttools "github.com/LoveLosita/smartflow/backend/newAgent/tools"
)

// Phase 表示 agent 主循环当前所处的大阶段。
type Phase string

const (
	PhasePlanning       Phase = "planning"
	PhaseWaitingConfirm Phase = "waiting_confirm"
	PhaseExecuting      Phase = "executing"
	PhaseDone           Phase = "done"
)

// FlowTerminalStatus 表示本轮流程最终是如何结束的。
//
// 说明：
// 1. completed 表示任务按预期完成，允许走正常交付与预览落盘；
// 2. aborted 表示业务语义上的主动终止，例如粗排异常、执行期明确中止；
// 3. exhausted 表示安全边界触发的被动停止，例如执行轮次耗尽。
type FlowTerminalStatus string

const (
	FlowTerminalStatusCompleted FlowTerminalStatus = "completed"
	FlowTerminalStatusAborted   FlowTerminalStatus = "aborted"
	FlowTerminalStatusExhausted FlowTerminalStatus = "exhausted"
)

// FlowTerminalOutcome 保存“流程为什么结束”的最终结果快照。
//
// 职责边界：
// 1. Stage 说明终止发生在哪个阶段，便于 graph/deliver/debug 统一收口；
// 2. Code 作为稳定机器码，便于后续前端或埋点按类型识别；
// 3. UserMessage 是最终给用户看的收口文案；
// 4. InternalReason 只用于日志与排查，不直接暴露给用户。
type FlowTerminalOutcome struct {
	Status         FlowTerminalStatus `json:"status"`
	Stage          string             `json:"stage,omitempty"`
	Code           string             `json:"code,omitempty"`
	UserMessage    string             `json:"user_message,omitempty"`
	InternalReason string             `json:"internal_reason,omitempty"`
}

// Normalize 统一清洗终止结果里的字符串字段。
func (o *FlowTerminalOutcome) Normalize() {
	if o == nil {
		return
	}
	o.Status = FlowTerminalStatus(strings.TrimSpace(string(o.Status)))
	o.Stage = strings.TrimSpace(o.Stage)
	o.Code = strings.TrimSpace(o.Code)
	o.UserMessage = strings.TrimSpace(o.UserMessage)
	o.InternalReason = strings.TrimSpace(o.InternalReason)
}

const DefaultMaxRounds = 30

// CommonState 承载可持久化的主流程状态。
//
// 职责边界：
// 1. 负责记录"当前处于哪个阶段、当前计划是什么、执行到了第几步、已经消耗了多少轮"；
// 2. 负责提供最小必要的安全访问方法，避免 graph/node/prompt 层到处手写切片越界判断；
// 3. 不负责承载对话历史、tool schema、pinned context 这类模型输入材料，它们仍然属于 ConversationContext。
type CommonState struct {
	// 身份信息
	TraceID        string `json:"trace_id"`
	UserID         int    `json:"user_id"`
	ConversationID string `json:"conversation_id"`

	// 流程阶段
	Phase Phase `json:"phase"`

	// 计划状态
	// 1. 这里直接使用结构化的 PlanStep，避免 planning -> execute 之间丢失 done_when。
	// 2. CurrentStep 表示"当前 plan 步骤下标"，不是 execute 内部 ReAct 的思考轮次。
	PlanSteps   []PlanStep `json:"plan_steps"`
	CurrentStep int        `json:"current_step"`

	// 安全边界
	MaxRounds int `json:"max_rounds"`
	RoundUsed int `json:"round_used"`

	// 连续修正计数：LLM 连续输出不合法决策的次数，超过阈值后强制终止避免死循环。
	ConsecutiveCorrections int `json:"consecutive_corrections"`

	// TaskClassIDs 本次排课请求涉及的任务类 ID 列表，由前端 extra.task_class_ids 传入。
	// Plan 节点据此判断是否需要粗排；跨轮次持久化，不会因会话恢复而丢失。
	TaskClassIDs []int `json:"task_class_ids,omitempty"`
	// TaskClasses 本次排课涉及的任务类约束元数据（含日期、策略、时段预算等），
	// 在 Service 层从 DB 加载并注入，供 Plan prompt 直接消费，避免 LLM 因信息不足而追问用户。
	TaskClasses []newagenttools.TaskClassMeta `json:"task_classes,omitempty"`

	// NeedsRoughBuild 由 Plan 节点在 plan_done 时写入，标记 Confirm 后是否需要走粗排节点。
	// 粗排节点执行完毕后会将此字段重置为 false。
	NeedsRoughBuild bool `json:"needs_rough_build,omitempty"`
	// NeedsRefineAfterRoughBuild 表示“粗排完成后是否需要立即进入微调”。
	//
	// 说明：
	// 1. 该标记主要用于 chat->execute 的直执行链路；
	// 2. true 表示用户已明确提出优化偏好，粗排后继续进 execute 微调；
	// 3. false 表示用户仅要求完成排入，粗排成功后可直接收口，等待后续再优化。
	NeedsRefineAfterRoughBuild bool `json:"needs_refine_after_rough_build,omitempty"`
	// AllowReorder 表示本轮是否允许打乱 suggested 任务的相对顺序。
	// 默认 false，只有用户明确说明“可以打乱顺序/顺序不重要”才会为 true。
	AllowReorder bool `json:"allow_reorder,omitempty"`
	// SuggestedOrderBaseline 保存“本轮 execute 启动前”的 suggested 任务相对顺序基线。
	// OrderGuard 节点会基于该基线判断微调是否破坏顺序约束。
	SuggestedOrderBaseline []int `json:"suggested_order_baseline,omitempty"`

	// TerminalOutcome 保存“本轮流程最终如何结束”的统一收口结果。
	// 第二轮开始，rough_build / execute / deliver 都应围绕这份快照判断收口语义。
	TerminalOutcome *FlowTerminalOutcome `json:"terminal_outcome,omitempty"`
}

func NewCommonState(traceID string, userID int, conversationID string) *CommonState {
	return &CommonState{
		TraceID:        traceID,
		UserID:         userID,
		ConversationID: conversationID,
		Phase:          PhasePlanning,
		MaxRounds:      DefaultMaxRounds,
	}
}

// NextRound 消耗一轮预算，并返回当前是否仍在允许范围内。
func (s *CommonState) NextRound() bool {
	s.RoundUsed++
	return s.RoundUsed <= s.MaxRounds
}

// Exhausted 判断是否已经耗尽轮次预算。
func (s *CommonState) Exhausted() bool {
	return s.RoundUsed >= s.MaxRounds
}

// FinishPlan 在 planning 完成后固化完整计划，并推进到待确认阶段。
//
// 步骤说明：
// 1. 直接保存完整的 []PlanStep，避免 execute 阶段再去依赖 pinned context 回捞完成判定；
// 2. 统一把 CurrentStep 重置到第 0 步，保证后续 confirm/execute 都从计划开头进入；
// 3. 这里只负责状态切换，不负责刷新 ConversationContext 中的置顶 plan 文本。
func (s *CommonState) FinishPlan(steps []PlanStep) {
	s.PlanSteps = steps
	s.CurrentStep = 0
	s.Phase = PhaseWaitingConfirm
	s.NeedsRefineAfterRoughBuild = false
	s.SuggestedOrderBaseline = nil
	s.ClearTerminalOutcome()
}

// ConfirmPlan 表示用户已确认计划，流程进入执行阶段。
func (s *CommonState) ConfirmPlan() {
	s.Phase = PhaseExecuting
	s.NeedsRefineAfterRoughBuild = false
	s.SuggestedOrderBaseline = nil
	s.ClearTerminalOutcome()
}

// StartDirectExecute 进入无 plan 的直接执行（ReAct）模式。
// Chat 节点路由到 execute 时必须调用此方法，而非直接赋值 Phase，
// 否则上一次任务残留的 PlanSteps 会被 HasPlan() 误判为仍有计划，
// 导致 Execute 节点用旧步骤跑 plan 模式而非 ReAct 模式。
func (s *CommonState) StartDirectExecute() {
	s.PlanSteps = nil
	s.CurrentStep = 0
	s.Phase = PhaseExecuting
	s.NeedsRoughBuild = false
	s.NeedsRefineAfterRoughBuild = false
	s.SuggestedOrderBaseline = nil
	s.ClearTerminalOutcome()
}

// RejectPlan 表示用户拒绝当前计划，清空计划并回退到 planning。
func (s *CommonState) RejectPlan() {
	s.PlanSteps = nil
	s.CurrentStep = 0
	s.Phase = PhasePlanning
	s.NeedsRefineAfterRoughBuild = false
	s.SuggestedOrderBaseline = nil
	s.ClearTerminalOutcome()
}

// ResetForNextRun 在“上一轮已经收口，且本轮准备开始新请求”时重置执行期临时状态。
//
// 职责边界：
// 1. 负责清理会污染新一轮执行的临时字段（轮次、修正计数、计划游标、粗排开关、顺序基线、终止结果）；
// 2. 不负责清理会话身份与跨轮共享数据（ConversationID/UserID/TaskClassIDs/TaskClasses/历史上下文/ScheduleState）；
// 3. 该方法是幂等操作：重复调用不会引入额外副作用，便于在“加载兜底 + chat 入口”双保险场景下复用。
func (s *CommonState) ResetForNextRun() {
	if s == nil {
		return
	}

	// 1. 先把阶段回收为 planning，确保新一轮从可路由的干净入口开始。
	// 2. 这样即使后续还有兜底重置判断，也不会因为仍处于 done 而重复触发。
	s.Phase = PhasePlanning

	// 3. 清理执行轮次与连续修正计数，避免上一轮预算/异常计数污染本轮。
	s.RoundUsed = 0
	s.ConsecutiveCorrections = 0

	// 4. 清理计划执行游标与粗排相关临时标记，确保新请求不会误沿用旧计划。
	s.PlanSteps = nil
	s.CurrentStep = 0
	s.NeedsRoughBuild = false
	s.NeedsRefineAfterRoughBuild = false

	// 5. 重置顺序约束临时态与终止结果，避免上一轮 completed/aborted/exhausted 语义串到下一轮。
	s.AllowReorder = false
	s.SuggestedOrderBaseline = nil
	s.ClearTerminalOutcome()
}

// AdvanceStep 推进到下一个计划步骤，并返回是否仍有剩余步骤。
func (s *CommonState) AdvanceStep() bool {
	s.CurrentStep++
	return s.CurrentStep < len(s.PlanSteps)
}

// Done 标记整个任务流程已经结束。
//
// 说明：
// 1. 若此前已经写入 aborted / exhausted 等终止结果，这里只负责兜底维持 PhaseDone，不覆盖已有语义；
// 2. 只有在尚未写入任何终止结果时，才默认补成 completed。
func (s *CommonState) Done() {
	s.Phase = PhaseDone
	if s.TerminalOutcome != nil {
		s.TerminalOutcome.Normalize()
		return
	}
	s.TerminalOutcome = &FlowTerminalOutcome{
		Status: FlowTerminalStatusCompleted,
	}
}

// Abort 将当前流程标记为“业务语义上的主动终止”。
//
// 步骤说明：
// 1. 统一写入 PhaseDone，保证 graph 后续直接进入 deliver 收口；
// 2. UserMessage 作为最终可见文案，必须尽量完整，避免 deliver 再二次猜测；
// 3. InternalReason 只用于排查，允许比用户文案更技术化。
func (s *CommonState) Abort(stage, code, userMessage, internalReason string) {
	s.Phase = PhaseDone
	s.TerminalOutcome = &FlowTerminalOutcome{
		Status:         FlowTerminalStatusAborted,
		Stage:          stage,
		Code:           code,
		UserMessage:    userMessage,
		InternalReason: internalReason,
	}
	s.TerminalOutcome.Normalize()
}

// Exhaust 将当前流程标记为“安全边界触发的被动停止”。
func (s *CommonState) Exhaust(stage, userMessage, internalReason string) {
	s.Phase = PhaseDone
	s.TerminalOutcome = &FlowTerminalOutcome{
		Status:         FlowTerminalStatusExhausted,
		Stage:          stage,
		Code:           "round_exhausted",
		UserMessage:    userMessage,
		InternalReason: internalReason,
	}
	s.TerminalOutcome.Normalize()
}

// ClearTerminalOutcome 清空上一轮遗留的终止结果。
func (s *CommonState) ClearTerminalOutcome() {
	if s == nil {
		return
	}
	s.TerminalOutcome = nil
}

// HasTerminalOutcome 判断当前是否已经写入正式终止结果。
func (s *CommonState) HasTerminalOutcome() bool {
	return s != nil && s.TerminalOutcome != nil
}

// TerminalStatus 返回当前终止结果的状态枚举。
func (s *CommonState) TerminalStatus() FlowTerminalStatus {
	if s == nil || s.TerminalOutcome == nil {
		return ""
	}
	return s.TerminalOutcome.Status
}

// IsCompleted 判断当前是否属于“正常完成”。
func (s *CommonState) IsCompleted() bool {
	return s.TerminalStatus() == FlowTerminalStatusCompleted
}

// IsAborted 判断当前是否属于“主动中止”。
func (s *CommonState) IsAborted() bool {
	return s.TerminalStatus() == FlowTerminalStatusAborted
}

// IsExhaustedTerminal 判断当前是否属于“轮次耗尽收口”。
func (s *CommonState) IsExhaustedTerminal() bool {
	return s.TerminalStatus() == FlowTerminalStatusExhausted
}

// HasPlan 判断当前 state 是否已经持有一份完整计划。
//
// 职责边界：
// 1. 负责收口"是否存在 plan"这一层判断，避免外层到处写 len(PlanSteps) > 0；
// 2. 不判断 CurrentStep 当前是否有效，当前步骤是否合法由 HasCurrentPlanStep 回答；
// 3. state 为空时统一返回 false，调用方可据此决定是否回退到 planning。
func (s *CommonState) HasPlan() bool {
	if s == nil {
		return false
	}
	return len(s.PlanSteps) > 0
}

// CurrentPlanStep 返回当前正在执行的结构化计划步骤。
//
// 职责边界：
// 1. 负责根据 CurrentStep 安全读取 PlanSteps，避免 graph/node/prompt 层重复写越界判断；
// 2. 若 state 为空、plan 为空、或当前索引越界，则统一返回 (PlanStep{}, false)；
// 3. 不负责推进步骤，也不负责修正 CurrentStep 的取值。
func (s *CommonState) CurrentPlanStep() (PlanStep, bool) {
	if s == nil {
		return PlanStep{}, false
	}
	if s.CurrentStep < 0 || s.CurrentStep >= len(s.PlanSteps) {
		return PlanStep{}, false
	}
	return s.PlanSteps[s.CurrentStep], true
}

// HasCurrentPlanStep 判断"当前步骤"是否存在且可安全读取。
func (s *CommonState) HasCurrentPlanStep() bool {
	_, ok := s.CurrentPlanStep()
	return ok
}

// PlanProgress 返回当前计划的执行进度。
//
// 输出语义：
// 1. current 使用更适合给用户看的 1-based 序号；
// 2. total 表示当前计划的总步数；
// 3. 若当前还没有计划，则返回 (0, 0)；
// 4. 若 CurrentStep 已越界到末尾之后，则把 current 收敛到 total，避免出现 total+1 这种噪音值。
func (s *CommonState) PlanProgress() (current int, total int) {
	if s == nil {
		return 0, 0
	}

	total = len(s.PlanSteps)
	if total == 0 {
		return 0, 0
	}
	if s.CurrentStep < 0 {
		return 0, total
	}
	if s.CurrentStep >= total {
		return total, total
	}
	return s.CurrentStep + 1, total
}
