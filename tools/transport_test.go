package tools

import (
	"context"
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
