package tools

import (
	"encoding/json"
	"strings"
)

// trimDeviationsList reshapes a raw /v1/messages response into the default
// LLM-friendly form. Upstream entries carry ~30 fields per deviation —
// versions, priorities, transport_authority nested inside scope.lines, etc.
// Callers almost never need any of that; this function keeps only the
// fields that answer "what's happening, where, on which lines, when."
//
// Per-entry output shape:
//
//	{
//	  deviation_case_id: int,
//	  header:            string,
//	  details:           string,
//	  publish_from:      string,
//	  publish_upto:      string,
//	  lines:             []string,   // designations only
//	  stop_areas:        []string,   // names only
//	  categories:        []string,   // flat "GROUP:NAME" strings if upstream sent any
//	}
//
// Malformed entries pass through verbatim so one bad row doesn't drop
// every deviation in the batch.
//
// filters is applied in-process after decode. Use this for the
// accessibility-aware facility filter and for client-side transport_mode
// filtering (needed because SL's upstream transport_mode filter drops
// scope.stop_areas-only entries — i.e. every lift/escalator notice).
func trimDeviationsList(raw []byte, filters deviationsClientFilters) ([]byte, error) {
	var entries []json.RawMessage
	if err := json.Unmarshal(raw, &entries); err != nil {
		return nil, err
	}
	out := make([]slimDeviation, 0, len(entries))
	for _, e := range entries {
		slim, ok := slimDeviationEntry(e)
		if !ok {
			continue
		}
		if !filters.keep(slim) {
			continue
		}
		out = append(out, slim)
	}
	return json.Marshal(out)
}

// deviationsClientFilters is the in-process filter pass. transportMode is
// lowercased when set; empty means "no mode filter". includeFacility
// toggles whether FACILITY-category entries survive.
type deviationsClientFilters struct {
	transportMode   string
	includeFacility bool
}

// keep returns true when the entry passes every active filter.
//
// The transport_mode rule: when set, the entry needs at least one line
// whose transport_mode matches. Entries with no line scope (the typical
// facility entry) fail this check — which is the whole reason we filter
// client-side in the first place. Facility entries are re-admitted below
// by the includeFacility branch.
//
// The facility rule: when includeFacility is false, FACILITY-category
// entries are dropped regardless of other filters. When true, FACILITY
// entries survive the transport_mode gate too — the caller has explicitly
// opted into accessibility notices and a lift outage doesn't have a clean
// "transport mode" to gate on.
func (f deviationsClientFilters) keep(d slimDeviation) bool {
	isFacility := hasFacilityCategory(d.Categories)

	if !f.includeFacility && isFacility {
		return false
	}

	if f.transportMode == "" {
		return true
	}
	if f.includeFacility && isFacility {
		return true
	}
	for _, mode := range d.lineModes {
		if strings.EqualFold(mode, f.transportMode) {
			return true
		}
	}
	return false
}

// hasFacilityCategory reports whether any of the flattened "GROUP:NAME"
// category strings has a FACILITY group prefix.
func hasFacilityCategory(cats []string) bool {
	for _, c := range cats {
		if strings.HasPrefix(strings.ToUpper(c), "FACILITY:") || strings.EqualFold(c, "FACILITY") {
			return true
		}
	}
	return false
}

type slimDeviation struct {
	DeviationCaseID int      `json:"deviation_case_id,omitempty"`
	Header          string   `json:"header,omitempty"`
	Details         string   `json:"details,omitempty"`
	PublishFrom     string   `json:"publish_from,omitempty"`
	PublishUpto     string   `json:"publish_upto,omitempty"`
	Lines           []string `json:"lines,omitempty"`
	StopAreas       []string `json:"stop_areas,omitempty"`
	Categories      []string `json:"categories,omitempty"`

	// lineModes carries the transport_mode of each line in scope, for the
	// client-side transport_mode filter. Not marshaled — intentional.
	lineModes []string
}

func slimDeviationEntry(raw json.RawMessage) (slimDeviation, bool) {
	var src struct {
		DeviationCaseID int `json:"deviation_case_id"`
		MessageVariants []struct {
			Header   string `json:"header"`
			Details  string `json:"details"`
			Language string `json:"language"`
		} `json:"message_variants"`
		Publish struct {
			From string `json:"from"`
			Upto string `json:"upto"`
		} `json:"publish"`
		Scope struct {
			Lines []struct {
				Designation   string `json:"designation"`
				TransportMode string `json:"transport_mode"`
			} `json:"lines"`
			StopAreas []struct {
				Name string `json:"name"`
			} `json:"stop_areas"`
		} `json:"scope"`
		Categories json.RawMessage `json:"categories"`
	}
	if err := json.Unmarshal(raw, &src); err != nil {
		return slimDeviation{}, false
	}
	out := slimDeviation{
		DeviationCaseID: src.DeviationCaseID,
		PublishFrom:     src.Publish.From,
		PublishUpto:     src.Publish.Upto,
		Categories:      flattenCategories(src.Categories),
	}
	if len(src.MessageVariants) > 0 {
		out.Header = src.MessageVariants[0].Header
		out.Details = src.MessageVariants[0].Details
	}
	for _, l := range src.Scope.Lines {
		if l.Designation != "" {
			out.Lines = append(out.Lines, l.Designation)
		}
		if l.TransportMode != "" {
			out.lineModes = append(out.lineModes, l.TransportMode)
		}
	}
	for _, sa := range src.Scope.StopAreas {
		if sa.Name != "" {
			out.StopAreas = append(out.StopAreas, sa.Name)
		}
	}
	return out, true
}

// flattenCategories handles both shapes SL has emitted for categories over
// time: a plain []string and a structured [{group, name}]. Structured form
// is flattened to "GROUP:NAME" (or just "GROUP" when name is absent).
// Returns nil for unknown/missing shapes so the slim response can omit the
// field with omitempty.
func flattenCategories(raw json.RawMessage) []string {
	if len(raw) == 0 {
		return nil
	}
	// Try []string first.
	var asStrings []string
	if err := json.Unmarshal(raw, &asStrings); err == nil {
		out := make([]string, 0, len(asStrings))
		for _, s := range asStrings {
			if s != "" {
				out = append(out, s)
			}
		}
		if len(out) == 0 {
			return nil
		}
		return out
	}
	// Structured form.
	var asObjects []struct {
		Group string `json:"group"`
		Name  string `json:"name"`
	}
	if err := json.Unmarshal(raw, &asObjects); err != nil {
		return nil
	}
	out := make([]string, 0, len(asObjects))
	for _, c := range asObjects {
		label := flattenCategoryLabel(c.Group, c.Name)
		if label != "" {
			out = append(out, label)
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func flattenCategoryLabel(group, name string) string {
	group = strings.TrimSpace(group)
	name = strings.TrimSpace(name)
	switch {
	case group != "" && name != "":
		return group + ":" + name
	case group != "":
		return group
	case name != "":
		return name
	default:
		return ""
	}
}
