<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, onMounted, ref, watch } from 'vue'

import type { TaskClassDetail, TaskClassListItem } from '@/types/schedule'

const props = defineProps<{
  taskClasses: TaskClassListItem[]
  loading?: boolean
  detailLoading?: boolean
  expandedTaskClassId: number | null
  expandedTaskClassDetail: TaskClassDetail | null
  selectedTaskClassIds: number[]
  taskClassMultiSelectMode: boolean
}>()

const emit = defineEmits<{
  activate: [taskClassId: number]
  toggleMultiMode: []
  create: []
  deleteItem: [taskItemId: number]
}>()

const taskClassCountLabel = computed(() => `共 ${props.taskClasses.length} 个`)
const viewportHeight = ref(typeof window === 'undefined' ? 900 : window.innerHeight)
const taskClassListRef = ref<HTMLElement | null>(null)
const listViewportHeight = ref(0)
let listResizeObserver: ResizeObserver | null = null

function isExpanded(taskClassId: number) {
  return props.expandedTaskClassId === taskClassId && !props.taskClassMultiSelectMode
}

function isSelected(taskClassId: number) {
  return props.selectedTaskClassIds.includes(taskClassId)
}

function formatEmbeddedTime(value: TaskClassDetail['items'][number]['embedded_time']) {
  if (!value?.date) {
    return '未安排'
  }

  const date = new Date(value.date)
  const month = `${date.getMonth() + 1}`.padStart(2, '0')
  const day = `${date.getDate()}`.padStart(2, '0')
  return `${month}.${day} ${value.section_from}-${value.section_to}节`
}

function syncViewportHeight() {
  viewportHeight.value = window.innerHeight
}

function syncTaskClassListViewportHeight() {
  listViewportHeight.value = taskClassListRef.value?.clientHeight ?? 0
}

function resolveDetailPanelStyle(items: TaskClassDetail['items']) {
  const count = items.length
  const itemHeight = viewportHeight.value <= 820 ? 54 : viewportHeight.value <= 900 ? 58 : 62
  const gap = 6
  const panelPadding = 14
  const preferredHeight = count * itemHeight + Math.max(0, count - 1) * gap + panelPadding
  const maxVisibleItems = viewportHeight.value <= 820 ? 4 : viewportHeight.value <= 900 ? 5 : 6
  const maxHeightByItemCount =
    maxVisibleItems * itemHeight + Math.max(0, maxVisibleItems - 1) * gap + panelPadding
  const maxHeightByContainer = Math.max(
    180,
    (listViewportHeight.value || Math.round(viewportHeight.value * 0.6)) - 116,
  )
  const finalHeight = Math.min(preferredHeight, maxHeightByItemCount, maxHeightByContainer)

  return {
    maxHeight: `${finalHeight}px`,
  }
}

onMounted(() => {
  window.addEventListener('resize', syncViewportHeight)
  window.addEventListener('resize', syncTaskClassListViewportHeight)
  syncTaskClassListViewportHeight()
  if (typeof ResizeObserver !== 'undefined') {
    listResizeObserver = new ResizeObserver(() => {
      syncTaskClassListViewportHeight()
    })
    if (taskClassListRef.value) {
      listResizeObserver.observe(taskClassListRef.value)
    }
  }
})

onBeforeUnmount(() => {
  window.removeEventListener('resize', syncViewportHeight)
  window.removeEventListener('resize', syncTaskClassListViewportHeight)
  listResizeObserver?.disconnect()
  listResizeObserver = null
})

watch(
  () => props.expandedTaskClassId,
  async (expandedId) => {
    await nextTick()
    syncTaskClassListViewportHeight()
    if (!expandedId || !taskClassListRef.value) {
      return
    }

    const expandedCard = taskClassListRef.value.querySelector<HTMLElement>('.task-class-card--expanded')
    expandedCard?.scrollIntoView({
      block: 'nearest',
      inline: 'nearest',
    })
  },
)
</script>

<template>
  <aside class="task-class-sidebar">
    <div class="task-class-sidebar__header">
      <div class="task-class-sidebar__title-row">
        <div class="task-class-sidebar__title-wrap">
          <span class="task-class-sidebar__title-icon" aria-hidden="true">
            <svg width="16" height="16" viewBox="0 0 16 16" fill="none" xmlns="http://www.w3.org/2000/svg">
              <path d="M2.40039 5.10742L7.99902 1.59961L13.5996 5.10742L7.99902 8.61523L2.40039 5.10742Z" fill="currentColor" />
              <path d="M2.40039 8.20312L7.99902 11.7109L13.5996 8.20312" stroke="currentColor" stroke-width="1.2" stroke-linecap="round" stroke-linejoin="round" />
              <path d="M2.40039 11.2891L7.99902 14.7969L13.5996 11.2891" stroke="currentColor" stroke-width="1.2" stroke-linecap="round" stroke-linejoin="round" />
            </svg>
          </span>
          <strong>任务类别列表</strong>
        </div>
        <span class="task-class-sidebar__count">{{ taskClassCountLabel }}</span>
      </div>

      <button type="button" class="task-class-sidebar__mode" @click="emit('toggleMultiMode')">
        {{ taskClassMultiSelectMode ? '取消批量' : '批量选择' }}
      </button>
    </div>

    <div v-if="loading" class="task-class-sidebar__skeleton">
      <div v-for="index in 4" :key="index" class="task-class-sidebar__skeleton-item" />
    </div>

    <div v-else ref="taskClassListRef" class="task-class-sidebar__list">
      <article
        v-for="taskClass in taskClasses"
        :key="taskClass.id"
        class="task-class-card"
        :class="{
          'task-class-card--expanded': isExpanded(taskClass.id),
          'task-class-card--selected': isSelected(taskClass.id),
        }"
      >
        <button type="button" class="task-class-card__summary" @click="emit('activate', taskClass.id)">
          <span
            v-if="taskClassMultiSelectMode"
            class="task-class-card__selector"
            :class="{ 'task-class-card__selector--active': isSelected(taskClass.id) }"
            aria-hidden="true"
          />

          <div class="task-class-card__content">
            <strong>{{ taskClass.name }}</strong>
            <span>{{ taskClass.total_slots }}节课</span>
          </div>

          <span class="task-class-card__corner" aria-hidden="true">
            <svg width="16" height="16" viewBox="0 0 16 16" fill="none" xmlns="http://www.w3.org/2000/svg">
              <path d="M5.22559 3.94922H12.0498V10.7734H10.7998V6.08301L4.3916 12.4912L3.50781 11.6074L9.91602 5.19922H5.22559V3.94922Z" fill="currentColor" />
            </svg>
          </span>
        </button>

        <transition name="task-detail">
          <div
            v-if="isExpanded(taskClass.id)"
            class="task-class-card__detail"
            :style="expandedTaskClassDetail ? resolveDetailPanelStyle(expandedTaskClassDetail.items) : { maxHeight: '60px' }"
          >
            <div v-if="detailLoading" class="task-class-card__detail-loading">正在载入任务块…</div>

            <div v-else-if="expandedTaskClassDetail" class="task-class-card__detail-list">
              <div
                v-for="item in expandedTaskClassDetail.items"
                :key="item.order"
                class="task-class-card__detail-item"
              >
                <span class="task-class-card__detail-order">{{ item.order }}</span>
                <span class="task-class-card__detail-text">{{ item.content }}</span>
                <span
                  class="task-class-card__detail-status"
                  :class="{ 'task-class-card__detail-status--arranged': item.embedded_time }"
                >
                  {{ formatEmbeddedTime(item.embedded_time) }}
                </span>
                <button
                  type="button"
                  class="task-class-card__detail-delete"
                  aria-label="删除任务块"
                  :disabled="typeof item.id !== 'number'"
                  @click="typeof item.id === 'number' && emit('deleteItem', item.id)"
                >
                  ×
                </button>
              </div>
            </div>
          </div>
        </transition>
      </article>

      <button type="button" class="task-class-sidebar__create" @click="emit('create')">
        <span class="task-class-sidebar__create-icon" aria-hidden="true">＋</span>
        <span>点击新建任务类</span>
      </button>
    </div>
  </aside>
</template>

<style scoped>
.task-class-sidebar {
  min-width: 0;
  min-height: 0;
  height: 100%;
  display: grid;
  grid-template-rows: auto minmax(0, 1fr);
  border-right: 1px solid rgba(15, 23, 42, 0.05);
  background: #ffffff;
  overflow: hidden;
}

/* --- 全局精致滚动条 --- */
::-webkit-scrollbar {
  width: 5px;
  height: 5px;
}

::-webkit-scrollbar-track {
  background: transparent;
}

::-webkit-scrollbar-thumb {
  background: rgba(15, 23, 42, 0.08);
  border-radius: 10px;
}

::-webkit-scrollbar-thumb:hover {
  background: rgba(15, 23, 42, 0.15);
}

.task-class-sidebar__header {
  padding: 20px 24px;
  border-bottom: 1px solid rgba(15, 23, 42, 0.05);
  display: grid;
  gap: 12px;
  min-width: 0;
}

.task-class-sidebar__title-row {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
  min-width: 0;
  flex-wrap: wrap;
}

.task-class-sidebar__title-wrap {
  display: inline-flex;
  align-items: center;
  gap: 10px;
  color: #1f2c42;
  min-width: 0;
}

.task-class-sidebar__title-wrap strong {
  font-size: 15px;
  font-weight: 800;
  min-width: 0;
}

.task-class-sidebar__title-icon {
  width: 16px;
  height: 16px;
  display: inline-flex;
  color: #3b82f6;
}

.task-class-sidebar__count {
  padding: 5px 12px;
  border-radius: 10px;
  background: #eef3f9;
  color: #75839a;
  font-size: 12px;
  line-height: 1;
}

.task-class-sidebar__mode {
  height: 34px;
  border: 1px solid rgba(59, 130, 246, 0.18);
  border-radius: 12px;
  background: #f8fafc;
  color: #3b82f6;
  font-size: 12px;
  font-weight: 700;
  justify-self: start;
  padding: 0 14px;
  cursor: pointer;
  transition: all 0.2s;
}

.task-class-sidebar__mode:hover {
  border-color: rgba(59, 130, 246, 0.34);
  background: #eff6ff;
}

.task-class-sidebar__list,
.task-class-sidebar__skeleton {
  min-height: 0;
  overflow-y: auto;
  overflow-x: hidden;
  padding: 24px;
  display: flex;
  flex-direction: column;
  gap: 14px;
  scrollbar-gutter: stable;
}

.task-class-sidebar__skeleton-item {
  flex: 0 0 auto;
  height: 120px;
  border-radius: 20px;
  background: rgba(15, 23, 42, 0.03);
  animation: task-class-skeleton 1.25s linear infinite;
}

.task-class-card {
  flex: 0 0 auto;
  min-width: 0;
  border-radius: 20px;
  border: 1px solid rgba(15, 23, 42, 0.06);
  background: #ffffff;
  box-shadow: 0 4px 12px rgba(15, 23, 42, 0.02);
  overflow: hidden;
  transition: all 0.25s cubic-bezier(0.4, 0, 0.2, 1);
}

.task-class-card--selected {
  border-color: #3b82f6;
  box-shadow: 0 10px 25px rgba(59, 130, 246, 0.1);
}

.task-class-card__summary {
  width: 100%;
  min-width: 0;
  min-height: 92px;
  border: none;
  background: transparent;
  padding: 18px 20px 18px 18px;
  display: grid;
  grid-template-columns: auto minmax(0, 1fr) auto;
  gap: 12px;
  align-items: start;
  text-align: left;
  cursor: pointer;
}

.task-class-card__summary:hover .task-class-card__corner {
  background: #eff6ff;
  color: #3b82f6;
}

.task-class-card__selector {
  width: 16px;
  height: 16px;
  margin-top: 5px;
  border-radius: 5px;
  border: 1px solid rgba(148, 163, 184, 0.55);
  background: #ffffff;
}

.task-class-card__selector--active {
  border-color: #3b82f6;
  background: #3b82f6;
  box-shadow: inset 0 0 0 3px #ffffff;
}

.task-class-card__content {
  min-width: 0;
  display: grid;
  gap: 8px;
}

.task-class-card__content strong {
  color: #182741;
  font-size: 16px;
  line-height: 1.35;
  font-weight: 800;
  overflow-wrap: anywhere;
}

.task-class-card__content span {
  color: #71819a;
  font-size: 13px;
}

.task-class-card__corner {
  flex: 0 0 auto;
  width: 48px;
  height: 48px;
  border-radius: 999px;
  display: inline-flex;
  align-items: center;
  justify-content: center;
  background: #f8fafc;
  color: #3b82f6;
  transition: all 0.2s;
}

.task-class-card__corner svg {
  transition: transform 0.35s cubic-bezier(0.34, 1.56, 0.64, 1);
}

.task-class-card--expanded .task-class-card__corner svg {
  transform: rotate(45deg);
}

.task-class-card__detail {
  box-sizing: border-box;
  min-width: 0;
  min-height: 0;
  padding: 0 14px 14px;
  overflow-y: auto;
  overflow-x: hidden;
  scrollbar-gutter: stable;
  overscroll-behavior: contain;
}

.task-detail-enter-active,
.task-detail-leave-active {
  transition: max-height 0.4s cubic-bezier(0.34, 1.56, 0.64, 1), padding 0.4s cubic-bezier(0.34, 1.56, 0.64, 1), opacity 0.3s ease;
  overflow: hidden;
}

.task-detail-enter-from,
.task-detail-leave-to {
  max-height: 0 !important;
  padding-top: 0 !important;
  padding-bottom: 0 !important;
  opacity: 0;
}

.task-class-card__detail-loading {
  padding: 14px 12px 10px;
  color: #7b88a1;
  font-size: 13px;
}

.task-class-card__detail-list {
  display: grid;
  gap: 6px;
  min-width: 0;
  padding-right: 4px;
}

.task-class-card__detail-item {
  min-width: 0;
  padding: 8px 10px;
  border-radius: 16px;
  border: 1px solid rgba(197, 209, 226, 0.8);
  background: rgba(255, 255, 255, 0.92);
  display: grid;
  grid-template-columns: 28px minmax(0, 1fr) auto 24px;
  gap: 10px;
  align-items: center;
}

.task-class-card__detail-order {
  color: #17253d;
  font-weight: 700;
  text-align: center;
}

.task-class-card__detail-text {
  min-width: 0;
  color: #1e293b;
  font-size: 13px;
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}

.task-class-card__detail-status {
  max-width: 100%;
  padding: 4px 10px;
  border-radius: 999px;
  background: #f1f5f9;
  color: #74839a;
  font-size: 12px;
  white-space: nowrap;
}

.task-class-card__detail-status--arranged {
  background: #dcf4bd;
  color: #486a18;
}

.task-class-card__detail-delete {
  width: 24px;
  height: 24px;
  border: none;
  border-radius: 999px;
  background: #ef4444;
  color: #ffffff;
  font-size: 16px;
  line-height: 1;
  cursor: pointer;
}

.task-class-card__detail-delete:disabled {
  opacity: 0.32;
  cursor: not-allowed;
}

.task-class-sidebar__create {
  flex: 0 0 auto;
  min-width: 0;
  min-height: 96px;
  border: 1.5px dashed rgba(15, 23, 42, 0.1);
  border-radius: 20px;
  background: transparent;
  color: #94a3b8;
  display: grid;
  justify-items: center;
  align-content: center;
  gap: 10px;
  cursor: pointer;
  transition: all 0.2s;
}

.task-class-sidebar__create:hover {
  border-color: #3b82f6;
  background: #f8fafc;
  color: #3b82f6;
}

.task-class-sidebar__create-icon {
  width: 24px;
  height: 24px;
  border-radius: 999px;
  border: 1px solid currentColor;
  display: inline-flex;
  align-items: center;
  justify-content: center;
  font-size: 18px;
  line-height: 1;
}

@keyframes task-class-skeleton {
  0% { background-position: 200% 0; }
  100% { background-position: -200% 0; }
}

@media (max-width: 1520px) {
  .task-class-sidebar__header,
  .task-class-sidebar__list,
  .task-class-sidebar__skeleton {
    padding-left: 18px;
    padding-right: 18px;
  }
}

@media (max-width: 1380px) {
  .task-class-sidebar {
    border-right: none;
    border-bottom: 1px solid rgba(15, 23, 42, 0.05);
  }
}

@media (max-height: 900px) {
  .task-class-sidebar__header { padding: 12px 18px; }
  .task-class-sidebar__list,
  .task-class-sidebar__skeleton { padding: 16px 18px; }
}
</style>
