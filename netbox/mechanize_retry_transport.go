package netbox

// MECHANIZE FORK: Retry transport
// Wraps the HTTP transport to automatically retry transient errors such as
// "TextConsumer is not supported", which occur when NetBox briefly returns a
// non-JSON response (e.g. a 502/503 from nginx under load).
//
// The transport is injected in client.go and is completely transparent to all
// resources — no per-resource changes are required.

import (
	"net/http"
	"strings"
	"time"

	log "github.com/sirupsen/logrus"
)

const (
	mechanizeRetryCount   = 3
	mechanizeRetryWaitMs  = 500
)

// mechanizeRetryTransport retries requests that fail with transient errors.
type mechanizeRetryTransport struct {
	original http.RoundTripper
}

func (t mechanizeRetryTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	var (
		resp *http.Response
		err  error
	)

	for attempt := 0; attempt < mechanizeRetryCount; attempt++ {
		if attempt > 0 {
			wait := time.Duration(mechanizeRetryWaitMs*attempt) * time.Millisecond
			log.WithFields(log.Fields{
				"attempt": attempt + 1,
				"wait_ms": wait.Milliseconds(),
				"url":     r.URL.String(),
			}).Debug("Retrying request after transient error")
			time.Sleep(wait)
		}

		resp, err = t.original.RoundTrip(r)

		if err == nil {
			return resp, nil
		}

		// Retry on the go-openapi TextConsumer error and other transient errors
		errStr := err.Error()
		if strings.Contains(errStr, "TextConsumer") ||
			strings.Contains(errStr, "is not supported") ||
			strings.Contains(errStr, "EOF") ||
			strings.Contains(errStr, "connection reset") ||
			strings.Contains(errStr, "connection refused") {
			log.WithFields(log.Fields{
				"attempt": attempt + 1,
				"error":   errStr,
			}).Warn("Transient NetBox API error, will retry")
			continue
		}

		// Non-transient error — return immediately
		return resp, err
	}

	return resp, err
}
