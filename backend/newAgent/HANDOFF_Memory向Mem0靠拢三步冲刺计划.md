# Memory 向 Mem0 靠拢三步冲刺计划（newAgent）

## 1. 一句话结论

当前 `memory` 已经具备了“可异步写入、可基础抽取、可基础检索、可注入 newAgent”的骨架，但距离真正有 Mem0 味道的记忆系统，还差三块核心能力：

1. 写入侧没有“先召回旧记忆，再做 `ADD/UPDATE/DELETE/NONE` 决策”的治理层。
2. 读侧没有把“硬约束优先、语义召回补充、结果去重、注入预算”做成稳定链路。
3. 系统层没有形成“可灰度、可解释、可清理、可回滚”的治理闭环。

因此建议按三步走推进，并严格遵守一个原则：

1. 每一轮只处理一个能力域。
2. 第一步只动写入决策层。
3. 第二步只动读链路与注入质量。
4. 第三步只动治理、清理、指标与切流收口。

---

## 2. 本文档给谁看

本文档面向三类读者：

1. 需要继续实现 `memory/newAgent` 的 agent。
2. 需要拆任务、排优先级的人。
3. 需要快速判断“本轮该改什么、不该改什么”的维护者。

本文档不是背景介绍文档，而是“可直接拿去拆工单和接力开发”的冲刺说明。

---

## 3. 当前现状与目标差距

### 3.1 当前已完成的部分

当前已经有的能力：

1. 聊天消息可通过 `outbox -> memory.extract.requested -> memory_jobs -> worker` 进入异步记忆链路。
2. Worker 可调用 LLM 做事实抽取，并通过 `NormalizeFacts` 做批内标准化和批内去重。
3. `memory_items / memory_jobs / memory_audit_logs / memory_user_settings` 四张核心表已经建立并接线。
4. `ReadService` 已可做基础查询与轻量排序。
5. `newAgent` 已通过 `injectMemoryContext` 把记忆写入 pinned block。
6. 用户设置、删除、审计已经具备基础治理能力。

### 3.2 当前离 Mem0 还差什么

最关键的差距如下：

| 能力 | 当前状态 | 与 Mem0 的差距 |
| --- | --- | --- |
| 异步入队 | 已完成 | 基本到位 |
| 抽取候选事实 | 已完成 | 缺少更强的抽取后治理 |
| 批内去重 | 已完成 | 仅限单批，不处理历史记忆 |
| 历史去重 | 未完成 | 需要按旧记忆召回后做决策 |
| `ADD/UPDATE/DELETE/NONE` 决策 | 未完成 | 这是最关键差距 |
| 语义召回 | 部分完成 | 接口有了，质量与稳定性未形成闭环 |
| 读侧去重 | 未完成 | 现在更多是展示层弱去重 |
| Prompt 注入 | 基础版已接 | 还没有类型分层与预算控制 |
| 管理治理 | 部分完成 | 还缺更新、恢复、历史清理、指标闭环 |
| 灰度/回滚 | 较弱 | 需要细粒度 feature flag 与分阶段切流 |

### 3.3 本次冲刺的目标定义

本轮不是要把项目做成完整 Mem0，也不是做图记忆或多 Provider 平台，而是要做到一个“Mem0-lite 可自信上线”的状态。满足以下条件，就可以认为基本靠近目标：

1. 相同或同义记忆不会无脑越写越多。
2. 用户纠正一条旧记忆时，系统更倾向于更新旧值，而不是新增一条冲突值。
3. 读侧能优先拿到“硬约束 + 偏好 + 当前话题相关事实”，而不是仅按最近更新时间胡乱注入。
4. Prompt 注入是稳定、可控、可解释的，而不是纯拼接。
5. 出问题时可以快速关掉某一层能力，而不是整条 memory 链路一起陪葬。

---

## 4. 设计原则与边界

### 4.1 每轮只处理一个能力域

为避免回归问题无法定位，本计划明确规定：

1. 第一步只处理“写入决策层”。
2. 第二步只处理“读取与注入层”。
3. 第三步只处理“治理、清理与切流层”。

禁止在同一轮里同时大改：

1. `memory` 写入逻辑。
2. `newAgent` 图节点结构。
3. WebSearch / 其他 RAG 语料。
4. 多个 prompt 体系。

### 4.2 保留旧实现，走并行迁移

整个冲刺必须遵守并行迁移策略：

1. 旧的“抽取后直接 `Create`”路径先保留。
2. 新的“决策后 ApplyAction”路径并行落地。
3. 用 feature flag 灰度切流。
4. 验证通过后，再决定是否删除旧路径。

### 4.3 不新增“memory 工具化”这条支线

本轮不建议把 `memory` 改成一个显式工具让 `newAgent` 主动调用，原因如下：

1. 当前 `pinned block` 已经接入主链路，切点稳定。
2. 本轮目标是让记忆“更准”，不是让图结构更复杂。
3. 若同时引入工具化调用，会把“写入决策层”和“图编排层”耦到一起。

因此本轮默认继续沿用：

1. `backend/memory/service/read_service.go`
2. `backend/service/agentsvc/agent_memory.go`
3. `pinned block` 注入

---

## 5. 三步走总览

| 步骤 | 只处理的能力域 | 核心目标 | 旧实现是否保留 |
| --- | --- | --- | --- |
| 第一步 | 写入决策层 | 把“抽取即新增”升级为“召回旧记忆 + 决策动作” | 保留 |
| 第二步 | 读链路与注入层 | 把“查到就拼”升级为“硬约束优先 + 语义补充 + 注入预算” | 保留 |
| 第三步 | 治理与切流层 | 把“能跑”升级为“可灰度、可观测、可清理、可回滚” | 收口 |

---

## 6. 第一步：先把写入侧做成 Mem0-lite

### 6.1 这一步解决什么问题

当前写入链路本质上还是：

`抽取 -> 标准化 -> 直接写 memory_items`

这会带来三个直接问题：

1. 历史同义记忆不会合并。
2. 用户纠正旧记忆时，系统更可能新增一条相反记忆。
3. `content_hash` 现在更多只是存了个字段，没有真正承担“历史治理”的职责。

第一步的目标是把写入链路升级为：

`抽取 -> 召回旧记忆候选 -> 临时 ID 映射 -> LLM 决策 -> ApplyAction`

### 6.2 本轮要落的能力

第一步必须落地以下能力：

1. 为每条新候选 fact 召回有限个旧记忆候选。
2. 用临时整数 ID 或候选序号喂给决策模型，避免模型直接编造真实 `memory_id`。
3. 让模型只输出结构化 JSON 决策：`ADD/UPDATE/DELETE/NONE`。
4. 后端严格校验决策合法性，再执行数据库动作。
5. `UPDATE/DELETE` 也必须补齐审计日志，而不是只有 `create/delete`。

### 6.3 推荐的文件落点

建议新增文件：

1. `backend/memory/model/decision.go`
   - 定义决策 DTO、候选旧记忆 DTO、ApplyAction DTO。
2. `backend/memory/orchestrator/llm_decision_orchestrator.go`
   - 负责“给定新 fact + 旧候选 -> 输出结构化动作决策”。
3. `backend/memory/utils/decision_id_map.go`
   - 负责“真实 memory_id <-> 临时决策 ID”的映射。
4. `backend/memory/utils/decision_validate.go`
   - 负责校验动作是否合法、目标 ID 是否存在、动作字段是否完整。
5. `backend/memory/worker/decision_flow.go`
   - 负责 worker 内的“候选召回 -> 决策 -> 动作执行编排”。
6. `backend/memory/worker/apply_actions.go`
   - 负责把 `ADD/UPDATE/DELETE/NONE` 落为数据库动作与审计。

建议修改文件：

1. `backend/memory/model/config.go`
2. `backend/memory/service/config_loader.go`
3. `backend/memory/repo/item_repo.go`
4. `backend/memory/worker/runner.go`
5. `backend/memory/utils/audit.go`

### 6.4 推荐新增配置

建议新增配置项，全部走 `memory` 命名空间：

1. `memory.decision.enabled`
   - 是否启用决策层。
2. `memory.decision.candidateTopK`
   - 每个新 fact 召回多少个旧记忆候选。
3. `memory.decision.fallbackMode`
   - 建议支持 `legacy_add` / `drop` 两种模式。
4. `memory.write.mode`
   - 建议支持 `legacy` / `decision` 两种模式。

建议默认值：

1. `memory.decision.enabled=false`
2. `memory.write.mode=legacy`
3. `memory.decision.candidateTopK=5`
4. `memory.decision.fallbackMode=legacy_add`

### 6.5 `ItemRepo` 需要补的能力

当前 `ItemRepo` 只有“查、建、删状态、刷访问时间、刷向量状态”，还不够支撑决策动作。第一步至少要补以下能力：

1. `FindDecisionCandidates(...)`
   - 按 `user_id + assistant_id + conversation_id + run_id + memory_type` 查候选。
   - 当 RAG 可用时，可优先用向量召回补候选。
2. `UpdateContentByID(...)`
   - 用于 `UPDATE`。
   - 至少要更新：`title/content/normalized_content/content_hash/confidence/importance/sensitivity_level/is_explicit/updated_at`。
3. `SoftDeleteByID(...)`
   - 用于决策型 `DELETE`。
4. `FindActiveByHash(...)`
   - 给兜底幂等或低成本重复检测预留接口。

注意：

1. 不要把这些逻辑继续堆进 `UpsertItems`。
2. `UpsertItems` 可以暂时保留给 legacy 路径使用。
3. 新路径应尽量使用显式动作函数，而不是一个“万能 Upsert”。

### 6.6 Worker 内推荐的执行顺序

对每个 job，建议执行以下顺序：

1. 先抽取新事实。
2. 对抽取结果做 `NormalizeFacts`。
3. 按用户设置过滤。
4. 若 `memory.decision.enabled=false`，直接走旧路径并返回。
5. 对每条新 fact 召回旧候选：
   - 先查强约束域内候选。
   - 若 `memory.rag.enabled=true`，再用 RAG 补充语义候选。
6. 对候选做临时 ID 映射。
7. 调 `LLMDecisionOrchestrator` 输出动作。
8. 后端校验动作合法性。
9. 执行动作：
   - `ADD`：创建 item + `create` audit
   - `UPDATE`：更新旧 item + `update` audit
   - `DELETE`：软删除旧 item + `delete` audit
   - `NONE`：只记日志，不动表
10. 根据动作决定是否做向量同步：
    - `ADD`：新增向量
    - `UPDATE`：重写向量
    - `DELETE`：删向量或打 pending 删除标记

### 6.7 决策 Prompt 的建议约束

决策 prompt 需要非常收敛，建议只允许模型做一件事：

1. 给定一条新 fact。
2. 给定少量旧候选。
3. 在 `ADD/UPDATE/DELETE/NONE` 中选一个动作。

不建议第一版就让模型：

1. 一次同时处理多条新 fact 与多条旧事实的复杂批量决策。
2. 自己生成复杂的替代文案策略。
3. 自己修改 scope 或元数据。

推荐第一版输出结构大致为：

```json
{
  "decisions": [
    {
      "candidate_index": 0,
      "action": "UPDATE",
      "target_temp_id": 2,
      "title": "更新后的标题",
      "content": "更新后的内容",
      "reason": "新事实是在纠正旧事实"
    }
  ]
}
```

### 6.8 这一步的验收标准

满足以下条件，可认为第一步完成：

1. 重复表达同一偏好，不会连续生成多条 `active` 记忆。
2. 用户显式纠正旧偏好时，会更倾向触发 `UPDATE`，而不是再新增一条冲突记忆。
3. `memory_audit_logs` 能明确区分 `create/update/delete`。
4. 决策层失败时，不会阻断原有 legacy 链路。
5. 关闭 `memory.decision.enabled` 后，系统行为可完全回到当前实现。

### 6.9 这一步的回滚点

第一步必须保留明确回滚点：

1. 关闭 `memory.decision.enabled`
2. `memory.write.mode` 切回 `legacy`

回滚后仍然使用：

1. `LLMWriteOrchestrator.ExtractFacts`
2. `NormalizeFacts`
3. `buildMemoryItems`
4. `ItemRepo.UpsertItems`

### 6.10 这一步明确不做什么

第一步不要顺手做以下事情：

1. 不重构 `newAgent` 图节点。
2. 不引入 memory 工具调用。
3. 不做图记忆。
4. 不做用户侧“编辑记忆内容”的管理 API。
5. 不同时改 WebSearch 的 RAG 链路。

---

## 7. 第二步：把读取与注入做成真正可用的记忆链路

### 7.1 这一步解决什么问题

写入侧即使更聪明，如果读出来的还是“按分数凑五条，再平铺给 prompt”，整体体验依然不会像 Mem0。

第二步要解决的问题是：

1. 硬约束和偏好不能被普通事实挤掉。
2. 历史重复项不能继续在读侧污染 TopK。
3. 注入给模型的文本需要可控，而不是简单平铺。
4. RAG 可用时要真正成为加分项，不可用时要稳定降级。

### 7.2 本轮要落的能力

第二步必须落地以下能力：

1. 读侧合并“结构化强约束召回”和“语义候选召回”。
2. 读侧在服务层做真正的去重，而不是只在渲染字符串时弱去重。
3. 注入文本按类型分组，而不是所有内容同一层级平铺。
4. 给每一类记忆设置注入预算，避免事实类把 prompt 撑爆。

### 7.3 推荐的文件落点

建议优先修改文件：

1. `backend/memory/service/read_service.go`
2. `backend/memory/repo/item_repo.go`
3. `backend/service/agentsvc/agent_memory.go`

如需补辅助文件，建议新增：

1. `backend/memory/service/retrieve_merge.go`
   - 负责多路召回的结果合并、去重、预算裁剪。
2. `backend/memory/service/retrieve_rank.go`
   - 负责重排与门控。
3. `backend/service/agentsvc/agent_memory_render.go`
   - 负责把 memory DTO 渲染成稳定的注入 block。

说明：

1. 当前 `agent_memory.go` 已经不算小。
2. 第二步不要继续往单文件里堆“召回策略 + 去重 + 渲染模板”。
3. 这一轮拆开渲染层是合理的职责拆分，不属于跨能力域大重构。

### 7.4 读取侧推荐的新流程

建议读侧升级为以下顺序：

1. 先从 MySQL 拉“必守约束”：
   - `constraint`
   - 高置信度 `preference`
2. 再按当前 query 做相关召回：
   - 若 `memory.rag.enabled=true`，优先走 RAG
   - 否则走 legacy DB 排序
3. 合并两路结果。
4. 先按 `memory_id` 去重。
5. 再按 `content_hash` 去重。
6. 最后才按渲染文本兜底去重。
7. 对结果做类型预算：
   - `constraint`：优先保留
   - `preference`：次优先
   - `todo_hint`：控制数量
   - `fact`：最容易膨胀，要严格限额

### 7.5 注入层推荐的渲染方式

当前渲染方式更像“扁平清单”。第二步建议升级成“分段注入”，例如：

1. 必守约束
2. 用户偏好
3. 当前话题相关事实
4. 近期线索

推荐生成类似文本：

```text
以下是与当前对话相关的用户记忆，仅在确实有帮助时参考，不要机械复述。

【必守约束】
- 用户点外卖不要香菜。

【用户偏好】
- 用户偏爱黑咖啡。

【当前话题相关事实】
- 用户最近在准备周四的程序设计作业。
```

这样做的好处：

1. 模型更容易区分“必须遵守”和“仅可参考”。
2. 日后更容易按类型做 budget。
3. 若发生错误注入，也更容易解释是哪一层出错。

### 7.6 第二步建议新增配置

建议新增：

1. `memory.read.mode`
   - 建议支持 `legacy` / `hybrid`
2. `memory.read.factLimit`
3. `memory.read.preferenceLimit`
4. `memory.read.constraintLimit`
5. `memory.inject.renderMode`
   - 建议支持 `flat` / `typed_v2`

建议默认值：

1. `memory.read.mode=legacy`
2. `memory.inject.renderMode=flat`

灰度时再逐步切到：

1. `memory.read.mode=hybrid`
2. `memory.inject.renderMode=typed_v2`

### 7.7 这一步的验收标准

满足以下条件，可认为第二步完成：

1. 同一条重复记忆即使数据库里有多条，最终注入给 prompt 也只保留一条。
2. `constraint` 类记忆不会轻易被 `fact` 类挤出注入集合。
3. RAG 异常时，系统仍能稳定退回 legacy 读取逻辑。
4. 注入文本结构清晰，且总长度稳定，不会一轮长一轮短。
5. newAgent 的 `pinned block` 内容更可读、更可解释。

### 7.8 这一步的回滚点

第二步必须支持快速回滚：

1. `memory.read.mode=legacy`
2. `memory.inject.renderMode=flat`
3. `memory.rag.enabled=false`

回滚后保留：

1. 旧的 `ReadService.retrieveByLegacy`
2. 当前 `agent_memory.go` 扁平渲染逻辑

### 7.9 这一步明确不做什么

第二步不要顺手做以下事情：

1. 不把 memory 改造成工具调用。
2. 不改 `newAgent` 的图路由结构。
3. 不把 WebSearch 一起并进统一召回。
4. 不在这一轮清理历史重复脏数据。

---

## 8. 第三步：做治理、清理、指标与切流收口

### 8.1 这一步解决什么问题

前两步做完后，系统可能“效果已经不错”，但仍缺三个上线必须项：

1. 出问题时怎么知道错在哪一层。
2. 历史已经写进去的重复脏数据怎么治理。
3. 什么时候能把 legacy 路径关掉。

第三步就是收口这一层。

### 8.2 本轮要落的能力

第三步建议至少做以下能力：

1. 为写入决策、读取召回、注入渲染补齐结构化日志和指标。
2. 增加历史重复清理能力。
3. 补齐 `update/restore` 等审计语义。
4. 明确 feature flag 切流策略与回滚手册。
5. 更新文档，避免后续维护者只看到旧 README。

### 8.3 推荐的文件落点

建议修改文件：

1. `backend/memory/utils/audit.go`
2. `backend/memory/service/manage_service.go`
3. `backend/memory/repo/item_repo.go`
4. `backend/memory/README.md`
5. `backend/memory/记忆模块实施计划.md`

建议新增文件：

1. `backend/memory/cleanup/dedup_runner.go`
   - 用于历史重复治理。
2. `backend/memory/cleanup/dedup_policy.go`
   - 负责定义“保留哪条、归档哪条”。
3. `backend/memory/observe/log_fields.go`
   - 统一日志字段，避免不同文件各写各的。

### 8.4 历史数据清理建议

建议不要直接写危险 SQL 一把梭清表，而是通过可审计的治理流程清理历史脏数据：

1. 按 `user_id + memory_type + content_hash + status=active` 扫描重复组。
2. 为每组挑一个保留主记录：
   - 优先保留最近更新
   - 或优先保留置信度更高
3. 其余重复项改为 `archived` 或 `deleted`。
4. 对每次治理动作写审计日志。

建议第一版优先做“离线治理工具”或“手动触发 job”，不要直接绑到主 worker 周期任务里。

### 8.5 建议补的指标

第三步建议至少打这些指标：

1. `memory_job_success_rate`
2. `memory_job_retry_rate`
3. `memory_decision_distribution`
4. `memory_decision_fallback_rate`
5. `memory_retrieve_hit_count`
6. `memory_retrieve_dedup_drop_count`
7. `memory_inject_item_count`
8. `memory_rag_fallback_rate`
9. `memory_wrong_mention_rate`
10. `memory_user_correction_rate`

其中前八项可以本轮先落，后两项可通过后续用户纠正入口接入。

### 8.6 建议的切流顺序

第三步不要“一刀切”。建议按以下顺序灰度：

1. 阶段 A：决策层 shadow 模式
   - 真正写库仍走 legacy
   - 新决策层只做日志，不生效
2. 阶段 B：决策层仅对显式记忆生效
3. 阶段 C：决策层对全部写入生效
4. 阶段 D：读侧切到 hybrid
5. 阶段 E：注入切到 typed_v2
6. 阶段 F：历史清理跑完，再考虑关闭 legacy 默认路径

### 8.7 这一步的验收标准

满足以下条件，可认为第三步完成：

1. 能从日志看清某条记忆为何被 `ADD/UPDATE/DELETE/NONE`。
2. 能从指标看清读侧命中、去重、降级、回滚情况。
3. 能对历史重复数据做可审计清理。
4. 出现异常时可在分钟级通过开关退回 legacy。
5. 文档与代码现状一致，不再依赖口头传递。

### 8.8 这一步的回滚点

第三步的回滚不应影响前两步代码保留，只需回切开关：

1. 决策层回到 `legacy`
2. 读侧回到 `legacy`
3. 注入渲染回到 `flat`
4. 停掉清理任务

### 8.9 这一步明确不做什么

第三步仍然不建议同时做以下事情：

1. 不做图记忆。
2. 不做多 Provider 工厂化。
3. 不拆独立 memory 服务。
4. 不把 WebSearch 与 Memory 强行合并到同一轮上线。

---

## 9. 推荐的三轮交付顺序

如果资源有限，建议严格按下面顺序推进：

1. 先做第一步。
   - 原因：写侧如果还是“抽取即新增”，读侧再怎么优化也会越来越脏。
2. 再做第二步。
   - 原因：写侧稳定后，读侧才能真正体现效果。
3. 最后做第三步。
   - 原因：治理、指标、清理要建立在前两步能力已经基本成形的前提下。

一句话总结：

1. 先让系统“会整理记忆”。
2. 再让系统“会正确读记忆”。
3. 最后让系统“可稳定上线和维护”。

---

## 10. 建议的任务拆分方式

如果后续要多人并行，建议按职责边界拆，而不是按文件随意拆：

### 10.1 第一步可拆为两块

1. 决策模型与编排
   - `decision.go`
   - `llm_decision_orchestrator.go`
   - `decision_validate.go`
2. Repo 与动作执行
   - `item_repo.go`
   - `apply_actions.go`
   - `audit.go`

### 10.2 第二步可拆为两块

1. 读侧召回与合并
   - `read_service.go`
   - `retrieve_merge.go`
   - `retrieve_rank.go`
2. newAgent 注入渲染
   - `agent_memory.go`
   - `agent_memory_render.go`

### 10.3 第三步可拆为两块

1. 治理与清理
   - `dedup_runner.go`
   - `manage_service.go`
2. 观测与文档
   - 指标日志
   - README / 计划文档更新

---

## 11. 如果只看一个结论，请看这里

要让当前 memory 真正靠近 Mem0，不是再加一张表，也不是再加一个 prompt，而是要完成以下收敛：

1. 写入侧从“抽到就加”升级为“先回看旧记忆，再决定加改删不做”。
2. 读侧从“查到就拼”升级为“硬约束优先、语义补充、结果去重、预算注入”。
3. 系统侧从“能跑”升级为“有灰度、有指标、有清理、有回滚”。

只要三步按这个顺序推进，最终得到的就不是一个“会不断积灰的记忆表”，而是一套真正能为 `newAgent` 服务的记忆系统。
