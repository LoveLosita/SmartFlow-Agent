# smartflow-mcp-server (MVP)

用于让 Codex 通过 MCP（stdio）只读访问 MySQL 与 Redis，面向接口联调与测试。

## 1. 功能范围（第一阶段）

只实现 3 个只读工具：

1. `mysql_query_readonly`
2. `redis_get`
3. `redis_scan`

未实现任何写操作工具。

## 2. 目录结构

```text
infra/smartflow-mcp-server
├─ cmd/server/main.go
├─ internal
│  ├─ audit
│  ├─ config
│  ├─ envutil
│  ├─ mcp
│  ├─ ratelimit
│  ├─ security
│  ├─ store
│  └─ tools
├─ .env.example
├─ go.mod
└─ README.md
```

## 3. 快速启动

```bash
go mod tidy
go test ./...
go run ./cmd/server
```

服务采用 stdio MCP 协议，不会启动 HTTP 端口。

## 4. 配置说明（全部来自环境变量）

复制并编辑：

```bash
cp .env.example .env
```

关键变量：

- `MYSQL_HOST` / `MYSQL_PORT` / `MYSQL_USER` / `MYSQL_PASSWORD` / `MYSQL_DATABASE`
- `REDIS_ADDR` / `REDIS_PASSWORD` / `REDIS_DB`
- `MYSQL_ALLOWED_DATABASES`：逗号分隔
- `MYSQL_ALLOWED_TABLES`：逗号分隔，支持 `db.table` 或 `table`；留空表示允许所有表
- `MCP_ENFORCE_WHITELIST`：`true` 时无明确表引用会拒绝执行
- `MCP_TOOL_TIMEOUT_MS`：单次工具调用超时
- `MCP_RATE_LIMIT_RPS` + `MCP_RATE_LIMIT_BURST`：基础令牌桶限流
- `MCP_MAX_RESULT_ROWS`：MySQL 最大返回行数
- `MCP_REDIS_SCAN_MAX_KEYS`：`redis_scan` 最大返回 key 数
- `MCP_AUDIT_LOG_PATH`：审计日志路径

## 5. 工具说明

### 5.1 `mysql_query_readonly`

输入：

```json
{
  "sql": "SELECT id, name FROM users WHERE id = ?",
  "params": [1]
}
```

安全限制：

- 仅允许 `SELECT` / `SHOW` / `DESCRIBE` / `EXPLAIN`
- 禁止分号 `;`（多语句）
- 禁止注释 `--` / `#` / `/* */`
- 禁止 DDL/DML 关键字（`INSERT`/`UPDATE`/`DELETE`/`ALTER`/`DROP`/`TRUNCATE` 等）
- 支持库/表白名单校验

输出（结构化）：

- `columns`
- `rows`
- `rowCount`
- `truncated`
- `durationMs`

### 5.2 `redis_get`

输入：

```json
{
  "key": "user:1001"
}
```

输出：

- `exists`
- `key`
- `type`
- `value`
- `truncated`
- `durationMs`

### 5.3 `redis_scan`

输入：

```json
{
  "pattern": "user:*",
  "count": 50
}
```

输出：

- `pattern`
- `keys`
- `returned`
- `nextCursor`
- `truncated`
- `durationMs`

## 6. 审计日志

每次工具调用会记录（JSON 行格式）：

- 时间
- 工具名
- 调用方（caller）
- 是否成功
- 耗时
- 脱敏后的输入摘要
- 错误信息（截断）

敏感字段处理：

- SQL 字符串字面量与数字会脱敏
- Redis key 仅保留前后少量字符

## 7. Codex MCP 配置示例（stdio）

可按客户端配置格式接入，示例：

```json
{
  "mcpServers": {
    "smartflow-db-readonly": {
      "command": "go",
      "args": ["run", "./cmd/server"],
      "cwd": "E:/SmartFlow-Agent/infra/smartflow-mcp-server",
      "env": {
        "MYSQL_HOST": "127.0.0.1",
        "MYSQL_PORT": "3306",
        "MYSQL_USER": "readonly_user",
        "MYSQL_PASSWORD": "replace_me",
        "MYSQL_DATABASE": "smartflow",
        "MYSQL_ALLOWED_DATABASES": "smartflow",
        "MYSQL_ALLOWED_TABLES": "",
        "REDIS_ADDR": "127.0.0.1:6379",
        "REDIS_DB": "0",
        "MCP_TOOL_TIMEOUT_MS": "5000",
        "MCP_RATE_LIMIT_RPS": "5",
        "MCP_RATE_LIMIT_BURST": "10",
        "MCP_MAX_RESULT_ROWS": "500",
        "MCP_REDIS_SCAN_MAX_KEYS": "200",
        "MCP_AUDIT_LOG_PATH": "logs/audit.log"
      }
    }
  }
}
```

## 8. 安全限制生效示例

- SQL 多语句：`SELECT 1; SELECT 2` -> 被拒绝（semicolon is not allowed）
- SQL 注释绕过：`SELECT * FROM users --x` -> 被拒绝（sql comments are not allowed）
- 写操作：`DELETE FROM users` -> 被拒绝（dangerous sql keyword detected）
- 白名单外表：`SELECT * FROM admin.secret` -> 被拒绝（table not in whitelist）
- Redis 大范围扫描：`redis_scan` 返回数量受 `MCP_REDIS_SCAN_MAX_KEYS` 限制

## 9. 风险说明（MVP 已知边界）

1. SQL 校验采用关键字与模式匹配，不是完整 SQL AST 解析，建议二阶段引入 AST 级校验。
2. `SHOW DATABASES` 等无显式表引用语句在非严格模式下可执行；生产建议开启 `MCP_ENFORCE_WHITELIST=true`。
3. Redis 复杂类型返回做了截断保护，但仍建议在生产环境设置更小上限。

## 10. 常见问题（FAQ）

### Q1: 启动时报 `MYSQL_USER and MYSQL_DATABASE are required`

检查环境变量是否正确加载，建议先确认 `.env` 存在于 `infra/smartflow-mcp-server`。

### Q2: 为什么调用工具报限流

默认启用了令牌桶限流，调大 `MCP_RATE_LIMIT_RPS` 与 `MCP_RATE_LIMIT_BURST` 即可。

### Q3: 为什么 `redis_scan` 返回不全

是预期行为，结果数被 `MCP_REDIS_SCAN_MAX_KEYS` 限制，避免全量扫描拖垮 Redis。

### Q4: 审计日志在哪里

默认在 `logs/audit.log`，可用 `MCP_AUDIT_LOG_PATH` 自定义。
