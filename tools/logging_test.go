package tools

import (
	"bytes"
	"context"
	"log"
	"strings"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

func TestWithLogging_Success(t *testing.T) {
	var buf bytes.Buffer
	logger := log.New(&buf, "", 0)

	inner := func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		return mcp.NewToolResultText("ok"), nil
	}

	wrapped := WithLogging(logger, "sl_test_tool", inner)

	req := mcp.CallToolRequest{}
	req.Params.Arguments = map[string]any{"foo": "bar"}

	result, err := wrapped(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsError {
		t.Fatal("expected success result")
	}

	output := buf.String()
	if !strings.Contains(output, "sl_test_tool") {
		t.Errorf("log should contain tool name, got: %s", output)
	}
	if !strings.Contains(output, "foo") {
		t.Errorf("log should contain param names, got: %s", output)
	}
}

func TestWithLogging_Error(t *testing.T) {
	var buf bytes.Buffer
	logger := log.New(&buf, "", 0)

	inner := func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		return mcp.NewToolResultError("something broke"), nil
	}

	wrapped := WithLogging(logger, "sl_failing_tool", inner)

	req := mcp.CallToolRequest{}
	req.Params.Arguments = map[string]any{}

	result, err := wrapped(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Fatal("expected error result")
	}

	output := buf.String()
	if !strings.Contains(output, "error") {
		t.Errorf("log should mention error, got: %s", output)
	}
}

var _ server.ToolHandlerFunc = WithLogging(nil, "", nil)
