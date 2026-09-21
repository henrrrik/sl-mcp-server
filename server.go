package main

import (
	"context"
	"log"
	"os"
	"time"

	"github.com/henrrrik/sl-mcp-server/slclient"
	"github.com/henrrrik/sl-mcp-server/tools"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

// toolCallTimeout bounds one whole tool call, across every upstream hop it
// makes. The per-request http.Client timeout (15 s) only bounds a single
// hop; trips can chain four of them, so without this a degraded SL API can
// hold a handler open for a minute after the client has already given up.
const toolCallTimeout = 30 * time.Second

func NewSLServer(client slclient.HTTPDoer) *server.MCPServer {
	return newSLServer(client, toolCallTimeout)
}

func newSLServer(client slclient.HTTPDoer, timeout time.Duration) *server.MCPServer {
	logger := log.New(os.Stderr, "", log.LstdFlags)

	// Middleware runs outermost-first in the order given: recovery wraps the
	// timeout so a panic anywhere below becomes a JSON-RPC error instead of
	// taking down the process and every connected SSE session with it.
	s := server.NewMCPServer(
		"sl-mcp-server",
		"1.2.0",
		server.WithToolCapabilities(true),
		server.WithRecovery(),
		server.WithToolHandlerMiddleware(withTimeout(timeout)),
	)

	add := func(tool mcp.Tool, handler server.ToolHandlerFunc) {
		s.AddTool(tool, tools.WithLogging(logger, tool.Name, handler))
	}

	add(tools.DeviationsTool(client))
	add(tools.SystemInfoTool(client))
	add(tools.StopFinderTool(client))
	add(tools.ResolveTool(client))
	add(tools.TripsTool(client))
	add(tools.SitesTool(client))
	add(tools.DeparturesTool(client))
	add(tools.LinesTool(client))
	add(tools.StopPointsTool(client))
	add(tools.TransportAuthoritiesTool(client))
	add(tools.NearestStopsTool(client))

	return s
}

// withTimeout gives every tool call a single deadline. mcp-go hands handlers
// a context that is never cancelled on client disconnect, so this is the
// only bound a call has; fetchJSONRaw honours it via NewRequestWithContext.
func withTimeout(d time.Duration) server.ToolHandlerMiddleware {
	return func(next server.ToolHandlerFunc) server.ToolHandlerFunc {
		return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			ctx, cancel := context.WithTimeout(ctx, d)
			defer cancel()
			return next(ctx, req)
		}
	}
}
