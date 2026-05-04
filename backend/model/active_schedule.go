package model

import (
	"time"

	"gorm.io/gorm"
)

const (
	// ActiveScheduleJobStatusPending 表示 job 已创建，等待到达 trigger_at 后扫描。
	ActiveScheduleJobStatusPending = "pending"
	// ActiveScheduleJobStatusTriggered 表示 job 已生成正式 trigger，后续由 trigger 串联状态。
	ActiveScheduleJobStatusTriggered = "triggered"
	// ActiveScheduleJobStatusCanceled 表示任务已完成或被取消，job 不再触发。
	ActiveScheduleJobStatusCanceled = "canceled"
	// ActiveScheduleJobStatusSkipped 表示扫描时发现已无需主动调度。
	ActiveScheduleJobStatusSkipped = "skipped"
	// ActiveScheduleJobStatusFailed 表示扫描或触发写入失败，保留错误供重试/排障。
	ActiveScheduleJobStatusFailed = "failed"
)

const (
	// ActiveScheduleTriggerStatusPending 表示触发信号已持久化，等待 worker 消费。
	ActiveScheduleTriggerStatusPending = "pending"
	// ActiveScheduleTriggerStatusProcessing 表示 worker 正在处理该触发信号。
	ActiveScheduleTriggerStatusProcessing = "processing"
	// ActiveScheduleTriggerStatusPreviewGenerated 表示已生成可查询的预览。
	ActiveScheduleTriggerStatusPreviewGenerated = "preview_generated"
	// ActiveScheduleTriggerStatusSkipped 表示本次触发被判定无需继续处理。
	ActiveScheduleTriggerStatusSkipped = "skipped"
	// ActiveScheduleTriggerStatusClosed 表示主动观测结论为关闭，不生成预览。
	ActiveScheduleTriggerStatusClosed = "closed"
	// ActiveScheduleTriggerStatusFailed 表示链路处理失败，可根据错误分类决定是否重试。
	ActiveScheduleTriggerStatusFailed = "failed"
	// ActiveScheduleTriggerStatusRejected 表示参数或归属校验失败，不进入 pipeline。
	ActiveScheduleTriggerStatusRejected = "rejected"
)

const (
	// ActiveSchedulePreviewStatusPending 表示预览正在组装，不应展示为可确认。
	ActiveSchedulePreviewStatusPending = "pending"
	// ActiveSchedulePreviewStatusReady 表示预览可查看、可确认。
	ActiveSchedulePreviewStatusReady = "ready"
	// ActiveSchedulePreviewStatusApplied 表示用户已确认并成功应用。
	ActiveSchedulePreviewStatusApplied = "applied"
	// ActiveSchedulePreviewStatusIgnored 表示用户明确忽略本次建议。
	ActiveSchedulePreviewStatusIgnored = "ignored"
	// ActiveSchedulePreviewStatusExpired 表示预览已过期，不再允许确认。
	ActiveSchedulePreviewStatusExpired = "expired"
	// ActiveSchedulePreviewStatusFailed 表示预览生成或回写失败。
	ActiveSchedulePreviewStatusFailed = "failed"
)

const (
	// ActiveScheduleApplyStatusNone 表示尚未发起确认应用。
	ActiveScheduleApplyStatusNone = "none"
	// ActiveScheduleApplyStatusApplying 表示确认请求正在事务应用中。
	ActiveScheduleApplyStatusApplying = "applying"
	// ActiveScheduleApplyStatusApplied 表示确认应用成功。
	ActiveScheduleApplyStatusApplied = "applied"
	// ActiveScheduleApplyStatusFailed 表示应用失败，正式日程不应产生半写状态。
	ActiveScheduleApplyStatusFailed = "failed"
	// ActiveScheduleApplyStatusRejected 表示请求因过期、幂等冲突等业务规则被拒绝。
	ActiveScheduleApplyStatusRejected = "rejected"
	// ActiveScheduleApplyStatusExpired 表示预览过期导致不可应用。
	ActiveScheduleApplyStatusExpired = "expired"
)

const (
	// ActiveScheduleTriggerTypeImportantUrgentTask 是重要且紧急任务到线触发。
	ActiveScheduleTriggerTypeImportantUrgentTask = "important_urgent_task"
	// ActiveScheduleTriggerTypeUnfinishedFeedback 是用户明确反馈已排任务未完成触发。
	ActiveScheduleTriggerTypeUnfinishedFeedback = "unfinished_feedback"

	// ActiveScheduleSourceWorkerDueJob 表示后台到期 job 扫描触发。
	ActiveScheduleSourceWorkerDueJob = "worker_due_job"
	// ActiveScheduleSourceAPITrigger 表示测试/开发 API 正式触发。
	ActiveScheduleSourceAPITrigger = "api_trigger"
	// ActiveScheduleSourceAPIDryRun 表示测试/开发 API dry-run，不应发布正式事件。
	ActiveScheduleSourceAPIDryRun = "api_dry_run"
	// ActiveScheduleSourceUserFeedback 表示用户反馈入口触发。
	ActiveScheduleSourceUserFeedback = "user_feedback"

	// ActiveScheduleTargetTypeTaskPool 表示 target_id 指向 tasks.id。
	ActiveScheduleTargetTypeTaskPool = "task_pool"
	// ActiveScheduleTargetTypeScheduleEvent 表示 target_id 指向 schedule_events.id。
	ActiveScheduleTargetTypeScheduleEvent = "schedule_event"
	// ActiveScheduleTargetTypeTaskItem 表示 target_id 指向 task_items.id。
	ActiveScheduleTargetTypeTaskItem = "task_item"
)

// ActiveScheduleJob 是主动调度 due job 表模型。
//
// 职责边界：
// 1. 负责记录 task 到达 urgency_threshold_at 后是否需要生成主动调度触发；
// 2. 不负责判断 task 当前是否仍重要且紧急，该判断由 worker 扫描时重新读取真实任务状态；
// 3. 不负责发布 outbox 事件，只保存扫描和排障所需状态。
type ActiveScheduleJob struct {
	ID string `gorm:"column:id;type:varchar(64);primaryKey"`

	UserID      int       `gorm:"column:user_id;not null;index:idx_active_jobs_user_status_trigger,priority:1;index:idx_active_jobs_task_status,priority:1"`
	TaskID      int       `gorm:"column:task_id;not null;index:idx_active_jobs_task_status,priority:2;comment:对应 tasks.id"`
	TriggerType string    `gorm:"column:trigger_type;type:varchar(64);not null;default:'important_urgent_task';comment:触发类型"`
	Status      string    `gorm:"column:status;type:varchar(32);not null;default:'pending';index:idx_active_jobs_user_status_trigger,priority:2;index:idx_active_jobs_task_status,priority:3;comment:pending/triggered/canceled/skipped/failed"`
	TriggerAt   time.Time `gorm:"column:trigger_at;not null;index:idx_active_jobs_user_status_trigger,priority:3;comment:到期触发时间"`

	DedupeKey     string     `gorm:"column:dedupe_key;type:varchar(191);index:idx_active_jobs_dedupe;comment:触发去重窗口键"`
	LastTriggerID *string    `gorm:"column:last_trigger_id;type:varchar(64);index:idx_active_jobs_last_trigger;comment:最近一次生成的 trigger_id"`
	LastErrorCode *string    `gorm:"column:last_error_code;type:varchar(64);comment:最近一次扫描错误码"`
	LastError     *string    `gorm:"column:last_error;type:text;comment:最近一次扫描错误详情"`
	LastScannedAt *time.Time `gorm:"column:last_scanned_at;comment:最近一次被 worker 扫描时间"`
	TraceID       string     `gorm:"column:trace_id;type:varchar(64);index:idx_active_jobs_trace_id"`

	CreatedAt time.Time      `gorm:"column:created_at;autoCreateTime"`
	UpdatedAt time.Time      `gorm:"column:updated_at;autoUpdateTime"`
	DeletedAt gorm.DeletedAt `gorm:"column:deleted_at;index"`
}

func (ActiveScheduleJob) TableName() string { return "active_schedule_jobs" }

// ActiveScheduleTrigger 是主动调度统一触发信号表模型。
//
// 职责边界：
// 1. 负责持久化 worker/API/用户反馈归一后的触发事实；
// 2. 负责串联 trigger -> preview -> notification -> apply 的审计主线；
// 3. 不承载候选生成、LLM 选择或通知投递的业务实现。
type ActiveScheduleTrigger struct {
	ID string `gorm:"column:id;type:varchar(64);primaryKey"`

	UserID         int        `gorm:"column:user_id;not null;index:idx_active_triggers_user_created,priority:1"`
	TriggerType    string     `gorm:"column:trigger_type;type:varchar(64);not null;index:idx_active_triggers_dedupe,priority:2"`
	Source         string     `gorm:"column:source;type:varchar(64);not null;comment:worker_due_job/api_trigger/api_dry_run/user_feedback"`
	TargetType     string     `gorm:"column:target_type;type:varchar(64);not null;index:idx_active_triggers_target,priority:1"`
	TargetID       int        `gorm:"column:target_id;not null;index:idx_active_triggers_target,priority:2"`
	FeedbackID     string     `gorm:"column:feedback_id;type:varchar(128);index:idx_active_triggers_feedback;comment:用户反馈来源ID，可为空"`
	JobID          *string    `gorm:"column:job_id;type:varchar(64);index:idx_active_triggers_job_id"`
	IdempotencyKey string     `gorm:"column:idempotency_key;type:varchar(191);index:idx_active_triggers_idempotency;comment:API/用户反馈幂等键"`
	DedupeKey      string     `gorm:"column:dedupe_key;type:varchar(191);index:idx_active_triggers_dedupe,priority:1;comment:触发去重窗口键"`
	Status         string     `gorm:"column:status;type:varchar(32);not null;default:'pending';index:idx_active_triggers_status_updated,priority:1"`
	MockNow        *time.Time `gorm:"column:mock_now;comment:测试触发模拟时间"`
	IsMockTime     bool       `gorm:"column:is_mock_time;not null;default:false;comment:是否使用模拟时间"`
	RequestedAt    time.Time  `gorm:"column:requested_at;not null;comment:触发请求时间"`
	PayloadJSON    *string    `gorm:"column:payload_json;type:json;comment:触发来源补充信息"`
	PreviewID      *string    `gorm:"column:preview_id;type:varchar(64);index:idx_active_triggers_preview_id"`
	LastErrorCode  *string    `gorm:"column:last_error_code;type:varchar(64);comment:链路错误码"`
	LastError      *string    `gorm:"column:last_error;type:text;comment:链路错误详情"`
	ProcessedAt    *time.Time `gorm:"column:processed_at;comment:worker 开始处理时间"`
	CompletedAt    *time.Time `gorm:"column:completed_at;comment:本触发进入终态时间"`
	TraceID        string     `gorm:"column:trace_id;type:varchar(64);index:idx_active_triggers_trace_id"`

	CreatedAt time.Time      `gorm:"column:created_at;autoCreateTime;index:idx_active_triggers_user_created,priority:2"`
	UpdatedAt time.Time      `gorm:"column:updated_at;autoUpdateTime;index:idx_active_triggers_status_updated,priority:2"`
	DeletedAt gorm.DeletedAt `gorm:"column:deleted_at;index"`
}

func (ActiveScheduleTrigger) TableName() string { return "active_schedule_triggers" }

// ActiveSchedulePreview 是主动调度可确认预览表模型。
//
// 职责边界：
// 1. 负责保存主动调度生成的候选、解释、before/after 摘要与过期时间；
// 2. 负责保存一次确认应用的轻量状态，不新增 apply request 表；
// 3. 不负责正式日程写入，正式写入仍由后续 apply/service port 完成。
type ActiveSchedulePreview struct {
	ID string `gorm:"column:preview_id;type:varchar(64);primaryKey;uniqueIndex:uk_active_previews_apply_idempotency,priority:1"`

	UserID      int    `gorm:"column:user_id;not null;index:idx_active_previews_user_created_at,priority:1"`
	TriggerID   string `gorm:"column:trigger_id;type:varchar(64);not null;index:idx_active_previews_trigger_id"`
	TriggerType string `gorm:"column:trigger_type;type:varchar(64);not null"`
	TargetType  string `gorm:"column:target_type;type:varchar(64);not null"`
	TargetID    int    `gorm:"column:target_id;not null"`
	Status      string `gorm:"column:status;type:varchar(32);not null;default:'pending';comment:pending/ready/applied/ignored/expired/failed"`

	SelectedCandidateID   string    `gorm:"column:selected_candidate_id;type:varchar(64);comment:LLM 或后端 fallback 选中的候选ID"`
	CandidateCount        int       `gorm:"column:candidate_count;not null;default:0"`
	SelectedCandidateJSON *string   `gorm:"column:selected_candidate_json;type:json"`
	CandidatesJSON        *string   `gorm:"column:candidates_json;type:json"`
	DecisionJSON          *string   `gorm:"column:decision_json;type:json"`
	MetricsJSON           *string   `gorm:"column:metrics_json;type:json"`
	IssuesJSON            *string   `gorm:"column:issues_json;type:json"`
	ContextSummaryJSON    *string   `gorm:"column:context_summary_json;type:json"`
	BeforeSummaryJSON     *string   `gorm:"column:before_summary_json;type:json"`
	PreviewChangesJSON    *string   `gorm:"column:preview_changes_json;type:json"`
	AfterSummaryJSON      *string   `gorm:"column:after_summary_json;type:json"`
	RiskJSON              *string   `gorm:"column:risk_json;type:json"`
	ExplanationText       string    `gorm:"column:explanation_text;type:text"`
	NotificationSummary   string    `gorm:"column:notification_summary;type:text"`
	BaseVersion           string    `gorm:"column:base_version;type:varchar(128);not null;comment:确认前重校验基准版本"`
	ExpiresAt             time.Time `gorm:"column:expires_at;not null;index:idx_active_previews_expires_at"`
	GeneratedAt           time.Time `gorm:"column:generated_at;not null"`

	ApplyID             *string    `gorm:"column:apply_id;type:varchar(64);index:idx_active_previews_apply_id"`
	ApplyStatus         string     `gorm:"column:apply_status;type:varchar(32);not null;default:'none';comment:none/applying/applied/failed/rejected/expired"`
	ApplyCandidateID    string     `gorm:"column:apply_candidate_id;type:varchar(64)"`
	ApplyIdempotencyKey string     `gorm:"column:apply_idempotency_key;type:varchar(191);uniqueIndex:uk_active_previews_apply_idempotency,priority:2"`
	ApplyRequestHash    string     `gorm:"column:apply_request_hash;type:varchar(128);comment:确认请求体摘要"`
	AppliedChangesJSON  *string    `gorm:"column:applied_changes_json;type:json"`
	AppliedEventIDsJSON *string    `gorm:"column:applied_event_ids_json;type:json"`
	ApplyError          *string    `gorm:"column:apply_error;type:text"`
	AppliedAt           *time.Time `gorm:"column:applied_at"`
	TraceID             string     `gorm:"column:trace_id;type:varchar(64);index:idx_active_previews_trace_id"`

	CreatedAt time.Time      `gorm:"column:created_at;autoCreateTime;index:idx_active_previews_user_created_at,priority:2"`
	UpdatedAt time.Time      `gorm:"column:updated_at;autoUpdateTime"`
	DeletedAt gorm.DeletedAt `gorm:"column:deleted_at;index"`
}

func (ActiveSchedulePreview) TableName() string { return "active_schedule_previews" }
