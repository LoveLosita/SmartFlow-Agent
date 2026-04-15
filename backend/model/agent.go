package model

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// AgentResumeType 表示本轮请求想恢复哪一类挂起交互。
type AgentResumeType string

const (
	AgentResumeTypeAskUser           AgentResumeType = "ask_user"
	AgentResumeTypeConfirm           AgentResumeType = "confirm"
	AgentResumeTypeConnectionRecover AgentResumeType = "connection_recover"
)

// AgentResumeAction 表示用户这次恢复请求携带的动作类型。
type AgentResumeAction string

const (
	AgentResumeActionReply   AgentResumeAction = "reply"
	AgentResumeActionApprove AgentResumeAction = "approve"
	AgentResumeActionReject  AgentResumeAction = "reject"
	AgentResumeActionCancel  AgentResumeAction = "cancel"
	AgentResumeActionResume  AgentResumeAction = "resume"
)

// AgentResumeRequest 是 extra.resume 的统一结构。
//
// 设计目的：
// 1. 继续复用现有聊天入口，不再额外新增一条“确认专用接口”；
// 2. 前端只提交“我要恢复哪次交互、这次动作是什么”，不直接改后端 state；
// 3. 后端进入聊天主链路前，先读取这份结构，再决定走 confirm / ask_user / connection_recover 哪条恢复路径。
//
// 推荐前端请求形态：
//
//	{
//	  "message": "",
//	  "extra": {
//	    "resume": {
//	      "interaction_id": "xxx",
//	      "type": "confirm",
//	      "action": "approve"
//	    }
//	  }
//	}
//
// TODO(newagent/api): 进入聊天主流程前，优先调用 req.ResumeRequest()；若命中恢复协议，则不要把本轮请求按普通聊天处理。
type AgentResumeRequest struct {
	InteractionID string            `json:"interaction_id"`
	Type          AgentResumeType   `json:"type,omitempty"`
	Action        AgentResumeAction `json:"action"`
}

type UserSendMessageRequest struct {
	ConversationID string         `json:"conversation_id,omitempty"`
	Message        string         `json:"message" binding:"required"`
	Model          string         `json:"model,omitempty"`
	Thinking       string         `json:"thinking,omitempty"`
	Extra          map[string]any `json:"extra,omitempty"`
}

// ResumeRequest 从 extra.resume 中解析结构化恢复请求。
//
// 步骤说明：
// 1. 若 extra 或 extra.resume 不存在，则直接返回 nil，表示本轮是普通聊天请求；
// 2. 先把任意 map/struct 形态统一转成 JSON，再反序列化到强类型结构，避免入口层到处手写断言；
// 3. 解析成功后先做 Normalize，再做最小必要校验，防止后续业务层拿到脏协议继续流转；
// 4. 这里只负责协议解析与基本校验，不负责真正恢复状态，也不负责改 Redis/MySQL。
func (r *UserSendMessageRequest) ResumeRequest() (*AgentResumeRequest, error) {
	if r == nil || len(r.Extra) == 0 {
		return nil, nil
	}

	rawResume, ok := r.Extra["resume"]
	if !ok || rawResume == nil {
		return nil, nil
	}

	data, err := json.Marshal(rawResume)
	if err != nil {
		return nil, fmt.Errorf("序列化 extra.resume 失败: %w", err)
	}

	var resume AgentResumeRequest
	if err := json.Unmarshal(data, &resume); err != nil {
		return nil, fmt.Errorf("解析 extra.resume 失败: %w", err)
	}

	resume.Normalize()
	if err := resume.Validate(); err != nil {
		return nil, err
	}
	return &resume, nil
}

// Normalize 统一清洗恢复协议中的字符串字段。
func (r *AgentResumeRequest) Normalize() {
	if r == nil {
		return
	}
	r.InteractionID = strings.TrimSpace(r.InteractionID)
	r.Type = AgentResumeType(strings.TrimSpace(string(r.Type)))
	r.Action = AgentResumeAction(strings.TrimSpace(string(r.Action)))
}

// Validate 校验恢复协议的最小合法性。
//
// 职责边界：
// 1. 只校验“是否像一份合法的恢复协议”，不校验 interaction_id 是否真实存在；
// 2. confirm / ask_user / connection_recover 共用一条入口，但动作集合不同，所以这里做显式分流校验；
// 3. 对于 ask_user 回复，真正的回答正文仍建议优先放在顶层 message，这里不强制要求额外 answer 字段。
func (r *AgentResumeRequest) Validate() error {
	if r == nil {
		return nil
	}
	if r.InteractionID == "" {
		return fmt.Errorf("extra.resume.interaction_id 不能为空")
	}
	if r.Action == "" {
		return fmt.Errorf("extra.resume.action 不能为空")
	}

	switch r.Type {
	case "", AgentResumeTypeConfirm:
		switch r.Action {
		case AgentResumeActionApprove, AgentResumeActionReject, AgentResumeActionCancel:
			return nil
		default:
			return fmt.Errorf("confirm 恢复动作非法: %s", r.Action)
		}
	case AgentResumeTypeAskUser:
		switch r.Action {
		case AgentResumeActionReply, AgentResumeActionCancel:
			return nil
		default:
			return fmt.Errorf("ask_user 恢复动作非法: %s", r.Action)
		}
	case AgentResumeTypeConnectionRecover:
		switch r.Action {
		case AgentResumeActionResume, AgentResumeActionCancel:
			return nil
		default:
			return fmt.Errorf("connection_recover 恢复动作非法: %s", r.Action)
		}
	default:
		return fmt.Errorf("extra.resume.type 非法: %s", r.Type)
	}
}

// IsConfirmResume 判断当前恢复请求是否属于 confirm 分支。
func (r *AgentResumeRequest) IsConfirmResume() bool {
	if r == nil {
		return false
	}
	return r.Type == "" || r.Type == AgentResumeTypeConfirm
}

// IsAskUserResume 判断当前恢复请求是否属于 ask_user 分支。
func (r *AgentResumeRequest) IsAskUserResume() bool {
	if r == nil {
		return false
	}
	return r.Type == AgentResumeTypeAskUser
}

// IsConnectionRecoverResume 判断当前恢复请求是否属于 connection_recover 分支。
func (r *AgentResumeRequest) IsConnectionRecoverResume() bool {
	if r == nil {
		return false
	}
	return r.Type == AgentResumeTypeConnectionRecover
}

type ChatHistoryPersistPayload struct {
	UserID                      int     `json:"user_id"`
	ConversationID              string  `json:"conversation_id"`
	Role                        string  `json:"role"`
	Message                     string  `json:"message"`
	ReasoningContent            string  `json:"reasoning_content,omitempty"`
	ReasoningDurationSeconds    int     `json:"reasoning_duration_seconds,omitempty"`
	RetryGroupID                *string `json:"retry_group_id,omitempty"`
	RetryIndex                  *int    `json:"retry_index,omitempty"`
	RetryFromUserMessageID      *int    `json:"retry_from_user_message_id,omitempty"`
	RetryFromAssistantMessageID *int    `json:"retry_from_assistant_message_id,omitempty"`
	TokensConsumed              int     `json:"tokens_consumed"`
}

type ChatTokenUsageAdjustPayload struct {
	UserID         int       `json:"user_id"`
	ConversationID string    `json:"conversation_id"`
	TokensDelta    int       `json:"tokens_delta"`
	Reason         string    `json:"reason"`
	TriggeredAt    time.Time `json:"triggered_at"`
}

type GetConversationMetaResponse struct {
	ConversationID string     `json:"conversation_id"`
	Title          string     `json:"title"`
	HasTitle       bool       `json:"has_title"`
	MessageCount   int        `json:"message_count"`
	LastMessageAt  *time.Time `json:"last_message_at,omitempty"`
	Status         string     `json:"status"`
}

type GetConversationListItem struct {
	ConversationID string     `json:"conversation_id"`
	Title          string     `json:"title"`
	HasTitle       bool       `json:"has_title"`
	MessageCount   int        `json:"message_count"`
	LastMessageAt  *time.Time `json:"last_message_at,omitempty"`
	Status         string     `json:"status"`
	CreatedAt      *time.Time `json:"created_at,omitempty"`
}

type GetConversationListResponse struct {
	List     []GetConversationListItem `json:"list"`
	Page     int                       `json:"page"`
	PageSize int                       `json:"page_size"`
	Limit    int                       `json:"limit"`
	Total    int64                     `json:"total"`
	HasMore  bool                      `json:"has_more"`
}

type GetConversationHistoryItem struct {
	ID                       int        `json:"id,omitempty"`
	Role                     string     `json:"role"`
	Content                  string     `json:"content"`
	CreatedAt                *time.Time `json:"created_at,omitempty"`
	ReasoningContent         string     `json:"reasoning_content,omitempty"`
	ReasoningDurationSeconds int        `json:"reasoning_duration_seconds,omitempty"`
	RetryGroupID             *string    `json:"retry_group_id"`
	RetryIndex               *int       `json:"retry_index"`
	RetryTotal               *int       `json:"retry_total"`
}

type SchedulePlanPreviewCache struct {
	UserID         int                   `json:"user_id"`
	ConversationID string                `json:"conversation_id"`
	TraceID        string                `json:"trace_id,omitempty"`
	Summary        string                `json:"summary"`
	CandidatePlans []UserWeekSchedule    `json:"candidate_plans"`
	TaskClassIDs   []int                 `json:"task_class_ids,omitempty"`
	HybridEntries  []HybridScheduleEntry `json:"hybrid_entries,omitempty"`
	AllocatedItems []TaskClassItem       `json:"allocated_items,omitempty"`
	GeneratedAt    time.Time             `json:"generated_at"`
}

type GetSchedulePlanPreviewResponse struct {
	ConversationID string                `json:"conversation_id"`
	TraceID        string                `json:"trace_id,omitempty"`
	Summary        string                `json:"summary"`
	CandidatePlans []UserWeekSchedule    `json:"candidate_plans"`
	HybridEntries  []HybridScheduleEntry `json:"hybrid_entries,omitempty"`
	GeneratedAt    time.Time             `json:"generated_at"`
}

type SSEResponse struct {
	Event string         `json:"event"`
	ID    int            `json:"id,omitempty"`
	Retry int64          `json:"retry,omitempty"`
	Data  SSEMessageData `json:"data"`
}

type SSEMessageData struct {
	Step    int    `json:"step,omitempty"`
	Message string `json:"message,omitempty"`
}

type AgentChat struct {
	ID            int64      `gorm:"column:id;primaryKey;autoIncrement;comment:自增ID"`
	ChatID        string     `gorm:"column:chat_id;type:varchar(36);not null;uniqueIndex:uk_chat_id;comment:会话UUID"`
	UserID        int        `gorm:"column:user_id;not null;index:idx_user_last,priority:1;index:idx_user_status,priority:1;comment:所属用户ID"`
	Title         *string    `gorm:"column:title;type:varchar(255);comment:会话标题"`
	SystemPrompt  *string    `gorm:"column:system_prompt;type:text;comment:系统提示词"`
	Model         *string    `gorm:"column:model;type:varchar(100);comment:模型标识"`
	MessageCount  int        `gorm:"column:message_count;not null;default:0;comment:消息总数"`
	TokensTotal   int        `gorm:"column:tokens_total;not null;default:0;comment:累计Token"`
	LastMessageAt *time.Time `gorm:"column:last_message_at;comment:最后消息时间"`
	Status        string     `gorm:"column:status;type:varchar(32);not null;default:active;index:idx_user_status,priority:2;comment:会话状态"`
	CreatedAt     *time.Time `gorm:"column:created_at;autoCreateTime"`
	UpdatedAt     *time.Time `gorm:"column:updated_at;autoUpdateTime"`
	DeletedAt     *time.Time `gorm:"column:deleted_at;comment:软删除时间"`
}

func (AgentChat) TableName() string { return "agent_chats" }

type ChatHistory struct {
	ID                          int        `gorm:"column:id;primaryKey;autoIncrement"`
	ChatID                      string     `gorm:"column:chat_id;type:varchar(36);not null;index:idx_user_chat,priority:2;index:idx_chat_id;comment:会话UUID"`
	UserID                      int        `gorm:"column:user_id;not null;index:idx_user_chat,priority:1"`
	MessageContent              *string    `gorm:"column:message_content;type:text;comment:消息内容"`
	ReasoningContent            *string    `gorm:"column:reasoning_content;type:text;comment:deep reasoning text"`
	ReasoningDurationSeconds    int        `gorm:"column:reasoning_duration_seconds;not null;default:0;comment:deep reasoning duration seconds"`
	RetryGroupID                *string    `gorm:"column:retry_group_id;type:varchar(64);index:idx_retry_group;comment:retry group id"`
	RetryIndex                  *int       `gorm:"column:retry_index;comment:retry page index"`
	RetryFromUserMessageID      *int       `gorm:"column:retry_from_user_message_id;comment:source user message id"`
	RetryFromAssistantMessageID *int       `gorm:"column:retry_from_assistant_message_id;comment:source assistant message id"`
	Role                        *string    `gorm:"column:role;type:varchar(32);comment:消息角色"`
	TokensConsumed              int        `gorm:"column:tokens_consumed;not null;default:0;comment:本轮消耗Token"`
	CreatedAt                   *time.Time `gorm:"column:created_at;autoCreateTime"`

	Chat AgentChat `gorm:"foreignKey:ChatID;references:ChatID;constraint:OnUpdate:CASCADE,OnDelete:CASCADE"`
}

func (ChatHistory) TableName() string { return "chat_histories" }
