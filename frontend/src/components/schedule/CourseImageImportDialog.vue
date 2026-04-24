<script setup lang="ts">
import { ref, computed } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { parseCourseImage, importCourses } from '@/api/scheduleCenter'
import type { CourseDraftRow, CourseImportPayload } from '@/types/schedule'

const props = defineProps<{
  modelValue: boolean
}>()

const emit = defineEmits<{
  (e: 'update:modelValue', value: boolean): void
  (e: 'success'): void
}>()

const visible = computed({
  get: () => props.modelValue,
  set: (val) => emit('update:modelValue', val)
})

// 状态管理
const step = ref<'upload' | 'edit'>('upload')
const imageFile = ref<File | null>(null)
const imagePreview = ref('')
const parseLoading = ref(false)
const importLoading = ref(false)
const abortController = ref<AbortController | null>(null)

const draftStatus = ref<'success' | 'partial' | 'reject' | null>(null)
const draftMessage = ref('')
const warnings = ref<string[]>([])
const rows = ref<CourseDraftRow[]>([])

// 新增课程相关的状态
const addDialogVisible = ref(false)
const newRowTemplate = (): CourseDraftRow => ({
  row_id: 'manual_' + Date.now(),
  course_name: '',
  location: '',
  start_week: 1,
  end_week: 16,
  day_of_week: 1,
  start_section: 1,
  end_section: 2,
  week_type: 'all',
  is_allow_tasks: false,
  confidence: 1,
  raw_text: '手动添加',
  row_warnings: []
})
const newRowData = ref<CourseDraftRow>(newRowTemplate())

// 拖拽相关状态
const draggingIndex = ref<number | null>(null)

// 模拟示例图 URL
const exampleImageUrl = 'https://dl2.lecspace.com/SmartFlow-Agent/%E8%AF%BE%E8%A1%A8%E4%B8%8A%E4%BC%A0%E7%A4%BA%E4%BE%8B%E5%9B%BE%E7%89%87.png'

const handleFileChange = (e: Event) => {
  const input = e.target as HTMLInputElement
  if (input.files && input.files[0]) {
    const file = input.files[0]
    // 简单校验格式
    if (!['image/jpeg', 'image/png', 'image/webp'].includes(file.type)) {
      ElMessage.warning('仅支持 JPG/PNG/WEBP 格式的图片')
      return
    }
    imageFile.value = file
    imagePreview.value = URL.createObjectURL(file)
  }
}

const startRecognition = async () => {
  if (!imageFile.value) return

  parseLoading.value = true
  abortController.value = new AbortController()

  try {
    const data = await parseCourseImage(imageFile.value, abortController.value.signal)
    if (data.draft_status === 'reject') {
      await ElMessageBox.alert(data.message || '图片识别失败，请重新上传清晰的课表图片。', '识别被拒绝', {
        type: 'error',
        confirmButtonText: '我知道了'
      })
      return
    }

    draftStatus.value = data.draft_status
    draftMessage.value = data.message
    warnings.value = data.warnings
    
    // 对识别出的行进行排序：先按课程名聚类，再按起始周从小到大排列
    rows.value = [...data.rows]
    sortRows()
    
    step.value = 'edit'
  } catch (error: any) {
    if (error.message !== '识别已取消') {
      ElMessage.error(error.message || '识别过程中出现错误')
    }
  } finally {
    parseLoading.value = false
    abortController.value = null
  }
}

const stopRecognition = () => {
  if (abortController.value) {
    abortController.value.abort()
    ElMessage.info('识别已手动停止')
  }
}

const sortRows = () => {
  rows.value.sort((a, b) => {
    const nameCompare = (a.course_name || '').localeCompare(b.course_name || '')
    if (nameCompare !== 0) return nameCompare
    return (a.start_week || 0) - (b.start_week || 0)
  })
}

const handleOpenAddDialog = () => {
  newRowData.value = newRowTemplate()
  addDialogVisible.value = true
}

const handleConfirmAddRow = () => {
  if (!newRowData.value.course_name.trim()) {
    ElMessage.warning('课程名不能为空')
    return
  }
  rows.value.push({ ...newRowData.value })
  sortRows()
  addDialogVisible.value = false
  ElMessage.success('课程已添加并自动排序')
}

const handleRemoveRow = async (index: number) => {
  try {
    await ElMessageBox.confirm('确定要删除这一行课程安排吗？', '确认删除', {
      confirmButtonText: '确定',
      cancelButtonText: '取消',
      type: 'warning'
    })
    rows.value.splice(index, 1)
    ElMessage.success('已删除')
  } catch {
    // 用户取消删除
  }
}

// 拖拽逻辑实现
const onDragStart = (index: number) => {
  draggingIndex.value = index
}

const onDragOver = (e: DragEvent) => {
  e.preventDefault() // 允许放置
}

const onDrop = (index: number) => {
  if (draggingIndex.value === null || draggingIndex.value === index) return
  
  const draggedRow = rows.value[draggingIndex.value]
  rows.value.splice(draggingIndex.value, 1)
  rows.value.splice(index, 0, draggedRow)
  draggingIndex.value = null
}

const reset = () => {
  step.value = 'upload'
  imageFile.value = null
  imagePreview.value = ''
  rows.value = []
  draftStatus.value = null
  warnings.value = []
}

// 校验与提交逻辑
const validateRows = () => {
  const errors: string[] = []
  rows.value.forEach((row, index) => {
    if (!row.course_name.trim()) errors.push(`第 ${index + 1} 行：课程名不能为空`)
    if (row.start_week == null || row.end_week == null || row.start_week < 1 || row.end_week > 24 || row.start_week > row.end_week) {
      errors.push(`第 ${index + 1} 行：周次范围非法 (1-24)`)
    }
    if (row.day_of_week == null || row.day_of_week < 1 || row.day_of_week > 7) {
      errors.push(`第 ${index + 1} 行：星期选择非法`)
    }
    if (row.start_section == null || row.end_section == null || row.start_section < 1 || row.end_section > 12 || row.start_section > row.end_section) {
      errors.push(`第 ${index + 1} 行：节次范围非法 (1-12)`)
    }
    if (!['all', 'odd', 'even'].includes(row.week_type)) {
      errors.push(`第 ${index + 1} 行：周类型非法`)
    }
  })
  return errors
}

const handleImport = async () => {
  const errors = validateRows()
  if (errors.length > 0) {
    ElMessage.error({
      message: errors[0], // 只显示第一个错误
      duration: 3000
    })
    return
  }

  try {
    await ElMessageBox.confirm('确定要导入识别出的课程吗？这可能会覆盖或冲突现有日程。', '确认导入', {
      confirmButtonText: '确定导入',
      cancelButtonText: '我再想想',
      type: 'info'
    })

    importLoading.value = true
    const payload = buildPayload(rows.value)
    await importCourses(payload)
    ElMessage.success('课程导入成功！')
    visible.value = false
    emit('success')
  } catch (error: any) {
    if (error !== 'cancel') {
      ElMessage.error(error.message || '导入失败')
    }
  } finally {
    importLoading.value = false
  }
}

const buildPayload = (draftRows: CourseDraftRow[]): CourseImportPayload => {
  const grouped = new Map<string, CourseImportPayload['courses'][number]>()

  for (const row of draftRows) {
    // 恢复原来的聚合逻辑：以 课程名 + 地点 + 是否允许任务 为主键
    const key = `${row.course_name.trim()}__${row.location.trim()}__${row.is_allow_tasks}`
    const arrangement = {
      start_week: row.start_week!,
      end_week: row.end_week!,
      day_of_week: row.day_of_week!,
      start_section: row.start_section!,
      end_section: row.end_section!,
      week_type: row.week_type as 'all' | 'odd' | 'even'
    }

    if (!grouped.has(key)) {
      grouped.set(key, {
        course_name: row.course_name.trim(),
        location: row.location.trim(),
        is_allow_tasks: row.is_allow_tasks,
        arrangements: [arrangement]
      })
    } else {
      grouped.get(key)!.arrangements.push(arrangement)
    }
  }

  return { courses: Array.from(grouped.values()) }
}

const getRowStyle = ({ row }: { row: CourseDraftRow }) => {
  if (row.confidence < 0.75 || row.row_warnings.length > 0) {
    return { backgroundColor: 'rgba(245, 158, 11, 0.05)' }
  }
  return {}
}
</script>

<template>
  <el-dialog
    v-model="visible"
    title="导入课表"
    width="98%"
    top="2vh"
    class="import-dialog"
    :close-on-click-modal="false"
    destroy-on-close
    @closed="reset"
  >
    <!-- 阶段一：上传识别 -->
    <div v-if="step === 'upload'" class="upload-stage">
      <div class="stage-header">
        <h3>1. 上传课表截图</h3>
        <p>请上传完整的课表图片，AI 将自动识别课程信息。</p>
        <div class="upload-tips">
          <span class="tip-item">⏱️ 预计识别时长约 2 分钟</span>
          <span class="tip-item">🤖 结果由 AI 生成，请务必仔细审核</span>
        </div>
      </div>

      <div class="upload-container">
        <div class="example-box">
          <span class="badge">截图示例</span>
          <img :src="exampleImageUrl" alt="Example" class="example-img" />
        </div>

        <div class="upload-box" :class="{ 'has-file': !!imageFile }">
          <input
            type="file"
            id="course-upload-input"
            accept="image/*"
            hidden
            @change="handleFileChange"
          />
          <label for="course-upload-input" class="upload-trigger">
            <div v-if="!imagePreview" class="placeholder">
              <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
                <path d="M21 15v4a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2v-4M17 8l-5-5-5 5M12 3v12"/>
              </svg>
              <span>点击选择或拖拽图片</span>
            </div>
            <img v-else :src="imagePreview" alt="Preview" class="preview-img" />
          </label>
        </div>
      </div>

      <div class="stage-footer">
        <button
          v-if="parseLoading"
          class="btn-ghost"
          @click="stopRecognition"
        >
          停止识别
        </button>
        <button
          class="btn-primary"
          :disabled="!imageFile || parseLoading"
          @click="startRecognition"
        >
          <span v-if="parseLoading" class="spinner"></span>
          {{ parseLoading ? '正在识别中...' : '开始识别' }}
        </button>
      </div>
    </div>

    <!-- 阶段二：核对修改 -->
    <div v-else class="edit-stage">
      <div class="stage-header">
        <div class="header-left">
          <h3>2. 核对识别结果</h3>
          <p>{{ draftMessage }}</p>
        </div>
        <div class="header-actions">
          <button class="btn-ghost btn-sm" @click="handleOpenAddDialog">
            <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" style="width:16px;height:16px;margin-right:4px;">
              <path d="M12 5v14M5 12h14"/>
            </svg>
            新增课程行
          </button>
        </div>
        <div v-if="warnings.length > 0" class="warning-banner">
          <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" class="w-icon">
            <path d="M10.29 3.86L1.82 18a2 2 0 0 0 1.71 3h16.94a2 2 0 0 0 1.71-3L13.71 3.86a2 2 0 0 0-3.42 0zM12 9v4M12 17h.01"/>
          </svg>
          <div class="w-list">
            <div v-for="(w, i) in warnings" :key="i" class="w-item">{{ w }}</div>
          </div>
        </div>
      </div>

      <div class="table-container">
        <el-table
          :data="rows"
          style="width: 100%"
          :row-style="getRowStyle"
          max-height="500px"
          class="flat-table"
        >
          <el-table-column width="50" align="center">
            <template #default="{ $index }">
              <div 
                class="drag-handle" 
                draggable="true"
                @dragstart="onDragStart($index)"
                @dragover="onDragOver"
                @drop="onDrop($index)"
              >
                <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
                  <path d="M4 8h16M4 16h16"/>
                </svg>
              </div>
            </template>
          </el-table-column>
          <el-table-column label="课程名" min-width="150">
            <template #default="{ row }">
              <el-input v-model="row.course_name" placeholder="请输入" />
            </template>
          </el-table-column>
          <el-table-column label="地点" min-width="120">
            <template #default="{ row }">
              <el-input v-model="row.location" placeholder="地点" />
            </template>
          </el-table-column>
          <el-table-column label="周次" width="180">
            <template #default="{ row }">
              <div class="week-range">
                <el-input-number v-model="row.start_week" :min="1" :max="24" controls-position="right" size="small" />
                <span>-</span>
                <el-input-number v-model="row.end_week" :min="1" :max="24" controls-position="right" size="small" />
              </div>
            </template>
          </el-table-column>
          <el-table-column label="星期" width="100">
            <template #default="{ row }">
              <el-select v-model="row.day_of_week" size="small">
                <el-option v-for="i in 7" :key="i" :label="'周' + ['一','二','三','四','五','六','日'][i-1]" :value="i" />
              </el-select>
            </template>
          </el-table-column>
          <el-table-column label="节次" width="160">
            <template #default="{ row }">
              <div class="section-range">
                <el-input-number v-model="row.start_section" :min="1" :max="12" controls-position="right" size="small" />
                <span>-</span>
                <el-input-number v-model="row.end_section" :min="1" :max="12" controls-position="right" size="small" />
              </div>
            </template>
          </el-table-column>
          <el-table-column label="类型" width="100">
            <template #default="{ row }">
              <el-select v-model="row.week_type" size="small">
                <el-option label="每周" value="all" />
                <el-option label="单周" value="odd" />
                <el-option label="双周" value="even" />
              </el-select>
            </template>
          </el-table-column>
          <el-table-column label="允许嵌入" width="100" align="center">
            <template #default="{ row }">
              <el-switch v-model="row.is_allow_tasks" />
            </template>
          </el-table-column>
          <el-table-column label="状态" width="100">
            <template #default="{ row }">
              <div v-if="row.row_warnings.length > 0" class="row-warning-dot">
                <el-tooltip :content="row.row_warnings.join('; ')" placement="top">
                  <span class="warning-icon">⚠️</span>
                </el-tooltip>
              </div>
              <div v-else-if="row.confidence < 0.75" class="row-warning-dot">
                <el-tooltip content="置信度较低，请人工核对" placement="top">
                  <span class="info-icon">💡</span>
                </el-tooltip>
              </div>
              <span v-else class="success-dot"></span>
            </template>
          </el-table-column>
          <el-table-column label="操作" width="80" align="center" fixed="right">
            <template #default="{ $index }">
              <button class="btn-icon-danger" @click="handleRemoveRow($index)">
                <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
                  <path d="M3 6h18M19 6v14a2 2 0 0 1-2 2H7a2 2 0 0 1-2-2V6m3 0V4a2 2 0 0 1 2-2h4a2 2 0 0 1 2 2v2"/>
                </svg>
              </button>
            </template>
          </el-table-column>
        </el-table>
      </div>

      <div class="stage-footer">
        <button class="btn-ghost" @click="step = 'upload'">重新上传</button>
        <button
          class="btn-primary"
          :disabled="importLoading"
          @click="handleImport"
        >
          <span v-if="importLoading" class="spinner"></span>
          {{ importLoading ? '正在导入...' : '导入课程' }}
        </button>
      </div>
    </div>

    <!-- 新增课程子弹窗 -->
    <el-dialog
      v-model="addDialogVisible"
      title="手动新增课程"
      width="500px"
      append-to-body
      class="sub-dialog"
    >
      <div class="add-row-form">
        <div class="form-item">
          <label>课程名</label>
          <el-input v-model="newRowData.course_name" placeholder="例如：高等数学" />
        </div>
        <div class="form-item">
          <label>地点</label>
          <el-input v-model="newRowData.location" placeholder="例如：教一203" />
        </div>
        <div class="form-row">
          <div class="form-item">
            <label>周次范围</label>
            <div class="range-inputs">
              <el-input-number v-model="newRowData.start_week" :min="1" :max="24" controls-position="right" />
              <span>至</span>
              <el-input-number v-model="newRowData.end_week" :min="1" :max="24" controls-position="right" />
            </div>
          </div>
        </div>
        <div class="form-row">
          <div class="form-item">
            <label>星期</label>
            <el-select v-model="newRowData.day_of_week" style="width: 100%">
              <el-option v-for="i in 7" :key="i" :label="'周' + ['一','二','三','四','五','六','日'][i-1]" :value="i" />
            </el-select>
          </div>
          <div class="form-item">
            <label>周类型</label>
            <el-select v-model="newRowData.week_type" style="width: 100%">
              <el-option label="每周" value="all" />
              <el-option label="单周" value="odd" />
              <el-option label="双周" value="even" />
            </el-select>
          </div>
        </div>
        <div class="form-item">
          <label>节次范围</label>
          <div class="range-inputs">
            <el-input-number v-model="newRowData.start_section" :min="1" :max="12" controls-position="right" />
            <span>至</span>
            <el-input-number v-model="newRowData.end_section" :min="1" :max="12" controls-position="right" />
          </div>
        </div>
        <div class="form-item flex-row">
          <label>允许在该时段嵌入任务</label>
          <el-switch v-model="newRowData.is_allow_tasks" />
        </div>
      </div>
      <template #footer>
        <div class="dialog-footer">
          <button class="btn-ghost" @click="addDialogVisible = false">取消</button>
          <button class="btn-primary" @click="handleConfirmAddRow">确定添加</button>
        </div>
      </template>
    </el-dialog>
  </el-dialog>
</template>

<style scoped>
.import-dialog :deep(.el-dialog) {
  border-radius: 24px;
  overflow: hidden;
  box-shadow: 0 20px 60px rgba(15, 23, 42, 0.15);
}

.import-dialog :deep(.el-dialog__header) {
  padding: 24px 24px 0;
  margin-right: 0;
}

.import-dialog :deep(.el-dialog__title) {
  font-weight: 800;
  color: #1e293b;
  font-size: 20px;
}

.stage-header {
  padding: 0 24px 20px;
}

.stage-header h3 {
  margin: 0 0 6px;
  font-size: 18px;
  color: #1e293b;
}

.stage-header p {
  margin: 0;
  font-size: 14px;
  color: #64748b;
}

.upload-tips {
  margin-top: 12px;
  display: flex;
  gap: 16px;
}

.tip-item {
  font-size: 12px;
  color: #94a3b8;
  display: flex;
  align-items: center;
  gap: 4px;
  background: #f8fafc;
  padding: 4px 10px;
  border-radius: 6px;
  border: 1px solid #f1f5f9;
}

.upload-container {
  padding: 0 24px;
  display: grid;
  grid-template-columns: 1fr 1fr;
  gap: 24px;
}

.example-box {
  position: relative;
  background: #f8fafc;
  border-radius: 16px;
  overflow: hidden;
  display: flex;
  align-items: center;
  justify-content: center;
  border: 1px dashed #e2e8f0;
  height: 550px;
}

.example-box .badge {
  position: absolute;
  top: 12px;
  left: 12px;
  background: rgba(15, 23, 42, 0.6);
  color: white;
  padding: 4px 10px;
  border-radius: 6px;
  font-size: 12px;
  backdrop-filter: blur(4px);
}

.example-img {
  max-width: 100%;
  max-height: 100%;
  object-fit: contain;
  opacity: 1;
}

.upload-box {
  background: #ffffff;
  border: 2px dashed #e2e8f0;
  border-radius: 16px;
  transition: all 0.2s ease;
  cursor: pointer;
  overflow: hidden;
  height: 550px;
}

.upload-box:hover {
  border-color: #3b82f6;
  background: #f0f7ff;
}

.upload-box.has-file {
  border-style: solid;
}

.upload-trigger {
  width: 100%;
  height: 100%;
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  cursor: pointer;
}

.upload-trigger .placeholder {
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
}

.upload-trigger svg {
  width: 40px;
  height: 40px;
  color: #94a3b8;
  margin-bottom: 12px;
}

.upload-trigger span {
  font-size: 14px;
  color: #64748b;
}

.preview-img {
  max-width: 100%;
  max-height: 100%;
  object-fit: contain;
}

.stage-footer {
  padding: 24px;
  display: flex;
  justify-content: flex-end;
  gap: 12px;
}

.btn-primary {
  height: 44px;
  padding: 0 24px;
  background: #3b82f6;
  color: white;
  border: none;
  border-radius: 12px;
  font-weight: 700;
  cursor: pointer;
  display: inline-flex;
  align-items: center;
  gap: 8px;
  transition: all 0.2s ease;
  box-shadow: 0 4px 12px rgba(59, 130, 246, 0.2);
}

.btn-primary:hover:not(:disabled) {
  background: #2563eb;
  transform: translateY(-1px);
  box-shadow: 0 6px 16px rgba(37, 99, 235, 0.3);
}

.btn-primary:disabled {
  opacity: 0.6;
  cursor: not-allowed;
}

.btn-ghost {
  height: 44px;
  padding: 0 24px;
  background: #f1f5f9;
  color: #475569;
  border: none;
  border-radius: 12px;
  font-weight: 700;
  cursor: pointer;
  transition: all 0.2s ease;
}

.btn-ghost:hover {
  background: #e2e8f0;
  color: #1e293b;
}

.warning-banner {
  margin-top: 12px;
  background: #fffbeb;
  border: 1px solid #fef3c7;
  border-radius: 12px;
  padding: 12px 16px;
  display: flex;
  gap: 12px;
  align-items: flex-start;
}

.w-icon {
  width: 20px;
  height: 20px;
  color: #d97706;
  flex-shrink: 0;
}

.w-list {
  font-size: 13px;
  color: #92400e;
  line-height: 1.5;
}

.table-container {
  padding: 0 24px;
}

.flat-table :deep(.el-table__header-wrapper) th {
  background: #f8fafc;
  color: #475569;
  font-weight: 700;
  border-bottom: 1px solid #f1f5f9;
}

.flat-table :deep(.el-table__row) td {
  border-bottom: 1px solid #f1f5f9;
  padding: 8px 0;
}

.week-range, .section-range {
  display: flex;
  align-items: center;
  gap: 4px;
}

.week-range span, .section-range span {
  color: #94a3b8;
}

.success-dot {
  display: inline-block;
  width: 8px;
  height: 8px;
  background: #22c55e;
  border-radius: 50%;
}

.row-warning-dot {
  cursor: help;
}

.spinner {
  width: 16px;
  height: 16px;
  border: 2px solid rgba(255, 255, 255, 0.3);
  border-top-color: #ffffff;
  border-radius: 50%;
  animation: spin 0.8s linear infinite;
}

.btn-icon-danger {
  background: transparent;
  border: none;
  color: #ef4444;
  cursor: pointer;
  padding: 4px;
  border-radius: 6px;
  display: flex;
  align-items: center;
  justify-content: center;
  transition: all 0.2s ease;
}

.btn-icon-danger:hover {
  background: #fee2e2;
}

.btn-icon-danger svg {
  width: 18px;
  height: 18px;
}

@keyframes spin {
  to { transform: rotate(360deg); }
}

:deep(.el-input__wrapper), :deep(.el-input-number), :deep(.el-select) {
  border-radius: 8px !important;
  box-shadow: none !important;
  border: 1px solid #e2e8f0 !important;
}

:deep(.el-input__wrapper.is-focus) {
  border-color: #3b82f6 !important;
}
.drag-handle {
  cursor: grab;
  color: #94a3b8;
  display: flex;
  align-items: center;
  justify-content: center;
  padding: 4px;
  border-radius: 4px;
  transition: all 0.2s ease;
}

.drag-handle:hover {
  background: #f1f5f9;
  color: #64748b;
}

.drag-handle svg {
  width: 16px;
  height: 16px;
}

.drag-handle:active {
  cursor: grabbing;
}

.header-actions {
  margin-left: auto;
  display: flex;
  align-items: center;
  gap: 12px;
}

.btn-sm {
  height: 32px;
  padding: 0 12px;
  font-size: 13px;
  border-radius: 8px;
}

.add-row-form {
  display: flex;
  flex-direction: column;
  gap: 16px;
}

.form-item {
  display: flex;
  flex-direction: column;
  gap: 8px;
}

.form-item label {
  font-size: 13px;
  font-weight: 600;
  color: #64748b;
}

.form-row {
  display: grid;
  grid-template-columns: 1fr 1fr;
  gap: 16px;
}

.range-inputs {
  display: flex;
  align-items: center;
  gap: 12px;
}

.range-inputs span {
  color: #94a3b8;
  font-size: 13px;
}

.flex-row {
  flex-direction: row;
  justify-content: space-between;
  align-items: center;
}

.dialog-footer {
  display: flex;
  justify-content: flex-end;
  gap: 12px;
}
</style>
