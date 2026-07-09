package netbox

// MECHANIZE FORK: tests for the retry transport.

import (
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func init() {
	// Keep the retry tests fast — no real backoff sleeps.
	mechanizeRetryWaitMs = 0
}

// fakeRoundTripper returns queued responses/errors in order, recording how many
// times it was called and the request body it saw on each call.
type fakeRoundTripper struct {
	responses  []*http.Response
	errs       []error
	calls      int
	seenBodies []string
}

func (f *fakeRoundTripper) RoundTrip(r *http.Request) (*http.Response, error) {
	idx := f.calls
	f.calls++

	body := ""
	if r.Body != nil {
		b, _ := io.ReadAll(r.Body)
		body = string(b)
	}
	f.seenBodies = append(f.seenBodies, body)

	var err error
	if idx < len(f.errs) {
		err = f.errs[idx]
	}
	var resp *http.Response
	if idx < len(f.responses) {
		resp = f.responses[idx]
	}
	return resp, err
}

func mkResp(status int, contentType, body string) *http.Response {
	h := http.Header{}
	if contentType != "" {
		h.Set("Content-Type", contentType)
	}
	return &http.Response{
		StatusCode: status,
		Header:     h,
		Body:       io.NopCloser(strings.NewReader(body)),
	}
}

// Root-cause reproduction: an nginx 502/503 with a text/html body is a
// SUCCESSFUL RoundTrip (err == nil). go-openapi later fails to parse it with the
// TextConsumer. The transport must retry on the status code, not just on errors.
func TestMechanizeRetryTransport_RetriesOnTransient5xx(t *testing.T) {
	f := &fakeRoundTripper{
		responses: []*http.Response{
			mkResp(503, "text/html", "<html>503 Service Unavailable</html>"),
			mkResp(502, "text/html", "<html>502 Bad Gateway</html>"),
			mkResp(200, "application/json", `{"id":1}`),
		},
	}
	tr := mechanizeRetryTransport{original: f}

	resp, err := tr.RoundTrip(mustGetRequest(t, http.MethodGet, "http://example/api", ""))

	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.Equal(t, 200, resp.StatusCode)
	assert.Equal(t, 3, f.calls, "should have retried twice before the 200")
}

// After exhausting retries the transport must return the last (still-5xx)
// response so go-openapi can produce its normal error path — never nil.
func TestMechanizeRetryTransport_ReturnsLastResponseWhenAllTransient(t *testing.T) {
	f := &fakeRoundTripper{
		responses: []*http.Response{
			mkResp(503, "text/html", "a"),
			mkResp(503, "text/html", "b"),
			mkResp(503, "text/html", "c"),
		},
	}
	tr := mechanizeRetryTransport{original: f}

	resp, err := tr.RoundTrip(mustGetRequest(t, http.MethodGet, "http://example/api", ""))

	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.Equal(t, 503, resp.StatusCode)
	assert.Equal(t, mechanizeRetryCount, f.calls)
}

// A POST body must be replayed intact on every retry, not sent empty.
func TestMechanizeRetryTransport_ReplaysBodyOnRetry(t *testing.T) {
	f := &fakeRoundTripper{
		responses: []*http.Response{
			mkResp(503, "text/html", "boom"),
			mkResp(201, "application/json", `{"id":7}`),
		},
	}
	tr := mechanizeRetryTransport{original: f}

	const payload = `{"name":"vlan","vid":1325}`
	resp, err := tr.RoundTrip(mustGetRequest(t, http.MethodPost, "http://example/api", payload))

	require.NoError(t, err)
	assert.Equal(t, 201, resp.StatusCode)
	require.Len(t, f.seenBodies, 2)
	assert.Equal(t, payload, f.seenBodies[0], "first attempt body")
	assert.Equal(t, payload, f.seenBodies[1], "retry must replay the same body, not send empty")
}

// Client errors (4xx) are not transient and must not be retried.
func TestMechanizeRetryTransport_NoRetryOn4xx(t *testing.T) {
	f := &fakeRoundTripper{
		responses: []*http.Response{mkResp(400, "application/json", `{"error":"bad"}`)},
	}
	tr := mechanizeRetryTransport{original: f}

	resp, err := tr.RoundTrip(mustGetRequest(t, http.MethodGet, "http://example/api", ""))

	require.NoError(t, err)
	assert.Equal(t, 400, resp.StatusCode)
	assert.Equal(t, 1, f.calls, "4xx must not be retried")
}

// A clean 2xx passes straight through with no retry.
func TestMechanizeRetryTransport_NoRetryOnSuccess(t *testing.T) {
	f := &fakeRoundTripper{
		responses: []*http.Response{mkResp(200, "application/json", `{}`)},
	}
	tr := mechanizeRetryTransport{original: f}

	resp, err := tr.RoundTrip(mustGetRequest(t, http.MethodGet, "http://example/api", ""))

	require.NoError(t, err)
	assert.Equal(t, 200, resp.StatusCode)
	assert.Equal(t, 1, f.calls)
}

// Transport-level transient errors (EOF, connection reset) are still retried.
func TestMechanizeRetryTransport_RetriesOnTransportError(t *testing.T) {
	f := &fakeRoundTripper{
		responses: []*http.Response{nil, mkResp(200, "application/json", `{}`)},
		errs:      []error{fmt.Errorf("read: connection reset by peer"), nil},
	}
	tr := mechanizeRetryTransport{original: f}

	resp, err := tr.RoundTrip(mustGetRequest(t, http.MethodGet, "http://example/api", ""))

	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.Equal(t, 200, resp.StatusCode)
	assert.Equal(t, 2, f.calls)
}

func mustGetRequest(t *testing.T, method, url, body string) *http.Request {
	t.Helper()
	var r *http.Request
	var err error
	if body == "" {
		r, err = http.NewRequest(method, url, nil)
	} else {
		r, err = http.NewRequest(method, url, strings.NewReader(body))
	}
	require.NoError(t, err)
	return r
}
