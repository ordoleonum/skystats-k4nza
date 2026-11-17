package main

// Database tests: they run the real SQL against a throwaway PostgreSQL.
// They are skipped unless SKYSTATS_DB_TESTS=1 and DB_HOST, DB_PORT, DB_USER,
// DB_PASSWORD and DB_NAME point at an empty test database (GitHub Actions
// starts one for every build; see .github/workflows/build.yml).

import (
	"context"
	"encoding/json"
	"math"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
)

const (
	testLat = 40.0
	testLon = -90.0
)

var (
	testDBOnce sync.Once
	testDB     *postgres
	testDBErr  error
)

func testPG(t *testing.T) *postgres {
	t.Helper()
	if os.Getenv("SKYSTATS_DB_TESTS") != "1" {
		t.Skip("database tests need SKYSTATS_DB_TESTS=1 and a test database")
	}
	testDBOnce.Do(func() {
		os.Setenv("LAT", "40.0")
		os.Setenv("LON", "-90.0")
		os.Setenv("RADIUS", "500")
		if testDBErr = RunDatabaseMigrations(); testDBErr != nil {
			return
		}
		testDB, testDBErr = NewPG(context.Background(), GetConnectionUrl())
		if testDBErr != nil {
			return
		}
		ensureK4nzaIndexes(testDB)
		ensureK4nzaSchema(testDB)
	})
	if testDBErr != nil {
		t.Fatalf("test database: %v", testDBErr)
	}
	ctx := context.Background()
	if _, err := testDB.db.Exec(ctx, `
		TRUNCATE aircraft_data, k4nza_aircraft_seen, k4nza_callsign_seen, k4nza_range_daily,
		         route_data, registration_data, interesting_aircraft RESTART IDENTITY;
		DELETE FROM cached_stats WHERE stat_key = 'k4nza_rollup_day'`); err != nil {
		t.Fatalf("clean test database: %v", err)
	}
	testDB.recentAircraftCache = map[string]*aircraftCacheEntry{}
	totalsExpires = time.Time{}
	os.Unsetenv("RETENTION_DAYS")
	return testDB
}

type testVisit struct {
	hex, flight, r, t string
	first             time.Time
	minutes           int
	rDstNm, rDir      float64
	lastLat, lastLon  float64
	lastKm            float64
	minKm             *float64
}

func insertVisit(t *testing.T, pg *postgres, v testVisit) {
	t.Helper()
	if v.minutes == 0 {
		v.minutes = 10
	}
	last := v.first.Add(time.Duration(v.minutes) * time.Minute)
	_, err := pg.db.Exec(context.Background(), `
		INSERT INTO aircraft_data (hex, flight, r, t, first_seen, first_seen_epoch, last_seen, last_seen_epoch,
		                           r_dst, r_dir, last_seen_lat, last_seen_lon, last_seen_distance, min_distance,
		                           alt_baro, gs)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, 30000, 400)`,
		v.hex, v.flight, v.r, v.t, v.first, v.first.Unix(), last, last.Unix(),
		v.rDstNm, v.rDir, v.lastLat, v.lastLon, v.lastKm, v.minKm)
	if err != nil {
		t.Fatalf("insert visit: %v", err)
	}
}

func daysAgo(d int) time.Time {
	return time.Now().UTC().AddDate(0, 0, -d).Add(-30 * time.Minute)
}

func testRouter(pg *postgres) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	NewAPIServer(pg).registerRoutes(r)
	return r
}

func getJSON(t *testing.T, r *gin.Engine, url string, out any) int {
	t.Helper()
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest("GET", url, nil))
	if out != nil && w.Code == http.StatusOK {
		if err := json.Unmarshal(w.Body.Bytes(), out); err != nil {
			t.Fatalf("%s: bad JSON: %v", url, err)
		}
	}
	return w.Code
}

func TestRollupKeepsAllTimeNumbers(t *testing.T) {
	pg := testPG(t)
	ctx := context.Background()

	insertVisit(t, pg, testVisit{hex: "a00001", flight: "DAL1", t: "B738", first: daysAgo(30)})
	insertVisit(t, pg, testVisit{hex: "a00001", flight: "DAL2", t: "B738", first: daysAgo(20)})
	insertVisit(t, pg, testVisit{hex: "b00002", flight: "RCH1", t: "C17", first: daysAgo(15)})
	insertVisit(t, pg, testVisit{hex: "a00001", flight: "DAL3", t: "B738", first: time.Now().Add(-time.Hour)})
	insertVisit(t, pg, testVisit{hex: "c00003", flight: "UAL9", t: "A320", first: time.Now().Add(-time.Hour)})
	if _, err := pg.db.Exec(ctx, `INSERT INTO route_data (route_callsign) VALUES ('DAL2')`); err != nil {
		t.Fatal(err)
	}

	check := func(stage string) {
		t.Helper()
		f, a, r, err := allTimeTotals(ctx, pg)
		if err != nil {
			t.Fatalf("%s: %v", stage, err)
		}
		if f != 5 || a != 3 || r != 1 {
			t.Errorf("%s: totals flights=%d aircraft=%d routed=%d, want 5 3 1", stage, f, a, r)
		}
	}
	check("before rollup")
	runRollup(pg)
	check("after rollup")
	runRollup(pg) // must not count anything twice
	check("after second rollup")

	os.Setenv("RETENTION_DAYS", "10")
	runRetention(pg)
	var left int
	if err := pg.db.QueryRow(ctx, `SELECT COUNT(*) FROM aircraft_data`).Scan(&left); err != nil {
		t.Fatal(err)
	}
	if left != 2 {
		t.Errorf("history limit left %d flights, want 2", left)
	}
	check("after history limit")

	// all-time top types still include the deleted flights
	r := testRouter(pg)
	var types []struct {
		Type  string `json:"aircraft_type"`
		Count int    `json:"count"`
	}
	if code := getJSON(t, r, "/api/stats/types/flights/all", &types); code != 200 {
		t.Fatalf("top types: HTTP %d", code)
	}
	counts := map[string]int{}
	for _, ty := range types {
		counts[ty.Type] = ty.Count
	}
	if counts["B738"] != 3 || counts["C17"] != 1 || counts["A320"] != 1 {
		t.Errorf("all-time flights by type = %v", counts)
	}
	if code := getJSON(t, r, "/api/stats/types/aircraft/all", &types); code != 200 {
		t.Fatalf("top aircraft types: HTTP %d", code)
	}
	if len(types) != 3 {
		t.Errorf("all-time aircraft types = %v, want 3 types", types)
	}
}

func TestRangeAllTimeSurvivesHistoryLimit(t *testing.T) {
	pg := testPG(t)
	ctx := context.Background()

	// first heard 150 nm due north; last heard about 92 nm due east
	insertVisit(t, pg, testVisit{hex: "d00004", first: daysAgo(20), rDstNm: 150, rDir: 5,
		lastLat: testLat, lastLon: testLon + 2, lastKm: 170.6})
	runRollup(pg)
	os.Setenv("RETENTION_DAYS", "10")
	runRetention(pg)

	sectors, err := allTimeRangeSectors(ctx, pg)
	if err != nil {
		t.Fatal(err)
	}
	if sectors[0] != 150 {
		t.Errorf("north sector = %v, want 150", sectors[0])
	}
	if math.Abs(sectors[8]-92.1) > 0.2 {
		t.Errorf("east sector (80-90 deg) = %v, want about 92.1", sectors[8])
	}
	for i, v := range sectors {
		if i != 0 && i != 8 && v != 0 {
			t.Errorf("sector %d = %v, want 0", i, v)
		}
	}
}

func TestSearch(t *testing.T) {
	pg := testPG(t)
	ctx := context.Background()
	ten, five := 10.0, 5.0

	insertVisit(t, pg, testVisit{hex: "a1b2c3", flight: "DAL1423", r: "N812DN", t: "B739", first: daysAgo(20), lastKm: 30, minKm: &ten})
	insertVisit(t, pg, testVisit{hex: "a1b2c3", flight: "DAL2210", r: "N812DN", t: "B739", first: time.Now().Add(-time.Hour), lastKm: 40, minKm: &five})
	insertVisit(t, pg, testVisit{hex: "ae1234", flight: "RCH123", r: "07-7178", t: "C17", first: daysAgo(3), lastKm: 80})
	if _, err := pg.db.Exec(ctx, `
		INSERT INTO registration_data (mode_s, registration, registered_owner, manufacturer, type)
		VALUES ('a1b2c3', 'N812DN', 'Delta Air Lines', 'Boeing', '737-932ER');
		INSERT INTO interesting_aircraft (icao, registration, operator, "group", category)
		VALUES ('AE1234', '07-7178', 'United States Air Force', 'Mil', 'Transport')`); err != nil {
		t.Fatal(err)
	}
	runRollup(pg) // the older visit is now only counted in the summary

	r := testRouter(pg)
	type result struct {
		Results []searchResult `json:"results"`
	}
	for q, want := range map[string]string{
		"n-812dn": "a1b2c3", "A1B2C3": "a1b2c3", "dal14": "a1b2c3", "delta": "a1b2c3",
		"c17": "ae1234", "air force": "ae1234", "07-71": "ae1234", "RCH": "ae1234",
	} {
		var res result
		if code := getJSON(t, r, "/api/search?q="+strings.ReplaceAll(q, " ", "+"), &res); code != 200 {
			t.Errorf("search %q: HTTP %d", q, code)
			continue
		}
		if len(res.Results) != 1 || res.Results[0].Hex != want {
			t.Errorf("search %q = %+v, want only %s", q, res.Results, want)
		}
	}
	var res result
	getJSON(t, r, "/api/search?q=n812dn", &res)
	if len(res.Results) == 1 {
		got := res.Results[0]
		if got.Visits != 2 || got.ClosestKm == nil || *got.ClosestKm != 5 || got.Operator != "Delta Air Lines" {
			t.Errorf("N812DN result = %+v", got)
		}
	}
	if code := getJSON(t, r, "/api/search?q=x", nil); code != 400 {
		t.Errorf("one-letter search: HTTP %d, want 400", code)
	}
	getJSON(t, r, "/api/search?q=c17&days=1", &res)
	if len(res.Results) != 0 {
		t.Errorf("c17 seen 3 days ago should not match days=1: %+v", res.Results)
	}

	// a whole interesting group
	res = result{}
	if code := getJSON(t, r, "/api/search?group=Mil", &res); code != 200 {
		t.Errorf("group search: HTTP %d", code)
	}
	if len(res.Results) != 1 || res.Results[0].Hex != "ae1234" || res.Results[0].Group != "Mil" {
		t.Errorf("group=Mil = %+v, want only ae1234", res.Results)
	}
	res = result{}
	getJSON(t, r, "/api/search?group=Mil&days=1", &res)
	if len(res.Results) != 0 {
		t.Errorf("group=Mil&days=1 should be empty: %+v", res.Results)
	}
	res = result{}
	getJSON(t, r, "/api/search?group=Civ", &res)
	if len(res.Results) != 0 {
		t.Errorf("group=Civ should be empty: %+v", res.Results)
	}
	if code := getJSON(t, r, "/api/search?group=Bad", nil); code != 400 {
		t.Errorf("unknown group: HTTP %d, want 400", code)
	}

	var detail struct {
		Visits    int64    `json:"visits"`
		InHistory int64    `json:"visits_in_history"`
		ClosestKm *float64 `json:"closest_km"`
		Owner     string   `json:"owner"`
		List      []visit  `json:"visits_list"`
	}
	if code := getJSON(t, r, "/api/search/aircraft/a1b2c3", &detail); code != 200 {
		t.Fatalf("aircraft detail: HTTP %d", code)
	}
	if detail.Visits != 2 || detail.InHistory != 2 || len(detail.List) != 2 || detail.ClosestKm == nil || *detail.ClosestKm != 5 {
		t.Errorf("detail = %+v", detail)
	}
	if len(detail.List) == 2 && detail.List[0].Callsign != "DAL2210" {
		t.Errorf("visits should be newest first, got %s", detail.List[0].Callsign)
	}
	if code := getJSON(t, r, "/api/search/aircraft/ffffff", nil); code != 404 {
		t.Errorf("unknown aircraft: HTTP %d, want 404", code)
	}

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest("GET", "/api/search/aircraft/a1b2c3/visits.csv", nil))
	lines := strings.Split(strings.TrimSpace(w.Body.String()), "\n")
	if len(lines) != 3 || !strings.Contains(lines[0], "closest_km") {
		t.Errorf("visits CSV = %q", w.Body.String())
	}
}

func TestClosestDistanceIsTracked(t *testing.T) {
	pg := testPG(t)
	ctx := context.Background()
	now := float64(time.Now().Unix())
	at := func(km float64) Aircraft {
		// due north of the station; 1 degree of latitude is about 111 km
		return Aircraft{Hex: "e00005", Lat: testLat + km/111.0, Lon: testLon}
	}
	forceWrite := func() {
		if e := pg.recentAircraftCache["e00005"]; e != nil {
			e.lastWritten = time.Now().Add(-time.Hour)
		}
	}

	pg.updateDatabase(now, []Aircraft{at(50)})
	pg.updateDatabase(now+5, []Aircraft{at(12)}) // not written yet (30 s rule)...
	pg.updateDatabase(now+10, []Aircraft{at(30)})
	forceWrite()
	pg.updateDatabase(now+15, []Aircraft{at(40)}) // ...but the 12 km is remembered

	var rows int
	var minKm float64
	if err := pg.db.QueryRow(ctx, `SELECT COUNT(*), MIN(min_distance)::float8 FROM aircraft_data WHERE hex = 'e00005'`).
		Scan(&rows, &minKm); err != nil {
		t.Fatal(err)
	}
	if rows != 1 || math.Abs(minKm-12) > 0.5 {
		t.Errorf("rows=%d closest=%v km, want 1 row and about 12 km", rows, minKm)
	}
}
