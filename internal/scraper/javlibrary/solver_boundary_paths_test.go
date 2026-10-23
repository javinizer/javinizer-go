package javlibrary

import (
	"context"
	"encoding/json"
	"github.com/go-resty/resty/v2"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

type solverBoundaryTransport func(*http.Request) (*http.Response, error)

func (f solverBoundaryTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func TestSolverFallbackProtocol(t *testing.T) {
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
				case "sessions.create":
					_, _ = io.WriteString(w, `{"status":"ok","session":"fixture-session"}`)
				case "request.get":
					assert.Equal(t, "fixture-session", req.Session)
					assert.Equal(t, "https://www.javlibrary.com/en/?v=fixture", req.URL)
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
			s.client = resty.New().SetTransport(solverBoundaryTransport(func(req *http.Request) (*http.Response, error) {
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
			html, err := s.fetchPageCtx(context.Background(), "https://www.javlibrary.com/en/?v=fixture")
			if tc.failed {
				require.Error(t, err)
				require.Empty(t, html)
				classified, ok := models.AsScraperError(err)
				require.True(t, ok)
				require.Equal(t, http.StatusForbidden, classified.StatusCode)
				require.Equal(t, models.ScraperErrorKindBlocked, classified.Kind)
			} else if strings.Contains(tc.name, "challenged") {
				require.Error(t, err)
				require.Empty(t, html)
				classified, ok := models.AsScraperError(err)
				require.True(t, ok)
				require.Equal(t, models.ScraperErrorKindBlocked, classified.Kind)
			} else {
				require.NoError(t, err)
				require.Equal(t, tc.html, html)
				html, err = s.fetchPageCtx(context.Background(), "https://www.javlibrary.com/en/?v=fixture")
				require.NoError(t, err)
				require.Equal(t, "reused cookie response", html)
			}
			if tc.failed || strings.Contains(tc.name, "challenged") {
				require.Equal(t, 1, calls)
			} else {
				require.Equal(t, 2, calls)
			}
			commandsMu.Lock()
			observed := append([]string(nil), commands...)
			commandsMu.Unlock()
			require.Equal(t, []string{"sessions.create", "request.get"}, observed)
		})
	}
}

func TestGalleryFallbacksFilterInvalidMediaAndDeduplicate(t *testing.T) {
	s := newScraper(&models.ScraperSettings{Enabled: true}, nil, models.FlareSolverrConfig{})
	html := `<img data-src="https://example.com/redirect.php?image=01.jpg">
 <img data-src="https://example.com/loading.jpg"><img data-src="https://example.com/moviepl.jpg"><img data-src="https://example.com/movieps.jpg">
 <div id="video_gallery"><a href="https://example.com/gallery.jpg">full image</a></div>
 <a href="https://example.com/sample.jpg">sample</a><a href="https://example.com/sample.jpg">duplicate</a>`
	require.Equal(t, []string{"https://example.com/gallery.jpg", "https://example.com/sample.jpg"}, s.extractScreenshotURLs(html))
	require.Equal(t, "https://example.com/clip.mp4", s.extractTrailerURL(`<video src="https://example.com/clip.mp4">sample</video>`))
	require.Equal(t, "?v=valid123", s.extractMovieURLFromHTML(`<div class="video"><div class="id">ABC-123</div></div><div class="video" id="vid_valid123"><div class="id">ABC-123</div></div>`, "ABC-123"))
}
