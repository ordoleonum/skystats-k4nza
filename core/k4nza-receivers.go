package main

// Radar Nodes: SkyStats per receiver.
//
// The feeder runs one extra SkyStats per receiver (this station's own radio
// and each node), each with its own database in the same PostgreSQL. This
// SkyStats (the whole network) lists them for the Activity menu; the page's
// requests for one receiver go to /r/<id>/api/..., which nginx passes on to
// that receiver's SkyStats (see rootfs/etc/nginx/nginx.conf).

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5"
	"github.com/rs/zerolog/log"
)

var dbNamePattern = regexp.MustCompile(`^[a-z0-9_]{1,63}$`)

// ensureDatabase makes this SkyStats' database when DB_CREATE=true and it
// doesn't exist yet (a receiver's SkyStats on its first start).
func ensureDatabase() {
	if os.Getenv("DB_CREATE") != "true" {
		return
	}
	name := os.Getenv("DB_NAME")
	if !dbNamePattern.MatchString(name) {
		log.Warn().Msgf("DB_CREATE: database name %q is not allowed; not creating it", name)
		return
	}
	url := "postgres://" + os.Getenv("DB_USER") + ":" + os.Getenv("DB_PASSWORD") + "@" +
		os.Getenv("DB_HOST") + ":" + os.Getenv("DB_PORT") + "/postgres?sslmode=disable"
	// PostgreSQL may still be starting
	for attempt := 0; attempt < 30; attempt++ {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		conn, err := pgx.Connect(ctx, url)
		if err == nil {
			var exists bool
			err = conn.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM pg_database WHERE datname = $1)`, name).Scan(&exists)
			if err == nil && !exists {
				// the name was checked above; CREATE DATABASE takes no parameters
				_, err = conn.Exec(ctx, `CREATE DATABASE "`+name+`"`)
				if err == nil {
					log.Info().Msgf("Created database %s", name)
				}
			}
			conn.Close(ctx)
			cancel()
			if err == nil {
				return
			}
			log.Warn().Err(err).Msg("DB_CREATE: could not check or create the database")
			return
		}
		cancel()
		time.Sleep(2 * time.Second)
	}
	log.Warn().Msg("DB_CREATE: PostgreSQL did not answer; continuing")
}

// poolMaxParam limits the connections of a receiver's SkyStats (DB_POOL_MAX),
// so many receivers fit within PostgreSQL's connection limit.
func poolMaxParam() string {
	n := os.Getenv("DB_POOL_MAX")
	if n == "" || strings.Trim(n, "0123456789") != "" {
		return ""
	}
	return "pool_max_conns=" + n
}

type receiver struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Paused bool   `json:"paused,omitempty"`
}

// getReceivers lists the receivers for the Activity menu (empty without nodes).
func (s *APIServer) getReceivers(c *gin.Context) {
	empty := gin.H{"receivers": []receiver{}}
	base := strings.TrimRight(os.Getenv("FEEDER_URL"), "/")
	if base == "" {
		c.JSON(http.StatusOK, empty)
		return
	}
	body, err := httpGetBytes(localHTTPClient, base+"/api/k4nza/receivers", nil)
	if err != nil {
		c.JSON(http.StatusOK, empty)
		return
	}
	var out struct {
		Receivers []receiver `json:"receivers"`
	}
	if json.Unmarshal(body, &out) != nil || out.Receivers == nil {
		c.JSON(http.StatusOK, empty)
		return
	}
	c.JSON(http.StatusOK, gin.H{"receivers": out.Receivers})
}
