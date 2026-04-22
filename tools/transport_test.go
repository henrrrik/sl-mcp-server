package tools

import (
	"context"
	"encoding/json"
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
	if !strings.Contains(text, "Stockholm City") {
		t.Error("result should contain fixture departure data")
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
