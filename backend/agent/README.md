# backend/agent 目录说明

该目录已按“路由 / 聊天 / 随口记”三层拆分，便于阅读、调试与扩展：

1. `route/`
- `route.go`：只负责模型控制码分流（`quick_note` / `chat`）。
- 提供控制码解析、nonce 校验、路由兜底，不参与写库与回复拼装。

2. `chat/`
- `stream.go`：普通聊天流式输出封装（SSE/OpenAI 兼容 chunk 转换）。
- `prompt.go`：聊天主系统提示词。

3. `quicknote/`
- `graph.go`：只负责图编排连线与分支，不承载节点内部实现。
- `nodes.go`：节点实现（意图识别、优先级评估、持久化、分支选择）。
- `tool.go`：工具定义、参数校验、deadline 解析、写库工具打包。
- `state.go`：随口记状态容器与重试状态记录。
- `prompt.go`：随口记提示词（控制码路由、聚合规划、优先级评估、回复润色）。

4. `README.md`（当前文件）
- 记录目录职责边界，帮助后续继续按同样范式扩展 `query/update` 等技能链路。

> 说明：服务层仍通过 `RunQuickNoteGraph` 调用随口记图；若判定为非随口记意图，会自动回落到普通流式聊天链路。
