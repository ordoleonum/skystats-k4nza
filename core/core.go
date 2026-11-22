package main

import (
	"context"
	"flag"
	"os"
	"path/filepath"
	"time"

	"github.com/joho/godotenv"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"github.com/sevlyar/go-daemon"
)

func main() {

	checkFlags()

	// Initialize logger
	consoleWriter := zerolog.ConsoleWriter{
		Out:          os.Stderr,
		TimeFormat:   "2006-01-02 15:04:05",
		TimeLocation: time.Local,
	}
	log.Logger = log.Output(consoleWriter)

	// Load .env file
	if err := godotenv.Load("../.env"); err != nil {
		if err := godotenv.Load(); err != nil {
			log.Info().Msg("No .env file found, using environment variables")
		}
	}

	// Set log level
	if os.Getenv("DOCKER_ENV") == "true" {
		setLogLevel()
	}

	// If running outside of docker, run as a daemon
	if os.Getenv("DOCKER_ENV") != "true" {

		execPath, _ := os.Executable()
		execDir := filepath.Dir(execPath)

		cntxt := &daemon.Context{
			PidFileName: filepath.Join(execDir, "skystats.pid"),
			PidFilePerm: 0644,
			LogFileName: filepath.Join(execDir, "skystats.log"),
			LogFilePerm: 0640,
			WorkDir:     execDir,
			Umask:       027,
		}

		d, err := cntxt.Reborn()

		if err != nil {
			log.Fatal().Err(err).Msg("Failed to launch daemon")
		}
		if d != nil {
			return
		}
		defer cntxt.Release()

		// when running as daemon, logs are written to file, disable ansi colors + set log level
		consoleWriter.NoColor = true
		log.Logger = log.Output(consoleWriter)
		setLogLevel()

		log.Info().Msg("Skystats: Running in daemon mode")
	}

	url := GetConnectionUrl()
	if p := poolMaxParam(); p != "" {
		url += "?" + p
	}

	// a receiver's SkyStats (Radar Nodes) makes its own database on first start
	ensureDatabase()

	log.Info().Msg("Connecting to Postgres database")

	pg, err := NewPG(context.Background(), url)
	if err != nil {
		log.Fatal().Err(err).Msg("Failed to connect to Postgres database")
		os.Exit(1)
	}

	// Setup db
	log.Info().Msg("Checking to see if any database initialisation / migrations are needed")
	if err := RunDatabaseMigrations(); err != nil {
		log.Error().Err(err).Msg("Error initialising or migrating the database")
		os.Exit(1)
	}

	log.Info().Msg("Checking performance indexes")
	ensureK4nzaIndexes(pg)
	ensureK4nzaSchema(pg)

	// A failed plane-alert-db download (no internet, GitHub rate limit) is
	// logged and retried by the daily refresh in the background loop.
	log.Info().Msg("Checking if interesting aircraft reference data needs updating from plane-alert-db")
	if err := UpsertPlaneAlertDb(pg); err != nil {
		log.Warn().Msgf("Could not update interesting aircraft data (will retry later): %v", err)
	}

	// Start API server in a separate goroutine
	log.Info().Msg("Starting API server")
	apiServer := NewAPIServer(pg)

	go func() {
		apiServer.Start()
	}()

	log.Info().Msg("Starting scheduled tasks")

	// Welcome to skystats
	if banner, err := os.ReadFile("../docs/logo/skystats_ascii.txt"); err == nil {
		log.Info().Msg("\n" + string(banner))
	}
	if version == "dev" {
		log.Info().Msgf("Welcome to Skystats! (build: %s • %s)", version, commit)
	} else {
		log.Info().Msgf("Welcome to Skystats %s!", version)
	}

	defer func() {
		log.Info().Msg("Closing database connection")
		pg.Close()
	}()

	// Recording has its own loop, so a slow call to adsbdb / GitHub or a slow
	// statistics query can't stop aircraft from being recorded. Everything
	// else runs in a background loop. Every task is protected against panics
	// so one bad record cannot take the whole program down.
	go backgroundLoop(pg)

	// summarise past days (the first time: the whole existing history) in
	// the background, so start-up isn't delayed on a large database
	go runSafely("summary rollup", func() { runRollup(pg) })
	go runSafely("feeder military backfill", func() { backfillFeederMilitary(pg) })

	// Aircraft stay in view for minutes, so polling every few seconds misses
	// nothing while keeping CPU use and database writes low.
	ingestTicker := time.NewTicker(ingestInterval)
	defer ingestTicker.Stop()
	for range ingestTicker.C {
		runSafely("update aircraft", func() { updateAircraftDatabase(pg) })
	}
}

func backgroundLoop(pg *postgres) {
	updateStatisticsTicker := time.NewTicker(120 * time.Second)
	updateRegistrationsTicker := time.NewTicker(30 * time.Second)
	updateRoutesTicker := time.NewTicker(300 * time.Second)
	updateInterestingSeenTicker := time.NewTicker(120 * time.Second)
	maintenanceTicker := time.NewTicker(1 * time.Hour)
	planeAlertTicker := time.NewTicker(24 * time.Hour)

	// run maintenance shortly after start-up, then hourly
	runSafely("maintenance", func() { runMaintenance(pg) })

	for {
		select {
		case <-updateStatisticsTicker.C:
			runSafely("update statistics", func() { updateMeasurementStatistics(pg) })
		case <-updateRegistrationsTicker.C:
			runSafely("update registrations", func() { updateRegistrations(pg) })
		case <-updateRoutesTicker.C:
			runSafely("update routes", func() { updateRoutes(pg) })
		case <-updateInterestingSeenTicker.C:
			runSafely("update interesting", func() { updateInterestingSeen(pg) })
		case <-maintenanceTicker.C:
			runSafely("maintenance", func() { runMaintenance(pg) })
		case <-planeAlertTicker.C:
			runSafely("plane-alert-db refresh", func() {
				if err := UpsertPlaneAlertDb(pg); err != nil {
					log.Warn().Msgf("plane-alert-db refresh failed (will retry tomorrow): %v", err)
				}
			})
		}
	}
}

// runSafely runs a task and recovers from panics, logging them instead of
// crashing the program.
func runSafely(name string, task func()) {
	defer func() {
		if r := recover(); r != nil {
			log.Error().Msgf("Recovered from panic in task %q: %v", name, r)
		}
	}()
	log.Debug().Msg("Running task: " + name)
	task()
}

func checkFlags() {
	flag.Parse()
	if showVersion {
		showVersionExit()
	}
}

func setLogLevel() {
	switch os.Getenv("LOG_LEVEL") {
	case "DEBUG":
		zerolog.SetGlobalLevel(zerolog.DebugLevel)
		log.Info().Msg("Log level set to DEBUG")
	case "INFO":
		zerolog.SetGlobalLevel(zerolog.InfoLevel)
		log.Info().Msg("Log level set to INFO")
	case "WARN":
		zerolog.SetGlobalLevel(zerolog.WarnLevel)
		log.Warn().Msg("Log level set to WARN")
	case "ERROR":
		zerolog.SetGlobalLevel(zerolog.ErrorLevel)
		log.Error().Msg("Log level set to ERROR")
	default:
		zerolog.SetGlobalLevel(zerolog.InfoLevel)
		log.Info().Msg("Log level not set or invalid, defaulting to INFO")
	}
}
