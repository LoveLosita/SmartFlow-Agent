# NewAgent 时间线对接说明（前端）

## 1. 变更目标

后端已将 **正文消息** 与 **卡片事件（工具调用/确认/排程完成）** 统一落到会话时间线，刷新页面后可完整恢复，不再依赖“页面不刷新且持续订阅 SSE”才能看到卡片。

本次是开发环境直切，旧接口已退役：

- 退役：`GET /api/v1/agent/conversation-history`
- 新增：`GET /api/v1/agent/conversation-timeline`

## 2. 新接口

### 2.1 请求

`GET /api/v1/agent/conversation-timeline?conversation_id={conversation_id}`

鉴权与其他 agent 接口一致：JWT。

### 2.2 响应结构

`data` 是按顺序返回的数组，每项结构如下：

```json
{
  "id": 123,
  "seq": 1,
  "kind": "assistant_text",
  "role": "assistant",
  "content": "你好，我来帮你处理。",
  "payload": {
    "reasoning_content": "..."
  },
  "tokens_consumed": 0,
  "created_at": "2026-04-19T12:00:00+08:00"
}
```

字段说明：

- `seq`：同一会话内严格递增顺序号，前端渲染顺序以它为准。
- `kind`：事件类型（见下文映射表）。
- `role`/`content`：正文消息使用。
- `payload`：卡片渲染所需结构化信息。

## 3. kind 映射（前端渲染）

- `user_text`：用户正文气泡。
- `assistant_text`：助手正文气泡。
- `tool_call`：工具开始卡片。
- `tool_result`：工具结果卡片。
- `confirm_request`：确认卡片。
- `schedule_completed`：排程完成卡片（展示完成态，详情仍走原有排程查询接口）。

### 3.1 卡片 payload 结构

`tool_call` / `tool_result`：

```json
{
  "stage": "execute",
  "block_id": "execute.status",
  "display_mode": "card",
  "tool": {
    "name": "move",
    "status": "start|done|blocked|failed",
    "summary": "xxx",
    "arguments_preview": "xxx"
  }
}
```

`confirm_request`：

```json
{
  "stage": "confirm",
  "block_id": "confirm.status",
  "display_mode": "card",
  "confirm": {
    "interaction_id": "xxx",
    "title": "操作确认",
    "summary": "xxx"
  }
}
```

`schedule_completed`：

```json
{
  "stage": "deliver",
  "block_id": "deliver.status",
  "display_mode": "card"
}
```

## 4. 前端建议改造

### 4.1 会话初始化/刷新

进入会话页或刷新后，调用一次：

- `GET /api/v1/agent/conversation-timeline`

然后：

1. 直接按返回数组顺序渲染；
2. 不需要再拼接旧 `conversation-history`；
3. 旧接口调用逻辑可直接删除。

### 4.2 进行中会话（SSE）

进行中仍可继续消费 SSE（实时体验不变）。

建议策略：

1. 首屏先用 `conversation-timeline` 重建历史；
2. 新一轮聊天过程中继续把 SSE 增量渲染到当前 UI；
3. 页面刷新时再次拉 `conversation-timeline`，即可恢复完整状态。

## 5. 顺序保证

后端在写入时间线时为每个事件分配 `seq`，并将正文与卡片写入同一链路：

- 用户正文（`user_text`）
- 助手正文（`assistant_text`）
- 工具开始/结果（`tool_call`/`tool_result`）
- 确认卡片（`confirm_request`）
- 排程完成卡片（`schedule_completed`）

因此前端只要遵循 `seq` 顺序渲染，就不会出现“正文和卡片乱序”问题。

## 6. Token 说明

时间线中的 `tokens_consumed` 仅作为展示冗余字段，真实 token 账本统计仍以后端原有口径（`chat_histories` / `agent_chats`）为准。

