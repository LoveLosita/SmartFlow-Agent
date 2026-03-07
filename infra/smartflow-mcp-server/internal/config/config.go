package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	ServerName          string
	ServerVersion       string
	ProtocolVersion     string
	DefaultCaller       string
	ToolTimeout         time.Duration
	RateLimitRPS        float64
	RateLimitBurst      float64
	MaxResultRows       int
	AuditLogPath        string
	EnforceWhitelist    bool
	RedisScanMaxKeys    int
	RedisScanMaxCount   int
	RedisValueMaxItems  int
	RedisMaxStringBytes int

	MySQL MySQLConfig
	Redis RedisConfig
}

type MySQLConfig struct {
	Host             string
	Port             int
	User             string
	Password         string
	Database         string
	Params           string
	AllowedDatabases []string
	AllowedTables    []string
}

type RedisConfig struct {
	Addr     string
	Password string
	DB       int
}

func LoadFromEnv() (Config, error) {
	cfg := Config{
		ServerName:          getEnv("MCP_SERVER_NAME", "smartflow-mcp-server"),
		ServerVersion:       getEnv("MCP_SERVER_VERSION", "0.1.0"),
		ProtocolVersion:     getEnv("MCP_PROTOCOL_VERSION", "2024-11-05"),
		DefaultCaller:       getEnv("MCP_DEFAULT_CALLER", "unknown"),
		ToolTimeout:         getEnvDurationMS("MCP_TOOL_TIMEOUT_MS", 5000),
		RateLimitRPS:        getEnvFloat("MCP_RATE_LIMIT_RPS", 5),
		RateLimitBurst:      getEnvFloat("MCP_RATE_LIMIT_BURST", 10),
		MaxResultRows:       getEnvInt("MCP_MAX_RESULT_ROWS", 500),
		AuditLogPath:        getEnv("MCP_AUDIT_LOG_PATH", "logs/audit.log"),
		EnforceWhitelist:    getEnvBool("MCP_ENFORCE_WHITELIST", false),
		RedisScanMaxKeys:    getEnvInt("MCP_REDIS_SCAN_MAX_KEYS", 200),
		RedisScanMaxCount:   getEnvInt("MCP_REDIS_SCAN_MAX_COUNT", 200),
		RedisValueMaxItems:  getEnvInt("MCP_REDIS_VALUE_MAX_ITEMS", 100),
		RedisMaxStringBytes: getEnvInt("MCP_REDIS_MAX_STRING_BYTES", 4096),
		MySQL: MySQLConfig{
			Host:             getEnv("MYSQL_HOST", "127.0.0.1"),
			Port:             getEnvInt("MYSQL_PORT", 3306),
			User:             getEnv("MYSQL_USER", ""),
			Password:         getEnv("MYSQL_PASSWORD", ""),
			Database:         getEnv("MYSQL_DATABASE", ""),
			Params:           getEnv("MYSQL_PARAMS", "charset=utf8mb4&parseTime=true&loc=Local"),
			AllowedDatabases: splitCommaList(getEnv("MYSQL_ALLOWED_DATABASES", "")),
			AllowedTables:    splitCommaList(getEnv("MYSQL_ALLOWED_TABLES", "")),
		},
		Redis: RedisConfig{
			Addr:     getEnv("REDIS_ADDR", "127.0.0.1:6379"),
			Password: getEnv("REDIS_PASSWORD", ""),
			DB:       getEnvInt("REDIS_DB", 0),
		},
	}

	if cfg.MySQL.User == "" || cfg.MySQL.Database == "" {
		return Config{}, fmt.Errorf("MYSQL_USER and MYSQL_DATABASE are required")
	}
	if cfg.Redis.Addr == "" {
		return Config{}, fmt.Errorf("REDIS_ADDR is required")
	}
	if cfg.MaxResultRows <= 0 {
		return Config{}, fmt.Errorf("MCP_MAX_RESULT_ROWS must be > 0")
	}
	if cfg.RedisScanMaxKeys <= 0 {
		return Config{}, fmt.Errorf("MCP_REDIS_SCAN_MAX_KEYS must be > 0")
	}
	if cfg.RedisScanMaxCount <= 0 {
		return Config{}, fmt.Errorf("MCP_REDIS_SCAN_MAX_COUNT must be > 0")
	}
	if cfg.ToolTimeout <= 0 {
		return Config{}, fmt.Errorf("MCP_TOOL_TIMEOUT_MS must be > 0")
	}
	if cfg.RateLimitRPS <= 0 || cfg.RateLimitBurst <= 0 {
		return Config{}, fmt.Errorf("MCP_RATE_LIMIT_RPS and MCP_RATE_LIMIT_BURST must be > 0")
	}

	return cfg, nil
}

func getEnv(key string, defaultValue string) string {
	if v, ok := os.LookupEnv(key); ok {
		return strings.TrimSpace(v)
	}
	return defaultValue
}

func getEnvInt(key string, defaultValue int) int {
	v := getEnv(key, "")
	if v == "" {
		return defaultValue
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return defaultValue
	}
	return n
}

func getEnvFloat(key string, defaultValue float64) float64 {
	v := getEnv(key, "")
	if v == "" {
		return defaultValue
	}
	n, err := strconv.ParseFloat(v, 64)
	if err != nil {
		return defaultValue
	}
	return n
}

func getEnvBool(key string, defaultValue bool) bool {
	v := strings.ToLower(getEnv(key, ""))
	if v == "" {
		return defaultValue
	}
	return v == "1" || v == "true" || v == "yes" || v == "on"
}

func getEnvDurationMS(key string, defaultValueMs int) time.Duration {
	ms := getEnvInt(key, defaultValueMs)
	return time.Duration(ms) * time.Millisecond
}

func splitCommaList(raw string) []string {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		trimmed := strings.TrimSpace(p)
		if trimmed != "" {
			out = append(out, strings.ToLower(trimmed))
		}
	}
	return out
}
