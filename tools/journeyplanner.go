package tools

import (
	"context"
	"fmt"
	"net/url"
	"time"

	"github.com/henrrrik/sl-mcp-server/slclient"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

const journeyPlannerBase = "https://journeyplanner.integration.sl.se"

// SL's journey planner interprets itd_time as Europe/Stockholm local time.
const stockholmTZ = "Europe/Stockholm"

func SystemInfoTool(client slclient.HTTPDoer) (mcp.Tool, server.ToolHandlerFunc) {
	tool := mcp.NewTool("system_info",
		mcp.WithDescription("Get SL timetable validity period"),
	)

	handler := func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		u := slclient.BuildURL(journeyPlannerBase, "/v2/system-info", nil)
		return fetchJSON(ctx, client, u)
	}

	return tool, handler
}

func StopFinderTool(client slclient.HTTPDoer) (mcp.Tool, server.ToolHandlerFunc) {
	tool := mcp.NewTool("stop_finder",
		mcp.WithDescription("Find SL stops/stations by name"),
		mcp.WithString("name", mcp.Required(), mcp.Description("Stop name to search for")),
	)

	handler := func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		name := request.GetString("name", "")
		if name == "" {
			return mcp.NewToolResultError("name is required"), nil
		}

		params := url.Values{
			"name_sf":           {name},
			"type_sf":           {"any"},
			"any_obj_filter_sf": {"2"},
		}

		u := slclient.BuildURL(journeyPlannerBase, "/v2/stop-finder", params)
		return fetchJSON(ctx, client, u)
	}

	return tool, handler
}

func TripsTool(client slclient.HTTPDoer) (mcp.Tool, server.ToolHandlerFunc) {
	tool := mcp.NewTool("trips",
		mcp.WithDescription("Plan a trip between two locations in Stockholm"),
		mcp.WithString("origin", mcp.Required(), mcp.Description("Origin stop/location name")),
		mcp.WithString("destination", mcp.Required(), mcp.Description("Destination stop/location name")),
		mcp.WithNumber("number_of_trips", mcp.Description("Number of trips to return (1-3, default 3)")),
		mcp.WithString("time", mcp.Description("ISO 8601 departure/arrival time (e.g. 2026-04-22T09:00:00+02:00). Defaults to now.")),
		mcp.WithString("time_mode", mcp.Description("'depart' or 'arrive' (default 'depart'). Only meaningful when 'time' is set.")),
	)

	handler := func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		origin := request.GetString("origin", "")
		destination := request.GetString("destination", "")
		if origin == "" || destination == "" {
			return mcp.NewToolResultError("origin and destination are required"), nil
		}

		numTrips := request.GetInt("number_of_trips", 3)
		if numTrips < 1 {
			numTrips = 1
		} else if numTrips > 3 {
			numTrips = 3
		}

		params := url.Values{
			"type_origin":          {"any"},
			"name_origin":          {origin},
			"type_destination":     {"any"},
			"name_destination":     {destination},
			"calc_number_of_trips": {fmt.Sprintf("%d", numTrips)},
		}

		if errResult := applyTripTime(request, params); errResult != nil {
			return errResult, nil
		}

		u := slclient.BuildURL(journeyPlannerBase, "/v2/trips", params)
		return fetchJSON(ctx, client, u)
	}

	return tool, handler
}

// applyTripTime translates the public "time" / "time_mode" arguments into the
// upstream's snake_case EFA parameters (itd_date, itd_time, itd_trip_date_time_dep_arr).
// Returns a non-nil error result if the inputs are invalid; nil on success or if
// no time was provided (leaving the broker to default to "now").
func applyTripTime(request mcp.CallToolRequest, params url.Values) *mcp.CallToolResult {
	timeStr := request.GetString("time", "")
	modeStr := request.GetString("time_mode", "")

	if timeStr == "" {
		if modeStr != "" {
			return mcp.NewToolResultError("time_mode requires 'time' to be set")
		}
		return nil
	}

	t, err := time.Parse(time.RFC3339, timeStr)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("invalid time %q: %v", timeStr, err))
	}

	loc, err := time.LoadLocation(stockholmTZ)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("failed to load %s timezone: %v", stockholmTZ, err))
	}
	local := t.In(loc)

	if modeStr == "" {
		modeStr = "depart"
	}
	var depArr string
	switch modeStr {
	case "depart":
		depArr = "dep"
	case "arrive":
		depArr = "arr"
	default:
		return mcp.NewToolResultError(fmt.Sprintf("time_mode must be 'depart' or 'arrive', got %q", modeStr))
	}

	params.Set("itd_date", local.Format("20060102"))
	params.Set("itd_time", local.Format("1504"))
	params.Set("itd_trip_date_time_dep_arr", depArr)
	return nil
}
