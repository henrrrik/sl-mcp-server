package main

import (
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
	"sl-mcp-server/slclient"
	"sl-mcp-server/tools"
)

func NewSLServer(client slclient.HTTPDoer) *server.MCPServer {
	s := server.NewMCPServer(
		"sl-mcp-server",
		"1.0.0",
		server.WithToolCapabilities(true),
	)

	add := func(tool mcp.Tool, handler server.ToolHandlerFunc) {
		s.AddTool(tool, handler)
	}

	add(tools.DeviationsTool(client))
	add(tools.SystemInfoTool(client))
	add(tools.StopFinderTool(client))
	add(tools.TripsTool(client))
	add(tools.SitesTool(client))
	add(tools.DeparturesTool(client))
	add(tools.LinesTool(client))
	add(tools.StopPointsTool(client))
	add(tools.TransportAuthoritiesTool(client))

	return s
}
