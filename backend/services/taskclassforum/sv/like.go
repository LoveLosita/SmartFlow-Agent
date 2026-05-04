package sv

import (
	"context"
	"fmt"
	"time"

	"github.com/LoveLosita/smartflow/backend/respond"
	forumdao "github.com/LoveLosita/smartflow/backend/services/taskclassforum/dao"
	forummodel "github.com/LoveLosita/smartflow/backend/services/taskclassforum/model"
	forumcontracts "github.com/LoveLosita/smartflow/backend/shared/contracts/taskclassforum"
)

// LikePost 点赞计划帖子。
//
// 职责边界：
// 1. 负责保证同一用户同一帖子只有一个 active 点赞状态；
// 2. 负责维护帖子 like_count 计数字段；
// 3. 不直接发放 Token，只写稳定 event_id，后续奖励链路可基于该 ID 幂等消费。
func (s *Service) LikePost(ctx context.Context, actorUserID uint64, postID uint64) (forumcontracts.ForumPostCounters, forumcontracts.ForumPostViewerState, error) {
	if err := s.Ready(); err != nil {
		return forumcontracts.ForumPostCounters{}, forumcontracts.ForumPostViewerState{}, err
	}
	if actorUserID == 0 || postID == 0 {
		return forumcontracts.ForumPostCounters{}, forumcontracts.ForumPostViewerState{}, respond.MissingParam
	}

	if err := s.forumDAO.Transaction(ctx, func(txDAO *forumdao.ForumDAO) error {
		post, err := txDAO.LockPublishedPost(ctx, postID)
		if err != nil {
			return normalizeRecordNotFound(err, respond.UserTaskClassNotFound)
		}
		like, err := txDAO.FindLike(ctx, postID, actorUserID)
		if err != nil {
			return err
		}
		if like == nil {
			return createActiveLike(ctx, txDAO, post, actorUserID)
		}
		if like.Status == forummodel.ForumLikeStatusActive {
			return nil
		}
		if err := txDAO.ActivateLike(ctx, like.ID); err != nil {
			return err
		}
		return txDAO.AddPostCounter(ctx, postID, "like_count", 1)
	}); err != nil {
		return forumcontracts.ForumPostCounters{}, forumcontracts.ForumPostViewerState{}, err
	}
	return s.postInteractionState(ctx, actorUserID, postID)
}

// UnlikePost 取消计划帖子点赞。
func (s *Service) UnlikePost(ctx context.Context, actorUserID uint64, postID uint64) (forumcontracts.ForumPostCounters, forumcontracts.ForumPostViewerState, error) {
	if err := s.Ready(); err != nil {
		return forumcontracts.ForumPostCounters{}, forumcontracts.ForumPostViewerState{}, err
	}
	if actorUserID == 0 || postID == 0 {
		return forumcontracts.ForumPostCounters{}, forumcontracts.ForumPostViewerState{}, respond.MissingParam
	}

	if err := s.forumDAO.Transaction(ctx, func(txDAO *forumdao.ForumDAO) error {
		if _, err := txDAO.LockPublishedPost(ctx, postID); err != nil {
			return normalizeRecordNotFound(err, respond.UserTaskClassNotFound)
		}
		like, err := txDAO.FindLike(ctx, postID, actorUserID)
		if err != nil {
			return err
		}
		if like == nil || like.Status != forummodel.ForumLikeStatusActive {
			return nil
		}
		if err := txDAO.CancelLike(ctx, like.ID, time.Now()); err != nil {
			return err
		}
		return txDAO.AddPostCounter(ctx, postID, "like_count", -1)
	}); err != nil {
		return forumcontracts.ForumPostCounters{}, forumcontracts.ForumPostViewerState{}, err
	}
	return s.postInteractionState(ctx, actorUserID, postID)
}

func createActiveLike(ctx context.Context, txDAO *forumdao.ForumDAO, post *forummodel.ForumPost, actorUserID uint64) error {
	like := &forummodel.ForumLike{
		PostID:       post.ID,
		UserID:       actorUserID,
		AuthorUserID: post.AuthorUserID,
		Status:       forummodel.ForumLikeStatusActive,
		EventID:      forumLikeEventID(post.ID, actorUserID),
	}
	if err := txDAO.CreateLike(ctx, like); err != nil {
		return err
	}
	return txDAO.AddPostCounter(ctx, post.ID, "like_count", 1)
}

func (s *Service) postInteractionState(ctx context.Context, actorUserID uint64, postID uint64) (forumcontracts.ForumPostCounters, forumcontracts.ForumPostViewerState, error) {
	post, err := s.forumDAO.FindPublishedPost(ctx, postID)
	if err != nil {
		return forumcontracts.ForumPostCounters{}, forumcontracts.ForumPostViewerState{}, normalizeRecordNotFound(err, respond.UserTaskClassNotFound)
	}
	liked, imported, err := s.viewerStateSets(ctx, actorUserID, []uint64{postID})
	if err != nil {
		return forumcontracts.ForumPostCounters{}, forumcontracts.ForumPostViewerState{}, err
	}
	return countersFromPost(*post), viewerState(postID, liked, imported), nil
}

func forumLikeEventID(postID uint64, userID uint64) string {
	return fmt.Sprintf("forum.post.liked:%d:%d", postID, userID)
}
