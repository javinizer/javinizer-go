package server

import (
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
)

type serverRoundTripFunc func(*http.Request) (*http.Response, error)

func (f serverRoundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

// mockVersionRelease exercises the real version-check handler and release
// parsing while keeping its hardcoded GitHub endpoint entirely in memory.
// Callers must not run in parallel while replacing the default transport.
func mockVersionRelease(t *testing.T) {
	t.Helper()
	t.Setenv("JAVINIZER_DATA_DIR", t.TempDir())
	original := http.DefaultTransport
	var requests atomic.Int64
	t.Cleanup(func() {
		http.DefaultTransport = original
		assert.Equal(t, int64(1), requests.Load(), "version check must fetch the synthetic release")
	})
	http.DefaultTransport = serverRoundTripFunc(func(r *http.Request) (*http.Response, error) {
		requests.Add(1)
		if r.Method != http.MethodGet || r.URL.String() != "https://api.github.com/repos/javinizer/javinizer-go/releases/latest" {
			t.Errorf("unexpected release request: %s %s", r.Method, r.URL)
			return nil, fmt.Errorf("unexpected release request: %s %s", r.Method, r.URL)
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body:       io.NopCloser(strings.NewReader(`{"tag_name":"v99.0.0","prerelease":false,"published_at":"2026-01-01T00:00:00Z"}`)),
			Request:    r,
		}, nil
	})
}
