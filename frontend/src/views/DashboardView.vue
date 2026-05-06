<script setup lang="ts">
import { computed, nextTick, onActivated, onBeforeUnmount, onMounted, reactive, ref, watch } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { useRouter } from 'vue-router'

import TaskQuadrantCard from '@/components/dashboard/TaskQuadrantCard.vue'
import TodayTimeline from '@/components/dashboard/TodayTimeline.vue'
import { completeTask, createTask, getTasks, undoCompleteTask, updateTask, deleteTask } from '@/api/task'
import { getTodaySchedule } from '@/api/schedule'
import { useAuthStore } from '@/stores/auth'
import type { TaskItem, TodayEvent } from '@/types/dashboard'
import { formatHeaderDate } from '@/utils/date'

defineOptions({
  name: 'DashboardView',
})

const router = useRouter()
const authStore = useAuthStore()

const taskLoading = ref(true)
const scheduleLoading = ref(true)
const saveTaskLoading = ref(false)
const logoutLoading = ref(false)
const taskDialogVisible = ref(false)
const isEditMode = ref(false)
const editingTaskId = ref<number | null>(null)

const dashboardMainRef = ref<HTMLElement | null>(null)
const dashboardMainInnerRef = ref<HTMLElement | null>(null)
const dashboardTopbarRef = ref<HTMLElement | null>(null)
const dashboardContentRef = ref<HTMLElement | null>(null)
const dashboardMainScale = ref(1)

const tasks = ref<TaskItem[]>([])
const todayEvents = ref<TodayEvent[]>([])

let dashboardScaleAnimationFrame = 0

const taskForm = reactive<{
  title: string
  priority_group: number
  deadline_at: Date | null
  urgency_threshold_at: Date | null
}>({
  title: '',
  priority_group: 2,
  deadline_at: null,
  urgency_threshold_at: null,
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
    emptyText: '暂无关键紧急任务',
  },
  2: {
    title: '重要不紧急',
    caption: '持续推进',
    tone: 'primary',
    emptyText: '这里放长期核心任务',
  },
  3: {
    title: '简单不重要',
    caption: '顺手完成',
    tone: 'warning',
    emptyText: '暂无琐碎低价值任务',
  },
  4: {
    title: '不简单不重要',
    caption: '谨慎投入',
    tone: 'slate',
    emptyText: '这里放辅助或暂缓任务',
  },
}

const pageTitleDate = computed(() => formatHeaderDate(new Date()))
const greetingName = computed(() => authStore.lastUsername || 'SmartMate 用户')

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
  await Promise.allSettled([loadTasksData(), loadScheduleData()])
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
  isEditMode.value = false
  editingTaskId.value = null
  taskForm.title = ''
  taskForm.priority_group = 2
  taskForm.deadline_at = null
  taskForm.urgency_threshold_at = null
  taskDialogVisible.value = true
}

function handleTaskEdit(task: TaskItem) {
  isEditMode.value = true
  editingTaskId.value = task.id
  taskForm.title = task.title
  taskForm.priority_group = task.priority_group
  taskForm.deadline_at = task.deadline ? new Date(task.deadline) : null
  taskForm.urgency_threshold_at = task.urgency_threshold_at ? new Date(task.urgency_threshold_at) : null
  taskDialogVisible.value = true
}

async function handleTaskDelete(task: TaskItem) {
  try {
    await ElMessageBox.confirm('确定要删除该任务吗？操作不可撤销。', '确认删除', {
      confirmButtonText: '确定删除',
      cancelButtonText: '取消',
      type: 'warning',
      roundButton: true
    })
    
    await deleteTask(task.id)
    tasks.value = tasks.value.filter(t => t.id !== task.id)
    ElMessage.success('任务已成功删除')
  } catch (error) {
    if (error === 'cancel') return
    ElMessage.error(error instanceof Error ? error.message : '任务删除失败')
  }
}

async function handleSaveTask() {
  if (!taskForm.title.trim()) { ElMessage.warning('请先填写任务标题'); return }
  saveTaskLoading.value = true
  try {
    if (isEditMode.value && editingTaskId.value) {
      // 执行更新交互
      const updated = await updateTask({
        task_id: editingTaskId.value,
        title: taskForm.title.trim(),
        priority_group: taskForm.priority_group,
        deadline_at: taskForm.deadline_at ? taskForm.deadline_at.toISOString() : null,
        urgency_threshold_at: taskForm.urgency_threshold_at ? taskForm.urgency_threshold_at.toISOString() : null,
      })
      const idx = tasks.value.findIndex(t => t.id === updated.id)
      if (idx !== -1) tasks.value[idx] = updated
      ElMessage.success('任务已更新')
    } else {
      // 执行创建交互
      const created = await createTask({
        title: taskForm.title.trim(),
        priority_group: taskForm.priority_group,
        deadline_at: taskForm.deadline_at ? taskForm.deadline_at.toISOString() : null,
        urgency_threshold_at: taskForm.urgency_threshold_at ? taskForm.urgency_threshold_at.toISOString() : null,
      })
      tasks.value.unshift({ 
        id: created.id, 
        user_id: 0, 
        title: created.title, 
        priority_group: created.priority_group, 
        status: created.status, 
        deadline: created.deadline_at ?? '', 
        is_completed: false,
        urgency_threshold_at: created.urgency_threshold_at
      })
      ElMessage.success('任务已添加')
    }
    taskDialogVisible.value = false
  } catch (error) { ElMessage.error(error instanceof Error ? error.message : '保存任务失败') }
  finally { saveTaskLoading.value = false }
}

async function handleLogout() {
  logoutLoading.value = true
  try { await authStore.logout(); ElMessage.success('已安全退出登录') }
  catch (error) { ElMessage.warning(error instanceof Error ? `${error.message}，本地登录态已清除` : '退出接口异常，本地登录态已清除') }
  finally { logoutLoading.value = false; await router.push('/auth') }
}

function handleCourseImportEntry() {
  void router.push('/schedule')
}

function syncDashboardMainScale() {
  const main = dashboardMainRef.value
  const inner = dashboardMainInnerRef.value
  const topbar = dashboardTopbarRef.value
  const content = dashboardContentRef.value
  if (!main || !inner || !topbar || !content || window.innerWidth <= 980) { dashboardMainScale.value = 1; return }

  const availableHeight = main.clientHeight
  const gridGap = 10
  const naturalHeight = topbar.offsetHeight + content.scrollHeight + gridGap
  if (!availableHeight || !naturalHeight) return

  const nextScale = Number(Math.min(1.1, (availableHeight / naturalHeight) * 1.05).toFixed(4))
  dashboardMainScale.value = nextScale
}

function scheduleDashboardMainScaleSync() {
  if (typeof window === 'undefined') return
  if (dashboardScaleAnimationFrame) window.cancelAnimationFrame(dashboardScaleAnimationFrame)

  // 1. 侧栏切回首页时，外层布局会比首页内容晚一点稳定。
  // 2. 延后两帧再测量，只处理“回首页首帧偏大”的问题，避免持续重算。
  dashboardScaleAnimationFrame = window.requestAnimationFrame(() => {
    dashboardScaleAnimationFrame = window.requestAnimationFrame(() => {
      dashboardScaleAnimationFrame = 0
      syncDashboardMainScale()
    })
  })
}

onMounted(async () => {
  await nextTick()
  scheduleDashboardMainScaleSync()
  await loadDashboardData()
  await nextTick()
  scheduleDashboardMainScaleSync()
  window.addEventListener('resize', scheduleDashboardMainScaleSync)
})

onActivated(async () => {
  await nextTick()
  scheduleDashboardMainScaleSync()
})

onBeforeUnmount(() => {
  if (dashboardScaleAnimationFrame) window.cancelAnimationFrame(dashboardScaleAnimationFrame)
  window.removeEventListener('resize', scheduleDashboardMainScaleSync)
})

watch([() => tasks.value.length, () => todayEvents.value.length, taskLoading, scheduleLoading], async () => {
  await nextTick()
  scheduleDashboardMainScaleSync()
}, { flush: 'post' })
</script>

<template>
  <section ref="dashboardMainRef" class="dashboard-main">
    <div ref="dashboardMainInnerRef" class="dashboard-main__scaled" :style="{ '--dashboard-main-scale': dashboardMainScale }">
          <header ref="dashboardTopbarRef" class="dashboard-topbar glass-panel dashboard-item-pop" :style="{ '--anim-delay': '0s' }">
            <div>
              <div class="dashboard-topbar__brandline">
                <strong>SmartMate</strong>
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
            <TodayTimeline :style="{ '--anim-delay': '0.04s' }" :events="todayEvents" :loading="scheduleLoading" />

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
                :loading="taskLoading"
                @toggle="handleTaskToggle"
                @edit="handleTaskEdit"
                @delete="handleTaskDelete"
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

    <el-dialog
      v-model="taskDialogVisible"
      :title="isEditMode ? '编辑任务详情' : '添加新任务'"
      width="440px"
      align-center
      class="dashboard-dialog premium-dialog"
    >
      <el-form label-position="top">
        <el-form-item label="任务标题">
          <el-input v-model="taskForm.title" maxlength="255" placeholder="例如：完成数据库复习" />
        </el-form-item>
        <el-form-item label="优先级象限">
          <el-select v-model="taskForm.priority_group" class="dashboard-dialog__select" popper-class="premium-select-popper" placement="bottom-start">
            <el-option :value="1" label="1 - 重要且紧急" />
            <el-option :value="2" label="2 - 重要不紧急" />
            <el-option :value="3" label="3 - 简单不重要" />
            <el-option :value="4" label="4 - 不简单不重要" />
          </el-select>
        </el-form-item>
        <div class="dialog-double-row">
          <el-form-item label="截止时间" class="half">
            <el-date-picker
              v-model="taskForm.deadline_at"
              type="datetime"
              placeholder="截止时间"
              class="dashboard-dialog__select"
              popper-class="premium-select-popper"
            />
          </el-form-item>
          <el-form-item label="紧急阈值" class="half">
            <el-date-picker
              v-model="taskForm.urgency_threshold_at"
              type="datetime"
              placeholder="进入紧急的时间点"
              class="dashboard-dialog__select"
              popper-class="premium-select-popper"
            />
          </el-form-item>
        </div>
      </el-form>
      <template #footer>
        <div class="premium-dialog__footer">
          <button class="premium-btn premium-btn--ghost" @click="taskDialogVisible = false">取消</button>
          <button class="premium-btn premium-btn--primary" :disabled="saveTaskLoading" @click="handleSaveTask">
            {{ saveTaskLoading ? '保存中...' : (isEditMode ? '保存修改' : '确认添加') }}
          </button>
        </div>
      </template>
    </el-dialog>
</template>

<style scoped>
::-webkit-scrollbar { width: 5px; height: 5px; }
::-webkit-scrollbar-track { background: transparent; }
::-webkit-scrollbar-thumb { background: rgba(15, 23, 42, 0.08); border-radius: 10px; }
::-webkit-scrollbar-thumb:hover { background: rgba(15, 23, 42, 0.15); }

@keyframes dashboard-item-fade-in {
  0% { opacity: 0; transform: translateY(10px); }
  100% { opacity: 1; transform: translateY(0); }
}

.dashboard-item-pop {
  animation: dashboard-item-fade-in 0.4s cubic-bezier(0.16, 1, 0.3, 1) both;
  animation-delay: var(--anim-delay, 0s);
  --anim-delay: 0s;
  transform-origin: center center;
}

.dashboard-main { min-width: 0; min-height: 0; overflow-x: hidden; overflow-y: auto; height: 100%; }

.dashboard-main__scaled {
  --dashboard-main-scale: 1;
  width: calc(100% / var(--dashboard-main-scale));
  height: calc(100% / var(--dashboard-main-scale));
  display: grid;
  grid-template-rows: auto 1fr;
  align-content: start;
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
  flex-shrink: 0;
  max-height: 72px; /* 锁定高度，防止在布局缩放时发生形变 */
}

.dashboard-topbar__brandline { display: flex; align-items: center; gap: 14px; }
.dashboard-topbar__brandline strong { font-size: 18px; color: #14233a; }
.dashboard-topbar__brandline span { color: #677588; font-size: 13px; }

.dashboard-topbar__actions { display: flex; align-items: center; gap: 14px; }
.dashboard-topbar__logout { min-width: 88px; height: 38px; border-radius: 13px; border: 1px solid rgba(28, 98, 205, 0.22); background: #f9fbff; color: #1d63cf; cursor: pointer; }
.dashboard-topbar__profile { display: flex; align-items: center; gap: 10px; }
.dashboard-topbar__profile strong { font-size: 13px; }
.dashboard-topbar__profile span { width: 38px; height: 38px; border-radius: 999px; background: #eef3fb; color: #314156; display: inline-flex; align-items: center; justify-content: center; font-weight: 800; }

.dashboard-content { width: 100%; display: grid; gap: 14px; align-content: start; padding-bottom: 60px; }
.dashboard-actions { display: flex; justify-content: flex-end; }
.dashboard-actions__primary { height: 42px; padding: 0 20px; border: none; border-radius: 15px; background: #3b82f6; color: #fff; font-weight: 700; cursor: pointer; box-shadow: 0 4px 12px rgba(59, 130, 246, 0.2); }

.dashboard-quadrants { display: grid; grid-template-columns: repeat(auto-fit, minmax(280px, 1fr)); gap: 14px; }

.dashboard-import {
  border-radius: 20px;
  padding: 24px 32px;
  min-height: 180px;
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
.dashboard-import h2 { margin: 0; font-size: 24px; color: #0f172a; font-weight: 800; }
.dashboard-import p { margin: 8px 0 16px; color: #64748b; font-size: 13px; line-height: 1.5; }
.dashboard-import__button { height: 44px; padding: 0 24px; border: none; border-radius: 12px; background: #3b82f6; color: #ffffff; font-weight: 700; cursor: pointer; }

.dashboard-import__shape { position: absolute; right: -50px; bottom: -50px; width: 220px; height: 220px; opacity: 0.1; pointer-events: none; }
.dashboard-import__shape-ring { position: absolute; inset: 0; border: 40px solid #3b82f6; border-radius: 50%; }
.dashboard-import__shape-core { position: absolute; inset: 80px; background: #3b82f6; border-radius: 50%; }

/* --- Premium Dialog Styles --- */
:global(.premium-dialog) {
  border-radius: 20px !important;
  background: #ffffff !important;
  border: 1px solid rgba(15, 23, 42, 0.08) !important;
  box-shadow: 0 25px 50px -12px rgba(0, 0, 0, 0.25) !important;
  overflow: hidden;
}

:global(.premium-dialog .el-dialog__header) {
  padding: 24px 28px 12px !important;
  margin-right: 0 !important;
  text-align: left;
}

:global(.premium-dialog .el-dialog__title) {
  font-size: 18px !important;
  font-weight: 800 !important;
  color: #0f172a !important;
  letter-spacing: -0.02em;
}

:global(.premium-dialog .el-dialog__body) {
  padding: 12px 28px 20px !important;
}

:global(.premium-dialog .el-dialog__footer) {
  padding: 0 !important;
}

.premium-dialog__footer {
  padding: 16px 28px 24px;
  background: #f8fafc;
  display: flex;
  justify-content: flex-end;
  gap: 12px;
  border-top: 1px solid #f1f5f9;
}

.dialog-double-row {
  display: flex;
  gap: 12px;
  width: 100%;
  overflow: hidden;
}
.dialog-double-row .half {
  flex: 1;
  min-width: 0; /* 关键：强制子项可收缩 */
}

.premium-btn {
  height: 40px;
  padding: 0 20px;
  border-radius: 12px;
  font-size: 14px;
  font-weight: 700;
  cursor: pointer;
  transition: all 0.2s cubic-bezier(0.4, 0, 0.2, 1);
  border: none;
  display: inline-flex;
  align-items: center;
  justify-content: center;
}

.premium-btn--primary {
  background: #3b82f6;
  color: #ffffff;
  box-shadow: 0 4px 12px rgba(59, 130, 246, 0.2);
}

.premium-btn--primary:hover:not(:disabled) {
  background: #2563eb;
  transform: translateY(-1px);
  box-shadow: 0 6px 16px rgba(59, 130, 246, 0.3);
}

.premium-btn--ghost {
  background: #ffffff;
  color: #64748b;
  border: 1px solid #e2e8f0;
}

.premium-btn--ghost:hover {
  background: #f1f5f9;
  color: #0f172a;
}

/* 弹出动画覆写 */
:global(.dialog-fade-enter-active .premium-dialog) {
  animation: premium-dialog-fade-in 0.35s cubic-bezier(0.16, 1, 0.3, 1) both;
}

@keyframes premium-dialog-fade-in {
  0% { opacity: 0; transform: scale(0.98) translateY(10px); }
  100% { opacity: 1; transform: scale(1) translateY(0); }
}

:global(.el-overlay) {
  backdrop-filter: blur(6px);
  background: rgba(15, 23, 42, 0.35) !important;
}

/* 表单美化 */
:global(.premium-dialog .el-form-item__label) {
  font-weight: 700 !important;
  color: #475569 !important;
  font-size: 13px !important;
  margin-bottom: 6px !important;
}

:global(.premium-dialog .el-input__wrapper),
:global(.premium-dialog .el-select__wrapper),
:global(.premium-dialog .el-date-editor.el-input__wrapper) {
  background-color: #f8fafc !important;
  box-shadow: 0 0 0 1px #e2e8f0 inset !important;
  border-radius: 10px !important;
  padding: 4px 12px !important;
  width: 100% !important;
}

:global(.premium-dialog .el-input__wrapper.is-focus),
:global(.premium-dialog .el-select__wrapper.is-focused) {
  box-shadow: 0 0 0 2px rgba(59, 130, 246, 0.2) inset !important;
  background-color: #ffffff !important;
}
:global(.premium-dialog .el-dialog__headerbtn) {
  top: 20px !important;
  right: 20px !important;
  width: 32px !important;
  height: 32px !important;
  border-radius: 50% !important;
  background: #f1f5f9 !important;
  transition: all 0.2s !important;
  display: flex !important;
  align-items: center !important;
  justify-content: center !important;
}

:global(.premium-dialog .el-dialog__headerbtn:hover) {
  background: #e2e8f0 !important;
  transform: rotate(90deg);
}

:global(.premium-dialog .el-dialog__headerbtn .el-dialog__close) {
  color: #64748b !important;
  font-size: 16px !important;
  font-weight: 800 !important;
}

/* 统一输入框高度与背景 */
:global(.premium-dialog .el-input__inner),
:global(.premium-dialog .el-select .el-input__inner) {
  height: 38px !important;
  color: #0f172a !important;
  font-weight: 600 !important;
}

/* --- 下拉菜单扁平化 --- */
:global(.premium-select-popper) {
  border-radius: 16px !important;
  border: 1px solid rgba(15, 23, 42, 0.08) !important;
  box-shadow: 0 12px 30px -5px rgba(0, 0, 0, 0.12) !important;
  background: #ffffff !important;
  overflow: hidden !important;
  margin-top: 8px !important;
}

:global(.premium-select-popper .el-select-dropdown__list) {
  padding: 6px !important;
}

:global(.premium-select-popper .el-select-dropdown__item) {
  border-radius: 10px !important;
  height: 38px !important;
  line-height: 38px !important;
  margin-bottom: 2px !important;
  font-weight: 600 !important;
  color: #475569 !important;
  padding: 0 12px !important;
}

:global(.premium-select-popper .el-select-dropdown__item.is-selected) {
  background: #eff6ff !important;
  color: #3b82f6 !important;
}

:global(.premium-select-popper .el-select-dropdown__item:hover) {
  background: #f1f5f9 !important;
  color: #0f172a !important;
}

:global(.premium-select-popper .el-popper__arrow) {
  display: none !important;
}

/* 时间选择器特定深度覆盖 */
:global(.premium-select-popper.el-picker-popper) {
  padding: 0 !important;
}

:global(.premium-select-popper .el-picker-panel) {
  background: transparent !important;
}
</style>
