# backend/agent 目录说明

该目录当前按“聊天流式输出能力”和“可编排的随口记能力”拆分：

1. `graph.go`
- 仅负责现有流式聊天输出封装（SSE/OpenAI 兼容 chunk 转换）。
- 已有线上链路依赖，当前不改业务逻辑。

2. `prompt.go`
- 通用 Agent 提示词。

3. `quick_note_prompt.go`
- AI 随口记专用提示词（意图识别、优先级评估）。

4. `state.go`
- 随口记链路状态结构（意图标记、抽取结果、重试计数、持久化结果）。

5. `tool.go`
- 随口记工具打包入口：
  - `BuildQuickNoteToolBundle`
  - 工具输入输出 schema
  - deadline 解析与优先级校验

6. `quick_note_graph.go`
- 随口记 graph 编排实现：
  - 节点1：意图识别
  - 节点2：优先级评估
  - 节点3：调用写库工具
  - 分支：失败自动重试（最多 3 次）

> 说明：服务层通过 `RunQuickNoteGraph` 调用该图；若判定为非随口记意图，会自动回落到原有普通流式聊天逻辑。
