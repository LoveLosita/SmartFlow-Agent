<script setup lang="ts">
import { computed } from 'vue'

import type { TaskItem } from '@/types/dashboard'
import { formatDeadline } from '@/utils/date'

const props = defineProps<{
  title: string
  caption: string
  count: number
  tone: 'danger' | 'primary' | 'warning' | 'slate'
  tasks: TaskItem[]
  emptyText: string
  loading?: boolean
}>()

const emit = defineEmits<{
  toggle: [task: TaskItem]
}>()

// 不再硬截断，全部展示；超出的部分通过 quadrant-list 的 max-height + overflow-y 滚动查看。
const visibleTasks = computed(() => props.tasks)
</script>

<template>
  <section class="quadrant-card" :class="`quadrant-card--${tone}`">
    <header class="quadrant-card__header">
      <div>
        <p class="quadrant-card__eyebrow">{{ caption }}</p>
        <h3>{{ title }}</h3>
      </div>
      <span class="quadrant-card__count">{{ count }}项</span>
    </header>

    <transition name="fade-switch" mode="out-in">
      <div v-if="loading" key="loading" class="quadrant-card__skeleton">
        <div v-for="index in 3" :key="index" class="quadrant-card__skeleton-item" />
      </div>

      <div v-else-if="visibleTasks.length === 0" key="empty" class="quadrant-card__empty">
        {{ emptyText }}
      </div>

      <TransitionGroup v-else tag="div" name="list-stagger" class="quadrant-list" key="list">
        <button
          v-for="task in visibleTasks"
          :key="task.id"
          type="button"
          class="quadrant-item"
          :class="{ 'quadrant-item--completed': task.is_completed }"
          @click="emit('toggle', task)"
        >
          <span class="quadrant-item__check">
            {{ task.is_completed ? '✓' : '' }}
          </span>
          <span class="quadrant-item__content">
            <strong>{{ task.title }}</strong>
            <small>{{ formatDeadline(task.deadline) }}</small>
          </span>
          <span class="quadrant-item__status">
            {{ task.is_completed ? '已完成' : '待处理' }}
          </span>
        </button>
      </TransitionGroup>
    </transition>
  </section>
</template>

<style scoped>
.quadrant-card {
  border-radius: 28px;
  padding: 22px;
  border: 1px solid rgba(17, 24, 39, 0.07);
  min-height: 260px;
}

.quadrant-card--danger {
  background: linear-gradient(180deg, #fff1f2 0%, #fff7f7 100%);
}

.quadrant-card--primary {
  background: linear-gradient(180deg, #eef7ff 0%, #f7fbff 100%);
}

.quadrant-card--warning {
  background: linear-gradient(180deg, #fff8df 0%, #fffdf1 100%);
}

.quadrant-card--slate {
  background: linear-gradient(180deg, #f2f5fb 0%, #f8fafc 100%);
}

.quadrant-card__header {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: 16px;
  margin-bottom: 18px;
}

.quadrant-card__eyebrow {
  margin: 0 0 8px;
  font-size: 13px;
  font-weight: 700;
  color: rgba(32, 50, 79, 0.72);
  letter-spacing: 0.06em;
  text-transform: uppercase;
}

.quadrant-card h3 {
  margin: 0;
  font-size: 28px;
  line-height: 1.1;
  letter-spacing: -0.03em;
}

.quadrant-card__count {
  min-width: 64px;
  padding: 8px 12px;
  border-radius: 999px;
  background: rgba(255, 255, 255, 0.78);
  font-size: 13px;
  font-weight: 700;
  color: #39506f;
  text-align: center;
}

.quadrant-list {
  display: grid;
  gap: 12px;
  /* 卡片 header 约 70px，列表区域最多约 320px（约 4 条可见），超出部分滚动 */
  max-height: 320px;
  overflow-y: auto;
  /* 滚动条样式：轨道透明，滑块圆角淡色，hover 加深 */
  scrollbar-width: thin;
  scrollbar-color: rgba(148, 163, 184, 0.32) transparent;
}

.quadrant-list::-webkit-scrollbar {
  width: 5px;
}

.quadrant-list::-webkit-scrollbar-track {
  background: transparent;
}

.quadrant-list::-webkit-scrollbar-thumb {
  border-radius: 999px;
  background: rgba(148, 163, 184, 0.32);
}

.quadrant-list::-webkit-scrollbar-thumb:hover {
  background: rgba(148, 163, 184, 0.52);
}

.quadrant-item,
.quadrant-card__skeleton-item {
  border-radius: 18px;
  min-height: 72px;
}

.quadrant-item {
  width: 100%;
  border: 1px solid rgba(17, 24, 39, 0.06);
  background: rgba(255, 255, 255, 0.92);
  display: grid;
  grid-template-columns: 34px minmax(0, 1fr) auto;
  gap: 14px;
  align-items: center;
  padding: 14px 16px;
  text-align: left;
  cursor: pointer;
  transition:
    transform 0.18s ease,
    box-shadow 0.18s ease,
    border-color 0.18s ease;
}

.quadrant-item:hover {
  transform: translateY(-2px);
  box-shadow: 0 14px 28px rgba(15, 23, 42, 0.08);
  border-color: rgba(37, 99, 235, 0.16);
}

.quadrant-item--completed {
  opacity: 0.82;
}

.quadrant-item__check {
  width: 28px;
  height: 28px;
  border-radius: 8px;
  border: 1px solid rgba(148, 163, 184, 0.3);
  background: #f8fafc;
  display: inline-flex;
  align-items: center;
  justify-content: center;
  color: #2c8d57;
  font-weight: 800;
}

.quadrant-item__content {
  min-width: 0;
}

.quadrant-item__content strong,
.quadrant-item__content small {
  display: block;
}

.quadrant-item__content strong {
  font-size: 16px;
  color: #122033;
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}

.quadrant-item--completed .quadrant-item__content strong {
  color: #96a0af;
  text-decoration: line-through;
}

.quadrant-item__content small {
  margin-top: 6px;
  color: #768396;
}

.quadrant-item__status {
  font-size: 13px;
  color: #8090a5;
  white-space: nowrap;
}

.quadrant-card__empty {
  min-height: 160px;
  display: flex;
  align-items: center;
  justify-content: center;
  color: #8b97a7;
  font-size: 16px;
}

.quadrant-card__skeleton {
  display: grid;
  gap: 12px;
}

.quadrant-card__skeleton-item {
  background: linear-gradient(90deg, rgba(230, 236, 244, 0.8), rgba(245, 248, 252, 1), rgba(230, 236, 244, 0.8));
  background-size: 200% 100%;
  animation: quadrant-shimmer 1.4s linear infinite;
}

@keyframes quadrant-shimmer {
  0% {
    background-position: 200% 0;
  }

  100% {
    background-position: -200% 0;
  }
}

.fade-switch-enter-active,
.fade-switch-leave-active {
  transition: opacity 0.25s ease, transform 0.25s ease;
}

.fade-switch-enter-from {
  opacity: 0;
  transform: translateY(4px);
}

.fade-switch-leave-to {
  opacity: 0;
  transform: translateY(-4px);
}

.list-stagger-enter-active,
.list-stagger-leave-active {
  transition: all 0.35s cubic-bezier(0.34, 1.56, 0.64, 1);
}

.list-stagger-enter-from {
  opacity: 0;
  transform: translateY(12px) scale(0.98);
}

.list-stagger-leave-to {
  opacity: 0;
  transform: translateX(12px) scale(0.98);
}

.list-stagger-leave-active {
  position: absolute;
}
</style>
