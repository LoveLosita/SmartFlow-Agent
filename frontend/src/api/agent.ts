import http from '@/api/http'
import type { ApiResponse } from '@/types/api'
import type { ConversationListResponse, ConversationMeta } from '@/types/dashboard'
import { extractErrorMessage } from '@/utils/http'

const conversationHistoryPath = '/agent/conversation-history'

export interface ConversationHistoryMessage {
  id?: string | number
  role: 'user' | 'assistant' | 'system'
  content: string
  created_at?: string | null
  reasoning_content?: string | null
  reasoning_duration_seconds?: number | null
  retry_group_id?: string | null
  retry_index?: number | null
  retry_total?: number | null
}

export interface ConversationListQuery {
  page?: number
  pageSize?: number
  status?: 'active' | 'archived'
}

function normalizeConversationHistoryMessage(raw: unknown): ConversationHistoryMessage | null {
  if (!raw || typeof raw !== 'object') {
    return null
  }

  const candidate = raw as Record<string, unknown>
  const role = candidate.role
  const content = candidate.content

  if ((role !== 'user' && role !== 'assistant' && role !== 'system') || typeof content !== 'string') {
    return null
  }

  // 1. 按 openapi 优先读取 reasoning_content，兼容后端历史接口新增的思考存储字段。
  // 2. 若后端灰度期间仍返回 legacy reasoning 字段，这里也做一次前端兜底兼容。
  // 3. 统一归一化成 string/null，避免页面层反复做类型分支判断。
  const normalizedReasoning =
    typeof candidate.reasoning_content === 'string'
      ? candidate.reasoning_content
      : typeof candidate.reasoning === 'string'
        ? candidate.reasoning
        : null

  return {
    id: typeof candidate.id === 'string' || typeof candidate.id === 'number' ? candidate.id : undefined,
    role,
    content,
    created_at: typeof candidate.created_at === 'string' ? candidate.created_at : null,
    reasoning_content: normalizedReasoning,
    reasoning_duration_seconds:
      typeof candidate.reasoning_duration_seconds === 'number' ? candidate.reasoning_duration_seconds : null,
    retry_group_id: typeof candidate.retry_group_id === 'string' ? candidate.retry_group_id : null,
    retry_index: typeof candidate.retry_index === 'number' ? candidate.retry_index : null,
    retry_total: typeof candidate.retry_total === 'number' ? candidate.retry_total : null,
  }
}

// getConversationList 负责按 openapi 约定读取会话列表分页。
// 职责边界：
// 1. 负责把前端分页参数映射为后端要求的 page/page_size。
// 2. 不负责前端滚动懒加载时的合并、去重和选中逻辑。
// 3. 接口失败时统一抛出中文错误，便于页面层直接提示。
export async function getConversationList(options: ConversationListQuery = {}) {
  const { page = 1, pageSize = 20, status = 'active' } = options

  try {
    const response = await http.get<ApiResponse<ConversationListResponse>>('/agent/conversation-list', {
      params: {
        page,
        page_size: pageSize,
        limit: pageSize,
        status,
      },
    })
    return response.data.data
  } catch (error) {
    throw new Error(extractErrorMessage(error, '会话列表加载失败，请稍后重试'))
  }
}

export async function getConversationMeta(conversationId: string) {
  try {
    const response = await http.get<ApiResponse<ConversationMeta>>('/agent/conversation-meta', {
      params: {
        conversation_id: conversationId,
      },
    })
    return response.data.data
  } catch (error) {
    throw new Error(extractErrorMessage(error, '会话信息加载失败，请稍后重试'))
  }
}

export async function getConversationHistory(conversationId: string) {
  try {
    const response = await http.get<ApiResponse<ConversationHistoryMessage[]>>(conversationHistoryPath, {
      params: {
        conversation_id: conversationId,
      },
    })
    return (response.data.data ?? [])
      .map(normalizeConversationHistoryMessage)
      .filter((message): message is ConversationHistoryMessage => Boolean(message))
  } catch (error) {
    throw new Error(extractErrorMessage(error, '会话消息加载失败，请稍后重试'))
  }
}
