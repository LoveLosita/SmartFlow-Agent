package pb

import proto "github.com/golang/protobuf/proto"

var _ = proto.Marshal

const _ = proto.ProtoPackageIsVersion3

type PageRequest struct {
	Page     int32 `protobuf:"varint,1,opt,name=page,proto3" json:"page,omitempty"`
	PageSize int32 `protobuf:"varint,2,opt,name=page_size,json=pageSize,proto3" json:"page_size,omitempty"`
}

func (m *PageRequest) Reset()         { *m = PageRequest{} }
func (m *PageRequest) String() string { return proto.CompactTextString(m) }
func (*PageRequest) ProtoMessage()    {}

type PageResponse struct {
	Page     int32 `protobuf:"varint,1,opt,name=page,proto3" json:"page,omitempty"`
	PageSize int32 `protobuf:"varint,2,opt,name=page_size,json=pageSize,proto3" json:"page_size,omitempty"`
	Total    int32 `protobuf:"varint,3,opt,name=total,proto3" json:"total,omitempty"`
	HasMore  bool  `protobuf:"varint,4,opt,name=has_more,json=hasMore,proto3" json:"has_more,omitempty"`
}

func (m *PageResponse) Reset()         { *m = PageResponse{} }
func (m *PageResponse) String() string { return proto.CompactTextString(m) }
func (*PageResponse) ProtoMessage()    {}

type UserBrief struct {
	UserId    uint64 `protobuf:"varint,1,opt,name=user_id,json=userId,proto3" json:"user_id,omitempty"`
	Nickname  string `protobuf:"bytes,2,opt,name=nickname,proto3" json:"nickname,omitempty"`
	AvatarUrl string `protobuf:"bytes,3,opt,name=avatar_url,json=avatarUrl,proto3" json:"avatar_url,omitempty"`
}

func (m *UserBrief) Reset()         { *m = UserBrief{} }
func (m *UserBrief) String() string { return proto.CompactTextString(m) }
func (*UserBrief) ProtoMessage()    {}

type TemplateSummary struct {
	TaskCount      int32    `protobuf:"varint,1,opt,name=task_count,json=taskCount,proto3" json:"task_count,omitempty"`
	Mode           string   `protobuf:"bytes,2,opt,name=mode,proto3" json:"mode,omitempty"`
	StartDate      string   `protobuf:"bytes,3,opt,name=start_date,json=startDate,proto3" json:"start_date,omitempty"`
	EndDate        string   `protobuf:"bytes,4,opt,name=end_date,json=endDate,proto3" json:"end_date,omitempty"`
	StrategyLabels []string `protobuf:"bytes,5,rep,name=strategy_labels,json=strategyLabels,proto3" json:"strategy_labels,omitempty"`
}

func (m *TemplateSummary) Reset()         { *m = TemplateSummary{} }
func (m *TemplateSummary) String() string { return proto.CompactTextString(m) }
func (*TemplateSummary) ProtoMessage()    {}

type ForumPostCounters struct {
	LikeCount    int64 `protobuf:"varint,1,opt,name=like_count,json=likeCount,proto3" json:"like_count,omitempty"`
	CommentCount int64 `protobuf:"varint,2,opt,name=comment_count,json=commentCount,proto3" json:"comment_count,omitempty"`
	ImportCount  int64 `protobuf:"varint,3,opt,name=import_count,json=importCount,proto3" json:"import_count,omitempty"`
}

func (m *ForumPostCounters) Reset()         { *m = ForumPostCounters{} }
func (m *ForumPostCounters) String() string { return proto.CompactTextString(m) }
func (*ForumPostCounters) ProtoMessage()    {}

type ForumPostViewerState struct {
	Liked        bool `protobuf:"varint,1,opt,name=liked,proto3" json:"liked,omitempty"`
	ImportedOnce bool `protobuf:"varint,2,opt,name=imported_once,json=importedOnce,proto3" json:"imported_once,omitempty"`
}

func (m *ForumPostViewerState) Reset()         { *m = ForumPostViewerState{} }
func (m *ForumPostViewerState) String() string { return proto.CompactTextString(m) }
func (*ForumPostViewerState) ProtoMessage()    {}

type ForumPostBrief struct {
	PostId          uint64                `protobuf:"varint,1,opt,name=post_id,json=postId,proto3" json:"post_id,omitempty"`
	Title           string                `protobuf:"bytes,2,opt,name=title,proto3" json:"title,omitempty"`
	Summary         string                `protobuf:"bytes,3,opt,name=summary,proto3" json:"summary,omitempty"`
	Tags            []string              `protobuf:"bytes,4,rep,name=tags,proto3" json:"tags,omitempty"`
	Author          *UserBrief            `protobuf:"bytes,5,opt,name=author,proto3" json:"author,omitempty"`
	TemplateSummary *TemplateSummary      `protobuf:"bytes,6,opt,name=template_summary,json=templateSummary,proto3" json:"template_summary,omitempty"`
	Counters        *ForumPostCounters    `protobuf:"bytes,7,opt,name=counters,proto3" json:"counters,omitempty"`
	ViewerState     *ForumPostViewerState `protobuf:"bytes,8,opt,name=viewer_state,json=viewerState,proto3" json:"viewer_state,omitempty"`
	Status          string                `protobuf:"bytes,9,opt,name=status,proto3" json:"status,omitempty"`
	CreatedAt       string                `protobuf:"bytes,10,opt,name=created_at,json=createdAt,proto3" json:"created_at,omitempty"`
}

func (m *ForumPostBrief) Reset()         { *m = ForumPostBrief{} }
func (m *ForumPostBrief) String() string { return proto.CompactTextString(m) }
func (*ForumPostBrief) ProtoMessage()    {}

type TemplateItemPreview struct {
	ItemId  uint64 `protobuf:"varint,1,opt,name=item_id,json=itemId,proto3" json:"item_id,omitempty"`
	Order   int32  `protobuf:"varint,2,opt,name=order,proto3" json:"order,omitempty"`
	Content string `protobuf:"bytes,3,opt,name=content,proto3" json:"content,omitempty"`
}

func (m *TemplateItemPreview) Reset()         { *m = TemplateItemPreview{} }
func (m *TemplateItemPreview) String() string { return proto.CompactTextString(m) }
func (*TemplateItemPreview) ProtoMessage()    {}

type TemplateDetail struct {
	Mode           string                 `protobuf:"bytes,1,opt,name=mode,proto3" json:"mode,omitempty"`
	StartDate      string                 `protobuf:"bytes,2,opt,name=start_date,json=startDate,proto3" json:"start_date,omitempty"`
	EndDate        string                 `protobuf:"bytes,3,opt,name=end_date,json=endDate,proto3" json:"end_date,omitempty"`
	StrategyLabels []string               `protobuf:"bytes,4,rep,name=strategy_labels,json=strategyLabels,proto3" json:"strategy_labels,omitempty"`
	TaskCount      int32                  `protobuf:"varint,5,opt,name=task_count,json=taskCount,proto3" json:"task_count,omitempty"`
	ItemsPreview   []*TemplateItemPreview `protobuf:"bytes,6,rep,name=items_preview,json=itemsPreview,proto3" json:"items_preview,omitempty"`
}

func (m *TemplateDetail) Reset()         { *m = TemplateDetail{} }
func (m *TemplateDetail) String() string { return proto.CompactTextString(m) }
func (*TemplateDetail) ProtoMessage()    {}

type ForumPostDetail struct {
	Post     *ForumPostBrief `protobuf:"bytes,1,opt,name=post,proto3" json:"post,omitempty"`
	Template *TemplateDetail `protobuf:"bytes,2,opt,name=template,proto3" json:"template,omitempty"`
}

func (m *ForumPostDetail) Reset()         { *m = ForumPostDetail{} }
func (m *ForumPostDetail) String() string { return proto.CompactTextString(m) }
func (*ForumPostDetail) ProtoMessage()    {}

type ForumCommentNode struct {
	CommentId       uint64              `protobuf:"varint,1,opt,name=comment_id,json=commentId,proto3" json:"comment_id,omitempty"`
	PostId          uint64              `protobuf:"varint,2,opt,name=post_id,json=postId,proto3" json:"post_id,omitempty"`
	ParentCommentId uint64              `protobuf:"varint,3,opt,name=parent_comment_id,json=parentCommentId,proto3" json:"parent_comment_id,omitempty"`
	Content         string              `protobuf:"bytes,4,opt,name=content,proto3" json:"content,omitempty"`
	Status          string              `protobuf:"bytes,5,opt,name=status,proto3" json:"status,omitempty"`
	Author          *UserBrief          `protobuf:"bytes,6,opt,name=author,proto3" json:"author,omitempty"`
	CanDelete       bool                `protobuf:"varint,7,opt,name=can_delete,json=canDelete,proto3" json:"can_delete,omitempty"`
	CreatedAt       string              `protobuf:"bytes,8,opt,name=created_at,json=createdAt,proto3" json:"created_at,omitempty"`
	DeletedAt       string              `protobuf:"bytes,9,opt,name=deleted_at,json=deletedAt,proto3" json:"deleted_at,omitempty"`
	Children        []*ForumCommentNode `protobuf:"bytes,10,rep,name=children,proto3" json:"children,omitempty"`
}

func (m *ForumCommentNode) Reset()         { *m = ForumCommentNode{} }
func (m *ForumCommentNode) String() string { return proto.CompactTextString(m) }
func (*ForumCommentNode) ProtoMessage()    {}

type ListForumPostsRequest struct {
	ActorUserId uint64 `protobuf:"varint,1,opt,name=actor_user_id,json=actorUserId,proto3" json:"actor_user_id,omitempty"`
	Page        int32  `protobuf:"varint,2,opt,name=page,proto3" json:"page,omitempty"`
	PageSize    int32  `protobuf:"varint,3,opt,name=page_size,json=pageSize,proto3" json:"page_size,omitempty"`
	Sort        string `protobuf:"bytes,4,opt,name=sort,proto3" json:"sort,omitempty"`
	Keyword     string `protobuf:"bytes,5,opt,name=keyword,proto3" json:"keyword,omitempty"`
	Tag         string `protobuf:"bytes,6,opt,name=tag,proto3" json:"tag,omitempty"`
}

func (m *ListForumPostsRequest) Reset()         { *m = ListForumPostsRequest{} }
func (m *ListForumPostsRequest) String() string { return proto.CompactTextString(m) }
func (*ListForumPostsRequest) ProtoMessage()    {}

type ListForumPostsResponse struct {
	Items []*ForumPostBrief `protobuf:"bytes,1,rep,name=items,proto3" json:"items,omitempty"`
	Page  *PageResponse     `protobuf:"bytes,2,opt,name=page,proto3" json:"page,omitempty"`
}

func (m *ListForumPostsResponse) Reset()         { *m = ListForumPostsResponse{} }
func (m *ListForumPostsResponse) String() string { return proto.CompactTextString(m) }
func (*ListForumPostsResponse) ProtoMessage()    {}

type ListForumTagsRequest struct {
	ActorUserId uint64 `protobuf:"varint,1,opt,name=actor_user_id,json=actorUserId,proto3" json:"actor_user_id,omitempty"`
	Limit       int32  `protobuf:"varint,2,opt,name=limit,proto3" json:"limit,omitempty"`
}

func (m *ListForumTagsRequest) Reset()         { *m = ListForumTagsRequest{} }
func (m *ListForumTagsRequest) String() string { return proto.CompactTextString(m) }
func (*ListForumTagsRequest) ProtoMessage()    {}

type ForumTagItem struct {
	Tag       string `protobuf:"bytes,1,opt,name=tag,proto3" json:"tag,omitempty"`
	PostCount int32  `protobuf:"varint,2,opt,name=post_count,json=postCount,proto3" json:"post_count,omitempty"`
}

func (m *ForumTagItem) Reset()         { *m = ForumTagItem{} }
func (m *ForumTagItem) String() string { return proto.CompactTextString(m) }
func (*ForumTagItem) ProtoMessage()    {}

type ListForumTagsResponse struct {
	Items []*ForumTagItem `protobuf:"bytes,1,rep,name=items,proto3" json:"items,omitempty"`
}

func (m *ListForumTagsResponse) Reset()         { *m = ListForumTagsResponse{} }
func (m *ListForumTagsResponse) String() string { return proto.CompactTextString(m) }
func (*ListForumTagsResponse) ProtoMessage()    {}

type CreateForumPostRequest struct {
	ActorUserId    uint64   `protobuf:"varint,1,opt,name=actor_user_id,json=actorUserId,proto3" json:"actor_user_id,omitempty"`
	TaskClassId    uint64   `protobuf:"varint,2,opt,name=task_class_id,json=taskClassId,proto3" json:"task_class_id,omitempty"`
	Title          string   `protobuf:"bytes,3,opt,name=title,proto3" json:"title,omitempty"`
	Summary        string   `protobuf:"bytes,4,opt,name=summary,proto3" json:"summary,omitempty"`
	Tags           []string `protobuf:"bytes,5,rep,name=tags,proto3" json:"tags,omitempty"`
	IdempotencyKey string   `protobuf:"bytes,6,opt,name=idempotency_key,json=idempotencyKey,proto3" json:"idempotency_key,omitempty"`
}

func (m *CreateForumPostRequest) Reset()         { *m = CreateForumPostRequest{} }
func (m *CreateForumPostRequest) String() string { return proto.CompactTextString(m) }
func (*CreateForumPostRequest) ProtoMessage()    {}

type CreateForumPostResponse struct {
	Post *ForumPostBrief `protobuf:"bytes,1,opt,name=post,proto3" json:"post,omitempty"`
}

func (m *CreateForumPostResponse) Reset()         { *m = CreateForumPostResponse{} }
func (m *CreateForumPostResponse) String() string { return proto.CompactTextString(m) }
func (*CreateForumPostResponse) ProtoMessage()    {}

type GetForumPostRequest struct {
	ActorUserId uint64 `protobuf:"varint,1,opt,name=actor_user_id,json=actorUserId,proto3" json:"actor_user_id,omitempty"`
	PostId      uint64 `protobuf:"varint,2,opt,name=post_id,json=postId,proto3" json:"post_id,omitempty"`
}

func (m *GetForumPostRequest) Reset()         { *m = GetForumPostRequest{} }
func (m *GetForumPostRequest) String() string { return proto.CompactTextString(m) }
func (*GetForumPostRequest) ProtoMessage()    {}

type GetForumPostResponse struct {
	Data *ForumPostDetail `protobuf:"bytes,1,opt,name=data,proto3" json:"data,omitempty"`
}

func (m *GetForumPostResponse) Reset()         { *m = GetForumPostResponse{} }
func (m *GetForumPostResponse) String() string { return proto.CompactTextString(m) }
func (*GetForumPostResponse) ProtoMessage()    {}

type LikeForumPostRequest struct {
	ActorUserId uint64 `protobuf:"varint,1,opt,name=actor_user_id,json=actorUserId,proto3" json:"actor_user_id,omitempty"`
	PostId      uint64 `protobuf:"varint,2,opt,name=post_id,json=postId,proto3" json:"post_id,omitempty"`
}

func (m *LikeForumPostRequest) Reset()         { *m = LikeForumPostRequest{} }
func (m *LikeForumPostRequest) String() string { return proto.CompactTextString(m) }
func (*LikeForumPostRequest) ProtoMessage()    {}

type LikeForumPostResponse struct {
	Counters    *ForumPostCounters    `protobuf:"bytes,1,opt,name=counters,proto3" json:"counters,omitempty"`
	ViewerState *ForumPostViewerState `protobuf:"bytes,2,opt,name=viewer_state,json=viewerState,proto3" json:"viewer_state,omitempty"`
}

func (m *LikeForumPostResponse) Reset()         { *m = LikeForumPostResponse{} }
func (m *LikeForumPostResponse) String() string { return proto.CompactTextString(m) }
func (*LikeForumPostResponse) ProtoMessage()    {}

type UnlikeForumPostRequest struct {
	ActorUserId uint64 `protobuf:"varint,1,opt,name=actor_user_id,json=actorUserId,proto3" json:"actor_user_id,omitempty"`
	PostId      uint64 `protobuf:"varint,2,opt,name=post_id,json=postId,proto3" json:"post_id,omitempty"`
}

func (m *UnlikeForumPostRequest) Reset()         { *m = UnlikeForumPostRequest{} }
func (m *UnlikeForumPostRequest) String() string { return proto.CompactTextString(m) }
func (*UnlikeForumPostRequest) ProtoMessage()    {}

type UnlikeForumPostResponse struct {
	Counters    *ForumPostCounters    `protobuf:"bytes,1,opt,name=counters,proto3" json:"counters,omitempty"`
	ViewerState *ForumPostViewerState `protobuf:"bytes,2,opt,name=viewer_state,json=viewerState,proto3" json:"viewer_state,omitempty"`
}

func (m *UnlikeForumPostResponse) Reset()         { *m = UnlikeForumPostResponse{} }
func (m *UnlikeForumPostResponse) String() string { return proto.CompactTextString(m) }
func (*UnlikeForumPostResponse) ProtoMessage()    {}

type ListForumCommentsRequest struct {
	ActorUserId uint64 `protobuf:"varint,1,opt,name=actor_user_id,json=actorUserId,proto3" json:"actor_user_id,omitempty"`
	PostId      uint64 `protobuf:"varint,2,opt,name=post_id,json=postId,proto3" json:"post_id,omitempty"`
	Page        int32  `protobuf:"varint,3,opt,name=page,proto3" json:"page,omitempty"`
	PageSize    int32  `protobuf:"varint,4,opt,name=page_size,json=pageSize,proto3" json:"page_size,omitempty"`
	Sort        string `protobuf:"bytes,5,opt,name=sort,proto3" json:"sort,omitempty"`
}

func (m *ListForumCommentsRequest) Reset()         { *m = ListForumCommentsRequest{} }
func (m *ListForumCommentsRequest) String() string { return proto.CompactTextString(m) }
func (*ListForumCommentsRequest) ProtoMessage()    {}

type ListForumCommentsResponse struct {
	Items []*ForumCommentNode `protobuf:"bytes,1,rep,name=items,proto3" json:"items,omitempty"`
	Page  *PageResponse       `protobuf:"bytes,2,opt,name=page,proto3" json:"page,omitempty"`
}

func (m *ListForumCommentsResponse) Reset()         { *m = ListForumCommentsResponse{} }
func (m *ListForumCommentsResponse) String() string { return proto.CompactTextString(m) }
func (*ListForumCommentsResponse) ProtoMessage()    {}

type CreateForumCommentRequest struct {
	ActorUserId     uint64 `protobuf:"varint,1,opt,name=actor_user_id,json=actorUserId,proto3" json:"actor_user_id,omitempty"`
	PostId          uint64 `protobuf:"varint,2,opt,name=post_id,json=postId,proto3" json:"post_id,omitempty"`
	Content         string `protobuf:"bytes,3,opt,name=content,proto3" json:"content,omitempty"`
	ParentCommentId uint64 `protobuf:"varint,4,opt,name=parent_comment_id,json=parentCommentId,proto3" json:"parent_comment_id,omitempty"`
	IdempotencyKey  string `protobuf:"bytes,5,opt,name=idempotency_key,json=idempotencyKey,proto3" json:"idempotency_key,omitempty"`
}

func (m *CreateForumCommentRequest) Reset()         { *m = CreateForumCommentRequest{} }
func (m *CreateForumCommentRequest) String() string { return proto.CompactTextString(m) }
func (*CreateForumCommentRequest) ProtoMessage()    {}

type CreateForumCommentResponse struct {
	Comment *ForumCommentNode `protobuf:"bytes,1,opt,name=comment,proto3" json:"comment,omitempty"`
}

func (m *CreateForumCommentResponse) Reset()         { *m = CreateForumCommentResponse{} }
func (m *CreateForumCommentResponse) String() string { return proto.CompactTextString(m) }
func (*CreateForumCommentResponse) ProtoMessage()    {}

type DeleteForumCommentRequest struct {
	ActorUserId uint64 `protobuf:"varint,1,opt,name=actor_user_id,json=actorUserId,proto3" json:"actor_user_id,omitempty"`
	CommentId   uint64 `protobuf:"varint,2,opt,name=comment_id,json=commentId,proto3" json:"comment_id,omitempty"`
}

func (m *DeleteForumCommentRequest) Reset()         { *m = DeleteForumCommentRequest{} }
func (m *DeleteForumCommentRequest) String() string { return proto.CompactTextString(m) }
func (*DeleteForumCommentRequest) ProtoMessage()    {}

type DeleteForumCommentResponse struct {
	CommentId uint64 `protobuf:"varint,1,opt,name=comment_id,json=commentId,proto3" json:"comment_id,omitempty"`
	Status    string `protobuf:"bytes,2,opt,name=status,proto3" json:"status,omitempty"`
}

func (m *DeleteForumCommentResponse) Reset()         { *m = DeleteForumCommentResponse{} }
func (m *DeleteForumCommentResponse) String() string { return proto.CompactTextString(m) }
func (*DeleteForumCommentResponse) ProtoMessage()    {}

type ImportForumPostRequest struct {
	ActorUserId    uint64 `protobuf:"varint,1,opt,name=actor_user_id,json=actorUserId,proto3" json:"actor_user_id,omitempty"`
	PostId         uint64 `protobuf:"varint,2,opt,name=post_id,json=postId,proto3" json:"post_id,omitempty"`
	TargetTitle    string `protobuf:"bytes,3,opt,name=target_title,json=targetTitle,proto3" json:"target_title,omitempty"`
	IdempotencyKey string `protobuf:"bytes,4,opt,name=idempotency_key,json=idempotencyKey,proto3" json:"idempotency_key,omitempty"`
}

func (m *ImportForumPostRequest) Reset()         { *m = ImportForumPostRequest{} }
func (m *ImportForumPostRequest) String() string { return proto.CompactTextString(m) }
func (*ImportForumPostRequest) ProtoMessage()    {}

type ImportForumPostResponse struct {
	ImportId       uint64 `protobuf:"varint,1,opt,name=import_id,json=importId,proto3" json:"import_id,omitempty"`
	PostId         uint64 `protobuf:"varint,2,opt,name=post_id,json=postId,proto3" json:"post_id,omitempty"`
	NewTaskClassId uint64 `protobuf:"varint,3,opt,name=new_task_class_id,json=newTaskClassId,proto3" json:"new_task_class_id,omitempty"`
	TaskClassTitle string `protobuf:"bytes,4,opt,name=task_class_title,json=taskClassTitle,proto3" json:"task_class_title,omitempty"`
	ImportCount    int64  `protobuf:"varint,5,opt,name=import_count,json=importCount,proto3" json:"import_count,omitempty"`
	CreatedAt      string `protobuf:"bytes,6,opt,name=created_at,json=createdAt,proto3" json:"created_at,omitempty"`
}

func (m *ImportForumPostResponse) Reset()         { *m = ImportForumPostResponse{} }
func (m *ImportForumPostResponse) String() string { return proto.CompactTextString(m) }
func (*ImportForumPostResponse) ProtoMessage()    {}
