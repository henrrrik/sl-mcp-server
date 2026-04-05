package tools

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"

	"github.com/henrrrik/sl-mcp-server/slclient"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

const transportBase = "https://transport.integration.sl.se"

const maxResponseSize = 5 * 1024 * 1024 // 5MB

func fetchJSON(ctx context.Context, client slclient.HTTPDoer, rawURL string) (*mcp.CallToolResult, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	resp, err := client.Do(req)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return mcp.NewToolResultError(fmt.Sprintf("SL API returned HTTP %d", resp.StatusCode)), nil
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseSize))
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	return mcp.NewToolResultText(string(body)), nil
}

func SitesTool(client slclient.HTTPDoer) (mcp.Tool, server.ToolHandlerFunc) {
	tool := mcp.NewTool("sites",
		mcp.WithDescription("List SL transit sites (stations/stops) in Stockholm"),
	)

	handler := func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		u := slclient.BuildURL(transportBase, "/v1/sites", nil)
		return fetchJSON(ctx, client, u)
	}

	return tool, handler
}

func DeparturesTool(client slclient.HTTPDoer) (mcp.Tool, server.ToolHandlerFunc) {
	tool := mcp.NewTool("departures",
		mcp.WithDescription("Get real-time departures from an SL transit site"),
		mcp.WithNumber("site_id", mcp.Required(), mcp.Description("Site ID (use sl_sites to find IDs)")),
	)

	handler := func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		siteID := request.GetInt("site_id", 0)
		if siteID == 0 {
			return mcp.NewToolResultError("site_id is required"), nil
		}

		params := url.Values{}
		path := fmt.Sprintf("/v1/sites/%d/departures", siteID)
		u := slclient.BuildURL(transportBase, path, params)
		return fetchJSON(ctx, client, u)
	}

	return tool, handler
}

func LinesTool(client slclient.HTTPDoer) (mcp.Tool, server.ToolHandlerFunc) {
	tool := mcp.NewTool("lines",
		mcp.WithDescription("List SL transit lines"),
	)

	handler := func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		u := slclient.BuildURL(transportBase, "/v1/lines", nil)
		return fetchJSON(ctx, client, u)
	}

	return tool, handler
}

func StopPointsTool(client slclient.HTTPDoer) (mcp.Tool, server.ToolHandlerFunc) {
	tool := mcp.NewTool("stop_points",
		mcp.WithDescription("List SL stop points (platforms, quays)"),
	)

	handler := func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		u := slclient.BuildURL(transportBase, "/v1/stop-points", nil)
		return fetchJSON(ctx, client, u)
	}

	return tool, handler
}

func TransportAuthoritiesTool(client slclient.HTTPDoer) (mcp.Tool, server.ToolHandlerFunc) {
	tool := mcp.NewTool("transport_authorities",
		mcp.WithDescription("List transport authorities in Stockholm region"),
	)

	handler := func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		u := slclient.BuildURL(transportBase, "/v1/transport-authorities", nil)
		return fetchJSON(ctx, client, u)
	}

	return tool, handler
}
