import type { TimelineBusinessCardPayload, ToolView } from '@/api/schedule_agent'
import type {
  AssistantMessage,
  ConversationListItem,
  SchedulePreviewData,
  ThinkingSummaryPayload,
} from '@/types/dashboard'

export interface StreamDeltaPayload {
  content?: string
}

export interface StreamChoicePayload {
  delta?: StreamDeltaPayload
  finish_reason?: string | null
}

export interface StreamErrorPayload {
  message?: string
}

export interface StreamConfirmPayload {
  interaction_id?: string
  title?: string
  summary?: string
}

export interface StreamStatusExtraPayload {
  code?: string
  summary?: string
}

export interface StreamToolExtraPayload {
  name?: string
  status?: string
  summary?: string
  arguments_preview?: string
  argument_view?: ToolView
  result_view?: ToolView
}

export interface StreamExtraPayload {
  kind?: string
  block_id?: string
  stage?: string
  status?: StreamStatusExtraPayload
  tool?: StreamToolExtraPayload
  confirm?: StreamConfirmPayload
  business_card?: TimelineBusinessCardPayload
  thinking_summary?: ThinkingSummaryPayload
}

export interface StreamEventPayload {
  choices?: StreamChoicePayload[]
  delta?: StreamDeltaPayload
  content?: string
  finish_reason?: string | null
  error?: StreamErrorPayload
  extra?: StreamExtraPayload
}

export type ToolTraceState = 'called' | 'completed' | 'create' | 'blocked'

export interface ToolTraceEvent {
  id: string
  seq: number
  state: ToolTraceState
  summary: string
  detail?: string
  toolName?: string
  argumentView?: ToolView
  resultView?: ToolView
}

export interface StatusTraceEvent {
  id: string
  seq: number
  code: string
  stage: string
  summary: string
}

export interface ConversationGroup {
  key: string
  label: string
  items: ConversationListItem[]
}

export interface ConfirmOverlayState {
  visible: boolean
  manuallyClosed: boolean
  interactionId: string
  title: string
  summary: string
}

export interface ConversationListItemRevealOptions {
  animate?: boolean
}

export interface EnsureConversationMetaOptions {
  forceReload?: boolean
  syncListItem?: boolean
  listItemReveal?: ConversationListItemRevealOptions
}

// 展示用消息：合并连续 assistant 消息后的视图模型。
export interface DisplayMessage {
  /** 第一条源消息的 id，用作 Vue key。 */
  id: string
  role: 'user' | 'assistant' | 'system'
  /** 合并后的正文内容。 */
  content: string
  /** 最后一条源消息的时间。 */
  createdAt: string
  /** 合并后的推理内容。 */
  reasoning?: string
  /** 原始消息引用列表。 */
  sources: AssistantMessage[]
  /** 是否为多条合并。 */
  merged: boolean
}

export interface DisplayAssistantBlock {
  id: string
  type: 'tool' | 'status' | 'reasoning' | 'content' | 'content_indicator' | 'schedule_card' | 'business_card'
  seq: number
  text?: string
  event?: ToolTraceEvent
  statusEvent?: StatusTraceEvent
  schedulePreview?: SchedulePreviewData
  businessCard?: TimelineBusinessCardPayload
  /** 所属的源消息 ID，用于状态查询。 */
  sourceId?: string
  /** 所属的源消息引用，用于渲染辅助信息。 */
  source?: AssistantMessage
}

export interface AssistantContentBlock {
  id: string
  seq: number
  text: string
}

export interface ThinkingSummaryBlockState {
  blockId: string
  lastSummarySeq: number
  lastSignature: string
  finished: boolean
}

export interface ApplyThinkingSummaryOptions {
  backendBlockId?: string
  stage?: string
  summary: ThinkingSummaryPayload
  /** 历史回放时使用 timeline 事件自身顺序；实时流未传时按真实到达顺序分配。 */
  eventSeq?: number
  /** true 表示来自 timeline 历史恢复：不写短摘要、不更新思考态、长摘要一次性写入。 */
  fromHistory?: boolean
}
