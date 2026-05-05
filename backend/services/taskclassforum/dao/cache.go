package dao

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	forumcontracts "github.com/LoveLosita/smartflow/backend/shared/contracts/taskclassforum"
	"github.com/go-redis/redis/v8"
)

const commentTreeCacheTTL = 2 * time.Minute

type commentTreeCachePayload struct {
	Items []forumcontracts.ForumCommentNode `json:"items"`
	Page  forumcontracts.PageResult         `json:"page"`
}

// CommentTreeCache 承载计划广场评论树的 Redis 缓存能力。
//
// 职责边界：
// 1. 只负责评论树读模型的 JSON 缓存和版本号失效，不读写 MySQL；
// 2. 不计算当前用户是否可删除评论，避免把用户视角写进共享缓存；
// 3. Redis 异常向上返回，由 service 层决定是否降级回源 DB。
type CommentTreeCache struct {
	client *redis.Client
	ttl    time.Duration
}

func NewCommentTreeCache(client *redis.Client) *CommentTreeCache {
	return &CommentTreeCache{
		client: client,
		ttl:    commentTreeCacheTTL,
	}
}

func commentTreeVersionKey(postID uint64) string {
	return fmt.Sprintf("forum:comments:%d:version", postID)
}

func commentTreeDataKey(postID uint64, version int64, sort string, page int, pageSize int) string {
	return fmt.Sprintf(
		"forum:comments:%d:v%d:sort:%s:page:%d:size:%d",
		postID,
		version,
		strings.TrimSpace(sort),
		page,
		pageSize,
	)
}

// GetCommentTree 读取指定帖子、排序和分页维度下的评论树缓存。
//
// 返回值语义：
// 1. hit=true 表示命中缓存，items/page 可直接用于返回前的用户视角补全；
// 2. hit=false 且 error=nil 表示未命中，调用方应回源 DB；
// 3. error 非空表示 Redis 或 JSON 异常，调用方应记录日志并回源 DB。
func (c *CommentTreeCache) GetCommentTree(ctx context.Context, postID uint64, page int, pageSize int, sort string) ([]forumcontracts.ForumCommentNode, forumcontracts.PageResult, bool, error) {
	if c == nil || c.client == nil {
		return nil, forumcontracts.PageResult{}, false, errors.New("评论树缓存未初始化")
	}
	version, err := c.currentCommentTreeVersion(ctx, postID)
	if err != nil {
		return nil, forumcontracts.PageResult{}, false, err
	}

	raw, err := c.client.Get(ctx, commentTreeDataKey(postID, version, sort, page, pageSize)).Result()
	if errors.Is(err, redis.Nil) {
		return nil, forumcontracts.PageResult{}, false, nil
	}
	if err != nil {
		return nil, forumcontracts.PageResult{}, false, err
	}

	var payload commentTreeCachePayload
	if err = json.Unmarshal([]byte(raw), &payload); err != nil {
		return nil, forumcontracts.PageResult{}, false, err
	}
	if payload.Items == nil {
		payload.Items = []forumcontracts.ForumCommentNode{}
	}
	return payload.Items, payload.Page, true, nil
}

// SetCommentTree 写入指定帖子、排序和分页维度下的评论树缓存。
//
// 步骤说明：
// 1. 先读取当前版本号，保证写入 key 与后续读取 key 一致；
// 2. 再序列化去个性化后的评论树，避免缓存里带入某个用户的 can_delete；
// 3. 最后写入短 TTL，让版本失效失败时也能靠自然过期兜底。
func (c *CommentTreeCache) SetCommentTree(ctx context.Context, postID uint64, page int, pageSize int, sort string, items []forumcontracts.ForumCommentNode, pageResult forumcontracts.PageResult) error {
	if c == nil || c.client == nil {
		return errors.New("评论树缓存未初始化")
	}
	version, err := c.currentCommentTreeVersion(ctx, postID)
	if err != nil {
		return err
	}

	if items == nil {
		items = []forumcontracts.ForumCommentNode{}
	}
	data, err := json.Marshal(commentTreeCachePayload{
		Items: items,
		Page:  pageResult,
	})
	if err != nil {
		return err
	}
	return c.client.Set(ctx, commentTreeDataKey(postID, version, sort, page, pageSize), data, c.ttl).Err()
}

// BumpCommentTreeVersion 递增帖子评论树版本号，让旧分页缓存自然失效。
//
// 职责边界：
// 1. 只做版本递增，不扫描删除旧 data key，避免写评论时阻塞 Redis；
// 2. 旧 data key 依赖短 TTL 自动回收；
// 3. 当 version key 不存在时 INCR 会从 1 开始，能够让默认 v0 缓存失效。
func (c *CommentTreeCache) BumpCommentTreeVersion(ctx context.Context, postID uint64) error {
	if c == nil || c.client == nil {
		return errors.New("评论树缓存未初始化")
	}
	return c.client.Incr(ctx, commentTreeVersionKey(postID)).Err()
}

func (c *CommentTreeCache) currentCommentTreeVersion(ctx context.Context, postID uint64) (int64, error) {
	raw, err := c.client.Get(ctx, commentTreeVersionKey(postID)).Result()
	if errors.Is(err, redis.Nil) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	version, err := strconv.ParseInt(strings.TrimSpace(raw), 10, 64)
	if err != nil {
		return 0, err
	}
	if version < 0 {
		return 0, nil
	}
	return version, nil
}
