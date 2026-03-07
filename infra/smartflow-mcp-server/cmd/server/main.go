package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/LoveLosita/smartflow/infra/smartflow-mcp-server/internal/audit"
	"github.com/LoveLosita/smartflow/infra/smartflow-mcp-server/internal/config"
	"github.com/LoveLosita/smartflow/infra/smartflow-mcp-server/internal/envutil"
	"github.com/LoveLosita/smartflow/infra/smartflow-mcp-server/internal/mcp"
	"github.com/LoveLosita/smartflow/infra/smartflow-mcp-server/internal/ratelimit"
	"github.com/LoveLosita/smartflow/infra/smartflow-mcp-server/internal/security"
	"github.com/LoveLosita/smartflow/infra/smartflow-mcp-server/internal/store"
	"github.com/LoveLosita/smartflow/infra/smartflow-mcp-server/internal/tools"
)

func main() {
	if err := envutil.LoadDotEnv(".env"); err != nil {
		log.Fatalf("load .env failed: %v", err)
	}

	cfg, err := config.LoadFromEnv()
	if err != nil {
		log.Fatalf("load config failed: %v", err)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	auditLogger, err := audit.New(cfg.AuditLogPath)
	if err != nil {
		log.Fatalf("init audit logger failed: %v", err)
	}
	defer func() {
		_ = auditLogger.Close()
	}()

	mysqlClient, err := store.NewMySQLClient(ctx, cfg.MySQL)
	if err != nil {
		log.Fatalf("init mysql failed: %v", err)
	}
	defer func() {
		_ = mysqlClient.Close()
	}()

	redisClient, err := store.NewRedisClient(ctx, cfg.Redis)
	if err != nil {
		log.Fatalf("init redis failed: %v", err)
	}
	defer func() {
		_ = redisClient.Close()
	}()

	sqlValidator := security.NewSQLValidator(
		cfg.MySQL.Database,
		cfg.EnforceWhitelist,
		cfg.MySQL.AllowedDatabases,
		cfg.MySQL.AllowedTables,
	)

	registry, err := tools.NewRegistry(
		tools.NewMySQLReadOnlyTool(mysqlClient, sqlValidator, cfg.MaxResultRows),
		tools.NewRedisGetTool(redisClient, cfg.RedisValueMaxItems, cfg.RedisMaxStringBytes),
		tools.NewRedisScanTool(redisClient, cfg.RedisScanMaxKeys, cfg.RedisScanMaxCount),
	)
	if err != nil {
		log.Fatalf("init tool registry failed: %v", err)
	}

	limiter := ratelimit.New(cfg.RateLimitRPS, cfg.RateLimitBurst)
	server := mcp.NewServer(
		os.Stdin,
		os.Stdout,
		registry,
		auditLogger,
		limiter,
		cfg.ServerName,
		cfg.ServerVersion,
		cfg.ProtocolVersion,
		cfg.DefaultCaller,
		cfg.ToolTimeout,
	)

	if err := server.Serve(ctx); err != nil {
		log.Fatalf("mcp server exited with error: %v", err)
	}
}
