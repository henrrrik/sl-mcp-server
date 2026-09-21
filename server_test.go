package main

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/henrrrik/sl-mcp-server/slclient"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

func TestNewSLServer(t *testing.T) {
	client := slclient.NewClient()
	s := NewSLServer(client)
	if s == nil {
		t.Fatal("NewSLServer returned nil")
	}
}

// doerFunc adapts a func to slclient.HTTPDoer.
type doerFunc func(*http.Request) (*http.Response, error)

func (f doerFunc) Do(req *http.Request) (*http.Response, error) { return f(req) }

// callTool drives a tools/call through the server's JSON-RPC dispatch, so
// server-level middleware (recovery, timeout) is exercised the same way it
// is over SSE. Returns the raw JSON-RPC response.
func callTool(t *testing.T, s *server.MCPServer, name string, args map[string]any) map[string]any {
	t.Helper()
	req := map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "tools/call",
		"params":  map[string]any{"name": name, "arguments": args},
	}
	raw, _ := json.Marshal(req)
	msg := s.HandleMessage(context.Background(), raw)
	out, err := json.Marshal(msg)
	if err != nil {
		t.Fatalf("marshal response: %v", err)
	}
	var resp map[string]any
	if err := json.Unmarshal(out, &resp); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	return resp
}

// A panic inside any tool handler must be turned into a JSON-RPC error,
// not take down the process (and every connected SSE session with it).
func TestNewSLServer_RecoversFromHandlerPanic(t *testing.T) {
	s := NewSLServer(slclient.NewClient())
	s.AddTool(mcp.NewTool("panic_tool"), func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		panic("boom")
	})

	resp := callTool(t, s, "panic_tool", nil)

	errObj, ok := resp["error"].(map[string]any)
	if !ok {
		t.Fatalf("expected JSON-RPC error response, got %v", resp)
	}
	if msg, _ := errObj["message"].(string); !strings.Contains(msg, "panic") {
		t.Errorf("expected error message to mention the recovered panic, got %q", msg)
	}
}

// Upstream returning a JSON `null` body must surface as a tool error, not
// a nil-map panic in the transform.
func TestNewSLServer_DeparturesNullBodyIsToolError(t *testing.T) {
	client := doerFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader("null"))}, nil
	})
	s := NewSLServer(client)

	resp := callTool(t, s, "departures", map[string]any{"site_id": "9001"})

	result, ok := resp["result"].(map[string]any)
	if !ok {
		t.Fatalf("expected a tool result, got %v", resp)
	}
	if isErr, _ := result["isError"].(bool); !isErr {
		t.Errorf("expected isError=true for null upstream body, got %v", result)
	}
}

// Every tool call gets one overall deadline. A hung upstream must not hold
// the handler open beyond it, regardless of how many hops the tool chains.
func TestNewSLServer_ToolCallHonoursTimeout(t *testing.T) {
	client := doerFunc(func(req *http.Request) (*http.Response, error) {
		<-req.Context().Done()
		return nil, req.Context().Err()
	})
	s := newSLServer(client, 50*time.Millisecond)

	start := time.Now()
	resp := callTool(t, s, "sites", map[string]any{"query": "Slussen"})
	elapsed := time.Since(start)

	if elapsed > 2*time.Second {
		t.Fatalf("tool call took %s; expected the per-call timeout to cut it short", elapsed)
	}
	result, ok := resp["result"].(map[string]any)
	if !ok {
		t.Fatalf("expected a tool result, got %v", resp)
	}
	if isErr, _ := result["isError"].(bool); !isErr {
		t.Errorf("expected isError=true after timeout, got %v", result)
	}
}
