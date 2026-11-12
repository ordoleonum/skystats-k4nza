package main

// K4NZA: reception charts for the Activity tab.
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

// The all-time outline scans the whole table, and it is drawn on every
// range chart, so keep it in memory for a while.
var (
	rangeAllMu      sync.Mutex
	rangeAllCache   []float64
	rangeAllExpires time.Time
)

func (s *APIServer) queryRangeSectors(interval string) ([]float64, error) {
	entryFilter, exitFilter := "", ""
	if interval != "" {
		entryFilter = `first_seen >= now() - INTERVAL '` + interval + `' AND`
		exitFilter = `last_seen >= now() - INTERVAL '` + interval + `' AND`
	}

	query := `
		WITH pts AS (
			SELECT r_dst::float8 AS nm, r_dir::float8 AS dir
			FROM aircraft_data
			WHERE ` + entryFilter + ` r_dst > 0 AND r_dir IS NOT NULL
			UNION ALL
			SELECT last_seen_distance::float8 / $3 AS nm,
			       atan2d(
			           sind(last_seen_lon::float8 - $2) * cosd(last_seen_lat::float8),
			           cosd($1) * sind(last_seen_lat::float8)
			             - sind($1) * cosd(last_seen_lat::float8) * cosd(last_seen_lon::float8 - $2)
			       ) AS dir
			FROM aircraft_data
			WHERE ` + exitFilter + ` last_seen_distance > 0
			  AND last_seen_lat IS NOT NULL AND last_seen_lon IS NOT NULL
			  AND NOT (last_seen_lat = 0 AND last_seen_lon = 0)
		)
		SELECT floor(mod((dir + 360)::numeric, 360) / 10)::int AS sector, max(nm)
		FROM pts
		WHERE nm < $4
		GROUP BY 1`

	// Anything beyond the SkyStats radius is a bad position report.
	maxNm := getRadius() / kmPerNm
	if maxNm <= 0 {
		maxNm = 1000
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	rows, err := s.pg.db.Query(ctx, query, getLat(), getLon(), kmPerNm, maxNm)
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

func (s *APIServer) getRangeAllTime() ([]float64, error) {
	rangeAllMu.Lock()
	defer rangeAllMu.Unlock()
	if rangeAllCache != nil && time.Now().Before(rangeAllExpires) {
		return rangeAllCache, nil
	}
	sectors, err := s.queryRangeSectors("")
	if err != nil {
		return nil, err
	}
	rangeAllCache = sectors
	rangeAllExpires = time.Now().Add(15 * time.Minute)
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

	maxNm := getRadius() / kmPerNm
	if maxNm <= 0 {
		maxNm = 1000
	}

	base := `FROM aircraft_data
		WHERE first_seen >= now() - INTERVAL '` + interval + `'
		  AND r_dst > 0 AND r_dst < $1
		  AND rssi IS NOT NULL AND rssi < 0 AND rssi > -60`

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	rows, err := s.pg.db.Query(ctx,
		`SELECT r_dst::float8, rssi::float8 `+base+` ORDER BY random() LIMIT 2500`, maxNm)
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
		`SELECT floor(r_dst / 10)::int * 10 + 5 AS mid,
		        percentile_cont(0.5) WITHIN GROUP (ORDER BY rssi::float8),
		        count(*) `+base+`
		 GROUP BY 1 HAVING count(*) >= 3 ORDER BY 1`, maxNm)
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
	})
}
