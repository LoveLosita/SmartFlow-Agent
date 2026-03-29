package model

// Phase 表示 agent 循环图当前所处的阶段。
type Phase string

const (
	PhasePlanning       Phase = "planning"
	PhaseWaitingConfirm Phase = "waiting_confirm"
	PhaseExecuting      Phase = "executing"
	PhaseDone           Phase = "done"
)

const DefaultMaxRounds = 30

type CommonState struct {
	// 身份
	TraceID        string
	UserID         int
	ConversationID string

	// 流程阶段
	Phase Phase

	// Plan
	PlanSteps   []string
	CurrentStep int

	// 安全边界
	MaxRounds int
	RoundUsed int
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

// NextRound 消耗一轮并返回是否还有余量。
func (s *CommonState) NextRound() bool {
	s.RoundUsed++
	return s.RoundUsed <= s.MaxRounds
}

// Exhausted 判断是否已耗尽轮次。
func (s *CommonState) Exhausted() bool {
	return s.RoundUsed >= s.MaxRounds
}

// FinishPlan 标记 plan 完成，进入等待确认阶段。
func (s *CommonState) FinishPlan(steps []string) {
	s.PlanSteps = steps
	s.CurrentStep = 0
	s.Phase = PhaseWaitingConfirm
}

// ConfirmPlan 用户确认后进入执行阶段。
func (s *CommonState) ConfirmPlan() {
	s.Phase = PhaseExecuting
}

// RejectPlan 用户拒绝，回到规划阶段。
func (s *CommonState) RejectPlan() {
	s.PlanSteps = nil
	s.CurrentStep = 0
	s.Phase = PhasePlanning
}

// AdvanceStep 推进到下一个 plan 步骤，返回是否还有剩余步骤。
func (s *CommonState) AdvanceStep() bool {
	s.CurrentStep++
	return s.CurrentStep < len(s.PlanSteps)
}

// Done 标记整个流程结束。
func (s *CommonState) Done() {
	s.Phase = PhaseDone
}

// HasPlan 判断当前 state 是否已经持有一份可执行的 plan。
//
// 职责边界：
// 1. 负责把“外部直接判断 len(PlanSteps) > 0”的零散逻辑收口到 state 内部；
// 2. 只回答“是否存在 plan”，不判断当前索引是否有效；
// 3. 当 state 为空时返回 false，调用方可据此决定是否回退到重新规划。
func (s *CommonState) HasPlan() bool {
	if s == nil {
		return false
	}
	return len(s.PlanSteps) > 0
}

// CurrentPlanStep 返回当前正在执行的 plan 步骤文本。
//
// 职责边界：
// 1. 负责根据 CurrentStep 安全读取 PlanSteps，避免调用方重复写切片越界判断；
// 2. 当 state 为空、plan 为空、或当前索引越界时，统一返回 ("", false)；
// 3. 不负责推进步骤，也不负责修正 CurrentStep 的取值。
func (s *CommonState) CurrentPlanStep() (string, bool) {
	if s == nil {
		return "", false
	}
	if s.CurrentStep < 0 || s.CurrentStep >= len(s.PlanSteps) {
		return "", false
	}
	return s.PlanSteps[s.CurrentStep], true
}

// HasCurrentPlanStep 判断“当前步骤”是否存在且可安全读取。
//
// 职责边界：
// 1. 负责给 graph / node 层提供一个更直白的布尔判断入口；
// 2. 内部复用 CurrentPlanStep，避免两处维护相同的索引边界逻辑；
// 3. 不返回步骤内容，只回答“当前是否还有可注入的步骤”。
func (s *CommonState) HasCurrentPlanStep() bool {
	_, ok := s.CurrentPlanStep()
	return ok
}

// PlanProgress 返回当前 plan 的执行进度。
//
// 输出语义：
// 1. current 使用对人类更友好的 1-based 序号，适合直接写入 prompt 或日志；
// 2. total 表示当前 plan 总步数；
// 3. 若尚未生成 plan，则返回 (0, 0)；
// 4. 若 CurrentStep 已越过末尾，则 current 会被收敛到 total，避免上层出现 total+1 这类噪音值。
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
