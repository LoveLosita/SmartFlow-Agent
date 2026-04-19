<script setup lang="ts">
import { onBeforeUnmount, onMounted, reactive, ref } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'

interface BaselineLine {
  id: string
  atMs: number
  text: string
}

type ToolLineState = 'called' | 'create' | 'blocked'

interface EnhancedLine {
  id: string
  atMs: number
  type: 'text' | 'tool' | 'schedule_card'
  text?: string
  summary?: string
  detail?: string
  state?: ToolLineState
  toolName?: string
}

const baselineScript: BaselineLine[] = [
  { id: 'base-1', atMs: 0, text: '我先看一下当前任务队列，再把晚间学习任务挪开。' },
  { id: 'base-2', atMs: 680, text: '我已把队首任务调到周六晚间，不影响你今天课程。' },
  { id: 'base-3', atMs: 1360, text: '另外已帮你记录“周日报告提交”的提醒。' },
  { id: 'base-4', atMs: 1980, text: '如果你还要我打乱同类任务顺序，需要你先明确授权。' },
]

const enhancedScript: EnhancedLine[] = [
  { id: 'enh-1', atMs: 0, type: 'text', text: '我先看一下当前任务队列，再把晚间学习任务挪开。' },
  {
    id: 'enh-2',
    atMs: 260,
    type: 'tool',
    state: 'called',
    toolName: 'queue_status',
    summary: '已调用工具：查看任务队列',
    detail: '现在还有 3 项待处理、已完成 1 项。我会先处理当前队首任务。',
  },
  {
    id: 'enh-3',
    atMs: 620,
    type: 'tool',
    state: 'called',
    toolName: 'queue_pop_head',
    summary: '已调用工具：取出当前要处理的任务',
    detail: '已取到“英语听力练习”，准备尝试挪到周六晚间。',
  },
  {
    id: 'enh-4',
    atMs: 980,
    type: 'tool',
    state: 'called',
    toolName: 'queue_apply_head_move',
    summary: '已调用工具：调整任务到新时段',
    detail: '调整成功，晚间冲突已解除，待处理数量减少到 2 项。',
  },
  { id: 'enh-5', atMs: 1360, type: 'text', text: '我已把队首任务调到周六晚间，不影响你今天课程。' },
  {
    id: 'enh-6',
    atMs: 1730,
    type: 'tool',
    state: 'create',
    toolName: 'quick_note_create',
    summary: '已创建：周日报告提交提醒',
    detail: '提醒已经记下，优先级是“重要不紧急”。',
  },
  {
    id: 'enh-7',
    atMs: 2100,
    type: 'tool',
    state: 'blocked',
    toolName: 'min_context_switch',
    summary: '已拦截：打乱顺序的操作',
    detail: '你还没授权“允许打乱顺序”，所以这一步先不执行。',
  },
  { id: 'enh-8', atMs: 2460, type: 'text', text: '如果你还要我打乱同类任务顺序，需要你先明确授权。' },
  {
    id: 'enh-9',
    atMs: 2800,
    type: 'schedule_card',
    summary: '日程表编排已就绪',
    detail: '已为你避开晚上课程，并插入了“周日报告”提醒。点击查看并微调。',
  },
]

const baselineLines = ref<BaselineLine[]>([])
const enhancedLines = ref<EnhancedLine[]>([])
const isReplaying = ref(false)
const replayRound = ref(0)
const expandedToolLineMap = reactive<Record<string, boolean>>({})
const isScheduleModalVisible = ref(false)

// 模拟日程数据
interface MockScheduleItem {
  id: number
  name: string
  day: number // 1-7
  order: number // 1-5
  type: 'course' | 'task'
}

const sectionSlots = [
  { order: 1, title: '1-2', timeRange: '08:00\n09:40' },
  { order: 2, title: '3-4', timeRange: '10:15\n11:55' },
  { order: 3, title: '5-6', timeRange: '14:00\n15:40' },
  { order: 4, title: '7-8', timeRange: '16:15\n17:55' },
  { order: 5, title: '9-10', timeRange: '19:00\n20:40' },
  { order: 6, title: '11-12', timeRange: '20:50\n22:30' },
]

const mockSchedule = ref<MockScheduleItem[]>([
  { id: 1, name: '软件架构设计 (课程)', day: 1, order: 1, type: 'course' },
  { id: 2, name: '英语听力练习', day: 1, order: 3, type: 'task' },
  { id: 3, name: '算法分析 (课程)', day: 2, order: 2, type: 'course' },
  { id: 4, name: '周日报告提交', day: 7, order: 5, type: 'task' },
])

const isSaving = ref(false)
const currentWeek = ref(1)

function openScheduleModal() {
  isScheduleModalVisible.value = true
}

function closeScheduleModal() {
  isScheduleModalVisible.value = false
}

function prevWeek() {
  if (currentWeek.value > 1) {
    currentWeek.value--
  }
}

function nextWeek() {
  currentWeek.value++
}

function handleSaveToState() {
  isSaving.value = true
  setTimeout(() => {
    isSaving.value = false
    ElMessage({
      message: '日程已成功暂存至运行时 State',
      type: 'success',
      plain: true
    })
  }, 600)
}

function handleOfficialSave() {
  ElMessageBox.confirm(
    '正式保存日程将把当前编排结果写入数据库。注意：保存后本轮编排微调将会终止，无法撤回。确认继续吗？',
    '正式保存确认',
    {
      confirmButtonText: '确认保存',
      cancelButtonText: '我再想想',
      type: 'warning',
      roundButton: true,
      customClass: 'premium-msg-box',
    }
  ).then(() => {
    isSaving.value = true
    setTimeout(() => {
      isSaving.value = false
      closeScheduleModal()
      ElMessage.success('日程已正式持久化到数据库')
    }, 1200)
  }).catch(() => {
    // 用户取消，无需操作
  })
}

// 拖拽逻辑
function onDragStart(event: DragEvent, item: MockScheduleItem) {
  if (event.dataTransfer) {
    event.dataTransfer.setData('text/plain', item.id.toString())
    event.dataTransfer.effectAllowed = 'move'
  }
}

function onDrop(event: DragEvent, day: number, order: number) {
  if (event.dataTransfer) {
    const id = parseInt(event.dataTransfer.getData('text/plain'))
    const item = mockSchedule.value.find(i => i.id === id)
    if (item) {
      item.day = day
      item.order = order
    }
  }
}

const timerHandles = new Set<number>()

function clearReplayTimers() {
  timerHandles.forEach((handle) => {
    window.clearTimeout(handle)
  })
  timerHandles.clear()
}

function resetExpandedToolLineState() {
  for (const key of Object.keys(expandedToolLineMap)) {
    delete expandedToolLineMap[key]
  }
}

function replay() {
  clearReplayTimers()
  replayRound.value += 1
  baselineLines.value = []
  enhancedLines.value = []
  resetExpandedToolLineState()
  isReplaying.value = true

  for (const line of baselineScript) {
    const handle = window.setTimeout(() => {
      baselineLines.value = [...baselineLines.value, line]
    }, line.atMs)
    timerHandles.add(handle)
  }

  for (const line of enhancedScript) {
    const handle = window.setTimeout(() => {
      enhancedLines.value = [...enhancedLines.value, line]
    }, line.atMs)
    timerHandles.add(handle)
  }

  const doneDelay = Math.max(
    ...baselineScript.map((item) => item.atMs),
    ...enhancedScript.map((item) => item.atMs),
  ) + 220
  const doneHandle = window.setTimeout(() => {
    isReplaying.value = false
    timerHandles.delete(doneHandle)
  }, doneDelay)
  timerHandles.add(doneHandle)
}

function toolStateLabel(state?: ToolLineState) {
  if (state === 'called') {
    return '已调用'
  }
  if (state === 'create') {
    return '已创建'
  }
  if (state === 'blocked') {
    return '已拦截'
  }
  return '工具'
}

function isToolLineExpanded(lineId: string) {
  return expandedToolLineMap[lineId] === true
}

function toggleToolLineExpanded(lineId: string) {
  expandedToolLineMap[lineId] = !expandedToolLineMap[lineId]
}

onMounted(() => {
  replay()
})

onBeforeUnmount(() => {
  clearReplayTimers()
})
</script>

<template>
  <main class="tool-compare-proto" :key="replayRound">
    <header class="tool-compare-proto__top">
      <div>
        <p class="tool-compare-proto__eyebrow">工具调用可视化原型（对比模式）</p>
        <h1>同一份聊天，左边原样，右边折叠式工具提示</h1>
        <p>目标：默认简洁，只在你展开时看具体细节。</p>
      </div>
      <div class="tool-compare-proto__actions">
        <button type="button" class="tool-compare-proto__btn tool-compare-proto__btn--primary" @click="replay">
          {{ isReplaying ? '重播中...' : '重播对比' }}
        </button>
        <a class="tool-compare-proto__btn tool-compare-proto__btn--ghost" href="/assistant">返回聊天页</a>
      </div>
    </header>

    <section class="tool-compare-proto__grid">
      <article class="tool-panel">
        <header class="tool-panel__header">
          <p class="tool-panel__tag">当前样式</p>
          <h2>聊天页基线（无工具提示）</h2>
        </header>

        <div class="tool-panel__body">
          <div class="tool-message tool-message--user">
            <div class="tool-message__bubble">帮我把今天任务重新安排一下，别影响晚上的课程，顺便记一下周日报告。</div>
          </div>

          <div class="tool-message tool-message--assistant">
            <div class="tool-message__meta">
              <span>Assistant</span>
              <small>{{ isReplaying ? '流式中' : '完成' }}</small>
            </div>
            <div class="tool-message__bubble tool-message__bubble--assistant">
              <p v-for="line in baselineLines" :key="line.id" class="proto-line">{{ line.text }}</p>
              <p v-if="baselineLines.length <= 0" class="proto-line proto-line--placeholder">正在生成回复...</p>
            </div>
          </div>
        </div>
      </article>

      <article class="tool-panel">
        <header class="tool-panel__header">
          <p class="tool-panel__tag">提案样式</p>
          <h2>折叠式工具提示（扳手图标）</h2>
        </header>

        <div class="tool-panel__body">
          <div class="tool-message tool-message--user">
            <div class="tool-message__bubble">帮我把今天任务重新安排一下，别影响晚上的课程，顺便记一下周日报告。</div>
          </div>

          <div class="tool-message tool-message--assistant">
            <div class="tool-message__meta">
              <span>Assistant</span>
              <small>{{ isReplaying ? '流式中' : '完成' }}</small>
            </div>
            <div class="tool-message__bubble tool-message__bubble--assistant">
              <template v-for="line in enhancedLines" :key="line.id">
                <p v-if="line.type === 'text'" class="proto-line">{{ line.text }}</p>

                <div
                  v-else-if="line.type === 'tool'"
                  class="proto-tool"
                  :class="{
                    'proto-tool--called': line.state === 'called',
                    'proto-tool--create': line.state === 'create',
                    'proto-tool--blocked': line.state === 'blocked',
                  }"
                >
                  <button type="button" class="proto-tool__head" @click="toggleToolLineExpanded(line.id)">
                    <span class="proto-tool__icon" aria-hidden="true">
                      <!-- 扳手图标 -->
                      <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
                        <path d="M14.7 6.3a1 1 0 0 0 0 1.4l1.6 1.6a1 1 0 0 0 1.4 0l3.77-3.77a6 6 0 0 1-7.94 7.94l-6.91 6.91a2.12 2.12 0 0 1-3-3l6.91-6.91a6 6 0 0 1 7.94-7.94l-3.76 3.76z" />
                      </svg>
                    </span>
                    <span class="proto-tool__summary">{{ line.summary }}</span>
                    <em class="proto-tool__badge">{{ toolStateLabel(line.state) }}</em>
                    <span class="proto-tool__chevron" :class="{ 'proto-tool__chevron--expanded': isToolLineExpanded(line.id) }" aria-hidden="true">
                      <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round" stroke-linejoin="round">
                        <polyline points="9 18 15 12 9 6"></polyline>
                      </svg>
                    </span>
                  </button>

                  <p v-if="isToolLineExpanded(line.id)" class="proto-tool__detail">
                    {{ line.detail }}
                  </p>
                </div>

                <!-- 日程小卡片 -->
                <div
                  v-else-if="line.type === 'schedule_card'"
                  class="proto-schedule-card"
                  @click="openScheduleModal"
                >
                  <div class="proto-schedule-card__icon">
                    <svg width="20" height="20" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
                      <rect x="3" y="4" width="18" height="18" rx="2" ry="2"></rect>
                      <line x1="16" y1="2" x2="16" y2="6"></line>
                      <line x1="8" y1="2" x2="8" y2="6"></line>
                      <line x1="3" y1="10" x2="21" y2="10"></line>
                    </svg>
                  </div>
                  <div class="proto-schedule-card__content">
                    <h4 class="proto-schedule-card__summary">{{ line.summary }}</h4>
                    <p class="proto-schedule-card__detail">{{ line.detail }}</p>
                  </div>
                  <div class="proto-schedule-card__arrow">
                    <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round" stroke-linejoin="round">
                      <polyline points="9 18 15 12 9 6"></polyline>
                    </svg>
                  </div>
                </div>
              </template>

              <p v-if="enhancedLines.length <= 0" class="proto-line proto-line--placeholder">正在生成回复...</p>
            </div>
          </div>
        </div>
      </article>
    </section>

    <!-- 日程编排弹窗 -->
    <Teleport to="body">
      <Transition name="modal">
        <div v-if="isScheduleModalVisible" class="schedule-modal-overlay" @click.self="closeScheduleModal">
          <div class="schedule-modal">
            <header class="schedule-modal__header">
              <h3>日程预览与精排 (第 {{ currentWeek }} 周)</h3>
              <div class="schedule-modal__header-actions">
                <div class="week-switcher">
                  <button class="week-switcher__btn" @click="prevWeek" :disabled="currentWeek <= 1">
                    <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round" stroke-linejoin="round">
                      <polyline points="15 18 9 12 15 6"></polyline>
                    </svg>
                  </button>
                  <span class="week-switcher__label">第 {{ currentWeek }} 周</span>
                  <button class="week-switcher__btn" @click="nextWeek" :disabled="currentWeek >= 20">
                    <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round" stroke-linejoin="round">
                      <polyline points="9 18 15 12 9 6"></polyline>
                    </svg>
                  </button>
                </div>
                <button class="schedule-modal__close" @click="closeScheduleModal">
                  <svg width="20" height="20" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round" stroke-linejoin="round">
                    <line x1="18" y1="6" x2="6" y2="18"></line>
                    <line x1="6" y1="6" x2="18" y2="18"></line>
                  </svg>
                </button>
              </div>
            </header>
            
            <div class="schedule-modal__body">
              <div class="planning-board__grid">
                <!-- 避让左上角 -->
                <div class="planning-board__corner" />

                <!-- 表头：周一到周日 -->
                <div v-for="d in 7" :key="`h-${d}`" class="planning-board__day-head">
                  <span>{{ ['周一', '周二', '周三', '周四', '周五', '周六', '周日'][d-1] }}</span>
                  <small>{{ d + 18 }}日</small>
                </div>

                <!-- 表身：6 个节次（每节次两行） -->
                <template v-for="slot in sectionSlots" :key="`r-${slot.order}`">
                  <div class="planning-board__time-cell">
                    <strong>{{ slot.title }}</strong>
                    <small>{{ slot.timeRange }}</small>
                  </div>
                  <div
                    v-for="d in 7"
                    :key="`c-${d}-${slot.order}`"
                    class="planning-board__cell"
                    :class="{
                      'planning-board__cell--empty': !mockSchedule.some(i => i.day === d && i.order === slot.order),
                      'board-item-pop': mockSchedule.some(i => i.day === d && i.order === slot.order)
                    }"
                    @dragover.prevent
                    @drop="onDrop($event, d, slot.order)"
                  >
                    <div
                      v-for="item in mockSchedule.filter(i => i.day === d && i.order === slot.order)"
                      :key="item.id"
                      class="planning-board__cell-main"
                      :class="`planning-board__cell-main--${item.type}`"
                      draggable="true"
                      @dragstart="onDragStart($event, item)"
                    >
                      <strong>{{ item.name }}</strong>
                      <span>{{ item.type === 'course' ? '教学楼 A' : '个人任务' }}</span>
                    </div>
                  </div>
                </template>
              </div>
            </div>

            <footer class="schedule-modal__footer">
              <p class="schedule-modal__hint">提示：拖动卡片可调整日程顺序</p>
              <div class="schedule-modal__actions">
                <button class="tool-compare-proto__btn tool-compare-proto__btn--ghost" @click="closeScheduleModal">取消</button>
                <button 
                  class="tool-compare-proto__btn tool-compare-proto__btn--state" 
                  :disabled="isSaving"
                  @click="handleSaveToState"
                >
                  暂存进state
                </button>
                <button 
                  class="tool-compare-proto__btn tool-compare-proto__btn--primary" 
                  :disabled="isSaving"
                  @click="handleOfficialSave"
                >
                  {{ isSaving ? '保存中...' : '正式保存日程' }}
                </button>
              </div>
            </footer>
          </div>
        </div>
      </Transition>
    </Teleport>
  </main>
</template>

<style scoped>
.tool-compare-proto {
  min-height: 100vh;
  padding: 24px 20px 48px;
  background:
    radial-gradient(circle at 10% 10%, rgba(59, 130, 246, 0.05), transparent 40%),
    radial-gradient(circle at 90% 90%, rgba(16, 185, 129, 0.05), transparent 40%),
    #f8fafc;
  color: #0f172a;
  font-family: 'Inter', -apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, sans-serif;
}

.tool-compare-proto__top {
  width: min(1200px, 100%);
  margin: 0 auto;
  border-radius: 20px;
  border: 1px solid rgba(226, 232, 240, 0.8);
  background: rgba(255, 255, 255, 0.7);
  backdrop-filter: blur(12px);
  padding: 24px 32px;
  display: flex;
  gap: 20px;
  justify-content: space-between;
  align-items: center;
  flex-wrap: wrap;
  box-shadow: 0 4px 6px -1px rgba(0, 0, 0, 0.02), 0 2px 4px -2px rgba(0, 0, 0, 0.02);
}

.tool-compare-proto__eyebrow {
  margin: 0;
  color: #3b82f6;
  font-size: 13px;
  font-weight: 600;
  text-transform: uppercase;
  letter-spacing: 0.05em;
}

.tool-compare-proto__top h1 {
  margin: 8px 0 6px;
  font-size: clamp(24px, 2.5vw, 32px);
  font-weight: 800;
  letter-spacing: -0.02em;
  color: #1e293b;
}

.tool-compare-proto__top p {
  margin: 0;
  color: #64748b;
  font-size: 14px;
}

.tool-compare-proto__actions {
  display: flex;
  gap: 12px;
}

.tool-compare-proto__btn {
  border: 1px solid transparent;
  border-radius: 12px;
  padding: 10px 20px;
  font-size: 14px;
  font-weight: 600;
  transition: all 0.2s cubic-bezier(0.4, 0, 0.2, 1);
  cursor: pointer;
  display: flex;
  align-items: center;
  justify-content: center;
  text-decoration: none;
}

.tool-compare-proto__btn--primary {
  background: #0f172a;
  color: #ffffff;
  box-shadow: 0 4px 12px rgba(15, 23, 42, 0.15);
}

.tool-compare-proto__btn--primary:hover {
  background: #1e293b;
  transform: translateY(-2px);
  box-shadow: 0 6px 16px rgba(15, 23, 42, 0.2);
}

.tool-compare-proto__btn--state {
  background: #eff6ff;
  color: #2563eb;
  border-color: #dbeafe;
}

.tool-compare-proto__btn--state:hover {
  background: #dbeafe;
  color: #1e40af;
  border-color: #bfdbfe;
}

.tool-compare-proto__btn--ghost {
  background: #ffffff;
  color: #475569;
  border-color: #e2e8f0;
}

.tool-compare-proto__btn--ghost:hover {
  background: #f8fafc;
  border-color: #cbd5e1;
  color: #1e293b;
}

.tool-compare-proto__grid {
  width: min(1200px, 100%);
  margin: 32px auto 0;
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 24px;
}

.tool-panel {
  border-radius: 20px;
  border: 1px solid #e2e8f0;
  background: #ffffff;
  min-height: 500px;
  display: flex;
  flex-direction: column;
  box-shadow: 0 1px 3px rgba(0,0,0,0.05);
  transition: transform 0.3s ease, box-shadow 0.3s ease;
}

.tool-panel:hover {
  transform: translateY(-2px);
  box-shadow: 0 12px 24px -8px rgba(0,0,0,0.08);
}

.tool-panel__header {
  padding: 20px 24px;
  border-bottom: 1px solid #f1f5f9;
}

.tool-panel__tag {
  margin: 0;
  color: #3b82f6;
  font-size: 11px;
  font-weight: 700;
  text-transform: uppercase;
  letter-spacing: 0.05em;
}

.tool-panel__header h2 {
  margin: 4px 0 0;
  font-size: 18px;
  font-weight: 600;
  color: #1e293b;
}

.tool-panel__body {
  padding: 24px;
  flex: 1;
  display: flex;
  flex-direction: column;
  gap: 16px;
  overflow-y: auto;
}

.tool-message {
  display: flex;
  flex-direction: column;
}

.tool-message--user {
  align-items: flex-end;
}

.tool-message__meta {
  margin-bottom: 8px;
  display: flex;
  gap: 8px;
  align-items: center;
  color: #94a3b8;
  font-size: 12px;
  font-weight: 500;
}

.tool-message__bubble {
  max-width: 90%;
  padding: 12px 16px;
  border-radius: 16px;
  border: 1px solid #e2e8f0;
  background: #ffffff;
  font-size: 14px;
  line-height: 1.6;
  color: #334155;
}

.tool-message--user .tool-message__bubble {
  color: #ffffff;
  border: none;
  background: linear-gradient(135deg, #3b82f6 0%, #2563eb 100%);
  border-bottom-right-radius: 4px;
  box-shadow: 0 4px 12px rgba(37, 99, 235, 0.15);
}

.tool-message__bubble--assistant {
  background: #f8fafc;
  border-color: #f1f5f9;
  border-bottom-left-radius: 4px;
}

.proto-line {
  margin: 0;
  opacity: 0;
  animation: line-fade-in 0.4s ease-out forwards;
}

.proto-line + .proto-line,
.proto-line + .proto-tool,
.proto-tool + .proto-line,
.proto-tool + .proto-tool {
  margin-top: 10px;
}

.proto-line--placeholder {
  color: #94a3b8;
  font-style: italic;
}

.proto-tool {
  font-size: 13px;
  border-radius: 10px;
  border: 1px solid #e2e8f0;
  background: #f8fafc;
  transition: all 0.2s cubic-bezier(0.4, 0, 0.2, 1);
  overflow: hidden;
  position: relative;
}

.proto-tool:hover {
  border-color: #cbd5e1;
  background: #f1f5f9;
  box-shadow: 0 4px 6px -1px rgba(0, 0, 0, 0.05), 0 2px 4px -1px rgba(0, 0, 0, 0.03);
}

/* 状态修饰符：左侧装饰条与特定背景色 */
.proto-tool--called {
  border-left: 4px solid #3b82f6;
}

.proto-tool--create {
  border-left: 4px solid #10b981;
}

.proto-tool--blocked {
  border-left: 4px solid #f43f5e;
}

.proto-tool__head {
  width: 100%;
  border: none;
  background: transparent;
  color: #1e293b;
  padding: 10px 12px;
  display: flex;
  align-items: center;
  gap: 10px;
  text-align: left;
  cursor: pointer;
  outline: none;
}

.proto-tool__icon {
  width: 24px;
  height: 24px;
  border-radius: 6px;
  display: flex;
  align-items: center;
  justify-content: center;
  color: #64748b;
  background: #ffffff;
  border: 1px solid #e2e8f0;
  flex: 0 0 24px;
  transition: all 0.2s;
}

.proto-tool:hover .proto-tool__icon {
  border-color: #cbd5e1;
  color: #334155;
}

.proto-tool__summary {
  flex: 1;
  min-width: 0;
  font-weight: 500;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.proto-tool__badge {
  font-style: normal;
  font-size: 11px;
  font-weight: 600;
  letter-spacing: 0.02em;
  border-radius: 6px;
  padding: 2px 8px;
  line-height: normal;
}

/* 根据状态调整 badge */
.proto-tool--called .proto-tool__badge {
  background: #dbeafe;
  color: #1e40af;
}

.proto-tool--create .proto-tool__badge {
  background: #d1fae5;
  color: #065f46;
}

.proto-tool--blocked .proto-tool__badge {
  background: #ffe4e6;
  color: #9f1239;
}

.proto-tool__chevron {
  display: flex;
  align-items: center;
  justify-content: center;
  width: 20px;
  height: 20px;
  color: #94a3b8;
  transition: transform 0.25s cubic-bezier(0.4, 0, 0.2, 1);
}

.proto-tool__chevron--expanded {
  transform: rotate(90deg);
}

.proto-tool__detail {
  margin: 0;
  padding: 0 16px 12px 46px;
  color: #475569;
  font-size: 13px;
  line-height: 1.6;
  border-top: 1px solid transparent;
  animation: detail-slide-down 0.2s ease-out;
}

@keyframes detail-slide-down {
  from {
    opacity: 0;
    transform: translateY(-4px);
  }
  to {
    opacity: 1;
    transform: translateY(0);
  }
}

@keyframes line-fade-in {
  from {
    opacity: 0;
    transform: translateY(8px);
  }
  to {
    opacity: 1;
    transform: translateY(0);
  }
}

@media (max-width: 980px) {
  .tool-compare-proto__grid {
    grid-template-columns: 1fr;
  }
}

/* 新增：日程小卡片样式 */
.proto-schedule-card {
  margin-top: 10px;
  background: white;
  border: 1px solid #e2e8f0;
  border-radius: 12px;
  padding: 16px;
  display: flex;
  align-items: center;
  gap: 16px;
  cursor: pointer;
  transition: all 0.3s cubic-bezier(0.4, 0, 0.2, 1);
  box-shadow: 0 4px 12px rgba(0, 0, 0, 0.05);
  animation: line-fade-in 0.5s ease-out forwards;
  animation-delay: 0.1s;
  opacity: 0;
}

.proto-schedule-card:hover {
  transform: translateY(-4px) scale(1.02);
  border-color: #3b82f6;
  box-shadow: 0 12px 24px -8px rgba(59, 130, 246, 0.15);
}

.proto-schedule-card__icon {
  width: 44px;
  height: 44px;
  background: #eff6ff;
  color: #3b82f6;
  border-radius: 10px;
  display: flex;
  align-items: center;
  justify-content: center;
  flex-shrink: 0;
}

.proto-schedule-card__content {
  flex: 1;
}

.proto-schedule-card__summary {
  margin: 0 0 4px;
  font-size: 15px;
  font-weight: 700;
  color: #1e293b;
}

.proto-schedule-card__detail {
  margin: 0;
  font-size: 13px;
  color: #64748b;
  line-height: 1.4;
}

.proto-schedule-card__arrow {
  color: #94a3b8;
  transition: transform 0.2s;
}

.proto-schedule-card:hover .proto-schedule-card__arrow {
  color: #3b82f6;
  transform: translateX(4px);
}

/* 弹窗核心样式 */
.schedule-modal-overlay {
  position: fixed;
  inset: 0;
  background: rgba(15, 23, 42, 0.4);
  backdrop-filter: blur(8px);
  z-index: 500;
  display: flex;
  align-items: center;
  justify-content: center;
  padding: 20px;
}

.schedule-modal {
  background: #ffffff;
  width: min(1400px, 92%);
  height: auto;
  max-height: 95vh;
  border-radius: 20px;
  display: flex;
  flex-direction: column;
  box-shadow: 0 25px 50px -12px rgba(0, 0, 0, 0.25);
  overflow: hidden;
}

.schedule-modal__header {
  padding: 20px 32px;
  border-bottom: 1px solid #f1f5f9;
  display: flex;
  justify-content: space-between;
  align-items: center;
  background: #ffffff;
}

.schedule-modal__header-actions {
  display: flex;
  align-items: center;
  gap: 20px;
}

.week-switcher {
  display: flex;
  align-items: center;
  gap: 12px;
  background: #f8fafc;
  padding: 4px;
  border-radius: 12px;
  border: 1px solid #f1f5f9;
}

.week-switcher__btn {
  width: 32px;
  height: 32px;
  border: none;
  background: #ffffff;
  border-radius: 8px;
  display: flex;
  align-items: center;
  justify-content: center;
  color: #64748b;
  cursor: pointer;
  transition: all 0.2s;
  box-shadow: 0 1px 2px rgba(0, 0, 0, 0.05);
}

.week-switcher__btn:hover:not(:disabled) {
  background: #eff6ff;
  color: #3b82f6;
  transform: translateY(-1px);
}

.week-switcher__btn:disabled {
  opacity: 0.4;
  cursor: not-allowed;
}

.week-switcher__label {
  font-size: 14px;
  font-weight: 700;
  color: #1e293b;
  min-width: 60px;
  text-align: center;
}

.schedule-modal__header h3 {
  margin: 0;
  font-size: 18px;
  font-weight: 800;
  color: #0f172a;
  letter-spacing: -0.02em;
}

.schedule-modal__close {
  background: #f1f5f9;
  border: none;
  width: 36px;
  height: 36px;
  border-radius: 50%;
  display: flex;
  align-items: center;
  justify-content: center;
  color: #64748b;
  cursor: pointer;
  transition: all 0.2s;
}

.schedule-modal__close:hover {
  background: #e2e8f0;
  color: #1e293b;
  transform: rotate(90deg);
}

.schedule-modal__body {
  padding: 0; /* 这里的 padding 让 grid 自己处理，保持 full-bleed 效果 */
  flex: 1;
  overflow-y: auto;
}

.schedule-modal__footer {
  padding: 20px 32px;
  background: #f8fafc;
  border-top: 1px solid #f1f5f9;
  display: flex;
  justify-content: space-between;
  align-items: center;
}

.schedule-modal__hint {
  margin: 0;
  font-size: 13px;
  color: #94a3b8;
  font-style: italic;
}

.schedule-modal__actions {
  display: flex;
  gap: 12px;
}

/* 基准样式：同步自 WeekPlanningBoard.vue */
.planning-board__grid {
  --planning-grid-padding-x: 20px;
  --planning-grid-padding-y: 16px;
  --planning-grid-gap-x: 10px;
  --planning-grid-gap-y: 10px;
  --planning-time-column-width: 76px;
  --planning-day-column-min: 96px;
  --planning-cell-height: 82px;
  
  display: grid;
  grid-template-columns: var(--planning-time-column-width) repeat(7, minmax(var(--planning-day-column-min), 1fr));
  gap: var(--planning-grid-gap-y) var(--planning-grid-gap-x);
  padding: var(--planning-grid-padding-y) var(--planning-grid-padding-x) 32px;
  overflow: auto;
  background: #ffffff;
}

.planning-board__corner {
  min-height: 1px;
}

.planning-board__day-head {
  display: grid;
  justify-items: center;
  gap: 4px;
  color: #64748b;
  padding-bottom: 12px;
}

.planning-board__day-head span {
  font-size: 14px;
  font-weight: 700;
  letter-spacing: 0.08em;
  color: #1e293b;
}

.planning-board__day-head small {
  font-size: 12px;
  color: #94a3b8;
}

.planning-board__time-cell {
  min-height: var(--planning-cell-height);
  display: grid;
  align-content: center;
  justify-items: end;
  color: #94a3b8;
  padding-right: 16px;
  border-right: 1px solid #f1f5f9;
}

.planning-board__time-cell strong {
  font-size: 15px;
  color: #475569;
  font-weight: 800;
}

.planning-board__time-cell small {
  font-size: 11px;
  color: #94a3b8;
  white-space: pre-line;
  text-align: right;
  line-height: 1.35;
}

.planning-board__cell {
  position: relative;
  min-height: var(--planning-cell-height);
  border-radius: 16px;
  border: 1px solid transparent;
  display: flex;
  align-items: center;
  justify-content: center;
  transition: all 0.3s cubic-bezier(0.4, 0, 0.2, 1);
  background: #f8fafc;
}

.planning-board__cell--empty {
  background: #ffffff;
  border: 1px dashed #e2e8f0;
}

.planning-board__cell:hover:not(.planning-board__cell--empty) {
  transform: translateY(-4px);
  box-shadow: 0 12px 20px -8px rgba(0, 0, 0, 0.1);
  z-index: 10;
}

.planning-board__cell-main {
  width: 100%;
  height: 100%;
  padding: 12px;
  display: flex;
  flex-direction: column;
  justify-content: center;
  align-items: center;
  text-align: center;
  gap: 6px;
  border-radius: 16px;
  cursor: grab;
}

.planning-board__cell-main:active {
  cursor: grabbing;
}

.planning-board__cell-main strong {
  font-size: 13px;
  font-weight: 700;
  line-height: 1.4;
  overflow: hidden;
  display: -webkit-box;
  -webkit-line-clamp: 2;
  -webkit-box-orient: vertical;
}

.planning-board__cell-main span {
  font-size: 11px;
  opacity: 0.8;
}

/* 颜色系统 */
.planning-board__cell-main--course {
  background: #e0f2fe;
  color: #0369a1;
}

.planning-board__cell-main--task {
  background: #dcfce7;
  color: #15803d;
}

/* 进场动画 */
@keyframes board-item-spring {
  0% { opacity: 0; transform: scale(0.6) translateY(20px); }
  60% { opacity: 1; transform: scale(1.05) translateY(-2px); }
  100% { opacity: 1; transform: scale(1) translateY(0); }
}

.board-item-pop {
  animation: board-item-spring 0.6s cubic-bezier(0.34, 1.56, 0.64, 1) both;
}

/* 弹窗动画 */
.modal-enter-active,
.modal-leave-active {
  transition: opacity 0.4s ease;
}

.modal-enter-from,
.modal-leave-to {
  opacity: 0;
}

.modal-enter-active .schedule-modal {
  animation: modal-in 0.5s cubic-bezier(0.16, 1, 0.3, 1);
}

.modal-leave-active .schedule-modal {
  animation: modal-in 0.3s cubic-bezier(0.7, 0, 0.84, 0) reverse;
}

@keyframes modal-in {
  from {
    transform: scale(0.95) translateY(30px);
    opacity: 0;
  }
  to {
    transform: scale(1) translateY(0);
    opacity: 1;
  }
}
</style>

<!-- 全局样式：用于拦截挂载在 body 上的弹窗组件 -->
<style>
.premium-msg-box {
  --el-messagebox-width: 420px;
  border-radius: 24px !important;
  border: 1px solid rgba(255, 255, 255, 0.6) !important;
  padding: 12px !important;
  background: rgba(255, 255, 255, 0.85) !important;
  backdrop-filter: blur(25px) saturate(180%) !important;
  box-shadow: 0 25px 50px -12px rgba(0, 0, 0, 0.2) !important;
  transition: all 0.3s cubic-bezier(0.4, 0, 0.2, 1);
}

.premium-msg-box .el-message-box__header {
  padding: 24px 28px 10px !important;
}

.premium-msg-box .el-message-box__title {
  font-size: 20px !important;
  font-weight: 900 !important;
  color: #0f172a !important;
  letter-spacing: -0.02em !important;
}

.premium-msg-box .el-message-box__content {
  padding: 10px 28px 24px !important;
  color: #64748b !important;
  font-size: 14px !important;
  line-height: 1.7 !important;
}

.premium-msg-box .el-message-box__btns {
  padding: 16px 24px 24px !important;
}

.premium-msg-box .el-button {
  height: 44px !important;
  padding: 0 24px !important;
  border-radius: 14px !important;
  font-weight: 700 !important;
  transition: all 0.2s cubic-bezier(0.4, 0, 0.2, 1) !important;
  font-size: 14px !important;
}

.premium-msg-box .el-button--primary {
  background: #0f172a !important;
  border: none !important;
  box-shadow: 0 4px 12px rgba(15, 23, 42, 0.2) !important;
}

.premium-msg-box .el-button--primary:hover {
  background: #1e293b !important;
  transform: translateY(-2px) !important;
  box-shadow: 0 8px 20px rgba(15, 23, 42, 0.25) !important;
}

.premium-msg-box .el-button--default:not(.el-button--primary) {
  background: #f1f5f9 !important;
  border: none !important;
  color: #475569 !important;
}

.premium-msg-box .el-button--default:not(.el-button--primary):hover {
  background: #e2e8f0 !important;
  color: #1e293b !important;
}
</style>
