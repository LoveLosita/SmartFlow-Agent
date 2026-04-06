package model

import (
	"fmt"
	"strings"
)

// PlanComplexity 表示规划阶段评估的任务复杂度。
type PlanComplexity string

const (
	// PlanComplexitySimple 表示简单明确的操作，步骤之间无复杂依赖。
	PlanComplexitySimple PlanComplexity = "simple"

	// PlanComplexityModerate 表示多步操作，需要一定推理但不涉及深度分析。
	PlanComplexityModerate PlanComplexity = "moderate"

	// PlanComplexityComplex 表示需要深度推理、多方案比较或复杂依赖关系的任务。
	PlanComplexityComplex PlanComplexity = "complex"
)

// PlanAction 表示规划阶段单轮决策的动作类型。
//
// 设计原则：
// 1. 规划阶段只关心“继续规划 / 追问用户 / 规划完成”这三类动作；
// 2. 这里先不把工具调用塞进 contract，避免过早把 plan loop 复杂化；
// 3. 规划层产出的是“自然语言计划”，不是执行层的工具动作。
type PlanAction string

const (
	// PlanActionContinue 表示当前信息已足够，继续规划下一轮。
	PlanActionContinue PlanAction = "continue"

	// PlanActionAskUser 表示当前规划缺少关键信息，需要中断并追问用户。
	PlanActionAskUser PlanAction = "ask_user"

	// PlanActionDone 表示规划已经完成，可以进入 confirm 或下一阶段。
	PlanActionDone PlanAction = "plan_done"
)

// PlanDecision 是 plan prompt 单轮产出的统一决策结构。
//
// 职责边界：
// 1. Speak 是本轮先对用户说的话；若 action=ask_user，通常这里会承载要追问的问题；
// 2. Action 是规划阶段的下一步动作类型；
// 3. Reason 是给后端和日志看的简短解释；
// 4. PlanSteps 只在 plan_done 时要求返回，表示本轮最终确认下来的完整自然语言计划；
// 5. NeedsRoughBuild 为 true 时，Confirm 后自动触发粗排节点，不需要 LLM 在 plan_steps 里手动描述放置步骤；
// 6. TaskClassIDs 是本次粗排涉及的任务类 ID 列表，与 CommonState.TaskClassIDs 保持一致。
type PlanDecision struct {
	Speak           string         `json:"speak,omitempty"`
	Action          PlanAction     `json:"action"`
	Reason          string         `json:"reason,omitempty"`
	Complexity      PlanComplexity `json:"complexity"`
	NeedThinking    bool           `json:"need_thinking"`
	PlanSteps       []PlanStep     `json:"plan_steps,omitempty"`
	NeedsRoughBuild bool           `json:"needs_rough_build,omitempty"`
	TaskClassIDs    []int          `json:"task_class_ids,omitempty"`
}

// Normalize 统一清洗规划决策中的字符串字段。
func (d *PlanDecision) Normalize() {
	if d == nil {
		return
	}
	d.Speak = strings.TrimSpace(d.Speak)
	d.Action = PlanAction(strings.TrimSpace(string(d.Action)))
	d.Reason = strings.TrimSpace(d.Reason)
	d.Complexity = PlanComplexity(strings.TrimSpace(string(d.Complexity)))
	for i := range d.PlanSteps {
		d.PlanSteps[i].Normalize()
	}
}

// Validate 校验规划决策的最小合法性。
//
// 校验原则：
// 1. 这里只校验“协议是否自洽”，不校验规划内容是否聪明、是否足够好；
// 2. 只有 plan_done 允许返回完整 plan_steps；
// 3. 真正的规划质量判断仍留给后续 node 层和用户确认环节。
func (d *PlanDecision) Validate() error {
	if d == nil {
		return fmt.Errorf("plan decision 不能为空")
	}

	d.Normalize()
	if d.Action == "" {
		return fmt.Errorf("plan decision.action 不能为空")
	}

	// 复杂度兜底：未填写时默认 moderate，不因此拒绝整个决策。
	switch d.Complexity {
	case PlanComplexitySimple, PlanComplexityModerate, PlanComplexityComplex:
		// ok
	case "":
		d.Complexity = PlanComplexityModerate
	default:
		return fmt.Errorf("未知 complexity: %s", d.Complexity)
	}

	switch d.Action {
	case PlanActionContinue, PlanActionAskUser:
		if len(d.PlanSteps) > 0 {
			return fmt.Errorf("%s 动作不应携带 plan_steps", d.Action)
		}
		return nil
	case PlanActionDone:
		if len(d.PlanSteps) == 0 {
			return fmt.Errorf("plan_done 动作必须携带完整 plan_steps")
		}
		for i := range d.PlanSteps {
			if err := d.PlanSteps[i].Validate(); err != nil {
				return fmt.Errorf("plan_steps[%d] 非法: %w", i, err)
			}
		}
		return nil
	default:
		return fmt.Errorf("未知 plan action: %s", d.Action)
	}
}

// PlanStep 表示规划阶段产出的一条自然语言步骤。
//
// 设计说明：
// 1. Content 是步骤正文，后续可直接落到 CommonState.PlanSteps；
// 2. DoneWhen 是可选的完成判定描述，用来给 execute 阶段提供最小退出条件；
// 3. 这里仍然保持“自然语言优先”，不把 plan step 过度结构化。
type PlanStep struct {
	Content  string `json:"content"`
	DoneWhen string `json:"done_when,omitempty"`
}

// Normalize 统一清洗 plan step 中的字符串字段。
func (s *PlanStep) Normalize() {
	if s == nil {
		return
	}
	s.Content = strings.TrimSpace(s.Content)
	s.DoneWhen = strings.TrimSpace(s.DoneWhen)
}

// Validate 校验单条 plan step 的最小合法性。
func (s *PlanStep) Validate() error {
	if s == nil {
		return fmt.Errorf("plan step 不能为空")
	}
	s.Normalize()
	if s.Content == "" {
		return fmt.Errorf("plan step.content 不能为空")
	}
	return nil
}
