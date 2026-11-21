package main

// Tests that need no database: input checks and small calculations.

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/tomcarman/skystats/data"
)

func TestParseSearchNormalises(t *testing.T) {
	cases := []struct {
		in, key, hex string
		ok           bool
	}{
		{"N-812DN", "N812DN", "", true},
		{"  n812dn ", "N812DN", "", true},
		{"a1b2c3", "A1B2C3", "a1b2c3", true},
		{"~1234ab", "~1234AB", "~1234ab", true},
		{"DAL 1423", "DAL1423", "", true},
		{"x", "", "", false},
		{"-%_", "", "", false},
		{"Delta Air Lines", "DELTAAIRLINES", "", true},
	}
	for _, c := range cases {
		got, ok := parseSearch(c.in)
		if ok != c.ok {
			t.Errorf("parseSearch(%q) ok = %v, want %v", c.in, ok, c.ok)
			continue
		}
		if !ok {
			continue
		}
		if got.key != c.key || got.hex != c.hex {
			t.Errorf("parseSearch(%q) = key %q hex %q, want %q %q", c.in, got.key, got.hex, c.key, c.hex)
		}
	}
}

func TestEscapeLike(t *testing.T) {
	if got := escapeLike(`50%_off\`); got != `50\%\_off\\` {
		t.Errorf("escapeLike = %q", got)
	}
}

func TestValidHex(t *testing.T) {
	for in, want := range map[string]bool{
		"a1b2c3": true, "A1B2C3": true, "~a1b2c3": true,
		"a1b2c": false, "a1b2c3d": false, "zzzzzz": false, "a1b2c3'": false,
	} {
		if _, ok := validHex(in); ok != want {
			t.Errorf("validHex(%q) = %v, want %v", in, ok, want)
		}
	}
}

func TestSampleEvery(t *testing.T) {
	for _, c := range []struct{ total, want, n int64 }{
		{0, 2500, 1}, {2500, 2500, 1}, {2501, 2500, 2}, {10000, 2500, 4}, {10001, 2500, 5}, {5, 0, 1},
	} {
		if got := sampleEvery(c.total, c.want); got != c.n {
			t.Errorf("sampleEvery(%d, %d) = %d, want %d", c.total, c.want, got, c.n)
		}
	}
}

func TestMinPositive(t *testing.T) {
	for _, c := range []struct{ a, b, want float64 }{
		{0, 5, 5}, {5, 0, 5}, {3, 5, 3}, {5, 3, 3}, {0, 0, 0},
	} {
		if got := minPositive(c.a, c.b); got != c.want {
			t.Errorf("minPositive(%v, %v) = %v, want %v", c.a, c.b, got, c.want)
		}
	}
}

func TestUtcDay(t *testing.T) {
	loc := time.FixedZone("CST", -6*3600)
	in := time.Date(2026, 11, 13, 21, 30, 0, 0, loc) // 03:30 UTC on the 14th
	if got := utcDay(in); !got.Equal(time.Date(2026, 11, 14, 0, 0, 0, 0, time.UTC)) {
		t.Errorf("utcDay = %v", got)
	}
}

func TestSameOriginWrites(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(sameOriginWrites)
	r.Any("/x", func(c *gin.Context) { c.Status(http.StatusOK) })

	cases := []struct {
		method  string
		headers map[string]string
		want    int
	}{
		{"GET", map[string]string{"Sec-Fetch-Site": "cross-site"}, 200},
		{"PUT", map[string]string{"Sec-Fetch-Site": "same-origin"}, 200},
		{"PUT", map[string]string{"Sec-Fetch-Site": "cross-site"}, 403},
		{"PUT", map[string]string{"Sec-Fetch-Site": "same-site"}, 403},
		{"POST", map[string]string{"Origin": "http://evil.example"}, 403},
		{"POST", map[string]string{"Origin": "http://feeder.local"}, 200},
		{"POST", map[string]string{}, 200}, // curl / scripts
	}
	for _, c := range cases {
		req := httptest.NewRequest(c.method, "http://feeder.local/x", nil)
		for k, v := range c.headers {
			req.Header.Set(k, v)
		}
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		if w.Code != c.want {
			t.Errorf("%s %v -> %d, want %d", c.method, c.headers, w.Code, c.want)
		}
	}
}

// Building the router panics if two routes conflict.
func TestRoutesRegister(t *testing.T) {
	gin.SetMode(gin.TestMode)
	s := &APIServer{}
	s.registerRoutes(gin.New())
}

func TestFeederMilitary(t *testing.T) {
	ia, ok := feederMilitary(Aircraft{Hex: "aeefbb", T: "E6", DbFlags: 1, Flight: "IRON12"})
	if !ok || ia.Icao != "AEEFBB" || ia.Group.String != "Mil" || ia.IcaoType.String != "E6" ||
		ia.Registration.Valid || ia.Flight != "IRON12" || ia.Hex != "aeefbb" {
		t.Errorf("military flag: %+v, %v", ia, ok)
	}
	// LADD (8) or PIA (4) alone is not military
	for _, flags := range []int{0, 2, 4, 8} {
		if _, ok := feederMilitary(Aircraft{Hex: "a00001", DbFlags: flags}); ok {
			t.Errorf("dbFlags %d tagged as military", flags)
		}
	}
	if _, ok := feederMilitary(Aircraft{Hex: " ", DbFlags: 1}); ok {
		t.Errorf("empty hex tagged as military")
	}
}

func TestFeederMilitaryDetails(t *testing.T) {
	ia, _ := feederMilitary(Aircraft{Hex: "ae1234", DbFlags: 1, OwnOp: "UNITED STATES AIR FORCE", Desc: "BOEING C-32A"})
	if ia.Operator.String != "United States Air Force" || ia.Type.String != "Boeing C-32A" {
		t.Errorf("feeder database details: %q / %q", ia.Operator.String, ia.Type.String)
	}
	if ia, _ := feederMilitary(Aircraft{Hex: "ae1235", DbFlags: 1}); ia.Operator.Valid || ia.Type.Valid {
		t.Errorf("empty details stored: %+v", ia)
	}
	for in, want := range map[string]string{
		"GULFSTREAM 5":                   "Gulfstream 5",
		"U S BANK NA TRUSTEE":            "U S Bank NA Trustee",
		"Cargojet Airways":               "Cargojet Airways",
		"AIR WISCONSIN LLC":              "Air Wisconsin LLC",
		"DEPARTMENT OF THE NAVY":         "Department of the Navy",
		"MCDONNELL-DOUGLAS F/A-18":       "McDonnell-Douglas F/A-18",
		"UNITED STATES AIR FORCE (USAF)": "United States Air Force (USAF)",
		"THE BOEING COMPANY":             "The Boeing Company",
		"  ":                             "",
	} {
		if got := niceCase(in); got != want {
			t.Errorf("niceCase(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestTypeNames(t *testing.T) {
	for code, want := range map[string]string{
		"B738": "Boeing 737-800",
		"b38m": "Boeing 737 MAX 8",
		"MD11": "McDonnell-Douglas MD-11",
		"DH8D": "de Havilland DHC-8-400 Dash 8",
		"E6":   "Boeing E-6 Mercury",
	} {
		if got, ok := data.LookupTypeName(code); !ok || got != want {
			t.Errorf("LookupTypeName(%q) = %q, %v; want %q", code, got, ok, want)
		}
	}
	if _, ok := data.LookupTypeName("ZZZZ"); ok {
		t.Errorf("unknown code found")
	}
}
