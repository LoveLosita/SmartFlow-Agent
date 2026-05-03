package selection

import (
	"context"
	"errors"
	"fmt"
	"log"
	"math"
	"strings"
	"time"

	"github.com/LoveLosita/smartflow/backend/active_scheduler/candidate"
	llmservice "github.com/LoveLosita/smartflow/backend/services/llm"
)

const selectionMaxTokens = 1200

// Service 负责主动调度候选的受限 LLM 选择。
//
// 职责边界：
// 1. 只在后端候选中做选择和解释生成，不生成新候选；
// 2. LLM 失败、输出非法或选择不存在候选时，回退到后端 fallback candidate；
// 3. 不写 preview、不发通知、不修改正式日程。
type Service struct {
	client *llmservice.Client
	clock  func() time.Time
	logger *log.Logger
}

// NewService 创建主动调度选择器。
//
// 说明：
// 1. client 允许为空；为空时选择器只走确定性 fallback，便于本地测试和降级；
// 2. 真正的模型接入在 cmd/start.go 中完成：aiHub.Pro -> llm.Client -> selection.Service；
// 3. 选择器本身不持有模型配置，只表达本业务域的 prompt 和结果校验。
func NewService(client *llmservice.Client) *Service {
	return &Service{
		client: client,
		clock:  time.Now,
		logger: log.Default(),
	}
}

func (s *Service) SetClock(clock func() time.Time) {
	if s != nil && clock != nil {
		s.clock = clock
	}
}

// Select 对主动调度候选做有限选择。
//
// 步骤化说明：
// 1. 先校验 dry-run 输入，保证 LLM 不会拿到空上下文或空候选；
// 2. 若模型不可用，直接使用后端 fallback candidate，并显式标记 FallbackUsed；
// 3. 若模型可用，构造只读候选视图，隐藏 score / confidence / 原始事实快照；
// 4. 校验 LLM 返回的 candidate_id 是否存在，非法则回退；
// 5. 最终结果只交给 preview 层落库，不在这里产生任何副作用。
func (s *Service) Select(ctx context.Context, req SelectRequest) (Result, error) {
	if err := validateRequest(req); err != nil {
		return Result{}, err
	}

	if s == nil || s.client == nil {
		return buildFallbackResult(req, "模型客户端未配置"), nil
	}

	input := buildSelectionPromptInput(req, s.now())
	userPrompt, err := buildSelectionUserPrompt(input)
	if err != nil {
		return buildFallbackResult(req, "选择器 prompt 构造失败: "+err.Error()), nil
	}

	messages := llmservice.BuildSystemUserMessages(
		strings.TrimSpace(selectionSystemPrompt),
		nil,
		userPrompt,
	)
	resp, rawResult, err := llmservice.GenerateJSON[llmSelectionResponse](
		ctx,
		s.client,
		messages,
		llmservice.GenerateOptions{
			Temperature: 0.1,
			MaxTokens:   selectionMaxTokens,
			Thinking:    llmservice.ThinkingModeDisabled,
			Metadata: map[string]any{
				"stage":           "active_scheduler_select",
				"candidate_count": len(req.Candidates),
			},
		},
	)
	if err != nil {
		if s.logger != nil {
			s.logger.Printf("[WARN] 主动调度 LLM 选择失败，使用 fallback: err=%v raw=%s", err, truncateRaw(rawResult))
		}
		return buildFallbackResult(req, "模型选择失败: "+err.Error()), nil
	}

	result, fallbackUsed := convertLLMResponse(req, resp)
	if fallbackUsed && s.logger != nil {
		selectedCandidateID := ""
		action := ""
		if resp != nil {
			selectedCandidateID = strings.TrimSpace(resp.SelectedCandidateID)
			action = strings.TrimSpace(resp.Action)
		}
		s.logger.Printf("[WARN] 主动调度 LLM 选择结果非法，使用 fallback: selected=%q action=%q",
			selectedCandidateID,
			action,
		)
	}
	return result, nil
}

type llmSelectionResponse struct {
	Action              string `json:"action"`
	SelectedCandidateID string `json:"selected_candidate_id"`
	Reason              string `json:"reason"`
	ExplanationText     string `json:"explanation_text"`
	NotificationSummary string `json:"notification_summary"`
	AskUserQuestion     string `json:"ask_user_question"`
}

func validateRequest(req SelectRequest) error {
	if req.ActiveContext == nil {
		return errors.New("active scheduler selection 缺少上下文")
	}
	if len(req.Candidates) == 0 {
		return errors.New("active scheduler selection 缺少候选")
	}
	return nil
}

func convertLLMResponse(req SelectRequest, resp *llmSelectionResponse) (Result, bool) {
	if resp == nil {
		return buildFallbackResult(req, "模型返回空选择结果"), true
	}

	selected, ok := findCandidate(req.Candidates, resp.SelectedCandidateID)
	if !ok {
		return buildFallbackResult(req, "模型选择了不存在的候选"), true
	}

	inferredAction := inferAction(selected)
	action := normalizeAction(resp.Action)
	fallbackUsed := false
	if action == "" || !isActionCompatible(action, selected.CandidateType) {
		action = inferredAction
		fallbackUsed = true
	}

	explanation := firstNonEmpty(resp.ExplanationText, selected.Summary)
	notificationSummary := firstNonEmpty(resp.NotificationSummary, explanation, selected.Summary)
	askUserQuestion := strings.TrimSpace(resp.AskUserQuestion)
	if action == ActionAskUser && askUserQuestion == "" {
		askUserQuestion = explanation
	}

	return Result{
		Action:              action,
		SelectedCandidateID: selected.CandidateID,
		Reason:              strings.TrimSpace(resp.Reason),
		ExplanationText:     explanation,
		NotificationSummary: notificationSummary,
		AskUserQuestion:     askUserQuestion,
		FallbackUsed:        fallbackUsed,
		Confidence:          deriveInternalConfidence(selected),
	}, fallbackUsed
}

func buildFallbackResult(req SelectRequest, reason string) Result {
	selected := pickFallbackCandidate(req)
	action := inferAction(selected)
	explanation := firstNonEmpty(selected.Summary, reason)
	return Result{
		Action:              action,
		SelectedCandidateID: selected.CandidateID,
		Reason:              strings.TrimSpace(reason),
		ExplanationText:     explanation,
		NotificationSummary: explanation,
		AskUserQuestion:     fallbackAskUserQuestion(action, explanation),
		FallbackUsed:        true,
		Confidence:          deriveInternalConfidence(selected),
	}
}

func pickFallbackCandidate(req SelectRequest) candidate.Candidate {
	fallbackID := strings.TrimSpace(req.Observation.Decision.FallbackCandidateID)
	if fallbackID != "" {
		if selected, ok := findCandidate(req.Candidates, fallbackID); ok {
			return selected
		}
	}
	return req.Candidates[0]
}

func findCandidate(candidates []candidate.Candidate, id string) (candidate.Candidate, bool) {
	id = strings.TrimSpace(id)
	if id == "" {
		return candidate.Candidate{}, false
	}
	for _, item := range candidates {
		if strings.TrimSpace(item.CandidateID) == id {
			return item, true
		}
	}
	return candidate.Candidate{}, false
}

func normalizeAction(raw string) string {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case ActionSelectCandidate:
		return ActionSelectCandidate
	case ActionAskUser:
		return ActionAskUser
	case ActionNotifyOnly:
		return ActionNotifyOnly
	case ActionClose:
		return ActionClose
	default:
		return ""
	}
}

func inferAction(item candidate.Candidate) string {
	switch item.CandidateType {
	case candidate.TypeAskUser:
		return ActionAskUser
	case candidate.TypeNotifyOnly:
		return ActionNotifyOnly
	case candidate.TypeClose:
		return ActionClose
	default:
		return ActionSelectCandidate
	}
}

func isActionCompatible(action string, candidateType candidate.Type) bool {
	switch candidateType {
	case candidate.TypeAskUser:
		return action == ActionAskUser
	case candidate.TypeNotifyOnly:
		return action == ActionNotifyOnly
	case candidate.TypeClose:
		return action == ActionClose
	default:
		return action == ActionSelectCandidate
	}
}

func fallbackAskUserQuestion(action string, explanation string) string {
	if action != ActionAskUser {
		return ""
	}
	return strings.TrimSpace(explanation)
}

func deriveInternalConfidence(item candidate.Candidate) float64 {
	if !item.Validation.Valid {
		return 0.2
	}
	if item.Score <= 0 {
		return 0.55
	}
	score := float64(item.Score) / 100
	return math.Max(0.35, math.Min(0.95, score))
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

func truncateRaw(raw *llmservice.TextResult) string {
	if raw == nil {
		return ""
	}
	text := strings.TrimSpace(raw.Text)
	runes := []rune(text)
	if len(runes) <= 200 {
		return text
	}
	return string(runes[:200]) + "..."
}

func (s *Service) now() time.Time {
	if s == nil || s.clock == nil {
		return time.Now()
	}
	return s.clock()
}

func (r Result) String() string {
	return fmt.Sprintf("active_scheduler_selection(action=%s, selected=%s, fallback=%t)",
		r.Action,
		r.SelectedCandidateID,
		r.FallbackUsed,
	)
}
