package data

// K4NZA: readable names for ICAO aircraft type codes ("B738" -> "Boeing
// 737-800"). icao_types.csv is tar1090's type list (icao_aircraft_types2.js,
// the one the live map uses), with the ALL CAPS maker names tidied up.

import (
	"bytes"
	_ "embed"
	"encoding/csv"
	"strings"
	"sync"
)

//go:embed icao_types.csv
var icaoTypesCSV []byte

var (
	typesOnce  sync.Once
	typesIndex map[string]string
)

func loadTypes() {
	typesIndex = make(map[string]string)
	records, err := csv.NewReader(bytes.NewReader(icaoTypesCSV)).ReadAll()
	if err != nil {
		return
	}
	for i, row := range records {
		if i == 0 || len(row) < 2 {
			continue // header
		}
		code := strings.ToUpper(strings.TrimSpace(row[0]))
		if name := strings.TrimSpace(row[1]); code != "" && name != "" {
			typesIndex[code] = name
		}
	}
}

// LookupTypeName returns the name of an ICAO type code, if it's in the list.
func LookupTypeName(code string) (string, bool) {
	code = strings.ToUpper(strings.TrimSpace(code))
	if code == "" {
		return "", false
	}
	typesOnce.Do(loadTypes)
	name, ok := typesIndex[code]
	return name, ok
}
