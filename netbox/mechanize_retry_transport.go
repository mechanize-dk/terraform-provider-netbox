package netbox

// MECHANIZE FORK: Retry transport
// Wraps the HTTP transport to automatically retry transient NetBox failures.
//
// The failure this primarily guards against is an intermittent gateway error
// (nginx/gunicorn returning 502/503/504 with a text/html body while NetBox is
// under load). Crucially, that is a *successful* HTTP round-trip: RoundTrip
// returns (resp, nil) with a 5xx status. The error the user actually sees —
//
//	(*interface {}) is not supported by the TextConsumer ...
//
// is produced later, by the go-openapi client layer, when it selects the
// TextConsumer for the text/html body and fails to unmarshal it into the
// generated *Default response payload. That error never travels back through
// this transport, so retrying only on RoundTrip errors (the previous behaviour)
// could never fix it. We must inspect the *response status code* here and retry
// before the bad response ever reaches go-openapi.
//
// The transport is injected in client.go and is transparent to all resources —
// no per-resource changes are required.

import (
	"io"
	"net/http"
	"strings"
	"time"

	log "github.com/sirupsen/logrus"
)

const mechanizeRetryCount = 3

// mechanizeRetryWaitMs is the base backoff between attempts (linear: base*attempt).
// It is a var, not a const, so tests can set it to 0 to run fast.
var mechanizeRetryWaitMs = 500

// mechanizeRetryableStatus is the set of transient HTTP status codes worth
// retrying. These are gateway/overload conditions that typically resolve on a
// subsequent attempt. Deliberately excludes 500 (usually a genuine application
// error) and all 4xx (client errors that will not change on retry).
var mechanizeRetryableStatus = map[int]bool{
	http.StatusTooManyRequests:    true, // 429
	http.StatusBadGateway:         true, // 502
	http.StatusServiceUnavailable: true, // 503
	http.StatusGatewayTimeout:     true, // 504
}

// mechanizeRetryTransport retries requests that fail with transient errors or
// return a transient HTTP status code.
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
			// Rewind the request body for replay. If the body cannot be
			// rewound (a streaming payload with no GetBody), we must not retry
			// a partially-consumed body — return what we have.
			if r.Body != nil {
				if r.GetBody == nil {
					return resp, err
				}
				body, bodyErr := r.GetBody()
				if bodyErr != nil {
					return resp, err
				}
				r.Body = body
			}

			wait := time.Duration(mechanizeRetryWaitMs*attempt) * time.Millisecond
			log.WithFields(log.Fields{
				"attempt": attempt + 1,
				"wait_ms": wait.Milliseconds(),
				"url":     r.URL.String(),
			}).Debug("Retrying request after transient error")
			time.Sleep(wait)
		}

		resp, err = t.original.RoundTrip(r)

		if err != nil {
			if mechanizeIsTransientErr(err) {
				log.WithFields(log.Fields{
					"attempt": attempt + 1,
					"error":   err.Error(),
				}).Warn("Transient NetBox API transport error, will retry")
				continue
			}
			// Non-transient transport error — return immediately.
			return resp, err
		}

		// Successful round-trip. A transient gateway status (502/503/504/429)
		// is what produces the downstream TextConsumer error, so retry here
		// instead of handing the bad response to go-openapi.
		if resp != nil && mechanizeRetryableStatus[resp.StatusCode] {
			if attempt < mechanizeRetryCount-1 {
				log.WithFields(log.Fields{
					"attempt": attempt + 1,
					"status":  resp.StatusCode,
					"url":     r.URL.String(),
				}).Warn("Transient NetBox API status, will retry")
				mechanizeDrainAndClose(resp) // free the connection for reuse
				continue
			}
			// Retries exhausted: return the last response so go-openapi can
			// surface its normal error rather than us swallowing it.
			return resp, nil
		}

		return resp, nil
	}

	return resp, err
}

// mechanizeIsTransientErr reports whether a transport-level error is worth
// retrying (connection dropped, reset, or refused mid-flight).
func mechanizeIsTransientErr(err error) bool {
	errStr := err.Error()
	return strings.Contains(errStr, "EOF") ||
		strings.Contains(errStr, "connection reset") ||
		strings.Contains(errStr, "connection refused")
}

// mechanizeDrainAndClose consumes and closes a response body so the underlying
// connection can be reused instead of being discarded.
func mechanizeDrainAndClose(resp *http.Response) {
	if resp == nil || resp.Body == nil {
		return
	}
	_, _ = io.Copy(io.Discard, resp.Body)
	_ = resp.Body.Close()
}
