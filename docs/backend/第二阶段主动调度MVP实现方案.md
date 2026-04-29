# 第二阶段主动调度 MVP 实现方案

## 0. Handoff 说明

本文档仍在讨论中，尚未进入代码实现阶段。接手者请优先阅读本节和第 12 / 13 节，再继续补齐未拍板问题。

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

当前仍需拍板的问题：

1. `schedule.apply.requested` 第一版到底如何切分同步 / 异步：
   - 已讨论倾向：确认接口同步写 Redis 状态和轻量校验；MySQL 正式写入可异步，因为重校验可能需要几百毫秒。
   - 待继续明确：是否必须通过 outbox / apply request 表保证异步 apply 请求可恢复，还是 MVP 先同步调用 service。
2. 应用幂等键：
   - 已解释：`preview_id + candidate_id` 只能定位候选；若支持拖动，实际 apply 内容可能不同。
   - 待拍板：是否使用独立 `apply_id`，并用 `idempotency_key` 绑定一次确认尝试。
3. 飞书通知：
   - 固定文案是否足够。
   - 跳转 URL 规则。
   - 通知幂等键按 `preview_id` 还是其它组合。
   - 第一版是否落 `notification_records` 表，还是先只记录日志 / outbox 状态。
4. 主动调度代码目录和迁移边界：
   - 第一版放 `backend/service/active_scheduler` 还是新建更独立的 `backend/active_scheduler`。
   - 事件契约是否提前放到 shared/events 风格目录。
5. `active_schedule_jobs / active_schedule_triggers / active_schedule_previews / apply request` 的具体表结构和状态机。
6. task_pool 任务预计节数字段是否本轮加到 `tasks`，还是 MVP 先默认 1 节并在预览中要求用户确认。

建议下一轮继续顺序：

1. 先拍板 apply 同步 / 异步和 `apply_id`。
2. 再拍板主动调度相关表结构与状态机。
3. 再拍板飞书通知最小实现。
4. 最后补详细执行计划：目录、DTO、迁移 SQL、API、worker handler、测试。

## 1. 文档目的

本文档承接《第二阶段主动调度 MVP 功能预期》和《微服务四步迁移与第二阶段并行开发计划》，用于把产品预期逐步落成可执行的工程方案。

本轮讨论采用“先业务逻辑，后执行计划”的方式推进：

1. 先按模块说明业务实现逻辑，确认这件事在产品上到底怎么流转。
2. 再列出需要拍板的问题，避免工程方案提前固化错误边界。
3. 等业务逻辑讨论完成后，再把详细执行计划、文件改动、测试方式补进对应模块。

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
  -> schedule.apply.requested
  -> 复用正式应用链路
  -> schedule.apply.succeeded / schedule.apply.failed
```

## 4. 模块一：触发入口与事件契约

### 4.1 业务实现逻辑简述

主动调度不应该依赖用户打开聊天后才发生。第一版需要支持三类入口：

1. 后台 worker 定时扫描或按事件触发。
2. API dry-run / trigger 测试触发，便于开发和验收。
3. 用户反馈类触发，例如明确说某个已排任务没完成，或表达疲劳。

三类入口最终都归一成同一个 `ActiveScheduleTrigger`，再进入同一条观测链路。

### 4.2 需要拍板的问题

1. 第一版触发源是否只做两个：`important_urgent_task` 和 `unfinished_feedback`？
   - 已确认：第一版先做这两类主触发。`fatigue_feedback` 可作为用户反馈类的后续扩展，不抢第一轮主链路。
2. API 测试触发是否允许直接同步返回诊断结果，还是必须也写入 outbox 后异步消费？
   - 已确认：两种都保留。`dry-run` 同步返回诊断结果，不写预览、不发通知；`trigger` 走正式异步链路，写预览并发布通知事件。
3. `mock_now` 是否只允许测试接口传入，后台真实 worker 禁止传入？
   - 已确认：`mock_now` 只允许 API dry-run / 测试 trigger 使用；后台 worker 正式定时触发必须使用真实当前时间。
4. 同一用户短时间多次触发的去重窗口设多长？
   - 已确认：`important_urgent_task` 按 `user_id + trigger_type + target_task_id` 做 30 分钟去重；`unfinished_feedback` 按用户反馈的 `feedback_id / idempotency_key` 防重复提交，不做固定时间窗强去重。

### 4.3 待补执行计划

业务逻辑确认后补充：

1. DTO 字段定义。
2. 事件名、event_version、payload 示例。
3. API trigger / dry-run 路由设计。
4. worker handler 注册位置。
5. 单元测试与集成测试方案。

## 5. 模块二：ActiveScheduleContext 构造

### 5.1 业务实现逻辑简述

`ActiveScheduleContext` 是主动调度的统一输入快照。它负责把用户、时间窗、任务、日程、四象限任务池、偏好、近期反馈和触发来源装配到一起。

上下文构造阶段需要先触发或复用四象限紧急性派生，避免后台读到懒加载前的旧任务池。

### 5.2 需要拍板的问题

1. 滚动 24 小时如何映射到当前“周 + 星期 + 节次”模型？是否第一版只按节次粒度处理？
   - 已确认：候选窗口按任务 DDL / 当前滚动 24 小时映射到现有相对时间坐标（week/day_of_week/section），正式写入仍同时维护 schedule 现有的绝对时间与相对时间字段。
   - 已确认：第一版统一按 1 节粒度处理；任务预计长度先限定在 1~4 节，后续可在创建 task 时由 AI 根据复杂度写入预计节数。
2. 四象限任务池里的 `tasks` 是否需要映射到 `task_items`，还是主动调度预览直接支持 task_pool 任务？
   - 已确认：不创建无所属任务类的“孤儿 task_item”。四象限任务进入日程时保留 task_pool 身份，通过 `schedule_events.task_source_type=task_pool` 指向 `tasks.id`。
3. 用户偏好第一版从哪里注入：memory 摘要、task_class 配置，还是先只消费已有排程约束？
   - 已确认：若候选目标来自 task 池，优先使用 memory 中的用户偏好；若候选目标来自 task_item，则使用所属 task_class 的硬性偏好和约束。
4. 近期用户反馈是否第一版只作为 trigger payload，不落数据库状态？
   - 已确认：用户反馈类触发信号需要持久化，但不面向前端展示；主要用于幂等、审计、排障和串联 trigger -> preview -> notification -> apply 链路。

### 5.3 待补执行计划

业务逻辑确认后补充：

1. `ActiveScheduleContext` 结构。
2. 任务、日程、偏好、反馈的读取入口。
3. 四象限刷新复用方案。
4. 时间窗转换与边界兜底。

## 6. 模块三：主动观测与候选生成

### 6.1 业务实现逻辑简述

主动观测能力参考 `analyze_health`：后端先做结构化观测，再生成候选，让 LLM 做选择题。

第一版候选限制为 1 到 3 个，动作范围包括：

1. 加入日程预览。
2. 未完成补救预览。
3. 后继挤压重排预览。
4. 延后结束询问。
5. 压缩融合预览。
6. 询问用户。
7. 仅提醒。
8. 收口。

### 6.2 需要拍板的问题

1. 主动观测最终是 Agent 工具，还是 worker 内部 service？第一版是否同时提供内部 service 和工具壳？
   - 已确认：主动观测不作为 ReAct 工具进入工具循环，而是串进固定 graph / service pipeline。LLM 直接消费观测与候选结果，负责选择和表达。
2. “重要且紧急任务未进入日程视图”的可用窗口查找，第一版是否允许打破 task_class 偏好？
   - 已纠正：task_pool 任务不属于 task_class，不存在 task_class 偏好可打破。第一版按用户 memory 偏好和滚动 24 小时内的可用时间生成候选；若 memory 偏好与可用容量冲突，候选中说明偏好未满足的代价，而不是称为“打破 task_class 偏好”。
3. 未完成补救里，局部重排第一版复用现有粗排算法到什么程度？
   - 已确认：第一版做“偏好软化版局部粗排”。输入时间窗为当前时刻到任务类结束日期，只传受影响的部分 item；周几偏好和时段偏好从硬约束降级为优先级，优先排偏好范围内，排不下再打破偏好追加进去，最后恢复这些任务的原有顺序语义。
   - 工程倾向：不直接污染现有粗排主函数，新增一条局部重排实现；底层时间格、可用槽位、冲突判断等公共能力优先抽公共层复用，避免复制第三份逻辑。
4. 压缩融合候选第一版是否固定只找“下一个动态任务”，并默认 50% / 50%？
   - 已确认：第一版固定只找下一个动态任务作为融合对象，并默认按 50% / 50% 压缩；该候选只作为兜底预览，不自动执行。
5. close / ask_user / notify_only 的判定阈值由后端固定，还是允许 LLM 结合上下文选择？
   - 已确认：参考 `analyze_health` 的裁决模式，由后端确定 `close / ask_user / notify_only / select_candidate`。LLM 不决定能不能调度，只在 `select_candidate` 时选择候选；其它场景只负责解释后端理由。

### 6.3 待补执行计划

业务逻辑确认后补充：

1. metrics / issues / decision / candidates schema。
2. 候选合法性校验规则。
3. 候选排序规则。
4. 与现有 `analyze_health` 的复用和隔离边界。
5. 单元测试覆盖场景。

## 7. 模块四：预览、前后对比与确认

### 7.1 业务实现逻辑简述

主动调度候选必须先写入待确认预览，让用户看到“为什么触发、改前是什么、改后是什么、风险是什么、不调整的后果是什么”。

确认粒度按候选项确认，不做整版黑盒确认。确认后才进入正式应用链路。

### 7.2 需要拍板的问题

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

### 7.3 待补执行计划

业务逻辑确认后补充：

1. 预览表或缓存结构。
2. `SchedulePreviewVersion` / `ActiveScheduleChangeItem` schema。
3. 查询预览 API。
4. 确认 API 或 resume 接入方案。
5. 幂等键与状态流转。

## 8. 模块五：正式应用链路

### 8.1 业务实现逻辑简述

主动调度模块不直接写正式日程。用户确认某个候选后，后端把候选转换为现有 service 能理解的正式应用请求。

应用成功后发布 `schedule.apply.succeeded`；失败则发布 `schedule.apply.failed`，并把失败原因写回预览状态。

### 8.2 需要拍板的问题

1. 从任务池任务加入日程时，正式写入目标是 `schedule_events(type=task, rel_id=tasks.id)`，还是先转为 `task_items`？
   - 已确认：不转为 `task_items`。正式写入 `schedule_events.type=task, task_source_type=task_pool, rel_id=tasks.id`，并写入对应 `schedules` 原子节次。
2. 未完成补救涉及已排任务移动时，是否第一版只支持生成新补做块，不支持直接移动原任务？
   - 已确认：第一版只支持生成新的补做块，不直接移动原已排任务。这样可以降低对既有 schedule / task_item 状态的扰动，后续再扩展移动原任务。
3. `schedule.apply.requested` 第一版是否需要 outbox 异步消费，还是确认接口内同步调用 service？
4. 应用幂等键用 `preview_id + candidate_id`，还是单独生成 `apply_id`？

### 8.3 待补执行计划

业务逻辑确认后补充：

1. 候选到正式请求的转换器。
2. 复用 `TaskClassService.BatchApplyPlans` 的条件。
3. task_pool 任务正式落库策略。
4. 应用失败回写方案。
5. 测试场景。

## 9. 模块六：通知触达与飞书边界

### 9.1 业务实现逻辑简述

飞书第一版只提醒用户回系统确认，不在飞书内应用日程、不标记完成、不做复杂 Agent Chat。

主动调度只发布 `notification.feishu.requested`，通知 handler/provider 负责具体投递。这样后续可以把 notification 拆成独立 Go module。

### 9.2 需要拍板的问题

1. 第一版飞书通知文案是否只需要固定模板？
2. 通知是否必须包含跳转链接？如果包含，Web 端预览详情 URL 规则是什么？
3. 通知幂等键是否按 `preview_id`，还是按 `user_id + trigger_type + time_window`？
4. 飞书 provider 第一版放在 backend worker 内，是否需要同步预留 `notification_records` 表？

### 9.3 待补执行计划

业务逻辑确认后补充：

1. `NotificationRequested` DTO。
2. 简版 provider 接口。
3. 飞书配置项。
4. 幂等与失败日志。
5. 后续迁出到 `backend/services/notification` 的边界。

## 10. 模块七：与微服务迁移的协作边界

### 10.1 业务实现逻辑简述

第二阶段开发必须避免阻塞微服务迁移。当前策略是：先在 `backend` 内按服务边界写清楚，等协议稳定后再迁出独立 module。

API、worker、active scheduler、notification、schedule apply 的边界必须从第一版就分清。

### 10.2 需要拍板的问题

1. 是否先完成 `api / worker / all` 启动边界拆分，再合入主动调度主链路？
2. 主动调度代码第一版放在 `backend/service/active_scheduler`，还是 `backend/active_scheduler`？
3. 事件契约是否提前放入 `backend/shared/events` 风格目录，即使当前还未多 module？
4. 第一版是否允许主动调度 service 直接依赖 DAO，还是通过现有 service 读取？

### 10.3 待补执行计划

业务逻辑确认后补充：

1. 目录结构。
2. 依赖注入关系。
3. API / worker 启动装配改动点。
4. 未来迁出 `active-scheduler` 的文件边界。

## 11. 建议讨论顺序

建议按以下顺序逐个讨论：

1. 任务池任务如何进入日程视图。
2. 预览与确认协议。
3. 主动观测候选 schema。
4. 触发事件与 worker 链路。
5. 正式应用链路。
6. 飞书通知边界。
7. 目录结构与迁移边界。

## 12. 本轮决策记录

后续每轮讨论完成后，在这里追加结论。

### 12.1 触发 job 机制

1. `task` 创建或更新时，若存在 `urgency_threshold_at`，则 upsert 一条对应的主动调度 job。
2. job 的触发时间统一取 `urgency_threshold_at`；主动调度不再自行维护 `deadline_at - X` 之类的额外阈值。
3. `task` 完成后，不物理删除 job，而是将仍未执行的 job 标记为 `canceled`，方便后续排查为什么没有触发。
4. `task` 更新 `deadline_at` 或 `urgency_threshold_at` 时，直接覆盖当前有效 job，并刷新 `updated_at`。
5. schedule 动态任务默认不写定时 job；计划时间过去后按 `assumed_completed` 推进，只有用户明确反馈未完成时才进入主动调度链路。

### 12.2 待继续讨论

1. `schedule.apply.requested` 第一版同步调用 service，还是进入 outbox 异步消费。
2. 应用幂等键使用 `preview_id + candidate_id`，还是单独生成 `apply_id`。
3. 飞书通知固定文案、跳转 URL、通知幂等键和 `notification_records` 是否第一版落表。

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

执行计划待补：需要评估迁移 SQL、模型字段、schedule 读取映射、task_pool apply 链路以及历史 `type=task` 数据的默认来源回填策略。

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

1. 压缩融合只作为局部重排和延后结束都不可用时的兜底候选。
2. 第一版固定选择“下一个动态任务”作为融合对象，不做跨多个后继任务的复杂搜索。
3. 第一版固定比例为 50% / 50%：
   - 未完成任务压缩到融合块的一半时间。
   - 下一个动态任务压缩到融合块的一半时间。
4. 压缩融合必须写清风险说明：两个任务都会被压缩，需要用户接受 rush 模式。
5. 压缩融合只生成预览，不允许后台自动执行。

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
   - `preview_changes`：候选准备做的改动，例如新增 task_pool 日程、移动 task_item、压缩融合预览。
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
2. 飞书通知只包含详情页链接，默认进入：
   ```text
   /active-schedule/previews/:preview_id
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
3. `select_candidate`：找到合法的加入日程 / 未完成补救 / 压缩融合候选。
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
