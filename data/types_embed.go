package data

// K4NZA: readable names for ICAO aircraft type codes ("B738" -> "Boeing
// 737-800"). icao_types.csv is tar1090's type list (icao_aircraft_types2.js,
// the one the live map uses), with the ALL CAPS maker names tidied up.

import (
	"bytes"
	_ "embed"
	"encoding/csv"
	"sort"
	"strings"
	"sync"
)

//go:embed icao_types.csv
var icaoTypesCSV []byte

var (
	typesOnce  sync.Once
	typesIndex map[string]string
	typesKeys  map[string]string // code -> name in upper-case letters and digits only
	typesCodes []string          // all codes, sorted
)

func searchKey(s string) string {
	return strings.Map(func(r rune) rune {
		if r >= 'a' && r <= 'z' {
			r -= 'a' - 'A'
		}
		if (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') {
			return r
		}
		return -1
	}, s)
}

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
	typesKeys = make(map[string]string, len(typesIndex))
	for code, name := range typesIndex {
		typesKeys[code] = searchKey(name)
		typesCodes = append(typesCodes, code)
	}
	sort.Strings(typesCodes)
}

// MatchTypeCodes: the ICAO type codes a search for key (upper-case letters and
// digits, as typed without spaces or dashes) should find.
//   - a code itself ("K35R", "C17"): just that code
//   - the start of a code ("K35"): K35E, K35R
//   - part of a type's name ("KC135R", "STRATOTANKER", "BLACKHAWK", "737"):
//     every type whose name has it (4+ characters, or 3 digits like "737")
// At most limit codes; the key itself is always included.
func MatchTypeCodes(key string, limit int) []string {
	key = searchKey(key)
	if key == "" {
		return nil
	}
	typesOnce.Do(loadTypes)
	out := []string{key}
	if _, ok := typesIndex[key]; ok || len(key) < 3 {
		return out
	}
	byName := len(key) >= 4 || strings.Trim(key, "0123456789") == ""
	for _, code := range typesCodes {
		if len(out) >= limit {
			break
		}
		if strings.HasPrefix(code, key) || (byName && strings.Contains(typesKeys[code], key)) {
			out = append(out, code)
		}
	}
	return out
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
