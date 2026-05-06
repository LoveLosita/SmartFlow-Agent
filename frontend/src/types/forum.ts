export interface ForumUserBrief {
  user_id: number
  nickname: string
  avatar_url: string
}

export interface ForumTemplateSummary {
  task_count: number
  mode: string
  start_date: string
  end_date: string
  strategy_labels: string[]
}

export interface ForumPostCounters {
  like_count: number
  comment_count: number
  import_count: number
}

export interface ForumPostViewerState {
  liked: boolean
  imported_once: boolean
}

export interface ForumPostBrief {
  post_id: number
  title: string
  summary: string
  tags: string[]
  author: ForumUserBrief
  template_summary: ForumTemplateSummary
  counters: ForumPostCounters
  viewer_state: ForumPostViewerState
  status: string
  created_at: string
}

export interface ForumTemplateItemPreview {
  item_id: number
  order: number
  content: string
}

export interface ForumTemplateDetail {
  mode: string
  start_date: string
  end_date: string
  strategy_labels: string[]
  task_count: number
  items_preview: ForumTemplateItemPreview[]
}

export interface ForumPostDetail {
  post: ForumPostBrief
  template: ForumTemplateDetail
}

export interface ForumCommentNode {
  comment_id: number
  post_id: number
  parent_comment_id: number | null
  content: string
  status: string
  author: ForumUserBrief
  can_delete: boolean
  created_at: string
  deleted_at: string | null
  children: ForumCommentNode[]
}

export interface ForumTagItem {
  tag: string
  post_count: number
}

export interface ForumPageEnvelope<T> {
  items: T[]
  page: number
  page_size: number
  total: number
  has_more: boolean
}

export interface ForumInteractionResult {
  post_id: number
  liked: boolean
  like_count: number
}

export interface ForumImportResult {
  import_id: number
  post_id: number
  new_task_class_id: number
  task_class_title: string
  import_count: number
  created_at: string
}

export interface ForumDeleteCommentResult {
  comment_id: number
  status: string
  content: string
  deleted_at: string | null
}

export interface ForumPostListQuery {
  page?: number
  page_size?: number
  sort?: string
  keyword?: string
  tag?: string
}

export interface ForumCommentListQuery {
  page?: number
  page_size?: number
  sort?: string
}

export interface CreateForumPostPayload {
  task_class_id: number
  title: string
  summary: string
  tags: string[]
}

export interface CreateForumCommentPayload {
  content: string
  parent_comment_id?: number | null
}
