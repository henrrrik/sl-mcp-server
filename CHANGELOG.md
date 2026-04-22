# Changelog

## Unreleased

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
