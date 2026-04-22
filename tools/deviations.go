package tools

import (
	"context"
	"errors"
	"fmt"
	"net/url"

	"github.com/henrrrik/sl-mcp-server/slclient"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

const deviationsBase = "https://deviations.integration.sl.se"

func DeviationsTool(client slclient.HTTPDoer) (mcp.Tool, server.ToolHandlerFunc) {
	tool := mcp.NewTool("deviations",
		mcp.WithDescription("Get SL traffic deviations and disruptions in Stockholm public transport."),
		mcp.WithBoolean("future", mcp.Description("Include future deviations")),
		mcp.WithString("site", mcp.Description("Filter by site. Accepts the short-form site_id (e.g. \"9001\" for T-Centralen), the 8-digit 18xx form, the 9-digit 3BA1CDEFG form, or the 16-digit GID — all normalized to the short form before the upstream call. Pass as a string; 16-digit GIDs exceed JS Number.MAX_SAFE_INTEGER.")),
		mcp.WithNumber("line", mcp.Description("Filter by line number")),
		mcp.WithString("transport_mode", mcp.Description("Filter by mode: BUS, METRO, TRAIN, TRAM, SHIP, FERRY")),
	)

	handler := func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		params := url.Values{}

		if v := request.GetBool("future", false); v {
			params.Set("future", "true")
		}
		if raw, present := request.GetArguments()["site"]; present {
			if errResult := applyDeviationsSiteFilter(raw, params); errResult != nil {
				return errResult, nil
			}
		}
		if v := request.GetInt("line", 0); v != 0 {
			params.Set("line", fmt.Sprintf("%d", v))
		}
		if v := request.GetString("transport_mode", ""); v != "" {
			params.Set("transport_mode", v)
		}

		u := slclient.BuildURL(deviationsBase, "/v1/messages", params)
		return fetchJSON(ctx, client, u)
	}

	return tool, handler
}

// applyDeviationsSiteFilter normalizes a raw "site" argument (number or
// string in any of the four recognized formats) to the short-form int and
// sets it on params. Returns a structured siteID error result on invalid
// input.
func applyDeviationsSiteFilter(raw any, params url.Values) *mcp.CallToolResult {
	input := coerceSiteIDArg(raw)
	if input == "" {
		return mcp.NewToolResultError((&siteIDError{
			Code:  errInvalidSiteIDFormat,
			Input: formatSiteIDArgForError(raw),
		}).asJSON())
	}
	short, err := normalizeSiteID(input)
	if err != nil {
		var se *siteIDError
		if errors.As(err, &se) {
			return mcp.NewToolResultError(se.asJSON())
		}
		return mcp.NewToolResultError(err.Error())
	}
	params.Set("site", fmt.Sprintf("%d", short))
	return nil
}
