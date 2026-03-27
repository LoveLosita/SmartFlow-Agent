<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue'
import { ElMessage } from 'element-plus'
import { useRoute, useRouter } from 'vue-router'

import {
  applyBatchIntoSchedule,
  createTaskClass,
  deleteScheduleEntries,
  deleteTaskClassItem,
  getTaskClassDetail,
  getTaskClassList,
  getWeekSchedule,
  smartPlanning,
  smartPlanningMulti,
} from '@/api/scheduleCenter'
import CreateTaskClassDialog from '@/components/schedule/CreateTaskClassDialog.vue'
import TaskClassSidebar from '@/components/schedule/TaskClassSidebar.vue'
import WeekPlanningBoard from '@/components/schedule/WeekPlanningBoard.vue'
import type { ApplyBatchIntoScheduleItem, ScheduleWeekData, ScheduleWeekEvent, TaskClassDetail, TaskClassListItem } from '@/types/schedule'
import { formatHeaderDate } from '@/utils/date'

interface SidebarItem {
  key: 'home' | 'task' | 'calendar' | 'ai'
  label: string
  short: string
  to?: '/dashboard' | '/assistant' | '/schedule'
}

interface WeekDayHeader {
  dayOfWeek: number
  label: string
  dateLabel: string
}

const router = useRouter()
const route = useRoute()

const sidebarItems: SidebarItem[] = [
  { key: 'home', label: '总览', short: '总', to: '/dashboard' },
  { key: 'task', label: '任务', short: '任' },
  { key: 'calendar', label: '日程', short: '程', to: '/schedule' },
  { key: 'ai', label: '助手', short: 'AI', to: '/assistant' },
]

const taskClassLoading = ref(false)
const taskClassDetailLoading = ref(false)
const weekLoading = ref(false)
const smartPlanningLoading = ref(false)
const applyingLoading = ref(false)
const deletingLoading = ref(false)
const createDialogVisible = ref(false)
const createDialogLoading = ref(false)

const taskClasses = ref<TaskClassListItem[]>([])
const expandedTaskClassId = ref<number | null>(null)
const expandedTaskClassDetail = ref<TaskClassDetail | null>(null)
const taskClassMultiSelectMode = ref(false)
const selectedTaskClassIds = ref<number[]>([])
const scheduleSelectionMode = ref(false)
const selectedScheduleEventIds = ref<number[]>([])

const liveWeeks = ref<ScheduleWeekData[]>([])
const previewWeeks = ref<ScheduleWeekData[] | null>(null)
const currentWeek = ref<number | null>(null)
const weekBase = ref<number | null>(null)
const baseMonday = ref<Date | null>(null)

const activeSidebarKey = computed<SidebarItem['key']>(() => {
  if (route.path.startsWith('/assistant')) {
    return 'ai'
  }
  if (route.path.startsWith('/schedule')) {
    return 'calendar'
  }
  return 'home'
})

const effectiveSelectedTaskClassIds = computed(() => {
  if (taskClassMultiSelectMode.value) {
    return selectedTaskClassIds.value
  }

  return expandedTaskClassId.value ? [expandedTaskClassId.value] : []
})

const currentWeekData = computed(() => {
  const source = previewWeeks.value ?? liveWeeks.value
  if (!source.length) {
    return null
  }

  const targetWeek = currentWeek.value ?? source[0].week
  return source.find((item) => item.week === targetWeek) ?? source[0]
})

const weekHeaders = computed<WeekDayHeader[]>(() => {
  const weekdayMap = ['Mon', 'Tue', 'Wed', 'Thu', 'Fri', 'Sat', 'Sun']

  return Array.from({ length: 7 }, (_, index) => {
    const dayOfWeek = index + 1
    const date = resolveDateByWeekDay(currentWeek.value, dayOfWeek)
    return {
      dayOfWeek,
      label: weekdayMap[index],
      dateLabel: date ? `${date.getDate()}日` : '',
    }
  })
})

const weekLabel = computed(() => {
  if (!currentWeek.value) {
    return '第--周'
  }
  return `第${numberToChinese(currentWeek.value)}周`
})

const currentTermLabel = computed(() => {
  const now = new Date()
  const semester = now.getMonth() + 1 >= 8 ? '秋季学期' : '春季学期'
  return `${now.getFullYear()}学年${semester}`
})

const showSmartPlanningButton = computed(() =>
  !scheduleSelectionMode.value && effectiveSelectedTaskClassIds.value.length > 0,
)

const showDeleteModeButton = computed(() =>
  !scheduleSelectionMode.value && effectiveSelectedTaskClassIds.value.length === 0,
)

const showApplyButton = computed(() =>
  !scheduleSelectionMode.value &&
  Boolean(previewWeeks.value?.length) &&
  effectiveSelectedTaskClassIds.value.length === 1,
)

function handleSidebarNavigate(item: SidebarItem) {
  if (item.to) {
    if (route.path !== item.to) {
      void router.push(item.to)
    }
    return
  }

  ElMessage.info(`${item.label} 页面正在开发中`)
}

function startOfWeek(date: Date) {
  const next = new Date(date)
  const day = next.getDay()
  const diff = day === 0 ? -6 : 1 - day
  next.setDate(next.getDate() + diff)
  next.setHours(0, 0, 0, 0)
  return next
}

function resolveDateByWeekDay(week: number | null, dayOfWeek: number) {
  if (weekBase.value === null || !baseMonday.value || week === null) {
    return null
  }

  const date = new Date(baseMonday.value)
  date.setDate(baseMonday.value.getDate() + (week - weekBase.value) * 7 + (dayOfWeek - 1))
  return date
}

function numberToChinese(value: number) {
  const digits = ['零', '一', '二', '三', '四', '五', '六', '七', '八', '九']
  if (value <= 10) {
    return value === 10 ? '十' : digits[value]
  }
  if (value < 20) {
    return `十${digits[value % 10]}`
  }
  const tens = Math.floor(value / 10)
  const units = value % 10
  return `${digits[tens]}十${units ? digits[units] : ''}`
}

async function loadTaskClasses() {
  taskClassLoading.value = true
  try {
    taskClasses.value = await getTaskClassList()
  } catch (error) {
    ElMessage.error(error instanceof Error ? error.message : '任务类列表加载失败')
  } finally {
    taskClassLoading.value = false
  }
}

async function loadWeekData(week?: number) {
  weekLoading.value = true
  try {
    const result = await getWeekSchedule(week)
    liveWeeks.value = result

    if (result[0]?.week && weekBase.value === null) {
      weekBase.value = result[0].week
      baseMonday.value = startOfWeek(new Date())
    }

    if (typeof week === 'number') {
      currentWeek.value = week
    } else if (result[0]?.week) {
      currentWeek.value = result[0].week
    }
  } catch (error) {
    ElMessage.error(error instanceof Error ? error.message : '周日程加载失败')
  } finally {
    weekLoading.value = false
  }
}

async function loadTaskClassDetail(taskClassId: number) {
  taskClassDetailLoading.value = true
  try {
    expandedTaskClassDetail.value = await getTaskClassDetail(taskClassId)
  } catch (error) {
    expandedTaskClassDetail.value = null
    ElMessage.error(error instanceof Error ? error.message : '任务类详情加载失败')
  } finally {
    taskClassDetailLoading.value = false
  }
}

async function handleActivateTaskClass(taskClassId: number) {
  if (taskClassMultiSelectMode.value) {
    selectedTaskClassIds.value = selectedTaskClassIds.value.includes(taskClassId)
      ? selectedTaskClassIds.value.filter((id) => id !== taskClassId)
      : [...selectedTaskClassIds.value, taskClassId]
    return
  }

  scheduleSelectionMode.value = false
  selectedScheduleEventIds.value = []
  previewWeeks.value = null

  if (expandedTaskClassId.value === taskClassId) {
    expandedTaskClassId.value = null
    expandedTaskClassDetail.value = null
    return
  }

  expandedTaskClassId.value = taskClassId
  expandedTaskClassDetail.value = null
  await loadTaskClassDetail(taskClassId)
}

function handleToggleTaskClassMultiMode() {
  taskClassMultiSelectMode.value = !taskClassMultiSelectMode.value
  previewWeeks.value = null
  scheduleSelectionMode.value = false
  selectedScheduleEventIds.value = []

  if (taskClassMultiSelectMode.value) {
    selectedTaskClassIds.value = expandedTaskClassId.value ? [expandedTaskClassId.value] : []
    expandedTaskClassId.value = null
    expandedTaskClassDetail.value = null
    return
  }

  selectedTaskClassIds.value = []
}

async function handleDeleteTaskItem(taskItemId: number) {
  try {
    await deleteTaskClassItem(taskItemId)
    ElMessage.success('任务块已删除')
    await loadTaskClasses()
    if (expandedTaskClassId.value) {
      await loadTaskClassDetail(expandedTaskClassId.value)
    }
  } catch (error) {
    ElMessage.error(error instanceof Error ? error.message : '删除任务块失败')
  }
}

async function handleSmartPlanning() {
  const ids = effectiveSelectedTaskClassIds.value
  if (ids.length === 0) {
    ElMessage.info('请先选择任务类')
    return
  }

  smartPlanningLoading.value = true
  try {
    previewWeeks.value = ids.length === 1 ? await smartPlanning(ids[0]!) : await smartPlanningMulti(ids)
    if (previewWeeks.value[0]?.week) {
      currentWeek.value = previewWeeks.value[0].week
    }
    ElMessage.success(ids.length === 1 ? '已生成粗排预览' : '已生成批量粗排预览')
  } catch (error) {
    ElMessage.error(error instanceof Error ? error.message : '智能粗排失败')
  } finally {
    smartPlanningLoading.value = false
  }
}

function toggleScheduleSelectionMode() {
  scheduleSelectionMode.value = !scheduleSelectionMode.value
  selectedScheduleEventIds.value = []
}

function handleToggleScheduleEvent(eventId: number) {
  selectedScheduleEventIds.value = selectedScheduleEventIds.value.includes(eventId)
    ? selectedScheduleEventIds.value.filter((id) => id !== eventId)
    : [...selectedScheduleEventIds.value, eventId]
}

async function handleDeleteSelectedScheduleEvents() {
  const events = currentWeekData.value?.events.filter((event) => selectedScheduleEventIds.value.includes(event.id)) ?? []
  if (!events.length) {
    ElMessage.info('请先选择要解除安排的格子')
    return
  }

  deletingLoading.value = true
  try {
    await deleteScheduleEntries(events.map((event) => ({
      id: event.id,
      delete_course: event.type === 'course',
      delete_embedded_task: Boolean(event.embedded_task_info?.id),
    })))

    ElMessage.success('已完成解除安排')
    scheduleSelectionMode.value = false
    selectedScheduleEventIds.value = []
    previewWeeks.value = null
    await loadWeekData(currentWeek.value ?? undefined)
    await loadTaskClasses()
    if (expandedTaskClassId.value) {
      await loadTaskClassDetail(expandedTaskClassId.value)
    }
  } catch (error) {
    ElMessage.error(error instanceof Error ? error.message : '解除安排失败')
  } finally {
    deletingLoading.value = false
  }
}

function buildApplyItemsFromPreview(weeks: ScheduleWeekData[]) {
  const items: ApplyBatchIntoScheduleItem[] = []

  for (const week of weeks) {
    for (const event of week.events) {
      if (event.status !== 'suggested') {
        continue
      }

      const startSection = (event.order - 1) * 2 + 1
      const taskItemId = event.type === 'course' && event.embedded_task_info?.id
        ? event.embedded_task_info.id
        : event.id

      items.push({
        task_item_id: taskItemId,
        week: week.week,
        day_of_week: event.day_of_week,
        start_section: startSection,
        end_section: startSection + Math.max(1, event.span || 2) - 1,
        embed_course_event_id: event.type === 'course' ? event.id : 0,
      })
    }
  }

  return items
}

async function handleApplyPreview() {
  if (!previewWeeks.value?.length || effectiveSelectedTaskClassIds.value.length !== 1) {
    ElMessage.info('当前预览暂不支持正式应用')
    return
  }

  const items = buildApplyItemsFromPreview(previewWeeks.value)
  if (!items.length) {
    ElMessage.info('当前预览没有可应用的建议排程')
    return
  }

  applyingLoading.value = true
  try {
    await applyBatchIntoSchedule(effectiveSelectedTaskClassIds.value[0]!, items)
    ElMessage.success('已正式应用到日程')
    previewWeeks.value = null
    await loadWeekData(currentWeek.value ?? undefined)
    await loadTaskClasses()
    if (expandedTaskClassId.value) {
      await loadTaskClassDetail(expandedTaskClassId.value)
    }
  } catch (error) {
    ElMessage.error(error instanceof Error ? error.message : '正式应用失败')
  } finally {
    applyingLoading.value = false
  }
}

async function handleCreateTaskClass(payload: Parameters<typeof createTaskClass>[0]) {
  createDialogLoading.value = true
  try {
    await createTaskClass(payload)
    ElMessage.success('任务类已创建')
    createDialogVisible.value = false
    await loadTaskClasses()
  } catch (error) {
    ElMessage.error(error instanceof Error ? error.message : '创建任务类失败')
  } finally {
    createDialogLoading.value = false
  }
}

function goPreviousWeek() {
  if (currentWeek.value === null) {
    return
  }
  currentWeek.value -= 1
}

function goNextWeek() {
  if (currentWeek.value === null) {
    return
  }
  currentWeek.value += 1
}

watch(currentWeek, async (nextWeek, previousWeek) => {
  if (nextWeek === null || nextWeek === previousWeek) {
    return
  }

  if (previewWeeks.value?.some((item) => item.week === nextWeek)) {
    return
  }

  if (previewWeeks.value) {
    previewWeeks.value = null
  }

  await loadWeekData(nextWeek)
})

onMounted(async () => {
  await Promise.all([loadTaskClasses(), loadWeekData()])
})
</script>

<template>
  <main class="schedule-page">
    <div class="schedule-layout">
      <aside class="dashboard-sidebar">
        <div class="dashboard-sidebar__brand">S</div>
        <nav class="dashboard-sidebar__nav">
          <button
            v-for="item in sidebarItems"
            :key="item.key"
            type="button"
            class="dashboard-sidebar__nav-item"
            :class="{ 'dashboard-sidebar__nav-item--active': item.key === activeSidebarKey }"
            @click="handleSidebarNavigate(item)"
          >
            <span>{{ item.short }}</span>
            <small>{{ item.label }}</small>
          </button>
        </nav>
        <button type="button" class="dashboard-sidebar__settings">设</button>
      </aside>

      <section class="schedule-shell">
        <header class="schedule-topbar">
          <div class="schedule-topbar__brand">
            <span class="schedule-topbar__brand-icon" aria-hidden="true">
              <svg width="22" height="22" viewBox="0 0 22 22" fill="none" xmlns="http://www.w3.org/2000/svg">
                <rect x="2" y="2" width="18" height="18" rx="5" fill="currentColor" />
                <path d="M7 8H15V9.6H7V8ZM7 11H15V12.6H7V11ZM7 14H12V15.6H7V14Z" fill="white" />
              </svg>
            </span>
            <strong>日程安排中心</strong>
          </div>

          <div class="schedule-topbar__meta">
            <strong>{{ currentTermLabel }}</strong>
            <span>当前日期: {{ formatHeaderDate(new Date()) }}</span>
          </div>
        </header>

        <div class="schedule-main">
          <TaskClassSidebar
            :task-classes="taskClasses"
            :loading="taskClassLoading"
            :detail-loading="taskClassDetailLoading"
            :expanded-task-class-id="expandedTaskClassId"
            :expanded-task-class-detail="expandedTaskClassDetail"
            :selected-task-class-ids="effectiveSelectedTaskClassIds"
            :task-class-multi-select-mode="taskClassMultiSelectMode"
            @activate="handleActivateTaskClass"
            @toggle-multi-mode="handleToggleTaskClassMultiMode"
            @create="createDialogVisible = true"
            @delete-item="handleDeleteTaskItem"
          />

          <section class="schedule-board-wrap">
            <div class="schedule-board__toolbar">
              <div class="schedule-board__toolbar-left">
                <button
                  v-if="showDeleteModeButton"
                  type="button"
                  class="schedule-board__toolbar-button schedule-board__toolbar-button--ghost"
                  @click="toggleScheduleSelectionMode"
                >
                  多选
                </button>

                <button
                  v-else-if="scheduleSelectionMode"
                  type="button"
                  class="schedule-board__toolbar-button schedule-board__toolbar-button--ghost"
                  @click="toggleScheduleSelectionMode"
                >
                  取消多选
                </button>

                <button
                  v-if="showSmartPlanningButton"
                  type="button"
                  class="schedule-board__toolbar-button schedule-board__toolbar-button--primary"
                  :disabled="smartPlanningLoading"
                  @click="handleSmartPlanning"
                >
                  {{ smartPlanningLoading ? '编排中…' : effectiveSelectedTaskClassIds.length > 1 ? '智能批量编排' : '智能一键编排' }}
                </button>
              </div>

              <div class="schedule-board__toolbar-right">
                <button
                  type="button"
                  class="schedule-board__toolbar-button schedule-board__toolbar-button--ghost"
                  @click="goPreviousWeek"
                >
                  上一周
                </button>
                <button
                  type="button"
                  class="schedule-board__toolbar-button schedule-board__toolbar-button--primary"
                  @click="goNextWeek"
                >
                  下一周
                </button>
              </div>
            </div>

            <WeekPlanningBoard
              :week-label="weekLabel"
              :week-headers="weekHeaders"
              :week-data="currentWeekData"
              :schedule-selection-mode="scheduleSelectionMode"
              :selected-schedule-event-ids="selectedScheduleEventIds"
              @toggle-schedule-event="handleToggleScheduleEvent"
            />

            <div v-if="showApplyButton || scheduleSelectionMode" class="schedule-board__footer">
              <button
                v-if="showApplyButton"
                type="button"
                class="schedule-board__footer-button schedule-board__footer-button--primary"
                :disabled="applyingLoading"
                @click="handleApplyPreview"
              >
                {{ applyingLoading ? '应用中…' : '正式应用日程' }}
              </button>

              <button
                v-if="scheduleSelectionMode"
                type="button"
                class="schedule-board__footer-button schedule-board__footer-button--danger"
                :disabled="deletingLoading || selectedScheduleEventIds.length === 0"
                @click="handleDeleteSelectedScheduleEvents"
              >
                {{ deletingLoading ? '处理中…' : '解除安排/删除课程' }}
              </button>
            </div>
          </section>
        </div>
      </section>
    </div>

    <CreateTaskClassDialog
      v-model="createDialogVisible"
      :loading="createDialogLoading"
      @submit="handleCreateTaskClass"
    />
  </main>
</template>

<style scoped>
.schedule-page {
  height: 100vh;
  padding: 10px;
  overflow: hidden;
  background: linear-gradient(180deg, #f6f9fd 0%, #eff4fb 100%);
}

.schedule-layout {
  height: calc(100vh - 20px);
  display: grid;
  grid-template-columns: 78px minmax(0, 1fr);
  gap: 8px;
  min-height: 0;
}

.dashboard-sidebar {
  height: 100%;
  border-radius: 26px;
  background: linear-gradient(180deg, #165ca8 0%, #104d8f 100%);
  padding: 16px 12px;
  display: grid;
  grid-template-rows: auto 1fr auto;
  gap: 16px;
}

.dashboard-sidebar__brand,
.dashboard-sidebar__settings {
  width: 50px;
  height: 50px;
  border: none;
  border-radius: 16px;
  background: rgba(255, 255, 255, 0.14);
  color: #fff;
  font-weight: 800;
  display: inline-flex;
  align-items: center;
  justify-content: center;
}

.dashboard-sidebar__nav {
  display: grid;
  gap: 12px;
  align-content: start;
}

.dashboard-sidebar__nav-item {
  width: 54px;
  border: none;
  border-radius: 16px;
  background: transparent;
  color: rgba(255, 255, 255, 0.74);
  padding: 10px 8px;
  display: grid;
  justify-items: center;
  gap: 5px;
  cursor: pointer;
}

.dashboard-sidebar__nav-item span {
  width: 32px;
  height: 32px;
  border-radius: 11px;
  display: inline-flex;
  align-items: center;
  justify-content: center;
  background: rgba(255, 255, 255, 0.08);
  font-weight: 700;
}

.dashboard-sidebar__nav-item small {
  font-size: 10px;
}

.dashboard-sidebar__nav-item--active {
  background: rgba(255, 255, 255, 0.08);
  color: #fff;
}

.schedule-shell {
  min-width: 0;
  min-height: 0;
  border-radius: 28px;
  border: 1px solid rgba(215, 224, 237, 0.84);
  background: linear-gradient(180deg, rgba(255, 255, 255, 0.96), rgba(248, 251, 255, 0.98));
  overflow: hidden;
  display: grid;
  grid-template-rows: auto minmax(0, 1fr);
}

.schedule-topbar {
  padding: 14px 24px;
  border-bottom: 1px solid rgba(218, 227, 239, 0.92);
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 20px;
  min-width: 0;
}

.schedule-topbar__brand {
  display: inline-flex;
  align-items: center;
  gap: 16px;
  min-width: 0;
}

.schedule-topbar__brand-icon {
  width: 44px;
  height: 44px;
  border-radius: 14px;
  background: linear-gradient(180deg, #1b64cf 0%, #0f56b7 100%);
  color: #ffffff;
  display: inline-flex;
  align-items: center;
  justify-content: center;
}

.schedule-topbar__brand strong {
  color: #19263d;
  font-size: 20px;
  font-weight: 800;
  min-width: 0;
}

.schedule-topbar__meta {
  display: grid;
  justify-items: end;
  gap: 6px;
  color: #8493aa;
  min-width: 0;
  text-align: right;
}

.schedule-topbar__meta strong {
  color: #344055;
  font-size: 14px;
}

.schedule-topbar__meta span {
  font-size: 12px;
}

.schedule-main {
  min-width: 0;
  min-height: 0;
  display: grid;
  grid-template-columns: clamp(280px, 26vw, 380px) minmax(0, 1fr);
  overflow: hidden;
}

.schedule-board-wrap {
  min-width: 0;
  min-height: 0;
  padding: 18px 22px 22px;
  display: grid;
  grid-template-rows: auto minmax(0, 1fr) auto;
  gap: 14px;
  overflow: hidden;
}

.schedule-board__toolbar {
  display: flex;
  justify-content: space-between;
  gap: 16px;
  align-items: center;
  min-width: 0;
  flex-wrap: wrap;
}

.schedule-board__toolbar-left,
.schedule-board__toolbar-right {
  display: flex;
  gap: 10px;
  align-items: center;
  min-width: 0;
  flex-wrap: wrap;
}

.schedule-board__toolbar-right {
  margin-left: auto;
}

.schedule-board__toolbar-button,
.schedule-board__footer-button {
  height: 36px;
  border-radius: 10px;
  border: 1px solid transparent;
  padding: 0 18px;
  font-size: 13px;
  font-weight: 700;
  cursor: pointer;
  transition: border-color 0.16s ease, background-color 0.16s ease, color 0.16s ease;
}

.schedule-board__toolbar-button--primary,
.schedule-board__footer-button--primary {
  background: linear-gradient(180deg, #1d64d1 0%, #1157bd 100%);
  color: #ffffff;
}

.schedule-board__toolbar-button--primary:hover,
.schedule-board__footer-button--primary:hover {
  background: linear-gradient(180deg, #1757b8 0%, #0f4ea9 100%);
}

.schedule-board__toolbar-button--ghost {
  border-color: rgba(27, 96, 208, 0.22);
  background: #ffffff;
  color: #1e66d4;
}

.schedule-board__toolbar-button--ghost:hover {
  border-color: rgba(27, 96, 208, 0.38);
  background: #f2f7ff;
}

.schedule-board__footer {
  display: flex;
  justify-content: flex-end;
  gap: 12px;
}

.schedule-board__footer-button--danger {
  background: #bb3326;
  color: #ffffff;
}

.schedule-board__footer-button--danger:disabled,
.schedule-board__footer-button--primary:disabled,
.schedule-board__toolbar-button:disabled {
  opacity: 0.46;
  cursor: not-allowed;
}

@media (max-width: 1520px) {
  .schedule-main {
    grid-template-columns: clamp(260px, 24vw, 332px) minmax(0, 1fr);
  }

  .schedule-board-wrap {
    padding: 16px 18px 18px;
  }
}

@media (max-width: 1380px) {
  .schedule-main {
    grid-template-columns: 1fr;
  }

  .schedule-topbar {
    flex-wrap: wrap;
  }

  .schedule-topbar__meta {
    justify-items: start;
    text-align: left;
  }
}

@media (max-height: 900px) {
  .schedule-page {
    padding: 8px;
  }

  .schedule-layout {
    height: calc(100vh - 16px);
  }

  .schedule-topbar {
    padding: 12px 18px;
  }

  .schedule-topbar__brand {
    gap: 12px;
  }

  .schedule-topbar__brand-icon {
    width: 38px;
    height: 38px;
    border-radius: 12px;
  }

  .schedule-topbar__brand strong {
    font-size: 18px;
  }

  .schedule-board-wrap {
    padding-top: 14px;
    padding-bottom: 16px;
    gap: 10px;
  }

  .schedule-board__toolbar-button,
  .schedule-board__footer-button {
    height: 34px;
    padding: 0 14px;
  }
}

@media (max-height: 820px) {
  .schedule-topbar {
    padding-top: 10px;
    padding-bottom: 10px;
  }

  .schedule-topbar__meta {
    gap: 4px;
  }

  .schedule-board-wrap {
    padding-left: 14px;
    padding-right: 14px;
    padding-bottom: 14px;
  }
}

@media (max-width: 1180px) {
  .schedule-layout {
    grid-template-columns: 1fr;
  }

  .dashboard-sidebar {
    display: none;
  }

  .schedule-main {
    grid-template-columns: 1fr;
  }
}
</style>
