package tools

import (
	"encoding/json"
	"errors"
	"strings"
	"time"
)

// departuresFilters is applied after the upstream fetch — SL's /v1/sites/:id/departures
// doesn't accept these as query params, so we filter the returned array
// in-process. Empty / zero fields mean "no restriction".
type departuresFilters struct {
	transportMode string // lowercased: matches line.transport_mode case-insensitively
	line          string // lowercased: exact match on line.designation
	directionCode int    // 0 = all, 1 / 2 = the upstream's direction codes
	limit         int    // 0 = no truncation; applied after stop_deviations are derived
}

// trimDepartures reshapes a /v1/sites/{id}/departures response for a specific
// site. The upstream's stop_deviations selection is unreliable — it attaches
// deviations based on shared lines rather than actual stop-area membership,
// yielding both false positives (Kungsträdgården notices on a T-Centralen
// query) and false negatives (missing a deviation whose scope.stop_areas
// literally names the queried site).
//
// When msgsBody is non-nil, stop_deviations is rebuilt from that /v1/messages
// snapshot — keeping only entries whose scope intersects the site's own stop
// areas, stop points, or lines (as observed in departures[]) and whose
// publish window is currently active.
//
// When msgsBody is nil (messages fetch failed), trimDepartures falls back to
// filtering upstream's stop_deviations using the same intersection rule —
// false positives still drop, but missing deviations stay missing.
//
// Either way, scope.stop_points is dropped (noisy, not useful after the site
// has been determined) and broken "null/..." href values are stripped.
//
// The `filters` argument applies transport_mode / line / direction_code /
// limit to the departures array before stop_deviations are re-derived.
// Zero-valued fields are ignored — the default is "no filtering".
//
// When verbose=false, the departures array is also slimmed — per-departure
// stop_area is dropped (every row belongs to the queried site, so it's
// redundant), journey internals are dropped, stop_point is reduced to just
// its designation, and line is flattened to {designation, transport_mode,
// group_of_lines}. When verbose=true, those fields are preserved verbatim.
func trimDepartures(depBody, msgsBody []byte, filters departuresFilters, verbose bool) ([]byte, error) {
	var root map[string]any
	if err := json.Unmarshal(depBody, &root); err != nil {
		return nil, err
	}
	// A JSON `null` body decodes to a nil map without error; assigning
	// stop_deviations into it below would panic.
	if root == nil {
		return nil, errors.New("upstream departures body was null")
	}

	all, hasDeps := root["departures"].([]any)
	filtered := filterDepartures(all, filters)
	if hasDeps {
		root["departures"] = filtered
	}

	// Derive the site identity AFTER filtering (so a line=40 query doesn't
	// carry a line-43 notice) but BEFORE the page-size limit, so which
	// disruptions are shown never depends on how many rows fit on a page.
	site := collectSiteIdentity(root)
	root["stop_deviations"] = deriveStopDeviations(root, msgsBody, site, len(all) == 0)

	if hasDeps {
		root["departures"] = truncateDepartures(filtered, filters.limit)
	}

	if !verbose {
		if deps, ok := root["departures"].([]any); ok {
			root["departures"] = slimDepartures(deps)
		}
	}
	return json.Marshal(root)
}

// deriveStopDeviations rebuilds stop_deviations for the site: from the
// /v1/messages snapshot when available, otherwise from upstream's own list
// filtered by the same intersection rule.
//
// noUpcoming is the case where upstream returned no departures at all (as
// opposed to filters removing them). The site's stop areas and lines can't
// be inferred then, so nothing could intersect and the result would always
// be empty — exactly when a "stop closed" notice matters most. Upstream's
// own list is site-scoped per SL, so it is returned as the best available
// answer.
func deriveStopDeviations(root map[string]any, msgsBody []byte, site siteIdentity, noUpcoming bool) []any {
	var stopDeviations []any
	switch {
	case noUpcoming:
		stopDeviations, _ = root["stop_deviations"].([]any)
		if stopDeviations == nil {
			stopDeviations = []any{}
		}
	case msgsBody != nil:
		if rederived, err := filterMessagesForSite(msgsBody, site, time.Now()); err == nil {
			stopDeviations = rederived
		}
	}
	if stopDeviations == nil {
		stopDeviations = filterUpstreamStopDeviations(root, site)
	}
	stripStopDeviationNoise(stopDeviations)
	return stopDeviations
}

// filterDepartures keeps only the departures that match every active
// filter (transport_mode / line / direction_code; limit is applied
// separately by truncateDepartures). Malformed rows pass through unchanged
// so one bad upstream row doesn't sneak past (conservative — we'd rather
// show a noisy row than silently drop a departure).
func filterDepartures(deps []any, f departuresFilters) []any {
	if f.transportMode == "" && f.line == "" && f.directionCode == 0 {
		return deps
	}
	out := make([]any, 0, len(deps))
	for _, depAny := range deps {
		dep, ok := depAny.(map[string]any)
		if !ok {
			out = append(out, depAny)
			continue
		}
		if !departureMatches(dep, f) {
			continue
		}
		out = append(out, dep)
	}
	return out
}

// truncateDepartures applies the page-size limit; 0 means no truncation.
func truncateDepartures(deps []any, limit int) []any {
	if limit > 0 && len(deps) > limit {
		return deps[:limit]
	}
	return deps
}

func departureMatches(dep map[string]any, f departuresFilters) bool {
	line, _ := dep["line"].(map[string]any)

	if f.transportMode != "" {
		mode, _ := line["transport_mode"].(string)
		if !strings.EqualFold(mode, f.transportMode) {
			return false
		}
	}
	if f.line != "" {
		designation, _ := line["designation"].(string)
		// Prefix match so "43" includes pendeltåg 43 AND 43X, and "54"
		// includes the 54x bus family. Case-insensitive on both sides.
		if !strings.HasPrefix(strings.ToLower(designation), strings.ToLower(f.line)) {
			return false
		}
	}
	if f.directionCode != 0 {
		if dc, _ := dep["direction_code"].(float64); int(dc) != f.directionCode {
			return false
		}
	}
	return true
}

// slimDepartures drops per-row redundancy from the departures array. Every
// row belongs to the queried site, so stop_area is uniform across the array
// and adds nothing. journey.* carries internal upstream state that callers
// don't need. line.* keeps only the human-readable fields; stop_point is
// reduced to its designation (the track/platform number).
func slimDepartures(deps []any) []any {
	out := make([]any, 0, len(deps))
	for _, depAny := range deps {
		dep, ok := depAny.(map[string]any)
		if !ok {
			out = append(out, depAny)
			continue
		}
		delete(dep, "stop_area")
		delete(dep, "journey")
		if sp, ok := dep["stop_point"].(map[string]any); ok {
			dep["stop_point"] = map[string]any{"designation": sp["designation"]}
		}
		if line, ok := dep["line"].(map[string]any); ok {
			dep["line"] = map[string]any{
				"designation":    line["designation"],
				"transport_mode": line["transport_mode"],
				"group_of_lines": line["group_of_lines"],
			}
		}
		out = append(out, dep)
	}
	return out
}

// siteIdentity is the set of identifiers that define "this site" for the
// purposes of matching deviations: every stop_area, stop_point, and line seen
// in an upcoming departure.
type siteIdentity struct {
	stopAreaIDs  map[int]bool
	stopPointIDs map[int]bool
	lineIDs      map[int]bool
}

func newSiteIdentity() siteIdentity {
	return siteIdentity{
		stopAreaIDs:  map[int]bool{},
		stopPointIDs: map[int]bool{},
		lineIDs:      map[int]bool{},
	}
}

// collectSiteIdentity walks the departures[] array and records every
// stop_area/stop_point/line ID. These are the anchors we'll test deviations
// against.
func collectSiteIdentity(root map[string]any) siteIdentity {
	s := newSiteIdentity()
	deps, ok := root["departures"].([]any)
	if !ok {
		return s
	}
	for _, depAny := range deps {
		dep, ok := depAny.(map[string]any)
		if !ok {
			continue
		}
		if id := extractID(dep["stop_area"]); id != 0 {
			s.stopAreaIDs[id] = true
		}
		if id := extractID(dep["stop_point"]); id != 0 {
			s.stopPointIDs[id] = true
		}
		if id := extractID(dep["line"]); id != 0 {
			s.lineIDs[id] = true
		}
	}
	return s
}

// extractID pulls a numeric id out of a {"id": ...} object, accepting both
// json.Number-decoded floats (the default) and already-int values.
func extractID(v any) int {
	m, ok := v.(map[string]any)
	if !ok {
		return 0
	}
	switch id := m["id"].(type) {
	case float64:
		return int(id)
	case int:
		return id
	}
	return 0
}

// scopeMatchesSite applies the precedence rule: stop-level scope (stop_areas
// or stop_points) must intersect the site if present; only fall back to line
// matching when the deviation has no stop-level scope at all.
func scopeMatchesSite(scope map[string]any, site siteIdentity) bool {
	stopAreas, _ := scope["stop_areas"].([]any)
	stopPoints, _ := scope["stop_points"].([]any)
	if len(stopAreas) > 0 || len(stopPoints) > 0 {
		return anyIDIn(stopAreas, site.stopAreaIDs) || anyIDIn(stopPoints, site.stopPointIDs)
	}
	if lines, _ := scope["lines"].([]any); len(lines) > 0 {
		return anyIDIn(lines, site.lineIDs)
	}
	// No scope at all — network-wide notice, keep.
	return true
}

// anyIDIn reports whether any entry's numeric id is in set.
func anyIDIn(entries []any, set map[int]bool) bool {
	for _, e := range entries {
		if id := extractID(e); id != 0 && set[id] {
			return true
		}
	}
	return false
}

// filterMessagesForSite decodes a /v1/messages snapshot and returns the
// subset that's scope-relevant for the given site AND active at `at`. Entries
// are reshaped into the stop_deviation shape { id, message, header, scope,
// publish } that downstream consumers already understand.
func filterMessagesForSite(msgsBody []byte, site siteIdentity, at time.Time) ([]any, error) {
	var msgs []map[string]any
	if err := json.Unmarshal(msgsBody, &msgs); err != nil {
		return nil, err
	}

	out := make([]any, 0)
	for _, m := range msgs {
		scope, _ := m["scope"].(map[string]any)
		if scope == nil || !scopeMatchesSite(scope, site) || !messageActiveAt(m, at) {
			continue
		}
		out = append(out, stopDeviationEntry(m, scope))
	}
	return out, nil
}

// stopDeviationEntry reshapes one /v1/messages entry into the
// { id, message, header, scope, publish } shape downstream consumers expect.
func stopDeviationEntry(m, scope map[string]any) map[string]any {
	entry := map[string]any{
		"id":    m["deviation_case_id"],
		"scope": scope,
	}
	if variants, ok := m["message_variants"].([]any); ok && len(variants) > 0 {
		if v0, ok := variants[0].(map[string]any); ok {
			entry["message"] = v0["details"]
			if hdr, ok := v0["header"].(string); ok && hdr != "" {
				entry["header"] = hdr
			}
		}
	}
	if pub, ok := m["publish"].(map[string]any); ok {
		entry["publish"] = pub
	}
	return entry
}

// messageActiveAt checks that `at` falls inside publish.from..publish.upto.
// If either bound is missing or unparseable, the entry is kept (conservative).
func messageActiveAt(m map[string]any, at time.Time) bool {
	pub, ok := m["publish"].(map[string]any)
	if !ok {
		return true
	}
	fromStr, _ := pub["from"].(string)
	uptoStr, _ := pub["upto"].(string)
	from, fErr := time.Parse(time.RFC3339Nano, fromStr)
	upto, uErr := time.Parse(time.RFC3339Nano, uptoStr)
	if fErr != nil || uErr != nil {
		return true
	}
	return !at.Before(from) && !at.After(upto)
}

// filterUpstreamStopDeviations drops any entry whose scope doesn't intersect
// the site. Used as fallback when /v1/messages can't be fetched.
func filterUpstreamStopDeviations(root map[string]any, site siteIdentity) []any {
	sds, _ := root["stop_deviations"].([]any)
	out := make([]any, 0, len(sds))
	for _, sdAny := range sds {
		sd, ok := sdAny.(map[string]any)
		if !ok {
			continue
		}
		scope, _ := sd["scope"].(map[string]any)
		if scope == nil {
			continue
		}
		if scopeMatchesSite(scope, site) {
			out = append(out, sd)
		}
	}
	return out
}

// stripStopDeviationNoise drops scope.stop_points (useless once the site has
// been determined) and strips "href" fields from remaining scope entries.
func stripStopDeviationNoise(sds []any) {
	for _, sdAny := range sds {
		sd, ok := sdAny.(map[string]any)
		if !ok {
			continue
		}
		scope, ok := sd["scope"].(map[string]any)
		if !ok {
			continue
		}
		delete(scope, "stop_points")
		for _, arrKey := range []string{"stop_areas", "lines"} {
			arr, ok := scope[arrKey].([]any)
			if !ok {
				continue
			}
			for _, entry := range arr {
				if m, ok := entry.(map[string]any); ok {
					delete(m, "href")
				}
			}
		}
	}
}
