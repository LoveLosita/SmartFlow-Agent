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
}

export type ThinkingModeType = 'auto' | 'true' | 'false'

export interface ChatRequestExtra {
  task_class_ids?: number[]
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

export interface HybridScheduleEntry {
  week: number
  day_of_week: number
  section_from: number
  section_to: number
  name: string
  type: 'course' | 'task'
  status: 'existing' | 'suggested'
  task_item_id: number
  task_class_id: number
  event_id: number
  can_be_embedded: boolean
  block_for_suggested: boolean
  context_tag: string
}

export interface ScheduleCandidatePlan {
  week: number
  events: TodayEvent[]
}

export interface SchedulePreviewData {
  conversation_id: string
  trace_id: string
  summary: string
  candidate_plans: ScheduleCandidatePlan[]
  hybrid_entries: HybridScheduleEntry[]
  task_class_ids: number[]
  generated_at: string
}

export interface PlacedItem {
  task_item_id: number
  week: number
  day_of_week: number
  start_section: number
  end_section: number
  embed_course_event_id?: number
}
