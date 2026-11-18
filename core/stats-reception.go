package main

// Reception charts for the Activity tab.
//
//   GET /api/stats/reception/range/:period   (day | week | month | all)
//   GET /api/stats/reception/signal/:period  (day | week | month)
//
// Both use data SkyStats already stores, so they work on existing history:
//   - r_dst / r_dir / rssi are written once, when an aircraft is first heard.
//     That is usually at the edge of coverage, which is what a range chart
//     wants. readsb reports r_dst in nautical miles.
//   - last_seen_lat/lon/distance are updated while the aircraft is tracked, so
//     the last point is where it was lost: the other edge of coverage.
//     last_seen_distance is stored in km.
// Distances are returned in nautical miles. The station position is never
// returned, only distances and bearings from it.

import (
	"context"
	"math"
	"net/http"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
)

const rangeSectors = 36 // 10 degree slices
const kmPerNm = 1.852

func receptionInterval(period string) (string, bool) {
	switch period {
	case "day":
		return "1 day", true
	case "week":
		return "7 days", true
	case "month":
		return "1 month", true
	case "all":
		return "", true
	}
	return "", false
}

// The all-time outline is drawn on every range chart; it comes from the daily
// summary table plus the not-yet-summarised days, kept in memory briefly.
var (
	rangeAllMu      sync.Mutex
	rangeAllCache   []float64
	rangeAllExpires time.Time
)

// receptionSince turns a period into the start time of the data shown.
func receptionSince(interval string) time.Time {
	now := time.Now()
	switch interval {
	case "1 day":
		return now.Add(-24 * time.Hour)
	case "7 days":
		return now.AddDate(0, 0, -7)
	case "1 month":
		return now.AddDate(0, -1, 0)
	}
	return time.Time{}
}

// queryRangeSectorsBetween: farthest distance (nm) per sector for visits that
// started in [from, to).
func queryRangeSectorsBetween(ctx context.Context, pg *postgres, from, to time.Time) ([]float64, error) {
	rows, err := pg.db.Query(ctx, rangeSectorsSQL, getLat(), getLon(), kmPerNm, maxPlausibleNm(), from, to)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	sectors := make([]float64, rangeSectors)
	for rows.Next() {
		var sector int
		var nm float64
		if err := rows.Scan(&sector, &nm); err != nil {
			return nil, err
		}
		if sector >= 0 && sector < rangeSectors {
			sectors[sector] = math.Round(nm*10) / 10
		}
	}
	return sectors, rows.Err()
}

func (s *APIServer) queryRangeSectors(interval string) ([]float64, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	return queryRangeSectorsBetween(ctx, s.pg, receptionSince(interval), time.Now().Add(24*time.Hour))
}

func (s *APIServer) getRangeAllTime() ([]float64, error) {
	rangeAllMu.Lock()
	defer rangeAllMu.Unlock()
	if rangeAllCache != nil && time.Now().Before(rangeAllExpires) {
		return rangeAllCache, nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	sectors, err := allTimeRangeSectors(ctx, s.pg)
	if err != nil {
		return nil, err
	}
	rangeAllCache = sectors
	rangeAllExpires = time.Now().Add(5 * time.Minute)
	return sectors, nil
}

func (s *APIServer) getReceptionRange(c *gin.Context) {
	period := c.Param("period")
	interval, ok := receptionInterval(period)
	if !ok {
		c.JSON(http.StatusBadRequest, gin.H{"error": "period must be day, week, month or all"})
		return
	}

	allTime, err := s.getRangeAllTime()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	current := allTime
	if interval != "" {
		current, err = s.queryRangeSectors(interval)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
	}

	maxNm := 0.0
	for _, v := range current {
		maxNm = math.Max(maxNm, v)
	}

	c.JSON(http.StatusOK, gin.H{
		"sector_degrees": 360 / rangeSectors,
		"period":         current,
		"all_time":       allTime,
		"max_nm":         maxNm,
	})
}

// Signal vs distance: a sample of individual aircraft (first-heard distance
// and signal, measured at the same moment) plus the median signal for every
// 10 nm, so the trend is readable even when the dots overlap.
func (s *APIServer) getReceptionSignal(c *gin.Context) {
	period := c.Param("period")
	interval, ok := receptionInterval(period)
	if !ok || interval == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "period must be day, week or month"})
		return
	}

	maxNm := maxPlausibleNm()

	since := receptionSince(interval)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// K4NZA: the signal at each visit's closest approach (rssi_at_min), which
	// falls with distance and shows gain and antenna quality. Visits recorded
	// before that existed only have the signal from when the aircraft was
	// first heard, which is nearly always the weakest the receiver can decode
	// (a flat line at the detection floor); those are used only while there
	// isn't enough closest-approach data yet.
	mode := "closest"
	dist, sig := "(min_distance / 1.852)", "rssi_at_min"
	base := `FROM aircraft_data
		WHERE first_seen >= $2
		  AND min_distance > 0 AND min_distance / 1.852 < $1
		  AND rssi_at_min IS NOT NULL AND rssi_at_min < 0 AND rssi_at_min > -49`
	var total int64
	if err := s.pg.db.QueryRow(ctx, `SELECT COUNT(*) `+base, maxNm, since).Scan(&total); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	closestCount := total
	if total < signalMinClosest {
		mode = "first_heard"
		dist, sig = "r_dst", "rssi"
		base = `FROM aircraft_data
		WHERE first_seen >= $2
		  AND r_dst > 0 AND r_dst < $1
		  AND rssi IS NOT NULL AND rssi < 0 AND rssi > -60`
		if err := s.pg.db.QueryRow(ctx, `SELECT COUNT(*) `+base, maxNm, since).Scan(&total); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
	}

	// An even sample of about signalSamplePoints visits: every n-th row by id.
	// (ORDER BY random() would have to sort every row in the period first.)
	every := sampleEvery(total, signalSamplePoints)

	rows, err := s.pg.db.Query(ctx,
		`SELECT `+dist+`::float8, `+sig+`::float8 `+base+` AND id % $3 = 0 LIMIT $4`, maxNm, since, every, signalSamplePoints)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	points := [][2]float64{}
	for rows.Next() {
		var d, r float64
		if err := rows.Scan(&d, &r); err != nil {
			rows.Close()
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		points = append(points, [2]float64{math.Round(d*10) / 10, math.Round(r*10) / 10})
	}
	rows.Close()

	rows, err = s.pg.db.Query(ctx,
		`SELECT floor(`+dist+` / 10)::int * 10 + 5 AS mid,
		        percentile_cont(0.5) WITHIN GROUP (ORDER BY `+sig+`::float8),
		        count(*) `+base+`
		 GROUP BY 1 HAVING count(*) >= 3 ORDER BY 1`, maxNm, since)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	defer rows.Close()
	median := [][2]float64{}
	for rows.Next() {
		var mid int
		var med float64
		var n int
		if err := rows.Scan(&mid, &med, &n); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		median = append(median, [2]float64{float64(mid), math.Round(med*10) / 10})
	}

	c.JSON(http.StatusOK, gin.H{
		"points": points,
		"median": median,
		"mode":   mode,
		// closest-approach readings in this period, and how many the chart waits for
		"closest_count":  closestCount,
		"closest_needed": signalMinClosest,
	})
}

const signalSamplePoints = 2500

// signalMinClosest: closest-approach readings needed before the chart uses them
const signalMinClosest = 50

// sampleEvery returns n so that taking every n-th row of total rows gives
// about want rows.
func sampleEvery(total int64, want int64) int64 {
	if want <= 0 || total <= want {
		return 1
	}
	return (total + want - 1) / want
}
