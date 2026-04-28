# NewAgent 思考摘要前端对接说明

## 背景

后端已经不再把模型原始 `reasoning_content` 直接透传给前端。新的展示入口是 SSE 顶层 `extra.kind = "thinking_summary"` 事件。

目标体验：

- 用户等待模型深度思考时，前端每隔几秒收到一条短摘要，作为当前思考状态的轻量提示。
- 展开后展示稍长的 `detail_summary`，多条按时间追加。
- 模型开始输出正文后，当前思考摘要停止更新。
- 刷新会话后，只恢复长摘要，不恢复短摘要。

## 实时 SSE 协议

聊天接口仍然是：

```http
POST /api/v1/agent/chat
Content-Type: application/json
Accept: text/event-stream
```

SSE 每个业务包仍是标准格式：

```text
data: {json}

data: [DONE]
```

后端保活心跳是 SSE 注释行：

```text
: ping
```

前端按现有逻辑忽略不能 JSON.parse 的块即可。

## thinking_summary 事件

实时思考摘要事件没有 `delta.content`，也没有 `delta.reasoning_content`。前端应从顶层 `extra.thinking_summary` 读取。

示例：

```json
{
  "id": "trace-id",
  "object": "chat.completion.chunk",
  "created": 1777399000,
  "model": "pro",
  "extra": {
    "kind": "thinking_summary",
    "block_id": "plan.speak",
    "stage": "plan",
    "display_mode": "append",
    "thinking_summary": {
      "summary_seq": 1,
      "short_summary": "正在梳理计划",
      "detail_summary": "正在把用户目标拆成可执行步骤，并检查是否需要补充约束。",
      "duration_seconds": 3.214
    }
  }
}
```

字段说明：

| 字段 | 说明 |
| --- | --- |
| `extra.kind` | 固定为 `thinking_summary`。 |
| `extra.block_id` | 当前摘要所属展示块，例如 `plan.speak`、`execute.speak`、`fallback.speak`。建议作为分组 key 的一部分。 |
| `extra.stage` | 当前节点阶段，例如 `plan`、`execute`、`fallback`。 |
| `extra.display_mode` | 当前固定为 `append`，表示长摘要按条追加。 |
| `thinking_summary.summary_seq` | 同一个摘要器内递增，用于忽略重复或乱序摘要。不要当作全局 timeline seq。 |
| `thinking_summary.short_summary` | 实时短摘要，只用于当前流式展示，不持久化。 |
| `thinking_summary.detail_summary` | 展开态长摘要，按 append 语义追加；刷新后也只恢复这个字段。 |
| `thinking_summary.duration_seconds` | 从首次收到 reasoning 到生成该摘要的耗时秒数，可能是小数。 |
| `thinking_summary.final` | 可选。若出现 `true`，表示该摘要器在没有正文打断的情况下自然收口。不要依赖它一定出现。 |

已删除字段：

- `state` 已从协议、prompt、timeline 持久化里删除，前端不要再依赖或展示。

## 前端处理建议

建议把思考摘要作为 assistant 消息内的一个子结构，而不是普通正文。

推荐 key：

```ts
const key = extra.block_id || extra.stage || 'thinking'
```

推荐类型：

```ts
export interface ThinkingSummaryPayload {
  summary_seq?: number
  short_summary?: string
  detail_summary?: string
  final?: boolean
  duration_seconds?: number
}

export interface ThinkingSummaryBlock {
  key: string
  stage?: string
  blockId?: string
  latestSeq: number
  latestShort: string
  details: Array<{
    seq: number
    text: string
    durationSeconds?: number
    final?: boolean
  }>
  active: boolean
  collapsed: boolean
}
```

实时处理伪代码：

```ts
function handleThinkingSummary(extra: StreamExtra, message: AssistantMessage) {
  if (extra.kind !== 'thinking_summary') return false

  const summary = extra.thinking_summary
  if (!summary) return true

  const key = extra.block_id || extra.stage || 'thinking'
  const block = ensureThinkingSummaryBlock(message, key, {
    stage: extra.stage,
    blockId: extra.block_id,
  })

  const seq = summary.summary_seq ?? block.latestSeq + 1
  if (seq <= block.latestSeq) return true

  block.latestSeq = seq
  block.active = summary.final !== true

  if (summary.short_summary?.trim()) {
    block.latestShort = summary.short_summary.trim()
  }

  if (summary.detail_summary?.trim()) {
    block.details.push({
      seq,
      text: summary.detail_summary.trim(),
      durationSeconds: summary.duration_seconds,
      final: summary.final,
    })
  }

  return true
}
```

正文开始时的处理：

```ts
function handleAssistantContentStart(message: AssistantMessage) {
  // 后端正文一出现就会停止当前 block 的摘要；
  // 前端这里也可以把活跃思考块收口，避免动效继续闪。
  message.thinkingSummaryBlocks?.forEach(block => {
    block.active = false
  })
}
```

注意：

- 收到 `thinking_summary` 时，不要追加到 `assistantMessage.content`。
- 收到 `thinking_summary` 时，不要写入旧的 `assistantMessage.reasoning`。
- 若仍收到旧链路 `delta.reasoning_content`，可以保留兼容，但新样式应优先使用 `thinking_summary`。
- `summary_seq` 只在同一个 `block_id/stage` 下去重；不同 block 不要互相比较。

## 展示语义

短摘要：

- 展示最新一条 `short_summary`。
- 适合放在折叠态标题、胶囊、加载条旁边。
- 不要持久化到本地历史，也不要在刷新恢复后强行补出来。

长摘要：

- 每次收到非空 `detail_summary` 就追加一条。
- 展开态展示 `details` 列表。
- 如果你想做得更像 Gemini/豆包，可以折叠态只露最新短摘要，展开态按时间展示长摘要列表。

收口条件：

- 收到第一段 `delta.content`：关闭当前 assistant 消息里的活跃思考态。
- 收到 `finish_reason` 或 `[DONE]`：关闭所有活跃思考态。
- 收到 `thinking_summary.final === true`：可以关闭对应 block，但不要依赖它总会出现。

## 历史 timeline 恢复

刷新会话时读取：

```http
GET /api/v1/agent/conversation-timeline?conversation_id={conversation_id}
```

统一响应仍是：

```json
{
  "status": "0",
  "info": "success",
  "data": []
}
```

`thinking_summary` timeline item 示例：

```json
{
  "id": 123,
  "seq": 8,
  "kind": "thinking_summary",
  "content": "正在把用户目标拆成可执行步骤，并检查是否需要补充约束。",
  "payload": {
    "stage": "plan",
    "block_id": "plan.speak",
    "display_mode": "append",
    "summary_seq": 1,
    "detail_summary": "正在把用户目标拆成可执行步骤，并检查是否需要补充约束。",
    "duration_seconds": 3.214
  },
  "created_at": "2026-04-28T21:00:00+08:00"
}
```

历史恢复规则：

- 只恢复 `detail_summary`，没有 `short_summary`。
- 按 timeline item 的 `seq` 排序渲染即可，后端已升序返回。
- 可用 `payload.block_id || payload.stage || "thinking"` 归组到对应 assistant 消息附近。
- 如果当前前端还没做跨事件归组，可以先把它渲染为 assistant 消息里的“思考摘要条目”，位置按 timeline 顺序插入。

建议更新现有前端类型：

```ts
export interface TimelineThinkingSummaryPayload {
  stage?: string
  block_id?: string
  display_mode?: 'append'
  summary_seq?: number
  detail_summary?: string
  duration_seconds?: number
  final?: boolean
}

export interface TimelineEvent {
  id: number
  seq: number
  kind:
    | 'user_text'
    | 'assistant_text'
    | 'tool_call'
    | 'tool_result'
    | 'confirm_request'
    | 'schedule_completed'
    | 'business_card'
    | 'thinking_summary'
  role?: 'user' | 'assistant'
  content?: string
  payload?: {
    stage?: string
    block_id?: string
    display_mode?: 'append' | 'replace' | 'card'
    thinking_summary?: never
    detail_summary?: string
    summary_seq?: number
    duration_seconds?: number
    final?: boolean
    tool?: TimelineToolPayload
    confirm?: TimelineConfirmPayload
    business_card?: TimelineBusinessCardPayload
  }
  tokens_consumed?: number
  created_at?: string
}
```

## 与正文/工具卡片的关系

同一轮流里可能出现：

1. `thinking_summary`
2. `tool_call` / `tool_result`
3. `assistant_text` 或 `delta.content`
4. `finish`
5. `[DONE]`

前端建议：

- `thinking_summary` 是“等待过程”组件。
- `tool_call` / `tool_result` 继续走现有工具卡片。
- `delta.content` 继续追加到 assistant 正文。
- `finish` / `[DONE]` 只负责收尾，不需要生成可见消息。

## 测试用例

### 1. 只有摘要，还没正文

输入事件：

```json
{
  "extra": {
    "kind": "thinking_summary",
    "block_id": "plan.speak",
    "stage": "plan",
    "display_mode": "append",
    "thinking_summary": {
      "summary_seq": 1,
      "short_summary": "正在理解需求",
      "detail_summary": "正在识别用户的目标、约束和需要补充的信息。",
      "duration_seconds": 2.1
    }
  }
}
```

预期：

- 折叠态显示“正在理解需求”。
- 展开态新增一条 detail。
- 正文区域不新增文字。

### 2. 多条摘要追加

输入 `summary_seq=1,2,3`。

预期：

- `latestShort` 使用第 3 条短摘要。
- `details` 有 3 条，按收到顺序或 seq 升序展示。

### 3. 乱序或重复摘要

已处理到 `summary_seq=3` 后，又收到 `summary_seq=2`。

预期：

- 忽略旧事件，不回退短摘要，不追加 detail。

### 4. 正文开始

收到：

```json
{
  "choices": [
    {
      "delta": { "content": "我整理好了，下面是建议：" }
    }
  ]
}
```

预期：

- 当前活跃思考块停止 loading 动效。
- 正文正常追加。
- 后续若仍意外收到同 block 摘要，可按 seq 处理，但 UI 上建议不再重新激活。

### 5. 历史恢复

timeline 返回 `kind=thinking_summary`。

预期：

- 只展示 `payload.detail_summary || content`。
- 不展示短摘要占位。
- 不需要显示 `state`，协议里已经没有这个字段。

## 最小改动清单

1. `StreamEventPayload.extra` 增加 `thinking_summary` 字段。
2. `TimelineEvent.kind` 增加 `thinking_summary`。
3. SSE 解析里在 `handleStreamExtraEvent` 增加 `extra.kind === "thinking_summary"` 分支。
4. 收到正文 `delta.content` 时，把当前思考摘要块置为非活跃。
5. 历史 timeline 恢复时支持 `kind === "thinking_summary"`，只恢复长摘要。
