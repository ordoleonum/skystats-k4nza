package main

// Summary tables that are never trimmed by the history limit (RETENTION_DAYS).
//
// The flight table (aircraft_data) has one row per aircraft visit and can be
// trimmed. Every complete UTC day is "rolled up" into three small tables:
//
//   k4nza_aircraft_seen   one row per aircraft ever seen (visits, first/last seen, closest)
//   k4nza_callsign_seen   one row per callsign + aircraft pair (for search)
//   k4nza_range_daily     farthest distance heard in each 10-degree direction, per day
//
// All-time numbers are then "rolled-up days" + "rows not rolled up yet" (the
// live part, normally less than two days of flights). That keeps them correct
// when old flights are deleted, and fast however big the history grows.
//
// The tables are created with IF NOT EXISTS rather than as numbered migrations,
// so they can never clash with a future upstream SkyStats migration. They can
// always be rebuilt from aircraft_data: a restored stock backup is rolled up
// again on the next start.

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/rs/zerolog/log"
)

const rollupWatermarkKey = "k4nza_rollup_day"

// A day is rolled up once it is this far in the past, so visits that started
// just before midnight have finished updating.
const rollupGrace = 2 * time.Hour

var k4nzaSchemaStatements = []string{
	// closest distance (km) of each visit; NULL for visits recorded before this existed
	`ALTER TABLE aircraft_data ADD COLUMN IF NOT EXISTS min_distance NUMERIC(8,2)`,
	`CREATE TABLE IF NOT EXISTS k4nza_aircraft_seen (
		hex           TEXT PRIMARY KEY,
		t             TEXT,
		r             TEXT,
		first_seen    TIMESTAMPTZ NOT NULL,
		last_seen     TIMESTAMPTZ NOT NULL,
		visits        INTEGER NOT NULL DEFAULT 0,
		routed_visits INTEGER NOT NULL DEFAULT 0,
		min_distance  NUMERIC(8,2)
	)`,
	`CREATE TABLE IF NOT EXISTS k4nza_callsign_seen (
		flight     TEXT NOT NULL,
		hex        TEXT NOT NULL,
		first_seen TIMESTAMPTZ NOT NULL,
		last_seen  TIMESTAMPTZ NOT NULL,
		visits     INTEGER NOT NULL DEFAULT 0,
		PRIMARY KEY (flight, hex)
	)`,
	`CREATE TABLE IF NOT EXISTS k4nza_range_daily (
		day    DATE NOT NULL,
		sector SMALLINT NOT NULL,
		max_nm REAL NOT NULL,
		PRIMARY KEY (day, sector)
	)`,
	// search: prefix lookups on registration and callsign (text_pattern_ops
	// lets LIKE 'ABC%' use the index whatever the database collation)
	`CREATE INDEX IF NOT EXISTS idx_k4nza_seen_regkey
		ON k4nza_aircraft_seen ((regexp_replace(upper(r), '[^A-Z0-9]', '', 'g')) text_pattern_ops)`,
	`CREATE INDEX IF NOT EXISTS idx_k4nza_seen_type ON k4nza_aircraft_seen (t)`,
	// one-time jobs that already ran (name)
	`CREATE TABLE IF NOT EXISTS k4nza_once (
		name TEXT PRIMARY KEY,
		done_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
	)`,
	// search: the group of an aircraft that plane-alert-db doesn't list
	`CREATE INDEX IF NOT EXISTS idx_k4nza_interesting_seen_icao ON interesting_aircraft_seen (icao)`,
	`CREATE INDEX IF NOT EXISTS idx_k4nza_callsign_prefix ON k4nza_callsign_seen (flight text_pattern_ops)`,
	`CREATE INDEX IF NOT EXISTS idx_k4nza_registration_regkey
		ON registration_data ((regexp_replace(upper(registration), '[^A-Z0-9]', '', 'g')) text_pattern_ops)`,
}

func ensureK4nzaSchema(pg *postgres) {
	for _, stmt := range k4nzaSchemaStatements {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
		if _, err := pg.db.Exec(ctx, stmt); err != nil {
			log.Warn().Err(err).Msg("Could not create K4NZA summary table / column")
		}
		cancel()
	}
}

func utcDay(t time.Time) time.Time {
	t = t.UTC()
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC)
}

// rollupWatermark returns the start of the first UTC day that has not been
// rolled up yet. Flights with first_seen >= watermark are the "live" part.
// With no rollup yet (new install, restored stock backup) it is the start of
// the oldest day in aircraft_data, so everything counts as live until the
// backfill has run.
func rollupWatermark(ctx context.Context, q interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}) (time.Time, error) {
	var epoch int64
	err := q.QueryRow(ctx, `SELECT stat_value FROM cached_stats WHERE stat_key = $1`, rollupWatermarkKey).Scan(&epoch)
	if err == nil {
		return time.Unix(epoch, 0).UTC(), nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return time.Time{}, err
	}
	var oldest *time.Time
	if err := q.QueryRow(ctx, `SELECT MIN(first_seen) FROM aircraft_data`).Scan(&oldest); err != nil {
		return time.Time{}, err
	}
	if oldest == nil {
		return utcDay(time.Now()), nil
	}
	return utcDay(*oldest), nil
}

// rangeSectorsSQL finds the farthest distance (nm) per 10-degree sector for the
// visits that started in [$5, $6). Two points per visit are used: where it was
// first heard (readsb's r_dst / r_dir) and where it was last heard.
// $1 station lat, $2 station lon, $3 km per nm, $4 max plausible nm.
const rangeSectorsSQL = `
	WITH a AS (
		SELECT r_dst, r_dir, last_seen_distance, last_seen_lat, last_seen_lon
		FROM aircraft_data
		WHERE first_seen >= $5 AND first_seen < $6
	), pts AS (
		SELECT r_dst::float8 AS nm, r_dir::float8 AS dir
		FROM a
		WHERE r_dst > 0 AND r_dir IS NOT NULL
		UNION ALL
		SELECT last_seen_distance::float8 / $3 AS nm,
		       atan2d(
		           sind(last_seen_lon::float8 - $2) * cosd(last_seen_lat::float8),
		           cosd($1) * sind(last_seen_lat::float8)
		             - sind($1) * cosd(last_seen_lat::float8) * cosd(last_seen_lon::float8 - $2)
		       ) AS dir
		FROM a
		WHERE last_seen_distance > 0
		  AND last_seen_lat IS NOT NULL AND last_seen_lon IS NOT NULL
		  AND NOT (last_seen_lat = 0 AND last_seen_lon = 0)
	)
	SELECT floor(mod((dir + 360)::numeric, 360) / 10)::int AS sector, max(nm)
	FROM pts
	WHERE nm < $4
	GROUP BY 1`

func maxPlausibleNm() float64 {
	maxNm := getRadius() / kmPerNm
	if maxNm <= 0 {
		maxNm = 1000
	}
	return maxNm
}

// rollupOneDay folds one complete UTC day into the summary tables and moves
// the watermark past it, all in one transaction (so a failure changes nothing).
func rollupOneDay(ctx context.Context, pg *postgres, day time.Time) error {
	next := day.AddDate(0, 0, 1)
	tx, err := pg.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	if _, err := tx.Exec(ctx, `
		INSERT INTO k4nza_aircraft_seen AS s (hex, t, r, first_seen, last_seen, visits, routed_visits, min_distance)
		SELECT a.hex,
		       MAX(NULLIF(a.t, '')), MAX(NULLIF(a.r, '')),
		       MIN(a.first_seen), MAX(a.last_seen), COUNT(*),
		       COUNT(rd.route_callsign),
		       MIN(LEAST(a.min_distance, a.last_seen_distance, NULLIF(a.r_dst, 0) * 1.852))
		FROM aircraft_data a
		LEFT JOIN route_data rd ON rd.route_callsign = a.flight
		WHERE a.first_seen >= $1 AND a.first_seen < $2 AND a.hex IS NOT NULL
		GROUP BY a.hex
		ON CONFLICT (hex) DO UPDATE SET
			t = COALESCE(EXCLUDED.t, s.t),
			r = COALESCE(EXCLUDED.r, s.r),
			first_seen = LEAST(s.first_seen, EXCLUDED.first_seen),
			last_seen = GREATEST(s.last_seen, EXCLUDED.last_seen),
			visits = s.visits + EXCLUDED.visits,
			routed_visits = s.routed_visits + EXCLUDED.routed_visits,
			min_distance = LEAST(s.min_distance, EXCLUDED.min_distance)`, day, next); err != nil {
		return err
	}

	if _, err := tx.Exec(ctx, `
		INSERT INTO k4nza_callsign_seen AS s (flight, hex, first_seen, last_seen, visits)
		SELECT flight, hex, MIN(first_seen), MAX(last_seen), COUNT(*)
		FROM aircraft_data
		WHERE first_seen >= $1 AND first_seen < $2 AND flight IS NOT NULL AND flight <> '' AND hex IS NOT NULL
		GROUP BY flight, hex
		ON CONFLICT (flight, hex) DO UPDATE SET
			first_seen = LEAST(s.first_seen, EXCLUDED.first_seen),
			last_seen = GREATEST(s.last_seen, EXCLUDED.last_seen),
			visits = s.visits + EXCLUDED.visits`, day, next); err != nil {
		return err
	}

	if _, err := tx.Exec(ctx, `
		INSERT INTO k4nza_range_daily (day, sector, max_nm)
		SELECT $7::date, sector, max FROM (`+rangeSectorsSQL+`) r
		WHERE sector >= 0 AND sector < 36
		ON CONFLICT (day, sector) DO UPDATE SET max_nm = GREATEST(k4nza_range_daily.max_nm, EXCLUDED.max_nm)`,
		getLat(), getLon(), kmPerNm, maxPlausibleNm(), day, next, day); err != nil {
		return err
	}

	if _, err := tx.Exec(ctx, `
		INSERT INTO cached_stats (stat_key, stat_value, last_updated) VALUES ($1, $2, NOW())
		ON CONFLICT (stat_key) DO UPDATE SET stat_value = EXCLUDED.stat_value, last_updated = NOW()`,
		rollupWatermarkKey, next.Unix()); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// runRollup rolls up every complete day not done yet. The first run on an
// existing database works through its whole history one day at a time, with
// short pauses so recording is never held up.
// one rollup at a time, or a day could be counted twice
var rollupMu sync.Mutex

func runRollup(pg *postgres) {
	rollupMu.Lock()
	defer rollupMu.Unlock()
	rollupLocked(pg)
}

// tryRollup runs a rollup unless one is already running (e.g. the first
// backfill of a large history); it reports whether the summary is current.
func tryRollup(pg *postgres) bool {
	if !rollupMu.TryLock() {
		return false
	}
	defer rollupMu.Unlock()
	return rollupLocked(pg)
}

func rollupLocked(pg *postgres) bool {
	ctx := context.Background()
	wctx, cancel := context.WithTimeout(ctx, time.Minute)
	day, err := rollupWatermark(wctx, pg.db)
	cancel()
	if err != nil {
		log.Error().Err(err).Msg("summary: could not read rollup position")
		return false
	}
	done := 0
	for !day.AddDate(0, 0, 1).Add(rollupGrace).After(time.Now()) {
		dctx, cancel := context.WithTimeout(ctx, 10*time.Minute)
		err := rollupOneDay(dctx, pg, day)
		cancel()
		if err != nil {
			log.Error().Err(err).Msgf("summary: rollup of %s failed (will retry)", day.Format("2006-01-02"))
			return false
		}
		day = day.AddDate(0, 0, 1)
		done++
		if done%30 == 0 {
			log.Info().Msgf("summary: rolled up %d days so far (up to %s)", done, day.Format("2006-01-02"))
		}
		time.Sleep(200 * time.Millisecond)
	}
	if done > 0 {
		log.Info().Msgf("summary: rolled up %d day(s); all-time statistics are kept even if old flights are deleted", done)
	}
	return true
}

// liveAircraftCTE / allAircraftCTE: every aircraft ever seen, combining the
// rolled-up table with the live (not yet rolled up) flights. $1 = watermark.
const allAircraftCTE = `
	live AS (
		SELECT hex, MAX(NULLIF(t, '')) AS t, MAX(NULLIF(r, '')) AS r,
		       MIN(first_seen) AS first_seen, MAX(last_seen) AS last_seen, COUNT(*) AS visits,
		       MIN(LEAST(min_distance, last_seen_distance, NULLIF(r_dst, 0) * 1.852)) AS min_distance
		FROM aircraft_data
		WHERE first_seen >= $1 AND hex IS NOT NULL
		GROUP BY hex
	), allac AS (
		SELECT COALESCE(s.hex, l.hex) AS hex,
		       COALESCE(l.t, s.t) AS t,
		       COALESCE(l.r, s.r) AS r,
		       LEAST(s.first_seen, l.first_seen) AS first_seen,
		       GREATEST(s.last_seen, l.last_seen) AS last_seen,
		       COALESCE(s.visits, 0) + COALESCE(l.visits, 0) AS visits,
		       LEAST(s.min_distance, l.min_distance) AS min_distance
		FROM k4nza_aircraft_seen s
		FULL OUTER JOIN live l ON l.hex = s.hex
	)`

// allTimeTotals returns flights (visits), distinct aircraft and visits with a
// known route, over the whole history including trimmed days.
func allTimeTotals(ctx context.Context, pg *postgres) (flights, aircraft, routed int64, err error) {
	w, err := rollupWatermark(ctx, pg.db)
	if err != nil {
		return 0, 0, 0, err
	}
	err = pg.db.QueryRow(ctx, `
		SELECT
			(SELECT COALESCE(SUM(visits), 0) FROM k4nza_aircraft_seen)
			  + (SELECT COUNT(*) FROM aircraft_data WHERE first_seen >= $1),
			(SELECT COUNT(*) FROM k4nza_aircraft_seen)
			  + (SELECT COUNT(DISTINCT a.hex) FROM aircraft_data a
			     WHERE a.first_seen >= $1
			       AND NOT EXISTS (SELECT 1 FROM k4nza_aircraft_seen s WHERE s.hex = a.hex)),
			(SELECT COALESCE(SUM(routed_visits), 0) FROM k4nza_aircraft_seen)
			  + (SELECT COUNT(*) FROM aircraft_data a
			     JOIN route_data rd ON rd.route_callsign = a.flight
			     WHERE a.first_seen >= $1)`, w).Scan(&flights, &aircraft, &routed)
	return
}

// allTimeRangeSectors: farthest distance per sector over the whole history.
func allTimeRangeSectors(ctx context.Context, pg *postgres) ([]float64, error) {
	w, err := rollupWatermark(ctx, pg.db)
	if err != nil {
		return nil, err
	}
	sectors, err := queryRangeSectorsBetween(ctx, pg, w, time.Now().Add(24*time.Hour))
	if err != nil {
		return nil, err
	}
	rows, err := pg.db.Query(ctx, `SELECT sector, MAX(max_nm)::float8 FROM k4nza_range_daily GROUP BY sector`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var sector int
		var nm float64
		if err := rows.Scan(&sector, &nm); err != nil {
			return nil, err
		}
		if sector >= 0 && sector < rangeSectors && nm > sectors[sector] {
			sectors[sector] = roundTenth(nm)
		}
	}
	return sectors, rows.Err()
}

func roundTenth(v float64) float64 {
	return float64(int64(v*10+0.5)) / 10
}

// rollupCutoff is the newest first_seen the history limit may delete: only
// days already rolled up, so all-time numbers never lose anything.
func rollupCutoff(ctx context.Context, pg *postgres) (time.Time, error) {
	return rollupWatermark(ctx, pg.db)
}
