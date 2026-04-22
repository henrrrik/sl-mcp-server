package tools

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/henrrrik/sl-mcp-server/slclient"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

const transportBase = "https://transport.integration.sl.se"

const maxResponseSize = 5 * 1024 * 1024 // 5MB

// Real site IDs from /v1/sites fall in 102..9999. Stop-finder returns a
// "180"-prefixed variant in properties.stopId (e.g. "18009192" = site 9192).
// Any numeric ID above this range that starts with "180" is the prefixed form.
const maxShortSiteID = 9999

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
		mcp.WithDescription("Get real-time departures from an SL transit site. Accepts either the short site ID from the sites tool (e.g. 9192) or the 180-prefixed form from stop-finder (e.g. 18009192)."),
		mcp.WithNumber("site_id", mcp.Required(), mcp.Description("Site ID. Short form (102-9999) or 180-prefixed form (e.g. 18009192).")),
	)

	handler := func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		siteID := request.GetInt("site_id", 0)
		if siteID == 0 {
			return mcp.NewToolResultError("site_id is required"), nil
		}
		siteID = normalizeSiteID(siteID)

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

// normalizeSiteID strips the "180" prefix returned by stop-finder's
// properties.stopId so it matches the short form accepted by /v1/sites.
// Only triggers on IDs outside the real short-id range (> 9999) to avoid
// rewriting legitimate IDs like 1800-1809 (Sköndals area).
func normalizeSiteID(id int) int {
	if id <= maxShortSiteID {
		return id
	}
	s := strconv.Itoa(id)
	rest, ok := strings.CutPrefix(s, "180")
	if !ok {
		return id
	}
	rest = strings.TrimLeft(rest, "0")
	if rest == "" {
		return id
	}
	n, err := strconv.Atoi(rest)
	if err != nil {
		return id
	}
	return n
}

func LinesTool(client slclient.HTTPDoer) (mcp.Tool, server.ToolHandlerFunc) {
	tool := mcp.NewTool("lines",
		mcp.WithDescription("List SL transit lines"),
		mcp.WithNumber("transport_authority_id", mcp.Description("Transport authority ID from the transport_authorities tool. Defaults to 1 (Storstockholms Lokaltrafik).")),
	)

	handler := func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		authorityID := request.GetInt("transport_authority_id", 1)
		params := url.Values{"transport_authority_id": {fmt.Sprintf("%d", authorityID)}}
		u := slclient.BuildURL(transportBase, "/v1/lines", params)
		return fetchJSON(ctx, client, u)
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
