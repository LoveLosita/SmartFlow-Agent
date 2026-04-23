<script setup lang="ts">
import { ref, reactive } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'

// --- 模拟数据 ---
const tasks = ref([
  { id: 1, title: '完成系统架构重构方案', is_completed: false, ddl: '2024-04-25T10:00:00', priority: 1 },
  { id: 2, title: '准备技术分享 PPT', is_completed: true, ddl: '2024-04-24T14:00:00', priority: 2 },
  { id: 3, title: '代码 Review：用户模块', is_completed: false, ddl: '2024-04-23T18:00:00', priority: 3 },
])

const quadrantMap = {
  1: { label: '重要且紧急', color: '#ef4444' },
  2: { label: '重要不紧急', color: '#3b82f6' },
  3: { label: '简单不重要', color: '#f59e0b' },
  4: { label: '不简单不重要', color: '#64748b' },
}

// --- 交互状态 ---
const editDialogVisible = ref(false)
const currentEditingTask = reactive({ 
  id: 0, 
  title: '', 
  ddl: '', 
  priority: 1 
})

// 1. 切换状态 (独立响应区)
const toggleTask = (task: any) => {
  task.is_completed = !task.is_completed
  ElMessage.success({
    message: task.is_completed ? '标记为已完成' : '已恢复为待办',
    customClass: 'premium-msg'
  })
}

// 2. 编辑任务 (正中主响应区)
const openEdit = (task: any) => {
  currentEditingTask.id = task.id
  currentEditingTask.title = task.title
  currentEditingTask.ddl = task.ddl
  currentEditingTask.priority = task.priority
  editDialogVisible.value = true
}

// 3. 删除任务 (悬浮侧响应区)
const deleteTask = (task: any) => {
  ElMessageBox.confirm('确定要删除这个任务吗？此操作不可撤销。', '确认删除', {
    confirmButtonText: '确定删除',
    cancelButtonText: '取消',
    confirmButtonClass: 'flat-btn-danger',
    cancelButtonClass: 'flat-btn-ghost',
    center: true,
  }).then(() => {
    tasks.value = tasks.value.filter(t => t.id !== task.id)
    ElMessage.success('任务已安全移除')
  })
}

const saveEdit = () => {
  const task = tasks.value.find(t => t.id === currentEditingTask.id)
  if (task) {
    task.title = currentEditingTask.title
    task.ddl = currentEditingTask.ddl
    task.priority = currentEditingTask.priority
    editDialogVisible.value = false
    ElMessage.success('任务已更新')
  }
}

const formatSimpleDate = (dateStr: string) => {
  if (!dateStr) return '无截止时间'
  const d = new Date(dateStr)
  return `${d.getMonth() + 1}-${d.getDate()} ${d.getHours().toString().padStart(2, '0')}:${d.getMinutes().toString().padStart(2, '0')}`
}
</script>

<template>
  <div class="demo-page">
    <div class="demo-card">
      <header class="card-header">
        <div class="card-title">
          <p class="subtitle">UPGRADED INTERACTION</p>
          <h2>全参数扁平化示例</h2>
        </div>
        <span class="count-badge">{{ tasks.length }} 项</span>
      </header>

      <div class="task-list">
        <TransitionGroup name="task-anime">
          <div 
            v-for="task in tasks" 
            :key="task.id" 
            class="task-item"
            :class="{ 'is-completed': task.is_completed }"
          >
            <!-- 区域1: 独立勾选框 -->
            <div class="check-box-wrapper" @click.stop="toggleTask(task)">
              <div class="check-box-inner">
                <svg v-if="task.is_completed" viewBox="0 0 24 24" width="14" height="14" fill="none" stroke="currentColor" stroke-width="4">
                  <polyline points="20 6 9 17 4 12"></polyline>
                </svg>
              </div>
            </div>

            <!-- 区域2: 文本主体内容 (点击触发编辑) -->
            <div class="task-body" @click="openEdit(task)">
              <div class="task-text-row">
                <span class="task-text">{{ task.title }}</span>
                <span class="priority-tag" :style="{ color: quadrantMap[task.priority as keyof typeof quadrantMap].color }">
                  {{ quadrantMap[task.priority as keyof typeof quadrantMap].label }}
                </span>
              </div>
              <div class="task-meta">
                <svg viewBox="0 0 24 24" width="12" height="12" fill="none" stroke="currentColor" stroke-width="2"><circle cx="12" cy="12" r="10"></circle><polyline points="12 6 12 12 16 14"></polyline></svg>
                {{ formatSimpleDate(task.ddl) }}
              </div>
            </div>

            <!-- 区域3: 悬浮操作菜单 -->
            <div class="hover-actions-panel">
               <button class="action-btn-mini delete-btn" @click.stop="deleteTask(task)">
                  <svg viewBox="0 0 24 24" width="16" height="16" fill="none" stroke="currentColor" stroke-width="2.5"><path d="M3 6h18M19 6v14a2 2 0 01-2 2H7a2 2 0 01-2-2V6m3 0V4a2 2 0 012-2h4a2 2 0 012 2v2"></path></svg>
               </button>
            </div>
          </div>
        </TransitionGroup>
      </div>

      <div class="footer-tip">快捷操作：点击任务主体进行全参数编辑</div>
    </div>

    <!-- 深度扁平化编辑弹窗 -->
    <el-dialog 
      v-model="editDialogVisible" 
      title="编辑详细参数" 
      width="440px" 
      align-center 
      class="flat-dialog"
      :show-close="false"
    >
      <div class="flat-form">
        <div class="form-item">
          <label>任务标题</label>
          <input v-model="currentEditingTask.title" class="flat-input" placeholder="输入任务标题..." />
        </div>

        <div class="form-row">
          <div class="form-item half">
            <label>优先级象限</label>
            <el-select v-model="currentEditingTask.priority" popper-class="flat-select-popper" class="flat-select">
              <el-option v-for="(v, k) in quadrantMap" :key="k" :label="v.label" :value="Number(k)" />
            </el-select>
          </div>
          <div class="form-item half">
            <label>截止日期</label>
            <el-date-picker
              v-model="currentEditingTask.ddl"
              type="datetime"
              placeholder="选择时间"
              format="YYYY-MM-DD HH:mm"
              value-format="YYYY-MM-DD[T]HH:mm:ss"
              class="flat-picker"
              popper-class="flat-picker-popper"
            />
          </div>
        </div>
      </div>

      <template #footer>
        <div class="flat-footer">
          <button class="flat-btn ghost" @click="editDialogVisible = false">放弃修改</button>
          <button class="flat-btn primary" @click="saveEdit">保存并同步</button>
        </div>
      </template>
    </el-dialog>
  </div>
</template>

<style scoped>
/* 基础布局 */
.demo-page {
  min-height: 100vh;
  background: #fdfdfd;
  display: flex;
  justify-content: center;
  align-items: center;
  padding: 20px;
  font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, sans-serif;
}

.demo-card {
  width: 100%;
  max-width: 460px;
  background: #ffffff;
  border-radius: 20px;
  border: 1px solid #eeeeee;
  box-shadow: 0 4px 24px rgba(0,0,0,0.03);
  padding: 32px;
}

.card-header {
  margin-bottom: 24px;
}

.subtitle {
  font-size: 10px;
  font-weight: 900;
  color: #94a3b8;
  letter-spacing: 0.2em;
  margin: 0 0 4px;
}

.card-title h2 {
  font-size: 22px;
  font-weight: 800;
  color: #0f172a;
  margin: 0;
}

.count-badge {
  font-size: 12px;
  color: #64748b;
  font-weight: 600;
  margin-top: 4px;
  display: inline-block;
}

/* 列表样式 */
.task-list {
  display: flex;
  flex-direction: column;
  gap: 10px;
}

.task-item {
  position: relative;
  background: #ffffff;
  border: 1px solid #f1f5f9;
  border-radius: 12px;
  padding: 14px 16px;
  display: flex;
  align-items: center;
  transition: all 0.2s ease;
  cursor: pointer;
}

.task-item:hover {
  border-color: #e2e8f0;
  background: #f8fafc;
}

.check-box-inner {
  width: 22px;
  height: 22px;
  border-radius: 6px;
  border: 2px solid #e2e8f0;
  display: flex;
  align-items: center;
  justify-content: center;
  transition: all 0.2s;
  background: #fff;
  margin-right: 14px;
}

.is-completed .check-box-inner {
  background: #10b981;
  border-color: #10b981;
  color: #fff;
}

.task-body { flex: 1; min-width: 0; }
.task-text-row { display: flex; align-items: center; gap: 8px; margin-bottom: 2px; }
.task-text { font-size: 15px; font-weight: 600; color: #1e293b; }
.is-completed .task-text { color: #94a3b8; text-decoration: line-through; }

.priority-tag { font-size: 11px; font-weight: 700; background: rgba(0,0,0,0.03); padding: 2px 6px; border-radius: 4px; }
.task-meta { font-size: 12px; color: #94a3b8; display: flex; align-items: center; gap: 4px; }

.hover-actions-panel {
  position: absolute;
  right: 12px;
  opacity: 0;
  transition: all 0.2s;
}

.task-item:hover .hover-actions-panel { opacity: 1; }
.action-btn-mini { border: none; background: transparent; color: #ef4444; cursor: pointer; padding: 4px; border-radius: 6px; }
.action-btn-mini:hover { background: #fee2e2; }

/* 深度扁平化弹窗 - 关键美化部分 */
:global(.flat-dialog) {
  border-radius: 16px !important;
  border: 1px solid #f1f5f9 !important;
  box-shadow: 0 20px 40px rgba(0,0,0,0.1) !important;
}

:global(.flat-dialog .el-dialog__header) {
  padding: 24px 28px 10px !important;
  text-align: left;
}

:global(.flat-dialog .el-dialog__title) {
  font-size: 16px !important;
  font-weight: 800 !important;
  color: #0f172a;
}

:global(.flat-dialog .el-dialog__body) {
  padding: 10px 28px 24px !important;
}

.flat-form { display: flex; flex-direction: column; gap: 20px; }
.form-row { display: flex; gap: 16px; }
.form-item { display: flex; flex-direction: column; gap: 8px; }
.form-item.half { flex: 1; }
.form-item label { font-size: 12px; font-weight: 800; color: #64748b; text-transform: uppercase; letter-spacing: 0.05em; }

/* 纯扁平输入框 */
.flat-input {
  height: 42px;
  border: 1px solid #e2e8f0;
  border-radius: 10px;
  padding: 0 14px;
  background: #f8fafc;
  outline: none;
  font-size: 14px;
  transition: all 0.2s;
  font-weight: 600;
}
.flat-input:focus { border-color: #3b82f6; background: #fff; }

/* Element Plus 深度覆盖 */
:global(.flat-select .el-select__wrapper),
:global(.flat-picker.el-input__wrapper) {
  background-color: #f8fafc !important;
  box-shadow: none !important;
  border: 1px solid #e2e8f0 !important;
  border-radius: 10px !important;
  height: 42px !important;
}

.flat-footer { display: flex; justify-content: flex-end; gap: 10px; border-top: 1px solid #f1f5f9; padding-top: 20px; }
.flat-btn { height: 42px; padding: 0 20px; border-radius: 10px; font-weight: 700; font-size: 13px; cursor: pointer; border: none; transition: all 0.2s; }
.flat-btn.primary { background: #0f172a; color: #fff; }
.flat-btn.primary:hover { background: #334155; }
.flat-btn.ghost { background: transparent; color: #64748b; }
.flat-btn.ghost:hover { background: #f1f5f9; }

.footer-tip { margin-top: 24px; font-size: 12px; color: #94a3b8; text-align: center; }

/* 动画 */
.task-anime-enter-active { transition: all 0.3s ease; }
.task-anime-enter-from { opacity: 0; transform: scale(0.95); }
.task-anime-leave-to { opacity: 0; transform: scale(1.05); }
</style>
