package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strings"
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
		mcp.WithDescription("Fuzzy, ranked search for SL stops/stations/addresses by name. Tolerates typos and partial names and returns candidates ordered by match_quality — intended for resolving free-form user input before calling trips. Returns a flat JSON array of {id, name, lat, lon, match_quality, type}; id is the upstream 16-digit GID string which departures accepts directly. Non-stop entries (type != \"stop\") are kept so addresses and POIs can still be resolved. For enumerating the canonical site catalog or looking up a numeric site_id for departures, use sites instead."),
		mcp.WithString("name", mcp.Required(), mcp.Description("Free-form name or partial name to search for. Fuzzy matching is applied.")),
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
		body, errResult := fetchJSONRaw(ctx, client, u)
		if errResult != nil {
			return errResult, nil
		}
		trimmed, err := trimStopFinder(body)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("failed to reshape stop_finder response: %v", err)), nil
		}
		return mcp.NewToolResultText(string(trimmed)), nil
	}

	return tool, handler
}

func TripsTool(client slclient.HTTPDoer) (mcp.Tool, server.ToolHandlerFunc) {
	tool := mcp.NewTool("trips",
		mcp.WithDescription("Plan a trip between two locations in Stockholm. Prefer origin_id and destination_id over origin and destination when you already have a stop's id (from the sites, stop_finder, or resolve tools) — they skip name resolution and eliminate fuzzy-match drift onto POIs or addresses. Returns a trimmed, LLM-friendly summary by default; set verbose=true for the full upstream payload. Every successful response echoes a resolved block with the actual origin/destination the planner used so callers can detect silent drift."),
		mcp.WithString("origin", mcp.Description("Origin stop/location name. Exactly one of 'origin' or 'origin_id' must be provided. Free-form name matching; prefer origin_id when available.")),
		mcp.WithString("destination", mcp.Description("Destination stop/location name. Exactly one of 'destination' or 'destination_id' must be provided. Free-form name matching; prefer destination_id when available.")),
		mcp.WithString("origin_id", mcp.Description("Origin site id. Accepts the short form (e.g. \"9702\"), the 8-digit 18xx form, the 9-digit 3BA1CDEFG form, or the 16-digit GID. Skips fuzzy name resolution and prevents silent drift onto POIs/addresses. Pass as a string — 16-digit GIDs exceed JS Number.MAX_SAFE_INTEGER.")),
		mcp.WithString("destination_id", mcp.Description("Destination site id. Same format rules as origin_id.")),
		mcp.WithNumber("number_of_trips", mcp.Description("Number of trips to return (1-3, default 3)")),
		mcp.WithString("time", mcp.Description("ISO 8601 departure/arrival time (e.g. 2026-04-22T09:00:00+02:00). Defaults to now.")),
		mcp.WithString("time_mode", mcp.Description("'depart' or 'arrive' (default 'depart'). Only meaningful when 'time' is set.")),
		mcp.WithBoolean("verbose", mcp.Description("Return the raw upstream response including coords, stopSequence, and footpath details. Default false.")),
		mcp.WithBoolean("skip_deviations", mcp.Description("Skip the second /v1/messages call that attaches active deviations to each transit leg. Default false.")),
	)

	handler := func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		origin := strings.TrimSpace(request.GetString("origin", ""))
		destination := strings.TrimSpace(request.GetString("destination", ""))
		originIDArg := coerceSiteIDArg(request.GetArguments()["origin_id"])
		destinationIDArg := coerceSiteIDArg(request.GetArguments()["destination_id"])

		originParam, errResult := resolveTripSideParam("origin", origin, originIDArg)
		if errResult != nil {
			return errResult, nil
		}
		destParam, errResult := resolveTripSideParam("destination", destination, destinationIDArg)
		if errResult != nil {
			return errResult, nil
		}

		originProvidedAsName := origin != ""
		destProvidedAsName := destination != ""

		numTrips := request.GetInt("number_of_trips", 3)
		if numTrips < 1 {
			numTrips = 1
		} else if numTrips > 3 {
			numTrips = 3
		}

		params := url.Values{
			"type_origin":          {"any"},
			"name_origin":          {originParam},
			"type_destination":     {"any"},
			"name_destination":     {destParam},
			"calc_number_of_trips": {fmt.Sprintf("%d", numTrips)},
		}

		if errResult := applyTripTime(request, params); errResult != nil {
			return errResult, nil
		}

		if request.GetBool("verbose", false) {
			return fetchVerboseTrips(ctx, client, params)
		}

		body, errResult := fetchTripsWithAmbiguityResolution(ctx, client, params, origin, destination, originProvidedAsName, destProvidedAsName)
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

		tt.Resolved = extractResolved(body)

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

// fetchVerboseTrips returns the raw upstream /v2/trips body with only the
// resolved echo block injected, leaving every other upstream field (coords,
// stopSequence, footpath details) in place.
func fetchVerboseTrips(ctx context.Context, client slclient.HTTPDoer, params url.Values) (*mcp.CallToolResult, error) {
	u := slclient.BuildURL(journeyPlannerBase, "/v2/trips", params)
	body, errResult := fetchJSONRaw(ctx, client, u)
	if errResult != nil {
		return errResult, nil
	}
	out, err := injectVerboseResolved(body)
	if err != nil {
		return mcp.NewToolResultText(string(body)), nil
	}
	return mcp.NewToolResultText(string(out)), nil
}

// resolveTripSideParam enforces exactly-one-of (name, id) and returns the
// value to feed into the upstream's name_origin / name_destination field.
// Name is passed verbatim (upstream fuzzy-matches). IDs are normalized to
// the 16-digit GID form the upstream also accepts.
func resolveTripSideParam(side, name, idArg string) (string, *mcp.CallToolResult) {
	if name == "" && idArg == "" {
		return "", mcp.NewToolResultError(fmt.Sprintf("exactly one of %q or %q_id must be set", side, side))
	}
	if name != "" && idArg != "" {
		return "", mcp.NewToolResultError(fmt.Sprintf("%q and %q_id are mutually exclusive", side, side))
	}
	if idArg == "" {
		return name, nil
	}
	gid, err := normalizeToGID(idArg)
	if err != nil {
		var se *siteIDError
		if errors.As(err, &se) {
			return "", mcp.NewToolResultError(se.asJSON())
		}
		return "", mcp.NewToolResultError(err.Error())
	}
	return gid, nil
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
//   - if every ambiguous side resolves to exactly one stop candidate, silently
//     re-fetches /v2/trips with those IDs and returns the resulting body;
//   - if all resolved candidates on a side are non-stops (POIs, addresses,
//     localities), returns origin_not_a_stop / destination_not_a_stop so the
//     caller can't silently plan from the wrong place;
//   - otherwise, returns a structured error result naming only the side(s)
//     that still require user disambiguation.
//
// originByName / destByName indicate whether the side was supplied by name
// (and thus subject to the fuzzy-match drift guard) or by id.
func fetchTripsWithAmbiguityResolution(ctx context.Context, client slclient.HTTPDoer, params url.Values, origin, destination string, originByName, destByName bool) ([]byte, *mcp.CallToolResult) {
	u := slclient.BuildURL(journeyPlannerBase, "/v2/trips", params)
	body, errResult := fetchJSONRaw(ctx, client, u)
	if errResult != nil {
		return nil, errResult
	}

	// Defensive POI guard: even when upstream returned journeys, fuzzy name
	// matching may have quietly resolved the origin/destination onto a POI
	// or address (e.g. "Järfälla kyrka" → "Järfälla Hyrkart").
	if errResult := rejectPoiResolutionIfNeeded(body, originByName, destByName); errResult != nil {
		return nil, errResult
	}

	originAmb, destAmb := ambiguousByNameSide(body, originByName, destByName)
	if !originAmb && !destAmb {
		return body, nil
	}
	return resolveAmbiguity(ctx, client, params, origin, destination, originAmb, destAmb)
}

// ambiguousByNameSide returns the broker's ambiguity flags, with id-provided
// sides forced to false: the upstream shouldn't flag an id-provided side,
// but if it does we skip stop-finder rather than resolving a name we don't
// have.
func ambiguousByNameSide(body []byte, originByName, destByName bool) (originAmb, destAmb bool) {
	originAmb, destAmb = detectAmbiguity(body)
	if !originByName {
		originAmb = false
	}
	if !destByName {
		destAmb = false
	}
	return
}

// resolveAmbiguity handles the stop-finder-and-retry path once we know at
// least one side is ambiguous. It either returns the retried /v2/trips body
// on successful auto-resolve, a not-a-stop error for POI-only candidates,
// or a picker error listing the remaining stop candidates.
func resolveAmbiguity(ctx context.Context, client slclient.HTTPDoer, params url.Values, origin, destination string, originAmb, destAmb bool) ([]byte, *mcp.CallToolResult) {
	oc, dc := fetchCandidatesInParallel(ctx, client, origin, destination, originAmb, destAmb)
	ocStops, ocAll := splitStopCandidates(oc)
	dcStops, dcAll := splitStopCandidates(dc)

	if originAmb && len(ocStops) == 0 && len(ocAll) > 0 {
		return nil, buildNotAStopResult("origin_not_a_stop", origin, ocAll)
	}
	if destAmb && len(dcStops) == 0 && len(dcAll) > 0 {
		return nil, buildNotAStopResult("destination_not_a_stop", destination, dcAll)
	}

	originNeedsPicker := originAmb && len(ocStops) != 1
	destNeedsPicker := destAmb && len(dcStops) != 1
	if originNeedsPicker || destNeedsPicker {
		return nil, buildAmbiguityErrorResult(origin, destination, originNeedsPicker, destNeedsPicker, ocStops, dcStops)
	}

	if originAmb {
		params.Set("name_origin", ocStops[0].ID)
	}
	if destAmb {
		params.Set("name_destination", dcStops[0].ID)
	}
	u := slclient.BuildURL(journeyPlannerBase, "/v2/trips", params)
	body, errResult := fetchJSONRaw(ctx, client, u)
	if errResult != nil {
		return nil, errResult
	}
	return body, nil
}

// splitStopCandidates partitions the candidates into (stops, all) where
// `stops` is the subset whose upstream type is "stop". Empty or unknown
// types are treated as non-stops — trip endpoints must be transit stops.
func splitStopCandidates(cands []locationCandidate) (stops, all []locationCandidate) {
	all = cands
	stops = make([]locationCandidate, 0, len(cands))
	for _, c := range cands {
		if c.Type == "stop" {
			stops = append(stops, c)
		}
	}
	return stops, all
}

// notAStopResponse is the error shape for origin_not_a_stop /
// destination_not_a_stop: the planner couldn't match the query to a transit
// stop but did resolve it to one or more non-stop locations (POI/address/
// locality). The geocoded candidates are preserved so the caller can still
// recover — e.g. by asking the user to pick a nearby stop.
type notAStopResponse struct {
	Error      string              `json:"error"`
	Query      string              `json:"query"`
	Candidates []locationCandidate `json:"candidates"`
	Hint       string              `json:"hint,omitempty"`
}

func buildNotAStopResult(code, query string, cands []locationCandidate) *mcp.CallToolResult {
	body, err := json.Marshal(notAStopResponse{
		Error:      code,
		Query:      query,
		Candidates: cands,
		Hint:       "Resolved to a non-stop (POI/address/locality). Ask the user to pick a nearby transit stop, or use the resolve / stop_finder tool.",
	})
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("failed to encode not-a-stop response: %v", err))
	}
	return mcp.NewToolResultText(string(body))
}

// rejectPoiResolutionIfNeeded inspects the upstream /v2/trips journeys and,
// when either side was supplied by name, returns origin_not_a_stop /
// destination_not_a_stop if the resolved location isn't a transit stop.
// Returns nil for all-good / no-journey responses.
func rejectPoiResolutionIfNeeded(body []byte, originByName, destByName bool) *mcp.CallToolResult {
	if !originByName && !destByName {
		return nil
	}
	origin, dest, ok := firstLegEndpoints(body)
	if !ok {
		return nil
	}
	if originByName && !isStopResolution(origin) {
		return buildNotAStopResult("origin_not_a_stop", "", []locationCandidate{candidateFromStopEvent(origin)})
	}
	if destByName && !isStopResolution(dest) {
		return buildNotAStopResult("destination_not_a_stop", "", []locationCandidate{candidateFromStopEvent(dest)})
	}
	return nil
}

// firstLegEndpoints returns the (origin, destination) stop-event objects
// representing the planner's chosen trip endpoints: first leg's origin and
// last leg's destination from the first journey. Returns ok=false when the
// body isn't a journey response.
func firstLegEndpoints(body []byte) (origin, destination upstreamStopEvent, ok bool) {
	var root struct {
		Journeys []struct {
			Legs []struct {
				Origin      upstreamStopEvent `json:"origin"`
				Destination upstreamStopEvent `json:"destination"`
			} `json:"legs"`
		} `json:"journeys"`
	}
	if err := json.Unmarshal(body, &root); err != nil {
		return upstreamStopEvent{}, upstreamStopEvent{}, false
	}
	if len(root.Journeys) == 0 || len(root.Journeys[0].Legs) == 0 {
		return upstreamStopEvent{}, upstreamStopEvent{}, false
	}
	legs := root.Journeys[0].Legs
	return legs[0].Origin, legs[len(legs)-1].Destination, true
}

// stopEventType values upstream uses for "this is a transit stop"
// (platforms are children of a stop, so they count). Anything else —
// poi, address, locality, street — is rejected as non-stop.
func isStopResolution(e upstreamStopEvent) bool {
	if e.Parent != nil && e.Parent.Type == "stop" {
		return true
	}
	switch e.Type {
	case "stop", "platform":
		return true
	default:
		return false
	}
}

func candidateFromStopEvent(e upstreamStopEvent) locationCandidate {
	out := locationCandidate{
		Name: e.Name,
		ID:   e.ID,
		Type: e.Type,
	}
	if len(e.Coord) >= 2 {
		out.Coord = e.Coord
	}
	if e.Parent != nil {
		out.Locality = e.Parent.Name
	}
	return out
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
