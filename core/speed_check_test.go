package main

import (
	"math"
	"testing"
	"time"
)

// plane returns one poll of an aircraft flying due north that has covered
// `movedKt` knots worth of distance after `t` seconds, reporting `gs`.
func plane(t, gs, movedKt float64) Aircraft {
	return Aircraft{
		Hex: "a013ee", Type: "adsb_icao",
		Gs: gs, Ias: 290, Tas: 450,
		Lat: 39.0 + movedKt*t/3600/60, Lon: -94.7,
		SeenPos: 0.3,
	}
}

func check(prevA, curA Aircraft, prevT, curT float64) (float64, int, int) {
	base := time.Unix(1794355616, 0)
	prev := newSpeedSample(1794355616+prevT, prevA)
	prev.seenAt = base.Add(time.Duration(prevT) * time.Second)
	cur := newSpeedSample(1794355616+curT, curA)
	cur.seenAt = base.Add(time.Duration(curT) * time.Second)
	return confirmedSpeeds(prev, cur)
}

func TestSpeedRealAircraftCounts(t *testing.T) {
	gs, ias, tas := check(plane(0, 452, 450), plane(5, 448, 450), 0, 5)
	if gs != 448 || ias != 290 || tas != 450 {
		t.Fatalf("steady 450 kt flight: got gs=%v ias=%v tas=%v", gs, ias, tas)
	}
}

func TestSpeedSingleSpikeIgnored(t *testing.T) {
	// N104DU: one bad 824.7 kt reading between normal ones
	if gs, _, _ := check(plane(0, 450, 450), plane(5, 824.7, 450), 0, 5); gs != 0 {
		t.Fatalf("spike after a normal reading counted: %v", gs)
	}
	if gs, _, _ := check(plane(0, 824.7, 450), plane(5, 450, 450), 0, 5); gs != 0 {
		t.Fatalf("spike before a normal reading counted: %v", gs)
	}
}

func TestSpeedRepeatedBadValueIgnored(t *testing.T) {
	// a bad value left standing in readsb while the aircraft really moves at 450 kt
	if gs, _, _ := check(plane(0, 824.7, 450), plane(5, 824.7, 450), 0, 5); gs != 0 {
		t.Fatalf("repeated bad value counted: %v", gs)
	}
}

func TestSpeedOtherSourcesIgnored(t *testing.T) {
	a, b := plane(0, 450, 450), plane(5, 450, 450)
	b.Mlat = []any{"lat", "lon", "gs"}
	if gs, _, _ := check(a, b, 0, 5); gs != 0 {
		t.Fatalf("MLAT speed counted: %v", gs)
	}
	a, b = plane(0, 450, 450), plane(5, 450, 450)
	a.Type, b.Type = "tisb_icao", "tisb_icao"
	if gs, _, _ := check(a, b, 0, 5); gs != 0 {
		t.Fatalf("TIS-B speed counted: %v", gs)
	}
	a, b = plane(0, 450, 450), plane(5, 450, 450)
	a.Type, b.Type = "mlat", "mlat"
	if gs, ias, tas := check(a, b, 0, 5); gs != 0 || ias != 0 || tas != 0 {
		t.Fatalf("MLAT aircraft speeds counted: %v %v %v", gs, ias, tas)
	}
}

func TestSpeedStaleOrMissingPositionIgnored(t *testing.T) {
	a, b := plane(0, 450, 450), plane(5, 450, 450)
	b.SeenPos = 20
	if gs, _, _ := check(a, b, 0, 5); gs != 0 {
		t.Fatalf("stale position counted: %v", gs)
	}
	a, b = plane(0, 450, 450), plane(5, 450, 450)
	b.Lat, b.Lon = 0, 0
	if gs, _, _ := check(a, b, 0, 5); gs != 0 {
		t.Fatalf("no position counted: %v", gs)
	}
	// readings too far apart in time
	if gs, _, _ := check(plane(0, 450, 450), plane(60, 450, 450), 0, 60); gs != 0 {
		t.Fatalf("readings a minute apart counted: %v", gs)
	}
}

func TestSpeedCeiling(t *testing.T) {
	if gs, _, _ := check(plane(0, 850, 850), plane(5, 850, 850), 0, 5); gs != 0 {
		t.Fatalf("850 kt counted: %v", gs)
	}
	// a fast jet with a strong tailwind is still fine
	if gs, _, _ := check(plane(0, 640, 640), plane(5, 645, 640), 0, 5); gs != 640 {
		t.Fatalf("640 kt not counted: %v", gs)
	}
}

func TestSpeedAirspeedSpikeIgnored(t *testing.T) {
	a, b := plane(0, 450, 450), plane(5, 450, 450)
	b.Tas = 518
	if _, _, tas := check(a, b, 0, 5); tas != 0 {
		t.Fatalf("TAS spike counted: %v", tas)
	}
}

func TestConfirmSpeedsNeedsTwoPolls(t *testing.T) {
	pg := &postgres{}
	now := 1794355616.0
	first := []Aircraft{plane(0, 450, 450)}
	pg.confirmSpeeds(now, first)
	if first[0].Gs != 0 || first[0].Ias != 0 || first[0].Tas != 0 {
		t.Fatalf("first poll kept unconfirmed speeds: %+v", first[0])
	}
	second := []Aircraft{plane(5, 455, 450)}
	pg.confirmSpeeds(now+5, second)
	if second[0].Gs != 450 {
		t.Fatalf("second poll: want 450, got %v", second[0].Gs)
	}
	spike := []Aircraft{plane(10, 824.7, 450)}
	pg.confirmSpeeds(now+10, spike)
	if spike[0].Gs != 0 {
		t.Fatalf("spike poll: want 0, got %v", spike[0].Gs)
	}
}

func TestDistanceNm(t *testing.T) {
	// one degree of latitude is 60 nm
	if d := distanceNm(39, -94.7, 40, -94.7); math.Abs(d-60) > 0.1 {
		t.Fatalf("want ~60 nm, got %v", d)
	}
}
