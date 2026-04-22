package tools

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
)

// SL's real site IDs fall in the range 102..9999. Anything accepted by
// normalizeSiteID that resolves outside this range is rejected as
// site_id_out_of_range rather than forwarded to upstream, which would
// silently return an empty response.
const (
	minSiteID = 102
	maxSiteID = 9999
)

// Site-ID normalization error codes. Kept machine-readable so downstream
// callers can branch on them without parsing prose.
const (
	errInvalidSiteIDFormat = "invalid_site_id_format"
	errSiteIDOutOfRange    = "site_id_out_of_range"
)

// 16-digit Journey Planner GID prefix (e.g. "9091001000009702" → site 9702).
// Ported from NecroKote/trafiklab-sl's tsl/utils.py::global_id_to_site_id.
const gidPrefix = "909100100"

type siteIDError struct {
	Code  string
	Input string
}

func (e *siteIDError) Error() string {
	return fmt.Sprintf("%s: %q", e.Code, e.Input)
}

// asJSON returns a stable JSON envelope suitable for surfacing through
// mcp.NewToolResultError so callers can branch on Code without prose parsing.
func (e *siteIDError) asJSON() string {
	b, _ := json.Marshal(map[string]string{
		"error": e.Code,
		"input": e.Input,
		"hint":  "Use SL:sites to enumerate canonical site IDs, or SL:stop_finder to resolve by name.",
	})
	return string(b)
}

// normalizeSiteID accepts any of the four site-id formats SL hands out and
// returns the canonical short form in the range 102..9999.
//
//   - Short form (3–4 digits, e.g. "9702") — returned as-is after range check.
//   - 8-digit 18xx form (e.g. "18009702" → 9702) — what stop_finder returns
//     in properties.stopId.
//   - 9-digit 3BA1CDEFG form (e.g. "300109001" → 9001) — documented at
//     https://support.trafiklab.se/org/trafiklabse/d/sl-olika-stationskoder-site-ids/
//   - 16-digit GID (e.g. "9091001000009702" → 9702) — what stop_finder
//     returns in its `id` field; exceeds JS Number.MAX_SAFE_INTEGER, so
//     callers must pass as a string.
//
// Error classes:
//   - invalid_site_id_format: the input wasn't a plain non-negative integer.
//   - site_id_out_of_range:   the input parsed as an integer but didn't
//     resolve to a short-form site in 102..9999.
func normalizeSiteID(input string) (int, error) {
	s := strings.TrimSpace(input)
	if s == "" {
		return 0, &siteIDError{Code: errInvalidSiteIDFormat, Input: input}
	}
	if _, err := strconv.ParseUint(s, 10, 64); err != nil {
		return 0, &siteIDError{Code: errInvalidSiteIDFormat, Input: input}
	}

	short, ok := extractShort(s)
	if !ok || short < minSiteID || short > maxSiteID {
		return 0, &siteIDError{Code: errSiteIDOutOfRange, Input: input}
	}
	return short, nil
}

// extractShort returns the short-form site id if s matches one of the four
// recognized numeric shapes, otherwise (0, false). Does NOT range-check —
// that's the caller's job so "101" and "42" both fall through to the same
// out_of_range error class.
func extractShort(s string) (int, bool) {
	switch len(s) {
	case 3, 4:
		n, _ := strconv.Atoi(s)
		return n, true
	case 8:
		rest, ok := strings.CutPrefix(s, "180")
		if !ok {
			return 0, false
		}
		return tailToInt(rest), true
	case 9:
		return tailToInt(s[len(s)-5:]), true
	case 16:
		rest, ok := strings.CutPrefix(s, gidPrefix)
		if !ok {
			return 0, false
		}
		return tailToInt(rest), true
	default:
		return 0, false
	}
}

// tailToInt strips leading zeros and returns the remaining digits as int.
// An all-zero tail returns 0, which the caller's range check rejects as
// out_of_range.
func tailToInt(s string) int {
	trimmed := strings.TrimLeft(s, "0")
	if trimmed == "" {
		return 0
	}
	n, _ := strconv.Atoi(trimmed)
	return n
}

// Number.MAX_SAFE_INTEGER in IEEE-754 double precision — any JSON number
// above this can lose precision in transit. A 16-digit GID like
// 9091001000009702 (~9.09e15) exceeds this, so callers must pass GIDs
// as strings to avoid silent corruption.
const maxSafeJSONNumber = float64(1 << 53)

// coerceSiteIDArg normalizes a raw MCP argument into the string form that
// normalizeSiteID expects. Accepts strings directly, and integers-as-numbers
// (JSON numbers arrive as float64) when they fit safely in a JSON double.
// Returns "" when the input is missing, non-integral, or large enough to
// have lost precision — callers should surface those as invalid_site_id_format.
func coerceSiteIDArg(v any) string {
	switch t := v.(type) {
	case string:
		return strings.TrimSpace(t)
	case float64:
		if t < 0 || t > maxSafeJSONNumber || t != float64(int64(t)) {
			return ""
		}
		return strconv.FormatInt(int64(t), 10)
	case int:
		return strconv.Itoa(t)
	case int64:
		return strconv.FormatInt(t, 10)
	default:
		return ""
	}
}
