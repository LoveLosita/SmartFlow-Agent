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
	// ControlTimeout 是“模型控制码分流”这一步的额外超时预算。
	//
	// 约束说明：
	// 1. 设为 0 代表完全继承父 ctx 的 deadline，不额外截断；
	// 2. 若后续线上观测到分流偶发超时，可再加一个小预算（例如 2s）做隔离。
	ControlTimeout = 0 * time.Second
)

var (
	// routeHeaderRegex 用于解析控制码头部。
	//
	// 支持动作：
	// 1. quick_note_create：新增随口记任务；
	// 2. task_query：查询任务；
	// 3. schedule_plan：智能排程（生成/微调排程计划）；
	// 4. chat：普通聊天；
	// 5. quick_note：历史兼容别名，解析后会映射到 quick_note_create。
	routeHeaderRegex = regexp.MustCompile(`(?is)<\s*smartflow_route\b[^>]*\bnonce\s*=\s*["']?([a-zA-Z0-9\-]+)["']?[^>]*\baction\s*=\s*["']?(quick_note_create|task_query|schedule_plan|quick_note|chat)["']?[^>]*>`)
	// routeReasonRegex 用于提取可选的理由块，方便日志排障。
	routeReasonRegex = regexp.MustCompile(`(?is)<\s*smartflow_reason\s*>(.*?)<\s*/\s*smartflow_reason\s*>`)
)

const routeControlPrompt = `你是 SmartFlow 的请求分流控制器。
你的唯一任务是给后端返回”可机读控制码”，不要做用户可见回复，不要解释。

动作定义：
1) quick_note_create：用户明确希望”记录/安排/提醒某件未来要做的事”。
2) task_query：用户想”查看/筛选/排序/获取”已有任务（如最紧急、按DDL、某象限、关键词）。
3) schedule_plan：用户想”生成/调整/微调日程排程”（如”帮我排个学习计划”、”把早八的课调走”、”我不想周末学习”）。
4) chat：其余全部普通对话（包括闲聊、知识问答、纯讨论”怎么安排任务”但未要求你真的去操作）。

判定优先级（冲突时按顺序）：
1) 若句子核心诉求是”帮我记一件事”，选 quick_note_create。
2) 若核心诉求是”帮我查任务列表/某类任务”，选 task_query。
3) 若核心诉求是”帮我排日程/调整日程/生成学习计划/修改排程”，选 schedule_plan。
4) 其他情况选 chat。

输出格式必须严格如下（两行）：
<SMARTFLOW_ROUTE nonce=”给定nonce” action=”quick_note_create|task_query|schedule_plan|chat”></SMARTFLOW_ROUTE>
<SMARTFLOW_REASON>一句不超过30字的中文理由</SMARTFLOW_REASON>

禁止输出任何其他内容。`

// Action 表示分流动作。
type Action string

const (
	ActionChat            Action = "chat"
	ActionQuickNoteCreate Action = "quick_note_create"
	ActionTaskQuery       Action = "task_query"
	ActionSchedulePlan    Action = "schedule_plan"

	// ActionQuickNote 是历史兼容别名，只用于解析旧 action 值。
	ActionQuickNote Action = "quick_note"
)

// ControlDecision 是“模型控制码解析结果”。
type ControlDecision struct {
	Action Action
	Reason string
	Raw    string
}

// RoutingDecision 是服务层使用的统一分流结果。
//
// 职责边界：
// 1. Action：最终动作（chat/quick_note_create/task_query）；
// 2. TrustRoute：是否允许下游跳过二次意图判定；
// 3. Detail：可选说明，用于阶段提示或日志。
type RoutingDecision struct {
	Action     Action
	TrustRoute bool
	Detail     string
	// RouteFailed 标记“控制码路由是否失败”。
	//
	// 语义：
	// 1. true：路由阶段发生异常（模型调用失败、控制码解析失败等）；
	// 2. false：路由阶段正常完成（无论最终 action 是 chat 还是其它分支）。
	//
	// 说明：
	// 1. 该字段用于让上层决定“是否直接报错而不是回落聊天”；
	// 2. 历史行为是失败回落 chat，本字段用于支持新的“失败即报错”策略。
	RouteFailed bool
}

// DecideActionRouting 通过“模型控制码”决定本次请求走向。
//
// 返回语义：
// 1. Action=quick_note_create：进入随口记写入图；
// 2. Action=task_query：进入任务查询 tool-calling；
// 3. Action=chat：进入普通聊天流；
// 4. 路由失败时会标记 RouteFailed=true，由上层直接返回内部错误。
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
		return RoutingDecision{
			Action:      ActionQuickNoteCreate,
			TrustRoute:  true,
			Detail:      reason,
			RouteFailed: false,
		}
	case ActionTaskQuery:
		reason := strings.TrimSpace(decision.Reason)
		if reason == "" {
			reason = "识别到任务查询请求，准备调用任务查询工具。"
		}
		return RoutingDecision{
			Action:      ActionTaskQuery,
			TrustRoute:  true,
			Detail:      reason,
			RouteFailed: false,
		}
	case ActionSchedulePlan:
		reason := strings.TrimSpace(decision.Reason)
		if reason == "" {
			reason = "识别到排程请求，准备执行智能排程流程。"
		}
		return RoutingDecision{
			Action:      ActionSchedulePlan,
			TrustRoute:  true,
			Detail:      reason,
			RouteFailed: false,
		}
	case ActionChat:
		return RoutingDecision{
			Action:      ActionChat,
			TrustRoute:  false,
			Detail:      "",
			RouteFailed: false,
		}
	default:
		// 兜底：未知动作视为路由异常，标记 RouteFailed 让上层统一报错。
		log.Printf("通用分流出现未知动作，标记路由失败并等待上层报错: action=%s raw=%s", decision.Action, decision.Raw)
		return RoutingDecision{
			Action:      ActionChat,
			TrustRoute:  false,
			Detail:      "",
			RouteFailed: true,
		}
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
//
// 设计要点：
// 1. timeout<=0 时不加额外 deadline，仅继承父上下文；
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
//
// 容错策略：
// 1. 允许大小写、属性顺序、额外属性差异；
// 2. nonce 必须精确匹配；
// 3. action 仅允许 quick_note_create/task_query/chat（兼容 quick_note）。
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
	case ActionQuickNoteCreate, ActionTaskQuery, ActionSchedulePlan, ActionChat:
		// 合法动作直接通过。
	case ActionQuickNote:
		// 兼容旧动作值：统一映射到 quick_note_create。
		action = ActionQuickNoteCreate
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
//
// 说明：
// 1. 旧代码只区分“进不进 quick_note”；
// 2. 新分流里 task_query 不应进入 quick_note，因此这里会映射为 false。
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
//
// 说明：
// 1. 旧测试仍调用该函数名；
// 2. 新实现统一委托给 ParseRouteControlTag。
func ParseQuickNoteRouteControlTag(raw, expectedNonce string) (*ControlDecision, error) {
	return ParseRouteControlTag(raw, expectedNonce)
}
