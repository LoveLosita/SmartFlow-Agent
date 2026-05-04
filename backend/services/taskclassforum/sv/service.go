package sv

import (
	"context"
	"errors"

	forumcontracts "github.com/LoveLosita/smartflow/backend/shared/contracts/taskclassforum"
	"gorm.io/gorm"
)

// ErrNotImplemented 表示 RPC 骨架已接线，但对应业务用例还在后续步骤实现。
var ErrNotImplemented = errors.New("taskclassforum service method not implemented")

// TaskClassSnapshotPort 是计划广场读取和写入 TaskClass 的端口。
//
// 职责边界：
// 1. P0 由 legacy adapter 适配旧 TaskClass DAO / Service；
// 2. 业务层只依赖快照语义，不关心底层是旧表、旧服务还是后续 RPC；
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
	DB            *gorm.DB
	TaskClassPort TaskClassSnapshotPort
}

// Service 承载计划广场服务内部业务编排。
//
// 职责边界：
// 1. 负责帖子、模板快照、点赞、评论、导入记录的事务编排；
// 2. 不负责 HTTP 参数绑定，也不直接返回 respond.Response；
// 3. 不拥有 TaskClass 原表，只通过 TaskClassSnapshotPort 读取和创建副本。
type Service struct {
	db            *gorm.DB
	taskClassPort TaskClassSnapshotPort
}

func New(opts Options) *Service {
	return &Service{
		db:            opts.DB,
		taskClassPort: opts.TaskClassPort,
	}
}

// Ready 用于第二步骨架阶段的依赖检查。
//
// 后续实现真实用例时，具体方法会做更细的参数校验；这里先帮助 cmd / 测试快速发现依赖未注入。
func (s *Service) Ready() error {
	if s == nil {
		return errors.New("taskclassforum service is nil")
	}
	if s.db == nil {
		return errors.New("taskclassforum db is nil")
	}
	return nil
}

// ListPosts 是计划列表用例占位，第三步实现真实查询。
func (s *Service) ListPosts(ctx context.Context, actorUserID uint64, page int, pageSize int, sort string, keyword string, tag string) ([]forumcontracts.ForumPostBrief, forumcontracts.PageResult, error) {
	_ = ctx
	_ = actorUserID
	_ = page
	_ = pageSize
	_ = sort
	_ = keyword
	_ = tag
	return nil, forumcontracts.PageResult{}, ErrNotImplemented
}

// ListTags 是标签列表用例占位，第三步实现真实聚合查询。
func (s *Service) ListTags(ctx context.Context, actorUserID uint64, limit int) ([]forumcontracts.ForumTagItem, error) {
	_ = ctx
	_ = actorUserID
	_ = limit
	return nil, ErrNotImplemented
}

// CreatePost 是发布计划用例占位，第三步会通过 TaskClassSnapshotPort 读取旧计划快照。
func (s *Service) CreatePost(ctx context.Context, req forumcontracts.CreateForumPostRequest) (*forumcontracts.ForumPostBrief, error) {
	_ = ctx
	_ = req
	return nil, ErrNotImplemented
}

// GetPost 是计划详情用例占位，第三步实现帖子和模板快照读取。
func (s *Service) GetPost(ctx context.Context, actorUserID uint64, postID uint64) (*forumcontracts.ForumPostDetail, error) {
	_ = ctx
	_ = actorUserID
	_ = postID
	return nil, ErrNotImplemented
}

// LikePost 是点赞用例占位，第三步实现唯一约束和计数更新。
func (s *Service) LikePost(ctx context.Context, actorUserID uint64, postID uint64) (forumcontracts.ForumPostCounters, forumcontracts.ForumPostViewerState, error) {
	_ = ctx
	_ = actorUserID
	_ = postID
	return forumcontracts.ForumPostCounters{}, forumcontracts.ForumPostViewerState{}, ErrNotImplemented
}

// UnlikePost 是取消点赞用例占位，第三步实现幂等撤销。
func (s *Service) UnlikePost(ctx context.Context, actorUserID uint64, postID uint64) (forumcontracts.ForumPostCounters, forumcontracts.ForumPostViewerState, error) {
	_ = ctx
	_ = actorUserID
	_ = postID
	return forumcontracts.ForumPostCounters{}, forumcontracts.ForumPostViewerState{}, ErrNotImplemented
}

// ListComments 是评论树查询用例占位，第三步实现根评论分页和服务层组树。
func (s *Service) ListComments(ctx context.Context, actorUserID uint64, postID uint64, page int, pageSize int, sort string) ([]forumcontracts.ForumCommentNode, forumcontracts.PageResult, error) {
	_ = ctx
	_ = actorUserID
	_ = postID
	_ = page
	_ = pageSize
	_ = sort
	return nil, forumcontracts.PageResult{}, ErrNotImplemented
}

// CreateComment 是发表评论或回复用例占位，第三步实现父子评论校验和幂等。
func (s *Service) CreateComment(ctx context.Context, req forumcontracts.CreateForumCommentRequest) (*forumcontracts.ForumCommentNode, error) {
	_ = ctx
	_ = req
	return nil, ErrNotImplemented
}

// DeleteComment 是删除自己评论用例占位，第三步实现软删除和权限判断。
func (s *Service) DeleteComment(ctx context.Context, actorUserID uint64, commentID uint64) (*forumcontracts.DeleteForumCommentResult, error) {
	_ = ctx
	_ = actorUserID
	_ = commentID
	return nil, ErrNotImplemented
}

// ImportPost 是一键导入用例占位，第三步会保证同一用户同一帖子只导入一次。
func (s *Service) ImportPost(ctx context.Context, req forumcontracts.ImportForumPostRequest) (*forumcontracts.ImportForumPostResult, error) {
	_ = ctx
	_ = req
	return nil, ErrNotImplemented
}
