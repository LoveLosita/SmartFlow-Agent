<script setup lang="ts">
import type { TaskRecordCardData, TaskRecordSource } from '@/api/schedule_agent'

const props = defineProps<{
  data: TaskRecordCardData
  source?: TaskRecordSource
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
  <div class="business-card creation-receipt" :style="{ background: getBgStyle(props.data.priority_group) }">
    <div class="receipt-inner">
      <div class="receipt-header">
        <div class="success-ring" :style="{ background: getTextColor(props.data.priority_group) + '20', color: getTextColor(props.data.priority_group) }">
          <svg width="20" height="20" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="3"><polyline points="20 6 9 17 4 12"/></svg>
        </div>
        <div class="success-msg">
          <strong>{{ title || (source === 'quick_note' ? '已帮您记下' : '任务已创建') }}</strong>
          <span v-if="data.priority_group">归类至：{{ quadMeta[data.priority_group].title }}</span>
        </div>
      </div>

      <div class="task-info-card">
        <div class="task-title">{{ data.title }}</div>
        <div class="task-footer">
          <span class="task-id" v-if="data.id">ID: {{ data.id }}</span>
          <span class="task-time" v-if="data.created_at || data.deadline_at">
            {{ data.deadline_at ? '截止：' + data.deadline_at : '刚刚创建' }}
          </span>
        </div>
      </div>

      <div class="receipt-actions">
        <button class="btn-outline">修改详情</button>
        <button class="btn-fill" :style="{ background: getTextColor(props.data.priority_group) }">打开查看</button>
      </div>
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
}

.receipt-inner {
  padding: 24px;
  display: flex;
  flex-direction: column;
  gap: 20px;
}

.receipt-header {
  display: flex;
  gap: 14px;
  align-items: center;
}

.success-ring {
  width: 44px;
  height: 44px;
  border-radius: 50%;
  display: flex;
  align-items: center;
  justify-content: center;
  flex-shrink: 0;
}

.success-msg {
  display: flex;
  flex-direction: column;
}

.success-msg strong {
  font-size: 15px;
  font-weight: 850;
  color: #0f172a;
}

.success-msg span {
  font-size: 12px;
  color: #64748b;
  font-weight: 500;
}

.task-info-card {
  background: rgba(255, 255, 255, 0.95);
  border: 1px solid rgba(0,0,0,0.03);
  border-radius: 20px;
  padding: 20px;
  box-shadow: 0 4px 12px rgba(0,0,0,0.01);
}

.task-title {
  font-size: 16px;
  font-weight: 800;
  color: #1e293b;
  margin-bottom: 12px;
  line-height: 1.4;
}

.task-footer {
  display: flex;
  justify-content: space-between;
  font-size: 11px;
  font-weight: 600;
  color: #94a3b8;
  border-top: 1px solid #f1f5f9;
  padding-top: 10px;
}

.receipt-actions {
  display: grid;
  grid-template-columns: 1fr 1fr;
  gap: 10px;
}

.btn-outline {
  height: 42px;
  border: 1px solid #e2e8f0;
  background: white;
  border-radius: 12px;
  font-size: 13px;
  font-weight: 750;
  color: #475569;
  cursor: pointer;
  transition: all 0.2s;
}

.btn-outline:hover {
  background: #f8fafc;
}

.btn-fill {
  height: 42px;
  border: none;
  border-radius: 12px;
  color: white;
  font-size: 13px;
  font-weight: 800;
  cursor: pointer;
  box-shadow: 0 8px 20px rgba(0,0,0,0.1);
  transition: all 0.2s;
}

.btn-fill:hover {
  filter: brightness(1.1);
  transform: translateY(-1px);
}
</style>
