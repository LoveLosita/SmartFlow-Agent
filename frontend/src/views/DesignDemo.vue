<script setup lang="ts">
import { ref } from 'vue'

// --- 数据结构定义 ---
interface Task {
  id: string
  title: string
  priority_group: 1 | 2 | 3 | 4
  deadline_at?: string
  is_completed: boolean
}

// --- 四象限元数据 (深度对齐首页提示词 & 视觉) ---
const quadMeta: any = {
  1: { title: '重要且紧急', caption: '优先处理', tone: 'danger', bg: 'linear-gradient(180deg, #fff1f2 0%, #fff7f7 100%)', text: '#ef4444' },
  2: { title: '重要不紧急', caption: '持续推进', tone: 'primary', bg: 'linear-gradient(180deg, #eef7ff 0%, #f7fbff 100%)', text: '#3b82f6' },
  3: { title: '简单不重要', caption: '顺手完成', tone: 'warning', bg: 'linear-gradient(180deg, #fff8df 0%, #fffdf1 100%)', text: '#f59e0b' },
  4: { title: '不简单不重要', caption: '谨慎投入', tone: 'slate', bg: 'linear-gradient(180deg, #f2f5fb 0%, #f8fafc 100%)', text: '#64748b' }
}

// --- 卡片模拟数据 ---
const cardData = {
  query: {
    query: '我第一象限里还有哪些事情？',
    group: 1 as const,
    tasks: [
      { id: '1', title: '修复生产环境登录异常', priority_group: 1, deadline_at: '2024-05-20 09:00', is_completed: false },
      { id: '2', title: '提交年度安全审计报告', priority_group: 1, deadline_at: '今天 18:00', is_completed: false },
      { id: '3', title: '确认猎选系统的集成计划', priority_group: 1, deadline_at: '明天', is_completed: false }
    ] as Task[]
  },
  receipt: {
    title: '联系供应商确认物料进度',
    group: 2 as const,
    id: 'TASK-520',
    created_at: '刚才'
  }
}

// --- 交互控制 ---
const activeView = ref<'query' | 'receipt'>('query')
const currentTone = ref<'danger' | 'primary' | 'warning' | 'slate'>('danger')

const switchTone = (tone: any) => {
  currentTone.value = tone
  // 模拟不同象限的查询结果
  const toneToGroup: any = { danger: 1, primary: 2, warning: 3, slate: 4 }
  cardData.query.group = toneToGroup[tone]
  cardData.receipt.group = toneToGroup[tone]
}
</script>

<template>
  <div class="design-demo-page">
    <div class="page-background">
      <div class="shape shape-1"></div>
      <div class="shape shape-2"></div>
    </div>

    <div class="page-header">
      <div class="chip">UI Refined V3.0</div>
      <h1>业务卡片收敛方案</h1>
      <p>首页风格同步 · 软渐变不晃眼 · 语义对齐</p>
    </div>

    <div class="demo-wrapper">
      <!-- 预览控制台 -->
      <aside class="demo-sidebar">
        <div class="sidebar-block">
          <h3>切换卡片类型</h3>
          <div class="view-btns">
            <button @click="activeView = 'query'" :class="{ active: activeView === 'query' }">查询记录</button>
            <button @click="activeView = 'receipt'" :class="{ active: activeView === 'receipt' }">创建回执</button>
          </div>
        </div>
        <div class="sidebar-block">
          <h3>模拟目标象限</h3>
          <div class="tone-btns">
            <button v-for="(v, k) in quadMeta" :key="k" @click="switchTone(v.tone)" :class="[v.tone, { active: currentTone === v.tone }]">
              {{ v.tone === 'danger' ? 'Q1' : v.tone === 'primary' ? 'Q2' : v.tone === 'warning' ? 'Q3' : 'Q4' }}
            </button>
          </div>
        </div>
      </aside>

      <!-- 画布区域 -->
      <main class="demo-canvas">
        <!-- 场景 A：任务查询结果 -->
        <div v-if="activeView === 'query'" class="card-stage" :key="'query-' + currentTone">
          <div class="card-label">预览：跨象限/单象限查询结果列表</div>
          <div class="chat-inline-mockup">
            <div class="business-card-final query-results" :style="{ background: quadMeta[cardData.query.group].bg }">
              <header class="card-header-final">
                <div class="header-left">
                  <p class="eyebrow">{{ quadMeta[cardData.query.group].caption }}</p>
                  <h3>{{ cardData.query.query }}</h3>
                </div>
                <div class="count-badge">找到 {{ cardData.query.tasks.length }} 项</div>
              </header>

              <div class="card-content-final">
                <div class="task-items-final">
                  <div v-for="task in cardData.query.tasks" :key="task.id" class="task-item-final">
                    <div class="item-check">
                      <div class="check-circle" :style="{ borderColor: quadMeta[cardData.query.group].text }"></div>
                    </div>
                    <div class="item-body">
                      <div class="item-title">{{ task.title }}</div>
                      <div class="item-meta">
                        <span class="q-pill" :style="{ color: quadMeta[cardData.query.group].text, background: quadMeta[cardData.query.group].text + '10' }">
                          Q{{ task.priority_group }} {{ quadMeta[task.priority_group].title }}
                        </span>
                        <span v-if="task.deadline_at" class="time-pill">
                          <svg width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><circle cx="12" cy="12" r="10"/><polyline points="12 6 12 12 16 14"/></svg>
                          {{ task.deadline_at }}
                        </span>
                      </div>
                    </div>
                  </div>
                </div>
                <button class="btn-more-final">查看完整任务列表</button>
              </div>
            </div>
          </div>
        </div>

        <!-- 场景 B：任务创建回执 -->
        <div v-else class="card-stage" :key="'receipt-' + currentTone">
          <div class="card-label">预览：任务创建成功的轻量回执</div>
          <div class="chat-inline-mockup">
            <div class="business-card-final creation-receipt" :style="{ background: quadMeta[cardData.receipt.group].bg }">
              <div class="receipt-inner">
                <div class="receipt-header-final">
                  <div class="success-ring-v3" :style="{ background: quadMeta[cardData.receipt.group].text + '20', color: quadMeta[cardData.receipt.group].text }">
                    <svg width="20" height="20" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="3"><polyline points="20 6 9 17 4 12"/></svg>
                  </div>
                  <div class="success-msg">
                    <strong>任务已由助手成功创建</strong>
                    <span>归类至：{{ quadMeta[cardData.receipt.group].title }}</span>
                  </div>
                </div>

                <div class="receipt-task-card">
                  <div class="task-card-title">{{ cardData.receipt.title }}</div>
                  <div class="task-card-footer">
                    <span class="task-id-final">ID: {{ cardData.receipt.id }}</span>
                    <span class="task-time-final">创建于今日 {{ cardData.receipt.created_at }}</span>
                  </div>
                </div>

                <div class="receipt-actions-final">
                  <button class="btn-action-outline">调整象限</button>
                  <button class="btn-action-fill" :style="{ background: quadMeta[cardData.receipt.group].text }">打开详情</button>
                </div>
              </div>
            </div>
          </div>
        </div>
      </main>
    </div>
  </div>
</template>

<style scoped>
.design-demo-page {
  padding: 80px 24px;
  background: #fdfdfe;
  min-height: 100vh;
  position: relative;
  overflow: hidden;
  font-family: 'Inter', -apple-system, sans-serif;
}

.page-background { position: fixed; inset: 0; z-index: -1; }
.shape { position: absolute; border-radius: 50%; filter: blur(80px); opacity: 0.1; }
.shape-1 { width: 500px; height: 500px; background: #3b82f6; top: -10%; left: -10%; }
.shape-2 { width: 400px; height: 400px; background: #f43f5e; bottom: -5%; right: -5%; }

.page-header { text-align: center; margin-bottom: 60px; }
.chip { display: inline-block; padding: 4px 12px; background: #f1f5f9; color: #475569; border-radius: 100px; font-size: 11px; font-weight: 800; margin-bottom: 12px; }
.page-header h1 { font-size: 32px; font-weight: 900; letter-spacing: -0.04em; color: #0f172a; margin-bottom: 8px; }
.page-header p { font-size: 16px; color: #64748b; font-weight: 500; }

.demo-wrapper { display: flex; gap: 48px; max-width: 1000px; margin: 0 auto; align-items: flex-start; }

.demo-sidebar { width: 200px; display: flex; flex-direction: column; gap: 32px; position: sticky; top: 80px; }
.sidebar-block h3 { font-size: 13px; font-weight: 800; color: #94a3b8; margin-bottom: 12px; text-transform: uppercase; letter-spacing: 0.05em; }
.view-btns, .tone-btns { display: flex; flex-direction: column; gap: 8px; }

.view-btns button, .tone-btns button { padding: 10px 14px; border: 1px solid #f1f5f9; background: white; border-radius: 12px; font-size: 13px; font-weight: 700; color: #475569; cursor: pointer; transition: all 0.2s; text-align: left; }
.view-btns button.active { background: #0f172a; color: white; border-color: #0f172a; }

.tone-btns { display: grid; grid-template-columns: 1fr 1fr; gap: 8px; }
.tone-btns button { text-align: center; }
.tone-btns button.danger.active { background: #fee2e2; color: #ef4444; border-color: #ef4444; }
.tone-btns button.primary.active { background: #dbeafe; color: #3b82f6; border-color: #3b82f6; }
.tone-btns button.warning.active { background: #fef3c7; color: #d97706; border-color: #d97706; }
.tone-btns button.slate.active { background: #f1f5f9; color: #475569; border-color: #475569; }

.demo-canvas { flex: 1; min-width: 0; }
.card-stage { animation: stage-in 0.5s cubic-bezier(0.34, 1.56, 0.64, 1) both; }
@keyframes stage-in { 0% { opacity: 0; transform: translateY(20px); } 100% { opacity: 1; transform: translateY(0); } }

.card-label { font-size: 12px; color: #94a3b8; margin-bottom: 12px; font-weight: 600; padding-left: 8px; }
.chat-inline-mockup { padding: 40px; background: rgba(255, 255, 255, 0.4); border-radius: 40px; border: 1px solid rgba(0,0,0,0.02); backdrop-filter: blur(20px); display: flex; justify-content: center; }

/* --- Final Business Card Refinement --- */
.business-card-final { width: 100%; max-width: 380px; border-radius: 28px; border: 1px solid rgba(17, 24, 39, 0.08); box-shadow: 0 4px 20px rgba(0,0,0,0.02); overflow: hidden; transition: all 0.3s; }
.business-card-final:hover { transform: translateY(-4px); box-shadow: 0 12px 40px rgba(15, 23, 42, 0.06); }

/* Header Sync with Homepage */
.card-header-final { padding: 24px 24px 16px; display: flex; justify-content: space-between; align-items: flex-start; }
.eyebrow { font-size: 11px; font-weight: 800; color: rgba(30, 41, 59, 0.5); text-transform: uppercase; letter-spacing: 0.1em; margin-bottom: 6px; }
.card-header-final h3 { font-size: 24px; font-weight: 850; color: #1e293b; margin: 0; line-height: 1.1; letter-spacing: -0.02em; }
.count-badge { padding: 4px 12px; background: rgba(255, 255, 255, 0.8); border-radius: 100px; font-size: 11px; font-weight: 700; color: #475569; box-shadow: 0 2px 8px rgba(0,0,0,0.02); }

/* Content List Sync */
.task-items-final { padding: 0 16px; display: flex; flex-direction: column; gap: 8px; }
.task-item-final { background: rgba(255, 255, 255, 0.95); border: 1px solid rgba(0,0,0,0.04); border-radius: 18px; padding: 14px 16px; display: flex; gap: 14px; align-items: center; }
.check-circle { width: 22px; height: 22px; border-radius: 50%; border: 2px solid #e2e8f0; }

.item-title { font-size: 15px; font-weight: 700; color: #122033; margin-bottom: 4px; }
.item-meta { display: flex; gap: 10px; align-items: center; }
.q-pill { font-size: 10px; font-weight: 800; padding: 1px 8px; border-radius: 4px; }
.time-pill { font-size: 10px; color: #94a3b8; display: flex; align-items: center; gap: 4px; font-weight: 500; }

.btn-more-final { width: calc(100% - 32px); margin: 16px 16px 20px; padding: 12px; border: none; background: rgba(255, 255, 255, 0.6); border-radius: 14px; font-size: 13px; font-weight: 800; color: #475569; cursor: pointer; transition: all 0.2s; }
.btn-more-final:hover { background: white; }

/* Receipt Card Refinement */
.receipt-inner { padding: 24px; display: flex; flex-direction: column; gap: 20px; }
.receipt-header-final { display: flex; gap: 14px; align-items: center; }
.success-ring-v3 { width: 44px; height: 44px; border-radius: 50%; display: flex; align-items: center; justify-content: center; flex-shrink: 0; }
.success-msg { display: flex; flex-direction: column; }
.success-msg strong { font-size: 15px; font-weight: 850; color: #0f172a; }
.success-msg span { font-size: 12px; color: #64748b; font-weight: 500; }

.receipt-task-card { background: rgba(255, 255, 255, 0.95); border: 1px solid rgba(0,0,0,0.03); border-radius: 20px; padding: 20px; box-shadow: 0 4px 12px rgba(0,0,0,0.01); }
.task-card-title { font-size: 17px; font-weight: 800; color: #1e293b; margin-bottom: 12px; line-height: 1.4; }
.task-card-footer { display: flex; justify-content: space-between; font-size: 11px; font-weight: 600; color: #94a3b8; border-top: 1px solid #f1f5f9; padding-top: 10px; }

.receipt-actions-final { display: grid; grid-template-columns: 1fr 1fr; gap: 10px; }
.btn-action-outline { height: 42px; border: 1px solid #e2e8f0; background: white; border-radius: 12px; font-size: 13px; font-weight: 750; color: #475569; cursor: pointer; }
.btn-action-fill { height: 42px; border: none; border-radius: 12px; color: white; font-size: 13px; font-weight: 800; cursor: pointer; box-shadow: 0 8px 20px rgba(0,0,0,0.1); }

/* Responsive */
@media (max-width: 800px) {
  .demo-wrapper { flex-direction: column; }
  .demo-sidebar { width: 100%; position: static; gap: 20px; }
  .tone-btns { grid-template-columns: repeat(4, 1fr); }
  .chat-inline-mockup { padding: 20px; border-radius: 24px; }
}
</style>
