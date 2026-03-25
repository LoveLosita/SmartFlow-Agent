<script setup lang="ts">
import { computed } from 'vue'

import type { TodayEvent } from '@/types/dashboard'
import { formatTimeRange } from '@/utils/date'

interface BaseSlot {
  key: string
  title: string
}

interface EventSlot extends BaseSlot {
  kind: 'event'
  startTime: string
  endTime: string
}

interface PauseSlot extends BaseSlot {
  kind: 'pause'
}

type TimelineSlot = EventSlot | PauseSlot

interface RenderEventSlot {
  key: string
  kind: 'event'
  timeText: string
  title: string
  locationText: string
  tone: string
}

interface RenderPauseSlot {
  key: string
  kind: 'pause'
  title: string
  hint: string
}

type RenderSlot = RenderEventSlot | RenderPauseSlot

const props = defineProps<{
  events: TodayEvent[]
  loading?: boolean
}>()

// 1. 时间轴始终固定为 8 个槽位，顺序不再受当天是否有课影响。
// 2. 课程槽位缺数据时显示“无课”，而不是直接消失，避免把后续块位挤乱。
// 3. 午休和晚餐是纯占位块，不展示时间文本，只负责占住用户指定的位置。
const slotBlueprint: TimelineSlot[] = [
  { key: 'slot-1', kind: 'event', title: '1-2节', startTime: '08:00', endTime: '09:40' },
  { key: 'slot-2', kind: 'event', title: '3-4节', startTime: '10:15', endTime: '11:55' },
  { key: 'slot-noon', kind: 'pause', title: '午休' },
  { key: 'slot-4', kind: 'event', title: '5-6节', startTime: '14:00', endTime: '15:40' },
  { key: 'slot-5', kind: 'event', title: '7-8节', startTime: '16:15', endTime: '17:55' },
  { key: 'slot-dinner', kind: 'pause', title: '晚餐' },
  { key: 'slot-6', kind: 'event', title: '9-10节', startTime: '19:00', endTime: '20:40' },
  { key: 'slot-7', kind: 'event', title: '11-12节', startTime: '20:50', endTime: '22:30' },
]

function buildTimeKey(start?: string | null, end?: string | null) {
  return `${(start || '').trim()}|${(end || '').trim()}`
}

const eventMap = computed(() => {
  const map = new Map<string, TodayEvent>()
  for (const event of props.events ?? []) {
    map.set(buildTimeKey(event.start_time, event.end_time), event)
  }
  return map
})

function resolveCardTone(event: TodayEvent | null) {
  if (!event) {
    return 'neutral'
  }

  if (event.type === 'course') {
    return 'course'
  }

  const orderToneMap: Record<number, string> = {
    1: 'sky',
    2: 'violet',
    4: 'mint',
    5: 'emerald',
    6: 'amber',
    7: 'cyan',
  }

  return orderToneMap[event.order] ?? 'neutral'
}

const renderSlots = computed<RenderSlot[]>(() =>
  slotBlueprint.map((slot) => {
    if (slot.kind === 'pause') {
      return {
        key: slot.key,
        kind: 'pause',
        title: slot.title,
        hint: '为中段留出缓冲与恢复时间',
      }
    }

    const event = eventMap.value.get(buildTimeKey(slot.startTime, slot.endTime)) ?? null
    return {
      key: slot.key,
      kind: 'event',
      timeText: formatTimeRange(event?.start_time || slot.startTime, event?.end_time || slot.endTime),
      title: event?.name || '无课',
      locationText: event?.location || '休息时间',
      tone: resolveCardTone(event),
    }
  }),
)
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
      <template v-for="slot in renderSlots" :key="slot.key">
        <article
          v-if="slot.kind === 'event'"
          class="timeline-event"
          :class="`timeline-event--${slot.tone}`"
        >
          <span class="timeline-event__time">{{ slot.timeText }}</span>
          <strong class="timeline-event__title">{{ slot.title }}</strong>
          <span class="timeline-event__location">{{ slot.locationText }}</span>
        </article>

        <article v-else class="timeline-placeholder timeline-placeholder--pause">
          <strong class="timeline-placeholder__title">{{ slot.title }}</strong>
          <span class="timeline-placeholder__hint">{{ slot.hint }}</span>
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
  /* 1. 使用自适应列数，避免固定列数把左侧主区撑爆。 */
  /* 2. 但槽位顺序固定，换行只影响视觉换行，不影响时间先后顺序。 */
  /* 3. 这样无论是否缺课，8 个槽位都会按既定顺序逐个渲染。 */
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

.timeline-event--sky {
  background: linear-gradient(180deg, #f8fbff 0%, #f3f7fc 100%);
}

.timeline-event--sky::before {
  background: #c8d6e8;
}

.timeline-event--violet {
  background: linear-gradient(180deg, #eef0ff 0%, #e6e8ff 100%);
}

.timeline-event--violet::before {
  background: #676cff;
}

.timeline-event--mint {
  background: linear-gradient(180deg, #e6f2ff 0%, #dceaff 100%);
}

.timeline-event--mint::before {
  background: #2f7de1;
}

.timeline-event--emerald {
  background: linear-gradient(180deg, #e6f8f1 0%, #def5ec 100%);
}

.timeline-event--emerald::before {
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
