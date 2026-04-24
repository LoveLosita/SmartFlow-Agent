package newagentrouter

import (
	"fmt"
	"regexp"
	"strings"

	newagentmodel "github.com/LoveLosita/smartflow/backend/newAgent/model"
)

var (
	// chatRouteHeaderRegex 从模型流式输出中解析 SMARTFLOW_ROUTE 控制码头部。
	//
	// 格式示例：
	//   <SMARTFLOW_ROUTE nonce="abc" route="execute" rough_build="true" refine="false" reorder="false" thinking="true"/>
	//
	// 属性说明：
	//   1. nonce：防注入校验，必须与调用方传入的 nonce 精确匹配；
	//   2. route：路由目标（direct_reply / execute / deep_answer / plan）；
	//   3. rough_build：可选，仅 route=execute 时有效，默认 false；
	//   4. refine：可选，仅 rough_build=true 时有效，默认 false；
	//   5. reorder：可选，仅 route=execute 时有效，默认 false；
	//   6. thinking：可选，仅 route=execute 时有效，默认 false。
	chatRouteHeaderRegex = regexp.MustCompile(
		`(?is)<\s*SMARTFLOW_ROUTE\b` +
			`[^>]*\bnonce\s*=\s*["']?([a-zA-Z0-9\-]+)["']?` +
			`[^>]*\broute\s*=\s*["']?(direct_reply|execute|deep_answer|plan|quick_task)["']?` +
			`(?:[^>]*\brough_build\s*=\s*["']?(true|false)["']?)?` +
			`(?:[^>]*\brefine\s*=\s*["']?(true|false)["']?)?` +
			`(?:[^>]*\breorder\s*=\s*["']?(true|false)["']?)?` +
			`(?:[^>]*\bthinking\s*=\s*["']?(true|false)["']?)?` +
			`[^>]*/\s*>`)
)

// StreamRouteParser 从 LLM 流式输出中增量提取路由决策。
//
// 协议约定：模型输出以 SMARTFLOW_ROUTE 控制码标签开头，标签结束后是用户可见内容。
// 例如：<SMARTFLOW_ROUTE nonce="abc" route="direct_reply"/>你好！很高兴见到你...
//
// 职责边界：
// 1. 只负责从流式 chunk 中提取控制码并解析为 ChatRoutingDecision；
// 2. 不负责推送 SSE chunk，不负责决定后续走哪条链路；
// 3. 控制码解析失败时标记 fallback，由上层决定降级策略。
type StreamRouteParser struct {
	buf        strings.Builder
	nonce      string
	routeFound bool
	decision   *newagentmodel.ChatRoutingDecision
}

// NewStreamRouteParser 创建流式路由解析器。
func NewStreamRouteParser(nonce string) *StreamRouteParser {
	return &StreamRouteParser{
		nonce: strings.ToLower(strings.TrimSpace(nonce)),
	}
}

// Feed 写入一段 chunk content。
//
// 返回值：
//   - visible：控制码标签之后的内容（用户可见文本）；
//   - routeReady：路由决策是否已确定；
//   - err：解析错误。
//
// 调用方应在 routeReady=true 后调用 Decision() 获取路由决策，
// 并根据 route 进入对应分支处理 visible 及后续 chunk。
func (p *StreamRouteParser) Feed(content string) (visible string, routeReady bool, err error) {
	if p.routeFound {
		// 路由已解析，后续 chunk 直接透传。
		return content, true, nil
	}

	p.buf.WriteString(content)

	text := p.buf.String()
	match := chatRouteHeaderRegex.FindStringSubmatchIndex(text)
	if match == nil {
		// 控制码尚未完整，检查是否应该 fallback。
		if len(text) > 500 {
			// 超过 500 字符仍未匹配到控制码 -> fallback 到 plan。
			p.routeFound = true
			p.decision = &newagentmodel.ChatRoutingDecision{
				Route: newagentmodel.ChatRoutePlan,
				Raw:   text,
			}
			return text, true, fmt.Errorf("控制码解析超时，fallback 到 plan")
		}
		return "", false, nil
	}

	// 提取匹配到的子组。
	groups := chatRouteHeaderRegex.FindStringSubmatch(text)
	if len(groups) < 3 {
		return "", false, fmt.Errorf("控制码正则子组不足: %d", len(groups))
	}

	// nonce 校验。
	parsedNonce := strings.ToLower(strings.TrimSpace(groups[1]))
	if parsedNonce != p.nonce {
		return "", false, fmt.Errorf("nonce 不匹配: got=%s expected=%s", parsedNonce, p.nonce)
	}

	// 解析 route。
	route := newagentmodel.ChatRoute(strings.TrimSpace(groups[2]))

	// 解析可选布尔属性（默认 false）。
	roughBuild := parseOptionalBool(groups, 3)
	refine := parseOptionalBool(groups, 4)
	reorder := parseOptionalBool(groups, 5)
	thinking := parseOptionalBool(groups, 6)

	p.decision = &newagentmodel.ChatRoutingDecision{
		Route:                      route,
		NeedsRoughBuild:            roughBuild,
		NeedsRefineAfterRoughBuild: refine,
		AllowReorder:               reorder,
		Thinking:                   thinking,
		Raw:                        groups[0],
	}

	// 归一化与校验。
	if validateErr := p.decision.Validate(); validateErr != nil {
		// 校验失败 -> fallback 到 plan。
		p.decision.Route = newagentmodel.ChatRoutePlan
		p.decision.NeedsRoughBuild = false
		p.decision.NeedsRefineAfterRoughBuild = false
		p.decision.AllowReorder = false
		p.decision.Thinking = false
	}

	p.routeFound = true

	// 控制码标签之后的文本作为 visible 返回。
	fullMatch := groups[0]
	tagEndIdx := strings.Index(text, fullMatch)
	if tagEndIdx >= 0 {
		afterTag := text[tagEndIdx+len(fullMatch):]
		// 去掉标签后紧跟的换行符（如果有）。
		afterTag = strings.TrimPrefix(afterTag, "\r\n")
		afterTag = strings.TrimPrefix(afterTag, "\n")
		return afterTag, true, nil
	}

	return "", true, nil
}

// RouteReady 返回路由决策是否已确定。
func (p *StreamRouteParser) RouteReady() bool {
	return p.routeFound
}

// Decision 返回已解析的路由决策（RouteReady=true 后可用）。
func (p *StreamRouteParser) Decision() *newagentmodel.ChatRoutingDecision {
	return p.decision
}

// parseOptionalBool 从正则子组中解析可选布尔值。
// 如果子组不存在或为空，返回 false。
func parseOptionalBool(groups []string, index int) bool {
	if index >= len(groups) {
		return false
	}
	return strings.TrimSpace(groups[index]) == "true"
}
