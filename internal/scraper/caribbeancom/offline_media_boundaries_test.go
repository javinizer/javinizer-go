package caribbeancom

import (
	"context"
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

type mediaBoundaryTransport func(*http.Request) (*http.Response, error)

func (f mediaBoundaryTransport) RoundTrip(req *http.Request) (*http.Response, error) { return f(req) }
func TestFetchPageCancellationAndChallenge(t *testing.T) {
	s := &scraper{rateLimiter: ratelimit.NewLimiter(time.Hour)}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	body, status, err := s.fetchPageCtx(ctx, "https://www.caribbeancom.com/")
	require.Empty(t, body)
	require.Zero(t, status)
	require.ErrorIs(t, err, context.Canceled)
	calls := 0
	s.rateLimiter = ratelimit.NewLimiter(0)
	s.client = resty.New().SetTransport(mediaBoundaryTransport(func(req *http.Request) (*http.Response, error) {
		calls++
		return &http.Response{StatusCode: 200, Header: make(http.Header), Request: req, Body: io.NopCloser(strings.NewReader(`<title>Just a moment...</title><div id="cf-challenge-running"></div>`))}, nil
	}))
	body, status, err = s.fetchPageCtx(context.Background(), "https://www.caribbeancom.com/")
	require.Empty(t, body)
	require.Equal(t, 200, status)
	require.Equal(t, 1, calls)
	classified, ok := models.AsScraperError(err)
	require.True(t, ok)
	require.Equal(t, models.ScraperErrorKindBlocked, classified.Kind)
}

func TestMetadataFallbacksAndMediaFiltering(t *testing.T) {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(`<li class="movie-spec"><span class="spec-title">Runtime:</span> 120 minutes</li><a class="gallery-item"></a><a class="gallery-item" href="/one.jpg" data-is_sample="1"></a><a class="gallery-item" href="/one.jpg"></a><a class="gallery-item" href="/private.jpg" data-is_sample="0"></a>`))
	require.NoError(t, err)
	require.Equal(t, "Runtime: 120 minutes", extractSpecValue(doc, []string{"Runtime"}))
	require.Equal(t, []string{"https://www.caribbeancom.com/one.jpg"}, extractScreenshots(doc, "https://www.caribbeancom.com"))
	require.Nil(t, parseReleaseDateFromID("023124-123"), "nonexistent February31 must not normalize into March")
	s := &scraper{language: "ja"}
	require.Equal(t, "http://%", s.applyLanguage("http://%"))
	require.Equal(t, "https://www.caribbeancom.com:8443/", s.applyLanguage("https://en.caribbeancom.com:8443/eng"))
}
