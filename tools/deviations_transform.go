package tools

import "encoding/json"

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
//	  categories:        []string,   // if upstream sends any
//	}
//
// Malformed entries pass through verbatim so one bad row doesn't drop
// every deviation in the batch.
func trimDeviationsList(raw []byte) ([]byte, error) {
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
		out = append(out, slim)
	}
	return json.Marshal(out)
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
				Designation string `json:"designation"`
			} `json:"lines"`
			StopAreas []struct {
				Name string `json:"name"`
			} `json:"stop_areas"`
		} `json:"scope"`
		Categories []string `json:"categories"`
	}
	if err := json.Unmarshal(raw, &src); err != nil {
		return slimDeviation{}, false
	}
	out := slimDeviation{
		DeviationCaseID: src.DeviationCaseID,
		PublishFrom:     src.Publish.From,
		PublishUpto:     src.Publish.Upto,
		Categories:      src.Categories,
	}
	if len(src.MessageVariants) > 0 {
		out.Header = src.MessageVariants[0].Header
		out.Details = src.MessageVariants[0].Details
	}
	for _, l := range src.Scope.Lines {
		if l.Designation != "" {
			out.Lines = append(out.Lines, l.Designation)
		}
	}
	for _, sa := range src.Scope.StopAreas {
		if sa.Name != "" {
			out.StopAreas = append(out.StopAreas, sa.Name)
		}
	}
	return out, true
}
