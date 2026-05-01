<script setup lang="ts">
import { ref, computed, watch } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import type { HybridScheduleEntry, PlacedItem, SchedulePreviewData } from '@/types/dashboard'
import {
  saveScheduleState,
  applyBatchIntoSchedule,
  confirmActiveSchedulePreview,
  type ActiveScheduleConfirmChange,
  type ActiveSchedulePreviewDetail,
} from '@/api/schedule_agent'

const props = defineProps<{
  previewData: SchedulePreviewData | null
  visible: boolean
  previewKind?: 'schedule' | 'active_schedule'
  activePreviewDetail?: ActiveSchedulePreviewDetail | null
}>()

const emit = defineEmits<{
  (e: 'close'): void
  (e: 'saved'): void
}>()

const currentWeek = ref(1)
const isSaving = ref(false)

// 内部维护一份可变的建议任务列表，组件初始化时默认空
const suggestedItems = ref<HybridScheduleEntry[]>([])

// 监听数据变化，当传了有效数据时才进行初始化，解决 v-if 延迟导致的空引用问题
watch(() => props.previewData, (newVal) => {
  if (newVal) {
    suggestedItems.value = JSON.parse(JSON.stringify(newVal.hybrid_entries))
    currentWeek.value = newVal.hybrid_entries.length > 0 ? Math.min(...newVal.hybrid_entries.map(e => e.week)) : 1
  }
}, { immediate: true })

const weekRange = computed(() => {
  if (!props.previewData) return { min: 1, max: 20 }
  const weeks = props.previewData.hybrid_entries.map(e => e.week)
  if (weeks.length === 0) return { min: 1, max: 20 }
  return {
    min: Math.min(...weeks),
    max: Math.max(...weeks)
  }
})

const sectionSlots = [
  { order: 1, title: '1-2', timeRange: '08:00\n09:40' },
  { order: 2, title: '3-4', timeRange: '10:15\n11:55' },
  { order: 3, title: '5-6', timeRange: '14:00\n15:40' },
  { order: 4, title: '7-8', timeRange: '16:15\n17:55' },
  { order: 5, title: '9-10', timeRange: '19:00\n20:40' },
  { order: 6, title: '11-12', timeRange: '20:50\n22:30' },
]

function prevWeek() {
  if (currentWeek.value > weekRange.value.min) currentWeek.value--
}

function nextWeek() {
  if (currentWeek.value < weekRange.value.max) currentWeek.value++
}

// 构建现有可嵌入课程的位置索引，key 为 "week-day-sectionFrom-sectionTo"
function buildCoursePositionIndex(items: HybridScheduleEntry[]): Map<string, HybridScheduleEntry> {
  const index = new Map<string, HybridScheduleEntry>()
  for (const e of items) {
    if (e.type === 'course' && e.status === 'existing' && e.can_be_embedded) {
      index.set(`${e.week}-${e.day_of_week}-${e.section_from}-${e.section_to}`, e)
    }
  }
  return index
}

// 查找 suggested task 同位置的宿主课程 event_id
function resolveEmbedCourseEventId(
  task: HybridScheduleEntry,
  courseIndex: Map<string, HybridScheduleEntry>,
): number | undefined {
  if (task.event_id) return task.event_id
  const host = courseIndex.get(`${task.week}-${task.day_of_week}-${task.section_from}-${task.section_to}`)
  return host ? host.event_id : undefined
}

// 转换当前状态为后端要求的 PlacedItem 数组
function buildPlacedItems(): PlacedItem[] {
  const courseIndex = buildCoursePositionIndex(suggestedItems.value)
  return suggestedItems.value
    .filter(e => e.type === 'task' && e.status === 'suggested')
    .map(e => ({
      task_item_id: e.task_item_id,
      week: e.week,
      day_of_week: e.day_of_week,
      start_section: e.section_from,
      end_section: e.section_to,
      embed_course_event_id: resolveEmbedCourseEventId(e, courseIndex),
    }))
}

function resolveItemSlot(item: HybridScheduleEntry) {
  return {
    week: item.week,
    day_of_week: item.day_of_week,
    section_from: item.section_from,
    section_to: item.section_to,
    duration_sections: item.section_to - item.section_from + 1,
  }
}

function resolveActiveChangeItem(change: ActiveSchedulePreviewDetail['changes'][number]) {
  if (change.target_type === 'task_pool' || change.change_type === 'add_task_pool_to_schedule') {
    return suggestedItems.value.find(item => item.task_item_id === change.target_id)
  }
  return suggestedItems.value.find(item => item.event_id === change.target_id)
}

function buildActiveEditedChanges(): ActiveScheduleConfirmChange[] {
  if (!props.activePreviewDetail) {
    return []
  }

  return props.activePreviewDetail.changes.map((change) => {
    const currentItem = resolveActiveChangeItem(change)
    const slot = currentItem ? resolveItemSlot(currentItem) : undefined
    const fallbackSlot = change.to_slot
    const week = slot?.week ?? fallbackSlot?.start.week ?? 1
    const dayOfWeek = slot?.day_of_week ?? fallbackSlot?.start.day_of_week ?? 1
    const sectionFrom = slot?.section_from ?? fallbackSlot?.start.section ?? 1
    const sectionTo = slot?.section_to ?? fallbackSlot?.end.section ?? sectionFrom
    const durationSections = slot?.duration_sections ?? fallbackSlot?.duration_sections ?? Math.max(1, sectionTo - sectionFrom + 1)

    return {
      change_id: change.change_id,
      type: change.change_type,
      target_type: change.target_type,
      target_id: change.target_id,
      task_id: change.target_type === 'task_pool' ? change.target_id : undefined,
      event_id: change.target_type === 'schedule_event' ? change.target_id : undefined,
      week,
      day_of_week: dayOfWeek,
      section_from: sectionFrom,
      section_to: sectionTo,
      duration_sections: durationSections,
      edited_allowed: change.edited_allowed,
      metadata: change.metadata,
    }
  })
}

/**
 * 暂存至 State (Redis)
 */
async function handleSaveToState() {
  if (!props.previewData) return
  isSaving.value = true
  try {
    const items = buildPlacedItems()
    await saveScheduleState(props.previewData.conversation_id, items)
    ElMessage.success('方案已成功暂存')
  } catch (error: any) {
    ElMessage.error(error.message || '暂存失败')
  } finally {
    isSaving.value = false
  }
}

// 每个预览会话维持一个稳定的幂等键，避免重试或延迟导致的重复落库
const officialSaveIdempotencyKey = ref(crypto.randomUUID())

/**
 * 正式保存到数据库 (MySQL)
 */
async function handleOfficialSave() {
  if (!props.previewData) return
  await ElMessageBox.confirm(
    '正式保存将把当前编排结果写入你的日程表。保存后本轮编排微调将终止，确认继续吗？',
    '正式保存确认',
    {
      confirmButtonText: '确认保存',
      cancelButtonText: '我再想想',
      type: 'warning',
      roundButton: true,
      customClass: 'premium-msg-box',
    }
  )

  isSaving.value = true
  try {
    if (props.previewKind === 'active_schedule') {
      const activeDetail = props.activePreviewDetail
      if (!activeDetail) {
        throw new Error('主动调度预览数据不完整')
      }

      const payload = {
        candidate_id: activeDetail.selected_candidate.candidate_id,
        action: 'confirm' as const,
        edited_changes: buildActiveEditedChanges(),
        idempotency_key: officialSaveIdempotencyKey.value,
      }

      await confirmActiveSchedulePreview(activeDetail.preview_id, payload)
      ElMessage.success('主动调度已确认')
    } else {
      // 按 task_class_id 分组
      const courseIndex = buildCoursePositionIndex(suggestedItems.value)
      const groups = new Map<number, PlacedItem[]>()
      suggestedItems.value.forEach(e => {
        if (e.type === 'task' && e.status === 'suggested' && e.task_class_id) {
          if (!groups.has(e.task_class_id)) groups.set(e.task_class_id, [])
          groups.get(e.task_class_id)!.push({
            task_item_id: e.task_item_id,
            week: e.week,
            day_of_week: e.day_of_week,
            start_section: e.section_from,
            end_section: e.section_to,
            embed_course_event_id: resolveEmbedCourseEventId(e, courseIndex),
          })
        }
      })

      const promises = Array.from(groups.entries()).map(([classId, groupItems]) =>
        applyBatchIntoSchedule(classId, groupItems, `${officialSaveIdempotencyKey.value}-${classId}`),
      )

      await Promise.all(promises)
      ElMessage.success('日程已正式保存到数据库')
    }

    // 保存成功后刷新幂等键，虽然通常弹窗会关闭，但这是为了逻辑严密
    officialSaveIdempotencyKey.value = crypto.randomUUID()

    emit('saved')
    emit('close')
  } catch (error: any) {
    ElMessage.error(error.message || '保存失败')
  } finally {
    isSaving.value = false
  }
}

// 拖拽逻辑
function onDragStart(event: DragEvent, item: HybridScheduleEntry) {
  if (item.status === 'existing') return // 不允许拖拽已有日程
  if (event.dataTransfer) {
    event.dataTransfer.setData('application/json', JSON.stringify(item))
    event.dataTransfer.effectAllowed = 'move'
  }
}

function onDrop(event: DragEvent, day: number, order: number) {
  const rawData = event.dataTransfer?.getData('application/json')
  if (!rawData) return

  const draggedItem = JSON.parse(rawData) as HybridScheduleEntry
  const itemIndex = suggestedItems.value.findIndex(i => i.task_item_id === draggedItem.task_item_id)
  
  if (itemIndex > -1) {
    // 转发为块起始节次
    const targetSectionFrom = (order - 1) * 2 + 1
    const targetSectionTo = targetSectionFrom + 1

    // 检查目标位置是否有冲突（block_for_suggested === true 且不是自己）
    const conflict = suggestedItems.value.find(i => 
      i.week === currentWeek.value && 
      i.day_of_week === day && 
      getBlockIndex(i.section_from) === order && 
      i.block_for_suggested &&
      i.task_item_id !== draggedItem.task_item_id
    )

    if (conflict) {
      if (conflict.can_be_embedded) {
        // 允许嵌入
        const item = suggestedItems.value[itemIndex]
        item.week = currentWeek.value
        item.day_of_week = day
        item.section_from = targetSectionFrom
        item.section_to = targetSectionTo
        item.event_id = conflict.event_id // 设置嵌入目标 ID
      } else {
        ElMessage.warning('该时段已有课程，无法安插任务')
      }
    } else {
      // 自由空位
      const item = suggestedItems.value[itemIndex]
      item.week = currentWeek.value
      item.day_of_week = day
      item.section_from = targetSectionFrom
      item.section_to = targetSectionTo
      item.event_id = 0 // 清除嵌入状态
    }
  }
}

// 将后端节次 (1, 3, 5...) 映射为前端 6 个双节块索引 (1, 2, 3...)
const getBlockIndex = (section: number) => Math.floor((section - 1) / 2) + 1

// 获取项的网格定位样式
function getItemStyle(item: HybridScheduleEntry) {
  // 网格第 1 行是表头，所以 row 为 section + 1
  const rowStart = item.section_from + 1
  const rowEnd = item.section_to + 2
  return {
    gridColumn: item.day_of_week + 1,
    gridRow: `${rowStart} / ${rowEnd}`,
    zIndex: item.status === 'suggested' ? 2 : 1
  }
}

// 被嵌入课程的位置集合：当 suggested task 与 existing course 同位置时，课程不单独渲染
const embeddedCoursePositions = computed(() => {
  const positions = new Set<string>()
  const courseIndex = buildCoursePositionIndex(suggestedItems.value)
  for (const e of suggestedItems.value) {
    if (e.type === 'task' && e.status === 'suggested') {
      const key = `${e.week}-${e.day_of_week}-${e.section_from}-${e.section_to}`
      if (courseIndex.has(key)) positions.add(key)
    }
  }
  return positions
})

const currentWeekEntries = computed(() =>
  suggestedItems.value.filter(e => {
    if (e.week !== currentWeek.value) return false
    if (e.type === 'course' && e.status === 'existing') {
      const key = `${e.week}-${e.day_of_week}-${e.section_from}-${e.section_to}`
      if (embeddedCoursePositions.value.has(key)) return false
    }
    return true
  })
)
</script>

<template>
  <Teleport to="body">
    <Transition name="modal">
      <div v-if="visible && previewData" class="schedule-modal-overlay" @click.self="emit('close')">
        <div class="schedule-modal">
          <header class="schedule-modal__header">
            <h3>日程预览与精排 (第 {{ currentWeek }} 周)</h3>
            <div class="schedule-modal__header-actions">
              <div class="week-switcher">
                <button
                  class="week-switcher__btn"
                  @click="prevWeek"
                  :disabled="currentWeek <= weekRange.min"
                >
                  <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round" stroke-linejoin="round">
                    <polyline points="15 18 9 12 15 6"></polyline>
                  </svg>
                </button>
                <span class="week-switcher__label">第 {{ currentWeek }} 周</span>
                <button
                  class="week-switcher__btn"
                  @click="nextWeek"
                  :disabled="currentWeek >= weekRange.max"
                >
                  <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round" stroke-linejoin="round">
                    <polyline points="9 18 15 12 9 6"></polyline>
                  </svg>
                </button>
              </div>
              <button class="schedule-modal__close" @click="emit('close')">
                <svg width="20" height="20" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round" stroke-linejoin="round">
                  <line x1="18" y1="6" x2="6" y2="18"></line>
                  <line x1="6" y1="6" x2="18" y2="18"></line>
                </svg>
              </button>
            </div>
          </header>

          <div class="schedule-modal__body">
            <div class="planning-board__grid">
              <div class="planning-board__corner" />

              <!-- 表头：周一到周日 -->
              <div
                v-for="d in 7"
                :key="`h-${d}`"
                class="planning-board__day-head"
                :style="{ gridColumn: d + 1, gridRow: 1 }"
              >
                <span>{{ ['周一', '周二', '周三', '周四', '周五', '周六', '周日'][d-1] }}</span>
              </div>

              <!-- 时间侧栏与背景格：采用跨行方式，每个 Block 占 2 行 -->
              <template v-for="slot in sectionSlots" :key="`r-${slot.order}`">
                <div
                  class="planning-board__time-cell"
                  :style="{ gridColumn: 1, gridRow: `${(slot.order-1)*2 + 2} / span 2` }"
                >
                  <strong>{{ slot.title }}</strong>
                  <small>{{ slot.timeRange }}</small>
                </div>
                <!-- 空白背景格：跨 2 行以形成大块感 -->
                <div
                  v-for="d in 7"
                  :key="`bg-${d}-${slot.order}`"
                  class="planning-board__cell planning-board__cell--empty"
                  :style="{ gridColumn: d + 1, gridRow: `${(slot.order-1)*2 + 2} / span 2` }"
                  @dragover.prevent
                  @drop="onDrop($event, d, slot.order)"
                />
              </template>

              <!-- 实际的内容项：基于 12 行粒度精确展示 -->
              <div
                v-for="item in currentWeekEntries"
                :key="item.task_item_id || `existing-${item.event_id}`"
                class="planning-board__cell-main board-item-pop"
                :class="`planning-board__cell-main--${item.type}`"
                :style="getItemStyle(item)"
                :draggable="item.status === 'suggested'"
                @dragstart="onDragStart($event, item)"
              >
                <strong>{{ item.name }}</strong>
                <span>{{ item.type === 'course' ? (item.context_tag || '教学楼 A') : '个人任务' }}</span>
              </div>
            </div>
          </div>

          <footer class="schedule-modal__footer">
            <p class="schedule-modal__hint">提示：拖动卡片可调整日程顺序</p>
            <div class="schedule-modal__actions">
              <button class="tool-btn tool-btn--ghost" @click="emit('close')">取消</button>
              <button
                class="tool-btn tool-btn--state"
                :disabled="isSaving"
                @click="handleSaveToState"
              >
                {{ isSaving ? '保存中...' : '暂存进state' }}
              </button>
              <button
                class="tool-btn tool-btn--primary"
                :disabled="isSaving"
                @click="handleOfficialSave"
              >
                {{ isSaving ? '保存中...' : '正式保存日程' }}
              </button>
            </div>
          </footer>
        </div>
      </div>
    </Transition>
  </Teleport>
</template>

<style scoped>
/* 弹窗核心样式 */
.schedule-modal-overlay {
  position: fixed;
  inset: 0;
  background: rgba(15, 23, 42, 0.4);
  backdrop-filter: blur(8px);
  z-index: 2000;
  display: flex;
  align-items: center;
  justify-content: center;
  padding: 20px;
}

.schedule-modal {
  background: #ffffff;
  width: min(1400px, 92%);
  height: auto;
  max-height: 95vh;
  border-radius: 20px;
  display: flex;
  flex-direction: column;
  box-shadow: 0 25px 50px -12px rgba(0, 0, 0, 0.25);
  overflow: hidden;
}

.schedule-modal__header {
  padding: 20px 32px;
  border-bottom: 1px solid #f1f5f9;
  display: flex;
  justify-content: space-between;
  align-items: center;
  background: #ffffff;
}

.schedule-modal__header h3 {
  margin: 0;
  font-size: 18px;
  font-weight: 800;
  color: #0f172a;
  letter-spacing: -0.02em;
}

.schedule-modal__header-actions {
  display: flex;
  align-items: center;
  gap: 20px;
}

.week-switcher {
  display: flex;
  align-items: center;
  gap: 12px;
  background: #f8fafc;
  padding: 4px;
  border-radius: 12px;
  border: 1px solid #f1f5f9;
}

.week-switcher__btn {
  width: 32px;
  height: 32px;
  border: none;
  background: #ffffff;
  border-radius: 8px;
  display: flex;
  align-items: center;
  justify-content: center;
  color: #64748b;
  cursor: pointer;
  transition: all 0.2s;
  box-shadow: 0 1px 2px rgba(0, 0, 0, 0.05);
}

.week-switcher__btn:hover:not(:disabled) {
  background: #eff6ff;
  color: #3b82f6;
  transform: translateY(-1px);
}

.week-switcher__btn:disabled {
  opacity: 0.4;
  cursor: not-allowed;
}

.week-switcher__label {
  font-size: 14px;
  font-weight: 700;
  color: #1e293b;
  min-width: 60px;
  text-align: center;
}

.schedule-modal__close {
  background: #f1f5f9;
  border: none;
  width: 36px;
  height: 36px;
  border-radius: 50%;
  display: flex;
  align-items: center;
  justify-content: center;
  color: #64748b;
  cursor: pointer;
  transition: all 0.2s;
}

.schedule-modal__close:hover {
  background: #e2e8f0;
  color: #1e293b;
  transform: rotate(90deg);
}

.schedule-modal__body {
  padding: 0;
  flex: 1;
  overflow-y: auto;
}

.schedule-modal__footer {
  padding: 20px 32px;
  background: #f8fafc;
  border-top: 1px solid #f1f5f9;
  display: flex;
  justify-content: space-between;
  align-items: center;
}

.schedule-modal__hint {
  margin: 0;
  font-size: 13px;
  color: #94a3b8;
  font-style: italic;
}

.schedule-modal__actions {
  display: flex;
  gap: 12px;
}

/* 按钮通用样式 */
.tool-btn {
  border: 1px solid transparent;
  border-radius: 12px;
  padding: 10px 20px;
  font-size: 14px;
  font-weight: 600;
  transition: all 0.2s cubic-bezier(0.4, 0, 0.2, 1);
  cursor: pointer;
  display: flex;
  align-items: center;
  justify-content: center;
}

.tool-btn--primary {
  background: #0f172a;
  color: #ffffff;
  box-shadow: 0 4px 12px rgba(15, 23, 42, 0.15);
}

.tool-btn--primary:hover:not(:disabled) {
  background: #1e293b;
  transform: translateY(-2px);
  box-shadow: 0 6px 16px rgba(15, 23, 42, 0.2);
}

.tool-btn--state {
  background: #eff6ff;
  color: #2563eb;
  border-color: #dbeafe;
}

.tool-btn--state:hover:not(:disabled) {
  background: #dbeafe;
  color: #1e40af;
  border-color: #bfdbfe;
}

.tool-btn--ghost {
  background: #ffffff;
  color: #475569;
  border-color: #e2e8f0;
}

.tool-btn--ghost:hover {
  background: #f8fafc;
  border-color: #cbd5e1;
  color: #1e293b;
}

.tool-btn:disabled {
  opacity: 0.5;
  cursor: not-allowed;
}

/* 网格样式 */
.planning-board__grid {
  --planning-grid-padding-x: 20px;
  --planning-grid-padding-y: 16px;
  --planning-grid-gap-x: 10px;
  --planning-grid-gap-y: 10px;
  --planning-time-column-width: 76px;
  --planning-day-column-min: 96px;
  --planning-cell-height: 82px;

  display: grid;
  grid-template-columns: var(--planning-time-column-width) repeat(7, minmax(var(--planning-day-column-min), 1fr));
  grid-template-rows: auto repeat(12, calc((var(--planning-cell-height) - var(--planning-grid-gap-y)) / 2));
  gap: var(--planning-grid-gap-y) var(--planning-grid-gap-x);
  padding: var(--planning-grid-padding-y) var(--planning-grid-padding-x) 32px;
  overflow: auto;
  background: #ffffff;
}

.planning-board__corner {
  min-height: 1px;
}

.planning-board__day-head {
  display: grid;
  justify-items: center;
  gap: 4px;
  color: #64748b;
  padding-bottom: 12px;
}

.planning-board__day-head span {
  font-size: 14px;
  font-weight: 700;
  letter-spacing: 0.08em;
  color: #1e293b;
}

.planning-board__time-cell {
  display: grid;
  align-content: center;
  justify-items: end;
  color: #94a3b8;
  padding-right: 16px;
  border-right: 1px solid #f1f5f9;
}

.planning-board__time-cell strong {
  font-size: 15px;
  color: #475569;
  font-weight: 800;
}

.planning-board__time-cell small {
  font-size: 11px;
  color: #94a3b8;
  white-space: pre-line;
  text-align: right;
  line-height: 1.35;
}

.planning-board__cell {
  position: relative;
  border-radius: 16px;
  border: 1px solid transparent;
  display: flex;
  align-items: center;
  justify-content: center;
  transition: all 0.3s cubic-bezier(0.4, 0, 0.2, 1);
  background: #f8fafc;
}

.planning-board__cell--empty {
  background: #ffffff;
  border: 1px dashed #e2e8f0;
}

.planning-board__cell:hover:not(.planning-board__cell--empty) {
  transform: translateY(-4px);
  box-shadow: 0 12px 20px -8px rgba(0, 0, 0, 0.1);
  z-index: 10;
}

.planning-board__cell-main {
  width: 100%;
  height: 100%;
  padding: 12px;
  display: flex;
  flex-direction: column;
  justify-content: center;
  align-items: center;
  text-align: center;
  gap: 6px;
  border-radius: 16px;
  cursor: grab;
  transition: all 0.2s;
}

.planning-board__cell-main:active {
  cursor: grabbing;
}

.planning-board__cell-main--course {
  background: #e0f2fe;
  color: #0369a1;
}

.planning-board__cell-main--task {
  background: #dcfce7;
  color: #15803d;
}

.planning-board__cell-main strong {
  font-size: 13px;
  font-weight: 700;
  line-height: 1.4;
  overflow: hidden;
  display: -webkit-box;
  -webkit-line-clamp: 2;
  -webkit-box-orient: vertical;
}

.planning-board__cell-main span {
  font-size: 11px;
  opacity: 0.8;
}

/* 进场动画 */
@keyframes board-item-spring {
  0% { opacity: 0; transform: scale(0.6) translateY(20px); }
  60% { opacity: 1; transform: scale(1.05) translateY(-2px); }
  100% { opacity: 1; transform: scale(1) translateY(0); }
}

.board-item-pop {
  animation: board-item-spring 0.6s cubic-bezier(0.34, 1.56, 0.64, 1) both;
}

/* 弹窗核心动画：采用物理弹簧质感 */
.modal-enter-active {
  transition: opacity 0.5s ease;
}

.modal-leave-active {
  transition: opacity 0.3s ease;
}

.modal-enter-from,
.modal-leave-to {
  opacity: 0;
}

.modal-enter-active .schedule-modal {
  animation: modal-pop-in 0.6s cubic-bezier(0.34, 1.56, 0.64, 1);
}

.modal-leave-active .schedule-modal {
  animation: modal-pop-in 0.4s cubic-bezier(0.34, 1.56, 0.64, 1) reverse;
}

@keyframes modal-pop-in {
  0% {
    transform: scale(0.9) translateY(40px);
    opacity: 0;
  }
  60% {
    transform: scale(1.02) translateY(-2px);
    opacity: 1;
  }
  100% {
    transform: scale(1) translateY(0);
    opacity: 1;
  }
}
</style>
