package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"syscall"

	taskclassclient "github.com/LoveLosita/smartflow/backend/client/taskclass"
	"github.com/LoveLosita/smartflow/backend/services/taskclassforum/adapter"
	forumdao "github.com/LoveLosita/smartflow/backend/services/taskclassforum/dao"
	forumrpc "github.com/LoveLosita/smartflow/backend/services/taskclassforum/rpc"
	forumsv "github.com/LoveLosita/smartflow/backend/services/taskclassforum/sv"
	sharedevents "github.com/LoveLosita/smartflow/backend/shared/events"
	"github.com/LoveLosita/smartflow/backend/shared/infra/bootstrap"
	redisinfra "github.com/LoveLosita/smartflow/backend/shared/infra/redis"
	outboxinfra "github.com/LoveLosita/smartflow/backend/shared/infra/outbox"
	"github.com/spf13/viper"
)

func main() {
	if err := bootstrap.LoadConfig(); err != nil {
		log.Fatalf("failed to load config: %v", err)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	db, err := forumdao.OpenDBFromConfig()
	if err != nil {
		log.Fatalf("failed to connect taskclassforum database: %v", err)
	}
	if err := registerForumRewardOutboxRoutes(); err != nil {
		log.Fatalf("failed to register taskclassforum outbox routes: %v", err)
	}

	taskClassClient, err := taskclassclient.NewClient(taskclassclient.ClientConfig{
		Endpoints: viper.GetStringSlice("taskClass.rpc.endpoints"),
		Target:    viper.GetString("taskClass.rpc.target"),
		Timeout:   viper.GetDuration("taskClass.rpc.timeout"),
	})
	if err != nil {
		log.Fatalf("failed to initialize task-class zrpc client: %v", err)
	}

	// 1. 论坛服务只依赖 TaskClass 快照端口，不直接操作 task_classes / task_items 物理表。
	// 2. 当前实现通过 task-class zrpc 读取/创建副本，保持 dev 主干的微服务边界不回退。
	// 3. 后续若 task-class 契约扩展，只需替换 adapter 内部映射，不需要改论坛业务层。
	taskClassPort := adapter.NewTaskClassRPCAdapter(taskClassClient)
	eventPublisher := outboxinfra.NewRepositoryPublisher(outboxinfra.NewRepository(db), viper.GetInt("kafka.maxRetry"))

	commentTreeCache := forumsv.CommentTreeCachePort(nil)
	if rdb, redisErr := redisinfra.OpenRedisFromConfig(); redisErr != nil {
		log.Printf("taskclassforum 评论树缓存已降级关闭，Redis 连接失败: %v", redisErr)
	} else {
		defer rdb.Close()
		commentTreeCache = forumdao.NewCommentTreeCache(rdb)
	}

	svc := forumsv.New(forumsv.Options{
		DB:               db,
		TaskClassPort:    taskClassPort,
		EventPublisher:   eventPublisher,
		CommentTreeCache: commentTreeCache,
	})

	server, listenOn, err := forumrpc.NewServer(forumrpc.ServerOptions{
		ListenOn: viper.GetString("taskclassforum.rpc.listenOn"),
		Timeout:  viper.GetDuration("taskclassforum.rpc.timeout"),
		Service:  svc,
	})
	if err != nil {
		log.Fatalf("failed to build taskclassforum zrpc server: %v", err)
	}
	defer server.Stop()

	go func() {
		log.Printf("taskclassforum zrpc service starting on %s", listenOn)
		server.Start()
	}()

	<-ctx.Done()
	log.Println("taskclassforum service stopping")
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
