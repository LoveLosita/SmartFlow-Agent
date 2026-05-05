package sv

import (
	"context"
	"log"
	"strings"
	"time"

	"github.com/LoveLosita/smartflow/backend/respond"
	"github.com/LoveLosita/smartflow/backend/services/taskclassforum/commenttree"
	forumdao "github.com/LoveLosita/smartflow/backend/services/taskclassforum/dao"
	forummodel "github.com/LoveLosita/smartflow/backend/services/taskclassforum/model"
	forumcontracts "github.com/LoveLosita/smartflow/backend/shared/contracts/taskclassforum"
)

// ListComments 查询评论树。
//
// 职责边界：
// 1. P0 按根评论分页，避免一次把超大评论区全部暴露给前端；
// 2. 数据库存储仍是扁平 parent_comment_id，树结构由 commenttree 包组装；
// 3. 采用 cache-aside 缓存去个性化评论树，返回前再补当前用户的删除权限。
func (s *Service) ListComments(ctx context.Context, actorUserID uint64, postID uint64, page int, pageSize int, sortBy string) ([]forumcontracts.ForumCommentNode, forumcontracts.PageResult, error) {
	if err := s.Ready(); err != nil {
		return nil, forumcontracts.PageResult{}, err
	}
	if postID == 0 {
		return nil, forumcontracts.PageResult{}, respond.MissingParam
	}
	page, pageSize = normalizePage(page, pageSize)
	sortBy = normalizeCommentSort(sortBy)
	if _, err := s.forumDAO.FindPublishedPost(ctx, postID); err != nil {
		return nil, forumcontracts.PageResult{}, normalizeRecordNotFound(err, respond.UserTaskClassNotFound)
	}

	if cachedItems, cachedPage, hit := s.getCommentTreeCacheBestEffort(ctx, postID, page, pageSize, sortBy); hit {
		return personalizeCommentNodesForActor(cachedItems, actorUserID), cachedPage, nil
	}

	total, err := s.forumDAO.CountRootComments(ctx, postID)
	if err != nil {
		return nil, forumcontracts.PageResult{}, err
	}
	roots, err := s.forumDAO.ListRootComments(ctx, postID, page, pageSize, sortBy)
	if err != nil {
		return nil, forumcontracts.PageResult{}, err
	}
	resultPage := pageResult(page, pageSize, total)
	if len(roots) == 0 {
		emptyItems := []forumcontracts.ForumCommentNode{}
		s.setCommentTreeCacheBestEffort(ctx, postID, page, pageSize, sortBy, emptyItems, resultPage)
		return emptyItems, resultPage, nil
	}
	allComments, err := s.forumDAO.ListCommentsByPostID(ctx, postID)
	if err != nil {
		return nil, forumcontracts.PageResult{}, err
	}
	sharedNodes := commenttree.BuildForumCommentTree(filterCommentsForRoots(allComments, roots), 0)
	s.setCommentTreeCacheBestEffort(ctx, postID, page, pageSize, sortBy, sharedNodes, resultPage)
	return personalizeCommentNodesForActor(sharedNodes, actorUserID), resultPage, nil
}

// CreateComment 创建帖子评论或多层回复。
func (s *Service) CreateComment(ctx context.Context, req forumcontracts.CreateForumCommentRequest) (*forumcontracts.ForumCommentNode, error) {
	if err := s.Ready(); err != nil {
		return nil, err
	}
	if req.ActorUserID == 0 || req.PostID == 0 || strings.TrimSpace(req.Content) == "" {
		return nil, respond.MissingParam
	}
	if err := validateRuneMax(req.Content, maxCommentLen); err != nil {
		return nil, err
	}
	idempotencyKey := strings.TrimSpace(req.IdempotencyKey)
	if idempotencyKey != "" {
		existing, err := s.forumDAO.FindCommentByIdempotencyKey(ctx, req.ActorUserID, idempotencyKey)
		if err != nil {
			return nil, err
		}
		if existing != nil {
			return commentModelToNode(*existing, req.ActorUserID), nil
		}
	}

	var created forummodel.ForumComment
	if err := s.forumDAO.Transaction(ctx, func(txDAO *forumdao.ForumDAO) error {
		if _, err := txDAO.LockPublishedPost(ctx, req.PostID); err != nil {
			return normalizeRecordNotFound(err, respond.UserTaskClassNotFound)
		}
		if req.ParentCommentID != nil {
			parent, err := txDAO.FindCommentByID(ctx, *req.ParentCommentID)
			if err != nil {
				return normalizeRecordNotFound(err, respond.MissingParam)
			}
			if parent.PostID != req.PostID {
				return respond.MissingParam
			}
		}
		created = forummodel.ForumComment{
			PostID:          req.PostID,
			ParentCommentID: req.ParentCommentID,
			UserID:          req.ActorUserID,
			Content:         strings.TrimSpace(req.Content),
			Status:          forummodel.ForumCommentStatusVisible,
			IdempotencyKey:  stringPtrFromNonEmpty(idempotencyKey),
		}
		if err := txDAO.CreateComment(ctx, &created); err != nil {
			return err
		}
		return txDAO.AddPostCounter(ctx, req.PostID, "comment_count", 1)
	}); err != nil {
		return nil, err
	}
	s.bumpCommentTreeVersionBestEffort(req.PostID)
	return commentModelToNode(created, req.ActorUserID), nil
}

// DeleteComment 软删除当前用户自己的评论。
func (s *Service) DeleteComment(ctx context.Context, actorUserID uint64, commentID uint64) (*forumcontracts.DeleteForumCommentResult, error) {
	if err := s.Ready(); err != nil {
		return nil, err
	}
	if actorUserID == 0 || commentID == 0 {
		return nil, respond.MissingParam
	}

	var deletedAt *string
	var changedPostID uint64
	status := forummodel.ForumCommentStatusDeleted
	if err := s.forumDAO.Transaction(ctx, func(txDAO *forumdao.ForumDAO) error {
		comment, err := txDAO.LockCommentByID(ctx, commentID)
		if err != nil {
			return normalizeRecordNotFound(err, respond.MissingParam)
		}
		if comment.UserID != actorUserID {
			return respond.ErrUnauthorized
		}
		if comment.Status == forummodel.ForumCommentStatusDeleted {
			deletedAt = formatTimePtr(comment.DeletedAt)
			return nil
		}
		now := time.Now()
		if err := txDAO.SoftDeleteComment(ctx, commentID, now); err != nil {
			return err
		}
		if err := txDAO.AddPostCounter(ctx, comment.PostID, "comment_count", -1); err != nil {
			return err
		}
		changedPostID = comment.PostID
		deletedAt = formatTimePtr(&now)
		return nil
	}); err != nil {
		return nil, err
	}
	if changedPostID != 0 {
		s.bumpCommentTreeVersionBestEffort(changedPostID)
	}
	return &forumcontracts.DeleteForumCommentResult{
		CommentID: commentID,
		Status:    status,
		Content:   "",
		DeletedAt: deletedAt,
	}, nil
}

func commentModelToNode(comment forummodel.ForumComment, actorUserID uint64) *forumcontracts.ForumCommentNode {
	content := comment.Content
	if comment.Status == forummodel.ForumCommentStatusDeleted {
		content = "该评论已删除"
	}
	return &forumcontracts.ForumCommentNode{
		CommentID:       comment.ID,
		PostID:          comment.PostID,
		ParentCommentID: comment.ParentCommentID,
		Content:         content,
		Status:          comment.Status,
		Author:          userBrief(comment.UserID),
		CanDelete:       comment.Status == forummodel.ForumCommentStatusVisible && comment.UserID == actorUserID,
		CreatedAt:       formatTime(comment.CreatedAt),
		DeletedAt:       formatTimePtr(comment.DeletedAt),
		Children:        []forumcontracts.ForumCommentNode{},
	}
}

func filterCommentsForRoots(allComments []forummodel.ForumComment, roots []forummodel.ForumComment) []forummodel.ForumComment {
	filtered := make([]forummodel.ForumComment, 0, len(allComments))
	included := make(map[uint64]struct{}, len(allComments))
	for _, root := range roots {
		filtered = append(filtered, root)
		included[root.ID] = struct{}{}
	}
	candidateSet := make(map[uint64]struct{}, len(allComments))
	for _, root := range roots {
		collectDescendantCommentIDs(root.ID, allComments, candidateSet)
	}
	for _, comment := range allComments {
		if _, ok := included[comment.ID]; ok {
			continue
		}
		if _, ok := candidateSet[comment.ID]; ok {
			filtered = append(filtered, comment)
			included[comment.ID] = struct{}{}
		}
	}
	return filtered
}

func collectDescendantCommentIDs(parentID uint64, comments []forummodel.ForumComment, result map[uint64]struct{}) {
	for _, comment := range comments {
		if comment.ParentCommentID == nil || *comment.ParentCommentID != parentID {
			continue
		}
		if _, exists := result[comment.ID]; exists {
			continue
		}
		result[comment.ID] = struct{}{}
		collectDescendantCommentIDs(comment.ID, comments, result)
	}
}

func normalizeCommentSort(sortBy string) string {
	if strings.TrimSpace(sortBy) == "latest" {
		return "latest"
	}
	return "oldest"
}

func (s *Service) getCommentTreeCacheBestEffort(ctx context.Context, postID uint64, page int, pageSize int, sortBy string) ([]forumcontracts.ForumCommentNode, forumcontracts.PageResult, bool) {
	if s == nil || s.commentTreeCache == nil {
		return nil, forumcontracts.PageResult{}, false
	}
	items, resultPage, hit, err := s.commentTreeCache.GetCommentTree(ctx, postID, page, pageSize, sortBy)
	if err != nil {
		log.Printf("评论树缓存读取失败，已降级回源 DB post_id=%d page=%d page_size=%d sort=%s err=%v", postID, page, pageSize, sortBy, err)
		return nil, forumcontracts.PageResult{}, false
	}
	return items, resultPage, hit
}

func (s *Service) setCommentTreeCacheBestEffort(ctx context.Context, postID uint64, page int, pageSize int, sortBy string, items []forumcontracts.ForumCommentNode, resultPage forumcontracts.PageResult) {
	if s == nil || s.commentTreeCache == nil {
		return
	}
	if err := s.commentTreeCache.SetCommentTree(ctx, postID, page, pageSize, sortBy, items, resultPage); err != nil {
		log.Printf("评论树缓存写入失败，已保持 DB 结果返回 post_id=%d page=%d page_size=%d sort=%s err=%v", postID, page, pageSize, sortBy, err)
	}
}

func (s *Service) bumpCommentTreeVersionBestEffort(postID uint64) {
	if s == nil || s.commentTreeCache == nil || postID == 0 {
		return
	}

	// 1. 写库事务已经成功，缓存失效不应再反向影响评论发布/删除结果。
	// 2. 使用独立短超时 context，避免客户端取消请求后漏掉版本递增。
	// 3. 失败时只记录日志，旧缓存依靠短 TTL 自然过期作为兜底。
	cacheCtx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()
	if err := s.commentTreeCache.BumpCommentTreeVersion(cacheCtx, postID); err != nil {
		log.Printf("评论树缓存版本递增失败，等待短 TTL 自然过期 post_id=%d err=%v", postID, err)
	}
}

func personalizeCommentNodesForActor(nodes []forumcontracts.ForumCommentNode, actorUserID uint64) []forumcontracts.ForumCommentNode {
	if nodes == nil {
		return []forumcontracts.ForumCommentNode{}
	}
	result := make([]forumcontracts.ForumCommentNode, 0, len(nodes))
	for _, node := range nodes {
		result = append(result, personalizeCommentNodeForActor(node, actorUserID))
	}
	return result
}

func personalizeCommentNodeForActor(node forumcontracts.ForumCommentNode, actorUserID uint64) forumcontracts.ForumCommentNode {
	children := make([]forumcontracts.ForumCommentNode, 0, len(node.Children))
	for _, child := range node.Children {
		children = append(children, personalizeCommentNodeForActor(child, actorUserID))
	}
	node.Children = children
	node.CanDelete = actorUserID != 0 &&
		node.Author.UserID == actorUserID &&
		node.Status == forummodel.ForumCommentStatusVisible
	return node
}
