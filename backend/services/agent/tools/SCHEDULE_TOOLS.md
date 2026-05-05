# 日程工具设计文档

> 本文档定义了 agent 日程调度场景下的工具层设计。
> 工具是 LLM 与日程数据之间的唯一边界——LLM 只能通过工具的输入/输出与日程交互，永远不直接接触原始数据。

---

## 1. 设计原则

1. **工具即边界**：LLM 通过工具感知和修改日程，不直接接触 state 或数据库
2. **自然语言返回**：工具返回值为自然语言 + 轻结构（缩进、列表），LLM 直接理解
3. **只报事实，不做判断**：读工具只报当前真实状态，不附建议/推荐/假设；写工具只报变更后的事实
4. **操作前自动校验**：写工具在执行前自动检测冲突和锁定，失败时 state 不变
5. **State 内操作**：所有写工具只修改内存中的 state，不直接写库；整个方案完成后由 Confirm 节点统一写库

---

## 2. 索引体系

LLM 不接触真实的日期和星期，使用两级整数索引：

- **天索引（day）**：规划窗口内的天数编号，从 1 开始连续递增
  - 例如：规划窗口为第1周周三至第3周周一，共13天，编号为第1天~第13天
  - 工具层负责 day ↔ 真实日期 的映射
  - 规划窗口由 Plan 节点向用户确认，不明确时走 ask_user

- **时段索引（slot）**：每天内的节课编号，范围 1-12
  - 标准节次：1-2, 3-4, 5-6, 7-8, 9-10, 11-12（共6个标准段）
  - 连堂课可能跨越：1-3（3连堂）、1-4（4连堂）、9-12（4连堂）等
  - 任务时段用 (slot_start, slot_end) 表示，例如 (1, 4) = 第1-4节

- **任务定位**：day + slot_start + slot_end，例如 (3, 1, 4) = 第3天第1-4节

---

## 3. State 数据结构

State 是工具层的操作对象，存在于内存中，不直接暴露给 LLM。

### 3.1 整体结构

```json
{
  "window": {
    "total_days": 13,
    "day_mapping": [
      { "day_index": 1,  "week": 5, "day_of_week": 1 },
      { "day_index": 2,  "week": 5, "day_of_week": 2 },
      { "day_index": 3,  "week": 5, "day_of_week": 3 },
      { "day_index": 4,  "week": 5, "day_of_week": 4 },
      { "day_index": 5,  "week": 5, "day_of_week": 5 },
      { "day_index": 6,  "week": 5, "day_of_week": 6 },
      { "day_index": 7,  "week": 5, "day_of_week": 7 },
      { "day_index": 8,  "week": 6, "day_of_week": 1 },
      { "day_index": 9,  "week": 6, "day_of_week": 2 },
      { "day_index": 10, "week": 6, "day_of_week": 3 },
      { "day_index": 11, "week": 6, "day_of_week": 4 },
      { "day_index": 12, "week": 6, "day_of_week": 5 },
      { "day_index": 13, "week": 6, "day_of_week": 6 }
    ]
  },
  "tasks": [
    {
      "state_id": 1,
      "source": "event",
      "source_id": 101,
      "name": "高等数学",
      "category": "课程",
      "status": "existing",
      "locked": true,
      "slots": [
        { "day": 1, "slot_start": 1, "slot_end": 2 },
        { "day": 4, "slot_start": 1, "slot_end": 2 },
        { "day": 8, "slot_start": 1, "slot_end": 2 }
      ]
    },
    {
      "state_id": 2,
      "source": "event",
      "source_id": 102,
      "name": "思政（水课）",
      "category": "课程",
      "status": "existing",
      "locked": false,
      "can_embed": true,
      "slots": [
        { "day": 2, "slot_start": 1, "slot_end": 2 }
      ]
    },
    {
      "state_id": 3,
      "source": "task_item",
      "source_id": 201,
      "name": "复习线代",
      "category": "学习",
      "status": "pending",
      "duration": 3,
      "category_id": 10
    }
  ]
}
```

### 3.2 字段说明

**任务通用字段：**

| 字段 | 类型 | 说明 |
|------|------|------|
| `state_id` | int | State 内唯一 ID（递增），工具层和 LLM 使用此 ID 交互 |
| `source` | string | 数据来源：`"event"` = 来自 ScheduleEvent，`"task_item"` = 来自 TaskClassItem |
| `source_id` | int | 原表主键（ScheduleEvent.ID 或 TaskClassItem.ID），写库时用于反查 |
| `name` | string | 任务名称，来自 ScheduleEvent.Name 或 TaskClassItem.Content |
| `category` | string | 类别名，来自 TaskClass.Name（如"课程"、"学习"、"作业"） |
| `status` | string | `"existing"`（已安排/已确定）| `"suggested"`（已预排/可优化）| `"pending"`（待安排）|
| `locked` | bool | 是否锁定。推导规则：ScheduleEvent.Type="course" 且 CanBeEmbed=false 时为 true |
| `slots` | array | 已安排任务的时段列表，每项含 day/slot_start/slot_end |
| `duration` | int | 待安排/已预排任务需要的连续时段数（pending / suggested 任务常见） |
| `category_id` | int | 所属 TaskClass 的 ID（仅 source=task_item 时有值） |

**嵌入任务相关字段（仅 can_embed=true 的任务）：**

| 字段 | 类型 | 说明 |
|------|------|------|
| `can_embed` | bool | 该时段是否允许嵌入其他任务，来自 ScheduleEvent.CanBeEmbedded |
| `embedded_by` | int | 被哪个 state_id 的任务嵌入（宿主视角） |
| `embed_host` | int | 嵌入到哪个 state_id 的时段里（嵌入任务视角） |

### 3.3 数据来源与映射

**existing 任务（从数据库加载）：**

| State 字段 | 数据库来源 |
|-----------|-----------|
| source_id | ScheduleEvent.ID |
| name | ScheduleEvent.Name |
| category | ScheduleEvent.Type（"course"→"课程"，"task"→取关联 TaskClass.Name） |
| locked | ScheduleEvent.Type="course" 且 CanBeEmbedded=false |
| can_embed | ScheduleEvent.CanBeEmbedded |
| slots | 查 Schedule 表（WHERE event_id=? AND week/day_of_week IN 窗口范围），按 section 连续段压缩 |

**pending 任务（从数据库加载）：**

| State 字段 | 数据库来源 |
|-----------|-----------|
| source_id | TaskClassItem.ID |
| name | TaskClassItem.Content |
| category | 关联 TaskClass.Name（通过 CategoryID） |
| duration | 由 TaskClass.TotalSlots / Item 数量推算，或固定为 2 |
| category_id | TaskClassItem.CategoryID |

### 3.4 Section 压缩/解压

数据库中 Schedule 表逐节存储（每节一条记录），State 中压缩为连续范围：

```
DB 记录：
  Schedule(event_id=101, week=5, day_of_week=1, section=1)
  Schedule(event_id=101, week=5, day_of_week=1, section=2)

压缩为 State：
  { "day": 1, "slot_start": 1, "slot_end": 2 }
```

反向操作（写库时）：将 slot_start/slot_end 展开为逐条 Schedule 记录插入。

### 3.5 Day 映射

工具层通过 day_mapping 数组完成 day_index ↔ (week, day_of_week) 的双向转换：

- **读操作**：从 Schedule 表查到 (week=5, day_of_week=1, section=1)，通过 day_mapping 反查 day_index=1
- **写操作**：LLM 指定 day=3，通过 day_mapping 查到 (week=5, day_of_week=3)，用于构造 Schedule 记录
- **规划窗口**：由 Plan 节点确认范围，工具层初始化时生成 day_mapping

---

## 4. 读工具

### 4.1 get_overview

获取规划窗口总览（任务视角，全量返回）。

行为约束：
- 保留课程占位统计（例如“第1天：占2/12”），避免误判可用空间。
- 每日明细只展开任务（非课程），课程不进入任务明细列表。
- 在当前阶段（窗口通常不超过 30 天）直接全量返回，不做截断。

**入参：** 无

**返回示例：**

```
规划窗口共13天，每天12个时段，总计156个时段。
当前已占用48个，空闲108个。课程占位条目7个（仅用于占位统计）；任务条目：已安排(existing)1个、已预排(suggested)2个、待安排(pending)3个。

每日概况：
第1天：总占6/12（课程占6/12，任务占0/12） — 任务：无
第2天：总占2/12（课程占2/12，任务占0/12） — 任务：无
第3天：总占2/12（课程占0/12，任务占2/12） — 任务：[35]第一章随机事件与概率(suggested,第5-6节)
第4天：总占4/12（课程占2/12，任务占2/12） — 任务：[36]第二章随机变量(suggested,第7-8节)
...

任务清单（全量，已过滤课程）：
[35]第一章随机事件与概率 | 状态:suggested | 类别:概率论 | 时段:第3天(5-6节)
[36]第二章随机变量 | 状态:suggested | 类别:概率论 | 时段:第4天(7-8节)
[37]第三章多维随机变量 | 状态:pending | 类别:概率论 | 需2个连续时段
```

---

### 4.2 query_range

查看某天（或某天某段）的细粒度占用详情。

**入参：**

| 字段 | 类型 | 必填 | 说明 |
|------|------|------|------|
| day | int | 是 | 天索引 |
| slot_start | int | 否 | 起始节次，不传则返回整天 |
| slot_end | int | 否 | 结束节次，不传则返回整天 |

**返回示例（查整天）：**

```
第4天 全天：

第1-2节：[1]高等数学（固定）
第3-4节：[6]线代
第5-6节：空
第7-8节：空
第9-10节：[8]程序设计
第11-12节：空

连续空闲区：第5-8节(4时段)、第11-12节(2时段)
可嵌入：第1-2节已有[1]高等数学（固定，不可嵌入）
```

**返回示例（查具体范围）：**

```
第4天 第5-8节：

第5节：空
第6节：空
第7节：空
第8节：空

该范围4个时段全部空闲。
```

---

### 4.3 query_available_slots

查询候选坑位池（结构化返回）：默认先返回“纯空位”，不足时再补“可嵌入位”。

**入参：**

| 字段 | 类型 | 必填 | 说明 |
|------|------|------|------|
| span / duration | int | 否 | 目标连续时段长度，默认 2 |
| limit | int | 否 | 返回候选上限，默认 12 |
| allow_embed | bool | 否 | 是否允许补可嵌入位，默认 true |
| day / day_start / day_end | int | 否 | 天级范围过滤（`day` 与区间互斥） |
| day_scope | string | 否 | `all` / `workday` / `weekend` |
| day_of_week | []int | 否 | 星期过滤（1-7） |
| week / week_filter / week_from / week_to | int / []int | 否 | 周级过滤 |
| slot_type / slot_types | string / []string | 否 | `pure/empty/strict` 会强制只返回纯空位 |
| exclude_sections | []int | 否 | 排除节次（1-12） |
| after_section / before_section | int | 否 | 只返回区间之后/之前的候选 |
| section_from + section_to | int | 否 | 精确节次区间查询（需同时提供） |

**返回示例：**

```json
{
  "tool": "query_available_slots",
  "count": 12,
  "strict_count": 8,
  "embedded_count": 4,
  "fallback_used": true,
  "day_scope": "all",
  "day_of_week": [],
  "week_filter": [12],
  "week_from": 12,
  "week_to": 12,
  "span": 2,
  "allow_embed": true,
  "exclude_sections": [],
  "slots": [
    {
      "day": 5,
      "week": 12,
      "day_of_week": 3,
      "slot_start": 1,
      "slot_end": 2,
      "slot_type": "empty"
    }
  ]
}
```

---

### 4.4 list_tasks

列出任务清单，可按类别和状态过滤。

**入参：**

| 字段 | 类型 | 必填 | 说明 |
|------|------|------|------|
| category | string | 否 | 过滤类别（对应 TaskClass.Name，如"课程"、"学习"；不支持 task_class_ids 列表） |
| status | string | 否 | existing / suggested / pending / all，默认 all（仅支持单值，不支持 `existing,suggested` 这类拼接） |

**返回示例（待安排）：**

```
待安排任务共3个：

[3]复习线代 — 需3个连续时段，类别：学习
[7]写实验报告 — 需2个连续时段，类别：作业
[9]小组讨论 — 需2个连续时段，类别：学习
```

**返回示例（全部）：**

```
共9个任务，已安排6个，待安排3个。

已安排：
  [1]高等数学(课程,固定) — 第1天(1-2节) 第4天(1-2节) 第8天(1-2节)
  [2]英语(课程) — 第1天(3-4节) 第6天(1-2节)
  [4]体育(课程) — 第1天(5-6节)
  [5]物理(课程) — 第2天(3-4节) 第8天(3-4节)
  [6]线代(学习) — 第4天(3-4节)
  [8]程序设计(课程) — 第4天(9-10节)
  [10]思政(课程,可嵌入) — 第7天(1-2节)

待安排：
  [3]复习线代(学习) — 需3时段
  [7]写实验报告(作业) — 需2时段
  [9]小组讨论(学习) — 需2时段
```

---

### 4.5 get_task_info

查询单个任务的详细信息。

**入参：**

| 字段 | 类型 | 必填 | 说明 |
|------|------|------|------|
| task_id | int | 是 | 任务 ID |

**返回示例（普通任务）：**

```
[1]高等数学
类别：课程 | 状态：已安排（固定）
来源：课程表
占用时段：
  第1天 第1-2节
  第4天 第1-2节
  第8天 第1-2节
```

**返回示例（可嵌入任务）：**

```
[10]思政
类别：课程 | 状态：已安排
来源：课程表
可嵌入：是（允许在此时段嵌入其他任务）
占用时段：
  第7天 第1-2节
当前嵌入任务：无
```

---

## 5. 写工具

### 5.1 place

将待安排任务预排到指定位置。

**入参：**

| 字段 | 类型 | 必填 | 说明 |
|------|------|------|------|
| task_id | int | 是 | 待安排任务的 ID |
| day | int | 是 | 目标天索引 |
| slot_start | int | 是 | 目标起始节次 |

**成功返回：**

```
已将 [3]复习线代 放到第5天第1-3节。
第5天当前占用：[3]复习线代(1-3节)，占用3/12。
待安排任务剩余：2个。
```

**失败返回（冲突）：**

```
放置失败：第5天第1-2节已被 [4]体育 占用。
第5天当前占用：[4]体育(1-4节)，占用4/12。空闲时段：第5-12节。
```

**失败返回（状态错误）：**

```
放置失败：[1]高等数学 不是待安排任务，无法放置。
```

**成功返回（嵌入到水课）：**

```
已将 [7]写实验报告 嵌入到第7天第1-2节（宿主：[10]思政）。
第7天当前占用：[10]思政(1-2节) [7]写实验报告(嵌入1-2节)，占用2/12。
待安排任务剩余：2个。
```

---

### 5.2 move

移动已预排任务（仅 suggested）到新位置。

**入参：**

| 字段 | 类型 | 必填 | 说明 |
|------|------|------|------|
| task_id | int | 是 | 任务 ID |
| new_day | int | 是 | 目标天索引 |
| new_slot_start | int | 是 | 目标起始节次 |

**成功返回：**

```
已将 [6]线代 从第4天第3-4节移至第9天第1-2节。
第4天当前占用：[1]高等数学(1-2节) [8]程序设计(9-10节)，占用4/12。
第9天当前占用：[6]线代(1-2节)，占用2/12。
```

**失败返回（冲突）：**

```
移动失败：第9天第1-2节已被 [9]小组讨论 占用。
第9天当前占用：[9]小组讨论(1-2节)，占用2/12。空闲时段：第3-12节。
```

**失败返回（锁定）：**

```
移动失败：[1]高等数学 是固定课程，不可移动。
```

**失败返回（状态错误）：**

```
移动失败：[3]复习线代 当前为待安排状态，请使用 place 放置。
```

```
移动失败：[2]英语 当前为已安排（existing）任务，不允许 move；仅 suggested 任务可移动。
```

---

### 5.3 swap

交换两个已落位任务的位置。

**入参：**

| 字段 | 类型 | 必填 | 说明 |
|------|------|------|------|
| task_a | int | 是 | 任务 A 的 ID |
| task_b | int | 是 | 任务 B 的 ID |

**成功返回：**

```
交换完成：
  [2]英语：第1天第3-4节 → 第6天第1-2节
  [6]线代：第6天第1-2节 → 第1天第3-4节
第1天当前占用：[1]高等数学(1-2节) [6]线代(3-4节) [4]体育(5-6节)，占用6/12。
第6天当前占用：[2]英语(1-2节)，占用2/12。
```

**失败返回（时长不匹配）：**

```
交换失败：[5]物理 占4个时段，[2]英语 占2个时段，时长不同无法直接交换。
```

**失败返回（任一任务锁定）：**

```
交换失败：[1]高等数学 是固定课程，不可交换。
```

---

### 5.4 batch_move

批量原子移动多个任务（仅 suggested，**单次最多 2 条**），要么全部成功，要么全部回滚。

**入参：**

| 字段 | 类型 | 必填 | 说明 |
|------|------|------|------|
| moves | array | 是 | 每项包含 task_id, new_day, new_slot_start |

**成功返回：**

```
批量移动完成，2个任务全部成功：
  [2]英语 → 第3天第1-2节
  [6]线代 → 第5天第3-4节
第3天当前占用：[2]英语(1-2节)，占用2/12。
第5天当前占用：[6]线代(3-4节)，占用2/12。
```

**失败返回（超出上限）：**

```
批量移动失败：当前最多支持 2 条移动请求。请改用队列化逐项处理（queue_pop_head + queue_apply_head_move）。
```

**失败返回：**

```
批量移动失败，全部回滚，无任何变更。
冲突：[6]线代 → 第5天第3-4节，该位置已被 [3]复习线代(1-3节) 占用。
```

---

### 5.5 unplace

将已落位任务恢复为待安排状态。

**入参：**

| 字段 | 类型 | 必填 | 说明 |
|------|------|------|------|
| task_id | int | 是 | 任务 ID |

**成功返回：**

```
已将 [3]复习线代 从第5天第1-3节移除，恢复为待安排状态。
第5天当前占用：0/12。
待安排任务剩余：1个。
```

**失败返回（锁定）：**

```
移除失败：[1]高等数学 是固定课程，不可移除。
```

---

### 5.6 min_context_switch

在给定任务集合内重排 suggested 任务，尽量把同类任务排成连续块，以减少上下文切换。

使用约束：
- 仅在用户明确说明“允许打乱顺序”时调用。
- 仅支持 suggested 且已落位任务。
- 工具只在传入集合内部重排，不会主动改动集合外任务。

**入参：**

| 字段 | 类型 | 必填 | 说明 |
|------|------|------|------|
| task_ids | array[int] | 是 | 参与重排的任务 ID 列表（至少 2 个） |
| task_id | int | 否 | 兼容单值参数，不建议新调用使用 |

**成功返回：**

```
最少上下文切换重排完成：共处理 6 个任务，上下文切换次数 5 -> 2。
本次调整：
  [35]概率第一章：第3天(星期3)第1-2节 -> 第2天(星期2)第5-6节
  [41]概率第二章：第4天(星期4)第1-2节 -> 第3天(星期3)第1-2节
第2天当前占用：...
第3天当前占用：...
第4天当前占用：...
```

**失败返回（未授权顺序重排时应由上层拦截）：**

```
已拒绝执行 min_context_switch：当前未授权打乱顺序。如需使用该工具，请先由用户明确说明“允许打乱顺序”。
```

---

### 5.7 queue_pop_head

弹出并返回当前队首任务；若已有 current 则复用，保证一次只处理一个任务。

**入参：**

无

**返回示例：**

```json
{"tool":"queue_pop_head","has_head":true,"pending_count":5,"current":{"task_id":35,"name":"示例任务","status":"suggested","slots":[{"day":3,"week":12,"day_of_week":1,"slot_start":5,"slot_end":6}]}}
```

---

### 5.8 queue_apply_head_move

将当前队首任务移动到指定位置并自动出队。仅作用于 current，不接受 task_id。

**入参：**

| 字段 | 类型 | 必填 | 说明 |
|------|------|------|------|
| new_day | int | 是 | 目标 day |
| new_slot_start | int | 是 | 目标起始节次 |

**返回示例：**

```json
{"tool":"queue_apply_head_move","success":true,"task_id":35,"pending_count":4,"completed_count":2,"result":"已将 [35]... 从第3天第5-6节移至第5天第3-4节。"}
```

---

### 5.9 queue_skip_head

跳过当前队首任务（不改日程），标记为 skipped 并继续后续队列。

**入参：**

| 字段 | 类型 | 必填 | 说明 |
|------|------|------|------|
| reason | string | 否 | 跳过原因 |

**返回示例：**

```json
{"tool":"queue_skip_head","success":true,"skipped_task_id":35,"pending_count":4,"skipped_count":1}
```

---

### 5.10 queue_status

查看当前待处理队列状态（pending/current/completed/skipped）。

**入参：**

无

**返回示例：**

```json
{"tool":"queue_status","pending_count":5,"completed_count":1,"skipped_count":0,"current_task_id":35,"current_attempt":1}
```

---

### 5.11 spread_even

在给定任务集合内执行“均匀化铺开”：  
先按筛选条件收集候选坑位，再用确定性规划器生成移动方案并原子提交。

**入参：**

| 字段 | 类型 | 必填 | 说明 |
|------|------|------|------|
| task_ids | array[int] | 是 | 参与均匀化的任务 ID 列表（至少 2 个） |
| task_id | int | 否 | 兼容单值参数，不建议新调用使用 |
| day/day_start/day_end | int | 否 | 天级范围过滤 |
| day_scope | string | 否 | `all` / `workday` / `weekend` |
| day_of_week | array[int] | 否 | 星期过滤（1~7） |
| week/week_filter/week_from/week_to | int/array[int] | 否 | 周级过滤 |
| limit | int | 否 | 每个跨度的候选坑位上限（内部会按任务数自动放大） |
| allow_embed | bool | 否 | 是否允许补充可嵌入位，默认 true |
| exclude_sections | array[int] | 否 | 排除节次 |
| after_section/before_section | int | 否 | 节次边界过滤 |

**成功返回：**

```
均匀化调整完成：共处理 6 个任务，候选坑位 24 个。
本次调整：
  [35]第一章复习：第3天(星期3)第5-6节 -> 第5天(星期5)第1-2节
  [41]第二章练习：第4天(星期4)第5-6节 -> 第6天(星期6)第1-2节
第5天当前占用：...
第6天当前占用：...
```

**失败返回（候选不足）：**

```
均匀化调整失败：跨度=2 可用坑位不足：required=4, got=2。
```

---

## 6. 公共规则

### 冲突检测
- 所有写操作执行前自动检测目标位置是否冲突
- 冲突时拒绝操作，返回冲突任务名称和占用节次
- state 保持不变

### 锁定保护
- locked=true 的任务，move / swap / unplace 直接拒绝
- place 新任务到锁定时段同样拒绝

### 状态约束
- pending 任务只能 place，不能 move / swap / unplace
- suggested 任务可以 move / swap / unplace / spread_even / min_context_switch
- existing 任务不能 move / batch_move / spread_even / min_context_switch（仅作已安排事实层）
- 状态不符时返回明确错误信息

### 返回格式
- 返回值为自然语言 + 轻结构（缩进、列表）
- 占用信息始终附带每个任务的具体节次范围
- 读工具只报当前真实状态，不做假设
- 写工具只报变更后的事实，不附建议

### ID 规范
- LLM 可见的任务 ID 为 `state_id`（递增整数），不暴露 source/source_id
- `state_id` 由工具层在加载 state 时分配，不区分来源
- `source` + `source_id` 为内部字段，仅在写库时使用，不对 LLM 可见

### 嵌入任务规则
- `can_embed=true` 的任务（水课）允许其他任务嵌入到同一时段
- 嵌入任务占位时不触发冲突检测（与宿主共存）
- `query_available_slots` 返回候选坑位池（先纯空位，必要时补可嵌入位）
- `place` 到可嵌入时段时，若已有宿主任务，自动标记 embed_host 关系
- 嵌入任务的 locked 继承宿主：宿主不可移动时，嵌入任务也不可单独移动

### 数据库交互
- State 初始化：从 Schedule + ScheduleEvent 加载 existing 任务，从 TaskClassItem 加载 pending 任务；粗排或工具预排成功后，任务转为 suggested
- State 落库：Confirm 节点统一处理，将 state 变更转换为 Schedule/ScheduleEvent/TaskClassItem 的增删改
- 落库时使用 source + source_id 定位原记录，使用 day_mapping 将 day_index 转回 (week, day_of_week)
- 落库时将 (slot_start, slot_end) 展开为逐条 Schedule 记录
