package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"
)

func TestSitesTool(t *testing.T) {
	body := loadTestData(t, "sites.json")
	mock := newMockDoer(body)

	_, handler := SitesTool(mock)

	req := mcp.CallToolRequest{}
	req.Params.Arguments = map[string]any{}

	result, err := handler(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if mock.lastReq.URL.String() != "https://transport.integration.sl.se/v1/sites" {
		t.Errorf("unexpected URL: %s", mock.lastReq.URL.String())
	}

	text := result.Content[0].(mcp.TextContent).Text
	if !strings.Contains(text, "Slussen") {
		t.Error("result should contain fixture data")
	}
}

func TestSitesTool_QueryFiltersByNameSubstring(t *testing.T) {
	body := loadTestData(t, "sites.json")
	mock := newMockDoer(body)

	_, handler := SitesTool(mock)

	req := mcp.CallToolRequest{}
	req.Params.Arguments = map[string]any{"query": "sluss"} // lowercase — match should be case-insensitive

	result, err := handler(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	text := result.Content[0].(mcp.TextContent).Text

	var sites []map[string]any
	if err := json.Unmarshal([]byte(text), &sites); err != nil {
		t.Fatalf("failed to parse response: %v", err)
	}

	// Fixture has "Slussen" and "Slussplan" matching "sluss" (case-insensitive).
	if len(sites) != 2 {
		t.Errorf("expected 2 matching sites, got %d: %v", len(sites), sites)
	}
	names := map[string]bool{}
	for _, s := range sites {
		names[s["name"].(string)] = true
	}
	if !names["Slussen"] || !names["Slussplan"] {
		t.Errorf("expected Slussen and Slussplan in results, got %v", names)
	}
}

func TestSitesTool_LimitTruncates(t *testing.T) {
	body := loadTestData(t, "sites.json")
	mock := newMockDoer(body)

	_, handler := SitesTool(mock)

	req := mcp.CallToolRequest{}
	req.Params.Arguments = map[string]any{"limit": float64(2)}

	result, err := handler(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	text := result.Content[0].(mcp.TextContent).Text

	var sites []map[string]any
	if err := json.Unmarshal([]byte(text), &sites); err != nil {
		t.Fatalf("failed to parse response: %v", err)
	}
	if len(sites) != 2 {
		t.Errorf("expected 2 sites after limit=2, got %d", len(sites))
	}
}

func TestSitesTool_QueryAndLimit(t *testing.T) {
	body := loadTestData(t, "sites.json")
	mock := newMockDoer(body)

	_, handler := SitesTool(mock)

	req := mcp.CallToolRequest{}
	req.Params.Arguments = map[string]any{"query": "sluss", "limit": float64(1)}

	result, err := handler(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	text := result.Content[0].(mcp.TextContent).Text

	var sites []map[string]any
	if err := json.Unmarshal([]byte(text), &sites); err != nil {
		t.Fatalf("failed to parse response: %v", err)
	}
	if len(sites) != 1 {
		t.Errorf("expected 1 site after query=sluss + limit=1, got %d", len(sites))
	}
}

func TestSitesTool_NoMatches(t *testing.T) {
	body := loadTestData(t, "sites.json")
	mock := newMockDoer(body)

	_, handler := SitesTool(mock)

	req := mcp.CallToolRequest{}
	req.Params.Arguments = map[string]any{"query": "nonexistent-xyzzy"}

	result, err := handler(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	text := result.Content[0].(mcp.TextContent).Text

	var sites []map[string]any
	if err := json.Unmarshal([]byte(text), &sites); err != nil {
		t.Fatalf("failed to parse response: %v", err)
	}
	if len(sites) != 0 {
		t.Errorf("expected 0 matches, got %d", len(sites))
	}
}

func TestSitesTool_NoParamsReturnsAll(t *testing.T) {
	body := loadTestData(t, "sites.json")
	mock := newMockDoer(body)

	_, handler := SitesTool(mock)

	req := mcp.CallToolRequest{}
	req.Params.Arguments = map[string]any{}

	result, err := handler(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	text := result.Content[0].(mcp.TextContent).Text

	var sites []map[string]any
	if err := json.Unmarshal([]byte(text), &sites); err != nil {
		t.Fatalf("failed to parse response: %v", err)
	}
	// Fixture has 6 entries; no filter / no limit should return all of them.
	if len(sites) != 6 {
		t.Errorf("expected full 6-entry fixture, got %d", len(sites))
	}
}

func TestDeparturesTool(t *testing.T) {
	body := loadTestData(t, "departures.json")
	mock := &routedMock{routes: []mockRoute{
		{pathContains: "/departures", body: body},
		{pathContains: "/v1/messages", body: "[]"},
	}}

	_, handler := DeparturesTool(mock)

	req := mcp.CallToolRequest{}
	req.Params.Arguments = map[string]any{"site_id": float64(9192)}

	result, err := handler(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if got := departuresURLSeen(mock); !strings.Contains(got, "/v1/sites/9192/departures") {
		t.Errorf("unexpected URL: %s", got)
	}

	text := result.Content[0].(mcp.TextContent).Text
	// Slim mode drops per-row stop_area ("Stockholm City"); destination
	// field survives and identifies each departure.
	if !strings.Contains(text, "Västerhaninge") {
		t.Error("result should contain fixture departure data (Västerhaninge destination)")
	}
}

// departuresFixture builds a /v1/sites/{id}/departures payload whose
// departures[] reference the given stop_area IDs (each with a line 43 entry).
// A placeholder upstream stop_deviation is included so tests can assert the
// rederive path replaces it.
func departuresFixture(siteStopAreaIDs ...int) string {
	type line struct {
		ID int `json:"id"`
	}
	type stopArea struct {
		ID   int    `json:"id"`
		Name string `json:"name"`
	}
	type dep struct {
		StopArea stopArea `json:"stop_area"`
		Line     line     `json:"line"`
	}
	var departures []dep
	for _, id := range siteStopAreaIDs {
		departures = append(departures, dep{
			StopArea: stopArea{ID: id, Name: "site"},
			Line:     line{ID: 43},
		})
	}
	root := map[string]any{
		"departures": departures,
		"stop_deviations": []map[string]any{{
			"id":      999000,
			"message": "(should be replaced)",
			"scope":   map[string]any{"stop_areas": []map[string]any{{"id": 3031, "name": "Kungsträdgården"}}},
		}},
	}
	b, _ := json.Marshal(root)
	return string(b)
}

type msgSpec struct {
	CaseID      int
	StopAreaIDs []int
	LineIDs     []int
	PublishFrom string
	PublishUpto string
	Header      string
}

func messagesFixture(specs ...msgSpec) string {
	type stopArea struct {
		ID   int    `json:"id"`
		Name string `json:"name"`
	}
	type line struct {
		ID            int    `json:"id"`
		Designation   string `json:"designation"`
		TransportMode string `json:"transport_mode"`
	}
	type scope struct {
		StopAreas []stopArea `json:"stop_areas,omitempty"`
		Lines     []line     `json:"lines,omitempty"`
	}
	type variant struct {
		Header   string `json:"header"`
		Language string `json:"language"`
	}
	type dev struct {
		DeviationCaseID int               `json:"deviation_case_id"`
		Publish         map[string]string `json:"publish"`
		Scope           scope             `json:"scope"`
		MessageVariants []variant         `json:"message_variants"`
	}
	var out []dev
	for _, s := range specs {
		var sas []stopArea
		for _, id := range s.StopAreaIDs {
			sas = append(sas, stopArea{ID: id, Name: "area"})
		}
		var lns []line
		for _, id := range s.LineIDs {
			lns = append(lns, line{ID: id, Designation: "X", TransportMode: "METRO"})
		}
		from := s.PublishFrom
		if from == "" {
			from = "2026-01-01T00:00:00+02:00"
		}
		upto := s.PublishUpto
		if upto == "" {
			upto = "2099-01-01T00:00:00+02:00"
		}
		out = append(out, dev{
			DeviationCaseID: s.CaseID,
			Publish:         map[string]string{"from": from, "upto": upto},
			Scope:           scope{StopAreas: sas, Lines: lns},
			MessageVariants: []variant{{Header: s.Header, Language: "sv"}},
		})
	}
	b, _ := json.Marshal(out)
	return string(b)
}

func TestDeparturesTool_RederivesStopDeviationsFromMessages(t *testing.T) {
	// Departures reference stop_area 1051 (T-Centralen metro) + 5310 (commuter rail).
	depBody := departuresFixture(1051, 5310)
	msgs := messagesFixture(
		msgSpec{CaseID: 10421009, StopAreaIDs: []int{1051}, Header: "T-Centralen escalator"},    // matches site
		msgSpec{CaseID: 11062315, StopAreaIDs: []int{3031}, Header: "Kungsträdgården schedule"}, // different area
		msgSpec{CaseID: 11073715, StopAreaIDs: []int{1021}, Header: "Gamla stan lift"},          // different area
	)
	mock := &routedMock{routes: []mockRoute{
		{pathContains: "/v1/sites/9001/departures", body: depBody},
		{pathContains: "/v1/messages", body: msgs},
	}}

	_, handler := DeparturesTool(mock)
	req := mcp.CallToolRequest{}
	req.Params.Arguments = map[string]any{"site_id": float64(9001)}

	result, err := handler(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	text := result.Content[0].(mcp.TextContent).Text

	var out struct {
		StopDeviations []struct {
			ID int `json:"id"`
		} `json:"stop_deviations"`
	}
	if err := json.Unmarshal([]byte(text), &out); err != nil {
		t.Fatalf("parse: %v\n%s", err, text)
	}
	got := map[int]bool{}
	for _, sd := range out.StopDeviations {
		got[sd.ID] = true
	}
	if !got[10421009] {
		t.Errorf("T-Centralen escalator (10421009) should be present")
	}
	if got[11062315] || got[11073715] {
		t.Errorf("neighbouring-station deviations should be dropped")
	}
	if got[999000] {
		t.Errorf("upstream placeholder stop_deviation should be replaced by rederive")
	}
}

func TestDeparturesTool_RederiveKeepsLineOnlyScope(t *testing.T) {
	depBody := departuresFixture(1051, 5310)
	msgs := messagesFixture(
		msgSpec{CaseID: 12345, LineIDs: []int{43}, Header: "Line 43 network-wide"},
		msgSpec{CaseID: 67890, LineIDs: []int{99}, Header: "Line 99 not served"},
	)
	mock := &routedMock{routes: []mockRoute{
		{pathContains: "/v1/sites/9001/departures", body: depBody},
		{pathContains: "/v1/messages", body: msgs},
	}}

	_, handler := DeparturesTool(mock)
	req := mcp.CallToolRequest{}
	req.Params.Arguments = map[string]any{"site_id": float64(9001)}

	result, _ := handler(context.Background(), req)
	text := result.Content[0].(mcp.TextContent).Text

	var out struct {
		StopDeviations []struct{ ID int } `json:"stop_deviations"`
	}
	_ = json.Unmarshal([]byte(text), &out)
	got := map[int]bool{}
	for _, sd := range out.StopDeviations {
		got[sd.ID] = true
	}
	if !got[12345] {
		t.Errorf("line-43 deviation (site serves line 43) should be kept")
	}
	if got[67890] {
		t.Errorf("line-99 deviation should be dropped — site doesn't serve line 99")
	}
}

func TestDeparturesTool_RederiveFiltersExpiredDeviations(t *testing.T) {
	depBody := departuresFixture(1051)
	expired := messagesFixture(msgSpec{
		CaseID:      55555,
		StopAreaIDs: []int{1051},
		PublishFrom: "2026-01-01T00:00:00+02:00",
		PublishUpto: "2026-02-01T00:00:00+02:00",
		Header:      "Already ended",
	})
	mock := &routedMock{routes: []mockRoute{
		{pathContains: "/v1/sites/9001/departures", body: depBody},
		{pathContains: "/v1/messages", body: expired},
	}}

	_, handler := DeparturesTool(mock)
	req := mcp.CallToolRequest{}
	req.Params.Arguments = map[string]any{"site_id": float64(9001)}

	result, _ := handler(context.Background(), req)
	text := result.Content[0].(mcp.TextContent).Text

	var out struct {
		StopDeviations []struct{ ID int } `json:"stop_deviations"`
	}
	_ = json.Unmarshal([]byte(text), &out)
	for _, sd := range out.StopDeviations {
		if sd.ID == 55555 {
			t.Errorf("expired deviation should not attach")
		}
	}
}

func TestDeparturesTool_RederiveFallbackWhenMessagesFails(t *testing.T) {
	depBody := departuresFixture(1051, 5310)
	mock := &routedMock{routes: []mockRoute{
		{pathContains: "/v1/sites/9001/departures", body: depBody},
		{pathContains: "/v1/messages", status: 500, body: "upstream down"},
	}}

	_, handler := DeparturesTool(mock)
	req := mcp.CallToolRequest{}
	req.Params.Arguments = map[string]any{"site_id": float64(9001)}

	result, err := handler(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsError {
		t.Fatalf("departures should not fail when /v1/messages fails: %s",
			result.Content[0].(mcp.TextContent).Text)
	}
	text := result.Content[0].(mcp.TextContent).Text

	var out struct {
		StopDeviations []struct {
			ID int `json:"id"`
		} `json:"stop_deviations"`
	}
	_ = json.Unmarshal([]byte(text), &out)
	for _, sd := range out.StopDeviations {
		if sd.ID == 999000 {
			t.Errorf("fallback should still drop upstream stop_deviation scoped to non-matching stop_area")
		}
	}
}

func TestDeparturesTool_StripsHrefsAndStopPointsFromRemainingScope(t *testing.T) {
	// Even the rederived deviations must not carry broken href values or
	// nested scope.stop_points — verify both survive the strip pass.
	depBody := departuresFixture(1051)
	// Hand-craft a messages body with a href field and a nested stop_points
	// array to prove they're stripped from the final output.
	msgs := `[{
		"deviation_case_id": 77777,
		"publish": {"from": "2026-01-01T00:00:00+02:00", "upto": "2099-01-01T00:00:00+02:00"},
		"scope": {
			"stop_areas": [{"id": 1051, "name": "T-Centralen", "href": "null/stop-areas/1051"}],
			"stop_points": [{"id": 3051, "name": "T-Centralen"}],
			"lines": [{"id": 17, "designation": "17", "transport_mode": "METRO", "href": "null/lines/17"}]
		},
		"message_variants": [{"header": "H", "language": "sv"}]
	}]`
	mock := &routedMock{routes: []mockRoute{
		{pathContains: "/v1/sites/9001/departures", body: depBody},
		{pathContains: "/v1/messages", body: msgs},
	}}

	_, handler := DeparturesTool(mock)
	req := mcp.CallToolRequest{}
	req.Params.Arguments = map[string]any{"site_id": float64(9001)}

	result, _ := handler(context.Background(), req)
	text := result.Content[0].(mcp.TextContent).Text

	if strings.Contains(text, `"null/`) {
		t.Errorf(`broken href values ("null/...") should be stripped`)
	}
	if strings.Contains(text, "stop_points") {
		t.Errorf("scope.stop_points should be dropped from rederived entries")
	}
}

// departuresURLSeen returns the /v1/sites/{id}/departures URL from the mock's
// recorded calls, or empty string if none was made.
func departuresURLSeen(m *routedMock) string {
	for _, c := range m.calls {
		if strings.Contains(c.URL.Path, "/departures") {
			return c.URL.String()
		}
	}
	return ""
}

func TestDeparturesTool_MissingSiteID(t *testing.T) {
	mock := newMockDoer("{}")

	_, handler := DeparturesTool(mock)

	req := mcp.CallToolRequest{}
	req.Params.Arguments = map[string]any{}

	result, err := handler(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !result.IsError {
		t.Error("expected error result when site_id is missing")
	}
}

// P2: line filter narrows a multi-line fixture to exactly the matching
// designation (case-insensitive).
func TestDeparturesTool_LineFilter(t *testing.T) {
	body := loadTestData(t, "departures.json")
	mock := &routedMock{routes: []mockRoute{
		{pathContains: "/departures", body: body},
		{pathContains: "/v1/messages", body: "[]"},
	}}

	_, handler := DeparturesTool(mock)

	req := mcp.CallToolRequest{}
	req.Params.Arguments = map[string]any{"site_id": float64(9192), "line": "43"}

	result, _ := handler(context.Background(), req)
	text := result.Content[0].(mcp.TextContent).Text

	var out struct {
		Departures []struct {
			Line struct {
				Designation string `json:"designation"`
			} `json:"line"`
		} `json:"departures"`
	}
	if err := json.Unmarshal([]byte(text), &out); err != nil {
		t.Fatalf("parse: %v\n%s", err, text)
	}
	// Fixture has line 43 + line 40. line=43 should keep only the first.
	if len(out.Departures) != 1 {
		t.Errorf("expected 1 departure after line=43, got %d", len(out.Departures))
	}
	if len(out.Departures) > 0 && out.Departures[0].Line.Designation != "43" {
		t.Errorf("expected designation=43, got %q", out.Departures[0].Line.Designation)
	}
}

// P2: transport_mode filter, matches case-insensitively on line.transport_mode.
func TestDeparturesTool_TransportModeFilter(t *testing.T) {
	body := loadTestData(t, "departures.json")
	mock := &routedMock{routes: []mockRoute{
		{pathContains: "/departures", body: body},
		{pathContains: "/v1/messages", body: "[]"},
	}}

	_, handler := DeparturesTool(mock)

	req := mcp.CallToolRequest{}
	req.Params.Arguments = map[string]any{"site_id": float64(9192), "transport_mode": "train"}

	result, _ := handler(context.Background(), req)
	text := result.Content[0].(mcp.TextContent).Text

	var out struct {
		Departures []any `json:"departures"`
	}
	_ = json.Unmarshal([]byte(text), &out)
	// Both fixture rows are TRAIN mode; both should survive the filter.
	if len(out.Departures) != 2 {
		t.Errorf("expected 2 TRAIN departures, got %d", len(out.Departures))
	}
}

// P2: direction_code filter keeps only rows whose direction_code matches.
func TestDeparturesTool_DirectionCodeFilter(t *testing.T) {
	// Craft a body with rows in both directions (1 and 2).
	body := `{
		"departures": [
			{"destination": "A", "direction_code": 1, "stop_area": {"id": 5310}, "line": {"id": 43, "designation": "43", "transport_mode": "TRAIN"}},
			{"destination": "B", "direction_code": 2, "stop_area": {"id": 5310}, "line": {"id": 43, "designation": "43", "transport_mode": "TRAIN"}}
		],
		"stop_deviations": []
	}`
	mock := &routedMock{routes: []mockRoute{
		{pathContains: "/departures", body: body},
		{pathContains: "/v1/messages", body: "[]"},
	}}

	_, handler := DeparturesTool(mock)

	req := mcp.CallToolRequest{}
	req.Params.Arguments = map[string]any{"site_id": float64(9192), "direction_code": float64(2)}

	result, _ := handler(context.Background(), req)
	text := result.Content[0].(mcp.TextContent).Text

	var out struct {
		Departures []struct {
			Destination   string  `json:"destination"`
			DirectionCode float64 `json:"direction_code"`
		} `json:"departures"`
	}
	_ = json.Unmarshal([]byte(text), &out)
	if len(out.Departures) != 1 {
		t.Fatalf("expected 1 departure after direction_code=2, got %d", len(out.Departures))
	}
	if out.Departures[0].Destination != "B" {
		t.Errorf("expected destination=B, got %q", out.Departures[0].Destination)
	}
}

// P2: limit truncates the filtered result.
func TestDeparturesTool_Limit(t *testing.T) {
	// Build 5 identical departures so we can observe truncation clearly.
	deps := []any{}
	for i := 0; i < 5; i++ {
		deps = append(deps, map[string]any{
			"destination":    "X",
			"direction_code": float64(1),
			"stop_area":      map[string]any{"id": 5310},
			"line":           map[string]any{"id": 43, "designation": "43", "transport_mode": "TRAIN"},
		})
	}
	b, _ := json.Marshal(map[string]any{"departures": deps, "stop_deviations": []any{}})

	mock := &routedMock{routes: []mockRoute{
		{pathContains: "/departures", body: string(b)},
		{pathContains: "/v1/messages", body: "[]"},
	}}

	_, handler := DeparturesTool(mock)

	req := mcp.CallToolRequest{}
	req.Params.Arguments = map[string]any{"site_id": float64(9192), "limit": float64(3)}

	result, _ := handler(context.Background(), req)
	text := result.Content[0].(mcp.TextContent).Text

	var out struct {
		Departures []any `json:"departures"`
	}
	_ = json.Unmarshal([]byte(text), &out)
	if len(out.Departures) != 3 {
		t.Errorf("expected 3 departures after limit=3, got %d", len(out.Departures))
	}
}

// Round 2, Section 5: no-arg departures caps at 20 by default (was
// unlimited in the previous release). Callers needing the full upstream
// set opt in with limit=0.
func TestDeparturesTool_DefaultLimitIs20(t *testing.T) {
	// 30 identical departures so truncation is visible.
	deps := []any{}
	for i := 0; i < 30; i++ {
		deps = append(deps, map[string]any{
			"destination":    "X",
			"direction_code": float64(1),
			"stop_area":      map[string]any{"id": 5310},
			"line":           map[string]any{"id": 43, "designation": "43", "transport_mode": "TRAIN"},
		})
	}
	b, _ := json.Marshal(map[string]any{"departures": deps, "stop_deviations": []any{}})

	mock := &routedMock{routes: []mockRoute{
		{pathContains: "/departures", body: string(b)},
		{pathContains: "/v1/messages", body: "[]"},
	}}

	_, handler := DeparturesTool(mock)
	req := mcp.CallToolRequest{}
	req.Params.Arguments = map[string]any{"site_id": float64(9192)}

	result, _ := handler(context.Background(), req)
	text := result.Content[0].(mcp.TextContent).Text

	var out struct {
		Departures []any `json:"departures"`
	}
	_ = json.Unmarshal([]byte(text), &out)
	if len(out.Departures) != 20 {
		t.Errorf("expected default limit 20, got %d", len(out.Departures))
	}
}

// Round 2, Section 5: limit=0 explicitly means unlimited.
func TestDeparturesTool_LimitZeroIsUnlimited(t *testing.T) {
	deps := []any{}
	for i := 0; i < 30; i++ {
		deps = append(deps, map[string]any{
			"destination":    "X",
			"direction_code": float64(1),
			"stop_area":      map[string]any{"id": 5310},
			"line":           map[string]any{"id": 43, "designation": "43", "transport_mode": "TRAIN"},
		})
	}
	b, _ := json.Marshal(map[string]any{"departures": deps, "stop_deviations": []any{}})

	mock := &routedMock{routes: []mockRoute{
		{pathContains: "/departures", body: string(b)},
		{pathContains: "/v1/messages", body: "[]"},
	}}

	_, handler := DeparturesTool(mock)
	req := mcp.CallToolRequest{}
	req.Params.Arguments = map[string]any{"site_id": float64(9192), "limit": float64(0)}

	result, _ := handler(context.Background(), req)
	text := result.Content[0].(mcp.TextContent).Text

	var out struct {
		Departures []any `json:"departures"`
	}
	_ = json.Unmarshal([]byte(text), &out)
	if len(out.Departures) != 30 {
		t.Errorf("expected limit=0 to mean unlimited (30), got %d", len(out.Departures))
	}
}

// Round 2, Section 5: line is now a prefix match. "43" matches 43 and 43X.
func TestDeparturesTool_LinePrefixMatch(t *testing.T) {
	body := `{
		"departures": [
			{"destination":"A","direction_code":1,"stop_area":{"id":5310},"line":{"id":43,"designation":"43","transport_mode":"TRAIN"}},
			{"destination":"B","direction_code":1,"stop_area":{"id":5310},"line":{"id":430,"designation":"43X","transport_mode":"TRAIN"}},
			{"destination":"C","direction_code":1,"stop_area":{"id":5310},"line":{"id":4,"designation":"4","transport_mode":"BUS"}}
		],
		"stop_deviations": []
	}`
	mock := &routedMock{routes: []mockRoute{
		{pathContains: "/departures", body: body},
		{pathContains: "/v1/messages", body: "[]"},
	}}

	_, handler := DeparturesTool(mock)
	req := mcp.CallToolRequest{}
	req.Params.Arguments = map[string]any{"site_id": float64(9192), "line": "43"}

	result, _ := handler(context.Background(), req)
	text := result.Content[0].(mcp.TextContent).Text

	var out struct {
		Departures []struct {
			Line struct {
				Designation string `json:"designation"`
			} `json:"line"`
		} `json:"departures"`
	}
	_ = json.Unmarshal([]byte(text), &out)
	// "43" should prefix-match 43 and 43X, but NOT "4" (that would be a
	// substring match, which is different — prefix of "4" would include
	// 43, 43X, and 4 itself; prefix of "43" excludes "4").
	if len(out.Departures) != 2 {
		t.Fatalf("expected 2 prefix matches for '43' (43 and 43X), got %d: %+v", len(out.Departures), out.Departures)
	}
	for _, d := range out.Departures {
		if !strings.HasPrefix(d.Line.Designation, "43") {
			t.Errorf("result %+v doesn't have prefix 43", d)
		}
	}
}

// Round 2, Section 5: prefix "4" would match 43, 43X, AND the bus "4" —
// confirms that prefix semantics differ from the old exact-match behavior.
func TestDeparturesTool_LinePrefixSingleChar(t *testing.T) {
	body := `{
		"departures": [
			{"destination":"A","direction_code":1,"stop_area":{"id":5310},"line":{"id":43,"designation":"43","transport_mode":"TRAIN"}},
			{"destination":"B","direction_code":1,"stop_area":{"id":5310},"line":{"id":4,"designation":"4","transport_mode":"BUS"}}
		],
		"stop_deviations": []
	}`
	mock := &routedMock{routes: []mockRoute{
		{pathContains: "/departures", body: body},
		{pathContains: "/v1/messages", body: "[]"},
	}}

	_, handler := DeparturesTool(mock)
	req := mcp.CallToolRequest{}
	req.Params.Arguments = map[string]any{"site_id": float64(9192), "line": "4"}

	result, _ := handler(context.Background(), req)
	text := result.Content[0].(mcp.TextContent).Text

	var out struct {
		Departures []any `json:"departures"`
	}
	_ = json.Unmarshal([]byte(text), &out)
	if len(out.Departures) != 2 {
		t.Errorf("expected prefix '4' to match both 43 and 4, got %d", len(out.Departures))
	}
}

// P2: filters compose. line + transport_mode + limit should narrow to
// exactly the first matching row.
func TestDeparturesTool_FiltersCompose(t *testing.T) {
	body := loadTestData(t, "departures.json")
	mock := &routedMock{routes: []mockRoute{
		{pathContains: "/departures", body: body},
		{pathContains: "/v1/messages", body: "[]"},
	}}

	_, handler := DeparturesTool(mock)

	req := mcp.CallToolRequest{}
	req.Params.Arguments = map[string]any{
		"site_id":        float64(9192),
		"transport_mode": "TRAIN",
		"line":           "40",
		"limit":          float64(10),
	}

	result, _ := handler(context.Background(), req)
	text := result.Content[0].(mcp.TextContent).Text

	var out struct {
		Departures []struct {
			Line struct {
				Designation string `json:"designation"`
			} `json:"line"`
		} `json:"departures"`
	}
	_ = json.Unmarshal([]byte(text), &out)
	if len(out.Departures) != 1 {
		t.Fatalf("expected 1 departure after compound filter, got %d", len(out.Departures))
	}
	if out.Departures[0].Line.Designation != "40" {
		t.Errorf("expected designation=40, got %q", out.Departures[0].Line.Designation)
	}
}

// P2: default response drops per-row stop_area and journey from each
// departure entry, and slims the line object to designation /
// transport_mode / group_of_lines. Payload is materially smaller than
// verbose.
func TestDeparturesTool_DefaultSlimsPerRowRedundancy(t *testing.T) {
	body := loadTestData(t, "departures.json")
	mock := &routedMock{routes: []mockRoute{
		{pathContains: "/departures", body: body},
		{pathContains: "/v1/messages", body: "[]"},
	}}

	_, handler := DeparturesTool(mock)

	req := mcp.CallToolRequest{}
	req.Params.Arguments = map[string]any{"site_id": float64(9192)}

	result, _ := handler(context.Background(), req)
	text := result.Content[0].(mcp.TextContent).Text

	var out struct {
		Departures []map[string]any `json:"departures"`
	}
	if err := json.Unmarshal([]byte(text), &out); err != nil {
		t.Fatalf("parse: %v\n%s", err, text)
	}
	for i, dep := range out.Departures {
		if _, ok := dep["stop_area"]; ok {
			t.Errorf("dep[%d]: stop_area should be dropped in slim mode", i)
		}
		if _, ok := dep["journey"]; ok {
			t.Errorf("dep[%d]: journey should be dropped in slim mode", i)
		}
		line, ok := dep["line"].(map[string]any)
		if !ok {
			t.Fatalf("dep[%d]: expected line object", i)
		}
		// Line should only carry designation / transport_mode /
		// group_of_lines in slim mode.
		for k := range line {
			switch k {
			case "designation", "transport_mode", "group_of_lines":
				// ok
			default:
				t.Errorf("dep[%d].line: unexpected field %q in slim mode", i, k)
			}
		}
	}
}

// P2: verbose=true preserves the full per-row stop_area / journey / line.
func TestDeparturesTool_VerbosePreservesFullRow(t *testing.T) {
	body := loadTestData(t, "departures.json")
	mock := &routedMock{routes: []mockRoute{
		{pathContains: "/departures", body: body},
		{pathContains: "/v1/messages", body: "[]"},
	}}

	_, handler := DeparturesTool(mock)

	req := mcp.CallToolRequest{}
	req.Params.Arguments = map[string]any{"site_id": float64(9192), "verbose": true}

	result, _ := handler(context.Background(), req)
	text := result.Content[0].(mcp.TextContent).Text

	var out struct {
		Departures []map[string]any `json:"departures"`
	}
	_ = json.Unmarshal([]byte(text), &out)
	if len(out.Departures) == 0 {
		t.Fatal("expected departures")
	}
	if _, ok := out.Departures[0]["stop_area"]; !ok {
		t.Errorf("verbose mode should preserve stop_area")
	}
	if _, ok := out.Departures[0]["journey"]; !ok {
		t.Errorf("verbose mode should preserve journey")
	}
	line, _ := out.Departures[0]["line"].(map[string]any)
	if _, ok := line["id"]; !ok {
		t.Errorf("verbose mode should preserve line.id")
	}
}

// P2: payload slimming must materially shrink the response for a busy
// terminal. Generate 35 departures all pointing at the same site and
// compare sizes.
func TestDeparturesTool_DefaultIsSignificantlySmaller(t *testing.T) {
	// Build a fixture with 35 departures, all of which share the same
	// stop_area/stop_point/line shapes.
	type line struct {
		ID                   int    `json:"id"`
		Designation          string `json:"designation"`
		TransportAuthorityID int    `json:"transport_authority_id"`
		TransportMode        string `json:"transport_mode"`
		GroupOfLines         string `json:"group_of_lines"`
	}
	type sa struct {
		ID   int    `json:"id"`
		Name string `json:"name"`
		Type string `json:"type"`
	}
	type sp struct {
		ID          int    `json:"id"`
		Name        string `json:"name"`
		Designation string `json:"designation"`
	}
	type journey struct {
		ID              int    `json:"id"`
		State           string `json:"state"`
		PredictionState string `json:"prediction_state"`
	}
	type dep struct {
		Destination   string  `json:"destination"`
		DirectionCode int     `json:"direction_code"`
		Direction     string  `json:"direction"`
		State         string  `json:"state"`
		Scheduled     string  `json:"scheduled"`
		Expected      string  `json:"expected"`
		Journey       journey `json:"journey"`
		StopArea      sa      `json:"stop_area"`
		StopPoint     sp      `json:"stop_point"`
		Line          line    `json:"line"`
		Deviations    []any   `json:"deviations"`
	}
	deps := make([]dep, 35)
	for i := range deps {
		deps[i] = dep{
			Destination: "Västerhaninge", DirectionCode: 1, Direction: "Nynäshamn",
			State: "ATSTOP", Scheduled: "2026-04-21T23:51:00", Expected: "2026-04-21T23:51:00",
			Journey:    journey{ID: 2026042102879 + i, State: "NORMALPROGRESS", PredictionState: "NORMAL"},
			StopArea:   sa{ID: 5310, Name: "Stockholm City", Type: "RAILWSTN"},
			StopPoint:  sp{ID: 5313, Name: "Stockholm City", Designation: "4"},
			Line:       line{ID: 43, Designation: "43", TransportAuthorityID: 1, TransportMode: "TRAIN", GroupOfLines: "Pendeltåg"},
			Deviations: []any{},
		}
	}
	body, _ := json.Marshal(map[string]any{"departures": deps, "stop_deviations": []any{}})

	makeMock := func() *routedMock {
		return &routedMock{routes: []mockRoute{
			{pathContains: "/departures", body: string(body)},
			{pathContains: "/v1/messages", body: "[]"},
		}}
	}

	_, handler := DeparturesTool(makeMock())
	req := mcp.CallToolRequest{}
	req.Params.Arguments = map[string]any{"site_id": float64(9192)}
	slimResult, _ := handler(context.Background(), req)
	slimSize := len(slimResult.Content[0].(mcp.TextContent).Text)

	_, handler = DeparturesTool(makeMock())
	req.Params.Arguments = map[string]any{"site_id": float64(9192), "verbose": true}
	verboseResult, _ := handler(context.Background(), req)
	verboseSize := len(verboseResult.Content[0].(mcp.TextContent).Text)

	// 40% target from the P2 spec: slim should be at most 60% of verbose.
	if slimSize >= verboseSize*6/10 {
		t.Errorf("slim response (%d bytes) should be <60%% of verbose (%d bytes)", slimSize, verboseSize)
	}
}

func TestDeparturesTool_NormalizesInputForms(t *testing.T) {
	// Every accepted site_id form — short, 8-digit 18xx, 9-digit 3BA1CDEFG,
	// and 16-digit GID — must resolve to the same short-form upstream URL.
	// Jakobsberg (9702) picked because stop_finder documents all four forms
	// for it in the trafiklab support article.
	cases := []struct {
		name  string
		input any
	}{
		{"short string", "9702"},
		{"short number (backwards compat)", float64(9702)},
		{"8-digit 18xx string", "18009702"},
		{"9-digit string", "300109702"},
		{"16-digit GID string", "9091001000009702"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			mock := &routedMock{routes: []mockRoute{
				{pathContains: "/v1/sites/9702/departures", body: `{"departures":[],"stop_deviations":[]}`},
				{pathContains: "/v1/messages", body: "[]"},
			}}

			_, handler := DeparturesTool(mock)

			req := mcp.CallToolRequest{}
			req.Params.Arguments = map[string]any{"site_id": tc.input}

			result, err := handler(context.Background(), req)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if result.IsError {
				t.Fatalf("normalization should succeed: %s", result.Content[0].(mcp.TextContent).Text)
			}
			if got := departuresURLSeen(mock); !strings.Contains(got, "/v1/sites/9702/departures") {
				t.Errorf("expected short-form URL regardless of input shape, got %s", got)
			}
		})
	}
}

func TestDeparturesTool_InvalidFormatReturnsStructuredError(t *testing.T) {
	cases := []any{
		"foo",
		"9702.5",
		"-1",
		// Oversized number loses precision at the JSON boundary; reject.
		float64(1e18),
	}
	for _, in := range cases {
		mock := newMockDoer("{}")
		_, handler := DeparturesTool(mock)

		req := mcp.CallToolRequest{}
		req.Params.Arguments = map[string]any{"site_id": in}

		result, err := handler(context.Background(), req)
		if err != nil {
			t.Fatalf("unexpected error for %v: %v", in, err)
		}
		if !result.IsError {
			t.Fatalf("expected IsError for %v", in)
		}
		text := result.Content[0].(mcp.TextContent).Text
		if !strings.Contains(text, `"error":"invalid_site_id_format"`) {
			t.Errorf("expected invalid_site_id_format JSON for %v, got %s", in, text)
		}
	}
}

func TestDeparturesTool_OutOfRangeReturnsStructuredError(t *testing.T) {
	cases := []any{
		"0",
		"101",
		"10000",
		"99999999",
	}
	for _, in := range cases {
		mock := newMockDoer("{}")
		_, handler := DeparturesTool(mock)

		req := mcp.CallToolRequest{}
		req.Params.Arguments = map[string]any{"site_id": in}

		result, err := handler(context.Background(), req)
		if err != nil {
			t.Fatalf("unexpected error for %v: %v", in, err)
		}
		if !result.IsError {
			t.Fatalf("expected IsError for %v", in)
		}
		text := result.Content[0].(mcp.TextContent).Text
		if !strings.Contains(text, `"error":"site_id_out_of_range"`) {
			t.Errorf("expected site_id_out_of_range JSON for %v, got %s", in, text)
		}
	}
}

func TestLinesTool(t *testing.T) {
	body := loadTestData(t, "lines.json")
	mock := newMockDoer(body)

	_, handler := LinesTool(mock)

	req := mcp.CallToolRequest{}
	req.Params.Arguments = map[string]any{}

	result, err := handler(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// /v1/lines requires transport_authority_id or SL returns HTTP 400.
	// Default to 1 (Storstockholms Lokaltrafik) since this is an SL server.
	if mock.lastReq.URL.String() != "https://transport.integration.sl.se/v1/lines?transport_authority_id=1" {
		t.Errorf("unexpected URL: %s", mock.lastReq.URL.String())
	}

	text := result.Content[0].(mcp.TextContent).Text
	if !strings.Contains(text, "blåbuss") {
		t.Error("result should contain fixture data")
	}
}

func TestLinesTool_TransportAuthorityIDOverride(t *testing.T) {
	body := loadTestData(t, "lines.json")
	mock := newMockDoer(body)

	_, handler := LinesTool(mock)

	req := mcp.CallToolRequest{}
	req.Params.Arguments = map[string]any{"transport_authority_id": float64(2)}

	if _, err := handler(context.Background(), req); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if mock.lastReq.URL.String() != "https://transport.integration.sl.se/v1/lines?transport_authority_id=2" {
		t.Errorf("unexpected URL: %s", mock.lastReq.URL.String())
	}
}

func TestLinesTool_NoParamsReturnsFlatArrayOfAll(t *testing.T) {
	body := loadTestData(t, "lines.json")
	mock := newMockDoer(body)

	_, handler := LinesTool(mock)

	req := mcp.CallToolRequest{}
	req.Params.Arguments = map[string]any{}

	result, err := handler(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	text := result.Content[0].(mcp.TextContent).Text

	var lines []map[string]any
	if err := json.Unmarshal([]byte(text), &lines); err != nil {
		t.Fatalf("expected flat array, got: %v\n%s", err, text)
	}
	// Fixture: 3 metro + 3 bus + 2 tram + 1 train + 1 ferry = 10.
	if len(lines) != 10 {
		t.Errorf("expected 10 lines from fixture, got %d", len(lines))
	}
}

// Round 2, Section 2: default shape is slim. Drops gid, transport_authority,
// contractor, valid; keeps {id, designation, transport_mode, group_of_lines, name}.
func TestLinesTool_DefaultShapeIsSlim(t *testing.T) {
	body := loadTestData(t, "lines.json")
	mock := newMockDoer(body)

	_, handler := LinesTool(mock)

	req := mcp.CallToolRequest{}
	req.Params.Arguments = map[string]any{"transport_mode": "metro"}

	result, _ := handler(context.Background(), req)
	text := result.Content[0].(mcp.TextContent).Text

	var lines []map[string]any
	_ = json.Unmarshal([]byte(text), &lines)
	if len(lines) == 0 {
		t.Fatal("expected metro lines")
	}
	allowed := map[string]bool{
		"id": true, "designation": true, "transport_mode": true,
		"group_of_lines": true, "name": true,
	}
	for i, l := range lines {
		for k := range l {
			if !allowed[k] {
				t.Errorf("line[%d]: unexpected field %q in slim shape", i, k)
			}
		}
	}

	// Fields the slim shape must drop.
	for _, forbidden := range []string{`"gid"`, `"transport_authority"`, `"contractor"`, `"valid"`} {
		if strings.Contains(text, forbidden) {
			t.Errorf("slim response should not contain %s, got %s", forbidden, text)
		}
	}
}

// Round 2, Section 2: verbose=true preserves all upstream fields so
// callers who need gid/transport_authority/valid can still get them.
func TestLinesTool_VerbosePreservesAllFields(t *testing.T) {
	body := loadTestData(t, "lines.json")
	mock := newMockDoer(body)

	_, handler := LinesTool(mock)

	req := mcp.CallToolRequest{}
	req.Params.Arguments = map[string]any{"transport_mode": "metro", "verbose": true}

	result, _ := handler(context.Background(), req)
	text := result.Content[0].(mcp.TextContent).Text

	for _, expected := range []string{`"gid"`, `"transport_authority"`, `"valid"`} {
		if !strings.Contains(text, expected) {
			t.Errorf("verbose response should contain %s, got %s", expected, text)
		}
	}
}

// Round 2, Section 2 acceptance: group_of_lines substring filter still
// works in slim mode. Fixture has two entries tagged "blåbuss".
func TestLinesTool_GroupOfLinesSlimFilter(t *testing.T) {
	body := loadTestData(t, "lines.json")
	mock := newMockDoer(body)

	_, handler := LinesTool(mock)

	req := mcp.CallToolRequest{}
	req.Params.Arguments = map[string]any{"group_of_lines": "blåbuss"}

	result, _ := handler(context.Background(), req)
	text := result.Content[0].(mcp.TextContent).Text

	var lines []map[string]any
	_ = json.Unmarshal([]byte(text), &lines)
	if len(lines) != 2 {
		t.Fatalf("expected 2 blåbuss entries, got %d: %v", len(lines), lines)
	}
	// Slim shape must carry group_of_lines; it's one of the kept fields.
	for _, l := range lines {
		if g, _ := l["group_of_lines"].(string); !strings.EqualFold(g, "blåbuss") {
			t.Errorf("expected group_of_lines=blåbuss, got %q", g)
		}
	}
}

func TestLinesTool_QueryFiltersByName(t *testing.T) {
	body := loadTestData(t, "lines.json")
	mock := newMockDoer(body)

	_, handler := LinesTool(mock)

	req := mcp.CallToolRequest{}
	req.Params.Arguments = map[string]any{"query": "röda"}

	result, err := handler(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	text := result.Content[0].(mcp.TextContent).Text

	var lines []map[string]any
	if err := json.Unmarshal([]byte(text), &lines); err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(lines) != 2 {
		t.Errorf("expected 2 Röda linjen entries, got %d: %v", len(lines), lines)
	}
	for _, l := range lines {
		if l["transport_mode"] != "METRO" {
			t.Errorf("expected METRO mode, got %v", l["transport_mode"])
		}
	}
}

func TestLinesTool_QueryMatchesDesignation(t *testing.T) {
	body := loadTestData(t, "lines.json")
	mock := newMockDoer(body)

	_, handler := LinesTool(mock)

	req := mcp.CallToolRequest{}
	req.Params.Arguments = map[string]any{"query": "471"}

	result, err := handler(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	text := result.Content[0].(mcp.TextContent).Text

	var lines []map[string]any
	if err := json.Unmarshal([]byte(text), &lines); err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(lines) != 1 {
		t.Fatalf("expected 1 match on designation 471, got %d: %v", len(lines), lines)
	}
	if lines[0]["designation"] != "471" {
		t.Errorf("expected designation 471, got %v", lines[0]["designation"])
	}
}

func TestLinesTool_LimitTruncates(t *testing.T) {
	body := loadTestData(t, "lines.json")
	mock := newMockDoer(body)

	_, handler := LinesTool(mock)

	req := mcp.CallToolRequest{}
	req.Params.Arguments = map[string]any{"limit": float64(3)}

	result, err := handler(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	text := result.Content[0].(mcp.TextContent).Text

	var lines []map[string]any
	if err := json.Unmarshal([]byte(text), &lines); err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(lines) != 3 {
		t.Errorf("expected 3 lines after limit=3, got %d", len(lines))
	}
}

func TestLinesTool_QueryAndLimit(t *testing.T) {
	body := loadTestData(t, "lines.json")
	mock := newMockDoer(body)

	_, handler := LinesTool(mock)

	req := mcp.CallToolRequest{}
	req.Params.Arguments = map[string]any{"query": "röda", "limit": float64(1)}

	result, err := handler(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	text := result.Content[0].(mcp.TextContent).Text

	var lines []map[string]any
	if err := json.Unmarshal([]byte(text), &lines); err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(lines) != 1 {
		t.Errorf("expected 1 line after query=röda + limit=1, got %d", len(lines))
	}
}

func TestLinesTool_TransportModeFilter(t *testing.T) {
	body := loadTestData(t, "lines.json")
	mock := newMockDoer(body)

	_, handler := LinesTool(mock)

	req := mcp.CallToolRequest{}
	req.Params.Arguments = map[string]any{"transport_mode": "metro"}

	result, err := handler(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	text := result.Content[0].(mcp.TextContent).Text

	var lines []map[string]any
	if err := json.Unmarshal([]byte(text), &lines); err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(lines) != 3 {
		t.Errorf("expected 3 metro lines, got %d", len(lines))
	}
	for _, l := range lines {
		if l["transport_mode"] != "METRO" {
			t.Errorf("expected transport_mode METRO, got %v", l["transport_mode"])
		}
	}
}

func TestLinesTool_TransportModeCaseInsensitive(t *testing.T) {
	body := loadTestData(t, "lines.json")
	mock := newMockDoer(body)

	_, handler := LinesTool(mock)

	req := mcp.CallToolRequest{}
	req.Params.Arguments = map[string]any{"transport_mode": "METRO"}

	result, err := handler(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	text := result.Content[0].(mcp.TextContent).Text

	var lines []map[string]any
	if err := json.Unmarshal([]byte(text), &lines); err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(lines) != 3 {
		t.Errorf("expected 3 metro lines (case-insensitive), got %d", len(lines))
	}
}

func TestLinesTool_TransportModeUnknownReturnsEmpty(t *testing.T) {
	body := loadTestData(t, "lines.json")
	mock := newMockDoer(body)

	_, handler := LinesTool(mock)

	req := mcp.CallToolRequest{}
	req.Params.Arguments = map[string]any{"transport_mode": "hovercraft"}

	result, err := handler(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsError {
		t.Fatalf("unknown mode should return empty array, not error: %s",
			result.Content[0].(mcp.TextContent).Text)
	}
	text := result.Content[0].(mcp.TextContent).Text

	var lines []map[string]any
	if err := json.Unmarshal([]byte(text), &lines); err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(lines) != 0 {
		t.Errorf("expected empty array for unknown mode, got %d entries", len(lines))
	}
}

func TestLinesTool_AllFiltersCombined(t *testing.T) {
	body := loadTestData(t, "lines.json")
	mock := newMockDoer(body)

	_, handler := LinesTool(mock)

	req := mcp.CallToolRequest{}
	req.Params.Arguments = map[string]any{
		"transport_mode": "bus",
		"query":          "sluss",
		"limit":          float64(5),
	}

	result, err := handler(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	text := result.Content[0].(mcp.TextContent).Text

	var lines []map[string]any
	if err := json.Unmarshal([]byte(text), &lines); err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(lines) != 1 {
		t.Fatalf("expected 1 bus+sluss match, got %d: %v", len(lines), lines)
	}
	if lines[0]["name"] != "Slussen-Saltsjöbaden" {
		t.Errorf("expected Slussen-Saltsjöbaden, got %v", lines[0]["name"])
	}
}

// P1: designation prefix filter. "4" matches designation 4 and 471 but not
// lines whose name starts with 4.
func TestLinesTool_DesignationPrefixFilter(t *testing.T) {
	body := loadTestData(t, "lines.json")
	mock := newMockDoer(body)

	_, handler := LinesTool(mock)

	req := mcp.CallToolRequest{}
	req.Params.Arguments = map[string]any{"designation": "4"}

	result, err := handler(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	text := result.Content[0].(mcp.TextContent).Text

	var lines []map[string]any
	if err := json.Unmarshal([]byte(text), &lines); err != nil {
		t.Fatalf("parse: %v", err)
	}
	// Fixture has designation=4 (bus), 471 (bus), 40 (train). All start with "4".
	wantSet := map[string]bool{"4": true, "471": true, "40": true}
	gotSet := map[string]bool{}
	for _, l := range lines {
		gotSet[l["designation"].(string)] = true
	}
	for d := range wantSet {
		if !gotSet[d] {
			t.Errorf("missing expected designation %q, got %v", d, gotSet)
		}
	}
	for d := range gotSet {
		if !wantSet[d] {
			t.Errorf("unexpected designation %q in results (prefix='4')", d)
		}
	}
}

// P1: designation is prefix-only. "47" matches 471 but not 4.
func TestLinesTool_DesignationPrefixIsStrict(t *testing.T) {
	body := loadTestData(t, "lines.json")
	mock := newMockDoer(body)

	_, handler := LinesTool(mock)

	req := mcp.CallToolRequest{}
	req.Params.Arguments = map[string]any{"designation": "47"}

	result, _ := handler(context.Background(), req)
	text := result.Content[0].(mcp.TextContent).Text

	var lines []map[string]any
	_ = json.Unmarshal([]byte(text), &lines)
	if len(lines) != 1 {
		t.Fatalf("expected 1 match for prefix '47', got %d", len(lines))
	}
	if lines[0]["designation"] != "471" {
		t.Errorf("expected designation=471, got %v", lines[0]["designation"])
	}
}

// P1: group_of_lines substring filter. "blåbuss" matches both fixture
// entries tagged with that group.
func TestLinesTool_GroupOfLinesFilter(t *testing.T) {
	body := loadTestData(t, "lines.json")
	mock := newMockDoer(body)

	_, handler := LinesTool(mock)

	req := mcp.CallToolRequest{}
	req.Params.Arguments = map[string]any{"group_of_lines": "blåbuss"}

	result, _ := handler(context.Background(), req)
	text := result.Content[0].(mcp.TextContent).Text

	var lines []map[string]any
	_ = json.Unmarshal([]byte(text), &lines)
	if len(lines) != 2 {
		t.Fatalf("expected 2 blåbuss entries, got %d", len(lines))
	}
	for _, l := range lines {
		if g, _ := l["group_of_lines"].(string); !strings.Contains(strings.ToLower(g), "blåbuss") {
			t.Errorf("expected group_of_lines to contain blåbuss, got %q", g)
		}
	}
}

// P1: group_of_lines is case-insensitive and matches substrings inside
// the full group name ("tunnelbanans röda linje" contains "röda").
func TestLinesTool_GroupOfLinesCaseInsensitiveAndSubstring(t *testing.T) {
	body := loadTestData(t, "lines.json")
	mock := newMockDoer(body)

	_, handler := LinesTool(mock)

	req := mcp.CallToolRequest{}
	req.Params.Arguments = map[string]any{"group_of_lines": "RÖDA"}

	result, _ := handler(context.Background(), req)
	text := result.Content[0].(mcp.TextContent).Text

	var lines []map[string]any
	_ = json.Unmarshal([]byte(text), &lines)
	if len(lines) != 2 {
		t.Fatalf("expected 2 röda-line entries (metro 13 and 14), got %d", len(lines))
	}
}

// P1: designation + transport_mode compose. "4" prefix + metro should
// return nothing (metros in the fixture are 10, 13, 14).
func TestLinesTool_DesignationAndModeCompose(t *testing.T) {
	body := loadTestData(t, "lines.json")
	mock := newMockDoer(body)

	_, handler := LinesTool(mock)

	req := mcp.CallToolRequest{}
	req.Params.Arguments = map[string]any{
		"designation":    "4",
		"transport_mode": "metro",
	}

	result, _ := handler(context.Background(), req)
	text := result.Content[0].(mcp.TextContent).Text

	var lines []map[string]any
	_ = json.Unmarshal([]byte(text), &lines)
	if len(lines) != 0 {
		t.Errorf("expected no metros starting with '4', got %d", len(lines))
	}
}

// P1: default limit is 50 (anti-flood). Fixture is 10 lines, so no
// truncation happens, but the filter default must be applied.
func TestLinesTool_DefaultLimitIsFifty(t *testing.T) {
	// Craft a response with 80 lines to prove the default cap kicks in.
	type line struct {
		ID            int    `json:"id"`
		Designation   string `json:"designation"`
		Name          string `json:"name"`
		TransportMode string `json:"transport_mode"`
	}
	bus := make([]line, 80)
	for i := range bus {
		bus[i] = line{
			ID:            1000 + i,
			Designation:   fmt.Sprintf("b%d", i),
			Name:          fmt.Sprintf("Line %d", i),
			TransportMode: "BUS",
		}
	}
	b, _ := json.Marshal(map[string]any{"bus": bus})
	mock := newMockDoer(string(b))

	_, handler := LinesTool(mock)
	req := mcp.CallToolRequest{}
	req.Params.Arguments = map[string]any{}

	result, err := handler(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	text := result.Content[0].(mcp.TextContent).Text

	var lines []map[string]any
	_ = json.Unmarshal([]byte(text), &lines)
	if len(lines) != 50 {
		t.Errorf("expected default limit of 50, got %d", len(lines))
	}
}

// P1: limit=0 means unlimited. 80 lines in, 80 out.
func TestLinesTool_ZeroLimitMeansUnlimited(t *testing.T) {
	type line struct {
		ID            int    `json:"id"`
		Designation   string `json:"designation"`
		Name          string `json:"name"`
		TransportMode string `json:"transport_mode"`
	}
	bus := make([]line, 80)
	for i := range bus {
		bus[i] = line{ID: i, Designation: fmt.Sprintf("b%d", i), Name: "x", TransportMode: "BUS"}
	}
	b, _ := json.Marshal(map[string]any{"bus": bus})
	mock := newMockDoer(string(b))

	_, handler := LinesTool(mock)
	req := mcp.CallToolRequest{}
	req.Params.Arguments = map[string]any{"limit": float64(0)}

	result, _ := handler(context.Background(), req)
	text := result.Content[0].(mcp.TextContent).Text

	var lines []map[string]any
	_ = json.Unmarshal([]byte(text), &lines)
	if len(lines) != 80 {
		t.Errorf("expected limit=0 to mean unlimited (80), got %d", len(lines))
	}
}

func TestStopPointsTool(t *testing.T) {
	body := loadTestData(t, "stop_points.json")
	mock := newMockDoer(body)

	_, handler := StopPointsTool(mock)

	req := mcp.CallToolRequest{}
	req.Params.Arguments = map[string]any{}

	result, err := handler(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if mock.lastReq.URL.String() != "https://transport.integration.sl.se/v1/stop-points" {
		t.Errorf("unexpected URL: %s", mock.lastReq.URL.String())
	}

	text := result.Content[0].(mcp.TextContent).Text
	if !strings.Contains(text, "BUSTERM") {
		t.Error("result should contain fixture data")
	}
}

func TestStopPointsTool_QueryFiltersByNameSubstring(t *testing.T) {
	body := loadTestData(t, "stop_points.json")
	mock := newMockDoer(body)

	_, handler := StopPointsTool(mock)

	req := mcp.CallToolRequest{}
	req.Params.Arguments = map[string]any{"query": "sluss"}

	result, err := handler(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	text := result.Content[0].(mcp.TextContent).Text

	var points []map[string]any
	if err := json.Unmarshal([]byte(text), &points); err != nil {
		t.Fatalf("failed to parse response: %v", err)
	}

	// Fixture: two "Slussen" platforms (A, B) + one "Slussplan" all match "sluss".
	if len(points) != 3 {
		t.Errorf("expected 3 matching stop points, got %d: %v", len(points), points)
	}
	names := map[string]int{}
	for _, p := range points {
		names[p["name"].(string)]++
	}
	if names["Slussen"] != 2 || names["Slussplan"] != 1 {
		t.Errorf("expected 2 Slussen + 1 Slussplan, got %v", names)
	}
}

func TestStopPointsTool_LimitTruncates(t *testing.T) {
	body := loadTestData(t, "stop_points.json")
	mock := newMockDoer(body)

	_, handler := StopPointsTool(mock)

	req := mcp.CallToolRequest{}
	req.Params.Arguments = map[string]any{"limit": float64(2)}

	result, err := handler(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	text := result.Content[0].(mcp.TextContent).Text

	var points []map[string]any
	if err := json.Unmarshal([]byte(text), &points); err != nil {
		t.Fatalf("failed to parse response: %v", err)
	}
	if len(points) != 2 {
		t.Errorf("expected 2 stop points after limit=2, got %d", len(points))
	}
}

func TestStopPointsTool_QueryAndLimit(t *testing.T) {
	body := loadTestData(t, "stop_points.json")
	mock := newMockDoer(body)

	_, handler := StopPointsTool(mock)

	req := mcp.CallToolRequest{}
	req.Params.Arguments = map[string]any{"query": "sluss", "limit": float64(1)}

	result, err := handler(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	text := result.Content[0].(mcp.TextContent).Text

	var points []map[string]any
	if err := json.Unmarshal([]byte(text), &points); err != nil {
		t.Fatalf("failed to parse response: %v", err)
	}
	if len(points) != 1 {
		t.Errorf("expected 1 stop point after query=sluss + limit=1, got %d", len(points))
	}
}

func TestTransportAuthoritiesTool(t *testing.T) {
	body := loadTestData(t, "transport_authorities.json")
	mock := newMockDoer(body)

	_, handler := TransportAuthoritiesTool(mock)

	req := mcp.CallToolRequest{}
	req.Params.Arguments = map[string]any{}

	result, err := handler(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if mock.lastReq.URL.String() != "https://transport.integration.sl.se/v1/transport-authorities" {
		t.Errorf("unexpected URL: %s", mock.lastReq.URL.String())
	}

	text := result.Content[0].(mcp.TextContent).Text
	if !strings.Contains(text, "Storstockholms") {
		t.Error("result should contain fixture data")
	}
}
