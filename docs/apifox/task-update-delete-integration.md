# 四象限任务编辑接口 — 前端对接文档

## 概述

新增两个任务编辑接口，用于在四象限卡片上直接修改任务属性和删除任务。

---

## 通用说明

### 认证

所有请求需在 Header 中携带 JWT access_token：

```
Authorization: Bearer <access_token>
```

### 幂等键

写操作接口需在 Header 中携带 `X-Idempotency-Key`，由前端生成唯一字符串（如 UUID）。

- 同一用户 + 同一幂等键的重复请求只执行一次
- 有效期 24 小时
- 缺少该 Header 会返回 `40037`

### 统一响应格式

```json
{
  "status": "10000",      // 状态码：10000=成功，4xxxx=客户端错误
  "info": "success",      // 描述文案
  "data": { ... }         // 业务数据（仅成功时存在）
}
```

---

## 1. 更新任务属性

**PUT** `/api/v1/task/update`

### 用途

修改任务的标题、象限、截止时间、紧急分界时间。**部分更新语义**：只传需要修改的字段，未传的字段保持不变。

### 请求头

| Header | 必填 | 说明 |
|--------|------|------|
| `Authorization` | 是 | `Bearer <access_token>` |
| `X-Idempotency-Key` | 是 | 幂等键（UUID） |

### 请求体

```json
{
  "task_id": 42,
  "title": "完成需求文档 v2",
  "priority_group": 2,
  "deadline_at": "2026-05-15T23:59:59+08:00",
  "urgency_threshold_at": "2026-05-10T00:00:00+08:00"
}
```

| 字段 | 类型 | 必填 | 说明 |
|------|------|------|------|
| `task_id` | int | **是** | 目标任务 ID |
| `title` | string? | 否 | 新标题，`null` 或不传表示不修改 |
| `priority_group` | int? | 否 | 新象限（1~4），`null` 或不传表示不修改 |
| `deadline_at` | datetime? | 否 | 新截止时间（ISO 8601），`null` 或不传表示不修改 |
| `urgency_threshold_at` | datetime? | 否 | 新紧急分界时间，`null` 或不传表示不修改 |

**priority_group 取值：**

| 值 | 含义 |
|----|------|
| 1 | 重要且紧急 |
| 2 | 重要不紧急 |
| 3 | 简单不重要 |
| 4 | 不简单不重要 |

> 至少需要传一个可更新字段（title / priority_group / deadline_at / urgency_threshold_at），否则返回 `40063`。

### 响应示例

**成功（200）：**

```json
{
  "status": "10000",
  "info": "success",
  "data": {
    "id": 42,
    "user_id": 1,
    "title": "完成需求文档 v2",
    "priority_group": 2,
    "status": "incomplete",
    "deadline": "2026-05-15 23:59:59",
    "is_completed": false
  }
}
```

**注意：** 响应中的 `deadline` 是格式化后的字符串（`"YYYY-MM-DD HH:mm:ss"`），与请求中的 `deadline_at`（ISO 8601）格式不同。

### 错误码

| status | info | 触发条件 |
|--------|------|----------|
| 40005 | wrong param type | JSON 解析失败 / 字段类型不匹配 |
| 40018 | invalid priority | priority_group 不在 1~4 范围 |
| 40037 | missing idempotency key | 缺少 X-Idempotency-Key Header |
| 40050 | wrong task id | task_id 不存在或不属于当前用户 |
| 40063 | no fields to update | 除 task_id 外没有传任何可更新字段 |

---

## 2. 删除任务

**DELETE** `/api/v1/task/delete`

### 用途

永久删除指定任务（硬删除，不可恢复）。

### 请求头

| Header | 必填 | 说明 |
|--------|------|------|
| `Authorization` | 是 | `Bearer <access_token>` |
| `X-Idempotency-Key` | 是 | 幂等键（UUID） |

### 请求体

```json
{
  "task_id": 42
}
```

| 字段 | 类型 | 必填 | 说明 |
|------|------|------|------|
| `task_id` | int | **是** | 要删除的任务 ID |

### 响应示例

**删除成功（200）：**

```json
{
  "status": "10000",
  "info": "success",
  "data": {
    "task_id": 42
  }
}
```

**重复删除 / 任务不存在（200，幂等信息码）：**

```json
{
  "status": "10003",
  "info": "task already deleted or not found"
}
```

> 注意：`10003` 也是 HTTP 200，不是错误。前端收到 10003 可视为删除成功（幂等）。

### 错误码

| status | info | 触发条件 |
|--------|------|----------|
| 40005 | wrong param type | JSON 解析失败 / 字段类型不匹配 |
| 40037 | missing idempotency key | 缺少 X-Idempotency-Key Header |
| 40050 | wrong task id | task_id <= 0 |
| 10003 | task already deleted or not found | 任务已删除或不存在（幂等，HTTP 200） |

---

## 前端对接建议

### 更新场景

1. 用户在四象限卡片上编辑标题、拖拽换象限、修改截止时间时，调用 `PUT /task/update`
2. 只传变化的字段，例如拖拽换象限时只传 `{ task_id, priority_group }`
3. 成功后用返回的 `data` 刷新本地状态，无需重新拉取列表

### 删除场景

1. 用户点击删除按钮时，调用 `DELETE /task/delete`
2. 收到 `10000` 或 `10003` 都视为成功，从本地列表移除该任务
3. 不需要二次确认删除是否成功——幂等机制保证安全

### 幂等键生成

```javascript
// 推荐使用 crypto.randomUUID()
const idempotencyKey = crypto.randomUUID();

// 或使用 uuid 库
import { v4 as uuidv4 } from 'uuid';
const idempotencyKey = uuidv4();
```

### Apifox 导入

OpenAPI 3.0 规范文件位于：`docs/apifox/task-update-delete.openapi.yaml`
