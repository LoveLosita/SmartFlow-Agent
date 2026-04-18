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

const pageLoading = ref(true)
const taskLoading = ref(true)
const scheduleLoading = ref(true)
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

const taskForm = reactive<{
  title: string
  priority_group: number
  deadline_at: Date | null
}>({
  title: '',
  priority_group: 2,
  deadline_at: null,
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

const groupedTasks = computed(() => {
  const groups: Record<number, TaskItem[]> = { 1: [], 2: [], 3: [], 4: [] }
  for (const task of tasks.value) {
    if (groups[task.priority_group]) groups[task.priority_group].push(task)
  }
  for (const key of Object.keys(groups)) {
    groups[Number(key)].sort((left, right) => {
      if (left.is_completed !== right.is_completed) return left.is_completed ? 1 : -1
      return left.id - right.id
    })
  }
  return groups
})

async function loadTasksData() {
  taskLoading.value = true
  try { tasks.value = await getTasks() }
  catch (error) { ElMessage.warning(error instanceof Error ? error.message : '任务加载失败') }
  finally { taskLoading.value = false }
}

async function loadScheduleData() {
  scheduleLoading.value = true
  try {
    const schedules = await getTodaySchedule()
    todayEvents.value = schedules.flatMap((item) => item.events).sort((left, right) => left.order - right.order)
  } catch (error) { ElMessage.warning(error instanceof Error ? error.message : '今日日程加载失败') }
  finally { scheduleLoading.value = false }
}

async function loadDashboardData() {
  pageLoading.value = true
  
  // 锁死最少加载时间，确保骨架屏平稳滑入定型后，再进行内外数据的交叉溶解
  const minLoadingTimer = new Promise((resolve) => setTimeout(resolve, 800))
  
  await Promise.allSettled([loadTasksData(), loadScheduleData(), minLoadingTimer])
  
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
  } catch (error) { ElMessage.error(error instanceof Error ? error.message : '任务更新失败') }
}

function openCreateTaskDialog() {
  taskForm.title = ''
  taskForm.priority_group = 2
  taskForm.deadline_at = null
  createTaskDialogVisible.value = true
}

async function handleCreateTask() {
  if (!taskForm.title.trim()) { ElMessage.warning('请先填写任务标题'); return }
  createTaskLoading.value = true
  try {
    const created = await createTask({
      title: taskForm.title.trim(),
      priority_group: taskForm.priority_group,
      deadline_at: taskForm.deadline_at ? taskForm.deadline_at.toISOString() : null,
    })
    tasks.value.unshift({ id: created.id, user_id: 0, title: created.title, priority_group: created.priority_group, status: created.status, deadline: created.deadline_at ?? '', is_completed: false })
    createTaskDialogVisible.value = false
    ElMessage.success('任务已添加')
  } catch (error) { ElMessage.error(error instanceof Error ? error.message : '创建任务失败') }
  finally { createTaskLoading.value = false }
}

async function handleLogout() {
  logoutLoading.value = true
  try { await authStore.logout(); ElMessage.success('已安全退出登录') }
  catch (error) { ElMessage.warning(error instanceof Error ? `${error.message}，本地登录态已清除` : '退出接口异常，本地登录态已清除') }
  finally { logoutLoading.value = false; await router.push('/auth') }
}

function handleCourseImportEntry() { ElMessage.info('课表导入入口已预留') }

function syncDashboardMainScale() {
  const main = dashboardMainRef.value
  const inner = dashboardMainInnerRef.value
  const topbar = dashboardTopbarRef.value
  const content = dashboardContentRef.value
  if (!main || !inner || !topbar || !content || window.innerWidth <= 980) { dashboardMainScale.value = 1; return }
  dashboardMainScale.value = 1
  window.requestAnimationFrame(() => {
    const availableHeight = main.clientHeight
    const gridGap = 10
    const naturalHeight = topbar.getBoundingClientRect().height + content.scrollHeight + gridGap
    if (!availableHeight || !naturalHeight) { dashboardMainScale.value = 1; return }
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
  window.removeEventListener('resize', syncDashboardMainScale)
})

watch([() => tasks.value.length, () => todayEvents.value.length, pageLoading], async () => {
  await nextTick()
  syncDashboardMainScale()
}, { flush: 'post' })
</script>

<template>
  <section ref="dashboardMainRef" class="dashboard-main">
    <div ref="dashboardMainInnerRef" class="dashboard-main__scaled" :style="{ '--dashboard-main-scale': dashboardMainScale }">
          <header ref="dashboardTopbarRef" class="dashboard-topbar glass-panel dashboard-item-pop" :style="{ '--anim-delay': '0s' }">
            <div>
              <div class="dashboard-topbar__brandline">
                <strong>AI 智慧日程系统</strong>
                <span>{{ pageTitleDate }}</span>
              </div>
            </div>

            <div class="dashboard-topbar__actions">
              <button type="button" class="dashboard-topbar__logout" :disabled="logoutLoading" @click="handleLogout">{{ logoutLoading ? '退出中...' : '登出' }}</button>
              <div class="dashboard-topbar__profile">
                <strong>{{ greetingName }}</strong>
                <span>{{ greetingName.slice(0, 1).toUpperCase() }}</span>
              </div>
            </div>
          </header>

          <div ref="dashboardContentRef" class="dashboard-content page-shell">
            <TodayTimeline class="dashboard-item-pop" :style="{ '--anim-delay': '0.04s' }" :events="todayEvents" :loading="scheduleLoading || pageLoading" />

            <div class="dashboard-actions dashboard-item-pop" :style="{ '--anim-delay': '0.08s' }">
              <button type="button" class="dashboard-actions__primary" @click="openCreateTaskDialog">添加任务</button>
            </div>

            <section class="dashboard-quadrants">
              <TaskQuadrantCard
                v-for="(group, index) in quadrantOrder"
                :key="group"
                class="dashboard-item-pop"
                :style="{ '--anim-delay': (0.12 + index * 0.04) + 's' }"
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

            <section class="dashboard-import glass-panel dashboard-item-pop" :style="{ '--anim-delay': '0.28s' }">
              <div class="dashboard-import__content">
                <p class="dashboard-import__eyebrow">课程导入</p>
                <h2>导入课表</h2>
                <p>导入课表后，可以在安排日程时避开上课时间。</p>
                <button type="button" class="dashboard-import__button" @click="handleCourseImportEntry">开始导入</button>
              </div>
              <div class="dashboard-import__shape">
                <span class="dashboard-import__shape-ring" />
                <span class="dashboard-import__shape-core" />
              </div>
            </section>
          </div>
        </div>
      </section>

    <el-dialog v-model="createTaskDialogVisible" title="添加任务" width="460px" align-center class="dashboard-dialog">
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
          <el-date-picker v-model="taskForm.deadline_at" type="datetime" placeholder="可选" class="dashboard-dialog__select" />
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="createTaskDialogVisible = false">取消</el-button>
        <el-button type="primary" :loading="createTaskLoading" @click="handleCreateTask">保存任务</el-button>
      </template>
    </el-dialog>
</template>

<style scoped>
::-webkit-scrollbar { width: 5px; height: 5px; }
::-webkit-scrollbar-track { background: transparent; }
::-webkit-scrollbar-thumb { background: rgba(15, 23, 42, 0.08); border-radius: 10px; }
::-webkit-scrollbar-thumb:hover { background: rgba(15, 23, 42, 0.15); }

@keyframes dashboard-item-spring {
  0% { opacity: 0; transform: scale(0.9) translateY(20px); }
  60% { opacity: 1; transform: scale(1.02) translateY(-2px); }
  100% { opacity: 1; transform: scale(1) translateY(0); }
}

.dashboard-item-pop {
  animation: dashboard-item-spring 0.55s cubic-bezier(0.34, 1.56, 0.64, 1) both;
  animation-delay: var(--anim-delay, 0s);
  transform-origin: center center;
}

.dashboard-main { min-width: 0; min-height: 0; overflow: hidden; height: 100%; }

.dashboard-main__scaled {
  --dashboard-main-scale: 1;
  width: calc(100% / var(--dashboard-main-scale));
  height: calc(100% / var(--dashboard-main-scale));
  display: grid;
  grid-template-rows: auto auto;
  gap: 10px;
  transform: scale(var(--dashboard-main-scale));
  transform-origin: top left;
}

.dashboard-topbar {
  border-radius: 20px;
  padding: 16px 24px;
  display: flex;
  justify-content: space-between;
  align-items: center;
  background: #ffffff;
  border: 1px solid rgba(15, 23, 42, 0.05);
  box-shadow: 0 4px 15px rgba(15, 23, 42, 0.03);
}

.dashboard-topbar__brandline { display: flex; align-items: center; gap: 14px; }
.dashboard-topbar__brandline strong { font-size: 18px; color: #14233a; }
.dashboard-topbar__brandline span { color: #677588; font-size: 13px; }

.dashboard-topbar__actions { display: flex; align-items: center; gap: 14px; }
.dashboard-topbar__logout { min-width: 88px; height: 38px; border-radius: 13px; border: 1px solid rgba(28, 98, 205, 0.22); background: #f9fbff; color: #1d63cf; cursor: pointer; }
.dashboard-topbar__profile { display: flex; align-items: center; gap: 10px; }
.dashboard-topbar__profile strong { font-size: 13px; }
.dashboard-topbar__profile span { width: 38px; height: 38px; border-radius: 999px; background: #eef3fb; color: #314156; display: inline-flex; align-items: center; justify-content: center; font-weight: 800; }

.dashboard-content { width: 100%; display: grid; gap: 14px; align-content: start; }
.dashboard-actions { display: flex; justify-content: flex-end; }
.dashboard-actions__primary { height: 42px; padding: 0 20px; border: none; border-radius: 15px; background: #3b82f6; color: #fff; font-weight: 700; cursor: pointer; box-shadow: 0 4px 12px rgba(59, 130, 246, 0.2); }

.dashboard-quadrants { display: grid; grid-template-columns: repeat(auto-fit, minmax(280px, 1fr)); gap: 14px; }

.dashboard-import {
  border-radius: 24px;
  padding: 32px;
  min-height: 220px;
  background: #ffffff;
  border: 1px solid rgba(15, 23, 42, 0.05);
  box-shadow: 0 4px 15px rgba(15, 23, 42, 0.02);
  display: flex;
  justify-content: space-between;
  gap: 24px;
  position: relative;
  overflow: hidden;
}

.dashboard-import__content { position: relative; z-index: 1; max-width: 460px; }
.dashboard-import__eyebrow { margin: 0 0 10px; color: #3b82f6; text-transform: uppercase; font-size: 12px; font-weight: 700; }
.dashboard-import h2 { margin: 0; font-size: 32px; color: #0f172a; font-weight: 800; }
.dashboard-import p { margin: 14px 0 24px; color: #64748b; font-size: 14px; }
.dashboard-import__button { height: 44px; padding: 0 24px; border: none; border-radius: 12px; background: #3b82f6; color: #ffffff; font-weight: 700; cursor: pointer; }

.dashboard-import__shape { position: absolute; right: -50px; bottom: -50px; width: 220px; height: 220px; opacity: 0.1; pointer-events: none; }
.dashboard-import__shape-ring { position: absolute; inset: 0; border: 40px solid #3b82f6; border-radius: 50%; }
.dashboard-import__shape-core { position: absolute; inset: 80px; background: #3b82f6; border-radius: 50%; }
</style>
