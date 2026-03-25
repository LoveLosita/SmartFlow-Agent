<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, reactive, ref, watch } from 'vue'
import { ElMessage } from 'element-plus'

import {
  getConversationHistory,
  getConversationList,
  getConversationMeta,
  type ConversationHistoryMessage,
} from '@/api/agent'
import { refreshToken } from '@/api/auth'
import { useAuthStore } from '@/stores/auth'
import type {
  AssistantMessage,
  ChatStreamRequest,
  ConversationListItem,
  ConversationMeta,
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

type ModelType = 'worker' | 'strategist'

interface ConversationGroup {
  key: string
  label: string
  items: ConversationListItem[]
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

const conversationLoading = ref(false)
const conversationLoadingMore = ref(false)
const chatLoading = ref(false)
const historyExpanded = ref(true)
const selectedConversationId = ref('')
const selectedModel = ref<ModelType>('worker')
const thinkingEnabled = ref(false)
const messageInput = ref('')
const historyPanelWidth = ref(props.initialHistoryWidth)
const activeStreamingMessageId = ref('')

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

const quickActions = [
  '帮我梳理今天最重要的三件事',
  '把当前任务拆成可执行步骤',
  '总结这段对话的关键结论',
  '给我一个更稳妥的推进方案',
]

const MODEL_PREFERENCE_STORAGE_KEY = 'smartflow.assistant.model.byConversation.v1'

let messageScrollRaf = 0
let reasoningTicker = 0
const reasoningDisplayNow = ref(Date.now())
const shouldAutoFollowMessages = ref(true)
const messageBottomTolerancePx = 6

const isStandaloneMode = computed(() => props.viewMode === 'standalone')

const assistantBodyStyle = computed(() => {
  if (isStandaloneMode.value) {
    return {}
  }

  return {
    '--assistant-history-width': `${historyExpanded.value ? historyPanelWidth.value : 68}px`,
  }
})

const selectedConversation = computed(() =>
  conversationList.value.find((item) => item.conversation_id === selectedConversationId.value),
)

const selectedMessages = computed(() => {
  if (!selectedConversationId.value) {
    return []
  }
  return conversationMessagesMap[selectedConversationId.value] ?? []
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
  const messageCount = meta?.message_count ?? current?.message_count ?? selectedMessages.value.length
  const lastMessageAt = meta?.last_message_at ?? current?.last_message_at
  return `消息 ${messageCount} 条 · 最近更新 ${formatConversationTime(lastMessageAt)}`
})

const shouldShowHistoryFallback = computed(() => {
  if (!selectedConversationId.value) {
    return false
  }

  return (
    unavailableHistoryMap[selectedConversationId.value] === true &&
    selectedMessages.value.length === 0 &&
    (selectedConversation.value?.message_count ?? 0) > 0
  )
})

function isModelType(value: unknown): value is ModelType {
  return value === 'worker' || value === 'strategist'
}

function loadModelPreferenceMap() {
  if (typeof window === 'undefined') {
    return {} as Record<string, ModelType>
  }

  try {
    const raw = window.localStorage.getItem(MODEL_PREFERENCE_STORAGE_KEY)
    if (!raw) {
      return {} as Record<string, ModelType>
    }

    const parsed = JSON.parse(raw) as unknown
    const normalized: Record<string, ModelType> = {}
    const entries = typeof parsed === 'object' && parsed ? Object.entries(parsed) : []

    // 1. 只接收结构合法且值在白名单内的记录，避免脏数据把模型值污染为非法字符串。
    // 2. 键为空字符串的记录直接丢弃，防止“新建会话未落库”场景写入无效索引。
    // 3. 解析失败时回退为空对象，不阻塞聊天主流程。
    for (const [conversationId, model] of entries) {
      if (!conversationId || !isModelType(model)) {
        continue
      }
      normalized[conversationId] = model
    }

    return normalized
  } catch {
    return {} as Record<string, ModelType>
  }
}

const modelPreferenceMap = ref<Record<string, ModelType>>(loadModelPreferenceMap())

function persistModelPreferenceMap() {
  if (typeof window === 'undefined') {
    return
  }

  try {
    window.localStorage.setItem(MODEL_PREFERENCE_STORAGE_KEY, JSON.stringify(modelPreferenceMap.value))
  } catch {
    // 1. 本地存储失败只影响“记忆体验”，不影响消息收发主链路。
    // 2. 这里静默处理，避免用户每次切模型都被错误提示打断。
    // 3. 若用户清理缓存或隐私模式限制写入，后续会自动退化为会话内临时选择。
  }
}

function savePreferredModel(conversationId: string, model: ModelType) {
  if (!conversationId || modelPreferenceMap.value[conversationId] === model) {
    return
  }

  modelPreferenceMap.value = {
    ...modelPreferenceMap.value,
    [conversationId]: model,
  }
  persistModelPreferenceMap()
}

function resolvePreferredModel(conversationId: string) {
  if (!conversationId) {
    return null
  }

  return modelPreferenceMap.value[conversationId] ?? null
}

function applyPreferredModelForConversation(conversationId: string) {
  const preferredModel = resolvePreferredModel(conversationId)
  if (!preferredModel || preferredModel === selectedModel.value) {
    return
  }

  selectedModel.value = preferredModel
}

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

  if (modelPreferenceMap.value[fromConversationId]) {
    const migratedModelMap = { ...modelPreferenceMap.value }
    if (!migratedModelMap[toConversationId]) {
      migratedModelMap[toConversationId] = migratedModelMap[fromConversationId]!
    }
    delete migratedModelMap[fromConversationId]
    modelPreferenceMap.value = migratedModelMap
    persistModelPreferenceMap()
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
  const normalized: AssistantMessage = {
    id,
    role: message.role,
    content: message.content,
    createdAt: message.created_at ?? new Date().toISOString(),
    reasoning: message.reasoning_content,
  }

  thinkingMessageMap[id] = false
  reasoningCollapsedMap[id] = Boolean(message.reasoning_content?.trim())
  return normalized
}

function renderMessageMarkdown(content: string) {
  return renderMarkdown(content)
}

function isStreamingMessage(message: AssistantMessage) {
  return message.id === activeStreamingMessageId.value
}

function isThinkingMessage(message: AssistantMessage) {
  return thinkingMessageMap[message.id] === true
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

    viewport.scrollTo({
      top: viewport.scrollHeight,
      behavior: smooth ? 'smooth' : 'auto',
    })
    messageScrollRaf = 0
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
  } catch (error) {
    ElMessage.warning(error instanceof Error ? error.message : '会话列表加载失败，请稍后重试')
  } finally {
    conversationLoading.value = false
    conversationLoadingMore.value = false
  }
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

// startResizeHistoryPanel 负责处理会话列表与聊天主区之间的横向拖拽。
// 职责边界：
// 1. 只负责更新助手面板内部的历史区宽度，不修改外层 Dashboard 的左右二分布局。
// 2. 会为正文区保留最小阅读宽度，避免把长回答挤压到难以阅读。
// 3. 拖拽结束后统一解绑事件并清理全局样式，防止页面残留 col-resize 状态。
function startResizeHistoryPanel(event: PointerEvent) {
  const body = assistantBodyRef.value
  if (isStandaloneMode.value || !body || window.innerWidth <= 960 || !historyExpanded.value) {
    return
  }

  const rect = body.getBoundingClientRect()
  const startX = event.clientX
  const startWidth = historyPanelWidth.value

  const handlePointerMove = (moveEvent: PointerEvent) => {
    const deltaX = moveEvent.clientX - startX
    const minHistoryWidth = 188
    const minChatWidth = 420
    const splitterWidth = 8
    const maxHistoryWidth = rect.width - splitterWidth - minChatWidth
    historyPanelWidth.value = Math.min(Math.max(startWidth + deltaX, minHistoryWidth), maxHistoryWidth)
  }

  const stopResize = () => {
    window.removeEventListener('pointermove', handlePointerMove)
    window.removeEventListener('pointerup', stopResize)
    document.body.classList.remove('dashboard-resizing')
  }

  document.body.classList.add('dashboard-resizing')
  window.addEventListener('pointermove', handlePointerMove)
  window.addEventListener('pointerup', stopResize)
}

function toggleHistoryPanel() {
  historyExpanded.value = !historyExpanded.value
}

async function loadConversationMessages(conversationId: string) {
  if (!conversationId) {
    return
  }

  if (conversationMessagesMap[conversationId] && unavailableHistoryMap[conversationId] !== true) {
    return
  }

  try {
    const history = await getConversationHistory(conversationId)
    conversationMessagesMap[conversationId] = history.map(normalizeHistoryMessage)
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

async function selectConversation(conversationId: string) {
  selectedConversationId.value = conversationId
  applyPreferredModelForConversation(conversationId)
  await Promise.allSettled([loadConversationMessages(conversationId), ensureConversationMeta(conversationId)])
  scheduleScrollMessagesToBottom(false, true)
}

function startNewConversation() {
  selectedConversationId.value = ''
  messageInput.value = ''
  activeStreamingMessageId.value = ''
  shouldAutoFollowMessages.value = true
}

// fetchChatStream 负责以 fetch 方式发起聊天请求，并处理一次 refresh token 自动重试。
// 职责边界：
// 1. 只负责把请求发出去并返回原始 Response，不在这里解析 SSE 数据。
// 2. 401 时优先尝试用 refresh token 换新 access token，并只重试一次，避免死循环。
// 3. 若最终仍未通过鉴权，则清空本地登录态，让页面统一回到重新登录的安全状态。
async function fetchChatStream(body: ChatStreamRequest, attempt = 0): Promise<Response> {
  const response = await fetch('/api/v1/agent/chat', {
    method: 'POST',
    headers: {
      'Content-Type': 'application/json',
      Authorization: `Bearer ${authStore.accessToken}`,
    },
    body: JSON.stringify(body),
  })

  if (response.status === 401 && attempt === 0 && authStore.refreshToken) {
    const tokens = await refreshToken({
      old_refresh_token: authStore.refreshToken,
    })
    authStore.applyTokenPair(tokens)
    return fetchChatStream(body, attempt + 1)
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
    delta.reasoning_content &&
    !assistantMessage.content.trim()
  ) {
    markReasoningStart(assistantMessage)
    assistantMessage.reasoning = `${assistantMessage.reasoning || ''}${delta.reasoning_content}`
    thinkingMessageMap[assistantMessage.id] = true
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

// sendMessage 负责执行“本地先上屏，再异步接流”的发送链路。
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

  const draftConversationId = selectedConversationId.value || createDraftConversationId()
  if (!selectedConversationId.value) {
    selectedConversationId.value = draftConversationId
  }
  savePreferredModel(draftConversationId, selectedModel.value)

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

  thinkingMessageMap[assistantMessage.id] = thinkingEnabled.value
  reasoningCollapsedMap[assistantMessage.id] = false
  activeStreamingMessageId.value = assistantMessage.id

  messageInput.value = ''
  prependConversationPreview(draftConversationId, text, now)
  scheduleScrollMessagesToBottom(false, true)

  try {
    const response = await fetchChatStream({
      conversation_id: isDraftConversationId(draftConversationId) ? undefined : draftConversationId,
      message: text,
      model: selectedModel.value,
      thinking: thinkingEnabled.value,
    })

    const responseConversationId = response.headers.get('X-Conversation-ID')?.trim()
    const actualConversationId = responseConversationId || draftConversationId

    if (actualConversationId !== draftConversationId) {
      migrateConversationState(draftConversationId, actualConversationId)
      prependConversationPreview(actualConversationId, text, now)
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

    await loadConversationListData(true)
    await ensureConversationMeta(actualConversationId)
  } catch (error) {
    if (!assistantMessage.content.trim()) {
      assistantMessage.content = '本次回复已中断，请稍后重试。'
    }
    reasoningCollapsedMap[assistantMessage.id] = false
    ElMessage.error(error instanceof Error ? error.message : '发送消息失败，请稍后重试')
  } finally {
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

watch(
  selectedModel,
  (nextModel) => {
    const conversationId = selectedConversationId.value
    if (!conversationId) {
      return
    }
    savePreferredModel(conversationId, nextModel)
  },
)

onMounted(async () => {
  reasoningTicker = window.setInterval(() => {
    reasoningDisplayNow.value = Date.now()
  }, 1000)
  await loadConversationListData(true)
})

onBeforeUnmount(() => {
  if (messageScrollRaf) {
    cancelAnimationFrame(messageScrollRaf)
  }
  if (reasoningTicker) {
    window.clearInterval(reasoningTicker)
    reasoningTicker = 0
  }
  document.body.classList.remove('dashboard-resizing')
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

          <div class="assistant-history__content" @scroll="handleHistoryScroll">
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
        :class="{ 'assistant-splitter--hidden': !historyExpanded || isStandaloneMode }"
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
            v-for="message in selectedMessages"
            :key="message.id"
            class="chat-message"
            :class="`chat-message--${message.role}`"
          >
            <div v-if="message.role === 'user'" class="chat-message__user-row">
              <div class="chat-message__user-bubble">
                <div class="chat-message__markdown" v-html="renderMessageMarkdown(message.content)" />
              </div>
              <span class="chat-message__time chat-message__time--user">{{ formatMessageTime(message.createdAt) }}</span>
            </div>

            <div v-else class="chat-message__assistant-flow">
              <div v-if="shouldShowReasoningBox(message)" class="chat-message__reasoning">
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
                    <span class="chat-message__reasoning-status">{{ getReasoningStatusLabel(message) }}</span>
                  </div>
                  <button
                    type="button"
                    class="chat-message__reasoning-toggle"
                    :aria-label="isReasoningCollapsed(message.id) ? '展开深度思考' : '折叠深度思考'"
                    @click="toggleReasoningCollapse(message.id)"
                  >
                    <span class="chat-message__reasoning-chevron">
                      <svg
                        class="chat-message__reasoning-chevron-icon"
                        :class="{ 'chat-message__reasoning-chevron-icon--expanded': !isReasoningCollapsed(message.id) }"
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

                <div v-if="!isReasoningCollapsed(message.id)" class="chat-message__reasoning-body">
                  <div
                    v-if="message.reasoning"
                    class="chat-message__markdown chat-message__markdown--reasoning"
                    v-html="renderMessageMarkdown(message.reasoning)"
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

              <div v-if="message.content" class="chat-message__assistant-content">
                <div class="chat-message__markdown chat-message__markdown--assistant" v-html="renderMessageMarkdown(message.content)" />
              </div>
              <div v-else-if="shouldShowAnsweringIndicator(message)" class="chat-message__streaming chat-message__streaming--plain">
                <div class="typing-indicator">
                  <span />
                  <span />
                  <span />
                </div>
              </div>

              <span class="chat-message__time">{{ formatMessageTime(message.createdAt) }}</span>
            </div>
          </article>
        </div>

        <div class="assistant-actions">
          <button
            v-for="action in quickActions"
            :key="action"
            type="button"
            class="assistant-actions__chip"
            @click="sendMessage(action)"
          >
            {{ action }}
          </button>
        </div>

        <div class="_9a2f8e4 assistant-composer-ds">
          <div class="aaff8b8f">
            <div class="_77cefa5 _9996a53">
              <div class="_020ab5b">
                <div class="_24fad49">
                  <textarea
                    v-model="messageInput"
                    class="_27c9245 ds-scroll-area ds-scroll-area--show-on-focus-within d96f2d2a"
                    placeholder="给 DeepSeek 发送消息 "
                    rows="2"
                    @keydown.enter.exact.prevent="sendMessage()"
                  />
                  <div class="b13855df" />
                </div>

                <div class="ec4f5d61">
                  <button
                    type="button"
                    class="ds-atom-button f79352dc ds-toggle-button ds-toggle-button--md"
                    :class="{ 'ds-toggle-button--selected': thinkingEnabled }"
                    @click="thinkingEnabled = !thinkingEnabled"
                  >
                    <div class="ds-icon ds-atom-button__icon">
                      <svg width="14" height="14" viewBox="0 0 14 14" fill="none" xmlns="http://www.w3.org/2000/svg">
                        <path d="M7.06428 5.93342C7.6876 5.93342 8.19304 6.43904 8.19319 7.06233C8.19319 7.68573 7.68769 8.19123 7.06428 8.19123C6.44096 8.19113 5.93537 7.68567 5.93537 7.06233C5.93552 6.43911 6.44105 5.93353 7.06428 5.93342Z" fill="currentColor" />
                        <path fill-rule="evenodd" clip-rule="evenodd" d="M8.68147 0.963693C10.1168 0.447019 11.6266 0.374829 12.5633 1.31135C13.5 2.24805 13.4276 3.75776 12.911 5.19319C12.7126 5.74431 12.4385 6.31796 12.0965 6.89729C12.4969 7.54638 12.8141 8.19018 13.036 8.80647C13.5527 10.2419 13.625 11.7516 12.6883 12.6883C11.7516 13.625 10.2419 13.5527 8.80647 13.036C8.19019 12.8141 7.54638 12.4969 6.89729 12.0965C6.31794 12.4386 5.74432 12.7125 5.19319 12.911C3.75774 13.4276 2.24807 13.5 1.31135 12.5633C0.374829 11.6266 0.447019 10.1168 0.963693 8.68147C1.17182 8.10338 1.46318 7.50063 1.82893 6.8924C1.52179 6.35711 1.27232 5.82825 1.08869 5.31819C0.572038 3.88278 0.499683 2.37306 1.43635 1.43635C2.37304 0.499655 3.88277 0.572044 5.31819 1.08869C5.82825 1.27232 6.35712 1.5218 6.8924 1.82893C7.50063 1.46318 8.10338 1.17181 8.68147 0.963693ZM11.3572 8.01154C10.9083 8.62253 10.3901 9.22873 9.8094 9.8094C9.22874 10.3901 8.62252 10.9083 8.01154 11.3572C8.42567 11.5841 8.82867 11.7688 9.21272 11.9071C10.5455 12.3868 11.4246 12.2547 11.8397 11.8397C12.2547 11.4246 12.3869 10.5456 11.9071 9.21272C11.7688 8.82866 11.5841 8.42568 11.3572 8.01154ZM2.56526 8.02912C2.3734 8.39322 2.21492 8.74796 2.0926 9.08772C1.61288 10.4204 1.74509 11.2995 2.15998 11.7147C2.57502 12.1297 3.45412 12.2618 4.78694 11.7821C5.11053 11.6656 5.44783 11.5164 5.79377 11.3367C5.24897 10.9223 4.70919 10.4533 4.19026 9.9344C3.57575 9.31987 3.03166 8.67633 2.56526 8.02912ZM6.90705 3.2469C6.24062 3.70479 5.56457 4.26321 4.91389 4.91389C4.26322 5.56456 3.70479 6.24063 3.2469 6.90705C3.72671 7.63325 4.32774 8.37459 5.03889 9.08576C5.6494 9.69627 6.2818 10.2265 6.90803 10.6678C7.59365 10.2025 8.29077 9.63076 8.96076 8.96076C9.63077 8.29075 10.2025 7.59366 10.6678 6.90803C10.2265 6.2818 9.69628 5.6494 9.08576 5.03889C8.37459 4.32773 7.63325 3.72672 6.90705 3.2469ZM11.7147 2.15998C11.2995 1.74509 10.4204 1.61288 9.08772 2.0926C8.74832 2.21479 8.39379 2.37271 8.0301 2.56428C8.67725 3.03065 9.31992 3.5758 9.9344 4.19026C10.4533 4.7092 10.9223 5.24896 11.3367 5.79377C11.5164 5.44785 11.6656 5.11052 11.7821 4.78694C12.2618 3.45416 12.1297 2.57502 11.7147 2.15998ZM4.91194 2.2176C3.57918 1.73788 2.70001 1.86995 2.28498 2.28498C1.86998 2.70003 1.73788 3.5792 2.2176 4.91194C2.31706 5.18822 2.44109 5.47427 2.58674 5.7674C3.01928 5.1887 3.51471 4.6158 4.06526 4.06526C4.61581 3.5147 5.18869 3.01928 5.7674 2.58674C5.47428 2.4411 5.18821 2.31706 4.91194 2.2176Z" fill="currentColor" />
                      </svg>
                    </div>
                    <span><span class="_6dbc175">深度思考</span></span>
                  </button>

                  <div class="assistant-toolbar__pill assistant-toolbar__pill--select assistant-toolbar__pill--ds-model">
                    <span class="assistant-toolbar__select-label">模型</span>
                    <el-select
                      v-model="selectedModel"
                      class="assistant-toolbar__select-box"
                      size="small"
                      popper-class="assistant-model-select-panel"
                      placement="top-start"
                      :teleported="true"
                    >
                      <el-option value="worker" label="标准" />
                      <el-option value="strategist" label="策略" />
                    </el-select>
                  </div>

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

                  <button
                    type="button"
                    class="_7436101 bcc55ca1 ds-icon-button ds-icon-button--l ds-icon-button--sizing-container"
                    :class="{ 'ds-icon-button--disabled': chatLoading || !messageInput.trim() }"
                    :disabled="chatLoading || !messageInput.trim()"
                    :aria-disabled="chatLoading || !messageInput.trim()"
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
  grid-template-columns: minmax(212px, 1fr) 8px minmax(0, 5fr);
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
  display: grid;
  align-content: start;
  gap: 12px;
  padding: 0 10px 14px 12px;
}

.assistant-history__new {
  width: 100%;
  height: 42px;
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
  min-height: 38px;
  padding: 8px 10px;
  border: 1px solid transparent;
  border-radius: 10px;
  background: transparent;
  color: #1f2937;
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 8px;
  text-align: left;
  transition: border-color 0.15s ease, background-color 0.15s ease, color 0.15s ease;
}

.assistant-history__item:hover {
  border-color: rgba(54, 96, 210, 0.18);
  background: rgba(237, 242, 255, 0.72);
}

.assistant-history__item-title {
  min-width: 0;
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
}

.assistant-shell--standalone .assistant-history__item--active {
  border-color: rgba(49, 96, 202, 0.3);
  background: #edf2ff;
  box-shadow: 0 4px 10px rgba(36, 67, 127, 0.08);
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
  display: grid;
  gap: 12px;
}

.chat-message__assistant-content {
  padding-right: 10px;
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

.assistant-composer-ds {
  --dsw-alias-brand-text: #3357c2;
  --dsw-alias-label-primary: #1f2430;
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

.assistant-toolbar__pill--ds-model {
  height: 32px;
  padding: 0 8px 0 10px;
  border: 1px solid rgba(15, 23, 42, 0.1);
  border-radius: 999px;
  background: #ffffff;
  display: inline-flex;
  align-items: center;
  gap: 8px;
  margin-right: auto;
  min-width: 144px;
  flex: 0 0 auto;
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
}
</style>
<style>
.assistant-model-select-panel.el-popper {
  border-radius: 12px;
  border: 1px solid rgba(15, 23, 42, 0.1);
  box-shadow: 0 10px 28px rgba(15, 23, 42, 0.14);
  padding: 6px;
}

.assistant-model-select-panel .el-select-dropdown__item {
  height: 36px;
  line-height: 36px;
  border-radius: 8px;
  padding: 0 12px;
  color: #4d5d73;
  font-size: 14px;
  font-weight: 600;
}

.assistant-model-select-panel .el-select-dropdown__item.hover,
.assistant-model-select-panel .el-select-dropdown__item:hover {
  background: rgba(51, 95, 194, 0.1);
}

.assistant-model-select-panel .el-select-dropdown__item.is-selected {
  color: #2f56b0;
  background: rgba(51, 95, 194, 0.16);
}
</style>

