<script setup lang="ts">
import { computed } from 'vue'
import type { TimelineBusinessCardPayload, TaskQueryCardData, TaskRecordCardData } from '@/api/schedule_agent'
import TaskQueryResultCard from './TaskQueryResultCard.vue'
import TaskRecordCard from './TaskRecordCard.vue'

const props = defineProps<{
  payload: TimelineBusinessCardPayload
}>()

const isTaskQuery = computed(() => props.payload.card_type === 'task_query')
const isTaskRecord = computed(() => props.payload.card_type === 'task_record')

const queryData = computed(() => props.payload.data as TaskQueryCardData)
const recordData = computed(() => props.payload.data as TaskRecordCardData)
</script>

<template>
  <div class="business-card-renderer">
    <TaskQueryResultCard
      v-if="isTaskQuery"
      :data="queryData"
      :title="payload.title"
      :summary="payload.summary"
    />
    
    <TaskRecordCard
      v-else-if="isTaskRecord"
      :data="recordData"
      :source="payload.source"
      :title="payload.title"
      :summary="payload.summary"
    />
    
    <div v-else class="unknown-card">
      <p>未知业务卡片类型: {{ payload.card_type }}</p>
    </div>
  </div>
</template>

<style scoped>
.business-card-renderer {
  margin: 12px 0;
  display: flex;
  flex-direction: column;
  animation: card-appear 0.4s cubic-bezier(0.34, 1.56, 0.64, 1);
}

@keyframes card-appear {
  0% { opacity: 0; transform: scale(0.95) translateY(10px); }
  100% { opacity: 1; transform: scale(1) translateY(0); }
}

.unknown-card {
  padding: 16px;
  background: #f1f5f9;
  border-radius: 12px;
  color: #64748b;
  font-size: 13px;
  text-align: center;
}
</style>
