# Storstockholms Lokaltrafik (SL) MCP Server
[![Go Report Card](https://goreportcard.com/badge/github.com/henrrrik/sl-mcp-server)](https://goreportcard.com/report/github.com/henrrrik/sl-mcp-server)

An [MCP](https://modelcontextprotocol.io/) server for Stockholm's public transit (SL). Gives AI assistants access to real-time departures, trip planning, deviations, and more via SL's open APIs.

Hosted on [Runway](https://www.runway.horse) at https://sl-mcp-server.pqapp.dev

## Tools

| Tool | What it does |
|------|-------------|
| [`trips`](#trips) | Plan a trip between two locations, with active deviations attached to each leg |
| [`departures`](#departures) | Real-time departures from a stop, with in-process filters |
| [`deviations`](#deviations) | Traffic disruptions, with opt-in facility (lift/escalator) alerts |
| [`resolve`](#resolve) | Turn a free-text name into a canonical site id (all three forms) |
| [`nearest_stops`](#nearest_stops) | Find the closest stops to a lat/lon coordinate |
| [`stop_finder`](#stop_finder) | Fuzzy name search across stops, addresses, POIs |
| [`sites`](#sites) | Enumerate SL's ~6500-entry site catalog with substring filter |
| [`lines`](#lines) | Enumerate SL's ~600-entry line catalog with mode / designation filters |
| `stop_points` | Individual platforms, quays, and stands within a site |
| `transport_authorities` | List transport authorities in the region |
| `system_info` | Timetable validity period |

Every tool that returns a large payload has a `verbose=false` default slim shape and a `verbose=true` escape hatch for the raw upstream data.

### Site IDs

SL hands out four id forms for the same stop; all are interchangeable at the server boundary:

- **Short form** — e.g. `9702` for Jakobsberg. Returned by `sites`, `resolve.best.short_id`, `nearest_stops`.
- **8-digit 18xx form** — e.g. `18009702`. Returned by `stop_finder.properties.stopId`, `resolve.best.gid_180`.
- **9-digit 3BA1CDEFG form** — e.g. `300109702`. Documented by Trafiklab.
- **16-digit GID** — e.g. `9091001000009702`. Returned by `stop_finder.id`, `resolve.best.gid_16`, and echoed in `trips.resolved`.

Every tool that takes a site parameter (`departures.site_id`, `deviations.site`, `trips.origin_id` / `destination_id`) normalizes all four forms. Pass 16-digit GIDs as **strings** — they exceed JS `Number.MAX_SAFE_INTEGER` and lose precision if passed as numbers.

### `trips`

Plan a trip between two locations. Returns a trimmed, LLM-friendly summary by default; pass `verbose=true` for the full upstream payload. Every successful response carries a `resolved` block echoing the actual origin/destination the planner used so callers can detect silent drift.

| Param | Type | Notes |
|---|---|---|
| `origin` | string | Stop/location name. Exactly one of `origin` / `origin_id` per side. |
| `origin_id` | string | Any site-id form (see above). Bypasses fuzzy name resolution. Prefer this when you have it. |
| `destination` | string | Same rules as `origin`. |
| `destination_id` | string | Same rules as `origin_id`. |
| `number_of_trips` | number | 1–3, default 3. |
| `time` | string | ISO 8601, e.g. `2026-04-22T09:00:00+02:00`. Defaults to now. |
| `time_mode` | `depart` \| `arrive` | Default `depart`. Only meaningful with `time`. |
| `verbose` | bool | Default false. Return the raw upstream response (coords, stopSequence, footpath details). |
| `skip_deviations` | bool | Default false. Skip the second `/v1/messages` call that attaches active deviations to each transit leg. |

**Trimmed response shape:**

```json
{
  "journeys": [
    {
      "duration": 1860,
      "interchanges": 1,
      "summary": "Buss 179 → Pendeltåg 43",
      "departure": "2026-04-21T21:03:00+02:00",
      "arrival": "2026-04-21T21:34:00+02:00",
      "legs": [
        {
          "mode": "bus",
          "line": "179",
          "direction": "Sollentuna station",
          "from": "Vällingby",
          "to": "Spånga station",
          "departure": "2026-04-21T21:03:00+02:00",
          "arrival": "2026-04-21T21:14:00+02:00",
          "duration": 660,
          "realtime": true,
          "deviations": [{ "case_id": 10864959, "header": "…", "details": "…", "from": "…", "upto": "…" }]
        },
        { "mode": "walk", "from": "…", "to": "…", "departure": "…", "arrival": "…", "duration": 240 }
      ]
    }
  ],
  "resolved": {
    "origin":      { "name": "Vällingby",      "id": "9021001012301000", "type": "stop", "coord": [59.363443, 17.87139] },
    "destination": { "name": "Stockholm City", "id": "9021001000005310", "type": "stop", "coord": [59.330487, 18.059196] }
  }
}
```

Leg modes: `bus`, `train`, `metro`, `tram`, `ship`, `walk`. Transit legs carry `line` and `direction`; walking legs omit them. Times are Europe/Stockholm with an explicit offset.

**Ambiguity and drift handling.** Several guards keep the tool from silently planning the wrong trip:

- **Exact-match short-circuit.** If `stop_finder` returns one candidate with `match_quality ≥ 1000` and the next-best is ≥ 100 points lower, the exact match is used automatically. Shadowed candidates are attached as a warning:

  ```json
  { "warnings": [{
      "code": "exact_match_shadowed", "side": "origin", "query": "Solna station",
      "picked":   { "name": "Solna station",       "id": "…" },
      "shadowed": [ { "name": "Solna station norra" }, { "name": "Ulriksdals station" } ]
  }] }
  ```

- **`origin_not_a_stop` / `destination_not_a_stop`.** When a name resolves to a POI / address / locality (e.g. *"Järfälla Hyrkart"* for a *"Järfälla kyrka"* query), the tool refuses rather than silently planning from the wrong place:

  ```json
  { "error": "origin_not_a_stop", "query": "Järfälla kyrka",
    "candidates": [{ "name": "Järfälla Hyrkart", "type": "poi", "coord": [...] }],
    "hint": "Resolved to a non-stop (POI/address/locality). …" }
  ```

- **`ambiguous_origin` / `ambiguous_destination` / `ambiguous_both`.** When genuine ambiguity remains (e.g. two stations both at `match_quality=1000`), the tool returns candidate pickers instead of journeys. Pass the chosen `id` back as `origin_id` or `destination_id` in the next call.

### `departures`

Real-time departures from a transit site, with in-process filtering and a slim default shape.

| Param | Type | Notes |
|---|---|---|
| `site_id` | string \| number | Required. Any site-id form. Pass 16-digit GIDs as strings. |
| `transport_mode` | string | `BUS`, `METRO`, `TRAIN`, `TRAM`, `SHIP`, `FERRY`, `TAXI`. Case-insensitive. |
| `line` | string | Prefix match on designation (case-insensitive). `"43"` matches `43` and `43X`; `"54"` matches the 54x bus family. |
| `direction_code` | number | SL's upstream direction code (typically 1 or 2). |
| `limit` | number | Default 20. Pass `0` for unlimited (keeps the upstream page size, typically 35). |
| `verbose` | bool | Default false. When true, preserves per-row `stop_area` / `journey` / full `line` object. |

Slim mode drops per-row redundancy (every row belongs to the queried site) and slims `line` to `{designation, transport_mode, group_of_lines}`. `stop_point` is reduced to just its `designation` (the track/platform number).

`stop_deviations` are rebuilt from `/v1/messages` and filtered to scopes that touch this site's stop_areas, stop_points, or lines — they are re-derived **after** client-side filtering, so a `line="40"` query doesn't carry a deviation scoped to line 43. If `/v1/messages` is unreachable the upstream's raw `stop_deviations` are filtered with the same intersection rule as a fallback.

### `deviations`

Traffic disruptions in the SL network. Messages are Swedish-only upstream.

| Param | Type | Notes |
|---|---|---|
| `future` | bool | Include upcoming deviations. |
| `site` | string \| number | Filter by site. Any site-id form. |
| `line` | number | Filter by line number. |
| `transport_mode` | string | `BUS`, `METRO`, `TRAIN`, `TRAM`, `SHIP`, `FERRY`. Applied in-process. |
| `include_facility` | bool | Default false. When true, FACILITY-category entries (lifts, escalators, closed entrances) are kept. Important for accessibility-aware trip planning. |
| `verbose` | bool | Default false. Return the raw upstream payload unchanged. |

**Slim response shape** (verbose=false, per entry):

```json
{
  "deviation_case_id": 10421009,
  "header": "T-Centralen escalator closed",
  "details": "Escalator between platforms 3 and 4 out of service until 2026-05-15",
  "publish_from": "2026-04-20T00:00:00+02:00",
  "publish_upto": "2026-05-15T00:00:00+02:00",
  "lines": ["17", "18"],
  "stop_areas": ["T-Centralen"],
  "categories": ["FACILITY:ESCALATOR"]
}
```

Fields the slim shape drops: `version`, `created`, `priority.*`, `scope.lines[].transport_authority`, `href` link stubs, non-first `message_variants` (Swedish wins). `categories` normalizes both historical upstream shapes (plain `[]string` and structured `[{group, name}]`) to flat `"GROUP:NAME"` strings.

Why `transport_mode` is client-side: SL's upstream filter requires a `scope.lines[]` match, which drops every FACILITY entry (those are scoped by `stop_areas`). Moving the filter into the MCP server lets `include_facility=true` actually do what it says.

### `resolve`

Turn a free-text location query into a canonical SL site. The "I want an id" primitive — prefer this over chaining `stop_finder` → manual id transform → `departures` / `trips`.

| Param | Type | Notes |
|---|---|---|
| `query` | string | Required. Free-form name; fuzzy matching applied. |
| `stop_only` | bool | Default true. When true, POIs / addresses / localities are dropped from both `best` and `candidates`. |

**Response shape:**

```json
{
  "best": {
    "name": "Slussen", "locality": "Stockholm",
    "short_id": 9192, "gid_180": "18009192", "gid_16": "9091001000009192",
    "type": "stop", "coord": [59.320316, 18.072451],
    "match_quality": 1000,
    "unambiguous": true
  },
  "candidates": [
    { "name": "Slussplan", "short_id": 9193, "match_quality": 850, "type": "stop" }
  ]
}
```

`best.unambiguous` is `true` when `match_quality ≥ 1000` AND no other stop candidate is within 50 points. Callers can skip the disambiguation round-trip on clear winners. `candidates` is capped at 4 runners-up.

POI-only queries return `best: null` even with `stop_only=false` — the POI shows up in `candidates` so the caller can recover, but `best` is reserved for transit stops.

### `nearest_stops`

Find SL transit sites nearest to a lat/lon coordinate. Chains cleanly from an external geocoder: resolve an address to lat/lon, then pick the closest stop for `departures` or `trips`.

| Param | Type | Notes |
|---|---|---|
| `lat` | number | Required. WGS84 decimal degrees. |
| `lon` | number | Required. WGS84 decimal degrees. |
| `radius_m` | number | Maximum search distance in metres. Default 500. |
| `limit` | number | Maximum results. Default 5. |

**Response shape** (one entry per stop, sorted by distance ascending):

```json
[
  {
    "short_id": 9001, "gid_16": "9091001000009001",
    "name": "T-Centralen", "locality": "Stockholm",
    "coord": [59.3311, 18.0593],
    "distance_m": 12
  }
]
```

Distance is haversine, rounded to the nearest metre. `transport_mode` filtering isn't implemented in v1 — the `/v1/sites` catalog doesn't surface served modes directly.

### `stop_finder`

Fuzzy, ranked search for stops, stations, and addresses. Tolerates typos and partial names; returns candidates ordered by `match_quality`. Non-stop entries (type != "stop") are kept so addresses and POIs can still resolve.

| Param | Type | Notes |
|---|---|---|
| `name` | string | Required. |

For the "turn a name into an id" use case, prefer `resolve` — it's ID-aware, drops POIs by default, and signals when a match is unambiguous. Use `stop_finder` directly when you specifically want ranked raw candidates.

### `sites`

Enumerate SL's ~6500-entry site catalog. Exact substring matching — for typo-tolerant or ranked search, use `stop_finder` or `resolve`.

| Param | Type | Notes |
|---|---|---|
| `query` | string | Case-insensitive substring match on site name. |
| `limit` | number | Cap on result count. `0` or omitted = no cap. |

### `lines`

Enumerate SL's ~600-entry line catalog. Default shape is slim; pass `verbose=true` for the full upstream fields.

| Param | Type | Notes |
|---|---|---|
| `transport_authority_id` | number | Defaults to 1 (Storstockholms Lokaltrafik). |
| `transport_mode` | string | Restrict to one mode (case-insensitive): `metro`, `bus`, `tram`, `train`, `ferry`, `ship`, `taxi`. |
| `designation` | string | Prefix match on designation. `"54"` matches 54, 540, 541, 549, etc. |
| `group_of_lines` | string | Case-insensitive substring match on `group_of_lines` (e.g. `"Pendeltåg"`, `"Blåbuss"`, `"Närtrafiken"`). |
| `query` | string | Substring match on name OR designation. Broader than `designation`. |
| `limit` | number | Default 50. Pass `0` for the full catalog. |
| `verbose` | bool | Default false. When true, includes `gid`, `transport_authority`, `contractor`, `valid`. |

Slim entries carry `{id, designation, transport_mode, group_of_lines, name}`.

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
gocyclo -over 10 .
```

See [`CHANGELOG.md`](CHANGELOG.md) for release notes.

## License

MIT
