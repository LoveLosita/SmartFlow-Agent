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

function formatCompactCount(value: number) {
  if (!Number.isFinite(value)) {
    return '--'
  }

  // 1. 千位及以上用 k 单位压缩，避免按钮过宽。
  // 2. 保留小数点后 1 位；如果刚好是整数千位，则去掉 .0，像 80k 这种展示会更干净。
  const absoluteValue = Math.abs(value)
  if (absoluteValue >= 1000) {
    const compactValue = value / 1000
    const compactText = compactValue.toFixed(1)
    return `${compactText.endsWith('.0') ? compactText.slice(0, -2) : compactText}k`
  }

  return `${Math.round(value)}`
}

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

  // 1. 进度条只负责表达相对占用率。
  // 2. 超过预算时只把宽度封顶到 100%，避免条形溢出容器。
  return Math.min(100, (safeStats.value.total / safeStats.value.budget) * 100)
})

const isOverBudget = computed(() => {
  if (!safeStats.value) {
    return false
  }
  return safeStats.value.total > safeStats.value.budget
})

const usagePercentText = computed(() => {
  if (props.loading) {
    return '--'
  }

  if (!safeStats.value) {
    return props.disabled ? '--' : '0%'
  }

  return `${usagePercent.value}%`
})

const usageSummaryText = computed(() => {
  if (props.loading) {
    return '...'
  }

  if (!safeStats.value) {
    return props.disabled ? '--/--' : '0/0'
  }

  return `${formatCompactCount(safeStats.value.total)}/${formatCompactCount(safeStats.value.budget)}`
})

const tooltipText = computed(() => {
  if (props.loading) {
    return '正在读取当前会话的上下文窗口统计'
  }

  if (!safeStats.value) {
    return props.disabled
      ? '新会话发送首条消息后展示上下文窗口统计'
      : '当前会话暂无上下文窗口统计'
  }

  return `上下文使用 ${usagePercentText.value}（${usageSummaryText.value}）`
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
    <div class="assistant-context-meter__core">
      <span class="assistant-context-meter__percent">{{ usagePercentText }}</span>
      <span class="assistant-context-meter__summary">{{ usageSummaryText }}</span>

      <div class="assistant-context-meter__track" aria-hidden="true">
        <div v-if="loading" class="assistant-context-meter__loading-bar" />
        <div
          v-else-if="barWidthPercent > 0"
          class="assistant-context-meter__bar"
          :style="{ width: `${barWidthPercent}%` }"
        />
      </div>
    </div>
  </div>
</template>

<style scoped>
.assistant-context-meter {
  width: 188px;
  min-width: 188px;
  max-width: 188px;
  height: 32px;
  padding: 0 8px;
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
.assistant-context-meter__percent,
.assistant-context-meter__summary {
  flex: 0 0 auto;
  font-size: 11px;
  line-height: 1;
  white-space: nowrap;
}

.assistant-context-meter__label {
  color: #4b5563;
  font-weight: 600;
}

.assistant-context-meter__core {
  flex: 1 1 auto;
  min-width: 0;
  display: grid;
  grid-template-columns: auto auto minmax(0, 1fr);
  align-items: center;
  column-gap: 2px;
}

.assistant-context-meter__percent {
  min-width: 24px;
  color: #334155;
  font-weight: 700;
  text-align: right;
}

.assistant-context-meter__summary {
  min-width: 52px;
  color: #667085;
  font-weight: 600;
  text-align: right;
}

.assistant-context-meter--disabled .assistant-context-meter__percent,
.assistant-context-meter--disabled .assistant-context-meter__summary {
  color: #6b7280;
}

.assistant-context-meter--danger .assistant-context-meter__percent,
.assistant-context-meter--danger .assistant-context-meter__summary {
  color: #b42318;
}

.assistant-context-meter__track {
  min-width: 0;
  width: 100%;
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
