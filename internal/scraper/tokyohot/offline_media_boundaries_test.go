package tokyohot

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
	body, status, err := s.fetchPageCtx(ctx, "https://www.tokyo-hot.com/")
	require.Empty(t, body)
	require.Zero(t, status)
	require.ErrorIs(t, err, context.Canceled)
	calls := 0
	s.rateLimiter = ratelimit.NewLimiter(0)
	s.client = resty.New().SetTransport(mediaBoundaryTransport(func(req *http.Request) (*http.Response, error) {
		calls++
		return &http.Response{StatusCode: 200, Header: make(http.Header), Request: req, Body: io.NopCloser(strings.NewReader(`<title>Just a moment...</title><div id="cf-challenge-running"></div>`))}, nil
	}))
	body, status, err = s.fetchPageCtx(context.Background(), "https://www.tokyo-hot.com/")
	require.Empty(t, body)
	require.Equal(t, 200, status)
	require.Equal(t, 1, calls)
	classified, ok := models.AsScraperError(err)
	require.True(t, ok)
	require.Equal(t, models.ScraperErrorKindBlocked, classified.Kind)
}

func TestMetadataLinksAndMediaFiltering(t *testing.T) {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(`<dl class="info"><dt>Maker</dt><dd>Plain studio</dd><dt>Play</dt><dd>Drama,Drama</dd><dd><a href="?type=genre">Additional</a><a href="?type=genre">Additional</a></dd></dl><div class="scap"><a href=" "></a><a href="/one.jpg"></a><a href="/one.jpg"></a></div><source src="/trailer.mp4">`))
	require.NoError(t, err)
	require.Equal(t, "Plain studio", extractInfoLinkValue(doc, []string{"Maker"}))
	require.Equal(t, []string{"Drama", "Additional"}, extractGenres(doc))
	require.Equal(t, []string{"https://www.tokyo-hot.com/one.jpg"}, extractScreenshotURLs(doc, "https://www.tokyo-hot.com"))
	require.Equal(t, "https://www.tokyo-hot.com/trailer.mp4", extractTrailerURL(doc, "https://www.tokyo-hot.com"))
	require.Equal(t, "http://%", (&scraper{}).applyLanguage("http://%"))
	id, err := (&scraper{}).ExtractIDFromURL("https://www.tokyo-hot.com/archive/n1234/")
	require.NoError(t, err)
	require.Equal(t, "N1234", id)
}
