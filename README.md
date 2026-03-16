# 1 项目概览

## 1.1 总体介绍

专门面向大学生/高执行力人群设计的日程平台，并且接入对话式AI，实现了AI的功能调用，实现在和AI对话的过程中灵活编排日程。

除了大规模编排任务的场景，本项目还包括日程提醒、AI驱动的"小事随口记"等功能，做到：**一个平台，包揽生活大小事。**

本项目采用前后端分离设计，并且有一个**完整的工业化设计链路**：写功能-墨刀画页面-根据页面写接口-写后端-写前端(AI)。

## 1.2 项目解决的痛点

> **问题1：** 传统的日程平台(类似于滴答清单等)要么设置重复任务(例如每日背单词、每日刷题等)，要么就需要手动设置任务，非常不方便。一旦遇到需要大规模安排任务的场景，例如期末考前半个月突击(涉及的科目多，手头的空余时间也多，每门课的任务又都不一样)，就需要先手动规划好任务，再手指点个不停给它排进日程软件里面，还得思考怎样排比较合理，一旦执行出问题需要调整，又像多米纳骨牌倒了一样，连着调整一大块。

**本项目带来的解决方案1：** 采用类似于老师备课-教务处排课的"备课-排课"模式。即假如你是学校上课的老师，你先在"备课区"把这门课程的"教学"大纲排好，然后再考虑后面安排上课的事情。

拿概率论举例子，你准备给它16节课时间复习，那你就在新增任务类的区域**创建新任务类**，并设置好在第x**任务块**看xx章节的速成课，然后第x任务块是真题练习。

设置完了之后，再通过我们的排课功能(会先设计一个保证能排的算法，第二批次开发还会考虑AI介入让课排的更好)，你设置一些排课的倾向(更倾向于在哪个时间点学、不想在哪个时间点学、想每天均匀推进还是快速突击等等)，点一下**智能一键编排**按钮，会将课直接以黄色打底的形式嵌入日程中，确认无误后你点击正式应用日程，课才会真正被排进去。

至于调整，本项目支持在课表区域直接拖拽调整时间。

> **问题2：** 期末周没课，确实可以按照上面一样操作。那我如果不是期末复习呢，我如果想安排一些别的事情呢，比如推进项目？我平时可是有课的，而且有不少水课。传统的日程软件可没法在水课处排课，要么忘记去上水课，要么忘记任务，十分恼火。

**本项目带来的解决方案2：** 本项目支持学校课表导入，甚至还支持水课嵌入任务。在导入课表之后，本项目支持勾选某些课程为"**可嵌入任务**"状态，此时就可以配合上面的排课系统，将水课作为可用的区域，排任务进去。

> **问题3：** 那么我作为一个规划能力比较差的懒人，也能用这个项目来让自己变的充实吗？

**本项目带来的解决方案3：** 当然可以，这就是本项目接入AI的意义。聊天区域的AI将会被调教成一个日程安排的小助手，既能满足你简单的对日程的增删改查，又能协助你从0开始一点点制定属于你的计划。(**第一批次开发计划**只支持AI随口记这一"增"的功能，以及大多数能想到的"查"功能，暂时无让AI改和删的想法)

> **问题4：** 我平时会突然冒出来一个能让自己活的更舒服亦或是变得更好的小想法(例如把桌面理一下、给自己挑一件新衣服等)，但是现在很忙，根本没时间做，然后等忙完了有时间了又忘记了。传统的日程软件确实能让我记录下来(比如将这个小想法记录在日程软件的四象限里面的"不重要不紧急"象限)，就是太麻烦了。
>
> 还有，平时上课时，接踵而至的实验报告、小组作业等，也面临着类似的情况，既容易忘记，又懒得记录。

**本项目带来的解决方案4：** 本项目支持AI驱动的"随口记"功能。

你可以和本项目的AI助手说："提醒我**有空的时候**给自己挑一件新衣服"(**请注意标粗的关键词**)，AI助手就会自动评估这件小事的难度以及执行所需花费的时间：如果这件事很简单或者不费时，会被加入"简单不重要"的队列中；如果比较费时或者困难，就会被加入"不简单不重要"队列中。

至于突发任务，也支持使用该"随口记"功能。你可以这么说："提醒我**下周周日之前**完成xx课程的大作业"(**请注意标粗的关键词**)，AI就会自动通过**截止时间和任务量**判断是否紧急，选择将其加入"重要并紧急"或者"重要不紧急"任务队列中(这里也是借鉴了四象限设计)。并且，系统会**每经过一个固定时间，就自动调整两个任务队列中未完成任务的位置**(比如：随着DDL临近，将重要不紧急队列中的未完成任务挪到重要并紧急队列中)。

当你在空闲时(做完你的大主线之后)，亦或是休息时间打开本项目，一眼就能看到这几个队列的事情，然后你就可以看心情选择做哪个，然后做完之后一划就完事。

## 1.3 项目实现的功能

1. **对用户目前时间尺度的适应。**

   如果用户是正在上学的大学生，时间尺度可以设置为以学校排课为主，通用时间为辅(以第1-2节，第3-4节这种的学校排课的时间方式为主，又能兼容某个特定时间的突发小事，做到对总体执行效率和事情安排效率的兼顾)；

   如果用户既想要自定义时间，又想要一键编排任务，本项目还支持用户自定义时间尺度，例如设置9:00-11:00为第一节课等。

2. **导入学校课表。** 如果用户选择以学校排课为主的时间尺度，本项目支持快速导入学校课表(只会尝试兼容CQUPT的课表格式)，以便后续以课表为基底的日程安排。

3. **"水课"任务嵌入。** 正如上方**问题2**所言，在导入课表后，支持设置某一门你想拿来干其它事情的课为"可嵌入任务"状态，此时这门课所占据的时间区域就是可以嵌入任务的了，但是仍然有区别于其它完全空白的时间区域，便于真正安排适合在嘈杂环境下做的事情。

4. **设置某一任务类，并提前安排其执行路线。** 正如上方**问题1**所言，用户可以先设置一个大的任务类(例如概率论复习、算法进阶计划等等)，再在这个任务类下方安排其在对应时间尺度下的执行计划(例如第1-2节干啥，第3-4节干啥)，方便后续的日程编排。

5. **一键编排任务。** 结合算法、用户偏好以及AI的建议，将任务基于上方的时间尺度、导入的课表，排进日程中，并给出这样排的理由(如果动用了AI)。  

6. **AI随口记。** 正如问题4所言，就是支持通过AI随手记录一些大小事。

7. **多用户。** 本系统可支持多个用户同时使用，并且记录AI对话、编排任务的Token使用情况等，并进行限额。

8. **动态任务和静态任务。**   **动态任务**包括学校的课和排入日程中的任务类，这些任务随着时间往后会默认已经完成，无需手动勾选；

   而**静态任务**为四个任务队列中的任务，这些任务需要手动勾选为完成状态。

9. **完成任务状态的撤回。** 无论是因为哪种情况，是误触给队列里面的任务打钩，还是水课翘课了被叫回去点名导致任务中断，都支持**撤回**这个"任务已完成"的状态。

   前者，用户只需要在队列的下沉列表中找到该任务然后点击一下灰色的勾即可(模仿了滴答清单的设计)。

   至于后者，由于后者为动态任务，所以用户需要手动去"最近已完成任务"的清单里面选择该任务然后恢复，此时任务会自动回到未安排状态。目前暂不支持课程的撤回，课程方面的改动目前仅支持删除，其它操作后续考虑开发。

# 2 产品逻辑与设计

## 2.1 业务流程图



## 2.2 原型展示

![登录页](./docs/design/Pics/登录页.png)

![平台首页_已登录](./docs/design/Pics/平台首页_已登录.png)

![日程查看&安排中心 多选后](./docs/design/Pics/日程查看&安排中心_多选后.png)

![日程查看&安排中心 展开数据结构并排进去一个任务后](./docs/design/Pics/日程查看&安排中心_展开数据结构并排进去一个任务后.png)

![日程查看&安排中心](./docs/design/Pics/日程查看&安排中心.png)

![用户设置&杂项](./docs/design/Pics/用户设置&杂项.png)

![注册页](./docs/design/Pics/注册页.png)

# 3 后端数据架构

## 3.1 ER图

PS：此图截至版本v0.3.3

![DB_ER_Design](./docs/pics/DB_ER_Design.png)

## 3.2 核心表结构

其实每个表都很核心。在此展示它们的创建语句：

```sql
CREATE TABLE `agent_chats`
(
    `id`              int       NOT NULL AUTO_INCREMENT,
    `user_id`         int            DEFAULT NULL,
    `message_content` text COMMENT '用户或AI的话',
    `role`            varchar(255)   DEFAULT NULL COMMENT 'user / assistant',
    `tokens_consumed` int            DEFAULT '0' COMMENT '单次消耗，用于累加到 users 表',
    `created_at`      timestamp NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (`id`),
    UNIQUE KEY `uk_agent_chats_id` (`id`),
    KEY `user_id` (`user_id`),
    CONSTRAINT `agent_chats_ibfk_1` FOREIGN KEY (`user_id`) REFERENCES `users` (`id`)
) ENGINE = InnoDB
  DEFAULT CHARSET = utf8mb4
  COLLATE = utf8mb4_0900_ai_ci
  
CREATE TABLE `courses`
(
    `id`        int          NOT NULL AUTO_INCREMENT,
    `user_id`   int          DEFAULT NULL,
    `name`      varchar(255) NOT NULL,
    `location`  varchar(255) DEFAULT NULL,
    `is_filler` tinyint(1)   DEFAULT '0',
    PRIMARY KEY (`id`),
    UNIQUE KEY `uk_courses_id` (`id`),
    KEY `user_id` (`user_id`),
    CONSTRAINT `courses_ibfk_1` FOREIGN KEY (`user_id`) REFERENCES `users` (`id`)
) ENGINE = InnoDB
  DEFAULT CHARSET = utf8mb4
  COLLATE = utf8mb4_0900_ai_ci
  
CREATE TABLE `schedule_events`
(
    `id`              int                    NOT NULL AUTO_INCREMENT,
    `user_id`         int                    NOT NULL,
    `name`            varchar(255)           NOT NULL COMMENT '课程或任务名称',
    `location`        varchar(255)                    DEFAULT '' COMMENT '地点 (教学楼/会议室)',
    `type`            enum ('course','task') NOT NULL COMMENT '日程类型',
    `rel_id`          int                             DEFAULT NULL COMMENT '关联原始数据ID (如教务系统的课程ID)',
    `can_be_embedded` tinyint(1)             NOT NULL DEFAULT '0' COMMENT '是否允许在此时段嵌入其他任务',
    `created_at`      timestamp              NULL     DEFAULT CURRENT_TIMESTAMP,
    `updated_at`      timestamp              NULL     DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
    `start_time`      datetime                        DEFAULT NULL COMMENT '任务开始的绝对时间',
    `end_time`        datetime                        DEFAULT NULL COMMENT '任务结束的绝对时间',
    PRIMARY KEY (`id`),
    KEY `idx_user_events` (`user_id`),
    KEY `idx_user_endtime` (`user_id`, `end_time` DESC),
    CONSTRAINT `fk_event_user` FOREIGN KEY (`user_id`) REFERENCES `users` (`id`) ON DELETE CASCADE
) ENGINE = InnoDB
  AUTO_INCREMENT = 148
  DEFAULT CHARSET = utf8mb4
  COLLATE = utf8mb4_0900_ai_ci
  
CREATE TABLE `schedules`
(
    `id`               int NOT NULL AUTO_INCREMENT,
    `event_id`         int NOT NULL COMMENT '关联元数据ID',
    `user_id`          int NOT NULL COMMENT '冗余UID方便直接查询',
    `week`             int NOT NULL COMMENT '周次 (1-25)',
    `day_of_week`      int NOT NULL COMMENT '星期 (1-7)',
    `section`          int NOT NULL COMMENT '原子化节次 (1-12)',
    `embedded_task_id` int                           DEFAULT NULL COMMENT '若为水课嵌入，记录具体的任务项ID',
    `status`           enum ('normal','interrupted') DEFAULT 'normal' COMMENT '状态: 正常/因故中断',
    PRIMARY KEY (`id`),
    UNIQUE KEY `idx_user_slot_atomic` (`user_id`, `week`, `day_of_week`, `section`),
    KEY `idx_event_id` (`event_id`),
    KEY `fk_embedded_task` (`embedded_task_id`),
    CONSTRAINT `fk_embedded_task` FOREIGN KEY (`embedded_task_id`) REFERENCES `task_items` (`id`) ON DELETE SET NULL,
    CONSTRAINT `fk_schedule_event` FOREIGN KEY (`event_id`) REFERENCES `schedule_events` (`id`) ON DELETE CASCADE,
    CONSTRAINT `fk_schedule_user` FOREIGN KEY (`user_id`) REFERENCES `users` (`id`) ON DELETE CASCADE
) ENGINE = InnoDB
  AUTO_INCREMENT = 214
  DEFAULT CHARSET = utf8mb4
  COLLATE = utf8mb4_0900_ai_ci
  
CREATE TABLE `task_classes`
(
    `id`                  int NOT NULL AUTO_INCREMENT,
    `user_id`             int                     DEFAULT NULL,
    `name`                varchar(255)            DEFAULT NULL,
    `mode`                enum ('auto','manual')  DEFAULT NULL,
    `start_date`          date                    DEFAULT NULL,
    `end_date`            date                    DEFAULT NULL,
    `total_slots`         int                     DEFAULT NULL COMMENT '分配的总节数',
    `allow_filler_course` tinyint(1)              DEFAULT '1',
    `strategy`            enum ('steady','rapid') DEFAULT NULL,
    `excluded_slots`      json                    DEFAULT NULL COMMENT '不想要的时段切片',
    PRIMARY KEY (`id`),
    UNIQUE KEY `uk_task_classes_id` (`id`),
    KEY `idx_task_classes_user_id` (`user_id`),
    CONSTRAINT `task_classes_ibfk_1` FOREIGN KEY (`user_id`) REFERENCES `users` (`id`)
) ENGINE = InnoDB
  AUTO_INCREMENT = 15
  DEFAULT CHARSET = utf8mb4
  COLLATE = utf8mb4_0900_ai_ci
  
CREATE TABLE `task_items`
(
    `id`            int NOT NULL AUTO_INCREMENT,
    `category_id`   int  DEFAULT NULL,
    `content`       text,
    `embedded_time` json DEFAULT NULL COMMENT '目标时间{date,section_from,section_to}',
    `status`        int  DEFAULT NULL COMMENT '1:未安排, 2:已应用',
    `order`         int  DEFAULT NULL,
    PRIMARY KEY (`id`),
    UNIQUE KEY `uk_task_items_id` (`id`),
    KEY `task_items_ibfk_1` (`category_id`),
    CONSTRAINT `task_items_ibfk_1` FOREIGN KEY (`category_id`) REFERENCES `task_classes` (`id`) ON DELETE CASCADE
) ENGINE = InnoDB
  AUTO_INCREMENT = 43
  DEFAULT CHARSET = utf8mb4
  COLLATE = utf8mb4_0900_ai_ci
  
CREATE TABLE `tasks`
(
    `id`           int          NOT NULL AUTO_INCREMENT,
    `user_id`      int               DEFAULT NULL,
    `title`        varchar(255) NOT NULL,
    `priority`     int               DEFAULT NULL,
    `is_completed` tinyint(1)        DEFAULT '0',
    `deadline_at`  timestamp    NULL DEFAULT NULL,
    PRIMARY KEY (`id`),
    UNIQUE KEY `uk_tasks_id` (`id`),
    KEY `idx_user_id` (`user_id`),
    CONSTRAINT `tasks_ibfk_1` FOREIGN KEY (`user_id`) REFERENCES `users` (`id`),
    CONSTRAINT `chk_priority` CHECK ((`priority` in (1, 2, 3, 4)))
) ENGINE = InnoDB
  AUTO_INCREMENT = 23
  DEFAULT CHARSET = utf8mb4
  COLLATE = utf8mb4_0900_ai_ci
  
CREATE TABLE `users`
(
    `id`            int          NOT NULL AUTO_INCREMENT,
    `username`      varchar(255) NOT NULL,
    `password`      varchar(255) NOT NULL,
    `phone_number`  varchar(255)      DEFAULT NULL,
    `token_limit`   int               DEFAULT '100000',
    `token_usage`   int               DEFAULT '0',
    `last_reset_at` timestamp    NULL DEFAULT NULL COMMENT '上次周用量重置时间',
    PRIMARY KEY (`id`),
    UNIQUE KEY `username` (`username`),
    UNIQUE KEY `uk_users_id` (`id`)
) ENGINE = InnoDB
  AUTO_INCREMENT = 4
  DEFAULT CHARSET = utf8mb4
  COLLATE = utf8mb4_0900_ai_ci
```

# 4 接口契约

## 4.1 核心API列表(ApiFox)

链接如下：https://oqg5uiubh0.apifox.cn

## 4.2 Agent可调用的工具定义



# 5 后端实现

## 5.1 技术栈

| **分类**          | **选用技术**     | **在 SmartFlow-Agent 中的应用场景**                          |
| ----------------- | ---------------- | ------------------------------------------------------------ |
| **Web 框架**      | **Gin**          | 负责全站 API 的路由分发，处理任务增删改查及智能排程的请求。  |
| **持久层数据库**  | **MySQL 8.0**    | 存储用户、任务、课表及日程运行图（Schedules）的核心数据。    |
| **ORM 框架**      | **GORM**         | 用于简化 Go 与数据库的交互，利用事务处理 `Apply` 接口的原子性操作。 |
| **高性能缓存**    | **Redis**        | 缓存用户的周日程视图（避免频繁扫表）、存储 Token 临时限额、实现分布式锁防止重复排程。 |
| **消息队列**      | **Outbox + Kafka** | **可靠异步解耦**：请求主链路先写 Outbox，后台再投递 Kafka 并消费落库，既降低首字延迟又避免消息瞬时丢失。 |
| **AI 编排框架**   | **Eino**         | 作为 AI Agent 的大脑，根据排程策略（Steady/Rapid）计算任务与水课的嵌入逻辑。 |
| **身份认证**      | **JWT**          | 实现无状态登录，将 `user_id` 封装在 Token 中，确保数据的用户隔离。 |
| **配置管理**      | **Viper**        | 管理数据库、Redis、Kafka 的连接参数，支持多环境（开发/生产）切换。 |
| **API 文档/调试** | **Apifox**       | 维护接口协议，进行前后端联调及自动化测试。                   |
| **日志监控**      | **Zap / Logrus** | 记录系统运行状态，特别是 Kafka 消费失败或 AI 接口超时的错误日志。 |

## 5.2 架构图

PS：截至v0.3.3。其中黑色箭头为请求数据链路，绿色箭头为返回数据，虚线箭头为控制流。

![后端架构图](docs/pics/backend_structure.png)

## 5.3 核心算法

### 5.3.1 智能排课算法

本系统采用 **“原子化时间网格（Atomic TimeGrid）”** 架构，实现了针对大学生复杂课表环境的智能任务填充。算法核心分为 **“沙盘模拟”、“边界感知探测”** 与 **“逻辑位移步进”** 三大模块。

**1. 原子化时间沙盘 (Grid Sandboxing)**

算法首先将物理时间窗口（StartDate 到 EndDate）抽象为一个三维矩阵 $Grid[Week][Day][Section]$。

- **多维状态标记**：每个格子（Slot）是携带 `Status` 和 `EventID` 的 `slotNode` 节点。
- **优先级注水（Hydration）**：
  1. **Blocked（屏蔽区）**：根据用户配置的 `ExcludedSlots` 强制锁定，优先级最高。
  2. **Filler（嵌入区）**：识别“水课”，标记为可利用资源。
  3. **Occupied（占用区）**：映射既有硬核课程，确保调度不产生物理冲突。

**2. 边界感知探测 (Boundary-Sensing Detection)**

为了解决“任务块跨课分身”的 Bug，算法引入了 **EventID 校验机制**。

- **容器自适应长度**：当算法探测到一个 `Filler` 槽位时，会向后贪心扫描，只有当相邻槽位的 `EventID` 相同且同为 `Filler` 时，才允许任务块拉伸。
- **逻辑闭环**：这保证了任务块（TaskItem）要么完美嵌入单门水课，要么占据空地，绝不会出现一个任务横跨两门不同课程的情况。

**3. 稳扎稳打：逻辑位移步进 (Logical-Offset Skipping)**

在 `Steady`（稳扎稳打）模式下，为了实现负载均衡，算法弃用了传统的“物理时间跳跃”，改用 **“逻辑坑位跳跃”**。

$$Gap = \frac{TotalAvailableSlots - (TaskCount \times 2)}{TaskCount + 1}$$

- **物理跳跃（旧版/错误）**：直接 $Time + Gap$，容易因遇到屏蔽时段或硬核课而导致游标溢出，从而“吞掉”后续任务。
- **逻辑跳跃（现行/优化）**：调用 `skipAvailableSlots` 函数，在 Grid 中沿时间轴向后数出 $Gap$ 个**真正可用**的格子作为下一个起点。
- **价值**：确保了在有限的 **2C4G** 服务器资源下，任务能像“等距列队”一样均匀分布在学期空隙中。

------

**🛠️ 算法运行流程**

1. **Build**：调用 `buildTimeGrid`，将数据库的离散 `Schedules` 映射为内存状态网格。
2. **Count**：统计当前窗口内所有 `Free` 与 `Filler` 的原子位总数。
3. **Allocate**：
   - 通过 `FindNextAvailable` 锁定首个合法坑位。
   - 进行 **容器探测** 决定任务块长度。
   - 执行 `skipAvailableSlots` 寻找下一个负载均衡点。
4. **Preview**：输出 DTO 到前端，标记 `status: "suggested"` 供用户预览高亮。

------

**⚡ 性能表现 (Optimization)**

- **时间复杂度**：$O(W \times D \times S)$，其中 $W$ 为任务类跨度周数。在处理典型的 16 周排程时，计算量仅在数千次操作级别，单机响应达毫秒级。
- **空间复杂度**：由于采用了按需创建周 Map 的策略，内存占用随任务跨度动态伸缩，极大地减轻了重庆邮电大学校园服务器环境下的 GC 压力。

------

**数据回填**

在执行完上述算法后，将任务块分成两类数据：

1. 需要新建`ScheduleEvent`的，插入纯空闲时段的数据；
2. 直接嵌入现有课程中的任务块；

然后分别调用不同的业务逻辑，开启大事务，批量插入，使得只需要连接2次数据库，并且若插入出错，支持批量回滚，不会存在任何脏数据。

## 5.4 Agent范式实现细节

### 1) 命中“添加日程/随口记”后的业务流转

```mermaid
flowchart TD
    A[用户消息进入 /agent/chat] --> B[规范会话ID + 选模型]
    B --> C[确保会话存在<br/>Redis会话状态检查<br/>必要时回源DB创建]
    C --> D[模型控制码路由<br/>action=quick_note/chat]
    D --> E{route是否命中quick_note}
    E -- 否 --> X[普通聊天链路<br/>StreamChat流式输出]
    E -- 是 --> F[quick_note.request.accepted<br/>推送reasoning状态块]
    F --> G[跳过二次意图判定<br/>直接进入聚合规划]
    G --> H[单请求聚合规划<br/>生成title/deadline/priority/banter]
    H --> I[时间校验<br/>quick_note.deadline.validating]
    I --> J{时间是否有效}
    J -- 否 --> K[返回纠错文案<br/>不写库<br/>quick_note.failed]
    J -- 是 --> L{优先级是否有效}
    L -- 是 --> M[复用聚合优先级]
    L -- 否 --> N[本地优先级兜底<br/>不再二次调用模型]
    M --> O[调用写库工具<br/>quick_note.persisting]
    N --> O
    O --> P{task_id是否有效}
    P -- 否 --> Q[按重试策略处理<br/>最终返回失败文案]
    P -- 是 --> R[quick_note.persisted]
    R --> S[拼接最终正文<br/>优先复用聚合banter<br/>一次性content输出]
    S --> T[后置持久化<br/>user+assistant写Redis<br/>并写outbox/DB]
    X --> T
```

### 2) 总分流图（消息识别后的去向）

```mermaid
flowchart TD
    A[用户消息进入 AgentChat] --> B[模型控制码路由<br/>action=quick_note/chat]
    B --> C{路由是否成功解析}

    C -- 是 --> D{action=quick_note?}
    D -- 否 --> E[普通聊天链路<br/>StreamChat token流式输出]
    D -- 是 --> F[随口记快路径<br/>跳过二次意图判定]
    F --> G[单请求聚合规划<br/>+本地时间校验/优先级兜底]
    G --> H[写库工具落库<br/>task_id有效校验]
    H --> I[返回一次性正文]

    C -- 否 --> J[随口记兜底路径<br/>恢复二次意图判定]
    J --> K{是否随口记意图}
    K -- 否 --> L[普通聊天链路<br/>StreamChat token流式输出]
    K -- 是 --> M[执行随口记写库链路<br/>返回一次性正文]

    E --> Z[后置持久化<br/>Redis + outbox/DB]
    I --> Z
    L --> Z
    M --> Z
```

# 6 前端实现

## 6.1 设计策略



## 6.2 组件拆解



# 7 部署与监控

## 7.1 容器化部署方案



## 7.2 性能监控&统计



# 8 快速开始



