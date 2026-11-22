package main

import (
	"context"
	"testing"
	"time"
)

// Records are judged on finished visits only, and Slowest skips aircraft that
// never left the ground.
func TestRecordsUseFinishedVisits(t *testing.T) {
	pg := testPG(t)
	ctx := context.Background()
	if _, err := pg.db.Exec(ctx, `TRUNCATE fastest_aircraft, slowest_aircraft, highest_aircraft, lowest_aircraft`); err != nil {
		t.Fatal(err)
	}

	add := func(hex string, lastSeen time.Time, altBaro int, gs float64) {
		t.Helper()
		first := lastSeen.Add(-10 * time.Minute)
		if _, err := pg.db.Exec(ctx, `
			INSERT INTO aircraft_data (hex, flight, r, t, first_seen, first_seen_epoch, last_seen, last_seen_epoch,
			                           alt_baro, alt_geom, gs, ias, tas)
			VALUES ($1, '', $1, 'C172', $2, $3, $4, $5, $6, 0, $7, 0, 0)`,
			hex, first, first.Unix(), lastSeen, lastSeen.Unix(), altBaro, gs); err != nil {
			t.Fatalf("insert %s: %v", hex, err)
		}
	}
	finished := time.Now().Add(-30 * time.Minute)
	add("aa0001", finished, 3000, 95)            // finished flight
	add("aa0002", time.Now(), 2500, 4)           // still in view: taxiing so far, not judged yet
	add("aa0003", finished, 0, 6)                // only ever on the ground
	add("aa0004", time.Now().Add(-5*time.Minute), 1200, 60) // gone 5 min: may still come back
	add("~297e16", finished, 1400, 72)          // unidentified (TIS-B): never a record

	updateMeasurementStatistics(pg)

	count := func(table, hex string) int {
		var n int
		if err := pg.db.QueryRow(ctx, `SELECT COUNT(*) FROM `+table+` WHERE hex = $1`, hex).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}
	if count("slowest_aircraft", "aa0001") != 1 || count("highest_aircraft", "aa0001") != 1 {
		t.Errorf("finished visit was not judged")
	}
	for _, hex := range []string{"aa0002", "aa0004"} {
		for _, table := range []string{"slowest_aircraft", "lowest_aircraft", "highest_aircraft", "fastest_aircraft"} {
			if count(table, hex) != 0 {
				t.Errorf("unfinished visit %s judged into %s", hex, table)
			}
		}
	}
	for _, table := range []string{"slowest_aircraft", "lowest_aircraft", "highest_aircraft", "fastest_aircraft"} {
		if count(table, "~297e16") != 0 {
			t.Errorf("unidentified target on %s", table)
		}
	}
	if count("slowest_aircraft", "aa0003") != 0 {
		t.Errorf("aircraft that never flew is on the Slowest list")
	}
	var processed bool
	if err := pg.db.QueryRow(ctx, `SELECT slowest_aircraft_processed FROM aircraft_data WHERE hex = 'aa0002'`).Scan(&processed); err != nil {
		t.Fatal(err)
	}
	if processed {
		t.Errorf("unfinished visit marked processed - it would never be judged again")
	}
}

// A row with empty (NULL) columns must not stop the record job: it used to
// drop the whole batch, and the same batch came up again every run.
func TestRecordsSurviveNullColumns(t *testing.T) {
	pg := testPG(t)
	ctx := context.Background()
	finished := time.Now().Add(-30 * time.Minute)
	first := finished.Add(-5 * time.Minute)
	if _, err := pg.db.Exec(ctx, `
		INSERT INTO aircraft_data (hex, first_seen, first_seen_epoch, last_seen, last_seen_epoch, alt_baro, gs)
		VALUES ('ab0001', $1, $2, $3, $4, 2000, 90)`,
		first, first.Unix(), finished, finished.Unix()); err != nil {
		t.Fatal(err)
	}
	updateMeasurementStatistics(pg)
	var done bool
	if err := pg.db.QueryRow(ctx, `SELECT slowest_aircraft_processed FROM aircraft_data WHERE hex = 'ab0001'`).Scan(&done); err != nil {
		t.Fatal(err)
	}
	if !done {
		t.Fatalf("row with NULL columns was never processed")
	}
	if len(unprocessedInteresting(pg)) == 0 {
		t.Fatalf("interesting job could not read a row with NULL columns")
	}
}
