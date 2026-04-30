package preview

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/LoveLosita/smartflow/backend/active_scheduler/candidate"
	schedulercontext "github.com/LoveLosita/smartflow/backend/active_scheduler/context"
	"github.com/LoveLosita/smartflow/backend/active_scheduler/observe"
	"github.com/LoveLosita/smartflow/backend/model"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

var (
	ErrInvalidPreviewRequest = errors.New("主动调度预览请求不合法")
	ErrPreviewNotFound       = errors.New("主动调度预览不存在")
)

// Repository 是 preview service 依赖的最小持久化端口。
//
// 职责边界：
// 1. 只覆盖本轮 preview 写入和详情查询需要的方法；
// 2. 不暴露正式日程写入、通知投递或 confirm apply 能力；
// 3. 现有 dao.ActiveScheduleDAO 已满足该接口，后续迁移独立 repo 时可并行替换实现。
type Repository interface {
	CreatePreview(ctx context.Context, preview *model.ActiveSchedulePreview) error
	GetPreviewByID(ctx context.Context, previewID string) (*model.ActiveSchedulePreview, error)
}

// Service 负责主动调度 preview 的写入和查询。
//
// 职责边界：
// 1. 将 dry-run 结果固化为 active_schedule_previews 中的 ready 快照；
// 2. 查询时校验 user_id，并返回 API 可直接透传的详情 DTO；
// 3. 不正式写日程、不发通知、不处理 confirm/apply，也不修改 trigger 状态。
type Service struct {
	repo  Repository
	clock func() time.Time
}

func NewService(repo Repository) (*Service, error) {
	if repo == nil {
		return nil, fmt.Errorf("%w: preview repository 不能为空", ErrInvalidPreviewRequest)
	}
	return &Service{repo: repo, clock: time.Now}, nil
}

// SetClock 注入测试时钟。
//
// 职责边界：
// 1. 只影响 generated_at / expires_at 和查询时的 expired 计算；
// 2. 不改写 dry-run 上下文中的业务当前时间；
// 3. clock 为空时保持原时钟，避免运行期误注入导致 panic。
func (s *Service) SetClock(clock func() time.Time) {
	if s == nil || clock == nil {
		return
	}
	s.clock = clock
}

// CreatePreview 把 dry-run 结果保存为 ready preview。
//
// 职责边界：
// 1. 只消费已经完成的 dry-run 结果，不重新读取任务/日程事实；
// 2. MVP 没有 LLM 选择器，固定使用后端排序后的 top1 candidate 作为 selected_candidate；
// 3. 写库后只返回详情 DTO，不发布通知、不正式应用候选、不回写 trigger。
func (s *Service) CreatePreview(ctx context.Context, req CreatePreviewRequest) (*CreatePreviewResponse, error) {
	if s == nil || s.repo == nil {
		return nil, fmt.Errorf("%w: preview service 未初始化", ErrInvalidPreviewRequest)
	}
	if req.ActiveContext == nil {
		return nil, fmt.Errorf("%w: dry-run 结果不能为空", ErrInvalidPreviewRequest)
	}
	if len(req.Candidates) == 0 {
		return nil, fmt.Errorf("%w: dry-run 未生成可保存候选", ErrInvalidPreviewRequest)
	}

	activeContext := req.ActiveContext
	triggerID := strings.TrimSpace(req.TriggerID)
	if triggerID == "" {
		triggerID = strings.TrimSpace(activeContext.Trigger.TriggerID)
	}
	if triggerID == "" {
		return nil, fmt.Errorf("%w: trigger_id 不能为空", ErrInvalidPreviewRequest)
	}

	generatedAt := req.GeneratedAt
	if generatedAt.IsZero() {
		generatedAt = s.now()
	}
	previewID := strings.TrimSpace(req.PreviewID)
	if previewID == "" {
		previewID = "asp_" + uuid.NewString()
	}

	// 1. 先构造所有展示快照，再写库；任何 JSON 转换失败都提前返回，避免落入半结构化记录。
	selected := req.Candidates[0]
	snapshot := buildSnapshot(activeContext, req.Observation, req.Candidates, selected)
	baseVersion := strings.TrimSpace(req.BaseVersion)
	if baseVersion == "" {
		baseVersion = buildBaseVersion(activeContext, snapshot.changes)
	}

	explanation := strings.TrimSpace(req.ExplanationText)
	if explanation == "" {
		explanation = selected.Summary
	}
	notificationSummary := strings.TrimSpace(req.NotificationSummary)
	if notificationSummary == "" {
		notificationSummary = selected.Summary
	}

	row, err := buildPreviewModel(previewID, triggerID, generatedAt, baseVersion, explanation, notificationSummary, activeContext, snapshot)
	if err != nil {
		return nil, err
	}

	// 2. 写入 active_schedule_previews。这里不包事务写其它表，因为本服务不负责 trigger/notification/apply 状态推进。
	if err := s.repo.CreatePreview(ctx, row); err != nil {
		return nil, err
	}

	detail, err := detailFromModel(row, s.now())
	if err != nil {
		return nil, err
	}
	return &CreatePreviewResponse{Detail: detail}, nil
}

// GetPreview 查询 preview 详情，并强制校验归属用户。
//
// 职责边界：
// 1. preview_id 不存在或不属于 user_id 时统一返回 ErrPreviewNotFound，避免泄漏其它用户数据；
// 2. 查询不会把过期 preview 回写为 expired，过期状态仅在 DTO 中计算；
// 3. 不读取正式日程实时状态，因此不会触发 confirm 的 base_version 重校验。
func (s *Service) GetPreview(ctx context.Context, userID int, previewID string) (*ActiveSchedulePreviewDetail, error) {
	if s == nil || s.repo == nil {
		return nil, fmt.Errorf("%w: preview service 未初始化", ErrInvalidPreviewRequest)
	}
	if userID <= 0 || strings.TrimSpace(previewID) == "" {
		return nil, ErrPreviewNotFound
	}

	row, err := s.repo.GetPreviewByID(ctx, strings.TrimSpace(previewID))
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrPreviewNotFound
		}
		return nil, err
	}
	if row == nil || row.UserID != userID {
		return nil, ErrPreviewNotFound
	}

	detail, err := detailFromModel(row, s.now())
	if err != nil {
		return nil, err
	}
	return &detail, nil
}

func (s *Service) now() time.Time {
	if s == nil || s.clock == nil {
		return time.Now()
	}
	return s.clock()
}

func buildPreviewModel(
	previewID string,
	triggerID string,
	generatedAt time.Time,
	baseVersion string,
	explanation string,
	notificationSummary string,
	activeContext *schedulercontext.ActiveScheduleContext,
	snapshot rawPreviewSnapshot,
) (*model.ActiveSchedulePreview, error) {
	selectedJSON, err := jsonString(snapshot.selectedCandidate)
	if err != nil {
		return nil, err
	}
	candidatesJSON, err := jsonString(snapshot.candidates)
	if err != nil {
		return nil, err
	}
	decisionJSON, err := jsonString(snapshot.decision)
	if err != nil {
		return nil, err
	}
	metricsJSON, err := jsonString(snapshot.metrics)
	if err != nil {
		return nil, err
	}
	issuesJSON, err := jsonString(snapshot.issues)
	if err != nil {
		return nil, err
	}
	contextJSON, err := jsonString(snapshot.contextSummary)
	if err != nil {
		return nil, err
	}
	beforeJSON, err := jsonString(snapshot.before)
	if err != nil {
		return nil, err
	}
	changesJSON, err := jsonString(snapshot.changes)
	if err != nil {
		return nil, err
	}
	afterJSON, err := jsonString(snapshot.after)
	if err != nil {
		return nil, err
	}
	riskJSON, err := jsonString(snapshot.risk)
	if err != nil {
		return nil, err
	}

	return &model.ActiveSchedulePreview{
		ID:                    previewID,
		UserID:                activeContext.User.UserID,
		TriggerID:             triggerID,
		TriggerType:           string(activeContext.Trigger.TriggerType),
		TargetType:            string(activeContext.Trigger.TargetType),
		TargetID:              activeContext.Trigger.TargetID,
		Status:                model.ActiveSchedulePreviewStatusReady,
		SelectedCandidateID:   snapshot.selectedCandidate.CandidateID,
		CandidateCount:        len(snapshot.candidates),
		SelectedCandidateJSON: &selectedJSON,
		CandidatesJSON:        &candidatesJSON,
		DecisionJSON:          &decisionJSON,
		MetricsJSON:           &metricsJSON,
		IssuesJSON:            &issuesJSON,
		ContextSummaryJSON:    &contextJSON,
		BeforeSummaryJSON:     &beforeJSON,
		PreviewChangesJSON:    &changesJSON,
		AfterSummaryJSON:      &afterJSON,
		RiskJSON:              &riskJSON,
		ExplanationText:       explanation,
		NotificationSummary:   notificationSummary,
		BaseVersion:           baseVersion,
		ExpiresAt:             generatedAt.Add(time.Hour),
		GeneratedAt:           generatedAt,
		ApplyStatus:           model.ActiveScheduleApplyStatusNone,
		TraceID:               activeContext.Trace.TraceID,
	}, nil
}

func buildSnapshot(
	activeContext *schedulercontext.ActiveScheduleContext,
	observation observe.Result,
	candidates []candidate.Candidate,
	selected candidate.Candidate,
) rawPreviewSnapshot {
	selectedDTO := candidateDTO(selected)
	candidateDTOs := make([]CandidateDTO, 0, len(candidates))
	for _, item := range candidates {
		candidateDTOs = append(candidateDTOs, candidateDTO(item))
	}
	changes := changeDTOs(selected.CandidateID, selected.Changes)
	before := buildBeforeSummary(activeContext, selected, changes)
	after := buildAfterSummary(before, selected, changes)

	return rawPreviewSnapshot{
		selectedCandidate: selectedDTO,
		candidates:        candidateDTOs,
		decision:          observation.Decision,
		metrics:           observation.Metrics,
		issues:            observation.Issues,
		contextSummary:    contextSummaryDTO(activeContext),
		before:            before,
		changes:           changes,
		after:             after,
		risk:              riskDTO(selected, observation, changes),
	}
}
