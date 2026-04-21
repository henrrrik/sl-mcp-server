package tools

import (
	"encoding/json"
	"strings"
)

// filterSites returns a JSON-encoded slice of the upstream /v1/sites entries
// matching the given name query (case-insensitive substring) and trimmed to
// at most `limit` entries. An empty query means "no name filter"; limit <= 0
// means "no truncation". Unknown fields in each entry are preserved verbatim.
func filterSites(raw []byte, query string, limit int) ([]byte, error) {
	var entries []json.RawMessage
	if err := json.Unmarshal(raw, &entries); err != nil {
		return nil, err
	}

	needle := strings.ToLower(query)

	kept := make([]json.RawMessage, 0, len(entries))
	for _, e := range entries {
		if needle != "" {
			var named struct {
				Name string `json:"name"`
			}
			if err := json.Unmarshal(e, &named); err != nil {
				continue
			}
			if !strings.Contains(strings.ToLower(named.Name), needle) {
				continue
			}
		}
		kept = append(kept, e)
		if limit > 0 && len(kept) >= limit {
			break
		}
	}
	return json.Marshal(kept)
}
