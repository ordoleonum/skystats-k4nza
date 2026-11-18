package main

import (
	"context"
	"sort"

	"github.com/rs/zerolog/log"
)

// MarkProcessed sets a *_processed flag on the given rows.
//
// K4NZA: one statement that locks the rows in id order (as the live aircraft
// update does), instead of up to 1000 single-row updates in one transaction
// in whatever order the job had them. The old way deadlocked with the live
// update about 40 times a day, which threw away both batches.
func MarkProcessed(pg *postgres, colName string, aircrafts []Aircraft) {
	if len(aircrafts) == 0 {
		return
	}
	ids := make([]int, 0, len(aircrafts))
	for _, aircraft := range aircrafts {
		ids = append(ids, aircraft.Id)
	}
	sort.Ints(ids)

	_, err := pg.db.Exec(context.Background(), `
		UPDATE aircraft_data a SET `+colName+` = true
		FROM (SELECT id FROM aircraft_data WHERE id = ANY($1) ORDER BY id FOR UPDATE) l
		WHERE a.id = l.id`, ids)
	if err != nil {
		log.Error().Err(err).Msg("MarkProcessed() - Unable to update data")
	}
}

func DeleteExcessRows(pg *postgres, tableName string, metricName string, sortOrder string, maxRows int) {

	queryCount := `SELECT COUNT(*) FROM ` + tableName

	var rowCount int
	err := pg.db.QueryRow(context.Background(), queryCount).Scan(&rowCount)
	if err != nil {
		log.Error().Err(err).Msg("DeleteExcessRows() - Error querying db in DeleteExcessRows()")
		return
	}

	if rowCount > maxRows {

		excessRows := rowCount - maxRows

		if excessRows <= 0 {
			log.Debug().Msgf("DeleteExcessRows() - No excess rows in %s", tableName)
			return
		}

		deleteStatement := `DELETE FROM ` + tableName + `
							WHERE id IN (
								SELECT id
								FROM ` + tableName + `
								ORDER BY ` + metricName + ` ` + sortOrder + ` , first_seen ASC
								LIMIT $1
								)`

		_, err := pg.db.Exec(context.Background(), deleteStatement, excessRows)
		if err != nil {
			log.Error().Err(err).Msgf("DeleteExcessRows() - Failed to delete excess rows in %s", tableName)
		}
	}
}
