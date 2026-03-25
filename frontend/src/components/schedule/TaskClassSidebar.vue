<script setup lang="ts">
import { computed } from 'vue'

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

    <div v-else class="task-class-sidebar__list">
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

        <div v-if="isExpanded(taskClass.id)" class="task-class-card__detail">
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
  border-right: 1px solid rgba(196, 209, 227, 0.55);
  background: linear-gradient(180deg, rgba(251, 253, 255, 0.96), rgba(247, 250, 254, 0.98));
}

.task-class-sidebar__header {
  padding: 16px 24px 14px;
  border-bottom: 1px solid rgba(214, 223, 238, 0.68);
  display: grid;
  gap: 12px;
}

.task-class-sidebar__title-row {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
}

.task-class-sidebar__title-wrap {
  display: inline-flex;
  align-items: center;
  gap: 10px;
  color: #1f2c42;
}

.task-class-sidebar__title-wrap strong {
  font-size: 15px;
  font-weight: 800;
}

.task-class-sidebar__title-icon {
  width: 16px;
  height: 16px;
  display: inline-flex;
  color: #165fd0;
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
  border: 1px solid rgba(25, 95, 213, 0.18);
  border-radius: 12px;
  background: #f6f9ff;
  color: #1d64d2;
  font-size: 12px;
  font-weight: 700;
  justify-self: start;
  padding: 0 14px;
  cursor: pointer;
  transition: border-color 0.16s ease, background-color 0.16s ease, color 0.16s ease;
}

.task-class-sidebar__mode:hover {
  border-color: rgba(25, 95, 213, 0.34);
  background: #edf4ff;
}

.task-class-sidebar__list,
.task-class-sidebar__skeleton {
  min-height: 0;
  overflow-y: auto;
  padding: 24px;
  display: grid;
  align-content: start;
  gap: 14px;
}

.task-class-sidebar__skeleton-item {
  height: 120px;
  border-radius: 24px;
  background: linear-gradient(90deg, rgba(234, 239, 246, 0.9), rgba(248, 251, 255, 1), rgba(234, 239, 246, 0.9));
  background-size: 200% 100%;
  animation: task-class-skeleton 1.25s linear infinite;
}

.task-class-card {
  border-radius: 24px;
  border: 1px solid rgba(216, 225, 238, 0.9);
  background: linear-gradient(180deg, #fdfefe 0%, #f8fbff 100%);
  box-shadow: 0 10px 22px rgba(19, 51, 107, 0.04);
  overflow: hidden;
}

.task-class-card--selected {
  border-color: rgba(28, 98, 206, 0.28);
  box-shadow: 0 14px 24px rgba(22, 95, 208, 0.08);
}

.task-class-card__summary {
  width: 100%;
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
  background: #edf4ff;
  color: #2067d5;
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
  border-color: #1e66d4;
  background: #1e66d4;
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
}

.task-class-card__content span {
  color: #71819a;
  font-size: 13px;
}

.task-class-card__corner {
  width: 48px;
  height: 48px;
  border-radius: 999px;
  display: inline-flex;
  align-items: center;
  justify-content: center;
  background: rgba(246, 249, 253, 0.9);
  color: #1e66d4;
  transition: background-color 0.16s ease, color 0.16s ease;
}

.task-class-card__detail {
  padding: 0 14px 14px;
}

.task-class-card__detail-loading {
  padding: 14px 12px 10px;
  color: #7b88a1;
  font-size: 13px;
}

.task-class-card__detail-list {
  display: grid;
  gap: 6px;
}

.task-class-card__detail-item {
  min-width: 0;
  padding: 10px 12px;
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
  padding: 4px 10px;
  border-radius: 999px;
  background: #f1f5f9;
  color: #74839a;
  font-size: 12px;
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
  background: #bb3326;
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
  min-height: 108px;
  border: 1px dashed rgba(204, 216, 232, 0.92);
  border-radius: 24px;
  background: linear-gradient(180deg, rgba(255, 255, 255, 0.82), rgba(249, 251, 255, 0.98));
  color: #b1bccd;
  display: grid;
  justify-items: center;
  align-content: center;
  gap: 10px;
  cursor: pointer;
  transition: border-color 0.16s ease, background-color 0.16s ease, color 0.16s ease;
}

.task-class-sidebar__create:hover {
  border-color: rgba(25, 95, 213, 0.22);
  background: #f7fbff;
  color: #6d7f99;
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
  0% {
    background-position: 200% 0;
  }

  100% {
    background-position: -200% 0;
  }
}
</style>
