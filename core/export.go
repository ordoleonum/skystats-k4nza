package main

// Data export.
//
//   GET /api/export/summary                 -> JSON: what can be exported (row counts, date range, DB size)
//   GET /api/export/csv/:dataset?from&to&tz -> one dataset as CSV (opens in Excel / Google Sheets)
//   GET /api/export/all.zip?from&to&tz      -> every dataset as CSV files in one ZIP
//
// Data is streamed straight from PostgreSQL (COPY ... TO STDOUT), so even a
// multi-year database is exported without loading it into memory.
// from / to are optional dates (YYYY-MM-DD, inclusive) in the timezone tz.

import (
	"archive/zip"
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/rs/zerolog/log"
)

type exportDataset struct {
	Key         string `json:"key"`
	Label       string `json:"label"`
	Description string `json:"description"`
	// SQL select; "%s" is replaced by a WHERE clause on dateColumn (or TRUE)
	query      string
	dateColumn string
	countQuery string
}

var exportDatasets = []exportDataset{
	{
		Key:         "flights",
		Label:       "Flights (easy to read)",
		Description: "One row per aircraft visit: times, callsign, registration, type, operator, route, max altitude/speed and distance.",
		dateColumn:  "a.first_seen",
		query: `SELECT
				a.first_seen, a.last_seen, a.hex AS icao_hex, a.flight AS callsign,
				COALESCE(NULLIF(a.r, ''), rd.registration) AS registration,
				COALESCE(NULLIF(a.t, ''), rd.icao_type) AS aircraft_type,
				rd.manufacturer, rd.type AS model, rd.registered_owner AS owner,
				rd.registered_owner_country_name AS owner_country,
				ro.airline_name, ro.origin_iata_code AS origin, ro.origin_name,
				ro.destination_iata_code AS destination, ro.destination_name,
				a.alt_baro AS max_altitude_ft, a.gs AS max_ground_speed_kt,
				a.last_seen_distance AS last_distance_km, a.squawk, a.rssi
			FROM aircraft_data a
			LEFT JOIN registration_data rd ON rd.mode_s = a.hex
			LEFT JOIN route_data ro ON ro.route_callsign = a.flight
			WHERE %s
			ORDER BY a.first_seen`,
		countQuery: "SELECT COUNT(*) FROM aircraft_data",
	},
	{
		Key:         "flights-raw",
		Label:       "Flights (all columns)",
		Description: "Every column SkyStats stores for each visit, exactly as in the database.",
		dateColumn:  "first_seen",
		query:       `SELECT * FROM aircraft_data WHERE %s ORDER BY first_seen`,
		countQuery:  "SELECT COUNT(*) FROM aircraft_data",
	},
	{
		Key:         "interesting",
		Label:       "Interesting aircraft sightings",
		Description: "Military, government, police and other notable aircraft from plane-alert-db.",
		dateColumn:  "seen",
		query:       `SELECT * FROM interesting_aircraft_seen WHERE %s ORDER BY seen`,
		countQuery:  "SELECT COUNT(*) FROM interesting_aircraft_seen",
	},
	{
		Key:         "records",
		Label:       "Record holders",
		Description: "Fastest, slowest, highest and lowest aircraft.",
		dateColumn:  "first_seen",
		query: `SELECT * FROM (
				SELECT 'fastest' AS record, hex, flight, registration, type, first_seen, last_seen,
				       ground_speed, indicated_air_speed, true_air_speed, NULL::int AS barometric_altitude, NULL::int AS geometric_altitude
				FROM fastest_aircraft
				UNION ALL
				SELECT 'slowest', hex, flight, registration, type, first_seen, last_seen,
				       ground_speed, indicated_air_speed, true_air_speed, NULL, NULL
				FROM slowest_aircraft
				UNION ALL
				SELECT 'highest', hex, flight, registration, type, first_seen, last_seen,
				       NULL, NULL, NULL, barometric_altitude, geometric_altitude
				FROM highest_aircraft
				UNION ALL
				SELECT 'lowest', hex, flight, registration, type, first_seen, last_seen,
				       NULL, NULL, NULL, barometric_altitude, geometric_altitude
				FROM lowest_aircraft
			) r WHERE %s ORDER BY record, first_seen`,
		countQuery: `SELECT (SELECT COUNT(*) FROM fastest_aircraft) + (SELECT COUNT(*) FROM slowest_aircraft) +
		             (SELECT COUNT(*) FROM highest_aircraft) + (SELECT COUNT(*) FROM lowest_aircraft)`,
	},
	{
		Key:         "registrations",
		Label:       "Aircraft registrations",
		Description: "Registration, manufacturer, model and owner for every aircraft looked up.",
		query:       `SELECT * FROM registration_data WHERE %s ORDER BY registration`,
		countQuery:  "SELECT COUNT(*) FROM registration_data",
	},
	{
		Key:         "routes",
		Label:       "Routes",
		Description: "Origin and destination airports for every callsign looked up.",
		query:       `SELECT * FROM route_data WHERE %s ORDER BY route_callsign`,
		countQuery:  "SELECT COUNT(*) FROM route_data",
	},
}

func findExportDataset(key string) *exportDataset {
	for i := range exportDatasets {
		if exportDatasets[i].Key == key {
			return &exportDatasets[i]
		}
	}
	return nil
}

// exportRange parses from/to (YYYY-MM-DD, inclusive) in timezone tz.
// Values are validated by time.Parse, then formatted by us, so the resulting
// SQL literals cannot contain user-supplied text.
type exportRange struct {
	from, to *time.Time // to is exclusive (day after the chosen end date)
	label    string
}

func parseExportRange(c *gin.Context, tz string) (exportRange, error) {
	loc, err := time.LoadLocation(tz)
	if err != nil {
		loc = time.UTC
	}
	var r exportRange
	parts := []string{}
	if v := c.Query("from"); v != "" {
		t, err := time.ParseInLocation("2006-01-02", v, loc)
		if err != nil {
			return r, fmt.Errorf("invalid 'from' date, use YYYY-MM-DD")
		}
		r.from = &t
		parts = append(parts, v)
	} else {
		parts = append(parts, "start")
	}
	if v := c.Query("to"); v != "" {
		t, err := time.ParseInLocation("2006-01-02", v, loc)
		if err != nil {
			return r, fmt.Errorf("invalid 'to' date, use YYYY-MM-DD")
		}
		t = t.AddDate(0, 0, 1)
		r.to = &t
		parts = append(parts, v)
	} else {
		parts = append(parts, "now")
	}
	r.label = strings.Join(parts, "_to_")
	return r, nil
}

func (r exportRange) whereClause(column string) string {
	if column == "" {
		return "TRUE"
	}
	conds := []string{}
	if r.from != nil {
		conds = append(conds, fmt.Sprintf("%s >= '%s'::timestamptz", column, r.from.UTC().Format(time.RFC3339)))
	}
	if r.to != nil {
		conds = append(conds, fmt.Sprintf("%s < '%s'::timestamptz", column, r.to.UTC().Format(time.RFC3339)))
	}
	if len(conds) == 0 {
		return "TRUE"
	}
	return strings.Join(conds, " AND ")
}

// copyCSV streams one dataset as CSV (with header row) into w.
func (s *APIServer) copyCSV(ctx context.Context, w io.Writer, ds *exportDataset, r exportRange) error {
	conn, err := s.pg.db.Acquire(ctx)
	if err != nil {
		return err
	}
	defer conn.Release()

	sql := fmt.Sprintf("COPY ("+ds.query+") TO STDOUT WITH (FORMAT csv, HEADER true)", r.whereClause(ds.dateColumn))
	_, err = conn.Conn().PgConn().CopyTo(ctx, w, sql)
	return err
}

func (s *APIServer) exportCSV(c *gin.Context) {
	ds := findExportDataset(c.Param("dataset"))
	if ds == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "unknown dataset"})
		return
	}
	r, err := parseExportRange(c, s.getTimezone(c))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	filename := fmt.Sprintf("skystats-%s-%s.csv", ds.Key, r.label)
	c.Header("Content-Type", "text/csv; charset=utf-8")
	c.Header("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, filename))
	c.Header("Cache-Control", "no-store")
	c.Status(http.StatusOK)

	ctx, cancel := context.WithTimeout(c.Request.Context(), 30*time.Minute)
	defer cancel()
	if err := s.copyCSV(ctx, c.Writer, ds, r); err != nil {
		log.Error().Err(err).Msgf("export of %s failed", ds.Key)
	}
}

func (s *APIServer) exportAllZip(c *gin.Context) {
	r, err := parseExportRange(c, s.getTimezone(c))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	filename := fmt.Sprintf("skystats-export-%s.zip", r.label)
	c.Header("Content-Type", "application/zip")
	c.Header("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, filename))
	c.Header("Cache-Control", "no-store")
	c.Status(http.StatusOK)

	ctx, cancel := context.WithTimeout(c.Request.Context(), 60*time.Minute)
	defer cancel()

	zw := zip.NewWriter(c.Writer)
	defer zw.Close()

	readme, _ := zw.Create("README.txt")
	fmt.Fprintf(readme, "SkyStats export created %s\r\nDate range: %s\r\n\r\n", time.Now().Format(time.RFC1123), r.label)
	for _, ds := range exportDatasets {
		fmt.Fprintf(readme, "%s.csv - %s: %s\r\n", ds.Key, ds.Label, ds.Description)
	}

	for i := range exportDatasets {
		ds := &exportDatasets[i]
		w, err := zw.CreateHeader(&zip.FileHeader{Name: ds.Key + ".csv", Method: zip.Deflate, Modified: time.Now()})
		if err != nil {
			log.Error().Err(err).Msg("zip export: cannot create entry")
			return
		}
		if err := s.copyCSV(ctx, w, ds, r); err != nil {
			log.Error().Err(err).Msgf("zip export of %s failed", ds.Key)
			return
		}
	}
}

func (s *APIServer) exportSummary(c *gin.Context) {
	ctx, cancel := context.WithTimeout(c.Request.Context(), 30*time.Second)
	defer cancel()

	type item struct {
		exportDataset
		Rows    int64 `json:"rows"`
		HasDate bool  `json:"has_date_filter"`
	}
	items := []item{}
	for _, ds := range exportDatasets {
		var n int64
		if err := s.pg.db.QueryRow(ctx, ds.countQuery).Scan(&n); err != nil {
			n = -1
		}
		items = append(items, item{exportDataset: ds, Rows: n, HasDate: ds.dateColumn != ""})
	}

	var dbSize string
	var oldest, newest *time.Time
	_ = s.pg.db.QueryRow(ctx, "SELECT pg_size_pretty(pg_database_size(current_database()))").Scan(&dbSize)
	_ = s.pg.db.QueryRow(ctx, "SELECT MIN(first_seen), MAX(first_seen) FROM aircraft_data").Scan(&oldest, &newest)

	c.JSON(http.StatusOK, gin.H{
		"datasets":       items,
		"database_size":  dbSize,
		"oldest":         oldest,
		"newest":         newest,
		"retention_days": getRetentionDays(),
	})
}
