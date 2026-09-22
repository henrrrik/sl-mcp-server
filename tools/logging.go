package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

func WithLogging(logger *log.Logger, toolName string, handler server.ToolHandlerFunc) server.ToolHandlerFunc {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		start := time.Now()
		params := formatParams(req)

		result, err := handler(ctx, req)

		duration := time.Since(start)

		if err != nil {
			logger.Printf("tool=%s params=%s duration=%s error=%v", toolName, params, duration, err)
			return result, err
		}
		logger.Printf("tool=%s params=%s duration=%s %s", toolName, params, duration, describeOutcome(result))
		return result, err
	}
}

// describeOutcome classifies a result for the log line so operators can
// separate outages from bad input. Error results and text results whose
// body is a structured {"error": "..."} envelope (ambiguity pickers,
// not-a-stop hints, site-id errors) both get their code; an upstream HTTP
// error also gets its status.
func describeOutcome(result *mcp.CallToolResult) string {
	if result == nil {
		return "outcome=ok"
	}
	code, status := structuredErrorCode(result)
	switch {
	case result.IsError && code != "":
		return withStatus("error=true code="+code, status)
	case result.IsError:
		return "error=true"
	case code != "":
		return withStatus("outcome=structured_error code="+code, status)
	default:
		return "outcome=ok"
	}
}

func withStatus(s string, status int) string {
	if status == 0 {
		return s
	}
	return fmt.Sprintf("%s status=%d", s, status)
}

// structuredErrorCode extracts "error" (and "status", if any) from a JSON
// object result. Returns "" when the result isn't such an envelope.
func structuredErrorCode(result *mcp.CallToolResult) (code string, status int) {
	text := errResultText(result)
	if !strings.HasPrefix(strings.TrimSpace(text), `{"error"`) {
		return "", 0
	}
	var env struct {
		Error  string `json:"error"`
		Status int    `json:"status"`
	}
	if json.Unmarshal([]byte(text), &env) != nil {
		return "", 0
	}
	return env.Error, env.Status
}

func formatParams(req mcp.CallToolRequest) string {
	args := req.GetArguments()
	if len(args) == 0 {
		return "{}"
	}
	parts := make([]string, 0, len(args))
	for k, v := range args {
		if redactedParams[k] {
			parts = append(parts, k+"=<redacted>")
			continue
		}
		parts = append(parts, fmt.Sprintf("%s=%v", k, v))
	}
	return "{" + strings.Join(parts, ", ") + "}"
}

// redactedParams are never written to the request log: coordinates are a
// user's location.
var redactedParams = map[string]bool{"lat": true, "lon": true}
