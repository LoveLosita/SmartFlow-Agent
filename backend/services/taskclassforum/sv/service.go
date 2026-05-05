package sv

import (
	"context"
	"errors"
	"log"
	"strings"
	"time"

	outboxinfra "github.com/LoveLosita/smartflow/backend/shared/infra/outbox"
	forumdao "github.com/LoveLosita/smartflow/backend/services/taskclassforum/dao"
	forumcontracts "github.com/LoveLosita/smartflow/backend/shared/contracts/taskclassforum"
	sharedevents "github.com/LoveLosita/smartflow/backend/shared/events"
	"gorm.io/gorm"
)

const forumRewardPublishTimeout = 800 * time.Millisecond

type transactionalEventPublisher interface {
	PublishWithTx(ctx context.Context, tx *gorm.DB, req outboxinfra.PublishRequest) error
}

// CommentTreeCachePort 是计划广场评论树缓存端口。
//
// 职责边界：
// 1. 只暴露“读分页树、写分页树、递增版本”三个能力，避免 service 依赖 Redis 细节；
// 2. 缓存内容必须是去个性化读模型，不能带入当前用户的 can_delete；
// 3. Redis 异常不应影响主链路，service 层会降级回源 DB。
type CommentTreeCachePort interface {
	GetCommentTree(ctx context.Context, postID uint64, page int, pageSize int, sort string) ([]forumcontracts.ForumCommentNode, forumcontracts.PageResult, bool, error)
	SetCommentTree(ctx context.Context, postID uint64, page int, pageSize int, sort string, items []forumcontracts.ForumCommentNode, pageResult forumcontracts.PageResult) error
	BumpCommentTreeVersion(ctx context.Context, postID uint64) error
}

// TaskClassSnapshotPort 是计划广场读取和写入 TaskClass 快照的端口。
//
// 职责边界：
// 1. P0 先由 legacy adapter 适配旧 TaskClass DAO / Service；
// 2. 业务层只依赖快照语义，不关心底层来自旧表、旧服务还是后续 RPC；
// 3. 不负责写 schedule，一键导入只创建当前用户自己的 TaskClass 副本。
type TaskClassSnapshotPort interface {
	GetOwnedTaskClassSnapshot(ctx context.Context, userID uint64, taskClassID uint64) (*TaskClassSnapshot, error)
	CreateTaskClassFromSnapshot(ctx context.Context, userID uint64, snapshot TaskClassSnapshot, targetTitle string) (*CreatedTaskClass, error)
}

// TaskClassSnapshot 是可分享的 TaskClass 白名单快照。
//
// 注意：这里刻意不包含 embedded_time、schedule 绑定和用户私有排程状态。
type TaskClassSnapshot struct {
	TaskClassID        uint64
	Title              string
	Mode               string
	StartDate          string
	EndDate            string
	SubjectType        string
	DifficultyLevel    string
	CognitiveIntensity string
	TotalSlots         int
	AllowFillerCourse  bool
	Strategy           string
	ExcludedSlots      []int
	ExcludedDaysOfWeek []int
	StrategyLabels     []string
	Items              []TaskClassSnapshotItem
	ConfigSnapshotJSON string
}

// TaskClassSnapshotItem 是 TaskClassItem 的可分享条目快照。
type TaskClassSnapshotItem struct {
	TaskItemID uint64
	Order      int
	Content    string
}

// CreatedTaskClass 是导入后创建出的当前用户 TaskClass。
type CreatedTaskClass struct {
	TaskClassID uint64
	Title       string
}

// Options 是计划广场服务的依赖注入参数。
type Options struct {
	DB               *gorm.DB
	TaskClassPort    TaskClassSnapshotPort
	EventPublisher   outboxinfra.EventPublisher
	CommentTreeCache CommentTreeCachePort
}

// Service 承载计划广场服务内部业务编排。
//
// 职责边界：
// 1. 负责帖子、模板快照、点赞、评论、导入记录的事务编排；
// 2. 不负责 HTTP 参数绑定，也不直接返回 respond.Response；
// 3. 不持有 TaskClass 原表，只通过 TaskClassSnapshotPort 读取和创建副本。
type Service struct {
	db               *gorm.DB
	forumDAO         *forumdao.ForumDAO
	taskClassPort    TaskClassSnapshotPort
	eventPublisher   outboxinfra.EventPublisher
	commentTreeCache CommentTreeCachePort
}

func New(opts Options) *Service {
	return &Service{
		db:               opts.DB,
		forumDAO:         forumdao.NewForumDAO(opts.DB),
		taskClassPort:    opts.TaskClassPort,
		eventPublisher:   opts.EventPublisher,
		commentTreeCache: opts.CommentTreeCache,
	}
}

// Ready 用于第二步骨架阶段的依赖检查。
//
// 后续实现真实用例时，具体方法会做更细的参数校验；这里只先帮助 cmd / 测试快速发现依赖未注入。
func (s *Service) Ready() error {
	if s == nil {
		return errors.New("taskclassforum service is nil")
	}
	if s.db == nil {
		return errors.New("taskclassforum db is nil")
	}
	return nil
}

// publishForumRewardEventBestEffort 在主事务成功后补发论坛奖励 outbox 事件。
//
// 职责边界：
// 1. 这里只处理“事务已经成功提交后的补发”，不再回头影响点赞/导入接口的成功结果；
// 2. 改用独立短超时 context，避免客户端断开直接打断补发，也避免 outbox 写入长时间拖慢接口尾部；
// 3. 发布失败时只记日志不返回 error，这是 P0 的明确取舍：先保住主链路，再靠日志和稳定 event_id 排障/补偿。
func (s *Service) publishForumRewardEventBestEffort(payload sharedevents.ForumPostRewardPayload) {
	if s == nil || s.eventPublisher == nil {
		return
	}
	if err := payload.Validate(); err != nil {
		log.Printf(
			"forum reward outbox payload 非法，跳过发布: event_id=%s post_id=%d import_id=%d source=%s err=%v",
			payload.EventID,
			payload.PostID,
			payload.ImportID,
			payload.Source,
			err,
		)
		return
	}

	eventType := strings.TrimSpace(payload.EventType())
	if eventType == "" {
		log.Printf(
			"forum reward outbox 事件类型为空，跳过发布: event_id=%s post_id=%d import_id=%d source=%s",
			payload.EventID,
			payload.PostID,
			payload.ImportID,
			payload.Source,
		)
		return
	}

	publishCtx, cancel := context.WithTimeout(context.Background(), forumRewardPublishTimeout)
	defer cancel()

	if err := s.eventPublisher.Publish(publishCtx, outboxinfra.PublishRequest{
		EventType:    eventType,
		EventVersion: sharedevents.ForumRewardEventVersion,
		MessageKey:   payload.MessageKey(),
		AggregateID:  payload.AggregateID(),
		EventID:      payload.EventID,
		Payload:      payload,
	}); err != nil {
		log.Printf(
			"forum reward outbox 发布失败，按 P0 约定忽略主链路错误: event_type=%s event_id=%s post_id=%d import_id=%d actor_user_id=%d err=%v",
			eventType,
			payload.EventID,
			payload.PostID,
			payload.ImportID,
			payload.ActorUserID,
			err,
		)
	}
}

// publishForumRewardEventInTx 尝试把论坛奖励事件写进当前业务事务。
//
// 返回值说明：
// 1. handled=true 表示发布器支持事务写入，调用方不需要再做事务后 best-effort 补发；
// 2. handled=false 表示当前发布器不支持事务写入，调用方可退回旧的事务后补发路径；
// 3. error 非空表示 outbox 入队失败，业务事务应一起回滚，避免成功互动永久漏奖。
func (s *Service) publishForumRewardEventInTx(ctx context.Context, tx *gorm.DB, payload sharedevents.ForumPostRewardPayload) (bool, error) {
	if s == nil || s.eventPublisher == nil {
		return false, nil
	}
	publisher, ok := s.eventPublisher.(transactionalEventPublisher)
	if !ok {
		return false, nil
	}
	if err := payload.Validate(); err != nil {
		return true, err
	}

	eventType := strings.TrimSpace(payload.EventType())
	if eventType == "" {
		return true, errors.New("论坛奖励事件类型为空")
	}

	return true, publisher.PublishWithTx(ctx, tx, outboxinfra.PublishRequest{
		EventType:    eventType,
		EventVersion: sharedevents.ForumRewardEventVersion,
		MessageKey:   payload.MessageKey(),
		AggregateID:  payload.AggregateID(),
		EventID:      payload.EventID,
		Payload:      payload,
	})
}
