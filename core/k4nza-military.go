package main

// K4NZA: aircraft that the feeder's own aircraft database flags as military
// (readsb "dbFlags" bit 1) count as Military even when plane-alert-db doesn't
// list them. Many military aircraft use hex codes that plane-alert-db doesn't
// know (an E-6B as AEEFBB, for example); they were recorded but never shown
// as Military. plane-alert-db stays first: its entry wins when it has one.

import (
	"context"
	"database/sql"
	"strings"
	"time"

	"github.com/rs/zerolog/log"
)

const (
	dbFlagMilitary       = 1
	feederMilitaryGroup  = "Mil"
	feederMilitaryOrigin = "Military (feeder database)"
)

func nullText(s string) sql.NullString {
	s = strings.TrimSpace(s)
	return sql.NullString{String: s, Valid: s != ""}
}

// niceCase turns the feeder database's ALL CAPS text ("UNITED STATES AIR
// FORCE", "GULFSTREAM 5") into "United States Air Force", "Gulfstream 5".
// Text with lower case letters is kept; so are words of one or two letters
// (US, NA), known abbreviations (LLC, USAF) and words with digits (C-37A).
var keepCaps = map[string]bool{
	"LLC": true, "USA": true, "USAF": true, "USN": true, "USMC": true, "USCG": true, "NASA": true,
	"FAA": true, "UPS": true, "DHL": true, "PHI": true, "BAE": true, "ATR": true, "CRJ": true,
	"ERJ": true, "MAX": true, "II": true, "III": true, "IV": true,
}

func niceCase(s string) string {
	s = strings.Join(strings.Fields(s), " ")
	if s == "" || strings.ToUpper(s) != s {
		return s
	}
	words := strings.Split(s, " ")
	for i, w := range words {
		if len(w) <= 2 || keepCaps[w] || strings.ContainsAny(w, "0123456789") {
			continue
		}
		words[i] = w[:1] + strings.ToLower(w[1:])
	}
	return strings.Join(words, " ")
}

// copyLiveFields fills in what was seen live (position, callsign, time).
func copyLiveFields(ia *InterestingAircraft, aircraft Aircraft) {
	ia.Hex = aircraft.Hex
	ia.Flight = aircraft.Flight
	ia.R = aircraft.R
	ia.T = aircraft.T
	ia.AltBaro = aircraft.AltBaro
	ia.AltGeom = aircraft.AltGeom
	ia.Gs = aircraft.Gs
	ia.Ias = aircraft.Ias
	ia.Tas = aircraft.Tas
	ia.Track = aircraft.Track
	ia.BaroRate = aircraft.BaroRate
	ia.Lat = aircraft.Lat
	ia.Lon = aircraft.Lon
	ia.Alert = aircraft.Alert
	ia.DbFlags = aircraft.DbFlags
	ia.Seen = aircraft.FirstSeen
	ia.SeenEpoch = aircraft.FirstSeenEpoch
}

// feederMilitary returns the interesting-aircraft record for an aircraft the
// feeder flags as military, or false when it isn't flagged.
func feederMilitary(aircraft Aircraft) (InterestingAircraft, bool) {
	if aircraft.DbFlags&dbFlagMilitary == 0 || strings.TrimSpace(aircraft.Hex) == "" {
		return InterestingAircraft{}, false
	}
	ia := InterestingAircraft{
		Icao:         strings.ToUpper(strings.TrimSpace(aircraft.Hex)),
		Registration: nullText(aircraft.R),
		IcaoType:     nullText(aircraft.T),
		Group:        sql.NullString{String: feederMilitaryGroup, Valid: true},
		Category:     sql.NullString{String: feederMilitaryOrigin, Valid: true},
		// what the feeder's own database knows: owner / operator and the
		// type in words (plane-alert-db's operator and type columns)
		Operator: nullText(niceCase(aircraft.OwnOp)),
		Type:     nullText(niceCase(aircraft.Desc)),
	}
	copyLiveFields(&ia, aircraft)
	return ia, true
}

// backfillFeederMilitary records, once, the visits of feeder-flagged military
// aircraft that were processed before this existed. One row per visit, as the
// live job writes them.
func backfillFeederMilitary(pg *postgres) {
	const name = "feeder_military_backfill"
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()
	var done bool
	if err := pg.db.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM k4nza_once WHERE name = $1)`, name).Scan(&done); err != nil {
		log.Warn().Err(err).Msg("feeder military backfill: could not check whether it already ran")
		return
	}
	if done {
		return
	}
	// plane-alert-db must be loaded first (its entries win), else wait for the next start
	var listLoaded bool
	if err := pg.db.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM interesting_aircraft)`).Scan(&listLoaded); err != nil || !listLoaded {
		log.Info().Msg("feeder military backfill: plane-alert-db not loaded yet, will run at the next start")
		return
	}
	tag, err := pg.db.Exec(ctx, `
		INSERT INTO interesting_aircraft_seen (
			icao, registration, icao_type, "group", category,
			hex, flight, r, t, alt_baro, alt_geom, gs, ias, tas, track, baro_rate,
			lat, lon, alert, db_flags, seen, seen_epoch)
		SELECT upper(ad.hex), NULLIF(trim(ad.r), ''), NULLIF(trim(ad.t), ''), $1, $2,
		       ad.hex, ad.flight, ad.r, ad.t, ad.alt_baro, ad.alt_geom, ad.gs, ad.ias, ad.tas, ad.track, ad.baro_rate,
		       ad.lat, ad.lon, ad.alert, ad.db_flags, ad.first_seen, ad.first_seen_epoch
		FROM aircraft_data ad
		WHERE ad.interesting_processed = true
		  AND ad.hex <> ''
		  AND (ad.db_flags & 1) = 1
		  AND NOT EXISTS (SELECT 1 FROM interesting_aircraft ia WHERE ia.icao = upper(ad.hex))
		  AND NOT EXISTS (SELECT 1 FROM interesting_aircraft_seen s WHERE s.icao = upper(ad.hex))
		ORDER BY ad.first_seen`, feederMilitaryGroup, feederMilitaryOrigin)
	if err != nil {
		log.Warn().Err(err).Msg("feeder military backfill failed (will try again at the next start)")
		return
	}
	if _, err := pg.db.Exec(ctx, `INSERT INTO k4nza_once (name) VALUES ($1) ON CONFLICT DO NOTHING`, name); err != nil {
		log.Warn().Err(err).Msg("feeder military backfill: could not record that it ran")
	}
	log.Info().Msgf("feeder military backfill: %d earlier visit(s) of feeder-flagged military aircraft marked Military", tag.RowsAffected())
}
