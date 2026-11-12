package main

// Every outbound HTTP request goes through a client with a hard timeout, so
// one hung request to readsb, adsbdb or GitHub can't freeze recording.

import (
	"fmt"
	"io"
	"net/http"
	"time"
)

var (
	// short timeout for the local readsb feed, polled every few seconds
	localHTTPClient = &http.Client{Timeout: 5 * time.Second}
	// longer timeout for internet APIs (adsbdb, GitHub, plane-alert-db CSV)
	remoteHTTPClient = &http.Client{Timeout: 30 * time.Second}
)

// maximum response size we are willing to read into memory (64 MiB)
const maxHTTPBodyBytes = 64 << 20

// HTTPStatusError is returned when a server answers with a non-2xx status.
type HTTPStatusError struct {
	URL        string
	StatusCode int
	Status     string
}

func (e *HTTPStatusError) Error() string {
	return fmt.Sprintf("GET %s returned %s", e.URL, e.Status)
}

// httpGetBytes performs a GET with the given client, always closes the body,
// limits the amount read and turns non-2xx responses into an HTTPStatusError
// (the body is still returned so callers can inspect it).
func httpGetBytes(client *http.Client, url string, headers map[string]string) ([]byte, error) {
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", fmt.Sprintf("Skystats/%s (K4NZA)", version))
	for k, v := range headers {
		req.Header.Set(k, v)
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxHTTPBodyBytes))
	if err != nil {
		return nil, err
	}

	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return body, &HTTPStatusError{URL: url, StatusCode: resp.StatusCode, Status: resp.Status}
	}

	return body, nil
}
