package agentllm

import (
	"context"

	agentprompt "github.com/LoveLosita/smartflow/backend/agent/prompt"
	"github.com/cloudwego/eino-ext/components/model/ark"
)

// TaskQueryPlanOutput 描述计划节点返回的结构化查询方案。
//
// 职责边界：
// 1. 只承接模型输出，不在这里做合法性校验。
// 2. 字段为空或非法时，由 node 层继续归一化与兜底。
type TaskQueryPlanOutput struct {
	UserGoal         string `json:"user_goal"`
	Quadrants        []int  `json:"quadrants"`
	SortBy           string `json:"sort_by"`
	Order            string `json:"order"`
	Limit            int    `json:"limit"`
	IncludeCompleted *bool  `json:"include_completed"`
	Keyword          string `json:"keyword"`
	DeadlineBefore   string `json:"deadline_before"`
	DeadlineAfter    string `json:"deadline_after"`
}

// TaskQueryRetryPatch 描述反思节点允许回写的计划补丁。
//
// 输入输出语义：
// 1. 指针字段为 nil 表示“不改这个字段”。
// 2. 非 nil 但值为空字符串，表示显式清空该条件。
type TaskQueryRetryPatch struct {
	Quadrants        *[]int  `json:"quadrants,omitempty"`
	SortBy           *string `json:"sort_by,omitempty"`
	Order            *string `json:"order,omitempty"`
	Limit            *int    `json:"limit,omitempty"`
	IncludeCompleted *bool   `json:"include_completed,omitempty"`
	Keyword          *string `json:"keyword,omitempty"`
	DeadlineBefore   *string `json:"deadline_before,omitempty"`
	DeadlineAfter    *string `json:"deadline_after,omitempty"`
}

// TaskQueryReflectOutput 描述反思节点对本轮查询结果的判定。
//
// 输入输出语义：
// 1. Satisfied=true 表示当前结果可直接收口。
// 2. NeedRetry=true 表示建议再跑一轮，但真正是否重试由 node 层结合次数上限决定。
// 3. Reply 是可直接给用户的候选文案，允许为空。
type TaskQueryReflectOutput struct {
	Satisfied  bool                `json:"satisfied"`
	NeedRetry  bool                `json:"need_retry"`
	Reason     string              `json:"reason"`
	Reply      string              `json:"reply"`
	RetryPatch TaskQueryRetryPatch `json:"retry_patch"`
}

// PlanTaskQuery 负责调用模型，把自然语言查询规划成结构化检索参数。
//
// 职责边界：
// 1. 只负责模型调用与 JSON 解析。
// 2. 不负责结果兜底、限流裁剪或时间归一化。
func PlanTaskQuery(ctx context.Context, chatModel *ark.ChatModel, nowText, userInput string) (*TaskQueryPlanOutput, error) {
	parsed, _, err := CallArkJSON[TaskQueryPlanOutput](ctx, chatModel, agentprompt.TaskQueryPlanPrompt, agentprompt.BuildTaskQueryPlanUserPrompt(nowText, userInput), ArkCallOptions{
		Temperature: 0,
		MaxTokens:   260,
		Thinking:    ThinkingModeDisabled,
	})
	return parsed, err
}

// ReflectTaskQuery 负责让模型判断当前查询结果是否满足用户意图。
//
// 职责边界：
// 1. 只负责反思提示词调用与结构化解析。
// 2. 不负责实际执行重试，也不负责拼接最终兜底回复。
func ReflectTaskQuery(ctx context.Context, chatModel *ark.ChatModel, prompt string) (*TaskQueryReflectOutput, error) {
	parsed, _, err := CallArkJSON[TaskQueryReflectOutput](ctx, chatModel, agentprompt.TaskQueryReflectPrompt, prompt, ArkCallOptions{
		Temperature: 0,
		MaxTokens:   380,
		Thinking:    ThinkingModeDisabled,
	})
	return parsed, err
}
