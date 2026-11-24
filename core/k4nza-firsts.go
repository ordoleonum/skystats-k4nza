package main

// Notable events: things that happened at this station for the first time
// ever - a new range record in some direction, a type never seen before, an
// aircraft back after a long time, a round number of different aircraft.
//
// A background job works them out once a minute and logs them in
// k4nza_notable_events (one row per event per station day, kept forever).
// The feeder's home page shows today's rows ("Today's notable events") and the
// Records tab shows the whole log as a feed. Because events are logged as they
// happen, one stays for the rest of the day even if the flights behind it are
// later summarised or trimmed.
//
// Everything is judged against the summary tables as well as the flight
// table, so it stays right when old flights are trimmed (RETENTION_DAYS).

import (
	"context"
	"encoding/json"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/rs/zerolog/log"
)

type homeFirst struct {
	// range | type | back | milestone | soon
	Kind     string        `json:"kind"`
	When     *string       `json:"when,omitempty"` // station time, YYYY-MM-DDTHH:MM
	Aircraft *homeAircraft `json:"aircraft,omitempty"`
	Nm       float64       `json:"distance_nm,omitempty"` // range: today's farthest in that direction
	OldNm    float64       `json:"old_nm,omitempty"`      // range: the best before today
	Dir      string        `json:"direction,omitempty"`   // range: 16-point compass
	Sector   int           `json:"sector,omitempty"`      // range: 10-degree sector (0-35)
	Days     int           `json:"days,omitempty"`        // back: days since the last visit
	Number   int64         `json:"number,omitempty"`      // milestone / soon: the round number
	Total    int64         `json:"total,omitempty"`       // soon: different aircraft so far
}

// Records, new types and returns need some history first: on a new station
// nearly everything is a "first".
const firstsMinHistory = 7 * 24 * time.Hour

// an aircraft counts as "back" after this long away
const firstsBackAfter = 7 * 24 * time.Hour

// the home page shows at most this many of each kind (the first, the rest folded)
const firstsPerKind = 5

// the log keeps more of each kind per minute's check
const eventsLogPerKind = 36

// a range record has to beat the old best by at least this much, so a few
// tenths of a mile of noise isn't news
const (
	recordMinGainNm   = 1.0
	recordMinGainFrac = 0.01
)

// how often the background job looks for new events
const eventsInterval = time.Minute

var kindOrder = map[string]int{"range": 0, "type": 1, "back": 2, "milestone": 3, "soon": 4}

// sortEvents: records (farthest first), new types (newest first), returns
// (longest away first), round numbers.
func sortEvents(list []homeFirst) {
	when := func(f homeFirst) string {
		if f.When != nil {
			return *f.When
		}
		return ""
	}
	sort.SliceStable(list, func(i, j int) bool {
		a, b := list[i], list[j]
		if kindOrder[a.Kind] != kindOrder[b.Kind] {
			return kindOrder[a.Kind] < kindOrder[b.Kind]
		}
		switch a.Kind {
		case "range":
			return a.Nm > b.Nm
		case "back":
			return a.Days > b.Days
		}
		return when(a) > when(b)
	})
}

// eventsZone: the station's time zone (the feeder always sets STATION_TZ);
// UTC on a SkyStats without one.
func eventsZone() (string, *time.Location) {
	tz := stationTimezone()
	if tz == "" {
		tz = "UTC"
	}
	loc, err := time.LoadLocation(tz)
	if err != nil {
		return "UTC", time.UTC
	}
	return tz, loc
}

// computeEvents works out today's events from the flights; "soon" (how close
// the next round number is) is included but isn't an event to log.
func (s *APIServer) computeEvents(ctx context.Context, start time.Time, tz string, perKind int) []homeFirst {
	out := []homeFirst{}
	w, err := rollupWatermark(ctx, s.pg.db)
	if err != nil {
		return out
	}
	// before today = rolled-up days that ended by the station's midnight, plus
	// the flight table from there up to midnight
	cut := utcDay(start)
	liveFrom := w
	if cut.Before(liveFrom) {
		liveFrom = cut
	}

	var oldest *time.Time
	_ = s.pg.db.QueryRow(ctx, `SELECT LEAST((SELECT MIN(first_seen) FROM k4nza_aircraft_seen),
		(SELECT MIN(first_seen) FROM aircraft_data))`).Scan(&oldest)
	if oldest != nil && start.Sub(*oldest) >= firstsMinHistory {
		out = append(out, s.firstRanges(ctx, start, cut, liveFrom, tz, perKind)...)
		out = append(out, s.firstTypes(ctx, start, liveFrom, tz, perKind)...)
		out = append(out, s.firstBacks(ctx, start, tz, perKind)...)
	}
	if f, ok := s.firstMilestone(ctx, start, liveFrom, tz); ok {
		out = append(out, f)
	}
	sortEvents(out)
	return out
}

// the latest "soon" line, kept in memory for the home page
var (
	soonMu  sync.Mutex
	soonDay string
	soonNow *homeFirst
)

// recordNotableEvents logs today's new events (run once a minute).
func recordNotableEvents(pg *postgres) {
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Second)
	defer cancel()
	tz, loc := eventsZone()
	p, _ := reportPeriodFor("today", time.Now(), loc)
	day := p.start.In(loc).Format("2006-01-02")
	s := &APIServer{pg: pg}
	events := s.computeEvents(ctx, p.start, tz, eventsLogPerKind)

	soonMu.Lock()
	soonDay, soonNow = day, nil
	soonMu.Unlock()
	for _, f := range events {
		if f.Kind == "soon" {
			f := f
			soonMu.Lock()
			soonNow = &f
			soonMu.Unlock()
			continue
		}
		if err := logEvent(ctx, pg, day, loc, f); err != nil {
			log.Warn().Err(err).Str("kind", f.Kind).Msg("notable events: could not log")
		}
	}
}

// eventKey: what makes an event unique within a day.
func eventKey(f homeFirst) string {
	switch f.Kind {
	case "range":
		return strconv.Itoa(f.Sector)
	case "type":
		if f.Aircraft != nil {
			return f.Aircraft.Type
		}
	case "back":
		if f.Aircraft != nil {
			return f.Aircraft.Hex
		}
	case "milestone":
		return strconv.FormatInt(f.Number, 10)
	}
	return ""
}

func logEvent(ctx context.Context, pg *postgres, day string, loc *time.Location, f homeFirst) error {
	key := eventKey(f)
	if key == "" {
		return nil
	}
	data, err := json.Marshal(f)
	if err != nil {
		return err
	}
	at := time.Now()
	if f.When != nil {
		if t, err := time.ParseInLocation("2006-01-02T15:04", *f.When, loc); err == nil {
			at = t
		}
	}
	// a record can be beaten again later the same day: keep the farthest;
	// anything else is logged once, as first found
	_, err = pg.db.Exec(ctx, `
		INSERT INTO k4nza_notable_events (day, kind, key, at, data)
		VALUES ($1::date, $2, $3, $4, $5)
		ON CONFLICT (day, kind, key) DO UPDATE SET at = EXCLUDED.at, data = EXCLUDED.data
		WHERE k4nza_notable_events.kind = 'range'
		  AND (EXCLUDED.data->>'distance_nm')::float8 > (k4nza_notable_events.data->>'distance_nm')::float8`,
		day, f.Kind, key, at, data)
	return err
}

// readEvents: the logged events of the given station days, newest day first.
func readEvents(ctx context.Context, pg *postgres, days []string) (map[string][]homeFirst, error) {
	out := map[string][]homeFirst{}
	if len(days) == 0 {
		return out, nil
	}
	rows, err := pg.db.Query(ctx, `
		SELECT day::text, data FROM k4nza_notable_events WHERE day::text = ANY($1)`, days)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var day string
		var data []byte
		if err := rows.Scan(&day, &data); err != nil {
			return nil, err
		}
		var f homeFirst
		if json.Unmarshal(data, &f) == nil {
			out[day] = append(out[day], f)
		}
	}
	for _, list := range out {
		sortEvents(list)
	}
	return out, rows.Err()
}

// todayEvents: today's logged events for the home page, at most firstsPerKind
// of each kind, without the type already shown as the rarest-type highlight,
// then how close the next round number is.
func (s *APIServer) todayEvents(ctx context.Context, skipType string) []homeFirst {
	out := []homeFirst{}
	_, loc := eventsZone()
	p, _ := reportPeriodFor("today", time.Now(), loc)
	day := p.start.In(loc).Format("2006-01-02")
	logged, err := readEvents(ctx, s.pg, []string{day})
	if err != nil {
		return out
	}
	count := map[string]int{}
	for _, f := range logged[day] {
		if f.Kind == "type" && f.Aircraft != nil && f.Aircraft.Type == skipType {
			continue
		}
		if count[f.Kind] >= firstsPerKind {
			continue
		}
		count[f.Kind]++
		out = append(out, f)
	}
	if count["milestone"] == 0 {
		soonMu.Lock()
		if soonNow != nil && soonDay == day {
			out = append(out, *soonNow)
		}
		soonMu.Unlock()
	}
	return out
}

// GET /api/k4nza/events?days=7&before=YYYY-MM-DD: the log, by station day,
// newest first - the days that have events, up to `days` of them, before
// `before` (default: tomorrow, so today is included).
func (s *APIServer) k4nzaEvents(c *gin.Context) {
	n, err := strconv.Atoi(c.DefaultQuery("days", "7"))
	if err != nil || n < 1 || n > 60 {
		n = 7
	}
	before := c.Query("before")
	if _, err := time.Parse("2006-01-02", before); err != nil {
		_, loc := eventsZone()
		before = time.Now().In(loc).AddDate(0, 0, 1).Format("2006-01-02")
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 20*time.Second)
	defer cancel()
	rows, err := s.pg.db.Query(ctx, `
		SELECT DISTINCT day::text FROM k4nza_notable_events WHERE day < $1::date ORDER BY 1 DESC LIMIT $2`, before, n+1)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	days := []string{}
	for rows.Next() {
		var d string
		if rows.Scan(&d) == nil {
			days = append(days, d)
		}
	}
	rows.Close()
	more := len(days) > n
	if more {
		days = days[:n]
	}
	logged, err := readEvents(ctx, s.pg, days)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	type dayEvents struct {
		Date   string      `json:"date"`
		Events []homeFirst `json:"events"`
	}
	list := []dayEvents{}
	for _, d := range days {
		list = append(list, dayEvents{Date: d, Events: logged[d]})
	}
	_, loc := eventsZone()
	c.JSON(http.StatusOK, gin.H{"days": list, "more": more, "today": time.Now().In(loc).Format("2006-01-02")})
}

// firstRanges: today's new range records, one per 10-degree direction,
// farthest first.
func (s *APIServer) firstRanges(ctx context.Context, start, cut, liveFrom time.Time, tz string, perKind int) []homeFirst {
	out := []homeFirst{}
	today, err := queryRangeSectorsBetween(ctx, s.pg, start, time.Now().Add(24*time.Hour))
	if err != nil {
		return out
	}
	before, err := queryRangeSectorsBetween(ctx, s.pg, liveFrom, start)
	if err != nil {
		return out
	}
	rows, err := s.pg.db.Query(ctx, `SELECT sector, MAX(max_nm)::float8 FROM k4nza_range_daily WHERE day < $1::date GROUP BY sector`, cut.Format("2006-01-02"))
	if err != nil {
		return out
	}
	for rows.Next() {
		var sector int
		var nm float64
		if rows.Scan(&sector, &nm) == nil && sector >= 0 && sector < rangeSectors && nm > before[sector] {
			before[sector] = roundTenth(nm)
		}
	}
	rows.Close()

	records := []int{}
	for i := range today {
		// a direction never heard before isn't a record, just a first; and the
		// gain has to be real (at least 1 nm and 1%), not a few tenths of noise
		if before[i] > 0 && today[i] >= before[i]+max(recordMinGainNm, before[i]*recordMinGainFrac) {
			records = append(records, i)
		}
	}
	sort.SliceStable(records, func(a, b int) bool { return today[records[a]] > today[records[b]] })
	for _, i := range records {
		if len(out) == perKind {
			break
		}
		far, ok := s.farthestInSector(ctx, start, tz, i)
		if !ok {
			continue
		}
		out = append(out, homeFirst{Kind: "range", When: far.When, Nm: today[i], OldNm: before[i],
			Dir: compass(float64(i*10) + 5), Sector: i, Aircraft: &far})
	}
	return out
}

// firstTypes: aircraft types heard today for the first time ever, newest first.
func (s *APIServer) firstTypes(ctx context.Context, start, liveFrom time.Time, tz string, perKind int) []homeFirst {
	out := []homeFirst{}
	rows, err := s.pg.db.Query(ctx, `
		WITH today AS (
			SELECT DISTINCT ON (t) t, hex, COALESCE(TRIM(flight), '') AS cs, first_seen
			FROM aircraft_data
			WHERE first_seen >= $1 AND t IS NOT NULL AND t <> '' AND hex NOT LIKE '~%'
			ORDER BY t, first_seen
		), earlier AS (
			-- types in the flights not summarised yet, before today
			SELECT DISTINCT t FROM aircraft_data WHERE first_seen >= $2 AND first_seen < $1
		)
		SELECT today.t, today.hex, today.cs, TO_CHAR(today.first_seen AT TIME ZONE $3, 'YYYY-MM-DD"T"HH24:MI')
		FROM today
		WHERE NOT EXISTS (SELECT 1 FROM k4nza_aircraft_seen s WHERE s.t = today.t AND s.first_seen < $1)
		  AND today.t NOT IN (SELECT t FROM earlier WHERE t IS NOT NULL)
		ORDER BY today.first_seen DESC LIMIT $4`, start, liveFrom, tz, perKind)
	if err != nil {
		return out
	}
	for rows.Next() {
		a := &homeAircraft{}
		var when string
		if rows.Scan(&a.Type, &a.Hex, &a.Callsign, &when) != nil {
			continue
		}
		a.When = &when
		out = append(out, homeFirst{Kind: "type", When: &when, Aircraft: a})
	}
	rows.Close()
	for _, f := range out {
		s.homeDetails(ctx, f.Aircraft)
	}
	return out
}

// firstBacks: today's aircraft that had been away at least a week, longest
// away first.
func (s *APIServer) firstBacks(ctx context.Context, start time.Time, tz string, perKind int) []homeFirst {
	out := []homeFirst{}
	rows, err := s.pg.db.Query(ctx, `
		WITH today AS (
			SELECT hex, MIN(first_seen) AS fs
			FROM aircraft_data WHERE first_seen >= $1 AND hex NOT LIKE '~%'
			GROUP BY hex
		), prev AS (
			-- the summary row only counts when it ended before today: a rolled-up
			-- day can already hold part of today
			SELECT t.hex, t.fs, GREATEST(
				(SELECT MAX(o.last_seen) FROM aircraft_data o WHERE o.hex = t.hex AND o.first_seen < $1),
				(SELECT s.last_seen FROM k4nza_aircraft_seen s WHERE s.hex = t.hex AND s.last_seen < $1)) AS before
			FROM today t
		)
		SELECT p.hex, COALESCE((SELECT TRIM(a.flight) FROM aircraft_data a WHERE a.hex = p.hex AND a.first_seen = p.fs LIMIT 1), ''),
		       COALESCE((SELECT a.t FROM aircraft_data a WHERE a.hex = p.hex AND a.first_seen = p.fs LIMIT 1), ''),
		       TO_CHAR(p.fs AT TIME ZONE $2, 'YYYY-MM-DD"T"HH24:MI'),
		       FLOOR(EXTRACT(EPOCH FROM (p.fs - p.before)) / 86400)::int
		FROM prev p
		WHERE p.before IS NOT NULL AND EXTRACT(EPOCH FROM (p.fs - p.before)) >= $3::float8
		ORDER BY p.fs - p.before DESC, p.hex LIMIT $4`,
		start, tz, firstsBackAfter.Seconds(), perKind)
	if err != nil {
		return out
	}
	for rows.Next() {
		a := &homeAircraft{}
		var when string
		var days int
		if rows.Scan(&a.Hex, &a.Callsign, &a.Type, &when, &days) != nil {
			continue
		}
		a.When = &when
		out = append(out, homeFirst{Kind: "back", When: &when, Aircraft: a, Days: days})
	}
	rows.Close()
	for _, f := range out {
		s.homeDetails(ctx, f.Aircraft)
	}
	return out
}

// milestones: 100, 250, 500, every 1,000 to 10,000, every 5,000 to 50,000,
// then every 10,000
func nextMilestone(n int64) int64 {
	switch {
	case n < 100:
		return 100
	case n < 250:
		return 250
	case n < 500:
		return 500
	case n < 10000:
		return (n/1000 + 1) * 1000
	case n < 50000:
		return (n/5000 + 1) * 5000
	}
	return (n/10000 + 1) * 10000
}

// firstMilestone: the round-numbered different aircraft heard today, or, when
// none, how close the next round number is (within 2%).
func (s *APIServer) firstMilestone(ctx context.Context, start, liveFrom time.Time, tz string) (homeFirst, bool) {
	_, total, _, err := s.allTimeTotalsCached()
	if err != nil || total <= 0 {
		return homeFirst{}, false
	}
	// aircraft heard for the first time ever today, in order
	const newToday = `
		SELECT a.hex, MIN(a.first_seen) AS fs
		FROM aircraft_data a
		WHERE a.first_seen >= $1
		  AND NOT EXISTS (SELECT 1 FROM k4nza_aircraft_seen s WHERE s.hex = a.hex AND s.first_seen < $1)
		  AND NOT EXISTS (SELECT 1 FROM aircraft_data o WHERE o.hex = a.hex AND o.first_seen >= $2 AND o.first_seen < $1)
		GROUP BY a.hex`
	var fresh int64
	if err := s.pg.db.QueryRow(ctx, `SELECT COUNT(*) FROM (`+newToday+`) n`, start, liveFrom).Scan(&fresh); err != nil {
		return homeFirst{}, false
	}
	before := total - fresh
	if before < 0 {
		before = 0
	}
	if m := nextMilestone(before); fresh > 0 && m <= total {
		// the biggest round number passed today (normally the only one)
		for next := nextMilestone(m); next <= total; next = nextMilestone(m) {
			m = next
		}
		var a homeAircraft
		var when string
		if err := s.pg.db.QueryRow(ctx, `
			SELECT n.hex, TO_CHAR(n.fs AT TIME ZONE $3, 'YYYY-MM-DD"T"HH24:MI'),
			       COALESCE((SELECT TRIM(d.flight) FROM aircraft_data d WHERE d.hex = n.hex AND d.first_seen = n.fs LIMIT 1), ''),
			       COALESCE((SELECT d.t FROM aircraft_data d WHERE d.hex = n.hex AND d.first_seen = n.fs LIMIT 1), '')
			FROM (`+newToday+`) n ORDER BY n.fs, n.hex OFFSET $4 LIMIT 1`,
			start, liveFrom, tz, m-before-1).Scan(&a.Hex, &when, &a.Callsign, &a.Type); err != nil {
			return homeFirst{}, false
		}
		// an unidentified target ("~": a TIS-B relay or other made-up address)
		// has nothing real to name: the milestone shows the count only
		if strings.HasPrefix(a.Hex, "~") {
			return homeFirst{Kind: "milestone", When: &when, Number: m}, true
		}
		a.When = &when
		s.homeDetails(ctx, &a)
		return homeFirst{Kind: "milestone", When: &when, Aircraft: &a, Number: m}, true
	}
	m := nextMilestone(total)
	if left := m - total; left <= max(10, m/50) {
		return homeFirst{Kind: "soon", Number: m, Total: total}, true
	}
	return homeFirst{}, false
}
