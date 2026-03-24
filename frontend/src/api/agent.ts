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
  reasoning_content?: string
}

export interface ConversationListQuery {
  page?: number
  pageSize?: number
  status?: 'active' | 'archived'
}

// getConversationList 负责按 openapi 约定读取会话列表分页。
// 职责边界：
// 1. 负责把前端分页参数映射为后端要求的 page/limit。
// 2. 不负责前端滚动懒加载时的合并、去重和选中逻辑。
// 3. 接口失败时统一抛出中文错误，便于页面层直接提示。
export async function getConversationList(options: ConversationListQuery = {}) {
  const { page = 1, pageSize = 20, status = 'active' } = options

  try {
    const response = await http.get<ApiResponse<ConversationListResponse>>('/agent/conversation-list', {
      params: {
        page,
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
    return response.data.data ?? []
  } catch (error) {
    throw new Error(extractErrorMessage(error, '会话消息加载失败，请稍后重试'))
  }
}
