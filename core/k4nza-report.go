package main

// The Activity tab's report (Daylight design): one request for the whole page.
//
//   GET /api/k4nza/report?period=today|week|month|all&tz=Area/City
//
// "week" is the last 7 local days including today, "month" the last 30. The
// previous period, for the "more than last week" sentence, covers the same
// stretch of time one period earlier (so a Tuesday afternoon is compared with
// the Tuesday afternoon before, not with a whole day).

import (
	"context"
	"math"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/tomcarman/skystats/data"
)

type reportPeriod struct {
	key   string
	start time.Time // first moment shown
	days  int       // length, for the previous period (0 = all time)
}

func reportPeriodFor(key string, now time.Time, loc *time.Location) (reportPeriod, bool) {
	local := now.In(loc)
	midnight := time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, loc)
	switch key {
	case "today":
		return reportPeriod{key, midnight, 1}, true
	case "week":
		return reportPeriod{key, midnight.AddDate(0, 0, -6), 7}, true
	case "month":
		return reportPeriod{key, midnight.AddDate(0, 0, -29), 30}, true
	case "all":
		return reportPeriod{key, time.Unix(0, 0).UTC(), 0}, true
	}
	return reportPeriod{}, false
}

// militaryHexSQL: aircraft marked military by plane-alert-db or by the
// feeder's own database (db_flags bit 1) that started a visit in [$1, now).
const militaryHexSQL = `
	SELECT lower(icao) AS hex FROM interesting_aircraft_seen WHERE "group" = 'Mil' AND seen >= $1
	UNION
	SELECT hex FROM aircraft_data WHERE first_seen >= $1 AND db_flags IS NOT NULL AND (db_flags & 1) = 1`

// farthestSince: the farthest aircraft heard since from, using the first-heard
// distance or the last position, whichever is farther.
func (s *APIServer) farthestSince(ctx context.Context, from time.Time, tz string) (homeAircraft, bool) {
	return s.farthestInSector(ctx, from, tz, -1)
}

// farthestInSector: the farthest aircraft since from in one 10-degree range
// sector (as on the range chart), or in any direction when sector < 0.
func (s *APIServer) farthestInSector(ctx context.Context, from time.Time, tz string, sector int) (homeAircraft, bool) {
	far := homeAircraft{}
	var nm, dir float64
	var when string
	err := s.pg.db.QueryRow(ctx, `
		WITH a AS (
			SELECT hex, COALESCE(TRIM(flight), '') AS cs, COALESCE(t, '') AS t, first_seen,
			       r_dst::float8 AS nm1, r_dir::float8 AS dir1,
			       last_seen_distance::float8 / $4 AS nm2,
			       CASE WHEN last_seen_lat IS NOT NULL AND last_seen_lon IS NOT NULL AND NOT (last_seen_lat = 0 AND last_seen_lon = 0)
			            THEN atan2d(sind(last_seen_lon::float8 - $3) * cosd(last_seen_lat::float8),
			                        cosd($2) * sind(last_seen_lat::float8) - sind($2) * cosd(last_seen_lat::float8) * cosd(last_seen_lon::float8 - $3))
			       END AS dir2
			FROM aircraft_data
			WHERE first_seen >= $1
		), p AS (
			SELECT hex, cs, t, first_seen, nm1 AS nm, dir1 AS dir FROM a WHERE nm1 > 0 AND dir1 IS NOT NULL
			UNION ALL
			SELECT hex, cs, t, first_seen, nm2, dir2 FROM a WHERE nm2 > 0 AND dir2 IS NOT NULL
		)
		SELECT hex, cs, t, nm, dir, TO_CHAR(first_seen AT TIME ZONE $6, 'YYYY-MM-DD"T"HH24:MI')
		FROM p WHERE nm < $5
		  AND ($7::int < 0 OR floor(mod((dir + 360)::numeric, 360) / 10)::int = $7::int)
		ORDER BY nm DESC LIMIT 1`,
		from, getLat(), getLon(), kmPerNm, maxPlausibleNm(), tz, sector).Scan(&far.Hex, &far.Callsign, &far.Type, &nm, &dir, &when)
	if err != nil {
		return far, false
	}
	nm = math.Round(nm*10) / 10
	far.Nm = &nm
	far.Dir = compass(dir)
	far.When = &when
	s.homeDetails(ctx, &far)
	return far, true
}

func (s *APIServer) k4nzaReport(c *gin.Context) {
	tz := s.getTimezone(c)
	loc, err := time.LoadLocation(tz)
	if err != nil {
		loc = time.UTC
	}
	now := time.Now()
	p, ok := reportPeriodFor(c.DefaultQuery("period", "week"), now, loc)
	if !ok {
		c.JSON(http.StatusBadRequest, gin.H{"error": "period must be today, week, month or all"})
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	from := p.start
	fail := func(err error) {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
	}
	out := gin.H{"period": p.key, "start": from.In(loc).Format("2006-01-02"), "today": now.In(loc).Format("2006-01-02")}

	// the oldest flight still kept (the history limit trims older ones)
	var oldest *time.Time
	_ = s.pg.db.QueryRow(ctx, `SELECT MIN(first_seen) FROM aircraft_data`).Scan(&oldest)

	// flights and different aircraft
	var flights, aircraft, fresh, types int64
	if err := s.pg.db.QueryRow(ctx, `
		WITH v AS (SELECT hex, t FROM aircraft_data WHERE first_seen >= $1 AND hex IS NOT NULL),
		     h AS (SELECT DISTINCT hex FROM v)
		SELECT (SELECT COUNT(*) FROM v),
		       (SELECT COUNT(*) FROM h),
		       (SELECT COUNT(*) FROM h WHERE
		            NOT EXISTS (SELECT 1 FROM aircraft_data o WHERE o.hex = h.hex AND o.first_seen < $1)
		        AND NOT EXISTS (SELECT 1 FROM k4nza_aircraft_seen s WHERE s.hex = h.hex AND s.first_seen < $1)),
		       (SELECT COUNT(DISTINCT t) FROM v WHERE t IS NOT NULL AND t <> '')`,
		from).Scan(&flights, &aircraft, &fresh, &types); err != nil {
		fail(err)
		return
	}
	if p.days == 0 {
		// all time includes the days the history limit has removed
		if f, a, _, err := s.allTimeTotalsCached(); err == nil {
			flights, aircraft, fresh = f, a, a
		}
		var first *time.Time
		_ = s.pg.db.QueryRow(ctx, `SELECT LEAST((SELECT MIN(first_seen) FROM k4nza_aircraft_seen), (SELECT MIN(first_seen) FROM aircraft_data))`).Scan(&first)
		if first != nil {
			out["start"] = first.In(loc).Format("2006-01-02")
		}
	}
	out["flights"], out["aircraft"], out["new_aircraft"], out["types"] = flights, aircraft, fresh, types

	// the same stretch one period earlier, when it is still in the history
	if p.days > 0 {
		prevFrom := from.AddDate(0, 0, -p.days)
		if oldest != nil && !oldest.After(prevFrom.Add(time.Hour)) {
			var prev int64
			if err := s.pg.db.QueryRow(ctx, `SELECT COUNT(*) FROM aircraft_data WHERE first_seen >= $1 AND first_seen < $2`,
				prevFrom, now.AddDate(0, 0, -p.days)).Scan(&prev); err == nil {
				out["flights_before"] = prev
			}
		}
	}

	// most common type (different aircraft)
	var topType string
	if s.pg.db.QueryRow(ctx, `SELECT t FROM aircraft_data WHERE first_seen >= $1 AND t IS NOT NULL AND t <> ''
		GROUP BY t ORDER BY COUNT(DISTINCT hex) DESC, t LIMIT 1`, from).Scan(&topType) == nil {
		out["top_type"] = gin.H{"code": topType, "name": s.typeNames(ctx, []string{topType})[topType]}
	}

	if far, ok := s.farthestSince(ctx, from, tz); ok {
		out["farthest"] = far
	}

	// military: how many, and the commonest kinds
	var mil int64
	_ = s.pg.db.QueryRow(ctx, `SELECT COUNT(*) FROM (`+militaryHexSQL+`) m`, from).Scan(&mil)
	out["military"] = mil
	milKinds := []string{}
	if rows, err := s.pg.db.Query(ctx, `
		SELECT k, COUNT(DISTINCT icao) FROM (
			SELECT COALESCE(NULLIF(icao_type, ''), NULLIF(TRIM(t), ''), '') AS k, icao
			FROM interesting_aircraft_seen WHERE "group" = 'Mil' AND seen >= $1
		) m WHERE k <> ''
		GROUP BY k ORDER BY 2 DESC, k LIMIT 3`, from); err == nil {
		for rows.Next() {
			var k string
			var n int64
			if rows.Scan(&k, &n) == nil {
				milKinds = append(milKinds, k)
			}
		}
		rows.Close()
	}
	out["military_types"] = milKinds

	// when the sky is busiest: flights per weekday (Mon = 0) and local hour,
	// as a daily average when the period spans several weeks
	heat := make([][]float64, 7)
	for i := range heat {
		heat[i] = make([]float64, 24)
	}
	if rows, err := s.pg.db.Query(ctx, `
		SELECT EXTRACT(ISODOW FROM first_seen AT TIME ZONE $2)::int - 1,
		       EXTRACT(HOUR FROM first_seen AT TIME ZONE $2)::int,
		       COUNT(*)::float8 / COUNT(DISTINCT (first_seen AT TIME ZONE $2)::date)
		FROM aircraft_data WHERE first_seen >= $1
		GROUP BY 1, 2`, from, tz); err == nil {
		for rows.Next() {
			var d, h int
			var n float64
			if rows.Scan(&d, &h, &n) == nil && d >= 0 && d < 7 && h >= 0 && h < 24 {
				heat[d][h] = math.Round(n*10) / 10
			}
		}
		rows.Close()
	} else {
		fail(err)
		return
	}
	out["heat"] = heat
	out["today_weekday"] = (int(now.In(loc).Weekday()) + 6) % 7

	// who flew over most (flights with a known route, by airline)
	airlines := []gin.H{}
	if rows, err := s.pg.db.Query(ctx, `
		SELECT rd.airline_name, COALESCE(rd.airline_icao, ''), COUNT(*)
		FROM aircraft_data ad JOIN route_data rd ON ad.flight = rd.route_callsign
		WHERE ad.first_seen >= $1 AND rd.airline_name IS NOT NULL AND rd.airline_name <> ''
		GROUP BY 1, 2 ORDER BY 3 DESC, 1 LIMIT 8`, from); err == nil {
		for rows.Next() {
			var name, icao string
			var n int64
			if rows.Scan(&name, &icao, &n) == nil {
				airlines = append(airlines, gin.H{"name": name, "icao": icao, "flights": n})
			}
		}
		rows.Close()
	}
	out["airlines"] = airlines

	// aircraft types (different aircraft)
	typeRows := []gin.H{}
	codes := []string{}
	if rows, err := s.pg.db.Query(ctx, `
		SELECT t, COUNT(DISTINCT hex) FROM aircraft_data
		WHERE first_seen >= $1 AND t IS NOT NULL AND t <> ''
		GROUP BY t ORDER BY 2 DESC, t LIMIT 8`, from); err == nil {
		for rows.Next() {
			var t string
			var n int64
			if rows.Scan(&t, &n) == nil {
				typeRows = append(typeRows, gin.H{"code": t, "aircraft": n})
				codes = append(codes, t)
			}
		}
		rows.Close()
	}
	names := s.typeNames(ctx, codes)
	for _, r := range typeRows {
		r["name"] = names[r["code"].(string)]
	}
	out["aircraft_types"] = typeRows

	// where they were going
	routes := []gin.H{}
	if rows, err := s.pg.db.Query(ctx, `
		SELECT rd.origin_iata_code, COALESCE(rd.origin_name, ''), rd.destination_iata_code, COALESCE(rd.destination_name, ''), COUNT(*)
		FROM aircraft_data ad JOIN route_data rd ON ad.flight = rd.route_callsign
		WHERE ad.first_seen >= $1
		  AND rd.origin_iata_code IS NOT NULL AND rd.origin_iata_code <> ''
		  AND rd.destination_iata_code IS NOT NULL AND rd.destination_iata_code <> ''
		  AND rd.origin_iata_code <> rd.destination_iata_code
		GROUP BY 1, 2, 3, 4 ORDER BY 5 DESC, 1, 3 LIMIT 8`, from); err == nil {
		for rows.Next() {
			var a, an, b, bn string
			var n int64
			if rows.Scan(&a, &an, &b, &bn, &n) == nil {
				routes = append(routes, gin.H{"from": a, "from_name": an, "to": b, "to_name": bn, "flights": n})
			}
		}
		rows.Close()
	}
	out["routes"] = routes

	// notable visitors: the latest interesting aircraft, one card each
	visitors := []gin.H{}
	if rows, err := s.pg.db.Query(ctx, `
		WITH last AS (
			SELECT DISTINCT ON (s.icao) lower(s.icao) AS hex, COALESCE(s."group", '') AS grp,
			       COALESCE(NULLIF(s.type, ''), rd.type, '') AS model,
			       COALESCE(NULLIF(s.operator, ''), rd.registered_owner, '') AS operator,
			       COALESCE(NULLIF(s.registration, ''), NULLIF(TRIM(s.r), ''), rd.registration, '') AS reg,
			       COALESCE(NULLIF(s.icao_type, ''), NULLIF(TRIM(s.t), ''), '') AS t,
			       COALESCE(TRIM(s.flight), '') AS cs, s.seen
			FROM interesting_aircraft_seen s
			LEFT JOIN registration_data rd ON rd.mode_s = lower(s.icao)
			WHERE s.seen >= $1 AND s."group" IN ('Mil', 'Gov', 'Pol', 'Civ')
			ORDER BY s.icao, s.seen DESC
		)
		SELECT hex, grp, model, operator, reg, t, cs, seen,
		       (SELECT MIN(a.min_distance)::float8 / $2 FROM aircraft_data a WHERE a.hex = last.hex AND a.first_seen >= $1),
		       (SELECT MIN(a.first_seen) FROM aircraft_data a WHERE a.hex = last.hex AND a.first_seen <= last.seen AND a.last_seen >= last.seen - INTERVAL '1 minute')
		FROM last ORDER BY seen DESC LIMIT 4`, from, kmPerNm); err == nil {
		for rows.Next() {
			var hex, grp, model, operator, reg, t, cs string
			var seen time.Time
			var nm *float64
			var visitStart *time.Time
			if rows.Scan(&hex, &grp, &model, &operator, &reg, &t, &cs, &seen, &nm, &visitStart) != nil {
				continue
			}
			if nm != nil {
				v := math.Round(*nm*10) / 10
				nm = &v
			}
			// the type in words: a "model" that is only a code ("E6") or none at all
			if name, ok := data.LookupTypeName(model); ok && !strings.Contains(model, " ") {
				if t == "" {
					t = strings.ToUpper(strings.TrimSpace(model))
				}
				model = name
			} else if model == "" {
				if name, ok := data.LookupTypeName(t); ok {
					model = name
				}
			}
			// the replay starts where the visit started
			start := seen
			if visitStart != nil {
				start = *visitStart
			}
			visitors = append(visitors, gin.H{
				"hex": hex, "group": grp, "model": model, "operator": operator, "registration": reg,
				"type": t, "callsign": cs, "distance_nm": nm,
				"seen":       seen.In(loc).Format("2006-01-02T15:04"),
				"replay_utc": start.UTC().Format(time.RFC3339),
			})
		}
		rows.Close()
	}
	out["visitors"] = visitors

	c.JSON(http.StatusOK, out)
}

// typeNames: a readable name for ICAO type codes ("B738" -> "Boeing 737-800"),
// from the ICAO type list (data/icao_types.csv); codes not on it use the
// registration lookups SkyStats has stored (the commonest name per code),
// which are often raw model strings such as "737NG 823/W".
func (s *APIServer) typeNames(ctx context.Context, codes []string) map[string]string {
	names := map[string]string{}
	missing := []string{}
	for _, code := range codes {
		if name, ok := data.LookupTypeName(code); ok {
			names[code] = name
		} else {
			missing = append(missing, code)
		}
	}
	if len(missing) == 0 {
		return names
	}
	codes = missing
	rows, err := s.pg.db.Query(ctx, `
		SELECT icao_type, mode() WITHIN GROUP (ORDER BY TRIM(COALESCE(manufacturer, '') || ' ' || COALESCE(type, '')))
		FROM registration_data
		WHERE icao_type = ANY($1) AND COALESCE(type, '') <> ''
		GROUP BY icao_type`, codes)
	if err != nil {
		return names
	}
	defer rows.Close()
	for rows.Next() {
		var code, name string
		if rows.Scan(&code, &name) == nil {
			names[code] = strings.TrimSpace(name)
		}
	}
	return names
}
