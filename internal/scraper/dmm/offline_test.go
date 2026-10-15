package dmm

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"testing"

	"github.com/go-resty/resty/v2"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/assert"
)

type dmmFixtureTransport func(*http.Request) (*http.Response, error)

func (f dmmFixtureTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func dmmFixtureResponse(r *http.Request, status int, body string) *http.Response {
	return &http.Response{StatusCode: status, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body)), Request: r}
}

// Metadata-only fixtures deliberately omit remote posters and actress photos.
// Assert the expected probe shapes and return a real HTTP 404, never a network
// failure masquerading as a passing test. Tests of successful media use explicit
// per-URL fixtures instead.
func stubDMMMetadataRequests(t *testing.T, s *scraper) {
	t.Helper()
	actressPage := regexp.MustCompile(`^/mono/dvd/-/list/=/article=actress/id=\d+/$`)
	rt := dmmFixtureTransport(func(r *http.Request) (*http.Response, error) {
		allowed := r.Method == http.MethodGet && r.URL.Host == "www.dmm.co.jp" && actressPage.MatchString(r.URL.Path)
		allowed = allowed || r.Method == http.MethodHead && r.URL.Host == "pics.dmm.co.jp" && strings.HasSuffix(r.URL.Path, ".jpg")
		allowed = allowed || r.Method == http.MethodGet && (r.URL.Host == "awsimgsrc.dmm.com" || r.URL.Host == "awsimgsrc.dmm.co.jp") && strings.HasSuffix(r.URL.Path, "ps.jpg")
		if !allowed {
			t.Errorf("unexpected metadata request: %s %s", r.Method, r.URL)
			return nil, fmt.Errorf("unexpected metadata request: %s %s", r.Method, r.URL)
		}
		return dmmFixtureResponse(r, http.StatusNotFound, ""), nil
	})
	s.client.SetTransport(rt)
	s.thumbnailClient = resty.New().SetTransport(rt).SetRedirectPolicy(resty.NoRedirectPolicy())
}

func stubDMMBrowser(t *testing.T, s *scraper, wantURL, html string, wantErr error) {
	t.Helper()
	calls := 0
	s.browserFetch = func(ctx context.Context, url string) (string, error) {
		calls++
		assert.Equal(t, wantURL, url)
		assert.NoError(t, ctx.Err())
		return html, wantErr
	}
	t.Cleanup(func() { assert.Equal(t, 1, calls, "browser fetch must be exercised exactly once") })
}

func TestFetchBrowserHTML_DefaultValidatesBeforeLaunch(t *testing.T) {
	s := &scraper{}
	_, err := s.fetchBrowserHTML(context.Background(), "")
	assert.ErrorContains(t, err, "browser URL is required")
}

func TestActressThumbnailClient_DefaultConstructionWithoutCandidates(t *testing.T) {
	for _, tc := range []struct {
		name  string
		proxy *models.ProxyProfile
	}{
		{name: "direct"},
		{name: "proxy", proxy: &models.ProxyProfile{URL: "http://127.0.0.1:1"}},
		{name: "invalid proxy fallback", proxy: &models.ProxyProfile{URL: "://invalid"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			// No candidate names or ID means no HTTP can be issued. This exercises
			// production client construction, including proxy error fallback.
			s := &scraper{proxyProfile: tc.proxy}
			assert.Empty(t, s.tryActressThumbURLs(context.Background(), "", "", 0))
		})
	}
}

func TestActressThumbnailClient_RejectsRedirect(t *testing.T) {
	s := newTestDMMScraper(t)
	var requested []string
	s.thumbnailClient.SetTransport(dmmFixtureTransport(func(r *http.Request) (*http.Response, error) {
		requested = append(requested, r.URL.String())
		assert.Equal(t, http.MethodHead, r.Method)
		assert.Equal(t, "pics.dmm.co.jp", r.URL.Host)
		resp := dmmFixtureResponse(r, http.StatusFound, "")
		resp.Header.Set("Location", "https://unexpected.invalid/redirect")
		return resp, nil
	}))
	assert.Empty(t, s.tryActressThumbURLs(context.Background(), "Yui", "Hatano", 0))
	assert.Equal(t, []string{
		"https://pics.dmm.co.jp/mono/actjpgs/hatano_yui.jpg",
		"https://pics.dmm.co.jp/mono/actjpgs/yui_hatano.jpg",
	}, requested, "the redirect destination must never be followed")
}

// A zero-delay limiter can return immediately before noticing cancellation.
// Preserve the request's cancelled context without reaching a real transport.
func stubDMMCanceledRequest(t *testing.T, s *scraper, wantURL string) {
	t.Helper()
	calls := 0
	s.client.SetTransport(dmmFixtureTransport(func(r *http.Request) (*http.Response, error) {
		calls++
		assert.Equal(t, http.MethodGet, r.Method)
		assert.Equal(t, wantURL, r.URL.String())
		if err := r.Context().Err(); err != nil {
			assert.ErrorIs(t, err, context.Canceled)
			return nil, err
		}
		t.Error("cancelled operation sent a request with a live context")
		return nil, fmt.Errorf("unexpected live request context")
	}))
	t.Cleanup(func() {
		assert.LessOrEqual(t, calls, 1, "a cancelled request must not be retried")
	})
}

func stubDMMFetchError(t *testing.T, s *scraper, wantURL string) {
	t.Helper()
	calls := 0
	s.client.SetTransport(dmmFixtureTransport(func(r *http.Request) (*http.Response, error) {
		calls++
		assert.Equal(t, http.MethodGet, r.Method)
		assert.Equal(t, wantURL, r.URL.String())
		assert.NoError(t, r.Context().Err())
		return nil, fmt.Errorf("fixture connection failure")
	}))
	t.Cleanup(func() { assert.Equal(t, 1, calls) })
}
