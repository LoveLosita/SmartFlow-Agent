<script setup lang="ts">
import { computed, reactive, watch } from 'vue'
import { ElMessage } from 'element-plus'

import type { TaskClassCreatePayload, TaskClassCreateItemPayload, TaskClassDetail } from '@/types/schedule'

const props = defineProps<{
  modelValue: boolean
  loading?: boolean
  initialData?: TaskClassDetail | null
}>()

const emit = defineEmits<{
  'update:modelValue': [value: boolean]
  submit: [payload: TaskClassCreatePayload]
}>()

const form = reactive({
  name: '',
  start_date: '',
  end_date: '',
  total_slots: 8,
  allow_filler_course: true,
  strategy: 'steady',
  excluded_slots: [] as number[],
  items: [
    { order: 1, content: '', embedded_time: null },
    { order: 2, content: '', embedded_time: null },
    { order: 3, content: '', embedded_time: null },
  ] as TaskClassCreateItemPayload[],
})

watch(
  () => props.modelValue,
  (visible) => {
    if (!visible) {
      return
    }

    if (props.initialData) {
      form.name = props.initialData.name
      form.start_date = props.initialData.start_date
      form.end_date = props.initialData.end_date
      form.total_slots = props.initialData.config.total_slots
      form.allow_filler_course = props.initialData.config.allow_filler_course
      form.strategy = props.initialData.config.strategy
      form.excluded_slots = props.initialData.config.excluded_slots || []
      form.items = props.initialData.items.map(item => ({
        id: item.id,
        order: item.order,
        content: item.content,
        embedded_time: item.embedded_time,
      }))
    } else {
      form.name = ''
      form.start_date = ''
      form.end_date = ''
      form.total_slots = 8
      form.allow_filler_course = true
      form.strategy = 'steady'
      form.excluded_slots = []
      form.items = [
        { order: 1, content: '', embedded_time: null },
        { order: 2, content: '', embedded_time: null },
        { order: 3, content: '', embedded_time: null },
      ]
    }
  },
)

const isEdit = computed(() => !!props.initialData)

function addItem() {
  form.items.push({
    order: form.items.length + 1,
    content: '',
    embedded_time: null,
  })
}

function removeItem(index: number) {
  form.items.splice(index, 1)
  form.items.forEach((item, itemIndex) => {
    item.order = itemIndex + 1
  })
}

function handleSubmit() {
  const filteredItems = form.items
    .map((item, index) => ({
      id: item.id,
      order: index + 1,
      content: item.content.trim(),
      embedded_time: item.embedded_time,
    }))
    .filter((item) => item.content)

  if (!form.name.trim()) {
    ElMessage.warning('请先填写任务类名称')
    return
  }

  if (!form.start_date || !form.end_date) {
    ElMessage.warning('请先补齐开始与结束日期')
    return
  }

  if (filteredItems.length === 0) {
    ElMessage.warning('至少添加一个任务块内容')
    return
  }

  emit('submit', {
    name: form.name.trim(),
    start_date: form.start_date,
    end_date: form.end_date,
    mode: 'auto',
    config: {
      total_slots: form.total_slots,
      allow_filler_course: form.allow_filler_course,
      strategy: form.strategy,
      excluded_slots: form.excluded_slots,
    },
    items: filteredItems,
  })
}
</script>

<template>
  <el-dialog
    :model-value="modelValue"
    :title="isEdit ? '编辑任务类' : '创建任务类'"
    width="720px"
    align-center
    class="task-class-dialog"
    @update:model-value="emit('update:modelValue', $event)"
  >
    <div class="task-class-dialog__body">
      <div class="task-class-dialog__grid">
        <label class="task-class-dialog__field">
          <span>任务类名称</span>
          <el-input v-model="form.name" placeholder="例如：数据结构复习" maxlength="64" />
        </label>

        <label class="task-class-dialog__field">
          <span>编排策略</span>
          <el-select v-model="form.strategy">
            <el-option value="steady" label="均衡推进" />
            <el-option value="rapid" label="快速冲刺" />
          </el-select>
        </label>

        <label class="task-class-dialog__field">
          <span>开始日期</span>
          <el-date-picker
            v-model="form.start_date"
            type="date"
            value-format="YYYY-MM-DD"
            placeholder="选择开始日期"
          />
        </label>

        <label class="task-class-dialog__field">
          <span>结束日期</span>
          <el-date-picker
            v-model="form.end_date"
            type="date"
            value-format="YYYY-MM-DD"
            placeholder="选择结束日期"
          />
        </label>

        <label class="task-class-dialog__field">
          <span>总节数</span>
          <el-input-number v-model="form.total_slots" :min="1" :max="48" />
        </label>

        <label class="task-class-dialog__field task-class-dialog__field--switch">
          <span>允许嵌入水课</span>
          <el-switch v-model="form.allow_filler_course" />
        </label>
      </div>

      <div class="task-class-dialog__items">
        <div class="task-class-dialog__items-head">
          <strong>任务块列表</strong>
          <button type="button" class="task-class-dialog__add" @click="addItem">新增任务块</button>
        </div>

        <div class="task-class-dialog__items-list">
          <div v-for="(item, index) in form.items" :key="index" class="task-class-dialog__item">
            <span class="task-class-dialog__item-order">{{ index + 1 }}</span>
            <el-input v-model="item.content" placeholder="填写任务块内容，例如：树与二叉树" />
            <button
              type="button"
              class="task-class-dialog__item-remove"
              :disabled="form.items.length <= 1"
              @click="removeItem(index)"
            >
              删除
            </button>
          </div>
        </div>
      </div>
    </div>

    <template #footer>
      <div class="task-class-dialog__footer">
        <el-button @click="emit('update:modelValue', false)">取消</el-button>
        <el-button type="primary" :loading="loading" @click="handleSubmit">
          {{ isEdit ? '保存修改' : '确认创建' }}
        </el-button>
      </div>
    </template>
  </el-dialog>
</template>

<style scoped>
.task-class-dialog__body {
  display: grid;
  gap: 22px;
  min-height: 0;
  max-height: min(72vh, 760px);
  overflow: hidden;
}

.task-class-dialog__grid {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 16px;
}

.task-class-dialog__field {
  display: grid;
  gap: 8px;
}

.task-class-dialog__field span,
.task-class-dialog__items-head strong {
  color: #1d2940;
  font-size: 13px;
  font-weight: 700;
}

.task-class-dialog__field--switch {
  align-content: start;
}

.task-class-dialog__field :deep(.el-input),
.task-class-dialog__field :deep(.el-select),
.task-class-dialog__field :deep(.el-date-editor) {
  width: 100%;
}

.task-class-dialog__items {
  display: grid;
  gap: 14px;
  min-width: 0;
  min-height: 0;
}

.task-class-dialog__items-head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
  min-width: 0;
  flex-wrap: wrap;
}

.task-class-dialog__add {
  height: 34px;
  border: 1px solid rgba(28, 98, 206, 0.18);
  border-radius: 12px;
  background: #f6f9ff;
  color: #1d64d2;
  font-size: 12px;
  font-weight: 700;
  padding: 0 14px;
  cursor: pointer;
}

.task-class-dialog__items-list {
  display: grid;
  gap: 10px;
  min-width: 0;
  max-height: min(44vh, 360px);
  overflow-y: auto;
  overflow-x: hidden;
  padding-right: 4px;
  scrollbar-gutter: stable;
  overscroll-behavior: contain;
}

.task-class-dialog__item {
  display: grid;
  grid-template-columns: 36px minmax(0, 1fr) auto;
  gap: 10px;
  align-items: center;
  min-width: 0;
}

.task-class-dialog__item-order {
  width: 36px;
  height: 36px;
  border-radius: 12px;
  background: #eef3f8;
  color: #354259;
  display: inline-flex;
  align-items: center;
  justify-content: center;
  font-weight: 700;
}

.task-class-dialog__item-remove {
  height: 36px;
  border: 1px solid rgba(185, 42, 29, 0.16);
  border-radius: 12px;
  background: #fff6f5;
  color: #bd3e2f;
  font-size: 12px;
  font-weight: 700;
  padding: 0 12px;
  cursor: pointer;
}

.task-class-dialog__item-remove:disabled {
  opacity: 0.45;
  cursor: not-allowed;
}

.task-class-dialog__footer {
  display: flex;
  justify-content: flex-end;
}

@media (max-height: 900px) {
  .task-class-dialog__body {
    max-height: min(76vh, 680px);
    gap: 18px;
  }

  .task-class-dialog__items-list {
    max-height: min(40vh, 300px);
  }
}

@media (max-height: 820px) {
  .task-class-dialog__items-list {
    max-height: min(34vh, 240px);
  }
}

@media (max-width: 840px) {
  .task-class-dialog__grid {
    grid-template-columns: 1fr;
  }

  .task-class-dialog__item {
    grid-template-columns: 32px minmax(0, 1fr);
    align-items: start;
  }

  .task-class-dialog__item-remove {
    grid-column: 2;
    justify-self: start;
  }
}

.task-class-dialog :deep(.el-dialog) {
  width: min(720px, calc(100vw - 24px));
  max-height: calc(100vh - 24px);
  overflow: hidden;
}

.task-class-dialog :deep(.el-dialog__body) {
  overflow: hidden;
}
</style>
