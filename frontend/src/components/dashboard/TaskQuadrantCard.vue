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
  edit: [task: TaskItem]
  delete: [task: TaskItem]
}>()

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
        <div class="empty-placeholder">
           <svg class="empty-icon" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5">
             <circle cx="12" cy="12" r="10"></circle>
             <path d="M12 8v1.5M12 12.5V16"></path>
           </svg>
           <p>{{ emptyText }}</p>
        </div>
      </div>

      <TransitionGroup v-else tag="div" name="list-stagger" class="quadrant-list" key="list">
        <div
          v-for="task in visibleTasks"
          :key="task.id"
          class="quadrant-item"
          :class="{ 'quadrant-item--completed': task.is_completed }"
        >
          <!-- 区域1: 独立勾选框 (阻止冒泡) -->
          <button 
            type="button"
            class="quadrant-item__check-btn"
            @click.stop="emit('toggle', task)"
          >
            <span class="quadrant-item__check">
              <svg v-if="task.is_completed" viewBox="0 0 24 24" width="14" height="14" fill="none" stroke="currentColor" stroke-width="4">
                <polyline points="20 6 9 17 4 12"></polyline>
              </svg>
            </span>
          </button>
          
          <!-- 区域2: 文本内容 (点击触发编辑) -->
          <div class="quadrant-item__body" @click="emit('edit', task)">
            <strong class="quadrant-item__title">{{ task.title }}</strong>
            <small class="quadrant-item__time">
               <svg viewBox="0 0 24 24" width="12" height="12" fill="none" stroke="currentColor" stroke-width="2"><circle cx="12" cy="12" r="10"></circle><polyline points="12 6 12 12 16 14"></polyline></svg>
               {{ formatDeadline(task.deadline) }}
            </small>
          </div>

          <!-- 区域3: 悬浮操作区 (平滑滑入) -->
          <div class="quadrant-item__actions">
             <button class="action-btn delete" @click.stop="emit('delete', task)" title="删除任务">
                <svg viewBox="0 0 24 24" width="16" height="16" fill="none" stroke="currentColor" stroke-width="2.5"><path d="M3 6h18M19 6v14a2 2 0 01-2 2H7a2 2 0 01-2-2V6m3 0V4a2 2 0 012-2h4a2 2 0 012 2v2"></path></svg>
             </button>
          </div>
        </div>
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
  border-radius: 99px;
  background: rgba(255, 255, 255, 0.78);
  font-size: 13px;
  font-weight: 700;
  color: #39506f;
  text-align: center;
}

.quadrant-list {
  display: grid;
  gap: 12px;
  max-height: 320px;
  overflow-y: auto;
  scrollbar-width: thin;
  scrollbar-color: rgba(148, 163, 184, 0.32) transparent;
}

.quadrant-list::-webkit-scrollbar { width: 5px; }
.quadrant-list::-webkit-scrollbar-track { background: transparent; }
.quadrant-list::-webkit-scrollbar-thumb { border-radius: 999px; background: rgba(148, 163, 184, 0.32); }

.quadrant-card__empty {
  flex: 1;
  display: flex;
  align-items: center;
  justify-content: center;
  padding: 30px 10px;
}

.empty-placeholder {
  width: 100%;
  height: 100%;
  min-height: 150px;
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  text-align: center;
  color: #94a3b8;
  transition: all 0.4s ease;
}

.empty-placeholder:hover {
  transform: translateY(-2px);
}

.empty-icon {
  width: 40px;
  height: 40px;
  margin-bottom: 12px;
  opacity: 0.15; /* 极低不透明度，实现融合感 */
  color: #64748b;
  stroke-width: 1.2;
}

.empty-placeholder p {
  margin: 0;
  font-size: 13px;
  font-weight: 600;
  line-height: 1.6;
  max-width: 180px;
  color: #64748b;
  opacity: 0.35; /* 文字也保持半透明融合 */
  letter-spacing: 0.02em;
}

.quadrant-item {
  width: 100%;
  position: relative;
  border-radius: 18px;
  min-height: 72px;
  border: 1px solid rgba(17, 24, 39, 0.06);
  background: rgba(255, 255, 255, 0.95);
  display: flex;
  align-items: center;
  padding: 14px 16px;
  transition: all 0.2s cubic-bezier(0.4, 0, 0.2, 1);
  overflow: hidden;
}

.quadrant-item:hover {
  transform: translateY(-2px);
  box-shadow: 0 12px 24px -8px rgba(15, 23, 42, 0.12);
  border-color: rgba(37, 99, 235, 0.16);
}

.quadrant-item--completed {
  opacity: 0.82;
}

/* 区域1: 勾选按钮 */
.quadrant-item__check-btn {
  border: none;
  background: transparent;
  padding: 0;
  margin-right: 14px;
  cursor: pointer;
  display: flex;
  align-items: center;
}

.quadrant-item__check {
  width: 28px;
  height: 28px;
  border-radius: 50%;
  border: 2px solid #e2e8f0;
  background: #fff;
  display: inline-flex;
  align-items: center;
  justify-content: center;
  color: #fff;
  transition: all 0.2s;
}

.quadrant-item:hover .quadrant-item__check {
  border-color: #3b82f6;
}

.quadrant-item--completed .quadrant-item__check {
  background: #10b981;
  border-color: #10b981;
}

/* 区域2: 主体文字 */
.quadrant-item__body {
  flex: 1;
  min-width: 0;
  cursor: pointer;
}

.quadrant-item__title {
  display: block;
  font-size: 16px;
  color: #122033;
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
  margin-bottom: 4px;
  transition: all 0.3s;
}

.quadrant-item--completed .quadrant-item__title {
  color: #96a0af;
  text-decoration: line-through;
}

.quadrant-item__time {
  display: flex;
  align-items: center;
  gap: 4px;
  color: #768396;
  font-size: 12px;
}

/* 区域3: 悬浮动作条 */
.quadrant-item__actions {
  position: absolute;
  right: -56px;
  top: 0;
  bottom: 0;
  width: 56px;
  background: rgba(255, 255, 255, 0.85);
  backdrop-filter: blur(8px);
  display: flex;
  align-items: center;
  justify-content: center;
  border-left: 1px solid rgba(0, 0, 0, 0.03);
  transition: all 0.3s cubic-bezier(0.34, 1.56, 0.64, 1);
  opacity: 0;
}

.quadrant-item:hover .quadrant-item__actions {
  right: 0;
  opacity: 1;
}

.action-btn {
  width: 36px;
  height: 36px;
  border: none;
  border-radius: 12px;
  background: transparent;
  cursor: pointer;
  display: flex;
  align-items: center;
  justify-content: center;
  transition: all 0.2s;
}

.action-btn.delete { color: #f43f5e; }
.action-btn.delete:hover { background: #fee2e2; transform: scale(1.1); }

/* --- 骨架屏 --- */
.quadrant-card__skeleton {
  display: grid;
  gap: 12px;
}

.quadrant-card__skeleton-item {
  border-radius: 18px;
  min-height: 72px;
  background: linear-gradient(90deg, rgba(230, 236, 244, 0.8), rgba(245, 248, 252, 1), rgba(230, 236, 244, 0.8));
  background-size: 200% 100%;
  animation: quadrant-shimmer 1.4s linear infinite;
}

@keyframes quadrant-shimmer {
  0% { background-position: 200% 0; }
  100% { background-position: -200% 0; }
}

/* --- 动画 --- */
.fade-switch-enter-active, .fade-switch-leave-active { transition: opacity 0.25s ease, transform 0.25s ease; }
.fade-switch-enter-from { opacity: 0; transform: translateY(4px); }
.fade-switch-leave-to { opacity: 0; transform: translateY(-4px); }

.list-stagger-enter-active, .list-stagger-leave-active { transition: all 0.35s cubic-bezier(0.34, 1.56, 0.64, 1); }
.list-stagger-enter-from { opacity: 0; transform: translateY(12px) scale(0.98); }
.list-stagger-leave-to { opacity: 0; transform: translateX(12px) scale(0.98); }
.list-stagger-leave-active { position: absolute; }
</style>
