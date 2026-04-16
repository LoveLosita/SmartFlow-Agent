export interface TaskItem {
  id: number
  user_id: number
  title: string
  priority_group: number
  status: string
  deadline: string
  is_completed: boolean
}

export interface TaskCreatePayload {
  title: string
  priority_group: number
  deadline_at?: string | null
}

export interface TaskCreateResult {
  id: number
  title: string
  priority_group: number
  deadline_at?: string | null
  status: string
  created_at: string
}

export interface TaskMutationResult {
  task_id: number
  is_completed: boolean
  already_completed?: boolean
  status: string
}

export interface TaskBrief {
  id: number
  name: string
  type: string
}

export interface TodayEvent {
  id: number
  order: number
  name: string
  start_time: string
  end_time: string
  location: string
  type: string
  span: number
  embedded_task_info?: TaskBrief
}

export interface TodaySchedule {
  day_of_week: number
  week: number
  events: TodayEvent[]
}

export interface ConversationListItem {
  conversation_id: string
  title: string
  has_title: boolean
  message_count: number
  last_message_at?: string | null
  status: string
  created_at?: string | null
}

export interface ConversationListResponse {
  list: ConversationListItem[]
  page: number
  page_size: number
  limit: number
  total: number
  has_more: boolean
}

export interface ConversationMeta {
  conversation_id: string
  title: string
  has_title: boolean
  message_count: number
  last_message_at?: string | null
  status: string
}

export interface AssistantMessage {
  id: string
  role: 'user' | 'assistant' | 'system'
  content: string
  createdAt: string
  reasoning?: string
  retryGroupId?: string
  retryIndex?: number
  retryTotal?: number
}

export type ThinkingModeType = 'auto' | 'true' | 'false'

export interface ChatRequestExtra {
  task_class_ids?: number[]
  request_mode?: 'retry'
  retry_group_id?: string
  retry_from_user_message_id?: string | number
  retry_from_assistant_message_id?: string | number
  confirm_action?: string
  always_execute?: boolean
  resume?: Record<string, unknown>
}

export interface ConversationContextStats {
  msg0: number
  msg1: number
  msg2: number
  msg3: number
  total: number
  budget: number
}

export interface ChatStreamRequest {
  conversation_id?: string
  message: string
  model?: string
  thinking?: ThinkingModeType
  extra?: ChatRequestExtra
}
