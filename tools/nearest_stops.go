package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"sort"

	"github.com/henrrrik/sl-mcp-server/slclient"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

// NearestStopsTool reports the closest SL sites to a given coordinate,
// ordered by distance. The /v1/sites catalog is fetched once and filtered
// in-process with a haversine distance calculation — no geocoding and no
// per-request upstream coord lookup.
//
// Chains cleanly with external geocoders: hand the lat/lon of a user's
// location (or a street address you've already resolved) to this tool,
// pick a stop, then call departures / trips with the resulting site_id.
func NearestStopsTool(client slclient.HTTPDoer) (mcp.Tool, server.ToolHandlerFunc) {
	tool := mcp.NewTool("nearest_stops",
		mcp.WithDescription("Find SL transit sites nearest to a lat/lon coordinate, ordered by distance. Use to chain from a geocoder: \"what stops are near 59.407, 17.872\" → call departures/trips with the returned site_id. radius_m bounds the search (default 500 m), limit caps the result (default 5)."),
		mcp.WithNumber("lat", mcp.Required(), mcp.Description("Latitude (WGS84, e.g. 59.3311 for T-Centralen).")),
		mcp.WithNumber("lon", mcp.Required(), mcp.Description("Longitude (WGS84, e.g. 18.0593 for T-Centralen).")),
		mcp.WithNumber("radius_m", mcp.Description("Maximum distance from (lat, lon) in meters. Default 500.")),
		mcp.WithNumber("limit", mcp.Description("Maximum number of stops to return. Default 5.")),
	)

	handler := func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		args := request.GetArguments()
		lat, latOK := coerceFloat(args["lat"])
		lon, lonOK := coerceFloat(args["lon"])
		if !latOK || !lonOK {
			return mcp.NewToolResultError("lat and lon are required numeric arguments"), nil
		}

		radiusM := request.GetFloat("radius_m", defaultNearestRadiusM)
		if radiusM <= 0 {
			radiusM = defaultNearestRadiusM
		}
		limit := request.GetInt("limit", defaultNearestLimit)
		if limit <= 0 {
			limit = defaultNearestLimit
		}

		u := slclient.BuildURL(transportBase, "/v1/sites", nil)
		body, errResult := fetchJSONRaw(ctx, client, u)
		if errResult != nil {
			return errResult, nil
		}

		stops, err := nearestStops(body, lat, lon, radiusM, limit)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("failed to compute nearest stops: %v", err)), nil
		}
		out, err := json.Marshal(stops)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("failed to encode nearest_stops response: %v", err)), nil
		}
		return mcp.NewToolResultText(string(out)), nil
	}

	return tool, handler
}

const (
	defaultNearestRadiusM = 500.0
	defaultNearestLimit   = 5
	// earthRadiusM is the mean radius used for haversine. Precise enough
	// for the tens-of-metres resolution we care about.
	earthRadiusM = 6_371_000.0
)

// nearestStop is the outward shape for each result. Distance is rounded to
// the nearest metre — sub-metre precision is noise at this scale.
type nearestStop struct {
	SiteID    int     `json:"site_id"`
	Name      string  `json:"name"`
	Lat       float64 `json:"lat"`
	Lon       float64 `json:"lon"`
	DistanceM int     `json:"distance_m"`
}

// nearestStops filters and sorts the /v1/sites catalog by haversine distance
// from (lat, lon). Sites without a lat/lon are silently skipped. Sites with
// distance greater than radiusM are dropped. The result is sorted ascending
// and capped at limit.
func nearestStops(raw []byte, lat, lon, radiusM float64, limit int) ([]nearestStop, error) {
	var sites []struct {
		ID   int     `json:"id"`
		Name string  `json:"name"`
		Lat  float64 `json:"lat"`
		Lon  float64 `json:"lon"`
	}
	if err := json.Unmarshal(raw, &sites); err != nil {
		return nil, err
	}

	out := make([]nearestStop, 0)
	for _, s := range sites {
		if s.Lat == 0 && s.Lon == 0 {
			continue
		}
		d := haversineM(lat, lon, s.Lat, s.Lon)
		if d > radiusM {
			continue
		}
		out = append(out, nearestStop{
			SiteID:    s.ID,
			Name:      s.Name,
			Lat:       s.Lat,
			Lon:       s.Lon,
			DistanceM: int(math.Round(d)),
		})
	}
	sort.SliceStable(out, func(i, j int) bool {
		return out[i].DistanceM < out[j].DistanceM
	})
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

// haversineM returns the great-circle distance in metres between two
// lat/lon points (WGS84, decimal degrees).
func haversineM(lat1, lon1, lat2, lon2 float64) float64 {
	rad := math.Pi / 180.0
	dLat := (lat2 - lat1) * rad
	dLon := (lon2 - lon1) * rad
	a := math.Sin(dLat/2)*math.Sin(dLat/2) +
		math.Cos(lat1*rad)*math.Cos(lat2*rad)*
			math.Sin(dLon/2)*math.Sin(dLon/2)
	c := 2 * math.Atan2(math.Sqrt(a), math.Sqrt(1-a))
	return earthRadiusM * c
}

// coerceFloat extracts a float64 from a raw MCP argument. Accepts numbers
// (JSON numbers arrive as float64) and returns (0, false) on anything else.
func coerceFloat(v any) (float64, bool) {
	switch t := v.(type) {
	case float64:
		return t, true
	case int:
		return float64(t), true
	case int64:
		return float64(t), true
	default:
		return 0, false
	}
}
