package main

import (
	"context"
	"fmt"
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
	insertVisit(t, pg, testVisit{hex: "f00001", t: "B738", first: daysAgo(30), rDstNm: 50, rDir: 92})
	insertVisit(t, pg, testVisit{hex: "f00004", t: "B738", first: daysAgo(30), rDstNm: 100, rDir: 182})
	insertVisit(t, pg, testVisit{hex: "f00006", t: "B738", first: daysAgo(30), rDstNm: 10, rDir: 272})
	runRollup(pg)
	if _, err := pg.db.Exec(ctx, `DELETE FROM aircraft_data WHERE first_seen < NOW() - INTERVAL '2 days'`); err != nil {
		t.Fatal(err)
	}

	recent := now.Add(-3 * time.Minute)
	insertVisit(t, pg, testVisit{hex: "f00001", t: "B738", first: recent, rDstNm: 80, rDir: 92})   // back, and a record to the east
	insertVisit(t, pg, testVisit{hex: "f00003", t: "B738", first: recent, rDstNm: 60, rDir: 182})  // not a record (100 before)
	insertVisit(t, pg, testVisit{hex: "f00004", t: "B738", first: recent, rDstNm: 40, rDir: 182})  // back too, no record
	insertVisit(t, pg, testVisit{hex: "f00002", t: "A35K", first: recent, rDstNm: 30, rDir: 272})  // new type (the rarest highlight), record to the west
	insertVisit(t, pg, testVisit{hex: "f00005", t: "C17", first: recent, rDstNm: 20, rDir: 272})   // new type

	var res firstsResult
	if code := getJSON(t, r, "/api/k4nza/home?tz=UTC", &res); code != 200 {
		t.Fatalf("HTTP %d", code)
	}
	if res.Rarest.Type != "A35K" {
		t.Fatalf("rarest %q, want A35K", res.Rarest.Type)
	}
	got := map[string]int{}
	records := map[string]string{} // direction -> "hex nm old"
	for i, f := range res.Firsts {
		got[f.Kind] = i
		if f.Kind == "range" {
			records[f.Dir] = fmt.Sprintf("%s %g %g", f.Aircraft.Hex, f.Nm, f.OldNm)
		}
	}
	kinds := []string{}
	for _, f := range res.Firsts {
		kinds = append(kinds, f.Kind)
	}
	if fmt.Sprint(kinds) != "[range range type back back]" {
		t.Errorf("firsts in order %v, want [range range type back back]", kinds)
	}
	if len(res.Firsts) == 5 && res.Firsts[0].Nm < res.Firsts[1].Nm {
		t.Errorf("records %+v, want the farthest first", res.Firsts[:2])
	}
	// one line per direction with a new record
	if len(records) != 2 || records["E"] != "f00001 80 50" || records["W"] != "f00002 30 10" {
		t.Errorf("range records %v, want E f00001 80 (was 50) and W f00002 30 (was 10)", records)
	}
	if i, ok := got["type"]; !ok {
		t.Error("no new type")
	} else if f := res.Firsts[i]; f.Aircraft.Type != "C17" || f.Aircraft.Hex != "f00005" {
		t.Errorf("type %+v, want C17 (A35K is already the rarest highlight)", f)
	}
	backs := []string{}
	for _, f := range res.Firsts {
		if f.Kind == "back" {
			if f.Days < 29 || f.Days > 30 {
				t.Errorf("back %+v, want about 30 days", f)
			}
			backs = append(backs, f.Aircraft.Hex)
		}
	}
	if fmt.Sprint(backs) != "[f00001 f00004]" {
		t.Errorf("returns %v, want f00001 and f00004 (each its own line)", backs)
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
	var res firstsResult
	if code := getJSON(t, r, "/api/k4nza/home?tz=UTC", &res); code != 200 {
		t.Fatalf("HTTP %d", code)
	}
	if res.Firsts == nil || len(res.Firsts) != 0 {
		t.Errorf("firsts %+v, want an empty list on a new station", res.Firsts)
	}
}
