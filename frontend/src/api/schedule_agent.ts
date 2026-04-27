import http from '@/api/http'
import type { ApiResponse } from '@/types/api'
import type { PlacedItem, SchedulePreviewData } from '@/types/dashboard'
import { extractErrorMessage } from '@/utils/http'

export interface TimelineToolPayload {
  name: string
  status: 'start' | 'done' | 'blocked' | 'failed'
  summary: string
  arguments_preview?: string
}

export interface TimelineConfirmPayload {
  interaction_id: string
  title: string
  summary: string
}

export interface TaskQueryCardTaskItem {
  id: number
  title: string
  priority_group?: number
  priority_label?: string
  deadline_at?: string
  is_completed?: boolean
}

export interface TaskQueryCardFilter {
  key:
    | 'quadrant'
    | 'keyword'
    | 'deadline_after'
    | 'deadline_before'
    | 'include_completed'
    | 'sort'
  label: string
  value: string | number | boolean
  operator?: 'eq' | 'contains' | 'gte' | 'lt'
  display_text: string
}

export interface TaskQueryCardData {
  query_summary?: string
  query_filters?: TaskQueryCardFilter[]
  result_count: number
  shown_count: number
  has_more?: boolean
  tasks: TaskQueryCardTaskItem[]
}

export interface TaskRecordCardData {
  id?: number
  title: string
  priority_group?: number
  priority_label?: string
  deadline_at?: string
  urgency_threshold_at?: string
  status?: string
  created_at?: string
}

export type BusinessCardType = 'task_query' | 'task_record'
export type TaskRecordSource = 'quick_note' | 'create_task'

export interface TimelineBusinessCardPayload {
  card_type: BusinessCardType
  title?: string
  summary?: string
  source?: TaskRecordSource
  data: TaskQueryCardData | TaskRecordCardData
}

export interface TimelineEvent {
  id: number
  seq: number
  kind:
    | 'user_text'
    | 'assistant_text'
    | 'tool_call'
    | 'tool_result'
    | 'confirm_request'
    | 'schedule_completed'
    | 'interrupt'
    | 'status'
    | 'business_card'
  role?: 'user' | 'assistant'
  content?: string
  payload?: {
    reasoning_content?: string
    stage?: string
    block_id?: string
    display_mode?: 'card'
    tool?: TimelineToolPayload
    confirm?: TimelineConfirmPayload
    business_card?: TimelineBusinessCardPayload
  }
  tokens_consumed?: number
  created_at: string
}

/**
 * 获取排程预览数据
 */
export async function getSchedulePreview(conversationId: string): Promise<SchedulePreviewData> {
  try {
    const response = await http.get<ApiResponse<SchedulePreviewData>>('/agent/schedule-preview', {
      params: { conversation_id: conversationId },
    })
    return response.data.data
  } catch (error) {
    throw new Error(extractErrorMessage(error, '获取方案预览失败'))
  }
}

/**
 * 获取会话完整时间线
 */
export async function getConversationTimeline(conversationId: string): Promise<TimelineEvent[]> {
  try {
    const response = await http.get<ApiResponse<TimelineEvent[]>>('/agent/conversation-timeline', {
      params: { conversation_id: conversationId },
    })
    return response.data.data
  } catch (error) {
    throw new Error(extractErrorMessage(error, '获取会话时间线失败'))
  }
}

/**
 * 暂存排程状态到 Redis
 */
export async function saveScheduleState(conversationId: string, items: PlacedItem[]): Promise<void> {
  try {
    await http.post<ApiResponse<void>>('/agent/schedule-state', {
      conversation_id: conversationId,
      items,
    })
  } catch (error) {
    throw new Error(extractErrorMessage(error, '暂存方案失败'))
  }
}

/**
 * 将排程结果正式应用到数据库
 */
export async function applyBatchIntoSchedule(
  taskClassId: number,
  items: PlacedItem[],
  idempotencyKey: string
): Promise<void> {
  try {
    await http.put<ApiResponse<void>>('/task-class/apply-batch-into-schedule', {
      task_class_id: taskClassId,
      items,
    }, {
      headers: {
        'X-Idempotency-Key': idempotencyKey
      }
    })
  } catch (error) {
    throw new Error(extractErrorMessage(error, '保存方案失败'))
  }
}
