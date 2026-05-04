# 计划广场与 Token 商店前端对接说明

## 1. 文档目标

本文面向前端页面设计和接口联调，先把 P0 页面所需接口、字段、状态和交互流程定成初版。

当前确认口径：

1. 论坛展示名使用“计划广场”。
2. 前端正常展示评论，评论支持多层回复。
3. 用户可以删除自己的评论；P0 暂不引入管理员删评、举报和审核流。
4. 点赞奖励只给帖子作者。
5. 同一用户对同一计划只允许导入一次，奖励随该次导入记录一次。
6. Token 发放本轮不改 `user/auth`，先在 `token-store` 内封装 Token 获取途径和后续发放出口。

## 2. 通用约定

### 2.1 鉴权

P0 所有接口都需要登录态。

```http
Authorization: Bearer <access_token>
```

### 2.2 统一响应壳

成功响应：

```json
{
  "status": "10000",
  "info": "success",
  "data": {}
}
```

失败响应：

```json
{
  "status": "40004",
  "info": "计划不存在或已下架",
  "data": null
}
```

前端处理建议：

1. `status === "10000"` 视为成功。
2. 非成功状态优先展示 `info`。
3. 表单类错误可按接口返回的 `info` 直接提示，不需要前端硬编码太多文案。

### 2.3 幂等请求头

以下写操作建议前端带 `X-Idempotency-Key`：

1. 发布计划。
2. 发表评论。
3. 一键导入。
4. 创建订单。
5. mock paid。

```http
X-Idempotency-Key: plan_square_publish_1710000000000_abcd
```

### 2.4 时间格式

所有时间字段使用 ISO 8601 字符串，带时区。

```json
"2026-05-04T20:30:00+08:00"
```

### 2.5 分页结构

分页接口统一返回：

```json
{
  "items": [],
  "page": 1,
  "page_size": 20,
  "total": 125,
  "has_more": true
}
```

## 3. 页面总览

### 3.1 计划广场

前端建议页面：

1. 计划广场列表页：展示公开计划卡片，支持排序、标签筛选和搜索。
2. 计划详情页 / 详情抽屉：展示计划说明、任务条目预览、点赞、评论和导入按钮。
3. 发布计划弹窗 / 页面：选择自己的 TaskClass，填写标题、简介、标签后发布。
4. 评论区：展示多层评论树，支持回复和删除自己的评论。

### 3.2 Token 商店

前端建议页面：

1. Token 商品页：展示 Token 包商品卡片。
2. 订单确认 / mock paid 页：P0 可直接展示“确认支付”按钮。
3. Token 获取记录页：展示购买、点赞奖励、导入奖励等账本记录。
4. Token 概览组件：展示本服务记录的累计获取 Token。P0 不承诺已经同步到 `user/auth` 可用额度。

## 4. 路由总览

### 4.1 计划广场接口

| 功能 | 方法 | 路径 |
| --- | --- | --- |
| 计划列表 | `GET` | `/api/v1/plan-square/posts` |
| 热门标签 | `GET` | `/api/v1/plan-square/tags` |
| 发布计划 | `POST` | `/api/v1/plan-square/posts` |
| 计划详情 | `GET` | `/api/v1/plan-square/posts/{post_id}` |
| 点赞 | `POST` | `/api/v1/plan-square/posts/{post_id}/like` |
| 取消点赞 | `DELETE` | `/api/v1/plan-square/posts/{post_id}/like` |
| 评论树 | `GET` | `/api/v1/plan-square/posts/{post_id}/comments` |
| 发表评论 / 回复 | `POST` | `/api/v1/plan-square/posts/{post_id}/comments` |
| 删除自己的评论 | `DELETE` | `/api/v1/plan-square/comments/{comment_id}` |
| 一键导入 | `POST` | `/api/v1/plan-square/posts/{post_id}/import` |

发布计划时，TaskClass 下拉列表复用现有接口：

```http
GET /api/v1/task-class/list
```

### 4.2 Token 商店接口

| 功能 | 方法 | 路径 |
| --- | --- | --- |
| Token 概览 | `GET` | `/api/v1/token-store/summary` |
| 商品列表 | `GET` | `/api/v1/token-store/products` |
| 创建订单 | `POST` | `/api/v1/token-store/orders` |
| 订单列表 | `GET` | `/api/v1/token-store/orders` |
| 订单详情 | `GET` | `/api/v1/token-store/orders/{order_id}` |
| mock paid | `POST` | `/api/v1/token-store/orders/{order_id}/mock-paid` |
| Token 获取记录 | `GET` | `/api/v1/token-store/grants` |

## 5. 计划广场接口详情

### 5.1 计划列表

```http
GET /api/v1/plan-square/posts?page=1&page_size=20&sort=latest&keyword=数学&tag=考研
```

查询参数：

| 字段 | 类型 | 必填 | 说明 |
| --- | --- | --- | --- |
| `page` | number | 否 | 默认 `1` |
| `page_size` | number | 否 | 默认 `20`，最大建议 `50` |
| `sort` | string | 否 | `latest` / `likes` / `imports` |
| `keyword` | string | 否 | 搜索标题和简介 |
| `tag` | string | 否 | 标签筛选 |

成功响应：

```json
{
  "status": "10000",
  "info": "success",
  "data": {
    "items": [
      {
        "post_id": 10001,
        "title": "30 天高数强化复习计划",
        "summary": "适合期末前一个月快速过完重点题型。",
        "tags": ["高数", "期末", "30天"],
        "author": {
          "user_id": 88,
          "nickname": "小鹿同学",
          "avatar_url": ""
        },
        "template_summary": {
          "task_count": 24,
          "mode": "date_range",
          "start_date": "2026-05-05",
          "end_date": "2026-06-04",
          "strategy_labels": ["每日推进", "错题复盘"]
        },
        "counters": {
          "like_count": 128,
          "comment_count": 32,
          "import_count": 45
        },
        "viewer_state": {
          "liked": false,
          "imported_once": true
        },
        "status": "published",
        "created_at": "2026-05-04T20:30:00+08:00"
      }
    ],
    "page": 1,
    "page_size": 20,
    "total": 125,
    "has_more": true
  }
}
```

前端展示建议：

1. 卡片主标题用 `title`。
2. 副文案用 `summary`，最多展示两到三行。
3. 标签 chips 用 `tags`。
4. 统计区展示点赞、评论、导入次数。
5. `viewer_state.liked` 控制点赞按钮初始态。

### 5.2 热门标签

```http
GET /api/v1/plan-square/tags?limit=20
```

成功响应：

```json
{
  "status": "10000",
  "info": "success",
  "data": {
    "items": [
      { "tag": "考研", "post_count": 30 },
      { "tag": "高数", "post_count": 18 }
    ]
  }
}
```

### 5.3 发布计划

```http
POST /api/v1/plan-square/posts
Content-Type: application/json
X-Idempotency-Key: plan_square_publish_1710000000000_abcd
```

请求体：

```json
{
  "task_class_id": 42,
  "title": "30 天高数强化复习计划",
  "summary": "适合期末前一个月快速过完重点题型。",
  "tags": ["高数", "期末", "30天"]
}
```

字段限制：

| 字段 | 规则 |
| --- | --- |
| `task_class_id` | 必填，只能选择当前用户自己的 TaskClass |
| `title` | 必填，建议 4 到 40 字 |
| `summary` | 可选，建议 0 到 300 字 |
| `tags` | 可选，最多 5 个，每个最多 12 字 |

成功响应：

```json
{
  "status": "10000",
  "info": "success",
  "data": {
    "post_id": 10001,
    "title": "30 天高数强化复习计划",
    "status": "published",
    "created_at": "2026-05-04T20:30:00+08:00"
  }
}
```

前端流程建议：

1. 先调用 `GET /api/v1/task-class/list` 获取用户自己的计划。
2. 用户选择一个 TaskClass 后填写发布信息。
3. 发布成功后跳转到详情页或刷新计划广场列表。

### 5.4 计划详情

```http
GET /api/v1/plan-square/posts/{post_id}
```

成功响应：

```json
{
  "status": "10000",
  "info": "success",
  "data": {
    "post": {
      "post_id": 10001,
      "title": "30 天高数强化复习计划",
      "summary": "适合期末前一个月快速过完重点题型。",
      "tags": ["高数", "期末", "30天"],
      "author": {
        "user_id": 88,
        "nickname": "小鹿同学",
        "avatar_url": ""
      },
      "counters": {
        "like_count": 128,
        "comment_count": 32,
        "import_count": 45
      },
      "viewer_state": {
        "liked": false,
        "imported_once": true
      },
      "created_at": "2026-05-04T20:30:00+08:00"
    },
    "template": {
      "mode": "date_range",
      "start_date": "2026-05-05",
      "end_date": "2026-06-04",
      "strategy_labels": ["每日推进", "错题复盘"],
      "task_count": 24,
      "items_preview": [
        {
          "item_id": 90001,
          "order": 1,
          "content": "复习极限与连续，完成基础例题。"
        },
        {
          "item_id": 90002,
          "order": 2,
          "content": "整理导数公式，完成 10 道综合题。"
        }
      ]
    }
  }
}
```

说明：

1. `items_preview` 是模板快照，不是原作者当前 TaskClass。
2. 不展示 `embedded_time`、schedule 绑定和用户私有排程状态。
3. 评论区单独调用评论树接口，避免详情首屏过重。

### 5.5 点赞

```http
POST /api/v1/plan-square/posts/{post_id}/like
```

成功响应：

```json
{
  "status": "10000",
  "info": "success",
  "data": {
    "post_id": 10001,
    "liked": true,
    "like_count": 129,
    "reward_hint": {
      "receiver": "author",
      "status": "recorded",
      "amount": 1
    }
  }
}
```

说明：

1. 同一用户对同一帖子只能点赞一次。
2. 点赞奖励只给作者。
3. 取消点赞不回滚已经记录的作者奖励，避免账本反复冲正。

### 5.6 取消点赞

```http
DELETE /api/v1/plan-square/posts/{post_id}/like
```

成功响应：

```json
{
  "status": "10000",
  "info": "success",
  "data": {
    "post_id": 10001,
    "liked": false,
    "like_count": 128
  }
}
```

### 5.7 评论树

```http
GET /api/v1/plan-square/posts/{post_id}/comments?page=1&page_size=20&sort=oldest
```

查询参数：

| 字段 | 类型 | 必填 | 说明 |
| --- | --- | --- | --- |
| `page` | number | 否 | 根评论分页，默认 `1` |
| `page_size` | number | 否 | 根评论分页大小，默认 `20` |
| `sort` | string | 否 | `oldest` / `latest`，默认 `oldest` |

成功响应：

```json
{
  "status": "10000",
  "info": "success",
  "data": {
    "items": [
      {
        "comment_id": 50001,
        "post_id": 10001,
        "parent_comment_id": null,
        "content": "这个计划很适合期末冲刺。",
        "status": "visible",
        "author": {
          "user_id": 91,
          "nickname": "西瓜同学",
          "avatar_url": ""
        },
        "can_delete": true,
        "created_at": "2026-05-04T20:40:00+08:00",
        "deleted_at": null,
        "children": [
          {
            "comment_id": 50002,
            "post_id": 10001,
            "parent_comment_id": 50001,
            "content": "我也准备照这个导入一份。",
            "status": "visible",
            "author": {
              "user_id": 92,
              "nickname": "青柠同学",
              "avatar_url": ""
            },
            "can_delete": false,
            "created_at": "2026-05-04T20:42:00+08:00",
            "deleted_at": null,
            "children": []
          }
        ]
      }
    ],
    "page": 1,
    "page_size": 20,
    "total": 32,
    "has_more": true
  }
}
```

评论状态：

| `status` | 前端展示 |
| --- | --- |
| `visible` | 正常展示 `content` |
| `deleted` | 展示“该评论已删除”，保留子回复 |

### 5.8 发表评论 / 回复

```http
POST /api/v1/plan-square/posts/{post_id}/comments
Content-Type: application/json
X-Idempotency-Key: plan_square_comment_1710000000000_abcd
```

请求体：

```json
{
  "content": "这个计划很适合期末冲刺。",
  "parent_comment_id": null
}
```

回复某条评论时：

```json
{
  "content": "我也准备照这个导入一份。",
  "parent_comment_id": 50001
}
```

字段限制：

| 字段 | 规则 |
| --- | --- |
| `content` | 必填，建议 1 到 500 字 |
| `parent_comment_id` | 可选，传 `null` 表示根评论 |

成功响应：

```json
{
  "status": "10000",
  "info": "success",
  "data": {
    "comment_id": 50003,
    "post_id": 10001,
    "parent_comment_id": 50001,
    "content": "我也准备照这个导入一份。",
    "status": "visible",
    "can_delete": true,
    "created_at": "2026-05-04T20:45:00+08:00"
  }
}
```

### 5.9 删除自己的评论

```http
DELETE /api/v1/plan-square/comments/{comment_id}
```

成功响应：

```json
{
  "status": "10000",
  "info": "success",
  "data": {
    "comment_id": 50003,
    "status": "deleted",
    "content": "",
    "deleted_at": "2026-05-04T20:50:00+08:00"
  }
}
```

说明：

1. 只能删除自己的评论。
2. 删除是软删除，不删除子回复。
3. 非本人删除返回失败，前端展示 `info` 即可。

### 5.10 一键导入

```http
POST /api/v1/plan-square/posts/{post_id}/import
Content-Type: application/json
X-Idempotency-Key: plan_square_import_1710000000000_abcd
```

请求体：

```json
{
  "target_title": "我的 30 天高数强化计划"
}
```

字段说明：

| 字段 | 必填 | 说明 |
| --- | --- | --- |
| `target_title` | 否 | 导入后的 TaskClass 名称；不传则后端生成默认名称 |

成功响应：

```json
{
  "status": "10000",
  "info": "success",
  "data": {
    "import_id": 70001,
    "post_id": 10001,
    "new_task_class_id": 430,
    "task_class_title": "我的 30 天高数强化计划",
    "import_count": 46,
    "reward_hint": {
      "receiver": "author",
      "status": "recorded",
      "amount": 2
    },
    "next_action": {
      "type": "open_task_class",
      "task_class_id": 430
    },
    "created_at": "2026-05-04T20:55:00+08:00"
  }
}
```

说明：

1. 一键导入只创建当前用户自己的 TaskClass 副本。
2. 不直接写 schedule。
3. 同一用户同一帖子只允许导入一次；导入成功后前端根据返回结果刷新 `imported_once` 和按钮状态。

## 6. Token 商店接口详情

### 6.1 Token 概览

```http
GET /api/v1/token-store/summary
```

成功响应：

```json
{
  "status": "10000",
  "info": "success",
  "data": {
    "recorded_token_total": 120,
    "applied_token_total": 0,
    "pending_apply_token_total": 120,
    "quota_sync_status": "not_connected",
    "tip": "当前为 Token 获取记录，后续会切换到 user/auth 权威额度。"
  }
}
```

字段说明：

| 字段 | 说明 |
| --- | --- |
| `recorded_token_total` | `token-store` 内已经记录的累计获取 Token |
| `applied_token_total` | 已同步到权威额度的 Token，P0 通常为 `0` |
| `pending_apply_token_total` | 已记录但尚未同步到权威额度的 Token |
| `quota_sync_status` | `not_connected` / `partial` / `synced` |

前端展示建议：

1. P0 可以展示“累计获取 Token”，不要写成“当前可用 Token”。
2. `quota_sync_status === "not_connected"` 时，展示轻提示，不阻塞购买流程。

### 6.2 商品列表

```http
GET /api/v1/token-store/products
```

说明：

1. 商品从 `token_products` 表读取。
2. P0 商品由后端 seed 初始化，不做商品管理后台。

成功响应：

```json
{
  "status": "10000",
  "info": "success",
  "data": {
    "items": [
      {
        "product_id": 1,
        "name": "基础 Token 包",
        "description": "适合轻量使用 Agent。",
        "token_amount": 100,
        "price_cent": 990,
        "price_text": "¥9.90",
        "currency": "CNY",
        "badge": "入门",
        "status": "active",
        "sort_order": 10
      },
      {
        "product_id": 2,
        "name": "进阶 Token 包",
        "description": "适合高频规划和复盘。",
        "token_amount": 300,
        "price_cent": 1990,
        "price_text": "¥19.90",
        "currency": "CNY",
        "badge": "推荐",
        "status": "active",
        "sort_order": 20
      }
    ]
  }
}
```

前端展示建议：

1. 只展示 `status === "active"` 的商品。
2. `badge` 可作为角标，不保证一定有值。

### 6.3 创建订单

```http
POST /api/v1/token-store/orders
Content-Type: application/json
X-Idempotency-Key: token_order_1710000000000_abcd
```

请求体：

```json
{
  "product_id": 1,
  "quantity": 1
}
```

成功响应：

```json
{
  "status": "10000",
  "info": "success",
  "data": {
    "order_id": 80001,
    "order_no": "TS202605042055000001",
    "status": "pending",
    "product_snapshot": {
      "product_id": 1,
      "name": "基础 Token 包",
      "token_amount": 100
    },
    "quantity": 1,
    "token_amount": 100,
    "amount_cent": 990,
    "price_text": "¥9.90",
    "currency": "CNY",
    "payment_mode": "mock",
    "payment_action": {
      "type": "mock_paid",
      "label": "确认支付"
    },
    "created_at": "2026-05-04T20:55:00+08:00"
  }
}
```

### 6.4 订单列表

```http
GET /api/v1/token-store/orders?page=1&page_size=20&status=pending
```

查询参数：

| 字段 | 类型 | 必填 | 说明 |
| --- | --- | --- | --- |
| `page` | number | 否 | 默认 `1` |
| `page_size` | number | 否 | 默认 `20` |
| `status` | string | 否 | `pending` / `paid` / `granted` / `closed` |

成功响应：

```json
{
  "status": "10000",
  "info": "success",
  "data": {
    "items": [
      {
        "order_id": 80001,
        "order_no": "TS202605042055000001",
        "status": "pending",
        "product_name": "基础 Token 包",
        "token_amount": 100,
        "price_text": "¥9.90",
        "created_at": "2026-05-04T20:55:00+08:00",
        "paid_at": null,
        "granted_at": null
      }
    ],
    "page": 1,
    "page_size": 20,
    "total": 1,
    "has_more": false
  }
}
```

订单状态：

| `status` | 前端含义 |
| --- | --- |
| `pending` | 待支付 |
| `paid` | 已支付，等待写入获取记录 |
| `granted` | 已写入 Token 获取记录 |
| `closed` | 已关闭 |

### 6.5 订单详情

```http
GET /api/v1/token-store/orders/{order_id}
```

成功响应：

```json
{
  "status": "10000",
  "info": "success",
  "data": {
    "order_id": 80001,
    "order_no": "TS202605042055000001",
    "status": "pending",
    "product_snapshot": {
      "product_id": 1,
      "name": "基础 Token 包",
      "token_amount": 100
    },
    "quantity": 1,
    "token_amount": 100,
    "amount_cent": 990,
    "price_text": "¥9.90",
    "currency": "CNY",
    "payment_mode": "mock",
    "grant": null,
    "created_at": "2026-05-04T20:55:00+08:00",
    "paid_at": null,
    "granted_at": null
  }
}
```

### 6.6 mock paid

```http
POST /api/v1/token-store/orders/{order_id}/mock-paid
Content-Type: application/json
X-Idempotency-Key: token_mock_paid_1710000000000_abcd
```

请求体：

```json
{
  "mock_channel": "dev"
}
```

成功响应：

```json
{
  "status": "10000",
  "info": "success",
  "data": {
    "order_id": 80001,
    "order_no": "TS202605042055000001",
    "status": "granted",
    "paid_at": "2026-05-04T21:00:00+08:00",
    "granted_at": "2026-05-04T21:00:01+08:00",
    "grant": {
      "grant_id": 90001,
      "event_id": "order:80001:paid",
      "source": "purchase",
      "amount": 100,
      "status": "recorded",
      "quota_applied": false
    }
  }
}
```

说明：

1. P0 不接真实支付网关。
2. `status === "granted"` 表示已经写入 `token-store` 获取记录。
3. `quota_applied === false` 表示本轮未同步到 `user/auth` 权威额度。

### 6.7 Token 获取记录

```http
GET /api/v1/token-store/grants?page=1&page_size=20&source=purchase
```

查询参数：

| 字段 | 类型 | 必填 | 说明 |
| --- | --- | --- | --- |
| `page` | number | 否 | 默认 `1` |
| `page_size` | number | 否 | 默认 `20` |
| `source` | string | 否 | `purchase` / `forum_like` / `forum_import` / `manual` |

成功响应：

```json
{
  "status": "10000",
  "info": "success",
  "data": {
    "items": [
      {
        "grant_id": 90001,
        "event_id": "order:80001:paid",
        "source": "purchase",
        "source_label": "购买 Token 包",
        "amount": 100,
        "status": "recorded",
        "quota_applied": false,
        "description": "购买基础 Token 包",
        "created_at": "2026-05-04T21:00:01+08:00"
      },
      {
        "grant_id": 90002,
        "event_id": "forum.post.liked:10001:91",
        "source": "forum_like",
        "source_label": "计划被点赞",
        "amount": 1,
        "status": "recorded",
        "quota_applied": false,
        "description": "你的计划《30 天高数强化复习计划》获得点赞",
        "created_at": "2026-05-04T21:05:00+08:00"
      }
    ],
    "page": 1,
    "page_size": 20,
    "total": 2,
    "has_more": false
  }
}
```

获取记录状态：

| `status` | 前端含义 |
| --- | --- |
| `recorded` | 已记录获取事实，P0 默认状态 |
| `applied` | 已同步到权威额度，后续切 `user/auth` 后使用 |
| `skipped` | 幂等重复或规则不发放 |
| `failed` | 写入或后续发放失败 |

## 7. 页面交互建议

### 7.1 计划广场列表页

1. 首屏调用计划列表接口，默认 `sort=latest`。
2. 顶部筛选可放搜索框、排序切换和热门标签。
3. 卡片 CTA 建议包含“查看详情”和“一键导入”。
4. 点赞按钮可在卡片上直接操作，成功后用接口返回的 `like_count` 覆盖本地计数。

### 7.2 计划详情页

1. 进入详情页先拉计划详情。
2. 评论区单独拉评论树。
3. 发表评论或删除评论成功后，建议重新拉评论树，避免多层结构局部更新出错。
4. 一键导入成功后，可弹出成功反馈，并提供“查看我的 TaskClass”入口。

### 7.3 发布计划

1. 打开发布弹窗时调用现有 `GET /api/v1/task-class/list`。
2. 用户选择 TaskClass 后填写标题、简介、标签。
3. 发布按钮需要防重复点击，并发送 `X-Idempotency-Key`。
4. 发布成功后跳转详情页或刷新列表。

### 7.4 Token 商店

1. 首屏并行调用 Token 概览和商品列表。
2. 商品卡片点击“购买”后创建订单。
3. P0 下单后展示 mock paid 确认按钮。
4. mock paid 成功后刷新 Token 概览和获取记录。
5. P0 文案建议使用“累计获取 Token”，暂不写“可用 Token 已到账”。

## 8. 前端类型参考

```ts
type ApiResponse<T> = {
  status: string
  info: string
  data: T
}

type PageData<T> = {
  items: T[]
  page: number
  page_size: number
  total: number
  has_more: boolean
}

type UserBrief = {
  user_id: number
  nickname: string
  avatar_url: string
}

type PlanSquarePostCard = {
  post_id: number
  title: string
  summary: string
  tags: string[]
  author: UserBrief
  template_summary: {
    task_count: number
    mode: string
    start_date: string
    end_date: string
    strategy_labels: string[]
  }
  counters: {
    like_count: number
    comment_count: number
    import_count: number
  }
  viewer_state: {
    liked: boolean
    imported_once: boolean
  }
  status: 'published'
  created_at: string
}

type CommentNode = {
  comment_id: number
  post_id: number
  parent_comment_id: number | null
  content: string
  status: 'visible' | 'deleted'
  author: UserBrief
  can_delete: boolean
  created_at: string
  deleted_at: string | null
  children: CommentNode[]
}

type TokenProduct = {
  product_id: number
  name: string
  description: string
  token_amount: number
  price_cent: number
  price_text: string
  currency: 'CNY'
  badge: string
  status: 'active' | 'inactive'
  sort_order: number
}

type TokenGrant = {
  grant_id: number
  event_id: string
  source: 'purchase' | 'forum_like' | 'forum_import' | 'manual'
  source_label: string
  amount: number
  status: 'recorded' | 'applied' | 'skipped' | 'failed'
  quota_applied: boolean
  description: string
  created_at: string
}
```

## 9. P0 不需要前端处理的内容

1. 不做计划推荐算法，只做排序和筛选。
2. 不做关注、私信、用户主页。
3. 不做富文本，简介和评论都是纯文本。
4. 不做管理员删评、举报和审核后台。
5. 不接真实支付网关。
6. 不直接展示 `user/auth` 权威可用额度，除非后续接口已经切通。
