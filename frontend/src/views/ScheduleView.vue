<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { useRoute, useRouter } from 'vue-router'

import {
  applyBatchIntoSchedule,
  createTaskClass,
  deleteScheduleEntries,
  deleteTaskClassItem,
  getTaskClassDetail,
  getTaskClassList,
  getWeekSchedule,
  smartPlanning,
  smartPlanningMulti,
  updateTaskClass,
} from '@/api/scheduleCenter'
import CreateTaskClassDialog from '@/components/schedule/CreateTaskClassDialog.vue'
import TaskClassSidebar from '@/components/schedule/TaskClassSidebar.vue'
import WeekPlanningBoard from '@/components/schedule/WeekPlanningBoard.vue'
import CourseImageImportDialog from '@/components/schedule/CourseImageImportDialog.vue'
import type { ApplyBatchIntoScheduleItem, ScheduleWeekData, ScheduleWeekEvent, TaskClassDetail, TaskClassListItem } from '@/types/schedule'
import { formatHeaderDate } from '@/utils/date'

interface SidebarItem {
  key: 'home' | 'task' | 'calendar' | 'ai'
  label: string
  short: string
  to?: '/dashboard' | '/assistant' | '/schedule'
}

interface WeekDayHeader {
  dayOfWeek: number
  label: string
  dateLabel: string
}

interface SchedulePreviewRuntimeState {
  weeks: ScheduleWeekData[] | null
  taskClassIds: number[]
  currentWeek: number | null
}

interface PreviewMovePayload {
  week: number
  sourceDayOfWeek: number
  sourceOrder: number
  targetDayOfWeek: number
  targetOrder: number
}

interface SuggestedPreviewItem {
  id: number
  name: string
  type: string
  span: number
}

interface SchedulePreviewSlotRef {
  week: number
  dayOfWeek: number
  order: number
}

type SchedulePageWindow = Window & {
  __schedulePreviewBeforeUnloadRegistered__?: boolean
}

// schedulePreviewRuntimeState 负责在“单页应用未刷新”的生命周期内保留智能编排预览。
//
// 设计说明：
// 1. 这里故意不用 sessionStorage/localStorage，因为用户明确要求“刷新就丢”，所以只保留运行时内存。
// 2. 这里负责跨路由回到 /schedule 时恢复预览；不负责持久化到浏览器磁盘。
// 3. 真正需要清空预览的时机，统一由 clearPreviewState 显式控制，避免切周时误清空。
const schedulePreviewRuntimeState: SchedulePreviewRuntimeState = {
  weeks: null,
  taskClassIds: [],
  currentWeek: null,
}

const EMPTY_EMBEDDED_TASK_INFO = {
  id: 0,
  name: '',
  type: 'task',
}

const SCHEDULE_SECTION_TIME_MAP: Record<number, [string, string]> = {
  1: ['08:00', '09:40'],
  2: ['10:15', '11:55'],
  3: ['14:00', '15:40'],
  4: ['16:15', '17:55'],
  5: ['19:00', '20:40'],
  6: ['20:50', '22:30'],
}

// handleSchedulePreviewBeforeUnload 负责在存在未应用预览时阻止页面刷新/关闭。
//
// 职责边界：
// 1. 这里只负责触发浏览器原生确认弹框，不负责展示自定义 UI。
// 2. 只有存在未应用的智能编排结果时才拦截，避免影响正常刷新体验。
function handleSchedulePreviewBeforeUnload(event: BeforeUnloadEvent) {
  // 1. 如果处于手动编辑模式且有变更，拦截刷新。
  if (manualEditMode.value && (Boolean(previewWeeks.value?.length) || pendingDeleteIds.value.length > 0)) {
    event.preventDefault()
    event.returnValue = ''
    return
  }

  // 2. 如果存在待应用的智能预览，拦截刷新。
  if (schedulePreviewRuntimeState.weeks?.length) {
    event.preventDefault()
    event.returnValue = ''
  }
}

if (typeof window !== 'undefined') {
  const schedulePageWindow = window as SchedulePageWindow
  if (!schedulePageWindow.__schedulePreviewBeforeUnloadRegistered__) {
    window.addEventListener('beforeunload', handleSchedulePreviewBeforeUnload)
    schedulePageWindow.__schedulePreviewBeforeUnloadRegistered__ = true
  }
}

const router = useRouter()
const route = useRoute()

const taskClassLoading = ref(false)
const taskClassDetailLoading = ref(false)
const weekLoading = ref(false)
const smartPlanningLoading = ref(false)
const applyingLoading = ref(false)
const deletingLoading = ref(false)
const createDialogVisible = ref(false)
const createDialogLoading = ref(false)
const editDialogInitialData = ref<TaskClassDetail | null>(null)
const courseImportDialogVisible = ref(false)

const taskClasses = ref<TaskClassListItem[]>([])
const expandedTaskClassId = ref<number | null>(null)
const expandedTaskClassDetail = ref<TaskClassDetail | null>(null)
const taskClassMultiSelectMode = ref(false)
const selectedTaskClassIds = ref<number[]>([])
const selectedScheduleEventIds = ref<number[]>([])
const scheduleSelectionMode = ref(false)
const manualEditMode = ref(false)
const pendingDeleteIds = ref<number[]>([])
const hasManualChanges = ref(false)

const liveWeeks = ref<ScheduleWeekData[]>([])
const previewWeeks = ref<ScheduleWeekData[] | null>(schedulePreviewRuntimeState.weeks)
const previewTaskClassIds = ref<number[]>([...schedulePreviewRuntimeState.taskClassIds])
const currentWeek = ref<number | null>(schedulePreviewRuntimeState.currentWeek)
const weekBase = ref<number | null>(null)
const baseMonday = ref<Date | null>(null)
const lastStableWeekData = ref<ScheduleWeekData | null>(null)
const weekScheduleCache = ref<Record<number, ScheduleWeekData>>({})

const MIN_SCHEDULE_WEEK = 1
const MAX_SCHEDULE_WEEK = 24

let weekRequestSequence = 0
let activeWeekRequestSequence = 0

const effectiveSelectedTaskClassIds = computed(() => {
  if (taskClassMultiSelectMode.value) {
    return selectedTaskClassIds.value
  }

  return expandedTaskClassId.value ? [expandedTaskClassId.value] : []
})

const augmentedTaskClassDetail = computed<TaskClassDetail | null>(() => {
  if (!expandedTaskClassDetail.value) {
    return null
  }

  const detail = { ...expandedTaskClassDetail.value }
  detail.items = detail.items.map((item) => {
    // 0. 只要有 ID，就尝试进行匹配（注意类型一致性）
    const targetId = Number(item.id)
    if (isNaN(targetId) || targetId <= 0) {
      return item
    }

    // 1. 先查找当前预览(previewWeeks)中是否有该任务块。
    for (const weekData of previewWeeks.value ?? []) {
      const event = weekData.events.find((e) =>
        (e.status === 'suggested') &&
        ((e.type === 'course' && Number(e.embedded_task_info?.id) === targetId) || (e.type !== 'course' && Number(e.id) === targetId)),
      )

      if (event) {
        return {
          ...item,
          embedded_time: {
            date: '', // 预览态下日期由预览周次决定，侧边栏格式化函数会处理
            section_from: (event.order - 1) * 2 + 1,
            section_to: (event.order - 1) * 2 + 2,
            _preview_week: weekData.week, // 标记这是预览周
            _day_of_week: event.day_of_week, // 注入周几信息
          } as any,
        }
      }
    }

    // 3. 如果该任务块被标记为待删除，则强制显示为“未安排”。
    if (typeof item.id === 'number' && pendingDeleteIds.value.includes(item.id)) {
      return { ...item, embedded_time: null }
    }

    return item
  })

  return detail
})

const previewWeekLookup = computed(() => {
  const map = new Map<number, ScheduleWeekData>()

  for (const item of previewWeeks.value ?? []) {
    map.set(item.week, item)
  }

  return map
})

const liveWeekLookup = computed(() => {
  const map = new Map<number, ScheduleWeekData>()

  for (const item of liveWeeks.value) {
    map.set(item.week, item)
  }

  return map
})

const hasPendingPreview = computed(() => Boolean(previewWeeks.value?.length) || manualEditMode.value)

const isEditUnsaved = computed(() =>
  manualEditMode.value && (Boolean(previewWeeks.value?.length) || pendingDeleteIds.value.length > 0),
)

const resolvedCurrentWeekData = computed(() => {
  if (!previewWeeks.value?.length && !liveWeeks.value.length) {
    return null
  }

  if (currentWeek.value === null) {
    return previewWeeks.value?.[0] ?? liveWeeks.value[0] ?? null
  }

  return previewWeekLookup.value.get(currentWeek.value)
    ?? liveWeekLookup.value.get(currentWeek.value)
    ?? null
})

const currentWeekData = computed(() =>
  resolvedCurrentWeekData.value ?? lastStableWeekData.value,
)

const weekHeaders = computed<WeekDayHeader[]>(() => {
  const weekdayMap = ['Mon', 'Tue', 'Wed', 'Thu', 'Fri', 'Sat', 'Sun']

  return Array.from({ length: 7 }, (_, index) => {
    const dayOfWeek = index + 1
    const date = resolveDateByWeekDay(currentWeek.value, dayOfWeek)
    return {
      dayOfWeek,
      label: weekdayMap[index],
      dateLabel: date ? `${date.getDate()}日` : '',
    }
  })
})

const weekLabel = computed(() => {
  if (!currentWeek.value) {
    return '第--周'
  }
  return `第${numberToChinese(currentWeek.value)}周`
})

const currentTermLabel = computed(() => {
  const now = new Date()
  const semester = now.getMonth() + 1 >= 8 ? '秋季学期' : '春季学期'
  return `${now.getFullYear()}学年${semester}`
})

const showSmartPlanningButton = computed(() =>
  !scheduleSelectionMode.value && effectiveSelectedTaskClassIds.value.length > 0,
)

const showDeleteModeButton = computed(() =>
  !scheduleSelectionMode.value && effectiveSelectedTaskClassIds.value.length === 0,
)

const showApplyButton = computed(() =>
  !scheduleSelectionMode.value && (hasPendingPreview.value || pendingDeleteIds.value.length > 0),
)

const canGoPreviousWeek = computed(() =>
  currentWeek.value !== null && currentWeek.value > MIN_SCHEDULE_WEEK,
)

const canGoNextWeek = computed(() =>
  currentWeek.value !== null && currentWeek.value < MAX_SCHEDULE_WEEK,
)

function handleSidebarNavigate(item: SidebarItem) {
  if (item.to) {
    if (route.path !== item.to) {
      void router.push(item.to)
    }
    return
  }

  ElMessage.info(`${item.label} 页面正在开发中`)
}

function startOfWeek(date: Date) {
  const next = new Date(date)
  const day = next.getDay()
  const diff = day === 0 ? -6 : 1 - day
  next.setDate(next.getDate() + diff)
  next.setHours(0, 0, 0, 0)
  return next
}

function resolveDateByWeekDay(week: number | null, dayOfWeek: number) {
  if (weekBase.value === null || !baseMonday.value || week === null) {
    return null
  }

  const date = new Date(baseMonday.value)
  date.setDate(baseMonday.value.getDate() + (week - weekBase.value) * 7 + (dayOfWeek - 1))
  return date
}

function numberToChinese(value: number) {
  const digits = ['零', '一', '二', '三', '四', '五', '六', '七', '八', '九']
  if (value <= 10) {
    return value === 10 ? '十' : digits[value]
  }
  if (value < 20) {
    return `十${digits[value % 10]}`
  }
  const tens = Math.floor(value / 10)
  const units = value % 10
  return `${digits[tens]}十${units ? digits[units] : ''}`
}

// clampWeekIntoRange 负责把周次限制在后端允许的 1-24 周内。
//
// 职责边界：
// 1. 这里只做前端边界保护，不替代后端校验。
// 2. 调用方若需要交互提示，应在函数外自行决定是否提示。
function clampWeekIntoRange(week: number) {
  return Math.min(MAX_SCHEDULE_WEEK, Math.max(MIN_SCHEDULE_WEEK, week))
}

// syncLiveWeeksFromCache 负责把按周缓存拍平成页面当前使用的 liveWeeks 数组。
//
// 设计说明：
// 1. 页面内部仍按数组消费周数据，因此缓存层统一在这里做结构转换。
// 2. 固定按 week 升序输出，避免切周过快时 currentWeekData 出现不稳定回退。
function syncLiveWeeksFromCache() {
  liveWeeks.value = Object.values(weekScheduleCache.value)
    .sort((left, right) => left.week - right.week)
}

// cacheWeekSchedules 负责把成功请求到的周数据写入页面级本地缓存。
//
// 设计说明：
// 1. 只缓存 1-24 周的合法结果，越界数据直接丢弃，避免污染页面状态。
// 2. 相同周次直接覆盖，保证强刷后的新结果能够替换旧缓存。
function cacheWeekSchedules(weeks: ScheduleWeekData[]) {
  for (const item of weeks) {
    if (item.week < MIN_SCHEDULE_WEEK || item.week > MAX_SCHEDULE_WEEK) {
      continue
    }

    weekScheduleCache.value[item.week] = item
  }

  syncLiveWeeksFromCache()
}

// setPreviewState 负责同步“组件内预览态”和“模块级运行时预览态”。
//
// 设计说明：
// 1. 统一从这个入口写预览，避免 ref 和模块级缓存出现一边更新、一边遗漏。
// 2. taskClassIds 记录本次预览来自哪个任务类，用于刷新前提示、回到页面后继续应用。
// 3. 这里不负责决定何时清空预览，清空策略由 clearPreviewState 和具体业务动作控制。
function setPreviewState(weeks: ScheduleWeekData[] | null, taskClassIds: number[] = []) {
  previewWeeks.value = weeks
  previewTaskClassIds.value = [...taskClassIds]
  schedulePreviewRuntimeState.weeks = weeks
  schedulePreviewRuntimeState.taskClassIds = [...taskClassIds]
}

// clearPreviewState 负责显式清空未应用的智能编排结果。
//
// 职责边界：
// 1. 这里只做状态清理，不做消息提示。
// 2. 调用方负责在“应用成功 / 删除后重算 / 用户主动替换预览”等时机决定是否清空。
function clearPreviewState() {
  setPreviewState(null, [])
  pendingDeleteIds.value = []
  hasManualChanges.value = false
}

function isSuggestedPreviewEvent(event?: ScheduleWeekEvent) {
  return Boolean(event && event.status === 'suggested')
}

function cloneScheduleEvent(event: ScheduleWeekEvent): ScheduleWeekEvent {
  return {
    ...event,
    embedded_task_info: event.embedded_task_info
      ? { ...event.embedded_task_info }
      : { ...EMPTY_EMBEDDED_TASK_INFO },
  }
}

// clonePreviewWeeks 负责复制 preview 周数据，确保拖拽编辑只改前端预览态，不污染原引用。
//
// 设计说明：
// 1. 这里只做前端内存数据的浅层结构复制 + 事件深复制，足够支撑拖拽改位。
// 2. 不负责校验事件语义是否合法，语义校验由后面的 move helper 负责。
function clonePreviewWeeks(weeks: ScheduleWeekData[]) {
  return weeks.map((week) => ({
    ...week,
    events: week.events.map((event) => cloneScheduleEvent(event)),
  }))
}

function findPreviewEventIndex(weeks: ScheduleWeekData[], slot: SchedulePreviewSlotRef) {
  const weekIndex = weeks.findIndex((week) => week.week === slot.week)
  if (weekIndex < 0) {
    return { weekIndex: -1, eventIndex: -1 }
  }

  const eventIndex = weeks[weekIndex]!.events.findIndex((event) =>
    event.day_of_week === slot.dayOfWeek && event.order === slot.order)

  return { weekIndex, eventIndex }
}

function buildEmptyPreviewEvent(slot: SchedulePreviewSlotRef): ScheduleWeekEvent {
  const [startTime, endTime] = SCHEDULE_SECTION_TIME_MAP[slot.order] ?? ['', '']

  return {
    id: 0,
    order: slot.order,
    day_of_week: slot.dayOfWeek,
    name: '空白',
    start_time: startTime,
    end_time: endTime,
    location: '',
    type: 'empty',
    span: 2,
    status: 'normal',
    embedded_task_info: { ...EMPTY_EMBEDDED_TASK_INFO },
  }
}

function extractSuggestedPreviewItem(event?: ScheduleWeekEvent): SuggestedPreviewItem | null {
  if (!isSuggestedPreviewEvent(event)) {
    return null
  }

  if (event!.type === 'course' && event!.embedded_task_info?.id) {
    return {
      id: event!.embedded_task_info.id,
      name: event!.embedded_task_info.name,
      type: event!.embedded_task_info.type || 'task',
      span: event!.span || 2,
    }
  }

  return {
    id: event!.id,
    name: event!.name,
    type: event!.type || 'task',
    span: event!.span || 2,
  }
}

function canDropSuggestedIntoCell(event?: ScheduleWeekEvent) {
  if (!event || event.type === 'empty') {
    return true
  }

  if (event.status === 'suggested') {
    return true
  }

  return event.type === 'course'
}

// buildPreviewEventWithSuggested 负责把“某个格子的底板”与“某个 suggested 任务”重新拼装成最终事件。
//
// 职责边界：
// 1. 课程格收到 suggested 时，转换为“课程 + embedded_task_info + suggested 状态”。
// 2. 空白格收到 suggested 时，转换为独立 task 建议块。
// 3. 不带 suggested 时，会把格子还原成普通课程或空白格，保证拖拽前后 JSON 与画面一致。
function buildPreviewEventWithSuggested(
  baseEvent: ScheduleWeekEvent | undefined,
  slot: SchedulePreviewSlotRef,
  suggestedItem: SuggestedPreviewItem | null,
): ScheduleWeekEvent {
  if (baseEvent?.type === 'course') {
    return {
      ...cloneScheduleEvent(baseEvent),
      status: suggestedItem ? 'suggested' : 'normal',
      embedded_task_info: suggestedItem
        ? {
            id: suggestedItem.id,
            name: suggestedItem.name,
            type: suggestedItem.type || 'task',
          }
        : { ...EMPTY_EMBEDDED_TASK_INFO },
    }
  }

  if (!suggestedItem) {
    return buildEmptyPreviewEvent(slot)
  }

  const [startTime, endTime] = SCHEDULE_SECTION_TIME_MAP[slot.order] ?? ['', '']
  return {
    id: suggestedItem.id,
    order: slot.order,
    day_of_week: slot.dayOfWeek,
    name: suggestedItem.name || '未命名任务',
    start_time: startTime,
    end_time: endTime,
    location: '',
    type: suggestedItem.type || 'task',
    span: suggestedItem.span || 2,
    status: 'suggested',
    embedded_task_info: { ...EMPTY_EMBEDDED_TASK_INFO },
  }
}

function replacePreviewEventAtSlot(
  weeks: ScheduleWeekData[],
  slot: SchedulePreviewSlotRef,
  nextEvent: ScheduleWeekEvent,
) {
  const { weekIndex, eventIndex } = findPreviewEventIndex(weeks, slot)
  if (weekIndex < 0) {
    return
  }

  if (eventIndex >= 0) {
    weeks[weekIndex]!.events[eventIndex] = nextEvent
  } else {
    weeks[weekIndex]!.events.push(nextEvent)
  }

  weeks[weekIndex]!.events.sort((left, right) => {
    if (left.day_of_week !== right.day_of_week) {
      return left.day_of_week - right.day_of_week
    }
    return left.order - right.order
  })
}

// handleMovePreviewEvent 负责把用户拖拽后的 suggested 位置回写到前端预览 JSON。
//
// 处理步骤：
// 1. 只允许修改当前内存中的 previewWeeks；正式课表与后端缓存都不在这里改。
// 2. 源格必须是 suggested；目标格允许是空白、课程或另一个 suggested（交换）。
// 3. 修改完成后立即回写 setPreviewState，保证界面显示与最终 apply 用到的 JSON 完全一致。
function handleMovePreviewEvent(payload: PreviewMovePayload) {
  if (!previewWeeks.value?.length) {
    return
  }

  const sourceSlot: SchedulePreviewSlotRef = {
    week: payload.week,
    dayOfWeek: payload.sourceDayOfWeek,
    order: payload.sourceOrder,
  }
  const targetSlot: SchedulePreviewSlotRef = {
    week: payload.week,
    dayOfWeek: payload.targetDayOfWeek,
    order: payload.targetOrder,
  }

  if (
    sourceSlot.dayOfWeek === targetSlot.dayOfWeek &&
    sourceSlot.order === targetSlot.order
  ) {
    return
  }

  const nextWeeks = clonePreviewWeeks(previewWeeks.value)
  const sourceLocate = findPreviewEventIndex(nextWeeks, sourceSlot)
  const targetLocate = findPreviewEventIndex(nextWeeks, targetSlot)
  if (sourceLocate.weekIndex < 0 || sourceLocate.eventIndex < 0) {
    return
  }

  const sourceEvent = nextWeeks[sourceLocate.weekIndex]!.events[sourceLocate.eventIndex]
  const targetEvent = targetLocate.weekIndex >= 0 && targetLocate.eventIndex >= 0
    ? nextWeeks[targetLocate.weekIndex]!.events[targetLocate.eventIndex]
    : undefined

  const sourceSuggestedItem = extractSuggestedPreviewItem(sourceEvent)
  if (!sourceSuggestedItem || !canDropSuggestedIntoCell(targetEvent)) {
    return
  }

  const targetSuggestedItem = extractSuggestedPreviewItem(targetEvent)
  const nextSourceEvent = buildPreviewEventWithSuggested(sourceEvent, sourceSlot, targetSuggestedItem)
  const nextTargetEvent = buildPreviewEventWithSuggested(targetEvent, targetSlot, sourceSuggestedItem)

  replacePreviewEventAtSlot(nextWeeks, sourceSlot, nextSourceEvent)
  replacePreviewEventAtSlot(nextWeeks, targetSlot, nextTargetEvent)
  setPreviewState(nextWeeks, previewTaskClassIds.value)
  hasManualChanges.value = true
}

// handleDropTaskItem 负责处理从侧边栏拖入新任务块到格子的逻辑。
function handleDropTaskItem(payload: {
  id: number | string
  content: string
  taskClassId: number
  week: number
  dayOfWeek: number
  order: number
}) {
  if (!manualEditMode.value) return

  // 0. 强制规整 ID 为数值类型，防止后续匹配失效
  const targetId = Number(payload.id)
  if (isNaN(targetId) || targetId === 0) return

  const targetSlot: SchedulePreviewSlotRef = {
    week: payload.week,
    dayOfWeek: payload.dayOfWeek,
    order: payload.order,
  }

  // 1. 初始化预览数据（如果当前没有预览态，则基于 live 数据克隆）。
  const nextWeeks = previewWeeks.value?.length
    ? clonePreviewWeeks(previewWeeks.value)
    : clonePreviewWeeks(liveWeeks.value)

  // 1.5 查重：严禁同一个任务块被重复安排
  const isDuplicate = nextWeeks.some(w => w.events.some(e => 
    (e.status === 'suggested') && (Number(e.id || e.embedded_task_info?.id) === targetId)
  ))

  if (isDuplicate) {
    ElMessage.warning('该任务块已在当前计划中安排，不可重复添加')
    return
  }

  const { weekIndex, eventIndex } = findPreviewEventIndex(nextWeeks, targetSlot)
  if (weekIndex < 0) return

  const targetEvent = nextWeeks[weekIndex]!.events[eventIndex]
  // 仅保护非编辑态下的课程，编辑态允许覆盖
  if (targetEvent?.type === 'course' && !manualEditMode.value) {
    ElMessage.warning('该位置无法放置任务块')
    return
  }

  // 2. 构造建议项，确保所有字段就绪。
  const suggestedItem: SuggestedPreviewItem = {
    id: targetId,
    name: payload.content || '未命名任务',
    type: 'task',
    span: 2,
  }

  // 3. 应用建议。
  // 重新获取当前格子引用，确保操作的是克隆后的最新数据结构
  const finalTargetEvent = nextWeeks[weekIndex]!.events[eventIndex]
  const nextTargetEvent = buildPreviewEventWithSuggested(finalTargetEvent, targetSlot, suggestedItem)
  replacePreviewEventAtSlot(nextWeeks, targetSlot, nextTargetEvent)

  // 4. 更新全局状态
  const nextTaskClassIds = Array.from(new Set([...previewTaskClassIds.value, Number(payload.taskClassId)]))
  setPreviewState(nextWeeks, nextTaskClassIds)
  
  // 5. 将该任务从待删除列表中移除（如果存在）
  pendingDeleteIds.value = pendingDeleteIds.value.filter(id => Number(id) !== targetId)
  hasManualChanges.value = true
}

// handleRemoveEvent 从预览中移除某个已排/建议的任务块。
function handleRemoveEvent(payload: {
  id: number
  type: string
  status?: string
  week: number
  dayOfWeek: number
  order: number
}) {
  if (!manualEditMode.value) return

  const slot: SchedulePreviewSlotRef = {
    week: payload.week,
    dayOfWeek: payload.dayOfWeek,
    order: payload.order,
  }

  const nextWeeks = previewWeeks.value?.length
    ? clonePreviewWeeks(previewWeeks.value)
    : clonePreviewWeeks(liveWeeks.value)

  const { weekIndex, eventIndex } = findPreviewEventIndex(nextWeeks, slot)
  if (weekIndex < 0 || eventIndex < 0) return

  const targetEvent = nextWeeks[weekIndex]!.events[eventIndex]
  
  // 1. 如果是建议块（刚生成的），直接还原格子。
  if (targetEvent.status === 'suggested') {
    const nextEvent = buildPreviewEventWithSuggested(targetEvent, slot, null)
    replacePreviewEventAtSlot(nextWeeks, slot, nextEvent)
    setPreviewState(nextWeeks, previewTaskClassIds.value)
  } 
  // 2. 如果是正式块（原有的），加入待删除列表，并在预览中移除。
  else if (targetEvent.type === 'task') {
    pendingDeleteIds.value = Array.from(new Set([...pendingDeleteIds.value, targetEvent.id]))
    const nextEvent = buildEmptyPreviewEvent(slot)
    replacePreviewEventAtSlot(nextWeeks, slot, nextEvent)
    setPreviewState(nextWeeks, previewTaskClassIds.value)
  }

  hasManualChanges.value = true
}

async function loadTaskClasses() {
  taskClassLoading.value = true
  try {
    taskClasses.value = await getTaskClassList()
  } catch (error) {
    ElMessage.error(error instanceof Error ? error.message : '任务类列表加载失败')
  } finally {
    taskClassLoading.value = false
  }
}

async function loadWeekData(week?: number, options: { force?: boolean } = {}) {
  const normalizedWeek = typeof week === 'number' ? clampWeekIntoRange(week) : undefined

  // 1. 已缓存的周直接命中本地数据，避免左右来回切周时重复请求后端。
  // 2. force 只用于“应用/删除后刷新当前周”，此时必须跳过缓存回源拿最新结果。
  if (typeof normalizedWeek === 'number' && !options.force) {
    const cachedWeek = weekScheduleCache.value[normalizedWeek]
    if (cachedWeek) {
      syncLiveWeeksFromCache()
      currentWeek.value = normalizedWeek
      return
    }
  }

  // 1. 每次真实请求都分配一个递增序号。
  // 2. 只有“当前最新”的那次请求才允许修改 loading 与当前展示周。
  // 3. 旧请求如果晚回，只允许悄悄写缓存，不能把页面状态回滚。
  const requestSequence = ++weekRequestSequence
  activeWeekRequestSequence = requestSequence
  weekLoading.value = true

  try {
    const result = await getWeekSchedule(normalizedWeek)
    cacheWeekSchedules(result)

    if (result[0]?.week && weekBase.value === null) {
      weekBase.value = result[0].week
      baseMonday.value = startOfWeek(new Date())
    }

    if (requestSequence !== activeWeekRequestSequence) {
      return
    }

    if (typeof normalizedWeek === 'number') {
      currentWeek.value = normalizedWeek
    } else if (result[0]?.week) {
      currentWeek.value = result[0].week
    }
  } catch (error) {
    if (requestSequence !== activeWeekRequestSequence) {
      return
    }

    ElMessage.error(error instanceof Error ? error.message : '周日程加载失败')
  } finally {
    if (requestSequence === activeWeekRequestSequence) {
      weekLoading.value = false
    }
  }
}

async function loadTaskClassDetail(taskClassId: number) {
  taskClassDetailLoading.value = true
  try {
    expandedTaskClassDetail.value = await getTaskClassDetail(taskClassId)
  } catch (error) {
    expandedTaskClassDetail.value = null
    ElMessage.error(error instanceof Error ? error.message : '任务类详情加载失败')
  } finally {
    taskClassDetailLoading.value = false
  }
}

async function handleActivateTaskClass(taskClassId: number) {
  if (taskClassMultiSelectMode.value) {
    selectedTaskClassIds.value = selectedTaskClassIds.value.includes(taskClassId)
      ? selectedTaskClassIds.value.filter((id) => id !== taskClassId)
      : [...selectedTaskClassIds.value, taskClassId]
    return
  }

  scheduleSelectionMode.value = false
  selectedScheduleEventIds.value = []

  if (expandedTaskClassId.value === taskClassId) {
    expandedTaskClassId.value = null
    expandedTaskClassDetail.value = null
    return
  }

  expandedTaskClassId.value = taskClassId
  expandedTaskClassDetail.value = null
  await loadTaskClassDetail(taskClassId)
}

function handleToggleTaskClassMultiMode() {
  taskClassMultiSelectMode.value = !taskClassMultiSelectMode.value
  scheduleSelectionMode.value = false
  selectedScheduleEventIds.value = []

  if (taskClassMultiSelectMode.value) {
    selectedTaskClassIds.value = expandedTaskClassId.value ? [expandedTaskClassId.value] : []
    expandedTaskClassId.value = null
    expandedTaskClassDetail.value = null
    return
  }

  selectedTaskClassIds.value = []
}

async function handleDeleteTaskItem(taskItemId: number) {
  try {
    await deleteTaskClassItem(taskItemId)
    ElMessage.success('任务块已删除')
    await loadTaskClasses()
    if (expandedTaskClassId.value) {
      await loadTaskClassDetail(expandedTaskClassId.value)
    }
  } catch (error) {
    ElMessage.error(error instanceof Error ? error.message : '删除任务块失败')
  }
}

async function handleSmartPlanning() {
  const ids = effectiveSelectedTaskClassIds.value
  if (ids.length === 0) {
    ElMessage.info('请先选择任务类')
    return
  }

  if (hasManualChanges.value) {
    try {
      await ElMessageBox.confirm(
        '自动编排将覆盖您当前的手动调整内容，是否继续？',
        '提示',
        {
          confirmButtonText: '确定覆盖',
          cancelButtonText: '取消',
          type: 'warning',
        },
      )
    } catch {
      return
    }
  }

  smartPlanningLoading.value = true
  try {
    const plannedWeeks = ids.length === 1 ? await smartPlanning(ids[0]!) : await smartPlanningMulti(ids)
    setPreviewState(plannedWeeks, ids)

    if (plannedWeeks[0]?.week) {
      currentWeek.value = plannedWeeks[0].week
    }
    ElMessage.success(ids.length === 1 ? '已生成粗排预览' : '已生成批量粗排预览')
  } catch (error) {
    ElMessage.error(error instanceof Error ? error.message : '智能粗排失败')
  } finally {
    smartPlanningLoading.value = false
  }
}

function toggleScheduleSelectionMode() {
  scheduleSelectionMode.value = !scheduleSelectionMode.value
  selectedScheduleEventIds.value = []
}

function handleToggleScheduleEvent(eventId: number) {
  selectedScheduleEventIds.value = selectedScheduleEventIds.value.includes(eventId)
    ? selectedScheduleEventIds.value.filter((id) => id !== eventId)
    : [...selectedScheduleEventIds.value, eventId]
}

async function handleDeleteSelectedScheduleEvents() {
  const events = currentWeekData.value?.events.filter((event) => selectedScheduleEventIds.value.includes(event.id)) ?? []
  if (!events.length) {
    ElMessage.info('请先选择要解除安排的格子')
    return
  }

  deletingLoading.value = true
  try {
    await deleteScheduleEntries(events.map((event) => ({
      id: event.id,
      delete_course: event.type === 'course',
      delete_embedded_task: Boolean(event.embedded_task_info?.id),
    })))

    ElMessage.success('已完成解除安排')
    scheduleSelectionMode.value = false
    selectedScheduleEventIds.value = []
    clearPreviewState()
    await loadWeekData(currentWeek.value ?? undefined, { force: true })
    await loadTaskClasses()
    if (expandedTaskClassId.value) {
      await loadTaskClassDetail(expandedTaskClassId.value)
    }
  } catch (error) {
    ElMessage.error(error instanceof Error ? error.message : '解除安排失败')
  } finally {
    deletingLoading.value = false
  }
}

function buildApplyItemsFromPreview(weeks: ScheduleWeekData[]) {
  const items: ApplyBatchIntoScheduleItem[] = []

  for (const week of weeks) {
    for (const event of week.events) {
      if (event.status !== 'suggested') {
        continue
      }

      const startSection = (event.order - 1) * 2 + 1
      const taskItemId = event.type === 'course' && event.embedded_task_info?.id
        ? event.embedded_task_info.id
        : event.id

      items.push({
        task_item_id: taskItemId,
        week: week.week,
        day_of_week: event.day_of_week,
        start_section: startSection,
        end_section: startSection + Math.max(1, event.span || 2) - 1,
        embed_course_event_id: event.type === 'course' ? event.id : 0,
      })
    }
  }

  return items
}

// buildApplyGroupsFromPreview 负责把当前预览里的 suggested 任务按“所属任务类”重新分组。
//
// 处理步骤：
// 1. 单任务类预览直接复用现有单接口，不做额外解析。
// 2. 多任务类预览先拉各任务类详情，建立 task_item_id -> task_class_id 映射。
// 3. 再把预览中的建议项按任务类分桶，后续逐桶调用现有 applyBatchIntoSchedule。
//
// 说明：
// 1. 这里不改 preview JSON，只负责为“正式应用”准备提交载荷。
// 2. 如果发现某个 suggested 任务找不到所属任务类，直接报错，避免提交错桶导致脏数据。
async function buildApplyGroupsFromPreview(
  weeks: ScheduleWeekData[],
  taskClassIds: number[],
) {
  const items = buildApplyItemsFromPreview(weeks)
  const groupedItems = new Map<number, ApplyBatchIntoScheduleItem[]>()

  if (items.length === 0) {
    return groupedItems
  }

  if (taskClassIds.length === 1) {
    groupedItems.set(taskClassIds[0]!, items)
    return groupedItems
  }

  const ownerMap = new Map<number, number>()
  const details = await Promise.all(taskClassIds.map(async (taskClassId) => ({
    taskClassId,
    detail: await getTaskClassDetail(taskClassId),
  })))

  for (const { taskClassId, detail } of details) {
    for (const item of detail.items) {
      if (typeof item.id === 'number' && item.id > 0) {
        ownerMap.set(item.id, taskClassId)
      }
    }
  }

  for (const item of items) {
    const ownerTaskClassId = ownerMap.get(item.task_item_id)
    if (!ownerTaskClassId) {
      throw new Error(`未找到任务块 ${item.task_item_id} 对应的任务类，无法正式应用批量粗排结果`)
    }

    if (!groupedItems.has(ownerTaskClassId)) {
      groupedItems.set(ownerTaskClassId, [])
    }
    groupedItems.get(ownerTaskClassId)!.push(item)
  }

  return groupedItems
}

async function handleApplyPreview() {
  const hasAdditions = previewWeeks.value?.some(w => w.events.some(e => e.status === 'suggested'))
  const hasDeletions = pendingDeleteIds.value.length > 0

  if (!hasAdditions && !hasDeletions) {
    ElMessage.info('当前没有可提交的变更')
    return
  }

  applyingLoading.value = true
  try {
    // 1. 处理新增项
    if (hasAdditions) {
      const groupedItems = await buildApplyGroupsFromPreview(previewWeeks.value!, previewTaskClassIds.value)
      for (const [taskClassId, items] of groupedItems) {
        await applyBatchIntoSchedule(taskClassId, items)
      }
    }

    // 2. 处理删除项
    if (hasDeletions) {
      await deleteScheduleEntries(pendingDeleteIds.value.map(id => ({
        id,
        delete_course: false,
        delete_embedded_task: false,
      })))
    }

    ElMessage.success('日程安排已保存')
    manualEditMode.value = false
    clearPreviewState()
    await loadWeekData(currentWeek.value ?? undefined, { force: true })
    await loadTaskClasses()
    if (expandedTaskClassId.value) {
      await loadTaskClassDetail(expandedTaskClassId.value)
    }
  } catch (error) {
    ElMessage.error(error instanceof Error ? error.message : '保存失败')
  } finally {
    applyingLoading.value = false
  }
}

function handleCancelEdit() {
  clearPreviewState()
  manualEditMode.value = false
}

function toggleManualEditMode() {
  if (manualEditMode.value && isEditUnsaved.value) {
    ElMessageBox.confirm('当前有未保存的修改，退出将丢失这些调整，确定继续吗？', '提示', {
      type: 'warning',
    }).then(() => {
      handleCancelEdit()
    }).catch(() => {})
    return
  }
  
  manualEditMode.value = !manualEditMode.value
  if (manualEditMode.value) {
    // 进入编辑态时，如果没有预览数据，先克隆一份当前的正式数据，方便增量修改
    if (!previewWeeks.value) {
      setPreviewState(clonePreviewWeeks(liveWeeks.value), [])
    }
  } else {
    handleCancelEdit()
  }
}

async function handleOpenEditDialog() {
  if (expandedTaskClassId.value === null) return

  taskClassDetailLoading.value = true
  try {
    const detail = await getTaskClassDetail(expandedTaskClassId.value)
    editDialogInitialData.value = detail
    createDialogVisible.value = true
  } catch (error) {
    ElMessage.error(error instanceof Error ? error.message : '获取详情失败')
  } finally {
    taskClassDetailLoading.value = false
  }
}

async function handleTaskClassSubmit(payload: Parameters<typeof createTaskClass>[0]) {
  createDialogLoading.value = true
  try {
    if (editDialogInitialData.value && expandedTaskClassId.value !== null) {
      await updateTaskClass(expandedTaskClassId.value, payload)
      ElMessage.success('任务类已更新')
      // 更新详情缓存
      await loadTaskClassDetail(expandedTaskClassId.value)
    } else {
      await createTaskClass(payload)
      ElMessage.success('任务类已创建')
    }
    createDialogVisible.value = false
    await loadTaskClasses()
  } catch (error) {
    ElMessage.error(error instanceof Error ? error.message : '保存失败')
  } finally {
    createDialogLoading.value = false
  }
}

function goPreviousWeek() {
  if (currentWeek.value === null || currentWeek.value <= MIN_SCHEDULE_WEEK) {
    return
  }
  currentWeek.value -= 1
}

function goNextWeek() {
  if (currentWeek.value === null || currentWeek.value >= MAX_SCHEDULE_WEEK) {
    return
  }
  currentWeek.value += 1
}

watch(currentWeek, async (nextWeek, previousWeek) => {
  if (nextWeek === null || nextWeek === previousWeek) {
    return
  }

  const normalizedWeek = clampWeekIntoRange(nextWeek)
  if (normalizedWeek !== nextWeek) {
    currentWeek.value = normalizedWeek
    return
  }

  if (previewWeeks.value?.some((item) => item.week === nextWeek)) {
    return
  }

  await loadWeekData(normalizedWeek)
})

watch(currentWeek, (nextWeek) => {
  schedulePreviewRuntimeState.currentWeek = nextWeek
}, { immediate: true })

watch(resolvedCurrentWeekData, (nextWeekData) => {
  // 1. 只有真正解析到“当前目标周”的数据时，才更新稳定展示态。
  // 2. 当目标周仍在请求中时，这里保持旧值不动，避免课表闪回到别的周。
  if (nextWeekData) {
    lastStableWeekData.value = nextWeekData
  }
}, { immediate: true })

onMounted(async () => {
  await Promise.all([loadTaskClasses(), loadWeekData(currentWeek.value ?? undefined)])
})
</script>

<template>
  <section class="schedule-shell">
    <header class="schedule-topbar">
          <div class="schedule-topbar__brand">
            <span class="schedule-topbar__brand-icon" aria-hidden="true">
              <svg width="22" height="22" viewBox="0 0 22 22" fill="none" xmlns="http://www.w3.org/2000/svg">
                <rect x="2" y="2" width="18" height="18" rx="5" fill="currentColor" />
                <path d="M7 8H15V9.6H7V8ZM7 11H15V12.6H7V11ZM7 14H12V15.6H7V14Z" fill="white" />
              </svg>
            </span>
            <strong>日程安排中心</strong>
          </div>

          <div class="schedule-topbar__meta">
            <strong>{{ currentTermLabel }}</strong>
            <span>当前日期: {{ formatHeaderDate(new Date()) }}</span>
          </div>
        </header>

        <div class="schedule-main">
          <TaskClassSidebar
            :task-classes="taskClasses"
            :loading="taskClassLoading"
            :detail-loading="taskClassDetailLoading"
            :expanded-task-class-id="expandedTaskClassId"
            :expanded-task-class-detail="augmentedTaskClassDetail"
            :selected-task-class-ids="effectiveSelectedTaskClassIds"
            :task-class-multi-select-mode="taskClassMultiSelectMode"
            :manual-edit-mode="manualEditMode"
            @activate="handleActivateTaskClass"
            @toggle-multi-mode="handleToggleTaskClassMultiMode"
            @create="() => { editDialogInitialData = null; createDialogVisible = true }"
            @delete-item="handleDeleteTaskItem"
          />

          <section class="schedule-board-wrap">
            <div class="schedule-board__toolbar">
              <div class="schedule-board__toolbar-left">
                <button
                  type="button"
                  class="schedule-board__toolbar-button schedule-board__toolbar-button--ghost"
                  :class="{ 'schedule-board__toolbar-button--active': manualEditMode }"
                  @click="toggleManualEditMode"
                >
                  {{ manualEditMode ? '退出编排' : '自定义编排' }}
                </button>

                <button
                  v-if="showDeleteModeButton && !manualEditMode"
                  type="button"
                  class="schedule-board__toolbar-button schedule-board__toolbar-button--ghost"
                  @click="toggleScheduleSelectionMode"
                >
                  多选解除
                </button>

                <button
                  v-else-if="scheduleSelectionMode && !manualEditMode"
                  type="button"
                  class="schedule-board__toolbar-button schedule-board__toolbar-button--ghost"
                  @click="toggleScheduleSelectionMode"
                >
                  取消多选
                </button>

                <button
                  v-if="!manualEditMode && !scheduleSelectionMode"
                  type="button"
                  class="schedule-board__toolbar-button schedule-board__toolbar-button--ghost"
                  @click="courseImportDialogVisible = true"
                >
                  导入课表
                </button>

                <button
                  v-if="expandedTaskClassId !== null && !taskClassMultiSelectMode && !manualEditMode && !scheduleSelectionMode"
                  type="button"
                  class="schedule-board__toolbar-button schedule-board__toolbar-button--ghost"
                  @click="handleOpenEditDialog"
                >
                  编辑任务类
                </button>

                <button
                  v-if="showSmartPlanningButton"
                  type="button"
                  class="schedule-board__toolbar-button schedule-board__toolbar-button--primary"
                  :disabled="smartPlanningLoading"
                  @click="handleSmartPlanning"
                >
                  {{ smartPlanningLoading ? '编排中…' : effectiveSelectedTaskClassIds.length > 1 ? '智能批量编排' : '智能一键编排' }}
                </button>
              </div>

              <div class="schedule-board__toolbar-right">
                <button
                  type="button"
                  class="schedule-board__toolbar-button schedule-board__toolbar-button--ghost"
                  :disabled="!canGoPreviousWeek"
                  @click="goPreviousWeek"
                >
                  上一周
                </button>
                <button
                  type="button"
                  class="schedule-board__toolbar-button schedule-board__toolbar-button--primary"
                  :disabled="!canGoNextWeek"
                  @click="goNextWeek"
                >
                  下一周
                </button>
              </div>
            </div>

            <WeekPlanningBoard
              :week-label="weekLabel"
              :week-headers="weekHeaders"
              :week-data="currentWeekData"
              :schedule-selection-mode="scheduleSelectionMode"
              :selected-schedule-event-ids="selectedScheduleEventIds"
              :preview-drag-enabled="hasPendingPreview"
              :manual-edit-mode="manualEditMode"
              @toggle-schedule-event="handleToggleScheduleEvent"
              @move-preview-event="handleMovePreviewEvent"
              @drop-task-item="handleDropTaskItem"
              @remove-event="handleRemoveEvent"
            />

            <div v-if="showApplyButton || scheduleSelectionMode || manualEditMode" class="schedule-board__footer">
              <button
                v-if="manualEditMode"
                type="button"
                class="schedule-board__footer-button schedule-board__footer-button--ghost"
                @click="handleCancelEdit"
              >
                取消修改
              </button>
              <button
                v-if="showApplyButton"
                type="button"
                class="schedule-board__footer-button schedule-board__footer-button--primary"
                :disabled="applyingLoading"
                @click="handleApplyPreview"
              >
                {{ applyingLoading ? '保存中…' : '保存日程' }}
              </button>

              <button
                v-if="scheduleSelectionMode"
                type="button"
                class="schedule-board__footer-button schedule-board__footer-button--danger"
                :disabled="deletingLoading || selectedScheduleEventIds.length === 0"
                @click="handleDeleteSelectedScheduleEvents"
              >
                {{ deletingLoading ? '处理中…' : '解除安排/删除课程' }}
              </button>
            </div>
          </section>
        </div>
      </section>

      <CreateTaskClassDialog
        v-model="createDialogVisible"
        :loading="createDialogLoading"
        :initial-data="editDialogInitialData"
        @submit="handleTaskClassSubmit"
      />

      <CourseImageImportDialog
        v-model="courseImportDialogVisible"
        @success="loadWeekData(currentWeek ?? undefined, { force: true })"
      />
</template>

<style scoped>
.schedule-shell {
  height: 100%;
  border-radius: 20px;
  background: #ffffff;
  border: 1px solid rgba(15, 23, 42, 0.05);
  box-shadow: 0 4px 15px rgba(15, 23, 42, 0.02);
  overflow: hidden;
  display: grid;
  grid-template-rows: auto minmax(0, 1fr);
  min-width: 0;
  min-height: 0;
}

.schedule-topbar {
  padding: 14px 24px;
  border-bottom: 1px solid rgba(218, 227, 239, 0.92);
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 20px;
  min-width: 0;
}

.schedule-topbar__brand {
  display: inline-flex;
  align-items: center;
  gap: 16px;
  min-width: 0;
}

.schedule-topbar__brand-icon {
  width: 42px;
  height: 42px;
  border-radius: 12px;
  background: #3b82f6;
  color: #ffffff;
  display: inline-flex;
  align-items: center;
  justify-content: center;
  box-shadow: 0 4px 12px rgba(59, 130, 246, 0.25);
}

.schedule-topbar__brand strong {
  color: #19263d;
  font-size: 20px;
  font-weight: 800;
  min-width: 0;
}

.schedule-topbar__meta {
  display: grid;
  justify-items: end;
  gap: 4px;
  color: #64748b;
  min-width: 0;
  text-align: right;
}

.schedule-topbar__meta strong {
  color: #344055;
  font-size: 14px;
}

.schedule-topbar__meta span {
  font-size: 12px;
}

.schedule-main {
  min-width: 0;
  min-height: 0;
  display: grid;
  grid-template-columns: clamp(280px, 26vw, 380px) minmax(0, 1fr);
  overflow: hidden;
}

.schedule-board-wrap {
  min-width: 0;
  min-height: 0;
  padding: 18px 22px 22px;
  display: grid;
  grid-template-rows: auto minmax(0, 1fr) auto;
  gap: 14px;
  overflow: hidden;
}

.schedule-board__toolbar {
  display: flex;
  justify-content: space-between;
  gap: 16px;
  align-items: center;
  min-width: 0;
  flex-wrap: wrap;
}

.schedule-board__toolbar-left,
.schedule-board__toolbar-right {
  display: flex;
  gap: 10px;
  align-items: center;
  min-width: 0;
  flex-wrap: wrap;
}

.schedule-board__toolbar-right {
  margin-left: auto;
}

.schedule-board__toolbar-button,
.schedule-board__footer-button {
  height: 38px;
  border-radius: 10px;
  border: 1px solid transparent;
  padding: 0 16px;
  font-size: 13px;
  font-weight: 700;
  cursor: pointer;
  transition: all 0.2s ease;
}

.schedule-board__toolbar-button--primary,
.schedule-board__footer-button--primary {
  background: #3b82f6;
  color: #ffffff;
  box-shadow: 0 4px 12px rgba(59, 130, 246, 0.25);
}

.schedule-board__toolbar-button--primary:hover,
.schedule-board__footer-button--primary:hover {
  background: #2563eb;
  transform: translateY(-1px);
  box-shadow: 0 6px 16px rgba(37, 99, 235, 0.3);
}

.schedule-board__toolbar-button--ghost {
  border-color: #e2e8f0;
  background: #ffffff;
  color: #475569;
}

.schedule-board__toolbar-button--ghost:hover,
.schedule-board__toolbar-button--active {
  border-color: #3b82f6;
  background: #eff6ff;
  color: #3b82f6;
}

.schedule-board__footer {
  display: flex;
  justify-content: flex-end;
  gap: 12px;
}

.schedule-board__footer-button--danger {
  background: #ef4444;
  color: #ffffff;
  box-shadow: 0 4px 12px rgba(239, 68, 68, 0.25);
}

.schedule-board__footer-button--danger:hover {
  background: #dc2626;
  transform: translateY(-1px);
}

.schedule-board__footer-button--danger:disabled,
.schedule-board__footer-button--primary:disabled,
.schedule-board__toolbar-button:disabled {
  opacity: 0.46;
  cursor: not-allowed;
}

@media (max-width: 1520px) {
  .schedule-main {
    grid-template-columns: clamp(260px, 24vw, 332px) minmax(0, 1fr);
  }

  .schedule-board-wrap {
    padding: 16px 18px 18px;
  }
}

@media (max-width: 1380px) {
  .schedule-main {
    grid-template-columns: 1fr;
  }

  .schedule-topbar {
    flex-wrap: wrap;
  }

  .schedule-topbar__meta {
    justify-items: start;
    text-align: left;
  }
}

@media (max-height: 900px) {
  .schedule-page {
    padding: 8px;
  }

  .schedule-layout {
    height: calc(100vh - 16px);
  }

  .schedule-topbar {
    padding: 12px 18px;
  }

  .schedule-topbar__brand {
    gap: 12px;
  }

  .schedule-topbar__brand-icon {
    width: 38px;
    height: 38px;
    border-radius: 12px;
  }

  .schedule-topbar__brand strong {
    font-size: 18px;
  }

  .schedule-board-wrap {
    padding-top: 14px;
    padding-bottom: 16px;
    gap: 10px;
  }

  .schedule-board__toolbar-button,
  .schedule-board__footer-button {
    height: 34px;
    padding: 0 14px;
  }
}

@media (max-height: 820px) {
  .schedule-topbar {
    padding-top: 10px;
    padding-bottom: 10px;
  }

  .schedule-topbar__meta {
    gap: 4px;
  }

  .schedule-board-wrap {
    padding-left: 14px;
    padding-right: 14px;
    padding-bottom: 14px;
  }
}

@media (max-width: 1440px) { .schedule-main { grid-template-columns: 320px 1fr; } }
</style>
