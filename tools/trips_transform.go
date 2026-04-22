package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/henrrrik/sl-mcp-server/slclient"
)

// upstreamTrips mirrors the fields of the SL journey-planner /v2/trips
// response that we actually use. Unused fields are ignored by the decoder.
type upstreamTrips struct {
	Journeys       []upstreamJourney `json:"journeys"`
	SystemMessages []json.RawMessage `json:"systemMessages,omitempty"`
}

type upstreamJourney struct {
	TripDuration   int           `json:"tripDuration"`
	TripRtDuration int           `json:"tripRtDuration"`
	Interchanges   int           `json:"interchanges"`
	Legs           []upstreamLeg `json:"legs"`
}

type upstreamLeg struct {
	Duration             int                    `json:"duration"`
	Origin               upstreamStopEvent      `json:"origin"`
	Destination          upstreamStopEvent      `json:"destination"`
	Transportation       upstreamTransportation `json:"transportation"`
	IsRealtimeControlled bool                   `json:"isRealtimeControlled"`
}

type upstreamStopEvent struct {
	Name                   string `json:"name"`
	DepartureTimePlanned   string `json:"departureTimePlanned"`
	DepartureTimeEstimated string `json:"departureTimeEstimated"`
	ArrivalTimePlanned     string `json:"arrivalTimePlanned"`
	ArrivalTimeEstimated   string `json:"arrivalTimeEstimated"`
}

type upstreamTransportation struct {
	DisassembledName string                 `json:"disassembledName"`
	Number           string                 `json:"number"`
	Product          upstreamProduct        `json:"product"`
	Destination      *upstreamTransportDest `json:"destination"`
}

type upstreamProduct struct {
	Name string `json:"name"`
}

type upstreamTransportDest struct {
	Name string `json:"name"`
}

// trimmedTrips is the LLM-friendly output shape.
type trimmedTrips struct {
	Journeys       []trimmedJourney  `json:"journeys"`
	SystemMessages []json.RawMessage `json:"systemMessages,omitempty"`
}

type trimmedJourney struct {
	Duration     int          `json:"duration"`
	Interchanges int          `json:"interchanges"`
	Summary      string       `json:"summary,omitempty"`
	Departure    string       `json:"departure,omitempty"`
	Arrival      string       `json:"arrival,omitempty"`
	Legs         []trimmedLeg `json:"legs"`
}

type trimmedLeg struct {
	Mode              string         `json:"mode"`
	Line              string         `json:"line,omitempty"`
	Direction         string         `json:"direction,omitempty"`
	From              string         `json:"from,omitempty"`
	To                string         `json:"to,omitempty"`
	Departure         string         `json:"departure,omitempty"`
	Arrival           string         `json:"arrival,omitempty"`
	Duration          int            `json:"duration"`
	Realtime          bool           `json:"realtime,omitempty"`
	Deviations        []legDeviation `json:"deviations,omitempty"`
	HasMoreDeviations bool           `json:"has_more_deviations,omitempty"`
}

// legDeviation is the per-leg summary of an active /v1/messages entry matching
// the leg's line and mode.
type legDeviation struct {
	CaseID  int    `json:"case_id,omitempty"`
	Header  string `json:"header,omitempty"`
	Details string `json:"details,omitempty"`
	From    string `json:"from,omitempty"`
	Upto    string `json:"upto,omitempty"`
}

// reshapeTrips decodes a raw upstream /v2/trips response into the trimmed
// form. Returns nil when the response has no journeys (error-only responses,
// which the caller should pass through verbatim).
func reshapeTrips(raw []byte) (*trimmedTrips, error) {
	var up upstreamTrips
	if err := json.Unmarshal(raw, &up); err != nil {
		return nil, err
	}
	if len(up.Journeys) == 0 {
		return nil, nil
	}

	out := &trimmedTrips{
		Journeys:       make([]trimmedJourney, len(up.Journeys)),
		SystemMessages: up.SystemMessages,
	}
	for i, j := range up.Journeys {
		out.Journeys[i] = trimJourney(j)
	}
	return out, nil
}

func trimJourney(j upstreamJourney) trimmedJourney {
	legs := make([]trimmedLeg, len(j.Legs))
	for i, leg := range j.Legs {
		legs[i] = trimLeg(leg)
	}

	duration := j.TripRtDuration
	if duration == 0 {
		duration = j.TripDuration
	}

	tj := trimmedJourney{
		Duration:     duration,
		Interchanges: j.Interchanges,
		Legs:         legs,
		Summary:      journeySummary(legs),
	}
	if len(legs) > 0 {
		tj.Departure = legs[0].Departure
		tj.Arrival = legs[len(legs)-1].Arrival
	}
	return tj
}

func trimLeg(leg upstreamLeg) trimmedLeg {
	mode := mapMode(leg.Transportation.Product.Name)
	out := trimmedLeg{
		Mode:      mode,
		From:      leg.Origin.Name,
		To:        leg.Destination.Name,
		Departure: pickTime(leg.Origin.DepartureTimeEstimated, leg.Origin.DepartureTimePlanned),
		Arrival:   pickTime(leg.Destination.ArrivalTimeEstimated, leg.Destination.ArrivalTimePlanned),
		Duration:  leg.Duration,
		Realtime:  leg.IsRealtimeControlled,
	}
	if mode != "walk" {
		out.Line = leg.Transportation.DisassembledName
		if leg.Transportation.Destination != nil {
			out.Direction = leg.Transportation.Destination.Name
		}
	}
	return out
}

// mapMode translates the Swedish product names returned by SL into concise
// English mode identifiers. Unknown products pass through lowercased.
func mapMode(name string) string {
	switch name {
	case "Buss":
		return "bus"
	case "Tåg":
		return "train"
	case "Tunnelbana":
		return "metro"
	case "Spårvagn":
		return "tram"
	case "Båt", "Skepp", "Ferja":
		return "ship"
	case "footpath":
		return "walk"
	default:
		return strings.ToLower(name)
	}
}

func pickTime(estimated, planned string) string {
	if estimated != "" {
		return estimated
	}
	return planned
}

// journeySummary joins transit legs with an arrow, skipping walking legs.
// Example: "Buss 179 → Pendeltåg 43".
func journeySummary(legs []trimmedLeg) string {
	parts := make([]string, 0, len(legs))
	for _, leg := range legs {
		if leg.Mode == "walk" || leg.Line == "" {
			continue
		}
		label := summaryLabel(leg.Mode, leg.Line)
		parts = append(parts, label)
	}
	return strings.Join(parts, " → ")
}

// summaryLabel picks a human-recognizable prefix for a leg. The upstream
// product names are Swedish; keeping them preserves the familiar branding
// ("Pendeltåg" is distinct from "Tåg" in Stockholm usage).
func summaryLabel(mode, line string) string {
	switch mode {
	case "bus":
		return "Buss " + line
	case "train":
		return "Pendeltåg " + line
	case "metro":
		return "Tunnelbana " + line
	case "tram":
		return "Spårvagn " + line
	case "ship":
		return "Båt " + line
	default:
		return mode + " " + line
	}
}

// detectAmbiguity inspects a raw /v2/trips response and decides whether the
// broker failed to resolve origin and/or destination. It only flags ambiguity
// when the response carries no journeys — if journeys came back, the broker
// made a best-effort match and callers don't need candidate pickers.
func detectAmbiguity(raw []byte) (originAmbiguous, destinationAmbiguous bool) {
	var u struct {
		Journeys       []json.RawMessage `json:"journeys"`
		SystemMessages []struct {
			Code int    `json:"code"`
			Text string `json:"text"`
		} `json:"systemMessages"`
	}
	if err := json.Unmarshal(raw, &u); err != nil {
		return false, false
	}
	if len(u.Journeys) > 0 {
		return false, false
	}
	for _, m := range u.SystemMessages {
		if m.Code != -8010 && m.Code != -8011 {
			continue
		}
		if strings.HasPrefix(m.Text, "origin:") {
			originAmbiguous = true
		}
		if strings.HasPrefix(m.Text, "destination:") {
			destinationAmbiguous = true
		}
	}
	return
}

// locationCandidate is the outward shape for a disambiguation suggestion.
type locationCandidate struct {
	Name     string    `json:"name"`
	Locality string    `json:"locality,omitempty"`
	ID       string    `json:"id"`
	Type     string    `json:"type,omitempty"`
	Coord    []float64 `json:"coord,omitempty"`
}

type ambiguitySingleResponse struct {
	Error      string              `json:"error"`
	Query      string              `json:"query"`
	Candidates []locationCandidate `json:"candidates"`
}

type sideCandidates struct {
	Query      string              `json:"query"`
	Candidates []locationCandidate `json:"candidates"`
}

type ambiguityBothResponse struct {
	Error       string         `json:"error"`
	Origin      sideCandidates `json:"origin"`
	Destination sideCandidates `json:"destination"`
}

const maxCandidates = 5

// modeToUpstreamTransport maps the trimmed-leg mode identifiers into the
// transport_mode values that /v1/messages.scope.lines uses.
func modeToUpstreamTransport(mode string) string {
	switch mode {
	case "bus":
		return "BUS"
	case "train":
		return "TRAIN"
	case "metro":
		return "METRO"
	case "tram":
		return "TRAM"
	case "ship":
		return "SHIP"
	default:
		return ""
	}
}

// upstreamDeviation mirrors the subset of /v1/messages fields we index and
// surface. Everything else in the response is ignored.
type upstreamDeviation struct {
	DeviationCaseID int `json:"deviation_case_id"`
	Scope           struct {
		Lines []struct {
			Designation   string `json:"designation"`
			TransportMode string `json:"transport_mode"`
		} `json:"lines"`
	} `json:"scope"`
	Publish struct {
		From string `json:"from"`
		Upto string `json:"upto"`
	} `json:"publish"`
	MessageVariants []struct {
		Header   string `json:"header"`
		Details  string `json:"details"`
		Language string `json:"language"`
	} `json:"message_variants"`
}

// indexDeviations groups upstream deviations by (line-designation, transport_mode).
// A single deviation can appear under multiple keys if its scope spans several lines.
func indexDeviations(raw []byte) (map[deviationKey][]legDeviation, error) {
	var entries []upstreamDeviation
	if err := json.Unmarshal(raw, &entries); err != nil {
		return nil, err
	}
	out := make(map[deviationKey][]legDeviation, len(entries))
	for _, e := range entries {
		ld := legDeviation{
			CaseID: e.DeviationCaseID,
			From:   e.Publish.From,
			Upto:   e.Publish.Upto,
		}
		if len(e.MessageVariants) > 0 {
			ld.Header = e.MessageVariants[0].Header
			ld.Details = e.MessageVariants[0].Details
		}
		for _, line := range e.Scope.Lines {
			if line.Designation == "" || line.TransportMode == "" {
				continue
			}
			k := deviationKey{line: line.Designation, mode: line.TransportMode}
			out[k] = append(out[k], ld)
		}
	}
	return out, nil
}

type deviationKey struct {
	line string
	mode string // upstream TRAIN/BUS/METRO/TRAM/SHIP
}

// maxDeviationsPerLeg caps the attached array to avoid flooding a trip response
// with dozens of marginally-relevant notices when a line has lots of concurrent
// deviations. The upstream /v1/messages endpoint doesn't expose a priority we
// can sort by reliably, so we keep the first N after time filtering.
const maxDeviationsPerLeg = 3

// attachDeviations walks each leg and attaches any deviations from the index
// whose (line, mode) matches AND whose publish window covers the leg's
// departure time. Walking legs and untyped legs are skipped. When more than
// maxDeviationsPerLeg match, the array is truncated and HasMoreDeviations set.
func attachDeviations(tt *trimmedTrips, index map[deviationKey][]legDeviation) {
	for ji := range tt.Journeys {
		for li := range tt.Journeys[ji].Legs {
			leg := &tt.Journeys[ji].Legs[li]
			if leg.Line == "" {
				continue
			}
			mode := modeToUpstreamTransport(leg.Mode)
			if mode == "" {
				continue
			}
			candidates, ok := index[deviationKey{line: leg.Line, mode: mode}]
			if !ok {
				continue
			}
			legTime, legTimeErr := parseDeviationTime(leg.Departure)
			filtered := make([]legDeviation, 0, len(candidates))
			for _, cand := range candidates {
				if legTimeErr == nil && !deviationActiveAt(cand, legTime) {
					continue
				}
				filtered = append(filtered, cand)
			}
			if len(filtered) > maxDeviationsPerLeg {
				leg.HasMoreDeviations = true
				filtered = filtered[:maxDeviationsPerLeg]
			}
			if len(filtered) > 0 {
				leg.Deviations = filtered
			}
		}
	}
}

// parseDeviationTime parses a trips/deviation timestamp. RFC3339Nano handles
// both "2026-04-19T09:00:00+02:00" and "2026-04-19T09:00:00.000+02:00".
func parseDeviationTime(s string) (time.Time, error) {
	if s == "" {
		return time.Time{}, fmt.Errorf("empty timestamp")
	}
	return time.Parse(time.RFC3339Nano, s)
}

// deviationActiveAt reports whether the deviation's publish window covers the
// given moment. If either bound is missing or unparseable we conservatively
// keep the deviation — over-including is less bad than silently dropping a
// real warning.
func deviationActiveAt(d legDeviation, at time.Time) bool {
	from, fromErr := parseDeviationTime(d.From)
	upto, uptoErr := parseDeviationTime(d.Upto)
	if fromErr != nil || uptoErr != nil {
		return true
	}
	return !at.Before(from) && !at.After(upto)
}

// resolveCandidates fetches /v2/stop-finder for the given query and returns
// up to maxCandidates of the most-relevant matches. Upstream orders results
// by matchQuality, so we take the first N.
func resolveCandidates(ctx context.Context, client slclient.HTTPDoer, query string) ([]locationCandidate, error) {
	params := url.Values{
		"name_sf":           {query},
		"type_sf":           {"any"},
		"any_obj_filter_sf": {"2"},
	}
	u := slclient.BuildURL(journeyPlannerBase, "/v2/stop-finder", params)
	body, errResult := fetchJSONRaw(ctx, client, u)
	if errResult != nil {
		return nil, fmt.Errorf("stop-finder failed: %s", errResultText(errResult))
	}

	var sf struct {
		Locations []struct {
			Coord            []float64 `json:"coord"`
			DisassembledName string    `json:"disassembledName"`
			ID               string    `json:"id"`
			Name             string    `json:"name"`
			Parent           struct {
				Name string `json:"name"`
			} `json:"parent"`
			Type string `json:"type"`
		} `json:"locations"`
	}
	if err := json.Unmarshal(body, &sf); err != nil {
		return nil, fmt.Errorf("stop-finder decode: %w", err)
	}

	n := len(sf.Locations)
	if n > maxCandidates {
		n = maxCandidates
	}
	out := make([]locationCandidate, n)
	for i := 0; i < n; i++ {
		loc := sf.Locations[i]
		name := loc.DisassembledName
		if name == "" {
			name = loc.Name
		}
		out[i] = locationCandidate{
			Name:     name,
			Locality: loc.Parent.Name,
			ID:       loc.ID,
			Type:     loc.Type,
			Coord:    loc.Coord,
		}
	}
	return out, nil
}
