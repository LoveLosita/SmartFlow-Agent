package taskclassforum

// PageResult 是计划广场分页响应的跨层契约。
//
// 职责边界：
// 1. 只描述分页元数据，不负责查询和排序逻辑；
// 2. Items 由具体接口决定，避免为了 P0 引入复杂泛型到 RPC 边界；
// 3. HTTP 层和 RPC 层需要保持字段语义一致。
type PageResult struct {
	Page     int  `json:"page"`
	PageSize int  `json:"page_size"`
	Total    int  `json:"total"`
	HasMore  bool `json:"has_more"`
}

// UserBrief 是计划广场前端展示作者和评论人的最小用户信息。
type UserBrief struct {
	UserID    uint64 `json:"user_id"`
	Nickname  string `json:"nickname"`
	AvatarURL string `json:"avatar_url"`
}

// TemplateSummary 是列表卡片里的模板摘要。
type TemplateSummary struct {
	TaskCount      int      `json:"task_count"`
	Mode           string   `json:"mode"`
	StartDate      string   `json:"start_date"`
	EndDate        string   `json:"end_date"`
	StrategyLabels []string `json:"strategy_labels"`
}

// ForumPostCounters 是帖子计数字段快照。
type ForumPostCounters struct {
	LikeCount    int64 `json:"like_count"`
	CommentCount int64 `json:"comment_count"`
	ImportCount  int64 `json:"import_count"`
}

// ForumPostViewerState 是当前登录用户相对该帖子的状态。
type ForumPostViewerState struct {
	Liked        bool `json:"liked"`
	ImportedOnce bool `json:"imported_once"`
}

// ForumTagItem 是计划广场标签筛选区的最小展示单元。
type ForumTagItem struct {
	Tag       string `json:"tag"`
	PostCount int    `json:"post_count"`
}

// ForumPostBrief 是计划列表和详情头部共用的帖子摘要。
type ForumPostBrief struct {
	PostID          uint64               `json:"post_id"`
	Title           string               `json:"title"`
	Summary         string               `json:"summary"`
	Tags            []string             `json:"tags"`
	Author          UserBrief            `json:"author"`
	TemplateSummary TemplateSummary      `json:"template_summary"`
	Counters        ForumPostCounters    `json:"counters"`
	ViewerState     ForumPostViewerState `json:"viewer_state"`
	Status          string               `json:"status"`
	CreatedAt       string               `json:"created_at"`
}

// TemplateItemPreview 是详情页展示的任务条目快照。
type TemplateItemPreview struct {
	ItemID  uint64 `json:"item_id"`
	Order   int    `json:"order"`
	Content string `json:"content"`
}

// TemplateDetail 是论坛模板快照的前端展示结构。
type TemplateDetail struct {
	Mode           string                `json:"mode"`
	StartDate      string                `json:"start_date"`
	EndDate        string                `json:"end_date"`
	StrategyLabels []string              `json:"strategy_labels"`
	TaskCount      int                   `json:"task_count"`
	ItemsPreview   []TemplateItemPreview `json:"items_preview"`
}

// ForumPostDetail 是计划详情接口响应主体。
type ForumPostDetail struct {
	Post     ForumPostBrief `json:"post"`
	Template TemplateDetail `json:"template"`
}

// ForumCommentNode 是服务层组装后的多层评论树节点。
type ForumCommentNode struct {
	CommentID       uint64             `json:"comment_id"`
	PostID          uint64             `json:"post_id"`
	ParentCommentID *uint64            `json:"parent_comment_id"`
	Content         string             `json:"content"`
	Status          string             `json:"status"`
	Author          UserBrief          `json:"author"`
	CanDelete       bool               `json:"can_delete"`
	CreatedAt       string             `json:"created_at"`
	DeletedAt       *string            `json:"deleted_at"`
	Children        []ForumCommentNode `json:"children"`
}

// CreateForumPostRequest 是发布计划请求契约。
type CreateForumPostRequest struct {
	ActorUserID    uint64   `json:"actor_user_id"`
	TaskClassID    uint64   `json:"task_class_id"`
	Title          string   `json:"title"`
	Summary        string   `json:"summary"`
	Tags           []string `json:"tags"`
	IdempotencyKey string   `json:"idempotency_key"`
}

// CreateForumCommentRequest 是发表评论或回复请求契约。
type CreateForumCommentRequest struct {
	ActorUserID     uint64  `json:"actor_user_id"`
	PostID          uint64  `json:"post_id"`
	Content         string  `json:"content"`
	ParentCommentID *uint64 `json:"parent_comment_id"`
	IdempotencyKey  string  `json:"idempotency_key"`
}

// ImportForumPostRequest 是一键导入请求契约。
type ImportForumPostRequest struct {
	ActorUserID    uint64 `json:"actor_user_id"`
	PostID         uint64 `json:"post_id"`
	TargetTitle    string `json:"target_title"`
	IdempotencyKey string `json:"idempotency_key"`
}

// DeleteForumCommentResult 是删除评论后的状态回执。
type DeleteForumCommentResult struct {
	CommentID uint64 `json:"comment_id"`
	Status    string `json:"status"`
}

// ImportForumPostResult 是一键导入后的回执。
type ImportForumPostResult struct {
	ImportID       uint64 `json:"import_id"`
	PostID         uint64 `json:"post_id"`
	NewTaskClassID uint64 `json:"new_task_class_id"`
	TaskClassTitle string `json:"task_class_title"`
	ImportCount    int64  `json:"import_count"`
	CreatedAt      string `json:"created_at"`
}
