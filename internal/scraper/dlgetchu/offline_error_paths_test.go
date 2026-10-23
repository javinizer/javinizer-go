package dlgetchu

import (
	"context"
	"fmt"
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

type offlineFixtureTransport func(*http.Request) (*http.Response, error)

func (f offlineFixtureTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

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
			s := &scraper{enabled: true, baseURL: "https://dl.getchu.com", rateLimiter: ratelimit.NewLimiter(0), client: resty.New().SetTransport(offlineFixtureTransport(func(req *http.Request) (*http.Response, error) {
				calls++
				require.Equal(t, "dl.getchu.com", req.URL.Host)
				require.Equal(t, "/i/item12345", req.URL.Path)
				if tc.transportErr {
					return nil, io.ErrUnexpectedEOF
				}
				return &http.Response{StatusCode: tc.status, Header: make(http.Header), Request: req, Body: io.NopCloser(strings.NewReader(`<title>Just a moment...</title><div id="cf-challenge-running"></div>`))}, nil
			}))}
			result, err := s.ScrapeURL(context.Background(), "https://dl.getchu.com/i/item12345")
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
			s := &scraper{enabled: true, baseURL: "https://dl.getchu.com", rateLimiter: ratelimit.NewLimiter(0), client: resty.New().SetTransport(offlineFixtureTransport(func(req *http.Request) (*http.Response, error) {
				calls++
				if status == 0 {
					return nil, io.ErrUnexpectedEOF
				}
				return &http.Response{StatusCode: status, Header: make(http.Header), Request: req, Body: io.NopCloser(strings.NewReader(""))}, nil
			}))}
			result, err := s.Search(context.Background(), "https://dl.getchu.com/i/item12345")
			require.Nil(t, result)
			require.Error(t, err)
			require.Equal(t, 1, calls)
			if status == 0 {
				require.ErrorIs(t, err, io.ErrUnexpectedEOF)
			} else {
				classified, ok := models.AsScraperError(err)
				require.True(t, ok)
				require.Equal(t, models.ScraperErrorKindUnavailable, classified.Kind)
				require.Equal(t, http.StatusBadGateway, classified.StatusCode)
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
	body, status, err := s.fetchPageCtx(ctx, "https://dl.getchu.com/i/item12345")
	require.Empty(t, body)
	require.Zero(t, status)
	require.ErrorIs(t, err, context.Canceled)
}

func TestOfflineLegacyItemURLPreservesIdentifier(t *testing.T) {
	s := &scraper{}
	id, err := s.ExtractIDFromURL("https://dl.getchu.com/i/itemlegacy/")
	require.NoError(t, err)
	require.Equal(t, "legacy", id)
}
