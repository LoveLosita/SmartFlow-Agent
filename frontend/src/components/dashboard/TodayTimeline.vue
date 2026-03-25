<script setup lang="ts">
import { computed } from 'vue'

import type { TodayEvent } from '@/types/dashboard'
import { formatTimeRange } from '@/utils/date'

interface TimelineSlot {
  key: string
  kind: 'event' | 'pause'
  label: string
  timeText?: string
  eventOrder?: number
}

const props = defineProps<{
  events: TodayEvent[]
  loading?: boolean
}>()

const slotBlueprint: TimelineSlot[] = [
  { key: 'slot-1', kind: 'event', label: '上午', timeText: '08:00 - 09:40', eventOrder: 1 },
  { key: 'slot-2', kind: 'event', label: '上午', timeText: '10:15 - 11:55', eventOrder: 2 },
  { key: 'slot-noon', kind: 'pause', label: '午休' },
  { key: 'slot-4', kind: 'event', label: '下午', timeText: '14:00 - 15:40', eventOrder: 4 },
  // 1. 晚餐块固定放在 7-8 节与 9-10 节之间，作为晚间课程前的过渡占位。
  // 2. 根据用户最新要求，它要出现在“17:55 结束的课块之后、19:00 黄色块之前”。
  // 3. 用户要求该块只保留单独卡片，不展示时间文本。
  { key: 'slot-dinner', kind: 'pause', label: '晚餐' },
  { key: 'slot-5', kind: 'event', label: '下午', timeText: '16:15 - 17:55', eventOrder: 5 },
  { key: 'slot-6', kind: 'event', label: '晚间', timeText: '19:00 - 20:40', eventOrder: 6 },
  { key: 'slot-7', kind: 'event', label: '晚间', timeText: '20:50 - 22:30', eventOrder: 7 },
]

const eventMap = computed(() => {
  const map = new Map<number, TodayEvent>()
  for (const event of props.events ?? []) {
    map.set(event.order, event)
  }
  return map
})

function resolveCardTone(event: TodayEvent) {
  if (event.type === 'course') {
    return 'course'
  }

  const orderToneMap: Record<number, string> = {
    1: 'sky',
    2: 'violet',
    4: 'mint',
    5: 'amber',
    7: 'cyan',
  }

  return orderToneMap[event.order] ?? 'neutral'
}

function resolveSlotEvent(slot: TimelineSlot) {
  if (typeof slot.eventOrder !== 'number') {
    return null
  }
  return eventMap.value.get(slot.eventOrder) ?? null
}
</script>

<template>
  <section class="timeline-card glass-panel">
    <header class="timeline-card__header">
      <div>
        <p class="timeline-card__eyebrow">今日总览</p>
        <h2>今日日程一览</h2>
      </div>
      <span class="timeline-card__caption">按时间顺序展示课程与任务安排</span>
    </header>

    <div v-if="loading" class="timeline-skeleton">
      <div v-for="slot in slotBlueprint" :key="slot.key" class="timeline-skeleton__item" />
    </div>

    <div v-else class="timeline-grid">
      <template v-for="slot in slotBlueprint" :key="slot.key">
        <article v-if="slot.kind === 'pause'" class="timeline-placeholder timeline-placeholder--pause">
          <span v-if="slot.timeText" class="timeline-placeholder__time">{{ slot.timeText }}</span>
          <strong class="timeline-placeholder__title">{{ slot.label }}</strong>
          <span class="timeline-placeholder__hint">为中段留出缓冲与恢复时间</span>
        </article>

        <article
          v-else-if="resolveSlotEvent(slot)"
          class="timeline-event"
          :class="`timeline-event--${resolveCardTone(resolveSlotEvent(slot)!)}`"
        >
          <span class="timeline-event__time">
            {{
              formatTimeRange(
                resolveSlotEvent(slot)?.start_time,
                resolveSlotEvent(slot)?.end_time,
              )
            }}
          </span>
          <strong class="timeline-event__title">{{ resolveSlotEvent(slot)?.name }}</strong>
          <span class="timeline-event__location">
            {{ resolveSlotEvent(slot)?.location || '休息时间' }}
          </span>
        </article>

        <article v-else class="timeline-event timeline-event--neutral">
          <span class="timeline-event__time">{{ slot.timeText }}</span>
          <strong class="timeline-event__title">无课</strong>
          <span class="timeline-event__location">休息时间</span>
        </article>
      </template>
    </div>
  </section>
</template>

<style scoped>
.timeline-card {
  min-width: 0;
  border-radius: 28px;
  padding: 22px 22px 20px;
  border: 1px solid rgba(17, 24, 39, 0.08);
}

.timeline-card__header {
  display: flex;
  justify-content: space-between;
  gap: 18px;
  align-items: flex-end;
  margin-bottom: 18px;
}

.timeline-card__eyebrow {
  margin: 0 0 6px;
  font-size: 12px;
  font-weight: 700;
  color: #2a6fdf;
  letter-spacing: 0.08em;
  text-transform: uppercase;
}

.timeline-card h2 {
  margin: 0;
  font-size: 28px;
  line-height: 1.1;
  letter-spacing: -0.03em;
}

.timeline-card__caption {
  color: var(--text-secondary);
  font-size: 13px;
}

.timeline-grid {
  min-width: 0;
  display: grid;
  /* 1. 改为 auto-fit 自适应列数，避免固定列数把左侧主区整体撑宽。 */
  /* 2. 每张卡片保留可读最小宽度，空间不足时自动换行，而不是出现横向滚动条。 */
  /* 3. 这样在左右近似二分的布局下，左侧信息板也能保持完整可见。 */
  grid-template-columns: repeat(auto-fit, minmax(132px, 1fr));
  gap: 12px;
  overflow: visible;
}

.timeline-event,
.timeline-placeholder,
.timeline-skeleton__item {
  min-width: 0;
  min-height: 124px;
  border-radius: 20px;
}

.timeline-event {
  padding: 16px 14px;
  display: flex;
  flex-direction: column;
  justify-content: space-between;
  border: 1px solid rgba(17, 24, 39, 0.06);
  position: relative;
  overflow: hidden;
}

.timeline-event::before {
  content: '';
  position: absolute;
  left: 0;
  top: 0;
  bottom: 0;
  width: 5px;
  opacity: 0.92;
}

.timeline-event__time {
  font-size: 12px;
  font-weight: 700;
  color: #295b9b;
}

.timeline-event__title {
  margin-top: 12px;
  font-size: 15px;
  line-height: 1.35;
  color: #172033;
}

.timeline-event__location {
  margin-top: 14px;
  font-size: 12px;
  color: #5f6980;
}

.timeline-event--course {
  background: linear-gradient(180deg, #ecf4ff 0%, #e4eefc 100%);
}

.timeline-event--course::before {
  background: #1669c1;
}

.timeline-event--violet {
  background: linear-gradient(180deg, #eef0ff 0%, #e6e8ff 100%);
}

.timeline-event--violet::before {
  background: #676cff;
}

.timeline-event--mint {
  background: linear-gradient(180deg, #e6f8f1 0%, #def5ec 100%);
}

.timeline-event--mint::before {
  background: #27b482;
}

.timeline-event--amber {
  background: linear-gradient(180deg, #fff5db 0%, #fff0cb 100%);
}

.timeline-event--amber::before {
  background: #f59e0b;
}

.timeline-event--cyan {
  background: linear-gradient(180deg, #e1f7ff 0%, #d6f2fb 100%);
}

.timeline-event--cyan::before {
  background: #57b8ea;
}

.timeline-event--neutral {
  background: linear-gradient(180deg, #f8fbff 0%, #f3f7fc 100%);
}

.timeline-event--neutral::before {
  background: #c8d6e8;
}

.timeline-placeholder {
  border: 1px dashed rgba(120, 144, 171, 0.28);
  background: rgba(255, 255, 255, 0.55);
  display: grid;
  align-content: center;
  justify-items: center;
  gap: 8px;
  padding: 14px 12px;
  text-align: center;
  color: #8a96a8;
}

.timeline-placeholder--pause {
  background: linear-gradient(180deg, #f5f9ff 0%, #eef4fb 100%);
}

.timeline-placeholder__time {
  font-size: 12px;
  font-weight: 700;
  color: #4c6c97;
}

.timeline-placeholder__title {
  font-size: 16px;
  color: #22324b;
}

.timeline-placeholder__hint {
  font-size: 12px;
  color: #7a889d;
  line-height: 1.5;
}

.timeline-skeleton {
  min-width: 0;
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(132px, 1fr));
  gap: 12px;
}

.timeline-skeleton__item {
  background: linear-gradient(90deg, rgba(230, 236, 244, 0.8), rgba(245, 248, 252, 1), rgba(230, 236, 244, 0.8));
  background-size: 200% 100%;
  animation: skeleton-shimmer 1.4s linear infinite;
}

@keyframes skeleton-shimmer {
  0% {
    background-position: 200% 0;
  }

  100% {
    background-position: -200% 0;
  }
}

@media (max-width: 1320px) {
  .timeline-grid,
  .timeline-skeleton {
    grid-template-columns: repeat(auto-fit, minmax(148px, 1fr));
  }
}

@media (max-width: 1040px) {
  .timeline-card__header {
    flex-direction: column;
    align-items: flex-start;
  }
}

@media (max-width: 780px) {
  .timeline-grid,
  .timeline-skeleton {
    grid-template-columns: repeat(auto-fit, minmax(180px, 1fr));
  }
}
</style>
