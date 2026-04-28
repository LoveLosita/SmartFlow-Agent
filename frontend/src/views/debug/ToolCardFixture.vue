<script setup lang="ts">
import { ref } from 'vue'
import ToolCardRenderer from '@/components/dashboard/ToolCardRenderer.vue'
import type { TimelineToolPayload } from '@/api/schedule_agent'

const analysisPayload: TimelineToolPayload = {
  "name": "analyze_health",
  "status": "done",
  "summary": "综合体检报告",
  "arguments_preview": "无参数",
  "result_view": {
    "view_type": "schedule.analysis_result",
    "version": 1,
    "collapsed": {
      "title": "综合体检报告",
      "subtitle": "发现 2 个关键冲突，3 个节奏风险。",
      "status": "done",
      "status_label": "已完成",
      "metrics": [
        { "label": "冲突项", "value": "2 个" },
        { "label": "风险点", "value": "3 个" },
        { "label": "健康分", "value": "72" }
      ]
    },
    "expanded": {
      "items": [
        {
          "title": "方案 A：优先保证英语作文",
          "subtitle": "移动 3 个任务，消除所有硬冲突",
          "tags": ["推荐方案", "低风险"],
          "detail_lines": ["涉及任务：[91]英语作文、[52]组合逻辑电路分析"],
          "meta": { "decision_id": "plan_a" }
        },
        {
          "title": "方案 B：最小化变动",
          "subtitle": "仅移动 [91]，保留 1 个软冲突",
          "tags": ["保守方案"],
          "detail_lines": ["涉及任务：[91]英语作文"],
          "meta": { "decision_id": "plan_b" }
        }
      ],
      "sections": [
        {
          "type": "kv",
          "title": "健康指标详情",
          "fields": [
            { "label": "硬性冲突", "value": "2 处 (时段重叠)" },
            { "label": "软性约束", "value": "1 处 (先修课顺序)" },
            { "label": "学习节奏", "value": "偏紧 (第 11-13 天)" }
          ]
        },
        {
          "type": "callout",
          "title": "核心建议",
          "subtitle": "建议在第 11 天前完成英语作文的初稿安排。",
          "tone": "info",
          "detail_lines": ["这样可以避开第 13 天的专业课复习高峰。"]
        }
      ],
      "raw_text": "体检分析 debug 原文，不要默认主展示"
    }
  }
}

const movePayload: TimelineToolPayload = {
  "name": "move",
  "status": "done",
  "summary": "移动任务成功",
  "arguments_preview": "任务：[52]组合逻辑电路分析，目标日期：第11天，目标时段：第7-8节",
  "result_view": {
    "view_type": "schedule.operation_result",
    "version": 1,
    "collapsed": {
      "title": "移动任务成功",
      "subtitle": "[52]组合逻辑电路分析：从第15天 第7-8节移动到第11天 第7-8节",
      "status": "done",
      "status_label": "已完成",
      "metrics": [
        { "label": "任务数量", "value": "1个" },
        { "label": "影响天数", "value": "2天" }
      ]
    },
    "expanded": {
      "affected_days_label": "第11天、第15天",
      "changes": [
        {
          "task_id": 52,
          "task_label": "[52]组合逻辑电路分析",
          "before_label": "第15天 第7-8节",
          "after_label": "第11天 第7-8节",
          "status_label": "已预排 -> 已预排",
          "operation_key": "move"
        }
      ],
      "raw_text": "移动完成 debug 原文，不要默认主展示"
    }
  }
}

const overviewPayload: TimelineToolPayload = {
  "name": "get_overview",
  "status": "done",
  "summary": "当前排程总览",
  "arguments_preview": "无参数",
  "result_view": {
    "view_type": "schedule.read_result",
    "version": 1,
    "collapsed": {
      "title": "当前排程总览",
      "subtitle": "15 天窗口，已占用 82/180 节，待安排 6 项。",
      "status": "done",
      "status_label": "已完成",
      "metrics": [
        { "label": "已占用", "value": "82 节" },
        { "label": "空闲", "value": "98 节" },
        { "label": "待安排", "value": "6 项" },
        { "label": "课程占位", "value": "14 项" }
      ]
    },
    "expanded": {
      "items": [
        {
          "title": "第1天（第1周 周一）",
          "subtitle": "总占用 6/12 节，任务占用 4/12 节",
          "tags": ["任务 2 项"],
          "detail_lines": [
            "[52]组合逻辑电路分析｜已预排｜第7-8节",
            "[43]谓词逻辑基础（量词与公式）｜已预排｜第9-10节"
          ],
          "meta": { "day": 1 }
        }
      ],
      "sections": [
        {
          "type": "kv",
          "title": "窗口概况",
          "fields": [
            { "label": "规划天数", "value": "15 天" },
            { "label": "总时段", "value": "180 节" },
            { "label": "已占用", "value": "82 节" },
            { "label": "空闲", "value": "98 节" },
            { "label": "课程占位", "value": "14 项" },
            { "label": "已预排任务", "value": "22 项" },
            { "label": "待安排任务", "value": "6 项" }
          ]
        },
        {
          "type": "items",
          "title": "每日概况",
          "items": [
            {
              "title": "第1天（第1周 周一）",
              "subtitle": "总占用 6/12 节，任务占用 4/12 节",
              "tags": ["任务 2 项"],
              "detail_lines": [
                "[52]组合逻辑电路分析｜已预排｜第7-8节",
                "[43]谓词逻辑基础（量词与公式）｜已预排｜第9-10节"
              ]
            },
            {
              "title": "第2天（第1周 周二）",
              "subtitle": "总占用 8/12 节，任务占用 6/12 节",
              "tags": ["任务 3 项"],
              "detail_lines": [
                "[36]向量组的线性相关性｜已预排｜第3-4节",
                "[68]社会主义改造理论｜已预排｜第5-6节"
              ]
            }
          ]
        },
        {
          "type": "items",
          "title": "任务清单",
          "items": [
            {
              "title": "[52]组合逻辑电路分析",
              "subtitle": "学习｜已预排",
              "tags": ["已预排"],
              "detail_lines": [
                "时段：第1天（第1周 周一） 第7-8节",
                "来源：任务项"
              ],
              "meta": { "task_id": 52, "status": "suggested" }
            },
            {
              "title": "[91]英语作文",
              "subtitle": "英语｜待安排",
              "tags": ["待安排"],
              "detail_lines": [
                "时段：尚未落位",
                "来源：任务项"
              ],
              "meta": { "task_id": 91, "status": "pending" }
            }
          ]
        },
        {
          "type": "items",
          "title": "任务类约束",
          "items": [
            {
              "title": "英语",
              "subtitle": "均匀分布",
              "tags": [],
              "detail_lines": [
                "排程策略：均匀分布",
                "总预算：6 节",
                "允许嵌入水课：是"
              ],
              "meta": { "task_class_id": 7, "strategy": "steady" }
            }
          ]
        }
      ],
      "machine_payload": {
        "total_days": 15,
        "total_slots": 180,
        "total_occupied": 82,
        "task_pending_count": 6
      },
      "raw_text": "总览 debug 原文，不要默认主展示"
    }
  }
}

const queuePayload: TimelineToolPayload = {
  "name": "queue_status",
  "status": "done",
  "summary": "队列待处理 3 项",
  "arguments_preview": "无参数",
  "result_view": {
    "view_type": "schedule.read_result",
    "version": 1,
    "collapsed": {
      "title": "队列待处理 3 项",
      "subtitle": "当前处理：[52]组合逻辑电路分析，第 2 次尝试。",
      "status": "done",
      "status_label": "已完成",
      "metrics": [
        { "label": "待处理", "value": "3 项" },
        { "label": "已完成", "value": "1 项" },
        { "label": "已跳过", "value": "0 项" }
      ]
    },
    "expanded": {
      "items": [
        {
          "title": "[52]组合逻辑电路分析",
          "subtitle": "学习｜已预排",
          "tags": ["当前处理"],
          "detail_lines": [
            "时段：第11天（第15周 周四） 第7-8节",
            "任务类 ID：3",
            "当前尝试：第 2 次"
          ],
          "meta": { "task_id": 52, "status": "suggested" }
        },
        {
          "title": "[43]谓词逻辑基础（量词与公式）",
          "subtitle": "学习｜已预排",
          "tags": ["待处理"],
          "detail_lines": [
            "时段：第15天（第16周 周一） 第7-8节"
          ],
          "meta": { "task_id": 43, "queue_index": 0 }
        }
      ],
      "sections": [
        {
          "type": "items",
          "title": "当前处理",
          "items": [
            {
              "title": "[52]组合逻辑电路分析",
              "subtitle": "学习｜已预排",
              "tags": ["当前处理"],
              "detail_lines": [
                "时段：第11天（第15周 周四） 第7-8节",
                "任务类 ID：3",
                "当前尝试：第 2 次"
              ]
            }
          ]
        },
        {
          "type": "items",
          "title": "待处理队列",
          "items": [
            {
              "title": "[43]谓词逻辑基础（量词与公式）",
              "subtitle": "学习｜已预排",
              "tags": ["待处理"],
              "detail_lines": [
                "时段：第15天（第16周 周一） 第7-8节"
              ]
            },
            {
              "title": "[91]英语作文",
              "subtitle": "英语｜待安排",
              "tags": ["待处理"],
              "detail_lines": [
                "时段：尚未落位"
              ]
            }
          ]
        },
        {
          "type": "kv",
          "title": "运行概况",
          "fields": [
            { "label": "待处理", "value": "3 项" },
            { "label": "已完成", "value": "1 项" },
            { "label": "已跳过", "value": "0 项" },
            { "label": "当前任务", "value": "[52]组合逻辑电路分析" }
          ]
        },
        {
          "type": "callout",
          "title": "最近一次失败",
          "subtitle": "队列中保留了上一轮 apply 的失败原因。",
          "tone": "warning",
          "detail_lines": [
            "移动失败：目标位置已被占用，请重新选择候选时段。"
          ]
        }
      ],
      "machine_payload": {
        "pending_count": 3,
        "completed_count": 1,
        "skipped_count": 0,
        "current_task_id": 52,
        "current_attempt": 2,
        "next_task_ids": [43, 91, 88]
      },
      "raw_text": "队列状态 debug 原文，不要默认主展示"
    }
  }
}

const swapPayload: TimelineToolPayload = {
  "name": "swap",
  "status": "done",
  "summary": "交换任务成功",
  "arguments_preview": "任务A：[52]组合逻辑电路分析，任务B：[43]谓词逻辑基础（量词与公式）",
  "result_view": {
    "view_type": "schedule.operation_result",
    "version": 1,
    "collapsed": {
      "title": "交换任务成功",
      "subtitle": "[52]组合逻辑电路分析 与 [43]谓词逻辑基础（量词与公式） 已交换位置",
      "status": "done",
      "status_label": "已完成",
      "metrics": [
        { "label": "任务数量", "value": "2个" },
        { "label": "影响天数", "value": "2天" }
      ]
    },
    "expanded": {
      "affected_days_label": "第11天、第15天",
      "changes": [
        {
          "task_id": 52,
          "task_label": "[52]组合逻辑电路分析",
          "before_label": "第15天 第7-8节",
          "after_label": "第11天 第7-8节",
          "status_label": "已预排 -> 已预排"
        },
        {
          "task_id": 43,
          "task_label": "[43]谓词逻辑基础（量词与公式）",
          "before_label": "第11天 第7-8节",
          "after_label": "第15天 第7-8节",
          "status_label": "已预排 -> 已预排"
        }
      ],
      "raw_text": "交换完成：这里是 debug 原文，不要默认主展示"
    }
  }
}

const queryRangePayload: TimelineToolPayload = {
  "name": "query_range",
  "status": "done",
  "summary": "第1天（第1周 周一）全日概况",
  "arguments_preview": "目标日期：第1天（第1周 周一）",
  "result_view": {
    "view_type": "schedule.read_result",
    "version": 1,
    "collapsed": {
      "title": "第1天（第1周 周一）全日概况",
      "subtitle": "已占用 6/12 节，连续空闲 3 段。",
      "status": "done",
      "status_label": "已完成",
      "metrics": [
        { "label": "总占用", "value": "6/12" },
        { "label": "任务占用", "value": "4/12" },
        { "label": "空闲段", "value": "3 段" }
      ]
    },
    "expanded": {
      "items": [
        {
          "title": "第1-2节",
          "subtitle": "1 个事项",
          "tags": ["2 节", "已占用"],
          "detail_lines": ["[52]组合逻辑电路分析｜已预排｜学习"],
          "meta": {
            "day": 1,
            "slot_start": 1,
            "slot_end": 2
          }
        },
        {
          "title": "第3-4节",
          "subtitle": "空闲",
          "tags": ["2 节", "空闲"],
          "detail_lines": ["这一段当前可直接安排任务。"],
          "meta": {
            "day": 1,
            "slot_start": 3,
            "slot_end": 4
          }
        }
      ],
      "sections": [
        {
          "type": "kv",
          "title": "当日概况",
          "fields": [
            { label: "总占用", value: "6/12 节" },
            { label: "任务占用", value: "4/12 节" },
            { label: "连续空闲段", value: "3 段" }
          ]
        },
        {
          "type": "items",
          "title": "时段分布",
          "items": [
            {
              "title": "第1-2节",
              "subtitle": "1 个事项",
              "tags": ["2 节", "已占用"],
              "detail_lines": ["[52]组合逻辑电路分析｜已预排｜学习"]
            },
            {
              "title": "第3-4节",
              "subtitle": "空闲",
              "tags": ["2 节", "空闲"],
              "detail_lines": ["这一段当前可直接安排任务。"]
            }
          ]
        },
        {
          "type": "callout",
          "title": "提示",
          "subtitle": "当前日期仍有可用时段。",
          "tone": "info",
          "detail_lines": ["可以继续查询可用时段或选择任务落位。"]
        }
      ],
      "raw_text": "debug 原始 observation，不要默认主展示",
      "machine_payload": {
        "mode": "full_day",
        "day": 1
      }
    }
  }
}

const expandedMap = ref<Record<string, boolean>>({
  analysis: true,
  move: true,
  overview: true,
  queue: true,
  swap: true,
  query: true
})

function toggle(key: string) {
  expandedMap.value[key] = !expandedMap.value[key]
}
</script>

<template>
  <div class="fixture-page">
    <header class="fixture-header">
      <h1>ToolCardRenderer Fixture</h1>
      <p>验证两类新协议卡片的通用性与健壮性</p>
    </header>

    <main class="fixture-content">
      <section class="fixture-section">
        <h2>1. analysis_result: analyze_health (综合体检)</h2>
        <div class="card-wrapper">
          <ToolCardRenderer 
            :payload="analysisPayload" 
            :expanded="expandedMap.analysis"
            @toggle="toggle('analysis')"
          />
        </div>
      </section>

      <section class="fixture-section">
        <h2>2. operation_result: move (移动任务)</h2>
        <div class="card-wrapper">
          <ToolCardRenderer 
            :payload="movePayload" 
            :expanded="expandedMap.move"
            @toggle="toggle('move')"
          />
        </div>
      </section>

      <section class="fixture-section">
        <h2>3. read_result: get_overview (排程总览)</h2>
        <div class="card-wrapper">
          <ToolCardRenderer 
            :payload="overviewPayload" 
            :expanded="expandedMap.overview"
            @toggle="toggle('overview')"
          />
        </div>
      </section>

      <section class="fixture-section">
        <h2>4. read_result: queue_status (队列状态 + Warning)</h2>
        <div class="card-wrapper">
          <ToolCardRenderer 
            :payload="queuePayload" 
            :expanded="expandedMap.queue"
            @toggle="toggle('queue')"
          />
        </div>
      </section>

      <section class="fixture-section">
        <h2>5. operation_result: swap (交换任务)</h2>
        <div class="card-wrapper">
          <ToolCardRenderer 
            :payload="swapPayload" 
            :expanded="expandedMap.swap"
            @toggle="toggle('swap')"
          />
        </div>
      </section>

      <section class="fixture-section">
        <h2>6. read_result: query_range (全日概况)</h2>
        <div class="card-wrapper">
          <ToolCardRenderer 
            :payload="queryRangePayload" 
            :expanded="expandedMap.query"
            @toggle="toggle('query')"
          />
        </div>
      </section>

      <section class="fixture-section">
        <h2>7. 降级测试 (未知协议)</h2>
        <div class="card-wrapper">
          <ToolCardRenderer 
            :payload="{
              name: 'unknown_tool',
              status: 'done',
              summary: '这是 fallback summary',
              result_view: {
                view_type: 'unknown.type',
                collapsed: {
                  title: '未知协议标题',
                  subtitle: '这是从 collapsed 中读取的副标题',
                  status_label: '进行中',
                  metrics: [{ label: '测试', value: '100' }]
                }
              }
            }" 
            :expanded="false"
            @toggle="() => {}"
          />
        </div>
      </section>
    </main>
  </div>
</template>

<style scoped>
.fixture-page {
  padding: 40px;
  background: #f8fafc;
  min-height: 100vh;
  font-family: 'Inter', sans-serif;
}

.fixture-header {
  margin-bottom: 40px;
  border-bottom: 1px solid #e2e8f0;
  padding-bottom: 20px;
}

.fixture-header h1 {
  font-size: 28px;
  color: #1e293b;
  margin: 0 0 8px;
}

.fixture-header p {
  color: #64748b;
  margin: 0;
}

.fixture-content {
  max-width: 900px;
}

.fixture-section {
  margin-bottom: 40px;
}

.fixture-section h2 {
  font-size: 18px;
  color: #334155;
  margin-bottom: 16px;
  padding-left: 12px;
  border-left: 4px solid #3b82f6;
}

.card-wrapper {
  background: white;
  padding: 24px;
  border-radius: 20px;
  box-shadow: 0 4px 6px -1px rgba(0, 0, 0, 0.05);
}
</style>
