# 第二阶段主动调度 MVP 实现方案

## 0. Handoff 说明

本文档已收口为第二阶段主动调度 MVP 的最终实施版。截至 2026-04-30，后端第一至第四阶段主体代码已实现并通过本地 `go test ./...`；真实飞书 webhook 配置接口和 `important_urgent_task` 主动触发端到端链路已通过本地后端验收。接手者请优先阅读本节、第 10 章装配边界和第 14 章验证 checklist，再从第五阶段剩余验收继续推进。

当前核心共识：

1. 主动调度主链路走固定 graph / service pipeline，不进入 ReAct 工具循环。
2. 第一版触发源先做 `important_urgent_task` 与 `unfinished_feedback`。
3. task 创建 / 更新时按 `urgency_threshold_at` upsert 主动调度 job；task 完成后把 job 标记为 `canceled`。
4. schedule 动态任务默认 `assumed_completed`，只有用户明确反馈未完成才触发补救。
5. 调度触发信号需要持久化，用于幂等、审计、排障和串联 trigger -> preview -> notification -> apply。
6. task_pool 任务进入日程时不创建孤儿 task_item，而是在 `schedule_events` 上新增 `task_source_type`：
   - `task_source_type=task_item` 时，`rel_id` 指向 `task_items.id`。
   - `task_source_type=task_pool` 时，`rel_id` 指向 `tasks.id`。
7. 主动调度预览新增 `active_schedule_previews`，不塞进 `agent_schedule_states`。
8. 预览保存 `base_version + before_summary + preview_changes`，不保存全量 before 快照。
9. 第一版不做 apply 成功后的撤销按钮；apply 失败必须事务不落库并回写失败原因。
10. 用户确认入口走主动调度详情页和确认 API，不走 Agent resume；详情页采用助手卡片式体验，支持拖动 after 方案后确认。
11. 预览有效期 1 小时。
12. 未完成补救第一版只生成新补做块，不直接移动原已排任务。
13. `schedule.apply.requested` 第一版不走 outbox 异步消费，确认 API 内同步完成重校验和正式应用；成功 / 失败直接回写预览状态。
14. 应用幂等使用独立 `apply_id + idempotency_key`，`preview_id + candidate_id` 只用于定位候选，不作为一次确认尝试的幂等键。
15. 飞书通知必须包含唯一预览链接 `/schedule-adjust/{preview_id}`；通知文案优先由 LLM 生成摘要，固定模板仅作为失败兜底。
16. 飞书通知幂等按 `user_id + trigger_type + time_window` 聚合，不按 `preview_id`；第一版落 `notification_records` 表支撑可观测与失败重试。
17. `api / worker / all` 启动边界第一阶段已完成；主动调度 MVP 可直接挂到 worker / 事件链路，不需要等待启动边界拆分。
18. 主动调度第一版采用“准独立模块”策略：不放进 `backend/service/active_scheduler`，而是放在 `backend/active_scheduler`；MVP 暂不拆独立 Go module / 独立进程。
19. 事件契约第一版提前放入 `backend/shared/events`，只承载 event type、event version、payload DTO 和基础校验，不放业务逻辑。
20. 主动调度采用 port / adapter 依赖边界：主链路不散落依赖其它领域 DAO；自有表用自有 repo；读取外部事实走 reader port；正式写入走 apply/service port。
21. 主动调度验收以“后端链路可观测 + 动作-预期 checklist”为准，覆盖 dry-run、trigger、worker、preview、notification、confirm apply、幂等、过期和失败回写。
22. 本轮给 `tasks` 新增 `estimated_sections`，默认 1，MVP 允许 1~4 节；主动调度只消费该字段，不在调度阶段重新推断任务复杂度。
23. 本轮给 `schedule_events` 新增来源与审计字段：`task_source_type / makeup_for_event_id / active_preview_id`。
24. `compress_with_next_dynamic_task` 第一轮实现先关闭，不生成该候选；保留 schema 和文档口径，待新增补做块主链路稳定后再打开。
25. 飞书第一版使用 mock / webhook 跑通主动触达闭环，不阻塞在用户 open_id 绑定体系上。
26. notification 去重窗口第一版固定为 30 分钟。
27. 真实飞书第一版走“用户级 Webhook 触发器”而不是群自定义机器人协议：后端按 `user_id` 查用户配置的 webhook URL，POST 极简业务 JSON；私聊、群聊、分支和后续动作由用户在飞书流程里自行编排。

### 0.1 多阶段推进计划

第一阶段：数据结构与事件契约。（已完成）

1. 新增迁移：`tasks.estimated_sections`、`schedule_events.task_source_type / makeup_for_event_id / active_preview_id`、`active_schedule_jobs`、`active_schedule_triggers`、`active_schedule_previews`、`notification_records`。
2. 新增 `backend/shared/events` 下的主动调度、通知、apply 结果事件契约。
3. 先补 repo / model / validate，不接 LLM、不接 provider。

第二阶段：主动调度 dry-run 主链路。（已完成）

1. 落 `backend/active_scheduler` 目录骨架、ports、adapters。
2. 实现 `BuildContext -> Observe -> GenerateCandidates`。
3. 先只支持 `important_urgent_task` 的 `add_task_pool_to_schedule` 和 `unfinished_feedback` 的 `create_makeup / ask_user / notify_only`。
4. `compress_with_next_dynamic_task` 首轮关闭，不生成候选。

第三阶段：预览与确认。（已完成）

1. 实现 `active_schedule_previews` 写入与详情查询。
2. 实现 confirm API：`apply_id + idempotency_key`、过期校验、`edited_changes` 重校验、同步 apply。
3. task_pool 正式落库写 `schedule_events(type=task, task_source_type=task_pool, rel_id=tasks.id)`。
4. 补做块新增 event，不移动原已排任务。

第四阶段：worker 与 notification。（主体代码已完成，真实 webhook 配置接口已验收）

1. 接入 `active_schedule.triggered` worker handler 和 due job scanner。
2. 接入 `notification.feishu.requested` handler。
3. 先使用 mock provider，再接用户级飞书 Webhook 触发器 provider。
4. `notification_records` 支持幂等、状态流转和 provider retry。
5. 新增用户通知配置入口：保存 / 查询 / 删除 / 测试当前用户的飞书 webhook。

第五阶段：端到端验收与收口。（部分验收中）

1. 跑通 `api / worker / all` 三种启动模式。
2. 按第 14 章 checklist 验证 dry-run、trigger、preview、notification、confirm apply、失败注入。
3. 根据日志和测试结果补齐 trace 字段与错误码。
4. 主链路稳定后再评估是否打开压缩融合候选。

### 0.2 子代理并行推进计划

可在实现阶段使用 3 到 5 个子代理并行推进，但必须按文件所有权拆分，避免互相覆盖。

1. 子代理 A：数据与契约。
   - 负责 migrations、model、repo、`backend/shared/events`。
   - 不改 API handler、不改 active_scheduler pipeline。
2. 子代理 B：主动调度核心。
   - 负责 `backend/active_scheduler/context / observe / candidate / selection / timegrid / ports`。
   - 不改正式 apply、不改 notification provider。
3. 子代理 C：预览与 apply。
   - 负责 `backend/active_scheduler/preview / apply / apply/convert` 和 confirm 相关服务。
   - 不改 worker handler、不改 notification。
4. 子代理 D：worker 与 notification。
   - 负责 `backend/service/events` 中主动调度与通知 handler、`backend/notification`、retry scanner。
   - 不改 active_scheduler 核心候选逻辑。
5. 子代理 E：API 与验证。
   - 负责 `backend/api/active_schedule.go`、路由接入、端到端测试脚本 / checklist 验证。
   - 不改底层 repo 和 provider。

并行规则：

1. 每个子代理只改自己负责的目录；跨目录依赖通过接口或临时占位实现对齐。
2. 先由子代理 A 完成事件契约和表结构，其他子代理基于契约开发。
3. 合并顺序建议：A -> B -> C -> D -> E。
4. 每轮集成后运行相关 Go 测试；按项目规则测试后清理 `.gocache`。
5. 若发现公共能力第三次复制，暂停并抽公共 helper，不让并行开发制造长期重复实现。

### 0.3 当前实现状态与接力记录

本节用于新对话接手后快速对齐当前代码状态，避免重新翻历史讨论。

已完成阶段：

1. 第一阶段：数据结构与事件契约。
   - 已新增 `tasks.estimated_sections`，默认 1。
   - 已新增 `schedule_events.task_source_type / makeup_for_event_id / active_preview_id`。
   - 已新增主动调度相关 model / DAO / 事件契约：`backend/model/active_schedule.go`、`backend/dao/active_schedule.go`、`backend/shared/events`。
   - AutoMigrate 已接入，并对历史 `schedule_events.type=task` 做 `task_source_type=task_item` 回填。
2. 第二阶段：主动调度 dry-run 主链路。
   - 已落 `backend/active_scheduler` 准独立模块骨架。
   - 已实现 `BuildContext -> Observe -> GenerateCandidates`。
   - 已开放 `POST /api/v1/active-schedule/dry-run`。
   - task 创建 / 更新会 upsert `active_schedule_jobs`；task 完成 / 删除会取消 pending job。
3. 第三阶段：预览与确认。
   - 已开放：
     ```text
     POST /api/v1/active-schedule/preview
     GET /api/v1/active-schedule/preview/:preview_id
     POST /api/v1/active-schedule/preview/:preview_id/confirm
     ```
   - 已实现 preview 写入、详情查询、`apply_id + idempotency_key`、候选转换、同步 apply adapter。
   - `add_task_pool_to_schedule` 已能正式写入 `schedule_events(type=task, task_source_type=task_pool, rel_id=tasks.id)` 和对应 `schedules`。
   - `create_makeup` 转换与 adapter 已预留并实现基本写入路径，但尚需在第四 / 第五阶段结合正式 unfinished feedback worker 场景补端到端验收。
4. 第四阶段：worker 与 notification 主体代码。
   - 已接入 `active_schedule.triggered` worker handler、due job scanner、`notification.feishu.requested` handler 和 notification retry loop。
   - 已新增 `backend/notification` provider / service 分层，mock provider 保留，真实投递切到用户级飞书 Webhook 触发器 provider。
   - 已新增 `user_notification_channels` model / DAO，并接入 AutoMigrate 与 `RepoManager`。
   - 已开放当前用户飞书 webhook 配置接口：
     ```text
     GET    /api/v1/notification/channels/feishu
     PUT    /api/v1/notification/channels/feishu
     DELETE /api/v1/notification/channels/feishu
     POST   /api/v1/notification/channels/feishu/test
     ```
   - `cmd/start.go` 已把正式 notification service 注入为 `WebhookFeishuProvider`；测试配置接口与正式投递复用同一个 provider 实例。
   - 用户未配置或禁用 webhook 时，通知记录进入 `skipped`，不阻塞主动调度 preview 链路。

本轮实测结果：

1. 测试账号：`test0430 / 123456`，当前本地环境 user_id 为 3。
2. API 验证链路：
   - 创建测试任务：`task_id=19`。
   - 生成 preview：`preview_id=asp_3bb18dcf-bd3a-433d-99ca-7ffadc1d6368`。
   - 后端候选：`candidate_id=add_task_pool_to_schedule:19:9:4:3`，`candidate_type=add_task_pool_to_schedule`。
   - confirm 成功：`apply_id=asap_19a3c6ae1cd7d308dc6b4fe2`。
3. DB 验证结果：
   - `active_schedule_previews.status=applied`，`apply_status=applied`，`applied_event_ids_json=[423]`。
   - `schedule_events.id=423`，`user_id=3`，`type=task`，`task_source_type=task_pool`，`rel_id=19`，`active_preview_id=asp_3bb18dcf-bd3a-433d-99ca-7ffadc1d6368`。
   - `schedules.id=877`，`event_id=423`，`week=9`，`day_of_week=4`，`section=3`，`status=normal`。
4. 幂等验证：
   - 使用同一 `preview_id + idempotency_key` 重复 confirm，返回同一 `apply_id` 和同一 `event_id=423`。
   - DB 中该 `active_preview_id` 只对应 1 条 `schedule_events` 和 1 条 `schedules`。
5. 测试命令：
   - 已在 `backend` 目录执行 `go test ./...` 并通过。
   - 已按项目规则清理根目录 `.gocache`。
6. 第四阶段本轮自动化结果：
   - 临时新增 `backend/notification/webhook_provider_test.go` 验证 payload 拼装、飞书 webhook URL 校验与脱敏规则；测试通过后已按项目规则删除临时 `*_test.go`。
   - 已再次执行 `go test ./...` 并通过；`GOCACHE` 明确指向项目根目录 `.gocache`，命令结束后已清理。
   - 后端按最新代码启动后，已注册本地测试账号 `codex_webhook_0430_183147`（user_id=6）。
   - 已调用 `PUT /api/v1/notification/channels/feishu` 保存用户飞书 webhook；接口返回 `configured=true`、`enabled=true`、脱敏回显为 `https://www.feishu.cn/flow/api/trigger-webhook/e889...6624`。
   - 已调用 `POST /api/v1/notification/channels/feishu/test`；接口返回 `status=success`、`outcome=success`，`last_test_status=success`，`last_test_at=2026-04-30T18:31:47.885+08:00`。
7. 第五阶段 `important_urgent_task` 端到端验收结果：
   - 测试账号：`codex_e2e_0430_185311 / 123456`，当前本地环境 user_id 为 7。
   - 已保存同一个飞书 webhook 配置，创建测试任务 `task_id=82`，同步 dry-run 返回 `decision=select_candidate` 且候选数为 1。
   - 已调用 `POST /api/v1/active-schedule/trigger` 写入正式 trigger：`trigger_id=ast_39a7f87a-d037-4361-82e5-03f58e4733a3`，`trace_id=trace_api_trigger_7_1777546391942562200`。
   - worker 已生成 preview：`preview_id=asp_e6701977-aeed-4bef-9964-29d26014f73d`，`active_schedule_triggers.status=preview_generated`，`active_schedule_previews.status=ready`。
   - outbox 两段均消费成功：`active_schedule.triggered` 对应 outbox id 2986 为 `consumed`；`notification.feishu.requested` 对应 outbox id 2987 为 `consumed`。
   - notification 投递成功：`notification_records.id=2`，`status=sent`，`attempt_count=1`，`provider_message_id=feishu_webhook_2_1777546395537770600`。
   - `provider_request_json.event=smartflow.schedule_adjustment_ready`，`message.title=SmartFlow 日程调整建议`，`message.action_url=https://smartflow.example.com/schedule-adjust/asp_e6701977-aeed-4bef-9964-29d26014f73d`。
   - 飞书 webhook 响应：HTTP 200，响应体 `{"code":0,"data":{},"msg":"success"}`。
8. 第五阶段补充自动验收结果：
   - skipped 场景：测试账号 `codex_skip_idem_0430_185759`（user_id=8）未配置 webhook，正式 trigger `ast_da60cd1c-1909-4855-ad5d-53125b19fb76` 生成 preview `asp_9e5c9c46-3460-4065-a2b8-1d531cf0c8aa`；`notification_records.id=3` 进入 `skipped`，`last_error_code=recipient_missing`，两段 outbox 均为 `consumed`。
   - trigger 幂等：同一账号、同一 task、同一 `idempotency_key` 重复调用 `POST /api/v1/active-schedule/trigger`，第二次返回同一个 trigger_id，`dedupe_hit=true`。
   - confirm apply 成功与幂等：对 preview `asp_e6701977-aeed-4bef-9964-29d26014f73d` 确认 candidate `add_task_pool_to_schedule:82:9:4:3`，生成 `apply_id=asap_039719fda4f2ae75f1d3d1fe`、`schedule_events.id=2488`、`schedules.id=5177`；同一幂等键重复确认返回同一个 apply_id 和 event_id，DB 中该 preview 只落 1 条正式事件。
   - `unfinished_feedback` 端到端：基于 `schedule_events.id=2488` 触发 `unfinished_feedback`，trigger `ast_25aced9e-554a-4021-9075-7166cf268480` 生成补做块 preview `asp_555e4cb9-b3c4-4e5e-8830-bd271c99e346`；`notification_records.id=4` 为 `sent`，飞书 webhook HTTP 200，响应体 `{"code":0,"data":{},"msg":"success"}`。
   - failed 场景：测试账号 `codex_fail_0430_190101`（user_id=10）配置 `https://www.feishu.cn:81/...` 不可达端口，trigger `ast_cd8b2de9-d836-4470-ad6a-c02c32142274` 生成 preview `asp_e5db98b2-b6bc-4683-8664-ae3d7eb76c25`；`notification_records.id=6` 进入 `failed`，`last_error_code=provider_timeout`，并写入 `next_retry_at`。
   - retry loop 恢复：将 `notification_records.id=6` 对应用户 webhook 改回真实地址并把 `next_retry_at` 调到当前时间，后台 retry loop 自动重试后该记录变为 `sent`，最终 `attempt_count=3`，HTTP 200。
   - dead 场景：测试账号 `codex_dead_runtime_0430_190150`（user_id=11）通过 DB 注入非法 `http://` webhook URL，trigger `ast_fc162833-7223-4aba-89c9-194ecdfbcf40` 生成 preview `asp_731f7cb2-c5dd-4629-83cd-627bec901e30`；`notification_records.id=7` 进入 `dead`，`last_error_code=invalid_url`，`next_retry_at=NULL`。
   - api-only 启动边界：仅启动 API 后，健康检查通过；测试账号 `codex_api_mode_0430_190708`（user_id=12）创建任务 `task_id=87` 并调用正式 trigger，得到 `trigger_id=ast_b48c955f-dcb3-4e87-a296-fd98583e4807`、`status=pending`。等待 6 秒后 DB 确认 `active_schedule_triggers.status=pending`、preview 数为 0、notification 数为 0、outbox id 3008 为 `active_schedule.triggered / pending`，证明 API 模式只写入 outbox，不启动 worker 消费。
   - worker-only 启动边界：仅启动 worker 后，HTTP 健康检查超时，符合“不注册 API 路由”预期；worker 消费 api-only 留下的 outbox id 3008，`active_schedule_triggers.status=preview_generated`，生成 preview `asp_badb4be4-cf2c-4f9b-9719-cbe92f50abed`，`notification_records.id=8` 因该用户未配置 webhook 进入 `skipped`，`notification.feishu.requested` outbox id 3009 为 `consumed`。

下一阶段入口：

1. 下一步继续第五阶段剩余验收，不需要重做 dry-run / preview / confirm 主链路，也不需要重做第四阶段 provider / handler 主体代码。
2. 第五阶段剩余重点：
   - confirm apply 冲突失败、过期拒绝。
   - 更完整的边界清理：测试数据隔离策略、失败注入脚本化、前端真实地址替换 `smartflow.example.com`。
4. 工作区注意：
   - 另一个前端对话可能在改前端；后端阶段不要碰 `frontend` 相关改动。
   - 当前允许单个 Go 文件 700 行以内；超过 700 再评估拆分。
   - 每次执行 `go test` 后必须清理根目录 `.gocache`。
   - 后续阶段必须优先自动化验收：能由代码、API、DB 查询、日志查询验证的内容，由实现者自己跑完并记录结果。
   - 如果受限于外部账号、真实飞书环境、浏览器人工交互、权限或本地环境，导致某项验收无法完成，不能默认为通过，也不能在报告中省略；必须明确写出未验收项、阻塞原因、建议由用户执行的操作和预期结果。

## 1. 文档目的

本文档承接《第二阶段主动调度 MVP 功能预期》和《微服务四步迁移与第二阶段并行开发计划》，用于把产品预期逐步落成可执行的工程方案。

本文档已经完成业务逻辑、工程边界、执行计划和验证流程收口。实现时按第 0.1 节阶段推进，遇到未覆盖细节时优先遵循第 2 章总体原则和第 10 章迁移边界。

## 2. 总体实现原则

1. 主动调度只生成诊断、候选和预览，不直接修改正式日程。
2. LLM 只在后端生成的候选里做选择，不自由构造正式写库参数。
3. 后台 worker 是主动调度主链路，API 只提供测试触发、预览查询、用户确认和正式应用入口。
4. 当前仍在 `backend` Go module 内实现，但代码边界按未来 `active-scheduler` 独立服务设计。
5. 飞书第一版只走 `notification.feishu.requested` 通知事件，不承载确认和复杂聊天。
6. 所有触发源统一进入 `active_schedule.triggered`，禁止每种触发单独写一套调度逻辑。
7. 正式应用优先复用现有 schedule / task_class service，不在主动调度模块绕过既有写入链路。

## 3. 目标链路

```text
后台定时 / 事件 / API 测试触发
  -> active_schedule.triggered
  -> 构造 ActiveScheduleContext
  -> 刷新四象限紧急性派生
  -> 读取滚动 24 小时任务与日程事实
  -> 主动观测并生成 issues / decision / candidates
  -> 写入待确认对比预览
  -> 发布 schedule.preview.generated
  -> 发布 notification.feishu.requested
  -> 用户回系统查看并按候选确认
  -> 确认 API 生成 apply_id 并同步重校验
  -> 复用正式应用链路写入 MySQL
  -> schedule.apply.succeeded / schedule.apply.failed
```

## 4. 模块一：触发入口与事件契约

### 4.1 业务实现逻辑简述

主动调度不应该依赖用户打开聊天后才发生。第一版需要支持三类入口：

1. 后台 worker 定时扫描或按事件触发。
2. API dry-run / trigger 测试触发，便于开发和验收。
3. 用户反馈类触发，例如明确说某个已排任务没完成，或表达疲劳。

三类入口最终都归一成同一个 `ActiveScheduleTrigger`，再进入同一条观测链路。

### 4.2 已拍板结论

1. 第一版触发源是否只做两个：`important_urgent_task` 和 `unfinished_feedback`？
   - 已确认：第一版先做这两类主触发。`fatigue_feedback` 可作为用户反馈类的后续扩展，不抢第一轮主链路。
2. API 测试触发是否允许直接同步返回诊断结果，还是必须也写入 outbox 后异步消费？
   - 已确认：两种都保留。`dry-run` 同步返回诊断结果，不写预览、不发通知；`trigger` 走正式异步链路，写预览并发布通知事件。
3. `mock_now` 是否只允许测试接口传入，后台真实 worker 禁止传入？
   - 已确认：`mock_now` 只允许 API dry-run / 测试 trigger 使用；后台 worker 正式定时触发必须使用真实当前时间。
4. 同一用户短时间多次触发的去重窗口设多长？
   - 已确认：`important_urgent_task` 按 `user_id + trigger_type + target_task_id` 做 30 分钟去重；`unfinished_feedback` 按用户反馈的 `feedback_id / idempotency_key` 防重复提交，不做固定时间窗强去重。

### 4.3 执行计划：触发入口与事件契约

本模块只负责把各类入口统一归一成 `ActiveScheduleTrigger`，并决定同步 dry-run、正式 trigger、worker due job 和用户反馈如何进入同一条主动调度 pipeline。上下文构造、候选生成、预览写入和通知投递的内部 schema 分别在后续模块细化。

#### 4.3.1 代码落点

1. 事件契约：
   ```text
   backend/shared/events/active_schedule.go
   ```
   只放 event type、event version、payload DTO、基础校验和消息键构造。
2. 主动调度触发入口：
   ```text
   backend/active_scheduler/trigger
   ```
   负责 trigger DTO、幂等判断、trigger 记录写入和正式 pipeline 入口编排。
3. API handler：
   ```text
   backend/api/active_schedule.go
   ```
   只负责鉴权用户、绑定请求、调用 active_scheduler service，不直接构造候选。
4. 路由注册：
   ```text
   backend/routers
   ```
   按现有鉴权路由风格挂载 dry-run、trigger、preview 查询和 confirm；本节只补 dry-run / trigger。
5. worker handler：
   ```text
   backend/service/events/active_schedule_triggered.go
   ```
   只负责消费事件、解析 payload、调用 active_scheduler trigger service。
6. due job 扫描器：
   ```text
   backend/active_scheduler/job
   ```
   负责扫描到期 `active_schedule_jobs`，重新读取 task 真值后生成 trigger。

#### 4.3.2 DTO 字段定义

`ActiveScheduleTrigger` 是内部统一输入，建议字段如下：

```text
trigger_id              # active_schedule_triggers.id；dry-run 可为空
user_id
trigger_type            # important_urgent_task / unfinished_feedback
source                  # worker_due_job / api_trigger / api_dry_run / user_feedback
target_type             # task_pool / schedule_event / task_item
target_id
feedback_id             # unfinished_feedback 场景使用，可为空
idempotency_key         # API / 用户反馈幂等键
dedupe_key              # important_urgent_task 30 分钟去重键，或 feedback 幂等键
mock_now
is_mock_time
requested_at
payload                 # 触发源补充信息，JSON DTO，不塞任意 map
trace_id
```

`trigger_type` 第一版只允许：

```text
important_urgent_task
unfinished_feedback
```

`source` 第一版只允许：

```text
worker_due_job
api_trigger
api_dry_run
user_feedback
```

`target_type` 第一版建议允许：

```text
task_pool       # rel_id / target_id 指向 tasks.id
schedule_event  # 用户反馈“某个已排日程没完成”
task_item       # 后续补救或明确定位 task_item 时使用
```

#### 4.3.3 事件契约

事件名：

```text
active_schedule.triggered
```

版本：

```text
event_version = 1
```

payload 示例：

```json
{
  "trigger_id": "ast_123",
  "user_id": 10001,
  "trigger_type": "important_urgent_task",
  "source": "worker_due_job",
  "target_type": "task_pool",
  "target_id": 345,
  "idempotency_key": "",
  "dedupe_key": "10001:important_urgent_task:task_pool:345:2026-04-30T10:00",
  "mock_now": null,
  "is_mock_time": false,
  "requested_at": "2026-04-30T10:00:00+08:00",
  "payload": {
    "job_id": "asj_789",
    "urgency_threshold_at": "2026-04-30T10:00:00+08:00"
  },
  "trace_id": "trace_xxx"
}
```

消息键建议：

```text
message_key = user_id
aggregate_id = trigger_id
```

规则：

1. `active_schedule.triggered` 只表示“主动调度链路需要处理一个触发信号”，不表示已经生成 preview。
2. payload 必须带 `trigger_id`，方便后续串联 `trigger -> preview -> notification -> apply`。
3. dry-run 不发布该事件。
4. API trigger、worker due job、用户反馈正式触发都可以发布该事件。
5. 消费者必须按 `event_type + event_version` 解析，不直接依赖 active_scheduler 内部 struct。

#### 4.3.4 API 路由设计

建议新增鉴权接口：

```text
POST /active-schedule/dry-run
POST /active-schedule/trigger
```

`dry-run` 请求：

```json
{
  "trigger_type": "important_urgent_task",
  "target_type": "task_pool",
  "target_id": 345,
  "mock_now": "2026-04-30T10:00:00+08:00",
  "payload": {}
}
```

`dry-run` 响应：

```json
{
  "trigger": {},
  "context_summary": {},
  "issues": [],
  "decision": {},
  "candidates": []
}
```

`trigger` 请求：

```json
{
  "trigger_type": "important_urgent_task",
  "target_type": "task_pool",
  "target_id": 345,
  "mock_now": "2026-04-30T10:00:00+08:00",
  "idempotency_key": "client-generated-key",
  "payload": {}
}
```

`trigger` 响应：

```json
{
  "trigger_id": "ast_123",
  "status": "pending",
  "deduped": false
}
```

接口语义：

1. `dry-run` 同步执行到 decision / candidates，绝不写 `active_schedule_triggers / active_schedule_previews / notification_records`。
2. `dry-run` 允许 `mock_now`，但必须在返回 trace 中标记 `is_mock_time=true`。
3. `trigger` 走正式链路，先写 trigger，再发布 `active_schedule.triggered`，由 worker 消费生成 preview 和 notification。
4. `trigger` 允许 `mock_now`，但必须持久化 `is_mock_time=true`，避免排障误判。
5. 后台 worker due job 不允许 `mock_now`，必须使用真实当前时间。

#### 4.3.5 幂等与去重

`important_urgent_task`：

```text
dedupe_key = user_id + trigger_type + target_type + target_id + 30分钟窗口
```

预期行为：

1. 30 分钟内命中相同 dedupe key 时，不重复写新 preview，不重复发飞书。
2. 若已有 trigger 仍在 `pending / processing / preview_generated`，直接返回已有 trigger 状态。
3. 若上一轮 `failed`，是否允许重新触发由表结构状态机阶段细化；MVP 倾向允许人工测试 trigger 重新触发，但必须生成新的 trace。

`unfinished_feedback`：

```text
dedupe_key = user_id + trigger_type + feedback_id/idempotency_key
```

预期行为：

1. 不做固定 30 分钟窗口强去重。
2. 同一 `feedback_id / idempotency_key` 重复提交时返回已有 trigger。
3. 用户连续表达“还是没做完”时，只要反馈 ID 或幂等键不同，就允许进入新的补救链路。

#### 4.3.6 worker handler 流程

worker 消费 `active_schedule.triggered`：

```text
1. 解析 shared/events payload。
2. 校验 trigger_id / user_id / trigger_type / target_type / target_id。
3. 查询 active_schedule_triggers 当前状态。
4. 若状态已完成或已跳过，直接幂等返回。
5. 将 trigger 标记为 processing。
6. 调用 active_scheduler pipeline：
   BuildContext -> Observe -> GenerateCandidates -> LLMSelectAndExplain -> WritePreview -> Notify
7. 成功写 preview 后，将 trigger 标记为 preview_generated。
8. 若无 issue 或后端裁决 close，将 trigger 标记为 skipped/closed，并记录 reason。
9. 失败则标记 failed，写 error，保留 outbox 重试语义。
```

due job 扫描器流程：

```text
1. 扫描 due 且未完成的 active_schedule_jobs。
2. 重新读取 task 真值。
3. task 已完成 -> job 标记 canceled/skipped。
4. task 不再满足重要且紧急 -> job 标记 skipped。
5. task 已进入 schedule -> job 标记 skipped。
6. 仍需主动调度 -> 写 trigger 并发布 active_schedule.triggered。
```

#### 4.3.7 错误处理与可观测

1. payload 解析失败：outbox 标记 dead，记录解析错误。
2. 参数非法：trigger 标记 failed 或 rejected，记录原因，不进入 pipeline。
3. 幂等命中：不视为错误，返回已有 trigger / preview 状态。
4. pipeline 失败：trigger 标记 failed，保留 error message 和 trace。
5. preview 写入失败：不发布 notification。
6. notification 发布失败：preview 保留，trigger 可标记 preview_generated，但 notification 状态由 notification 模块记录。
7. 所有正式 trigger 必须能通过 `trace_id / trigger_id / target_id` 查到链路日志。

#### 4.3.8 测试方案

单元测试：

1. `trigger_type / source / target_type` 枚举校验。
2. `mock_now` 只在 `api_dry_run / api_trigger` 允许。
3. `important_urgent_task` dedupe key 生成。
4. `unfinished_feedback` idempotency key 生成。
5. `active_schedule.triggered` payload validate。
6. dry-run 不写 trigger / preview / notification。

集成测试：

1. API `dry-run` 返回 diagnosis / candidates，不落库。
2. API `trigger` 写 `active_schedule_triggers` 并发布 `active_schedule.triggered`。
3. worker 消费事件后推进 trigger 状态到 `processing -> preview_generated`。
4. 30 分钟内重复 `important_urgent_task` 触发命中去重。
5. 相同 `unfinished_feedback.idempotency_key` 重复提交命中幂等。
6. due job 到期但 task 已完成时标记 skipped/canceled，不写 preview。
7. payload 非法时 outbox dead 或 trigger failed，错误可查询。

人工验收：

1. 使用 dry-run 验证某个 task_pool 任务能生成候选。
2. 使用 trigger 验证 worker 能写 preview。
3. 重复点击 trigger，确认不重复生成多条 preview 和飞书通知。
4. 修改 task 为 completed 后触发 due job，确认不会进入主动调度链路。

## 5. 模块二：ActiveScheduleContext 构造

### 5.1 业务实现逻辑简述

`ActiveScheduleContext` 是主动调度的统一输入快照。它负责把用户、时间窗、任务、日程、四象限任务池、偏好、近期反馈和触发来源装配到一起。

上下文构造阶段需要先触发或复用四象限紧急性派生，避免后台读到懒加载前的旧任务池。

### 5.2 已拍板结论

1. 滚动 24 小时如何映射到当前“周 + 星期 + 节次”模型？是否第一版只按节次粒度处理？
   - 已确认：候选窗口按任务 DDL / 当前滚动 24 小时映射到现有相对时间坐标（week/day_of_week/section），正式写入仍同时维护 schedule 现有的绝对时间与相对时间字段。
   - 已确认：第一版统一按 1 节粒度处理；任务预计长度先限定在 1~4 节，后续可在创建 task 时由 AI 根据复杂度写入预计节数。
2. 四象限任务池里的 `tasks` 是否需要映射到 `task_items`，还是主动调度预览直接支持 task_pool 任务？
   - 已确认：不创建无所属任务类的“孤儿 task_item”。四象限任务进入日程时保留 task_pool 身份，通过 `schedule_events.task_source_type=task_pool` 指向 `tasks.id`。
3. 用户偏好第一版从哪里注入：memory 摘要、task_class 配置，还是先只消费已有排程约束？
   - 已确认：若候选目标来自 task 池，优先使用 memory 中的用户偏好；若候选目标来自 task_item，则使用所属 task_class 的硬性偏好和约束。
4. 近期用户反馈是否第一版只作为 trigger payload，不落数据库状态？
   - 已确认：用户反馈类触发信号需要持久化，但不面向前端展示；主要用于幂等、审计、排障和串联 trigger -> preview -> notification -> apply 链路。

### 5.3 执行计划：ActiveScheduleContext 构造

本模块只负责把触发信号转换成主动观测所需的事实快照，不负责生成候选、不调用 LLM、不写 preview。上下文构造必须尽量确定性、可测试、可排障。

#### 5.3.1 代码落点

1. Context DTO：
   ```text
   backend/active_scheduler/context
   ```
2. 读取端口定义：
   ```text
   backend/active_scheduler/ports
   ```
3. 本地 adapter：
   ```text
   backend/active_scheduler/adapters
   ```
4. 时间窗与节次转换辅助：
   ```text
   backend/active_scheduler/timegrid
   ```
5. 与既有公共能力复用：
   - 优先复用 `conv.RealDateToRelativeDate`、`conv.RelativeTimeToRealTime` 等现有时间转换能力。
   - 若需要新的滚动窗口到节次格转换，放入 `timegrid`，避免散落在 observe / candidate 里。

#### 5.3.2 ActiveScheduleContext 结构

建议结构方向：

```text
ActiveScheduleContext
  Trigger
  User
  Now
  Window
  Target
  TaskPoolFacts
  ScheduleFacts
  TaskClassFacts
  PreferenceFacts
  FeedbackFacts
  DerivedFacts
  Trace
```

字段语义：

```text
Trigger
  trigger_id
  trigger_type
  source
  target_type
  target_id
  is_mock_time
  payload

User
  user_id
  timezone

Now
  real_now        # 后台真实当前时间
  effective_now   # dry-run / trigger 可使用 mock_now

Window
  start_at
  end_at
  relative_slots  # week/day_of_week/section 原子格列表
  window_reason   # rolling_24h / task_deadline / task_class_end_date

Target
  source_type     # task_pool / schedule_event / task_item
  task_id
  schedule_event_id
  task_item_id
  title
  estimated_sections
  deadline_at
  urgency_threshold_at
  priority
  status

TaskPoolFacts
  target_task
  urgent_unscheduled_tasks

ScheduleFacts
  events
  occupied_slots
  free_slots
  next_dynamic_task

TaskClassFacts
  task_class
  affected_items
  constraints

PreferenceFacts
  memory_context_text
  memory_items
  task_class_constraints
  preference_source

FeedbackFacts
  feedback_id
  feedback_text
  feedback_target

DerivedFacts
  target_already_scheduled
  target_completed
  available_capacity
  missing_info

Trace
  trace_id
  build_steps
  warnings
```

约束：

1. `ActiveScheduleContext` 是只读快照，不包含 DAO / service 实例。
2. context 中的时间统一使用带时区的绝对时间，同时保留相对节次格。
3. context 中只放主动观测需要的事实，不塞完整数据库 model。
4. `missing_info` 是正常输出，用于后续裁决 `ask_user / notify_only`，不是构造失败。

#### 5.3.3 读取端口

主动调度 pipeline 只依赖 port，不直接 import 其它领域 DAO。

建议端口：

```go
type TaskReader interface {
    GetTaskForActiveSchedule(...)
    ListUrgentUnscheduledTasks(...)
    IsTaskScheduled(...)
}

type ScheduleReader interface {
    GetScheduleFactsByWindow(...)
    GetFreeSlots(...)
    GetNextDynamicTask(...)
    HasSlotConflict(...)
}

type TaskClassReader interface {
    GetTaskItemWithClass(...)
    ListAffectedTaskItems(...)
    GetTaskClassConstraints(...)
}

type MemoryContextReader interface {
    LoadScheduleMemoryContext(...)
}

type FeedbackReader interface {
    GetFeedbackSignal(...)
}

type UrgencyRefresher interface {
    RefreshTaskUrgency(...)
}
```

`MemoryContextReader` 的语义建议：

```go
type ScheduleMemoryContextRequest struct {
    UserID      int
    Query       string
    TargetTitle string
    TriggerType string
    WindowStart time.Time
    WindowEnd   time.Time
    Now         time.Time
}

type ScheduleMemoryContextFacts struct {
    RenderedText string
    Items        []ScheduleMemoryItem
    Source       string
    Warnings     []string
}
```

说明：

1. port 命名为 `MemoryContextReader`，不命名为 `PreferenceReader`，避免暗示 memory 模块已经提供结构化日程偏好。
2. `RenderedText` 对齐 newAgent 的 `memory_context`：给 LLM 参考，但不作为硬规则。
3. `Items` 只保留排障需要的轻量字段，例如 `id / memory_type / title / content / confidence / importance`，不把 memory 模块内部 model 泄漏到主动调度主链路。

MVP adapter 规则：

1. 优先复用现有 service。
2. 若现有 service 无合适读模型，adapter 内部可调用 DAO 组装事实。
3. DAO 调用不能出现在 `BuildContext / Observe / GenerateCandidates` 主链路中。
4. adapter 返回 active_scheduler 自己的轻量事实 DTO，不直接返回 GORM model。
5. memory 侧不新增 `GetMemorySchedulePreferences` 这类结构化偏好 DAO；第一版复用现有 `memoryReader.Retrieve(ctx, memorymodel.RetrieveRequest)` 召回能力。
6. active_scheduler 不 import `backend/newAgent/node/execute`，也不依赖 `ConversationContext` / pinned block；只复用“召回 + 渲染为 memory context 文本”的底层能力。
7. 若实现时发现 memory 渲染逻辑只能从 `agentsvc` 访问，应先抽到 `backend/memory` 或 `backend/shared` 下的公共渲染 helper，再让 `agentsvc` 和 active_scheduler adapter 共同复用，避免复制第三份 prompt 拼装逻辑。

#### 5.3.4 构造顺序

建议固定顺序：

```text
1. NormalizeTrigger
2. ResolveEffectiveNow
3. RefreshUrgencyIfNeeded
4. ResolveTarget
5. BuildWindow
6. LoadScheduleFacts
7. LoadPreferenceFacts
8. LoadFeedbackFacts
9. DeriveFacts
10. ValidateContextForObserve
```

步骤说明：

1. `NormalizeTrigger`
   - 校验 trigger 枚举、target 枚举、用户归属。
   - 失败时直接返回构造错误，不进入观测。
2. `ResolveEffectiveNow`
   - API dry-run / trigger 可使用 `mock_now`。
   - worker due job 必须使用真实 `time.Now()`。
   - 写入 `is_mock_time` 到 trace。
3. `RefreshUrgencyIfNeeded`
   - 对 `important_urgent_task` 先刷新或复用四象限紧急性派生。
   - 目的是避免读到懒平移之前的旧优先级。
4. `ResolveTarget`
   - `task_pool`：读取 `tasks`。
   - `schedule_event`：读取对应日程块，并根据 `task_source_type` 判断来源。
   - `task_item`：读取 task_item 及 task_class。
5. `BuildWindow`
   - 默认窗口为 `[effective_now, effective_now + 24h]`。
   - 未完成补救场景若目标属于 task_class，可扩展到 `task_class.end_date`，供局部补救使用。
   - 所有窗口必须映射到 `week / day_of_week / section`。
6. `LoadScheduleFacts`
   - 读取窗口内课程、已排任务、可嵌入课程、空闲槽。
   - 生成 `occupied_slots / free_slots`。
7. `LoadPreferenceFacts`
   - target 是 `task_pool`：通过 `MemoryContextReader` 召回与排程相关的 memory context，作为软偏好输入。
   - target 是 `task_item`：读 task_class 约束。
8. `LoadFeedbackFacts`
   - `unfinished_feedback` 必须加载反馈目标和文本摘要。
   - 若无法定位反馈目标，写入 `missing_info`，由后续裁决 `ask_user`。
9. `DeriveFacts`
   - 判断目标是否已完成、是否已进入日程、窗口容量是否足够。
   - 这些是后续 observe 的确定性输入。
10. `ValidateContextForObserve`
   - 只校验能否进入 observe。
   - 信息不全但仍可 ask_user 的场景，不应直接失败。

#### 5.3.5 四象限刷新复用方案

规则：

1. `important_urgent_task` 构造 context 前必须调用 `UrgencyRefresher`。
2. 刷新以数据库真实时间或 `effective_now` 为准：
   - API dry-run / trigger：可使用 `mock_now`。
   - worker due job：使用真实当前时间。
3. 刷新结果不要求本次一定更新 task；如果 task 已不满足平移条件，后续 `DerivedFacts` 会标记 skipped/close。
4. 刷新失败：
   - dry-run：返回错误，便于开发发现问题。
   - 正式 trigger：trigger 标记 failed，记录 error，不继续生成 preview。
5. 不在 context 构造中重新实现四象限推导算法；复用现有 task urgency 能力或其 adapter。

#### 5.3.6 时间窗转换与边界兜底

时间窗默认：

```text
start_at = effective_now
end_at = effective_now + 24h
```

兜底规则：

1. 如果 `deadline_at` 早于 `effective_now`：
   - 仍构造 24 小时窗口。
   - `DerivedFacts` 标记 `deadline_already_passed=true`。
2. 如果 `deadline_at` 位于 24 小时内：
   - `window_reason` 标记包含 `task_deadline`。
   - 候选生成时优先考虑 deadline 前的槽位。
3. 如果窗口跨天 / 跨周：
   - 拆成多个相对时间格，不能只取当天。
4. 如果某段绝对时间无法映射到节次：
   - 丢弃该格，并在 `Trace.warnings` 记录。
   - 若全部无法映射，则 context 标记 `missing_info=invalid_time_window`，后续裁决为 `ask_user / notify_only`。
5. 第一版统一 1 节粒度：
   - `estimated_sections` 为空时默认 1。
   - 非法值小于 1 时按 1 兜底。
   - 非法值大于 4 时按 4 截断，并记录 warning。

#### 5.3.7 偏好来源

与当前 `execute.go` 链路的关系：

1. `backend/newAgent/node/execute.go` 本身只是转发壳，真正的 memory 注入发生在 graph/service 边界。
2. `agentsvc.injectMemoryContext` 会先读 Redis 预取缓存，再启动后台检索；检索结果来自 `memoryReader.Retrieve(ctx, memorymodel.RetrieveRequest)`。
3. `agent_nodes.ensureFreshMemory` 只负责等待 `MemoryFuture`，并把已渲染文本写入 `ConversationContext` 的 `memory_context` pinned block。
4. `execute` prompt 只通过 `renderUnifiedMemoryContext(ctx)` 消费该 pinned block，不直接读取 memory DAO。
5. 因此主动调度应复用 memory 的“Retrieve + 渲染”能力，不复用 execute node / ConversationContext；主动调度没有对话轮次，也不需要引入 pinned block。

task_pool：

1. 不读取 task_class 约束。
2. 通过 `MemoryContextReader.LoadScheduleMemoryContext` 读取排程相关 memory。
3. adapter 内部使用现有 memory 模块的 `Retrieve`：
   - `UserID=user_id`
   - `Query` 由目标任务标题、触发类型、当前窗口意图拼成，例如“为 X 安排未来 24 小时的执行时间，参考用户的时间偏好和约束”
   - `MemoryTypes` 优先限制为 `constraint / preference / fact`
   - `Limit` 沿用 newAgent 注入预算或 active_scheduler 独立配置
   - `Now=effective_now`
4. adapter 返回 active_scheduler 自己的 `ScheduleMemoryContextFacts`，至少包含：
   - `items`：memory item 的轻量快照，用于排障和 trace。
   - `rendered_text`：复用公共 memory 渲染 helper 后得到的文本，用于 LLM 选择和解释。
   - `source=memory_retrieve`
   - `warnings`
5. memory 缺失时继续构造 context，`PreferenceFacts.preference_source=none`。
6. memory 查询失败不阻断主动调度，只记录 warning；这与 execute 链路“记忆检索失败不阻断主链路”的策略保持一致。
7. memory 中的偏好不能作为硬约束，只作为候选排序和解释输入；真正的硬冲突仍以后端 schedule facts / task_class constraints 为准。

task_item：

1. 必须读取所属 task_class。
2. 使用 task_class 的周几、时段、结束日期等约束。
3. 未完成补救场景中，这些约束后续可被局部重排模块软化，但 context 中仍保留原始约束。

unfinished_feedback：

1. 优先从 trigger payload 中解析 `feedback_id / feedback_text / target_id`。
2. 如果 payload 只有自然语言文本但无法定位目标，context 不失败，写入 `missing_info=feedback_target_unknown`。
3. 若能定位 `schedule_event`，需要读取该 event 的来源：
   - `task_source_type=task_pool`：关联 tasks。
   - `task_source_type=task_item` 或空：兼容旧数据，关联 task_items。

#### 5.3.8 输出给后续模块的契约

context 构造成功后，后续 observe 可依赖以下事实已经可用：

1. `Trigger` 已标准化。
2. `effective_now` 已确定。
3. `Window.relative_slots` 已生成。
4. 目标归属已校验。
5. schedule facts 已加载，至少包含空切片而不是 nil。
6. preference facts 已按 target 类型分流。
7. feedback facts 已持久化并能串联 trigger。
8. `DerivedFacts` 至少包含：
   - `target_completed`
   - `target_already_scheduled`
   - `available_capacity`
   - `missing_info`

#### 5.3.9 错误处理与可观测

1. 用户无权访问 target：构造失败，trigger 标记 failed/rejected。
2. target 不存在：构造失败，trigger 标记 failed/rejected。
3. schedule 查询失败：构造失败，trigger 标记 failed，可重试。
4. memory 查询失败：不阻断，写 warning，偏好来源置为 none。
5. task_class 查询失败：
   - target 是 task_item：阻断。
   - target 是 task_pool：不应查询 task_class，若发生说明 adapter 边界错误。
6. 时间窗部分映射失败：不阻断，写 warning。
7. 时间窗完全不可用：构造成功但 `missing_info=invalid_time_window`，交给 observe 裁决。

#### 5.3.10 测试方案

单元测试：

1. `mock_now` 与真实时间的 `effective_now` 选择。
2. 24 小时窗口跨天 / 跨周映射到相对节次。
3. `estimated_sections` 默认值、截断和 warning。
4. task_pool 偏好来源为 memory。
5. task_item 偏好来源为 task_class。
6. memory 读取失败不阻断 context。
7. feedback 无法定位目标时写入 missing_info。
8. target 已完成 / 已安排时写入 DerivedFacts。

集成测试：

1. API dry-run 触发 context 构造，返回 context summary。
2. 正式 trigger 通过 worker 构造 context，并推进 trigger 状态。
3. due job 触发前刷新四象限紧急性。
4. schedule 窗口存在冲突和空闲槽时，context 同时包含 `occupied_slots / free_slots`。
5. task_pool 不读取 task_class，task_item 必须读取 task_class。

人工验收：

1. 构造一个 24 小时内有空闲节次的 task_pool，dry-run 能看到可用窗口。
2. 构造一个 memory 偏好，例如“晚上更适合写作”，dry-run context summary 能显示偏好来源。
3. 构造一个已排 task_item 的 unfinished feedback，context 能定位到 schedule_event 和 task_item。
4. 构造无法定位的“刚才那个没做完”，context 不崩溃，后续裁决应进入 ask_user。

## 6. 模块三：主动观测与候选生成

### 6.1 业务实现逻辑简述

主动观测能力参考 `analyze_health`：后端先做结构化观测，再生成候选，让 LLM 做选择题。

第一版候选限制为 1 到 3 个，动作范围包括：

1. 加入日程预览。
2. 未完成补救预览。
3. 后继挤压重排预览。
4. 延后结束询问。
5. 询问用户。
6. 仅提醒。
7. 收口。

压缩融合候选第一轮只保留 schema 和文档口径，不进入候选生成动作范围。

### 6.2 已拍板结论

1. 主动观测最终是 Agent 工具，还是 worker 内部 service？第一版是否同时提供内部 service 和工具壳？
   - 已确认：主动观测不作为 ReAct 工具进入工具循环，而是串进固定 graph / service pipeline。LLM 直接消费观测与候选结果，负责选择和表达。
2. “重要且紧急任务未进入日程视图”的可用窗口查找，第一版是否允许打破 task_class 偏好？
   - 已纠正：task_pool 任务不属于 task_class，不存在 task_class 偏好可打破。第一版按用户 memory 偏好和滚动 24 小时内的可用时间生成候选；若 memory 偏好与可用容量冲突，候选中说明偏好未满足的代价，而不是称为“打破 task_class 偏好”。
3. 未完成补救里，局部重排第一版复用现有粗排算法到什么程度？
   - 已确认：第一版做“偏好软化版局部粗排”。输入时间窗为当前时刻到任务类结束日期，只传受影响的部分 item；周几偏好和时段偏好从硬约束降级为优先级，优先排偏好范围内，排不下再打破偏好追加进去，最后恢复这些任务的原有顺序语义。
   - 工程倾向：不直接污染现有粗排主函数，新增一条局部重排实现；底层时间格、可用槽位、冲突判断等公共能力优先抽公共层复用，避免复制第三份逻辑。
4. 压缩融合候选第一轮是否打开？
   - 已确认：第一轮先关闭，不生成 `compress_with_next_dynamic_task` 候选；保留 schema 和实现预留，待新增补做块主链路稳定后再评估打开。
5. close / ask_user / notify_only 的判定阈值由后端固定，还是允许 LLM 结合上下文选择？
   - 已确认：参考 `analyze_health` 的裁决模式，由后端确定 `close / ask_user / notify_only / select_candidate`。LLM 不决定能不能调度，只在 `select_candidate` 时选择候选；其它场景只负责解释后端理由。

### 6.3 执行计划：主动观测与候选生成

本模块负责把 `ActiveScheduleContext` 转成结构化诊断结果，并生成 1 到 3 个后端已校验的候选。它不写 preview、不发通知、不正式改日程；LLM 只在 `decision.action=select_candidate` 时从候选中选择和解释，不负责决定是否允许调度。

#### 6.3.1 代码落点

1. 主动观测：
   ```text
   backend/active_scheduler/observe
   ```
   负责 metrics / issues / decision 的确定性计算。
2. 候选生成：
   ```text
   backend/active_scheduler/candidate
   ```
   负责枚举、模拟、校验、排序和截断候选。
3. LLM 选择与解释：
   ```text
   backend/active_scheduler/selection
   ```
   只负责把后端候选喂给 LLM，让 LLM 返回 `selected_candidate_id / summary / reason / risk_text`。
4. 与 schedule 公共能力复用：
   ```text
   backend/active_scheduler/scheduleutil
   ```
   或后续下沉到更公共目录，用于放时间格、冲突判断、before/after 摘要转换等可复用能力。
5. 本模块输出 DTO：
   ```text
   backend/active_scheduler/model
   ```
   放 `ObservationResult / ActiveScheduleDecision / ActiveScheduleCandidate` 等主动调度内部结构。

#### 6.3.2 Pipeline 输入输出

输入：

```text
ActiveScheduleContext
```

输出：

```text
ActiveScheduleObservationResult
  metrics
  issues
  decision
  candidates
  trace
```

处理顺序：

```text
1. BuildMetrics
2. DetectIssues
3. DecideAction
4. GenerateCandidates
5. ValidateCandidates
6. RankAndTrimCandidates
7. SelectAndExplainByLLM
```

说明：

1. `BuildMetrics / DetectIssues / DecideAction / GenerateCandidates / ValidateCandidates / RankAndTrimCandidates` 全部由后端确定性完成。
2. `SelectAndExplainByLLM` 只在 `decision.action=select_candidate` 且候选数大于 0 时执行。
3. LLM 返回的 `candidate_id` 必须存在于后端候选列表；不存在或格式非法时，先进行一次受限重试。
4. 受限重试仍失败时不影响 preview 生成，使用后端 top1 和固定解释 fallback。

#### 6.3.3 Metrics schema

建议第一版 metrics 只保留能驱动裁决和排障的指标：

```text
ActiveScheduleMetrics
  target
    completed
    already_scheduled
    deadline_already_passed
    minutes_to_deadline
    estimated_sections

  window
    total_slots
    free_slots
    occupied_slots
    usable_slots_before_deadline
    capacity_gap

  preference
    source                 # memory / task_class / none
    matched_slot_count
    unmatched_reason

  feedback
    has_feedback
    feedback_target_known
    unfinished_elapsed_minutes

  risk
    conflict_count
    affected_event_count
    affected_task_count
    requires_reorder
```

指标语义：

1. `capacity_gap = estimated_sections - usable_slots_before_deadline`。
2. `matched_slot_count` 只表示满足软偏好的可用槽数量，不表示硬可排容量。
3. `requires_reorder=true` 表示候选可能涉及局部补救或压缩融合，不表示已经修改正式日程。
4. metrics 只描述事实，不夹带最终动作文案。

#### 6.3.4 Issues schema

issue 是后端观察到的问题或阻断点：

```text
ActiveScheduleIssue
  issue_id
  code
  severity              # critical / warning / info
  target_type
  target_id
  reason
  evidence
  can_generate_candidate
```

第一版 issue code：

```text
target_completed
target_already_scheduled
deadline_passed
no_valid_time_window
capacity_insufficient
no_free_slot
preference_not_satisfied
feedback_target_unknown
need_makeup_block
need_local_reorder
can_add_task_pool_to_schedule
can_compress_with_next_dynamic_task  # 预留，第一轮不生成
```

生成规则：

1. `target_completed`：目标已完成，后续 `decision.action=close`。
2. `target_already_scheduled`：任务已进入正式日程，后续 `decision.action=close` 或 `notify_only`。
3. `feedback_target_unknown`：无法定位用户说的“没完成”是哪一个日程块，后续 `decision.action=ask_user`。
4. `no_valid_time_window`：窗口无法映射成任何节次，后续 `decision.action=ask_user`。
5. `capacity_insufficient`：可用容量不足但存在补救可能，第一轮优先生成询问或仅提醒；压缩融合只保留预留 code，不生成候选。
6. `can_add_task_pool_to_schedule`：task_pool 任务可直接加入日程，是 `important_urgent_task` 的主路径。
7. `need_makeup_block / need_local_reorder`：未完成反馈需要生成补做块或局部补救候选。

#### 6.3.5 Decision schema

后端裁决结构：

```text
ActiveScheduleDecision
  action                 # close / ask_user / notify_only / select_candidate
  reason_code
  primary_issue_code
  should_notify
  should_write_preview
  llm_selection_required
  fallback_candidate_id
```

裁决优先级：

```text
1. close
2. ask_user
3. notify_only
4. select_candidate
```

规则：

1. `close`
   - 目标已完成。
   - 目标已进入日程且无需补救。
   - 没有观察到需要用户处理的问题。
2. `ask_user`
   - 反馈目标无法定位。
   - 时间窗完全不可用。
   - 任务缺少必要信息，且后端无法安全生成候选。
3. `notify_only`
   - 有风险或状态变化需要告知用户，但不适合自动生成可确认变更。
   - 例如 deadline 已过且没有合理补救窗口。
4. `select_candidate`
   - 至少存在 1 个后端合法候选。
   - `should_write_preview=true`。
   - `llm_selection_required=true`，让 LLM 在候选内选择并生成解释。

兜底：

1. `select_candidate` 但 LLM 输出非法：先受限重试一次，仍失败再使用 `fallback_candidate_id`。
2. `select_candidate` 但候选列表最终为空：降级为 `ask_user` 或 `notify_only`，不能写空 preview。
3. `ask_user / notify_only / close` 不调用 LLM 选择；是否需要 LLM 解释文案可在通知模块单独生成 summary，但不改变 decision。

#### 6.3.6 Candidate schema

候选结构：

```text
ActiveScheduleCandidate
  candidate_id
  candidate_type
  title
  summary
  target
  changes
  before_summary
  after_summary
  risk
  score
  validation
  source
```

`candidate_type` 第一版：

```text
add_task_pool_to_schedule
makeup_block
local_reorder_makeup
ask_delay_end
compress_with_next_dynamic_task  # 预留，第一轮关闭
notify_only
close
```

`changes` 使用预览模块可消费的统一变更项：

```text
ActiveScheduleChangeItem
  change_type            # add / move / compress / create_makeup / ask_user / none
  target_type            # task_pool / task_item / schedule_event
  target_id
  from_slot
  to_slot
  duration_sections
  affected_event_ids
  edited_allowed
  metadata
```

约束：

1. 候选必须能转成预览模块的 `preview_changes`。
2. 候选不能直接携带 DAO model。
3. `close / notify_only / ask_delay_end` 可以没有正式日程变更，但仍要有明确 `candidate_type` 和解释。
4. `edited_allowed=true` 只表示详情页可以让用户拖动 after 方案；confirm 时仍必须重校验。

#### 6.3.7 候选生成规则

`important_urgent_task` 主路径：

1. 若 `target_completed=true`：生成 `close`。
2. 若 `target_already_scheduled=true`：生成 `close` 或 `notify_only`。
3. 若存在满足容量的 free slot：
   - 生成 `add_task_pool_to_schedule`。
   - 优先使用 deadline 前槽位。
   - 优先使用 memory 偏好匹配槽位，但 memory 只能影响排序，不能覆盖硬冲突。
4. 若没有完整 free slot：
   - 第一轮不生成 `compress_with_next_dynamic_task`。
   - 记录 `capacity_insufficient`，生成 `notify_only / ask_user`，提示用户重新选择时间或缩短任务。
5. 若无候选但信息完整：
   - 生成 `notify_only`，说明无法安全安排。

`unfinished_feedback` 主路径：

1. 若无法定位 feedback target：生成 `ask_user`。
2. 若能定位 schedule_event：
   - 第一版优先生成 `makeup_block`，只新增补做块，不移动原任务。
   - 若补做块挤压后续动态任务，第一轮不生成压缩融合候选，降级为 `ask_user / notify_only`。
3. 若目标属于 task_item 且需要局部补救：
   - 调用“偏好软化版局部粗排”生成 `local_reorder_makeup`。
   - 输入范围为当前时刻到 task_class.end_date。
   - 只传受影响的 item，不重排整张大表。
4. 若 deadline / end_date 已过且无可用窗口：
   - 生成 `ask_delay_end` 或 `notify_only`，不强行安排到无效时间。

#### 6.3.8 合法性校验规则

候选写入 preview 前必须通过后端校验：

1. 用户归属：
   - candidate 中所有 target / affected event 必须属于同一 user。
2. 时间窗：
   - 所有 `to_slot` 必须在 context window 或局部补救窗口内。
   - `to_slot` 必须能映射到正式 `schedules` 原子节次。
3. 冲突：
   - `add / create_makeup` 不得覆盖课程、固定日程、已确认任务。
   - `compress` 第一版不依赖“是否允许压缩”的显式配置项；只允许作用在后端识别出的 `next_dynamic_task` 上，且必须排除课程、固定日程、已锁定任务、已完成任务和无法缩短的任务块。
4. 时长：
   - `duration_sections` 必须等于目标预计节数，或明确记录压缩比例。
   - task_pool 第一版限制 1 到 4 节。
5. 来源：
   - task_pool 候选必须保持 `task_source_type=task_pool`。
   - task_item 候选必须保留 task_class 归属与顺序语义。
6. 局部重排：
   - 只能移动局部补救输入集合内的 item。
   - 不得打乱同一 task_class 内必须保持的前后顺序。
7. 幂等：
   - 同一 context 内候选用 `candidate_type + target_id + normalized_changes_hash` 去重。
8. 可解释性：
   - 候选必须有 `before_summary / after_summary / risk`，否则不能进入 LLM 选择。

校验失败处理：

1. 单个候选失败：丢弃该候选并写入 trace warning。
2. 全部候选失败：decision 降级为 `ask_user / notify_only`。
3. 校验失败不能交给 LLM 自行判断。

#### 6.3.9 候选排序规则

排序因子建议：

```text
score =
  deadline_score
  + capacity_score
  + preference_score
  + minimal_change_score
  + risk_penalty
  + disruption_penalty
```

排序原则：

1. deadline 前候选优先于 deadline 后候选。
2. 不移动已有任务优先于移动 / 压缩已有任务。
3. 满足 memory 偏好的候选优先于不满足偏好的候选。
4. 影响事件数量少的候选优先。
5. 同分时选择更早可执行的槽位。
6. 第一版最多保留 top3 给 LLM。
7. 必须保留一个后端 fallback top1，LLM 受限重试后仍失败时使用。

候选数量：

```text
min = 1
max = 3
```

但 `close / ask_user / notify_only` 场景允许没有可应用候选。

#### 6.3.10 LLM 选择与解释边界

LLM 输入：

```text
context_summary
metrics
issues
decision
candidates(top3)
memory_context_text
```

LLM 输出：

```text
selected_candidate_id
summary
reason
risk_text
notification_summary
```

约束：

1. LLM 不能新增候选。
2. LLM 不能修改 `changes`。
3. LLM 不能把 `ask_user / notify_only / close` 改成 `select_candidate`。
4. LLM 返回的 `selected_candidate_id` 不存在或格式非法时，不立刻采纳 top1，而是进行一次受限重试；重试 prompt 只允许从现有 candidate_id 列表中选择，不能新增候选或修改 changes。
5. 受限重试仍失败时，使用后端 top1 作为推荐候选写入 preview，并记录 `llm_fallback_used=true`；该候选仍需用户确认后才会正式应用。
6. LLM 文案需要做长度和空值校验；失败时使用固定 fallback：
   ```text
   我为你生成了一份日程调整建议，请回到系统确认是否应用。
   ```
7. `notification_summary` 可传给通知模块，但通知模块仍保留自己的模板 fallback。

#### 6.3.11 与 `analyze_health` 的复用和隔离边界

可复用思想：

1. 后端先算 metrics / issues。
2. 后端先做 decision。
3. 候选由后端枚举并校验。
4. 候选需要模拟 after，并保留 before/after 摘要。
5. LLM 只在合法候选里选择，不做开放式搜索。

不直接复用的部分：

1. 不把主动调度做成 `analyze_health` 工具。
2. 不进入 Execute ReAct 循环。
3. 不直接复用 `analyze_health` 的 `move / swap` 候选类型，因为主动调度第一版候选包含 task_pool 加入日程、补做块、通知和询问；压缩融合只作为后续预留候选。
4. 不复用 `ScheduleState` 作为唯一输入；主动调度输入是 `ActiveScheduleContext`，其中包含 trigger、memory、feedback、task_pool facts 和 schedule facts。

建议抽公共层：

1. 时间格和节次合法性。
2. 冲突判断。
3. 局部 before/after 摘要。
4. 候选模拟后的收益 / 风险评分框架。

这些公共能力若已经在 `backend/newAgent/tools/schedule` 中存在，迁移时按并行迁移策略抽出小 helper；不要本轮直接大搬整个 schedule tool 包。

#### 6.3.12 错误处理与可观测

1. observe 失败：trigger 标记 failed，可重试。
2. candidate 生成失败但 context 可解释：decision 降级为 `notify_only / ask_user`。
3. LLM 选择输出非法：先受限重试一次，仍失败再使用后端 fallback candidate。
4. LLM 文案失败：使用固定 fallback 文案。
5. 每个候选保留 `validation.warnings` 和 `score_breakdown`，便于 dry-run 查看为什么它被保留或丢弃。
6. trace 至少记录：
   - metrics 摘要。
   - issue codes。
   - decision action / reason_code。
   - generated_candidate_count。
   - valid_candidate_count。
   - selected_candidate_id。
   - llm_fallback_used。

#### 6.3.13 测试方案

单元测试：

1. target 已完成时 decision 为 `close`。
2. target 已进入日程时不生成重复 `add_task_pool_to_schedule`。
3. feedback 目标未知时 decision 为 `ask_user`。
4. 24 小时窗口内存在 free slot 时生成 `add_task_pool_to_schedule`。
5. memory 偏好匹配槽位排序高于非匹配槽位。
6. 无 free slot 但存在 next dynamic task 时不生成 `compress_with_next_dynamic_task`，decision 降级为 `ask_user / notify_only`。
7. task_pool 候选非法时被校验丢弃。
8. 局部补救候选不得移动输入集合外的 task_item。
9. 候选去重基于 normalized changes hash。
10. LLM 返回非法 candidate_id 时先受限重试一次，重试仍失败再 fallback 到后端 top1。

集成测试：

1. dry-run 返回 metrics / issues / decision / candidates。
2. 正式 trigger 生成合法候选后进入 LLM selection，并输出 selected candidate。
3. LLM 超时、失败或受限重试后仍输出非法时，仍能生成 preview fallback。
4. 与 5.3 context 串联：task_pool 只用 memory 软偏好，task_item 使用 task_class 约束。
5. 与 7.x preview 串联：候选可转换为 preview_changes。

人工验收：

1. 创建一个 24 小时内有空闲节次的紧急 task，dry-run 能看到 1 到 3 个候选。
2. 添加“晚上更适合写作”的 memory 后，晚间槽位排序更靠前或 explanation 说明偏好命中。
3. 制造无空闲但有下一个动态任务的场景，看不到压缩融合候选，并返回 `ask_user / notify_only`。
4. 制造“刚才那个没做完”但无法定位目标的反馈，返回 ask_user，不生成危险候选。
5. 关闭 LLM 或模拟 LLM 两次输出非法，后端仍能用 top1 fallback 生成 preview。

## 7. 模块四：预览、前后对比与确认

### 7.1 业务实现逻辑简述

主动调度候选必须先写入待确认预览，让用户看到“为什么触发、改前是什么、改后是什么、风险是什么、不调整的后果是什么”。

确认粒度按候选项确认，不做整版黑盒确认。确认后才进入正式应用链路。

### 7.2 已拍板结论

1. 预览复用 `agent_schedule_states`，还是新增 `active_schedule_previews`？
   - 已确认：新增 `active_schedule_previews` 承载主动调度预览持久化；不直接塞进 `agent_schedule_states`。展示层可以抽通用 before/after change schema，供现有会话排程预览和主动调度预览复用。
2. 预览是否必须保存 before 快照，还是第一版只保存 change item + 当前状态版本？
   - 已确认：第一版不保存全量 before 快照，保存受影响范围的 `before_summary + preview_changes + base_version`，用于展示改前/改后和确认前安全校验。
3. 回滚第一版是“失败后不落库即可”，还是必须支持已应用后的撤销？
   - 已确认：第一版不开放 apply 成功后的撤销能力；apply 必须事务化，失败不落库，并回写 `apply_status / apply_error`。成功后轻量记录 `applied_event_ids`，为审计和后续撤销能力预留。
4. 用户确认入口走现有 Agent resume 协议，还是新增主动调度确认 API？
   - 已确认：不走 Agent resume。MVP 新增主动调度详情页和确认 API；飞书链接进入详情页。详情页采用助手卡片式体验，展示解释文案和日程对比卡片，支持拖动 after 方案后确认。
5. 预览过期时间设多久？
   - 已确认：MVP 预览过期时间为 1 小时；过期后不可确认应用，只能重新触发生成新的预览。

### 7.3 执行计划：预览、前后对比与确认协议

本模块负责把 6.3 选出的候选持久化成用户可查看、可确认、可审计的主动调度预览。它不负责重新生成候选，也不在用户未确认前修改正式日程。确认 API 第一版同步调用正式应用链路，但“如何把 candidate 转成正式写库请求”的细节放到 8.3。

#### 7.3.1 代码落点

1. 预览领域模型与 DTO：
   ```text
   backend/active_scheduler/preview
   ```
2. 预览 repo：
   ```text
   backend/active_scheduler/repo
   ```
3. API handler：
   ```text
   backend/api/active_schedule.go
   ```
4. 路由注册：
   ```text
   backend/routers
   ```
5. 与前端共享的展示 DTO：
   ```text
   backend/active_scheduler/model
   ```
   若现有会话排程预览也要复用 before/after 展示结构，后续可再抽到更公共的 schedule preview DTO 包；MVP 先不大搬旧链路。

#### 7.3.2 active_schedule_previews 表结构方向

第一版新增 `active_schedule_previews`，不复用 `agent_schedule_states` 和 Redis 会话预览缓存。

建议字段：

```text
preview_id                 # 建议字符串或雪花 ID
user_id
trigger_id
trigger_type
target_type
target_id
status                     # pending / ready / applied / ignored / expired / failed
selected_candidate_id
candidate_count
selected_candidate_json
candidates_json
decision_json
metrics_json
issues_json
context_summary_json
before_summary_json
preview_changes_json
after_summary_json
risk_json
explanation_text
notification_summary
base_version
expires_at
generated_at

apply_id
apply_status               # none / applying / applied / failed / rejected / expired
apply_candidate_id
apply_idempotency_key
apply_request_hash
applied_changes_json
applied_event_ids_json
apply_error
applied_at

trace_id
created_at
updated_at
deleted_at
```

索引建议：

```text
idx_active_previews_user_created_at(user_id, created_at)
idx_active_previews_trigger_id(trigger_id)
idx_active_previews_expires_at(expires_at)
uk_active_previews_apply_idempotency(preview_id, apply_idempotency_key)
```

约束：

1. `preview_id` 是飞书跳转和详情页查询的唯一定位键。
2. `trigger_id` 用于串联 `trigger -> preview -> notification -> apply`。
3. `candidates_json` 保存后端合法候选全集，通常最多 3 个。
4. `selected_candidate_json` 保存 LLM 选择后或 fallback top1 的推荐候选。
5. `before_summary_json / preview_changes_json / after_summary_json` 是详情页展示和 confirm 重校验的核心输入。
6. `base_version` 用于确认时判断预览生成后的正式日程是否发生变化；MVP 可先使用受影响范围的更新时间摘要或 schedule 版本摘要，后续再收敛为正式 version 字段。
7. `apply_*` 字段先放在 preview 表内，MVP 不新增 apply request 表；后续异步化时可平滑迁到 `active_schedule_apply_requests`。

#### 7.3.3 Preview 状态机

预览主状态：

```text
pending -> ready
ready -> applied
ready -> ignored
ready -> expired
ready -> failed
pending -> failed
```

状态语义：

1. `pending`：已准备写入或正在组装预览，不应对用户展示为可确认。
2. `ready`：可查看、可确认，且未过期。
3. `applied`：用户已确认并成功应用。
4. `ignored`：用户明确忽略本次建议。
5. `expired`：超过 `expires_at`，不可确认。
6. `failed`：预览写入、候选转换或 apply 回写失败。

apply 子状态：

```text
none -> applying -> applied
none -> applying -> failed
none -> rejected
none -> expired
```

状态约束：

1. `status=applied` 时，`apply_status` 必须为 `applied`。
2. `status=expired` 时，confirm API 必须拒绝确认，并把 `apply_status` 置为 `expired` 或保持不可应用状态。
3. `status=ignored / applied / expired` 后，默认不允许再次 confirm。
4. 第一版同一个 preview 只允许成功 apply 一次。

#### 7.3.4 Preview 写入流程

worker pipeline 在 `LLMSelectAndExplain` 后写 preview：

```text
1. 接收 observation result、候选列表、selected candidate 和解释文案。
2. 构造 before_summary：
   - 只记录受影响时间窗 / 受影响事件 / 目标任务摘要。
   - 不保存全量日程快照。
3. 构造 preview_changes：
   - 由 selected candidate 的 changes 转换而来。
   - 保留 candidate_id / change_id / target / slot / duration / affected ids。
4. 构造 after_summary：
   - 基于 before_summary + preview_changes 生成用户可读改后视图。
   - 不写正式 schedule 表。
5. 生成 base_version：
   - 记录受影响范围内当前正式日程版本或更新时间摘要。
6. 写入 active_schedule_previews：
   - status=ready
   - apply_status=none
   - expires_at=generated_at + 1h
7. 回写 trigger 状态为 preview_generated。
8. 发布 notification.feishu.requested。
```

失败处理：

1. preview 写入失败：trigger 标记 failed，不发布 notification。
2. selected candidate 缺少可展示字段：preview 不写入，trigger 标记 failed 或降级 notify_only。
3. notification 失败不回滚 preview，通知状态由 `notification_records` 承载。

#### 7.3.5 展示 DTO

详情页响应建议：

```text
ActiveSchedulePreviewDetail
  preview_id
  status
  apply_status
  expires_at
  expired
  trigger
  explanation
  selected_candidate
  candidates
  before
  after
  changes
  risk
  can_confirm
  can_ignore
  trace_id
```

`before / after` 建议使用轻量展示结构：

```text
SchedulePreviewVersion
  title
  window_start
  window_end
  entries
  summary_lines
```

`entries`：

```text
SchedulePreviewEntry
  entry_id
  source_type             # course / schedule_event / task_pool / task_item / virtual
  source_id
  title
  start_at
  end_at
  week
  day_of_week
  section_from
  section_to
  status                  # unchanged / added / moved / compressed / affected / removed
  editable
```

`changes`：

```text
ActiveScheduleChangeItem
  change_id
  change_type             # add / move / compress / create_makeup / ask_user / none
  target_type
  target_id
  from_slot
  to_slot
  duration_sections
  affected_event_ids
  edited_allowed
  metadata
```

展示原则：

1. 前端展示的是后端持久化的 preview，不重新实时计算候选。
2. `expired=true` 时仍可查看解释和 before/after，但不能确认。
3. `editable=true / edited_allowed=true` 只控制前端是否允许拖动 after 方案，不代表后端会信任前端结果。
4. 前端不要从 URL 中传 `candidate_id`；详情页通过 `preview_id` 读取完整数据。

#### 7.3.6 API 设计

新增鉴权接口：

```text
GET  /active-schedule/previews/{preview_id}
POST /active-schedule/previews/{preview_id}/confirm
POST /active-schedule/previews/{preview_id}/ignore
```

飞书和 Web 路由：

```text
/schedule-adjust/{preview_id}
```

页面打开流程：

```text
1. Web 路由解析 preview_id。
2. 前端调用 GET /active-schedule/previews/{preview_id}。
3. 后端校验 preview 属于当前用户。
4. 返回详情 DTO。
5. 前端根据 can_confirm / expired / apply_status 展示确认、忽略或历史状态。
```

`confirm` 请求：

```json
{
  "candidate_id": "cand_1",
  "action": "confirm",
  "edited_changes": [
    {
      "change_id": "chg_1",
      "change_type": "add",
      "target_type": "task_pool",
      "target_id": 123,
      "to_slot": {
        "week": 8,
        "day_of_week": 4,
        "section_from": 8,
        "section_to": 8
      },
      "duration_sections": 1
    }
  ],
  "idempotency_key": "frontend-generated-uuid"
}
```

`confirm` 响应：

```json
{
  "preview_id": "asp_123",
  "apply_id": "asa_456",
  "apply_status": "applied",
  "applied_event_ids": [1001],
  "message": "已应用"
}
```

`ignore` 请求：

```json
{
  "reason": "user_dismissed"
}
```

`ignore` 语义：

1. 只把 preview 标记为 `ignored`。
2. 不修改正式日程。
3. 不影响同一任务后续在新的时间窗再次触发；触发去重仍按 trigger 模块规则处理。

#### 7.3.7 Confirm API 校验流程

确认接口固定流程：

```text
1. 鉴权并读取 preview。
2. 校验 preview.user_id == 当前用户。
3. 校验 status=ready 且 apply_status=none；若命中幂等记录，则按幂等规则返回上一轮结果。
4. 校验 expires_at 未过期。
5. 校验 candidate_id 属于 preview.candidates。
6. 读取 idempotency_key，计算 apply_request_hash。
7. 命中同一 preview_id + idempotency_key：
   - hash 相同：返回上一次 apply 结果。
   - hash 不同：拒绝，提示幂等键复用到不同请求。
8. 生成 apply_id，写 apply_status=applying。
9. 后端重校验 edited_changes：
   - target 归属。
   - slot 合法。
   - 不覆盖课程 / 固定日程 / 已确认任务。
   - base_version 未失效或受影响范围未变化。
10. 调用 8.3 的正式应用服务。
11. 成功：写 applied_changes_json / applied_event_ids_json / applied_at，状态变为 applied。
12. 失败：写 apply_error，apply_status=failed；是否把主状态置为 failed 由失败类型决定，正式写入失败时不允许绕过幂等重复应用。
```

关键约束：

1. 后端必须允许 `edited_changes` 为空；为空时使用候选原始 changes。
2. 后端必须允许 `edited_changes` 与候选原始 changes 不同；不同表示用户拖动了 after 方案，但仍要在同一 candidate 的允许编辑范围内。
3. 后端不能相信前端传来的 title、summary、risk，只信 target 和 slot 等结构化字段。
4. confirm 成功前不得发布 `schedule.apply.succeeded`。
5. confirm 失败不得产生半写正式日程。

#### 7.3.8 幂等与重复提交

幂等键：

```text
unique(preview_id, idempotency_key)
```

请求摘要：

```text
apply_request_hash = hash(candidate_id + action + normalized_edited_changes)
```

规则：

1. 前端每次点击确认生成一个新的 `idempotency_key`。
2. 同一次点击的网络重试必须复用同一个 `idempotency_key`。
3. 同一 key + 同一 hash：返回同一 `apply_id` 和结果。
4. 同一 key + 不同 hash：拒绝。
5. 不同 key + 同 preview：如果 preview 已 applied，拒绝重复应用或返回已应用状态。
6. 第一版不支持一个 preview 成功应用多个候选。

#### 7.3.9 过期与重建

过期规则：

```text
expires_at = generated_at + 1h
```

处理方式：

1. GET 详情时若已过期，返回 `expired=true / can_confirm=false`。
2. confirm 时若已过期，拒绝并更新状态为 `expired`。
3. 过期 preview 仍可查看历史解释。
4. 前端提示用户重新生成建议。
5. 重新生成必须走新的 trigger / dry-run 链路，生成新的 preview_id，不在旧 preview 上覆盖。

#### 7.3.10 与现有 Agent 预览的关系

复用边界：

1. 不复用 `agent_schedule_states`。
2. 不复用 Redis `schedule_preview` key。
3. 可参考 `SchedulePlanPreviewCache / GetSchedulePlanPreviewResponse` 的展示思想，但主动调度使用独立 DTO。
4. 可抽通用 `SchedulePreviewVersion / SchedulePreviewEntry / ActiveScheduleChangeItem` 展示结构，供后续会话排程预览复用。

隔离原因：

1. 主动调度 preview 可能来自后台 worker，没有 `conversation_id`。
2. 主动调度 preview 绑定 `trigger_id / preview_id / expires_at / apply_status`。
3. 会话排程预览是 Agent state 的派生视图，不适合承载后台通知和 apply 审计。

#### 7.3.11 错误处理与可观测

1. preview 不存在：返回 not found，不泄漏是否属于其它用户。
2. preview 不属于当前用户：返回 not found 或 forbidden，产品上建议统一 not found。
3. preview 已过期：GET 可返回详情，confirm 返回业务错误。
4. preview 已 applied：confirm 幂等命中则返回原结果；非幂等重复确认则拒绝。
5. base_version 失效：confirm 失败，不写正式日程，提示重新生成。
6. edited_changes 非法：confirm 失败，写 apply_error。
7. 正式应用服务失败：事务回滚，写 apply_status=failed。
8. 所有 confirm 路径必须能通过 `preview_id / apply_id / idempotency_key / trace_id` 查日志。

#### 7.3.12 测试方案

单元测试：

1. candidate 转 `preview_changes`。
2. before_summary + preview_changes 生成 after_summary。
3. preview 过期判断。
4. `preview_id + idempotency_key` 幂等命中。
5. 同一 idempotency_key 不同 hash 被拒绝。
6. `edited_changes` 越界、冲突、跨用户 target 被拒绝。
7. preview 状态机流转合法性。

集成测试：

1. worker 写入 `active_schedule_previews` 后，GET 详情能读取完整 before/after。
2. 飞书链接 `/schedule-adjust/{preview_id}` 能进入详情页并读取同一 preview。
3. confirm 原始候选成功，状态变为 `applied`。
4. confirm 拖动后的 `edited_changes` 成功，应用内容以 edited changes 为准。
5. preview 过期后 confirm 被拒绝。
6. base_version 改变后 confirm 被拒绝。
7. 用户重复点击确认不会重复写正式日程。

人工验收：

1. 打开主动调度详情页，能看到触发原因、推荐调整、改前 / 改后、风险说明。
2. 拖动 after 方案后确认，后端按拖动后位置应用。
3. 对过期 preview 点击确认，页面提示重新生成。
4. 对已应用 preview 再次点击确认，不产生重复日程块。

## 8. 模块五：正式应用链路

### 8.1 业务实现逻辑简述

主动调度模块不直接写正式日程。用户确认某个候选后，后端把候选转换为现有 service 能理解的正式应用请求。

应用成功后发布 `schedule.apply.succeeded`；失败则发布 `schedule.apply.failed`，并把失败原因写回预览状态。

### 8.2 已拍板结论

1. 从任务池任务加入日程时，正式写入目标是 `schedule_events(type=task, rel_id=tasks.id)`，还是先转为 `task_items`？
   - 已确认：不转为 `task_items`。正式写入 `schedule_events.type=task, task_source_type=task_pool, rel_id=tasks.id`，并写入对应 `schedules` 原子节次。
2. 未完成补救涉及已排任务移动时，是否第一版只支持生成新补做块，不支持直接移动原任务？
   - 已确认：第一版只支持生成新的补做块，不直接移动原已排任务。这样可以降低对既有 schedule / task_item 状态的扰动，后续再扩展移动原任务。
3. `schedule.apply.requested` 第一版是否需要 outbox 异步消费，还是确认接口内同步调用 service？
   - 已确认：MVP 确认接口内同步调用正式应用 service，不新增 outbox apply 消费链路，也不强制新增 apply request 表。
   - 已确认：确认接口负责完成预览读取、过期校验、候选归属校验、`edited_changes` 重校验、事务写库和 apply 结果回写。
   - 已确认：后续若 apply 变重，再迁移为 `active_schedule_apply_requests + schedule.apply.requested` 异步消费；MVP 先通过 preview 表内 apply 字段保留迁移空间。
4. 应用幂等键用 `preview_id + candidate_id`，还是单独生成 `apply_id`？
   - 已确认：使用独立 `apply_id` 表示一次确认应用尝试。
   - 已确认：使用 `idempotency_key` 绑定一次确认请求，推荐唯一约束为 `preview_id + idempotency_key`。
   - 已确认：`preview_id + candidate_id` 只用于定位用户基于哪一个候选确认，不代表最终应用内容；拖动后的最终内容以 `edited_changes` 为准，并必须重新校验。

### 8.3 执行计划：正式应用链路

本模块负责把用户确认后的 `preview_changes / edited_changes` 转成正式日程写入。它必须在事务内完成重校验、写库和结果回写；失败时不能产生半写状态。MVP 确认接口内同步调用本模块，不发布 `schedule.apply.requested` 给异步 worker。

#### 8.3.1 代码落点

1. 正式应用入口：
   ```text
   backend/active_scheduler/apply
   ```
2. 候选 / change 转换器：
   ```text
   backend/active_scheduler/apply/convert
   ```
3. 正式写入 adapter：
   ```text
   backend/active_scheduler/adapters
   ```
4. 复用既有领域 service：
   ```text
   backend/service/task-class.go
   backend/service/schedule.go
   backend/dao/schedule.go
   ```
5. apply 结果回写：
   ```text
   backend/active_scheduler/preview
   ```

建议定义主动调度自己的 apply port：

```go
type ScheduleApplyPort interface {
    ApplyActiveScheduleChanges(ctx context.Context, req ApplyActiveScheduleRequest) (ApplyActiveScheduleResult, error)
}
```

说明：

1. confirm API 只依赖主动调度 apply service，不直接调用 DAO。
2. apply service 内部按 change 类型分流到现有 service 或本地 adapter。
3. 所有正式写库都必须走事务。

#### 8.3.2 Apply 请求与结果

请求结构方向：

```text
ApplyActiveScheduleRequest
  preview_id
  apply_id
  idempotency_key
  user_id
  candidate_id
  base_version
  changes
  requested_at
  trace_id
```

结果结构方向：

```text
ApplyActiveScheduleResult
  apply_id
  apply_status
  applied_event_ids
  applied_schedule_ids
  applied_changes
  skipped_changes
  warning_messages
```

约束：

1. `changes` 来自 `edited_changes`；若前端未编辑，则使用 preview 中的原始 `preview_changes`。
2. `candidate_id` 只用于定位候选来源，不作为幂等键。
3. `base_version` 必须参与重校验，避免预览生成后正式日程已变化。
4. `applied_changes` 必须记录最终真实落库内容，而不是原始候选内容。

#### 8.3.3 支持的 change_type

MVP 正式应用支持：

```text
add_task_pool_to_schedule
create_makeup
```

有条件支持：

```text
add_task_item_to_schedule
local_reorder_makeup
```

预留但第一轮不启用：

```text
compress_with_next_dynamic_task
```

不直接应用：

```text
ask_user
notify_only
close
```

规则：

1. `ask_user / notify_only / close` 只更新 preview 状态或通知结果，不写正式日程。
2. `add_task_item_to_schedule` 仅用于未安排过的 task_item，可复用 `TaskClassService.BatchApplyPlans`。
3. `local_reorder_makeup` 若只包含未安排 task_item 的新增落位，可转成 `BatchApplyPlans`；若包含移动既有已排事件，MVP 不正式应用，应在候选生成阶段过滤或降级为 `ask_user`。
4. `create_makeup` 表示新增一个补做块，不移动原已排任务。
5. `compress_with_next_dynamic_task` 第一轮不生成、不应用；后续打开前必须先完成 8.3.9 的事务落库能力和端到端测试。

#### 8.3.4 候选到正式请求的转换器

转换流程：

```text
1. 读取 preview 中的 selected_candidate / candidates。
2. 校验 confirm candidate_id 存在。
3. 选择 changes：
   - edited_changes 非空：使用 edited_changes。
   - edited_changes 为空：使用 candidate 原始 changes。
4. NormalizeChanges：
   - 排序。
   - 填充缺省 duration。
   - 合并连续节次。
   - 生成 normalized hash。
5. ValidateChangeScope：
   - 不允许新增 preview 中不存在的 target。
   - 不允许越过 candidate 的 edited_allowed 范围。
6. ConvertToApplyRequest：
   - 按 change_type 转换为具体写入命令。
```

转换器输出：

```text
ApplyCommand
  command_type            # insert_task_pool_event / insert_makeup_event / batch_apply_task_items / split_compress_event
  target_type
  target_id
  slots
  source_event_id
  metadata
```

转换器不做 DB 写入，只生成可校验、可事务执行的命令。

#### 8.3.5 重校验规则

正式写入前必须重新读取数据库真值：

1. 预览归属：
   - preview 属于当前 user。
   - preview 未过期。
   - preview 未 applied / ignored / expired。
2. target 归属：
   - task_pool 属于当前 user。
   - task_item 属于当前 user 的 task_class。
   - schedule_event 属于当前 user。
3. target 状态：
   - task_pool 未完成。
   - task_pool 未进入日程。
   - task_item 若走 `BatchApplyPlans`，必须未安排。
   - 补做块允许原任务已安排，但不能更新原任务的 embedded_time。
4. 时间合法：
   - week / day_of_week / section 在合法范围内。
   - 相对时间能转换为绝对时间。
   - 节次数量符合 duration。
5. 冲突合法：
   - 不覆盖课程。
   - 不覆盖固定日程。
   - 不覆盖已确认任务。
   - 若嵌入课程，必须满足课程可嵌入规则。
6. base_version：
   - 受影响范围内 schedule 版本或更新时间摘要未变化。
   - 若变化，拒绝 apply，提示重新生成 preview。

#### 8.3.6 task_pool 正式落库策略

`add_task_pool_to_schedule` 写入：

```text
schedule_events
  user_id = user_id
  name = task.title
  type = task
  task_source_type = task_pool
  rel_id = tasks.id
  start_time = conv.RelativeTimeToRealTime(...)
  end_time = conv.RelativeTimeToRealTime(...)
  can_be_embedded = false

schedules
  event_id = schedule_events.id
  user_id = user_id
  week = week
  day_of_week = day_of_week
  section = each section
  status = normal
  embedded_task_id = null
```

事务步骤：

```text
1. 读取 task，校验 user_id / completed / status。
2. 校验 task 未已有 task_pool schedule_event。
3. 构造 schedules 原子节次。
4. 调用冲突检查。
5. 插入 schedule_events。
6. 回填 event_id 后插入 schedules。
7. 返回 applied_event_ids。
```

说明：

1. 不创建 task_item。
2. 不更新 task_class。
3. 是否把 task 标记为“已安排”需要在 task 表结构阶段决定；MVP 可先通过 `schedule_events.task_source_type=task_pool + rel_id` 判断是否已进入日程。
4. 若后续要在 task 上加 `scheduled_event_id / scheduled_at`，也应在同一事务内更新。

#### 8.3.7 task_item 正式落库策略

`add_task_item_to_schedule` / 可转换的 `local_reorder_makeup`：

1. 若所有 change 都是未安排 task_item 的新增落位：
   - 转成 `model.UserInsertTaskClassItemToScheduleRequestBatch`。
   - 调用 `TaskClassService.BatchApplyPlans(ctx, taskClassID, userID, batch)`。
2. 使用 `BatchApplyPlans` 的条件：
   - 所有 task_item 属于同一个 task_class。
   - task_class mode 为 `auto`。
   - task_item 当前未安排。
   - 不包含移动既有 schedule_event。
   - 不包含 task_pool。
3. 不满足以上条件：
   - 不调用 `BatchApplyPlans`。
   - 若是移动已排任务，MVP 拒绝 apply 或在候选生成阶段不生成该候选。

原因：

1. `BatchApplyPlans` 已经包含 task_class 归属校验、时间范围校验、课程嵌入校验、冲突校验和 task_item embedded_time 更新。
2. 但它会校验 task_item 未安排，因此不能拿它处理“已排任务的补做块”。

#### 8.3.8 补做块正式落库策略

`create_makeup` 用于未完成反馈后的新增补做块。

写入原则：

1. 不移动原 schedule_event。
2. 不更新原 task_item 的 `embedded_time`。
3. 新增一个独立 schedule_event 和对应 schedules。
4. 必须记录它是补做块，避免后续误认为原任务唯一安排。

建议表结构配合：

```text
schedule_events.task_source_type     # task_pool / task_item
schedule_events.makeup_for_event_id  # nullable，指向原未完成 schedule_event
schedule_events.active_preview_id    # nullable，用于审计来源
```

如果 MVP 不想立刻加 `makeup_for_event_id / active_preview_id`，至少必须在 `active_schedule_previews.applied_changes_json` 中记录：

```text
makeup_for_event_id
original_target_type
original_target_id
new_event_id
```

但工程倾向仍是给 `schedule_events` 加轻量来源字段，因为只存在 preview 审计里会让后续日程列表难以解释“这是补做块”。

写入流程：

```text
1. 读取原 schedule_event，校验归属。
2. 判断原 event 来源：
   - task_source_type=task_pool：rel_id 指向 tasks.id。
   - task_source_type=task_item 或空：兼容旧数据，rel_id 指向 task_items.id。
3. 构造新 schedule_event：
   - type=task
   - task_source_type 沿用原来源
   - rel_id 沿用原 target id
   - makeup_for_event_id=原 event id
4. 插入新 event 和 schedules。
5. 返回新 event id。
```

#### 8.3.9 压缩融合正式落库策略（预留）

`compress_with_next_dynamic_task` 第一轮实现关闭，本节只保留后续打开时的落库边界。任何 confirm 可见的候选都必须能正式应用；在该能力完成前，候选生成阶段不得返回压缩融合。

后续打开后的事务步骤：

```text
1. 读取补做目标和 next_dynamic_task 当前真值。
2. 校验 next_dynamic_task 仍是同一个 event，且未完成、未锁定、不是课程。
3. 校验压缩后的两个时间段仍在 preview / edited_changes 允许范围内。
4. 删除或缩短 next_dynamic_task 原 schedules。
5. 写入补做块 schedules。
6. 写入压缩后的 next_dynamic_task schedules。
7. 更新两个 event 的 start_time / end_time。
8. 记录 applied_changes_json，标明 compression_ratio=50/50 或用户编辑后的比例。
```

MVP 保守规则：

1. 只允许压缩动态任务，不允许压缩课程和固定日程。
2. 只允许处理一个后继动态任务，不做多任务链式压缩。
3. 压缩后每个任务至少保留 1 节，否则候选不合法。
4. 若实现成本过高，第一版可在候选生成阶段关闭该候选；不能生成一个 confirm 后无法应用的 preview。

#### 8.3.10 事务与回写

确认 API 中的状态流：

```text
1. preview apply_status=none
2. confirm 获取幂等锁或行锁
3. 写 apply_id / apply_status=applying
4. 执行正式应用事务
5. 成功：
   - preview.status=applied
   - preview.apply_status=applied
   - 写 applied_changes_json / applied_event_ids_json / applied_at
   - 可发布 schedule.apply.succeeded
6. 失败：
   - preview.apply_status=failed
   - 写 apply_error
   - 不写 applied_event_ids
   - 可发布 schedule.apply.failed
```

事务边界：

1. 正式 schedule 写入和 preview apply 回写应尽量在同一个数据库事务中完成。
2. 若 preview 表与 schedule 表未来拆库，MVP 的同步事务需迁移为 apply request + outbox。
3. 事件发布不能早于事务成功；若使用 outbox，应在同一事务内写 outbox。

#### 8.3.11 应用失败分类

失败类型：

```text
expired
idempotency_conflict
base_version_changed
target_not_found
target_completed
target_already_scheduled
slot_conflict
invalid_edited_changes
unsupported_change_type
db_error
```

处理规则：

1. 可预期业务失败：返回明确业务错误，`apply_status=failed/rejected`。
2. DB 或事务失败：`apply_status=failed`，保留 error code 和 trace。
3. 幂等冲突：不进入正式写库。
4. base_version 变化：不进入正式写库，提示重新生成预览。
5. unsupported change type：说明候选生成和 apply 能力不匹配，视为后端 bug，trigger / preview 需可排障。

#### 8.3.12 与事件契约的关系

MVP 不发布 `schedule.apply.requested`。

可发布：

```text
schedule.apply.succeeded
schedule.apply.failed
```

事件 payload 建议包含：

```text
preview_id
apply_id
user_id
trigger_id
candidate_id
applied_event_ids
apply_status
error_code
trace_id
```

说明：

1. succeeded / failed 是结果事件，不是请求事件。
2. 后续异步化时再新增 `schedule.apply.requested`，并把当前 confirm 内同步逻辑迁到 apply worker。
3. 事件 payload 放 `backend/shared/events`，不直接复用 preview DB model。

#### 8.3.13 测试方案

单元测试：

1. `preview_changes` 转 `ApplyCommand`。
2. `edited_changes` 为空时使用候选原始 changes。
3. `edited_changes` 越界时拒绝。
4. task_pool change 转 schedule_event / schedules。
5. task_item change 转 `BatchApplyPlans` 请求。
6. create_makeup 不更新原 task_item embedded_time。
7. compress 后两个任务均至少保留 1 节。
8. apply_request_hash 稳定生成。

集成测试：

1. task_pool 确认成功后写入 `schedule_events(type=task, task_source_type=task_pool, rel_id=tasks.id)`。
2. task_pool 重复确认不会重复写事件。
3. task_item 未安排块通过 `BatchApplyPlans` 成功落库。
4. 已安排 task_item 补做块不调用 `BatchApplyPlans`，而是新增补做 event。
5. slot 冲突时事务回滚，preview 写 `apply_status=failed`。
6. base_version 变化时拒绝 apply。
7. apply 成功后可通过 `applied_event_ids` 查到正式日程。

人工验收：

1. 从详情页确认 task_pool 候选，周视图出现新的任务块。
2. 从详情页确认补做块，原任务不被移动，新补做块出现。
3. 制造冲突后确认，页面显示失败，数据库没有半写 event。
4. 网络重试同一 confirm 请求，只产生一组正式日程。

## 9. 模块六：通知触达与飞书边界

### 9.1 业务实现逻辑简述

飞书第一版只提醒用户回系统确认，不在飞书内应用日程、不标记完成、不做复杂 Agent Chat。

主动调度只发布 `notification.feishu.requested`，通知 handler/provider 负责具体投递。这样后续可以把 notification 拆成独立 Go module。

### 9.2 已拍板结论

1. 第一版飞书通知文案是否只需要固定模板？
   - 已确认：不只用固定模板。既然主动调度链路已经调用 LLM，通知文案优先由 LLM 生成简短 summary。
   - 已确认：固定模板作为 fallback，只有 LLM 生成失败、超时或返回空内容时使用，避免通知链路因为文案生成失败而整体中断。
2. 通知是否必须包含跳转链接？如果包含，Web 端预览详情 URL 规则是什么？
   - 已确认：必须包含跳转链接。
   - 已确认：URL 规则采用 `/schedule-adjust/{preview_id}`，每个主动调度 preview 对应一个唯一调整链接。
3. 通知幂等键是否按 `preview_id`，还是按 `user_id + trigger_type + time_window`？
   - 已确认：按 `user_id + trigger_type + time_window` 聚合去重，不按 `preview_id`。
   - 已确认：MVP 语义是同一用户同一触发类型在同一时间窗口内只推一次飞书，避免短时间重复打扰；具体 time_window 长度在表结构与状态机阶段细化。
4. 飞书 provider 第一版放在 backend worker 内，是否需要同步预留 `notification_records` 表？
   - 已确认：需要落 `notification_records` 表。
   - 已确认：飞书 provider 属于不可靠外部服务调用，必须保留可观测、可重试、可排障的投递记录，而不是只写日志。

### 9.3 执行计划：通知触达与飞书边界

本模块负责把主动调度预览转成“可观测、可重试、可去重”的飞书提醒。主动调度只发布 `notification.feishu.requested`，不直接调用飞书 provider；notification handler 负责落 `notification_records`、生成/兜底文案、调用 provider、记录结果和安排重试。

#### 9.3.1 代码落点

1. 事件契约：
   ```text
   backend/shared/events/notification.go
   ```
   只放事件类型、版本、payload DTO、基础校验和消息键构造。
2. notification 模块：
   ```text
   backend/notification
   ```
   放 service、provider interface、record repo、重试策略。
3. outbox handler：
   ```text
   backend/service/events/notification_feishu_requested.go
   ```
   负责注册并消费 `notification.feishu.requested`。
4. 飞书 provider：
   ```text
   backend/notification/providers/feishu
   ```
5. mock provider：
   ```text
   backend/notification/providers/mock
   ```
   用于本地联调和自动化测试，避免真实打扰用户。
6. 配置加载：
   ```text
   backend/config.example.yaml
   backend/cmd/start.go
   ```
   注入 notification service 和 provider。

#### 9.3.2 事件契约

事件名：

```text
notification.feishu.requested
```

版本：

```text
event_version = 1
```

payload：

```text
FeishuNotificationRequested
  notification_id          # 可为空；若发布前已创建 record，则携带
  user_id
  trigger_id
  preview_id
  trigger_type
  target_type
  target_id
  dedupe_key
  target_url               # /schedule-adjust/{preview_id}
  summary_text             # LLM 已生成摘要，可为空
  fallback_text
  trace_id
  requested_at
```

消息键：

```text
message_key = user_id
aggregate_id = preview_id
```

校验规则：

1. `user_id / preview_id / target_url / dedupe_key` 必填。
2. `target_url` 必须是站内相对路径，例如 `/schedule-adjust/{preview_id}`，不允许 provider payload 携带任意外部跳转链接。
3. `summary_text` 可为空；为空时 handler 使用 fallback 文案。
4. payload 不直接复用 `active_schedule_previews` DB model。

#### 9.3.3 notification_records 表结构方向

建议新增 `notification_records`：

```text
id
channel                    # feishu
user_id
trigger_id
preview_id
trigger_type
target_type
target_id
dedupe_key
target_url
summary_text
fallback_text
fallback_used
status                     # pending / sending / sent / failed / dead / skipped
attempt_count
max_attempts
next_retry_at
last_error_code
last_error
provider_message_id
provider_request_json
provider_response_json
sent_at
trace_id
created_at
updated_at
deleted_at
```

索引建议：

```text
uk_notification_dedupe(channel, dedupe_key)
idx_notification_status_retry(status, next_retry_at)
idx_notification_preview(preview_id)
idx_notification_user_created(user_id, created_at)
```

状态语义：

1. `pending`：记录已创建，等待投递。
2. `sending`：当前 worker 正在调用 provider。
3. `sent`：provider 明确返回成功。
4. `failed`：本次投递失败，但仍可重试。
5. `dead`：达到最大重试次数或不可恢复错误，不再自动重试。
6. `skipped`：命中去重或配置关闭，本次不投递。

#### 9.3.4 Provider 接口

notification 模块只依赖 provider interface：

```go
type Provider interface {
    Send(ctx context.Context, req SendRequest) (SendResult, error)
}

type SendRequest struct {
    UserID      int
    OpenID      string
    TargetURL   string
    Title       string
    Text        string
    TraceID     string
}

type SendResult struct {
    ProviderMessageID string
    RawResponse       []byte
    Retryable         bool
}
```

职责边界：

1. provider 只负责和飞书通信。
2. provider 不做 dedupe。
3. provider 不读取 preview。
4. provider 不决定是否通知。
5. provider 返回错误分类，notification service 决定 retry / dead。

MVP provider：

1. `mock`：打印日志或写入 record，不发真实飞书。
2. `feishu`：通过配置的 webhook / app token / open_id 发送卡片或文本。
3. 若用户缺少飞书 open_id：记录 `failed` 或 `dead`，错误码为 `recipient_missing`。

#### 9.3.5 飞书配置项

建议配置：

```yaml
notification:
  enabled: true
  provider: mock # mock / feishu
  baseURL: "https://your-web-domain.example.com"
  dedupeWindow: 30m
  maxRetry: 5
  retryBaseDelay: 30s
  retryMaxDelay: 30m
  feishu:
    enabled: false
    webhookURL: ""
    appID: ""
    appSecret: ""
```

说明：

1. `baseURL` 用于把 `/schedule-adjust/{preview_id}` 拼成飞书可点击链接。
2. 本地和测试环境默认 `provider=mock`。
3. `notification.enabled=false` 时不调用 provider，但仍可按需要写 `skipped` record 便于验证链路。
4. `dedupeWindow` 默认可先与 `important_urgent_task` 的 30 分钟触发去重窗口保持一致。

#### 9.3.6 文案生成与 fallback

文案来源优先级：

```text
1. payload.summary_text
2. preview.notification_summary
3. 后端固定 fallback_text
```

固定 fallback：

```text
我为你生成了一份日程调整建议，请回到系统确认是否应用。
```

校验规则：

1. summary 为空：使用 fallback。
2. summary 过长：截断或使用 fallback，避免飞书卡片超限。
3. summary 包含不允许的链接：去除链接或使用 fallback。
4. LLM summary 失败不能阻断通知投递。
5. `fallback_used=true` 必须记录到 `notification_records`，方便排查 LLM 文案质量。

#### 9.3.7 通知处理流程

handler 消费 `notification.feishu.requested`：

```text
1. 解析 shared/events payload。
2. 校验 user_id / preview_id / target_url / dedupe_key。
3. 按 channel + dedupe_key 查询 notification_records。
4. 若已有 pending / sending / sent：
   - 标记当前 outbox consumed。
   - 不重复创建记录，不重复发飞书。
5. 若已有 failed：
   - 复用旧 record 进入重试流程，不新建重复通知。
6. 若不存在 record：
   - 创建 pending 记录。
7. 读取用户飞书身份或 webhook 目标。
8. 生成最终文案。
9. 将 record 标记 sending，递增 attempt_count。
10. 调用 provider.Send。
11. 成功：status=sent，写 provider_message_id / response / sent_at。
12. 可重试失败：status=failed，写 last_error / next_retry_at。
13. 不可恢复失败：status=dead，写 last_error。
```

outbox 语义：

1. handler 业务处理成功后才把 outbox 标记 consumed。
2. 对 provider 临时失败，可选择：
   - 让 outbox 重试整个 handler。
   - 或 handler 自己写 `notification_records.next_retry_at` 后 consumed，由 notification retry scanner 处理。
3. MVP 建议采用“record 自己管理 provider 重试，outbox 只保证 notification request 被接收一次”的模式，避免 provider 慢失败阻塞通用 outbox 消费。

#### 9.3.8 provider 重试扫描器

新增 notification retry worker：

```text
1. 扫描 status=failed 且 next_retry_at <= now 的 notification_records。
2. 加行锁或状态 CAS，改为 sending。
3. 再次调用 provider。
4. 成功则 sent。
5. 失败则根据 attempt_count / max_attempts 决定 failed 或 dead。
```

退避策略：

```text
next_retry_at = now + min(retryBaseDelay * 2^(attempt_count-1), retryMaxDelay)
```

不可重试错误：

```text
recipient_missing
invalid_url
provider_auth_failed
payload_invalid
```

可重试错误：

```text
provider_timeout
provider_rate_limited
provider_5xx
network_error
```

#### 9.3.9 幂等与去重

通知 dedupe key：

```text
user_id + trigger_type + time_window
```

MVP 窗口：

```text
time_window = floor(requested_at / 30m)
```

规则：

1. 同一 `channel + dedupe_key` 同一时间只允许一条有效 notification record。
2. 如果同一 dedupe key 已 sent，不再发送。
3. 如果同一 dedupe key 已 pending / sending，不再创建。
4. 如果同一 dedupe key failed，进入重试，不创建第二条。
5. preview_id 不参与 dedupe 主键，但 record 仍保存 preview_id，用于知道最终跳转到哪份预览。

注意：

1. 如果同一窗口多个 preview 命中同一 dedupe_key，MVP 先以减少打扰为优先，只保留第一条通知。
2. 后续如需“聚合多条 preview”，可在 record 中增加 `related_preview_ids_json`，但不作为第一版范围。

#### 9.3.10 与主动调度的边界

active_scheduler 负责：

1. 决定是否需要通知。
2. 生成 preview。
3. 生成 `notification.feishu.requested` payload。
4. 发布 outbox 事件。

notification 负责：

1. dedupe。
2. 落 `notification_records`。
3. 文案 fallback。
4. provider 调用。
5. provider retry。
6. provider 结果观测。

notification 不负责：

1. 生成调度候选。
2. 修改 preview。
3. 应用日程。
4. 判断任务是否紧急。

#### 9.3.11 启动与注册

接入点：

1. `cmd/start.go` 初始化 notification service。
2. `RegisterCoreOutboxHandlers` 增加 `RegisterFeishuNotificationRequestedHandler`。
3. `worker` 和 `all` 模式启动 notification retry scanner。
4. `api` 模式只允许发布 outbox，不启动 provider 消费和 retry scanner。

依赖注入：

```text
notification service
  -> notification repo
  -> provider(mock/feishu)
  -> user contact reader
  -> config
```

若第一版暂时没有用户飞书身份表：

1. provider 先支持 webhook 模式，用测试群 webhook 完成链路验证。
2. `user contact reader` 预留接口，后续再接 user profile / feishu binding。

#### 9.3.12 迁出边界

后续迁出独立 notification 服务时保留：

```text
backend/shared/events/notification.go
notification_records schema
Provider 接口语义
dedupe_key 规则
```

迁移方式：

1. active_scheduler 继续只发布 `notification.feishu.requested`。
2. notification 服务独立消费同一事件。
3. 原 backend worker 停止注册 notification handler。
4. `notification_records` 可按数据所有权迁出，或先保留在同库读写。

不能迁出的内容：

1. active_scheduler 内部候选结构。
2. preview DB model 的完整字段。
3. 飞书 provider SDK 细节。

#### 9.3.13 错误处理与可观测

必须记录：

```text
notification_id
dedupe_key
preview_id
trigger_id
channel
status
attempt_count
last_error_code
last_error
provider_message_id
trace_id
```

日志要求：

1. 每次 provider 调用记录 `notification_id / preview_id / attempt_count / trace_id`。
2. provider response 不直接打印敏感 token。
3. dead 状态必须有明确 error_code。
4. dedupe 命中不视为错误，但要记录 debug / info 日志。

指标建议：

```text
notification_requested_total
notification_sent_total
notification_failed_total
notification_dead_total
notification_dedupe_hit_total
notification_fallback_used_total
notification_provider_latency_ms
```

#### 9.3.14 测试方案

单元测试：

1. `notification.feishu.requested` payload validate。
2. dedupe key 生成。
3. summary 为空时使用 fallback。
4. summary 过长时截断或 fallback。
5. provider 可重试错误计算 `next_retry_at`。
6. provider 不可重试错误进入 dead。
7. 同一 `channel + dedupe_key` 不重复创建 record。

集成测试：

1. preview 生成后发布 `notification.feishu.requested`。
2. handler 消费事件后写 `notification_records`。
3. mock provider 成功后 record 变为 sent。
4. mock provider 临时失败后 record 变为 failed，并写 next_retry_at。
5. retry scanner 再次投递成功后 record 变为 sent。
6. 重复消费同一 outbox 不重复发通知。
7. `notification.enabled=false` 时生成 skipped 或不调用 provider，链路可观测。

人工验收：

1. 使用 mock provider 验证 dry-run 不发通知、正式 trigger 发通知记录。
2. 使用测试飞书 webhook 收到包含 `/schedule-adjust/{preview_id}` 的消息。
3. 模拟 provider 失败后能看到 failed / retry / sent 状态变化。
4. 30 分钟窗口内重复触发，不重复收到飞书。

## 10. 模块七：与微服务迁移的协作边界

### 10.1 业务实现逻辑简述

第二阶段开发必须避免阻塞微服务迁移。当前策略是：先在 `backend` 内按服务边界写清楚，等协议稳定后再迁出独立 module。

`api / worker / all` 启动边界第一阶段已经完成。当前剩余工作不是继续拆启动入口，而是在既有 worker / API 边界上接入主动调度、notification 和 schedule apply。

API、worker、active scheduler、notification、schedule apply 的职责边界仍必须从第一版就分清。

### 10.2 已拍板结论

1. 是否先完成 `api / worker / all` 启动边界拆分，再合入主动调度主链路？
   - 已确认：当前已完成第一阶段启动边界拆分，存在 `api / worker / all` 三种启动入口。
   - 已确认：`api` 模式只启动 Gin 和同步 service / DAO 依赖，不启动后台 worker；`worker` 模式只启动 outbox、Kafka consumer、事件 handler、memory worker，不注册 Gin 路由；`all` 模式保留迁移期单体兼容行为。
   - 已确认：主动调度 MVP 可以直接挂到 worker / 事件链路，不需要再等待启动边界拆分。
   - 说明：这里完成的是运行生命周期边界，不是完整微服务拆分；独立 Go module、独立部署配置和数据所有权拆分后续再做。
2. 主动调度代码第一版放在 `backend/service/active_scheduler`，还是 `backend/active_scheduler`？
   - 已确认：第一版不放 `backend/service/active_scheduler`，避免继续并入旧 service 单体。
   - 已确认：第一版放 `backend/active_scheduler`，按未来独立 active-scheduler 服务组织目录、DTO、状态机、pipeline 和 handler。
   - 已确认：MVP 暂不拆成独立 Go module / 独立进程，仍复用当前 `backend` 的启动、DAO、outbox、LLM 初始化和事务能力。
   - 已确认：等事件契约、表结构、预览 / apply 协议稳定后，再按并行迁移策略迁出独立 active-scheduler module。
3. 事件契约是否提前放入 `backend/shared/events` 风格目录，即使当前还未多 module？
   - 已确认：提前放入 `backend/shared/events`。
   - 已确认：该目录只承载跨模块事件协议，包括 event type、event version、payload DTO、基础校验和少量 normalize。
   - 已确认：该目录不放 DAO、service、handler、provider、LLM prompt、复杂业务判断，避免 shared 目录变成共享业务层。
   - 已确认：主动调度、notification、worker handler、API 依赖 `backend/shared/events`，而不是互相依赖业务包。
   - 已确认：后续微服务切流时，`backend/shared/events` 可迁出为独立 contracts module。
4. 第一版是否允许主动调度 service 直接依赖 DAO，还是通过现有 service 读取？
   - 已确认：不允许主动调度主链路散落依赖其它领域 DAO。
   - 已确认：采用 port / adapter 方式组织依赖。`backend/active_scheduler` 内定义读取事实和正式应用所需的接口，MVP adapter 可复用现有 service；若现有 service 缺少合适读模型，允许 adapter 内部调用 DAO 组装，但不能把 DAO 泄漏到主动调度 pipeline。
   - 已确认：主动调度自有表使用 `backend/active_scheduler` 自己的 repo / DAO。
   - 已确认：正式写入 schedule / task_class 必须走现有领域 service 或明确的 apply port，不能在主动调度里绕过既有写入链路。
   - 已确认：notification provider 不归 active_scheduler 管；主动调度只发布 `notification.feishu.requested`。

### 10.3 执行计划：迁移协作边界与装配方案

本模块负责把主动调度、notification、API、worker、正式应用链路的代码边界和启动边界固定下来。第一版仍在 `backend` 单体内实现，但目录、事件契约、port / adapter 和启动装配必须按未来独立服务来组织，避免 MVP 写成新的大单体。

#### 10.3.1 目录总览

建议目录：

```text
backend/
  active_scheduler/
    trigger/
    context/
    observe/
    candidate/
    selection/
    preview/
    apply/
      convert/
    job/
    ports/
    adapters/
    repo/
    model/
    timegrid/
    scheduleutil/

  notification/
    service/
    repo/
    model/
    providers/
      mock/
      feishu/
    retry/

  shared/
    events/
      active_schedule.go
      notification.go
      schedule_apply.go

  service/
    events/
      active_schedule_triggered.go
      notification_feishu_requested.go
      schedule_apply_result.go

  api/
    active_schedule.go
```

目录职责：

1. `backend/active_scheduler`：主动调度业务闭环，拥有 job / trigger / preview 自有表。
2. `backend/notification`：通知投递业务，拥有 `notification_records`。
3. `backend/shared/events`：跨模块事件契约，只放 DTO / event type / version / validate。
4. `backend/service/events`：当前单体 worker 的 outbox handler 注册和消费实现。
5. `backend/api/active_schedule.go`：HTTP 入站，负责鉴权、绑定请求、调用 active_scheduler service。

禁止事项：

1. 不把主动调度放进 `backend/service/active_scheduler`。
2. 不把 notification provider 放进 active_scheduler。
3. 不在 `shared/events` 放 DAO、service、provider、LLM prompt。
4. 不让 active_scheduler 主链路直接 import 其它领域 DAO。

#### 10.3.2 active_scheduler 内部分层

推荐主链路：

```text
trigger
  -> context
  -> observe
  -> candidate
  -> selection
  -> preview
  -> notification event
```

各层职责：

1. `trigger`：统一 dry-run / API trigger / worker due job / unfinished feedback 入口，处理去重和 trigger 状态。
2. `context`：构造 `ActiveScheduleContext`，读取事实快照。
3. `observe`：生成 metrics / issues / decision。
4. `candidate`：生成并校验候选。
5. `selection`：调用 LLM 做候选选择和解释，失败时受限重试，再 fallback。
6. `preview`：写 `active_schedule_previews`，提供详情查询、confirm 状态回写。
7. `apply`：确认后同步调用正式应用链路。
8. `job`：扫描 `active_schedule_jobs` 到期任务并发布 trigger。
9. `ports`：定义 `TaskReader / ScheduleReader / MemoryContextReader / TaskClassReader / ScheduleApplyPort / NotificationPublisher`。
10. `adapters`：把 ports 接到当前单体里的 service / DAO / memory / outbox。

#### 10.3.3 notification 内部分层

推荐主链路：

```text
notification.feishu.requested
  -> service
  -> record repo
  -> provider
  -> retry scanner
```

职责：

1. `service`：处理 dedupe、文案 fallback、provider 调用、状态流转。
2. `repo`：管理 `notification_records`。
3. `providers/mock`：本地测试，不发真实飞书。
4. `providers/feishu`：飞书 webhook / app 调用。
5. `retry`：扫描 failed 记录，按退避策略重试。

notification 不读取 active_scheduler 内部 model，只消费 `shared/events.NotificationFeishuRequested` 和必要的 preview 查询接口。

#### 10.3.4 依赖注入关系

`cmd/start.go` 的 `buildRuntime` 继续作为单体装配入口。

建议新增 runtime 字段：

```text
activeSchedulerService
activeSchedulerJobRunner
notificationService
notificationRetryRunner
activeScheduleHandler
notificationProvider
```

装配顺序：

```text
1. 初始化 config / db / redis / aiHub / rag / memory。
2. 初始化 DAO / RepoManager / outboxRepo / eventBus。
3. 初始化现有 user / task / schedule / taskClass / agent service。
4. 初始化 active_scheduler repo。
5. 初始化 active_scheduler adapters：
   - TaskReader -> task service / task DAO adapter
   - ScheduleReader -> schedule service / schedule DAO adapter
   - MemoryContextReader -> memory.Retrieve + 公共渲染 helper
   - TaskClassReader -> taskClass service / DAO adapter
   - ScheduleApplyPort -> schedule / taskClass apply adapter
   - NotificationPublisher -> outbox event publisher
6. 初始化 active_scheduler service / job runner。
7. 初始化 notification repo / provider / service / retry runner。
8. 初始化 API handlers。
```

依赖方向：

```text
api -> active_scheduler service
worker handler -> active_scheduler service
active_scheduler -> ports
ports adapter -> existing service / DAO / memory / outbox
notification handler -> notification service
notification service -> provider
```

不允许：

```text
notification -> active_scheduler internal candidate model
active_scheduler observe/candidate -> dao.ScheduleDAO
api handler -> dao.ActiveSchedulePreviewDAO
shared/events -> active_scheduler repo
```

#### 10.3.5 API 接入装配点

新增 handler：

```text
api.NewActiveScheduleHandler(activeSchedulerService)
```

`ApiHandlers` 增加：

```text
ActiveScheduleHandler *ActiveScheduleHandler
```

路由：

```text
POST /active-schedule/dry-run
POST /active-schedule/trigger
GET  /active-schedule/previews/{preview_id}
POST /active-schedule/previews/{preview_id}/confirm
POST /active-schedule/previews/{preview_id}/ignore
```

API 模式职责：

1. 可以调用 dry-run。
2. 可以写 trigger 和 outbox。
3. 可以查询 preview。
4. 可以同步 confirm apply。
5. 不启动 due job scanner。
6. 不消费 outbox。
7. 不启动 notification retry scanner。

#### 10.3.6 Worker 接入装配点

`RegisterCoreOutboxHandlers` 增加：

```text
RegisterActiveScheduleTriggeredHandler(...)
RegisterFeishuNotificationRequestedHandler(...)
```

worker 模式启动：

```text
1. eventBus.Start(ctx)
2. memoryModule.StartWorker(ctx)
3. activeSchedulerJobRunner.Start(ctx)
4. notificationRetryRunner.Start(ctx)
```

worker handler 职责：

1. `active_schedule.triggered`：
   - 解析 shared event。
   - 幂等检查 trigger。
   - 调用 active_scheduler pipeline。
   - 写 preview。
   - 发布 notification event。
2. `notification.feishu.requested`：
   - 写 / 查 notification record。
   - 调 provider。
   - 记录 sent / failed / dead。

注意：

1. worker 不注册 Gin 路由。
2. worker 不处理用户 confirm HTTP 请求。
3. confirm 是 API 强交互动作，MVP 同步执行。

#### 10.3.7 all 模式接入

`all` 模式仍是迁移期兼容入口：

```text
StartAll:
  buildRuntime
  startWorkers
  startHTTP
```

要求：

1. 行为等于 API + worker 同进程。
2. 本地开发可优先使用 all 跑全链路。
3. 生产逐步切到 api / worker 分进程。
4. 不能在 all 模式写专属业务逻辑。

#### 10.3.8 配置项

建议新增：

```yaml
activeScheduler:
  enabled: true
  jobScanInterval: 30s
  jobScanBatch: 100
  triggerDedupeWindow: 30m
  previewTTL: 1h
  llmSelectionRetry: 1
  dryRunAllowMockNow: true

notification:
  enabled: true
  provider: mock
  baseURL: "https://your-web-domain.example.com"
  dedupeWindow: 30m
  maxRetry: 5
  retryBaseDelay: 30s
  retryMaxDelay: 30m
  feishu:
    enabled: false
    webhookURL: ""
    appID: ""
    appSecret: ""
```

配置规则：

1. `activeScheduler.enabled=false` 时不启动 job scanner，不消费主动调度事件；API 可返回功能关闭。
2. `notification.enabled=false` 时不调用 provider，可写 skipped record。
3. `provider=mock` 是本地默认。
4. `previewTTL` 与 7.3 保持一致，默认 1 小时。
5. `llmSelectionRetry` 默认 1，对齐 6.3 的受限重试。

#### 10.3.9 数据所有权

active_scheduler 拥有：

```text
active_schedule_jobs
active_schedule_triggers
active_schedule_previews
```

notification 拥有：

```text
notification_records
```

schedule 域拥有：

```text
schedule_events
schedules
task_items.embedded_time
```

task 域拥有：

```text
tasks
```

规则：

1. active_scheduler 可以写自己的表。
2. active_scheduler 读取 task / schedule / task_class 事实必须走 port。
3. active_scheduler 正式写 schedule 必须走 apply port。
4. notification 只写 notification_records，不写 preview / schedule。
5. preview 表可以记录 `applied_event_ids`，但不拥有这些 event。

#### 10.3.10 未来迁出 active-scheduler 的文件边界

未来可整体迁出的目录：

```text
backend/active_scheduler
backend/shared/events/active_schedule.go
backend/shared/events/schedule_apply.go
```

迁出时需要替换的 adapter：

```text
TaskReader local adapter        -> task service RPC / HTTP adapter
ScheduleReader local adapter    -> schedule service RPC / read model adapter
TaskClassReader local adapter   -> task-class service RPC adapter
MemoryContextReader adapter     -> memory service RPC adapter
ScheduleApplyPort local adapter -> schedule apply RPC / event adapter
NotificationPublisher adapter   -> Kafka producer / outbox adapter
```

不应迁出的内容：

```text
backend/api/active_schedule.go
backend/service/events/active_schedule_triggered.go
backend/notification
backend/service/task-class.go
backend/service/schedule.go
```

说明：

1. API handler 属于当前 backend API 入口，未来可改成调用 active-scheduler 服务。
2. outbox handler 是当前单体 worker 的接线层，未来独立服务会自己消费事件。
3. notification 是独立服务边界，不随 active-scheduler 迁出。
4. schedule / task / task-class 领域 service 不随 active-scheduler 迁出。

#### 10.3.11 未来迁出 notification 的文件边界

未来可整体迁出的目录：

```text
backend/notification
backend/shared/events/notification.go
```

当前单体内保留 / 替换：

```text
backend/service/events/notification_feishu_requested.go
```

迁出步骤：

1. 独立 notification 服务消费 `notification.feishu.requested`。
2. backend worker 停止注册 `RegisterFeishuNotificationRequestedHandler`。
3. active_scheduler 继续发布同一个事件。
4. `notification_records` 按数据所有权迁出，或迁移期继续同库。

#### 10.3.12 并行迁移策略

遵循并行迁移：

```text
1. 新目录先落地。
2. 旧 service / DAO 保持不动。
3. adapter 调现有能力。
4. 跑通 API dry-run / trigger / worker / preview / confirm / notification。
5. 协议稳定后再切 module / 服务边界。
6. 最后清理旧兼容代码。
```

本轮不做：

1. 不拆独立 Go module。
2. 不新增独立部署配置。
3. 不把 schedule / task 远程化。
4. 不重命名大范围旧目录。
5. 不删除现有 Agent 排程预览能力。

#### 10.3.13 验收 checklist

| 动作 | 预期 |
| --- | --- |
| `api` 模式启动 | 注册主动调度 API，不启动 worker / job scanner / notification retry |
| `worker` 模式启动 | 不占用 HTTP 端口，注册主动调度和 notification outbox handler |
| `all` 模式启动 | API + worker 同进程跑通全链路 |
| API dry-run | 不写 trigger / preview / notification |
| API trigger | 写 trigger 并发布 `active_schedule.triggered` |
| worker 消费 trigger | 生成 preview 并发布 `notification.feishu.requested` |
| notification handler 消费事件 | 写 notification_records 并调用 mock / feishu provider |
| confirm API | 同步 apply，并回写 preview apply 状态 |
| 关闭 notification.enabled | 不调用 provider，但链路可观测 |
| 关闭 activeScheduler.enabled | 不启动主动调度后台能力，API 返回功能关闭或明确错误 |

#### 10.3.14 风险控制

1. 若 adapter 需要直接调 DAO，必须只出现在 `backend/active_scheduler/adapters`，并返回主动调度自己的 facts DTO。
2. 若发现同一公共能力第三次复制，优先抽公共 helper。
3. 若要修改 `schedule_events / schedules / task_items` 结构，必须配合迁移 SQL 和兼容旧数据。
4. 若 notification provider 未配置，默认 mock，不阻断主动调度 preview。
5. 若 outbox 未启用，正式 trigger 应返回明确错误或降级为同步 dry-run，不假装已通知。
6. 新增 Eino / LLM 能力前必须按项目规则先查官方文档；本节只定义边界，不直接编码。

## 11. 实施顺序

本章作为开工时的短施工单，详细阶段计划见 0.1。

1. 先做迁移 SQL、model、repo、shared events，保证后续模块有稳定契约。
2. 再做 active_scheduler dry-run：context / observe / candidate，不写 preview、不发通知。
3. 再做 preview 查询与写入，跑通正式 trigger 后生成待确认预览。
4. 再做 confirm apply，同步重校验并事务写正式日程。
5. 再做 notification mock / webhook 和 retry。
6. 最后接 due job scanner、worker handler、端到端验收。
7. 第一轮不打开压缩融合；主链路稳定后再单独评估该候选。

## 12. 本轮决策记录

本章保留本轮已经拍板的实施结论，作为编码时遇到细节分歧的裁决依据。

### 12.1 触发 job 机制

1. `task` 创建或更新时，若存在 `urgency_threshold_at`，则 upsert 一条对应的主动调度 job。
2. job 的触发时间统一取 `urgency_threshold_at`；主动调度不再自行维护 `deadline_at - X` 之类的额外阈值。
3. `task` 完成后，不物理删除 job，而是将仍未执行的 job 标记为 `canceled`，方便后续排查为什么没有触发。
4. `task` 更新 `deadline_at` 或 `urgency_threshold_at` 时，直接覆盖当前有效 job，并刷新 `updated_at`。
5. schedule 动态任务默认不写定时 job；计划时间过去后按 `assumed_completed` 推进，只有用户明确反馈未完成时才进入主动调度链路。

### 12.2 最终实施拍板

1. 主动调度相关表和状态机按 4.3 / 7.3 / 9.3 / 10.3 执行。
2. `tasks` 本轮新增 `estimated_sections`，默认 1，MVP 允许 1~4。
3. `schedule_events` 本轮新增 `task_source_type / makeup_for_event_id / active_preview_id`。
4. `compress_with_next_dynamic_task` 第一轮关闭，不生成候选。
5. 飞书第一轮使用 mock / webhook，不依赖用户 open_id 绑定。
6. notification 去重窗口第一轮为 30 分钟。

### 12.3 API 触发、mock_now 与去重

1. API 侧同时提供 `dry-run` 与 `trigger` 两类测试入口：
   - `dry-run`：同步执行主动观测并直接返回诊断和候选；不写预览、不发布飞书通知，主要用于开发调试和验收。
   - `trigger`：进入正式主动调度链路；写入预览，并发布 `notification.feishu.requested`。
2. `mock_now` 只允许 API dry-run / 测试 trigger 使用，用于模拟未来或历史时刻；后台 worker 正式定时触发必须使用真实 `time.Now()`。
3. 使用 `mock_now` 的触发应在 trace / payload 中标记 `is_mock_time=true`，避免排障时把测试触发误认为真实后台触发。
4. `important_urgent_task` 触发按 `user_id + trigger_type + target_task_id` 做 30 分钟去重，避免重复生成预览和重复飞书打扰。
5. `unfinished_feedback` 触发按用户反馈的 `feedback_id / idempotency_key` 做请求幂等；不做固定时间窗强去重，避免用户连续反馈未完成时被错误吞掉。

### 12.4 上下文构造与偏好来源

1. 滚动 24 小时窗口需要映射到现有 `week / day_of_week / section` 坐标，正式应用时仍按现有 schedule 口径同时维护绝对时间与相对时间。
2. 第一版候选以 1 节为最小粒度，任务预计长度限定为 1~4 节。
3. 后续在 task 创建阶段增加预计节数字段时，可由 AI 根据任务复杂度写入该值；主动调度只消费该字段，不在调度阶段重新发明复杂度判断。
4. 偏好来源按目标类型分流：
   - task 池任务：使用 memory 注入的用户偏好。
   - task_item：使用所属 task_class 的硬性偏好和约束。
5. `用户反馈` 在本文档中指显式调度触发信号，不是普通聊天上下文。第一版重点支持 `unfinished_feedback`，即用户明确反馈某个已排动态任务未完成。
6. 调度触发信号持久化为后端链路状态，不直接展示给前端。建议使用类似 `active_schedule_triggers` 的结构承载 `trigger_type / target_type / target_id / idempotency_key / payload_json / status`。

### 12.5 task 池任务进入 schedule 的 schema 分叉

已确认采用方案 A：

1. 在 `schedule_events` 上新增任务来源列：`task_source_type`。
2. `schedule_events.type` 继续表示日程展示与占用类型，保持现有 `course / task` 语义。
3. 当 `type = task` 时，`task_source_type` 表示任务来源：
   - `task_item`：`rel_id` 指向 `task_items.id`。
   - `task_pool`：`rel_id` 指向 `tasks.id`。
4. 原有动态任务块继续使用 `type = task, task_source_type = task_item`。
5. 四象限任务进入日程后使用 `type = task, task_source_type = task_pool`，不创建孤儿 `task_item`。
6. 不扩展 `schedule_events.type` 为 `quadrant_task`，避免把任务来源语义混入日程块展示类型，也避免影响现有按 `event.Type == "task"` 判断的前端、冲突、撤销和预览逻辑。

实施要求：迁移 SQL 需要回填历史 `type=task` 数据为 `task_source_type=task_item`，新写入的 task_pool 任务必须显式写 `task_source_type=task_pool`。

### 12.6 主动观测链路形态

1. 主动调度主链路走固定 graph / service pipeline，不进入 ReAct 工具循环。
2. graph 建议形态：
   ```text
   ActiveScheduleTrigger
     -> BuildContext
     -> Observe
     -> GenerateCandidates
     -> LLMSelectAndExplain
     -> WritePreview
     -> Notify
   ```
3. `BuildContext / Observe / GenerateCandidates` 使用确定性后端逻辑，负责读取事实、生成诊断、校验候选合法性。
4. `LLMSelectAndExplain` 不调用工具，只直接消费后端给出的结构化结果，负责在候选中选择、生成用户可读解释，或选择 ask_user / close / notify_only。
5. 第一版不提供 ReAct 工具壳；后续如果用户在聊天中主动要求“帮我看看接下来 24 小时安排”，可以再加一个人工触发入口复用同一套 service。
6. API dry-run、API trigger、worker 后台触发都调用同一套主动调度 graph / service，避免出现多套观测逻辑。

### 12.7 未完成补救的局部重排策略

1. 未完成补救里的局部重排不是整周 / 整任务类重排，而是只处理受影响的部分 `task_item`。
2. 局部重排输入：
   - 起点：当前时刻对应的相对时间坐标。
   - 终点：目标任务所属 `task_class.end_date`。
   - 任务集：未完成任务及其被挤压的后继 item，而不是整个 task_class 的全部 item。
3. 粗排约束调整：
   - 原有周几偏好、时段偏好在正式粗排里偏硬约束。
   - 局部补救中改成软偏好：优先落在偏好范围内。
   - 如果偏好范围内排不下，允许打破偏好，把剩余任务继续追加到可用时间里。
4. 排序语义：
   - 补救过程中可以为了找槽位临时调整候选顺序。
   - 输出结果需要恢复这些受影响任务的原有顺序语义，避免把后继关系打乱。
5. 工程实现：
   - 不直接修改现有全量粗排主函数，避免影响现有智能排程行为。
   - 新增一条“局部重排 / 偏好软化粗排”实现。
   - 时间格构建、空位扫描、冲突判断、节次候选等公共能力优先抽公共层复用；若短期无法完全抽出，需要在实现注释中说明原因，避免长期复制第三份粗排逻辑。

### 12.8 压缩融合兜底候选

1. 压缩融合只作为局部重排和延后结束都不可用时的后续兜底候选。
2. 第一轮实现先关闭，不生成 `compress_with_next_dynamic_task`。
3. 后续打开时固定选择“下一个动态任务”作为融合对象，不做跨多个后继任务的复杂搜索。
4. 后续打开时默认比例为 50% / 50%：
   - 未完成任务压缩到融合块的一半时间。
   - 下一个动态任务压缩到融合块的一半时间。
5. 压缩融合必须写清风险说明：两个任务都会被压缩，需要用户接受 rush 模式。
6. 压缩融合只生成预览，不允许后台自动执行。

### 12.9 主动调度裁决模式

1. 主动调度参考 `analyze_health` 的裁决模式，但不复用其节奏指标。
2. 后端固定执行：
   ```text
   观测事实
     -> 生成 issues
     -> 收集 missing_info
     -> 尝试生成合法 candidates
     -> 构造 decision
   ```
3. `decision.action` 第一版包含：
   - `close`：没有值得处理的问题，或问题已被现有日程覆盖。
   - `ask_user`：缺少关键事实，或需要用户放宽边界才能继续。
   - `notify_only`：有风险但无合法调整候选，也没有一个明确问题能继续推进。
   - `select_candidate`：存在 1~3 个后端校验过的合法候选。
4. 基础裁决规则：
   - 没有 issue -> `close`。
   - 有 issue，但缺关键事实 -> `ask_user`。
   - 有 issue，且有合法 candidates -> `select_candidate`。
   - 有 issue，但没有合法 candidates：
     - 若能通过一个明确问题继续推进 -> `ask_user`。
     - 否则 -> `notify_only`。
5. LLM 职责边界：
   - 不判断候选是否合法。
   - 不自由构造新候选。
   - `select_candidate` 时只在候选里选择最合适的一项，并生成用户可读解释。
   - `ask_user / notify_only / close` 时只负责把后端裁决理由说清楚。

### 12.10 主动调度预览持久化边界

1. 主动调度预览新增独立持久化结构，建议命名为 `active_schedule_previews`。
2. 不复用 `agent_schedule_states` 作为主动调度预览主存储，原因：
   - `agent_schedule_states` 强绑定 `conversation_id`，更适合会话内智能排程快照。
   - 主动调度来自后台 worker，可能没有会话上下文。
   - 主动调度预览需要绑定 `trigger_id / candidate_id / expires_at / apply_status / notification_status`，语义与会话快照不同。
3. 展示协议可以复用：
   - 抽通用 `SchedulePreviewChangeItem` / before-after schema。
   - 现有会话排程预览后续也应补齐改前 / 改后能力。
   - 主动调度预览复用同一套 change schema，但独立存储和流转状态。
4. 这一路径更符合后续微服务拆分：
   - `active-scheduler` 负责生成 `active_schedule_previews`。
   - API 负责查询预览与接收确认。
   - schedule 域负责正式应用。

### 12.11 预览快照、确认校验与 apply 结果

1. 第一版不保存全量 before 快照，避免主动调度预览表过重，也避免未来误用全量快照覆盖用户后续改动。
2. 第一版必须保存：
   - `base_version`：生成预览时的日程基准版本，可使用 schedule hash、相关 event 更新时间摘要或等价版本标识。
   - `before_summary`：只保存受影响范围的改前信息，例如受影响 event、空闲槽位、原 task_item 落位。
   - `preview_changes`：候选准备做的改动，例如新增 task_pool 日程、创建补做块、移动可安全处理的 task_item；压缩融合字段只保留后续预留。
3. `before_summary + preview_changes` 的用途：
   - 给用户展示改前 / 改后。
   - 用户确认时校验当前日程是否仍符合预览生成时的基准。
   - 后续补撤销能力时，可以作为局部反向操作的基础。
4. 第一版 apply 策略：
   - 用户确认前不改正式日程，因此不需要回滚。
   - 用户确认后，正式应用必须放在事务里执行。
   - 如果事务失败，正式日程不落库，只把预览标记为 `apply_failed` 并写入 `apply_error`。
5. 第一版不开放 apply 成功后的撤销按钮，不做整版快照覆盖式回滚。
6. apply 成功后轻量记录：
   - `apply_status = applied`
   - `applied_at`
   - `applied_event_ids`
   - 必要时记录 `applied_change_ids`
7. 后续若要支持撤销，应基于后端实际应用成功的 change 做局部反向操作，不能用 apply 前全量快照覆盖整张日程表，避免误删用户后续手动修改。

### 12.12 用户确认入口与聊天增强预留

1. MVP 不走现有 Agent resume 协议，新增主动调度详情页与主动调度确认 API。
2. 飞书通知包含 LLM 生成的简短摘要和详情页链接，默认进入：
   ```text
   /schedule-adjust/{preview_id}
   ```
3. 详情页体验采用“助手卡片式”设计，但后端不依赖完整 Agent Chat：
   - 顶部展示助手解释文案。
   - 中间展示日程前后对比卡片。
   - 展示触发原因、建议理由、风险和不调整后果。
   - 支持用户拖动调整 after 方案。
   - 支持确认应用、忽略 / 拒绝。
4. 拖动后的确认请求必须携带 `edited_changes`，后端重新校验，不信任前端坐标。
5. 确认 API 建议语义：
   ```text
   POST /active-schedule/previews/:preview_id/confirm
   ```
   请求包含 `candidate_id / action / edited_changes / idempotency_key`。
6. 后续增强可把同一个 `preview_id` 导入聊天页：
   ```text
   /agent/chat?active_preview_id=xxx
   ```
   聊天页加载同一份主动调度预览，由助手吐出解释消息和同一张日程卡片。
7. 聊天增强必须复用 `active_schedule_previews / preview_changes / confirm API`，不能另起一套确认和应用协议。
8. 若用户从详情页点击“和助手讨论”，再创建或绑定 `conversation_id`；主动调度预览本身的 `conversation_id` 保持可空。

### 12.13 预览过期策略

1. MVP 主动调度预览有效期为 1 小时。
2. `active_schedule_previews` 需要保存 `expires_at = generated_at + 1h`。
3. 超过 `expires_at` 后：
   - 预览仍可查看历史说明。
   - 不允许确认应用。
   - 前端提示用户重新生成建议。
4. 确认 API 必须校验过期状态，避免用户对旧日程基准执行过期候选。

### 12.14 正式应用同步策略与幂等

1. `schedule.apply.requested` 第一版不进入 outbox 异步消费，确认 API 内同步调用正式应用 service。
2. 同步 apply 的职责包括：
   - 校验 preview 存在、属于当前用户且未过期。
   - 校验 preview 尚未 `applied / rejected / expired`。
   - 校验 `candidate_id` 属于当前 preview。
   - 校验 `edited_changes` 没有越权改目标、没有越过候选允许范围、没有产生日程冲突。
   - 在事务内写入正式日程。
   - 成功或失败后回写 preview 的 apply 状态。
3. 第一版不新增独立 `active_schedule_apply_requests` 表；apply 尝试状态先落在 `active_schedule_previews` 的 apply 字段中。
4. 仍然生成独立 `apply_id`，用于标识一次用户确认应用尝试。
5. 确认请求必须携带 `idempotency_key`，后端建议按 `preview_id + idempotency_key` 做幂等约束。
6. `preview_id + candidate_id` 只定位“用户基于哪一个候选确认”，不代表最终应用内容；若用户拖动 after 方案，最终落库内容以 `edited_changes` 为准。
7. 同一个 preview MVP 只允许成功 apply 一次；apply 成功后再次确认直接返回已应用结果或业务错误，避免重复写入正式日程。
8. 后续若 apply 变重或需要跨服务恢复，再迁移为 `active_schedule_apply_requests + schedule.apply.requested` 异步消费；迁移时复用当前 `apply_id / idempotency_key / apply_status` 语义。

### 12.15 飞书通知最小实现

1. 飞书通知第一版不是纯固定模板：
   - 主链路已调用 LLM 时，顺手生成一段面向用户的调整摘要。
   - 摘要应短、明确、可行动，避免制造焦虑。
   - 固定模板只作为 fallback，用于 LLM 超时、失败、返回空内容或内容校验不过时。
2. 飞书通知必须包含跳转链接：
   ```text
   /schedule-adjust/{preview_id}
   ```
   每个 `preview_id` 对应唯一详情 / 调整页面，用户从飞书点击后回系统查看并确认。
3. 通知幂等键按 `user_id + trigger_type + time_window` 聚合，而不是按 `preview_id`。
4. MVP 的去重含义是：同一用户、同一触发类型、同一时间窗口内只发一条飞书，避免主动调度在短时间内重复打扰用户。
5. 飞书 provider 第一版可以放在 backend worker 内，但必须同步落 `notification_records` 表。
6. `notification_records` 用于：
   - 记录待发送、发送中、成功、失败、死亡状态。
   - 保存 provider 请求摘要、响应摘要、失败原因和重试次数。
   - 支撑后台重试和人工排障。
   - 串联 `trigger_id / preview_id / notification_id`，回答“为什么发了这条飞书”。
7. `notification.feishu.requested` 事件只表达“需要通知用户回来确认”，不承载飞书内确认、日程应用或聊天回复能力。

### 12.16 启动边界拆分状态

1. `api / worker / all` 启动边界第一阶段已经完成，不再作为主动调度 MVP 的前置阻塞项。
2. 当前启动边界语义：
   - `api`：只启动 Gin HTTP 与同步 service / DAO 依赖，不启动后台 worker。
   - `worker`：只启动 outbox relay、Kafka consumer、事件 handler、memory worker，不注册 Gin 路由。
   - `all`：保持迁移期兼容模式，同时启动 HTTP 与 worker，适合本地开发和旧启动方式兜底。
3. 主动调度 MVP 可以直接接入现有 worker / 事件链路：
   - 后台触发、outbox 消费、飞书通知投递放在 worker。
   - dry-run、trigger 测试、预览查询、确认 apply 放在 API。
   - all 模式继续用于本地一键联调。
4. 后续还未完成的是服务边界和模块边界拆分，不是启动生命周期拆分：
   - active-scheduler 尚未独立 Go module。
   - notification 尚未独立 Go module。
   - DAO / service 依赖边界已按 port / adapter 策略拍板，后续执行计划需细化具体端口。

### 12.17 主动调度代码目录与迁移策略

1. 主动调度第一版采用“准独立模块”策略。
2. 第一版不放在 `backend/service/active_scheduler`：
   - 避免主动调度继续长进旧 service 单体。
   - 避免后续迁移时再从既有 service 目录里拆业务边界。
   - 避免把主动调度 graph / pipeline / prompt / 状态机和传统同步 service 混在一起。
3. 第一版放在：
   ```text
   backend/active_scheduler
   ```
4. `backend/active_scheduler` 按未来独立 active-scheduler 服务组织代码：
   - `pipeline / graph`：固定主动调度链路。
   - `context`：ActiveScheduleContext 构造。
   - `observe`：确定性观测和 issue 生成。
   - `candidate`：候选生成与合法性校验。
   - `preview`：预览构造与写入。
   - `apply`：候选到正式应用请求的转换与确认入口协作。
   - `notification`：发布通知请求，不直接沉淀 provider 细节。
5. MVP 暂不拆独立 Go module / 独立进程：
   - 主动调度仍需读取 task、schedule、memory 等现有数据。
   - 确认 apply 已拍板为 API 内同步事务写库，过早独立进程会放大事务边界复杂度。
   - LLM、outbox、worker 注册和配置初始化当前仍在 `backend` 内，先复用现有装配能更快验证主链路。
6. 后续迁出条件：
   - 事件契约稳定。
   - `active_schedule_*` 表结构和状态机稳定。
   - preview / confirm / apply 协议稳定。
   - notification 与 schedule apply 的边界清楚。
7. 迁出时优先采用并行迁移：
   - 保留 `backend/active_scheduler` 旧模块。
   - 新建独立 active-scheduler Go module。
   - 先迁移事件契约和只读链路。
   - 再迁移 worker handler。
   - 验证后切流，最后删除旧实现。

### 12.18 事件契约目录策略

1. 事件契约第一版提前放入：
   ```text
   backend/shared/events
   ```
2. 该目录用于承载异步消息世界里的“IDL”：
   - `event_type` 常量。
   - `event_version`。
   - payload DTO。
   - 基础 validate / normalize。
   - 幂等键、消息键、聚合 ID 等协议字段的构造约定。
3. 该目录禁止承载业务实现：
   - 不放 DAO。
   - 不放 service。
   - 不放 worker handler。
   - 不放 notification provider。
   - 不放 LLM prompt。
   - 不放复杂业务判断。
4. 主动调度、notification、worker handler、API 都依赖 `backend/shared/events` 的事件契约，而不是互相 import 业务模块。
5. 这样可以避免：
   - notification 为了消费通知事件反向依赖 `backend/active_scheduler`。
   - worker handler 为了注册事件依赖具体业务内部 model。
   - 后续 active-scheduler 独立服务时需要大规模重写事件 DTO。
6. 后续迁出微服务时，`backend/shared/events` 可以按并行迁移策略迁出为独立 contracts module，例如：
   ```text
   pkg/events
   ```
   或独立的 event contracts Go module。
7. 事件 payload 不直接复用数据库 model，也不直接复用内部 service request：
   - 数据库 model 容易夹带 GORM tag、关联关系和内部字段。
   - service request 往往表达同步调用语义，不等于异步业务事实。
   - 事件 payload 应表达“发生了什么 / 请求了什么异步动作”，并带明确版本。

### 12.19 主动调度依赖边界

1. 主动调度主链路不直接散落依赖其它领域 DAO。
2. 第一版采用 port / adapter 方式：
   - `backend/active_scheduler` 内定义 `TaskReader / ScheduleReader / MemoryContextReader / ApplyService` 等端口。
   - 主动调度 pipeline 只依赖这些端口，不直接 import `dao.TaskDAO / dao.ScheduleDAO / dao.TaskClassDAO` 等其它领域 DAO。
   - MVP adapter 可以复用现有 service。
   - 如果现有 service 缺少适合后台调度的读模型，允许 adapter 内部调用 DAO 组装事实快照，但 DAO 调用必须封装在 adapter 内。
3. 主动调度自有表由主动调度自己管理：
   ```text
   active_schedule_jobs
   active_schedule_triggers
   active_schedule_previews
   ```
   这些表的数据所有权属于 active-scheduler，后续迁出独立服务时随模块迁移。
4. 读取其它领域事实时使用 reader port：
   - task 池任务读取走 `TaskReader`。
   - schedule 时间窗、冲突和空闲槽读取走 `ScheduleReader`。
   - memory / 用户偏好读取走 `MemoryContextReader`，由 adapter 复用 memory 模块 `Retrieve` 和公共渲染 helper。
   - task_class 约束读取走对应 reader port 或由 schedule/task_class adapter 组合。
5. 正式写入必须走领域 service 或 apply port：
   - task_pool 写入 schedule。
   - task_item 补做块落库。
   - schedule 冲突校验。
   - schedules 原子节次写入。
   - task_class item 状态更新。
6. 主动调度不能绕过既有 schedule / task_class 写入链路直接改正式业务真值。
7. notification provider 不归 `backend/active_scheduler` 管：
   - 主动调度只发布 `notification.feishu.requested`。
   - `notification_records`、飞书 provider 调用、重试和失败观测属于 notification 模块 / worker handler。
8. 这样后续迁移时可以把 adapter 从本地 DAO / service 实现替换为 RPC、HTTP 或事件投影实现，主动调度 pipeline 不需要整体重写。

## 13. 共识详述与实现备忘

本节用于保存讨论过程中的关键推理，避免后续上下文压缩或换对话后只剩简短结论。

### 13.1 为什么后台触发不是全量定时扫描

主动调度的“定时”不是 worker 每隔几分钟全表扫 `tasks`，而是 task 本身在创建或更新时写入一条未来到期 job。

推荐语义：

1. task 创建时，如果有 `urgency_threshold_at`，写入或更新对应 `active_schedule_jobs`。
2. task 更新 `deadline_at / urgency_threshold_at` 时，直接 upsert 覆盖当前有效 job，并刷新 `updated_at`。
3. task 完成时，不物理删除 job，而是把未执行 job 标记为 `canceled`。
4. job 到期后，worker 读取 due job，再重新读取 task 真值：
   - task 已完成 -> 标记 skipped / canceled，不进入主动调度。
   - task 已不满足重要且紧急条件 -> 标记 skipped。
   - task 仍未完成且到达触发条件 -> 生成 `active_schedule.triggered`。

这样做的原因：

1. 避免后台全表扫描放大数据库压力。
2. 触发时间与四象限懒平移机制一致，统一使用 `urgency_threshold_at`，不再维护 `deadline_at - X` 这类主动调度私有阈值。
3. `canceled` 比物理删除更利于审计：后续可以解释“为什么这个任务没有触发主动调度”。
4. upsert 覆盖比“取消旧 job 再新建 job”简单，MVP 足够用。

### 13.2 schedule 动态任务为什么不写定时 job

schedule 里的动态任务计划时间过去后，第一版默认按 `assumed_completed` 推进体验，不主动追问、不自动补救。

只有用户明确反馈未完成时，才进入主动调度链路。例如：

```text
刚才那个没做完
这项要延后
今天撑不住了
```

原因：

1. 自动追问会打扰用户，且用户没有反馈时系统无法确认是真没做还是没打卡。
2. 产品口径已经确定为“默认完成，用户反馈纠偏”。
3. 未完成补救属于用户显式触发，不应由时间流逝自动触发。

### 13.3 用户反馈触发信号为什么要持久化

用户反馈类触发信号不展示给前端，它是后端链路状态。建议使用 `active_schedule_triggers` 保存。

它的目的不是做产品卡片，而是：

1. 幂等：同一条“没做完”反馈不要重复触发两次。
2. 审计：用户问“为什么系统给我发飞书”，可以查到触发原因。
3. 排障：worker 失败、跳过、重试都有状态可查。
4. 串链路：`trigger -> preview -> notification -> apply` 能通过 `trigger_id` 串起来。

建议字段方向：

```text
id
user_id
trigger_type          # important_urgent_task / unfinished_feedback
target_type           # task_pool / schedule_event / task_item
target_id
idempotency_key
payload_json
status                # pending / processing / preview_generated / skipped / failed
created_at
updated_at
```

其中 `unfinished_feedback` 不做固定时间窗强去重，而是依赖 `feedback_id / idempotency_key` 幂等；这样用户连续反馈“还是没做完”不会被 30 分钟窗口误吞。

### 13.4 为什么 task_pool 不转成孤儿 task_item

我们讨论过“把四象限任务转成孤儿 task_item”来复用 `BatchApplyPlans`。最终不采用这个方案。

原因：

1. 现有 `task_items` 基本语义是归属于 `task_classes` 的任务块。
2. 虽然模型里 `CategoryID` 是指针，但 DAO / service 很多地方默认 task_item 有所属 task_class：
   - `BatchApplyPlans` 必须传 `TaskClassID`。
   - `ValidateTaskItemIDsBelongToTaskClass` 用 `category_id` 做归属校验。
   - `GetTaskClassIDByTaskItemID` 直接解引用 `CategoryID`。
   - 预览分类、撤销、约束读取也默认 item 有父级。
3. 孤儿 task_item 会带来一串问题：
   - 属于哪个任务类？
   - 用哪个 task_class 的周几 / 时段偏好？
   - 撤销后回到哪里？
   - task 完成后怎么同步 task_item？
   - 前端任务类列表是否显示这个隐藏 item？

最终方案是保留 task_pool 身份，让 schedule 引用 `tasks.id`。

### 13.5 为什么新增 `task_source_type`，而不是扩展 `schedule_events.type`

已确认在 `schedule_events` 上新增 `task_source_type`。

字段语义：

```text
schedule_events.type             # 日程展示 / 占用类型：course / task
schedule_events.task_source_type # 当 type=task 时的业务来源：task_item / task_pool
schedule_events.rel_id           # 指向对应来源表的 id
```

示例：

```text
动态任务块：
type = task
task_source_type = task_item
rel_id = task_items.id

四象限任务：
type = task
task_source_type = task_pool
rel_id = tasks.id
```

不扩展 `type = quadrant_task` 的原因：

1. `type` 现有语义更像“日历上展示/占用的类型”，四象限任务进入日程后仍然是任务块。
2. 现有代码和前端可能大量判断 `event.Type == "task"`；新增 `quadrant_task` 容易漏分支。
3. “四象限”是任务来源 / 优先级语义，不是日程块类型。
4. 后续如果还有 `manual_task / habit_task / external_task`，都塞进 `type` 会把字段语义撑乱。

历史数据回填策略后续执行计划里再细化：历史 `type=task` 可默认回填为 `task_item`，避免破坏旧动态任务块。

### 13.6 task_pool 任务进入日程的正式写入语义

用户确认 task_pool 候选后，不创建 task_item，直接写正式日程：

```text
schedule_events:
  type = task
  task_source_type = task_pool
  rel_id = tasks.id
  name = tasks.title
  start_time / end_time = 绝对时间

schedules:
  event_id = schedule_events.id
  user_id
  week
  day_of_week
  section
```

这意味着后续读取 schedule 时：

1. 如果 `type=task, task_source_type=task_item`，按旧链路关联 `task_items`。
2. 如果 `type=task, task_source_type=task_pool`，关联 `tasks`。
3. 如果 `task_source_type` 为空且 `type=task`，兼容历史数据，默认按 `task_item` 处理。

### 13.7 滚动 24 小时与节次粒度

MVP 按现有课程表坐标工作，滚动 24 小时需要映射到：

```text
week / day_of_week / section
```

正式应用时仍维护现有 schedule 的绝对时间与相对时间：

1. `schedule_events.start_time / end_time` 保存绝对时间。
2. `schedules.week / day_of_week / section` 保存相对节次原子格。

第一版任务长度：

1. 最小粒度统一为 1 节。
2. task_pool 任务预计长度初步限定在 1~4 节。
3. 由于当前 task 缺少预计耗时，第一版可以使用默认值或在候选里让用户确认。
4. 后续创建 task 时增加预计节数字段，由 AI 根据任务复杂度写入；主动调度只消费该字段，不在调度阶段重新判断复杂度。

### 13.8 task_pool 与 task_item 的偏好来源不同

偏好不能混用。

task_pool 任务：

1. 不属于 task_class。
2. 不存在 task_class 的周几 / 时段硬约束。
3. 按用户 memory 中注入的软偏好安排。
4. 如果 memory 偏好与 24 小时容量冲突，候选里说明“未满足偏好”的代价，而不是称为“打破 task_class 偏好”。

task_item：

1. 属于 task_class。
2. 优先使用所属 task_class 的硬性偏好和约束。
3. 未完成补救场景下，部分 task_class 偏好会在局部重排里从硬约束软化为优先级。

### 13.9 主动观测为什么不进 ReAct

主动调度主链路走固定 graph / service pipeline，不进入 ReAct 工具循环。

原因：

1. 这是后台 worker 触发的链路，不是用户实时开放式问答。
2. 它需要稳定、可幂等、可审计、可重试。
3. ReAct 适合开放探索；主动调度 MVP 的目标是减少开放性，让后端出选择题。
4. LLM 不应该自由查全窗、自由构造写库参数或直接 apply。

固定 graph 形态：

```text
ActiveScheduleTrigger
  -> BuildContext
  -> Observe
  -> GenerateCandidates
  -> LLMSelectAndExplain
  -> WritePreview
  -> Notify
```

其中：

1. `BuildContext / Observe / GenerateCandidates` 是确定性后端逻辑。
2. `LLMSelectAndExplain` 不调用工具，只消费结构化观测结果和候选。
3. API dry-run、API trigger、worker 后台触发都复用同一套 graph / service。
4. 后续若聊天里需要“帮我看看接下来 24 小时安排”，可以加人工触发入口，但也只是调用同一套 service，不另写 ReAct 工具循环。

### 13.10 LLM 在选择题模式里的作用

后端给候选，并不代表 LLM 没有价值。后端负责合法性和硬约束，LLM 负责软约束仲裁与表达。

后端擅长：

1. 判断时段是否冲突。
2. 判断候选是否越过 24 小时窗口。
3. 判断容量是否足够。
4. 判断正式写入参数是否合法。
5. 生成 1~3 个可执行候选。

LLM 擅长：

1. 结合用户刚才语气判断是否疲劳。
2. 在候选分数接近时，根据 memory 软偏好选更容易被接受的方案。
3. 把结构化风险翻译成用户能理解的解释。
4. ask_user 时问得更自然，不让用户觉得被系统打断。
5. notify_only 时用提醒语气，而不是制造焦虑。

边界：

1. LLM 不判断候选是否合法。
2. LLM 不自由构造新候选。
3. LLM 只在 `decision.action=select_candidate` 时从候选里选。
4. `close / ask_user / notify_only` 时，LLM 只负责表达后端裁决理由。

一句话：后端保证不出错，LLM 负责更像人。

### 13.11 后端裁决如何参考 analyze_health

主动调度参考 `analyze_health` 的裁决模式，而不是复用其节奏指标。

主动调度自己的裁决流程：

```text
观测事实
  -> 生成 issues
  -> 收集 missing_info
  -> 尝试生成合法 candidates
  -> 构造 decision
```

裁决规则：

1. 没有 issue -> `close`。
2. 有 issue，但缺关键事实 -> `ask_user`。
3. 有 issue，且有合法 candidates -> `select_candidate`。
4. 有 issue，但没有合法 candidates：
   - 如果能通过一个明确问题继续推进 -> `ask_user`。
   - 如果问用户也不能立刻推进，只是需要提醒 -> `notify_only`。

例子：

1. `close`：重要且紧急 task 已经在 schedule 里，或任务已完成。
2. `ask_user`：用户说“刚才那个没做完”，但系统无法定位是哪条 schedule_event；或容量不足，需要问能否延后结束时间。
3. `select_candidate`：找到合法的加入日程 / 未完成补救候选；压缩融合第一轮关闭，后续打开后再纳入该分支。
4. `notify_only`：有风险但没有安全可挪的任务，也没有一个明确问题能继续推进。

### 13.12 未完成补救的局部重排不是全量粗排

未完成补救里的局部重排是“偏好软化版局部粗排”。

输入：

1. 起点：当前时刻对应的相对时间坐标。
2. 终点：目标任务所属 `task_class.end_date`。
3. 任务集：未完成任务及被挤压的后继 item。
4. 不传整个 task_class 的全部 item。

偏好处理：

1. 现有全量粗排里的周几 / 时段偏好偏硬约束。
2. 局部补救中改为软偏好。
3. 优先排偏好范围内。
4. 偏好范围内排不下时，允许打破偏好，把剩余任务继续追加到可用时间里。

顺序处理：

1. 搜索候选时可以为了找槽位临时调整。
2. 输出需要恢复受影响任务的原有顺序语义，避免打乱后继关系。

工程策略：

1. 不直接改现有全量粗排主函数，避免影响当前智能排程行为。
2. 新增局部重排实现。
3. 时间格、可用槽位、冲突判断、节次候选等能力优先抽公共层。
4. 如果短期必须 copy 逻辑，需要在注释里写清楚为什么暂时不能抽公共层，避免长期复制第三份。

### 13.13 压缩融合为什么是兜底

压缩融合不是理想调度，只是当局部重排和延后结束都不可用时的兜底预览。

MVP 规则：

1. 只找下一个动态任务作为融合对象。
2. 不跨多个后继任务搜索。
3. 默认 50% / 50%。
4. 必须向用户说明两个任务都会被压缩。
5. 只生成预览，不允许后台自动执行。

产品语义：

1. 它通常比直接跳过失败任务更好。
2. 但它会牺牲两个任务质量，所以必须用户确认。
3. 后续可以用优先级、DDL、预计耗时动态调整比例，但第一版固定。

### 13.14 为什么主动调度预览不塞进 agent_schedule_states

`agent_schedule_states` 更像会话内智能排程快照，强绑定 `conversation_id`，用于粗排、拖拽、微调。

主动调度预览不同：

1. 可能没有 conversation。
2. 来自后台 worker。
3. 绑定 `trigger_id`。
4. 有 `candidate_id`。
5. 有 `expires_at`。
6. 有通知状态和 apply 状态。
7. 要做幂等、防重复触达、审计。

因此新增 `active_schedule_previews`，但抽通用 before/after 展示协议。

这意味着：

1. 持久化表不复用。
2. 展示 schema 可以复用。
3. 现有会话排程预览后续也应该补改前 / 改后能力。
4. 未来迁出 `active-scheduler` 时，预览表边界更清晰。

### 13.15 before_summary、preview_changes 和 applied_event_ids 的意义

MVP 不保存全量 before 快照，也不做成功后的撤销按钮。

必须保存：

```text
base_version
before_summary
preview_changes
```

原因：

1. 用户打开预览时能看到当时那版改前 / 改后，而不是重新查一个已经变化的当前日程。
2. 用户确认时能校验：生成预览时空的时段，现在是否仍然空。
3. 后续要做撤销时，有局部反向操作基础。

不保存全量 before 的原因：

1. 表会很重。
2. 后续如果误用全量快照覆盖日程，会抹掉用户 apply 后手动做的其它修改。
3. 真正安全的撤销应该按后端实际应用成功的 change 做局部反向操作，而不是整版覆写。

apply 成功后轻量记录：

```text
apply_status
applied_at
applied_event_ids
apply_error
```

这些当前用于审计和排障，不是为了第一版开放撤销按钮。

### 13.16 确认入口为什么先做详情页，而不是直接聊天页

聊天页效果最好，但第一版直接做完整聊天页会引入很多复杂度：

1. 后台 preview 没有天然 `conversation_id`。
2. 用户拖动卡片后，要同步到 Agent state 还是 active preview。
3. 用户一句“换晚点”是否重新跑 graph。
4. 聊天 SSE、卡片状态、确认状态要保持一致。
5. notification 和 agent channel 容易混边界。

折中方案：

1. MVP 做主动调度详情页。
2. UI 设计成助手卡片式：
   - 顶部助手解释。
   - 中间日程对比卡片。
   - 支持拖动 after。
   - 支持确认 / 忽略。
3. 后端仍走 `active_schedule_previews` 和确认 API，不依赖完整 Agent Chat。
4. 后续可以通过 `/agent/chat?active_preview_id=xxx` 把同一份 preview 导入聊天页。
5. 聊天增强必须复用同一套 preview / changes / confirm API。

这样第一版稳定，后续聊天效果也能接上，不会重写链路。

### 13.17 预览 1 小时过期的具体语义

MVP 预览有效期为 1 小时：

```text
expires_at = generated_at + 1h
```

过期后：

1. 可以查看历史说明。
2. 不能确认应用。
3. 前端提示重新生成建议。
4. 确认 API 必须拒绝过期 preview。

原因：主动调度候选依赖当时日程基准，时间越久越可能被用户或其它流程改动。1 小时是 MVP 的安全折中。

### 13.18 为什么 MVP 确认接口内同步 apply

第一版正式应用不走 outbox 异步消费，而是在确认 API 内同步调用正式应用 service。

原因：

1. 用户确认是强交互动作，不是后台自然发生的动作。
2. 用户点击确认后，需要尽快知道应用是否成功；同步返回成功 / 失败更符合详情页体验。
3. 当前 MVP 的 apply 范围较小，主要是新增 task_pool 日程块、生成未完成补做块和后续少量候选变体，预计重校验与事务写库在可接受延迟内。
4. 异步 apply 需要新增 apply request 表、恢复扫描、重复消费幂等、前端轮询或 SSE 状态同步，会把第一版链路明显拉长。
5. 当前项目已有 outbox 能力，但主动调度 apply 还没有跨服务边界；提前异步化会让状态机复杂度先于业务复杂度增长。

同步 apply 不等于绕过事件语义。MVP 仍然保留以下状态和事件口径：

```text
用户确认
  -> confirm API 生成 apply_id
  -> 写入 applying 状态
  -> 事务内重校验并调用正式写入 service
  -> 成功：apply_status=applied，记录 applied_event_ids
  -> 失败：apply_status=failed，记录 apply_error
  -> 可按需发布 schedule.apply.succeeded / schedule.apply.failed
```

第一版暂不发布 `schedule.apply.requested` 给 outbox 消费；该事件名可作为后续异步化时的协议入口。

后续迁移到异步 apply 的触发条件：

1. apply 需要调用多个外部服务，确认接口延迟不可控。
2. apply 可能超过普通 HTTP 请求可接受时长。
3. 需要后台自动重试和失败恢复。
4. `active-scheduler` 与 schedule 写入服务拆成独立进程，确认 API 不再适合直接持有完整写入事务。

届时新增：

```text
active_schedule_apply_requests
schedule.apply.requested
apply worker
```

但 `apply_id / idempotency_key / apply_status / applied_event_ids` 的语义保持不变，避免推翻 MVP 数据模型。

### 13.19 为什么 `preview_id + candidate_id` 不能当应用幂等键

`preview_id + candidate_id` 只能说明“用户基于哪份预览里的哪个候选确认”，不能说明“这一次最终要应用什么内容”。

典型场景：

```text
preview_id = p1
candidate_id = c1

c1 原建议：把“写实验报告”安排到今天第 7 节。
用户拖动后：改到今天第 8 节再确认。
```

此时 `p1 + c1` 没变，但最终 apply 内容已经变化。如果把 `preview_id + candidate_id` 当幂等键，会混淆候选身份和执行请求。

推荐分层：

```text
preview_id       # 哪一份主动调度预览
candidate_id     # 基于哪一个候选
edited_changes   # 用户最终确认的真实变更
apply_id         # 哪一次确认应用尝试
idempotency_key  # 防止同一次确认动作重复提交
```

确认请求建议：

```json
{
  "candidate_id": "c1",
  "action": "confirm",
  "edited_changes": [
    {
      "change_id": "chg_1",
      "type": "add_task_pool_to_schedule",
      "task_id": 123,
      "week": 8,
      "day_of_week": 4,
      "section_from": 8,
      "section_to": 8
    }
  ],
  "idempotency_key": "frontend-generated-uuid"
}
```

后端处理规则：

1. `idempotency_key` 由前端为一次确认动作生成；用户双击、请求超时重试、移动端 WebView 重放时必须复用同一个 key。
2. 后端建议按 `preview_id + idempotency_key` 做唯一约束或等价幂等查询。
3. 如果同一个 `preview_id + idempotency_key` 已成功应用，直接返回上一次 `apply_id / applied_event_ids`。
4. 如果同一个 `preview_id + idempotency_key` 正在处理中，返回 `applying` 或在同步路径内等待本次结果。
5. 如果同一个 `preview_id + idempotency_key` 对应的请求体摘要与本次不同，应拒绝请求，避免同一个幂等键被复用到另一套变更。
6. 如果 `idempotency_key` 不同，即使 `candidate_id` 相同，也必须按新的确认尝试处理，并重新校验 preview 是否仍允许 apply。

`active_schedule_previews` 第一版可预留以下 apply 字段：

```text
apply_id
apply_status              # none / applying / applied / failed / rejected / expired
apply_candidate_id
apply_idempotency_key
apply_request_hash
applied_changes_json
applied_event_ids_json
apply_error
applied_at
```

MVP 状态流转建议：

```text
none -> applying -> applied
none -> applying -> failed
none -> rejected
none -> expired
```

第一版建议同一个 preview 只允许成功 apply 一次。`failed` 后是否允许换一个候选再次确认，先不作为 MVP 主路径；若要支持，应生成新的 `apply_id`，并明确旧失败记录如何保留，避免审计链路被覆盖。

### 13.20 飞书通知为什么需要 LLM 摘要、链接和记录表

飞书第一版只做“提醒用户回系统确认”，不在飞书内应用日程，也不做复杂聊天。但它仍然是用户会感知到的主动打扰，因此要兼顾表达质量、跳转确定性和投递可观测。

通知文案：

1. 主动调度链路已经让 LLM 参与候选选择和解释，此时让 LLM 生成一段 summary 成本很低。
2. LLM summary 比固定模板更能解释“为什么现在提醒你”，例如任务即将变紧急、刚才反馈未完成、后续日程被挤压等。
3. summary 只负责表达，不负责决定是否通知、通知谁、跳到哪里；这些仍由后端结构化字段决定。
4. 固定模板必须保留为 fallback，避免 LLM 超时、失败、内容为空或内容校验不过时，整条通知链路直接断掉。

推荐 fallback 方向：

```text
我为你生成了一份日程调整建议，请回到系统确认是否应用。
```

链接规则：

```text
/schedule-adjust/{preview_id}
```

原因：

1. 每个主动调度 preview 都有唯一 `preview_id`，天然适合作为详情页定位键。
2. 用户从飞书点进来后，只进入系统详情页，不在飞书里直接应用日程，避免外部 IM 承担高风险写操作。
3. URL 不暴露 `candidate_id / apply_id`，因为用户进入详情页后仍可查看候选、拖动 after 方案并生成新的确认尝试。
4. 如果后续接入聊天增强，也应由详情页或聊天页读取同一个 `preview_id`，不能另起一套确认协议。

通知幂等键按：

```text
user_id + trigger_type + time_window
```

不按 `preview_id` 的原因：

1. `preview_id` 每次生成都不同，如果按它去重，短时间内重复触发会重复发飞书。
2. 主动调度通知的产品目标是“提醒用户回来处理一类调整”，不是把每次后台生成都推一遍。
3. `user_id + trigger_type + time_window` 更符合“同类打扰聚合”的口径。

MVP 需要注意：

1. `important_urgent_task` 的触发本身已按 `user_id + trigger_type + target_task_id` 做 30 分钟去重；通知层再按 `user_id + trigger_type + time_window` 聚合，可以进一步避免多任务同时到线时连续轰炸。
2. `unfinished_feedback` 触发按反馈幂等键防重复提交；通知层仍可按窗口聚合，避免用户连续表达未完成时收到多条相似飞书。
3. 具体 `time_window` 长度需要在表结构阶段拍板。MVP 可以先与触发去重窗口保持一致，例如 30 分钟；如果未完成反馈希望更即时，也可以单独设更短窗口。
4. 如果后续产品要求“同一窗口内多个不同任务都必须分别通知”，再把 `target_id` 纳入幂等键；MVP 当前先以减少打扰为优先。

`notification_records` 第一版建议字段方向：

```text
id
channel                 # feishu
user_id
trigger_id
preview_id
dedupe_key              # user_id + trigger_type + time_window
target_url              # /schedule-adjust/{preview_id}
summary_text
fallback_used
status                  # pending / sending / sent / failed / dead
attempt_count
next_retry_at
last_error
provider_request_json
provider_response_json
sent_at
created_at
updated_at
```

状态语义：

1. `pending`：已生成记录，等待 provider 投递。
2. `sending`：当前 worker 正在调用飞书 provider。
3. `sent`：provider 明确返回成功。
4. `failed`：本次投递失败，但仍可重试。
5. `dead`：超过最大重试次数或遇到不可恢复错误，不再自动重试。

重试原则：

1. 飞书 provider 属于不可靠外部服务，不能只依赖日志排障。
2. provider 调用前先落 `notification_records`，避免进程崩溃后丢失“本该通知”的事实。
3. 重试时必须复用同一条 `notification_records`，递增 `attempt_count`，更新 `last_error / next_retry_at`。
4. 若同一 `dedupe_key` 已存在 `pending / sending / sent` 记录，应避免重复创建新通知；如果上一条是 `failed`，可按重试策略推进，而不是新建多条相同飞书。
5. 记录表只负责通知投递状态，不负责 apply 状态；apply 状态仍属于 `active_schedule_previews`。

### 13.20.1 飞书 Webhook 触发器与多用户配置

本轮真实飞书接入不使用群自定义机器人 `msg_type=text/post` 协议，而是使用飞书 Webhook 触发器。后端只负责把“SmartFlow 生成了一条日程调整建议”这个业务事实 POST 给用户配置的 webhook；飞书侧如何私聊、群发、分支、追加查询或调用其它流程，全部由用户在飞书工作流中编排。

用户级配置表建议：

```text
user_notification_channels
- id
- user_id
- channel                 # feishu_webhook
- enabled
- webhook_url             # 用户复制的飞书 Webhook 触发器 URL
- auth_type               # none / bearer
- bearer_token            # 可选；飞书触发器启用 Bearer Token 时使用
- last_test_status        # success / failed
- last_test_error
- last_test_at
- created_at
- updated_at
```

管理接口建议：

```text
GET    /api/v1/notification/channels/feishu
PUT    /api/v1/notification/channels/feishu
DELETE /api/v1/notification/channels/feishu
POST   /api/v1/notification/channels/feishu/test
```

接口语义：

1. `PUT` 保存当前用户的 webhook 配置；`webhook_url` 必须是 HTTPS URL，域名第一版限制为 `www.feishu.cn` 或 `feishu.cn`。
2. `GET` 返回当前用户配置状态，`webhook_url / bearer_token` 只允许脱敏回显。
3. `DELETE` 关闭并软删除当前用户飞书通知配置。
4. `test` 使用同一套 provider 发送测试 JSON，并把 `last_test_status / last_test_error / last_test_at` 写回配置表。
5. 未配置或未启用时，真实通知不报错阻断主链路，`notification_records.status` 记为 `skipped`，表示当前用户没有启用飞书触达。

发送给飞书 Webhook 触发器的业务 JSON 固定从简：

```json
{
  "event": "smartflow.schedule_adjustment_ready",
  "version": "1",
  "notification_id": 123,
  "user_id": 5,
  "preview_id": "asp_xxx",
  "trigger_id": "ast_xxx",
  "trigger_type": "important_urgent_task",
  "target_type": "task_pool",
  "target_id": 81,
  "message": {
    "title": "SmartFlow 日程调整建议",
    "summary": "把重要且紧急任务放入滚动 24 小时内的空闲节次。",
    "action_text": "查看并确认调整",
    "action_url": "https://smartflow.example.com/schedule-adjust/asp_xxx"
  },
  "trace_id": "trace_xxx",
  "sent_at": "2026-04-30T17:34:52+08:00"
}
```

拼装规则：

1. `message.title` 固定为 `SmartFlow 日程调整建议`。
2. `message.summary` 优先使用 preview 的 `notification_summary`，为空时使用 notification fallback 文案。
3. `message.action_text` 固定为 `查看并确认调整`。
4. `message.action_url` 使用 `frontend_base_url + target_url`；若 `target_url` 已经是完整 HTTPS URL，则直接使用。
5. 其它字段只做飞书流程编排、排障和审计，不要求用户流程全部使用。

飞书侧推荐消息模板：

```text
{{message.title}}

{{message.summary}}  {{message.action_text}}
{{message.action_url}}
```

真实 provider 状态映射：

1. HTTP 2xx 且响应体 `code=0` 或响应体为空：视为成功，`notification_records.status=sent`。
2. 网络错误、超时、HTTP 429、HTTP 5xx：视为临时失败，进入 `failed` 并按现有 retry loop 重试。
3. URL 非法、未配置、未启用：视为 `skipped`，不重试。
4. HTTP 401 / 403 或飞书明确返回鉴权失败：视为不可恢复失败，进入 `dead`。

安全约束：

1. webhook URL 本身等同密钥，接口和日志必须脱敏，禁止完整回显。
2. bearer token 同样禁止完整回显；后续若引入统一密钥加密能力，再把明文存储替换为加密存储。
3. 测试接口可以暴露成功 / 失败分类，但不能把完整 webhook 或 token 打到响应和日志里。

### 13.21 为什么事件契约要提前独立

事件契约可以理解为异步消息世界里的 IDL。Thrift / gRPC 描述同步 RPC 的请求、响应和字段语义；事件契约描述某个业务事实或异步动作的事件名、版本、payload、幂等键和消费语义。

主动调度里会出现多类跨边界事件：

```text
active_schedule.triggered
schedule.preview.generated
notification.feishu.requested
schedule.apply.succeeded
schedule.apply.failed
```

这些事件会被不同模块使用：

1. `backend/active_scheduler` 生成 trigger、preview 和 notification request。
2. worker handler 注册和消费 outbox / Kafka 事件。
3. notification 投递模块消费 `notification.feishu.requested`。
4. API 查询 preview、确认 apply 后可能发布 apply 成功 / 失败事件。

如果事件 DTO 放在 `backend/active_scheduler` 内部，后续容易形成反向依赖：

```text
notification -> active_scheduler
worker handler -> active_scheduler
API -> active_scheduler internal model
```

这样主动调度就会变成事实上的共享业务包，未来拆独立服务时边界会很难清理。

提前放到 `backend/shared/events` 的目的：

1. 让发布方和消费方只共享协议，不共享实现。
2. 让 notification 不需要理解主动调度内部 preview 结构，只消费稳定的通知事件 payload。
3. 让 worker handler 不需要 import 主动调度内部包才能注册事件。
4. 给后续切到独立 active-scheduler / notification 服务预留 contracts module。
5. 让事件版本演进有明确入口，避免直接复用内部 Go struct 导致兼容性失控。

边界约束：

1. `backend/shared/events` 只放契约，不放业务。
2. payload DTO 必须是为事件专门设计的结构，不直接复用 GORM model。
3. 字段新增优先保持向后兼容；破坏性调整必须提升 `event_version`。
4. 消费者必须按 `event_type + event_version` 解析，不能依赖生产者内部实现。
5. 幂等键、消息键、聚合 ID 的构造口径应写在事件契约旁边，避免发布方和消费方各猜一套。

### 13.22 为什么主动调度依赖边界采用 port / adapter

主动调度如果直接依赖一堆其它领域 DAO，后续拆微服务时边界会变得很模糊。

风险：

1. 主动调度会绕过 task、schedule、task_class 现有 service 中的权限校验、冲突判断、时间转换和状态流转。
2. 主动调度会知道太多表结构，后续 task / schedule 拆服务时需要大改主动调度主链路。
3. 调度决策会和领域数据所有权混在一起，难以判断哪些调用只是读事实，哪些调用在修改业务真值。
4. 未来迁移时容易变成“换了目录的单体代码”，不是清晰的 active-scheduler 服务。

但也不能简单要求全部走现有 service：

1. 现有 service 很多是面向 HTTP API 入参和前端响应设计的，不一定适合后台主动调度。
2. 主动调度需要滚动 24 小时事实快照、局部可用槽、触发上下文等读模型，现有 service 未必已经提供。
3. 主动调度自有表不属于 task / schedule / task_class，没必要绕到旧 service。
4. 某些现有 service 太粗，可能带缓存、响应结构或前端 DTO，不适合作为内部调度 pipeline 的稳定边界。

因此采用分层策略：

```text
读事实：优先通过领域 service / query port
写正式业务：必须通过领域 service / apply port
写主动调度自有表：使用 active_scheduler 自己的 repo
```

推荐端口方向：

```go
type TaskReader interface {
    GetTaskForActiveSchedule(...)
    ListUrgentUnscheduledTasks(...)
}

type ScheduleReader interface {
    GetScheduleFacts(...)
    HasSlotConflict(...)
}

type MemoryContextReader interface {
    LoadScheduleMemoryContext(...)
}

type ScheduleApplyService interface {
    ApplyActiveScheduleChanges(...)
}
```

MVP 里这些端口的 adapter 可以在 `backend` 内调用现有 service。若现有 service 缺少合适读模型，adapter 内部可以调用 DAO 组装，但主动调度 pipeline 不应该直接依赖 DAO。memory 读取不新造结构化偏好 DAO，先复用 memory 模块 `Retrieve`，并把渲染逻辑抽成公共 helper 供 newAgent 与主动调度共同使用。

这样未来迁出时替换的是 adapter：

```text
本地 service / DAO adapter
  -> RPC / HTTP adapter
  -> 事件投影 / read model adapter
```

主动调度的 `BuildContext / Observe / GenerateCandidates / LLMSelectAndExplain / WritePreview / Notify` 主链路不需要重写。

一句话：主动调度可以拥有自己的 repo，但不能把别人的 DAO 当自己的内部能力随便用。

## 14. 验证流程与动作-预期 checklist

### 14.1 验证目标

主动调度 MVP 的验收重点在后端闭环，而不是前端页面完成度。前端第一版只需要能打开 `/schedule-adjust/{preview_id}`、展示预览、提交确认即可；核心验证应覆盖：

1. 触发是否正确：task 到达 `urgency_threshold_at`、用户反馈未完成、API 测试触发都能进入统一链路。
2. 去重是否正确：同一触发不会重复生成预览、重复通知或重复 apply。
3. 预览是否正确：只写 `active_schedule_previews`，不提前修改正式日程。
4. 通知是否正确：写 `notification_records`，失败可观测、可重试。
5. 确认是否正确：确认后同步重校验并事务写入正式日程，失败不落库。
6. 状态是否正确：`job -> trigger -> preview -> notification -> apply` 能通过 ID 串起来排障。
7. 边界是否正确：主动调度不进入 ReAct 工具循环，不绕过 schedule / task_class 正式写入链路。

### 14.2 验证环境

建议至少准备三种运行方式：

1. `all` 模式：本地一键联调，验证 API + worker 同进程闭环。
2. `api + worker` 分进程模式：验证启动边界拆分后，API 发布 outbox、worker 消费事件。
3. provider mock 模式：飞书 provider 使用 mock 或测试 webhook，避免真实通知影响用户。

验证时需要可观察以下数据：

```text
active_schedule_jobs
active_schedule_triggers
active_schedule_previews
notification_records
user_notification_channels
outbox / event bus 消费状态
schedule_events
schedules
tasks
```

### 14.3 最小测试数据

建议准备一个固定测试用户和以下数据：

1. 至少 1 个 `important_urgent_task`：
   - `is_completed=false`
   - `urgency_threshold_at` 可通过 `mock_now` 命中
   - 当前 24 小时内尚未进入 schedule
2. 至少 1 个已完成或不再紧急的 task：
   - 用于验证 job 到期后能 `skipped / canceled`
3. 至少 1 个已有 schedule 动态任务：
   - 用于模拟用户反馈 `unfinished_feedback`
4. 至少 1 段 24 小时内空闲节次：
   - 用于生成 `add_task_pool_to_schedule` 候选
5. 至少 1 段冲突节次：
   - 用于验证候选校验和 confirm 重校验失败
6. 至少 1 条用户偏好 memory：
   - 用于验证 task_pool 候选使用 memory 软偏好

### 14.4 API dry-run checklist

| 动作 | 预期 |
| --- | --- |
| 调用主动调度 `dry-run`，传入 `important_urgent_task` 与 `mock_now` | 同步返回 context / issues / decision / candidates，不写 `active_schedule_previews` |
| dry-run 命中无问题任务 | 返回 `decision.action=close`，不生成 candidates |
| dry-run 任务缺少必要事实 | 返回 `ask_user` 或 `notify_only`，说明 missing_info |
| dry-run 传入非法 `mock_now` 或非法 target | 返回参数错误，不写 trigger / preview / notification |
| dry-run 连续调用同一输入 | 每次只返回诊断结果，不触发去重状态、不发飞书 |

### 14.5 trigger 与 worker checklist

| 动作 | 预期 |
| --- | --- |
| task 创建时写入 `urgency_threshold_at` | upsert `active_schedule_jobs`，状态为待触发 |
| task 更新 `deadline_at / urgency_threshold_at` | 覆盖当前有效 job 的触发时间并更新 `updated_at` |
| task 完成 | 未执行 job 标记为 `canceled`，不物理删除 |
| worker 扫描到 due job 且 task 仍未完成 / 未进入日程 | 生成 `active_schedule.triggered` 或等价 trigger 记录 |
| worker 扫描到 due job 但 task 已完成 | job / trigger 标记为 `skipped / canceled`，不写 preview |
| API `trigger` 使用 `mock_now` | 写入 trigger，payload 标记 `is_mock_time=true` |
| 后台真实 worker 触发 | 不允许传入 `mock_now`，使用真实当前时间 |
| 同一用户同一 task 30 分钟内重复触发 `important_urgent_task` | 命中去重，不重复生成 preview 和飞书通知 |

### 14.6 preview checklist

| 动作 | 预期 |
| --- | --- |
| 正式 trigger 成功生成候选 | 写入 `active_schedule_previews`，包含 `trigger_id / candidate_id / base_version / before_summary / preview_changes / expires_at` |
| preview 生成完成 | `active_schedule_triggers.status=preview_generated` 或等价状态 |
| preview 生成后查询正式日程 | `schedule_events / schedules` 未发生变化 |
| preview 查询接口读取详情 | 返回触发原因、解释摘要、before/after、风险、不调整后果、候选信息 |
| preview 超过 1 小时 | 仍可查看历史说明，但确认 API 拒绝 apply |
| 日程在 preview 生成后被用户手动改动 | confirm 时基于 `base_version / before_summary` 重校验失败，正式日程不落库 |

### 14.7 notification checklist

| 动作 | 预期 |
| --- | --- |
| preview 生成成功 | 发布 `notification.feishu.requested` 或等价 outbox 事件 |
| notification handler 收到事件 | 先写 `notification_records`，再调用 provider |
| LLM summary 生成成功 | 飞书文案使用 summary，包含 `/schedule-adjust/{preview_id}` |
| LLM summary 失败 / 超时 / 空内容 | 使用固定 fallback 文案，通知链路不中断 |
| 飞书 provider 返回成功 | `notification_records.status=sent`，记录 `sent_at / provider_response_json` |
| 飞书 provider 返回临时失败 | `notification_records.status=failed`，递增 `attempt_count`，写 `last_error / next_retry_at` |
| 重试到达上限或不可恢复错误 | `notification_records.status=dead`，不再自动重试 |
| 同一 `user_id + trigger_type + time_window` 内重复通知 | 命中 `dedupe_key`，不重复创建多条待发送通知 |
| 用户未配置或禁用飞书 webhook | `notification_records.status=skipped`，不重试，不影响 preview 查询 |
| 调用飞书 webhook 测试接口 | 写入 / 更新 `user_notification_channels.last_test_status / last_test_at`，飞书流程收到极简 JSON |

### 14.8 confirm apply checklist

| 动作 | 预期 |
| --- | --- |
| 用户打开 `/schedule-adjust/{preview_id}` | 能读取 preview 详情；如果已过期，页面显示不可确认 |
| 用户确认原候选 | confirm API 生成 `apply_id`，写入 `applying`，同步重校验后事务写正式日程 |
| 用户拖动 after 方案后确认 | 请求携带 `edited_changes`，后端重新校验坐标和目标，不信任前端 |
| task_pool 候选确认成功 | 写入 `schedule_events(type=task, task_source_type=task_pool, rel_id=tasks.id)` 和对应 `schedules` 原子节次 |
| task_item 补做块确认成功 | 通过 schedule / task_class apply port 或现有领域 service 写入，不绕过既有正式写入链路 |
| confirm 时发生冲突 | 事务不落库，preview 标记 `apply_failed`，写入 `apply_error` |
| confirm 成功 | preview 标记 `applied`，记录 `applied_at / applied_event_ids / applied_changes_json` |
| confirm 成功后再次确认同一 preview | 不重复写日程，返回已应用结果或明确业务错误 |
| confirm 使用过期 preview | 拒绝 apply，不写正式日程 |

### 14.9 幂等与重复提交 checklist

| 动作 | 预期 |
| --- | --- |
| 同一 `preview_id + idempotency_key` 重复提交相同 confirm 请求 | 返回同一个 `apply_id` 和同一组 apply 结果，不重复写日程 |
| 同一 `preview_id + idempotency_key` 提交不同请求体 | 拒绝请求，提示幂等键被复用到不同内容 |
| 同一 `preview_id` 使用不同 `idempotency_key` 再次确认 | 若 preview 已 applied，拒绝重复应用或返回已应用状态 |
| 网络超时后前端重试 | 后端根据 `idempotency_key` 返回上一轮结果 |
| worker 重复消费同一通知事件 | `notification_records.dedupe_key` 防止重复飞书 |

### 14.10 失败注入 checklist

| 动作 | 预期 |
| --- | --- |
| 构造 LLM 选择超时 | 使用后端 fallback 决策或标记失败，trigger 状态可排障 |
| 构造 LLM summary 超时 | 使用固定通知模板，preview 仍可通知 |
| 构造 DB 写 preview 失败 | trigger 标记 failed，不发布 notification |
| 构造 notification provider 失败 | preview 保留，notification record 进入 failed / retry，不影响 preview 查询 |
| 构造 apply 写 schedule 中途失败 | 事务回滚，`schedule_events / schedules` 不产生半写状态 |
| 构造 outbox 消费重复 | 消费幂等，业务状态不重复推进 |

### 14.11 自动化测试建议

自动化测试原则：

1. 能自动验收的，由实现者自己完成，不把可自动验证的工作转交给用户。
2. 能用测试代码验证的，优先写单元测试或集成测试；若按项目规则临时生成 `*_test.go`，测试后必须删除临时测试文件。
3. 能用 API + DB 查询验证的，必须实际调用接口并核对落库结果，不能只说“理论上可行”。
4. 能用 mock provider 验证的外部服务链路，必须先用 mock 跑通状态机，再说明真实 provider 还剩哪些人工配置。
5. 实在无法自动完成的验收项必须显式列入“需要用户验收”清单，写清动作、预期和阻塞原因。
6. 最终报告必须区分：
   - 已自动验收通过。
   - 已自动验收失败并修复。
   - 因环境 / 权限 / 外部服务限制未能验收，需要用户执行。

建议自动化流程：

1. 静态与编译验证：
   ```text
   gofmt 相关改动文件
   go test ./...
   清理项目根目录 .gocache
   检查新增 / 改动 Go 文件是否超过 700 行
   检查是否遗留临时 *_test.go
   ```
2. 单元测试：
   - 时间窗转换：绝对时间到 `week / day_of_week / section`。
   - 候选合法性校验：冲突、越界、预计节数、target 篡改。
   - decision 裁决：`close / ask_user / notify_only / select_candidate`。
   - 幂等键与 `apply_request_hash` 校验。
   - notification `dedupe_key` 生成。
3. API + DB 集成测试：
   - dry-run 不落库。
   - trigger 写 trigger / preview / notification record。
   - confirm 成功写 schedule。
   - confirm 冲突失败不落库。
   - 过期 preview 拒绝 apply。
4. Worker 测试：
   - due job 扫描。
   - outbox 发布和消费。
   - notification retry。
5. 外部 provider 测试：
   - mock provider 成功：`notification_records.status=sent`。
   - mock provider 临时失败：`status=failed`，写 `attempt_count / last_error / next_retry_at`。
   - retry 后成功：同一条 record 变为 `sent`，不新建重复通知。
   - 真实飞书 webhook / open_id 受限时，必须记录为“需要用户验收”，不能用 mock 结果冒充真实 provider 验收。
6. 手工验收：
   - 使用 `/schedule-adjust/{preview_id}` 打开详情页。
   - 拖动 after 方案并确认。
   - 查看飞书测试消息跳转。

每阶段交付报告模板：

```text
已自动验收：
- go test ./...：通过 / 失败后已修复
- API 链路：列出请求、关键 ID、响应状态
- DB 核对：列出表名、关键字段和结果
- 幂等 / 失败注入：列出动作和结果

未能自动验收：
- 验收项：
- 阻塞原因：
- 需要用户执行的动作：
- 预期结果：

风险与下一步：
- 尚未覆盖的边界：
- 建议下一阶段优先补的自动化：
```

### 14.12 验收通过标准

MVP 验收通过至少需要满足：

1. `important_urgent_task` 和 `unfinished_feedback` 两条主触发均可生成 preview。
2. dry-run、trigger、worker 三类入口进入同一套主动调度 pipeline。
3. preview 生成前后正式日程不被提前修改。
4. 飞书通知记录可查，成功 / 失败 / 重试状态可观察。
5. confirm 成功后正式日程正确写入，失败时事务不落库。
6. 重复触发、重复通知、重复 confirm 都有幂等保护。
7. 过期 preview、日程基准变化、前端篡改 `edited_changes` 都能被拒绝。
8. 关键状态能通过 `trigger_id / preview_id / notification_id / apply_id` 串起来排障。
