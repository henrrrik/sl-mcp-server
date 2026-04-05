package tools

import (
	"context"
	"fmt"
	"net/url"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
	"github.com/henrrrik/sl-mcp-server/slclient"
)

const deviationsBase = "https://deviations.integration.sl.se"

func DeviationsTool(client slclient.HTTPDoer) (mcp.Tool, server.ToolHandlerFunc) {
	tool := mcp.NewTool("deviations",
		mcp.WithDescription("Get SL traffic deviations and disruptions in Stockholm public transport"),
		mcp.WithBoolean("future", mcp.Description("Include future deviations")),
		mcp.WithNumber("site", mcp.Description("Filter by site ID")),
		mcp.WithNumber("line", mcp.Description("Filter by line number")),
		mcp.WithString("transport_mode", mcp.Description("Filter by mode: BUS, METRO, TRAIN, TRAM, SHIP, FERRY")),
	)

	handler := func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		params := url.Values{}

		if v := request.GetBool("future", false); v {
			params.Set("future", "true")
		}
		if v := request.GetInt("site", 0); v != 0 {
			params.Set("site", fmt.Sprintf("%d", v))
		}
		if v := request.GetInt("line", 0); v != 0 {
			params.Set("line", fmt.Sprintf("%d", v))
		}
		if v := request.GetString("transport_mode", ""); v != "" {
			params.Set("transport_mode", v)
		}

		u := slclient.BuildURL(deviationsBase, "/v1/messages", params)
		return fetchJSON(ctx, client, u)
	}

	return tool, handler
}
