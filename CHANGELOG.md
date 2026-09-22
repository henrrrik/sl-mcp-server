# Changelog

## Unreleased

### upstream errors (all tools)

- Non-2xx upstream replies are reported as a structured
  `upstream_http_error` with the status, the first 300 characters of the
  body and any `Retry-After`. Previously the body was discarded and the
  tool said only `SL API returned HTTP 400`, although SL's 400s explain
  exactly what was wrong (e.g. the `itd_date` pattern).
- Requests that never get a reply are `upstream_unreachable` or
  `upstream_timeout` instead of a bare Go error string.
- GET requests that fail with a transport error, `429` or `5xx` are
  retried once after 250 ms (or a `Retry-After` of up to 2 s), below the
  response cache so coalesced callers share the retry.
- `trips`: when the broker flags ambiguity and the follow-up `stop_finder`
  call fails, the upstream error is returned. Previously the failure was
  swallowed and the caller got `ambiguous_origin` with an empty
  `candidates` list.
- `trips` and `departures`: a failed best-effort `/v1/messages` fetch now
  adds a `deviations_unavailable` warning (with the upstream error) so a
  missing `deviations` field is distinguishable from "no disruptions".
- Request log lines now classify the outcome: `outcome=ok`,
  `outcome=structured_error code=…` for text results carrying an
  `{"error": …}` envelope (pickers, not-a-stop hints, site-id errors), or
  `error=true code=… status=…` for failures — so operators can tell SL
  outages from bad input.

### argument handling (all tools)

- Site ids for `departures.site_id`, `deviations.site` and
  `trips.origin_id` / `destination_id` go through one shared parser. A
  present-but-unusable value — e.g. a 16-digit GID passed as a JSON number,
  or `9702.5` — is now `invalid_site_id_format` everywhere; `trips`
  previously treated it as absent, which produced a misleading "exactly
  one of origin / origin_id" error, or silently planned by name when
  `origin` was also set (bypassing the mutual-exclusion rule).
- `departures.limit`, `resolve.stop_only` and `nearest_stops.lat` / `lon`
  accept their string forms, as the other tools' parameters already did.
- `nearest_stops.limit=0` now means unlimited (within `radius_m`), as
  `limit=0` does for `sites`, `lines`, `stop_points` and `departures`;
  it previously fell back to the default of 5.
- `trips.time` accepts values without a zone offset (`2026-04-22T09:00`
  or with seconds) as Europe/Stockholm local time, so callers don't need
  to know the current DST offset. The cached location is used instead of
  loading the zone on every call.

### deviations

- `verbose=true` now applies the same in-process `transport_mode` and
  `include_facility` filters as the slim path. Previously the verbose path
  returned before any client-side filtering, so
  `deviations(transport_mode="METRO", verbose=true)` silently returned
  every mode (and every facility notice) with no indication.

### server

- In-memory response cache for the slowly-changing upstream payloads:
  `/v1/sites`, `/v1/lines`, `/v1/stop-points` and
  `/v1/transport-authorities` for 1 hour, `/v1/messages` for 30 seconds.
  Previously every `sites` / `nearest_stops` call re-downloaded the
  1.35 MB site catalog and every `stop_points` call ~8 MB, and every
  `departures` and `trips` call re-fetched the 360 KB deviations
  snapshot. Concurrent misses for one URL are coalesced into a single
  upstream fetch. Real-time endpoints are never cached.
- `departures` and `trips` now fetch `/v1/messages` concurrently with
  their primary request instead of strictly afterwards.
- The HTTP transport keeps up to 16 idle connections per SL host (Go's
  default is 2), so bursts of tool calls reuse connections instead of
  paying a TLS handshake each.
- Streamable HTTP transport served at `/mcp` (stateless) alongside SSE at
  `/sse`. It survives restarts and multiple replicas; SSE sessions live in
  one process and are dropped on every deploy. The root endpoint
  advertises both.
- Graceful shutdown closes SSE streams server-side before stopping the
  listener. Previously `SSEServer.Shutdown` was never wired in, so with
  any SSE client connected every deploy stalled for the full 5 s deadline
  and then cut the streams mid-chunk.
- `ReadHeaderTimeout` (10 s) and `IdleTimeout` (2 min) on the listener.
  `WriteTimeout` stays unset because SSE streams are long-lived.

### trips

- `resolved.*.site_id` is no longer derived from the planner's 18xx
  `stopId` or `9021…` stop-area GIDs. Those encode stop-area ids, which
  collide with the site-id space — the README example echoed `site_id:
  5310` for Stockholm City, but site 5310 is Brunnby Vik, so feeding it
  to `departures` returned the wrong board. `site_id` now appears only
  when the planner echoed a true `909100100…` site GID; otherwise pass
  `resolved.*.name` to `resolve`.
- The POI-drift guard, ambiguity auto-resolution and `exact_match_shadowed`
  warnings now run on every path. Previously `verbose=true` returned before
  any of them (so a query that drifted onto a POI came back as journeys
  from the wrong place), and the body from an ambiguity retry was never
  re-checked — the first body carries no journeys whenever ambiguity is
  flagged, so the guard was a no-op exactly when auto-resolution happened.
- A retry that still comes back ambiguous now returns the structured
  picker with the candidates already fetched, instead of the broker's raw
  `systemMessages` with the collected warnings dropped.
- `verbose=true` responses carry `warnings` alongside `resolved`. Per-leg
  deviation enrichment remains trimmed-shape only, and `skip_deviations`
  is documented as such.

### stop-finder ranking (trips, resolve, stop_finder)

- Every `/v2/stop-finder` call now sends `any_obj_filter_sf=0`. The
  previous value (`2`) restricted upstream to stops only, which made
  `resolve(stop_only=false)`, `stop_finder`'s address/POI results and the
  `origin_not_a_stop` / `destination_not_a_stop` candidate branch in
  `trips` unreachable in practice.
- Results are ranked server-side by `match_quality` with an exact-name
  tie-break (case- and diacritic-insensitive). Upstream does not sort: a
  "Nockeby" query returned the bus stop *Nockeby (på Drottningholmsvägen)*
  ahead of the tram terminus *Nockeby*, both at 1000, and `resolve` crowned
  the wrong one.
- `trips` auto-resolves when the top candidate is the only one named
  exactly as the query, even at a quality tie or narrow gap. Live data has
  *Slussen* / *Slussen (ersättningstrafik)* tied at 1000 and *Alvik*,
  *Sundbyberg*, *Odenplan* within 20–52 points of a runner-up, so by-name
  trips for the busiest stations always ended in a picker. The runner-up is
  still reported in the `exact_match_shadowed` warning.
- `resolve.best.unambiguous` uses the same exact-name rule and is now
  computed against every stop candidate *before* `candidates` is capped
  at 4, so a tied stop in fifth place no longer yields `unambiguous=true`.
- Picker, not-a-stop and shadowed lists are capped at 5 after ranking,
  so the strongest candidates are the ones shown.

### deviations

- Fixes an accessibility regression: `deviations(transport_mode="METRO")`
  silently dropped every FACILITY-category entry (lift outages, escalator
  work, closed entrances) because SL's upstream `transport_mode` filter
  requires a `scope.lines[]` match and facility entries are scoped by
  `stop_areas` only.
- `transport_mode` is now applied client-side, never forwarded upstream.
- New `include_facility` flag (default `false`). When `true`, FACILITY
  entries are preserved through the client-side filter so wheelchair
  users and parents with strollers can see live lift/escalator status.
- The slim response now echoes `categories` as flat `"GROUP:NAME"` strings
  (e.g. `"FACILITY:LIFT"`, `"FACILITY:ESCALATOR"`) so callers can branch
  without a `verbose=true` fetch. Both upstream category shapes (plain
  strings and structured `{group, name}`) are normalized.
- Behavior change: no-arg `deviations()` now excludes FACILITY entries by
  default. This matches the tool description; pass `include_facility=true`
  for the pre-change default.

### lines

- Default shape is now slim: `{id, designation, transport_mode,
  group_of_lines, name}`. `gid`, `transport_authority`, `contractor`,
  and `valid` are dropped from the default response — none of them are
  useful to an LLM trying to answer "what's this line's designation and
  mode?" and together they triple the token footprint of a typical
  `lines(transport_mode="METRO")` call.
- Set `verbose=true` to preserve the full upstream shape.
- Behavior change: the no-arg `lines()` call already capped at 50 (from
  the previous release); it now also returns the slim shape. Document in
  the tool description so callers know to pass `verbose=true` if they
  actually need gid / transport_authority / contractor / valid.

### resolve

- New `stop_only` parameter (default `true`). When `true`, POIs /
  addresses / localities are dropped from both `best` and `candidates`
  — trip planning should never suggest "Järfälla Hyrkart" for a
  "Järfälla kyrka" query. Pass `stop_only=false` to keep non-stop
  candidates (best still stays nil if no stop matches).
- `best.unambiguous: true` is now set when `match_quality >= 1000` and
  no other stop candidate is within 50 points. Callers can skip the
  disambiguation round-trip on clear winners.
- `candidates` now caps at 4 runners-up so the response stays compact
  on heavily-shared names.
- Internals: stop-type classification is now a single `isStopType`
  helper shared with the `trips` ambiguity-resolver, so both tools
  agree on what counts as a stop.

### departures

- `stop_deviations` are now derived before the page-size `limit` is
  applied. Previously the site identity (stop areas / lines) was collected
  from the truncated page, so with the default `limit=20` a disruption on
  a line whose next departure was row 21 silently vanished, and changing
  `limit` changed which disruptions were shown.
- When the site has no upcoming departures at all, upstream's own
  site-scoped `stop_deviations` are returned instead of an always-empty
  array (the site's stop areas can't be inferred without departures —
  exactly the "stop closed" case where the notice matters most). Filters
  that remove every departure still yield an empty intersection.

- `line` filter is now a prefix match, case-insensitive. `"43"` matches
  both pendeltåg 43 and 43X; `"54"` matches the whole 54x bus family.
  Previous behavior was exact match, which required callers to know the
  full designation upfront.
- Default `limit` is now 20 (was unlimited). Pass `limit=0` for the full
  upstream page (typically 35). This trims the typical "when's my next X"
  response by ~40% and keeps compound filters (e.g. line="43" + limit=3)
  working unchanged.

### nearest_stops

- Output shape refactor to match the id naming used by `resolve` and
  `trips.resolved`. Each result is now `{short_id, gid_16, name,
  locality, coord: [lat, lon], distance_m}`.
  - `site_id` → `short_id` (rename, same int).
  - New `gid_16` field (the 16-digit GID form) so callers can plug the
    result into `trips.origin_id` / `trips.destination_id` without a
    manual transform.
  - `lat` + `lon` → `coord` (array) to match the sites/stop_finder
    output shape.
  - New `locality` field, sourced from the sites catalog's `note`.
- `transport_mode` filter is not yet implemented — the sites catalog
  doesn't expose served modes directly, and joining against lines would
  need extra upstream calls. Left as a v1 TODO; filter client-side if
  needed.
- **Breaking** for callers consuming the previous shape: rename
  `site_id` → `short_id`, read `coord[0]`/`coord[1]` instead of
  `lat`/`lon`.

### nearest_stops (new tool)

- `nearest_stops(lat, lon, radius_m=500, limit=5)` returns SL sites
  ordered by haversine distance from the given coordinate. Fetches the
  `/v1/sites` catalog once and filters in-process — no per-request
  geocoding call. Chains cleanly with an external geocoder: hand the
  lat/lon of a user's location (or a street address you've already
  resolved) to this tool, pick a stop, then call `departures` / `trips`
  with the resulting `site_id`.

### resolve (new tool)

- `resolve(query)` is the canonical "turn a name into an id" primitive.
  Returns the single best stop match with all three id forms (short
  `site_id`, 8-digit 18xx `gid_180`, 16-digit `gid_16`) plus a
  `candidates` array of runners-up. POIs and addresses are preserved in
  `candidates` for recovery but never surface as `best`.

### deviations

- `site` now accepts the same four id formats `departures` does: short,
  8-digit 18xx, 9-digit 3BA1CDEFG, and 16-digit GID. Pass as a string;
  16-digit GIDs exceed JS Number.MAX_SAFE_INTEGER.
- Invalid `site` input is echoed back as a decimal string, never as
  scientific notation (`9091001000000000`, not `9.09e+15`).
- New default shape: each entry carries only `{deviation_case_id, header,
  details, publish_from, publish_upto, lines: [designation], stop_areas:
  [name], categories}`. Previously-noisy fields (version, created,
  priority levels, nested transport_authority, href links, every
  language variant) are dropped in the default.
- `verbose=true` preserves the full upstream payload exactly.

### departures

- New optional filters applied after the upstream fetch:
  - `transport_mode` — BUS / METRO / TRAIN / TRAM / SHIP / FERRY / TAXI.
  - `line` — exact match on line designation, case-insensitive.
  - `direction_code` — SL's upstream direction code (1 or 2).
  - `limit` — truncate the filtered result.
- stop_deviations are re-derived from the filtered departures, so a
  `line=40` query doesn't carry a deviation that only affects line 43.
- Invalid `site_id` errors now echo the input as a decimal string (same
  fix as `deviations`).
- New default shape drops per-row redundancy: `stop_area` (uniform across
  the query) and `journey` (internal upstream state) are removed; `line`
  is slimmed to `{designation, transport_mode, group_of_lines}`; the
  `stop_point` retained is just its `designation` (the track/platform
  number).
- `verbose=true` preserves the pre-change shape (stop_area / journey /
  full line intact, rederived stop_deviations still applied).

### lines

- New `designation` filter: prefix match on the line designation. `"54"`
  matches 54, 540, 541, 542, … without pulling in unrelated lines whose
  name happens to contain "54".
- New `group_of_lines` filter: case-insensitive substring match on the
  upstream `group_of_lines` field. Useful for "all Pendeltåg", "all
  Blåbuss", or "all Närtrafiken" shortcuts.
- Default `limit` is now 50 (was unlimited). Pass `limit=0` for the full
  catalog — the ~600-entry raw list is too big for typical LLM contexts.
- Tool description updated to strongly hint at filter usage.

## 1.2.0

### trips

- Exact-match short-circuit: when stop-finder returns one candidate with
  match_quality ≥ 1000 and the next-best is ≥ 100 points lower, the exact
  match auto-resolves instead of erroring as ambiguous. "Solna station"
  now plans on the first call even when "Solna station norra" is in the
  candidate set. Genuine ties (e.g. "Jakobsberg" pendeltåg vs "Jakobsbergs
  centrum" at comparable quality) still return ambiguous_origin.
- Successful auto-resolves attach an `exact_match_shadowed` warning to the
  response listing the lower-quality candidates that were skipped, so
  callers can see what was shadowed and retry with a different name or an
  explicit id if they meant something else.

## 1.1.0

### trips

- Added `origin_id` and `destination_id` parameters. Accept the short-form
  site id, the 8-digit 18xx form, the 9-digit form, or the 16-digit GID;
  bypass fuzzy name resolution and eliminate silent drift onto POIs.
- Exactly one of `origin` / `origin_id` must be set per side (same for
  destination).
- `origin` and `destination` are no longer marked required at the schema
  level, since either form satisfies the requirement.
- Stop-type guard: when the caller provided a name and the planner (or
  stop-finder fallback) resolved it to a non-stop (POI, address, locality),
  the response is now `origin_not_a_stop` / `destination_not_a_stop` with
  the geocoded candidates preserved, instead of silently planning from a
  nearby POI.
- Every successful response now includes a top-level `resolved` block with
  `{name, id, site_id, coord, type}` for both origin and destination.
  Verbose responses get the same injection.
