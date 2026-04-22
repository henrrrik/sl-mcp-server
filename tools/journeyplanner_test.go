package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"
)

func TestSystemInfoTool(t *testing.T) {
	body := loadTestData(t, "system_info.json")
	mock := newMockDoer(body)

	_, handler := SystemInfoTool(mock)

	req := mcp.CallToolRequest{}
	req.Params.Arguments = map[string]any{}

	result, err := handler(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if mock.lastReq.URL.String() != "https://journeyplanner.integration.sl.se/v2/system-info" {
		t.Errorf("unexpected URL: %s", mock.lastReq.URL.String())
	}

	text := result.Content[0].(mcp.TextContent).Text
	if !strings.Contains(text, "validity") {
		t.Error("result should contain fixture data")
	}
}

func TestStopFinderTool(t *testing.T) {
	body := loadTestData(t, "stop_finder.json")
	mock := newMockDoer(body)

	_, handler := StopFinderTool(mock)

	req := mcp.CallToolRequest{}
	req.Params.Arguments = map[string]any{
		"name": "Slussen",
	}

	result, err := handler(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	q := mock.lastReq.URL.Query()
	if q.Get("name_sf") != "Slussen" {
		t.Errorf("expected name_sf=Slussen, got %q", q.Get("name_sf"))
	}
	if q.Get("type_sf") != "any" {
		t.Errorf("expected type_sf=any, got %q", q.Get("type_sf"))
	}
	if q.Get("any_obj_filter_sf") != "2" {
		t.Errorf("expected any_obj_filter_sf=2, got %q", q.Get("any_obj_filter_sf"))
	}

	text := result.Content[0].(mcp.TextContent).Text
	if !strings.Contains(text, "Slussen") {
		t.Error("result should contain fixture data")
	}
}

func TestStopFinderTool_MissingName(t *testing.T) {
	mock := newMockDoer("{}")

	_, handler := StopFinderTool(mock)

	req := mcp.CallToolRequest{}
	req.Params.Arguments = map[string]any{}

	result, err := handler(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !result.IsError {
		t.Error("expected error result when name is missing")
	}
}

func TestTripsTool(t *testing.T) {
	body := loadTestData(t, "trips.json")
	mock := newMockDoer(body)

	_, handler := TripsTool(mock)

	req := mcp.CallToolRequest{}
	req.Params.Arguments = map[string]any{
		"origin":          "Slussen",
		"destination":     "T-Centralen",
		"skip_deviations": true,
	}

	result, err := handler(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	q := mock.lastReq.URL.Query()
	if q.Get("name_origin") != "Slussen" {
		t.Errorf("expected name_origin=Slussen, got %q", q.Get("name_origin"))
	}
	if q.Get("name_destination") != "T-Centralen" {
		t.Errorf("expected name_destination=T-Centralen, got %q", q.Get("name_destination"))
	}
	if q.Get("type_origin") != "any" {
		t.Errorf("expected type_origin=any, got %q", q.Get("type_origin"))
	}
	if q.Get("calc_number_of_trips") != "3" {
		t.Errorf("expected calc_number_of_trips=3, got %q", q.Get("calc_number_of_trips"))
	}

	text := result.Content[0].(mcp.TextContent).Text
	if !strings.Contains(text, "Vällingby") {
		t.Error("result should contain fixture data")
	}
}

func TestTripsTool_ClampsNumberOfTrips(t *testing.T) {
	body := loadTestData(t, "trips.json")
	mock := newMockDoer(body)

	_, handler := TripsTool(mock)

	req := mcp.CallToolRequest{}
	req.Params.Arguments = map[string]any{
		"origin":          "Slussen",
		"destination":     "T-Centralen",
		"number_of_trips": float64(100),
		"skip_deviations": true,
	}

	_, err := handler(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	q := mock.lastReq.URL.Query()
	if q.Get("calc_number_of_trips") != "3" {
		t.Errorf("expected clamped to 3, got %q", q.Get("calc_number_of_trips"))
	}
}

func TestTripsTool_TimeParamDepart(t *testing.T) {
	body := loadTestData(t, "trips.json")
	mock := newMockDoer(body)

	_, handler := TripsTool(mock)

	req := mcp.CallToolRequest{}
	req.Params.Arguments = map[string]any{
		"origin":          "Slussen",
		"destination":     "T-Centralen",
		"time":            "2026-04-22T09:00:00+02:00",
		"skip_deviations": true,
	}

	_, err := handler(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	q := mock.lastReq.URL.Query()
	if q.Get("itd_date") != "20260422" {
		t.Errorf("expected itd_date=20260422, got %q", q.Get("itd_date"))
	}
	if q.Get("itd_time") != "0900" {
		t.Errorf("expected itd_time=0900, got %q", q.Get("itd_time"))
	}
	if q.Get("itd_trip_date_time_dep_arr") != "dep" {
		t.Errorf("expected itd_trip_date_time_dep_arr=dep (default), got %q", q.Get("itd_trip_date_time_dep_arr"))
	}
}

func TestTripsTool_TimeParamArrive(t *testing.T) {
	body := loadTestData(t, "trips.json")
	mock := newMockDoer(body)

	_, handler := TripsTool(mock)

	req := mcp.CallToolRequest{}
	req.Params.Arguments = map[string]any{
		"origin":          "Slussen",
		"destination":     "T-Centralen",
		"time":            "2026-04-22T09:00:00+02:00",
		"time_mode":       "arrive",
		"skip_deviations": true,
	}

	_, err := handler(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	q := mock.lastReq.URL.Query()
	if q.Get("itd_trip_date_time_dep_arr") != "arr" {
		t.Errorf("expected itd_trip_date_time_dep_arr=arr, got %q", q.Get("itd_trip_date_time_dep_arr"))
	}
}

func TestTripsTool_TimeConvertsToStockholmLocal(t *testing.T) {
	body := loadTestData(t, "trips.json")
	mock := newMockDoer(body)

	_, handler := TripsTool(mock)

	// 07:00 UTC = 09:00 CEST (Stockholm, April — DST active)
	req := mcp.CallToolRequest{}
	req.Params.Arguments = map[string]any{
		"origin":          "Slussen",
		"destination":     "T-Centralen",
		"time":            "2026-04-22T07:00:00Z",
		"skip_deviations": true,
	}

	_, err := handler(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	q := mock.lastReq.URL.Query()
	if q.Get("itd_date") != "20260422" {
		t.Errorf("expected itd_date=20260422, got %q", q.Get("itd_date"))
	}
	if q.Get("itd_time") != "0900" {
		t.Errorf("expected itd_time=0900 (Stockholm local), got %q", q.Get("itd_time"))
	}
}

func TestTripsTool_InvalidTime(t *testing.T) {
	mock := newMockDoer("{}")

	_, handler := TripsTool(mock)

	req := mcp.CallToolRequest{}
	req.Params.Arguments = map[string]any{
		"origin":      "Slussen",
		"destination": "T-Centralen",
		"time":        "not-a-timestamp",
	}

	result, err := handler(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !result.IsError {
		t.Error("expected error result for invalid time")
	}
}

func TestTripsTool_InvalidTimeMode(t *testing.T) {
	mock := newMockDoer("{}")

	_, handler := TripsTool(mock)

	req := mcp.CallToolRequest{}
	req.Params.Arguments = map[string]any{
		"origin":      "Slussen",
		"destination": "T-Centralen",
		"time":        "2026-04-22T09:00:00+02:00",
		"time_mode":   "whenever",
	}

	result, err := handler(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !result.IsError {
		t.Error("expected error result for invalid time_mode")
	}
}

func TestTripsTool_NoTimeParamsOmitted(t *testing.T) {
	body := loadTestData(t, "trips.json")
	mock := newMockDoer(body)

	_, handler := TripsTool(mock)

	req := mcp.CallToolRequest{}
	req.Params.Arguments = map[string]any{
		"origin":      "Slussen",
		"destination": "T-Centralen",
	}

	_, err := handler(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	q := mock.lastReq.URL.Query()
	if q.Get("itd_date") != "" || q.Get("itd_time") != "" || q.Get("itd_trip_date_time_dep_arr") != "" {
		t.Errorf("expected no itd_* params when time is omitted, got date=%q time=%q dep_arr=%q",
			q.Get("itd_date"), q.Get("itd_time"), q.Get("itd_trip_date_time_dep_arr"))
	}
}

// tripsResult is a test-only shape for asserting on the trimmed response.
type tripsResult struct {
	Journeys []struct {
		Duration     int    `json:"duration"`
		Interchanges int    `json:"interchanges"`
		Summary      string `json:"summary"`
		Departure    string `json:"departure"`
		Arrival      string `json:"arrival"`
		Legs         []struct {
			Mode      string `json:"mode"`
			Line      string `json:"line"`
			Direction string `json:"direction"`
			From      string `json:"from"`
			To        string `json:"to"`
			Departure string `json:"departure"`
			Arrival   string `json:"arrival"`
			Duration  int    `json:"duration"`
			Realtime  bool   `json:"realtime"`
		} `json:"legs"`
	} `json:"journeys"`
}

func callTrips(t *testing.T, extra map[string]any) string {
	t.Helper()
	body := loadTestData(t, "trips.json")
	mock := newMockDoer(body)
	_, handler := TripsTool(mock)

	args := map[string]any{
		"origin":      "Vällingby",
		"destination": "Stockholm City",
	}
	for k, v := range extra {
		args[k] = v
	}
	req := mcp.CallToolRequest{}
	req.Params.Arguments = args

	result, err := handler(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsError {
		t.Fatalf("unexpected error result: %s", result.Content[0].(mcp.TextContent).Text)
	}
	return result.Content[0].(mcp.TextContent).Text
}

func TestTripsTool_DefaultResponseTrimsVerboseFields(t *testing.T) {
	text := callTrips(t, map[string]any{})

	// These large fields should be dropped from the default response.
	for _, forbidden := range []string{"coords", "stopSequence", "footPathInfo", "pathDescriptions"} {
		if strings.Contains(text, forbidden) {
			t.Errorf("default response should not contain %q, but did", forbidden)
		}
	}

	// Real fixture is ~65 KB; trimmed target is well under 8 KB.
	if len(text) > 8*1024 {
		t.Errorf("default response should be < 8 KB, got %d bytes", len(text))
	}
}

func TestTripsTool_DefaultResponseHasSummaryFields(t *testing.T) {
	text := callTrips(t, map[string]any{})

	var out tripsResult
	if err := json.Unmarshal([]byte(text), &out); err != nil {
		t.Fatalf("failed to parse response: %v", err)
	}
	if len(out.Journeys) == 0 {
		t.Fatal("expected at least one journey")
	}
	j := out.Journeys[0]
	if j.Duration == 0 {
		t.Error("expected non-zero duration")
	}
	if j.Summary == "" {
		t.Error("expected summary field populated")
	}
	if j.Departure == "" {
		t.Error("expected departure field populated")
	}
	if j.Arrival == "" {
		t.Error("expected arrival field populated")
	}
}

func TestTripsTool_LegsAreFlattened(t *testing.T) {
	text := callTrips(t, map[string]any{})

	var out tripsResult
	if err := json.Unmarshal([]byte(text), &out); err != nil {
		t.Fatalf("failed to parse response: %v", err)
	}
	legs := out.Journeys[0].Legs

	// Fixture: Vällingby → Spånga station (bus 179) → Spånga (walk) → Stockholm City (Pendeltåg 43)
	if len(legs) != 3 {
		t.Fatalf("expected 3 legs, got %d", len(legs))
	}

	if legs[0].Mode != "bus" {
		t.Errorf("leg[0] mode: expected 'bus', got %q", legs[0].Mode)
	}
	if legs[0].Line != "179" {
		t.Errorf("leg[0] line: expected '179', got %q", legs[0].Line)
	}
	if legs[0].Direction == "" {
		t.Errorf("leg[0] direction: expected populated, got empty")
	}
	if legs[0].From == "" || legs[0].To == "" {
		t.Errorf("leg[0] from/to: expected populated, got from=%q to=%q", legs[0].From, legs[0].To)
	}

	if legs[1].Mode != "walk" {
		t.Errorf("leg[1] mode: expected 'walk', got %q", legs[1].Mode)
	}
	if legs[1].Line != "" {
		t.Errorf("leg[1] line: walking legs should have empty line, got %q", legs[1].Line)
	}

	if legs[2].Mode != "train" {
		t.Errorf("leg[2] mode: expected 'train', got %q", legs[2].Mode)
	}
	if legs[2].Line != "43" {
		t.Errorf("leg[2] line: expected '43', got %q", legs[2].Line)
	}
}

func TestTripsTool_SummaryExcludesWalkingLegs(t *testing.T) {
	text := callTrips(t, map[string]any{})

	var out tripsResult
	if err := json.Unmarshal([]byte(text), &out); err != nil {
		t.Fatalf("failed to parse response: %v", err)
	}
	summary := out.Journeys[0].Summary

	// "Buss 179 → Pendeltåg 43" — walking legs omitted
	if !strings.Contains(summary, "179") || !strings.Contains(summary, "43") {
		t.Errorf("summary should mention line numbers 179 and 43, got %q", summary)
	}
	if !strings.Contains(summary, "→") {
		t.Errorf("summary should join legs with ' → ', got %q", summary)
	}
}

func TestTripsTool_VerboseReturnsRaw(t *testing.T) {
	text := callTrips(t, map[string]any{"verbose": true})

	// Verbose should preserve the upstream shape, which has these fields.
	for _, expected := range []string{"coords", "stopSequence", "tripDuration", "tripRtDuration"} {
		if !strings.Contains(text, expected) {
			t.Errorf("verbose response should contain %q, but did not", expected)
		}
	}
}

// stopFinderResponseFor builds a minimal stop-finder response with N candidates.
func stopFinderResponse(names ...string) string {
	type loc struct {
		Coord            []float64         `json:"coord"`
		DisassembledName string            `json:"disassembledName"`
		ID               string            `json:"id"`
		MatchQuality     int               `json:"matchQuality"`
		Name             string            `json:"name"`
		Parent           map[string]string `json:"parent"`
		Type             string            `json:"type"`
	}
	type body struct {
		Locations []loc `json:"locations"`
	}
	b := body{}
	for i, n := range names {
		b.Locations = append(b.Locations, loc{
			Coord:            []float64{59.0 + float64(i)*0.01, 18.0 + float64(i)*0.01},
			DisassembledName: n,
			ID:               fmt.Sprintf("909100100000%04d", 9000+i),
			MatchQuality:     1000 - i*10,
			Name:             "Stockholm, " + n,
			Parent:           map[string]string{"name": "Vällingby", "type": "locality"},
			Type:             "stop",
		})
	}
	out, _ := json.Marshal(b)
	return string(out)
}

func TestTripsTool_AmbiguousOriginReturnsCandidates(t *testing.T) {
	tripsErr := `{"systemMessages":[{"type":"error","module":"BROKER","code":-8011,"text":"origin: multiple matches"}]}`

	mock := &routedMock{routes: []mockRoute{
		{pathContains: "/v2/trips", body: tripsErr},
		{pathContains: "/v2/stop-finder", queryMatches: map[string]string{"name_sf": "Tumultgränd"},
			body: stopFinderResponse("Tumultgränd", "Tumultgränd 35", "Tumultgränd (skola)")},
	}}

	_, handler := TripsTool(mock)
	req := mcp.CallToolRequest{}
	req.Params.Arguments = map[string]any{"origin": "Tumultgränd", "destination": "T-Centralen"}

	result, err := handler(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	text := result.Content[0].(mcp.TextContent).Text

	var out struct {
		Error      string `json:"error"`
		Query      string `json:"query"`
		Candidates []struct {
			Name     string `json:"name"`
			Locality string `json:"locality"`
			ID       string `json:"id"`
			Type     string `json:"type"`
		} `json:"candidates"`
	}
	if err := json.Unmarshal([]byte(text), &out); err != nil {
		t.Fatalf("failed to parse response: %v\npayload: %s", err, text)
	}
	if out.Error != "ambiguous_origin" {
		t.Errorf("expected error=ambiguous_origin, got %q", out.Error)
	}
	if out.Query != "Tumultgränd" {
		t.Errorf("expected query=Tumultgränd, got %q", out.Query)
	}
	if len(out.Candidates) == 0 {
		t.Fatalf("expected candidates, got none")
	}
	first := out.Candidates[0]
	if first.Name == "" || first.ID == "" {
		t.Errorf("expected populated name/id, got %+v", first)
	}
}

func TestTripsTool_AmbiguousDestinationReturnsCandidates(t *testing.T) {
	tripsErr := `{"systemMessages":[{"type":"error","module":"BROKER","code":-8011,"text":"destination: multiple matches"}]}`

	mock := &routedMock{routes: []mockRoute{
		{pathContains: "/v2/trips", body: tripsErr},
		{pathContains: "/v2/stop-finder", queryMatches: map[string]string{"name_sf": "Gamla"},
			body: stopFinderResponse("Gamla stan", "Gamla Enskede")},
	}}

	_, handler := TripsTool(mock)
	req := mcp.CallToolRequest{}
	req.Params.Arguments = map[string]any{"origin": "Slussen", "destination": "Gamla"}

	result, err := handler(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	text := result.Content[0].(mcp.TextContent).Text

	var out struct {
		Error      string            `json:"error"`
		Query      string            `json:"query"`
		Candidates []json.RawMessage `json:"candidates"`
	}
	if err := json.Unmarshal([]byte(text), &out); err != nil {
		t.Fatalf("failed to parse response: %v\npayload: %s", err, text)
	}
	if out.Error != "ambiguous_destination" {
		t.Errorf("expected error=ambiguous_destination, got %q", out.Error)
	}
	if out.Query != "Gamla" {
		t.Errorf("expected query=Gamla, got %q", out.Query)
	}
	if len(out.Candidates) == 0 {
		t.Error("expected candidates")
	}
}

func TestTripsTool_BothAmbiguousReturnsBothCandidates(t *testing.T) {
	tripsErr := `{"systemMessages":[
		{"type":"error","module":"BROKER","code":-8011,"text":"origin: multiple matches"},
		{"type":"error","module":"BROKER","code":-8010,"text":"destination: "}
	]}`

	mock := &routedMock{routes: []mockRoute{
		{pathContains: "/v2/trips", body: tripsErr},
		{pathContains: "/v2/stop-finder", queryMatches: map[string]string{"name_sf": "Tumultgränd"},
			body: stopFinderResponse("Tumultgränd", "Tumultgränd 35")},
		{pathContains: "/v2/stop-finder", queryMatches: map[string]string{"name_sf": "Centrum"},
			body: stopFinderResponse("Vällingby centrum", "Kista centrum", "Farsta centrum")},
	}}

	_, handler := TripsTool(mock)
	req := mcp.CallToolRequest{}
	req.Params.Arguments = map[string]any{"origin": "Tumultgränd", "destination": "Centrum"}

	result, err := handler(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	text := result.Content[0].(mcp.TextContent).Text

	var out struct {
		Error  string `json:"error"`
		Origin struct {
			Query      string            `json:"query"`
			Candidates []json.RawMessage `json:"candidates"`
		} `json:"origin"`
		Destination struct {
			Query      string            `json:"query"`
			Candidates []json.RawMessage `json:"candidates"`
		} `json:"destination"`
	}
	if err := json.Unmarshal([]byte(text), &out); err != nil {
		t.Fatalf("failed to parse response: %v\npayload: %s", err, text)
	}
	if out.Error != "ambiguous_both" {
		t.Errorf("expected error=ambiguous_both, got %q", out.Error)
	}
	if out.Origin.Query != "Tumultgränd" || len(out.Origin.Candidates) == 0 {
		t.Errorf("origin candidates missing or wrong query: %+v", out.Origin)
	}
	if out.Destination.Query != "Centrum" || len(out.Destination.Candidates) == 0 {
		t.Errorf("destination candidates missing or wrong query: %+v", out.Destination)
	}
}

func TestTripsTool_WarningWithJourneysDoesNotIntercept(t *testing.T) {
	// -8010 by itself (code for warning) with journeys present means the broker
	// made a best-effort match. Don't intercept — trim normally.
	body := loadTestData(t, "trips.json")
	// Inject a -8010 warning into the fixture via simple string substitution
	body = strings.Replace(body,
		`"systemMessages": [

  ]`,
		`"systemMessages": [
    {"type":"error","module":"BROKER","code":-8010,"text":"destination: "}
  ]`, 1)

	mock := &routedMock{routes: []mockRoute{
		{pathContains: "/v2/trips", body: body},
	}}

	_, handler := TripsTool(mock)
	req := mcp.CallToolRequest{}
	req.Params.Arguments = map[string]any{"origin": "Vällingby", "destination": "T-Centralen"}

	result, err := handler(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	text := result.Content[0].(mcp.TextContent).Text
	if strings.Contains(text, "ambiguous_") {
		t.Errorf("should not have intercepted — journeys were returned; got %.200s", text)
	}

	var out struct {
		Journeys []json.RawMessage `json:"journeys"`
	}
	if err := json.Unmarshal([]byte(text), &out); err != nil {
		t.Fatalf("expected trimmed-journeys shape, got parse error: %v", err)
	}
	if len(out.Journeys) == 0 {
		t.Error("expected journeys to be preserved")
	}
}

// deviationsBodyWithWindow builds a /v1/messages response with the given
// publish window applied to every deviation entry.
func deviationsBodyWithWindow(publishFrom, publishUpto string, specs ...[2]string) string {
	type dline struct {
		Designation   string `json:"designation"`
		ID            int    `json:"id"`
		TransportMode string `json:"transport_mode"`
	}
	type scope struct {
		Lines []dline `json:"lines"`
	}
	type variant struct {
		Header   string `json:"header"`
		Details  string `json:"details"`
		Language string `json:"language"`
	}
	type dev struct {
		DeviationCaseID int               `json:"deviation_case_id"`
		Scope           scope             `json:"scope"`
		Publish         map[string]string `json:"publish"`
		MessageVariants []variant         `json:"message_variants"`
	}
	var out []dev
	for i, s := range specs {
		out = append(out, dev{
			DeviationCaseID: 1000 + i,
			Scope: scope{
				Lines: []dline{{Designation: s[0], TransportMode: s[1]}},
			},
			Publish: map[string]string{
				"from": publishFrom,
				"upto": publishUpto,
			},
			MessageVariants: []variant{{
				Header:   fmt.Sprintf("Test deviation %d for %s %s", i, s[1], s[0]),
				Details:  "Details",
				Language: "sv",
			}},
		})
	}
	b, _ := json.Marshal(out)
	return string(b)
}

// deviationsBody builds a /v1/messages response with one deviation per
// (line, mode) pair passed in.
func deviationsBody(specs ...[2]string) string {
	type dline struct {
		Designation   string `json:"designation"`
		ID            int    `json:"id"`
		TransportMode string `json:"transport_mode"`
	}
	type scope struct {
		Lines []dline `json:"lines"`
	}
	type variant struct {
		Header   string `json:"header"`
		Details  string `json:"details"`
		Language string `json:"language"`
	}
	type dev struct {
		DeviationCaseID int               `json:"deviation_case_id"`
		Scope           scope             `json:"scope"`
		Publish         map[string]string `json:"publish"`
		MessageVariants []variant         `json:"message_variants"`
	}
	var out []dev
	for i, s := range specs {
		out = append(out, dev{
			DeviationCaseID: 1000 + i,
			Scope: scope{
				Lines: []dline{{Designation: s[0], TransportMode: s[1]}},
			},
			Publish: map[string]string{
				"from": "2026-04-20T00:00:00+02:00",
				"upto": "2026-05-01T00:00:00+02:00",
			},
			MessageVariants: []variant{{
				Header:   "Test deviation for " + s[1] + " " + s[0],
				Details:  "Details for " + s[0],
				Language: "sv",
			}},
		})
	}
	b, _ := json.Marshal(out)
	return string(b)
}

func TestTripsTool_DeviationsAttachedToMatchingLegs(t *testing.T) {
	tripsBody := loadTestData(t, "trips.json")
	// Fixture legs: bus 179, walk, train 43. Publish deviations for both.
	devs := deviationsBody([2]string{"179", "BUS"}, [2]string{"43", "TRAIN"})

	mock := &routedMock{routes: []mockRoute{
		{pathContains: "/v2/trips", body: tripsBody},
		{pathContains: "/v1/messages", body: devs},
	}}

	_, handler := TripsTool(mock)
	req := mcp.CallToolRequest{}
	req.Params.Arguments = map[string]any{"origin": "Vällingby", "destination": "Stockholm City"}

	result, err := handler(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	text := result.Content[0].(mcp.TextContent).Text

	var out struct {
		Journeys []struct {
			Legs []struct {
				Mode       string `json:"mode"`
				Line       string `json:"line"`
				Deviations []struct {
					Header string `json:"header"`
				} `json:"deviations"`
			} `json:"legs"`
		} `json:"journeys"`
	}
	if err := json.Unmarshal([]byte(text), &out); err != nil {
		t.Fatalf("failed to parse: %v\n%s", err, text)
	}
	legs := out.Journeys[0].Legs

	// Bus 179 leg: should have one matching deviation.
	if len(legs[0].Deviations) != 1 {
		t.Errorf("leg[0] (bus 179): expected 1 deviation, got %d", len(legs[0].Deviations))
	}
	// Walk leg: should have none.
	if len(legs[1].Deviations) != 0 {
		t.Errorf("leg[1] (walk): expected 0 deviations, got %d", len(legs[1].Deviations))
	}
	// Train 43 leg: should have one matching deviation.
	if len(legs[2].Deviations) != 1 {
		t.Errorf("leg[2] (train 43): expected 1 deviation, got %d", len(legs[2].Deviations))
	}
}

func TestTripsTool_DeviationsFilterByTimeWindow(t *testing.T) {
	tripsBody := loadTestData(t, "trips.json")
	// Fixture bus 179 leg departs 2026-04-21T21:03:00Z. Publish a deviation
	// for BUS 179 that's only active May 15-29 — must NOT attach.
	devs := deviationsBodyWithWindow("2026-05-15T00:00:00+02:00", "2026-05-29T23:59:00+02:00",
		[2]string{"179", "BUS"})

	mock := &routedMock{routes: []mockRoute{
		{pathContains: "/v2/trips", body: tripsBody},
		{pathContains: "/v1/messages", body: devs},
	}}

	_, handler := TripsTool(mock)
	req := mcp.CallToolRequest{}
	req.Params.Arguments = map[string]any{"origin": "Vällingby", "destination": "Stockholm City"}

	result, _ := handler(context.Background(), req)
	text := result.Content[0].(mcp.TextContent).Text

	var out struct {
		Journeys []struct {
			Legs []struct {
				Line              string            `json:"line"`
				Deviations        []json.RawMessage `json:"deviations"`
				HasMoreDeviations bool              `json:"has_more_deviations"`
			} `json:"legs"`
		} `json:"journeys"`
	}
	if err := json.Unmarshal([]byte(text), &out); err != nil {
		t.Fatalf("parse: %v\n%s", err, text)
	}
	for _, leg := range out.Journeys[0].Legs {
		if len(leg.Deviations) != 0 {
			t.Errorf("leg (line=%q) should have 0 deviations — publish window is future; got %d", leg.Line, len(leg.Deviations))
		}
	}
}

func TestTripsTool_DeviationsCappedAtThree(t *testing.T) {
	tripsBody := loadTestData(t, "trips.json")
	// 5 deviations all matching bus 179. Should cap at 3 with has_more_deviations=true.
	devs := deviationsBodyWithWindow("2026-04-01T00:00:00+02:00", "2026-05-01T00:00:00+02:00",
		[2]string{"179", "BUS"},
		[2]string{"179", "BUS"},
		[2]string{"179", "BUS"},
		[2]string{"179", "BUS"},
		[2]string{"179", "BUS"},
	)

	mock := &routedMock{routes: []mockRoute{
		{pathContains: "/v2/trips", body: tripsBody},
		{pathContains: "/v1/messages", body: devs},
	}}

	_, handler := TripsTool(mock)
	req := mcp.CallToolRequest{}
	req.Params.Arguments = map[string]any{"origin": "Vällingby", "destination": "Stockholm City"}

	result, _ := handler(context.Background(), req)
	text := result.Content[0].(mcp.TextContent).Text

	var out struct {
		Journeys []struct {
			Legs []struct {
				Line              string            `json:"line"`
				Mode              string            `json:"mode"`
				Deviations        []json.RawMessage `json:"deviations"`
				HasMoreDeviations bool              `json:"has_more_deviations"`
			} `json:"legs"`
		} `json:"journeys"`
	}
	if err := json.Unmarshal([]byte(text), &out); err != nil {
		t.Fatalf("parse: %v\n%s", err, text)
	}
	// Find the bus 179 leg
	for _, leg := range out.Journeys[0].Legs {
		if leg.Mode != "bus" || leg.Line != "179" {
			continue
		}
		if len(leg.Deviations) != 3 {
			t.Errorf("expected 3 deviations after cap, got %d", len(leg.Deviations))
		}
		if !leg.HasMoreDeviations {
			t.Errorf("expected has_more_deviations=true when truncated")
		}
	}
}

func TestTripsTool_DeviationsUnderCapNoFlag(t *testing.T) {
	tripsBody := loadTestData(t, "trips.json")
	// 2 deviations — under the cap, no flag expected.
	devs := deviationsBodyWithWindow("2026-04-01T00:00:00+02:00", "2026-05-01T00:00:00+02:00",
		[2]string{"179", "BUS"},
		[2]string{"179", "BUS"},
	)

	mock := &routedMock{routes: []mockRoute{
		{pathContains: "/v2/trips", body: tripsBody},
		{pathContains: "/v1/messages", body: devs},
	}}

	_, handler := TripsTool(mock)
	req := mcp.CallToolRequest{}
	req.Params.Arguments = map[string]any{"origin": "Vällingby", "destination": "Stockholm City"}

	result, _ := handler(context.Background(), req)
	text := result.Content[0].(mcp.TextContent).Text

	if strings.Contains(text, "has_more_deviations") {
		t.Errorf("has_more_deviations should be omitted when under cap")
	}
}

func TestTripsTool_DeviationsFilterByMode(t *testing.T) {
	tripsBody := loadTestData(t, "trips.json")
	// Publish deviation for BUS line 43 — must NOT attach to the train 43 leg.
	devs := deviationsBody([2]string{"43", "BUS"})

	mock := &routedMock{routes: []mockRoute{
		{pathContains: "/v2/trips", body: tripsBody},
		{pathContains: "/v1/messages", body: devs},
	}}

	_, handler := TripsTool(mock)
	req := mcp.CallToolRequest{}
	req.Params.Arguments = map[string]any{"origin": "Vällingby", "destination": "Stockholm City"}

	result, _ := handler(context.Background(), req)
	text := result.Content[0].(mcp.TextContent).Text

	var out struct {
		Journeys []struct {
			Legs []struct {
				Line       string `json:"line"`
				Deviations []any  `json:"deviations"`
			} `json:"legs"`
		} `json:"journeys"`
	}
	_ = json.Unmarshal([]byte(text), &out)
	for _, leg := range out.Journeys[0].Legs {
		if len(leg.Deviations) != 0 {
			t.Errorf("no leg should have deviations (only BUS 43 published, no bus 43 in trip), got line=%q with %d", leg.Line, len(leg.Deviations))
		}
	}
}

func TestTripsTool_SkipDeviationsParamAvoidsSecondCall(t *testing.T) {
	tripsBody := loadTestData(t, "trips.json")
	mock := &routedMock{routes: []mockRoute{
		{pathContains: "/v2/trips", body: tripsBody},
		{pathContains: "/v1/messages", body: "[]"},
	}}

	_, handler := TripsTool(mock)
	req := mcp.CallToolRequest{}
	req.Params.Arguments = map[string]any{
		"origin":          "Vällingby",
		"destination":     "Stockholm City",
		"skip_deviations": true,
	}

	_, err := handler(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	for _, c := range mock.calls {
		if strings.Contains(c.URL.Path, "/v1/messages") {
			t.Errorf("/v1/messages should not have been called with skip_deviations=true")
		}
	}
}

func TestTripsTool_DeviationsFetchFailureNonFatal(t *testing.T) {
	tripsBody := loadTestData(t, "trips.json")
	mock := &routedMock{routes: []mockRoute{
		{pathContains: "/v2/trips", body: tripsBody},
		{pathContains: "/v1/messages", body: "upstream error", status: 500},
	}}

	_, handler := TripsTool(mock)
	req := mcp.CallToolRequest{}
	req.Params.Arguments = map[string]any{"origin": "Vällingby", "destination": "Stockholm City"}

	result, err := handler(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsError {
		t.Fatalf("trips call should not fail because deviations fetch did; got %s", result.Content[0].(mcp.TextContent).Text)
	}
	text := result.Content[0].(mcp.TextContent).Text
	if !strings.Contains(text, "journeys") {
		t.Errorf("expected trimmed journeys to still be returned; got %q", text)
	}
}

func TestTripsTool_ErrorResponsePassedThroughWhenNoAmbiguityPattern(t *testing.T) {
	// An error shape with neither -8011 nor journeys passes through untouched.
	errBody := `{"systemMessages":[{"type":"error","module":"BROKER","code":-9999,"text":"something else"}]}`
	mock := &routedMock{routes: []mockRoute{
		{pathContains: "/v2/trips", body: errBody},
	}}

	_, handler := TripsTool(mock)
	req := mcp.CallToolRequest{}
	req.Params.Arguments = map[string]any{"origin": "x", "destination": "y"}

	result, err := handler(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	text := result.Content[0].(mcp.TextContent).Text
	if !strings.Contains(text, "something else") {
		t.Errorf("expected unknown-error body to pass through, got %q", text)
	}
}

func TestTripsTool_MissingOrigin(t *testing.T) {
	mock := newMockDoer("{}")

	_, handler := TripsTool(mock)

	req := mcp.CallToolRequest{}
	req.Params.Arguments = map[string]any{
		"destination": "T-Centralen",
	}

	result, err := handler(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !result.IsError {
		t.Error("expected error result when origin is missing")
	}
}
