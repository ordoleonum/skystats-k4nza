package main

import "testing"

// A trimmed aircraft.json as written by readsb 3.16: rssi is a decimal and an
// aircraft on the ground reports alt_baro as the string "ground".
const sampleAircraftJSON = `{
  "now": 1794355616.0,
  "messages": 64528,
  "aircraft": [
    {"hex": "a1b2c3", "type": "adsb_icao", "flight": "SWA1234 ", "alt_baro": 35000,
     "gs": 451.2, "lat": 38.91, "lon": -94.72, "messages": 120, "seen": 0.4, "rssi": -23.8},
    {"hex": "a4d5e6", "type": "adsb_icao", "flight": "N123AB  ", "alt_baro": "ground",
     "gs": 12.0, "lat": 39.29, "lon": -94.71, "messages": 33, "seen": 1.1, "rssi": -30.1}
  ]
}`

func TestParseAircraftJSONKeepsAllAircraft(t *testing.T) {
	resp, err := parseAircraftJSON([]byte(sampleAircraftJSON))
	if err != nil {
		t.Fatalf("parse failed: %v", err)
	}
	if resp.Now == 0 {
		t.Fatalf("now not parsed")
	}
	if len(resp.Aircraft) != 2 {
		t.Fatalf("want 2 aircraft, got %d", len(resp.Aircraft))
	}
	first := resp.Aircraft[0]
	if first.Rssi != -23.8 || first.AltBaro != 35000 || first.Lat != 38.91 {
		t.Errorf("first aircraft fields wrong: rssi=%v alt=%v lat=%v", first.Rssi, first.AltBaro, first.Lat)
	}
	ground := resp.Aircraft[1]
	// alt_baro "ground" can't go into an int: that field is skipped, the rest is kept
	if ground.AltBaro != 0 || ground.Lat != 39.29 || ground.Rssi != -30.1 || ground.Messages != 33 {
		t.Errorf("ground aircraft fields wrong: alt=%v lat=%v rssi=%v msgs=%v", ground.AltBaro, ground.Lat, ground.Rssi, ground.Messages)
	}
}

func TestParseAircraftJSONRejectsBrokenFile(t *testing.T) {
	if _, err := parseAircraftJSON([]byte(`{"now": 1794355616.0, "aircraft": [`)); err == nil {
		t.Fatal("expected an error for truncated JSON")
	}
}
