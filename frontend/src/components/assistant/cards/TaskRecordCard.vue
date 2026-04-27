<script setup lang="ts">
import { computed, inject } from 'vue'
import type { TaskRecordCardData, TaskRecordSource } from '@/api/schedule_agent'

const props = defineProps<{
  data: TaskRecordCardData
  source?: TaskRecordSource
  title?: string
  summary?: string
}>()

// 注入来自 AssistantPanel 的全局任务状态管理
const taskStatusMap = inject<Record<number, { 
  is_completed: boolean, 
  syncing: boolean, 
  is_deleted?: boolean,
  title?: string,
  priority_group?: number,
  deadline_at?: string | null
}>>('taskStatusMap')
const toggleTaskStatus = inject<(id: number) => Promise<void>>('toggleTaskStatus')
const onEditTask = inject<(task: any) => void>('onEditTask')
const onDeleteTask = inject<(id: number) => void>('onDeleteTask')

const isCompleted = (id: any, fallback: boolean = false) => {
  if (!id) return fallback
  const nid = Number(id)
  return taskStatusMap?.[nid]?.is_completed ?? fallback
}

const isSyncing = (id: any) => {
  if (!id) return false
  const nid = Number(id)
  return taskStatusMap?.[nid]?.syncing ?? false
}

const isDeleted = (id: any) => {
  if (!id) return false
  const nid = Number(id)
  return taskStatusMap?.[nid]?.is_deleted ?? false
}

// 实时获取覆盖后的属性
const displayTitle = computed(() => {
  const id = props.data.id ? Number(props.data.id) : null
  if (id && taskStatusMap?.[id]?.title) {
    return taskStatusMap[id].title
  }
  return props.data.title
})

const displayPriority = computed(() => {
  const id = props.data.id ? Number(props.data.id) : null
  if (id && taskStatusMap?.[id]?.priority_group) {
    return taskStatusMap[id].priority_group
  }
  return props.data.priority_group || 2
})

const displayDeadline = computed(() => {
  const id = props.data.id ? Number(props.data.id) : null
  if (id && taskStatusMap?.[id]?.deadline_at !== undefined) {
    return taskStatusMap[id].deadline_at
  }
  return props.data.deadline_at
})

// 对齐首页象限体系
const quadMeta: any = {
  1: { title: '重要且紧急', tone: 'danger', color: '#ef4444' },
  2: { title: '重要不紧急', tone: 'primary', color: '#3b82f6' },
  3: { title: '简单不重要', tone: 'warning', color: '#f59e0b' },
  4: { title: '不简单不重要', tone: 'slate', color: '#64748b' }
}

const getBgStyle = (group: number = 2) => {
  const bgMap: any = {
    1: 'linear-gradient(180deg, #fff1f2 0%, #fff7f7 100%)',
    2: 'linear-gradient(180deg, #eef7ff 0%, #f7fbff 100%)',
    3: 'linear-gradient(180deg, #fff8df 0%, #fffdf1 100%)',
    4: 'linear-gradient(180deg, #f2f5fb 0%, #f8fafc 100%)'
  }
  return bgMap[group] || bgMap[2]
}

const getTextColor = (group: number = 2) => {
  return quadMeta[group]?.color || '#3b82f6'
}
</script>

<template>
  <div 
    class="business-card creation-receipt" 
    :class="{ 'is-card-deleted': data.id && isDeleted(data.id) }"
    :style="{ background: getBgStyle(displayPriority) }"
  >
    <div class="receipt-inner">
      <div class="receipt-header">
        <div 
          class="success-ring" 
          :class="{ 
            'is-completed': isCompleted(data.id),
            'is-clickable': data.id && !isDeleted(data.id),
            'is-syncing': data.id && isSyncing(data.id)
          }"
          :style="{ 
            background: isDeleted(data.id) ? 'rgba(100, 116, 139, 0.1)' : (isCompleted(data.id) ? 'rgba(34, 197, 94, 0.15)' : 'rgba(239, 68, 68, 0.125)'),
            color: isDeleted(data.id) ? '#64748b' : (isCompleted(data.id) ? '#22c55e' : '#ef4448')
          }"
          @click.stop="data.id && !isDeleted(data.id) && toggleTaskStatus?.(data.id)"
        >
          <svg v-if="isDeleted(data.id)" width="20" height="20" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="3">
            <path d="M3 6h18M19 6v14a2 2 0 01-2 2H7a2 2 0 01-2-2V6m3 0V4a2 2 0 012-2h4a2 2 0 012 2v2"></path>
          </svg>
          <svg v-else-if="isSyncing(data.id)" class="spinner" width="20" height="20" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="3" stroke-linecap="round">
            <path d="M12 2v4m0 12v4M4.93 4.93l2.83 2.83m8.48 8.48l2.83 2.83M2 12h4m12 0h4M4.93 19.07l2.83-2.83m8.48-8.48l2.83-2.83"></path>
          </svg>
          <svg v-else width="20" height="20" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="3">
            <polyline points="20 6 9 17 4 12"></polyline>
          </svg>
        </div>
        <div class="success-msg">
          <strong v-if="isDeleted(data.id)">任务已删除</strong>
          <strong v-else-if="isCompleted(data.id)">任务已完成</strong>
          <strong v-else>已帮你记下</strong>
          <span v-if="!isDeleted(data.id)">归类至：{{ quadMeta[displayPriority]?.title || '重要不紧急' }}</span>
        </div>
      </div>

      <div 
        class="task-info-card" 
        :class="{ 
          'is-item-completed': data.id && isCompleted(data.id),
          'is-syncing': data.id && isSyncing(data.id),
          'is-deleted': data.id && isDeleted(data.id)
        }"
        @click="!isDeleted(data.id) && onEditTask?.({ ...data, title: displayTitle, priority_group: displayPriority, deadline_at: displayDeadline })"
      >
        <div class="task-main-row">
          <div class="item-check" v-if="data.id && !isDeleted(data.id)" @click.stop="toggleTaskStatus?.(data.id)">
            <div 
              class="check-circle" 
              :class="{ 
                'is-checked': isCompleted(data.id),
                'is-syncing': isSyncing(data.id)
              }"
              :style="{ 
                borderColor: getTextColor(displayPriority),
                backgroundColor: isCompleted(data.id) ? getTextColor(displayPriority) : 'transparent'
              }"
            >
              <svg v-if="isCompleted(data.id) && !isSyncing(data.id)" width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="white" stroke-width="4"><polyline points="20 6 9 17 4 12"/></svg>
              <div v-if="isSyncing(data.id)" class="sync-spinner"></div>
            </div>
          </div>
          <div class="task-title">{{ displayTitle }}</div>

          <div class="task-actions" v-if="data.id && !isDeleted(data.id)">
            <!-- 编辑按钮 -->
            <button 
              class="item-action-btn edit-btn" 
              @click.stop="onEditTask?.({ ...data, title: displayTitle, priority_group: displayPriority, deadline_at: displayDeadline })"
              title="编辑任务"
            >
              <svg viewBox="0 0 24 24" width="14" height="14" fill="none" stroke="currentColor" stroke-width="2.5"><path d="M11 4H4a2 2 0 00-2 2v14a2 2 0 002 2h14a2 2 0 002-2v-7"></path><path d="M18.5 2.5a2.121 2.121 0 013 3L12 15l-4 1 1-4 9.5-9.5z"></path></svg>
            </button>
            <!-- 删除按钮 -->
            <button 
              class="item-action-btn delete-btn" 
              @click.stop="onDeleteTask?.(data.id)"
              title="删除任务"
            >
              <svg viewBox="0 0 24 24" width="14" height="14" fill="none" stroke="currentColor" stroke-width="2.5"><path d="M3 6h18M19 6v14a2 2 0 01-2 2H7a2 2 0 01-2-2V6m3 0V4a2 2 0 012-2h4a2 2 0 012 2v2"></path></svg>
            </button>
          </div>
        </div>
        <div class="task-footer">
          <span class="task-id" v-if="data.id">ID: {{ data.id }}</span>
          <span class="task-time" v-if="data.created_at || displayDeadline">
            {{ displayDeadline ? '截止：' + displayDeadline : '刚刚创建' }}
          </span>
        </div>
      </div>
    </div>
  </div>
</template>

<style scoped>
.business-card {
  width: 100%;
  border-radius: 24px;
  border: 1px solid rgba(15, 23, 42, 0.08);
  box-shadow: 0 4px 20px rgba(15, 23, 42, 0.02);
  overflow: hidden;
  transition: all 0.3s cubic-bezier(0.4, 0, 0.2, 1);
  margin-bottom: 8px;
}

.business-card:hover {
  transform: translateY(-2px);
  box-shadow: 0 12px 40px rgba(15, 23, 42, 0.08);
  border-color: rgba(15, 23, 42, 0.12);
}

.receipt-inner {
  padding: 24px;
  display: flex;
  flex-direction: column;
  gap: 20px;
}

.receipt-header {
  display: flex;
  gap: 16px;
  align-items: center;
}

.success-ring {
  width: 38px;
  height: 38px;
  border-radius: 12px;
  display: flex;
  align-items: center;
  justify-content: center;
  flex-shrink: 0;
  transition: all 0.3s cubic-bezier(0.4, 0, 0.2, 1);
}

.success-ring.is-clickable {
  cursor: pointer;
}

.success-ring.is-clickable:hover {
  transform: scale(1.1) rotate(5deg);
  box-shadow: 0 4px 12px rgba(0, 0, 0, 0.05);
}

.success-ring.is-completed {
  background: rgba(34, 197, 94, 0.15) !important;
  color: #22c55e !important;
}

.spinner {
  animation: rotate 2s linear infinite;
}

@keyframes rotate {
  100% {
    transform: rotate(360deg);
  }
}

.success-msg {
  display: flex;
  flex-direction: column;
  gap: 2px;
}

.success-msg strong {
  font-size: 16px;
  font-weight: 850;
  color: #0f172a;
  letter-spacing: -0.01em;
}

.success-msg span {
  font-size: 12px;
  color: #64748b;
  font-weight: 600;
}

.task-info-card {
  background: rgba(255, 255, 255, 0.8);
  backdrop-filter: blur(8px);
  border: 1px solid rgba(255, 255, 255, 0.4);
  border-radius: 20px;
  padding: 20px;
  box-shadow: 0 4px 12px rgba(0, 0, 0, 0.02);
  transition: all 0.3s cubic-bezier(0.4, 0, 0.2, 1);
  cursor: pointer;
  position: relative;
  overflow: hidden;
}

.task-info-card:hover {
  transform: translateY(-2px);
  box-shadow: 0 8px 24px rgba(0, 0, 0, 0.08);
  border-color: rgba(37, 99, 235, 0.2);
}

.task-actions {
  margin-left: auto;
  display: flex;
  gap: 8px;
  opacity: 0;
  transition: all 0.2s;
}

.task-info-card:hover .task-actions {
  opacity: 1;
}

.item-action-btn {
  width: 30px;
  height: 30px;
  border-radius: 8px;
  border: none;
  background: rgba(15, 23, 42, 0.05);
  color: #64748b;
  display: flex;
  align-items: center;
  justify-content: center;
  cursor: pointer;
  transition: all 0.2s;
}

.item-action-btn.edit-btn:hover {
  background: #dbeafe;
  color: #2563eb;
  transform: scale(1.1);
}

.item-action-btn.delete-btn:hover {
  background: #fee2e2;
  color: #f43f5e;
  transform: scale(1.1);
}

.task-info-card.is-deleted {
  opacity: 0.6;
  filter: grayscale(0.5);
  cursor: not-allowed;
  pointer-events: none;
}

.is-card-deleted {
  opacity: 0.7;
}

.task-info-card.is-syncing {
  pointer-events: none;
  opacity: 0.8;
  position: relative;
  overflow: hidden;
}

.task-info-card.is-syncing::after {
  content: "";
  position: absolute;
  top: 0;
  left: 0;
  width: 100%;
  height: 100%;
  background: linear-gradient(
    90deg,
    transparent,
    rgba(255, 255, 255, 0.6),
    transparent
  );
  animation: shimmer 1.5s infinite;
}

@keyframes shimmer {
  0% { transform: translateX(-100%); }
  100% { transform: translateX(100%); }
}

.task-main-row {
  display: flex;
  gap: 12px;
  align-items: flex-start;
  margin-bottom: 12px;
}

.item-check {
  padding-top: 2px;
}

.check-circle {
  width: 18px;
  height: 18px;
  border-radius: 50%;
  border: 2px solid #e2e8f0;
  display: flex;
  align-items: center;
  justify-content: center;
  cursor: pointer;
  transition: all 0.2s cubic-bezier(0.4, 0, 0.2, 1);
  position: relative;
  flex-shrink: 0;
}

.check-circle.is-syncing {
  cursor: not-allowed;
  opacity: 0.7;
}

.sync-spinner {
  width: 10px;
  height: 10px;
  border: 1.5px solid rgba(0, 0, 0, 0.1);
  border-top-color: currentColor;
  border-radius: 50%;
  animation: spin 0.8s linear infinite;
}

@keyframes spin {
  to { transform: rotate(360deg); }
}

.is-item-completed .task-title {
  text-decoration: line-through;
  opacity: 0.5;
}

.task-title {
  font-size: 17px;
  font-weight: 800;
  color: #1e293b;
  margin: 0;
  line-height: 1.5;
  letter-spacing: -0.01em;
}

.task-footer {
  display: flex;
  justify-content: space-between;
  align-items: center;
  font-size: 11px;
  font-weight: 700;
  color: #94a3b8;
  border-top: 1px solid rgba(0, 0, 0, 0.04);
  padding-top: 12px;
}

.task-id {
  background: #f1f5f9;
  padding: 2px 8px;
  border-radius: 6px;
  color: #64748b;
}

.receipt-actions {
  display: grid;
  grid-template-columns: 1fr 1fr;
  gap: 12px;
}

.btn-outline {
  height: 44px;
  border: 1px solid #e2e8f0;
  background: #ffffff;
  border-radius: 14px;
  font-size: 13px;
  font-weight: 800;
  color: #475569;
  cursor: pointer;
  transition: all 0.2s;
}

.btn-outline:hover {
  background: #f8fafc;
  color: #0f172a;
  border-color: #cbd5e1;
}

.btn-fill {
  height: 44px;
  border: none;
  border-radius: 14px;
  color: white;
  font-size: 13px;
  font-weight: 850;
  cursor: pointer;
  box-shadow: 0 8px 24px rgba(0, 0, 0, 0.12);
  transition: all 0.2s;
}

.btn-fill:hover {
  filter: brightness(1.05);
  transform: translateY(-1px);
  box-shadow: 0 12px 28px rgba(0, 0, 0, 0.15);
}
</style>
