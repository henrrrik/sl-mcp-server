package tools

import (
	"encoding/json"
	"sort"
	"strings"
)

// linesFilters holds the optional filters applied when flattening the
// /v1/lines response. All fields are already lowercased (or empty) so the
// per-entry check is cheap.
type linesFilters struct {
	mode         string // lowercased; matches the upstream group key
	query        string // substring over name OR designation; legacy param
	designation  string // prefix match on designation
	groupOfLines string // substring match on group_of_lines
	limit        int    // 0 = unlimited
}

// flattenAndFilterLines walks the /v1/lines grouped-by-mode response shape
// ({"metro": [...], "bus": [...], ...}) and returns a flat JSON array of
// the filtered line entries.
//
// Filter semantics:
//   - mode: case-insensitive exact match on the group key. Unknown modes
//     match nothing (an empty array is returned).
//   - query: substring match over name OR designation. Legacy parameter
//     kept for backward compatibility.
//   - designation: prefix match on the line's designation. "54" matches
//     54, 540, 541, …; narrower than query.
//   - groupOfLines: substring match on the line's group_of_lines field
//     ("Pendeltåg", "Blåbuss", "Närtrafiken").
//   - limit: truncates the result; 0 means unlimited.
//
// Group keys are walked in alphabetical order so the output is
// deterministic across Go map-iteration randomisation. Unknown fields on
// each line pass through verbatim.
func flattenAndFilterLines(raw []byte, f linesFilters) ([]byte, error) {
	var groups map[string]json.RawMessage
	if err := json.Unmarshal(raw, &groups); err != nil {
		return nil, err
	}

	keys := selectGroupKeys(groups, f.mode)

	kept := make([]json.RawMessage, 0)
	for _, k := range keys {
		var entries []json.RawMessage
		if err := json.Unmarshal(groups[k], &entries); err != nil {
			continue
		}
		for _, e := range entries {
			if !lineMatchesFilters(e, f) {
				continue
			}
			kept = append(kept, e)
			if f.limit > 0 && len(kept) >= f.limit {
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

// lineMatchesFilters reports whether a line entry passes every active
// filter. All filters are ANDed. A malformed entry (one that fails to
// decode into the known fields) is treated as non-matching so one bad
// upstream row doesn't sneak past the filters.
func lineMatchesFilters(e json.RawMessage, f linesFilters) bool {
	var line struct {
		Name         string `json:"name"`
		Designation  string `json:"designation"`
		GroupOfLines string `json:"group_of_lines"`
	}
	if err := json.Unmarshal(e, &line); err != nil {
		return false
	}
	name := strings.ToLower(line.Name)
	designation := strings.ToLower(line.Designation)
	group := strings.ToLower(line.GroupOfLines)

	if f.query != "" && !strings.Contains(name, f.query) && !strings.Contains(designation, f.query) {
		return false
	}
	if f.designation != "" && !strings.HasPrefix(designation, f.designation) {
		return false
	}
	if f.groupOfLines != "" && !strings.Contains(group, f.groupOfLines) {
		return false
	}
	return true
}
