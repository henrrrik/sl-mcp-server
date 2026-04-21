package tools

import (
	"encoding/json"
	"strings"
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
	Mode      string `json:"mode"`
	Line      string `json:"line,omitempty"`
	Direction string `json:"direction,omitempty"`
	From      string `json:"from,omitempty"`
	To        string `json:"to,omitempty"`
	Departure string `json:"departure,omitempty"`
	Arrival   string `json:"arrival,omitempty"`
	Duration  int    `json:"duration"`
	Realtime  bool   `json:"realtime,omitempty"`
}

// trimTrips reshapes a raw upstream trips response into the trimmed form.
// Returns the original bytes unchanged if the response has no journeys
// (e.g. error-only responses from the upstream broker), so callers still
// see systemMessages verbatim.
func trimTrips(raw []byte) ([]byte, error) {
	var up upstreamTrips
	if err := json.Unmarshal(raw, &up); err != nil {
		return nil, err
	}
	if len(up.Journeys) == 0 {
		return raw, nil
	}

	out := trimmedTrips{
		Journeys:       make([]trimmedJourney, len(up.Journeys)),
		SystemMessages: up.SystemMessages,
	}
	for i, j := range up.Journeys {
		out.Journeys[i] = trimJourney(j)
	}
	return json.Marshal(out)
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
