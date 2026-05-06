package feedbacklocate

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"errors"
	"fmt"
	"log"
	"sort"
	"strings"
	"time"

	"github.com/LoveLosita/smartflow/backend/services/active_scheduler/core/ports"
	"github.com/LoveLosita/smartflow/backend/services/active_scheduler/core/trigger"
	llmservice "github.com/LoveLosita/smartflow/backend/services/llm"
)

const locateMaxTokens = 800

// Service 负责把 unfinished_feedback 的补充话术定位到当前滚动窗口内的 schedule_event。
//
// 职责边界：
// 1. 只做“定位”与“继续追问”的判断，不负责正式日程写入。
// 2. 候选来自 ScheduleReader，JSON 判定来自 LLM，二者任一不可用时都回退为 ask_user。
// 3. 不创建新工具系统，也不直接产出 preview。
type Service struct {
	reader ports.ScheduleReader
	client *llmservice.Client
	clock  func() time.Time
	logger *log.Logger
}

// NewService 创建反馈定位服务。
//
// 说明：
// 1. reader / client 允许为空，方便在模型不可用或读模型暂时不可用时直接回退 ask_user。
// 2. 真正的定位能力只在 Resolve 内部按需启用。
func NewService(reader ports.ScheduleReader, client *llmservice.Client) *Service {
	return &Service{
		reader: reader,
		client: client,
		clock:  time.Now,
		logger: log.Default(),
	}
}

// SetClock 允许测试注入稳定时间。
func (s *Service) SetClock(clock func() time.Time) {
	if s != nil && clock != nil {
		s.clock = clock
	}
}

// SetLogger 允许外部替换日志器。
func (s *Service) SetLogger(logger *log.Logger) {
	if s != nil && logger != nil {
		s.logger = logger
	}
}

// Resolve 负责把用户补充信息定位到当前滚动窗口中的一个 schedule_event。
//
// 输入输出语义：
// 1. 成功时返回 action=select_candidate，且 target_type=schedule_event、target_id 可校验。
// 2. 失败时不硬猜，统一返回 action=ask_user。
// 3. 只有上下文取消这类外部中断才会返回 error。
func (s *Service) Resolve(ctx context.Context, req Request) (Result, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return Result{}, err
	}
	if req.UserID <= 0 {
		return Result{}, errors.New("feedback locate user_id 不能为空")
	}

	now := s.now()
	windowStart := now
	windowEnd := now.Add(24 * time.Hour)

	candidates, err := s.loadCandidates(ctx, req.UserID, windowStart, windowEnd, now)
	if err != nil {
		return s.buildAskUserResult(req, "读取滚动窗口日程失败: "+err.Error()), nil
	}
	if len(candidates) == 0 {
		return s.buildAskUserResult(req, "当前滚动窗口内没有可定位的日程块"), nil
	}

	if s == nil || s.client == nil {
		return s.buildAskUserResult(req, "模型暂不可用"), nil
	}

	userPrompt, err := buildUserPrompt(buildPromptInput(
		req,
		now.In(time.Local).Format(time.RFC3339),
		windowStart.In(time.Local).Format(time.RFC3339),
		windowEnd.In(time.Local).Format(time.RFC3339),
		candidates,
	))
	if err != nil {
		return s.buildAskUserResult(req, "定位 prompt 构造失败"), nil
	}

	messages := llmservice.BuildSystemUserMessages(strings.TrimSpace(locateSystemPrompt), nil, userPrompt)
	invokeCtx := llmservice.WithBillingContext(ctx, buildFeedbackLocateBillingContext(req))
	resp, rawResult, err := llmservice.GenerateJSON[llmResponse](
		invokeCtx,
		s.client,
		messages,
		llmservice.GenerateOptions{
			Temperature: 0.1,
			MaxTokens:   locateMaxTokens,
			Thinking:    llmservice.ThinkingModeDisabled,
			Metadata: map[string]any{
				"stage":           "active_scheduler_feedback_locate",
				"candidate_count": len(candidates),
			},
		},
	)
	if err != nil {
		if s.logger != nil {
			s.logger.Printf("[WARN] active scheduler feedback locate failed: err=%v raw=%s", err, truncateRaw(rawResult))
		}
		return s.buildAskUserResult(req, "模型定位失败"), nil
	}

	result, fallbackUsed := s.convertResponse(req, resp, candidates)
	if fallbackUsed && s.logger != nil {
		selectedID := 0
		action := ""
		targetType := ""
		if resp != nil {
			selectedID = resp.TargetID
			action = strings.TrimSpace(resp.Action)
			targetType = strings.TrimSpace(resp.TargetType)
		}
		s.logger.Printf("[WARN] active scheduler feedback locate fallback: action=%q target_type=%q target_id=%d", action, targetType, selectedID)
	}
	return result, nil
}

func (s *Service) convertResponse(req Request, resp *llmResponse, candidates []eventCandidate) (Result, bool) {
	if resp == nil {
		return s.buildAskUserResult(req, "模型返回空结果"), true
	}

	candidateMap := make(map[int]eventCandidate, len(candidates))
	for _, item := range candidates {
		candidateMap[item.TargetID] = item
	}

	action := normalizeAction(resp.Action)
	targetType := strings.TrimSpace(resp.TargetType)
	targetID := resp.TargetID
	reason := strings.TrimSpace(resp.Reason)
	askUserQuestion := strings.TrimSpace(resp.AskUserQuestion)

	if action == ActionSelectCandidate &&
		strings.EqualFold(targetType, TargetTypeScheduleEvent) &&
		targetID > 0 {
		if _, ok := candidateMap[targetID]; ok {
			return Result{
				Action:          ActionSelectCandidate,
				TargetType:      TargetTypeScheduleEvent,
				TargetID:        targetID,
				Reason:          reason,
				AskUserQuestion: "",
			}, false
		}
	}

	question := firstNonEmptyString(
		askUserQuestion,
		BuildAskUserQuestion(req.MissingInfo),
		req.PendingQuestion,
	)
	return Result{
		Action:          ActionAskUser,
		TargetType:      TargetTypeScheduleEvent,
		TargetID:        0,
		Reason:          reason,
		AskUserQuestion: question,
	}, true
}

func (s *Service) buildAskUserResult(req Request, reason string) Result {
	return Result{
		Action:          ActionAskUser,
		TargetType:      TargetTypeScheduleEvent,
		TargetID:        0,
		Reason:          strings.TrimSpace(reason),
		AskUserQuestion: firstNonEmptyString(BuildAskUserQuestion(req.MissingInfo), req.PendingQuestion),
	}
}

func (s *Service) loadCandidates(ctx context.Context, userID int, windowStart time.Time, windowEnd time.Time, now time.Time) ([]eventCandidate, error) {
	if s == nil || s.reader == nil {
		return nil, nil
	}

	facts, err := s.reader.GetScheduleFactsByWindow(ctx, ports.ScheduleWindowRequest{
		UserID:      userID,
		TargetType:  string(trigger.TargetTypeScheduleEvent),
		TargetID:    0,
		WindowStart: windowStart,
		WindowEnd:   windowEnd,
		Now:         now,
	})
	if err != nil {
		return nil, err
	}
	return buildEventCandidates(facts.Events), nil
}

func buildEventCandidates(events []ports.ScheduleEventFact) []eventCandidate {
	if len(events) == 0 {
		return nil
	}

	sorted := append([]ports.ScheduleEventFact(nil), events...)
	sort.SliceStable(sorted, func(i, j int) bool {
		return eventBefore(sorted[i], sorted[j])
	})

	candidates := make([]eventCandidate, 0, len(sorted))
	for _, item := range sorted {
		candidates = append(candidates, eventCandidate{
			TargetID:    item.ID,
			Title:       strings.TrimSpace(item.Title),
			SourceType:  strings.TrimSpace(item.SourceType),
			RelatedID:   item.RelID,
			SlotSummary: summarizeSlots(item.Slots),
		})
	}
	return candidates
}

func eventBefore(left, right ports.ScheduleEventFact) bool {
	leftStart := firstSlotStart(left.Slots)
	rightStart := firstSlotStart(right.Slots)
	if !leftStart.IsZero() && !rightStart.IsZero() && !leftStart.Equal(rightStart) {
		return leftStart.Before(rightStart)
	}
	if left.ID != right.ID {
		return left.ID < right.ID
	}
	return strings.TrimSpace(left.Title) < strings.TrimSpace(right.Title)
}

func firstSlotStart(slots []ports.Slot) time.Time {
	if len(slots) == 0 {
		return time.Time{}
	}
	sorted := append([]ports.Slot(nil), slots...)
	sort.SliceStable(sorted, func(i, j int) bool {
		return slotBefore(sorted[i], sorted[j])
	})
	return sorted[0].StartAt
}

func slotBefore(left, right ports.Slot) bool {
	if !left.StartAt.IsZero() && !right.StartAt.IsZero() && !left.StartAt.Equal(right.StartAt) {
		return left.StartAt.Before(right.StartAt)
	}
	if left.Week != right.Week {
		return left.Week < right.Week
	}
	if left.DayOfWeek != right.DayOfWeek {
		return left.DayOfWeek < right.DayOfWeek
	}
	return left.Section < right.Section
}

func summarizeSlots(slots []ports.Slot) string {
	if len(slots) == 0 {
		return ""
	}

	sorted := append([]ports.Slot(nil), slots...)
	sort.SliceStable(sorted, func(i, j int) bool {
		return slotBefore(sorted[i], sorted[j])
	})

	parts := make([]string, 0, minInt(3, len(sorted)))
	for idx, slot := range sorted {
		if idx >= 3 {
			break
		}
		parts = append(parts, summarizeSlot(slot))
	}
	if len(sorted) > 3 {
		parts = append(parts, "...")
	}
	return strings.Join(parts, "；")
}

func summarizeSlot(slot ports.Slot) string {
	if !slot.StartAt.IsZero() && !slot.EndAt.IsZero() {
		return fmt.Sprintf("%s-%s", slot.StartAt.In(time.Local).Format("01-02 15:04"), slot.EndAt.In(time.Local).Format("15:04"))
	}
	return fmt.Sprintf("W%d-D%d-S%d", slot.Week, slot.DayOfWeek, slot.Section)
}

func normalizeAction(raw string) string {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case ActionSelectCandidate:
		return ActionSelectCandidate
	case ActionAskUser:
		return ActionAskUser
	default:
		return ""
	}
}

func firstNonEmptyString(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

func cloneAndTrimStrings(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	result := make([]string, 0, len(values))
	seen := make(map[string]struct{}, len(values))
	for _, item := range values {
		trimmed := strings.TrimSpace(item)
		if trimmed == "" {
			continue
		}
		if _, ok := seen[trimmed]; ok {
			continue
		}
		seen[trimmed] = struct{}{}
		result = append(result, trimmed)
	}
	return result
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

func minInt(left, right int) int {
	if left < right {
		return left
	}
	return right
}

func buildFeedbackLocateBillingContext(req Request) llmservice.BillingContext {
	if req.UserID <= 0 {
		return llmservice.BillingContext{
			Scene:      "active_scheduler_feedback_locate",
			ModelAlias: "active_scheduler_feedback_locate",
		}
	}
	sum := sha1.Sum([]byte(strings.TrimSpace(req.UserMessage) + "|" + strings.TrimSpace(req.PendingQuestion)))
	requestID := fmt.Sprintf("active_scheduler_feedback_locate:%d:%s", req.UserID, hex.EncodeToString(sum[:]))
	return llmservice.BillingContext{
		UserID:     uint64(req.UserID),
		EventID:    requestID,
		Scene:      "active_scheduler_feedback_locate",
		RequestID:  requestID,
		ModelAlias: "active_scheduler_feedback_locate",
	}
}
