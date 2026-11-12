package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/rs/zerolog/log"

	cheapruler "github.com/JamesLMilner/cheap-ruler-go"
	"github.com/jackc/pgx/v5"
)

func updateAircraftDatabase(pg *postgres) {

	responseData, err := Fetch()

	if err != nil {
		log.Error().Err(err).Msg("Unable to fetch data from feeder aircraft.json")
		return
	}

	response, err := parseAircraftJSON(responseData)
	if err != nil {
		log.Error().Err(err).Msg("Unable to parse feeder aircraft.json")
		return
	}
	if response.Now == 0 {
		// partial or empty file (e.g. readsb restarting) - nothing to record
		return
	}

	response.TrimFlightStrings()

	loc := []float64{getLon(), getLat()}
	ruler := getRuler()
	radius := getRadius()

	var aircraftsInRange []Aircraft

	for _, aircraft := range response.Aircraft {

		// Filter out non-aircraft
		if isNonAircraft(aircraft) {
			continue
		}

		// Filter out aircraft not within radius
		planeLoc := []float64{aircraft.Lon, aircraft.Lat}
		distance := ruler.Distance(loc, planeLoc)

		if distance < radius {
			aircraftsInRange = append(aircraftsInRange, aircraft)
		}

	}
	pg.updateDatabase(response.Now, aircraftsInRange)
}

func isNonAircraft(aircraft Aircraft) bool {
	return aircraft.T == "TWR" ||
		aircraft.R == "TWR" ||
		strings.HasPrefix(aircraft.Category, "C") ||
		aircraft.Squawk == "7777"
}

// The station position, radius and distance calculator never change while
// running, so they are worked out once.
var (
	stationOnce   sync.Once
	stationLat    float64
	stationLon    float64
	stationRadius float64
	stationRuler  *cheapruler.CheapRuler
)

func loadStation() {
	stationOnce.Do(func() {
		stationLat, _ = strconv.ParseFloat(os.Getenv("LAT"), 64)
		stationLon, _ = strconv.ParseFloat(os.Getenv("LON"), 64)
		stationRadius, _ = strconv.ParseFloat(os.Getenv("RADIUS"), 64)
		ruler, err := cheapruler.NewCheapruler(stationLat, "kilometers")
		if err != nil {
			fmt.Println("Error creating ruler: ", err)
			return
		}
		stationRuler = &ruler
	})
}

func getRuler() *cheapruler.CheapRuler {
	loadStation()
	return stationRuler
}

func getDistance(aircraft []float64) *float64 {
	loc := []float64{getLon(), getLat()}
	distance := getRuler().Distance(loc, aircraft)
	return &distance
}

// Database calls on the ingest path get a deadline, so a stuck query can
// never block recording.
const ingestQueryTimeout = 30 * time.Second

// how often readsb's aircraft.json is read and recorded
const ingestInterval = 5 * time.Second

// An aircraft that is still in view is written to the database at most once
// per this interval, unless something recorded changed (new maximum
// altitude/speed, callsign appeared). Every UPDATE creates a new row version
// plus index entries, so this keeps SD card writes low.
const aircraftWriteInterval = 30 * time.Second

var lastCacheSweep time.Time

// sweepAircraftCache removes expired entries so the cache doesn't grow with
// every aircraft ever seen.
func (pg *postgres) sweepAircraftCache(now time.Time) {
	if now.Sub(lastCacheSweep) < time.Minute {
		return
	}
	lastCacheSweep = now
	for hex, entry := range pg.recentAircraftCache {
		if now.After(entry.expiresAt) {
			delete(pg.recentAircraftCache, hex)
		}
	}
}

func (pg *postgres) updateDatabase(nowEpoch float64, aircrafts []Aircraft) {

	pg.sweepAircraftCache(time.Now())

	if len(aircrafts) == 0 {
		// nothing in range: no database work needed this pass
		return
	}

	existingAircrafts := getAircraftsRecentlySeen(pg, nowEpoch, aircrafts)
	if existingAircrafts == nil {
		// lookup failed - skip this cycle rather than inserting duplicates
		return
	}

	if len(existingAircrafts) > 0 {
		updateExistingAircrafts(pg, nowEpoch, aircrafts, existingAircrafts)
	}

	insertNewAircrafts(pg, nowEpoch, existingAircrafts, aircrafts)

}

func getAircraftsRecentlySeen(pg *postgres, nowEpoch float64, aircrafts []Aircraft) map[string]*Aircraft {

	existingAircrafts := make(map[string]*Aircraft)
	now := time.Now()
	newExpiry := now.Add(10 * time.Minute)

	var uncachedHexes []string

	for _, a := range aircrafts {
		entry, ok := pg.recentAircraftCache[a.Hex]
		if ok && now.Before(entry.expiresAt) {
			entry.expiresAt = newExpiry
			existingAircrafts[a.Hex] = &entry.aircraft
		} else {
			uncachedHexes = append(uncachedHexes, a.Hex)
		}
	}

	if len(uncachedHexes) == 0 {
		return existingAircrafts
	}

	recentThreshold := int(nowEpoch) - 600

	query := `
		SELECT DISTINCT ON (hex)
			id,
			hex,
			last_seen_epoch,
			last_seen_lat,
			last_seen_lon,
			last_seen_distance,
			alt_baro,
			alt_geom,
			gs,
			ias,
			tas
		FROM aircraft_data
		WHERE hex = ANY($1::text[])
			AND last_seen_epoch > $2
		ORDER BY hex, last_seen DESC;
	`

	ctx, cancel := context.WithTimeout(context.Background(), ingestQueryTimeout)
	defer cancel()

	rows, err := pg.db.Query(ctx, query, uncachedHexes, recentThreshold)
	if err != nil {
		log.Error().Err(err).Msg("getAircraftsRecentlySeen() - error querying db, skipping this cycle")
		return nil
	}
	defer rows.Close()

	for rows.Next() {
		var a Aircraft
		err := rows.Scan(
			&a.Id,
			&a.Hex,
			&a.LastSeenEpoch,
			&a.LastSeenLat,
			&a.LastSeenLon,
			&a.LastSeenDistance,
			&a.AltBaro,
			&a.AltGeom,
			&a.Gs,
			&a.Ias,
			&a.Tas)

		if err != nil {
			log.Error().Err(err).Msg("getAircraftsRecentlySeen() - error scanning rows")
			continue
		}

		pg.recentAircraftCache[a.Hex] = &aircraftCacheEntry{
			aircraft:    a,
			expiresAt:   newExpiry,
			lastWritten: time.Unix(int64(a.LastSeenEpoch), 0),
		}
		existingAircrafts[a.Hex] = &pg.recentAircraftCache[a.Hex].aircraft
	}

	return existingAircrafts

}

func insertNewAircrafts(pg *postgres, nowEpoch float64, existingAircrafts map[string]*Aircraft, aircrafts []Aircraft) {

	batch := &pgx.Batch{}

	nowAsTime := time.Unix(int64(nowEpoch), 0)
	nowAsEpoch := int64(nowEpoch)

	var aircraftsToInsert []Aircraft

	for _, aircraft := range aircrafts {
		_, exists := existingAircrafts[aircraft.Hex]
		if !exists {
			lastSeenDistance := getDistance([]float64{aircraft.Lon, aircraft.Lat})
			aircraftsToInsert = append(aircraftsToInsert, aircraft)
			insertStatement := `
				INSERT INTO aircraft_data (
					hex,
					flight,
					first_seen,
					first_seen_epoch,
					last_seen,
					last_seen_epoch,
					last_seen_lat,
					last_seen_lon,
					last_seen_distance,
					type,
					r,
					t,
					alt_baro,
					alt_geom,
					gs,
					ias,
					tas,
					track,
					baro_rate,
					nav_qnh,
					nav_altitude_mcp,
					nav_heading,
					lat,
					lon,
					nic,
					rc,
					seen_pos,
					r_dst,
					r_dir,
					version,
					nic_baro,
					nac_p,
					nac_v,
					sil,
					sil_type,
					alert,
					spi,
					mlat,
					tisb,
					messages,
					seen,
					rssi,
					db_flags
				) VALUES (
					$1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15,
					$16, $17, $18, $19, $20, $21, $22, $23, $24, $25, $26, $27, $28,
					$29, $30, $31, $32, $33, $34, $35, $36, $37, $38, $39, $40, $41, $42, $43
				)`

			batch.Queue(insertStatement,
				aircraft.Hex,
				aircraft.Flight,
				nowAsTime,
				nowAsEpoch,
				nowAsTime,
				nowAsEpoch,
				aircraft.Lat,
				aircraft.Lon,
				lastSeenDistance,
				aircraft.Type,
				aircraft.R,
				aircraft.T,
				aircraft.AltBaro,
				aircraft.AltGeom,
				aircraft.Gs,
				aircraft.Ias,
				aircraft.Tas,
				aircraft.Track,
				aircraft.BaroRate,
				aircraft.NavQnh,
				aircraft.NavAltitudeMcp,
				aircraft.NavHeading,
				aircraft.Lat,
				aircraft.Lon,
				aircraft.Nic,
				aircraft.Rc,
				aircraft.SeenPos,
				aircraft.RDst,
				aircraft.RDir,
				aircraft.Version,
				aircraft.NicBaro,
				aircraft.NacP,
				aircraft.NacV,
				aircraft.Sil,
				aircraft.SilType,
				aircraft.Alert,
				aircraft.Spi,
				aircraft.Mlat,
				aircraft.Tisb,
				aircraft.Messages,
				aircraft.Seen,
				aircraft.Rssi,
				aircraft.DbFlags)
		}
	}

	if batch.Len() == 0 {
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), ingestQueryTimeout)
	defer cancel()

	br := pg.db.SendBatch(ctx, batch)
	defer br.Close()

	for i := 0; i < batch.Len(); i++ {
		_, err := br.Exec()
		if err != nil {
			log.Error().Err(err).Msg("insertNewAircrafts() - unable to insert data")
		}
	}
}

func updateExistingAircrafts(pg *postgres, nowEpoch float64, aircrafts []Aircraft, existingAircrafts map[string]*Aircraft) {

	batch := &pgx.Batch{}

	for _, aircraft := range aircrafts {
		existingAircraft, exists := existingAircrafts[aircraft.Hex]
		if !exists {
			continue
		}

		// Track whether anything kept as a record changed; if not, only write
		// once per aircraftWriteInterval.
		significantChange := false

		// Update flight if null and found in subsequent messages
		if existingAircraft.Flight == "" {
			if aircraft.Flight != "" {
				existingAircraft.Flight = aircraft.Flight
				significantChange = true
			}
		}

		// Update last seen time
		existingAircraft.LastSeenEpoch = nowEpoch
		existingAircraft.LastSeen = time.Unix(int64(nowEpoch), 0)

		// Update last_seen_lat, last_seen_lon, last_seen_distance with the latest lat/lon
		existingAircraft.LastSeenLat = sql.NullFloat64{Float64: aircraft.Lat, Valid: true}
		existingAircraft.LastSeenLon = sql.NullFloat64{Float64: aircraft.Lon, Valid: true}
		lastSeenDistance := getDistance([]float64{aircraft.Lon, aircraft.Lat})
		existingAircraft.LastSeenDistance = sql.NullFloat64{Float64: *lastSeenDistance, Valid: true}

		// Update barometric altitude & geometric altitudes if higher than already stored
		if existingAircraft.AltBaro < aircraft.AltBaro {
			existingAircraft.AltBaro = aircraft.AltBaro
			significantChange = true
		}
		if existingAircraft.AltGeom < aircraft.AltGeom {
			existingAircraft.AltGeom = aircraft.AltGeom
			significantChange = true
		}

		// Update ground speed, indicated air speed, and true air speed if higher than already stored
		if existingAircraft.Gs < aircraft.Gs {
			existingAircraft.Gs = aircraft.Gs
			significantChange = true
		}
		if existingAircraft.Ias < aircraft.Ias {
			existingAircraft.Ias = aircraft.Ias
			significantChange = true
		}
		if existingAircraft.Tas < aircraft.Tas {
			existingAircraft.Tas = aircraft.Tas
			significantChange = true
		}

		// Update track (kept in memory, written with the next flush)
		existingAircraft.Track = aircraft.Track

		cacheEntry := pg.recentAircraftCache[aircraft.Hex]
		if cacheEntry != nil && !significantChange &&
			time.Since(cacheEntry.lastWritten) < aircraftWriteInterval {
			continue
		}
		if cacheEntry != nil {
			cacheEntry.lastWritten = time.Now()
		}

		// Update destination distance
		if aircraft.Flight != "" {
			routeData, err := getRouteDataCached(pg, aircraft.Flight)
			if err != nil {
			} else if routeData != nil && routeData.DestinationLatitude.Valid && routeData.DestinationLongitude.Valid {
				destinationDistance := getDestinationDistance(
					aircraft.Lat,
					aircraft.Lon,
					routeData.DestinationLatitude.Float64,
					routeData.DestinationLongitude.Float64)
				existingAircraft.DestinationDistance = sql.NullFloat64{Float64: destinationDistance, Valid: true}
			}
		}


		updateStatement := `UPDATE aircraft_data
							SET last_seen = $1,
								last_seen_epoch = $2,
								last_seen_lat = $3,
								last_seen_lon = $4,
								last_seen_distance = $5,
								destination_distance = $6,
								track = $7,
								alt_baro = $8,
								alt_geom = $9,
								gs = $10,
								ias = $11,
								tas = $12,
								flight = $13
							WHERE id = $14`

		batch.Queue(
			updateStatement,
			existingAircraft.LastSeen,
			existingAircraft.LastSeenEpoch,
			existingAircraft.LastSeenLat,
			existingAircraft.LastSeenLon,
			existingAircraft.LastSeenDistance,
			existingAircraft.DestinationDistance,
			existingAircraft.Track,
			existingAircraft.AltBaro,
			existingAircraft.AltGeom,
			existingAircraft.Gs,
			existingAircraft.Ias,
			existingAircraft.Tas,
			existingAircraft.Flight,
			existingAircraft.Id,
		)
	}

	if batch.Len() == 0 {
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), ingestQueryTimeout)
	defer cancel()

	br := pg.db.SendBatch(ctx, batch)
	defer br.Close()

	// unchanged aircraft are skipped, so read exactly as many results as were queued
	for i := 0; i < batch.Len(); i++ {
		_, err := br.Exec()
		if err != nil {
			log.Error().Err(err).Msg("updateExistingAircrafts() - unable to update data")
		}
	}
}

func getLat() float64 {
	loadStation()
	return stationLat
}

func getLon() float64 {
	loadStation()
	return stationLon
}

func getRadius() float64 {
	loadStation()
	return stationRadius
}

type RouteData struct {
	DestinationLatitude  sql.NullFloat64
	DestinationLongitude sql.NullFloat64
}

// Destination lookups are cached in memory for 10 minutes instead of running
// one query per visible aircraft on every pass.
type routeCacheEntry struct {
	route     *RouteData
	expiresAt time.Time
}

var routeCache = map[string]routeCacheEntry{}
var lastRouteCacheSweep time.Time

func getRouteDataCached(pg *postgres, flight string) (*RouteData, error) {
	now := time.Now()
	if now.Sub(lastRouteCacheSweep) > 5*time.Minute {
		lastRouteCacheSweep = now
		for k, v := range routeCache {
			if now.After(v.expiresAt) {
				delete(routeCache, k)
			}
		}
	}
	if entry, ok := routeCache[flight]; ok && now.Before(entry.expiresAt) {
		return entry.route, nil
	}
	route, err := getRouteData(pg, flight)
	if err != nil {
		return nil, err
	}
	routeCache[flight] = routeCacheEntry{route: route, expiresAt: now.Add(10 * time.Minute)}
	return route, nil
}

func getRouteData(pg *postgres, flight string) (*RouteData, error) {
	if flight == "" {
		return nil, nil
	}

	var route RouteData
	query := `
		SELECT destination_latitude, destination_longitude
		FROM route_data
		WHERE route_callsign = $1
		AND destination_latitude IS NOT NULL
		AND destination_longitude IS NOT NULL
		LIMIT 1
	`

	ctx, cancel := context.WithTimeout(context.Background(), ingestQueryTimeout)
	defer cancel()

	err := pg.db.QueryRow(ctx, query, flight).Scan(
		&route.DestinationLatitude,
		&route.DestinationLongitude,
	)

	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, nil // No route data found
		}
		return nil, err
	}

	return &route, nil
}

func getDestinationDistance(currentLat, currentLon, destLat, destLon float64) float64 {
	ruler, err := cheapruler.NewCheapruler(currentLat, "kilometers")
	if err != nil {
		log.Error().Err(err).Msg("Error creating ruler for destination distance")
		return 0
	}

	currentLoc := []float64{currentLon, currentLat}
	destLoc := []float64{destLon, destLat}

	distance := ruler.Distance(currentLoc, destLoc)
	return math.Round(distance*100) / 100
}

// logTypeMismatch reports an aircraft.json field whose type differs from the
// model at most once per hour, so the log isn't flooded on every pass.
var lastTypeMismatchLog time.Time

func logTypeMismatch(err *json.UnmarshalTypeError) {
	if time.Since(lastTypeMismatchLog) < time.Hour {
		return
	}
	lastTypeMismatchLog = time.Now()
	log.Debug().Str("field", err.Field).Str("value", err.Value).Msg("aircraft.json field type differs from model; field skipped")
}

// parseAircraftJSON decodes readsb's aircraft.json. A field with an unexpected
// type (e.g. alt_baro "ground") only skips that one field - encoding/json still
// fills in everything else - so that is not an error. Only invalid JSON is.
func parseAircraftJSON(data []byte) (Response, error) {
	var response Response
	if err := json.Unmarshal(data, &response); err != nil {
		var typeErr *json.UnmarshalTypeError
		if !errors.As(err, &typeErr) {
			return response, err
		}
		logTypeMismatch(typeErr)
	}
	return response, nil
}
