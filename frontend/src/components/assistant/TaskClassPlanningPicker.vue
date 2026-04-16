<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { ElMessage } from 'element-plus'

import { getTaskClassList } from '@/api/scheduleCenter'
import type { TaskClassListItem } from '@/types/schedule'

interface SelectedTaskClassSummary {
  id: number
  name: string
}

const props = withDefaults(
  defineProps<{
    modelValue: number[]
    disabled?: boolean
  }>(),
  {
    disabled: false,
  },
)

const emit = defineEmits<{
  'update:modelValue': [taskClassIds: number[]]
  applied: [taskClassIds: number[]]
}>()

const popoverVisible = ref(false)
const taskClassLoading = ref(false)
const taskClasses = ref<TaskClassListItem[]>([])
const draftSelectedIds = ref<number[]>([])
const taskClassListReady = ref(false)

const triggerLabel = computed(() => {
  if (props.modelValue.length <= 0) {
    return '智能编排'
  }
  return `编排 ${props.modelValue.length}`
})

const selectedTaskClasses = computed<SelectedTaskClassSummary[]>(() => {
  const lookup = new Map(taskClasses.value.map((item) => [item.id, item]))
  return props.modelValue.map((taskClassId) => {
    const taskClass = lookup.get(taskClassId)
    return {
      id: taskClassId,
      name: taskClass?.name || `任务类 #${taskClassId}`,
    }
  })
})

watch(
  () => props.modelValue,
  (nextValue) => {
    if (!popoverVisible.value) {
      draftSelectedIds.value = [...nextValue]
    }
  },
  { immediate: true },
)

watch(popoverVisible, (visible) => {
  if (!visible) {
    return
  }
  draftSelectedIds.value = [...props.modelValue]
  void ensureTaskClassListLoaded()
})

function normalizeTaskClassIds(taskClassIds: number[]) {
  const seen = new Set<number>()
  const normalized: number[] = []

  for (const taskClassId of taskClassIds) {
    if (!Number.isInteger(taskClassId) || taskClassId <= 0 || seen.has(taskClassId)) {
      continue
    }
    seen.add(taskClassId)
    normalized.push(taskClassId)
  }

  return normalized
}

async function ensureTaskClassListLoaded() {
  if (taskClassLoading.value || taskClassListReady.value) {
    return
  }

  taskClassLoading.value = true
  try {
    taskClasses.value = await getTaskClassList()
    taskClassListReady.value = true
  } catch (error) {
    ElMessage.error(error instanceof Error ? error.message : '任务类列表加载失败')
  } finally {
    taskClassLoading.value = false
  }
}

function toggleDraftSelection(taskClassId: number) {
  if (draftSelectedIds.value.includes(taskClassId)) {
    draftSelectedIds.value = draftSelectedIds.value.filter((id) => id !== taskClassId)
    return
  }
  draftSelectedIds.value = [...draftSelectedIds.value, taskClassId]
}

function applySelection() {
  // 1. 先在前端做一次去重和非法值过滤，避免把脏 ID 直接发给后端。
  // 2. 这里只负责提交“下一条消息要带的任务类上下文”，不负责直接触发发送。
  // 3. 提交成功后关闭弹层，让用户回到输入区继续编辑本轮提示词。
  const normalizedTaskClassIds = normalizeTaskClassIds(draftSelectedIds.value)
  emit('update:modelValue', normalizedTaskClassIds)
  emit('applied', normalizedTaskClassIds)
  popoverVisible.value = false
}

function clearSelectionFromPanel() {
  draftSelectedIds.value = []
  emit('update:modelValue', [])
  emit('applied', [])
  popoverVisible.value = false
}

function removeSelectedTaskClass(taskClassId: number) {
  emit(
    'update:modelValue',
    props.modelValue.filter((id) => id !== taskClassId),
  )
}

function clearSelectedTaskClasses() {
  emit('update:modelValue', [])
}

function formatDateRange(taskClass: TaskClassListItem) {
  const startDate = formatDateLabel(taskClass.start_date)
  const endDate = formatDateLabel(taskClass.end_date)
  if (!startDate || !endDate) {
    return '时间范围待补充'
  }
  return `${startDate} - ${endDate}`
}

function formatDateLabel(value: string) {
  const parsedDate = new Date(value)
  if (Number.isNaN(parsedDate.getTime())) {
    return ''
  }
  const month = `${parsedDate.getMonth() + 1}`.padStart(2, '0')
  const day = `${parsedDate.getDate()}`.padStart(2, '0')
  return `${month}.${day}`
}
</script>

<template>
  <div class="assistant-planning">
    <el-popover
      v-model:visible="popoverVisible"
      placement="top-start"
      trigger="click"
      :width="360"
      :teleported="true"
      popper-class="assistant-planning-popover"
    >
      <template #reference>
        <button
          type="button"
          class="assistant-planning__trigger"
          :class="{ 'assistant-planning__trigger--active': modelValue.length > 0 }"
          :disabled="disabled"
        >
          <span class="assistant-planning__trigger-icon" aria-hidden="true">
            <svg width="14" height="14" viewBox="0 0 14 14" fill="none" xmlns="http://www.w3.org/2000/svg">
              <path d="M7 1.25L12.25 4.375L7 7.5L1.75 4.375L7 1.25Z" fill="currentColor" />
              <path d="M1.75 6.5625L7 9.6875L12.25 6.5625" stroke="currentColor" stroke-width="1.1" stroke-linecap="round" stroke-linejoin="round" />
              <path d="M1.75 8.75L7 11.875L12.25 8.75" stroke="currentColor" stroke-width="1.1" stroke-linecap="round" stroke-linejoin="round" />
            </svg>
          </span>
          <span class="assistant-planning__trigger-text">{{ triggerLabel }}</span>
        </button>
      </template>

      <div class="assistant-planning__panel">
        <div class="assistant-planning__panel-header">
          <div>
            <strong>选择任务类</strong>
            <p>本次发送将把所选任务类作为智能编排上下文带给后端。</p>
          </div>
        </div>

        <div v-if="taskClassLoading" class="assistant-planning__loading">
          <div v-for="index in 4" :key="index" class="assistant-planning__loading-item" />
        </div>

        <div v-else-if="taskClasses.length" class="assistant-planning__list">
          <button
            v-for="taskClass in taskClasses"
            :key="taskClass.id"
            type="button"
            class="assistant-planning__item"
            :class="{ 'assistant-planning__item--selected': draftSelectedIds.includes(taskClass.id) }"
            @click="toggleDraftSelection(taskClass.id)"
          >
            <span
              class="assistant-planning__item-check"
              :class="{ 'assistant-planning__item-check--selected': draftSelectedIds.includes(taskClass.id) }"
              aria-hidden="true"
            />
            <span class="assistant-planning__item-body">
              <strong>{{ taskClass.name }}</strong>
              <small>{{ formatDateRange(taskClass) }}</small>
            </span>
            <span class="assistant-planning__item-slots">{{ taskClass.total_slots }} 节</span>
          </button>
        </div>

        <div v-else class="assistant-planning__empty">
          当前还没有可用于智能编排的任务类。
        </div>

        <div class="assistant-planning__panel-actions">
          <button type="button" class="assistant-planning__panel-button assistant-planning__panel-button--ghost" @click="clearSelectionFromPanel">
            清空
          </button>
          <button type="button" class="assistant-planning__panel-button assistant-planning__panel-button--primary" @click="applySelection">
            应用选择
          </button>
        </div>
      </div>
    </el-popover>

    <div v-if="selectedTaskClasses.length" class="assistant-planning__summary">
      <span class="assistant-planning__summary-label">已选任务类</span>
      <div class="assistant-planning__tags">
        <button
          v-for="taskClass in selectedTaskClasses"
          :key="taskClass.id"
          type="button"
          class="assistant-planning__tag"
          :disabled="disabled"
          @click="removeSelectedTaskClass(taskClass.id)"
        >
          <span>{{ taskClass.name }}</span>
          <span aria-hidden="true">×</span>
        </button>
        <button type="button" class="assistant-planning__clear" :disabled="disabled" @click="clearSelectedTaskClasses">
          清空全部
        </button>
      </div>
    </div>
  </div>
</template>

<style scoped>
.assistant-planning {
  display: grid;
  justify-items: start;
  gap: 10px;
  min-width: 0;
  padding: 10px 12px 0;
}

.assistant-planning__trigger {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  gap: 6px;
  width: 138px;
  min-width: 138px;
  max-width: 138px;
  height: 32px;
  padding: 0 10px;
  box-sizing: border-box;
  border: 1px solid rgba(15, 23, 42, 0.1);
  border-radius: 999px;
  background: #ffffff;
  color: #1f2430;
  font-size: 13px;
  font-weight: 600;
  transition: border-color 0.15s ease, background-color 0.15s ease, color 0.15s ease;
}

.assistant-planning__trigger:hover:not(:disabled) {
  border-color: rgba(57, 86, 178, 0.26);
  background: #f8fafc;
}

.assistant-planning__trigger:disabled {
  cursor: not-allowed;
  opacity: 0.58;
}

.assistant-planning__trigger--active {
  border-color: rgba(57, 86, 178, 0.24);
  background: #eef3ff;
  color: #3357c2;
}

.assistant-planning__trigger-icon {
  display: inline-flex;
  width: 14px;
  height: 14px;
}

.assistant-planning__trigger-text {
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.assistant-planning__summary {
  display: grid;
  gap: 8px;
}

.assistant-planning__summary-label {
  color: #5b6677;
  font-size: 12px;
  font-weight: 600;
}

.assistant-planning__tags {
  display: flex;
  flex-wrap: wrap;
  gap: 8px;
}

.assistant-planning__tag,
.assistant-planning__clear {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  height: 28px;
  padding: 0 10px;
  border-radius: 999px;
  border: 1px solid rgba(57, 86, 178, 0.16);
  background: #f6f8ff;
  color: #3559c3;
  font-size: 12px;
  font-weight: 600;
}

.assistant-planning__clear {
  border-style: dashed;
  background: #ffffff;
  color: #64748b;
}

.assistant-planning__panel {
  display: grid;
  gap: 14px;
}

.assistant-planning__panel-header strong {
  display: block;
  color: #1f2937;
  font-size: 15px;
}

.assistant-planning__panel-header p {
  margin: 6px 0 0;
  color: #6b7280;
  font-size: 12px;
  line-height: 1.5;
}

.assistant-planning__loading,
.assistant-planning__list {
  display: grid;
  gap: 8px;
  max-height: 260px;
  overflow-y: auto;
}

.assistant-planning__loading-item {
  height: 62px;
  border-radius: 16px;
  background: linear-gradient(90deg, rgba(241, 245, 249, 0.9), rgba(226, 232, 240, 0.72), rgba(241, 245, 249, 0.9));
}

.assistant-planning__item {
  width: 100%;
  display: grid;
  grid-template-columns: auto minmax(0, 1fr) auto;
  align-items: center;
  gap: 12px;
  padding: 12px 14px;
  border: 1px solid rgba(15, 23, 42, 0.08);
  border-radius: 16px;
  background: #ffffff;
  text-align: left;
  transition: border-color 0.15s ease, background-color 0.15s ease, transform 0.15s ease;
}

.assistant-planning__item:hover {
  border-color: rgba(57, 86, 178, 0.24);
  background: #fafcff;
}

.assistant-planning__item--selected {
  border-color: rgba(57, 86, 178, 0.28);
  background: #f5f8ff;
}

.assistant-planning__item-check {
  width: 16px;
  height: 16px;
  border-radius: 999px;
  border: 1.5px solid rgba(148, 163, 184, 0.8);
  background: #ffffff;
}

.assistant-planning__item-check--selected {
  border-color: #3357c2;
  background: radial-gradient(circle at center, #3357c2 0 45%, transparent 46%);
}

.assistant-planning__item-body {
  min-width: 0;
  display: grid;
  gap: 4px;
}

.assistant-planning__item-body strong {
  color: #1f2430;
  font-size: 13px;
}

.assistant-planning__item-body small {
  color: #64748b;
  font-size: 12px;
}

.assistant-planning__item-slots {
  color: #475569;
  font-size: 12px;
  font-weight: 600;
}

.assistant-planning__empty {
  padding: 18px 16px;
  border-radius: 16px;
  background: #f8fafc;
  color: #64748b;
  font-size: 13px;
  line-height: 1.6;
}

.assistant-planning__panel-actions {
  display: flex;
  justify-content: flex-end;
  gap: 10px;
}

.assistant-planning__panel-button {
  height: 34px;
  padding: 0 14px;
  border-radius: 999px;
  border: 1px solid rgba(15, 23, 42, 0.1);
  font-size: 13px;
  font-weight: 600;
}

.assistant-planning__panel-button--ghost {
  background: #ffffff;
  color: #475569;
}

.assistant-planning__panel-button--primary {
  border-color: transparent;
  background: #3357c2;
  color: #ffffff;
}

:global(.assistant-planning-popover) {
  padding: 14px;
  border-radius: 20px;
  border: 1px solid rgba(203, 213, 225, 0.78);
  box-shadow: 0 18px 44px rgba(15, 23, 42, 0.14);
}
</style>
