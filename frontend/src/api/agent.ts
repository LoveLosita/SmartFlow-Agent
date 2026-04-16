import http from '@/api/http'
import type { ApiResponse } from '@/types/api'
import type { ConversationContextStats, ConversationListResponse, ConversationMeta } from '@/types/dashboard'
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

function normalizeNonNegativeInteger(value: unknown) {
  if (typeof value !== 'number' || !Number.isFinite(value)) {
    return null
  }

  return Math.max(0, Math.round(value))
}

function normalizeConversationContextStats(raw: unknown): ConversationContextStats | null {
  // 1. 后端这里直接透传数据库中的 JSON，前端需要同时兜住 object / null / 空字符串三种返回。
  // 2. 若后端灰度期间字段缺失，则尽量使用四段消息之和回填 total，避免展示层继续散落兼容逻辑。
  // 3. budget 缺失时说明统计不完整，此时返回 null，让界面统一走“暂无统计”态更安全。
  if (raw == null || raw === '') {
    return null
  }

  let candidate: unknown = raw
  if (typeof candidate === 'string') {
    const trimmed = candidate.trim()
    if (!trimmed) {
      return null
    }

    try {
      candidate = JSON.parse(trimmed) as unknown
    } catch {
      return null
    }
  }

  if (!candidate || typeof candidate !== 'object') {
    return null
  }

  const stats = candidate as Record<string, unknown>
  const msg0 = normalizeNonNegativeInteger(stats.msg0) ?? 0
  const msg1 = normalizeNonNegativeInteger(stats.msg1) ?? 0
  const msg2 = normalizeNonNegativeInteger(stats.msg2) ?? 0
  const msg3 = normalizeNonNegativeInteger(stats.msg3) ?? 0
  const fallbackTotal = msg0 + msg1 + msg2 + msg3
  const total = normalizeNonNegativeInteger(stats.total) ?? fallbackTotal
  const budget = normalizeNonNegativeInteger(stats.budget)

  if (budget == null || budget <= 0) {
    return null
  }

  return {
    msg0,
    msg1,
    msg2,
    msg3,
    total: Math.max(total, fallbackTotal),
    budget,
  }
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

export async function getContextStats(conversationId: string) {
  try {
    const response = await http.get<ApiResponse<unknown>>('/agent/context-stats', {
      params: {
        conversation_id: conversationId,
      },
    })
    return normalizeConversationContextStats(response.data.data)
  } catch (error) {
    throw new Error(extractErrorMessage(error, '上下文窗口统计加载失败，请稍后重试'))
  }
}
