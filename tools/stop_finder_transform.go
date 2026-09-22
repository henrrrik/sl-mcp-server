package tools

import (
	"encoding/json"

	"github.com/mark3labs/mcp-go/mcp"
)

// errStopFinder is the code for a /v2/stop-finder reply that carries a
// broker error instead of locations.
const errStopFinder = "stop_finder_error"

// stopFinderBrokerError returns a structured error result when the body
// has no locations and a systemMessages entry of type "error". Both
// stop_finder and resolve decoded only `locations`, so such a reply
// collapsed to an empty result. Returns nil for normal (even empty)
// replies.
func stopFinderBrokerError(raw []byte) *mcp.CallToolResult {
	var env struct {
		Locations      []json.RawMessage `json:"locations"`
		SystemMessages []struct {
			Type   string `json:"type"`
			Module string `json:"module"`
			Code   int    `json:"code"`
			Text   string `json:"text"`
		} `json:"systemMessages"`
	}
	if err := json.Unmarshal(raw, &env); err != nil || len(env.Locations) > 0 {
		return nil
	}
	for _, m := range env.SystemMessages {
		if m.Type != "error" {
			continue
		}
		b, _ := json.Marshal(map[string]any{
			"error":   errStopFinder,
			"module":  m.Module,
			"code":    m.Code,
			"message": m.Text,
		})
		return mcp.NewToolResultError(string(b))
	}
	return nil
}

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

// unambiguous thresholds: a clear winner scores >= 1000 AND either beats
// the next stop candidate by at least 50 points or is the only one of the
// two named exactly as the query. Genuine ambiguity (two pendeltåg stations
// both at 1000 quality, neither the literal query) fails both checks and
// stays flagged so the caller still knows to disambiguate.
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
// tool's shape: the top-ranked stop as `best`, with the rest preserved as
// `candidates`. Results are ranked by matchQuality with an exact-name
// tie-break on query, since upstream does not guarantee quality order. All
// three id forms are computed for any entry whose GID we can parse into a
// short site id.
//
// When stopOnly is true (the default for resolve), non-stop entries are
// dropped from both best and candidates. When false, non-stop entries can
// appear in candidates but never as best — callers who asked for
// "Järfälla Hyrkart" with stop_only=false still shouldn't plan trips from
// a go-kart track.
func buildResolveResponse(raw []byte, query string, stopOnly bool) ([]byte, error) {
	sites, err := decodeStopFinderSites(raw)
	if err != nil {
		return nil, err
	}
	rankByQuality(sites, query,
		func(s resolvedSite) int { return s.MatchQuality },
		func(s resolvedSite) string { return s.Name })

	out := resolveResponse{}
	for _, rs := range sites {
		if stopOnly && !isStopType(rs.Type) {
			continue
		}
		if out.Best == nil && isStopType(rs.Type) {
			best := rs
			out.Best = &best
			continue
		}
		out.Candidates = append(out.Candidates, rs)
	}

	// Judge ambiguity against every candidate, then cap the payload — a
	// tied stop must not become invisible just because it ranked fifth.
	if out.Best != nil {
		out.Best.Unambiguous = isUnambiguousResolve(*out.Best, out.Candidates, query)
	}
	if len(out.Candidates) > resolveCandidateCap {
		out.Candidates = out.Candidates[:resolveCandidateCap]
	}
	return json.Marshal(out)
}

// decodeStopFinderSites decodes the upstream locations into resolvedSite
// values, computing every id form for entries with a parseable site GID.
func decodeStopFinderSites(raw []byte) ([]resolvedSite, error) {
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

	sites := make([]resolvedSite, 0, len(env.Locations))
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
		sites = append(sites, rs)
	}
	return sites, nil
}

// isUnambiguousResolve returns true when the best match scores at least
// resolveUnambiguousQualityMin AND either outranks the next-best STOP
// candidate by resolveUnambiguousDeltaMin or is the only one of the two
// named exactly as the query. Candidates arrive ranked, so the first
// stop-typed one is the strongest rival. Non-stop candidates don't count
// toward ambiguity — a POI that happens to share a name doesn't reduce
// confidence in the stop match.
func isUnambiguousResolve(best resolvedSite, candidates []resolvedSite, query string) bool {
	if best.MatchQuality < resolveUnambiguousQualityMin {
		return false
	}
	for _, c := range candidates {
		if !isStopType(c.Type) {
			continue
		}
		if best.MatchQuality-c.MatchQuality >= resolveUnambiguousDeltaMin {
			return true
		}
		return nameMatchesQuery(best.Name, query) && !nameMatchesQuery(c.Name, query)
	}
	// No other stop candidates — unambiguous by elimination.
	return true
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
// Results are ranked by match_quality since upstream doesn't guarantee it.
func trimStopFinder(raw []byte, query string) ([]byte, error) {
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
	rankByQuality(trimmed, query,
		func(l trimmedLocation) int { return l.MatchQuality },
		func(l trimmedLocation) string { return l.Name })
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
