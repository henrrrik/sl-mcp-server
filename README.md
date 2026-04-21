# SL MCP Server
[![Go Report Card](https://goreportcard.com/badge/github.com/henrrrik/sl-mcp-server)](https://goreportcard.com/report/github.com/henrrrik/sl-mcp-server)

An [MCP](https://modelcontextprotocol.io/) server for Stockholm's public transit (SL). Gives AI assistants access to real-time departures, trip planning, deviations, and more via SL's open APIs.

Hosted on [Runway](https://www.runway.horse) at https://sl-mcp-server.pqapp.dev

## Tools

| Tool | What it does |
|------|-------------|
| [`trips`](#trips) | Plan a trip between two locations, with active deviations attached to each leg |
| [`departures`](#departures) | Real-time departures from a stop |
| [`stop_finder`](#stop_finder) | Search for stops by name |
| [`sites`](#sites) | List transit sites, with optional name filtering |
| [`deviations`](#deviations) | Traffic disruptions and deviations |
| `lines` | List all transit lines |
| `stop_points` | List stop points (platforms, quays) |
| `transport_authorities` | List transport authorities |
| `system_info` | Timetable validity period |

### `trips`

Plan a trip between two locations. By default the response is a trimmed, LLM-friendly summary with flat per-journey and per-leg fields; pass `verbose=true` to get the full upstream payload.

| Param | Type | Notes |
|---|---|---|
| `origin` | string | Required. Stop name or address. |
| `destination` | string | Required. Stop name or address. |
| `number_of_trips` | number | 1–3, default 3. |
| `time` | string | ISO 8601, e.g. `2026-04-22T09:00:00+02:00`. Defaults to now. |
| `time_mode` | `depart` \| `arrive` | Default `depart`. Only meaningful with `time`. |
| `verbose` | bool | Default false. Return the raw upstream response (including coords, stopSequence, footpath details). |
| `skip_deviations` | bool | Default false. Skip the second `/v1/messages` call that attaches active deviations to each transit leg. |

**Trimmed response shape:**

```json
{
  "journeys": [
    {
      "duration": 1860,
      "interchanges": 1,
      "summary": "Buss 179 → Pendeltåg 43",
      "departure": "2026-04-21T21:03:00Z",
      "arrival": "2026-04-21T21:34:00Z",
      "legs": [
        {
          "mode": "bus",
          "line": "179",
          "direction": "Sollentuna station",
          "from": "Vällingby, Stockholm",
          "to": "Spånga station, Stockholm",
          "departure": "2026-04-21T21:03:00Z",
          "arrival": "2026-04-21T21:14:00Z",
          "duration": 660,
          "realtime": true,
          "deviations": [{ "case_id": 10864959, "header": "…", "details": "…", "from": "…", "upto": "…" }]
        },
        { "mode": "walk", "from": "…", "to": "…", "departure": "…", "arrival": "…", "duration": 240 }
      ]
    }
  ]
}
```

Leg modes: `bus`, `train`, `metro`, `tram`, `ship`, `walk`. Transit legs also get a `line` and `direction`; walking legs omit them. Active deviations whose `scope.lines` matches a leg's line + mode are attached as `deviations[]`.

**Ambiguity handling.** When the broker can't resolve `origin` or `destination` by name, the tool calls `stop_finder` for the ambiguous side(s) and returns candidate pickers instead of journeys:

```jsonc
// Single side ambiguous
{ "error": "ambiguous_origin", "query": "Tumultgränd", "candidates": [
  { "name": "Tumultgränd", "locality": "Vällingby", "id": "9091001000009123", "type": "stop", "coord": [...] }
]}

// Both sides ambiguous
{ "error": "ambiguous_both",
  "origin":      { "query": "…", "candidates": [...] },
  "destination": { "query": "…", "candidates": [...] } }
```

Pass the chosen candidate's `id` (or a more specific name) back in the next call.

### `departures`

Real-time departures from a transit site.

| Param | Type | Notes |
|---|---|---|
| `site_id` | number | Required. Short form (e.g. `9192`) **or** the `180`-prefixed form from `stop_finder` (e.g. `18009192`) — both work. |

### `stop_finder`

Fuzzy-search for stops, stations, and addresses by name.

| Param | Type | Notes |
|---|---|---|
| `name` | string | Required. |

### `sites`

List transit sites (stations/stops). The full upstream list is ~6500 entries — filter with `query` and/or `limit` for discovery.

| Param | Type | Notes |
|---|---|---|
| `query` | string | Case-insensitive substring match on site name. |
| `limit` | number | Cap on result count. `0` or omitted = no cap. |

Without either parameter, the raw upstream list is returned unchanged.

### `deviations`

Traffic disruptions in the SL network. Messages are Swedish-only upstream.

| Param | Type | Notes |
|---|---|---|
| `future` | bool | Include upcoming deviations. |
| `site` | number | Filter by site ID. |
| `line` | number | Filter by line number. |
| `transport_mode` | string | `BUS`, `METRO`, `TRAIN`, `TRAM`, `SHIP`, `FERRY`. |

## Usage

No API key required — SL's integration APIs are open.

The hosted instance is available as an SSE MCP server at `https://sl-mcp-server.pqapp.dev/sse`.

### Claude Desktop

Add to your `claude_desktop_config.json`:

```json
{
  "mcpServers": {
    "sl": {
      "url": "https://sl-mcp-server.pqapp.dev/sse"
    }
  }
}
```

### Self-hosting

```sh
go build -o sl-mcp-server
PORT=5000 ./sl-mcp-server
```

## Development

```sh
go test -race ./...
go vet ./...
gofmt -s -w .
```

## License

MIT
