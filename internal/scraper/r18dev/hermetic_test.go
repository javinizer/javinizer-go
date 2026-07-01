package r18dev

import (
	"errors"
	"net/http"
	"sync/atomic"
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
