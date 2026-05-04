package sv

import (
	"context"
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
// 3. 不做评论缓存，新增、回复、删除后直接读库保持语义简单。
func (s *Service) ListComments(ctx context.Context, actorUserID uint64, postID uint64, page int, pageSize int, sortBy string) ([]forumcontracts.ForumCommentNode, forumcontracts.PageResult, error) {
	if err := s.Ready(); err != nil {
		return nil, forumcontracts.PageResult{}, err
	}
	if postID == 0 {
		return nil, forumcontracts.PageResult{}, respond.MissingParam
	}
	page, pageSize = normalizePage(page, pageSize)
	if _, err := s.forumDAO.FindPublishedPost(ctx, postID); err != nil {
		return nil, forumcontracts.PageResult{}, normalizeRecordNotFound(err, respond.UserTaskClassNotFound)
	}

	total, err := s.forumDAO.CountRootComments(ctx, postID)
	if err != nil {
		return nil, forumcontracts.PageResult{}, err
	}
	roots, err := s.forumDAO.ListRootComments(ctx, postID, page, pageSize, sortBy)
	if err != nil {
		return nil, forumcontracts.PageResult{}, err
	}
	if len(roots) == 0 {
		return []forumcontracts.ForumCommentNode{}, pageResult(page, pageSize, total), nil
	}
	allComments, err := s.forumDAO.ListCommentsByPostID(ctx, postID)
	if err != nil {
		return nil, forumcontracts.PageResult{}, err
	}
	nodes := commenttree.BuildForumCommentTree(filterCommentsForRoots(allComments, roots), actorUserID)
	return nodes, pageResult(page, pageSize, total), nil
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
		deletedAt = formatTimePtr(&now)
		return nil
	}); err != nil {
		return nil, err
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
