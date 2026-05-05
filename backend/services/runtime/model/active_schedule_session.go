package model

import "time"

const (
	// ActiveScheduleSessionStatusWaitingUserReply 表示当前会话正在等待用户补充信息，后端应拦截普通聊天。
	ActiveScheduleSessionStatusWaitingUserReply = "waiting_user_reply"
	// ActiveScheduleSessionStatusRerunning 表示用户已回复，主动调度图正在重跑，后端仍需拦截普通聊天。
	ActiveScheduleSessionStatusRerunning = "rerunning"
	// ActiveScheduleSessionStatusReadyPreview 表示已生成可展示预览，当前会话可以释放回普通聊天。
	ActiveScheduleSessionStatusReadyPreview = "ready_preview"
	// ActiveScheduleSessionStatusApplied 表示用户已确认应用，主动调度会话已经收口。
	ActiveScheduleSessionStatusApplied = "applied"
	// ActiveScheduleSessionStatusIgnored 表示用户明确忽略本次建议。
	ActiveScheduleSessionStatusIgnored = "ignored"
	// ActiveScheduleSessionStatusExpired 表示会话已过期，不再承担路由管辖权。
	ActiveScheduleSessionStatusExpired = "expired"
	// ActiveScheduleSessionStatusFailed 表示会话在绑定、重跑或写回过程中失败。
	ActiveScheduleSessionStatusFailed = "failed"
)

// ActiveScheduleSession 是“主动调度会话路由桥”的持久化模型。
//
// 职责边界：
// 1. 只保存会话级路由权与轻量状态，不承载 preview 主表的完整业务内容；
// 2. conversation_id 允许在通知前为空，等站内会话绑定完成后再写入；
// 3. state_json 只存轻量状态，避免把重对象和历史消息继续塞进 session 表。
type ActiveScheduleSession struct {
	SessionID string `gorm:"column:session_id;type:varchar(64);primaryKey"`

	// 1. user_id + conversation_id 用于在聊天入口侧定位当前管辖中的主动调度会话。
	// 2. conversation_id 允许为空，因此这里使用可空列，方便先建 session 再绑定会话。
	UserID         int     `gorm:"column:user_id;not null;index:idx_active_schedule_sessions_user_conv,priority:1;index:idx_active_schedule_sessions_user_status_updated,priority:1"`
	ConversationID *string `gorm:"column:conversation_id;type:varchar(128);index:idx_active_schedule_sessions_user_conv,priority:2;index:idx_active_schedule_sessions_conversation_status_updated,priority:1"`

	// 3. trigger_id / current_preview_id 分别串起触发源与当前预览，方便后续审计和回放。
	TriggerID        string  `gorm:"column:trigger_id;type:varchar(64);not null;index:idx_active_schedule_sessions_trigger_id"`
	CurrentPreviewID *string `gorm:"column:current_preview_id;type:varchar(64);index:idx_active_schedule_sessions_preview_id"`
	Status           string  `gorm:"column:status;type:varchar(32);not null;default:'waiting_user_reply';index:idx_active_schedule_sessions_user_status_updated,priority:2;index:idx_active_schedule_sessions_status_updated,priority:1;index:idx_active_schedule_sessions_conversation_status_updated,priority:2"`
	StateJSON        string  `gorm:"column:state_json;type:json;not null"`

	CreatedAt time.Time `gorm:"column:created_at;autoCreateTime"`
	UpdatedAt time.Time `gorm:"column:updated_at;autoUpdateTime;index:idx_active_schedule_sessions_user_status_updated,priority:3;index:idx_active_schedule_sessions_status_updated,priority:2;index:idx_active_schedule_sessions_conversation_status_updated,priority:3"`
}

// TableName 返回主动调度会话表名。
func (ActiveScheduleSession) TableName() string {
	return "active_schedule_sessions"
}

// ActiveScheduleSessionState 是 session 表内 state_json 对应的轻量状态。
//
// 职责边界：
// 1. 这里只放“路由和补信息闭环”需要的少量字段；
// 2. 不承载完整 preview、正文历史或大块工具结果；
// 3. 便于 cache / DAO 之间直接复用同一份 JSON 语义。
type ActiveScheduleSessionState struct {
	PendingQuestion    string     `json:"pending_question,omitempty"`
	MissingInfo        []string   `json:"missing_info,omitempty"`
	LastCandidateID    string     `json:"last_candidate_id,omitempty"`
	LastNotificationID string     `json:"last_notification_id,omitempty"`
	ExpiresAt          *time.Time `json:"expires_at,omitempty"`
	FailedReason       string     `json:"failed_reason,omitempty"`
}

// ActiveScheduleSessionSnapshot 是 service、DAO、cache 之间共享的会话快照 DTO。
//
// 职责边界：
// 1. 负责在三层之间传递强类型会话状态；
// 2. 不负责业务决策，不负责拦截判定；
// 3. DAO 再把它拆成数据库列和 state_json，cache 则直接按 JSON 存取。
type ActiveScheduleSessionSnapshot struct {
	SessionID        string
	UserID           int
	ConversationID   string
	TriggerID        string
	CurrentPreviewID string
	Status           string
	State            ActiveScheduleSessionState
	CreatedAt        time.Time
	UpdatedAt        time.Time
}
