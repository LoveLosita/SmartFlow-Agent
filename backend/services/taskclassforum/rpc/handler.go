package rpc

import (
	"context"
	"errors"

	"github.com/LoveLosita/smartflow/backend/respond"
	"github.com/LoveLosita/smartflow/backend/services/taskclassforum/rpc/pb"
	forumsv "github.com/LoveLosita/smartflow/backend/services/taskclassforum/sv"
	forumcontracts "github.com/LoveLosita/smartflow/backend/shared/contracts/taskclassforum"
)

type Handler struct {
	pb.UnimplementedTaskClassForumServiceServer
	svc *forumsv.Service
}

func NewHandler(svc *forumsv.Service) *Handler {
	return &Handler{svc: svc}
}

// service 负责统一校验 RPC 层依赖是否已经注入。
//
// 职责边界：
// 1. 只判断 handler 自身和业务 service 是否可用；
// 2. 不负责校验请求参数，也不处理具体业务规则；
// 3. 失败时返回可直接转成 gRPC status 的业务错误。
func (h *Handler) service() (*forumsv.Service, error) {
	if h == nil || h.svc == nil {
		return nil, errors.New("taskclassforum service dependency not initialized")
	}
	return h.svc, nil
}

// ListPosts 负责把计划广场列表请求从 gRPC 协议转成内部服务调用。
func (h *Handler) ListPosts(ctx context.Context, req *pb.ListForumPostsRequest) (*pb.ListForumPostsResponse, error) {
	svc, err := h.service()
	if err != nil {
		return nil, grpcErrorFromServiceError(err)
	}
	if req == nil {
		return nil, grpcErrorFromServiceError(respond.MissingParam)
	}

	items, page, err := svc.ListPosts(ctx, req.ActorUserId, int(req.Page), int(req.PageSize), req.Sort, req.Keyword, req.Tag)
	if err != nil {
		return nil, grpcErrorFromServiceError(err)
	}
	return &pb.ListForumPostsResponse{
		Items: forumPostBriefsToPB(items),
		Page:  forumPageToPB(page),
	}, nil
}

func (h *Handler) ListTags(ctx context.Context, req *pb.ListForumTagsRequest) (*pb.ListForumTagsResponse, error) {
	svc, err := h.service()
	if err != nil {
		return nil, grpcErrorFromServiceError(err)
	}
	if req == nil {
		return nil, grpcErrorFromServiceError(respond.MissingParam)
	}

	items, err := svc.ListTags(ctx, req.ActorUserId, int(req.Limit))
	if err != nil {
		return nil, grpcErrorFromServiceError(err)
	}
	return &pb.ListForumTagsResponse{Items: forumTagItemsToPB(items)}, nil
}

func (h *Handler) CreatePost(ctx context.Context, req *pb.CreateForumPostRequest) (*pb.CreateForumPostResponse, error) {
	svc, err := h.service()
	if err != nil {
		return nil, grpcErrorFromServiceError(err)
	}
	if req == nil {
		return nil, grpcErrorFromServiceError(respond.MissingParam)
	}

	post, err := svc.CreatePost(ctx, forumcontracts.CreateForumPostRequest{
		ActorUserID:    req.ActorUserId,
		TaskClassID:    req.TaskClassId,
		Title:          req.Title,
		Summary:        req.Summary,
		Tags:           append([]string(nil), req.Tags...),
		IdempotencyKey: req.IdempotencyKey,
	})
	if err != nil {
		return nil, grpcErrorFromServiceError(err)
	}
	return &pb.CreateForumPostResponse{Post: forumPostBriefToPB(post)}, nil
}

func (h *Handler) GetPost(ctx context.Context, req *pb.GetForumPostRequest) (*pb.GetForumPostResponse, error) {
	svc, err := h.service()
	if err != nil {
		return nil, grpcErrorFromServiceError(err)
	}
	if req == nil {
		return nil, grpcErrorFromServiceError(respond.MissingParam)
	}

	data, err := svc.GetPost(ctx, req.ActorUserId, req.PostId)
	if err != nil {
		return nil, grpcErrorFromServiceError(err)
	}
	return &pb.GetForumPostResponse{Data: forumPostDetailToPB(data)}, nil
}

func (h *Handler) LikePost(ctx context.Context, req *pb.LikeForumPostRequest) (*pb.LikeForumPostResponse, error) {
	svc, err := h.service()
	if err != nil {
		return nil, grpcErrorFromServiceError(err)
	}
	if req == nil {
		return nil, grpcErrorFromServiceError(respond.MissingParam)
	}

	counters, viewerState, err := svc.LikePost(ctx, req.ActorUserId, req.PostId)
	if err != nil {
		return nil, grpcErrorFromServiceError(err)
	}
	return &pb.LikeForumPostResponse{
		Counters:    forumPostCountersToPB(counters),
		ViewerState: forumPostViewerStateToPB(viewerState),
	}, nil
}

func (h *Handler) UnlikePost(ctx context.Context, req *pb.UnlikeForumPostRequest) (*pb.UnlikeForumPostResponse, error) {
	svc, err := h.service()
	if err != nil {
		return nil, grpcErrorFromServiceError(err)
	}
	if req == nil {
		return nil, grpcErrorFromServiceError(respond.MissingParam)
	}

	counters, viewerState, err := svc.UnlikePost(ctx, req.ActorUserId, req.PostId)
	if err != nil {
		return nil, grpcErrorFromServiceError(err)
	}
	return &pb.UnlikeForumPostResponse{
		Counters:    forumPostCountersToPB(counters),
		ViewerState: forumPostViewerStateToPB(viewerState),
	}, nil
}

func (h *Handler) ListComments(ctx context.Context, req *pb.ListForumCommentsRequest) (*pb.ListForumCommentsResponse, error) {
	svc, err := h.service()
	if err != nil {
		return nil, grpcErrorFromServiceError(err)
	}
	if req == nil {
		return nil, grpcErrorFromServiceError(respond.MissingParam)
	}

	items, page, err := svc.ListComments(ctx, req.ActorUserId, req.PostId, int(req.Page), int(req.PageSize), req.Sort)
	if err != nil {
		return nil, grpcErrorFromServiceError(err)
	}
	return &pb.ListForumCommentsResponse{
		Items: forumCommentNodesToPB(items),
		Page:  forumPageToPB(page),
	}, nil
}

func (h *Handler) CreateComment(ctx context.Context, req *pb.CreateForumCommentRequest) (*pb.CreateForumCommentResponse, error) {
	svc, err := h.service()
	if err != nil {
		return nil, grpcErrorFromServiceError(err)
	}
	if req == nil {
		return nil, grpcErrorFromServiceError(respond.MissingParam)
	}

	comment, err := svc.CreateComment(ctx, forumcontracts.CreateForumCommentRequest{
		ActorUserID:     req.ActorUserId,
		PostID:          req.PostId,
		Content:         req.Content,
		ParentCommentID: forumUint64PtrFromPositive(req.ParentCommentId),
		IdempotencyKey:  req.IdempotencyKey,
	})
	if err != nil {
		return nil, grpcErrorFromServiceError(err)
	}
	return &pb.CreateForumCommentResponse{Comment: forumCommentNodeToPB(comment)}, nil
}

func (h *Handler) DeleteComment(ctx context.Context, req *pb.DeleteForumCommentRequest) (*pb.DeleteForumCommentResponse, error) {
	svc, err := h.service()
	if err != nil {
		return nil, grpcErrorFromServiceError(err)
	}
	if req == nil {
		return nil, grpcErrorFromServiceError(respond.MissingParam)
	}

	result, err := svc.DeleteComment(ctx, req.ActorUserId, req.CommentId)
	if err != nil {
		return nil, grpcErrorFromServiceError(err)
	}
	return &pb.DeleteForumCommentResponse{
		CommentId: result.CommentID,
		Status:    result.Status,
	}, nil
}

func (h *Handler) ImportPost(ctx context.Context, req *pb.ImportForumPostRequest) (*pb.ImportForumPostResponse, error) {
	svc, err := h.service()
	if err != nil {
		return nil, grpcErrorFromServiceError(err)
	}
	if req == nil {
		return nil, grpcErrorFromServiceError(respond.MissingParam)
	}

	result, err := svc.ImportPost(ctx, forumcontracts.ImportForumPostRequest{
		ActorUserID:    req.ActorUserId,
		PostID:         req.PostId,
		TargetTitle:    req.TargetTitle,
		IdempotencyKey: req.IdempotencyKey,
	})
	if err != nil {
		return nil, grpcErrorFromServiceError(err)
	}
	return &pb.ImportForumPostResponse{
		ImportId:       result.ImportID,
		PostId:         result.PostID,
		NewTaskClassId: result.NewTaskClassID,
		TaskClassTitle: result.TaskClassTitle,
		ImportCount:    result.ImportCount,
		CreatedAt:      result.CreatedAt,
	}, nil
}

func forumPageToPB(page forumcontracts.PageResult) *pb.PageResponse {
	return &pb.PageResponse{
		Page:     int32(page.Page),
		PageSize: int32(page.PageSize),
		Total:    int32(page.Total),
		HasMore:  page.HasMore,
	}
}

func forumUserToPB(user forumcontracts.UserBrief) *pb.UserBrief {
	return &pb.UserBrief{
		UserId:    user.UserID,
		Nickname:  user.Nickname,
		AvatarUrl: user.AvatarURL,
	}
}

func forumTemplateSummaryToPB(summary forumcontracts.TemplateSummary) *pb.TemplateSummary {
	return &pb.TemplateSummary{
		TaskCount:      int32(summary.TaskCount),
		Mode:           summary.Mode,
		StartDate:      summary.StartDate,
		EndDate:        summary.EndDate,
		StrategyLabels: append([]string(nil), summary.StrategyLabels...),
	}
}

func forumPostCountersToPB(counters forumcontracts.ForumPostCounters) *pb.ForumPostCounters {
	return &pb.ForumPostCounters{
		LikeCount:    counters.LikeCount,
		CommentCount: counters.CommentCount,
		ImportCount:  counters.ImportCount,
	}
}

func forumPostViewerStateToPB(viewerState forumcontracts.ForumPostViewerState) *pb.ForumPostViewerState {
	return &pb.ForumPostViewerState{
		Liked:        viewerState.Liked,
		ImportedOnce: viewerState.ImportedOnce,
	}
}

func forumPostBriefToPB(post *forumcontracts.ForumPostBrief) *pb.ForumPostBrief {
	if post == nil {
		return nil
	}
	return &pb.ForumPostBrief{
		PostId:          post.PostID,
		Title:           post.Title,
		Summary:         post.Summary,
		Tags:            append([]string(nil), post.Tags...),
		Author:          forumUserToPB(post.Author),
		TemplateSummary: forumTemplateSummaryToPB(post.TemplateSummary),
		Counters:        forumPostCountersToPB(post.Counters),
		ViewerState:     forumPostViewerStateToPB(post.ViewerState),
		Status:          post.Status,
		CreatedAt:       post.CreatedAt,
	}
}

func forumPostBriefsToPB(items []forumcontracts.ForumPostBrief) []*pb.ForumPostBrief {
	if len(items) == 0 {
		return nil
	}
	result := make([]*pb.ForumPostBrief, 0, len(items))
	for i := range items {
		item := items[i]
		result = append(result, forumPostBriefToPB(&item))
	}
	return result
}

func forumTemplateItemPreviewToPB(item forumcontracts.TemplateItemPreview) *pb.TemplateItemPreview {
	return &pb.TemplateItemPreview{
		ItemId:  item.ItemID,
		Order:   int32(item.Order),
		Content: item.Content,
	}
}

func forumTemplateDetailToPB(detail forumcontracts.TemplateDetail) *pb.TemplateDetail {
	preview := make([]*pb.TemplateItemPreview, 0, len(detail.ItemsPreview))
	for i := range detail.ItemsPreview {
		item := detail.ItemsPreview[i]
		preview = append(preview, forumTemplateItemPreviewToPB(item))
	}
	return &pb.TemplateDetail{
		Mode:           detail.Mode,
		StartDate:      detail.StartDate,
		EndDate:        detail.EndDate,
		StrategyLabels: append([]string(nil), detail.StrategyLabels...),
		TaskCount:      int32(detail.TaskCount),
		ItemsPreview:   preview,
	}
}

func forumPostDetailToPB(detail *forumcontracts.ForumPostDetail) *pb.ForumPostDetail {
	if detail == nil {
		return nil
	}
	return &pb.ForumPostDetail{
		Post:     forumPostBriefToPB(&detail.Post),
		Template: forumTemplateDetailToPB(detail.Template),
	}
}

func forumTagItemsToPB(items []forumcontracts.ForumTagItem) []*pb.ForumTagItem {
	if len(items) == 0 {
		return nil
	}
	result := make([]*pb.ForumTagItem, 0, len(items))
	for i := range items {
		item := items[i]
		result = append(result, &pb.ForumTagItem{
			Tag:       item.Tag,
			PostCount: int32(item.PostCount),
		})
	}
	return result
}

func forumCommentNodeToPB(node *forumcontracts.ForumCommentNode) *pb.ForumCommentNode {
	if node == nil {
		return nil
	}
	children := make([]*pb.ForumCommentNode, 0, len(node.Children))
	for i := range node.Children {
		child := node.Children[i]
		children = append(children, forumCommentNodeToPB(&child))
	}
	return &pb.ForumCommentNode{
		CommentId:       node.CommentID,
		PostId:          node.PostID,
		ParentCommentId: forumUint64FromPtr(node.ParentCommentID),
		Content:         node.Content,
		Status:          node.Status,
		Author:          forumUserToPB(node.Author),
		CanDelete:       node.CanDelete,
		CreatedAt:       node.CreatedAt,
		DeletedAt:       forumStringFromPtr(node.DeletedAt),
		Children:        children,
	}
}

func forumCommentNodesToPB(items []forumcontracts.ForumCommentNode) []*pb.ForumCommentNode {
	if len(items) == 0 {
		return nil
	}
	result := make([]*pb.ForumCommentNode, 0, len(items))
	for i := range items {
		item := items[i]
		result = append(result, forumCommentNodeToPB(&item))
	}
	return result
}

func forumUint64FromPtr(value *uint64) uint64 {
	if value == nil {
		return 0
	}
	return *value
}

func forumUint64PtrFromPositive(value uint64) *uint64 {
	if value == 0 {
		return nil
	}
	result := value
	return &result
}

func forumStringFromPtr(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}
