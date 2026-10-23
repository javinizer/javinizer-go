package jav321

import (
	"context"
	"fmt"
	"github.com/PuerkitoBio/goquery"
	"github.com/go-resty/resty/v2"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/ratelimit"
	"github.com/stretchr/testify/require"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestOfflineDetailFailureClassification(t *testing.T) {
	for _, tc := range []struct {
		name         string
		status       int
		transportErr bool
	}{
		{"missing", 404, false}, {"rate limited", 429, false}, {"forbidden", 403, false}, {"legal block", 451, false}, {"unavailable", 502, false}, {"challenge", 200, false}, {"transport", 0, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			s := &scraper{enabled: true, baseURL: "https://www.jav321.com", rateLimiter: ratelimit.NewLimiter(0), client: resty.New().SetTransport(offlineFixtureTransport(func(req *http.Request) (*http.Response, error) {
				calls++
				require.Equal(t, "www.jav321.com", req.URL.Host)
				require.Equal(t, "/video/ABC-123", req.URL.Path)
				if tc.transportErr {
					return nil, io.ErrUnexpectedEOF
				}
				return &http.Response{StatusCode: tc.status, Header: make(http.Header), Request: req, Body: io.NopCloser(strings.NewReader(`<title>Just a moment...</title><div id="cf-challenge-running"></div>`))}, nil
			}))}
			result, err := s.ScrapeURL(context.Background(), "https://www.jav321.com/video/ABC-123")
			require.Nil(t, result)
			require.Error(t, err)
			require.Equal(t, 1, calls)
			if tc.transportErr {
				require.ErrorIs(t, err, io.ErrUnexpectedEOF)
			} else {
				classified, ok := models.AsScraperError(err)
				require.True(t, ok)
				if tc.status == 200 {
					require.Equal(t, models.ScraperErrorKindBlocked, classified.Kind)
				} else if tc.status == 404 {
					require.Equal(t, models.ScraperErrorKindNotFound, classified.Kind)
				} else {
					require.Equal(t, tc.status, classified.StatusCode)
					wantKind := map[int]models.ScraperErrorKind{429: models.ScraperErrorKindRateLimited, 403: models.ScraperErrorKindBlocked, 451: models.ScraperErrorKindBlocked, 502: models.ScraperErrorKindUnavailable}[tc.status]
					require.Equal(t, wantKind, classified.Kind)
				}
			}
		})
	}
}
func TestOfflineDetailSearchPropagatesFetchFailure(t *testing.T) {
	for _, status := range []int{0, 502} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			calls := 0
			s := &scraper{enabled: true, baseURL: "https://www.jav321.com", rateLimiter: ratelimit.NewLimiter(0), client: resty.New().SetTransport(offlineFixtureTransport(func(req *http.Request) (*http.Response, error) {
				calls++
				if status == 0 {
					return nil, io.ErrUnexpectedEOF
				}
				return &http.Response{StatusCode: status, Header: make(http.Header), Request: req, Body: io.NopCloser(strings.NewReader(""))}, nil
			}))}
			result, err := s.Search(context.Background(), "https://www.jav321.com/video/ABC-123")
			require.Nil(t, result)
			require.Error(t, err)
			require.Equal(t, 1, calls)
			if status == 0 {
				require.ErrorIs(t, err, io.ErrUnexpectedEOF)
			} else {
				require.ErrorContains(t, err, "502")
			}
		})
	}
}
func TestOfflineRateLimitCancellationPreventsFetch(t *testing.T) {
	s := &scraper{rateLimiter: ratelimit.NewLimiter(time.Hour), client: resty.New().SetTransport(offlineFixtureTransport(func(*http.Request) (*http.Response, error) {
		t.Error("cancelled limiter must not dispatch HTTP")
		return nil, context.Canceled
	}))}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	body, status, err := s.fetchPageCtx(ctx, "https://www.jav321.com/video/ABC-123")
	require.Empty(t, body)
	require.Zero(t, status)
	require.ErrorIs(t, err, context.Canceled)
}

func TestOfflineSearchCandidateFallbacks(t *testing.T) {
	for _, tc := range []struct{ name, html, want string }{
		{"one unmatched link", `<div><a href="/video/opaque">title without ID</a></div>`, "https://www.jav321.com/video/opaque"},
		{"multiple unmatched links", `<div><a href="/video/one">one</a><a href="/video/two">two</a></div>`, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			s := &scraper{baseURL: "https://www.jav321.com", rateLimiter: ratelimit.NewLimiter(0), client: resty.New().SetTransport(offlineFixtureTransport(func(req *http.Request) (*http.Response, error) {
				calls++
				require.Equal(t, http.MethodPost, req.Method)
				require.Equal(t, "/search", req.URL.Path)
				require.NoError(t, req.ParseForm())
				require.Equal(t, "ABC-123", req.Form.Get("sn"))
				return &http.Response{StatusCode: 200, Header: make(http.Header), Request: req, Body: io.NopCloser(strings.NewReader(tc.html))}, nil
			}))}
			found, err := s.GetURL(context.Background(), "ABC-123")
			require.Equal(t, tc.want, found)
			require.Equal(t, 1, calls)
			if tc.want == "" {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
		})
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	s := &scraper{rateLimiter: ratelimit.NewLimiter(time.Hour)}
	found, err := s.GetURL(ctx, "ABC-123")
	require.Empty(t, found)
	require.ErrorIs(t, err, context.Canceled)
}

func TestOfflineScreenshotsIgnoreBlankAndDuplicateImages(t *testing.T) {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(`<a href="/snapshot/1"><img src=" "></a><a href="/snapshot/2"><img src="/one.jpg"></a><a href="/snapshot/3"><img src="/one.jpg"></a>`))
	require.NoError(t, err)
	require.Equal(t, []string{"https://www.jav321.com/one.jpg"}, extractScreenshotURLs(doc, "https://www.jav321.com"))
}
