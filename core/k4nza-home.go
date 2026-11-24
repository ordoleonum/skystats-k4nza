package main

// The feeder's home page summary (Daylight dashboard): what happened today at
// this station, in one small request. All times are the viewer's local day.

import (
	"context"
	"math"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/tomcarman/skystats/data"
)

type homeAircraft struct {
	Hex      string   `json:"hex"`
	Callsign string   `json:"callsign"`
	Reg      string   `json:"registration"`
	Type     string   `json:"type"`
	Model    string   `json:"model"`
	Operator string   `json:"operator"`
	AltFt    *int64   `json:"altitude_ft,omitempty"`
	Nm       *float64 `json:"distance_nm,omitempty"`
	Dir      string   `json:"direction,omitempty"`
	When     *string  `json:"when,omitempty"`
	Count    *int64   `json:"times_ever,omitempty"`

	// the model is the registration lookup's (often a raw model string)
	modelFromLookup bool
}

// compass turns a bearing into one of 16 points ("WNW").
func compass(deg float64) string {
	points := []string{"N", "NNE", "NE", "ENE", "E", "ESE", "SE", "SSE", "S", "SSW", "SW", "WSW", "W", "WNW", "NW", "NNW"}
	d := math.Mod(math.Mod(deg, 360)+360, 360)
	return points[int(math.Floor(d/22.5+0.5))%16]
}

func (s *APIServer) k4nzaHome(c *gin.Context) {
	tz := s.getTimezone(c)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	out := gin.H{}
	loc, err := time.LoadLocation(tz)
	if err != nil {
		loc = time.UTC
	}
	day, _ := reportPeriodFor("today", time.Now(), loc)

	// the hour-by-hour card's Week / Month views: different aircraft per local
	// day for the last 7 or 30 days (today included), and nothing else
	if d := c.Query("days"); d == "7" || d == "30" {
		n := 7
		if d == "30" {
			n = 30
		}
		daily, err := s.homeDaily(ctx, tz, loc, n)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{"daily": daily})
		return
	}

	// aircraft per local hour today
	hours := make([]int64, 24)
	rows, err := s.pg.db.Query(ctx, `
		SELECT EXTRACT(HOUR FROM first_seen AT TIME ZONE $1)::int, COUNT(DISTINCT hex)
		FROM aircraft_data
		WHERE first_seen >= DATE_TRUNC('day', NOW(), $1)
		GROUP BY 1`, tz)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	for rows.Next() {
		var h int
		var n int64
		if rows.Scan(&h, &n) == nil && h >= 0 && h < 24 {
			hours[h] = n
		}
	}
	rows.Close()
	out["hours"] = hours

	// different aircraft today, and how many of them were never seen before today
	var today, fresh int64
	_ = s.pg.db.QueryRow(ctx, `
		WITH t AS (SELECT DISTINCT hex FROM aircraft_data WHERE first_seen >= DATE_TRUNC('day', NOW(), $1))
		SELECT (SELECT COUNT(*) FROM t),
		       (SELECT COUNT(*) FROM t WHERE NOT EXISTS (
		           SELECT 1 FROM aircraft_data o WHERE o.hex = t.hex AND o.first_seen < DATE_TRUNC('day', NOW(), $1)))`,
		tz).Scan(&today, &fresh)
	out["aircraft_today"] = today
	out["new_today"] = fresh

	// farthest today
	if far, ok := s.farthestSince(ctx, day.start, tz); ok {
		out["farthest"] = far
	}

	// highest today
	high := homeAircraft{}
	var alt int64
	if err := s.pg.db.QueryRow(ctx, `
		SELECT hex, COALESCE(TRIM(flight), ''), COALESCE(t, ''), alt_baro, TO_CHAR(first_seen AT TIME ZONE $1, 'YYYY-MM-DD"T"HH24:MI')
		FROM aircraft_data
		WHERE first_seen >= DATE_TRUNC('day', NOW(), $1) AND alt_baro > 0 AND alt_baro < 70000
		ORDER BY alt_baro DESC LIMIT 1`, tz).Scan(&high.Hex, &high.Callsign, &high.Type, &alt, &high.When); err == nil {
		high.AltFt = &alt
		s.homeDetails(ctx, &high)
		out["highest"] = high
	}

	// latest military aircraft today (plane-alert-db or the feeder's own military flag)
	mil := homeAircraft{}
	var milAlt *int64
	var milNm *float64
	if err := s.pg.db.QueryRow(ctx, `
		SELECT lower(s.icao), COALESCE(s.registration, ''), COALESCE(NULLIF(s.type, ''), s.icao_type, ''),
		       COALESCE(s.operator, ''), TO_CHAR(s.seen AT TIME ZONE $1, 'YYYY-MM-DD"T"HH24:MI'),
		       (SELECT MAX(a.alt_baro)::bigint FROM aircraft_data a WHERE a.hex = lower(s.icao) AND a.first_seen >= DATE_TRUNC('day', NOW(), $1)),
		       (SELECT MIN(a.min_distance)::float8 / $2 FROM aircraft_data a WHERE a.hex = lower(s.icao) AND a.first_seen >= DATE_TRUNC('day', NOW(), $1))
		FROM interesting_aircraft_seen s
		WHERE s."group" = 'Mil' AND s.seen >= DATE_TRUNC('day', NOW(), $1)
		ORDER BY s.seen DESC LIMIT 1`, tz, kmPerNm).Scan(&mil.Hex, &mil.Reg, &mil.Model, &mil.Operator, &mil.When, &milAlt, &milNm); err == nil {
		mil.AltFt, mil.Nm = milAlt, milNm
		s.homeDetails(ctx, &mil)
		out["military"] = mil
	}
	var milCount int64
	_ = s.pg.db.QueryRow(ctx, `SELECT COUNT(*) FROM (`+militaryHexSQL+`) m`, day.start).Scan(&milCount)
	out["military_today"] = milCount

	// rarest type today: of the types heard today, the one with the fewest
	// different aircraft ever. Counted from the summary table (indexed by type,
	// never trimmed) plus the flights not summarised yet: counting the whole
	// flight table once per type took about a second on a Pi after two weeks,
	// and grew with every day of history.
	rare := homeAircraft{}
	var ever int64
	w, werr := rollupWatermark(ctx, s.pg.db)
	if werr != nil {
		w = time.Unix(0, 0).UTC()
	}
	if err := s.pg.db.QueryRow(ctx, `
		WITH today AS (
			SELECT DISTINCT ON (t) t, hex, COALESCE(TRIM(flight), '') AS cs
			FROM aircraft_data
			WHERE first_seen >= DATE_TRUNC('day', NOW(), $1) AND t IS NOT NULL AND t <> ''
			ORDER BY t, first_seen DESC
		)
		SELECT today.t, today.hex, today.cs,
		       (SELECT COUNT(*) FROM k4nza_aircraft_seen s WHERE s.t = today.t)
		       + (SELECT COUNT(DISTINCT a.hex) FROM aircraft_data a
		          WHERE a.t = today.t AND a.first_seen >= $2
		            AND NOT EXISTS (SELECT 1 FROM k4nza_aircraft_seen s WHERE s.hex = a.hex)) AS n
		FROM today
		ORDER BY n ASC, today.t LIMIT 1`, tz, w).Scan(&rare.Type, &rare.Hex, &rare.Callsign, &ever); err == nil {
		rare.Count = &ever
		s.homeDetails(ctx, &rare)
		out["rarest"] = rare
	}

	// today's notable events, as logged by the once-a-minute job
	out["firsts"] = s.todayEvents(ctx, rare.Type)

	c.JSON(http.StatusOK, out)
}

type homeDay struct {
	Date     string `json:"date"` // YYYY-MM-DD, station time
	Aircraft int64  `json:"aircraft"`
}

// homeDaily: different aircraft heard on each of the last n local days,
// oldest first, today last (days without any are 0).
func (s *APIServer) homeDaily(ctx context.Context, tz string, loc *time.Location, n int) ([]homeDay, error) {
	rows, err := s.pg.db.Query(ctx, `
		SELECT TO_CHAR(first_seen AT TIME ZONE $1, 'YYYY-MM-DD'), COUNT(DISTINCT hex)
		FROM aircraft_data
		WHERE first_seen >= DATE_TRUNC('day', NOW(), $1) - make_interval(days => $2)
		GROUP BY 1`, tz, n-1)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	counts := map[string]int64{}
	for rows.Next() {
		var d string
		var c int64
		if err := rows.Scan(&d, &c); err != nil {
			return nil, err
		}
		counts[d] = c
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	now := time.Now().In(loc)
	today := time.Date(now.Year(), now.Month(), now.Day(), 12, 0, 0, 0, loc)
	out := make([]homeDay, 0, n)
	for i := n - 1; i >= 0; i-- {
		d := today.AddDate(0, 0, -i).Format("2006-01-02")
		out = append(out, homeDay{Date: d, Aircraft: counts[d]})
	}
	return out, nil
}

// homeDetails fills registration, model and operator from the registration
// lookup and plane-alert-db, where known. The model in words comes from
// plane-alert-db ("Boeing E-6B Mercury"), else the ICAO type list ("Boeing
// 737-800"), else the registration lookup (often a raw "737NG 823/W").
func (s *APIServer) homeDetails(ctx context.Context, a *homeAircraft) {
	defer func() {
		// a "model" that is only a type code ("E6", from plane-alert-db or the
		// feeder's database) becomes the type: "Boeing E-6 Mercury"
		if name, ok := data.LookupTypeName(a.Model); ok && !strings.Contains(a.Model, " ") {
			if a.Type == "" {
				a.Type = strings.ToUpper(strings.TrimSpace(a.Model))
			}
			a.Model = name
			return
		}
		if a.Model == "" || a.modelFromLookup {
			if name, ok := data.LookupTypeName(a.Type); ok {
				a.Model = name
			}
		}
	}()
	var reg, maker, model, owner, icaoType string
	if err := s.pg.db.QueryRow(ctx, `
		SELECT COALESCE(registration, ''), COALESCE(manufacturer, ''), COALESCE(type, ''), COALESCE(registered_owner, ''), COALESCE(icao_type, '')
		FROM registration_data WHERE mode_s = lower($1) LIMIT 1`, a.Hex).Scan(&reg, &maker, &model, &owner, &icaoType); err == nil {
		if a.Reg == "" {
			a.Reg = reg
		}
		if a.Model == "" && model != "" {
			a.Model = model
			if maker != "" {
				a.Model = maker + " " + model
			}
			a.modelFromLookup = true
		}
		if a.Operator == "" {
			a.Operator = owner
		}
		if a.Type == "" {
			a.Type = icaoType
		}
	}
	var op, paType string
	if err := s.pg.db.QueryRow(ctx, `
		SELECT COALESCE(operator, ''), COALESCE(type, '') FROM interesting_aircraft WHERE icao = upper($1) LIMIT 1`, a.Hex).Scan(&op, &paType); err == nil {
		if op != "" {
			a.Operator = op
		}
		if paType != "" && (a.Model == "" || a.modelFromLookup) {
			a.Model = paType
			a.modelFromLookup = false
		}
	}
}
