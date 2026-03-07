package tools

import (
	"context"
	"os"
	"strconv"
	"testing"
	"time"

	"github.com/LoveLosita/smartflow/infra/smartflow-mcp-server/internal/config"
	"github.com/LoveLosita/smartflow/infra/smartflow-mcp-server/internal/security"
	"github.com/LoveLosita/smartflow/infra/smartflow-mcp-server/internal/store"
)

func TestIntegrationMySQLReadOnlyTool(t *testing.T) {
	if os.Getenv("MCP_IT_RUN") != "1" {
		t.Skip("set MCP_IT_RUN=1 to run integration tests")
	}

	port := 3306
	if p := os.Getenv("MYSQL_PORT"); p != "" {
		if n, err := strconv.Atoi(p); err == nil {
			port = n
		}
	}

	mysqlCfg := config.MySQLConfig{
		Host:     os.Getenv("MYSQL_HOST"),
		Port:     port,
		User:     os.Getenv("MYSQL_USER"),
		Password: os.Getenv("MYSQL_PASSWORD"),
		Database: os.Getenv("MYSQL_DATABASE"),
		Params:   "charset=utf8mb4&parseTime=true&loc=Local",
	}
	if mysqlCfg.Host == "" || mysqlCfg.User == "" || mysqlCfg.Database == "" {
		t.Skip("missing MYSQL_* env for integration test")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	client, err := store.NewMySQLClient(ctx, mysqlCfg)
	if err != nil {
		t.Fatalf("mysql not available: %v", err)
	}
	defer func() { _ = client.Close() }()

	validator := security.NewSQLValidator(mysqlCfg.Database, false, nil, nil)
	tool := NewMySQLReadOnlyTool(client, validator, 10)
	res, err := tool.Execute(ctx, map[string]any{"sql": "SELECT 1 AS ok"})
	if err != nil {
		t.Fatalf("tool execute failed: %v", err)
	}
	if res["rowCount"].(int) < 1 {
		t.Fatalf("expected rowCount >= 1")
	}
}

func TestIntegrationRedisTools(t *testing.T) {
	if os.Getenv("MCP_IT_RUN") != "1" {
		t.Skip("set MCP_IT_RUN=1 to run integration tests")
	}

	db := 0
	if p := os.Getenv("REDIS_DB"); p != "" {
		if n, err := strconv.Atoi(p); err == nil {
			db = n
		}
	}
	redisCfg := config.RedisConfig{
		Addr:     os.Getenv("REDIS_ADDR"),
		Password: os.Getenv("REDIS_PASSWORD"),
		DB:       db,
	}
	if redisCfg.Addr == "" {
		t.Skip("REDIS_ADDR is empty")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	client, err := store.NewRedisClient(ctx, redisCfg)
	if err != nil {
		t.Fatalf("redis not available: %v", err)
	}
	defer func() { _ = client.Close() }()

	getTool := NewRedisGetTool(client, 10, 128)
	if _, err := getTool.Execute(ctx, map[string]any{"key": "__integration_missing_key__"}); err != nil {
		t.Fatalf("redis_get failed: %v", err)
	}

	scanTool := NewRedisScanTool(client, 10, 10)
	if _, err := scanTool.Execute(ctx, map[string]any{"pattern": "*", "count": float64(5)}); err != nil {
		t.Fatalf("redis_scan failed: %v", err)
	}
}
