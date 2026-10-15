package r18dev

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
)

// errHTTPBlocked is returned by recordingTransport to short-circuit any
// outgoing HTTP request in tests. It lets tests assert that HTTP was (or was
// not) attempted without depending on a real network or a local server.
var errHTTPBlocked = errors.New("hermetic test: HTTP blocked")

// recordingTransport is an http.RoundTripper that counts every RoundTrip call
// and returns errHTTPBlocked. Tests inject it onto the scraper's resty client
// to PROVE the zero-HTTP claim: after a dump-hit Search, hits must be 0; after
// a dump-miss Search, hits must be > 0 (HTTP fallback was attempted). This
// replaces the previous real-network fallback tests, which flaked on r18.dev
// 429 rate-limiting and were non-hermetic.
type recordingTransport struct {
	hits int32 // accessed atomically
	err  error // returned on every request; defaults to errHTTPBlocked
}

func (r *recordingTransport) RoundTrip(*http.Request) (*http.Response, error) {
	atomic.AddInt32(&r.hits, 1)
	err := r.err
	if err == nil {
		err = errHTTPBlocked
	}
	return nil, err
}

// hits returns the number of RoundTrip calls observed.
func (r *recordingTransport) count() int {
	return int(atomic.LoadInt32(&r.hits))
}

// Parser fixtures have metadata but no reachable image assets. Return real
// missing-image responses for the expected CDN probes, rather than permitting
// transport errors or silently swallowing an unexpected API request.
func stubR18MetadataRequests(t *testing.T, s *scraper) {
	t.Helper()
	s.client.SetTransport(roundTripperFunc(func(r *http.Request) (*http.Response, error) {
		allowedHost := r.URL.Host == "pics.dmm.co.jp" || r.URL.Host == "awsimgsrc.dmm.com" || r.URL.Host == "awsimgsrc.dmm.co.jp" || r.URL.Host == "example.com"
		if !allowedHost || (r.Method != http.MethodGet && r.Method != http.MethodHead) || !strings.HasSuffix(r.URL.Path, ".jpg") {
			t.Errorf("unexpected metadata request: %s %s", r.Method, r.URL)
			return nil, fmt.Errorf("unexpected metadata request: %s %s", r.Method, r.URL)
		}
		return &http.Response{StatusCode: http.StatusNotFound, Header: make(http.Header), Body: io.NopCloser(strings.NewReader("")), Request: r}, nil
	}))
}

func stubR18EmptyIDRequests(t *testing.T, s *scraper) {
	t.Helper()
	calls := make(map[string]int)
	s.client.SetTransport(roundTripperFunc(func(r *http.Request) (*http.Response, error) {
		path := r.URL.Path
		if r.Method != http.MethodGet || r.URL.Host != "r18.dev" || (path != "/videos/vod/movies/detail/-/dvd_id=/json" && path != "/videos/vod/movies/detail/-/combined=/json") {
			t.Errorf("unexpected empty-ID request: %s %s", r.Method, r.URL)
			return nil, fmt.Errorf("unexpected empty-ID request: %s %s", r.Method, r.URL)
		}
		calls[path]++
		return &http.Response{StatusCode: http.StatusNotFound, Header: make(http.Header), Body: io.NopCloser(strings.NewReader("")), Request: r}, nil
	}))
	t.Cleanup(func() {
		for _, path := range []string{"/videos/vod/movies/detail/-/dvd_id=/json", "/videos/vod/movies/detail/-/combined=/json"} {
			if calls[path] != 1 {
				t.Errorf("%s called %d times, want 1", path, calls[path])
			}
		}
	})
}
