package store

import (
	"context"
	"database/sql"
	"fmt"
	"net/url"
	"reflect"
	"strconv"
	"strings"
	"time"

	cfgpkg "github.com/LoveLosita/smartflow/infra/smartflow-mcp-server/internal/config"
	"github.com/go-sql-driver/mysql"
)

type MySQLClient struct {
	db *sql.DB
}

type QueryColumn struct {
	Name         string `json:"name"`
	DatabaseType string `json:"databaseType"`
	Nullable     *bool  `json:"nullable,omitempty"`
	ScanType     string `json:"scanType,omitempty"`
}

type QueryResult struct {
	Columns    []QueryColumn    `json:"columns"`
	Rows       []map[string]any `json:"rows"`
	RowCount   int              `json:"rowCount"`
	Truncated  bool             `json:"truncated"`
	DurationMs int64            `json:"durationMs"`
}

func NewMySQLClient(ctx context.Context, cfg cfgpkg.MySQLConfig) (*MySQLClient, error) {
	dsn := mysql.Config{
		User:                 cfg.User,
		Passwd:               cfg.Password,
		Addr:                 fmt.Sprintf("%s:%d", cfg.Host, cfg.Port),
		Net:                  "tcp",
		DBName:               cfg.Database,
		AllowNativePasswords: true,
		ParseTime:            true,
	}

	applyMySQLParams(&dsn, cfg.Params)

	db, err := sql.Open("mysql", dsn.FormatDSN())
	if err != nil {
		return nil, fmt.Errorf("open mysql: %w", err)
	}
	db.SetConnMaxLifetime(5 * time.Minute)
	db.SetMaxOpenConns(5)
	db.SetMaxIdleConns(5)

	pingCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	if err := db.PingContext(pingCtx); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("ping mysql: %w", err)
	}

	return &MySQLClient{db: db}, nil
}

func applyMySQLParams(cfg *mysql.Config, raw string) {
	if strings.TrimSpace(raw) == "" {
		return
	}
	values, err := url.ParseQuery(raw)
	if err != nil {
		return
	}
	if cfg.Params == nil {
		cfg.Params = make(map[string]string)
	}
	for key, valueList := range values {
		if len(valueList) == 0 {
			continue
		}
		value := valueList[0]
		switch strings.ToLower(key) {
		case "parsetime":
			if parsed, err := strconv.ParseBool(value); err == nil {
				cfg.ParseTime = parsed
			}
		case "loc":
			if loc, err := time.LoadLocation(value); err == nil {
				cfg.Loc = loc
			}
		case "collation":
			cfg.Collation = value
		default:
			cfg.Params[key] = value
		}
	}
}

func (c *MySQLClient) Close() error {
	if c == nil || c.db == nil {
		return nil
	}
	return c.db.Close()
}

func (c *MySQLClient) QueryReadOnly(ctx context.Context, query string, args []any, maxRows int) (QueryResult, error) {
	start := time.Now()
	rows, err := c.db.QueryContext(ctx, query, args...)
	if err != nil {
		return QueryResult{}, err
	}
	defer rows.Close()

	columnTypes, err := rows.ColumnTypes()
	if err != nil {
		return QueryResult{}, err
	}

	columns := make([]QueryColumn, 0, len(columnTypes))
	columnNames := make([]string, 0, len(columnTypes))
	for _, ct := range columnTypes {
		var nullablePtr *bool
		if nullable, ok := ct.Nullable(); ok {
			n := nullable
			nullablePtr = &n
		}
		scanType := ""
		if st := ct.ScanType(); st != nil {
			scanType = st.String()
		}
		columns = append(columns, QueryColumn{
			Name:         ct.Name(),
			DatabaseType: ct.DatabaseTypeName(),
			Nullable:     nullablePtr,
			ScanType:     scanType,
		})
		columnNames = append(columnNames, ct.Name())
	}

	resultRows := make([]map[string]any, 0)
	truncated := false
	for rows.Next() {
		if len(resultRows) >= maxRows {
			truncated = true
			break
		}
		scanned, err := scanRow(rows, len(columnNames))
		if err != nil {
			return QueryResult{}, err
		}
		rowMap := make(map[string]any, len(columnNames))
		for i, name := range columnNames {
			rowMap[name] = normalizeValue(scanned[i])
		}
		resultRows = append(resultRows, rowMap)
	}
	if err := rows.Err(); err != nil {
		return QueryResult{}, err
	}

	return QueryResult{
		Columns:    columns,
		Rows:       resultRows,
		RowCount:   len(resultRows),
		Truncated:  truncated,
		DurationMs: time.Since(start).Milliseconds(),
	}, nil
}

func scanRow(rows *sql.Rows, size int) ([]any, error) {
	dest := make([]any, size)
	holders := make([]any, size)
	for i := range dest {
		holders[i] = &dest[i]
	}
	if err := rows.Scan(holders...); err != nil {
		return nil, err
	}
	return dest, nil
}

func normalizeValue(v any) any {
	switch val := v.(type) {
	case nil:
		return nil
	case []byte:
		return string(val)
	case time.Time:
		return val.Format(time.RFC3339Nano)
	default:
		rv := reflect.ValueOf(v)
		if rv.Kind() == reflect.Ptr && rv.IsNil() {
			return nil
		}
		return v
	}
}
