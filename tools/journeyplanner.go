package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"sync"
	"time"

	"github.com/henrrrik/sl-mcp-server/slclient"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

const journeyPlannerBase = "https://journeyplanner.integration.sl.se"

// SL's journey planner interprets itd_time as Europe/Stockholm local time.
const stockholmTZ = "Europe/Stockholm"

func SystemInfoTool(client slclient.HTTPDoer) (mcp.Tool, server.ToolHandlerFunc) {
	tool := mcp.NewTool("system_info",
		mcp.WithDescription("Get SL timetable validity period"),
	)

	handler := func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		u := slclient.BuildURL(journeyPlannerBase, "/v2/system-info", nil)
		return fetchJSON(ctx, client, u)
	}

	return tool, handler
}

func StopFinderTool(client slclient.HTTPDoer) (mcp.Tool, server.ToolHandlerFunc) {
	tool := mcp.NewTool("stop_finder",
		mcp.WithDescription("Find SL stops/stations by name"),
		mcp.WithString("name", mcp.Required(), mcp.Description("Stop name to search for")),
	)

	handler := func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		name := request.GetString("name", "")
		if name == "" {
			return mcp.NewToolResultError("name is required"), nil
		}

		params := url.Values{
			"name_sf":           {name},
			"type_sf":           {"any"},
			"any_obj_filter_sf": {"2"},
		}

		u := slclient.BuildURL(journeyPlannerBase, "/v2/stop-finder", params)
		return fetchJSON(ctx, client, u)
	}

	return tool, handler
}

func TripsTool(client slclient.HTTPDoer) (mcp.Tool, server.ToolHandlerFunc) {
	tool := mcp.NewTool("trips",
		mcp.WithDescription("Plan a trip between two locations in Stockholm. Returns a trimmed, LLM-friendly summary by default; set verbose=true for the full upstream payload."),
		mcp.WithString("origin", mcp.Required(), mcp.Description("Origin stop/location name")),
		mcp.WithString("destination", mcp.Required(), mcp.Description("Destination stop/location name")),
		mcp.WithNumber("number_of_trips", mcp.Description("Number of trips to return (1-3, default 3)")),
		mcp.WithString("time", mcp.Description("ISO 8601 departure/arrival time (e.g. 2026-04-22T09:00:00+02:00). Defaults to now.")),
		mcp.WithString("time_mode", mcp.Description("'depart' or 'arrive' (default 'depart'). Only meaningful when 'time' is set.")),
		mcp.WithBoolean("verbose", mcp.Description("Return the raw upstream response including coords, stopSequence, and footpath details. Default false.")),
		mcp.WithBoolean("skip_deviations", mcp.Description("Skip the second /v1/messages call that attaches active deviations to each transit leg. Default false.")),
	)

	handler := func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		origin := request.GetString("origin", "")
		destination := request.GetString("destination", "")
		if origin == "" || destination == "" {
			return mcp.NewToolResultError("origin and destination are required"), nil
		}

		numTrips := request.GetInt("number_of_trips", 3)
		if numTrips < 1 {
			numTrips = 1
		} else if numTrips > 3 {
			numTrips = 3
		}

		params := url.Values{
			"type_origin":          {"any"},
			"name_origin":          {origin},
			"type_destination":     {"any"},
			"name_destination":     {destination},
			"calc_number_of_trips": {fmt.Sprintf("%d", numTrips)},
		}

		if errResult := applyTripTime(request, params); errResult != nil {
			return errResult, nil
		}

		if request.GetBool("verbose", false) {
			u := slclient.BuildURL(journeyPlannerBase, "/v2/trips", params)
			return fetchJSON(ctx, client, u)
		}

		body, errResult := fetchTripsWithAmbiguityResolution(ctx, client, params, origin, destination)
		if errResult != nil {
			return errResult, nil
		}

		tt, err := reshapeTrips(body)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("failed to reshape trips response: %v", err)), nil
		}
		if tt == nil {
			// Error-only upstream response — pass through verbatim so
			// callers still see systemMessages.
			return mcp.NewToolResultText(string(body)), nil
		}

		if !request.GetBool("skip_deviations", false) {
			enrichWithDeviations(ctx, client, tt)
		}

		out, err := json.Marshal(tt)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("failed to encode trips response: %v", err)), nil
		}
		return mcp.NewToolResultText(string(out)), nil
	}

	return tool, handler
}

// enrichWithDeviations fetches active /v1/messages entries and attaches any
// that match each transit leg's (line, mode). Failures are swallowed — the
// trips response is still returned without deviations.
func enrichWithDeviations(ctx context.Context, client slclient.HTTPDoer, tt *trimmedTrips) {
	params := url.Values{"future": {"true"}}
	u := slclient.BuildURL(deviationsBase, "/v1/messages", params)
	body, errResult := fetchJSONRaw(ctx, client, u)
	if errResult != nil {
		return
	}
	index, err := indexDeviations(body)
	if err != nil {
		return
	}
	attachDeviations(tt, index)
}

// fetchTripsWithAmbiguityResolution fetches /v2/trips. When the broker reports
// ambiguity, it calls stop-finder for each ambiguous side in parallel and:
//   - if every ambiguous side resolves to exactly one candidate, silently
//     re-fetches /v2/trips with those IDs and returns the resulting body;
//   - otherwise, returns a structured error result naming only the side(s)
//     that still require user disambiguation.
func fetchTripsWithAmbiguityResolution(ctx context.Context, client slclient.HTTPDoer, params url.Values, origin, destination string) ([]byte, *mcp.CallToolResult) {
	u := slclient.BuildURL(journeyPlannerBase, "/v2/trips", params)
	body, errResult := fetchJSONRaw(ctx, client, u)
	if errResult != nil {
		return nil, errResult
	}

	originAmb, destAmb := detectAmbiguity(body)
	if !originAmb && !destAmb {
		return body, nil
	}

	oc, dc := fetchCandidatesInParallel(ctx, client, origin, destination, originAmb, destAmb)

	// A side is still ambiguous to the caller if the broker flagged it AND
	// we didn't land on exactly one candidate (either 0 = stop-finder failed
	// or ≥2 = real ambiguity). Otherwise we can silently use the sole match.
	originNeedsPicker := originAmb && len(oc) != 1
	destNeedsPicker := destAmb && len(dc) != 1

	if !originNeedsPicker && !destNeedsPicker {
		if originAmb {
			params.Set("name_origin", oc[0].ID)
		}
		if destAmb {
			params.Set("name_destination", dc[0].ID)
		}
		u = slclient.BuildURL(journeyPlannerBase, "/v2/trips", params)
		body, errResult = fetchJSONRaw(ctx, client, u)
		if errResult != nil {
			return nil, errResult
		}
		return body, nil
	}

	return nil, buildAmbiguityErrorResult(origin, destination, originNeedsPicker, destNeedsPicker, oc, dc)
}

// fetchCandidatesInParallel resolves stop-finder candidates for the ambiguous
// sides concurrently. Non-ambiguous sides get a nil slice.
func fetchCandidatesInParallel(ctx context.Context, client slclient.HTTPDoer, origin, destination string, originAmb, destAmb bool) ([]locationCandidate, []locationCandidate) {
	var (
		oc, dc []locationCandidate
		wg     sync.WaitGroup
	)
	if originAmb {
		wg.Add(1)
		go func() {
			defer wg.Done()
			oc, _ = resolveCandidates(ctx, client, origin)
		}()
	}
	if destAmb {
		wg.Add(1)
		go func() {
			defer wg.Done()
			dc, _ = resolveCandidates(ctx, client, destination)
		}()
	}
	wg.Wait()
	return oc, dc
}

// buildAmbiguityErrorResult produces the ambiguous_origin / ambiguous_destination
// / ambiguous_both structured error based on which side(s) the caller still has
// to disambiguate. Any side that's already been silently resolved is omitted.
func buildAmbiguityErrorResult(origin, destination string, originNeedsPicker, destNeedsPicker bool, oc, dc []locationCandidate) *mcp.CallToolResult {
	var body []byte
	var err error
	switch {
	case originNeedsPicker && destNeedsPicker:
		body, err = json.Marshal(ambiguityBothResponse{
			Error:       "ambiguous_both",
			Origin:      sideCandidates{Query: origin, Candidates: oc},
			Destination: sideCandidates{Query: destination, Candidates: dc},
		})
	case originNeedsPicker:
		body, err = json.Marshal(ambiguitySingleResponse{
			Error:      "ambiguous_origin",
			Query:      origin,
			Candidates: oc,
		})
	case destNeedsPicker:
		body, err = json.Marshal(ambiguitySingleResponse{
			Error:      "ambiguous_destination",
			Query:      destination,
			Candidates: dc,
		})
	}
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("failed to encode ambiguity response: %v", err))
	}
	return mcp.NewToolResultText(string(body))
}

// applyTripTime translates the public "time" / "time_mode" arguments into the
// upstream's snake_case EFA parameters (itd_date, itd_time, itd_trip_date_time_dep_arr).
// Returns a non-nil error result if the inputs are invalid; nil on success or if
// no time was provided (leaving the broker to default to "now").
func applyTripTime(request mcp.CallToolRequest, params url.Values) *mcp.CallToolResult {
	timeStr := request.GetString("time", "")
	modeStr := request.GetString("time_mode", "")

	if timeStr == "" {
		if modeStr != "" {
			return mcp.NewToolResultError("time_mode requires 'time' to be set")
		}
		return nil
	}

	t, err := time.Parse(time.RFC3339, timeStr)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("invalid time %q: %v", timeStr, err))
	}

	loc, err := time.LoadLocation(stockholmTZ)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("failed to load %s timezone: %v", stockholmTZ, err))
	}
	local := t.In(loc)

	if modeStr == "" {
		modeStr = "depart"
	}
	var depArr string
	switch modeStr {
	case "depart":
		depArr = "dep"
	case "arrive":
		depArr = "arr"
	default:
		return mcp.NewToolResultError(fmt.Sprintf("time_mode must be 'depart' or 'arrive', got %q", modeStr))
	}

	params.Set("itd_date", local.Format("20060102"))
	params.Set("itd_time", local.Format("1504"))
	params.Set("itd_trip_date_time_dep_arr", depArr)
	return nil
}
