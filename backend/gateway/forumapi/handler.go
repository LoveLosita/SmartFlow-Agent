package forumapi

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/LoveLosita/smartflow/backend/respond"
	contracts "github.com/LoveLosita/smartflow/backend/shared/contracts/taskclassforum"
	"github.com/gin-gonic/gin"
)

const requestTimeout = 2 * time.Second

type ForumClient interface {
	ListPosts(ctx context.Context, actorUserID uint64, page int, pageSize int, sort string, keyword string, tag string) ([]contracts.ForumPostBrief, contracts.PageResult, error)
	ListTags(ctx context.Context, actorUserID uint64, limit int) ([]contracts.ForumTagItem, error)
	CreatePost(ctx context.Context, req contracts.CreateForumPostRequest) (*contracts.ForumPostBrief, error)
	GetPost(ctx context.Context, actorUserID uint64, postID uint64) (*contracts.ForumPostDetail, error)
	LikePost(ctx context.Context, actorUserID uint64, postID uint64) (contracts.ForumPostCounters, contracts.ForumPostViewerState, error)
	UnlikePost(ctx context.Context, actorUserID uint64, postID uint64) (contracts.ForumPostCounters, contracts.ForumPostViewerState, error)
	ListComments(ctx context.Context, actorUserID uint64, postID uint64, page int, pageSize int, sort string) ([]contracts.ForumCommentNode, contracts.PageResult, error)
	CreateComment(ctx context.Context, req contracts.CreateForumCommentRequest) (*contracts.ForumCommentNode, error)
	DeleteComment(ctx context.Context, actorUserID uint64, commentID uint64) (*contracts.DeleteForumCommentResult, error)
	ImportPost(ctx context.Context, req contracts.ImportForumPostRequest) (*contracts.ImportForumPostResult, error)
}

type Handler struct {
	client ForumClient
}

func NewHandler(client ForumClient) *Handler {
	return &Handler{client: client}
}

type pageEnvelope[T any] struct {
	Items    []T  `json:"items"`
	Page     int  `json:"page"`
	PageSize int  `json:"page_size"`
	Total    int  `json:"total"`
	HasMore  bool `json:"has_more"`
}

type interactionEnvelope struct {
	PostID     uint64      `json:"post_id"`
	Liked      bool        `json:"liked"`
	LikeCount  int64       `json:"like_count"`
	RewardHint *rewardHint `json:"reward_hint,omitempty"`
}

type rewardHint struct {
	Receiver string `json:"receiver"`
	Status   string `json:"status"`
	Amount   int64  `json:"amount"`
}

type nextAction struct {
	Type        string `json:"type"`
	TaskClassID uint64 `json:"task_class_id"`
}

type importEnvelope struct {
	ImportID       uint64     `json:"import_id"`
	PostID         uint64     `json:"post_id"`
	NewTaskClassID uint64     `json:"new_task_class_id"`
	TaskClassTitle string     `json:"task_class_title"`
	ImportCount    int64      `json:"import_count"`
	RewardHint     rewardHint `json:"reward_hint"`
	NextAction     nextAction `json:"next_action"`
	CreatedAt      string     `json:"created_at"`
}

type deleteCommentEnvelope struct {
	CommentID uint64  `json:"comment_id"`
	Status    string  `json:"status"`
	Content   string  `json:"content"`
	DeletedAt *string `json:"deleted_at"`
}

type createPostBody struct {
	TaskClassID uint64   `json:"task_class_id"`
	Title       string   `json:"title"`
	Summary     string   `json:"summary"`
	Tags        []string `json:"tags"`
}

type createCommentBody struct {
	Content         string  `json:"content"`
	ParentCommentID *uint64 `json:"parent_comment_id"`
}

type importPostBody struct {
	TargetTitle string `json:"target_title"`
}

func (h *Handler) ListPosts(c *gin.Context) {
	client, ok := h.ready(c)
	if !ok {
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), requestTimeout)
	defer cancel()

	pageValue, ok := intQuery(c, "page")
	if !ok {
		return
	}
	pageSize, ok := intQuery(c, "page_size")
	if !ok {
		return
	}
	items, page, err := client.ListPosts(
		ctx,
		currentUserID(c),
		pageValue,
		pageSize,
		c.Query("sort"),
		c.Query("keyword"),
		c.Query("tag"),
	)
	if err != nil {
		respond.DealWithError(c, err)
		return
	}
	c.JSON(http.StatusOK, respond.RespWithData(respond.Ok, newPageEnvelope(items, page)))
}

func (h *Handler) ListTags(c *gin.Context) {
	client, ok := h.ready(c)
	if !ok {
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), requestTimeout)
	defer cancel()

	limit, ok := intQuery(c, "limit")
	if !ok {
		return
	}
	items, err := client.ListTags(ctx, currentUserID(c), limit)
	if err != nil {
		respond.DealWithError(c, err)
		return
	}
	c.JSON(http.StatusOK, respond.RespWithData(respond.Ok, gin.H{"items": items}))
}

func (h *Handler) CreatePost(c *gin.Context) {
	client, ok := h.ready(c)
	if !ok {
		return
	}
	var body createPostBody
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, respond.WrongParamType)
		return
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), requestTimeout)
	defer cancel()
	post, err := client.CreatePost(ctx, contracts.CreateForumPostRequest{
		ActorUserID:    currentUserID(c),
		TaskClassID:    body.TaskClassID,
		Title:          body.Title,
		Summary:        body.Summary,
		Tags:           append([]string(nil), body.Tags...),
		IdempotencyKey: strings.TrimSpace(c.GetHeader("X-Idempotency-Key")),
	})
	if err != nil {
		respond.DealWithError(c, err)
		return
	}
	c.JSON(http.StatusOK, respond.RespWithData(respond.Ok, post))
}

func (h *Handler) GetPost(c *gin.Context) {
	client, ok := h.ready(c)
	if !ok {
		return
	}
	postID, ok := uint64Param(c, "post_id")
	if !ok {
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), requestTimeout)
	defer cancel()

	detail, err := client.GetPost(ctx, currentUserID(c), postID)
	if err != nil {
		respond.DealWithError(c, err)
		return
	}
	c.JSON(http.StatusOK, respond.RespWithData(respond.Ok, detail))
}

func (h *Handler) LikePost(c *gin.Context) {
	client, ok := h.ready(c)
	if !ok {
		return
	}
	postID, ok := uint64Param(c, "post_id")
	if !ok {
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), requestTimeout)
	defer cancel()

	counters, state, err := client.LikePost(ctx, currentUserID(c), postID)
	if err != nil {
		respond.DealWithError(c, err)
		return
	}
	c.JSON(http.StatusOK, respond.RespWithData(respond.Ok, interactionEnvelope{
		PostID:    postID,
		Liked:     state.Liked,
		LikeCount: counters.LikeCount,
		RewardHint: &rewardHint{
			Receiver: "author",
			Status:   "recorded",
			Amount:   1,
		},
	}))
}

func (h *Handler) UnlikePost(c *gin.Context) {
	client, ok := h.ready(c)
	if !ok {
		return
	}
	postID, ok := uint64Param(c, "post_id")
	if !ok {
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), requestTimeout)
	defer cancel()

	counters, state, err := client.UnlikePost(ctx, currentUserID(c), postID)
	if err != nil {
		respond.DealWithError(c, err)
		return
	}
	c.JSON(http.StatusOK, respond.RespWithData(respond.Ok, interactionEnvelope{
		PostID:    postID,
		Liked:     state.Liked,
		LikeCount: counters.LikeCount,
	}))
}

func (h *Handler) ListComments(c *gin.Context) {
	client, ok := h.ready(c)
	if !ok {
		return
	}
	postID, ok := uint64Param(c, "post_id")
	if !ok {
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), requestTimeout)
	defer cancel()

	pageValue, ok := intQuery(c, "page")
	if !ok {
		return
	}
	pageSize, ok := intQuery(c, "page_size")
	if !ok {
		return
	}
	items, page, err := client.ListComments(ctx, currentUserID(c), postID, pageValue, pageSize, c.Query("sort"))
	if err != nil {
		respond.DealWithError(c, err)
		return
	}
	c.JSON(http.StatusOK, respond.RespWithData(respond.Ok, newPageEnvelope(items, page)))
}

func (h *Handler) CreateComment(c *gin.Context) {
	client, ok := h.ready(c)
	if !ok {
		return
	}
	postID, ok := uint64Param(c, "post_id")
	if !ok {
		return
	}
	var body createCommentBody
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, respond.WrongParamType)
		return
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), requestTimeout)
	defer cancel()
	comment, err := client.CreateComment(ctx, contracts.CreateForumCommentRequest{
		ActorUserID:     currentUserID(c),
		PostID:          postID,
		Content:         body.Content,
		ParentCommentID: body.ParentCommentID,
		IdempotencyKey:  strings.TrimSpace(c.GetHeader("X-Idempotency-Key")),
	})
	if err != nil {
		respond.DealWithError(c, err)
		return
	}
	c.JSON(http.StatusOK, respond.RespWithData(respond.Ok, comment))
}

func (h *Handler) DeleteComment(c *gin.Context) {
	client, ok := h.ready(c)
	if !ok {
		return
	}
	commentID, ok := uint64Param(c, "comment_id")
	if !ok {
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), requestTimeout)
	defer cancel()

	result, err := client.DeleteComment(ctx, currentUserID(c), commentID)
	if err != nil {
		respond.DealWithError(c, err)
		return
	}
	c.JSON(http.StatusOK, respond.RespWithData(respond.Ok, deleteCommentEnvelope{
		CommentID: result.CommentID,
		Status:    result.Status,
		Content:   result.Content,
		DeletedAt: result.DeletedAt,
	}))
}

func (h *Handler) ImportPost(c *gin.Context) {
	client, ok := h.ready(c)
	if !ok {
		return
	}
	postID, ok := uint64Param(c, "post_id")
	if !ok {
		return
	}
	var body importPostBody
	if err := c.ShouldBindJSON(&body); err != nil && !errors.Is(err, io.EOF) {
		c.JSON(http.StatusBadRequest, respond.WrongParamType)
		return
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), requestTimeout)
	defer cancel()
	result, err := client.ImportPost(ctx, contracts.ImportForumPostRequest{
		ActorUserID:    currentUserID(c),
		PostID:         postID,
		TargetTitle:    body.TargetTitle,
		IdempotencyKey: strings.TrimSpace(c.GetHeader("X-Idempotency-Key")),
	})
	if err != nil {
		respond.DealWithError(c, err)
		return
	}
	c.JSON(http.StatusOK, respond.RespWithData(respond.Ok, importEnvelope{
		ImportID:       result.ImportID,
		PostID:         result.PostID,
		NewTaskClassID: result.NewTaskClassID,
		TaskClassTitle: result.TaskClassTitle,
		ImportCount:    result.ImportCount,
		RewardHint: rewardHint{
			Receiver: "author",
			Status:   "recorded",
			Amount:   2,
		},
		NextAction: nextAction{
			Type:        "open_task_class",
			TaskClassID: result.NewTaskClassID,
		},
		CreatedAt: result.CreatedAt,
	}))
}

func (h *Handler) ready(c *gin.Context) (ForumClient, bool) {
	if h == nil || h.client == nil {
		c.JSON(http.StatusInternalServerError, respond.InternalError(errors.New("计划广场 gateway client 未初始化")))
		return nil, false
	}
	return h.client, true
}

func currentUserID(c *gin.Context) uint64 {
	userID := c.GetInt("user_id")
	if userID <= 0 {
		return 0
	}
	return uint64(userID)
}

func newPageEnvelope[T any](items []T, page contracts.PageResult) pageEnvelope[T] {
	return pageEnvelope[T]{
		Items:    items,
		Page:     page.Page,
		PageSize: page.PageSize,
		Total:    page.Total,
		HasMore:  page.HasMore,
	}
}

func intQuery(c *gin.Context, key string) (int, bool) {
	raw := strings.TrimSpace(c.Query(key))
	if raw == "" {
		return 0, true
	}
	value, err := strconv.Atoi(raw)
	if err != nil {
		c.JSON(http.StatusBadRequest, respond.WrongParamType)
		return 0, false
	}
	return value, true
}

func uint64Param(c *gin.Context, key string) (uint64, bool) {
	value, err := strconv.ParseUint(strings.TrimSpace(c.Param(key)), 10, 64)
	if err != nil || value == 0 {
		c.JSON(http.StatusBadRequest, respond.WrongParamType)
		return 0, false
	}
	return value, true
}
