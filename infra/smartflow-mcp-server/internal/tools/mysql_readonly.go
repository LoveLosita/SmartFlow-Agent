package tools

import (
	"context"
	"fmt"

	"github.com/LoveLosita/smartflow/infra/smartflow-mcp-server/internal/security"
	"github.com/LoveLosita/smartflow/infra/smartflow-mcp-server/internal/store"
)

type MySQLReadOnlyTool struct {
	client    *store.MySQLClient
	validator *security.SQLValidator
	maxRows   int
}

func NewMySQLReadOnlyTool(client *store.MySQLClient, validator *security.SQLValidator, maxRows int) *MySQLReadOnlyTool {
	return &MySQLReadOnlyTool{client: client, validator: validator, maxRows: maxRows}
}

func (t *MySQLReadOnlyTool) Name() string {
	return "mysql_query_readonly"
}

func (t *MySQLReadOnlyTool) Description() string {
	return "Execute read-only SQL on MySQL. Only SELECT/SHOW/DESCRIBE/EXPLAIN are allowed."
}

func (t *MySQLReadOnlyTool) InputSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"sql": map[string]any{
				"type":        "string",
				"description": "Read-only SQL statement",
			},
			"params": map[string]any{
				"type":        "array",
				"description": "Optional bind parameters",
				"items": map[string]any{
					"type": []string{"string", "number", "boolean", "null"},
				},
			},
		},
		"required":             []string{"sql"},
		"additionalProperties": false,
	}
}

func (t *MySQLReadOnlyTool) Execute(ctx context.Context, args map[string]any) (map[string]any, error) {
	rawSQL, ok := args["sql"].(string)
	if !ok || rawSQL == "" {
		return nil, fmt.Errorf("sql must be a non-empty string")
	}
	if err := t.validator.ValidateReadOnlySQL(rawSQL); err != nil {
		return nil, err
	}

	params, err := normalizeParams(args["params"])
	if err != nil {
		return nil, err
	}

	res, err := t.client.QueryReadOnly(ctx, rawSQL, params, t.maxRows)
	if err != nil {
		return nil, err
	}
	return map[string]any{
		"columns":    res.Columns,
		"rows":       res.Rows,
		"rowCount":   res.RowCount,
		"truncated":  res.Truncated,
		"durationMs": res.DurationMs,
	}, nil
}

func normalizeParams(raw any) ([]any, error) {
	if raw == nil {
		return nil, nil
	}
	arr, ok := raw.([]any)
	if !ok {
		return nil, fmt.Errorf("params must be an array")
	}
	out := make([]any, 0, len(arr))
	for _, item := range arr {
		switch v := item.(type) {
		case string, float64, bool, nil:
			out = append(out, v)
		default:
			return nil, fmt.Errorf("params contains unsupported type")
		}
	}
	return out, nil
}
