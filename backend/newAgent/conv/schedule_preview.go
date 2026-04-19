package newagentconv

import (
	"fmt"
	"time"

	"github.com/LoveLosita/smartflow/backend/model"
	schedule "github.com/LoveLosita/smartflow/backend/newAgent/tools/schedule"
)

// ScheduleStateToPreview 将 newAgent 的 ScheduleState 转换为前端预览缓存格式。
//
// 职责边界：
// 1. 只做数据格式转换，不做业务逻辑；
// 2. 将每个 ScheduleTask 的每个 TaskSlot 转为一条 HybridScheduleEntry；
// 3. Day → (Week, DayOfWeek) 通过 ScheduleState.DayToWeekDay 转换；
// 4. 转换失败的 slot（day_index 无效）静默跳过。
func ScheduleStateToPreview(
	state *schedule.ScheduleState,
	userID int,
	conversationID string,
	taskClassIDs []int,
	summary string,
) *model.SchedulePlanPreviewCache {
	if state == nil {
		return nil
	}

	entries := make([]model.HybridScheduleEntry, 0, len(state.Tasks))
	for i := range state.Tasks {
		t := &state.Tasks[i]
		// 待安排且无位置的任务不生成 entry。
		if schedule.IsPendingTask(*t) {
			continue
		}

		for _, slot := range t.Slots {
			week, dayOfWeek, ok := state.DayToWeekDay(slot.Day)
			if !ok {
				continue
			}

			entry := model.HybridScheduleEntry{
				Week:        week,
				DayOfWeek:   dayOfWeek,
				SectionFrom: slot.SlotStart,
				SectionTo:   slot.SlotEnd,
				Name:        t.Name,
			}

			// Type 映射。
			if t.Source == "event" {
				if t.EventType != "" {
					entry.Type = t.EventType
				} else {
					entry.Type = "course"
				}
			} else {
				entry.Type = "task"
			}

			// Status 映射：existing 不变，suggested / 兼容建议态统一输出为 suggested。
			if shouldMarkSuggestedInPreview(*t) {
				entry.Status = "suggested"
			} else {
				entry.Status = "existing"
			}

			// ID 映射。
			if t.Source == "event" {
				entry.EventID = t.SourceID
			} else {
				entry.TaskItemID = t.SourceID
				entry.TaskClassID = t.TaskClassID
			}

			// 嵌入与阻塞语义。
			entry.CanBeEmbedded = t.CanEmbed
			if t.Source == "event" && t.CanEmbed && t.EmbeddedBy == nil {
				// 可嵌入且当前无嵌入任务 → 不阻塞 suggested 占位。
				entry.BlockForSuggested = false
			} else {
				entry.BlockForSuggested = true
			}

			entries = append(entries, entry)
		}
	}

	// 生成摘要（若调用方未提供）。
	if summary == "" {
		existingCount := 0
		suggestedCount := 0
		for _, e := range entries {
			if e.Status == "existing" {
				existingCount++
			} else {
				suggestedCount++
			}
		}
		summary = fmt.Sprintf("共 %d 个日程条目，其中已确定 %d 个，新安排 %d 个。", len(entries), existingCount, suggestedCount)
	}

	return &model.SchedulePlanPreviewCache{
		UserID:         userID,
		ConversationID: conversationID,
		Summary:        summary,
		HybridEntries:  entries,
		TaskClassIDs:   taskClassIDs,
		GeneratedAt:    time.Now(),
	}
}

// shouldMarkSuggestedInPreview 判断某条 ScheduleTask 在预览层是否应标记为 suggested。
//
// 规则说明：
//  1. 新语义下，显式 suggested 直接输出为建议态；
//  2. 兼容旧快照：pending+Slots、existing+Duration>0 的 task_item 也继续按 suggested 输出；
//  3. 这样前端预览口径可以在迁移期保持稳定，不会因为状态枚举切换而抖动。
func shouldMarkSuggestedInPreview(t schedule.ScheduleTask) bool {
	return schedule.IsSuggestedTask(t)
}
