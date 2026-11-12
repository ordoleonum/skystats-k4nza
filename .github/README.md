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

Every push to `main` runs `go vet`, the tests, and builds a multi-architecture container,
tagged `main`, `main-<sha>-<time>` and `sha-<short sha>`. The feeder pins one of these in
its `docker.image.versions`.

## Credits

SkyStats was created by [Tom Carman](https://github.com/tomcarman/skystats); adsb.im ships
[wiedehopf's fork](https://github.com/wiedehopf/skystats). The original upstream README is
kept in the root [README.md](../README.md).
