package newagenttools

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	newagentshared "github.com/LoveLosita/smartflow/backend/newAgent/shared"
	"github.com/LoveLosita/smartflow/backend/newAgent/tools/schedule"
)

// QuickNoteDeps 描述随口记工具所需的外部依赖。
//
// 职责边界：
// 1. CreateTask 负责真正写库，工具层不直接依赖 DAO；
// 2. UserID 由 execute 节点通过 args["_user_id"] 注入，工具层不自行解析会话身份。
type QuickNoteDeps struct {
	// CreateTask 将解析后的任务字段写入数据库。
	// 调用目的：解耦工具层与 DAO 层，方便测试和替换。
	CreateTask func(userID int, title string, priorityGroup int, deadlineAt *time.Time) (taskID int, err error)
}

// QuickNoteCreateResult 是 quick_note_create 工具的结构化返回。
type QuickNoteCreateResult struct {
	TaskID        int    `json:"task_id"`
	Title         string `json:"title"`
	PriorityLabel string `json:"priority_label"`
	DeadlineAt    string `json:"deadline_at,omitempty"`
	Message       string `json:"message"`
}

// quickNoteFallbackPriority 根据截止时间推断默认优先级。
//
// 推断规则：
// 1. 有截止时间且距今 ≤48h → 1（重要且紧急）；
// 2. 有截止时间且距今 >48h → 2（重要不紧急）；
// 3. 无截止时间 → 3（简单不重要）。
func quickNoteFallbackPriority(deadline *time.Time) int {
	if deadline != nil {
		if time.Until(*deadline) <= 48*time.Hour {
			return newagentshared.QuickNotePriorityImportantUrgent
		}
		return newagentshared.QuickNotePriorityImportantNotUrgent
	}
	return newagentshared.QuickNotePrioritySimpleNotImportant
}

// NewQuickNoteToolHandler 创建 quick_note_create 工具的 handler 闭包。
//
// 职责边界：
// 1. 负责参数校验、时间解析、优先级推断、调 deps 写库、组装返回；
// 2. 不负责 LLM 交互和会话管理。
// 3. state 参数忽略——随口记不需要 ScheduleState，已注册到 scheduleFreeTools。
func NewQuickNoteToolHandler(deps QuickNoteDeps) ToolHandler {
	return func(state *schedule.ScheduleState, args map[string]any) string {
		_ = state

		// 1. 提取 _user_id（由 execute 节点在调用前注入）。
		userID := 0
		if uid, ok := args["_user_id"].(int); ok {
			userID = uid
		}
		if userID <= 0 {
			return "工具调用失败：无法识别用户身份。"
		}

		// 2. 提取必填参数 title。
		title := ""
		if t, ok := args["title"].(string); ok {
			title = strings.TrimSpace(t)
		}
		if title == "" {
			return "工具调用失败：缺少必填参数 title（任务标题）。"
		}

		// 3. 提取可选参数 deadline_at，复用旧链路时间解析能力。
		var deadline *time.Time
		if raw, ok := args["deadline_at"].(string); ok {
			raw = strings.TrimSpace(raw)
			if raw != "" {
				// 调用目的：复用旧链路成熟的中文相对时间解析器，支持"明天下午3点"等格式。
				parsed, err := newagentshared.ParseOptionalDeadline(raw)
				if err != nil {
					return fmt.Sprintf("工具调用失败：截止时间格式无法解析（%s）。支持格式：2026-04-20 18:00、明天下午3点、下周一上午9点。", err)
				}
				deadline = parsed
			}
		}

		// 4. 提取可选参数 priority_group；未提供时按截止时间自动推断。
		priorityGroup := 0
		if pg, ok := args["priority_group"].(float64); ok {
			priorityGroup = int(pg)
		}
		if !newagentshared.IsValidTaskPriority(priorityGroup) {
			priorityGroup = quickNoteFallbackPriority(deadline)
		}

		// 5. 调用依赖写库。
		taskID, err := deps.CreateTask(userID, title, priorityGroup, deadline)
		if err != nil {
			return fmt.Sprintf("工具调用失败：写入任务时出错（%s）。", err)
		}
		if taskID <= 0 {
			return "工具调用失败：写入任务后未返回有效 task_id。"
		}

		// 6. 组装结构化返回，包含 banter 提示引导 LLM 自然生成调侃。
		priorityLabel := newagentshared.PriorityLabelCN(priorityGroup)
		deadlineStr := ""
		if deadline != nil {
			deadlineStr = deadline.In(newagentshared.ShanghaiLocation()).Format("2006-01-02 15:04")
		}

		result := QuickNoteCreateResult{
			TaskID:        taskID,
			Title:         title,
			PriorityLabel: priorityLabel,
			DeadlineAt:    deadlineStr,
		}

		// 6.1 成功事实 + banter 提示：通过工具返回值引导 ReAct LLM 在 speak 中自然加入轻松跟进。
		if deadlineStr != "" {
			result.Message = fmt.Sprintf("已记录：%s（%s，截止 %s）。回复时请用轻松友好的语气，加一句与任务内容相关的俏皮话（不超过30字）。",
				title, priorityLabel, deadlineStr)
		} else {
			result.Message = fmt.Sprintf("已记录：%s（%s）。回复时请用轻松友好的语气，加一句与任务内容相关的俏皮话（不超过30字）。",
				title, priorityLabel)
		}

		jsonBytes, marshalErr := json.Marshal(result)
		if marshalErr != nil {
			// 6.2 JSON 序列化失败时降级为纯文本，确保 LLM 仍能拿到关键信息。
			return result.Message
		}
		return string(jsonBytes)
	}
}
