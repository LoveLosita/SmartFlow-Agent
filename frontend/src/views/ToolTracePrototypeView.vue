<script setup lang="ts">
import { onBeforeUnmount, onMounted, reactive, ref } from 'vue'

interface BaselineLine {
  id: string
  atMs: number
  text: string
}

type ToolLineState = 'called' | 'create' | 'blocked'

interface EnhancedLine {
  id: string
  atMs: number
  type: 'text' | 'tool'
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
]

const baselineLines = ref<BaselineLine[]>([])
const enhancedLines = ref<EnhancedLine[]>([])
const isReplaying = ref(false)
const replayRound = ref(0)
const expandedToolLineMap = reactive<Record<string, boolean>>({})

const timerHandles = new Set<number>()

function clearReplayTimers() {
  for (const handle of timerHandles) {
    window.clearTimeout(handle)
  }
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
                  v-else
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
              </template>

              <p v-if="enhancedLines.length <= 0" class="proto-line proto-line--placeholder">正在生成回复...</p>
            </div>
          </div>
        </div>
      </article>
    </section>
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
</style>
