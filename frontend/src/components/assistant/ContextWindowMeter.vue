<script setup lang="ts">
import { computed } from 'vue'

import type { ConversationContextStats } from '@/types/dashboard'

const props = withDefaults(
  defineProps<{
    stats?: ConversationContextStats | null
    loading?: boolean
    disabled?: boolean
  }>(),
  {
    stats: null,
    loading: false,
    disabled: false,
  },
)

const safeStats = computed(() => props.stats ?? null)

const usagePercent = computed(() => {
  if (!safeStats.value || safeStats.value.budget <= 0) {
    return 0
  }
  return Math.round((safeStats.value.total / safeStats.value.budget) * 100)
})

const barWidthPercent = computed(() => {
  if (!safeStats.value || safeStats.value.budget <= 0) {
    return 0
  }
  // 1. 按 total / budget 计算宽度，上限 100%（超预算时撑满进度条）。
  return Math.min(100, (safeStats.value.total / safeStats.value.budget) * 100)
})

const isOverBudget = computed(() => {
  if (!safeStats.value) {
    return false
  }
  return safeStats.value.total > safeStats.value.budget
})

const usageText = computed(() => {
  if (props.loading) {
    return '...'
  }

  if (!safeStats.value) {
    return props.disabled ? '--' : '空'
  }

  return `${usagePercent.value}%`
})

const tooltipText = computed(() => {
  if (props.loading) {
    return '正在读取当前会话的上下文窗口统计'
  }

  if (!safeStats.value) {
    return props.disabled ? '新会话发送首条消息后展示上下文窗口统计' : '当前会话暂无上下文窗口统计'
  }

  return `总计 ${safeStats.value.total} / 预算 ${safeStats.value.budget}（${usagePercent.value}%）`
})
</script>

<template>
  <div
    class="assistant-context-meter"
    :class="{
      'assistant-context-meter--loading': loading,
      'assistant-context-meter--disabled': disabled,
      'assistant-context-meter--danger': isOverBudget,
    }"
    :title="tooltipText"
  >
    <span class="assistant-context-meter__label">窗口</span>

    <div class="assistant-context-meter__track" aria-hidden="true">
      <div v-if="loading" class="assistant-context-meter__loading-bar" />
      <div v-else-if="barWidthPercent > 0" class="assistant-context-meter__bar" :style="{ width: `${barWidthPercent}%` }" />
    </div>

    <span class="assistant-context-meter__value">{{ usageText }}</span>
  </div>
</template>

<style scoped>
.assistant-context-meter {
  width: 144px;
  min-width: 144px;
  max-width: 144px;
  height: 32px;
  padding: 0 9px 0 10px;
  border: 1px solid rgba(15, 23, 42, 0.1);
  border-radius: 999px;
  background: #ffffff;
  display: inline-flex;
  align-items: center;
  gap: 6px;
  box-sizing: border-box;
  color: #243042;
  transition: border-color 0.15s ease, background-color 0.15s ease, box-shadow 0.15s ease;
}

.assistant-context-meter:hover {
  border-color: rgba(58, 96, 195, 0.24);
  background: #fbfcff;
}

.assistant-context-meter--disabled {
  color: #6b7280;
  background: #fbfcfd;
}

.assistant-context-meter--danger {
  border-color: rgba(220, 38, 38, 0.22);
  background: linear-gradient(180deg, rgba(255, 255, 255, 1), rgba(255, 246, 246, 1));
}

.assistant-context-meter__label,
.assistant-context-meter__value {
  flex: 0 0 auto;
  font-size: 12px;
  line-height: 1;
  white-space: nowrap;
}

.assistant-context-meter__label {
  color: #4b5563;
  font-weight: 600;
}

.assistant-context-meter__value {
  width: 28px;
  min-width: 28px;
  text-align: right;
  color: #334155;
  font-weight: 700;
}

.assistant-context-meter--disabled .assistant-context-meter__value {
  color: #6b7280;
}

.assistant-context-meter--danger .assistant-context-meter__value {
  color: #b42318;
}

.assistant-context-meter__track {
  flex: 1 1 auto;
  min-width: 0;
  height: 7px;
  overflow: hidden;
  border-radius: 999px;
  background:
    linear-gradient(180deg, rgba(232, 238, 246, 0.95), rgba(243, 247, 251, 0.95)),
    #edf2f7;
}

.assistant-context-meter--disabled .assistant-context-meter__track {
  background:
    linear-gradient(180deg, rgba(239, 243, 247, 0.95), rgba(245, 247, 250, 0.95)),
    #eef2f7;
}

.assistant-context-meter__bar {
  height: 100%;
  border-radius: inherit;
  background: linear-gradient(90deg, #2556c7, #3b82f6);
  transition: width 0.3s ease;
}

.assistant-context-meter--danger .assistant-context-meter__bar {
  background: linear-gradient(90deg, #b42318, #ef4444);
}

.assistant-context-meter__loading-bar {
  width: 100%;
  height: 100%;
  border-radius: inherit;
  background: linear-gradient(90deg, rgba(221, 231, 244, 0.78), rgba(162, 188, 229, 0.95), rgba(221, 231, 244, 0.78));
  background-size: 200% 100%;
  animation: context-meter-loading 1.15s linear infinite;
}

@keyframes context-meter-loading {
  0% {
    background-position: 200% 0;
  }
  100% {
    background-position: -200% 0;
  }
}
</style>
