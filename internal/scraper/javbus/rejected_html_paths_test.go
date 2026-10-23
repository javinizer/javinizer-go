package javbus

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/PuerkitoBio/goquery"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/require"
)

func TestRejectedHTMLPublicRoutes(t *testing.T) {
	rejected := strings.Repeat("<dl>", 511)
	_, err := goquery.NewDocumentFromReader(strings.NewReader(strings.Repeat("<dl>", 510)))
	require.NoError(t, err)
	_, err = goquery.NewDocumentFromReader(strings.NewReader(rejected))
	require.ErrorContains(t, err, "open stack of elements exceeds 512 nodes")
	for _, search := range []bool{false, true} {
		t.Run(map[bool]string{false: "direct", true: "search"}[search], func(t *testing.T) {
			s := newScraper(&models.ScraperSettings{Enabled: true, Language: "ja"}, nil, models.FlareSolverrConfig{})
			calls := 0
			s.client.SetTransport(javbusFixtureRoundTripper(func(r *http.Request) (*http.Response, error) {
				calls++
				require.Equal(t, "/ja/ABC-123", r.URL.Path)
				return &http.Response{StatusCode: 200, Header: make(http.Header), Request: r, Body: io.NopCloser(strings.NewReader(rejected))}, nil
			}))
			var result *models.ScraperResult
			var err error
			if search {
				result, err = s.Search(context.Background(), "https://www.javbus.com/ABC-123")
			} else {
				result, err = s.ScrapeURL(context.Background(), "https://www.javbus.com/ABC-123")
			}
			require.Nil(t, result)
			require.ErrorContains(t, err, "failed to parse JavBus detail page")
			require.ErrorContains(t, err, "open stack of elements exceeds 512 nodes")
			require.Equal(t, 1, calls)
		})
	}
	s := newScraper(&models.ScraperSettings{Enabled: true}, nil, models.FlareSolverrConfig{})
	require.Empty(t, s.findDetailURL(rejected, "https://www.javbus.com", "ABC-123"))
}

func TestEmptyHTMLAttributesDoNotCreateMediaOrDetailURLs(t *testing.T) {
	s := newScraper(&models.ScraperSettings{Enabled: true}, nil, models.FlareSolverrConfig{})
	require.Empty(t, s.findDetailURL(`<a class="movie-box" href="">empty</a>`, "https://www.javbus.com", "ABC-123"))
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(`<a class="sample-box" href=" "></a><a class="sample-box"><img data-original="https://example.com/valid.jpg"></a>`))
	require.NoError(t, err)
	require.Equal(t, []string{"https://example.com/valid.jpg"}, extractScreenshotURLs(doc, "https://www.javbus.com"))
}

func TestScraperSettingsRejectInvalidLanguageAndHost(t *testing.T) {
	require.ErrorContains(t, validateScraperSettings(&models.ScraperSettings{Language: "fr"}), "language must be")
	require.Error(t, validateScraperSettings(&models.ScraperSettings{Language: "ja", BaseURL: "https://untrusted.example"}))
}

func TestLanguageOnlyURLHasNoMovieID(t *testing.T) {
	s := newScraper(&models.ScraperSettings{Enabled: true}, nil, models.FlareSolverrConfig{})
	id, err := s.ExtractIDFromURL("https://www.javbus.com/ja/")
	require.Empty(t, id)
	require.EqualError(t, err, "URL has no ID path segment, not a detail page")
}

func TestSearchPropagatesCanceledDetailRequest(t *testing.T) {
	s := newScraper(&models.ScraperSettings{Enabled: true}, nil, models.FlareSolverrConfig{})
	calls := 0
	s.client.SetTransport(javbusFixtureRoundTripper(func(req *http.Request) (*http.Response, error) {
		calls++
		require.ErrorIs(t, req.Context().Err(), context.Canceled)
		return nil, req.Context().Err()
	}))
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	result, err := s.Search(ctx, "https://www.javbus.com/ABC-123")
	require.Nil(t, result)
	require.ErrorIs(t, err, context.Canceled)
	require.ErrorContains(t, err, "failed to fetch JavBus detail page")
	require.Equal(t, 1, calls)
}
