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
	mock := newMockDoer(body)

	_, handler := DeparturesTool(mock)

	req := mcp.CallToolRequest{}
	req.Params.Arguments = map[string]any{
		"site_id": float64(9192),
	}

	result, err := handler(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !strings.Contains(mock.lastReq.URL.String(), "/v1/sites/9192/departures") {
		t.Errorf("unexpected URL: %s", mock.lastReq.URL.String())
	}

	text := result.Content[0].(mcp.TextContent).Text
	if !strings.Contains(text, "Kungsträdgården") {
		t.Error("result should contain fixture data")
	}
}

func TestDeparturesTool_StripsPrefixedID(t *testing.T) {
	// 18009192 is the zero-padded "180" + shortId form returned by stop-finder;
	// the departures endpoint only accepts the short id (9192).
	body := loadTestData(t, "departures.json")
	mock := newMockDoer(body)

	_, handler := DeparturesTool(mock)

	req := mcp.CallToolRequest{}
	req.Params.Arguments = map[string]any{
		"site_id": float64(18009192),
	}

	_, err := handler(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !strings.Contains(mock.lastReq.URL.String(), "/v1/sites/9192/departures") {
		t.Errorf("expected /v1/sites/9192/departures after normalizing 18009192, got %s", mock.lastReq.URL.String())
	}
}

func TestDeparturesTool_StripsWishlistPrefixedID(t *testing.T) {
	// 1809001 is the non-zero-padded form (180 + 9001) that a caller might
	// construct by hand; accept it too.
	body := loadTestData(t, "departures.json")
	mock := newMockDoer(body)

	_, handler := DeparturesTool(mock)

	req := mcp.CallToolRequest{}
	req.Params.Arguments = map[string]any{
		"site_id": float64(1809001),
	}

	_, err := handler(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !strings.Contains(mock.lastReq.URL.String(), "/v1/sites/9001/departures") {
		t.Errorf("expected /v1/sites/9001/departures after normalizing 1809001, got %s", mock.lastReq.URL.String())
	}
}

func TestDeparturesTool_PreservesLow180IDs(t *testing.T) {
	// Real sites exist in the 1800-1809 range (e.g. 1809 = Söndagsvägen).
	// These must pass through unchanged — normalization only applies to
	// IDs outside the real short-id range (> 9999).
	body := loadTestData(t, "departures.json")
	mock := newMockDoer(body)

	_, handler := DeparturesTool(mock)

	req := mcp.CallToolRequest{}
	req.Params.Arguments = map[string]any{
		"site_id": float64(1809),
	}

	_, err := handler(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !strings.Contains(mock.lastReq.URL.String(), "/v1/sites/1809/departures") {
		t.Errorf("expected /v1/sites/1809/departures unchanged, got %s", mock.lastReq.URL.String())
	}
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

	if mock.lastReq.URL.String() != "https://transport.integration.sl.se/v1/lines" {
		t.Errorf("unexpected URL: %s", mock.lastReq.URL.String())
	}

	text := result.Content[0].(mcp.TextContent).Text
	if !strings.Contains(text, "blåbuss") {
		t.Error("result should contain fixture data")
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
