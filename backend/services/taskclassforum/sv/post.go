package sv

import (
	"context"
	"encoding/json"
	"errors"
	"sort"
	"strings"

	"github.com/LoveLosita/smartflow/backend/shared/respond"
	forumdao "github.com/LoveLosita/smartflow/backend/services/taskclassforum/dao"
	forummodel "github.com/LoveLosita/smartflow/backend/services/taskclassforum/model"
	forumcontracts "github.com/LoveLosita/smartflow/backend/shared/contracts/taskclassforum"
	"gorm.io/gorm"
)

// ListPosts 查询计划广场帖子列表。
//
// 职责边界：
// 1. 负责分页、排序、关键词和标签筛选的业务口径；
// 2. 负责补齐模板摘要、当前用户点赞/导入状态；
// 3. 不读取原作者当前 TaskClass，列表只基于论坛快照表。
func (s *Service) ListPosts(ctx context.Context, actorUserID uint64, page int, pageSize int, sortBy string, keyword string, tag string) ([]forumcontracts.ForumPostBrief, forumcontracts.PageResult, error) {
	if err := s.Ready(); err != nil {
		return nil, forumcontracts.PageResult{}, err
	}
	page, pageSize = normalizePage(page, pageSize)

	posts, total, err := s.forumDAO.ListPosts(ctx, forumdao.ListPostsQuery{
		Page:     page,
		PageSize: pageSize,
		Sort:     sortBy,
		Keyword:  keyword,
		Tag:      tag,
	})
	if err != nil {
		return nil, forumcontracts.PageResult{}, err
	}
	if len(posts) == 0 {
		return []forumcontracts.ForumPostBrief{}, pageResult(page, pageSize, total), nil
	}

	postIDs := collectPostIDs(posts)
	templates, err := s.forumDAO.FindTemplatesByPostIDs(ctx, postIDs)
	if err != nil {
		return nil, forumcontracts.PageResult{}, err
	}
	itemCounts, err := s.forumDAO.CountTemplateItemsByPostIDs(ctx, postIDs)
	if err != nil {
		return nil, forumcontracts.PageResult{}, err
	}
	liked, imported, err := s.viewerStateSets(ctx, actorUserID, postIDs)
	if err != nil {
		return nil, forumcontracts.PageResult{}, err
	}

	result := make([]forumcontracts.ForumPostBrief, 0, len(posts))
	for _, post := range posts {
		template, ok := templates[post.ID]
		var templatePtr *forummodel.ForumPostTemplate
		if ok {
			templateCopy := template
			templatePtr = &templateCopy
		}
		result = append(result, postBriefFromModel(post, templatePtr, itemCounts[post.ID], viewerState(post.ID, liked, imported)))
	}
	return result, pageResult(page, pageSize, total), nil
}

// ListTags 聚合计划广场已发布帖子的标签。
func (s *Service) ListTags(ctx context.Context, actorUserID uint64, limit int) ([]forumcontracts.ForumTagItem, error) {
	_ = actorUserID
	if err := s.Ready(); err != nil {
		return nil, err
	}
	if limit <= 0 || limit > 50 {
		limit = 20
	}

	rawTags, err := s.forumDAO.ListPublishedTagJSONs(ctx)
	if err != nil {
		return nil, err
	}
	counter := make(map[string]int)
	for _, raw := range rawTags {
		for _, tag := range tagsFromJSON(raw) {
			if strings.TrimSpace(tag) == "" {
				continue
			}
			counter[tag]++
		}
	}

	items := make([]forumcontracts.ForumTagItem, 0, len(counter))
	for tag, count := range counter {
		items = append(items, forumcontracts.ForumTagItem{Tag: tag, PostCount: count})
	}
	sort.SliceStable(items, func(i, j int) bool {
		if items[i].PostCount == items[j].PostCount {
			return items[i].Tag < items[j].Tag
		}
		return items[i].PostCount > items[j].PostCount
	})
	if len(items) > limit {
		items = items[:limit]
	}
	return items, nil
}

// CreatePost 发布计划，并把旧 TaskClass 复制为论坛快照。
//
// 职责边界：
// 1. 通过 TaskClassSnapshotPort 获取当前用户自己的 TaskClass 快照；
// 2. 在论坛私有表写帖子、模板和模板条目；
// 3. 不修改旧 TaskClass，也不写 schedule。
func (s *Service) CreatePost(ctx context.Context, req forumcontracts.CreateForumPostRequest) (*forumcontracts.ForumPostBrief, error) {
	if err := s.Ready(); err != nil {
		return nil, err
	}
	if req.ActorUserID == 0 || req.TaskClassID == 0 || strings.TrimSpace(req.Title) == "" {
		return nil, respond.MissingParam
	}
	if err := validateRuneMax(req.Title, maxPostTitleLen); err != nil {
		return nil, err
	}
	if err := validateRuneMax(req.Summary, maxSummaryLen); err != nil {
		return nil, err
	}
	if s.taskClassPort == nil {
		return nil, ErrTaskClassPortMissing
	}

	idempotencyKey := strings.TrimSpace(req.IdempotencyKey)
	if idempotencyKey != "" {
		existing, err := s.forumDAO.FindPostByIdempotencyKey(ctx, req.ActorUserID, idempotencyKey)
		if err != nil {
			return nil, err
		}
		if existing != nil {
			return s.postBriefByID(ctx, req.ActorUserID, existing.ID)
		}
	}

	tags, err := normalizeTags(req.Tags)
	if err != nil {
		return nil, err
	}
	tagsJSON, err := tagsToJSON(tags)
	if err != nil {
		return nil, err
	}
	snapshot, err := s.taskClassPort.GetOwnedTaskClassSnapshot(ctx, req.ActorUserID, req.TaskClassID)
	if err != nil {
		return nil, normalizeRecordNotFound(err, respond.UserTaskClassNotFound)
	}
	if snapshot == nil {
		return nil, respond.UserTaskClassNotFound
	}

	post, template, items, err := buildPostSnapshotModels(req, idempotencyKey, tagsJSON, *snapshot)
	if err != nil {
		return nil, err
	}
	if err := s.forumDAO.CreatePostSnapshot(ctx, &post, &template, items); err != nil {
		return nil, err
	}
	return s.postBriefByID(ctx, req.ActorUserID, post.ID)
}

// GetPost 查询帖子详情和模板快照。
func (s *Service) GetPost(ctx context.Context, actorUserID uint64, postID uint64) (*forumcontracts.ForumPostDetail, error) {
	if err := s.Ready(); err != nil {
		return nil, err
	}
	if postID == 0 {
		return nil, respond.MissingParam
	}

	post, template, items, err := s.loadPostTemplate(ctx, postID)
	if err != nil {
		return nil, err
	}
	liked, imported, err := s.viewerStateSets(ctx, actorUserID, []uint64{postID})
	if err != nil {
		return nil, err
	}
	state := viewerState(postID, liked, imported)
	return &forumcontracts.ForumPostDetail{
		Post:     postBriefFromModel(*post, template, len(items), state),
		Template: templateDetailFromModel(*template, items),
	}, nil
}

func (s *Service) postBriefByID(ctx context.Context, actorUserID uint64, postID uint64) (*forumcontracts.ForumPostBrief, error) {
	post, template, items, err := s.loadPostTemplate(ctx, postID)
	if err != nil {
		return nil, err
	}
	liked, imported, err := s.viewerStateSets(ctx, actorUserID, []uint64{postID})
	if err != nil {
		return nil, err
	}
	brief := postBriefFromModel(*post, template, len(items), viewerState(postID, liked, imported))
	return &brief, nil
}

func (s *Service) loadPostTemplate(ctx context.Context, postID uint64) (*forummodel.ForumPost, *forummodel.ForumPostTemplate, []forummodel.ForumPostTemplateItem, error) {
	post, err := s.forumDAO.FindPublishedPost(ctx, postID)
	if err != nil {
		return nil, nil, nil, normalizeRecordNotFound(err, respond.UserTaskClassNotFound)
	}
	template, err := s.forumDAO.FindTemplateByPostID(ctx, postID)
	if err != nil {
		return nil, nil, nil, normalizeRecordNotFound(err, respond.UserTaskClassNotFound)
	}
	items, err := s.forumDAO.ListTemplateItemsByPostID(ctx, postID)
	if err != nil {
		return nil, nil, nil, err
	}
	return post, template, items, nil
}

func (s *Service) viewerStateSets(ctx context.Context, actorUserID uint64, postIDs []uint64) (map[uint64]bool, map[uint64]bool, error) {
	if actorUserID == 0 || len(postIDs) == 0 {
		return map[uint64]bool{}, map[uint64]bool{}, nil
	}
	liked, err := s.forumDAO.LikedPostIDSet(ctx, actorUserID, postIDs)
	if err != nil {
		return nil, nil, err
	}
	imported, err := s.forumDAO.ImportedPostIDSet(ctx, actorUserID, postIDs)
	if err != nil {
		return nil, nil, err
	}
	return liked, imported, nil
}

func collectPostIDs(posts []forummodel.ForumPost) []uint64 {
	result := make([]uint64, 0, len(posts))
	for _, post := range posts {
		result = append(result, post.ID)
	}
	return result
}

func buildPostSnapshotModels(req forumcontracts.CreateForumPostRequest, idempotencyKey string, tagsJSON string, snapshot TaskClassSnapshot) (forummodel.ForumPost, forummodel.ForumPostTemplate, []forummodel.ForumPostTemplateItem, error) {
	configJSON, err := configSnapshotJSON(snapshot)
	if err != nil {
		return forummodel.ForumPost{}, forummodel.ForumPostTemplate{}, nil, err
	}
	excludedSlotsJSON, err := intSliceToJSONPtr(snapshot.ExcludedSlots)
	if err != nil {
		return forummodel.ForumPost{}, forummodel.ForumPostTemplate{}, nil, err
	}
	excludedDaysJSON, err := intSliceToJSONPtr(snapshot.ExcludedDaysOfWeek)
	if err != nil {
		return forummodel.ForumPost{}, forummodel.ForumPostTemplate{}, nil, err
	}
	labelsJSON, err := stringSliceToJSONPtr(snapshot.StrategyLabels)
	if err != nil {
		return forummodel.ForumPost{}, forummodel.ForumPostTemplate{}, nil, err
	}

	post := forummodel.ForumPost{
		AuthorUserID:      req.ActorUserID,
		SourceTaskClassID: req.TaskClassID,
		Title:             strings.TrimSpace(req.Title),
		Summary:           strings.TrimSpace(req.Summary),
		TagsJSON:          tagsJSON,
		IdempotencyKey:    stringPtrFromNonEmpty(idempotencyKey),
		Status:            forummodel.ForumPostStatusPublished,
	}
	template := forummodel.ForumPostTemplate{
		SourceTaskClassID:      snapshot.TaskClassID,
		Mode:                   snapshot.Mode,
		StartDate:              parseSnapshotDate(snapshot.StartDate),
		EndDate:                parseSnapshotDate(snapshot.EndDate),
		SubjectType:            snapshot.SubjectType,
		DifficultyLevel:        snapshot.DifficultyLevel,
		CognitiveIntensity:     snapshot.CognitiveIntensity,
		TotalSlots:             snapshot.TotalSlots,
		AllowFillerCourse:      snapshot.AllowFillerCourse,
		Strategy:               snapshot.Strategy,
		ExcludedSlotsJSON:      excludedSlotsJSON,
		ExcludedDaysOfWeekJSON: excludedDaysJSON,
		StrategyLabelsJSON:     labelsJSON,
		ConfigSnapshotJSON:     &configJSON,
	}
	snapshotItems := append([]TaskClassSnapshotItem(nil), snapshot.Items...)
	sort.SliceStable(snapshotItems, func(i, j int) bool {
		if snapshotItems[i].Order != snapshotItems[j].Order {
			return snapshotItems[i].Order < snapshotItems[j].Order
		}
		return snapshotItems[i].TaskItemID < snapshotItems[j].TaskItemID
	})
	items := make([]forummodel.ForumPostTemplateItem, 0, len(snapshotItems))
	for _, item := range snapshotItems {
		if strings.TrimSpace(item.Content) == "" {
			continue
		}
		items = append(items, forummodel.ForumPostTemplateItem{
			SourceTaskItemID: item.TaskItemID,
			Order:            len(items) + 1,
			Content:          item.Content,
		})
	}
	return post, template, items, nil
}

func configSnapshotJSON(snapshot TaskClassSnapshot) (string, error) {
	if strings.TrimSpace(snapshot.ConfigSnapshotJSON) != "" {
		return snapshot.ConfigSnapshotJSON, nil
	}
	raw, err := json.Marshal(map[string]any{
		"mode":                  snapshot.Mode,
		"start_date":            snapshot.StartDate,
		"end_date":              snapshot.EndDate,
		"subject_type":          snapshot.SubjectType,
		"difficulty_level":      snapshot.DifficultyLevel,
		"cognitive_intensity":   snapshot.CognitiveIntensity,
		"total_slots":           snapshot.TotalSlots,
		"allow_filler_course":   snapshot.AllowFillerCourse,
		"strategy":              snapshot.Strategy,
		"excluded_slots":        snapshot.ExcludedSlots,
		"excluded_days_of_week": snapshot.ExcludedDaysOfWeek,
		"strategy_labels":       snapshot.StrategyLabels,
	})
	if err != nil {
		return "", err
	}
	return string(raw), nil
}

func normalizeRecordNotFound(err error, fallback error) error {
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return fallback
	}
	return err
}
