package tools

import (
	"encoding/json"
	"time"
)

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
func trimDepartures(depBody, msgsBody []byte) ([]byte, error) {
	var root map[string]any
	if err := json.Unmarshal(depBody, &root); err != nil {
		return nil, err
	}

	site := collectSiteIdentity(root)

	var stopDeviations []any
	if msgsBody != nil {
		if rederived, err := filterMessagesForSite(msgsBody, site, time.Now()); err == nil {
			stopDeviations = rederived
		}
	}
	if stopDeviations == nil {
		stopDeviations = filterUpstreamStopDeviations(root, site)
	}

	stripStopDeviationNoise(stopDeviations)
	root["stop_deviations"] = stopDeviations
	return json.Marshal(root)
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

	hasStopScope := len(stopAreas) > 0 || len(stopPoints) > 0
	if hasStopScope {
		for _, sa := range stopAreas {
			if id := extractID(sa); id != 0 && site.stopAreaIDs[id] {
				return true
			}
		}
		for _, sp := range stopPoints {
			if id := extractID(sp); id != 0 && site.stopPointIDs[id] {
				return true
			}
		}
		return false
	}

	lines, _ := scope["lines"].([]any)
	if len(lines) > 0 {
		for _, ln := range lines {
			if id := extractID(ln); id != 0 && site.lineIDs[id] {
				return true
			}
		}
		return false
	}

	// No scope at all — network-wide notice, keep.
	return true
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
		if scope == nil {
			continue
		}
		if !scopeMatchesSite(scope, site) {
			continue
		}
		if !messageActiveAt(m, at) {
			continue
		}
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
		out = append(out, entry)
	}
	return out, nil
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
