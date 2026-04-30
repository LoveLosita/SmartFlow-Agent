import type { ApplyThinkingSummaryOptions, StreamExtraPayload, StreamToolExtraPayload, ToolTraceState } from '@/types/assistant-panel'
import type {
  AssistantMessage,
  ChatRequestExtra,
  ConversationListItem,
  ThinkingModeType,
  ThinkingSummaryPayload,
} from '@/types/dashboard'

interface TaskFormLike {
  title: string
  priority_group: number
  deadline_at: Date | string | null
  urgency_threshold_at: Date | string | null
}

export function createDraftConversationId() {
  return `draft-${Date.now()}-${Math.random().toString(36).slice(2, 8)}`
}

export function createMessageId(role: AssistantMessage['role']) {
  return `${role}-${Date.now()}-${Math.random().toString(36).slice(2, 8)}`
}

export function isDraftConversationId(conversationId: string) {
  return conversationId.startsWith('draft-')
}

export function isLocalEphemeralMessageId(id: string) {
  return /^(user|assistant|system)-\d{13}-[a-z0-9]+$/i.test(id)
}

export function resolveConversationGroupLabel(timeText?: string | null) {
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

export function migrateConversationListIds(
  items: ConversationListItem[],
  fromConversationId: string,
  toConversationId: string,
) {
  const latestMap = new Map<string, ConversationListItem>()
  const deduplicated: ConversationListItem[] = []
  const seen = new Set<string>()

  for (const item of items) {
    const nextItem =
      item.conversation_id === fromConversationId ? { ...item, conversation_id: toConversationId } : item
    latestMap.set(nextItem.conversation_id, nextItem)
  }

  for (const item of items) {
    const nextId = item.conversation_id === fromConversationId ? toConversationId : item.conversation_id
    if (seen.has(nextId)) {
      continue
    }
    seen.add(nextId)
    deduplicated.push(latestMap.get(nextId)!)
  }

  return deduplicated
}

export function mergeConversationListItems(
  currentItems: ConversationListItem[],
  nextItems: ConversationListItem[],
) {
  const merged = [...currentItems, ...nextItems]
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

  return deduplicated
}

export function buildConversationPreviewItem(
  conversationId: string,
  previewText: string,
  createdAt: string,
  current: ConversationListItem | undefined,
  messageCount: number,
): ConversationListItem {
  return {
    conversation_id: conversationId,
    title: current?.title || previewText.slice(0, 24),
    has_title: current?.has_title ?? false,
    message_count: Math.max(current?.message_count ?? 0, messageCount),
    last_message_at: createdAt,
    status: current?.status || 'active',
    created_at: current?.created_at || createdAt,
  }
}

export function serializeTaskDateForApi(value: Date | string | null) {
  if (!value) {
    return null
  }
  return typeof value === 'string' ? value : value.toISOString()
}

export function formatTaskDeadlineForStatus(value: Date | string | null) {
  if (!value) {
    return null
  }
  return value instanceof Date
    ? value.toLocaleDateString('zh-CN').replace(/\//g, '-')
    : String(value).split('T')[0]
}

export function buildTaskUpdatePayload(taskId: number, taskForm: TaskFormLike) {
  return {
    task_id: taskId,
    title: taskForm.title.trim(),
    priority_group: taskForm.priority_group,
    deadline_at: serializeTaskDateForApi(taskForm.deadline_at),
    urgency_threshold_at: serializeTaskDateForApi(taskForm.urgency_threshold_at),
  }
}

export function buildAssistantChatRequestExtra(
  planningTaskClassIds: number[] = [],
  executionMode: 'manual' | 'always',
): ChatRequestExtra | undefined {
  const extra: ChatRequestExtra = {}

  // 1. 任务类别过滤：将智能编排所需的 task_class_ids 透传给后端。
  if (planningTaskClassIds.length > 0) {
    extra.task_class_ids = [...planningTaskClassIds]
  }

  // 2. 执行模式控制：若开启“自动执行”，则透传 always_execute 标志，跳过工具调用确认逻辑。
  if (executionMode === 'always') {
    extra.always_execute = true
  }

  return Object.keys(extra).length > 0 ? extra : undefined
}

export function isManualThinkingEnabled(mode: ThinkingModeType) {
  return mode === 'true'
}

export function getThinkingBackendKey(opts: Pick<ApplyThinkingSummaryOptions, 'backendBlockId' | 'stage'>) {
  const backendKeyRaw = (opts.backendBlockId || opts.stage || 'thinking').trim()
  return backendKeyRaw || 'thinking'
}

export function buildThinkingSummarySignature(summary: ThinkingSummaryPayload) {
  return [
    typeof summary.summary_seq === 'number' ? summary.summary_seq : '',
    (summary.short_summary || '').trim(),
    (summary.detail_summary || '').trim(),
    typeof summary.duration_seconds === 'number' ? summary.duration_seconds : '',
    summary.final === true ? '1' : '0',
  ].join('\u001f')
}

export function mapToolEventState(rawStatus?: string): ToolTraceState {
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

export function normalizeToolSummary(extra: StreamToolExtraPayload): string {
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

export function buildToolDetail(extra: StreamToolExtraPayload): string {
  const argsPreview = `${extra.arguments_preview || ''}`.trim()
  if (!argsPreview || argsPreview === '{}') {
    return ''
  }
  return argsPreview
}

export function normalizeStatusCode(rawCode?: string) {
  const code = `${rawCode || ''}`.trim().toLowerCase()
  if (!code) {
    return 'status'
  }
  return code
}

export function mapStatusCodeLabel(code: string) {
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

export function buildStatusSummary(extra: StreamExtraPayload): string {
  const summary = `${extra.status?.summary || ''}`.trim()
  if (summary) {
    return summary
  }
  return mapStatusCodeLabel(normalizeStatusCode(extra.status?.code))
}

export function isLegacyToolStatusCode(code: string) {
  return code === 'tool_call' || code === 'tool_result' || code === 'tool_blocked'
}

export function mapLegacyToolStatusToState(code: string): ToolTraceState {
  if (code === 'tool_call') {
    return 'called'
  }
  if (code === 'tool_blocked') {
    return 'blocked'
  }
  return 'completed'
}

export function shouldSkipStatusEvent(code: string, stage = '') {
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

export function isAssistantTimelineKind(kind: string) {
  const assistantKinds = new Set([
    'assistant_text',
    'tool_call',
    'tool_result',
    'confirm_request',
    'schedule_completed',
    'interrupt',
    'status',
    'business_card',
    'thinking_summary',
  ])
  return assistantKinds.has(kind)
}
