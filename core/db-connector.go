package main

import (
	"context"
	"os"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/rs/zerolog/log"
)

type aircraftCacheEntry struct {
	aircraft    Aircraft
	expiresAt   time.Time
	lastWritten time.Time // last time this aircraft was written to the DB
	pendingMin  float64   // closest distance (km) since the last write, 0 = none
	pendingRssi float64   // signal (dBFS) at pendingMin
}

type postgres struct {
	db                 *pgxpool.Pool
	recentAircraftCache map[string]*aircraftCacheEntry
	speedSamples        map[string]speedSample // K4NZA: last poll's speed readings, see speed_check.go
}

var (
	pgInstance *postgres
	pgOnce     sync.Once
)

func NewPG(ctx context.Context, connString string) (*postgres, error) {
	pgOnce.Do(func() {
		db, err := pgxpool.New(ctx, connString)
		if err != nil {
			log.Error().Err(err).Msg("Unable to connect to database")
		}

		pgInstance = &postgres{
			db:                  db,
			recentAircraftCache: make(map[string]*aircraftCacheEntry),
		}
	})

	return pgInstance, nil
}

func (pg *postgres) Ping(ctx context.Context) error {
	return pg.db.Ping(ctx)
}

func (pg *postgres) Close() {
	pg.db.Close()
}

func GetConnectionUrl() string {

	return "postgres://" +
		os.Getenv("DB_USER") + ":" +
		os.Getenv("DB_PASSWORD") + "@" +
		os.Getenv("DB_HOST") + ":" +
		os.Getenv("DB_PORT") + "/" +
		os.Getenv("DB_NAME")
}
