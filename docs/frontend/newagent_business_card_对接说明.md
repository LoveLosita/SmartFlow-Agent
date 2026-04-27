# NewAgent 业务卡片前后端对接说明

## 1. 文档目标

本文用于约定 NewAgent 聊天时间线中的“业务结果卡片”协议，供前端先行实现卡片容器与渲染逻辑，后端后续按同一标准补齐事件发射。

本次只覆盖两类卡片：

1. 查询任务卡片
2. 任务记录卡片

其中“任务记录卡片”统一承载以下两个入口语义：

- 随口记
- 创建任务

这样做的原因是：两者最终落到前端展示时，表达的都是“系统中新增了一条任务/提醒”，如果硬拆成两套完全独立协议，会造成字段重复、渲染重复和样式分叉。

## 2. 适用范围

本说明只约定聊天时间线中的结构化卡片事件，不重做整套聊天 UI，也不影响现有：

- `assistant_text`
- `tool_call`
- `tool_result`
- `confirm_request`
- `schedule_completed`

现阶段建议复用“像 schedule_completed 一样通过 extra 事件驱动前端卡片”的模式，但不复用 `schedule_completed` 这个具体 kind。

## 3. 总体设计原则

### 3.1 业务卡片走独立事件

业务卡片不应伪装成：

- `status`
- `tool_result`
- `schedule_completed`

原因如下：

1. `status` 更适合阶段提示，不适合承载稳定业务结果。
2. `tool_result` 更适合工具过程回执，不适合承载用户真正关心的结果实体。
3. `schedule_completed` 是“排程完成信号卡”，语义过窄，不适合继续复用到任务域。

因此，本次建议新增统一事件类型：

- `business_card`

### 3.2 统一入口，卡片内再分类型

后端统一发：

- `kind = business_card`
- `display_mode = card`

再在 payload 中细分：

- `card_type = task_query`
- `card_type = task_record`

其中：

- `task_query` 表示“查到了什么”
- `task_record` 表示“刚刚记下/创建了什么”

### 3.3 尽量直接携带结果快照

本批业务卡片不建议完全照搬 schedule 的“只发信号、前端二次补拉”模式，而应优先直接携带卡片渲染所需的最小结果快照。

原因如下：

1. 查询任务结果是“本轮对话当时查到的内容”，若前端二次补拉，结果可能已变化。
2. 随口记 / 创建任务通常只需要 1 条新增结果，直接随事件下发最稳。
3. 业务卡字段本身不重，没有必要为一张小卡额外走一轮查询接口。

因此，本次推荐：

1. 查询任务卡：直接下发查询条件摘要 + 命中列表快照
2. 任务记录卡：直接下发新建任务摘要

## 4. 事件协议

## 4.1 Timeline kind 扩展

前端 `TimelineEvent.kind` 建议新增：

```ts
type TimelineEventKind =
  | 'user_text'
  | 'assistant_text'
  | 'tool_call'
  | 'tool_result'
  | 'confirm_request'
  | 'schedule_completed'
  | 'interrupt'
  | 'status'
  | 'business_card'
```

## 4.2 payload 扩展建议

建议在现有 `payload` 中显式新增 `business_card` 字段，不建议长期把正式协议塞进 `meta`。

推荐结构：

```ts
type BusinessCardType = 'task_query' | 'task_record'

type TaskRecordSource = 'quick_note' | 'create_task'

interface TimelineBusinessCardPayload {
  card_type: BusinessCardType
  title?: string
  summary?: string
  source?: TaskRecordSource
  data: TaskQueryCardData | TaskRecordCardData
}
```

对应地，`TimelineEvent.payload` 可扩为：

```ts
interface TimelineEventPayload {
  reasoning_content?: string
  stage?: string
  block_id?: string
  display_mode?: 'card'
  tool?: TimelineToolPayload
  confirm?: TimelineConfirmPayload
  business_card?: TimelineBusinessCardPayload
}
```

## 4.3 SSE extra 扩展建议

后端流式 extra 建议新增：

```json
{
  "kind": "business_card",
  "block_id": "quick_task.result",
  "stage": "quick_task",
  "display_mode": "card",
  "business_card": {
    "card_type": "task_query",
    "title": "找到 4 条未完成任务",
    "summary": "按截止时间升序",
    "data": {}
  }
}
```

建议后端在 `OpenAIChunkExtra` 中显式新增：

- `BusinessCard *StreamBusinessCardExtra`

而不是长期挂入 `Meta`。

原因：

1. 业务卡已经被确认会长期存在，不再是灰度字段。
2. 前端渲染会高频依赖这些字段，显式类型更稳。
3. 时间线持久化也更容易做结构校验。

## 5. 两类卡片的数据结构

## 5.1 查询任务卡片

### 5.1.1 目标

用于展示“本轮查询任务时实际查到了哪些任务”，重点是结果快照，而不是工具过程。

### 5.1.2 推荐数据结构

```ts
interface TaskQueryCardTaskItem {
  id: number
  title: string
  priority_group?: number
  priority_label?: string
  deadline_at?: string
  is_completed?: boolean
}

type TaskQueryFilterOperator = 'eq' | 'contains' | 'gte' | 'lt'

interface TaskQueryCardFilter {
  key:
    | 'quadrant'
    | 'keyword'
    | 'deadline_after'
    | 'deadline_before'
    | 'include_completed'
    | 'sort'
  label: string
  value: string | number | boolean
  operator?: TaskQueryFilterOperator
  display_text: string
}

interface TaskQueryCardData {
  // 展示摘要：只适合整段展示，不作为前端切分协议。
  query_summary?: string
  // 稳定结构化筛选条件：前端若要渲染标签/chip，应优先消费此字段。
  query_filters?: TaskQueryCardFilter[]
  result_count: number
  shown_count: number
  has_more?: boolean
  tasks: TaskQueryCardTaskItem[]
}
```

### 5.1.3 最小字段集

最少需要：

- `card_type = task_query`
- `title`
- `data.result_count`
- `data.tasks`

其中 `tasks` 每项最少建议包含：

- `id`
- `title`

### 5.1.4 增强字段

有条件时建议补充：

- `query_summary`
- `query_filters`
- `priority_label`
- `deadline_at`
- `is_completed`
- `shown_count`
- `has_more`

其中 `query_summary` 是给人看的整段摘要，不保证分隔符可解析；前端若要拆成标签，应使用 `query_filters[].display_text` 或根据 `key/operator/value` 自行格式化，禁止按 `；` 切分 `query_summary`。

### 5.1.5 降级规则

1. 若只有 `result_count` 无任务列表：
   - 前端仍可渲染简版统计卡，不展示列表区。
2. 若任务项缺少 `priority_label` / `deadline_at`：
   - 隐藏对应字段行，不展示占位符。
3. 若 `result_count = 0`：
   - 渲染空结果态卡片，而不是回退纯文本。

### 5.1.6 示例

```json
{
  "kind": "business_card",
  "payload": {
    "stage": "quick_task",
    "block_id": "quick_task.result",
    "display_mode": "card",
    "business_card": {
      "card_type": "task_query",
      "title": "找到 4 条未完成任务",
      "summary": "按截止时间升序",
      "data": {
        "query_summary": "关键词：离散数学；仅未完成；截止时间升序",
        "query_filters": [
          {
            "key": "keyword",
            "label": "关键词",
            "value": "离散数学",
            "operator": "contains",
            "display_text": "关键词：离散数学"
          },
          {
            "key": "include_completed",
            "label": "完成状态",
            "value": false,
            "operator": "eq",
            "display_text": "仅未完成"
          },
          {
            "key": "sort",
            "label": "排序",
            "value": "deadline_asc",
            "operator": "eq",
            "display_text": "按截止时间升序"
          }
        ],
        "result_count": 4,
        "shown_count": 3,
        "has_more": true,
        "tasks": [
          {
            "id": 101,
            "title": "离散数学作业 3",
            "priority_group": 2,
            "priority_label": "重要不紧急",
            "deadline_at": "2026-04-29 21:00",
            "is_completed": false
          },
          {
            "id": 105,
            "title": "离散数学命题证明复习",
            "priority_group": 2,
            "priority_label": "重要不紧急",
            "deadline_at": "2026-05-01 18:00",
            "is_completed": false
          },
          {
            "id": 108,
            "title": "离散数学错题整理",
            "priority_group": 3,
            "priority_label": "普通任务",
            "deadline_at": "",
            "is_completed": false
          }
        ]
      }
    }
  }
}
```

## 5.2 任务记录卡片

### 5.2.1 目标

用于展示“刚刚记下/创建出的那条任务结果”，统一承载：

- 随口记
- 创建任务

### 5.2.2 推荐数据结构

```ts
interface TaskRecordCardData {
  id?: number
  title: string
  priority_group?: number
  priority_label?: string
  deadline_at?: string
  urgency_threshold_at?: string
  status?: string
  created_at?: string
}
```

卡片额外语义通过外层字段区分：

```ts
interface TimelineBusinessCardPayload {
  card_type: 'task_record'
  title?: string
  summary?: string
  source?: 'quick_note' | 'create_task'
  data: TaskRecordCardData
}
```

### 5.2.3 最小字段集

最少需要：

- `card_type = task_record`
- `source`
- `data.title`

### 5.2.4 增强字段

有条件时建议补充：

- `id`
- `priority_label`
- `deadline_at`
- `urgency_threshold_at`
- `status`
- `created_at`
- `summary`

### 5.2.5 source 语义约定

#### `source = quick_note`

表示这条记录来自“随口记”入口。

前端展示建议：

1. 头部弱化“正式创建”措辞。
2. 更强调“已帮你记下”。
3. 若字段较少，只展示标题和轻量标签即可成立。

#### `source = create_task`

表示这条记录来自“明确创建任务”入口。

前端展示建议：

1. 头部可用更正式的“任务已创建”表达。
2. 更适合展示 deadline / priority / status 等结构化信息。

### 5.2.6 降级规则

1. 若只有 `title`：
   - 仍渲染最简任务记录卡。
2. 若没有 `deadline_at`：
   - 不展示“无截止时间”字样，直接隐藏该字段区。
3. 若没有 `priority_label`：
   - 不展示优先级标签，避免为了填满 UI 硬造信息。

### 5.2.7 示例：随口记

```json
{
  "kind": "business_card",
  "payload": {
    "stage": "quick_task",
    "block_id": "quick_task.result",
    "display_mode": "card",
    "business_card": {
      "card_type": "task_record",
      "title": "已帮你记下",
      "summary": "一条轻量提醒已写入任务系统",
      "source": "quick_note",
      "data": {
        "id": 301,
        "title": "周三晚上给导师发周报",
        "priority_group": 2,
        "priority_label": "重要不紧急",
        "deadline_at": "2026-04-29 20:00",
        "created_at": "2026-04-27 16:10:00"
      }
    }
  }
}
```

### 5.2.8 示例：创建任务

```json
{
  "kind": "business_card",
  "payload": {
    "stage": "execute",
    "block_id": "execute.result",
    "display_mode": "card",
    "business_card": {
      "card_type": "task_record",
      "title": "任务已创建",
      "summary": "已写入任务系统",
      "source": "create_task",
      "data": {
        "id": 405,
        "title": "完成离散数学第 1 节复习",
        "priority_group": 1,
        "priority_label": "重要紧急",
        "deadline_at": "2026-04-28 22:00",
        "status": "todo",
        "created_at": "2026-04-27 16:12:00"
      }
    }
  }
}
```

## 6. 前端对接要求

## 6.1 时间线模型扩展

前端建议扩展：

1. `TimelineEvent.kind` 增加 `business_card`
2. `payload.business_card` 增加强类型
3. `DisplayAssistantBlock.type` 增加 `business_card`

建议结构：

```ts
type DisplayAssistantBlockType =
  | 'tool'
  | 'status'
  | 'reasoning'
  | 'content'
  | 'content_indicator'
  | 'schedule_card'
  | 'business_card'
```

## 6.2 组件拆分建议

建议前端按“两层组件”实现：

### 第一层：统一容器

- `BusinessCardRenderer.vue`

职责：

1. 根据 `card_type` 分发子组件
2. 兜底空态 / 未知类型
3. 统一外边距、动画、时间线嵌入样式

### 第二层：两张业务卡

- `TaskQueryResultCard.vue`
- `TaskRecordCard.vue`

这样可以保持：

1. 外层时间线接入统一
2. 卡片内部样式独立演进
3. 后续若新增其他业务卡，只需继续扩展 renderer

## 6.3 渲染策略

### 查询任务卡

建议展示：

1. 头部标题
2. 查询摘要
3. 命中数量
4. 任务列表（建议最多展示 3~5 条）
5. 若有更多结果，展示“还有 N 条”

### 任务记录卡

建议展示：

1. 头部标题
2. 来源标签：`随口记生成` / `已创建任务`
3. 任务标题
4. 优先级 / 截止时间等辅助信息

## 6.4 未知字段兼容

前端渲染必须允许以下情况存在：

1. 后端只返回最小字段集
2. 某些增强字段为空
3. 不同入口产生的字段丰富度不同

因此前端实现原则是：

- 只消费拿得到的字段
- 不对缺失字段报错
- 不渲染“空标签”“空时间”“--”

## 7. 后端对接要求

## 7.1 发射位置建议

考虑到当前 `node` 目录正在整理，本轮先定协议，不要求立刻改动节点实现。后端后续落地时，建议在以下业务完成点发射：

### 查询任务卡

建议在“查询任务成功且拿到最终结果快照”后发射。

候选位置：

- `quick_task` 查询成功路径
- 后续若有独立查询任务工具域，也应在最终结果汇总后发射

### 任务记录卡

建议在“写入任务系统成功并拿到任务结果”后发射。

候选位置：

- `quick_task` create 成功路径，对应 `source = quick_note`
- 正式任务创建成功路径，对应 `source = create_task`

## 7.2 发射时机约束

业务卡片必须满足以下约束：

1. 只在业务真实成功后发射
2. 不能在参数未齐、等待确认、仅计划阶段时提前发射
3. 不能把“工具调用开始”误当成“业务结果卡”

换句话说：

- `tool_call/tool_result` 负责过程
- `business_card` 负责结果

## 7.3 与纯文本回复的关系

业务卡片不是纯文本回复的替代物，而是补充物。

建议后端保持：

1. 正常 assistant speak 继续输出
2. 业务卡片作为同轮时间线中的独立 block 插入

这样用户既能看到自然语言结果，也能看到结构化回执。

### 7.3.1 默认范式：短正文 + 结果卡

本次明确约定：业务卡片默认采用“短正文 + 结果卡”的组合范式，不采用“用卡片替换 LLM 正文”的方案。

推荐理解如下：

1. LLM 正文负责自然语言衔接、解释和收口。
2. 业务卡片负责结构化结果展示。
3. 两者是互补关系，不是替代关系。

推荐表现形态：

- 查询任务：
  - 正文示例：`我找到 4 条相关任务，先给你列重点。`
  - 后接：查询任务卡片
- 随口记：
  - 正文示例：`我帮你记下来了。`
  - 后接：任务记录卡片
- 创建任务：
  - 正文示例：`这条任务已经创建好了。`
  - 后接：任务记录卡片

### 7.3.2 为什么不采用“卡片替换正文”

不建议让节点直接吞掉 LLM 原本准备输出的正文，只保留卡片，原因如下：

1. 自然语言回复承担上下文衔接作用，直接去掉后，聊天感会突然中断。
2. 卡片负责结果快照，正文负责语气和解释，两者职责不同。
3. “替换正文”会引入额外分支判断，容易再次出现“该显示的被吞掉 / 不该显示的被露出”的问题。

因此，本次协议层明确规定：

- `business_card` 是正文补充，不是正文替代。
- 前端收到 `business_card` 时，不应主动隐藏同轮 `assistant_text`。
- 后端发出 `business_card` 时，也不应把它当作“正文已无需输出”的信号。

### 7.3.3 正文长度约束

虽然保留正文，但在存在业务卡片的场景下，正文应尽量短，不要把卡片里已经结构化展示的内容再用长段文字完整复述一遍。

建议约束：

1. 正文以一句或两句为宜。
2. 正文只表达结论、态度或过渡，不重复列出完整字段。
3. 任务标题、时间、优先级、命中列表等细节尽量交给卡片承载。

换句话说，本次推荐的最终交互形态是：

- 先给一句自然语言反馈
- 再给结构化业务卡片

而不是：

- 大段正文完整复述一遍
- 再来一张内容几乎重复的卡片

### 7.3.4 顺序建议

如果同一轮既有正文又有业务卡片，推荐前端按以下顺序展示：

1. `assistant_text`
2. `business_card`

原因：

1. 更符合自然阅读流：先“听结果”，再“看详情”。
2. 能保持聊天节奏，不会让卡片突兀抢到正文前面。
3. 与当前 `assistant_text + 结构化卡片` 的时间线模型一致。

### 7.3.5 卡片位置约束

本次进一步明确：业务卡片应当紧跟在“与之对应的那段 assistant 正文”后面，而不是拖到整轮消息流的绝对结尾再统一补发。

推荐顺序：

1. 业务成功
2. 输出一句简短 `assistant_text`
3. 立即输出对应的 `business_card`
4. 本轮结束，或仅保留极短的必要收尾

不推荐顺序：

1. 先输出结果正文
2. 中间再插入其他阶段提示、补充说明或收尾文案
3. 最后才在整轮末尾补一张业务卡片

这样做的问题是：

1. 卡片和对应正文的语义绑定会变弱。
2. 用户会误以为卡片是在回应更后面的内容，而不是前面的那句结果。
3. 卡片会更像“附录”或“补充材料”，而不是本轮结果的结构化主回执。

因此，前后端统一按以下口径理解：

- 业务卡片不是“整轮结束彩蛋”
- 业务卡片是“对应正文的紧随结果块”

也就是说，位置上应理解为：

- 紧跟对应消息后面
- 而不是放在整轮会话的绝对结尾

### 7.3.6 对后端发射时机的直接要求

后端后续补发 `business_card` 时，应尽量保证：

1. 卡片事件在对应 `assistant_text` 之后立即进入时间线。
2. 卡片事件之后不要再接大段重复解释。
3. 若确实需要补一句收尾，也应控制在极短长度内，避免把卡片重新推离它所服务的正文。

前端在渲染时，也不应为了“统一收口”而把业务卡片重新移动到该轮消息的最末尾。

## 8. 时间线持久化要求

若当前时间线已经会持久化 `tool_call`、`tool_result`、`confirm_request`、`schedule_completed`，则 `business_card` 也应进入同一条时间线持久化链路。

要求：

1. 刷新页面后能恢复卡片
2. 渲染顺序仍以 `seq` 为准
3. 不能只在 SSE 在线期间可见

## 9. 推荐落地顺序

考虑到当前 execute/node 还在精简，推荐顺序如下：

### 第一步：前端先落承载层

1. 扩展 timeline 类型
2. 扩展 `business_card` payload 类型
3. 新增 `BusinessCardRenderer.vue`
4. 新增 `TaskQueryResultCard.vue`
5. 新增 `TaskRecordCard.vue`
6. 在 `AssistantPanel.vue` 接入渲染分支

### 第二步：后端补协议结构

1. 在 stream extra 中新增 `business_card`
2. 在 timeline 持久化 DTO 中补 `business_card`
3. 保证刷新后可恢复

### 第三步：后端补业务发射点

1. quick task 查询成功 -> 发 `task_query`
2. quick note 创建成功 -> 发 `task_record(source=quick_note)`
3. 正式创建任务成功 -> 发 `task_record(source=create_task)`

## 10. 本次明确不做的事

本说明暂不覆盖：

1. 修改工具调用卡片协议
2. 修改确认卡片协议
3. 把业务卡片统一改为前端二次补拉
4. 把 `随口记` 和 `创建任务` 再拆成两套完全独立事件类型

## 11. 最终结论

本次业务卡片推荐采用以下标准：

1. 新增统一时间线事件：`business_card`
2. 卡片类型只保留两类：
   - `task_query`
   - `task_record`
3. `task_record` 用 `source` 区分：
   - `quick_note`
   - `create_task`
4. 卡片优先直接携带结果快照，不走“仅发信号、前端再查一次”的默认模式
5. 前端可先按本文把渲染层做完，后端后续按同一协议补发事件
