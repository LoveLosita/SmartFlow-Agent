<script setup lang="ts">
import { computed, ref } from 'vue'

import type { ScheduleWeekData, ScheduleWeekEvent } from '@/types/schedule'

interface WeekDayHeader {
  dayOfWeek: number
  label: string
  dateLabel: string
}

interface SectionSlot {
  order: number
  title: string
  timeRange: string
}

interface PreviewMovePayload {
  week: number
  sourceDayOfWeek: number
  sourceOrder: number
  targetDayOfWeek: number
  targetOrder: number
}

const props = defineProps<{
  weekLabel: string
  weekHeaders: WeekDayHeader[]
  weekData: ScheduleWeekData | null
  scheduleSelectionMode: boolean
  selectedScheduleEventIds: number[]
  previewDragEnabled: boolean
  manualEditMode: boolean
}>()

const emit = defineEmits<{
  toggleScheduleEvent: [eventId: number]
  movePreviewEvent: [payload: PreviewMovePayload]
  dropTaskItem: [payload: { id: number; content: string; taskClassId: number; week: number; dayOfWeek: number; order: number }]
  removeEvent: [payload: { id: number; type: string; status?: string; week: number; dayOfWeek: number; order: number }]
}>()

const draggingCellKey = ref<string | null>(null)
const dragOverCellKey = ref<string | null>(null)
const isDraggingOverDeleteZone = ref(false)
const isExternalDragging = ref(false)

const sectionSlots: SectionSlot[] = [
  { order: 1, title: '1-2', timeRange: '08:00\n09:40' },
  { order: 2, title: '3-4', timeRange: '10:15\n11:55' },
  { order: 3, title: '5-6', timeRange: '14:00\n15:40' },
  { order: 4, title: '7-8', timeRange: '16:15\n17:55' },
  { order: 5, title: '9-10', timeRange: '19:00\n20:40' },
  { order: 6, title: '11-12', timeRange: '20:50\n22:30' },
]

const eventLookup = computed(() => {
  const map = new Map<string, ScheduleWeekEvent>()

  for (const event of props.weekData?.events ?? []) {
    map.set(`${event.day_of_week}-${event.order}`, event)
  }

  return map
})

function resolveEvent(dayOfWeek: number, order: number) {
  return eventLookup.value.get(`${dayOfWeek}-${order}`)
}

function isSelected(eventId: number) {
  return props.selectedScheduleEventIds.includes(eventId)
}

function hasEmbeddedTask(event?: ScheduleWeekEvent) {
  const taskId = Number(event?.embedded_task_info?.id)
  return Boolean(
    event &&
    event.type === 'course' &&
    !isNaN(taskId) &&
    taskId > 0
  )
}

function resolveEventTone(event?: ScheduleWeekEvent) {
  if (!event || event.type === 'empty') {
    return 'empty'
  }

  if (hasEmbeddedTask(event)) {
    return 'course-embedded'
  }

  if (event.type === 'course') {
    return 'course'
  }

  const toneByOrder: Record<number, string> = {
    1: 'amber',
    2: 'mint',
    3: 'emerald',
    4: 'rose',
    5: 'violet',
    6: 'sky',
  }

  return toneByOrder[event.order] ?? 'task'
}

function resolveCellTitle(event?: ScheduleWeekEvent) {
  if (!event || event.type === 'empty') {
    return '空'
  }
  return event.name
}

function resolveCellMeta(event?: ScheduleWeekEvent) {
  if (!event || event.type === 'empty') {
    return ''
  }
  return event.location || '未定'
}

function resolveEmbeddedTaskName(event?: ScheduleWeekEvent) {
  if (!hasEmbeddedTask(event)) {
    return ''
  }

  return event!.embedded_task_info.name
}

// isSuggestedPreviewEvent 负责判断当前格子是否允许作为“拖拽源”。
//
// 职责边界：
// 1. 这里只判断前端交互条件，不负责真正改写 preview JSON。
// 2. 只有 preview 模式下的 suggested 条目才允许拖拽，正式课表与普通课程保持只读。
function isSuggestedPreviewEvent(event?: ScheduleWeekEvent) {
  return Boolean(
    (props.previewDragEnabled || props.manualEditMode) &&
    !props.scheduleSelectionMode &&
    event &&
    event.status === 'suggested',
  )
}

function isEmbeddedSuggestedPreviewEvent(event?: ScheduleWeekEvent) {
  return Boolean(
    isSuggestedPreviewEvent(event) &&
    event &&
    event.type === 'course' &&
    hasEmbeddedTask(event),
  )
}

function isWholeCellDraggable(event?: ScheduleWeekEvent) {
  if (props.scheduleSelectionMode || !event) return false

  // 1. 建议块可拖拽
  if (event.status === 'suggested' && event.type !== 'course') return true
  
  // 2. 已安排的任务块，仅在手动编辑模式下可拖拽（用于删除/移动）
  if (props.manualEditMode && event.type === 'task') return true

  return false
}

// canDropPreviewEvent 负责判断当前格子是否允许作为“拖拽目标”。
//
// 设计说明：
// 1. 空白格允许放置 suggested 任务。
// 2. 课程格允许接收 suggested 任务，父组件会把它转换成“嵌入课程”的预览结构。
// 3. suggested 格本身也允许作为目标，用于交换两个建议任务的位置。
function canDropPreviewEvent(event?: ScheduleWeekEvent) {
  if (!props.manualEditMode && !props.previewDragEnabled) {
    return false
  }

  if (props.scheduleSelectionMode) {
    return false
  }

  if (!event || event.type === 'empty') {
    return true
  }

  if (event.status === 'suggested') {
    return true
  }

  return event.type === 'course'
}

function buildCellKey(dayOfWeek: number, order: number) {
  return `${dayOfWeek}-${order}`
}

function handlePreviewDragStart(dayOfWeek: number, order: number, dragEvent: DragEvent) {
  const event = resolveEvent(dayOfWeek, order)
  if (!isWholeCellDraggable(event) && !isEmbeddedSuggestedPreviewEvent(event)) {
    dragEvent.preventDefault()
    return
  }

  draggingCellKey.value = buildCellKey(dayOfWeek, order)
  dragOverCellKey.value = null
  isExternalDragging.value = false

  dragEvent.dataTransfer?.setData(
    'application/json',
    JSON.stringify({
      week: props.weekData?.week ?? 0,
      sourceDayOfWeek: dayOfWeek,
      sourceOrder: order,
    }),
  )
  if (dragEvent.dataTransfer) {
    dragEvent.dataTransfer.effectAllowed = 'move'
  }
}

function handlePreviewDragOver(dayOfWeek: number, order: number, dragEvent: DragEvent) {
  const cellKey = buildCellKey(dayOfWeek, order)
  if (cellKey === draggingCellKey.value || !canDropPreviewEvent(resolveEvent(dayOfWeek, order))) {
    return
  }

  dragEvent.preventDefault()
  dragOverCellKey.value = cellKey
  isDraggingOverDeleteZone.value = false
  if (dragEvent.dataTransfer) {
    dragEvent.dataTransfer.dropEffect = 'move'
  }
}

function handleExternalDragOver(dragEvent: DragEvent) {
  if (dragEvent.dataTransfer?.types.includes('application/task-item')) {
    dragEvent.preventDefault()
    isExternalDragging.value = true
  }
}

function handlePreviewDrop(dayOfWeek: number, order: number, dragEvent: DragEvent) {
  const cellKey = buildCellKey(dayOfWeek, order)
  
  // 1. 处理从侧边栏拖入的任务块
  const taskItemData = dragEvent.dataTransfer?.getData('application/task-item')
  if (taskItemData) {
    try {
      const payload = JSON.parse(taskItemData)
      // 强制转换 ID 为数字，确保后续匹配逻辑一致
      if (payload.id) payload.id = Number(payload.id)
      
      dragEvent.preventDefault()
      emit('dropTaskItem', {
        ...payload,
        week: props.weekData?.week ?? 0,
        dayOfWeek,
        order,
      })
    } finally {
      draggingCellKey.value = null
      dragOverCellKey.value = null
      isExternalDragging.value = false
    }
    return
  }

  // 2. 处理内部拖拽移动
  const payloadText = dragEvent.dataTransfer?.getData('application/json')
  if (!payloadText || cellKey === draggingCellKey.value || !canDropPreviewEvent(resolveEvent(dayOfWeek, order))) {
    draggingCellKey.value = null
    dragOverCellKey.value = null
    return
  }

  try {
    const payload = JSON.parse(payloadText) as Partial<PreviewMovePayload>
    if (
      typeof payload.week !== 'number' ||
      typeof payload.sourceDayOfWeek !== 'number' ||
      typeof payload.sourceOrder !== 'number'
    ) {
      return
    }

    dragEvent.preventDefault()
    emit('movePreviewEvent', {
      week: payload.week,
      sourceDayOfWeek: payload.sourceDayOfWeek,
      sourceOrder: payload.sourceOrder,
      targetDayOfWeek: dayOfWeek,
      targetOrder: order,
    })
  } finally {
    draggingCellKey.value = null
    dragOverCellKey.value = null
  }
}

function handleDragOverDeleteZone(dragEvent: DragEvent) {
  if (draggingCellKey.value) {
    dragEvent.preventDefault()
    isDraggingOverDeleteZone.value = true
    dragOverCellKey.value = null
  }
}

function handleDropOnDeleteZone(dragEvent: DragEvent) {
  if (!draggingCellKey.value) return
  
  const payloadText = dragEvent.dataTransfer?.getData('application/json')
  if (!payloadText) return

  try {
    const payload = JSON.parse(payloadText)
    const event = resolveEvent(payload.sourceDayOfWeek, payload.sourceOrder)
    if (event) {
      dragEvent.preventDefault()
      emit('removeEvent', {
        id: event.id,
        type: event.type,
        status: event.status,
        week: payload.week,
        dayOfWeek: payload.sourceDayOfWeek,
        order: payload.sourceOrder,
      })
    }
  } finally {
    draggingCellKey.value = null
    isDraggingOverDeleteZone.value = false
  }
}

function handlePreviewDragEnd() {
  draggingCellKey.value = null
  dragOverCellKey.value = null
  isDraggingOverDeleteZone.value = false
  isExternalDragging.value = false
}
</script>

<template>
  <section class="planning-board">
    <header class="planning-board__header">
      <strong>{{ weekLabel }}</strong>
    </header>

    <div class="planning-board__grid" @dragover="handleExternalDragOver">
      <div class="planning-board__corner" />

      <div v-for="header in weekHeaders" :key="header.dayOfWeek" class="planning-board__day-head">
        <span>{{ header.label }}</span>
        <small>{{ header.dateLabel }}</small>
      </div>

      <template v-for="slot in sectionSlots" :key="slot.order">
        <div class="planning-board__time-cell">
          <strong>{{ slot.title }}</strong>
          <small>{{ slot.timeRange }}</small>
        </div>

        <article
          v-for="header in weekHeaders"
          :key="`${weekData?.week ?? 0}-${header.dayOfWeek}-${slot.order}`"
          class="planning-board__cell"
          :class="[
            `planning-board__cell--${resolveEventTone(resolveEvent(header.dayOfWeek, slot.order))}`,
            {
              'board-item-pop': resolveEvent(header.dayOfWeek, slot.order)?.type !== 'empty',
              'planning-board__cell--selectable': scheduleSelectionMode && resolveEvent(header.dayOfWeek, slot.order)?.type !== 'empty',
              'planning-board__cell--selected': resolveEvent(header.dayOfWeek, slot.order) && isSelected(resolveEvent(header.dayOfWeek, slot.order)!.id),
              'planning-board__cell--draggable': isWholeCellDraggable(resolveEvent(header.dayOfWeek, slot.order)),
              'planning-board__cell--suggested': isSuggestedPreviewEvent(resolveEvent(header.dayOfWeek, slot.order)),
              'planning-board__cell--dragging': draggingCellKey === buildCellKey(header.dayOfWeek, slot.order),
              'planning-board__cell--dragover': dragOverCellKey === buildCellKey(header.dayOfWeek, slot.order),
            },
          ]"
          :style="{ '--anim-delay': (header.dayOfWeek - 1) * 0.035 + (slot.order - 1) * 0.045 + 's' }"
          :draggable="isWholeCellDraggable(resolveEvent(header.dayOfWeek, slot.order))"
          @dragstart="handlePreviewDragStart(header.dayOfWeek, slot.order, $event)"
          @dragover="handlePreviewDragOver(header.dayOfWeek, slot.order, $event)"
          @drop="handlePreviewDrop(header.dayOfWeek, slot.order, $event)"
          @dragend="handlePreviewDragEnd"
        >
          <button
            v-if="(scheduleSelectionMode || manualEditMode) && resolveEvent(header.dayOfWeek, slot.order)?.type !== 'empty'"
            type="button"
            class="planning-board__checkbox"
            :class="{ 
              'planning-board__checkbox--active': isSelected(resolveEvent(header.dayOfWeek, slot.order)!.id),
              'planning-board__checkbox--hidden': manualEditMode && resolveEvent(header.dayOfWeek, slot.order)?.status !== 'suggested'
            }"
            @click="emit('toggleScheduleEvent', resolveEvent(header.dayOfWeek, slot.order)!.id)"
          />

          <template v-if="resolveEvent(header.dayOfWeek, slot.order)">
            <div
              v-if="hasEmbeddedTask(resolveEvent(header.dayOfWeek, slot.order))"
              class="planning-board__embedded-shell"
            >
              <div class="planning-board__embedded-course">
                <strong>{{ resolveCellTitle(resolveEvent(header.dayOfWeek, slot.order)) }}</strong>
              </div>

              <div class="planning-board__embedded-task">
                <strong
                  class="planning-board__embedded-task-dragger"
                  :class="{
                    'planning-board__embedded-task-dragger--active': isEmbeddedSuggestedPreviewEvent(resolveEvent(header.dayOfWeek, slot.order)),
                  }"
                  :draggable="isEmbeddedSuggestedPreviewEvent(resolveEvent(header.dayOfWeek, slot.order))"
                  @dragstart.stop="handlePreviewDragStart(header.dayOfWeek, slot.order, $event)"
                  @dragend.stop="handlePreviewDragEnd"
                >
                  {{ resolveEmbeddedTaskName(resolveEvent(header.dayOfWeek, slot.order)) }}
                </strong>
              </div>
            </div>

            <div
              v-else-if="resolveEvent(header.dayOfWeek, slot.order)?.type === 'task' || resolveEvent(header.dayOfWeek, slot.order)?.status === 'suggested'"
              class="planning-board__cell-main"
            >
              <strong>{{ resolveCellTitle(resolveEvent(header.dayOfWeek, slot.order)) }}</strong>
              <span>{{ resolveCellMeta(resolveEvent(header.dayOfWeek, slot.order)) }}</span>
            </div>

            <div
              v-else
              class="planning-board__cell-main"
            >
              <strong>{{ resolveCellTitle(resolveEvent(header.dayOfWeek, slot.order)) }}</strong>
              <span>{{ resolveCellMeta(resolveEvent(header.dayOfWeek, slot.order)) }}</span>
            </div>
          </template>
        </article>
      </template>
    </div>

    <!-- 悬浮删除热区 -->
    <transition name="delete-zone">
      <div 
        v-if="draggingCellKey && manualEditMode" 
        class="planning-board__delete-zone"
        :class="{ 'planning-board__delete-zone--active': isDraggingOverDeleteZone }"
        @dragover="handleDragOverDeleteZone"
        @dragleave="isDraggingOverDeleteZone = false"
        @drop="handleDropOnDeleteZone"
      >
        <span class="delete-zone-icon">
          <svg width="24" height="24" viewBox="0 0 24 24" fill="none" xmlns="http://www.w3.org/2000/svg">
            <path d="M19 7L18.1327 19.1425C18.0579 20.1891 17.187 21 16.1378 21H7.86224C6.81296 21 5.94208 20.1891 5.86732 19.1425L5 7M10 11V17M14 11V17M15 7V4C15 3.44772 14.5523 3 14 3H10C9.44772 3 9 3.44772 9 4V7M4 7H20" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"/>
          </svg>
        </span>
        <strong>在此处松开以解除安排</strong>
      </div>
    </transition>
  </section>
</template>

<style scoped>
.planning-board {
  --planning-grid-padding-x: 20px;
  --planning-grid-padding-y: 20px;
  --planning-grid-gap-x: 10px;
  --planning-grid-gap-y: 10px;
  --planning-time-column-width: 68px;
  --planning-day-column-min: 96px;
  --planning-cell-height: clamp(72px, 9.2vh, 112px);
  min-width: 0;
  min-height: 0;
  border-radius: 20px;
  border: 1px solid rgba(15, 23, 42, 0.05);
  background: #ffffff;
  display: grid;
  grid-template-rows: auto minmax(0, 1fr);
  overflow: hidden;
}

.planning-board__header {
  padding: 18px 24px 16px;
  border-bottom: 1px solid rgba(15, 23, 42, 0.05);
  color: #0f172a;
  font-size: 16px;
  font-weight: 700;
}

.planning-board__grid {
  min-width: 0;
  min-height: 0;
  display: grid;
  grid-template-columns: var(--planning-time-column-width) repeat(7, minmax(var(--planning-day-column-min), 1fr));
  gap: var(--planning-grid-gap-y) var(--planning-grid-gap-x);
  padding: var(--planning-grid-padding-y) var(--planning-grid-padding-x) 24px;
  overflow: auto;
  scrollbar-gutter: stable both-edges;
}

.planning-board__corner {
  min-height: 1px;
}

.planning-board__day-head {
  display: grid;
  justify-items: center;
  gap: 4px;
  color: #64748b;
}

.planning-board__day-head span {
  font-size: 14px;
  font-weight: 700;
  letter-spacing: 0.08em;
}

.planning-board__day-head small {
  font-size: 12px;
}

.planning-board__time-cell {
  min-height: var(--planning-cell-height);
  display: grid;
  align-content: center;
  justify-items: end;
  color: #94a3b8;
  padding-right: 12px;
}

.planning-board__time-cell strong {
  font-size: 14px;
  color: #64748b;
  font-weight: 700;
}

.planning-board__time-cell small {
  white-space: pre-line;
  text-align: right;
  line-height: 1.35;
  font-size: 11px;
}

.planning-board__cell {
  position: relative;
  min-height: var(--planning-cell-height);
  border-radius: 14px;
  border: 1px solid transparent;
  padding: 14px;
  display: flex;
  align-items: center;
  justify-content: center;
  text-align: center;
  overflow: hidden;
  transition: transform 0.2s, box-shadow 0.2s;
}

.planning-board__cell-main {
  display: grid;
  gap: 10px;
  min-width: 0;
}

.planning-board__cell-main strong {
  color: #334155;
  font-size: 14px;
  line-height: 1.4;
  font-weight: 700;
  min-width: 0;
  overflow-wrap: anywhere;
}

.planning-board__cell-main span {
  color: #64748b;
  font-size: 12px;
  min-width: 0;
  overflow-wrap: anywhere;
}

.planning-board__cell--course {
  background: #f0f7ff;
}

.planning-board__cell--course-embedded {
  background: #f0f7ff;
  align-items: stretch;
  padding: 8px;
}

.planning-board__cell--suggested {
  outline: 2px dashed #3b82f6;
  outline-offset: -2px;
  background: #ffffff !important;
  box-shadow: inset 0 0 0 100px #eff6ffaa;
}

.planning-board__cell--course-embedded.planning-board__cell--suggested {
  outline-color: #0284c7;
  background: #f0f9ff !important;
}

.planning-board__cell--course .planning-board__cell-main strong,
.planning-board__cell--course .planning-board__cell-main span {
  color: #0369a1;
}

.planning-board__embedded-shell {
  display: flex;
  flex-direction: column;
  gap: 4px;
  width: 100%;
  height: 100%;
  min-height: 0;
}

.planning-board__embedded-course {
  padding: 2px 4px;
  font-size: 13px;
  color: #0369a1;
  font-weight: 800;
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
  text-align: center;
}

.planning-board__embedded-task {
  flex: 1;
  background: #ffffff;
  border-radius: 12px;
  box-shadow: 0 4px 12px rgba(15, 23, 42, 0.08);
  border: 1px solid rgba(15, 23, 42, 0.04);
  display: flex;
  align-items: center;
  justify-content: center;
  padding: 6px;
  min-height: 0;
  transition: all 0.2s cubic-bezier(0.4, 0, 0.2, 1);
}

.planning-board__embedded-task:hover {
  transform: translateY(-1px);
  box-shadow: 0 6px 16px rgba(15, 23, 42, 0.12);
  border-color: #3b82f6;
}

.planning-board__embedded-task-dragger {
  font-size: 12px;
  color: #334155;
  font-weight: 700;
  text-align: center;
  padding: 2px 4px;
  cursor: grab;
  width: 100%;
  overflow: hidden;
  text-overflow: ellipsis;
  display: -webkit-box;
  -webkit-line-clamp: 2;
  -webkit-box-orient: vertical;
}

.planning-board__embedded-task-dragger--active {
  color: #3b82f6;
}

.planning-board__embedded-task-dragger--active {
  cursor: grab;
}

.planning-board__cell--amber {
  background: #fef3c7;
}

.planning-board__cell--amber .planning-board__cell-main strong,
.planning-board__cell--amber .planning-board__cell-main span {
  color: #d97706;
}

.planning-board__cell--mint {
  background: #dcfce7;
}

.planning-board__cell--mint .planning-board__cell-main strong,
.planning-board__cell--mint .planning-board__cell-main span,
.planning-board__cell--emerald .planning-board__cell-main strong,
.planning-board__cell--emerald .planning-board__cell-main span {
  color: #059669;
}

.planning-board__cell--emerald {
  background: #d1fae5;
}

.planning-board__cell--rose {
  background: #fee2e2;
}

.planning-board__cell--rose .planning-board__cell-main strong,
.planning-board__cell--rose .planning-board__cell-main span {
  color: #e11d48;
}

.planning-board__cell--violet {
  background: #f3e8ff;
}

.planning-board__cell--violet .planning-board__cell-main strong,
.planning-board__cell--violet .planning-board__cell-main span {
  color: #7c3aed;
}

.planning-board__cell--sky {
  background: #e0f2fe;
}

.planning-board__cell--sky .planning-board__cell-main strong,
.planning-board__cell--sky .planning-board__cell-main span {
  color: #0284c7;
}

.planning-board__cell--empty {
  background: #f8fafc;
  border-color: rgba(15, 23, 42, 0.05);
}

.planning-board__cell--selectable {
  cursor: pointer;
}

.planning-board__cell--selected {
  box-shadow: inset 0 0 0 2px #3b82f6;
}

.planning-board__cell--draggable {
  cursor: grab;
}
.planning-board__cell--draggable:hover {
  transform: translateY(-2px);
  box-shadow: 0 4px 12px rgba(15, 23, 42, 0.06);
}

.planning-board__cell--dragging {
  opacity: 0.42;
}

.planning-board__cell--dragover {
  box-shadow:
    inset 0 0 0 2px #2563eb,
    0 0 0 4px rgba(59, 130, 246, 0.15);
}

.planning-board__checkbox {
  position: absolute;
  top: 16px;
  right: 16px;
  width: 20px;
  height: 20px;
  border-radius: 6px;
  border: 1px solid rgba(118, 133, 160, 0.46);
  background: rgba(255, 255, 255, 0.92);
  cursor: pointer;
}

.planning-board__checkbox--active {
  border-color: #3b82f6;
  background: #3b82f6;
  box-shadow: inset 0 0 0 3px #ffffff;
}

.planning-board__checkbox--hidden {
  display: none !important;
}

/* 悬浮删除区样式 */
.planning-board__delete-zone {
  position: absolute;
  left: 50%;
  bottom: 80px;
  transform: translateX(-50%);
  z-index: 100;
  width: 280px;
  height: 64px;
  border-radius: 32px;
  background: rgba(239, 68, 68, 0.9);
  backdrop-filter: blur(8px);
  border: 2px dashed rgba(255, 255, 255, 0.4);
  color: #ffffff;
  display: flex;
  align-items: center;
  justify-content: center;
  gap: 12px;
  box-shadow: 0 12px 32px rgba(239, 68, 68, 0.3);
  transition: all 0.3s cubic-bezier(0.34, 1.56, 0.64, 1);
}

.planning-board__delete-zone--active {
  background: #ef4444;
  transform: translateX(-50%) scale(1.1);
  box-shadow: 0 16px 48px rgba(239, 68, 68, 0.45);
  border-style: solid;
}

.delete-zone-icon {
  animation: delete-icon-shake 1.5s infinite;
}

@keyframes delete-icon-shake {
  0%, 100% { transform: rotate(0deg); }
  25% { transform: rotate(-10deg); }
  75% { transform: rotate(10deg); }
}

.delete-zone-enter-active,
.delete-zone-leave-active {
  transition: all 0.3s cubic-bezier(0.34, 1.56, 0.64, 1);
}

.delete-zone-enter-from,
.delete-zone-leave-to {
  opacity: 0;
  transform: translateX(-50%) translateY(40px) scale(0.8);
}

@keyframes board-item-spring {
  0% { opacity: 0; transform: scale(0.6) translateY(20px); }
  60% { opacity: 1; transform: scale(1.05) translateY(-2px); }
  100% { opacity: 1; transform: scale(1) translateY(0); }
}

.board-item-pop {
  animation: board-item-spring 0.5s cubic-bezier(0.34, 1.56, 0.64, 1) both;
  animation-delay: var(--anim-delay, 0s);
  transform-origin: center center;
}

@media (max-width: 1560px) {
  .planning-board__grid {
    --planning-time-column-width: 64px;
    --planning-day-column-min: 92px;
    --planning-grid-padding-x: 18px;
    --planning-grid-padding-y: 22px;
    --planning-grid-gap-x: 10px;
  }
}

@media (max-width: 1380px) {
  .planning-board__header {
    padding: 16px 20px 14px;
  }

  .planning-board__grid {
    --planning-time-column-width: 58px;
    --planning-day-column-min: 84px;
    --planning-grid-padding-x: 14px;
    --planning-grid-padding-y: 18px;
    --planning-grid-gap-x: 8px;
    --planning-grid-gap-y: 8px;
  }

  .planning-board__time-cell,
  .planning-board__cell {
    min-height: 98px;
  }

  .planning-board__cell {
    padding: 14px 10px;
    border-radius: 18px;
  }

  .planning-board__cell-main {
    gap: 8px;
  }

  .planning-board__cell-main strong {
    font-size: 14px;
  }

  .planning-board__embedded-course strong {
    font-size: 12px;
  }

  .planning-board__cell--course-embedded {
    padding: 8px;
  }
}

@media (max-width: 1180px) {
  .planning-board__grid {
    --planning-time-column-width: 56px;
    --planning-day-column-min: 78px;
  }

  .planning-board__day-head span {
    font-size: 13px;
  }

  .planning-board__day-head small,
  .planning-board__cell-main span {
    font-size: 11px;
  }
}

@media (max-height: 900px) {
  .planning-board {
    --planning-grid-padding-y: 18px;
    --planning-cell-height: clamp(66px, 8.2vh, 92px);
  }

  .planning-board__header {
    padding-top: 14px;
    padding-bottom: 12px;
  }

  .planning-board__header strong {
    font-size: 16px;
  }
}

@media (max-height: 820px) {
  .planning-board {
    --planning-grid-padding-y: 14px;
    --planning-grid-gap-y: 6px;
    --planning-cell-height: clamp(58px, 7.2vh, 82px);
  }

  .planning-board__time-cell strong,
  .planning-board__cell-main strong {
    font-size: 13px;
  }

  .planning-board__time-cell small,
  .planning-board__day-head small,
  .planning-board__cell-main span {
    font-size: 10px;
  }

  .planning-board__cell {
    padding: 12px 8px;
  }

  .planning-board__embedded-task {
    padding: 5px 7px;
    border-radius: 12px;
  }

  .planning-board__embedded-course {
    padding: 4px 2px;
  }

  .planning-board__embedded-course strong,
  .planning-board__embedded-task strong {
    font-size: 11px;
  }

  .planning-board__cell--course-embedded {
    padding: 7px;
  }
}
</style>
