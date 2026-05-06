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
  border: 1px solid rgba(15, 23, 42, 0.06);
  border-radius: 20px;
  cursor: pointer;
  transition: all 0.2s ease;
  margin: 12px 0;
  position: relative;
  overflow: hidden;
  /* 弹出动画 */
}

.schedule-result-card:hover {
  border-color: #3b82f6;
  background: #fcfdfe;
  box-shadow: 0 4px 20px rgba(59, 130, 246, 0.04);
}

.schedule-result-card__icon {
  width: 48px;
  height: 48px;
  display: flex;
  align-items: center;
  justify-content: center;
  background: linear-gradient(135deg, #eff6ff 0%, #dbebff 100%);
  color: #3b82f6;
  border-radius: 14px;
  flex-shrink: 0;
  transition: transform 0.3s ease;
}

.schedule-result-card:hover .schedule-result-card__icon {
  transform: rotate(-5deg) scale(1.05);
}

.schedule-result-card__content {
  flex: 1;
  min-width: 0;
}

.schedule-result-card__summary {
  margin: 0 0 4px;
  font-size: 15px;
  font-weight: 850;
  color: #0f172a;
  letter-spacing: -0.01em;
}

.schedule-result-card__detail {
  margin: 0;
  font-size: 13px;
  font-weight: 500;
  color: #64748b;
}

.schedule-result-card__arrow {
  width: 32px;
  height: 32px;
  display: flex;
  align-items: center;
  justify-content: center;
  background: #f8fafc;
  border-radius: 10px;
  color: #94a3b8;
  transition: all 0.3s;
}

.schedule-result-card:hover .schedule-result-card__arrow {
  transform: translateX(4px);
  background: #3b82f6;
  color: #ffffff;
}
</style>
