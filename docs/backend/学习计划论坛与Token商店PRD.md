# 学习计划论坛与 Token 商店 PRD

## 1. 文档定位

本文只记录当前需要讨论和快速推进的核心产品口径，不展开完整交互稿、运营后台和支付细节。

本轮目标是新增两个终态服务模块：

1. `taskclass-forum`：支持用户分享 TaskClass 学习计划，并让其他用户一键导入。
2. `token-store`：支持 Token 商品购买、活动奖励和发放账本。

两个模块后续都放在 `backend/services` 下，以独立服务为目标设计；当前仓库工作区未干净前只讨论 PRD，不进入代码实现。

## 2. 背景与目标

当前产品已经具备用户自建 TaskClass、智能排程和 Token 额度门禁能力。下一阶段希望补上社区化与商业化闭环：

1. 用户可以把自己的复习计划分享出去，形成可浏览、可点赞、可评论、可复用的学习计划论坛。
2. 其他用户可以一键导入计划模板，快速生成自己的 TaskClass。
3. 被点赞、被导入等社区行为可以转化为 Token 激励。
4. 用户可以通过 Token 商店购买或领取 Token，为后续高频 Agent 使用建立基础商业闭环。

## 3. 模块一：学习计划论坛

### 3.1 产品定位

学习计划论坛不是普通帖子论坛，而是“帖子 + TaskClass 模板快照”的社区。

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
5. 评论：支持基础评论列表和发表评论，P0 不做楼中楼。
6. 一键导入：从论坛模板复制出当前用户自己的 TaskClass。
7. 基础激励：模板获得点赞或导入后，可触发 Token 奖励事件。

### 3.3 P0 不做

1. 不做复杂推荐算法。
2. 不做关注、私信、用户主页。
3. 不做富文本编辑器，先用纯文本简介。
4. 不做审核后台，先预留状态字段。
5. 不直接把模板应用进 schedule；导入后由用户走现有 TaskClass / 排程链路。

### 3.4 核心实体

1. `forum_posts`：帖子主体，记录作者、标题、简介、状态、点赞数、评论数、导入数。
2. `forum_post_templates`：TaskClass 快照，记录模式、日期范围、策略、约束配置等。
3. `forum_post_template_items`：TaskClassItem 快照，只记录 order/content 等模板信息。
4. `forum_likes`：点赞幂等记录。
5. `forum_comments`：评论记录。
6. `forum_imports`：导入记录，记录从哪个帖子导入到哪个用户和新 TaskClass ID。

### 3.5 关键流程

发布流程：

1. 用户选择自己的 TaskClass。
2. `taskclass-forum` 通过 TaskClass 读取端口拿到完整模板。
3. 服务过滤私有字段，生成论坛快照。
4. 写入帖子和模板快照。

导入流程：

1. 用户点击一键导入。
2. `taskclass-forum` 读取帖子模板快照。
3. 通过 TaskClass 写入端口为当前用户创建 TaskClass 副本。
4. 写入导入记录并增加导入计数。
5. 可异步发布 Token 奖励事件。

## 4. 模块二：Token 商店

### 4.1 产品定位

Token 商店负责 Token 的购买、奖励、发放和账本，不负责登录鉴权，也不直接承载 Agent 消耗统计。

`user/auth` 继续负责用户 Token quota 的权威判断；`token-store` 只负责产生“发放 Token”的业务事实，并通过跨服务契约通知 `user/auth` 增加用户额度。

### 4.2 P0 功能

1. 商品列表：展示可购买 Token 包。
2. 创建订单：用户选择商品生成订单。
3. 支付确认：P0 先支持 mock paid 或管理端确认 paid，不接真实支付网关。
4. Token 发放：订单支付成功后发放 Token。
5. 奖励发放：支持论坛点赞、导入等事件触发奖励。
6. 发放账本：所有发放必须有幂等 event_id，避免重复加额度。

### 4.3 P0 不做

1. 不接真实微信 / 支付宝 / Stripe。
2. 不做退款、发票、优惠券。
3. 不做复杂会员体系。
4. 不直接改 `users.token_usage`，避免和消费统计混淆。

### 4.4 核心实体

1. `token_products`：Token 商品。
2. `token_orders`：订单。
3. `token_grants`：Token 发放账本，记录购买、奖励、补偿等来源。
4. `token_reward_rules`：奖励规则，P0 可先用配置或简单表。

### 4.5 关键流程

购买流程：

1. 用户选择商品并创建订单。
2. 订单进入 `pending`。
3. P0 通过 mock paid 或管理端确认，把订单置为 `paid`。
4. `token-store` 写入 token grant 账本。
5. `token-store` 调用 `user/auth` 的额度发放能力。
6. 发放成功后订单进入 `granted`。

奖励流程：

1. 论坛产生点赞或导入事件。
2. `token-store` 按奖励规则判断是否发放。
3. 写入 token grant 账本。
4. 调用 `user/auth` 增加额度。

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
4. 调用 `user/auth` 增加 Token 额度。

不负责：

1. JWT、登录、注册。
2. Agent 消耗统计。
3. TaskClass 论坛内容。
4. 真实第三方支付回调，P0 只预留状态机。

### 5.3 与现有服务关系

1. 论坛读取和导入 TaskClass 时，先通过端口适配旧 `TaskClassService/DAO`。
2. 后续 `task-class` 独立成服务后，只替换端口适配器。
3. 论坛 P0 不直接写 schedule，避免被 `schedule` 未拆服务影响。
4. Token 商店不直接改 users 表，通过 `user/auth` 契约发放额度。

## 6. 事件与激励

P0 建议事件：

1. `forum.post.liked`：帖子被点赞。
2. `forum.post.imported`：帖子被导入。
3. `token.grant.requested`：请求发放 Token。
4. `token.grant.completed`：Token 发放完成。

奖励口径先从简单规则开始：

1. 每个帖子每个用户首次点赞只奖励一次。
2. 每个帖子每个用户首次导入只奖励一次。
3. 同一 event_id 的 Token 发放必须幂等。
4. 奖励额度先走配置，不在 PRD 阶段定死。

## 7. 当前推进策略

1. 当前工作区存在其它拆服务改动，本阶段只提交 PRD。
2. 等工作区干净后，从集成分支新开功能分支或单独 git worktree。
3. 实现时两个服务主体可以并行推进。
4. `gateway/router`、`shared/contracts`、`shared/ports`、`outbox route`、`config` 由主代理统一收口。
5. 先做 P0 闭环，再扩展审核、真实支付和推荐排序。

## 8. 待讨论问题

1. 论坛展示名使用“学习计划论坛”“计划广场”还是“模板市场”。
2. 点赞奖励是否给作者、点赞者，还是双方都给。
3. 导入奖励是否需要上限，避免刷导入。
4. 评论是否需要删除、举报、审核状态。
5. Token 发放应增加 `user/auth` 的 `GrantTokenQuota`，还是命名为 `AdjustTokenLimit`。
6. P0 是否需要前端先隐藏评论，只保留后端能力。
