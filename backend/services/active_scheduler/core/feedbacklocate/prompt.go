package feedbacklocate

import (
	"encoding/json"
	"strings"
)

const locateSystemPrompt = `
你是 SmartFlow 主动调度里专门负责 unfinished_feedback 的定位器。
你的任务只有一个：根据用户补充的话，把它定位到当前滚动窗口中的某一个 schedule_event；定位不了就继续 ask_user。

硬规则：
1. 只允许输出 JSON，不要输出 markdown，不要输出解释性正文。
2. 只允许返回 action / target_type / target_id / reason / ask_user_question 这几个字段。
3. target_type 只能是 schedule_event。
4. target_id 必须来自候选列表里的 target_id，不要编造，不要猜一个新的。
5. 当你不能稳定定位时，action 必须是 ask_user，并给出一句短问题。
6. 当用户补充信息已经足够时，action 必须是 select_candidate。
7. 请优先结合当前时间、用户原始补充话术、pending question 和候选日程的时间顺序来判断。
`

func buildPromptInput(req Request, generatedAt string, windowStart string, windowEnd string, candidates []eventCandidate) promptInput {
	input := promptInput{
		GeneratedAt: generatedAt,
		UserMessage: strings.TrimSpace(req.UserMessage),
		Window: promptWindowInput{
			StartAt: windowStart,
			EndAt:   windowEnd,
		},
	}

	if trimmed := strings.TrimSpace(req.PendingQuestion); trimmed != "" {
		input.PendingQuestion = trimmed
	}
	if len(req.MissingInfo) > 0 {
		input.MissingInfo = cloneAndTrimStrings(req.MissingInfo)
	}
	input.Candidates = append([]eventCandidate(nil), candidates...)
	return input
}

func buildUserPrompt(input promptInput) (string, error) {
	raw, err := json.MarshalIndent(input, "", "  ")
	if err != nil {
		return "", err
	}

	var builder strings.Builder
	builder.WriteString("请根据输入定位当前滚动窗口中的 schedule_event。")
	builder.WriteString("只输出 JSON，不要补充任何其它内容。\n")
	builder.WriteString("输入：\n")
	builder.WriteString(string(raw))
	return builder.String(), nil
}

// BuildAskUserQuestion 负责把 missing_info 转成继续追问用户的短问题。
func BuildAskUserQuestion(missingInfo []string) string {
	normalized := cloneAndTrimStrings(missingInfo)
	if len(normalized) == 0 {
		return "请补充能唯一定位到未完成日程块的信息。"
	}

	for _, item := range normalized {
		if item == "feedback_target" {
			return "请告诉我你指的是哪一个未完成的日程块，比如具体时间或名称。"
		}
	}
	return "请补充 " + strings.Join(normalized, "、") + " 对应的信息。"
}
