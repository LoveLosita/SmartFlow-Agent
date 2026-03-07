package mcp

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/LoveLosita/smartflow/infra/smartflow-mcp-server/internal/ratelimit"
	"github.com/LoveLosita/smartflow/infra/smartflow-mcp-server/internal/tools"
)

type dummyTool struct{}

func (d dummyTool) Name() string        { return "dummy" }
func (d dummyTool) Description() string { return "dummy" }
func (d dummyTool) InputSchema() map[string]any {
	return map[string]any{"type": "object"}
}
func (d dummyTool) Execute(ctx context.Context, args map[string]any) (map[string]any, error) {
	return map[string]any{"ok": true}, nil
}

func TestHandleInitialize(t *testing.T) {
	registry, err := tools.NewRegistry(dummyTool{})
	if err != nil {
		t.Fatal(err)
	}
	out := bytes.NewBuffer(nil)
	s := NewServer(bytes.NewBuffer(nil), out, registry, nil, ratelimit.New(10, 10), "name", "ver", "2024-11-05", "unknown", time.Second)

	req := request{JSONRPC: jsonRPCVersion, ID: json.RawMessage("1"), Method: "initialize", Params: json.RawMessage(`{"protocolVersion":"2024-11-05"}`)}
	if err := s.handleRequest(context.Background(), req); err != nil {
		t.Fatalf("handle initialize error: %v", err)
	}
	if !bytes.Contains(out.Bytes(), []byte("protocolVersion")) {
		t.Fatalf("response missing protocolVersion: %s", out.String())
	}
}

func TestReadMessage(t *testing.T) {
	body := []byte(`{"jsonrpc":"2.0","id":1,"method":"ping"}`)
	frame := fmt.Sprintf("Content-Length: %d\r\n\r\n%s", len(body), string(body))
	input := bufio.NewReader(bytes.NewBufferString(frame))

	msg, err := readMessage(input)
	if err != nil {
		t.Fatalf("read message failed: %v", err)
	}
	if !bytes.Equal(msg, body) {
		t.Fatalf("unexpected body: %s", string(msg))
	}
}
