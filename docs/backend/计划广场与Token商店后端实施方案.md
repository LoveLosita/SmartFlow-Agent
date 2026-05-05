# 计划广场与 Token 商店后端实施方案

## 1. 文档定位

本文记录当前需要讨论和快速推进的核心产品口径、前后端接口契约、服务边界、事件口径和实施计划，不展开完整交互稿、运营后台和真实支付细节。

本轮目标是新增两个终态服务模块：

1. `taskclass-forum`：支持用户分享 TaskClass 学习计划，并让其他用户一键导入。
2. `token-store`：支持 Token 商品购买、活动奖励和发放账本。

两个模块后续都放在 `backend/services` 下，以独立服务为目标设计；当前仓库工作区未干净前只讨论方案，不进入代码实现。

## 2. 背景与目标

当前产品已经具备用户自建 TaskClass、智能排程和 Token 额度门禁能力。下一阶段希望补上社区化与商业化闭环：

1. 用户可以把自己的复习计划分享出去，形成可浏览、可点赞、可评论、可复用的计划广场。
2. 其他用户可以一键导入计划模板，快速生成自己的 TaskClass。
3. 被点赞、被导入等社区行为可以转化为 Token 激励。
4. 用户可以通过 Token 商店购买或领取 Token，为后续高频 Agent 使用建立基础商业闭环。

## 3. 模块一：学习计划论坛

### 3.1 产品定位

计划广场不是普通帖子论坛，而是“帖子 + TaskClass 模板快照”的社区。

对外展示名先使用“计划广场”。

用户发布时，系统从用户自己的 TaskClass 复制一份模板快照。其他用户导入时，再从快照生成自己的 TaskClass 副本。

快照原则：

1. 发布后不直接引用原作者的 `task_classes` / `task_items`。
2. 原作者后续修改自己的计划，不影响已发布模板。
3. 导入用户拿到的是自己的 TaskClass 副本，后续可自由编辑。
4. 不分享 `embedded_time`、schedule 绑定、用户私有排程状态。

### 3.2 P0 功能

1. 发布学习计划：用户选择一个 TaskClass，填写标题、简介、标签后发布。
2. 浏览列表：支持分页查看公开计划，按最新、点赞数、导入数排序。
3. 查看详情：展示计划说明、TaskClass 配置摘要和任务条目预览。
4. 点赞：同一用户对同一帖子只能点赞一次，可取消点赞。
5. 评论：支持发表评论、多层回复和删除自己的评论，接口返回评论树 JSON。
6. 一键导入：从论坛模板复制出当前用户自己的 TaskClass。
7. 基础激励：模板获得点赞或导入后，可触发 Token 奖励事件。

### 3.3 P0 不做

1. 不做复杂推荐算法。
2. 不做关注、私信、用户主页。
3. 不做富文本编辑器，先用纯文本简介。
4. 不做审核后台和管理员删评，先预留状态字段。
5. 不直接把模板应用进 schedule；导入后由用户走现有 TaskClass / 排程链路。
6. P0 暂不做评论举报和管理员审核流。

### 3.4 核心实体

1. `forum_posts`：帖子主体，记录作者、标题、简介、状态、点赞数、评论数、导入数。
2. `forum_post_templates`：TaskClass 快照，记录模式、日期范围、策略、约束配置等。
3. `forum_post_template_items`：TaskClassItem 快照，只记录 order/content 等模板信息。
4. `forum_likes`：点赞幂等记录。
5. `forum_comments`：评论记录，使用 `parent_comment_id` 表达多层回复关系。
6. `forum_imports`：导入记录，记录从哪个帖子导入到哪个用户和新 TaskClass ID。

### 3.5 关键流程

发布流程：

1. 用户选择自己的 TaskClass。
2. `taskclass-forum` 通过 TaskClass 读取端口拿到完整模板。
3. 服务过滤私有字段，生成论坛快照。
4. 写入帖子和模板快照。

评论流程：

1. 用户可以直接评论帖子，也可以回复任意一条评论。
2. 评论表用 `parent_comment_id` 记录父评论，根评论的 `parent_comment_id` 为空。
3. 列表查询先按帖子读取扁平评论，再由服务层组装成多层评论树。
4. 删除评论时 P0 只允许用户删除自己的评论，并采用软删除，保留子回复结构，避免整棵回复树断链。
5. P0 暂不引入管理员删评、举报和审核流，后续如需治理再单独扩展。

导入流程：

1. 用户点击一键导入。
2. `taskclass-forum` 读取帖子模板快照。
3. 通过 TaskClass 写入端口为当前用户创建 TaskClass 副本。
4. 写入导入记录并增加导入计数。
5. 可异步发布 Token 奖励事件。

## 4. 模块二：Token 商店

### 4.1 产品定位

Token 商店负责 Token 的购买、奖励、发放和账本，不负责登录鉴权，也不直接承载 Agent 消耗统计。

`user/auth` 继续负责用户 Token quota 的权威判断；`token-store` 只负责产生“获取 Token / 发放 Token”的业务事实和账本。本轮不新增或修改 `user/auth` 契约，先在 `token-store` 内封装 Token 获取途径和后续发放出口，等主线合并稳定后再切到 `user/auth` 的权威额度发放能力。

### 4.2 P0 功能

1. 商品列表：展示可购买 Token 包。
2. 创建订单：用户选择商品生成订单。
3. 支付确认：P0 先支持 mock paid 或管理端确认 paid，不接真实支付网关。
4. Token 发放记录：订单支付成功后写入发放账本，并通过本服务内部端口封装后续发放出口。
5. 奖励发放：支持论坛点赞、导入等事件触发奖励。
6. 发放账本：所有发放必须有幂等 event_id，避免后续重复加额度。

### 4.3 P0 不做

1. 不接真实微信 / 支付宝 / Stripe。
2. 不做退款、发票、优惠券。
3. 不做复杂会员体系。
4. 不直接改 `users.token_usage`，避免和消费统计混淆。

### 4.4 核心实体

1. `token_products`：Token 商品，P0 从表读取，并通过 seed 初始化 2-3 个商品，不做管理后台。
2. `token_orders`：订单。
3. `token_grants`：Token 发放账本，记录购买、奖励、补偿等来源。
4. `token_reward_rules`：奖励规则，P0 可先用配置或简单表。

### 4.5 关键流程

购买流程：

1. 用户选择商品并创建订单。
2. 订单进入 `pending`。
3. P0 通过 mock paid 或管理端确认，把订单置为 `paid`。
4. `token-store` 写入 token grant 账本。
5. `token-store` 通过内部发放端口记录本次 Token 获取事实，本轮不修改 `user/auth`。
6. 发放记录写入成功后订单进入 `granted`；后续切到 `user/auth` 时只替换发放端口实现。

奖励流程：

1. 论坛产生点赞或导入事件。
2. `token-store` 按奖励规则判断是否发放。
3. 写入 token grant 账本。
4. 通过内部发放端口记录奖励获取事实；后续切到 `user/auth` 时只替换发放端口实现。

## 5. 服务边界

### 5.1 `taskclass-forum`

负责：

1. 论坛帖子、点赞、评论、导入记录。
2. TaskClass 模板快照。
3. 导入时的模板复制编排。
4. 发布社区行为事件，供 Token 激励消费。

不负责：

1. TaskClass 原始表所有权。
2. schedule 写入和排程应用。
3. Token 额度发放。
4. 用户登录鉴权。

### 5.2 `token-store`

负责：

1. 商品、订单、发放账本。
2. 社区奖励规则。
3. 幂等发放。
4. 封装 Token 获取途径和后续发放出口。

不负责：

1. JWT、登录、注册。
2. Agent 消耗统计。
3. TaskClass 论坛内容。
4. 真实第三方支付回调，P0 只预留状态机。

### 5.3 与现有服务关系

1. 论坛读取和导入 TaskClass 时，先通过端口适配旧 `TaskClassService/DAO`。
2. 后续 `task-class` 独立成服务后，只替换端口适配器。
3. 论坛 P0 不直接写 schedule，避免被 `schedule` 未拆服务影响。
4. Token 商店本轮不直接改 users 表，也不新增或修改 `user/auth` 契约；先封装自己的 Token 获取途径和后续发放出口，等合并后再切到 `user/auth`。

### 5.4 并行迁移细则

1. 当前 `task` / `task-class` 还没有完成服务迁移时，本轮不改它们的核心模块，也不提前给它们补新的 RPC 边界，避免和另一条拆分线发生合并冲突。
2. `taskclass-forum` 的实现层可以先通过 legacy adapter 复用现有 DAO / Service，必要时也可以直接读旧表，但这类直连只能收敛在 adapter 内，不能扩散到论坛业务层。
3. 论坛业务层只依赖“读取 TaskClass 快照”和“写入 TaskClass 副本”这类端口，不关心底层是 DAO、Service 还是后续 RPC。
4. 等 `task-class` 侧完成独立服务后，论坛只替换 adapter，不回改论坛领域逻辑，不重写帖子、点赞、评论和导入编排。
5. 这轮优先保证论坛和 Token 商店自己的边界稳定，`task` 相关能力保持当前状态即可，等主线合并后再按统一节奏推进下一步迁移。

### 5.5 理想内部结构

参考 `userauth` 样板和微服务迁移总纲，两个新模块都按“`cmd` 进程入口 + `gateway/api` HTTP 适配 + `gateway/client` RPC client + `services` 服务主体”的结构推进。

命名约定：

1. 产品和事件域继续使用 `taskclass-forum`、`token-store`，方便和方案、topic、事件名保持一致。
2. Go 目录和包名建议使用 `taskclassforum`、`tokenstore`，对齐当前 `userauth` 样板，避免包名里出现连字符。
3. 如果后续统一目录规范改成带连字符目录，也只调整目录名和 import，不改变服务内职责分层。

推荐目标树：

这里是两套服务边缘入口：

1. `gateway/api` 下放 HTTP API 入口，每个服务一个子目录。
2. `gateway/client` 下放 zrpc client / error 反解，每个服务一个子目录。
3. 旧 `gateway/userapi`、`gateway/userauth` 暂不在本轮调整，后续合并时再统一处理。

```text
backend/
├── cmd/
│   ├── taskclassforum/
│   │   └── main.go
│   └── tokenstore/
│       └── main.go
├── gateway/
│   ├── api/
│   │   ├── taskclassforum/
│   │   │   ├── handler.go
│   │   │   └── routes.go
│   │   └── tokenstore/
│   │       ├── handler.go
│   │       └── routes.go
│   └── client/
│       ├── taskclassforum/
│       │   ├── client.go
│       │   └── errors.go
│       └── tokenstore/
│           ├── client.go
│           └── errors.go
├── services/
│   ├── taskclassforum/
│   │   ├── sv/
│   │   │   ├── service.go
│   │   │   ├── post.go
│   │   │   ├── like.go
│   │   │   ├── comment.go
│   │   │   └── import.go
│   │   ├── dao/
│   │   │   ├── connect.go
│   │   │   ├── post.go
│   │   │   ├── template.go
│   │   │   ├── like.go
│   │   │   ├── comment.go
│   │   │   └── import.go
│   │   ├── model/
│   │   │   ├── forum_post.go
│   │   │   ├── forum_post_template.go
│   │   │   ├── forum_post_template_item.go
│   │   │   ├── forum_like.go
│   │   │   ├── forum_comment.go
│   │   │   └── forum_import.go
│   │   ├── internal/
│   │   │   ├── adapter/
│   │   │   ├── snapshot/
│   │   │   ├── commenttree/
│   │   │   └── event/
│   │   └── rpc/
│   │       ├── pb/
│   │       ├── taskclassforum.proto
│   │       ├── errors.go
│   │       ├── handler.go
│   │       └── server.go
│   └── tokenstore/
│       ├── sv/
│       │   ├── service.go
│       │   ├── product.go
│       │   ├── order.go
│       │   ├── grant.go
│       │   └── reward.go
│       ├── dao/
│       │   ├── connect.go
│       │   ├── product.go
│       │   ├── order.go
│       │   ├── grant.go
│       │   └── reward_rule.go
│       ├── model/
│       │   ├── token_product.go
│       │   ├── token_order.go
│       │   ├── token_grant.go
│       │   └── token_reward_rule.go
│       ├── internal/
│       │   ├── adapter/
│       │   ├── paymentmock/
│       │   ├── grant/
│       │   ├── reward/
│       │   └── event/
│       └── rpc/
│           ├── pb/
│           ├── tokenstore.proto
│           ├── errors.go
│           ├── handler.go
│           └── server.go
└── shared/
    ├── contracts/
    │   ├── taskclassforum/
    │   └── tokenstore/
    ├── events/
    └── ports/
```

目录职责：

1. `cmd/<service>/main.go` 只负责读取配置、初始化资源、启动 go-zero zrpc 服务，不承载业务规则。
2. `gateway/api/<service>` 只负责 HTTP 参数绑定、鉴权后用户 ID 注入、调用对应 client、复用 `respond` 返回前端，不直接访问服务数据库。
3. `gateway/client/<service>` 只放 zrpc client 和错误反解；不要把 client 放进 `cmd`，也不要让 gateway 直接 import 服务端 DAO / model。
4. `services/<service>/sv` 是服务主业务编排层，负责事务顺序、幂等判断、状态流转和跨端口调用。
5. `services/<service>/dao` 只负责本服务数据库访问；`model` 只放本服务私有表模型，不放跨服务 DTO。
6. `services/<service>/internal` 只放服务私有子能力，外部服务禁止直接 import。
7. `services/<service>/rpc` 对齐 `userauth`，放 proto、server、handler、errors 和生成后的 `pb`。
8. `shared/contracts`、`shared/events`、`shared/ports` 只放跨服务契约、事件 payload 和端口接口，不放 DAO、model、sv 或业务状态机。

`taskclassforum/internal` 建议职责：

1. `adapter/`：承载 TaskClass 端口实现。P0 先放 legacy adapter，复用旧 DAO / Service 或临时直读旧表；后续替换为 task-class RPC adapter。
2. `snapshot/`：负责 TaskClass 原始数据到论坛模板快照的过滤、复制和字段白名单，避免分享 `embedded_time`、schedule 绑定和私有排程状态。
3. `commenttree/`：负责把 `forum_comments` 扁平记录组装成多层评论树，并处理软删除节点的展示兜底。
4. `event/`：负责组装 `forum.post.liked`、`forum.post.imported` 等领域事件 payload；事件投递仍走全局 outbox 路由和服务 catalog。

`tokenstore/internal` 建议职责：

1. `adapter/`：承载后续 `user/auth` 额度发放适配点。P0 先封装 `token-store` 自己的 Token 获取途径和发放出口，不新增或修改 `user/auth` 契约，也禁止直接改 `users` 表。
2. `paymentmock/`：只处理 P0 mock paid 状态确认，不承载真实支付网关逻辑。
3. `grant/`：封装 `token_grants` 幂等账本写入、event_id 去重和 grant 状态流转。
4. `reward/`：封装论坛点赞、导入等奖励规则判断，P0 可先走配置或简单表。
5. `event/`：负责组装 `token.grant.requested`、`token.grant.completed` 等事件 payload；后续真实异步化时复用服务级 outbox 基线。

## 6. 事件与激励

P0 建议事件：

1. `forum.post.liked`：帖子被点赞。
2. `forum.post.imported`：帖子被导入。
3. `token.grant.requested`：请求发放 Token。
4. `token.grant.completed`：Token 发放完成。

奖励口径先从简单规则开始：

1. 点赞奖励先只给帖子作者，每个帖子每个用户首次点赞只奖励一次。
2. 同一用户对同一计划只允许导入一次，导入奖励随该次导入记录一次。
3. 同一 event_id 的 Token 发放必须幂等。
4. 奖励额度先走配置，不在本方案阶段定死。

Outbox 使用口径：

1. P0 用户可见主链路不强依赖 outbox：发布、点赞、评论、导入、下单、mock paid、写 grant 账本都应在服务自己的同步事务里完成。
2. Outbox 用于异步副作用：论坛点赞 / 导入奖励事件、后续搜索索引同步、通知、排行榜刷新、运营统计，以及后续 `token-store` 到 `user/auth` 的权威额度同步。
3. 第一版如果为了赶进度先同步写奖励账本，也必须保留事件名、payload 和 `event_id` 规则，后续切到 outbox 消费时不改业务语义。
4. 新服务后续接入服务级 outbox 时，建议使用独立表、topic 和 consumer group，不回退到共享 outbox。

## 7. 后端契约初版

本节把前端对接草案收束成后端 P0 契约。若后续实现时发现字段和现有模型存在细小出入，允许在保持语义不变的前提下微调字段名，但必须同步更新 `docs/frontend/计划广场与Token商店对接说明.md`。

### 7.1 通用 HTTP 约定

1. 所有接口统一挂在 `/api/v1` 下。
2. P0 默认都需要登录态，`user_id` 由 gateway 从 JWT 注入，前端不传 `user_id`。
3. 响应复用项目统一壳：`status`、`info`、`data`。
4. 写接口优先支持 `X-Idempotency-Key`，避免前端重复点击造成重复发布、重复评论、重复导入或重复下单。
5. 时间字段统一返回 ISO 8601 字符串，带时区。
6. 分页统一使用 `page`、`page_size`、`total`、`has_more`。

### 7.2 计划广场 HTTP 契约

| 功能 | 方法 | 路径 | 幂等 |
| --- | --- | --- | --- |
| 计划列表 | `GET` | `/api/v1/plan-square/posts` | 否 |
| 热门标签 | `GET` | `/api/v1/plan-square/tags` | 否 |
| 发布计划 | `POST` | `/api/v1/plan-square/posts` | 是 |
| 计划详情 | `GET` | `/api/v1/plan-square/posts/{post_id}` | 否 |
| 点赞 | `POST` | `/api/v1/plan-square/posts/{post_id}/like` | 由唯一约束兜底 |
| 取消点赞 | `DELETE` | `/api/v1/plan-square/posts/{post_id}/like` | 否 |
| 评论树 | `GET` | `/api/v1/plan-square/posts/{post_id}/comments` | 否 |
| 发表评论 / 回复 | `POST` | `/api/v1/plan-square/posts/{post_id}/comments` | 是 |
| 删除自己的评论 | `DELETE` | `/api/v1/plan-square/comments/{comment_id}` | 否 |
| 一键导入 | `POST` | `/api/v1/plan-square/posts/{post_id}/import` | 是 |

核心请求 DTO：

```go
type CreateForumPostRequest struct {
	TaskClassID uint64   `json:"task_class_id"`
	Title       string   `json:"title"`
	Summary     string   `json:"summary"`
	Tags        []string `json:"tags"`
}

type CreateForumCommentRequest struct {
	Content         string  `json:"content"`
	ParentCommentID *uint64 `json:"parent_comment_id"`
}

type ImportForumPostRequest struct {
	TargetTitle string `json:"target_title"`
}
```

核心响应 DTO：

```go
type ForumPostBrief struct {
	PostID          uint64              `json:"post_id"`
	Title           string              `json:"title"`
	Summary         string              `json:"summary"`
	Tags            []string            `json:"tags"`
	Author          UserBrief           `json:"author"`
	TemplateSummary TemplateSummary     `json:"template_summary"`
	Counters        ForumPostCounters   `json:"counters"`
	ViewerState     ForumPostViewerState `json:"viewer_state"`
	Status          string              `json:"status"`
	CreatedAt       string              `json:"created_at"`
}

type ForumPostDetail struct {
	Post     ForumPostBrief  `json:"post"`
	Template TemplateDetail  `json:"template"`
}

type ForumCommentNode struct {
	CommentID       uint64             `json:"comment_id"`
	PostID          uint64             `json:"post_id"`
	ParentCommentID *uint64            `json:"parent_comment_id"`
	Content         string             `json:"content"`
	Status          string             `json:"status"`
	Author          UserBrief          `json:"author"`
	CanDelete       bool               `json:"can_delete"`
	CreatedAt       string             `json:"created_at"`
	DeletedAt       *string            `json:"deleted_at"`
	Children        []ForumCommentNode `json:"children"`
}
```

字段口径：

1. `ForumPostBrief.status` P0 先使用 `published`，后续审核可扩展 `hidden`、`deleted`、`pending_review`。
2. `ForumCommentNode.status` P0 使用 `visible`、`deleted`。
3. `CanDelete` 由后端根据当前用户判断，前端只按字段展示删除入口。
4. `TemplateDetail.items_preview` 来自论坛快照，不读取原作者当前 TaskClass。
5. 一键导入成功只返回新 TaskClass ID，不直接写 schedule。

### 7.3 Token 商店 HTTP 契约

| 功能 | 方法 | 路径 | 幂等 |
| --- | --- | --- | --- |
| Token 概览 | `GET` | `/api/v1/token-store/summary` | 否 |
| 商品列表 | `GET` | `/api/v1/token-store/products` | 否 |
| 创建订单 | `POST` | `/api/v1/token-store/orders` | 是 |
| 订单列表 | `GET` | `/api/v1/token-store/orders` | 否 |
| 订单详情 | `GET` | `/api/v1/token-store/orders/{order_id}` | 否 |
| mock paid | `POST` | `/api/v1/token-store/orders/{order_id}/mock-paid` | 是 |
| Token 获取记录 | `GET` | `/api/v1/token-store/grants` | 否 |

核心请求 DTO：

```go
type CreateTokenOrderRequest struct {
	ProductID uint64 `json:"product_id"`
	Quantity  int    `json:"quantity"`
}

type MockPaidOrderRequest struct {
	MockChannel string `json:"mock_channel"`
}
```

核心响应 DTO：

```go
type TokenSummary struct {
	RecordedTokenTotal    int64  `json:"recorded_token_total"`
	AppliedTokenTotal     int64  `json:"applied_token_total"`
	PendingApplyTokenTotal int64  `json:"pending_apply_token_total"`
	QuotaSyncStatus       string `json:"quota_sync_status"`
	Tip                   string `json:"tip"`
}

type TokenProductView struct {
	ProductID   uint64 `json:"product_id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	TokenAmount int64  `json:"token_amount"`
	PriceCent   int64  `json:"price_cent"`
	PriceText   string `json:"price_text"`
	Currency    string `json:"currency"`
	Badge       string `json:"badge"`
	Status      string `json:"status"`
	SortOrder   int    `json:"sort_order"`
}

type TokenOrderView struct {
	OrderID      uint64          `json:"order_id"`
	OrderNo      string          `json:"order_no"`
	Status       string          `json:"status"`
	TokenAmount  int64           `json:"token_amount"`
	AmountCent   int64           `json:"amount_cent"`
	PriceText    string          `json:"price_text"`
	Currency     string          `json:"currency"`
	PaymentMode  string          `json:"payment_mode"`
	Grant        *TokenGrantView `json:"grant"`
	CreatedAt    string          `json:"created_at"`
	PaidAt       *string         `json:"paid_at"`
	GrantedAt    *string         `json:"granted_at"`
}

type TokenGrantView struct {
	GrantID      uint64 `json:"grant_id"`
	EventID      string `json:"event_id"`
	Source       string `json:"source"`
	SourceLabel  string `json:"source_label"`
	Amount       int64  `json:"amount"`
	Status       string `json:"status"`
	QuotaApplied bool   `json:"quota_applied"`
	Description  string `json:"description"`
	CreatedAt    string `json:"created_at"`
}
```

字段口径：

1. `TokenSummary.quota_sync_status` P0 返回 `not_connected`，后续可扩展 `partial`、`synced`。
2. `TokenOrderView.status` 使用 `pending`、`paid`、`granted`、`closed`。
3. `TokenGrantView.status` 使用 `recorded`、`applied`、`skipped`、`failed`。
4. `quota_applied` P0 默认为 `false`，因为本轮不改 `user/auth`。
5. 前端 P0 展示“累计获取 Token”，不要展示为权威可用额度。

### 7.4 RPC 服务契约

RPC 只承载 gateway 到服务主体的跨进程调用，不暴露给前端。字段可以按 proto 生成规则转成 snake_case JSON，但语义必须和 HTTP DTO 保持一致。

`taskclassforum` P0 方法：

```protobuf
service TaskClassForumService {
  rpc ListPosts(ListForumPostsRequest) returns (ListForumPostsResponse);
  rpc ListTags(ListForumTagsRequest) returns (ListForumTagsResponse);
  rpc CreatePost(CreateForumPostRPCRequest) returns (CreateForumPostRPCResponse);
  rpc GetPost(GetForumPostRequest) returns (GetForumPostResponse);
  rpc LikePost(LikeForumPostRequest) returns (LikeForumPostResponse);
  rpc UnlikePost(UnlikeForumPostRequest) returns (UnlikeForumPostResponse);
  rpc ListComments(ListForumCommentsRequest) returns (ListForumCommentsResponse);
  rpc CreateComment(CreateForumCommentRPCRequest) returns (CreateForumCommentRPCResponse);
  rpc DeleteComment(DeleteForumCommentRequest) returns (DeleteForumCommentResponse);
  rpc ImportPost(ImportForumPostRPCRequest) returns (ImportForumPostRPCResponse);
}
```

`tokenstore` P0 方法：

```protobuf
service TokenStoreService {
  rpc GetSummary(GetTokenSummaryRequest) returns (GetTokenSummaryResponse);
  rpc ListProducts(ListTokenProductsRequest) returns (ListTokenProductsResponse);
  rpc CreateOrder(CreateTokenOrderRPCRequest) returns (CreateTokenOrderRPCResponse);
  rpc ListOrders(ListTokenOrdersRequest) returns (ListTokenOrdersResponse);
  rpc GetOrder(GetTokenOrderRequest) returns (GetTokenOrderResponse);
  rpc MockPaidOrder(MockPaidOrderRequest) returns (MockPaidOrderResponse);
  rpc ListGrants(ListTokenGrantsRequest) returns (ListTokenGrantsResponse);
}
```

RPC 入参通用规则：

1. 所有需要用户态的方法都带 `actor_user_id`，由 gateway 注入。
2. 写方法带 `idempotency_key`，由 gateway 从 `X-Idempotency-Key` 读取。
3. 列表方法带 `page`、`page_size`、筛选字段和排序字段。
4. RPC 层错误使用 go-zero / gRPC `error` 返回，gateway client 负责转成项目统一响应。

### 7.5 服务内部端口契约

`taskclassforum` 依赖 TaskClass 端口：

```go
type TaskClassSnapshotPort interface {
	GetOwnedTaskClassSnapshot(ctx context.Context, userID uint64, taskClassID uint64) (*TaskClassSnapshot, error)
	CreateTaskClassFromSnapshot(ctx context.Context, userID uint64, snapshot TaskClassSnapshot, targetTitle string) (*CreatedTaskClass, error)
}
```

端口口径：

1. `GetOwnedTaskClassSnapshot` 只能读取当前用户自己的 TaskClass。
2. `TaskClassSnapshot` 必须剔除 `embedded_time`、schedule 绑定和用户私有排程状态。
3. P0 实现为 legacy adapter，可复用旧 DAO / Service 或收敛式直读旧表。
4. 后续 `task-class` 服务拆出后，只替换 adapter。

`tokenstore` 内部发放端口：

```go
type TokenGrantOutlet interface {
	RecordAcquisition(ctx context.Context, grant TokenGrantRecord) error
}
```

端口口径：

1. P0 只记录 `token-store` 自己的获取事实和账本，不新增或修改 `user/auth` 契约。
2. 禁止直接修改 `users` 表。
3. 后续切到 `user/auth` 时新增 adapter 实现，服务编排层不重写。

### 7.6 事件契约

P0 事件按当前服务级 outbox 思路设计，但不要求主链路第一版强依赖异步投递；如果第一版先同步写账本，也必须按下面 event_id 生成规则保留幂等口径。

`forum.post.liked`：

```json
{
  "event_id": "forum.post.liked:{post_id}:{actor_user_id}",
  "post_id": 10001,
  "author_user_id": 88,
  "actor_user_id": 91,
  "reward_receiver_user_id": 88,
  "reward_amount": 1,
  "occurred_at": "2026-05-04T21:05:00+08:00"
}
```

`forum.post.imported`：

```json
{
  "event_id": "forum.post.imported:{post_id}:{actor_user_id}",
  "post_id": 10001,
  "import_id": 70001,
  "author_user_id": 88,
  "actor_user_id": 91,
  "new_task_class_id": 430,
  "reward_receiver_user_id": 88,
  "reward_amount": 2,
  "occurred_at": "2026-05-04T21:10:00+08:00"
}
```

`token.grant.requested`：

```json
{
  "event_id": "order:80001:paid",
  "user_id": 91,
  "source": "purchase",
  "amount": 100,
  "description": "购买基础 Token 包",
  "occurred_at": "2026-05-04T21:00:00+08:00"
}
```

`token.grant.completed`：

```json
{
  "event_id": "order:80001:paid",
  "grant_id": 90001,
  "user_id": 91,
  "source": "purchase",
  "amount": 100,
  "status": "recorded",
  "quota_applied": false,
  "occurred_at": "2026-05-04T21:00:01+08:00"
}
```

事件口径：

1. 点赞奖励只给作者，取消点赞不回滚已记录奖励。
2. 同一用户对同一计划只允许导入一次，导入奖励随该次导入记录一次。
3. Token 发放 P0 的 `quota_applied` 为 `false`，后续接入 `user/auth` 后再变成真实同步状态。
4. 事件 payload 放 `backend/shared/events`；服务私有处理逻辑留在各自 `internal/event` 或 `sv`。
5. 事件不要阻塞用户主流程；异步消费失败时依赖 `event_id` 和 grant 账本幂等重试。

### 7.7 幂等与唯一约束

论坛侧：

1. `forum_likes` 对 `(post_id, user_id)` 建唯一约束。
2. `forum_imports` 对 `(post_id, user_id)` 建唯一约束，同一用户同一计划只允许导入一次，并保留 `event_id`；奖励去重依赖 `token_grants.event_id`。
3. 评论创建支持 `X-Idempotency-Key`，避免重复提交。
4. 删除评论是软删除，重复删除同一条自己的评论返回成功态。

Token 侧：

1. `token_orders.order_no` 唯一。
2. `token_grants.event_id` 唯一，是所有发放事实的最终幂等边界。
3. mock paid 重复调用同一订单时，不重复创建 grant。
4. 创建订单使用 `X-Idempotency-Key` 时，同一用户同一 key 返回同一订单。

### 7.8 错误码初版

| status | 场景 | 前端处理 |
| --- | --- | --- |
| `40001` | 参数错误 | 展示 `info` |
| `40003` | 未登录或登录态失效 | 跳登录 |
| `40004` | 资源不存在或已下架 | 展示空态或返回列表 |
| `40009` | 重复操作，如已点赞 | 以返回状态刷新 UI |
| `40013` | 无权限，如删除他人评论 | 展示 `info` |
| `40029` | 请求过于频繁 | 展示稍后重试 |
| `50000` | 服务内部错误 | 展示通用失败提示 |

错误处理口径：

1. 业务错误不要把 RPC 内部错误原文透给前端。
2. gateway client 负责把服务错误转成统一响应。
3. 幂等重复不应默认作为失败；能返回已有结果时优先返回已有结果。

## 8. 当前推进策略

1. 当前工作区存在其它拆服务改动，本阶段只提交方案文档。
2. 等工作区干净后，从集成分支新开功能分支或单独 git worktree。
3. 实现时两个服务主体可以并行推进。
4. `gateway/router`、`shared/contracts`、`shared/ports`、`outbox route`、`config` 由主代理统一收口。
5. 先做 P0 闭环，再扩展审核、真实支付和推荐排序。
6. `task` / `task-class` 未迁出前，论坛侧优先走 legacy adapter，不提前把本轮推进绑死在 task 迁移完成之后。

## 9. 已确认口径

1. 论坛展示名使用“计划广场”。
2. 点赞奖励只给帖子作者。
3. 同一用户对同一计划只允许导入一次，前端根据 `imported_once` 刷新按钮状态，奖励随该次导入记录一次。
4. 评论 P0 支持用户删除自己的评论，暂不引入管理员删评、举报和审核流。
5. 评论层级后端不硬限制，数据库只存 `parent_comment_id`，服务层负责组装评论树；前端可以按体验做折叠或缩进。
6. Token 发放本轮不改 `user/auth`，先在 `token-store` 内封装 Token 获取途径和后续发放出口，后续合并稳定后再切到 `user/auth` 的权威额度发放能力。
7. Token 商品从 `token_products` 表读取，P0 通过 seed 初始化商品，不做管理后台。
8. 前端正常展示评论，不隐藏评论能力。

## 10. 实施计划

### 10.1 P0 实施原则

1. 本轮先把 `taskclass-forum` 和 `token-store` 两个新服务跑通，不改 `task` / `task-class` 核心模块，不改 `user/auth` 契约。
2. 论坛依赖 TaskClass 的能力必须收敛在 legacy adapter 内，后续 `task-class` RPC 合并后只替换 adapter。
3. Token 获取和发放先记录在 `token-store` 自己的账本里，不直接改 `users` 表。
4. 搜索 P0 先走 MySQL，服务层预留 `PostSearchPort`；后续需要中文分词、相关性排序或内容检索时，再替换为 Elasticsearch / OpenSearch adapter。
5. 商品 P0 从 `token_products` 表读取，通过 seed 初始化 2-3 个商品；不做商品管理后台。
6. 同一用户对同一计划只允许导入一次，后端通过唯一约束兜底，前端通过 `imported_once` 刷新按钮状态。
7. 评论层级不硬限制，表里只存 `parent_comment_id`，服务层按帖子组装评论树。
8. Outbox 总线只承载异步副作用，不承载 P0 用户可见主链路，避免联调期被 relay / consumer 状态拖慢。

### 10.2 表结构与迁移

第一步先落服务私有表和必要唯一约束：

1. 计划广场表：`forum_posts`、`forum_post_templates`、`forum_post_template_items`、`forum_likes`、`forum_comments`、`forum_imports`。
2. Token 商店表：`token_products`、`token_orders`、`token_grants`、`token_reward_rules`。
3. `forum_likes` 建 `(post_id, user_id)` 唯一约束。
4. `forum_imports` 建 `(post_id, user_id)` 唯一约束，保证同一用户同一计划只导入一次。
5. `forum_comments` 建 `post_id`、`parent_comment_id`、`created_at` 索引，支撑按帖子读取扁平评论后组树。
6. `token_orders.order_no` 唯一，`token_grants.event_id` 唯一。
7. `token_products` 通过 seed 初始化商品，后续如要管理后台再单独扩展。

### 10.3 服务骨架

第二步搭建目录和进程骨架：

1. 新增 `backend/cmd/taskclassforum`、`backend/cmd/tokenstore`。
2. 新增 `backend/services/taskclassforum`、`backend/services/tokenstore`，内部按 `sv`、`dao`、`model`、`internal`、`rpc` 分层。
3. 新增 `backend/gateway/api/taskclassforum`、`backend/gateway/api/tokenstore` 承载 HTTP 入口。
4. 新增 `backend/gateway/client/taskclassforum`、`backend/gateway/client/tokenstore` 承载 zrpc client 和错误反解。
5. `gateway/router`、配置、outbox route、shared contracts 由主代理统一收口，避免多处同时改同一个装配点。

### 10.4 计划广场 P0

第三步实现计划广场闭环：

1. 发布计划：读取用户自己的 TaskClass，生成论坛模板快照，写入帖子和模板表。
2. 列表和详情：先用 MySQL 查询 `title`、`summary`、`tags`、状态和计数字段，支持 `latest`、`likes`、`imports` 排序。
3. 点赞 / 取消点赞：用唯一约束保证同一用户只点赞一次，点赞奖励事件只给作者。
4. 评论：发表评论、回复任意评论、删除自己的评论；删除采用软删除，保留子回复结构。
5. 评论树：按帖子读取扁平评论，服务层按 `parent_comment_id` 组装树；后端不限制层级。
6. 一键导入：同一用户同一帖子只允许导入一次，只生成当前用户自己的 TaskClass，不写 schedule。

### 10.5 Token 商店 P0

第四步实现 Token 商店闭环：

1. 商品列表从 `token_products` 表读取，P0 商品由 seed 提供。
2. 创建订单：写入 `token_orders`，状态为 `pending`。
3. mock paid：把订单置为已支付，并写入 `token_grants`。
4. 获取记录：从 `token_grants` 分页查询购买、点赞奖励、导入奖励等记录。
5. Token 概览：展示 `token-store` 已记录的累计获取 Token；P0 不展示为 `user/auth` 权威可用额度。
6. 预留 `TokenGrantOutlet`，后续切到 `user/auth` 时只替换发放出口。

### 10.6 Outbox 总线接入

第五步按复杂度分两档推进 outbox：

1. P0 主链路同步闭环：计划发布、点赞状态、评论、导入、订单、mock paid 和 grant 账本都同步写入本服务数据库。
2. P0 可同步写奖励账本，但必须按 `forum.post.liked`、`forum.post.imported` 生成稳定 `event_id`，保证后续切异步时可复用同一幂等边界。
3. 若本轮顺手接入服务级 outbox，建议新增 `taskclass_forum_outbox_messages`、`token_store_outbox_messages`，对应 topic 为 `smartflow.taskclass-forum.outbox`、`smartflow.token-store.outbox`。
4. 对应 consumer group 建议为 `smartflow-taskclass-forum-outbox-consumer`、`smartflow-token-store-outbox-consumer`，延续当前服务级 topic / group 隔离规则。
5. `taskclass-forum` 适合发布 `forum.post.liked`、`forum.post.imported`、后续 `forum.post.published`、`forum.post.updated`、`forum.post.deleted`。
6. `token-store` 适合发布 `token.grant.requested`、`token.grant.completed`，后续接 `user/auth` 时再消费或转发到权威额度同步。
7. 搜索索引、排行榜、通知、运营统计都走 outbox 异步消费，不进入 HTTP 主事务。
8. relay / consumer 失败时不得影响用户已经成功的主操作，通过 outbox 重试和 `token_grants.event_id` 幂等兜底。

### 10.7 缓存策略

P0 不引入复杂缓存，但评论区读多写少，评论树需要接短 TTL 缓存：

1. 评论树采用 cache-aside + 版本号失效，缓存粒度为 `post_id + sort + page + page_size + version`。版本 key 为 `forum:comments:{post_id}:version`，数据 key 为 `forum:comments:{post_id}:v{version}:sort:{sort}:page:{page}:size:{page_size}`。
2. 缓存内容是“当前页根评论 + 子孙评论”组装后的去个性化评论树 JSON，并连同分页结果一起缓存；`can_delete` 这类当前用户视角字段不进共享缓存，返回前由 service 按 `actor_user_id` 补齐。
3. 新增评论、回复或删除评论的 DB 事务成功后，递增 `forum:comments:{post_id}:version`。旧 data key 不扫描删除，依赖短 TTL 自然回收，避免写评论时阻塞 Redis。
4. Redis 读取、写入或版本递增失败都不影响主链路：读失败直接回源 DB，写失败保持 DB 结果返回，版本递增失败则等待短 TTL 兜底。
5. 评论接口仍按根评论分页，后端只读取当前页根评论及其子孙评论后组树，避免一次拉完整帖子全部评论。
6. 帖子列表和详情 P0 可先不缓存；如果出现热点，再对列表首屏或详情头部做短 TTL 缓存，并在点赞、评论、导入后按帖子维度失效。
7. 点赞数、评论数、导入数优先存 `forum_posts` 计数字段，写操作事务内增减，避免每次列表都聚合统计。
8. `token_products` 读取频率高、变化少，可做短 TTL 缓存；但 P0 直接读表也可以接受。
9. 后续若上 Elasticsearch，只缓存搜索索引，不改变前端接口和论坛业务编排。

### 10.8 联调与验收

最后按接口闭环做 smoke：

1. 发布计划后，列表和详情能看到模板快照。
2. 点赞成功后，点赞状态和计数刷新，作者获得一条 Token 获取记录。
3. 评论支持多层回复；删除自己的评论后，子回复仍保留。
4. 同一用户同一计划第二次导入被后端拒绝或返回已导入状态，前端按钮状态保持一致。
5. mock paid 后订单进入 `granted`，`token_grants` 只写一条幂等记录。
6. Token 概览展示累计获取 Token，但不声称已经同步到 `user/auth` 权威可用额度。
7. 如果本轮接入 outbox，需要额外验证事件写入、投递、消费失败重试和幂等不重复发放。
