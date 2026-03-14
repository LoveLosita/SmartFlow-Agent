package route

import (
	"context"
	"fmt"
	"log"
	"regexp"
	"strings"
	"time"

	"github.com/LoveLosita/smartflow/backend/agent/quicknote"
	"github.com/cloudwego/eino-ext/components/model/ark"
	einoModel "github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
	"github.com/google/uuid"
	arkModel "github.com/volcengine/volcengine-go-sdk/service/arkruntime/model"
)

const (
	// ControlTimeout 是“模型控制码分流”步骤的额外子超时。
	// 设为 0 表示完全跟随父请求上下文，不额外截断。
	ControlTimeout = 0 * time.Second
)

var (
	// 控制头格式：
	// <SMARTFLOW_ROUTE nonce="xxx" action="quick_note|chat"></SMARTFLOW_ROUTE>
	routeHeaderRegex = regexp.MustCompile(`(?is)<\s*smartflow_route\b[^>]*\bnonce\s*=\s*["']?([a-zA-Z0-9\-]+)["']?[^>]*\baction\s*=\s*["']?(quick_note|chat)["']?[^>]*>`)
	// 可选理由块：
	// <SMARTFLOW_REASON>...</SMARTFLOW_REASON>
	routeReasonRegex = regexp.MustCompile(`(?is)<\s*smartflow_reason\s*>(.*?)<\s*/\s*smartflow_reason\s*>`)
)

// Action 表示控制码路由动作。
type Action string

const (
	ActionChat      Action = "chat"
	ActionQuickNote Action = "quick_note"
)

// ControlDecision 是“控制码解析结果”。
type ControlDecision struct {
	Action Action
	Reason string
	Raw    string
}

// RoutingDecision 是服务层最终使用的路由结果。
type RoutingDecision struct {
	EnterQuickNote bool
	TrustRoute     bool
	Detail         string
}

// DecideQuickNoteRouting 通过“模型控制码”决定本次请求走向。
// 返回语义：
// 1) EnterQuickNote=true：进入 quick_note graph；
// 2) TrustRoute=true：表示可跳过 graph 二次意图判定。
func DecideQuickNoteRouting(ctx context.Context, selectedModel *ark.ChatModel, userMessage string) RoutingDecision {
	decision, err := routeByModelControlTag(ctx, selectedModel, userMessage)
	if err != nil {
		if deadline, ok := ctx.Deadline(); ok {
			log.Printf("quick note 路由控制码失败，进入 graph 兜底: err=%v parent_deadline_in_ms=%d route_timeout_ms=%d",
				err, time.Until(deadline).Milliseconds(), ControlTimeout.Milliseconds())
		} else {
			log.Printf("quick note 路由控制码失败，进入 graph 兜底: err=%v parent_deadline=none route_timeout_ms=%d",
				err, ControlTimeout.Milliseconds())
		}
		return RoutingDecision{
			EnterQuickNote: true,
			TrustRoute:     false,
			Detail:         "路由判定暂不可用，已进入任务识别兜底流程。",
		}
	}

	switch decision.Action {
	case ActionQuickNote:
		reason := strings.TrimSpace(decision.Reason)
		if reason == "" {
			reason = "模型识别到任务安排请求，准备执行随口记。"
		}
		return RoutingDecision{
			EnterQuickNote: true,
			TrustRoute:     true,
			Detail:         reason,
		}
	case ActionChat:
		return RoutingDecision{
			EnterQuickNote: false,
			TrustRoute:     false,
			Detail:         "",
		}
	default:
		log.Printf("quick note 未知路由动作，进入 graph 兜底: action=%s raw=%s", decision.Action, decision.Raw)
		return RoutingDecision{
			EnterQuickNote: true,
			TrustRoute:     false,
			Detail:         "路由结果异常，已进入任务识别兜底流程。",
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
		schema.SystemMessage(quicknote.QuickNoteRouteControlPrompt),
		schema.UserMessage(userPrompt),
	},
		ark.WithThinking(&arkModel.Thinking{Type: arkModel.ThinkingTypeDisabled}),
		einoModel.WithTemperature(0),
		einoModel.WithMaxTokens(80),
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

	return ParseQuickNoteRouteControlTag(raw, nonce)
}

// deriveRouteControlContext 为“控制码路由”创建子上下文。
// 设计要点：
// 1) timeout<=0 时不加额外 deadline，仅继承父上下文；
// 2) 父 ctx deadline 更紧时，沿用父上下文，避免过早超时误判。
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

// ParseQuickNoteRouteControlTag 解析控制码返回。
// 容错策略：
// 1) 允许大小写、属性顺序、额外属性差异；
// 2) nonce 必须精确匹配；
// 3) action 仅允许 quick_note/chat。
func ParseQuickNoteRouteControlTag(raw, expectedNonce string) (*ControlDecision, error) {
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
	if action != ActionQuickNote && action != ActionChat {
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
