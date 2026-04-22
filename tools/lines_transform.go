package tools

import (
	"encoding/json"
	"sort"
	"strings"
)

// flattenAndFilterLines walks the /v1/lines grouped-by-mode response shape
// ({"metro": [...], "bus": [...], ...}) and returns a flat JSON array of
// the filtered line entries. An empty transportMode means "any mode";
// otherwise only the matching group is iterated (case-insensitive). An
// empty query means "no name/designation filter"; otherwise entries are
// kept when either field contains the lowercased needle. A limit <= 0
// means "no truncation". Group keys are walked in alphabetical order so
// the output is deterministic across Go map-iteration randomisation.
// Unknown fields on each line pass through verbatim.
func flattenAndFilterLines(raw []byte, transportMode, query string, limit int) ([]byte, error) {
	var groups map[string]json.RawMessage
	if err := json.Unmarshal(raw, &groups); err != nil {
		return nil, err
	}

	keys := selectGroupKeys(groups, strings.ToLower(transportMode))
	needle := strings.ToLower(query)

	kept := make([]json.RawMessage, 0)
	for _, k := range keys {
		var entries []json.RawMessage
		if err := json.Unmarshal(groups[k], &entries); err != nil {
			continue
		}
		for _, e := range entries {
			if !lineMatchesNeedle(e, needle) {
				continue
			}
			kept = append(kept, e)
			if limit > 0 && len(kept) >= limit {
				return json.Marshal(kept)
			}
		}
	}
	return json.Marshal(kept)
}

// selectGroupKeys returns the (sorted) subset of upstream group keys to walk.
// When modeFilter is empty, every group is selected. Otherwise only the key
// whose lowercased form matches the filter is returned. Sorting makes output
// deterministic across Go map-iteration randomisation.
func selectGroupKeys(groups map[string]json.RawMessage, modeFilter string) []string {
	keys := make([]string, 0, len(groups))
	for k := range groups {
		if modeFilter != "" && strings.ToLower(k) != modeFilter {
			continue
		}
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// lineMatchesNeedle reports whether a line entry matches the (lowercased)
// query. An empty needle matches everything. A malformed entry (one that
// fails to decode into {name, designation}) is treated as non-matching so
// one bad upstream row doesn't sneak past the filter.
func lineMatchesNeedle(e json.RawMessage, needle string) bool {
	if needle == "" {
		return true
	}
	var named struct {
		Name        string `json:"name"`
		Designation string `json:"designation"`
	}
	if err := json.Unmarshal(e, &named); err != nil {
		return false
	}
	return strings.Contains(strings.ToLower(named.Name), needle) ||
		strings.Contains(strings.ToLower(named.Designation), needle)
}
