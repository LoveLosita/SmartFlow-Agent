package dao

import (
	"context"
	"errors"
	"time"

	"github.com/LoveLosita/smartflow/backend/services/runtime/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// ActiveScheduleDAO 管理主动调度阶段 1 的自有表。
//
// 职责边界：
// 1. 只负责 active_schedule_jobs / triggers / previews 的基础读写；
// 2. 不负责构造候选、调用 LLM、投递 provider 或写正式日程；
// 3. 幂等查询只按持久化键读取事实，是否复用结果由上层状态机判断。
type ActiveScheduleDAO struct {
	db *gorm.DB
}

func NewActiveScheduleDAO(db *gorm.DB) *ActiveScheduleDAO {
	return &ActiveScheduleDAO{db: db}
}

func (d *ActiveScheduleDAO) WithTx(tx *gorm.DB) *ActiveScheduleDAO {
	return &ActiveScheduleDAO{db: tx}
}

func (d *ActiveScheduleDAO) ensureDB() error {
	if d == nil || d.db == nil {
		return errors.New("active schedule dao 未初始化")
	}
	return nil
}

// CreateOrUpdateJob 按 job.id 幂等创建或覆盖主动调度 job。
//
// 职责边界：
// 1. 只按主键 upsert 当前传入的 job 快照；
// 2. 不判断 task 是否仍满足主动调度条件，该判断由 job scanner 读取 task 真值后完成；
// 3. 调用方需要保证 ID 稳定，例如按 task_id 当前有效 job 或生成 asj_*。
func (d *ActiveScheduleDAO) CreateOrUpdateJob(ctx context.Context, job *model.ActiveScheduleJob) error {
	if err := d.ensureDB(); err != nil {
		return err
	}
	if job == nil || job.ID == "" {
		return errors.New("active schedule job 不能为空且必须包含 id")
	}
	return d.db.WithContext(ctx).
		Clauses(clause.OnConflict{
			Columns:   []clause.Column{{Name: "id"}},
			UpdateAll: true,
		}).
		Create(job).Error
}

// UpdateJobFields 按 job_id 更新指定字段。
//
// 职责边界：
// 1. 只执行局部字段更新，不隐式改变其它状态；
// 2. updates 为空时直接返回 nil，方便上层按条件拼装更新；
// 3. 不做状态机合法性校验，状态流转由 active_scheduler/job 负责。
func (d *ActiveScheduleDAO) UpdateJobFields(ctx context.Context, jobID string, updates map[string]any) error {
	if err := d.ensureDB(); err != nil {
		return err
	}
	if jobID == "" {
		return errors.New("active schedule job id 不能为空")
	}
	if len(updates) == 0 {
		return nil
	}
	return d.db.WithContext(ctx).
		Model(&model.ActiveScheduleJob{}).
		Where("id = ?", jobID).
		Updates(updates).Error
}

func (d *ActiveScheduleDAO) GetJobByID(ctx context.Context, jobID string) (*model.ActiveScheduleJob, error) {
	if err := d.ensureDB(); err != nil {
		return nil, err
	}
	if jobID == "" {
		return nil, gorm.ErrRecordNotFound
	}
	var job model.ActiveScheduleJob
	err := d.db.WithContext(ctx).Where("id = ?", jobID).First(&job).Error
	if err != nil {
		return nil, err
	}
	return &job, nil
}

// FindPendingJobByTask 查询某个 task 当前待触发 job。
//
// 说明：
// 1. 用于 task 创建/更新时决定复用还是覆盖当前有效 job；
// 2. 只查 pending，已 triggered/canceled/skipped 的历史 job 保留审计，不再被覆盖。
func (d *ActiveScheduleDAO) FindPendingJobByTask(ctx context.Context, userID int, taskID int) (*model.ActiveScheduleJob, error) {
	if err := d.ensureDB(); err != nil {
		return nil, err
	}
	if userID <= 0 || taskID <= 0 {
		return nil, gorm.ErrRecordNotFound
	}
	var job model.ActiveScheduleJob
	err := d.db.WithContext(ctx).
		Where("user_id = ? AND task_id = ? AND status = ?", userID, taskID, model.ActiveScheduleJobStatusPending).
		Order("trigger_at ASC, created_at ASC").
		First(&job).Error
	if err != nil {
		return nil, err
	}
	return &job, nil
}

// ListDueJobs 读取到期且仍待触发的 job。
//
// 失败处理：
// 1. 参数非法时返回空列表，避免 worker 因配置抖动误扫全表；
// 2. 数据库错误直接返回，让上层按扫描器策略记录并重试。
func (d *ActiveScheduleDAO) ListDueJobs(ctx context.Context, now time.Time, limit int) ([]model.ActiveScheduleJob, error) {
	if err := d.ensureDB(); err != nil {
		return nil, err
	}
	if limit <= 0 || now.IsZero() {
		return []model.ActiveScheduleJob{}, nil
	}
	var jobs []model.ActiveScheduleJob
	err := d.db.WithContext(ctx).
		Where("status = ? AND trigger_at <= ?", model.ActiveScheduleJobStatusPending, now).
		Order("trigger_at ASC, id ASC").
		Limit(limit).
		Find(&jobs).Error
	if err != nil {
		return nil, err
	}
	return jobs, nil
}

func (d *ActiveScheduleDAO) CreateTrigger(ctx context.Context, trigger *model.ActiveScheduleTrigger) error {
	if err := d.ensureDB(); err != nil {
		return err
	}
	if trigger == nil || trigger.ID == "" {
		return errors.New("active schedule trigger 不能为空且必须包含 id")
	}
	return d.db.WithContext(ctx).Create(trigger).Error
}

// UpdateTriggerFields 按 trigger_id 局部更新触发状态。
//
// 职责边界：
// 1. 只提供字段更新能力，不判断 pending -> processing -> preview_generated 是否合规；
// 2. 上层若需要 CAS 状态流转，应在 updates 外自行加 where 条件或后续扩展专用方法；
// 3. updates 为空时直接返回 nil。
func (d *ActiveScheduleDAO) UpdateTriggerFields(ctx context.Context, triggerID string, updates map[string]any) error {
	if err := d.ensureDB(); err != nil {
		return err
	}
	if triggerID == "" {
		return errors.New("active schedule trigger id 不能为空")
	}
	if len(updates) == 0 {
		return nil
	}
	return d.db.WithContext(ctx).
		Model(&model.ActiveScheduleTrigger{}).
		Where("id = ?", triggerID).
		Updates(updates).Error
}

func (d *ActiveScheduleDAO) GetTriggerByID(ctx context.Context, triggerID string) (*model.ActiveScheduleTrigger, error) {
	if err := d.ensureDB(); err != nil {
		return nil, err
	}
	if triggerID == "" {
		return nil, gorm.ErrRecordNotFound
	}
	var trigger model.ActiveScheduleTrigger
	err := d.db.WithContext(ctx).Where("id = ?", triggerID).First(&trigger).Error
	if err != nil {
		return nil, err
	}
	return &trigger, nil
}

// FindTriggerByDedupeKey 查询触发去重键对应的最近 trigger。
//
// 说明：
// 1. important_urgent_task 使用 user_id + trigger_type + target + 30 分钟窗口构造 dedupe_key；
// 2. unfinished_feedback 可把反馈幂等键放入 dedupe_key；
// 3. statuses 为空时读取所有状态，方便调用方按场景选择是否复用 failed 记录。
func (d *ActiveScheduleDAO) FindTriggerByDedupeKey(ctx context.Context, dedupeKey string, statuses []string) (*model.ActiveScheduleTrigger, error) {
	if err := d.ensureDB(); err != nil {
		return nil, err
	}
	if dedupeKey == "" {
		return nil, gorm.ErrRecordNotFound
	}
	query := d.db.WithContext(ctx).
		Where("dedupe_key = ?", dedupeKey)
	if len(statuses) > 0 {
		query = query.Where("status IN ?", statuses)
	}
	var trigger model.ActiveScheduleTrigger
	err := query.Order("created_at DESC, id DESC").First(&trigger).Error
	if err != nil {
		return nil, err
	}
	return &trigger, nil
}

// FindTriggerByIdempotencyKey 查询 API/用户反馈幂等键对应的 trigger。
func (d *ActiveScheduleDAO) FindTriggerByIdempotencyKey(ctx context.Context, userID int, triggerType string, idempotencyKey string) (*model.ActiveScheduleTrigger, error) {
	if err := d.ensureDB(); err != nil {
		return nil, err
	}
	if userID <= 0 || triggerType == "" || idempotencyKey == "" {
		return nil, gorm.ErrRecordNotFound
	}
	var trigger model.ActiveScheduleTrigger
	err := d.db.WithContext(ctx).
		Where("user_id = ? AND trigger_type = ? AND idempotency_key = ?", userID, triggerType, idempotencyKey).
		Order("created_at DESC, id DESC").
		First(&trigger).Error
	if err != nil {
		return nil, err
	}
	return &trigger, nil
}

func (d *ActiveScheduleDAO) CreatePreview(ctx context.Context, preview *model.ActiveSchedulePreview) error {
	if err := d.ensureDB(); err != nil {
		return err
	}
	if preview == nil || preview.ID == "" {
		return errors.New("active schedule preview 不能为空且必须包含 preview_id")
	}
	return d.db.WithContext(ctx).Create(preview).Error
}

func (d *ActiveScheduleDAO) UpdatePreviewFields(ctx context.Context, previewID string, updates map[string]any) error {
	if err := d.ensureDB(); err != nil {
		return err
	}
	if previewID == "" {
		return errors.New("active schedule preview id 不能为空")
	}
	if len(updates) == 0 {
		return nil
	}
	return d.db.WithContext(ctx).
		Model(&model.ActiveSchedulePreview{}).
		Where("preview_id = ?", previewID).
		Updates(updates).Error
}

func (d *ActiveScheduleDAO) GetPreviewByID(ctx context.Context, previewID string) (*model.ActiveSchedulePreview, error) {
	if err := d.ensureDB(); err != nil {
		return nil, err
	}
	if previewID == "" {
		return nil, gorm.ErrRecordNotFound
	}
	var preview model.ActiveSchedulePreview
	err := d.db.WithContext(ctx).Where("preview_id = ?", previewID).First(&preview).Error
	if err != nil {
		return nil, err
	}
	return &preview, nil
}

func (d *ActiveScheduleDAO) GetPreviewByTriggerID(ctx context.Context, triggerID string) (*model.ActiveSchedulePreview, error) {
	if err := d.ensureDB(); err != nil {
		return nil, err
	}
	if triggerID == "" {
		return nil, gorm.ErrRecordNotFound
	}
	var preview model.ActiveSchedulePreview
	err := d.db.WithContext(ctx).
		Where("trigger_id = ?", triggerID).
		Order("created_at DESC").
		First(&preview).Error
	if err != nil {
		return nil, err
	}
	return &preview, nil
}

// FindPreviewByApplyIdempotencyKey 查询 confirm 重试时的预览应用状态。
func (d *ActiveScheduleDAO) FindPreviewByApplyIdempotencyKey(ctx context.Context, previewID string, idempotencyKey string) (*model.ActiveSchedulePreview, error) {
	if err := d.ensureDB(); err != nil {
		return nil, err
	}
	if previewID == "" || idempotencyKey == "" {
		return nil, gorm.ErrRecordNotFound
	}
	var preview model.ActiveSchedulePreview
	err := d.db.WithContext(ctx).
		Where("preview_id = ? AND apply_idempotency_key = ?", previewID, idempotencyKey).
		First(&preview).Error
	if err != nil {
		return nil, err
	}
	return &preview, nil
}
