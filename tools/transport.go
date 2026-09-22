package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/henrrrik/sl-mcp-server/slclient"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

const transportBase = "https://transport.integration.sl.se"

// maxResponseSize caps how much of an upstream body we buffer. The largest
// catalog SL serves is /v1/stop-points at ~8 MB decompressed (the cap
// applies after Go's transparent gzip inflate), so 16 MB leaves headroom.
// Bodies over the cap are rejected with upstream_response_too_large rather
// than silently truncated.
const maxResponseSize = 16 * 1024 * 1024

const errUpstreamResponseTooLarge = "upstream_response_too_large"

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
		return nil, mcp.NewToolResultError(transportErrorJSON(rawURL, err))
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, mcp.NewToolResultError(upstreamHTTPErrorJSON(rawURL, resp))
	}

	body, tooLarge, err := readBodyLimited(resp.Body, maxResponseSize)
	if err != nil {
		return nil, mcp.NewToolResultError(err.Error())
	}
	if tooLarge {
		return nil, mcp.NewToolResultError(tooLargeErrorJSON(rawURL))
	}

	return body, nil
}

// Upstream error codes. Machine-readable so callers (and the request log)
// can tell an SL outage from bad input without parsing prose.
const (
	errUpstreamHTTP        = "upstream_http_error"
	errUpstreamUnreachable = "upstream_unreachable"
	errUpstreamTimeout     = "upstream_timeout"
)

// upstreamErrorBodySnippet bounds how much of an error body is echoed.
// SL's 4xx bodies are short JSON explanations; 5xx bodies can be HTML.
const upstreamErrorBodySnippet = 300

// upstreamHTTPErrorJSON describes a non-2xx upstream reply: status, the
// start of the body (SL's 400s say exactly what was wrong), and any
// Retry-After so a caller can back off sensibly.
func upstreamHTTPErrorJSON(rawURL string, resp *http.Response) string {
	payload := map[string]any{
		"error":  errUpstreamHTTP,
		"status": resp.StatusCode,
		"url":    rawURL,
	}
	if ra := resp.Header.Get("Retry-After"); ra != "" {
		payload["retry_after"] = ra
	}
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	if snippet := strings.TrimSpace(string(body)); snippet != "" {
		if len(snippet) > upstreamErrorBodySnippet {
			snippet = snippet[:upstreamErrorBodySnippet] + "…"
		}
		payload["body"] = snippet
	}
	b, _ := json.Marshal(payload)
	return string(b)
}

// transportErrorJSON describes a request that never got an HTTP reply:
// a timeout (the per-call deadline or the client's own) or a connection
// failure.
func transportErrorJSON(rawURL string, err error) string {
	code := errUpstreamUnreachable
	if errors.Is(err, context.DeadlineExceeded) {
		code = errUpstreamTimeout
	}
	b, _ := json.Marshal(map[string]any{
		"error":  code,
		"url":    rawURL,
		"detail": err.Error(),
	})
	return string(b)
}

// prefetch starts fetching rawURL immediately and returns a func that
// blocks for the result, so a fetch with no dependency on the primary
// response (e.g. /v1/messages) overlaps it instead of following it. The
// result channel is buffered, so an abandoned prefetch doesn't leak.
func prefetch(ctx context.Context, client slclient.HTTPDoer, rawURL string) func() ([]byte, *mcp.CallToolResult) {
	type result struct {
		body      []byte
		errResult *mcp.CallToolResult
	}
	ch := make(chan result, 1)
	go func() {
		body, errResult := fetchJSONRaw(ctx, client, rawURL)
		ch <- result{body, errResult}
	}()
	return func() ([]byte, *mcp.CallToolResult) {
		r := <-ch
		return r.body, r.errResult
	}
}

// readBodyLimited reads at most limit bytes from r and reports whether r
// held more than that. Reading limit+1 is what lets us tell "exactly at the
// cap" apart from "truncated" — a plain LimitReader cannot.
func readBodyLimited(r io.Reader, limit int64) (body []byte, tooLarge bool, err error) {
	body, err = io.ReadAll(io.LimitReader(r, limit+1))
	if err != nil {
		return nil, false, err
	}
	if int64(len(body)) > limit {
		return nil, true, nil
	}
	return body, false, nil
}

func tooLargeErrorJSON(rawURL string) string {
	b, _ := json.Marshal(map[string]any{
		"error":       errUpstreamResponseTooLarge,
		"limit_bytes": maxResponseSize,
		"url":         rawURL,
		"hint":        "The upstream response exceeded the server's buffer cap. Narrow the request with query/limit parameters.",
	})
	return string(b)
}

func SitesTool(client slclient.HTTPDoer) (mcp.Tool, server.ToolHandlerFunc) {
	tool := mcp.NewTool("sites",
		mcp.WithDescription("Enumerate SL's catalog of transit sites (stations/stops) and their canonical numeric site_ids. Use this to look up the site_id for tools like departures. Matching is exact substring only — for typo-tolerant or ranked name search, use stop_finder instead. The full list is ~6500 entries; the default page is 200, so combine query and limit to narrow the result, or pass limit=0 for the full catalog."),
		mcp.WithString("query", mcp.Description("Case-insensitive substring match on site name. Exact substrings only — no fuzzy matching.")),
		mcp.WithNumber("limit", mcp.Description("Maximum number of sites to return. Default 200; pass 0 for the full catalog.")),
	)

	handler := func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		query := request.GetString("query", "")
		// A no-argument call used to return the whole ~1.3 MB catalog as one
		// text result; page it by default and let limit=0 ask for everything.
		limit := request.GetInt("limit", defaultSitesLimit)

		u := slclient.BuildURL(transportBase, "/v1/sites", nil)
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
		mcp.WithDescription("Get real-time departures from an SL transit site. Accepts the short-form site_id from SL:sites, the 8-digit 18xx form from SL:stop_finder.properties.stopId, or the 16-digit GID from SL:stop_finder.id — all are normalized to the short form before the upstream call. Default limit is 20 departures; narrow further with transport_mode / line / direction_code or raise with limit=0 (unlimited). By default, per-row stop_area / journey internals are dropped (every row belongs to the queried site) and line is slimmed to designation/transport_mode/group_of_lines; set verbose=true for the full upstream shape. stop_deviations are rebuilt from /v1/messages, filtered to scopes that touch this site — with a fallback to the upstream's raw list if /v1/messages is unreachable."),
		mcp.WithString("site_id", mcp.Required(), mcp.Description("Site ID. Accepts the short form from SL:sites (e.g. \"9702\"), the 8-digit form from stop_finder.properties.stopId (e.g. \"18009702\"), or the 16-digit GID from stop_finder.id (e.g. \"9091001000009702\"). All are normalized to the short form before the upstream call. Pass as a string — 16-digit GIDs exceed JS Number.MAX_SAFE_INTEGER and lose precision if typed as a number.")),
		mcp.WithString("transport_mode", mcp.Description("Filter departures by line mode: BUS, METRO, TRAIN, TRAM, SHIP, FERRY, TAXI. Case-insensitive.")),
		mcp.WithString("line", mcp.Description("Filter departures by line designation (prefix match, case-insensitive). Example: \"43\" matches pendeltåg 43 and 43X; \"54\" matches the 54x bus family (54, 540, 541, …).")),
		mcp.WithNumber("direction_code", mcp.Description("Filter departures by direction code (SL's upstream field; typically 1 or 2). Use to show only departures heading one way.")),
		mcp.WithNumber("limit", mcp.Description("Maximum number of departures to return after filtering. Default 20. Pass 0 for unlimited (keeps the upstream's default page size, typically 35).")),
		mcp.WithBoolean("verbose", mcp.Description("Preserve per-departure stop_area / journey / full line object. Default false (slim: stop_area and journey dropped, line reduced to designation/transport_mode/group_of_lines).")),
	)

	handler := func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		siteID, present, errResult := normalizeSiteIDArg(request.GetArguments(), "site_id")
		if errResult != nil {
			return errResult, nil
		}
		if !present {
			return mcp.NewToolResultError("site_id is required"), nil
		}

		// Default limit caps a busy-terminal response at a readable page
		// size. Callers who need the full upstream set pass limit=0.
		filters := departuresFilters{
			transportMode: request.GetString("transport_mode", ""),
			line:          request.GetString("line", ""),
			directionCode: request.GetInt("direction_code", 0),
			limit:         request.GetInt("limit", defaultDeparturesLimit),
		}

		params := url.Values{}
		// /v1/messages doesn't depend on the departures response, so start
		// it now and let the two fetches overlap.
		msgsURL := slclient.BuildURL(deviationsBase, "/v1/messages", url.Values{"future": {"true"}})
		messages := prefetch(ctx, client, msgsURL)

		path := fmt.Sprintf("/v1/sites/%d/departures", siteID)
		u := slclient.BuildURL(transportBase, path, params)

		body, errResult := fetchJSONRaw(ctx, client, u)
		if errResult != nil {
			return errResult, nil
		}

		// Best-effort: if /v1/messages fails, trimDepartures falls back to
		// filtering upstream's (less trustworthy) stop_deviations, and the
		// response says so — the fallback entries have a different shape and
		// no publish-window check.
		msgsBody, msgsErr := messages()

		trimmed, err := trimDepartures(body, msgsBody, filters, request.GetBool("verbose", false))
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("failed to reshape departures response: %v", err)), nil
		}
		if msgsErr != nil {
			trimmed, err = injectTopLevel(trimmed, map[string]any{"warnings": []tripWarning{deviationsUnavailableWarning(msgsErr)}})
			if err != nil {
				return mcp.NewToolResultError(fmt.Sprintf("failed to attach warnings: %v", err)), nil
			}
		}
		return mcp.NewToolResultText(string(trimmed)), nil
	}

	return tool, handler
}

func LinesTool(client slclient.HTTPDoer) (mcp.Tool, server.ToolHandlerFunc) {
	tool := mcp.NewTool("lines",
		mcp.WithDescription("Enumerate SL's catalog of transit lines and their canonical numeric ids. Returns a flat JSON array — by default slimmed to {id, designation, transport_mode, group_of_lines, name}; set verbose=true for the full upstream fields (gid, transport_authority, contractor, valid). The full catalog is ~600 entries; always narrow the result with transport_mode, designation, or group_of_lines before reading it into an LLM context. Default limit is 50; pass limit=0 to page through the full catalog."),
		mcp.WithNumber("transport_authority_id", mcp.Description("Transport authority ID from the transport_authorities tool. Defaults to 1 (Storstockholms Lokaltrafik).")),
		mcp.WithString("transport_mode", mcp.Description("Restrict to a single mode (case-insensitive): metro, bus, tram, train, ferry, ship, taxi. Unknown modes return an empty array.")),
		mcp.WithString("designation", mcp.Description("Prefix match on the line's designation. '54' matches 54 / 540 / 541 / 542 / …. Narrower than query; prefer this when searching by line number.")),
		mcp.WithString("group_of_lines", mcp.Description("Case-insensitive substring match on the group_of_lines field. Examples: 'pendeltåg', 'blåbuss', 'närtrafiken'.")),
		mcp.WithString("query", mcp.Description("Case-insensitive substring match on line name OR designation. Example: 'röda' matches the Red metro lines; '471' matches bus 471. Broader than designation.")),
		mcp.WithNumber("limit", mcp.Description("Maximum number of lines to return. Defaults to 50; pass 0 for unlimited.")),
		mcp.WithBoolean("verbose", mcp.Description("Return the full upstream shape per line (includes gid, transport_authority, contractor, valid, etc.). Default false (slim: id, designation, transport_mode, group_of_lines, name).")),
	)

	handler := func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		authorityID := request.GetInt("transport_authority_id", 1)
		filters := linesFilters{
			mode:         strings.ToLower(request.GetString("transport_mode", "")),
			query:        strings.ToLower(request.GetString("query", "")),
			designation:  strings.ToLower(request.GetString("designation", "")),
			groupOfLines: strings.ToLower(request.GetString("group_of_lines", "")),
			limit:        request.GetInt("limit", defaultLinesLimit),
			verbose:      request.GetBool("verbose", false),
		}

		params := url.Values{"transport_authority_id": {fmt.Sprintf("%d", authorityID)}}
		u := slclient.BuildURL(transportBase, "/v1/lines", params)

		body, errResult := fetchJSONRaw(ctx, client, u)
		if errResult != nil {
			return errResult, nil
		}
		filtered, err := flattenAndFilterLines(body, filters)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("failed to reshape lines response: %v", err)), nil
		}
		return mcp.NewToolResultText(string(filtered)), nil
	}

	return tool, handler
}

// defaultLinesLimit caps the no-arg response at something an LLM can
// reasonably read. Callers asking for the full ~600-entry catalog must
// opt in with limit=0 (unlimited).
const defaultLinesLimit = 50

// defaultDeparturesLimit caps the no-arg response to a single "page" of
// upcoming departures. SL's upstream typically returns 35; 20 is enough
// for "when's my next X" queries without flooding the context.
const defaultDeparturesLimit = 20

// defaultSitesLimit pages a no-argument sites call; the full catalog is
// ~6500 entries (~1.3 MB), far beyond any useful LLM context.
const defaultSitesLimit = 200

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
