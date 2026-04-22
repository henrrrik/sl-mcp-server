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
	ID                     string              `json:"id"`
	Name                   string              `json:"name"`
	Type                   string              `json:"type"`
	Coord                  []float64           `json:"coord"`
	Parent                 *upstreamStopParent `json:"parent,omitempty"`
	DepartureTimePlanned   string              `json:"departureTimePlanned"`
	DepartureTimeEstimated string              `json:"departureTimeEstimated"`
	ArrivalTimePlanned     string              `json:"arrivalTimePlanned"`
	ArrivalTimeEstimated   string              `json:"arrivalTimeEstimated"`
}

// upstreamStopParent is the enclosing stop for a platform-level stop event.
// Only the subset of fields used for the resolved-echo and stop-type guard
// is decoded — everything else on the upstream struct is ignored.
type upstreamStopParent struct {
	ID         string                 `json:"id"`
	Name       string                 `json:"name"`
	Type       string                 `json:"type"`
	Coord      []float64              `json:"coord"`
	Properties upstreamStopProperties `json:"properties"`
}

type upstreamStopProperties struct {
	StopID string `json:"stopId"`
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
	Resolved       *resolvedTrip     `json:"resolved,omitempty"`
	Warnings       []tripWarning     `json:"warnings,omitempty"`
}

// tripWarning surfaces non-fatal disambiguation notices — e.g. an exact-name
// match was picked over close-but-lower-quality shadows. The caller can
// still inspect the shadowed candidates and retry with a different name
// or an explicit id.
type tripWarning struct {
	Code     string              `json:"code"`
	Side     string              `json:"side,omitempty"`
	Query    string              `json:"query,omitempty"`
	Picked   *locationCandidate  `json:"picked,omitempty"`
	Shadowed []locationCandidate `json:"shadowed,omitempty"`
}

// resolvedTrip echoes the actual origin/destination the planner used, so
// callers can detect silent drift when fuzzy name matching produced a
// different location than they expected.
type resolvedTrip struct {
	Origin      resolvedLocation `json:"origin"`
	Destination resolvedLocation `json:"destination"`
}

type resolvedLocation struct {
	Name   string    `json:"name,omitempty"`
	ID     string    `json:"id,omitempty"`      // 16-digit GID when the upstream supplied one.
	SiteID int       `json:"site_id,omitempty"` // short form, when derivable.
	Type   string    `json:"type,omitempty"`    // stop / platform / poi / address / locality.
	Coord  []float64 `json:"coord,omitempty"`
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
		SystemMessages: filterStaleBrokerMessages(up.SystemMessages),
	}
	for i, j := range up.Journeys {
		out.Journeys[i] = trimJourney(j)
	}
	return out, nil
}

// stockholmLocation is the time zone SL operates in. Used to rewrite upstream
// leg times (which JP emits in UTC) into local time with an explicit offset,
// matching how every other timestamp in SL-land reads. Falls back to UTC if
// the zone database isn't present (shouldn't happen on Runway's Go buildpack
// but belt-and-braces on a helper this cheap).
var stockholmLocation = func() *time.Location {
	if loc, err := time.LoadLocation(stockholmTZ); err == nil {
		return loc
	}
	return time.UTC
}()

// localizeTime parses an RFC3339 timestamp and re-emits it in Europe/Stockholm
// with an explicit offset. Empty input returns empty; unparseable input is
// passed through verbatim so upstream format drift surfaces rather than
// silently drops the field.
func localizeTime(ts string) string {
	if ts == "" {
		return ""
	}
	t, err := time.Parse(time.RFC3339, ts)
	if err != nil {
		return ts
	}
	return t.In(stockholmLocation).Format(time.RFC3339)
}

// filterStaleBrokerMessages drops the -8010/-8011 "origin:" / "destination:"
// BROKER error messages that JP leaves on a successful /v2/trips response
// even after ambiguity was auto-resolved. reshapeTrips is only called when
// journeys exist, so any "couldn't resolve origin" message at this point is
// stale — keeping it confuses callers into thinking their resolution failed.
// Non-matching system messages pass through unchanged.
func filterStaleBrokerMessages(raw []json.RawMessage) []json.RawMessage {
	out := make([]json.RawMessage, 0, len(raw))
	for _, m := range raw {
		var msg struct {
			Type   string `json:"type"`
			Module string `json:"module"`
			Code   int    `json:"code"`
			Text   string `json:"text"`
		}
		if err := json.Unmarshal(m, &msg); err != nil {
			out = append(out, m)
			continue
		}
		if isStaleResolutionMessage(msg.Type, msg.Module, msg.Code, msg.Text) {
			continue
		}
		out = append(out, m)
	}
	return out
}

func isStaleResolutionMessage(msgType, module string, code int, text string) bool {
	if msgType != "error" || module != "BROKER" {
		return false
	}
	if code != -8010 && code != -8011 {
		return false
	}
	return strings.HasPrefix(text, "origin:") || strings.HasPrefix(text, "destination:")
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
		return localizeTime(estimated)
	}
	return localizeTime(planned)
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
	Name         string    `json:"name"`
	Locality     string    `json:"locality,omitempty"`
	ID           string    `json:"id"`
	Type         string    `json:"type,omitempty"`
	Coord        []float64 `json:"coord,omitempty"`
	MatchQuality int       `json:"match_quality,omitempty"`
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

// extractResolved derives the resolved-origin / resolved-destination block
// from a raw /v2/trips response. Returns nil on malformed JSON or on
// error-only responses (no journeys), so the caller can omit the field.
//
// When a leg endpoint is a platform with a stop parent, the stop's name and
// id are surfaced (that's the useful disambiguation level for callers); the
// platform coord stays because platforms are more geographically precise.
func extractResolved(body []byte) *resolvedTrip {
	origin, dest, ok := firstLegEndpoints(body)
	if !ok {
		return nil
	}
	return &resolvedTrip{
		Origin:      resolvedFromStopEvent(origin),
		Destination: resolvedFromStopEvent(dest),
	}
}

// resolvedFromStopEvent synthesizes the echo-able shape from an upstream
// stop event. Uses the parent's stop identity when present (that's the
// "station" a human recognizes, not the platform), and falls back to the
// stop event itself otherwise.
func resolvedFromStopEvent(e upstreamStopEvent) resolvedLocation {
	out := resolvedLocation{
		Name:  e.Name,
		ID:    e.ID,
		Type:  e.Type,
		Coord: e.Coord,
	}
	if e.Parent != nil && e.Parent.Type == "stop" {
		out.Name = e.Parent.Name
		out.ID = e.Parent.ID
		out.Type = e.Parent.Type
		// Prefer the platform coord (more precise) but fall back to parent.
		if len(out.Coord) == 0 && len(e.Parent.Coord) >= 2 {
			out.Coord = e.Parent.Coord
		}
	}
	// Derive the short site id from whichever id we have. Try the parent's
	// 8-digit stopId first (clean short form); fall back to GID normalization.
	if e.Parent != nil && e.Parent.Properties.StopID != "" {
		if short, err := normalizeSiteID(e.Parent.Properties.StopID); err == nil {
			out.SiteID = short
		}
	}
	if out.SiteID == 0 && out.ID != "" {
		if short, err := normalizeSiteID(out.ID); err == nil {
			out.SiteID = short
		}
	}
	return out
}

// injectVerboseResolved decodes the raw /v2/trips body, injects the resolved
// echo block at top-level, and re-marshals. Used by verbose=true so callers
// still see the resolved section even when the rest of the payload is
// passed through untrimmed.
func injectVerboseResolved(body []byte) ([]byte, error) {
	resolved := extractResolved(body)
	if resolved == nil {
		return body, nil
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, err
	}
	r, err := json.Marshal(resolved)
	if err != nil {
		return nil, err
	}
	raw["resolved"] = r
	return json.Marshal(raw)
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
			MatchQuality     int       `json:"matchQuality"`
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
			Name:         name,
			Locality:     loc.Parent.Name,
			ID:           loc.ID,
			Type:         loc.Type,
			Coord:        loc.Coord,
			MatchQuality: loc.MatchQuality,
		}
	}
	return out, nil
}

// Exact-match short-circuit thresholds. Upstream tags an exact stop-name
// hit as matchQuality 1000; shadowing candidates typically score 700–900.
// We require at least a 100-point gap between the exact match and the
// next-best candidate before auto-picking, so "Slussen" wins over the
// 850-quality "Slussplan" but a pair tied at 1000 still errors as
// ambiguous (which shouldn't happen in practice — upstream would have
// returned journeys for the first).
const (
	exactMatchQualityMin = 1000
	exactMatchQualityGap = 100
)

// pickExactMatch returns the single candidate that qualifies as an exact
// name match (match_quality >= exactMatchQualityMin and strictly higher
// than every other candidate by at least exactMatchQualityGap). Returns
// (nil, _, false) when no candidate qualifies.
//
// The second return value is the remaining candidates — what would have
// been offered as an ambiguity picker — so callers can attach them as a
// "shadowed" warning on the successful response.
func pickExactMatch(cands []locationCandidate) (*locationCandidate, []locationCandidate, bool) {
	if len(cands) < 2 {
		return nil, nil, false
	}
	var bestIdx = -1
	var bestQ, secondQ int
	for i, c := range cands {
		if c.MatchQuality > bestQ {
			secondQ = bestQ
			bestQ = c.MatchQuality
			bestIdx = i
			continue
		}
		if c.MatchQuality > secondQ {
			secondQ = c.MatchQuality
		}
	}
	if bestIdx < 0 || bestQ < exactMatchQualityMin || bestQ-secondQ < exactMatchQualityGap {
		return nil, nil, false
	}
	winner := cands[bestIdx]
	shadowed := make([]locationCandidate, 0, len(cands)-1)
	for i, c := range cands {
		if i == bestIdx {
			continue
		}
		shadowed = append(shadowed, c)
	}
	return &winner, shadowed, true
}
