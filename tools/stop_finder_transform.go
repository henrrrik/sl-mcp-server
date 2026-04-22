package tools

import "encoding/json"

// trimStopFinder reshapes the Journey Planner /v2/stop-finder response into
// a flat array of the fields an LLM consumer actually needs, matching the
// shape sites returns. Drops the {locations, systemMessages} wrapper and
// the noisy per-entry fields (disassembledName, isBest, isGlobalId, parent,
// productClasses, properties) that don't help downstream tools.
//
// The id is preserved verbatim as the upstream 16-digit GID string; the
// departures tool accepts that form directly via its site_id normalizer,
// so no pre-normalization is needed here. Non-stop entries (type != "stop")
// are kept so callers can still resolve addresses/POIs for trip planning.
func trimStopFinder(raw []byte) ([]byte, error) {
	var env struct {
		Locations []json.RawMessage `json:"locations"`
	}
	if err := json.Unmarshal(raw, &env); err != nil {
		return nil, err
	}

	trimmed := make([]trimmedLocation, 0, len(env.Locations))
	for _, loc := range env.Locations {
		var src struct {
			ID           string    `json:"id"`
			Name         string    `json:"name"`
			Coord        []float64 `json:"coord"`
			MatchQuality int       `json:"matchQuality"`
			Type         string    `json:"type"`
		}
		if err := json.Unmarshal(loc, &src); err != nil {
			continue
		}
		out := trimmedLocation{
			ID:           src.ID,
			Name:         src.Name,
			MatchQuality: src.MatchQuality,
			Type:         src.Type,
		}
		if len(src.Coord) >= 2 {
			out.Lat = src.Coord[0]
			out.Lon = src.Coord[1]
		}
		trimmed = append(trimmed, out)
	}
	return json.Marshal(trimmed)
}

type trimmedLocation struct {
	ID           string  `json:"id,omitempty"`
	Name         string  `json:"name,omitempty"`
	Lat          float64 `json:"lat,omitempty"`
	Lon          float64 `json:"lon,omitempty"`
	MatchQuality int     `json:"match_quality,omitempty"`
	Type         string  `json:"type,omitempty"`
}
