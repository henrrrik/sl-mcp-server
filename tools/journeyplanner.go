package tools

import (
	"context"
	"fmt"
	"net/url"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
	"github.com/henrrrik/sl-mcp-server/slclient"
)

const journeyPlannerBase = "https://journeyplanner.integration.sl.se"

func SystemInfoTool(client slclient.HTTPDoer) (mcp.Tool, server.ToolHandlerFunc) {
	tool := mcp.NewTool("sl_system_info",
		mcp.WithDescription("Get SL timetable validity period"),
	)

	handler := func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		u := slclient.BuildURL(journeyPlannerBase, "/v2/system-info", nil)
		return fetchJSON(ctx, client, u)
	}

	return tool, handler
}

func StopFinderTool(client slclient.HTTPDoer) (mcp.Tool, server.ToolHandlerFunc) {
	tool := mcp.NewTool("sl_stop_finder",
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
	tool := mcp.NewTool("sl_trips",
		mcp.WithDescription("Plan a trip between two locations in Stockholm"),
		mcp.WithString("origin", mcp.Required(), mcp.Description("Origin stop/location name")),
		mcp.WithString("destination", mcp.Required(), mcp.Description("Destination stop/location name")),
		mcp.WithNumber("number_of_trips", mcp.Description("Number of trips to return (1-3, default 3)")),
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

		u := slclient.BuildURL(journeyPlannerBase, "/v2/trips", params)
		return fetchJSON(ctx, client, u)
	}

	return tool, handler
}
