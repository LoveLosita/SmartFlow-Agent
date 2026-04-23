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

const slotBlueprint: TimelineSlot[] = [
  { key: 'slot-1', kind: 'event', title: '1-2节', startTime: '08:00', endTime: '09:40' },
  { key: 'slot-2', kind: 'event', title: '3-4节', startTime: '10:15', endTime: '11:55' },
  { key: 'slot-noon', kind: 'pause', title: '午间' },
  { key: 'slot-4', kind: 'event', title: '5-6节', startTime: '14:00', endTime: '15:40' },
  { key: 'slot-5', kind: 'event', title: '7-8节', startTime: '16:15', endTime: '17:55' },
  { key: 'slot-dinner', kind: 'pause', title: '晚休' },
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

// 统一色调系统
function resolveCardTone(event: TodayEvent | null) {
  if (!event) return 'empty'
  if (event.type === 'course') return 'primary'
  
  const tones = ['sky', 'violet', 'mint', 'amber', 'rose']
  return tones[event.order % tones.length]
}

const renderSlots = computed<RenderSlot[]>(() =>
  slotBlueprint.map((slot) => {
    if (slot.kind === 'pause') {
      return {
        key: slot.key,
        kind: 'pause',
        title: slot.title,
        hint: 'Rest Time',
      }
    }

    const event = eventMap.value.get(buildTimeKey(slot.startTime, slot.endTime)) ?? null
    return {
      key: slot.key,
      kind: 'event',
      timeText: formatTimeRange(event?.start_time || slot.startTime, event?.end_time || slot.endTime),
      title: event?.name || '今日无安排',
      locationText: event?.location || '休息时间',
      tone: resolveCardTone(event),
    }
  }),
)
</script>

<template>
  <section class="pastel-container">
    <header class="pastel-header">
      <div class="header-content">
        <p class="header-label">DAILY TIMELINE</p>
        <h2>今日日程</h2>
      </div>
    </header>

    <transition name="grid-pop" mode="out-in">
      <div v-if="loading" key="loading" class="pastel-grid">
        <div v-for="n in 8" :key="n" class="skeleton-pill" />
      </div>

      <div v-else key="content" class="pastel-grid">
        <template v-for="slot in renderSlots" :key="slot.key">
          <article
            v-if="slot.kind === 'event'"
            class="pastel-item"
            :class="[`tone--${slot.tone}`]"
          >
            <div class="item-time">{{ slot.timeText }}</div>
            <strong class="item-title">{{ slot.title }}</strong>
            <div class="item-footer">
              <svg class="location-svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round" stroke-linejoin="round">
                <path d="M21 10c0 7-9 13-9 13s-9-6-9-13a9 9 0 0118 0z"></path>
                <circle cx="12" cy="10" r="3"></circle>
              </svg>
              <span class="location-text">{{ slot.locationText }}</span>
            </div>
          </article>

          <article v-else class="pause-item">
             <span class="pause-tag">{{ slot.title }}</span>
          </article>
        </template>
      </div>
    </transition>
  </section>
</template>

<style scoped>
.pastel-container {
  padding: 32px;
  background: #ffffff;
  border-radius: 32px;
  border: 1px solid rgba(0, 0, 0, 0.05);
  box-shadow: 0 10px 40px rgba(0, 0, 0, 0.02);
}

.pastel-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
  margin-bottom: 28px;
}

.header-label {
  font-size: 11px;
  font-weight: 900;
  color: #c4c9d5;
  letter-spacing: 0.15em;
  margin: 0 0 4px;
}

.header-content h2 {
  margin: 0;
  font-size: 24px;
  font-weight: 800;
  color: #1e293b;
}

.pastel-grid {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(130px, 1fr));
  gap: 14px;
}

/* 核心卡片样式 */
.pastel-item {
  position: relative;
  border-radius: 26px;
  padding: 20px 18px;
  display: flex;
  flex-direction: column;
  justify-content: space-between;
  min-height: 140px;
  transition: all 0.3s cubic-bezier(0.34, 1.56, 0.64, 1);
  cursor: default;
}

.pastel-item:hover {
  transform: scale(1.03) translateY(-4px);
  box-shadow: 0 15px 30px -10px rgba(0, 0, 0, 0.1);
  z-index: 10;
}

.item-time {
  font-size: 12px;
  font-weight: 800;
  opacity: 0.6;
  margin-bottom: 8px;
}

.item-title {
  font-size: 18px;
  font-weight: 850;
  line-height: 1.3;
  margin-top: 4px;
  flex: 1;
}

.item-footer {
  margin-top: 16px;
  display: flex;
  align-items: center;
  gap: 6px;
}

.location-svg { 
  width: 13px; 
  height: 13px; 
  opacity: 0.8;
}

.location-text { 
  font-size: 13px; 
  font-weight: 700; 
  opacity: 0.8; 
}

/* 莫兰迪色相系统 */
.tone--primary { background: #eff6ff; color: #1e40af; }
.tone--sky { background: #f0f9ff; color: #0369a1; }
.tone--violet { background: #f5f3ff; color: #5b21b6; }
.tone--mint { background: #ecfdf5; color: #065f46; }
.tone--amber { background: #fffdf2; color: #92400e; }
.tone--rose { background: #fff1f2; color: #9f1239; }
.tone--empty { background: #f8fafc; color: #64748b; }

/* 休息时间样式 */
.pause-item {
  background: #f1f5f9;
  border-radius: 26px;
  display: flex;
  align-items: center;
  justify-content: center;
  min-height: 80px;
  opacity: 0.7;
}

.pause-tag {
  font-size: 15px;
  font-weight: 900;
  color: #94a3b8;
  letter-spacing: 0.1em;
}

/* 骨架屏 */
.skeleton-pill {
  min-height: 140px;
  background: #f1f5f9;
  border-radius: 26px;
  animation: pill-shimmer 1.5s infinite linear;
}

@keyframes pill-shimmer {
  0% { opacity: 0.5; }
  50% { opacity: 1; }
  100% { opacity: 0.5; }
}

/* 动画效果 */
.grid-pop-enter-active {
  transition: all 0.5s cubic-bezier(0.34, 1.56, 0.64, 1);
}
.grid-pop-enter-from {
  opacity: 0;
  transform: scale(0.9);
}

@media (max-width: 1200px) {
  .pastel-grid { grid-template-columns: repeat(4, 1fr); }
}

@media (max-width: 800px) {
  .pastel-grid { grid-template-columns: repeat(2, 1fr); }
}
</style>
