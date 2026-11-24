package main

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"
)

func TestNextMilestone(t *testing.T) {
	for n, want := range map[int64]int64{
		0: 100, 99: 100, 100: 250, 499: 500, 500: 1000, 7986: 8000, 9999: 10000,
		10000: 15000, 49999: 50000, 50000: 60000, 123456: 130000,
	} {
		if got := nextMilestone(n); got != want {
			t.Errorf("nextMilestone(%d) = %d, want %d", n, got, want)
		}
	}
}

type firstsResult struct {
	Firsts []struct {
		Kind     string  `json:"kind"`
		Nm       float64 `json:"distance_nm"`
		OldNm    float64 `json:"old_nm"`
		Dir      string  `json:"direction"`
		Days     int     `json:"days"`
		Aircraft struct {
			Hex  string `json:"hex"`
			Type string `json:"type"`
		} `json:"aircraft"`
	} `json:"firsts"`
	Rarest struct {
		Type string `json:"type"`
	} `json:"rarest"`
}

func TestK4nzaHomeFirsts(t *testing.T) {
	now := time.Now().UTC()
	if now.Hour() == 0 && now.Minute() < 10 {
		t.Skip("too close to midnight UTC")
	}
	pg := testPG(t)
	ctx := context.Background()
	r := testRouter(pg)

	// a month ago: rolled up, then trimmed from the flight table, so only the
	// summary tables remember it
	insertVisit(t, pg, testVisit{hex: "f00001", flight: "AAL100", t: "B738", first: daysAgo(30), rDstNm: 50, rDir: 92})
	insertVisit(t, pg, testVisit{hex: "f00004", t: "B738", first: daysAgo(30), rDstNm: 100, rDir: 182})
	insertVisit(t, pg, testVisit{hex: "f00006", t: "B738", first: daysAgo(30), rDstNm: 10, rDir: 272})
	insertVisit(t, pg, testVisit{hex: "f00007", t: "B738", first: daysAgo(30), rDstNm: 100, rDir: 2})
	runRollup(pg)
	if _, err := pg.db.Exec(ctx, `DELETE FROM aircraft_data WHERE first_seen < NOW() - INTERVAL '2 days'`); err != nil {
		t.Fatal(err)
	}
	if _, err := pg.db.Exec(ctx, `
		INSERT INTO registration_data (mode_s, registered_owner_country_name) VALUES
			('f00001', 'United States'), ('f00003', 'United States'), ('f00002', 'Qatar')`); err != nil {
		t.Fatal(err)
	}

	recent := now.Add(-3 * time.Minute)
	insertVisit(t, pg, testVisit{hex: "f00001", flight: "AAL101", t: "B738", first: recent, rDstNm: 80, rDir: 92}) // record to the east; American isn't new
	insertVisit(t, pg, testVisit{hex: "f00003", t: "B738", first: recent, rDstNm: 60, rDir: 182})                  // not a record (100 before)
	insertVisit(t, pg, testVisit{hex: "f00002", flight: "QTR713", t: "A35K", first: recent, rDstNm: 30, rDir: 272}) // new type (the rarest highlight), airline, country; record to the west
	insertVisit(t, pg, testVisit{hex: "f00005", flight: "ZZZ123", t: "C17", first: recent, rDstNm: 20, rDir: 272})  // new type; ZZZ isn't an airline
	insertVisit(t, pg, testVisit{hex: "f00008", t: "B738", first: recent, rDstNm: 100.5, rDir: 2})                 // only 0.5 nm farther: not a record
	recordNotableEvents(pg) // the once-a-minute job

	type event struct {
		Kind     string  `json:"kind"`
		Nm       float64 `json:"distance_nm"`
		OldNm    float64 `json:"old_nm"`
		Dir      string  `json:"direction"`
		Code     string  `json:"code"`
		Airline  string  `json:"airline"`
		Country  string  `json:"country"`
		Aircraft struct {
			Hex  string `json:"hex"`
			Type string `json:"type"`
		} `json:"aircraft"`
	}
	var res struct {
		Firsts []event `json:"firsts"`
		Rarest struct {
			Type string `json:"type"`
		} `json:"rarest"`
	}
	describe := func(list []event) (kinds []string, records map[string]string) {
		records = map[string]string{}
		for _, f := range list {
			kinds = append(kinds, f.Kind)
			if f.Kind == "range" {
				records[f.Dir] = fmt.Sprintf("%s %g %g", f.Aircraft.Hex, f.Nm, f.OldNm)
			}
		}
		return
	}
	if code := getJSON(t, r, "/api/k4nza/home?tz=UTC", &res); code != 200 {
		t.Fatalf("HTTP %d", code)
	}
	if res.Rarest.Type != "A35K" {
		t.Fatalf("rarest %q, want A35K", res.Rarest.Type)
	}
	kinds, records := describe(res.Firsts)
	if fmt.Sprint(kinds) != "[range range type airline country]" {
		t.Errorf("firsts in order %v, want [range range type airline country]", kinds)
	}
	// one line per direction with a new record, farthest first
	if len(records) != 2 || records["E"] != "f00001 80 50" || records["W"] != "f00002 30 10" {
		t.Errorf("range records %v, want E f00001 80 (was 50) and W f00002 30 (was 10)", records)
	}
	if len(res.Firsts) == 5 && res.Firsts[0].Nm < res.Firsts[1].Nm {
		t.Errorf("records %+v, want the farthest first", res.Firsts[:2])
	}
	for _, f := range res.Firsts {
		switch f.Kind {
		case "type":
			if f.Aircraft.Type != "C17" || f.Aircraft.Hex != "f00005" {
				t.Errorf("type %+v, want C17 (A35K is already the rarest highlight)", f)
			}
		case "airline":
			if f.Code != "QTR" || f.Airline != "Qatar Airways" || f.Aircraft.Hex != "f00002" {
				t.Errorf("airline %+v, want QTR Qatar Airways (f00002)", f)
			}
		case "country":
			if f.Country != "Qatar" || f.Aircraft.Hex != "f00002" {
				t.Errorf("country %+v, want Qatar (f00002)", f)
			}
		}
	}

	// once logged, an event stays for the day even when its flight is gone;
	// a record beaten again later the same day is replaced by the farther one
	if _, err := pg.db.Exec(ctx, `DELETE FROM aircraft_data WHERE hex = 'f00002'`); err != nil {
		t.Fatal(err)
	}
	insertVisit(t, pg, testVisit{hex: "f00009", t: "B738", first: recent, rDstNm: 90, rDir: 92})
	recordNotableEvents(pg)
	res.Firsts = nil
	getJSON(t, r, "/api/k4nza/home?tz=UTC", &res)
	kinds, records = describe(res.Firsts)
	if records["E"] != "f00009 90 50" {
		t.Errorf("east record %q, want the farther f00009 90 (was 50)", records["E"])
	}
	if records["W"] != "f00002 30 10" || !strings.Contains(fmt.Sprint(kinds), "airline country") {
		t.Errorf("after f00002's flight was removed: %v %v, want its record, airline and country still listed", kinds, records)
	}

	// the log for the Records tab: every event, the rarest type's included
	var feed struct {
		Days []struct {
			Date   string `json:"date"`
			Events []struct {
				Kind string `json:"kind"`
			} `json:"events"`
		} `json:"days"`
		More bool `json:"more"`
	}
	if code := getJSON(t, r, "/api/k4nza/events", &feed); code != 200 {
		t.Fatalf("events: HTTP %d", code)
	}
	if len(feed.Days) != 1 || feed.Days[0].Date != now.Format("2006-01-02") || feed.More {
		t.Fatalf("events log %+v, want today only", feed)
	}
	kinds = nil
	for _, e := range feed.Days[0].Events {
		kinds = append(kinds, e.Kind)
	}
	if fmt.Sprint(kinds) != "[range range type type airline country]" {
		t.Errorf("logged events %v, want two records, A35K and C17, Qatar Airways, Qatar", kinds)
	}
}

// Busiest day ever: once today has more different aircraft than any day before.
func TestK4nzaBusiestDay(t *testing.T) {
	now := time.Now().UTC()
	if now.Hour() == 0 && now.Minute() < 10 {
		t.Skip("too close to midnight UTC")
	}
	pg := testPG(t)
	r := testRouter(pg)
	// the best day so far: 3 aircraft, 10 days ago (and a quieter one before it)
	for i := 0; i < 3; i++ {
		insertVisit(t, pg, testVisit{hex: fmt.Sprintf("c%05d", i), t: "B738", first: daysAgo(10), rDstNm: 20, rDir: 90})
	}
	insertVisit(t, pg, testVisit{hex: "c00009", t: "B738", first: daysAgo(12), rDstNm: 20, rDir: 90})
	recent := now.Add(-3 * time.Minute)
	for i := 0; i < 3; i++ {
		insertVisit(t, pg, testVisit{hex: fmt.Sprintf("d%05d", i), t: "B738", first: recent, rDstNm: 20, rDir: 90})
	}
	type result struct {
		Firsts []struct {
			Kind     string `json:"kind"`
			Count    int64  `json:"count"`
			OldCount int64  `json:"old_count"`
			OldDate  string `json:"old_date"`
		} `json:"firsts"`
	}
	busiest := func() (n, old int64, oldDate string, found bool) {
		var res result
		getJSON(t, r, "/api/k4nza/home?tz=UTC", &res)
		for _, f := range res.Firsts {
			if f.Kind == "busiest" {
				return f.Count, f.OldCount, f.OldDate, true
			}
		}
		return 0, 0, "", false
	}
	recordNotableEvents(pg)
	if _, _, _, found := busiest(); found {
		t.Fatal("3 aircraft today ties the best day: not a record yet")
	}
	insertVisit(t, pg, testVisit{hex: "d00003", t: "B738", first: recent, rDstNm: 20, rDir: 90})
	recordNotableEvents(pg)
	n, old, oldDate, found := busiest()
	if !found || n != 4 || old != 3 || oldDate != daysAgo(10).Format("2006-01-02") {
		t.Fatalf("busiest %d (old best %d on %s, found %v), want 4 beating 3 from 10 days ago", n, old, oldDate, found)
	}
	// the day keeps counting
	insertVisit(t, pg, testVisit{hex: "d00004", t: "B738", first: recent, rDstNm: 20, rDir: 90})
	recordNotableEvents(pg)
	if n, _, _, _ := busiest(); n != 5 {
		t.Errorf("busiest after one more aircraft: %d, want 5", n)
	}
}

func TestK4nzaHomeFirstsNewStation(t *testing.T) {
	now := time.Now().UTC()
	if now.Hour() == 0 && now.Minute() < 10 {
		t.Skip("too close to midnight UTC")
	}
	pg := testPG(t)
	r := testRouter(pg)
	// two days of history: too new for records, new types or returns
	insertVisit(t, pg, testVisit{hex: "f00001", t: "B738", first: now.Add(-48 * time.Hour), rDstNm: 50, rDir: 92})
	insertVisit(t, pg, testVisit{hex: "f00001", t: "B738", first: now.Add(-3 * time.Minute), rDstNm: 80, rDir: 92})
	insertVisit(t, pg, testVisit{hex: "f00002", t: "C17", first: now.Add(-3 * time.Minute), rDstNm: 20, rDir: 272})
	recordNotableEvents(pg)
	var res firstsResult
	if code := getJSON(t, r, "/api/k4nza/home?tz=UTC", &res); code != 200 {
		t.Fatalf("HTTP %d", code)
	}
	if res.Firsts == nil || len(res.Firsts) != 0 {
		t.Errorf("firsts %+v, want an empty list on a new station", res.Firsts)
	}
}

// The 100th different aircraft is an unidentified "~" target: the milestone
// still shows, without an aircraft.
func TestK4nzaMilestoneUnidentified(t *testing.T) {
	now := time.Now().UTC()
	if now.Hour() == 0 && now.Minute() < 10 {
		t.Skip("too close to midnight UTC")
	}
	pg := testPG(t)
	r := testRouter(pg)
	for i := 0; i < 99; i++ {
		insertVisit(t, pg, testVisit{hex: fmt.Sprintf("b%05d", i), t: "B738", first: now.Add(-48 * time.Hour), rDstNm: 20, rDir: 90})
	}
	insertVisit(t, pg, testVisit{hex: "~abc123", first: now.Add(-5 * time.Minute), rDstNm: 20, rDir: 90}) // the 100th
	insertVisit(t, pg, testVisit{hex: "b00100", t: "B738", first: now.Add(-3 * time.Minute), rDstNm: 20, rDir: 90})
	recordNotableEvents(pg)
	var res struct {
		Firsts []struct {
			Kind     string `json:"kind"`
			Number   int64  `json:"number"`
			Aircraft *struct {
				Hex string `json:"hex"`
			} `json:"aircraft"`
		} `json:"firsts"`
	}
	if code := getJSON(t, r, "/api/k4nza/home?tz=UTC", &res); code != 200 {
		t.Fatalf("HTTP %d", code)
	}
	if len(res.Firsts) != 1 || res.Firsts[0].Kind != "milestone" || res.Firsts[0].Number != 100 || res.Firsts[0].Aircraft != nil {
		t.Errorf("firsts %+v, want the 100th aircraft milestone without an aircraft", res.Firsts)
	}
}
