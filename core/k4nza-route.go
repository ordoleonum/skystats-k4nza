package main

// K4NZA: GET /api/route/:callsign returns the route SkyStats looked up for a
// callsign (adsb.im routeset, cached up to 18 hours), with the airport
// positions so the live map can check it against where the aircraft is.

import (
	"context"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

var callsignPattern = regexp.MustCompile(`^[A-Z0-9]{2,8}$`)

type routeAirport struct {
	IATA string   `json:"iata"`
	ICAO string   `json:"icao"`
	Name string   `json:"name"`
	City string   `json:"city"`
	Lat  *float64 `json:"lat"`
	Lon  *float64 `json:"lon"`
}

func (s *APIServer) routeForCallsign(c *gin.Context) {
	cs := strings.ToUpper(strings.TrimSpace(c.Param("callsign")))
	if !callsignPattern.MatchString(cs) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "not a callsign"})
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	var o, d routeAirport
	var updated *time.Time
	err := s.pg.db.QueryRow(ctx, `
		SELECT COALESCE(origin_iata_code, ''), COALESCE(origin_icao_code, ''), COALESCE(origin_name, ''),
		       COALESCE(origin_municipality, ''), origin_latitude::float8, origin_longitude::float8,
		       COALESCE(destination_iata_code, ''), COALESCE(destination_icao_code, ''), COALESCE(destination_name, ''),
		       COALESCE(destination_municipality, ''), destination_latitude::float8, destination_longitude::float8,
		       last_updated
		FROM route_data
		WHERE upper(trim(route_callsign)) = $1
		  AND (origin_icao_code IS NOT NULL OR origin_iata_code IS NOT NULL)
		ORDER BY last_updated DESC NULLS LAST
		LIMIT 1`, cs).Scan(&o.IATA, &o.ICAO, &o.Name, &o.City, &o.Lat, &o.Lon,
		&d.IATA, &d.ICAO, &d.Name, &d.City, &d.Lat, &d.Lon, &updated)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"callsign": cs, "error": "no route known"})
		return
	}
	c.Header("Cache-Control", "max-age=60")
	c.JSON(http.StatusOK, gin.H{"callsign": cs, "origin": o, "destination": d, "updated": updated})
}
