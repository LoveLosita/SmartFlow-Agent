<script setup lang="ts">
/**
 * 排程结果展示卡片
 * 显示在 AI 助手的消息流中，告知用户排程已就绪。
 */
defineProps<{
  summary: string
}>()

const emit = defineEmits<{
  (e: 'click'): void
}>()
</script>

<template>
  <div class="schedule-result-card" @click="emit('click')">
    <div class="schedule-result-card__icon">
      <svg width="20" height="20" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
        <rect x="3" y="4" width="18" height="18" rx="2" ry="2"></rect>
        <line x1="16" y1="2" x2="16" y2="6"></line>
        <line x1="8" y1="2" x2="8" y2="6"></line>
        <line x1="3" y1="10" x2="21" y2="10"></line>
      </svg>
    </div>
    <div class="schedule-result-card__content">
      <h4 class="schedule-result-card__summary">{{ summary }}</h4>
      <p class="schedule-result-card__detail">点击查看排程方案并进行微调</p>
    </div>
    <div class="schedule-result-card__arrow">
      <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round" stroke-linejoin="round">
        <polyline points="9 18 15 12 9 6"></polyline>
      </svg>
    </div>
  </div>
</template>

<style scoped>
.schedule-result-card {
  display: flex;
  align-items: center;
  gap: 16px;
  padding: 16px;
  background: #ffffff;
  border: 1px solid rgba(15, 23, 42, 0.08);
  border-radius: 16px;
  cursor: pointer;
  transition: all 0.3s cubic-bezier(0.34, 1.56, 0.64, 1);
  box-shadow: 0 4px 12px rgba(15, 23, 42, 0.03);
  margin: 8px 0;
}

.schedule-result-card:hover {
  transform: translateY(-2px);
  border-color: #3b82f6;
  box-shadow: 0 10px 20px -5px rgba(59, 130, 246, 0.15);
}

.schedule-result-card__icon {
  width: 44px;
  height: 44px;
  display: flex;
  align-items: center;
  justify-content: center;
  background: #eff6ff;
  color: #3b82f6;
  border-radius: 12px;
  flex-shrink: 0;
}

.schedule-result-card__content {
  flex: 1;
  min-width: 0;
}

.schedule-result-card__summary {
  margin: 0 0 4px;
  font-size: 15px;
  font-weight: 700;
  color: #1e293b;
}

.schedule-result-card__detail {
  margin: 0;
  font-size: 13px;
  color: #64748b;
}

.schedule-result-card__arrow {
  color: #cbd5e1;
  transition: transform 0.3s;
}

.schedule-result-card:hover .schedule-result-card__arrow {
  transform: translateX(4px);
  color: #3b82f6;
}
</style>
