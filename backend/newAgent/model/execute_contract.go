package model

import (
	"fmt"
	"strings"
)

// ExecuteAction 表示 execute 阶段单轮决策的动作类型。
//
// 设计原则：
// 1. LLM 只负责“申报本轮想做什么”，不直接推进状态；
// 2. 后端只围绕这些有限动作做流程校验、证据校验、安全校验；
// 3. 动作枚举保持收敛，避免 execute 节点后续再次长成“自由文本协议”。
type ExecuteAction string

const (
	// ExecuteActionContinue 表示当前步骤尚未完成，需要继续本步骤的 ReAct 循环。
	ExecuteActionContinue ExecuteAction = "continue"

	// ExecuteActionAskUser 表示当前步骤缺少外部信息，需要中断并追问用户。
	ExecuteActionAskUser ExecuteAction = "ask_user"

	// ExecuteActionConfirm 表示当前步骤准备执行写操作，但必须先进入确认闸门。
	ExecuteActionConfirm ExecuteAction = "confirm"

	// ExecuteActionNextPlan 表示当前步骤已完成，可以推进到下一个 plan 步骤。
	ExecuteActionNextPlan ExecuteAction = "next_plan"

	// ExecuteActionDone 表示整个任务已完成，可以进入最终交付。
	ExecuteActionDone ExecuteAction = "done"

	// ExecuteActionAbort 表示本轮流程应立即终止，并进入 deliver 做正式收口。
	ExecuteActionAbort ExecuteAction = "abort"
)

// ExecuteDecision 是 execute prompt 单轮产出的统一决策结构。
//
// 职责边界：
// 1. Speak 是这轮先对用户说的话，适合在真正调工具前流式吐给前端；
// 2. Action 是模型申报的“下一步动作类型”；
// 3. Reason 是给后端和日志看的简短解释，不直接等价于完成证明；
// 4. ToolCall 只是“意图”，不代表工具已经真正执行成功。
type ExecuteDecision struct {
	Speak     string          `json:"speak,omitempty"`
	Action    ExecuteAction   `json:"action"`
	Reason    string          `json:"reason,omitempty"`
	GoalCheck string          `json:"goal_check,omitempty"`
	ToolCall  *ToolCallIntent `json:"tool_call,omitempty"`
	Abort     *AbortIntent    `json:"abort,omitempty"`
}

// Normalize 统一清洗 execute 决策中的字符串字段。
func (d *ExecuteDecision) Normalize() {
	if d == nil {
		return
	}
	d.Speak = strings.TrimSpace(d.Speak)
	d.Action = ExecuteAction(strings.TrimSpace(string(d.Action)))
	d.Reason = strings.TrimSpace(d.Reason)
	d.GoalCheck = strings.TrimSpace(d.GoalCheck)
	if d.ToolCall != nil {
		d.ToolCall.Normalize()
	}
	if d.Abort != nil {
		d.Abort.Normalize()
	}
}

// Validate 校验 execute 决策的最小合法性。
//
// 校验原则：
// 1. 这里只校验“协议是否自洽”，不校验工具是否真实存在，也不校验当前步骤是否真的完成；
// 2. 只允许少量动作与 tool_call 共存，避免后续 node 层收到含糊决策；
// 3. 真正的三类最小校验应放在执行层，这里只做第一道轻量门禁。
func (d *ExecuteDecision) Validate() error {
	if d == nil {
		return fmt.Errorf("execute decision 不能为空")
	}

	d.Normalize()
	if d.Action == "" {
		return fmt.Errorf("execute decision.action 不能为空")
	}

	switch d.Action {
	case ExecuteActionContinue:
		if d.Abort != nil {
			return fmt.Errorf("continue 动作不应携带 abort")
		}
		if d.ToolCall != nil {
			return d.ToolCall.Validate()
		}
		return nil
	case ExecuteActionAskUser:
		if d.ToolCall != nil {
			return fmt.Errorf("ask_user 动作不应携带 tool_call")
		}
		if d.Abort != nil {
			return fmt.Errorf("ask_user 动作不应携带 abort")
		}
		return nil
	case ExecuteActionConfirm:
		if d.ToolCall == nil {
			return fmt.Errorf("confirm 动作必须携带待确认的 tool_call")
		}
		if d.Abort != nil {
			return fmt.Errorf("confirm 动作不应同时携带 abort")
		}
		return d.ToolCall.Validate()
	case ExecuteActionNextPlan, ExecuteActionDone:
		if d.ToolCall != nil {
			return fmt.Errorf("%s 动作不应携带 tool_call", d.Action)
		}
		if d.Abort != nil {
			return fmt.Errorf("%s 动作不应携带 abort", d.Action)
		}
		return nil
	case ExecuteActionAbort:
		if d.ToolCall != nil {
			return fmt.Errorf("abort 动作不应携带 tool_call")
		}
		if d.Abort == nil {
			return fmt.Errorf("abort 动作必须携带 abort 字段")
		}
		return d.Abort.Validate()
	default:
		return fmt.Errorf("未知 execute action: %s", d.Action)
	}
}

// AbortIntent 表示 execute 阶段声明的正式终止意图。
//
// 说明：
// 1. code 是稳定机器码，便于后续前端/埋点识别终止类型；
// 2. user_message 是最终给用户看的收口文案；
// 3. internal_reason 只用于日志排查，允许更技术化。
type AbortIntent struct {
	Code           string `json:"code,omitempty"`
	UserMessage    string `json:"user_message"`
	InternalReason string `json:"internal_reason,omitempty"`
}

// Normalize 清洗终止意图中的稳定字段。
func (a *AbortIntent) Normalize() {
	if a == nil {
		return
	}
	a.Code = strings.TrimSpace(a.Code)
	a.UserMessage = strings.TrimSpace(a.UserMessage)
	a.InternalReason = strings.TrimSpace(a.InternalReason)
}

// Validate 校验终止意图的最小可用性。
func (a *AbortIntent) Validate() error {
	if a == nil {
		return fmt.Errorf("abort 不能为空")
	}
	a.Normalize()
	if a.UserMessage == "" {
		return fmt.Errorf("abort.user_message 不能为空")
	}
	return nil
}

// ToolCallIntent 表示 execute 阶段申报的工具调用意图。
//
// 设计目的：
// 1. 这里只描述“模型想调用什么工具、传什么参数”，不代表调用已经发生；
// 2. Arguments 暂时保留 map 结构，方便 prompt 输出原生 JSON 对象；
// 3. 是否需要 confirm 不应由模型决定，后续应由工具注册表或后端策略判定。
type ToolCallIntent struct {
	Name      string         `json:"name"`
	Arguments map[string]any `json:"arguments,omitempty"`
}

// Normalize 清洗工具调用意图中的稳定字段。
func (t *ToolCallIntent) Normalize() {
	if t == nil {
		return
	}
	t.Name = strings.TrimSpace(t.Name)
}

// Validate 校验工具调用意图的最小合法性。
func (t *ToolCallIntent) Validate() error {
	if t == nil {
		return fmt.Errorf("tool_call 不能为空")
	}
	t.Normalize()
	if t.Name == "" {
		return fmt.Errorf("tool_call.name 不能为空")
	}
	return nil
}

// ExecuteEvidenceSource 表示“当前步骤完成证明”来自哪里。
type ExecuteEvidenceSource string

const (
	// ExecuteEvidenceSourceToolObservation 表示来自读工具或分析工具的真实 observation。
	ExecuteEvidenceSourceToolObservation ExecuteEvidenceSource = "tool_observation"

	// ExecuteEvidenceSourceWriteReceipt 表示来自写工具成功执行后的回执。
	ExecuteEvidenceSourceWriteReceipt ExecuteEvidenceSource = "write_receipt"

	// ExecuteEvidenceSourceUserReply 表示来自用户补充回答的外部事实。
	ExecuteEvidenceSourceUserReply ExecuteEvidenceSource = "user_reply"
)

// ExecuteEvidenceReceipt 表示“一条可被后端认可的最小事实证据”。
//
// 职责边界：
// 1. StepIndex 用来绑定这条证据属于哪个 plan 步骤，避免旧 observation 污染新步骤；
// 2. Source / Name / Success 描述“这条证据是怎么来的、是否真的发生了”；
// 3. Summary 只用于日志、调试和交付串联，不替代原始 observation 本身；
// 4. 这里不做语义推理，只负责记录事实。
type ExecuteEvidenceReceipt struct {
	StepIndex       int                   `json:"step_index"`
	Source          ExecuteEvidenceSource `json:"source"`
	Name            string                `json:"name,omitempty"`
	ArgumentsDigest string                `json:"arguments_digest,omitempty"`
	Success         bool                  `json:"success"`
	Summary         string                `json:"summary,omitempty"`
}

// Normalize 清洗证据回执中的稳定字段。
func (r *ExecuteEvidenceReceipt) Normalize() {
	if r == nil {
		return
	}
	r.Source = ExecuteEvidenceSource(strings.TrimSpace(string(r.Source)))
	r.Name = strings.TrimSpace(r.Name)
	r.ArgumentsDigest = strings.TrimSpace(r.ArgumentsDigest)
	r.Summary = strings.TrimSpace(r.Summary)
}

// Validate 校验证据回执是否具备最小可用信息。
func (r *ExecuteEvidenceReceipt) Validate() error {
	if r == nil {
		return fmt.Errorf("evidence receipt 不能为空")
	}

	r.Normalize()
	if r.StepIndex < 0 {
		return fmt.Errorf("evidence receipt.step_index 不能小于 0")
	}
	switch r.Source {
	case ExecuteEvidenceSourceToolObservation, ExecuteEvidenceSourceWriteReceipt, ExecuteEvidenceSourceUserReply:
	default:
		return fmt.Errorf("未知 evidence source: %s", r.Source)
	}
	return nil
}

// ExecuteValidationResult 保存 execute 单轮的三类最小校验结果。
//
// 三类校验语义：
// 1. FlowPassed：当前动作在流程上是否合法，例如 done 是否允许直接发生；
// 2. EvidencePassed：当前动作是否有最小事实证据支撑；
// 3. SafetyPassed：当前动作是否触发了安全兜底，例如超轮次、重复空转、待确认未完成。
type ExecuteValidationResult struct {
	FlowPassed     bool   `json:"flow_passed"`
	FlowReason     string `json:"flow_reason,omitempty"`
	EvidencePassed bool   `json:"evidence_passed"`
	EvidenceReason string `json:"evidence_reason,omitempty"`
	SafetyPassed   bool   `json:"safety_passed"`
	SafetyReason   string `json:"safety_reason,omitempty"`
}
