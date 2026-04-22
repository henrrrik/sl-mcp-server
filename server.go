package main

import (
	"log"
	"os"

	"github.com/henrrrik/sl-mcp-server/slclient"
	"github.com/henrrrik/sl-mcp-server/tools"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

func NewSLServer(client slclient.HTTPDoer) *server.MCPServer {
	logger := log.New(os.Stderr, "", log.LstdFlags)

	s := server.NewMCPServer(
		"sl-mcp-server",
		"1.2.0",
		server.WithToolCapabilities(true),
	)

	add := func(tool mcp.Tool, handler server.ToolHandlerFunc) {
		s.AddTool(tool, tools.WithLogging(logger, tool.Name, handler))
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
