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
	// QuickNoteGraphNodeIntent 是随口记图里的“意图识别”节点名。
	// 这里把节点名下沉到 node 层，是为了让：
	// 1. 节点自己的分支方法可以直接返回目标节点名；
	// 2. graph 层只负责连线，不需要反向暴露常量给 node 层；
	// 3. 后续若节点改名，只需要在这里统一收口。
	QuickNoteGraphNodeIntent = "quick_note_intent"
	// QuickNoteGraphNodeRank 是随口记图里的“优先级评估”节点名。
	QuickNoteGraphNodeRank = "quick_note_priority"
	// QuickNoteGraphNodePersist 是随口记图里的“持久化写库”节点名。
	QuickNoteGraphNodePersist = "quick_note_persist"
	// QuickNoteGraphNodeExit 是随口记图里的“提前退出”节点名。
	QuickNoteGraphNodeExit = "quick_note_exit"
)

// QuickNoteGraphRunInput 描述一次“随口记图运行”所需的请求级依赖。
//
// 职责边界：
// 1. Model：当前请求实际使用的聊天模型；
// 2. State：本次图运行共享的状态对象；
// 3. Deps：工具层依赖，例如解析 user_id、执行写库；
// 4. SkipIntentVerification：若上游路由已高置信命中，可跳过二次意图判断；
// 5. EmitStage：向外层推送阶段消息的可选回调。
//
// 不负责什么：
// 1. 不负责真正的 graph 连线；
// 2. 不负责工具注册与提取；
// 3. 不负责节点内部业务流转。
type QuickNoteGraphRunInput struct {
	Model                  *ark.ChatModel
	State                  *agentmodel.QuickNoteState
	Deps                   QuickNoteToolDeps
	SkipIntentVerification bool
	EmitStage              func(stage, detail string)
}

// QuickNoteNodes 是“随口记”节点容器。
//
// 设计目的：
// 1. 把“请求级依赖”收口到 node 层，而不是继续堆在 graph 层；
// 2. 让 graph 层直接挂 `nodes.Intent / nodes.Priority / nodes.Persist` 这些方法；
// 3. 这样 graph 文件就只负责画图，不再负责依赖转接。
//
// 职责边界：
// 1. 负责提供可直接挂载到 graph 的节点方法；
// 2. 负责在节点执行时读取本次请求的 input / tool / stage emitter；
// 3. 不负责 graph 编译与运行，也不负责 service 层收尾持久化。
type QuickNoteNodes struct {
	input          QuickNoteGraphRunInput
	createTaskTool tool.InvokableTool
	emitStage      func(stage, detail string)
}

// NewQuickNoteNodes 创建随口记节点容器。
//
// 说明：
// 1. 这里做的是“节点依赖注入”，不是 graph 连线；
// 2. emitStage 允许为空，内部会补成 no-op，避免节点里反复判空；
// 3. createTaskTool 为 persist 节点的硬依赖，缺失时直接报错，避免跑到写库节点再失败。
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

// Exit 是图里的显式退出节点。
//
// 职责边界：
// 1. 只负责把当前 state 原样透传到 END；
// 2. 不负责追加业务逻辑；
// 3. 保留这个节点，是为了后续若要补统一埋点、日志、收尾逻辑时有稳定挂载点。
func (n *QuickNoteNodes) Exit(ctx context.Context, st *agentmodel.QuickNoteState) (*agentmodel.QuickNoteState, error) {
	_ = ctx
	return st, nil
}

// NextAfterIntent 负责根据意图识别结果决定 intent 后的分支走向。
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

// NextAfterPersist 负责根据持久化结果决定 persist 后的分支走向。
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
		// 1. 重试次数耗尽且上游没有明确失败文案时，在这里补一条兜底回复；
		// 2. 这样可以保证图结束后 service 层一定能拿到稳定可展示的失败信息；
		// 3. 不在 graph 层处理，是因为这属于节点业务状态修正。
		st.AssistantReply = "抱歉，我已经重试了多次，还是没能成功记录这条任务，请稍后再试。"
	}
	return compose.END, nil
}
