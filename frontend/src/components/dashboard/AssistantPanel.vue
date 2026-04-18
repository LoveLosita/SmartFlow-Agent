<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, onMounted, reactive, ref, watch } from 'vue'
import { ElMessage } from 'element-plus'

import ContextWindowMeter from '@/components/assistant/ContextWindowMeter.vue'
import TaskClassPlanningPicker from '@/components/assistant/TaskClassPlanningPicker.vue'
import {
  getContextStats,
  getConversationHistory,
  getConversationList,
  getConversationMeta,
  type ConversationHistoryMessage,
} from '@/api/agent'
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
} from '@/types/dashboard'
import { formatConversationTime, formatMessageTime } from '@/utils/date'
import { renderMarkdown } from '@/utils/markdown'

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

interface StreamEventPayload {
  choices?: StreamChoicePayload[]
  delta?: StreamDeltaPayload
  content?: string
  reasoning_content?: string
  finish_reason?: string | null
  error?: StreamErrorPayload
}


interface ConversationGroup {
  key: string
  label: string
  items: ConversationListItem[]
}

// 展示用消息：合并连续 assistant 消息后的视图模型
interface DisplayMessage {
  /** 第一条源消息的 id，用作 Vue key */
  id: string
  role: 'user' | 'assistant'
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

const conversationLoading = ref(false)
const conversationLoadingMore = ref(false)
const chatLoading = ref(false)
const historyExpanded = ref(true)
const selectedConversationId = ref('')

const selectedThinkingMode = ref<ThinkingModeType>('auto')
const messageInput = ref('')
const historyPanelWidth = ref(props.initialHistoryWidth)
const activeStreamingMessageId = ref('')
// 流式请求的 AbortController：发送时创建，流结束或用户点击停止时 abort。
const streamAbortController = ref<AbortController | null>(null)
const editingUserMessageId = ref('')
const editingUserMessageDraft = ref('')
const pendingPlanningTaskClassIds = ref<number[]>([])

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
const conversationContextStatsMap = reactive<Record<string, ConversationContextStats | null>>({})
const conversationContextStatsLoadingMap = reactive<Record<string, boolean>>({})
const conversationContextStatsReadyMap = reactive<Record<string, boolean>>({})

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
const reasoningDisplayNow = ref(Date.now())
const shouldAutoFollowMessages = ref(true)
const messageBottomTolerancePx = 24
const isProgrammaticMessageScroll = ref(false)

const isStandaloneMode = computed(() => props.viewMode === 'standalone')

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
  if (meta?.has_title && meta.title) {
    return meta.title
  }

  const current = selectedConversation.value
  if (current?.has_title && current.title) {
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

function normalizeHistoryMessage(message: ConversationHistoryMessage, index: number): AssistantMessage {
  const id = `${message.id ?? `${message.role}-${index}`}`
  const reasoningText = typeof message.reasoning_content === 'string' ? message.reasoning_content : ''
  const normalized: AssistantMessage = {
    id,
    role: message.role,
    content: message.content,
    createdAt: message.created_at ?? new Date().toISOString(),
    reasoning: reasoningText || undefined,
  }

  // 1. 历史消息优先使用后端持久化的思考时长，避免刷新后重新按“当前时间 - 创建时间”误算。
  // 2. 若后端当前未返回有效时长，则清掉旧缓存，回退为“仅展示已思考文案”。
  // 3. 同时清理 startedAt，防止历史消息误进入前端实时计时分支。
  delete reasoningStartedAtMap[id]
  if (typeof message.reasoning_duration_seconds === 'number' && message.reasoning_duration_seconds > 0) {
    reasoningDurationMap[id] = Math.max(1, Math.round(message.reasoning_duration_seconds))
  } else {
    delete reasoningDurationMap[id]
  }

  thinkingMessageMap[id] = false
  reasoningCollapsedMap[id] = Boolean(reasoningText.trim())
  return normalized
}

function isSameLogicalMessage(left: AssistantMessage, right: AssistantMessage) {
  return (
    left.role === right.role &&
    left.content === right.content &&
    (left.reasoning || '') === (right.reasoning || '')
  )
}

// mergeServerHistoryWithLocalState 将服务端历史与本地乐观消息合并为最终消息流。
//
// 核心策略：保留本地消息的原始顺序，用服务端数据"就地替换"匹配到的本地消息。
//
// 为什么不按时间戳排序？
// 1. 聊天历史通过 Kafka 异步持久化，数据库 created_at 是消费者落库时刻，
//    而非消息产生时刻。Kafka 消费顺序不保证与发布顺序一致，
//    导致 assistant 消息可能比 user 消息先落库，created_at 反而更早。
// 2. 本地消息按"用户发送 → 占位 → 流式填充"的顺序 append，天然是正确时序，
//    任何基于时间戳的排序都会被异步落库的时钟偏差破坏。
// 3. 因此：本地顺序权威，服务端数据用于刷新字段（如 reasoning_duration_seconds），
//    新增的服务端消息（其他端产生）追加到尾部。
function mergeServerHistoryWithLocalState(
  conversationId: string,
  history: ConversationHistoryMessage[],
) {
  const existingBucket = conversationMessagesMap[conversationId] ?? []
  const normalizedHistory = history.map(normalizeHistoryMessage)

  // 1. 构建服务端消息的快速查找索引：按 ID 和按角色+内容两种方式。
  const serverById = new Map(normalizedHistory.map((m) => [m.id, m]))
  const usedServerIds = new Set<string>()

  // 2. 按本地消息的原始顺序逐一处理：
  //    - ID 精确命中 → 用服务端数据替换，保持当前位置；
  //    - 临时 ID 按语义匹配 → 同样替换，保持当前位置；
  //    - 无法匹配 → 保留为乐观消息，保持当前位置。
  const result: AssistantMessage[] = []
  for (const localMsg of existingBucket) {
    // 2.1 先按 ID 精确匹配（非临时 ID 的消息，如历史加载过的服务端消息）。
    const exactMatch = serverById.get(localMsg.id)
    if (exactMatch && !usedServerIds.has(exactMatch.id)) {
      result.push(exactMatch)
      usedServerIds.add(exactMatch.id)
      continue
    }

    // 2.2 临时 ID（如 user-1700000000000-abc）走语义匹配：
    //     同一角色 + 同一内容的消息视为同一条逻辑消息。
    if (isLocalEphemeralMessageId(localMsg.id)) {
      const logicalMatch = normalizedHistory.find(
        (sm) => !usedServerIds.has(sm.id) && isSameLogicalMessage(sm, localMsg),
      )
      if (logicalMatch) {
        result.push(logicalMatch)
        usedServerIds.add(logicalMatch.id)
        continue
      }
    }

    // 2.3 无法匹配服务端消息时保留本地乐观消息（流式中的占位 / 网络延迟未落库）。
    result.push(localMsg)
  }

  // 3. 本地不存在的服务端消息（如其他设备发送的）追加到尾部，按服务端返回顺序排列。
  for (const serverMsg of normalizedHistory) {
    if (!usedServerIds.has(serverMsg.id)) {
      result.push(serverMsg)
    }
  }

  return result
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

function markReasoningFinished(message: AssistantMessage) {
  const startedAt = reasoningStartedAtMap[message.id]
  if (startedAt && !reasoningDurationMap[message.id]) {
    reasoningDurationMap[message.id] = Math.max(1, Math.round((Date.now() - startedAt) / 1000))
  }

  thinkingMessageMap[message.id] = false
}

function getReasoningDurationSeconds(message: AssistantMessage) {
  const fixedDuration = reasoningDurationMap[message.id]
  if (fixedDuration) {
    return fixedDuration
  }

  const startedAt = reasoningStartedAtMap[message.id]
  if (!startedAt) {
    return 0
  }

  return Math.max(1, Math.round((reasoningDisplayNow.value - startedAt) / 1000))
}

function getReasoningStatusLabel(message: AssistantMessage) {
  const durationSeconds = getReasoningDurationSeconds(message)
  if (durationSeconds > 0) {
    return `已思考（用时 ${durationSeconds} 秒）`
  }

  return isStreamingMessage(message) && isThinkingMessage(message) ? '思考中' : '已思考'
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

function shouldShowDisplayReasoningBox(dm: DisplayMessage): boolean {
  if (dm.role !== 'assistant') return false
  return dm.sources.some(m =>
    Boolean(m.reasoning?.trim()) ||
    (m.id === activeStreamingMessageId.value && thinkingMessageMap[m.id] === true),
  )
}

function shouldShowDisplayAnsweringIndicator(dm: DisplayMessage): boolean {
  if (dm.content) return false
  return isDisplayStreaming(dm) && dm.sources.every(m => thinkingMessageMap[m.id] !== true)
}

function isDisplayReasoningCollapsed(dm: DisplayMessage): boolean {
  return dm.sources.every(m => reasoningCollapsedMap[m.id] === true)
}

function toggleDisplayReasoningCollapse(dm: DisplayMessage): void {
  const newCollapsed = !isDisplayReasoningCollapsed(dm)
  dm.sources.forEach(m => { reasoningCollapsedMap[m.id] = newCollapsed })
}

function getDisplayReasoningStatusLabel(dm: DisplayMessage): string {
  const totalSeconds = dm.sources.reduce(
    (sum, m) => sum + (reasoningDurationMap[m.id] ?? 0), 0,
  )
  if (totalSeconds > 0) return `已思考（用时 ${totalSeconds} 秒）`
  const hasActiveThinking = dm.sources.some(
    m => m.id === activeStreamingMessageId.value && thinkingMessageMap[m.id] === true,
  )
  return hasActiveThinking ? '思考中' : '已思考'
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
  if (!selectedConversationId.value && conversationList.value.length > 0) {
    await selectConversation(conversationList.value[0].conversation_id)
  }
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
    const result = await getConversationList({
      page: conversationPage.value,
      pageSize: conversationPageSize,
      status: 'active',
    })

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

async function loadConversationMessages(conversationId: string, forceReload = false) {
  if (!conversationId) {
    return
  }

  if (!forceReload && conversationMessagesMap[conversationId] && unavailableHistoryMap[conversationId] !== true) {
    return
  }

  try {
    const history = await getConversationHistory(conversationId)
    conversationMessagesMap[conversationId] = mergeServerHistoryWithLocalState(conversationId, history)
    unavailableHistoryMap[conversationId] = false
  } catch {
    unavailableHistoryMap[conversationId] = true
    ensureConversationBucket(conversationId)
  }
}

async function ensureConversationMeta(conversationId: string) {
  if (!conversationId || isDraftConversationId(conversationId) || conversationMetaMap[conversationId]) {
    return
  }

  try {
    const meta = await getConversationMeta(conversationId)
    upsertConversationMeta(meta)
  } catch {
    // 1. 标题和条数属于增强信息，不应阻塞聊天主链路。
    // 2. 即使元信息失败，列表里的回退标题仍可保证界面可用。
    // 3. 后续再次刷新列表或重新进入会话时，还会有机会补齐这些字段。
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
  selectedConversationId.value = ''
  messageInput.value = ''
  activeStreamingMessageId.value = ''
  shouldAutoFollowMessages.value = true
}

function isManualThinkingEnabled(mode: ThinkingModeType) {
  return mode === 'true'
}

function buildChatRequestExtra(
  planningTaskClassIds: number[] = [],
): ChatRequestExtra | undefined {
  // retry 机制已整体下线，这里只负责把智能编排所需的 task_class_ids 透传给后端。
  if (planningTaskClassIds.length <= 0) {
    return undefined
  }

  return {
    task_class_ids: [...planningTaskClassIds],
  }
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
      markReasoningFinished(assistantMessage)
    }
    activeStreamingMessageId.value = ''
    reasoningCollapsedMap[assistantMessage.id] = true
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

  const choice = parsed.choices?.[0]
  const delta = choice?.delta ?? parsed.delta ?? parsed
  const finishReason = choice?.finish_reason ?? parsed.finish_reason ?? null

  if (
    typeof delta?.reasoning_content === 'string' &&
    delta.reasoning_content
  ) {
    // 正文回流后仍允许追加 reasoning（工具调用摘要、阶段状态等），
    // 但不再切换面板状态，避免 UI 闪烁。
    if (!assistantMessage.content.trim()) {
      markReasoningStart(assistantMessage)
      thinkingMessageMap[assistantMessage.id] = true
    }
    assistantMessage.reasoning = `${assistantMessage.reasoning || ''}${delta.reasoning_content}`
  }

  if (typeof delta?.content === 'string' && delta.content) {
    if (isThinkingMessage(assistantMessage)) {
      // 1. 一旦正文开始回流，立刻结束“思考中”阶段，避免两个等待动画同时出现。
      // 2. 这样视觉上始终保持“先思考，再输出正文”的单阶段感知。
      // 3. 若后端偶发交错发送 reasoning/content，也以前端阶段机兜底，优先保证阅读一致性。
      markReasoningFinished(assistantMessage)
    }
    assistantMessage.content += delta.content
  }

  if (finishReason) {
    if (isThinkingMessage(assistantMessage)) {
      markReasoningFinished(assistantMessage)
    }
    activeStreamingMessageId.value = ''
    reasoningCollapsedMap[assistantMessage.id] = true
  }

  scheduleScrollMessagesToBottom(false)
}

async function streamAssistantReply(
  draftConversationId: string,
  text: string,
  assistantMessage: AssistantMessage,
  createdAt: string,
  refreshPreview: boolean,
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
    if (refreshPreview) {
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

  if (!assistantMessage.content.trim()) {
    assistantMessage.content = assistantMessage.reasoning?.trim()
      ? '已完成深度思考，但当前响应未返回正文内容。'
      : '暂未收到回复正文，请稍后重试。'
  }

  if (refreshPreview) {
    await loadConversationListData(true)
    await ensureConversationMeta(actualConversationId)
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
async function sendMessage(preset?: string) {
  const text = (preset ?? messageInput.value).trim()
  if (!text || chatLoading.value) {
    return
  }

  chatLoading.value = true

  const planningTaskClassIdsForRequest = [...pendingPlanningTaskClassIds.value]
  // 智能编排不再强制新开对话：直接沿用当前会话，在原地发送编排请求。
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

  // 1. 创建 AbortController：用户点击停止按钮时可通过 controller.abort() 中断 fetch 请求。
  const controller = new AbortController()
  streamAbortController.value = controller

  try {
    const actualConversationId = await streamAssistantReply(
      draftConversationId,
      text,
      assistantMessage,
      now,
      true,
      buildChatRequestExtra(planningTaskClassIdsForRequest),
      controller.signal,
    )
    if (planningTaskClassIdsForRequest.length > 0) {
      pendingPlanningTaskClassIds.value = []
    }
    // 流式成功后不重新加载历史：流式数据就是当前会话的权威来源，
    // 过早 reload 会因 persistVisibleMessage 尚未落库导致 merge 产生重复/丢失。
    // 历史数据在下次切换会话或刷新页面时自然加载。
    await Promise.allSettled([
      loadConversationContextStats(actualConversationId, true),
    ])
  } catch (error) {
    // 用户主动中断：不弹出错误提示，只给占位消息补一段中断文案。
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
  releaseHistoryResizeListeners()
  window.removeEventListener('resize', syncHistoryPanelWidthForViewport)
})
</script>

<template>
  <aside class="assistant-shell glass-panel" :class="{ 'assistant-shell--standalone': isStandaloneMode }">
    <header class="assistant-header">
      <div class="assistant-header__text">
        <span class="assistant-header__eyebrow">AI 对话</span>
        <strong>{{ selectedConversationTitle }}</strong>
        <p>{{ selectedConversationSubtitle }}</p>
      </div>
    </header>

    <div
      ref="assistantBodyRef"
      class="assistant-body"
      :class="{
        'assistant-body--collapsed': !historyExpanded,
        'assistant-body--standalone': isStandaloneMode,
      }"
      :style="assistantBodyStyle"
      >
        <aside class="assistant-history" :class="{ 'assistant-history--collapsed': !historyExpanded }">
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
                  :class="{ 'assistant-history__item--active': item.conversation_id === selectedConversationId }"
                  @click="selectConversation(item.conversation_id)"
                >
                  <span class="assistant-history__item-title">
                    {{ item.has_title && item.title ? item.title : '未命名会话' }}
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
        class="assistant-splitter"
        :class="{ 'assistant-splitter--hidden': !historyExpanded }"
        role="separator"
        aria-label="调整会话列表宽度"
        @pointerdown.prevent="startResizeHistoryPanel"
      >
        <span class="assistant-splitter__line" />
      </div>

      <section class="assistant-chat">
        <div
          ref="messageViewportRef"
          class="assistant-messages"
          @scroll.passive="handleMessageViewportScroll"
          @wheel.passive="handleMessageViewportWheel"
        >
          <div v-if="shouldShowHistoryFallback" class="assistant-chat__fallback">
            当前会话的历史消息暂时不可读，但你仍然可以继续追问；后续刷新后会自动恢复。
          </div>

          <div v-if="!selectedMessages.length && !chatLoading" class="assistant-empty">
            <div class="assistant-empty__halo" />
            <strong>从这里开始和 AI 协作</strong>
            <p>右侧采用更接近 DeepSeek 的阅读式布局，只保留用户气泡，AI 回复直接按正文流展示。</p>
          </div>

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
              <div v-if="shouldShowDisplayReasoningBox(dm)" class="chat-message__reasoning">
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
                    <span class="chat-message__reasoning-status">{{ getDisplayReasoningStatusLabel(dm) }}</span>
                  </div>
                  <button
                    type="button"
                    class="chat-message__reasoning-toggle"
                    :aria-label="isDisplayReasoningCollapsed(dm) ? '展开深度思考' : '折叠深度思考'"
                    @click="toggleDisplayReasoningCollapse(dm)"
                  >
                    <span class="chat-message__reasoning-chevron">
                      <svg
                        class="chat-message__reasoning-chevron-icon"
                        :class="{ 'chat-message__reasoning-chevron-icon--expanded': !isDisplayReasoningCollapsed(dm) }"
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

                <div v-if="!isDisplayReasoningCollapsed(dm)" class="chat-message__reasoning-body">
                  <div
                    v-if="dm.reasoning"
                    class="chat-message__markdown chat-message__markdown--reasoning"
                    v-html="renderMessageMarkdown(dm.reasoning)"
                  />
                  <div v-else class="chat-message__streaming chat-message__streaming--reasoning">
                    <div class="typing-indicator">
                      <span />
                      <span />
                      <span />
                    </div>
                  </div>
                </div>
              </div>

              <div v-if="dm.content" class="chat-message__assistant-content">
                <div class="chat-message__markdown chat-message__markdown--assistant" v-html="renderMessageMarkdown(dm.content)" />
              </div>
              <div v-else-if="shouldShowDisplayAnsweringIndicator(dm)" class="chat-message__streaming chat-message__streaming--plain">
                <div class="typing-indicator">
                  <span />
                  <span />
                  <span />
                </div>
              </div>

              <div v-if="dm.content" class="chat-message__action-bar">
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
        </div>

        <div class="assistant-actions">
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

        <div class="_9a2f8e4 assistant-composer-ds">
          <div class="aaff8b8f">
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
      </section>
    </div>
  </aside>
</template>

<style scoped>
.assistant-shell {
  height: 100%;
  min-height: 0;
  display: grid;
  grid-template-rows: auto minmax(0, 1fr);
  overflow: hidden;
  border-radius: 30px;
  border: 1px solid rgba(16, 24, 40, 0.08);
  background:
    linear-gradient(180deg, rgba(255, 255, 255, 0.96), rgba(247, 249, 252, 0.98)),
    radial-gradient(circle at top right, rgba(127, 169, 255, 0.16), transparent 34%);
  font-family: 'PingFang SC', 'Hiragino Sans GB', 'Microsoft YaHei UI', 'Segoe UI Variable Text', sans-serif;
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
  padding: 18px 20px 16px;
  border-bottom: 1px solid rgba(16, 24, 40, 0.06);
}

.assistant-header__eyebrow {
  display: inline-flex;
  padding: 5px 10px;
  border-radius: 999px;
  background: rgba(39, 110, 241, 0.08);
  color: #2263cb;
  font-size: 11px;
  font-weight: 700;
}

.assistant-header strong {
  display: block;
  margin-top: 10px;
  color: #142133;
  font-size: 20px;
}

.assistant-header p {
  margin: 6px 0 0;
  color: #738197;
  font-size: 12px;
}

.assistant-history__toggle,
.assistant-actions__chip,
.assistant-toolbar__pill,
.chat-message__reasoning-toggle {
  cursor: pointer;
}

.assistant-body {
  --assistant-history-width: 228px;
  min-height: 0;
  display: grid;
  grid-template-columns: var(--assistant-history-width) 8px minmax(0, 1fr);
}

.assistant-body--collapsed {
  grid-template-columns: 68px 0 minmax(0, 1fr);
}

.assistant-body--standalone {
  grid-template-columns: var(--assistant-history-width) 8px minmax(0, 1fr);
}

.assistant-body--standalone.assistant-body--collapsed {
  grid-template-columns: 68px 0 minmax(0, 1fr);
}

.assistant-history {
  min-width: 0;
  min-height: 0;
  display: grid;
  grid-template-rows: auto minmax(0, 1fr);
  border-right: 1px solid rgba(16, 24, 40, 0.05);
  background: linear-gradient(180deg, #f8f9fc 0%, #f5f7fb 100%);
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
  padding: 0 10px 14px 12px;
  scrollbar-gutter: stable;
}

.assistant-history__new {
  width: 100%;
  max-width: 100%;
  min-width: 0;
  height: 42px;
  box-sizing: border-box;
  border: 1px solid rgba(15, 23, 42, 0.1);
  border-radius: 12px;
  background: #ffffff;
  display: inline-flex;
  align-items: center;
  justify-content: center;
  gap: 8px;
  color: #344054;
  transition: border-color 0.15s ease, background-color 0.15s ease, color 0.15s ease;
}

.assistant-history__new:hover {
  border-color: rgba(54, 96, 210, 0.35);
  background: #edf2ff;
  color: #355fd5;
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

.assistant-history--collapsed .assistant-history__toolbar {
  padding-inline: 8px;
  justify-content: center;
}

.assistant-history--collapsed .assistant-history__brand,
.assistant-history--collapsed .assistant-history__new-text,
.assistant-history--collapsed .assistant-history__group-title,
.assistant-history--collapsed .assistant-history__item-time {
  display: none;
}

.assistant-history--collapsed .assistant-history__content {
  padding-inline: 8px;
}

.assistant-history--collapsed .assistant-history__new {
  width: 42px;
  justify-self: center;
  padding: 0;
}

.assistant-history--collapsed .assistant-history__item {
  width: 42px;
  min-height: 42px;
  justify-self: center;
  justify-content: center;
  padding: 0;
}

.assistant-history--collapsed .assistant-history__item-title {
  display: none;
}

.assistant-history--collapsed .assistant-history__item::before {
  content: '';
  width: 8px;
  height: 8px;
  border-radius: 999px;
  background: currentColor;
  opacity: 0.42;
}

.assistant-history--collapsed .assistant-history__item--active::before {
  opacity: 0.72;
}

.assistant-history__loading {
  display: grid;
  gap: 8px;
}

.assistant-history__loading-item {
  height: 38px;
  border-radius: 10px;
  background: linear-gradient(90deg, rgba(231, 236, 244, 0.85), rgba(246, 249, 252, 1), rgba(231, 236, 244, 0.85));
  background-size: 200% 100%;
  animation: history-shimmer 1.3s linear infinite;
}

.assistant-history__loading--more .assistant-history__loading-item {
  height: 54px;
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
  display: grid;
  grid-template-rows: minmax(0, 1fr) auto auto auto;
}

.assistant-shell--standalone .assistant-chat {
  background: #ffffff;
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

.assistant-chat__fallback,
.chat-message__reasoning {
  border-radius: 16px;
  border: 1px solid rgba(36, 102, 220, 0.1);
  background: #f8fbff;
}

.assistant-chat__fallback {
  padding: 14px 16px;
  color: #617189;
  font-size: 12px;
}

.assistant-empty {
  min-height: 260px;
  display: grid;
  place-items: center;
  align-content: center;
  justify-items: center;
  text-align: center;
  gap: 10px;
  color: #68778e;
}

.assistant-empty strong {
  color: #162334;
}

.assistant-empty p {
  margin: 0;
  max-width: 420px;
  line-height: 1.75;
}

.assistant-empty__halo {
  width: 74px;
  height: 74px;
  border-radius: 26px;
  background: radial-gradient(circle at center, rgba(37, 99, 235, 0.18), rgba(37, 99, 235, 0.02));
  animation: halo-breathe 2.4s ease-in-out infinite;
}

.chat-message__user-row {
  display: grid;
  justify-items: end;
  gap: 8px;
}

.chat-message__user-bubble {
  max-width: min(90%, 760px);
  padding: 14px 16px;
  border-radius: 20px;
  background: linear-gradient(180deg, #dff0ff, #d7ebff);
  color: #173252;
  border: 1px solid rgba(64, 138, 240, 0.18);
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
  margin-left: 7px;
  padding-left: 14px;
  border-left: 2px solid rgba(120, 134, 156, 0.24);
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
  border: 1px solid rgba(15, 23, 42, 0.1);
  border-radius: 20px;
  background: #ffffff;
  box-shadow: 0 8px 20px rgba(15, 23, 42, 0.05);
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

._6dbc175 {
  font-weight: 600;
}

.assistant-toolbar__pill--ds-thinking {
  height: 32px;
  padding: 0 8px 0 10px;
  border: 1px solid rgba(15, 23, 42, 0.1);
  border-radius: 999px;
  background: #ffffff;
  display: inline-flex;
  align-items: center;
  gap: 8px;
  flex: 0 0 auto;
}

.assistant-toolbar__pill--ds-thinking {
  min-width: 138px;
}

.assistant-toolbar__context-meter {
  width: 188px;
  min-width: 188px;
  flex: 0 0 188px;
  margin-right: auto;
}

.assistant-toolbar__select-label {
  color: #4b5563;
  font-weight: 600;
  font-size: 13px;
  line-height: 1;
  white-space: nowrap;
  writing-mode: horizontal-tb;
  text-orientation: mixed;
  flex: 0 0 auto;
}

.assistant-toolbar__select-box {
  min-width: 96px;
  flex: 0 0 96px;
}

.assistant-toolbar__select-box--thinking {
  min-width: 86px;
  flex: 0 0 86px;
}

.assistant-toolbar__select-box :deep(.el-select__wrapper) {
  min-height: 28px;
  padding: 0 6px 0 8px;
  border-radius: 9px;
  border: 1px solid transparent;
  box-shadow: none;
  background: rgba(248, 250, 252, 0.9);
  transition: border-color 0.15s ease, background-color 0.15s ease;
}

.assistant-toolbar__select-box:hover :deep(.el-select__wrapper) {
  border-color: rgba(77, 107, 254, 0.24);
  background: #ffffff;
}

.assistant-toolbar__select-box :deep(.el-select__selected-item) {
  color: #334155;
  font-size: 13px;
  font-weight: 600;
}

.assistant-toolbar__select-box :deep(.el-select__caret) {
  color: #64748b;
  font-size: 13px;
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

.typing-indicator {
  display: flex;
  align-items: center;
  gap: 6px;
}

.typing-indicator span {
  width: 8px;
  height: 8px;
  border-radius: 999px;
  background: #86a9dd;
  animation: typing-bounce 1.2s ease-in-out infinite;
}

.typing-indicator span:nth-child(2) {
  animation-delay: 0.12s;
}

.typing-indicator span:nth-child(3) {
  animation-delay: 0.24s;
}

@keyframes typing-bounce {
  0%, 80%, 100% { transform: translateY(0); opacity: 0.5; }
  40% { transform: translateY(-4px); opacity: 1; }
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

</style>
