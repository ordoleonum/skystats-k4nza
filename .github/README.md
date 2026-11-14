# SkyStats (K4NZA build)

The SkyStats used inside the [K4NZA ADS-B Feeder](https://github.com/ordoleonum/adsb-feeder-k4nza-public).
It records every aircraft your receiver hears and turns that into statistics: flights seen,
routes, airlines, record holders (fastest, slowest, highest, lowest) and "interesting"
aircraft from the community [plane-alert-db](https://github.com/sdr-enthusiasts/plane-alert-db).

It is based on [SkyStats by Tom Carman](https://github.com/tomcarman/skystats) and is built
automatically into a container (`ghcr.io/ordoleonum/skystats-k4nza`) that the K4NZA feeder
image includes. You don't install it on its own.

## Changes in this build

**Stability**
- Every request to the decoder and to internet services has a timeout, so one hung request
  can't freeze recording.
- Recording runs separately from the slower background jobs, and every job recovers from
  crashes instead of stopping SkyStats.
- Tolerant reading of the decoder's aircraft list: one unexpected field (such as altitude
  "ground") no longer throws away the whole list.
- The in-memory aircraft cache forgets old aircraft, so memory use stays flat.
- A failed plane-alert-db download is retried the next day instead of causing a restart loop.

**Gentle on the SD card and CPU**
- Aircraft in view are saved at most every 30 s (or when a record changes) instead of every 2 s.
- The decoder is read every 5 s; nothing is done when no aircraft are in range.
- Small indexes so background jobs don't scan the whole flight history.
- Hourly housekeeping, optional history limit (`RETENTION_DAYS`, 0 = keep forever) and a
  weekly `VACUUM (ANALYZE)`.
- Every finished day is summarised into small tables (`k4nza_aircraft_seen`,
  `k4nza_callsign_seen`, `k4nza_range_daily`) that the history limit never trims. All-time
  totals, all-time top types and the all-time range outline come from them, so they stay
  correct and fast. The history limit only deletes days that are already summarised.
- Routes are looked up again after 18 hours instead of 1 (from wiedehopf's fork).

**Search**
- A **Search** tab (also opened by `?q=...`, which the feeder's Home page uses): find an
  aircraft by registration, hex code, callsign, type or operator; see every visit with route,
  closest distance, highest altitude and time in view, and download the visits as CSV.
- The aircraft card shows a photo from planespotters.net (credited to the photographer) and
  links to FlightAware.
- The closest distance of every visit is recorded (`min_distance`).
- API: `/api/search`, `/api/search/aircraft/:hex`, `/api/search/aircraft/:hex/visits.csv`.

**Security**
- Requests that change something are refused when the browser says they come from another
  site, and no `Access-Control-Allow-Origin: *` header is sent any more.

**Activity tab**
- **Range by Direction**: the farthest aircraft heard in each 10° slice around the antenna, with
  the all-time best as an outline. Shows where the antenna is blocked.
- **Signal vs Distance**: signal strength (RSSI) against distance, with the median per 10 nm
  and the overload level, for tuning gain and comparing antenna or filter changes.
- The duplicate flight-count charts were removed (they matched the aircraft charts).

**Export**
- An **Export** page: download flights, interesting aircraft, records, registrations and
  routes as CSV (Excel/Sheets), one at a time or all together as a ZIP, with an optional
  date range.

## Building

Every push to `main` runs `go vet`, the unit tests and the database tests (against a throwaway
PostgreSQL started by the workflow), and builds a multi-architecture container,
tagged `main`, `main-<sha>-<time>` and `sha-<short sha>`. The feeder pins one of these in
its `docker.image.versions`.

To run the database tests locally, point `DB_HOST`, `DB_PORT`, `DB_USER`, `DB_PASSWORD` and
`DB_NAME` at an empty PostgreSQL database and run `SKYSTATS_DB_TESTS=1 go test ./core`.

## Credits

SkyStats was created by [Tom Carman](https://github.com/tomcarman/skystats); adsb.im ships
[wiedehopf's fork](https://github.com/wiedehopf/skystats). The original upstream README is
kept in the root [README.md](../README.md).
