# Changelog

## Unreleased

### nearest_stops (new tool)

- `nearest_stops(lat, lon, radius_m=500, limit=5)` returns SL sites
  ordered by haversine distance from the given coordinate. Fetches the
  `/v1/sites` catalog once and filters in-process — no per-request
  geocoding call. Chains cleanly with an external geocoder: hand the
  lat/lon of a user's location (or a street address you've already
  resolved) to this tool, pick a stop, then call `departures` / `trips`
  with the resulting `site_id`.

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
