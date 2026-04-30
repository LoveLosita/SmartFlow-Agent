package applyadapter

import "time"

const (
	ChangeTypeAddTaskPoolToSchedule = "add_task_pool_to_schedule"
	ChangeTypeCreateMakeup          = "create_makeup"

	changeTypeAdd = "add"

	TargetTypeTaskPool      = "task_pool"
	TargetTypeTaskItem      = "task_item"
	TargetTypeScheduleEvent = "schedule_event"

	scheduleEventTypeTask = "task"
	scheduleStatusNormal  = "normal"

	TaskSourceTypeTaskPool = "task_pool"
	TaskSourceTypeTaskItem = "task_item"
)

const (
	ErrorCodeInvalidRequest         = "invalid_request"
	ErrorCodeUnsupportedChangeType  = "unsupported_change_type"
	ErrorCodeTargetNotFound         = "target_not_found"
	ErrorCodeTargetCompleted        = "target_completed"
	ErrorCodeTargetAlreadyScheduled = "target_already_scheduled"
	ErrorCodeSlotConflict           = "slot_conflict"
	ErrorCodeInvalidEditedChanges   = "invalid_edited_changes"
	ErrorCodeDBError                = "db_error"
)

// ApplyActiveScheduleRequest 是主动调度确认后交给 schedule 域的正式写库请求。
//
// 职责边界：
// 1. 只承载已经由上游 preview/confirm 校验过的用户、候选和变更事实；
// 2. 不负责表达 preview 状态回写，adapter 成功后仅返回正式落库 ID；
// 3. Changes 可以来自原始 preview_changes，也可以来自用户编辑后的 edited_changes。
type ApplyActiveScheduleRequest struct {
	PreviewID   string
	ApplyID     string
	UserID      int
	CandidateID string
	Changes     []ApplyChange
	RequestedAt time.Time
	TraceID     string
}

// ApplyChange 是 apply adapter 可执行的最小变更单元。
//
// 字段语义：
// 1. ChangeType 支持 add_task_pool_to_schedule / create_makeup；
// 2. TargetType + TargetID 描述要落库的任务来源或原日程块；
// 3. ToSlot 是最终确认后的落位节次，adapter 不信任调用方的冲突判断，会在事务内重查。
type ApplyChange struct {
	ChangeID         string
	ChangeType       string
	TargetType       string
	TargetID         int
	ToSlot           *SlotSpan
	DurationSections int
	Metadata         map[string]string
}

// Slot 描述 schedules 表的一格原子节次坐标。
type Slot struct {
	Week      int
	DayOfWeek int
	Section   int
}

// SlotSpan 描述一个连续节次块。
//
// 说明：
// 1. Start 必填；
// 2. End 可由 DurationSections 推导，但调用方传入时必须与 Start 同周同日且连续；
// 3. DurationSections 小于等于 0 时，adapter 会按 Start/End 计算。
type SlotSpan struct {
	Start            Slot
	End              Slot
	DurationSections int
}

// ApplyActiveScheduleResult 是正式日程写库结果。
//
// 职责边界：
// 1. AppliedEventIDs 返回本次新建的 schedule_events.id；
// 2. AppliedScheduleIDs 返回本次新建的 schedules.id；
// 3. 不包含 preview apply_status，避免 adapter 越权回写 active_schedule_previews。
type ApplyActiveScheduleResult struct {
	ApplyID            string
	AppliedEventIDs    []int
	AppliedScheduleIDs []int
}

// ApplyError 是 adapter 返回给上游的可分类业务错误。
//
// 说明：
// 1. Code 用于上游决定 preview apply_error / 交互文案；
// 2. Cause 保留底层错误，便于日志排障；
// 3. Error() 面向调用方，保持中文可读。
type ApplyError struct {
	Code    string
	Message string
	Cause   error
}

func (e *ApplyError) Error() string {
	if e == nil {
		return ""
	}
	if e.Cause == nil {
		return e.Message
	}
	return e.Message + ": " + e.Cause.Error()
}

func (e *ApplyError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Cause
}

func newApplyError(code, message string, cause error) error {
	return &ApplyError{Code: code, Message: message, Cause: cause}
}
