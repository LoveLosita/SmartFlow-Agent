package sv

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	forummodel "github.com/LoveLosita/smartflow/backend/services/taskclassforum/model"
	forumcontracts "github.com/LoveLosita/smartflow/backend/shared/contracts/taskclassforum"
	"github.com/LoveLosita/smartflow/backend/shared/respond"
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

var defaultForumTags = []string{
	"期末复习",
	"考研备考",
	"四六级",
	"编程学习",
	"实习求职",
	"习惯养成",
	"竞赛项目",
	"证书考试",
}

func buildForumTagItems(rawTags []string, limit int) []forumcontracts.ForumTagItem {
	if limit <= 0 || limit > 50 {
		limit = 20
	}

	// 1. 先聚合真实帖子里的标签热度，保证已有社区语义优先展示。
	// 2. 空白标签直接忽略，避免把脏数据继续透给前端。
	// 3. 默认标签只在缺失时兜底补齐，保证清库后分类区仍可用。
	counter := make(map[string]int)
	for _, raw := range rawTags {
		for _, tag := range tagsFromJSON(raw) {
			trimmedTag := strings.TrimSpace(tag)
			if trimmedTag == "" {
				continue
			}
			counter[trimmedTag]++
		}
	}

	items := make([]forumcontracts.ForumTagItem, 0, len(counter)+len(defaultForumTags))
	for tag, count := range counter {
		items = append(items, forumcontracts.ForumTagItem{Tag: tag, PostCount: count})
	}
	sort.SliceStable(items, func(i, j int) bool {
		if items[i].PostCount == items[j].PostCount {
			return items[i].Tag < items[j].Tag
		}
		return items[i].PostCount > items[j].PostCount
	})

	seen := make(map[string]struct{}, len(items))
	for _, item := range items {
		seen[item.Tag] = struct{}{}
	}
	for _, tag := range defaultForumTags {
		if _, exists := seen[tag]; exists {
			continue
		}
		items = append(items, forumcontracts.ForumTagItem{
			Tag:       tag,
			PostCount: 0,
		})
	}
	if len(items) > limit {
		return items[:limit]
	}
	return items
}

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

// normalizeRequiredTags 负责统一“帖子标签去重 + 必填”规则。
//
// 职责边界：
// 1. 负责清洗空白、去重、数量和长度限制；
// 2. 额外补上“发布帖子至少一个标签”的业务校验；
// 3. 不负责前端提示文案，调用方只消费 error。
func normalizeRequiredTags(tags []string) ([]string, error) {
	normalizedTags, err := normalizeTags(tags)
	if err != nil {
		return nil, err
	}
	if len(normalizedTags) == 0 {
		return nil, ErrForumTagsRequired
	}
	return normalizedTags, nil
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
