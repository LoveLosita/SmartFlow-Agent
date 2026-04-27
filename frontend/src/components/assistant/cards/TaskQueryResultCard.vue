<script setup lang="ts">
import { computed, inject } from 'vue'
import type { TaskQueryCardData } from '@/api/schedule_agent'

const props = defineProps<{
  data: TaskQueryCardData
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

const isCompleted = (id: number, fallback: boolean = false) => {
  return taskStatusMap?.[id]?.is_completed ?? fallback
}

const isSyncing = (id: number) => {
  return taskStatusMap?.[id]?.syncing ?? false
}

const isDeleted = (id: number) => {
  return taskStatusMap?.[id]?.is_deleted ?? false
}

// 实时获取覆盖后的属性
const getDisplayTitle = (task: any) => {
  if (task?.id && taskStatusMap?.[task.id]?.title) {
    return taskStatusMap[task.id].title
  }
  return task?.title || ''
}

const getDisplayPriority = (task: any) => {
  if (task?.id && taskStatusMap?.[task.id]?.priority_group) {
    return taskStatusMap[task.id].priority_group
  }
  return task?.priority_group || 2
}

const getDisplayDeadline = (task: any) => {
  if (task?.id && taskStatusMap?.[task.id]?.deadline_at !== undefined) {
    return taskStatusMap[task.id].deadline_at
  }
  return task?.deadline_at
}

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
  <div class="business-card query-results" :style="{ background: getBgStyle(props.data.tasks?.[0] ? getDisplayPriority(props.data.tasks[0]) : 2) }">
    <header class="card-header">
      <div class="header-left">
        <p class="eyebrow">查询结果</p>
        <h3 class="card-title">{{ title || '为您找到以下任务' }}</h3>
        
        <div class="filter-tags" v-if="data.query_filters && data.query_filters.length > 0">
          <span v-for="f in data.query_filters" :key="f.key" class="filter-tag">
            <template v-if="f.key === 'deadline_after' || f.key === 'deadline_before'">
              <svg width="10" height="10" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" class="tag-icon"><circle cx="12" cy="12" r="10"/><polyline points="12 6 12 12 16 14"/></svg>
            </template>
            <template v-else-if="f.key === 'sort'">
              <svg width="10" height="10" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" class="tag-icon"><line x1="3" y1="6" x2="21" y2="6"/><line x1="3" y1="12" x2="21" y2="12"/><line x1="3" y1="18" x2="21" y2="18"/></svg>
            </template>
            <template v-else>
              <svg width="10" height="10" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" class="tag-icon"><polygon points="22 3 2 3 10 12.46 10 19 14 21 14 12.46 22 3"/></svg>
            </template>
            {{ f.display_text }}
          </span>
        </div>
        
        <p v-else-if="summary" class="query-summary-fallback">{{ summary }}</p>
      </div>
      <div class="count-badge" v-if="data.result_count > 0">
        {{ data.result_count }} 项
      </div>
    </header>

    <div class="card-content">
      <div v-if="data.tasks && data.tasks.length > 0" class="task-items">
        <div 
          v-for="task in data.tasks" 
          :key="task.id" 
          class="task-item" 
          :class="{ 
            'is-item-completed': isCompleted(task.id, task.is_completed),
            'is-syncing': isSyncing(task.id),
            'is-deleted': isDeleted(task.id)
          }"
          @click="!isDeleted(task.id) && onEditTask?.({ ...task, title: getDisplayTitle(task), priority_group: getDisplayPriority(task), deadline_at: getDisplayDeadline(task) })"
        >
          <div class="item-check" v-if="!isDeleted(task.id)" @click.stop="toggleTaskStatus?.(task.id)">
            <div 
              class="check-circle" 
              :class="{ 
                'is-checked': isCompleted(task.id, task.is_completed),
                'is-syncing': isSyncing(task.id)
              }"
              :style="{ 
                borderColor: getTextColor(getDisplayPriority(task)),
                backgroundColor: isCompleted(task.id, task.is_completed) ? getTextColor(getDisplayPriority(task)) : 'transparent'
              }"
            >
              <svg v-if="isCompleted(task.id, task.is_completed) && !isSyncing(task.id)" width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="white" stroke-width="4"><polyline points="20 6 9 17 4 12"/></svg>
              <div v-if="isSyncing(task.id)" class="sync-spinner"></div>
            </div>
          </div>
          <div class="item-body">
            <div class="item-title">{{ isDeleted(task.id) ? '（任务已删除）' : getDisplayTitle(task) }}</div>
            <div class="item-meta" v-if="!isDeleted(task.id)">
              <span 
                class="q-pill" 
                v-if="getDisplayPriority(task)"
                :style="{ color: getTextColor(getDisplayPriority(task)), background: getTextColor(getDisplayPriority(task)) + '10' }"
              >
                Q{{ getDisplayPriority(task) }} {{ quadMeta[getDisplayPriority(task)]?.title }}
              </span>
              <span v-if="getDisplayDeadline(task)" class="time-pill">
                <svg width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><circle cx="12" cy="12" r="10"/><polyline points="12 6 12 12 16 14"/></svg>
                {{ getDisplayDeadline(task) }}
              </span>
            </div>
          </div>

          <div class="task-actions" v-if="!isDeleted(task.id)">
            <button 
              class="item-action-btn edit-btn" 
              @click.stop="onEditTask?.({ ...task, title: getDisplayTitle(task), priority_group: getDisplayPriority(task), deadline_at: getDisplayDeadline(task) })"
              title="编辑任务"
            >
              <svg viewBox="0 0 24 24" width="12" height="12" fill="none" stroke="currentColor" stroke-width="2.5"><path d="M11 4H4a2 2 0 00-2 2v14a2 2 0 002 2h14a2 2 0 002-2v-7"></path><path d="M18.5 2.5a2.121 2.121 0 013 3L12 15l-4 1 1-4 9.5-9.5z"></path></svg>
            </button>
            <button 
              class="item-action-btn delete-btn" 
              @click.stop="onDeleteTask?.(task.id)"
              title="删除任务"
            >
              <svg viewBox="0 0 24 24" width="12" height="12" fill="none" stroke="currentColor" stroke-width="2.5"><path d="M3 6h18M19 6v14a2 2 0 01-2 2H7a2 2 0 01-2-2V6m3 0V4a2 2 0 012-2h4a2 2 0 012 2v2"></path></svg>
            </button>
          </div>
        </div>
      </div>
      
      <div v-else class="empty-state">
        <div class="empty-icon">
          <svg width="40" height="40" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5"><circle cx="12" cy="12" r="10"/><path d="M10 10l4 4m0-4l-4 4"/></svg>
        </div>
        <p>暂无符合条目</p>
      </div>

      <button v-if="data.has_more" class="btn-more">查看完整列表</button>
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
  background: #ffffff;
  margin-bottom: 8px;
}

.business-card:hover {
  transform: translateY(-2px);
  box-shadow: 0 12px 40px rgba(15, 23, 42, 0.08);
  border-color: rgba(15, 23, 42, 0.12);
}

.card-header {
  padding: 20px 24px 16px;
  display: flex;
  justify-content: space-between;
  align-items: flex-start;
  border-bottom: 1px solid rgba(0, 0, 0, 0.03);
}

.header-left {
  flex: 1;
}

.eyebrow {
  font-size: 11px;
  font-weight: 800;
  color: #64748b;
  text-transform: uppercase;
  letter-spacing: 0.1em;
  margin: 0 0 4px 0;
  opacity: 0.8;
}

.card-header h3.card-title {
  font-size: 18px;
  font-weight: 850;
  color: #0f172a;
  margin: 0 0 10px 0;
  line-height: 1.3;
  letter-spacing: -0.01em;
}

.filter-tags {
  display: flex;
  flex-wrap: wrap;
  gap: 6px;
}

.filter-tag {
  display: inline-flex;
  align-items: center;
  gap: 4px;
  padding: 4px 10px;
  background: rgba(15, 23, 42, 0.05);
  border: 1px solid rgba(15, 23, 42, 0.03);
  border-radius: 8px;
  font-size: 11px;
  font-weight: 700;
  color: #475569;
  transition: all 0.2s;
}

.filter-tag:hover {
  background: rgba(15, 23, 42, 0.08);
  color: #1e293b;
}

.tag-icon {
  opacity: 0.6;
}

.query-summary-fallback {
  font-size: 12px;
  color: #64748b;
  margin: 4px 0 0;
  line-height: 1.4;
  font-weight: 600;
}

.count-badge {
  padding: 4px 10px;
  background: #f1f5f9;
  border-radius: 999px;
  font-size: 11px;
  font-weight: 700;
  color: #475569;
  margin-left: 12px;
  flex-shrink: 0;
}

.card-content {
  padding: 16px;
}

.task-items {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(280px, 1fr));
  gap: 12px;
}

.task-item {
  background: #ffffff;
  border: 1px solid rgba(15, 23, 42, 0.06);
  border-radius: 16px;
  padding: 14px 16px;
  display: flex;
  gap: 12px;
  align-items: center;
  transition: all 0.2s;
  box-shadow: 0 2px 8px rgba(0, 0, 0, 0.02);
}

.task-item:hover {
  border-color: rgba(15, 23, 42, 0.15);
  background: #f8fafc;
  transform: scale(1.01);
}

.task-actions {
  display: flex;
  gap: 6px;
  opacity: 0;
  transition: all 0.2s;
}

.task-item:hover .task-actions {
  opacity: 1;
}

.item-action-btn {
  width: 26px;
  height: 26px;
  border-radius: 7px;
  border: none;
  background: rgba(15, 23, 42, 0.04);
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

.task-item.is-deleted {
  opacity: 0.5;
  filter: grayscale(0.8);
  cursor: not-allowed;
  pointer-events: none;
}

.task-item.is-syncing {
  pointer-events: none;
  opacity: 0.85;
  background: #f8fafc;
  position: relative;
  overflow: hidden;
}

.task-item.is-syncing::after {
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

.item-check {
  flex-shrink: 0;
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

.is-item-completed .item-title {
  text-decoration: line-through;
  opacity: 0.5;
}

.item-body {
  flex: 1;
  min-width: 0;
}

.item-title {
  font-size: 14px;
  font-weight: 700;
  color: #1e293b;
  margin-bottom: 6px;
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}

.item-meta {
  display: flex;
  gap: 8px;
  align-items: center;
  flex-wrap: wrap;
}

.q-pill {
  font-size: 10px;
  font-weight: 750;
  padding: 2px 8px;
  border-radius: 6px;
  white-space: nowrap;
}

.time-pill {
  font-size: 10px;
  color: #64748b;
  display: flex;
  align-items: center;
  gap: 4px;
  font-weight: 600;
}

.btn-more {
  width: 100%;
  margin-top: 16px;
  padding: 10px;
  border: 1px solid #e2e8f0;
  background: #ffffff;
  border-radius: 12px;
  font-size: 12px;
  font-weight: 750;
  color: #64748b;
  cursor: pointer;
  transition: all 0.2s;
}

.btn-more:hover {
  background: #f8fafc;
  color: #0f172a;
  border-color: #cbd5e1;
}

.empty-state {
  padding: 40px 24px;
  text-align: center;
  color: #94a3b8;
}

.empty-icon {
  margin-bottom: 12px;
  opacity: 0.4;
  display: flex;
  justify-content: center;
}

.empty-state p {
  font-size: 14px;
  font-weight: 600;
  margin: 0;
}
</style>
