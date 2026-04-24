export type ScheduleEventType = 'course' | 'task' | 'empty'

export interface ScheduleEmbeddedTaskInfo {
  id: number
  name: string
  type: string
}

export interface ScheduleWeekEvent {
  id: number
  order: number
  day_of_week: number
  name: string
  start_time: string
  end_time: string
  location: string
  type: ScheduleEventType | string
  span: number
  status?: string
  embedded_task_info: ScheduleEmbeddedTaskInfo
}

export interface ScheduleWeekData {
  week: number
  events: ScheduleWeekEvent[]
}

export interface TaskClassListItem {
  id: number
  name: string
  mode: string
  strategy: string
  start_date: string
  end_date: string
  total_slots: number
}

export interface TaskClassEmbeddedTime {
  date: string
  section_from: number
  section_to: number
}

export interface TaskClassDetailItem {
  id?: number
  order: number
  content: string
  embedded_time: TaskClassEmbeddedTime | null
}

export interface TaskClassConfig {
  total_slots: number
  allow_filler_course: boolean
  strategy: string
  excluded_slots: number[]
}

export interface TaskClassDetail {
  name: string
  start_date: string
  end_date: string
  mode: string
  config: TaskClassConfig
  items: TaskClassDetailItem[]
}

export interface TaskClassCreateItemPayload {
  order: number
  content: string
  embedded_time: TaskClassEmbeddedTime | null
}

export interface TaskClassCreatePayload {
  name: string
  start_date: string
  end_date: string
  mode: string
  config: TaskClassConfig
  items: TaskClassCreateItemPayload[]
}

export interface SmartPlanningMultiPayload {
  task_class_ids: number[]
}

export interface ApplyBatchIntoScheduleItem {
  task_item_id: number
  week: number
  day_of_week: number
  start_section: number
  end_section: number
  embed_course_event_id: number
}

export interface ScheduleDeletePayloadItem {
  id: number
  delete_course: boolean
  delete_embedded_task: boolean
}

export interface CourseDraftRow {
  row_id: string
  course_name: string
  location: string
  is_allow_tasks: boolean
  start_week: number | null
  end_week: number | null
  day_of_week: number | null
  start_section: number | null
  end_section: number | null
  week_type: 'all' | 'odd' | 'even' | ''
  confidence: number
  raw_text: string
  row_warnings: string[]
}

export interface CourseImportPayload {
  courses: Array<{
    course_name: string
    location: string
    is_allow_tasks: boolean
    arrangements: Array<{
      start_week: number
      end_week: number
      day_of_week: number
      start_section: number
      end_section: number
      week_type: 'all' | 'odd' | 'even'
    }>
  }>
}

export interface CourseImageParseResponse {
  draft_status: 'success' | 'partial' | 'reject'
  message: string
  warnings: string[]
  rows: CourseDraftRow[]
}
