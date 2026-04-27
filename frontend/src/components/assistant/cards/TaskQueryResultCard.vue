<script setup lang="ts">
import type { TaskQueryCardData } from '@/api/schedule_agent'

const props = defineProps<{
  data: TaskQueryCardData
  title?: string
  summary?: string
}>()

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
  <div class="business-card query-results" :style="{ background: getBgStyle(props.data.tasks[0]?.priority_group) }">
    <header class="card-header">
      <div class="header-left">
        <p class="eyebrow">{{ summary || '查询结果' }}</p>
        <h3>{{ title || '为您找到以下任务' }}</h3>
      </div>
      <div class="count-badge" v-if="data.result_count > 0">
        {{ data.result_count }} 项
      </div>
    </header>

    <div class="card-content">
      <div v-if="data.tasks && data.tasks.length > 0" class="task-items">
        <div v-for="task in data.tasks" :key="task.id" class="task-item">
          <div class="item-check">
            <div class="check-circle" :style="{ borderColor: getTextColor(task.priority_group) }"></div>
          </div>
          <div class="item-body">
            <div class="item-title">{{ task.title }}</div>
            <div class="item-meta">
              <span 
                class="q-pill" 
                v-if="task.priority_group"
                :style="{ color: getTextColor(task.priority_group), background: getTextColor(task.priority_group) + '10' }"
              >
                Q{{ task.priority_group }} {{ quadMeta[task.priority_group]?.title }}
              </span>
              <span v-if="task.deadline_at" class="time-pill">
                <svg width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><circle cx="12" cy="12" r="10"/><polyline points="12 6 12 12 16 14"/></svg>
                {{ task.deadline_at }}
              </span>
            </div>
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
  max-width: 400px;
  border-radius: 28px;
  border: 1px solid rgba(17, 24, 39, 0.08);
  box-shadow: 0 4px 20px rgba(0,0,0,0.02);
  overflow: hidden;
  transition: all 0.3s;
  background: white;
}

.business-card:hover {
  transform: translateY(-2px);
  box-shadow: 0 12px 40px rgba(15, 23, 42, 0.06);
}

.card-header {
  padding: 24px 24px 16px;
  display: flex;
  justify-content: space-between;
  align-items: flex-start;
}

.eyebrow {
  font-size: 11px;
  font-weight: 800;
  color: rgba(30, 41, 59, 0.5);
  text-transform: uppercase;
  letter-spacing: 0.1em;
  margin: 0 0 6px 0;
}

.card-header h3 {
  font-size: 20px;
  font-weight: 850;
  color: #1e293b;
  margin: 0;
  line-height: 1.2;
  letter-spacing: -0.02em;
}

.count-badge {
  padding: 4px 12px;
  background: rgba(255, 255, 255, 0.8);
  border-radius: 100px;
  font-size: 11px;
  font-weight: 700;
  color: #475569;
  box-shadow: 0 2px 8px rgba(0,0,0,0.02);
}

.task-items {
  padding: 0 16px;
  display: flex;
  flex-direction: column;
  gap: 8px;
}

.task-item {
  background: rgba(255, 255, 255, 0.95);
  border: 1px solid rgba(0,0,0,0.04);
  border-radius: 18px;
  padding: 14px 16px;
  display: flex;
  gap: 14px;
  align-items: center;
}

.check-circle {
  width: 20px;
  height: 20px;
  border-radius: 50%;
  border: 2px solid #e2e8f0;
  flex-shrink: 0;
}

.item-body {
  flex: 1;
  min-width: 0;
}

.item-title {
  font-size: 14px;
  font-weight: 700;
  color: #122033;
  margin-bottom: 4px;
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}

.item-meta {
  display: flex;
  gap: 10px;
  align-items: center;
}

.q-pill {
  font-size: 9px;
  font-weight: 800;
  padding: 1px 6px;
  border-radius: 4px;
}

.time-pill {
  font-size: 9px;
  color: #94a3b8;
  display: flex;
  align-items: center;
  gap: 4px;
  font-weight: 500;
}

.btn-more {
  width: calc(100% - 32px);
  margin: 16px 16px 20px;
  padding: 12px;
  border: none;
  background: rgba(255, 255, 255, 0.6);
  border-radius: 14px;
  font-size: 12px;
  font-weight: 800;
  color: #475569;
  cursor: pointer;
  transition: all 0.2s;
}

.btn-more:hover {
  background: white;
}

.empty-state {
  padding: 32px 16px;
  text-align: center;
  color: #94a3b8;
}

.empty-icon {
  margin-bottom: 8px;
  opacity: 0.5;
}

.empty-state p {
  font-size: 13px;
  font-weight: 600;
  margin: 0;
}
</style>
