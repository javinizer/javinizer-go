package libredmm

import (
	"bytes"
	"fmt"
	"image"
	"image/png"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
)

// posterFixture generates image metadata locally; no CDN content is needed.
func posterFixture(t *testing.T, width, height int) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := png.Encode(&buf, image.NewGray(image.Rect(0, 0, width, height))); err != nil {
		t.Fatalf("encode poster fixture: %v", err)
	}
	return buf.Bytes()
}

// fixtureTransport routes only the named media URLs and this test server's own
// origin to local fixtures. An unexpected URL fails before DNS or dialing.
func fixtureTransport(t *testing.T, server *httptest.Server, mediaURLs ...string) http.RoundTripper {
	t.Helper()
	target, err := url.Parse(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	allowed := make(map[string]bool, len(mediaURLs))
	for _, mediaURL := range mediaURLs {
		allowed[mediaURL] = true
	}
	base := server.Client().Transport
	return roundTripperFunc(func(req *http.Request) (*http.Response, error) {
		if req.URL.Scheme == target.Scheme && req.URL.Host == target.Host {
			return base.RoundTrip(req)
		}
		if !allowed[req.URL.String()] {
			t.Errorf("unexpected fixture request: %s %s", req.Method, req.URL)
			return nil, fmt.Errorf("unconfigured fixture URL: %s", req.URL)
		}
		local := req.Clone(req.Context())
		local.URL.Scheme = target.Scheme
		local.URL.Host = target.Host
		local.Host = target.Host
		return base.RoundTrip(local)
	})
}
