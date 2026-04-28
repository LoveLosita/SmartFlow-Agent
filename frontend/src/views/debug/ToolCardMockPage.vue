<script setup lang="ts">
import { ref } from 'vue'
import ToolCardRenderer from '@/components/dashboard/ToolCardRenderer.vue'

// 后端真实结构全家桶 mock
const mockData = [
  {
    "tool": "web_search",
    "status": "done",
    "success": true,
    "summary": "找到 2 条网页结果",
    "arguments_preview": "查询内容：高中数学学习方法，结果上限：2",
    "argument_view": {
      "view_type": "tool.arguments",
      "version": 1,
      "collapsed": { "summary": "查询内容：高中数学学习方法，结果上限：2", "args_count": 2 },
      "expanded": {
        "fields": [
          { "key": "query", "label": "查询内容", "value": "高中数学学习方法", "display": "高中数学学习方法" },
          { "key": "top_k", "label": "数量上限", "value": 2, "display": "2" }
        ]
      }
    },
    "result_view": {
      "view_type": "web.search_result",
      "version": 1,
      "collapsed": {
        "title": "找到 2 条网页结果",
        "subtitle": "关键词：高中数学学习方法",
        "status": "done",
        "status_label": "已完成",
        "metrics": [
          { "label": "结果数", "value": "2" },
          { "label": "时效", "value": "近 30 天" }
        ]
      },
      "expanded": {
        "items": [
          {
            "title": "高中数学如何高效提分",
            "subtitle": "example.edu",
            "tags": ["example.edu", "2026-04-20"],
            "detail_lines": ["系统梳理基础概念、错题复盘和专题训练。", "https://example.edu/math-study"],
            "meta": { "url": "https://example.edu/math-study" }
          }
        ],
        "sections": [
          {
            "type": "kv",
            "title": "搜索参数",
            "fields": [
              { "label": "关键词", "value": "高中数学学习方法" },
              { "label": "结果上限", "value": "2" }
            ]
          },
          {
            "type": "items",
            "title": "搜索结果",
            "items": [
              {
                "title": "高中数学如何高效提分",
                "subtitle": "example.edu",
                "tags": ["example.edu", "2026-04-20"],
                "detail_lines": ["系统梳理基础概念、错题复盘和专题训练。"]
              }
            ]
          }
        ],
        "raw_text": "{\"tool\":\"web_search\",\"query\":\"高中数学学习方法\",\"count\":2}",
        "machine_payload": { "tool": "web_search", "query": "高中数学学习方法", "count": 2 }
      }
    }
  },
  {
    "tool": "web_fetch",
    "status": "done",
    "success": true,
    "summary": "已抓取：高中数学如何高效提分",
    "result_view": {
      "view_type": "web.fetch_result",
      "version": 1,
      "collapsed": {
        "title": "已抓取：高中数学如何高效提分",
        "subtitle": "来源：example.edu",
        "status": "done",
        "status_label": "已完成",
        "metrics": [
          { "label": "正文长度", "value": "1280 字" },
          { "label": "是否截断", "value": "否" },
          { "label": "来源", "value": "example.edu" }
        ]
      },
      "expanded": {
        "items": [
          {
            "title": "高中数学如何高效提分",
            "subtitle": "https://example.edu/math-study",
            "tags": ["example.edu"],
            "detail_lines": ["第一段正文预览。", "第二段正文预览。"]
          }
        ],
        "sections": [
          {
            "type": "kv",
            "title": "页面信息",
            "fields": [
              { "label": "链接", "value": "https://example.edu/math-study" },
              { "label": "标题", "value": "高中数学如何高效提分" },
              { "label": "正文长度", "value": "1280 字" },
              { "label": "是否截断", "value": "否" }
            ]
          },
          {
            "type": "callout",
            "title": "正文预览",
            "subtitle": "这是一段网页正文摘要预览。",
            "tone": "info",
            "detail_lines": ["第一段正文预览。", "第二段正文预览。"]
          }
        ],
        "raw_text": "{\"tool\":\"web_fetch\",\"url\":\"https://example.edu/math-study\"}",
        "machine_payload": { "tool": "web_fetch", "url": "https://example.edu/math-study", "truncated": false }
      }
    }
  },
  {
    "tool": "upsert_task_class",
    "status": "done",
    "success": true,
    "summary": "任务类已创建",
    "result_view": {
      "view_type": "taskclass.write_result",
      "version": 1,
      "collapsed": {
        "title": "任务类已创建",
        "subtitle": "已创建「数学压轴题」，共 3 项任务",
        "status": "done",
        "status_label": "已完成",
        "metrics": [
          { "label": "任务类数量", "value": "1 个" },
          { "label": "任务项数量", "value": "3 项" },
          { "label": "来源", "value": "对话" },
          { "label": "写入方式", "value": "创建" }
        ]
      },
      "expanded": {
        "items": [
          {
            "title": "完成圆锥曲线专项",
            "subtitle": "第 1 项",
            "tags": ["顺序 1", "未指定嵌入时间"],
            "detail_lines": ["内容：完成圆锥曲线专项", "嵌入时间：未指定"]
          }
        ],
        "sections": [
          {
            "type": "callout",
            "title": "写入结果",
            "subtitle": "已创建任务类，结果可直接用于后续排程。",
            "tone": "success",
            "detail_lines": ["任务类：数学压轴题", "任务类 ID：88", "任务项数量：3 项"]
          },
          {
            "type": "kv",
            "title": "任务类字段",
            "fields": [
              { "label": "任务类 ID", "value": "88" },
              { "label": "名称", "value": "数学压轴题" },
              { "label": "模式", "value": "自动排布" },
              { "label": "学科类型", "value": "计算型" },
              { "label": "难度等级", "value": "高" },
              { "label": "认知强度", "value": "高" }
            ]
          },
          {
            "type": "items",
            "title": "任务项列表",
            "items": [
              {
                "title": "完成圆锥曲线专项",
                "subtitle": "第 1 项",
                "tags": ["顺序 1"],
                "detail_lines": ["内容：完成圆锥曲线专项"]
              }
            ]
          }
        ],
        "raw_text": "{\"tool\":\"upsert_task_class\",\"success\":true,\"task_class_id\":88,\"created\":true}",
        "machine_payload": { "parsed_result": { "task_class_id": 88, "created": true } }
      }
    }
  },
  {
    "tool": "context_tools_add",
    "status": "done",
    "success": true,
    "summary": "已激活排程工具域",
    "result_view": {
      "view_type": "tool.context_result",
      "version": 1,
      "collapsed": {
        "title": "已激活排程工具域",
        "subtitle": "排程工具域已激活，模式=替换，启用 排程改写、健康分析。",
        "status": "done",
        "status_label": "已完成",
        "metrics": [
          { "label": "域", "value": "排程" },
          { "label": "包", "value": "2 个" },
          { "label": "模式", "value": "替换" }
        ]
      },
      "expanded": {
        "items": [
          {
            "title": "排程工具域",
            "subtitle": "排程工具域已激活，模式=替换，启用 排程改写、健康分析。",
            "tags": ["激活", "替换", "2 个包"],
            "detail_lines": ["工具域：排程工具域", "工具包：排程改写、健康分析", "注入模式：替换"]
          }
        ],
        "sections": [
          {
            "type": "callout",
            "title": "动态工具区已更新",
            "summary": "排程工具域已激活，模式=替换，启用 排程改写、健康分析。",
            "tone": "info",
            "detail_lines": ["已激活目标工具域，可继续调用对应业务工具。"]
          },
          {
            "type": "kv",
            "title": "当前工具区参数",
            "fields": [
              { "label": "工具域", "value": "排程工具域" },
              { "label": "工具包", "value": "排程改写、健康分析" },
              { "label": "注入模式", "value": "替换" },
              { "label": "清空全部", "value": "否" }
            ]
          }
        ],
        "raw_text": "{\"tool\":\"context_tools_add\",\"success\":true,\"action\":\"activate\"}",
        "machine_payload": { "tool": "context_tools_add", "domain": "schedule", "packs": ["mutation", "analyze"] }
      }
    }
  },
  {
    "tool": "queue_pop_head",
    "status": "done",
    "success": true,
    "summary": "已获取队首任务",
    "result_view": {
      "view_type": "schedule.read_result",
      "version": 1,
      "collapsed": {
        "title": "已获取队首任务",
        "subtitle": "[101]数学压轴题，待处理 2 项。",
        "status": "done",
        "status_label": "已完成",
        "metrics": [
          { "label": "待处理", "value": "2 项" },
          { "label": "已完成", "value": "1 项" },
          { "label": "已跳过", "value": "0 项" }
        ]
      },
      "expanded": {
        "items": [
          {
            "title": "[101]数学压轴题",
            "subtitle": "学习，已预排",
            "tags": ["当前处理", "已预排", "2 节"],
            "detail_lines": ["时段：第4天 第7-8节", "任务类 ID：88", "时长需求：2 节"]
          }
        ],
        "sections": [
          {
            "type": "items",
            "title": "当前处理",
            "items": [
              {
                "title": "[101]数学压轴题",
                "subtitle": "学习，已预排",
                "tags": ["当前处理"],
                "detail_lines": ["时段：第4天 第7-8节"]
              }
            ]
          },
          {
            "type": "callout",
            "title": "队首任务已就位",
            "summary": "可以继续调用 queue_apply_head_move 或 queue_skip_head。",
            "tone": "info",
            "detail_lines": ["待处理：2 项", "已完成：1 项", "已跳过：0 项"]
          }
        ],
        "raw_text": "{\"tool\":\"queue_pop_head\",\"has_head\":true}",
        "machine_payload": { "tool": "queue_pop_head", "has_head": true, "pending_count": 2 }
      }
    }
  },
  {
    "tool": "legacy_tool",
    "status": "done",
    "success": true,
    "summary": "旧协议兜底",
    "result_view": {
      "view_type": "legacy_text",
      "version": 1,
      "collapsed": {
        "title": "旧协议工具已完成",
        "status": "done",
        "status_label": "已完成",
        "tool": "legacy_tool",
        "tool_label": "旧协议工具",
        "has_output": true
      },
      "expanded": {
        "raw_text_label": "原始结果",
        "raw_text": "这里是 legacy_text 的原始文本。"
      }
    }
  }
]

const expandedStates = ref<Record<number, boolean>>({
  0: true, 1: true, 2: true, 3: true, 4: true, 5: true
})

function toggle(index: number) {
  expandedStates.value[index] = !expandedStates.value[index]
}

const showDebug = ref(false)
</script>

<template>
  <div class="mock-page">
    <header class="mock-header">
      <div class="mock-header__content">
        <h1>ToolCardRenderer 全家桶验收页</h1>
        <p>集成后端所有当前稳定 view_type，验证结构化渲染及降级逻辑。</p>
      </div>
      <div class="mock-header__actions">
        <button class="debug-toggle" @click="showDebug = !showDebug">
          {{ showDebug ? '隐藏调试信息' : '显示调试信息' }}
        </button>
      </div>
    </header>

    <main class="mock-content">
      <section v-for="(payload, idx) in mockData" :key="idx" class="fixture-item">
        <div class="fixture-item__label">
          <span class="tool-tag">{{ payload.tool }}</span>
          <span class="view-tag">{{ payload.result_view.view_type }}</span>
        </div>
        
        <div class="fixture-item__card">
          <ToolCardRenderer 
            :payload="{
              name: payload.tool,
              status: payload.status,
              summary: payload.summary,
              arguments_preview: payload.arguments_preview || '',
              argument_view: payload.argument_view,
              result_view: payload.result_view
            }" 
            :expanded="!!expandedStates[idx]"
            @toggle="toggle(idx)"
          />

          <div v-if="showDebug" class="debug-info">
            <div class="debug-section">
              <span class="debug-label">raw_text:</span>
              <pre>{{ payload.result_view.expanded?.raw_text }}</pre>
            </div>
            <div class="debug-section">
              <span class="debug-label">machine_payload:</span>
              <pre>{{ JSON.stringify(payload.result_view.expanded?.machine_payload, null, 2) }}</pre>
            </div>
          </div>
        </div>
      </section>
    </main>
  </div>
</template>

<style scoped>
.mock-page {
  padding: 40px;
  background: #f8fafc;
  min-height: 100vh;
  font-family: 'Inter', -apple-system, BlinkMacSystemFont, sans-serif;
}

.mock-header {
  max-width: 1000px;
  margin: 0 auto 40px;
  display: flex;
  justify-content: space-between;
  align-items: center;
  border-bottom: 1px solid #e2e8f0;
  padding-bottom: 24px;
}

.mock-header h1 {
  font-size: 28px;
  font-weight: 800;
  color: #0f172a;
}

.debug-toggle {
  background: #ffffff;
  border: 1px solid #e2e8f0;
  padding: 8px 16px;
  border-radius: 8px;
  font-size: 13px;
  font-weight: 600;
  color: #475569;
  cursor: pointer;
}

.mock-content {
  max-width: 1000px;
  margin: 0 auto;
  display: grid;
  gap: 40px;
}

.fixture-item {
  display: grid;
  grid-template-columns: 240px 1fr;
  gap: 32px;
}

.fixture-item__label {
  display: flex;
  flex-direction: column;
  gap: 8px;
}

.tool-tag {
  font-size: 12px;
  font-weight: 700;
  color: #3b82f6;
  background: #eff6ff;
  padding: 4px 8px;
  border-radius: 6px;
  width: fit-content;
}

.view-tag {
  font-size: 11px;
  color: #64748b;
  background: #f1f5f9;
  padding: 2px 6px;
  border-radius: 4px;
  width: fit-content;
}

.debug-info {
  margin-top: 16px;
  padding: 16px;
  background: #1e293b;
  border-radius: 12px;
  color: #e2e8f0;
  font-size: 11px;
}

.debug-info pre {
  margin: 0;
  white-space: pre-wrap;
  word-break: break-all;
  background: rgba(255, 255, 255, 0.05);
  padding: 8px;
}

@media (max-width: 900px) {
  .fixture-item {
    grid-template-columns: 1fr;
    gap: 12px;
  }
}
</style>
