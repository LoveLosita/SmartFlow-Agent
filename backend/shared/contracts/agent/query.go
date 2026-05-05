package agent

// ConversationQueryRequest 描述 gateway 查询单个 agent 会话资源的最小跨进程参数。
//
// 职责边界：
// 1. UserID 由 gateway 鉴权后填充，不信任前端传入；
// 2. ConversationID 只表达会话归属，不承载 HTTP query 或 SSE 细节；
// 3. 会话是否存在、是否属于当前用户仍由 agent 服务内部校验。
type ConversationQueryRequest struct {
	UserID         int    `json:"user_id"`
	ConversationID string `json:"conversation_id"`
}

// ConversationListRequest 描述 gateway 拉取当前用户会话列表的最小查询条件。
//
// 职责边界：
// 1. Page/PageSize 允许为 0，默认值和上限由 agent 服务统一兜底；
// 2. Status 只透传过滤条件，合法值仍由 agent 服务决定；
// 3. 不包含消息正文，避免列表接口被扩成重查询。
type ConversationListRequest struct {
	UserID   int    `json:"user_id"`
	Page     int    `json:"page,omitempty"`
	PageSize int    `json:"page_size,omitempty"`
	Status   string `json:"status,omitempty"`
}

// SaveScheduleStatePlacedItem 描述前端拖拽后的单个 task_item 绝对时间位置。
//
// 职责边界：
// 1. 字段形状与 HTTP 入参保持一致，避免 gateway 做额外翻译；
// 2. 这里只承载跨进程 JSON 契约，不判断节次、周次或 task_item 是否有效；
// 3. agent 服务会把绝对坐标转换为内部 ScheduleState 相对坐标。
type SaveScheduleStatePlacedItem struct {
	TaskItemID         int `json:"task_item_id"`
	Week               int `json:"week"`
	DayOfWeek          int `json:"day_of_week"`
	StartSection       int `json:"start_section"`
	EndSection         int `json:"end_section"`
	EmbedCourseEventID int `json:"embed_course_event_id,omitempty"`
}

// SaveScheduleStateRequest 描述 gateway 暂存会话内排程拖拽状态的跨进程命令。
//
// 职责边界：
// 1. UserID 由 gateway 注入，用于 agent 服务做会话归属校验；
// 2. ConversationID 指向当前会话快照；
// 3. Items 只包含用户拖拽后的 task_item 位置，不承载课程写入或正式确认语义。
type SaveScheduleStateRequest struct {
	UserID         int                           `json:"user_id"`
	ConversationID string                        `json:"conversation_id"`
	Items          []SaveScheduleStatePlacedItem `json:"items"`
}
