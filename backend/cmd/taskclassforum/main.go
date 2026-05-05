package main

import (
	"log"

	"github.com/LoveLosita/smartflow/backend/bootstrap"
	legacydao "github.com/LoveLosita/smartflow/backend/dao"
	outboxinfra "github.com/LoveLosita/smartflow/backend/infra/outbox"
	"github.com/LoveLosita/smartflow/backend/services/taskclassforum/adapter"
	forumdao "github.com/LoveLosita/smartflow/backend/services/taskclassforum/dao"
	forumrpc "github.com/LoveLosita/smartflow/backend/services/taskclassforum/rpc"
	forumsv "github.com/LoveLosita/smartflow/backend/services/taskclassforum/sv"
	sharedevents "github.com/LoveLosita/smartflow/backend/shared/events"
	"github.com/spf13/viper"
)

func main() {
	if err := bootstrap.LoadConfig(); err != nil {
		log.Fatalf("failed to load config: %v", err)
	}

	db, err := forumdao.OpenDBFromConfig()
	if err != nil {
		log.Fatalf("failed to connect taskclassforum database: %v", err)
	}
	if err := registerForumRewardOutboxRoutes(); err != nil {
		log.Fatalf("failed to register taskclassforum outbox routes: %v", err)
	}

	// 1. 复用同一个 DB 句柄装配 legacy TaskClass DAO，避免本轮抢改 task-class 模块。
	// 2. 计划广场只通过快照端口读取和创建 TaskClass，不直接写 schedule。
	// 3. 后续 task-class 独立成服务后，只替换这里的 adapter 注入点。
	taskClassPort := adapter.NewLegacyTaskClassAdapter(legacydao.NewTaskClassDAO(db))
	eventPublisher := outboxinfra.NewRepositoryPublisher(outboxinfra.NewRepository(db), viper.GetInt("kafka.maxRetry"))
	svc := forumsv.New(forumsv.Options{
		DB:             db,
		TaskClassPort:  taskClassPort,
		EventPublisher: eventPublisher,
	})
	forumrpc.Start(forumrpc.ServerOptions{
		ListenOn: viper.GetString("taskclassforum.rpc.listenOn"),
		Timeout:  viper.GetDuration("taskclassforum.rpc.timeout"),
		Service:  svc,
	})
}

// registerForumRewardOutboxRoutes 负责让独立 taskclassforum RPC 进程认识奖励事件的落表归属。
//
// 步骤说明：
// 1. 点赞、导入事件都由 token-store 消费并写 token_grants，所以事件路由归属 token-store；
// 2. taskclassforum 进程只负责发布事件，不启动 consumer，也不直接写奖励账本；
// 3. 若注册失败直接阻止启动，避免后续点赞/导入看似成功但 outbox 永远无法入队。
func registerForumRewardOutboxRoutes() error {
	if err := outboxinfra.RegisterEventService(sharedevents.ForumPostLikedEventType, outboxinfra.ServiceTokenStore); err != nil {
		return err
	}
	return outboxinfra.RegisterEventService(sharedevents.ForumPostImportedEventType, outboxinfra.ServiceTokenStore)
}
