package sv

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/LoveLosita/smartflow/backend/respond"
	forummodel "github.com/LoveLosita/smartflow/backend/services/taskclassforum/model"
	forumcontracts "github.com/LoveLosita/smartflow/backend/shared/contracts/taskclassforum"
)

const (
	defaultPage     = 1
	defaultPageSize = 20
	maxPageSize     = 50
	maxPostTitleLen = 40
	maxSummaryLen   = 300
	maxTagCount     = 5
	maxTagLength    = 12
	maxCommentLen   = 500
	maxImportTitle  = 80
)

func normalizePage(page int, pageSize int) (int, int) {
	if page <= 0 {
		page = defaultPage
	}
	if pageSize <= 0 {
		pageSize = defaultPageSize
	}
	if pageSize > maxPageSize {
		pageSize = maxPageSize
	}
	return page, pageSize
}

func pageResult(page int, pageSize int, total int64) forumcontracts.PageResult {
	return forumcontracts.PageResult{
		Page:     page,
		PageSize: pageSize,
		Total:    int(total),
		HasMore:  int64(page*pageSize) < total,
	}
}

func normalizeTags(tags []string) ([]string, error) {
	result := make([]string, 0, len(tags))
	seen := make(map[string]struct{}, len(tags))
	for _, raw := range tags {
		tag := strings.TrimSpace(raw)
		if tag == "" {
			continue
		}
		if len([]rune(tag)) > maxTagLength {
			return nil, respond.ParamTooLong
		}
		if _, exists := seen[tag]; exists {
			continue
		}
		seen[tag] = struct{}{}
		result = append(result, tag)
		if len(result) > maxTagCount {
			return nil, respond.ParamTooLong
		}
	}
	return result, nil
}

func validateRuneMax(value string, maxLen int) error {
	if len([]rune(strings.TrimSpace(value))) > maxLen {
		return respond.ParamTooLong
	}
	return nil
}

func tagsToJSON(tags []string) (string, error) {
	if tags == nil {
		tags = []string{}
	}
	raw, err := json.Marshal(tags)
	if err != nil {
		return "", err
	}
	return string(raw), nil
}

func tagsFromJSON(raw string) []string {
	var tags []string
	if err := json.Unmarshal([]byte(raw), &tags); err != nil {
		return []string{}
	}
	return tags
}

func intSliceToJSONPtr(values []int) (*string, error) {
	if values == nil {
		values = []int{}
	}
	raw, err := json.Marshal(values)
	if err != nil {
		return nil, err
	}
	result := string(raw)
	return &result, nil
}

func stringSliceToJSONPtr(values []string) (*string, error) {
	if values == nil {
		values = []string{}
	}
	raw, err := json.Marshal(values)
	if err != nil {
		return nil, err
	}
	result := string(raw)
	return &result, nil
}

func intSliceFromJSONPtr(raw *string) []int {
	if raw == nil || strings.TrimSpace(*raw) == "" {
		return []int{}
	}
	var values []int
	if err := json.Unmarshal([]byte(*raw), &values); err != nil {
		return []int{}
	}
	return values
}

func stringSliceFromJSONPtr(raw *string) []string {
	if raw == nil || strings.TrimSpace(*raw) == "" {
		return []string{}
	}
	var values []string
	if err := json.Unmarshal([]byte(*raw), &values); err != nil {
		return []string{}
	}
	return values
}

func parseSnapshotDate(value string) *time.Time {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	parsed, err := time.ParseInLocation("2006-01-02", strings.TrimSpace(value), time.Local)
	if err != nil {
		return nil
	}
	return &parsed
}

func formatDate(value *time.Time) string {
	if value == nil || value.IsZero() {
		return ""
	}
	return value.Format("2006-01-02")
}

func formatTime(value time.Time) string {
	if value.IsZero() {
		return ""
	}
	return value.Format(time.RFC3339)
}

func formatTimePtr(value *time.Time) *string {
	if value == nil || value.IsZero() {
		return nil
	}
	formatted := value.Format(time.RFC3339)
	return &formatted
}

func userBrief(userID uint64) forumcontracts.UserBrief {
	return forumcontracts.UserBrief{
		UserID:   userID,
		Nickname: fmt.Sprintf("用户%d", userID),
	}
}

func countersFromPost(post forummodel.ForumPost) forumcontracts.ForumPostCounters {
	return forumcontracts.ForumPostCounters{
		LikeCount:    post.LikeCount,
		CommentCount: post.CommentCount,
		ImportCount:  post.ImportCount,
	}
}

func viewerState(postID uint64, liked map[uint64]bool, imported map[uint64]bool) forumcontracts.ForumPostViewerState {
	return forumcontracts.ForumPostViewerState{
		Liked:        liked[postID],
		ImportedOnce: imported[postID],
	}
}

func templateSummaryFromTemplate(template *forummodel.ForumPostTemplate, itemCount int) forumcontracts.TemplateSummary {
	if template == nil {
		return forumcontracts.TemplateSummary{}
	}
	return forumcontracts.TemplateSummary{
		TaskCount:      itemCount,
		Mode:           template.Mode,
		StartDate:      formatDate(template.StartDate),
		EndDate:        formatDate(template.EndDate),
		StrategyLabels: stringSliceFromJSONPtr(template.StrategyLabelsJSON),
	}
}

func postBriefFromModel(post forummodel.ForumPost, template *forummodel.ForumPostTemplate, itemCount int, state forumcontracts.ForumPostViewerState) forumcontracts.ForumPostBrief {
	return forumcontracts.ForumPostBrief{
		PostID:          post.ID,
		Title:           post.Title,
		Summary:         post.Summary,
		Tags:            tagsFromJSON(post.TagsJSON),
		Author:          userBrief(post.AuthorUserID),
		TemplateSummary: templateSummaryFromTemplate(template, itemCount),
		Counters:        countersFromPost(post),
		ViewerState:     state,
		Status:          post.Status,
		CreatedAt:       formatTime(post.CreatedAt),
	}
}

func templateDetailFromModel(template forummodel.ForumPostTemplate, items []forummodel.ForumPostTemplateItem) forumcontracts.TemplateDetail {
	sort.SliceStable(items, func(i, j int) bool {
		return items[i].Order < items[j].Order
	})
	preview := make([]forumcontracts.TemplateItemPreview, 0, len(items))
	for _, item := range items {
		preview = append(preview, forumcontracts.TemplateItemPreview{
			ItemID:  item.ID,
			Order:   item.Order,
			Content: item.Content,
		})
	}
	return forumcontracts.TemplateDetail{
		Mode:           template.Mode,
		StartDate:      formatDate(template.StartDate),
		EndDate:        formatDate(template.EndDate),
		StrategyLabels: stringSliceFromJSONPtr(template.StrategyLabelsJSON),
		TaskCount:      len(items),
		ItemsPreview:   preview,
	}
}

func snapshotFromTemplate(post forummodel.ForumPost, template forummodel.ForumPostTemplate, items []forummodel.ForumPostTemplateItem) TaskClassSnapshot {
	sort.SliceStable(items, func(i, j int) bool {
		return items[i].Order < items[j].Order
	})
	snapshotItems := make([]TaskClassSnapshotItem, 0, len(items))
	for _, item := range items {
		snapshotItems = append(snapshotItems, TaskClassSnapshotItem{
			TaskItemID: item.SourceTaskItemID,
			Order:      item.Order,
			Content:    item.Content,
		})
	}
	return TaskClassSnapshot{
		TaskClassID:        template.SourceTaskClassID,
		Title:              post.Title,
		Mode:               template.Mode,
		StartDate:          formatDate(template.StartDate),
		EndDate:            formatDate(template.EndDate),
		SubjectType:        template.SubjectType,
		DifficultyLevel:    template.DifficultyLevel,
		CognitiveIntensity: template.CognitiveIntensity,
		TotalSlots:         template.TotalSlots,
		AllowFillerCourse:  template.AllowFillerCourse,
		Strategy:           template.Strategy,
		ExcludedSlots:      intSliceFromJSONPtr(template.ExcludedSlotsJSON),
		ExcludedDaysOfWeek: intSliceFromJSONPtr(template.ExcludedDaysOfWeekJSON),
		StrategyLabels:     stringSliceFromJSONPtr(template.StrategyLabelsJSON),
		Items:              snapshotItems,
		ConfigSnapshotJSON: stringFromPtr(template.ConfigSnapshotJSON),
	}
}

func stringFromPtr(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func stringPtrFromNonEmpty(value string) *string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return nil
	}
	return &trimmed
}
