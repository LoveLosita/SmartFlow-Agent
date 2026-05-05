package preview

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/LoveLosita/smartflow/backend/services/active_scheduler/core/candidate"
	schedulercontext "github.com/LoveLosita/smartflow/backend/services/active_scheduler/core/context"
	"github.com/LoveLosita/smartflow/backend/services/active_scheduler/core/observe"
	"github.com/LoveLosita/smartflow/backend/services/active_scheduler/core/ports"
	"github.com/LoveLosita/smartflow/backend/services/runtime/model"
)

func candidateDTO(item candidate.Candidate) CandidateDTO {
	return CandidateDTO{
		CandidateID:   item.CandidateID,
		CandidateType: string(item.CandidateType),
		Title:         item.Title,
		Summary:       item.Summary,
		Target: CandidateTargetDTO{
			TargetType: item.Target.TargetType,
			TargetID:   item.Target.TargetID,
			Title:      item.Target.Title,
		},
		Changes:       changeDTOs(item.CandidateID, item.Changes),
		BeforeSummary: item.BeforeSummary,
		AfterSummary:  item.AfterSummary,
		Risk:          item.Risk,
		Score:         item.Score,
		Validation:    item.Validation,
		Source:        item.Source,
	}
}

func changeDTOs(candidateID string, changes []candidate.ChangeItem) []ActiveScheduleChangeItem {
	result := make([]ActiveScheduleChangeItem, 0, len(changes))
	for index, change := range changes {
		var fromSlot *SlotDTO
		if change.FromSlot != nil {
			value := slotDTO(*change.FromSlot)
			fromSlot = &value
		}
		var toSlot *SlotSpanDTO
		if change.ToSlot != nil {
			value := slotSpanDTO(*change.ToSlot)
			toSlot = &value
		}
		result = append(result, ActiveScheduleChangeItem{
			ChangeID:         fmt.Sprintf("%s:chg_%d", candidateID, index+1),
			ChangeType:       string(change.ChangeType),
			TargetType:       change.TargetType,
			TargetID:         change.TargetID,
			FromSlot:         fromSlot,
			ToSlot:           toSlot,
			DurationSections: change.DurationSections,
			AffectedEventIDs: append([]int(nil), change.AffectedEventIDs...),
			EditedAllowed:    change.EditedAllowed,
			Metadata:         copyStringMap(change.Metadata),
		})
	}
	return result
}

func contextSummaryDTO(activeContext *schedulercontext.ActiveScheduleContext) ContextSummaryDTO {
	if activeContext == nil {
		return ContextSummaryDTO{}
	}
	return ContextSummaryDTO{
		UserID:        activeContext.User.UserID,
		Timezone:      activeContext.User.Timezone,
		TriggerSource: string(activeContext.Trigger.Source),
		RequestedAt:   activeContext.Trigger.RequestedAt,
		WindowStart:   activeContext.Window.StartAt,
		WindowEnd:     activeContext.Window.EndAt,
		WindowReason:  activeContext.Window.WindowReason,
		TargetType:    string(activeContext.Trigger.TargetType),
		TargetID:      activeContext.Trigger.TargetID,
		TargetTitle:   activeContext.Target.Title,
		MissingInfo:   append([]string(nil), activeContext.DerivedFacts.MissingInfo...),
		TraceSteps:    append([]string(nil), activeContext.Trace.BuildSteps...),
		Warnings:      append([]string(nil), activeContext.Trace.Warnings...),
	}
}

func buildBeforeSummary(activeContext *schedulercontext.ActiveScheduleContext, selected candidate.Candidate, changes []ActiveScheduleChangeItem) SchedulePreviewVersion {
	version := SchedulePreviewVersion{
		Title:        "调整前",
		WindowStart:  activeContext.Window.StartAt,
		WindowEnd:    activeContext.Window.EndAt,
		SummaryLines: compactLines(selected.BeforeSummary),
	}
	affected := affectedEventSet(changes)
	for _, event := range activeContext.ScheduleFacts.Events {
		entry := entryFromEvent(event)
		if affected[event.ID] || (selected.Target.TargetType == "schedule_event" && selected.Target.TargetID == event.ID) {
			entry.Status = "affected"
		}
		version.Entries = append(version.Entries, entry)
	}
	return version
}

func buildAfterSummary(before SchedulePreviewVersion, selected candidate.Candidate, changes []ActiveScheduleChangeItem) SchedulePreviewVersion {
	after := SchedulePreviewVersion{
		Title:        "调整后",
		WindowStart:  before.WindowStart,
		WindowEnd:    before.WindowEnd,
		Entries:      append([]SchedulePreviewEntry(nil), before.Entries...),
		SummaryLines: compactLines(selected.AfterSummary),
	}
	for _, change := range changes {
		// 1. 只把会产生可视化新块的 change 追加到 after；ask_user / none 不伪造正式日程。
		// 2. 该 entry 仅用于展示和后续 confirm 校验输入，不代表已经写入 schedule_events / schedules。
		if change.ToSlot == nil || (change.ChangeType != string(candidate.ChangeTypeAdd) && change.ChangeType != string(candidate.ChangeTypeCreateMakeup)) {
			continue
		}
		after.Entries = append(after.Entries, SchedulePreviewEntry{
			EntryID:     "preview:" + change.ChangeID,
			SourceType:  change.TargetType,
			SourceID:    change.TargetID,
			Title:       selected.Target.Title,
			StartAt:     change.ToSlot.Start.StartAt,
			EndAt:       change.ToSlot.End.EndAt,
			Week:        change.ToSlot.Start.Week,
			DayOfWeek:   change.ToSlot.Start.DayOfWeek,
			SectionFrom: change.ToSlot.Start.Section,
			SectionTo:   change.ToSlot.End.Section,
			Status:      "added",
			Editable:    change.EditedAllowed,
		})
	}
	return after
}

func entryFromEvent(event ports.ScheduleEventFact) SchedulePreviewEntry {
	slots := append([]ports.Slot(nil), event.Slots...)
	sort.Slice(slots, func(i, j int) bool {
		if !slots[i].StartAt.IsZero() && !slots[j].StartAt.IsZero() && !slots[i].StartAt.Equal(slots[j].StartAt) {
			return slots[i].StartAt.Before(slots[j].StartAt)
		}
		if slots[i].Week != slots[j].Week {
			return slots[i].Week < slots[j].Week
		}
		if slots[i].DayOfWeek != slots[j].DayOfWeek {
			return slots[i].DayOfWeek < slots[j].DayOfWeek
		}
		return slots[i].Section < slots[j].Section
	})

	entry := SchedulePreviewEntry{
		EntryID:    fmt.Sprintf("%s:%d", event.SourceType, event.ID),
		SourceType: event.SourceType,
		SourceID:   event.ID,
		Title:      event.Title,
		Status:     "unchanged",
		Editable:   event.IsDynamicTask,
	}
	if len(slots) == 0 {
		return entry
	}
	first := slots[0]
	last := slots[len(slots)-1]
	entry.StartAt = first.StartAt
	entry.EndAt = last.EndAt
	entry.Week = first.Week
	entry.DayOfWeek = first.DayOfWeek
	entry.SectionFrom = first.Section
	entry.SectionTo = last.Section
	return entry
}

func riskDTO(selected candidate.Candidate, observation observe.Result, changes []ActiveScheduleChangeItem, fallbackUsed bool) RiskDTO {
	affectedIDs := make([]int, 0)
	seen := make(map[int]bool)
	for _, change := range changes {
		for _, id := range change.AffectedEventIDs {
			if !seen[id] {
				seen[id] = true
				affectedIDs = append(affectedIDs, id)
			}
		}
	}
	level := "low"
	if !selected.Validation.Valid {
		level = "high"
	} else if observation.Metrics.Risk.RequiresReorder || len(affectedIDs) > 0 {
		level = "medium"
	}
	return RiskDTO{
		Level:        level,
		Summary:      selected.Risk,
		Validation:   selected.Validation,
		RiskMetrics:  observation.Metrics.Risk,
		AffectedIDs:  affectedIDs,
		RequiresLLM:  observation.Decision.LLMSelectionRequired,
		FallbackUsed: fallbackUsed,
	}
}

func buildBaseVersion(activeContext *schedulercontext.ActiveScheduleContext, changes []ActiveScheduleChangeItem) string {
	type eventVersion struct {
		ID    int       `json:"id"`
		Slots []SlotDTO `json:"slots"`
	}
	events := make([]eventVersion, 0, len(activeContext.ScheduleFacts.Events))
	for _, event := range activeContext.ScheduleFacts.Events {
		slots := make([]SlotDTO, 0, len(event.Slots))
		for _, slot := range event.Slots {
			slots = append(slots, slotDTO(slot))
		}
		events = append(events, eventVersion{ID: event.ID, Slots: slots})
	}
	payload := struct {
		UserID      int                        `json:"user_id"`
		TargetType  string                     `json:"target_type"`
		TargetID    int                        `json:"target_id"`
		WindowStart time.Time                  `json:"window_start"`
		WindowEnd   time.Time                  `json:"window_end"`
		Events      []eventVersion             `json:"events"`
		Changes     []ActiveScheduleChangeItem `json:"changes"`
	}{
		UserID:      activeContext.User.UserID,
		TargetType:  string(activeContext.Trigger.TargetType),
		TargetID:    activeContext.Trigger.TargetID,
		WindowStart: activeContext.Window.StartAt,
		WindowEnd:   activeContext.Window.EndAt,
		Events:      events,
		Changes:     changes,
	}
	raw, _ := json.Marshal(payload)
	sum := sha256.Sum256(raw)
	return "sha256:" + hex.EncodeToString(sum[:])
}

func detailFromModel(row *model.ActiveSchedulePreview, now time.Time) (ActiveSchedulePreviewDetail, error) {
	selected, err := decodeJSONField(row.SelectedCandidateJSON, CandidateDTO{})
	if err != nil {
		return ActiveSchedulePreviewDetail{}, err
	}
	candidates, err := decodeJSONField(row.CandidatesJSON, []CandidateDTO{})
	if err != nil {
		return ActiveSchedulePreviewDetail{}, err
	}
	decision, err := decodeJSONField(row.DecisionJSON, observe.Decision{})
	if err != nil {
		return ActiveSchedulePreviewDetail{}, err
	}
	metrics, err := decodeJSONField(row.MetricsJSON, observe.Metrics{})
	if err != nil {
		return ActiveSchedulePreviewDetail{}, err
	}
	issues, err := decodeJSONField(row.IssuesJSON, []observe.Issue{})
	if err != nil {
		return ActiveSchedulePreviewDetail{}, err
	}
	contextSummary, err := decodeJSONField(row.ContextSummaryJSON, ContextSummaryDTO{})
	if err != nil {
		return ActiveSchedulePreviewDetail{}, err
	}
	before, err := decodeJSONField(row.BeforeSummaryJSON, SchedulePreviewVersion{})
	if err != nil {
		return ActiveSchedulePreviewDetail{}, err
	}
	changes, err := decodeJSONField(row.PreviewChangesJSON, []ActiveScheduleChangeItem{})
	if err != nil {
		return ActiveSchedulePreviewDetail{}, err
	}
	after, err := decodeJSONField(row.AfterSummaryJSON, SchedulePreviewVersion{})
	if err != nil {
		return ActiveSchedulePreviewDetail{}, err
	}
	risk, err := decodeJSONField(row.RiskJSON, RiskDTO{})
	if err != nil {
		return ActiveSchedulePreviewDetail{}, err
	}

	expired := !row.ExpiresAt.After(now)
	canConfirm := row.Status == model.ActiveSchedulePreviewStatusReady && row.ApplyStatus == model.ActiveScheduleApplyStatusNone && !expired
	canIgnore := row.Status == model.ActiveSchedulePreviewStatusReady && row.ApplyStatus == model.ActiveScheduleApplyStatusNone && !expired

	return ActiveSchedulePreviewDetail{
		PreviewID:   row.ID,
		Status:      row.Status,
		ApplyStatus: row.ApplyStatus,
		ExpiresAt:   row.ExpiresAt,
		GeneratedAt: row.GeneratedAt,
		Expired:     expired,
		Trigger: PreviewTriggerDTO{
			TriggerID:   row.TriggerID,
			TriggerType: row.TriggerType,
			Source:      contextSummary.TriggerSource,
			TargetType:  row.TargetType,
			TargetID:    row.TargetID,
			RequestedAt: contextSummary.RequestedAt,
		},
		Explanation:       row.ExplanationText,
		Notification:      row.NotificationSummary,
		SelectedCandidate: selected,
		Candidates:        candidates,
		Decision:          decision,
		Metrics:           metrics,
		Issues:            issues,
		ContextSummary:    contextSummary,
		Before:            before,
		After:             after,
		Changes:           changes,
		Risk:              risk,
		BaseVersion:       row.BaseVersion,
		CanConfirm:        canConfirm,
		CanIgnore:         canIgnore,
		TraceID:           row.TraceID,
	}, nil
}

func jsonString(value any) (string, error) {
	raw, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	return string(raw), nil
}

func compactLines(lines ...string) []string {
	result := make([]string, 0, len(lines))
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line != "" {
			result = append(result, line)
		}
	}
	return result
}

func affectedEventSet(changes []ActiveScheduleChangeItem) map[int]bool {
	result := make(map[int]bool)
	for _, change := range changes {
		for _, id := range change.AffectedEventIDs {
			result[id] = true
		}
	}
	return result
}

func copyStringMap(input map[string]string) map[string]string {
	if len(input) == 0 {
		return nil
	}
	output := make(map[string]string, len(input))
	for key, value := range input {
		output[key] = value
	}
	return output
}
