package tools

import "encoding/json"

// trimDepartures strips noise from the upstream /v1/sites/{id}/departures
// response: scope.stop_points is dropped entirely (platform-level entries
// span the whole network and aren't useful when the caller asked about a
// single site), and broken "null/..." href values are removed from the
// remaining scope arrays. All other fields — including scope.stop_areas
// and scope.lines — are preserved verbatim.
func trimDepartures(raw []byte) ([]byte, error) {
	var root map[string]any
	if err := json.Unmarshal(raw, &root); err != nil {
		return nil, err
	}

	sds, ok := root["stop_deviations"].([]any)
	if !ok {
		return json.Marshal(root)
	}

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

	return json.Marshal(root)
}
