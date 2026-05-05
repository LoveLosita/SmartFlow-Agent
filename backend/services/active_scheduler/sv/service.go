package sv

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	activeadapters "github.com/LoveLosita/smartflow/backend/services/active_scheduler/core/adapters"
	activeapply "github.com/LoveLosita/smartflow/backend/services/active_scheduler/core/apply"
	activeapplyadapter "github.com/LoveLosita/smartflow/backend/services/active_scheduler/core/applyadapter"
	activegraph "github.com/LoveLosita/smartflow/backend/services/active_scheduler/core/graph"
	activejob "github.com/LoveLosita/smartflow/backend/services/active_scheduler/core/job"
	activepreview "github.com/LoveLosita/smartflow/backend/services/active_scheduler/core/preview"
	activesel "github.com/LoveLosita/smartflow/backend/services/active_scheduler/core/selection"
	activesvc "github.com/LoveLosita/smartflow/backend/services/active_scheduler/core/service"
	"github.com/LoveLosita/smartflow/backend/services/active_scheduler/core/trigger"
	llmservice "github.com/LoveLosita/smartflow/backend/services/llm"
	rootdao "github.com/LoveLosita/smartflow/backend/services/runtime/dao"
	eventsvc "github.com/LoveLosita/smartflow/backend/services/runtime/eventsvc"
	contracts "github.com/LoveLosita/smartflow/backend/shared/contracts/activescheduler"
	sharedevents "github.com/LoveLosita/smartflow/backend/shared/events"
	kafkabus "github.com/LoveLosita/smartflow/backend/shared/infra/kafka"
	outboxinfra "github.com/LoveLosita/smartflow/backend/shared/infra/outbox"
	"gorm.io/gorm"
)

const defaultJobScanLimit = 50

// Options 描述 active-scheduler 独立服务的启动参数。
//
// 职责边界：
// 1. 只承载服务内部 worker 节奏和 outbox 配置；
// 2. 不承载数据库连接、模型配置或 HTTP/gateway 配置；
// 3. 零值使用安全默认值，便于本地 smoke 先跑通。
type Options struct {
	JobScanEvery time.Duration
	JobScanLimit int
	KafkaConfig  kafkabus.Config
	TaskRPC      activeadapters.TaskRPCConfig
	ScheduleRPC  activeadapters.ScheduleRPCConfig
}

// Service 是 active-scheduler 独立进程内的服务门面。
//
// 职责边界：
// 1. 对 RPC 层暴露 dry-run / trigger / preview / confirm；
// 2. 对 cmd 层暴露 outbox consumer 和 due job scanner 生命周期；
// 3. 内部复用 services/active_scheduler/core 下的领域核心，避免服务入口和算法实现散落在旧根目录。
type Service struct {
	dryRun         *activesvc.DryRunService
	trigger        *activesvc.TriggerService
	previewConfirm *activesvc.PreviewConfirmService
	eventBus       *outboxinfra.EventBus
	jobScanner     *activejob.Scanner
}

// New 构造 active-scheduler 服务运行态。
//
// 步骤化说明：
// 1. 先组装 active-scheduler 自有 DAO、只读 readers、dry-run 和 preview/confirm；
// 2. 再按 active-scheduler 服务归属注册 outbox 路由与 active_schedule.triggered handler；
// 3. 最后创建 due job scanner，让 worker 能从 active_schedule_jobs 产生正式 trigger；
// 4. Kafka 关闭时保留 dry-run / preview / confirm，同步 trigger 会返回明确错误。
func New(db *gorm.DB, llmService *llmservice.Service, opts Options) (*Service, error) {
	if db == nil {
		return nil, errors.New("active-scheduler database 未初始化")
	}

	activeDAO := rootdao.NewActiveScheduleDAO(db)
	taskRPCAdapter, err := activeadapters.NewTaskRPCAdapter(opts.TaskRPC)
	if err != nil {
		return nil, fmt.Errorf("initialize task rpc adapter failed: %w", err)
	}
	scheduleRPCAdapter, err := activeadapters.NewScheduleRPCAdapter(opts.ScheduleRPC)
	if err != nil {
		return nil, fmt.Errorf("initialize schedule rpc adapter failed: %w", err)
	}
	readers := activeadapters.ReadersWithScheduleRPC(taskRPCAdapter, scheduleRPCAdapter)
	dryRun, err := activesvc.NewDryRunService(readers)
	if err != nil {
		return nil, err
	}
	previewConfirm, err := buildPreviewConfirmService(activeDAO, dryRun, scheduleRPCAdapter)
	if err != nil {
		return nil, err
	}

	outboxRepo := outboxinfra.NewRepository(db)
	eventBus, err := buildActiveSchedulerEventBus(outboxRepo, opts.KafkaConfig)
	if err != nil {
		return nil, err
	}
	triggerService, err := activesvc.NewTriggerService(activeDAO, eventBus)
	if err != nil {
		return nil, err
	}

	var jobScanner *activejob.Scanner
	if eventBus != nil {
		graphRunner, err := buildGraphRunner(dryRun, llmService)
		if err != nil {
			return nil, err
		}
		workflow, err := activesvc.NewTriggerWorkflowServiceWithOptions(
			activeDAO,
			graphRunner,
			outboxRepo,
			opts.KafkaConfig,
			activesvc.WithActiveScheduleSessionBridge(rootdao.NewAgentDAO(db), rootdao.NewActiveScheduleSessionDAO(db)),
		)
		if err != nil {
			return nil, err
		}
		if err := registerActiveSchedulerOutboxHandler(eventBus, outboxRepo, workflow); err != nil {
			return nil, err
		}
		jobScanner, err = activejob.NewScanner(activeDAO, readers, triggerService, activejob.ScannerOptions{
			ScanEvery: opts.JobScanEvery,
			Limit:     normalizeJobScanLimit(opts.JobScanLimit),
		})
		if err != nil {
			return nil, err
		}
	}

	return &Service{
		dryRun:         dryRun,
		trigger:        triggerService,
		previewConfirm: previewConfirm,
		eventBus:       eventBus,
		jobScanner:     jobScanner,
	}, nil
}

// StartWorkers 启动 active-scheduler 自己的 outbox relay/consumer 和 due job scanner。
func (s *Service) StartWorkers(ctx context.Context) {
	if s == nil {
		return
	}
	if s.eventBus != nil {
		s.eventBus.Start(ctx)
	}
	if s.jobScanner != nil {
		s.jobScanner.Start(ctx)
	}
}

// Close 关闭 active-scheduler 持有的 Kafka 资源。
func (s *Service) Close() {
	if s != nil && s.eventBus != nil {
		s.eventBus.Close()
	}
}

// DryRun 同步执行主动调度诊断，并以 JSON 形式返回现有响应结构。
func (s *Service) DryRun(ctx context.Context, req contracts.ActiveScheduleRequest) (json.RawMessage, error) {
	if s == nil || s.dryRun == nil {
		return nil, errors.New("active-scheduler dry-run service 未初始化")
	}
	trig := buildDryRunTrigger(req, time.Now())
	result, err := s.dryRun.DryRun(ctx, trig)
	if err != nil {
		return nil, err
	}
	return marshalResponseJSON(result)
}

// Trigger 创建正式 trigger 并发布 active_schedule.triggered。
func (s *Service) Trigger(ctx context.Context, req contracts.ActiveScheduleRequest) (*contracts.TriggerResponse, error) {
	if s == nil || s.trigger == nil {
		return nil, errors.New("active-scheduler trigger service 未初始化")
	}
	now := time.Now()
	resp, err := s.trigger.CreateAndPublish(ctx, activesvc.TriggerRequest{
		UserID:         req.UserID,
		TriggerType:    trigger.TriggerType(req.TriggerType),
		Source:         trigger.SourceAPITrigger,
		TargetType:     trigger.TargetType(req.TargetType),
		TargetID:       req.TargetID,
		FeedbackID:     req.FeedbackID,
		IdempotencyKey: req.IdempotencyKey,
		MockNow:        req.MockNow,
		IsMockTime:     req.MockNow != nil,
		RequestedAt:    now,
		Payload:        normalizePayload(req.Payload),
		TraceID:        fmt.Sprintf("trace_api_trigger_%d_%d", req.UserID, now.UnixNano()),
	})
	if err != nil {
		return nil, err
	}
	return &contracts.TriggerResponse{
		TriggerID: resp.TriggerID,
		Status:    resp.Status,
		PreviewID: resp.PreviewID,
		DedupeHit: resp.DedupeHit,
		TraceID:   resp.TraceID,
	}, nil
}

// CreatePreview 同步 dry-run 后把 top1 候选固化为待确认预览。
func (s *Service) CreatePreview(ctx context.Context, req contracts.ActiveScheduleRequest) (json.RawMessage, error) {
	if s == nil || s.dryRun == nil || s.previewConfirm == nil {
		return nil, errors.New("active-scheduler preview service 未初始化")
	}
	now := time.Now()
	trig := buildDryRunTrigger(req, now)
	trig.TriggerID = fmt.Sprintf("ast_api_%d_%d", req.UserID, now.UnixNano())
	trig.TraceID = fmt.Sprintf("trace_api_preview_%d_%d", req.UserID, now.UnixNano())

	dryRunResult, err := s.dryRun.DryRun(ctx, trig)
	if err != nil {
		return nil, err
	}
	previewResp, err := s.previewConfirm.CreatePreviewFromDryRun(ctx, activepreview.CreatePreviewRequest{
		ActiveContext: dryRunResult.Context,
		Observation:   dryRunResult.Observation,
		Candidates:    dryRunResult.Candidates,
		TriggerID:     trig.TriggerID,
		GeneratedAt:   now,
	})
	if err != nil {
		return nil, err
	}
	return marshalResponseJSON(previewResp.Detail)
}

// GetPreview 查询主动调度预览详情。
func (s *Service) GetPreview(ctx context.Context, req contracts.GetPreviewRequest) (json.RawMessage, error) {
	if s == nil || s.previewConfirm == nil {
		return nil, errors.New("active-scheduler preview service 未初始化")
	}
	detail, err := s.previewConfirm.GetPreview(ctx, req.UserID, req.PreviewID)
	if err != nil {
		return nil, err
	}
	return marshalResponseJSON(detail)
}

// ConfirmPreview 同步确认并正式应用主动调度预览。
func (s *Service) ConfirmPreview(ctx context.Context, req contracts.ConfirmPreviewRequest) (json.RawMessage, error) {
	if s == nil || s.previewConfirm == nil {
		return nil, errors.New("active-scheduler confirm service 未初始化")
	}
	editedChanges, err := decodeEditedChanges(req.EditedChanges)
	if err != nil {
		return nil, activeapply.NewApplyError(activeapply.ErrorCodeInvalidEditedChanges, "edited_changes 不是合法的变更数组", err)
	}
	requestedAt := req.RequestedAt
	if requestedAt.IsZero() {
		requestedAt = time.Now()
	}
	result, err := s.previewConfirm.ConfirmPreview(ctx, activeapply.ConfirmRequest{
		PreviewID:      req.PreviewID,
		UserID:         req.UserID,
		CandidateID:    req.CandidateID,
		Action:         activeapply.ConfirmAction(req.Action),
		EditedChanges:  editedChanges,
		IdempotencyKey: req.IdempotencyKey,
		RequestedAt:    requestedAt,
		TraceID:        req.TraceID,
	})
	if err != nil {
		return nil, err
	}
	return marshalResponseJSON(result)
}

func buildPreviewConfirmService(activeDAO *rootdao.ActiveScheduleDAO, dryRun *activesvc.DryRunService, scheduleApplyAdapter interface {
	ApplyActiveScheduleChanges(context.Context, activeapplyadapter.ApplyActiveScheduleRequest) (activeapplyadapter.ApplyActiveScheduleResult, error)
}) (*activesvc.PreviewConfirmService, error) {
	previewService, err := activepreview.NewService(activeDAO)
	if err != nil {
		return nil, err
	}
	return activesvc.NewPreviewConfirmService(dryRun, previewService, activeDAO, scheduleApplyAdapter)
}

func buildGraphRunner(dryRun *activesvc.DryRunService, llmService *llmservice.Service) (*activegraph.Runner, error) {
	var llmClient *llmservice.Client
	if llmService != nil {
		llmClient = llmService.ProClient()
	}
	return activegraph.NewRunner(dryRun.AsGraphDryRunFunc(), activesel.NewService(llmClient))
}

func buildActiveSchedulerEventBus(outboxRepo *outboxinfra.Repository, kafkaCfg kafkabus.Config) (*outboxinfra.EventBus, error) {
	if outboxRepo == nil {
		return nil, errors.New("active-scheduler outbox repository 未初始化")
	}
	if err := outboxinfra.RegisterEventService(sharedevents.ActiveScheduleTriggeredEventType, outboxinfra.ServiceActiveScheduler); err != nil {
		return nil, err
	}
	eventBus, err := outboxinfra.NewEventBus(outboxRepo, kafkaCfg)
	if err != nil {
		return nil, err
	}
	return eventBus, nil
}

func registerActiveSchedulerOutboxHandler(eventBus *outboxinfra.EventBus, outboxRepo *outboxinfra.Repository, workflow eventsvc.ActiveScheduleTriggeredProcessor) error {
	if eventBus == nil {
		return nil
	}
	return eventsvc.RegisterActiveScheduleTriggeredHandler(eventBus, outboxRepo, workflow)
}

func buildDryRunTrigger(req contracts.ActiveScheduleRequest, now time.Time) trigger.ActiveScheduleTrigger {
	return trigger.ActiveScheduleTrigger{
		UserID:         req.UserID,
		TriggerType:    trigger.TriggerType(req.TriggerType),
		Source:         trigger.SourceAPIDryRun,
		TargetType:     trigger.TargetType(req.TargetType),
		TargetID:       req.TargetID,
		FeedbackID:     req.FeedbackID,
		IdempotencyKey: req.IdempotencyKey,
		MockNow:        req.MockNow,
		IsMockTime:     req.MockNow != nil,
		RequestedAt:    now,
	}
}

func normalizePayload(raw json.RawMessage) json.RawMessage {
	if len(raw) == 0 || strings.TrimSpace(string(raw)) == "" || strings.TrimSpace(string(raw)) == "null" {
		return json.RawMessage("{}")
	}
	return raw
}

func decodeEditedChanges(raw json.RawMessage) ([]activeapply.ApplyChange, error) {
	if len(raw) == 0 || strings.TrimSpace(string(raw)) == "" || strings.TrimSpace(string(raw)) == "null" {
		return nil, nil
	}
	var changes []activeapply.ApplyChange
	if err := json.Unmarshal(raw, &changes); err != nil {
		return nil, err
	}
	return changes, nil
}

func marshalResponseJSON(value any) (json.RawMessage, error) {
	raw, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	return json.RawMessage(raw), nil
}

func normalizeJobScanLimit(limit int) int {
	if limit <= 0 {
		return defaultJobScanLimit
	}
	return limit
}
