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

const authStore = useAuthStore()

const assistantBodyRef = ref<HTMLElement | null>(null)
const messageViewportRef = ref<HTMLElement | null>(null)

const conversationLoading = ref(false)
const conversationLoadingMore = ref(false)
const chatLoading = ref(false)
const historyExpanded = ref(true)
const selectedConversationId = ref('')
const selectedModel = ref<'worker' | 'strategist'>('worker')
const thinkingEnabled = ref(false)
const messageInput = ref('')
const historyPanelWidth = ref(228)
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

const quickActions = [
  '帮我梳理今天最重要的三件事',
  '把当前任务拆成可执行步骤',
  '总结这段对话的关键结论',
  '给我一个更稳妥的推进方案',
]

let messageScrollRaf = 0

const assistantBodyStyle = computed(() => ({
  '--assistant-history-width': `${historyExpanded.value ? historyPanelWidth.value : 68}px`,
}))

const selectedConversation = computed(() =>
  conversationList.value.find((item) => item.conversation_id === selectedConversationId.value),
)

const selectedMessages = computed(() => {
  if (!selectedConversationId.value) {
    return []
  }
  return conversationMessagesMap[selectedConversationId.value] ?? []
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
  const normalized: AssistantMessage = {
    id,
    role: message.role,
    content: message.content,
    createdAt: message.created_at ?? new Date().toISOString(),
    reasoning: message.reasoning_content,
  }

  thinkingMessageMap[id] = Boolean(message.reasoning_content?.trim())
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

function scheduleScrollMessagesToBottom(smooth = false) {
  if (messageScrollRaf) {
    cancelAnimationFrame(messageScrollRaf)
  }

  messageScrollRaf = window.requestAnimationFrame(() => {
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
  if (!body || window.innerWidth <= 960 || !historyExpanded.value) {
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
  await Promise.allSettled([loadConversationMessages(conversationId), ensureConversationMeta(conversationId)])
  scheduleScrollMessagesToBottom(false)
}

function startNewConversation() {
  selectedConversationId.value = ''
  messageInput.value = ''
  activeStreamingMessageId.value = ''
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

  if (typeof delta?.reasoning_content === 'string' && delta.reasoning_content) {
    assistantMessage.reasoning = `${assistantMessage.reasoning || ''}${delta.reasoning_content}`
    thinkingMessageMap[assistantMessage.id] = true
  }

  if (typeof delta?.content === 'string' && delta.content) {
    assistantMessage.content += delta.content
  }

  if (finishReason) {
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
  scheduleScrollMessagesToBottom(false)

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

onMounted(async () => {
  await loadConversationListData(true)
})

onBeforeUnmount(() => {
  if (messageScrollRaf) {
    cancelAnimationFrame(messageScrollRaf)
  }
  document.body.classList.remove('dashboard-resizing')
})
</script>

<template>
  <aside class="assistant-shell glass-panel">
    <header class="assistant-header">
      <div class="assistant-header__text">
        <span class="assistant-header__eyebrow">AI 对话</span>
        <strong>{{ selectedConversationTitle }}</strong>
        <p>{{ selectedConversationSubtitle }}</p>
      </div>
      <button type="button" class="assistant-header__action" @click="startNewConversation">新对话</button>
    </header>

    <div
      ref="assistantBodyRef"
      class="assistant-body"
      :class="{ 'assistant-body--collapsed': !historyExpanded }"
      :style="assistantBodyStyle"
    >
      <aside class="assistant-history" :class="{ 'assistant-history--collapsed': !historyExpanded }">
        <div class="assistant-history__toolbar">
          <strong v-if="historyExpanded">会话</strong>
          <button type="button" class="assistant-history__toggle" @click="toggleHistoryPanel">
            {{ historyExpanded ? '收起' : '展开' }}
          </button>
        </div>

        <div class="assistant-history__content" @scroll="handleHistoryScroll">
          <button type="button" class="assistant-history__new" @click="startNewConversation">
            <span>+</span>
            <strong>{{ historyExpanded ? '新建会话' : '新' }}</strong>
            <small v-if="historyExpanded">从空白上下文开始</small>
          </button>

          <div v-if="conversationLoading && !conversationListReady" class="assistant-history__loading">
            <div v-for="index in 4" :key="index" class="assistant-history__loading-item" />
          </div>

          <template v-else>
            <button
              v-for="item in conversationList"
              :key="item.conversation_id"
              type="button"
              class="assistant-history__item"
              :class="{ 'assistant-history__item--active': item.conversation_id === selectedConversationId }"
              @click="selectConversation(item.conversation_id)"
            >
              <strong>{{ item.has_title && item.title ? item.title : '未命名会话' }}</strong>
              <small v-if="historyExpanded">{{ formatConversationTime(item.last_message_at || item.created_at) }}</small>
            </button>

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
        <div ref="messageViewportRef" class="assistant-messages">
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
                    <span class="chat-message__reasoning-dot" />
                    <strong>{{ isStreamingMessage(message) ? '深度思考中' : '深度思考' }}</strong>
                  </div>
                  <button
                    type="button"
                    class="chat-message__reasoning-toggle"
                    @click="toggleReasoningCollapse(message.id)"
                  >
                    {{ isReasoningCollapsed(message.id) ? '展开' : '折叠' }}
                  </button>
                </div>

                <div v-if="!isReasoningCollapsed(message.id)">
                  <div
                    v-if="message.reasoning"
                    class="chat-message__markdown chat-message__markdown--reasoning"
                    v-html="renderMessageMarkdown(message.reasoning)"
                  />
                  <div v-else class="chat-message__streaming">
                    <span>正在接收 reasoning 增量...</span>
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
              <div v-else-if="isStreamingMessage(message)" class="chat-message__streaming chat-message__streaming--plain">
                <span>{{ message.reasoning ? '正在生成正文内容...' : '正在建立连接...' }}</span>
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

        <div class="assistant-composer">
          <textarea
            v-model="messageInput"
            class="assistant-composer__input"
            placeholder="给 AI 发消息，Enter 发送，Shift + Enter 换行"
            rows="3"
            @keydown.enter.exact.prevent="sendMessage()"
          />
          <button
            type="button"
            class="assistant-composer__send"
            :disabled="chatLoading || !messageInput.trim()"
            @click="sendMessage()"
          >
            发送
          </button>
        </div>

        <div class="assistant-toolbar">
          <button
            type="button"
            class="assistant-toolbar__pill"
            :class="{ 'assistant-toolbar__pill--active': thinkingEnabled }"
            @click="thinkingEnabled = !thinkingEnabled"
          >
            深度思考
          </button>

          <label class="assistant-toolbar__pill assistant-toolbar__pill--select">
            <span>模型</span>
            <select v-model="selectedModel" class="assistant-toolbar__select">
              <option value="worker">标准</option>
              <option value="strategist">策略</option>
            </select>
          </label>
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

.assistant-header,
.assistant-history__toolbar,
.assistant-actions,
.assistant-composer,
.assistant-toolbar {
  background: rgba(255, 255, 255, 0.92);
}

.assistant-header {
  display: flex;
  justify-content: space-between;
  gap: 16px;
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

.assistant-header__action,
.assistant-history__toggle,
.assistant-actions__chip,
.assistant-composer__send,
.assistant-toolbar__pill,
.chat-message__reasoning-toggle {
  cursor: pointer;
}

.assistant-header__action {
  height: 40px;
  padding: 0 15px;
  border: 1px solid rgba(36, 102, 220, 0.14);
  border-radius: 14px;
  background: #f8fbff;
  color: #2363cb;
  font-weight: 700;
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

.assistant-history {
  min-width: 0;
  min-height: 0;
  display: grid;
  grid-template-rows: auto minmax(0, 1fr);
  border-right: 1px solid rgba(16, 24, 40, 0.05);
  background: linear-gradient(180deg, rgba(248, 250, 253, 0.96), rgba(244, 247, 252, 0.92));
}

.assistant-history__toolbar {
  display: flex;
  justify-content: space-between;
  gap: 8px;
  padding: 14px 12px 10px;
}

.assistant-history__toggle {
  border: none;
  background: transparent;
  color: #718097;
  font-size: 12px;
}

.assistant-history__content {
  min-height: 0;
  overflow-y: auto;
  display: grid;
  align-content: start;
  gap: 10px;
  padding: 0 10px 14px;
}

.assistant-history__new,
.assistant-history__item {
  width: 100%;
  padding: 12px;
  border: 1px solid rgba(16, 24, 40, 0.05);
  border-radius: 18px;
  background: rgba(255, 255, 255, 0.88);
  text-align: left;
}

.assistant-history__new span {
  display: inline-flex;
  width: 28px;
  height: 28px;
  align-items: center;
  justify-content: center;
  border-radius: 10px;
  background: #eef4ff;
  color: #2b69d4;
  font-size: 17px;
  font-weight: 700;
}

.assistant-history__new strong,
.assistant-history__item strong,
.assistant-history__new small,
.assistant-history__item small {
  display: block;
}

.assistant-history__new strong,
.assistant-history__item strong {
  margin-top: 8px;
  color: #182335;
  font-size: 12px;
  line-height: 1.45;
}

.assistant-history__new small,
.assistant-history__item small,
.assistant-history__empty,
.assistant-history__end {
  color: #7b889b;
  font-size: 11px;
}

.assistant-history__item--active {
  border-color: rgba(36, 102, 220, 0.16);
  background: linear-gradient(180deg, #f5f9ff, #eef5ff);
}

.assistant-history--collapsed .assistant-history__new,
.assistant-history--collapsed .assistant-history__item {
  padding: 10px;
  display: grid;
  place-items: center;
}

.assistant-history--collapsed .assistant-history__new strong,
.assistant-history--collapsed .assistant-history__new small,
.assistant-history--collapsed .assistant-history__item small {
  display: none;
}

.assistant-history__loading {
  display: grid;
  gap: 10px;
}

.assistant-history__loading-item {
  height: 72px;
  border-radius: 18px;
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
  padding-top: 4px;
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
  padding: 14px 16px;
  border-color: rgba(92, 122, 170, 0.14);
  background: linear-gradient(180deg, rgba(245, 247, 251, 0.96), rgba(239, 243, 248, 0.98));
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
  margin-bottom: 10px;
}

.chat-message__reasoning-title {
  display: flex;
  align-items: center;
  gap: 8px;
}

.chat-message__reasoning-dot {
  width: 8px;
  height: 8px;
  border-radius: 999px;
  background: #5a98ff;
  box-shadow: 0 0 0 0 rgba(90, 152, 255, 0.34);
  animation: pulse-dot 1.6s ease-in-out infinite;
}

.chat-message__reasoning-toggle {
  border: none;
  background: rgba(255, 255, 255, 0.72);
  color: #5f728b;
  font-size: 12px;
  border-radius: 999px;
  padding: 6px 10px;
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
  font-size: 13px;
  line-height: 1.8;
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

.chat-message__streaming {
  justify-content: space-between;
  gap: 12px;
  min-height: 26px;
  font-size: 13px;
}

.chat-message__streaming--plain {
  padding-right: 10px;
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

.assistant-composer {
  display: grid;
  grid-template-columns: minmax(0, 1fr) auto;
  gap: 12px;
  padding: 12px 22px 10px;
  border-top: 1px solid rgba(16, 24, 40, 0.05);
}

.assistant-composer__input {
  width: 100%;
  resize: none;
  border: 1px solid rgba(16, 24, 40, 0.08);
  border-radius: 18px;
  padding: 14px 16px;
  outline: none;
  background: #fbfcfe;
  font-size: 14px;
  line-height: 1.65;
  font-family: inherit;
}

.assistant-composer__send {
  align-self: end;
  min-width: 88px;
  height: 50px;
  border: none;
  border-radius: 16px;
  background: linear-gradient(180deg, #1656b8, #15469a);
  color: #fff;
  font-weight: 700;
}

.assistant-toolbar {
  gap: 10px;
  flex-wrap: wrap;
  padding: 0 22px 18px;
}

.assistant-toolbar__pill {
  height: 38px;
  padding: 0 14px;
  border: 1px solid rgba(16, 24, 40, 0.06);
  border-radius: 999px;
  background: #f7f9fc;
  color: #55657b;
  font-size: 13px;
  font-weight: 600;
  display: inline-flex;
  align-items: center;
  gap: 8px;
}

.assistant-toolbar__pill--active {
  border-color: rgba(36, 102, 220, 0.16);
  background: #edf4ff;
  color: #225fc5;
}

.assistant-toolbar__pill--select {
  padding-right: 10px;
}

.assistant-toolbar__select {
  border: none;
  background: transparent;
  color: inherit;
  font: inherit;
  outline: none;
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
  .assistant-composer,
  .assistant-toolbar {
    padding-left: 18px;
    padding-right: 18px;
  }
}
</style>
