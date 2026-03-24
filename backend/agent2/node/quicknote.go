package agentnode

import (
	"context"
	"errors"

	agentmodel "github.com/LoveLosita/smartflow/backend/agent2/model"
	"github.com/cloudwego/eino-ext/components/model/ark"
	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/compose"
)

const (
	// QuickNoteGraphNodeIntent 是随口记图中的“意图识别”节点名。
	QuickNoteGraphNodeIntent = "quick_note_intent"
	// QuickNoteGraphNodeRank 是随口记图中的“优先级评估”节点名。
	QuickNoteGraphNodeRank = "quick_note_priority"
	// QuickNoteGraphNodePersist 是随口记图中的“持久化写库”节点名。
	QuickNoteGraphNodePersist = "quick_note_persist"
	// QuickNoteGraphNodeExit 是随口记图中的“提前退出”节点名。
	QuickNoteGraphNodeExit = "quick_note_exit"
)

// QuickNoteGraphRunInput 描述一次随口记图运行所需的请求级依赖。
//
// 职责边界：
// 1. 负责把模型、初始状态、工具依赖和阶段回调打包给 graph 层。
// 2. 不负责做依赖校验，校验逻辑由 graph/node 构造阶段处理。
type QuickNoteGraphRunInput struct {
	Model                  *ark.ChatModel
	State                  *agentmodel.QuickNoteState
	Deps                   QuickNoteToolDeps
	SkipIntentVerification bool
	EmitStage              func(stage, detail string)
}

// QuickNoteNodes 是随口记图的节点容器。
//
// 职责边界：
// 1. 负责承接节点运行时依赖，并向 graph 暴露可直接挂载的方法。
// 2. 不负责 graph 编译，也不负责 service 层接口接线。
type QuickNoteNodes struct {
	input          QuickNoteGraphRunInput
	createTaskTool tool.InvokableTool
	emitStage      func(stage, detail string)
}

// NewQuickNoteNodes 负责构造随口记节点容器。
//
// 输入输出语义：
// 1. createTaskTool 不能为空，否则 persist 节点无法落库。
// 2. EmitStage 为空时会回退到空实现，避免节点内部到处判空。
func NewQuickNoteNodes(input QuickNoteGraphRunInput, createTaskTool tool.InvokableTool) (*QuickNoteNodes, error) {
	if createTaskTool == nil {
		return nil, errors.New("quick note nodes: createTaskTool is nil")
	}

	emitStage := input.EmitStage
	if emitStage == nil {
		emitStage = func(stage, detail string) {}
	}

	return &QuickNoteNodes{
		input:          input,
		createTaskTool: createTaskTool,
		emitStage:      emitStage,
	}, nil
}

// Exit 是图中的显式退出节点。
//
// 职责边界：
// 1. 仅作为图收口占位，保持状态原样透传。
// 2. 不做额外业务处理，避免退出节点再引入副作用。
func (n *QuickNoteNodes) Exit(ctx context.Context, st *agentmodel.QuickNoteState) (*agentmodel.QuickNoteState, error) {
	_ = ctx
	return st, nil
}

// NextAfterIntent 根据意图识别结果决定 intent 节点后的分支走向。
//
// 步骤说明：
// 1. 非随口记意图时直接退出，避免误把普通聊天写成任务。
// 2. 截止时间校验失败时同样直接退出，让上层优先把错误提示给用户。
// 3. 只有意图成立且时间合法，才进入优先级评估节点。
func (n *QuickNoteNodes) NextAfterIntent(ctx context.Context, st *agentmodel.QuickNoteState) (string, error) {
	_ = ctx
	if st == nil || !st.IsQuickNoteIntent {
		return QuickNoteGraphNodeExit, nil
	}
	if st.DeadlineValidationError != "" {
		return QuickNoteGraphNodeExit, nil
	}
	return QuickNoteGraphNodeRank, nil
}

// NextAfterPersist 根据持久化结果决定 persist 节点后的分支走向。
//
// 输入输出语义：
// 1. Persisted=true 表示已经成功写库，可以直接结束。
// 2. Persisted=false 且 CanRetryTool()=true 表示继续重试写库。
// 3. 重试用尽后会补齐兜底回复，再结束链路，避免用户拿到空响应。
func (n *QuickNoteNodes) NextAfterPersist(ctx context.Context, st *agentmodel.QuickNoteState) (string, error) {
	_ = ctx
	if st == nil {
		return compose.END, nil
	}
	if st.Persisted {
		return compose.END, nil
	}
	if st.CanRetryTool() {
		return QuickNoteGraphNodePersist, nil
	}
	if st.AssistantReply == "" {
		st.AssistantReply = "抱歉，我已经重试了多次，还是没能成功记录这条任务，请稍后再试。"
	}
	return compose.END, nil
}
