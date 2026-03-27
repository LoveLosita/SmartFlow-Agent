<script setup lang="ts">
import { computed } from 'vue'

import type { ScheduleWeekData, ScheduleWeekEvent } from '@/types/schedule'

interface WeekDayHeader {
  dayOfWeek: number
  label: string
  dateLabel: string
}

interface SectionSlot {
  order: number
  title: string
  timeRange: string
}

const props = defineProps<{
  weekLabel: string
  weekHeaders: WeekDayHeader[]
  weekData: ScheduleWeekData | null
  scheduleSelectionMode: boolean
  selectedScheduleEventIds: number[]
}>()

const emit = defineEmits<{
  toggleScheduleEvent: [eventId: number]
}>()

const sectionSlots: SectionSlot[] = [
  { order: 1, title: '1-2', timeRange: '08:00\n09:40' },
  { order: 2, title: '3-4', timeRange: '10:15\n11:55' },
  { order: 3, title: '5-6', timeRange: '14:00\n15:40' },
  { order: 4, title: '7-8', timeRange: '16:15\n17:55' },
  { order: 5, title: '9-10', timeRange: '19:00\n20:40' },
  { order: 6, title: '11-12', timeRange: '20:50\n22:30' },
]

const eventLookup = computed(() => {
  const map = new Map<string, ScheduleWeekEvent>()

  for (const event of props.weekData?.events ?? []) {
    map.set(`${event.day_of_week}-${event.order}`, event)
  }

  return map
})

function resolveEvent(dayOfWeek: number, order: number) {
  return eventLookup.value.get(`${dayOfWeek}-${order}`)
}

function isSelected(eventId: number) {
  return props.selectedScheduleEventIds.includes(eventId)
}

function resolveEventTone(event?: ScheduleWeekEvent) {
  if (!event || event.type === 'empty') {
    return 'empty'
  }

  if (event.type === 'course') {
    return 'course'
  }

  const toneByOrder: Record<number, string> = {
    1: 'amber',
    2: 'mint',
    3: 'emerald',
    4: 'rose',
    5: 'violet',
    6: 'sky',
  }

  return toneByOrder[event.order] ?? 'task'
}

function resolveCellTitle(event?: ScheduleWeekEvent) {
  if (!event || event.type === 'empty') {
    return '空'
  }
  return event.name
}

function resolveCellMeta(event?: ScheduleWeekEvent) {
  if (!event || event.type === 'empty') {
    return ''
  }
  return event.location || '未定'
}
</script>

<template>
  <section class="planning-board">
    <header class="planning-board__header">
      <strong>{{ weekLabel }}</strong>
    </header>

    <div class="planning-board__grid">
      <div class="planning-board__corner" />

      <div v-for="header in weekHeaders" :key="header.dayOfWeek" class="planning-board__day-head">
        <span>{{ header.label }}</span>
        <small>{{ header.dateLabel }}</small>
      </div>

      <template v-for="slot in sectionSlots" :key="slot.order">
        <div class="planning-board__time-cell">
          <strong>{{ slot.title }}</strong>
          <small>{{ slot.timeRange }}</small>
        </div>

        <article
          v-for="header in weekHeaders"
          :key="`${header.dayOfWeek}-${slot.order}`"
          class="planning-board__cell"
          :class="[
            `planning-board__cell--${resolveEventTone(resolveEvent(header.dayOfWeek, slot.order))}`,
            {
              'planning-board__cell--selectable': scheduleSelectionMode && resolveEvent(header.dayOfWeek, slot.order)?.type !== 'empty',
              'planning-board__cell--selected': resolveEvent(header.dayOfWeek, slot.order) && isSelected(resolveEvent(header.dayOfWeek, slot.order)!.id),
            },
          ]"
        >
          <button
            v-if="scheduleSelectionMode && resolveEvent(header.dayOfWeek, slot.order)?.type !== 'empty'"
            type="button"
            class="planning-board__checkbox"
            :class="{ 'planning-board__checkbox--active': isSelected(resolveEvent(header.dayOfWeek, slot.order)!.id) }"
            @click="emit('toggleScheduleEvent', resolveEvent(header.dayOfWeek, slot.order)!.id)"
          />

          <template v-if="resolveEvent(header.dayOfWeek, slot.order)">
            <div class="planning-board__cell-main">
              <strong>{{ resolveCellTitle(resolveEvent(header.dayOfWeek, slot.order)) }}</strong>
              <span>{{ resolveCellMeta(resolveEvent(header.dayOfWeek, slot.order)) }}</span>
            </div>
          </template>
        </article>
      </template>
    </div>
  </section>
</template>

<style scoped>
.planning-board {
  --planning-grid-padding-x: 24px;
  --planning-grid-padding-y: 28px;
  --planning-grid-gap-x: 12px;
  --planning-grid-gap-y: 10px;
  --planning-time-column-width: 74px;
  --planning-day-column-min: 96px;
  --planning-cell-height: clamp(72px, 9.2vh, 112px);
  min-width: 0;
  min-height: 0;
  border-radius: 28px;
  border: 1px solid rgba(214, 223, 236, 0.82);
  background: linear-gradient(180deg, rgba(252, 253, 255, 0.98), rgba(248, 251, 255, 0.98));
  box-shadow: inset 0 1px 0 rgba(255, 255, 255, 0.82);
  display: grid;
  grid-template-rows: auto minmax(0, 1fr);
  overflow: hidden;
}

.planning-board__header {
  padding: 18px 28px 16px;
  border-bottom: 1px solid rgba(221, 229, 240, 0.86);
  color: #1f2b42;
  font-size: 18px;
}

.planning-board__grid {
  min-width: 0;
  min-height: 0;
  display: grid;
  grid-template-columns: var(--planning-time-column-width) repeat(7, minmax(var(--planning-day-column-min), 1fr));
  gap: var(--planning-grid-gap-y) var(--planning-grid-gap-x);
  padding: var(--planning-grid-padding-y) var(--planning-grid-padding-x) 24px;
  overflow: auto;
  scrollbar-gutter: stable both-edges;
}

.planning-board__corner {
  min-height: 1px;
}

.planning-board__day-head {
  display: grid;
  justify-items: center;
  gap: 4px;
  color: #8ca0bd;
}

.planning-board__day-head span {
  font-size: 14px;
  font-weight: 700;
  letter-spacing: 0.08em;
}

.planning-board__day-head small {
  font-size: 12px;
}

.planning-board__time-cell {
  min-height: var(--planning-cell-height);
  display: grid;
  align-content: center;
  justify-items: end;
  color: #9aacbf;
  padding-right: 8px;
}

.planning-board__time-cell strong {
  font-size: 15px;
  color: #8da0bc;
}

.planning-board__time-cell small {
  white-space: pre-line;
  text-align: right;
  line-height: 1.35;
  font-size: 11px;
}

.planning-board__cell {
  position: relative;
  min-height: var(--planning-cell-height);
  border-radius: 22px;
  border: 1px solid rgba(228, 234, 243, 0.92);
  padding: 18px 14px;
  display: flex;
  align-items: center;
  justify-content: center;
  text-align: center;
  overflow: hidden;
}

.planning-board__cell-main {
  display: grid;
  gap: 10px;
  min-width: 0;
}

.planning-board__cell-main strong {
  color: #7387a3;
  font-size: 15px;
  line-height: 1.35;
  font-weight: 700;
  min-width: 0;
  overflow-wrap: anywhere;
}

.planning-board__cell-main span {
  color: #9badc5;
  font-size: 12px;
  min-width: 0;
  overflow-wrap: anywhere;
}

.planning-board__cell--course {
  background: #acd6f4;
}

.planning-board__cell--course .planning-board__cell-main strong,
.planning-board__cell--course .planning-board__cell-main span {
  color: #2576cc;
}

.planning-board__cell--amber {
  background: #ffe58b;
}

.planning-board__cell--amber .planning-board__cell-main strong,
.planning-board__cell--amber .planning-board__cell-main span {
  color: #7d6917;
}

.planning-board__cell--mint {
  background: #d7f7a7;
}

.planning-board__cell--mint .planning-board__cell-main strong,
.planning-board__cell--mint .planning-board__cell-main span,
.planning-board__cell--emerald .planning-board__cell-main strong,
.planning-board__cell--emerald .planning-board__cell-main span {
  color: #72a91d;
}

.planning-board__cell--emerald {
  background: #d3f3ac;
}

.planning-board__cell--rose {
  background: #f6dfe2;
}

.planning-board__cell--rose .planning-board__cell-main strong,
.planning-board__cell--rose .planning-board__cell-main span {
  color: #e6696e;
}

.planning-board__cell--violet {
  background: #e9dcfb;
}

.planning-board__cell--sky {
  background: #d8ecfb;
}

.planning-board__cell--empty {
  background: #f8fbff;
}

.planning-board__cell--selectable {
  cursor: pointer;
}

.planning-board__cell--selected {
  box-shadow: inset 0 0 0 2px rgba(32, 102, 212, 0.52);
}

.planning-board__checkbox {
  position: absolute;
  top: 16px;
  right: 16px;
  width: 20px;
  height: 20px;
  border-radius: 6px;
  border: 1px solid rgba(118, 133, 160, 0.46);
  background: rgba(255, 255, 255, 0.92);
  cursor: pointer;
}

.planning-board__checkbox--active {
  border-color: #1e66d4;
  background: #1e66d4;
  box-shadow: inset 0 0 0 3px #ffffff;
}

@media (max-width: 1560px) {
  .planning-board__grid {
    --planning-time-column-width: 64px;
    --planning-day-column-min: 92px;
    --planning-grid-padding-x: 18px;
    --planning-grid-padding-y: 22px;
    --planning-grid-gap-x: 10px;
  }
}

@media (max-width: 1380px) {
  .planning-board__header {
    padding: 16px 20px 14px;
  }

  .planning-board__grid {
    --planning-time-column-width: 58px;
    --planning-day-column-min: 84px;
    --planning-grid-padding-x: 14px;
    --planning-grid-padding-y: 18px;
    --planning-grid-gap-x: 8px;
    --planning-grid-gap-y: 8px;
  }

  .planning-board__time-cell,
  .planning-board__cell {
    min-height: 98px;
  }

  .planning-board__cell {
    padding: 14px 10px;
    border-radius: 18px;
  }

  .planning-board__cell-main {
    gap: 8px;
  }

  .planning-board__cell-main strong {
    font-size: 14px;
  }
}

@media (max-width: 1180px) {
  .planning-board__grid {
    --planning-time-column-width: 56px;
    --planning-day-column-min: 78px;
  }

  .planning-board__day-head span {
    font-size: 13px;
  }

  .planning-board__day-head small,
  .planning-board__cell-main span {
    font-size: 11px;
  }
}

@media (max-height: 900px) {
  .planning-board {
    --planning-grid-padding-y: 18px;
    --planning-cell-height: clamp(66px, 8.2vh, 92px);
  }

  .planning-board__header {
    padding-top: 14px;
    padding-bottom: 12px;
  }

  .planning-board__header strong {
    font-size: 16px;
  }
}

@media (max-height: 820px) {
  .planning-board {
    --planning-grid-padding-y: 14px;
    --planning-grid-gap-y: 6px;
    --planning-cell-height: clamp(58px, 7.2vh, 82px);
  }

  .planning-board__time-cell strong,
  .planning-board__cell-main strong {
    font-size: 13px;
  }

  .planning-board__time-cell small,
  .planning-board__day-head small,
  .planning-board__cell-main span {
    font-size: 10px;
  }

  .planning-board__cell {
    padding: 12px 8px;
  }
}
</style>
