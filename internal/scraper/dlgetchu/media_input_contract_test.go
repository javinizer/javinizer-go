package dlgetchu

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/require"
)

func TestScrapeURLUsesPageIdentityAndDeduplicatesScreenshots(t *testing.T) {
	const sourceURL = "https://dl.getchu.com/detail/featured"
	const html = `<html><head><title>Fixture title</title></head><body>
<div>作品ID: 12345</div>
<a href="/data/item_img/demo/first.jpg" class="highslide">First</a>
<a href="/data/item_img/demo/first.jpg" class="highslide">Duplicate</a>
<a href="/data/item_img/demo/second.webp" class="highslide">Second</a>
</body></html>`
	calls := 0
	s := newScraper(&models.ScraperSettings{Enabled: true}, nil, models.FlareSolverrConfig{})
	t.Cleanup(func() { require.NoError(t, s.Close()) })
	s.client.SetTransport(offlineFixtureTransport(func(req *http.Request) (*http.Response, error) {
		calls++
		require.Equal(t, http.MethodGet, req.Method)
		require.Equal(t, sourceURL, req.URL.String())
		return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": {"text/html; charset=utf-8"}}, Request: req, Body: io.NopCloser(strings.NewReader(html))}, nil
	}))

	// The URL has no item identifier: the fetched page must supply it instead.
	result, err := s.ScrapeURL(context.Background(), sourceURL)
	require.NoError(t, err)
	require.NotNil(t, result)
	require.Equal(t, 1, calls)
	require.Equal(t, "12345", result.ID)
	require.Equal(t, "12345", result.ContentID)
	require.Equal(t, "Fixture title", result.Title)
	require.Equal(t, "dlgetchu", result.Source)
	require.Equal(t, sourceURL, result.SourceURL)
	require.Equal(t, []string{
		"https://dl.getchu.com/data/item_img/demo/first.jpg",
		"https://dl.getchu.com/data/item_img/demo/second.webp",
	}, result.ScreenshotURL, "duplicates must not displace the next distinct screenshot")
}

func TestGetURLNumericCandidateAvoidsSearch(t *testing.T) {
	const wantURL = "https://dl.getchu.com/i/item12345"
	calls := 0
	s := newScraper(&models.ScraperSettings{Enabled: true, BaseURL: "https://dl.getchu.com"}, nil, models.FlareSolverrConfig{})
	t.Cleanup(func() { require.NoError(t, s.Close()) })
	s.client.SetTransport(offlineFixtureTransport(func(req *http.Request) (*http.Response, error) {
		calls++
		require.Equal(t, http.MethodGet, req.Method)
		require.Equal(t, wantURL, req.URL.String(), "a valid numeric detail page must bypass keyword searches")
		return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Request: req, Body: io.NopCloser(strings.NewReader("<title>Fixture title</title>"))}, nil
	}))

	got, err := s.GetURL(context.Background(), " 12345 ")
	require.NoError(t, err)
	require.Equal(t, wantURL, got)
	require.Equal(t, 1, calls)
}
