package events

import (
	"errors"
	"fmt"
	"strings"
	"time"
)

const (
	ForumPostLikedEventType    = "forum.post.liked"
	ForumPostImportedEventType = "forum.post.imported"
	ForumRewardEventVersion    = "v1"

	ForumRewardSourceLike   = "forum_like"
	ForumRewardSourceImport = "forum_import"
)

// ForumPostRewardPayload 是计划广场作者奖励事件的统一载荷。
//
// 职责边界：
// 1. 只描述“哪个帖子因什么互动触发了作者奖励”，不直接携带最终 Token 数额；
// 2. source 负责表达奖励来源，真正的奖励规则仍由 token-store 自己解析；
// 3. event_id 必须稳定，供 outbox 重试和下游记账幂等共同使用。
type ForumPostRewardPayload struct {
	EventID              string    `json:"event_id"`
	PostID               uint64    `json:"post_id"`
	ImportID             uint64    `json:"import_id"`
	AuthorUserID         uint64    `json:"author_user_id"`
	ActorUserID          uint64    `json:"actor_user_id"`
	RewardReceiverUserID uint64    `json:"reward_receiver_user_id"`
	Source               string    `json:"source"`
	OccurredAt           time.Time `json:"occurred_at"`
}

func NewForumPostLikedPayload(postID uint64, authorUserID uint64, actorUserID uint64, occurredAt time.Time) ForumPostRewardPayload {
	return newForumPostRewardPayload(
		ForumPostLikedEventType,
		ForumRewardSourceLike,
		postID,
		0,
		authorUserID,
		actorUserID,
		occurredAt,
	)
}

func NewForumPostImportedPayload(postID uint64, importID uint64, authorUserID uint64, actorUserID uint64, occurredAt time.Time) ForumPostRewardPayload {
	return newForumPostRewardPayload(
		ForumPostImportedEventType,
		ForumRewardSourceImport,
		postID,
		importID,
		authorUserID,
		actorUserID,
		occurredAt,
	)
}

func newForumPostRewardPayload(
	eventType string,
	source string,
	postID uint64,
	importID uint64,
	authorUserID uint64,
	actorUserID uint64,
	occurredAt time.Time,
) ForumPostRewardPayload {
	if occurredAt.IsZero() {
		occurredAt = time.Now()
	}
	return ForumPostRewardPayload{
		EventID:              ForumRewardEventID(eventType, postID, actorUserID),
		PostID:               postID,
		ImportID:             importID,
		AuthorUserID:         authorUserID,
		ActorUserID:          actorUserID,
		RewardReceiverUserID: authorUserID,
		Source:               strings.TrimSpace(source),
		OccurredAt:           occurredAt,
	}
}

func ForumRewardEventID(eventType string, postID uint64, actorUserID uint64) string {
	return fmt.Sprintf("%s:%d:%d", strings.TrimSpace(eventType), postID, actorUserID)
}

// EventType 根据 source 反推出当前奖励事件类型。
func (p ForumPostRewardPayload) EventType() string {
	switch strings.TrimSpace(p.Source) {
	case ForumRewardSourceLike:
		return ForumPostLikedEventType
	case ForumRewardSourceImport:
		return ForumPostImportedEventType
	default:
		return ""
	}
}

func (p ForumPostRewardPayload) MessageKey() string {
	return strings.TrimSpace(p.EventID)
}

func (p ForumPostRewardPayload) AggregateID() string {
	return fmt.Sprintf("post:%d", p.PostID)
}

func (p ForumPostRewardPayload) Validate() error {
	if strings.TrimSpace(p.EventID) == "" {
		return errors.New("forum reward event_id 不能为空")
	}
	if strings.TrimSpace(p.EventType()) == "" {
		return errors.New("forum reward source 非法")
	}
	if p.PostID == 0 {
		return errors.New("forum reward post_id 不能为空")
	}
	if p.AuthorUserID == 0 || p.ActorUserID == 0 || p.RewardReceiverUserID == 0 {
		return errors.New("forum reward user_id 不能为空")
	}
	if strings.TrimSpace(p.Source) == ForumRewardSourceImport && p.ImportID == 0 {
		return errors.New("forum import reward import_id 不能为空")
	}
	if p.OccurredAt.IsZero() {
		return errors.New("forum reward occurred_at 不能为空")
	}
	return nil
}
