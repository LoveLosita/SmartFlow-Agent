package sv

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/LoveLosita/smartflow/backend/shared/respond"
	forumdao "github.com/LoveLosita/smartflow/backend/services/taskclassforum/dao"
	forummodel "github.com/LoveLosita/smartflow/backend/services/taskclassforum/model"
	forumcontracts "github.com/LoveLosita/smartflow/backend/shared/contracts/taskclassforum"
	sharedevents "github.com/LoveLosita/smartflow/backend/shared/events"
)

// ImportPost 从论坛模板导入当前用户自己的 TaskClass 副本。
//
// 职责边界：
// 1. 同一用户同一帖子只允许导入一次，由 forum_imports 唯一约束兜底；
// 2. 只通过 TaskClassSnapshotPort 创建 TaskClass，不写 schedule；
// 3. 只写 forum_imports 和 import_count，Token 奖励后续基于 event_id 消费。
func (s *Service) ImportPost(ctx context.Context, req forumcontracts.ImportForumPostRequest) (*forumcontracts.ImportForumPostResult, error) {
	if err := s.Ready(); err != nil {
		return nil, err
	}
	if req.ActorUserID == 0 || req.PostID == 0 {
		return nil, respond.MissingParam
	}
	if strings.TrimSpace(req.TargetTitle) != "" {
		if err := validateRuneMax(req.TargetTitle, maxImportTitle); err != nil {
			return nil, err
		}
	}
	if s.taskClassPort == nil {
		return nil, ErrTaskClassPortMissing
	}

	idempotencyKey := strings.TrimSpace(req.IdempotencyKey)
	if idempotencyKey != "" {
		existing, err := s.forumDAO.FindImportByIdempotencyKey(ctx, req.ActorUserID, idempotencyKey)
		if err != nil {
			return nil, err
		}
		if existing != nil && existing.Status == forummodel.ForumImportStatusImported {
			return s.importResultWithCurrentImportCount(ctx, *existing), nil
		}
	}
	existing, err := s.forumDAO.FindImport(ctx, req.PostID, req.ActorUserID)
	if err != nil {
		return nil, err
	}
	if existing != nil && existing.Status == forummodel.ForumImportStatusImported {
		return s.importResultWithCurrentImportCount(ctx, *existing), nil
	}
	if existing != nil && existing.Status == forummodel.ForumImportStatusFailed && existing.NewTaskClassID != nil {
		return s.recoverCreatedImport(ctx, req, *existing)
	}
	if existing != nil && existing.Status == forummodel.ForumImportStatusPending {
		return nil, respond.RequestIsProcessing
	}

	post, template, items, err := s.loadPostTemplate(ctx, req.PostID)
	if err != nil {
		return nil, err
	}
	snapshot := snapshotFromTemplate(*post, *template, items)
	targetTitle := strings.TrimSpace(req.TargetTitle)
	if targetTitle == "" {
		targetTitle = post.Title
	}

	pending, err := s.reserveImport(ctx, req, post.AuthorUserID, targetTitle, idempotencyKey)
	if err != nil {
		return nil, err
	}
	if pending.Status == forummodel.ForumImportStatusImported {
		return s.importResultWithCurrentImportCount(ctx, *pending), nil
	}

	created, err := s.taskClassPort.CreateTaskClassFromSnapshot(ctx, req.ActorUserID, snapshot, targetTitle)
	if err != nil {
		_ = s.forumDAO.MarkImportFailed(ctx, pending.ID, err.Error(), time.Now())
		return nil, err
	}
	if created == nil {
		err := respond.InternalError(fmt.Errorf("taskclass adapter returned nil created taskclass"))
		_ = s.forumDAO.MarkImportFailed(ctx, pending.ID, err.Info, time.Now())
		return nil, err
	}

	var imported forummodel.ForumImport
	var rewardPayload *sharedevents.ForumPostRewardPayload
	if err := s.forumDAO.Transaction(ctx, func(txDAO *forumdao.ForumDAO) error {
		if _, err := txDAO.LockPublishedPost(ctx, req.PostID); err != nil {
			return normalizeRecordNotFound(err, respond.UserTaskClassNotFound)
		}
		again, err := txDAO.FindImport(ctx, req.PostID, req.ActorUserID)
		if err != nil {
			return err
		}
		if again == nil || again.ID != pending.ID {
			return respond.RequestIsProcessing
		}
		if again.Status == forummodel.ForumImportStatusImported {
			imported = *again
			return nil
		}
		finalizedAt := time.Now()
		if err := txDAO.FinalizeImport(ctx, pending.ID, created.TaskClassID, created.Title, finalizedAt); err != nil {
			return err
		}
		imported = *again
		imported.NewTaskClassID = &created.TaskClassID
		imported.TargetTitle = created.Title
		imported.Status = forummodel.ForumImportStatusImported
		if again.Status != forummodel.ForumImportStatusImported {
			payload := sharedevents.NewForumPostImportedPayload(req.PostID, again.ID, again.AuthorUserID, req.ActorUserID, finalizedAt)
			if again.EventID != "" {
				payload.EventID = again.EventID
			}
			// 调用目的：导入成功和作者奖励事件必须同事务提交，避免只创建副本却永久漏发奖励。
			handled, publishErr := s.publishForumRewardEventInTx(ctx, txDAO.GormDB(), payload)
			if publishErr != nil {
				return publishErr
			}
			if !handled {
				rewardPayload = &payload
			}
			return txDAO.AddPostCounter(ctx, req.PostID, "import_count", 1)
		}
		return nil
	}); err != nil {
		_ = s.forumDAO.MarkImportFailedAfterTaskClassCreated(ctx, pending.ID, created.TaskClassID, created.Title, err.Error(), time.Now())
		return nil, err
	}
	if rewardPayload != nil {
		s.publishForumRewardEventBestEffort(*rewardPayload)
	}
	result := importResultFromModel(imported)
	if postAfter, err := s.forumDAO.FindPublishedPost(ctx, req.PostID); err == nil {
		result.ImportCount = postAfter.ImportCount
	}
	return result, nil
}

func (s *Service) reserveImport(ctx context.Context, req forumcontracts.ImportForumPostRequest, authorUserID uint64, targetTitle string, idempotencyKey string) (*forummodel.ForumImport, error) {
	var reserved *forummodel.ForumImport
	if err := s.forumDAO.Transaction(ctx, func(txDAO *forumdao.ForumDAO) error {
		if _, err := txDAO.LockPublishedPost(ctx, req.PostID); err != nil {
			return normalizeRecordNotFound(err, respond.UserTaskClassNotFound)
		}
		existing, err := txDAO.FindImport(ctx, req.PostID, req.ActorUserID)
		if err != nil {
			return err
		}
		if existing != nil {
			switch existing.Status {
			case forummodel.ForumImportStatusImported:
				reserved = existing
				return nil
			case forummodel.ForumImportStatusPending:
				return respond.RequestIsProcessing
			case forummodel.ForumImportStatusFailed:
				if existing.NewTaskClassID != nil {
					reserved = existing
					return nil
				}
				if err := txDAO.UpdateImportProcessing(ctx, existing.ID, targetTitle, time.Now()); err != nil {
					return err
				}
				existing.Status = forummodel.ForumImportStatusPending
				existing.TargetTitle = targetTitle
				reserved = existing
				return nil
			}
		}
		item := &forummodel.ForumImport{
			PostID:         req.PostID,
			UserID:         req.ActorUserID,
			AuthorUserID:   authorUserID,
			TargetTitle:    targetTitle,
			Status:         forummodel.ForumImportStatusPending,
			EventID:        forumImportEventID(req.PostID, req.ActorUserID),
			IdempotencyKey: stringPtrFromNonEmpty(idempotencyKey),
		}
		if err := txDAO.CreateImport(ctx, item); err != nil {
			return err
		}
		reserved = item
		return nil
	}); err != nil {
		return nil, err
	}
	return reserved, nil
}

func (s *Service) recoverCreatedImport(ctx context.Context, req forumcontracts.ImportForumPostRequest, existing forummodel.ForumImport) (*forumcontracts.ImportForumPostResult, error) {
	if existing.NewTaskClassID == nil {
		return nil, respond.RequestIsProcessing
	}
	imported := existing
	var rewardPayload *sharedevents.ForumPostRewardPayload
	if err := s.forumDAO.Transaction(ctx, func(txDAO *forumdao.ForumDAO) error {
		if _, err := txDAO.LockPublishedPost(ctx, req.PostID); err != nil {
			return normalizeRecordNotFound(err, respond.UserTaskClassNotFound)
		}
		again, err := txDAO.FindImport(ctx, req.PostID, req.ActorUserID)
		if err != nil {
			return err
		}
		if again == nil || again.ID != existing.ID {
			return respond.RequestIsProcessing
		}
		if again.Status == forummodel.ForumImportStatusImported {
			imported = *again
			return nil
		}
		if again.Status != forummodel.ForumImportStatusFailed || again.NewTaskClassID == nil {
			return respond.RequestIsProcessing
		}
		finalizedAt := time.Now()
		if err := txDAO.FinalizeImport(ctx, again.ID, *again.NewTaskClassID, again.TargetTitle, finalizedAt); err != nil {
			return err
		}
		imported = *again
		imported.Status = forummodel.ForumImportStatusImported
		payload := sharedevents.NewForumPostImportedPayload(req.PostID, again.ID, again.AuthorUserID, req.ActorUserID, finalizedAt)
		if again.EventID != "" {
			payload.EventID = again.EventID
		}
		// 调用目的：恢复已创建副本的导入记录时，同步补齐奖励 outbox，保证恢复路径和首次成功路径一致。
		handled, publishErr := s.publishForumRewardEventInTx(ctx, txDAO.GormDB(), payload)
		if publishErr != nil {
			return publishErr
		}
		if !handled {
			rewardPayload = &payload
		}
		return txDAO.AddPostCounter(ctx, req.PostID, "import_count", 1)
	}); err != nil {
		return nil, err
	}
	if rewardPayload != nil {
		s.publishForumRewardEventBestEffort(*rewardPayload)
	}
	result := importResultFromModel(imported)
	if postAfter, err := s.forumDAO.FindPublishedPost(ctx, req.PostID); err == nil {
		result.ImportCount = postAfter.ImportCount
	}
	return result, nil
}

func importResultFromModel(item forummodel.ForumImport) *forumcontracts.ImportForumPostResult {
	var newTaskClassID uint64
	if item.NewTaskClassID != nil {
		newTaskClassID = *item.NewTaskClassID
	}
	return &forumcontracts.ImportForumPostResult{
		ImportID:       item.ID,
		PostID:         item.PostID,
		NewTaskClassID: newTaskClassID,
		TaskClassTitle: item.TargetTitle,
		CreatedAt:      formatTime(item.CreatedAt),
	}
}

// importResultWithCurrentImportCount 复用已有导入记录时补齐帖子当前导入计数。
//
// 职责边界：
// 1. 只补齐响应展示用的 import_count，不改变 forum_imports 状态；
// 2. 查询帖子失败时保留基础导入回执，避免幂等重放因为展示字段失败而误报导入失败；
// 3. 新导入路径仍以事务内 AddPostCounter 为准，这里只处理已导入短路路径。
func (s *Service) importResultWithCurrentImportCount(ctx context.Context, item forummodel.ForumImport) *forumcontracts.ImportForumPostResult {
	result := importResultFromModel(item)
	if post, err := s.forumDAO.FindPublishedPost(ctx, item.PostID); err == nil {
		result.ImportCount = post.ImportCount
	}
	return result
}

func forumImportEventID(postID uint64, userID uint64) string {
	return sharedevents.ForumRewardEventID(sharedevents.ForumPostImportedEventType, postID, userID)
}
