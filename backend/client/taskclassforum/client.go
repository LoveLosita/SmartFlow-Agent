package taskclassforum

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/LoveLosita/smartflow/backend/services/taskclassforum/rpc/pb"
	contracts "github.com/LoveLosita/smartflow/backend/shared/contracts/taskclassforum"
	"github.com/zeromicro/go-zero/zrpc"
)

const (
	defaultEndpoint = "127.0.0.1:9090"
	defaultTimeout  = 2 * time.Second
)

type ClientConfig struct {
	Endpoints []string
	Target    string
	Timeout   time.Duration
}

// Client 是 gateway 侧访问计划广场 zrpc 的适配层。
//
// 职责边界：
// 1. 只负责 HTTP gateway 与 taskclassforum zrpc 之间的协议转译；
// 2. 不直连 forum_* 表，也不读取旧 TaskClass 表，所有业务规则交给 taskclassforum 服务；
// 3. gRPC 业务错误会在这里反解回 respond.Response，便于 HTTP 层统一返回。
type Client struct {
	rpc pb.TaskClassForumServiceClient
}

func NewClient(cfg ClientConfig) (*Client, error) {
	timeout := cfg.Timeout
	if timeout <= 0 {
		timeout = defaultTimeout
	}
	endpoints := normalizeEndpoints(cfg.Endpoints)
	target := strings.TrimSpace(cfg.Target)
	if len(endpoints) == 0 && target == "" {
		endpoints = []string{defaultEndpoint}
	}

	zclient, err := zrpc.NewClient(zrpc.RpcClientConf{
		Endpoints: endpoints,
		Target:    target,
		NonBlock:  true,
		Timeout:   int64(timeout / time.Millisecond),
	})
	if err != nil {
		return nil, err
	}
	return &Client{rpc: pb.NewTaskClassForumServiceClient(zclient.Conn())}, nil
}

func (c *Client) ListPosts(ctx context.Context, actorUserID uint64, page int, pageSize int, sort string, keyword string, tag string) ([]contracts.ForumPostBrief, contracts.PageResult, error) {
	if err := c.ensureReady(); err != nil {
		return nil, contracts.PageResult{}, err
	}
	resp, err := c.rpc.ListPosts(ctx, &pb.ListForumPostsRequest{
		ActorUserId: actorUserID,
		Page:        int32(page),
		PageSize:    int32(pageSize),
		Sort:        sort,
		Keyword:     keyword,
		Tag:         tag,
	})
	if err != nil {
		return nil, contracts.PageResult{}, responseFromRPCError(err)
	}
	if resp == nil {
		return nil, contracts.PageResult{}, errors.New("taskclassforum zrpc service returned empty list posts response")
	}
	return forumPostBriefsFromPB(resp.Items), pageFromPB(resp.Page), nil
}

func (c *Client) ListTags(ctx context.Context, actorUserID uint64, limit int) ([]contracts.ForumTagItem, error) {
	if err := c.ensureReady(); err != nil {
		return nil, err
	}
	resp, err := c.rpc.ListTags(ctx, &pb.ListForumTagsRequest{
		ActorUserId: actorUserID,
		Limit:       int32(limit),
	})
	if err != nil {
		return nil, responseFromRPCError(err)
	}
	if resp == nil {
		return nil, errors.New("taskclassforum zrpc service returned empty list tags response")
	}
	return forumTagItemsFromPB(resp.Items), nil
}

func (c *Client) CreatePost(ctx context.Context, req contracts.CreateForumPostRequest) (*contracts.ForumPostBrief, error) {
	if err := c.ensureReady(); err != nil {
		return nil, err
	}
	resp, err := c.rpc.CreatePost(ctx, &pb.CreateForumPostRequest{
		ActorUserId:    req.ActorUserID,
		TaskClassId:    req.TaskClassID,
		Title:          req.Title,
		Summary:        req.Summary,
		Tags:           append([]string(nil), req.Tags...),
		IdempotencyKey: req.IdempotencyKey,
	})
	if err != nil {
		return nil, responseFromRPCError(err)
	}
	if resp == nil {
		return nil, errors.New("taskclassforum zrpc service returned empty create post response")
	}
	post := forumPostBriefFromPB(resp.Post)
	return &post, nil
}

func (c *Client) GetPost(ctx context.Context, actorUserID uint64, postID uint64) (*contracts.ForumPostDetail, error) {
	if err := c.ensureReady(); err != nil {
		return nil, err
	}
	resp, err := c.rpc.GetPost(ctx, &pb.GetForumPostRequest{
		ActorUserId: actorUserID,
		PostId:      postID,
	})
	if err != nil {
		return nil, responseFromRPCError(err)
	}
	if resp == nil {
		return nil, errors.New("taskclassforum zrpc service returned empty get post response")
	}
	data := forumPostDetailFromPB(resp.Data)
	return &data, nil
}

func (c *Client) LikePost(ctx context.Context, actorUserID uint64, postID uint64) (contracts.ForumPostCounters, contracts.ForumPostViewerState, error) {
	if err := c.ensureReady(); err != nil {
		return contracts.ForumPostCounters{}, contracts.ForumPostViewerState{}, err
	}
	resp, err := c.rpc.LikePost(ctx, &pb.LikeForumPostRequest{
		ActorUserId: actorUserID,
		PostId:      postID,
	})
	if err != nil {
		return contracts.ForumPostCounters{}, contracts.ForumPostViewerState{}, responseFromRPCError(err)
	}
	if resp == nil {
		return contracts.ForumPostCounters{}, contracts.ForumPostViewerState{}, errors.New("taskclassforum zrpc service returned empty like response")
	}
	return forumPostCountersFromPB(resp.Counters), forumPostViewerStateFromPB(resp.ViewerState), nil
}

func (c *Client) UnlikePost(ctx context.Context, actorUserID uint64, postID uint64) (contracts.ForumPostCounters, contracts.ForumPostViewerState, error) {
	if err := c.ensureReady(); err != nil {
		return contracts.ForumPostCounters{}, contracts.ForumPostViewerState{}, err
	}
	resp, err := c.rpc.UnlikePost(ctx, &pb.UnlikeForumPostRequest{
		ActorUserId: actorUserID,
		PostId:      postID,
	})
	if err != nil {
		return contracts.ForumPostCounters{}, contracts.ForumPostViewerState{}, responseFromRPCError(err)
	}
	if resp == nil {
		return contracts.ForumPostCounters{}, contracts.ForumPostViewerState{}, errors.New("taskclassforum zrpc service returned empty unlike response")
	}
	return forumPostCountersFromPB(resp.Counters), forumPostViewerStateFromPB(resp.ViewerState), nil
}

func (c *Client) ListComments(ctx context.Context, actorUserID uint64, postID uint64, page int, pageSize int, sort string) ([]contracts.ForumCommentNode, contracts.PageResult, error) {
	if err := c.ensureReady(); err != nil {
		return nil, contracts.PageResult{}, err
	}
	resp, err := c.rpc.ListComments(ctx, &pb.ListForumCommentsRequest{
		ActorUserId: actorUserID,
		PostId:      postID,
		Page:        int32(page),
		PageSize:    int32(pageSize),
		Sort:        sort,
	})
	if err != nil {
		return nil, contracts.PageResult{}, responseFromRPCError(err)
	}
	if resp == nil {
		return nil, contracts.PageResult{}, errors.New("taskclassforum zrpc service returned empty list comments response")
	}
	return forumCommentNodesFromPB(resp.Items), pageFromPB(resp.Page), nil
}

func (c *Client) CreateComment(ctx context.Context, req contracts.CreateForumCommentRequest) (*contracts.ForumCommentNode, error) {
	if err := c.ensureReady(); err != nil {
		return nil, err
	}
	resp, err := c.rpc.CreateComment(ctx, &pb.CreateForumCommentRequest{
		ActorUserId:     req.ActorUserID,
		PostId:          req.PostID,
		Content:         req.Content,
		ParentCommentId: uint64FromPtr(req.ParentCommentID),
		IdempotencyKey:  req.IdempotencyKey,
	})
	if err != nil {
		return nil, responseFromRPCError(err)
	}
	if resp == nil {
		return nil, errors.New("taskclassforum zrpc service returned empty create comment response")
	}
	comment := forumCommentNodeFromPB(resp.Comment)
	return &comment, nil
}

func (c *Client) DeleteComment(ctx context.Context, actorUserID uint64, commentID uint64) (*contracts.DeleteForumCommentResult, error) {
	if err := c.ensureReady(); err != nil {
		return nil, err
	}
	resp, err := c.rpc.DeleteComment(ctx, &pb.DeleteForumCommentRequest{
		ActorUserId: actorUserID,
		CommentId:   commentID,
	})
	if err != nil {
		return nil, responseFromRPCError(err)
	}
	if resp == nil {
		return nil, errors.New("taskclassforum zrpc service returned empty delete comment response")
	}
	deletedAt := time.Now().Format(time.RFC3339)
	return &contracts.DeleteForumCommentResult{
		CommentID: resp.CommentId,
		Status:    resp.Status,
		Content:   "",
		DeletedAt: &deletedAt,
	}, nil
}

func (c *Client) ImportPost(ctx context.Context, req contracts.ImportForumPostRequest) (*contracts.ImportForumPostResult, error) {
	if err := c.ensureReady(); err != nil {
		return nil, err
	}
	resp, err := c.rpc.ImportPost(ctx, &pb.ImportForumPostRequest{
		ActorUserId:    req.ActorUserID,
		PostId:         req.PostID,
		TargetTitle:    req.TargetTitle,
		IdempotencyKey: req.IdempotencyKey,
	})
	if err != nil {
		return nil, responseFromRPCError(err)
	}
	if resp == nil {
		return nil, errors.New("taskclassforum zrpc service returned empty import post response")
	}
	return &contracts.ImportForumPostResult{
		ImportID:       resp.ImportId,
		PostID:         resp.PostId,
		NewTaskClassID: resp.NewTaskClassId,
		TaskClassTitle: resp.TaskClassTitle,
		ImportCount:    resp.ImportCount,
		CreatedAt:      resp.CreatedAt,
	}, nil
}

func (c *Client) ensureReady() error {
	if c == nil || c.rpc == nil {
		return errors.New("taskclassforum zrpc client is not initialized")
	}
	return nil
}

func normalizeEndpoints(values []string) []string {
	endpoints := make([]string, 0, len(values))
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed != "" {
			endpoints = append(endpoints, trimmed)
		}
	}
	return endpoints
}

func pageFromPB(page *pb.PageResponse) contracts.PageResult {
	if page == nil {
		return contracts.PageResult{}
	}
	return contracts.PageResult{
		Page:     int(page.Page),
		PageSize: int(page.PageSize),
		Total:    int(page.Total),
		HasMore:  page.HasMore,
	}
}

func forumUserFromPB(user *pb.UserBrief) contracts.UserBrief {
	if user == nil {
		return contracts.UserBrief{}
	}
	return contracts.UserBrief{
		UserID:    user.UserId,
		Nickname:  user.Nickname,
		AvatarURL: user.AvatarUrl,
	}
}

func forumTemplateSummaryFromPB(summary *pb.TemplateSummary) contracts.TemplateSummary {
	if summary == nil {
		return contracts.TemplateSummary{}
	}
	return contracts.TemplateSummary{
		TaskCount:      int(summary.TaskCount),
		Mode:           summary.Mode,
		StartDate:      summary.StartDate,
		EndDate:        summary.EndDate,
		StrategyLabels: append([]string(nil), summary.StrategyLabels...),
	}
}

func forumPostCountersFromPB(counters *pb.ForumPostCounters) contracts.ForumPostCounters {
	if counters == nil {
		return contracts.ForumPostCounters{}
	}
	return contracts.ForumPostCounters{
		LikeCount:    counters.LikeCount,
		CommentCount: counters.CommentCount,
		ImportCount:  counters.ImportCount,
	}
}

func forumPostViewerStateFromPB(state *pb.ForumPostViewerState) contracts.ForumPostViewerState {
	if state == nil {
		return contracts.ForumPostViewerState{}
	}
	return contracts.ForumPostViewerState{
		Liked:        state.Liked,
		ImportedOnce: state.ImportedOnce,
	}
}

func forumPostBriefFromPB(post *pb.ForumPostBrief) contracts.ForumPostBrief {
	if post == nil {
		return contracts.ForumPostBrief{}
	}
	return contracts.ForumPostBrief{
		PostID:          post.PostId,
		Title:           post.Title,
		Summary:         post.Summary,
		Tags:            append([]string(nil), post.Tags...),
		Author:          forumUserFromPB(post.Author),
		TemplateSummary: forumTemplateSummaryFromPB(post.TemplateSummary),
		Counters:        forumPostCountersFromPB(post.Counters),
		ViewerState:     forumPostViewerStateFromPB(post.ViewerState),
		Status:          post.Status,
		CreatedAt:       post.CreatedAt,
	}
}

func forumPostBriefsFromPB(items []*pb.ForumPostBrief) []contracts.ForumPostBrief {
	if len(items) == 0 {
		return []contracts.ForumPostBrief{}
	}
	result := make([]contracts.ForumPostBrief, 0, len(items))
	for _, item := range items {
		result = append(result, forumPostBriefFromPB(item))
	}
	return result
}

func forumTemplateDetailFromPB(detail *pb.TemplateDetail) contracts.TemplateDetail {
	if detail == nil {
		return contracts.TemplateDetail{}
	}
	items := make([]contracts.TemplateItemPreview, 0, len(detail.ItemsPreview))
	for _, item := range detail.ItemsPreview {
		if item == nil {
			continue
		}
		items = append(items, contracts.TemplateItemPreview{
			ItemID:  item.ItemId,
			Order:   int(item.Order),
			Content: item.Content,
		})
	}
	return contracts.TemplateDetail{
		Mode:           detail.Mode,
		StartDate:      detail.StartDate,
		EndDate:        detail.EndDate,
		StrategyLabels: append([]string(nil), detail.StrategyLabels...),
		TaskCount:      int(detail.TaskCount),
		ItemsPreview:   items,
	}
}

func forumPostDetailFromPB(detail *pb.ForumPostDetail) contracts.ForumPostDetail {
	if detail == nil {
		return contracts.ForumPostDetail{}
	}
	return contracts.ForumPostDetail{
		Post:     forumPostBriefFromPB(detail.Post),
		Template: forumTemplateDetailFromPB(detail.Template),
	}
}

func forumTagItemsFromPB(items []*pb.ForumTagItem) []contracts.ForumTagItem {
	if len(items) == 0 {
		return []contracts.ForumTagItem{}
	}
	result := make([]contracts.ForumTagItem, 0, len(items))
	for _, item := range items {
		if item == nil {
			continue
		}
		result = append(result, contracts.ForumTagItem{
			Tag:       item.Tag,
			PostCount: int(item.PostCount),
		})
	}
	return result
}

func forumCommentNodeFromPB(node *pb.ForumCommentNode) contracts.ForumCommentNode {
	if node == nil {
		return contracts.ForumCommentNode{}
	}
	children := make([]contracts.ForumCommentNode, 0, len(node.Children))
	for _, child := range node.Children {
		children = append(children, forumCommentNodeFromPB(child))
	}
	return contracts.ForumCommentNode{
		CommentID:       node.CommentId,
		PostID:          node.PostId,
		ParentCommentID: uint64PtrFromPositive(node.ParentCommentId),
		Content:         node.Content,
		Status:          node.Status,
		Author:          forumUserFromPB(node.Author),
		CanDelete:       node.CanDelete,
		CreatedAt:       node.CreatedAt,
		DeletedAt:       stringPtrFromNonEmpty(node.DeletedAt),
		Children:        children,
	}
}

func forumCommentNodesFromPB(items []*pb.ForumCommentNode) []contracts.ForumCommentNode {
	if len(items) == 0 {
		return []contracts.ForumCommentNode{}
	}
	result := make([]contracts.ForumCommentNode, 0, len(items))
	for _, item := range items {
		result = append(result, forumCommentNodeFromPB(item))
	}
	return result
}

func uint64FromPtr(value *uint64) uint64 {
	if value == nil {
		return 0
	}
	return *value
}

func uint64PtrFromPositive(value uint64) *uint64 {
	if value == 0 {
		return nil
	}
	result := value
	return &result
}

func stringPtrFromNonEmpty(value string) *string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return nil
	}
	return &trimmed
}
