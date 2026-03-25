<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, onMounted, reactive, ref, watch } from 'vue'
import { ElMessage } from 'element-plus'
import { useRoute, useRouter } from 'vue-router'

import TaskQuadrantCard from '@/components/dashboard/TaskQuadrantCard.vue'
import TodayTimeline from '@/components/dashboard/TodayTimeline.vue'
import { completeTask, createTask, getTasks, undoCompleteTask } from '@/api/task'
import { getTodaySchedule } from '@/api/schedule'
import { useAuthStore } from '@/stores/auth'
import type { TaskItem, TodayEvent } from '@/types/dashboard'
import { formatHeaderDate } from '@/utils/date'

const router = useRouter()
const route = useRoute()
const authStore = useAuthStore()

const pageLoading = ref(false)
const taskLoading = ref(false)
const scheduleLoading = ref(false)
const createTaskLoading = ref(false)
const logoutLoading = ref(false)
const createTaskDialogVisible = ref(false)
const dashboardLayoutRef = ref<HTMLElement | null>(null)
const dashboardMainRef = ref<HTMLElement | null>(null)
const dashboardMainInnerRef = ref<HTMLElement | null>(null)
const dashboardTopbarRef = ref<HTMLElement | null>(null)
const dashboardContentRef = ref<HTMLElement | null>(null)
const dashboardMainScale = ref(1)

const tasks = ref<TaskItem[]>([])
const todayEvents = ref<TodayEvent[]>([])

const sidebarWidth = ref(78)

const taskForm = reactive<{
  title: string
  priority_group: number
  deadline_at: Date | null
}>({
  title: '',
  priority_group: 2,
  deadline_at: null,
})

interface SidebarItem {
  key: 'home' | 'task' | 'calendar' | 'ai'
  label: string
  short: string
  to?: '/dashboard' | '/assistant' | '/schedule'
}

const sidebarItems: SidebarItem[] = [
  { key: 'home', label: '总览', short: '总', to: '/dashboard' },
  { key: 'task', label: '任务', short: '任' },
  { key: 'calendar', label: '日程', short: '程', to: '/schedule' },
  { key: 'ai', label: '助手', short: 'AI', to: '/assistant' },
]

const activeSidebarKey = computed<SidebarItem['key']>(() => {
  if (route.path.startsWith('/assistant')) {
    return 'ai'
  }
  if (route.path.startsWith('/schedule')) {
    return 'calendar'
  }
  return 'home'
})

const quadrantOrder = [1, 2, 3, 4] as const

const quadrantMeta: Record<
  (typeof quadrantOrder)[number],
  { title: string; caption: string; tone: 'danger' | 'primary' | 'warning' | 'slate'; emptyText: string }
> = {
  1: {
    title: '重要且紧急',
    caption: '优先处理',
    tone: 'danger',
    emptyText: '暂无需要立刻推进的事项',
  },
  2: {
    title: '重要不紧急',
    caption: '持续推进',
    tone: 'primary',
    emptyText: '这里适合放长期投入的关键任务',
  },
  3: {
    title: '简单不重要',
    caption: '顺手完成',
    tone: 'warning',
    emptyText: '暂无高频但低价值的小任务',
  },
  4: {
    title: '不简单不重要',
    caption: '谨慎投入',
    tone: 'slate',
    emptyText: '这里可以放暂缓事项或后续再评估的任务',
  },
}

const pageTitleDate = computed(() => formatHeaderDate(new Date()))
const greetingName = computed(() => authStore.lastUsername || 'SmartFlow 用户')
const layoutStyle = computed(() => ({
  '--dashboard-sidebar-width': `${sidebarWidth.value}px`,
}))
const dashboardMainScaleStyle = computed(() => ({
  '--dashboard-main-scale': `${dashboardMainScale.value}`,
}))

const groupedTasks = computed(() => {
  const groups: Record<number, TaskItem[]> = {
    1: [],
    2: [],
    3: [],
    4: [],
  }

  for (const task of tasks.value) {
    if (groups[task.priority_group]) {
      groups[task.priority_group].push(task)
    }
  }

  for (const key of Object.keys(groups)) {
    groups[Number(key)].sort((left, right) => {
      if (left.is_completed !== right.is_completed) {
        return left.is_completed ? 1 : -1
      }
      return left.id - right.id
    })
  }

  return groups
})

async function loadTasksData() {
  taskLoading.value = true
  try {
    tasks.value = await getTasks()
  } catch (error) {
    ElMessage.warning(error instanceof Error ? error.message : '任务加载失败')
  } finally {
    taskLoading.value = false
  }
}

async function loadScheduleData() {
  scheduleLoading.value = true
  try {
    const schedules = await getTodaySchedule()
    todayEvents.value = schedules.flatMap((item) => item.events).sort((left, right) => left.order - right.order)
  } catch (error) {
    ElMessage.warning(error instanceof Error ? error.message : '今日日程加载失败')
  } finally {
    scheduleLoading.value = false
  }
}

async function loadDashboardData() {
  pageLoading.value = true
  await Promise.allSettled([loadTasksData(), loadScheduleData()])
  pageLoading.value = false
}

async function handleTaskToggle(task: TaskItem) {
  try {
    if (task.is_completed) {
      const result = await undoCompleteTask(task.id)
      task.is_completed = result.is_completed
      task.status = result.status
      ElMessage.success('任务已恢复为未完成')
      return
    }

    const result = await completeTask(task.id)
    task.is_completed = result.is_completed
    task.status = result.status
    ElMessage.success(result.already_completed ? '任务已经是完成状态' : '任务已标记为完成')
  } catch (error) {
    ElMessage.error(error instanceof Error ? error.message : '任务更新失败')
  }
}

function openCreateTaskDialog() {
  taskForm.title = ''
  taskForm.priority_group = 2
  taskForm.deadline_at = null
  createTaskDialogVisible.value = true
}

async function handleCreateTask() {
  if (!taskForm.title.trim()) {
    ElMessage.warning('请先填写任务标题')
    return
  }

  createTaskLoading.value = true
  try {
    const created = await createTask({
      title: taskForm.title.trim(),
      priority_group: taskForm.priority_group,
      deadline_at: taskForm.deadline_at ? taskForm.deadline_at.toISOString() : null,
    })

    tasks.value.unshift({
      id: created.id,
      user_id: 0,
      title: created.title,
      priority_group: created.priority_group,
      status: created.status,
      deadline: created.deadline_at ?? '',
      is_completed: false,
    })

    createTaskDialogVisible.value = false
    ElMessage.success('任务已添加')
  } catch (error) {
    ElMessage.error(error instanceof Error ? error.message : '创建任务失败')
  } finally {
    createTaskLoading.value = false
  }
}

async function handleLogout() {
  logoutLoading.value = true

  try {
    await authStore.logout()
    ElMessage.success('已安全退出登录')
  } catch (error) {
    ElMessage.warning(error instanceof Error ? `${error.message}，本地登录态已清除` : '退出接口异常，本地登录态已清除')
  } finally {
    logoutLoading.value = false
    await router.push('/auth')
  }
}

function handleCourseImportEntry() {
  ElMessage.info('课表导入入口已预留，下一步我可以继续把导入流程页接出来')
}

function handleSidebarNavigate(item: SidebarItem) {
  // 1. 已接通路由的入口直接跳转，避免侧栏按钮成为“仅装饰”元素。
  // 2. 未接通的入口先给出明确提示，防止用户误以为点击失效。
  // 3. 同路由不重复 push，避免产生无意义导航与日志噪音。
  if (item.to) {
    if (route.path !== item.to) {
      void router.push(item.to)
    }
    return
  }

  ElMessage.info(`${item.label} 页面正在开发中`)
}

function clampSidebarWidth(nextWidth: number) {
  return Math.min(110, Math.max(68, nextWidth))
}

function startResize(type: 'sidebar', event: PointerEvent) {
  const layout = dashboardLayoutRef.value
  if (!layout || window.innerWidth <= 1380) {
    return
  }

  const rect = layout.getBoundingClientRect()
  const startX = event.clientX
  const startSidebarWidth = sidebarWidth.value

  // 1. 拖拽时先记录容器宽度和起始位置，避免每次 move 都重复读布局造成抖动。
  // 2. 主区域需要保留最小宽度，防止侧栏被拖得过宽后正文不可读。
  // 3. 结束时统一解绑事件，避免指针松开后仍残留拖拽状态。
  const handlePointerMove = (moveEvent: PointerEvent) => {
    const deltaX = moveEvent.clientX - startX
    const splitterTotalWidth = 10
    const minMainWidth = 560

    const nextSidebarWidth = clampSidebarWidth(startSidebarWidth + deltaX)
    const maxSidebarWidth = rect.width - splitterTotalWidth - minMainWidth
    sidebarWidth.value = Math.min(nextSidebarWidth, Math.max(68, maxSidebarWidth))
  }

  const stopResize = () => {
    window.removeEventListener('pointermove', handlePointerMove)
    window.removeEventListener('pointerup', stopResize)
    document.body.classList.remove('dashboard-resizing')
  }

  document.body.classList.add('dashboard-resizing')
  window.addEventListener('pointermove', handlePointerMove)
  window.addEventListener('pointerup', stopResize)
}

function syncDashboardMainScale() {
  const main = dashboardMainRef.value
  const inner = dashboardMainInnerRef.value
  const topbar = dashboardTopbarRef.value
  const content = dashboardContentRef.value
  if (!main || !inner || !topbar || !content || window.innerWidth <= 980) {
    dashboardMainScale.value = 1
    return
  }

  // 1. 先回到 1:1，确保拿到未缩放状态下的真实高度。
  // 2. 自然高度按“顶部栏高度 + 内容 scrollHeight + 栅格间距”计算，避免被 1fr 约束低估。
  // 3. 仅在桌面端启用缩放，小屏仍走原生滚动布局。
  dashboardMainScale.value = 1
  window.requestAnimationFrame(() => {
    const availableHeight = main.clientHeight
    const gridGap = 10
    const naturalHeight = topbar.getBoundingClientRect().height + content.scrollHeight + gridGap
    if (!availableHeight || !naturalHeight) {
      dashboardMainScale.value = 1
      return
    }

    // 预留适中安全边距，优先保证底部卡片完整可见，避免再次出现裁切。
    const nextScale = Math.min(1, (availableHeight / naturalHeight) * 0.98)
    dashboardMainScale.value = Number(nextScale.toFixed(4))
  })
}

onMounted(async () => {
  await loadDashboardData()
  await nextTick()
  syncDashboardMainScale()
  window.addEventListener('resize', syncDashboardMainScale)
})

onBeforeUnmount(() => {
  document.body.classList.remove('dashboard-resizing')
  window.removeEventListener('resize', syncDashboardMainScale)
})

watch(
  [() => tasks.value.length, () => todayEvents.value.length, pageLoading],
  async () => {
    await nextTick()
    syncDashboardMainScale()
  },
  { flush: 'post' },
)

watch(
  sidebarWidth,
  async () => {
    await nextTick()
    syncDashboardMainScale()
  },
  { flush: 'post' },
)
</script>

<template>
  <main class="dashboard-page">
    <div ref="dashboardLayoutRef" class="dashboard-layout" :style="layoutStyle">
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

      <div
        class="dashboard-splitter"
        role="separator"
        aria-label="调整侧边导航宽度"
        @pointerdown.prevent="startResize('sidebar', $event)"
      >
        <span class="dashboard-splitter__line" />
      </div>

      <section ref="dashboardMainRef" class="dashboard-main">
        <div ref="dashboardMainInnerRef" class="dashboard-main__scaled" :style="dashboardMainScaleStyle">
          <header ref="dashboardTopbarRef" class="dashboard-topbar glass-panel">
            <div>
              <div class="dashboard-topbar__brandline">
                <strong>AI 智慧日程系统</strong>
                <span>{{ pageTitleDate }}</span>
              </div>
            </div>

            <div class="dashboard-topbar__actions">
              <button
                type="button"
                class="dashboard-topbar__logout"
                :disabled="logoutLoading"
                @click="handleLogout"
              >
                {{ logoutLoading ? '退出中…' : '登出' }}
              </button>
              <div class="dashboard-topbar__profile">
                <strong>{{ greetingName }}</strong>
                <span>{{ greetingName.slice(0, 1).toUpperCase() }}</span>
              </div>
            </div>
          </header>

          <div ref="dashboardContentRef" class="dashboard-content page-shell">
            <TodayTimeline :events="todayEvents" :loading="scheduleLoading || pageLoading" />

            <div class="dashboard-actions">
              <button type="button" class="dashboard-actions__primary" @click="openCreateTaskDialog">
                添加任务
              </button>
            </div>

            <section class="dashboard-quadrants">
              <TaskQuadrantCard
                v-for="group in quadrantOrder"
                :key="group"
                :title="quadrantMeta[group].title"
                :caption="quadrantMeta[group].caption"
                :tone="quadrantMeta[group].tone"
                :empty-text="quadrantMeta[group].emptyText"
                :count="groupedTasks[group].length"
                :tasks="groupedTasks[group]"
                :loading="taskLoading || pageLoading"
                @toggle="handleTaskToggle"
              />
            </section>

            <section class="dashboard-import glass-panel">
              <div class="dashboard-import__content">
                <p class="dashboard-import__eyebrow">课程导入</p>
                <h2>导入课表</h2>
                <p>导入课表后，可以在安排日程时避开上课时间，后续我会继续把导入流程页接完整。</p>
                <button type="button" class="dashboard-import__button" @click="handleCourseImportEntry">
                  开始导入
                </button>
              </div>
              <div class="dashboard-import__shape">
                <span class="dashboard-import__shape-ring" />
                <span class="dashboard-import__shape-core" />
              </div>
            </section>
          </div>
        </div>
      </section>

    </div>

    <el-dialog
      v-model="createTaskDialogVisible"
      title="添加任务"
      width="460px"
      align-center
      class="dashboard-dialog"
    >
      <el-form label-position="top">
        <el-form-item label="任务标题">
          <el-input v-model="taskForm.title" maxlength="255" placeholder="例如：完成数据库复习" />
        </el-form-item>

        <el-form-item label="优先级象限">
          <el-select v-model="taskForm.priority_group" class="dashboard-dialog__select">
            <el-option :value="1" label="1 - 重要且紧急" />
            <el-option :value="2" label="2 - 重要不紧急" />
            <el-option :value="3" label="3 - 简单不重要" />
            <el-option :value="4" label="4 - 不简单不重要" />
          </el-select>
        </el-form-item>

        <el-form-item label="截止时间">
          <el-date-picker
            v-model="taskForm.deadline_at"
            type="datetime"
            placeholder="可选，不设置也可以"
            class="dashboard-dialog__select"
          />
        </el-form-item>
      </el-form>

      <template #footer>
        <el-button @click="createTaskDialogVisible = false">取消</el-button>
        <el-button type="primary" :loading="createTaskLoading" @click="handleCreateTask">保存任务</el-button>
      </template>
    </el-dialog>
  </main>
</template>

<style scoped>
.dashboard-page {
  height: 100vh;
  padding: 10px;
  overflow: hidden;
}

.dashboard-layout {
  --dashboard-sidebar-width: 78px;
  height: calc(100vh - 20px);
  display: grid;
  grid-template-columns:
    var(--dashboard-sidebar-width)
    10px
    minmax(0, 1fr);
  gap: 8px;
  align-items: stretch;
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

.dashboard-splitter {
  position: relative;
  height: 100%;
  display: flex;
  align-items: center;
  justify-content: center;
  cursor: col-resize;
  touch-action: none;
}

.dashboard-splitter__line {
  width: 4px;
  height: 64px;
  border-radius: 999px;
  background: linear-gradient(180deg, rgba(145, 163, 188, 0.24), rgba(88, 124, 177, 0.4), rgba(145, 163, 188, 0.24));
  transition:
    background-color 0.18s ease,
    transform 0.18s ease;
}

.dashboard-splitter:hover .dashboard-splitter__line {
  transform: scaleX(1.15);
  background: linear-gradient(180deg, rgba(104, 140, 194, 0.34), rgba(42, 108, 214, 0.62), rgba(104, 140, 194, 0.34));
}

.dashboard-main {
  min-width: 0;
  min-height: 0;
  overflow: hidden;
}

.dashboard-main__scaled {
  --dashboard-main-scale: 1;
  min-width: 0;
  min-height: 0;
  width: calc(100% / var(--dashboard-main-scale));
  height: calc(100% / var(--dashboard-main-scale));
  display: grid;
  grid-template-rows: auto auto;
  gap: 10px;
  transform: scale(var(--dashboard-main-scale));
  transform-origin: top left;
}

.dashboard-topbar {
  border-radius: 24px;
  padding: 18px 22px;
  display: flex;
  justify-content: space-between;
  align-items: center;
  border: 1px solid rgba(17, 24, 39, 0.08);
}

.dashboard-topbar__brandline {
  display: flex;
  align-items: center;
  gap: 14px;
}

.dashboard-topbar__brandline strong {
  font-size: 18px;
  color: #14233a;
}

.dashboard-topbar__brandline span {
  color: #677588;
  font-size: 13px;
}

.dashboard-topbar__actions {
  display: flex;
  align-items: center;
  gap: 14px;
}

.dashboard-topbar__logout {
  min-width: 88px;
  height: 38px;
  border-radius: 13px;
  border: 1px solid rgba(28, 98, 205, 0.22);
  background: #f9fbff;
  color: #1d63cf;
  cursor: pointer;
}

.dashboard-topbar__profile {
  display: flex;
  align-items: center;
  gap: 10px;
}

.dashboard-topbar__profile strong {
  font-size: 13px;
}

.dashboard-topbar__profile span {
  width: 38px;
  height: 38px;
  border-radius: 999px;
  background: #eef3fb;
  color: #314156;
  display: inline-flex;
  align-items: center;
  justify-content: center;
  font-weight: 800;
}

.dashboard-content {
  width: 100%;
  max-width: none;
  min-height: 0;
  min-width: 0;
  display: grid;
  gap: 14px;
  overflow-y: hidden;
  overflow-x: hidden;
  padding-right: 0;
  align-content: start;
}

.dashboard-actions {
  display: flex;
  justify-content: flex-end;
}

.dashboard-actions__primary {
  height: 42px;
  padding: 0 20px;
  border: none;
  border-radius: 15px;
  background: linear-gradient(180deg, #246ff1 0%, #1a5dc8 100%);
  color: #fff;
  font-weight: 700;
  cursor: pointer;
}

.dashboard-quadrants {
  min-width: 0;
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(280px, 1fr));
  gap: 14px;
}

.dashboard-import {
  border-radius: 26px;
  padding: 28px 30px;
  min-height: 240px;
  background: linear-gradient(135deg, #0f5ca9 0%, #0b4b89 100%);
  color: #fff;
  display: flex;
  justify-content: space-between;
  gap: 20px;
  overflow: hidden;
  position: relative;
}

.dashboard-import__content {
  position: relative;
  z-index: 1;
  max-width: 460px;
  padding-bottom: 22px;
}

.dashboard-import__eyebrow {
  margin: 0 0 8px;
  opacity: 0.76;
  text-transform: uppercase;
  letter-spacing: 0.08em;
  font-size: 12px;
  font-weight: 700;
}

.dashboard-import h2 {
  margin: 0;
  font-size: 38px;
  line-height: 1.08;
  letter-spacing: -0.04em;
}

.dashboard-import p {
  margin: 12px 0 22px;
  color: rgba(255, 255, 255, 0.82);
  line-height: 1.7;
  font-size: 14px;
}

.dashboard-import__button {
  height: 46px;
  padding: 0 20px;
  border: none;
  border-radius: 15px;
  background: #fff;
  color: #0d55a0;
  font-weight: 800;
  cursor: pointer;
}

.dashboard-import__shape {
  position: relative;
  width: 250px;
  min-width: 250px;
  display: grid;
  place-items: center;
}

.dashboard-import__shape-ring,
.dashboard-import__shape-core {
  position: absolute;
  border-radius: 999px;
  border: 14px solid rgba(255, 255, 255, 0.92);
}

.dashboard-import__shape-ring {
  width: 168px;
  height: 168px;
  right: 22px;
  bottom: 10px;
}

.dashboard-import__shape-core {
  width: 96px;
  height: 96px;
  right: 0;
  bottom: 2px;
}

.dashboard-dialog :deep(.el-dialog) {
  border-radius: 24px;
}

.dashboard-dialog__select {
  width: 100%;
}

@media (max-width: 1380px) {
  .dashboard-layout {
    height: calc(100vh - 20px);
    grid-template-columns: 78px minmax(0, 1fr);
  }

  .dashboard-splitter {
    display: none;
  }
}

@media (max-width: 980px) {
  .dashboard-layout {
    height: auto;
    grid-template-columns: 1fr;
  }

  .dashboard-sidebar {
    height: auto;
    grid-template-columns: auto 1fr auto;
    grid-template-rows: none;
    align-items: center;
  }

  .dashboard-sidebar__nav {
    grid-auto-flow: column;
    justify-content: center;
  }

  .dashboard-main {
    grid-column: auto;
  }

  .dashboard-main__scaled {
    width: 100%;
    height: 100%;
    transform: none;
  }

  .dashboard-content {
    overflow-y: auto;
  }

  .dashboard-quadrants {
    grid-template-columns: 1fr;
  }

  .dashboard-import {
    flex-direction: column;
  }
}

@media (max-width: 720px) {
  .dashboard-page {
    height: auto;
    padding: 8px;
    overflow: visible;
  }

  .dashboard-topbar {
    flex-direction: column;
    align-items: flex-start;
  }

  .dashboard-topbar__actions,
  .dashboard-topbar__brandline {
    width: 100%;
    justify-content: space-between;
  }
}
</style>
