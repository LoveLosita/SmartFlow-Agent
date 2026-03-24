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
const historyPanelWidth = ref(220)
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

const quickActions = ['帮我梳理今天最重要的三件事', '把当前任务拆成可执行步骤', '总结这段对话的关键结论', '给我一个更稳妥的推进方案']
const capabilityPoints = ['流式输出', 'Markdown 渲染', '会话懒加载', '深度思考展示']

let messageScrollRaf = 0

const assistantBodyStyle = computed(() => ({
  '--assistant-history-width': `${historyExpanded.value ? historyPanelWidth.value : 64}px`,
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
    return '新的会话'
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
    return '支持流式输出、Markdown 渲染与深度思考展示'
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
  conversationMessagesMap[conversationId].push(message)
  thinkingMessageMap[message.id] = Boolean(message.reasoning?.trim())
}

function upsertConversationMeta(meta: ConversationMeta) {
  conversationMetaMap[meta.conversation_id] = meta
}

// mergeConversationList 负责把分页拿到的会话列表按会话 ID 合并进本地状态。
// 职责边界：
// 1. 负责“保留现有顺序 + 更新最新字段 + 去重”，避免懒加载时列表闪烁。
// 2. 不负责决定选中哪个会话，选中逻辑交给 ensureSelectedConversationAfterListLoad。
// 3. 若后端返回重复项，以后出现的记录覆盖前面的字段，保证时间和标题尽量新。
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

  conversationList.value = [nextItem, ...conversationList.value.filter((item) => item.conversation_id !== conversationId)]
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

function shouldShowReasoningBox(message: AssistantMessage) {
  return message.role === 'assistant' && (Boolean(message.reasoning?.trim()) || (isStreamingMessage(message) && isThinkingMessage(message)))
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
// 1. reset=true 时重置分页并重新获取第一页，适合新消息发送后刷新列表顺序。
// 2. reset=false 时只在还有更多数据且当前不在加载时继续拉下一页，避免重复请求。
// 3. 接口失败时只提示并保留现有列表，防止用户当前聊天内容被清空。
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

    const currentItems = result?.list ?? []
    if (reset) {
      conversationList.value = currentItems
    } else {
      mergeConversationList(currentItems)
    }

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
// 1. 只负责更新内部历史面板宽度，不修改整个 Dashboard 的左右布局。
// 2. 为聊天区保留最小宽度，避免拖拽后消息正文被压到无法阅读。
// 3. 拖拽结束后必须解绑事件并清理全局样式，避免页面残留 col-resize 状态。
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
    const minHistoryWidth = 180
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
  if (!conversationId || conversationMessagesMap[conversationId]) {
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
  if (!conversationId || conversationMetaMap[conversationId]) {
    return
  }

  try {
    const meta = await getConversationMeta(conversationId)
    upsertConversationMeta(meta)
  } catch {
    // 这里故意静默失败：
    // 1. 标题和条数属于增强信息，不应阻塞聊天主链路。
    // 2. 即使元信息失败，列表里已有的回退标题仍可保证界面可用。
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

// fetchChatStream 负责以 fetch 方式发起聊天请求并处理一次 refresh token 自动重试。
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
      const errorBody = (await response.json()) as { info?: string }
      throw new Error(errorBody.info || '发送消息失败，请稍后重试')
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
// 1. 只处理 data: 开头的事件行，忽略 [DONE] 和无法解析的脏数据，保证前端更健壮。
// 2. reasoning_content 与 content 分开追加，确保“思考过程”和“最终回答”能同时流式可见。
// 3. 如果后端显式下发 error.message 或 finish_reason，这里立即同步到当前状态，便于上层收尾。
function processSseBlock(block: string, assistantMessage: AssistantMessage) {
  const dataLines = block
    .split('\n')
    .map((line) => line.trim())
    .filter((line) => line.startsWith('data:'))

  for (const line of dataLines) {
    const payload = line.replace(/^data:\s*/, '')
    if (!payload || payload === '[DONE]') {
      continue
    }

    let parsed: StreamEventPayload
    try {
      parsed = JSON.parse(payload) as StreamEventPayload
    } catch {
      continue
    }

    if (parsed.error?.message) {
      throw new Error(parsed.error.message)
    }

    const choice = parsed.choices?.[0]
    const delta = choice?.delta

    if (typeof delta?.reasoning_content === 'string' && delta.reasoning_content) {
      assistantMessage.reasoning = `${assistantMessage.reasoning || ''}${delta.reasoning_content}`
      thinkingMessageMap[assistantMessage.id] = true
    }

    if (typeof delta?.content === 'string' && delta.content) {
      assistantMessage.content += delta.content
    }

    if (choice?.finish_reason) {
      activeStreamingMessageId.value = ''
    }

    scheduleScrollMessagesToBottom(false)
  }
}

async function sendMessage(preset?: string) {
  const text = (preset ?? messageInput.value).trim()
  if (!text || chatLoading.value) {
    return
  }

  chatLoading.value = true
  let assistantMessage: AssistantMessage | null = null

  try {
    const response = await fetchChatStream({
      conversation_id: selectedConversationId.value || undefined,
      message: text,
      model: selectedModel.value,
      thinking: thinkingEnabled.value,
    })

    const conversationId =
      response.headers.get('X-Conversation-ID')?.trim() || selectedConversationId.value || `draft-${Date.now()}`

    if (!selectedConversationId.value) {
      selectedConversationId.value = conversationId
      unavailableHistoryMap[conversationId] = false
    }

    ensureConversationBucket(conversationId)

    const now = new Date().toISOString()
    appendConversationMessage(conversationId, {
      id: `user-${Date.now()}`,
      role: 'user',
      content: text,
      createdAt: now,
    })

    assistantMessage = {
      id: `assistant-${Date.now()}`,
      role: 'assistant',
      content: '',
      createdAt: now,
      reasoning: '',
    }
    appendConversationMessage(conversationId, assistantMessage)
    thinkingMessageMap[assistantMessage.id] = thinkingEnabled.value
    activeStreamingMessageId.value = assistantMessage.id

    messageInput.value = ''
    prependConversationPreview(conversationId, text, now)
    scheduleScrollMessagesToBottom(false)

    const responseBody = response.body
    if (!responseBody) {
      throw new Error('流式响应体为空，无法继续接收消息')
    }

    const reader = responseBody.getReader()
    const decoder = new TextDecoder('utf-8')
    let buffer = ''

    while (true) {
      const { done, value } = await reader.read()
      if (done) {
        break
      }

      buffer += decoder.decode(value, { stream: true })
      const blocks = buffer.split('\n\n')
      buffer = blocks.pop() ?? ''

      for (const block of blocks) {
        processSseBlock(block, assistantMessage)
      }
    }

    buffer += decoder.decode()
    if (buffer.trim()) {
      processSseBlock(buffer, assistantMessage)
    }

    if (!assistantMessage.content.trim() && assistantMessage.reasoning?.trim()) {
      assistantMessage.content = '已完成深度思考，但当前响应未返回正文内容。'
    }

    await loadConversationListData(true)

    try {
      const meta = await getConversationMeta(conversationId)
      upsertConversationMeta(meta)
    } catch {
      // 这里保持静默兜底：
      // 1. 会话元信息失败不影响当前已经拿到的正文与历史消息。
      // 2. 列表刷新后仍可继续点击、继续追问，不阻断主流程。
      // 3. 等后端元信息接口稳定后，重新进入页面即可自然补齐。
    }
  } catch (error) {
    if (assistantMessage && !assistantMessage.content.trim()) {
      assistantMessage.content = '本次回复已中断，请稍后重试。'
    }
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
    <div class="assistant-header">
      <div class="assistant-title">
        <span class="assistant-title__badge">AI</span>
        <div>
          <strong>AI 助手</strong>
          <small>接近 ChatGPT Web 风格的扁平化对话区，支持会话历史和流式回答。</small>
        </div>
      </div>

      <button type="button" class="assistant-header__action" @click="startNewConversation">
        新建会话
      </button>
    </div>

    <div
      ref="assistantBodyRef"
      class="assistant-body"
      :class="{ 'assistant-body--collapsed': !historyExpanded }"
      :style="assistantBodyStyle"
    >
      <section class="assistant-history" :class="{ 'assistant-history--collapsed': !historyExpanded }">
        <div class="assistant-history__toolbar">
          <strong v-if="historyExpanded">聊天记录</strong>
          <button type="button" class="assistant-history__toggle" @click="toggleHistoryPanel">
            {{ historyExpanded ? '收起' : '展开' }}
          </button>
        </div>

        <div class="assistant-history__content" @scroll="handleHistoryScroll">
          <button type="button" class="assistant-history__new" @click="startNewConversation">
            <span>+</span>
            <template v-if="historyExpanded">
              <strong>发起新对话</strong>
              <small>清空当前上下文，开始新会话</small>
            </template>
          </button>

          <div v-if="conversationLoading && !conversationList.length" class="assistant-history__loading">
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
              <strong>{{ historyExpanded ? item.title || '未命名会话' : '对' }}</strong>
              <small v-if="historyExpanded">
                {{ formatConversationTime(item.last_message_at || item.created_at) }} · {{ item.message_count }} 条消息
              </small>
            </button>

            <div v-if="conversationLoadingMore" class="assistant-history__loading assistant-history__loading--more">
              <div v-for="index in 2" :key="index" class="assistant-history__loading-item" />
            </div>

            <p v-if="conversationListReady && !conversationList.length" class="assistant-history__empty">
              还没有历史会话，先发起一段新对话吧。
            </p>
            <p v-else-if="conversationListReady && !conversationHasMore && conversationList.length" class="assistant-history__end">
              已经到底了
            </p>
          </template>
        </div>
      </section>

      <div
        class="assistant-splitter"
        :class="{ 'assistant-splitter--hidden': !historyExpanded }"
        role="separator"
        aria-label="调整历史记录宽度"
        @pointerdown.prevent="startResizeHistoryPanel"
      >
        <span class="assistant-splitter__line" />
      </div>

      <section class="assistant-chat">
        <header class="assistant-chat__header">
          <div>
            <h3>{{ selectedConversationTitle }}</h3>
            <p>{{ selectedConversationSubtitle }}</p>
          </div>

          <div class="assistant-chat__header-side">
            <span class="assistant-chat__status" :class="{ 'assistant-chat__status--active': chatLoading }">
              {{ chatLoading ? '流式输出中' : '待命中' }}
            </span>
          </div>
        </header>

        <div class="assistant-capabilities">
          <span
            v-for="point in capabilityPoints"
            :key="point"
            class="assistant-capabilities__item"
            :class="{ 'assistant-capabilities__item--active': chatLoading }"
          >
            {{ point }}
          </span>
        </div>
        <div ref="messageViewportRef" class="assistant-messages">
          <div v-if="shouldShowHistoryFallback" class="assistant-chat__fallback">
            当前会话的历史消息接口暂时不可用，但你仍然可以继续在这个会话里追问；等接口补齐后，历史内容会自动恢复。
          </div>

          <div v-if="!selectedMessages.length && !chatLoading" class="assistant-empty">
            <div class="assistant-empty__halo" />
            <strong>从这里开始和 AI 协作</strong>
            <p>你可以直接输入问题，也可以点下方快捷操作。回答支持流式输出，长内容会按 Markdown 渲染。</p>
          </div>

          <article
            v-for="message in selectedMessages"
            :key="message.id"
            class="chat-message"
            :class="`chat-message--${message.role}`"
          >
            <div class="chat-message__avatar">
              {{ message.role === 'assistant' ? 'AI' : message.role === 'user' ? '我' : '系统' }}
            </div>
            <div class="chat-message__bubble">
              <div v-if="shouldShowReasoningBox(message)" class="chat-message__reasoning">
                <div class="chat-message__reasoning-head">
                  <span class="chat-message__reasoning-dot" />
                  <strong>{{ isStreamingMessage(message) ? '深度思考中' : '思考过程' }}</strong>
                </div>

                <div
                  v-if="message.reasoning"
                  class="chat-message__markdown chat-message__markdown--reasoning"
                  v-html="renderMessageMarkdown(message.reasoning)"
                />
                <div v-else class="chat-message__reasoning-streaming">
                  <span>正在接收 reasoning 增量...</span>
                  <div class="typing-indicator">
                    <span />
                    <span />
                    <span />
                  </div>
                </div>
              </div>

              <div
                v-if="message.content"
                class="chat-message__markdown"
                v-html="renderMessageMarkdown(message.content)"
              />
              <div v-else-if="message.role === 'assistant' && isStreamingMessage(message)" class="chat-message__streaming">
                <span>{{ message.reasoning ? '正在生成正文内容...' : '正在建立流式输出...' }}</span>
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
            placeholder="输入你的问题，Enter 发送，Shift + Enter 换行"
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

        <div class="assistant-options">
          <label class="assistant-options__group">
            <span>模型</span>
            <el-select v-model="selectedModel" size="small" class="assistant-options__select">
              <el-option label="Worker" value="worker" />
              <el-option label="Strategist" value="strategist" />
            </el-select>
          </label>

          <label class="assistant-options__group assistant-options__group--switch">
            <span>深度思考</span>
            <el-switch v-model="thinkingEnabled" inline-prompt active-text="开" inactive-text="关" />
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
  max-height: 100%;
  border-radius: 28px;
  border: 1px solid rgba(17, 24, 39, 0.08);
  display: grid;
  grid-template-rows: auto minmax(0, 1fr);
  overflow: hidden;
}

.assistant-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
  gap: 14px;
  padding: 16px 16px 14px;
  border-bottom: 1px solid rgba(17, 24, 39, 0.06);
  background: rgba(255, 255, 255, 0.78);
}

.assistant-title {
  display: flex;
  align-items: center;
  gap: 10px;
}

.assistant-title__badge {
  width: 38px;
  height: 38px;
  border-radius: 12px;
  background: linear-gradient(180deg, #0f73ea 0%, #1d5ec7 100%);
  color: #fff;
  display: inline-flex;
  align-items: center;
  justify-content: center;
  font-weight: 800;
}

.assistant-title strong,
.assistant-title small {
  display: block;
}

.assistant-title strong {
  font-size: 17px;
  color: #111c30;
}

.assistant-title small {
  margin-top: 4px;
  color: #738198;
  font-size: 12px;
}

.assistant-header__action {
  border: 1px solid rgba(37, 99, 235, 0.16);
  background: #f5f9ff;
  color: #1f63d1;
  border-radius: 12px;
  padding: 9px 12px;
  font-weight: 700;
  cursor: pointer;
  transition:
    transform 0.18s ease,
    box-shadow 0.18s ease,
    background-color 0.18s ease;
}

.assistant-header__action:hover {
  transform: translateY(-1px);
  box-shadow: 0 10px 20px rgba(15, 23, 42, 0.06);
}

.assistant-body {
  --assistant-history-width: 220px;
  height: 100%;
  min-height: 0;
  display: grid;
  grid-template-columns: var(--assistant-history-width) 8px minmax(0, 1fr);
}

.assistant-body--collapsed {
  grid-template-columns: 64px 0 minmax(0, 1fr);
}

.assistant-history {
  min-width: 0;
  min-height: 0;
  display: grid;
  grid-template-rows: auto minmax(0, 1fr);
  border-right: 1px solid rgba(17, 24, 39, 0.06);
  background: rgba(249, 251, 254, 0.82);
}

.assistant-history--collapsed {
  border-right: none;
}

.assistant-history__toolbar {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 8px;
  padding: 12px 10px;
}

.assistant-history__toolbar strong {
  font-size: 13px;
  color: #3d4b62;
}

.assistant-history__toggle {
  border: none;
  background: transparent;
  color: #6f7d92;
  cursor: pointer;
  font-size: 12px;
  padding: 0;
}

.assistant-history__content {
  display: grid;
  gap: 9px;
  padding: 0 8px 12px;
  min-height: 0;
  overflow-y: auto;
  align-content: start;
}
.assistant-history__new,
.assistant-history__item {
  width: 100%;
  border: 1px solid rgba(17, 24, 39, 0.06);
  background: rgba(255, 255, 255, 0.92);
  border-radius: 16px;
  padding: 11px 10px;
  text-align: left;
  cursor: pointer;
  transition:
    transform 0.18s ease,
    box-shadow 0.18s ease,
    border-color 0.18s ease,
    background-color 0.18s ease;
}

.assistant-history__new:hover,
.assistant-history__item:hover {
  transform: translateY(-1px);
  box-shadow: 0 10px 20px rgba(15, 23, 42, 0.06);
}

.assistant-history__new span {
  display: inline-flex;
  width: 26px;
  height: 26px;
  align-items: center;
  justify-content: center;
  border-radius: 9px;
  background: #eef4ff;
  color: #2c69d4;
  font-size: 17px;
  font-weight: 700;
}

.assistant-history__new strong,
.assistant-history__item strong,
.assistant-history__item small,
.assistant-history__new small {
  display: block;
}

.assistant-history__new strong,
.assistant-history__item strong {
  margin-top: 8px;
  color: #152036;
  font-size: 12px;
  line-height: 1.45;
}

.assistant-history__new small,
.assistant-history__item small {
  margin-top: 6px;
  color: #7b8799;
  font-size: 11px;
  line-height: 1.4;
}

.assistant-history__item--active {
  border-color: rgba(37, 99, 235, 0.18);
  background: linear-gradient(180deg, #f4f8ff 0%, #edf4ff 100%);
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

.assistant-history--collapsed .assistant-history__item strong {
  margin-top: 0;
}

.assistant-history__loading {
  display: grid;
  gap: 9px;
}

.assistant-history__loading--more .assistant-history__loading-item {
  height: 56px;
}

.assistant-history__loading-item {
  height: 76px;
  border-radius: 16px;
  background: linear-gradient(
    90deg,
    rgba(230, 236, 244, 0.8),
    rgba(245, 248, 252, 1),
    rgba(230, 236, 244, 0.8)
  );
  background-size: 200% 100%;
  animation: history-shimmer 1.3s linear infinite;
}

.assistant-history__empty,
.assistant-history__end {
  margin: 0;
  padding: 6px 2px 0;
  text-align: center;
  color: #90a0b4;
  font-size: 12px;
}

.assistant-splitter {
  position: relative;
  height: 100%;
  display: flex;
  align-items: center;
  justify-content: center;
  cursor: col-resize;
  touch-action: none;
}

.assistant-splitter--hidden {
  opacity: 0;
  pointer-events: none;
}

.assistant-splitter__line {
  width: 3px;
  height: 56px;
  border-radius: 999px;
  background: linear-gradient(180deg, rgba(145, 163, 188, 0.24), rgba(88, 124, 177, 0.4), rgba(145, 163, 188, 0.24));
  transition:
    background-color 0.18s ease,
    transform 0.18s ease;
}

.assistant-splitter:hover .assistant-splitter__line {
  transform: scaleX(1.18);
  background: linear-gradient(180deg, rgba(104, 140, 194, 0.34), rgba(42, 108, 214, 0.62), rgba(104, 140, 194, 0.34));
}

.assistant-chat {
  min-width: 0;
  min-height: 0;
  display: grid;
  grid-template-rows: auto auto minmax(0, 1fr) auto auto auto;
}

.assistant-chat__header {
  display: flex;
  justify-content: space-between;
  gap: 14px;
  align-items: flex-start;
  padding: 16px 18px 10px;
}

.assistant-chat__header h3 {
  margin: 0;
  font-size: 19px;
  line-height: 1.2;
  letter-spacing: -0.02em;
  color: #111c30;
}

.assistant-chat__header p {
  margin: 6px 0 0;
  color: #768396;
  font-size: 12px;
}

.assistant-chat__header-side {
  display: flex;
  align-items: center;
  justify-content: flex-end;
}

.assistant-chat__status {
  border-radius: 999px;
  padding: 7px 10px;
  font-size: 11px;
  color: #607086;
  background: #f5f8fc;
  border: 1px solid rgba(17, 24, 39, 0.04);
}

.assistant-chat__status--active {
  color: #1f63d1;
  background: #edf4ff;
}

.assistant-capabilities {
  display: flex;
  flex-wrap: wrap;
  gap: 7px;
  padding: 0 18px 10px;
}

.assistant-capabilities__item {
  padding: 7px 11px;
  border-radius: 999px;
  background: #f5f8fc;
  color: #607086;
  font-size: 11px;
  border: 1px solid rgba(17, 24, 39, 0.04);
  transition: background-color 0.24s ease;
}

.assistant-capabilities__item--active {
  background: #ecf4ff;
  color: #1f63d1;
}

.assistant-messages {
  min-height: 0;
  overflow-y: auto;
  padding: 8px 18px 16px;
  display: grid;
  gap: 12px;
  align-content: start;
  background:
    linear-gradient(180deg, rgba(248, 250, 252, 0.74) 0%, rgba(255, 255, 255, 0.96) 60%),
    radial-gradient(circle at top center, rgba(122, 169, 255, 0.08), transparent 34%);
}

.assistant-chat__fallback {
  padding: 13px 14px;
  border-radius: 14px;
  background: #f8fbff;
  border: 1px solid rgba(37, 99, 235, 0.1);
  color: #617189;
  font-size: 12px;
}
.assistant-empty {
  min-height: 200px;
  display: grid;
  place-items: center;
  align-content: center;
  justify-items: center;
  text-align: center;
  gap: 10px;
  color: #677588;
}

.assistant-empty strong {
  color: #12243e;
}

.assistant-empty p {
  margin: 0;
  max-width: 420px;
  line-height: 1.7;
}

.assistant-empty__halo {
  width: 68px;
  height: 68px;
  border-radius: 24px;
  background: radial-gradient(circle at center, rgba(37, 99, 235, 0.2), rgba(37, 99, 235, 0.02));
  animation: halo-breathe 2.4s ease-in-out infinite;
}

.chat-message {
  display: grid;
  grid-template-columns: 38px minmax(0, 1fr);
  gap: 10px;
  align-items: start;
  animation: message-rise 0.26s ease;
}

.chat-message--user {
  grid-template-columns: minmax(0, 1fr) 38px;
}

.chat-message--user .chat-message__avatar {
  order: 2;
  background: #eff4fb;
  color: #2d415e;
}

.chat-message--user .chat-message__bubble {
  order: 1;
  margin-left: auto;
  background: linear-gradient(180deg, #1368dd 0%, #165bc0 100%);
  color: #fff;
}

.chat-message__avatar {
  width: 38px;
  height: 38px;
  border-radius: 12px;
  display: inline-flex;
  align-items: center;
  justify-content: center;
  background: #11284b;
  color: #fff;
  font-weight: 800;
  font-size: 12px;
}

.chat-message__bubble {
  max-width: min(96%, 880px);
  padding: 13px 14px 11px;
  border-radius: 18px;
  background: #ffffff;
  border: 1px solid rgba(17, 24, 39, 0.06);
  box-shadow: 0 10px 24px rgba(15, 23, 42, 0.05);
}

.chat-message__reasoning {
  margin-bottom: 10px;
  padding: 10px 11px;
  border-radius: 12px;
  background: #f5f8fe;
  border: 1px solid rgba(90, 152, 255, 0.12);
  color: #5d6d84;
}

.chat-message__reasoning-head {
  display: flex;
  align-items: center;
  gap: 8px;
  margin-bottom: 8px;
  color: #446388;
  font-size: 12px;
}

.chat-message__reasoning-dot {
  width: 8px;
  height: 8px;
  border-radius: 999px;
  background: #5a98ff;
  box-shadow: 0 0 0 0 rgba(90, 152, 255, 0.34);
  animation: pulse-dot 1.6s ease-in-out infinite;
}

.chat-message__reasoning-streaming,
.chat-message__streaming {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
  min-height: 24px;
  color: #62738a;
  font-size: 13px;
}

.chat-message__markdown {
  color: inherit;
  font-size: 13px;
  line-height: 1.7;
  word-break: break-word;
}

.chat-message__markdown--reasoning {
  color: #5b6d84;
}

.chat-message__markdown :deep(p) {
  margin: 0;
}

.chat-message__markdown :deep(p + p),
.chat-message__markdown :deep(p + ul),
.chat-message__markdown :deep(p + ol),
.chat-message__markdown :deep(p + blockquote),
.chat-message__markdown :deep(ul + p),
.chat-message__markdown :deep(ol + p),
.chat-message__markdown :deep(pre + p),
.chat-message__markdown :deep(p + pre) {
  margin-top: 10px;
}

.chat-message__markdown :deep(h1),
.chat-message__markdown :deep(h2),
.chat-message__markdown :deep(h3),
.chat-message__markdown :deep(h4),
.chat-message__markdown :deep(h5),
.chat-message__markdown :deep(h6) {
  margin: 0 0 10px;
  line-height: 1.35;
  color: inherit;
}

.chat-message__markdown :deep(ul),
.chat-message__markdown :deep(ol) {
  margin: 0;
  padding-left: 20px;
}

.chat-message__markdown :deep(li + li) {
  margin-top: 6px;
}

.chat-message__markdown :deep(blockquote) {
  margin: 0;
  padding-left: 12px;
  border-left: 3px solid rgba(71, 115, 179, 0.18);
  color: inherit;
}

.chat-message__markdown :deep(a) {
  color: #1f63d1;
  text-decoration: none;
}

.chat-message--user .chat-message__markdown :deep(a) {
  color: #fff;
  text-decoration: underline;
}

.chat-message__markdown :deep(code) {
  padding: 2px 6px;
  border-radius: 6px;
  background: rgba(15, 23, 42, 0.06);
  font-family: Consolas, 'Courier New', monospace;
  font-size: 12px;
}

.chat-message--user .chat-message__markdown :deep(code) {
  background: rgba(255, 255, 255, 0.16);
}

.chat-message__markdown :deep(.md-pre) {
  margin: 0;
  padding: 12px 13px;
  border-radius: 12px;
  background: #f4f7fb;
  overflow-x: auto;
}

.chat-message--user .chat-message__markdown :deep(.md-pre) {
  background: rgba(255, 255, 255, 0.14);
}

.chat-message__markdown :deep(.md-pre code) {
  padding: 0;
  background: transparent;
}

.chat-message__time {
  display: block;
  margin-top: 10px;
  font-size: 11px;
  color: rgba(110, 124, 146, 0.9);
}

.chat-message--user .chat-message__time {
  color: rgba(255, 255, 255, 0.72);
}
.assistant-actions {
  display: flex;
  flex-wrap: wrap;
  gap: 8px;
  padding: 0 18px 12px;
}

.assistant-actions__chip {
  border: 1px solid rgba(17, 24, 39, 0.05);
  background: #f8fafc;
  color: #506076;
  border-radius: 999px;
  padding: 8px 11px;
  cursor: pointer;
  font-size: 12px;
  transition:
    transform 0.18s ease,
    background-color 0.18s ease;
}

.assistant-actions__chip:hover {
  transform: translateY(-1px);
  background: #eef4ff;
}

.assistant-composer {
  display: grid;
  grid-template-columns: minmax(0, 1fr) auto;
  gap: 10px;
  padding: 12px 18px 10px;
  border-top: 1px solid rgba(17, 24, 39, 0.06);
  background: rgba(255, 255, 255, 0.92);
}

.assistant-composer__input {
  width: 100%;
  resize: none;
  border: 1px solid rgba(17, 24, 39, 0.08);
  border-radius: 16px;
  padding: 13px 14px;
  outline: none;
  background: #fbfcfe;
  transition: border-color 0.18s ease;
  font-size: 13px;
  font-family: inherit;
}

.assistant-composer__input:focus {
  border-color: rgba(37, 99, 235, 0.28);
}

.assistant-composer__send {
  align-self: end;
  height: 48px;
  min-width: 84px;
  border: none;
  border-radius: 14px;
  background: linear-gradient(180deg, #126ce4 0%, #1358bb 100%);
  color: #fff;
  font-weight: 700;
  cursor: pointer;
  transition:
    transform 0.18s ease,
    box-shadow 0.18s ease,
    opacity 0.18s ease;
}

.assistant-composer__send:hover:not(:disabled) {
  transform: translateY(-1px);
  box-shadow: 0 14px 24px rgba(18, 108, 228, 0.26);
}

.assistant-composer__send:disabled {
  opacity: 0.52;
  cursor: not-allowed;
}

.assistant-options {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
  padding: 0 18px 14px;
  background: rgba(255, 255, 255, 0.92);
}

.assistant-options__group {
  display: flex;
  align-items: center;
  gap: 8px;
  color: #55657c;
  font-size: 12px;
  font-weight: 600;
}

.assistant-options__group--switch {
  margin-left: auto;
}

.assistant-options__select {
  width: 136px;
}

.typing-indicator {
  display: flex;
  gap: 6px;
  align-items: center;
  min-height: 20px;
}

.typing-indicator span {
  width: 9px;
  height: 9px;
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
  0%,
  80%,
  100% {
    transform: translateY(0);
    opacity: 0.5;
  }

  40% {
    transform: translateY(-4px);
    opacity: 1;
  }
}

@keyframes pulse-dot {
  0% {
    box-shadow: 0 0 0 0 rgba(90, 152, 255, 0.34);
  }

  70% {
    box-shadow: 0 0 0 8px rgba(90, 152, 255, 0);
  }

  100% {
    box-shadow: 0 0 0 0 rgba(90, 152, 255, 0);
  }
}

@keyframes halo-breathe {
  0%,
  100% {
    transform: scale(0.96);
    opacity: 0.82;
  }

  50% {
    transform: scale(1.06);
    opacity: 1;
  }
}

@keyframes message-rise {
  from {
    transform: translateY(8px);
    opacity: 0;
  }

  to {
    transform: translateY(0);
    opacity: 1;
  }
}

@keyframes history-shimmer {
  0% {
    background-position: 200% 0;
  }

  100% {
    background-position: -200% 0;
  }
}

@media (max-width: 960px) {
  .assistant-body,
  .assistant-body--collapsed {
    grid-template-columns: 1fr;
  }

  .assistant-history {
    border-right: none;
    border-bottom: 1px solid rgba(17, 24, 39, 0.06);
  }

  .assistant-history__content {
    max-height: 240px;
  }

  .assistant-splitter {
    display: none;
  }

  .assistant-chat__header,
  .assistant-options {
    flex-wrap: wrap;
  }

  .assistant-options__group--switch {
    margin-left: 0;
  }
}
</style>