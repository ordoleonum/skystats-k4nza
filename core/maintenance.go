package main

// K4NZA: hourly housekeeping.
//
//   - Optional data retention: set RETENTION_DAYS (e.g. 365) to delete flight
//     rows older than that. 0 or unset keeps everything forever (stock
//     behaviour). Record holders (fastest/slowest/highest/lowest) and the
//     interesting-aircraft log are always kept.
//   - Logs the database size so growth is visible in the container log.
//
// Deletes run in small batches with pauses so they never lock the table or
// saturate the SD card.

import (
	"context"
	"os"
	"strconv"
	"time"

	"github.com/rs/zerolog/log"
)

const (
	retentionBatchSize = 5000
	retentionMaxBatches = 200 // at most 1M rows per hourly run
)

func getRetentionDays() int {
	days, err := strconv.Atoi(os.Getenv("RETENTION_DAYS"))
	if err != nil || days < 0 {
		return 0
	}
	return days
}

func runMaintenance(pg *postgres) {
	logDatabaseSize(pg)
	weeklyVacuum(pg)

	days := getRetentionDays()
	if days == 0 {
		return
	}
	// never accept a tiny value by accident
	if days < 7 {
		days = 7
	}

	total := 0
	for i := 0; i < retentionMaxBatches; i++ {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		tag, err := pg.db.Exec(ctx, `
			DELETE FROM aircraft_data
			WHERE id IN (
				SELECT id FROM aircraft_data
				WHERE last_seen < NOW() - make_interval(days => $1)
				LIMIT $2
			)`, days, retentionBatchSize)
		cancel()
		if err != nil {
			log.Error().Err(err).Msg("maintenance: retention delete failed")
			break
		}
		n := int(tag.RowsAffected())
		total += n
		if n < retentionBatchSize {
			break
		}
		time.Sleep(2 * time.Second)
	}

	if total > 0 {
		log.Info().Msgf("maintenance: removed %d flight rows older than %d days (RETENTION_DAYS)", total, days)
	}
}

func logDatabaseSize(pg *postgres) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	var size string
	var rows int64
	err := pg.db.QueryRow(ctx, `
		SELECT pg_size_pretty(pg_database_size(current_database())),
		       COALESCE((SELECT n_live_tup FROM pg_stat_user_tables WHERE relname = 'aircraft_data'), 0)`).
		Scan(&size, &rows)
	if err != nil {
		log.Warn().Err(err).Msg("maintenance: could not read database size")
		return
	}
	log.Info().Msgf("maintenance: database size %s, about %d flight rows, retention %d days (0 = keep forever)",
		size, rows, getRetentionDays())
}

// ensureK4nzaIndexes creates small partial indexes for the background jobs
// that look for "not yet processed" rows every 30 s - 5 min. Without them those
// lookups read the entire (ever-growing) flight table, so SkyStats became
// slower and heavier on the SD card the longer it ran.
//
// These are applied here with IF NOT EXISTS rather than as a numbered
// migration so they can never collide with future upstream migrations.
func ensureK4nzaIndexes(pg *postgres) {
	statements := []string{
		`CREATE INDEX IF NOT EXISTS idx_k4nza_unprocessed_registration
			ON aircraft_data (first_seen) WHERE registration_processed = false`,
		`CREATE INDEX IF NOT EXISTS idx_k4nza_unprocessed_route
			ON aircraft_data (first_seen) WHERE route_processed = false`,
		`CREATE INDEX IF NOT EXISTS idx_k4nza_unprocessed_interesting
			ON aircraft_data (first_seen) WHERE interesting_processed = false`,
		`CREATE INDEX IF NOT EXISTS idx_k4nza_unprocessed_motion
			ON aircraft_data (first_seen)
			WHERE lowest_aircraft_processed = false OR
			      highest_aircraft_processed = false OR
			      fastest_aircraft_processed = false OR
			      slowest_aircraft_processed = false`,
		// note: no extra last_seen index - the existing
		// idx_aircraft_data_last_seen_distance already serves retention, and
		// every extra index on a frequently updated column adds SD writes
	}
	for _, stmt := range statements {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
		if _, err := pg.db.Exec(ctx, stmt); err != nil {
			log.Warn().Err(err).Msg("Could not create K4NZA index (SkyStats still works, just slower)")
		}
		cancel()
	}
}

// weeklyVacuum runs VACUUM (ANALYZE) on the whole database at most once a week.
// Autovacuum already runs, but on a small, busy table it can fall behind;
// this reclaims space from old row versions and refreshes the planner's
// statistics so queries stay fast as the data grows. Unlike VACUUM FULL it
// does not lock tables or rewrite them (which would be hard on the SD card).
func weeklyVacuum(pg *postgres) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	var days float64
	err := pg.db.QueryRow(ctx, `
		SELECT COALESCE(EXTRACT(EPOCH FROM NOW() - MAX(last_vacuum)) / 86400, 9999)
		FROM pg_stat_user_tables`).Scan(&days)
	cancel()
	if err != nil {
		log.Error().Err(err).Msg("maintenance: could not check last vacuum")
		return
	}
	if days < 7 {
		return
	}
	start := time.Now()
	ctx, cancel = context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()
	if _, err := pg.db.Exec(ctx, "VACUUM (ANALYZE)"); err != nil {
		log.Error().Err(err).Msg("maintenance: weekly vacuum failed")
		return
	}
	log.Info().Msgf("maintenance: weekly database tidy-up done in %s", time.Since(start).Round(time.Second))
}
