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

func TestStopFinderTool_ReturnsFlatTrimmedArray(t *testing.T) {
	body := loadTestData(t, "stop_finder.json")
	mock := newMockDoer(body)

	_, handler := StopFinderTool(mock)

	req := mcp.CallToolRequest{}
	req.Params.Arguments = map[string]any{"name": "Slussen"}

	result, err := handler(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	text := result.Content[0].(mcp.TextContent).Text

	// Wrapper fields must be gone from the trimmed output.
	for _, k := range []string{"locations", "systemMessages"} {
		if strings.Contains(text, `"`+k+`"`) {
			t.Errorf("wrapper key %q should be trimmed, got %s", k, text)
		}
	}

	// Output is now a flat array, mirroring sites.
	var entries []map[string]any
	if err := json.Unmarshal([]byte(text), &entries); err != nil {
		t.Fatalf("expected flat array, got: %v\n%s", err, text)
	}
	if len(entries) != 3 {
		t.Fatalf("expected 3 entries from fixture, got %d", len(entries))
	}

	// Per-entry: only the six agreed fields survive.
	allowed := map[string]bool{"id": true, "name": true, "lat": true, "lon": true, "match_quality": true, "type": true}
	for _, e := range entries {
		for k := range e {
			if !allowed[k] {
				t.Errorf("unexpected field %q survived trim: %v", k, e)
			}
		}
	}

	// Noisy fields the user asked to drop must be absent.
	for _, k := range []string{"disassembledName", "isBest", "isGlobalId", "parent", "productClasses", "properties", "coord"} {
		if strings.Contains(text, `"`+k+`"`) {
			t.Errorf("field %q should be stripped, got %s", k, text)
		}
	}
}

func TestStopFinderTool_ExtractsLatLonFromCoord(t *testing.T) {
	body := loadTestData(t, "stop_finder.json")
	mock := newMockDoer(body)

	_, handler := StopFinderTool(mock)

	req := mcp.CallToolRequest{}
	req.Params.Arguments = map[string]any{"name": "Slussen"}

	result, _ := handler(context.Background(), req)
	text := result.Content[0].(mcp.TextContent).Text

	var entries []struct {
		ID  string  `json:"id"`
		Lat float64 `json:"lat"`
		Lon float64 `json:"lon"`
	}
	if err := json.Unmarshal([]byte(text), &entries); err != nil {
		t.Fatalf("parse: %v", err)
	}
	// Slussen fixture: coord [59.320316, 18.072451] → lat=59.320316, lon=18.072451.
	var found bool
	for _, e := range entries {
		if e.ID == "9091001000009192" {
			found = true
			if e.Lat != 59.320316 || e.Lon != 18.072451 {
				t.Errorf("expected Slussen lat=59.320316 lon=18.072451, got lat=%v lon=%v", e.Lat, e.Lon)
			}
		}
	}
	if !found {
		t.Fatal("Slussen entry missing")
	}
}

func TestStopFinderTool_PreservesGIDAndType(t *testing.T) {
	// id stays as the 16-digit GID string (departures accepts it via its
	// site_id normalizer), and non-stop entries (type="poi") aren't dropped.
	body := loadTestData(t, "stop_finder.json")
	mock := newMockDoer(body)

	_, handler := StopFinderTool(mock)

	req := mcp.CallToolRequest{}
	req.Params.Arguments = map[string]any{"name": "Slussen"}

	result, _ := handler(context.Background(), req)
	text := result.Content[0].(mcp.TextContent).Text

	var entries []struct {
		ID   string `json:"id"`
		Type string `json:"type"`
	}
	_ = json.Unmarshal([]byte(text), &entries)

	var gotGID, gotPOI bool
	for _, e := range entries {
		if e.ID == "9091001000009192" && e.Type == "stop" {
			gotGID = true
		}
		if e.Type == "poi" {
			gotPOI = true
		}
	}
	if !gotGID {
		t.Error("expected Slussen entry with 16-digit GID preserved and type=stop")
	}
	if !gotPOI {
		t.Error("expected POI entry kept (non-stop results shouldn't be filtered)")
	}
}

func TestStopFinderTool_OrdersByMatchQuality(t *testing.T) {
	// Upstream hands results in descending match_quality already; verify
	// the trim preserves that order (fixture: 1000, 850, 700).
	body := loadTestData(t, "stop_finder.json")
	mock := newMockDoer(body)

	_, handler := StopFinderTool(mock)

	req := mcp.CallToolRequest{}
	req.Params.Arguments = map[string]any{"name": "Slussen"}

	result, _ := handler(context.Background(), req)
	text := result.Content[0].(mcp.TextContent).Text

	var entries []struct {
		MatchQuality int `json:"match_quality"`
	}
	_ = json.Unmarshal([]byte(text), &entries)

	for i := 1; i < len(entries); i++ {
		if entries[i].MatchQuality > entries[i-1].MatchQuality {
			t.Errorf("match_quality order broken at index %d: %d > %d",
				i, entries[i].MatchQuality, entries[i-1].MatchQuality)
		}
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

// P1: resolve returns the best stop-typed match with all three id forms.
func TestResolveTool_ReturnsBestWithAllIDForms(t *testing.T) {
	body := loadTestData(t, "stop_finder.json")
	mock := newMockDoer(body)

	_, handler := ResolveTool(mock)

	req := mcp.CallToolRequest{}
	req.Params.Arguments = map[string]any{"query": "Slussen"}

	result, err := handler(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	q := mock.lastReq.URL.Query()
	if q.Get("name_sf") != "Slussen" {
		t.Errorf("expected name_sf=Slussen, got %q", q.Get("name_sf"))
	}

	text := result.Content[0].(mcp.TextContent).Text
	var out struct {
		Best *struct {
			Name         string    `json:"name"`
			ShortID      int       `json:"short_id"`
			GID180       string    `json:"gid_180"`
			GID16        string    `json:"gid_16"`
			Type         string    `json:"type"`
			MatchQuality int       `json:"match_quality"`
			Coord        []float64 `json:"coord"`
		} `json:"best"`
		Candidates []struct {
			Type string `json:"type"`
		} `json:"candidates"`
	}
	if err := json.Unmarshal([]byte(text), &out); err != nil {
		t.Fatalf("parse: %v\n%s", err, text)
	}
	if out.Best == nil {
		t.Fatal("expected best to be populated")
	}
	if out.Best.Type != "stop" {
		t.Errorf("best.type should be stop (POIs never win), got %q", out.Best.Type)
	}
	if out.Best.ShortID != 9192 {
		t.Errorf("expected short_id=9192 (Slussen), got %d", out.Best.ShortID)
	}
	if out.Best.GID16 != "9091001000009192" {
		t.Errorf("expected gid_16=9091001000009192, got %q", out.Best.GID16)
	}
	if out.Best.GID180 != "18009192" {
		t.Errorf("expected gid_180=18009192, got %q", out.Best.GID180)
	}
	if out.Best.MatchQuality != 1000 {
		t.Errorf("expected match_quality=1000, got %d", out.Best.MatchQuality)
	}

	// Default is stop_only=true — Slussplan (stop, 850) passes, the POI
	// "Slussen T-bana" is dropped.
	if len(out.Candidates) != 1 {
		t.Fatalf("expected 1 stop candidate with default stop_only=true, got %d", len(out.Candidates))
	}
	if out.Candidates[0].Type != "stop" {
		t.Errorf("default stop_only should drop POI candidates, got %+v", out.Candidates)
	}
}

// Round 2, Section 4: stop_only=false lets POI / address / locality entries
// appear in candidates for callers disambiguating free-form user input.
func TestResolveTool_StopOnlyFalseKeepsPOIs(t *testing.T) {
	body := loadTestData(t, "stop_finder.json")
	mock := newMockDoer(body)

	_, handler := ResolveTool(mock)

	req := mcp.CallToolRequest{}
	req.Params.Arguments = map[string]any{"query": "Slussen", "stop_only": false}

	result, _ := handler(context.Background(), req)
	text := result.Content[0].(mcp.TextContent).Text

	var out struct {
		Candidates []struct {
			Type string `json:"type"`
		} `json:"candidates"`
	}
	_ = json.Unmarshal([]byte(text), &out)
	var gotStop, gotPOI bool
	for _, c := range out.Candidates {
		switch c.Type {
		case "stop":
			gotStop = true
		case "poi":
			gotPOI = true
		}
	}
	if !gotStop || !gotPOI {
		t.Errorf("expected both stop and poi candidates with stop_only=false, got %+v", out.Candidates)
	}
}

// Round 2, Section 4: unambiguous=true when best scores ≥1000 and the
// next stop candidate is ≥50 points lower. Slussen fixture: Slussen=1000,
// Slussplan=850. Delta=150, well above threshold.
func TestResolveTool_UnambiguousClearWinner(t *testing.T) {
	body := loadTestData(t, "stop_finder.json")
	mock := newMockDoer(body)

	_, handler := ResolveTool(mock)
	req := mcp.CallToolRequest{}
	req.Params.Arguments = map[string]any{"query": "Slussen"}

	result, _ := handler(context.Background(), req)
	text := result.Content[0].(mcp.TextContent).Text

	var out struct {
		Best *struct {
			Unambiguous bool `json:"unambiguous"`
		} `json:"best"`
	}
	_ = json.Unmarshal([]byte(text), &out)
	if out.Best == nil {
		t.Fatal("expected best populated")
	}
	if !out.Best.Unambiguous {
		t.Errorf("expected unambiguous=true (1000 vs 850, delta=150)")
	}
}

// Round 2, Section 4: unambiguous=false when two stops are within 50
// points of each other — the caller still needs to pick.
func TestResolveTool_UnambiguousFalseForNearTie(t *testing.T) {
	body := `{"locations":[
		{"coord":[59.4,17.8],"disassembledName":"Jakobsberg","id":"9091001000009702","matchQuality":1000,"name":"Jakobsberg","parent":{"name":"Järfälla"},"properties":{"stopId":"18009702"},"type":"stop"},
		{"coord":[59.4,17.8],"disassembledName":"Jakobsbergs centrum","id":"9091001000009703","matchQuality":970,"name":"Jakobsbergs centrum","parent":{"name":"Järfälla"},"properties":{"stopId":"18009703"},"type":"stop"}
	]}`
	mock := newMockDoer(body)

	_, handler := ResolveTool(mock)
	req := mcp.CallToolRequest{}
	req.Params.Arguments = map[string]any{"query": "Jakobsberg"}

	result, _ := handler(context.Background(), req)
	text := result.Content[0].(mcp.TextContent).Text

	var out struct {
		Best *struct {
			Unambiguous bool `json:"unambiguous"`
		} `json:"best"`
	}
	_ = json.Unmarshal([]byte(text), &out)
	if out.Best == nil {
		t.Fatal("expected best populated")
	}
	if out.Best.Unambiguous {
		t.Errorf("expected unambiguous=false (1000 vs 970, delta=30 < 50)")
	}
}

// Round 2, Section 4: candidates is capped at 4 runners-up to bound the
// response size on heavily-shared names.
func TestResolveTool_CandidatesCappedAtFour(t *testing.T) {
	// 7 stops — best + 6 more. We should see best + 4 candidates, no more.
	body := `{"locations":[
		{"coord":[59.0,18.0],"disassembledName":"A","id":"9091001000001001","matchQuality":1000,"name":"A","parent":{"name":"X"},"type":"stop"},
		{"coord":[59.0,18.0],"disassembledName":"B","id":"9091001000001002","matchQuality":900,"name":"B","parent":{"name":"X"},"type":"stop"},
		{"coord":[59.0,18.0],"disassembledName":"C","id":"9091001000001003","matchQuality":800,"name":"C","parent":{"name":"X"},"type":"stop"},
		{"coord":[59.0,18.0],"disassembledName":"D","id":"9091001000001004","matchQuality":700,"name":"D","parent":{"name":"X"},"type":"stop"},
		{"coord":[59.0,18.0],"disassembledName":"E","id":"9091001000001005","matchQuality":600,"name":"E","parent":{"name":"X"},"type":"stop"},
		{"coord":[59.0,18.0],"disassembledName":"F","id":"9091001000001006","matchQuality":500,"name":"F","parent":{"name":"X"},"type":"stop"},
		{"coord":[59.0,18.0],"disassembledName":"G","id":"9091001000001007","matchQuality":400,"name":"G","parent":{"name":"X"},"type":"stop"}
	]}`
	mock := newMockDoer(body)

	_, handler := ResolveTool(mock)
	req := mcp.CallToolRequest{}
	req.Params.Arguments = map[string]any{"query": "X"}

	result, _ := handler(context.Background(), req)
	text := result.Content[0].(mcp.TextContent).Text

	var out struct {
		Best       any              `json:"best"`
		Candidates []map[string]any `json:"candidates"`
	}
	_ = json.Unmarshal([]byte(text), &out)
	if len(out.Candidates) != 4 {
		t.Errorf("expected candidates capped at 4, got %d", len(out.Candidates))
	}
}

func TestResolveTool_MissingQuery(t *testing.T) {
	mock := newMockDoer("{}")
	_, handler := ResolveTool(mock)

	req := mcp.CallToolRequest{}
	req.Params.Arguments = map[string]any{}

	result, err := handler(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Error("expected error result when query is missing")
	}
}

// Round 2, Section 4 acceptance: with default stop_only=true, an input
// that only matches POIs returns empty best AND empty candidates — no
// stop available and we refuse to suggest POIs for trip planning.
func TestResolveTool_OnlyPOIsDefaultsEmpty(t *testing.T) {
	body := `{"locations":[
		{"coord":[59.4,17.8],"disassembledName":"Järfälla Hyrkart","id":"poi:1","matchQuality":900,"name":"Järfälla Hyrkart","parent":{"name":"Järfälla"},"type":"poi"}
	]}`
	mock := newMockDoer(body)
	_, handler := ResolveTool(mock)

	req := mcp.CallToolRequest{}
	req.Params.Arguments = map[string]any{"query": "Järfälla Hyrkart"}

	result, _ := handler(context.Background(), req)
	text := result.Content[0].(mcp.TextContent).Text

	var out struct {
		Best       any   `json:"best"`
		Candidates []any `json:"candidates"`
	}
	if err := json.Unmarshal([]byte(text), &out); err != nil {
		t.Fatalf("parse: %v", err)
	}
	if out.Best != nil {
		t.Errorf("best should be absent when no stops match, got %+v", out.Best)
	}
	if len(out.Candidates) != 0 {
		t.Errorf("default stop_only=true should drop POIs from candidates, got %+v", out.Candidates)
	}
}

// Round 2, Section 4 acceptance: same POI-only body with stop_only=false
// preserves the POI as a candidate (and best is still nil — POIs never win).
func TestResolveTool_OnlyPOIsWithStopOnlyFalse(t *testing.T) {
	body := `{"locations":[
		{"coord":[59.4,17.8],"disassembledName":"Järfälla Hyrkart","id":"poi:1","matchQuality":900,"name":"Järfälla Hyrkart","parent":{"name":"Järfälla"},"type":"poi"}
	]}`
	mock := newMockDoer(body)
	_, handler := ResolveTool(mock)

	req := mcp.CallToolRequest{}
	req.Params.Arguments = map[string]any{"query": "Järfälla Hyrkart", "stop_only": false}

	result, _ := handler(context.Background(), req)
	text := result.Content[0].(mcp.TextContent).Text

	var out struct {
		Best       any `json:"best"`
		Candidates []struct {
			Type string `json:"type"`
		} `json:"candidates"`
	}
	_ = json.Unmarshal([]byte(text), &out)
	if out.Best != nil {
		t.Errorf("best should still be nil — POIs never win, got %+v", out.Best)
	}
	if len(out.Candidates) != 1 || out.Candidates[0].Type != "poi" {
		t.Errorf("expected POI candidate with stop_only=false, got %+v", out.Candidates)
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

// stopFinderResponseWithQualities builds a stop-finder response from explicit
// (name, matchQuality) pairs so tests can exercise the exact-match
// short-circuit thresholds directly.
func stopFinderResponseWithQualities(pairs ...stopFinderPair) string {
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
	for i, p := range pairs {
		b.Locations = append(b.Locations, loc{
			Coord:            []float64{59.0 + float64(i)*0.01, 18.0 + float64(i)*0.01},
			DisassembledName: p.Name,
			ID:               fmt.Sprintf("909100100000%04d", 9000+i),
			MatchQuality:     p.Quality,
			Name:             "Stockholm, " + p.Name,
			Parent:           map[string]string{"name": "Stockholm", "type": "locality"},
			Type:             "stop",
		})
	}
	out, _ := json.Marshal(b)
	return string(out)
}

type stopFinderPair struct {
	Name    string
	Quality int
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

func TestTripsTool_SingleCandidateAutoResolvesOrigin(t *testing.T) {
	// Origin is ambiguous per upstream, but stop-finder returns only ONE
	// candidate. The tool should silently use that candidate and retry
	// rather than returning an error picker.
	tripsErr := `{"systemMessages":[{"type":"error","module":"BROKER","code":-8011,"text":"origin: multiple matches"}]}`
	tripsOK := loadTestData(t, "trips.json")

	mock := &routedMock{routes: []mockRoute{
		// Ambiguous first call (no resolved ID in params)
		{pathContains: "/v2/trips", queryMatches: map[string]string{"name_origin": "Tumultgränd"}, body: tripsErr},
		// Retry with resolved id returns journeys
		{pathContains: "/v2/trips", body: tripsOK},
		{pathContains: "/v2/stop-finder", queryMatches: map[string]string{"name_sf": "Tumultgränd"},
			body: stopFinderResponse("Tumultgränd")},
		{pathContains: "/v1/messages", body: "[]"},
	}}

	_, handler := TripsTool(mock)
	req := mcp.CallToolRequest{}
	req.Params.Arguments = map[string]any{
		"origin":          "Tumultgränd",
		"destination":     "T-Centralen",
		"skip_deviations": true,
	}

	result, err := handler(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	text := result.Content[0].(mcp.TextContent).Text

	if strings.Contains(text, "ambiguous_") {
		t.Errorf("single-candidate resolution should not return an ambiguous_* error; got %.200s", text)
	}
	var out struct {
		Journeys []json.RawMessage `json:"journeys"`
	}
	if err := json.Unmarshal([]byte(text), &out); err != nil {
		t.Fatalf("expected trimmed-journeys shape after retry, got parse error: %v", err)
	}
	if len(out.Journeys) == 0 {
		t.Errorf("expected journeys after auto-resolve retry, got none")
	}

	// Verify the retried call used the resolved candidate ID
	retried := false
	for _, c := range mock.calls {
		if strings.Contains(c.URL.Path, "/v2/trips") && strings.HasPrefix(c.URL.Query().Get("name_origin"), "909100100000") {
			retried = true
		}
	}
	if !retried {
		t.Errorf("expected retry call to /v2/trips with a resolved global ID as name_origin")
	}
}

func TestTripsTool_SingleCandidateAutoResolvesBoth(t *testing.T) {
	tripsErr := `{"systemMessages":[
		{"type":"error","module":"BROKER","code":-8011,"text":"origin: multiple matches"},
		{"type":"error","module":"BROKER","code":-8010,"text":"destination: "}
	]}`
	tripsOK := loadTestData(t, "trips.json")

	mock := &routedMock{routes: []mockRoute{
		// First call (both sides by name) — ambiguous
		{pathContains: "/v2/trips", queryMatches: map[string]string{"name_origin": "Tumultgränd"}, body: tripsErr},
		// Retry — both resolved to IDs, returns journeys
		{pathContains: "/v2/trips", body: tripsOK},
		{pathContains: "/v2/stop-finder", queryMatches: map[string]string{"name_sf": "Tumultgränd"},
			body: stopFinderResponse("Tumultgränd")},
		{pathContains: "/v2/stop-finder", queryMatches: map[string]string{"name_sf": "T-Cen"},
			body: stopFinderResponse("T-Centralen")},
		{pathContains: "/v1/messages", body: "[]"},
	}}

	_, handler := TripsTool(mock)
	req := mcp.CallToolRequest{}
	req.Params.Arguments = map[string]any{
		"origin":          "Tumultgränd",
		"destination":     "T-Cen",
		"skip_deviations": true,
	}

	result, err := handler(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	text := result.Content[0].(mcp.TextContent).Text

	if strings.Contains(text, "ambiguous_") {
		t.Errorf("should have auto-resolved both sides; got %.200s", text)
	}
	var out struct {
		Journeys []json.RawMessage `json:"journeys"`
	}
	if err := json.Unmarshal([]byte(text), &out); err != nil {
		t.Fatalf("expected trimmed-journeys, got parse error: %v", err)
	}
	if len(out.Journeys) == 0 {
		t.Errorf("expected journeys after auto-resolve")
	}
}

func TestTripsTool_OriginMultiDestSingleOnlyOriginError(t *testing.T) {
	// Origin has multiple candidates, destination has exactly one.
	// Response should be ambiguous_origin alone — NOT ambiguous_both.
	tripsErr := `{"systemMessages":[
		{"type":"error","module":"BROKER","code":-8011,"text":"origin: multiple matches"},
		{"type":"error","module":"BROKER","code":-8010,"text":"destination: "}
	]}`

	mock := &routedMock{routes: []mockRoute{
		{pathContains: "/v2/trips", body: tripsErr},
		{pathContains: "/v2/stop-finder", queryMatches: map[string]string{"name_sf": "Tumultgränd"},
			body: stopFinderResponse("Tumultgränd", "Tumultgränd 35", "Tumultgränd (skola)")},
		{pathContains: "/v2/stop-finder", queryMatches: map[string]string{"name_sf": "T-Centralen"},
			body: stopFinderResponse("T-Centralen")},
	}}

	_, handler := TripsTool(mock)
	req := mcp.CallToolRequest{}
	req.Params.Arguments = map[string]any{"origin": "Tumultgränd", "destination": "T-Centralen"}

	result, _ := handler(context.Background(), req)
	text := result.Content[0].(mcp.TextContent).Text

	var out struct {
		Error      string            `json:"error"`
		Query      string            `json:"query"`
		Candidates []json.RawMessage `json:"candidates"`
	}
	if err := json.Unmarshal([]byte(text), &out); err != nil {
		t.Fatalf("parse: %v\n%s", err, text)
	}
	if out.Error != "ambiguous_origin" {
		t.Errorf("expected ambiguous_origin (destination was resolvable), got %q", out.Error)
	}
	if out.Query != "Tumultgränd" {
		t.Errorf("expected query=Tumultgränd, got %q", out.Query)
	}
	if len(out.Candidates) < 2 {
		t.Errorf("expected ≥2 candidates, got %d", len(out.Candidates))
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

func TestTripsTool_FiltersStaleBrokerNoiseFromSuccessResponse(t *testing.T) {
	// When ambiguity auto-resolves, the retry /v2/trips can still come back
	// with BROKER -8010/-8011 "origin:" / "destination:" entries left over
	// from the first-attempt name lookup. They're stale — we already got
	// journeys — and confuse callers. Filter them out, keep everything else.
	body := loadTestData(t, "trips.json")
	body = strings.Replace(body,
		"\"systemMessages\": [\n    \n  ]",
		`"systemMessages": [
    {"type":"error","module":"BROKER","code":-8010,"text":"origin: "},
    {"type":"error","module":"BROKER","code":-8011,"text":"destination: multiple matches"},
    {"type":"info","module":"SERVER","code":9001,"text":"keep-me"}
  ]`, 1)
	if !strings.Contains(body, "keep-me") {
		t.Fatal("test precondition: fixture systemMessages injection did not apply — update the Replace needle")
	}

	mock := &routedMock{routes: []mockRoute{{pathContains: "/v2/trips", body: body}}}

	_, handler := TripsTool(mock)
	req := mcp.CallToolRequest{}
	req.Params.Arguments = map[string]any{"origin": "Vällingby", "destination": "T-Centralen", "skip_deviations": true}

	result, err := handler(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	text := result.Content[0].(mcp.TextContent).Text

	if strings.Contains(text, "origin: ") || strings.Contains(text, "destination: multiple matches") {
		t.Errorf("stale BROKER resolution messages should be filtered, got: %.400s", text)
	}
	if !strings.Contains(text, "keep-me") {
		t.Error("non-matching system messages must pass through, but keep-me was dropped")
	}
}

func TestTripsTool_LegTimesLocalizeToStockholm(t *testing.T) {
	// Upstream emits times in UTC (Z suffix). Everything else in SL-land is
	// local time — rewrite to Europe/Stockholm with an explicit offset so
	// callers see the same clock the passenger sees on the platform.
	body := loadTestData(t, "trips.json")
	mock := newMockDoer(body)
	_, handler := TripsTool(mock)

	req := mcp.CallToolRequest{}
	req.Params.Arguments = map[string]any{
		"origin": "Vällingby", "destination": "T-Centralen", "skip_deviations": true,
	}
	result, err := handler(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	text := result.Content[0].(mcp.TextContent).Text

	// UTC 21:03 → Stockholm +02:00 = 23:03. The exact offset varies by DST,
	// but the suffix must carry one and the Z form must be gone.
	if strings.Contains(text, "Z\"") {
		t.Errorf("leg times should be localized, found UTC Z-suffix in: %.600s", text)
	}

	var out tripsResult
	if err := json.Unmarshal([]byte(text), &out); err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(out.Journeys) == 0 || len(out.Journeys[0].Legs) == 0 {
		t.Fatal("expected at least one journey with one leg")
	}
	dep := out.Journeys[0].Legs[0].Departure
	if !strings.Contains(dep, "+01:00") && !strings.Contains(dep, "+02:00") {
		t.Errorf("expected Stockholm offset (+01:00 or +02:00) in leg departure, got %q", dep)
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

// P0: origin_id / destination_id skip name resolution entirely and pass the
// canonical 16-digit GID straight to the upstream planner. Accepts short,
// 8-digit 18xx, 9-digit 3BA1CDEFG, and 16-digit GID input.
func TestTripsTool_OriginIDAndDestinationID(t *testing.T) {
	body := loadTestData(t, "trips.json")
	mock := &routedMock{routes: []mockRoute{
		{pathContains: "/v2/trips", body: body},
	}}

	_, handler := TripsTool(mock)
	req := mcp.CallToolRequest{}
	req.Params.Arguments = map[string]any{
		"origin_id":       "5868",
		"destination_id":  "9195",
		"skip_deviations": true,
	}

	result, err := handler(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsError {
		t.Fatalf("unexpected error result: %s", result.Content[0].(mcp.TextContent).Text)
	}

	// name_origin / name_destination must be the canonical GID so the planner
	// skips fuzzy name resolution entirely.
	tripsCalls := 0
	for _, c := range mock.calls {
		if strings.Contains(c.URL.Path, "/v2/trips") {
			tripsCalls++
			q := c.URL.Query()
			if got := q.Get("name_origin"); got != "9091001000005868" {
				t.Errorf("expected name_origin=9091001000005868 (canonical GID), got %q", got)
			}
			if got := q.Get("name_destination"); got != "9091001000009195" {
				t.Errorf("expected name_destination=9091001000009195, got %q", got)
			}
		}
		if strings.Contains(c.URL.Path, "/v2/stop-finder") {
			t.Errorf("stop-finder should not be called when IDs are provided")
		}
	}
	if tripsCalls != 1 {
		t.Errorf("expected 1 /v2/trips call, got %d", tripsCalls)
	}
}

func TestTripsTool_IDAcceptsAllFormats(t *testing.T) {
	cases := []struct {
		name    string
		input   any
		wantGID string
	}{
		{"short string", "9702", "9091001000009702"},
		{"short number", float64(9702), "9091001000009702"},
		{"8-digit 18xx", "18009702", "9091001000009702"},
		{"9-digit", "300109702", "9091001000009702"},
		{"16-digit GID", "9091001000009702", "9091001000009702"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			body := loadTestData(t, "trips.json")
			mock := &routedMock{routes: []mockRoute{
				{pathContains: "/v2/trips", body: body},
			}}
			_, handler := TripsTool(mock)
			req := mcp.CallToolRequest{}
			req.Params.Arguments = map[string]any{
				"origin_id":       tc.input,
				"destination_id":  "9192",
				"skip_deviations": true,
			}
			if _, err := handler(context.Background(), req); err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			var gotGID string
			for _, c := range mock.calls {
				if strings.Contains(c.URL.Path, "/v2/trips") {
					gotGID = c.URL.Query().Get("name_origin")
				}
			}
			if gotGID != tc.wantGID {
				t.Errorf("expected GID %q, got %q", tc.wantGID, gotGID)
			}
		})
	}
}

func TestTripsTool_NameAndIDMutuallyExclusive(t *testing.T) {
	mock := newMockDoer("{}")
	_, handler := TripsTool(mock)

	req := mcp.CallToolRequest{}
	req.Params.Arguments = map[string]any{
		"origin":         "Slussen",
		"origin_id":      "9192",
		"destination_id": "9001",
	}
	result, err := handler(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Error("expected error when both origin and origin_id are set")
	}
}

func TestTripsTool_MissingBothOriginForms(t *testing.T) {
	mock := newMockDoer("{}")
	_, handler := TripsTool(mock)

	req := mcp.CallToolRequest{}
	req.Params.Arguments = map[string]any{
		"destination_id": "9001",
	}
	result, err := handler(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Error("expected error when neither origin nor origin_id is set")
	}
}

func TestTripsTool_InvalidOriginIDReturnsStructuredError(t *testing.T) {
	mock := newMockDoer("{}")
	_, handler := TripsTool(mock)

	req := mcp.CallToolRequest{}
	req.Params.Arguments = map[string]any{
		"origin_id":      "not-an-id",
		"destination_id": "9001",
	}
	result, err := handler(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Fatal("expected error result")
	}
	text := result.Content[0].(mcp.TextContent).Text
	if !strings.Contains(text, `"error":"invalid_site_id_format"`) {
		t.Errorf("expected structured siteID error, got %s", text)
	}
}

// P0: when the name-resolution step resolves ambiguously to non-stop types
// only (POIs/addresses), return origin_not_a_stop rather than silently
// picking a candidate.
func TestTripsTool_NameResolvesToOnlyPOIsReturnsNotAStop(t *testing.T) {
	tripsErr := `{"systemMessages":[{"type":"error","module":"BROKER","code":-8011,"text":"origin: multiple matches"}]}`

	// Stop-finder returns only POI-typed results for "Järfälla kyrka".
	poiOnly := `{"locations":[
		{"coord":[59.4,17.8],"disassembledName":"Järfälla kyrka","id":"poi:1","matchQuality":900,"name":"Järfälla kyrka","parent":{"name":"Järfälla","type":"locality"},"type":"poi"},
		{"coord":[59.4,17.8],"disassembledName":"Järfälla Hyrkart","id":"poi:2","matchQuality":800,"name":"Järfälla Hyrkart","parent":{"name":"Järfälla","type":"locality"},"type":"poi"}
	]}`

	mock := &routedMock{routes: []mockRoute{
		{pathContains: "/v2/trips", body: tripsErr},
		{pathContains: "/v2/stop-finder", body: poiOnly},
	}}

	_, handler := TripsTool(mock)
	req := mcp.CallToolRequest{}
	req.Params.Arguments = map[string]any{"origin": "Järfälla kyrka", "destination": "T-Centralen"}

	result, err := handler(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	text := result.Content[0].(mcp.TextContent).Text

	var out struct {
		Error      string              `json:"error"`
		Candidates []locationCandidate `json:"candidates"`
	}
	if err := json.Unmarshal([]byte(text), &out); err != nil {
		t.Fatalf("parse: %v\n%s", err, text)
	}
	if out.Error != "origin_not_a_stop" {
		t.Errorf("expected origin_not_a_stop, got %q\n%s", out.Error, text)
	}
	if len(out.Candidates) == 0 {
		t.Errorf("expected POI candidates preserved in payload")
	}
}

// P0: when stop-finder returns a mix of stops and POIs, only stop-typed
// candidates are considered for ambiguity auto-resolution. A single stop
// among several POIs should auto-resolve, not error.
func TestTripsTool_MixedStopsAndPOIsPicksTheStop(t *testing.T) {
	tripsErr := `{"systemMessages":[{"type":"error","module":"BROKER","code":-8011,"text":"origin: multiple matches"}]}`
	tripsOK := loadTestData(t, "trips.json")

	mixed := `{"locations":[
		{"coord":[59.4,17.8],"disassembledName":"Sluss Cafe","id":"poi:1","matchQuality":900,"name":"Sluss Cafe","parent":{"name":"Stockholm","type":"locality"},"type":"poi"},
		{"coord":[59.3,18.07],"disassembledName":"Slussen","id":"9091001000009192","matchQuality":850,"name":"Slussen","parent":{"name":"Stockholm","type":"locality"},"type":"stop"}
	]}`

	mock := &routedMock{routes: []mockRoute{
		{pathContains: "/v2/trips", queryMatches: map[string]string{"name_origin": "Sluss"}, body: tripsErr},
		{pathContains: "/v2/trips", body: tripsOK},
		{pathContains: "/v2/stop-finder", body: mixed},
		{pathContains: "/v1/messages", body: "[]"},
	}}

	_, handler := TripsTool(mock)
	req := mcp.CallToolRequest{}
	req.Params.Arguments = map[string]any{"origin": "Sluss", "destination": "T-Centralen", "skip_deviations": true}

	result, _ := handler(context.Background(), req)
	text := result.Content[0].(mcp.TextContent).Text

	if strings.Contains(text, "ambiguous_") || strings.Contains(text, "_not_a_stop") {
		t.Errorf("single-stop among POIs should auto-resolve; got %.300s", text)
	}

	// The retry must have used the stop's GID as name_origin.
	var retried bool
	for _, c := range mock.calls {
		if strings.Contains(c.URL.Path, "/v2/trips") && c.URL.Query().Get("name_origin") == "9091001000009192" {
			retried = true
		}
	}
	if !retried {
		t.Errorf("expected retry with the stop GID as name_origin")
	}
}

// P0: every successful trips response includes a top-level resolved block
// with origin and destination {name, id, site_id, coord, type}. Callers can
// use this to detect silent drift.
func TestTripsTool_SuccessfulResponseIncludesResolvedBlock(t *testing.T) {
	body := loadTestData(t, "trips.json")
	mock := newMockDoer(body)
	_, handler := TripsTool(mock)

	req := mcp.CallToolRequest{}
	req.Params.Arguments = map[string]any{
		"origin": "Vällingby", "destination": "Stockholm City",
		"skip_deviations": true,
	}

	result, err := handler(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	text := result.Content[0].(mcp.TextContent).Text

	var out struct {
		Resolved *struct {
			Origin struct {
				Name   string    `json:"name"`
				ID     string    `json:"id"`
				SiteID int       `json:"site_id"`
				Type   string    `json:"type"`
				Coord  []float64 `json:"coord"`
			} `json:"origin"`
			Destination struct {
				Name   string `json:"name"`
				ID     string `json:"id"`
				SiteID int    `json:"site_id"`
			} `json:"destination"`
		} `json:"resolved"`
	}
	if err := json.Unmarshal([]byte(text), &out); err != nil {
		t.Fatalf("parse: %v\n%s", err, text)
	}
	if out.Resolved == nil {
		t.Fatal("expected resolved block in trips response")
	}
	if out.Resolved.Origin.Name == "" {
		t.Errorf("expected resolved.origin.name, got empty")
	}
	if out.Resolved.Origin.ID == "" {
		t.Errorf("expected resolved.origin.id (16-digit GID), got empty")
	}
	// Fixture: origin.parent.properties.stopId = "18012301" → site_id 12301
	// Actually upstream's parent.id "9021001012301000" normalizes via 9-digit
	// rule but that's a 16-digit number — trafiklab's normalizer takes the
	// last 4..5 digits. The more reliable path is parent.properties.stopId
	// (18012301 → 12301, out of range) — so we may end up with 0. Don't
	// require site_id to be set in the fixture case, but if set it must be
	// positive.
	if out.Resolved.Origin.SiteID < 0 {
		t.Errorf("site_id should never be negative, got %d", out.Resolved.Origin.SiteID)
	}
	if len(out.Resolved.Origin.Coord) != 2 {
		t.Errorf("expected resolved.origin.coord [lat, lon], got %v", out.Resolved.Origin.Coord)
	}
	if out.Resolved.Destination.Name == "" {
		t.Errorf("expected resolved.destination.name, got empty")
	}
}

// P0: when upstream silently plans from a POI (no ambiguity error), the
// defensive guard still rejects it rather than pretending the trip was OK.
// This is the "Järfälla kyrka → Järfälla Hyrkart" scenario.
func TestTripsTool_SilentPOIResolutionRejected(t *testing.T) {
	// Hand-crafted journey with a POI origin and a stop destination.
	body := `{
		"journeys": [{
			"tripDuration": 600,
			"interchanges": 0,
			"legs": [{
				"duration": 600,
				"origin": {
					"id": "poi:hyrkart",
					"name": "Järfälla Hyrkart",
					"type": "poi",
					"coord": [59.4, 17.85],
					"departureTimePlanned": "2026-04-22T09:00:00Z"
				},
				"destination": {
					"id": "9025001000012559",
					"name": "Slussen",
					"type": "platform",
					"coord": [59.32, 18.07],
					"parent": {"id": "9021001000009192", "name": "Slussen", "type": "stop", "properties": {"stopId": "18009192"}},
					"arrivalTimePlanned": "2026-04-22T09:10:00Z"
				},
				"transportation": {"disassembledName": "54", "product": {"name": "Buss"}}
			}]
		}]
	}`

	mock := &routedMock{routes: []mockRoute{
		{pathContains: "/v2/trips", body: body},
	}}

	_, handler := TripsTool(mock)
	req := mcp.CallToolRequest{}
	req.Params.Arguments = map[string]any{
		"origin":          "Järfälla kyrka",
		"destination":     "Slussen",
		"skip_deviations": true,
	}

	result, err := handler(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	text := result.Content[0].(mcp.TextContent).Text

	var out struct {
		Error string `json:"error"`
	}
	if err := json.Unmarshal([]byte(text), &out); err != nil {
		t.Fatalf("parse: %v\n%s", err, text)
	}
	if out.Error != "origin_not_a_stop" {
		t.Errorf("expected origin_not_a_stop, got %q — planner silently used a POI!\n%s", out.Error, text)
	}
}

// P0: when origin_id was provided, don't run the POI guard on origin — the
// caller explicitly chose that location. Same for destination.
func TestTripsTool_PoiGuardSkippedWhenIDProvided(t *testing.T) {
	// Even if the upstream claimed a poi-typed origin (unlikely with a real
	// GID), we trust the caller since they passed an explicit ID.
	body := `{
		"journeys": [{
			"tripDuration": 600,
			"interchanges": 0,
			"legs": [{
				"duration": 600,
				"origin": {"id": "9091001000009192", "name": "Some POI", "type": "poi", "coord": [59.3, 18.07]},
				"destination": {"id": "9091001000009001", "name": "T-Centralen", "type": "platform", "coord": [59.33, 18.06], "parent": {"id": "9021001000009001", "name": "T-Centralen", "type": "stop"}},
				"transportation": {"disassembledName": "54", "product": {"name": "Buss"}}
			}]
		}]
	}`

	mock := &routedMock{routes: []mockRoute{
		{pathContains: "/v2/trips", body: body},
	}}

	_, handler := TripsTool(mock)
	req := mcp.CallToolRequest{}
	req.Params.Arguments = map[string]any{
		"origin_id":       "9192",
		"destination_id":  "9001",
		"skip_deviations": true,
	}

	result, err := handler(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	text := result.Content[0].(mcp.TextContent).Text

	if strings.Contains(text, "_not_a_stop") {
		t.Errorf("when origin_id was provided, POI guard should not trigger; got %.300s", text)
	}
}

// P1: when stop-finder returns one exact match (quality 1000) and the
// next-best is meaningfully lower, auto-pick the exact match instead of
// erroring as ambiguous. Preserve the shadowed list in a warning.
func TestTripsTool_ExactMatchShadowsShorterSuffix(t *testing.T) {
	tripsErr := `{"systemMessages":[{"type":"error","module":"BROKER","code":-8011,"text":"origin: multiple matches"}]}`
	tripsOK := loadTestData(t, "trips.json")

	// Solna station (exact 1000) vs Solna station norra (850) + Ulriksdals station (780).
	finder := stopFinderResponseWithQualities(
		stopFinderPair{Name: "Solna station", Quality: 1000},
		stopFinderPair{Name: "Solna station norra", Quality: 850},
		stopFinderPair{Name: "Ulriksdals station", Quality: 780},
	)

	mock := &routedMock{routes: []mockRoute{
		{pathContains: "/v2/trips", queryMatches: map[string]string{"name_origin": "Solna station"}, body: tripsErr},
		{pathContains: "/v2/trips", body: tripsOK},
		{pathContains: "/v2/stop-finder", body: finder},
		{pathContains: "/v1/messages", body: "[]"},
	}}

	_, handler := TripsTool(mock)
	req := mcp.CallToolRequest{}
	req.Params.Arguments = map[string]any{
		"origin": "Solna station", "destination": "T-Centralen", "skip_deviations": true,
	}

	result, err := handler(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	text := result.Content[0].(mcp.TextContent).Text

	if strings.Contains(text, "ambiguous_") {
		t.Errorf("exact match should auto-resolve, not error; got %.300s", text)
	}

	var out struct {
		Journeys []json.RawMessage `json:"journeys"`
		Warnings []struct {
			Code     string              `json:"code"`
			Side     string              `json:"side"`
			Query    string              `json:"query"`
			Picked   *locationCandidate  `json:"picked"`
			Shadowed []locationCandidate `json:"shadowed"`
		} `json:"warnings"`
	}
	if err := json.Unmarshal([]byte(text), &out); err != nil {
		t.Fatalf("parse: %v\n%s", err, text)
	}
	if len(out.Journeys) == 0 {
		t.Fatal("expected journeys from auto-resolve retry")
	}
	if len(out.Warnings) != 1 {
		t.Fatalf("expected 1 warning, got %d: %+v", len(out.Warnings), out.Warnings)
	}
	w := out.Warnings[0]
	if w.Code != "exact_match_shadowed" {
		t.Errorf("expected code=exact_match_shadowed, got %q", w.Code)
	}
	if w.Side != "origin" {
		t.Errorf("expected side=origin, got %q", w.Side)
	}
	if w.Picked == nil || w.Picked.Name != "Solna station" {
		t.Errorf("expected picked=Solna station, got %+v", w.Picked)
	}
	if len(w.Shadowed) != 2 {
		t.Errorf("expected 2 shadowed candidates, got %d", len(w.Shadowed))
	}
}

// P1: when the gap between exact (1000) and next-best (950) is less than
// the required delta of 100, the match isn't "clear enough" — fall back to
// the ambiguity picker.
func TestTripsTool_ExactMatchNarrowGapStillAmbiguous(t *testing.T) {
	tripsErr := `{"systemMessages":[{"type":"error","module":"BROKER","code":-8011,"text":"origin: multiple matches"}]}`

	finder := stopFinderResponseWithQualities(
		stopFinderPair{Name: "Jakobsberg", Quality: 1000},
		stopFinderPair{Name: "Jakobsbergs centrum", Quality: 950},
	)

	mock := &routedMock{routes: []mockRoute{
		{pathContains: "/v2/trips", body: tripsErr},
		{pathContains: "/v2/stop-finder", body: finder},
	}}

	_, handler := TripsTool(mock)
	req := mcp.CallToolRequest{}
	req.Params.Arguments = map[string]any{
		"origin": "Jakobsberg", "destination": "Slussen",
	}

	result, _ := handler(context.Background(), req)
	text := result.Content[0].(mcp.TextContent).Text

	var out struct {
		Error string `json:"error"`
	}
	_ = json.Unmarshal([]byte(text), &out)
	if out.Error != "ambiguous_origin" {
		t.Errorf("expected ambiguous_origin when top two are close (delta=50 < 100); got %q", out.Error)
	}
}

// P1: two candidates both scoring 1000 (genuinely tied) still error — the
// short-circuit requires a unique winner.
func TestTripsTool_TiedTopScoresStillAmbiguous(t *testing.T) {
	tripsErr := `{"systemMessages":[{"type":"error","module":"BROKER","code":-8011,"text":"origin: multiple matches"}]}`

	finder := stopFinderResponseWithQualities(
		stopFinderPair{Name: "Centrum A", Quality: 1000},
		stopFinderPair{Name: "Centrum B", Quality: 1000},
	)

	mock := &routedMock{routes: []mockRoute{
		{pathContains: "/v2/trips", body: tripsErr},
		{pathContains: "/v2/stop-finder", body: finder},
	}}

	_, handler := TripsTool(mock)
	req := mcp.CallToolRequest{}
	req.Params.Arguments = map[string]any{
		"origin": "Centrum", "destination": "Slussen",
	}

	result, _ := handler(context.Background(), req)
	text := result.Content[0].(mcp.TextContent).Text

	var out struct {
		Error string `json:"error"`
	}
	_ = json.Unmarshal([]byte(text), &out)
	if out.Error != "ambiguous_origin" {
		t.Errorf("tied top scores should still error as ambiguous; got %q", out.Error)
	}
}

// P1 helper: pickExactMatch unit tests
func TestPickExactMatch(t *testing.T) {
	cases := []struct {
		name    string
		cands   []locationCandidate
		wantOK  bool
		wantIdx int // index of winner in the input; only meaningful when wantOK
	}{
		{
			name:   "empty list",
			cands:  nil,
			wantOK: false,
		},
		{
			name:   "single candidate",
			cands:  []locationCandidate{{Name: "a", MatchQuality: 1000}},
			wantOK: false, // single candidate handled earlier in the pipeline
		},
		{
			name: "clear winner",
			cands: []locationCandidate{
				{Name: "exact", MatchQuality: 1000},
				{Name: "shadow", MatchQuality: 850},
			},
			wantOK:  true,
			wantIdx: 0,
		},
		{
			name: "winner not first position",
			cands: []locationCandidate{
				{Name: "shadow", MatchQuality: 850},
				{Name: "exact", MatchQuality: 1000},
			},
			wantOK:  true,
			wantIdx: 1,
		},
		{
			name: "narrow gap",
			cands: []locationCandidate{
				{Name: "a", MatchQuality: 1000},
				{Name: "b", MatchQuality: 950},
			},
			wantOK: false,
		},
		{
			name: "tied at 1000",
			cands: []locationCandidate{
				{Name: "a", MatchQuality: 1000},
				{Name: "b", MatchQuality: 1000},
			},
			wantOK: false,
		},
		{
			name: "best below 1000",
			cands: []locationCandidate{
				{Name: "a", MatchQuality: 950},
				{Name: "b", MatchQuality: 800},
			},
			wantOK: false,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			picked, shadowed, ok := pickExactMatch(tc.cands)
			if ok != tc.wantOK {
				t.Fatalf("ok=%v, want %v", ok, tc.wantOK)
			}
			if !ok {
				return
			}
			if picked.Name != tc.cands[tc.wantIdx].Name {
				t.Errorf("picked=%q, want %q", picked.Name, tc.cands[tc.wantIdx].Name)
			}
			if len(shadowed) != len(tc.cands)-1 {
				t.Errorf("shadowed=%d, want %d", len(shadowed), len(tc.cands)-1)
			}
		})
	}
}

// P0: verbose=true still gets a resolved block injected at the top level.
func TestTripsTool_VerboseIncludesResolvedBlock(t *testing.T) {
	body := loadTestData(t, "trips.json")
	mock := newMockDoer(body)
	_, handler := TripsTool(mock)

	req := mcp.CallToolRequest{}
	req.Params.Arguments = map[string]any{
		"origin": "Vällingby", "destination": "Stockholm City",
		"verbose": true,
	}

	result, err := handler(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	text := result.Content[0].(mcp.TextContent).Text

	if !strings.Contains(text, `"resolved"`) {
		t.Errorf("verbose response should still include a resolved block, got %.400s", text)
	}
	// Verbose must still preserve the upstream-only fields.
	for _, expected := range []string{"coords", "stopSequence"} {
		if !strings.Contains(text, expected) {
			t.Errorf("verbose mode dropped upstream field %q", expected)
		}
	}
}
