package javdb

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/PuerkitoBio/goquery"
	"github.com/go-resty/resty/v2"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/ratelimit"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCurrentScopeJavDBSolverProtocol(t *testing.T) {
	for _, tc := range []struct {
		name, html string
		failed     bool
	}{
		{"resolved response and cookie reuse", "<html>resolved detail</html>", false},
		{"solver still challenged", `<html><title>Just a moment...</title><div id="cf-challenge-running"></div></html>`, false},
		{"solver failure preserves direct status", "", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var commands []string
			var commandsMu sync.Mutex
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var req struct{ Cmd, URL, Session string }
				if !assert.NoError(t, json.NewDecoder(r.Body).Decode(&req)) {
					w.WriteHeader(400)
					return
				}
				commandsMu.Lock()
				commands = append(commands, req.Cmd)
				commandsMu.Unlock()
				w.Header().Set("Content-Type", "application/json")
				if tc.failed {
					_, _ = io.WriteString(w, `{"status":"error","message":"fixture failure"}`)
					return
				}
				switch req.Cmd {
				case "sessions.destroy":
					assert.Equal(t, "fixture-session", req.Session)
					_, _ = io.WriteString(w, `{"status":"ok"}`)
				case "sessions.create":
					_, _ = io.WriteString(w, `{"status":"ok","session":"fixture-session"}`)
				case "request.get":
					assert.Equal(t, "fixture-session", req.Session)
					assert.Equal(t, "https://javdb.com/v/fixture", req.URL)
					_ = json.NewEncoder(w).Encode(map[string]any{"status": "ok", "solution": map[string]any{"response": tc.html, "cookies": []map[string]string{{"name": "clearance", "value": "fixture-cookie"}}}})
				default:
					t.Errorf("unexpected solver command %q", req.Cmd)
					w.WriteHeader(400)
				}
			}))
			t.Cleanup(server.Close)
			s := newScraper(&models.ScraperSettings{Enabled: true, UseFlareSolverr: true}, nil, models.FlareSolverrConfig{Enabled: true, URL: server.URL, Timeout: 2})
			require.NotNil(t, s.flaresolverr)
			t.Cleanup(func() { require.NoError(t, s.Close()) })
			calls := 0
			s.client = resty.New().SetTransport(offlineFixtureTransport(func(req *http.Request) (*http.Response, error) {
				calls++
				status, body := http.StatusForbidden, "blocked direct response"
				if calls == 2 {
					cookie, err := req.Cookie("clearance")
					require.NoError(t, err)
					require.Equal(t, "fixture-cookie", cookie.Value)
					status, body = http.StatusOK, "reused cookie response"
				}
				return &http.Response{StatusCode: status, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body)), Request: req}, nil
			}))
			html, err := s.fetchPageCtx(context.Background(), "https://javdb.com/v/fixture")
			if tc.failed {
				require.Error(t, err)
				require.Empty(t, html)
				classified, ok := models.AsScraperError(err)
				require.True(t, ok)
				require.Equal(t, http.StatusForbidden, classified.StatusCode)
			} else if strings.Contains(tc.name, "challenged") {
				require.Error(t, err)
				require.Empty(t, html)
				classified, ok := models.AsScraperError(err)
				require.True(t, ok)
				require.Equal(t, models.ScraperErrorKindBlocked, classified.Kind)
			} else {
				require.NoError(t, err)
				require.Equal(t, tc.html, html)
				html, err = s.fetchPageCtx(context.Background(), "https://javdb.com/v/fixture")
				require.NoError(t, err)
				require.Equal(t, "reused cookie response", html)
			}
			require.NoError(t, s.Close())
			commandsMu.Lock()
			observed := append([]string(nil), commands...)
			commandsMu.Unlock()
			expected := []string{"sessions.create", "request.get"}
			if !tc.failed {
				expected = append(expected, "sessions.destroy")
			}
			require.Equal(t, expected, observed)
		})
	}
}

func TestCurrentScopeJavDBFetchBoundary(t *testing.T) {
	s := &scraper{baseURL: "https://javdb.com", rateLimiter: ratelimit.NewLimiter(time.Hour), client: resty.New().SetTransport(offlineFixtureTransport(func(*http.Request) (*http.Response, error) {
		t.Error("invalid or cancelled request must not reach transport")
		return nil, context.Canceled
	}))}
	for _, raw := range []string{"http://%", "file:///tmp/movie", "https:///missing-host", "http://127.0.0.1/private", "https://192.0.2.1/movie"} {
		t.Run(raw, func(t *testing.T) {
			for _, fetch := range []func(context.Context, string) (string, error){s.fetchPageCtx, s.fetchPageDirectCtx} {
				body, err := fetch(context.Background(), raw)
				require.Empty(t, body)
				require.Error(t, err)
			}
		})
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	for _, fetch := range []func(context.Context, string) (string, error){s.fetchPageCtx, s.fetchPageDirectCtx} {
		body, err := fetch(ctx, "https://javdb.com/v/fixture")
		require.Empty(t, body)
		require.ErrorIs(t, err, context.Canceled)
	}
}

func TestCurrentScopeJavDBListFiltering(t *testing.T) {
	for _, tc := range []struct {
		html string
		want []string
	}{
		{`<div>first, --, second / 未知</div>`, []string{"first", "second", "未知"}},
		{`<div>none, --</div>`, nil},
	} {
		doc, err := goquery.NewDocumentFromReader(strings.NewReader(tc.html))
		require.NoError(t, err)
		require.Equal(t, tc.want, extractStringList(doc.Find("div")))
	}
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(`<div><a> </a><a href="/actors/a">花子</a><a href="/actors/a">花子</a></div>`))
	require.NoError(t, err)
	cast := extractActresses(doc.Find("div"))
	require.Len(t, cast, 1)
	require.Equal(t, "花子", cast[0].JapaneseName)
	require.False(t, isNotAvailableValue(""))
}

func TestCurrentScopeJavDBScrapeURLSparseRetry(t *testing.T) {
	for _, tc := range []struct {
		name   string
		status int
		rich   bool
	}{
		{"failed retry", http.StatusBadGateway, false},
		{"still sparse", http.StatusOK, false},
		{"rich retry", http.StatusOK, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			s := &scraper{enabled: true, baseURL: "https://javdb.com", rateLimiter: ratelimit.NewLimiter(0), client: resty.New().SetTransport(offlineFixtureTransport(func(req *http.Request) (*http.Response, error) {
				calls++
				status, body := http.StatusOK, "<html></html>"
				if calls == 2 {
					status = tc.status
					if tc.rich {
						body = `<h2 class="title is-4"><strong>ABC-123</strong> A meaningful title</h2>`
					}
				}
				return &http.Response{StatusCode: status, Header: make(http.Header), Request: req, Body: io.NopCloser(strings.NewReader(body))}, nil
			}))}
			result, err := s.ScrapeURL(context.Background(), "https://javdb.com/v/fixture")
			require.Equal(t, 2, calls)
			if tc.rich {
				require.NoError(t, err)
				require.Equal(t, "A meaningful title", result.Title)
				require.Equal(t, "ABC-123", result.ID)
			} else {
				require.Nil(t, result)
				require.Error(t, err)
				if tc.status != http.StatusOK {
					require.ErrorContains(t, err, "direct retry failed")
				} else {
					require.ErrorContains(t, err, "non-detail content")
				}
			}
		})
	}
}

func TestCurrentScopeJavDBScreenshotDeduplication(t *testing.T) {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(`<div class="preview-images"><a href="/one.jpg"></a><a href="/one.jpg"></a><a href=""></a><a href="/two.jpg"></a></div>`))
	require.NoError(t, err)
	require.Equal(t, []string{"https://javdb.com/one.jpg", "https://javdb.com/two.jpg"}, extractScreenshotURLs(doc, "https://javdb.com"))
}
