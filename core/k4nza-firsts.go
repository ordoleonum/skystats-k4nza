package main

// "Today's firsts" on the feeder's home page: things that happened at this
// station today for the first time ever - a new range record in some
// direction, a type never seen before, an aircraft back after a long time,
// a round number of different aircraft. Everything is judged against the
// summary tables as well as the flight table, so it stays right when old
// flights are trimmed (RETENTION_DAYS).

import (
	"context"
	"sort"
	"time"
)

type homeFirst struct {
	// range | type | back | milestone | soon
	Kind     string        `json:"kind"`
	When     *string       `json:"when,omitempty"` // station time, YYYY-MM-DDTHH:MM
	Aircraft *homeAircraft `json:"aircraft,omitempty"`
	Nm       float64       `json:"distance_nm,omitempty"` // range: today's farthest in that direction
	OldNm    float64       `json:"old_nm,omitempty"`      // range: the best before today
	Dir      string        `json:"direction,omitempty"`   // range: 16-point compass
	Days     int           `json:"days,omitempty"`        // back: days since the last visit
	Number   int64         `json:"number,omitempty"`      // milestone / soon: the round number
	Total    int64         `json:"total,omitempty"`       // soon: different aircraft so far
}

// Records, new types and returns need some history first: on a new station
// nearly everything is a "first".
const firstsMinHistory = 7 * 24 * time.Hour

// an aircraft counts as "back" after this long away
const firstsBackAfter = 7 * 24 * time.Hour

// at most this many of each kind (the card shows the first and folds the rest)
const firstsPerKind = 5

// homeFirsts builds the list: records, new types, returns, round numbers.
// skipType is the type already shown as the rarest-type highlight.
func (s *APIServer) homeFirsts(ctx context.Context, start time.Time, tz, skipType string) []homeFirst {
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
		out = append(out, s.firstRanges(ctx, start, cut, liveFrom, tz)...)
		out = append(out, s.firstTypes(ctx, start, liveFrom, tz, skipType)...)
		out = append(out, s.firstBacks(ctx, start, tz)...)
	}
	if f, ok := s.firstMilestone(ctx, start, liveFrom, tz); ok {
		out = append(out, f)
	}

	// records, then new types, then returns, then round numbers; within a kind
	// the best first (farthest record, newest type, longest away)
	order := map[string]int{"range": 0, "type": 1, "back": 2, "milestone": 3, "soon": 4}
	sort.SliceStable(out, func(i, j int) bool { return order[out[i].Kind] < order[out[j].Kind] })
	return out
}

// firstRanges: today's new range records, one per 10-degree direction,
// farthest first.
func (s *APIServer) firstRanges(ctx context.Context, start, cut, liveFrom time.Time, tz string) []homeFirst {
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
		// a direction never heard before isn't a record, just a first
		if before[i] > 0 && today[i] > before[i] {
			records = append(records, i)
		}
	}
	sort.SliceStable(records, func(a, b int) bool { return today[records[a]] > today[records[b]] })
	for _, i := range records {
		if len(out) == firstsPerKind {
			break
		}
		far, ok := s.farthestInSector(ctx, start, tz, i)
		if !ok {
			continue
		}
		out = append(out, homeFirst{Kind: "range", When: far.When, Nm: today[i], OldNm: before[i],
			Dir: compass(float64(i*10) + 5), Aircraft: &far})
	}
	return out
}

// firstTypes: aircraft types heard today for the first time ever, newest
// first, other than the rarest-type highlight's.
func (s *APIServer) firstTypes(ctx context.Context, start, liveFrom time.Time, tz, skipType string) []homeFirst {
	out := []homeFirst{}
	rows, err := s.pg.db.Query(ctx, `
		WITH today AS (
			SELECT DISTINCT ON (t) t, hex, COALESCE(TRIM(flight), '') AS cs, first_seen
			FROM aircraft_data
			WHERE first_seen >= $1 AND t IS NOT NULL AND t <> '' AND hex NOT LIKE '~%'
			ORDER BY t, first_seen
		)
		SELECT today.t, today.hex, today.cs, TO_CHAR(today.first_seen AT TIME ZONE $3, 'YYYY-MM-DD"T"HH24:MI')
		FROM today
		WHERE NOT EXISTS (SELECT 1 FROM k4nza_aircraft_seen s WHERE s.t = today.t AND s.first_seen < $1)
		  AND NOT EXISTS (SELECT 1 FROM aircraft_data a WHERE a.t = today.t AND a.first_seen >= $2 AND a.first_seen < $1)
		ORDER BY today.first_seen DESC`, start, liveFrom, tz)
	if err != nil {
		return out
	}
	for rows.Next() && len(out) < firstsPerKind {
		a := &homeAircraft{}
		var when string
		if rows.Scan(&a.Type, &a.Hex, &a.Callsign, &when) != nil || a.Type == skipType {
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
func (s *APIServer) firstBacks(ctx context.Context, start time.Time, tz string) []homeFirst {
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
		start, tz, firstsBackAfter.Seconds(), firstsPerKind)
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
