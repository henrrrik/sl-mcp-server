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
	body, errResult := fetchJSONRaw(ctx, client, rawURL)
	if errResult != nil {
		return errResult, nil
	}
	return mcp.NewToolResultText(string(body)), nil
}

// errResultText extracts the first text content from an error *CallToolResult,
// for log/error-message composition.
func errResultText(r *mcp.CallToolResult) string {
	if r == nil || len(r.Content) == 0 {
		return ""
	}
	if tc, ok := r.Content[0].(mcp.TextContent); ok {
		return tc.Text
	}
	return ""
}

// fetchJSONRaw is fetchJSON without the final MCP-text wrapping. Returns the
// raw response body on success, or a populated *mcp.CallToolResult describing
// a transport or HTTP error. Exactly one of (body, errResult) is non-nil.
func fetchJSONRaw(ctx context.Context, client slclient.HTTPDoer, rawURL string) ([]byte, *mcp.CallToolResult) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, mcp.NewToolResultError(err.Error())
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, mcp.NewToolResultError(err.Error())
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, mcp.NewToolResultError(fmt.Sprintf("SL API returned HTTP %d", resp.StatusCode))
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseSize))
	if err != nil {
		return nil, mcp.NewToolResultError(err.Error())
	}

	return body, nil
}

func SitesTool(client slclient.HTTPDoer) (mcp.Tool, server.ToolHandlerFunc) {
	tool := mcp.NewTool("sites",
		mcp.WithDescription("Enumerate SL's catalog of transit sites (stations/stops) and their canonical numeric site_ids. Use this to look up the site_id for tools like departures. Matching is exact substring only — for typo-tolerant or ranked name search, use stop_finder instead. The full list is ~6500 entries; combine query and limit to narrow the result."),
		mcp.WithString("query", mcp.Description("Case-insensitive substring match on site name. Exact substrings only — no fuzzy matching.")),
		mcp.WithNumber("limit", mcp.Description("Maximum number of sites to return. Omitted or 0 means no limit.")),
	)

	handler := func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		query := request.GetString("query", "")
		limit := request.GetInt("limit", 0)

		u := slclient.BuildURL(transportBase, "/v1/sites", nil)

		if query == "" && limit <= 0 {
			return fetchJSON(ctx, client, u)
		}

		body, errResult := fetchJSONRaw(ctx, client, u)
		if errResult != nil {
			return errResult, nil
		}
		filtered, err := filterByName(body, query, limit)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("failed to filter sites: %v", err)), nil
		}
		return mcp.NewToolResultText(string(filtered)), nil
	}

	return tool, handler
}

func DeparturesTool(client slclient.HTTPDoer) (mcp.Tool, server.ToolHandlerFunc) {
	tool := mcp.NewTool("departures",
		mcp.WithDescription("Get real-time departures from an SL transit site. The returned stop_deviations are rebuilt from /v1/messages, filtered to scopes that touch this site's stop_areas, stop_points, or lines, so the disruptions attached actually apply here — with a fallback to filtering upstream's raw list if /v1/messages is unreachable. Use the sites tool to discover the id."),
		mcp.WithNumber("site_id", mcp.Required(), mcp.Description("Site ID from the sites tool (e.g. 9192 for Slussen, 9001 for T-Centralen).")),
	)

	handler := func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		siteID := request.GetInt("site_id", 0)
		if siteID == 0 {
			return mcp.NewToolResultError("site_id is required"), nil
		}

		params := url.Values{}
		path := fmt.Sprintf("/v1/sites/%d/departures", siteID)
		u := slclient.BuildURL(transportBase, path, params)

		body, errResult := fetchJSONRaw(ctx, client, u)
		if errResult != nil {
			return errResult, nil
		}

		// Best-effort fetch of /v1/messages so we can rebuild stop_deviations
		// from a trustworthy source. If this fails, trimDepartures falls back
		// to filtering upstream's (less trustworthy) list.
		msgsURL := slclient.BuildURL(deviationsBase, "/v1/messages", url.Values{"future": {"true"}})
		msgsBody, _ := fetchJSONRaw(ctx, client, msgsURL)

		trimmed, err := trimDepartures(body, msgsBody)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("failed to reshape departures response: %v", err)), nil
		}
		return mcp.NewToolResultText(string(trimmed)), nil
	}

	return tool, handler
}

func LinesTool(client slclient.HTTPDoer) (mcp.Tool, server.ToolHandlerFunc) {
	tool := mcp.NewTool("lines",
		mcp.WithDescription("Enumerate SL's catalog of transit lines and their canonical numeric ids. Returns a flat JSON array of line objects (upstream groups them by mode, but each object carries transport_mode so the grouping adds no information). Combine transport_mode, query, and limit to narrow the result."),
		mcp.WithNumber("transport_authority_id", mcp.Description("Transport authority ID from the transport_authorities tool. Defaults to 1 (Storstockholms Lokaltrafik).")),
		mcp.WithString("transport_mode", mcp.Description("Restrict to a single mode (case-insensitive): metro, bus, tram, train, ferry, ship, taxi. Unknown modes return an empty array.")),
		mcp.WithString("query", mcp.Description("Case-insensitive substring match on line name OR designation. Example: 'röda' matches the Red metro lines; '471' matches bus 471 via its designation.")),
		mcp.WithNumber("limit", mcp.Description("Maximum number of lines to return. Omitted or 0 means no limit.")),
	)

	handler := func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		authorityID := request.GetInt("transport_authority_id", 1)
		transportMode := request.GetString("transport_mode", "")
		query := request.GetString("query", "")
		limit := request.GetInt("limit", 0)

		params := url.Values{"transport_authority_id": {fmt.Sprintf("%d", authorityID)}}
		u := slclient.BuildURL(transportBase, "/v1/lines", params)

		body, errResult := fetchJSONRaw(ctx, client, u)
		if errResult != nil {
			return errResult, nil
		}
		filtered, err := flattenAndFilterLines(body, transportMode, query, limit)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("failed to reshape lines response: %v", err)), nil
		}
		return mcp.NewToolResultText(string(filtered)), nil
	}

	return tool, handler
}

func StopPointsTool(client slclient.HTTPDoer) (mcp.Tool, server.ToolHandlerFunc) {
	tool := mcp.NewTool("stop_points",
		mcp.WithDescription("Enumerate SL's catalog of stop points (individual platforms, quays, and stands within a site) and their canonical numeric ids. Matching is exact substring only. The full list is large; combine query and limit to narrow the result."),
		mcp.WithString("query", mcp.Description("Case-insensitive substring match on stop point name. Exact substrings only — no fuzzy matching.")),
		mcp.WithNumber("limit", mcp.Description("Maximum number of stop points to return. Omitted or 0 means no limit.")),
	)

	handler := func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		query := request.GetString("query", "")
		limit := request.GetInt("limit", 0)

		u := slclient.BuildURL(transportBase, "/v1/stop-points", nil)

		if query == "" && limit <= 0 {
			return fetchJSON(ctx, client, u)
		}

		body, errResult := fetchJSONRaw(ctx, client, u)
		if errResult != nil {
			return errResult, nil
		}
		filtered, err := filterByName(body, query, limit)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("failed to filter stop points: %v", err)), nil
		}
		return mcp.NewToolResultText(string(filtered)), nil
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
