package model

import "time"

const (
	// ForumPostStatusPublished 表示帖子已公开展示在计划广场。
	ForumPostStatusPublished = "published"
	// ForumPostStatusHidden 表示帖子被作者隐藏或后续治理流程下架。
	ForumPostStatusHidden = "hidden"
	// ForumPostStatusDeleted 表示帖子已软删除，P0 暂不对前端展示。
	ForumPostStatusDeleted = "deleted"
	// ForumPostStatusPendingReview 预留审核态，P0 不启用审核后台。
	ForumPostStatusPendingReview = "pending_review"
)

const (
	// ForumLikeStatusActive 表示当前用户仍保持点赞。
	ForumLikeStatusActive = "active"
	// ForumLikeStatusCanceled 表示用户取消点赞，保留记录用于奖励幂等。
	ForumLikeStatusCanceled = "canceled"
)

const (
	// ForumCommentStatusVisible 表示评论正常展示。
	ForumCommentStatusVisible = "visible"
	// ForumCommentStatusDeleted 表示评论已由本人删除，服务层仍保留子回复结构。
	ForumCommentStatusDeleted = "deleted"
)

const (
	// ForumImportStatusPending 表示导入记录已占位，正在创建 TaskClass 副本。
	ForumImportStatusPending = "pending"
	// ForumImportStatusImported 表示导入已成功创建当前用户自己的 TaskClass 副本。
	ForumImportStatusImported = "imported"
	// ForumImportStatusFailed 表示导入副本创建或最终确认失败，可由后续重试覆盖。
	ForumImportStatusFailed = "failed"
)

// ForumPost 是计划广场帖子主体表。
//
// 职责边界：
// 1. 只保存社区帖子可展示信息、作者和计数字段；
// 2. 不保存完整 TaskClass 模板，模板快照归 ForumPostTemplate / ForumPostTemplateItem；
// 3. 计数字段由服务事务内维护，避免列表页每次做聚合统计。
type ForumPost struct {
	ID                uint64     `gorm:"column:id;primaryKey;autoIncrement"`
	AuthorUserID      uint64     `gorm:"column:author_user_id;not null;index:idx_forum_posts_author_status,priority:1;uniqueIndex:uk_forum_posts_author_idem,priority:1;comment:作者用户ID"`
	SourceTaskClassID uint64     `gorm:"column:source_task_class_id;not null;index:idx_forum_posts_source_task_class;comment:发布时选择的原始TaskClass ID，仅用于审计"`
	Title             string     `gorm:"column:title;type:varchar(80);not null;comment:帖子标题"`
	Summary           string     `gorm:"column:summary;type:text;comment:帖子简介"`
	TagsJSON          string     `gorm:"column:tags_json;type:json;not null;comment:标签JSON数组"`
	IdempotencyKey    *string    `gorm:"column:idempotency_key;type:varchar(128);uniqueIndex:uk_forum_posts_author_idem,priority:2;comment:发布请求幂等键"`
	Status            string     `gorm:"column:status;type:varchar(32);not null;default:'published';index:idx_forum_posts_status_created,priority:1;index:idx_forum_posts_author_status,priority:2;comment:published/hidden/deleted/pending_review"`
	LikeCount         int64      `gorm:"column:like_count;not null;default:0;index:idx_forum_posts_like_count;comment:点赞数冗余计数"`
	CommentCount      int64      `gorm:"column:comment_count;not null;default:0;comment:评论数冗余计数"`
	ImportCount       int64      `gorm:"column:import_count;not null;default:0;index:idx_forum_posts_import_count;comment:导入数冗余计数"`
	CreatedAt         time.Time  `gorm:"column:created_at;autoCreateTime;index:idx_forum_posts_status_created,priority:2;comment:创建时间"`
	UpdatedAt         time.Time  `gorm:"column:updated_at;autoUpdateTime;comment:更新时间"`
	DeletedAt         *time.Time `gorm:"column:deleted_at;index;comment:软删除时间"`
}

func (ForumPost) TableName() string {
	return "forum_posts"
}

// ForumPostTemplate 是发布瞬间复制出的 TaskClass 配置快照。
//
// 职责边界：
// 1. 只保存可分享的 TaskClass 配置白名单；
// 2. 不保存 embedded_time、schedule 绑定和用户私有排程状态；
// 3. 后续原作者修改原 TaskClass 时，本快照不跟随变化。
type ForumPostTemplate struct {
	ID                     uint64     `gorm:"column:id;primaryKey;autoIncrement"`
	PostID                 uint64     `gorm:"column:post_id;not null;uniqueIndex:uk_forum_templates_post;comment:所属帖子ID"`
	SourceTaskClassID      uint64     `gorm:"column:source_task_class_id;not null;comment:原始TaskClass ID"`
	Mode                   string     `gorm:"column:mode;type:varchar(32);comment:TaskClass 模式"`
	StartDate              *time.Time `gorm:"column:start_date;comment:计划开始日期"`
	EndDate                *time.Time `gorm:"column:end_date;comment:计划结束日期"`
	SubjectType            string     `gorm:"column:subject_type;type:varchar(32);comment:学科类型"`
	DifficultyLevel        string     `gorm:"column:difficulty_level;type:varchar(16);comment:难度等级"`
	CognitiveIntensity     string     `gorm:"column:cognitive_intensity;type:varchar(16);comment:认知强度"`
	TotalSlots             int        `gorm:"column:total_slots;comment:分配的总节数"`
	AllowFillerCourse      bool       `gorm:"column:allow_filler_course;not null;default:true;comment:是否允许填充课程空隙"`
	Strategy               string     `gorm:"column:strategy;type:varchar(32);comment:规划策略"`
	ExcludedSlotsJSON      *string    `gorm:"column:excluded_slots_json;type:json;comment:排除节次JSON数组"`
	ExcludedDaysOfWeekJSON *string    `gorm:"column:excluded_days_of_week_json;type:json;comment:排除星期JSON数组"`
	StrategyLabelsJSON     *string    `gorm:"column:strategy_labels_json;type:json;comment:前端展示用策略标签JSON数组"`
	ConfigSnapshotJSON     *string    `gorm:"column:config_snapshot_json;type:json;comment:过滤后的配置快照，便于后续兼容扩展"`
	CreatedAt              time.Time  `gorm:"column:created_at;autoCreateTime;comment:创建时间"`
	UpdatedAt              time.Time  `gorm:"column:updated_at;autoUpdateTime;comment:更新时间"`
}

func (ForumPostTemplate) TableName() string {
	return "forum_post_templates"
}

// ForumPostTemplateItem 是 TaskClassItem 的可分享快照。
//
// 职责边界：
// 1. 只保存任务条目的顺序和内容；
// 2. 不保存 embedded_time，避免把原作者私有排程状态带给导入用户；
// 3. 导入时服务层按这些快照重新创建当前用户自己的 TaskClassItem。
type ForumPostTemplateItem struct {
	ID               uint64    `gorm:"column:id;primaryKey;autoIncrement"`
	TemplateID       uint64    `gorm:"column:template_id;not null;uniqueIndex:uk_forum_template_items_order,priority:1;index:idx_forum_template_items_template;comment:模板ID"`
	PostID           uint64    `gorm:"column:post_id;not null;index:idx_forum_template_items_post;comment:帖子ID，便于按帖子直接读取预览"`
	SourceTaskItemID uint64    `gorm:"column:source_task_item_id;not null;comment:原始TaskClassItem ID，仅用于审计"`
	Order            int       `gorm:"column:item_order;not null;uniqueIndex:uk_forum_template_items_order,priority:2;comment:条目顺序"`
	Content          string    `gorm:"column:content;type:text;not null;comment:任务条目内容"`
	CreatedAt        time.Time `gorm:"column:created_at;autoCreateTime;comment:创建时间"`
}

func (ForumPostTemplateItem) TableName() string {
	return "forum_post_template_items"
}

// ForumLike 是点赞幂等表。
//
// 职责边界：
// 1. 通过 post_id + user_id 唯一约束保证同一用户同一帖子只有一条点赞状态；
// 2. 取消点赞只把状态改为 canceled，不删除记录，避免作者奖励被反复触发；
// 3. event_id 对应首次点赞奖励事件，供 token-store 账本幂等使用。
type ForumLike struct {
	ID           uint64     `gorm:"column:id;primaryKey;autoIncrement"`
	PostID       uint64     `gorm:"column:post_id;not null;uniqueIndex:uk_forum_likes_post_user,priority:1;index:idx_forum_likes_post_status,priority:1;comment:帖子ID"`
	UserID       uint64     `gorm:"column:user_id;not null;uniqueIndex:uk_forum_likes_post_user,priority:2;comment:点赞用户ID"`
	AuthorUserID uint64     `gorm:"column:author_user_id;not null;index:idx_forum_likes_author;comment:帖子作者ID，便于奖励和审计"`
	Status       string     `gorm:"column:status;type:varchar(32);not null;default:'active';index:idx_forum_likes_post_status,priority:2;comment:active/canceled"`
	EventID      string     `gorm:"column:event_id;type:varchar(128);not null;uniqueIndex:uk_forum_likes_event;comment:首次点赞事件ID"`
	LikedAt      time.Time  `gorm:"column:liked_at;autoCreateTime;comment:首次点赞时间"`
	CanceledAt   *time.Time `gorm:"column:canceled_at;comment:最近一次取消点赞时间"`
	CreatedAt    time.Time  `gorm:"column:created_at;autoCreateTime;comment:创建时间"`
	UpdatedAt    time.Time  `gorm:"column:updated_at;autoUpdateTime;comment:更新时间"`
}

func (ForumLike) TableName() string {
	return "forum_likes"
}

// ForumComment 是评论和多层回复的扁平存储表。
//
// 职责边界：
// 1. 数据库只保存 parent_comment_id，不保存树结构；
// 2. 服务层按帖子读取扁平评论后组装评论树；
// 3. 删除评论使用 status + deleted_at 软删除，保留子回复链路。
type ForumComment struct {
	ID              uint64     `gorm:"column:id;primaryKey;autoIncrement"`
	PostID          uint64     `gorm:"column:post_id;not null;index:idx_forum_comments_post_parent_created,priority:1;comment:帖子ID"`
	ParentCommentID *uint64    `gorm:"column:parent_comment_id;index:idx_forum_comments_post_parent_created,priority:2;comment:父评论ID，根评论为空"`
	UserID          uint64     `gorm:"column:user_id;not null;uniqueIndex:uk_forum_comments_user_idem,priority:1;index:idx_forum_comments_user;comment:评论用户ID"`
	Content         string     `gorm:"column:content;type:text;not null;comment:评论内容"`
	Status          string     `gorm:"column:status;type:varchar(32);not null;default:'visible';index:idx_forum_comments_status;comment:visible/deleted"`
	IdempotencyKey  *string    `gorm:"column:idempotency_key;type:varchar(128);uniqueIndex:uk_forum_comments_user_idem,priority:2;comment:评论创建幂等键"`
	CreatedAt       time.Time  `gorm:"column:created_at;autoCreateTime;index:idx_forum_comments_post_parent_created,priority:3;comment:创建时间"`
	UpdatedAt       time.Time  `gorm:"column:updated_at;autoUpdateTime;comment:更新时间"`
	DeletedAt       *time.Time `gorm:"column:deleted_at;comment:用户删除时间"`
}

func (ForumComment) TableName() string {
	return "forum_comments"
}

// ForumImport 是一键导入记录表。
//
// 职责边界：
// 1. 通过 post_id + user_id 唯一约束保证同一用户同一计划只导入一次；
// 2. 只记录导入到 TaskClass 的结果，不写 schedule；
// 3. event_id 对应导入奖励事件，供 token-store 账本幂等使用。
type ForumImport struct {
	ID             uint64    `gorm:"column:id;primaryKey;autoIncrement"`
	PostID         uint64    `gorm:"column:post_id;not null;uniqueIndex:uk_forum_imports_post_user,priority:1;index:idx_forum_imports_post;comment:帖子ID"`
	UserID         uint64    `gorm:"column:user_id;not null;uniqueIndex:uk_forum_imports_post_user,priority:2;uniqueIndex:uk_forum_imports_user_idem,priority:1;index:idx_forum_imports_user;comment:导入用户ID"`
	AuthorUserID   uint64    `gorm:"column:author_user_id;not null;index:idx_forum_imports_author;comment:帖子作者ID，便于奖励和审计"`
	NewTaskClassID *uint64   `gorm:"column:new_task_class_id;comment:导入后创建的当前用户TaskClass ID，pending/failed 时为空"`
	TargetTitle    string    `gorm:"column:target_title;type:varchar(80);comment:导入后的TaskClass标题"`
	Status         string    `gorm:"column:status;type:varchar(32);not null;default:'pending';comment:pending/imported/failed"`
	EventID        string    `gorm:"column:event_id;type:varchar(128);not null;uniqueIndex:uk_forum_imports_event;comment:导入事件ID"`
	IdempotencyKey *string   `gorm:"column:idempotency_key;type:varchar(128);uniqueIndex:uk_forum_imports_user_idem,priority:2;comment:导入请求幂等键"`
	LastError      *string   `gorm:"column:last_error;type:text;comment:最近一次导入失败原因"`
	CreatedAt      time.Time `gorm:"column:created_at;autoCreateTime;comment:创建时间"`
	UpdatedAt      time.Time `gorm:"column:updated_at;autoUpdateTime;comment:更新时间"`
}

func (ForumImport) TableName() string {
	return "forum_imports"
}
