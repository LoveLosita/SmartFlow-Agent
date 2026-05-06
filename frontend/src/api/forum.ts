import http from '@/api/http'
import type { ApiResponse } from '@/types/api'
import type {
  CreateForumCommentPayload,
  CreateForumPostPayload,
  ForumCommentListQuery,
  ForumCommentNode,
  ForumDeleteCommentResult,
  ForumImportResult,
  ForumInteractionResult,
  ForumPageEnvelope,
  ForumPostBrief,
  ForumPostDetail,
  ForumPostListQuery,
  ForumTagItem,
} from '@/types/forum'
import { createIdempotencyKey } from '@/utils/idempotency'
import { extractErrorMessage } from '@/utils/http'

interface ForumTagsEnvelope {
  items: ForumTagItem[]
}

export async function listForumPosts(query: ForumPostListQuery = {}) {
  try {
    const response = await http.get<ApiResponse<ForumPageEnvelope<ForumPostBrief>>>('/plan-square/posts', {
      params: query,
    })
    return response.data.data
  } catch (error) {
    throw new Error(extractErrorMessage(error, '计划广场列表加载失败，请稍后重试'))
  }
}

export async function listForumTags(limit = 20) {
  try {
    const response = await http.get<ApiResponse<ForumTagsEnvelope>>('/plan-square/tags', {
      params: { limit },
    })
    return response.data.data?.items ?? []
  } catch (error) {
    throw new Error(extractErrorMessage(error, '计划广场标签加载失败，请稍后重试'))
  }
}

export async function createForumPost(
  payload: CreateForumPostPayload,
  idempotencyKey = createIdempotencyKey('forum-post-create'),
) {
  try {
    const response = await http.post<ApiResponse<ForumPostBrief>>('/plan-square/posts', payload, {
      headers: {
        'X-Idempotency-Key': idempotencyKey,
      },
    })
    return response.data.data
  } catch (error) {
    throw new Error(extractErrorMessage(error, '发布计划失败，请稍后重试'))
  }
}

export async function getForumPostDetail(postId: number) {
  try {
    const response = await http.get<ApiResponse<ForumPostDetail>>(`/plan-square/posts/${postId}`)
    return response.data.data
  } catch (error) {
    throw new Error(extractErrorMessage(error, '计划详情加载失败，请稍后重试'))
  }
}

export async function likeForumPost(postId: number) {
  try {
    const response = await http.post<ApiResponse<ForumInteractionResult>>(`/plan-square/posts/${postId}/like`)
    return response.data.data
  } catch (error) {
    throw new Error(extractErrorMessage(error, '点赞失败，请稍后重试'))
  }
}

export async function unlikeForumPost(postId: number) {
  try {
    const response = await http.delete<ApiResponse<ForumInteractionResult>>(`/plan-square/posts/${postId}/like`)
    return response.data.data
  } catch (error) {
    throw new Error(extractErrorMessage(error, '取消点赞失败，请稍后重试'))
  }
}

export async function listForumComments(postId: number, query: ForumCommentListQuery = {}) {
  try {
    const response = await http.get<ApiResponse<ForumPageEnvelope<ForumCommentNode>>>(`/plan-square/posts/${postId}/comments`, {
      params: query,
    })
    return response.data.data
  } catch (error) {
    throw new Error(extractErrorMessage(error, '评论列表加载失败，请稍后重试'))
  }
}

export async function createForumComment(
  postId: number,
  payload: CreateForumCommentPayload,
  idempotencyKey = createIdempotencyKey('forum-comment-create'),
) {
  try {
    const response = await http.post<ApiResponse<ForumCommentNode>>(`/plan-square/posts/${postId}/comments`, payload, {
      headers: {
        'X-Idempotency-Key': idempotencyKey,
      },
    })
    return response.data.data
  } catch (error) {
    throw new Error(extractErrorMessage(error, '发表评论失败，请稍后重试'))
  }
}

export async function deleteForumComment(commentId: number) {
  try {
    const response = await http.delete<ApiResponse<ForumDeleteCommentResult>>(`/plan-square/comments/${commentId}`)
    return response.data.data
  } catch (error) {
    throw new Error(extractErrorMessage(error, '删除评论失败，请稍后重试'))
  }
}

export async function importForumPost(
  postId: number,
  targetTitle = '',
  idempotencyKey = createIdempotencyKey('forum-post-import'),
) {
  try {
    const response = await http.post<ApiResponse<ForumImportResult>>(
      `/plan-square/posts/${postId}/import`,
      { target_title: targetTitle },
      {
        headers: {
          'X-Idempotency-Key': idempotencyKey,
        },
      },
    )
    return response.data.data
  } catch (error) {
    throw new Error(extractErrorMessage(error, '导入计划失败，请稍后重试'))
  }
}
