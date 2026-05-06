# 统一出口 Credit 计费最终计划

## 1. 文档目的

本计划用于把当前系统从“各业务服务进程内直接持有 LLM 组件、调用结束后再走旧 token 累加”的模式，重构为：

1. `LLM` 成为真正的独立服务，统一掌管模型调用入口、出口、usage 采集与计费事件发布。
2. `TokenStore` 服务切换为 Credit 语义权威服务，掌管余额、商品、订单、流水与最终扣费落账。
3. 前端商店直接消费 Credit 语义接口，并能展示“当前登录用户自己的 Credit 流水”。

这份计划改成“三步走”版本，便于直接按阶段批准与执行。

---

## 2. 总体结论

### 2.1 核心结论

1. `backend/services/llm` 不能再以“进程内共享组件”存在，必须升级为独立服务。
2. 所有用户态模型调用统一经过 `LLM RPC`，由 `LLM` 服务统一采集 usage。
3. `LLM` 服务自己做同步准入 `CreditBalanceGuard`，并使用 Redis 余额快照提速。
4. `LLM` 服务不做同步扣费，也不允许裸 goroutine 异步扣费。
5. `LLM` 服务在拿到最终 usage 后，把 `credit.charge.requested` 写入 **自己的 outbox 表**。
6. `TokenStore` 服务异步消费扣费事件，更新 Credit 余额与流水。
7. 旧 token 累加链路、旧 token 商店链路和旧 `/token-store` 接口本轮直接删除，不保留兼容层。

### 2.2 关键原则

1. **统一出口**：所有 LLM 计费只在 `LLM` 服务出口发生，不允许业务层各自重复扣费。
2. **强制入参**：不能只靠 `Metadata` 约定传 `user_id`，必须改 RPC/函数签名，让漏传在编译期报错。
3. **准入同步、结算异步**：调用模型前同步做 `CreditBalanceGuard`，拿到最终 usage 后通过 outbox 异步结算。
4. **旧商店直删**：旧 token 商店前后端都未真正接线，本轮不做 token/credit 双轨并存。
5. **调用面兼容优先**：`LLM RPC` 可以新增内部协议，但 `backend/client/llm` 对业务侧暴露的调用方式应尽量贴近当前 `services/llm`，把迁移成本压到最低。

---

## 3. 为什么要这样改

### 3.1 当前最大问题

1. `services/llm` 名字像服务，实际上不是服务，而是多个进程直接 new 的本地组件。
2. 计费边界分散，谁发起调用谁就得自己记账，很容易遗漏。
3. 当前只有旧 `token_usage` 累加，没有人民币成本与 Credit 扣费真相。
4. `userauth` 当前承担的是 `token quota` 门禁，不是 Credit 余额门禁。
5. 旧 token 商店前后端都没有真正接上，因此没有保留兼容层的价值。

### 3.2 这次改完后的好处

1. LLM 调用入口统一。
2. usage 采集统一。
3. 余额准入统一。
4. 扣费事件发布统一。
5. TokenStore 只做 Credit 权威账本，不再和模型调用散乱耦合。

---

## 4. 三步走总览

### 第一步：把 LLM 变成真正独立服务

目标：

1. 所有模型调用统一改走 `LLM RPC`
2. `LLM` 服务拥有自己的 `CreditBalanceGuard`
3. `LLM` 服务拥有自己的 outbox 表

### 第二步：把 TokenStore 改造成 Credit 权威服务

目标：

1. 所有余额、流水、商品、订单都切成 Credit 语义
2. 异步消费 `credit.charge.requested`
3. 提供“查看自己流水”的正式接口

### 第三步：切 Gateway 与前端，并删旧链路

目标：

1. 上线 `/credit-store/*`
2. 商店页接 Credit 真接口与流水列表
3. 删掉旧 token 累加和旧 token 商店链路

---

## 5. 第一步：把 LLM 变成真正独立服务

## 5.1 这一步的目标

把当前“各进程内直接持有 LLM 组件”的模式，改成“统一调 LLM 服务”。

## 5.2 新增/改造内容

### 5.2.1 新增独立 LLM 进程与 RPC

新增：

1. `backend/cmd/llm/main.go`
2. `backend/client/llm/client.go`
3. `backend/services/llm/rpc/pb/llm.proto`
4. `backend/services/llm/rpc/handler.go`
5. `backend/services/llm/rpc/server.go`
6. `backend/services/llm/dao/connect.go`

说明：

1. 这样 `services/llm` 放在 `services` 目录下才真正名副其实。
2. 业务服务以后全部只依赖 `client/llm`，不再直接 import `services/llm` 的本地组件。

### 5.2.2 统一 RPC 能力，但业务侧调用面尽量保持原状

服务间 `LLM RPC` 至少提供三类底层能力：

1. `GenerateText`
2. `StreamText`
3. `GenerateResponsesText`

但是对业务代码，不建议直接散落调用底层 RPC 方法名，而是由 `backend/client/llm` 继续暴露与当前本地 `llm client` 基本一致的门面：

1. `(*Client).GenerateText(ctx, messages, options)`
2. `GenerateJSON[T](ctx, client, messages, options)`
3. `(*Client).Stream(ctx, messages, options)`
4. `(*ArkResponsesClient).GenerateText(ctx, messages, options)`
5. `GenerateArkResponsesJSON[T](ctx, client, messages, options)`

迁移目标：

1. 业务层尽量只替换 client 来源，不重写 prompt 组织方式。
2. `GenerateOptions`、`ArkResponsesOptions` 现有字段语义尽量不变。
3. `generateJson / stream / responses json` 这些现有能力名和使用习惯尽量延续，避免把改造面扩大成“全链路重写调用协议”。

使用分工：

1. `GenerateText`：memory、active-scheduler、agent 非流式调用
2. `StreamText`：agent chat / plan / execute / deliver
3. `GenerateResponsesText`：course 图片解析

### 5.2.3 统一 BillingContext，但不要污染现有 GenerateOptions

所有请求都必须携带：

```go
type BillingContext struct {
    UserID         int
    EventID        string
    Scene          string
    RequestID      string
    ConversationID string
    ModelAlias     string
    SkipCharge     bool
}
```

要求：

1. `UserID` 必须明确，普通用户态调用禁止省略。
2. `EventID` 必须稳定，作为幂等扣费键。
3. `Scene` 必须明确，方便价格表与审计。
4. `SkipCharge` 只允许系统内部场景显式声明。
5. `BillingContext` 不再塞进 `GenerateOptions.Metadata` 里，避免继续靠弱约定传关键字段。
6. `GenerateOptions`、`ArkResponsesOptions` 继续只承载模型行为参数，例如 `Temperature / MaxTokens / Thinking`。

落地建议：

1. `client/llm` 对调用方继续保留现有 `GenerateText / GenerateJSON / Stream / Responses` 风格方法。
2. 计费必填信息通过独立的调用包装层传入，例如“绑定过 BillingContext 的 client”或“显式 request 参数对象”，但要由 `client/llm` 吸收复杂度。
3. 调用方改造目标应控制在“补一处 BillingContext 组装 + 替换 client 初始化来源”，而不是每个业务点重写整段调用代码。

### 5.2.4 LLM 服务自己的 CreditBalanceGuard

这一步必须同时完成，否则独立 LLM 服务没有准入价值。

Guard 设计要求：

1. 模型真正发起前同步执行。
2. 不直接查 MySQL 真相表，必须走 Redis 快照优先。
3. 缓存 miss 或过期时，再回源 `TokenStore RPC`。

建议缓存 key：

1. `smartflow:credit_balance_snapshot:{user_id}`
2. `smartflow:credit_balance_blocked:{user_id}`

执行顺序：

1. 先查 `blocked` 键
2. 再查 `snapshot` 键
3. miss 时调用 TokenStore 获取余额快照
4. 若余额不足，写短 TTL 的 `blocked` 键

### 5.2.5 LLM 服务自己的 outbox

这一步也必须同时完成。

新增：

1. `llm_outbox_messages`

并在 outbox 目录注册：

1. `ServiceLLM`
2. `ServiceNameLLM`
3. `smartflow.llm.outbox`
4. `smartflow-llm-outbox-consumer`

涉及文件：

1. `backend/shared/infra/outbox/service_catalog.go`
2. `backend/shared/infra/outbox/service_route.go`
3. `backend/services/llm/dao/connect.go`

### 5.2.6 LLM 服务统一 usage 采集

在 `LLM` 服务内部统一输出：

```go
type BillingUsage struct {
    InputTokens     int64
    OutputTokens    int64
    CachedTokens    int64
    ReasoningTokens int64
    TotalTokens     int64
    ModelName       string
    ProviderName    string
}
```

### 5.2.7 LLM 服务只发 charge 事件，不同步扣费

新增事件：

1. `credit.charge.requested`

事件载荷至少包含：

1. `event_id`
2. `user_id`
3. `scene`
4. `request_id`
5. `conversation_id`
6. `provider_name`
7. `model_name`
8. `input_tokens`
9. `output_tokens`
10. `cached_tokens`
11. `reasoning_tokens`
12. `rmb_cost_micros`
13. `credit_cost`
14. `triggered_at`

时机：

1. 拿到最终 usage
2. 算出本次 `credit_cost`
3. 写入 `llm_outbox_messages`
4. 结果即可返回调用方

## 5.3 这一步涉及哪些调用方改造

以下服务都要从“本地 import `services/llm`”改成“调用 `client/llm`”：

1. `agent`
2. `memory`
3. `active-scheduler`
4. `course`

---

## 6. 第二步：把 TokenStore 改造成 Credit 权威服务

## 6.1 这一步的目标

让 `TokenStore` 不再只是“充值/奖励入账中心”，而是：

1. Credit 余额真相
2. Credit 商品与订单中心
3. Credit 流水中心
4. LLM 扣费事件的最终结算方

## 6.2 新的 Credit 数据表

本轮不复用旧 `token_*` 表，直接建立纯 Credit 语义的新表：

1. `credit_products`
2. `credit_orders`
3. `credit_accounts`
4. `credit_ledger`
5. `credit_price_rules`
6. `credit_reward_rules`

### 6.2.1 `credit_products`

用途：商店展示的 Credit 商品

核心字段：

1. `sku`
2. `name`
3. `description`
4. `credit_amount`
5. `price_cent`
6. `currency`
7. `badge`
8. `status`
9. `sort_order`

### 6.2.2 `credit_orders`

用途：购买订单与 mock paid 状态机

核心字段：

1. `order_no`
2. `user_id`
3. `product_id`
4. `product_snapshot_json`
5. `quantity`
6. `credit_amount`
7. `amount_cent`
8. `status`
9. `payment_mode`
10. `idempotency_key`
11. `paid_at`
12. `credited_at`

### 6.2.3 `credit_accounts`

用途：余额真相与门禁回源表

核心字段：

1. `user_id`
2. `balance`
3. `total_recharged`
4. `total_rewarded`
5. `total_consumed`
6. `version`

### 6.2.4 `credit_ledger`

用途：统一正负流水，也是“查看自己流水”的唯一真相源

核心字段：

1. `event_id`
2. `user_id`
3. `direction`
4. `source`
5. `scene`
6. `description`
7. `credit_delta`
8. `balance_after`
9. `input_tokens`
10. `output_tokens`
11. `cached_tokens`
12. `reasoning_tokens`
13. `rmb_cost_micros`
14. `created_at`

### 6.2.5 `credit_price_rules`

用途：模型价格表

核心字段：

1. `provider_name`
2. `model_name`
3. `scene`
4. `input_price_micros_per_1k`
5. `output_price_micros_per_1k`
6. `cached_price_micros_per_1k`
7. `reasoning_price_micros_per_1k`
8. `credits_per_yuan`
9. `status`

### 6.2.6 `credit_reward_rules`

用途：社区奖励规则

核心字段：

1. `source`
2. `name`
3. `credit_amount`
4. `status`
5. `config_json`

## 6.3 TokenStore 如何消费 charge 事件

新增消费逻辑：

1. 订阅 `credit.charge.requested`
2. 幂等校验 `event_id`
3. 在事务中写 `credit_ledger`
4. 扣减 `credit_accounts.balance`
5. 刷新或删除 Redis 余额快照

## 6.4 用户查看自己流水接口

这是本轮必须落的能力。

对外新增：

1. `ListCreditTransactions`

要求：

1. 明确用于“当前登录用户查看自己的 Credit 流水”
2. 必须覆盖购买入账、社区奖励入账、LLM 消费扣费三类记录

HTTP 接口：

1. `GET /api/v1/credit-store/transactions`

必须满足：

1. 只返回当前登录用户自己的 Credit 流水
2. 支持分页：`page`、`page_size`
3. 支持可选筛选：`direction`、`source`、`scene`
4. 返回字段至少包含：
   - `transaction_id`
   - `event_id`
   - `direction`
   - `source`
   - `scene`
   - `description`
   - `credit_delta`
   - `balance_after`
   - `created_at`

## 6.5 这一步要删除什么旧东西

直接删除：

1. `token_products`
2. `token_orders`
3. `token_grants`
4. `token_reward_rules`

---

## 7. 第三步：切 Gateway/前端并删旧链路

## 7.1 这一步的目标

1. 上线 `/credit-store/*`
2. 商店页接 Credit 真接口
3. 删掉旧 token 累加和旧 token 商店链路

## 7.2 Gateway 切换

只保留 Credit 语义路由：

1. `GET /api/v1/credit-store/summary`
2. `GET /api/v1/credit-store/products`
3. `POST /api/v1/credit-store/orders`
4. `POST /api/v1/credit-store/orders/:order_id/mock-paid`
5. `GET /api/v1/credit-store/transactions`

直接删除：

1. `/token-store/*`
2. `backend/gateway/api/tokenstoreapi/*`

## 7.3 前端商店页切换

商店页必须改成：

1. 接 `/credit-store/*`
2. 新增“我的 Credit 流水”展示区
3. 流水数据源只允许来自 `GET /api/v1/credit-store/transactions`
4. 至少展示：
   - 流水类型
   - 流水说明
   - Credit 增减值
   - 流水发生时间
   - 可选余额快照

## 7.4 旧 token 累加机制下线

直接删除：

1. `backend/services/runtime/eventsvc/chat_token_usage_adjust.go`
2. `backend/services/runtime/model/agent.go` 中 `ChatTokenUsageAdjustPayload`
3. `backend/services/agent/sv/agent_graph.go` 中旧 token 调整调用
4. `backend/services/agent/sv/agent_meta.go` 中旧 token 调整调用
5. `backend/services/runtime/dao/agent.go` 中 `tokens_total` 累加写路径
6. `backend/gateway/middleware/token_quota_guard.go` 在聊天主链的使用
7. `userauth` 中 `CheckTokenQuota / AdjustTokenUsage` 的计费用途
8. `agent_chats.tokens_total` 字段

---

## 8. 这三步分别改哪些地方

### 第一步改动面

1. `backend/services/llm/*`
2. `backend/client/llm/*`
3. `backend/cmd/llm/*`
4. `backend/shared/infra/outbox/service_catalog.go`
5. `backend/shared/infra/outbox/service_route.go`
6. `agent / memory / active-scheduler / course` 的所有本地 LLM 调用点

### 第二步改动面

1. `backend/services/tokenstore/*`
2. `backend/client/tokenstore/*`
3. TokenStore 的 outbox consumer
4. Redis 余额快照与失效逻辑

### 第三步改动面

1. `backend/gateway/*`
2. `frontend/src/views/StoreView.vue`
3. 可能新增商店流水列表组件
4. `runtime/userauth` 旧 token 累加链删除

---

## 9. 验证清单

### 9.1 第一步验证

1. 业务服务代码里不再直接 import `services/llm`
2. 所有模型调用都改经 `LLM RPC`
3. 所有请求都必须带 `BillingContext`
4. `CreditBalanceGuard` 命中缓存、miss 回源、余额不足封禁都通过测试
5. `llm_outbox_messages` 能正常写入 `credit.charge.requested`

### 9.2 第二步验证

1. `TokenStore` 能幂等消费 `credit.charge.requested`
2. `credit_accounts` 余额更新正确
3. `credit_ledger` 流水写入正确
4. `ListCreditTransactions` 返回当前用户自己的流水

### 9.3 第三步验证

1. 商店页能展示 Credit 概览
2. 商店页能展示商品
3. 商店页能展示“我的 Credit 流水”
4. 全文搜索确认以下旧链路不再出现在主路径：
   - `PublishChatTokenUsageAdjustRequested`
   - `AdjustTokenUsage(`
   - `CheckTokenQuota(`
   - `TokenQuotaGuard`
   - `tokens_total +`
   - `tokenstoreapi`
   - `/token-store`
   - `token_products`
   - `token_orders`
   - `token_grants`
   - `token_reward_rules`

---

## 10. 风险与取舍

### 10.1 这次改造面确实更大

因为这次不是只改计费，而是顺带把 `LLM` 从“共享组件”升级成真正独立服务，所以会涉及：

1. 新 RPC
2. 新 client
3. 新 outbox
4. 业务服务调用方式切换

### 10.2 但长期结构更对

收益是：

1. LLM 调用入口统一
2. usage 采集统一
3. 准入 guard 统一
4. 计费事件发布统一
5. TokenStore 只做账本，不再和模型调用散乱耦合

---

## 11. 服务影响总表

### 必改服务

1. `backend/services/llm`
2. `backend/services/tokenstore`
3. `backend/gateway`
4. `backend/services/agent`
5. `backend/services/course`
6. `backend/services/memory`
7. `backend/services/active_scheduler`

### 新增

1. `backend/cmd/llm`
2. `backend/client/llm`
3. `backend/services/llm/rpc/*`
4. `llm_outbox_messages`

### 后续前端接入

1. `frontend/src/views/StoreView.vue`
2. 若商店页拆组件，则补充独立的 Credit 流水列表组件
