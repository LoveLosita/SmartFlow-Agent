package model

import (
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
}

// ConfirmPlan 表示用户已确认计划，流程进入执行阶段。
func (s *CommonState) ConfirmPlan() {
	s.Phase = PhaseExecuting
}

// RejectPlan 表示用户拒绝当前计划，清空计划并回退到 planning。
func (s *CommonState) RejectPlan() {
	s.PlanSteps = nil
	s.CurrentStep = 0
	s.Phase = PhasePlanning
}

// AdvanceStep 推进到下一个计划步骤，并返回是否仍有剩余步骤。
func (s *CommonState) AdvanceStep() bool {
	s.CurrentStep++
	return s.CurrentStep < len(s.PlanSteps)
}

// Done 标记整个任务流程已经结束。
func (s *CommonState) Done() {
	s.Phase = PhaseDone
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
