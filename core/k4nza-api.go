package main

// K4NZA additions to the API:
//
//   GET /api/search?q=...&days=0|7|30|365      aircraft matching a registration, hex code,
//                                             callsign, type or operator
//   GET /api/search?group=Mil|Gov|Pol|Civ&days every aircraft of one interesting group
//   GET /api/search/aircraft/:hex?limit&offset one aircraft: details and its visits
//   GET /api/search/aircraft/:hex/visits.csv   every visit of one aircraft as CSV
//
// plus the same-origin check for requests that change something, and a short
// cache for the all-time totals.

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5"
	"github.com/rs/zerolog/log"
	"github.com/tomcarman/skystats/data"
)

// sameOriginWrites refuses requests that would change something when the
// browser says they come from another site. Browsers send Sec-Fetch-Site on
// every request; Origin is checked for older ones. Requests without either
// header (curl, scripts on the feeder) are allowed, as before.
func sameOriginWrites(c *gin.Context) {
	switch c.Request.Method {
	case http.MethodGet, http.MethodHead:
		c.Next()
		return
	}
	if !isSameOriginRequest(c.Request) {
		c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"error": "changes are only accepted from the SkyStats page"})
		return
	}
	c.Next()
}

func isSameOriginRequest(r *http.Request) bool {
	switch r.Header.Get("Sec-Fetch-Site") {
	case "same-origin", "none":
		return true
	case "":
		// older browser or a script: fall back to Origin
	default: // cross-site, same-site
		return false
	}
	origin := r.Header.Get("Origin")
	if origin == "" {
		return true
	}
	u, err := url.Parse(origin)
	if err != nil {
		return false
	}
	host := r.Header.Get("X-Forwarded-Host")
	if host == "" {
		host = r.Host
	}
	return strings.EqualFold(u.Host, host)
}

// ---------------------------------------------------------------------------
// all-time totals, cached for a minute (several cards ask at once)

var (
	totalsMu      sync.Mutex
	totalsExpires time.Time
	totalsCache   [3]int64
)

func (s *APIServer) allTimeTotalsCached() (flights, aircraft, routed int64, err error) {
	totalsMu.Lock()
	defer totalsMu.Unlock()
	if time.Now().Before(totalsExpires) {
		return totalsCache[0], totalsCache[1], totalsCache[2], nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	flights, aircraft, routed, err = allTimeTotals(ctx, s.pg)
	if err != nil {
		log.Error().Err(err).Msg("all-time totals failed")
		return
	}
	totalsCache = [3]int64{flights, aircraft, routed}
	totalsExpires = time.Now().Add(time.Minute)
	return
}

// ---------------------------------------------------------------------------
// search

const searchMaxResults = 200

// a whole group can be large (thousands of civilian aircraft of interest)
const groupSearchMaxResults = 1000

// interesting groups that can be listed with /api/search?group=
var searchGroups = map[string]bool{"Mil": true, "Gov": true, "Pol": true, "Civ": true}

var (
	hexPattern      = regexp.MustCompile(`^~?[0-9a-f]{6}$`)
	searchSanitizer = regexp.MustCompile(`[^A-Z0-9~]`)
)

// searchTerms normalises what was typed: upper case, without spaces, dashes
// or wildcards, so "n812dn", "N-812DN" and "N812DN " all match N812DN.
type searchTerms struct {
	raw   string // trimmed, upper case, for operator names
	key   string // letters and digits only
	hex   string // lower-case hex code, if the input looks like one
	isHex bool
}

func parseSearch(q string) (searchTerms, bool) {
	raw := strings.ToUpper(strings.TrimSpace(q))
	key := searchSanitizer.ReplaceAllString(raw, "")
	if len(key) < 2 || len(raw) > 60 {
		return searchTerms{}, false
	}
	t := searchTerms{raw: raw, key: key}
	if h := strings.ToLower(key); hexPattern.MatchString(h) {
		t.hex = h
		t.isHex = true
	}
	return t, true
}

// escapeLike makes user text safe inside LIKE / ILIKE patterns.
func escapeLike(s string) string {
	return strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(s)
}

func searchDays(c *gin.Context) int {
	d, err := strconv.Atoi(c.DefaultQuery("days", "0"))
	if err != nil || d < 0 || d > 36500 {
		return 0
	}
	return d
}

// regKey is how registrations are compared: upper case, letters and digits
// only. The same expression is indexed (see k4nzaSchemaStatements).
const regKeyR = `regexp_replace(upper(r), '[^A-Z0-9]', '', 'g')`
const regKeyRegistration = `regexp_replace(upper(registration), '[^A-Z0-9]', '', 'g')`

// searchSQL finds the matching aircraft first (cheap, mostly by index) and
// only then adds up their visits. $1 watermark, $2 key (letters/digits),
// $3 hex (” = not a hex code), $4 operator pattern (” = don't search
// operators or models), $5 days (0 = all time), $6 max rows, $7 ICAO type
// codes (the key itself, codes it starts, and types whose name has it, so
// "KC-135R", "Stratotanker" and "K35" all find K35R).
const searchSQL = `
	WITH ` + searchLiveCTE + `,
	cand AS (
		SELECT hex, 'hex code' AS m FROM k4nza_aircraft_seen WHERE $3 <> '' AND hex = $3
		UNION SELECT hex, 'hex code' FROM live WHERE $3 <> '' AND hex = $3
		UNION SELECT hex, 'registration' FROM k4nza_aircraft_seen WHERE ` + regKeyR + ` LIKE $2 || '%'
		UNION SELECT hex, 'registration' FROM live WHERE ` + regKeyR + ` LIKE $2 || '%'
		UNION SELECT mode_s, 'registration' FROM registration_data WHERE ` + regKeyRegistration + ` LIKE $2 || '%'
		UNION SELECT hex, 'type' FROM k4nza_aircraft_seen WHERE t = ANY($7)
		UNION SELECT hex, 'type' FROM live WHERE t = ANY($7)
		UNION SELECT mode_s, 'type' FROM registration_data WHERE $4 <> '' AND (type ILIKE $4 OR icao_type = ANY($7))
		UNION SELECT lower(icao), 'type' FROM interesting_aircraft WHERE $4 <> '' AND type ILIKE $4
		UNION SELECT hex, 'callsign' FROM k4nza_callsign_seen WHERE flight LIKE $2 || '%'
		UNION SELECT hex, 'callsign' FROM live WHERE EXISTS (SELECT 1 FROM unnest(flights) f WHERE f LIKE $2 || '%')
		UNION SELECT mode_s, 'operator' FROM registration_data WHERE $4 <> '' AND registered_owner ILIKE $4
		UNION SELECT lower(icao), 'operator' FROM interesting_aircraft WHERE $4 <> '' AND operator ILIKE $4
		UNION SELECT c.hex, 'operator' FROM route_data rd
		      JOIN k4nza_callsign_seen c ON c.flight = rd.route_callsign
		      WHERE $4 <> '' AND rd.airline_name ILIKE $4
		UNION SELECT l.hex, 'operator' FROM route_data rd
		      JOIN live l ON rd.route_callsign = ANY(l.flights)
		      WHERE $4 <> '' AND rd.airline_name ILIKE $4
	),
` + searchResultSQL

// groupSearchSQL lists every aircraft of one interesting group (Mil, Gov,
// Pol, Civ) that was ever seen: by the plane-alert-db list, and by what was
// recorded when it was seen (the list can change). $1 watermark, $2 group,
// $3 days (0 = all time), $4 max rows.
var groupSearchSQL = `
	WITH ` + searchLiveCTE + `,
	cand AS (
		SELECT lower(icao) AS hex, 'group' AS m FROM interesting_aircraft WHERE "group" = $2
		UNION SELECT lower(icao), 'group' FROM interesting_aircraft_seen WHERE "group" = $2
	),
` + strings.NewReplacer("$5", "$3", "$6", "$4").Replace(searchResultSQL)

// searchLiveCTE: visits not yet rolled up into k4nza_aircraft_seen. $1 watermark.
const searchLiveCTE = `live AS (
		SELECT hex, MAX(NULLIF(t, '')) AS t, MAX(NULLIF(r, '')) AS r,
		       MIN(first_seen) AS first_seen, MAX(last_seen) AS last_seen, COUNT(*) AS visits,
		       MIN(LEAST(min_distance, last_seen_distance, NULLIF(r_dst, 0) * 1.852)) AS min_distance,
		       ARRAY_AGG(DISTINCT flight) FILTER (WHERE flight <> '') AS flights
		FROM aircraft_data
		WHERE first_seen >= $1 AND hex IS NOT NULL
		GROUP BY hex
	)`

// searchResultSQL adds up the visits of the candidate aircraft (cand: hex,
// how it matched). $5 days (0 = all time), $6 max rows.
const searchResultSQL = `
	matched AS (
		SELECT hex, string_agg(DISTINCT m, ', ') AS how FROM cand GROUP BY hex
	)
	SELECT m.hex,
	       COALESCE(l.r, s.r, rd.registration, ''), COALESCE(l.t, s.t, rd.icao_type, ''),
	       LEAST(s.first_seen, l.first_seen) AS first_seen,
	       GREATEST(s.last_seen, l.last_seen) AS last_seen,
	       COALESCE(s.visits, 0) + COALESCE(l.visits, 0),
	       LEAST(s.min_distance, l.min_distance)::float8,
	       COALESCE(NULLIF(ia.operator, ''), rd.registered_owner, ''),
	       COALESCE(rd.manufacturer, ''), COALESCE(rd.type, ''),
	       COALESCE(ia."group", ''), m.how
	FROM matched m
	LEFT JOIN k4nza_aircraft_seen s ON s.hex = m.hex
	LEFT JOIN live l ON l.hex = m.hex
	LEFT JOIN registration_data rd ON rd.mode_s = m.hex
	LEFT JOIN LATERAL (
		-- plane-alert-db first, else what was recorded when it was seen
		-- (aircraft the feeder's database flags as military)
		SELECT operator, "group" FROM (
			SELECT 1 AS o, operator, "group" FROM interesting_aircraft WHERE icao = upper(m.hex)
			UNION ALL
			SELECT 2, operator, "group" FROM interesting_aircraft_seen WHERE icao = upper(m.hex)
		) x ORDER BY o LIMIT 1
	) ia ON TRUE
	WHERE (s.hex IS NOT NULL OR l.hex IS NOT NULL)
	  AND ($5 = 0 OR GREATEST(s.last_seen, l.last_seen) >= NOW() - make_interval(days => $5))
	ORDER BY last_seen DESC
	LIMIT $6`

type searchResult struct {
	Hex          string    `json:"hex"`
	Registration string    `json:"registration"`
	Type         string    `json:"type"`
	FirstSeen    time.Time `json:"first_seen"`
	LastSeen     time.Time `json:"last_seen"`
	Visits       int64     `json:"visits"`
	ClosestKm    *float64  `json:"closest_km"`
	Operator     string    `json:"operator"`
	Manufacturer string    `json:"manufacturer"`
	Model        string    `json:"model"`
	Group        string    `json:"interesting_group"`
	MatchedOn    string    `json:"matched_on"`
}

func (s *APIServer) search(c *gin.Context) {
	group := c.Query("group")
	terms, ok := parseSearch(c.Query("q"))
	if group != "" {
		if !searchGroups[group] {
			c.JSON(http.StatusBadRequest, gin.H{"error": "unknown group"})
			return
		}
	} else if !ok {
		c.JSON(http.StatusBadRequest, gin.H{"error": "type at least 2 letters or digits"})
		return
	}
	days := searchDays(c)

	ctx, cancel := context.WithTimeout(c.Request.Context(), 30*time.Second)
	defer cancel()
	w, err := rollupWatermark(ctx, s.pg.db)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	maxResults := searchMaxResults
	var rows pgx.Rows
	if group != "" {
		maxResults = groupSearchMaxResults
		terms.raw = group
		rows, err = s.pg.db.Query(ctx, groupSearchSQL, w, group, days, maxResults+1)
	} else {
		operator := ""
		if len(terms.raw) >= 3 {
			operator = "%" + escapeLike(terms.raw) + "%"
		}
		types := data.MatchTypeCodes(terms.key, 60)
		rows, err = s.pg.db.Query(ctx, searchSQL, w, escapeLike(terms.key), terms.hex, operator, days, maxResults+1, types)
	}
	if err != nil {
		log.Error().Err(err).Msg("search failed")
		c.JSON(http.StatusInternalServerError, gin.H{"error": "search failed"})
		return
	}
	defer rows.Close()

	results := []searchResult{}
	for rows.Next() {
		var r searchResult
		if err := rows.Scan(&r.Hex, &r.Registration, &r.Type, &r.FirstSeen, &r.LastSeen, &r.Visits,
			&r.ClosestKm, &r.Operator, &r.Manufacturer, &r.Model, &r.Group, &r.MatchedOn); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		results = append(results, r)
	}
	if err := rows.Err(); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	more := len(results) > maxResults
	if more {
		results = results[:maxResults]
	}
	c.JSON(http.StatusOK, gin.H{"query": terms.raw, "group": group, "results": results, "more": more, "limit": maxResults})
}

func validHex(h string) (string, bool) {
	h = strings.ToLower(strings.TrimSpace(h))
	return h, hexPattern.MatchString(h)
}

// visitsSQL lists the visits of one aircraft ($1 hex), newest first. Closest
// distance falls back to the first/last position for visits recorded before
// closest distance was tracked.
const visitsSQL = `
	SELECT a.first_seen, a.last_seen, COALESCE(a.flight, ''),
	       LEAST(a.min_distance, a.last_seen_distance, NULLIF(a.r_dst, 0) * 1.852)::float8,
	       a.alt_baro, NULLIF(a.gs, 0)::float8, COALESCE(a.squawk, ''),
	       COALESCE(ro.origin_iata_code, ''), COALESCE(ro.origin_icao_code, ''), COALESCE(ro.origin_name, ''),
	       COALESCE(ro.destination_iata_code, ''), COALESCE(ro.destination_icao_code, ''), COALESCE(ro.destination_name, ''),
	       COALESCE(ro.airline_name, '')
	FROM aircraft_data a
	LEFT JOIN route_data ro ON ro.route_callsign = a.flight
	WHERE a.hex = $1
	ORDER BY a.first_seen DESC
	LIMIT $2 OFFSET $3`

type visit struct {
	FirstSeen   time.Time `json:"first_seen"`
	LastSeen    time.Time `json:"last_seen"`
	Callsign    string    `json:"callsign"`
	ClosestKm   *float64  `json:"closest_km"`
	MaxAltFt    *int64    `json:"max_altitude_ft"`
	MaxSpeedKt  *float64  `json:"max_ground_speed_kt"`
	Squawk      string    `json:"squawk"`
	Origin      string    `json:"origin"`
	OriginIcao  string    `json:"origin_icao"`
	OriginName  string    `json:"origin_name"`
	Dest        string    `json:"destination"`
	DestIcao    string    `json:"destination_icao"`
	DestName    string    `json:"destination_name"`
	AirlineName string    `json:"airline"`
}

func (s *APIServer) searchAircraft(c *gin.Context) {
	hex, ok := validHex(c.Param("hex"))
	if !ok {
		c.JSON(http.StatusBadRequest, gin.H{"error": "not an aircraft hex code"})
		return
	}
	limit, err := strconv.Atoi(c.DefaultQuery("limit", "25"))
	if err != nil || limit < 1 || limit > 500 {
		limit = 25
	}
	offset, err := strconv.Atoi(c.DefaultQuery("offset", "0"))
	if err != nil || offset < 0 {
		offset = 0
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), 30*time.Second)
	defer cancel()
	w, err := rollupWatermark(ctx, s.pg.db)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	// summary over the whole history (includes trimmed days)
	info := gin.H{"hex": hex}
	var r, t string
	var first, last *time.Time
	var visits, inHistory int64
	var closest *float64
	var maxAlt *int64
	err = s.pg.db.QueryRow(ctx, `
		WITH s AS (SELECT * FROM k4nza_aircraft_seen WHERE hex = $2),
		l AS (
			SELECT MAX(NULLIF(t, '')) AS t, MAX(NULLIF(r, '')) AS r, MIN(first_seen) AS first_seen,
			       MAX(last_seen) AS last_seen, COUNT(*) AS visits,
			       MIN(LEAST(min_distance, last_seen_distance, NULLIF(r_dst, 0) * 1.852)) AS min_distance
			FROM aircraft_data WHERE hex = $2 AND first_seen >= $1
		)
		SELECT COALESCE(l.r, s.r, ''), COALESCE(l.t, s.t, ''),
		       LEAST(s.first_seen, l.first_seen), GREATEST(s.last_seen, l.last_seen),
		       COALESCE(s.visits, 0) + l.visits,
		       LEAST(s.min_distance, l.min_distance)::float8,
		       (SELECT COUNT(*) FROM aircraft_data WHERE hex = $2),
		       (SELECT MAX(alt_baro)::bigint FROM aircraft_data WHERE hex = $2)
		FROM l LEFT JOIN s ON TRUE`, w, hex).Scan(&r, &t, &first, &last, &visits, &closest, &inHistory, &maxAlt)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if visits == 0 {
		c.JSON(http.StatusNotFound, gin.H{"error": "this aircraft hasn't been seen"})
		return
	}
	info["registration"], info["type"] = r, t
	info["first_seen"], info["last_seen"] = first, last
	info["visits"], info["visits_in_history"] = visits, inHistory
	info["closest_km"] = closest
	info["max_altitude_ft"] = maxAlt

	// registration details (from adsbdb) and plane-alert-db
	var manufacturer, model, owner, country, photo, thumb, icaoType, reg string
	if err := s.pg.db.QueryRow(ctx, `
		SELECT COALESCE(manufacturer, ''), COALESCE(type, ''), COALESCE(registered_owner, ''),
		       COALESCE(registered_owner_country_name, ''), COALESCE(url_photo, ''),
		       COALESCE(url_photo_thumbnail, ''), COALESCE(icao_type, ''), COALESCE(registration, '')
		FROM registration_data WHERE mode_s = $1 LIMIT 1`, hex).
		Scan(&manufacturer, &model, &owner, &country, &photo, &thumb, &icaoType, &reg); err == nil {
		info["manufacturer"], info["model"], info["owner"] = manufacturer, model, owner
		info["owner_country"], info["photo"], info["photo_thumbnail"] = country, photo, thumb
		if r == "" {
			info["registration"] = reg
		}
		if t == "" {
			info["type"] = icaoType
		}
	}
	// the type in words, from the ICAO type list (when the lookups have no model)
	if name, ok := data.LookupTypeName(fmt.Sprint(info["type"])); ok {
		info["type_name"] = name
	}
	var iaOperator, iaGroup, iaCategory, iaLink, iaType, tag1, tag2, tag3 string
	if err := s.pg.db.QueryRow(ctx, `
		SELECT COALESCE(operator, ''), COALESCE("group", ''), COALESCE(category, ''), COALESCE(link, ''),
		       COALESCE(type, ''), COALESCE(tag1, ''), COALESCE(tag2, ''), COALESCE(tag3, '')
		FROM (
			SELECT 1 AS o, operator, "group", category, link, type, tag1, tag2, tag3, NULL::timestamptz AS seen
			FROM interesting_aircraft WHERE icao = upper($1)
			UNION ALL
			SELECT 2, operator, "group", category, link, type, tag1, tag2, tag3, seen
			FROM interesting_aircraft_seen WHERE icao = upper($1)
		) x ORDER BY o, COALESCE(operator, '') = '', seen DESC NULLS LAST LIMIT 1`, hex).
		Scan(&iaOperator, &iaGroup, &iaCategory, &iaLink, &iaType, &tag1, &tag2, &tag3); err == nil {
		info["interesting"] = gin.H{
			"operator": iaOperator, "group": iaGroup, "category": iaCategory, "link": iaLink,
			"type": iaType, "tags": []string{tag1, tag2, tag3},
		}
	}

	rows, err := s.pg.db.Query(ctx, visitsSQL, hex, limit, offset)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	defer rows.Close()
	list := []visit{}
	for rows.Next() {
		var v visit
		if err := rows.Scan(&v.FirstSeen, &v.LastSeen, &v.Callsign, &v.ClosestKm, &v.MaxAltFt, &v.MaxSpeedKt,
			&v.Squawk, &v.Origin, &v.OriginIcao, &v.OriginName, &v.Dest, &v.DestIcao, &v.DestName, &v.AirlineName); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		v.Callsign = strings.TrimSpace(v.Callsign)
		list = append(list, v)
	}
	if err := rows.Err(); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	info["visits_list"] = list
	info["offset"], info["limit"] = offset, limit
	c.JSON(http.StatusOK, info)
}

func (s *APIServer) searchAircraftCSV(c *gin.Context) {
	hex, ok := validHex(c.Param("hex"))
	if !ok {
		c.JSON(http.StatusBadRequest, gin.H{"error": "not an aircraft hex code"})
		return
	}
	// hex is checked against hexPattern above, so it is safe in the SQL text
	// (COPY can't take parameters)
	ds := &exportDataset{
		Key: "visits",
		query: `SELECT a.first_seen, a.last_seen, a.hex AS icao_hex, a.flight AS callsign,
				COALESCE(NULLIF(a.r, ''), rd.registration) AS registration,
				COALESCE(NULLIF(a.t, ''), rd.icao_type) AS aircraft_type,
				ro.airline_name, ro.origin_iata_code AS origin, ro.origin_name,
				ro.destination_iata_code AS destination, ro.destination_name,
				a.alt_baro AS max_altitude_ft, NULLIF(a.gs, 0) AS max_ground_speed_kt,
				ROUND(LEAST(a.min_distance, a.last_seen_distance, NULLIF(a.r_dst, 0) * 1.852), 2) AS closest_km,
				ROUND(LEAST(a.min_distance, a.last_seen_distance, NULLIF(a.r_dst, 0) * 1.852) / 1.609344, 2) AS closest_mi,
				ROUND(LEAST(a.min_distance, a.last_seen_distance, NULLIF(a.r_dst, 0) * 1.852) / 1.852, 2) AS closest_nm,
				a.squawk
			FROM aircraft_data a
			LEFT JOIN registration_data rd ON rd.mode_s = a.hex
			LEFT JOIN route_data ro ON ro.route_callsign = a.flight
			WHERE a.hex = '` + hex + `' AND %s
			ORDER BY a.first_seen`,
	}
	c.Header("Content-Type", "text/csv; charset=utf-8")
	c.Header("Content-Disposition", fmt.Sprintf(`attachment; filename="skystats-visits-%s.csv"`, strings.TrimPrefix(hex, "~")))
	c.Header("Cache-Control", "no-store")
	c.Status(http.StatusOK)
	ctx, cancel := context.WithTimeout(c.Request.Context(), 10*time.Minute)
	defer cancel()
	if err := s.copyCSV(ctx, c.Writer, ds, exportRange{}); err != nil {
		log.Error().Err(err).Msg("visits export failed")
	}
}
