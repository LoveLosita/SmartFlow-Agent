# 课表图片识别与正式导入前端对接说明

## 目标

这套流程拆成两步：

1. 先调用识图接口，把“总课表图片”转成前端可编辑的草稿表格数据。
2. 用户确认无误后，再调用正式导入接口，把课程写入后端日程表。

这样做的好处是：

- 识图接口保持无状态，只负责“图片 -> 草稿”
- 前端完全掌握编辑态，不依赖后端保存临时草稿
- 正式落库仍统一走现有 `/api/v1/course/import`

## 总体流程

推荐前端流程：

1. 用户上传总课表图片。
2. 前端调用 `/api/v1/course/parse-image` 获取识别草稿。
3. 前端把 `rows` 存在本地页面状态中，渲染成可编辑表格。
4. 用户修改并确认所有行数据。
5. 前端把表格行数据组装成 `/api/v1/course/import` 所需结构。
6. 前端携带 `X-Idempotency-Key` 调用正式导入接口。
7. 导入成功后刷新课表页；若有冲突则提示用户处理。

## 一、识图接口

### 1.1 接口定义

- 方法：`POST`
- 路径：`/api/v1/course/parse-image`
- 鉴权：需要登录态
- Content-Type：`multipart/form-data`
- 文件字段名：`image`

说明：

- 该接口不需要 `X-Idempotency-Key`
- 该接口不落库，只返回草稿

### 1.2 请求示例

```ts
async function parseCourseImage(file: File, token: string) {
  const formData = new FormData()
  formData.append('image', file)

  const response = await fetch('/api/v1/course/parse-image', {
    method: 'POST',
    headers: {
      Authorization: `Bearer ${token}`,
    },
    body: formData,
  })

  const result = await response.json()
  if (!response.ok) {
    throw new Error(result?.info || '课表图片识别失败')
  }
  return result.data
}
```

### 1.3 成功响应

外层仍复用项目统一响应结构：

```json
{
  "status": "10000",
  "info": "success",
  "data": {
    "draft_status": "success",
    "message": "已识别 12 条课程安排，请重点核对周次、星期和节次。",
    "warnings": [
      "图片右下角疑似被裁切，请重点检查最后几列课程。"
    ],
    "rows": [
      {
        "row_id": "row_001",
        "course_name": "高等数学",
        "location": "教一203",
        "is_allow_tasks": false,
        "start_week": 1,
        "end_week": 16,
        "day_of_week": 1,
        "start_section": 1,
        "end_section": 2,
        "week_type": "all",
        "confidence": 0.92,
        "raw_text": "高等数学 1-16周 周一1-2节 教一203",
        "row_warnings": []
      }
    ]
  }
}
```

### 1.4 `draft_status` 语义

- `success`
  说明图片整体较完整，前端可直接进入表格核对。

- `partial`
  说明后端识别到了部分安排，但仍存在不确定字段。前端仍可进入表格核对，但应醒目展示 `warnings` 与 `row_warnings`。

- `reject`
  说明图片不适合继续使用，例如裁切严重、模糊、主体不是课表等。此时 `rows` 为空数组，前端应提示用户重新上传，而不是进入编辑表格。

### 1.5 行字段说明

每一行表示一个“课程安排片段”，不是一门课的完整聚合对象。

- `row_id`
  前端表格的稳定 key。即使用户编辑字段，也建议保留该 key。

- `course_name`
  课程名。`success` 场景下通常应有值；`partial` 场景下可能为空，前端要允许人工补齐。

- `location`
  地点。允许为空字符串。

- `is_allow_tasks`
  当前后端默认给 `false`。因为该字段无法从课表图片稳定识别，建议前端提供开关让用户手改。

- `start_week` / `end_week`
  周次范围。可能为 `null`，前端应允许编辑。

- `day_of_week`
  星期，取值 `1-7`，分别对应周一到周日。可能为 `null`。

- `start_section` / `end_section`
  原子节次编号，例如 `1-2 节` 应表示为 `1` 和 `2`。可能为 `null`。

- `week_type`
  仅允许以下三种值：
  - `all`
  - `odd`
  - `even`

- `confidence`
  0 到 1 之间的小数，用于前端做低置信度高亮。建议把 `<0.75` 的行重点提示用户复核。

- `raw_text`
  模型从图片中抽出的近似原文，建议在“展开详情”或 tooltip 中展示，帮助用户比对。

- `row_warnings`
  当前行的局部问题提示，例如“单双周不清晰”“教室模糊”等。

### 1.6 前端表格建议列

建议首版直接用可编辑表格，不必先做课表画布。

推荐列：

- 课程名
- 地点
- 起始周
- 结束周
- 星期
- 开始节次
- 结束节次
- 周类型
- 允许嵌入任务
- 置信度
- 原文
- 行级警告

### 1.7 识图接口常见失败响应

- `50003`：`course image parser is not configured`
  后端尚未配置多模态模型。

- `40064`：`course image too large`
  图片超过后端配置的字节上限。

- `40065`：`unsupported course image format`
  当前仅支持 `jpg/jpeg/png/webp`。

- `40066`：`course image is empty`
  上传文件为空。

### 1.8 前端交互建议

- `draft_status=reject` 时，不进入表格页，直接提示重新上传。
- `draft_status=partial` 时，允许进入表格页，但默认展开 warning 区域。
- 当某行 `confidence < 0.75` 或 `row_warnings` 非空时，给该行加醒目边框或标签。

## 二、正式导入接口

### 2.1 接口定义

- 方法：`POST`
- 路径：`/api/v1/course/import`
- 鉴权：需要登录态
- Content-Type：`application/json`
- 必传请求头：`X-Idempotency-Key`

说明：

- 正式导入接口会真正写库
- 该接口已经挂了幂等中间件，前端必须传 `X-Idempotency-Key`

### 2.2 请求体结构

后端正式导入接口需要的是：

```json
{
  "courses": [
    {
      "course_name": "高等数学",
      "location": "教一203",
      "is_allow_tasks": false,
      "arrangements": [
        {
          "start_week": 1,
          "end_week": 16,
          "day_of_week": 1,
          "start_section": 1,
          "end_section": 2,
          "week_type": "all"
        }
      ]
    }
  ]
}
```

字段语义：

- `course_name`: 课程名
- `location`: 地点
- `is_allow_tasks`: 是否允许该课程时段嵌入任务
- `arrangements`: 该课程的所有上课安排

### 2.3 前端如何从草稿行表组装正式导入 payload

推荐规则：

1. 先过滤掉仍未补齐必填字段的行。
2. 以 `course_name + location + is_allow_tasks` 作为分组键。
3. 同组行合并为同一个 `course`。
4. 每一行转成该 `course` 下的一个 `arrangements` 项。

推荐的前端类型：

```ts
type CourseDraftRow = {
  row_id: string
  course_name: string
  location: string
  is_allow_tasks: boolean
  start_week: number | null
  end_week: number | null
  day_of_week: number | null
  start_section: number | null
  end_section: number | null
  week_type: 'all' | 'odd' | 'even' | ''
  confidence: number
  raw_text: string
  row_warnings: string[]
}

type CourseImportPayload = {
  courses: Array<{
    course_name: string
    location: string
    is_allow_tasks: boolean
    arrangements: Array<{
      start_week: number
      end_week: number
      day_of_week: number
      start_section: number
      end_section: number
      week_type: 'all' | 'odd' | 'even'
    }>
  }>
}
```

组装示例：

```ts
function buildCourseImportPayload(rows: CourseDraftRow[]): CourseImportPayload {
  const validRows = rows.filter((row) =>
    row.course_name.trim() &&
    row.start_week != null &&
    row.end_week != null &&
    row.day_of_week != null &&
    row.start_section != null &&
    row.end_section != null &&
    (row.week_type === 'all' || row.week_type === 'odd' || row.week_type === 'even'),
  )

  const grouped = new Map<string, CourseImportPayload['courses'][number]>()

  for (const row of validRows) {
    const key = `${row.course_name}__${row.location}__${row.is_allow_tasks}`
    const arrangement = {
      start_week: row.start_week!,
      end_week: row.end_week!,
      day_of_week: row.day_of_week!,
      start_section: row.start_section!,
      end_section: row.end_section!,
      week_type: row.week_type as 'all' | 'odd' | 'even',
    }

    if (!grouped.has(key)) {
      grouped.set(key, {
        course_name: row.course_name.trim(),
        location: row.location.trim(),
        is_allow_tasks: row.is_allow_tasks,
        arrangements: [arrangement],
      })
      continue
    }

    grouped.get(key)!.arrangements.push(arrangement)
  }

  return {
    courses: Array.from(grouped.values()),
  }
}
```

### 2.4 调用正式导入接口示例

```ts
function createIdempotencyKey(prefix = 'course-import') {
  return `${prefix}-${Date.now()}-${Math.random().toString(36).slice(2, 10)}`
}

async function importCourses(
  payload: CourseImportPayload,
  token: string,
  idempotencyKey = createIdempotencyKey(),
) {
  const response = await fetch('/api/v1/course/import', {
    method: 'POST',
    headers: {
      'Content-Type': 'application/json',
      Authorization: `Bearer ${token}`,
      'X-Idempotency-Key': idempotencyKey,
    },
    body: JSON.stringify(payload),
  })

  const result = await response.json()

  if (!response.ok) {
    const error = new Error(result?.info || '课程导入失败')
    ;(error as any).status = response.status
    ;(error as any).payload = result
    throw error
  }

  return result
}
```

### 2.5 正式导入成功响应

```json
{
  "status": "10000",
  "info": "success"
}
```

## 三、前端在正式导入前应做的本地校验

建议在调用 `/api/v1/course/import` 前先做一次本地校验，避免把明显不完整的数据提交给后端。

建议校验项：

1. `course_name` 非空
2. `start_week` / `end_week` 非空，且 `1 <= start_week <= end_week <= 24`
3. `day_of_week` 在 `1-7`
4. `start_section` / `end_section` 非空，且 `1 <= start_section <= end_section <= 12`
5. `week_type` 只能是 `all | odd | even`

如果校验失败，建议直接在表格中标红，不要发请求。

## 四、正式导入接口的常见失败响应

### 4.1 缺少幂等键

HTTP 状态：`400`

```json
{
  "status": "40037",
  "info": "missing idempotency key"
}
```

前端处理建议：

- 检查是否传了 `X-Idempotency-Key`
- 每次“点击确认导入”生成一个新的 key
- 不要在一次导入流程里反复变化同一个 key

### 4.2 请求处理中

HTTP 状态：`409`

```json
{
  "status": "40038",
  "info": "request is processing, please do not repeat click"
}
```

前端处理建议：

- 导入按钮进入 loading 态
- 阻止用户连续点击
- 若收到该错误，提示“正在导入，请勿重复提交”

### 4.3 课程结构不合法

HTTP 状态：`400`

```json
{
  "status": "40019",
  "info": "wrong course info"
}
```

前端处理建议：

- 说明当前编辑结果不满足导入约束
- 引导用户重点检查周次、星期、节次、周类型

### 4.4 重复导入课程

HTTP 状态：`400`

```json
{
  "status": "40029",
  "info": "insert course twice"
}
```

前端处理建议：

- 提示用户当前课程可能已经导入过
- 可提供“返回课表页查看现有课程”的入口

### 4.5 与已有非课程日程冲突

HTTP 状态：`409`

响应示例：

```json
{
  "status": "40026",
  "info": "schedule conflict",
  "data": [
    {
      "event_id": 101,
      "name": "实验报告",
      "location": "图书馆",
      "day_of_week": 1,
      "week": 3,
      "sections": [1, 2],
      "start_section": 1,
      "end_section": 2,
      "type": "task",
      "embedded_tasks": []
    }
  ]
}
```

前端处理建议：

- 不要把冲突吞掉
- 可以弹窗列出冲突项
- 提示用户先处理已有日程，再重新导入

## 五、推荐的前端调用顺序

推荐把整个导入流程写成这三个阶段：

### 阶段一：识图

- 上传图片
- 调用 `/course/parse-image`
- 若 `draft_status=reject`，直接结束
- 若 `success` 或 `partial`，进入草稿编辑页

### 阶段二：草稿编辑

- 在本地状态中维护 `rows`
- 支持用户改课程名、周次、节次、地点、周类型、`is_allow_tasks`
- 在页面上展示 `warnings`、`row_warnings`、`confidence`

### 阶段三：正式导入

- 先做本地校验
- 再调用 `buildCourseImportPayload(rows)`
- 生成 `X-Idempotency-Key`
- 调用 `/course/import`
- 成功后提示并刷新课表

## 六、可直接复用的页面状态建议

```ts
type CourseImageImportPageState = {
  imageFile: File | null
  parseLoading: boolean
  importLoading: boolean
  draftStatus: 'success' | 'partial' | 'reject' | null
  draftMessage: string
  warnings: string[]
  rows: CourseDraftRow[]
}
```

## 七、配置提醒

后端需要额外配置一个多模态模型：

```yaml
courseImport:
  visionModel: "你的多模态模型名"
  maxImageBytes: 5242880
  maxTokens: 8192
```

若 `visionModel` 留空，则主服务仍可启动，但 `/course/parse-image` 会返回“识图模型未配置”。
若后端日志出现 `finish_reason="length"`，说明模型输出被长度上限截断，应优先增大 `courseImport.maxTokens`。
