package tools

import (
	"context"
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
		} else if result != nil && result.IsError {
			logger.Printf("tool=%s params=%s duration=%s error=true", toolName, params, duration)
		} else {
			logger.Printf("tool=%s params=%s duration=%s", toolName, params, duration)
		}

		return result, err
	}
}

func formatParams(req mcp.CallToolRequest) string {
	args := req.GetArguments()
	if len(args) == 0 {
		return "{}"
	}
	parts := make([]string, 0, len(args))
	for k, v := range args {
		parts = append(parts, fmt.Sprintf("%s=%v", k, v))
	}
	return "{" + strings.Join(parts, ", ") + "}"
}
