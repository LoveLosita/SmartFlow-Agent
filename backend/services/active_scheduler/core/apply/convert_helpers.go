package apply

import (
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
)

func normalizeChange(change ApplyChange, candidateType ChangeType) (ApplyChange, error) {
	change.Type = normalizeChangeType(change.Type, candidateType)
	if change.Type == ChangeTypeCompressWithNextDynamicTask {
		return ApplyChange{}, newApplyError(ErrorCodeUnsupportedChangeType, "MVP 明确关闭压缩融合 apply", nil)
	}
	if isNoopChangeType(change.Type) {
		return change, nil
	}

	fillTargetFields(&change)
	fillSlotFields(&change)
	if err := validateSlotFields(change); err != nil {
		return ApplyChange{}, err
	}
	return change, nil
}

func normalizeChangeType(changeType ChangeType, candidateType ChangeType) ChangeType {
	if changeType == "" || changeType == rawChangeTypeNone {
		if isNoopChangeType(candidateType) {
			return candidateType
		}
		return candidateType
	}
	if changeType == rawChangeTypeAdd && candidateType == ChangeTypeAddTaskPoolToSchedule {
		return ChangeTypeAddTaskPoolToSchedule
	}
	if changeType == rawChangeTypeAdd && candidateType == ChangeTypeCreateMakeup {
		return ChangeTypeCreateMakeup
	}
	return changeType
}

func fillTargetFields(change *ApplyChange) {
	switch change.Type {
	case ChangeTypeAddTaskPoolToSchedule:
		change.TargetType = firstNonEmpty(change.TargetType, "task_pool")
		change.TargetID = firstPositive(change.TargetID, change.TaskID)
		change.TaskID = firstPositive(change.TaskID, change.TargetID)
	case ChangeTypeCreateMakeup:
		sourceEventID := firstPositive(change.SourceEventID, change.MakeupForEventID, change.EventID, change.TargetID)
		change.TargetType = firstNonEmpty(change.TargetType, "schedule_event")
		change.TargetID = firstPositive(change.TargetID, sourceEventID)
		change.EventID = firstPositive(change.EventID, sourceEventID)
		change.SourceEventID = sourceEventID
		change.MakeupForEventID = firstPositive(change.MakeupForEventID, sourceEventID)
	}
}

func fillSlotFields(change *ApplyChange) {
	if len(change.Slots) > 0 {
		sort.SliceStable(change.Slots, func(i, j int) bool {
			return slotSortKey(change.Slots[i]) < slotSortKey(change.Slots[j])
		})
		first := change.Slots[0]
		last := change.Slots[len(change.Slots)-1]
		change.Week = firstPositive(change.Week, first.Week)
		change.DayOfWeek = firstPositive(change.DayOfWeek, first.DayOfWeek)
		change.SectionFrom = firstPositive(change.SectionFrom, first.Section)
		change.SectionTo = firstPositive(change.SectionTo, last.Section)
	}
	if change.DurationSections <= 0 && change.SectionFrom > 0 && change.SectionTo >= change.SectionFrom {
		change.DurationSections = change.SectionTo - change.SectionFrom + 1
	}
	if change.DurationSections <= 0 {
		change.DurationSections = 1
	}
	if change.SectionTo <= 0 && change.SectionFrom > 0 {
		change.SectionTo = change.SectionFrom + change.DurationSections - 1
	}
	if len(change.Slots) == 0 && change.Week > 0 && change.DayOfWeek > 0 && change.SectionFrom > 0 && change.SectionTo >= change.SectionFrom {
		change.Slots = buildSlots(change.Week, change.DayOfWeek, change.SectionFrom, change.SectionTo)
	}
}

func validateSlotFields(change ApplyChange) error {
	if change.TargetID <= 0 {
		return newApplyError(ErrorCodeInvalidEditedChanges, "change 缺少合法 target_id", nil)
	}
	if change.Week <= 0 || change.DayOfWeek <= 0 || change.SectionFrom <= 0 || change.SectionTo <= 0 {
		return newApplyError(ErrorCodeInvalidEditedChanges, "change 缺少合法节次坐标", nil)
	}
	if change.DayOfWeek < 1 || change.DayOfWeek > 7 {
		return newApplyError(ErrorCodeInvalidEditedChanges, "day_of_week 必须在 1 到 7 之间", nil)
	}
	if change.SectionFrom < 1 || change.SectionFrom > 12 || change.SectionTo < 1 || change.SectionTo > 12 || change.SectionTo < change.SectionFrom {
		return newApplyError(ErrorCodeInvalidEditedChanges, "section_from/section_to 必须是合法连续节次", nil)
	}
	if change.DurationSections != change.SectionTo-change.SectionFrom+1 {
		return newApplyError(ErrorCodeInvalidEditedChanges, "duration_sections 与节次数量不一致", nil)
	}
	return nil
}

func parseCandidateList(raw *string) ([]candidateSnapshot, error) {
	if raw == nil || strings.TrimSpace(*raw) == "" {
		return nil, nil
	}
	var first any
	if err := json.Unmarshal([]byte(*raw), &first); err != nil {
		return nil, newApplyError(ErrorCodeInvalidEditedChanges, "candidates_json 不是合法 JSON", err)
	}
	switch typed := first.(type) {
	case []any:
		result := make([]candidateSnapshot, 0, len(typed))
		var raws []json.RawMessage
		if err := json.Unmarshal([]byte(*raw), &raws); err != nil {
			return nil, newApplyError(ErrorCodeInvalidEditedChanges, "candidates_json 候选数组解析失败", err)
		}
		for _, item := range raws {
			candidate, err := parseCandidateSnapshot(item)
			if err != nil {
				return nil, err
			}
			result = append(result, candidate)
		}
		return result, nil
	case map[string]any:
		obj := make(map[string]json.RawMessage)
		if err := json.Unmarshal([]byte(*raw), &obj); err != nil {
			return nil, newApplyError(ErrorCodeInvalidEditedChanges, "candidates_json 候选对象解析失败", err)
		}
		if nested := rawField(obj, "candidates", "Candidates"); len(nested) > 0 {
			nestedText := string(nested)
			return parseCandidateList(&nestedText)
		}
		candidate, err := parseCandidateSnapshot([]byte(*raw))
		if err != nil {
			return nil, err
		}
		return []candidateSnapshot{candidate}, nil
	default:
		return nil, newApplyError(ErrorCodeInvalidEditedChanges, "candidates_json 结构不受支持", nil)
	}
}

func parseCandidateSnapshot(raw []byte) (candidateSnapshot, error) {
	obj := make(map[string]json.RawMessage)
	if err := json.Unmarshal(raw, &obj); err != nil {
		return candidateSnapshot{}, newApplyError(ErrorCodeInvalidEditedChanges, "candidate JSON 解析失败", err)
	}
	candidate := candidateSnapshot{
		CandidateID:   stringField(obj, "candidate_id", "CandidateID", "id", "ID"),
		CandidateType: ChangeType(stringField(obj, "candidate_type", "CandidateType", "type", "change_type")),
	}
	if candidate.CandidateID == "" {
		return candidateSnapshot{}, newApplyError(ErrorCodeInvalidEditedChanges, "candidate 缺少 candidate_id", nil)
	}
	if candidate.CandidateType == "" {
		return candidateSnapshot{}, newApplyError(ErrorCodeInvalidEditedChanges, "candidate 缺少 candidate_type", nil)
	}

	changeRaws := rawArrayField(obj, "changes", "Changes", "preview_changes", "PreviewChanges")
	candidate.Changes = make([]ApplyChange, 0, len(changeRaws))
	for _, rawChange := range changeRaws {
		change, err := parseApplyChange(rawChange, candidate.CandidateType)
		if err != nil {
			return candidateSnapshot{}, err
		}
		candidate.Changes = append(candidate.Changes, change)
	}
	if len(candidate.Changes) == 0 && isNoopChangeType(candidate.CandidateType) {
		candidate.Changes = []ApplyChange{{Type: candidate.CandidateType}}
	}
	return candidate, nil
}

func parseApplyChange(raw []byte, candidateType ChangeType) (ApplyChange, error) {
	obj := make(map[string]json.RawMessage)
	if err := json.Unmarshal(raw, &obj); err != nil {
		return ApplyChange{}, newApplyError(ErrorCodeInvalidEditedChanges, "change JSON 解析失败", err)
	}
	change := ApplyChange{
		ChangeID:         stringField(obj, "change_id", "ChangeID", "id", "ID"),
		Type:             ChangeType(stringField(obj, "type", "change_type", "ChangeType")),
		TargetType:       stringField(obj, "target_type", "TargetType"),
		TargetID:         intField(obj, "target_id", "TargetID"),
		TaskID:           intField(obj, "task_id", "TaskID"),
		EventID:          intField(obj, "event_id", "EventID"),
		Week:             intField(obj, "week", "Week"),
		DayOfWeek:        intField(obj, "day_of_week", "DayOfWeek"),
		SectionFrom:      intField(obj, "section_from", "SectionFrom"),
		SectionTo:        intField(obj, "section_to", "SectionTo"),
		DurationSections: intField(obj, "duration_sections", "DurationSections"),
		MakeupForEventID: intField(obj, "makeup_for_event_id", "MakeupForEventID"),
		SourceEventID:    intField(obj, "source_event_id", "SourceEventID"),
		EditedAllowed:    boolField(obj, "edited_allowed", "EditedAllowed"),
		Metadata:         mapStringField(obj, "metadata", "Metadata"),
	}
	if change.Type == "" {
		change.Type = candidateType
	}
	if len(change.Metadata) > 0 {
		change.MakeupForEventID = firstPositive(change.MakeupForEventID, parsePositiveInt(change.Metadata["makeup_for_event_id"]))
		change.SourceEventID = firstPositive(change.SourceEventID, parsePositiveInt(change.Metadata["source_event_id"]))
	}
	change.Slots = slotsFromRawChange(obj)
	return change, nil
}

func slotsFromRawChange(obj map[string]json.RawMessage) []Slot {
	if raw := rawField(obj, "slots", "Slots"); len(raw) > 0 {
		var slots []Slot
		if err := json.Unmarshal(raw, &slots); err == nil && len(slots) > 0 {
			return slots
		}
	}
	if raw := rawField(obj, "to_slot", "ToSlot"); len(raw) > 0 {
		span, ok := parseSlotSpan(raw)
		if ok {
			return buildSlots(span.Start.Week, span.Start.DayOfWeek, span.Start.Section, span.End.Section)
		}
	}
	return nil
}

func parseSlotSpan(raw []byte) (SlotSpan, bool) {
	obj := make(map[string]json.RawMessage)
	if err := json.Unmarshal(raw, &obj); err != nil {
		return SlotSpan{}, false
	}
	start := parseSlot(rawField(obj, "start", "Start"))
	end := parseSlot(rawField(obj, "end", "End"))
	duration := intField(obj, "duration_sections", "DurationSections")
	if end.IsZero() && !start.IsZero() {
		end = start
		if duration > 1 {
			end.Section = start.Section + duration - 1
		}
	}
	return SlotSpan{Start: start, End: end, DurationSections: firstPositive(duration, end.Section-start.Section+1)}, !start.IsZero() && !end.IsZero()
}

func parseSlot(raw []byte) Slot {
	if len(raw) == 0 {
		return Slot{}
	}
	obj := make(map[string]json.RawMessage)
	if err := json.Unmarshal(raw, &obj); err != nil {
		return Slot{}
	}
	return Slot{
		Week:      intField(obj, "week", "Week"),
		DayOfWeek: intField(obj, "day_of_week", "DayOfWeek"),
		Section:   intField(obj, "section", "Section"),
	}
}

func rawField(obj map[string]json.RawMessage, keys ...string) json.RawMessage {
	for _, key := range keys {
		if raw, ok := obj[key]; ok && len(raw) > 0 && string(raw) != "null" {
			return raw
		}
	}
	return nil
}

func rawArrayField(obj map[string]json.RawMessage, keys ...string) []json.RawMessage {
	raw := rawField(obj, keys...)
	if len(raw) == 0 {
		return nil
	}
	var items []json.RawMessage
	if err := json.Unmarshal(raw, &items); err != nil {
		return nil
	}
	return items
}

func stringField(obj map[string]json.RawMessage, keys ...string) string {
	raw := rawField(obj, keys...)
	if len(raw) == 0 {
		return ""
	}
	var value string
	if err := json.Unmarshal(raw, &value); err == nil {
		return strings.TrimSpace(value)
	}
	return strings.Trim(strings.TrimSpace(string(raw)), `"`)
}

func intField(obj map[string]json.RawMessage, keys ...string) int {
	raw := rawField(obj, keys...)
	if len(raw) == 0 {
		return 0
	}
	var value int
	if err := json.Unmarshal(raw, &value); err == nil {
		return value
	}
	var asString string
	if err := json.Unmarshal(raw, &asString); err == nil {
		return parsePositiveInt(asString)
	}
	return 0
}

func boolField(obj map[string]json.RawMessage, keys ...string) bool {
	raw := rawField(obj, keys...)
	if len(raw) == 0 {
		return false
	}
	var value bool
	if err := json.Unmarshal(raw, &value); err == nil {
		return value
	}
	return false
}

func mapStringField(obj map[string]json.RawMessage, keys ...string) map[string]string {
	raw := rawField(obj, keys...)
	if len(raw) == 0 {
		return nil
	}
	var result map[string]string
	if err := json.Unmarshal(raw, &result); err == nil {
		return result
	}
	var loose map[string]any
	if err := json.Unmarshal(raw, &loose); err != nil {
		return nil
	}
	result = make(map[string]string, len(loose))
	for key, value := range loose {
		result[key] = fmt.Sprint(value)
	}
	return result
}

func buildSlots(week int, dayOfWeek int, sectionFrom int, sectionTo int) []Slot {
	if week <= 0 || dayOfWeek <= 0 || sectionFrom <= 0 || sectionTo < sectionFrom {
		return nil
	}
	slots := make([]Slot, 0, sectionTo-sectionFrom+1)
	for section := sectionFrom; section <= sectionTo; section++ {
		slots = append(slots, Slot{Week: week, DayOfWeek: dayOfWeek, Section: section})
	}
	return slots
}

func slotsFromChange(change ApplyChange) []Slot {
	if len(change.Slots) > 0 {
		return append([]Slot(nil), change.Slots...)
	}
	return buildSlots(change.Week, change.DayOfWeek, change.SectionFrom, change.SectionTo)
}

func mergeContinuousChanges(changes []ApplyChange) []ApplyChange {
	if len(changes) <= 1 {
		return changes
	}
	merged := make([]ApplyChange, 0, len(changes))
	for _, change := range changes {
		if len(merged) == 0 {
			merged = append(merged, change)
			continue
		}
		last := &merged[len(merged)-1]
		if canMergeChange(*last, change) {
			last.SectionTo = change.SectionTo
			last.DurationSections += change.DurationSections
			last.Slots = append(last.Slots, change.Slots...)
			continue
		}
		merged = append(merged, change)
	}
	return merged
}

func canMergeChange(left ApplyChange, right ApplyChange) bool {
	return left.Type == right.Type &&
		left.TargetType == right.TargetType &&
		left.TargetID == right.TargetID &&
		left.SourceEventID == right.SourceEventID &&
		left.Week == right.Week &&
		left.DayOfWeek == right.DayOfWeek &&
		left.SectionTo+1 == right.SectionFrom &&
		left.EditedAllowed == right.EditedAllowed
}

func isNoopChangeType(changeType ChangeType) bool {
	return changeType == ChangeTypeAskUser || changeType == ChangeTypeNotifyOnly || changeType == ChangeTypeClose
}

func changeSortKey(change ApplyChange) string {
	return fmt.Sprintf("%s:%s:%010d:%010d:%010d:%010d:%010d",
		change.Type, change.TargetType, change.TargetID, change.SourceEventID, change.Week, change.DayOfWeek, change.SectionFrom)
}

func slotSortKey(slot Slot) string {
	return fmt.Sprintf("%010d:%010d:%010d", slot.Week, slot.DayOfWeek, slot.Section)
}

func changeScopeKey(change ApplyChange) string {
	return fmt.Sprintf("%s:%s:%d:%d", change.Type, change.TargetType, change.TargetID, change.SourceEventID)
}

func sameChangeForScope(left ApplyChange, right ApplyChange) bool {
	return left.Type == right.Type &&
		left.TargetType == right.TargetType &&
		left.TargetID == right.TargetID &&
		left.SourceEventID == right.SourceEventID &&
		left.Week == right.Week &&
		left.DayOfWeek == right.DayOfWeek &&
		left.SectionFrom == right.SectionFrom &&
		left.SectionTo == right.SectionTo &&
		left.DurationSections == right.DurationSections
}

func changeForHash(change ApplyChange) ApplyChange {
	change.NormalizedHash = ""
	return change
}

func cloneMetadata(metadata map[string]string) map[string]string {
	if len(metadata) == 0 {
		return nil
	}
	cloned := make(map[string]string, len(metadata))
	for key, value := range metadata {
		cloned[key] = value
	}
	return cloned
}

func firstPositive(values ...int) int {
	for _, value := range values {
		if value > 0 {
			return value
		}
	}
	return 0
}

func parsePositiveInt(value string) int {
	value = strings.TrimSpace(value)
	if value == "" {
		return 0
	}
	parsed, err := strconv.Atoi(value)
	if err != nil || parsed <= 0 {
		return 0
	}
	return parsed
}
