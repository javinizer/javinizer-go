package mgstage

import (
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/go-resty/resty/v2"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/ratelimit"
	"github.com/stretchr/testify/assert"
)

// scriptedMGStageTransport serves absolute production URLs entirely in memory.
// Unexpected requests fail the test instead of silently reaching a network.
type scriptedMGStageTransport struct {
	t        *testing.T
	routes   map[string]mockHTTPResponse
	requests map[string]int
}

func (s *scriptedMGStageTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	assert.Equal(s.t, http.MethodGet, req.Method)
	assert.Equal(s.t, "https", req.URL.Scheme)
	assert.Equal(s.t, "www.mgstage.com", req.URL.Host)
	uri := req.URL.RequestURI()
	s.requests[uri]++
	response, ok := s.routes[uri]
	if !ok {
		s.t.Errorf("unexpected MGStage fixture request: %s", req.URL)
		return nil, fmt.Errorf("unexpected MGStage fixture request: %s", req.URL)
	}
	return &http.Response{
		StatusCode: response.statusCode,
		Header:     http.Header{"Content-Type": []string{"text/html; charset=UTF-8"}},
		Body:       io.NopCloser(strings.NewReader(response.body)),
		Request:    req,
	}, nil
}

func newScriptedMGStageScraper(t *testing.T, enabled bool, routes map[string]mockHTTPResponse) *scraper {
	t.Helper()
	transport := &scriptedMGStageTransport{t: t, routes: routes, requests: make(map[string]int)}
	t.Cleanup(func() {
		for uri := range routes {
			assert.Equal(t, 1, transport.requests[uri], "fixture route must be exercised exactly once: %s", uri)
		}
	})
	return &scraper{
		client:      resty.New().SetTransport(transport),
		enabled:     enabled,
		rateLimiter: ratelimit.NewLimiter(0),
		settings:    models.ScraperSettings{Enabled: enabled},
	}
}

func newMissingMGStageScraper(t *testing.T, enabled bool) *scraper {
	t.Helper()
	return newScriptedMGStageScraper(t, enabled, map[string]mockHTTPResponse{
		"/search/cSearch.php?search_word=INVALID-ID&type=top&page=1&list_cnt=120": {statusCode: http.StatusNotFound},
		"/product/product_detail/INVALID-ID/":                                     {statusCode: http.StatusNotFound},
	})
}
