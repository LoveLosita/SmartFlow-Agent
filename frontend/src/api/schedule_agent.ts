import http from '@/api/http'
import type { ApiResponse } from '@/types/api'
import type { PlacedItem, SchedulePreviewData } from '@/types/dashboard'
import { extractErrorMessage } from '@/utils/http'

export type ToolView = {
  view_type?: string
  version?: number
  collapsed?: Record<string, any>
  expanded?: Record<string, any>
}

export interface TimelineToolPayload {
  name: string
  status: 'start' | 'done' | 'blocked' | 'failed' | string
  summary: string
  arguments_preview?: string
  argument_view?: ToolView
  result_view?: ToolView
}

export interface TimelineConfirmPayload {
  interaction_id: string
  title: string
  summary: string
}

export interface ActiveSchedulePreviewTrigger {
  trigger_id: string
  trigger_type: string
  source: string
  target_type: string
  target_id: number
  requested_at: string
}

export interface ActiveSchedulePreviewEntry {
  entry_id: string
  source_type: string
  source_id: number
  title: string
  start_at?: string
  end_at?: string
  week?: number
  day_of_week?: number
  section_from?: number
  section_to?: number
  status: string
  editable: boolean
}

export interface ActiveSchedulePreviewVersion {
  title: string
  window_start?: string
  window_end?: string
  entries: ActiveSchedulePreviewEntry[]
  summary_lines: string[]
}

export interface ActiveSchedulePreviewSlot {
  week: number
  day_of_week: number
  section: number
}

export interface ActiveSchedulePreviewSlotSpan {
  start: ActiveSchedulePreviewSlot
  end: ActiveSchedulePreviewSlot
  duration_sections: number
}

export interface ActiveSchedulePreviewChangeItem {
  change_id: string
  change_type: string
  target_type: string
  target_id: number
  from_slot?: ActiveSchedulePreviewSlot
  to_slot?: ActiveSchedulePreviewSlotSpan
  duration_sections: number
  affected_event_ids: number[]
  edited_allowed: boolean
  metadata?: Record<string, string>
}

export interface ActiveSchedulePreviewCandidateTarget {
  target_type: string
  target_id: number
  title: string
}

export interface ActiveSchedulePreviewCandidate {
  candidate_id: string
  candidate_type: string
  title: string
  summary: string
  target: ActiveSchedulePreviewCandidateTarget
  changes: ActiveSchedulePreviewChangeItem[]
  before_summary: string
  after_summary: string
  risk: string
  score: number
  validation: Record<string, any>
  source: string
}

export interface ActiveSchedulePreviewDetail {
  preview_id: string
  status: string
  apply_status: string
  expires_at: string
  generated_at: string
  expired: boolean
  trigger: ActiveSchedulePreviewTrigger
  explanation: string
  notification_summary: string
  selected_candidate: ActiveSchedulePreviewCandidate
  candidates: ActiveSchedulePreviewCandidate[]
  decision: Record<string, any>
  metrics: Record<string, any>
  issues: Array<Record<string, any>>
  context_summary: Record<string, any>
  before: ActiveSchedulePreviewVersion
  after: ActiveSchedulePreviewVersion
  changes: ActiveSchedulePreviewChangeItem[]
  risk: Record<string, any>
  base_version: string
  can_confirm: boolean
  can_ignore: boolean
  trace_id: string
}

export interface ActiveScheduleConfirmChange {
  change_id?: string
  type: string
  target_type?: string
  target_id?: number
  task_id?: number
  event_id?: number
  week?: number
  day_of_week?: number
  section_from?: number
  section_to?: number
  duration_sections?: number
  makeup_for_event_id?: number
  source_event_id?: number
  slots?: ActiveSchedulePreviewSlot[]
  edited_allowed?: boolean
  metadata?: Record<string, string>
}

export interface ActiveScheduleConfirmRequest {
  candidate_id: string
  action: 'confirm'
  edited_changes?: ActiveScheduleConfirmChange[]
  idempotency_key: string
}

export interface ActiveScheduleConfirmResult {
  preview_id: string
  apply_id: string
  apply_status: string
  candidate_id: string
  request_hash?: string
  request_body_hash?: string
  skipped_changes?: Array<{
    change_id?: string
    change_type: string
    reason: string
  }>
  error_code?: string
  error_message?: string
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

export type BusinessCardType = 'task_query' | 'task_record' | 'active_schedule_preview'
export type TaskRecordSource = 'quick_note' | 'create_task'

export interface TimelineThinkingSummaryPayload {
  stage?: string
  block_id?: string
  display_mode?: 'append'
  summary_seq?: number
  detail_summary?: string
  duration_seconds?: number
  final?: boolean
}

export interface TimelineBusinessCardPayload {
  card_type: BusinessCardType
  title?: string
  summary?: string
  source?: TaskRecordSource
  data: TaskQueryCardData | TaskRecordCardData | ActiveSchedulePreviewDetail
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
    | 'thinking_summary'
  role?: 'user' | 'assistant'
  content?: string
  payload?: {
    /** @deprecated 仅供 Debug/mock 路径兼容；正式后端已经切到 thinking_summary 协议。 */
    reasoning_content?: string
    stage?: string
    block_id?: string
    display_mode?: 'card' | 'append'
    tool?: TimelineToolPayload
    confirm?: TimelineConfirmPayload
    business_card?: TimelineBusinessCardPayload
    summary_seq?: number
    detail_summary?: string
    duration_seconds?: number
    final?: boolean
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
  idempotencyKey: string,
): Promise<void> {
  try {
    await http.put<ApiResponse<void>>(
      '/task-class/apply-batch-into-schedule',
      {
        task_class_id: taskClassId,
        items,
      },
      {
        headers: {
          'X-Idempotency-Key': idempotencyKey,
        },
      },
    )
  } catch (error) {
    throw new Error(extractErrorMessage(error, '保存方案失败'))
  }
}

/**
 * 获取主动调度预览详情。
 */
export async function getActiveSchedulePreview(previewId: string): Promise<ActiveSchedulePreviewDetail> {
  try {
    const response = await http.get<ApiResponse<ActiveSchedulePreviewDetail>>(`/active-schedule/preview/${previewId}`)
    return response.data.data
  } catch (error) {
    throw new Error(extractErrorMessage(error, '获取主动调度预览失败'))
  }
}

/**
 * 确认并应用主动调度预览。
 */
export async function confirmActiveSchedulePreview(
  previewId: string,
  payload: ActiveScheduleConfirmRequest,
): Promise<ActiveScheduleConfirmResult> {
  try {
    const response = await http.post<ApiResponse<ActiveScheduleConfirmResult>>(
      `/active-schedule/preview/${previewId}/confirm`,
      payload,
    )
    return response.data.data
  } catch (error) {
    throw new Error(extractErrorMessage(error, '确认主动调度预览失败'))
  }
}
