package tools

import (
	"context"
	"encoding/json"
	"math"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"
)

// P3: near (59.3311, 18.0593) — T-Centralen's own coords — the top hit
// must be T-Centralen at ~0 m.
func TestNearestStopsTool_TopHitIsClosestByHaversine(t *testing.T) {
	body := loadTestData(t, "sites.json")
	mock := newMockDoer(body)

	_, handler := NearestStopsTool(mock)

	req := mcp.CallToolRequest{}
	req.Params.Arguments = map[string]any{
		"lat":      float64(59.3311),
		"lon":      float64(18.0593),
		"radius_m": float64(5000),
	}

	result, err := handler(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsError {
		t.Fatalf("unexpected error result: %s", result.Content[0].(mcp.TextContent).Text)
	}

	var out []nearestStop
	_ = json.Unmarshal([]byte(result.Content[0].(mcp.TextContent).Text), &out)
	if len(out) == 0 {
		t.Fatal("expected at least one stop")
	}
	if out[0].Name != "T-Centralen" {
		t.Errorf("expected T-Centralen as top hit, got %q", out[0].Name)
	}
	if out[0].DistanceM > 10 {
		t.Errorf("expected T-Centralen distance near 0 m, got %d", out[0].DistanceM)
	}
}

// P3: radius_m bounds the search. A tight 50 m ring around T-Centralen's
// own coords should return only T-Centralen (other fixture sites are km
// away).
func TestNearestStopsTool_RadiusExcludesFarStops(t *testing.T) {
	body := loadTestData(t, "sites.json")
	mock := newMockDoer(body)

	_, handler := NearestStopsTool(mock)

	req := mcp.CallToolRequest{}
	req.Params.Arguments = map[string]any{
		"lat":      float64(59.3311),
		"lon":      float64(18.0593),
		"radius_m": float64(50),
	}

	result, _ := handler(context.Background(), req)
	var out []nearestStop
	_ = json.Unmarshal([]byte(result.Content[0].(mcp.TextContent).Text), &out)
	if len(out) != 1 {
		t.Fatalf("expected 1 stop within 50 m of T-Centralen, got %d", len(out))
	}
	if out[0].Name != "T-Centralen" {
		t.Errorf("expected T-Centralen, got %q", out[0].Name)
	}
}

// P3: results are sorted by distance. Pick a point equidistant-ish and
// verify the returned order is monotonically increasing.
func TestNearestStopsTool_SortsByDistance(t *testing.T) {
	body := loadTestData(t, "sites.json")
	mock := newMockDoer(body)

	_, handler := NearestStopsTool(mock)

	req := mcp.CallToolRequest{}
	req.Params.Arguments = map[string]any{
		"lat":      float64(59.3200),
		"lon":      float64(18.0720),
		"radius_m": float64(10_000),
	}

	result, _ := handler(context.Background(), req)
	var out []nearestStop
	_ = json.Unmarshal([]byte(result.Content[0].(mcp.TextContent).Text), &out)
	for i := 1; i < len(out); i++ {
		if out[i].DistanceM < out[i-1].DistanceM {
			t.Errorf("result[%d].DistanceM (%d) < result[%d].DistanceM (%d); not sorted",
				i, out[i].DistanceM, i-1, out[i-1].DistanceM)
		}
	}
}

// P3: limit truncates the result.
func TestNearestStopsTool_LimitTruncates(t *testing.T) {
	body := loadTestData(t, "sites.json")
	mock := newMockDoer(body)

	_, handler := NearestStopsTool(mock)

	req := mcp.CallToolRequest{}
	req.Params.Arguments = map[string]any{
		"lat":      float64(59.3200),
		"lon":      float64(18.0720),
		"radius_m": float64(100_000),
		"limit":    float64(2),
	}

	result, _ := handler(context.Background(), req)
	var out []nearestStop
	_ = json.Unmarshal([]byte(result.Content[0].(mcp.TextContent).Text), &out)
	if len(out) != 2 {
		t.Errorf("expected 2 results after limit=2, got %d", len(out))
	}
}

// P3: missing lat / lon returns an error (both are required).
func TestNearestStopsTool_MissingCoords(t *testing.T) {
	mock := newMockDoer("[]")
	_, handler := NearestStopsTool(mock)

	req := mcp.CallToolRequest{}
	req.Params.Arguments = map[string]any{"lat": float64(59.3)}
	result, _ := handler(context.Background(), req)
	if !result.IsError {
		t.Error("expected error result when lon is missing")
	}
}

// P3: haversine unit test — Stockholm to Oslo is ~416 km along the great
// circle. Check we're in the ballpark (within 5%).
func TestHaversineM(t *testing.T) {
	// Stockholm (59.3293, 18.0686) → Oslo (59.9139, 10.7522) ≈ 416 km.
	d := haversineM(59.3293, 18.0686, 59.9139, 10.7522)
	expected := 416_000.0
	if math.Abs(d-expected)/expected > 0.05 {
		t.Errorf("expected ≈%v m Stockholm→Oslo, got %v", expected, d)
	}
}
