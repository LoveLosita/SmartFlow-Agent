package model

import "strings"

const (
	// PhaseChatting 表示当前请求只需正常聊天，不进入 plan / execute 主链路。
	PhaseChatting Phase = "chatting"

	// PhaseInterrupted 表示本轮执行被“待用户交互”显式打断，当前连接应结束并等待恢复。
	PhaseInterrupted Phase = "interrupted"
)

const PendingInteractionSnapshotVersion = 1

// PendingInteractionType 表示当前挂起交互的类型。
type PendingInteractionType string

const (
	PendingInteractionTypeAskUser        PendingInteractionType = "ask_user"
	PendingInteractionTypeConfirm        PendingInteractionType = "confirm"
	PendingInteractionTypeConnectionLost PendingInteractionType = "connection_lost"
)

// PendingInteractionStatus 表示挂起交互的生命周期状态。
type PendingInteractionStatus string

const (
	PendingInteractionStatusOpen     PendingInteractionStatus = "open"
	PendingInteractionStatusResolved PendingInteractionStatus = "resolved"
	PendingInteractionStatusCanceled PendingInteractionStatus = "canceled"
)

// PendingToolCallSnapshot 保存“待确认工具调用”的最小快照。
//
// 职责边界：
// 1. 负责保存真正落库 / 落缓存恢复执行所需的最小信息；
// 2. ArgsJSON 约定存已经序列化好的参数快照，避免此处反向依赖具体 tool 参数结构；
// 3. 不负责工具执行，不负责幂等校验，不负责回滚。
type PendingToolCallSnapshot struct {
	ToolName string
	ArgsJSON string
	Summary  string
}

// PendingInteraction 保存“本轮需要中断并等待用户后续动作”的交互快照。
//
// 设计目的：
// 1. ask_user 与 confirm 都不是业务 tool，而是流程级中断，所以单独建模；
// 2. ResumeNode / ResumePhase / ResumeStep 用来记录恢复点，避免用户回答后整条链路从头乱跑；
// 3. 该结构设计成可被 Redis + MySQL 直接存储的快照骨架，后续只需要补序列化与持久化接线。
//
// TODO(newagent/store): 后续把该结构整体快照到 Redis + MySQL，形成双保险恢复点。
// TODO(newagent/api): 后续由“用户追问回复接口 / 确认回调接口”读取这份快照并恢复运行。
type PendingInteraction struct {
	Version       int
	InteractionID string
	Type          PendingInteractionType
	Status        PendingInteractionStatus
	DisplayText   string
	ResumeNode    string
	ResumePhase   Phase
	ResumeStep    int
	PendingTool   *PendingToolCallSnapshot
	Metadata      map[string]any
}

// AgentRuntimeState 是 graph 运行时真正流转的状态容器。
//
// 职责边界：
// 1. CommonState 继续只负责主流程控制；
// 2. PendingInteraction 负责承载“需要中断后恢复”的交互快照；
// 3. 这样既不污染 CommonState 的职责，又能让 graph 在一次入参里拿到完整运行态。
type AgentRuntimeState struct {
	*CommonState
	PendingInteraction *PendingInteraction
}

// NewAgentRuntimeState 创建 graph 运行态。
func NewAgentRuntimeState(state *CommonState) *AgentRuntimeState {
	rt := &AgentRuntimeState{CommonState: state}
	rt.EnsureCommonState()
	return rt
}

// EnsureCommonState 保证运行态里始终有一份可用的流程状态。
//
// 步骤说明：
// 1. 若 CommonState 为空，则补一份最小默认值，避免 graph / node 层空指针；
// 2. 若 Phase 尚未设置，则默认回到 planning，保持当前主链路的保守起点；
// 3. 若 MaxRounds 未设置，则回填默认值，避免编译后运行时无上限循环。
func (s *AgentRuntimeState) EnsureCommonState() *CommonState {
	if s == nil {
		return nil
	}
	if s.CommonState == nil {
		s.CommonState = &CommonState{}
	}
	if s.CommonState.Phase == "" {
		s.CommonState.Phase = PhasePlanning
	}
	if s.CommonState.MaxRounds <= 0 {
		s.CommonState.MaxRounds = DefaultMaxRounds
	}
	return s.CommonState
}

// HasPendingInteraction 判断当前是否存在待恢复交互。
func (s *AgentRuntimeState) HasPendingInteraction() bool {
	if s == nil || s.PendingInteraction == nil {
		return false
	}
	return s.PendingInteraction.Status == PendingInteractionStatusOpen
}

// PendingInteractionType 返回当前挂起交互类型。
func (s *AgentRuntimeState) PendingInteractionType() PendingInteractionType {
	if !s.HasPendingInteraction() {
		return ""
	}
	return s.PendingInteraction.Type
}

// OpenAskUserInteraction 打开一个“向用户追问”的中断快照。
func (s *AgentRuntimeState) OpenAskUserInteraction(interactionID, question, resumeNode string) {
	s.openPendingInteraction(
		PendingInteractionTypeAskUser,
		interactionID,
		question,
		resumeNode,
		nil,
	)
}

// OpenConfirmInteraction 打开一个“写操作待确认”的中断快照。
func (s *AgentRuntimeState) OpenConfirmInteraction(interactionID, confirmText, resumeNode string, pendingTool *PendingToolCallSnapshot) {
	s.openPendingInteraction(
		PendingInteractionTypeConfirm,
		interactionID,
		confirmText,
		resumeNode,
		pendingTool,
	)
}

// ResumeFromPending 从挂起交互恢复主流程。
//
// 步骤说明：
// 1. 仅当存在 open 状态的 pending interaction 时才执行恢复；
// 2. 恢复时回写之前快照下来的 phase / step，确保继续跑的是原任务位置而不是新分支；
// 3. 恢复成功后清空挂起快照，避免同一份 pending 被重复消费。
func (s *AgentRuntimeState) ResumeFromPending() bool {
	if !s.HasPendingInteraction() {
		return false
	}

	flowState := s.EnsureCommonState()
	pending := s.PendingInteraction
	flowState.Phase = pending.ResumePhase
	flowState.CurrentStep = pending.ResumeStep
	pending.Status = PendingInteractionStatusResolved
	s.PendingInteraction = nil
	return true
}

// ClearPendingInteraction 直接清空挂起交互。
//
// 职责边界：
// 1. 仅负责粗暴清空快照；
// 2. 不自动恢复 phase / step，避免误把“取消交互”与“恢复执行”混为一谈；
// 3. 若需要恢复流程，应优先使用 ResumeFromPending。
func (s *AgentRuntimeState) ClearPendingInteraction() {
	if s == nil || s.PendingInteraction == nil {
		return
	}
	s.PendingInteraction.Status = PendingInteractionStatusCanceled
	s.PendingInteraction = nil
}

func (s *AgentRuntimeState) openPendingInteraction(
	interactionType PendingInteractionType,
	interactionID string,
	displayText string,
	resumeNode string,
	pendingTool *PendingToolCallSnapshot,
) {
	if s == nil {
		return
	}

	flowState := s.EnsureCommonState()
	resumePhase := flowState.Phase
	if resumePhase == "" {
		resumePhase = PhasePlanning
	}

	s.PendingInteraction = &PendingInteraction{
		Version:       PendingInteractionSnapshotVersion,
		InteractionID: strings.TrimSpace(interactionID),
		Type:          interactionType,
		Status:        PendingInteractionStatusOpen,
		DisplayText:   strings.TrimSpace(displayText),
		ResumeNode:    strings.TrimSpace(resumeNode),
		ResumePhase:   resumePhase,
		ResumeStep:    flowState.CurrentStep,
		PendingTool:   clonePendingToolCallSnapshot(pendingTool),
	}

	// 1. 一旦进入 pending 状态，当前连接上的 graph 应立即停止向后执行。
	// 2. 这里先统一把 Phase 置为 interrupted，后续恢复时再按快照写回原阶段。
	// 3. 这样分支函数只需要判断 HasPendingInteraction()，无需猜测“当前 phase 是否仍可信”。
	flowState.Phase = PhaseInterrupted
}

func clonePendingToolCallSnapshot(snapshot *PendingToolCallSnapshot) *PendingToolCallSnapshot {
	if snapshot == nil {
		return nil
	}
	copied := *snapshot
	return &copied
}
