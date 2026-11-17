package javdb

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/go-resty/resty/v2"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/ratelimit"
	"github.com/stretchr/testify/require"
)

const countryNotice = `Due to copyright restrictions, access to this site is prohibited in the country where your internet is located.`

func requireCountryBlocked(t *testing.T, err error) {
	t.Helper()
	classified, ok := models.AsScraperError(err)
	require.True(t, ok, "expected typed error, got %v", err)
	require.Equal(t, models.ScraperErrorKindBlocked, classified.Kind)
	require.Equal(t, 200, classified.StatusCode)
	require.False(t, classified.Retryable)
	require.False(t, classified.Temporary)
	require.Contains(t, classified.Message, "country")
}

func TestCountryRestrictionPage(t *testing.T) {
	for _, tc := range []struct {
		name, page string
		blocked    bool
	}{
		{"notice", "<html><body>" + countryNotice + "</body></html>", true},
		{"line break", strings.Replace(countryNotice, ", ", ",<br>", 1), true},
		{"paragraphs", "<p>Due to copyright restrictions,</p><p>access to this site is prohibited in the country where your internet is located.</p>", true},
		{"formatting", `<p>DUE TO COPYRIGHT RESTRICTIONS, <strong>access to this site is prohibited</strong> in the country where your internet is located.</p>`, true},
		{"entities whitespace", strings.ReplaceAll(countryNotice, " ", "&#32;\n"), true},
		{"normal", `<html><body><h2>Movie</h2><p>Copyright information</p></body></html>`, false},
		{"not found", `<html><body>No results found</body></html>`, false},
		{"script only", "<script>" + countryNotice + "</script><body>Movie</body>", false},
		{"style only", "<style>" + countryNotice + "</style><body>Movie</body>", false},
		{"comment only", "<!--" + countryNotice + "--><body>Movie</body>", false},
		{"partial", "Due to copyright restrictions, some images are unavailable", false},
	} {
		t.Run(tc.name, func(t *testing.T) { require.Equal(t, tc.blocked, isCountryRestrictionPage(tc.page)) })
	}
}

func TestCountryRestrictionStopsSearchAndDetail(t *testing.T) {
	for _, mode := range []string{"search", "short code", "detail", "direct retry"} {
		t.Run(mode, func(t *testing.T) {
			var requests, solverRequests atomic.Int32
			solver := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				solverRequests.Add(1)
				http.Error(w, "unexpected solver", 500)
			}))
			defer solver.Close()
			s := newScraper(&models.ScraperSettings{Enabled: true, UseFlareSolverr: true}, nil, models.FlareSolverrConfig{Enabled: true, URL: solver.URL, Timeout: 1})
			s.rateLimiter = ratelimit.NewLimiter(0)
			s.client = resty.New().SetTransport(offlineFixtureTransport(func(req *http.Request) (*http.Response, error) {
				requests.Add(1)
				return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader("<html><body>" + countryNotice + "</body></html>")), Request: req}, nil
			}))
			var err error
			switch mode {
			case "search":
				result, e := s.Search(context.Background(), "ABC-123")
				require.Nil(t, result)
				err = e
			case "short code":
				result, e := s.Search(context.Background(), "AbC12")
				require.Nil(t, result)
				err = e
			case "detail":
				result, e := s.ScrapeURL(context.Background(), "https://javdb.com/v/AbC12")
				require.Nil(t, result)
				err = e
			case "direct retry":
				page, e := s.fetchPageDirectCtx(context.Background(), "https://javdb.com/v/AbC12")
				require.Empty(t, page)
				err = e
			}
			requireCountryBlocked(t, err)
			require.EqualValues(t, 1, requests.Load(), "must not retry or fall back to search")
			require.Zero(t, solverRequests.Load(), "a country block is not a solver challenge")
			require.NoError(t, s.Close())
		})
	}
}

func TestCountryRestrictionFromSolver(t *testing.T) {
	var gets atomic.Int32
	solver := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct{ Cmd string }
		require.NoError(t, json.NewDecoder(r.Body).Decode(&req))
		w.Header().Set("Content-Type", "application/json")
		switch req.Cmd {
		case "sessions.create":
			io.WriteString(w, `{"status":"ok","session":"test"}`)
		case "sessions.destroy":
			io.WriteString(w, `{"status":"ok"}`)
		case "request.get":
			gets.Add(1)
			json.NewEncoder(w).Encode(map[string]any{"status": "ok", "solution": map[string]any{"response": "<body>" + countryNotice + "</body>"}})
		default:
			t.Errorf("unexpected command %s", req.Cmd)
		}
	}))
	defer solver.Close()
	s := newScraper(&models.ScraperSettings{Enabled: true, UseFlareSolverr: true}, nil, models.FlareSolverrConfig{Enabled: true, URL: solver.URL, Timeout: 2})
	defer s.Close()
	s.client = resty.New().SetTransport(offlineFixtureTransport(func(req *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 403, Header: make(http.Header), Body: io.NopCloser(strings.NewReader("blocked")), Request: req}, nil
	}))
	page, err := s.fetchPageCtx(context.Background(), "https://javdb.com/v/test")
	require.Empty(t, page)
	requireCountryBlocked(t, err)
	require.EqualValues(t, 1, gets.Load())
}

func TestCountryRestrictionAfterSearchOrSparseDetail(t *testing.T) {
	for _, mode := range []string{"search detail", "sparse retry"} {
		t.Run(mode, func(t *testing.T) {
			calls := 0
			s := newScraper(&models.ScraperSettings{Enabled: true}, nil, models.FlareSolverrConfig{})
			s.client = resty.New().SetTransport(offlineFixtureTransport(func(req *http.Request) (*http.Response, error) {
				calls++
				body := "<body>" + countryNotice + "</body>"
				if calls == 1 {
					if mode == "search detail" {
						body = `<html><body><div class="movie-list"><div class="item"><a href="/v/fixture"><div class="video-title"><strong>ABC-123</strong> Movie</div></a></div></div></body></html>`
					} else {
						body = "<body>Empty page</body>"
					}
				}
				return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body)), Request: req}, nil
			}))
			var result *models.ScraperResult
			var err error
			if mode == "search detail" {
				result, err = s.Search(context.Background(), "ABC-123")
			} else {
				result, err = s.ScrapeURL(context.Background(), "https://javdb.com/v/fixture")
			}
			require.Nil(t, result)
			requireCountryBlocked(t, err)
			require.Equal(t, 2, calls)
		})
	}
}
