package memory

import (
	"context"
	"errors"
	"log"

	infrallm "github.com/LoveLosita/smartflow/backend/infra/llm"
	infrarag "github.com/LoveLosita/smartflow/backend/infra/rag"
	memorymodel "github.com/LoveLosita/smartflow/backend/memory/model"
	memoryorchestrator "github.com/LoveLosita/smartflow/backend/memory/orchestrator"
	memoryrepo "github.com/LoveLosita/smartflow/backend/memory/repo"
	memoryservice "github.com/LoveLosita/smartflow/backend/memory/service"
	memoryworker "github.com/LoveLosita/smartflow/backend/memory/worker"
	"gorm.io/gorm"
)

// Module 是 memory 模块对外暴露的统一门面。
//
// 职责边界：
// 1. 负责把 repo、service、worker、orchestrator 组装成一个稳定入口；
// 2. 负责对外暴露“写入 / 读取 / 管理 / 启动 worker”这些高层意图；
// 3. 不负责替代应用层 DI，也不负责替代上层事务管理器，事务边界仍由调用方掌控。
type Module struct {
	db         *gorm.DB
	cfg        memorymodel.Config
	llmClient  *infrallm.Client
	ragRuntime infrarag.Runtime

	jobRepo      *memoryrepo.JobRepo
	itemRepo     *memoryrepo.ItemRepo
	auditRepo    *memoryrepo.AuditRepo
	settingsRepo *memoryrepo.SettingsRepo

	enqueueService *memoryservice.EnqueueService
	readService    *memoryservice.ReadService
	manageService  *memoryservice.ManageService
	runner         *memoryworker.Runner
}

// LoadConfigFromViper 复用 memory 子包里的配置加载逻辑，对外收口一个统一入口。
func LoadConfigFromViper() memorymodel.Config {
	return memoryservice.LoadConfigFromViper()
}

// NewModule 创建 memory 模块门面。
//
// 设计说明：
// 1. 这里做的是“轻组装”，不引入额外容器概念，方便先接进现有项目；
// 2. llmClient 允许为 nil，此时写入链路会自动回退到本地 fallback 抽取；
// 3. ragRuntime 允许为 nil，此时读取/向量同步自动回退旧逻辑；
// 4. 若后续接入统一 DI 容器，也应优先注册这个 Module，而不是把内部 repo/service 继续向外泄漏。
func NewModule(db *gorm.DB, llmClient *infrallm.Client, ragRuntime infrarag.Runtime, cfg memorymodel.Config) *Module {
	return wireModule(db, llmClient, ragRuntime, cfg)
}

// WithTx 返回绑定到指定事务连接的同构门面。
//
// 步骤化说明：
// 1. 上层事务管理器先创建 tx；
// 2. 再通过 WithTx(tx) 把 memory 内部所有 repo/service 一次性切到同一个事务连接；
// 3. 这样外部无需重新 new 一堆 repo，也不会破坏既有跨表事务边界。
func (m *Module) WithTx(tx *gorm.DB) *Module {
	if m == nil {
		return nil
	}
	if tx == nil {
		return m
	}
	return wireModule(tx, m.llmClient, m.ragRuntime, m.cfg)
}

// EnqueueExtract 把一次记忆抽取请求入队到 memory_jobs。
func (m *Module) EnqueueExtract(
	ctx context.Context,
	payload memorymodel.ExtractJobPayload,
	sourceEventID string,
) error {
	if m == nil || m.enqueueService == nil {
		return errors.New("memory module enqueue service is nil")
	}
	return m.enqueueService.EnqueueExtractJob(ctx, payload, sourceEventID)
}

// Retrieve 读取后续可供 prompt 注入使用的候选记忆。
func (m *Module) Retrieve(ctx context.Context, req memorymodel.RetrieveRequest) ([]memorymodel.ItemDTO, error) {
	if m == nil || m.readService == nil {
		return nil, errors.New("memory module read service is nil")
	}
	return m.readService.Retrieve(ctx, req)
}

// ListItems 列出用户当前可管理的记忆条目。
func (m *Module) ListItems(ctx context.Context, req memorymodel.ListItemsRequest) ([]memorymodel.ItemDTO, error) {
	if m == nil || m.manageService == nil {
		return nil, errors.New("memory module manage service is nil")
	}
	return m.manageService.ListItems(ctx, req)
}

// DeleteItem 软删除一条记忆，并补写审计日志。
func (m *Module) DeleteItem(ctx context.Context, req memorymodel.DeleteItemRequest) (*memorymodel.ItemDTO, error) {
	if m == nil || m.manageService == nil {
		return nil, errors.New("memory module manage service is nil")
	}
	return m.manageService.DeleteItem(ctx, req)
}

// GetUserSetting 读取用户当前生效的记忆开关。
func (m *Module) GetUserSetting(ctx context.Context, userID int) (memorymodel.UserSettingDTO, error) {
	if m == nil || m.manageService == nil {
		return memorymodel.UserSettingDTO{}, errors.New("memory module manage service is nil")
	}
	return m.manageService.GetUserSetting(ctx, userID)
}

// UpsertUserSetting 写入用户记忆开关。
func (m *Module) UpsertUserSetting(ctx context.Context, req memorymodel.UpdateUserSettingRequest) (memorymodel.UserSettingDTO, error) {
	if m == nil || m.manageService == nil {
		return memorymodel.UserSettingDTO{}, errors.New("memory module manage service is nil")
	}
	return m.manageService.UpsertUserSetting(ctx, req)
}

// StartWorker 启动 memory 后台 worker。
//
// 说明：
// 1. 这里只负责按当前配置拉起轮询循环；
// 2. 若 memory.enabled=false，则直接记录日志并返回；
// 3. 当前不做重复启动保护，生命周期仍假设由应用启动层统一掌控。
func (m *Module) StartWorker(ctx context.Context) {
	if m == nil || m.runner == nil {
		log.Println("Memory worker is not initialized")
		return
	}
	if !m.cfg.Enabled {
		log.Println("Memory worker is disabled")
		return
	}

	go memoryworker.RunPollingLoop(ctx, m.runner, m.cfg.WorkerPollEvery, m.cfg.WorkerClaimBatch)
	log.Println("Memory worker started")
}

func wireModule(db *gorm.DB, llmClient *infrallm.Client, ragRuntime infrarag.Runtime, cfg memorymodel.Config) *Module {
	jobRepo := memoryrepo.NewJobRepo(db)
	itemRepo := memoryrepo.NewItemRepo(db)
	auditRepo := memoryrepo.NewAuditRepo(db)
	settingsRepo := memoryrepo.NewSettingsRepo(db)

	enqueueService := memoryservice.NewEnqueueService(jobRepo)
	readService := memoryservice.NewReadService(itemRepo, settingsRepo, ragRuntime, cfg)
	manageService := memoryservice.NewManageService(db, itemRepo, auditRepo, settingsRepo)
	extractor := memoryorchestrator.NewLLMWriteOrchestrator(llmClient, cfg)

	// 决策编排器：仅在 DecisionEnabled 时才创建有效实例。
	// 原因：cfg.DecisionEnabled=false 时，Runner 不走决策路径，编排器不会使用，
	// 但仍然创建以保持构造签名统一，避免上层调用方感知条件逻辑。
	var decisionOrchestrator *memoryorchestrator.LLMDecisionOrchestrator
	if cfg.DecisionEnabled && llmClient != nil {
		decisionOrchestrator = memoryorchestrator.NewLLMDecisionOrchestrator(llmClient, cfg)
	}

	runner := memoryworker.NewRunner(db, jobRepo, itemRepo, auditRepo, settingsRepo, extractor, ragRuntime, cfg, decisionOrchestrator)

	return &Module{
		db:             db,
		cfg:            cfg,
		llmClient:      llmClient,
		ragRuntime:     ragRuntime,
		jobRepo:        jobRepo,
		itemRepo:       itemRepo,
		auditRepo:      auditRepo,
		settingsRepo:   settingsRepo,
		enqueueService: enqueueService,
		readService:    readService,
		manageService:  manageService,
		runner:         runner,
	}
}
