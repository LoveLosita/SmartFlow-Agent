package agentllm

import (
	"context"
	"time"

	"github.com/cloudwego/eino-ext/components/model/ark"
)

const scheduleRefineNodeTimeout = 120 * time.Second

type ScheduleRefineContractOutput struct {
	Intent            string                        `json:"intent"`
	Strategy          string                        `json:"strategy"`
	HardRequirements  []string                      `json:"hard_requirements"`
	HardAssertions    []ScheduleRefineAssertionLite `json:"hard_assertions"`
	KeepRelativeOrder bool                          `json:"keep_relative_order"`
	OrderScope        string                        `json:"order_scope"`
}

type ScheduleRefineAssertionLite struct {
	Metric     string `json:"metric"`
	Operator   string `json:"operator"`
	Value      int    `json:"value"`
	Min        int    `json:"min"`
	Max        int    `json:"max"`
	Week       int    `json:"week"`
	TargetWeek int    `json:"target_week"`
}

type ScheduleRefinePlannerOutput struct {
	Summary string   `json:"summary"`
	Steps   []string `json:"steps"`
}

type ScheduleRefineToolCall struct {
	Tool   string         `json:"tool"`
	Params map[string]any `json:"params"`
}

type ScheduleRefineReactOutput struct {
	Done        bool                     `json:"done"`
	Summary     string                   `json:"summary"`
	GoalCheck   string                   `json:"goal_check"`
	Decision    string                   `json:"decision"`
	MissingInfo []string                 `json:"missing_info,omitempty"`
	ToolCalls   []ScheduleRefineToolCall `json:"tool_calls"`
}

type ScheduleRefinePostReflectOutput struct {
	Reflection   string `json:"reflection"`
	NextStrategy string `json:"next_strategy"`
	ShouldStop   bool   `json:"should_stop"`
}

type ScheduleRefineReviewOutput struct {
	Pass   bool     `json:"pass"`
	Reason string   `json:"reason"`
	Unmet  []string `json:"unmet"`
}

func GenerateScheduleRefineContract(ctx context.Context, chatModel *ark.ChatModel, systemPrompt, userPrompt string) (*ScheduleRefineContractOutput, string, error) {
	return callScheduleRefineJSON[ScheduleRefineContractOutput](ctx, chatModel, systemPrompt, userPrompt, ArkCallOptions{
		Temperature: 0,
		MaxTokens:   260,
		Thinking:    ThinkingModeDisabled,
	})
}

func GenerateScheduleRefinePlanner(ctx context.Context, chatModel *ark.ChatModel, systemPrompt, userPrompt string, maxTokens int) (*ScheduleRefinePlannerOutput, string, error) {
	return callScheduleRefineJSON[ScheduleRefinePlannerOutput](ctx, chatModel, systemPrompt, userPrompt, ArkCallOptions{
		Temperature: 0,
		MaxTokens:   maxTokens,
		Thinking:    ThinkingModeDisabled,
	})
}

func GenerateScheduleRefineReact(ctx context.Context, chatModel *ark.ChatModel, systemPrompt, userPrompt string, useThinking bool, maxTokens int) (string, error) {
	thinking := ThinkingModeDisabled
	if useThinking {
		thinking = ThinkingModeEnabled
	}
	return callScheduleRefineText(ctx, chatModel, systemPrompt, userPrompt, ArkCallOptions{
		Temperature: 0,
		MaxTokens:   maxTokens,
		Thinking:    thinking,
	})
}

func GenerateScheduleRefinePostReflect(ctx context.Context, chatModel *ark.ChatModel, systemPrompt, userPrompt string) (*ScheduleRefinePostReflectOutput, string, error) {
	return callScheduleRefineJSON[ScheduleRefinePostReflectOutput](ctx, chatModel, systemPrompt, userPrompt, ArkCallOptions{
		Temperature: 0,
		MaxTokens:   220,
		Thinking:    ThinkingModeDisabled,
	})
}

func GenerateScheduleRefineReview(ctx context.Context, chatModel *ark.ChatModel, systemPrompt, userPrompt string) (*ScheduleRefineReviewOutput, string, error) {
	return callScheduleRefineJSON[ScheduleRefineReviewOutput](ctx, chatModel, systemPrompt, userPrompt, ArkCallOptions{
		Temperature: 0,
		MaxTokens:   240,
		Thinking:    ThinkingModeDisabled,
	})
}

func GenerateScheduleRefineSummary(ctx context.Context, chatModel *ark.ChatModel, systemPrompt, userPrompt string) (string, error) {
	return callScheduleRefineText(ctx, chatModel, systemPrompt, userPrompt, ArkCallOptions{
		Temperature: 0.35,
		MaxTokens:   280,
		Thinking:    ThinkingModeDisabled,
	})
}

func GenerateScheduleRefineRepair(ctx context.Context, chatModel *ark.ChatModel, systemPrompt, userPrompt string) (string, error) {
	return callScheduleRefineText(ctx, chatModel, systemPrompt, userPrompt, ArkCallOptions{
		Temperature: 0.15,
		MaxTokens:   240,
		Thinking:    ThinkingModeDisabled,
	})
}

func callScheduleRefineText(ctx context.Context, chatModel *ark.ChatModel, systemPrompt, userPrompt string, options ArkCallOptions) (string, error) {
	nodeCtx, cancel := context.WithTimeout(ctx, scheduleRefineNodeTimeout)
	defer cancel()
	return CallArkText(nodeCtx, chatModel, systemPrompt, userPrompt, options)
}

func callScheduleRefineJSON[T any](ctx context.Context, chatModel *ark.ChatModel, systemPrompt, userPrompt string, options ArkCallOptions) (*T, string, error) {
	nodeCtx, cancel := context.WithTimeout(ctx, scheduleRefineNodeTimeout)
	defer cancel()
	return CallArkJSON[T](nodeCtx, chatModel, systemPrompt, userPrompt, options)
}
