<script setup lang="ts">
import { computed } from 'vue'
import type { TimelineToolPayload, ToolView } from '@/api/schedule_agent'

const props = defineProps<{
  payload: TimelineToolPayload
  expanded: boolean
}>()

const emit = defineEmits<{
  (e: 'toggle'): void
}>()

function getStatusLabel(status: string) {
  const map: Record<string, string> = {
    start: '进行中',
    done: '已完成',
    failed: '失败',
    blocked: '已拦截',
  }
  return map[status] || status
}

// 模拟原有的 getOperationLabel 逻辑（如果后端没传标签）
function getOperationFallbackLabel(op: string) {
  const map: Record<string, string> = {
    move: '移动',
    place: '放置',
    swap: '交换',
    batch_move: '批量移动',
    unplace: '取消放置',
    queue_apply_head_move: '队列首项确认',
  }
  return map[op] || op
}
</script>

<template>
  <article
    class="tool-card"
    :class="[
      `tool-card--${payload.status}`,
      { 'tool-card--expanded': expanded },
    ]"
  >
    <!-- 1. 折叠态头部 (优先取 result_view.collapsed) -->
    <header class="tool-card__header" @click="emit('toggle')">
      <div class="tool-card__icon-box">
        <svg v-if="payload.status === 'failed'" width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round" stroke-linejoin="round">
          <circle cx="12" cy="12" r="10"></circle>
          <line x1="15" y1="9" x2="9" y2="15"></line>
          <line x1="9" y1="9" x2="15" y2="15"></line>
        </svg>
        <svg v-else width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round" stroke-linejoin="round">
          <path d="M14.7 6.3a1 1 0 0 0 0 1.4l1.6 1.6a1 1 0 0 0 1.4 0l3.77-3.77a6 6 0 0 1-7.94 7.94l-6.91 6.91a2.12 2.12 0 0 1-3-3l6.91-6.91a6 6 0 0 1 7.94-7.94l-3.76 3.76z" />
        </svg>
      </div>

      <div class="tool-card__title-group">
        <div class="tool-card__title-row">
          <h3 class="tool-card__title">
            {{ payload.result_view?.collapsed?.title || payload.summary }}
          </h3>
          <span class="tool-card__badge">
            {{ payload.result_view?.collapsed?.status_label || getStatusLabel(payload.status) }}
          </span>
        </div>
        <p class="tool-card__subtitle">
          {{ payload.result_view?.collapsed?.subtitle || payload.arguments_preview }}
        </p>
      </div>

      <!-- 简短指标区 (优先读取 result_view.collapsed.metrics) -->
      <div v-if="payload.result_view?.collapsed?.metrics" class="tool-card__metrics">
        <div v-for="(m, mi) in payload.result_view.collapsed.metrics" :key="mi" class="metric-item">
          <span class="metric-value">{{ m.value }}</span>
          <span class="metric-label">{{ m.label }}</span>
        </div>
      </div>
      <div v-else-if="!expanded && payload.result_view?.collapsed?.operation_label" class="tool-card__metrics">
        <div class="metric-item">
          <span class="metric-label">{{ payload.result_view.collapsed.operation_label }}</span>
        </div>
      </div>

      <div class="tool-card__chevron" :class="{ 'tool-card__chevron--expanded': expanded }">
        <svg width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round" stroke-linejoin="round">
          <polyline points="6 9 12 15 18 9"></polyline>
        </svg>
      </div>
    </header>

    <!-- 2. 展开态详情 -->
    <transition name="tool-expand">
      <section v-if="expanded" class="tool-card__content">
        <div class="tool-card__divider"></div>

        <!-- 2.1 参数展示 (如果有 argument_view) -->
        <div v-if="payload.argument_view" class="section-block view-arguments">
          <details class="arguments-details">
            <summary class="arguments-summary">
              <span class="summary-label">输入参数</span>
              <span class="summary-preview">{{ payload.argument_view.collapsed?.summary }}</span>
            </summary>
            <div class="kv-grid kv-grid--arguments">
              <div v-for="field in payload.argument_view.expanded?.fields" :key="field.key" class="kv-item">
                <span class="kv-label">{{ field.label }}</span>
                <span class="kv-value">{{ field.display || field.value }}</span>
              </div>
            </div>
          </details>
        </div>

        <!-- 2.2 结果渲染: schedule.operation_result -->
        <div v-if="payload.result_view?.view_type === 'schedule.operation_result'" class="section-block view-operation">
          <h4 class="detail-section-title">操作变更明细</h4>
          
          <!-- 影响日期汇总 -->
          <div v-if="payload.result_view.expanded?.affected_days_label" class="affected-days-box">
             <span class="affected-label">影响日期：</span>
             <span class="affected-value">{{ payload.result_view.expanded.affected_days_label }}</span>
          </div>

          <div v-if="payload.result_view.expanded?.changes?.length" class="changes-list">
            <div v-for="(change, idx) in payload.result_view.expanded.changes" :key="idx" class="change-item">
              <div class="change-item__header">
                <span class="change-item__task-icon"></span>
                <span class="change-item__task-name">{{ change.task_label }}</span>
                <span v-if="change.status_label" class="change-item__status-tag">{{ change.status_label }}</span>
              </div>
 
              <div class="change-item__path">
                <div class="slot-box slot-box--before">
                  <span class="slot-tag">之前</span>
                  <div class="slot-text">{{ change.before_label || '未排程' }}</div>
                </div>
                <div class="path-arrow">
                  <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round" stroke-linejoin="round">
                    <line x1="5" y1="12" x2="19" y2="12"></line>
                    <polyline points="12 5 19 12 12 19"></polyline>
                  </svg>
                </div>
                <div class="slot-box slot-box--after">
                  <span class="slot-tag">之后</span>
                  <div class="slot-text">{{ change.after_label || '未排程' }}</div>
                </div>
              </div>
            </div>
          </div>
 
          <!-- 队列快照 (带标签) -->
          <div v-if="payload.result_view.expanded?.queue_snapshot" class="queue-snapshot">
            <h5 class="sub-section-title">{{ payload.result_view.expanded.queue_snapshot.summary_label || '队列变更' }}</h5>
            <div class="queue-compare">
              <div class="queue-side">
                <span class="queue-count">{{ payload.result_view.expanded.queue_snapshot.before_label }}</span>
              </div>
              <div class="queue-arrow"></div>
              <div class="queue-side">
                <span class="queue-count highlight">{{ payload.result_view.expanded.queue_snapshot.after_label }}</span>
              </div>
            </div>
          </div>
 
          <!-- 失败信息 -->
          <div v-if="payload.result_view.expanded?.failure_reason" class="failure-box">
            <span class="failure-icon">!</span>
            <p class="failure-text">{{ payload.result_view.expanded.failure_reason }}</p>
          </div>
        </div>

        <!-- 2.3 结果渲染: 结构化渲染 (read_result, analysis_result, search_result, fetch_result, write_result, context_result) -->
        <div v-else-if="[
          'schedule.read_result', 
          'schedule.analysis_result', 
          'web.search_result', 
          'web.fetch_result', 
          'taskclass.write_result', 
          'tool.context_result'
        ].includes(payload.result_view?.view_type || '')" class="section-block view-read">
          <!-- 优先展示 sections -->
          <template v-if="payload.result_view?.expanded?.sections?.length">
            <div v-for="(section, sidx) in payload.result_view.expanded.sections" :key="sidx" class="read-section">
              <!-- 1. items 类型: 渲染列表 -->
              <div v-if="section.type === 'items'" class="read-section__items">
                 <h4 v-if="section.title" class="detail-section-title">{{ section.title }}</h4>
                 <p v-if="section.summary" class="section-summary">{{ section.summary }}</p>
                 <div class="items-list">
                    <div v-for="(item, iidx) in section.items" :key="iidx" class="list-item-card">
                       <div class="item-main">
                          <div class="item-title">{{ item.title }}</div>
                          <div class="item-subtitle" v-if="item.subtitle">{{ item.subtitle }}</div>
                       </div>
                       <div class="item-tags" v-if="item.tags?.length">
                          <span v-for="tag in item.tags" :key="tag" class="item-tag">{{ tag }}</span>
                       </div>
                       <div class="item-details" v-if="item.detail_lines?.length">
                          <p v-for="line in item.detail_lines" :key="line" class="detail-line">{{ line }}</p>
                       </div>
                    </div>
                 </div>
              </div>

              <!-- 2. kv 类型: 渲染字段列表 -->
              <div v-else-if="section.type === 'kv'" class="read-section__kv">
                 <h4 v-if="section.title" class="detail-section-title">{{ section.title }}</h4>
                 <div class="kv-grid">
                    <div v-for="(field, fidx) in section.fields" :key="fidx" class="kv-item">
                       <span class="kv-label">{{ field.label }}</span>
                       <span class="kv-value">{{ field.value }}</span>
                    </div>
                 </div>
              </div>

              <!-- 3. callout 类型: 渲染提示块 -->
              <div v-else-if="section.type === 'callout'" class="read-section__callout" :class="`tone--${section.tone || 'info'}`">
                 <div class="callout-header">
                    <span class="callout-title">{{ section.title }}</span>
                 </div>
                 <p v-if="section.summary || section.subtitle" class="callout-summary">{{ section.summary || section.subtitle }}</p>
                 <div v-if="section.detail_lines?.length" class="callout-details">
                    <p v-for="line in section.detail_lines" :key="line" class="detail-line">{{ line }}</p>
                 </div>
              </div>

              <!-- 4. 降级展示: 未知类型 -->
              <div v-else class="read-section__fallback">
                 <h4 v-if="section.title" class="detail-section-title">{{ section.title }}</h4>
                 <p v-if="section.summary" class="section-summary">{{ section.summary }}</p>
                 <div v-if="section.detail_lines?.length" class="fallback-details">
                    <p v-for="line in section.detail_lines" :key="line" class="detail-line">{{ line }}</p>
                 </div>
              </div>
            </div>
          </template>

          <!-- 如果没有 sections，尝试展示 items -->
          <template v-else-if="payload.result_view?.expanded?.items?.length">
             <div class="read-section__items">
                <h4 class="detail-section-title">结果列表</h4>
                <div class="items-list">
                   <div v-for="(item, iidx) in payload.result_view.expanded.items" :key="iidx" class="list-item-card">
                      <div class="item-main">
                         <div class="item-title">{{ item.title }}</div>
                         <div class="item-subtitle" v-if="item.subtitle">{{ item.subtitle }}</div>
                      </div>
                      <div class="item-tags" v-if="item.tags?.length">
                         <span v-for="tag in item.tags" :key="tag" class="item-tag">{{ tag }}</span>
                      </div>
                      <div v-if="item.detail_lines?.length" class="item-details">
                         <p v-for="line in item.detail_lines" :key="line" class="detail-line">{{ line }}</p>
                      </div>
                   </div>
                </div>
             </div>
          </template>
        </div>

        <!-- 2.4 结果渲染: legacy_text -->
        <div v-else-if="payload.result_view?.view_type === 'legacy_text'" class="section-block view-legacy">
          <h4 class="detail-section-title">{{ payload.result_view.expanded?.raw_text_label || '输出内容' }}</h4>
          <div class="raw-text-container">
            <pre class="raw-text">{{ payload.result_view.expanded?.raw_text }}</pre>
          </div>
        </div>

        <!-- 2.5 未知协议展示: 只要有 collapsed 就显示基础信息 -->
        <div v-else-if="payload.result_view?.collapsed" class="section-block view-unknown">
           <h4 class="detail-section-title">结果详情 (未知协议)</h4>
           <div class="unknown-content">
              <p class="unknown-subtitle">{{ payload.result_view.collapsed.subtitle }}</p>
              <div v-if="payload.result_view.collapsed.metrics" class="unknown-metrics">
                 <div v-for="(m, mi) in payload.result_view.collapsed.metrics" :key="mi" class="metric-chip">
                    <span class="chip-label">{{ m.label }}:</span>
                    <span class="chip-value">{{ m.value }}</span>
                 </div>
              </div>
           </div>
        </div>

        <!-- 2.6 旧协议兜底 -->
        <div v-else-if="!payload.result_view" class="section-block view-old-fallback">
           <h4 class="detail-section-title">工具输出 (兼容模式)</h4>
           <div class="fallback-summary-box">
              <p class="fallback-summary">{{ payload.summary }}</p>
              <div v-if="payload.arguments_preview" class="fallback-json-box">
                 <span class="json-label">调用参数：</span>
                 <code>{{ payload.arguments_preview }}</code>
              </div>
           </div>
        </div>

        <!-- 原始 Observation (仅开发/调试可见入口, 默认收起) -->
        <details v-if="payload.result_view?.expanded?.raw_text" class="debug-details">
          <summary>调试信息 (RAW Observation)</summary>
          <pre class="debug-raw-pre">{{ payload.result_view.expanded.raw_text }}</pre>
        </details>
      </section>
    </transition>
  </article>
</template>

<style scoped>
/* Tool Card Styles */
.tool-card {
  background: #ffffff;
  border: 1px solid #eef2f6;
  border-radius: 16px;
  overflow: hidden;
  overflow-x: hidden;
  transition: border-color 0.3s, box-shadow 0.3s, transform 0.3s, background-color 0.3s;
  box-shadow: 0 4px 6px -1px rgba(0, 0, 0, 0.02);
  margin: 8px 0;
  width: 100%;
  box-sizing: border-box;
  min-width: 0;
  overflow-wrap: break-word;
}

.tool-card:hover {
  border-color: #d1d5db;
  transform: translateY(-1px);
}

.tool-card--expanded {
  border-color: #3b82f6;
  box-shadow: 0 8px 16px -4px rgba(59, 130, 246, 0.08);
}

.tool-card--failed {
  border-left: 4px solid #f43f5e;
}

.tool-card--done {
  border-left: 4px solid #10b981;
}

.tool-card__header {
  padding: 12px 16px;
  display: flex;
  align-items: center;
  gap: 12px;
  cursor: pointer;
  user-select: none;
  width: 100%;
  box-sizing: border-box;
  min-width: 0;
}

.tool-card__icon-box {
  width: 32px;
  height: 32px;
  background: #f8fafc;
  border-radius: 10px;
  display: flex;
  align-items: center;
  justify-content: center;
  color: #64748b;
  flex-shrink: 0;
  border: 1px solid #f1f5f9;
}

.tool-card--done .tool-card__icon-box {
  color: #10b981;
  background: #f0fdf4;
  border-color: #dcfce7;
}

.tool-card--failed .tool-card__icon-box {
  color: #f43f5e;
  background: #fff1f2;
  border-color: #fee2e2;
}

.tool-card__title-group {
  flex: 1;
  min-width: 0;
}

.tool-card__title-row {
  display: flex;
  align-items: center;
  gap: 8px;
  margin-bottom: 1px;
}

.tool-card__title {
  margin: 0;
  font-size: 14px;
  font-weight: 700;
  color: #0f172a;
}

.tool-card__badge {
  font-size: 10px;
  font-weight: 600;
  padding: 1px 8px;
  border-radius: 6px;
  background: #f1f5f9;
  color: #64748b;
}

.tool-card--done .tool-card__badge {
  background: #ecfdf5;
  color: #059669;
}

.tool-card--failed .tool-card__badge {
  background: #fff1f2;
  color: #e11d48;
}

.tool-card__subtitle {
  margin: 0;
  font-size: 12px;
  color: #94a3b8;
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}

.tool-card__metrics {
  display: flex;
  gap: 10px;
  margin-left: 8px;
}

.metric-item {
  display: flex;
  flex-direction: column;
  align-items: flex-end;
}

.metric-value {
  font-size: 13px;
  font-weight: 800;
  color: #334155;
  line-height: 1;
}

.metric-label {
  font-size: 9px;
  color: #94a3b8;
  font-weight: 600;
}

.tool-card__chevron {
  width: 20px;
  height: 20px;
  display: flex;
  align-items: center;
  justify-content: center;
  color: #cbd5e1;
  transition: transform 0.3s;
}

.tool-card__chevron--expanded {
  transform: rotate(180deg);
  color: #3b82f6;
}

/* Content */
.tool-card__content {
  padding: 0 16px 20px;
  min-width: 0;
  width: 100%;
  box-sizing: border-box;
  overflow: hidden;
}

.tool-card__divider {
  height: 1px;
  background: #f1f5f9;
  margin-bottom: 16px;
}

.section-block + .section-block {
  margin-top: 20px;
}

.detail-section-title {
  margin: 0 0 10px;
  font-size: 11px;
  font-weight: 800;
  color: #94a3b8;
  text-transform: uppercase;
  letter-spacing: 0.05em;
}

/* Arguments Section */
.view-arguments {
  margin-bottom: 16px;
  padding: 0 4px;
}

.arguments-details {
  border: 1px solid #f1f5f9;
  border-radius: 10px;
  background: #f8fafc;
  overflow: hidden;
}

.arguments-summary {
  padding: 10px 12px;
  cursor: pointer;
  display: flex;
  align-items: center;
  gap: 8px;
  list-style: none;
  user-select: none;
}

.arguments-summary::-webkit-details-marker {
  display: none;
}

.arguments-summary .summary-label {
  font-size: 12px;
  font-weight: 600;
  color: #64748b;
  background: #fff;
  padding: 2px 6px;
  border-radius: 4px;
  border: 1px solid #e2e8f0;
}

.arguments-summary .summary-preview {
  flex: 1;
  font-size: 12px;
  color: #94a3b8;
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}

.kv-grid--arguments {
  padding: 8px 12px 12px;
  border-top: 1px solid #f1f5f9;
  background: #fff;
}

/* Operation Changes */
.changes-list {
  display: flex;
  flex-direction: column;
  gap: 12px;
}

.change-item {
  background: #ffffff;
  border: 1px solid #f1f5f9;
  border-radius: 12px;
  padding: 12px;
}

.change-item__header {
  display: flex;
  align-items: center;
  gap: 6px;
  margin-bottom: 10px;
}

.change-item__task-icon {
  width: 6px;
  height: 6px;
  background: #3b82f6;
  border-radius: 2px;
}

.change-item__task-name {
  font-size: 13px;
  font-weight: 700;
  color: #0f172a;
  word-break: break-all;
}

.change-item__status-tag {
  font-size: 10px;
  color: #2563eb;
  background: #eff6ff;
  padding: 0px 6px;
  border-radius: 4px;
  font-weight: 500;
}

.change-item__path {
  display: flex;
  align-items: center;
  gap: 10px;
}

.slot-box {
  flex: 1;
  padding: 8px 10px;
  border-radius: 10px;
  min-width: 0;
  overflow: hidden;
}

.slot-box--before {
  background: #fdfdfd;
  border: 1px dashed #e2e8f0;
  color: #64748b;
}

.slot-box--after {
  background: #f0f7ff;
  border: 1px solid #dbeafe;
  color: #1e40af;
}

.slot-tag {
  display: block;
  font-size: 9px;
  font-weight: 700;
  margin-bottom: 2px;
  opacity: 0.6;
}

.slot-text {
  font-size: 12px;
  font-weight: 600;
  word-break: break-all;
}

/* Queue Snapshot */
.sub-section-title {
  margin: 0 0 10px;
  font-size: 11px;
  font-weight: 700;
  color: #64748b;
}

.queue-snapshot {
  margin-top: 16px;
  padding: 12px;
  background: #f8fafc;
  border: 1px solid #eef2f6;
  border-radius: 12px;
}

.queue-compare {
  display: flex;
  align-items: center;
  gap: 16px;
}

.queue-side {
  flex: 1;
}

.queue-count {
  font-size: 13px;
  font-weight: 700;
  color: #64748b;
}

.queue-count.highlight {
  color: #10b981;
}

/* Operation View Styles */
.affected-days-box {
  margin: -4px 0 16px;
  padding: 10px 14px;
  background: #f8fafc;
  border-radius: 12px;
  border: 1px solid #f1f5f9;
  display: flex;
  align-items: center;
  gap: 8px;
}

.affected-label {
  font-size: 11px;
  font-weight: 700;
  color: #94a3b8;
}

.affected-value {
  font-size: 12px;
  font-weight: 600;
  color: #334155;
}

.path-arrow {
  color: #cbd5e1;
  display: flex;
  align-items: center;
  justify-content: center;
  width: 24px;
}

.tool-card--expanded .path-arrow {
  color: #3b82f6;
  filter: drop-shadow(0 0 4px rgba(59, 130, 246, 0.2));
}

.queue-arrow {
  flex: 2;
  height: 2px;
  background: #e2e8f0;
  position: relative;
}

.queue-arrow::after {
  content: '';
  position: absolute;
  right: 0;
  top: 50%;
  transform: translateY(-50%);
  border-left: 6px solid #e2e8f0;
  border-top: 4px solid transparent;
  border-bottom: 4px solid transparent;
}

/* Legacy Text */
.raw-text-container {
  background: #1e293b;
  border-radius: 12px;
  padding: 12px;
}

.raw-text {
  margin: 0;
  font-family: 'JetBrains Mono', monospace;
  font-size: 12px;
  line-height: 1.5;
  color: #e2e8f0;
  white-space: pre-wrap;
}

/* Fallback Old */
.fallback-summary-box {
  padding: 12px;
  background: #fefce8;
  border: 1px solid #fef3c7;
  border-radius: 12px;
}

.fallback-summary {
  font-size: 13px;
  color: #92400e;
  font-weight: 600;
  margin: 0 0 6px;
}

.fallback-json-box {
  font-size: 11px;
  color: #d97706;
}

/* Failure */
.failure-box {
  margin-top: 14px;
  padding: 12px;
  background: #fff1f2;
  border: 1px solid #fee2e2;
  border-radius: 12px;
  display: flex;
  gap: 10px;
}

.failure-icon {
  width: 18px;
  height: 18px;
  background: #f43f5e;
  color: #fff;
  border-radius: 50%;
  display: flex;
  align-items: center;
  justify-content: center;
  font-size: 11px;
  font-weight: 900;
  flex-shrink: 0;
}

.failure-text {
  margin: 0;
  font-size: 12px;
  color: #9f1239;
  font-weight: 500;
  line-height: 1.5;
}

/* Debug */
.debug-details {
  margin-top: 24px;
  border-top: 1px solid #f1f5f9;
  padding-top: 12px;
}

.debug-details summary {
  font-size: 11px;
  color: #cbd5e1;
  cursor: pointer;
}

.debug-raw-pre {
  margin-top: 10px;
  font-size: 10px;
  padding: 10px;
  border-radius: 8px;
  background: #f8fafc;
  color: #94a3b8;
  max-height: 120px;
  overflow-y: auto;
}

/* Read Result Styles */
.read-section + .read-section {
  margin-top: 24px;
}

.section-summary {
  font-size: 13px;
  color: #475569;
  line-height: 1.6;
  margin: -4px 0 12px;
}

.items-list {
  display: flex;
  flex-direction: column;
  gap: 10px;
}

.list-item-card {
  background: #f8fafc;
  border: 1px solid #f1f5f9;
  border-radius: 12px;
  padding: 12px 14px;
  transition: all 0.2s ease;
  min-width: 0;
  box-sizing: border-box;
}

.list-item-card:hover {
  background: #f1f5f9;
  border-color: #e2e8f0;
}

.item-main {
  display: flex;
  flex-direction: column;
  gap: 2px;
}

.item-title {
  font-size: 14px;
  font-weight: 700;
  color: #1e293b;
  word-break: break-all;
}

.item-subtitle {
  font-size: 12px;
  color: #64748b;
  font-weight: 500;
}

.item-tags {
  display: flex;
  flex-wrap: wrap;
  gap: 6px;
  margin-top: 8px;
}

.item-tag {
  font-size: 10px;
  font-weight: 600;
  color: #3b82f6;
  background: #eff6ff;
  padding: 1px 8px;
  border-radius: 6px;
  border: 1px solid #dbeafe;
}

.item-details {
  margin-top: 10px;
  padding-top: 10px;
  border-top: 1px dashed #e2e8f0;
  display: flex;
  flex-direction: column;
  gap: 4px;
}

.detail-line {
  font-size: 12px;
  color: #475569;
  margin: 0;
  line-height: 1.5;
  word-break: break-word;
}

/* KV Grid */
.kv-grid {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(min(100%, 140px), 1fr));
  gap: 12px;
  background: #f8fafc;
  padding: 16px;
  border-radius: 14px;
  border: 1px solid #f1f5f9;
}

.kv-item {
  display: flex;
  flex-direction: column;
  gap: 2px;
  min-width: 0;
  overflow: hidden;
}

.kv-label {
  font-size: 10px;
  font-weight: 700;
  color: #94a3b8;
  text-transform: uppercase;
  letter-spacing: 0.02em;
}

.kv-value {
  flex: 1;
  min-width: 0;
  font-size: 13px;
  color: #1e293b;
  font-weight: 500;
  text-align: left;
  word-break: break-word;
  line-height: 1.4;
}

/* Callout */
.read-section__callout {
  padding: 14px 16px;
  border-radius: 14px;
  border-left: 4px solid #cbd5e1;
  background: #f8fafc;
  min-width: 0;
  box-sizing: border-box;
}

.read-section__callout.tone--info {
  background: #f0f9ff;
  border-color: #3b82f6;
}

.read-section__callout.tone--info .callout-title { color: #0369a1; }
.read-section__callout.tone--info .callout-summary { color: #0c4a6e; }

.read-section__callout.tone--warning {
  background: #fffbeb;
  border-color: #f59e0b;
}

.read-section__callout.tone--warning .callout-title { color: #b45309; }
.read-section__callout.tone--warning .callout-summary { color: #78350f; }

.read-section__callout.tone--danger {
  background: #fef2f2;
  border-color: #ef4444;
}

.read-section__callout.tone--danger .callout-title { color: #b91c1c; }
.read-section__callout.tone--danger .callout-summary { color: #7f1d1d; }

.callout-header {
  margin-bottom: 4px;
}

.callout-title {
  font-size: 13px;
  font-weight: 800;
  word-break: break-word;
}

.callout-summary {
  font-size: 12px;
  margin: 0;
  line-height: 1.5;
  font-weight: 500;
  word-break: break-word;
}

.callout-details {
  margin-top: 8px;
  padding-top: 8px;
  border-top: 1px solid rgba(0,0,0,0.05);
}

/* Unknown View Styles */
.unknown-subtitle {
  font-size: 13px;
  color: #475569;
  margin-bottom: 12px;
  line-height: 1.5;
}

.unknown-metrics {
  display: flex;
  flex-wrap: wrap;
  gap: 8px;
}

.metric-chip {
  background: #f1f5f9;
  padding: 4px 10px;
  border-radius: 8px;
  font-size: 11px;
  display: flex;
  gap: 4px;
}

.chip-label {
  color: #64748b;
  font-weight: 600;
}

.chip-value {
  color: #0f172a;
  font-weight: 700;
}

/* Animations */
.tool-expand-enter-active,
.tool-expand-leave-active {
  transition: all 0.3s cubic-bezier(0.4, 0, 0.2, 1);
  max-height: 1000px;
}

.tool-expand-enter-from,
.tool-expand-leave-to {
  max-height: 0;
  opacity: 0;
  overflow: hidden;
}
</style>
