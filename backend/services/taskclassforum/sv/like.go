package sv

import (
	"context"
	"time"

	"github.com/LoveLosita/smartflow/backend/respond"
	forumdao "github.com/LoveLosita/smartflow/backend/services/taskclassforum/dao"
	forummodel "github.com/LoveLosita/smartflow/backend/services/taskclassforum/model"
	forumcontracts "github.com/LoveLosita/smartflow/backend/shared/contracts/taskclassforum"
	sharedevents "github.com/LoveLosita/smartflow/backend/shared/events"
)

// LikePost 点赞计划帖子。
//
// 职责边界：
// 1. 保证同一用户同一帖子只有一个 active 点赞状态；
// 2. 维护帖子 like_count 计数字段；
// 3. 只在首次创建 like 记录时补发 outbox 事件，取消后重新激活旧记录不重复发奖励。
func (s *Service) LikePost(ctx context.Context, actorUserID uint64, postID uint64) (forumcontracts.ForumPostCounters, forumcontracts.ForumPostViewerState, error) {
	if err := s.Ready(); err != nil {
		return forumcontracts.ForumPostCounters{}, forumcontracts.ForumPostViewerState{}, err
	}
	if actorUserID == 0 || postID == 0 {
		return forumcontracts.ForumPostCounters{}, forumcontracts.ForumPostViewerState{}, respond.MissingParam
	}

	var rewardPayload *sharedevents.ForumPostRewardPayload
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
			payload, createErr := createActiveLike(ctx, txDAO, post, actorUserID)
			if createErr != nil {
				return createErr
			}
			// 调用目的：优先把首次点赞奖励事件写入当前事务，保证点赞记录和 outbox 入队原子提交。
			handled, publishErr := s.publishForumRewardEventInTx(ctx, txDAO.GormDB(), payload)
			if publishErr != nil {
				return publishErr
			}
			if !handled {
				rewardPayload = &payload
			}
			return nil
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

	if rewardPayload != nil {
		s.publishForumRewardEventBestEffort(*rewardPayload)
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

func createActiveLike(ctx context.Context, txDAO *forumdao.ForumDAO, post *forummodel.ForumPost, actorUserID uint64) (sharedevents.ForumPostRewardPayload, error) {
	like := &forummodel.ForumLike{
		PostID:       post.ID,
		UserID:       actorUserID,
		AuthorUserID: post.AuthorUserID,
		Status:       forummodel.ForumLikeStatusActive,
		EventID:      forumLikeEventID(post.ID, actorUserID),
	}
	if err := txDAO.CreateLike(ctx, like); err != nil {
		return sharedevents.ForumPostRewardPayload{}, err
	}
	if err := txDAO.AddPostCounter(ctx, post.ID, "like_count", 1); err != nil {
		return sharedevents.ForumPostRewardPayload{}, err
	}

	likedAt := like.LikedAt
	if likedAt.IsZero() {
		likedAt = time.Now()
	}
	payload := sharedevents.NewForumPostLikedPayload(post.ID, post.AuthorUserID, actorUserID, likedAt)
	if like.EventID != "" {
		payload.EventID = like.EventID
	}
	return payload, nil
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
	return sharedevents.ForumRewardEventID(sharedevents.ForumPostLikedEventType, postID, userID)
}
