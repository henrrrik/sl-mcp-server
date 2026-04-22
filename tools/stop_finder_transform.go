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
	Unambiguous  bool      `json:"unambiguous,omitempty"`
}

// resolveResponse is the outer shape emitted by the resolve tool.
type resolveResponse struct {
	Best       *resolvedSite  `json:"best,omitempty"`
	Candidates []resolvedSite `json:"candidates,omitempty"`
	Query      string         `json:"query,omitempty"`
}

// resolveCandidateCap caps the candidates array so chatty upstreams can't
// flood the response. Round 2 spec: best + up to 4 runners-up.
const resolveCandidateCap = 4

// unambiguous thresholds: a clear winner scores >= 1000 AND beats the next
// stop candidate by at least 50 points. Genuine ambiguity (two pendeltåg
// stations both at 1000 quality) fails the delta check and stays flagged
// ambiguous so the caller still knows to disambiguate.
const (
	resolveUnambiguousQualityMin = 1000
	resolveUnambiguousDeltaMin   = 50
)

// isStopType reports whether an upstream stop-finder type qualifies as a
// transit stop for the purposes of resolve / trips endpoint selection.
// Shared so resolve and trips agree on what counts as a stop (POIs,
// addresses, localities, streets all fail the check).
func isStopType(typ string) bool {
	return typ == "stop"
}

// buildResolveResponse reshapes a raw /v2/stop-finder body into the resolve
// tool's shape: the single highest-quality stop as `best`, with the rest
// preserved as `candidates`. All three id forms are computed for any
// entry whose GID we can parse into a short site id.
//
// When stopOnly is true (the default for resolve), non-stop entries are
// dropped from both best and candidates. When false, non-stop entries can
// appear in candidates but never as best — callers who asked for
// "Järfälla Hyrkart" with stop_only=false still shouldn't plan trips from
// a go-kart track.
func buildResolveResponse(raw []byte, stopOnly bool) ([]byte, error) {
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
		if stopOnly && !isStopType(loc.Type) {
			continue
		}
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
		if out.Best == nil && isStopType(loc.Type) {
			best := rs
			out.Best = &best
			continue
		}
		if len(out.Candidates) >= resolveCandidateCap {
			continue
		}
		out.Candidates = append(out.Candidates, rs)
	}

	if out.Best != nil {
		out.Best.Unambiguous = isUnambiguousResolve(*out.Best, out.Candidates)
	}

	return json.Marshal(out)
}

// isUnambiguousResolve returns true when the best match scores at least
// resolveUnambiguousQualityMin AND outranks the next-best STOP candidate
// by at least resolveUnambiguousDeltaMin. Non-stop candidates don't count
// toward ambiguity — a POI that happens to share a name doesn't reduce
// confidence in the stop match.
func isUnambiguousResolve(best resolvedSite, candidates []resolvedSite) bool {
	if best.MatchQuality < resolveUnambiguousQualityMin {
		return false
	}
	nextStopQ := -1
	for _, c := range candidates {
		if !isStopType(c.Type) {
			continue
		}
		if c.MatchQuality > nextStopQ {
			nextStopQ = c.MatchQuality
		}
	}
	if nextStopQ < 0 {
		// No other stop candidates — unambiguous by elimination.
		return true
	}
	return best.MatchQuality-nextStopQ >= resolveUnambiguousDeltaMin
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
