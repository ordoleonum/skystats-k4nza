package main

import (
	"math"
	"strings"
	"time"
)

// K4NZA: speed records only from readings that check out.
//
// readsb reports the latest speed it decoded. A single garbled message, a
// noisy MLAT/TIS-B estimate, or a bad value left standing while reception
// fades used to become the aircraft's "max speed" (e.g. an A220 at 825 kt).
// A speed now only counts when two readings from consecutive polls
//   - come from the aircraft's own ADS-B (not MLAT or TIS-B),
//   - agree with each other,
//   - agree with how fast the aircraft actually moved between the two
//     positions, and
//   - are below a hard ceiling no aircraft near a feeder reaches.
// The recorded value is the lower of the two readings.

const (
	speedMaxPositionAge = 5.0   // s: a position older than this is too stale to check against
	speedMinGap         = 2.0   // s between the two positions
	speedMaxGap         = 30.0  // s between the two positions
	speedAgreeKt        = 25.0  // two readings may differ by this much
	speedTrackMinKt     = 30.0  // reported vs. measured speed tolerance (absolute) ...
	speedTrackFraction  = 0.10  // ... or this fraction of the speed, whichever is larger
	speedCeilingKt      = 800.0 // nothing that passes a feeder flies faster than this
	speedSampleExpiry   = 2 * time.Minute
)

// speedSample is one aircraft's readings from one poll.
type speedSample struct {
	gs       float64
	ias, tas int
	lat, lon float64
	posTime  float64 // epoch of the position
	gsOK     bool    // gs and position come from the aircraft's own ADS-B
	iasOK    bool
	tasOK    bool
	seenAt   time.Time
}

func listHas(list []any, field string) bool {
	for _, v := range list {
		if s, ok := v.(string); ok && s == field {
			return true
		}
	}
	return false
}

func fromOtherSource(a Aircraft, field string) bool {
	return listHas(a.Mlat, field) || listHas(a.Tisb, field)
}

func newSpeedSample(nowEpoch float64, a Aircraft) speedSample {
	s := speedSample{
		gs: a.Gs, ias: a.Ias, tas: a.Tas,
		lat: a.Lat, lon: a.Lon,
		posTime: nowEpoch - a.SeenPos,
		seenAt:  time.Now(),
	}
	ownADSB := strings.HasPrefix(a.Type, "adsb") || strings.HasPrefix(a.Type, "adsr")
	hasPos := (a.Lat != 0 || a.Lon != 0) && a.SeenPos >= 0 && a.SeenPos <= speedMaxPositionAge &&
		!fromOtherSource(a, "lat") && !fromOtherSource(a, "lon")
	s.gsOK = ownADSB && hasPos && a.Gs > 0 && !fromOtherSource(a, "gs")
	s.iasOK = ownADSB && a.Ias > 0 && !fromOtherSource(a, "ias")
	s.tasOK = ownADSB && a.Tas > 0 && !fromOtherSource(a, "tas")
	return s
}

// distanceNm is the great-circle distance between two points in nautical miles.
func distanceNm(lat1, lon1, lat2, lon2 float64) float64 {
	const earthRadiusNm = 3440.065
	rad := math.Pi / 180
	dLat := (lat2 - lat1) * rad
	dLon := (lon2 - lon1) * rad
	h := math.Sin(dLat/2)*math.Sin(dLat/2) +
		math.Cos(lat1*rad)*math.Cos(lat2*rad)*math.Sin(dLon/2)*math.Sin(dLon/2)
	return 2 * earthRadiusNm * math.Asin(math.Min(1, math.Sqrt(h)))
}

// confirmedSpeeds returns the speeds both samples back up; 0 means not confirmed.
func confirmedSpeeds(prev, cur speedSample) (gs float64, ias, tas int) {
	dt := cur.posTime - prev.posTime
	if prev.gsOK && cur.gsOK && dt >= speedMinGap && dt <= speedMaxGap &&
		math.Abs(cur.gs-prev.gs) <= speedAgreeKt {
		avg := (cur.gs + prev.gs) / 2
		measured := distanceNm(prev.lat, prev.lon, cur.lat, cur.lon) / dt * 3600
		tolerance := math.Max(speedTrackMinKt, speedTrackFraction*avg)
		low := math.Min(cur.gs, prev.gs)
		if math.Abs(measured-avg) <= tolerance && low <= speedCeilingKt {
			gs = low
		}
	}
	// airspeeds have no position to check against: two agreeing readings in a row
	gap := cur.seenAt.Sub(prev.seenAt).Seconds()
	if gap <= speedMaxGap {
		if prev.iasOK && cur.iasOK && math.Abs(float64(cur.ias-prev.ias)) <= speedAgreeKt &&
			float64(min(cur.ias, prev.ias)) <= speedCeilingKt {
			ias = min(cur.ias, prev.ias)
		}
		if prev.tasOK && cur.tasOK && math.Abs(float64(cur.tas-prev.tas)) <= speedAgreeKt &&
			float64(min(cur.tas, prev.tas)) <= speedCeilingKt {
			tas = min(cur.tas, prev.tas)
		}
	}
	return gs, ias, tas
}

// confirmSpeeds replaces each aircraft's reported speeds with the confirmed
// ones (0 when not confirmed yet) and remembers this poll's readings.
func (pg *postgres) confirmSpeeds(nowEpoch float64, aircrafts []Aircraft) {
	if pg.speedSamples == nil {
		pg.speedSamples = make(map[string]speedSample)
	}
	for i := range aircrafts {
		a := &aircrafts[i]
		cur := newSpeedSample(nowEpoch, *a)
		prev, had := pg.speedSamples[a.Hex]
		a.Gs, a.Ias, a.Tas = 0, 0, 0
		if had {
			a.Gs, a.Ias, a.Tas = confirmedSpeeds(prev, cur)
		}
		pg.speedSamples[a.Hex] = cur
	}
}

func (pg *postgres) sweepSpeedSamples(now time.Time) {
	for hex, s := range pg.speedSamples {
		if now.Sub(s.seenAt) > speedSampleExpiry {
			delete(pg.speedSamples, hex)
		}
	}
}
