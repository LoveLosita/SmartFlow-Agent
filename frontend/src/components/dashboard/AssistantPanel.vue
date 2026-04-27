<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, onMounted, reactive, ref, watch } from 'vue'
import { ElMessage } from 'element-plus'

import ContextWindowMeter from '@/components/assistant/ContextWindowMeter.vue'
import TaskClassPlanningPicker from '@/components/assistant/TaskClassPlanningPicker.vue'
import {
  getContextStats,
  getConversationList,
  getConversationMeta,
} from '@/api/agent'
import {
  getSchedulePreview,
  getConversationTimeline,
  type TimelineEvent,
  type TimelineToolPayload,
  type TimelineConfirmPayload
} from '@/api/schedule_agent'
import { refreshToken } from '@/api/auth'
import { useAuthStore } from '@/stores/auth'
import type {
  AssistantMessage,
  ChatRequestExtra,
  ChatStreamRequest,
  ConversationContextStats,
  ConversationListItem,
  ConversationMeta,
  ThinkingModeType,
  SchedulePreviewData,
} from '@/types/dashboard'
import ScheduleResultCard from '@/components/assistant/ScheduleResultCard.vue'
import ScheduleFineTuneModal from '@/components/assistant/ScheduleFineTuneModal.vue'
import { formatConversationTime, formatMessageTime } from '@/utils/date'
import { renderMarkdown } from '@/utils/markdown'
import BusinessCardRenderer from '@/components/assistant/cards/BusinessCardRenderer.vue'
import type { TimelineBusinessCardPayload } from '@/api/schedule_agent'

interface StreamDeltaPayload {
  content?: string
  reasoning_content?: string
}

interface StreamChoicePayload {
  delta?: StreamDeltaPayload
  finish_reason?: string | null
}

interface StreamErrorPayload {
  message?: string
}

interface StreamConfirmPayload {
  interaction_id?: string
  title?: string
  summary?: string
}

interface StreamStatusExtraPayload {
  code?: string
  summary?: string
}

interface StreamToolExtraPayload {
  name?: string
  status?: string
  summary?: string
  arguments_preview?: string
}

interface StreamExtraPayload {
  kind?: string
  block_id?: string
  stage?: string
  status?: StreamStatusExtraPayload
  tool?: StreamToolExtraPayload
  confirm?: StreamConfirmPayload
}

interface StreamEventPayload {
  choices?: StreamChoicePayload[]
  delta?: StreamDeltaPayload
  content?: string
  reasoning_content?: string
  finish_reason?: string | null
  error?: StreamErrorPayload
  extra?: StreamExtraPayload
}

type ToolTraceState = 'called' | 'completed' | 'create' | 'blocked'

interface ToolTraceEvent {
  id: string
  seq: number
  state: ToolTraceState
  summary: string
  detail?: string
  toolName?: string
}

interface StatusTraceEvent {
  id: string
  seq: number
  code: string
  stage: string
  summary: string
}


interface ConversationGroup {
  key: string
  label: string
  items: ConversationListItem[]
}

interface ConfirmOverlayState {
  visible: boolean
  manuallyClosed: boolean
  interactionId: string
  title: string
  summary: string
}

interface ConversationListItemRevealOptions {
  animate?: boolean
}

interface EnsureConversationMetaOptions {
  forceReload?: boolean
  syncListItem?: boolean
  listItemReveal?: ConversationListItemRevealOptions
}

// 展示用消息：合并连续 assistant 消息后的视图模型
interface DisplayMessage {
  /** 第一条源消息的 id，用作 Vue key */
  id: string
  role: 'user' | 'assistant' | 'system'
  /** 合并后的正文内容 */
  content: string
  /** 最后一条源消息的时间 */
  createdAt: string
  /** 合并后的推理内容 */
  reasoning?: string
  /** 原始消息引用列表 */
  sources: AssistantMessage[]
  /** 是否为多条合并 */
  merged: boolean
}

interface DisplayAssistantBlock {
  id: string
  type: 'tool' | 'status' | 'reasoning' | 'content' | 'content_indicator' | 'schedule_card' | 'business_card'
  seq: number
  text?: string
  event?: ToolTraceEvent
  statusEvent?: StatusTraceEvent
  schedulePreview?: SchedulePreviewData
  businessCard?: TimelineBusinessCardPayload
  /** 所属的源消息 ID，用于状态查询 */
  sourceId?: string
  /** 所属的源消息引用，用于渲染辅助信息 */
  source?: AssistantMessage
}

interface AssistantContentBlock {
  id: string
  seq: number
  text: string
}

const props = withDefaults(
  defineProps<{
    initialHistoryWidth?: number
    viewMode?: 'embedded' | 'standalone'
  }>(),
  {
    initialHistoryWidth: 228,
    viewMode: 'embedded',
  },
)

const authStore = useAuthStore()

const assistantBodyRef = ref<HTMLElement | null>(null)
const messageViewportRef = ref<HTMLElement | null>(null)
const historyContentRef = ref<HTMLElement | null>(null)

const conversationLoading = ref(true)
const conversationLoadingMore = ref(false)
const chatLoading = ref(false)
const historyExpanded = ref(true)
const selectedConversationId = ref('')

const selectedThinkingMode = ref<ThinkingModeType>('auto')
const selectedExecutionMode = ref<'manual' | 'always'>('manual')
const messageInput = ref('')
const historyPanelWidth = ref(props.initialHistoryWidth)
const activeStreamingMessageId = ref('')
// 流式请求的 AbortController：发送时创建，流结束或用户点击停止时 abort。
const streamAbortController = ref<AbortController | null>(null)
const editingUserMessageId = ref('')
const editingUserMessageDraft = ref('')
const pendingPlanningTaskClassIds = ref<number[]>([])
const confirmRejectDraft = ref('')
const confirmOverlayState = reactive<ConfirmOverlayState>({
  visible: false,
  manuallyClosed: false,
  interactionId: '',
  title: '',
  summary: '',
})

const conversationPage = ref(1)
const conversationPageSize = 12
const conversationHasMore = ref(false)
const conversationListReady = ref(false)

const conversationList = ref<ConversationListItem[]>([])
const conversationMetaMap = reactive<Record<string, ConversationMeta>>({})
const conversationMessagesMap = reactive<Record<string, AssistantMessage[]>>({})
const unavailableHistoryMap = reactive<Record<string, boolean>>({})
const thinkingMessageMap = reactive<Record<string, boolean>>({})
const reasoningCollapsedMap = reactive<Record<string, boolean>>({})
const reasoningStartedAtMap = reactive<Record<string, number>>({})
const reasoningDurationMap = reactive<Record<string, number>>({})
const confirmOnlyStreamMap = reactive<Record<string, boolean>>({})
const confirmVisiblePrefixMap = reactive<Record<string, boolean>>({})
const toolTraceEventsMap = reactive<Record<string, ToolTraceEvent[]>>({})
const statusTraceEventsMap = reactive<Record<string, StatusTraceEvent[]>>({})
const toolTraceExpandedMap = reactive<Record<string, boolean>>({})
const assistantReasoningSeqMap = reactive<Record<string, number>>({})
const assistantContentBlocksMap = reactive<Record<string, AssistantContentBlock[]>>({})
const assistantReasoningBlocksMap = reactive<Record<string, AssistantContentBlock[]>>({})
const assistantTimelineLastKindMap = reactive<Record<string, 'content' | 'tool' | 'status' | 'reasoning' | 'business_card' | 'other'>>({})
const conversationContextStatsMap = reactive<Record<string, ConversationContextStats | null>>({})
const conversationContextStatsLoadingMap = reactive<Record<string, boolean>>({})
const conversationContextStatsReadyMap = reactive<Record<string, boolean>>({})
const conversationListItemRevealMap = reactive<Record<string, boolean>>({})
const scheduleResultMap = reactive<Record<string, SchedulePreviewData>>({})
const businessCardEventsMap = reactive<Record<string, TimelineBusinessCardPayload[]>>({})
const isFineTuneModalVisible = ref(false)
const fineTuneLoading = ref(false)
const activeFineTuneData = ref<SchedulePreviewData | null>(null)

const quickActions = [
  '帮我梳理今天最重要的三件事',
  '把当前任务拆成可执行步骤',
  '总结这段对话的关键结论',
  '给我一个更稳妥的推进方案',
]


const DEFAULT_PLANNING_PROMPT = '请基于这些任务类帮我做一版智能编排。'

let messageScrollRaf = 0
let messageScrollReleaseRaf = 0
let reasoningTicker = 0
let historyResizeCleanup: (() => void) | null = null
const conversationListItemRevealTimerMap = new Map<string, number>()
let assistantTimelineSeq = 0
const reasoningDisplayNow = ref(Date.now())
const shouldAutoFollowMessages = ref(true)
const messageBottomTolerancePx = 24
const isProgrammaticMessageScroll = ref(false)

const isStandaloneMode = computed(() => props.viewMode === 'standalone')
const shouldShowDialogConfirmOverlay = computed(() => confirmOverlayState.visible)

const assistantBodyStyle = computed(() => {
  return {
    '--assistant-history-width': `${historyExpanded.value ? historyPanelWidth.value : 68}px`,
  }
})

const selectedConversation = computed(() =>
  conversationList.value.find((item) => item.conversation_id === selectedConversationId.value),
)

const rawSelectedMessages = computed(() => {
  if (!selectedConversationId.value) {
    return []
  }
  return conversationMessagesMap[selectedConversationId.value] ?? []
})

// retry 机制已整体下线：selectedMessages 直接回退到原始消息流，不再做分组/翻页。
const selectedMessages = computed(() => rawSelectedMessages.value)

// 1. 将连续 assistant 消息合并为一条展示消息。
// 2. ReAct 循环中 plan/execute/deliver 各节点都会产生 assistant speak，
//    合并后用户看到的是一段连续的 AI 回复，而非多段割裂输出。
const displayMessages = computed<DisplayMessage[]>(() => {
  const result: DisplayMessage[] = []
  const src = selectedMessages.value
  let i = 0
  while (i < src.length) {
    const msg = src[i]
    if (msg.role !== 'assistant') {
      result.push({
        id: msg.id,
        role: msg.role,
        content: msg.content,
        createdAt: msg.createdAt,
        reasoning: msg.reasoning,
        sources: [msg],
        merged: false,
      })
      i++
      continue
    }
    // 收集连续 assistant 消息并合并
    const group: AssistantMessage[] = []
    while (i < src.length && src[i].role === 'assistant') {
      group.push(src[i])
      i++
    }
    result.push({
      id: group[0].id,
      role: 'assistant',
      content: group.map(m => m.content).filter(Boolean).join('\n\n'),
      createdAt: group[group.length - 1].createdAt,
      reasoning: group.map(m => m.reasoning).filter(Boolean).join('\n\n') || undefined,
      sources: group,
      merged: group.length > 1,
    })
  }
  return result
})

function resolveConversationGroupLabel(timeText?: string | null) {
  if (!timeText) {
    return '更早'
  }

  const messageDate = new Date(timeText)
  if (Number.isNaN(messageDate.getTime())) {
    return '更早'
  }

  const now = new Date()
  const today = new Date(now.getFullYear(), now.getMonth(), now.getDate())
  const targetDay = new Date(messageDate.getFullYear(), messageDate.getMonth(), messageDate.getDate())
  const diffDays = Math.floor((today.getTime() - targetDay.getTime()) / (24 * 60 * 60 * 1000))

  if (diffDays <= 0) {
    return '今天'
  }
  if (diffDays < 7) {
    return '7 天内'
  }
  if (diffDays < 30) {
    return '30 天内'
  }

  return `${messageDate.getFullYear()}-${String(messageDate.getMonth() + 1).padStart(2, '0')}`
}

const groupedConversationList = computed<ConversationGroup[]>(() => {
  const orderedGroups: ConversationGroup[] = []
  const groupMap = new Map<string, ConversationGroup>()

  for (const item of conversationList.value) {
    const label = resolveConversationGroupLabel(item.last_message_at || item.created_at)
    const key = label
    const existed = groupMap.get(key)
    if (existed) {
      existed.items.push(item)
      continue
    }

    const nextGroup: ConversationGroup = {
      key,
      label,
      items: [item],
    }
    groupMap.set(key, nextGroup)
    orderedGroups.push(nextGroup)
  }

  return orderedGroups
})

const selectedConversationTitle = computed(() => {
  if (!selectedConversationId.value) {
    return '新对话'
  }

  const meta = conversationMetaMap[selectedConversationId.value]
  if (meta?.title) {
    return meta.title
  }

  const current = selectedConversation.value
  if (current?.title) {
    return current.title
  }

  return '未命名会话'
})

const selectedConversationSubtitle = computed(() => {
  if (!selectedConversationId.value) {
    return '发送后立即上屏，思考流和正文流会连续更新'
  }

  const meta = conversationMetaMap[selectedConversationId.value]
  const current = selectedConversation.value
  const messageCount = meta?.message_count ?? current?.message_count ?? rawSelectedMessages.value.length
  const lastMessageAt = meta?.last_message_at ?? current?.last_message_at
  return `消息 ${messageCount} 条 · 最近更新 ${formatConversationTime(lastMessageAt)}`
})

const shouldShowHistoryFallback = computed(() => {
  if (!selectedConversationId.value) {
    return false
  }

  return (
    unavailableHistoryMap[selectedConversationId.value] === true &&
    rawSelectedMessages.value.length === 0 &&
    (selectedConversation.value?.message_count ?? 0) > 0
  )
})

const selectedConversationContextStats = computed(() => {
  const conversationId = selectedConversationId.value
  if (!conversationId || isDraftConversationId(conversationId)) {
    return null
  }
  return conversationContextStatsMap[conversationId] ?? null
})

const contextStatsLoading = computed(() => {
  const conversationId = selectedConversationId.value
  if (!conversationId) {
    return false
  }
  return conversationContextStatsLoadingMap[conversationId] === true
})

const contextStatsDisabled = computed(() => {
  return !selectedConversationId.value || isDraftConversationId(selectedConversationId.value)
})


function ensureConversationBucket(conversationId: string) {
  if (!conversationMessagesMap[conversationId]) {
    conversationMessagesMap[conversationId] = []
  }
}

function appendConversationMessage(conversationId: string, message: AssistantMessage) {
  ensureConversationBucket(conversationId)
  const bucket = conversationMessagesMap[conversationId]
  bucket.push(message)
  const appended = bucket[bucket.length - 1]!
  thinkingMessageMap[appended.id] = Boolean(appended.reasoning?.trim())
  return appended
}

function ensureToolTraceBucket(messageId: string) {
  if (!toolTraceEventsMap[messageId]) {
    toolTraceEventsMap[messageId] = []
  }
}

function ensureStatusTraceBucket(messageId: string) {
  if (!statusTraceEventsMap[messageId]) {
    statusTraceEventsMap[messageId] = []
  }
}

function ensureAssistantContentBucket(messageId: string) {
  if (!assistantContentBlocksMap[messageId]) {
    assistantContentBlocksMap[messageId] = []
  }
}

function nextAssistantTimelineSeq() {
  assistantTimelineSeq += 1
  return assistantTimelineSeq
}

function clearToolTraceState(messageId: string) {
  delete toolTraceEventsMap[messageId]
  delete statusTraceEventsMap[messageId]
  delete assistantReasoningSeqMap[messageId]
  delete assistantContentBlocksMap[messageId]
  delete assistantTimelineLastKindMap[messageId]
  delete scheduleResultMap[messageId]
  delete businessCardEventsMap[messageId]
  for (const key of Object.keys(toolTraceExpandedMap)) {
    if (key.startsWith(`${messageId}:tool:`)) {
      delete toolTraceExpandedMap[key]
    }
  }
}

function appendToolTraceEvent(
  messageId: string,
  state: ToolTraceState,
  summary: string,
  detail = '',
  toolName = '',
) {
  const normalizedSummary = summary.trim()
  if (!normalizedSummary) {
    return
  }

  ensureToolTraceBucket(messageId)
  const normalizedDetail = detail.trim()
  const normalizedToolName = toolName.trim()
  const matchedPendingEvent = findMergeableToolTraceEvent(
    messageId,
    state,
    normalizedSummary,
    normalizedDetail,
    normalizedToolName,
  )
  if (matchedPendingEvent) {
    matchedPendingEvent.state = state
    matchedPendingEvent.summary = normalizedSummary
    matchedPendingEvent.detail = normalizedDetail || matchedPendingEvent.detail
    matchedPendingEvent.toolName = normalizedToolName || matchedPendingEvent.toolName
    return
  }
  const eventSeq = nextAssistantTimelineSeq()
  const eventId = `${messageId}:tool:${eventSeq}`

  // 如果上一个阶段是推理，则结束并折叠它
  if (assistantTimelineLastKindMap[messageId] === 'reasoning') {
    finishCurrentReasoningBlock(messageId)
  }

  toolTraceEventsMap[messageId].push({
    id: eventId,
    seq: eventSeq,
    state,
    summary: normalizedSummary,
    detail: normalizedDetail || undefined,
    toolName: normalizedToolName || undefined,
  })
  assistantTimelineLastKindMap[messageId] = 'tool'
}

function isPendingToolTraceState(state: ToolTraceState) {
  return state === 'called'
}

function findMergeableToolTraceEvent(
  messageId: string,
  nextState: ToolTraceState,
  summary: string,
  detail: string,
  toolName: string,
): ToolTraceEvent | null {
  if (nextState === 'called') {
    return null
  }

  const pendingEvents = (toolTraceEventsMap[messageId] || [])
    .slice()
    .reverse()
    .filter((event) => isPendingToolTraceState(event.state))

  if (pendingEvents.length <= 0) {
    return null
  }

  const normalizedToolName = toolName.trim().toLowerCase()
  const normalizedDetail = detail.trim()
  const normalizedSummary = summary.trim()

  if (normalizedToolName && normalizedDetail) {
    const exactMatch = pendingEvents.find((event) => {
      return (
        `${event.toolName || ''}`.trim().toLowerCase() === normalizedToolName &&
        `${event.detail || ''}`.trim() === normalizedDetail
      )
    })
    if (exactMatch) {
      return exactMatch
    }
  }

  if (normalizedToolName) {
    const toolNameMatch = pendingEvents.find((event) => {
      return `${event.toolName || ''}`.trim().toLowerCase() === normalizedToolName
    })
    if (toolNameMatch) {
      return toolNameMatch
    }
  }

  if (normalizedDetail) {
    const detailMatch = pendingEvents.find((event) => {
      return `${event.detail || ''}`.trim() === normalizedDetail
    })
    if (detailMatch) {
      return detailMatch
    }
  }

  if (normalizedSummary) {
    const summaryMatch = pendingEvents.find((event) => event.summary === normalizedSummary)
    if (summaryMatch) {
      return summaryMatch
    }
  }

  if (pendingEvents.length === 1) {
    return pendingEvents[0]
  }

  return null
}

function appendStatusTraceEvent(
  messageId: string,
  code: string,
  summary: string,
  stage = '',
) {
  const normalizedSummary = summary.trim()
  if (!normalizedSummary) {
    return
  }

  ensureStatusTraceBucket(messageId)

  // 1. 状态事件可能在同一轮里被重复推送（如重试/补偿分片）。
  // 2. 这里按“同 code + 同摘要 + 同 stage”做相邻去重，避免前端刷出重复提示行。
  // 3. 仅做相邻去重，不做全局去重，保留真实阶段演进顺序。
  const statusEvents = statusTraceEventsMap[messageId]
  const last = statusEvents[statusEvents.length - 1]
  if (last && last.code === code && last.summary === normalizedSummary && last.stage === stage) {
    return
  }

  const eventSeq = nextAssistantTimelineSeq()
  // 如果上一个阶段是推理，则结束并折叠它
  if (assistantTimelineLastKindMap[messageId] === 'reasoning') {
    finishCurrentReasoningBlock(messageId)
  }

  statusEvents.push({
    id: `${messageId}:status:${eventSeq}`,
    seq: eventSeq,
    code: code.trim(),
    stage: stage.trim(),
    summary: normalizedSummary,
  })
  assistantTimelineLastKindMap[messageId] = 'status'
}

function appendAssistantContentChunk(messageId: string, chunk: string) {
  if (!chunk) {
    return
  }
  ensureAssistantContentBucket(messageId)
  const blocks = assistantContentBlocksMap[messageId]
  const lastKind = assistantTimelineLastKindMap[messageId]

  // 如果是从推理切换到正文，则结束并折叠推理块
  if (lastKind === 'reasoning') {
    finishCurrentReasoningBlock(messageId)
  }

  if (lastKind === 'content' && blocks.length > 0) {
    blocks[blocks.length - 1]!.text += chunk
    return
  }

  const seq = nextAssistantTimelineSeq()
  blocks.push({
    id: `${messageId}:content:${seq}`,
    seq,
    text: chunk,
  })
  assistantTimelineLastKindMap[messageId] = 'content'
}

/**
 * 追加助理推理片段到特定消息的块映射中
 * 1. 采用与正文相同的块化存储逻辑，确保推理片段能按 sequence 与工具等交错排序
 * 2. 如果当前时间线最后一种类型就是 'reasoning'，则追加到最后一个块，避免碎片化
 */
function appendAssistantReasoningChunk(messageId: string, chunk: string) {
  if (!chunk) {
    return
  }
  if (!assistantReasoningBlocksMap[messageId]) {
    assistantReasoningBlocksMap[messageId] = []
  }
  const blocks = assistantReasoningBlocksMap[messageId]
  const lastKind = assistantTimelineLastKindMap[messageId]

  if (lastKind === 'reasoning' && blocks.length > 0) {
    blocks[blocks.length - 1]!.text += chunk
    return
  }

  const seq = nextAssistantTimelineSeq()
  const blockId = `${messageId}:reasoning:${seq}`
  blocks.push({
    id: blockId,
    seq,
    text: chunk,
  })
  
  // 记录块级别的起始时间和初始折叠状态
  reasoningStartedAtMap[blockId] = Date.now()
  reasoningCollapsedMap[blockId] = false
  
  assistantTimelineLastKindMap[messageId] = 'reasoning'
}

/**
 * 追加业务卡片事件
 */
function appendBusinessCardEvent(messageId: string, payload: TimelineBusinessCardPayload, seq?: number) {
  if (!businessCardEventsMap[messageId]) {
    businessCardEventsMap[messageId] = []
  }
  
  // 如果上一个阶段是推理，则结束并折叠它
  if (assistantTimelineLastKindMap[messageId] === 'reasoning') {
    finishCurrentReasoningBlock(messageId)
  }

  const eventSeq = seq || nextAssistantTimelineSeq()
  businessCardEventsMap[messageId].push({
    ...payload,
    // 借用 payload 存储 seq，便于 getDisplayAssistantBlocks 排序
    _seq: eventSeq
  } as any)
  
  assistantTimelineLastKindMap[messageId] = 'business_card'
}

function mapToolEventState(rawStatus?: string): ToolTraceState {
  const normalized = `${rawStatus || ''}`.trim().toLowerCase()
  if (normalized === 'start' || normalized === 'calling' || normalized === 'called') {
    return 'called'
  }
  if (normalized === 'create' || normalized === 'created') {
    return 'create'
  }
  if (normalized === 'blocked') {
    return 'blocked'
  }
  if (normalized === 'failed' || normalized === 'error') {
    return 'blocked'
  }
  return 'completed'
}

function normalizeToolSummary(extra: StreamToolExtraPayload): string {
  const summary = `${extra.summary || ''}`.trim()
  if (summary) {
    return summary
  }
  const toolName = `${extra.name || ''}`.trim()
  if (!toolName) {
    return '工具事件'
  }
  return `已调用工具：${toolName}`
}

function buildToolDetail(extra: StreamToolExtraPayload): string {
  const argsPreview = `${extra.arguments_preview || ''}`.trim()
  if (!argsPreview || argsPreview === '{}') {
    return ''
  }
  return argsPreview
}

function normalizeStatusCode(rawCode?: string) {
  const code = `${rawCode || ''}`.trim().toLowerCase()
  if (!code) {
    return 'status'
  }
  return code
}

function mapStatusCodeLabel(code: string) {
  const labelMap: Record<string, string> = {
    accepted: '请求已接收',
    planning: '正在规划',
    resumed: '继续处理中',
    confirmed: '确认后继续执行',
    rejected: '已取消并重新规划',
    executing: '正在执行',
    plan_confirm: '等待计划确认',
    tool_confirm: '等待操作确认',
    ask_user: '等待补充信息',
    confirm: '等待用户确认',
    interrupted: '会话已中断',
    summarizing: '正在生成总结',
    done: '流程已结束',
    rough_building: '正在生成初始排课方案',
    rough_build_failed: '初始排课失败',
    rough_build_done: '初始排课已完成',
    rough_build_done_no_refine: '初始排课已完成',
    order_guard_initialized: '已记录顺序基线',
    order_guard_passed: '顺序校验通过',
    order_guard_restored: '顺序已自动恢复',
    order_guard_restore_skipped: '顺序恢复已跳过',
    context_compact_start: '正在压缩上下文',
    context_compact_done: '上下文压缩完成',
    plan_auto_confirmed: '计划已自动确认',
  }
  return labelMap[code] || '状态已更新'
}

function buildStatusSummary(extra: StreamExtraPayload): string {
  const summary = `${extra.status?.summary || ''}`.trim()
  if (summary) {
    return summary
  }
  return mapStatusCodeLabel(normalizeStatusCode(extra.status?.code))
}

function isLegacyToolStatusCode(code: string) {
  return code === 'tool_call' || code === 'tool_result' || code === 'tool_blocked'
}

function mapLegacyToolStatusToState(code: string): ToolTraceState {
  if (code === 'tool_call') {
    return 'called'
  }
  if (code === 'tool_blocked') {
    return 'blocked'
  }
  return 'completed'
}

function shouldSkipStatusEvent(code: string, stage = '') {
  // confirm_request 已有专属卡片，避免重复显示同语义状态行。
  if (stage === 'confirm' && (code === 'plan_confirm' || code === 'tool_confirm' || code === 'confirm')) {
    return true
  }

  const hiddenStatusCodes = new Set([
    'accepted',
    'ask_user',
    'planning',
    'resumed',
    'confirmed',
    'rejected',
    'executing',
    'summarizing',
    'done',
    'rough_building',
    'order_guard_initialized',
    'order_guard_passed',
    'order_guard_restored',
    'order_guard_restore_skipped',
    'context_compact_start',
    'context_compact_done',
    'plan_auto_confirmed',
  ])

  if (hiddenStatusCodes.has(code)) {
    return true
  }
  return false
}

function isAssistantTimelineKind(kind: string) {
  const assistantKinds = new Set([
    'assistant_text',
    'tool_call',
    'tool_result',
    'confirm_request',
    'schedule_completed',
    'interrupt',
    'status',
  ])
  return assistantKinds.has(kind)
}

function isToolTraceExpanded(eventId: string) {
  return toolTraceExpandedMap[eventId] === true
}

function toggleToolTraceExpanded(eventId: string) {
  toolTraceExpandedMap[eventId] = !toolTraceExpandedMap[eventId]
}

function removeConversationMessage(conversationId: string, messageId: string) {
  const bucket = conversationMessagesMap[conversationId]
  if (!bucket || bucket.length <= 0) {
    return
  }
  const targetIndex = bucket.findIndex((item) => item.id === messageId)
  if (targetIndex < 0) {
    return
  }
  bucket.splice(targetIndex, 1)
  clearToolTraceState(messageId)
}

function cleanupHiddenAssistantMessageState(messageId: string) {
  // 1. confirm 事件由卡片消费时，这条 assistant 占位消息会从正文列表移除。
  // 2. 这里同步清理与消息 ID 绑定的前端状态，避免残留状态影响后续新消息。
  // 3. 即使某个字段不存在也直接 delete，保证幂等，重复调用不会引入副作用。
  delete thinkingMessageMap[messageId]
  delete reasoningCollapsedMap[messageId]
  delete reasoningStartedAtMap[messageId]
  delete reasoningDurationMap[messageId]
  delete confirmOnlyStreamMap[messageId]
  delete confirmVisiblePrefixMap[messageId]
  clearToolTraceState(messageId)
}

function clearConfirmStreamFlags(messageId: string) {
  // 1. confirm 分流结束后，清理该条消息的分流标记，避免后续同 ID 复用时误判。
  // 2. 只清理 confirm 相关标记，不触碰正文展示状态，确保 confirm 前文本仍可见。
  delete confirmOnlyStreamMap[messageId]
  delete confirmVisiblePrefixMap[messageId]
}

function createDraftConversationId() {
  return `draft-${Date.now()}-${Math.random().toString(36).slice(2, 8)}`
}

function createMessageId(role: AssistantMessage['role']) {
  return `${role}-${Date.now()}-${Math.random().toString(36).slice(2, 8)}`
}

function isDraftConversationId(conversationId: string) {
  return conversationId.startsWith('draft-')
}

function upsertConversationMeta(meta: ConversationMeta) {
  conversationMetaMap[meta.conversation_id] = meta
}

// migrateConversationState 负责把“本地 draft 会话”迁移成后端返回的真实会话 ID。
// 职责边界：
// 1. 先迁移消息、元信息、异常状态，再切换当前选中会话，避免流式过程中界面抖动。
// 2. 若列表里同时存在 draft 和真实会话，则按真实会话 ID 去重，保留较新的字段。
// 3. 这里只处理前端状态搬迁，不额外发网络请求；标题和条数的最终修正仍交给后续 meta/list 刷新。
function migrateConversationState(fromConversationId: string, toConversationId: string) {
  if (!fromConversationId || !toConversationId || fromConversationId === toConversationId) {
    return
  }

  if (conversationMessagesMap[fromConversationId]) {
    conversationMessagesMap[toConversationId] =
      conversationMessagesMap[toConversationId] ?? conversationMessagesMap[fromConversationId]
    delete conversationMessagesMap[fromConversationId]
  }

  if (typeof unavailableHistoryMap[fromConversationId] !== 'undefined') {
    unavailableHistoryMap[toConversationId] = unavailableHistoryMap[fromConversationId]
    delete unavailableHistoryMap[fromConversationId]
  }

  if (conversationMetaMap[fromConversationId]) {
    conversationMetaMap[toConversationId] = {
      ...conversationMetaMap[fromConversationId],
      conversation_id: toConversationId,
    }
    delete conversationMetaMap[fromConversationId]
  }

  const latestMap = new Map<string, ConversationListItem>()
  const deduplicated: ConversationListItem[] = []
  const seen = new Set<string>()

  for (const item of conversationList.value) {
    const nextItem =
      item.conversation_id === fromConversationId ? { ...item, conversation_id: toConversationId } : item
    latestMap.set(nextItem.conversation_id, nextItem)
  }

  for (const item of conversationList.value) {
    const nextId = item.conversation_id === fromConversationId ? toConversationId : item.conversation_id
    if (seen.has(nextId)) {
      continue
    }
    seen.add(nextId)
    deduplicated.push(latestMap.get(nextId)!)
  }

  conversationList.value = deduplicated

  if (selectedConversationId.value === fromConversationId) {
    selectedConversationId.value = toConversationId
  }
}

// mergeConversationList 负责把分页拿到的会话列表按会话 ID 合并进本地状态。
// 职责边界：
// 1. 负责“保留现有顺序 + 更新最新字段 + 去重”，避免懒加载时列表闪烁。
// 2. 不负责决定选中哪个会话，选中逻辑交给 ensureSelectedConversationAfterListLoad。
// 3. 本地尚未完成 round-trip 的 draft 会话会原样保留，避免用户发出首条消息后列表瞬间丢失。
function mergeConversationList(items: ConversationListItem[]) {
  const merged = [...conversationList.value, ...items]
  const latestMap = new Map<string, ConversationListItem>()
  const deduplicated: ConversationListItem[] = []
  const seen = new Set<string>()

  for (const item of merged) {
    latestMap.set(item.conversation_id, item)
  }

  for (const item of merged) {
    if (seen.has(item.conversation_id)) {
      continue
    }
    seen.add(item.conversation_id)
    deduplicated.push(latestMap.get(item.conversation_id) ?? item)
  }

  conversationList.value = deduplicated
}

function prependConversationPreview(conversationId: string, previewText: string, createdAt: string) {
  const current = conversationList.value.find((item) => item.conversation_id === conversationId)
  const nextItem: ConversationListItem = {
    conversation_id: conversationId,
    title: current?.title || previewText.slice(0, 24),
    has_title: current?.has_title ?? false,
    message_count: Math.max(current?.message_count ?? 0, conversationMessagesMap[conversationId]?.length ?? 0),
    last_message_at: createdAt,
    status: current?.status || 'active',
    created_at: current?.created_at || createdAt,
  }

  conversationList.value = [
    nextItem,
    ...conversationList.value.filter((item) => item.conversation_id !== conversationId),
  ]
}

function isConversationListItemRevealing(conversationId: string) {
  return conversationListItemRevealMap[conversationId] === true
}

function triggerConversationListItemReveal(conversationId: string) {
  if (!conversationId) {
    return
  }

  // 1. 相同会话在短时间内可能被连续更新，这里先清理旧定时器，避免动画状态错乱。
  // 2. 通过 nextTick 让 class 先完成一次“移除 -> 添加”，确保同一元素可重复触发动画。
  // 3. 动画结束后回收标记，防止无关渲染继续保留高亮状态。
  const previousTimer = conversationListItemRevealTimerMap.get(conversationId)
  if (typeof previousTimer === 'number') {
    window.clearTimeout(previousTimer)
  }

  conversationListItemRevealMap[conversationId] = false
  void nextTick(() => {
    conversationListItemRevealMap[conversationId] = true
    const nextTimer = window.setTimeout(() => {
      delete conversationListItemRevealMap[conversationId]
      conversationListItemRevealTimerMap.delete(conversationId)
    }, 460)
    conversationListItemRevealTimerMap.set(conversationId, nextTimer)
  })
}

function syncConversationListItemFromMeta(
  meta: ConversationMeta,
  options: ConversationListItemRevealOptions = {},
) {
  const targetIndex = conversationList.value.findIndex((item) => item.conversation_id === meta.conversation_id)
  if (targetIndex < 0) {
    return
  }

  const current = conversationList.value[targetIndex]!
  const nextTitle = meta.has_title && meta.title ? meta.title : current.title
  const titleChanged = nextTitle !== current.title

  const nextItem: ConversationListItem = {
    ...current,
    title: nextTitle,
    has_title: meta.has_title,
    message_count: meta.message_count,
    last_message_at: meta.last_message_at ?? current.last_message_at,
    status: meta.status || current.status,
  }

  conversationList.value.splice(targetIndex, 1, nextItem)

  if (options.animate && titleChanged) {
    triggerConversationListItemReveal(meta.conversation_id)
  }
}

function renderMessageMarkdown(content: string) {
  return renderMarkdown(content)
}

function isStreamingMessage(message: AssistantMessage) {
  return message.id === activeStreamingMessageId.value
}

function isEditingUserMessage(messageId: string) {
  return editingUserMessageId.value === messageId
}

function isThinkingMessage(message: AssistantMessage) {
  return thinkingMessageMap[message.id] === true
}

function findMessageIndex(messageId: string) {
  return selectedMessages.value.findIndex((message) => message.id === messageId)
}

function isLatestAssistantMessage(messageId: string) {
  const lastAssistant = [...selectedMessages.value].reverse().find((message) => message.role === 'assistant')
  return lastAssistant?.id === messageId
}

function isLocalEphemeralMessageId(id: string) {
  return /^(user|assistant|system)-\d{13}-[a-z0-9]+$/i.test(id)
}

function resolvePromptBeforeAssistantMessage(messageId: string) {
  const index = findMessageIndex(messageId)
  if (index <= 0) {
    return ''
  }

  for (let current = index - 1; current >= 0; current -= 1) {
    const candidate = selectedMessages.value[current]
    if (candidate?.role === 'user' && candidate.content.trim()) {
      return candidate.content
    }
  }

  return ''
}

async function copyText(text: string, successMessage: string) {
  try {
    if (navigator?.clipboard?.writeText) {
      await navigator.clipboard.writeText(text)
    } else {
      const textarea = document.createElement('textarea')
      textarea.value = text
      textarea.style.position = 'fixed'
      textarea.style.opacity = '0'
      document.body.appendChild(textarea)
      textarea.focus()
      textarea.select()
      document.execCommand('copy')
      document.body.removeChild(textarea)
    }

    ElMessage.success(successMessage)
  } catch {
    ElMessage.error('复制失败，请稍后重试')
  }
}

function startEditUserMessage(message: AssistantMessage) {
  editingUserMessageId.value = message.id
  editingUserMessageDraft.value = message.content
}

function cancelEditUserMessage() {
  editingUserMessageId.value = ''
  editingUserMessageDraft.value = ''
}

function submitEditedUserMessage(message: AssistantMessage) {
  const nextContent = editingUserMessageDraft.value.trim()
  if (!nextContent) {
    ElMessage.warning('消息内容不能为空')
    return
  }

  if (chatLoading.value) {
    ElMessage.info('当前正在生成回复，请稍后再发送修改后的消息')
    return
  }

  // 1. “修改消息”当前按产品定义等价于“复制原消息到输入区后，编辑并重新发送一条新消息”。
  // 2. 因此这里不改写历史里的旧消息，只关闭编辑态并走现有 sendMessage 主链路。
  // 3. 这样无需后端新增接口，也能和普通发送保持完全一致的会话语义。
  cancelEditUserMessage()
  void sendMessage(nextContent)
}

function markReasoningStart(message: AssistantMessage) {
  if (reasoningStartedAtMap[message.id]) {
    return
  }

  // 1. 计时起点绑定到“首个思考 token 到达前端”的瞬间，而不是消息发送时间。
  // 2. 这样可避免网络排队/后端排队时间被错误计入“已思考用时”。
  // 3. 只在首次命中时写入，后续增量不会重复覆盖起点。
  reasoningStartedAtMap[message.id] = Date.now()
}

function markReasoningFinished(blockId: string, messageId: string) {
  const startedAt = reasoningStartedAtMap[blockId]
  if (startedAt && !reasoningDurationMap[blockId]) {
    reasoningDurationMap[blockId] = Math.max(1, Math.round((Date.now() - startedAt) / 1000))
  }
  thinkingMessageMap[messageId] = false
}

function getReasoningDurationSeconds(blockId: string) {
  const fixedDuration = reasoningDurationMap[blockId]
  if (fixedDuration) {
    return fixedDuration
  }

  const startedAt = reasoningStartedAtMap[blockId]
  if (!startedAt) {
    return 0
  }

  return Math.max(1, Math.round((reasoningDisplayNow.value - startedAt) / 1000))
}

function getReasoningStatusLabel(block: DisplayAssistantBlock) {
  const durationSeconds = getReasoningDurationSeconds(block.id)
  if (durationSeconds > 0) {
    return `已思考（用时 ${durationSeconds} 秒）`
  }

  const isThinking = block.sourceId === activeStreamingMessageId.value && thinkingMessageMap[block.sourceId]
  return isThinking ? '思考中' : '已思考'
}

/**
 * 结束当前消息正在进行的推理块
 * 1. 计算耗时
 * 2. 自动折叠
 */
function finishCurrentReasoningBlock(messageId: string) {
  const blocks = assistantReasoningBlocksMap[messageId] || []
  if (blocks.length === 0) return
  const lastBlock = blocks[blocks.length - 1]
  
  markReasoningFinished(lastBlock.id, messageId)
  reasoningCollapsedMap[lastBlock.id] = true
}

function isReasoningCollapsed(messageId: string) {
  return reasoningCollapsedMap[messageId] === true
}

function toggleReasoningCollapse(messageId: string) {
  reasoningCollapsedMap[messageId] = !reasoningCollapsedMap[messageId]
}

function shouldShowReasoningBox(message: AssistantMessage) {
  return message.role === 'assistant' && (
    Boolean(message.reasoning?.trim()) ||
    (isStreamingMessage(message) && isThinkingMessage(message))
  )
}

function shouldShowAnsweringIndicator(message: AssistantMessage) {
  return isStreamingMessage(message) && !isThinkingMessage(message) && !message.content.trim()
}

// ---------- DisplayMessage 适配函数 ----------
// 合并后的 DisplayMessage 包含多条源消息，以下函数统一处理
// 流式状态、推理框、折叠等在合并场景下的语义。

function isDisplayStreaming(dm: DisplayMessage): boolean {
  return dm.sources.some(m => m.id === activeStreamingMessageId.value)
}

function getDisplayReasoningSeq(dm: DisplayMessage) {
  const seqList: number[] = []
  for (const source of dm.sources) {
    const seq = assistantReasoningSeqMap[source.id]
    if (typeof seq === 'number' && seq > 0) {
      seqList.push(seq)
    }
  }
  if (seqList.length > 0) {
    return Math.min(...seqList)
  }
  if (dm.reasoning?.trim()) {
    return 10
  }
  return -1
}

function getDisplayAssistantBlocks(dm: DisplayMessage): DisplayAssistantBlock[] {
  if (dm.role !== 'assistant') {
    return []
  }

  const blocks: DisplayAssistantBlock[] = []
  let fallbackSeq = -100000
  let hasContentBlock = false

  for (const source of dm.sources) {
    const sourceEvents = (toolTraceEventsMap[source.id] || []).slice().sort((left, right) => left.seq - right.seq)
    for (const event of sourceEvents) {
      blocks.push({
        id: event.id,
        type: 'tool',
        seq: event.seq,
        event,
        sourceId: source.id,
        source,
      })
    }
    const statusEvents = (statusTraceEventsMap[source.id] || []).slice().sort((left, right) => left.seq - right.seq)
    for (const statusEvent of statusEvents) {
      blocks.push({
        id: statusEvent.id,
        type: 'status',
        seq: statusEvent.seq,
        statusEvent,
        sourceId: source.id,
        source,
      })
    }

    // 从推理块映射中提取所有独立的推理片段
    const reasoningBlocks = assistantReasoningBlocksMap[source.id] || []
    if (reasoningBlocks.length > 0) {
      for (const rb of reasoningBlocks) {
        blocks.push({
          id: rb.id,
          type: 'reasoning',
          seq: rb.seq,
          text: rb.text,
          sourceId: source.id,
          source,
        })
      }
    } else if (source.id === activeStreamingMessageId.value && thinkingMessageMap[source.id]) {
      // 流式过程中尚未有实质文本产出时的“思考中”占位块
      blocks.push({
        id: `${source.id}:reasoning:streaming`,
        type: 'reasoning',
        seq: assistantReasoningSeqMap[source.id] || 10,
        sourceId: source.id,
        source,
      })
    }

    const businessCards = businessCardEventsMap[source.id] || []
    for (const card of businessCards) {
      blocks.push({
        id: `${source.id}:card:${(card as any)._seq}`,
        type: 'business_card',
        seq: (card as any)._seq,
        businessCard: card,
        sourceId: source.id,
        source,
      })
    }

    const contentBlocks = assistantContentBlocksMap[source.id] || []
    if (contentBlocks.length > 0) {
      hasContentBlock = true
      for (const contentBlock of contentBlocks) {
        blocks.push({
          id: contentBlock.id,
          type: 'content',
          seq: contentBlock.seq,
          text: contentBlock.text,
          sourceId: source.id,
          source,
        })
      }
      continue
    }

    if (source.content) {
      hasContentBlock = true
      fallbackSeq += 1
      blocks.push({
        id: `${source.id}:content:fallback`,
        type: 'content',
        seq: fallbackSeq,
        text: source.content,
        sourceId: source.id,
        source,
      })
    }
  }

  const schedulePreview = scheduleResultMap[dm.id]
  if (schedulePreview) {
    blocks.push({
      id: `${dm.id}:schedule-card`,
      type: 'schedule_card',
      seq: nextAssistantTimelineSeq(),
      schedulePreview,
    })
  }

  if (!hasContentBlock && dm.content) {
    fallbackSeq += 1
    blocks.push({
      id: `${dm.id}:content`,
      type: 'content',
      seq: fallbackSeq,
      text: dm.content,
    })
  }

  if (shouldShowDisplayAnsweringIndicator(dm)) {
    const maxSeq = blocks.length > 0 ? Math.max(...blocks.map((item) => item.seq)) : 0
    blocks.push({
      id: `${dm.id}:content-indicator`,
      type: 'content_indicator',
      seq: maxSeq + 1,
    })
  }

  return blocks.sort((left, right) => left.seq - right.seq)
}

function getToolTraceStateLabel(state: ToolTraceState): string {
  if (state === 'called') {
    return '已调用'
  }
  if (state === 'create') {
    return '已创建'
  }
  if (state === 'blocked') {
    return '已拦截'
  }
  return '已完成'
}

function shouldShowDisplayAnsweringIndicator(dm: DisplayMessage): boolean {
  return isDisplayStreaming(dm) && 
         dm.sources.every(m => thinkingMessageMap[m.id] !== true) &&
         !dm.content.trim()
}

function getDisplayReasoningStatusLabel(dm: DisplayMessage): string {
  // 此函数已废弃，推理状态现已下沉到各 source 块处理。
  // 仅保留空实现以防意外调用。
  return '已思考'
}

function isMessageViewportAtBottom(viewport: HTMLElement) {
  return viewport.scrollHeight - viewport.scrollTop - viewport.clientHeight <= messageBottomTolerancePx
}

function stopMessageAutoFollow() {
  shouldAutoFollowMessages.value = false
  if (messageScrollRaf) {
    cancelAnimationFrame(messageScrollRaf)
    messageScrollRaf = 0
  }
}

function handleMessageViewportWheel(event: WheelEvent) {
  if (event.deltaY < 0) {
    // 1. 用户一旦尝试向上滚动，立即关闭自动跟随，优先保证人工浏览体验。
    // 2. 这里不依赖是否真的滚动成功，避免 SSE 高频刷新把用户拉回底部。
    // 3. 恢复自动跟随交给 handleMessageViewportScroll 在“回到底部”时统一处理。
    stopMessageAutoFollow()
  }
}

function handleMessageViewportScroll(event: Event) {
  const viewport = event.target as HTMLElement | null
  if (!viewport) {
    return
  }

  if (isProgrammaticMessageScroll.value) {
    shouldAutoFollowMessages.value = true
    return
  }

  // 1. 若滚动到底部（最后一行完整露出），恢复自动跟随。
  // 2. 只要离底部有距离，就维持“手动阅读模式”，防止流式输出打断阅读。
  // 3. 该状态会影响后续 scheduleScrollMessagesToBottom，形成可控的跟随策略。
  shouldAutoFollowMessages.value = isMessageViewportAtBottom(viewport)
}

function scheduleScrollMessagesToBottom(smooth = false, force = false) {
  if (!force && !shouldAutoFollowMessages.value) {
    return
  }

  if (force) {
    shouldAutoFollowMessages.value = true
  }

  if (messageScrollRaf) {
    cancelAnimationFrame(messageScrollRaf)
  }
  if (messageScrollReleaseRaf) {
    cancelAnimationFrame(messageScrollReleaseRaf)
  }

  messageScrollRaf = window.requestAnimationFrame(() => {
    if (!force && !shouldAutoFollowMessages.value) {
      messageScrollRaf = 0
      return
    }

    const viewport = messageViewportRef.value
    if (!viewport) {
      messageScrollRaf = 0
      return
    }

    // 1. 先标记为程序触发滚动，避免 scroll 事件把自动跟随错误关闭。
    // 2. 采用双 requestAnimationFrame，等待本轮文本增量和布局波动稳定后再落到底部。
    // 3. 下一帧统一释放程序滚动标记，恢复用户主动滚动的判断能力。
    isProgrammaticMessageScroll.value = true
    viewport.scrollTo({
      top: viewport.scrollHeight,
      behavior: smooth ? 'smooth' : 'auto',
    })
    messageScrollRaf = window.requestAnimationFrame(() => {
      viewport.scrollTo({
        top: viewport.scrollHeight,
        behavior: 'auto',
      })
      messageScrollRaf = 0
      messageScrollReleaseRaf = window.requestAnimationFrame(() => {
        isProgrammaticMessageScroll.value = false
        shouldAutoFollowMessages.value = isMessageViewportAtBottom(viewport)
        messageScrollReleaseRaf = 0
      })
    })
  })
}

async function ensureSelectedConversationAfterListLoad() {
  // 1. 根据用户最新要求：进入页面时不自动加载最后一次对话。
  // 2. 默认保持 selectedConversationId 为空，从而触发居中的“新会话”看板及动画过渡逻辑。
  // 3. 用户若需查看历史，可从左侧列表中手动点击。
  /*
  if (!selectedConversationId.value && conversationList.value.length > 0) {
    await selectConversation(conversationList.value[0].conversation_id)
  }
  */
}

// loadConversationListData 负责按页读取会话列表，并驱动首屏选中与懒加载状态。
// 职责边界：
// 1. reset=true 时重置分页并重新获取第一页，适合新消息发送完成后刷新标题和时间。
// 2. reset=false 时只在还有更多数据且当前不在加载时继续拉下一页，避免重复请求。
// 3. 接口失败时保留现有列表，不清空本地草稿会话，防止用户当前上下文丢失。
async function loadConversationListData(reset = false) {
  let loadSucceeded = false

  if (reset) {
    conversationPage.value = 1
    conversationHasMore.value = false
    conversationListReady.value = false
    conversationLoading.value = true
  } else {
    if (conversationLoading.value || conversationLoadingMore.value || !conversationHasMore.value) {
      return
    }
    conversationLoadingMore.value = true
  }

  try {
    const minTimer = new Promise((resolve) => setTimeout(resolve, 800))
    const [result] = await Promise.all([
      getConversationList({
        page: conversationPage.value,
        pageSize: conversationPageSize,
        status: 'active',
      }),
      reset ? minTimer : Promise.resolve(),
    ])

    if (reset) {
      conversationList.value = conversationList.value.filter((item) => isDraftConversationId(item.conversation_id))
    }
    mergeConversationList(result?.list ?? [])

    conversationHasMore.value = Boolean(result?.has_more)
    conversationPage.value += 1
    conversationListReady.value = true
    await ensureSelectedConversationAfterListLoad()
    loadSucceeded = true
  } catch (error) {
    ElMessage.warning(error instanceof Error ? error.message : '会话列表加载失败，请稍后重试')
  } finally {
    conversationLoading.value = false
    conversationLoadingMore.value = false
  }

  if (loadSucceeded) {
    await ensureHistoryPanelCanScroll()
  }
}

// ensureHistoryPanelCanScroll 负责在“首屏列表不足以形成滚动条”时自动补拉后续分页。
// 职责边界：
// 1. 只处理左侧历史列表的可滚动性，不参与会话选中、标题计算等业务逻辑。
// 2. 仅当容器已经渲染完成、且当前内容高度仍未超过可视高度时才继续拉下一页，避免无意义请求。
// 3. 若已经到底、容器不存在，或当前正在加载，则直接停止，防止递归触发形成请求风暴。
async function ensureHistoryPanelCanScroll() {
  await nextTick()

  const container = historyContentRef.value
  if (!container || conversationLoading.value || conversationLoadingMore.value || !conversationHasMore.value) {
    return
  }

  const canScroll = container.scrollHeight - container.clientHeight > 1
  if (canScroll) {
    return
  }

  await loadConversationListData(false)
}

function handleHistoryScroll(event: Event) {
  const target = event.target as HTMLElement | null
  if (!target || !historyExpanded.value || conversationLoading.value || conversationLoadingMore.value) {
    return
  }

  const remaining = target.scrollHeight - target.scrollTop - target.clientHeight
  if (remaining <= 56) {
    void loadConversationListData(false)
  }
}

function getHistoryPanelWidthBounds(containerWidth: number) {
  const standalone = isStandaloneMode.value
  const minHistoryWidth = standalone ? 196 : 188
  const minChatWidth = standalone ? 560 : 420
  const splitterWidth = 8
  const rawMaxHistoryWidth = standalone
    ? Math.min(320, containerWidth - splitterWidth - minChatWidth)
    : containerWidth - splitterWidth - minChatWidth

  return {
    minHistoryWidth,
    maxHistoryWidth: Math.max(minHistoryWidth, rawMaxHistoryWidth),
  }
}

function syncHistoryPanelWidthForViewport() {
  if (!historyExpanded.value) {
    return
  }

  const body = assistantBodyRef.value
  const containerWidth =
    body?.getBoundingClientRect().width ??
    Math.max(0, window.innerWidth - (isStandaloneMode.value ? 120 : 0))
  const bounds = getHistoryPanelWidthBounds(containerWidth)
  historyPanelWidth.value = Math.min(
    Math.max(historyPanelWidth.value, bounds.minHistoryWidth),
    bounds.maxHistoryWidth,
  )
}

function releaseHistoryResizeListeners() {
  if (!historyResizeCleanup) {
    return
  }

  historyResizeCleanup()
  historyResizeCleanup = null
}

// startResizeHistoryPanel 负责处理会话列表与聊天主区之间的横向拖拽。
// 职责边界：
// 1. 只负责更新助手面板内部的历史区宽度，不修改外层 Dashboard 的左右二分布局。
// 2. 会为正文区保留最小阅读宽度，避免把长回答挤压到难以阅读。
// 3. 拖拽结束后统一解绑事件并清理全局样式，防止页面残留 col-resize 状态。
function startResizeHistoryPanel(event: PointerEvent) {
  const body = assistantBodyRef.value
  if (!body || !historyExpanded.value) {
    return
  }

  const isStandalone = isStandaloneMode.value
  const minViewportWidth = isStandalone ? 860 : 960
  if (window.innerWidth <= minViewportWidth) {
    return
  }

  const rect = body.getBoundingClientRect()
  const startX = event.clientX
  const startWidth = historyPanelWidth.value
  const bounds = getHistoryPanelWidthBounds(rect.width)
  // 1. 先清理上一次拖拽遗留的监听器，避免重复绑定导致的光标残留和状态错乱。
  // 2. 再注册本次拖拽监听，并把清理函数保存起来，方便 pointerup / pointercancel / 卸载时统一回收。
  releaseHistoryResizeListeners()

  const handlePointerMove = (moveEvent: PointerEvent) => {
    const deltaX = moveEvent.clientX - startX
    historyPanelWidth.value = Math.min(
      Math.max(startWidth + deltaX, bounds.minHistoryWidth),
      bounds.maxHistoryWidth,
    )
  }

  const stopResize = () => {
    releaseHistoryResizeListeners()
  }

  historyResizeCleanup = () => {
    window.removeEventListener('pointermove', handlePointerMove)
    window.removeEventListener('pointerup', stopResize)
    window.removeEventListener('pointercancel', stopResize)
    window.removeEventListener('blur', stopResize)
    document.body.classList.remove('dashboard-resizing')
  }

  document.body.classList.add('dashboard-resizing')
  window.addEventListener('pointermove', handlePointerMove)
  window.addEventListener('pointerup', stopResize)
  window.addEventListener('pointercancel', stopResize)
  window.addEventListener('blur', stopResize)
}

function toggleHistoryPanel() {
  historyExpanded.value = !historyExpanded.value
}

function rebuildStateFromTimeline(conversationId: string, events: TimelineEvent[]) {
  const result: AssistantMessage[] = []
  let currentAssistantMessage: AssistantMessage | null = null

  // 清理该会话旧的辅助状态（工具、排程卡片等）
  // 注意：此处不清理 bucket 容器，只清理每个消息关联的映射
  const existingMessages = conversationMessagesMap[conversationId] || []
  existingMessages.forEach(msg => {
    if (msg.role === 'assistant') {
      clearToolTraceState(msg.id)
    }
  })

  for (const event of events) {
    const kind = String(event.kind || '').toLowerCase()
    const rawRole = String(event.role || '').toLowerCase()
    
    // 1. timeline 重建时先识别显式 user 事件，避免把真正的用户输入吞进 assistant 回合。
    // 2. interrupt / status 这类 assistant 侧协议事件不能再掉进 user 兜底，否则会把 ask_user 正文切断。
    // 3. 这里仍保留 kind.includes('user') 的保守判断，只是把 assistant 白名单补齐到本轮真实协议。
    let isUser = rawRole === 'user' || kind.includes('user')
    if (!isUser) {
      if (!isAssistantTimelineKind(kind)) {
        isUser = true
      }
    }
    if (isUser) {
      currentAssistantMessage = null
      result.push({
        id: `t-${event.id}`,
        role: 'user',
        content: event.content || '',
        createdAt: event.created_at,
      })
      continue
    }

    // 助手事件
    if (!currentAssistantMessage) {
      currentAssistantMessage = {
        id: `t-${event.id}`,
        role: 'assistant',
        content: '',
        createdAt: event.created_at,
        reasoning: '',
      }
      result.push(currentAssistantMessage)
      thinkingMessageMap[currentAssistantMessage.id] = false
      reasoningCollapsedMap[currentAssistantMessage.id] = true
    }

    const mid = currentAssistantMessage.id

    switch (event.kind) {
      case 'assistant_text':
      case 'interrupt':
        if (event.content) {
          const newContent = event.content
          const oldContent = currentAssistantMessage.content || ''
          let chunk = newContent
          if (newContent.startsWith(oldContent)) {
            chunk = newContent.slice(oldContent.length)
          }
          
          if (chunk) {
            currentAssistantMessage.content += chunk
            // 同时存入 blocks 以支持和工具交错显示
            appendAssistantContentChunk(mid, chunk)
          }
        }
        
        if (event.payload?.reasoning_content) {
          const newReasoning = event.payload.reasoning_content
          const oldReasoning = currentAssistantMessage.reasoning || ''
          let reasoningChunk = newReasoning
          if (newReasoning.startsWith(oldReasoning)) {
            reasoningChunk = newReasoning.slice(oldReasoning.length)
          }
          
          if (reasoningChunk) {
            currentAssistantMessage.reasoning = oldReasoning + reasoningChunk
            // 时序化存储推理内容
            appendAssistantReasoningChunk(mid, reasoningChunk)
            if (!assistantReasoningSeqMap[mid]) {
              assistantReasoningSeqMap[mid] = event.seq
            }
          }
        }
        break
      
      case 'tool_call':
        if (event.payload?.tool) {
          const t = event.payload.tool
          appendToolTraceEvent(mid, mapToolEventState(t.status), normalizeToolSummary(t), buildToolDetail(t), t.name)
        }
        break
      
      case 'tool_result':
        if (event.payload?.tool) {
          const t = event.payload.tool
          appendToolTraceEvent(mid, mapToolEventState(t.status), normalizeToolSummary(t), buildToolDetail(t), t.name)
        }
        break

      case 'confirm_request':
        confirmOnlyStreamMap[mid] = true
        // 记录确认卡片
        if (event.payload?.confirm) {
          // 这里我们只是记录，由 computed 判断是否需要弹出
          // 实际上 applyConfirmOverlay 会立即修改全局状态，
          // 在刷新恢复场景下，我们只需设置状态即可。
        }
        break
      case 'business_card':
        if (event.payload?.business_card) {
          appendBusinessCardEvent(mid, event.payload.business_card)
        }
        break

      case 'business_card':
        if (event.payload?.business_card) {
          appendBusinessCardEvent(mid, event.payload.business_card, event.seq)
        }
        break
    }
  }

  // 特殊逻辑：如果最后一条是 confirm_request，则激活弹出层
  const lastEvent = events[events.length - 1]
  if (lastEvent?.kind === 'confirm_request' && lastEvent.payload?.confirm) {
    applyConfirmOverlay(lastEvent.payload.confirm)
  }

  return result
}

async function loadConversationMessages(conversationId: string, forceReload = false) {
  if (!conversationId) {
    return
  }

  if (!forceReload && conversationMessagesMap[conversationId] && unavailableHistoryMap[conversationId] !== true) {
    return
  }

  try {
    const events = await getConversationTimeline(conversationId)
    conversationMessagesMap[conversationId] = rebuildStateFromTimeline(conversationId, events)
    unavailableHistoryMap[conversationId] = false
  } catch (error) {
    console.error('Failed to load timeline:', error)
    unavailableHistoryMap[conversationId] = true
    ensureConversationBucket(conversationId)
  }
}

async function ensureConversationMeta(
  conversationId: string,
  options: EnsureConversationMetaOptions = {},
) {
  const { forceReload = false, syncListItem = false, listItemReveal } = options
  if (!conversationId || isDraftConversationId(conversationId)) {
    return
  }

  // 1. 默认复用本地 meta，避免每次发消息都触发重复请求。
  // 2. 新建会话需要“只更新当前一条标题”时，可通过 syncListItem 将 meta 回写到列表项。
  // 3. forceReload 仅用于需要强制拉取最新标题/计数的场景，避免改成全列表刷新。
  if (!forceReload && conversationMetaMap[conversationId]) {
    if (syncListItem) {
      syncConversationListItemFromMeta(conversationMetaMap[conversationId], listItemReveal)
    }
    return
  }

  try {
    const meta = await getConversationMeta(conversationId)
    upsertConversationMeta(meta)
    if (syncListItem) {
      syncConversationListItemFromMeta(meta, listItemReveal)
    }
  } catch {
    // 1. 标题和条数属于增强信息，不应阻塞聊天主链路。
    // 2. 即使元信息失败，列表里的回退标题仍可保证界面可用。
    // 3. 后续再次刷新列表或重新进入会话时，还会有机会补齐这些字段。
  }
}

// syncNewConversationTitleAfterFirstReply 负责在“新对话首条消息的 SSE 结束后”补齐标题。
// 职责边界：
// 1. 只处理首轮新会话标题补齐，不参与老会话每轮消息后的标题刷新策略。
// 2. 先立即拉一次，再做少量延迟重试，兼容后端“标题异步生成稍晚到达”的场景。
// 3. 每次都只回写当前会话对应的列表项，不触发整表刷新，避免界面出现“重置式加载”。
async function syncNewConversationTitleAfterFirstReply(conversationId: string) {
  if (!conversationId || isDraftConversationId(conversationId)) {
    return
  }

  const retryDelaysMs = [0, 420, 980]
  for (const delayMs of retryDelaysMs) {
    // 1. 首次 0ms 立即请求，尽量在流结束后第一时间拿到真实标题。
    // 2. 后续延迟重试只做短时补偿，避免长时间轮询造成多余请求。
    // 3. 单次请求失败由 ensureConversationMeta 内部兜底吞掉，不阻塞聊天主流程。
    if (delayMs > 0) {
      await new Promise<void>((resolve) => {
        window.setTimeout(resolve, delayMs)
      })
    }

    await ensureConversationMeta(conversationId, {
      forceReload: true,
      syncListItem: true,
      listItemReveal: {
        animate: true,
      },
    })

    const meta = conversationMetaMap[conversationId]
    if (meta?.has_title && meta.title) {
      return
    }
  }
}

async function loadConversationContextStats(conversationId: string, forceReload = false) {
  // 1. draft 会话还没有稳定 chat_id，直接请求只会得到无意义的空结果，因此这里提前短路。
  // 2. 已经读过且本轮没有强制刷新时复用本地缓存，避免切换同一会话时重复打点接口。
  // 3. 接口失败时统一回退为 null 占位，不在切会话时弹错误，避免把增强信息做成高频打扰。
  if (!conversationId || isDraftConversationId(conversationId)) {
    return
  }

  if (!forceReload && conversationContextStatsReadyMap[conversationId] === true) {
    return
  }

  conversationContextStatsLoadingMap[conversationId] = true
  try {
    conversationContextStatsMap[conversationId] = await getContextStats(conversationId)
    conversationContextStatsReadyMap[conversationId] = true
  } catch {
    delete conversationContextStatsMap[conversationId]
    conversationContextStatsReadyMap[conversationId] = false
  } finally {
    conversationContextStatsLoadingMap[conversationId] = false
  }
}

async function selectConversation(conversationId: string) {
  cancelEditUserMessage()
  resetConfirmOverlay()
  selectedConversationId.value = conversationId
  await Promise.allSettled([
    loadConversationMessages(conversationId),
    ensureConversationMeta(conversationId),
    loadConversationContextStats(conversationId),
  ])
  scheduleScrollMessagesToBottom(false, true)
}

function startNewConversation() {
  cancelEditUserMessage()
  resetConfirmOverlay()
  selectedConversationId.value = ''
  messageInput.value = ''
  activeStreamingMessageId.value = ''
  shouldAutoFollowMessages.value = true
}

function isManualThinkingEnabled(mode: ThinkingModeType) {
  return mode === 'true'
}

async function openFineTuneModal(data: SchedulePreviewData) {
  // 1. 如果点击的是占位卡片（尚未加载详情），则触发实时拉取。
  if ((data as any).is_placeholder) {
    if (fineTuneLoading.value) return
    
    fineTuneLoading.value = true
    try {
      const realData = await getSchedulePreview(selectedConversationId.value)
      // 成功后覆盖占位符，下次点击无需再查
      activeFineTuneData.value = realData
      
      // 这里的逻辑主要是为了同步界面上的 card 状态（如果是合并消息，对应的 id 为 dm.id）
      for (const mid of Object.keys(scheduleResultMap)) {
        if ((scheduleResultMap[mid] as any).is_placeholder) {
          scheduleResultMap[mid] = realData
        }
      }
    } catch (error: any) {
      console.error('Lazy load schedule failed:', error)
      ElMessage.warning('编排方案正在生成中，请稍候再试...')
      return
    } finally {
      fineTuneLoading.value = false
    }
  } else {
    activeFineTuneData.value = data
  }

  isFineTuneModalVisible.value = true
}

function closeFineTuneModal() {
  isFineTuneModalVisible.value = false
}

function handleScheduleSaved() {
  // 保存成功后可选的操作：重新刷新历史或状态
  if (selectedConversationId.value) {
    void loadConversationContextStats(selectedConversationId.value, true)
  }
}

function closeConfirmOverlay() {
  // 1. “手动关闭”与“自动收起”要区分：手动关闭后，本次 interaction 的重复分片不应反复弹层。
  // 2. 仅恢复对话框可见性，不改后端 pending 状态；真正的确认流转仍由用户点击确认/拒绝触发。
  confirmOverlayState.visible = false
  confirmOverlayState.manuallyClosed = true
  confirmRejectDraft.value = ''
}

function resetConfirmOverlay() {
  // 1. 会话切换/新建会话时直接重置确认覆盖层，避免把上一个会话的确认状态误带到当前会话。
  // 2. interactionId 同时清空，确保下一次收到相同 ID 的事件也能被视为新事件并重新拉起卡片。
  confirmOverlayState.visible = false
  confirmOverlayState.manuallyClosed = false
  confirmOverlayState.interactionId = ''
  confirmOverlayState.title = ''
  confirmOverlayState.summary = ''
  confirmRejectDraft.value = ''
}

async function sendConfirmAction(action: 'approve' | 'reject' | 'cancel') {
  const interactionId = confirmOverlayState.interactionId
  if (!interactionId) return

  // 1. 立即关闭覆盖层，并标记为“已手动处理”。
  // 这样在同一轮流式响应中，若后端重复推送相同的 interactionId，也不会再误拉起层。
  confirmOverlayState.visible = false
  confirmOverlayState.manuallyClosed = true
  const actionText = action === 'approve' ? '确认执行' : (action === 'reject' ? '拒绝执行' : '取消操作')
  await sendMessageInternal({
    preset: actionText,
    bypassConfirmOverlayCheck: true,
    requestExtra: {
      resume: {
        interaction_id: interactionId,
        type: 'confirm',
        action
      }
    }
  })
}

async function submitConfirmRejectMessage() {
  const text = confirmRejectDraft.value.trim()
  if (!text) return

  const interactionId = confirmOverlayState.interactionId
  if (!interactionId) return

  confirmOverlayState.visible = false
  confirmOverlayState.manuallyClosed = true
  await sendMessageInternal({
    preset: text,
    bypassConfirmOverlayCheck: true,
    requestExtra: {
      resume: {
        interaction_id: interactionId,
        type: 'confirm',
        action: 'reject'
      }
    }
  })
}

function handleConfirmRejectInputEnter(event: KeyboardEvent) {
  if (event.shiftKey) return
  event.preventDefault()
  void submitConfirmRejectMessage()
}

function applyConfirmOverlay(confirmPayload?: StreamConfirmPayload) {
  if (!confirmPayload) {
    return
  }

  const nextInteractionId = `${confirmPayload.interaction_id || ''}`.trim()
  const isNewInteraction = Boolean(nextInteractionId) && nextInteractionId !== confirmOverlayState.interactionId

  // 1. 同一个 interaction 会被多次分片推送；若用户已经手动关闭，则不重复弹出。
  // 2. 只有拿到新的 interaction_id，才重置手动关闭标记并重新显示覆盖层。
  if (isNewInteraction) {
    confirmOverlayState.manuallyClosed = false
    confirmRejectDraft.value = ''
  }
  if (confirmOverlayState.manuallyClosed && !isNewInteraction) {
    return
  }

  if (nextInteractionId) {
    confirmOverlayState.interactionId = nextInteractionId
  }
  confirmOverlayState.title = `${confirmPayload.title || '操作确认'}`.trim() || '操作确认'
  confirmOverlayState.summary = `${confirmPayload.summary || ''}`.trim()
  confirmOverlayState.visible = true
}

function buildChatRequestExtra(
  planningTaskClassIds: number[] = [],
): ChatRequestExtra | undefined {
  const extra: ChatRequestExtra = {}
  
  // 1. 任务类别过滤：将智能编排所需的 task_class_ids 透传给后端。
  if (planningTaskClassIds.length > 0) {
    extra.task_class_ids = [...planningTaskClassIds]
  }

  // 2. 执行模式控制：若开启“自动执行”，则透传 always_execute 标志，跳过工具调用确认逻辑。
  if (selectedExecutionMode.value === 'always') {
    extra.always_execute = true
  }

  return Object.keys(extra).length > 0 ? extra : undefined
}

function handlePlanningSelectionApplied(taskClassIds: number[]) {
  if (taskClassIds.length <= 0 || messageInput.value.trim()) {
    return
  }
  messageInput.value = DEFAULT_PLANNING_PROMPT
}

// fetchChatStream 负责以 fetch 方式发起聊天请求，并处理一次 refresh token 自动重试。
// 职责边界：
// 1. 只负责把请求发出去并返回原始 Response，不在这里解析 SSE 数据。
// 2. 401 时优先尝试用 refresh token 换新 access token，并只重试一次，避免死循环。
// 3. 若最终仍未通过鉴权，则清空本地登录态，让页面统一回到重新登录的安全状态。
async function fetchChatStream(body: ChatStreamRequest, signal?: AbortSignal, attempt = 0): Promise<Response> {
  const response = await fetch('/api/v1/agent/chat', {
    method: 'POST',
    headers: {
      'Content-Type': 'application/json',
      Authorization: `Bearer ${authStore.accessToken}`,
    },
    body: JSON.stringify(body),
    signal,
  })

  if (response.status === 401 && attempt === 0 && authStore.refreshToken) {
    const tokens = await refreshToken({
      old_refresh_token: authStore.refreshToken,
    })
    authStore.applyTokenPair(tokens)
    return fetchChatStream(body, signal, attempt + 1)
  }

  if (response.status === 401) {
    authStore.clearSession()
    throw new Error('登录状态已失效，请重新登录')
  }

  if (!response.ok) {
    try {
      const errorBody = (await response.json()) as { info?: string; message?: string }
      throw new Error(errorBody.info || errorBody.message || '发送消息失败，请稍后重试')
    } catch (error) {
      if (error instanceof Error) {
        throw error
      }
      throw new Error('发送消息失败，请稍后重试')
    }
  }

  if (!response.body) {
    throw new Error('流式响应体为空，无法继续接收消息')
  }

  return response
}

function prepareAssistantMessageForStreaming(message: AssistantMessage, createdAt: string) {
  message.content = ''
  message.reasoning = ''
  message.createdAt = createdAt
  thinkingMessageMap[message.id] = isManualThinkingEnabled(selectedThinkingMode.value)
  reasoningCollapsedMap[message.id] = false
  delete reasoningStartedAtMap[message.id]
  delete reasoningDurationMap[message.id]
  clearToolTraceState(message.id)
  toolTraceEventsMap[message.id] = []
  statusTraceEventsMap[message.id] = []
  assistantContentBlocksMap[message.id] = []
  assistantTimelineLastKindMap[message.id] = 'other'
}

function handleStreamExtraEvent(extra: StreamExtraPayload | undefined, assistantMessage: AssistantMessage) {
  if (!extra?.kind) {
    return
  }

  // 结构化 extra 事件（如卡片、工具调用、状态变更）处理逻辑。
  // 注意：为了避免请求爆炸，不再每个事件都刷新统计信息。仅在里程碑事件（如卡片生成）处精准刷新。

  if (extra.kind === 'confirm_request') {
    // 1. 记录“confirm 到来前是否已存在可见正文/思考”。
    // 2. 若已有可见前缀，后续流结束时只隐藏 confirm 相关部分，不删除整条消息。
    if (assistantMessage.content.trim() || `${assistantMessage.reasoning || ''}`.trim()) {
      confirmVisiblePrefixMap[assistantMessage.id] = true
    }
    confirmOnlyStreamMap[assistantMessage.id] = true
    applyConfirmOverlay(extra.confirm)
    return
  }

  if (extra.kind === 'tool_call' && extra.tool) {
    appendToolTraceEvent(
      assistantMessage.id,
      mapToolEventState(extra.tool.status || 'start'),
      normalizeToolSummary(extra.tool),
      buildToolDetail(extra.tool),
      `${extra.tool.name || ''}`,
    )
    return
  }

  if (extra.kind === 'tool_result' && extra.tool) {
    appendToolTraceEvent(
      assistantMessage.id,
      mapToolEventState(extra.tool.status || 'done'),
      normalizeToolSummary(extra.tool),
      buildToolDetail(extra.tool),
      `${extra.tool.name || ''}`,
    )
    if (extra.tool.status === 'done') {
      void loadConversationContextStats(selectedConversationId.value, true)
    }
    return
  }

  if (extra.kind === 'status' && extra.status) {
    // 1. status 是固定节点（rough_build/order_guard/compact 等）的主通道，需要进入时间线。
    // 2. 兼容老协议：若 status.code 仍是 tool_*，归并到工具事件，避免重复两条。
    // 3. 非工具状态统一转为“节点状态行”，和正文按 seq 自然穿插。
    const code = normalizeStatusCode(extra.status.code)
    if (isLegacyToolStatusCode(code)) {
      appendToolTraceEvent(
        assistantMessage.id,
        mapLegacyToolStatusToState(code),
        `${extra.status.summary || '工具事件'}`.trim() || '工具事件',
      )
      return
    }
    if (!shouldSkipStatusEvent(code, `${extra.stage || ''}`.trim())) {
      appendStatusTraceEvent(
        assistantMessage.id,
        code,
        buildStatusSummary(extra),
        `${extra.stage || ''}`,
      )
    }
    scheduleScrollMessagesToBottom(true)
  }

  if (extra.kind === 'business_card' && extra.business_card) {
    appendBusinessCardEvent(assistantMessage.id, extra.business_card)
    scheduleScrollMessagesToBottom(true)
  }
}

function shouldSuppressReasoningDeltaByExtraKind(kind?: string) {
  if (!kind) {
    return false
  }
  return kind === 'status' || kind === 'tool_call' || kind === 'tool_result'
}

// processSseBlock 负责解析单个 SSE block，并把增量内容落到当前 assistant message 上。
// 职责边界：
// 1. 会把同一个 block 里的多行 data: 合并后再解析，兼容标准 SSE 多行数据格式。
// 2. 同时兼容 choices[0].delta 和平铺 content/reasoning_content 两种载荷，避免后端切换实现时前端失配。
// 3. 收到 finish_reason 或 [DONE] 时立即收尾，并自动折叠思考框，让最终阅读视图更接近 DeepSeek 风格。
function processSseBlock(block: string, assistantMessage: AssistantMessage) {
  const dataLines = block
    .split('\n')
    .map((line) => line.trim())
    .filter((line) => line.startsWith('data:'))
    .map((line) => line.replace(/^data:\s*/, ''))

  if (dataLines.length === 0) {
    return
  }

  const payload = dataLines.join('\n').trim()
  if (!payload) {
    return
  }

  if (payload === '[DONE]') {
    if (isThinkingMessage(assistantMessage)) {
      finishCurrentReasoningBlock(assistantMessage.id)
    }
    activeStreamingMessageId.value = ''
    // 整个 SSE 流结束信号
    void loadConversationContextStats(selectedConversationId.value, true)
    return
  }

  let parsed: StreamEventPayload
  try {
    parsed = JSON.parse(payload) as StreamEventPayload
  } catch {
    return
  }

  if (parsed.error?.message) {
    throw new Error(parsed.error.message)
  }

  handleStreamExtraEvent(parsed.extra, assistantMessage)

  const shouldSuppressVisibleDelta = confirmOnlyStreamMap[assistantMessage.id] === true
  const shouldSuppressReasoningByExtraKind = shouldSuppressReasoningDeltaByExtraKind(parsed.extra?.kind)
  const choice = parsed.choices?.[0]
  const delta = choice?.delta ?? parsed.delta ?? parsed
  const finishReason = choice?.finish_reason ?? parsed.finish_reason ?? null

  if (
    !shouldSuppressVisibleDelta &&
    !shouldSuppressReasoningByExtraKind &&
    typeof delta?.reasoning_content === 'string' &&
    delta.reasoning_content
  ) {
    // 正文回流后仍允许追加 reasoning（工具调用摘要、阶段状态等），
    // 但不再切换面板状态，避免 UI 闪烁。
    if (!assistantMessage.content.trim()) {
      markReasoningStart(assistantMessage)
      thinkingMessageMap[assistantMessage.id] = true
    }
    if (!assistantReasoningSeqMap[assistantMessage.id]) {
      assistantReasoningSeqMap[assistantMessage.id] = nextAssistantTimelineSeq()
    }
    appendAssistantReasoningChunk(assistantMessage.id, delta.reasoning_content)
    assistantMessage.reasoning = `${assistantMessage.reasoning || ''}${delta.reasoning_content}`
  }

  if (!shouldSuppressVisibleDelta && typeof delta?.content === 'string' && delta.content) {
    appendAssistantContentChunk(assistantMessage.id, delta.content)
    if (isThinkingMessage(assistantMessage)) {
      finishCurrentReasoningBlock(assistantMessage.id)
    }
    assistantMessage.content += delta.content
  }

  if (finishReason) {
    if (isThinkingMessage(assistantMessage)) {
      finishCurrentReasoningBlock(assistantMessage.id)
    }
    activeStreamingMessageId.value = ''
    // 单条消息结束标志
    void loadConversationContextStats(selectedConversationId.value, true)
  }

  if (!shouldSuppressVisibleDelta) {
    scheduleScrollMessagesToBottom(false)
  }
}

async function streamAssistantReply(
  draftConversationId: string,
  text: string,
  assistantMessage: AssistantMessage,
  createdAt: string,
  shouldSyncCurrentConversationMeta: boolean,
  requestExtra?: ChatRequestExtra,
  signal?: AbortSignal,
) : Promise<string> {
  const response = await fetchChatStream({
    conversation_id: isDraftConversationId(draftConversationId) ? undefined : draftConversationId,
    message: text,
    model: 'worker',
    thinking: selectedThinkingMode.value,
    extra: requestExtra,
  }, signal)

  const responseConversationId = response.headers.get('X-Conversation-ID')?.trim()
  const actualConversationId = responseConversationId || draftConversationId

  if (actualConversationId !== draftConversationId) {
    migrateConversationState(draftConversationId, actualConversationId)
    if (shouldSyncCurrentConversationMeta) {
      prependConversationPreview(actualConversationId, text, createdAt)
    }
  }

  const reader = response.body!.getReader()
  const decoder = new TextDecoder('utf-8')
  let buffer = ''

  while (true) {
    const { done, value } = await reader.read()
    if (done) {
      break
    }

    buffer += decoder.decode(value, { stream: true })
    const blocks = buffer.split(/\r?\n\r?\n/)
    buffer = blocks.pop() ?? ''

    for (const block of blocks) {
      processSseBlock(block, assistantMessage)
    }
  }

  buffer += decoder.decode()
  if (buffer.trim()) {
    processSseBlock(buffer, assistantMessage)
  }

  const confirmConsumedByCard = confirmOnlyStreamMap[assistantMessage.id] === true
  const hasVisiblePrefixBeforeConfirm = confirmVisiblePrefixMap[assistantMessage.id] === true
  if (confirmConsumedByCard && !hasVisiblePrefixBeforeConfirm) {
    // 1. confirm_request 属于“卡片专属事件”，正文区域不应再显示这条 assistant 占位消息。
    // 2. 这里直接移除占位消息，避免出现空白气泡、时间戳残留或兜底文案误导。
    // 3. 同步清理消息状态映射，防止同一 ID 的历史状态污染下一轮发送。
    removeConversationMessage(actualConversationId, assistantMessage.id)
    cleanupHiddenAssistantMessageState(assistantMessage.id)
  } else if (confirmConsumedByCard) {
    // confirm 前已有正文/思考，保留前缀内容，仅清理 confirm 分流标记。
    clearConfirmStreamFlags(assistantMessage.id)
  } else if (!assistantMessage.content.trim()) {
    assistantMessage.content = assistantMessage.reasoning?.trim()
      ? '已完成深度思考，但当前响应未返回正文内容。'
      : '暂未收到回复正文，请稍后重试。'
  }

  // 4. 传输彻底结束后，做最后一次上下文统计更新，确保最终的 Token 用量胶囊是准确的。
  void loadConversationContextStats(actualConversationId, true)

  if (shouldSyncCurrentConversationMeta) {
    await syncNewConversationTitleAfterFirstReply(actualConversationId)
  }

  return actualConversationId
}

// stopStreaming 负责中断正在进行的 SSE 流式请求。
// 职责边界：只调用 AbortController.abort()，不修改 chatLoading 等状态——
// 这些状态由 sendMessage 的 finally 块统一清理，避免多处重置导致状态不一致。
function stopStreaming() {
  streamAbortController.value?.abort()
}

// sendMessage 负责执行”本地先上屏，再异步接流”的发送链路。
// 职责边界：
// 1. 先创建用户消息和 assistant 占位消息，让发送动作立即反馈到界面，等待建连过程无感化。
// 2. 若当前是新会话，则先使用 draft 会话承接本地状态，等响应头返回真实 conversation_id 后再整体迁移。
// 3. 网络错误只中断当前这轮 assistant 占位，不回滚用户已发送的内容，避免“点了发送却像没发出去”。
interface SendMessageOptions {
  preset?: string
  requestExtra?: ChatRequestExtra
  resetPlanningSelectionOnSuccess?: boolean
  bypassConfirmOverlayCheck?: boolean
}

// sendMessageInternal 负责执行“本地先上屏，再异步接流”的统一发送链路。
//
// 职责边界：
// 1. 统一承接普通发送和 confirm_action 发送，避免两套发送逻辑分叉后状态不一致。
// 2. 只负责前端本轮状态流转，不负责后端 pending 语义判定。
// 3. 失败时保留用户已发文本，只补齐占位消息兜底文案，确保交互可感知。
async function sendMessageInternal(options: SendMessageOptions = {}) {
  const text = (options.preset ?? messageInput.value).trim()
  const isResume = Boolean(options.requestExtra?.resume)
  if ((!text && !isResume) || chatLoading.value) {
    return
  }

  // 1. 有 confirm 覆盖层且不是“覆盖层按钮触发”的发送时，阻止误发送。
  // 2. 覆盖层内确认/拒绝按钮会显式传入 bypass，允许继续发送 confirm_action。
  if (shouldShowDialogConfirmOverlay.value && !options.bypassConfirmOverlayCheck) {
    ElMessage.warning('当前有待确认操作，请先处理确认卡片。')
    return
  }

  chatLoading.value = true

  const planningTaskClassIdsForRequest = options.requestExtra ? [] : [...pendingPlanningTaskClassIds.value]
  const requestExtra = options.requestExtra ?? buildChatRequestExtra(planningTaskClassIdsForRequest)
  const resetPlanningSelectionOnSuccess =
    options.resetPlanningSelectionOnSuccess ?? planningTaskClassIdsForRequest.length > 0
  const isNewConversationRound = !selectedConversationId.value
  const draftConversationId = selectedConversationId.value || createDraftConversationId()

  if (!selectedConversationId.value) {
    selectedConversationId.value = draftConversationId
  }
  ensureConversationBucket(draftConversationId)
  unavailableHistoryMap[draftConversationId] = false

  const now = new Date().toISOString()
  appendConversationMessage(draftConversationId, {
    id: createMessageId('user'),
    role: 'user',
    content: text,
    createdAt: now,
  })

  const assistantMessage = appendConversationMessage(draftConversationId, {
    id: createMessageId('assistant'),
    role: 'assistant',
    content: '',
    createdAt: now,
    reasoning: '',
  })

  thinkingMessageMap[assistantMessage.id] = isManualThinkingEnabled(selectedThinkingMode.value)
  reasoningCollapsedMap[assistantMessage.id] = false
  activeStreamingMessageId.value = assistantMessage.id

  messageInput.value = ''
  prependConversationPreview(draftConversationId, text, now)
  scheduleScrollMessagesToBottom(false, true)

  const controller = new AbortController()
  streamAbortController.value = controller

  try {
    const actualConversationId = await streamAssistantReply(
      draftConversationId,
      text,
      assistantMessage,
      now,
      isNewConversationRound,
      requestExtra,
      controller.signal,
    )
    if (resetPlanningSelectionOnSuccess) {
      pendingPlanningTaskClassIds.value = []
    }
    await Promise.allSettled([
      loadConversationContextStats(actualConversationId, true),
    ])
  } catch (error) {
    if (confirmOnlyStreamMap[assistantMessage.id] === true) {
      // 1. confirm 事件流中断时，正文占位消息同样不应暴露给用户。
      // 2. 这里按“卡片优先”策略移除占位，并清理消息状态，避免出现半截文本。
      const hasVisiblePrefixBeforeConfirm = confirmVisiblePrefixMap[assistantMessage.id] === true
      if (!hasVisiblePrefixBeforeConfirm) {
        const fallbackConversationId = selectedConversationId.value || draftConversationId
        removeConversationMessage(fallbackConversationId, assistantMessage.id)
        cleanupHiddenAssistantMessageState(assistantMessage.id)
      } else {
        // confirm 前已有可见内容时，保留前缀文本并只清理分流标记。
        clearConfirmStreamFlags(assistantMessage.id)
      }
      return
    }

    if (controller.signal.aborted) {
      if (!assistantMessage.content.trim()) {
        assistantMessage.content = '本次回复已手动停止。'
      }
    } else {
      if (!assistantMessage.content.trim()) {
        assistantMessage.content = '本次回复已中断，请稍后重试。'
      }
      ElMessage.error(error instanceof Error ? error.message : '发送消息失败，请稍后重试')
    }
    reasoningCollapsedMap[assistantMessage.id] = false
  } finally {
    streamAbortController.value = null
    activeStreamingMessageId.value = ''
    chatLoading.value = false
  }
}

async function sendMessage(preset?: string) {
  await sendMessageInternal({ preset })
}

watch(
  () => selectedMessages.value.length,
  () => {
    scheduleScrollMessagesToBottom(false)
  },
)


onMounted(async () => {
  reasoningTicker = window.setInterval(() => {
    reasoningDisplayNow.value = Date.now()
  }, 1000)
  window.addEventListener('resize', syncHistoryPanelWidthForViewport)
  syncHistoryPanelWidthForViewport()
  await loadConversationListData(true)
  syncHistoryPanelWidthForViewport()
})

onBeforeUnmount(() => {
  if (messageScrollRaf) {
    cancelAnimationFrame(messageScrollRaf)
  }
  if (messageScrollReleaseRaf) {
    cancelAnimationFrame(messageScrollReleaseRaf)
  }
  if (reasoningTicker) {
    window.clearInterval(reasoningTicker)
    reasoningTicker = 0
  }
  for (const timerId of conversationListItemRevealTimerMap.values()) {
    window.clearTimeout(timerId)
  }
  conversationListItemRevealTimerMap.clear()
  releaseHistoryResizeListeners()
  window.removeEventListener('resize', syncHistoryPanelWidthForViewport)
})
</script>

<template>
  <aside class="assistant-shell" :class="{ 'assistant-shell--standalone': isStandaloneMode }">

    <div
      ref="assistantBodyRef"
      class="assistant-body"
      :class="{
        'assistant-body--collapsed': !historyExpanded,
        'assistant-body--standalone': isStandaloneMode,
      }"
      :style="assistantBodyStyle"
      >
        <aside class="assistant-history dashboard-item-pop" :class="{ 'assistant-history--collapsed': !historyExpanded }" :style="{ '--anim-delay': '0.05s' }">
          <div class="assistant-history__toolbar">
            <div v-if="historyExpanded" class="assistant-history__brand">
              <span class="assistant-history__brand-icon" aria-hidden="true">
                <svg width="16" height="16" viewBox="0 0 16 16" fill="none" xmlns="http://www.w3.org/2000/svg">
                  <path d="M3.09961 2.34961H12.9004C13.3146 2.34961 13.6504 2.6854 13.6504 3.09961V12.9004C13.6504 13.3146 13.3146 13.6504 12.9004 13.6504H3.09961C2.6854 13.6504 2.34961 13.3146 2.34961 12.9004V3.09961C2.34961 2.6854 2.6854 2.34961 3.09961 2.34961Z" fill="currentColor" />
                  <path d="M4.7998 5.34961H11.2002V6.65039H4.7998V5.34961ZM4.7998 7.34961H9.2998V8.65039H4.7998V7.34961ZM4.7998 9.34961H11.2002V10.6504H4.7998V9.34961Z" fill="white" />
                </svg>
              </span>
              <strong>对话历史</strong>
            </div>
            <button
              type="button"
              class="assistant-history__toggle"
              :aria-label="historyExpanded ? '收起历史会话' : '展开历史会话'"
              @click="toggleHistoryPanel"
            >
              <svg
                class="assistant-history__toggle-icon"
                :class="{ 'assistant-history__toggle-icon--collapsed': !historyExpanded }"
                width="14"
                height="14"
                viewBox="0 0 14 14"
                fill="none"
                xmlns="http://www.w3.org/2000/svg"
                aria-hidden="true"
              >
                <path d="M8.5 2.15137L8.07617 2.57617L5.34863 5.30273C5.09294 5.55843 4.86618 5.78438 4.70215 5.98828C4.53117 6.20088 4.38244 6.44405 4.33398 6.75C4.30778 6.91565 4.30778 7.08435 4.33398 7.25C4.38244 7.55595 4.53117 7.79912 4.70215 8.01172C4.86618 8.21561 5.09294 8.44157 5.34863 8.69727L8.07617 11.4238L8.5 11.8486L9.34863 11L8.92383 10.5762L6.19727 7.84863C5.92268 7.57405 5.75151 7.40124 5.6377 7.25977C5.53096 7.12709 5.52187 7.07728 5.51953 7.0625C5.51297 7.02105 5.51297 6.97895 5.51953 6.9375C5.52187 6.92272 5.53096 6.87291 5.6377 6.74023C5.75152 6.59876 5.92268 6.42595 6.19727 6.15137L8.92383 3.42383L9.34863 3L8.5 2.15137Z" fill="currentColor" />
              </svg>
            </button>
          </div>

          <div ref="historyContentRef" class="assistant-history__content" @scroll="handleHistoryScroll">
            <button type="button" class="assistant-history__new" @click="startNewConversation">
              <span class="assistant-history__new-icon" aria-hidden="true">
                <svg width="16" height="16" viewBox="0 0 16 16" fill="none" xmlns="http://www.w3.org/2000/svg">
                  <path d="M8 0.599609C3.91309 0.599609 0.599609 3.91309 0.599609 8C0.599609 9.13376 0.855461 10.2098 1.3125 11.1719L1.5918 11.7588L2.76562 11.2012L2.48633 10.6143C2.11034 9.82278 1.90039 8.93675 1.90039 8C1.90039 4.63106 4.63106 1.90039 8 1.90039C11.3689 1.90039 14.0996 4.63106 14.0996 8C14.0996 11.3689 11.3689 14.0996 8 14.0996C7.31041 14.0996 6.80528 14.0514 6.35742 13.9277C5.91623 13.8059 5.49768 13.6021 4.99707 13.2529C4.26492 12.7422 3.21611 12.5616 2.35156 13.1074L2.33789 13.1162L2.32422 13.126L1.58789 13.6436L2.01953 14.9297L3.0459 14.207C3.36351 14.0065 3.83838 14.0294 4.25293 14.3184C4.84547 14.7317 5.39743 15.011 6.01172 15.1807C6.61947 15.3485 7.25549 15.4004 8 15.4004C12.0869 15.4004 15.4004 12.0869 15.4004 8C15.4004 3.91309 12.0869 0.599609 8 0.599609ZM7.34473 4.93945V7.34961H4.93945V8.65039H7.34473V11.0605H8.64551V8.65039H11.0605V7.34961H8.64551V4.93945H7.34473Z" fill="currentColor" />
                </svg>
              </span>
              <span v-if="historyExpanded" class="assistant-history__new-text">开启新对话</span>
            </button>

            <div v-if="conversationLoading && !conversationListReady" class="assistant-history__loading">
              <div v-for="index in 4" :key="index" class="assistant-history__loading-item" />
            </div>

            <template v-else>
              <div v-for="group in groupedConversationList" :key="group.key" class="assistant-history__group">
                <p v-if="historyExpanded" class="assistant-history__group-title">{{ group.label }}</p>
                <button
                  v-for="item in group.items"
                  :key="item.conversation_id"
                  type="button"
                  class="assistant-history__item"
                  :class="{
                    'assistant-history__item--active': item.conversation_id === selectedConversationId,
                    'assistant-history__item--reveal': isConversationListItemRevealing(item.conversation_id),
                  }"
                  @click="selectConversation(item.conversation_id)"
                >
                  <span class="assistant-history__item-title">
                    {{ item.title || '未命名会话' }}
                  </span>
                  <small v-if="historyExpanded" class="assistant-history__item-time">
                    {{ formatConversationTime(item.last_message_at || item.created_at) }}
                  </small>
                </button>
              </div>

              <p v-if="!conversationList.length" class="assistant-history__empty">暂无历史会话</p>
              <p v-else-if="!conversationHasMore && !conversationLoadingMore" class="assistant-history__end">已经到底了</p>

            <div v-if="conversationLoadingMore" class="assistant-history__loading assistant-history__loading--more">
              <div v-for="index in 2" :key="index" class="assistant-history__loading-item" />
            </div>
          </template>
        </div>
      </aside>

      <div
        class="assistant-splitter dashboard-item-pop"
        :class="{ 'assistant-splitter--hidden': !historyExpanded }"
        :style="{ '--anim-delay': '0.08s' }"
        role="separator"
        aria-label="调整会话列表宽度"
        @pointerdown.prevent="startResizeHistoryPanel"
      >
        <span class="assistant-splitter__line" />
      </div>

      <section 
        class="assistant-chat dashboard-item-pop" 
        :class="{ 'assistant-chat--empty': !selectedMessages.length && !chatLoading }"
        :style="{ '--anim-delay': '0.1s' }"
      >
        <div class="assistant-chat__spacer-top" />
        <div
          ref="messageViewportRef"
          class="assistant-messages"
          @scroll.passive="handleMessageViewportScroll"
          @wheel.passive="handleMessageViewportWheel"
        >
          <div v-if="shouldShowHistoryFallback" class="assistant-chat__fallback">
            当前会话的历史消息暂时不可读，但你仍然可以继续追问；后续刷新后会自动恢复。
          </div>

          <TransitionGroup v-if="selectedMessages.length" tag="div" name="message-stagger" class="assistant-message-list">
              <article
                v-for="dm in displayMessages"
                :key="dm.id"
                class="chat-message"
                :class="`chat-message--${dm.role}`"
              >
            <div v-if="dm.role === 'user'" class="chat-message__user-row">
              <div class="chat-message__user-bubble">
                <template v-if="isEditingUserMessage(dm.id)">
                  <div class="chat-message__editor">
                    <textarea
                      v-model="editingUserMessageDraft"
                      class="chat-message__editor-textarea"
                      rows="3"
                    />
                    <div class="chat-message__editor-actions">
                      <button type="button" class="chat-message__editor-button chat-message__editor-button--ghost" @click="cancelEditUserMessage()">
                        取消
                      </button>
                      <button type="button" class="chat-message__editor-button chat-message__editor-button--primary" @click="submitEditedUserMessage(dm.sources[0])">
                        发送
                      </button>
                    </div>
                  </div>
                </template>
                <div v-else class="chat-message__markdown" v-html="renderMessageMarkdown(dm.content)" />
              </div>
              <div v-if="!isEditingUserMessage(dm.id)" class="chat-message__action-bar chat-message__action-bar--user">
                <button
                  type="button"
                  class="chat-message__icon-button"
                  aria-label="复制消息"
                  @click="copyText(dm.content, '已复制用户消息')"
                >
                  <svg width="16" height="16" viewBox="0 0 16 16" fill="none" xmlns="http://www.w3.org/2000/svg">
                    <path d="M6.14923 4.02032C7.11191 4.02032 7.87977 4.02017 8.49591 4.07599C9.12122 4.1327 9.65786 4.25188 10.1414 4.53107C10.7201 4.8653 11.2008 5.34591 11.535 5.92462C11.8142 6.40818 11.9333 6.94482 11.9901 7.57013C12.0459 8.18625 12.0457 8.9542 12.0457 9.91681C12.0457 10.8795 12.0459 11.6474 11.9901 12.2635C11.9333 12.8888 11.8142 13.4254 11.535 13.909C11.2008 14.4877 10.7201 14.9683 10.1414 15.3026C9.65786 15.5817 9.12122 15.7009 8.49591 15.7576C7.87977 15.8134 7.1119 15.8133 6.14923 15.8133C5.18661 15.8133 4.41868 15.8134 3.80255 15.7576C3.17724 15.7009 2.6406 15.5817 2.15704 15.3026C1.57834 14.9684 1.09772 14.4877 0.763489 13.909C0.484305 13.4254 0.365123 12.8888 0.308411 12.2635C0.252587 11.6474 0.252747 10.8795 0.252747 9.91681C0.252747 8.95419 0.252603 8.18625 0.308411 7.57013C0.365123 6.94482 0.484305 6.40818 0.763489 5.92462C1.09771 5.3459 1.57833 4.86529 2.15704 4.53107C2.6406 4.25188 3.17724 4.1327 3.80255 4.07599C4.41868 4.02018 5.1866 4.02032 6.14923 4.02032Z" fill="currentColor" />
                    <path d="M9.80157 0.367981C10.7637 0.367981 11.5313 0.367886 12.1473 0.423645C12.7725 0.480313 13.3093 0.598765 13.7928 0.877747C14.3716 1.21192 14.852 1.69355 15.1863 2.27228C15.4655 2.75575 15.5857 3.29165 15.6424 3.91681C15.6982 4.53301 15.6971 5.30161 15.6971 6.26447V7.8299C15.6971 8.29265 15.6989 8.58994 15.6649 8.84845C15.4667 10.3525 14.4009 11.5738 12.9832 11.9988V10.5467C13.6973 10.1903 14.2104 9.49662 14.3192 8.67169C14.3387 8.52348 14.3406 8.3358 14.3406 7.8299V6.26447C14.3406 5.27707 14.3398 4.58149 14.2908 4.04083C14.2427 3.50969 14.1526 3.19373 14.0125 2.95099C13.7974 2.5785 13.4875 2.2687 13.1151 2.05353C12.8723 1.91347 12.5563 1.82237 12.0252 1.77423C11.4846 1.72528 10.7888 1.7254 9.80157 1.7254H7.71466C6.75614 1.72559 5.92659 2.27697 5.52325 3.07892H4.07013C4.54215 1.51132 5.99314 0.368192 7.71466 0.367981H9.80157Z" fill="currentColor" />
                  </svg>
                </button>
                <button
                  type="button"
                  class="chat-message__icon-button"
                  aria-label="修改消息"
                  @click="startEditUserMessage(dm.sources[0])"
                >
                  <svg width="16" height="16" viewBox="0 0 16 16" fill="none" xmlns="http://www.w3.org/2000/svg">
                    <path d="M9.94073 1.34942C10.7047 0.902314 11.6503 0.902418 12.4143 1.34942C12.706 1.52016 12.9687 1.79118 13.3104 2.13284C13.652 2.47448 13.9231 2.73721 14.0938 3.02894C14.5408 3.79295 14.5409 4.73856 14.0938 5.50251C13.9231 5.79415 13.652 6.05704 13.3104 6.39861L6.65929 13.0497C6.28065 13.4284 6.00692 13.7108 5.6654 13.9097C5.32388 14.1085 4.94312 14.2074 4.42702 14.3498L3.24391 14.6761C2.77524 14.8054 2.34535 14.9262 2.00128 14.9684C1.65193 15.0112 1.17961 15.0013 0.810733 14.6325C0.44189 14.2637 0.432076 13.7913 0.474829 13.442C0.517004 13.0979 0.63787 12.668 0.767151 12.1993L1.09349 11.0162C1.23585 10.5001 1.33478 10.1194 1.53356 9.77785C1.73246 9.43633 2.01487 9.1626 2.39352 8.78395L9.04463 2.13284C9.38622 1.79126 9.64908 1.52017 9.94073 1.34942Z" fill="currentColor" />
                    <path d="M15.5427 14.8398H7.5522L8.96704 13.425H15.5427V14.8398Z" fill="currentColor" />
                  </svg>
                </button>
              </div>
              <span class="chat-message__time chat-message__time--user">{{ formatMessageTime(dm.createdAt) }}</span>
            </div>

            <div v-else class="chat-message__assistant-flow">
              <TransitionGroup name="inner-fade">
                <div v-for="block in getDisplayAssistantBlocks(dm)" :key="block.id">
                  <article v-if="block.type === 'tool'" class="chat-message__tool">
                    <button
                      type="button"
                      class="chat-message__tool-head"
                      @click="block.event && toggleToolTraceExpanded(block.event.id)"
                    >
                      <span class="chat-message__tool-icon" aria-hidden="true">
                        <svg width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round" stroke-linejoin="round">
                          <path d="M14.7 6.3a1 1 0 0 0 0 1.4l1.6 1.6a1 1 0 0 0 1.4 0l3.77-3.77a6 6 0 0 1-7.94 7.94l-6.91 6.91a2.12 2.12 0 0 1-3-3l6.91-6.91a6 6 0 0 1 7.94-7.94l-3.76 3.76z" />
                        </svg>
                      </span>
                      <span class="chat-message__tool-summary">{{ block.event?.summary }}</span>
                      <em class="chat-message__tool-badge">{{ getToolTraceStateLabel(block.event?.state || 'completed') }}</em>
                      <span class="chat-message__tool-chevron" :class="{ 'chat-message__tool-chevron--expanded': block.event ? isToolTraceExpanded(block.event.id) : false }" aria-hidden="true">
                        <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round" stroke-linejoin="round">
                          <polyline points="9 18 15 12 9 6"></polyline>
                        </svg>
                      </span>
                    </button>

                    <p v-if="block.event && isToolTraceExpanded(block.event.id) && block.event.detail" class="chat-message__tool-detail">
                      {{ block.event.detail }}
                    </p>
                  </article>

                  <div v-else-if="block.type === 'status'" class="chat-message__status-line">
                    <span class="chat-message__status-icon" aria-hidden="true">
                      <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
                        <path d="M12 2v4" />
                        <path d="M12 18v4" />
                        <path d="M4.93 4.93l2.83 2.83" />
                        <path d="M16.24 16.24l2.83 2.83" />
                        <path d="M2 12h4" />
                        <path d="M18 12h4" />
                        <path d="M4.93 19.07l2.83-2.83" />
                        <path d="M16.24 7.76l2.83-2.83" />
                      </svg>
                    </span>
                    <span class="chat-message__status-text">{{ block.statusEvent?.summary }}</span>
                  </div>

                  <div v-else-if="block.type === 'reasoning'" class="chat-message__reasoning">
                    <div class="chat-message__reasoning-head">
                      <div class="chat-message__reasoning-title">
                        <span class="chat-message__reasoning-icon">
                          <svg
                            class="chat-message__reasoning-icon-svg"
                            viewBox="0 0 16 16"
                            fill="none"
                            xmlns="http://www.w3.org/2000/svg"
                            aria-hidden="true"
                          >
                            <path
                              d="M8.00195 6.64454C8.75029 6.64454 9.35735 7.25169 9.35742 8.00001C9.35742 8.74838 8.75033 9.35548 8.00195 9.35548C7.2537 9.35533 6.64746 8.74829 6.64746 8.00001C6.64753 7.25178 7.25374 6.64468 8.00195 6.64454Z"
                              fill="currentColor"
                            />
                            <path
                              fill-rule="evenodd"
                              clip-rule="evenodd"
                              d="M9.97168 1.29981C11.5854 0.718916 13.271 0.642197 14.3145 1.68555C15.3578 2.72902 15.2811 4.41466 14.7002 6.02833C14.4708 6.66561 14.1505 7.32937 13.75 8.00001C14.1505 8.67062 14.4708 9.33444 14.7002 9.97169C15.2811 11.5854 15.3579 13.271 14.3145 14.3145C13.271 15.3579 11.5854 15.2811 9.97168 14.7002C9.33443 14.4708 8.67062 14.1505 8 13.75C7.32936 14.1505 6.66561 14.4708 6.02832 14.7002C4.41464 15.2811 2.72902 15.3578 1.68555 14.3145C0.642186 13.271 0.718901 11.5854 1.29981 9.97169C1.52918 9.33454 1.84868 8.67049 2.24902 8.00001C1.84869 7.32953 1.52918 6.66544 1.29981 6.02833C0.718882 4.41459 0.6421 2.729 1.68555 1.68555C2.729 0.642112 4.41459 0.718887 6.02832 1.29981C6.66544 1.52918 7.32953 1.8487 8 2.24903C8.67048 1.84869 9.33454 1.52919 9.97168 1.29981ZM12.9404 9.2129C12.4391 9.893 11.8616 10.5681 11.2148 11.2149C10.5681 11.8616 9.89299 12.4391 9.21289 12.9404C9.62535 13.1579 10.0271 13.338 10.4121 13.4766C11.9146 14.0174 12.9173 13.8738 13.3955 13.3955C13.8737 12.9173 14.0174 11.9146 13.4766 10.4121C13.338 10.0271 13.1579 9.62535 12.9404 9.2129ZM3.05859 9.2129C2.84124 9.62523 2.662 10.0272 2.52344 10.4121C1.98255 11.9146 2.1263 12.9172 2.60449 13.3955C3.08281 13.8737 4.08548 14.0174 5.58789 13.4766C5.97267 13.338 6.37392 13.1577 6.78613 12.9404C6.10627 12.4393 5.43171 11.8614 4.78516 11.2149C4.13826 10.5679 3.55995 9.89313 3.05859 9.2129ZM7.99902 3.792C7.23182 4.31419 6.45309 4.95512 5.7041 5.70411C4.95512 6.45309 4.31418 7.23184 3.79199 7.99903C4.31434 8.76666 4.95474 9.54653 5.7041 10.2959C6.45312 11.0449 7.23274 11.6848 8 12.207C8.76728 11.6848 9.54686 11.0449 10.2959 10.2959C11.0449 9.54686 11.6848 8.76729 12.207 8.00001C11.6848 7.23275 11.0449 6.45312 10.2959 5.70411C9.54653 4.95475 8.76665 4.31434 7.99902 3.792ZM5.58789 2.52344C4.08536 1.98255 3.08275 2.12625 2.60449 2.6045C2.12624 3.08275 1.98255 4.08536 2.52344 5.5879C2.66192 5.97253 2.84143 6.37409 3.05859 6.78614C3.55986 6.10611 4.13843 5.43189 4.78516 4.78516C5.4319 4.13843 6.10609 3.55987 6.78613 3.0586C6.37408 2.84144 5.97252 2.66192 5.58789 2.52344ZM13.3955 2.6045C12.9172 2.12631 11.9146 1.98257 10.4121 2.52344C10.0272 2.66201 9.62522 2.84125 9.21289 3.0586C9.89313 3.55996 10.5679 4.13827 11.2148 4.78516C11.8614 5.43172 12.4392 6.10627 12.9404 6.78614C13.1577 6.37393 13.338 5.97267 13.4766 5.5879C14.0174 4.08549 13.8736 3.08281 13.3955 2.6045Z"
                              fill="currentColor"
                            />
                          </svg>
                        </span>
                        <span class="chat-message__reasoning-status">{{ getReasoningStatusLabel(block) }}</span>
                      </div>
                      <button
                        type="button"
                        class="chat-message__reasoning-toggle"
                        :aria-label="isReasoningCollapsed(block.id) ? '展开深度思考' : '折叠深度思考'"
                        @click="toggleReasoningCollapse(block.id)"
                      >
                        <span class="chat-message__reasoning-chevron">
                          <svg
                            class="chat-message__reasoning-chevron-icon"
                            :class="{ 'chat-message__reasoning-chevron-icon--expanded': !isReasoningCollapsed(block.id) }"
                            width="14"
                            height="14"
                            viewBox="0 0 14 14"
                            fill="none"
                            xmlns="http://www.w3.org/2000/svg"
                            aria-hidden="true"
                          >
                            <path
                              d="M5.5 2.15137L5.92383 2.57617L8.65137 5.30273C8.90706 5.55843 9.13382 5.78438 9.29785 5.98828C9.46883 6.20088 9.61756 6.44405 9.66602 6.75C9.69222 6.91565 9.69222 7.08435 9.66602 7.25C9.61756 7.55595 9.46883 7.79912 9.29785 8.01172C9.13382 8.21561 8.90706 8.44157 8.65137 8.69727L5.92383 11.4238L5.5 11.8486L4.65137 11L5.07617 10.5762L7.80273 7.84863C8.07732 7.57405 8.24849 7.40124 8.3623 7.25977C8.46904 7.12709 8.47813 7.07728 8.48047 7.0625C8.48703 7.02105 8.48703 6.97895 8.48047 6.9375C8.47813 6.92272 8.46904 6.87291 8.3623 6.74023C8.24848 6.59876 8.07732 6.42595 7.80273 6.15137L5.07617 3.42383L4.65137 3L5.5 2.15137Z"
                              fill="currentColor"
                            />
                          </svg>
                        </span>
                      </button>
                    </div>

                    <div v-if="isReasoningCollapsed(block.id) === false" class="chat-message__reasoning-body">
                      <div
                        v-if="block.text"
                        class="chat-message__markdown chat-message__markdown--reasoning"
                        v-html="renderMessageMarkdown(block.text || '')"
                      />
                      <div v-else class="chat-message__streaming chat-message__streaming--reasoning">
                        <div class="thinking-indicator">
                          <span class="thinking-indicator__text">正在思考</span>
                        </div>
                      </div>
                    </div>
                  </div>

                  <div v-else-if="block.type === 'business_card'" class="chat-message__business-card">
                    <BusinessCardRenderer :payload="block.businessCard" />
                  </div>

                  <div v-else-if="block.type === 'content'" class="chat-message__assistant-content">
                    <div class="chat-message__markdown chat-message__markdown--assistant" v-html="renderMessageMarkdown(block.text || '')" />
                  </div>

                  <template v-else-if="block.type === 'schedule_card' && block.schedulePreview">
                    <ScheduleResultCard
                      :summary="block.schedulePreview.summary"
                      @click="openFineTuneModal(block.schedulePreview)"
                    />
                  </template>

                  <div v-else-if="block.type === 'content_indicator'" class="assistant-timeline__answering-indicator">
                    <div class="thinking-indicator">
                      <span class="thinking-indicator__text">正在思考</span>
                    </div>
                  </div>
                </div>
              </TransitionGroup>

              <div v-if="dm.content && !isDisplayStreaming(dm)" class="chat-message__action-bar">
                <button
                  type="button"
                  class="chat-message__icon-button"
                  aria-label="复制回复"
                  @click="copyText(dm.content, '已复制回复内容')"
                >
                  <svg width="16" height="16" viewBox="0 0 16 16" fill="none" xmlns="http://www.w3.org/2000/svg">
                    <path d="M6.14923 4.02032C7.11191 4.02032 7.87977 4.02017 8.49591 4.07599C9.12122 4.1327 9.65786 4.25188 10.1414 4.53107C10.7201 4.8653 11.2008 5.34591 11.535 5.92462C11.8142 6.40818 11.9333 6.94482 11.9901 7.57013C12.0459 8.18625 12.0457 8.9542 12.0457 9.91681C12.0457 10.8795 12.0459 11.6474 11.9901 12.2635C11.9333 12.8888 11.8142 13.4254 11.535 13.909C11.2008 14.4877 10.7201 14.9683 10.1414 15.3026C9.65786 15.5817 9.12122 15.7009 8.49591 15.7576C7.87977 15.8134 7.1119 15.8133 6.14923 15.8133C5.18661 15.8133 4.41868 15.8134 3.80255 15.7576C3.17724 15.7009 2.6406 15.5817 2.15704 15.3026C1.57834 14.9684 1.09772 14.4877 0.763489 13.909C0.484305 13.4254 0.365123 12.8888 0.308411 12.2635C0.252587 11.6474 0.252747 10.8795 0.252747 9.91681C0.252747 8.95419 0.252603 8.18625 0.308411 7.57013C0.365123 6.94482 0.484305 6.40818 0.763489 5.92462C1.09771 5.3459 1.57833 4.86529 2.15704 4.53107C2.6406 4.25188 3.17724 4.1327 3.80255 4.07599C4.41868 4.02018 5.1866 4.02032 6.14923 4.02032Z" fill="currentColor" />
                    <path d="M9.80157 0.367981C10.7637 0.367981 11.5313 0.367886 12.1473 0.423645C12.7725 0.480313 13.3093 0.598765 13.7928 0.877747C14.3716 1.21192 14.852 1.69355 15.1863 2.27228C15.4655 2.75575 15.5857 3.29165 15.6424 3.91681C15.6982 4.53301 15.6971 5.30161 15.6971 6.26447V7.8299C15.6971 8.29265 15.6989 8.58994 15.6649 8.84845C15.4667 10.3525 14.4009 11.5738 12.9832 11.9988V10.5467C13.6973 10.1903 14.2104 9.49662 14.3192 8.67169C14.3387 8.52348 14.3406 8.3358 14.3406 7.8299V6.26447C14.3406 5.27707 14.3398 4.58149 14.2908 4.04083C14.2427 3.50969 14.1526 3.19373 14.0125 2.95099C13.7974 2.5785 13.4875 2.2687 13.1151 2.05353C12.8723 1.91347 12.5563 1.82237 12.0252 1.77423C11.4846 1.72528 10.7888 1.7254 9.80157 1.7254H7.71466C6.75614 1.72559 5.92659 2.27697 5.52325 3.07892H4.07013C4.54215 1.51132 5.99314 0.368192 7.71466 0.367981H9.80157Z" fill="currentColor" />
                  </svg>
                </button>
              </div>
              <span class="chat-message__time">{{ formatMessageTime(dm.createdAt) }}</span>
            </div>
            </article>
          </TransitionGroup>
        </div>

        <div class="assistant-chat__interaction-group">
          <!-- Welcome Content (Only in empty state) -->
          <Transition name="fade-switch">
            <div v-if="!selectedMessages.length && !chatLoading" class="assistant-empty">
              <div class="assistant-empty__halo" />
              <div class="assistant-empty__content">
                <strong>SmartMate AI 伙伴</strong>
                <p>我是你的智能助理，你可以从直接输入任务开始。</p>
              </div>
            </div>
          </Transition>

          <!-- Suggestion Chips -->
          <div v-if="quickActions.length" class="assistant-actions">
            <button
              v-for="action in quickActions"
              :key="action"
              type="button"
              class="assistant-actions__chip"
              :disabled="chatLoading"
              @click="sendMessage(action)"
            >
              {{ action }}
            </button>
          </div>

        <div class="_9a2f8e4 assistant-composer-ds" :class="{ 'assistant-composer-ds--confirm': shouldShowDialogConfirmOverlay }">
          <div
            v-if="shouldShowDialogConfirmOverlay"
            class="assistant-confirm-composer"
            role="dialog"
            aria-modal="true"
            aria-label="确认操作"
          >
            <article class="assistant-confirm-card">
              <header class="assistant-confirm-card__header">
                <p class="assistant-confirm-card__eyebrow">待确认操作</p>
                <h3 class="assistant-confirm-card__title">{{ confirmOverlayState.title || '操作确认' }}</h3>
              </header>

              <p v-if="confirmOverlayState.summary" class="assistant-confirm-card__summary">
                {{ confirmOverlayState.summary }}
              </p>
              <p class="assistant-confirm-card__hint">
                该操作需要你的明确确认。点击“关闭卡片”会恢复输入框，不会自动确认。
              </p>

              <div class="assistant-confirm-card__actions">
                <button
                  type="button"
                  class="assistant-confirm-card__button assistant-confirm-card__button--primary"
                  :disabled="chatLoading"
                  @click="sendConfirmAction('approve')"
                >
                  确认执行
                </button>

                <div class="assistant-confirm-card__reject-box">
                  <label class="assistant-confirm-card__reject-label" for="confirm-reject-input">
                    拒绝并提出调整要求
                  </label>
                  <textarea
                    id="confirm-reject-input"
                    v-model="confirmRejectDraft"
                    class="assistant-confirm-card__reject-input"
                    rows="3"
                    :disabled="chatLoading"
                    placeholder="例如：先不要执行，把学习任务改到周末晚上，课程日只保留复习。按 Enter 发送，Shift+Enter 换行。"
                    @keydown.enter.exact="handleConfirmRejectInputEnter"
                  />
                  <button
                    type="button"
                    class="assistant-confirm-card__button assistant-confirm-card__button--ghost"
                    :disabled="chatLoading || !confirmRejectDraft.trim()"
                    @click="submitConfirmRejectMessage"
                  >
                    发送拒绝要求
                  </button>
                </div>

                <button
                  type="button"
                  class="assistant-confirm-card__button assistant-confirm-card__button--plain"
                  @click="closeConfirmOverlay"
                >
                  关闭卡片
                </button>
              </div>
            </article>
          </div>

          <div v-else class="aaff8b8f">
            <div class="assistant-chat__spacer-bottom" />
            <div class="_77cefa5 _9996a53">
              <div class="_020ab5b">
                <TaskClassPlanningPicker
                  v-model="pendingPlanningTaskClassIds"
                  :disabled="chatLoading"
                  @applied="handlePlanningSelectionApplied"
                />

                <div class="_24fad49">
                  <textarea
                    v-model="messageInput"
                    class="_27c9245 ds-scroll-area ds-scroll-area--show-on-focus-within d96f2d2a"
                    placeholder="输入消息，按 Enter 发送"
                    rows="2"
                    @keydown.enter.exact.prevent="sendMessage()"
                  />
                  <div class="b13855df" />
                </div>

                <div class="ec4f5d61">
                  <div class="assistant-toolbar__pill assistant-toolbar__pill--select assistant-toolbar__pill--ds-thinking">
                    <span class="assistant-toolbar__select-label">思考</span>
                    <el-select
                      v-model="selectedThinkingMode"
                      class="assistant-toolbar__select-box assistant-toolbar__select-box--thinking"
                      size="small"
                      popper-class="assistant-thinking-select-panel"
                      placement="top-start"
                      :teleported="true"
                    >
                      <el-option value="auto" label="自动" />
                      <el-option value="true" label="开启" />
                      <el-option value="false" label="关闭" />
                    </el-select>
                  </div>

                  <div class="assistant-toolbar__pill assistant-toolbar__pill--select assistant-toolbar__pill--execution-mode">
                    <span class="assistant-toolbar__select-label">模式</span>
                    <el-select
                      v-model="selectedExecutionMode"
                      class="assistant-toolbar__select-box assistant-toolbar__select-box--execution"
                      size="small"
                      popper-class="assistant-thinking-select-panel"
                      placement="top-start"
                      :teleported="true"
                    >
                      <el-option value="manual" label="手动确认" />
                      <el-option value="always" label="自动执行" />
                    </el-select>
                  </div>


                  <ContextWindowMeter
                    class="assistant-toolbar__context-meter"
                    :stats="selectedConversationContextStats"
                    :loading="contextStatsLoading"
                    :disabled="contextStatsDisabled"
                  />

                  <label class="f02f0e25 ds-icon-button ds-icon-button--l ds-icon-button--sizing-container" role="button" aria-disabled="false">
                    <div class="ds-icon-button__hover-bg" />
                    <div class="ds-icon">
                      <svg width="16" height="16" viewBox="0 0 16 16" fill="none" xmlns="http://www.w3.org/2000/svg">
                        <path d="M5.5498 9.75V5H6.9502V9.75C6.9502 10.3299 7.4201 10.7998 8 10.7998C8.5799 10.7998 9.0498 10.3299 9.0498 9.75V4.5C9.0498 2.9536 7.7964 1.7002 6.25 1.7002C4.7036 1.7002 3.4502 2.9536 3.4502 4.5V9.75C3.4502 12.2629 5.4871 14.2998 8 14.2998C10.5129 14.2998 12.5498 12.2629 12.5498 9.75V4H13.9502V9.75C13.9502 13.0361 11.2861 15.7002 8 15.7002C4.71391 15.7002 2.0498 13.0361 2.0498 9.75V4.5C2.04981 2.1804 3.9304 0.299806 6.25 0.299805C8.5696 0.299805 10.4502 2.1804 10.4502 4.5V9.75C10.4502 11.1031 9.3531 12.2002 8 12.2002C6.6469 12.2002 5.5498 11.1031 5.5498 9.75Z" fill="currentColor" />
                      </svg>
                    </div>
                    <input
                      type="file"
                      multiple
                      accept=".pdf,.png,.jpg,.jpeg,.webp,.txt,.md,.csv,.json,.doc,.docx,.ppt,.pptx,.xls,.xlsx,.js,.ts,.tsx,.go,.py,.java,.c,.cpp,.h,.html,.css,.yaml,.yml,.log"
                      style="display: none"
                    >
                  </label>

                  <!-- 流式期间显示停止按钮，其余时刻显示发送按钮 -->
                  <button
                    v-if="chatLoading"
                    type="button"
                    class="_7436101 bcc55ca1 _52c986b ds-icon-button ds-icon-button--l ds-icon-button--sizing-container"
                    aria-label="停止生成"
                    @click="stopStreaming()"
                  >
                    <div class="ds-icon-button__hover-bg" />
                    <div class="ds-icon">
                      <svg width="16" height="16" viewBox="0 0 16 16" fill="none" xmlns="http://www.w3.org/2000/svg">
                        <path d="M2 4.88C2 3.68009 2 3.08013 2.30557 2.65954C2.40426 2.52371 2.52371 2.40426 2.65954 2.30557C3.08013 2 3.68009 2 4.88 2H11.12C12.3199 2 12.9199 2 13.3405 2.30557C13.4763 2.40426 13.5957 2.52371 13.6944 2.65954C14 3.08013 14 3.68009 14 4.88V11.12C14 12.3199 14 12.9199 13.6944 13.3405C13.5957 13.4763 13.4763 13.5957 13.3405 13.6944C12.9199 14 12.3199 14 11.12 14H4.88C3.68009 14 3.08013 14 2.65954 13.6944C2.52371 13.5957 2.40426 13.4763 2.30557 13.3405C2 12.9199 2 12.3199 2 11.12V4.88Z" fill="currentColor" />
                      </svg>
                    </div>
                    <div class="ds-focus-ring" style="--ds-focus-ring-offset: -2px;" />
                  </button>
                  <button
                    v-else
                    type="button"
                    class="_7436101 bcc55ca1 ds-icon-button ds-icon-button--l ds-icon-button--sizing-container"
                    :class="{ 'ds-icon-button--disabled': !messageInput.trim() }"
                    :disabled="!messageInput.trim()"
                    :aria-disabled="!messageInput.trim()"
                    @click="sendMessage()"
                  >
                    <div class="ds-icon-button__hover-bg" />
                    <div class="ds-icon">
                      <svg width="16" height="16" viewBox="0 0 16 16" fill="none" xmlns="http://www.w3.org/2000/svg">
                        <path d="M8.3125 0.981587C8.66767 1.0545 8.97902 1.20558 9.2627 1.43374C9.48724 1.61438 9.73029 1.85933 9.97949 2.10854L14.707 6.83608L13.293 8.25014L9 3.95717V15.0431H7V3.95717L2.70703 8.25014L1.29297 6.83608L6.02051 2.10854C6.26971 1.85933 6.51277 1.61438 6.7373 1.43374C6.97662 1.24126 7.28445 1.04542 7.6875 0.981587C7.8973 0.94841 8.1031 0.956564 8.3125 0.981587Z" fill="currentColor" />
                      </svg>
                    </div>
                    <div class="ds-focus-ring" style="--ds-focus-ring-offset: -2px;" />
                  </button>
                </div>
              </div>
            </div>
          </div>
          </div>
        </div>
        <div class="assistant-chat__spacer-bottom" />
      </section>
    </div>
  </aside>

  <!-- 日程排程方案精排弹窗 -->
  <ScheduleFineTuneModal
    :visible="isFineTuneModalVisible"
    :preview-data="activeFineTuneData"
    @close="closeFineTuneModal"
    @saved="handleScheduleSaved"
  />
</template>

<style scoped>
@keyframes assistant-item-pop {
  0% { opacity: 0; transform: scale(0.98) translateY(10px); }
  60% { opacity: 1; transform: scale(1.01) translateY(-1px); }
  100% { opacity: 1; transform: scale(1) translateY(0); }
}

.dashboard-item-pop {
  animation: assistant-item-pop 0.5s cubic-bezier(0.34, 1.56, 0.64, 1) both;
  animation-delay: var(--anim-delay, 0s);
}

.fade-switch-enter-active,
.fade-switch-leave-active {
  transition: opacity 0.3s ease, transform 0.3s ease;
}

.fade-switch-enter-from {
  opacity: 0;
  transform: translateY(10px);
}

.fade-switch-leave-to {
  opacity: 0;
  transform: translateY(-10px);
}

.message-stagger-enter-active,
.message-stagger-leave-active {
  transition: all 0.4s cubic-bezier(0.34, 1.56, 0.64, 1);
}

.message-stagger-enter-from {
  opacity: 0;
  transform: translateY(20px) scale(0.98);
}

.message-stagger-leave-to {
  opacity: 0;
  transform: translateX(30px) scale(0.95);
}

.message-stagger-leave-active {
  position: absolute;
  width: 100%;
}

.inner-fade-enter-active,
.inner-fade-leave-active {
  transition: all 0.5s ease;
}

.inner-fade-enter-from {
  opacity: 0;
  transform: translateY(5px);
}

.inner-fade-leave-to {
  opacity: 0;
}

.assistant-shell {
  height: 100%;
  min-height: 0;
  display: flex;
  flex-direction: column;
  overflow: hidden;
  background: transparent;
  padding: 12px;
  box-sizing: border-box;
  font-family: 'Inter', 'Segoe UI', Roboto, -apple-system, sans-serif;
}

/* --- 全局精致滚动条 --- */
:deep(::-webkit-scrollbar) {
  width: 5px;
  height: 5px;
}

:deep(::-webkit-scrollbar-track) {
  background: transparent;
}

:deep(::-webkit-scrollbar-thumb) {
  background: rgba(15, 23, 42, 0.08);
  border-radius: 10px;
  transition: background 0.3s;
}

:deep(::-webkit-scrollbar-thumb:hover) {
  background: rgba(15, 23, 42, 0.15);
}

.assistant-shell--standalone {
  border-radius: 18px;
  border-color: rgba(15, 23, 42, 0.08);
  box-shadow: 0 10px 28px rgba(15, 23, 42, 0.08);
  background: #ffffff;
}

.assistant-shell--standalone .assistant-header,
.assistant-shell--standalone .assistant-history__toolbar,
.assistant-shell--standalone .assistant-actions,
.assistant-shell--standalone .assistant-composer-ds {
  background: #ffffff;
}

.assistant-shell--standalone .assistant-header {
  padding: 14px 18px 12px;
  border-bottom: 1px solid rgba(15, 23, 42, 0.08);
  background: #fafbfd;
}

.assistant-shell--standalone .assistant-header__eyebrow {
  background: rgba(57, 99, 213, 0.1);
  color: #315ec2;
}

.assistant-shell--standalone .assistant-header strong {
  margin-top: 8px;
  font-size: 18px;
}

.assistant-shell--standalone .assistant-header p {
  color: #7e8a9f;
}

.assistant-header,
.assistant-history__toolbar,
.assistant-actions,
.assistant-composer-ds {
  background: rgba(255, 255, 255, 0.92);
}

.assistant-header {
  padding: 24px 32px;
  border-bottom: 1px solid #f1f5f9;
  background: #ffffff;
}

.assistant-header__eyebrow {
  display: inline-flex;
  padding: 4px 12px;
  border-radius: 6px;
  background: #eff6ff;
  color: #3b82f6;
  font-size: 11px;
  font-weight: 700;
  text-transform: uppercase;
  letter-spacing: 0.05em;
}

.assistant-header strong {
  display: block;
  margin-top: 12px;
  color: #0f172a;
  font-size: 24px;
  font-weight: 800;
  letter-spacing: -0.02em;
}

.assistant-header p {
  margin: 6px 0 0;
  color: #64748b;
  font-size: 13px;
}

.assistant-history__toggle,
.assistant-actions__chip,
.assistant-toolbar__pill,
.chat-message__reasoning-toggle {
  cursor: pointer;
}

.assistant-body {
  --assistant-history-width: 228px;
  flex: 1;
  min-height: 0;
  display: grid;
  grid-template-columns: var(--assistant-history-width) auto minmax(0, 1fr);
  gap: 12px;
  position: relative;
  transition: grid-template-columns 0.35s cubic-bezier(0.4, 0, 0.2, 1);
}

.assistant-body--collapsed {
  grid-template-columns: 0 0 minmax(0, 1fr);
}

.assistant-body--standalone {
  grid-template-columns: var(--assistant-history-width) auto minmax(0, 1fr);
  gap: 12px;
}

.assistant-body--standalone.assistant-body--collapsed {
  grid-template-columns: 0 0 minmax(0, 1fr);
}

.assistant-history {
  min-width: 0;
  min-height: 0;
  display: grid;
  grid-template-rows: auto minmax(0, 1fr);
  background: #ffffff;
  border-radius: 24px;
  border: 1px solid rgba(15, 23, 42, 0.05);
  transition: all 0.35s cubic-bezier(0.4, 0, 0.2, 1);
  position: relative;
  box-shadow: 0 4px 15px rgba(15, 23, 42, 0.02);
}

.assistant-history__toolbar {
  display: flex;
  justify-content: space-between;
  gap: 8px;
  padding: 12px;
  align-items: center;
}

.assistant-history__brand {
  display: inline-flex;
  align-items: center;
  gap: 8px;
  min-width: 0;
}

.assistant-history__brand-icon {
  width: 20px;
  height: 20px;
  display: inline-flex;
  align-items: center;
  justify-content: center;
  color: #355fd5;
}

.assistant-history__brand strong {
  color: #1f2a3d;
  font-size: 13px;
  font-weight: 700;
}

.assistant-history__toggle {
  width: 28px;
  height: 28px;
  border: 1px solid rgba(15, 23, 42, 0.1);
  border-radius: 10px;
  background: #ffffff;
  color: #667085;
  display: inline-flex;
  align-items: center;
  justify-content: center;
  transition: border-color 0.15s ease, background-color 0.15s ease, color 0.15s ease;
}

.assistant-history__toggle:hover {
  border-color: rgba(54, 96, 210, 0.35);
  background: #edf2ff;
  color: #355fd5;
}

.assistant-history__toggle-icon {
  width: 14px;
  height: 14px;
  display: block;
  transition: transform 0.16s ease;
}

.assistant-history__toggle-icon--collapsed {
  transform: rotate(180deg);
}

.assistant-history__content {
  min-height: 0;
  overflow-y: auto;
  overflow-x: hidden;
  overscroll-behavior: contain;
  display: grid;
  align-content: start;
  gap: 12px;
  padding: 12px 10px 14px 12px; /* 增加顶部内边距 */
  scrollbar-gutter: stable;
  transition: opacity 0.3s;
  opacity: 1;
}

.assistant-history__new {
  width: 100%;
  height: 48px;
  padding: 0 16px;
  border-radius: 12px;
  border: 1px solid #e2e8f0;
  background: #ffffff;
  color: #1e293b;
  font-weight: 600;
  font-size: 14px;
  display: flex;
  align-items: center;
  gap: 10px;
  transition: all 0.2s cubic-bezier(0.4, 0, 0.2, 1);
  box-shadow: 0 1px 2px rgba(0, 0, 0, 0.05);
}

.assistant-history__new:hover {
  border-color: #3b82f6;
  background: #eff6ff;
  color: #2563eb;
  /* 移除位移效果，避免溢出 */
}

.assistant-history__new-icon {
  width: 16px;
  height: 16px;
  display: inline-flex;
}

.assistant-history__new-text {
  font-size: 13px;
  font-weight: 600;
  line-height: 1;
}

.assistant-history__group {
  display: grid;
  gap: 6px;
  min-width: 0;
}

.assistant-history__group-title {
  margin: 0;
  padding: 2px 2px 0;
  color: #7a879d;
  font-size: 11px;
  font-weight: 600;
  line-height: 1.2;
}

.assistant-history__item {
  width: 100%;
  max-width: 100%;
  min-width: 0;
  min-height: 38px;
  padding: 8px 10px;
  box-sizing: border-box;
  border: 1px solid transparent;
  border-radius: 10px;
  background: transparent;
  color: #1f2937;
  display: grid;
  grid-template-columns: minmax(0, 1fr) auto;
  align-items: center;
  gap: 8px;
  text-align: left;
  overflow: hidden;
  transition: border-color 0.15s ease, background-color 0.15s ease, color 0.15s ease;
}

.assistant-history__item--reveal {
  position: relative;
}

.assistant-history__item--reveal::after {
  content: '';
  position: absolute;
  inset: 0;
  pointer-events: none;
  background: linear-gradient(
    90deg,
    rgba(66, 112, 234, 0) 0%,
    rgba(66, 112, 234, 0.2) 42%,
    rgba(66, 112, 234, 0) 100%
  );
  transform: translateX(-118%);
  animation: history-item-reveal-sweep 420ms ease-out forwards;
}

.assistant-history__item:hover {
  border-color: rgba(54, 96, 210, 0.18);
  background: rgba(237, 242, 255, 0.72);
}

.assistant-history__item-title {
  min-width: 0;
  max-width: 100%;
  font-size: 13px;
  line-height: 1.3;
  color: inherit;
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}

.assistant-history__item--reveal .assistant-history__item-title {
  animation: history-item-title-fade-in 420ms ease-out;
}

.assistant-history__item-time,
.assistant-history__empty,
.assistant-history__end {
  color: #8792a7;
  font-size: 11px;
  line-height: 1;
  flex: 0 0 auto;
}

.assistant-history__item--active {
  border-color: rgba(54, 96, 210, 0.3);
  background: #eaf0ff;
  color: #234ab3;
}

.assistant-shell--standalone .assistant-history {
  background: linear-gradient(180deg, #f8f9fc 0%, #f4f7fb 100%);
  border-right: 1px solid rgba(15, 23, 42, 0.08);
  overflow: hidden;
}

.assistant-shell--standalone .assistant-history__item--active {
  border-color: rgba(49, 96, 202, 0.3);
  background: #edf2ff;
  box-shadow: 0 4px 10px rgba(36, 67, 127, 0.08);
}

.assistant-shell--standalone .assistant-history__toolbar {
  padding: 10px 9px 8px 10px;
}

.assistant-shell--standalone .assistant-history__content {
  gap: 10px;
  padding: 0 8px 12px 10px;
}

.assistant-shell--standalone .assistant-history__new {
  height: 40px;
}

.assistant-shell--standalone .assistant-history__item {
  min-height: 54px;
  padding: 10px 10px 10px 11px;
}

.assistant-shell--standalone .assistant-history__item-title {
  font-size: 12px;
}

.assistant-history--collapsed {
  border-right: none;
  background: transparent;
  overflow: visible !important; /* 强制允许绝对定位子元素外溢 */
}

.assistant-history--collapsed .assistant-history__content,
.assistant-history--collapsed .assistant-history__brand,
.assistant-history--collapsed .assistant-history__new {
  opacity: 0 !important;
  pointer-events: none !important;
  display: none; /* 彻底移除这些占用空间的东西，防止由于 0 宽度导致的堆叠挤压 */
}

/* 在收起状态下，切换按钮应该绝对定位在内容区左上角 */
.assistant-history--collapsed .assistant-history__toolbar {
  position: absolute;
  top: 10px;
  left: 10px;
  z-index: 2000; /* 调高 z-index 层级 */
  padding: 0;
  display: flex !important;
  visibility: visible !important;
  opacity: 1 !important;
  pointer-events: auto !important;
  width: 32px;
  height: 32px;
  transition: all 0.35s cubic-bezier(0.4, 0, 0.2, 1);
}

.assistant-history--collapsed .assistant-history__toggle {
  width: 32px;
  height: 32px;
  background: #ffffff !important;
  color: #3b82f6 !important; /* 强制使用蓝色，确保可见 */
  border: 1px solid #e2e8f0 !important;
  border-radius: 8px !important;
  box-shadow: 0 4px 12px rgba(15, 23, 42, 0.12) !important;
  display: flex !important;
  align-items: center;
  justify-content: center;
}

.assistant-history__loading {
  display: grid;
  gap: 8px;
}

.assistant-history__loading-item {
  height: 48px;
  border-radius: 12px;
  background: linear-gradient(90deg, #f1f5f9 25%, #e2e8f0 50%, #f1f5f9 75%);
  background-size: 200% 100%;
  animation: history-shimmer 1.5s infinite linear;
}

.assistant-history__empty,
.assistant-history__end {
  margin: 0;
  padding-top: 2px;
  text-align: center;
}

.assistant-splitter {
  display: flex;
  align-items: center;
  justify-content: center;
  cursor: col-resize;
  width: 8px;
  margin: 0 -4px;
  z-index: 20;
}

.assistant-splitter--hidden {
  opacity: 0;
  pointer-events: none;
}

.assistant-body--standalone .assistant-splitter {
  display: flex;
}

.assistant-shell--standalone .assistant-splitter__line {
  background: linear-gradient(180deg, rgba(130, 148, 180, 0.18), rgba(78, 110, 168, 0.32), rgba(130, 148, 180, 0.18));
}

.assistant-splitter__line {
  width: 3px;
  height: 56px;
  border-radius: 999px;
  background: linear-gradient(180deg, rgba(145, 163, 188, 0.22), rgba(88, 124, 177, 0.42), rgba(145, 163, 188, 0.22));
}

.assistant-chat {
  min-width: 0;
  min-height: 0;
  display: flex;
  flex-direction: column;
  position: relative;
  background: #ffffff;
  border-radius: 24px;
  border: 1px solid rgba(15, 23, 42, 0.05);
  box-shadow: 0 4px 15px rgba(15, 23, 42, 0.02);
  overflow: hidden;
}

.assistant-chat__spacer-top {
  flex: 0;
  transition: flex 0.75s cubic-bezier(0.34, 1.56, 0.64, 1);
}

.assistant-chat__spacer-bottom {
  flex: 0;
  transition: flex 0.75s cubic-bezier(0.34, 1.56, 0.64, 1);
}

.assistant-chat--empty .assistant-chat__spacer-top {
  flex: 1.2; /* 稍微偏上一点，视觉重心更舒适 */
}

.assistant-chat--empty .assistant-chat__spacer-bottom {
  flex: 1;
}

.assistant-messages {
  flex: 1;
  min-height: 0;
  overflow-y: auto;
  /* 隐藏滚动条，保持纯净感，仅在非空时显示 */
  scrollbar-width: thin;
}

.assistant-chat--empty .assistant-messages {
  flex: 0;
  overflow: hidden;
  pointer-events: none;
}

.assistant-shell--standalone .assistant-chat {
  background: #ffffff;
}

.assistant-composer-ds--confirm {
  border-top: 1px solid rgba(16, 24, 40, 0.05);
}

/* 深度美化 Select 下拉面板 - 极简扁平化 */
:global(.assistant-thinking-select-panel) {
  border: 1px solid #f1f5f9 !important; /* 极淡的边框代替多层投影 */
  border-radius: 12px !important;
  box-shadow: 0 10px 15px -3px rgba(0, 0, 0, 0.08) !important;
  padding: 0 !important; /* 移除外层内边距 */
  margin-top: 6px !important;
  background: #ffffff !important;
  overflow: hidden !important;
}

:global(.assistant-thinking-select-panel .el-select-dropdown__list) {
  padding: 4px !important; /* 让内部列表直接决定间距 */
}

:global(.assistant-thinking-select-panel .el-select-dropdown__item) {
  border-radius: 8px !important;
  margin: 0 !important;
  font-size: 13px !important;
  color: #475569 !important;
  height: 36px !important;
  line-height: 36px !important;
}

:global(.assistant-thinking-select-panel .el-popper__arrow) {
  display: none !important;
}

.assistant-toolbar__pill--ds-thinking,
.assistant-toolbar__pill--execution-mode {
  height: 32px;
  padding: 0 4px 0 10px;
  background: #f1f5f9;
  border-radius: 9px;
  border: none;
  display: flex;
  align-items: center;
  gap: 2px;
  transition: all 0.2s;
  cursor: pointer;
}

.assistant-toolbar__pill--ds-thinking:hover,
.assistant-toolbar__pill--execution-mode:hover {
  background: #eef2f6;
}

.assistant-toolbar__select-label {
  color: #64748b;
  font-size: 12px;
  font-weight: 700;
}

.assistant-toolbar__select-box--thinking {
  width: 58px;
}

.assistant-toolbar__select-box--execution {
  width: 110px;
}

.assistant-toolbar__select-box :deep(.el-select__wrapper) {
  min-height: 24px !important;
  padding: 0 4px !important;
  background: transparent !important; /* 彻底透明，由外层 pill 提供背景 */
  box-shadow: none !important;
  border: none !important;
  outline: none !important;
}

.assistant-toolbar__select-box :deep(.el-select__selected-item) {
  color: #1e293b;
  font-size: 12px;
  font-weight: 700;
}

.assistant-toolbar__select-box :deep(.el-select__caret) {
  color: #94a3b8;
  font-size: 11px;
}

.assistant-confirm-composer {
  width: 100%;
  padding: 2px 0;
}

.assistant-confirm-card {
  width: 100%;
  border-radius: 16px;
  border: 1px solid #e2e8f0;
  background: #ffffff;
  box-shadow: 0 20px 25px -5px rgba(0, 0, 0, 0.1), 0 10px 10px -5px rgba(0, 0, 0, 0.04);
  padding: 28px;
  display: flex;
  flex-direction: column;
  gap: 20px;
  position: relative;
  overflow: hidden;
  border-top: 4px solid #f59e0b; /* 警告色顶部装饰条 */
  animation: confirm-card-enter 0.3s cubic-bezier(0.34, 1.56, 0.64, 1);
}

.assistant-confirm-card__header {
  display: flex;
  flex-direction: column;
  gap: 4px;
}

.assistant-confirm-card__eyebrow {
  margin: 0;
  color: #f59e0b;
  font-size: 11px;
  font-weight: 700;
  text-transform: uppercase;
  letter-spacing: 0.1em;
}

.assistant-confirm-card__title {
  margin: 0;
  color: #0f172a;
  font-size: 20px;
  font-weight: 700;
  letter-spacing: -0.01em;
}

.assistant-confirm-card__summary {
  margin: 0;
  padding: 16px;
  background: #fffbeb;
  border-radius: 12px;
  border-left: 4px solid #fef3c7;
  color: #92400e;
  font-size: 14px;
  line-height: 1.6;
  white-space: pre-wrap;
}

.assistant-confirm-card__hint {
  margin: 0;
  color: #64748b;
  font-size: 13px;
  line-height: 1.5;
}

.assistant-confirm-card__actions {
  display: grid;
  gap: 16px;
}

.assistant-confirm-card__button {
  height: 44px;
  border-radius: 12px;
  border: 1px solid transparent;
  padding: 0 20px;
  font-size: 14px;
  font-weight: 600;
  cursor: pointer;
  transition: all 0.2s cubic-bezier(0.4, 0, 0.2, 1);
  display: flex;
  align-items: center;
  justify-content: center;
}

.assistant-confirm-card__button--primary {
  background: #f59e0b;
  color: #ffffff;
  box-shadow: 0 4px 6px -1px rgba(245, 158, 11, 0.2);
}

.assistant-confirm-card__button--primary:hover {
  background: #d97706;
  transform: translateY(-1px);
  box-shadow: 0 10px 15px -3px rgba(245, 158, 11, 0.3);
}

.assistant-confirm-card__button--ghost {
  border-color: #e2e8f0;
  background: #ffffff;
  color: #475569;
}

.assistant-confirm-card__button--ghost:hover {
  border-color: #cbd5e1;
  background: #f8fafc;
  color: #1e293b;
}

.assistant-confirm-card__reject-box {
  display: flex;
  flex-direction: column;
  gap: 10px;
  padding-top: 16px;
  border-top: 1px solid #f1f5f9;
}

.assistant-confirm-card__reject-label {
  color: #1e293b;
  font-size: 13px;
  font-weight: 600;
}

.assistant-confirm-card__reject-input {
  width: 100%;
  min-height: 80px;
  border-radius: 12px;
  border: 1px solid #e2e8f0;
  background: #f8fafc;
  padding: 12px;
  font: inherit;
  font-size: 14px;
  color: #0f172a;
  resize: none;
  transition: all 0.2s;
}

.assistant-confirm-card__reject-input:focus {
  background: #ffffff;
  border-color: #f59e0b;
  box-shadow: 0 0 0 4px rgba(245, 158, 11, 0.1);
  outline: none;
}

.assistant-confirm-card__button--plain {
  background: transparent;
  color: #94a3b8;
  height: 32px;
  font-size: 13px;
}

.assistant-confirm-card__button--plain:hover {
  color: #64748b;
  background: #f1f5f9;
}

.assistant-messages {
  min-height: 0;
  overflow-y: auto;
  padding: 24px 28px 18px;
  overscroll-behavior: contain;
  display: grid;
  gap: 20px;
  align-content: start;
  background:
    linear-gradient(180deg, rgba(249, 251, 253, 0.42), rgba(255, 255, 255, 0.9) 28%, rgba(255, 255, 255, 1)),
    radial-gradient(circle at top center, rgba(129, 171, 255, 0.1), transparent 34%);
}

.assistant-shell--standalone .assistant-messages {
  background:
    linear-gradient(180deg, rgba(252, 253, 255, 1), rgba(255, 255, 255, 1)),
    radial-gradient(circle at top center, rgba(126, 150, 199, 0.08), transparent 36%);
}

.assistant-chat__fallback {
  padding: 16px 20px;
  background: #fffbeb;
  border: 1px solid #fef3c7;
  border-radius: 12px;
  color: #92400e;
  font-size: 13px;
}

.chat-message__reasoning {
  border-radius: 14px;
  border: 1px solid #f1f5f9;
  background: #f8fafc;
  padding: 4px 0;
}

.assistant-empty {
  width: 100%;
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  text-align: center;
  padding-bottom: 24px;
}

.assistant-empty__content {
  position: relative;
  z-index: 10;
}

.assistant-empty__halo {
  position: absolute;
  top: 50%;
  left: 50%;
  transform: translate(-50%, -50%);
  width: 400px;
  height: 400px;
  background: radial-gradient(circle, rgba(59, 130, 246, 0.08) 0%, transparent 70%);
  filter: blur(40px);
  pointer-events: none;
}

.assistant-empty strong {
  display: block;
  font-size: 24px;
  font-weight: 850;
  color: #0f172a;
  margin-bottom: 12px;
  letter-spacing: -0.02em;
}

.assistant-empty p {
  color: #64748b;
  font-size: 15px;
  max-width: 320px;
  line-height: 1.6;
}

.chat-message__user-row {
  width: 100%;
  max-width: min(92%, 860px);
  margin: 0 auto;
  display: grid;
  justify-items: end;
  gap: 8px;
}

.chat-message__user-bubble {
  max-width: 85%;
  padding: 12px 18px;
  border-radius: 18px;
  background: #3b82f6; 
  color: #ffffff;
  box-shadow: 0 4px 14px -3px rgba(59, 130, 246, 0.4), 0 2px 6px -2px rgba(59, 130, 246, 0.2);
  border: none;
  font-size: 15px;
  line-height: 1.6;
}

.chat-message__assistant-flow {
  max-width: min(92%, 860px);
  margin: 0 auto;
  display: grid;
  gap: 12px;
}

.chat-message__assistant-content {
  padding-right: 10px;
}

.chat-message__tool-list {
  display: grid;
  gap: 8px;
}

.chat-message__tool-item {
  font-size: 13px;
  border-radius: 10px;
  border: 1px solid #e2e8f0;
  background: #f8fafc;
  transition: all 0.2s cubic-bezier(0.4, 0, 0.2, 1);
  overflow: hidden;
  position: relative;
}

.chat-message__tool-item:hover {
  border-color: #cbd5e1;
  background: #f1f5f9;
  box-shadow: 0 4px 6px -1px rgba(0, 0, 0, 0.05);
}

.chat-message__tool-item--called {
  border-left: 4px solid #3b82f6;
}

.chat-message__tool-item--completed {
  border-left: 4px solid #10b981;
}

.chat-message__tool-item--create {
  border-left: 4px solid #10b981;
}

.chat-message__tool-item--blocked {
  border-left: 4px solid #f43f5e;
}

.chat-message__tool-head {
  width: 100%;
  border: none;
  background: transparent;
  color: #1e293b;
  padding: 8px 12px;
  display: flex;
  align-items: center;
  gap: 10px;
  text-align: left;
  cursor: pointer;
  outline: none;
}

.chat-message__tool-icon {
  width: 22px;
  height: 22px;
  border-radius: 6px;
  display: flex;
  align-items: center;
  justify-content: center;
  color: #64748b;
  background: #ffffff;
  border: 1px solid #e2e8f0;
  flex: 0 0 22px;
}

.chat-message__tool-summary {
  flex: 1;
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  font-size: 13px;
  font-weight: 500;
}

.chat-message__tool-badge {
  font-style: normal;
  font-size: 11px;
  font-weight: 600;
  border-radius: 6px;
  padding: 2px 8px;
  line-height: normal;
  background: #eff6ff;
  color: #3b82f6;
}

.chat-message__tool-chevron {
  width: 20px;
  height: 20px;
  display: flex;
  align-items: center;
  justify-content: center;
  color: #94a3b8;
  transition: transform 0.25s cubic-bezier(0.4, 0, 0.2, 1);
}

.chat-message__tool-chevron--expanded {
  transform: rotate(90deg);
}

.chat-message__tool-detail {
  margin: 0;
  padding: 0 16px 12px 44px;
  white-space: pre-wrap;
  font-size: 13px;
  line-height: 1.6;
  color: #475569;
  animation: detail-slide-down 0.2s ease-out;
}

.chat-message__status-line {
  display: inline-flex;
  align-items: center;
  gap: 8px;
  padding: 6px 14px;
  border-radius: 999px;
  background: #ffffff;
  border: 1px solid rgba(15, 23, 42, 0.06);
  box-shadow: 0 2px 5px rgba(15, 23, 42, 0.03);
  color: #64748b;
  font-size: 12px;
  font-weight: 600;
  margin: 4px 0;
}

.chat-message__status-icon {
  width: 16px;
  height: 16px;
  display: inline-flex;
  align-items: center;
  justify-content: center;
  color: #6b778a;
}

.chat-message__status-text {
  font-size: 12px;
  line-height: 1.4;
}

.chat-message__action-bar {
  display: flex;
  align-items: center;
  gap: 10px;
  flex-wrap: wrap;
}

.chat-message__action-bar--user {
  justify-content: flex-end;
}

.chat-message__icon-button {
  width: 30px;
  height: 30px;
  border: none;
  border-radius: 999px;
  background: transparent;
  color: #7b8798;
  display: inline-flex;
  align-items: center;
  justify-content: center;
  cursor: pointer;
  transition: background-color 0.15s ease, color 0.15s ease;
}

.chat-message__icon-button:hover {
  background: rgba(79, 118, 234, 0.1);
  color: #3f69d3;
}

.chat-message__icon-button:disabled {
  opacity: 0.38;
  cursor: not-allowed;
}

.chat-message__editor {
  width: min(100%, 640px);
  border: 1px solid rgba(77, 107, 254, 0.22);
  border-radius: 22px;
  background: #ffffff;
  box-shadow: 0 10px 22px rgba(15, 23, 42, 0.05);
}

.chat-message__editor-textarea {
  width: 100%;
  min-height: 82px;
  border: none;
  outline: none;
  resize: vertical;
  background: transparent;
  padding: 12px 16px 0;
  font: inherit;
  font-size: 16px;
  line-height: 24px;
  color: #1f2b3f;
  box-sizing: border-box;
}

.chat-message__editor-actions {
  display: flex;
  justify-content: flex-end;
  gap: 12px;
  padding: 6px 10px 10px;
}

.chat-message__editor-button {
  height: 32px;
  padding: 0 14px;
  border-radius: 12px;
  border: 1px solid transparent;
  font-size: 13px;
  line-height: 20px;
  cursor: pointer;
  transition: border-color 0.15s ease, background-color 0.15s ease, color 0.15s ease;
}

.chat-message__editor-button--ghost {
  border-color: rgba(15, 23, 42, 0.12);
  background: #ffffff;
  color: #475569;
}

.chat-message__editor-button--ghost:hover {
  background: #f8fafc;
}

.chat-message__editor-button--primary {
  background: #235ff1;
  color: #ffffff;
}

.chat-message__editor-button--primary:hover {
  background: #1b53d7;
}

.chat-message__reasoning {
  padding: 2px 0 0;
  border: none;
  background: transparent;
}

.chat-message__reasoning-head,
.chat-message__streaming,
.assistant-toolbar,
.assistant-actions {
  display: flex;
  align-items: center;
}

.chat-message__reasoning-head {
  justify-content: space-between;
  gap: 12px;
  margin-bottom: 8px;
}

.chat-message__reasoning-title {
  display: flex;
  align-items: center;
  gap: 8px;
  color: #5a6577;
}

/* --- Tooling & Selector Beautification --- */
:global(.assistant-thinking-select-panel) {
  border: 1px solid #f1f5f9 !important;
  border-radius: 12px !important;
  box-shadow: 0 12px 20px -5px rgba(15, 23, 42, 0.12) !important;
  padding: 4px !important;
  margin-top: 6px !important;
  background: #ffffff !important;
}

:global(.assistant-thinking-select-panel .el-select-dropdown__item) {
  border-radius: 8px !important;
  margin-bottom: 2px !important;
  font-size: 13px !important;
  height: 36px !important;
  line-height: 36px !important;
}

:global(.assistant-thinking-select-panel .el-select-dropdown__item.is-hovering) {
  background: #f8fafc !important;
}

:global(.assistant-thinking-select-panel .el-popper__arrow) {
  display: none !important;
}

.assistant-toolbar__pill--ds-thinking {
  height: 32px;
  padding: 0 4px 0 10px;
  background: #f1f5f9; /* 统一的浅色底 */
  border-radius: 9px;
  display: flex;
  align-items: center;
  gap: 2px;
  cursor: pointer;
  transition: all 0.2s cubic-bezier(0.4, 0, 0.2, 1);
}

.assistant-toolbar__pill--ds-thinking:hover {
  background: #eef2f6;
  filter: brightness(0.98);
}

.assistant-toolbar__select-label {
  color: #64748b;
  font-size: 12px;
  font-weight: 700;
  white-space: nowrap;
}

.assistant-toolbar__select-box--thinking {
  width: 58px;
}

.assistant-toolbar__select-box--execution {
  width: 110px;
}

.assistant-toolbar__select-box :deep(.el-select__wrapper) {
  min-height: 24px !important;
  padding: 0 4px !important;
  background: transparent !important; /* 核心：去掉内部背景，杜绝多层感 */
  box-shadow: none !important;
  border: none !important;
}

.assistant-toolbar__select-box :deep(.el-select__selected-item) {
  color: #1e293b;
  font-size: 12px;
  font-weight: 700;
}

.chat-message__reasoning-status {
  font-size: 13px;
  font-weight: 600;
  line-height: 1.35;
}

.chat-message__reasoning-icon {
  width: 16px;
  height: 16px;
  display: inline-flex;
  color: #4f76ea;
}

.chat-message__reasoning-icon-svg {
  width: 16px;
  height: 16px;
  display: block;
}

.chat-message__reasoning-toggle {
  width: 24px;
  height: 24px;
  border: none;
  background: transparent;
  color: #6f7b8e;
  border-radius: 8px;
  display: inline-flex;
  align-items: center;
  justify-content: center;
  transition: background-color 0.15s ease, color 0.15s ease;
}

.chat-message__reasoning-toggle:hover {
  background: rgba(79, 118, 234, 0.1);
  color: #4f76ea;
}

.chat-message__reasoning-chevron {
  display: inline-flex;
}

.chat-message__reasoning-chevron-icon {
  width: 14px;
  height: 14px;
  display: block;
  transition: transform 0.15s ease;
}

.chat-message__reasoning-chevron-icon--expanded {
  transform: rotate(90deg);
}

.chat-message__reasoning-body {
  margin: 10px 0 10px 7px;
  padding-left: 16px;
  border-left: 2px dashed rgba(59, 130, 246, 0.3); /* 改为虚线，更具“思考中”的科技感 */
  font-style: italic;
  color: #64748b;
}

.chat-message__markdown {
  font-size: 15px;
  line-height: 1.9;
  word-break: break-word;
  color: inherit;
}

.chat-message__markdown--assistant {
  color: #1c2b3f;
}

.chat-message__markdown--reasoning,
.chat-message__streaming,
.chat-message__time,
.chat-message__time--user {
  color: #64758c;
}

.chat-message__markdown--reasoning {
  font-size: 14px;
  line-height: 1.75;
  color: #5b6676;
}

.chat-message__markdown :deep(p) {
  margin: 0;
}

.chat-message__markdown :deep(p + p),
.chat-message__markdown :deep(p + ul),
.chat-message__markdown :deep(p + ol),
.chat-message__markdown :deep(p + blockquote),
.chat-message__markdown :deep(p + pre),
.chat-message__markdown :deep(pre + p),
.chat-message__markdown :deep(ul + p),
.chat-message__markdown :deep(ol + p) {
  margin-top: 12px;
}

.chat-message__markdown :deep(h1),
.chat-message__markdown :deep(h2),
.chat-message__markdown :deep(h3),
.chat-message__markdown :deep(h4),
.chat-message__markdown :deep(h5),
.chat-message__markdown :deep(h6) {
  margin: 0 0 12px;
  line-height: 1.4;
}

.chat-message__markdown :deep(ul),
.chat-message__markdown :deep(ol) {
  margin: 0;
  padding-left: 22px;
}

.chat-message__markdown :deep(blockquote) {
  margin: 0;
  padding-left: 14px;
  border-left: 3px solid rgba(73, 110, 167, 0.18);
}

.chat-message__markdown :deep(a) {
  color: #2667d2;
  text-decoration: none;
}

.chat-message__markdown :deep(code) {
  padding: 2px 6px;
  border-radius: 6px;
  background: rgba(15, 23, 42, 0.06);
  font-family: Consolas, 'Courier New', monospace;
  font-size: 13px;
}

.chat-message__markdown :deep(.md-pre) {
  margin: 0;
  padding: 14px 16px;
  border-radius: 16px;
  background: #f5f7fb;
  overflow-x: auto;
}

.chat-message__markdown :deep(.md-pre code) {
  padding: 0;
  background: transparent;
}

.chat-message__markdown :deep(.md-pre .hljs) {
  display: block;
  padding: 0;
  background: transparent;
}

.chat-message__markdown :deep(.md-table-wrap) {
  margin: 0;
  border-radius: 12px;
  border: 1px solid rgba(15, 23, 42, 0.1);
  overflow-x: auto;
  background: #ffffff;
}

.chat-message__markdown :deep(.md-table) {
  width: 100%;
  min-width: 520px;
  border-collapse: collapse;
  font-size: 13px;
}

.chat-message__markdown :deep(.md-table th),
.chat-message__markdown :deep(.md-table td) {
  padding: 10px 12px;
  border-bottom: 1px solid rgba(15, 23, 42, 0.08);
  text-align: left;
  vertical-align: top;
}

.chat-message__markdown :deep(.md-table th) {
  background: rgba(68, 98, 158, 0.08);
  color: #1f2f47;
  font-weight: 700;
}

.chat-message__markdown :deep(.md-table tr:last-child td) {
  border-bottom: none;
}

.chat-message__streaming {
  justify-content: flex-start;
  gap: 0;
  min-height: 22px;
  font-size: 13px;
}

.chat-message__streaming--plain {
  padding: 2px 10px 2px 0;
}

.chat-message__streaming--reasoning {
  padding: 2px 0;
}

.chat-message__time,
.chat-message__time--user {
  font-size: 11px;
}

.assistant-actions {
  flex-wrap: wrap;
  gap: 8px;
  /* grid item 需要 width:100% 撑满，再由 max-width 限宽 + margin 居中 */
  width: 100%;
  max-width: min(92%, calc(860px + 44px));
  margin: 0 auto;
  padding: 0 22px 12px;
}

.assistant-actions__chip {
  border: 1px solid rgba(16, 24, 40, 0.05);
  background: #f8fafc;
  color: #536378;
  border-radius: 999px;
  padding: 8px 12px;
  font-size: 12px;
}

.assistant-actions__chip:disabled {
  opacity: 0.48;
  cursor: not-allowed;
}

.assistant-history__toggle:focus-visible,
.assistant-history__new:focus-visible,
.assistant-history__item:focus-visible,
.assistant-actions__chip:focus-visible,
.chat-message__icon-button:focus-visible,
.chat-message__editor-button:focus-visible,
.chat-message__reasoning-toggle:focus-visible,
.ds-icon-button:focus-visible {
  outline: 2px solid rgba(37, 99, 235, 0.36);
  outline-offset: 2px;
}

.assistant-composer-ds {
  --dsw-alias-brand-text: #3357c2;
  --dsw-alias-label-primary: #1f2430;
  /* grid item 需要 width:100% 撑满，再由 max-width 限宽 + margin 居中 */
  width: 100%;
  max-width: min(92%, calc(860px + 44px));
  margin: 0 auto;
  padding: 8px 22px 18px;
  border-top: 1px solid rgba(16, 24, 40, 0.05);
}

.aaff8b8f {
  border: 1px solid rgba(15, 23, 42, 0.08);
  border-radius: 20px;
  background: #ffffff;
  box-shadow: 0 10px 25px -5px rgba(15, 23, 42, 0.1), 0 8px 10px -6px rgba(15, 23, 42, 0.1);
  transition: all 0.3s cubic-bezier(0.4, 0, 0.2, 1);
}

.aaff8b8f:focus-within {
  border-color: rgba(59, 130, 246, 0.3);
  box-shadow: 0 20px 25px -5px rgba(15, 23, 42, 0.12), 0 10px 10px -5px rgba(15, 23, 42, 0.04);
  transform: translateY(-2px);
}

._77cefa5,
._9996a53,
._020ab5b {
  width: 100%;
}

._24fad49 {
  position: relative;
  padding: 10px 12px 0;
}

._27c9245 {
  width: 100%;
  min-height: 62px;
  max-height: 180px;
  resize: vertical;
  border: none;
  background: transparent;
  outline: none;
  font-size: 15px;
  line-height: 1.6;
  color: #1f2430;
  font-family: inherit;
}

._27c9245::placeholder {
  color: #9ca3af;
}

.b13855df {
  height: 2px;
}

.ec4f5d61 {
  display: flex;
  align-items: center;
  gap: 8px;
  min-width: 0;
  padding: 8px 10px 10px;
}

.ds-atom-button {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  height: 32px;
  padding: 0 10px;
  border: 1px solid rgba(15, 23, 42, 0.1);
  border-radius: 999px;
  background: #ffffff;
  color: #1f2430;
  font-size: 13px;
  line-height: 1;
  transition: border-color 0.15s ease, background-color 0.15s ease, color 0.15s ease;
}

.ds-atom-button:hover {
  border-color: rgba(15, 23, 42, 0.18);
  background: #f8fafc;
}

.ds-atom-button .ds-atom-button__icon {
  width: 14px;
  height: 14px;
  color: var(--dsw-alias-label-primary);
}

.ds-toggle-button--selected {
  border-color: rgba(57, 86, 178, 0.24);
  background: #eef3ff;
  color: var(--dsw-alias-brand-text);
}

.ds-toggle-button--selected:hover {
  border-color: rgba(57, 86, 178, 0.34);
  background: #e4ecff;
}

.assistant-toolbar__context-meter {
  flex: 1;
  max-width: 160px;
  margin-right: auto;
}

.ds-icon-button {
  position: relative;
  width: 32px;
  height: 32px;
  border: 1px solid rgba(15, 23, 42, 0.1);
  border-radius: 50%;
  display: inline-flex;
  align-items: center;
  justify-content: center;
  background: #ffffff;
  color: #4b5563;
  cursor: pointer;
  transition: border-color 0.15s ease, background-color 0.15s ease, color 0.15s ease;
}

.ds-icon-button .ds-icon {
  width: 16px;
  height: 16px;
  display: inline-flex;
}

._7436101.bcc55ca1 {
  color: #ffffff;
  background: #2f5af3;
  border-color: #2f5af3;
}

._7436101.bcc55ca1:not(.ds-icon-button--disabled):hover {
  background: #244ce0;
  border-color: #244ce0;
}

/* 停止按钮：流式期间替代发送按钮，hover 态加深底色提示可点击 */
._7436101.bcc55ca1._52c986b {
  color: #ffffff;
  background: #dc2626;
  border-color: #dc2626;
}

._7436101.bcc55ca1._52c986b:hover {
  background: #b91c1c;
  border-color: #b91c1c;
}

.ds-icon-button--disabled {
  opacity: 0.45;
  cursor: not-allowed;
  background: #eef2ff;
  color: #6b7280;
  border-color: rgba(15, 23, 42, 0.08);
}

.thinking-indicator {
  display: inline-flex;
  align-items: center;
}

.thinking-indicator__text {
  font-size: 15px;
  font-weight: 600;
  color: #64748b;
  background: linear-gradient(
    90deg, 
    #64748b 0%, 
    #64748b 25%, 
    #e2e8f0 50%, 
    #64748b 75%, 
    #64748b 100%
  );
  background-size: 200% 100%;
  -webkit-background-clip: text;
  -webkit-text-fill-color: transparent;
  background-clip: text;
  animation: thinking-shimmer 2s infinite linear;
}

@keyframes thinking-shimmer {
  from { background-position: 200% 0; }
  to { background-position: 0% 0; }
}

@keyframes confirm-card-enter {
  0% { opacity: 0; transform: translateY(10px) scale(0.985); }
  100% { opacity: 1; transform: translateY(0) scale(1); }
}

@keyframes pulse-dot {
  0% { box-shadow: 0 0 0 0 rgba(90, 152, 255, 0.34); }
  70% { box-shadow: 0 0 0 8px rgba(90, 152, 255, 0); }
  100% { box-shadow: 0 0 0 0 rgba(90, 152, 255, 0); }
}

@keyframes halo-breathe {
  0%, 100% { transform: scale(0.96); opacity: 0.82; }
  50% { transform: scale(1.06); opacity: 1; }
}

@keyframes history-shimmer {
  0% { background-position: 200% 0; }
  100% { background-position: -200% 0; }
}

@keyframes history-item-title-fade-in {
  0% {
    opacity: 0.42;
    transform: translateX(-8px);
  }
  100% {
    opacity: 1;
    transform: translateX(0);
  }
}

@keyframes history-item-reveal-sweep {
  0% {
    transform: translateX(-118%);
  }
  100% {
    transform: translateX(118%);
  }
}

@media (max-width: 960px) {
  .assistant-body,
  .assistant-body--collapsed {
    grid-template-columns: 1fr;
  }

  .assistant-history {
    border-right: none;
    border-bottom: 1px solid rgba(16, 24, 40, 0.05);
  }

  .assistant-history__content {
    max-height: 240px;
  }

  .assistant-splitter {
    display: none;
  }

  .assistant-messages {
    padding: 20px 18px 16px;
  }

  .assistant-actions,
  .assistant-composer-ds {
    padding-left: 18px;
    padding-right: 18px;
  }

  .assistant-confirm-composer {
    padding: 0;
  }

  .assistant-confirm-card {
    border-radius: 18px;
    padding: 18px 16px 14px;
  }

  .assistant-confirm-card__title {
    font-size: 22px;
  }

  .ec4f5d61 {
    flex-wrap: wrap;
  }

  .assistant-toolbar__context-meter {
    width: 188px;
    min-width: 188px;
    flex-basis: 188px;
    margin-right: 0;
    order: 3;
  }
}

@media (max-width: 1280px) {
  .assistant-body--standalone {
    grid-template-columns: var(--assistant-history-width) 8px minmax(0, 1fr);
  }

  .assistant-shell--standalone .assistant-history__content {
    padding-left: 7px;
    padding-right: 7px;
  }
}

@media (max-width: 1120px) {
  .assistant-body--standalone {
    grid-template-columns: var(--assistant-history-width) 8px minmax(0, 1fr);
  }

  .assistant-shell--standalone .assistant-history__toolbar {
    padding-inline: 8px;
  }

  .assistant-shell--standalone .assistant-history__content {
    gap: 8px;
    padding-inline: 6px;
  }

  .assistant-shell--standalone .assistant-history__item {
    min-height: 50px;
    padding: 9px 9px 9px 10px;
  }

  .assistant-shell--standalone .assistant-history__item-title {
    font-size: 11px;
  }
}

@media (max-width: 860px) {
  .assistant-body--standalone,
  .assistant-body--standalone.assistant-body--collapsed {
    grid-template-columns: 1fr;
  }

  .assistant-shell--standalone .assistant-history {
    border-right: none;
    border-bottom: 1px solid rgba(15, 23, 42, 0.08);
  }

  .assistant-shell--standalone .assistant-history__content {
    max-height: 260px;
  }
}
</style>
<style>
/* --- AI 助手确认卡片 & 弹窗高级样式 --- */
:global(.premium-msg-box) {
  --el-messagebox-width: 420px;
  border-radius: 24px !important;
  padding: 24px !important;
  border: 1px solid rgba(15, 23, 42, 0.08) !important;
  box-shadow: 0 25px 50px -12px rgba(15, 23, 42, 0.25) !important;
  backdrop-filter: blur(16px) !important;
  background: rgba(255, 255, 255, 0.9) !important;
}

:global(.premium-msg-box .el-message-box__header) {
  padding-bottom: 8px !important;
}

:global(.premium-msg-box .el-message-box__title) {
  font-size: 18px !important;
  font-weight: 800 !important;
  color: #0f172a !important;
}

:global(.premium-msg-box .el-message-box__message) {
  color: #64748b !important;
  line-height: 1.6 !important;
}

.assistant-confirm-composer {
  padding: 16px;
  background: #ffffff;
}

.assistant-confirm-card {
  background: #f8fafc;
  border: 1px solid rgba(15, 23, 42, 0.08);
  border-radius: 24px;
  padding: 24px;
  box-shadow: 0 10px 15px -3px rgba(15, 23, 42, 0.05);
}

.assistant-confirm-card__eyebrow {
  font-size: 11px;
  font-weight: 700;
  text-transform: uppercase;
  color: #3b82f6;
  margin-bottom: 8px;
  letter-spacing: 0.05em;
}

.assistant-confirm-card__title {
  margin: 0 0 12px;
  font-size: 20px;
  font-weight: 800;
  color: #0f172a;
}

.assistant-confirm-card__summary {
  margin-bottom: 20px;
  font-size: 15px;
  color: #1e293b;
  line-height: 1.5;
}

.assistant-confirm-card__hint {
  font-size: 13px;
  color: #94a3b8;
  margin-bottom: 24px;
  padding: 12px;
  background: rgba(255, 255, 255, 0.5);
  border-radius: 12px;
}

.assistant-confirm-card__actions {
  display: flex;
  flex-direction: column;
  gap: 12px;
}

.assistant-confirm-card__button {
  height: 48px;
  border-radius: 16px;
  font-weight: 700;
  cursor: pointer;
  border: none;
  transition: all 0.2s;
  display: flex;
  align-items: center;
  justify-content: center;
}

.assistant-confirm-card__button--primary {
  background: #3b82f6;
  color: #ffffff;
}

.assistant-confirm-card__button--primary:hover {
  background: #2563eb;
  transform: translateY(-1px);
}

.assistant-confirm-card__button--ghost {
  background: #ffffff;
  color: #475569;
  border: 1px solid #e2e8f0;
}

.assistant-confirm-card__button--plain {
  background: transparent;
  color: #94a3b8;
  font-size: 14px;
}

.assistant-confirm-card__reject-box {
  margin: 8px 0;
  display: flex;
  flex-direction: column;
  gap: 8px;
}

.assistant-confirm-card__reject-label {
  font-size: 13px;
  font-weight: 600;
  color: #64748b;
}

.assistant-confirm-card__reject-input {
  width: 100%;
  padding: 12px;
  border-radius: 12px;
  border: 1px solid #e2e8f0;
  background: #ffffff;
  resize: none;
  font-family: inherit;
  font-size: 14px;
}

.assistant-confirm-card__reject-input:focus {
  outline: none;
  border-color: #3b82f6;
}
</style>
