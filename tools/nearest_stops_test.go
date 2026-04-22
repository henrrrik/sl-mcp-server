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

// Round 2, Section 6: each result carries short_id, gid_16, coord as
// [lat, lon], and locality (from the sites catalog's `note` field).
func TestNearestStopsTool_OutputShape(t *testing.T) {
	body := loadTestData(t, "sites.json")
	mock := newMockDoer(body)

	_, handler := NearestStopsTool(mock)

	req := mcp.CallToolRequest{}
	req.Params.Arguments = map[string]any{
		"lat":      float64(59.3311),
		"lon":      float64(18.0593),
		"radius_m": float64(5000),
	}

	result, _ := handler(context.Background(), req)
	text := result.Content[0].(mcp.TextContent).Text

	var out []map[string]any
	_ = json.Unmarshal([]byte(text), &out)
	if len(out) == 0 {
		t.Fatal("expected at least one stop")
	}
	first := out[0]

	// Required fields in the round-2 shape.
	for _, k := range []string{"short_id", "gid_16", "name", "coord", "distance_m"} {
		if _, ok := first[k]; !ok {
			t.Errorf("missing field %q in result: %+v", k, first)
		}
	}

	// Old field names should be gone — we renamed them explicitly.
	for _, forbidden := range []string{"site_id", "lat", "lon"} {
		if _, ok := first[forbidden]; ok {
			t.Errorf("old field %q should be removed, got %+v", forbidden, first)
		}
	}

	// gid_16 should be the 16-digit GID for the T-Centralen short id 9001.
	if g, _ := first["gid_16"].(string); g != "9091001000009001" {
		t.Errorf("expected gid_16=9091001000009001, got %q", g)
	}

	// coord should be [lat, lon] as a float array.
	coord, _ := first["coord"].([]any)
	if len(coord) != 2 {
		t.Errorf("expected coord as [lat, lon] array, got %+v", coord)
	}
}

// Round 2, Section 6: locality comes from the sites catalog's `note`
// field. The fixture tags Aska with note=Södertälje.
func TestNearestStopsTool_LocalityFromNote(t *testing.T) {
	body := loadTestData(t, "sites.json")
	mock := newMockDoer(body)

	_, handler := NearestStopsTool(mock)

	req := mcp.CallToolRequest{}
	// Aska, Södertälje: lat 59.2538, lon 17.4567
	req.Params.Arguments = map[string]any{
		"lat":      float64(59.2538),
		"lon":      float64(17.4567),
		"radius_m": float64(1000),
	}

	result, _ := handler(context.Background(), req)
	text := result.Content[0].(mcp.TextContent).Text

	var out []struct {
		Name     string `json:"name"`
		Locality string `json:"locality"`
	}
	_ = json.Unmarshal([]byte(text), &out)
	if len(out) == 0 {
		t.Fatal("expected Aska within 1 km of its own coords")
	}
	if out[0].Name != "Aska" {
		t.Errorf("expected Aska as top hit, got %q", out[0].Name)
	}
	if out[0].Locality != "Södertälje" {
		t.Errorf("expected locality=Södertälje from fixture note, got %q", out[0].Locality)
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
