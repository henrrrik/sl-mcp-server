package tools

import (
	"context"
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
		"origin":      "Slussen",
		"destination": "T-Centralen",
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
	if !strings.Contains(text, "T-Centralen") {
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
