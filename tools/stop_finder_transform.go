package tools

import (
	"encoding/json"
)

// resolvedSite is the canonical disambiguated site shape: one struct that
// carries every id form a caller might need, plus the upstream's geocoding
// metadata. Returned as `best` by the resolve tool and as elements of
// `candidates` for runners-up.
type resolvedSite struct {
	Name         string    `json:"name,omitempty"`
	Locality     string    `json:"locality,omitempty"`
	SiteID       int       `json:"short_id,omitempty"`
	GID180       string    `json:"gid_180,omitempty"`
	GID16        string    `json:"gid_16,omitempty"`
	Type         string    `json:"type,omitempty"`
	Coord        []float64 `json:"coord,omitempty"`
	MatchQuality int       `json:"match_quality,omitempty"`
}

// resolveResponse is the outer shape emitted by the resolve tool.
type resolveResponse struct {
	Best       *resolvedSite  `json:"best,omitempty"`
	Candidates []resolvedSite `json:"candidates,omitempty"`
	Query      string         `json:"query,omitempty"`
}

// buildResolveResponse reshapes a raw /v2/stop-finder body into the resolve
// tool's shape: the single highest-quality stop as `best`, with the rest
// (stops and non-stops alike) preserved as `candidates`. All three id
// forms are computed for any entry whose GID we can parse into a short
// site id.
func buildResolveResponse(raw []byte) ([]byte, error) {
	var env struct {
		Locations []struct {
			ID               string    `json:"id"`
			Name             string    `json:"name"`
			DisassembledName string    `json:"disassembledName"`
			Coord            []float64 `json:"coord"`
			MatchQuality     int       `json:"matchQuality"`
			Type             string    `json:"type"`
			Parent           struct {
				Name string `json:"name"`
			} `json:"parent"`
			Properties struct {
				StopID string `json:"stopId"`
			} `json:"properties"`
		} `json:"locations"`
	}
	if err := json.Unmarshal(raw, &env); err != nil {
		return nil, err
	}

	out := resolveResponse{}
	for _, loc := range env.Locations {
		name := loc.DisassembledName
		if name == "" {
			name = loc.Name
		}
		rs := resolvedSite{
			Name:         name,
			Locality:     loc.Parent.Name,
			Type:         loc.Type,
			Coord:        loc.Coord,
			MatchQuality: loc.MatchQuality,
		}
		if short, ok := siteIDFromStopFinderEntry(loc.ID, loc.Properties.StopID); ok {
			rs.SiteID = short
			rs.GID16 = siteIDToGID(short)
			rs.GID180 = siteIDTo180(short)
		}
		if out.Best == nil && rs.Type == "stop" {
			best := rs
			out.Best = &best
			continue
		}
		out.Candidates = append(out.Candidates, rs)
	}

	return json.Marshal(out)
}

// siteIDFromStopFinderEntry returns the short-form site id for a
// stop-finder entry. Tries the 16-digit GID first, falling back to the
// 8-digit 18xx stopId in properties. Returns (0, false) when neither
// parses to a valid short-form site.
func siteIDFromStopFinderEntry(gid, stopID string) (int, bool) {
	if gid != "" {
		if short, err := normalizeSiteID(gid); err == nil {
			return short, true
		}
	}
	if stopID != "" {
		if short, err := normalizeSiteID(stopID); err == nil {
			return short, true
		}
	}
	return 0, false
}

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
