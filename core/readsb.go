package main

import (
	"os"
)

// Fetch downloads readsb's aircraft.json. It uses a client with a 5 second
// timeout and always closes the response body (see httpclient.go).
func Fetch() ([]byte, error) {
	url := os.Getenv("READSB_AIRCRAFT_JSON")
	return httpGetBytes(localHTTPClient, url, nil)
}
