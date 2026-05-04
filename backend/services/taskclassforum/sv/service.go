package sv

import (
	"context"
	"errors"

	forumdao "github.com/LoveLosita/smartflow/backend/services/taskclassforum/dao"
	"gorm.io/gorm"
)

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
	forumDAO      *forumdao.ForumDAO
	taskClassPort TaskClassSnapshotPort
}

func New(opts Options) *Service {
	return &Service{
		db:            opts.DB,
		forumDAO:      forumdao.NewForumDAO(opts.DB),
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
