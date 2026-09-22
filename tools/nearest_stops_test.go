package tools

import (
	"context"
	"encoding/json"
	"math"
	"strings"
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

// lat/lon must accept the same string forms as radius_m and limit do.
func TestNearestStopsTool_AcceptsStringCoordinates(t *testing.T) {
	_, handler := NearestStopsTool(newMockDoer(loadTestData(t, "sites.json")))
	req := mcp.CallToolRequest{}
	req.Params.Arguments = map[string]any{"lat": "59.3186", "lon": "18.0716", "radius_m": "800"}
	result, err := handler(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	if result.IsError {
		t.Fatalf("string coordinates should be accepted, got %s", result.Content[0].(mcp.TextContent).Text)
	}
	var out []any
	_ = json.Unmarshal([]byte(result.Content[0].(mcp.TextContent).Text), &out)
	if len(out) == 0 {
		t.Errorf("expected at least one stop near Slussen")
	}
}

// limit=0 means unlimited, as it does for every other tool.
func TestNearestStopsTool_LimitZeroIsUnlimited(t *testing.T) {
	sitesJSON := loadTestData(t, "sites.json")
	var all []any
	_ = json.Unmarshal([]byte(sitesJSON), &all)

	_, handler := NearestStopsTool(newMockDoer(sitesJSON))
	req := mcp.CallToolRequest{}
	req.Params.Arguments = map[string]any{"lat": 59.33, "lon": 18.06, "radius_m": 1e7, "limit": 0}
	result, _ := handler(context.Background(), req)
	var out []any
	_ = json.Unmarshal([]byte(result.Content[0].(mcp.TextContent).Text), &out)
	if len(out) != len(all) {
		t.Errorf("limit=0 with a planet-sized radius should return every site (%d), got %d", len(all), len(out))
	}
}

// Coordinates that can't match an SL stop must be an error, not an empty
// result. Swapped lat/lon is the classic mistake — and a swapped Stockholm
// pair is still valid WGS84, so a plain range check can't catch it; the
// service-area check can, and the hint says so.
func TestNearestStopsTool_RejectsOutOfRangeCoordinates(t *testing.T) {
	_, handler := NearestStopsTool(newMockDoer(loadTestData(t, "sites.json")))
	req := mcp.CallToolRequest{}
	req.Params.Arguments = map[string]any{"lat": 18.0593, "lon": 59.3311}
	result, _ := handler(context.Background(), req)
	if !result.IsError {
		t.Fatalf("expected an error for lat=18.06 lon=59.33, got %s", result.Content[0].(mcp.TextContent).Text)
	}
	text := result.Content[0].(mcp.TextContent).Text
	if !strings.Contains(text, `"error":"invalid_coordinates"`) || !strings.Contains(strings.ToLower(text), "swapped") {
		t.Errorf("expected invalid_coordinates with a swapped-coordinates hint, got %s", text)
	}

	req.Params.Arguments = map[string]any{"lat": 59.33, "lon": 181}
	result, _ = handler(context.Background(), req)
	if !result.IsError || !strings.Contains(result.Content[0].(mcp.TextContent).Text, `"error":"invalid_coordinates"`) {
		t.Errorf("expected invalid_coordinates for lon=181")
	}

	// Gothenburg: valid WGS84, not swapped, but not SL territory either.
	req.Params.Arguments = map[string]any{"lat": 57.71, "lon": 11.97}
	result, _ = handler(context.Background(), req)
	text = result.Content[0].(mcp.TextContent).Text
	if !result.IsError || !strings.Contains(text, "service area") || strings.Contains(strings.ToLower(text), "swapped") {
		t.Errorf("expected an out-of-service-area error without the swapped hint, got %s", text)
	}
}
