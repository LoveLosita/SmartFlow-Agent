# 日程卡片前端集成文档

本文档描述前端实现"排程结果卡片 + 暂存/写库按钮"所需的全部接口、数据结构和交互流程。

---

## 一、整体交互流程

```
用户发消息 → SSE 流式返回 → 收到 schedule_completed 事件
                                    ↓
                      调用 schedule-preview 接口拉取排程数据
                                    ↓
                        渲染日程卡片（可拖拽调整位置）
                                    ↓
                  ┌─────────────────┴─────────────────┐
                  │                                   │
          "暂存 state"按钮                      "写库"按钮
          POST /agent/schedule-state         PUT /task-class/apply-batch-into-schedule
          （暂存到 Redis 快照）                （真正写入 MySQL 日程表）
```

---

## 二、SSE 事件格式

### 2.1 基础 SSE 壳

所有 SSE data 行都是 JSON，格式遵循 OpenAI 兼容协议：

```json
{
  "id": "request-id",
  "object": "chat.completion.chunk",
  "created": 1745036800,
  "model": "worker",
  "choices": [
    {
      "index": 0,
      "delta": {
        "role": "assistant",
        "content": "正文内容",
        "reasoning_content": "思考内容"
      },
      "finish_reason": null
    }
  ],
  "extra": { ... }
}
```

- `choices` 可以为空数组（纯结构化事件时）
- `extra` 为可选字段，旧事件不含 extra

### 2.2 心跳保活

```
: ping
```

SSE 标准注释行，每 5 秒一次。前端 `JSON.parse` 失败后丢弃即可。

### 2.3 流结束标记

```
data: [DONE]
```

### 2.4 错误事件

```json
{
  "error": {
    "message": "错误描述",
    "type": "server_error",
    "code": "5xxxx"
  }
}
```

---

## 三、`extra` 结构化事件类型

前端通过 `extra.kind` 判断事件类型。

### 3.1 事件类型枚举

| kind | 含义 | display_mode | 说明 |
|------|------|-------------|------|
| `reasoning_text` | 思考文字 | `append` | 逐块追加 |
| `assistant_text` | 回复正文 | `append` | 逐块追加 |
| `status` | 阶段状态 | `card` | 如"正在排程" |
| `tool_call` | 工具调用开始 | `card` | 如"正在查询任务" |
| `tool_result` | 工具调用结果 | `card` | 如"找到 3 个任务" |
| `confirm_request` | 待确认事件 | `card` | **需要用户确认** |
| `interrupt` | 中断/追问 | `card` | ask_user 追问 |
| `schedule_completed` | **排程完毕** | `card` | **前端拉取排程数据的信号** |
| `finish` | 流结束 | `replace` | 收尾 |

### 3.2 status 事件

```json
{
  "extra": {
    "kind": "status",
    "block_id": "execute.status",
    "stage": "execute",
    "display_mode": "card",
    "status": {
      "code": "planning",
      "summary": "正在智能排程..."
    }
  }
}
```

### 3.3 tool_call 事件（工具调用开始）

```json
{
  "extra": {
    "kind": "tool_call",
    "block_id": "execute.tool.1",
    "stage": "execute",
    "display_mode": "card",
    "tool": {
      "name": "smart_planning",
      "status": "start",
      "summary": "正在为任务类智能排程",
      "arguments_preview": "任务类: [高数作业, 英语阅读]"
    }
  }
}
```

### 3.4 tool_result 事件（工具调用结果）

```json
{
  "extra": {
    "kind": "tool_result",
    "block_id": "execute.tool.1",
    "stage": "execute",
    "display_mode": "card",
    "tool": {
      "name": "smart_planning",
      "status": "done",
      "summary": "成功生成排程方案",
      "arguments_preview": "已排 3 个任务"
    }
  }
}
```

- `tool.status` 取值：`start` | `done` | `blocked` | `failed`

### 3.5 confirm_request 事件（用户确认）

```json
{
  "choices": [{
    "index": 0,
    "delta": { "role": "assistant", "content": "请确认是否应用排程结果...\n" }
  }],
  "extra": {
    "kind": "confirm_request",
    "block_id": "execute.confirm.1",
    "stage": "execute",
    "display_mode": "card",
    "confirm": {
      "interaction_id": "confirm_abc123",
      "title": "确认应用排程结果",
      "summary": "是否将 3 个任务安排到日程中？"
    }
  }
}
```

**前端需要：**
1. 保存 `interaction_id`
2. 展示确认卡片（title + summary + 确认/拒绝按钮）
3. 用户点击后发送 resume 请求（见第五节）

### 3.6 schedule_completed 事件（排程完毕信号）

```json
{
  "extra": {
    "kind": "schedule_completed",
    "block_id": "deliver.schedule",
    "stage": "deliver",
    "display_mode": "card"
  }
}
```

**前端收到后：** 用当前 `conversation_id` 调用 `GET /agent/schedule-preview` 拉取排程数据。

### 3.7 finish 事件

```json
{
  "choices": [{
    "index": 0,
    "delta": {},
    "finish_reason": "stop"
  }],
  "extra": {
    "kind": "finish",
    "block_id": "deliver.finish",
    "stage": "deliver",
    "display_mode": "replace"
  }
}
```

---

## 四、排程预览接口

收到 `schedule_completed` 事件后调用此接口获取排程数据。

### GET `/api/v1/agent/schedule-preview`

**Query 参数：**

| 参数 | 类型 | 必填 | 说明 |
|------|------|------|------|
| `conversation_id` | string | 是 | 会话 ID |

**响应：**

```json
{
  "status": "10000",
  "info": "success",
  "data": {
    "conversation_id": "1655dd9b-...",
    "trace_id": "trace-...",
    "summary": "已为你安排了 3 个任务",
    "candidate_plans": [
      {
        "week": 1,
        "events": [
          {
            "id": 10,
            "order": 1,
            "day_of_week": 1,
            "name": "高等数学",
            "start_time": "08:00",
            "end_time": "09:30",
            "location": "教学楼A",
            "type": "course",
            "span": 2,
            "status": "normal",
            "embedded_task_info": {
              "id": 100,
              "name": "数学作业",
              "type": "task"
            }
          },
          {
            "id": 11,
            "order": 2,
            "day_of_week": 1,
            "name": "编程练习",
            "start_time": "10:00",
            "end_time": "11:30",
            "location": "",
            "type": "task",
            "span": 2,
            "status": "normal",
            "embedded_task_info": {}
          }
        ]
      },
      {
        "week": 2,
        "events": [...]
      }
    ],
    "hybrid_entries": [
      {
        "week": 1,
        "day_of_week": 1,
        "section_from": 3,
        "section_to": 4,
        "name": "英语阅读",
        "type": "task",
        "status": "suggested",
        "task_item_id": 101,
        "task_class_id": 5,
        "event_id": 0,
        "can_be_embedded": false,
        "block_for_suggested": true,
        "context_tag": "Memory"
      },
      {
        "week": 1,
        "day_of_week": 2,
        "section_from": 1,
        "section_to": 2,
        "name": "高等数学",
        "type": "course",
        "status": "existing",
        "task_item_id": 0,
        "task_class_id": 0,
        "event_id": 10,
        "can_be_embedded": true,
        "block_for_suggested": false,
        "context_tag": ""
      }
    ],
    "task_class_ids": [5, 6],
    "generated_at": "2026-04-19T10:00:00Z"
  }
}
```

### 数据结构说明

#### HybridScheduleEntry（混合日程条目）

前端渲染日程卡片的核心数据结构。课程和任务统一到同一个列表中。

| 字段 | 类型 | 说明 |
|------|------|------|
| `week` | int | 学期周数 |
| `day_of_week` | int | 星期几（1=周一，7=周日） |
| `section_from` | int | 起始节次（1-based） |
| `section_to` | int | 结束节次（1-based） |
| `name` | string | 名称 |
| `type` | string | `"course"`（课程）或 `"task"`（任务） |
| `status` | string | `"existing"`（已确定）或 `"suggested"`（建议） |
| `task_item_id` | int | 任务项 ID（仅 type=task 且 status=suggested 时有值） |
| `task_class_id` | int | 任务类 ID（仅 type=task 且 status=suggested 时有值，对应写库接口的 `task_class_id`） |
| `event_id` | int | 日程事件 ID（仅 existing 时有值） |
| `can_be_embedded` | bool | 课程是否允许嵌入任务 |
| `block_for_suggested` | bool | 是否阻塞建议任务占位 |
| `context_tag` | string | 认知类型标签：`"High-Logic"` / `"Memory"` / `"Review"` / `"General"` |

**渲染建议：**
- `status=existing` → 已有课程/日程，渲染为固定色块（不可拖拽）
- `status=suggested` → AI 建议的任务，渲染为可拖拽色块
- `can_be_embedded=true` 的课程 → 任务可嵌入到其时段内

#### UserWeekSchedule（按周视图的已有日程）

| 字段 | 类型 | 说明 |
|------|------|------|
| `week` | int | 周数 |
| `events` | WeeklyEventBrief[] | 该周事件列表 |

#### WeeklyEventBrief

| 字段 | 类型 | 说明 |
|------|------|------|
| `id` | int | ScheduleEvent.ID |
| `order` | int | 天内显示顺序 |
| `day_of_week` | int | 星期几 |
| `name` | string | 名称 |
| `start_time` | string | 开始时间（如 "08:00"） |
| `end_time` | string | 结束时间 |
| `location` | string | 地点 |
| `type` | string | `"course"` / `"task"` |
| `span` | int | 跨越节数（渲染高度） |
| `status` | string | `"normal"` / `"interrupted"` |
| `embedded_task_info` | TaskBrief | 嵌入的任务信息（可选） |

#### TaskBrief

| 字段 | 类型 | 说明 |
|------|------|------|
| `id` | int | 关联 ID |
| `name` | string | 任务名称 |
| `type` | string | `"task"` |

---

## 五、用户确认流程（confirm / resume）

### 5.1 流程说明

当后端需要用户确认时：
1. SSE 推送 `kind=confirm_request` 事件
2. 前端展示确认卡片
3. 用户点击"确认"或"拒绝"
4. 前端发送 resume 请求回同一聊天接口

### 5.2 请求格式

**POST `/api/v1/agent/chat`**（复用聊天入口）

```json
{
  "conversation_id": "1655dd9b-2c4c-4b56-a712-f34c11b2634d",
  "message": "",
  "extra": {
    "resume": {
      "interaction_id": "confirm_abc123",
      "type": "confirm",
      "action": "approve"
    }
  }
}
```

### 5.3 resume 字段说明

| 字段 | 类型 | 必填 | 说明 |
|------|------|------|------|
| `interaction_id` | string | 是 | 从 confirm_request 事件中获取 |
| `type` | string | 否 | 默认为 `"confirm"`，也可为 `"ask_user"` 或 `"connection_recover"` |
| `action` | string | 是 | 见下表 |

**confirm 类型的 action：**

| action | 含义 |
|--------|------|
| `approve` | 用户同意，继续执行 |
| `reject` | 用户拒绝 |
| `cancel` | 用户取消 |

**ask_user 类型的 action：**

| action | 含义 |
|--------|------|
| `reply` | 用户回答追问（回答内容放在顶层 `message` 字段） |
| `cancel` | 用户取消 |

---

## 六、"暂存 state"按钮

将用户在卡片上拖拽调整后的任务位置暂存到 Redis 快照。**不写 MySQL，不触发 LLM。**

### POST `/api/v1/agent/schedule-state`

**请求体：**

```json
{
  "conversation_id": "1655dd9b-2c4c-4b56-a712-f34c11b2634d",
  "items": [
    {
      "task_item_id": 101,
      "week": 1,
      "day_of_week": 1,
      "start_section": 3,
      "end_section": 4
    },
    {
      "task_item_id": 102,
      "week": 2,
      "day_of_week": 3,
      "start_section": 5,
      "end_section": 6,
      "embed_course_event_id": 20
    }
  ]
}
```

### SaveScheduleStatePlacedItem 字段说明

| 字段 | 类型 | 必填 | 说明 |
|------|------|------|------|
| `task_item_id` | int | 是 | 任务项 ID（来自 HybridScheduleEntry 的 `task_item_id`） |
| `week` | int | 是 | 学期周数（≥1） |
| `day_of_week` | int | 是 | 星期几（1-7） |
| `start_section` | int | 是 | 起始节次（≥1） |
| `end_section` | int | 是 | 结束节次（≥1，须 ≥ start_section） |
| `embed_course_event_id` | int | 否 | 嵌入目标课程的 event_id（来自 HybridScheduleEntry 的 `event_id`） |

**安全保证：** 只修改 `type=task` 的建议任务，课程数据永远不变。不在 items 中的任务保持原样。

### 成功响应

```json
{
  "status": "10000",
  "info": "success",
  "data": null
}
```

### 错误码

| 错误码 status | 含义 |
|--------------|------|
| `40004` | 缺少 conversation_id |
| `40005` | 请求体格式错误 |
| `40058` | 排程快照不存在或已过期（需重新对话） |
| `40059` | week/day_of_week 坐标超出排程窗口范围 |
| `40060` | task_item_id 在快照中不存在 |
| `40061` | embed_course_event_id 在快照课程中不存在 |
| `40062` | 请求中包含重复的 task_item_id |

---

## 七、"写库"按钮

将任务真正写入 MySQL 日程表。**需要按任务类（task_class）分组调用。**

### PUT `/api/v1/task-class/apply-batch-into-schedule`

**注意：** 该接口需要幂等性 Key（`Idempotency-Key` header），防止重复点击。

**请求体：**

```json
{
  "task_class_id": 123,
  "items": [
    {
      "task_item_id": 101,
      "week": 1,
      "day_of_week": 1,
      "start_section": 3,
      "end_section": 4
    },
    {
      "task_item_id": 102,
      "week": 2,
      "day_of_week": 3,
      "start_section": 5,
      "end_section": 6,
      "embed_course_event_id": 20
    }
  ]
}
```

### 请求字段说明

| 字段 | 类型 | 必填 | 说明 |
|------|------|------|------|
| `task_class_id` | int | 是 | 任务类 ID（来自 HybridScheduleEntry 关联的任务类） |
| `items` | SingleTaskClassItem[] | 是 | 放置项列表 |

### SingleTaskClassItem 字段说明

| 字段 | 类型 | 必填 | 说明 |
|------|------|------|------|
| `task_item_id` | int | 是 | 任务项 ID |
| `week` | int | 是 | 学期周数（≥1） |
| `day_of_week` | int | 是 | 星期几（1-7） |
| `start_section` | int | 是 | 起始节次（≥1） |
| `end_section` | int | 是 | 结束节次（≥1，须 ≥ start_section） |
| `embed_course_event_id` | int | 否 | 嵌入目标课程的 ScheduleEvent.ID |

### 成功响应

```json
{
  "status": "10000",
  "info": "success"
}
```

### 错误码

| 错误码 status | 含义 |
|--------------|------|
| `40005` | 请求体格式错误 |
| `40037` | 缺少 Idempotency-Key header |
| `40038` | 请求正在处理中（幂等性防重） |
| `40026` | 日程冲突 |
| `40025` | 课程已被其他任务块嵌入 |
| `40034` | 任务项已安排 |
| `40048` | 任务项不属于该任务类 |
| `40049` | 任务项时间超出学期范围 |

---

## 八、两个按钮的 items 格式完全一致

**关键设计：** "暂存 state"和"写库"两个按钮的 `items` 数据格式完全相同。

```
前端只需维护一份 items 数组：
  - 用户拖拽调整位置 → 更新 items
  - 点击"暂存" → POST /agent/schedule-state { items }
  - 点击"写库" → PUT /task-class/apply-batch-into-schedule { task_class_id, items }
```

**区别：**
- "暂存"接口需要 `conversation_id`，"写库"接口需要 `task_class_id`
- `task_class_id` 来自 `HybridScheduleEntry.task_class_id`（每个 suggested 任务条目都有），也可从响应顶层 `task_class_ids` 数组获取
- 写库时需按 `task_class_id` 分组：相同 `task_class_id` 的 items 放在同一个请求中
- "暂存"写 Redis 快照（可恢复），"写库"写 MySQL 日程表（持久化）
- "写库"需要 `Idempotency-Key` header 防重

---

## 九、前端实现建议

### 9.1 日程卡片渲染

1. 用 `hybrid_entries` 作为主数据源渲染周视图
2. `status=existing` 的条目渲染为**只读**色块（灰色/蓝色课程块）
3. `status=suggested` 的条目渲染为**可拖拽**色块（绿色/橙色任务块）
4. 拖拽时校验：目标位置是否与 `block_for_suggested=true` 的条目冲突
5. 嵌入：拖拽到 `can_be_embedded=true` 的课程块上时，设置 `embed_course_event_id`

### 9.2 items 数组维护

```typescript
interface PlacedItem {
  task_item_id: number;
  week: number;
  day_of_week: number;
  start_section: number;
  end_section: number;
  embed_course_event_id?: number;
}

// 从 hybrid_entries 中筛选 suggested 任务，构建 items
function buildItemsFromEntries(entries: HybridScheduleEntry[]): PlacedItem[] {
  return entries
    .filter(e => e.status === 'suggested' && e.task_item_id)
    .map(e => ({
      task_item_id: e.task_item_id,
      week: e.week,
      day_of_week: e.day_of_week,
      start_section: e.section_from,
      end_section: e.section_to,
      embed_course_event_id: e.event_id || undefined,
    }));
}
```

### 9.3 SSE 连接管理

- 使用 `fetch` + `ReadableStream`（非 `EventSource`，因为需要 POST body）
- 心跳 `: ping` 行不是 `data:` 开头，`JSON.parse` 会失败，直接忽略
- 错误事件格式为 `{ "error": { ... } }`，注意与正常事件区分
- 流结束标记为 `data: [DONE]`
- `X-Conversation-ID` 响应头包含服务端分配的 conversation_id

### 9.4 完整交互时序

```
1. 用户发送消息 → POST /agent/chat
2. SSE 流返回 thinking + 工具事件 + 正文
3. 收到 extra.kind === "schedule_completed"
4. GET /agent/schedule-preview?conversation_id=xxx
5. 渲染日程卡片
6. 用户拖拽调整任务位置 → 更新本地 items 数组
7a. 点击"暂存" → POST /agent/schedule-state { conversation_id, items }
7b. 点击"写库" → PUT /task-class/apply-batch-into-schedule { task_class_id, items }
```
