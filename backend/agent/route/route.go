package route

import (
	"context"
	"fmt"
	"log"
	"regexp"
	"strings"
	"time"

	"github.com/cloudwego/eino-ext/components/model/ark"
	einoModel "github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
	"github.com/google/uuid"
	arkModel "github.com/volcengine/volcengine-go-sdk/service/arkruntime/model"
)

const (
	// ControlTimeout 表示“路由控制码”阶段的额外超时预算。
	// 说明：
	// 1. 设为 0 表示完全继承父 ctx 的 deadline，不额外截断。
	// 2. 若后续观察到路由阶段偶发超时，可按需配置一个小预算（例如 2s）。
	ControlTimeout = 0 * time.Second
)

var (
	// routeHeaderRegex 用于解析控制码头部。
	// 支持动作：
	// 1. quick_note_create：新增随口记任务。
	// 2. task_query：任务查询。
	// 3. schedule_plan_create：新建排程。
	// 4. schedule_plan_refine：连续对话微调排程。
	// 5. schedule_plan：历史兼容动作（解析后映射到 schedule_plan_create）。
	// 6. quick_note：历史兼容动作（解析后映射到 quick_note_create）。
	// 7. chat：普通聊天。
	routeHeaderRegex = regexp.MustCompile(`(?is)<\s*smartflow_route\b[^>]*\bnonce\s*=\s*["']?([a-zA-Z0-9\-]+)["']?[^>]*\baction\s*=\s*["']?(quick_note_create|task_query|schedule_plan_create|schedule_plan_refine|schedule_plan|quick_note|chat)["']?[^>]*>`)
	// routeReasonRegex 用于提取可选 reason，便于日志排障。
	routeReasonRegex = regexp.MustCompile(`(?is)<\s*smartflow_reason\s*>(.*?)<\s*/\s*smartflow_reason\s*>`)
)

const routeControlPrompt = `你是 SmartFlow 的请求分流控制器。
你的唯一任务是给后端返回“可机读控制码”，不要做用户可见回复，不要解释。

动作定义：
1) quick_note_create：用户明确要“帮我记一下/安排一个未来要做的事/提醒我”。
2) task_query：用户要“查任务、筛任务、按条件列任务”。
3) schedule_plan_create：用户要“新建/生成一份排程方案”。
4) schedule_plan_refine：用户要“基于已有排程做连续微调”（如挪动某天、限制某时段、局部改动）。
5) chat：其余普通聊天与讨论。

优先级（冲突时按顺序）：
1) quick_note_create
2) task_query
3) schedule_plan_refine
4) schedule_plan_create
5) chat

输出格式必须严格如下（两行）：
<SMARTFLOW_ROUTE nonce="给定nonce" action="quick_note_create|task_query|schedule_plan_create|schedule_plan_refine|chat"></SMARTFLOW_ROUTE>
<SMARTFLOW_REASON>一句不超过30字的中文理由</SMARTFLOW_REASON>

禁止输出任何其他内容。`

// Action 表示分流动作。
type Action string

const (
	ActionChat               Action = "chat"
	ActionQuickNoteCreate    Action = "quick_note_create"
	ActionTaskQuery          Action = "task_query"
	ActionSchedulePlanCreate Action = "schedule_plan_create"
	ActionSchedulePlanRefine Action = "schedule_plan_refine"

	// ActionSchedulePlan 是历史兼容动作值。
	// 说明：旧模型可能返回 schedule_plan，解析后统一映射到 schedule_plan_create。
	ActionSchedulePlan Action = "schedule_plan"
	// ActionQuickNote 是历史兼容动作值，解析后统一映射到 quick_note_create。
	ActionQuickNote Action = "quick_note"
)

// ControlDecision 表示“模型控制码解析结果”。
type ControlDecision struct {
	Action Action
	Reason string
	Raw    string
}

// RoutingDecision 是服务层使用的统一分流结果。
// 职责边界：
// 1. Action：最终动作（chat/quick_note_create/task_query/schedule_plan_create/schedule_plan_refine）。
// 2. TrustRoute：是否允许下游跳过二次意图判定。
// 3. Detail：可选说明，用于阶段提示或日志。
// 4. RouteFailed：标记“控制码路由是否失败”，供上层决定是否直接报错。
type RoutingDecision struct {
	Action      Action
	TrustRoute  bool
	Detail      string
	RouteFailed bool
}

// DecideActionRouting 通过“模型控制码”决定本次请求走向。
// 返回语义：
// 1. Action=quick_note_create：进入随口记链路。
// 2. Action=task_query：进入任务查询链路。
// 3. Action=schedule_plan_create：进入新建排程链路。
// 4. Action=schedule_plan_refine：进入连续微调链路。
// 5. Action=chat：进入普通聊天链路。
// 6. 路由失败时标记 RouteFailed=true，由上层统一处理。
func DecideActionRouting(ctx context.Context, selectedModel *ark.ChatModel, userMessage string) RoutingDecision {
	decision, err := routeByModelControlTag(ctx, selectedModel, userMessage)
	if err != nil {
		if deadline, ok := ctx.Deadline(); ok {
			log.Printf("通用分流控制码失败，标记路由失败并等待上层报错: err=%v parent_deadline_in_ms=%d route_timeout_ms=%d",
				err, time.Until(deadline).Milliseconds(), ControlTimeout.Milliseconds())
		} else {
			log.Printf("通用分流控制码失败，标记路由失败并等待上层报错: err=%v parent_deadline=none route_timeout_ms=%d",
				err, ControlTimeout.Milliseconds())
		}
		return RoutingDecision{
			Action:      ActionChat,
			TrustRoute:  false,
			Detail:      "",
			RouteFailed: true,
		}
	}

	switch decision.Action {
	case ActionQuickNoteCreate:
		reason := strings.TrimSpace(decision.Reason)
		if reason == "" {
			reason = "识别到新增任务请求，准备执行随口记流程。"
		}
		return RoutingDecision{Action: ActionQuickNoteCreate, TrustRoute: true, Detail: reason, RouteFailed: false}
	case ActionTaskQuery:
		reason := strings.TrimSpace(decision.Reason)
		if reason == "" {
			reason = "识别到任务查询请求，准备执行任务查询流程。"
		}
		return RoutingDecision{Action: ActionTaskQuery, TrustRoute: true, Detail: reason, RouteFailed: false}
	case ActionSchedulePlanCreate:
		reason := strings.TrimSpace(decision.Reason)
		if reason == "" {
			reason = "识别到新建排程请求，准备执行智能排程流程。"
		}
		return RoutingDecision{Action: ActionSchedulePlanCreate, TrustRoute: true, Detail: reason, RouteFailed: false}
	case ActionSchedulePlanRefine:
		reason := strings.TrimSpace(decision.Reason)
		if reason == "" {
			reason = "识别到排程微调请求，准备执行连续微调流程。"
		}
		return RoutingDecision{Action: ActionSchedulePlanRefine, TrustRoute: true, Detail: reason, RouteFailed: false}
	case ActionChat:
		return RoutingDecision{Action: ActionChat, TrustRoute: false, Detail: "", RouteFailed: false}
	default:
		log.Printf("通用分流出现未知动作，标记路由失败并等待上层报错: action=%s raw=%s", decision.Action, decision.Raw)
		return RoutingDecision{Action: ActionChat, TrustRoute: false, Detail: "", RouteFailed: true}
	}
}

func routeByModelControlTag(ctx context.Context, selectedModel *ark.ChatModel, userMessage string) (*ControlDecision, error) {
	if selectedModel == nil {
		return nil, fmt.Errorf("model is nil")
	}

	nonce := strings.ToLower(strings.ReplaceAll(uuid.NewString(), "-", ""))
	routeCtx, cancel := deriveRouteControlContext(ctx, ControlTimeout)
	defer cancel()

	nowText := time.Now().In(time.Local).Format("2006-01-02 15:04")
	userPrompt := fmt.Sprintf("nonce=%s\n当前时间=%s\n用户输入=%s", nonce, nowText, strings.TrimSpace(userMessage))

	resp, err := selectedModel.Generate(routeCtx, []*schema.Message{
		schema.SystemMessage(routeControlPrompt),
		schema.UserMessage(userPrompt),
	},
		ark.WithThinking(&arkModel.Thinking{Type: arkModel.ThinkingTypeDisabled}),
		einoModel.WithTemperature(0),
		einoModel.WithMaxTokens(120),
	)
	if err != nil {
		return nil, err
	}
	if resp == nil {
		return nil, fmt.Errorf("empty route response")
	}

	raw := strings.TrimSpace(resp.Content)
	if raw == "" {
		return nil, fmt.Errorf("empty route content")
	}

	return ParseRouteControlTag(raw, nonce)
}

// deriveRouteControlContext 为“控制码路由”创建子上下文。
// 设计要点：
// 1. timeout<=0 时不加额外 deadline，仅继承父上下文。
// 2. 父 ctx deadline 更紧时，沿用父上下文，避免过早超时误判。
func deriveRouteControlContext(parent context.Context, timeout time.Duration) (context.Context, context.CancelFunc) {
	if timeout <= 0 {
		return context.WithCancel(parent)
	}
	if deadline, ok := parent.Deadline(); ok {
		if time.Until(deadline) <= timeout {
			return context.WithCancel(parent)
		}
	}
	return context.WithTimeout(parent, timeout)
}

// ParseRouteControlTag 解析通用控制码返回。
// 容错策略：
// 1. 允许大小写、属性顺序、额外属性差异；
// 2. nonce 必须精确匹配；
// 3. 兼容旧 action 值（schedule_plan/quick_note）。
func ParseRouteControlTag(raw, expectedNonce string) (*ControlDecision, error) {
	text := strings.TrimSpace(raw)
	if text == "" {
		return nil, fmt.Errorf("route content is empty")
	}

	header := routeHeaderRegex.FindStringSubmatch(text)
	if len(header) < 3 {
		return nil, fmt.Errorf("route header not found: %s", text)
	}

	nonce := strings.ToLower(strings.TrimSpace(header[1]))
	if nonce != strings.ToLower(strings.TrimSpace(expectedNonce)) {
		return nil, fmt.Errorf("route nonce mismatch")
	}

	actionText := strings.ToLower(strings.TrimSpace(header[2]))
	action := Action(actionText)
	switch action {
	case ActionQuickNoteCreate, ActionTaskQuery, ActionSchedulePlanCreate, ActionSchedulePlanRefine, ActionChat:
		// 合法动作直接通过。
	case ActionQuickNote:
		action = ActionQuickNoteCreate
	case ActionSchedulePlan:
		action = ActionSchedulePlanCreate
	default:
		return nil, fmt.Errorf("invalid route action: %s", actionText)
	}

	reason := ""
	reasonMatch := routeReasonRegex.FindStringSubmatch(text)
	if len(reasonMatch) >= 2 {
		reason = strings.TrimSpace(reasonMatch[1])
	}

	return &ControlDecision{
		Action: action,
		Reason: reason,
		Raw:    text,
	}, nil
}

// DecideQuickNoteRouting 是历史兼容入口。
// 说明：
// 1. 旧代码只区分“是否进入 quick_note”；
// 2. 新分流中 task_query/schedule_plan_* 都不应进入 quick_note。
func DecideQuickNoteRouting(ctx context.Context, selectedModel *ark.ChatModel, userMessage string) RoutingDecision {
	decision := DecideActionRouting(ctx, selectedModel, userMessage)
	if decision.Action == ActionQuickNoteCreate {
		return decision
	}
	return RoutingDecision{
		Action:      ActionChat,
		TrustRoute:  false,
		Detail:      "",
		RouteFailed: decision.RouteFailed,
	}
}

// ParseQuickNoteRouteControlTag 是历史兼容解析入口。
// 说明：旧测试仍使用该方法名，内部统一委托 ParseRouteControlTag。
func ParseQuickNoteRouteControlTag(raw, expectedNonce string) (*ControlDecision, error) {
	return ParseRouteControlTag(raw, expectedNonce)
}
